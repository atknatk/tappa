package tenant

// externalref.go -- the integration identity of an employee and of a venue (M11 API-1,
// ADR 0028 §9, K-18).
//
// An external ref is the key the customer's own HR/ERP system uses for a person or a
// venue; the integration API (API-5, API-6) addresses rows by it. Before the API is
// switched on, the OWNER binds the people and venues that already exist to their refs
// here, so the first sync updates them instead of opening a second row for each (K-18;
// venues are never matched by name).
//
// 🔴 OWNER-ONLY, AND THE REASON IS WHAT A REF ROUTES. Once the API is on, the
// integrator's writes for ref X land on whoever holds X -- a rename, a new address (and
// with it the next activation link), a deactivation. Binding X to the wrong person is
// therefore the employee-email takeover of ADR 0005 Y-D in another form, and it follows
// the email change's gate (handler: mayChangeEmployeeEmail's predicate). The domain does
// not see roles; the handler refuses a manager before calling here.
//
// THE DATABASE IS THE AUTHORITY ON THE SHAPE AND THE UNIQUENESS (migration 00037's
// CHECKs and indexes); the boundary below checks the same pattern so a manager reads a
// sentence before a round trip, and a 23505/23514 that does reach the database is
// recognised by its constraint NAME, never by its SQLSTATE alone.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// The audit actions of the two writes. The detail carries the ref before and after: a
// ref is the integrator's opaque identifier, not personal data in the sense of a name
// or an address, and "who bound which record to whom, when" is exactly what an
// investigation of a mis-routed sync has to answer.
const (
	ActionEmployeeExternalRefChanged = "employee.external_ref_changed"
	ActionLocationExternalRefChanged = "location.external_ref_changed"
)

// MaxExternalRefLength is the bound migration 00037's CHECK writes ({0,63} after the
// first character). The alphabet is ASCII, so bytes and runes agree.
const MaxExternalRefLength = 64

