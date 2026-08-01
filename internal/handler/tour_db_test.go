package handler

// The mini tour (M5-07) against a REAL Postgres, through ONE router carrying BOTH
// the activation flow and the tap flow. Two of the card's four criteria are only
// checkable here, and neither can be checked with fakes:
//
//   - THE TOUR WRITES NOTHING. A fake cannot prove the absence of a write; a row
//     count can. Walking all three slides and skipping out of them must leave the
//     `transactions` and `audit_log` counts exactly where they were.
//   - SKIPPING THE TOUR STILL LEAVES THE PRACTICE FLAG. The card's fourth
//     criterion is about the RECORD, so it is read back out of the column
//     (transactions.practice) rather than inferred from a screen.
//
// Fixtures are NOT cleaned up (tappa_app has REVOKE DELETE on the tables
// involved). Fresh random ids keep runs from colliding; `make db-reset` clears
// the dev database.

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
)

// tourFlow is the tap harness with the ACTIVATION routes mounted beside the tap
// routes, so one client can walk invitation -> tour -> plaque without swapping
// servers halfway. It replaces h.router on purpose: postTap and the rest of the
// harness then drive the combined router, which is the point — the record at the
// end has to come out of the same process the activation went into.
type tourFlow struct {
	*tapHarness
	invites *invite.Manager
	server  *httptest.Server
}

func newTourFlow(t *testing.T) *tourFlow {
	t.Helper()
	h := newTapHarness(t) // skips when DATABASE_URL is unset

	invites, err := invite.New(h.data, h.cfg)
	if err != nil {
		t.Fatalf("invite.New: %v", err)
	}
	act, err := NewActivation(invites, h.sessions, h.trail, h.cfg,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("NewActivation: %v", err)
	}
	r := chi.NewRouter()
	h.tap.Mount(r)
	act.Mount(r)
	h.router = r

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &tourFlow{tapHarness: h, invites: invites, server: srv}
}

// invitedEmployee adds somebody who has NEVER been activated: status 'invited'
// and activated_at NULL. That is the shape the tour is shown to, and the shape
// the practice run belongs to — newEmployee stamps activated_at, which would skip
// the very step under test.
func (f *tourFlow) invitedEmployee(t *testing.T) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, 'Joseph Camilleri', 'invited', now())`,
			id, f.tenantID, f.locationID)
		return e
	})
	if err != nil {
		t.Fatalf("fixture employee: %v", err)
	}
	return id
}

// inviteLinkFor mints an invitation and returns the raw code off the delivery
// channel, exactly as a mail sender would receive it (linkChannel lives in
// e2e_db_test.go).
func (f *tourFlow) inviteLinkFor(t *testing.T, employeeID uuid.UUID) string {
	t.Helper()
	ch := &linkChannel{}
	if _, err := f.invites.IssueAndDeliver(context.Background(), invite.IssueParams{
		TenantID: f.tenantID, EmployeeID: employeeID,
	}, ch); err != nil {
		t.Fatalf("IssueAndDeliver: %v", err)
	}
	u, err := url.Parse(ch.url)
	if err != nil {
		t.Fatalf("activation url: %v", err)
	}
	code := u.Query().Get("code")
	if code == "" {
		t.Fatal("the delivered link carries no code")
	}
	return code
}

// auditTotal counts EVERY audit row of this tenant, not one action's. The tour's
// promise is that it writes nothing at all, and a per-action count could only ever
// prove it does not write the actions somebody thought of.
func (h *tapHarness) auditTotal(t *testing.T) int {
	t.Helper()
	var n int
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1`, h.tenantID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count audit_log: %v", err)
	}
	return n
}

