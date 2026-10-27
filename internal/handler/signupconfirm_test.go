package handler

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/web/templates/pages"
)

// M14-17 (ADR 0031 §5.10): the verified registration at the HTTP boundary. The claims are the in-memory
// fake (signupclaimfake_test.go); adminauth and internal/domain/signup hold the real ones on Postgres.

// recordingMail is a SignupMailSender that keeps what it was handed.
type recordingMail struct {
	mu   sync.Mutex
	sent []recordedMail
}

type recordedMail struct {
	scope string
	m     mail.Message
}

func (r *recordingMail) Send(_ context.Context, scope string, m mail.Message) (mail.Receipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, recordedMail{scope, m})
	return mail.Receipt{MessageID: "<id@relay>"}, nil
}

func (r *recordingMail) all() []recordedMail {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.sent)
}

// emailSignupConfig is an installation that e-mails its links.
func emailSignupConfig() *config.Config {
	c := signupTestConfig()
	c.ResetDelivery = config.ResetDeliveryEmail
	return c
}

// newEmailSignupDriver is newSignupDriver with the letters going to a recording sender (the outbox and
// its worker are real) and the log captured.
func newEmailSignupDriver(t *testing.T, p *fakeProvisioner, floor time.Duration) (*wizardDriver, *Signup, *recordingMail, *lockedWriter) {
	t.Helper()
	claims := newFakeSignupClaims()
	p.claims, claims.onOpen = claims, p.opened
	rec := &recordingMail{}
	logged := &lockedWriter{w: &bytes.Buffer{}, mu: &sync.Mutex{}}
	log := slog.New(slog.NewJSONHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	wizard, err := NewSignup(p, claims, nil, rec, emailSignupConfig(), log)
	if err != nil {
		t.Fatalf("NewSignup: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := wizard.Drain(ctx); err != nil {
			t.Errorf("drain: %v", err)
		}
	})
	now := time.Now()
	wizard.state.now = func() time.Time { return now }
	wizard.floor = floor
	d := &wizardDriver{t: t, router: httpx.NewRouter(nil, nil, wizard), jar: map[string]string{}, clock: &now, claims: claims}
	return d, wizard, rec, logged
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// String reads what was written so far, under the writer's lock (the outbox's worker logs too).
func (l *lockedWriter) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.String()
}

