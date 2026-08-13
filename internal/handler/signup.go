package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The routes of the sign-up wizard. They are constants because a second surface
// links to them — handler.Marketing's "Start free" button reads signupPath — and
// M7-01 already paid for the version of this that was a string: the landing page's
// "Sign in" link became adminLoginPath for exactly this reason, so a rename cannot
// leave a dead button on the most public URL in the product.
const (
	signupPath        = "/signup"
	signupPlacesPath  = "/signup/places"
	signupAccountPath = "/signup/account"
	signupDonePath    = "/signup/done"
)

// signupProvisioner is the slice of signup.Provisioner this package needs, declared
// HERE at the consumer (§7).
type signupProvisioner interface {
	Provision(ctx context.Context, d signup.Draft) (signup.Result, error)
}

// vatChecker is the slice of signup.Checker this package needs (§7).
//
// IT RETURNS NO ERROR, and that is the interface enforcing Q09 rather than merely
// describing it: there is no error value for this handler to propagate, so "VIES was
// down" CANNOT become "your registration failed" through a branch somebody forgets
// to write. internal/domain/signup/vies.go states the same rule from the other side.
type vatChecker interface {
	Check(ctx context.Context, normalisedVAT string) signup.VATStatus
}

// Signup is the PUBLIC sign-up wizard (M7-02) — the flow that turns a stranger into
// a tenant.
//
// 🔴 UNTIL THIS TYPE EXISTED NOBODY COULD REGISTER. M7-01 opened the landing page
// and left its "Start free" button rendering as a sentence, because
// pages.LandingView.SignupHref was "" and a button that answers 404 is worse than
// no button. This is the other half.
//
// 🔴 AND IT IS THE FIRST UNAUTHENTICATED SURFACE IN THIS PRODUCT THAT **WRITES**.
// The activation flow writes, but only for somebody holding a 256-bit invitation
// this business issued; the tap flow writes, but only behind a chip's CMAC. Here
// there is no prior credential of any kind: the only thing between a stranger and
// two permanent rows is what this file does. So its security profile is DERIVED
// rather than copied, and the derivation is:
//
//	Origin        strict on every POST, checked before anything else costs money.
//	synchronizer  a token in an HttpOnly cookie, echoed by every form.
//	signed state  steps cannot be skipped; the VIES verdict cannot be chosen by the
//	              caller (signupstate.go).
//	four budgets  page loads, outbound VIES calls, bcrypt, and CREATED BUSINESSES
//	              are metered separately (signupratelimit.go).
//	bot defence   a honeypot field and a minimum fill time, with the reason a
//	              visible puzzle was rejected written down beside them.
//	no session    this type holds no session codec and issues no cookie a visitor
//	              could be authenticated by. See Done for why the wizard ends at the
//	              sign-in page rather than inside the panel.
//
// WHAT IT DELIBERATELY DOES NOT DO:
//
//   - NOTHING ON THIS TYPE ASKS THE DATABASE WHETHER AN EMAIL EXISTS. That would be
//     an enumeration oracle on an unauthenticated endpoint, and migration 00011's
//     OBLIGATION 1 spends real effort making the LOGIN path refuse to answer that
//     question. There is no field here through which a query could be made at all —
//     this type holds a provisioner and a VAT checker, not a pool.
//
//     ⚠️ AND THE STATEMENT IS SCOPED TO **THIS TYPE**, WHICH IT WAS NOT WHEN IT FIRST
//     READ "IT NEVER ASKS". internal/domain/signup DOES resolve the address, ONCE,
//     AFTER the provisioning transaction has committed, to answer whether the row it
//     just wrote can ever be reached by a sign-in (Provisioner.signInBlocked, and
//     migration 00017 for why that question had to be answerable). It is not the same
//     question and not the same price: it costs a COMPLETED registration rather than a
//     request.
//
//     🔴 IT IS STILL A CHANNEL, AND THIS COMMENT USED TO DENY IT. It read "an address
//     with one, two or seven existing accounts answers exactly as an unknown one
//     does" — true only while the attacker plants nothing, which is the premise every
//     other control in this area is built against.
//     internal/domain/signup.Provisioner.signInBlocked carries the measurement, the
//     price (adminauth.MaxCandidates completed registrations per address, ~3 hours
//     from one address) and the fact that varying the plant count reveals k EXACTLY
//     rather than one bit. It is counted and observable (signup.sign_in_unreachable),
//     not closed. Writing "never" here while a sibling package does it would be the
//     defect this task was blocked for twice — a sentence stating a guarantee wider
//     than the one the product keeps.
//
//     The VAT number is treated differently again, and internal/domain/signup says
//     why: it is public data in a public register.
//
//   - IT DOES NOT SIGN ANYBODY IN. See Done.
type Signup struct {
	provisioner signupProvisioner
	// vat may be nil: every check then answers "not verified", which is stored as
	// such. It is OPTIONAL rather than required because a deployment with no
	// outbound network must still be able to register a customer — the whole point
	// of Q09's "best effort".
	vat vatChecker

	cookies signupCookies
	state   signupState
	// baseURL is this deployment's own origin, for the Origin header check.
	baseURL string

	// See signupratelimit.go for what each may refuse.
	floodLimiter   *limiter
	checkLimiter   *limiter
	attemptLimiter *limiter
	createLimiter  *limiter

	log *slog.Logger
}

