package operator

import (
	"bytes"
	"errors"
	"net/http"

	"github.com/a-h/templ"

	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// operatorCSP is the content policy securityHeaders sets (renderEnroll sets enrollCSP
// instead). Measured, on the response headers at WriteHeader, on the 48 classes of the two
// header tables (TestOperatorHeaders_FortyResponseClassesCarryThePolicy and
// TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy).
//
// It is the panel's adminCSP, directive for directive, for the panel's reasons
// (internal/handler/adminlogin.go): default-src 'none' with no script-src names no script
// source; styles and fonts from this origin; form-action 'self' keeps a form's target on
// this origin; base-uri 'none'; frame-ancestors 'none' (the OP-8 acceptance names it).
//
//	default-src 'none'  style-src 'self'  font-src 'self'  form-action 'self'
//	base-uri 'none'     frame-ancestors 'none'
//
// 'self' here is the OPERATOR origin -- the operator host serves its own /static
// (httpx.operatorHostOnly lets that customer-side route through).
const operatorCSP = "default-src 'none'; style-src 'self'; font-src 'self'; " +
	"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

// enrollCSP is operatorCSP plus one script source: the enrollment script's full URL on the
// operator origin, not 'self'. The operator host serves the files under /static
// (httpx.operatorHostOnly), the customer product's scripts among them -- a vendored htmx,
// whose attribute-driven requests are a known way to turn an HTML injection into script
// behaviour under a 'self' policy; a source naming the one path does not let a page under
// this policy load them. No 'unsafe-inline', no 'unsafe-eval', no connect-src. Measured on
// the response headers at WriteHeader of the 48 classes of the two header tables: the four
// scripted classes (C18, C20, C21, C22) carry it, the other 44 do not.
func (s *Surface) enrollCSP() string {
	return operatorCSP + "; script-src " + s.origin + operatorpages.EnrollScript()
}

// maxFormBytes bounds an operator form body. The largest legitimate one is the enrollment
// form: an account id (36), a link token (43), a pending blob (under 200), two passwords
// (72 bytes each at most that count) and a code -- under 1 KiB. 16 KiB leaves room for a
// password manager's padding and keeps a caller from making the form parser read the
// ingress's 1 MiB.
const maxFormBytes = 16 << 10

// readForm parses the request under maxFormBytes; the handlers read r.PostForm (the body),
// not r.Form. A body past the bound gets 413, one that does not parse gets 400 -- before a
// credential is read from it -- and it reports false.
func (s *Surface) readForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.problem(w, r, http.StatusRequestEntityTooLarge, problemFormTooLarge)
			return false
		}
		s.problem(w, r, http.StatusBadRequest, problemBadForm)
		return false
	}
	return true
}

// render writes one screen. It renders into a buffer first, so a component that refuses
// (operatorpages.TenantScreen without a tenant's name) or fails writes no partial page:
// the status line has not been sent, and the answer is a plain 500
// (TestTenantScreen_RefusesToRenderWithoutAName).
func (s *Surface) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	var buf bytes.Buffer
	if err := c.Render(r.Context(), &buf); err != nil {
		s.log.ErrorContext(r.Context(), "operator: rendering a screen failed", "err", err)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("500 the page could not be rendered\n"))
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// redirect answers 303 See Other: a POST becomes a plain GET and a refresh is harmless.
// Measured: in the response headers at WriteHeader of the 48 classes of the two header
// tables (TestOperatorHeaders_FortyResponseClassesCarryThePolicy and
// TestOperatorHeaders_TheWrongMethodAndOversizedClassesCarryThePolicy), Location is the
// designed path on each 303 and absent on the others, with a hostile Location and a
// hostile Referer request header sent. Pinned:
// TestResponseHeaders_TheListedNamesAreWrittenOnlyInTheirFunctions catches the list in its
// header.
func (s *Surface) redirect(w http.ResponseWriter, to string) {
	w.Header().Set("Location", to)
	w.WriteHeader(http.StatusSeeOther)
}

