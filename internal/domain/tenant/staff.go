package tenant

// staff.go -- the WRITE side of the employee lifecycle (M6-05 phase B): ending
// somebody's employment, and moving them between venues or departments.
//
// 🔴 WHY IT IS IN THIS PACKAGE. CLAUDE.md section 3 assigns "tenant / lokasyon /
// departman / calisan is kurallari" to internal/domain/tenant, and this file is the
// first thing that makes that sentence true for employees. The two alternatives
// were weighed rather than dismissed:
//
//	internal/domain/ledger   already holds the roster READ and already has the
//	                         section 4.5 scope net, so putting the writes there
//	                         would have cost nothing to build -- and would have
//	                         destroyed the one property that package advertises:
//	                         "no store call in ledger is not a SELECT", which is a
//	                         fact grep can check. roster.go says the write path does
//	                         not live there, and it is right.
//	a NEW package            starts with no scope net either way, so it buys
//	                         nothing this one does not.
//
// The cost of choosing here is a THIRD copy of the scope net (query_test.go), and
// that cost is real: this repository has already paid for the "second
// representation" failure class more than once. It is written down there rather
// than smuggled, together with what this copy does NOT carry.
//
// 🔴 WHAT THIS FILE DOES NOT DO, in the order somebody would expect it to:
//
//   - It does NOT revoke sessions when it deactivates somebody. That is a user
//     decision (2026-08-07) and db/queries/sessions.sql carries the argument at
//     RevokeSessionsForEmployee. Deactivate below states it again at the point a
//     future reader would add the call.
//   - It does NOT reactivate. There is no query for it and no method here; see
//     docs/adr/0010.
//   - It does NOT issue invitations. Minting a code is internal/invite's, and the
//     code leaves that package through exactly one seam (its Channel). A copy of
//     that flow here would be a second egress for a section 4.7 secret.
//   - It does NOT speak HTTP and does not render. It returns facts and typed
//     errors; internal/handler decides what a manager is told.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// Trail is the slice of internal/audit this file needs, declared HERE at the
// consumer (section 7).
//
// 🔴 IT IS RecordTx AND NOT Record, WHICH IS THE OPPOSITE OF WHAT MOST CALLERS OF
// THAT PACKAGE WANT, and internal/domain/review states the same reasoning for the
// same reason. audit.Record opens its OWN transaction because the caller who most
// needs a trail row is usually the one whose main transaction FAILED. Here the two
// writes are one act and either direction is a false statement:
//
//	employee changed, no audit row   an administrative act with no trail -- the
//	                                 half of section 4.6 this section exists to
//	                                 provide. The card's criterion is "every action
//	                                 is written to audit_log", and a row that can be
//	                                 lost independently does not satisfy it.
//	audit row, employee unchanged    the trail says somebody was deactivated while
//	                                 their taps still succeed. A trail that says
//	                                 something untrue is worse than no trail.
//
// Both directions are measured in staff_db_test.go by breaking each side in turn
// and counting rows.
type Trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

// Audit actions this file writes. The vocabulary is free text by schema decision
// (00005), so these constants are the vocabulary.
//
// ONE ACTION PER TRANSITION, with the FROM and TO facts in `detail`. An
// investigator asking "what has been done to this person" gets every answer from
// `target = <employee id>`, and `action LIKE 'employee.%'` is the whole of what the
// panel can do to somebody.
//
// ⚠️ THE INVITE ACTION IS NOT HERE, and its absence is deliberate rather than an
// omission: issuing an invitation writes invite.ActionCodeShownToManager
// ("invite.code_shown_to_manager") from internal/invite's Channel, which is the
// moment worth recording -- ADR 0005 Y-D's detection signal is who READ a code, not
// who created a row. Adding a second "employee.invited" row for the same act would
// double-count it and give an investigator two half-answers instead of one.
const (
	ActionEmployeeDeactivated = "employee.deactivated"
	ActionEmployeeMoved       = "employee.moved"
)

