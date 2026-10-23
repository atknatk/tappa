package manual

// race_db_test.go -- M15-05 review round (ADR 0032 §A.6 "Eşzamanlı yazım"): the THIRD order of a
// manager's correction and the person's checkout tap. created_at is the transaction's start, so a
// correction whose transaction BEGAN before the tap but reached the person's lock after the tap
// committed would carry the earlier created_at and make the tap's out read as an orphan (c). Record
// detects it under the lock and starts again. The other two orders are internal/domain/checkin's
// TestManualRecord_TakesThePersonsTapLockSoACorrectionIsNeverAnOrphan.
//
// THE ORDER IS FORCED, NOT TIMED: Recorder.beforeLock holds the correction's first attempt after
// its BEGIN while the real tap path (checkin.Service.Record) runs to its commit. Real Postgres, own
// tenant; the seed tenant is not touched.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/test/fixtures"
)

type raceWorld struct {
	t                         *testing.T
	ctx                       context.Context
	data                      *db.DB
	owner                     *pgx.Conn
	svc                       *checkin.Service
	rec                       *Recorder
	tenant, venue, admin, who uuid.UUID
	plaque                    string
}

func newRaceWorld(t *testing.T) *raceWorld {
	t.Helper()
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	cfg := &config.Config{
		Env: config.EnvDev, BaseURL: "http://localhost:8080", DatabaseURL: appDSN,
		SessionHMACKey: []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"), InviteHMACKey: []byte("IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII"),
		TagKEK: make([]byte, 32), RetentionYears: 2, GPSRadiusMeters: 150, Debounce: 60 * time.Second, Freshness: 180 * time.Second,
		BusinessDayFrom: fixtures.BusinessDaySwitchOn(),
	}
	data, err := db.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := checkin.New(data, trail, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := NewRecorder(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	w := &raceWorld{t: t, ctx: ctx, data: data, owner: owner, svc: svc, rec: rec, tenant: uuid.New(), admin: uuid.New(), who: uuid.New()}
	w.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'm15-05 race', $2, 'restaurant', 'multi')`,
		w.tenant, "VAT-M1505R-"+w.tenant.String())
	if err := owner.QueryRow(ctx, `INSERT INTO locations (tenant_id, name, static_ips, shift_start, shift_end)
	                               VALUES ($1, 'm15-05 race venue', '{203.0.113.0/24}', '09:00', '17:00') RETURNING id`, w.tenant).Scan(&w.venue); err != nil {
		t.Fatal(err)
	}
	b, key := make([]byte, 7), make([]byte, 44)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	w.plaque = strings.ToUpper(hex.EncodeToString(b))
	w.exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 100, 'active')`,
		w.plaque, w.tenant, w.venue, key)
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Owner', $3, $4, 'owner')`,
		w.admin, w.tenant, "o-"+w.admin.String()[:12]+"@example.test", "$2a$12$"+strings.Repeat("h", 53))
	w.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at, location_id) VALUES ($1, $2, $3, 'Race Staff', 'active', now(), $4)`,
		w.who, w.tenant, "ext-"+w.who.String()[:8], w.venue)
	// A cutoff far from now (M15-04's helper), so the check-in 3 h ago and the tap now share a day.
	if err := data.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := fixtures.GiveFarDayCutoff(ctx, tx, w.tenant, w.who, time.Now())
		return e
	}); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w *raceWorld) exec(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.owner.Exec(w.ctx, sql, args...); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
}

// directed writes a directed tap of the person's, committed, as tappa_app in the tenant's context.
// written nil: NOW (created_at DEFAULT now()); otherwise that instant -- the check-in the scene starts
// from is written at its own instant, so the tap's debounce (which measures created_at) does not
// see it.
func (w *raceWorld) directed(dir string, at time.Time, ctr int, written *time.Time) {
	w.t.Helper()
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, created_at, verdict, channel, type, sun_valid, ctr)
		                      VALUES ($1, $2, $3, $4, $5, coalesce($8, now()), 'ok', 'nfc', $6, true, $7)`, w.tenant, w.who, w.venue, w.plaque, at, dir, ctr, written)
		return e
	}); err != nil {
		w.t.Fatalf("write a %s: %v", dir, err)
	}
}

