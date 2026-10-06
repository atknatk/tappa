package handler

// brandupload_db_test.go -- the brand editor's three routes driven over HTTP against
// REAL Postgres (M10 WL-7): the real writer (tenant.Brands), the real trail
// (audit.Recorder), the real chrome reader (tenant.BrandReader) and the real decode
// gate behind a real server. What a double cannot answer: that the bytes stored are the
// re-encoder's, that a manager's POST leaves the row exactly as it was while its refusal
// row lands in the business's trail under RLS, and that concurrent writes leave one of
// their own states.
//
// FIXTURES ARE NOT CLEANED UP: tappa_app holds no DELETE on tenant_branding or
// audit_log. Fresh random ids keep runs from colliding.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/test/fixtures"
)

// brandDBFixture is one business with an owner and a manager, and the panel wired with
// the real brand writer, reader and trail.
type brandDBFixture struct {
	data                       *db.DB
	tenantID, owner, manager   uuid.UUID
	ownerServer, managerServer *httptest.Server
	ownerPanel                 *AdminAuth
}

func newBrandDBFixture(t *testing.T) *brandDBFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping the brand editor's end-to-end tests (real Postgres required)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	f := &brandDBFixture{data: data, tenantID: uuid.New(), owner: uuid.New(), manager: uuid.New()}
	err = data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Brand Editor Fixture Ltd', $2, 'bar', 'single')`,
			f.tenantID, "VAT-"+f.tenantID.String()); e != nil {
			return e
		}
		for id, role := range map[uuid.UUID]string{f.owner: "owner", f.manager: "manager"} {
			if _, e := tx.Exec(ctx,
				`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
				 VALUES ($1, $2, 'Brand Editor Admin', $3, $4, $5, 'active')`,
				id, f.tenantID, "wl7-"+uuid.NewString()+"@iso.example", fixtures.UnusablePasswordHash, role); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	writer, err := tenant.NewBrands(data, trail, discardLogger())
	if err != nil {
		t.Fatalf("tenant.NewBrands: %v", err)
	}
	reader, err := tenant.NewBrandReader(data)
	if err != nil {
		t.Fatalf("tenant.NewBrandReader: %v", err)
	}
	panel := func(role string, admin uuid.UUID) (*AdminAuth, *httptest.Server) {
		admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
			return adminauth.Resolved{SessionID: uuid.NewSHA1(uuid.Nil, admin[:]), TenantID: f.tenantID,
				AdminUserID: admin, Role: role, FullName: "Brand Editor Admin"}, nil
		}}
		records := newFakeLedger()
		h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
			&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
			newFakeAccount(), reader, writer, nil, adminTestConfig(), discardLogger())
		if err != nil {
			t.Fatalf("NewAdminAuth: %v", err)
		}
		r := chi.NewRouter()
		h.Mount(r)
		s := httptest.NewServer(r)
		t.Cleanup(s.Close)
		return h, s
	}
	f.ownerPanel, f.ownerServer = panel("owner", f.owner)
	_, f.managerServer = panel("manager", f.manager)
	return f
}

// brandRow is the business's tenant_branding row as the database holds it.
type brandRow struct {
	exists           bool
	accent, sha, upd string
	bytes            int
	updatedBy        string
}

func (f *brandDBFixture) row(t *testing.T) brandRow {
	t.Helper()
	var r brandRow
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var accent, sha *string
		var n *int
		var upd time.Time
		var by uuid.UUID
		err := tx.QueryRow(ctx, `SELECT accent, logo_sha256, octet_length(logo), updated_at, updated_by
			FROM tenant_branding WHERE tenant_id = $1`, f.tenantID).Scan(&accent, &sha, &n, &upd, &by)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		r.exists = true
		if accent != nil {
			r.accent = *accent
		}
		if sha != nil {
			r.sha = *sha
		}
		if n != nil {
			r.bytes = *n
		}
		r.upd = upd.UTC().Format(time.RFC3339Nano)
		r.updatedBy = by.String()
		return nil
	})
	if err != nil {
		t.Fatalf("reading the brand row: %v", err)
	}
	return r
}

