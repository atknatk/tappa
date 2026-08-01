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

// TourSteps is how many slides the mini tour has (M5-07's card: "Tap the plaque"
// -> "One button" -> "First tap is practice").
//
// IT IS A CONSTANT BECAUSE THE SLIDES COUNT THEMSELVES OUT LOUD. Every slide
// renders "Step N of TourSteps", so a number typed into the prose would be a
// second source of truth that drifts the moment a slide is added — the failure
// mode agent-brief.md lists as its own class of finding. The handler clamps
// against this same constant, and TestTour_HasExactlyTourStepsSlides ties it to
// the slides that actually render.
const TourSteps = 3

// TourView is one slide of the mini tour shown right after a first activation.
//
// ONE FIELD, AND IT CARRIES NO PERSONAL DATA AT ALL — not a name, not a venue,
// not an id. That is the strongest form of this package's §4.7 rule: the other
// views promise "no field a secret could travel in", and this one has no field a
// PERSON could travel in either. The tour teaches; it reports nothing, so it
// needs to know nothing. The consequence is that the whole tour is literals from
// activate.templ, and the only value reaching the template from a request is a
// small integer the handler has already clamped.
//
// IT IS ALSO WHY THE TOUR NEEDS NO WRITE OF ANY KIND. There is no progress to
// remember: which slide you are on is the URL you are looking at, so skipping and
// finishing are the same act — following a link — and neither leaves a
// `transactions` row, an `audit_log` row or a cookie behind.
type TourView struct {
	// Step is 1..TourSteps. The handler clamps anything else to 1 rather than
	// erroring: a mistyped step is not a failure worth a screen.
	Step int
}

// TourPageTitle is the browser title for one slide. It lives in Go rather than in
// the template because layout.Page takes the title as a value; the SLIDES
// themselves are literals in activate.templ, where Tailwind can see their classes
// (see the note on ResultView for what happens to a class name that lives here).
func TourPageTitle(step int) string {
	switch step {
	case 2:
		return "One button — Tappa"
	case 3:
		return "Your first tap — Tappa"
	default:
		return "How Tappa works — Tappa"
	}
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

	// RetryURL turns the Hint into a link labelled "Try again" (M5-06). EMPTY IS
	// THE DEFAULT AND THE COMMON CASE, and that is the honest shape rather than a
	// missing feature.
	//
	// 🔴 A RETRY THAT CANNOT WORK IS A LIE, and on this flow most retries cannot.
	// Nothing on a screen can produce an attendance record — the next one needs a
	// new physical touch of the plaque (§9) — so a "Try again" here can only mean
	// "fetch this page again", and that is useful in exactly one situation: the
	// failure was TRANSIENT and the page it would re-fetch is a GET that writes
	// nothing. A malformed URL re-fetches to the same error; an unknown plaque
	// re-fetches to the same 404; a rate-limited request re-fetches into the very
	// budget it is being asked to stop spending; and a POST failure has no address
	// to offer at all, because the tap context is single-use and the chip's URL is
	// not in the request. Those screens say what to DO instead ("Ask your manager
	// for the new plaque"), which is the brand's rule for an error message anyway.
	//
	// It is a LINK and never a form: an anchor cannot resubmit a POST, so no
	// FAILURE screen in this package can produce a second `transactions` row. The
	// qualifier matters and was missing here while activate.templ carried it —
	// pages.Tap is in this package and renders exactly such a form
	// (<form method="post" action="/api/checkin"> and the one button), which is
	// the whole point of that screen.
	//
	// 🔴 IT DOES CARRY REQUEST BYTES, AND SAYING OTHERWISE WAS WRONG. An earlier
	// version of this comment claimed the value is "never taken from a parameter —
	// nothing builds it from user input". The handler sets it to
	// r.URL.RequestURI(), which is EscapedPath() + "?" + RawQuery, and RawQuery is
	// the request line verbatim; internal/sun's parser ignores parameters it does
	// not know, so /t?tag=<14 hex>&anything=… reaches this screen and "anything"
	// is reflected into the href. MEASURED: a raw `"><script>alert(1)</script>` in
	// the query arrives in RequestURI() unchanged.
	//
	// TWO MECHANISMS MAKE THAT SAFE, and they are named because a guarantee with
	// no named mechanism is a wish:
	//
	//  1. THE PATH IS PINNED BY ROUTING. Only chi's /t route reaches the handler
	//     that sets this, so EscapedPath() is /t. Measured: `//t`, `//evil.com/t`
	//     and `/%2Ft` all answer 404 without ever entering the handler, and an
	//     absolute-form request line (GET http://evil.com/t?…) yields a
	//     RequestURI() of /t?… with the host dropped. The link therefore cannot be
	//     re-pointed at another origin.
	//  2. TEMPL ESCAPES THE ATTRIBUTE. The value goes through templ.EscapeString,
	//     so the probe above renders as
	//     &#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt; — inside the quotes,
	//     inert. What it is NOT protected by is templ.URL's scheme filter: see
	//     the note beside the anchor in activate.templ for what that check does
	//     and does not do.
	RetryURL string
}

