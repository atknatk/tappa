package checkin

// manuallock_db_test.go -- M15-05 step 9 (ADR 0032 §A.6 "Eşzamanlı yazım"; the engine lens's
// DÜŞÜK-2): a manager's record and the person's own checkout tap, written at the same moment, are
// serial for that person -- manual.Recorder takes the tap path's lock (LockEmployeeForTap) as the
// first statement of its transaction -- so the checkout classification's W (which row was WRITTEN
// later) tells the truth in both orders.
//
// THE INTERLEAVING IS FORCED, NOT HOPED FOR. A third connection holds the person's employees row
// FOR UPDATE: every INSERT into transactions checks its foreign key with FOR KEY SHARE on that row,
// so each writer stops at its INSERT, with whatever it has already read and locked, until the
// holder lets go. The first writer is started and seen waiting (pg_blocking_pids) before the
// second starts; then the row is released.
//
//	manual first   the correction's transaction BEGINs first. With the lock, the tap waits for it,
//	               then sees the check-in closed and records an `in`. Without it (measured by
//	               mutation: the lock removed), the tap reads the check-in still open, writes an
//	               `out` with the LATER created_at, and that out -- preceded by the correction,
//	               which was written EARLIER -- classifies as an orphan (c): a false "check-in
//	               missing". This order is the one the lock exists for.
//	tap first      the tap's `out` is written first, the correction after it: the out is the
//	               correction's surplus (e) -- ADR 0011's "out too late" shape.
//
// So the card's "the correction shape is (e) in every order" holds where the tap's out exists;
// where the manager is first the tap is an `in` (it saw the correction) and there is no surplus
// out to classify. In both orders no checkout is (c). The THIRD order -- the correction's
// transaction BEGAN before the tap but reached the lock after the tap committed -- is serial yet
// would carry the earlier created_at; the lock cannot close it and Record's retry does
// (internal/domain/manual's TestManualRecord_RetriesACorrectionThatBeganBeforeATap).
//
// THE HOLDING PEER'S VENUE TOO: a correction filed at a peer's venue (recordAtPartner) beside the
// person's tap on that peer's plaque, both orders: both complete (no 40P01) -- the manual path's
// lock order is the tap's, person key first, then the holding feed's counters the INSERT's trigger
// takes (ADR 0027 §8, K-31).

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tap"
)

// lockScene is one forced interleaving's tools: a connection that holds the person's row and one
// that watches who waits.
type lockScene struct {
	t        *testing.T
	ctx      context.Context
	holder   pgx.Tx
	holderID int32
	watch    *pgx.Conn
}

func (w *holdingWorld) holdEmployeeRow(employee uuid.UUID) *lockScene {
	w.t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	hc, err := pgx.Connect(w.ctx, dsn)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { _ = hc.Close(context.Background()) })
	wc, err := pgx.Connect(w.ctx, dsn)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { _ = wc.Close(context.Background()) })
	tx, err := hc.Begin(w.ctx)
	if err != nil {
		w.t.Fatal(err)
	}
	s := &lockScene{t: w.t, ctx: w.ctx, holder: tx, watch: wc}
	if err := tx.QueryRow(w.ctx, `SELECT pg_backend_pid()`).Scan(&s.holderID); err != nil {
		w.t.Fatal(err)
	}
	var got uuid.UUID
	if err := tx.QueryRow(w.ctx, `SELECT id FROM employees WHERE id = $1 FOR UPDATE`, employee).Scan(&got); err != nil {
		w.t.Fatal(err)
	}
	return s
}

// waiters returns the backends waiting on the holder, directly or behind one that does.
func (s *lockScene) waiters() []int32 {
	s.t.Helper()
	var pids []int32
	if err := s.watch.QueryRow(s.ctx, `
		WITH direct AS (SELECT pid FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))
		SELECT coalesce(array_agg(pid ORDER BY pid), '{}') FROM (
		    SELECT pid FROM direct
		    UNION
		    SELECT a.pid FROM pg_stat_activity a, direct d WHERE d.pid = ANY(pg_blocking_pids(a.pid))
		) x`, s.holderID).Scan(&pids); err != nil {
		s.t.Fatal(err)
	}
	return pids
}

// waitFor polls until n backends wait on the holder.
func (s *lockScene) waitFor(n int) {
	s.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if len(s.waiters()) >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.t.Fatalf("PREMISE: %d backend(s) wait on the held row, want %d -- the interleaving was not forced", len(s.waiters()), n)
}

func (s *lockScene) release() {
	s.t.Helper()
	if err := s.holder.Rollback(s.ctx); err != nil {
		s.t.Fatal(err)
	}
}

func (w *holdingWorld) admin(tenant uuid.UUID) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Owner', $3, $4, 'owner')`,
		id, tenant, "o-"+id.String()[:12]+"@example.test", "$2a$12$"+strings.Repeat("h", 53))
	return id
}

func (w *holdingWorld) recorder() *manual.Recorder {
	w.t.Helper()
	trail, err := audit.New(w.data)
	if err != nil {
		w.t.Fatal(err)
	}
	rec, err := manual.NewRecorder(w.data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		w.t.Fatal(err)
	}
	return rec
}

