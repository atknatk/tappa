package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/db"
)

// --- fakes ---------------------------------------------------------------------

// fakeResets stands in for adminauth.Resets. It is deliberately DUMB: every
// decision the product makes lives in internal/adminauth and is measured there
// against real Postgres (resetrequest_db_test.go, reset_db_test.go). What this fake
// buys is control over the two facts the HANDLER branches on — how many links an
// address produced, and how Consume failed — plus a clock, so the timing test can
// make the two arms genuinely unequal in work.
type fakeResets struct {
	mu sync.Mutex

	// grantsFor decides what an address resolves to. A nil entry means "no active
	// administrator", which is the unregistered arm.
	grantsFor map[string][]adminauth.ResetGrant
	// issueDelay is how long IssueForEmail takes for an address that resolves. It
	// exists so the timing test measures a real inequality rather than a hoped-for
	// one.
	issueDelay time.Duration
	issueErr   error

	consumed        adminauth.ConsumedReset
	consumeResolved db.ResolvedPasswordReset
	consumeErr      error

	issueCalls    int
	consumeCalls  int
	issuedFor     []string
	consumedWith  []string
	consumePasswd []string
}

func (f *fakeResets) IssueForEmail(_ context.Context, email string) ([]adminauth.ResetGrant, error) {
	f.mu.Lock()
	f.issueCalls++
	f.issuedFor = append(f.issuedFor, email)
	delay, err, grants := f.issueDelay, f.issueErr, f.grantsFor[email]
	f.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	if err != nil {
		return nil, err
	}
	return grants, nil
}

func (f *fakeResets) Consume(_ context.Context, t adminauth.ResetToken, newPassword string) (adminauth.ConsumedReset, db.ResolvedPasswordReset, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.consumeCalls++
	// reveal is unexported in adminauth, which is the point of that type; a test can
	// still see what the handler passed by asking the value to render itself the one
	// way this package can — through the link the handler would build. Here the
	// token's identity is not what is being measured, so the redacted form is enough
	// to count calls, and the RAW value is measured in the DB test instead.
	f.consumedWith = append(f.consumedWith, t.String())
	f.consumePasswd = append(f.consumePasswd, newPassword)
	return f.consumed, f.consumeResolved, f.consumeErr
}

func (f *fakeResets) calls() (issue, consume int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.issueCalls, f.consumeCalls
}

// recordingChannel is a ResetChannel that keeps what it was handed.
type recordingChannel struct {
	mu        sync.Mutex
	delivered []ResetDelivery
	delay     time.Duration
	err       error
}

func (c *recordingChannel) DeliverReset(_ context.Context, d ResetDelivery) error {
	c.mu.Lock()
	delay, err := c.delay, c.err
	c.delivered = append(c.delivered, d)
	c.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	return err
}

func (c *recordingChannel) all() []ResetDelivery {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ResetDelivery, len(c.delivered))
	copy(out, c.delivered)
	return out
}

// grantFor builds one issued link the way production does, so the link a test
// inspects is the string adminauth.IssuedReset.Link produces rather than one this
// file formats.
func grantFor(recipient string) adminauth.ResetGrant {
	return adminauth.ResetGrant{
		Recipient: recipient,
		Issued: adminauth.IssuedReset{
			Reset: adminauth.Reset{
				ID:          uuid.New(),
				TenantID:    uuid.New(),
				AdminUserID: uuid.New(),
				CreatedAt:   time.Now(),
				ExpiresAt:   time.Now().Add(adminauth.ResetTTL),
			},
			Token: adminauth.ParseResetToken("qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8"),
		},
	}
}

// newResetRouter wires the REAL handler with the REAL budgets, so a test drives what
// production drives (the M5-04 lesson: a test that builds its own limiter measures
// its own limiter).
func newResetRouter(t *testing.T, resets panelResets, mail ResetChannel, trail *fakeTrail) (http.Handler, *AdminReset) {
	t.Helper()
	h, err := NewAdminReset(resets, mail, trail, adminTestConfig(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	return r, h
}

// requestReset drives the whole request form: GET for the synchronizer token, then
// POST with the address.
func requestReset(t *testing.T, b *browser, email string) string {
	t.Helper()
	page := b.do(http.MethodGet, adminResetPath, nil)
	if page.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", adminResetPath, page.Code)
	}
	csrf := csrfFrom(t, htmlOf(t, page))
	rec := b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {email}})
	return htmlOf(t, rec)
}

// --- criterion 5: one answer for every address ----------------------------------

// TestAdminReset_RegisteredAndUnregisteredAreByteIdentical is the fifth acceptance
// criterion's first three words — status, body, wording.
//
// 🔴 THE POSITIVE CONTROL IS NOT DERIVED FROM THE THING UNDER TEST. Comparing two
// bodies proves nothing unless the comparison can FAIL, and a control built by
// mutating one of the two bodies would only prove that string equality works. The
// control here is a DIFFERENT REAL RESPONSE from the same production template: the
// same route on a deployment with no delivery channel. If that one compares equal
// too, this test is reading something that does not vary and says so.
func TestAdminReset_RegisteredAndUnregisteredAreByteIdentical(t *testing.T) {
	t.Parallel()
	known := "owner@registered.example.test"
	resets := &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{
		known: {grantFor(known)},
	}}
	mail := &recordingChannel{}
	router, _ := newResetRouter(t, resets, mail, &fakeTrail{})

	registered := requestReset(t, newBrowser(t, router), known)
	unregistered := requestReset(t, newBrowser(t, router), "nobody@unregistered.example.test")

	if registered != unregistered {
		t.Errorf("the two answers differ.\nREGISTERED:\n%s\n\nUNREGISTERED:\n%s", registered, unregistered)
	}
	// ANTI-VACUITY: the registered arm really did resolve to a link, so the two
	// bodies above are equal DESPITE a difference having happened behind them.
	if n := len(mail.all()); n != 1 {
		t.Fatalf("the channel received %d link(s); if it received none, the two arms of this "+
			"test were the same arm and the comparison proved nothing", n)
	}
	// THE DELIVERABLE PAGE'S TITLE, pinned here so the two arms cannot be repaired by
	// collapsing them onto one string: a deployment that CAN send must still say so in
	// the tab, and the no-channel test refuses that same string in its own document.
	if !strings.Contains(registered, "<title>Check your email") {
		t.Errorf("a deployment that can send does not say so in the document title:\n%s", registered)
	}

	// THE INDEPENDENT CONTROL: a deployment that cannot deliver answers the same
	// route differently. Same handler, same template, same address.
	dark, _ := newResetRouter(t, &fakeResets{}, nil, &fakeTrail{})
	undeliverable := requestReset(t, newBrowser(t, dark), known)
	if undeliverable == registered {
		t.Error("the undeliverable deployment's answer is byte-identical to the deliverable " +
			"one, so this comparison cannot distinguish two genuinely different pages and " +
			"the equality asserted above means nothing")
	}
}

// TestAdminReset_TimingIsFlat is the fifth criterion's LAST word, which no body
// comparison can see.
//
// The two arms are made GENUINELY unequal in work — the registered one pays 40 ms of
// issuing plus 15 ms of delivery, the unregistered one pays nothing — and the
// response times are still required to be indistinguishable, because
// resetRequestFloor holds both.
func TestAdminReset_TimingIsFlat(t *testing.T) {
	t.Parallel()
	known := "slow@registered.example.test"
	resets := &fakeResets{
		grantsFor:  map[string][]adminauth.ResetGrant{known: {grantFor(known)}},
		issueDelay: 40 * time.Millisecond,
	}
	router, _ := newResetRouter(t, resets, &recordingChannel{delay: 15 * time.Millisecond}, &fakeTrail{})

	const samples = 5
	var hit, miss []time.Duration
	for i := 0; i < samples; i++ {
		// Interleaved, so a machine that gets busier during the run moves both arms.
		hit = append(hit, timeOneRequest(t, router, known))
		miss = append(miss, timeOneRequest(t, router, "nobody@unregistered.example.test"))
	}
	sort.Slice(hit, func(i, j int) bool { return hit[i] < hit[j] })
	sort.Slice(miss, func(i, j int) bool { return miss[i] < miss[j] })
	mHit, mMiss := hit[samples/2], miss[samples/2]

	// BOTH ARMS PAY THE FLOOR. This is the assertion that goes red the moment the
	// floor is removed: without it the unregistered arm answers in microseconds.
	for _, d := range append(append([]time.Duration{}, hit...), miss...) {
		if d < resetRequestFloor {
			t.Fatalf("a response took %v, under the floor of %v — the two arms are no longer "+
				"held to a common time and the difference between them is observable",
				d, resetRequestFloor)
		}
	}
	// AND THEY ARE CLOSE. The band is wide on purpose: this suite runs with -race on
	// a shared machine and a narrow band would be a flake rather than a measurement.
	// The property being measured is that the 55 ms of extra work has DISAPPEARED,
	// not that two clocks agree.
	delta := mHit - mMiss
	if delta < 0 {
		delta = -delta
	}
	if delta > 60*time.Millisecond {
		t.Errorf("median registered %v vs unregistered %v (delta %v): the work difference is "+
			"leaking through the clock", mHit, mMiss, delta)
	}
	t.Logf("median registered %v, unregistered %v, floor %v", mHit, mMiss, resetRequestFloor)
}

