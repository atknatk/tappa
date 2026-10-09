package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web/templates/components"
)

// devTapPost presses the dev strip's button in browser c and follows the
// redirects the way a browser does: /dev/simulate-tap → /t?… → (for an
// activation) /activate/complete.
func (h *harness) devTapPost(t *testing.T, c *http.Client) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.server.URL+components.DevSimulateTapPath, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", components.DevSimulateTapPath, err)
	}
	return resp, body(t, resp)
}

// TestDevTapDB_ASimulatedTapActivatesThroughTheRealPath is the dev tool end to
// end (ADR 0026, "Geliştirme aracı"): after consent, the button produces a REAL
// activation — the minted URL goes through GET /t, sun.Verify and the atomic
// counter advance — so exactly one session, the invitation spent, zero
// attendance rows and last_ctr + 1. Pressed again with the live session it
// lands on the ordinary tap page, which advances nothing.
func TestDevTapDB_ASimulatedTapActivatesThroughTheRealPath(t *testing.T) {
	h := newHarness(t, "invited")
	// What cmd/tappa does on a dev deployment (this harness is dev on localhost).
	h.activation.EnableDevTools(h.cfg)
	h.tap.EnableDevTools(h.cfg)
	// A fresh server AFTER the switch, so the handlers' goroutines start after the
	// write (the race detector does not model happens-before through sockets).
	h.server.Close()
	h.server = httptest.NewServer(h.router)
	t.Cleanup(h.server.Close)
	c := h.client(t)
	h.consent(t, c, h.issue(t))
	before := h.lastCtr(t, h.tagUID)
	txBefore := h.transactionCount(t)

	resp, page := h.devTapPost(t, c)
	if resp.StatusCode != http.StatusOK || resp.Request.URL.Path != ActivationCompletePath || !strings.Contains(page, "Activation complete") {
		t.Fatalf("the simulated tap landed on %s with %d", resp.Request.URL, resp.StatusCode)
	}
	h.assertSessionCount(t, 1)
	h.assertInviteConsumed(t, true)
	h.assertEmployeeStatus(t, "active")
	if got := h.transactionCount(t); got != txBefore {
		t.Errorf("the simulated activation wrote %d transactions rows", got-txBefore)
	}
	if got := h.lastCtr(t, h.tagUID); got != before+1 {
		t.Errorf("last_ctr = %d, want %d: the minted tap must run the atomic advance", got, before+1)
	}
	if !strings.Contains(page, "DEV ONLY") {
		t.Error("the completion page in dev must offer the next simulated tap")
	}

	resp, page = h.devTapPost(t, c)
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, "data-tap-button") {
		t.Fatalf("with a live session the simulated tap must land on the tap page; got %d at %s", resp.StatusCode, resp.Request.URL)
	}
	if got := h.lastCtr(t, h.tagUID); got != before+1 {
		t.Errorf("opening the tap page moved last_ctr to %d", got)
	}
}