// The refusals a caller must be able to tell apart, because each one is a
// different sentence to a manager (section 4.6: no silent failure).
var (
	// ErrUnknownEmployee: no employee with that id in this tenant. It covers "does
	// not exist" and "belongs to somebody else" together, deliberately -- telling
	// them apart would answer "does this id exist elsewhere?" for anybody who can
	// post a form, which is the enumeration oracle the sign-in flow refuses to be.
	ErrUnknownEmployee = errors.New("tenant: no such employee in this tenant")

	// ErrAlreadyDeactivated: the person is already deactivated, so nothing was
	// written and no trail row was added. A double submission lands here.
	ErrAlreadyDeactivated = errors.New("tenant: this employee is already deactivated")

	// ErrUnknownPlacement: the requested placement cannot be written. THREE causes
	// collapse into it and the collapse is deliberate, for the reason
	// ErrUnknownEmployee gives: the venue is not this tenant's, the department is not
	// this tenant's, or -- the one a security audit found being accepted silently --
	// the department belongs to a DIFFERENT VENUE of this same tenant.
	//
	// 🔴 THE THIRD IS SECTION 5 RATHER THAN SECTION 4.5, and it is the one with teeth:
	// lateness is computed from the department's shift when there is one, and policies
	// can be scoped per department, so a mismatched pair judges somebody against
	// another branch's shift and another branch's rules. The screen names both
	// readings because the panel's dropdown offers every department in the business.
	ErrUnknownPlacement = errors.New("tenant: that venue and department cannot be paired in this tenant")

	// ErrSamePlacement: the requested placement is the one the person already has.
	//
	// 🔴 IT IS A REFUSAL RATHER THAN A NO-OP SUCCESS, and the difference is the
	// trail. Writing the row anyway would put "moved from A to A" in an append-only
	// table on every double submission and every stray refresh, which is how a trail
	// stops being readable. Telling the manager "that is where they already work" is
	// also the true sentence, and section 4.6 asks for the true one.
	ErrSamePlacement = errors.New("tenant: that is already this employee's placement")
)

// Person is one employee as the panel's action card reads them.
//
// 🔴 THE FIELDS THAT ARE NOT HERE ARE THE POINT (section 4.7), exactly as on
// ledger.Person: there is no Email, no invitation code, no code hash, no session
// token and no device label. The query does not select them, this struct has no
// field for them, and the view model above it has none either -- three independent
// walls.
type Person struct {
	ID   uuid.UUID
	Name string
	// Status is one of the three values migration 00003's CHECK admits. It is never
	// defaulted here: an empty string would mean the query stopped selecting it.
	Status     string
	LocationID uuid.UUID
	// DepartmentID is nil for a business that does not model departments (00003) --
	// a first-class state rather than a gap.
	DepartmentID *uuid.UUID
	// LocationName and DepartmentName are "" when there is no such name.
	// location_id is NOT NULL, so an empty LocationName means a join missed rather
	// than "no venue", and the screen says so in words.
	LocationName   string
	DepartmentName string
}

// Deactivated reports whether this person's taps are already being refused.
func (p Person) Deactivated() bool { return p.Status == statusDeactivated }

// Invitable reports whether an activation link would be usable by this person.
//
// IT IS THE SAME SET db/queries/invites.sql's consuming statement tests
// ('invited' or 'active'), spelled once here so the button and the database agree.
// An invitation minted for a deactivated employee would be a code that can never be
// spent -- the consuming statement refuses it -- so offering the control would be
// offering something the server cannot do.
func (p Person) Invitable() bool {
	return p.Status == statusInvited || p.Status == statusActive
}

// The lifecycle vocabulary migration 00003's CHECK admits. internal/domain/ledger
// exports the same three for the panel's dropdown; they are spelled again here
// rather than imported because a domain package importing another domain package
// for three strings is a dependency the two do not otherwise have.
const (
	statusInvited     = "invited"
	statusActive      = "active"
	statusDeactivated = "deactivated"
)