// activate drives a browser through the real invitation flow and returns the
// session cookie plus the Location the POST redirected to.
func (f *tourFlow) activate(t *testing.T, code string) (*http.Cookie, string) {
	t.Helper()
	c := f.client(t)

	page, err := c.Get(f.server.URL + "/activate?code=" + url.QueryEscape(code))
	if err != nil {
		t.Fatalf("GET /activate: %v", err)
	}
	form := body(t, page)
	if page.StatusCode != http.StatusOK {
		t.Fatalf("activation page status = %d", page.StatusCode)
	}

	// Do NOT follow the redirect: the destination is exactly what is under test.
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	done, err := c.PostForm(f.server.URL+"/api/activate",
		url.Values{"consent": {"yes"}, "csrf": {formToken(t, form)}})
	if err != nil {
		t.Fatalf("POST /api/activate: %v", err)
	}
	defer done.Body.Close()
	if done.StatusCode != http.StatusSeeOther {
		t.Fatalf("submit status = %d, want 303", done.StatusCode)
	}
	for _, ck := range done.Cookies() {
		if ck.Name == session.CookieName && ck.Value != "" {
			return ck, done.Header.Get("Location")
		}
	}
	t.Fatal("activation set no session cookie")
	return nil, ""
}

// client is a cookie-jar browser for the combined server. The jar matters: the
// activation cookie has to survive the 303 between GET /activate and the POST,
// exactly as it does in a phone.
func (f *tourFlow) client(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{Jar: jar, Timeout: 10 * time.Second}
}

// tapAgo is an occurred_at the caller declares, far enough back to clear the
// harness's 120 s person-debounce and well inside the 72 h occurred-at ceiling.
func tapAgo(seconds int) url.Values {
	return url.Values{occurredAtField: {
		time.Now().UTC().Add(-time.Duration(seconds) * time.Second).Format(time.RFC3339),
	}}
}

// TestTourDB_WritesNothing is the card's "the tour can be skipped" reduced to the
// only thing that makes skipping safe: there is nothing to undo.
//
// It walks every slide, revisits one, asks for an out-of-range step and then
// leaves — and asserts the `transactions` and `audit_log` counts are unchanged
// throughout. Both tables matter and for different reasons: `transactions` is
// immutable (§4.3) so a stray row could never be removed, and `audit_log` is
// append-only at the DATABASE level (measured in M5-02: even tappa_owner cannot
// DELETE), which makes an unbounded writer into it an attack on the trail itself.
//
// POSITIVE CONTROL INCLUDED. A count that never moves is also what a broken
// assertion looks like, so the test finishes by posting a real tap and requiring
// the transaction count to go UP by one. Without it, "the tour wrote nothing"
// would be equally true of a test that was reading the wrong tenant.
func TestTourDB_WritesNothing(t *testing.T) {
	f := newTourFlow(t)
	emp := f.newEmployee(t, "active")
	cookie := f.cookieForEmployee(t, emp)

	txBefore, auditBefore := f.transactionCount(t), f.auditTotal(t)

	for _, target := range []string{
		"/activate/tour",
		"/activate/tour?step=1",
		"/activate/tour?step=2",
		"/activate/tour?step=3",
		"/activate/tour?step=2", // going back is a link like any other
		"/activate/tour?step=99",
		"/activate/done", // the way out, i.e. the skip
	} {
		w := f.get(t, target, cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", target, w.Code)
		}
		if got := f.transactionCount(t); got != txBefore {
			t.Fatalf("%s wrote a transaction: %d -> %d", target, txBefore, got)
		}
		if got := f.auditTotal(t); got != auditBefore {
			t.Fatalf("%s wrote an audit row: %d -> %d", target, auditBefore, got)
		}
	}

	// The positive control: the counter under the assertion really can move.
	if w := f.postTap(t, f.nfcContext(uint32(f.startCtr)+1), cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("control tap status = %d", w.Code)
	}
	if got := f.transactionCount(t); got != txBefore+1 {
		t.Fatalf("control: transactions went %d -> %d, want +1 — the count above proved nothing",
			txBefore, got)
	}
}

