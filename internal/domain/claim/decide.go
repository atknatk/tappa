package claim

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/store"
)

// Decision is what a manager posted on one claim, after the handler validated it (§7).
type Decision struct {
	TenantID uuid.UUID
	ClaimID  uuid.UUID
	// DecidedBy is the administrator, from the signed panel session -- never from the request.
	DecidedBy uuid.UUID
	Outcome   Outcome
	// At is the manager's Edit: the moment to record instead of the claimed one (nil approves the
	// claimed moment). Read on an approval only. It is held to the same window, the same closing
	// check and the same band as the claimed moment (ADR 0032 §C.4, ürün N8: "Edit'le gir, adımı
	// atla" must not be a shortcut).
	At *time.Time
	// Confirmed is the confirmation step's second post: an approval outside the band (or with no
	// shift) writes only with it.
	Confirmed bool
	// Note is the manager's optional sentence, already bounded by the handler. Stored on the
	// decision once; never logged, never in audit_log.
	Note string
}

// Decided is a decision as written -- or, with ErrNeedsConfirmation, the assessment the
// confirmation step repeats (nothing written).
type Decided struct {
	ID         uuid.UUID
	Outcome    Outcome
	RecordID   *uuid.UUID
	ApprovedAt *time.Time
	// Assessment is the approval's (nil on a rejection).
	Assessment *Assessment
	AuditID    uuid.UUID
}

// decideDetail is claim.decided's audit detail: ids, an outcome, the server's instants, booleans. The
// manager's note is NOT copied (only whether there was one -- manual's and review's rule).
type decideDetail struct {
	ClaimID              string `json:"claim_id"`
	AnchorID             string `json:"anchor_id"`
	Kind                 string `json:"kind"`
	Outcome              string `json:"outcome"`
	ClaimedAt            string `json:"claimed_at"`
	ApprovedAt           string `json:"approved_at,omitempty"`
	RecordID             string `json:"record_id,omitempty"`
	Edited               bool   `json:"edited"`
	RequiredConfirmation bool   `json:"required_confirmation"`
	HasNote              bool   `json:"has_note"`
}

// State is where a claim stands for its decision.
type State string

const (
	// StatePending: undecided, and an approval at the assessed moment would write.
	StatePending State = "pending"
	// StateDecided: a decision exists (ADR 0009: final).
	StateDecided State = "decided"
	// StateResolved: the anchor no longer misses the half, or the pair would not close it -- "already
	// resolved", only Reject (§C.5).
	StateResolved State = "resolved"
	// StateOutsideWindow: the assessed moment is outside the window (an Edit, or a window a later
	// record narrowed) -- Reject, or an Edit inside it.
	StateOutsideWindow State = "outside_window"
	// StateExpired: the moment is further back than manual.MaxBackdate -- "expired", only Reject.
	StateExpired State = "expired"
)

// evaluation is the chain's answer about one claim at one moment -- the same function behind
// Inspect and Decide, so the queue cannot show one thing and the decision do another.
type evaluation struct {
	state      State
	anchor     anchorRecord
	chain      Chain
	index      int
	window     *Window
	assessment Assessment
}

// evaluate judges an approval of claim c at `at`, read in the person's tenant (Decide: under the lock).
func (s *Service) evaluate(ctx context.Context, q *store.Queries, tenantID uuid.UUID, c store.GetPunchClaimForDecisionRow, at, now time.Time) (evaluation, error) {
	ev := evaluation{index: -1}
	p, err := s.readPerson(ctx, q, tenantID, c.EmployeeID, now)
	if err != nil {
		return ev, fmt.Errorf("read the person: %w", err)
	}
	ev.anchor, err = s.readAnchor(ctx, q, tenantID, c.EmployeeID, c.AnchorTransactionID, p.zone)
	if err != nil {
		return ev, fmt.Errorf("read the anchor: %w", err)
	}
	kind := Kind(c.Kind)
	ev.assessment = Assess(kind, ev.anchor.at, at, ev.anchor.shift, c.PrefilledAt)
	if c.DecidedOutcome != nil {
		ev.state = StateDecided
		return ev, nil
	}
	ev.chain, err = chainAround(ctx, q, tenantID, c.EmployeeID, ev.anchor.at)
	if err != nil {
		return ev, err
	}
	ev.index = ev.chain.Index(ev.anchor.id)
	if ev.index < 0 {
		ev.state = StateResolved
		return ev, nil
	}
	if k, ok := ev.chain.Missing(ev.index); !ok || k != kind {
		ev.state = StateResolved
		return ev, nil
	}
	w, ok := ev.chain.WindowOf(ev.index, p.cutoff, now)
	if ok {
		ev.window = &w
	}
	switch {
	case !ok || !w.Contains(at):
		ev.state = StateOutsideWindow
	case !ev.chain.Closes(ev.index, at, p.cutoff):
		ev.state = StateResolved
	case now.Sub(at) > manual.MaxBackdate:
		ev.state = StateExpired
	default:
		ev.state = StatePending
	}
	return ev, nil
}

