package handler

// accountsection_db_test.go -- the ACCOUNT section driven over HTTP against REAL
// Postgres, with the real domain writer behind it (M7-05, round 2).
//
// 🔴 WHY THIS EXISTS WHEN account_test.go ALREADY DRIVES THE ROUTER. Every test there
// runs against fakeAccounts, which cannot answer three of this section's claims:
//
//   - THE "ON FILE" BLOCK PRINTS THE ROW. A double returns whatever a test told it to,
//     so it agrees with a page reading the SUBMISSION just as happily as with one
//     reading the database. The round-1 defect lived exactly in that gap: on a refused
//     save the screen printed the typed name and the typed zone under the heading "On
//     file", and the fake could not tell. Here the values are read back from `tenants`
//     with SQL and compared against the bytes the browser got.
//   - A REFUSED SAVE'S AUDIT ROW SURVIVES THE TRIP. The fake trail proves the handler
//     BUILDS the event; only a database says whether the purpose-built detail struct
//     marshals into jsonb with its keys intact and lands in the SESSION's tenant under
//     RLS. locationrefusal_db_test.go makes the same argument for the other role gate.
//   - A REFUSED SAVE WRITES NO tenants ROW AT ALL. "saves == 0" against a double is a
//     statement about the double.
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

// accountDBFixture is one business, the REAL account writer and the REAL trail behind
// the router, with a panel session whose role the test chooses.
type accountDBFixture struct {
	data     *db.DB
	router   http.Handler
	tenantID uuid.UUID
	adminID  uuid.UUID
}

func newAccountDBFixture(t *testing.T, role string) *accountDBFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the account section's end-to-end tests (real Postgres required)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	f := &accountDBFixture{data: data, tenantID: uuid.New(), adminID: uuid.New()}
	sessionID := uuid.New()
	// The tenant must exist: it is the row this whole section reads, and audit_log's
	// RLS WITH CHECK refuses a row stamped with a tenant this connection cannot see.
	// The VAT columns are written here because migration 00017 grants INSERT (and only
	// INSERT) on them — the same door the sign-up wizard uses.
	if err := data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone,
			                      vat_verified, vat_checked_at)
			 VALUES ($1, 'Account Fixture Ltd', $2, 'restaurant', 'multi', 'Europe/Malta',
			         false, now())`,
			f.tenantID, "VAT-"+f.tenantID.String())
		return e
	}); err != nil {
		t.Fatalf("seed tenant: %v", err)
	}

	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	// 🔴 THE ACCOUNT SIDE IS REAL, WHICH IS THE POINT OF THE FILE. Everything else is a
	// double because nothing else is under test here.
	accounts, err := tenant.NewAccounts(data, trail, discardLogger())
	if err != nil {
		t.Fatalf("tenant.NewAccounts: %v", err)
	}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: sessionID, TenantID: f.tenantID, AdminUserID: f.adminID,
			Role: role, FullName: "Account Fixture Admin",
		}, nil
	}}
	records := newFakeLedger()
	h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{},
		newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeTexts(), accounts, nil,
		adminTestConfig(), discardLogger())
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	f.router = r
	return f
}

func (f *accountDBFixture) browser(t *testing.T) *browser {
	t.Helper()
	b := newBrowser(t, f.router)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// stored reads the three editable columns back, IN THE TENANT'S OWN CONTEXT so RLS
// scopes the read exactly as production's does.
func (f *accountDBFixture) stored(t *testing.T) (name, businessType, zone string) {
	t.Helper()
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT name, business_type, timezone FROM tenants WHERE id = $1`, f.tenantID).
			Scan(&name, &businessType, &zone)
	}); err != nil {
		t.Fatalf("read the stored row: %v", err)
	}
	return
}