// Staff performs the manager's actions on one employee. Dependencies are injected
// as struct fields (section 7): no package-level state, no singleton.
type Staff struct {
	data  Database
	trail Trail
	log   *slog.Logger
}

// NewStaff wires it. A nil database or a nil trail is REFUSED rather than
// tolerated: a Staff that cannot write the trail would deactivate people with no
// audit row, which is the state the card's criterion exists to prevent. The M5-04
// lesson is that a capability can be delivered, approved and DEAD in the wired
// product because two halves were never assembled, so the failure has to be
// impossible to construct rather than merely unlikely.
func NewStaff(data Database, trail Trail, log *slog.Logger) (*Staff, error) {
	switch {
	case data == nil:
		return nil, errors.New("tenant: nil database")
	case trail == nil:
		return nil, errors.New("tenant: nil audit trail")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Staff{data: data, trail: trail, log: log}, nil
}

// Person loads one employee for the action card.
//
// pgx.ErrNoRows becomes ErrUnknownEmployee: a foreign or invented id is "not on
// this roster", never a database failure and never another tenant's person.
func (s *Staff) Person(ctx context.Context, tenantID, employeeID uuid.UUID) (Person, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return Person{}, ErrUnknownEmployee
	}
	var out Person
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetPanelEmployeeForAction(ctx, store.GetPanelEmployeeForActionParams{
			TenantID: tenantID,
			ID:       employeeID,
		})
		if err != nil {
			return err
		}
		out = person(row)
		return nil
	})
	switch {
	case err == nil:
		return out, nil
	case errors.Is(err, pgx.ErrNoRows):
		return Person{}, ErrUnknownEmployee
	default:
		return Person{}, fmt.Errorf("tenant: load employee: %w", err)
	}
}

// DeactivateCommand is what a manager submitted.
type DeactivateCommand struct {
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	// ActorID is the ADMIN performing the action, taken from the signed panel
	// session by the handler -- NEVER from the request. It is required: an
	// administrative act with no actor is an unattributable trail row, and
	// audit_log.actor_id is exactly the column that exists to answer "who".
	ActorID uuid.UUID
}

// deactivateDetail is the audit row's jsonb payload.
//
// 🔴 IT IS A PURPOSE-BUILT STRUCT WITH KNOWN KEYS, which is the rule internal/audit
// states and the reason section 4.7 survives a jsonb column: nothing here is a dump
// of a request or a map of whatever was in scope, so no future edit can add a
// secret to it by accident.
//
// THE NAME IS NOT COPIED IN. The row already carries the employee id as its target
// and audit_log is append-only; a person's name in a jsonb column is a second copy
// of personal data that nothing can ever correct or delete.
type deactivateDetail struct {
	// PreviousStatus is what the person was before this act -- 'invited' or
	// 'active'. It is the FROM half of the transition, read inside the same
	// transaction as the write, so it describes the row that actually changed.
	PreviousStatus string `json:"previous_status"`
}