// Inspection is what the queue's focus shows about one claim (ADR 0032 §C.4): the claim, its anchor
// and the anchor's neighbours, where it stands, and the assessment of approving it at the assessed
// moment -- computed here, rendered there.
type Inspection struct {
	ClaimID    uuid.UUID
	EmployeeID uuid.UUID
	Kind       Kind
	ClaimedAt  time.Time
	Prefilled  *time.Time
	CreatedAt  time.Time
	// Anchor is the record the claim names; Before and After are the directed records right before
	// and after it in the chain -- After only within the StaleOpenIn the claim is judged in (a record
	// later than that bounds no window). nil for none, and when the claim is decided (not read then).
	Anchor        Event
	Before, After *Event
	Decided       *Outcome
	State         State
	Window        *Window
	Assessment    Assessment
}

// Inspect reads one claim for the queue, assessed at `at` (nil: the claimed moment). It writes
// nothing and takes no lock -- Decide judges again under it.
func (s *Service) Inspect(ctx context.Context, tenantID, claimID uuid.UUID, at *time.Time) (Inspection, error) {
	if tenantID == uuid.Nil || claimID == uuid.Nil {
		return Inspection{}, ErrUnknownClaim
	}
	now := s.clock()
	var out Inspection
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		c, err := q.GetPunchClaimForDecision(ctx, store.GetPunchClaimForDecisionParams{TenantID: tenantID, ID: claimID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUnknownClaim
		}
		if err != nil {
			return fmt.Errorf("read the claim: %w", err)
		}
		moment := c.ClaimedAt
		if at != nil {
			moment = *at
		}
		ev, err := s.evaluate(ctx, q, tenantID, c, moment, now)
		if err != nil {
			return err
		}
		out = Inspection{ClaimID: c.ID, EmployeeID: c.EmployeeID, Kind: Kind(c.Kind), ClaimedAt: c.ClaimedAt,
			Prefilled: c.PrefilledAt, CreatedAt: c.CreatedAt, State: ev.state, Window: ev.window, Assessment: ev.assessment,
			Anchor: Event{ID: ev.anchor.id, At: ev.anchor.at, DayStart: ev.anchor.dayStart}}
		if ev.anchor.direction != nil {
			out.Anchor.Direction = *ev.anchor.direction
		}
		if c.DecidedOutcome != nil {
			o := Outcome(*c.DecidedOutcome)
			out.Decided = &o
		}
		if ev.index >= 0 {
			events := ev.chain.events
			out.Anchor = events[ev.index]
			if ev.index > 0 {
				b := events[ev.index-1]
				out.Before = &b
			}
			if ev.index+1 < len(events) {
				a := events[ev.index+1]
				out.After = &a
			}
		}
		return nil
	})
	if err != nil {
		if isTyped(err) {
			return Inspection{}, err
		}
		return Inspection{}, fmt.Errorf("claim: inspect: %w", err)
	}
	return out, nil
}

