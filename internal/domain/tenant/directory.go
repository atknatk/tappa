// Package tenant holds the tenant-side reads and rules — tenants, locations,
// departments and employees (CLAUDE.md §3). It exists so the HTTP layer can ask
// a question in domain terms instead of holding SQL: §3 is explicit that no
// business rule and no query lives in internal/handler, and the alternative
// (two store calls inside the tap handler) would put both there.
//
// It is deliberately thin. The tap page needs two display strings and, since M10
// WL-9, its business's brand (pagebrand.go), so that is what Directory offers; it
// will grow when M6 needs it to, not before.
package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Database is the narrow slice of *db.DB this package needs, declared HERE at
// the consumer (§7). Only WithTenant: everything this package reads is
// tenant-scoped, and the context-less resolvers (ADR 0002 item 7) belong to the
// callers that produce a tenant, not to one that already has it.
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// Directory answers "who is this and where are they" inside one tenant.
type Directory struct {
	data Database
}

// NewDirectory wires a Directory. A nil database is refused rather than
// deferred to a nil dereference on the first tap.
func NewDirectory(data Database) (*Directory, error) {
	if data == nil {
		return nil, errors.New("tenant: nil database")
	}
	return &Directory{data: data}, nil
}

// ErrForeignLocation reports that the location id does not belong to the tenant
// the read ran in.
//
// IT COMES BACK WITH A POPULATED TapPageFacts, on purpose — the same contract
// invite.Lookup makes with ErrCodeUsed and session.Verify makes with ErrRevoked,
// for the same reason (§4.6): the caller still has to serve a page and, later,
// record an attempt, and it cannot do either from a zero value. The employee's
// name is filled in; only LocationName is empty.
//
// ⚠️ IT IS NOT A TENANT-MISMATCH DECISION AND MUST NOT BE READ AS ONE. Whether a
// tap on another tenant's plaque is allowed is the sys:tenant-mismatch
// guardrail's answer (hand-off N5, state.md), it needs a RECORDED decision, and
// it belongs to M5-05 via the policy engine. All this error says is that a name
// could not be shown — a display fact, chosen so that one tenant's venue name is
// never rendered to another tenant's employee.
var ErrForeignLocation = errors.New("tenant: location does not belong to this tenant")

// TapPageFacts is everything the tap screen displays. Two strings, because the
// screen is one greeting, one venue and one button (CLAUDE.md §9), and since M10
// WL-9 the business's brand, which the screen draws in two slots the user decided
// (D-C, K-2a, K-2b: the header and the button's fill; ADR 0023 §2) and nowhere else.
//
// There is deliberately no status, no id and no shift here. A view model with a
// field cannot help rendering it, and the tap screen's whole discipline is that
// there is nothing on it to read.
type TapPageFacts struct {
	// EmployeeName greets the person: "Hello Maria".
	EmployeeName string
	// LocationName is the TAPPED venue — the plaque in front of them, not the
	// location on their profile (§5: a chain moves people between branches, and
	// the screen must say where it thinks they are). Empty when the plaque
	// belongs to another tenant; see ErrForeignLocation.
	LocationName string
	// Brand is the session's business's brand, read in the same transaction as
	// the two strings above. It is the ZERO value whenever LocationName is empty
	// (ErrForeignLocation: the plaque is another business's, or on no wall -- ADR
	// 0023 §2: either business's brand would be wrong there) and whenever the read
	// failed (ErrBrandUnread). The zero value is Taptime's own screen.
	Brand PageBrand
}

// TapPage loads the two display facts for a tap.
//
// employeeID comes from the session cookie and locationID from resolving the
// TAG — both server-produced; neither is a value the client chose. tenantID is
// the SESSION's tenant, and that choice is doing work:
//
//   - it is the tenant the employee is actually in, so the greeting is read
//     under the right policy;
//   - the location read is scoped to it as well, so a plaque belonging to
//     ANOTHER tenant returns no row instead of leaking that tenant's venue name
//     onto this employee's screen. That is a disclosure choice, not the
//     isolation defence — see ErrForeignLocation.
//
// The reads run in ONE transaction, so the venue named on the page belongs to
// the same snapshot as the employee row. Every query carries an explicit
// tenant_id predicate on top of RLS (§4.5, belt and braces).
//
// THE BRAND IS THE THIRD READ AND THE LAST STATEMENT (M10 WL-9; ADR 0024's WL-6
// hand-off: hasLogo is read in this transaction). It is skipped when the plaque is
// foreign -- the page then draws Taptime's own look -- and when it fails the first
// two reads still reach the page: the transaction is rolled back (a failed
// statement aborts it, so a commit would fail), the facts are returned with a zero
// Brand, and the error is ErrBrandUnread wrapping the cause (§4.6: a brand never
// costs the page). The business's name for the logo's alt comes from the employee
// read, which already joins it.
func (d *Directory) TapPage(ctx context.Context, tenantID, employeeID, locationID uuid.UUID) (TapPageFacts, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return TapPageFacts{}, errors.New("tenant: tap page: tenant and employee are required")
	}
	var (
		out      TapPageFacts
		brandErr error
	)
	foreign := false
	err := d.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		emp, err := q.GetEmployeeActivationContext(ctx, store.GetEmployeeActivationContextParams{
			TenantID:   tenantID,
			EmployeeID: employeeID,
		})
		if err != nil {
			return fmt.Errorf("load employee: %w", err)
		}
		out.EmployeeName = emp.FullName

		if locationID == uuid.Nil {
			foreign = true
			return nil
		}
		venue, err := q.GetLocationWiFi(ctx, store.GetLocationWiFiParams{
			TenantID: tenantID,
			ID:       locationID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				// Not this tenant's plaque. Recorded as a flag rather than
				// returned from here, so the transaction commits and the
				// employee's name survives — the caller needs it.
				foreign = true
				return nil
			}
			return fmt.Errorf("load venue: %w", err)
		}
		out.LocationName = venue.Name

		b, err := pageBrandOf(q.GetTenantBrand(ctx, tenantID))
		if err != nil {
			brandErr = err
			return errBrandRollback
		}
		if b.HasLogo {
			b.Name = emp.TenantName
		}
		out.Brand = b
		return nil
	})
	if errors.Is(err, errBrandRollback) {
		return out, fmt.Errorf("%w: %w", ErrBrandUnread, brandErr)
	}
	if err != nil {
		return TapPageFacts{}, fmt.Errorf("tenant: tap page: %w", err)
	}
	if foreign {
		return out, ErrForeignLocation
	}
	return out, nil
}
