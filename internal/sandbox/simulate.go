package sandbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/handler/api"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/punchwire"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/internal/sun"
)

// POST /v1/sandbox/taps {employeeRef, locationRef, channel} (ADR 0028 §11). The body's three keys,
// all required, nothing else (api.ParseObject's rules):
//
//	employeeRef  the key's business's employee, by the integrator's ref -- the live one, else the
//	             most recent deactivated one (a deactivated person's tap is §5 row 4's reject)
//	locationRef  the venue, by its ref: the key's business's own first; else exactly one holding
//	             PEER's (holding_site_locations, ADR 0027 §2) -- a tap at a peer's venue is the
//	             holding tap (ADR 0027 §5: the employee's business records it, the venue's sees it)
//	channel      "nfc" (the chip's signed read) or "qr" (the printed code: no counter, no MAC)
//
// ORDER: the body (400/422), then the write -- the runner's transaction in the key's business, the
// suspension checkpoint FIRST (the zero class: a tap is not in §4.7's table, so it is closed while
// the business is suspended -- api/suspension.go), the two refs (404), the plaque (provisioned at an
// own venue; 404 at a peer venue that has none), the pre-check, the record. 201 with the outcome.
//
// IDEMPOTENCY: the Idempotency-Key ledger keeps the record's id; a replay reads the record again
// (GetTapOutcome) and answers the same body, the refs coming from the request it replays (the
// request hash binds the body). ⚠️ THE TAP COMMITS BEFORE THE LEDGER ROW DOES: Record runs in its
// own transactions (it must -- the plaque's counter is advanced in the plaque's business, the
// person's lock is the employee's), so a process that dies between the two leaves the tap recorded
// and the key unclaimed; the retry is then a second touch, and within the debounce window it is
// recorded as `ignored` (§5 row 5) -- a record, never a lost or a doubled punch.

// The body's keys, exactly.
const (
	fieldEmployeeRef = "employeeRef"
	fieldLocationRef = "locationRef"
	fieldChannel     = "channel"
)

var tapFields = []string{fieldEmployeeRef, fieldLocationRef, fieldChannel}

// The 404s' reasons (details.reason): which of the request's names found nothing.
const (
	reasonNoEmployee = "unknown_employee"
	reasonNoLocation = "unknown_location"
	reasonNoPlaque   = "no_plaque"
)

var (
	problemNoEmployee = api.NotFound("No employee of this business has that reference.",
		map[string]any{"field": fieldEmployeeRef, "reason": reasonNoEmployee})
	problemNoLocation = api.NotFound("Neither this business nor a business of its group has a location with that reference.",
		map[string]any{"field": fieldLocationRef, "reason": reasonNoLocation})
	problemNoPlaque = api.NotFound("That location has no virtual plaque to tap. A partner company's location gets one on its own business's first simulated tap there.",
		map[string]any{"field": fieldLocationRef, "reason": reasonNoPlaque})
	problemAmbiguousLocation = api.InvalidField(fieldLocationRef, api.ReasonInvalid,
		"More than one business of the group has a location with that reference.")
	problemChannel = api.InvalidField(fieldChannel, api.ReasonInvalid, `channel must be "nfc" or "qr".`)
)

// tapRequest is a parsed body.
type tapRequest struct {
	employeeRef, locationRef string
	nfc                      bool
}

func parseTap(body []byte) (tapRequest, *api.Problem) {
	raw, prob := api.ParseObject(body, tapFields)
	if prob != nil {
		return tapRequest{}, prob
	}
	var out tapRequest
	ref := func(key string) (string, *api.Problem) {
		v, present, prob := api.StringField(raw, key)
		switch {
		case prob != nil:
			return "", prob
		case !present:
			return "", api.InvalidField(key, api.ReasonRequired, key+" is required.")
		case !tenant.ValidAPIExternalRef(v):
			return "", api.InvalidField(key, api.ReasonInvalid,
				key+" must be 1 to 64 characters of A-Z, a-z, 0-9, '.', '_', '~' and '-', starting with a letter or a digit.")
		}
		return v, nil
	}
	if out.employeeRef, prob = ref(fieldEmployeeRef); prob != nil {
		return tapRequest{}, prob
	}
	if out.locationRef, prob = ref(fieldLocationRef); prob != nil {
		return tapRequest{}, prob
	}
	ch, present, prob := api.StringField(raw, fieldChannel)
	switch {
	case prob != nil:
		return tapRequest{}, prob
	case !present:
		return tapRequest{}, api.InvalidField(fieldChannel, api.ReasonRequired, "channel is required.")
	case ch == string(sun.ChannelNFC):
		out.nfc = true
	case ch == string(sun.ChannelQR):
	default:
		return tapRequest{}, problemChannel
	}
	return out, nil
}

