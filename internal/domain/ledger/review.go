package ledger

// review.go -- the READ side of the FLAGGED approval queue (M6-04).
//
// IT IS IN THIS PACKAGE AND THE WRITE SIDE IS NOT, which is a deliberate split
// rather than an arbitrary one. This file adds two SELECTs and nothing else, so
// the package's opening claim -- "there is no store call in this package that is
// not a SELECT" -- stays literally true and greppable. The INSERT that records a
// decision lives in internal/domain/review, where it can be read on its own.
//
// 🔴 AND THE SPLIT IS WHAT KEEPS THE §4.5 NET OVER THESE QUERIES. query_test.go
// derives what it checks by PARSING this package with go/ast and collecting every
// selector whose name is a query db/queries declares -- so `q.X(...)`,
// `store.New(tx).X(...)` (which Decision below uses) and a method taken as a value
// are all inside it. It used to scan for the text `q.X(`, which made the net depend
// on a variable name and missed exactly the shape Decision writes; that is recorded
// where the derivation lives.

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// PendingCap is how far CountPendingFlagged counts before it stops.
//
// 🔴 IT EXISTS BECAUSE AN EXACT COUNT IS A SEQUENTIAL SCAN, and this number is
// charged to EVERY panel page rather than to one screen: the badge sits in the
// navigation, so it is read whichever section a manager opens. Measured on the
// development database (EXPLAIN ANALYZE, seed tenant, 21 419 records of which
// 4 634 flagged, warm cache, after ANALYZE, 3 repeats):
//
//	exact count(*), no cap        59.4 / 63.6 / 66.4 ms   (parallel seq scan)
//	capped at 100, queue full      0.7 /  1.0 /  7.1 ms   (index scan, stops early)
//	capped at 100, queue EMPTY    17.8 / 20.3 ms          (index scan to the end)
//
// The third row is the one that sizes this constant, and it is the STEADY STATE of
// a panel somebody keeps clear: with nothing pending the cap never fills, so the
// scan runs to the end of the tenant's timeline whatever the cap is. Lowering the
// number does not help that case and raising it only costs in the first.
//
// 100 IS ALSO A UI DECISION: past it the badge says "100+", which is true, and a
// manager with more than a hundred flagged records does not need the exact figure
// to know the queue needs work. Nothing is hidden -- the list pages through all of
// them.
const PendingCap = 100

// QueuePageSize is how many records one page of the queue returns.
//
// It is PageSize's sibling and deliberately the same number: a page of the queue is
// the same reading task as a page of the day, and two different page sizes in one
// panel is a difference a reader has to learn for no reason.
const QueuePageSize = PageSize

// Pending is the size of the approval queue AS FAR AS IT WAS COUNTED.
//
// 🔴 Capped IS NOT A DISPLAY DETAIL, IT IS THE HONESTY OF THE NUMBER. N is exact
// when Capped is false and a FLOOR when it is true, and a caller that renders N
// without looking at Capped would print "100" for a queue of nine thousand. The two
// fields travel together so the distinction cannot be dropped by accident.
type Pending struct {
	N      int
	Capped bool
}

// Pending counts the flagged records that have no decision yet.
//
// A FAILED COUNT IS AN ERROR, NEVER A ZERO. Returning Pending{} on failure would
// tell every manager that nothing is waiting, which is the §4.6 class M5-11 closed:
// the product may not state something it has not measured. The caller decides what
// an unknown count looks like on screen; this returns the error.
func (r *Reader) Pending(ctx context.Context, tenantID uuid.UUID) (Pending, error) {
	var out Pending
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		n, err := q.CountPendingFlagged(ctx, store.CountPendingFlaggedParams{
			TenantID: tenantID,
			// ONE MORE THAN THE CAP, so "is there a hundred and first" is answered by
			// the same query rather than guessed from equality. Counting exactly
			// PendingCap cannot tell a queue of exactly 100 from a queue of 9 000.
			Cap: int32(PendingCap) + 1,
		})
		if err != nil {
			return fmt.Errorf("count pending flagged: %w", err)
		}
		out = Pending{N: int(n)}
		if out.N > PendingCap {
			out = Pending{N: PendingCap, Capped: true}
		}
		return nil
	})
	if err != nil {
		return Pending{}, fmt.Errorf("ledger: pending: %w", err)
	}
	return out, nil
}

