// Package claim is the employee's claim of the missing half of a punch and the manager's decision
// on it (M15-16; ADR 0032 Faz C, §C.1–§C.5): "Yesterday you clocked in at 09:12 but there's no
// clock-out. What time did you leave?" on the result screen, and a manual record when a manager
// approves the answer.
//
// 🔴 A CLAIM IS NOT A RECORD (§C.1). Submit writes a punch_claims row and an audit_log row, never a
// transactions row: it moves no hour. Only an approval does, through the ONE writer of manual
// records (internal/domain/manual, its Within variant) -- the same statement narrowing, the same
// person's lock, the same race retry, the same trail row -- plus the decision row and its own trail
// row, in that one transaction. A rejection writes the decision and the trail row. Nothing here
// updates or deletes: both tables are append-only (migration 00068), transactions too (§4.3).
//
// 🔴 EVERY ANSWER ABOUT THE CHAIN IS THE READER'S, NOT THE TAP'S. Whether the anchor still misses
// its half, where the window ends and which two taps show the card are computed from the person's
// stored chain with tap.ClassifyCheckout (chain.go) -- the Go half of tappa_checkout_class, the
// classification the report and the anomaly lists read -- never from Decision.LeftOpen or
// Decision.MissingIn, which say what one tap did when it was decided. They are computed again under
// the person's lock at every Submit and every Decide, and the decision checks the pair it would
// write a second time by classifying it (Chain.Closes), so a window arithmetic error cannot become
// hours on its own.
//
// THE PREFILL IS THE SERVER'S (§C.2, the M15-15 security lens's handover): Submit computes it from
// the anchor's shift and the window under the lock, whatever the form showed, so "default not
// changed" (prefilled_at IS NOT NULL AND claimed_at = prefilled_at) is a statement about the shift,
// never about a value a client posted.
//
// 🔴 SELF-DECISION -- AN HONEST LIMIT, NOT A GUARANTEE (§C.4, G-O1). Decide compares the decider (an
// admin_users id) with the claimant (an employees id), and migration 00068's trigger compares them
// again; the two are separate id spaces, so today the comparison NEVER holds -- one person with a
// manager login and an employee session can approve their own claim. It is written so that M14's
// single identity (ADR 0031) has a line to make true; until then every decision names its decider in
// audit_log, and that is all.
//
// SUSPENSION (ADR 0025, §C.4): Decide is a panel write and belongs in mountWriting's main group,
// where a suspended business is refused; Submit is a tap surface (POST /api/claim, Tap.Mount) and
// does not know suspension. Neither is this package's to enforce -- the routes are M15-17's and
// M15-18's -- and nothing here reads the suspension.
//
// LOGGING: opaque ids, a kind, an outcome. Never the note, a name, an e-mail or a coordinate
// (CLAUDE.md §7, ADR 0032 §C.3).
package claim

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// Kind is the missing half a claim names -- migration 00068's punch_claims_kind_is_a_direction.
type Kind string

const (
	// KindIn: "What time did you come in?" -- the anchor is an orphan out ((b) or (c)).
	KindIn Kind = "in"
	// KindOut: "What time did you leave?" -- the anchor is an open in.
	KindOut Kind = "out"
)

// ParseKind reads a kind from the closed list; anything else is refused, never defaulted.
func ParseKind(s string) (Kind, bool) {
	switch k := Kind(s); k {
	case KindIn, KindOut:
		return k, true
	}
	return "", false
}

// direction is the transactions type of the claimed half.
func (k Kind) direction() manual.Direction { return manual.Direction(k) }

// Outcome is a decision's outcome -- migration 00068's punch_claim_decisions_outcome_is_known.
type Outcome string

const (
	Approved Outcome = "approved"
	Rejected Outcome = "rejected"
)