// Deactivate ends somebody's employment and records that it did, in ONE
// transaction.
//
// 🔴 THE NEXT TAP BECOMES A REJECT AND THIS IS THE ONLY THING THAT MAKES IT SO.
// Section 5 row 4 wants a deactivated employee's tap REFUSED, RECORDED and alerted
// on; what produces all three is sys:employee-deactivated reading employees.status
// out of the policy context, so writing the column IS the mechanism.
//
// 🔴 AND THIS FUNCTION DELIBERATELY DOES NOT CALL RevokeSessionsForEmployee (user
// decision, 2026-08-07). It is written here because this is where the next reader
// will want to add it. Revoking would change nothing about the refusal -- the
// guardrail already denies on status alone -- and would move every later tap by
// that person off the branch where the outcome is CERTAIN (reject + RECORD + alert)
// onto the "revoked session" branch, whose correctness depends on each caller
// remembering to write the record anyway. That is a section 4.6 risk taken for no
// gain. revoked_at keeps its two jobs: a lost or stolen phone, and the second
// activation. tenant/staff_db_test.go pins the OUTCOME (a deactivated person's tap
// is rejected, alerted and RECORDED) rather than the absence alone, because a bare
// negative assertion is the shape that quietly empties out.
func (s *Staff) Deactivate(ctx context.Context, c DeactivateCommand) (Person, error) {
	if err := c.validate(); err != nil {
		return Person{}, err
	}
	var out Person
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		// THE READ IS INSIDE THE WRITE'S TRANSACTION so the trail's "previous status"
		// is the status of the row the UPDATE is about to change, not of a snapshot
		// taken a round trip earlier.
		before, err := q.GetPanelEmployeeForAction(ctx, store.GetPanelEmployeeForActionParams{
			TenantID: c.TenantID,
			ID:       c.EmployeeID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownEmployee
			}
			return fmt.Errorf("load employee: %w", err)
		}
		out = person(before)

		row, err := q.DeactivateEmployee(ctx, store.DeactivateEmployeeParams{
			TenantID: c.TenantID,
			ID:       c.EmployeeID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// The statement's own `status <> 'deactivated'` predicate refused it.
				// Nothing was written, so nothing is trailed: a second press is not a
				// second act.
				return ErrAlreadyDeactivated
			}
			return fmt.Errorf("deactivate: %w", err)
		}
		out.Status = row.Status

		// THE TRAIL IS WRITTEN INSIDE THE SAME TRANSACTION. If it fails, the status
		// change above rolls back with it -- see Trail for why that is right here and
		// wrong almost everywhere else in this codebase.
		if _, err := s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionEmployeeDeactivated,
			Target:   c.EmployeeID.String(),
			Detail:   deactivateDetail{PreviousStatus: before.Status},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return out, wrap("deactivate", err)
	}
	// 🔴 THE NAME IS NOT LOGGED. Employee ids are opaque handles; a person's name in
	// a process log is personal data in a place with no retention story (section 4.7,
	// section 7).
	s.log.Info("employee deactivated",
		"employee_id", c.EmployeeID, "actor_id", c.ActorID)
	return out, nil
}

// MoveCommand is a re-placement.
type MoveCommand struct {
	TenantID   uuid.UUID
	EmployeeID uuid.UUID
	ActorID    uuid.UUID
	// LocationID is the destination venue. It is REQUIRED because
	// employees.location_id is NOT NULL (00003): everybody has a home venue, so
	// there is no "no venue" to move to.
	LocationID uuid.UUID
	// DepartmentID is the destination department, or nil for "no department" --
	// which is a legitimate destination rather than a missing argument (00003: a bar
	// does not model departments).
	DepartmentID *uuid.UUID
}

// moveDetail is the audit row's payload: where they were, where they are.
//
// THE IDS ARE TEXT AND "" MEANS "no department". A uuid.UUID cannot express
// "absent" in JSON without marshalling as a zero uuid, which reads as a real id
// that happens to be all zeroes -- the trail must not be ambiguous about the state
// migration 00003 calls first class.
//
// NO NAMES, same rule as deactivateDetail: an id is a handle, a venue name is a
// second copy of a fact that lives in `locations`.
type moveDetail struct {
	FromLocationID   string `json:"from_location_id"`
	ToLocationID     string `json:"to_location_id"`
	FromDepartmentID string `json:"from_department_id"`
	ToDepartmentID   string `json:"to_department_id"`
}