// venueRef is the venue a request names: whose (site) and which.
type venueRef struct {
	site, venue uuid.UUID
	name        string
	own         bool
}

func (s *Simulation) simulate(w http.ResponseWriter, r *http.Request, write api.WriteFunc) {
	body, prob := api.ReadBody(r)
	if prob != nil {
		api.WriteProblem(w, prob)
		return
	}
	in, prob := parseTap(body)
	if prob != nil {
		api.WriteProblem(w, prob)
		return
	}
	p, ok := api.PrincipalOf(r)
	if !ok {
		s.log.ErrorContext(r.Context(), "sandbox: a simulation reached the handler without a resolved key")
		api.WriteProblem(w, api.Unavailable())
		return
	}
	source := httpx.ClientIP(r)
	out, _, ok := write(w, r, body, func(ctx context.Context, tx pgx.Tx, gate *api.Gate) (apikey.Outcome, error) {
		var unclassified api.WriteClass // the zero class: closed while the business is suspended
		if err := gate.Admit(ctx, unclassified); err != nil {
			return apikey.Outcome{}, err
		}
		emp, err := employeeByRef(ctx, tx, p.TenantID, in.employeeRef)
		if err != nil {
			return apikey.Outcome{}, err
		}
		v, err := venueByRef(ctx, tx, p.TenantID, in.locationRef)
		if err != nil {
			return apikey.Outcome{}, err
		}
		if v.own {
			if _, err := s.provision(ctx, p, v.venue, v.name); err != nil {
				return apikey.Outcome{}, err
			}
		}
		id, err := s.tap(ctx, p, emp, v, in.nfc, source)
		if err != nil {
			return apikey.Outcome{}, err
		}
		return apikey.Outcome{Status: http.StatusCreated, Ref: &id}, nil
	})
	if !ok {
		return
	}
	if out.Ref == nil {
		s.log.ErrorContext(r.Context(), "sandbox: a recorded simulation carries no record id")
		api.WriteProblem(w, api.Unavailable())
		return
	}
	var rec store.GetTapOutcomeRow
	err := s.data.WithTenant(r.Context(), p.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		rec, err = store.New(tx).GetTapOutcome(ctx, store.GetTapOutcomeParams{TenantID: p.TenantID, ID: *out.Ref})
		return err
	})
	if err != nil {
		s.log.ErrorContext(r.Context(), "sandbox: reading the simulated tap back failed", "err", err)
		api.WriteProblem(w, api.Unavailable())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(out.Status)
	_ = json.NewEncoder(w).Encode(tapBodyOf(rec, in))
}

// employeeByRef is the key's business's employee under ref: the live row, else the latest one.
func employeeByRef(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, ref string) (uuid.UUID, error) {
	id, err := store.New(tx).GetLatestEmployeeByExternalRef(ctx, store.GetLatestEmployeeByExternalRefParams{
		TenantID: tenantID, ExternalRef: &ref,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, problemNoEmployee
	case err != nil:
		return uuid.Nil, fmt.Errorf("sandbox: read the employee: %w", err)
	}
	return id, nil
}

// venueByRef finds the venue: the key's business's own (WithTenant + an explicit filter -- the
// runner's transaction), else a holding peer's through holding_site_locations on the same context
// (the closed list; ADR 0027 §2). Two peers holding the ref is 422: the request does not say which.
func venueByRef(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID, ref string) (venueRef, error) {
	own, err := store.New(tx).LockLocationByExternalRef(ctx, store.LockLocationByExternalRefParams{
		TenantID: tenantID, ExternalRef: &ref,
	})
	switch {
	case err == nil:
		return venueRef{site: tenantID, venue: own.ID, name: own.Name, own: true}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return venueRef{}, fmt.Errorf("sandbox: read the location: %w", err)
	}
	peers, err := db.HoldingOn(tx).SiteLocations(ctx)
	if err != nil {
		return venueRef{}, fmt.Errorf("sandbox: read the group's locations: %w", err)
	}
	var found []venueRef
	for _, l := range peers {
		if l.ExternalRef != nil && *l.ExternalRef == ref {
			found = append(found, venueRef{site: l.SiteTenantID, venue: l.LocationID, name: l.Name})
		}
	}
	switch len(found) {
	case 0:
		return venueRef{}, problemNoLocation
	case 1:
		return found[0], nil
	default:
		return venueRef{}, problemAmbiguousLocation
	}
}

// tap is the touch: the plaque, the URL it emits, the tap page's pre-check, the record.
func (s *Simulation) tap(ctx context.Context, p apikey.Principal, employee uuid.UUID, v venueRef, nfc bool, source netip.Addr) (uuid.UUID, error) {
	tag, err := s.plaqueAt(ctx, v.site, v.venue)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return uuid.Nil, problemNoPlaque
	case err != nil:
		return uuid.Nil, fmt.Errorf("sandbox: resolve the plaque: %w", err)
	}
	path, err := s.tapPath(tag, nfc)
	if err != nil {
		return uuid.Nil, err
	}
	u, err := url.Parse(path)
	if err != nil {
		return uuid.Nil, fmt.Errorf("sandbox: the minted URL: %w", err)
	}
	params, err := sun.Parse(u.Query())
	if err != nil {
		return uuid.Nil, fmt.Errorf("sandbox: the minted URL does not parse: %w", err)
	}
	pv, err := s.verifier.PreviewWithoutReplayProtection(ctx, params)
	switch {
	case errors.Is(err, sun.ErrUnknownTag):
		return uuid.Nil, problemNoPlaque
	case err != nil:
		return uuid.Nil, fmt.Errorf("sandbox: the pre-check: %w", err)
	}
	req := checkin.Request{
		SessionTenantID: p.TenantID,
		EmployeeID:      employee,
		TagUID:          params.UID,
		Ctr:             params.Ctr,
		Channel:         tap.Channel(params.Channel),
		CMACVerified:    pv.CMACValid,
		TagTenantID:     pv.TenantID,
		// The page was "minted" now: the touch and the press are one call (sys:tap-freshness).
		PageIssuedAt: time.Now().UTC(),
		// The caller's own address is the "where" (the tap page's rule: httpx.ClientIP). No GPS.
		SourceIP: source,
	}
	res, err := s.taps.Record(ctx, req)
	switch {
	case errors.Is(err, checkin.ErrUnknownTag):
		return uuid.Nil, problemNoPlaque
	case err != nil:
		return uuid.Nil, fmt.Errorf("sandbox: record the tap: %w", err)
	case res.Outcome != checkin.OutcomeRecorded || res.TransactionID == uuid.Nil:
		// No record: the decision redirected (a plaque of a business outside the group -- which the
		// ref lookup does not reach -- or the group ended between the lookup and the tap).
		return uuid.Nil, problemNoPlaque
	}
	return res.TransactionID, nil
}

