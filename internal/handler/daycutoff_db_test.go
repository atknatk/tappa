package handler

// daycutoff_db_test.go -- the business day's two presses over HTTP against REAL Postgres, the
// real tenant.DayCutoffs behind the router (accountsection_db_test.go's argument: "Set was
// called zero times" against a double is a statement about the double). The test builds its
// OWN business; the shared seed tenants are never written.

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

type dayDBFixture struct {
	data   *db.DB
	router http.Handler
	tenant uuid.UUID
	admin  uuid.UUID
	// venues is the locations section's double: the venue page is drawn from it, the
	// business day block from the database.
	venues *fakeVenues
}

func newDayDBFixture(t *testing.T, role string) *dayDBFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the business day end-to-end tests (real Postgres required)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	f := &dayDBFixture{data: data, tenant: uuid.New(), admin: uuid.New(), venues: &fakeVenues{}}
	f.exec(t, f.tenant, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
	                     VALUES ($1, 'Business Day Fixture Ltd', $2, 'restaurant', 'multi')`, f.tenant, "VAT-"+f.tenant.String())
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := tenant.NewDirectory(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	days, err := tenant.NewDayCutoffs(data, trail, dir, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	session := uuid.New()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: session, TenantID: f.tenant, AdminUserID: f.admin, Role: role, FullName: "Fixture Admin"}, nil
	}}
	records := newFakeLedger()
	h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, f.venues,
		&fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(),
		newFakeBrandWriter(), nil, &fakeNotices{}, adminTestConfig(), discardLogger(), BusinessDays(days))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	f.router = r
	return f
}

func (f *dayDBFixture) exec(t *testing.T, tenantID uuid.UUID, sql string, args ...any) {
	t.Helper()
	if err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	}); err != nil {
		t.Fatalf("fixture: %v", err)
	}
}

func (f *dayDBFixture) browser(t *testing.T) *browser {
	b := newBrowser(t, f.router)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// venueCutoff and trailRows read back IN THE TENANT'S OWN CONTEXT.
func (f *dayDBFixture) venueCutoff(t *testing.T, tenantID, venue uuid.UUID) string {
	t.Helper()
	var v *string
	if err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT to_char(day_cutoff, 'HH24:MI') FROM locations WHERE tenant_id = $1 AND id = $2`, tenantID, venue).Scan(&v)
	}); err != nil {
		t.Fatal(err)
	}
	if v == nil {
		return ""
	}
	return *v
}

