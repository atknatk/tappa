package handler

import (
	"errors"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// resetlivelink_test.go — M10 EM-7C's one live link per account as the REQUEST FORM
// meets it, against fakes: the answer for an account that already holds a live link
// is the answer for every other address, its "kept" row is the trail's, and a link
// recorded undelivered is withdrawn so the account's one slot is never held by a link
// no inbox has. The rule itself is internal/adminauth's (livelink_db_test.go), and the
// wired flow against real Postgres is adminreset_db_test.go's
// TestPanelRecoveryDB_OneLiveLinkPerAccount.

// keptReset is the live link an account already holds.
func keptReset() adminauth.Reset {
	now := time.Now()
	return adminauth.Reset{
		ID:          uuid.New(),
		TenantID:    uuid.New(),
		AdminUserID: uuid.New(),
		CreatedAt:   now.Add(-10 * time.Minute),
		ExpiresAt:   now.Add(adminauth.ResetTTL - 10*time.Minute),
	}
}

// TestAdminReset_ALiveLinkAnswersLikeEveryOtherAddress: three addresses — one nobody
// holds, one whose account holds a live link (nothing is minted), one whose account
// holds none (a link is minted and sent) — get the same status, headers and body, and
// each answer lands inside [resetRequestFloor, resetRequestFloor+50ms]. The kept arm
// does the LEAST work of the three registered paths; without the floor it would answer
// in microseconds and tell a caller "this account already has a link out".
//
// The kept arm sends nothing, writes ONE "kept" row naming the live link, and withdraws
// nothing; the minted arm sends one link and writes one "requested" row.
func TestAdminReset_ALiveLinkAnswersLikeEveryOtherAddress(t *testing.T) {
	t.Parallel()
	const (
		unregistered = "nobody@livelink.example.test"
		holding      = "holding@livelink.example.test"
		fresh        = "fresh@livelink.example.test"
	)
	live := keptReset()
	resets := &fakeResets{
		grantsFor: map[string][]adminauth.ResetGrant{fresh: {grantFor(fresh)}},
		keptFor:   map[string][]adminauth.Reset{holding: {live}},
	}
	mail := &recordingChannel{}
	trail := &fakeTrail{}
	router, h := newResetRouter(t, resets, mail, trail)

	type answer struct {
		code    int
		header  http.Header
		body    string
		elapsed time.Duration
	}
	post := func(email string) answer {
		t.Helper()
		br := newBrowser(t, router)
		page := br.do(http.MethodGet, adminResetPath, nil)
		csrf := csrfFrom(t, htmlOf(t, page))
		start := time.Now()
		rec := br.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {email}})
		elapsed := time.Since(start)
		return answer{code: rec.Code, header: rec.Header().Clone(), body: htmlOf(t, rec), elapsed: elapsed}
	}

	// The kept arm FIRST and on its own, so the trail it leaves is read before any
	// other arm writes.
	kept := post(holding)
	rows := trail.eventsSnapshot()
	if len(rows) != 1 || rows[0].Action != ActionAdminResetKept {
		t.Fatalf("the kept arm left %d row(s) %v when it answered, want one %s row", len(rows), rows, ActionAdminResetKept)
	}
	d, _ := rows[0].Detail.(adminResetDetail)
	if d.Outcome != "kept" || d.Reason != resetReasonLiveLink || d.ResetID != live.ID.String() ||
		d.ExpiresAt != live.ExpiresAt.UTC().Format(time.RFC3339) || rows[0].TenantID != live.TenantID ||
		rows[0].Target != live.AdminUserID.String() {
		t.Errorf("the kept row is %+v / %+v, want outcome kept, the live link's id and expiry, in its account's tenant", rows[0], d)
	}
	nobody := post(unregistered)
	minted := post(fresh)
	drain(t, h)

	if n := len(mail.all()); n != 1 || mail.all()[0].Recipient != fresh {
		t.Fatalf("CONTROL FAILED: the channel received %d link(s), want exactly the fresh account's one", n)
	}
	if n := trail.count(ActionAdminResetRequested); n != 1 {
		t.Errorf("%d requested row(s), want the fresh account's one", n)
	}
	if n := trail.count(ActionAdminResetKept); n != 1 {
		t.Errorf("%d kept row(s), want the holding account's one", n)
	}
	if w := resets.withdrawals(); len(w) != 0 {
		t.Errorf("%d link(s) withdrawn, want none: nothing failed", len(w))
	}

	for name, a := range map[string]answer{"live link held": kept, "unregistered": nobody} {
		if a.code != minted.code {
			t.Errorf("%s: status %d, want %d", name, a.code, minted.code)
		}
		if !reflect.DeepEqual(a.header, minted.header) {
			t.Errorf("%s: headers %v, want %v", name, a.header, minted.header)
		}
		if a.body != minted.body {
			t.Errorf("%s: the body differs from the minted arm's:\n%s\n---\n%s", name, a.body, minted.body)
		}
	}
	for name, a := range map[string]answer{"live link held": kept, "unregistered": nobody, "no live link": minted} {
		if a.elapsed < resetRequestFloor {
			t.Errorf("%s answered in %v, under the floor %v: the branch it took is visible in the clock",
				name, a.elapsed, resetRequestFloor)
		}
		if a.elapsed > resetRequestFloor+50*time.Millisecond {
			t.Errorf("%s answered in %v, over floor+50ms (%v)", name, a.elapsed, resetRequestFloor+50*time.Millisecond)
		}
	}
	t.Logf("answered in: live link held %v, unregistered %v, no live link %v (floor %v)",
		kept.elapsed, nobody.elapsed, minted.elapsed, resetRequestFloor)

	// THE INDEPENDENT CONTROL: the comparison above can fail.
	dark, _ := newResetRouter(t, &fakeResets{}, nil, &fakeTrail{})
	br := newBrowser(t, dark)
	csrf := csrfFrom(t, htmlOf(t, br.do(http.MethodGet, adminResetPath, nil)))
	if other := htmlOf(t, br.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {holding}})); other == minted.body {
		t.Fatal("CONTROL FAILED: an undeliverable deployment's page equals the deliverable one, so the comparison proves nothing")
	}
}

