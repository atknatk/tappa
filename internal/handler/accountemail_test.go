package handler

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/mail"
)

// accountemail_test.go -- T173 EC-2's handler half against fakes: the request's order (the unlinked
// row's free refusal, the bucket before the comparison, every exit at the floor), its one flash, the
// refusals' closed set, the letter queue (its own, the person's scope, a contained panic), the two
// links' GETs that spend nothing, the press's two secrets, the undo's gates, and the log lines' keys.
// The database half (the three flat arms over HTTP, the whole loop) is accountemail_db_test.go.

const emailTestNewAddress = "New.Person+Tag@Example.TEST"

// emailChangePanel is the real panel over fakes, flag on, signed in as an owner whose session resolves
// linked or not, with the address change letters' transport over sender (nil: no e-mail), notices as
// the recovery flow, the undo page mounted, and the panel's JSON log.
func emailChangePanel(t *testing.T, linked bool, admins *fakeAdmins, sender *fakeMailSender, notices passwordNotifier) (*browser, *AdminAuth, *lockedBuffer) {
	t.Helper()
	if admins.verify == nil {
		admins.verify = func() (adminauth.Resolved, error) {
			return adminauth.Resolved{
				SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
				Role: "owner", FullName: "KF Owner", Linked: linked, TenantName: "KF Ltd",
			}, nil
		}
	}
	if notices == nil {
		notices = &fakeNotices{}
	}
	cfg := adminTestConfig()
	cfg.AdminIdentity = true
	var opts []AdminAuthOption
	if sender != nil {
		cfg.InviteDelivery = config.InviteDeliveryEmail
		transport, err := NewEmailInvitations(sender, cfg.BaseURL)
		if err != nil {
			t.Fatalf("NewEmailInvitations: %v", err)
		}
		opts = append(opts, InvitationsByEmail(transport))
	}
	logs := &lockedBuffer{}
	records := newFakeLedger()
	h, err := NewAdminAuth(admins, &fakeTrail{}, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
		newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, notices, cfg,
		slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})), opts...)
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	// The floor's wait is not this file's subject except where a test sets it.
	h.emailFloor = 0
	r := chi.NewRouter()
	h.Mount(r)
	h.MountEmailUndo(r)
	registerPanel(r, h)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = panelCookie().Value
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := h.Drain(ctx); err != nil {
			t.Errorf("drain: %v", err)
		}
	})
	return b, h, logs
}

func emailRequestForm(password string) url.Values {
	return url.Values{"new_email": {emailTestNewAddress}, "current_password": {password}}
}

// waitSent waits for the letter queue's worker to have handed n letters to sender.
func waitSent(t *testing.T, sender *fakeMailSender, n int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		sender.mu.Lock()
		got := len(sender.sentScopes)
		sender.mu.Unlock()
		if got >= n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the worker handed %d letter(s) to the sender, want %d", got, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestEmailChange_AnUnlinkedRowIsRefusedForFree (card EC-2 item 1): an older per-business sign-in is not
// the person -- its account page draws no card, and its POST answers "unavailable" without reading the
// bucket, charging it or reaching adminauth. MUTATION (measured): the Linked check removed -> red.
func TestEmailChange_AnUnlinkedRowIsRefusedForFree(t *testing.T) {
	admins := &fakeAdmins{}
	sender := &fakeMailSender{}
	b, h, _ := emailChangePanel(t, false, admins, sender, nil)
	if page := b.do(http.MethodGet, accountHref, nil).Body.String(); strings.Contains(page, "Change your sign-in email") {
		t.Error("an unlinked sign-in's account page draws the address change card")
	}
	for i := 0; i < adminEmailChangeLimit+3; i++ {
		res := b.do(http.MethodPost, accountEmailHref, emailRequestForm("pw"))
		if res.Code != http.StatusSeeOther || !strings.Contains(res.Header().Get("Location"), "problem=unavailable") {
			t.Fatalf("unlinked request %d: %d %q, want 303 unavailable", i+1, res.Code, res.Header().Get("Location"))
		}
	}
	if n := countCalls(admins, "request-email-change"); n != 0 {
		t.Errorf("an unlinked row reached RequestEmailChange %d time(s)", n)
	}
	if within, _ := h.emailChangeLimiter.TryCharge("row:" + panelTestAdmin.String()); !within {
		t.Error("the unlinked row's requests spent a bucket")
	}
}

// TestEmailChange_TheBucketIsChargedBeforeTheComparison (ADR 0044 §5.4, card item 2): every request --
// a wrong password included -- spends the person's unit before adminauth compares anything; past
// adminEmailChangeLimit the page answers 429 and adminauth is not reached. A work-gate refusal gives
// the unit back. MUTATION (measured): Refund dropped -> red.
func TestEmailChange_TheBucketIsChargedBeforeTheComparison(t *testing.T) {
	admins := &fakeAdmins{}
	busy := true
	admins.ident.emailRequest = func(adminauth.Resolved, string, string) (adminauth.EmailChangeRequested, error) {
		if busy {
			return adminauth.EmailChangeRequested{}, adminauth.ErrBusy
		}
		return adminauth.EmailChangeRequested{}, adminauth.ErrBadCredentials
	}
	b, _, _ := emailChangePanel(t, true, admins, &fakeMailSender{}, nil)
	for i := 0; i < 3; i++ {
		if res := b.do(http.MethodPost, accountEmailHref, emailRequestForm("pw")); res.Code != http.StatusServiceUnavailable {
			t.Fatalf("a busy gate answered %d", res.Code)
		}
	}
	busy = false
	for i := 0; i < adminEmailChangeLimit; i++ {
		res := b.do(http.MethodPost, accountEmailHref, emailRequestForm("wrong"))
		if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), emailWrongPassword) {
			t.Fatalf("request %d: %d, want the page with %q (a busy gate's refusals gave their units back)", i+1, res.Code, emailWrongPassword)
		}
	}
	before := countCalls(admins, "request-email-change")
	res := b.do(http.MethodPost, accountEmailHref, emailRequestForm("wrong"))
	if res.Code != http.StatusTooManyRequests || !strings.Contains(res.Body.String(), emailTooManySentence) {
		t.Fatalf("request %d: %d, want 429 with %q", adminEmailChangeLimit+1, res.Code, emailTooManySentence)
	}
	if countCalls(admins, "request-email-change") != before {
		t.Error("the request past the budget reached RequestEmailChange")
	}
}

