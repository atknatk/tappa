package signup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
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
				// A body past the 16 KiB bound, at a FIXED size (17 KiB) rather than one
				// built from viesMaxBody, which grew with the constant and left a bound
				// raised to 16 MiB green (OP-16 B audit, D1; the byte edge is
				// TestVIESCheck_TheBodyBoundIsSixteenKiBToTheByte). It even STARTS like a
				// valid answer, which is the case io.LimitReader would have turned into a
				// confident wrong verdict — see limitedReader.
				_, _ = w.Write([]byte(`{"isValid":true,"padding":"` + strings.Repeat("x", 17<<10) + `"}`))
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

// TestVIESCheck_UserErrorDecidesWhetherIsValidIsAnAnswer — OP-16C.
//
// 🔴 THE OUTAGE vies.go's HEADER NAMED AS COMMON ARRIVES AS A 200. A member state's
// register being down comes back with `isValid` false beside a userError code, and a
// client that read isValid alone stored it as "this VAT number is not valid" — an
// accusation built out of somebody else's outage. Every row is served by a local
// server and read through Check, so what is measured is the path a registration
// takes, not the rule in isolation.
//
// THE CODES ARE THE SERVICE'S OWN VOCABULARY (vies.go says where each was read). The
// rule does not enumerate them — anything but VALID and INVALID is no answer — so the
// list is a sample the rule must hold for rather than a list it depends on; the
// undocumented-code rows pin the "anything else" half.
//
// ROUND 2 added the real-shaped answers (E01) and the Valid side's spelling (E04).
func TestVIESCheck_UserErrorDecidesWhetherIsValidIsAnAnswer(t *testing.T) {
	t.Parallel()
	failures := []string{
		"INVALID_INPUT", "INVALID_REQUESTER_INFO", "SERVICE_UNAVAILABLE", "MS_UNAVAILABLE",
		"TIMEOUT", "VAT_BLOCKED", "IP_BLOCKED", "GLOBAL_MAX_CONCURRENT_REQ",
		"GLOBAL_MAX_CONCURRENT_REQ_TIME", "MS_MAX_CONCURRENT_REQ", "MS_MAX_CONCURRENT_REQ_TIME",
	}
	type row struct {
		name string
		body string
		want VATStatus
	}
	tests := []row{
		// The two verdicts, in every shape that is one.
		{"true beside VALID", `{"isValid":true,"userError":"VALID"}`, VATValid},
		{"true with no userError", `{"isValid":true}`, VATValid},
		{"true beside a null userError", `{"isValid":true,"userError":null}`, VATValid},
		{"true beside an empty userError", `{"isValid":true,"userError":""}`, VATValid},
		{"false beside INVALID", `{"isValid":false,"userError":"INVALID"}`, VATInvalid},

		// A false that is not a verdict.
		{"false with no userError", `{"isValid":false}`, VATUnknown},
		{"false beside a null userError", `{"isValid":false,"userError":null}`, VATUnknown},
		{"false beside an empty userError", `{"isValid":false,"userError":""}`, VATUnknown},
		{"false beside VALID, a contradiction", `{"isValid":false,"userError":"VALID"}`, VATUnknown},
		{"false beside an undocumented code", `{"isValid":false,"userError":"SOMETHING_NEW"}`, VATUnknown},
		{"false beside a lower-case invalid", `{"isValid":false,"userError":"invalid"}`, VATUnknown},
		{"false beside a padded INVALID", `{"isValid":false,"userError":" INVALID"}`, VATUnknown},

		// A true that is not a verdict.
		{"true beside INVALID, a contradiction", `{"isValid":true,"userError":"INVALID"}`, VATUnknown},
		{"true beside an undocumented code", `{"isValid":true,"userError":"SOMETHING_NEW"}`, VATUnknown},

		// No isValid at all: before OP-16C every one of these decoded to false.
		{"an empty object", `{}`, VATUnknown},
		{"INVALID with no isValid", `{"userError":"INVALID"}`, VATUnknown},
		{"VALID with no isValid", `{"userError":"VALID"}`, VATUnknown},
		{"INVALID beside a null isValid", `{"isValid":null,"userError":"INVALID"}`, VATUnknown},
		{"the POST endpoint's error envelope",
			`{"actionSucceed":false,"errorWrappers":[{"error":"MS_UNAVAILABLE","message":"x"}]}`, VATUnknown},

		// A userError that is not a string does not decode at all.
		{"a userError that is a number", `{"isValid":false,"userError":301}`, VATUnknown},

		// Round 2: the Valid side's spelling, as the INVALID side's is pinned above.
		{"true beside a lower-case valid", `{"isValid":true,"userError":"valid"}`, VATUnknown},
		{"true beside a padded VALID", `{"isValid":true,"userError":" VALID"}`, VATUnknown},

		// Round 2: THE REAL SHAPE, WITH FAKE VALUES. Every fixture above carries only the
		// two fields this client reads, so a decoder that refused the fields VIES really
		// sends (DisallowUnknownFields) passed all of them (measured, E01). These two
		// carry the rest of the answer as third-party clients report it — requestDate,
		// name, address, viesApproximate — and viesApproximate is given isValid and
		// userError keys of its own saying the OPPOSITE, so a reader that took those keys
		// from anywhere but the top level would turn the verdict round.
		{"the real shape, valid", `{"isValid":true,"requestDate":"2026-10-07T09:30:00.000Z",` +
			`"userError":"VALID","name":"PROBE TRADING LTD","address":"1 PROBE STREET, NOWHERE",` +
			`"requestIdentifier":"","originalVatNumber":"12345678","vatNumber":"12345678",` +
			`"viesApproximate":{"name":"---","street":"---","postalCode":"---","city":"---",` +
			`"companyType":"---","matchName":3,"matchStreet":3,"matchPostalCode":3,"matchCity":3,` +
			`"matchCompanyType":3,"isValid":false,"userError":"INVALID"}}`, VATValid},
		{"the real shape, invalid", `{"isValid":false,"requestDate":"2026-10-07T09:30:00.000Z",` +
			`"userError":"INVALID","name":"---","address":"---",` +
			`"requestIdentifier":"","originalVatNumber":"12345678","vatNumber":"12345678",` +
			`"viesApproximate":{"name":"---","street":"---","postalCode":"---","city":"---",` +
			`"companyType":"---","matchName":3,"matchStreet":3,"matchPostalCode":3,"matchCity":3,` +
			`"matchCompanyType":3,"isValid":true,"userError":"VALID"}}`, VATInvalid},
	}
	for _, code := range failures {
		tests = append(tests,
			row{"false beside " + code, `{"isValid":false,"userError":"` + code + `"}`, VATUnknown},
			row{"true beside " + code + ", a contradiction", `{"isValid":true,"userError":"` + code + `"}`, VATUnknown},
		)
	}
	// ANTI-VACUITY: a table with no verdict row would pass against a Check that always
	// answers Unknown, which is exactly the direction this test cannot otherwise see.
	verdicts := map[VATStatus]int{}
	for _, tc := range tests {
		verdicts[tc.want]++
	}
	if len(failures) != 11 || verdicts[VATValid] != 5 || verdicts[VATInvalid] != 2 {
		t.Fatalf("the table drifted: %d failure codes, %d valid and %d invalid row(s); want "+
			"11, 5 and 2", len(failures), verdicts[VATValid], verdicts[VATInvalid])
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			c := newCheckerAt(srv.URL, srv.Client())
			if got := c.Check(context.Background(), "MT12345678"); got != tc.want {
				t.Errorf("Check over %s = %v, want %v", tc.body, got, tc.want)
			}
			// The answer was READ: an Unknown that never reached the server would pass
			// every Unknown row above for the wrong reason.
			if n := calls.Load(); n != 1 {
				t.Errorf("the local server was asked %d time(s), want exactly 1", n)
			}
		})
	}
}