// classes is the person's checkouts' classes in (occurred_at, id) order -- tappa_checkout_class
// with P, I and W found as ADR 0032 §A.6 defines them (the parity test's query).
func (w *raceWorld) classes() string {
	w.t.Helper()
	var out []string
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT coalesce(public.tappa_checkout_class(
			           CASE WHEN p.type = 'in' THEN p.occurred_at END, p.type = 'out',
			           EXISTS (SELECT 1 FROM transactions i
			                    WHERE i.tenant_id = o.tenant_id AND i.employee_id = o.employee_id
			                      AND i.type = 'in' AND NOT i.practice
			                      AND (i.occurred_at, i.id) < (o.occurred_at, o.id)
			                      AND i.occurred_at >= o.business_day_start),
			           p.created_at > o.created_at, o.occurred_at, o.business_day_start), '')
			  FROM transactions o
			  LEFT JOIN LATERAL (
			      SELECT x.type, x.occurred_at, x.created_at FROM transactions x
			       WHERE x.tenant_id = o.tenant_id AND x.employee_id = o.employee_id
			         AND x.type IS NOT NULL AND NOT x.practice
			         AND (x.occurred_at, x.id) < (o.occurred_at, o.id)
			       ORDER BY x.occurred_at DESC, x.id DESC LIMIT 1) p ON true
			 WHERE o.tenant_id = $1 AND o.employee_id = $2 AND o.type = 'out' AND NOT o.practice
			 ORDER BY o.occurred_at, o.id`, w.tenant, w.who)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	}); err != nil {
		w.t.Fatalf("classify: %v", err)
	}
	return strings.Join(out, ",")
}

// TestManualRecord_RetriesACorrectionThatBeganBeforeATap is the third order: the correction's
// transaction BEGINs (the hook holds it there), the person's checkout tap takes the lock, reads the
// check-in open, writes its out and commits, and only then does the correction reach the lock.
// Record sees the tap's row written after its transaction began and starts again, so the correction
// is written after the tap -- the tap's out is the correction's surplus (e), never an orphan (c).
// Measured by mutation: without the retry the same scene answers `closes,orphan`.
func TestManualRecord_RetriesACorrectionThatBeganBeforeATap(t *testing.T) {
	w := newRaceWorld(t)
	now := time.Now()
	start := now.Add(-3 * time.Hour)
	w.directed("in", start, 90, &start)
	began, release := make(chan struct{}), make(chan struct{})
	var attempts []int
	var mu sync.Mutex
	w.rec.beforeLock = func(attempt int) {
		mu.Lock()
		attempts = append(attempts, attempt)
		mu.Unlock()
		if attempt == 0 {
			close(began)
			<-release
		}
	}
	var wg sync.WaitGroup
	var typedErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, typedErr = w.rec.Record(w.ctx, Entry{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin,
			Direction: Out, At: now.Add(-time.Hour).UTC()})
	}()
	<-began
	res, err := w.svc.Record(w.ctx, checkin.Request{SessionTenantID: w.tenant, EmployeeID: w.who, TagUID: w.plaque, Ctr: 101,
		Channel: tap.ChannelNFC, CMACVerified: true, TagTenantID: w.tenant, SourceIP: netip.MustParseAddr("203.0.113.7")})
	if err != nil {
		t.Fatalf("the tap: %v", err)
	}
	close(release)
	wg.Wait()
	if typedErr != nil {
		t.Fatalf("the correction: %v", typedErr)
	}
	if res.Decision.Type == nil || *res.Decision.Type != tap.TypeOut {
		t.Fatalf("PREMISE: the tap is %v, want out -- it must have read the check-in open before the correction", res.Decision.Type)
	}
	if got := w.classes(); got != "closes,extra" {
		t.Errorf("checkout classes %q, want closes,extra: the correction that began before the tap must be written after it", got)
	}
	if len(attempts) != 2 || attempts[0] != 0 || attempts[1] != 1 {
		t.Errorf("attempts %v, want [0 1]: one start again, because one tap committed while the correction waited", attempts)
	}
}

// TestManualRecord_WritesTheCorrectionAfterItsLastRetry pins the bound and §4.6: a directed record
// of the person's commits during EVERY attempt, and the correction is still written -- on the
// attempt after the last retry, which no longer checks. retriesAfterATap retries, one write.
func TestManualRecord_WritesTheCorrectionAfterItsLastRetry(t *testing.T) {
	w := newRaceWorld(t)
	now := time.Now()
	start := now.Add(-5 * time.Hour)
	w.directed("in", start, 90, &start)
	var attempts []int
	w.rec.beforeLock = func(attempt int) {
		attempts = append(attempts, attempt)
		w.directed("in", now.Add(-4*time.Hour+time.Duration(attempt)*time.Minute), 91+attempt, nil)
	}
	got, err := w.rec.Record(w.ctx, Entry{TenantID: w.tenant, EmployeeID: w.who, EnteredBy: w.admin,
		Direction: Out, At: now.Add(-time.Hour).UTC()})
	if err != nil {
		t.Fatalf("the correction was not written: %v -- a record is never lost (§4.6)", err)
	}
	if len(attempts) != retriesAfterATap+1 {
		t.Errorf("attempts %v, want %d (the retries, then the write)", attempts, retriesAfterATap+1)
	}
	var n int
	if err := w.owner.QueryRow(w.ctx, `SELECT count(*) FROM transactions WHERE id = $1 AND channel = 'manual'`, got.ID).Scan(&n); err != nil || n != 1 {
		t.Errorf("the correction's row: %d (%v), want 1", n, err)
	}
}
