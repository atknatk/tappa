// Package manual records the ONE attendance record a human types rather than taps
// (M6-08): a shift supervisor entering time for somebody with no phone, and the
// checkout Q18 says the system will never invent for itself.
//
// 🔴 IT IS THE SECOND WRITER OF `transactions` IN THE WHOLE PRODUCT, and until
// 2026-08-10 there was exactly one (internal/domain/checkin, the tap orchestrator).
// That is the fact everything below is arranged around, because the barriers that
// protect an immutable, monetary, legally-usable record were built when there was one
// writer and several of them were CODE INVARIANTS rather than schema constraints.
// Measured before this package existed, on the development database:
//
//	transactions rows                                              319 067
//	rows with verdict IN ('reject','ignored') AND type IS NOT NULL        0
//
// i.e. "a refused record carries no direction" is TRUE of every row ever written.
// Migration 00005's transactions_ok_has_direction runs the other way only (ok => a
// direction), so before M6-08 there was no CHECK for the converse at all.
//
// ⚠️ THE FIRST VERSION OF THIS PARAGRAPH SAID IT WAS "enforced by NOTHING" AND THAT A
// SUCH A ROW "would enter a report as worked hours". BOTH WERE WRONG, AND A SECURITY
// LENS MEASURED THE CORRECTION — the risk was real but SMALLER, and there are THREE
// barriers rather than one:
//
//  1. internal/domain/tap assigns a direction only on ok/flag (decide.go). It is NOT
//     a bare code invariant: TestDecide_DirectionNilForNonRecordVerdicts is its
//     subject, and a mutation is red in four separate sub-cases.
//
//  2. THIS package cannot express the shape at all — verdict is a SQL literal.
//
//  3. internal/domain/ledger's endpointState FAIL-SAFES an unrecognised verdict to
//     HoursAwaiting rather than to payable. Measured, on the real accumulate:
//
//     both ends ok                 worked 8h   awaiting 0
//     both ends reject + direction worked 0    awaiting 8h
//     both ends ignored + direction worked 0   awaiting 8h
//     in ok / out reject           worked 0    awaiting 8h
//     an unknown verdict "void"    worked 0    awaiting 8h
//
//     So a directed reject would be QUARANTINED and reported separately, never paid.
//
// Migration 00014 adds the missing CHECK anyway, which turns "a broken row that gets
// quarantined" into "no such row" — see its own header for the measurement.
//
// 🔴 SO THIS PACKAGE CANNOT PRODUCE THE SHAPE AT ALL, and the refusal is in SQL
// rather than here. db/queries/transactions.sql's InsertManualTransaction writes the
// literal 'ok' for verdict and the literal 'manual' for channel; there is no
// parameter for either. A future edit to this file cannot widen what it writes
// without editing the statement, and the statement says why in its own header.
//
// WHAT A RECORD FROM HERE LOOKS LIKE, exactly, so a reader does not have to
// reconstruct it: channel='manual', entered_by = the signed-in administrator,
// verdict='ok', type in/out, trust = tap.TrustBase, practice=false, queued=false, and
// NULL in every column that would claim machine evidence -- no tag uid, no counter,
// no sun_valid, no source ip, no ip/gps match, no coordinates, no policy decision.
//
// 🔴 WHAT THIS PACKAGE DOES NOT DO, in the order somebody would expect it to:
//
//   - It does NOT update or delete anything. `transactions` takes neither, by
//     privilege and by trigger (00005), and section 4.3's remedy is a NEW ROW.
//   - It does NOT decide a verdict. There is no policy evaluation here and no call
//     to internal/domain/tap: nothing was judged, somebody was told. See
//     Recorder.Record for what that costs and what it is measured against.
//   - It does NOT reconcile the record with the ones already there. Whether a second
//     record in the same direction supersedes an earlier one is the REPORT's
//     arithmetic, not this write's, and what it actually does was measured -- see
//     the CorrectionsOnlyShorten block below.
//   - It does NOT speak HTTP. The manager's wall-clock time is converted to UTC in
//     the render layer (section 6), so Entry.At arrives as an instant.
package manual

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// 🔴 CorrectionsOnlyShorten -- THE MEASURED LIMIT OF "A CORRECTION IS A NEW ROW".
//
// The M6-08 card's fifth criterion is "correcting an existing record = a new row +
// audit; no UPDATE". The no-UPDATE half is structural and holds. The other half --
// that appending a row CORRECTS anything -- was measured against the shipped report
// engine (internal/domain/ledger's accumulate) rather than assumed, and it holds in
// only half the directions:
//
//	the engine pairs a person's LATEST `in` with the EARLIEST `out` after it,
//	so an appended row can only ever make an interval SHORTER.
//
// Measured 2026-08-10, one person, truth = in 09:00 / out 17:00 = 8h:
//
//	mistake                     appended correction   report before   after
//	out too EARLY (12:00)       out 17:00             3h              3h   <- NOT applied
//	out too LATE  (20:00)       out 17:00             11h             8h   <- applied
//	in  too LATE  (10:00)       in  09:00             7h              7h   <- NOT applied
//	in  too EARLY (08:00)       in  09:00             9h              8h   <- applied
//
// The two that do not apply are exactly the two that would RESTORE pay. The surplus
// row is not lost -- a stranded `out` is counted as Report.StartedEarlier and a
// stranded `in` becomes an OpenEntry -- but neither is labelled as a correction, and
// StartedEarlier renders as "this shift began before the period", which is a false
// sentence about that row.
//
// ⚠️ THERE IS A WORKING REMEDY AND IT IS UGLY: a compensating PAIR. After a wrongly
// early checkout at 12:00, entering in 12:01 + out 17:00 restores 7h59m (measured).
// It restores the money and writes a statement about the world that is not true.
//
// 🔴 AND THE CHECK-IN CASE IS WORSE THAN "NOT APPLIED" — THE ENTRY IT LEAVES BEHIND
// CANNOT BE CLOSED AT ALL. Two independent audits produced this separately. Correcting
// a check-in flushes the earlier one into an OpenEntry, and every later checkout then
// finds no open entry and is counted as StartedEarlier instead. Measured (truth
// 09:00-17:00, the check-in first typed as 10:00):
//
//	the wrong pair                      worked 7h   open 0   startedEarlier 0
//	+ the correcting check-in           worked 7h   open 1   startedEarlier 0
//	+ one attempt to close it           worked 7h   open 1   startedEarlier 1
//	+ a second attempt                  worked 7h   open 1   startedEarlier 2
//	+ a third attempt                   worked 7h   open 1   startedEarlier 3
//
// So the entry sits in the manager's "needs action" list permanently, and every attempt
// to clear it writes one more undeletable row plus one more wrong sentence on the
// report ("a shift that began before this week" — wrong twice: it began inside the
// week, and its hours are in no report at all).
//
// ✅ THE PRODUCT'S OWN SCENARIO IS UNAFFECTED, which is what keeps this a counted limit
// rather than a broken feature: Q18's forgotten checkout closes exactly as designed —
// an unclosed check-in reports 0 worked / 1 open, and the manager's checkout makes it
// 8h / 0 open (measured, same test). The trap needs a manager to have typed a CHECK-IN
// wrongly first. docs/adr/0011 records the decision and the three ways out.
//
// SO WHAT SHIPS: this package writes records, the screen calls them records, and
// nothing in the product claims that a later record supersedes an earlier one. A
// complete correction flow needs either supersede semantics on the report side or a
// third table (docs/adr/0009 lists the same three options for reviews); it is not
// this task's, and pretending otherwise on a payroll screen would be worse than the
// gap. The confirmation screen says the record is permanent before it is written.
const CorrectionsOnlyShorten = "a later record can shorten a shift but never lengthen it"

