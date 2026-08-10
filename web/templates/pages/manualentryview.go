package pages

// The MANUAL RECORD ENTRY screen's view model (M6-08).
//
// 🔴 IT IS THE PANEL'S ONLY WRITE FORM WHOSE PRODUCT IS PERMANENT, and the fields
// below are arranged around that. `transactions` takes no UPDATE and no DELETE (by
// privilege and by trigger, migration 00005), and §4.3's remedy — "a correction is a
// new row" — was measured against the report engine and only works in half the
// directions (internal/domain/manual's CorrectionsOnlyShorten). So this screen has a
// second step whose entire purpose is to say what is about to be written, in the
// tenant's own wall clock, before anybody presses anything.
//
// 🔴 §4.7 — THERE IS NO FIELD A SECRET OR A COORDINATE COULD TRAVEL IN. No latitude,
// no longitude, no source address, no session token, no invitation code. The write
// statement has no column for the first three and the two steps of this flow exchange
// a person, a direction, a wall clock and a sentence.
//
// ⚠️ ConfirmToken IS NOT A SECRET IN THE §4.7 SENSE. It is a MAC over server-chosen
// fields, rendered into the page on purpose — exactly like the login form's
// synchronizer token — and the cookie half is what makes it worth anything.
//
// EVERY INSTANT IS ALREADY A STRING. §6 keeps instants UTC below the render layer and
// converts once, in the handler, against the zone the database gave for this tenant. A
// template that did its own conversion would be a second answer to the one question
// this screen must not get wrong.
type ManualEntryView struct {
	PanelChrome

	// Action is where both steps post: the form's own URL. It is a FIELD rather than a
	// literal in the template because the server owns the address — M6-04 measured
	// what a hand-written form target costs, when changing the template's action to a
	// dead path left the whole handler package green over real HTTP.
	Action string
	// RecordAction is where the CONFIRMED record posts. It is empty on the first step.
	//
	// ⚠️ THAT EMPTINESS IS A CONSEQUENCE, NOT A BARRIER, and this comment claimed the
	// opposite until a mutation refuted it: setting RecordAction on the first step
	// changes nothing, because the first step does not render it. What keeps the write
	// form off that step is the template's `if v.Confirming`, and that is what
	// TestManualForm_NeverRendersTheWriteFormNoMatterWhatTheViewCarries asserts.
	RecordAction string
	// BackHref returns to the person's card on the roster — where the manager came
	// from, and where every other action on them lives.
	BackHref string

	// EmployeeID is echoed as a hidden field so the second step names the same person
	// the first one did. It is a narrowing value inside the caller's own tenant: the
	// tenant predicate it is ANDed with is not one the client supplied.
	EmployeeID string

	// Name, Venue and Department are read from the database in the SAME request that
	// renders the screen, so what the manager acts on is what they see.
	//
	// Venue is "" only if the join missed — employees.location_id is NOT NULL — so an
	// empty string is a data defect rather than "no venue", and the template says so
	// in words instead of leaving a blank that reads as "none". Department is ""
	// for a business that does not model departments, which is a first-class state.
	Name       string
	Venue      string
	Department string
	// Deactivated is SAID, NEVER ENFORCED here. Somebody's last shift is often typed
	// after they have left, and refusing it would create an unpayable shift with no
	// remedy: deactivation is one-way (docs/adr/0010) and this table takes no UPDATE.
	// The screen tells the manager who they are recording for.
	Deactivated bool

	// Zone is the IANA name every wall clock on this screen is read and printed in. It
	// is DISPLAYED rather than assumed, because the whole screen is a person typing a
	// time and a server deciding which instant that is (§6).
	Zone string

	// Directions is the closed vocabulary, supplied by the handler from THE SAME LIST
	// IT VALIDATES AGAINST — so the control cannot offer a value the server would
	// refuse, and the server cannot accept one the control never showed.
	Directions []ManualDirectionView

	// The three things the manager typed, echoed back so a refusal does not lose their
	// work. Date and Time are the wall clock; Direction is the chosen value.
	Date      string
	Time      string
	Direction string
	Note      string
	// MaxNote is the note box's maxlength. It is a FIELD rather than a number in the
	// template because the server enforces the same bound and a browser attribute is a
	// courtesy rather than a limit — two copies of it is how one of them drifts.
	MaxNote int

	// Problem is one word from the handler's closed vocabulary, or "". A reflected
	// parameter is a value somebody can put anything in, and the answer is not to
	// escape it more carefully but to have nothing to escape.
	Problem string

	// --- the second step -----------------------------------------------------------
	//
	// Confirming is true once the server has resolved the submission and is showing
	// what it would write. It gates the whole confirmation block, so the permanence
	// warning and the write button appear together or not at all.
	Confirming bool
	// WhenLocal is the resolved instant in the tenant's zone, spelled out with its
	// weekday: the one line a manager has to read before pressing.
	//
	// 🔴 THE WEEKDAY IS THERE ON PURPOSE. The mistake this screen exists to catch is a
	// date typed from a paper rota — a wrong year, a wrong day — and a bare
	// "2026-08-05" is the same shape as the thing that was typed. "Wednesday 5 August"
	// is checkable against a memory of the shift.
	WhenLocal string
	// WhenUTC is the same instant in ISO 8601, mono, beside it. It is what the
	// database will hold, and printing both is the M6-07 CSV's rule applied to a
	// screen: never make somebody guess which clock a time is in.
	WhenUTC string
	// Backdated says the record is about a moment meaningfully in the past, which is
	// the ordinary case here and worth naming — it is the difference between "closing
	// the shift that just ended" and "reconstructing last Tuesday".
	Backdated bool
	// ConfirmToken is the value this server minted for THIS person, THIS direction and
	// THIS instant, and ConfirmField is the input name it is read back from. Both are
	// fields because the server owns both halves: a form carrying the wrong name would
	// fail closed rather than silently skip the gate.
	ConfirmToken string
	ConfirmField string
}

