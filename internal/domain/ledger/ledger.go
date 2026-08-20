// Package ledger is the panel's READ SIDE.
//
// THE NAME IS STILL THE CONSTRAINT, but the constraint is narrower than the name
// suggests and saying so exactly is the point. A ledger is a book you add lines to
// and never rub out, which is what CLAUDE.md §4.3 says transactions is, and this
// package began as the READING half of that table alone. What holds for every file
// in it, transactions or not, is the operative half: there is no Insert, no Update
// and no Delete here, and there is no store call in this package that is not a
// SELECT. The write path for a record lives in internal/domain/checkin, the
// correction flow (a NEW row plus audit_log, never an edit) is M6-08, and the write
// path for a PERSON -- invite, deactivate, move -- is M6-05 phase B and is not here
// either.
//
// ⚠️ IT READS ONE MUTABLE TABLE, AND THAT IS WRITTEN DOWN RATHER THAN ELIDED.
// roster.go reads employees, whose status changes over a person's employment. The
// sentence "this package reads the immutable record" was true until M6-05 and is
// not any more; what replaced it is the sentence above, which is the property the
// tests and the §4.5 net actually depend on. roster.go's own header says why the
// roster landed here rather than in a package of its own.
//
// 🔴 §4.7 — WHAT A RECORD CANNOT CARRY. The row on disk holds gps_lat, gps_lng
// and source_ip. Record below has no field for any of them, and the query it is
// built from does not select them (db/queries/transactions.sql,
// ListPanelTransactions). That is two independent walls rather than one: a
// template cannot render a coordinate it has no field for, and a future field
// added to this struct would still have nothing to fill it from. What the panel
// shows instead is the SIGNS — ip_match and gps_match — which is what §5 rows
// 6-7 actually decided on.
//
// TENANT SCOPE (§4.5, belt + braces). Every read here runs inside
// db.(*DB).WithTenant, which sets app.tenant_id transaction-locally so RLS
// applies, AND passes the same tenant id as an explicit predicate to the query.
// The id is the caller's — the panel takes it from the signed session
// (httpx.AdminIdentity), never from the request — and this package has no way to
// read one from anywhere else.
package ledger

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// Database is the slice of the store this package needs, declared HERE because
// CLAUDE.md §7 puts an interface on the consumer's side. It is one method: the
// tenant-scoped transaction. Nothing in this package can reach an unscoped pool.
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// PageSize is how many dockets one request returns.
//
// IT IS THE UNIT THE REQUEST BUDGET IS COUNTED IN (adminratelimit.go): a section
// view costs one request, and each further page costs one more. 25 fills a
// laptop screen twice over, so an ordinary day at a single venue — the simulated
// day is 31 records — is one page and one "show more" rather than a scroll that
// bills a request every few centimetres.
const PageSize = 25

// Reader is the panel's read side.
type Reader struct {
	data Database
	log  *slog.Logger
}

// NewReader wires it. A nil database is refused rather than tolerated: a reader
// that cannot read must not be constructible, because the failure it would
// produce — an empty page that looks like a quiet day — is indistinguishable
// from a real answer (§4.6).
func NewReader(data Database, log *slog.Logger) (*Reader, error) {
	if data == nil {
		return nil, errors.New("ledger: nil database")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Reader{data: data, log: log}, nil
}

// Date is a calendar day with no clock and no zone — the thing a manager types
// into a date field.
//
// IT IS NOT A time.Time ON PURPOSE. A time.Time is an instant, and "the 5th of
// August" is not one: it becomes an instant only once a zone is known, and the
// zone is the tenant's and lives in the database. Carrying the intent as a
// time.Time would mean picking a zone at parse time, in the handler, from
// nothing.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

// Zero reports whether no day was asked for. The zero Date means "the tenant's
// today", which cannot be computed until the tenant's zone has been read.
func (d Date) Zero() bool { return d.Year == 0 && d.Month == 0 && d.Day == 0 }

// String renders the ISO form the date input and the query string use.
func (d Date) String() string {
	if d.Zero() {
		return ""
	}
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, int(d.Month), d.Day)
}

// ParseDate reads the ISO form. An empty string is the zero Date rather than an
// error: "no date given" is a legitimate request, and it means today.
func ParseDate(s string) (Date, error) {
	if s == "" {
		return Date{}, nil
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Date{}, fmt.Errorf("ledger: parse date %q: %w", s, err)
	}
	return Date{Year: t.Year(), Month: t.Month(), Day: t.Day()}, nil
}

