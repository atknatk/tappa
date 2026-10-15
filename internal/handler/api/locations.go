package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// PUT /v1/locations/{ref} (M11 API-5; ADR 0028 §9, K-7, K-18, K-20, K-24). The integrator's
// venue, addressed by ITS ref inside the KEY's business: a new ref opens a venue (201), a known
// one is renamed / relabelled (200), an identical PUT changes nothing (200, no trail row). The
// evidence a tap is judged on -- address ranges, the coordinate -- is never written here: a
// venue the API opens records its taps as `flag` until the panel gives it some (plan §2 item 10).
// The work is internal/domain/tenant's PutVenueTx, the panel's own venue domain, so the name
// passes the panel's rule and the ref the schema's.
//
// THE BODY (decided here -- ADR 0028 names no unknown-field policy):
//
//	name         string, required; the panel's venue-name rule (trimmed, 1..80 runes)
//	active       boolean, required; K-20's label -- the tap path never reads it
//	timezone     string, optional; when present it must be the business's zone (K-7) -> else
//	             422 timezone_mismatch naming the business's zone
//	externalRef  string, optional; when present it must be the path's ref
//
// A KEY NAMED TWICE IS 422 (validation_failed, reason duplicate_field), not "the last one wins".
// ANY OTHER KEY IS 422 (validation_failed, reason unknown_field), and keys match EXACTLY (not
// encoding/json's case-folding): a misspelt "acitve":false silently dropped would leave the
// label as it was and answer 200, and a coordinate or an address range sent here must not look
// accepted when the API never sets evidence. A refusal names the field from a fixed list,
// never the unknown key the request sent (errors.go: no message repeats the request).
//
// ORDER: the ref (422), the body (400/422), then the write -- the runner's transaction, the
// suspension checkpoint FIRST (WriteLocationPut is closed while the business is suspended:
// ADR 0028 §10), then the domain. Every refusal inside the write rolls it back, so it leaves no
// venue, no trail row and no Idempotency-Key row.

// Locations is the venue domain's API half (*tenant.Venues), declared at the consumer (§7).
type Locations interface {
	PutVenueTx(ctx context.Context, tx pgx.Tx, c tenant.VenuePut) (tenant.VenuePutResult, error)
	APIVenue(ctx context.Context, tenantID, venueID uuid.UUID) (tenant.APIVenue, error)
}

var _ Locations = (*tenant.Venues)(nil)

// locationPath is the route under Prefix.
const locationPath = "/locations/{ref}"

// locationIDPrefix is K-8's external id prefix of a venue.
const locationIDPrefix = "loc_"

// The body's keys, exactly.
const (
	fieldName        = "name"
	fieldActive      = "active"
	fieldTimezone    = "timezone"
	fieldExternalRef = "externalRef"
	fieldRef         = "ref" // the path parameter
)

var locationFields = []string{fieldName, fieldActive, fieldTimezone, fieldExternalRef}

// The validation reasons: a closed set, so details.reason is machine-readable.
const (
	reasonRequired     = "required"
	reasonType         = "type"
	reasonInvalid      = "invalid"
	reasonMismatch     = "mismatch"
	reasonUnknownField = "unknown_field"
	// reasonHiddenCharacter: a new or changed name carries a character the name rule refuses (API-5
	// review, fourth round: "invalid" could not be told from a length refusal).
	reasonHiddenCharacter = "hidden_character"
	// reasonDuplicateField: a key twice in the object (API-5 review).
	reasonDuplicateField = "duplicate_field"
)

func invalidField(field, reason, message string) *Problem {
	return &Problem{Code: CodeValidationFailed, Message: message, Details: map[string]any{"field": field, "reason": reason}}
}

var (
	problemLocationBody = &Problem{Code: CodeInvalidRequest,
		Message: "The request body must be one JSON object.", Details: map[string]any{"reason": "malformed_json"}}
	problemLocationRef = invalidField(fieldRef, reasonInvalid,
		"The location reference must be 1 to 64 characters of A-Z, a-z, 0-9, '.', '_', '~' and '-', starting with a letter or a digit.")
	problemUnknownField = &Problem{Code: CodeValidationFailed,
		Message: "The body carries a field this endpoint does not accept.",
		Details: map[string]any{"reason": reasonUnknownField, "allowed": locationFields}}
	problemNotUTF8 = &Problem{Code: CodeInvalidRequest,
		Message: "The request body must be UTF-8, and its strings cannot carry an unpaired surrogate escape.",
		Details: map[string]any{"reason": "invalid_utf8"}}
	problemDuplicateField = &Problem{Code: CodeValidationFailed,
		Message: "The body names one field twice.", Details: map[string]any{"reason": reasonDuplicateField}}
	problemNameRequired = invalidField(fieldName, reasonRequired, "name is required.")
	problemNameType     = invalidField(fieldName, reasonType, "name must be a string.")
	problemNameInvalid  = invalidField(fieldName, reasonInvalid,
		"name must be 1 to 80 characters once surrounding spaces are removed.")
	problemNameHidden = invalidField(fieldName, reasonHiddenCharacter,
		"name cannot carry a control character, a line or paragraph separator, a direction override or isolate (U+202A-U+202E, U+2066-U+2069), a zero-width space, a word joiner or an inner U+FEFF, nor a joiner or tag character outside an emoji, a flag or a word; emoji, flags and every script's letters are fine.")
	problemActiveRequired = invalidField(fieldActive, reasonRequired, "active is required.")
	problemActiveType     = invalidField(fieldActive, reasonType, "active must be true or false.")
	problemTimezoneType   = invalidField(fieldTimezone, reasonType, "timezone must be a non-empty string.")
	problemRefType        = invalidField(fieldExternalRef, reasonType, "externalRef must be a string.")
	problemRefMismatch    = invalidField(fieldExternalRef, reasonMismatch,
		"externalRef in the body must be the reference in the path.")
	problemVenueGone = &Problem{Code: CodeNotFound,
		Message: "The location this Idempotency-Key recorded no longer exists."}
)

