package tenant

// tapprediction_failure_db_test.go -- §4.6 for the tap screen's predicted direction (M15-12, ADR
// 0032 §B.2), at the read, against REAL Postgres: a prediction read that fails on the real
// connection -- the transaction really aborted, the way a database error leaves it -- never costs
// the page. TapPage and PeerTapPage answer without an error, with no prediction, the cause in
// PredictionErr, and every other fact they read (the greeting, the venue, TapPage's brand). The
// failure is produced the way brandAbortingDB produces the brand's (pagebrand_db_test.go): one
// statement replaced by a division by zero on the real transaction.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/test/fixtures"
)

// predictionFailingDB runs the real transaction, but the statement named fail (an sqlc query's
// "-- name: X :one" header) executes SELECT 1/0 on the real connection instead, and -- when
// badAccent is set -- holding_tap_site's row comes back with an accent no reader accepts.
type predictionFailingDB struct {
	real      *db.DB
	fail      string
	badAccent bool
}

func (d predictionFailingDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return d.real.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, predictionFailingTx{tx, d})
	})
}

type predictionFailingTx struct {
	pgx.Tx
	d predictionFailingDB
}

func (tx predictionFailingTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx.d.fail != "" && strings.HasPrefix(sql, "-- name: "+tx.d.fail+" :one") {
		return tx.Tx.QueryRow(ctx, "SELECT 1/0")
	}
	row := tx.Tx.QueryRow(ctx, sql, args...)
	if tx.d.badAccent && strings.Contains(sql, "holding_tap_site(") {
		return badAccentRow{row}
	}
	return row
}

// badAccentRow scans holding_tap_site's row and then spoils its accent (column 14 of 19): the
// peer's brand cannot be read, the way a stored value the reader refuses leaves it.
type badAccentRow struct{ pgx.Row }

func (r badAccentRow) Scan(dest ...any) error {
	if err := r.Row.Scan(dest...); err != nil {
		return err
	}
	if len(dest) == 19 {
		if p, ok := dest[13].(**string); ok {
			bad := "not-a-colour"
			*p = &bad
		}
	}
	return nil
}

// isDivisionByZero reports the database's own 22012 inside err.
func isDivisionByZero(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22012"
}

