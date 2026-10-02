package operator_test

// op8r3_test.go -- OP-8's third round: the console's flood gate (F4) and what a refused
// cross-origin request writes to the process log at production's level (F5, changed in
// the 4th round by B6). The round's structural pins are typepins_test.go's.

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/httpx"
)

// TestFloodGate_AnExhaustedAddressReachesNoConsolePredicate measures the console's
// floodGate (ADR 0020 §4's chain; surface.go's sessionLimit comment rests on it: the
// predicate's database work is bounded per ADDRESS by this gate). An address that has
// spent its flood budget (300 sign-in page loads -- each a charge, none a store call)
// gets 429 on the console with a LIVE session cookie, and the session predicate is not
// run: zero store calls, op_touch_session included. CONTROL: the same cookie from
// another address renders the console with exactly one predicate call.
func TestFloodGate_AnExhaustedAddressReachesNoConsolePredicate(t *testing.T) {
	g := newRig(t)
	sess := g.signIn(g.active())
	const spent, fresh = "198.51.100.7:1", "198.51.100.8:1"
	console := func(remote string) req {
		return req{method: http.MethodGet, host: opHost, path: "/operator", cookies: []*http.Cookie{sess},
			remote: remote, header: map[string]string{"Sec-Fetch-Site": "same-origin"}}
	}
	before := g.store.total()
	for i := 0; i < 300; i++ {
		if w := g.do(req{method: http.MethodGet, host: opHost, path: "/operator/login", remote: spent}); w.Code != http.StatusOK {
			t.Fatalf("sign-in page load %d from one address = %d, want 200 (the flood budget is 300)", i+1, w.Code)
		}
	}
	if g.store.total() != before {
		t.Fatalf("PREMISE: the sign-in page made %d store call(s)", g.store.total()-before)
	}
	w := g.do(console(spent))
	if w.Code != http.StatusTooManyRequests || !strings.Contains(w.Body.String(), "Too many requests") {
		t.Errorf("the console from an exhausted address = %d, want 429 and the throttle page", w.Code)
	}
	if n := g.store.total() - before; n != 0 {
		t.Errorf("the refused console request made %d store call(s) (TouchOperatorSession %d), want 0 -- the predicate ran past the flood gate",
			n, g.store.count("TouchOperatorSession"))
	}
	touches := g.store.count("TouchOperatorSession")
	if w := g.do(console(fresh)); w.Code != http.StatusOK || g.store.count("TouchOperatorSession") != touches+1 {
		t.Fatalf("CONTROL: the same session from another address = %d with %d predicate call(s), want 200 with 1",
			w.Code, g.store.count("TouchOperatorSession")-touches)
	}
}

// TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel measures the 4th
// round's decision (B6) at production's level (Info: deploy/k8s/05-config.yaml):
//   - 500 cookieless, Origin-less sign-outs from one address (sign-out has no flood gate
//     in front of the check) -> 500 x 403;
//   - then 500 cross-origin sign-ins from the same address -> 300 x 403 and 200 x 429
//     (the refused sign-outs charged no flood budget);
//   - then one refusal from a second address; the 801 refusals made 0 store calls;
//   - the 801 refusals wrote ONE record with the refusal's message, at WARN (the
//     window's first; the window is process-wide); the access log wrote 1 001 records;
//   - then the operator's own sign-out from the same address answers 303 and leaves 0
//     live sessions (the 2nd round's B3).
//
// CONTROL: at Debug, two refusals write one WARN and one DEBUG record.
func TestSameOriginGate_RefusalsWriteOneWarnPerWindowAtTheShippedLevel(t *testing.T) {
	const refused = "operator request refused: not from the operator origin"
	g := newRigAt(t, slog.LevelInfo)
	sess := g.signIn(g.active())
	g.logs.Reset()
	before := g.store.total()
	const attacker = "198.51.100.9:1"
	for i := 0; i < 500; i++ {
		w := g.do(req{method: http.MethodPost, host: opHost, path: "/operator/logout", form: url.Values{}, remote: attacker})
		if w.Code != http.StatusForbidden {
			t.Fatalf("cross-origin sign-out %d = %d, want 403", i+1, w.Code)
		}
	}
	if n := g.store.total() - before; n != 0 {
		t.Errorf("500 refused sign-outs made %d store call(s), want 0", n)
	}
	codes := map[int]int{}
	for i := 0; i < 500; i++ {
		codes[g.do(req{method: http.MethodPost, host: opHost, path: "/operator/login", origin: "https://taptime.mt",
			form: url.Values{"email": {"x@example.test"}, "password": {"x"}}, remote: attacker}).Code]++
	}
	if codes[http.StatusForbidden] != 300 || codes[http.StatusTooManyRequests] != 200 || len(codes) != 2 {
		t.Errorf("500 cross-origin sign-ins from one address = %v, want 300 x 403 and 200 x 429", codes)
	}
	// A refusal from a second address in the same window: the window is process-wide.
	if w := g.do(req{method: http.MethodPost, host: opHost, path: "/operator/logout", form: url.Values{}, remote: "198.51.100.10:1"}); w.Code != http.StatusForbidden {
		t.Fatalf("a cross-origin sign-out from a second address = %d", w.Code)
	}
	// Measured over all 801 (5th round, N3: the count used to stop after the first 500).
	if n := g.store.total() - before; n != 0 {
		t.Errorf("the 801 refusals made %d store call(s), want 0", n)
	}
	logged := g.logs.String()
	if n, warns := strings.Count(logged, `"msg":"`+refused+`"`), strings.Count(logged, `"level":"WARN","msg":"`+refused+`"`); n != 1 || warns != 1 {
		t.Errorf("at Info the 801 refusals wrote %d record(s) with the refusal's message, %d at WARN; want exactly 1 WARN", n, warns)
	}
	// The access log is at Info: one record per request, written by both handlers.
	if n := strings.Count(logged, `"msg":"`+httpx.EventHTTPRequest+`"`); n != 1001 {
		t.Errorf("PREMISE: %d access record(s) for 1 001 requests -- the Info capture is not the one the requests reach", n)
	}
	// B3's gain is intact: the operator's own sign-out from that address revokes.
	if w := g.do(req{method: http.MethodPost, host: opHost, path: "/operator/logout", form: url.Values{}, origin: opOrigin,
		cookies: []*http.Cookie{sess}, remote: attacker}); w.Code != http.StatusSeeOther || g.store.liveSessions() != 0 {
		t.Errorf("the operator's sign-out after the refusals = %d with %d live session(s), want 303 and 0", w.Code, g.store.liveSessions())
	}
	d := newRig(t)
	for i := 0; i < 2; i++ {
		if w := d.do(req{method: http.MethodPost, host: opHost, path: "/operator/logout", form: url.Values{}}); w.Code != http.StatusForbidden {
			t.Fatalf("CONTROL: a cross-origin sign-out = %d", w.Code)
		}
	}
	dl := d.logs.String()
	if warns, debugs := strings.Count(dl, `"level":"WARN","msg":"`+refused+`"`), strings.Count(dl, `"level":"DEBUG","msg":"`+refused+`"`); warns != 1 || debugs != 1 {
		t.Fatalf("CONTROL: at Debug two refusals wrote %d WARN and %d DEBUG record(s), want 1 and 1", warns, debugs)
	}
}