func timeOneRequest(t *testing.T, router http.Handler, email string) time.Duration {
	t.Helper()
	b := newBrowser(t, router)
	page := b.do(http.MethodGet, adminResetPath, nil)
	csrf := csrfFrom(t, htmlOf(t, page))
	start := time.Now()
	b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {email}})
	return time.Since(start)
}

// --- criterion 3: the link goes to one place, and never to the caller -----------

// TestAdminReset_NoResponseCarriesTheLink is the half of criterion 3 this layer can
// hold: whatever else happens, the raw token must not come back to whoever asked.
//
// 🔴 THE POSITIVE CONTROL IS THE DELIVERED LINK ITSELF, which is produced by
// adminauth.IssuedReset.Link and not by this file: the scanner is pointed at that
// string first and MUST find the token there. A scanner that cannot find a token it
// is looking straight at would report every page clean.
func TestAdminReset_NoResponseCarriesTheLink(t *testing.T) {
	t.Parallel()
	known := "owner@registered.example.test"
	g := grantFor(known)
	resets := &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: {g}}}
	mail := &recordingChannel{}
	router, h := newResetRouter(t, resets, mail, &fakeTrail{})

	b := newBrowser(t, router)
	bodies := map[string]string{}
	page := b.do(http.MethodGet, adminResetPath, nil)
	bodies["GET "+adminResetPath] = htmlOf(t, page)
	csrf := csrfFrom(t, bodies["GET "+adminResetPath])
	sent := b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {known}})
	bodies["POST "+adminResetPath] = htmlOf(t, sent)

	delivered := mail.all()
	if len(delivered) != 1 {
		t.Fatalf("the channel received %d link(s), want 1", len(delivered))
	}
	link := delivered[0].Link
	token := strings.TrimPrefix(link, h.linkBase+"?t=")
	if token == link || token == "" {
		t.Fatalf("could not take the token out of %q; this test is looking for the wrong string", link)
	}
	// THE CONTROL: the scanner sees the token where it genuinely is.
	if !strings.Contains(link, token) {
		t.Fatal("the scan cannot find the token inside the link it was taken from, so a page " +
			"carrying one would be reported clean")
	}

	// And the whole rest of the flow, including the page the link opens.
	opened := b.do(http.MethodGet, adminResetNewPath+"?t="+token, nil)
	bodies["GET "+adminResetNewPath+"?t="] = htmlOf(t, opened)
	if loc := opened.Header().Get("Location"); strings.Contains(loc, token) {
		t.Errorf("the redirect puts the token back in a URL: %s", loc)
	}
	form := b.do(http.MethodGet, adminResetNewPath, nil)
	bodies["GET "+adminResetNewPath] = htmlOf(t, form)

	for where, body := range bodies {
		if strings.Contains(body, token) {
			t.Errorf("%s carries the raw recovery token in its body", where)
		}
		if strings.Contains(body, link) {
			t.Errorf("%s carries the whole recovery link in its body", where)
		}
	}
	if len(bodies) != 4 {
		t.Fatalf("scanned %d bodies; the flow has four pages and this test is missing one", len(bodies))
	}
}

// TestAdminReset_DeliveryGoesToTheAddressOnTheRow pins the direction of criterion 3
// at THIS layer: the handler passes on the recipient the domain resolved and never
// the one the form carried. The domain half — that the resolved recipient comes off
// the administrator's own row — is measured against real Postgres in
// internal/adminauth.
func TestAdminReset_DeliveryGoesToTheAddressOnTheRow(t *testing.T) {
	t.Parallel()
	typed := "OWNER@Registered.Example.Test"
	onTheRow := "owner@registered.example.test"
	g := grantFor(onTheRow)
	resets := &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{typed: {g}}}
	mail := &recordingChannel{}
	router, _ := newResetRouter(t, resets, mail, &fakeTrail{})

	requestReset(t, newBrowser(t, router), typed)

	delivered := mail.all()
	if len(delivered) != 1 {
		t.Fatalf("the channel received %d link(s), want 1", len(delivered))
	}
	if delivered[0].Recipient != onTheRow {
		t.Errorf("delivered to %q, want the address on the administrator's row (%q). The typed "+
			"spelling was %q and citext equality is case-insensitive, so the two are one "+
			"identity and can be two mailboxes.", delivered[0].Recipient, onTheRow, typed)
	}
}

// --- the undeliverable deployment ----------------------------------------------

// TestAdminReset_WithoutAChannelNothingIsIssuedAndTheScreenSaysSo is the honesty
// half of the M7-04 card's original criterion ("gönderim başarısızlığı kullanıcıya
// dürüstçe bildiriliyor, sessiz yutulmuyor") AND the reason the check runs before
// the address is resolved.
func TestAdminReset_WithoutAChannelNothingIsIssuedAndTheScreenSaysSo(t *testing.T) {
	t.Parallel()
	known := "owner@registered.example.test"
	resets := &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: {grantFor(known)}}}
	router, _ := newResetRouter(t, resets, nil, &fakeTrail{})

	form := htmlOf(t, newBrowser(t, router).do(http.MethodGet, adminResetPath, nil))
	if !strings.Contains(form, "cannot send email yet") {
		t.Error("the request form does not tell the visitor that no link can arrive, so it " +
			"accepts an address and quietly does nothing")
	}
	body := requestReset(t, newBrowser(t, router), known)
	if !strings.Contains(body, "Nothing was sent") {
		t.Errorf("the answer does not say that nothing was sent:\n%s", body)
	}
	// 🔴 THE FORBIDDEN LIST IS SCANNED OVER THE WHOLE DOCUMENT, AND "Check your email"
	// IS ON IT BECAUSE ITS ABSENCE FROM THIS LIST LET A DEFECT SHIP. The page's <title>
	// was a constant outside the CanDeliver branch, so the running product served
	// <title>Check your email — Tappa</title> above <h1>Nothing was sent</h1>: the tab,
	// the history entry, the bookmark and any shared link all promised mail that was
	// never sent. This test read every byte of that document and asked only for the
	// presence of one string and the absence of another; neither mentioned the title.
	// A body scan is only as good as what it refuses.
	for _, promise := range []string{"on its way", "Check your email", "expires in an hour"} {
		if strings.Contains(body, promise) {
			t.Errorf("the answer contains %q — a deployment that cannot send a link must not "+
				"say that anywhere in the document, headings and <title> included", promise)
		}
	}
	// 🔴 AND NOTHING HAPPENED. This is the part that matters more than the wording:
	// issuing would have retired the administrator's pending links (ADR 0015's
	// accepted harm) in exchange for a link nobody can receive.
	if issue, _ := resets.calls(); issue != 0 {
		t.Errorf("IssueForEmail was called %d time(s) on a deployment with no channel; that "+
			"mints a token nobody can be given and kills any link that was already live", issue)
	}
}