// tapBody is the response: the record's outcome, in the API's identifiers (K-8).
type tapBody struct {
	// PunchID is the id the feed and the webhook carry for this tap when it is a punch.
	PunchID string `json:"punchId"`
	// InFeed is whether it is a punch now: an ok nfc/qr tap (K-4). A flagged one enters the feed,
	// under the same id, when the employer approves it.
	InFeed    bool    `json:"inFeed"`
	Verdict   string  `json:"verdict"`
	Direction *string `json:"direction"`
	// Trust and Rule are null for a tap at a partner company's venue (K-39).
	Trust      *int16    `json:"trust"`
	Rule       *string   `json:"rule"`
	Channel    string    `json:"channel"`
	OccurredAt time.Time `json:"occurredAt"`
	Employee   refBody   `json:"employee"`
	Location   refBody   `json:"location"`
	TagID      string    `json:"tagId"`
}

type refBody struct {
	ID          string `json:"id"`
	ExternalRef string `json:"externalRef"`
}

func tapBodyOf(r store.GetTapOutcomeRow, in tapRequest) tapBody {
	b := tapBody{
		PunchID: punchwire.PunchIDPrefix + r.ID.String(),
		InFeed:  r.Verdict == "ok" && !r.Practice && (r.Channel == string(sun.ChannelNFC) || r.Channel == string(sun.ChannelQR)),
		Verdict: r.Verdict, Direction: r.Type, Trust: r.Trust, Rule: r.MatchedSid, Channel: r.Channel,
		OccurredAt: r.OccurredAt,
		Employee:   refBody{ExternalRef: in.employeeRef},
		Location:   refBody{ExternalRef: in.locationRef},
	}
	if r.SiteLocationID != nil {
		// K-39 (ADR 0027 §9): a tap at a PARTNER's venue was judged on that venue's evidence, and the
		// employer is not shown it -- the trust and the deciding rule say whether the caller's address
		// is in the partner's ranges. The verdict is the residue every employer's record carries (§13
		// item 5), as on the day list (internal/domain/ledger's nameSiteRecords).
		b.Trust, b.Rule = nil, nil
	}
	if r.EmployeeID != nil {
		b.Employee.ID = punchwire.EmployeeIDPrefix + r.EmployeeID.String()
	}
	if l := firstID(r.LocationID, r.SiteLocationID); l != nil {
		b.Location.ID = punchwire.LocationIDPrefix + l.String()
	}
	for _, uid := range []*string{r.TagUid, r.SiteTagUid} {
		if uid != nil && strings.TrimSpace(*uid) != "" {
			b.TagID = punchwire.TagIDPrefix + strings.TrimSpace(*uid)
			break
		}
	}
	return b
}

func firstID(ids ...*uuid.UUID) *uuid.UUID {
	for _, id := range ids {
		if id != nil {
			return id
		}
	}
	return nil
}