// trail is the business's rows of one action, detail as a map, oldest first.
func (f *brandDBFixture) trail(t *testing.T, action string) []map[string]any {
	t.Helper()
	var out []map[string]any
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT detail::text FROM audit_log
			WHERE tenant_id = $1 AND action = $2 ORDER BY at, id`, f.tenantID, action)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			m := map[string]any{}
			if err := json.Unmarshal([]byte(s), &m); err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("reading the trail: %v", err)
	}
	return out
}

// post sends form (or, with a content type, body) to route on s as a same-origin browser.
func dbPost(t *testing.T, s *httptest.Server, route string, body []byte, contentType string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, s.URL+route, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Origin", testBaseURL)
	req.AddCookie(panelCookie())
	res, err := noRedirects().Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", route, err)
	}
	_, _ = io.Copy(io.Discard, res.Body)
	res.Body.Close()
	return res
}

func dbForm(t *testing.T, s *httptest.Server, route string, form url.Values) *http.Response {
	t.Helper()
	return dbPost(t, s, route, []byte(form.Encode()), "application/x-www-form-urlencoded")
}

// TestBrandRoutesDB_AnOwnersBrandIsStoredAndAManagersChangesNothing, over real HTTP and
// real Postgres:
//   - an owner's PNG is stored as the RE-ENCODER's bytes: the row's digest is the digest
//     of brand.LogoGate.Normalize's output for the same file, not of the file, and one
//     tenant.brand_updated row names the logo; the owner's Account page then draws the
//     preview from /admin/brand/logo/<that digest> under a policy naming img-src;
//   - the owner's accent is stored as DA291C;
//   - a manager's upload, accent and reset are each answered not-permitted, the row is
//     unchanged to the microsecond of updated_at, and three tenant.brand_update_refused
//     rows land in the business's trail, each with exactly the five keys;
//   - past ten upload attempts the owner's eleventh is answered 429, with one budget
//     refusal row and the row unchanged;
//   - the owner's two resets clear the accent, then the logo.
func TestBrandRoutesDB_AnOwnersBrandIsStoredAndAManagersChangesNothing(t *testing.T) {
	f := newBrandDBFixture(t)
	gate, err := brand.NewLogoGate(1)
	if err != nil {
		t.Fatal(err)
	}
	file := inkLogo(t)
	want, err := gate.Normalize(context.Background(), bytes.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(want.Data, file) {
		t.Fatal("PREMISE: the re-encoded bytes equal the file; the digest check would not tell them apart")
	}
	body, ct := oneLogo(t, file)
	if got := outcomeOf(dbPost(t, f.ownerServer, brandLogoHref, body, ct)); got != "logo-saved" {
		t.Fatalf("owner upload: %q, want logo-saved", got)
	}
	r := f.row(t)
	if r.sha != want.SHA256 || r.bytes != len(want.Data) || r.updatedBy != f.owner.String() {
		t.Fatalf("stored %+v, want the re-encoder's digest %s (%d bytes) by the owner", r, want.SHA256, len(want.Data))
	}
	if rows := f.trail(t, tenant.ActionBrandUpdated); len(rows) != 1 || rows[0]["field"] != "logo" || rows[0]["after"] != want.SHA256 {
		t.Fatalf("trail %v, want one logo row whose after is the digest", rows)
	}
	req, _ := http.NewRequest(http.MethodGet, f.ownerServer.URL+accountHref, nil)
	req.AddCookie(panelCookie())
	res, err := noRedirects().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(previewOf(t, string(page)), `src="/admin/brand/logo/`+want.SHA256+`"`) ||
		!strings.Contains(res.Header.Get("Content-Security-Policy"), "img-src 'self'") {
		t.Errorf("the owner's preview does not draw the stored logo from the panel's route under img-src")
	}
	if got := outcomeOf(dbForm(t, f.ownerServer, brandAccentHref, url.Values{"accent": {"#da291c"}})); got != "accent-saved" {
		t.Fatalf("owner accent: %q", got)
	}
	before := f.row(t)
	if before.accent != "DA291C" {
		t.Fatalf("stored accent %q, want DA291C", before.accent)
	}

	for _, attempt := range []func() *http.Response{
		func() *http.Response { return dbPost(t, f.managerServer, brandLogoHref, body, ct) },
		func() *http.Response {
			return dbForm(t, f.managerServer, brandAccentHref, url.Values{"accent": {"#1F5C41"}})
		},
		func() *http.Response { return dbForm(t, f.managerServer, brandResetHref, url.Values{"what": {"logo"}}) },
	} {
		if got := outcomeOf(attempt()); got != "not-permitted" {
			t.Errorf("a manager's POST: %q, want not-permitted", got)
		}
	}
	if after := f.row(t); after != before {
		t.Errorf("a manager changed the row: %+v -> %+v", before, after)
	}
	refused := f.trail(t, ActionBrandUpdateRefused)
	if len(refused) != 3 {
		t.Fatalf("%d refusal rows, want 3", len(refused))
	}
	for i, field := range []string{"logo", "accent", "reset"} {
		keys := make([]string, 0, len(refused[i]))
		for k := range refused[i] {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, []string{"field", "outcome", "reason", "required_role", "role"}) ||
			refused[i]["field"] != field || refused[i]["role"] != "manager" || refused[i]["reason"] != brandRefusedRole {
			t.Errorf("refusal %d: %v", i, refused[i])
		}
	}

	notAnImage, nct := oneLogo(t, []byte("not an image"))
	for i := 2; i <= brandUploadLimit; i++ {
		if got := outcomeOf(dbPost(t, f.ownerServer, brandLogoHref, notAnImage, nct)); got != "logo-format" {
			t.Fatalf("owner attempt %d: %q", i, got)
		}
	}
	if res := dbPost(t, f.ownerServer, brandLogoHref, notAnImage, nct); res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("owner attempt %d: %d, want 429", brandUploadLimit+1, res.StatusCode)
	}
	if after := f.row(t); after != before {
		t.Errorf("the refused attempts changed the row: %+v -> %+v", before, after)
	}
	if refused := f.trail(t, ActionBrandUpdateRefused); len(refused) != 4 || refused[3]["reason"] != brandRefusedBudget {
		t.Errorf("after the budget: %v, want a fourth row with the budget reason", refused)
	}

	if got := outcomeOf(dbForm(t, f.ownerServer, brandResetHref, url.Values{"what": {"accent"}})); got != "accent-removed" {
		t.Fatalf("reset accent: %q", got)
	}
	if r := f.row(t); r.accent != "" || r.sha != want.SHA256 {
		t.Errorf("after the accent reset: %+v", r)
	}
	if got := outcomeOf(dbForm(t, f.ownerServer, brandResetHref, url.Values{"what": {"logo"}})); got != "logo-removed" {
		t.Fatalf("reset logo: %q", got)
	}
	if r := f.row(t); r.accent != "" || r.sha != "" || r.bytes != 0 || !r.exists {
		t.Errorf("after both resets: %+v, want the row with every brand field empty", r)
	}
}

// TestBrandRoutesDB_ConcurrentWritesLeaveOneOfTheirStates: eighteen of the owner's
// changes at once -- six saves of DA291C, six of FFC72C, six resets of the accent --
// are each answered with their own word (none unavailable), each leaves one
// tenant.brand_updated row, and the stored accent is one of the three states they
// write. The domain serialises them on the row (WL-4); no answer is lost and no state
// is invented.
func TestBrandRoutesDB_ConcurrentWritesLeaveOneOfTheirStates(t *testing.T) {
	f := newBrandDBFixture(t)
	type change struct {
		route string
		form  url.Values
		want  string
	}
	var changes []change
	for i := 0; i < 6; i++ {
		changes = append(changes,
			change{brandAccentHref, url.Values{"accent_hex": {"DA291C"}}, "accent-saved"},
			change{brandAccentHref, url.Values{"accent_hex": {"FFC72C"}}, "accent-saved"},
			change{brandResetHref, url.Values{"what": {"accent"}}, "accent-removed"})
	}
	var wg sync.WaitGroup
	answers := make([]string, len(changes))
	start := make(chan struct{})
	for i, c := range changes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req, _ := http.NewRequest(http.MethodPost, f.ownerServer.URL+c.route, strings.NewReader(c.form.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", testBaseURL)
			req.AddCookie(panelCookie())
			res, err := noRedirects().Do(req)
			if err != nil {
				answers[i] = "error: " + err.Error()
				return
			}
			res.Body.Close()
			answers[i] = outcomeOf(res)
		}()
	}
	close(start)
	wg.Wait()
	for i, c := range changes {
		if answers[i] != c.want {
			t.Errorf("change %d (%s %v): %q, want %s", i, c.route, c.form, answers[i], c.want)
		}
	}
	if rows := f.trail(t, tenant.ActionBrandUpdated); len(rows) != len(changes) {
		t.Errorf("%d trail rows for %d changes", len(rows), len(changes))
	}
	if a := f.row(t).accent; !slices.Contains([]string{"", "DA291C", "FFC72C"}, a) {
		t.Errorf("the stored accent is %q, which none of the changes wrote", a)
	}
}