// TestAdminReset_AnUndeliveredLinkIsWithdrawn: a link recorded undelivered is
// withdrawn — before its row, and whatever the row's fate — so the account's one live
// link is never one no inbox holds; a delivered link is not. A failed withdrawal still
// leaves the undelivered row (the trail outlives the failure).
func TestAdminReset_AnUndeliveredLinkIsWithdrawn(t *testing.T) {
	t.Parallel()
	const known = "owner@withdraw.example.test"
	for _, tc := range []struct {
		name        string
		sendErr     error
		withdrawErr error
		withdrawn   bool
		row         string
	}{
		{"delivered (control)", nil, nil, false, ActionAdminResetRequested},
		{"the send failed", errors.New("relay refused"), nil, true, ActionAdminResetUndelivered},
		{"the send failed and so did the withdrawal", errors.New("relay refused"), errors.New("db down"), true, ActionAdminResetUndelivered},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := grantFor(known)
			resets := &fakeResets{grantsFor: map[string][]adminauth.ResetGrant{known: {g}}, withdrawErr: tc.withdrawErr}
			trail := &fakeTrail{}
			router, h := newResetRouter(t, resets, &recordingChannel{err: tc.sendErr}, trail)
			requestReset(t, newBrowser(t, router), known)
			drain(t, h)

			w := resets.withdrawals()
			if tc.withdrawn && (len(w) != 1 || w[0] != g.Issued.Reset.ID) {
				t.Errorf("withdrawn %v, want exactly the undelivered link %v", w, g.Issued.Reset.ID)
			}
			if !tc.withdrawn && len(w) != 0 {
				t.Errorf("withdrawn %v, want nothing: the link was delivered", w)
			}
			if n := trail.count(tc.row); n != 1 || trail.total() != 1 {
				t.Errorf("%d %s row(s) of %d, want exactly one", n, tc.row, trail.total())
			}
		})
	}
}

