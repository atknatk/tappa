package handler

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/invite"
)

// The activation cookie: the short-lived carrier of the invite code between
// GET /activate and POST /api/activate.
//
// WHY THE CODE TRAVELS IN A COOKIE AND NOT IN THE FORM. The obvious design puts
// the code in a hidden input and posts it back. That works, and it puts a §4.7
// secret into the HTML of a page that is rendered on a shared shop phone, shown
// to whoever is standing there, kept in the browser's back/forward cache and
// saved by "save page". Three concrete gains from the cookie instead:
//
//  1. The response BODY carries no code. That is testable and it is tested
//     (activate_test.go greps every rendered byte).
//  2. The ADDRESS BAR carries no code after the first hop — ON THE ORDINARY PATH:
//     GET /activate?code=… sets the cookie and answers 303 to a bare /activate,
//     so the URL that stays in history, in the tab title and in any Referer we
//     emit is clean. The code still exists in the link the employee was sent —
//     unavoidable, not claimed away — we simply stop re-emitting it.
//
//     ⚠️ ONE PATH DOES NOT GET THIS, and an audit was right to name it: a
//     cross-site arrival that collides with the session already on the phone
//     answers 200 with the confirmation page instead of a 303, so THAT url —
//     code and all — stays in the address bar and in history. It is the correct
//     trade (see measure 3 below: refusing to store anything is the whole point,
//     and a redirect would have to store something first), but it is an exception
//     and is written here rather than left for the next reader to measure.
//  3. HttpOnly removes page script from the picture entirely.
//
// AND ONE MORE THING IT BUYS, which is worth stating because it is not obvious:
// the flow now FAILS EARLY if the browser refuses cookies. Without it, activation
// would "succeed", the session cookie would be dropped just as silently, and the
// employee would discover the problem at the plaque with a queue behind them.
//
// COST, stated plainly: a browser that blocks cookies cannot activate at all. The
// product requires a persistent cookie to work (the whole premise is "the phone
// remembers you"), so this converts a late, confusing failure into an early,
// explainable one — but it IS a hard requirement and the Problem page says so.
//
// WHAT SameSite=Lax DOES AND DOES NOT BUY — an earlier version of this comment
// claimed "the worst a forced activation could do is spend a code the attacker
// already holds", and an independent audit MEASURED that to be false.
//
//	CLOSED by Lax:   a cross-site POST to /api/activate does not carry this
//	                 cookie, so an attacker's page cannot submit the form for a
//	                 victim.
//	NOT CLOSED:      SameSite restricts SENDING, not WRITING. A cross-site GET
//	                 navigation to /activate?code=<attacker's code> is a top-level
//	                 navigation, so the browser stores the Set-Cookie that comes
//	                 back. The audit drove exactly that: the victim's browser was
//	                 made to plant the attacker's code, the next same-site GET
//	                 rendered ANOTHER TENANT's activation form, and one same-site
//	                 POST (which Lax does carry) bound the victim's phone to a
//	                 stranger's employee record and overwrote their session.
//
// THREE MEASURES NOW STAND BETWEEN THAT AND AN ACCOUNT TAKEOVER, and they are
// deliberately layered because none of them is sufficient alone:
//
//  1. A SYNCHRONIZER TOKEN in this cookie (the csrf half), echoed by the form and
//     compared in constant time on POST. The attacker can plant a cookie, but the
//     token minted while doing so went into the VICTIM's browser, behind HttpOnly
//     and the same-origin policy; a token the attacker obtains by opening the
//     same link in their OWN browser belongs to their own cookie and does not
//     match. So the forged POST fails. What remains is PHISHING — the victim
//     pressing "Activate this phone" themselves, on a page showing a stranger's
//     name and employer — which is a different thing, is what measure 2 and the
//     form's wording address, and is NOT claimed to be closed.
//  2. Submit READS THE EXISTING SESSION and refuses to bind this phone to a
//     different employee without an explicit confirmation (activate.go). A
//     session is evidence of who this phone belongs to; overwriting it silently
//     is what turned the fixation into a lost shift.
//  3. A cross-site GET that would collide with an existing session does NOT get
//     a cookie at all; it lands on a same-site confirmation step. Defence in
//     depth only: Sec-Fetch-Site is absent on older browsers, so this can never
//     be the load-bearing check. The comparison is case-insensitive (crossSite,
//     activate.go) after a probe showed "Cross-Site" sailing past a
//     case-sensitive test.
//
//     ⚠️ IT IS CONDITIONAL, AND HERE IS WHAT THAT LEAVES OPEN. On a phone with NO
//     session, a cross-site GET still plants the cookie, and the next same-site
//     GET renders the offered employer's form. Today that grants an attacker
//     NOTHING they did not already have — they could send the same link directly,
//     and the form names the employer and the employee about to be activated. The
//     reason it is written down anyway is M5-04: `GET /t` will redirect a
//     session-less tap to /activate, i.e. straight onto whatever this cookie
//     holds. When that lands, M5-04 must decide whether a planted cookie may
//     satisfy that redirect, or whether the redirect should carry its own
//     one-shot marker. Making the form say WHOSE activation this is (it does) is
//     the mitigation that exists now; it is not a substitute for that decision.
//
// The cookie value is "<csrf>.<code>". Both halves are 43-character base64url,
// and '.' is outside that alphabet, so the split is unambiguous.
//
// KNOWN LIMIT — COOKIE SHADOWING, deliberately not addressed here. A page on a
// SUBDOMAIN (or anything that can write a Domain-scoped cookie for this site) can
// set a second cookie with this name; net/http's r.Cookie returns the FIRST match
// and there is no way to tell which one that was. The standard defence is the
// __Host- prefix, which internal/session already considered and rejected for now
// because it forces Secure and would break http://localhost development
// (cookie.go says so from its side). Closing it properly is a deployment-time
// decision about which hosts exist under this domain — M8 — and is out of scope
// here. Stated so nobody reads the paragraphs above as covering it.
//
// 🔴 REVISITED AT M8-04 (2026-08-19, backlog T3) AND STILL NOT ADOPTED — BUT THE
// RECORD ABOVE WAS TOO KIND AND IS CORRECTED HERE. Three things were measured.
//
//  1. THE PRECONDITION IS SATISFIED BY THE ACTUAL DEPLOYMENT, NOT HYPOTHETICALLY.
//     The product is served from a SUBDOMAIN of a registrable domain it does not
//     own alone (deploy/k8s/05-config.yaml, 40-ingress.yaml). Any other host under
//     that registrable domain can set a Domain-scoped cookie with any of this
//     product's ten cookie names, and net/http's r.Cookie returns the FIRST match
//     with no way to tell which. So "a page on a subdomain" is not a thought
//     experiment here; it is the shape of the deployment.
//  2. __Host- IS NOT A UNIFORM OPTION, and that is a schema fact rather than a
//     preference: the prefix REQUIRES Path=/, and internal/adminauth deliberately
//     scopes the panel cookie to /admin — that narrower path is the structural half
//     of "an admin cookie cannot reach the tap surface" and is pinned by a test
//     (internal/handler/admincookiepath_test.go). Adopting __Host- would either
//     undo that decision or apply the prefix to some cookies and not others.
//  3. THE FAILURE MODE OF DOING IT WRONG IS PRODUCTION-ONLY. Browsers require
//     Secure for __Host-, so the prefix has to be conditional on TAPPA_ENV — which
//     means a single missed read site breaks sign-in ONLY in production, where no
//     test and no `make dev` can see it.
//
// WHAT THIS MEANS FOR THE RISK, honestly: SameSite=Lax, HttpOnly and Secure-in-prod
// are in place and a planted SESSION value is not a session (it must resolve against
// the database), but neither of those stops session FIXATION — an attacker planting
// a cookie for a session they own. The narrowing that would actually close it is a
// deployment decision (serve from a registrable domain of the product's own, or from
// a host with no siblings), and it is a decision for the operator rather than a
// change to make inside a security-fix round.

