package handler

// locationrefusal_db_test.go -- a REFUSED removal writes a real audit row (user
// decision, 2026-08-09).
//
// 🔴 WHY THIS NEEDS REAL POSTGRES WHEN locations_test.go ALREADY DRIVES THE ROUTER.
// The fake trail proves the handler BUILDS the right event; it cannot prove the event
// survives the trip. What only a database can answer: does the purpose-built detail
// struct marshal into jsonb and come back with its keys intact, does the row land in
// the SESSION's tenant under RLS, and is it visible to the owner it exists for. A
// mock agrees with all three unconditionally.
//
// 🔴 AND THE SHAPE UNDER TEST IS A WRITE ON A PATH THAT WRITES NOTHING ELSE. The
// removal does not happen -- that is the whole point -- so there is no surrounding
// transaction for the row to share a fate with, and audit.Record opens its own. That
// is exactly the case audit.Record's own comment describes: the caller who most needs
// a trail row is the one whose act did not happen.
//
// FIXTURES ARE NOT CLEANED UP: audit_log is append-only (00005) and tappa_app holds
// INSERT and SELECT only. Fresh random ids keep runs from colliding.

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// refusalFixture is one business with a real audit recorder behind it, and a panel
// session whose ROLE the test chooses.
type refusalFixture struct {
	data     *db.DB
	router   http.Handler
	venues   *fakeVenues
	plaques  *fakePlaques
	tenantID uuid.UUID
	adminID  uuid.UUID
}

func newRefusalFixture(t *testing.T, role string) *refusalFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the refusal trail test (real Postgres required)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	f := &refusalFixture{data: data, tenantID: uuid.New(), adminID: uuid.New()}
	// 🔴 ONE SESSION ID FOR THE WHOLE FIXTURE. A first draft minted a fresh uuid on
	// every verify call, so the confirmation was signed against one session and checked
	// against another and every owner path answered "confirm-required" — the binding
	// working exactly as designed, on a fixture that was lying to it.
	sessionID := uuid.New()
	// The tenant must exist: audit_log's RLS WITH CHECK refuses a row stamped with a
	// tenant this connection cannot see.
	if err := data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Refusal Fixture Ltd', $2, 'restaurant', 'single')`,
			f.tenantID, "VAT-"+f.tenantID.String())
		return e
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	// 🔴 THE TRAIL IS REAL AND EVERYTHING ELSE IS NOT, deliberately. What is under
	// test is the audit row; the venues side is a fake because the removal must NEVER
	// reach it — a test that needed a real one would be testing the wrong thing.
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	f.venues = twoVenues()
	f.plaques = &fakePlaques{}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: sessionID, TenantID: f.tenantID, AdminUserID: f.adminID,
			Role: role, FullName: "Refusal Fixture Admin",
		}, nil
	}}
	records := newFakeLedger()
	h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, f.venues, f.plaques, &fakeRecorder{}, newFakeRules(), newFakeScribe(), adminTestConfig(), discardLogger())
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	f.router = r
	return f
}

func (f *refusalFixture) browser(t *testing.T) *browser {
	t.Helper()
	b := newBrowser(t, f.router)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// trailRows reads the rows this business holds for one action, with their detail, IN
// THE TENANT'S OWN CONTEXT so RLS scopes the read exactly as production's does.
func (f *refusalFixture) trailRows(t *testing.T, action string) []struct {
	Target  string
	Actor   *uuid.UUID
	Detail  string
	TenetID uuid.UUID
} {
	t.Helper()
	type row = struct {
		Target  string
		Actor   *uuid.UUID
		Detail  string
		TenetID uuid.UUID
	}
	var out []row
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx,
			`SELECT target, actor_id, detail::text, tenant_id FROM audit_log
			  WHERE tenant_id = $1 AND action = $2 ORDER BY at`,
			f.tenantID, action)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var r row
			var target *string
			if e := rows.Scan(&target, &r.Actor, &r.Detail, &r.TenetID); e != nil {
				return e
			}
			if target != nil {
				r.Target = *target
			}
			out = append(out, r)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read the trail: %v", err)
	}
	return out
}