// drainLetters waits for the outbox to hand n letters to the sender.
func drainLetters(t *testing.T, rec *recordingMail, n int) []recordedMail {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for len(rec.all()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d letter(s) reached the sender, want %d", len(rec.all()), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	return rec.all()
}

var letterLinkRE = regexp.MustCompile(regexp.QuoteMeta(testBaseURL+signupConfirmPath) + `\?t=[A-Za-z0-9_-]+`)

// TestSignupAccount_ANewAndAKnownAddressAreIndistinguishable is R18 at the boundary, MEASURED: three
// registrations -- an address that has a sign-in, one whose sign-in has no active business left (M14-17b),
// and one that has none -- with the REAL floor: the same status, the same Location, the same header
// names, the same "Check your inbox" bytes, the same log lines (keys and messages), and every answer inside
// [floor, floor + 50 ms] over alternating samples. The only difference is in the MAILBOX, and only
// between "has a sign-in" and "has none": the two sign-ins get the SAME letter (subject, scope,
// recipient), through scope "signup" -- the letter is not chosen by the businesses.
func TestSignupAccount_ANewAndAKnownAddressAreIndistinguishable(t *testing.T) {
	const samples = 5
	type arm struct {
		durations []time.Duration
		status    int
		location  string
		headers   []string
		sentPage  string
		lines     []string
		letters   []string
	}
	kinds := []string{"new", "known", "sign-in-less"}
	arms := map[string]*arm{"new": {}, "known": {}, "sign-in-less": {}}
	logLine := regexp.MustCompile(`"msg":"([^"]*)"`)
	for i := 0; i < len(kinds)*samples; i++ {
		kind := kinds[i%len(kinds)]
		p := &fakeProvisioner{}
		d, _, rec, logged := newEmailSignupDriver(t, p, signupClaimFloor)
		form := accountForm("")
		if kind != "new" {
			d.claims.known[strings.ToLower(form.Get("email"))] = true
		}
		if kind == "sign-in-less" {
			d.claims.signInless[strings.ToLower(form.Get("email"))] = "the sign-in's password"
		}
		csrf := d.walkToAccount(t)
		d.advance(time.Minute)
		form.Set("csrf", csrf)
		start := time.Now()
		res := d.post(signupAccountPath, form)
		took := time.Since(start)
		a := arms[kind]
		a.durations = append(a.durations, took)
		a.status, a.location = res.Code, res.Header().Get("Location")
		var names []string
		for k := range res.Header() {
			names = append(names, k)
		}
		sort.Strings(names)
		a.headers = names
		a.sentPage = d.get(signupSentPath).Body.String()
		letters := drainLetters(t, rec, 1)
		a.letters = append(a.letters, letters[0].scope+"|"+letters[0].m.Subject+"|"+letters[0].m.To)
		// The worker logs its accepted line after the send returns: wait for it, so both arms compare
		// complete logs.
		for deadline := time.Now().Add(5 * time.Second); !strings.Contains(logged.String(), "the relay accepted the registration letter"); {
			if time.Now().After(deadline) {
				t.Fatal("the worker never logged the accepted letter")
			}
			time.Sleep(5 * time.Millisecond)
		}
		var lines []string
		for _, m := range logLine.FindAllStringSubmatch(logged.String(), -1) {
			lines = append(lines, m[1])
		}
		a.lines = lines
	}
	n, k, l := arms["new"], arms["known"], arms["sign-in-less"]
	if n.status != http.StatusSeeOther || n.location != signupSentPath {
		t.Fatalf("status/location: new %d %q", n.status, n.location)
	}
	for name, x := range map[string]*arm{"known": k, "sign-in-less": l} {
		if x.status != n.status || x.location != n.location {
			t.Errorf("status/location: new %d %q, %s %d %q", n.status, n.location, name, x.status, x.location)
		}
		if !slices.Equal(n.headers, x.headers) {
			t.Errorf("header names differ: new %v, %s %v", n.headers, name, x.headers)
		}
		if n.sentPage != x.sentPage {
			t.Errorf("\"Check your inbox\" differs between a new address and a %s one", name)
		}
		if !slices.Equal(n.lines, x.lines) {
			t.Errorf("the log lines differ: new %v, %s %v", n.lines, name, x.lines)
		}
	}
	median := func(ds []time.Duration) time.Duration {
		s := slices.Clone(ds)
		slices.Sort(s)
		return s[len(s)/2]
	}
	for name, a := range arms {
		m := median(a.durations)
		t.Logf("%s address: median %v over %v", name, m, a.durations)
		if m < signupClaimFloor || m > signupClaimFloor+50*time.Millisecond {
			t.Errorf("%s address: median %v outside [floor, floor+50ms]", name, m)
		}
	}
	// The mailbox is where the difference is, and only there.
	if !strings.HasPrefix(n.letters[0], signupClaimScope+"|Confirm your Taptime registration|") ||
		!strings.HasPrefix(k.letters[0], signupClaimScope+"|A Taptime registration used your address|") {
		t.Errorf("letters: new %q, known %q", n.letters[0], k.letters[0])
	}
	if !slices.Equal(k.letters, l.letters) {
		t.Errorf("the two sign-ins got different letters: %q / %q", k.letters, l.letters)
	}
}

// TestSignupAccount_NothingIsCreatedBeforeTheClick (R18, K-9): the wizard's last POST opens a draft and
// provisions nothing; the letter carries the link, "Check your inbox" does not (an e-mailing
// installation), and neither the address nor the token reaches the log.
func TestSignupAccount_NothingIsCreatedBeforeTheClick(t *testing.T) {
	p := &fakeProvisioner{}
	d, _, rec, logged := newEmailSignupDriver(t, p, 0)
	csrf := d.walkToAccount(t)
	d.advance(time.Minute)
	if res := d.post(signupAccountPath, accountForm(csrf)); res.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d", signupAccountPath, res.Code)
	}
	if len(p.calls) != 1 || len(p.provisioned) != 0 {
		t.Fatalf("%d draft(s) opened and %d business(es) provisioned; want 1 and 0", len(p.calls), len(p.provisioned))
	}
	letters := drainLetters(t, rec, 1)
	link := letterLinkRE.FindString(letters[0].m.Text)
	if link == "" || letters[0].m.To != accountForm("").Get("email") || letters[0].scope != signupClaimScope {
		t.Fatalf("the letter: to %q, scope %q, link found %v", letters[0].m.To, letters[0].scope, link != "")
	}
	sent := d.get(signupSentPath).Body.String()
	if strings.Contains(sent, signupConfirmPath) || !strings.Contains(screenText(t, sent), "We emailed a link") {
		t.Error("an e-mailing installation's \"Check your inbox\" shows the link, or does not say it was emailed")
	}
	token := link[strings.Index(link, "?t=")+3:]
	for _, secret := range []string{accountForm("").Get("email"), token, accountForm("").Get("password")} {
		if strings.Contains(strings.ToLower(logged.String()), strings.ToLower(secret)) {
			t.Errorf("the log carries %q", secret[:4]+"...")
		}
	}
}

