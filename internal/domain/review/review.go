// Package review records a manager's decision on a FLAGGED record (M6-04).
//
// 🔴 §4.3 IS THE WHOLE SHAPE OF THIS PACKAGE. A flagged tap is evidence and may
// become a legal one; the manager's approval does not edit it, argue with it or
// replace it. There is no UPDATE and no DELETE anywhere below, and there could not
// be: tappa_app holds SELECT + INSERT only on transactions, transaction_reviews and
// audit_log (measured with has_table_privilege), and migration 00005 adds a BEFORE
// UPDATE OR DELETE trigger that refuses even tappa_owner. A decision is TWO NEW
// ROWS -- one in transaction_reviews, one in audit_log -- and the tap record goes
// on saying exactly what the engine decided at the time.
//
// WHY IT IS NOT internal/domain/ledger. That package is the reading half of the
// immutable record and says so in its first paragraph; keeping the one INSERT out
// of it means "no store call in ledger is not a SELECT" stays a fact somebody can
// check with grep rather than a sentence somebody has to trust.
//
// WHAT THIS PACKAGE DOES NOT DECIDE. Whether a record is reviewable at all is the
// DATABASE's answer, in three places that do not depend on this code being right:
// the INSERT's own SELECT (it can only build a row from a verdict='flag' record in
// this tenant), migration 00005's tappa_check_transaction_review trigger (same
// rule, plus no self-review), and the composite FK (transaction_id, tenant_id) ->
// transactions, which makes a cross-tenant review structurally impossible.
package review

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// Outcome is the closed vocabulary migration 00005's CHECK constrains.
//
// IT IS A NAMED TYPE so a caller cannot post an arbitrary string into the column by
// passing it through; Parse below is the only way to make one from user input.
type Outcome string

const (
	Approved Outcome = "approved"
	Rejected Outcome = "rejected"
)

// Action is the audit_log action name this flow writes.
//
// ONE NAME FOR BOTH OUTCOMES, with the outcome in `detail`, because an investigator
// asking "what has been reviewed here" should not have to know every verb the
// product might add later. It is also what makes `action LIKE 'review%'` a complete
// answer -- measured before this task: that query returned 0 rows, i.e. no review
// had ever been recorded by anything.
const Action = "review.recorded"

// Errors a caller must be able to tell apart, because each one is a different
// sentence to a manager.
var (
	// ErrNotReviewable means the id names nothing this tenant may decide on: it
	// does not exist, it belongs to somebody else, or its verdict is not 'flag'.
	//
	// 🔴 THE THREE COLLAPSE INTO ONE ERROR ON PURPOSE. Telling a caller which of
	// them it was would answer "does this id exist in another tenant?" for anyone
	// who can post a form -- the same enumeration oracle the sign-in flow refuses
	// to be. The distinction that matters for an investigation is in the process
	// log, not in the response.
	ErrNotReviewable = errors.New("review: not a reviewable record")
	// ErrAlreadyDecided means the record already carries a decision.
	// UNIQUE (transaction_id).
	//
	// 🔴 AND THERE IS NO WAY BACK — see docs/adr/0009-onay-karari-geri-alinamaz.md.
	// A decision cannot be superseded (UNIQUE), changed (REVOKE UPDATE) or removed
	// (REVOKE DELETE + a trigger that refuses tappa_owner too, measured). So §4.3's
	// own remedy — "a correction is a NEW ROW plus audit_log" — is structurally
	// unavailable on this table, deliberately: 00005 closed it so a manager could
	// not flip the effective outcome at will. A manager who presses Approve by
	// mistake has no route to undo it inside the product.
	//
	// ⚠️ IT DOES NOT SAY WHOSE. A caller that needs to know unwraps a
	// *DecidedError, which Record returns whenever it could read the existing row;
	// errors.Is(err, ErrAlreadyDecided) is true for both.
	ErrAlreadyDecided = errors.New("review: this record has already been decided")
	// ErrSelfReview is the trigger's third rule surfacing: a reviewer may not
	// decide their own record. It is unreachable today (a reviewer is an
	// admin_users id and the reviewed party an employees id) and is mapped anyway,
	// because M6-05 and M7 bring the two identity spaces closer together and a
	// bare 500 at that point would be indistinguishable from an outage.
	ErrSelfReview = errors.New("review: a reviewer may not decide their own record")
)

