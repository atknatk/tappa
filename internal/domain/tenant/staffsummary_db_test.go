package tenant

// staffsummary_db_test.go -- M12-20's person summary against real Postgres, as tappa_app in the
// reading tenant's context (staffFixture's shape).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

func (f *staffFixture) exec(t *testing.T, tenantID uuid.UUID, sql string, args ...any) {
	t.Helper()
	if err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, sql, args...)
		return err
	}); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}

func summaryHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// session adds a browser session: created at created, last used at used (nil: never), revoked
// when revoked is set.
func (f *staffFixture) session(t *testing.T, tenantID, employeeID uuid.UUID, created time.Time, used *time.Time, revoked bool) {
	t.Helper()
	var revokedAt any
	if revoked {
		revokedAt = created.Add(time.Minute)
	}
	f.exec(t, tenantID, `INSERT INTO sessions (tenant_id, employee_id, token_hash, device_info, created_at, last_used_at, revoked_at)
	                     VALUES ($1, $2, $3, 'iPhone Safari', $4, $5, $6)`,
		tenantID, employeeID, summaryHex(t), created, used, revokedAt)
}

// link adds an activation link row: state "" pending, "used", "cancelled" or "expired".
func (f *staffFixture) link(t *testing.T, tenantID, employeeID uuid.UUID, created time.Time, state string) {
	t.Helper()
	expires := time.Now().Add(72 * time.Hour)
	var used, cancelled any
	switch state {
	case "used":
		used = created.Add(time.Minute)
	case "cancelled":
		cancelled = created.Add(time.Minute)
	case "expired":
		expires = time.Now().Add(-time.Hour)
	}
	f.exec(t, tenantID, `INSERT INTO employee_invites (tenant_id, employee_id, code_hash, created_at, expires_at, used_at, cancelled_at)
	                     VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		tenantID, employeeID, summaryHex(t), created, expires, used, cancelled)
}

// TestStaffSummaryDB_ReadsTheCardsFacts is M12-20's definition on one person with a history:
// role, the three stamps, live sessions only (count, oldest, last used), and the newest link that
// could still be used.
func TestStaffSummaryDB_ReadsTheCardsFacts(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	day := func(n int) time.Time { return now.Add(time.Duration(n) * 24 * time.Hour) }
	activated := day(-20)
	f.exec(t, f.tenantID, `UPDATE employees SET role = 'Head chef', activated_at = $2 WHERE id = $1`, f.employeeID, activated)

	// The oldest session is REVOKED: it is not the "since" of a live one.
	f.session(t, f.tenantID, f.employeeID, day(-30), nil, true)
	used := day(-1)
	f.session(t, f.tenantID, f.employeeID, day(-10), &used, false)
	f.session(t, f.tenantID, f.employeeID, day(-5), nil, false)

	f.link(t, f.tenantID, f.employeeID, day(-9), "used")
	f.link(t, f.tenantID, f.employeeID, day(-8), "expired")
	f.link(t, f.tenantID, f.employeeID, day(-7), "cancelled")
	f.link(t, f.tenantID, f.employeeID, day(-3), "")
	f.link(t, f.tenantID, f.employeeID, day(-2), "")

	got, err := f.staff.Summary(ctx, f.tenantID, f.employeeID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != Measured || got.Role != "Head chef" || got.Status != statusActive {
		t.Fatalf("summary %+v", got)
	}
	if got.Offered == nil || got.Activated == nil || !got.Activated.Equal(activated) || got.Deactivated != nil {
		t.Errorf("stamps offered=%v activated=%v deactivated=%v", got.Offered, got.Activated, got.Deactivated)
	}
	if s := got.Since(); s == nil || !s.Equal(activated) {
		t.Errorf("since %v, want the activation stamp for an active person", s)
	}
	if got.LiveDevices != 2 {
		t.Errorf("live devices %d, want 2 (the revoked one is not live)", got.LiveDevices)
	}
	if got.FirstSignedIn == nil || !got.FirstSignedIn.Equal(day(-10)) {
		t.Errorf("first signed in %v, want the oldest LIVE session %v", got.FirstSignedIn, day(-10))
	}
	if got.LastUsed == nil || !got.LastUsed.Equal(used) {
		t.Errorf("last used %v, want %v", got.LastUsed, used)
	}
	l := got.Link
	if l.State != Measured || l.Pending != 2 || l.Created == nil || !l.Created.Equal(day(-2)) || l.Expires == nil {
		t.Errorf("link %+v, want two pending, the newest created %v", l, day(-2))
	}
}

// TestStaffSummaryDB_NothingIsInventedForAPanelAddedPerson: added in the panel, never activated,
// no session, no link -- every date nil, every count a measured 0. And a deactivated person keeps
// the live sessions they had (deactivating does not sign phones out).
func TestStaffSummaryDB_NothingIsInventedForAPanelAddedPerson(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	fresh := uuid.New()
	f.exec(t, f.tenantID, `INSERT INTO employees (id, tenant_id, location_id, full_name) VALUES ($1, $2, $3, 'Just added')`,
		fresh, f.tenantID, f.locationID)
	got, err := f.staff.Summary(ctx, f.tenantID, fresh)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != Measured || got.Status != statusInvited || got.Role != "" {
		t.Fatalf("summary %+v", got)
	}
	if got.Offered != nil || got.Since() != nil || got.FirstSignedIn != nil || got.LastUsed != nil || got.LiveDevices != 0 {
		t.Errorf("a date or a device was invented: %+v", got)
	}
	if got.Link.State != Measured || got.Link.Pending != 0 || got.Link.Created != nil {
		t.Errorf("link %+v, want a measured none", got.Link)
	}

	f.session(t, f.tenantID, f.employeeID, time.Now().Add(-time.Hour), nil, false)
	f.exec(t, f.tenantID, `UPDATE employees SET status = 'deactivated', deactivated_at = now() WHERE id = $1`, f.employeeID)
	left, err := f.staff.Summary(ctx, f.tenantID, f.employeeID)
	if err != nil {
		t.Fatal(err)
	}
	if left.Status != statusDeactivated || left.Since() == nil || left.LiveDevices != 1 {
		t.Errorf("deactivated summary %+v, want the deactivation stamp and the one live session", left)
	}
}

// TestStaffSummaryDB_AForeignPersonIsUnknown: another tenant's employee id is "not on this
// roster", from either side of the call.
func TestStaffSummaryDB_AForeignPersonIsUnknown(t *testing.T) {
	f := newStaffFixture(t)
	for _, id := range []uuid.UUID{f.foreignEmployee, uuid.New(), uuid.Nil} {
		if _, err := f.staff.Summary(context.Background(), f.tenantID, id); !errors.Is(err, ErrUnknownEmployee) {
			t.Errorf("%v: %v, want ErrUnknownEmployee", id, err)
		}
	}
}

// TestRLS_PersonSummary_AnotherTenantsPersonIsNotRead: A's connection reads none of B's person,
// sessions or links. The probes carry no tenant predicate (CLAUDE.md §6); the production statements
// are then asked, from A's context, for B's tenant and person.
func TestRLS_PersonSummary_AnotherTenantsPersonIsNotRead(t *testing.T) {
	f := newStaffFixture(t)
	a, b, bWho := f.tenantID, f.foreignTenant, f.foreignEmployee
	f.session(t, b, bWho, time.Now().Add(-time.Hour), nil, false)
	f.link(t, b, bWho, time.Now().Add(-time.Hour), "")

	probes := []string{
		`SELECT count(*) FROM employees e WHERE e.id = $1`,
		`SELECT count(*) FROM sessions s WHERE s.employee_id = $1`,
		`SELECT count(*) FROM employee_invites i WHERE i.employee_id = $1`,
	}
	countIn := func(tenant uuid.UUID, probe string) int64 {
		var n int64
		if err := f.data.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, probe, bWho).Scan(&n)
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, p := range probes {
		if countIn(b, p) != 1 {
			t.Fatalf("positive control: B's own context does not see its row: %s", p)
		}
		if n := countIn(a, p); n != 0 {
			t.Errorf("RLS: A's tenant-filter-free probe sees %d of B's rows: %s", n, p)
		}
	}

	for _, ctxTenant := range []uuid.UUID{a, b} {
		var sumErr error
		var links []store.ListPendingInvitesForEmployeeRow
		if err := f.data.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			q := store.New(tx)
			_, sumErr = q.GetPanelPersonSummary(ctx, store.GetPanelPersonSummaryParams{TenantID: b, EmployeeID: bWho})
			if sumErr != nil && !errors.Is(sumErr, pgx.ErrNoRows) {
				return sumErr
			}
			var err error
			links, err = q.ListPendingInvitesForEmployee(ctx, store.ListPendingInvitesForEmployeeParams{TenantID: b, EmployeeID: bWho})
			return err
		}); err != nil {
			t.Fatal(err)
		}
		switch {
		case ctxTenant == b && (sumErr != nil || len(links) != 1):
			t.Errorf("positive control: B's context reads B's person: %v, %d links", sumErr, len(links))
		case ctxTenant == a && (!errors.Is(sumErr, pgx.ErrNoRows) || len(links) != 0):
			t.Errorf("RLS: A's context asked for B's person: %v, %d links", sumErr, len(links))
		}
	}
}

// failOneSummaryDB answers ONE named sqlc statement with a genuine statement error on the real
// connection (the transaction really aborts); everything else, COMMIT included, is production's.
type failOneSummaryDB struct {
	real *db.DB
	name string
}

func (d failOneSummaryDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return d.real.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, failOneSummaryTx{Tx: tx, name: d.name})
	})
}

type failOneSummaryTx struct {
	pgx.Tx
	name string
}

func (f failOneSummaryTx) Begin(ctx context.Context) (pgx.Tx, error) {
	sp, err := f.Tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return failOneSummaryTx{Tx: sp, name: f.name}, nil
}

func (f failOneSummaryTx) swap(sql string, args []any) (string, []any) {
	if strings.HasPrefix(sql, "-- name: "+f.name+" ") {
		return "SELECT 1/0", nil
	}
	return sql, args
}

func (f failOneSummaryTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	sql, args = f.swap(sql, args)
	return f.Tx.Query(ctx, sql, args...)
}

func (f failOneSummaryTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	sql, args = f.swap(sql, args)
	return f.Tx.QueryRow(ctx, sql, args...)
}

// TestStaffSummaryDB_AFailedReadSaysSo: a failed link read costs the link and keeps the summary
// (the savepoint, through the real COMMIT); a failed summary is ReadFailed with no error -- the
// card stands, the figures say they could not be read, none of them is a 0.
func TestStaffSummaryDB_AFailedReadSaysSo(t *testing.T) {
	f := newStaffFixture(t)
	f.link(t, f.tenantID, f.employeeID, time.Now().Add(-time.Hour), "")
	for _, tc := range []struct {
		failing          string
		summary, linkSay Reading
	}{
		{"ListPendingInvitesForEmployee", Measured, ReadFailed},
		{"GetPanelPersonSummary", ReadFailed, NotQueried},
	} {
		t.Run(tc.failing, func(t *testing.T) {
			staff, err := NewStaff(failOneSummaryDB{real: f.data, name: tc.failing}, f.trail, slog.New(slog.DiscardHandler))
			if err != nil {
				t.Fatal(err)
			}
			got, err := staff.Summary(context.Background(), f.tenantID, f.employeeID)
			if err != nil {
				t.Fatalf("Summary returned %v: a failed read must not cost the card", err)
			}
			if got.State != tc.summary || got.Link.State != tc.linkSay {
				t.Errorf("summary %v / link %v, want %v / %v", got.State, got.Link.State, tc.summary, tc.linkSay)
			}
			if got.Link.Pending != 0 || got.Link.Created != nil {
				t.Errorf("a failed read carries a figure: %+v", got.Link)
			}
		})
	}
	// Control: the real database measures both.
	if got, err := f.staff.Summary(context.Background(), f.tenantID, f.employeeID); err != nil || got.State != Measured || got.Link.Pending != 1 {
		t.Errorf("control: %+v, %v", got, err)
	}
}