// NewSignup wires the wizard. The provisioner is required — a nil one would put a
// "Create my account" button on a screen where pressing it panics, which is the
// M5-04 shape (a capability delivered, tested and DEAD in the wired product because
// two halves were never assembled).
func NewSignup(p signupProvisioner, vat vatChecker, cfg *config.Config, log *slog.Logger) (*Signup, error) {
	if p == nil {
		return nil, errors.New("handler: nil signup provisioner")
	}
	if cfg == nil {
		return nil, errors.New("handler: nil config")
	}
	st, err := newSignupState(cfg)
	if err != nil {
		return nil, err
	}
	if log == nil {
		log = slog.Default()
	}
	return &Signup{
		provisioner:    p,
		vat:            vat,
		cookies:        newSignupCookies(cfg),
		state:          st,
		baseURL:        originOf(cfg.BaseURL),
		floodLimiter:   newLimiter(signupFloodLimit, signupFloodPeriod),
		checkLimiter:   newLimiter(signupCheckLimit, signupCheckPeriod),
		attemptLimiter: newLimiter(signupAttemptLimit, signupAttemptPeriod),
		createLimiter:  newLimiter(signupCreateLimit, signupCreatePeriod),
		log:            log,
	}, nil
}

// Mount registers the routes.
//
// SEVEN ROUTES, THREE FORMS AND ONE CONFIRMATION. Every POST answers 303 rather
// than rendering, which is the shape adminlogin.go argues for and it matters more
// here: a rendered POST means a refresh RE-SUBMITS, and on the last step that would
// mean a second registration attempt with the same password.
//
// NO HEAD ROUTES, UNLIKE handler.Marketing. That surface answers HEAD because
// crawlers and link checkers reach it; this one must not be crawled at all
// (layout.RobotsPrivate below), so a HEAD here would serve nobody and would widen
// the surface for a flow that writes.
func (s *Signup) Mount(r chi.Router) {
	r.Get(signupPath, s.Business)
	r.Post(signupPath, s.SubmitBusiness)
	r.Get(signupPlacesPath, s.Places)
	r.Post(signupPlacesPath, s.SubmitPlaces)
	r.Get(signupAccountPath, s.Account)
	r.Post(signupAccountPath, s.SubmitAccount)
	r.Get(signupDonePath, s.Done)
}

// The failure screens. They follow adminlogin.go's shape: ONE view used for several
// internal outcomes, because the distinction belongs in a log and not in a body.
var (
	// 🔴 THE WORDING IS DELIBERATELY CAUSE-FREE, AND IT DID NOT USED TO BE. It said "a
	// half-finished registration is only kept for half an hour, and this one is past
	// that" — and an audit measured a submission 1.118 s old getting this screen,
	// because the bot defence answers with it too. INDISTINGUISHABILITY IS THE RIGHT
	// DECISION (naming the mechanism tells whoever wrote the script what to change);
	// stating a cause the server knows to be false is not, and it is the same defect
	// class as the two blocking findings this task already carries: a sentence
	// declaring something the product does not provide.
	problemSignupRestart = pages.SignupProblemView{
		Title:   "We could not continue that registration",
		Message: "The form was not accepted. Nothing has been created and nothing has been charged.",
		Hint:    "Start again from the beginning — it takes a couple of minutes. A half-finished registration is also dropped after half an hour.",
		Retry:   signupPath,
	}
	problemSignupNoCookie = pages.SignupProblemView{
		Title:   "Your browser didn't keep the form",
		Message: "Tappa needs a cookie to carry your answers from one step to the next, and this browser is not storing one.",
		Hint:    "Allow cookies for this site, or turn off private browsing, then start again.",
		Retry:   signupPath,
	}
	problemSignupTooMany = pages.SignupProblemView{
		Title:   "Too many attempts",
		Message: "We have stopped accepting registrations from this connection for a few minutes.",
		Hint:    "Wait a little and try again. If you are setting up several businesses, they can be registered a few at a time.",
	}
	problemSignupServer = pages.SignupProblemView{
		Title:   "Something went wrong on our side",
		Message: "Your business was not created and nothing was saved.",
		Hint:    "Try again in a minute.",
		Retry:   signupPath,
	}
)

// Business serves GET /signup — step one of three.
func (s *Signup) Business(w http.ResponseWriter, r *http.Request) {
	if s.flooded(w, r, "signup_business") {
		return
	}
	// A FRESH synchronizer pair on every render of the FIRST step, and only there. It
	// costs 64 bytes of randomness and it means a token left in a shared browser is
	// superseded the moment anybody opens the wizard again. Re-minting it on a LATER
	// step would invalidate the state blob bound to the old one.
	st, err := newSignupLoginState()
	if err != nil {
		// The message says "csrf" rather than the word redline R7 matches inside a
		// log call (activate.go states the same reasoning).
		s.log.Error("signup: minting the csrf value failed", "err", err)
		s.problem(w, r, http.StatusInternalServerError, problemSignupServer)
		return
	}
	s.cookies.setLogin(w, st)
	// Any state from an abandoned attempt is dead the moment a new wizard starts:
	// leaving it would let a stale draft survive into a new registration.
	s.cookies.clear(w, signupStateCookieName)
	s.render(w, r, http.StatusOK, pages.SignupBusiness(pages.SignupBusinessView{
		CSRFToken: st.csrf,
		Types:     businessTypeOptions(),
	}))
}