func timezoneMismatch(zone string) *Problem {
	return &Problem{Code: CodeTimezoneMismatch,
		Message: "A location's time zone is its business's; send the business's zone or none.",
		Details: map[string]any{"field": fieldTimezone, "businessTimezone": zone}}
}

// locationPut is a parsed body.
type locationPut struct {
	name     string
	active   bool
	timezone string
}

// parseLocationBody reads exactly one JSON object with locationFields' keys and types.
func parseLocationBody(body []byte, ref string) (locationPut, *Problem) {
	// A body that is not UTF-8 is not JSON (RFC 8259 §8.1), and encoding/json would turn each bad
	// byte into U+FFFD and STORE that -- where the panel's form path drops the bytes instead
	// (venueName's ToValidUTF8). Neither is what the integrator sent, so it is refused (API-5
	// review).
	if !utf8.Valid(body) {
		return locationPut{}, problemNotUTF8
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	raw, duplicate, ok := readObject(dec)
	if !ok {
		return locationPut{}, problemLocationBody
	}
	if _, err := dec.Token(); err != io.EOF {
		return locationPut{}, problemLocationBody
	}
	if duplicate {
		return locationPut{}, problemDuplicateField
	}
	for k := range raw {
		known := false
		for _, f := range locationFields {
			known = known || k == f
		}
		if !known {
			return locationPut{}, problemUnknownField
		}
	}
	var out locationPut
	str := func(key string, required, typ *Problem) (string, bool, *Problem) {
		v, ok := raw[key]
		if !ok {
			return "", false, required
		}
		var s string
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) || json.Unmarshal(v, &s) != nil {
			return "", true, typ
		}
		if brokenEscape(v, s) {
			return "", true, problemNotUTF8
		}
		return s, true, nil
	}
	name, _, prob := str(fieldName, problemNameRequired, problemNameType)
	if prob != nil {
		return locationPut{}, prob
	}
	out.name = name
	act, ok := raw[fieldActive]
	if !ok {
		return locationPut{}, problemActiveRequired
	}
	if bytes.Equal(bytes.TrimSpace(act), []byte("null")) || json.Unmarshal(act, &out.active) != nil {
		return locationPut{}, problemActiveType
	}
	tz, present, prob := str(fieldTimezone, nil, problemTimezoneType)
	switch {
	case prob != nil:
		return locationPut{}, prob
	case present && tz == "":
		return locationPut{}, problemTimezoneType
	}
	out.timezone = tz
	bodyRef, present, prob := str(fieldExternalRef, nil, problemRefType)
	switch {
	case prob != nil:
		return locationPut{}, prob
	case present && bodyRef != ref:
		return locationPut{}, problemRefMismatch
	}
	return out, nil
}

// locationRef is the path's ref, PERCENT-DECODED EXACTLY ONCE and then held to the strict pattern
// (API-5 review). chi's {ref} is not a stable source for that: it is the segment as the client
// escaped it when net/url keeps a RawPath and the already-decoded segment when it does not (a
// "%2541" arrives as "%41" -- decoding that again would decode twice). So the segment is read
// off the request's ESCAPED path, which is what the client sent, and decoded once. An
// unnecessarily encoded but legal ref -- "%41BC", or "~" as Java's URLEncoder writes it, "%7E" --
// is then the ref it spells; whatever decodes to a character outside the pattern ("/", a control
// character, the "%" of a double encoding) is still 422. Two spellings of one ref are one venue;
// for an Idempotency-Key they are two requests (the request hash reads the escaped path -- ADR
// 0028 "API-4 uygulama notu").
func locationRef(r *http.Request) (string, bool) {
	seg, found := strings.CutPrefix(r.URL.EscapedPath(), Prefix+strings.TrimSuffix(locationPath, "{ref}"))
	if !found || strings.Contains(seg, "/") {
		return "", false
	}
	ref, err := url.PathUnescape(seg)
	if err != nil || !tenant.ValidAPIExternalRef(ref) {
		return "", false
	}
	return ref, true
}

