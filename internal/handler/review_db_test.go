package handler

// review_db_test.go -- M6-04's red-line proofs, against REAL Postgres, over REAL
// HTTP, with a real panel session.
//
// 🔴 WHY THESE CANNOT BE UNIT TESTS. The three rules this task touches are:
//
//	§4.3  a decision must not change the transaction
//	§4.5  tenant A must not be able to decide tenant B's record
//	§4.6  a refused decision must never be reported as a recorded one
//
// A fake proves none of them. It has no immutable table to leave alone, no second
// tenant to leak, and no UNIQUE constraint to lose a race against. §4.3 in
// particular is only observable by reading the row back out of the database and
// comparing it byte for byte with what was there before.
//
// EVERY TEST BELOW OPENS WITH A POSITIVE CONTROL, the shape internal/db/rls_test.go
// uses and says why: the panel is first shown to do the thing, so that "it did not
// do the forbidden thing" cannot be satisfied by a handler that does nothing at all.
//
// Fixtures are NOT cleaned up: transactions, transaction_reviews and audit_log are
// append-only and tappa_app holds no DELETE. That is the guarantee, not a leak.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/review"
)

// flaggedIDs returns the tenant's flagged transaction ids, newest first.
func flaggedIDs(t *testing.T, p *panelHarness, tenantID uuid.UUID) []uuid.UUID {
	t.Helper()
	var out []uuid.UUID
	err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT id FROM transactions
			 WHERE tenant_id = $1 AND verdict = 'flag'
			 ORDER BY occurred_at DESC, id DESC`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("reading flagged ids: %v", err)
	}
	return out
}

// verdictID returns one transaction id of the tenant with the given verdict,
// INSERTING one if the fixture did not write it.
//
// 🔴 IT TAKES THE VERDICT AS A PARAMETER BECAUSE THE FIRST VERSION HARD-CODED 'ok'
// AND PINNED ONE THIRD OF THE RULE. migration 00005's CHECK admits four verdicts
// and exactly one of them is reviewable; a test that drives only 'ok' leaves
// 'reject' and 'ignored' to whoever thinks to try them by hand (an audit did, and
// they held — but the suite did not say so).
func verdictID(t *testing.T, p *panelHarness, f panelFixture, verdict string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := p.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		e := tx.QueryRow(ctx,
			`SELECT id FROM transactions WHERE tenant_id = $1 AND verdict = $2 LIMIT 1`,
			f.tenantID, verdict).Scan(&id)
		if e == nil {
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		// reject and ignored carry NO direction (00005's CHECK) and may carry no
		// employee, so this row is written the way the engine would write one.
		return tx.QueryRow(ctx,
			`INSERT INTO transactions
			   (tenant_id, employee_id, location_id, tag_uid, ctr, occurred_at,
			    verdict, channel, practice, queued)
			 VALUES ($1, $2, $3, $4, 9999, $5, $6, 'nfc', false, false)
			 RETURNING id`,
			f.tenantID, f.employeeID, f.locationID, f.tagUID, dayOf().Add(time.Hour), verdict).Scan(&id)
	})
	if err != nil {
		t.Fatalf("getting a %q transaction: %v", verdict, err)
	}
	return id
}

// transactionSnapshot renders EVERY column of one transaction as text.
//
// 🔴 IT IS `SELECT t.*::text` RATHER THAN A COLUMN LIST, deliberately. A hand-written
// list is a list somebody has to maintain, and the one thing this comparison must
// notice is a column that changed -- including a column added tomorrow. Casting the
// whole row to text makes the check total by construction.
func transactionSnapshot(t *testing.T, p *panelHarness, tenantID, txnID uuid.UUID) string {
	t.Helper()
	var s string
	err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT t::text FROM transactions t WHERE t.tenant_id = $1 AND t.id = $2`,
			tenantID, txnID).Scan(&s)
	})
	if err != nil {
		t.Fatalf("snapshotting the transaction: %v", err)
	}
	return s
}

func countRows(t *testing.T, p *panelHarness, tenantID uuid.UUID, sql string, args ...any) int {
	t.Helper()
	var n int
	all := append([]any{tenantID}, args...)
	err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, sql, all...).Scan(&n)
	})
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	return n
}

func reviewCount(t *testing.T, p *panelHarness, tenantID, txnID uuid.UUID) int {
	return countRows(t, p, tenantID,
		`SELECT count(*) FROM transaction_reviews WHERE tenant_id = $1 AND transaction_id = $2`, txnID)
}

func reviewAuditCount(t *testing.T, p *panelHarness, tenantID, txnID uuid.UUID) int {
	return countRows(t, p, tenantID,
		`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2 AND target = $3`,
		review.Action, txnID.String())
}

// newReviewer builds the REAL domain writer over the harness's pool.
func newReviewer(t *testing.T, p *panelHarness, trail review.Trail) *review.Reviewer {
	t.Helper()
	r, err := review.NewReviewer(p.data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("review.NewReviewer: %v", err)
	}
	return r
}

// --- §4.3 -------------------------------------------------------------------