// TestVIESCheck_AnOutageInsideA200IsStoredAsNoVerdict — what OP-16C changes in the
// tenants row a registration writes.
//
// Provision hands CreateTenant `VAT.Verified()` and `checkedAt(VAT)` for the two VIES
// columns (signup.go), and this reads those two functions over Check's answer — the
// values Provision passes, not the row. What Postgres then holds, for all three
// statuses, is read back by TestSignupProvision_CreatesTheWholeBusinessInOneTransaction
// (round 2; it alone sees a change to the CreateTenant call itself). Before OP-16C the
// outage below wrote `false` and a timestamp — migration
// 00017's "VIES answered: not a valid number" — and the panel called it "Not found".
// Now it writes NULL in both, the cohort the sign-up screen words as "we could not
// reach the EU VAT register" and the account screen as "Not checked". The INVALID row
// is the control: a real refusal is still written as one.
func TestVIESCheck_AnOutageInsideA200IsStoredAsNoVerdict(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		body         string
		wantVerified *bool
		wantStamped  bool
	}{
		{"a member state's register is down", `{"isValid":false,"userError":"MS_UNAVAILABLE"}`, nil, false},
		{"the register does not know the number", `{"isValid":false,"userError":"INVALID"}`, new(bool), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			status := newCheckerAt(srv.URL, srv.Client()).Check(context.Background(), "MT12345678")
			got := status.Verified()
			switch {
			case tc.wantVerified == nil && got != nil:
				t.Errorf("%s stores vat_verified = %v; an outage is no verdict and must store NULL",
					tc.body, *got)
			case tc.wantVerified != nil && (got == nil || *got != *tc.wantVerified):
				t.Errorf("%s stores vat_verified = %v, want %v", tc.body, got, *tc.wantVerified)
			}
			if stamped := checkedAt(status) != nil; stamped != tc.wantStamped {
				t.Errorf("%s stamps vat_checked_at: %v, want %v", tc.body, stamped, tc.wantStamped)
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