// TestAdminReset_AFailedDeliveryIsRecordedAndNotShown holds both halves of the split
// this flow makes between a DEPLOYMENT-level failure and a PER-RECIPIENT one.
func TestAdminReset_AFailedDeliveryIsRecordedAndNotShown(t *testing.T) {
	t.Parallel()
	known := "owner@registered.example.test"
	resets := &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: {grantFor(known)}}}
	trail := &fakeTrail{}
	router, _ := newResetRouter(t, resets, &recordingChannel{err: errors.New("smtp: nope")}, trail)

	failed := requestReset(t, newBrowser(t, router), known)

	// The visitor is told nothing: a "we could not send it" screen appears only for
	// an address that HAS an administrator, which answers the enumeration question
	// exactly.
	ok := requestReset(t, newBrowser(t, router), "nobody@unregistered.example.test")
	if failed != ok {
		t.Error("a failed delivery produced a different page from an unregistered address, " +
			"which tells a stranger that the address is registered")
	}
	if n := trail.count(ActionAdminResetUndelivered); n != 1 {
		t.Errorf("%d undelivered row(s) in the trail, want 1 — the failure has to land "+
			"somewhere (§4.6) and the response is not allowed to be that place", n)
	}
	if n := trail.count(ActionAdminResetRequested); n != 0 {
		t.Errorf("%d requested row(s); a link that was not delivered must not be recorded as "+
			"one that was", n)
	}
}

// --- criterion 2: every failed attempt lands somewhere ---------------------------

// TestAdminReset_EveryFailedAttemptLandsSomewhere is criterion 2, in the two shapes
// internal/adminauth/reset.go says are the only ones available.
func TestAdminReset_EveryFailedAttemptLandsSomewhere(t *testing.T) {
	t.Parallel()
	tenantID, adminID, resetID := uuid.New(), uuid.New(), uuid.New()
	used := time.Now().Add(-time.Minute)

	t.Run("a link that RESOLVED writes an attributable audit row", func(t *testing.T) {
		t.Parallel()
		resets := &fakeResets{
			consumeErr: adminauth.ErrResetUnusable,
			consumeResolved: db.ResolvedPasswordReset{
				ID: resetID, TenantID: tenantID, AdminUserID: adminID,
				ExpiresAt: time.Now().Add(time.Hour), UsedAt: &used,
			},
		}
		trail := &fakeTrail{}
		router, _ := newResetRouter(t, resets, &recordingChannel{}, trail)
		b := newBrowser(t, router)
		submitLink(t, b, "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8", "a-good-enough-password")

		events := trail.eventsSnapshot()
		if len(events) != 1 {
			t.Fatalf("%d audit row(s), want 1", len(events))
		}
		e := events[0]
		if e.Action != ActionAdminResetRefused {
			t.Errorf("action = %q, want %q", e.Action, ActionAdminResetRefused)
		}
		if e.TenantID != tenantID {
			t.Errorf("row written into tenant %s, want the one the link resolved to (%s)", e.TenantID, tenantID)
		}
		d, ok := e.Detail.(adminResetDetail)
		if !ok {
			t.Fatalf("detail is %T, want the purpose-built struct", e.Detail)
		}
		if !d.AlreadyUsed {
			t.Error("the row does not say the link had already been spent, which is the one " +
				"fact that tells a replay from a typo")
		}
	})

	t.Run("a link that did NOT resolve cannot be an audit row", func(t *testing.T) {
		t.Parallel()
		resets := &fakeResets{consumeErr: adminauth.ErrResetUnusable}
		trail := &fakeTrail{}
		router, _ := newResetRouter(t, resets, &recordingChannel{}, trail)
		b := newBrowser(t, router)
		submitLink(t, b, "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8", "a-good-enough-password")

		if n := len(trail.eventsSnapshot()); n != 0 {
			t.Errorf("%d audit row(s) for a link with no tenant; audit_log.tenant_id is NOT "+
				"NULL with an FK (00005), so such a row can only have been written into a "+
				"tenant the request never proved anything about", n)
		}
	})
}

// TestAdminReset_TheRefusalIsOneScreenWhateverWentWrong is
// internal/adminauth.ErrResetUnusable's promise, held at the HTTP layer.
func TestAdminReset_TheRefusalIsOneScreenWhateverWentWrong(t *testing.T) {
	t.Parallel()
	now := time.Now()
	past := now.Add(-time.Hour)
	cases := []struct {
		name     string
		resolved db.ResolvedPasswordReset
	}{
		{"unknown link", db.ResolvedPasswordReset{}},
		{"already spent", db.ResolvedPasswordReset{ID: uuid.New(), TenantID: uuid.New(), AdminUserID: uuid.New(), ExpiresAt: now.Add(time.Hour), UsedAt: &past}},
		{"expired", db.ResolvedPasswordReset{ID: uuid.New(), TenantID: uuid.New(), AdminUserID: uuid.New(), ExpiresAt: past}},
		{"superseded", db.ResolvedPasswordReset{ID: uuid.New(), TenantID: uuid.New(), AdminUserID: uuid.New(), ExpiresAt: now.Add(time.Hour), CancelledAt: &past}},
	}
	var bodies []string
	var codes []int
	for _, tc := range cases {
		resets := &fakeResets{consumeErr: adminauth.ErrResetUnusable, consumeResolved: tc.resolved}
		router, _ := newResetRouter(t, resets, &recordingChannel{}, &fakeTrail{})
		rec := submitLink(t, newBrowser(t, router), "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8", "a-good-enough-password")
		bodies = append(bodies, htmlOf(t, rec))
		codes = append(codes, rec.Code)
	}
	for i := range cases {
		if bodies[i] != bodies[0] {
			t.Errorf("%q renders a different page from %q", cases[i].name, cases[0].name)
		}
		if codes[i] != codes[0] {
			t.Errorf("%q answers %d and %q answers %d", cases[i].name, codes[i], cases[0].name, codes[0])
		}
	}
	// ANTI-VACUITY: the four bodies are equal because they are the same SCREEN, not
	// because they are empty.
	if !strings.Contains(bodies[0], "no longer works") {
		t.Fatalf("the refusal screen is not the one this test thinks it is:\n%s", bodies[0])
	}
}

// --- T5: the successful path ends at the sign-in form ---------------------------

// TestAdminReset_SuccessSendsThePersonToSignIn holds the consequence of Consume
// revoking the resetter's OWN sessions: there is nowhere else it could send them.
func TestAdminReset_SuccessSendsThePersonToSignIn(t *testing.T) {
	t.Parallel()
	tenantID, adminID := uuid.New(), uuid.New()
	resets := &fakeResets{consumed: adminauth.ConsumedReset{
		ResetID: uuid.New(), TenantID: tenantID, AdminUserID: adminID,
		RetiredCount: 1, RevokedSessions: 3,
	}}
	trail := &fakeTrail{}
	router, _ := newResetRouter(t, resets, &recordingChannel{}, trail)
	b := newBrowser(t, router)
	rec := submitLink(t, b, "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8", "a-good-enough-password")

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, adminLoginPath) {
		t.Errorf("Location = %q, want the sign-in form: every session was just revoked, "+
			"including this browser's, so anywhere inside the panel answers 303 anyway", loc)
	}
	if _, ok := b.cookies[adminResetLinkCookieName]; ok {
		t.Error("the spent link is still in the browser")
	}
	if n := trail.count(ActionAdminResetCompleted); n != 1 {
		t.Errorf("%d completed row(s), want 1", n)
	}
	for _, e := range trail.eventsSnapshot() {
		d, ok := e.Detail.(adminResetDetail)
		if !ok {
			continue
		}
		if d.RevokedSessions != 3 {
			t.Errorf("the trail records %d revoked session(s), want 3 — the number is what an "+
				"investigation reads to know the takeover's cookies died with the password",
				d.RevokedSessions)
		}
	}
}

// --- the budgets ----------------------------------------------------------------

