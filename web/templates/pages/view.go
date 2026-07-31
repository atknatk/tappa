// Package pages holds the employee-facing screens and the plain view models they
// render.
//
// WHY VIEW MODELS INSTEAD OF DOMAIN TYPES. CLAUDE.md §3 keeps store/domain types
// out of the HTTP surface; the same rule applied to templates buys something
// concrete here: a template cannot accidentally render a code hash, a session
// token or a tenant id, because no view in this file has a field to travel in.
//
// THE INVITE CODE IS THE ONE EXCEPTION, and it is declared rather than implied:
// ConfirmView.Code carries it, because that page's whole job is to hand the code
// back through a same-site POST without having stored it first. Every OTHER view
// here — the activation form, the confirmation, the failure pages — has no field
// it could travel in, which is what makes "no code in a body" checkable: the
// exception is one named field on one type, and a test greps every other body.
// An earlier version of this comment claimed no view carried a code at all, 67
// lines above the type that does.
package pages

// ActivateView is the activation form: who is activating, which venue's network
// to join, and the GDPR Art. 13 facts.
//
// THE FIELDS ARE ALL DISPLAY DATA. There is deliberately no Code, no CodeHash and
// no ids: the code travels in an HttpOnly cookie set by GET /activate (see
// internal/handler/activate.go for why it is not in the form), so nothing about
// it needs to reach a template.
type ActivateView struct {
	// EmployeeName greets the visitor: "Hello Maria".
	EmployeeName string
	// EmployerName is the GDPR Art. 13(1)(a) data CONTROLLER — the employer, not
	// Tappa.
	EmployerName string
	// LocationName is the venue whose Wi-Fi the next step names.
	LocationName string
	// WiFiSSID is the venue network, or "" when the location has none
	// (locations.wifi_ssid IS NULL, migration 00010). Empty is NOT an error: the
	// step renders a shorter, network-less version.
	WiFiSSID string
	// RetentionYears is how long attendance records are kept, from
	// TAPPA_RETENTION_YEARS. Rendered, never hard-coded: a retention period is a
	// legal statement and this repo does not get to invent one for a customer.
	RetentionYears int
	// ShowConfigNotice adds a line saying the retention figure comes from this
	// server's configuration. It is set on non-production deployments, where the
	// number is a development placeholder rather than a vetted legal figure
	// (Q13 / backlog B3).
	ShowConfigNotice bool
	// SecondDevice is true when this employee is already active — a new phone.
	// The page warns that the other phone will be signed out.
	SecondDevice bool
	// ConsentMissing re-renders the form after a submit that skipped the consent
	// box, with the error attached to the box itself.
	ConsentMissing bool

	// CSRFToken is the synchronizer token echoed by the form. It is rendered ON
	// PURPOSE and is NOT a §4.7 secret: it exists precisely so that a page an
	// attacker cannot read is the only page that can produce a valid submission.
	// The invite code, which IS a secret, has no field here at all.
	CSRFToken string

	// HeldByName / HeldByEmployer name the person this phone is currently signed
	// in as, when that is SOMEBODY ELSE. Shown so a takeover cannot be silent:
	// the audit's activation-fixation attack ended with a victim losing their
	// shift without ever seeing a warning.
	HeldByName     string
	HeldByEmployer string
	// SwitchMissing marks a submission that would have replaced another person's
	// session without saying so. SwitchConfirmed keeps the box ticked when the
	// form returns for an unrelated reason.
	SwitchMissing   bool
	SwitchConfirmed bool
}

// ConfirmView is the same-site confirmation step: a cross-site navigation
// offering to activate a phone that already belongs to someone else does not get
// to store anything until the person holding the phone says so on OUR page.
//
// Code IS carried here, in a hidden field, and that is a deliberate exception to
// this package's "no code in a body" rule. It cannot be a redacting type: templ
// renders a value by asking for its string form, so a redacted one would post
// the placeholder instead of the code (internal/handler/cookies.go says the same
// from its side).
//
// WHEN THIS PAGE IS REACHED — and "never on the normal path" is WRONG, which an
// audit measured. It renders whenever a cross-site arrival collides with a
// session already on the phone, and the SHARED SHOP PHONE is exactly that: one
// employee is signed in, the next opens their OWN link from a chat app (always
// cross-site), and they get this page. That is a completely legitimate flow, so
// the exception it carries — the code stays in the address bar and appears in a
// hidden field — is wider than "only under attack". It is still the right trade
// (the alternative is storing something before the person confirms), but it is
// not rare.
type ConfirmView struct {
	Code         string
	EmployeeName string
	EmployerName string
}

// DoneView is the confirmation after a successful activation.
type DoneView struct {
	EmployeeName string
	LocationName string
	// WiFiSSID repeats the network name as a reminder, or "" when there is none.
	WiFiSSID string
	// SecondDevice reports that other phones were signed out, so the person is
	// not surprised later.
	SecondDevice bool
}