// Cursor is the keyset position: the (occurred_at, id) of the last docket a
// reader has already been shown.
//
// IT IS CLIENT-SUPPLIED AND THAT IS SAFE. It can only move the reader within
// their own tenant's timeline, because the tenant predicate is not something the
// client contributes to. A cursor that makes no sense yields a page that starts
// somewhere odd; it can never widen the scope.
type Cursor struct {
	At time.Time
	ID uuid.UUID
}

// Filter is the six filters M6-03's card names, plus the paging position.
//
// EVERY OPTIONAL FILTER IS A POINTER OR AN EMPTY STRING, and "unset" means "do
// not narrow" rather than "match nothing". The distinction is the whole reason
// the query uses sqlc.narg: a zero uuid.UUID passed as a value would match no
// row and the screen would say the day was empty when it was not.
type Filter struct {
	Date         Date
	LocationID   *uuid.UUID
	DepartmentID *uuid.UUID
	// EmployeeName is a NAME FRAGMENT, not an id, and "" means "everyone".
	//
	// 🔴 IT WAS AN ID UNTIL 2026-08-06 AND THE CHANGE IS A USER DECISION MADE ON A
	// MEASUREMENT. Picking an id needs the panel to offer every employee as an
	// <option>: measured on a real page that list was 835 319 of 867 233 bytes --
	// 96% -- and it grew with the payroll forever on a page that is never cached.
	// Matching a name server-side makes the control a text box and the page ~31 KB
	// at any size of business.
	//
	// THE CALLER HAS ALREADY ESCAPED % AND _ (internal/handler). This layer passes
	// the fragment through; the query wraps it in wildcards.
	EmployeeName string
	// Verdict is one of ok, flag, reject, ignored, or "" for all four. The
	// handler validates it against the closed set before it gets here (§7:
	// the domain sees data that is already valid).
	Verdict string
	// Channel is one of nfc, qr, manual, or "" for all three.
	Channel string
	// After is the keyset cursor. nil starts at the newest record of the day.
	After *Cursor
}