// TestEmailChange_EveryExitPaysTheFloor (card item 4's structural half): whichever way the request ends --
// unlinked, no e-mail, past the budget, the address refused, a wrong password, a refusal of the person's
// own state, the letter queued -- the answer is not written before emailFloor. MUTATION (measured):
// accountEmailRequest calling emailRequest directly (no atFloor) -> red on every exit.
func TestEmailChange_EveryExitPaysTheFloor(t *testing.T) {
	const floor = 120 * time.Millisecond
	cases := []struct {
		name   string
		linked bool
		mail   bool
		answer func() (adminauth.EmailChangeRequested, error)
		spend  int // requests sent first, to reach the case (the budget's)
	}{
		{"unlinked", false, true, nil, 0},
		{"no e-mail", true, false, nil, 0},
		{"over the budget", true, true, func() (adminauth.EmailChangeRequested, error) {
			return adminauth.EmailChangeRequested{}, adminauth.ErrBadCredentials
		}, adminEmailChangeLimit},
		{"not an address", true, true, func() (adminauth.EmailChangeRequested, error) {
			return adminauth.EmailChangeRequested{}, adminauth.ErrEmailNotAddress
		}, 0},
		{"wrong password", true, true, func() (adminauth.EmailChangeRequested, error) {
			return adminauth.EmailChangeRequested{}, adminauth.ErrBadCredentials
		}, 0},
		{"own state", true, true, func() (adminauth.EmailChangeRequested, error) {
			return adminauth.EmailChangeRequested{Refusal: db.EmailChangeWindowOpen}, nil
		}, 0},
		{"queued", true, true, func() (adminauth.EmailChangeRequested, error) {
			return adminauth.EmailChangeRequested{ID: uuid.New(), Recipient: emailTestNewAddress, Taken: true, ExpiresAt: time.Now().Add(24 * time.Hour)}, nil
		}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			admins := &fakeAdmins{}
			if c.answer != nil {
				admins.ident.emailRequest = func(adminauth.Resolved, string, string) (adminauth.EmailChangeRequested, error) { return c.answer() }
			}
			var sender *fakeMailSender
			if c.mail {
				sender = &fakeMailSender{}
			}
			b, h, _ := emailChangePanel(t, c.linked, admins, sender, nil)
			for i := 0; i < c.spend; i++ {
				b.do(http.MethodPost, accountEmailHref, emailRequestForm("pw"))
			}
			h.emailFloor = floor
			start := time.Now()
			b.do(http.MethodPost, accountEmailHref, emailRequestForm("pw"))
			if d := time.Since(start); d < floor {
				t.Errorf("%s answered after %v, under the floor of %v", c.name, d, floor)
			}
		})
	}
}

// TestEmailChange_TheFlashCarriesNoAddress (card item 2b, K-E10 (i) (a)): a queued request answers 303
// to the account page with ONE closed word, and neither the Location nor the page it leads to carries the
// typed address in any case; the page says the K-E10 (i) sentence. MUTATION (measured): the address
// appended to the redirect -> red.
func TestEmailChange_TheFlashCarriesNoAddress(t *testing.T) {
	admins := &fakeAdmins{}
	admins.ident.emailRequest = func(_ adminauth.Resolved, _, newEmail string) (adminauth.EmailChangeRequested, error) {
		return adminauth.EmailChangeRequested{ID: uuid.New(), Recipient: newEmail, Taken: true, ExpiresAt: time.Now().Add(24 * time.Hour)}, nil
	}
	sender := &fakeMailSender{}
	b, _, _ := emailChangePanel(t, true, admins, sender, nil)
	res := b.do(http.MethodPost, accountEmailHref, emailRequestForm("pw"))
	loc := res.Header().Get("Location")
	if res.Code != http.StatusSeeOther || loc != accountReturn("email-sent", "") {
		t.Fatalf("the request answered %d %q, want 303 to %q", res.Code, loc, accountReturn("email-sent", ""))
	}
	page := b.do(http.MethodGet, loc, nil).Body.String()
	for _, where := range []string{loc, page, res.Body.String()} {
		low := strings.ToLower(where)
		if strings.Contains(low, strings.ToLower(emailTestNewAddress)) || strings.Contains(low, "new.person") ||
			strings.Contains(low, url.QueryEscape(strings.ToLower(emailTestNewAddress))) {
			t.Errorf("the answer carries the typed address: %.200q", where)
		}
	}
	if !strings.Contains(page, emailSentSentence) {
		t.Errorf("the page does not say %q", emailSentSentence)
	}
}

