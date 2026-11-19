package tap

import (
	"net/netip"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/google/uuid"
)

// This file is M16-21's engine half (ADR 0045 §2.5; Q38, Q39, Q43; the user's decision
// of 2026-11-19): a LIVE QR tap -- a venue screen's rotating signed token, no plaque --
// decided by the same Decide, through the same policy Set, with Screen and Token in
// place of Tag and SUN. Every case carries the TestDecide_ prefix and the LiveQR word, so §5's coverage rule
// (CLAUDE.md §5: every row a TestDecide_ case) reaches the new channel too.
//
//	qr_live + IP -> ok ............................ TestDecide_LiveQREvidence/ip
//	qr_live + GPS only -> FLAG (2026-11-19) ....... TestDecide_LiveQREvidence/gps_only
//	qr_live + nothing -> flag ..................... TestDecide_LiveQREvidence/nothing
//	same person within 60 s -> ignored ............ TestDecide_LiveQRDebounce/same_person
//	nfc <-> qr_live across channels -> ignored .... TestDecide_LiveQRDebounce/across_*
//	two people, one token -> both recorded ........ TestDecide_LiveQRTwoPeopleOneToken
//	holding -> reject (sys:cross-tenant-live-qr) .. TestDecide_LiveQRHoldingIsRefused
//	forged token -> reject (Q39 a) ................ TestDecide_LiveQRForgedTokenIsRejected
//	expired token -> flag + note (Q39 b) .......... TestDecide_LiveQRExpiredTokenIsFlagged
//	revoked screen / inactive venue -> reject ..... TestDecide_LiveQRScreenNotInService
//	stale page -> reject (freshness) .............. TestDecide_LiveQRStalePageIsRejected
//	sun_valid never true .......................... TestDecide_LiveQRNeverCarriesAValidSUN
//	static QR unchanged ........................... TestDecide_LiveQRLeavesStaticQRAsItWas
//	the context's plaque keys -> the screen's ..... TestDecide_LiveQRContextNamesTheScreenNotAPlaque
//	backdated predecessor (server clock leg) ...... DB test, internal/domain/checkin

var liveQRNow = time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)

// liveQRInput is a valid, evidence-less live QR tap: a verified token seen 5 s into its
// 20 s window, an active screen at the person's home venue, a fresh page, no prior tap.
// On its own it lands on §5 row 7 (flag); each test changes the one thing it is about.
func liveQRInput() Input {
	in := baseInput()
	in.Now = liveQRNow
	in.Channel = ChannelQRLive
	in.SUN = SUNResult{}
	loc := *in.Employee.Location
	in.Tag = nil
	in.Screen = &Screen{Status: ScreenActive, Location: loc}
	in.PageIssuedAt = liveQRNow.Add(-3 * time.Second)
	// Issued 8 s before the page: 12 s of its 20 left when the server first saw it.
	in.Token = &LiveQRToken{Verified: true, ExpiresAt: in.PageIssuedAt.Add(12 * time.Second)}
	in.Client = ClientBrowser
	return in
}

var (
	liveVenueIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	liveVenueGPS = geo.Point{Lat: 35.8989, Lng: 14.5146}
)

func withVenueIP(in Input) Input {
	in.SourceIP = netip.MustParseAddr("203.0.113.7")
	in.LocationIPs = liveVenueIPs
	return in
}

func withVenueGPS(in Input) Input {
	here := liveVenueGPS
	in.GPS, in.LocationGPS = &here, &here
	return in
}

func TestDecide_LiveQREvidence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		in        Input
		want      Verdict
		wantSid   string
		wantTrust int
	}{
		{"ip", withVenueIP(liveQRInput()), VerdictOK, policy.SidIPOrGPSOK, 70},
		{"ip_and_gps", withVenueGPS(withVenueIP(liveQRInput())), VerdictOK, policy.SidIPOrGPSOK, 100},
		// The user's decision of 2026-11-19: GPS alone is not enough on a live QR either.
		{"gps_only", withVenueGPS(liveQRInput()), VerdictFlag, policy.SidLiveQRRequiresIP, 50},
		{"nothing", liveQRInput(), VerdictFlag, "", 20},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Decide(tc.in)
			if got.Verdict != tc.want || got.Trust != tc.wantTrust {
				t.Fatalf("verdict %q trust %d via %q, want %q trust %d", got.Verdict, got.Trust, got.MatchedSid, tc.want, tc.wantTrust)
			}
			if tc.wantSid != "" && got.MatchedSid != tc.wantSid {
				t.Fatalf("decided by %q, want %q", got.MatchedSid, tc.wantSid)
			}
			if got.Layer != policy.LayerBaseline {
				t.Fatalf("an evidence verdict is the baseline's, got layer %q via %q", got.Layer, got.MatchedSid)
			}
			// A recorded tap carries a direction, like any other channel.
			if got.Type == nil || *got.Type != TypeIn {
				t.Fatalf("a recorded live QR tap must carry a direction, got %v", got.Type)
			}
		})
	}
}