// Record is one docket.
//
// 🔴 THE FIELDS THAT ARE NOT HERE ARE THE POINT — see the package comment. There
// is no Latitude, no Longitude, no SourceIP, and no PolicyContext.
type Record struct {
	ID         uuid.UUID
	OccurredAt time.Time
	// Direction is "in", "out", or "" — reject and ignored carry no direction
	// (migration 0005's CHECK), and an empty string is how that says so.
	Direction string
	// Trust is nil when the engine recorded none; 0 is a real score and must
	// not be confused with absence.
	Trust   *int16
	Verdict string
	Channel string
	// Practice is the post-activation training tap (§5). It is rendered as a
	// TRAINING stamp and NOTHING is claimed about hours here: totalling hours
	// is M6-07 and does not exist yet, so a sentence about what does or does
	// not count toward them would be this screen describing work the product
	// has not done.
	Practice bool
	// Queued means the tap engine put this record in the approval queue.
	Queued bool
	// Review is the MANAGER's decision on this record: "approved", "rejected", or
	// "" for none yet. It comes from transaction_reviews through a LEFT JOIN
	// (M6-04), never from transactions.
	//
	// 🔴 IT IS A SEPARATE FIELD FROM Verdict AND MUST STAY ONE. Verdict is what the
	// ENGINE decided at the moment of the tap and §4.3 makes it permanent; this is
	// what a human decided afterwards. Folding the second into the first -- rendering
	// an approved flag as if the engine had said "ok" -- would be the exact edit Q20
	// exists to prevent, done in the read path instead of the write path.
	Review string
	// ReviewNote is the sentence the manager typed beside their decision, "" when
	// they typed none. It comes from transaction_reviews.note through the same
	// LEFT JOIN as Review.
	//
	// 🔴 IT IS RENDERED, AND THAT IS A USER DECISION (2026-08-06) TAKEN ON A
	// MEASUREMENT. It was write-only when M6-04 first shipped, and the reason it is
	// not any more is not that showing it is useful: it is that a field nobody ever
	// reads back is a field the product can TRUNCATE IN SILENCE. The boundary cuts
	// at 500 characters (internal/handler's maxReviewNote), and with nothing
	// rendering the note a manager could never see that their sentence had been
	// cut. Reading it back is what makes that visible.
	//
	// IT IS FREE TEXT A HUMAN WROTE, AND IT IS THE ONLY SUCH FIELD ON THIS TYPE —
	// unlike Note below, which is one of our own policy sentences whatever layer
	// selected it. A round of M8-04 struck that clause out, on a claim about
	// "author-written reasons" the audit after it measured to be false; see
	// NoteIsTenants for what a customer really controls. templ escapes both on
	// output, proved rather than asserted:
	// TestReviewDB_AHostileNoteIsEscapedWhereItIsRendered for this field, which
	// arrives through a POST, and TestPolicyNote_IsEscapedOnBothSurfacesThatPrintIt
	// for Note, which arrives through the engine.
	ReviewNote string
	// Manual is derived from the channel rather than from entered_by. §5 pairs
	// the two ("channel='manual' + entered_by dolu"), and the channel is the
	// column with a CHECK on it; a manual row with no operator recorded is a
	// data defect that should still render AS manual rather than silently as a
	// tap.
	Manual bool
	TagUID string
	Ctr    *int32
	// IPMatch and GPSMatch are the two evidence SIGNS. nil means "not assessed
	// on this channel" — migration 0005 keeps them three-state on purpose, and
	// collapsing nil to false here would report a manual entry as having failed
	// a check that was never run.
	IPMatch  *bool
	GPSMatch *bool
	Note     string
	// NoteIsTenants says the DECISION behind Note came from this organisation's own
	// policy document rather than from ours (M8-04 FAZ B3). It is derived from
	// transactions.policy_layer — 'tenant' means the winning statement sat on a
	// customer document.
	//
	// 🔴 IT MARKS THE RULE, NOT THE AUTHORSHIP OF THE WORDS, AND A ROUND OF M8-04 GOT
	// THAT BACKWARDS. That round wrote here that a tenant's `reason` is "free text the
	// tenant typed, bounded only by maxReasonLen"; the audit after it measured the
	// write paths and found no such path — tenant.copyOfShipped copies a shipped
	// statement WHOLE (effect, action, condition and reason) and replaces only the sid
	// and the resource list, tenant.AuthorCommand has no field a sentence could arrive
	// in, and the panel form uploads no document. What a customer chooses is WHICH
	// shipped statement applies and WHERE. The behavioural half is held by
	// TestAuthoredRule_TheProseIsOursAndOnlyTheScopeIsTheirs, which calls the one
	// document generator; the structural half by cmd/tappa's three note-provenance
	// tests, which derive the writers from db/ rather than trusting a list of names
	// (TestNoteProvenance_TheWriterListIsDerivedFromTheQueriesRatherThanNamed,
	// TestNoteProvenance_TheStoreCarriesNoWriterTheQueriesDoNotName and
	// TestNoteProvenance_OnlyTwoProductionCallSitesWriteAPolicyDocument). The day a
	// raw-JSON editor lands (Q22 defers it to M9-07) one of the four goes red, and
	// which one depends on the shape it takes -- the cmd/tappa file's header names
	// the shapes it caught when they were injected, and the one it did not.
	//
	// ⚠️ SO SAY PLAINLY WHAT THE LABEL BUYS AND WHAT IT MISSES. It buys a manager the
	// answer to "which document do I open to find this rule" without putting a sid on
	// the card. It does NOT catch the one note class this product has measured saying
	// something the same row denies: a venue whose stored range was too wide to tell
	// it apart from anywhere else produced ip_match=true and OUR baseline sentence
	// "network proof of place: the source IP matches the location" (backlog T40).
	// Those rows read policy_layer='baseline', so this boolean is false on every one
	// of them. The class is closed on both sides now — netx.TooWideForProofOfPlace at
	// save time, tap.ipMatches at read time — but transactions are immutable (§4.3),
	// so the rows written while it was open keep the sentence for good.
	//
	// ⚠️ IT IS A BOOLEAN RATHER THAN THE LAYER STRING, deliberately: the docket asks
	// ONE question ("was this our rule, or theirs"), and the three-value column would
	// put a vocabulary on a screen that has no use for the difference between
	// guardrail and baseline — both are ours.
	NoteIsTenants bool
	// The three names are empty when the row carries no such id, which §4.6
	// requires to be possible: a stolen plaque touched with no cookie writes a
	// reject with no employee at all, and that record must still be listed.
	LocationName   string
	DepartmentName string
	EmployeeName   string
}