// SubmitBusiness serves POST /signup.
//
// ORDER OF CHECKS, and why each is where it is:
//
//	flood ceiling  BEFORE anything; the only position a limiter can shed load.
//	Origin         strict. A cross-origin POST here is not merely CSRF — it is a
//	               third-party page starting a registration in somebody's browser.
//	cookie + form + synchronizer   the shared preamble (beginSignupPost).
//	validation     server-side and complete (the card's criterion). The browser's
//	               own validation is convenience only and nothing here trusts it.
//	VIES           LAST, and only for a number that passed the FORMAT check, so
//	               garbage costs no outbound request.
func (s *Signup) SubmitBusiness(w http.ResponseWriter, r *http.Request) {
	if s.flooded(w, r, "signup_business_post") {
		return
	}
	st, ok := s.beginPost(w, r, "signup_business_post")
	if !ok {
		return
	}

	typed := signup.Business{
		Name:         r.PostFormValue("name"),
		VATNumber:    r.PostFormValue("vat_number"),
		BusinessType: r.PostFormValue("business_type"),
		Structure:    r.PostFormValue("structure"),
	}
	b, errs := signup.ValidateBusiness(typed)
	if errs.Any() {
		s.render(w, r, http.StatusUnprocessableEntity, pages.SignupBusiness(pages.SignupBusinessView{
			CSRFToken: st.csrf,
			Types:     businessTypeOptions(),
			// echoSafe: the refused value goes back into the form so the visitor does
			// not lose their typing, but never carries a byte we just declared
			// unstorable onto the wire.
			Name:         echoSafe(b.Name),
			VATNumber:    echoSafe(b.VATNumber),
			BusinessType: b.BusinessType,
			Structure:    b.Structure,
			Errors:       map[string]string(errs),
		}))
		return
	}

	b.VAT = s.checkVAT(r, b.VATNumber)

	// A VENUE LIST ALREADY TYPED IS CARRIED FORWARD, AND THE STEP IS RESET TO 1
	// REGARDLESS. Somebody who reaches step three, is told the VAT number is taken
	// and comes back here has already named their venues; making them type them
	// again would be gratuitous. Resetting the step is what keeps that safe: the
	// venue list is re-validated against the (possibly changed) structure when
	// /signup/places is posted again, which is the only route out of here.
	//
	// THE ISSUED STAMP IS CARRIED TOO, so correcting a field does not extend the
	// wizard's overall lifetime — signupTTL bounds the whole registration, not the
	// current step.
	prior, hasPrior := s.priorDraft(r, st)
	next := signupDraft{
		Name:         b.Name,
		VATNumber:    b.VATNumber,
		BusinessType: b.BusinessType,
		Structure:    b.Structure,
		VATCheck:     b.VAT.String(),
		Step:         1,
	}
	if hasPrior {
		next.Venues = prior.Venues
		next.Issued = prior.Issued
	}
	blob, err := s.state.mint(next, st.bind)
	if err != nil {
		s.log.Error("signup: minting the wizard state failed", "err", err)
		s.problem(w, r, http.StatusInternalServerError, problemSignupServer)
		return
	}
	s.cookies.set(w, signupStateCookieName, blob)
	s.redirect(w, signupPlacesPath)
}

// checkVAT asks VIES, best effort (Q09).
//
// THREE WAYS IT ANSWERS "not verified", and none of them stops the registration:
// no checker is wired, this address is over its outbound budget, or the lookup
// itself failed. The distinction is in the log, never in the response, because a
// visitor can do nothing with any of it.
func (s *Signup) checkVAT(r *http.Request, normalisedVAT string) signup.VATStatus {
	if s.vat == nil {
		return signup.VATUnknown
	}
	ip := clientIP(r)
	if n := s.checkLimiter.Charge(ip); n > signupCheckLimit {
		if s.checkLimiter.FirstOverLimit(n) {
			// Once per window: a shield whose own log lines are unbounded is a disk
			// writer (internal/handler/ratelimit.go's rule).
			s.log.Warn("signup: outbound VAT-check budget reached; registrations continue unverified",
				"ip", ip, "limit", signupCheckLimit, "period", signupCheckPeriod.String())
		}
		return signup.VATUnknown
	}
	status := s.vat.Check(r.Context(), normalisedVAT)
	// The VAT NUMBER IS NOT IN THIS LINE. It identifies a business, this request is
	// unauthenticated, and a flood of guesses would otherwise write a list of them
	// into the process log — the same rule adminlogin.go applies to email addresses
	// on its unauthenticated path.
	s.log.Debug("signup: vat check", "result", status.String())
	return status
}

// Places serves GET /signup/places — step two.
func (s *Signup) Places(w http.ResponseWriter, r *http.Request) {
	if s.flooded(w, r, "signup_places") {
		return
	}
	st, d, ok := s.readState(w, r, 1)
	if !ok {
		return
	}
	s.render(w, r, http.StatusOK, pages.SignupPlaces(s.placesView(st, d, nil, nil)))
}

