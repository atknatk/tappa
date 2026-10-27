package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/web/templates/email"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE VERIFIED REGISTRATION (M14-17, ADR 0031 §5.10). The wizard's last POST no longer creates a
// business: it opens a DRAFT (adminauth.SignupClaims.Open -- admin_signup_claims, nothing else) and
// mails a link to the address; the business, the identity and the owner membership are born only when
// that link is CONFIRMED -- a POST from the link's page, never its GET (R19).
//
//	POST /signup/account           the draft, the link handed to the outbox, the SAME response after the
//	                               SAME floor for every address (R18) -> 303 /signup/sent
//	GET  /signup/sent              "Check your inbox" (and, on a dev or sandbox installation that sends no
//	                               e-mail, the link itself -- the user's decision of 2026-10-26)
//	GET  /admin/signup/confirm?t=  the token into a short cookie, 303 to the clean URL (adminreset.go's
//	                               readLink pattern: the token leaves the address bar after one hop)
//	GET  /admin/signup/confirm     the page: the new address re-enters its password (R2); an address that
//	                               already has a sign-in is asked to sign in in this browser first (R18);
//	                               an address whose sign-in has no active business left (it cannot sign
//	                               in) is asked for that sign-in's password (M14-17b)
//	POST /admin/signup/confirm     the proof (a password compared HERE, in the application -- S12 -- or
//	                               this browser's panel session) and the business, in one transaction
//
// WHY UNDER /admin: the existing identity's proof is its panel session, and the panel cookie's Path is
// /admin (adminauth.CookiePath) -- a page under /signup would never receive it.
//
// WHAT THE FLAG DOES NOT DO (ADR 0031 §8): TAPPA_ADMIN_IDENTITY does not gate any of this -- the verified
// registration is open with the flag off, and the two addresses see the same "Check your inbox".

const (
	signupSentPath = "/signup/sent"
	// signupConfirmPath is pages.SignupConfirmPath written as a literal (the cookie-path route scan
	// resolves literals; TestSignupConfirmPath_IsThePagesOwn holds the two equal).
	signupConfirmPath = "/admin/signup/confirm"

	// signupClaimScope is the per-recipient cap's scope for registration letters (ADR 0031 §5.10: the
	// EM-7C (mailbox) bucket's shape, 5 an hour and 20 a day per mailbox -- mail.RecipientCap).
	signupClaimScope = "signup"

	// signupConfirmCookieName carries "<csrf>.<token>" from the link's GET to its POST, on the
	// confirmation's path only, for signupConfirmCookieMaxAge.
	signupConfirmCookieName   = "tappa_signup_claim"
	signupConfirmCookieMaxAge = 30 * 60

	// signupClaimFloor is how long POST /signup/account takes when it opens a draft, whatever the
	// address. The work is the same for both arms (one bcrypt at cost 12 -- the floor's bulk -- one
	// resolver read, one insert; the letter is chosen in memory and the send is the outbox's), so the
	// floor is the belt over the residue: measured by
	// TestSignupAccount_ANewAndAKnownAddressAreIndistinguishable.
	signupClaimFloor = 600 * time.Millisecond

	// signupConfirmLimit / Period: confirmation POSTs per address (the password comparison is a cost-12
	// bcrypt -- adminResetSubmitLimit's arithmetic and number).
	signupConfirmLimit  = 10
	signupConfirmPeriod = 10 * time.Minute

	// signupClaimLinkLimit / Period: refused identity-password confirmations per registration LINK
	// (M14-17b; the 2026-10-27 security audit's ORTA finding, the user's decision "İkisini birden"), keyed
	// by the draft's id -- never its token or its hash (ratelimit.go's `invite` key discipline). IT IS A
	// GATE, ratelimit.go's inviteFailureLimit in shape and number: checked BEFORE the attempt, so exactly
	// ten refusals are answered normally and every later attempt on the link -- the right password
	// included -- gets the same "too many attempts" page, and the gate's own refusal is charged too
	// (activate.go's overInviteBudget). Ten: a person mistyping a password a few times stays well inside
	// it; a loop does not. It bounds ONE link; signupClaimIdentityLimit bounds every link of an identity.
	signupClaimLinkLimit  = 10
	signupClaimLinkPeriod = 10 * time.Minute

	// signupClaimIdentityLimit / Period: attempts on the identity-password branch per IDENTITY, every link
	// together (M14-17b; the user's decision of 2026-10-27, "Ret sayacını ayır"). Reserved before the
	// comparison (a right password spends its unit too); past it the branch refuses without comparing.
	// ITS OWN COUNTER, not the sign-in's: the sign-in charges the account limiter per identity, but a
	// sign-in-less identity's sign-in answers every password the same -- no oracle -- so sharing that
	// budget bought nothing and let anybody who knows the address keep this branch paused forever with ten
	// failed sign-ins a window. Here only the link holder can spend it (the mailbox; in an on-screen
	// sandbox or dev installation anybody can -- accepted: no customer data there). TEN IN TEN MINUTES is
	// adminAccountLimit's number and window: the per-account ceiling the sign-in already uses for the same
	// identity's trail, so one guesser gets no more against one identity here than its trail admits there.
	signupClaimIdentityLimit  = 10
	signupClaimIdentityPeriod = 10 * time.Minute

	// signupOutboxSize, signupSendGrace: the letters' outbox (adminresetoutbox.go's shape, without the
	// audit rows -- a draft has no business to write one into). A letter the outbox cannot take is not
	// sent and is logged by its claim id; the person starts again. Its drain runs beside the reset
	// outbox's, inside handler.ResetDrainGrace (cmd/tappa's drainers).
	signupOutboxSize = 32
	signupSendGrace  = 15 * time.Second
)