// Option is one entry of a filter's dropdown.
type Option struct {
	ID    uuid.UUID
	Label string
	// Group is the location a department belongs to — what makes the list readable
	// when several venues each have a "Kitchen". Empty when the list needs no
	// grouping.
	//
	// It used to read "or the lifecycle status of an employee". Employees stopped
	// being a list on 2026-08-06 (see Filter.EmployeeName), so the department
	// dropdown is the only caller left.
	Group string
}

// Options are the contents of the two id-valued filters.
//
// 🔴 THERE IS NO Employees FIELD ANY MORE, and its absence is the fix rather than
// an omission. The person filter is a name match now (see Filter.EmployeeName), so
// there is no list to render -- which is the entire saving: an employee roster is
// the only one of the three that grows without bound, and it was 96% of the page.
// Locations and departments are bounded by how many venues a business has, so they
// stay as pickable lists.
type Options struct {
	Locations   []Option
	Departments []Option
}

// Page is one page of dockets.
type Page struct {
	// Queried is set ONLY after the database has answered.
	//
	// 🔴 IT IS THE ANTI-FABRICATION FLAG AND IT IS LOAD-BEARING. §4.6's
	// underlying rule — the class M5-11 closed — is that the product must never
	// state something it has not measured, and "no records for this day" is
	// exactly such a statement. A zero Page has Records == nil, which renders
	// identically to a real empty day unless something distinguishes them. The
	// template refuses to print the empty state when this is false.
	Queried bool
	Records []Record
	// Next is the cursor for the following page, or nil when this page is the
	// last one. It is nil rather than a sentinel so "there is no more" cannot be
	// mistaken for "start again from the top".
	Next *Cursor
	// Zone is the tenant's timezone, resolved. The renderer converts the UTC
	// instants above into it (CLAUDE.md §6: conversion happens at render).
	Zone *time.Location
	// Day is the resolved calendar day — the tenant's today when the filter
	// asked for none. The form echoes it so a manager can see which day they
	// are looking at.
	Day Date
}

// Plaques is what this business's wall plaques amount to, as two counts.
//
// 🔴 IT IS A FACT ABOUT THE BUSINESS, NOT ABOUT THE DAY, and that is the whole
// reason it rides on Screen rather than on Page. Page answers "what happened on
// this date"; a business that has never had a plaque in service is not having a
// quiet day, it is a business no tap can reach yet, and no amount of looking at
// other dates changes that. The panel's landing section told a first-afternoon
// customer to "pick another day" because the only fact it held was the day's.
//
// 🔴 Queried IS HERE FOR THE SAME REASON Page.Queried IS. Two zero counts and "we
// never asked" are the same struct otherwise, and the difference decides whether a
// screen may say "nothing can be recorded yet" — which is a claim about the world
// (§4.6, the class M5-11 closed). Day() leaves this false and the fragment it
// serves prints nothing that depends on it.
type Plaques struct {
	Queried bool
	// InService is the number of plaques a tap could actually use — the same
	// `status = 'active'` test §5 row 1 applies, so this is not a second opinion
	// about what would be rejected.
	InService int
	// InStock is how many are loaded but not yet mounted ('unassigned').
	//
	// IT IS COUNTED RATHER THAN INFERRED FROM Loaded - InService, and the
	// difference is a wrong sentence: the leftover also contains retired and lost
	// plaques, so a business whose last plaque was retired would be told its
	// replacement is waiting in the box.
	InStock int
	// Loaded is every plaque row this business has, whatever its status. It
	// separates "Tappa has not loaded any" from "every one of them is out of
	// service", which would otherwise be one silence.
	Loaded int
}

// History is whether this business has any record at all, on any day.
//
// 🔴 IT IS A SEPARATE FACT FROM Plaques AND AN AUDIT IS WHY. The first version
// decided "is there any point offering another day" from the plaque counts, on the
// assumption that a business with no plaque can have no records. That is false: a
// manager types records by hand (channel='manual', no plaque), and the audit proved
// it with a real INSERT into a tenant holding zero plaques. Advice was therefore
// being withdrawn from exactly the manager who needed it — the one whose records are
// on another day.
//
// ⚠️ IT MAY NEVER BE PRINTED. "This business has no records" is a claim about the
// world; this bit only decides whether a piece of ADVICE is offered, and withdrawing
// advice makes no claim at all.
type History struct {
	// Queried is the anti-fabrication flag. Unmeasured means the advice STAYS —
	// never withdraw on the strength of something nobody asked.
	Queried bool
	// Any is true when at least one transactions row exists for this tenant.
	Any bool
}

