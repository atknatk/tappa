package signup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// VIES, driven against a LOCAL server.
//
// 🔴 NOTHING HERE REACHES THE EUROPEAN COMMISSION, and that is a property of the
// test rather than a hope: newCheckerAt is unexported and takes a base URL, so these
// run against httptest and `make test` never depends on the internet or on a third
// party's uptime. The production base URL is a constant with no configuration behind
// it (vies.go), so no deployment can be pointed elsewhere either.
//
// THE PROPERTY EVERY CASE BELOW IS REALLY ABOUT is Q09's rule: a VIES failure NEVER
// stops a registration. Check returns no error at all, so the tests measure the
// STATUS — and every failure mode must come back Unknown, never Invalid, because an
// outage recorded as "this number is not valid" is an accusation built out of a
// network problem.

func TestVIESCheck_EveryFailureIsUnknownNeverInvalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		handler http.HandlerFunc
		want    VATStatus
	}{
		{
			name: "the register confirms the number",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"isValid":true,"userError":"VALID"}`))
			},
			want: VATValid,
		},
		{
			name: "the register does not know the number",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"isValid":false,"userError":"INVALID"}`))
			},
			want: VATInvalid,
		},
		{
			name:    "the service is down",
			handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
			want:    VATUnknown,
		},
		{
			name:    "the service refuses the request",
			handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadRequest) },
			want:    VATUnknown,
		},
		{
			name: "the answer is not JSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("<html>we moved</html>"))
			},
			want: VATUnknown,
		},
		{
			name: "the answer is enormous",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				// A body past viesMaxBody. It even STARTS like a valid answer, which is
				// the case io.LimitReader would have turned into a confident wrong
				// verdict — see limitedReader.
				_, _ = w.Write([]byte(`{"isValid":true,"padding":"` + strings.Repeat("x", viesMaxBody+1024) + `"}`))
			},
			want: VATUnknown,
		},
		{
			name: "the service redirects us somewhere else",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				// A host WE did not choose. net/http follows up to ten redirects by
				// default; this client refuses the first, which is what keeps
				// "the URL is built from constants" true at run time as well as in the
				// source.
				http.Redirect(w, &http.Request{}, "https://example.invalid/vat", http.StatusFound)
			},
			want: VATUnknown,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(tc.handler)
			t.Cleanup(srv.Close)
			c := newCheckerAt(srv.URL, srv.Client())
			if got := c.Check(context.Background(), "MT12345678"); got != tc.want {
				t.Errorf("Check = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestVIESCheck_ATimeoutIsUnknown — the case Q09 is actually about.
func TestVIESCheck_ATimeoutIsUnknown(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = w.Write([]byte(`{"isValid":true}`))
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	client := srv.Client()
	// The production checker's own timeout is viesTimeout; this drives the same
	// mechanism at a length a test can wait for.
	client.Timeout = 50 * time.Millisecond
	c := newCheckerAt(srv.URL, client)
	if got := c.Check(context.Background(), "MT12345678"); got != VATUnknown {
		t.Errorf("a timed-out lookup answered %v; Q09 requires that it neither blocks the "+
			"registration nor accuses the number", got)
	}
}

// TestVIESCheck_HonoursTheCallersContext. A visitor who closed the tab must stop
// this call: the outbound request is work WE are doing on their behalf, on an
// unauthenticated endpoint.
func TestVIESCheck_HonoursTheCallersContext(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = w.Write([]byte(`{"isValid":true}`))
	}))
	t.Cleanup(func() { close(release); srv.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := newCheckerAt(srv.URL, srv.Client())
	if got := c.Check(ctx, "MT12345678"); got != VATUnknown {
		t.Errorf("Check on a cancelled context answered %v, want unknown", got)
	}
}

// TestVIESCheck_AsksForTheRightThingAndNothingElse.
//
// 🔴 THE URL IS THE SSRF ARGUMENT, MEASURED. vies.go claims the path is built from a
// constant base plus two segments that came out of an anchored pattern; this reads
// what the server actually received.
func TestVIESCheck_AsksForTheRightThingAndNothingElse(t *testing.T) {
	t.Parallel()
	var gotPath, gotAccept, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAccept, gotMethod = r.URL.Path, r.Header.Get("Accept"), r.Method
		_, _ = w.Write([]byte(`{"isValid":true}`))
	}))
	t.Cleanup(srv.Close)
	c := newCheckerAt(srv.URL, srv.Client())
	if got := c.Check(context.Background(), "MT12345678"); got != VATValid {
		t.Fatalf("Check = %v, want valid", got)
	}
	if gotPath != "/MT/vat/12345678" {
		t.Errorf("requested %q, want /MT/vat/12345678", gotPath)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("used %s; a lookup must not be a write", gotMethod)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept was %q", gotAccept)
	}
}

// TestVIESCheck_MakesNoRequestForAnythingItShouldNotAddress.
//
// The zero checker, a nil checker and a value that never passed the format check
// must all answer Unknown WITHOUT touching the network — the last one is what keeps
// "garbage costs no outbound request" true.
func TestVIESCheck_MakesNoRequestForAnythingItShouldNotAddress(t *testing.T) {
	t.Parallel()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"isValid":true}`))
	}))
	t.Cleanup(srv.Close)

	var nilChecker *Checker
	if got := nilChecker.Check(context.Background(), "MT12345678"); got != VATUnknown {
		t.Errorf("a nil checker answered %v, want unknown", got)
	}
	if got := (&Checker{}).Check(context.Background(), "MT12345678"); got != VATUnknown {
		t.Errorf("a zero checker answered %v, want unknown", got)
	}

	c := newCheckerAt(srv.URL, srv.Client())
	for _, bad := range []string{"", "MT", "ZZ12345678", "MT1234567", "not a vat number"} {
		if got := c.Check(context.Background(), bad); got != VATUnknown {
			t.Errorf("Check(%q) = %v, want unknown", bad, got)
		}
	}
	if calls != 0 {
		t.Errorf("%d outbound request(s) were made for values that never passed the format "+
			"check; garbage must cost none", calls)
	}
}

// TestVATStatus_IsTheThreeStateValueTheColumnStores — migration 00017's vocabulary.
func TestVATStatus_IsTheThreeStateValueTheColumnStores(t *testing.T) {
	t.Parallel()
	if v := VATValid.Verified(); v == nil || !*v {
		t.Error("VATValid must store true")
	}
	if v := VATInvalid.Verified(); v == nil || *v {
		t.Error("VATInvalid must store false")
	}
	if v := VATUnknown.Verified(); v != nil {
		t.Error("VATUnknown must store NULL — an outage recorded as `false` is an accusation, " +
			"which is exactly what migration 00017 made the column nullable to avoid")
	}
	// The stamp follows the same rule: a check that never happened leaves no time.
	if checkedAt(VATUnknown) != nil {
		t.Error("an unknown outcome must leave vat_checked_at NULL")
	}
	for _, s := range []VATStatus{VATValid, VATInvalid} {
		if checkedAt(s) == nil {
			t.Errorf("%v must stamp vat_checked_at", s)
		}
	}
	// The strings are STORED IN A SIGNED COOKIE and read back by internal/handler,
	// so they are part of a format rather than debug output.
	for s, want := range map[VATStatus]string{VATValid: "valid", VATInvalid: "invalid", VATUnknown: "unknown"} {
		if s.String() != want {
			t.Errorf("VATStatus(%d).String() = %q, want %q", s, s.String(), want)
		}
	}
}