// signupClaimer is the slice of adminauth.SignupClaims this flow needs (§7).
type signupClaimer interface {
	Open(ctx context.Context, email, password string, draft []byte) (adminauth.ClaimGrant, error)
	Resolve(ctx context.Context, t adminauth.ResetToken) (adminauth.SignupClaim, error)
	ProveWithPassword(c adminauth.SignupClaim, password string) (adminauth.ClaimProof, bool)
	ProveWithSession(c adminauth.SignupClaim, session adminauth.Token) (adminauth.ClaimProof, bool)
	ProveWithIdentityPassword(ctx context.Context, c adminauth.SignupClaim, password string, budget adminauth.FailureBudget) (adminauth.ClaimProof, error)
	Now() time.Time
	Sweep(ctx context.Context) (int, error)
}

var _ signupClaimer = (*adminauth.SignupClaims)(nil)

// SignupMailSender is what the registration letters need from internal/mail: the per-recipient cap
// around the process's ONE breaker (*mail.RecipientCap) -- every letter counted by both, refusable by
// either (ADR 0022 §9, EM-7C).
type SignupMailSender interface {
	Send(ctx context.Context, scope string, m mail.Message) (mail.Receipt, error)
}

var _ SignupMailSender = (*mail.RecipientCap)(nil)

// signupDelivery is how a registration link reaches the address (the user's decision of 2026-10-26).
type signupDelivery int

const (
	// signupByEmail: TAPPA_RESET_DELIVERY=email -- the letter, through the cap and the breaker.
	signupByEmail signupDelivery = iota + 1
	// signupOnScreen: no e-mail on a dev or sandbox installation -- the link on "Check your inbox".
	signupOnScreen
	// signupClosed: no e-mail on any other installation -- the wizard says it takes no registrations.
	signupClosed
)

// signupDeliveryFor decides it from the configuration and the sender cmd/tappa built (one iff email).
func signupDeliveryFor(cfg *config.Config, mailer SignupMailSender) (signupDelivery, error) {
	email := cfg.ResetDelivery == config.ResetDeliveryEmail
	switch {
	case email && mailer == nil:
		return 0, errors.New("handler: TAPPA_RESET_DELIVERY=email and no registration mailer")
	case !email && mailer != nil:
		return 0, errors.New("handler: a registration mailer without TAPPA_RESET_DELIVERY=email")
	case email:
		return signupByEmail, nil
	case cfg.RegistrationLinksOnScreen():
		return signupOnScreen, nil
	}
	return signupClosed, nil
}

var problemSignupClosed = pages.SignupProblemView{
	Title:   "Registration is closed here",
	Message: "This installation of Taptime has no email set up, so it cannot take new registrations.",
	Hint:    "If you were expecting to register a business, contact Taptime.",
}

// closed answers the wizard's every request on an installation that takes no registrations. It reports
// whether it answered.
func (s *Signup) closed(w http.ResponseWriter, r *http.Request) bool {
	if s.delivery != signupClosed {
		return false
	}
	s.problem(w, r, http.StatusServiceUnavailable, problemSignupClosed)
	return true
}

