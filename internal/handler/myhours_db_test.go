package handler

// myhours_db_test.go -- M16-32 end to end over the tap surface's real chain and real Postgres:
// [G32-c] no session is the plain sentence and reads nothing; [G32-b] the setting off is the router's
// own 404 with no record text; [G32-a] on, the cookie's person sees only their own records, and a
// session of another business sees none of this one's; a week or a cursor the read refuses is 400;
// [G32-e] a week of 201 records pages with "Show more" and both pages carry the same total; every
// answer is no-store and draws no logo.

import (
	"context"
	"encoding/hex"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/session"
)

// countingHours is the real reader, counted.
type countingHours struct {
	real  myHoursReader
	calls atomic.Int32
}

func (c *countingHours) OwnHours(ctx context.Context, tenantID uuid.UUID, q ledger.OwnQuery) (ledger.OwnWeek, error) {
	c.calls.Add(1)
	return c.real.OwnHours(ctx, tenantID, q)
}

type myHoursFixture struct {
	h      *tapHarness
	reader *countingHours
	router http.Handler
	owner  *pgx.Conn
}

func newMyHoursFixture(t *testing.T) *myHoursFixture {
	t.Helper()
	h := newTapHarness(t)
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if ownerDSN == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the fixture's records are written as the owner")
	}
	real, err := ledger.NewReader(h.data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	reader := &countingHours{real: real}
	if err := h.tap.ServeMyHours(reader); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	h.tap.Mount(r)
	owner, err := pgx.Connect(context.Background(), ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	return &myHoursFixture{h: h, reader: reader, router: r, owner: owner}
}

func (f *myHoursFixture) setWeeks(t *testing.T, tenantID uuid.UUID, n any) {
	t.Helper()
	if _, err := f.owner.Exec(context.Background(), `UPDATE tenants SET staff_hours_weeks = $2 WHERE id = $1`, tenantID, n); err != nil {
		t.Fatal(err)
	}
}

// write commits count flag check-ins of person at at, at-1s, at-2s, ...
func (f *myHoursFixture) write(t *testing.T, tenantID, person, location uuid.UUID, at time.Time, count int) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenantID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, created_at, verdict, channel, type, practice)
	                           SELECT $1, $2, $3, $4::timestamptz - make_interval(secs => g), $4::timestamptz - make_interval(secs => g), 'flag', 'nfc', 'in', false
	                           FROM generate_series(0, $5 - 1) AS g`, tenantID, person, location, at, count); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func (f *myHoursFixture) get(t *testing.T, target string, c *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if c != nil {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("%s: Cache-Control %q, want no-store", target, cc)
	}
	if strings.Contains(w.Body.String(), "<img") || strings.Contains(w.Body.String(), "/brand/theme/") {
		t.Errorf("%s: the page draws a logo or a theme", target)
	}
	return w
}

// zoneOf is the business's zone, as the page prints its times.
func (f *myHoursFixture) zoneOf(t *testing.T, tenantID uuid.UUID) *time.Location {
	t.Helper()
	var name string
	if err := f.owner.QueryRow(context.Background(), `SELECT timezone FROM tenants WHERE id = $1`, tenantID).Scan(&name); err != nil {
		t.Fatal(err)
	}
	z, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return z
}

func TestMyHoursDB_WithoutASessionIsThePlainSentenceAndReadsNothing(t *testing.T) {
	f := newMyHoursFixture(t)
	f.setWeeks(t, f.h.tenantID, 1)
	t.Cleanup(func() { f.setWeeks(t, f.h.tenantID, nil) })
	w := f.get(t, MyHoursPath, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Open this on the phone you clock in with.") {
		t.Fatalf("no session = %d %s", w.Code, w.Body.String())
	}
	// A revoked session is no session.
	c := f.h.cookieForEmployee(t, f.h.employeeID)
	if _, err := f.owner.Exec(context.Background(), `UPDATE sessions SET revoked_at = now() WHERE tenant_id = $1 AND employee_id = $2`, f.h.tenantID, f.h.employeeID); err != nil {
		t.Fatal(err)
	}
	if w := f.get(t, MyHoursPath, c); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Open this on the phone you clock in with.") {
		t.Fatalf("revoked session = %d %s", w.Code, w.Body.String())
	}
	if n := f.reader.calls.Load(); n != 0 {
		t.Fatalf("the read ran %d time(s) without a live session, want 0", n)
	}
}

func TestMyHoursDB_TheSettingOffIsTheRoutersOwn404(t *testing.T) {
	f := newMyHoursFixture(t)
	f.write(t, f.h.tenantID, f.h.employeeID, f.h.locationID, time.Now().UTC().Add(-time.Minute), 1)
	w := f.get(t, MyHoursPath, f.h.cookieForEmployee(t, f.h.employeeID))
	if w.Code != http.StatusNotFound || w.Body.String() != "404 page not found\n" {
		t.Fatalf("off = %d %q, want the router's own 404", w.Code, w.Body.String())
	}
}

func TestMyHoursDB_ThePersonSeesOnlyTheirOwnRecords(t *testing.T) {
	f := newMyHoursFixture(t)
	f.setWeeks(t, f.h.tenantID, 1)
	t.Cleanup(func() { f.setWeeks(t, f.h.tenantID, nil) })
	zone := f.zoneOf(t, f.h.tenantID)
	now := time.Now().UTC().Truncate(time.Minute)
	other := f.h.newEmployee(t, "active")
	mine, theirs := now.Add(-time.Minute), now.Add(-3*time.Minute)
	f.write(t, f.h.tenantID, f.h.employeeID, f.h.locationID, mine, 1)
	f.write(t, f.h.tenantID, other, f.h.locationID, theirs, 1)

	w := f.get(t, MyHoursPath, f.h.cookieForEmployee(t, f.h.employeeID))
	body := html.UnescapeString(w.Body.String())
	if w.Code != http.StatusOK {
		t.Fatalf("on = %d %s", w.Code, body)
	}
	if !strings.Contains(body, mine.In(zone).Format("15:04")) || !strings.Contains(body, "Waiting for your manager") {
		t.Fatalf("the person's own record is not listed in plain words:\n%s", body)
	}
	if strings.Contains(body, theirs.In(zone).Format("15:04")) {
		t.Fatalf("another person's record is on the page:\n%s", body)
	}
	for _, s := range []string{"APPROVED", "FLAGGED", "stamp", "trust", "Trust"} {
		if strings.Contains(body, s) {
			t.Errorf("the page carries %q (plain words only, no score)", s)
		}
	}

	// Another business's session: its own week, none of this one's records.
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	tenantB, locB, empB := uuid.New(), uuid.New(), uuid.New()
	f.h.newTag(t, kek, tenantB, locB, 1)
	if _, err := f.owner.Exec(context.Background(), `INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
	                                                  VALUES ($1, $2, $3, 'Tenant B Person', 'active', now())`, empB, tenantB, locB); err != nil {
		t.Fatal(err)
	}
	f.setWeeks(t, tenantB, 1)
	issued, err := f.h.sessions.Issue(context.Background(), session.IssueParams{TenantID: tenantB, EmployeeID: empB, DeviceInfo: "test"})
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := f.h.cookies.Set(rec, issued.Token); err != nil {
		t.Fatal(err)
	}
	var cookieB *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			cookieB = c
		}
	}
	wb := f.get(t, MyHoursPath, cookieB)
	bodyB := html.UnescapeString(wb.Body.String())
	if wb.Code != http.StatusOK || strings.Contains(bodyB, mine.In(zone).Format("15:04")) || strings.Contains(bodyB, "Waiting for your manager") {
		t.Fatalf("another business's session = %d, and it shows this business's record:\n%s", wb.Code, bodyB)
	}

	// A week the read refuses and a cursor of the wrong shape are 400.
	prev := now.AddDate(0, 0, -14).In(zone)
	for prev.Weekday() != time.Monday {
		prev = prev.AddDate(0, 0, -1)
	}
	for _, q := range []string{"?week=" + prev.Format("2006-01-02"), "?week=x", "?after=nope"} {
		if w := f.get(t, MyHoursPath+q, f.h.cookieForEmployee(t, f.h.employeeID)); w.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", q, w.Code)
		}
	}
}

var showMoreHref = regexp.MustCompile(`href="(/me/hours\?[^"]*after=[^"]*)"[^>]*>Show more<`)

func TestMyHoursDB_ShowMoreOpensTheNextPageWithTheSameTotal(t *testing.T) {
	f := newMyHoursFixture(t)
	f.setWeeks(t, f.h.tenantID, 1)
	t.Cleanup(func() { f.setWeeks(t, f.h.tenantID, nil) })
	f.write(t, f.h.tenantID, f.h.employeeID, f.h.locationID, time.Now().UTC().Add(-time.Second), ledger.OwnPageSize+1)
	c := f.h.cookieForEmployee(t, f.h.employeeID)

	first := f.get(t, MyHoursPath, c)
	m := showMoreHref.FindStringSubmatch(first.Body.String())
	if first.Code != http.StatusOK || m == nil {
		t.Fatalf("page one = %d, no Show more link", first.Code)
	}
	second := f.get(t, html.UnescapeString(m[1]), c)
	if second.Code != http.StatusOK || showMoreHref.MatchString(second.Body.String()) {
		t.Fatalf("page two = %d, or it offers another page", second.Code)
	}
	count := func(s string) int { return strings.Count(s, "Waiting for your manager") }
	if a, b := count(first.Body.String()), count(second.Body.String()); a != ledger.OwnPageSize || b != 1 {
		t.Fatalf("records on the two pages: %d and %d, want %d and 1", a, b, ledger.OwnPageSize)
	}
	total := regexp.MustCompile(`class="report-total">([^<]*)<`)
	t1, t2 := total.FindStringSubmatch(first.Body.String()), total.FindStringSubmatch(second.Body.String())
	if t1 == nil || t2 == nil || t1[1] != t2[1] {
		t.Fatalf("the totals differ between the pages: %v %v", t1, t2)
	}
}