// The audit_log actions (the trail's "<noun>.<past participle>"). One name for both outcomes, with
// the outcome in detail -- internal/domain/review's reasoning: `action LIKE 'claim.%'` is the whole
// answer to "what was claimed and decided here". An approval ALSO writes manual.Action
// (record.entered) for the record it appends: that row is about the record, this one about the claim.
const (
	ActionSubmitted = "claim.submitted"
	ActionDecided   = "claim.decided"
)

// ClaimApprovedNote is the note on the manual record an approval writes: where the record came from,
// in the record itself, for the transactions list and the export. No secret, no free text -- the
// manager's own note is the decision's (punch_claim_decisions.note), stored once.
const ClaimApprovedNote = "approved from the employee's claim"

// The refusals a caller tells apart -- each is a different sentence (§4.6: nothing fails silently).
var (
	// ErrUnknownAnchor: no record with that id of this person's in this tenant (another tenant's,
	// another person's, none) -- one sentinel, the enumeration-oracle collapse manual and review make.
	ErrUnknownAnchor = errors.New("claim: no such record of yours")
	// ErrUnknownClaim: no claim with that id in this tenant.
	ErrUnknownClaim = errors.New("claim: no such claim")
	// ErrNotActive: the claimant is not active (deactivated -- §C.5 -- or not yet activated): no new
	// claim. A pending claim stays decidable.
	ErrNotActive = errors.New("claim: only an active employee can send a claim")
	// ErrResolved: the anchor no longer misses that half (another record closed it, or it never did,
	// or it has no business day -- Ü-Y2), or the pair an approval would write does not close it.
	// "Already resolved" -- only Reject is left.
	ErrResolved = errors.New("claim: that record no longer misses this half")
	// ErrOutsideWindow: the moment is outside the claim's window (§C.3), or no window exists.
	ErrOutsideWindow = errors.New("claim: that time is outside what can be claimed for this record")
	// ErrExpired: the moment is further back than manual.MaxBackdate (§C.5): no record can be written
	// for it any more; the queue says "expired" and offers Reject.
	ErrExpired = errors.New("claim: that time is too far in the past to be recorded")
	// ErrAlreadySent: this record already carries a claim (23505 punch_claims_one_per_anchor). The
	// handler answers it with the "Sent" screen, never a 500 (§C.5).
	ErrAlreadySent = errors.New("claim: a claim for this record was already sent")
	// ErrAlreadyDecided: the claim already carries a decision (ADR 0009: final).
	ErrAlreadyDecided = errors.New("claim: already decided")
	// ErrSelfDecision: the decider is the claimant -- unreachable today (the package doc, G-O1).
	ErrSelfDecision = errors.New("claim: a claim cannot be decided by its claimant")
	// ErrNeedsConfirmation: the approval's moment is outside the shift's band, or there is no shift
	// (§C.4, K3, G-Y1): nothing was written; the decision must be posted again with Confirmed.
	ErrNeedsConfirmation = errors.New("claim: this approval needs the confirmation step")
	// ErrPartnershipEnded: the anchor was taken at a holding peer's venue and that partnership has
	// ended (or the venue is no longer the peer's): the approval's record cannot be filed there and
	// nothing was written (manual's strict site gate, ADR 0027 §6). Only Reject is left. It replaces
	// manual.ErrVenue on this path, whose sentence ("no home venue; choose the venue") is a manager's
	// form instruction that does not fit a claim.
	ErrPartnershipEnded = errors.New("claim: the partnership the record was taken in has ended")
	// ErrNoVenue: the anchor names no venue (a plaque on no wall) and the person has no home venue,
	// so the approval's record has nowhere to be filed; nothing was written.
	ErrNoVenue = errors.New("claim: there is no venue to file the answer at")
	// ErrRefused: the database refused the row (a trigger of migration 00068). The domain's checks
	// make it unreachable; it is a 500's sentence, not a manager's.
	ErrRefused = errors.New("claim: the database refused the row")
)

// errChainTooLong: the chain read reached its row limit. A truncated chain is not judged.
var errChainTooLong = errors.New("claim: the person's chain around the record is longer than this read allows")

