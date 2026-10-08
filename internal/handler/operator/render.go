package operator

import (
	"bytes"
	"context"
	"errors"
	"net/http"

	"github.com/a-h/templ"

	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// operatorCSP is the content policy securityHeaders sets (renderEnroll sets enrollCSP
// instead). Measured, on the response headers at WriteHeader, on every class of the header
// tables (the tests' classRoutes; the tables are named at securityHeaders, routes.go).
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
// the response headers at WriteHeader of every class of the header tables: the four
// scripted classes (C18, C20, C21, C22) carry it, no other class does.
func (s *Surface) enrollCSP() string {
	return operatorCSP + "; script-src " + s.origin + operatorpages.EnrollScript()
}

// maxFormBytes bounds an operator form body. The largest legitimate one is the enrollment
// form: an account id (36), a link token (43), a pending blob (under 200), two passwords
// (72 bytes each at most that count) and a code -- under 1 KiB. 16 KiB leaves room for a
// password manager's padding and keeps a caller from making the form parser read the
// ingress's 1 MiB. (The legal text's form has its own bound, maxLegalBody.)
const maxFormBytes = 16 << 10

// readForm parses the request under limit; the handlers read r.PostForm (the body), not
// r.Form. A body past the bound gets 413 with tooLarge, one that does not parse gets 400
// -- before a value is read from it -- and it reports false.
func (s *Surface) readForm(w http.ResponseWriter, r *http.Request, limit int64, tooLarge operatorpages.ProblemView) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	if err := r.ParseForm(); err != nil {
		var past *http.MaxBytesError
		if errors.As(err, &past) {
			s.problem(w, r, http.StatusRequestEntityTooLarge, tooLarge)
			return false
		}
		s.problem(w, r, http.StatusBadRequest, problemBadForm)
		return false
	}
	return true
}

// render writes one screen. It renders into a buffer first, so a component that refuses
// (operatorpages.TenantScreen without a tenant's name -- the tenant overview, the plaque
// screen and the billing screen render through it) or fails writes no partial page:
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

// answered is r once the sign-in's step has decided (the password step, the code step, the
// enrollment's last step): its values, none of its cancellation, so the page that says
// what was decided is rendered whatever the client did (OP-14 phase E; the WL-9 precedent,
// checkin.go's post).
//
// WHY (measured, OP-14 phase C's security audit, P8, the real router over TCP): a client
// that half-closes its connection -- FIN after the request, still reading -- has the
// request's context cancelled by net/http and still receives the answer. render's
// component then refuses the cancelled context and the answer is a plain 500, while a
// right password's 303 writes no body and goes out unchanged: with the step's rows now
// written detached, a refusal answered 500 against a success answered 303 would still read
// the guess's result. Rendered with answered, a refusal is the same status and the same
// bytes as the refusal of a client that stayed (TestSurface_AHalfClosedRefusalIsTheSameAnswer).
func answered(r *http.Request) *http.Request {
	return r.WithContext(context.WithoutCancel(r.Context()))
}

// redirect answers 303 See Other: a POST becomes a plain GET and a refresh is harmless.
// Measured: in the response headers at WriteHeader of every class of the header tables
// (named at securityHeaders, routes.go), Location is the designed path on each 303 and
// absent on the others, with a hostile Location and a hostile Referer request header
// sent. Pinned:
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
// script. Measured on the response headers at WriteHeader: of the classes of the header
// tables, the four whose body loads a script (C18, C20, C21, C22) carry enrollCSP and
// every other class carries operatorCSP.
// Pinned: TestEnrollScreen_TheListedFormsRenderItOnlyInRenderEnroll catches the list in
// its header.
func (s *Surface) renderEnroll(w http.ResponseWriter, r *http.Request, status int, v operatorpages.EnrollView) {
	w.Header().Set("Content-Security-Policy", s.enrollCSP())
	s.render(w, r, status, operatorpages.Enroll(v))
}