// Direction is the closed vocabulary migration 00005's transactions_type_check
// admits for a record that carries one.
//
// IT IS A NAMED TYPE so a caller cannot post an arbitrary string into the column by
// passing it through; ParseDirection is the only way to make one from user input.
// The same shape review.Outcome uses, for the same reason.
type Direction string

const (
	// In is a check-in: the person started work at Entry.At.
	In Direction = "in"
	// Out is a checkout: the person stopped work at Entry.At.
	Out Direction = "out"
)

// ParseDirection turns posted text into a Direction. Anything else is refused rather
// than defaulted -- a mistyped direction must not silently become a check-in, which
// is the one direction that opens an interval nobody closes.
func ParseDirection(s string) (Direction, bool) {
	switch Direction(s) {
	case In:
		return In, true
	case Out:
		return Out, true
	default:
		return "", false
	}
}

// Action is the audit_log action name this flow writes.
//
// THE NAME IS DERIVED FROM THE TRAIL'S OWN VOCABULARY rather than invented:
// "<noun>.<past participle>", as in review.recorded, employee.deactivated,
// plaque.mounted and report.exported. `action LIKE 'record.%'` is therefore the whole
// answer to "what has been typed into this business's attendance by hand", and it
// returned 0 rows before this task (measured).
//
// ONE NAME FOR BOTH DIRECTIONS, with the direction in `detail`, for the reason
// internal/domain/review gives: an investigator asking "what has been entered here"
// should not have to know every verb the product might add later.
const Action = "record.entered"