// checkoutClasses is the person's checkouts' classes in (occurred_at, id) order, by
// tappa_checkout_class with P, I and W found as ADR 0032 §A.6 defines them (the parity test's
// query, internal/domain/ledger).
func (w *holdingWorld) checkoutClasses(tenant, person uuid.UUID) []string {
	w.t.Helper()
	var out []string
	if err := w.data.WithTenant(w.ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
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
			 ORDER BY o.occurred_at, o.id`, tenant, person)
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
	return out
}

// race runs the two writers in the order given, the first seen waiting before the second starts,
// and returns their errors and the tap's result.
func race(t *testing.T, scene *lockScene, manualFirst bool, tapIt func() (Result, error), typeIt func() error) (Result, error, error) {
	t.Helper()
	var (
		wg               sync.WaitGroup
		res              Result
		tapErr, typedErr error
	)
	first, second := func() { res, tapErr = tapIt() }, func() { typedErr = typeIt() }
	if manualFirst {
		first, second = second, first
	}
	wg.Add(2)
	go func() { defer wg.Done(); first() }()
	scene.waitFor(1)
	go func() { defer wg.Done(); second() }()
	scene.waitFor(2)
	scene.release()
	wg.Wait()
	return res, tapErr, typedErr
}

// TestManualRecord_TakesThePersonsTapLockSoACorrectionIsNeverAnOrphan is the card's lock criterion
// on the business's own venue, both orders (see the file comment for what each order shows).
func TestManualRecord_TakesThePersonsTapLockSoACorrectionIsNeverAnOrphan(t *testing.T) {
	w := newHoldingWorld(t)
	boss := w.admin(w.kf)
	rec := w.recorder()
	for i, manualFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("manual first %v", manualFirst), func(t *testing.T) {
			person := w.employee(w.kf, "active")
			now := time.Now()
			w.seedIn(w.kf, person, w.kfVenue, w.kfTag, now.Add(-3*time.Hour))
			scene := w.holdEmployeeRow(person)
			res, tapErr, typedErr := race(t, scene, manualFirst,
				func() (Result, error) {
					return w.svc.Record(w.ctx, w.nfc(w.kf, person, w.kfTag, w.kf, uint32(201+i), w.kfOnNet))
				},
				func() error {
					_, err := rec.Record(w.ctx, manual.Entry{TenantID: w.kf, EmployeeID: person, EnteredBy: boss,
						Direction: manual.Out, At: now.Add(-time.Hour).UTC(), LocationID: w.kfVenue})
					return err
				})
			if tapErr != nil || typedErr != nil {
				t.Fatalf("tap: %v; typed record: %v", tapErr, typedErr)
			}
			got := strings.Join(w.checkoutClasses(w.kf, person), ",")
			switch {
			case strings.Contains(got, "orphan"):
				t.Errorf("checkout classes %q: a correction written beside the tap made an orphan -- the two writes were not serial", got)
			case manualFirst && (res.Decision.Type == nil || *res.Decision.Type != tap.TypeIn || got != "closes"):
				t.Errorf("manual first: the tap is %v and the checkouts %q; want in (it saw the correction) and closes", res.Decision.Type, got)
			case !manualFirst && (res.Decision.Type == nil || *res.Decision.Type != tap.TypeOut || got != "closes,extra"):
				t.Errorf("tap first: the tap is %v and the checkouts %q; want out, then closes,extra (the correction's surplus)", res.Decision.Type, got)
			}
		})
	}
}

// TestManualRecord_AtAPeersVenueBesideAPeerTapCompletesInBothOrders is the holding half: a KM
// manager files a correction at KF's venue for KM's employee while that employee taps KF's plaque;
// in both orders both writes complete (the manual path takes the person's key before the INSERT's
// holding-feed counters, the tap path's order) and no checkout is an orphan.
func TestManualRecord_AtAPeersVenueBesideAPeerTapCompletesInBothOrders(t *testing.T) {
	w := newHoldingWorld(t)
	boss := w.admin(w.km)
	rec := w.recorder()
	for i, manualFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("manual first %v", manualFirst), func(t *testing.T) {
			person := w.employee(w.km, "active")
			w.seedSiteIn(person, 3*time.Hour)
			scene := w.holdEmployeeRow(person)
			_, tapErr, typedErr := race(t, scene, manualFirst,
				func() (Result, error) {
					return w.svc.Record(w.ctx, w.nfc(w.km, person, w.kfTag, w.kf, uint32(301+i), w.kfOnNet))
				},
				func() error {
					_, err := rec.Record(w.ctx, manual.Entry{TenantID: w.km, EmployeeID: person, EnteredBy: boss,
						Direction: manual.Out, At: time.Now().Add(-time.Hour).UTC(), SiteLocationID: w.kfVenue})
					return err
				})
			if tapErr != nil || typedErr != nil {
				t.Fatalf("tap: %v; typed record: %v -- a 40P01 here is the lock order breaking", tapErr, typedErr)
			}
			if got := strings.Join(w.checkoutClasses(w.km, person), ","); strings.Contains(got, "orphan") {
				t.Errorf("checkout classes %q: a correction at the peer's venue made an orphan", got)
			}
		})
	}
}