// TestReviewDB_ADecisionWritesTwoRowsAndChangesNoTransaction is the card's first
// criterion, measured.
func TestReviewDB_ADecisionWritesTwoRowsAndChangesNoTransaction(t *testing.T) {
	p := newPanelHarness(t)
	seedPanelTenant(t, p, p.tenantID, "Review", dayOf(), 4)
	p.signIn(t)

	flagged := flaggedIDs(t, p, p.tenantID)
	if len(flagged) == 0 {
		t.Fatal("the fixture wrote no flagged record, so every assertion below would be vacuous")
	}
	target := flagged[0]

	// POSITIVE CONTROL 1: the queue shows it before the decision.
	_, before := p.get(t, reviewHref)
	if !strings.Contains(before, target.String()) {
		t.Fatalf("the queue does not offer %s for decision; the POST below would be "+
			"testing a screen nobody could reach", target)
	}

	snapshot := transactionSnapshot(t, p, p.tenantID, target)
	if snapshot == "" {
		t.Fatal("the snapshot is empty, so the comparison at the end proves nothing")
	}

	res, _ := p.post(t, reviewHref, url.Values{
		"id": {target.String()}, "outcome": {"approved"}, "note": {"Maria was on the terrace"},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /admin/review answered %d, want 303", res.StatusCode)
	}

	// 🔴 THE TRANSACTION IS UNTOUCHED. Every column, compared as one string.
	if after := transactionSnapshot(t, p, p.tenantID, target); after != snapshot {
		t.Errorf("the transaction row CHANGED.\nbefore: %s\nafter:  %s\n"+
			"§4.3: transactions is immutable and a decision is a NEW ROW elsewhere.",
			snapshot, after)
	}

	// One review row, with the outcome, the reviewer and the note.
	var outcome, note string
	var reviewer uuid.UUID
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT outcome, reviewer_id, coalesce(note, '') FROM transaction_reviews
			 WHERE tenant_id = $1 AND transaction_id = $2`, p.tenantID, target).
			Scan(&outcome, &reviewer, &note)
	}); err != nil {
		t.Fatalf("reading the review row: %v", err)
	}
	switch {
	case outcome != "approved":
		t.Errorf("the review says %q, want approved", outcome)
	case reviewer != p.adminID:
		t.Errorf("the review names reviewer %s, want the signed-in admin %s", reviewer, p.adminID)
	case note != "Maria was on the terrace":
		t.Errorf("the review's note is %q", note)
	}

	// One audit row, and it names the transaction.
	if n := reviewAuditCount(t, p, p.tenantID, target); n != 1 {
		t.Errorf("audit_log holds %d %q row(s) for %s, want 1.\n"+
			"§4.3 says a correction is a new row PLUS audit_log; this task is the "+
			"first code in the product that writes the second half.", n, review.Action, target)
	}
	// 🔴 AND THE NOTE'S TEXT IS NOT IN THE TRAIL. detail carries has_note, not the
	// sentence: a manager's free text about a named person does not need a second
	// permanent copy in a table nobody can clean up (§4.7's spirit, 00005's rule).
	if n := countRows(t, p, p.tenantID,
		`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2 AND detail::text LIKE '%terrace%'`,
		review.Action); n != 0 {
		t.Errorf("%d audit row(s) carry the note's text in `detail`", n)
	}

	// 🔴 THE CONFIRMATION IS CHECKED AGAINST THE DATABASE, and this is the real-DB
	// half of that: following the redirect the handler produced must show the
	// banner, because the decision genuinely exists. The negative cases (an invented
	// id, no id, the wrong outcome) are driven against the fake in review_test.go —
	// this one proves the positive path is not simply dead.
	_, confirmed := p.get(t, res.Header.Get("Location"))
	if !strings.Contains(confirmed, "Recorded as approved") {
		t.Errorf("the redirect the handler issued does not confirm the decision it just "+
			"recorded.\nLocation: %s", res.Header.Get("Location"))
	}
	// AND A CONFIRMATION FOR A RECORD NOBODY DECIDED IS REFUSED, over real Postgres.
	_, invented := p.get(t, reviewHref+"?done=approved&for="+uuid.NewString())
	if strings.Contains(invented, "Recorded as approved") {
		t.Error("the panel confirmed a decision that is not in the database. The banner " +
			"was printable from a link until this was checked (measured: 0 review rows, " +
			"banner rendered).")
	}

	// The queue no longer offers it, and the day view says who decided it.
	_, queue := p.get(t, reviewHref)
	if strings.Contains(queue, target.String()) {
		t.Errorf("the queue still offers %s after it was decided", target)
	}
	_, day := p.get(t, "/admin?date="+dayParam(dayOf()))
	if !strings.Contains(day, "Approved by a manager") {
		t.Error("the transactions list does not read the decision back through the JOIN")
	}
	if !strings.Contains(day, "FLAGGED") {
		t.Error("the transactions list lost the FLAGGED stamp. The record keeps the " +
			"verdict the engine gave it; that is what makes §4.3 visible.")
	}
}

// TestReviewDB_TheQueueCountIsTheQueue -- the badge and the list must agree, and
// neither may see another tenant's work.
func TestReviewDB_TheQueueCountIsTheQueue(t *testing.T) {
	p := newPanelHarness(t)
	a := seedPanelTenant(t, p, p.tenantID, "CountA", dayOf(), 6) // 3 flagged
	other := uuid.New()
	seedTenantRow(t, p, other, "Count E2E B")
	seedPanelTenant(t, p, other, "CountB", dayOf(), 10) // 5 flagged, invisible here
	p.signIn(t)

	wantA := len(flaggedIDs(t, p, a.tenantID))
	if wantA == 0 {
		t.Fatal("tenant A has no flagged record; the count assertion would be vacuous")
	}
	if wantB := len(flaggedIDs(t, p, other)); wantB == 0 {
		t.Fatal("tenant B has no flagged record either, so 'B is not counted' proves nothing")
	}

	_, body := p.get(t, reviewHref)
	want := fmt.Sprintf("Waiting for review: %d", wantA)
	if !strings.Contains(body, want) {
		t.Errorf("the queue does not say %q.\nIt must count THIS tenant's flagged records "+
			"and no others.\nbody: %s", want, truncateForMsg(body))
	}
}

// --- §4.5 -------------------------------------------------------------------

// TestReviewDB_NeverDecidesAnotherTenantsRecord is §4.5 measured end to end AND
// defence by defence.
//
// 🔴 MEASURING THE THREE SEPARATELY IS THE POINT. A behavioural test can only ever
// observe their CONJUNCTION: as long as ONE of the query predicate, RLS and the
// composite FK holds, nothing leaks and every assertion passes -- which is exactly
// how M6-03 found its explicit tenant predicate to be deletable with the whole
// suite green. Each sub-test below removes the others and names which one refused.
func TestReviewDB_NeverDecidesAnotherTenantsRecord(t *testing.T) {
	p := newPanelHarness(t)
	mine := seedPanelTenant(t, p, p.tenantID, "Mine", dayOf(), 4)
	other := uuid.New()
	seedTenantRow(t, p, other, "Foreign E2E Ltd")
	seedPanelTenant(t, p, other, "Theirs", dayOf(), 4)
	p.signIn(t)

	myFlag := flaggedIDs(t, p, mine.tenantID)[0]
	theirFlag := flaggedIDs(t, p, other)[0]

	// POSITIVE CONTROL: the same request shape SUCCEEDS on my own record. Without
	// it, a handler that refuses everything satisfies every assertion below.
	res, _ := p.post(t, reviewHref, url.Values{
		"id": {myFlag.String()}, "outcome": {"approved"},
	})
	if res.StatusCode != http.StatusSeeOther || reviewCount(t, p, p.tenantID, myFlag) != 1 {
		t.Fatalf("the control decision failed (%d, %d row(s)); the refusals below would "+
			"be vacuous", res.StatusCode, reviewCount(t, p, p.tenantID, myFlag))
	}

	t.Run("through the panel, all three defences in place", func(t *testing.T) {
		res, _ := p.post(t, reviewHref, url.Values{
			"id": {theirFlag.String()}, "outcome": {"approved"},
		})
		if res.StatusCode != http.StatusSeeOther {
			t.Errorf("a cross-tenant decision answered %d", res.StatusCode)
		}
		if n := reviewCount(t, p, other, theirFlag); n != 0 {
			t.Errorf("%d review row(s) were written against another tenant's record", n)
		}
		if n := reviewAuditCount(t, p, other, theirFlag); n != 0 {
			t.Errorf("%d audit row(s) were written into another tenant's trail", n)
		}
	})

	t.Run("the query predicate alone: RLS and the FK cannot see the mismatch", func(t *testing.T) {
		// The shipped query, run in MY tenant context, asked for THEIR id. RLS does
		// not refuse it (the SELECT simply matches no visible row) and the FK is never
		// reached, so what stops it here is the `t.tenant_id = @tenant_id` predicate
		// plus the fact that the SELECT produces nothing.
		var got uuid.UUID
		err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome)
				 SELECT $1, t.id, $2, 'approved' FROM transactions t
				 WHERE t.tenant_id = $1 AND t.id = $3 AND t.verdict = 'flag'
				 RETURNING id`, p.tenantID, p.adminID, theirFlag).Scan(&got)
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("the shipped shape answered %v, want pgx.ErrNoRows -- the SELECT must "+
				"produce nothing for a foreign id", err)
		}
	})

	t.Run("RLS alone: measured with NO tenant predicate at all", func(t *testing.T) {
		// 🔴 RLS IS MEASURED WITH A SELECT RATHER THAN WITH AN INSERT, and the reason
		// is the ordering finding recorded in the sub-test below: no INSERT can reach
		// the RLS WITH CHECK on transaction_reviews, because the BEFORE INSERT trigger
		// gets there first. What CAN be measured directly is RLS's own contribution --
		// a query carrying no tenant predicate whatsoever cannot see the other
		// tenant's row from inside my transaction, which is precisely the second
		// defence §4.5 asks for.
		var theirs, mine int
		if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			if e := tx.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE id = $1`, theirFlag).Scan(&theirs); e != nil {
				return e
			}
			return tx.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE id = $1`, myFlag).Scan(&mine)
		}); err != nil {
			t.Fatalf("probing: %v", err)
		}
		// POSITIVE CONTROL: the same predicate-free query DOES see my own row, so the
		// zero above is RLS rather than an empty table or a broken probe.
		if mine != 1 {
			t.Fatalf("a predicate-free lookup found %d of my OWN rows; the probe is not "+
				"reading anything and the zero below would prove nothing", mine)
		}
		if theirs != 0 {
			t.Errorf("a predicate-free lookup inside my tenant found %d row(s) of another "+
				"tenant's. RLS is the defence that must make this zero even when a query "+
				"forgets its own predicate.", theirs)
		}
	})

	t.Run("the trigger fires FIRST, and the composite FK is shadowed by it", func(t *testing.T) {
		// 🔴 THIS SUB-TEST RECORDS A MEASURED ORDERING RATHER THAN ASSERTING A LAYER,
		// because the first version of it asserted the wrong one and the database said
		// so. Both cross-tenant INSERT shapes are refused by migration 00005's BEFORE
		// INSERT trigger, whose own SELECT is tenant-scoped AND runs under RLS:
		//
		//	tenant_id = THEIRS, transaction_id = THEIRS
		//	  the trigger looks up (id, tenant_id) and RLS hides the row from my
		//	  transaction -> "review target transaction ... not found in tenant ..."
		//	  (SQLSTATE 23503). The RLS WITH CHECK never runs.
		//	tenant_id = MINE,   transaction_id = THEIRS
		//	  the same lookup, same message. transaction_reviews_txn_fk never runs.
		//
		// SO THE COMPOSITE FK IS NOT OBSERVABLE THROUGH ANY REACHABLE INSERT -- but it
		// IS real and it HAS been seen firing. A security audit disabled the trigger
		// inside a rolled-back transaction and got
		// `violates foreign key constraint "transaction_reviews_txn_fk"`, which is the
		// defence that survives if the trigger is ever turned off (00005 admits a
		// superuser can). It is not asserted HERE because doing so means a DDL
		// mutation on the shared development database, which this suite does not take.
		for _, shape := range []struct {
			name   string
			tenant uuid.UUID
		}{
			{"tenant column names THEM", other},
			{"tenant column names ME", p.tenantID},
		} {
			err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx,
					`INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome)
					 VALUES ($1, $2, $3, 'approved')`, shape.tenant, theirFlag, p.adminID)
				return e
			})
			if err == nil {
				t.Errorf("%s: a review row was written against another tenant's transaction "+
					"with no query predicate at all", shape.name)
				continue
			}
			if !strings.Contains(err.Error(), "not found in tenant") {
				t.Errorf("%s: the refusal is not the trigger's: %v.\n"+
					"If this changed, the ordering above changed with it and the comment "+
					"needs re-measuring rather than the test needs loosening.", shape.name, err)
			}
		}
	})
}