// auditRows returns target, actor, tenant and detail for one action.
func (f *accountDBFixture) auditRows(t *testing.T, action string) []accountTrailRow {
	t.Helper()
	var out []accountTrailRow
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx,
			`SELECT target, actor_id, tenant_id, detail::text FROM audit_log
			  WHERE tenant_id = $1 AND action = $2 ORDER BY at`, f.tenantID, action)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var r accountTrailRow
			var target *string
			if e := rows.Scan(&target, &r.Actor, &r.TenantID, &r.Detail); e != nil {
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

type accountTrailRow struct {
	Target   string
	Actor    *uuid.UUID
	TenantID uuid.UUID
	Detail   string
}

// TestAccountSectionDB_ARefusedSaveShowsTheRowAndWritesNothing is the round-1 blocking
// defect measured where the fake could not see it.
//
// 🔴 THE VALUES THE PAGE PRINTS ARE COMPARED AGAINST THE COLUMNS, not against a
// constant. The screen's own heading is "On file", so the only thing that makes that
// heading true is agreement with `tenants` — and the round-1 bug is precisely a page
// that agreed with the SUBMISSION instead.
func TestAccountSectionDB_ARefusedSaveShowsTheRowAndWritesNothing(t *testing.T) {
	f := newAccountDBFixture(t, accountTestOwnerRole)
	beforeName, beforeType, beforeZone := f.stored(t)

	rec := f.browser(t).do(http.MethodPost, accountHref, url.Values{
		"name":          {"NOT THE STORED NAME Ltd"},
		"business_type": {"production"},
		"timezone":      {"Europe/Maltaa"}, // refused: not a zone this build can load
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("a refused save answered %d, want 200 with the form again", rec.Code)
	}
	body := htmlOf(t, rec)

	// NOTHING WAS WRITTEN — measured on the COLUMNS.
	afterName, afterType, afterZone := f.stored(t)
	if afterName != beforeName || afterType != beforeType || afterZone != beforeZone {
		t.Fatalf("a refused save changed the row: (%q,%q,%q) -> (%q,%q,%q)",
			beforeName, beforeType, beforeZone, afterName, afterType, afterZone)
	}

	// AND THE PAGE SAYS SO. The stored values must be on it; the typed ones must not be
	// in the facts docket.
	for _, want := range []string{beforeName, beforeZone} {
		if !strings.Contains(body, htmlText(want)) {
			t.Errorf("the value %q is in the tenants row and nowhere on the page.\n"+
				"The docket is headed \"On file\"; a customer who cannot see what is on file "+
				"cannot see that their edit did not take.", want)
		}
	}
	for _, forbidden := range []string{
		`tracking-tight text-ink">NOT THE STORED NAME Ltd<`,
		`font-mono text-sm text-ink">Europe/Maltaa<`,
	} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the facts docket printed what was TYPED (%s) while the row still says "+
				"otherwise", forbidden)
		}
	}
	// THE TYPING SURVIVES, in the boxes.
	if !strings.Contains(body, htmlText("NOT THE STORED NAME Ltd")) {
		t.Error("the owner's typing was thrown away")
	}
	// AND THE GAP IS NAMED.
	if n := strings.Count(body, htmlText("Not saved — on file this is still")); n != 3 {
		t.Errorf("the page names %d unsaved field(s), want 3", n)
	}
	// NO TRAIL ROW EITHER: an owner's rejected form is not an event.
	if rows := f.auditRows(t, tenant.ActionAccountUpdated); len(rows) != 0 {
		t.Errorf("a refused save wrote %d %s row(s)", len(rows), tenant.ActionAccountUpdated)
	}
}