// Move re-places somebody and records it, in ONE transaction.
//
// A MOVE IS NOT A LIFECYCLE CHANGE. The statement does not name status, so this
// cannot activate or deactivate anybody, and a DEACTIVATED person can be moved --
// deliberately: correcting where somebody worked is exactly the repair section 4.6
// wants to stay possible months later.
func (s *Staff) Move(ctx context.Context, c MoveCommand) (Person, error) {
	if err := c.validate(); err != nil {
		return Person{}, err
	}
	var out Person
	err := s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		before, err := q.GetPanelEmployeeForAction(ctx, store.GetPanelEmployeeForActionParams{
			TenantID: c.TenantID,
			ID:       c.EmployeeID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownEmployee
			}
			return fmt.Errorf("load employee: %w", err)
		}
		out = person(before)
		if before.LocationID == c.LocationID && sameDepartment(before.DepartmentID, c.DepartmentID) {
			return ErrSamePlacement
		}

		row, err := q.MoveEmployee(ctx, store.MoveEmployeeParams{
			TenantID:     c.TenantID,
			ID:           c.EmployeeID,
			LocationID:   c.LocationID,
			DepartmentID: c.DepartmentID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// The employee was found a moment ago inside this transaction, so zero
				// rows here means the PLACEMENT did not resolve: the statement's join
				// and its department guard are what refused it.
				return ErrUnknownPlacement
			}
			return fmt.Errorf("move: %w", err)
		}
		out.LocationID = row.LocationID
		out.DepartmentID = row.DepartmentID

		if _, err := s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionEmployeeMoved,
			Target:   c.EmployeeID.String(),
			Detail: moveDetail{
				FromLocationID:   before.LocationID.String(),
				ToLocationID:     row.LocationID.String(),
				FromDepartmentID: idText(before.DepartmentID),
				ToDepartmentID:   idText(row.DepartmentID),
			},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return out, wrap("move", err)
	}
	s.log.Info("employee moved",
		"employee_id", c.EmployeeID, "actor_id", c.ActorID,
		"location_id", c.LocationID)
	return out, nil
}

func (c DeactivateCommand) validate() error {
	return requireIDs(c.TenantID, c.EmployeeID, c.ActorID)
}

func (c MoveCommand) validate() error {
	if err := requireIDs(c.TenantID, c.EmployeeID, c.ActorID); err != nil {
		return err
	}
	if c.LocationID == uuid.Nil {
		return ErrUnknownPlacement
	}
	if c.DepartmentID != nil && *c.DepartmentID == uuid.Nil {
		// A zero uuid is not "no department" -- nil is. Accepting it would let a
		// caller express absence in two ways and only one of them is the one the
		// database understands.
		return ErrUnknownPlacement
	}
	return nil
}

// requireIDs refuses the zero value for the three ids every command needs.
//
// THE ACTOR IS AS REQUIRED AS THE SUBJECT. A command with no actor would write an
// audit row with a NULL actor_id, which the schema permits (00005: the column is
// polymorphic and nullable, for system events) and which would be a lie here --
// every action this file performs is performed BY somebody.
func requireIDs(tenantID, employeeID, actorID uuid.UUID) error {
	switch {
	case tenantID == uuid.Nil:
		return errors.New("tenant: tenant id is required")
	case actorID == uuid.Nil:
		return errors.New("tenant: actor id is required")
	case employeeID == uuid.Nil:
		return ErrUnknownEmployee
	}
	return nil
}

// wrap keeps the sentinels unwrapped-by-name so a caller can switch on them, and
// wraps everything else so a real failure reaches the handler as a failure. Guessing
// at an unknown error is how an outage gets rendered as "already deactivated".
func wrap(op string, err error) error {
	switch {
	case errors.Is(err, ErrUnknownEmployee),
		errors.Is(err, ErrAlreadyDeactivated),
		errors.Is(err, ErrUnknownPlacement),
		errors.Is(err, ErrSamePlacement):
		return err
	default:
		return fmt.Errorf("tenant: %s: %w", op, err)
	}
}

func person(row store.GetPanelEmployeeForActionRow) Person {
	return Person{
		ID:             row.ID,
		Name:           row.FullName,
		Status:         row.Status,
		LocationID:     row.LocationID,
		DepartmentID:   row.DepartmentID,
		LocationName:   deref(row.LocationName),
		DepartmentName: deref(row.DepartmentName),
	}
}

func sameDepartment(a, b *uuid.UUID) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	default:
		return *a == *b
	}
}

func idText(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