// The surface's fixed refusal and fault pages. Their sentences are constants, each
// saying what to do next (skill tappa-brand: a message does not blame, it says what to
// do). problemPages lists them (every variable below and problemTooMany twice), and
// TestProblemPages_LinkOnlyToMountedRoutes renders each and holds their links to the
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

	// The legal texts' pages (legal.go). Each says whether anything was published -- or,
	// for problemLegalNotPublished, that it is not known: an error from the publication
	// (legalWriteTimeout's cancel, a connection lost after the COMMIT was sent) does not
	// prove the row was not written, and the version list one click away does.
	problemLegalTooLarge = operatorpages.ProblemView{
		Title:     "That text is too long",
		Message:   "A document can be at most 256 KiB. Nothing was published.",
		Back:      pathLegal,
		BackLabel: "Back to the legal texts",
		SignedIn:  true,
	}
	problemLegalUnknownDocument = operatorpages.ProblemView{
		Title:     "That document does not exist",
		Message:   "The form named a document Taptime does not publish. Nothing was published.",
		Back:      pathLegal,
		BackLabel: "Back to the legal texts",
		SignedIn:  true,
	}
	problemLegalEmpty = operatorpages.ProblemView{
		Title: "There is no text to publish",
		Message: "The box had no visible text — spaces, line breaks and invisible characters do not count. " +
			"Nothing was published.",
		Back:      pathLegal,
		BackLabel: "Back to the legal texts",
		SignedIn:  true,
	}
	problemLegalNotPublished = operatorpages.ProblemView{
		Title: "The publication was not confirmed",
		Message: "The database did not confirm it. Check the version list before you publish again " +
			"— it shows whether this text went through.",
		Back:      pathLegal,
		BackLabel: "Back to the legal texts",
		SignedIn:  true,
	}
	problemLegalUnreadable = operatorpages.ProblemView{
		Title:     "The legal texts could not be loaded",
		Message:   "Try again in a moment.",
		Back:      pathConsole,
		BackLabel: "Back to the console",
		SignedIn:  true,
	}

	// The tenant screens' pages (tenants.go). NONE REPEATS THE SEARCH TERM: the term
	// belongs on the page it was searched from, and a refusal page is not one (the leak
	// test's G17 holds the 400s, 413, 503 and 303 of a search to that). The four
	// refusals of a search are answered before any store call and say nothing was
	// searched; the numbers in two of them are db.MaxTenantSearchRunes and maxTenantPage
	// (TestTenantSearch_TheBoundaryRefusesBeforeTheStore reads both off the pages).
	problemTenantSearchTooLarge = operatorpages.ProblemView{
		Title:     "That search was too large",
		Message:   "A search is one line of at most 254 characters. Nothing was searched.",
		Back:      pathTenants,
		BackLabel: "Back to the tenants",
		SignedIn:  true,
	}
	problemTenantSearchRefused = operatorpages.ProblemView{
		Title: "That search could not be used",
		Message: "A search is one line of at most 254 characters, with no line breaks or control characters. " +
			"Nothing was searched.",
		Back:      pathTenants,
		BackLabel: "Back to the tenants",
		SignedIn:  true,
	}
	problemTenantPageRefused = operatorpages.ProblemView{
		Title:     "That page does not exist",
		Message:   "The list has pages 1 to 1000. Nothing was searched; start again from the first page.",
		Back:      pathTenants,
		BackLabel: "Back to the tenants",
		SignedIn:  true,
	}
	problemTenantsUnreadable = operatorpages.ProblemView{
		Title:     "The tenants could not be loaded",
		Message:   "Try again in a moment.",
		Back:      pathConsole,
		BackLabel: "Back to the console",
		SignedIn:  true,
	}
	problemTenantNotAnID = operatorpages.ProblemView{
		Title:     "That link does not name a tenant",
		Message:   "Open the tenant from the list.",
		Back:      pathTenants,
		BackLabel: "Go to the tenants",
		SignedIn:  true,
	}
	problemNoSuchTenant = operatorpages.ProblemView{
		Title:     "There is no tenant with that id",
		Message:   "The id may have been copied wrongly. Open the tenant from the list.",
		Back:      pathTenants,
		BackLabel: "Go to the tenants",
		SignedIn:  true,
	}
	problemTenantUnreadable = operatorpages.ProblemView{
		Title:     "That tenant could not be loaded",
		Message:   "Try again in a moment.",
		Back:      pathTenants,
		BackLabel: "Back to the tenants",
		SignedIn:  true,
	}

	// The plaque screen's own page (plaques.go); its malformed and unknown ids answer with
	// the two tenant pages above.
	problemPlaquesUnreadable = operatorpages.ProblemView{
		Title:     "The plaques could not be loaded",
		Message:   "Try again in a moment.",
		Back:      pathTenants,
		BackLabel: "Back to the tenants",
		SignedIn:  true,
	}

	// The audit log's pages (audit.go). Its three refusals are answered before any store
	// call and before the read budget, and say nothing was read; the number in the page
	// refusal is db.MaxOperatorAuditPage (TestAuditScreen_TheBoundaryRefusesBeforeTheStore
	// reads it off the page). An unreadable form is readForm's problemBadForm.
	problemAuditFormTooLarge = operatorpages.ProblemView{
		Title:     "That request was too large",
		Message:   "Open the audit log again and choose a kind there. Nothing was read.",
		Back:      pathAudit,
		BackLabel: "Back to the audit log",
		SignedIn:  true,
	}
	problemAuditKindRefused = operatorpages.ProblemView{
		Title:     "That kind is not in the audit log",
		Message:   "Choose a kind from the list on the audit log. Nothing was read.",
		Back:      pathAudit,
		BackLabel: "Back to the audit log",
		SignedIn:  true,
	}
	problemAuditPageRefused = operatorpages.ProblemView{
		Title:     "That page does not exist",
		Message:   "The audit log has pages 1 to 1000. Nothing was read; start again from the first page.",
		Back:      pathAudit,
		BackLabel: "Back to the audit log",
		SignedIn:  true,
	}
	problemAuditUnreadable = operatorpages.ProblemView{
		Title:     "The audit log could not be loaded",
		Message:   "Try again in a moment.",
		Back:      pathConsole,
		BackLabel: "Back to the console",
		SignedIn:  true,
	}

	// The billing screen's pages (billing.go); its malformed and unknown ids answer with the
	// two tenant pages above. Its two refusals are answered before any store call and before
	// the read budget, and say nothing was read; the number in the page refusal is
	// db.MaxTenantBillingPage (TestBillingScreen_TheBoundaryRefusesBeforeTheStore reads it
	// off the page). 🔴 Its fault page carries NO FIGURE and says so: a failed read is a
	// problem page, never a zero invoice (CLAUDE.md §4.6 in its money form).
	problemBillingFormTooLarge = operatorpages.ProblemView{
		Title:     "That request was too large",
		Message:   "Open the tenant's billing again and choose a page there. Nothing was read.",
		Back:      pathTenants,
		BackLabel: "Back to the tenants",
		SignedIn:  true,
	}
	problemBillingPageRefused = operatorpages.ProblemView{
		Title:     "That page does not exist",
		Message:   "A tenant's billing has pages 1 to 5. Nothing was read; start again from the first page.",
		Back:      pathTenants,
		BackLabel: "Back to the tenants",
		SignedIn:  true,
	}
	problemBillingUnreadable = operatorpages.ProblemView{
		Title:     "This tenant's billing could not be loaded",
		Message:   "Try again in a moment. No figure was read, so none is shown — not even a zero.",
		Back:      pathTenants,
		BackLabel: "Back to the tenants",
		SignedIn:  true,
	}

	// The VAT screen's two fault pages (vat.go); its malformed and unknown ids answer with the
	// two tenant pages above, and the answers that change nothing -- VIES did not answer, a
	// number VIES does not take, the VIES budget -- are the screen itself with a sentence. NEITHER
	// PAGE NAMES THE NUMBER. A write the database did not confirm may still have been recorded
	// (a connection lost after the statement was sent), and the screen one click away shows the
	// verdict on file -- so the page sends the operator there rather than to ask again blind.
	problemVATUnreadable = operatorpages.ProblemView{
		Title:     "This tenant's VAT number could not be loaded",
		Message:   "Try again in a moment. Nothing was sent to VIES.",
		Back:      pathTenants,
		BackLabel: "Back to the tenants",
		SignedIn:  true,
	}
	problemVATNotRecorded = operatorpages.ProblemView{
		Title: "The re-check was not confirmed",
		Message: "VIES answered, but the database did not confirm the answer was recorded. Open the tenant's VAT " +
			"number again — it shows the verdict on file — and ask again from there if it has not changed.",
		Back:      pathTenants,
		BackLabel: "Back to the tenants",
		SignedIn:  true,
	}
)

// problemPages lists the variables above and problemTooMany(false) and (true), for the
// link test.
func problemPages() []operatorpages.ProblemView {
	return []operatorpages.ProblemView{
		problemTooMany(false), problemTooMany(true), problemCrossOrigin, problemFormTooLarge, problemBadForm,
		problemUnavailable, problemEnrollIncomplete, problemEnrollRefused, problemSignOutFailed, problemSignOutThrottled,
		problemLegalTooLarge, problemLegalUnknownDocument, problemLegalEmpty, problemLegalNotPublished,
		problemLegalUnreadable,
		problemTenantSearchTooLarge, problemTenantSearchRefused, problemTenantPageRefused, problemTenantsUnreadable,
		problemTenantNotAnID, problemNoSuchTenant, problemTenantUnreadable,
		problemPlaquesUnreadable,
		problemAuditFormTooLarge, problemAuditKindRefused, problemAuditPageRefused, problemAuditUnreadable,
		problemBillingFormTooLarge, problemBillingPageRefused, problemBillingUnreadable,
		problemVATUnreadable, problemVATNotRecorded,
	}
}