// TestEmailChange_TheRefusalsAreTheClosedSet (card item 3): every value of 00102's closed set has its own
// K-E10 (v) sentence; revert_live carries the person's own undo link's end in the business's zone
// (K-E18 (a)); an unknown value is the generic refusal, not a 500; and no sentence speaks of the new
// address belonging to anybody. MUTATION (measured): a refusal mapped to the generic sentence -> red.
func TestEmailChange_TheRefusalsAreTheClosedSet(t *testing.T) {
	zone, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	until := time.Date(2026, 11, 20, 13, 0, 0, 0, time.UTC)
	seen := map[string]bool{}
	for _, code := range db.EmailChangeRefusals {
		s := emailRefusalSentence(code, until, zone)
		if s == "" || s == emailRefusedGeneric || seen[s] {
			t.Errorf("%s: sentence %q is empty, generic or another refusal's", code, s)
		}
		seen[s] = true
		for _, word := range []string{"taken", "in use", "belongs", "already has", "someone else", "another sign-in", "another business"} {
			if strings.Contains(strings.ToLower(s), word) {
				t.Errorf("%s: %q speaks of the new address's owner (%q)", code, s, word)
			}
		}
	}
	if len(emailRefusalSentences)+1 != len(db.EmailChangeRefusals) {
		t.Errorf("the map holds %d sentences beside revert_live, the closed set %d values", len(emailRefusalSentences), len(db.EmailChangeRefusals))
	}
	if got := emailRefusalSentence(db.EmailChangeRevertLive, until, zone); !strings.Contains(got, "20 November 2026, 14:00") {
		t.Errorf("revert_live says %q, want the undo link's end in the business's zone", got)
	}
	if got := emailRefusalSentence("something_new", until, zone); got != emailRefusedGeneric {
		t.Errorf("an unknown refusal says %q, want the generic one", got)
	}
	// Over HTTP: the refusal is a 200 page with the sentence beside the form, the address not echoed.
	admins := &fakeAdmins{}
	admins.ident.emailRequest = func(adminauth.Resolved, string, string) (adminauth.EmailChangeRequested, error) {
		return adminauth.EmailChangeRequested{Refusal: db.EmailChangeUnlinkedRows}, nil
	}
	b, _, _ := emailChangePanel(t, true, admins, &fakeMailSender{}, nil)
	res := b.do(http.MethodPost, accountEmailHref, emailRequestForm("pw"))
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), emailRefusalSentences[db.EmailChangeUnlinkedRows]) ||
		strings.Contains(strings.ToLower(res.Body.String()), "new.person") {
		t.Errorf("an own-state refusal answered %d without its sentence, or echoed the address", res.Code)
	}
}

// TestEmailChange_TheLetterGoesThroughItsOwnQueueUnderThePersonsScope (card item 5): the letter leaves
// the request on the address change's own queue -- not the reset flow (the notifier is not called) -- and
// the worker sends it through the per-mailbox cap's Send under the PERSON's scope, to the typed address.
// A full queue drops the letter with one line naming the queue's state and the answer does not change.
// MUTATION (measured): the scope keyed on the business -> red. The letter handed to the recovery flow's
// notifier is not measured (the fake notifier would take it silently; the assertion is that it is not
// called).
func TestEmailChange_TheLetterGoesThroughItsOwnQueueUnderThePersonsScope(t *testing.T) {
	admins := &fakeAdmins{}
	admins.ident.bucket = func(adminauth.Resolved) (string, error) { return "person-bucket-1", nil }
	admins.ident.emailRequest = func(_ adminauth.Resolved, _, newEmail string) (adminauth.EmailChangeRequested, error) {
		return adminauth.EmailChangeRequested{ID: uuid.New(), Recipient: newEmail, Taken: true, ExpiresAt: time.Now().Add(24 * time.Hour)}, nil
	}
	sender := &fakeMailSender{}
	notices := &fakeNotices{}
	b, h, logs := emailChangePanel(t, true, admins, sender, notices)
	b.do(http.MethodPost, accountEmailHref, emailRequestForm("pw"))
	waitSent(t, sender, 1)
	sender.mu.Lock()
	scope, msg := sender.sentScopes[0], sender.sent[0]
	sender.mu.Unlock()
	if scope != emailChangeScope("person-bucket-1") || scope == panelTestTenant.String() {
		t.Errorf("the letter was sent under scope %q, want the person's %q", scope, emailChangeScope("person-bucket-1"))
	}
	if msg.To != emailTestNewAddress {
		t.Errorf("the letter went to %q, want the typed address", msg.To)
	}
	if len(notices.emails) != 0 || len(notices.restored) != 0 || len(notices.all()) != 0 {
		t.Error("the request reached the recovery flow")
	}
	// A full queue: a queue of one whose worker is held.
	held := &blockingSender{release: make(chan struct{})}
	q := newEmailLetterQueue(1, &EmailInvitations{sender: held, baseURL: testBaseURL}, slog.New(slog.NewJSONHandler(logs, nil)))
	h.emailLetters = q
	t.Cleanup(func() { close(held.release) })
	for i := 0; i < 3; i++ {
		res := b.do(http.MethodPost, accountEmailHref, emailRequestForm("pw"))
		if res.Code != http.StatusSeeOther || res.Header().Get("Location") != accountReturn("email-sent", "") {
			t.Fatalf("request %d against a full queue answered %d %q", i+1, res.Code, res.Header().Get("Location"))
		}
	}
	if !strings.Contains(logs.String(), `"queue":"full"`) {
		t.Error("a letter the full queue refused left no line")
	}
}