const (
	// activationCookieName is separate from the session cookie so that clearing
	// one never disturbs the other.
	activationCookieName = "tappa_activation"

	// activationCookieMaxAge is 15 minutes. It only has to survive the time
	// between opening the link and reading two screens of text, and a shorter
	// window shrinks the period in which a shared phone left unlocked on a
	// counter carries someone else's activation credential.
	activationCookieMaxAge = 15 * 60
)

// activationState is what the cookie carries: the invite code and the
// synchronizer token that must come back with the form.
//
// The code half is a §4.7 secret; the csrf half is NOT — it is rendered into the
// page on purpose, which is the whole mechanism. They travel together so that
// one cookie, one lifetime and one clear() covers both.
//
// THE CODE FIELD IS AN invite.Code, NOT A string, AND THAT IS THE POINT. The
// first version of this struct stored a bare string and an audit measured the
// consequence: %v, %+v, %#v and %s all printed the raw code — including from a
// wrapping struct, which is exactly the shape a handler writes when it logs its
// own state. That is the SAME defect internal/session's Token was RED for in
// M5-01 round 2, reproduced in a new package the moment the value was unwrapped
// out of its type. invite.Code carries the redaction (a *string field plus the
// five printing interfaces), so the indirection survives the cookie boundary.
//
// In THIS package the raw string therefore exists only as a FUNCTION LOCAL: at
// the two request entry points that read it (a query parameter, a form field)
// and inside set, where it becomes a Set-Cookie header.
//
// ⚠️ ONE STRUCT FIELD OUTSIDE THIS PACKAGE DOES HOLD IT, and an audit was right
// that the sentence above used to deny it: pages.ConfirmView.Code, an exported
// plain string. Measured — fmt.Sprintf("%+v", pages.ConfirmView{Code: …}) prints
// the code, while activationState prints an address in the same run.
//
// WHY IT CANNOT BE THE REDACTED TYPE, since "use invite.Code there too" is the
// obvious answer and it does not work: that page renders the value into a hidden
// form field, and templ renders a value by asking it for its string form — which
// is precisely what the redaction overrides. A redacting type there would render
// the placeholder and the confirmation step would post "invite.Code(redacted)".
// The field has to be a plain string because its job is to be printed.
//
// What is done instead: the exception is DECLARED (web/templates/pages/view.go
// names it in the package doc), it exists on exactly one view, that view is
// reached only by the cross-site-conflict path, and no code in this repo logs a
// ConfirmView. The protection there is review, not the type system — which is
// the honest version of what the old sentence claimed for free.
type activationState struct {
	csrf string
	code invite.Code
}