// --- only a flag is reviewable ----------------------------------------------

// TestReviewDB_OnlyFlaggedRecordsAreReviewable. Without this predicate the flow is
// an arbitrary RELABELLING CHANNEL: post the id of a trust-100 'ok' record, have it
// recorded as rejected, and erase somebody's hours while §4.3 looks intact.
func TestReviewDB_OnlyFlaggedRecordsAreReviewable(t *testing.T) {
	p := newPanelHarness(t)
	fixture := seedPanelTenant(t, p, p.tenantID, "OnlyFlag", dayOf(), 4)
	p.signIn(t)

	good := flaggedIDs(t, p, p.tenantID)[0]

	// POSITIVE CONTROL first.
	if res, _ := p.post(t, reviewHref, url.Values{
		"id": {good.String()}, "outcome": {"rejected"},
	}); res.StatusCode != http.StatusSeeOther || reviewCount(t, p, p.tenantID, good) != 1 {
		t.Fatalf("the control decision on a flagged record failed (%d)", res.StatusCode)
	}

	// 🔴 THE LIST IS DERIVED, NOT TYPED, AND THAT IS THE FIX FOR A GAP THIS TEST HAD.
	// It drove 'ok' alone at first, leaving 'reject' and 'ignored' unpinned -- and the
	// dangerous one is not 'ok': rewriting a 'reject' as approved ADDS paid hours the
	// engine refused. Writing the three out by hand does not close that, because a
	// hand-written table cannot notice itself shrinking: measured, cutting it back to
	// {"ok"} took the sub-test count from 6 to 2 and FAILED NOTHING.
	//
	// So the set comes from panelVerdicts -- the same closed vocabulary the filter bar
	// renders and the handler validates against, which is migration 00005's CHECK --
	// minus the one verdict that IS reviewable. A fifth verdict added to the schema
	// arrives here automatically; removing one from the loop is no longer possible
	// without removing it from the product.
	var nonFlag []string
	for _, v := range panelVerdicts {
		if v.Value != "flag" {
			nonFlag = append(nonFlag, v.Value)
		}
	}
	if len(nonFlag) != len(panelVerdicts)-1 || len(nonFlag) < 2 {
		t.Fatalf("derived %d non-flag verdict(s) from %d in panelVerdicts (%v); the loop "+
			"below would run over the wrong set", len(nonFlag), len(panelVerdicts), nonFlag)
	}
	for _, verdict := range nonFlag {
		notFlagged := verdictID(t, p, fixture, verdict)

		t.Run("through the panel: "+verdict, func(t *testing.T) {
			res, _ := p.post(t, reviewHref, url.Values{
				"id": {notFlagged.String()}, "outcome": {"approved"},
			})
			if res.StatusCode != http.StatusSeeOther {
				t.Errorf("reviewing a %q record answered %d, want 303 (a refusal, not a 500)",
					verdict, res.StatusCode)
			}
			if loc := res.Header.Get("Location"); !strings.Contains(loc, "problem=gone") {
				t.Errorf("%q: the refusal redirected to %q; §4.6 says it must be told",
					verdict, loc)
			}
			if n := reviewCount(t, p, p.tenantID, notFlagged); n != 0 {
				t.Errorf("%d review row(s) exist against a %q record. Without this "+
					"predicate the flow is an arbitrary RELABELLING CHANNEL.", n, verdict)
			}
		})

		t.Run("the trigger alone: "+verdict, func(t *testing.T) {
			// This is what stops it if somebody rewrites the query as an INSERT ... VALUES.
			err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
				_, e := tx.Exec(ctx,
					`INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome)
					 VALUES ($1, $2, $3, 'approved')`, p.tenantID, notFlagged, p.adminID)
				return e
			})
			if err == nil {
				t.Fatalf("a review row was written against a %q record with no predicate. "+
					"migration 00005's tappa_check_transaction_review is what must refuse it.",
					verdict)
			}
			if !strings.Contains(err.Error(), "only flag transactions are reviewable") {
				t.Errorf("%q: the refusal did not come from the trigger: %v", verdict, err)
			}
		})
	}
}