// ---------------------------------------------------------------- the letters --

// signupJob is one letter waiting for the worker. It carries the link (a §4.7 secret: never logged,
// never stored -- the outbox is memory), the address and which letter.
type signupJob struct {
	base      context.Context
	claimID   uuid.UUID
	recipient string
	link      string
	expiresAt time.Time
	known     bool
}

// signupOutbox is the bounded hand-over from the request to the one worker (adminresetoutbox.go's
// reasons: the relay's seconds must not reach the response's timing -- R18).
type signupOutbox struct {
	jobs   chan signupJob
	mu     sync.Mutex
	closed bool
	done   chan struct{}
	stop   context.Context
	cancel context.CancelFunc
}

func newSignupOutbox(size int) *signupOutbox {
	q := &signupOutbox{jobs: make(chan signupJob, size), done: make(chan struct{})}
	q.stop, q.cancel = context.WithCancel(context.Background())
	return q
}

// offer hands one letter over without blocking; false when full or closing.
func (q *signupOutbox) offer(j signupJob) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	select {
	case q.jobs <- j:
		return true
	default:
		return false
	}
}

func (q *signupOutbox) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if !q.closed {
		q.closed = true
		close(q.jobs)
	}
}

// work is the one worker goroutine; it returns when the outbox is closed and empty.
func (s *Signup) work() {
	defer close(s.outbox.done)
	for j := range s.outbox.jobs {
		s.sendContained(j)
	}
}

// sendContained sends one letter; a panic ends the letter, never the worker (the panic value may carry
// the link or the address, so it is not logged).
func (s *Signup) sendContained(j signupJob) {
	defer func() {
		if recover() != nil {
			s.log.ErrorContext(j.base, "signup: a registration letter panicked; the worker recovered", "claim_id", j.claimID)
		}
	}()
	if s.outbox.stop.Err() != nil {
		s.log.WarnContext(j.base, "signup: the process stopped before the registration letter was sent", "claim_id", j.claimID)
		return
	}
	ctx, cancel := boundedBy(j.base, signupSendGrace, s.outbox.stop)
	defer cancel()
	if err := s.deliver(ctx, j); err != nil {
		class, code, ok := sendErrorClass(err)
		if ok {
			s.log.WarnContext(j.base, "signup: the registration letter was not sent", "claim_id", j.claimID,
				"class", class, "smtp_code", code)
			return
		}
		s.log.WarnContext(j.base, "signup: the registration letter was not sent", "claim_id", j.claimID,
			"err_type", fmt.Sprintf("%T", err))
	}
}

// deliver renders the letter -- the new address's verification or the known address's -- and sends it
// through the cap (scope "signup") and the breaker. The lifetime it states is the time left.
func (s *Signup) deliver(ctx context.Context, j signupJob) error {
	v := email.SignupView{BaseURL: s.mailBase, ValidFor: time.Until(j.expiresAt)}
	render := email.RenderSignupVerification
	if j.known {
		render = email.RenderSignupKnownAddress
	}
	m, err := render(ctx, j.link, v)
	if err != nil {
		return fmt.Errorf("handler: rendering the registration letter: %w", err)
	}
	m.To = j.recipient
	m.Ref = j.claimID.String()
	receipt, err := s.mailer.Send(ctx, signupClaimScope, m)
	if err != nil {
		return err
	}
	// The same line for both letters (R18): the claim's id and the relay's, never the address or link.
	s.log.InfoContext(ctx, "signup: the relay accepted the registration letter", "claim_id", j.claimID,
		"message_id", receipt.MessageID)
	return nil
}

// Drain stops the outbox and waits for the worker inside ctx (cmd/tappa's shutdown, beside the reset
// outbox). With no outbox it returns at once.
func (s *Signup) Drain(ctx context.Context) error {
	q := s.outbox
	if q == nil {
		return nil
	}
	q.close()
	select {
	case <-q.done:
		return nil
	case <-ctx.Done():
		q.cancel()
		return fmt.Errorf("handler: the registration outbox did not drain within its budget: %w", ctx.Err())
	}
}