// TestTourDB_SkippingTheTourStillLeavesThePracticeFlag is the card's fourth
// criterion end to end, and the whole point of it is that the two things are
// UNCONNECTED: nothing about the tour feeds the practice derivation, so leaving
// early cannot cost somebody their practice run.
//
// The flow is the real one: an invitation is minted and consumed over HTTP, the
// session cookie comes off the activation response, the tour is NOT visited at
// all, and then a tap is posted through the same router. The assertion is read
// back from the COLUMN — transactions.practice — because a screen saying
// "TRAINING" is a rendering of the record and this criterion is about the record.
func TestTourDB_SkippingTheTourStillLeavesThePracticeFlag(t *testing.T) {
	f := newTourFlow(t)
	emp := f.invitedEmployee(t)
	code := f.inviteLinkFor(t, emp)

	cookie, dest := f.activate(t, code)
	if dest != "/activate/tour" {
		t.Fatalf("a first activation redirected to %q, want /activate/tour", dest)
	}
	if n := f.countFor(t, emp); n != 0 {
		t.Fatalf("the employee already has %d records; the practice run needs none", n)
	}

	// THE SKIP: the tour is never fetched. Straight from activation to the plaque.
	if w := f.postTap(t, f.nfcContext(uint32(f.startCtr)+1), cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("tap status = %d", w.Code)
	}

	rec := f.lastRecord(t, emp)
	if !rec.Practice {
		t.Fatalf("the first record after activation is not marked practice (verdict %q): "+
			"skipping the tour must not cost the practice run", rec.Verdict)
	}
	if rec.Type == nil || *rec.Type != "in" {
		t.Fatalf("practice direction = %q, want in", deref(rec.Type))
	}
}

// TestTourDB_WalkingTheTourLeavesTheSameRecordAsSkippingIt is the other half of
// the same claim, and it is a separate test because "skipping does not cost the
// flag" and "walking does not EARN it" are different failures. If the tour ever
// grew a write — a "seen the tour" stamp, a cookie, an audit row — the two paths
// would stop producing identical records, and this is what would say so.
func TestTourDB_WalkingTheTourLeavesTheSameRecordAsSkippingIt(t *testing.T) {
	f := newTourFlow(t)
	emp := f.invitedEmployee(t)
	code := f.inviteLinkFor(t, emp)

	cookie, dest := f.activate(t, code)
	if dest != "/activate/tour" {
		t.Fatalf("a first activation redirected to %q, want /activate/tour", dest)
	}

	for _, target := range []string{
		"/activate/tour", "/activate/tour?step=2", "/activate/tour?step=3", "/activate/done",
	} {
		w := f.get(t, target, cookie)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d", target, w.Code)
		}
	}

	if w := f.postTap(t, f.nfcContext(uint32(f.startCtr)+1), cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("tap status = %d", w.Code)
	}
	rec := f.lastRecord(t, emp)
	if !rec.Practice {
		t.Fatalf("walking the tour changed the first record (verdict %q, practice %v)",
			rec.Verdict, rec.Practice)
	}
	if n := f.countFor(t, emp); n != 1 {
		t.Fatalf("the employee has %d records after one tap, want 1", n)
	}
}