// TestAdminResetBudgets_RefuseAtTheCeilingAndDoNotShareABucket drives the REAL
// budgets on the REAL router.
//
// 🔴 THE SECOND HALF IS THE POINT. Sharing one bucket between the two endpoints
// would let somebody who spammed the REQUEST form stop a victim from FINISHING a
// recovery, which is harm (a) reached by a second route.
func TestAdminResetBudgets_RefuseAtTheCeilingAndDoNotShareABucket(t *testing.T) {
	t.Parallel()
	resets := &fakeResets{consumeErr: adminauth.ErrResetUnusable}
	router, _ := newResetRouter(t, resets, &recordingChannel{}, &fakeTrail{})
	b := newBrowser(t, router)

	page := b.do(http.MethodGet, adminResetPath, nil)
	csrf := csrfFrom(t, htmlOf(t, page))
	served, refused := 0, 0
	for i := 0; i < adminResetRequestLimit+1; i++ {
		rec := b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {"x@y.test"}})
		switch rec.Code {
		case http.StatusOK:
			served++
		case http.StatusTooManyRequests:
			refused++
		default:
			t.Fatalf("request %d answered %d", i, rec.Code)
		}
	}
	if served != adminResetRequestLimit || refused != 1 {
		t.Errorf("%d served + %d refused, want %d + 1", served, refused, adminResetRequestLimit)
	}

	// The submit budget is untouched by all of that: the same address can still
	// finish a recovery.
	if rec := submitLink(t, b, "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8", "a-good-enough-password"); rec.Code == http.StatusTooManyRequests {
		t.Error("spending the request budget also refused a submission; the two share a " +
			"bucket, so flooding the public form denies somebody their recovery")
	}
}

// TestAdminResetSubmit_BudgetBoundsTheBcrypt is harm (b): every submission costs a
// full cost-12 digest before the link is even looked up.
func TestAdminResetSubmit_BudgetBoundsTheBcrypt(t *testing.T) {
	t.Parallel()
	resets := &fakeResets{consumeErr: adminauth.ErrResetUnusable}
	router, _ := newResetRouter(t, resets, &recordingChannel{}, &fakeTrail{})
	b := newBrowser(t, router)

	refused := 0
	for i := 0; i < adminResetSubmitLimit+3; i++ {
		if rec := submitLink(t, b, "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8", "a-good-enough-password"); rec.Code == http.StatusTooManyRequests {
			refused++
		}
	}
	if refused != 3 {
		t.Errorf("%d refusal(s) past the ceiling, want 3", refused)
	}
	// AND THE REFUSALS COST NO DIGEST, which is the entire point of the budget.
	if _, consume := resets.calls(); consume != adminResetSubmitLimit {
		t.Errorf("Consume ran %d time(s), want %d — a refused submission must not reach the "+
			"digest computation it exists to bound", consume, adminResetSubmitLimit)
	}
}

// TestAdminResetSubmit_AMistypedConfirmationCostsNoDigest is why the form's own
// checks run before Consume.
func TestAdminResetSubmit_AMistypedConfirmationCostsNoDigest(t *testing.T) {
	t.Parallel()
	resets := &fakeResets{}
	router, _ := newResetRouter(t, resets, &recordingChannel{}, &fakeTrail{})
	b := newBrowser(t, router)
	openLink(t, b, "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8")
	form := htmlOf(t, b.do(http.MethodGet, adminResetNewPath, nil))
	csrf := csrfFrom(t, form)

	cases := []struct {
		name              string
		password, confirm string
	}{
		{"empty", "", ""},
		{"too short", "Zq7", "Zq7"},
		{"mismatched", "a-good-enough-password", "a-good-enough-passwerd"},
		{"over 72 bytes", strings.Repeat("y", 73), strings.Repeat("y", 73)},
	}
	for _, tc := range cases {
		rec := b.do(http.MethodPost, adminResetNewPath, url.Values{
			"csrf": {csrf}, "password": {tc.password}, "password_confirm": {tc.confirm},
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: code = %d, want 422", tc.name, rec.Code)
		}
		if body := htmlOf(t, rec); strings.Contains(body, tc.password) && tc.password != "" {
			t.Errorf("%s: the refused password is echoed back into the page", tc.name)
		}
	}
	if _, consume := resets.calls(); consume != 0 {
		t.Errorf("Consume ran %d time(s) for %d invalid submissions; each one is a cost-12 "+
			"bcrypt on a page anybody can reach", consume, len(cases))
	}
}

// --- CSRF and Origin ------------------------------------------------------------

func TestAdminReset_BothPostsRefuseCrossOriginAndAMismatchedToken(t *testing.T) {
	t.Parallel()
	resets := &fakeResets{}
	router, _ := newResetRouter(t, resets, &recordingChannel{}, &fakeTrail{})

	t.Run("request form", func(t *testing.T) {
		b := newBrowser(t, router)
		csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
		b.origin = "https://evil.example"
		if rec := b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {"x@y.test"}}); rec.Code != http.StatusBadRequest {
			t.Errorf("cross-origin POST answered %d, want 400", rec.Code)
		}
		b.origin = testBaseURL
		if rec := b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {"wrong"}, "email": {"x@y.test"}}); rec.Code != http.StatusBadRequest {
			t.Errorf("mismatched token answered %d, want 400", rec.Code)
		}
	})

	t.Run("new password form", func(t *testing.T) {
		b := newBrowser(t, router)
		openLink(t, b, "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8")
		csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetNewPath, nil)))
		b.origin = "https://evil.example"
		if rec := b.do(http.MethodPost, adminResetNewPath, url.Values{"csrf": {csrf}, "password": {"a-good-enough-password"}, "password_confirm": {"a-good-enough-password"}}); rec.Code != http.StatusBadRequest {
			t.Errorf("cross-origin POST answered %d, want 400", rec.Code)
		}
		b.origin = testBaseURL
		if rec := b.do(http.MethodPost, adminResetNewPath, url.Values{"csrf": {"wrong"}, "password": {"a-good-enough-password"}, "password_confirm": {"a-good-enough-password"}}); rec.Code != http.StatusBadRequest {
			t.Errorf("mismatched token answered %d, want 400", rec.Code)
		}
	})

	if _, consume := resets.calls(); consume != 0 {
		t.Errorf("Consume ran %d time(s) behind a refused request", consume)
	}
}

// TestAdminResetNew_TakesTheTokenOutOfTheURL is the first hop of the link, and the
// header internal/adminauth/reset.go asks this page to send.
func TestAdminResetNew_TakesTheTokenOutOfTheURL(t *testing.T) {
	t.Parallel()
	router, _ := newResetRouter(t, &fakeResets{}, &recordingChannel{}, &fakeTrail{})
	b := newBrowser(t, router)
	const token = "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8"

	rec := b.do(http.MethodGet, adminResetNewPath+"?t="+token, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != adminResetNewPath {
		t.Errorf("Location = %q, want the clean path %q", got, adminResetNewPath)
	}
	if rec.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Errorf("Referrer-Policy = %q; internal/adminauth/reset.go requires no-referrer on "+
			"the one page whose URL can carry the token", rec.Header().Get("Referrer-Policy"))
	}
	if b.cookies[adminResetLinkCookieName] == "" {
		t.Fatal("no link cookie was set, so the token was dropped rather than carried")
	}
	if !strings.HasSuffix(b.cookies[adminResetLinkCookieName], "."+token) {
		t.Error("the link cookie does not carry the token in the shape the POST reads")
	}
	if form := b.do(http.MethodGet, adminResetNewPath, nil); form.Code != http.StatusOK {
		t.Errorf("the clean URL answered %d, want 200", form.Code)
	}
}

// TestAdminResetNew_MakesNoQueryBeforeThePasswordIsSubmitted holds the deliberate
// absence: checking the link on GET would answer "does this link exist" for free.
func TestAdminResetNew_MakesNoQueryBeforeThePasswordIsSubmitted(t *testing.T) {
	t.Parallel()
	resets := &fakeResets{}
	router, _ := newResetRouter(t, resets, &recordingChannel{}, &fakeTrail{})
	b := newBrowser(t, router)
	openLink(t, b, "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8")
	b.do(http.MethodGet, adminResetNewPath, nil)
	if issue, consume := resets.calls(); issue != 0 || consume != 0 {
		t.Errorf("opening the link cost %d issue and %d consume call(s); a GET that touches "+
			"the database answers whether the link exists, in microseconds, for free",
			issue, consume)
	}
}

// --- the shipped numbers --------------------------------------------------------