// externalRefPattern is migration 00037's CHECK, character for character: RFC 3986's
// unreserved set, starting with a letter or a digit (the ref ends up in a URL path, and
// "." / ".." are path segments, not identities).
var externalRefPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,63}$`)

var (
	// ErrExternalRef: the ref does not have the shape the integration API can carry.
	ErrExternalRef = errors.New("tenant: that external reference cannot be stored")
	// ErrExternalRefTaken: another live person (or another venue) of THIS business
	// already holds the ref. Tenant-scoped: it says nothing about any other business.
	ErrExternalRefTaken = errors.New("tenant: somebody in this business already has that external reference")
	// ErrSameExternalRef: the requested ref is the one on file -- nothing written, no
	// trail row (ErrSameEmail's rule).
	ErrSameExternalRef = errors.New("tenant: that is already the external reference on file")
	// ErrExternalRefRequired: removing the ref of somebody who has no home venue. The ref
	// is then their only anchor (K-14, employees_home_or_external_ref); moving them to a
	// venue first makes it removable.
	ErrExternalRefRequired = errors.New("tenant: this employee has no home venue, so the external reference cannot be removed")
)

// ExternalRef cleans and checks one submitted ref. Surrounding whitespace is trimmed;
// an EMPTY ref means "none" and returns nil. Anything else must match the schema's
// pattern exactly -- it is refused rather than altered, because a ref the manager did
// not type is a ref the integrator does not hold.
func ExternalRef(raw string) (*string, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return nil, nil
	}
	if !externalRefPattern.MatchString(s) {
		return nil, ErrExternalRef
	}
	return &s, nil
}

// ExternalRefCommand binds or unbinds one employee or one venue. SubjectID is the
// employee's id for Staff.ChangeExternalRef and the venue's for Venues.ChangeExternalRef.
type ExternalRefCommand struct {
	TenantID  uuid.UUID
	SubjectID uuid.UUID
	// ActorID is the ADMIN performing the act, from the signed panel session -- never
	// from the request.
	ActorID uuid.UUID
	// ExternalRef is the raw submitted value; "" removes the ref.
	ExternalRef string
}

// ExternalRefChange is what was written.
type ExternalRefChange struct {
	SubjectID uuid.UUID
	From      string
	To        string
}

// externalRefDetail is the trail row's payload: the ref before and after, "" for none.
type externalRefDetail struct {
	FromExternalRef string `json:"from_external_ref"`
	ToExternalRef   string `json:"to_external_ref"`
}

// ChangeExternalRef binds one employee to the integrator's identity, or unbinds them,
// and records it -- one transaction (Trail's rule).
func (s *Staff) ChangeExternalRef(ctx context.Context, c ExternalRefCommand) (ExternalRefChange, error) {
	if err := requireIDs(c.TenantID, c.SubjectID, c.ActorID); err != nil {
		return ExternalRefChange{}, err
	}
	ref, err := ExternalRef(c.ExternalRef)
	if err != nil {
		return ExternalRefChange{}, err
	}
	out := ExternalRefChange{SubjectID: c.SubjectID, To: deref(ref)}
	err = s.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		before, err := q.LockEmployeeForExternalRefChange(ctx, store.LockEmployeeForExternalRefChangeParams{
			TenantID: c.TenantID,
			ID:       c.SubjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownEmployee
			}
			return fmt.Errorf("lock employee: %w", err)
		}
		if deref(before.ExternalRef) == out.To {
			return ErrSameExternalRef
		}
		if ref == nil && before.LocationID == nil {
			// Said before the database would say it (employees_home_or_external_ref), so
			// the refusal names the reason rather than a constraint.
			return ErrExternalRefRequired
		}
		if _, err := q.SetEmployeeExternalRef(ctx, store.SetEmployeeExternalRefParams{
			ExternalRef: ref,
			TenantID:    c.TenantID,
			ID:          c.SubjectID,
		}); err != nil {
			switch constraintOf(err) {
			case "employees_tenant_external_ref_key":
				return ErrExternalRefTaken
			case "employees_external_ref_shape":
				return ErrExternalRef
			case "employees_home_or_external_ref":
				return ErrExternalRefRequired
			}
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownEmployee
			}
			return fmt.Errorf("set external ref: %w", err)
		}
		out.From = deref(before.ExternalRef)
		if _, err := s.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionEmployeeExternalRefChanged,
			Target:   c.SubjectID.String(),
			Detail:   externalRefDetail{FromExternalRef: out.From, ToExternalRef: out.To},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return ExternalRefChange{}, wrap("change external ref", err)
	}
	s.log.Info("employee external ref changed",
		"employee_id", c.SubjectID, "actor_id", c.ActorID, "has_external_ref", ref != nil)
	return out, nil
}

// ChangeExternalRef binds one venue to the integrator's identity, or unbinds it, and
// records it -- one transaction.
func (v *Venues) ChangeExternalRef(ctx context.Context, c ExternalRefCommand) (ExternalRefChange, error) {
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return ExternalRefChange{}, err
	}
	if c.SubjectID == uuid.Nil {
		return ExternalRefChange{}, ErrUnknownVenue
	}
	ref, err := ExternalRef(c.ExternalRef)
	if err != nil {
		return ExternalRefChange{}, err
	}
	out := ExternalRefChange{SubjectID: c.SubjectID, To: deref(ref)}
	err = v.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		before, err := q.LockLocationForExternalRefChange(ctx, store.LockLocationForExternalRefChangeParams{
			TenantID: c.TenantID,
			ID:       c.SubjectID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownVenue
			}
			return fmt.Errorf("lock venue: %w", err)
		}
		if deref(before.ExternalRef) == out.To {
			return ErrSameExternalRef
		}
		if _, err := q.SetLocationExternalRef(ctx, store.SetLocationExternalRefParams{
			ExternalRef: ref,
			TenantID:    c.TenantID,
			ID:          c.SubjectID,
		}); err != nil {
			switch constraintOf(err) {
			case "locations_tenant_external_ref_key":
				return ErrExternalRefTaken
			case "locations_external_ref_shape":
				return ErrExternalRef
			}
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownVenue
			}
			return fmt.Errorf("set external ref: %w", err)
		}
		out.From = deref(before.ExternalRef)
		if _, err := v.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionLocationExternalRefChanged,
			Target:   c.SubjectID.String(),
			Detail:   externalRefDetail{FromExternalRef: out.From, ToExternalRef: out.To},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return ExternalRefChange{}, wrapVenue("change external ref", err)
	}
	v.log.Info("venue external ref changed",
		"venue_id", c.SubjectID, "actor_id", c.ActorID, "has_external_ref", ref != nil)
	return out, nil
}

// constraintOf names the unique or check constraint a statement violated, "" for any
// other error. Recognising a refusal by NAME is isEmailTaken's rule: a bare 23505 could
// be a primary-key collision, which is a fault and not a sentence.
func constraintOf(err error) string {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || (pg.Code != "23505" && pg.Code != "23514") {
		return ""
	}
	return pg.ConstraintName
}