// newCSRFToken draws a fresh synchronizer token. It MUST NOT be derived from the
// code: an attacker who plants a cookie knows their own code, so a derived token
// would be a token they can compute, and the whole measure would be decorative.
func newCSRFToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("handler: read randomness: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// codeCookies writes, reads and clears the activation cookie.
//
// THE FIELD'S POLARITY IS THE SECURITY MECHANISM — `insecure`, not `secure`, so
// the ZERO VALUE MEANS SECURE. This is not a style choice; it is the M5-01 round-3
// audit finding applied to the second cookie in the codebase. Go forbids another
// package from NAMING an unexported field, not from obtaining the zero value:
// `var c codeCookies`, `codeCookies{}` and — the realistic one — a handler struct
// literal that simply leaves the field out all compile. With `secure bool` every
// one of those ships a credential-bearing cookie without Secure; with this
// polarity they all ship Secure and only newCodeCookies can ask for the
// relaxation, in exactly one branch.
type codeCookies struct {
	insecure bool
}

// newCodeCookies derives the hardening from configuration, mirroring
// session.NewCookies row for row (prod is always Secure; a non-prod deployment
// served over plain http relaxes so http://localhost works). The two are
// deliberately independent codecs rather than one shared type: the session cookie
// lives a year and the activation cookie fifteen minutes, and a shared type would
// grow parameters until neither guarantee was legible.
//
// The same residual gap applies and is not papered over: a Config that skipped
// config.Load (a struct literal, a misspelled Env) takes the non-prod branch and
// is believed. config.IsProd carries the note from its side.
func newCodeCookies(cfg *config.Config) codeCookies {
	if cfg == nil || cfg.IsProd() {
		return codeCookies{}
	}
	return codeCookies{insecure: !strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://")}
}

// secure reports the hardening this codec applies.
func (c codeCookies) secure() bool { return !c.insecure }

// set writes the activation cookie. The raw code passes through here and through
// nowhere else in this package.
// set takes the raw code as a plain argument rather than inside a struct, so the
// only place it is spelled as a string is the one line that writes the header.
func (c codeCookies) set(w http.ResponseWriter, csrf, rawCode string) {
	http.SetCookie(w, &http.Cookie{
		Name:     activationCookieName,
		Value:    csrf + "." + rawCode,
		Path:     "/",
		MaxAge:   activationCookieMaxAge,
		HttpOnly: true,
		Secure:   c.secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

// read lifts the state off a request. The second result is false when the cookie
// is absent, empty or malformed; the caller shows the generic "you need your
// link" page and consumes nothing.
//
// A cookie that does not split into exactly two parts is treated as ABSENT
// rather than repaired: a value in that shape was not written by set, and
// guessing at what half of it means is how a parser becomes an attack surface.
func (c codeCookies) read(r *http.Request) (activationState, bool) {
	ck, err := r.Cookie(activationCookieName)
	if err != nil || ck.Value == "" {
		return activationState{}, false
	}
	csrf, code, ok := strings.Cut(ck.Value, ".")
	if !ok || csrf == "" || code == "" {
		return activationState{}, false
	}
	// Wrapped immediately: the raw code is a local for exactly one statement.
	return activationState{csrf: csrf, code: invite.ParseCode(code)}, true
}

// csrfMatches compares the token the form sent with the one in the cookie, in
// constant time (redline R7: a secret comparison is never == or bytes.Equal).
//
// The comparison is on a token, not on a code, and the empty case is handled
// explicitly: ConstantTimeCompare returns 0 for different lengths, so an absent
// form field can never match.
func (st activationState) csrfMatches(sent string) bool {
	if st.csrf == "" || sent == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(st.csrf), []byte(sent)) == 1
}

// clear expires the activation cookie. Called the moment the code has been spent
// — the credential is dead in the database by then, but leaving a dead secret in
// a browser is still a habit worth not forming. Attributes must match set exactly
// or the browser keeps the original cookie.
func (c codeCookies) clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     activationCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.secure(),
		SameSite: http.SameSiteLaxMode,
	})
}