// brokenEscape reports whether decoding the JSON string raw produced a U+FFFD the client did not
// send: encoding/json turns an unpaired surrogate escape ("\ud800") into U+FFFD silently (API-5
// review). The body is already known to be UTF-8, so every U+FFFD in decoded is either a literal
// one in raw, a "\ufffd" escape in raw, or a broken surrogate -- the last is refused.
func brokenEscape(raw []byte, decoded string) bool {
	got := strings.Count(decoded, "\uFFFD")
	if got == 0 {
		return false
	}
	// Count the \ufffd ESCAPES only where the backslash starts an escape: in `\\ufffd` the backslash is
	// itself escaped and the text after it is plain (fourth round, API-5 review -- that spelling made a
	// following "\ud800" pass).
	escapes := 0
	lower := strings.ToLower(string(raw))
	for i := 0; i < len(lower); i++ {
		if lower[i] != '\\' || i+1 >= len(lower) {
			continue
		}
		if strings.HasPrefix(lower[i+1:], "ufffd") {
			escapes++
		}
		i++ // the escaped character, whatever it is, is not the start of another escape
	}
	sent := bytes.Count(raw, []byte("\uFFFD")) + escapes
	return got > sent
}

// readObject reads ONE JSON object member by member -- encoding/json's map decoding keeps the last
// of two equal keys and says nothing, and {"active":true,"active":false} would then be a silent
// "false" (API-5 review: the same "nothing is dropped quietly" argument as the unknown key). Keys
// are compared after JSON unescaping ("n\u0061me" is "name"). ok=false for anything that is not one
// well-formed object.
func readObject(dec *json.Decoder) (raw map[string]json.RawMessage, duplicate, ok bool) {
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, false, false
	}
	raw = map[string]json.RawMessage{}
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, false, false
		}
		k, isKey := t.(string)
		if !isKey {
			return nil, false, false
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, false, false
		}
		if _, seen := raw[k]; seen {
			duplicate = true
		}
		raw[k] = v
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, false, false
	}
	return raw, duplicate, true
}

// locationBody is the response.
type locationBody struct {
	ID          string    `json:"id"`
	ExternalRef string    `json:"externalRef"`
	Name        string    `json:"name"`
	Timezone    string    `json:"timezone"`
	Active      bool      `json:"active"`
	CreatedAt   time.Time `json:"createdAt"`
}

func locationOf(v tenant.APIVenue) locationBody {
	return locationBody{ID: locationIDPrefix + v.ID.String(), ExternalRef: v.ExternalRef, Name: v.Name,
		Timezone: v.Timezone, Active: v.Active, CreatedAt: v.CreatedAt.UTC()}
}

func (s *Surface) putLocation(w http.ResponseWriter, r *http.Request) {
	ref, ok := locationRef(r)
	if !ok {
		writeProblem(w, problemLocationRef)
		return
	}
	body, prob := readBody(r)
	if prob != nil {
		writeProblem(w, prob)
		return
	}
	in, prob := parseLocationBody(body, ref)
	if prob != nil {
		writeProblem(w, prob)
		return
	}
	p, _ := PrincipalOf(r)
	var done tenant.VenuePutResult
	out, replayed, ok := s.write(w, r, body, func(ctx context.Context, tx pgx.Tx, g *Gate) (apikey.Outcome, error) {
		if err := g.Admit(ctx, WriteLocationPut); err != nil {
			return apikey.Outcome{}, err
		}
		res, err := s.locations.PutVenueTx(ctx, tx, tenant.VenuePut{
			TenantID: p.TenantID, ActorID: *p.Actor(), Via: p.Attribution().Via,
			ExternalRef: ref, Name: in.name, Timezone: in.timezone, Active: in.active,
		})
		var tz *tenant.TimezoneMismatch
		switch {
		case err == nil:
		case errors.As(err, &tz):
			return apikey.Outcome{}, timezoneMismatch(tz.BusinessZone)
		case errors.Is(err, tenant.ErrNameHiddenCharacter):
			return apikey.Outcome{}, problemNameHidden
		case errors.Is(err, tenant.ErrVenueName):
			return apikey.Outcome{}, problemNameInvalid
		case errors.Is(err, tenant.ErrExternalRef):
			return apikey.Outcome{}, problemLocationRef
		default:
			return apikey.Outcome{}, err
		}
		done = res
		status := http.StatusOK
		if res.Created {
			status = http.StatusCreated
		}
		id := res.Venue.ID
		return apikey.Outcome{Status: status, Ref: &id}, nil
	})
	if !ok {
		return
	}
	venue := done.Venue
	if replayed {
		if out.Ref == nil {
			s.unavailable(w, r, "a replayed location write carries no venue id", errors.New("nil result_ref"))
			return
		}
		v, err := s.locations.APIVenue(r.Context(), p.TenantID, *out.Ref)
		switch {
		case errors.Is(err, tenant.ErrUnknownVenue):
			writeProblem(w, problemVenueGone)
			return
		case err != nil:
			s.unavailable(w, r, "reading a replayed location failed", err)
			return
		}
		venue = v
	}
	writeJSON(w, out.Status, locationOf(venue))
}

// writeJSON answers status with v as JSON.
func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		writeProblem(w, problemUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(append(b, '\n'))
}