// SweepEvery runs identity_sweep_signup_claims every period until ctx ends (R17: expired and used drafts
// go; nothing else deletes from admin_signup_claims). A failed sweep is logged and tried again next time.
func (s *Signup) SweepEvery(ctx context.Context, period time.Duration) {
	t := time.NewTicker(period)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n, err := s.claims.Sweep(ctx)
			if err != nil {
				s.log.ErrorContext(ctx, "signup: sweeping the registration drafts failed", "err", err)
				continue
			}
			s.log.InfoContext(ctx, "signup: registration drafts swept", "deleted", n)
		}
	}
}

// dispatch hands a just-opened draft's link to the address -- the outbox (email) or the screen (dev and
// sandbox) -- and returns the link to show, "" when it is not to be shown. It never blocks.
func (s *Signup) dispatch(r *http.Request, g adminauth.ClaimGrant) string {
	link := g.Link(s.confirmBase)
	switch s.delivery {
	case signupOnScreen:
		return link
	case signupByEmail:
		j := signupJob{base: context.WithoutCancel(r.Context()), claimID: g.ClaimID, recipient: g.Recipient,
			link: link, expiresAt: g.ExpiresAt, known: g.KnownAddress}
		if !s.outbox.offer(j) {
			// The same line for both addresses; the person can start again.
			s.log.WarnContext(r.Context(), "signup: the delivery queue did not take the registration letter, so it was not sent",
				"claim_id", g.ClaimID)
		}
	}
	return ""
}

// ------------------------------------------------------------------ the pages --

// Sent serves GET /signup/sent -- "Check your inbox". It reads the wizard's state (step 3, the draft
// opened) and spends it, like the old done page: a reload shows the honest "start again".
func (s *Signup) Sent(w http.ResponseWriter, r *http.Request) {
	if s.flooded(w, r, "signup_sent") || s.closed(w, r) {
		return
	}
	_, d, ok := s.readState(w, r, signupDoneStep)
	if !ok {
		return
	}
	s.cookies.clear(w, signupStateCookieName)
	s.cookies.clear(w, signupCookieName)
	view := pages.SignupSentView{BusinessName: d.Name, LinkLife: signupLinkLife}
	if s.delivery == signupOnScreen {
		view.ScreenLink = d.Link
	}
	s.render(w, r, http.StatusOK, pages.SignupSent(view))
}

// signupLinkLife is a registration link's life as its letter states it (email.Lifetime of
// adminauth.SignupClaimTTL -- "23 hours": the TTL is the 24-hour ceiling less the clock-skew margin, and
// the length floors). The pages that state it print this, never a literal (M14-17b's audit, DÜŞÜK-1).
var signupLinkLife = func() string {
	life, err := email.Lifetime(adminauth.SignupClaimTTL)
	if err != nil {
		panic("handler: the registration link's lifetime does not render: " + err.Error())
	}
	return life
}()

// The confirmation's failure screens.
var (
	problemSignupLinkDead = pages.SignupProblemView{
		Title:   "This link no longer works",
		Message: "A registration link works once, for " + signupLinkLife + ". This one has been used or has expired.",
		Hint:    "If your business was not created, start again — it takes a couple of minutes.",
		Retry:   signupPath,
	}
	problemSignupConfirmNoCookie = pages.SignupProblemView{
		Title:   "Open the link from your email again",
		Message: "This page needs the link from the email in the same browser, and this browser does not have it.",
		Hint:    "Allow cookies for this site, then open the link from the email again.",
	}
	problemSignupClaimRefused = pages.SignupProblemView{
		Title:   "We could not finish this registration",
		Message: "Nothing was created.",
		Hint:    "If this email address already has a Taptime sign-in, sign in with it in this browser and open the link from your email again.",
	}
)

// Confirm serves GET /admin/signup/confirm. With ?t= it moves the token into a short cookie and
// redirects to the clean URL; without, it shows the page. IT SPENDS NOTHING (R19): the resolver reads.
func (s *Signup) Confirm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if s.flooded(w, r, "signup_confirm") || s.closed(w, r) {
		return
	}
	if t := r.URL.Query().Get("t"); t != "" {
		// adminreset.go's three transport refusals (the shape gate is adminauth's hash).
		if strings.ContainsAny(t, ".") || len(t) > 200 || !cookieSafe(t) {
			s.problem(w, r, http.StatusBadRequest, problemSignupLinkDead)
			return
		}
		csrf, err := newCSRFToken()
		if err != nil {
			s.log.Error("signup: minting the csrf value failed", "err", err)
			s.problem(w, r, http.StatusInternalServerError, problemSignupServer)
			return
		}
		s.setConfirmCookie(w, csrf+"."+t)
		s.redirect(w, signupConfirmPath)
		return
	}
	csrf, tok, ok := s.readConfirmCookie(r)
	if !ok {
		s.problem(w, r, http.StatusBadRequest, problemSignupConfirmNoCookie)
		return
	}
	c, ok := s.liveClaim(w, r, tok)
	if !ok {
		return
	}
	view, err := s.confirmView(csrf, c)
	if err != nil {
		s.log.Error("signup: a stored draft did not decode", "claim_id", c.ID, "err", err)
		s.problem(w, r, http.StatusInternalServerError, problemSignupServer)
		return
	}
	s.render(w, r, http.StatusOK, pages.SignupConfirm(view))
}