// blockingSender holds every Send until release closes.
type blockingSender struct {
	release chan struct{}
}

func (s *blockingSender) Send(ctx context.Context, _ string, _ mail.Message) (mail.Receipt, error) {
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return mail.Receipt{}, errors.New("held")
}
func (s *blockingSender) Capped(string, string) bool { return false }
func (s *blockingSender) Refusing() bool             { return false }

// panickingSender panics with the recipient in its value on its first call.
type panickingSender struct {
	mu    sync.Mutex
	calls int
	sent  []mail.Message
}

func (s *panickingSender) Send(_ context.Context, _ string, m mail.Message) (mail.Receipt, error) {
	s.mu.Lock()
	s.calls++
	n := s.calls
	s.mu.Unlock()
	if n == 1 {
		panic("relay blew up for " + m.To)
	}
	s.mu.Lock()
	s.sent = append(s.sent, m)
	s.mu.Unlock()
	return mail.Receipt{MessageID: "id-2"}, nil
}
func (s *panickingSender) Capped(string, string) bool { return false }
func (s *panickingSender) Refusing() bool             { return false }

// TestEmailChange_TheWorkerContainsAPanicAndLogsNoValue (card item 5): a letter whose send panics is
// caught on the worker; the line names ids only (the panic's value -- the address here -- is not
// logged) and the next letter still goes. MUTATIONS (measured): the recover removed -> red (the test binary
// dies); the recipient added to the accepted line -> red (the log scan).
func TestEmailChange_TheWorkerContainsAPanicAndLogsNoValue(t *testing.T) {
	logs := &lockedBuffer{}
	s := &panickingSender{}
	q := newEmailLetterQueue(4, &EmailInvitations{sender: s, baseURL: testBaseURL}, slog.New(slog.NewJSONHandler(logs, nil)))
	req, _ := http.NewRequest(http.MethodPost, "/", nil)
	for i := 0; i < 2; i++ {
		q.hand(req, slog.New(slog.NewJSONHandler(logs, nil)), emailLetterJob{scope: "s", recipient: emailTestNewAddress, adminUserID: panelTestAdmin, tenantID: panelTestTenant, ref: uuid.New()})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := q.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if len(s.sent) != 1 {
		t.Fatalf("%d letters sent after the panic, want the second one", len(s.sent))
	}
	if !strings.Contains(logs.String(), "a letter panicked") {
		t.Error("the panic left no line")
	}
	assertEmailLogClean(t, logs.String())
}

// emailLogKeys is ADR 0022 §10's closed set as this file's lines use it, plus identity_id (ADR 0031
// §5.15) and slog's own three: ids, fixed words, a SendError's class and code, a message id, the
// queue's state.
var emailLogKeys = map[string]bool{
	"time": true, "level": true, "msg": true,
	"admin_user_id": true, "tenant_id": true, "identity_id": true, "ip": true, "err_type": true, "err": true,
	"class": true, "smtp_code": true, "message_id": true, "queue": true, "outcome": true, "reason": true,
	"at": true, "scope": true,
}

// assertEmailLogClean holds every JSON line to emailLogKeys and to no address in any case.
func assertEmailLogClean(t *testing.T, jsonLines string) {
	t.Helper()
	low := strings.ToLower(jsonLines)
	for _, a := range []string{strings.ToLower(emailTestNewAddress), "new.person", "old.person@example.test"} {
		if strings.Contains(low, a) {
			t.Errorf("the log carries an address (%q)", a)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(jsonLines), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("a log line is not JSON: %v", err)
		}
		for k := range rec {
			if !emailLogKeys[k] {
				t.Errorf("log key %q outside the closed set (line %.160q)", k, line)
			}
		}
	}
}

// TestEmailChange_TheLinksGETSpendsNothing (R19, card items 6 and 9): the change link's GET calls
// nothing in adminauth, moves the value into a short-lived cookie and redirects to the account page,
// which then offers the press -- worded without the address; the undo link's GET calls nothing either,
// moves "<csrf>.<token>" into its own cookie, redirects to the bare path and draws the one press.
// MUTATION (measured): the landing calling ConfirmEmailChange -> red.
func TestEmailChange_TheLinksGETSpendsNothing(t *testing.T) {
	admins := &fakeAdmins{}
	b, _, _ := emailChangePanel(t, true, admins, &fakeMailSender{}, nil)
	const value = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCdE"
	got := b.do(http.MethodGet, emailChangeLandingPath+"?t="+value, nil)
	if got.Code != http.StatusSeeOther || got.Header().Get("Location") != accountHref || got.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("the change link's GET: %d %q", got.Code, got.Header().Get("Location"))
	}
	if b.cookies[accountEmailCookieName] != value {
		t.Fatalf("the cookie holds %q", b.cookies[accountEmailCookieName])
	}
	page := b.do(http.MethodGet, accountHref, nil).Body.String()
	if !strings.Contains(page, "Change your sign-in email to the address this link was sent to?") {
		t.Error("the account page does not offer the press for the link this browser opened")
	}
	if !strings.Contains(page, "Your new email will be visible to the admins of every business you belong to.") {
		t.Error("the card does not carry K-E10 (ii)'s warning")
	}
	got = b.do(http.MethodGet, emailUndoPath+"?t="+value, nil)
	if got.Code != http.StatusSeeOther || got.Header().Get("Location") != emailUndoPath {
		t.Fatalf("the undo link's GET: %d %q", got.Code, got.Header().Get("Location"))
	}
	if _, tok, ok := strings.Cut(b.cookies[emailUndoCookieName], "."); !ok || tok != value {
		t.Fatalf("the undo cookie holds %q", b.cookies[emailUndoCookieName])
	}
	if page := b.do(http.MethodGet, emailUndoPath, nil).Body.String(); !strings.Contains(page, "Undo the change") {
		t.Error("the undo page does not draw its press")
	}
	for _, c := range admins.identCalls() {
		if c == "confirm-email-change" || c == "undo-email-change" {
			t.Fatalf("a link's GET reached %s", c)
		}
	}
}