// TestAdminReset_KeptRowsSpendTheAccountsBudget: a "kept" row is a request-side row and
// goes through the per-account audit budget (recordForAdmin) — so anonymous requests
// against an account holding a live link write at most adminResetAccountLimit rows and
// one rate_limited row, never a row per request into a named tenant's append-only
// table (EM-5A's reason for the budget). Nothing is sent and nothing withdrawn.
func TestAdminReset_KeptRowsSpendTheAccountsBudget(t *testing.T) {
	t.Parallel()
	const holding = "holding@budget.example.test"
	resets := &fakeResets{keptFor: map[string][]adminauth.Reset{holding: {keptReset()}}}
	mail := &recordingChannel{}
	trail := &fakeTrail{}
	router, h := newResetRouter(t, resets, mail, trail)
	const asks = adminResetAccountLimit + 2
	for i := 0; i < asks; i++ {
		requestReset(t, newBrowser(t, router), holding)
	}
	drain(t, h)
	if n := trail.count(ActionAdminResetKept); n != adminResetAccountLimit {
		t.Errorf("%d kept row(s) for %d requests, want the account budget's %d", n, asks, adminResetAccountLimit)
	}
	if n := trail.count(ActionAdminResetLimited); n != 1 {
		t.Errorf("%d rate_limited row(s), want 1", n)
	}
	if n := len(mail.all()); n != 0 {
		t.Errorf("%d link(s) sent to an account holding a live one, want none", n)
	}
	if w := resets.withdrawals(); len(w) != 0 {
		t.Errorf("%d withdrawal(s), want none", len(w))
	}
}

// TestAdminReset_TheSentPageSaysWhatAskingAgainDoes pins the recovery page's sentence
// about asking again (M10 EM-7C round 4) to what the flow does: no new link while one
// still works. Its two earlier forms are gone — "asking again replaces the earlier
// links" (false since one live link per account) and "within the last hour, its link
// still works" (false for a link withdrawn after a timed-out send that reached the
// inbox anyway, and for a link that arrived late with less than an hour left). The
// reset e-mail's matching sentence is web/templates/email's
// TestReset_AskingAgainSaysWhatTheFlowDoes.
func TestAdminReset_TheSentPageSaysWhatAskingAgainDoes(t *testing.T) {
	t.Parallel()
	router, _ := newResetRouter(t, &fakeResets{}, &recordingChannel{}, &fakeTrail{})
	page := strings.Join(strings.Fields(requestReset(t, newBrowser(t, router), "nobody@sentpage.example.test")), " ")
	const now = "If a recovery email reached you, its link works for as long as it says — asking again sends no new link while that one still works."
	if n := strings.Count(page, now); n != 1 {
		t.Errorf("the page says %q %d time(s), want once:\n%s", now, n, page)
	}
	for _, old := range []string{"replaces the earlier links", "within the last hour", "use the newest"} {
		if strings.Contains(page, old) {
			t.Errorf("the page still says %q", old)
		}
	}
}

// TestAdminReset_AnUndeliveredLinkIsWithdrawnPastTheAccountsBudget: the withdrawal
// comes BEFORE the account budget is asked (recordOutcome), so an account whose budget
// anonymous "kept" requests have spent still gets its undelivered link withdrawn —
// otherwise those requests would buy the attacker an hour in which the owner's account
// holds a link no inbox has and every request mints nothing. CONTROL: the budget really
// is spent (the undelivered row is suppressed and the one rate_limited row written).
func TestAdminReset_AnUndeliveredLinkIsWithdrawnPastTheAccountsBudget(t *testing.T) {
	t.Parallel()
	const holding, failing = "holding@pastbudget.example.test", "failing@pastbudget.example.test"
	live := keptReset()
	g := grantFor(failing)
	g.Issued.Reset.TenantID, g.Issued.Reset.AdminUserID = live.TenantID, live.AdminUserID // the same account
	resets := &fakeResets{
		keptFor:   map[string][]adminauth.Reset{holding: {live}},
		grantsFor: map[string][]adminauth.ResetGrant{failing: {g}},
	}
	trail := &fakeTrail{}
	router, h := newResetRouter(t, resets, &recordingChannel{err: errors.New("relay refused")}, trail)
	for i := 0; i < adminResetAccountLimit; i++ {
		requestReset(t, newBrowser(t, router), holding)
	}
	requestReset(t, newBrowser(t, router), failing)
	drain(t, h)

	if n := trail.count(ActionAdminResetKept); n != adminResetAccountLimit {
		t.Fatalf("CONTROL FAILED: %d kept row(s), want the budget's %d", n, adminResetAccountLimit)
	}
	if n, l := trail.count(ActionAdminResetUndelivered), trail.count(ActionAdminResetLimited); n != 0 || l != 1 {
		t.Fatalf("CONTROL FAILED: %d undelivered and %d rate_limited row(s), want 0 and 1 — the budget is not spent", n, l)
	}
	if w := resets.withdrawals(); len(w) != 1 || w[0] != g.Issued.Reset.ID {
		t.Errorf("withdrawn %v, want the undelivered link %v although the account's budget was spent", w, g.Issued.Reset.ID)
	}
}