// ConfirmSubmit serves POST /admin/signup/confirm: the proof, then the business.
//
// 🔴 THE NEW ADDRESS'S PASSWORD IS COMPARED HERE (S12): adminauth.ProveWithPassword is the only way to a
// proof for it, and a mismatch re-renders the form with nothing created (R2). The known address's proof
// is this browser's panel session; identity_claim_signup decides whether it is the address's person
// (R18). A known address whose identity has no active membership (M14-17b) cannot sign in: its proof is
// the identity's password, compared here too (adminauth.ProveWithIdentityPassword) -- the database
// claims only while that digest is still the identity's and the identity still cannot sign in. Each way
// the business is created by signup.Provision with that proof, in ONE transaction.
//
// IT DOES NOT SIGN ANYBODY IN (the wizard's rule since M7-02): it ends at the sign-in page. The done
// docket is rendered by this response (200), not behind a redirect -- the draft is spent, so a refresh
// that re-posts finds "this link no longer works" and creates nothing.
func (s *Signup) ConfirmSubmit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if s.flooded(w, r, "signup_confirm_post") || s.closed(w, r) {
		return
	}
	if !s.sameOrigin(r) {
		s.log.Warn("signup refused: not same-origin", "at", "signup_confirm_post", "ip", clientIP(r))
		s.problem(w, r, http.StatusBadRequest, problemSignupConfirmNoCookie)
		return
	}
	csrf, tok, ok := s.readConfirmCookie(r)
	if !ok {
		s.problem(w, r, http.StatusBadRequest, problemSignupConfirmNoCookie)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, signupMaxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.problem(w, r, http.StatusBadRequest, problemSignupServer)
		return
	}
	if !constantTimeMatch(csrf, r.PostFormValue("csrf")) {
		s.log.Warn("signup refused: csrf mismatch", "at", "signup_confirm_post", "ip", clientIP(r))
		s.problem(w, r, http.StatusBadRequest, problemSignupConfirmNoCookie)
		return
	}
	ip := clientIP(r)
	if !s.confirmLimiter.Allowed(ip) {
		if n := s.confirmLimiter.Charge(ip); s.confirmLimiter.FirstOverLimit(n) {
			s.log.Warn("signup rate limited", "scope", "confirm", "ip", ip,
				"limit", signupConfirmLimit, "period", signupConfirmPeriod.String())
		}
		s.problem(w, r, http.StatusTooManyRequests, problemSignupTooMany)
		return
	}
	s.confirmLimiter.Charge(ip)

	c, ok := s.liveClaim(w, r, tok)
	if !ok {
		return
	}
	view, err := s.confirmView(csrf, c)
	if err != nil {
		s.log.Error("signup: a stored draft did not decode", "claim_id", c.ID, "err", err)
		s.problem(w, r, http.StatusInternalServerError, problemSignupServer)
		return
	}

	var proof adminauth.ClaimProof
	if c.IdentityPassword {
		if proof, ok = s.proveIdentityPassword(w, r, c, view); !ok {
			return
		}
	} else if c.KnownAddress {
		session, err := s.panelCookies.Read(r)
		if err == nil {
			proof, ok = s.claims.ProveWithSession(c, session)
		}
		if err != nil || !ok {
			view.NotSignedIn = true
			s.render(w, r, http.StatusForbidden, pages.SignupConfirm(view))
			return
		}
	} else {
		password := r.PostFormValue("password")
		if password == "" || len(password) > signup.MaxPasswordBytes {
			view.Errors = map[string]string{"password": "Type the password you chose when you started registering."}
			s.render(w, r, http.StatusUnprocessableEntity, pages.SignupConfirm(view))
			return
		}
		if proof, ok = s.claims.ProveWithPassword(c, password); !ok {
			// R2: whoever clicked is not whoever chose the password -- nothing is created.
			view.Errors = map[string]string{"password": "That is not the password chosen for this registration."}
			s.render(w, r, http.StatusUnprocessableEntity, pages.SignupConfirm(view))
			return
		}
	}

	res, err := s.provisioner.Provision(r.Context(), proof)
	switch {
	case errors.Is(err, signup.ErrVATTaken):
		// The one failure the visitor can act on (signup.ErrVATTaken says why a VAT number may be named):
		// another draft with this number was confirmed first. The draft stays unspent and dies with its TTL.
		s.log.Info("signup refused: the VAT number is already registered", "ip", ip)
		s.clearConfirmCookie(w)
		s.problem(w, r, http.StatusConflict, pages.SignupProblemView{
			Title:   "That VAT number is already registered",
			Message: "A business with this VAT number was registered with Taptime while this link was waiting. Nothing new was created.",
			Hint:    "If it is your business, sign in instead of registering again.",
			Retry:   signupPath,
		})
		return
	case errors.Is(err, signup.ErrClaimRefused):
		s.log.Info("signup: the registration link was refused at confirmation", "claim_id", c.ID, "ip", ip)
		// The cookie notice's promise: cleared as soon as the link is refused. The person opens the
		// link from the e-mail again (the page says so).
		s.clearConfirmCookie(w)
		s.problem(w, r, http.StatusConflict, problemSignupClaimRefused)
		return
	case err != nil:
		s.log.Error("signup: provisioning failed", "claim_id", c.ID, "err", err)
		s.problem(w, r, http.StatusInternalServerError, problemSignupServer)
		return
	}
	s.clearConfirmCookie(w)
	s.render(w, r, http.StatusOK, pages.SignupDone(pages.SignupDoneView{
		BusinessName: res.BusinessName,
		Venues:       res.VenueNames,
		VATNumber:    res.VATNumber,
		VATVerified:  res.VAT == signup.VATValid,
		VATRefused:   res.VAT == signup.VATInvalid,
		KnownAddress: c.KnownAddress,
		SignInHref:   adminLoginPath,
	}))
}