// TestTourDB_ASecondDeviceIsNotShownThePractisePromise is the honesty gate on
// Submit, measured rather than reasoned about.
//
// Somebody activating a NEW PHONE has already tapped, so their next tap is NOT a
// practice run — practice needs no prior record at all (internal/domain/tap,
// isPracticeTap, and the table in trust_practice_test.go). Showing them a slide
// that says otherwise would be exactly the sort of sentence §4.6 forbids, so the
// second-device redirect goes to the confirmation instead. This test drives both
// halves on one employee: first activation -> tour, tap, second activation ->
// confirmation, tap, and the second record is NOT practice.
func TestTourDB_ASecondDeviceIsNotShownThePractisePromise(t *testing.T) {
	f := newTourFlow(t)
	emp := f.invitedEmployee(t)

	first, dest := f.activate(t, f.inviteLinkFor(t, emp))
	if dest != "/activate/tour" {
		t.Fatalf("first activation redirected to %q, want /activate/tour", dest)
	}
	if w := f.postTap(t, f.nfcContext(uint32(f.startCtr)+1), first, nil); w.Code != http.StatusOK {
		t.Fatalf("first tap status = %d", w.Code)
	}
	if rec := f.lastRecord(t, emp); !rec.Practice {
		t.Fatalf("precondition: the first tap is the practice run; got practice=%v verdict=%q",
			rec.Practice, rec.Verdict)
	}

	// A NEW PHONE. The employee is 'active' now, so this is SecondDeviceReplaced.
	second, dest := f.activate(t, f.inviteLinkFor(t, emp))
	if dest != "/activate/done?replaced=1" {
		t.Fatalf("second activation redirected to %q, want /activate/done?replaced=1 — a second "+
			"device must not be promised a practice run it will not get", dest)
	}

	// The declared time clears the harness's 120 s person-debounce so the tap is
	// recorded rather than swallowed as a duplicate.
	if w := f.postTap(t, f.nfcContext(uint32(f.startCtr)+2), second, tapAgo(300)); w.Code != http.StatusOK {
		t.Fatalf("second tap status = %d", w.Code)
	}
	rec := f.lastRecord(t, emp)
	if rec.Practice {
		t.Fatalf("the tap after a SECOND activation was marked practice (verdict %q): the "+
			"practice run belongs to the first record, and the tour's promise is scoped to it",
			rec.Verdict)
	}
	if n := f.countFor(t, emp); n != 2 {
		t.Fatalf("the employee has %d records, want 2", n)
	}
}

// TestTourDB_TheTourIsBehindTheSessionGate: the slides are literals, but the
// route still refuses a visitor with no session — the same answer the
// confirmation gives, so a stranger opening /activate/tour learns nothing about
// whether a session exists on this phone beyond what /activate/done already says.
func TestTourDB_TheTourIsBehindTheSessionGate(t *testing.T) {
	f := newTourFlow(t)
	w := f.get(t, "/activate/tour")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (a calm page, not an error)", w.Code)
	}
	text := screenText(t, w.Body.String())
	if !strings.Contains(text, problemNoSession.Title) {
		t.Fatalf("a session-less visitor was not sent to the sign-in screen:\n%s", text)
	}
	if strings.Contains(text, "Tap the plaque") {
		t.Fatal("a session-less visitor was shown the tour")
	}
}