// SubmitPlaces serves POST /signup/places.
func (s *Signup) SubmitPlaces(w http.ResponseWriter, r *http.Request) {
	if s.flooded(w, r, "signup_places_post") {
		return
	}
	st, ok := s.beginPost(w, r, "signup_places_post")
	if !ok {
		return
	}
	_, d, ok := s.readState(w, r, 1)
	if !ok {
		return
	}

	// The form posts one `venue` field per row. r.PostForm gives them in document
	// order, which is the order they are created in and the order the done screen
	// prints back.
	//
	// THE SLICE IS BOUNDED BEFORE IT IS COPIED. Without this, a caller posting fifty
	// thousand `venue` fields would make this process allocate fifty thousand strings
	// before the domain's MaxVenues check ran. The domain still applies its own bound
	// — that one protects the WRITE — and this one protects the allocation.
	posted := r.PostForm["venue"]
	if len(posted) > signup.MaxVenues+1 {
		posted = posted[:signup.MaxVenues+1]
	}
	typed := make([]signup.Venue, 0, len(posted))
	for _, n := range posted {
		typed = append(typed, signup.Venue{Name: n})
	}

	venues, errs := signup.ValidateVenues(d.Structure, typed)
	if errs.Any() {
		s.render(w, r, http.StatusUnprocessableEntity,
			pages.SignupPlaces(s.placesView(st, d, venues, errs)))
		return
	}

	names := make([]string, 0, len(venues))
	for _, v := range venues {
		names = append(names, v.Name)
	}
	// THE ISSUED STAMP IS CARRIED FORWARD, NOT REFRESHED. signupstate.go says why:
	// it makes signupTTL a bound on the whole wizard and it is what the minimum fill
	// time reads.
	d.Venues = names
	d.Step = 2
	blob, err := s.state.mint(d, st.bind)
	if err != nil {
		s.log.Error("signup: minting the wizard state failed", "err", err)
		s.problem(w, r, http.StatusInternalServerError, problemSignupServer)
		return
	}
	s.cookies.set(w, signupStateCookieName, blob)
	s.redirect(w, signupAccountPath)
}

// Account serves GET /signup/account — step three.
func (s *Signup) Account(w http.ResponseWriter, r *http.Request) {
	if s.flooded(w, r, "signup_account") {
		return
	}
	st, d, ok := s.readState(w, r, 2)
	if !ok {
		return
	}
	s.render(w, r, http.StatusOK, pages.SignupAccount(pages.SignupAccountView{
		CSRFToken:     st.csrf,
		BusinessName:  d.Name,
		Venues:        d.Venues,
		HoneypotField: signupHoneypotField,
	}))
}

