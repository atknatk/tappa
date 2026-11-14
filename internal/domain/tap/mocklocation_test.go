package tap

import (
	"fmt"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/google/uuid"
)

// This file is M16-06 (ADR 0045 §2.4): tap.Input carries Client and MockLocation,
// both reach the policy context, and base:mock-location-review sends a tap whose
// app reported a mock position to review. The browser path -- no MockLocation --
// must decide every shape exactly as it did before the rule existed.

func boolp(b bool) *bool { return &b }

// mockShapes are evidence shapes over the inputs the baseline reads: channel, IP,
// GPS (none / near / far), a counter gap, a queued declared time and a tap at
// another location. 2 × 2 × 3 × 2 × 2 × 2 = 96 inputs.
func mockShapes() []struct {
	name string
	in   Input
} {
	onNet := netip.MustParseAddr("203.0.113.7")
	prefix := netip.MustParsePrefix("203.0.113.0/24")
	venue := geo.Point{Lat: 35.8989, Lng: 14.5146}
	far := geo.Point{Lat: 35.9100, Lng: 14.5300}
	var out []struct {
		name string
		in   Input
	}
	for _, ch := range []Channel{ChannelNFC, ChannelQR} {
		for _, ip := range []bool{false, true} {
			for _, gps := range []string{"none", "near", "far"} {
				for _, gap := range []int{0, 3} {
					for _, queued := range []bool{false, true} {
						for _, cross := range []bool{false, true} {
							in := baseInput()
							in.Channel = ch
							if ch == ChannelQR {
								in.SUN = SUNResult{}
							} else {
								in.SUN.CtrGap = gap
							}
							in.LocationIPs = []netip.Prefix{prefix}
							if ip {
								in.SourceIP = onNet
							}
							in.LocationGPS = &venue
							switch gps {
							case "near":
								in.GPS = &venue
							case "far":
								in.GPS = &far
							}
							if queued {
								in.OccurredAt, in.OccurredAtFromClient = in.Now.Add(-10*time.Minute), true
							}
							if cross {
								other := uuid.New()
								in.Employee.Location = &other
							}
							out = append(out, struct {
								name string
								in   Input
							}{fmt.Sprintf("%s ip=%v gps=%s gap=%d queued=%v cross=%v", ch, ip, gps, gap, queued, cross), in})
						}
					}
				}
			}
		}
	}
	return out
}

// setWithoutMockRule is the production-shaped Set as it stood before M16-06: the
// same guardrails and baseline, minus base:mock-location-review.
func setWithoutMockRule() policy.Set {
	set := testSet()
	var kept []policy.Policy
	for _, p := range set.Baseline {
		if p.Document.Statements[0].Sid != policy.SidMockLocationReview {
			kept = append(kept, p)
		}
	}
	if len(kept) != len(set.Baseline)-1 {
		panic("test bug: the shipped baseline has no mock-location document")
	}
	set.Baseline = kept
	return set
}

func wantTrust(d Decision) int {
	t := 20
	if d.IPMatch {
		t += 50
	}
	if d.GPSMatch {
		t += 30
	}
	return t
}

// withoutKeys is d with its policy context stripped of the given keys -- every
// other field compared as it is.
func withoutKeys(d Decision, keys ...policy.ContextKey) Decision {
	ctx := make(map[policy.ContextKey]any, len(d.PolicyContext))
	for k, v := range d.PolicyContext {
		ctx[k] = v
	}
	for _, k := range keys {
		delete(ctx, k)
	}
	d.PolicyContext = ctx
	return d
}