// DecidedError is ErrAlreadyDecided WITH the two facts a screen needs in order to
// say something true: who decided the record, and what they decided.
//
// 🔴 IT EXISTS BECAUSE THE SCREEN WAS FABRICATING A SUBJECT. A bare
// ErrAlreadyDecided renders as "somebody else decided that record first", and a
// manager who double-clicks Approve is shown exactly that about a decision they had
// just made themselves — their own click reported as another person's, with "yours
// was not written" attached to it. The sentence was true about the WRITE and false
// about the WHO, and nothing in the system had measured the who.
//
// ByYou IS COMPUTED HERE rather than in the handler so the comparison happens in
// the one place that holds both ids at once.
type DecidedError struct {
	// ByYou is true when the existing decision's reviewer is the caller.
	ByYou bool
	// Outcome is what was recorded, so the screen can say WHICH decision it was.
	Outcome Outcome
}

func (e *DecidedError) Error() string {
	if e.ByYou {
		return "review: you have already decided this record"
	}
	return ErrAlreadyDecided.Error()
}

// Unwrap makes errors.Is(err, ErrAlreadyDecided) true for both shapes, so a caller
// that does not care who decided it keeps working unchanged.
func (e *DecidedError) Unwrap() error { return ErrAlreadyDecided }

// Parse turns posted text into an Outcome. Anything else is refused rather than
// defaulted: a mistyped outcome must not silently become an approval.
func Parse(s string) (Outcome, bool) {
	switch Outcome(s) {
	case Approved:
		return Approved, true
	case Rejected:
		return Rejected, true
	default:
		return "", false
	}
}

// Database is the slice of the store this package needs, declared HERE at the
// consumer (CLAUDE.md §7). One method: the tenant-scoped transaction. Nothing here
// can reach an unscoped pool.
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// Trail is the slice of internal/audit this package needs.
//
// 🔴 IT IS RecordTx AND NOT Record, AND THAT IS THE OPPOSITE OF WHAT MOST CALLERS
// WANT. internal/audit's Record opens its OWN transaction, and its doc explains why
// that is normally right: the caller who most needs an audit row is the one whose
// main transaction FAILED, and a row written inside that transaction would roll
// back with it.
//
// HERE THE TWO ROWS MUST SHARE A FATE, and both directions are a lie if they do not:
//
//	review row written, audit row missing  -> a decision with no trail, which is
//	                                          exactly the half of §4.3 ("a
//	                                          correction is a new row PLUS
//	                                          audit_log") that had never been built.
//	audit row written, review row missing  -> the trail says a decision was recorded
//	                                          when the queue still shows the record
//	                                          waiting. A trail that says something
//	                                          untrue is worse than no trail.
//
// So the audit write rides in the caller's transaction and a failure on either side
// rolls both back. Measured in both directions -- see
// internal/handler/review_db_test.go.
type Trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// Decision is what a manager submitted.
type Decision struct {
	TenantID      uuid.UUID
	TransactionID uuid.UUID
	// ReviewerID is the ADMIN making the decision, taken from the signed panel
	// session by the handler -- never from the request.
	ReviewerID uuid.UUID
	Outcome    Outcome
	// Note is the manager's optional sentence.
	//
	// IT IS STORED AND IT IS READ BACK (user decision, 2026-08-06): the panel's
	// transactions list renders it on the docket of the record it decided, beside
	// the decision itself. It was write-only for one round, and the reason that
	// changed is not that showing it is useful — it is that a field nothing reads
	// can be TRUNCATED IN SILENCE, and the boundary does truncate it (see
	// internal/handler's maxReviewNote and the clip signal beside it).
	//
	// The handler bounds and cleans it before it gets here (§7); this package
	// stores what it is given and makes no claim about how it is encoded on the way
	// out, because that depends on the renderer — templ escapes, a CSV does not.
	Note string
}

