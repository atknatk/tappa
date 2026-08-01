package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// POST /api/checkin — the one request that records attendance (M5-05).
//
// THIS FILE IS A BOUNDARY AND NOTHING ELSE. §3 puts business rules and SQL out of
// internal/handler, and on the most consequential path in the product that rule
// earns its keep: everything below parses a form, hands typed facts to
// internal/domain/checkin, and renders what comes back. There is no verdict
// logic here, no query, and no decision about what a tap means.
//
// WHAT THE FORM CARRIES, and why it is so little:
//
//	ctx          the SERVER-SIGNED tap context minted by GET /t. It is the only
//	             thing that says which plaque, which counter, which channel and
//	             whether the chip's MAC verified — none of which a client may
//	             choose (internal/handler/tapcontext.go).
//	lat/lng      one geolocation fix, read when the button was pressed (§4.2).
//	             CLIENT INPUT, and treated as such: out-of-range values are
//	             dropped to "no fix" rather than refused, because GPS is BACKUP
//	             evidence and a bad coordinate must cost trust, not the record.
//	acc          the fix's accuracy, which tap.js sends and this endpoint READS
//	             NOTHING FROM — said plainly rather than left to be discovered.
//	             There is no column for it (migration 00005) and no rule that
//	             consults it, so storing or judging on it would be a schema and a
//	             policy decision, not a parsing one. A wide fix currently counts
//	             the same as a tight one, which is a real limit: a 5 km-accurate
//	             "fix" inside a 150 m radius is evidence of nothing. Worth its own
//	             task, and it is not this one.
//	occurred_at  optional, RFC 3339, for a client that queued a tap offline
//	             (M9-01). It is the one unverified input that matters, and it is
//	             bounded by a guardrail nobody can switch off — see below.
//
// WHAT IS NOT IN THE FORM, deliberately: no tag, no counter, no channel, no
// verdict, no practice flag, no employee. Every one of those would be a client
// declaring something the server is supposed to establish (ADR 0004 §8).
//
// 🔴 THE COUNTER IS ADVANCED BY THE REQUEST THIS FILE SERVES, and only by it. GET
// /t deliberately advances nothing, so a person who opens the page and walks away
// does not spend a counter value their real tap needs — which means the replay
// guard of §4.4 runs HERE or it does not run at all. It runs inside the domain
// service, in one atomic statement.

// checkinRecorder is the slice of internal/domain/checkin this screen needs,
// declared HERE at the consumer (§7). The narrow interface is what keeps the
// handler from reaching for anything else the service might grow.
type checkinRecorder interface {
	Record(ctx context.Context, req checkin.Request) (checkin.Result, error)
}

// Request-body ceiling. The real body is a signed context (~200 bytes), three
// short numbers and possibly a timestamp; a kilobyte is already generous. It is
// bounded because ParseForm reads the whole body into memory and this endpoint
// sits in front of a database write.
const checkinMaxBody = 8 << 10

// occurredAtField is the optional client-declared tap time. It is RFC 3339 with
// an offset, so a client cannot leave the zone to interpretation — a naive local
// timestamp is exactly how an hour goes missing twice a year.
const occurredAtField = "occurred_at"