// TestAdminResetConstants_ShippedValuesArePinned. Not ceremony: adminresetlimits.go
// is an argument about these numbers, and every test above is written in terms of
// the constants, so their values are free to change without a single failure. They
// are the product decision, so they are pinned as literals here.
func TestAdminResetConstants_ShippedValuesArePinned(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		got  int
		want int
		why  string
	}{
		{"adminResetRequestLimit", adminResetRequestLimit, 20,
			"10 admins behind one NAT x2 for the retry a person actually makes; it is also the number of pending links one address can extinguish per window"},
		{"adminResetSubmitLimit", adminResetSubmitLimit, 10,
			"10 x ~213 ms of cost-12 bcrypt = ~2.1 s of CPU per window per address"},
		{"adminResetAccountLimit", adminResetAccountLimit, 10,
			"audit_log only; it never refuses a request (see adminAccountLimit)"},
		{"adminResetUnknownLimit", adminResetUnknownLimit, 60,
			"process log only; ratelimit.go's number for the same job"},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %d, want %d.\nRe-derive it rather than editing this line: %s",
				tc.name, tc.got, tc.want, tc.why)
		}
	}
	if resetRequestFloor != 250*time.Millisecond {
		t.Errorf("resetRequestFloor = %v, want 250ms; it is roughly 4x the measured median "+
			"of the slowest arm of POST /admin/reset (a full adminauth.ResetWindow, 60-64 ms) "+
			"and every millisecond is a held goroutine. The measurement and its three arms "+
			"are at resetRequestFloor; do not restate a multiple here that drifts from it.",
			resetRequestFloor)
	}
	if adminauth.ResetWindow != adminauth.MaxCandidates {
		t.Errorf("adminauth.ResetWindow = %d and MaxCandidates = %d; a recovery link outside "+
			"the sign-in window cannot restore a sign-in, and one inside it must not be refused",
			adminauth.ResetWindow, adminauth.MaxCandidates)
	}
}

// --- helpers --------------------------------------------------------------------

// openLink drives the first hop: the URL the link points at, which plants the cookie.
func openLink(t *testing.T, b *browser, token string) {
	t.Helper()
	if rec := b.do(http.MethodGet, adminResetNewPath+"?t="+token, nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("opening the link answered %d, want 303", rec.Code)
	}
}

// submitLink drives the whole set-a-new-password form.
func submitLink(t *testing.T, b *browser, token, password string) *httptest.ResponseRecorder {
	t.Helper()
	openLink(t, b, token)
	form := b.do(http.MethodGet, adminResetNewPath, nil)
	if form.Code != http.StatusOK {
		t.Fatalf("the form answered %d, want 200", form.Code)
	}
	csrf := csrfFrom(t, htmlOf(t, form))
	return b.do(http.MethodPost, adminResetNewPath, url.Values{
		"csrf": {csrf}, "password": {password}, "password_confirm": {password},
	})
}

// --- the audit budgets: what may be silenced, and by whom -----------------------

// TestAdminReset_ACompletedRecoveryIsNeverSilenced is the regression for the one
// BLOCKING finding of round 1, and it is written the way the exploit was run.
//
// 🔴 THE DEFECT: every audit row in this flow went through the per-account budget, so
// eleven anonymous POSTs to the PUBLIC request form burnt it and the next completed
// reset — password written, every session revoked — wrote NOTHING. A security audit
// printed exactly that (10 requested + 1 rate_limited, then 0 completed) on the real
// router. §4.6's core is that a record is never lost, and the precedent refuting the
// design was already in this package: adminlogin.go writes ActionAdminLoginSucceeded
// with a direct record call and budgets only its FAILURE loop.
//
// THE ANONYMOUS HALF IS DRIVEN THROUGH THE REAL ROUTER, not by poking the limiter,
// because "an anonymous caller can spend this counter" is the whole claim.
func TestAdminReset_ACompletedRecoveryIsNeverSilenced(t *testing.T) {
	t.Parallel()
	victim := "victim@registered.example.test"
	g := grantFor(victim)
	tenantID, adminID := g.Issued.Reset.TenantID, g.Issued.Reset.AdminUserID

	resets := &fakeResets{
		grantsFor: map[string][]adminauth.ResetGrant{victim: {g}},
		consumed: adminauth.ConsumedReset{
			ResetID: uuid.New(), TenantID: tenantID, AdminUserID: adminID,
			RetiredCount: 0, RevokedSessions: 2,
		},
	}
	trail := &fakeTrail{}
	router, _ := newResetRouter(t, resets, &recordingChannel{}, trail)

	// STEP 1 — burn the account budget from the public form. One MORE than the
	// ceiling, which is what makes the suppression row appear, and still well under
	// adminResetRequestLimit so nothing is refused.
	b := newBrowser(t, router)
	page := b.do(http.MethodGet, adminResetPath, nil)
	csrf := csrfFrom(t, htmlOf(t, page))
	for i := 0; i < adminResetAccountLimit+1; i++ {
		if rec := b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {victim}}); rec.Code != http.StatusOK {
			t.Fatalf("anonymous request %d answered %d, want 200 — this test needs the budget "+
				"spent by SERVED requests, not by refused ones", i, rec.Code)
		}
	}
	if n := trail.count(ActionAdminResetRequested); n != adminResetAccountLimit {
		t.Fatalf("%d requested row(s), want %d — the account budget did not trip and the "+
			"suppression this test is about never happened", n, adminResetAccountLimit)
	}
	if n := trail.count(ActionAdminResetLimited); n != 1 {
		t.Fatalf("%d rate_limited row(s), want 1", n)
	}

	// STEP 2 — a genuine, successful recovery from the same account, in a fresh
	// browser (a different person, holding the link).
	rec := submitLink(t, newBrowser(t, router), "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8", "a-good-enough-password")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("the recovery answered %d, want 303 — it has to SUCCEED for this test to be "+
			"about the trail rather than about the refusal", rec.Code)
	}
	if _, consume := resets.calls(); consume != 1 {
		t.Fatalf("Consume ran %d time(s), want 1", consume)
	}

	if n := trail.count(ActionAdminResetCompleted); n != 1 {
		t.Errorf("%d completed row(s) in the trail, want 1.\n"+
			"A password was written and every session revoked, and %d anonymous requests "+
			"from a public form were enough to make it leave no trace. §4.6: kayit asla "+
			"kaybolmaz.", n, adminResetAccountLimit+1)
	}
}

// TestAdminReset_ARefusedLinkIsBudgetedOnTheLinkAndNotOnTheAccount holds the OTHER
// half of the same decision, in both directions.
func TestAdminReset_ARefusedLinkIsBudgetedOnTheLinkAndNotOnTheAccount(t *testing.T) {
	t.Parallel()
	victim := "replayed@registered.example.test"
	g := grantFor(victim)
	tenantID, adminID := g.Issued.Reset.TenantID, g.Issued.Reset.AdminUserID
	spent := time.Now().Add(-time.Minute)
	resolved := db.ResolvedPasswordReset{
		ID: uuid.New(), TenantID: tenantID, AdminUserID: adminID,
		ExpiresAt: time.Now().Add(time.Hour), UsedAt: &spent,
	}

	t.Run("a stranger flooding the public form cannot silence it", func(t *testing.T) {
		t.Parallel()
		resets := &fakeResets{
			grantsFor:       map[string][]adminauth.ResetGrant{victim: {g}},
			consumeErr:      adminauth.ErrResetUnusable,
			consumeResolved: resolved,
		}
		trail := &fakeTrail{}
		router, _ := newResetRouter(t, resets, &recordingChannel{}, trail)

		b := newBrowser(t, router)
		csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
		for i := 0; i < adminResetAccountLimit+1; i++ {
			b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {victim}})
		}

		submitLink(t, newBrowser(t, router), "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8", "a-good-enough-password")
		if n := trail.count(ActionAdminResetRefused); n != 1 {
			t.Errorf("%d refused row(s), want 1. A replayed link is the takeover signal for "+
				"this account, and a stranger who holds no link just silenced it by typing "+
				"an address into a public form.", n)
		}
	})

	t.Run("replaying ONE link is bounded, and says which counter tripped", func(t *testing.T) {
		t.Parallel()
		resets := &fakeResets{consumeErr: adminauth.ErrResetUnusable, consumeResolved: resolved}
		trail := &fakeTrail{}
		router, _ := newResetRouter(t, resets, &recordingChannel{}, trail)

		// Each replay from its OWN address, so the per-address submit budget cannot be
		// what stops the writing — this subtest is about the AUDIT budget.
		for i := 0; i < adminResetLinkLimit+3; i++ {
			b := newBrowser(t, router)
			b.ip = "198.51.100." + strconv.Itoa(i+1) + ":9000"
			submitLink(t, b, "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8", "a-good-enough-password")
		}
		if n := trail.count(ActionAdminResetRefused); n != adminResetLinkLimit {
			t.Errorf("%d refused row(s), want %d — a dead link can be re-submitted forever "+
				"and each replay would otherwise append a permanent row", n, adminResetLinkLimit)
		}
		if n := trail.count(ActionAdminResetLimited); n != 1 {
			t.Fatalf("%d rate_limited row(s), want exactly 1", n)
		}
		for _, e := range trail.eventsSnapshot() {
			if e.Action != ActionAdminResetLimited {
				continue
			}
			d, ok := e.Detail.(adminResetDetail)
			if !ok {
				t.Fatalf("detail is %T", e.Detail)
			}
			if d.Scope != "link" {
				t.Errorf("the suppression row says scope %q, want \"link\" — there are two "+
					"counters and they silence different things, so a row that does not say "+
					"which one tripped tells an investigator less than it looks like it does",
					d.Scope)
			}
		}
	})
}