// MaxBackdate is how far into the past a manager may place a record.
//
// 🔴 IT IS NOT THE GUARDRAIL'S BOUND AND MUST NOT BE. internal/policy's
// sys:occurred-at-bound refuses a TAP whose declared time is more than
// OccurredAtSkewMax in the past, and its own comment exempts this path by name:
// manual entry "may legitimately backdate". That exemption is why a bound has to
// exist HERE instead -- an exemption from one rule is not an absence of all of them.
//
// NINETY DAYS, AND THE NUMBER IS CHOSEN AGAINST A FAILURE MODE RATHER THAN A FEELING.
// The realistic mistake on a date field is the YEAR: typing last year's, or next
// year's, while copying a date off a paper rota. Ninety days is long enough for a
// payroll cycle and a dispute about one, and short enough that a year typed wrong is
// refused rather than written into an immutable table. It is deliberately far LOOSER
// than the tap bound (72 h) because a manager reconstructing a month-old shift is the
// ordinary case here and impossible there.
const MaxBackdate = 90 * 24 * time.Hour

// FutureGrace is how far AHEAD of the server's clock a record may be placed.
//
// A record of work not yet done is not a record, so the answer is "essentially none".
// It is not zero because the manager types a WALL CLOCK and the two clocks are not
// the same one: somebody entering "the shift that is ending right now" at 17:00:00
// against a server a few seconds ahead would be refused for a reason no screen could
// explain. One minute absorbs that and nothing else.
const FutureGrace = time.Minute

// The refusals a caller must be able to tell apart, because each one is a different
// sentence to a manager (section 4.6: no silent failure).
var (
	// ErrUnknownEmployee: no employee with that id in this tenant. It covers "does
	// not exist" and "belongs to somebody else" together, deliberately -- telling
	// them apart would answer "does this id exist elsewhere?" for anybody who can
	// post a form, which is the enumeration oracle the sign-in flow refuses to be.
	// The same collapse internal/domain/tenant and internal/domain/review make.
	ErrUnknownEmployee = errors.New("manual: no such employee in this tenant")
	// ErrFuture: the instant is ahead of this server's clock by more than
	// FutureGrace.
	ErrFuture = errors.New("manual: that time has not happened yet")
	// ErrTooOld: the instant is further back than MaxBackdate.
	ErrTooOld = errors.New("manual: that time is too far in the past to enter here")
	// ErrDirection: the direction is neither of the two.
	ErrDirection = errors.New("manual: a record is either a check-in or a checkout")
)