// ResultView is the screen a tap lands on: what was recorded, and nothing else.
//
// FINISHED AS OF M5-06, and "finished" is a claim about a specific list rather
// than a feeling: the stamp and its fixed colour, the sentence, the wall clock in
// mono, the tenant-kind brand line after a good tap, and NO BUTTON. What M5-06
// did NOT do is make that brand line editable from a panel — the copy is fixed in
// result.templ and keyed on the tenant's business TYPE, so two restaurants share
// one sentence. Per-tenant copy needs a column that does not exist yet and is
// M9-04's work; it is a limit, not a gap left unnoticed.
//
// EIGHT FIELDS, AND THE LIST IS THE SPEC — the same rule TapView carries. There
// is no history, no shift total, no "you are currently clocked in", no settings
// and no second action, because a view model with a field cannot help rendering
// it. Adding one is adding a feature to the screen CLAUDE.md §9 calls sacred, and
// the rule there is to ask first.
//
// ⚠️ THE COUNT IS ENFORCED, NOT ASSERTED. A number in a comment only catches a
// smuggled field if somebody re-counts, and this one was WRONG within a single
// task: it said seven while the struct carried eight (BusinessType had been
// added). TestResultView_FieldCountIsTheSpec reflects over the type and fails on
// any change to the count, so the tripwire is a test and this sentence is only
// its explanation. Changing the number is fine — changing it without the
// conversation §9 asks for is not.
//
// NO §4.7 VALUE CAN TRAVEL HERE, and that is a property of the field list rather
// than of this comment: there is no field for a session token, a CMAC, a key, an
// invite code or a COORDINATE. The tap's position reaches the database as a
// number in two columns and reaches this screen not at all — what a person sees
// is a verdict, a venue name and a clock. BusinessType, added by M5-06, does not
// change that: it is one of eight CHECK-constrained CATEGORIES (migration 00001),
// shared by every tenant of that kind, so it identifies nobody — and it is not
// rendered at all, it only chooses between sentences written in the template.
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
	// BusinessType is the tenant's category ('restaurant', 'production', … —
	// migration 00001's CHECK list), and it exists to pick ONE SENTENCE: the brand
	// line a good tap ends with. It is never printed: result.templ switches on it
	// and renders a literal, so an unknown or empty value falls to the neutral
	// wording rather than to a blank space or to a made-up trade.
	BusinessType string
}

// NOTE — THERE IS NO Recorded() HERE ANY MORE. It answered "did this put an hour
// on the record" with one bool for a screen that needs four different sentences
// (a flag is recorded AND awaiting a manager, §4.6; an ignored duplicate is
// recorded and counted nowhere; a reject is recorded and refused), and a bool
// that collapses those into two was how "All done" ended up over a FLAGGED tap.
// The copy now branches on the verdict itself, once, inside result.templ.

// NOTE — THERE IS NEITHER StampClass NOR Stamp() HERE, and both absences are
// deliberate.
//
// StampClass returned "stamp stamp--approved" and friends from Go, and Tailwind
// does not scan Go files (content globs: web/templates/**/*.templ,
// web/static/js/**/*.js), so those four classes were compiled OUT of app.css and
// every stamp rendered unstyled. The class names now live as literals inside
// result.templ's `stamp` component, where the tool can see them. Anything that
// maps a value to a CSS class belongs in a scanned template for the same reason.
//
// Stamp() returned the WORD ("APPROVED"…) and was removed by M5-06 for the
// neighbouring reason: the template has to write the word as a literal anyway,
// next to the class it belongs with, so a Go copy was a second source of truth
// that nothing consulted — and a second source of truth for a thing that must
// agree is how "the class says reject, the word says approved" becomes possible.
// If a caller outside a template ever needs the word, it should read it from the
// same switch the screen renders, not from a copy.

// Headline is the one sentence the screen exists to say. Short, warm, factual —
// "Tapped in at 14:03", never "Your check-in operation has been processed".
//
// 🔴 IT IS THE BIGGEST TEXT ON THE PAGE, SO IT IS ALSO THE EASIEST PLACE TO DENY
// A RECORD. For `reject` it used to read "Not recorded", four lines above a
// closing sentence that said "This tap WAS recorded but not counted" — the page
// contradicted itself, and the headline was the half that was false. Measured
// rather than argued: internal/domain/checkin returns an ERROR when the insert
// fails and only builds OutcomeRecorded after it succeeds, so a rendered Result
// page for a reject is proof a `transactions` row exists (that service's write()
// says the same from its side: "a reject is the outcome the record matters MOST
// for, because it is the one somebody will dispute").
//
// That is not a cosmetic slip. §5 rows 1, 2 and 4 — a retired or lost plaque, an
// invalid SUN or a replay, a deactivated account — are all `reject`, and §4.6's
// entire promise to the person standing there is that the attempt left a record
// they can dispute. A headline telling them there is nothing to dispute is the
// promise being withdrawn in the largest type on the screen.
//
// SO THE SWITCH IS ON THE VERDICT FIRST, and a direction is only spoken for the
// two verdicts that HAVE one. It used to fall through to "Tapped in" for any
// verdict this file did not recognise, which read as a counted tap under a
// RECORDED stamp; an unknown verdict now gets the neutral word, which claims
// neither that the hours counted nor that the record is missing.
func (v ResultView) Headline() string {
	switch v.Verdict {
	case "ok", "flag":
		switch v.Direction {
		case "in":
			return "Tapped in"
		case "out":
			return "Tapped out"
		default:
			return "Tapped"
		}
	case "ignored":
		// Says a tap happened a moment ago, which is exactly what the debounce
		// established, and says NOTHING about what became of it (see the ignored
		// branch in result.templ for why that distinction is load-bearing).
		return "Already tapped"
	case "reject":
		// "Not counted", never "Not recorded". The hours are what was refused; the
		// record exists and is the point.
		return "Not counted"
	default:
		return "Tapped"
	}
}