// TestAdminResetSubmit_FailsClosedWhenNothingCanDeliver is the second half of the
// short-circuit Request already had.
//
// 🔴 WITHOUT IT THE PRODUCT CARRIES AN UNAUTHENTICATED CPU SURFACE FOR A FLOW THAT
// CANNOT SUCCEED: no channel means no link was ever issued, yet every POST here paid
// a full cost-12 digest (~213 ms) before discovering there was nothing to spend it
// on.
func TestAdminResetSubmit_FailsClosedWhenNothingCanDeliver(t *testing.T) {
	t.Parallel()
	resets := &fakeResets{}
	router, _ := newResetRouter(t, resets, nil, &fakeTrail{})
	b := newBrowser(t, router)

	opened := b.do(http.MethodGet, adminResetNewPath+"?t=qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8", nil)
	if opened.Code != http.StatusNotFound {
		t.Errorf("opening a link answered %d, want 404 — no link can exist on this deployment", opened.Code)
	}
	if _, ok := b.cookies[adminResetLinkCookieName]; ok {
		t.Error("the page planted a cookie carrying an attacker-supplied value on a flow that " +
			"cannot succeed")
	}
	body := htmlOf(t, opened)
	if !strings.Contains(body, "cannot send recovery links") {
		t.Errorf("the refusal does not say why:\n%s", body)
	}
	// The POST too, driven directly since the GET refuses to set up the form.
	posted := b.do(http.MethodPost, adminResetNewPath, url.Values{
		"csrf": {"whatever"}, "password": {"a-good-enough-password"}, "password_confirm": {"a-good-enough-password"},
	})
	if posted.Code != http.StatusNotFound {
		t.Errorf("submitting answered %d, want 404", posted.Code)
	}
	if _, consume := resets.calls(); consume != 0 {
		t.Errorf("Consume ran %d time(s) on a deployment where no link can exist; each call is "+
			"a full cost-12 digest bought by an anonymous caller", consume)
	}
}

// --- the recovery-link cookie's scope -------------------------------------------

// cookiePathMatches is RFC 6265 section 5.1.4's path-match algorithm: would a browser
// holding a cookie at cookiePath send it with a request for requestPath?
//
// IT IS WRITTEN OUT RATHER THAN ASSERTED BY STRING EQUALITY because the property that
// matters is "which requests carry this credential", and only the algorithm answers
// that. A test comparing the Path field to a constant passes just as happily when the
// constant is widened, which is exactly what two mutations proved: widening the path
// back to /admin, and clearing at a path that does not match the one it was set at,
// both left every earlier test in this file GREEN.
func cookiePathMatches(cookiePath, requestPath string) bool {
	if requestPath == cookiePath {
		return true
	}
	if !strings.HasPrefix(requestPath, cookiePath) {
		return false
	}
	if strings.HasSuffix(cookiePath, "/") {
		return true
	}
	return strings.HasPrefix(requestPath[len(cookiePath):], "/")
}

// TestAdminResetLinkCookie_TravelsOnlyWithThePageThatSpendsIt is §4.7 applied to the
// one cookie in this product that carries a bearer credential.
//
// 🔴 THE POSITIVE CONTROL IS THE OTHER COOKIE THE SAME FLOW SETS, and it is
// independent of this test's own patterns: the synchronizer cookie is written by
// production code at the ordinary panel path, so it MUST match /admin. If it does
// not, the algorithm above is wrong and every refusal it reports below is worthless.
func TestAdminResetLinkCookie_TravelsOnlyWithThePageThatSpendsIt(t *testing.T) {
	t.Parallel()
	router, _ := newResetRouter(t, &fakeResets{}, &recordingChannel{}, &fakeTrail{})
	const token = "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8"

	// The real Set-Cookie the real route writes, read off the response rather than
	// taken from a constant.
	plant := newBrowser(t, router).do(http.MethodGet, adminResetNewPath+"?t="+token, nil)
	var linkCookie, syncCookie *http.Cookie
	for _, ck := range plant.Result().Cookies() {
		if ck.Name == adminResetLinkCookieName {
			linkCookie = ck
		}
	}
	form := newBrowser(t, router).do(http.MethodGet, adminResetPath, nil)
	for _, ck := range form.Result().Cookies() {
		if ck.Name == adminResetCookieName {
			syncCookie = ck
		}
	}
	if linkCookie == nil || syncCookie == nil {
		t.Fatalf("link cookie set: %v, synchronizer cookie set: %v — this test needs both",
			linkCookie != nil, syncCookie != nil)
	}

	// THE CONTROL, from production's own value: the synchronizer cookie is meant to be
	// panel-wide and must reach the panel.
	if !cookiePathMatches(syncCookie.Path, "/admin") {
		t.Fatalf("the synchronizer cookie at Path=%q does not match /admin, so the path-match "+
			"algorithm in this test is wrong and its refusals below prove nothing", syncCookie.Path)
	}

	// AND THE LINK COOKIE REACHES EXACTLY ONE ROUTE.
	for _, reach := range []string{adminResetNewPath} {
		if !cookiePathMatches(linkCookie.Path, reach) {
			t.Errorf("the recovery link cookie at Path=%q would NOT be sent to %s, which is the "+
				"one page that has to read it", linkCookie.Path, reach)
		}
	}
	for _, keepOut := range []string{"/admin", "/admin/login", "/admin/dockets", "/admin/reset", "/admin/employees"} {
		if cookiePathMatches(linkCookie.Path, keepOut) {
			t.Errorf("the recovery link cookie at Path=%q rides along on %s. It holds the RAW "+
				"recovery token for %v, so widening its scope multiplies the requests, proxies "+
				"and access logs a §4.7 secret can appear in, for no benefit — nothing outside "+
				"%s ever reads it.",
				linkCookie.Path, keepOut, adminauth.ResetTTL, adminResetNewPath)
		}
	}

	// THE DELETION TRAP: a browser matches Set-Cookie on name AND path, so a clear at a
	// different path leaves the credential in place until it expires on its own.
	b := newBrowser(t, router)
	openLink(t, b, token)
	page := b.do(http.MethodGet, adminResetNewPath, nil)
	csrf := csrfFrom(t, htmlOf(t, page))
	cleared := b.do(http.MethodPost, adminResetNewPath, url.Values{
		"csrf": {csrf}, "password": {"a-good-enough-password"}, "password_confirm": {"a-good-enough-password"},
	})
	var clear *http.Cookie
	for _, ck := range cleared.Result().Cookies() {
		if ck.Name == adminResetLinkCookieName {
			clear = ck
		}
	}
	if clear == nil {
		t.Fatal("the spent link was never cleared")
	}
	if clear.MaxAge >= 0 {
		t.Errorf("the clearing cookie has MaxAge %d, want a negative one", clear.MaxAge)
	}
	if clear.Path != linkCookie.Path {
		t.Errorf("the cookie is SET at Path=%q and CLEARED at Path=%q. A browser matches on "+
			"name AND path, so this deletes nothing and the recovery token stays in the "+
			"browser for its full %v.", linkCookie.Path, clear.Path, adminauth.ResetTTL)
	}
}