func (s *Surface) problem(w http.ResponseWriter, r *http.Request, status int, v operatorpages.ProblemView) {
	s.render(w, r, status, operatorpages.Problem(v))
}

// renderEnroll renders the enrollment screen -- its first load and its re-render after a
// refusal the person can fix -- and sets enrollCSP, the policy that names the screen's
// script. Measured on the response headers at WriteHeader: of the 48 classes of the two
// header tables, the four whose body loads a script (C18, C20, C21, C22) carry enrollCSP
// and the other 44 carry operatorCSP.
// Pinned: TestEnrollScreen_TheListedFormsRenderItOnlyInRenderEnroll catches the list in
// its header.
func (s *Surface) renderEnroll(w http.ResponseWriter, r *http.Request, status int, v operatorpages.EnrollView) {
	w.Header().Set("Content-Security-Policy", s.enrollCSP())
	s.render(w, r, status, operatorpages.Enroll(v))
}

// The surface's fixed refusal and fault pages. Their sentences are constants, each
// saying what to do next (skill tappa-brand: a message does not blame, it says what to
// do). problemPages lists them (eight variables and problemTooMany twice), and
// TestProblemPages_LinkOnlyToMountedRoutes renders the ten and holds their links to the
// mounted routes. Pinned: TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo catches
// the list in its header.
func problemTooMany(signedIn bool) operatorpages.ProblemView {
	return operatorpages.ProblemView{
		Title:    "Too many requests",
		Message:  "Wait ten minutes, then try again.",
		SignedIn: signedIn,
	}
}

var (
	problemCrossOrigin = operatorpages.ProblemView{
		Title:     "That request did not come from this site",
		Message:   "Open the page again and send the form from there.",
		Back:      pathSignIn,
		BackLabel: "Go to the sign-in",
	}
	problemFormTooLarge = operatorpages.ProblemView{
		Title:   "That form was too large",
		Message: "Open the page again and send the form from there.",
	}
	problemBadForm = operatorpages.ProblemView{
		Title:   "That form could not be read",
		Message: "Open the page again and send the form from there.",
	}
	problemUnavailable = operatorpages.ProblemView{
		Title:     "Sign-in is not available right now",
		Message:   "Try again in a few minutes.",
		Back:      pathSignIn,
		BackLabel: "Go to the sign-in",
	}
	problemEnrollIncomplete = operatorpages.ProblemView{
		Title:   "This setup link is incomplete",
		Message: "Open the whole link from your message, including the part after the # sign.",
	}
	problemEnrollRefused = operatorpages.ProblemView{
		Title:   "This setup link does not work",
		Message: "It may have expired (a link lasts 30 minutes) or been used already. Ask for a new one.",
	}
	problemSignOutFailed = operatorpages.ProblemView{
		Title:     "Sign-out did not finish",
		Message:   "Your session is still open. Try signing out again in a moment.",
		Back:      pathConsole,
		BackLabel: "Back to the console",
		SignedIn:  true,
	}
	problemSignOutThrottled = operatorpages.ProblemView{
		Title: "Too many sign-out requests from your network",
		Message: "This browser no longer holds your session, but the server could not be asked to end it. " +
			"It ends after 30 minutes without use; if someone else may hold it, ask for it to be revoked.",
		Back:      pathSignIn,
		BackLabel: "Go to the sign-in",
	}
)

// problemPages lists the eight variables above and problemTooMany(false) and (true), for
// the link test.
func problemPages() []operatorpages.ProblemView {
	return []operatorpages.ProblemView{
		problemTooMany(false), problemTooMany(true), problemCrossOrigin, problemFormTooLarge, problemBadForm,
		problemUnavailable, problemEnrollIncomplete, problemEnrollRefused, problemSignOutFailed, problemSignOutThrottled,
	}
}