// confirmWalk drives the wizard to "Check your inbox" on the dev installation (link on screen) and
// returns the link's path with its token.
func confirmWalk(t *testing.T, d *wizardDriver) string {
	t.Helper()
	csrf := d.walkToAccount(t)
	d.advance(time.Minute)
	if res := d.post(signupAccountPath, accountForm(csrf)); res.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d", signupAccountPath, res.Code)
	}
	sent := d.get(signupSentPath)
	m := screenLinkRE.FindStringSubmatch(sent.Body.String())
	if m == nil {
		t.Fatal("no link on the dev installation's page")
	}
	u, err := url.Parse(m[1])
	if err != nil {
		t.Fatal(err)
	}
	return u.RequestURI()
}

// TestSignupConfirm_TheGETSpendsNothing (R19): the link opened twice, the page loaded three times --
// the draft is unspent and nothing is provisioned; then the POST creates the business once.
func TestSignupConfirm_TheGETSpendsNothing(t *testing.T) {
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	link := confirmWalk(t, d)
	for i := 0; i < 2; i++ {
		if res := d.get(link); res.Code != http.StatusSeeOther || res.Header().Get("Location") != signupConfirmPath {
			t.Fatalf("GET the link = %d -> %q", res.Code, res.Header().Get("Location"))
		}
		for j := 0; j < 3; j++ {
			if res := d.get(signupConfirmPath); res.Code != http.StatusOK {
				t.Fatalf("GET %s = %d", signupConfirmPath, res.Code)
			}
		}
	}
	for _, c := range d.claims.claims {
		if c.used {
			t.Fatal("a GET spent the draft")
		}
	}
	if len(p.provisioned) != 0 {
		t.Fatal("a GET provisioned a business")
	}
	page := d.get(signupConfirmPath)
	if res := d.post(signupConfirmPath, url.Values{"csrf": {d.token(page)}, "password": {accountForm("").Get("password")}}); res.Code != http.StatusOK ||
		!strings.Contains(res.Body.String(), "stamp--approved") {
		t.Fatalf("the POST = %d, want the done page", res.Code)
	}
	if len(p.provisioned) != 1 {
		t.Fatalf("%d business(es) provisioned, want 1", len(p.provisioned))
	}
}

// TestSignupConfirm_AWrongPasswordCreatesNothing (R2): the new address's page asks for the password the
// form chose; another one -- the clicker is not the chooser -- re-renders the form (422, no password
// echoed) and provisions nothing; the right one creates the business.
func TestSignupConfirm_AWrongPasswordCreatesNothing(t *testing.T) {
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	link := confirmWalk(t, d)
	d.get(link)
	page := d.get(signupConfirmPath)
	if !strings.Contains(page.Body.String(), `name="password"`) || strings.Contains(page.Body.String(), "Add the business") {
		t.Fatal("a new address's page does not ask for the password")
	}
	csrf := d.token(page)
	for _, wrong := range []string{"", "an attacker's own password", strings.Repeat("x", signup.MaxPasswordBytes+1)} {
		res := d.post(signupConfirmPath, url.Values{"csrf": {csrf}, "password": {wrong}})
		if res.Code != http.StatusUnprocessableEntity {
			t.Errorf("password %q...: %d, want 422", firstN(wrong, 6), res.Code)
		}
		if wrong != "" && strings.Contains(res.Body.String(), wrong) {
			t.Error("the refused password was echoed")
		}
	}
	if len(p.provisioned) != 0 {
		t.Fatalf("a wrong password provisioned %d business(es)", len(p.provisioned))
	}
	if res := d.post(signupConfirmPath, url.Values{"csrf": {csrf}, "password": {accountForm("").Get("password")}}); res.Code != http.StatusOK {
		t.Fatalf("the right password = %d", res.Code)
	}
	if len(p.provisioned) != 1 {
		t.Fatalf("%d business(es) after the right password, want 1", len(p.provisioned))
	}
}

