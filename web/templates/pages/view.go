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
