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
	"time"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
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
	// businessDayFrom is the business-day switch (TAPPA_BUSINESS_DAY_FROM, ADR 0032 §A.8), nil
	// when it is unset. It is the SAME value checkin.Service carries — cmd/tappa hands both the
	// one config field — so the GET /t prediction (M15-12) and the record cannot run by two
	// rules. Injected as a field (CLAUDE.md §7): this package does not import checkin and checkin
	// does not import this one; both ask tap.SwitchedDayCutoff.
	businessDayFrom *time.Time
	// clock is the SERVER's clock the tap screen's prediction reads its instant from (M15-12):
	// the switch is judged at it (security lens F-2) and the open check-in and the business day
	// are read at it. time.Now in production; a test of this package sets it. No caller hands an
	// instant in -- TapPage and PeerTapPage take none -- so a declared one cannot reach the gate.
	clock func() time.Time
}

// NewDirectory wires a Directory. A nil database is refused rather than
// deferred to a nil dereference on the first tap. businessDayFrom is the
// business-day switch's value (config.Config.BusinessDayFrom), nil when unset;
// it is copied, so the caller's later writes do not reach it.
func NewDirectory(data Database, businessDayFrom *time.Time) (*Directory, error) {
	if data == nil {
		return nil, errors.New("tenant: nil database")
	}
	d := &Directory{data: data, clock: time.Now}
	if businessDayFrom != nil {
		v := *businessDayFrom
		d.businessDayFrom = &v
	}
	return d, nil
}

// DayCutoff is the business-day cutoff the tap screen's prediction must use (ADR 0032 §A.8,
// §B.2): tap.SwitchedDayCutoff over this directory's switch — nil while the switch is off, so a
// prediction is made by the same rule the orchestrator then decides by. now is the SERVER's
// clock (never a declared instant — security lens F-2); cutoff and timezone are the person's
// resolved effective_day_cutoff and the tenant's zone. Its caller is M15-12's GET /t.
func (d *Directory) DayCutoff(now time.Time, cutoff time.Duration, timezone string) *tap.DayCutoff {
	return tap.SwitchedDayCutoff(d.businessDayFrom, now, cutoff, timezone)
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
// there is nothing on it to read. The one fact about the person's day it carries is
// the predicted direction (M15-12, ADR 0032 §B.1-§B.2) -- the word on the button,
// a user decision under CLAUDE.md §9 -- and only through its gate (Prediction).
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
	// Prediction is the engine's direction for a tap NOW (tap.PredictDirection), or nil. It
	// is read only when the caller asked for it (predict: the plaque and channel half of
	// ADR 0032 §B.2's gate, which the handler judges from the SUN preview), the person is
	// active, and the business-day switch is on at the server's clock (Directory.DayCutoff,
	// the ONE gate the orchestrator also asks). nil as well for another business's plaque,
	// for a plaque on no wall, and whenever a read failed -- the brand's included, because a
	// failed statement aborts the transaction the prediction is read in. A non-nil value may
	// still carry no BusinessDayStart (a zone the server cannot load): the engine then drops
	// a request, so the screen must not offer one either -- the handler's rule.
	Prediction *tap.Prediction
	// PredictionErr is why the prediction read failed, for the caller's log; the page is
	// drawn without a prediction (§4.6: a prediction never costs the page). It is never
	// rendered.
	PredictionErr error
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
//
// THE PREDICTION COMES AFTER THE BRAND (M15-12), and only when predict is set -- the
// plaque and channel half of ADR 0032 §B.2's gate, which the caller judges from the SUN
// preview. It is never read for a foreign plaque or one on no wall. When it fails the
// facts above it still reach the page: the transaction is rolled back the brand's way,
// Prediction is nil and PredictionErr carries the cause, and the error is nil -- a
// prediction never costs the page (§4.6). A brand failure ends the transaction before it,
// so a page without its brand also has no prediction.
func (d *Directory) TapPage(ctx context.Context, tenantID, employeeID, locationID uuid.UUID, predict bool) (TapPageFacts, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return TapPageFacts{}, errors.New("tenant: tap page: tenant and employee are required")
	}
	var (
		out      TapPageFacts
		brandErr error
		predErr  error
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
		if predict {
			if out.Prediction, predErr = d.predictTap(ctx, q, tenantID, employeeID); predErr != nil {
				return errPredictionRollback
			}
		}
		return nil
	})
	if errors.Is(err, errBrandRollback) {
		return out, fmt.Errorf("%w: %w", ErrBrandUnread, brandErr)
	}
	if errors.Is(err, errPredictionRollback) {
		out.Prediction, out.PredictionErr = nil, fmt.Errorf("tenant: tap page: %w", predErr)
		return out, nil
	}
	if err != nil {
		return TapPageFacts{}, fmt.Errorf("tenant: tap page: %w", err)
	}
	if foreign {
		return out, ErrForeignLocation
	}
	return out, nil
}