func TestDecide_LiveQRDebounce(t *testing.T) {
	t.Parallel()
	recent := func(ch Channel, ago time.Duration) Input {
		in := withVenueIP(liveQRInput())
		if ch != ChannelQRLive {
			in = withVenueIP(onSiteInput())
			in.Now = liveQRNow
		}
		in.LastForPerson = &Transaction{ID: uuid.New(), Direction: TypeIn, OccurredAt: liveQRNow.Add(-ago)}
		return in
	}
	// Across channels the PERSON is the key: an NFC tap 20 s after a live QR one, and the
	// other way round, is the same person again (ADR 0006: the server-clock leg spans
	// every tap channel -- checkin's SecondsSinceLastRecordedTap, measured in the DB test).
	cases := []struct {
		name string
		in   Input
		want Verdict
	}{
		{"same_person", recent(ChannelQRLive, 20*time.Second), VerdictIgnored},
		{"across_nfc_then_qr_live", recent(ChannelQRLive, 20*time.Second), VerdictIgnored},
		{"across_qr_live_then_nfc", recent(ChannelNFC, 20*time.Second), VerdictIgnored},
		{"past_the_window", recent(ChannelQRLive, 61*time.Second), VerdictOK},
	}
	// The server-clock leg alone, as checkin hands it over for a live QR predecessor.
	serverLeg := withVenueIP(liveQRInput())
	age := 12.0
	serverLeg.SecondsSinceLastRecordedTap = &age
	cases = append(cases, struct {
		name string
		in   Input
		want Verdict
	}{"server_clock_leg", serverLeg, VerdictIgnored})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := Decide(tc.in); got.Verdict != tc.want {
				t.Fatalf("verdict %q via %q, want %q", got.Verdict, got.MatchedSid, tc.want)
			}
		})
	}
}

// TestDecide_LiveQRTwoPeopleOneToken: the token is not single use (~20 s, anyone in the
// room reads it); the brake is per person. Two people, one token, a second apart: both
// recorded.
func TestDecide_LiveQRTwoPeopleOneToken(t *testing.T) {
	t.Parallel()
	first := withVenueIP(liveQRInput())
	second := withVenueIP(liveQRInput())
	second.Token = first.Token // the same token
	second.Now = first.Now.Add(time.Second)
	for i, in := range []Input{first, second} {
		if got := Decide(in); got.Verdict != VerdictOK {
			t.Fatalf("person %d: %q via %q, want ok", i+1, got.Verdict, got.MatchedSid)
		}
	}
}

// TestDecide_LiveQRHoldingIsRefused: Q43 (b), the user's decision of 2026-11-14 -- a KM
// employee at a KF screen is refused and the refusal is RECORDED (a verdict, not a
// redirect), whatever the evidence. Outside a holding the same pair redirects and
// nothing is written (sys:tenant-mismatch).
func TestDecide_LiveQRHoldingIsRefused(t *testing.T) {
	t.Parallel()
	employer, site := uuid.New(), uuid.New()
	in := withVenueGPS(withVenueIP(liveQRInput()))
	in.SessionTenantID, in.TagTenantID, in.SameHolding = &employer, &site, true
	got := Decide(in)
	if got.Verdict != VerdictReject || got.MatchedSid != "sys:cross-tenant-live-qr" || got.Redirect != RedirectNone {
		t.Fatalf("holding: %q / %q / redirect %q, want a recorded reject via sys:cross-tenant-live-qr", got.Verdict, got.MatchedSid, got.Redirect)
	}
	if got.Type != nil || got.Security {
		t.Fatalf("a refusal carries no direction and raises no alert: %+v", got)
	}
	// An expired token at a peer's screen is still the refusal, not a flag.
	in.Token = &LiveQRToken{Verified: true, ExpiresAt: in.PageIssuedAt.Add(-time.Hour)}
	if got := Decide(in); got.MatchedSid != "sys:cross-tenant-live-qr" {
		t.Fatalf("holding + expired: %q, want sys:cross-tenant-live-qr", got.MatchedSid)
	}
	in.SameHolding = false
	if got := Decide(in); got.Redirect != RedirectActivation || got.MatchedSid != "sys:tenant-mismatch" {
		t.Fatalf("outside a holding: %q / %q, want the tenant-mismatch redirect", got.Redirect, got.MatchedSid)
	}
	// Same tenant: SameHolding is meaningless and the rule stays out.
	in.TagTenantID, in.SameHolding = &employer, true
	in.Token = liveQRInput().Token
	if got := Decide(in); got.Verdict != VerdictOK {
		t.Fatalf("own screen: %q via %q, want ok", got.Verdict, got.MatchedSid)
	}
}

