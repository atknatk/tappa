package operator

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// THE ENROLLMENT (ADR 0020 §3; ADR 0021 §1 op_complete_enrollment).
//
// THE LINK'S SHAPE IS DECIDED HERE AND cmd/opadmin (OP-9) PRODUCES IT:
//
//	https://<operator host>/operator/enroll?id=<account id>#<token>
//
// The account id is in the query: it is not a secret (ADR 0020 §6), the GET needs it to
// seal the page's key to that account (BeginEnrollment), and the access record logs the
// route pattern, not the query (requestlog.go) -- the ingress log does see it. The token
// is in the fragment, which a browser does not put in a request (ADR 0020 §6: "token
// sorgu dizgisinde TAŞINMAZ"; the ADR's other option, a path segment, is in the path the
// ingress logs). The page's script moves the fragment into the form and out of the
// address bar; without script the person pastes it (operatorpages.Enroll).
//
// enrollPage reads the query's id; enroll reads r.PostForm. Measured with a token in the
// query: TestEnroll_TheTokenNeverTravelsInTheURL (the token, raw, in the page); the leak
// test's arm A29; and the two header tables, whose 32 classes with a last request to
// /operator/login, /operator/login/totp or /operator/enroll (C1-C26, C38, C40, C44, C45,
// C47, C48) are sent with a query carrying a token, an address, a password, a code and a
// blob (and an id on a POST) -- none of those values is found in those responses in the
// two forms the tables search, raw and query-escaped (an HTML-escaped or base32 echo is
// not searched there). Pinned:
// TestFormValues_TheListedSitesAloneRevealOrReadTheForm catches the list in its header.

// enrollPage is GET /operator/enroll?id=<account>. It shows a fresh key for that account
// and makes no store call: tappa_operator can see neither a pending account nor its token
// (00026), so a well-formed id gets the same kind of page whether it names a pending
// account, an active one or nobody ("aynı yanıt" for pending, ADR 0020 §3). Whether the
// link is genuine is decided at the POST, by op_complete_enrollment.
//
// The key is in the page twice (base32 and the otpauth:// URI); the response is no-store
// and no-referrer, and the Secret is zeroed once the page is rendered -- the rendered
// strings are not wiped (OP-6 md. 18 P6).
func (s *Surface) enrollPage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.URL.Query().Get("id"))
	if err != nil || id == uuid.Nil {
		s.problem(w, r, http.StatusBadRequest, problemEnrollIncomplete)
		return
	}
	p, err := s.auth.BeginEnrollment(id)
	if err != nil {
		s.log.ErrorContext(r.Context(), "operator: could not begin an enrollment", "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemUnavailable)
		return
	}
	defer p.Secret.Zero()
	s.renderEnroll(w, r, http.StatusOK, operatorpages.EnrollView{
		AccountID: id.String(),
		Key:       groupKey(p.Secret.Base32()),
		URI:       p.Secret.URI(totpIssuer, s.host),
		Blob:      p.Blob.RevealForForm(),
	})
}

// totpIssuer is the label an authenticator app shows for the entry; the account part
// is the operator host, so an entry says which deployment it opens.
const totpIssuer = "Taptime operator"

// groupKey prints base32 in groups of four, the way a person reads a key aloud and
// types it. Authenticator apps ignore the spaces.
func groupKey(k string) string {
	var b strings.Builder
	for i := 0; i < len(k); i += 4 {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(k[i:min(i+4, len(k))])
	}
	return b.String()
}

// enroll is POST /operator/enroll: the account id, the link's token, the page's sealed key,
// the new password twice and the first code. Apart from the two passwords' equality,
// CompleteEnrollment decides: the work budget, the password rule, the token's shape, the
// page, the code, the per-address and process-wide enrollment budgets, and then
// op_complete_enrollment's statement, which consumes the token, writes the credentials,
// activates the account and opens its first session.
//
// The two passwords are compared here first: a mismatch is answered before a budget is
// charged or a store method is called (operatorauth answers its password rule the same
// way).
//
// A refusal the person can fix (passwords that differ, a password the rule refuses -- 4th
// round, B7: shorter than 14 characters, longer than 72 bytes or not UTF-8, one notice for
// the three, TestEnroll_TheWeakPasswordNoticeNamesBothLimits -- a code the window does not
// accept) re-renders the form with the account id, sealed key and link token it was
// posted with, so the retry uses the authenticator entry already added. That writes the
// token into the response body of the POST that sent it, on a no-store page (the leak
// test's designed egress D5). A refusal the person cannot fix (ErrEnrollment: a wrong,
// used or expired link, a foreign or expired page) is a problem page without a form.
func (s *Surface) enroll(w http.ResponseWriter, r *http.Request) {
	if !s.readForm(w, r, maxFormBytes, problemFormTooLarge) {
		return
	}
	// An id that does not parse becomes uuid.Nil, which CompleteEnrollment refuses as a
	// malformed link on its own path (its budget and its row), rather than a refusal of
	// this handler's.
	id, err := uuid.Parse(r.PostForm.Get("id"))
	if err != nil {
		id = uuid.Nil
	}
	token, blob := postValue(r, "token"), r.PostForm.Get("blob")
	password, again, code := postValue(r, "password"), postValue(r, "password_again"), postValue(r, "code")

	retry := operatorpages.EnrollView{AccountID: r.PostForm.Get("id"), Blob: blob, Token: token.reveal()}
	if password.reveal() != again.reveal() {
		retry.Mismatch = true
		s.renderEnroll(w, r, http.StatusBadRequest, retry)
		return
	}
	issued, err := s.auth.CompleteEnrollment(r.Context(), rateKey(r), id, token.reveal(),
		operatorauth.PendingBlobFromForm(blob), password.reveal(), code.reveal())
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
	case errors.Is(err, operatorauth.ErrWeakPassword):
		retry.WeakPassword = true
		s.renderEnroll(w, r, http.StatusBadRequest, retry)
	case errors.Is(err, operatorauth.ErrCodeRejected):
		retry.CodeRejected = true
		s.renderEnroll(w, r, http.StatusUnauthorized, retry)
	case errors.Is(err, operatorauth.ErrEnrollment):
		s.problem(w, r, http.StatusBadRequest, problemEnrollRefused)
	case errors.Is(err, operatorauth.ErrThrottled):
		s.problem(w, r, http.StatusTooManyRequests, problemTooMany(false))
	default:
		s.log.ErrorContext(r.Context(), "operator: the enrollment failed", "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemUnavailable)
	}
}
