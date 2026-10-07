package operator

import (
	"errors"
	"net/http"

	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// THE SIGN-IN (ADR 0020 §3): the password step mints a login challenge in its own
// cookie; the TOTP step turns the challenge and a code into a session.

// signInPage is GET /operator/login. It renders the form; it makes no store call and sets
// no cookie.
func (s *Surface) signInPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, operatorpages.SignIn(operatorpages.SignInView{}))
}

// signIn is POST /operator/login: the address and the password go to
// Authenticator.Password; besides a refusal it can return a challenge, which goes into its
// cookie on the way to the TOTP step.
//
// ONE ANSWER FOR THE REFUSED ARMS. An unknown address, a wrong password, a pending account
// and a disabled one are one error (operatorauth.ErrRefused, each having paid one bcrypt
// comparison and written one audit row) and one page here: 401 with
// SignInView{Failed: true}. Measured: the same headers and body bytes and one comparison
// on each of the eight arms of TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike.
// And the same whether the client stayed or half-closed its connection: every answer here
// and in code and enroll is rendered on answered(r) (OP-14 phase E).
// The address is not validated here (no length, UTF-8 or NUL check): internal/db answers an
// address it cannot store as "no operator" (OP-6 md. 8), so such an address takes the
// unknown address's path (dummy bcrypt, unknown_email row, ErrRefused). The checks before
// Password are readForm's: a body over 16 KiB is 413, a body that does not parse is 400.
func (s *Surface) signIn(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r, maxFormBytes, problemFormTooLarge) {
		return
	}
	email, password := postValue(r, "email"), postValue(r, "password")
	c, err := s.auth.Password(r.Context(), rateKey(r), email.reveal(), password.reveal())
	r = answered(r)
	switch {
	case err == nil:
		if err := operatorauth.SetChallengeCookie(w, c); err != nil {
			s.log.ErrorContext(r.Context(), "operator: could not set the sign-in challenge", "err", err)
			s.problem(w, r, http.StatusServiceUnavailable, problemUnavailable)
			return
		}
		s.redirect(w, pathCode)
	case errors.Is(err, operatorauth.ErrRefused):
		s.render(w, r, http.StatusUnauthorized, operatorpages.SignIn(operatorpages.SignInView{Failed: true}))
	case errors.Is(err, operatorauth.ErrThrottled):
		s.problem(w, r, http.StatusTooManyRequests, problemTooMany(false))
	default:
		s.log.ErrorContext(r.Context(), "operator: the first sign-in step failed", "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemUnavailable)
	}
}

// codePage is GET /operator/login/totp. Without a challenge cookie it redirects to the
// password step. The challenge is not verified here (that is TOTP's, with the code).
func (s *Surface) codePage(w http.ResponseWriter, r *http.Request) {
	if _, err := operatorauth.ReadChallengeCookie(r); err != nil {
		s.redirect(w, pathSignIn)
		return
	}
	s.render(w, r, http.StatusOK, operatorpages.Code(operatorpages.CodeView{}))
}

// code is POST /operator/login/totp: the challenge from its cookie and the code from the
// form go to Authenticator.TOTP; the database decides replay, lock and clock in
// op_open_session's statement. On success the session cookie is set and the challenge
// cookie is cleared: the challenge is a bearer value for further code attempts
// (operatorauth's challenge is not single-use) (TestSignIn_TheCodeStepClearsTheChallenge).
func (s *Surface) code(w http.ResponseWriter, r *http.Request) {
	c, err := operatorauth.ReadChallengeCookie(r)
	if err != nil {
		s.redirect(w, pathSignIn)
		return
	}
	if !s.readForm(w, r, maxFormBytes, problemFormTooLarge) {
		return
	}
	code := postValue(r, "code")
	issued, err := s.auth.TOTP(r.Context(), c, code.reveal())
	r = answered(r)
	switch {
	case err == nil:
		if err := operatorauth.SetSessionCookie(w, issued.Token); err != nil {
			s.log.ErrorContext(r.Context(), "operator: could not set the session cookie", "err", err)
			s.problem(w, r, http.StatusServiceUnavailable, problemUnavailable)
			return
		}
		operatorauth.ClearChallengeCookie(w)
		s.redirect(w, pathConsole)
	case errors.Is(err, operatorauth.ErrChallenge), errors.Is(err, operatorauth.ErrRefused):
		// The challenge expired or does not verify, or its account is no longer active:
		// the password step starts over.
		operatorauth.ClearChallengeCookie(w)
		s.render(w, r, http.StatusUnauthorized, operatorpages.SignIn(operatorpages.SignInView{Expired: true}))
	case errors.Is(err, operatorauth.ErrCodeRejected):
		s.render(w, r, http.StatusUnauthorized, operatorpages.Code(operatorpages.CodeView{Rejected: true}))
	case errors.Is(err, operatorauth.ErrLocked):
		s.render(w, r, http.StatusUnauthorized, operatorpages.Code(operatorpages.CodeView{Locked: true}))
	case errors.Is(err, operatorauth.ErrThrottled):
		s.problem(w, r, http.StatusTooManyRequests, problemTooMany(false))
	default:
		s.log.ErrorContext(r.Context(), "operator: the code step failed", "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemUnavailable)
	}
}