// Decide records the manager's decision on a claim (ADR 0032 §C.3–§C.5), in ONE transaction of the
// person's -- manual.Recorder.Within: the person's tap lock first, the race retry, then:
//
//	the claim undecided (ADR 0009)                                else ErrAlreadyDecided
//	the decider is not the claimant (unreachable today, G-O1)     else ErrSelfDecision
//	reject: the decision row and its trail row.
//	approve, at the claimed moment or the Edit:
//	  the anchor still misses that half (the reader's class)      else ErrResolved
//	  the moment is inside the window                             else ErrOutsideWindow
//	  the pair the record would make truly closes (Chain.Closes)  else ErrResolved
//	  the moment within manual.MaxBackdate                        else ErrExpired
//	  inside the band, or Confirmed                               else ErrNeedsConfirmation
//	  the manual record at the anchor's venue (manual's write: its trail row, record.entered; a
//	  peer's venue through the partner path, ErrVenue when the membership ended), the decision row,
//	  its trail row.
//
// Every refusal writes nothing; a failure at any later step rolls the record back with it -- an
// approval half done is no row at all. A pending claim of a deactivated person stays decidable
// (§C.5): nothing here reads the status.
func (s *Service) Decide(ctx context.Context, d Decision) (Decided, error) {
	switch {
	case d.TenantID == uuid.Nil:
		return Decided{}, errors.New("claim: tenant id is required")
	case d.ClaimID == uuid.Nil:
		return Decided{}, ErrUnknownClaim
	case d.DecidedBy == uuid.Nil:
		return Decided{}, errors.New("claim: the deciding administrator is required")
	case d.Outcome != Approved && d.Outcome != Rejected:
		return Decided{}, errors.New("claim: an outcome is approved or rejected")
	}
	// The claim names the person whose lock the decision takes. It is read before the lock -- a claim
	// row never changes (§4.3) -- and read again under it, where its decision is the answer.
	var employeeID uuid.UUID
	if err := s.data.WithTenant(ctx, d.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		c, err := store.New(tx).GetPunchClaimForDecision(ctx, store.GetPunchClaimForDecisionParams{TenantID: d.TenantID, ID: d.ClaimID})
		if err != nil {
			return err
		}
		employeeID = c.EmployeeID
		return nil
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Decided{}, ErrUnknownClaim
		}
		return Decided{}, fmt.Errorf("claim: decide: read the claim: %w", err)
	}

	var out Decided
	err := s.rec.Within(ctx, d.TenantID, employeeID, func(ctx context.Context, tx pgx.Tx, write manual.Write) error {
		out = Decided{}
		now := s.clock()
		q := store.New(tx)
		c, err := q.GetPunchClaimForDecision(ctx, store.GetPunchClaimForDecisionParams{TenantID: d.TenantID, ID: d.ClaimID})
		if err != nil {
			return fmt.Errorf("read the claim again: %w", err)
		}
		if c.DecidedOutcome != nil {
			return ErrAlreadyDecided
		}
		// 🔴 NOT A GUARANTEE (the package doc, G-O1): an admin_users id is never an employees id
		// today, so this never holds; M14's single identity is what can make it mean something.
		if d.DecidedBy == c.EmployeeID {
			return ErrSelfDecision
		}
		detail := decideDetail{ClaimID: c.ID.String(), AnchorID: c.AnchorTransactionID.String(), Kind: c.Kind,
			Outcome: string(d.Outcome), ClaimedAt: rfc3339(c.ClaimedAt), HasNote: d.Note != ""}
		params := store.InsertPunchClaimDecisionParams{Outcome: string(d.Outcome), DecidedBy: d.DecidedBy,
			ClaimID: c.ID, TenantID: d.TenantID}
		if d.Note != "" {
			n := d.Note
			params.Note = &n
		}
		if d.Outcome == Approved {
			at := c.ClaimedAt
			if d.At != nil {
				at = *d.At
				detail.Edited = !at.Equal(c.ClaimedAt)
			}
			ev, err := s.evaluate(ctx, q, d.TenantID, c, at, now)
			if err != nil {
				return err
			}
			a := ev.assessment
			out.Assessment = &a
			switch ev.state {
			case StateResolved:
				return ErrResolved
			case StateOutsideWindow:
				return ErrOutsideWindow
			case StateExpired:
				return ErrExpired
			case StateDecided:
				return ErrAlreadyDecided
			}
			if a.RequiresConfirmation && !d.Confirmed {
				return ErrNeedsConfirmation
			}
			detail.RequiredConfirmation = a.RequiresConfirmation
			if s.afterCheck != nil {
				s.afterCheck()
			}
			entry := manual.Entry{TenantID: d.TenantID, EmployeeID: c.EmployeeID, EnteredBy: d.DecidedBy,
				Direction: Kind(c.Kind).direction(), At: at.UTC(), Note: ClaimApprovedNote}
			switch {
			case ev.anchor.siteLocationID != nil:
				entry.SiteLocationID = *ev.anchor.siteLocationID
			case ev.anchor.locationID != nil:
				entry.AnchorID = ev.anchor.id
			}
			rec, err := write(entry)
			switch {
			case errors.Is(err, manual.ErrTooOld):
				return ErrExpired
			case errors.Is(err, manual.ErrFuture):
				return ErrOutsideWindow
			case err != nil:
				return err
			}
			id, approvedAt := rec.ID, rec.At
			params.TransactionID, params.ApprovedAt = &id, &approvedAt
			out.RecordID, out.ApprovedAt = &id, &approvedAt
			detail.RecordID, detail.ApprovedAt = id.String(), rfc3339(approvedAt)
		} else if s.afterCheck != nil {
			s.afterCheck()
		}
		row, err := q.InsertPunchClaimDecision(ctx, params)
		if err != nil {
			return insertDecisionError(err)
		}
		out.ID, out.Outcome = row.ID, d.Outcome
		out.AuditID, err = s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: d.TenantID, ActorID: &d.DecidedBy, Action: ActionDecided, Target: c.EmployeeID.String(), Detail: detail,
		})
		return err
	})
	if err != nil {
		if errors.Is(err, ErrNeedsConfirmation) {
			return Decided{Assessment: out.Assessment}, ErrNeedsConfirmation
		}
		if isTyped(err) {
			return Decided{}, err
		}
		return Decided{}, fmt.Errorf("claim: decide: %w", err)
	}
	s.log.Info("punch claim decided", "claim_id", d.ClaimID, "employee_id", employeeID, "decided_by", d.DecidedBy,
		"outcome", string(d.Outcome), "record_id", out.RecordID)
	return out, nil
}

// insertDecisionError maps InsertPunchClaimDecision's refusals.
func insertDecisionError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUnknownClaim
	}
	switch code, constraint, msg := pgCode(err); {
	case code == "23505" && constraint == "punch_claim_decisions_one_per_claim":
		return ErrAlreadyDecided
	case code == "23514" && strings.Contains(msg, "the decider is the claimant"):
		return ErrSelfDecision
	case code == "23514":
		return ErrRefused
	}
	return err
}