// TestDecide_MockLocationReported is the card's table: a reported mock position is
// a RECORDED flag whatever the place evidence, the trust score is the formula's,
// and a report of "not mock" decides like no report.
func TestDecide_MockLocationReported(t *testing.T) {
	t.Parallel()
	onNet := netip.MustParseAddr("203.0.113.7")
	prefix := netip.MustParsePrefix("203.0.113.0/24")
	venue := geo.Point{Lat: 35.8989, Lng: 14.5146}

	ipOnly := func(in *Input) { in.SourceIP, in.LocationIPs = onNet, []netip.Prefix{prefix} }
	gpsOnly := func(in *Input) { in.GPS, in.LocationGPS = &venue, &venue }
	both := func(in *Input) { ipOnly(in); gpsOnly(in) }
	none := func(*Input) {}
	qr := func(in *Input) { in.Channel, in.SUN = ChannelQR, SUNResult{} }

	tests := []struct {
		name      string
		evidence  func(*Input)
		mock      *bool
		wantVerd  Verdict
		wantSid   string
		wantTrust int
	}{
		{"mock_plus_ip_flags", ipOnly, boolp(true), VerdictFlag, policy.SidMockLocationReview, 70},
		{"mock_plus_gps_only_flags", gpsOnly, boolp(true), VerdictFlag, policy.SidMockLocationReview, 50},
		{"mock_plus_ip_and_gps_flags", both, boolp(true), VerdictFlag, policy.SidMockLocationReview, 100},
		// No evidence is already a review; the earlier document in order names it.
		{"mock_plus_no_evidence_flags", none, boolp(true), VerdictFlag, policy.SidNoEvidenceReview, 20},
		{"qr_mock_plus_ip_flags", func(in *Input) { qr(in); ipOnly(in) }, boolp(true), VerdictFlag, policy.SidMockLocationReview, 70},
		{"not_mock_plus_ip_ok", ipOnly, boolp(false), VerdictOK, policy.SidIPOrGPSOK, 70},
		{"not_mock_plus_gps_only_ok", gpsOnly, boolp(false), VerdictOK, policy.SidGPSOnlyAllow, 50},
		{"no_report_plus_ip_ok", ipOnly, nil, VerdictOK, policy.SidIPOrGPSOK, 70},
		{"no_report_plus_gps_only_ok", gpsOnly, nil, VerdictOK, policy.SidGPSOnlyAllow, 50},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := baseInput()
			tc.evidence(&in)
			in.Client = ClientApp
			in.MockLocation = tc.mock
			got := Decide(in)
			if got.Verdict != tc.wantVerd || got.MatchedSid != tc.wantSid {
				t.Fatalf("got %q via %q, want %q via %q", got.Verdict, got.MatchedSid, tc.wantVerd, tc.wantSid)
			}
			if got.Redirect != "" {
				t.Fatalf("a mock-location report must be RECORDED (§4.6); Redirect = %q", got.Redirect)
			}
			if got.Trust != tc.wantTrust || got.Trust != wantTrust(got) {
				t.Fatalf("Trust = %d, want %d -- the formula 20 + 50 + 30 does not read the report", got.Trust, tc.wantTrust)
			}
			v, present := got.PolicyContext[policy.CtxTapMockLocation]
			if tc.mock == nil {
				if present {
					t.Fatalf("no report put tap:mockLocation = %v in the context", v)
				}
			} else if !present || v != *tc.mock {
				t.Fatalf("tap:mockLocation = %v (present %v), want %v", v, present, *tc.mock)
			}
		})
	}
}

// TestDecide_MockLocationAbsentLeavesEveryOutcomeAsItWas is the browser path's
// regression (the card's trap: MockLocation is filled on /app/v1 only). Over every
// evidence shape, with no report, Decide under the shipped baseline returns
// EXACTLY the Decision it returned under the baseline as it stood before the mock
// rule -- verdict, sid, version, reason, trust, direction, notes and context. A
// report of false changes nothing but the context key it adds. And a true report
// only ever TIGHTENS: an ok becomes a flag, anything else is untouched.
func TestDecide_MockLocationAbsentLeavesEveryOutcomeAsItWas(t *testing.T) {
	t.Parallel()
	before := setWithoutMockRule()
	tightened := 0
	for _, sh := range mockShapes() {
		for _, client := range []Client{"", ClientBrowser, ClientApp} {
			in := sh.in
			in.Client = client
			old := in
			old.PolicySet = before
			want := Decide(old)

			if got := Decide(in); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s client=%q: with no report the outcome changed:\n got  %q via %q\n want %q via %q",
					sh.name, client, got.Verdict, got.MatchedSid, want.Verdict, want.MatchedSid)
			}

			in.MockLocation = boolp(false)
			if got := Decide(in); !reflect.DeepEqual(withoutKeys(got, policy.CtxTapMockLocation), want) {
				t.Fatalf("%s client=%q: a report of false changed the outcome: %q via %q, want %q via %q",
					sh.name, client, got.Verdict, got.MatchedSid, want.Verdict, want.MatchedSid)
			}

			in.MockLocation = boolp(true)
			got := Decide(in)
			if got.Trust != want.Trust || got.IPMatch != want.IPMatch || got.GPSMatch != want.GPSMatch {
				t.Fatalf("%s: a mock report moved the evidence or the trust: trust %d -> %d", sh.name, want.Trust, got.Trust)
			}
			if want.Verdict == VerdictOK {
				if got.Verdict != VerdictFlag || got.MatchedSid != policy.SidMockLocationReview {
					t.Fatalf("%s: an ok tap with a mock report is %q via %q, want a flag via %s",
						sh.name, got.Verdict, got.MatchedSid, policy.SidMockLocationReview)
				}
				tightened++
				continue
			}
			if got.Verdict != want.Verdict {
				t.Fatalf("%s: a mock report turned %q into %q -- it may only turn ok into flag",
					sh.name, want.Verdict, got.Verdict)
			}
		}
	}
	if tightened == 0 {
		t.Fatal("no shape was ok, so the tightening half proved nothing")
	}
}