// Database is the slice of the store this package needs, declared HERE at the
// consumer (section 7). One method: the tenant-scoped transaction. Nothing here can
// reach an unscoped pool.
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// Trail is the slice of internal/audit this package needs.
//
// 🔴 IT IS RecordTx AND NOT Record, WHICH IS THE OPPOSITE OF WHAT MOST CALLERS OF
// THAT PACKAGE WANT, and internal/domain/review and internal/domain/tenant state the
// same reasoning for the same reason. audit.Record opens its OWN transaction because
// the caller who most needs a trail row is usually the one whose main transaction
// FAILED. Here the two writes are one act and either direction is a false statement:
//
//	record written, audit row missing  an attendance record that appeared from
//	                                   nowhere, in a table nobody can clean up. The
//	                                   card's criterion is "an audit_log row is
//	                                   mandatory", and a row that can be lost
//	                                   independently does not satisfy it.
//	audit row, record missing          the trail says somebody's hours were entered
//	                                   while the report still shows the entry open. A
//	                                   trail that says something untrue is worse than
//	                                   no trail.
//
// ⚠️ IT IS ALSO NOT M6-07's CHOICE, AND THE DIFFERENCE IS THE DIRECTION OF THE
// REQUEST. The CSV export uses Record (its own transaction) because it is a READ and
// the audit row records that a file was produced; nothing would be inconsistent if
// the read succeeded and the row did not. This is a WRITE.
type Trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// Subject is who the record will be about, and the clock the manager is typing in.
//
// IT IS READ IN ONE TRANSACTION WITH TWO EXISTING STATEMENTS rather than through a
// new joined query: GetPanelEmployeeForAction (M6-05) already answers "who is this
// person and where do they work", and GetTenantClock (M6-03) already answers "what
// wall clock does this business keep". A third statement joining them would be a
// second representation of both.
type Subject struct {
	EmployeeID uuid.UUID
	Name       string
	// Status is the raw lifecycle value (invited | active | deactivated). It is NOT a
	// gate here -- see InsertManualTransaction's header for why a deactivated person
	// may still have a record typed for them -- it is what the screen says out loud.
	Status string
	// LocationName and DepartmentName are where the record will be filed, read from
	// the employee row. DepartmentName is "" for a business with no departments.
	LocationName   string
	DepartmentName string
	// Zone is the tenant's IANA timezone name. The render layer converts the
	// manager's wall clock with it, and shows it, so nobody types 17:00 meaning one
	// zone into a field the server reads as another.
	Zone string
}

// Deactivated reports whether this person's taps are already being refused. The
// screen says so before the button is pressed; nothing here refuses on it.
func (s Subject) Deactivated() bool { return s.Status == "deactivated" }

// Entry is what a manager submitted, after the boundary validated it (section 7).
type Entry struct {
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	// EnteredBy is the ADMINISTRATOR typing the record, taken from the signed panel
	// session by the handler -- NEVER from the request. It is required: the whole
	// argument for verdict='ok' rests on this column naming somebody answerable, so a
	// record with no administrator on it would be an unattributed declaration.
	EnteredBy uuid.UUID
	Direction Direction
	// At is the instant the record claims, in UTC. The manager typed a wall clock and
	// the render layer converted it against Subject.Zone (section 6).
	At time.Time
	// Note is the manager's optional sentence, already bounded and cleaned by the
	// handler.
	Note string
}

// Recorded is what was written, read back from the row itself.
//
// CreatedAt IS RETURNED BECAUSE THE CARD ASKS FOR IT: "backdating is possible but
// created_at shows the real moment of writing". It is the database's DEFAULT now(),
// never a value this package supplies, so the two timestamps cannot be made to agree
// by anything in Go.
type Recorded struct {
	ID         uuid.UUID
	At         time.Time
	CreatedAt  time.Time
	Direction  Direction
	Verdict    string
	Channel    string
	Trust      int16
	AuditID    uuid.UUID
	Backdated  bool
	EmployeeID uuid.UUID
}

// detail is the audit row's jsonb payload.
//
// 🔴 IT IS A PURPOSE-BUILT STRUCT WITH KNOWN KEYS, which is internal/audit's rule and
// the reason section 4.7 survives a jsonb column: nothing here is a dump of a
// request, a params struct, or a map of whatever was in scope. Walk the fields: a
// direction from a two-value vocabulary, two RFC3339 instants the server itself
// produced, a verdict this path cannot vary, an opaque row id, and a boolean. There
// is no coordinate, no address, no token and no free text -- the manager's note is
// NOT copied in, only whether there was one, for internal/domain/review's reason
// (it is already stored once, on the record, and a second copy in an append-only
// jsonb column doubles the surface for no investigative gain).
//
// BOTH TIMESTAMPS ARE HERE ON PURPOSE. The gap between them is the whole of what a
// later investigation would ask about a manual record -- "how long after the shift
// was this typed" -- and an audit row that carried only one of them would need a join
// back to a table nobody may modify to answer it.
type detail struct {
	Direction  string `json:"direction"`
	OccurredAt string `json:"occurred_at"`
	CreatedAt  string `json:"created_at"`
	Verdict    string `json:"verdict"`
	Channel    string `json:"channel"`
	RecordID   string `json:"record_id"`
	Backdated  bool   `json:"backdated"`
	HasNote    bool   `json:"has_note"`
}