// TestEmailChange_ThePressNeedsTheLinkAndALinkedSession (card item 7): without the link's cookie, or from
// an unlinked sign-in, the press is refused before adminauth; with both it spends THAT value, clears the
// cookie, hands the result to the recovery flow's notice (item 8) and answers the done word; a refusal
// is the one sentence. MUTATION (measured): the notice call dropped -> red.
func TestEmailChange_ThePressNeedsTheLinkAndALinkedSession(t *testing.T) {
	const value = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCdE"
	ident := uuid.New()
	admins := &fakeAdmins{}
	admins.ident.emailConfirm = func(_ adminauth.Resolved, raw string) (adminauth.EmailChanged, error) {
		if raw != value {
			return adminauth.EmailChanged{}, adminauth.ErrEmailChangeRefused
		}
		return adminauth.EmailChanged{IdentityID: ident, OldEmail: "old.person@example.test", NewEmail: emailTestNewAddress}, nil
	}
	notices := &fakeNotices{}
	b, _, logs := emailChangePanel(t, true, admins, &fakeMailSender{}, notices)
	res := b.do(http.MethodPost, accountEmailConfirmHref, url.Values{})
	if !strings.Contains(res.Header().Get("Location"), "problem=email-refused") || countCalls(admins, "confirm-email-change") != 0 {
		t.Fatalf("a press without the link: %q, %d call(s)", res.Header().Get("Location"), countCalls(admins, "confirm-email-change"))
	}
	unlinkedAdmins := &fakeAdmins{}
	unlinkedAdmins.ident.emailConfirm = admins.ident.emailConfirm
	unlinked, _, _ := emailChangePanel(t, false, unlinkedAdmins, &fakeMailSender{}, nil)
	unlinked.cookies[accountEmailCookieName] = value
	if res := unlinked.do(http.MethodPost, accountEmailConfirmHref, url.Values{}); !strings.Contains(res.Header().Get("Location"), "problem=email-refused") {
		t.Fatalf("an unlinked press answered %q", res.Header().Get("Location"))
	}
	if countCalls(unlinkedAdmins, "confirm-email-change") != 0 {
		t.Fatal("an unlinked sign-in's press reached adminauth")
	}
	b.cookies[accountEmailCookieName] = value
	res = b.do(http.MethodPost, accountEmailConfirmHref, url.Values{})
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != accountReturn("email-changed", "") {
		t.Fatalf("the press answered %d %q", res.Code, res.Header().Get("Location"))
	}
	if _, kept := b.cookies[accountEmailCookieName]; kept {
		t.Error("the press left the link's cookie")
	}
	if len(notices.emails) != 1 || notices.emails[0].IdentityID != ident {
		t.Fatalf("the recovery flow was told %d change(s), want this one", len(notices.emails))
	}
	b.cookies[accountEmailCookieName] = "AnotherValueAnotherValueAnotherValue0123456"
	if res := b.do(http.MethodPost, accountEmailConfirmHref, url.Values{}); res.Header().Get("Location") != accountReturn("", "email-refused") {
		t.Fatalf("a refused press answered %q", res.Header().Get("Location"))
	}
	if page := b.do(http.MethodGet, accountReturn("", "email-refused"), nil).Body.String(); !strings.Contains(page, html.EscapeString(emailRefusedSentence)) {
		t.Error("the refusal's page does not say K-E10 (iv)")
	}
	assertEmailLogClean(t, logs.String())
}

