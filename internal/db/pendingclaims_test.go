package db

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// pendingclaims_test.go -- T166: CountPendingPunchClaims counts "claims minus decisions" instead of
// anti-joining the two tables (db/queries/punchclaims.sql says why that is an identity of 00068's
// keys). This pins that the shipped statement is the set the anti-join counted, capped the way the
// anti-join's LIMIT capped it, for two businesses side by side -- one's decisions never move the
// other's number -- and that the numbers are the ones the fixture makes, so two equally wrong forms
// cannot agree with each other and pass.

// pcPendingReference is the statement CountPendingPunchClaims was before T166, word for word.
const pcPendingReference = `
SELECT count(*)
  FROM (
        SELECT 1
          FROM punch_claims AS c
         WHERE c.tenant_id = $1
           AND NOT EXISTS (
               SELECT 1
                 FROM punch_claim_decisions AS d
                WHERE d.tenant_id = $1
                  AND d.claim_id = c.id
           )
         LIMIT $2::int
       ) AS capped`

// TestPendingPunchClaims_TheCountIsTheClaimsWithoutADecision walks two businesses from four
// undecided claims each to none undecided in one of them, and at every step compares the shipped
// count with the anti-join and with the number the step leaves, for caps below, at and above it.
func TestPendingPunchClaims_TheCountIsTheClaimsWithoutADecision(t *testing.T) {
	app := appDB(t)
	assertAppRole(t, app)
	a, b := pcBuildWorld(t, app), pcBuildWorld(t, app)

	claims := func(w pcWorld) []uuid.UUID {
		later := w.inAt.Add(8 * time.Hour)
		return []uuid.UUID{
			pcClaim(t, app, w, w.openIn, "out", later, nil),
			pcClaim(t, app, w, w.openIn2, "out", later, nil),
			pcClaim(t, app, w, w.spareIn, "out", later, nil),
			pcClaim(t, app, w, w.orphanOut, "in", w.inAt, nil),
		}
	}
	ca, cb := claims(a), claims(b)

	reject := func(w pcWorld, claim uuid.UUID) {
		t.Helper()
		if err := app.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, pcInsertDecision, w.tenant, claim, "rejected", w.admin, nil, nil)
			return e
		}); err != nil {
			t.Fatalf("reject %s: %v", claim, err)
		}
	}
	approve := func(w pcWorld, claim uuid.UUID) {
		t.Helper()
		at := w.inAt.Add(8 * time.Hour)
		manual := pcManual(t, app, w, w.employee, "out", at, w.admin)
		if err := app.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, pcInsertDecision, w.tenant, claim, "approved", w.admin, at, manual)
			return e
		}); err != nil {
			t.Fatalf("approve %s: %v", claim, err)
		}
	}
	check := func(step string, w pcWorld, pending int64) {
		t.Helper()
		for _, cap := range []int32{1, 2, 3, 4, 5, 100} {
			var got, ref int64
			if err := app.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				var e error
				if got, e = store.New(tx).CountPendingPunchClaims(ctx, store.CountPendingPunchClaimsParams{TenantID: w.tenant, Cap: cap}); e != nil {
					return fmt.Errorf("the shipped count: %w", e)
				}
				return tx.QueryRow(ctx, pcPendingReference, w.tenant, cap).Scan(&ref)
			}); err != nil {
				t.Fatalf("%s, cap %d: %v", step, cap, err)
			}
			want := min(pending, int64(cap))
			if got != ref || got != want {
				t.Errorf("%s, cap %d: the shipped count is %d, the anti-join %d, the fixture leaves %d", step, cap, got, ref, want)
			}
		}
	}

	check("four claims, none decided (A)", a, 4)
	check("four claims, none decided (B)", b, 4)

	reject(a, ca[0])
	check("A rejected one (A)", a, 3)
	check("A rejected one (B is untouched)", b, 4)

	approve(a, ca[1])
	reject(b, cb[3])
	check("A approved one, B rejected one (A)", a, 2)
	check("A approved one, B rejected one (B)", b, 3)

	reject(a, ca[2])
	reject(a, ca[3])
	check("A decided all four (A)", a, 0)
	check("A decided all four (B)", b, 3)
}