// TestTourDB_AClientCannotDeclareOrRefuseThePracticeFlag is the M4-06
// hours-inflation exploit, TRIED rather than reasoned about.
//
// The structural half is already pinned in the decision engine
// (TestInput_HasNoClientPracticeField: tap.Input has no field a practice claim
// could travel in, and adding one fails that test — measured). This is the half
// that could not be pinned there, because "no field on a struct" says nothing
// about what an HTTP body may contain: it drives real form fields through the
// real endpoint into a real row.
//
// BOTH DIRECTIONS, because the exploit has two shapes and only one of them is the
// obvious one:
//
//	claiming practice   would let somebody mark a real check-in as training and
//	                    then dispute the hours — or, worse, mark a CHECKOUT as
//	                    training so the check-in stays open and the day runs long.
//	refusing practice   would let somebody turn their free training tap into a
//	                    counted arrival, which is the same theft in the other
//	                    direction.
//
// The handler reads exactly four form fields (ctx, occurred_at, lat, lng), so
// every name below is ignored on the way in — and the assertion is on the COLUMN,
// which is where the claim would have had to land to matter.
func TestTourDB_AClientCannotDeclareOrRefuseThePracticeFlag(t *testing.T) {
	claims := url.Values{
		"practice": {"true"}, "Practice": {"1"}, "is_practice": {"yes"},
		"training": {"true"}, "practice_tap": {"on"},
	}

	t.Run("a first tap cannot refuse its practice run", func(t *testing.T) {
		f := newTourFlow(t)
		emp := f.newEmployee(t, "active")
		cookie := f.cookieForEmployee(t, emp)

		deny := url.Values{"practice": {"false"}, "Practice": {"0"}, "training": {"no"}}
		if w := f.postTap(t, f.nfcContext(uint32(f.startCtr)+1), cookie, deny); w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		if rec := f.lastRecord(t, emp); !rec.Practice {
			t.Fatalf("a client talked the server out of the practice run (verdict %q)", rec.Verdict)
		}
	})

	t.Run("a CHECKOUT cannot claim one", func(t *testing.T) {
		f := newTourFlow(t)
		emp := f.newEmployee(t, "active")
		cookie := f.cookieForEmployee(t, emp)

		// THE HISTORY IS SEEDED, NOT TAPPED, and ADR 0006 is why. This used to fire
		// taps one and two as real POSTs declaring tapAgo(900)/tapAgo(600), which
		// cleared the person-debounce only because the debounce read a CLIENT-
		// DECLARED time. It now also reads transactions.created_at, so two POSTs a
		// second apart are a second apart and the second one is `ignored`. The two
		// rows this case needs are therefore WRITTEN aged (both stamps together) —
		// a practice `in`, then the ordinary `in` a practice tap does not hold open —
		// and the tap under test, the CHECKOUT, is still a real POST.
		f.seedTapAgedBy(t, emp, 900*time.Second, "in", true)  // the practice run, spent
		f.seedTapAgedBy(t, emp, 600*time.Second, "in", false) // the ordinary check-IN
		if rec := f.lastRecord(t, emp); rec.Practice || rec.Type == nil || *rec.Type != "in" {
			t.Fatalf("precondition: the open record is an ordinary check-IN; practice=%v type=%q",
				rec.Practice, deref(rec.Type))
		}

		// Tap three arrives with every practice-shaped field a client could invent.
		claimed := url.Values{}
		for k, v := range claims {
			claimed[k] = v
		}
		if w := f.postTap(t, f.nfcContext(uint32(f.startCtr)+3), cookie, claimed); w.Code != http.StatusOK {
			t.Fatalf("third tap status = %d", w.Code)
		}
		rec := f.lastRecord(t, emp)
		if rec.Type == nil || *rec.Type != "out" {
			t.Fatalf("third tap direction = %q, want out (it closes tap two)", deref(rec.Type))
		}
		if rec.Practice {
			t.Fatalf("a client marked a CHECKOUT as training with a form field (verdict %q): the "+
				"hours-inflation exploit is open", rec.Verdict)
		}
	})
}

