package operator

import (
	"errors"
	"net/http"

	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// home is GET /operator: the console's front page, behind requireOperator and sessionGate.
// It shows no tenant data -- 00026 has no definer that reads one -- and its links are held
// to the mounted routes by TestOperatorScreens_EveryActionAndLinkIsAMountedRoute.
func (s *Surface) home(w http.ResponseWriter, r *http.Request) {
	if _, ok := operatorOf(r); !ok {
		// Unreachable through mount (sessionGate puts the identity in place); a route
		// mounted outside the chain by mistake answers the sign-in, not a page.
		s.redirect(w, pathSignIn)
		return
	}
	s.render(w, r, http.StatusOK, operatorpages.Home())
}

// logout is POST /operator/logout: op_close_session revokes the session and writes its
// 'logout' row in one statement (ADR 0020 §2), then the cookies are cleared and the person
// lands on the sign-in.
//
// On a database fault the session cookie is kept and the page says the session is still
// open: clearing it would leave a live session this browser can no longer revoke
// (operatorauth.ClearSessionCookie's own warning). Measured:
// TestLogout_IsNotRefusedByTheBudgetAThirdPartyCanSpend's last case. (The sign-out
// ceiling's 429 clears the cookie on purpose: routes.go, mount and logoutGate.)
//
// requireOperator is in front: a request without the cookie gets the sign-in redirect
// there (TestLogout_ACookielessFloodCostsNothingAndCannotRefuseIt).
func (s *Surface) logout(w http.ResponseWriter, r *http.Request) {
	tok, ok := sessionTokenOf(r)
	if !ok {
		// Unreachable through mount (requireOperator is in front); fail closed.
		s.redirect(w, pathSignIn)
		return
	}
	if err := s.auth.Logout(r.Context(), tok); err != nil && !errors.Is(err, operatorauth.ErrNoSession) {
		s.log.ErrorContext(r.Context(), "operator: sign-out failed", "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemSignOutFailed)
		return
	}
	operatorauth.ClearSessionCookie(w)
	operatorauth.ClearChallengeCookie(w)
	s.redirect(w, pathSignIn)
}