// TestDecide_LiveQRForgedTokenIsRejected: Q39 (a) -- a token that did not verify is a
// RECORDED reject, whatever the evidence, and inside the debounce window too (a forgery
// is never swallowed as a duplicate). No screen is believed, so the context names no
// location; a deactivated person's attempt still raises the alert (§5 row 4 wins).
func TestDecide_LiveQRForgedTokenIsRejected(t *testing.T) {
	t.Parallel()
	forged := withVenueGPS(withVenueIP(liveQRInput()))
	forged.Token = &LiveQRToken{Verified: false}
	forged.Screen = nil
	got := Decide(forged)
	if got.Verdict != VerdictReject || got.MatchedSid != "sys:live-qr-unverified" {
		t.Fatalf("forged: %q via %q, want reject via sys:live-qr-unverified", got.Verdict, got.MatchedSid)
	}
	if _, ok := got.PolicyContext[policy.CtxLocationID]; ok {
		t.Fatal("a forged token's context names a location")
	}
	if _, ok := got.PolicyContext[policy.CtxTapTokenOverdueSeconds]; ok {
		t.Fatal("a forged token's stamps reached the context")
	}
	noToken := forged
	noToken.Token = nil
	if got := Decide(noToken); got.MatchedSid != "sys:live-qr-unverified" {
		t.Fatalf("no token facts: %q, want sys:live-qr-unverified (fail-closed)", got.MatchedSid)
	}
	inWindow := forged
	inWindow.LastForPerson = &Transaction{ID: uuid.New(), Direction: TypeIn, OccurredAt: liveQRNow.Add(-10 * time.Second)}
	if got := Decide(inWindow); got.Verdict != VerdictReject {
		t.Fatalf("forged inside the window: %q via %q, want reject", got.Verdict, got.MatchedSid)
	}
	deactivated := forged
	emp := *forged.Employee
	emp.Status = EmployeeDeactivated
	deactivated.Employee = &emp
	if got := Decide(deactivated); got.MatchedSid != "sys:employee-deactivated" || !got.Security {
		t.Fatalf("deactivated + forged: %q security=%v, want the row-4 alert", got.MatchedSid, got.Security)
	}
}

// TestDecide_LiveQRExpiredTokenIsFlagged: Q39 (b) -- a genuine token first seen more
// than LiveQRNetworkGraceSeconds past its expiry is a RECORDED flag with its note, even
// with an IP match; at exactly the grace it is fresh. The moment is the page's mint,
// never the press.
func TestDecide_LiveQRExpiredTokenIsFlagged(t *testing.T) {
	t.Parallel()
	at := func(pastExpiry time.Duration) Input {
		in := withVenueIP(liveQRInput())
		in.Token = &LiveQRToken{Verified: true, ExpiresAt: in.PageIssuedAt.Add(-pastExpiry)}
		return in
	}
	grace := time.Duration(policy.LiveQRNetworkGraceSeconds) * time.Second
	late := Decide(at(grace + time.Second))
	if late.Verdict != VerdictFlag || late.MatchedSid != "sys:live-qr-expired" || late.Note == "" {
		t.Fatalf("expired: %q via %q note %q, want a flag with its note", late.Verdict, late.MatchedSid, late.Note)
	}
	if late.Type == nil {
		t.Fatal("a flagged live QR tap carries a direction (it joins the chain once approved)")
	}
	if got := Decide(at(grace)); got.Verdict != VerdictOK {
		t.Fatalf("at exactly the grace: %q via %q, want ok", got.Verdict, got.MatchedSid)
	}
	// Pressed long after a FRESH scan: not expired -- the scan moment is the page's.
	pressedLater := withVenueIP(liveQRInput())
	pressedLater.Now = pressedLater.PageIssuedAt.Add(90 * time.Second)
	if got := Decide(pressedLater); got.Verdict != VerdictOK {
		t.Fatalf("a fresh scan pressed 90 s later: %q via %q, want ok", got.Verdict, got.MatchedSid)
	}
	// No page (a caller that verifies at the press): Now is the moment.
	noPage := at(0)
	noPage.PageIssuedAt = time.Time{}
	noPage.Token = &LiveQRToken{Verified: true, ExpiresAt: noPage.Now.Add(-grace - time.Second)}
	if got := Decide(noPage); got.MatchedSid != "sys:live-qr-expired" {
		t.Fatalf("no page: %q, want sys:live-qr-expired measured at Now", got.MatchedSid)
	}
}