// ManualDirectionView is one option of the closed direction vocabulary.
//
// IT CARRIES A SENTENCE AS WELL AS A LABEL. "Check-in" and "Checkout" differ by two
// characters and are the whole meaning of the record; the confirmation screen prints
// the sentence ("started work" / "stopped work") so the thing being confirmed cannot
// be misread at a glance.
type ManualDirectionView struct {
	Value    string
	Label    string
	Sentence string
}

// directionSentence is the chosen direction's sentence, or "" if nothing matches.
// Written as a lookup over the SAME slice the control renders from, so the
// confirmation cannot describe a direction the form never offered.
func directionSentence(v ManualEntryView) string {
	for _, d := range v.Directions {
		if d.Value == v.Direction {
			return d.Sentence
		}
	}
	return ""
}

// directionLabel is the chosen direction's label, from the same slice.
func directionLabel(v ManualEntryView) string {
	for _, d := range v.Directions {
		if d.Value == v.Direction {
			return d.Label
		}
	}
	return ""
}

// manualProblemHeading is the sentence for each refusal.
//
// EVERY ONE OF THEM SAYS WHAT WAS NOT WRITTEN. §4.6's question about a refused request
// is "what is the manager told, and what is left in the database", and on this screen
// the second half is always "nothing" — so every heading says so rather than leaving
// the manager to wonder whether half a record went in.
func manualProblemHeading(problem string) string {
	switch problem {
	case "direction":
		return "Nothing was recorded — pick check-in or checkout"
	case "when", "future", "too-old":
		return "Nothing was recorded — that time cannot be used"
	case "confirm-required", "confirm-stale":
		return "Nothing was recorded — the record was not confirmed"
	case "unavailable":
		return "Nothing was recorded — this action is unavailable right now"
	default:
		return "Nothing was recorded"
	}
}