// Checkin serves POST /api/checkin.
func (t *Tap) Checkin(w http.ResponseWriter, r *http.Request) {
	id := httpx.IdentityOf(r)
	switch id.State {
	case httpx.SessionUnresolved:
		// NOT "no session" — the same distinction GET /t makes, and for the same
		// reason (state.md hand-off 3): the zero Identity has Err == nil AND
		// Live() == false, so treating it as a session-less tap would answer a
		// database outage, or a route that forgot the middleware, with a redirect
		// to activation. We do not know who this is, so we say so.
		t.log.Error("checkin: no resolved identity on the request",
			"hint", "mount httpx.Identify in front of POST /api/checkin",
			"err", id.Err, "path", r.URL.Path)
		t.renderProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return

	case httpx.SessionAbsent, httpx.SessionRevoked:
		// §5 ROW 3: no valid session -> activation, and NO RECORD.
		//
		// 🔴 IT IS ALSO THE ONLY THING THIS ENDPOINT COULD DO, and that is worth
		// stating rather than presenting the redirect as a free choice. The tap
		// context is signed over the SESSION ID, so without a session there is no
		// key to verify it with: the request carries no trustworthy tag, counter or
		// channel, and there is nothing to attribute a record to. Migration 00005
		// keeps employee_id nullable precisely so an anonymous reject CAN be
		// recorded, and that shape is unreachable today — GET /t already redirects
		// a session-less visitor, so the button is never rendered for them. Stated
		// as a LIMIT, not claimed as a design: the schema allows a record this flow
		// cannot produce.
		t.redirectToActivation(w, r)
		return

	case httpx.SessionLive:
		// fall through

	default:
		t.log.Error("checkin: unknown session state", "state", int(id.State))
		t.renderProblem(w, r, http.StatusInternalServerError, tapProblemServer)
		return
	}

	// ORIGIN, checked as DEPTH rather than as the defence. What actually stops a
	// cross-site POST here is that the body must carry a context this server
	// minted FOR THIS SESSION — an attacker who cannot read the tap page cannot
	// produce one — and that SameSite=Lax keeps the session cookie off a
	// cross-site POST in the first place, which makes the signature unverifiable.
	// An Origin that disagrees is still refused, because at that point the request
	// is not one of ours and refusing costs a legitimate tap nothing.
	//
	// TWO VALUES ARE DELIBERATELY ACCEPTED and both are boundaries rather than
	// oversights. An ABSENT Origin passes, because it is absent on same-origin form
	// posts in some browsers and the signature does not depend on a header. The
	// literal `null` passes too — it is what a sandboxed iframe, a `data:` document
	// or some privacy tooling sends, so refusing it would break legitimate callers
	// while an attacker who could produce a valid context would simply omit the
	// header instead. Neither is a hole in the defence, because neither is the
	// defence: that is the session-bound MAC, with SameSite=Lax keeping the cookie
	// off a cross-site POST so the MAC cannot be checked at all.
	if origin := r.Header.Get("Origin"); origin != "" && origin != "null" &&
		!strings.EqualFold(strings.TrimRight(origin, "/"), strings.TrimRight(t.baseURL, "/")) {
		t.log.Warn("checkin: refusing a cross-origin post", "employee_id", id.EmployeeID())
		t.renderProblem(w, r, http.StatusForbidden, tapProblemBadURL)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, checkinMaxBody)
	if err := r.ParseForm(); err != nil {
		t.log.Info("checkin: unusable form body", "err", err)
		t.renderProblem(w, r, http.StatusBadRequest, tapProblemBadURL)
		return
	}

	tctx, err := t.contexts.parse(r.PostFormValue("ctx"), id.Session.ID)
	if err != nil {
		// A stale page is told to tap again — honest and actionable. A tampered or
		// foreign context gets the same page, because telling the two apart is for
		// us and not for whoever sent it (tapcontext.go).
		switch {
		case errors.Is(err, errTapContextExpired):
			t.log.Info("checkin: expired tap context", "employee_id", id.EmployeeID())
		default:
			t.log.Warn("checkin: unusable tap context", "employee_id", id.EmployeeID(), "err", err)
		}
		t.renderProblem(w, r, http.StatusBadRequest, tapProblemStale)
		return
	}

	occurredAt, ok := parseOccurredAt(r.PostFormValue(occurredAtField))
	if !ok {
		t.log.Info("checkin: unusable occurred_at", "employee_id", id.EmployeeID())
		t.renderProblem(w, r, http.StatusBadRequest, tapProblemBadURL)
		return
	}

	res, err := t.checkins.Record(r.Context(), checkin.Request{
		SessionTenantID: id.Session.TenantID,
		EmployeeID:      id.Session.EmployeeID,
		TagUID:          tctx.UID,
		Ctr:             tctx.Ctr,
		// SERVER-DERIVED, carried inside the MAC (hand-off N2): sun.Parse decided
		// this from whether the URL had a counter and a MAC, at page-build time. A
		// client that could say "qr" here would shed the NFC-only guardrails.
		Channel:      tap.Channel(tctx.Channel),
		CMACVerified: tctx.CMACVerified,
		TagTenantID:  tctx.TagTenantID,
		// The page's own mint time, authenticated inside the context. It is what
		// sys:tap-freshness measures, and it is READ FROM THE MAC rather than from
		// a form field for the obvious reason: a caller that could state its page's
		// age would state whatever kept it inside the window.
		PageIssuedAt: tctx.IssuedAt,
		SourceIP:     httpx.ClientIP(r),
		GPS:          parseFix(r.PostFormValue("lat"), r.PostFormValue("lng")),
		OccurredAt:   occurredAt,
	})
	if err != nil {
		t.renderCheckinFailure(w, r, id, err)
		return
	}

	switch res.Outcome {
	case checkin.OutcomeForeignTenant:
		// sys:tenant-mismatch. NOT the activation page: this person has a perfectly
		// good session, they are simply standing in front of somebody else's
		// plaque, and sending them to activate would be a nonsense instruction.
		// (tap.Decide maps both redirects to RedirectActivation; the domain tells
		// them apart by the deciding sid — the note M4-03 left for M5-05.)
		t.renderProblem(w, r, http.StatusForbidden, tapProblemForeignTenant)
	case checkin.OutcomeActivation:
		t.redirectToActivation(w, r)
	default:
		t.render(w, r, http.StatusOK, pages.Result(resultView(res)))
	}
}