func firstN(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// TestSignupConfirm_AKnownAddressNeedsItsOwnSignInInThisBrowser (R18): an address that has a sign-in gets
// the "sign in first" page and no password field; pressed with no panel session in the browser -> 403,
// the page says to sign in, nothing provisioned and no proof asked; with the panel cookie -> the session
// proof is asked and the business created (identity_claim_signup decides on Postgres whether the session
// is the address's person -- internal/db's TestIdentityClaimSignup_AnExistingIdentityClaimsOnlyWithItsOwnSession).
func TestSignupConfirm_AKnownAddressNeedsItsOwnSignInInThisBrowser(t *testing.T) {
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	d.claims.known[strings.ToLower(accountForm("").Get("email"))] = true
	link := confirmWalk(t, d)
	d.get(link)
	page := d.get(signupConfirmPath)
	if strings.Contains(page.Body.String(), `name="password"`) || !strings.Contains(page.Body.String(), "Add the business") {
		t.Fatal("a known address's page is not the sign-in-first form")
	}
	csrf := d.token(page)
	res := d.post(signupConfirmPath, url.Values{"csrf": {csrf}})
	if res.Code != http.StatusForbidden || !strings.Contains(screenText(t, res.Body.String()), "Sign in with this address first") {
		t.Fatalf("pressed without a session: %d", res.Code)
	}
	if len(p.provisioned) != 0 || d.claims.sessionProofs != 0 {
		t.Fatal("pressed without a session, something was proved or provisioned")
	}
	d.jar[adminauth.CookieName] = strings.Repeat("A", 43)
	res = d.post(signupConfirmPath, url.Values{"csrf": {csrf}})
	if res.Code != http.StatusOK || d.claims.sessionProofs != 1 || len(p.provisioned) != 1 {
		t.Fatalf("pressed signed in: %d, %d proof(s), %d business(es)", res.Code, d.claims.sessionProofs, len(p.provisioned))
	}
	if !strings.Contains(screenText(t, res.Body.String()), "one of the businesses of your sign-in") {
		t.Error("the known address's done page does not say the business joined the sign-in")
	}
}

// TestSignupConfirm_ASignInlessAddressConfirmsWithItsPassword (M14-17b, the user's decision of
// 2026-10-27): an address whose sign-in has no active business left cannot sign in, so its page asks for
// THAT sign-in's password (a password field, no "sign in first" form); an empty or wrong one re-renders
// the form (422, no password echoed) and creates nothing, no session proof is ever asked; the right one
// creates the business and the done page says it joined the sign-in. identity_claim_signup re-checks the
// digest and the missing membership on Postgres (internal/db's
// TestIdentityClaimSignup00080_EveryGuardRefusesAlone).
func TestSignupConfirm_ASignInlessAddressConfirmsWithItsPassword(t *testing.T) {
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	email := strings.ToLower(accountForm("").Get("email"))
	d.claims.known[email] = true
	d.claims.signInless[email] = "the sign-in's own password"
	link := confirmWalk(t, d)
	d.get(link)
	page := d.get(signupConfirmPath)
	body := screenText(t, page.Body.String())
	if !strings.Contains(page.Body.String(), `name="password"`) || !strings.Contains(body, "Your Taptime password") ||
		strings.Contains(body, "sign in with your usual password in this browser") {
		t.Fatal("a sign-in-less address's page does not ask for the sign-in's password")
	}
	csrf := d.token(page)
	for _, wrong := range []string{"", accountForm("").Get("password"), "an attacker's guess", strings.Repeat("x", signup.MaxPasswordBytes+1)} {
		res := d.post(signupConfirmPath, url.Values{"csrf": {csrf}, "password": {wrong}})
		if res.Code != http.StatusUnprocessableEntity {
			t.Errorf("password %q...: %d, want 422", firstN(wrong, 6), res.Code)
		}
		if wrong != "" && strings.Contains(res.Body.String(), wrong) {
			t.Error("the refused password was echoed")
		}
	}
	if len(p.provisioned) != 0 || d.claims.sessionProofs != 0 {
		t.Fatalf("a wrong password provisioned %d business(es), %d session proof(s)", len(p.provisioned), d.claims.sessionProofs)
	}
	res := d.post(signupConfirmPath, url.Values{"csrf": {csrf}, "password": {"the sign-in's own password"}})
	if res.Code != http.StatusOK || len(p.provisioned) != 1 || d.claims.sessionProofs != 0 {
		t.Fatalf("the right password: %d, %d business(es), %d session proof(s)", res.Code, len(p.provisioned), d.claims.sessionProofs)
	}
	if !strings.Contains(screenText(t, res.Body.String()), "one of the businesses of your sign-in") {
		t.Error("the done page does not say the business joined the sign-in")
	}
}

// TestSignupConfirm_ALinkPastItsGateRefusesEvenTheRightPassword (M14-17b; the 2026-10-27 security
// audit's ORTA finding, the user's decision "İkisini birden"): a sign-in-less address's link answers
// signupClaimLinkLimit wrong passwords with the ordinary 422; every later attempt on THAT link -- the right
// password included -- gets the same "too many attempts" page (429), reaches no proof and creates nothing
// (ratelimit.go's inviteFailureLimit gate, activate.go's overInviteBudget). The gate is per link: a NEW link
// for the same address is answered normally. (The identity's budget, shared with the sign-in, is
// internal/adminauth's TestSignupClaims_WrongIdentityPasswordsSpendTheIdentitysBudgetAcrossLinks; the
// claims fake here never spends it.)
//
// MUTATION (measured): the wrong-password charge removed -> the right password after ten wrong ones
// creates the business, red.
func TestSignupConfirm_ALinkPastItsGateRefusesEvenTheRightPassword(t *testing.T) {
	p := &fakeProvisioner{}
	d, wizard := newSignupDriver(t, p, nil)
	email := strings.ToLower(accountForm("").Get("email"))
	d.claims.known[email] = true
	d.claims.signInless[email] = "the sign-in's own password"
	link := confirmWalk(t, d)
	d.get(link)
	page := d.get(signupConfirmPath)
	csrf := d.token(page)
	// The per-address gate is not this test's: lift it so only the link's gate can refuse.
	wizard.confirmLimiter = newLimiter(1000, signupConfirmPeriod)
	for i := 0; i < signupClaimLinkLimit; i++ {
		if res := d.post(signupConfirmPath, url.Values{"csrf": {csrf}, "password": {"guess " + strconv.Itoa(i)}}); res.Code != http.StatusUnprocessableEntity {
			t.Fatalf("wrong password %d: %d, want 422", i+1, res.Code)
		}
	}
	proofs := d.claims.identityProofs
	res := d.post(signupConfirmPath, url.Values{"csrf": {csrf}, "password": {"the sign-in's own password"}})
	if res.Code != http.StatusTooManyRequests || !strings.Contains(screenText(t, res.Body.String()), "tried too many times") {
		t.Fatalf("the right password past the link's gate: %d, want 429 and the paused page", res.Code)
	}
	if len(p.provisioned) != 0 || d.claims.identityProofs != proofs {
		t.Fatalf("past the gate: %d business(es), %d more proof(s)", len(p.provisioned), d.claims.identityProofs-proofs)
	}
	// A NEW link for the same address: its own gate.
	link2 := confirmWalk(t, d)
	d.get(link2)
	page2 := d.get(signupConfirmPath)
	if res := d.post(signupConfirmPath, url.Values{"csrf": {d.token(page2)}, "password": {"the sign-in's own password"}}); res.Code != http.StatusOK || len(p.provisioned) != 1 {
		t.Fatalf("a new link's right password: %d, %d business(es)", res.Code, len(p.provisioned))
	}
}

// TestSignupConfirm_ASpentIdentityBudgetPausesTheLink: while the identity's failure budget is spent
// (adminauth.ErrClaimBudgetSpent -- the sign-in's budget, shared) the branch answers the same paused page
// as the link's gate, right password or wrong, and creates nothing. Without a budget wired the branch
// refuses with a 500 and asks no proof -- never runs unbudgeted.
func TestSignupConfirm_ASpentIdentityBudgetPausesTheLink(t *testing.T) {
	p := &fakeProvisioner{}
	d, wizard := newSignupDriver(t, p, nil)
	email := strings.ToLower(accountForm("").Get("email"))
	d.claims.known[email] = true
	d.claims.signInless[email] = "the sign-in's own password"
	link := confirmWalk(t, d)
	d.get(link)
	csrf := d.token(d.get(signupConfirmPath))
	d.claims.budgetSpent = true
	for _, pw := range []string{"the sign-in's own password", "a guess"} {
		res := d.post(signupConfirmPath, url.Values{"csrf": {csrf}, "password": {pw}})
		if res.Code != http.StatusTooManyRequests || !strings.Contains(screenText(t, res.Body.String()), "tried too many times") {
			t.Fatalf("%q with the budget spent: %d, want 429 and the paused page", pw, res.Code)
		}
	}
	if len(p.provisioned) != 0 {
		t.Fatal("a spent budget created a business")
	}
	d.claims.budgetSpent = false
	wizard.identityBudget = nil
	proofs := d.claims.identityProofs
	if res := d.post(signupConfirmPath, url.Values{"csrf": {csrf}, "password": {"the sign-in's own password"}}); res.Code != http.StatusInternalServerError ||
		d.claims.identityProofs != proofs || len(p.provisioned) != 0 {
		t.Fatalf("no budget wired: %d, %d proof(s), %d business(es); want 500 and nothing", res.Code, d.claims.identityProofs-proofs, len(p.provisioned))
	}
}

// TestSignupConfirm_ADeadLinkCreatesNothing: an expired draft (410), an unknown token (404), a spent one
// (a second confirmation) and a link value no token can be (400) each answer the "no longer works" page
// and provision nothing more.
func TestSignupConfirm_ADeadLinkCreatesNothing(t *testing.T) {
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	link := confirmWalk(t, d)
	d.get(link)
	page := d.get(signupConfirmPath)
	form := url.Values{"csrf": {d.token(page)}, "password": {accountForm("").Get("password")}}

	d.claims.mu.Lock()
	d.claims.now = d.claims.now.Add(adminauth.SignupClaimTTL + time.Minute)
	d.claims.mu.Unlock()
	if res := d.post(signupConfirmPath, form); res.Code != http.StatusGone {
		t.Errorf("an expired draft = %d, want 410", res.Code)
	}
	d.claims.mu.Lock()
	d.claims.now = time.Now()
	d.claims.mu.Unlock()

	d2, _ := newSignupDriver(t, &fakeProvisioner{}, nil)
	d2.get(signupConfirmPath + "?t=" + strings.Repeat("Z", 43))
	if res := d2.get(signupConfirmPath); res.Code != http.StatusNotFound {
		t.Errorf("an unknown token = %d, want 404", res.Code)
	}
	if res := d2.get(signupConfirmPath + "?t=a.b"); res.Code != http.StatusBadRequest {
		t.Errorf("a value no token can be = %d, want 400", res.Code)
	}

	d.get(link)
	page = d.get(signupConfirmPath)
	form.Set("csrf", d.token(page))
	if res := d.post(signupConfirmPath, form); res.Code != http.StatusOK {
		t.Fatalf("the live draft = %d", res.Code)
	}
	d.get(link)
	if res := d.get(signupConfirmPath); res.Code != http.StatusGone {
		t.Errorf("a spent draft's page = %d, want 410", res.Code)
	}
	if len(p.provisioned) != 1 {
		t.Fatalf("%d business(es), want the one", len(p.provisioned))
	}
}

// TestSignupConfirm_ATakenVATNumberIsSaidAtTheConfirmation (ADR 0031 §5.10): a draft reserves nothing, so
// a VAT number registered while the link waited is today's conflict sentence -- at the confirmation.
func TestSignupConfirm_ATakenVATNumberIsSaidAtTheConfirmation(t *testing.T) {
	p := &fakeProvisioner{err: signup.ErrVATTaken}
	d, _ := newSignupDriver(t, p, nil)
	res := func() *httptest.ResponseRecorder {
		confirmWalk(t, d)
		return d.finishFromLink(t)
	}()
	if res.Code != http.StatusConflict || !strings.Contains(screenText(t, res.Body.String()), "already registered") {
		t.Fatalf("a taken VAT number at the confirmation = %d", res.Code)
	}
}

// finishFromLink is finish for a driver whose sent page was already read: it re-reads the link from the
// claims fake.
func (d *wizardDriver) finishFromLink(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	var raw string
	for k := range d.claims.claims {
		raw = k
	}
	d.get(signupConfirmPath + "?t=" + raw)
	page := d.get(signupConfirmPath)
	return d.post(signupConfirmPath, url.Values{"csrf": {d.token(page)}, "password": {accountForm("").Get("password")}})
}

// TestSignup_TheDeliveryModeIsTheInstallations (the user's decision of 2026-10-26): with
// TAPPA_RESET_DELIVERY=email the letter is sent and never shown; with none on a dev or sandbox
// installation the link is shown on "Check your inbox"; with none anywhere else the wizard says it takes
// no registrations (503, every step); a mailer without e-mail, or e-mail without a mailer, refuses to boot.
func TestSignup_TheDeliveryModeIsTheInstallations(t *testing.T) {
	for _, c := range []struct {
		env, delivery string
		mailer        bool
		want          signupDelivery
		bootErr       bool
	}{
		{config.EnvDev, config.ResetDeliveryNone, false, signupOnScreen, false},
		{config.EnvSandbox, config.ResetDeliveryNone, false, signupOnScreen, false},
		{config.EnvStaging, config.ResetDeliveryNone, false, signupClosed, false},
		{config.EnvProd, config.ResetDeliveryNone, false, signupClosed, false},
		{config.EnvProd, config.ResetDeliveryEmail, true, signupByEmail, false},
		{config.EnvDev, config.ResetDeliveryEmail, true, signupByEmail, false},
		{config.EnvProd, config.ResetDeliveryEmail, false, 0, true},
		{config.EnvDev, config.ResetDeliveryNone, true, 0, true},
	} {
		cfg := signupTestConfig()
		cfg.Env, cfg.ResetDelivery = c.env, c.delivery
		var mailer SignupMailSender
		if c.mailer {
			mailer = &recordingMail{}
		}
		w, err := NewSignup(&fakeProvisioner{}, newFakeSignupClaims(), nil, mailer, cfg, slog.New(slog.DiscardHandler))
		if c.bootErr {
			if err == nil {
				t.Errorf("%s/%s/mailer=%v booted", c.env, c.delivery, c.mailer)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s/%s: %v", c.env, c.delivery, err)
		}
		if w.delivery != c.want {
			t.Errorf("%s/%s/mailer=%v: delivery %d, want %d", c.env, c.delivery, c.mailer, w.delivery, c.want)
		}
		if c.want == signupClosed {
			r := httpx.NewRouter(nil, nil, w)
			for _, path := range []string{signupPath, signupPlacesPath, signupAccountPath, signupSentPath, signupConfirmPath} {
				rec := httptest.NewRecorder()
				r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
				if rec.Code != http.StatusServiceUnavailable || !strings.Contains(screenText(t, rec.Body.String()), "cannot take new registrations") {
					t.Errorf("%s/%s: GET %s = %d, want the closed page", c.env, c.delivery, path, rec.Code)
				}
			}
		}
		if w.outbox != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			if err := w.Drain(ctx); err != nil {
				t.Error(err)
			}
			cancel()
		}
	}
}

// TestSignupConfirm_TheVerifiedRegistrationViewsNameNoAddress: none of the M14-17 screens carries the
// address the link went to (R18 also holds for a shoulder-surfed screen).
func TestSignupConfirm_TheVerifiedRegistrationViewsNameNoAddress(t *testing.T) {
	p := &fakeProvisioner{}
	d, _ := newSignupDriver(t, p, nil)
	done := d.finishAfterWalk(t)
	for name, body := range map[string]string{"sent": d.lastSent, "confirm": d.lastConfirm, "done": done.Body.String()} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(accountForm("").Get("email"))) {
			t.Errorf("the %s screen names the address", name)
		}
	}
}