// TestAccountSectionDB_ASavedChangeReachesTheColumnsAndTheTrail is the success path end
// to end: the browser posts, the columns move, the trail records it inside the same
// transaction, and the page the owner lands on is read back from the row.
func TestAccountSectionDB_ASavedChangeReachesTheColumnsAndTheTrail(t *testing.T) {
	f := newAccountDBFixture(t, accountTestOwnerRole)
	b := f.browser(t)

	rec := b.do(http.MethodPost, accountHref, url.Values{
		"name":          {"Rusty Bar Ltd"},
		"business_type": {"bar"},
		"timezone":      {"Europe/Rome"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a valid save answered %d, want 303\nbody: %s", rec.Code, htmlOf(t, rec))
	}
	name, businessType, zone := f.stored(t)
	if name != "Rusty Bar Ltd" || businessType != "bar" || zone != "Europe/Rome" {
		t.Fatalf("the columns read back as (%q,%q,%q)", name, businessType, zone)
	}

	rows := f.auditRows(t, tenant.ActionAccountUpdated)
	if len(rows) != 1 {
		t.Fatalf("the trail holds %d %s row(s), want 1", len(rows), tenant.ActionAccountUpdated)
	}
	if rows[0].TenantID != f.tenantID {
		t.Errorf("the row names tenant %s, want the session's %s (§4.5)", rows[0].TenantID, f.tenantID)
	}
	if rows[0].Actor == nil || *rows[0].Actor != f.adminID {
		t.Errorf("the row names actor %v, want the session's %s", rows[0].Actor, f.adminID)
	}
	if rows[0].Target != f.tenantID.String() {
		t.Errorf("the row targets %q, want the business itself", rows[0].Target)
	}
	// THE DETAIL SURVIVED THE TRIP INTO jsonb WITH ITS KEYS INTACT.
	for _, want := range []string{`"changed"`, `"timezone_before"`, `"timezone_after"`, "Europe/Rome"} {
		if !strings.Contains(rows[0].Detail, want) {
			t.Errorf("the trail detail lost %s\ndetail: %s", want, rows[0].Detail)
		}
	}

	// AND THE PAGE AFTER THE REDIRECT IS READ FROM THE ROW.
	page := b.do(http.MethodGet, accountHref, nil)
	if page.Code != http.StatusOK {
		t.Fatalf("the section answered %d after a save", page.Code)
	}
	body := htmlOf(t, page)
	for _, want := range []string{"Rusty Bar Ltd", "Europe/Rome"} {
		if !strings.Contains(body, htmlText(want)) {
			t.Errorf("the page after a save does not carry the stored value %q", want)
		}
	}
}

// TestAccountSectionDB_AManagersRefusedSaveLeavesARealAuditRow is the role gate measured
// against the trail it actually writes to.
//
// 🔴 THE FAKE PROVED THE EVENT WAS BUILT; THIS PROVES IT SURVIVES. What only a database
// answers: does refusedAccountDetail marshal into jsonb with its keys intact, does the
// row land in the SESSION's tenant under RLS, and is it there for the owner it exists
// for. And on the same request: nothing at all reached the tenants row.
func TestAccountSectionDB_AManagersRefusedSaveLeavesARealAuditRow(t *testing.T) {
	f := newAccountDBFixture(t, accountTestManagerRole)
	beforeName, beforeType, beforeZone := f.stored(t)

	rec := f.browser(t).do(http.MethodPost, accountHref, url.Values{
		"name":          {"Manager Took Over Ltd"},
		"business_type": {"cafe"},
		"timezone":      {"Europe/Istanbul"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a manager's POST answered %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); !strings.Contains(got, "problem=not-permitted") {
		t.Errorf("a refused manager is sent to %q, want the not-permitted word", got)
	}
	if name, businessType, zone := f.stored(t); name != beforeName || businessType != beforeType || zone != beforeZone {
		t.Fatalf("a manager's refused save changed the row: (%q,%q,%q)", name, businessType, zone)
	}

	rows := f.auditRows(t, ActionAccountUpdateRefused)
	if len(rows) != 1 {
		t.Fatalf("the trail holds %d %s row(s), want exactly 1", len(rows), ActionAccountUpdateRefused)
	}
	if rows[0].TenantID != f.tenantID {
		t.Errorf("the refusal row names tenant %s, want the session's %s (§4.5)", rows[0].TenantID, f.tenantID)
	}
	if rows[0].Actor == nil || *rows[0].Actor != f.adminID {
		t.Errorf("the refusal row names actor %v, want the session's %s", rows[0].Actor, f.adminID)
	}
	for _, want := range []string{`"outcome"`, `"reason"`, `"role"`, `"required_role"`} {
		if !strings.Contains(rows[0].Detail, want) {
			t.Errorf("the refusal detail lost the key %s\ndetail: %s", want, rows[0].Detail)
		}
	}
	// 🔴 AND NOTHING THAT WAS POSTED IS IN THE ROW. Anybody who can POST this route
	// could otherwise append text of their choosing to a tenant-scoped, append-only
	// table nobody can clean up.
	for _, forbidden := range []string{"Manager Took Over Ltd", "Europe/Istanbul", "cafe"} {
		if strings.Contains(rows[0].Detail, forbidden) {
			t.Errorf("the refusal row carries %q from the posted form\ndetail: %s",
				forbidden, rows[0].Detail)
		}
	}
	// The success action must not appear at all — the two are different events and a
	// prefix match on one must not find the other.
	if rows := f.auditRows(t, tenant.ActionAccountUpdated); len(rows) != 0 {
		t.Errorf("a refused manager produced %d success row(s)", len(rows))
	}
}

// TestAccountSectionDB_TheVATVerdictReachesTheScreen closes the M7-05 card's 2026-08-13
// addition end to end: a verdict written at registration, read back by a panel screen.
//
// THE FIXTURE STORES vat_verified = false, which is the state the card names as the one
// a customer could previously never see again.
func TestAccountSectionDB_TheVATVerdictReachesTheScreen(t *testing.T) {
	f := newAccountDBFixture(t, accountTestManagerRole)
	rec := f.browser(t).do(http.MethodGet, accountHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", accountHref, rec.Code)
	}
	body := htmlOf(t, rec)
	if !strings.Contains(body, htmlText("Not found")) {
		t.Error("a business whose VAT number the register refused is not told so on the one " +
			"screen that reads the column")
	}
	if !strings.Contains(body, htmlText("did not recognise this number")) {
		t.Error("the refused state's sentence is missing")
	}
	if strings.Contains(body, htmlText("Confirmed")) {
		t.Error("a refused number is also shown as confirmed")
	}
}