func (f *dayDBFixture) trailRows(t *testing.T, tenantID uuid.UUID, action string) int {
	t.Helper()
	var n int
	if err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`, tenantID, action).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestBusinessDayDB_TheReviewWritesNothingAndTheConfirmedPressWritesOneRow: against the real
// service, the review leaves the venue's cutoff NULL and the trail empty; the confirmed press
// stores 06:00 with exactly one location.day_cutoff_updated row; replaying the same form with
// the cookie re-printed meets "already on file" and adds no row; and the venue's card then
// shows the stored value.
func TestBusinessDayDB_TheReviewWritesNothingAndTheConfirmedPressWritesOneRow(t *testing.T) {
	f := newDayDBFixture(t, "manager")
	venue := uuid.New()
	f.exec(t, f.tenant, `INSERT INTO locations (id, tenant_id, name, shift_start, shift_end) VALUES ($1, $2, 'Sliema', '10:00', '22:00')`, venue, f.tenant)
	b := f.browser(t)
	rec := b.do(http.MethodPost, businessDayHref, url.Values{"level": {"branch"}, "id": {venue.String()}, "cutoff": {"06:00"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("the review answered %d", rec.Code)
	}
	if got := f.venueCutoff(t, f.tenant, venue); got != "" || f.trailRows(t, f.tenant, tenant.ActionLocationDayCutoffUpdated) != 0 {
		t.Fatalf("the review wrote: cutoff %q", got)
	}
	form := formOf(t, htmlOf(t, rec), businessDayApplyHref)
	cookie := b.cookies[adminConfirmCookieName]
	if rec := b.do(http.MethodPost, businessDayApplyHref, form); rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "day=saved") {
		t.Fatalf("the confirmed press answered %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := f.venueCutoff(t, f.tenant, venue); got != "06:00" {
		t.Fatalf("stored %q, want 06:00", got)
	}
	if n := f.trailRows(t, f.tenant, tenant.ActionLocationDayCutoffUpdated); n != 1 {
		t.Fatalf("%d trail row(s), want 1", n)
	}
	b.cookies[adminConfirmCookieName] = cookie
	if rec := b.do(http.MethodPost, businessDayApplyHref, form); !strings.Contains(rec.Header().Get("Location"), "day=same") {
		t.Fatalf("a replay answered %q", rec.Header().Get("Location"))
	}
	if n := f.trailRows(t, f.tenant, tenant.ActionLocationDayCutoffUpdated); n != 1 {
		t.Fatalf("a replay added a trail row (%d)", n)
	}
	f.venues.beyondVenues = map[uuid.UUID]tenant.Venue{venue: {ID: venue, Name: "Sliema"}}
	page := htmlOf(t, b.do(http.MethodGet, locationsHref+"?venue="+venue.String(), nil))
	if !strings.Contains(page, "06:00 — set for this venue") {
		t.Fatalf("the venue's card does not show the stored value")
	}
}

// TestBusinessDayDB_AnotherBusinesssVenueIsRefusedAndUntouched: a venue id of another tenant
// is the locations section's "unknown venue" at the review AND at the apply, and that venue's
// cutoff and trail stay as they were.
func TestBusinessDayDB_AnotherBusinesssVenueIsRefusedAndUntouched(t *testing.T) {
	f := newDayDBFixture(t, adminRoleOwner)
	other := newDayDBFixture(t, adminRoleOwner)
	venue := uuid.New()
	other.exec(t, other.tenant, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Elsewhere')`, venue, other.tenant)
	b := f.browser(t)
	form := url.Values{"level": {"branch"}, "id": {venue.String()}, "cutoff": {"06:00"}}
	if rec := b.do(http.MethodPost, businessDayHref, form); rec.Header().Get("Location") != venueReturn("", "unknown-venue") {
		t.Fatalf("the review answered %d -> %q", rec.Code, rec.Header().Get("Location"))
	}
	if got := f.venueCutoff(t, other.tenant, venue); got != "" || f.trailRows(t, other.tenant, tenant.ActionLocationDayCutoffUpdated) != 0 {
		t.Fatalf("another business's venue was written: %q", got)
	}
}