// TestTourDB_ARejectedFirstTapSpendsThePracticeRun runs the SEQUENCE the tour's
// third slide is worded around, end to end, instead of inferring it from two
// separate measurements.
//
// 🔴 WHY IT HAD TO BE THE SEQUENCE. pages.Tour's comment says "tap 1 reject, tap 2
// practice=false", and until this test that was a DEDUCTION from two facts proven
// apart: that §4.6 records a reject (checkin_db_test.go) and that any prior record
// spends the practice run (trust_practice_test.go). Both halves were true and the
// join was still unproven — a reject's row would have to carry employee_id for the
// second fact to see the first, and nothing asserted that it does. An audit called
// the word "Measured" unearned, correctly. Now it is measured.
//
// WHAT IT MEANS FOR THE COPY: the slide may not promise anything about the NEXT
// tap, only about the FIRST one, and it must lean on the confirmation screen for
// what any given tap turned out to be. It does both.
func TestTourDB_ARejectedFirstTapSpendsThePracticeRun(t *testing.T) {
	f := newTourFlow(t)
	emp := f.newEmployee(t, "active") // activated_at is set: the practice shape
	cookie := f.cookieForEmployee(t, emp)

	// 🔴 THE SEQUENCE IS NOW RUN IN TWO HALVES, and ADR 0006 is why. It used to be
	// two real POSTs a second apart, with tap one declaring tapAgo(600) so tap two
	// cleared the person-debounce — this file's own comment said exactly that. The
	// debounce now also measures against transactions.created_at, so a declared
	// past no longer buys distance and tap two would be `ignored`. Splitting keeps
	// BOTH halves production: half one still produces the reject through the real
	// flow, half two still decides the following tap through the real engine.

	// HALF ONE — a rejected tap IS a record, and it is recorded AGAINST THE PERSON.
	// (lastRecord and countFor both key on employee_id, so finding the row at all
	// is the proof that a §5 row-1 reject carries one. That is the join this test
	// exists for: without it, "a reject spends the practice run" would stay a
	// deduction from two facts proven apart.)
	f.retireTag(t, f.tagUID, "retired")
	if w := f.postTap(t, f.nfcContext(uint32(f.startCtr)+1), cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("tap one status = %d, want 200 (a denied tap is RECORDED, not refused)", w.Code)
	}
	first := f.lastRecord(t, emp)
	if first.Verdict != "reject" {
		t.Fatalf("tap one verdict = %q, want reject", first.Verdict)
	}
	if first.Practice {
		t.Fatal("a rejected tap must not be marked practice: there are no hours to exclude")
	}
	if first.Type != nil {
		t.Fatalf("a reject carries a direction (%q): nothing is opened by a refusal", deref(first.Type))
	}
	if n := f.countFor(t, emp); n != 1 {
		t.Fatalf("the reject left %d rows, want 1 — §4.6 is what makes this test possible", n)
	}

	// HALF TWO — the tap that FOLLOWS such a reject. Same shape, its own person,
	// and the reject in the history is written aged (the row half one just proved
	// production writes). The tap itself is a real POST on a live plaque.
	//
	// TWO DETAILS ARE MEASURED RATHER THAN GUESSED, and both cost a red run:
	//
	//   - IT DECLARES NO TIME. A caller-stated past time is what
	//     base:queued-window sends to review, so declaring one made this `flag`.
	//     A live tap posts no timestamp, which is also the shape the tap page has.
	//   - ITS COUNTER IS READ OFF THE TAG. A reject on a retired plaque does NOT
	//     advance tags.last_ctr, so assuming startCtr+2 opened a gap of two and
	//     base:ctr-gap-review made this `flag` as well ("the tag counter jumped").
	//     Reading the live row keeps the test measuring the practice rule instead
	//     of accidentally measuring the counter.
	f.retireTag(t, f.tagUID, "active") // the plaque is back in service
	next := f.newEmployee(t, "active")
	f.seedAgedRecord(t, next, 600*time.Second, nil, "reject", false)
	ctr := uint32(f.lastCtr(t, f.tagUID)) + 1 //nolint:gosec // fixture counter, far inside uint32
	if w := f.postTap(t, f.nfcContext(ctr), f.cookieForEmployee(t, next), nil); w.Code != http.StatusOK {
		t.Fatalf("tap two status = %d", w.Code)
	}
	second := f.lastRecord(t, next)
	if second.Verdict != "ok" {
		t.Fatalf("tap two verdict = %q via %q, want ok", second.Verdict, deref(second.MatchedSid))
	}
	if second.Practice {
		t.Fatal("tap two was marked practice: the rejected tap did NOT spend the run, and the " +
			"tour's third slide would then be understating what the system gives")
	}
	if second.Type == nil || *second.Type != "in" {
		t.Fatalf("tap two direction = %q, want in (a reject carries none, so nothing was open)",
			deref(second.Type))
	}
}
