package handler

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// emailchangenotice_test.go -- T173 EC-2 item 8: the old address's notice through the REAL e-mail channel
// and the real breaker. The undo link itself is drawn by adminauth (its token is unexported); the whole
// loop with a real link is accountemail_db_test.go's.

const oldNoticeAddress = "Old.Person@Example.TEST"

func newAddressNoticeFlow(t *testing.T) (*AdminReset, *breakerProbe, *bytes.Buffer, ResetChannel) {
	t.Helper()
	p := &breakerProbe{}
	var logged bytes.Buffer
	ch, b, log := newProbeChannel(t, p, &logged)
	h, err := NewAdminReset(&fakeResets{}, ch, &fakeTrail{}, adminTestConfig(), log)
	if err != nil {
		t.Fatal(err)
	}
	stopWorkerAtCleanup(t, h)
	fillBreaker(t, b, 300)
	if !ch.RefusingResets() {
		t.Fatal("CONTROL FAILED: the breaker passes after 300 sends")
	}
	return h, p, &logged, ch
}

// TestEmailChangeNotice_ATrippedBreakerDoesNotStopIt (ADR 0044 §5.1 step 3, §5.8): with the process-wide
// breaker refusing every refusable send (300 in the window -- the control: RefusingResets), the old
// address's notice still reaches the relay, to the address the change returned, and its line says sent
// with the identity's id and no address. MUTATION (measured): DeliverEmailChangeNotice through Send
// instead of SendExempt -> red.
func TestEmailChangeNotice_ATrippedBreakerDoesNotStopIt(t *testing.T) {
	h, p, logged, _ := newAddressNoticeFlow(t)
	ident := uuid.New()
	h.emailChanged(context.Background(), adminauth.EmailChanged{IdentityID: ident, OldEmail: oldNoticeAddress, At: time.Now()})
	if p.count() != 301 || p.last().To != oldNoticeAddress {
		t.Fatalf("%d messages reached the relay (want 301), the last to %q", p.count(), p.last().To)
	}
	if !strings.Contains(logged.String(), `"outcome":"sent"`) || !strings.Contains(logged.String(), ident.String()) {
		t.Errorf("no sent line naming the identity: %s", logged.String())
	}
	// This file's lines (the breaker's own "limit reached" line is mail's, with its own keys).
	var ours []string
	for _, line := range strings.Split(logged.String(), "\n") {
		if strings.Contains(line, "panel address change") {
			ours = append(ours, line)
		}
	}
	if len(ours) < 2 {
		t.Fatalf("CONTROL: %d of this file's lines found", len(ours))
	}
	assertEmailLogClean(t, strings.Join(ours, "\n"))
}

// TestEmailChangeNotice_ThePerPersonCapIsItsOwn (ADR 0044 §5.8): exactly adminEmailNoticeLimit notices
// an hour per identity; past it the notice is not sent and one line says why; another identity's still
// goes; the password notice's cap is another key. MUTATION (measured): the cap keyed on nothing (one key
// for every identity) -> red.
func TestEmailChangeNotice_ThePerPersonCapIsItsOwn(t *testing.T) {
	h, p, logged, _ := newAddressNoticeFlow(t)
	ident := uuid.New()
	for i := 0; i < adminEmailNoticeLimit+2; i++ {
		h.emailChanged(context.Background(), adminauth.EmailChanged{IdentityID: ident, OldEmail: oldNoticeAddress, At: time.Now()})
	}
	if got := p.count() - 300; got != adminEmailNoticeLimit {
		t.Fatalf("%d notices sent for one identity, want %d", got, adminEmailNoticeLimit)
	}
	if !strings.Contains(logged.String(), emailNoticeReasonLimited) {
		t.Error("the capped notice left no line with its reason")
	}
	h.emailChanged(context.Background(), adminauth.EmailChanged{IdentityID: uuid.New(), OldEmail: oldNoticeAddress, At: time.Now()})
	if got := p.count() - 300; got != adminEmailNoticeLimit+1 {
		t.Errorf("another identity's notice was not sent (%d)", got)
	}
	if within, _ := h.noticeLimiter.TryCharge(identityResetBudgetKey(ident)); !within {
		t.Error("the address change notices spent the password notice's budget")
	}
}

// TestEmailChangeNotice_TheTwoShapes (K-E10 (vi), card item 8): with an undo link the letter carries THAT
// link and "within 7 days"; without one its one link is the configured sign-in page and it says the
// address can no longer undo it. Neither names the new address (K-E8 (a)). MUTATION (measured): the
// channel rendering the undo shape with an empty link -> red (the render refuses it; the notice is
// undelivered).
func TestEmailChangeNotice_TheTwoShapes(t *testing.T) {
	_, p, _, ch := newAddressNoticeFlow(t)
	undo := testBaseURL + adminauth.UndoEmailPath + "?" + adminauth.UndoEmailParam + "=AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_AbCdE"
	at := time.Date(2026, 11, 13, 14, 3, 0, 0, time.UTC)
	if err := ch.DeliverEmailChangeNotice(context.Background(), EmailChangeNotice{
		Recipient: oldNoticeAddress, IdentityID: uuid.New(), ChangedAt: at, UndoLink: undo, UndoExpiresAt: time.Now().Add(7*24*time.Hour + time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	m := p.last()
	if !strings.Contains(m.Text, undo) || !strings.Contains(m.Text, "within 7 days") || !strings.Contains(m.Text, "13 November 2026 at 14:03 UTC") {
		t.Errorf("the undo shape lacks its link, its seven days or its date:\n%s", m.Text)
	}
	if err := ch.DeliverEmailChangeNotice(context.Background(), EmailChangeNotice{Recipient: oldNoticeAddress, IdentityID: uuid.New(), ChangedAt: at}); err != nil {
		t.Fatal(err)
	}
	m = p.last()
	if !strings.Contains(m.Text, testBaseURL+adminLoginPath) || strings.Contains(m.Text, "within") || !strings.Contains(m.Text, "can no longer undo it") {
		t.Errorf("the linkless shape is not the sign-in page's letter:\n%s", m.Text)
	}
	if strings.Contains(strings.ToLower(m.Text+m.HTML), "new.person") {
		t.Error("a notice names the new address")
	}
}
