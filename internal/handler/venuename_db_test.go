package handler

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

// TestVenueSave_ANameOnFileStaysEditableAndANewHiddenOneIsASentence (API-5 review, second round)
// is the panel's half of the name rule, through the REAL handler and the REAL venue domain:
//
//	a venue and a department already NAMED with a refused character (U+202E -- written before the
//	rule existed) are saved with that SAME name and new address ranges / a new shift: both saved,
//	ranges and shift stored -- the form resends the name on every save, and the rule must not lock
//	the other fields
//	the same venue renamed to a NEW name that still carries U+202E: the form comes back with the
//	sentence, nothing changes
//	a new venue named with the chef emoji (a ZWJ sequence) and one with the Scotland flag (tag
//	characters): both saved, the names stored as typed
//
// MUTANT (measured, API-5 report): the "unchanged name is exempt" arm removed from
// refuseHiddenName -> the first save comes back with the sentence, red.
func TestVenueSave_ANameOnFileStaysEditableAndANewHiddenOneIsASentence(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set (real Postgres required)")
	}
	ctx := context.Background()
	data, err := db.New(ctx, &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(data.Close)
	tenantID, adminID := uuid.New(), uuid.New()
	const onFile = "Rusty\u202eBar"
	var venueID, deptID uuid.UUID
	if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'Name Rule Ltd', $2, 'restaurant', 'multi')`,
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		if e := tx.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, $2) RETURNING id`, tenantID, onFile).Scan(&venueID); e != nil {
			return e
		}
		return tx.QueryRow(ctx, `INSERT INTO departments (tenant_id, location_id, name) VALUES ($1, $2, $3) RETURNING id`, tenantID, venueID, onFile).Scan(&deptID)
	}); err != nil {
		t.Fatal(err)
	}
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := tenant.NewVenues(data, trail, discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{SessionID: uuid.New(), TenantID: tenantID, AdminUserID: adminID, Role: "owner", FullName: "Owner"}, nil
	}}
	records := newFakeLedger()
	h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, venues, &fakePlaques{},
		&fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil,
		&fakeNotices{}, adminTestConfig(), discardLogger())
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	read := func(q string, args ...any) (n int, name string) {
		t.Helper()
		if err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, q, args...).Scan(&n, &name)
		}); err != nil {
			t.Fatal(err)
		}
		return n, name
	}
	saved := func(rec interface{ Header() http.Header }) bool {
		return strings.Contains(rec.Header().Get("Location"), "done=")
	}

	rec := b.do(http.MethodPost, venueSaveHref, url.Values{"id": {venueID.String()}, "name": {onFile}, "static_ips": {"203.0.113.0/24"}})
	if !saved(rec) {
		t.Fatalf("the venue's own name resent with new ranges: %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if n, name := read(`SELECT cardinality(static_ips), name FROM locations WHERE tenant_id = $1 AND id = $2`, tenantID, venueID); n != 1 || name != onFile {
		t.Fatalf("after the save: %d range(s), name %q", n, name)
	}
	rec = b.do(http.MethodPost, departmentSaveHref, url.Values{"id": {deptID.String()}, "location_id": {venueID.String()}, "name": {onFile},
		"shift_start": {"09:00"}, "shift_end": {"17:00"}})
	if !saved(rec) {
		t.Fatalf("the department's own name resent with a shift: %d %q %s", rec.Code, rec.Header().Get("Location"), rec.Body.String())
	}
	if n, _ := read(`SELECT count(*)::int, '' FROM departments WHERE tenant_id = $1 AND id = $2 AND shift_start IS NOT NULL`, tenantID, deptID); n != 1 {
		t.Fatal("the department's shift was not stored")
	}

	rec = b.do(http.MethodPost, venueSaveHref, url.Values{"id": {venueID.String()}, "name": {"Rusty\u202eBaz"}, "static_ips": {"203.0.113.0/24"}})
	if saved(rec) || !strings.Contains(rec.Body.String(), "direction override") {
		t.Fatalf("a NEW name with U+202E: %d %q, want the form with the sentence", rec.Code, rec.Header().Get("Location"))
	}
	if _, name := read(`SELECT 0, name FROM locations WHERE tenant_id = $1 AND id = $2`, tenantID, venueID); name != onFile {
		t.Fatalf("a refused rename changed the name to %q", name)
	}

	for _, name := range []string{"Kitchen \U0001F468\u200d\U0001F373", "Glasgow \U0001F3F4\U000E0067\U000E0062\U000E0073\U000E0063\U000E0074\U000E007F"} {
		rec = b.do(http.MethodPost, venueSaveHref, url.Values{"name": {name}})
		if !saved(rec) {
			t.Fatalf("a new venue named %q: %d %s", name, rec.Code, rec.Body.String())
		}
		if n, _ := read(`SELECT count(*)::int, '' FROM locations WHERE tenant_id = $1 AND name = $2`, tenantID, name); n != 1 {
			t.Fatalf("%q was not stored as typed", name)
		}
	}
}