// problemSignupClaimPaused is the identity-password branch's refusal past the link's gate or the identity's
// failure budget (M14-17b): the same page for the right password and a wrong one, so it says nothing about
// the password.
var problemSignupClaimPaused = pages.SignupProblemView{
	Title:   "Too many attempts",
	Message: "This registration link has been tried too many times, so it is paused for now.",
	Hint:    "Wait a few minutes, then open the link from your email again.",
}

// proveIdentityPassword is the identity-password branch (M14-17b): the link's gate, the password, and
// the proof -- or the answer. It reports whether the caller may go on with the proof.
//
// 🔴 TWO BRAKES, BOTH RESERVED BEFORE ANYTHING IS COMPARED (the 2026-10-27 audits; the user's decisions
// "İkisini birden" and "Ret sayacını ayır"): the LINK's gate (claimLinkLimiter, signupClaimLinkLimit,
// keyed by the draft's id) and the IDENTITY's counter of this branch (claimIdentityLimiter,
// signupClaimIdentityLimit, charged and traced by adminauth -- its own key, never the sign-in's, so a
// sign-in failure never pauses this branch). Each is ONE locked step (TryCharge): with Allowed-then-
// Charge, K simultaneous guesses all read "allowed" during the 250 ms comparison (measured by both
// audits of 0b3d018a: 40 guesses, 40 comparisons, against a limit of 10). Past either, the answer is
// problemSignupClaimPaused whatever was typed.
func (s *Signup) proveIdentityPassword(w http.ResponseWriter, r *http.Request, c adminauth.SignupClaim, view pages.SignupConfirmView) (adminauth.ClaimProof, bool) {
	if within, n := s.claimLinkLimiter.TryCharge(c.ID.String()); !within {
		if s.claimLinkLimiter.FirstOverLimit(n) {
			s.log.Warn("signup rate limited", "scope", "link", "claim_id", c.ID,
				"limit", signupClaimLinkLimit, "period", signupClaimLinkPeriod.String())
		}
		s.problem(w, r, http.StatusTooManyRequests, problemSignupClaimPaused)
		return adminauth.ClaimProof{}, false
	}
	password := r.PostFormValue("password")
	if password == "" || len(password) > signup.MaxPasswordBytes {
		view.Errors = map[string]string{"password": "Type the password of your Taptime sign-in."}
		s.render(w, r, http.StatusUnprocessableEntity, pages.SignupConfirm(view))
		return adminauth.ClaimProof{}, false
	}
	if s.claimIdentityLimiter == nil {
		// Unreachable from NewSignup; refused rather than run uncounted.
		s.log.Error("signup: the sign-in-less branch has no failure budget", "claim_id", c.ID)
		s.problem(w, r, http.StatusInternalServerError, problemSignupServer)
		return adminauth.ClaimProof{}, false
	}
	proof, err := s.claims.ProveWithIdentityPassword(r.Context(), c, password, s.claimIdentityLimiter)
	switch {
	case err == nil:
		return proof, true
	case errors.Is(err, adminauth.ErrClaimBudgetSpent):
		s.log.Info("signup refused: the sign-in-less branch's budget is spent", "claim_id", c.ID)
		s.problem(w, r, http.StatusTooManyRequests, problemSignupClaimPaused)
	case errors.Is(err, adminauth.ErrBadCredentials):
		view.Errors = map[string]string{"password": "That is not the password of this Taptime sign-in."}
		s.render(w, r, http.StatusUnprocessableEntity, pages.SignupConfirm(view))
	default:
		s.log.Error("signup: the sign-in-less proof failed", "claim_id", c.ID, "err", err)
		s.problem(w, r, http.StatusInternalServerError, problemSignupServer)
	}
	return adminauth.ClaimProof{}, false
}