// TapView is the tap screen — the one an employee sees several times a day, and
// the one CLAUDE.md §9 calls sacred: one screen, one button, zero learning.
//
// THREE FIELDS, AND THAT IS THE SPEC. There is no menu, no history, no shift
// summary, no "you are currently clocked in" and no settings, because a view
// model with a field cannot help rendering it. Adding one here is the first step
// of adding a feature to this screen, and the rule is to ask first.
//
// NOTE WHAT IS ABSENT: no direction. The button does not say "Tap in" or "Tap
// out" because direction is a COMPUTATION over the employee's last open entry
// (§5), it belongs to the decision engine at POST time, and a page that guessed
// it would sometimes contradict the confirmation screen that follows.
type TapView struct {
	// EmployeeName greets the person: "Hello Maria".
	EmployeeName string
	// LocationName is the TAPPED venue — the plaque in front of them, not the
	// location on their profile (§5: branch changes are normal in a chain).
	// Empty renders without the line rather than with a blank one.
	LocationName string
	// TapContext is the server-signed, opaque blob that POST /api/checkin will
	// act on (internal/handler/tapcontext.go). It is NOT a §4.7 secret and it is
	// deliberately not one: the chip's CMAC never enters it, only the one-bit
	// outcome of checking it, so nothing on §4.7's list can travel here. The
	// contract it carries, and what M5-05 must do with it, is documented at the
	// type that mints it.
	TapContext string
}

// ProblemView is every "this did not work" screen.
//
// ONE TYPE FOR ALL OF THEM ON PURPOSE. An unknown code, an expired code and an
// already-used code MUST be indistinguishable to the visitor (§4.7: no oracle),
// so the handler builds this view from a fixed set of constants and the three
// cases share one value. The distinction is kept in audit_log, where it belongs.
type ProblemView struct {
	Title   string
	Message string
	// Hint is the "what do I do now" line — the brand's rule that an error
	// message never blames and always says what to do next.
	Hint string
}

// ResultView is the screen a tap lands on: what was recorded, and nothing else.
//
// ⚠️ THIS IS THE INTERIM CONFIRMATION SCREEN. M5-05 has to answer the POST with
// SOMETHING — the form is a plain browser navigation, so "the endpoint returns a
// decision" means "a page appears" — and M5-06 owns the finished article: the
// tenant's own message ("Have a great shift — keep those kebabs rolling!"), the
// full docket treatment and the copy review. What is here is the part M5-05
// cannot avoid deciding: which stamp, whether there is a button, and the fact
// that a time is shown in mono. The brand rules it does follow are the ones a
// later change must not undo, not a claim that the screen is finished.
//
// NO §4.7 VALUE CAN TRAVEL HERE, and that is a property of the field list rather
// than of this comment: there is no field for a session token, a CMAC, a key, an
// invite code or a COORDINATE. The tap's position reaches the database as a
// number in two columns and reaches this screen not at all — what a person sees
// is a verdict, a venue name and a clock.
type ResultView struct {
	// Verdict is the recorded outcome: ok | flag | reject | ignored. It drives
	// both the stamp text and the colour, and the two are never separated —
	// status is never told by colour alone (accessibility, skill tappa-brand).
	Verdict string
	// Direction is "in" or "out", or "" for a verdict that carries none (a
	// reject, an ignored duplicate). It is the difference between "Tapped in" and
	// "Tapped out", which is the one sentence somebody actually reads.
	Direction string
	// At is the tap time as a WALL CLOCK in the tenant's zone, already formatted
	// by the handler (§6: everything below the render layer is UTC).
	At string
	// Venue is the TAPPED location's name, or "" when it could not be read.
	Venue string
	// Trust is the confidence score (20/50/70/100). Shown as data, in mono,
	// because it is the number a manager asks about when a tap is queried.
	Trust int
	// Note is the deciding rule's human sentence ("verified via GPS only — no
	// network proof of place"). It never carries a secret or a coordinate —
	// internal/policy's reasons are fixed strings written by us.
	Note string
	// Practice marks the training tap that never counts toward hours (§5).
	Practice bool
}

// Recorded reports whether this outcome put an hour on the record — the question
// that decides whether the screen offers a "Try again" button. ok and flag both
// did (a flag is a real record awaiting approval, §4.6); reject and ignored did
// not put an hour anywhere, but they ARE recorded, so the screen still does not
// invite a retry — pressing again would produce the same answer, and the tap
// screen's rule is that the next action is a new physical touch.
func (v ResultView) Recorded() bool { return v.Verdict == "ok" || v.Verdict == "flag" }

// Stamp is the rubber-stamp text. It is the accessibility half of the status:
// the colour says the same thing, and neither is allowed to say it alone.
func (v ResultView) Stamp() string {
	switch v.Verdict {
	case "ok":
		return "APPROVED"
	case "flag":
		return "FLAGGED"
	case "reject":
		return "REJECTED"
	case "ignored":
		return "IGNORED"
	default:
		return "RECORDED"
	}
}

// NOTE — THERE IS NO StampClass HERE ANY MORE, and its absence is deliberate.
// It returned "stamp stamp--approved" and friends from Go, and Tailwind does not
// scan Go files (content globs: web/templates/**/*.templ, web/static/js/**/*.js),
// so those four classes were compiled OUT of app.css and every stamp rendered
// unstyled. The class names now live as literals inside result.templ's `stamp`
// component, where the tool can see them. Anything that maps a value to a CSS
// class belongs in a scanned template for the same reason.

// Headline is the one sentence the screen exists to say. Short, warm, factual —
// "Tapped in at 14:03", never "Your check-in operation has been processed".
func (v ResultView) Headline() string {
	switch {
	case v.Verdict == "ignored":
		return "Already tapped"
	case v.Verdict == "reject":
		return "Not recorded"
	case v.Direction == "in":
		return "Tapped in"
	case v.Direction == "out":
		return "Tapped out"
	default:
		return "Tapped"
	}
}