func TestDecide_LiveQRScreenNotInService(t *testing.T) {
	t.Parallel()
	for _, st := range []ScreenStatus{ScreenRevoked, ScreenLocationInactive, ScreenStatus("a_state_added_later")} {
		in := withVenueIP(liveQRInput())
		in.Screen.Status = st
		got := Decide(in)
		if got.Verdict != VerdictReject || got.MatchedSid != "sys:screen-not-active" || got.Security {
			t.Fatalf("%s: %q via %q security=%v, want a recorded reject via sys:screen-not-active", st, got.Verdict, got.MatchedSid, got.Security)
		}
	}
	// A verified token whose screen the caller did not resolve is not in service either.
	in := withVenueIP(liveQRInput())
	in.Screen = nil
	if got := Decide(in); got.MatchedSid != "sys:screen-not-active" {
		t.Fatalf("no screen: %q, want sys:screen-not-active", got.MatchedSid)
	}
}

// TestDecide_LiveQRStalePageIsRejected: sys:tap-freshness reaches the live channel (the
// token proves a moment; the record is timed at the press) and still not the static one.
func TestDecide_LiveQRStalePageIsRejected(t *testing.T) {
	t.Parallel()
	window := policy.DefaultParams().FreshnessWindow
	live := withVenueIP(liveQRInput())
	live.Now = live.PageIssuedAt.Add(window + time.Second)
	if got := Decide(live); got.Verdict != VerdictReject || got.MatchedSid != "sys:tap-freshness" {
		t.Fatalf("stale live QR page: %q via %q, want reject via sys:tap-freshness", got.Verdict, got.MatchedSid)
	}
	static := withVenueIP(baseInput())
	static.Channel, static.SUN = ChannelQR, SUNResult{}
	static.PageIssuedAt = static.Now.Add(-window - time.Minute)
	if got := Decide(static); got.Verdict != VerdictOK {
		t.Fatalf("stale STATIC QR page: %q via %q, want ok (freshness is not a QR rule)", got.Verdict, got.MatchedSid)
	}
}

// TestDecide_LiveQRNeverCarriesAValidSUN is M16-19's hand-off: whatever SUN a caller
// passes on a live QR tap, tap:sunValid -- which the record's sun_valid copies -- is
// false. Both values of the input are tried; NFC is the control.
func TestDecide_LiveQRNeverCarriesAValidSUN(t *testing.T) {
	t.Parallel()
	for _, valid := range []bool{true, false} {
		in := withVenueIP(liveQRInput())
		in.SUN = SUNResult{Valid: valid}
		got := Decide(in)
		if v, _ := got.PolicyContext[policy.CtxTapSunValid].(bool); v {
			t.Fatalf("SUN.Valid=%v on a live QR tap put tap:sunValid=true in the context", valid)
		}
		if got.Verdict != VerdictOK {
			t.Fatalf("SUN.Valid=%v moved a live QR verdict: %q via %q", valid, got.Verdict, got.MatchedSid)
		}
	}
	if v, _ := Decide(onSiteInput()).PolicyContext[policy.CtxTapSunValid].(bool); !v {
		t.Fatal("CONTROL: an NFC tap with a valid SUN lost tap:sunValid")
	}
}