// Recorder writes manager-entered records. Dependencies are injected as struct
// fields (section 7): no package-level state, no singleton.
type Recorder struct {
	data  Database
	trail Trail
	log   *slog.Logger
	// now is injectable for tests only; production leaves it nil. The bounds this
	// package enforces are against the SERVER's clock (ADR 0006's rule for the
	// debounce, applied to the one other place a client declares a time), and a test
	// that could not move that clock would have to sleep.
	now func() time.Time
}

// NewRecorder wires it. A nil database or a nil trail is REFUSED rather than
// tolerated: a recorder that cannot write the trail would append attendance records
// with no audit row, which is the state the card's fourth criterion forbids. The
// failure has to be impossible to construct, not merely unlikely -- the M5-04 lesson,
// where a capability was delivered, approved and dead in production because two
// halves were never assembled.
func NewRecorder(data Database, trail Trail, log *slog.Logger) (*Recorder, error) {
	switch {
	case data == nil:
		return nil, errors.New("manual: nil database")
	case trail == nil:
		return nil, errors.New("manual: nil audit trail")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Recorder{data: data, trail: trail, log: log}, nil
}

func (r *Recorder) clock() time.Time {
	if r.now != nil {
		return r.now()
	}
	return time.Now()
}

// Subject loads who a record would be about and the clock it would be typed in.
//
// IT IS A READ AND ONLY A READ. The form needs it to name a person, the confirmation
// screen needs it to state the instant in the tenant's own zone, and neither may
// invent either.
func (r *Recorder) Subject(ctx context.Context, tenantID, employeeID uuid.UUID) (Subject, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return Subject{}, ErrUnknownEmployee
	}
	var out Subject
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, err := q.GetPanelEmployeeForAction(ctx, store.GetPanelEmployeeForActionParams{
			TenantID: tenantID, ID: employeeID,
		})
		if err != nil {
			return err
		}
		clock, err := q.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load tenant clock: %w", err)
		}
		out = Subject{
			EmployeeID:     row.ID,
			Name:           row.FullName,
			Status:         row.Status,
			LocationName:   deref(row.LocationName),
			DepartmentName: deref(row.DepartmentName),
			Zone:           clock.Timezone,
		}
		return nil
	})
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, pgx.ErrNoRows):
		return Subject{}, ErrUnknownEmployee
	default:
		return Subject{}, fmt.Errorf("manual: load subject: %w", err)
	}
}

// Bounds checks the instant against the server's clock WITHOUT writing anything.
//
// 🔴 IT IS CALLED TWICE PER RECORD AND THAT IS THE DESIGN, NOT A DUPLICATE. The
// confirmation screen calls it so a manager is refused before being shown a warning
// about a record that could never be written; Record calls it again because a
// confirmation lasts ten minutes and the bound is measured against now. The two calls
// answer the same question at two instants, which is exactly what a time bound is
// for.
func (r *Recorder) Bounds(at time.Time) error {
	now := r.clock()
	switch {
	case at.After(now.Add(FutureGrace)):
		return ErrFuture
	case now.Sub(at) > MaxBackdate:
		return ErrTooOld
	}
	return nil
}

// Backdated reports whether this record is about a moment meaningfully before now.
//
// IT IS A PRESENTATION FACT AND A TRAIL FACT, not a gate: every value Bounds admits
// is writable. The threshold is FutureGrace's mirror -- anything inside a minute of
// now is "the shift that is ending right now" rather than a reconstruction.
func (r *Recorder) Backdated(at time.Time) bool {
	return r.clock().Sub(at) > FutureGrace
}