// TestTapPageDB_AFailedPredictionReadNeverDropsThePage is §4.6 on TapPage: with the switch on, an
// active person with an open check-in, a brand saved and the caller asking, a failure of either
// prediction read -- the open check-in (GetLastOpenTransaction) or the person's day
// (GetEmployeeForTap, read only for the prediction) -- leaves TapPage answering WITHOUT an error,
// with no prediction (not a guessed one), the database's error in PredictionErr, and the greeting,
// the venue and the brand it had already read. CONTROL: the same directory without the failure
// predicts `out`.
func TestTapPageDB_AFailedPredictionReadNeverDropsThePage(t *testing.T) {
	f := newBrandFixture(t)
	ctx := context.Background()
	loc, emp := f.wall(t, f.tenant, "St Julians")
	if err := f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: f.tenant, ActorID: f.owner, Accent: mustColour(t, "DA291C")}); err != nil {
		t.Fatalf("save accent: %v", err)
	}
	if err := f.data.WithTenant(ctx, f.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := fixtures.GiveFarDayCutoff(ctx, tx, f.tenant, emp, time.Now()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, created_at, verdict, channel, type, practice)
		                        VALUES ($1, $2, $3, now() - interval '1 hour', now() - interval '1 hour', 'ok', 'nfc', 'in', false)`, f.tenant, emp, loc)
		return err
	}); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	ok, _ := NewDirectory(f.data, fixtures.BusinessDaySwitchOn())
	control, err := ok.TapPage(ctx, f.tenant, emp, loc, true)
	if err != nil || control.Prediction == nil || control.Prediction.Direction != tap.TypeOut || !control.Brand.HasAccent {
		t.Fatalf("control: %+v, %v -- want a prediction of out and the brand", control, err)
	}

	for _, failing := range []string{"GetLastOpenTransaction", "GetEmployeeForTap"} {
		t.Run(failing, func(t *testing.T) {
			dir, err := NewDirectory(predictionFailingDB{real: f.data, fail: failing}, fixtures.BusinessDaySwitchOn())
			if err != nil {
				t.Fatal(err)
			}
			facts, err := dir.TapPage(ctx, f.tenant, emp, loc, true)
			if err != nil {
				t.Fatalf("err = %v: a failed prediction read cost the page (§4.6)", err)
			}
			if facts.Prediction != nil {
				t.Errorf("Prediction = %+v: a failed read must give none, not a guess", *facts.Prediction)
			}
			if !isDivisionByZero(facts.PredictionErr) {
				t.Errorf("PredictionErr = %v, want the database's 22012", facts.PredictionErr)
			}
			if facts.EmployeeName != "Maria Borg" || facts.LocationName != "St Julians" || facts.Brand != control.Brand {
				t.Errorf("facts = %+v; want the greeting, the venue and the brand %+v", facts, control.Brand)
			}
		})
	}
}

// TestPeerTapPageDB_AFailedPredictionReadNeverDropsThePage is the same on a PEER's plaque (M11
// API-1b): the employer's person with an open check-in in front of the site's plaque. A failed
// open-check-in read leaves PeerTapPage answering the peer (site) WITHOUT an error, with no
// prediction, the database's error in PredictionErr, and the greeting and the peer's venue. And
// when the peer's brand cannot be read (ErrBrandUnread), the facts carry no prediction either --
// TapPageFacts' rule, a second line behind the handler's. CONTROL: no failure predicts `out`.
func TestPeerTapPageDB_AFailedPredictionReadNeverDropsThePage(t *testing.T) {
	f := newSiteFixture(t)
	venue := f.venue("KF Sliema")
	uid := f.plaque(venue)
	if err := f.data.WithTenant(f.ctx, f.km, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := fixtures.GiveFarDayCutoff(ctx, tx, f.km, f.emp, time.Now()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, occurred_at, created_at, verdict, channel, type, practice)
		                        VALUES ($1, $2, now() - interval '1 hour', now() - interval '1 hour', 'ok', 'manual', 'in', false)`, f.km, f.emp)
		return err
	}); err != nil {
		t.Fatalf("fixture: %v", err)
	}

	peer := func(t *testing.T, d Database) (TapPageFacts, uuid.UUID, error) {
		t.Helper()
		dir, err := NewDirectory(d, fixtures.BusinessDaySwitchOn())
		if err != nil {
			t.Fatal(err)
		}
		return dir.PeerTapPage(f.ctx, f.km, f.emp, uid, true)
	}
	control, site, err := peer(t, f.data)
	if err != nil || site != f.kf || control.Prediction == nil || control.Prediction.Direction != tap.TypeOut || control.LocationName != "KF Sliema" {
		t.Fatalf("control: %+v, site %v, %v -- want the peer's venue and a prediction of out", control, site, err)
	}

	t.Run("the open check-in read fails", func(t *testing.T) {
		facts, site, err := peer(t, predictionFailingDB{real: f.data, fail: "GetLastOpenTransaction"})
		if err != nil || site != f.kf {
			t.Fatalf("site %v, err %v: a failed prediction read cost the peer's page (§4.6)", site, err)
		}
		if facts.Prediction != nil {
			t.Errorf("Prediction = %+v: a failed read must give none, not a guess", *facts.Prediction)
		}
		if !isDivisionByZero(facts.PredictionErr) {
			t.Errorf("PredictionErr = %v, want the database's 22012", facts.PredictionErr)
		}
		if facts.EmployeeName != "KM Staff" || facts.LocationName != "KF Sliema" {
			t.Errorf("facts = %+v; want the greeting and the peer's venue", facts)
		}
	})

	t.Run("the peer's brand cannot be read", func(t *testing.T) {
		facts, site, err := peer(t, predictionFailingDB{real: f.data, badAccent: true})
		if !errors.Is(err, ErrBrandUnread) || site != f.kf {
			t.Fatalf("site %v, err %v; want the peer with ErrBrandUnread", site, err)
		}
		if facts.Prediction != nil {
			t.Errorf("Prediction = %+v: a page without its brand has no prediction either", *facts.Prediction)
		}
		if facts.Brand != (PageBrand{}) || facts.LocationName != "KF Sliema" {
			t.Errorf("facts = %+v; want the zero brand and the peer's venue", facts)
		}
	})
}