// Decision reports what a record was decided as, or ok=false if it carries no
// decision.
//
// 🔴 IT EXISTS SO A CONFIRMATION CAN BE A MEASUREMENT RATHER THAN A CLAIM. The panel
// used to print "Recorded as approved" from the query string alone, so
// `GET /admin/review?done=approved` rendered a confirmation with ZERO review rows in
// the database (measured by an audit: before 0, after 0) and the sentence was
// sendable as a link. No XSS and no lost record -- the vocabulary is closed and the
// queue below it contradicted the banner -- but the product was stating something it
// had not looked up, which is the class M5-11 closed.
//
// IT ADDS NO SQL: GetTransactionReview already exists for internal/domain/review's
// "who decided this" answer, and this is the second reader of it.
func (r *Reader) Decision(ctx context.Context, tenantID, transactionID uuid.UUID) (outcome string, ok bool, err error) {
	e := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetTransactionReview(ctx, store.GetTransactionReviewParams{
			TenantID:      tenantID,
			TransactionID: transactionID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read the decision: %w", err)
		}
		outcome, ok = row.Outcome, true
		return nil
	})
	if e != nil {
		return "", false, fmt.Errorf("ledger: decision: %w", e)
	}
	return outcome, ok, nil
}

// QueuePage is one page of the approval queue.
type QueuePage struct {
	// Queried is the anti-fabrication flag, the same one Page carries and for the
	// same reason: "nothing is waiting" is a claim about the world, and a zero
	// QueuePage renders identically to an empty queue unless something separates
	// them.
	Queried bool
	Records []Record
	// Next is the cursor for the following page, nil when this is the last one.
	Next *Cursor
	// Zone is the tenant's timezone. Every instant below is UTC and is converted at
	// render (CLAUDE.md §6).
	Zone *time.Location
}

// Queue loads one page of flagged records that are still waiting for a decision.
//
// ONE TRANSACTION, TWO QUERIES, the same shape as Day: the tenant's zone has to
// come from the same snapshot as the rows, or a page could render its times against
// a zone that changed between the two round trips.
func (r *Reader) Queue(ctx context.Context, tenantID uuid.UUID, after *Cursor) (QueuePage, error) {
	var out QueuePage
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		clock, err := q.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load tenant clock: %w", err)
		}
		params := store.ListFlaggedForReviewParams{
			TenantID: tenantID,
			PageSize: QueuePageSize + 1, // one extra: see below
		}
		if after != nil {
			at, id := after.At, after.ID
			params.CursorAt = &at
			params.CursorID = &id
		}
		rows, err := q.ListFlaggedForReview(ctx, params)
		if err != nil {
			return fmt.Errorf("list flagged for review: %w", err)
		}

		// THE EXTRA ROW IS DROPPED, which answers "is there another page" without a
		// second count -- the same trick page() uses, and it matters more here: the
		// queue SHRINKS as decisions are made, so a count taken now would be stale by
		// the time the next page is asked for.
		out = QueuePage{Queried: true, Zone: r.loadZone(clock.Timezone)}
		more := len(rows) > QueuePageSize
		if more {
			rows = rows[:QueuePageSize]
		}
		out.Records = make([]Record, 0, len(rows))
		for _, row := range rows {
			out.Records = append(out.Records, queueRecord(row))
		}
		if more && len(out.Records) > 0 {
			last := out.Records[len(out.Records)-1]
			out.Next = &Cursor{At: last.OccurredAt, ID: last.ID}
		}
		return nil
	})
	if err != nil {
		return QueuePage{}, fmt.Errorf("ledger: queue: %w", err)
	}
	return out, nil
}

// queueRecord maps one queue row onto the same Record the day list uses.
//
// IT IS A SECOND MAPPER RATHER THAN A SHARED ONE because sqlc generates a distinct
// row type per query and Go has no structural typing to bridge them. The column
// lists are deliberately identical (db/queries/reviews.sql says why), and
// TestQueueRecord_CarriesEverythingTheDayRecordDoes pins that this mapper fills
// every field the other one does -- so a column added to one list and not the other
// turns red rather than rendering a blank docket.
//
// Review is empty by construction: this query returns only records with NO decision
// yet, so there is nothing to map. That is not the same as "not selected" -- it is
// the queue's defining predicate.
func queueRecord(row store.ListFlaggedForReviewRow) Record {
	return Record{
		ID:             row.ID,
		OccurredAt:     row.OccurredAt,
		Direction:      deref(row.Type),
		Trust:          row.Trust,
		Verdict:        row.Verdict,
		Channel:        row.Channel,
		Practice:       row.Practice,
		Queued:         row.Queued,
		Manual:         row.Channel == "manual",
		TagUID:         deref(row.TagUid),
		Ctr:            row.Ctr,
		IPMatch:        row.IpMatch,
		GPSMatch:       row.GpsMatch,
		Note:           deref(row.Note),
		NoteIsTenants:  deref(row.PolicyLayer) == policyLayerTenant,
		LocationName:   deref(row.LocationName),
		DepartmentName: deref(row.DepartmentName),
		EmployeeName:   deref(row.EmployeeName),
	}
}