// --- §4.6 and the race ------------------------------------------------------

// TestReviewDB_ConcurrentDecisionsProduceExactlyOneWinner.
//
// 🔴 IT IS THE §4.3-SHAPED QUESTION ON A DIFFERENT TABLE: UNIQUE (transaction_id)
// exists so that a record is decided ONCE, and a read-then-write would lose it the
// same way §4.4 says a counter check loses. Here there is no read-then-write -- the
// uniqueness IS the check -- so what this measures is that the LOSERS are told
// rather than silently treated as winners (§4.6: no silent approval).
//
// TEN GOROUTINES, NOT FIFTY. internal/db/invites_test.go and
// internal/sun/advance_test.go open 54 connections each and the suite already sits
// at Postgres's 97 usable slots (recorded as an infrastructure limit); this harness
// additionally caps its pool at 4, so ten callers contend for four connections and
// the race is real without adding to that pressure.
func TestReviewDB_ConcurrentDecisionsProduceExactlyOneWinner(t *testing.T) {
	p := newPanelHarness(t)
	seedPanelTenant(t, p, p.tenantID, "Race", dayOf(), 4)

	target := flaggedIDs(t, p, p.tenantID)[0]
	trail, err := audit.New(p.data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	reviewer := newReviewer(t, p, trail)

	// 🔴 EACH RACER IS A DIFFERENT MANAGER, which is the scenario the constraint
	// exists for and is also what makes the "who decided it" answer meaningful: with
	// one shared reviewer id every loser would be told "you already decided this",
	// and the two-managers branch would go unmeasured.
	const racers = 10
	reviewers := make([]uuid.UUID, racers)
	for i := range reviewers {
		reviewers[i] = uuid.New()
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]error, racers)
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = reviewer.Record(context.Background(), review.Decision{
				TenantID:      p.tenantID,
				TransactionID: target,
				ReviewerID:    reviewers[i],
				Outcome:       review.Approved,
			})
		}(i)
	}
	close(start)
	wg.Wait()

	winners, taken, other, misattributed := 0, 0, 0, 0
	for _, err := range results {
		var decided *review.DecidedError
		switch {
		case err == nil:
			winners++
		case errors.Is(err, review.ErrAlreadyDecided):
			taken++
			// §4.6 ON THE SUBJECT: a loser here really was beaten by SOMEBODY ELSE,
			// and the error must say so rather than accusing them of their own click.
			if errors.As(err, &decided) && decided.ByYou {
				misattributed++
			}
		default:
			other++
			t.Errorf("a racer got an unexpected error: %v", err)
		}
	}
	if misattributed != 0 {
		t.Errorf("%d loser(s) were told THEY had already decided the record, but every "+
			"racer here is a different manager", misattributed)
	}
	if winners != 1 {
		t.Errorf("%d of %d racers were told their decision was recorded, want exactly 1",
			winners, racers)
	}
	if taken != racers-1 {
		t.Errorf("%d racers were told the record was already decided, want %d.\n"+
			"§4.6: a loser must not be left believing they decided it.", taken, racers-1)
	}
	if other != 0 {
		t.Errorf("%d racers got a 500-shaped error; the loser of a race must get a "+
			"sentence, not a server failure", other)
	}
	if n := reviewCount(t, p, p.tenantID, target); n != 1 {
		t.Errorf("%d review row(s) exist for one record, want 1", n)
	}
	if n := reviewAuditCount(t, p, p.tenantID, target); n != 1 {
		t.Errorf("%d audit row(s) exist for one decision, want 1.\n"+
			"A losing racer that still wrote a trail row would put a decision in the "+
			"audit that never happened.", n)
	}
}