// TestDecide_LiveQRLeavesStaticQRAsItWas: the four live QR guardrails and the new
// baseline document key on qr_live; a static QR tap carrying the WORST live QR inputs
// (a forged token, a revoked screen) is decided exactly as without them.
func TestDecide_LiveQRLeavesStaticQRAsItWas(t *testing.T) {
	t.Parallel()
	shapes := map[string]func(Input) Input{
		"ip": withVenueIP, "gps_only": withVenueGPS, "nothing": func(in Input) Input { return in },
	}
	for name, shape := range shapes {
		plain := shape(baseInput())
		plain.Channel, plain.SUN = ChannelQR, SUNResult{}
		noisy := plain
		noisy.Token = &LiveQRToken{Verified: false}
		noisy.Screen = &Screen{Status: ScreenRevoked, Location: uuid.New()}
		a, b := Decide(plain), Decide(noisy)
		if a.Verdict != b.Verdict || a.MatchedSid != b.MatchedSid || a.Trust != b.Trust || len(a.PolicyContext) != len(b.PolicyContext) {
			t.Fatalf("%s: static QR moved: %q/%q/%d -> %q/%q/%d", name, a.Verdict, a.MatchedSid, a.Trust, b.Verdict, b.MatchedSid, b.Trust)
		}
	}
	// And the static outcomes themselves are the shipped ones.
	gps := withVenueGPS(baseInput())
	gps.Channel, gps.SUN = ChannelQR, SUNResult{}
	if got := Decide(gps); got.Verdict != VerdictFlag || got.MatchedSid != policy.SidQRRequiresIP {
		t.Fatalf("static QR + GPS: %q via %q, want flag via %s", got.Verdict, got.MatchedSid, policy.SidQRRequiresIP)
	}
}

// TestDecide_LiveQRContextNamesTheScreenNotAPlaque is the parity rule for the context's
// plaque keys (the card's "ekran karşılıkları"): on a live QR tap tag:status and
// tag:locationId are ABSENT, screen:status is present, location:id is the screen's venue
// and the policy resource is that venue -- so a branch override written for location/<id>
// applies to the live QR tap at that branch exactly as to its plaque. Every other key is
// the one a static QR tap at the same venue with the same evidence carries.
func TestDecide_LiveQRContextNamesTheScreenNotAPlaque(t *testing.T) {
	t.Parallel()
	live := withVenueGPS(withVenueIP(liveQRInput()))
	static := withVenueGPS(withVenueIP(baseInput()))
	static.Now, static.PageIssuedAt, static.Client, static.Employee = live.Now, live.PageIssuedAt, live.Client, live.Employee
	static.Channel, static.SUN = ChannelQR, SUNResult{}
	static.Tag = &Tag{Status: TagActive, Location: live.Screen.Location}
	l, s := Decide(live).PolicyContext, Decide(static).PolicyContext
	for _, k := range []policy.ContextKey{policy.CtxTagStatus, policy.CtxTagLocationID} {
		if _, ok := l[k]; ok {
			t.Fatalf("a live QR context carries the plaque key %s", k)
		}
	}
	if l[policy.CtxScreenStatus] != string(ScreenActive) || l[policy.CtxLocationID] != live.Screen.Location.String() {
		t.Fatalf("screen keys: %v / %v", l[policy.CtxScreenStatus], l[policy.CtxLocationID])
	}
	known := map[policy.ContextKey]bool{
		policy.CtxTapChannel: true, policy.CtxTagStatus: true, policy.CtxTagLocationID: true,
		policy.CtxScreenStatus: true, policy.CtxTapTokenVerified: true, policy.CtxTapTokenOverdueSeconds: true,
	}
	for k, v := range s {
		if known[k] {
			continue
		}
		if l[k] != v {
			t.Errorf("%s: live %v, static %v", k, l[k], v)
		}
	}
	for k := range l {
		if _, ok := s[k]; !ok && !known[k] {
			t.Errorf("%s is only on the live QR context", k)
		}
	}
	// The resource: a tenant rule at THIS venue decides the live QR tap.
	only := policy.Set{Guardrails: policy.DefaultGuardrails(), Tenant: []policy.Policy{{
		VersionID: uuid.New(), Layer: policy.LayerTenant,
		Document: policy.Document{Version: "2026-11-19", Statements: []policy.Statement{{
			Sid: "own:venue", Effect: policy.EffectReview, Action: []policy.Action{policy.ActionTapRecord},
			Resource: []string{"location/" + live.Screen.Location.String()}, Reason: "this venue reviews everything",
		}}},
	}}}
	live.PolicySet = only
	if got := Decide(live); got.MatchedSid != "own:venue" {
		t.Fatalf("a venue-scoped rule did not reach the live QR tap at that venue: %q", got.MatchedSid)
	}
}