// liveClaim resolves the cookie's token to a draft that can still be confirmed, or answers.
func (s *Signup) liveClaim(w http.ResponseWriter, r *http.Request, tok string) (adminauth.SignupClaim, bool) {
	c, err := s.claims.Resolve(r.Context(), adminauth.ParseSignupClaimToken(tok))
	switch {
	case errors.Is(err, adminauth.ErrNoSuchClaim):
		s.clearConfirmCookie(w)
		s.problem(w, r, http.StatusNotFound, problemSignupLinkDead)
		return adminauth.SignupClaim{}, false
	case err != nil:
		s.log.Error("signup: resolving a registration link failed", "err", err)
		s.problem(w, r, http.StatusInternalServerError, problemSignupServer)
		return adminauth.SignupClaim{}, false
	case !c.Live(s.claims.Now()):
		s.clearConfirmCookie(w)
		s.problem(w, r, http.StatusGone, problemSignupLinkDead)
		return adminauth.SignupClaim{}, false
	}
	return c, true
}

// confirmView builds the page from the stored draft.
func (s *Signup) confirmView(csrf string, c adminauth.SignupClaim) (pages.SignupConfirmView, error) {
	d, err := signup.DecodeDraft(c.Draft)
	if err != nil {
		return pages.SignupConfirmView{}, err
	}
	venues := make([]string, 0, len(d.Venues))
	for _, v := range d.Venues {
		venues = append(venues, v.Name)
	}
	return pages.SignupConfirmView{CSRFToken: csrf, BusinessName: d.Business.Name, Venues: venues,
		KnownAddress: c.KnownAddress, IdentityPassword: c.IdentityPassword, SignInHref: adminLoginPath}, nil
}

func (s *Signup) setConfirmCookie(w http.ResponseWriter, v string) {
	http.SetCookie(w, &http.Cookie{
		Name:     signupConfirmCookieName,
		Value:    v,
		Path:     signupConfirmPath,
		MaxAge:   signupConfirmCookieMaxAge,
		HttpOnly: true,
		Secure:   s.cookies.secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Signup) clearConfirmCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     signupConfirmCookieName,
		Value:    "",
		Path:     signupConfirmPath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cookies.secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

// readConfirmCookie returns the csrf value and the token, or ok=false.
func (s *Signup) readConfirmCookie(r *http.Request) (csrf, token string, ok bool) {
	ck, err := r.Cookie(signupConfirmCookieName)
	if err != nil || ck.Value == "" {
		return "", "", false
	}
	csrf, token, found := strings.Cut(ck.Value, ".")
	if !found || csrf == "" || token == "" {
		return "", "", false
	}
	return csrf, token, true
}