// Screen is a Page plus the things only the full document needs.
type Screen struct {
	Page
	Options    Options
	Plaques    Plaques
	History    History
	TenantName string
}

// Day loads one page of dockets for the given filter.
//
// ONE TRANSACTION, TWO QUERIES. The clock has to be read before the day's
// boundaries can be computed, and reading it in the same transaction as the rows
// means the page cannot straddle a zone change or a second WithTenant round trip.
func (r *Reader) Day(ctx context.Context, tenantID uuid.UUID, f Filter) (Page, error) {
	var out Page
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		clock, err := q.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load tenant clock: %w", err)
		}
		out, err = r.page(ctx, q, tenantID, f, clock.Timezone)
		return err
	})
	if err != nil {
		return Page{}, fmt.Errorf("ledger: day: %w", err)
	}
	return out, nil
}

// Screen loads the full section: the same page plus the filter dropdowns.
//
// THE OPTION QUERIES RIDE IN THE SAME TRANSACTION AND THE SAME TENANT CONTEXT as
// the page, so every statement is answered with one `SET LOCAL app.tenant_id` in
// force and one connection's view of the schema, and a failure anywhere abandons
// the whole read rather than half of it.
//
// ⚠️ THAT IS NOT THE SAME AS ONE SNAPSHOT, AND THIS COMMENT USED TO SAY IT WAS.
// db.WithTenant opens the transaction with pool.Begin, so the isolation level is
// READ COMMITTED and EACH STATEMENT TAKES ITS OWN SNAPSHOT. An audit measured it:
// two identical counts inside one of these transactions, with another session
// committing in between, returned different numbers. Genuine cross-statement
// stability would need REPEATABLE READ, which costs a serialisation failure this
// read has no retry for — so it was not taken, and the sentence was corrected
// instead of the isolation level.
func (r *Reader) Screen(ctx context.Context, tenantID uuid.UUID, f Filter) (Screen, error) {
	var out Screen
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		clock, err := q.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load tenant clock: %w", err)
		}
		out.TenantName = clock.Name
		if out.Page, err = r.page(ctx, q, tenantID, f, clock.Timezone); err != nil {
			return err
		}
		if out.Options, err = options(ctx, q, tenantID); err != nil {
			return err
		}
		out.Plaques, out.History = r.advisories(ctx, tx, tenantID)
		return nil
	})
	if err != nil {
		return Screen{}, fmt.Errorf("ledger: screen: %w", err)
	}
	return out, nil
}