// SubmitAccount serves POST /signup/account — the request that creates a business.
//
// IT IS THE ONLY EXPENSIVE REQUEST IN THE FLOW: one bcrypt at cost 12 (~380 ms) and
// one write transaction. Everything before it is a form render. That is why the
// attempt budget is checked here and nowhere else, and why the create budget is
// charged only after the transaction commits.
func (s *Signup) SubmitAccount(w http.ResponseWriter, r *http.Request) {
	if s.flooded(w, r, "signup_account_post") {
		return
	}
	st, ok := s.beginPost(w, r, "signup_account_post")
	if !ok {
		return
	}
	_, d, ok := s.readState(w, r, 2)
	if !ok {
		return
	}

	// THE BUDGET IS CHECKED BEFORE THE ATTEMPT IS COUNTED, so exactly
	// signupAttemptLimit attempts are answered normally and the next one is refused —
	// rather than the limit-th request being the one that trips. adminlogin.go uses
	// the same two-step shape for the same reason.
	ip := clientIP(r)
	if !s.attemptLimiter.Allowed(ip) {
		// The refusal itself is charged: a limiter whose own refusals are free is a
		// cheaper way to reach the thing it protects (ratelimit.go's rule).
		if n := s.attemptLimiter.Charge(ip); s.attemptLimiter.FirstOverLimit(n) {
			s.log.Warn("signup rate limited", "scope", "attempt", "ip", ip,
				"limit", signupAttemptLimit, "period", signupAttemptPeriod.String())
		}
		s.problem(w, r, http.StatusTooManyRequests, problemSignupTooMany)
		return
	}

	// THE BOT DEFENCE, BOTH HALVES, BEFORE ANY EXPENSIVE WORK — see
	// signupratelimit.go for what each catches and for the honest limit of both.
	//
	// A REFUSAL LOOKS LIKE AN EXPIRED WIZARD rather than like a detection. Telling an
	// automated caller which mechanism caught it is telling whoever wrote it what to
	// change; the person who somehow triggered it gets a screen that says to start
	// again, which is true and actionable.
	if r.PostFormValue(signupHoneypotField) != "" {
		s.attemptLimiter.Charge(ip)
		s.log.Warn("signup refused: the honeypot field was filled in", "ip", ip)
		s.problem(w, r, http.StatusBadRequest, problemSignupRestart)
		return
	}
	if age := s.state.age(d); age.Seconds() < signupMinFillSeconds {
		s.attemptLimiter.Charge(ip)
		s.log.Warn("signup refused: the form was completed faster than a person can type",
			"ip", ip, "seconds", age.Seconds(), "minimum", signupMinFillSeconds)
		s.problem(w, r, http.StatusBadRequest, problemSignupRestart)
		return
	}

	typed := signup.Account{
		FullName: r.PostFormValue("full_name"),
		Email:    r.PostFormValue("email"),
		Password: r.PostFormValue("password"),
	}
	account, errs := signup.ValidateAccount(typed)
	if errs.Any() {
		// The form is re-rendered WITHOUT the password: the field comes back empty and
		// the person types it again. A password echoed into a body is a password in
		// the browser's back/forward cache and in "save page".
		s.render(w, r, http.StatusUnprocessableEntity, pages.SignupAccount(pages.SignupAccountView{
			CSRFToken:     st.csrf,
			BusinessName:  d.Name,
			Venues:        d.Venues,
			HoneypotField: signupHoneypotField,
			FullName:      echoSafe(account.FullName),
			Email:         echoSafe(account.Email),
			Errors:        map[string]string(errs),
		}))
		return
	}

	// The create budget is checked BEFORE the transaction and charged after it
	// succeeds, so a refused attempt costs no rows and a failed one costs no budget.
	if !s.createLimiter.Allowed(ip) {
		if n := s.createLimiter.Charge(ip); s.createLimiter.FirstOverLimit(n) {
			s.log.Warn("signup rate limited", "scope", "create", "ip", ip,
				"limit", signupCreateLimit, "period", signupCreatePeriod.String())
		}
		s.problem(w, r, http.StatusTooManyRequests, problemSignupTooMany)
		return
	}
	s.attemptLimiter.Charge(ip)

	res, err := s.provisioner.Provision(r.Context(), signup.Draft{
		Business: d.business(),
		Venues:   d.venues(),
		Account:  account,
	})
	switch {
	case errors.Is(err, signup.ErrVATTaken):
		// The one failure the visitor can act on, and the one place this flow
		// deliberately answers a question about existing data. internal/domain/signup
		// argues why a VAT number is different from an email address: it is public in
		// a public register, so refusing to say costs a real customer the sentence
		// that tells them what to do and buys an attacker nothing.
		//
		// 🔴 IT SENDS THEM BACK TO STEP ONE, WHERE THE FIELD IS. Re-rendering the
		// account form with a VAT error attached would put the message on a page that
		// does not contain the input it is about.
		// The state cookie is left ALONE — it still carries step two and the venue
		// list, so correcting the number and posting step one again keeps everything
		// the visitor already typed (SubmitBusiness carries it forward).
		s.log.Info("signup refused: the VAT number is already registered", "ip", ip)
		s.render(w, r, http.StatusConflict, pages.SignupBusiness(pages.SignupBusinessView{
			CSRFToken:    st.csrf,
			Types:        businessTypeOptions(),
			Name:         d.Name,
			VATNumber:    d.VATNumber,
			BusinessType: d.BusinessType,
			Structure:    d.Structure,
			Errors: map[string]string{
				"vat_number": "That VAT number is already registered with Tappa. If this is your " +
					"business, sign in instead of registering again.",
			},
			SignInHref: adminLoginPath,
		}))
		return
	case err != nil:
		s.log.Error("signup: provisioning failed", "err", err)
		s.problem(w, r, http.StatusInternalServerError, problemSignupServer)
		return
	}
	s.createLimiter.Charge(ip)

	// THE DONE PAGE IS A REDIRECT TARGET, NOT THIS RESPONSE. A rendered POST means a
	// refresh re-submits, which here would mean a second registration attempt.
	//
	// WHAT CARRIES THE RESULT ACROSS THE REDIRECT is the same signed cookie the
	// wizard already uses, re-minted at step 3. It holds a business name, the venue
	// names and one VIES verdict — all of which the visitor just typed or was already
	// shown — and NO id, NO email and NO token. So the done screen needs no query
	// parameter and nothing about the new tenant reaches an address bar, browser
	// history or a Referer.
	done := signupDraft{
		Name:      res.BusinessName,
		VATNumber: d.VATNumber,
		VATCheck:  res.VAT.String(),
		Venues:    res.VenueNames,
		Structure: d.Structure,
		Blocked:   res.SignInBlocked,
		Step:      3,
		// A FRESH stamp, unlike every other transition. The wizard's TTL measured how
		// long somebody had to FILL THE FORM; from here it measures how long the
		// confirmation stays readable on a reload, which is a different clock.
		Issued: 0,
	}
	blob, err := s.state.mint(done, st.bind)
	if err != nil {
		// The business EXISTS at this point. Losing the confirmation screen must not
		// read as a failed registration, so this is an error to us and a redirect to
		// the sign-in page for them.
		s.log.Error("signup: the business was created but its confirmation could not be minted",
			"tenant_id", res.TenantID, "err", err)
		s.redirect(w, adminLoginPath)
		return
	}
	s.cookies.set(w, signupStateCookieName, blob)
	s.redirect(w, signupDonePath)
}

