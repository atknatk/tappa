package tenant

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// AppHomeFacts is what the native app's single tap screen shows BEFORE a plaque is read (GET
// /app/v1/me, M16-07; Q41 (a), the user's decision of 2026-11-14): an iPhone 7-X reads the plaque
// only inside the app, after a press, so the screen must already say the person's name, wear the
// business's brand and name the button -- "Clock in" / "Clock out" -- and the press then starts
// the NFC read. It is TapPageFacts without the venue: there is no plaque yet, so there is no
// venue to name.
type AppHomeFacts struct {
	// EmployeeName greets the person.
	EmployeeName string
	// Brand is the session's business's brand, the tap screen's two slots (the logo, with the
	// business's name as its alt, and the accent) -- read and gated exactly as TapPage reads it.
	// Zero when the read failed (ErrBrandUnread).
	Brand PageBrand
	// Prediction is the engine's direction for a tap NOW, through the SAME function and the SAME
	// person-and-switch half of the gate as GET /t's (predictTap: the person active, the
	// business-day switch on at the server's clock). nil when the gate is closed or a read
	// failed; PredictionErr then says why, for the caller's log.
	Prediction    *tap.Prediction
	PredictionErr error
	// Presence is the open check-in the next tap would close (M16-45): only with a prediction of
	// `out` -- a check-in left open from an earlier business day, or stale, is not "at work". It is
	// computed from the SAME row and cutoff the prediction read.
	Presence *Presence
	// Shift is the person's usual shift, only when the owner marked the shifts real (00116).
	// ShiftErr says why it is nil after a failed read (the caller logs it; the rest stands).
	Shift    *UsualShift
	ShiftErr error
	// HoursWeeks is the owner's My hours window (00117, M16-30): 0 when off or unread.
	HoursWeeks int
}

// AppHome reads the app's home screen for one device's person (tenantID and employeeID come from
// a VERIFIED signed read, appdevice.Verifier.VerifyRead -- never from the request body).
//
// THE PREDICTION WITHOUT A PLAQUE (Q41's "Doğrula", measured in apphome_db_test.go). GET /t's
// gate has two halves (ADR 0032 §B.2): the plaque-and-channel half (predictionAllowed in
// internal/handler: an active plaque whose CMAC verified, or a QR) and the person-and-switch half
// (predictTap here). The prediction ITSELF reads no plaque: the business day is the person's own
// effective_day_cutoff (ADR 0032 K1 -- the profile's, not the tapped venue's), the open check-in
// is the person's, and the clock is the server's. So AppHome runs predictTap unchanged and
// answers exactly what TapPage answers for the same person at the same instant when the plaque
// half is open. What it does NOT have is the plaque half: there is no plaque to judge. That half
// exists because the browser page renders for a dead plaque and a bad CMAC (§4.6: the POST
// records the refusal), and the prediction says whether this person is at work -- shown only on
// a genuine touch. Here the request is signed by a key that cannot leave the phone, so what the
// prediction tells is the device holder's own state, to the device holder; ADR 0032 §B.2 already
// accepts the browser's twin of that (a saved URL plus the cookie shows its holder a prediction).
//
// The reads run in ONE transaction (the employee, the brand, the prediction), and a failure of the
// brand or of the prediction costs only that part -- TapPage's rollback rules, word for word.
// errShiftRollback ends the home screen's read when the usual-shift read failed (errBrandRollback's
// reasoning: the reads before it are in Go values and the transaction is read-only).
var errShiftRollback = errors.New("tenant: usual shift read failed; rolling back the home screen's reads")

func (d *Directory) AppHome(ctx context.Context, tenantID, employeeID uuid.UUID) (AppHomeFacts, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil {
		return AppHomeFacts{}, errors.New("tenant: app home: tenant and employee are required")
	}
	var (
		out      AppHomeFacts
		brandErr error
		predErr  error
	)
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

		b, err := pageBrandOf(q.GetTenantBrand(ctx, tenantID))
		if err != nil {
			brandErr = err
			return errBrandRollback
		}
		if b.HasLogo {
			b.Name = emp.TenantName
		}
		out.Brand = b
		f, err := d.predictTapFacts(ctx, q, tenantID, employeeID)
		if err != nil {
			predErr = err
			return errPredictionRollback
		}
		out.Prediction = f.prediction
		if f.prediction == nil {
			// A person who is not active, or a closed business-day switch: no presence and no shift
			// (the prediction's own gate).
			return nil
		}
		row, err := q.GetEmployeeUsualShift(ctx, store.GetEmployeeUsualShiftParams{TenantID: tenantID, EmployeeID: employeeID})
		if err != nil {
			out.ShiftErr = fmt.Errorf("load the usual shift: %w", err)
			return errShiftRollback
		}
		out.Shift = usualShiftOf(row)
		if row.StaffHoursWeeks != nil {
			out.HoursWeeks = int(*row.StaffHoursWeeks)
		}
		if f.prediction.Direction == tap.TypeOut && f.open != nil {
			p := presenceOf(f.open.OccurredAt, frozenShiftOf(f.open), row.StaffShiftVisible, f.cutoff, f.cutoff.Timezone)
			out.Presence = &p
		}
		return nil
	})
	if errors.Is(err, errShiftRollback) {
		// The prediction is already in Go; without the switch's answer no shift is used and the
		// presence (which needs the switch) is dropped too -- never a guessed "not real".
		out.Presence, out.Shift = nil, nil
		return out, nil
	}
	if errors.Is(err, errBrandRollback) {
		return out, fmt.Errorf("%w: %w", ErrBrandUnread, brandErr)
	}
	if errors.Is(err, errPredictionRollback) {
		out.Prediction, out.PredictionErr = nil, fmt.Errorf("tenant: app home: %w", predErr)
		return out, nil
	}
	if err != nil {
		return AppHomeFacts{}, fmt.Errorf("tenant: app home: %w", err)
	}
	return out, nil
}