// TestEmailUndo_ItsGatesAndItsOneResult (card items 9, 13): the undo press refuses a cross-site POST, a
// browser without the link's cookie and a wrong synchronizer value before adminauth; spent, it clears the
// cookie, asks the recovery flow for ONE link to the address it gave back (K-E11 (a)) and draws the
// result; refused, the one sentence; past adminEmailUndoLimit from one address, 429 before adminauth. It
// never reads the panel session (the browser here holds none). MUTATIONS (measured): the Origin check
// removed -> red; addressRestored dropped -> red.
func TestEmailUndo_ItsGatesAndItsOneResult(t *testing.T) {
	const value = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCdE"
	admins := &fakeAdmins{}
	admins.ident.emailUndo = func(raw string) (adminauth.EmailRestored, error) {
		if raw != value {
			return adminauth.EmailRestored{}, adminauth.ErrEmailUndoRefused
		}
		return adminauth.EmailRestored{Address: "old.person@example.test", RevokedSessions: 3}, nil
	}
	notices := &fakeNotices{}
	b, _, logs := emailChangePanel(t, true, admins, &fakeMailSender{}, notices)
	delete(b.cookies, adminauth.CookieName)
	open := func() string {
		b.do(http.MethodGet, emailUndoPath+"?t="+value, nil)
		csrf, _, _ := strings.Cut(b.cookies[emailUndoCookieName], ".")
		return csrf
	}
	csrf := open()
	b.origin = "https://evil.example"
	if res := b.do(http.MethodPost, emailUndoPath, url.Values{"csrf": {csrf}}); res.Code != http.StatusBadRequest {
		t.Errorf("a cross-site press answered %d", res.Code)
	}
	b.origin = testBaseURL
	if res := b.do(http.MethodPost, emailUndoPath, url.Values{"csrf": {"not-it"}}); res.Code != http.StatusBadRequest {
		t.Errorf("a wrong synchronizer value answered %d", res.Code)
	}
	saved := b.cookies[emailUndoCookieName]
	delete(b.cookies, emailUndoCookieName)
	if res := b.do(http.MethodPost, emailUndoPath, url.Values{"csrf": {csrf}}); res.Code != http.StatusBadRequest {
		t.Errorf("a press without the cookie answered %d", res.Code)
	}
	if n := countCalls(admins, "undo-email-change"); n != 0 {
		t.Fatalf("a refused gate reached adminauth %d time(s)", n)
	}
	b.cookies[emailUndoCookieName] = saved
	res := b.do(http.MethodPost, emailUndoPath, url.Values{"csrf": {csrf}})
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "Your sign-in email is back. We sent a link to set a new password.") {
		t.Fatalf("the undo answered %d without K-E10 (viii)", res.Code)
	}
	if _, kept := b.cookies[emailUndoCookieName]; kept {
		t.Error("the undo left its cookie")
	}
	if len(notices.restored) != 1 || notices.restored[0] != "old.person@example.test" {
		t.Fatalf("the recovery flow was asked for %v, want one link to the address given back", notices.restored)
	}
	// A spent link: the one sentence.
	csrf = open()
	b.cookies[emailUndoCookieName] = csrf + ".SomeOtherValueSomeOtherValueSomeOtherValue0"
	if res := b.do(http.MethodPost, emailUndoPath, url.Values{"csrf": {csrf}}); res.Code != http.StatusOK || !strings.Contains(res.Body.String(), "That link can't be used") {
		t.Errorf("a refused undo answered %d", res.Code)
	}
	// The per-address budget: two presses so far reached it; the rest up to the limit, then 429.
	for i := 2; i < adminEmailUndoLimit; i++ {
		csrf = open()
		b.do(http.MethodPost, emailUndoPath, url.Values{"csrf": {csrf}})
	}
	before := countCalls(admins, "undo-email-change")
	csrf = open()
	if res := b.do(http.MethodPost, emailUndoPath, url.Values{"csrf": {csrf}}); res.Code != http.StatusTooManyRequests {
		t.Errorf("press %d from one address answered %d, want 429", adminEmailUndoLimit+1, res.Code)
	}
	if countCalls(admins, "undo-email-change") != before {
		t.Error("the press past the budget reached adminauth")
	}
	assertEmailLogClean(t, logs.String())
}

// TestEmailUndo_IsAdminauthsPath: the literal the route scans resolve is adminauth's path.
func TestEmailUndo_IsAdminauthsPath(t *testing.T) {
	if emailUndoPath != adminauth.UndoEmailPath {
		t.Fatalf("emailUndoPath %q, adminauth mints %q", emailUndoPath, adminauth.UndoEmailPath)
	}
}