// Done serves GET /signup/done — the APPROVED docket the card asks for.
//
// 🔴 IT DOES NOT SIGN ANYBODY IN, AND THAT IS A DECISION WITH A COST. handoff §9's
// portal ends at "Go to my dashboard"; this ends at the sign-in page with the
// address they just registered. Two reasons, and the first is the one that decides
// it: issuing a panel session here would put adminauth.Manager — a session codec, a
// token minter and a database write — on a type whose whole security argument is
// that it holds none of those, on the one unauthenticated surface in this product
// that writes. The second is that a customer who signs in once, immediately, has
// proved to themselves that the password they just chose works, which is worth more
// than one saved click on the day they set up payroll evidence.
//
// THE COST IS ONE EXTRA STEP and it is named here rather than left as a surprise for
// whoever reads handoff §9 next.
func (s *Signup) Done(w http.ResponseWriter, r *http.Request) {
	if s.flooded(w, r, "signup_done") {
		return
	}
	_, d, ok := s.readState(w, r, 3)
	if !ok {
		return
	}
	// The confirmation is spent: both cookies are cleared so a shared browser does
	// not keep a finished registration lying around, and so a reload after a back
	// button shows the honest "that expired" screen rather than a second APPROVED
	// stamp over a business that was created once.
	s.cookies.clear(w, signupStateCookieName)
	s.cookies.clear(w, signupCookieName)
	if d.Blocked {
		// 🔴 THE ONE CASE WHERE THE CONFIRMATION MUST NOT SAY "SIGN IN". The account
		// exists and cannot be used: internal/domain/signup MEASURED that this address
		// resolves past the window a login compares. Sending them to a form that
		// answers 401 with no explanation — because 00011's OBLIGATION 1 forbids one —
		// is the defect an audit found under the APPROVED stamp.
		s.log.Warn("signup: the new account cannot be reached by sign-in",
			"ip", clientIP(r), "venues", len(d.Venues))
	}
	s.render(w, r, http.StatusOK, pages.SignupDone(pages.SignupDoneView{
		BusinessName:  d.Name,
		Venues:        d.Venues,
		VATNumber:     d.VATNumber,
		VATVerified:   d.VATCheck == signup.VATValid.String(),
		VATRefused:    d.VATCheck == signup.VATInvalid.String(),
		SignInBlocked: d.Blocked,
		SignInHref:    adminLoginPath,
	}))
}

// ------------------------------------------------------------------ plumbing --

// priorDraft reads an existing wizard state WITHOUT answering the request on
// failure. It is the one place a missing or unreadable state is not an error: on
// step one there may genuinely not be one yet, and a corrupt one simply means the
// visitor starts from a blank venue list rather than seeing a screen about it.
//
// IT STILL VERIFIES THE SIGNATURE — this calls state.parse, so nothing unsigned can
// influence what is carried forward.
func (s *Signup) priorDraft(r *http.Request, st signupLoginState) (signupDraft, bool) {
	ck, err := r.Cookie(signupStateCookieName)
	if err != nil || ck.Value == "" {
		return signupDraft{}, false
	}
	d, err := s.state.parse(ck.Value, st.bind)
	if err != nil {
		return signupDraft{}, false
	}
	return d, true
}

// beginPost runs the checks every wizard POST shares: Origin, the synchronizer
// cookie, the form, and the token. It returns ok=false having ALREADY answered.
func (s *Signup) beginPost(w http.ResponseWriter, r *http.Request, where string) (signupLoginState, bool) {
	if !s.sameOrigin(r) {
		s.log.Warn("signup refused: not same-origin", "at", where, "ip", clientIP(r))
		s.problem(w, r, http.StatusBadRequest, problemSignupRestart)
		return signupLoginState{}, false
	}
	st, ok := s.cookies.readLogin(r)
	if !ok {
		// No cookie: either the wizard was never opened in this browser, or cookies
		// are blocked. The screen says which to fix. The branch is decided entirely by
		// the caller's own request, so it reveals nothing.
		s.problem(w, r, http.StatusBadRequest, problemSignupNoCookie)
		return signupLoginState{}, false
	}
	// A BOUNDED BODY. ParseForm reads the whole request into memory, and this is an
	// unauthenticated POST — without a limit a single request decides how much this
	// process allocates. 64 KB is far above the largest legitimate submission (a
	// company name, a VAT number, up to MaxVenues venue names and three account
	// fields).
	r.Body = http.MaxBytesReader(w, r.Body, signupMaxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.problem(w, r, http.StatusBadRequest, problemSignupServer)
		return signupLoginState{}, false
	}
	if !st.csrfMatches(r.PostFormValue("csrf")) {
		// An attack signal, not a user slip.
		s.log.Warn("signup refused: csrf mismatch", "at", where, "ip", clientIP(r))
		s.problem(w, r, http.StatusBadRequest, problemSignupRestart)
		return signupLoginState{}, false
	}
	return st, true
}

// signupMaxFormBytes bounds a wizard submission. See beginPost.
const signupMaxFormBytes = 64 << 10

// echoSafe strips the bytes a re-rendered form must not carry back.
//
// 🔴 A 422 USED TO PUT A RAW NUL IN THE RESPONSE. The refused value is echoed into the
// form's value= so the visitor does not lose their typing, and internal/domain/signup
// refuses NUL and invalid UTF-8 precisely because the DATABASE cannot store them — so
// the product was declaring a byte unstorable and then writing it onto the wire. It is
// not XSS (templ escapes, and a browser maps U+0000 to U+FFFD), but it pollutes every
// byte-oriented thing between here and the reader: a WAF, a log shipper, a proxy that
// treats NUL as a terminator.
//
// INVALID UTF-8 GOES THE SAME WAY AND FOR THE SAME REASON. strings.ToValidUTF8
// replaces it rather than dropping the field, so the person still sees what they typed
// with the offending bytes removed and the error beside it.
func echoSafe(s string) string {
	return strings.ToValidUTF8(strings.ReplaceAll(s, "\x00", ""), "")
}

