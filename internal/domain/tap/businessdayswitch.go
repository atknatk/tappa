package tap

import "time"

// SwitchedDayCutoff is the ONE gate of the business-day switch, TAPPA_BUSINESS_DAY_FROM
// (ADR 0032 §A.8): the person's resolved cutoff in its zone when the switch is on, nil
// when it is off. ON ⇔ a value is set AND now ≥ that value.
//
// `now` IS THE SERVER'S CLOCK, NEVER THE DECLARED occurred_at (security lens F-2). The
// declared instant belongs to the client: comparing the switch with it would let a POST
// that backdates its tap below the switch's instant take the old rule after the switch
// is on. A queued tap that was declared before the switch but ARRIVES after it is judged
// by the new rule, which is what the panel's announcement says ("records received
// before that were paired by the old rule").
//
// nil is not 05:00 and not "no tap": it is Input.DayCutoff's nil (§A.3) — StaleOpenIn
// alone decides the direction, no shift is cut, Decision.BusinessDayStart stays nil and
// the row's business_day_start is NULL. The engine never invents a cutoff, and neither
// does this gate: an unresolvable cutoff or zone it passes through is the engine's
// "no business day".
//
// ONE FUNCTION FOR EVERY CALLER: the orchestrator's gather (internal/domain/checkin)
// and the GET /t prediction (tenant.Directory, M15-12) both call it, so the screen
// cannot predict by one rule while the record is decided by another. It is pure: the
// caller passes the switch's value and its clock; there is no package-level state
// (CLAUDE.md §7).
func SwitchedDayCutoff(from *time.Time, now time.Time, cutoff time.Duration, timezone string) *DayCutoff {
	if from == nil || now.Before(*from) {
		return nil
	}
	return &DayCutoff{Cutoff: cutoff, Timezone: timezone}
}