// TestEmailChange_FlagOffIsAPathThePanelNeverHad (ADR 0031 §8, ADR 0044 §5.10): with
// TAPPA_ADMIN_IDENTITY off the five routes answer the router's own 404 and the card is not drawn.
// MUTATION (measured): the request mounted outside mountGated -> red.
func TestEmailChange_FlagOffIsAPathThePanelNeverHad(t *testing.T) {
	b, _ := identityPanel(t, false, true, &fakeAdmins{}, nil)
	never := b.do(http.MethodGet, "/admin/never-had-this", nil)
	for _, rt := range [][2]string{{http.MethodPost, accountEmailHref}, {http.MethodPost, accountEmailConfirmHref},
		{http.MethodGet, emailChangeLandingPath}} {
		if got := b.do(rt[0], rt[1], url.Values{}); got.Code != http.StatusNotFound || got.Body.String() != never.Body.String() {
			t.Errorf("flag off, %s %s: %d", rt[0], rt[1], got.Code)
		}
	}
	if page := b.do(http.MethodGet, accountHref, nil).Body.String(); strings.Contains(page, "Change your sign-in email") {
		t.Error("flag off, the account page draws the card")
	}
	cfg := adminTestConfig()
	h, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
		newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	h.MountEmailUndo(r)
	ub := newBrowser(t, r)
	for _, m := range []string{http.MethodGet, http.MethodPost} {
		if got := ub.do(m, emailUndoPath, url.Values{}); got.Code != http.StatusNotFound {
			t.Errorf("flag off, %s %s: %d", m, emailUndoPath, got.Code)
		}
	}
}

// TestEmailChange_TheWindowsRefusalSaysWhyDuringTheUndoWindow (K-E19 (a), card item 5b): a refused
// "Remove" shows the window's generic refusal with the K-E19 sentence. The database half -- the row stays
// linked while the undo link lives (K-E16 (a)) -- is EC-1's internal/db test.
func TestEmailChange_TheWindowsRefusalSaysWhyDuringTheUndoWindow(t *testing.T) {
	admins := &fakeAdmins{}
	admins.ident.window = func() (adminauth.Window, error) {
		return adminauth.Window{Linked: true, Verified: true, Open: true, Scope: "reset", OpenUntil: time.Now().Add(time.Hour),
			Rows: []adminauth.WindowRow{{TenantID: panelTestTenant, TenantName: "KF Ltd", Role: "owner"}}}, nil
	}
	admins.ident.unlink = func(adminauth.Resolved, string, uuid.UUID) (adminauth.FreshAct, bool, error) {
		return adminauth.FreshAct{}, false, adminauth.ErrWindowRefused
	}
	b, _ := identityPanel(t, true, true, admins, nil)
	res := b.do(http.MethodPost, accountUnlinkHref, url.Values{"tenant_id": {panelTestTenant.String()}, "password": {"pw"}})
	page := b.do(http.MethodGet, res.Header().Get("Location"), nil).Body.String()
	if !strings.Contains(page, "If you changed your sign-in email in the last 7 days, removing a business waits until then.") {
		t.Error("the window's refusal does not carry K-E19's sentence")
	}
}

// TestEmailChange_ARequestWithoutTheRecoveryChannelIsRefused (EC-2 security review, ORTA): the old
// address's notice and its undo link ride the recovery channel (TAPPA_RESET_DELIVERY); with invitations
// e-mailed but recovery e-mail off, a change would go through with no notice and no way back. The request
// is refused with the no-e-mail sentence before the bucket is read or adminauth is reached. MUTATION
// (measured): the canNotify check removed -> red.
func TestEmailChange_ARequestWithoutTheRecoveryChannelIsRefused(t *testing.T) {
	admins := &fakeAdmins{}
	admins.ident.emailRequest = func(adminauth.Resolved, string, string) (adminauth.EmailChangeRequested, error) {
		return adminauth.EmailChangeRequested{ID: uuid.New(), Recipient: emailTestNewAddress, Taken: true, ExpiresAt: time.Now().Add(24 * time.Hour)}, nil
	}
	sender := &fakeMailSender{}
	b, h, _ := emailChangePanel(t, true, admins, sender, &fakeNotices{cannotNotify: true})
	res := b.do(http.MethodPost, accountEmailHref, emailRequestForm("pw"))
	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != accountReturn("", "email-no-mail") {
		t.Fatalf("the request answered %d %q, want the no-e-mail refusal", res.Code, res.Header().Get("Location"))
	}
	if n := countCalls(admins, "request-email-change"); n != 0 {
		t.Errorf("the request reached RequestEmailChange %d time(s)", n)
	}
	if within, n := h.emailChangeLimiter.TryCharge("row:" + panelTestAdmin.String()); !within || n != 1 {
		t.Errorf("the refused request spent the bucket (count %d)", n)
	}
	if sender.count() != 0 {
		t.Error("a letter was sent")
	}
}