// readState reads and verifies the signed draft and requires it to have completed
// at least `step`. It returns ok=false having already answered.
//
// 🔴 THE STEP CHECK IS WHAT MAKES THE SEQUENCE UNSKIPPABLE. Without it a caller
// could take a step-1 blob straight to POST /signup/account and register a business
// with no venues — the one write in this flow that must not be reachable early.
//
// IT REQUIRES AN EXACT STEP FOR THE CONFIRMATION (3) AND AT LEAST THE ASKED-FOR STEP
// OTHERWISE, which is deliberate in both directions: going BACK to the venue form
// after reaching the account form is ordinary (the wizard's own links do it), while
// a finished registration must not be able to walk back into a form and create a
// second business from the same cookie.
func (s *Signup) readState(w http.ResponseWriter, r *http.Request, step int) (signupLoginState, signupDraft, bool) {
	st, ok := s.cookies.readLogin(r)
	if !ok {
		s.problem(w, r, http.StatusBadRequest, problemSignupNoCookie)
		return signupLoginState{}, signupDraft{}, false
	}
	ck, err := r.Cookie(signupStateCookieName)
	if err != nil || ck.Value == "" {
		s.problem(w, r, http.StatusBadRequest, problemSignupRestart)
		return signupLoginState{}, signupDraft{}, false
	}
	d, err := s.state.parse(ck.Value, st.bind)
	if err != nil {
		// An expired draft and a tampered one get the SAME screen; the difference is
		// only in what we log. A signature failure is worth an error line because it
		// means somebody presented a value this deployment did not mint (or minted for
		// a different browser); an expiry is ordinary.
		if errors.Is(err, errSignupSignature) || errors.Is(err, errSignupMalformed) {
			s.log.Error("signup state rejected", "ip", clientIP(r), "err", err)
		}
		s.cookies.clear(w, signupStateCookieName)
		s.problem(w, r, http.StatusBadRequest, problemSignupRestart)
		return signupLoginState{}, signupDraft{}, false
	}
	if (step == signupDoneStep && d.Step != signupDoneStep) || (step != signupDoneStep && d.Step < step) {
		s.problem(w, r, http.StatusBadRequest, problemSignupRestart)
		return signupLoginState{}, signupDraft{}, false
	}
	return st, d, true
}

// signupDoneStep is the state a completed registration carries. It is named so the
// exact-match rule in readState reads as a rule rather than as a magic number.
const signupDoneStep = 3

// flooded is the entry gate on every wizard handler. It reports true (and has
// already answered) when this address is over the ceiling.
//
// IT WRITES NO AUDIT ROW, a stated limit rather than an oversight: nothing on this
// path has a tenant until the last statement of the last step, and audit_log.tenant_id
// is NOT NULL with an FK (00005). The trail for a flooded caller is the process log,
// written once per window so the shield cannot be turned into an unbounded disk
// writer.
func (s *Signup) flooded(w http.ResponseWriter, r *http.Request, where string) bool {
	ip := clientIP(r)
	n := s.floodLimiter.Charge(ip)
	if n <= signupFloodLimit {
		return false
	}
	if s.floodLimiter.FirstOverLimit(n) {
		s.log.Warn("signup flood ceiling reached", "ip", ip, "at", where,
			"limit", signupFloodLimit, "period", signupFloodPeriod.String())
	}
	s.problem(w, r, http.StatusTooManyRequests, problemSignupTooMany)
	return true
}

// sameOrigin checks the Origin header against this deployment's own, STRICTLY: an
// absent Origin falls back to the fetch metadata and is refused if that is missing
// too.
//
// STRICT, LIKE THE PANEL AND UNLIKE THE ACTIVATION FLOW. The activation version
// allows an absent header because its audience is an arbitrary phone opening a link
// from a chat app; this audience is somebody who arrived from our own landing page
// in a desktop or mobile browser, and every browser that can run this wizard sends
// Origin on a cross-origin POST. What refusing buys is the one attack the
// synchronizer token cannot stop — an attacker who loads the form in their own
// browser holds a matching (cookie, token) pair and can plant both.
//
// IT IS A THIRD COPY OF THIS FUNCTION AND THAT IS DELIBERATE, for the reason
// adminlogin.go gives about the second: the activation version carries a `strict`
// flag whose two settings were argued over four audit rounds, and re-shaping it as a
// side effect of adding a wizard is how a settled flow acquires an unreviewed change.
// The duplication is named so it is a decision rather than an accident.
func (s *Signup) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		switch strings.ToLower(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site"))) {
		case "same-origin", "same-site":
			return true
		default:
			return false
		}
	}
	return strings.EqualFold(strings.TrimRight(origin, "/"), strings.TrimRight(s.baseURL, "/"))
}

