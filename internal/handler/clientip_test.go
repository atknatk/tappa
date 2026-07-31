package handler

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/atknatk/tappa/internal/httpx"
)

// The M5-02 audits handed M5-03 a NAMED obligation: clientIP read r.RemoteAddr
// and nothing else, so behind the planned reverse proxy every request on earth
// shared one rate-limit key. The flood ceiling — the single budget that can
// refuse a VALID activation — was therefore global, and one outsider could spend
// it and keep every venue from activating until the window rolled.
//
// This file measures the fix, and measures the BEFORE state in the same test so
// the AFTER is not vacuously green.

// proxied wraps the activation router in the real-IP middleware, the way
// httpx.NewRouter does in production.
func proxied(h http.Handler, trusted ...string) http.Handler {
	ps := make([]netip.Prefix, 0, len(trusted))
	for _, s := range trusted {
		ps = append(ps, netip.MustParsePrefix(s))
	}
	return httpx.RealIP(ps)(h)
}

// fromClient sends one request that arrived through the proxy at 10.0.0.1 on
// behalf of client, and reports the status.
func fromClient(h http.Handler, client string) int {
	r := httptest.NewRequest(http.MethodGet, "/activate", nil)
	r.RemoteAddr = "10.0.0.1:9000"
	r.Header.Set("X-Forwarded-For", client)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// TestClientIP_ProxiedCallersDoNotShareABucket is the hand-off, closed and
// measured. Client A spends the entire flood ceiling; client B, arriving through
// the same proxy, must still be served.
func TestClientIP_ProxiedCallersDoNotShareABucket(t *testing.T) {
	t.Parallel()
	const a, b = "203.0.113.9", "198.51.100.7"

	t.Run("with the resolver: separate buckets", func(t *testing.T) {
		t.Parallel()
		h := proxied(newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{}), "10.0.0.0/8")
		for i := 0; i < floodLimit; i++ {
			if got := fromClient(h, a); got == http.StatusTooManyRequests {
				t.Fatalf("client A was refused after %d requests, before its ceiling", i)
			}
		}
		if got := fromClient(h, a); got != http.StatusTooManyRequests {
			t.Fatalf("client A over its ceiling answered %d, want 429", got)
		}
		if got := fromClient(h, b); got != http.StatusOK {
			t.Fatalf("client B answered %d, want 200: the ceiling is still shared", got)
		}
	})

	t.Run("without the resolver: the old shared bucket", func(t *testing.T) {
		t.Parallel()
		// The NEGATIVE CONTROL. Same requests, no trusted proxy configured, so
		// every caller resolves to the proxy's address — which is precisely the
		// state M5-02 handed over. If this sub-test ever passes B, the test above
		// proves nothing, because it would mean B was never sharing anything.
		h := proxied(newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{}))
		for i := 0; i < floodLimit; i++ {
			fromClient(h, a)
		}
		if got := fromClient(h, b); got != http.StatusTooManyRequests {
			t.Fatalf("client B answered %d; the shared-key state was expected to refuse it", got)
		}
	})
}

// TestClientIP_CallerCannotPickItsOwnBucket: the other half of the same fix. If
// a caller could choose its key, spending a ceiling would cost it nothing.
func TestClientIP_CallerCannotPickItsOwnBucket(t *testing.T) {
	t.Parallel()
	h := proxied(newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{}), "10.0.0.0/8")
	for i := 0; i < floodLimit; i++ {
		fromClient(h, "203.0.113.9")
	}
	if got := fromClient(h, "203.0.113.9"); got != http.StatusTooManyRequests {
		t.Fatalf("the ceiling was not reached: %d", got)
	}
	// Prepending forged hops does not buy a new bucket: the walk is right to
	// left, and only what our trusted proxy appended counts.
	for _, forged := range []string{
		"1.1.1.1, 203.0.113.9",
		"10.0.0.99, 203.0.113.9",
		"::ffff:203.0.113.9, 203.0.113.9",
	} {
		if got := fromClient(h, forged); got != http.StatusTooManyRequests {
			t.Fatalf("X-Forwarded-For %q bought a fresh budget: %d", forged, got)
		}
	}
	// And the mapped form of the SAME address is the same bucket, not a second
	// one — a caller must not get two budgets for one host.
	if got := fromClient(h, "::ffff:203.0.113.9"); got != http.StatusTooManyRequests {
		t.Fatalf("the IPv4-mapped form of the same address got its own budget: %d", got)
	}
}

// TestClientIP_UntrustedPeerKeepsItsOwnKey: a request that did NOT come through
// a trusted proxy is keyed by its TCP peer, whatever it claims.
func TestClientIP_UntrustedPeerKeepsItsOwnKey(t *testing.T) {
	t.Parallel()
	h := proxied(newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{}), "10.0.0.0/8")
	direct := func(peer, claim string) int {
		r := httptest.NewRequest(http.MethodGet, "/activate", nil)
		r.RemoteAddr = peer
		r.Header.Set("X-Forwarded-For", claim)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	for i := 0; i < floodLimit; i++ {
		direct("198.51.100.7:44321", "203.0.113.9")
	}
	if got := direct("198.51.100.7:44321", "203.0.113.9"); got != http.StatusTooManyRequests {
		t.Fatalf("the direct caller escaped its ceiling by claiming another address: %d", got)
	}
	// Changing the claim does not change the key, because the claim was never
	// read.
	if got := direct("198.51.100.7:44321", "1.2.3.4"); got != http.StatusTooManyRequests {
		t.Fatalf("a new claim bought a new budget: %d", got)
	}
	// A genuinely different peer is a different caller.
	if got := direct("192.0.2.55:44321", "203.0.113.9"); got != http.StatusOK {
		t.Fatalf("a different peer answered %d, want 200", got)
	}
}