// renderCheckinFailure answers the paths where NOTHING was recorded.
//
// EVERY ONE OF THEM SAYS "TAP AGAIN" AND LOGS. §4.6 does not permit a screen that
// implies a record exists when it does not, and §7 does not permit a swallowed
// error — so the two halves of every branch here are a truthful page and a log
// line. The person is never blamed and never told about our internals.
func (t *Tap) renderCheckinFailure(w http.ResponseWriter, r *http.Request, id httpx.Identity, err error) {
	switch {
	case errors.Is(err, checkin.ErrUnknownTag):
		// The signed context named a plaque that no longer resolves. The domain has
		// already logged it and left an audit row; there is no tag and no location,
		// so there is nothing to record against.
		t.renderProblem(w, r, http.StatusNotFound, tapProblemUnknownTag)
	case errors.Is(err, checkin.ErrInvalidRequest), errors.Is(err, checkin.ErrEnteredByRequired):
		// A request this handler built wrong — our bug, not the caller's.
		t.log.Error("checkin: refused an invalid request", "employee_id", id.EmployeeID(), "err", err)
		t.renderProblem(w, r, http.StatusInternalServerError, tapProblemServer)
	default:
		// The database was unreachable, the insert failed, the counter could not be
		// advanced. NOT recorded, said plainly, and loud in the log — this is the
		// case where an hour goes missing if we are quiet about it.
		t.log.Error("checkin: recording the tap failed; NOTHING was written",
			"employee_id", id.EmployeeID(), "err", err)
		t.renderProblem(w, r, http.StatusInternalServerError, tapProblemServer)
	}
}