// chainRowLimit bounds ListPunchClaimChain. A read spans about two business days of one person; a
// person taps at most once a minute past the debounce, and the limit is far above any real day.
const chainRowLimit = 1000

// chainBefore is how far before the anchor (or the tap) the chain read reaches: a business day is at
// most 26 hours long (the M15-03 property test), so the anchor's day start and the 18 hours before
// it -- every bound the window and the classification read -- are inside it; the record right before
// the range comes with it (ListPunchClaimChain's before_range row).
const chainBefore = 27 * time.Hour

// Database is the slice of *db.DB this package needs, declared at the consumer (§7).
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// Trail is the slice of internal/audit this package needs: RecordTx, because a claim and its trail
// row are one act -- manual.Trail's reasoning.
type Trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// Recorder is the slice of internal/domain/manual a decision needs: the person's transaction (lock,
// race retry) with the manual record's write inside it.
type Recorder interface {
	Within(ctx context.Context, tenantID, employeeID uuid.UUID, fn manual.TxFunc) error
}

// Service submits and decides claims. Dependencies are struct fields (§7).
type Service struct {
	data  Database
	trail Trail
	rec   Recorder
	log   *slog.Logger
	// businessDayFrom is the business-day switch (config.BusinessDayFrom, ADR 0032 §A.8): the
	// person's cutoff applies through tap.SwitchedDayCutoff -- the one gate -- at the server's now.
	businessDayFrom *time.Time
	// now is injectable for tests only.
	now func() time.Time
	// afterCheck is injectable for tests only: it runs inside Submit's and Decide's transaction
	// after every check and before the first write -- the one place a test can hold a claim or a
	// decision whose checks passed while somebody else acts on the person.
	afterCheck func()
}