// --- criterion 2's process-log arm ----------------------------------------------

// newResetRouterLogging is newResetRouter with a REAL text handler over a buffer, so
// a test can read what the flow wrote rather than what it says it writes.
func newResetRouterLogging(t *testing.T, resets panelResets, mail ResetChannel, logged *bytes.Buffer) http.Handler {
	t.Helper()
	h, err := NewAdminReset(resets, mail, &fakeTrail{}, adminTestConfig(),
		slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

// TestAdminReset_AnAttemptOnAnUndeliverableDeploymentStillLandsSomewhere is the
// regression for the round-3 blocking finding.
//
// 🔴 THE DEFECT WAS A LEDGER ENTRY, NOT A CRASH. The type comment counted criterion 2
// as "the audit_log arm is dead, the process-log arm IS live" — and the fail-closed
// short-circuit added one round earlier had killed the process-log arm too, by
// returning before both of the call sites that write one. Measured on the real binary
// with TAPPA_LOG_LEVEL=debug: four requests, two of them carrying a 43-character
// value, produced ZERO log lines while two documents claimed otherwise. Declaring a
// guarantee the product does not provide is this repository's signature defect, and
// the counted-gaps ledger is the one place whose only job is to be accurate.
func TestAdminReset_AnAttemptOnAnUndeliverableDeploymentStillLandsSomewhere(t *testing.T) {
	t.Parallel()
	const token = "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8"
	var logged bytes.Buffer
	router := newResetRouterLogging(t, &fakeResets{}, nil, &logged)
	b := newBrowser(t, router)

	if rec := b.do(http.MethodGet, adminResetNewPath+"?t="+token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("GET answered %d, want 404", rec.Code)
	}
	// THE POST MUST CARRY A LINK to be a presentation at all — a bare POST is a browse
	// and writes nothing on purpose (TestAdminReset_TheUndeliverableLineClaimsOnly...).
	b.cookies[adminResetLinkCookieName] = "abc." + token
	if rec := b.do(http.MethodPost, adminResetNewPath, url.Values{"csrf": {"x"}}); rec.Code != http.StatusNotFound {
		t.Fatalf("POST answered %d, want 404", rec.Code)
	}

	got := logged.String()
	for _, where := range []string{"admin_reset_open", "admin_reset_submit"} {
		if !strings.Contains(got, where) {
			t.Errorf("presenting a recovery link at %s left NO trace.\n"+
				"A 404 here does not mean 'no such feature': the route is mounted, the "+
				"value's shape is inspected and anybody can call it without a credential. "+
				"§4.6 — kayit asla kaybolmaz.\nLOG:\n%s", where, got)
		}
	}
	// AND THE LINE CARRIES NEITHER THE VALUE NOR AN ADDRESS (§4.7).
	if strings.Contains(got, token) {
		t.Error("the process log carries the raw value that was presented")
	}

	// IT IS BOUNDED. The same budget the other two process-log writers use, which is
	// safe to share because the three are mutually exclusive per process — see
	// logUndeliverableAttempt.
	before := strings.Count(logged.String(), "admin_reset_open")
	for i := 0; i < adminResetUnknownLimit+10; i++ {
		b.do(http.MethodGet, adminResetNewPath+"?t="+token, nil)
	}
	after := strings.Count(logged.String(), "admin_reset_open")
	if written := after - before; written > adminResetUnknownLimit {
		t.Errorf("%d further lines for %d requests; past adminResetUnknownLimit (%d) this "+
			"branch has to stop writing, or an anonymous caller owns the disk",
			written, adminResetUnknownLimit+10, adminResetUnknownLimit)
	} else if written == 0 {
		t.Error("the budget was already spent before the loop began, so this bound measured nothing")
	}
}

// TestAdminResetNew_RefusesBytesThatCannotGoInACookie closes a channel net/http would
// otherwise open for us.
//
// 🔴 MEASURED ON go1.26's OWN SOURCE AND BY DRIVING IT: http.SetCookie hands the value
// to sanitizeOrWarn, which writes ONE line to the STANDARD logger (the loop breaks at
// the first bad byte — so per REQUEST, not per byte as an audit reported) and then
// SILENTLY STRIPS the offending bytes. Two separate problems: the line bypasses this
// product's slog handler entirely, so no level and no budget can bound it; and the
// stored cookie holds a DIFFERENT value from the one the link carried.
func TestAdminResetNew_RefusesBytesThatCannotGoInACookie(t *testing.T) {
	t.Parallel()
	router, _ := newResetRouter(t, &fakeResets{}, &recordingChannel{}, &fakeTrail{})

	cases := map[string]string{
		"a NUL byte":     "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7Yy\x00CJ8",
		"a semicolon":    "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7Yy;CJ8",
		"a double quote": "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7Yy\"CJ8",
		"a backslash":    "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7Yy\\CJ8",
		"a newline":      "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7Yy\nCJ8",
		"a high byte":    "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7Yy\xffCJ8",
	}
	for name, bad := range cases {
		b := newBrowser(t, router)
		rec := b.do(http.MethodGet, adminResetNewPath+"?t="+url.QueryEscape(bad), nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: answered %d, want 400", name, rec.Code)
		}
		for _, ck := range rec.Result().Cookies() {
			if ck.Name == adminResetLinkCookieName && ck.MaxAge > 0 {
				t.Errorf("%s: a cookie was planted with value %q — net/http silently strips "+
					"what it cannot store, so the cookie now holds a value the link never "+
					"carried", name, ck.Value)
			}
		}
	}

	// POSITIVE CONTROL: the alphabet adminauth actually mints in. newResetToken encodes
	// 32 random bytes as unpadded base64url, so a real link carries only [A-Za-z0-9_-]
	// — every byte of which cookieSafe accepts. If this arm ever went red the refusals
	// above would be refusing everything and proving nothing.
	b := newBrowser(t, router)
	if rec := b.do(http.MethodGet, adminResetNewPath+"?t=qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8", nil); rec.Code != http.StatusSeeOther {
		t.Fatalf("a well-formed base64url value answered %d, want 303 — the refusals above "+
			"would then be refusing everything and proving nothing", rec.Code)
	}
	if b.cookies[adminResetLinkCookieName] == "" {
		t.Error("the well-formed value planted no cookie")
	}
}

// TestAdminResetBudgets_TheProcessLogBudgetCoversItsTwoCoLiveConsumers pins the
// arithmetic that keeps two simultaneously-live process-log writers out of each
// other's way.
//
// 🔴 IT EXISTS BECAUSE A COMMENT WAS DOING THIS JOB AND WAS WRONG. That comment said
// the three consumers of unknownLimiter are "mutually exclusive per process"; two
// auditors refuted it independently — Request's no-candidate branch and refused's
// unresolved branch are BOTH live whenever a channel is configured, and they have
// shared the bucket since before the third consumer existed. What actually saves them
// is that their combined worst-case demand fits, and an audit mutated the three into
// simultaneous reachability and watched the WHOLE package stay green. Nothing was
// holding it. This is what holds it.
//
// THE FAILURE MESSAGE RE-DERIVES THE ARITHMETIC rather than quoting a verdict, so the
// next person changing a budget is told what to re-check instead of which line to
// edit.
func TestAdminResetBudgets_TheProcessLogBudgetCoversItsTwoCoLiveConsumers(t *testing.T) {
	t.Parallel()
	// Each consumer's demand per address per window is bounded by the gate in front
	// of it, so the worst case is the sum.
	demand := adminResetRequestLimit + adminResetSubmitLimit
	if demand > adminResetUnknownLimit {
		t.Errorf("the two CO-LIVE process-log writers can demand %d lines per window per "+
			"address and the shared budget is %d:\n"+
			"  Request's no-candidate branch  <= adminResetRequestLimit (%d)\n"+
			"  refused's unresolved branch    <= adminResetSubmitLimit  (%d)\n"+
			"  shared budget                     adminResetUnknownLimit (%d)\n"+
			"Past the budget one consumer silences the other, which is a §4.6 gap opened "+
			"by a rate limit — the class of the blocking finding this flow already shipped "+
			"once. Either raise adminResetUnknownLimit to at least %d, or give the two "+
			"consumers separate counters and say why here.",
			demand, adminResetUnknownLimit,
			adminResetRequestLimit, adminResetSubmitLimit, adminResetUnknownLimit, demand)
	}
	// AND THE TWO CONSUMERS REALLY ARE BOTH LIVE IN ONE DEPLOYMENT, WHICH IS WHAT MAKES
	// THE ARITHMETIC ABOVE LOAD-BEARING RATHER THAN DECORATIVE. This is the audit's own
	// measurement, driven from ONE address against a configured channel: the request
	// form's no-candidate branch and the submit path's unresolved branch both write,
	// into the same budget, in the same process.
	var logged bytes.Buffer
	router := newResetRouterLogging(t, &fakeResets{consumeErr: adminauth.ErrResetUnusable}, &recordingChannel{}, &logged)
	b := newBrowser(t, router)
	csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
	for i := 0; i < adminResetRequestLimit; i++ {
		b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {"nobody@unregistered.example.test"}})
	}
	for i := 0; i < adminResetSubmitLimit; i++ {
		submitLink(t, newBrowser(t, router), "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8", "a-good-enough-password")
	}
	got := logged.String()
	noCandidate := strings.Count(got, "no active admin for that address")
	unresolved := strings.Count(got, "link did not resolve")
	if noCandidate == 0 || unresolved == 0 {
		t.Fatalf("the two consumers are not both live in one deployment (%d no-candidate, "+
			"%d unresolved); if only one can run, the arithmetic above is guarding nothing "+
			"and this test is not measuring what it claims", noCandidate, unresolved)
	}
	if total := noCandidate + unresolved; total > adminResetUnknownLimit {
		t.Errorf("%d lines written (%d + %d) against a budget of %d — one consumer is "+
			"already eating the other's share", total, noCandidate, unresolved, adminResetUnknownLimit)
	}
	t.Logf("co-live consumers wrote %d + %d = %d lines into a budget of %d",
		noCandidate, unresolved, noCandidate+unresolved, adminResetUnknownLimit)
}