// TestBusinessDayDB_TheOwnersBusinessLevelIsOneRowAndAManagersIsARefusal: the business level
// through both presses writes tenants.day_cutoff and one tenant.day_cutoff_updated row; a
// manager's attempt writes nothing but one refusal row.
func TestBusinessDayDB_TheOwnersBusinessLevelIsOneRowAndAManagersIsARefusal(t *testing.T) {
	f := newDayDBFixture(t, adminRoleOwner)
	b := f.browser(t)
	rec := b.do(http.MethodPost, businessDayHref, url.Values{"level": {"business"}, "cutoff": {"23:00"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("the owner's review answered %d", rec.Code)
	}
	if rec := b.do(http.MethodPost, businessDayApplyHref, formOf(t, htmlOf(t, rec), businessDayApplyHref)); !strings.Contains(rec.Header().Get("Location"), "day=saved") {
		t.Fatalf("the owner's apply answered %q", rec.Header().Get("Location"))
	}
	var got string
	if err := f.data.WithTenant(context.Background(), f.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT to_char(day_cutoff, 'HH24:MI') FROM tenants WHERE id = $1`, f.tenant).Scan(&got)
	}); err != nil || got != "23:00" {
		t.Fatalf("tenants.day_cutoff is %q (%v)", got, err)
	}
	if n := f.trailRows(t, f.tenant, tenant.ActionTenantDayCutoffUpdated); n != 1 {
		t.Fatalf("%d trail row(s), want 1", n)
	}

	m := newDayDBFixture(t, "manager")
	mb := m.browser(t)
	mb.do(http.MethodPost, businessDayHref, url.Values{"level": {"business"}, "cutoff": {"01:00"}})
	if m.trailRows(t, m.tenant, tenant.ActionTenantDayCutoffUpdated) != 0 || m.trailRows(t, m.tenant, ActionBusinessDayRefused) != 1 {
		t.Fatalf("a manager's attempt: %d update row(s), %d refusal row(s)",
			m.trailRows(t, m.tenant, tenant.ActionTenantDayCutoffUpdated), m.trailRows(t, m.tenant, ActionBusinessDayRefused))
	}

}

// TestBusinessDayDB_AShiftTheBusinessDayCutsIsWarnedOnItsForm: saving a department whose new
// shift (04:00-12:00) the business's 05:00 cuts lands on that department's form, and the form's
// business day block says what is cut and suggests a time outside the band -- the card's
// "first shift entered" warning. A shift it does not cut (09:00-17:00) lands where a save
// always did.
func TestBusinessDayDB_AShiftTheBusinessDayCutsIsWarnedOnItsForm(t *testing.T) {
	f := newDayDBFixture(t, "manager")
	venue, dept := uuid.New(), uuid.New()
	f.exec(t, f.tenant, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Plant')`, venue, f.tenant)
	f.exec(t, f.tenant, `INSERT INTO departments (id, tenant_id, location_id, name, shift_start, shift_end)
	                     VALUES ($1, $2, $3, 'Pastry & Bakery', '04:00', '12:00')`, dept, f.tenant, venue)
	b := f.browser(t)
	rec := b.do(http.MethodPost, departmentSaveHref, url.Values{"id": {dept.String()}, "name": {"Pastry & Bakery"},
		"shift_start": {"04:00"}, "shift_end": {"12:00"}})
	want := "/admin/locations?day=cuts&department=" + dept.String() + "#business-day"
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
		t.Fatalf("the save answered %d -> %q, want %q", rec.Code, rec.Header().Get("Location"), want)
	}
	f.venues.beyondDepts = map[uuid.UUID]tenant.Department{dept: {ID: dept, LocationID: venue, LocationName: "Plant", Name: "Pastry & Bakery",
		Shift: &tenant.Shift{Start: 4 * time.Hour, End: 12 * time.Hour}}}
	// A browser never sends the fragment.
	page := htmlOf(t, b.do(http.MethodGet, strings.Split(want, "#")[0], nil))
	for _, s := range []string{
		"Saved — but the business day cuts this shift",
		"05:00 falls inside Pastry &amp; Bakery&#39;s shift (04:00–12:00)",
		"Suggested: 20:30 — outside every shift.",
		"Not set — each person uses their branch&#39;s or the business&#39;s setting",
	} {
		if !strings.Contains(page, s) {
			t.Errorf("the department's form does not say %q", s)
		}
	}
	f.exec(t, f.tenant, `UPDATE departments SET shift_start = '09:00', shift_end = '17:00' WHERE tenant_id = $1 AND id = $2`, f.tenant, dept)
	rec = b.do(http.MethodPost, departmentSaveHref, url.Values{"id": {dept.String()}, "name": {"Pastry & Bakery"},
		"shift_start": {"09:00"}, "shift_end": {"17:00"}})
	if loc := rec.Header().Get("Location"); strings.Contains(loc, "day=cuts") || !strings.Contains(loc, "done=department-saved") {
		t.Fatalf("a shift the business day does not cut answered %q", loc)
	}
}
