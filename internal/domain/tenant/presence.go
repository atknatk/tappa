package tenant

// presence.go -- the staff app's "at work" facts (M16-45, ADR 0050 (ii), (iv)): when the person
// clocked in, when the phone may remind them to clock out, how long the lock-screen timer may run,
// and their usual shift. The app computes NONE of this -- no direction, no zone, no shift: it shows
// the instants and the server-formatted clocks it is given (§6: local time only where the server
// renders it).
//
// NO PUSH, NO LOCATION (§4.2). These instants become a LOCAL notification and a lock-screen timer
// the phone schedules for itself; the server never wakes the phone and never learns where it is.

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

const (
	// remindAfterIn is the reminder's base for a check-in with no real shift (Q51 (2) (b)): an
	// 8-hour shift, a break and a margin. The measured closed pairs (M16-42 (1)) put 0 of 45
	// between 10 and 12 hours, so the fixed 10 h contradicts no measured KF day.
	remindAfterIn = 10 * time.Hour
	// remindAfterShift is the shift arm's offset past the shift's end (Q51 (1) (a)).
	remindAfterShift = 30 * time.Minute
	// remindBeforeStale clips the reminder to before the business day's end (Q51 (2) (d)): after
	// OpenUntil a tap records a new check-in, so a "clock out" reminder must come before it.
	remindBeforeStale = 30 * time.Minute
	// remindNotBefore drops a reminder the clip would put right after the check-in (a 22:45
	// check-in against a 23:00 cutoff): no "still at work?" an hour after arriving.
	remindNotBefore = time.Hour
	// showAfterRemind is how long past the reminder the lock-screen timer keeps saying "Clocked
	// in" without news (Q53 (3) (a)): a checkout in the browser cannot reach the phone.
	showAfterRemind = 2 * time.Hour
)

// Presence is an open check-in the next tap would close, seen from the staff app.
type Presence struct {
	// InSince is the open check-in's instant.
	InSince time.Time
	// ShiftEndsAt is the end of the shift the check-in was judged against (its frozen shift,
	// 00095), only when the owner marked the shifts real AND that end lies in (InSince, StaleAt].
	ShiftEndsAt *time.Time
	// RemindAt is when the phone may say "Still at work?"; nil when the clip leaves no sensible
	// moment.
	RemindAt *time.Time
	// ShowUntil bounds the lock-screen timer: min(reminder + 2 h, StaleAt), StaleAt with no
	// reminder. Never after StaleAt.
	ShowUntil time.Time
	// StaleAt is tap.OpenUntil: from here the check-in is left open, not closable.
	StaleAt time.Time
	// Timezone is the business's zone, for the server-side HH:MM fields.
	Timezone string
}

// UsualShift is the person's usual shift: their department's, else their profile location's. It
// is a daily wall-clock pair -- there is no roster, so it never names a date (Q52 (1) (a)).
type UsualShift struct {
	Start, End time.Duration
	Overnight  bool
	Venue      string
}

// presenceOf computes the presence of an open check-in at `in`. frozen is the check-in's frozen
// shift (nil for shift_source 'none' or NULL); shiftReal is the owner's switch.
func presenceOf(in time.Time, frozen *tap.Shift, shiftReal bool, cutoff *tap.DayCutoff, zone string) Presence {
	p := Presence{InSince: in, StaleAt: tap.OpenUntil(in, cutoff), Timezone: zone}
	if shiftReal && frozen != nil {
		// tap.ShiftSpanAt clamps an instant to a shift instance; one that does not hold the
		// check-in (an 02:30 check-in against 18:00-02:00 gives the NEXT evening's) is no "end of
		// this shift" -- the claim prefill's rule (claim.Prefill).
		if _, end, ok := tap.ShiftSpanAt(in, *frozen); ok && end.After(in) && !end.After(p.StaleAt) {
			p.ShiftEndsAt = &end
		}
	}
	base := in.Add(remindAfterIn)
	if p.ShiftEndsAt != nil {
		base = p.ShiftEndsAt.Add(remindAfterShift)
	}
	if clip := p.StaleAt.Add(-remindBeforeStale); clip.Before(base) {
		base = clip
	}
	p.ShowUntil = p.StaleAt
	if !base.Before(in.Add(remindNotBefore)) {
		r := base
		p.RemindAt = &r
		if until := r.Add(showAfterRemind); until.Before(p.ShowUntil) {
			p.ShowUntil = until
		}
	}
	return p
}

// frozenShiftOf reads an open check-in row's frozen shift (00095): nil when it was judged
// against none ('none', or NULL for a row written before 00095) -- checkin.shiftOf's rule.
func frozenShiftOf(row *store.Transaction) *tap.Shift {
	if row.ShiftSource == nil || *row.ShiftSource == "none" || row.ShiftTimezone == nil {
		return nil
	}
	return shiftFromTimes(row.ShiftStart, row.ShiftEnd, row.ShiftOvernight != nil && *row.ShiftOvernight, *row.ShiftTimezone)
}

func shiftFromTimes(start, end pgtype.Time, overnight bool, zone string) *tap.Shift {
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

// usualShiftOf is the department's shift when it is set, else the profile location's; nil when
// the owner has not marked the shifts real or neither is set.
func usualShiftOf(row store.GetEmployeeUsualShiftRow) *UsualShift {
	if !row.StaffShiftVisible {
		return nil
	}
	venue := ""
	if row.LocationName != nil { // no home venue (K-14): the line names none
		venue = *row.LocationName
	}
	if row.DepartmentShiftStart.Valid && row.DepartmentShiftEnd.Valid {
		return &UsualShift{
			Start:     time.Duration(row.DepartmentShiftStart.Microseconds) * time.Microsecond,
			End:       time.Duration(row.DepartmentShiftEnd.Microseconds) * time.Microsecond,
			Overnight: row.DepartmentOvernight != nil && *row.DepartmentOvernight,
			Venue:     venue,
		}
	}
	if row.LocationShiftStart.Valid && row.LocationShiftEnd.Valid {
		return &UsualShift{
			Start:     time.Duration(row.LocationShiftStart.Microseconds) * time.Microsecond,
			End:       time.Duration(row.LocationShiftEnd.Microseconds) * time.Microsecond,
			Overnight: row.LocationOvernight != nil && *row.LocationOvernight,
			Venue:     venue,
		}
	}
	return nil
}