// TestAdminReset_ADeliveryFailureNeverLogsTheChannelsText is §4.7 on the one path
// where a value we do not control quotes values we do.
//
// 🔴 THE EXPLOIT IT REGRESSES: a security audit drove a channel returning an ordinary
// SMTP failure — one that quotes the recipient and the URL it was sending, which is
// what SMTP failures do — and lifted the RAW TOKEN and the RECIPIENT out of the real
// log line, two lines under a comment promising neither would be there.
//
// THE POSITIVE CONTROL IS THE ERROR ITSELF, built from production's own Link output
// rather than from a string this test invents: the scanner is pointed at the error
// text first and MUST find both secrets there. A scanner that cannot see them where
// they certainly are would report every log clean.
func TestAdminReset_ADeliveryFailureNeverLogsTheChannelsText(t *testing.T) {
	t.Parallel()
	const recipient = "leak-probe@probe.example"
	g := grantFor(recipient)
	resets := &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{recipient: {g}}}

	var logged bytes.Buffer
	h, err := NewAdminReset(resets, nil, &fakeTrail{}, adminTestConfig(),
		slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewAdminReset: %v", err)
	}
	link := g.Issued.Link(h.linkBase)
	token := strings.TrimPrefix(link, h.linkBase+"?t=")
	if token == link || token == "" {
		t.Fatalf("could not take the token out of %q", link)
	}
	// TWO ERROR SHAPES, AND THE SECOND IS WHAT MAKES THIS TEST DISCRIMINATE. The first
	// is the audit's: a verbatim SMTP failure quoting the address and the URL. A
	// substring scrub at this boundary — option (a) — cleans that one, so a test using
	// only it cannot tell a denylist from a structural guarantee, and would have
	// accepted the weaker fix. The second is what ORDINARY mail software also does:
	// upper-case the address it is complaining about, and echo the request body back
	// base64-encoded. Measured over six such shapes, a scrub leaks on two.
	shapes := map[string]error{
		"verbatim": errors.New("smtp: 550 5.1.1 <" + recipient + "> user unknown while sending " + link),
		"address upper-cased, body base64": errors.New("api 500: <" + strings.ToUpper(recipient) +
			"> rejected; payload " + base64.StdEncoding.EncodeToString([]byte(link))),
	}
	// THE CONTROL: both secrets are genuinely in every probe error, in some form.
	for name, e := range shapes {
		if !strings.Contains(strings.ToLower(e.Error()), strings.ToLower(recipient)) {
			t.Fatalf("%s: the probe error does not contain the address, so finding it absent "+
				"from the log below would prove nothing", name)
		}
	}

	router := chi.NewRouter()
	h.Mount(router)
	var got string
	for name, channelErr := range shapes {
		logged.Reset()
		h.mail = &recordingChannel{err: channelErr}
		requestReset(t, newBrowser(t, router), recipient)
		got = logged.String()
		if !strings.Contains(got, "delivery failed") {
			t.Fatalf("%s: the failure was not logged at all, so §7 is broken in the other "+
				"direction:\n%s", name, got)
		}
		// The address is compared CASE-INSENSITIVELY and the link in BOTH encodings,
		// because a denylist that only matches the spelling it was given is exactly the
		// thing being ruled out here.
		if strings.Contains(strings.ToLower(got), strings.ToLower(recipient)) {
			t.Errorf("%s: the recipient address reached the process log (§4.7).\nLOG:\n%s", name, got)
		}
		for what, secret := range map[string]string{
			"raw token":   token,
			"whole link":  link,
			"base64 link": base64.StdEncoding.EncodeToString([]byte(link)),
		} {
			if strings.Contains(got, secret) {
				t.Errorf("%s: the %s reached the process log (§4.7).\nLOG:\n%s", name, what, got)
			}
		}
	}

	// AND THE LINE IS STILL WORTH HAVING: who it was, and what kind of failure.
	if !strings.Contains(got, g.Issued.Reset.AdminUserID.String()) {
		t.Error("the line does not say which administrator's delivery failed")
	}
	if !strings.Contains(got, "err_type") {
		t.Error("the line carries no classification at all, so the diagnosability this " +
			"design pays for the structural guarantee was not actually bought")
	}
}

// TestAdminReset_TheUndeliverableLineClaimsOnlyWhatHappened closes a trail that
// invented events.
//
// Measured: GET /admin/reset/new with NO ?t= and no cookie answered 404 and wrote
// reason="a recovery link was presented and this deployment issues none". Nothing had
// been presented. A trail that invents attempts is worse than one that misses them,
// because an investigator cannot tell the invented ones from the real.
func TestAdminReset_TheUndeliverableLineClaimsOnlyWhatHappened(t *testing.T) {
	t.Parallel()
	const token = "qCq0TQlXqhQ0T2vJmMHhVh2mSpXk4rKzM0f7YyQvCJ8"

	t.Run("a bare browse writes nothing", func(t *testing.T) {
		t.Parallel()
		var logged bytes.Buffer
		router := newResetRouterLogging(t, &fakeResets{}, nil, &logged)
		b := newBrowser(t, router)
		if rec := b.do(http.MethodGet, adminResetNewPath, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want 404", rec.Code)
		}
		if rec := b.do(http.MethodPost, adminResetNewPath, url.Values{"csrf": {"x"}}); rec.Code != http.StatusNotFound {
			t.Fatalf("code = %d, want 404", rec.Code)
		}
		if got := logged.String(); strings.Contains(got, "was presented") {
			t.Errorf("nothing was presented and the log says one was:\n%s", got)
		}
	})

	t.Run("a real presentation still writes", func(t *testing.T) {
		t.Parallel()
		var logged bytes.Buffer
		router := newResetRouterLogging(t, &fakeResets{}, nil, &logged)
		b := newBrowser(t, router)
		b.do(http.MethodGet, adminResetNewPath+"?t="+token, nil)
		// A POST carrying the cookie is a presentation too, even on a dark deployment.
		b.cookies[adminResetLinkCookieName] = "abc." + token
		b.do(http.MethodPost, adminResetNewPath, url.Values{"csrf": {"x"}})
		got := logged.String()
		for _, where := range []string{"admin_reset_open", "admin_reset_submit"} {
			if !strings.Contains(got, where) {
				t.Errorf("a link WAS presented at %s and nothing was written:\n%s", where, got)
			}
		}
	})
}