// Record appends the record and its trail in ONE transaction.
//
// 🔴 EVERY REFUSAL HAPPENS BEFORE THE WRITE, AND A REFUSAL WRITES NOTHING -- no
// record, no audit row. Section 4.6's question about a refused request ("what is the
// manager told, and what is left in the database") is answered by a typed error and
// by zero rows.
//
// 🔴 THE STATEMENT IS THE AUTHORITY ON SCOPE, NOT THIS FUNCTION. A foreign employee
// id produces no row, which arrives as pgx.ErrNoRows and becomes ErrUnknownEmployee
// -- so section 4.5's belt is the SELECT's own tenant predicate and the braces are
// the composite foreign key (employee_id, tenant_id). Nothing here compares tenants
// in Go.
func (r *Recorder) Record(ctx context.Context, e Entry) (Recorded, error) {
	switch {
	case e.TenantID == uuid.Nil:
		return Recorded{}, errors.New("manual: tenant id is required")
	case e.EmployeeID == uuid.Nil:
		return Recorded{}, ErrUnknownEmployee
	case e.EnteredBy == uuid.Nil:
		return Recorded{}, errors.New("manual: the administrator entering the record is required")
	}
	if _, ok := ParseDirection(string(e.Direction)); !ok {
		return Recorded{}, ErrDirection
	}
	if err := r.Bounds(e.At); err != nil {
		return Recorded{}, err
	}
	backdated := r.Backdated(e.At)

	var out Recorded
	err := r.data.WithTenant(ctx, e.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		params := store.InsertManualTransactionParams{
			TenantID:   e.TenantID,
			EmployeeID: e.EmployeeID,
			EnteredBy:  e.EnteredBy,
			Type:       string(e.Direction),
			// UTC IS RESTATED HERE EVEN THOUGH THE COLUMN IS timestamptz AND WOULD
			// STORE THE SAME INSTANT EITHER WAY. What it protects is the RETURNING
			// value: a time.Time carrying a location travels back into a template, and
			// section 6 puts exactly one conversion in the render layer.
			OccurredAt: e.At.UTC(),
			Trust:      tap.TrustBase,
		}
		if e.Note != "" {
			n := e.Note
			params.Note = &n
		}
		row, err := q.InsertManualTransaction(ctx, params)
		if err != nil {
			return err
		}
		out = Recorded{
			ID:         row.ID,
			At:         row.OccurredAt,
			CreatedAt:  row.CreatedAt,
			Direction:  Direction(deref(row.Type)),
			Verdict:    row.Verdict,
			Channel:    row.Channel,
			Backdated:  backdated,
			EmployeeID: e.EmployeeID,
		}
		if row.Trust != nil {
			out.Trust = *row.Trust
		}
		// THE TRAIL IS WRITTEN SECOND AND INSIDE THE SAME TRANSACTION. If it fails,
		// the record above is rolled back with it -- see Trail's comment for why that
		// is the right direction here and the wrong one almost everywhere else.
		auditID, err := r.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: e.TenantID,
			ActorID:  &e.EnteredBy,
			Action:   Action,
			Target:   e.EmployeeID.String(),
			Detail: detail{
				Direction: string(e.Direction),
				// BOTH INSTANTS COME FROM THE ROW, NOT FROM THE REQUEST. occurred_at is
				// what the database stored and created_at is what it stamped, so the
				// trail cannot disagree with the record it describes.
				OccurredAt: row.OccurredAt.UTC().Format(time.RFC3339),
				CreatedAt:  row.CreatedAt.UTC().Format(time.RFC3339),
				Verdict:    row.Verdict,
				Channel:    row.Channel,
				RecordID:   row.ID.String(),
				Backdated:  backdated,
				HasNote:    e.Note != "",
			},
		})
		if err != nil {
			return err
		}
		out.AuditID = auditID
		return nil
	})
	switch {
	case err == nil:
	case errors.Is(err, pgx.ErrNoRows):
		// 🔴 NO ROWS IS A REFUSAL HERE, NOT AN ABSENCE, and it is the statement's design
		// rather than an accident: the INSERT builds its row FROM an employee in this
		// tenant, so "nothing was inserted" means "that is not your employee". Treating
		// it as success would make this endpoint report records it never wrote.
		return Recorded{}, ErrUnknownEmployee
	default:
		return Recorded{}, fmt.Errorf("manual: record entry: %w", err)
	}
	// 🔴 THE NOTE IS NOT LOGGED. It is a manager's free text about a named person, so
	// it stays in the row it was written into; the ids below are opaque handles and
	// the direction is a two-value vocabulary (sections 4.7, 7).
	r.log.Info("attendance record entered by hand",
		"record_id", out.ID, "employee_id", e.EmployeeID, "entered_by", e.EnteredBy,
		"direction", string(e.Direction), "backdated", backdated)
	return out, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