// --- the note: rendered, escaped, and never cut in silence ------------------

// hostileNote is stored verbatim and must come back escaped.
//
// EVERY CHARACTER IN IT IS THERE FOR A REASON: the tag opens a script, the quote
// would break out of an attribute, the ampersand is what a double-escape would
// mangle, and the Maltese ħ is the ordinary case this product ships a latin-ext
// font subset for.
const hostileNote = `Maria & "the boss" said <script>alert(1)</script> — ħadd ma kien hemm`

// TestReviewDB_AHostileNoteIsEscapedWhereItIsRendered.
//
// 🔴 IT PROVES THE SENTENCE THREE COMMENTS HAVE NOW CLAIMED. Two earlier versions
// of internal/handler/review.go's reviewNote block described the note's escaping,
// and both described a system that did not exist — first a renderer that had not
// been written, then a write-only field that the user then decided to render. A
// claim about escaping is worth exactly as much as the markup it was read out of,
// so this stores the note through the real POST and reads the real page.
func TestReviewDB_AHostileNoteIsEscapedWhereItIsRendered(t *testing.T) {
	p := newPanelHarness(t)
	seedPanelTenant(t, p, p.tenantID, "Escape", dayOf(), 4)
	p.signIn(t)

	target := flaggedIDs(t, p, p.tenantID)[0]
	res, _ := p.post(t, reviewHref, url.Values{
		"id": {target.String()}, "outcome": {"approved"}, "note": {hostileNote},
	})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST answered %d, want 303", res.StatusCode)
	}
	if loc := res.Header.Get("Location"); strings.Contains(loc, "clipped") {
		t.Fatalf("a short note was reported as clipped: %q", loc)
	}

	// IT REALLY IS IN THE DATABASE, VERBATIM. Without this the assertions below
	// could be satisfied by a boundary that had quietly thrown the note away.
	var stored string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT coalesce(note, '') FROM transaction_reviews
			  WHERE tenant_id = $1 AND transaction_id = $2`, p.tenantID, target).Scan(&stored)
	}); err != nil {
		t.Fatalf("reading the stored note: %v", err)
	}
	if stored != hostileNote {
		t.Fatalf("the stored note is %q; the boundary cleans, it must not escape —\n"+
			"escaping there is how an escape gets stored and shown back (M6-03 shipped "+
			"that once on the name filter)", stored)
	}

	_, day := p.get(t, "/admin?date="+dayParam(dayOf()))

	// 🔴 THE ASSERTIONS ARE SCOPED TO THE CARD, NOT TO THE PAGE, AND THE FIRST
	// VERSION WAS NOT — it searched the whole document for "</script>" and failed on
	// the transactions section's own <script src="/static/vendor/htmx.min.js">. That
	// is a legitimate tag this section has loaded since M6-03, so a page-wide scan
	// measures the wrong thing in the alarming direction. What has to be free of
	// markup is the note, so the note's own card is the unit.
	card := ""
	for _, c := range strings.Split(day, "<article") {
		if strings.Contains(c, "Manager") && strings.Contains(c, "add ma kien hemm") {
			card = c
			break
		}
	}
	// POSITIVE CONTROL: the note is actually ON the page, inside a docket. Every
	// "the raw form is absent" check below is vacuous over a page that renders no
	// note at all — which is exactly the state this task shipped in for one round.
	if card == "" {
		t.Fatalf("no docket on the transactions page carries the manager's note; the "+
			"escaping assertions below would pass over nothing.\npage: %s",
			truncateForMsg(day))
	}
	if !strings.Contains(card, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Errorf("the note's script tag does not appear in its ESCAPED form inside the "+
			"card.\ncard: %s", truncateForMsg(card))
	}
	for _, raw := range []string{"<script", "</script>", `"the boss"`} {
		if strings.Contains(card, raw) {
			t.Errorf("the docket contains %q verbatim — the note reaches the browser as "+
				"markup rather than as text", raw)
		}
	}
	// The ampersand must be escaped ONCE, not twice: &amp;amp; is what a boundary
	// that escaped and a renderer that escaped again would produce.
	if strings.Contains(card, "&amp;amp;") {
		t.Error("the note is double-escaped; the manager reads &amp; where they wrote &")
	}
	if !strings.Contains(card, "ħadd ma kien hemm") {
		t.Error("the Maltese characters did not survive the round trip")
	}

	// AND THE PAGE'S OWN SCRIPT IS STILL THE ONLY ONE, which is what makes the
	// card-scoped check above safe rather than merely narrower: a note that DID
	// inject a tag would add a second <script to the document.
	if n := strings.Count(day, "<script"); n != 1 {
		t.Errorf("the transactions page carries %d <script tags, want exactly 1 (the "+
			"vendored htmx). A note that injected markup would show up here.", n)
	}
}

// TestReviewDB_ALongNoteIsCutAndTheManagerIsTOLD is the other half of the user
// decision (2026-08-06), and it is the half that decision was actually made for.
//
// 🔴 THE SILENT VERSION WAS THE DEFECT. maxReviewNote cut at 500 characters, the
// note was rendered nowhere, and the confirmation said "Recorded as approved" — so
// a manager could lose most of what they wrote with no signal anywhere in the
// product. Both halves of the fix are measured here: they are TOLD, and they can
// SEE what was kept.
func TestReviewDB_ALongNoteIsCutAndTheManagerIsTold(t *testing.T) {
	p := newPanelHarness(t)
	seedPanelTenant(t, p, p.tenantID, "Clip", dayOf(), 4)
	p.signIn(t)

	target := flaggedIDs(t, p, p.tenantID)[0]
	// 501 characters: one over, so the boundary is measured at its edge rather than
	// somewhere comfortably past it.
	const tail = "THIS-TAIL-IS-CUT"
	long := strings.Repeat("m", maxReviewNote-len(tail)+1) + tail
	if n := len([]rune(long)); n != maxReviewNote+1 {
		t.Fatalf("the probe note is %d runes, want %d", n, maxReviewNote+1)
	}

	res, _ := p.post(t, reviewHref, url.Values{
		"id": {target.String()}, "outcome": {"approved"}, "note": {long},
	})
	loc := res.Header.Get("Location")
	if !strings.Contains(loc, "clipped=1") {
		t.Fatalf("a %d-character note was cut and the redirect says nothing: %q",
			len([]rune(long)), loc)
	}

	_, page := p.get(t, loc)
	for _, want := range []string{"longer than 500 characters", "only the first 500 were"} {
		if !strings.Contains(page, want) {
			t.Errorf("the confirmation does not say %q.\npage: %s", want, truncateForMsg(page))
		}
	}

	// AND WHAT WAS KEPT IS READABLE, which is the half that makes the sentence
	// actionable rather than merely apologetic.
	_, day := p.get(t, "/admin?date="+dayParam(dayOf()))
	kept := strings.Repeat("m", 40)
	if !strings.Contains(day, kept) {
		t.Error("the stored note is not rendered on the record, so the manager is told " +
			"text was cut and cannot see what survived")
	}
	if strings.Contains(day, tail) {
		t.Errorf("the tail %q was stored after all; the cut did not happen and the "+
			"warning is false", tail)
	}

	// AND THE STORED VALUE IS THE CUT ONE, so the warning describes the database
	// rather than the screen.
	var stored string
	if err := p.data.WithTenant(context.Background(), p.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT coalesce(note, '') FROM transaction_reviews
			  WHERE tenant_id = $1 AND transaction_id = $2`, p.tenantID, target).Scan(&stored)
	}); err != nil {
		t.Fatalf("reading the stored note: %v", err)
	}
	if n := len([]rune(stored)); n != maxReviewNote {
		t.Errorf("the database holds %d characters, want %d", n, maxReviewNote)
	}
}