// placesView builds step two's view model.
func (s *Signup) placesView(st signupLoginState, d signupDraft, venues []signup.Venue, errs signup.Errors) pages.SignupPlacesView {
	names := d.Venues
	if venues != nil {
		names = make([]string, 0, len(venues))
		for _, v := range venues {
			// echoSafe for the reason given at its definition: a refused venue name is
			// echoed back into the form, and it is the field most likely to carry the
			// byte internal/domain/signup just refused.
			names = append(names, echoSafe(v.Name))
		}
	}
	if len(names) == 0 {
		// One empty row so the form is usable on first arrival. A `single` business
		// gets exactly one and no way to add more — the structure decided that in
		// step one, and offering an "add" button that the server would then refuse
		// would be a screen contradicting itself.
		names = []string{""}
	}
	return pages.SignupPlacesView{
		CSRFToken:    st.csrf,
		BusinessName: d.Name,
		Multi:        d.Structure == signup.StructureMulti,
		MaxVenues:    signup.MaxVenues,
		Venues:       names,
		Errors:       map[string]string(errs),
		BackHref:     signupPath,
	}
}

// businessTypeOptions maps the domain's chip set to the view's.
//
// IT IS A MAPPING RATHER THAN A SECOND LIST. internal/domain/signup.BusinessTypes is
// pinned against migration 00001's CHECK by a test; a literal here would be a third
// copy of a vocabulary the database owns.
func businessTypeOptions() []pages.SignupChoice {
	out := make([]pages.SignupChoice, 0, len(signup.BusinessTypes))
	for _, t := range signup.BusinessTypes {
		out = append(out, pages.SignupChoice{Value: t.Value, Label: t.Label})
	}
	return out
}

// render writes one component with the headers this surface needs.
//
// 🔴 Cache-Control: no-store, WHICH IS THE OPPOSITE OF THE MARKETING SURFACE NEXT
// DOOR AND IS THE POINT. handler.Marketing sends `public, max-age=300` and states
// the premise that makes it safe: "The bytes are the same for every caller". That is
// false here in every possible way — these responses carry a synchronizer token, a
// company name, a VAT number, venue names and, on a re-rendered form, somebody's own
// email address. Copying that header would have been "copied half the pattern"
// running in reverse: bringing across a header that belongs to a different threat
// model.
func (s *Signup) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", signupCSP)
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		// A visitor who left mid-render is not a fault — handler.Marketing measured
		// what logging it at ERROR cost on a public URL (200 cancelled page loads,
		// 200 ERROR lines) and the same reasoning applies to a public form.
		if errors.Is(err, context.Canceled) {
			s.log.Debug("a visitor left before the page finished", "path", r.URL.Path)
			return
		}
		// The status line is already on the wire, so there is nothing to send but a
		// log line. Never swallowed (§7).
		s.log.Error("rendering a signup page failed", "err", err)
	}
}

func (s *Signup) problem(w http.ResponseWriter, r *http.Request, status int, v pages.SignupProblemView) {
	s.render(w, r, status, pages.SignupProblem(v))
}

// redirect answers 303 See Other: the browser follows with GET, which turns a POST
// into a plain page load and makes a refresh harmless.
func (s *Signup) redirect(w http.ResponseWriter, to string) {
	w.Header().Set("Cache-Control", "no-store")
	// nosniff ON THE REDIRECT TOO, which adminlogin.go's twin does not do. It costs
	// nothing and it means EVERY response from this surface carries it rather than
	// most of them — a header set on some responses is a header nobody can assert
	// about the surface, and this one is asserted.
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Location", to)
	w.WriteHeader(http.StatusSeeOther)
}

// signupCSP is the content policy for the wizard.
//
// IT IS DERIVED DIRECTIVE BY DIRECTIVE FROM WHAT THESE PAGES LOAD, not inherited
// from the marketing surface or from the panel — the argument handler.Marketing
// makes about NOT writing `= adminCSP`, applied a third time:
//
//	default-src 'none'      nothing loads unless named below
//	style-src 'self'        one stylesheet, /static/css/app.css
//	font-src 'self'         the six woff2 files app.css declares, same origin
//	form-action 'self'      THE DIRECTIVE THAT EARNS ITS PLACE HERE. This is the one
//	                        surface in the product with a public form carrying a
//	                        PASSWORD, and form-action does not fall back to
//	                        default-src — without it a browser would let that form be
//	                        repointed at another host.
//	base-uri 'none'         a <base> tag cannot re-root the relative asset paths
//	frame-ancestors 'none'  the wizard must not be framed under somebody else's page,
//	                        which for a form that takes a password is clickjacking
//	                        with a keyboard.
//
// NO script-src AND NO img-src, because there is no script and no image: the
// wizard's chrome is the marketing chrome, whose plaque and docket are drawn with
// CSS. Naming a directive for something that does not load would make adding it a
// silent inheritance instead of a visible edit.
//
// ⚠️ IT IS BYTE-IDENTICAL TO marketingCSP AND adminCSP TODAY AND THAT IS NOT A
// DEPENDENCY. All three surfaces happen to load exactly one stylesheet and the brand
// faces, so the same six directives fall out of three separate derivations. The test
// checks the directives one at a time rather than comparing strings, so the three are
// free to diverge without a test having to be edited to permit it.
const signupCSP = "default-src 'none'; style-src 'self'; font-src 'self'; " +
	"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"