// TestEmailChange_TheChangeLetterSaysOneDay (EC-2 third eye, ORTA): the letter rendered a moment after
// the link was written still says the approved "1 day" (the lifetime rounds down, so the time left is
// rounded to the minute first, as the verification letter's is). MUTATION (measured): the Round removed
// -> red ("23 hours").
func TestEmailChange_TheChangeLetterSaysOneDay(t *testing.T) {
	s := &fakeMailSender{}
	q := newEmailLetterQueue(4, &EmailInvitations{sender: s, baseURL: testBaseURL}, slog.New(slog.DiscardHandler))
	link := testBaseURL + adminauth.ChangeEmailPath + "?" + adminauth.ChangeEmailParam + "=AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCdE"
	expires := time.Now().Add(24 * time.Hour)
	time.Sleep(30 * time.Millisecond)
	req, _ := http.NewRequest(http.MethodPost, "/", nil)
	q.hand(req, slog.New(slog.DiscardHandler), emailLetterJob{scope: "s", recipient: emailTestNewAddress, link: link, expiresAt: expires, ref: uuid.New()})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := q.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if s.count() != 1 || !strings.Contains(s.sent[0].Text, "stays valid for 1 day.") || !strings.Contains(s.sent[0].Text, link) {
		t.Fatalf("the change letter does not say its approved lifetime (or lacks its link):\n%v", s.sent)
	}
}

// TestEmailChange_AFullResetOutboxDoesNotStopTheLetter (card EC-2 item 15, the third eye's DÜŞÜK-2): with
// the recovery flow's outbox held full (one grant inside a send, resetOutboxSize waiting, one more
// refused -- the control), the change letter still reaches its sender: the letters' queue is its own.
// MUTATION (measured): the request refusing the letter while the reset outbox is full (a shared capacity)
// -> red.
func TestEmailChange_AFullResetOutboxDoesNotStopTheLetter(t *testing.T) {
	gate := newGateChannel()
	first, fill, late := grantsFor("first@x.test", 1), grantsFor("fill@x.test", resetOutboxSize), grantsFor("late@x.test", 1)
	resets := &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{"first@x.test": first, "fill@x.test": fill, "late@x.test": late}}
	trail := &fakeTrail{}
	router, flow := newResetRouter(t, resets, gate, trail)
	t.Cleanup(func() { close(gate.release) })
	requestReset(t, newBrowser(t, router), "first@x.test")
	gate.waitStarted(t)
	requestReset(t, newBrowser(t, router), "fill@x.test")
	requestReset(t, newBrowser(t, router), "late@x.test")
	full := 0
	for _, e := range trail.eventsSnapshot() {
		if d, ok := e.Detail.(adminResetDetail); ok && d.Reason == resetReasonOutboxFull {
			full++
		}
	}
	if full != 1 {
		t.Fatalf("CONTROL FAILED: %d grant(s) refused by a full outbox, want 1", full)
	}
	admins := &fakeAdmins{}
	admins.ident.emailRequest = func(_ adminauth.Resolved, _, newEmail string) (adminauth.EmailChangeRequested, error) {
		return adminauth.EmailChangeRequested{ID: uuid.New(), Recipient: newEmail, Taken: true, ExpiresAt: time.Now().Add(24 * time.Hour)}, nil
	}
	sender := &fakeMailSender{}
	b, _, _ := emailChangePanel(t, true, admins, sender, flow)
	if res := b.do(http.MethodPost, accountEmailHref, emailRequestForm("pw")); res.Header().Get("Location") != accountReturn("email-sent", "") {
		t.Fatalf("the request answered %d %q", res.Code, res.Header().Get("Location"))
	}
	waitSent(t, sender, 1)
}

// TestEmailChange_ThePressIsRefusedWithoutTheRecoveryChannel (EC-2 re-review, DÜŞÜK): a link written
// while recovery e-mail was on is pressed after it was switched off -- the press is refused with the one
// refusal before adminauth, so no change completes without its notice and undo link. MUTATION (measured):
// the canNotify check removed from the press -> red.
func TestEmailChange_ThePressIsRefusedWithoutTheRecoveryChannel(t *testing.T) {
	admins := &fakeAdmins{}
	admins.ident.emailConfirm = func(adminauth.Resolved, string) (adminauth.EmailChanged, error) {
		return adminauth.EmailChanged{IdentityID: uuid.New(), OldEmail: "old.person@example.test"}, nil
	}
	notices := &fakeNotices{cannotNotify: true}
	b, _, _ := emailChangePanel(t, true, admins, &fakeMailSender{}, notices)
	b.cookies[accountEmailCookieName] = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCdE"
	res := b.do(http.MethodPost, accountEmailConfirmHref, url.Values{})
	if res.Header().Get("Location") != accountReturn("", "email-refused") {
		t.Fatalf("the press answered %d %q, want the one refusal", res.Code, res.Header().Get("Location"))
	}
	if n := countCalls(admins, "confirm-email-change"); n != 0 || len(notices.emails) != 0 {
		t.Errorf("the press reached ConfirmEmailChange %d time(s)", n)
	}
}