// --- §4.6: the refusal names the RIGHT person -------------------------------

// TestReviewDB_ADoubleClickIsNotBlamedOnSomebodyElse.
//
// 🔴 THE FIRST VERSION OF THIS SCREEN INVENTED THE SUBJECT OF ITS OWN SENTENCE. A
// manager double-clicks Approve: the first POST records the decision, the second
// hits UNIQUE (transaction_id) — and the screen said "somebody else decided that
// record first … nothing was written for yours". Every word about the WRITE was
// true and the word about the WHO was made up, about the one person it was
// certainly wrong about. This is the class M5-11 closed, and the diff's own
// concurrency test was producing the behaviour.
//
// BOTH BRANCHES ARE MEASURED HERE, against the real database, because a fake
// cannot produce a unique violation and a test that only drove the "somebody else"
// arm would have passed on the broken code.
func TestReviewDB_ADoubleClickIsNotBlamedOnSomebodyElse(t *testing.T) {
	p := newPanelHarness(t)
	seedPanelTenant(t, p, p.tenantID, "Double", dayOf(), 6)
	flagged := flaggedIDs(t, p, p.tenantID)
	if len(flagged) < 2 {
		t.Fatalf("the fixture wrote %d flagged record(s); this test needs two", len(flagged))
	}
	trail, err := audit.New(p.data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	r := newReviewer(t, p, trail)

	decide := func(target, reviewer uuid.UUID, outcome review.Outcome) error {
		return r.Record(context.Background(), review.Decision{
			TenantID: p.tenantID, TransactionID: target,
			ReviewerID: reviewer, Outcome: outcome,
		})
	}

	t.Run("the same manager twice: it was YOURS, and it says which", func(t *testing.T) {
		target := flagged[0]
		if err := decide(target, p.adminID, review.Rejected); err != nil {
			t.Fatalf("the first decision failed: %v", err)
		}
		err := decide(target, p.adminID, review.Approved)

		var decided *review.DecidedError
		if !errors.As(err, &decided) {
			t.Fatalf("the second decision answered %v; a caller cannot tell whose "+
				"decision is on record", err)
		}
		if !errors.Is(err, review.ErrAlreadyDecided) {
			t.Error("the richer error no longer satisfies errors.Is(ErrAlreadyDecided); " +
				"every caller that does not care who decided it would break")
		}
		if !decided.ByYou {
			t.Error("a manager's own second click is reported as somebody else's " +
				"decision. That is the sentence this test exists to stop.")
		}
		// AND IT NAMES THE DECISION THAT IS ACTUALLY ON RECORD — the FIRST one
		// (rejected), not the one just attempted (approved). Reporting the attempt
		// back would be a second fabricated fact.
		if decided.Outcome != review.Rejected {
			t.Errorf("the refusal says the record is %q; it was decided as %q and the "+
				"second press asked for %q", decided.Outcome, review.Rejected, review.Approved)
		}
	})

	t.Run("a different manager: it was SOMEBODY ELSE", func(t *testing.T) {
		target := flagged[1]
		other := uuid.New()
		if err := decide(target, other, review.Approved); err != nil {
			t.Fatalf("the first decision failed: %v", err)
		}
		err := decide(target, p.adminID, review.Approved)

		var decided *review.DecidedError
		if !errors.As(err, &decided) {
			t.Fatalf("the second decision answered %v", err)
		}
		if decided.ByYou {
			t.Error("a decision made by another manager is reported as the caller's own. " +
				"The fix for the double-click case must not invert the race case.")
		}
		if decided.Outcome != review.Approved {
			t.Errorf("the refusal says %q, want approved", decided.Outcome)
		}
	})
}

// --- the two rows share a fate ----------------------------------------------

// failingTrail refuses to write, without touching the transaction.
type failingTrail struct{ calls int }

func (f *failingTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	f.calls++
	return uuid.Nil, errors.New("the audit trail is unavailable")
}

// TestReviewDB_TheReviewRowAndTheAuditRowShareAFate, measured in BOTH directions.
//
// 🔴 THE TWO FAILURES ARE DIFFERENT LIES AND BOTH ARE UNACCEPTABLE:
//
//	review written, audit missing  -> a decision with no trail, which is exactly the
//	                                  half of §4.3 nothing in this product had built.
//	audit written, review missing  -> a trail claiming a decision the queue still
//	                                  shows as waiting.
func TestReviewDB_TheReviewRowAndTheAuditRowShareAFate(t *testing.T) {
	p := newPanelHarness(t)
	seedPanelTenant(t, p, p.tenantID, "Fate", dayOf(), 6)
	flagged := flaggedIDs(t, p, p.tenantID)
	if len(flagged) < 2 {
		t.Fatalf("the fixture wrote %d flagged record(s); this test needs two", len(flagged))
	}

	realTrail, err := audit.New(p.data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}

	t.Run("the audit write fails: NO review row survives", func(t *testing.T) {
		target := flagged[0]
		broken := &failingTrail{}
		r := newReviewer(t, p, broken)

		err := r.Record(context.Background(), review.Decision{
			TenantID: p.tenantID, TransactionID: target,
			ReviewerID: p.adminID, Outcome: review.Approved,
		})
		if err == nil {
			t.Fatal("the decision reported success while its trail could not be written")
		}
		if broken.calls != 1 {
			t.Fatalf("the trail was called %d time(s); the fake never ran and this "+
				"measures nothing", broken.calls)
		}
		if n := reviewCount(t, p, p.tenantID, target); n != 0 {
			t.Errorf("%d review row(s) survived a failed audit write. The two must share "+
				"one transaction: a decision with no trail is the half of §4.3 that had "+
				"never been built.", n)
		}
		// POSITIVE CONTROL: the SAME record decides cleanly once the trail works, so
		// the zero above is the rollback rather than a record that was never
		// reviewable in the first place.
		if err := newReviewer(t, p, realTrail).Record(context.Background(), review.Decision{
			TenantID: p.tenantID, TransactionID: target,
			ReviewerID: p.adminID, Outcome: review.Approved,
		}); err != nil {
			t.Fatalf("the control decision on the same record failed: %v", err)
		}
		if n := reviewCount(t, p, p.tenantID, target); n != 1 {
			t.Errorf("the control wrote %d review row(s), want 1", n)
		}
	})

	t.Run("the review write fails: NO audit row survives", func(t *testing.T) {
		target := flagged[1]
		r := newReviewer(t, p, realTrail)

		// First decision succeeds; the second is refused by UNIQUE (transaction_id).
		if err := r.Record(context.Background(), review.Decision{
			TenantID: p.tenantID, TransactionID: target,
			ReviewerID: p.adminID, Outcome: review.Approved,
		}); err != nil {
			t.Fatalf("the first decision failed: %v", err)
		}
		if n := reviewAuditCount(t, p, p.tenantID, target); n != 1 {
			t.Fatalf("the first decision wrote %d audit row(s), want 1 -- the count below "+
				"is measured against this one", n)
		}
		err := r.Record(context.Background(), review.Decision{
			TenantID: p.tenantID, TransactionID: target,
			ReviewerID: p.adminID, Outcome: review.Rejected,
		})
		if !errors.Is(err, review.ErrAlreadyDecided) {
			t.Fatalf("the second decision answered %v, want ErrAlreadyDecided", err)
		}
		if n := reviewAuditCount(t, p, p.tenantID, target); n != 1 {
			t.Errorf("audit_log holds %d row(s) for this record, want 1. A refused "+
				"decision that still wrote a trail row would put a rejection in the "+
				"audit that never happened -- and audit_log cannot be cleaned up.", n)
		}
	})
}

// seedTenantRow inserts a bare tenant so a second fixture has somewhere to live.
func seedTenantRow(t *testing.T, p *panelHarness, tenantID uuid.UUID, name string) {
	t.Helper()
	if err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, $2, $3, 'bar', 'single')`,
			tenantID, name, "VAT-"+tenantID.String()[:8])
		return e
	}); err != nil {
		t.Fatalf("insert tenant %s: %v", name, err)
	}
}