// resultView maps a domain Result onto the screen's view model.
//
// IT IS THE WHOLE OF THE RESPONSE CONTRACT, and it is why no internal/store type
// can reach the wire: the view model has fields for a stamp, a sentence and a
// clock time, and nothing that could hold a database row, a policy version id or
// a tenant id. The mapping is explicit rather than a struct copy for exactly that
// reason.
//
// The wall clock is computed HERE, at the render boundary, from the tenant's zone
// (§6: UTC everywhere below, local only where a human reads it). An unknown or
// missing zone falls back to UTC and says so, rather than silently showing a time
// that is an hour out.
//
// BusinessType is copied for ONE purpose and is never printed: result.templ
// switches on it to pick which fixed brand sentence a good tap ends with (M5-06).
// It is a CATEGORY out of migration 00001's CHECK list, shared by every tenant of
// that kind, so copying it does not widen what this screen can disclose — the
// view model still has no field for an id, a token, a key or a coordinate.
func resultView(res checkin.Result) pages.ResultView {
	v := pages.ResultView{
		Verdict:      string(res.Decision.Verdict),
		Note:         res.Decision.Note,
		Venue:        res.LocationName,
		At:           localClock(res.OccurredAt, res.Timezone),
		Trust:        res.Decision.Trust,
		BusinessType: res.BusinessType,
	}
	if res.Decision.Type != nil {
		v.Direction = string(*res.Decision.Type)
	}
	v.Practice = res.Decision.Practice
	return v
}

// localClock renders an instant in the tenant's zone as HH:MM:SS.
func localClock(t time.Time, timezone string) string {
	if timezone != "" {
		if loc, err := time.LoadLocation(timezone); err == nil {
			return t.In(loc).Format("15:04:05")
		}
	}
	return t.UTC().Format("15:04:05") + " UTC"
}

// parseFix turns the two coordinate fields into a point, or nil.
//
// EVERY FAILURE IS "NO FIX", NEVER AN ERROR, and that is the §5 reading rather
// than leniency: GPS is the BACKUP proof of place, an absent fix is an ordinary
// state (rows 6 and 7 both handle it), and refusing the request over a malformed
// coordinate would turn a decision the engine can make into an hour somebody has
// to claim back. A partial pair is dropped whole — half a coordinate places
// nothing — and out-of-range values go the same way, which is also what keeps the
// numeric(9,6) columns from ever seeing something they cannot hold (00005 leaves
// the range check to this boundary on purpose).
func parseFix(latRaw, lngRaw string) *geo.Point {
	if strings.TrimSpace(latRaw) == "" || strings.TrimSpace(lngRaw) == "" {
		return nil
	}
	lat, err := strconv.ParseFloat(strings.TrimSpace(latRaw), 64)
	if err != nil || lat < -90 || lat > 90 || lat != lat {
		return nil
	}
	lng, err := strconv.ParseFloat(strings.TrimSpace(lngRaw), 64)
	if err != nil || lng < -180 || lng > 180 || lng != lng {
		return nil
	}
	return &geo.Point{Lat: lat, Lng: lng}
}

// parseOccurredAt reads the optional client-declared tap time.
//
// ABSENT MEANS "NOW" and is the normal case: nothing on the tap page sends this
// field, and the server clock is the honest answer for a tap happening in front
// of it. A PRESENT-BUT-MALFORMED value is REFUSED rather than quietly treated as
// absent (§7 forbids silent acceptance) — a client that meant to declare a time
// and got the format wrong must be told, not have its record silently stamped
// with a different one.
//
// 🔴 A WELL-FORMED VALUE IS NOT TRUSTED, IT IS BOUNDED. Nothing here checks
// whether the time is plausible: that is sys:occurred-at-bound's job, it is a
// GUARDRAIL, it cannot be switched off by any tenant, and it denies both a future
// stamp and one older than the configured tolerance. Doing the check here instead
// would put a red line in a handler where a later refactor could lose it — and
// would produce a REFUSAL where the guardrail produces a RECORDED reject (§4.6).
func parseOccurredAt(raw string) (*time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, false
	}
	// A POINTER, NOT A SENTINEL VALUE. Returning the zero time for "absent" made
	// 0001-01-01T00:00:00Z — a perfectly well-formed RFC 3339 value — indis-
	// tinguishable from silence, so that one declared time was silently replaced
	// by the server's clock while every neighbouring value was judged (measured:
	// 00:00:00Z accepted as `ok`, 00:00:01Z rejected by sys:occurred-at-bound).
	// Nil is not something a form field can contain.
	u := t.UTC()
	return &u, true
}