// detail is the audit row's jsonb payload.
//
// 🔴 IT IS A PURPOSE-BUILT STRUCT WITH KNOWN KEYS, which is the rule internal/audit
// states and the reason §4.7 survives a jsonb column: nothing here is a dump of a
// request, a params struct or a map of whatever was in scope. The fields are the
// four facts an investigator needs -- what was decided, on what, by whom, and
// whether a sentence was attached.
//
// THE NOTE'S TEXT IS NOT COPIED IN, only whether there was one. It is a manager's
// free text about an employee and it is already stored, once, in
// transaction_reviews.note; putting a second copy in an append-only jsonb column
// doubles the surface for no investigative gain.
type detail struct {
	Outcome  string `json:"outcome"`
	Verdict  string `json:"reviewed_verdict"`
	ReviewID string `json:"review_id"`
	HasNote  bool   `json:"has_note"`
}

// Reviewer records decisions. Dependencies are injected as struct fields (§7).
type Reviewer struct {
	data  Database
	trail Trail
	log   *slog.Logger
}

// NewReviewer wires it. A nil database or a nil trail is REFUSED rather than
// tolerated: a reviewer that cannot write the trail would record decisions with no
// audit row, which is the state this task exists to end. The failure has to be
// impossible to construct, not merely unlikely -- the M5-04 lesson, where a
// capability was delivered, approved and dead in production because two halves were
// never assembled.
func NewReviewer(data Database, trail Trail, log *slog.Logger) (*Reviewer, error) {
	switch {
	case data == nil:
		return nil, errors.New("review: nil database")
	case trail == nil:
		return nil, errors.New("review: nil audit trail")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Reviewer{data: data, trail: trail, log: log}, nil
}

// Record writes the decision and its trail in ONE transaction.
func (r *Reviewer) Record(ctx context.Context, d Decision) error {
	switch {
	case d.TenantID == uuid.Nil:
		return errors.New("review: tenant id is required")
	case d.TransactionID == uuid.Nil:
		return ErrNotReviewable
	case d.ReviewerID == uuid.Nil:
		return errors.New("review: reviewer id is required")
	}
	if _, ok := Parse(string(d.Outcome)); !ok {
		return fmt.Errorf("review: %q is not an outcome", d.Outcome)
	}

	err := r.data.WithTenant(ctx, d.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		params := store.InsertTransactionReviewParams{
			TenantID:      d.TenantID,
			TransactionID: d.TransactionID,
			ReviewerID:    d.ReviewerID,
			Outcome:       string(d.Outcome),
		}
		if d.Note != "" {
			n := d.Note
			params.Note = &n
		}
		row, err := q.InsertTransactionReview(ctx, params)
		if err != nil {
			return classify(err)
		}
		// THE TRAIL IS WRITTEN SECOND AND INSIDE THE SAME TRANSACTION. If it fails,
		// the review row above is rolled back with it -- see Trail's comment for why
		// that is the right direction here and the wrong one almost everywhere else.
		if _, err := r.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: d.TenantID,
			ActorID:  &d.ReviewerID,
			Action:   Action,
			Target:   d.TransactionID.String(),
			Detail: detail{
				Outcome: string(d.Outcome),
				// The record being reviewed is a flag by construction: the INSERT
				// above could not have produced a row otherwise. Stating it in the
				// trail means an investigator reading audit_log alone can see WHICH
				// engine verdict a human overrode without joining back.
				Verdict:  "flag",
				ReviewID: row.ID.String(),
				HasNote:  d.Note != "",
			},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrAlreadyDecided) {
			// 🔴 A SECOND TRANSACTION, NOT THE SAME ONE. The unique violation has
			// aborted the transaction above, so no further statement can run inside
			// it; PostgreSQL answers "current transaction is aborted" to anything
			// else. The read below therefore runs on its own, AFTER the rollback.
			return r.describeExisting(ctx, d)
		}
		if errors.Is(err, ErrNotReviewable) || errors.Is(err, ErrSelfReview) {
			return err
		}
		return fmt.Errorf("review: record decision: %w", err)
	}
	// 🔴 THE NOTE IS NOT LOGGED. It is a manager's free text about a named person,
	// so it stays in the row it was written into; the ids below are opaque handles
	// and the outcome is a two-value vocabulary (§4.7, §7).
	r.log.Info("flagged record decided",
		"transaction_id", d.TransactionID, "reviewer_id", d.ReviewerID,
		"outcome", string(d.Outcome))
	return nil
}