// detailKeys reads the jsonb keys of one trail row, in the tenant's own context.
func (f *refusalFixture) detailKeys(t *testing.T, action, target string) []string {
	t.Helper()
	var keys []string
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT array_agg(k ORDER BY k) FROM audit_log, jsonb_object_keys(detail) k
			  WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			f.tenantID, action, target).Scan(&keys)
	}); err != nil {
		t.Fatalf("read the detail keys: %v", err)
	}
	return keys
}

// TestRemovalRefusal_LeavesARealAuditRow.
//
// 🔴 THE ROW EXISTS SO AN OWNER CAN SEE WHAT THEIR MANAGER TRIED. That is the whole
// argument the user's decision turned on: a structured log line has no tenant
// boundary, no retention story and no reader outside the operator, so it cannot
// answer "has anybody been trying to delete my venues?".
func TestRemovalRefusal_LeavesARealAuditRow(t *testing.T) {
	f := newRefusalFixture(t, "manager")
	b := f.browser(t)
	venue := f.venues.firstVenueID()
	f.venues.refs = map[uuid.UUID]tenant.References{venue: {}}

	rec := b.do(http.MethodPost, venueDeleteHref, url.Values{"id": {venue.String()}})
	if loc := rec.Header().Get("Location"); loc != locationsHref+"?problem=not-permitted" {
		t.Fatalf("the refusal answered %d / %q", rec.Code, loc)
	}
	if got := f.venues.removals(); len(got) != 0 {
		t.Fatalf("the refused POST still removed something: %+v", got)
	}

	rows := f.trailRows(t, ActionLocationDeleteRefused)
	if len(rows) != 1 {
		t.Fatalf("the refused attempt left %d audit row(s), want 1", len(rows))
	}
	r := rows[0]

	// §4.5: the row is in the SESSION's business, not one the request named.
	if r.TenetID != f.tenantID {
		t.Errorf("the row landed in tenant %s, want the session's %s", r.TenetID, f.tenantID)
	}
	// The attempt is attributable — an unattributable refusal is worse than none.
	if r.Actor == nil || *r.Actor != f.adminID {
		t.Errorf("the row's actor is %v, want the admin who tried (%s)", r.Actor, f.adminID)
	}
	if r.Target != venue.String() {
		t.Errorf("the row targets %q, want the venue that was aimed at (%s)", r.Target, venue)
	}

	// 🔴 THE DETAIL SURVIVED THE jsonb ROUND TRIP WITH ITS KEYS INTACT. This is the
	// half only a database can answer, and X3's lesson is why the keys matter: an
	// absent key cannot be told apart from an empty value.
	detail := compactJSONHandler(r.Detail)
	for _, want := range []string{
		`"outcome":"refused"`,
		`"reason":"removing is reserved for an owner"`,
		`"what":"venue"`,
		`"role":"manager"`,
		`"required_role":"owner"`,
	} {
		if !strings.Contains(detail, want) {
			t.Errorf("the detail does not carry %s: %s", want, detail)
		}
	}

	// 🔴 §4.7 IS ASSERTED AS AN ALLOWED KEY SET, NOT AS A FORBIDDEN-WORD SCAN, AND THE
	// DIFFERENCE IS THE FINDING THAT PRODUCED IT. The first version listed seven words
	// — token, cookie, gps, lat, lng, password, hash — and an audit defeated it in one
	// edit: adding a `Note` field carrying the SESSION ID and the confirmation value
	// left the stored jsonb reading `"note":"session=9f1c… confirm="` and all three
	// tests GREEN, because a uuid hits none of those words and neither would a base64
	// token. A denylist can only refuse what somebody thought of.
	//
	// An ALLOWLIST inverts that: a new field is a failure until somebody adds it here
	// and says why. internal/domain/tenant/venue_db_test.go already reads keys this
	// way with jsonb_object_keys, so this is the same pattern rather than a second one.
	allowed := map[string]bool{
		"outcome": true, "reason": true, "what": true,
		"role": true, "required_role": true,
	}
	keys := f.detailKeys(t, ActionLocationDeleteRefused, venue.String())
	// ANTI-VACUITY: the key read really returned the payload. An empty set would make
	// the loop below pass over nothing at all.
	if len(keys) != len(allowed) {
		t.Errorf("the detail carries %d key(s) %v, want exactly the %d allowed",
			len(keys), keys, len(allowed))
	}
	for _, k := range keys {
		if !allowed[k] {
			t.Errorf("the refusal detail carries an unlisted key %q (%s). Every field on "+
				"this path is a §4.7 decision: add it to the allowlist above WITH a reason, "+
				"or do not record it.", k, detail)
		}
	}
}