// advisories takes the landing section's two ADVISORY readings, and IT RETURNS NO
// ERROR ON PURPOSE.
//
// 🔴 A FAILED ADVICE MUST NOT COST A MANAGER THE EVIDENCE PAGE. Both readings decide
// prose: one whether to explain that no plaque is in service, the other whether
// "Pick another day above" is worth offering. Neither is a record. Returning their
// error from Screen would send the whole section to problemPanelUnavailable — so a
// permission slip, a statement timeout or a bug in either count would take away the
// day's transactions, which had already been read successfully one statement
// earlier. §4.6's rule is that evidence stays reachable; this is that rule applied
// to a read.
//
// 🔴 AND IT IS WHAT MAKES THE Queried FLAGS REACHABLE. Plaques.Queried and
// History.Queried were written for exactly the state "we did not measure", with two
// deliberately OPPOSITE safe directions (say nothing about plaques; keep the date
// advice) — and until this function existed the only producer either wrote `true` or
// failed the page, so the degradation they were designed for could not be entered.
// A designed path nothing can reach is a claim the types make and the code does not.
//
// 🔴 EACH READ RUNS INSIDE ITS OWN SAVEPOINT, AND WITHOUT THAT THE WHOLE THING IS
// DECORATION. Swallowing the Go error is not enough: Postgres ABORTS the transaction
// on any statement error, so the outer tx.Commit() in db.WithTenant then answers
// pgx.ErrTxCommitRollback ("commit unexpectedly resulted in rollback"), WithTenant
// returns non-nil, Screen returns an error, and the handler renders the 500 this
// function exists to prevent. An audit reproduced exactly that chain against real
// Postgres. All three failures this comment names — a permission slip, a statement
// timeout, a bug in either count — are STATEMENT errors, so all three abort.
// pgx.Tx.Begin issues a SAVEPOINT; rolling back to it clears the aborted state and
// leaves the outer transaction committable.
//
// COST: two extra statements per advisory read (SAVEPOINT + RELEASE), four per
// request, against a connection that is already open. They touch no disk and write
// no WAL. That is the price of not converting an advisory read into a 500.
//
// ⚠️ THE TWO SAVEPOINTS ARE INDEPENDENT, NOT ONE AROUND BOTH. One would make the
// readings all-or-nothing again at a finer grain: a failure on `tags` would take
// away the date-picker answer too, which is read from a different table and had
// nothing wrong with it. An audit's fixture produces exactly that split.
//
// "Independent" rather than "sibling": ROLLBACK TO SAVEPOINT does not DESTROY the
// savepoint, so on the failure path the second one is opened while the first still
// stands — pgx names them sp_1 and sp_2 and they nest two deep. Both are released by
// the outer COMMIT and nothing else depends on the depth.
//
// ⚠️ THE QUERY ERRORS ARE NOT SWALLOWED (§7): each is logged at ERROR with the
// tenant, and what is dropped is the SENTENCE. The one error that IS discarded is
// sp.Rollback's — deliberately, and with the same idiom db.WithTenant uses for its
// own rollback: it runs on the failure path, there is no second remedy to try, and
// surfacing it would replace a precise "this read failed" with a vaguer one. If the
// rollback really did not take, the outer COMMIT fails and the caller gets its 500.
//
// ⚠️ THE ORDER MATTERS AND IS DELIBERATE. These are the LAST two reads in the
// transaction, so the page and the filter options are already in hand when they run.
// WHAT THAT BUYS IS NARROW, and the narrowness is the honest part: a STATEMENT error
// here (a permission slip, a timeout, a bug in either count) cannot cost the records.
// A CONNECTION LOSS still can — the savepoint, the release and the outer COMMIT all
// fail together, and Screen returns an error however far the read had got. An
// unreachable database is the easy case: it fails at pool.Begin before any statement
// runs at all, which is the correct 500.
//
// 🔴 FOR WHOEVER MOVES THE TENANT CONTEXT: `SET LOCAL app.tenant_id` is issued by
// db.WithTenant BEFORE any of this, so it is OUTSIDE both savepoints and survives a
// ROLLBACK TO (verified against Postgres). That is why this is safe today. If the
// tenant context is ever established INSIDE a savepoint, rolling back would unset it
// and §4.5 would open silently — RLS reads the GUC, and NULLIF makes an unset one a
// policy that matches nothing rather than an error.
func (r *Reader) advisories(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (Plaques, History) {
	var plaques Plaques
	// IN THE SAME TRANSACTION AND TENANT CONTEXT AS EVERYTHING ELSE — see Screen's
	// doc comment for what that does and does not buy.
	//
	// ⚠️ IT IS AN EXTRA STATEMENT, NOT A FREE ONE. An earlier version of this comment
	// said "costs no second round trip"; a statement IS a round trip. What it costs
	// no second of is a CONNECTION and a TRANSACTION — WithTenant is already open, so
	// there is no second pool checkout and no second `SET LOCAL`. The statement's own
	// price is measured in db/queries/tags.sql (heap access, ~0.2 ms at 15 plaques).
	if err := inSavepoint(ctx, tx, func(q *store.Queries) error {
		row, err := q.CountTenantPlaques(ctx, tenantID)
		if err != nil {
			return err
		}
		plaques = Plaques{
			Queried:   true,
			InService: int(row.InService),
			InStock:   int(row.InStock),
			Loaded:    int(row.Loaded),
		}
		return nil
	}); err != nil {
		plaques = Plaques{}
		r.log.Error("ledger: could not read the plaque state; the landing section will "+
			"say nothing about plaques", "err", err, "tenant_id", tenantID)
	}

	var history History
	// The cheaper of the two: an index-only EXISTS that stops at the first row
	// (db/queries/transactions.sql measures it, and says what its heap fetch depends
	// on). It answers whether "Pick another day above" is worth saying.
	if err := inSavepoint(ctx, tx, func(q *store.Queries) error {
		any, err := q.TenantHasAnyTransaction(ctx, tenantID)
		if err != nil {
			return err
		}
		history = History{Queried: true, Any: any}
		return nil
	}); err != nil {
		history = History{}
		r.log.Error("ledger: could not read whether this business has any record; the "+
			"landing section will keep offering another day",
			"err", err, "tenant_id", tenantID)
	}
	return plaques, history
}

// inSavepoint runs one advisory read inside a SAVEPOINT and rolls back to it on
// failure, so a failed statement cannot abort the transaction it is riding on.
//
// THE ROLLBACK USES A BACKGROUND CONTEXT, for the reason db.WithTenant's does: ctx
// may already be cancelled, and a skipped ROLLBACK TO SAVEPOINT would leave the
// outer transaction aborted — which is the exact failure this function exists to
// prevent, arriving by a different door.
func inSavepoint(ctx context.Context, tx pgx.Tx, fn func(*store.Queries) error) error {
	sp, err := tx.Begin(ctx) // SAVEPOINT
	if err != nil {
		return fmt.Errorf("savepoint: %w", err)
	}
	if err := fn(store.New(sp)); err != nil {
		_ = sp.Rollback(context.Background()) // ROLLBACK TO SAVEPOINT
		return err
	}
	if err := sp.Commit(ctx); err != nil { // RELEASE SAVEPOINT
		return fmt.Errorf("release savepoint: %w", err)
	}
	return nil
}

// page is the shared body of Day and Screen.
func (r *Reader) page(ctx context.Context, q *store.Queries, tenantID uuid.UUID, f Filter, zoneName string) (Page, error) {
	zone := r.loadZone(zoneName)
	day := f.Date
	if day.Zero() {
		now := time.Now().In(zone)
		day = Date{Year: now.Year(), Month: now.Month(), Day: now.Day()}
	}
	from := time.Date(day.Year, day.Month, day.Day, 0, 0, 0, 0, zone)
	// AddDate rather than +24h: a day is not always 24 hours long. Malta puts
	// its clocks forward in March, and a fixed 24-hour window on that date would
	// reach an hour into the next day and show its first taps on the wrong page.
	to := from.AddDate(0, 0, 1)

	params := store.ListPanelTransactionsParams{
		TenantID:     tenantID,
		FromAt:       from,
		ToAt:         to,
		LocationID:   f.LocationID,
		DepartmentID: f.DepartmentID,
		PageSize:     PageSize + 1, // one extra: see below
	}
	if f.EmployeeName != "" {
		n := escapeLike(f.EmployeeName)
		params.EmployeeName = &n
	}
	if f.Verdict != "" {
		v := f.Verdict
		params.Verdict = &v
	}
	if f.Channel != "" {
		c := f.Channel
		params.Channel = &c
	}
	if f.After != nil {
		at, id := f.After.At, f.After.ID
		params.CursorAt = &at
		params.CursorID = &id
	}

	rows, err := q.ListPanelTransactions(ctx, params)
	if err != nil {
		return Page{}, fmt.Errorf("list transactions: %w", err)
	}

	// ONE EXTRA ROW IS FETCHED AND DROPPED, which is how "is there another page"
	// is answered without a COUNT over the day. A count would be a second scan
	// and would still be a guess by the time the next page is asked for; the
	// extra row is proof, and it costs one tuple.
	page := Page{Queried: true, Zone: zone, Day: day}
	more := len(rows) > PageSize
	if more {
		rows = rows[:PageSize]
	}
	page.Records = make([]Record, 0, len(rows))
	for _, row := range rows {
		page.Records = append(page.Records, record(row))
	}
	if more && len(page.Records) > 0 {
		last := page.Records[len(page.Records)-1]
		page.Next = &Cursor{At: last.OccurredAt, ID: last.ID}
	}
	return page, nil
}

// record maps one store row to one docket.
//
// IT IS A NARROWING, NOT A COPY. The store row is already free of coordinates
// (the query does not select them); this step additionally derives Manual and
// flattens the nullable names, so nothing downstream has to know that a record
// without an employee is normal.
func record(row store.ListPanelTransactionsRow) Record {
	return Record{
		ID:             row.ID,
		OccurredAt:     row.OccurredAt,
		Direction:      deref(row.Type),
		Trust:          row.Trust,
		Verdict:        row.Verdict,
		Channel:        row.Channel,
		Practice:       row.Practice,
		Queued:         row.Queued,
		Review:         deref(row.ReviewOutcome),
		ReviewNote:     deref(row.ReviewNote),
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

// policyLayerTenant is the one value of transactions.policy_layer that means the
// deciding statement sat on a document the ORGANISATION owns rather than one of
// ours. The note's WORDS are ours either way — see Record.NoteIsTenants.
//
// 🔴 IT IS COMPARED, NOT CARRIED. The alternative was to put the layer string on
// Record and let the template switch on it, which would make every future value of
// a CHECK-constrained column (migration 0008: guardrail|baseline|tenant) a screen
// vocabulary. The docket has exactly one question to ask of this column.
//
// ⚠️ A NULL policy_layer READS AS "OURS", which is the fail-safe direction and is
// stated rather than left to be inferred: rows written before M3-07 carry NULL, as
// do manager-entered records (channel='manual' names no policy at all), and no
// tenant DOCUMENT decided either kind. The direction that would mislead is calling
// a tenant's rule ours, and that requires the column to say 'tenant' and this
// comparison to fail, which TestLedger_ATenantsPolicySentenceIsMarkedAsTheirs pins.
const policyLayerTenant = "tenant"

// options loads the three id-valued filters' contents.
func options(ctx context.Context, q *store.Queries, tenantID uuid.UUID) (Options, error) {
	var out Options

	locs, err := q.ListLocationsForTenant(ctx, tenantID)
	if err != nil {
		return Options{}, fmt.Errorf("list locations: %w", err)
	}
	out.Locations = make([]Option, 0, len(locs))
	for _, l := range locs {
		out.Locations = append(out.Locations, Option{ID: l.ID, Label: l.Name})
	}

	deps, err := q.ListDepartmentsForTenant(ctx, tenantID)
	if err != nil {
		return Options{}, fmt.Errorf("list departments: %w", err)
	}
	out.Departments = make([]Option, 0, len(deps))
	for _, d := range deps {
		out.Departments = append(out.Departments, Option{ID: d.ID, Label: d.Name, Group: d.LocationName})
	}
	return out, nil
}

// loadZone resolves the tenant's timezone name, falling back to UTC.
//
// A ZONE THE RUNTIME CANNOT LOAD MUST NOT COST THE MANAGER THEIR PAGE. The
// column is NOT NULL with a sane default (migration 0001), so this is a
// deployment problem (a container with no tzdata) rather than a data problem,
// and the honest degradation is to show the day in UTC rather than to fail.
//
// 🔴 BUT IT IS NOT SWALLOWED. CLAUDE.md §7 forbids that, and here the reason is
// concrete rather than stylistic: falling back to UTC silently means every
// timestamp on the page is quietly wrong by an hour or two for that tenant, the
// day boundary moves, and NOTHING anywhere says so. A container shipped without
// tzdata would look like a working panel with subtly misfiled records. The
// degradation stays, the silence goes.
func (r *Reader) loadZone(name string) *time.Location {
	if name == "" {
		r.log.Warn("ledger: tenant has no timezone; showing the day in UTC",
			"fallback", "UTC")
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		r.log.Error("ledger: cannot load the tenant timezone; showing the day in UTC",
			"zone", name, "fallback", "UTC", "err", err)
		return time.UTC
	}
	return loc
}

// escapeLike neutralises ILIKE's wildcards in a name the manager typed.
//
// 🔴 IT IS A CORRECTNESS FIX, NOT A SAFETY ONE, and the difference is worth being
// exact about: the value is a BOUND PARAMETER and the tenant predicate it is ANDed
// with is not something a client contributes to, so injection is not what this
// prevents -- pgx already does. What it prevents is a filter that looks applied and
// is not: ILIKE reads % and _ as wildcards, so "100%" typed into a box whose whole
// job is to NARROW would quietly match everyone.
//
// IT LIVES HERE RATHER THAN IN THE HANDLER because it is an artifact of the
// operator. An earlier version escaped at the boundary, and the escaped string was
// then what the form echoed back and what every paging link carried, so each
// submission escaped the previous escape. What travels through the handler, the
// view and the URL is now exactly what was typed; the backslashes exist only
// between here and Postgres.
//
// The backslash is replaced FIRST because it is ILIKE's own escape character;
// doing it last would escape the escapes this function just added.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