// describeExisting answers "who decided it, and what did they decide" after a
// unique violation.
//
// 🔴 IT DEGRADES TO THE BARE ERROR RATHER THAN TO A FAILURE. If the read cannot be
// made, the caller still learns the true thing — the record is already decided —
// and simply does not learn whose it was. The alternative, turning a successful
// refusal into a 500 because a follow-up read failed, would report an outage the
// manager does not have and would hide the fact that their decision was not
// recorded. The failure is logged rather than swallowed (§7).
//
// IT CLAIMS NOTHING ABOUT A RACE IT DID NOT SEE. Between the failed INSERT and this
// read the row cannot change — transaction_reviews is append-only and UNIQUE on
// transaction_id, so there is exactly one row and it never moves.
func (r *Reviewer) describeExisting(ctx context.Context, d Decision) error {
	var existing store.GetTransactionReviewRow
	err := r.data.WithTenant(ctx, d.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, e := store.New(tx).GetTransactionReview(ctx, store.GetTransactionReviewParams{
			TenantID:      d.TenantID,
			TransactionID: d.TransactionID,
		})
		existing = row
		return e
	})
	if err != nil {
		r.log.Error("review: the record is already decided and the existing decision "+
			"could not be read; the caller is told it is decided but not by whom",
			"transaction_id", d.TransactionID, "err", err)
		return ErrAlreadyDecided
	}
	return &DecidedError{
		ByYou:   existing.ReviewerID == d.ReviewerID,
		Outcome: Outcome(existing.Outcome),
	}
}

// classify turns the database's refusals into the errors a caller can act on.
//
// 🔴 pgx.ErrNoRows IS A REFUSAL HERE, NOT AN ABSENCE, and it is the query's design
// rather than an accident: InsertTransactionReview is an INSERT ... SELECT whose
// SELECT carries the tenant and the verdict='flag' predicate, so "no row was
// inserted" means "you may not decide that". Treating it as success would make this
// endpoint report decisions it never recorded.
//
// EVERY OTHER ERROR IS PASSED THROUGH UNWRAPPED-BY-NAME so it reaches the caller as
// a server failure. Guessing at an unknown SQLSTATE is how a real outage gets
// rendered as "already decided" and quietly dropped.
func classify(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotReviewable
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch {
		case pg.Code == "23505":
			// unique_violation on transaction_reviews_txn_key: somebody decided this
			// record first. 00005 makes that a ONE-TIME event on purpose.
			return ErrAlreadyDecided
		case pg.Code == "23514" && containsSelfReview(pg.Message):
			// check_violation raised by tappa_check_transaction_review. The trigger
			// raises the same SQLSTATE for "not a flag", but that branch is
			// unreachable through this query (the SELECT already filtered it), so
			// the message is what separates them.
			return ErrSelfReview
		}
	}
	return err
}

// selfReviewMessage is the prefix migration 00005's trigger raises. Matching on the
// MESSAGE is a stated weakness: the migration owns the sentence and this reads it.
// A mismatch costs a 500 rather than a wrong answer, which is the safe direction --
// and the alternative, a SQLSTATE of its own, is a migration.
const selfReviewMessage = "self-review forbidden"

func containsSelfReview(msg string) bool {
	return strings.HasPrefix(msg, selfReviewMessage)
}