// TestRemovalRefusal_ADepartmentGetsItsOwnAction. Two actions rather than one, so an
// investigator reading `action LIKE 'department.%'` sees the whole of what can be
// attempted against a department.
func TestRemovalRefusal_ADepartmentGetsItsOwnAction(t *testing.T) {
	f := newRefusalFixture(t, "manager")
	b := f.browser(t)
	dept := f.venues.firstDepartmentID()
	f.venues.refs = map[uuid.UUID]tenant.References{dept: {}}

	b.do(http.MethodPost, departmentDeleteHref, url.Values{"id": {dept.String()}})

	rows := f.trailRows(t, ActionDepartmentDeleteRefused)
	if len(rows) != 1 {
		t.Fatalf("a refused department removal left %d row(s), want 1", len(rows))
	}
	if !strings.Contains(compactJSONHandler(rows[0].Detail), `"what":"department"`) {
		t.Errorf("the detail does not say what was aimed at: %s", rows[0].Detail)
	}
	// AND IT IS NOT FILED UNDER THE VENUE ACTION.
	if n := len(f.trailRows(t, ActionLocationDeleteRefused)); n != 0 {
		t.Errorf("a department refusal wrote %d location.delete_refused row(s)", n)
	}
}

// TestRemovalRefusal_AnOwnersSuccessLeavesNoRefusal is the positive control. Without
// it, a handler that wrote a refusal row on EVERY removal would satisfy the tests
// above and quietly accuse every owner of something they were allowed to do.
func TestRemovalRefusal_AnOwnersSuccessLeavesNoRefusal(t *testing.T) {
	f := newRefusalFixture(t, "owner")
	b := f.browser(t)
	venue := f.venues.firstVenueID()
	f.venues.refs = map[uuid.UUID]tenant.References{venue: {}}

	armed := htmlOf(t, b.do(http.MethodGet,
		locationsHref+"?venue="+venue.String()+"&confirm=remove", nil))
	token := confirmTokenFrom(t, armed)
	rec := b.do(http.MethodPost, venueDeleteHref, url.Values{
		"id": {venue.String()}, confirmField: {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("an owner's removal answered %d, want 303", rec.Code)
	}
	if got := f.venues.removals(); len(got) != 1 {
		t.Fatalf("an owner's removal built %d command(s), want 1", len(got))
	}
	if n := len(f.trailRows(t, ActionLocationDeleteRefused)); n != 0 {
		t.Errorf("an owner's successful removal wrote %d refusal row(s); the trail would "+
			"accuse somebody of an attempt they were entitled to make", n)
	}
}

// compactJSONHandler removes the whitespace jsonb's text rendering inserts, so an
// assertion can name a key and its value as one token. Same reason
// internal/domain/tenant/venue_db_test.go carries its own copy: the two packages do
// not share test helpers.
func compactJSONHandler(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' && i > 0 && (s[i-1] == ':' || s[i-1] == ',') {
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}