// TestSignupConfirmPath_IsThePagesOwn: the link's page, the form's action and the letter's link name one
// path (the e-mail renderer's copy is held by its own boot-time render, NewSignup).
func TestSignupConfirmPath_IsThePagesOwn(t *testing.T) {
	if signupConfirmPath != pages.SignupConfirmPath {
		t.Fatalf("handler %q, pages %q", signupConfirmPath, pages.SignupConfirmPath)
	}
}

func (d *wizardDriver) finishAfterWalk(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	csrf := d.walkToAccount(t)
	d.advance(time.Minute)
	d.post(signupAccountPath, accountForm(csrf))
	return d.finish(t)
}

// TestSignupConfirm_ARefusedClaimClearsTheLinkCookie (the cookie notice: "cleared as soon as the
// registration is finished or the link is refused"): identity_claim_signup's refusal answers the
// refusal page AND expires tappa_signup_claim.
//
// MUTATION (measured): the clear removed from the ErrClaimRefused branch -> red.
func TestSignupConfirm_ARefusedClaimClearsTheLinkCookie(t *testing.T) {
	p := &fakeProvisioner{err: signup.ErrClaimRefused}
	d, _ := newSignupDriver(t, p, nil)
	confirmWalk(t, d)
	res := d.finishFromLink(t)
	if res.Code != http.StatusConflict {
		t.Fatalf("a refused claim = %d, want 409", res.Code)
	}
	cleared := false
	for _, c := range res.Result().Cookies() {
		if c.Name == signupConfirmCookieName && c.MaxAge < 0 && c.Path == signupConfirmPath {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("the refused claim left the registration link's cookie in the browser")
	}
}

var validForRE = regexp.MustCompile(`valid for (\d+ (?:day|days|hour|hours|minute|minutes))\b`)

// TestSignupLink_ThePagesStateTheLettersLifetime (M14-17b's third-eye audit, DÜŞÜK-1): "Check your inbox"
// and the dead-link page state a registration link's life in the words its letter uses -- one rounding
// (email.Lifetime) of the draft's TTL, which is the 24-hour ceiling less the clock-skew margin, so never
// "24 hours".
//
// MUTATION (measured): the sent page's lifetime back to the literal "24 hours" -> red.
func TestSignupLink_ThePagesStateTheLettersLifetime(t *testing.T) {
	p := &fakeProvisioner{}
	d, _, rec, _ := newEmailSignupDriver(t, p, 0)
	csrf := d.walkToAccount(t)
	d.advance(time.Minute)
	if res := d.post(signupAccountPath, accountForm(csrf)); res.Code != http.StatusSeeOther {
		t.Fatalf("POST %s = %d", signupAccountPath, res.Code)
	}
	sent := screenText(t, d.get(signupSentPath).Body.String())
	letter := drainLetters(t, rec, 1)[0].m.Text
	lm, sm := validForRE.FindStringSubmatch(letter), validForRE.FindStringSubmatch(sent)
	if lm == nil || sm == nil {
		t.Fatalf("a lifetime is missing: letter %v, page %v", lm != nil, sm != nil)
	}
	if lm[1] != sm[1] || sm[1] == "24 hours" {
		t.Fatalf("the letter says %q and \"Check your inbox\" %q; want the same, and not the 24-hour ceiling", lm[1], sm[1])
	}
	if !strings.Contains(problemSignupLinkDead.Message, "for "+lm[1]+".") {
		t.Fatalf("the dead-link page says %q; want the letter's %q", problemSignupLinkDead.Message, lm[1])
	}
}