// TestDecide_ClientAloneChangesNoOutcome: Client reaches the context as tap:client
// (only for a known value) and changes nothing else -- not the verdict, the sid,
// the trust, the direction or any other key.
func TestDecide_ClientAloneChangesNoOutcome(t *testing.T) {
	t.Parallel()
	for _, sh := range mockShapes() {
		base := Decide(sh.in) // Client zero: no client recorded
		if _, ok := base.PolicyContext[policy.CtxTapClient]; ok {
			t.Fatalf("%s: a tap with no client carries tap:client", sh.name)
		}
		for _, tc := range []struct {
			client Client
			key    bool
		}{{ClientBrowser, true}, {ClientApp, true}, {Client("ios"), false}} {
			in := sh.in
			in.Client = tc.client
			got := Decide(in)
			v, present := got.PolicyContext[policy.CtxTapClient]
			if present != tc.key || (tc.key && v != string(tc.client)) {
				t.Fatalf("%s client=%q: tap:client = %v (present %v)", sh.name, tc.client, v, present)
			}
			if !reflect.DeepEqual(withoutKeys(got, policy.CtxTapClient), base) {
				t.Fatalf("%s client=%q: the client changed the outcome: %q via %q (trust %d), want %q via %q (trust %d)",
					sh.name, tc.client, got.Verdict, got.MatchedSid, got.Trust, base.Verdict, base.MatchedSid, base.Trust)
			}
		}
	}
}

// TestDecide_TenantRuleCanReadClient: the keys are there so a business can write a
// rule with them (ADR 0045 §2.4). A tenant review on tap:client = app holds an app
// tap and lets the same browser tap through.
func TestDecide_TenantRuleCanReadClient(t *testing.T) {
	t.Parallel()
	set := testSet()
	set.Tenant = []policy.Policy{{
		VersionID: uuid.New(), Layer: policy.LayerTenant,
		Document: policy.Document{Version: "v", Name: "app taps need a look", Statements: []policy.Statement{{
			Sid: "own:app-review", Effect: policy.EffectReview, Action: []policy.Action{policy.ActionTapRecord},
			Resource:  []string{"location/*"},
			Condition: policy.Condition{policy.OpStringEquals: {policy.CtxTapClient: string(ClientApp)}},
			Reason:    "app taps are reviewed here",
		}}},
	}}
	for _, tc := range []struct {
		client   Client
		wantVerd Verdict
		wantSid  string
	}{
		{ClientBrowser, VerdictOK, policy.SidIPOrGPSOK},
		{ClientApp, VerdictFlag, "own:app-review"},
	} {
		in := onSiteInput()
		in.PolicySet = set
		in.Client = tc.client
		if got := Decide(in); got.Verdict != tc.wantVerd || got.MatchedSid != tc.wantSid {
			t.Fatalf("client=%q: got %q via %q, want %q via %q", tc.client, got.Verdict, got.MatchedSid, tc.wantVerd, tc.wantSid)
		}
	}
}

// TestDecide_MockLocationLeavesTheGuardrailsAsTheyWere: §5 rows 1-5 are guardrails and
// terminal, so a mock-location report changes none of them -- lost, retired, SUN invalid,
// no session, deactivated and the per-person repeat each come back as the same Decision
// (verdict, sid, layer, trust, redirect, security alert) with or without the report; only
// the frozen context carries it.
func TestDecide_MockLocationLeavesTheGuardrailsAsTheyWere(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(*Input)
		wantSid string
	}{
		{"row1_lost_tag", func(in *Input) { in.Tag.Status = TagLost }, "sys:tag-not-active"},
		{"row1_retired_tag", func(in *Input) { in.Tag.Status = TagRetired }, "sys:tag-not-active"},
		{"row2_sun_invalid", func(in *Input) { in.SUN = SUNResult{Valid: false} }, "sys:sun-invalid"},
		{"row3_no_session", func(in *Input) { in.Employee = nil }, "sys:no-session"},
		{"row4_deactivated", func(in *Input) { in.Employee.Status = EmployeeDeactivated }, "sys:employee-deactivated"},
		{"row5_person_debounce", func(in *Input) {
			in.LastForPerson = &Transaction{OccurredAt: in.Now.Add(-20 * time.Second), Direction: TypeIn}
		}, "sys:person-debounce"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, client := range []Client{ClientApp, ClientBrowser} {
				in := onSiteInput()
				in.Client = client
				tc.mutate(&in)
				want := Decide(in)
				if want.MatchedSid != tc.wantSid {
					t.Fatalf("precondition: %q, want %q", want.MatchedSid, tc.wantSid)
				}
				in.MockLocation = boolp(true)
				got := Decide(in)
				if got.PolicyContext[policy.CtxTapMockLocation] != true {
					t.Fatalf("the report is missing from the context")
				}
				if !reflect.DeepEqual(withoutKeys(got, policy.CtxTapMockLocation), want) {
					t.Fatalf("client=%q: a mock report changed a guardrail's decision: %q via %q (trust %d), want %q via %q (trust %d)",
						client, got.Verdict, got.MatchedSid, got.Trust, want.Verdict, want.MatchedSid, want.Trust)
				}
			}
		})
	}
}