// New wires a Service. A nil database, trail or recorder is refused: a claim service that could
// not write its trail would leave claims nobody can account for.
func New(data Database, trail Trail, rec Recorder, businessDayFrom *time.Time, log *slog.Logger) (*Service, error) {
	switch {
	case data == nil:
		return nil, errors.New("claim: nil database")
	case trail == nil:
		return nil, errors.New("claim: nil audit trail")
	case rec == nil:
		return nil, errors.New("claim: nil manual recorder")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Service{data: data, trail: trail, rec: rec, log: log, businessDayFrom: businessDayFrom}, nil
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// person is what a claim reads about the person: their lifecycle and their business day.
type person struct {
	status string
	zone   string
	cutoff *tap.DayCutoff
}

// readPerson reads the person through GetEmployeeForTap -- the tap's own read of the status, the
// zone and the resolved cutoff -- and gates the cutoff through the switch at now.
func (s *Service) readPerson(ctx context.Context, q *store.Queries, tenantID, employeeID uuid.UUID, now time.Time) (person, error) {
	emp, err := q.GetEmployeeForTap(ctx, store.GetEmployeeForTapParams{TenantID: tenantID, EmployeeID: employeeID})
	if err != nil {
		return person{}, err
	}
	p := person{status: emp.Status, zone: emp.TenantTimezone}
	if emp.EffectiveDayCutoff.Valid {
		p.cutoff = tap.SwitchedDayCutoff(s.businessDayFrom, now,
			time.Duration(emp.EffectiveDayCutoff.Microseconds)*time.Microsecond, emp.TenantTimezone)
	}
	return p, nil
}

// anchorRecord is GetPunchClaimAnchor's row in domain terms.
type anchorRecord struct {
	id        uuid.UUID
	direction *tap.Type
	channel   tap.Channel
	// source is who chose the anchor's direction, read through tap.RecordedDirectionSource (ADR 0032
	// §B.4's one reader rule); sourceKnown is its ok -- false on a manual row (whatever is stored
	// there is not believed) and on an unknown value.
	source         tap.DirectionSource
	sourceKnown    bool
	at             time.Time
	dayStart       *time.Time
	practice       bool
	locationID     *uuid.UUID
	siteLocationID *uuid.UUID
	shift          *tap.Shift
}

func (s *Service) readAnchor(ctx context.Context, q *store.Queries, tenantID, employeeID, id uuid.UUID, zone string) (anchorRecord, error) {
	row, err := q.GetPunchClaimAnchor(ctx, store.GetPunchClaimAnchorParams{TenantID: tenantID, ID: id, EmployeeID: employeeID})
	if err != nil {
		return anchorRecord{}, err
	}
	return anchorOf(row, zone), nil
}

// anchorOf maps the row. The direction's source goes through tap.RecordedDirectionSource and
// nowhere else (M15-18, the card's step 4): the stored column is never compared raw, so a manual
// row carrying 'employee' (no code path writes one; the schema does not refuse it) is no mark.
func anchorOf(row store.GetPunchClaimAnchorRow, zone string) anchorRecord {
	a := anchorRecord{id: row.ID, channel: tap.Channel(row.Channel), at: row.OccurredAt, dayStart: row.BusinessDayStart,
		practice: row.Practice, locationID: row.LocationID, siteLocationID: row.SiteLocationID,
		shift: shiftOf(row.ShiftStart, row.ShiftEnd, row.ShiftOvernight, zone)}
	if row.Type != nil {
		d := tap.Type(*row.Type)
		a.direction = &d
	}
	a.source, a.sourceKnown = tap.RecordedDirectionSource(a.channel, a.direction, (*tap.DirectionSource)(row.DirectionSource))
	return a
}

// shiftOf is internal/domain/checkin's shiftOf: a stored shift in the domain shape, nil without one.
func shiftOf(start, end pgtype.Time, overnight bool, zone string) *tap.Shift {
	if !start.Valid || !end.Valid {
		return nil
	}
	return &tap.Shift{
		Start:     time.Duration(start.Microseconds) * time.Microsecond,
		End:       time.Duration(end.Microseconds) * time.Microsecond,
		Overnight: overnight,
		Timezone:  zone,
	}
}

// readChain reads the person's chain in [from, to] and the record right before it.
func readChain(ctx context.Context, q *store.Queries, tenantID, employeeID uuid.UUID, from, to time.Time) (Chain, error) {
	rows, err := q.ListPunchClaimChain(ctx, store.ListPunchClaimChainParams{
		TenantID: tenantID, EmployeeID: employeeID, FromAt: from, ToAt: to, RowLimit: chainRowLimit,
	})
	if err != nil {
		return Chain{}, fmt.Errorf("read the chain: %w", err)
	}
	complete, inRange := true, 0
	events := make([]Event, 0, len(rows))
	for _, r := range rows {
		if r.BeforeRange {
			complete = false
		} else {
			inRange++
		}
		if r.Type == nil {
			continue // the statement selects directed rows only; a NULL here is no event
		}
		events = append(events, Event{ID: r.ID, Direction: tap.Type(*r.Type), At: r.OccurredAt, WrittenAt: r.CreatedAt,
			DayStart: r.BusinessDayStart, Channel: tap.Channel(r.Channel)})
	}
	if inRange >= chainRowLimit {
		return Chain{}, errChainTooLong
	}
	return NewChain(events, complete), nil
}

// chainAround is the chain a claim on the record at `at` is judged on: the anchor's business day and
// the 18 hours on either side (WindowOf and Closes read nothing further).
func chainAround(ctx context.Context, q *store.Queries, tenantID, employeeID uuid.UUID, at time.Time) (Chain, error) {
	return readChain(ctx, q, tenantID, employeeID, at.Add(-chainBefore), at.Add(tap.StaleOpenIn))
}

// pgCode is the SQLSTATE and constraint of a database error, or "".
func pgCode(err error) (code, constraint, message string) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code, pg.ConstraintName, pg.Message
	}
	return "", "", ""
}

// rfc3339 renders an instant for an audit detail: UTC, seconds.
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }
