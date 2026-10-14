package tap

// The two Input additions M5-05 needed, proven where they are PURE: the tenant
// pair that makes sys:tenant-mismatch bite (hand-off N5) and the declared tap
// time that feeds sys:occurred-at-bound (trap K1). Everything here is a function
// of one value; the end-to-end HTTP proof lives in internal/handler.

import (
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/policy"
)

// evidencedInput is a tap with PERFECT proof of place — the venue's own network —
// so that anything other than `ok` is the rule under test firing and not weak
// evidence. It is the exact shape of the cross-tenant attack: an employee of one
// organisation, standing physically inside another, where every evidence matches.
func evidencedInput() Input {
	in := baseInput()
	in.SourceIP = netip.MustParseAddr("203.0.113.5")
	in.LocationIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	return in
}

// TestDecide_TenantMismatchRedirectsAndWritesNothing is §4.5 in the decision
// engine, and the mutation half is the point: WITHOUT the two tenant ids the very
// same tap is a plain `ok`, which is the hole state.md tracked as N5 and which
// this test would go green on if the feed were ever removed.
func TestDecide_TenantMismatchRedirectsAndWritesNothing(t *testing.T) {
	t.Parallel()
	tenantA, tenantB := uuid.New(), uuid.New()

	// FED: the session belongs to B, the tag to A.
	fed := evidencedInput()
	fed.TagTenantID, fed.SessionTenantID = &tenantA, &tenantB
	got := Decide(fed)
	if got.Redirect != RedirectActivation {
		t.Fatalf("redirect = %q, want a redirect: a cross-tenant tap must write NO record", got.Redirect)
	}
	if got.MatchedSid != "sys:tenant-mismatch" {
		t.Fatalf("sid = %q, want sys:tenant-mismatch", got.MatchedSid)
	}
	if got.Verdict != "" {
		t.Fatalf("verdict = %q, want empty: a redirect is the ABSENCE of a record", got.Verdict)
	}

	// SAME TENANT: nothing changes for an ordinary tap.
	same := evidencedInput()
	same.TagTenantID, same.SessionTenantID = &tenantA, &tenantA
	if got := Decide(same); got.Verdict != VerdictOK || got.Redirect != RedirectNone {
		t.Fatalf("same-tenant tap = %q/%q, want ok and no redirect", got.Verdict, got.Redirect)
	}

	// 🔴 THE MUTATION: remove the feed. This is the engine as it stood before
	// M5-05, and it is what makes the assertion above non-vacuous.
	starved := evidencedInput()
	if got := Decide(starved); got.Verdict != VerdictOK {
		t.Fatalf("with no tenant ids the cross-tenant tap produced %q; this test is no longer measuring "+
			"the hole it claims to (unfed, it must be a plain ok)", got.Verdict)
	}
}

// TestDecide_TenantMismatchNeverFiresOnThePresenceMarker. Decide falls back to a
// value-less placeholder when a caller supplies no session tenant, and comparing
// a REAL tag tenant against that placeholder would manufacture a mismatch — which
// redirects, which writes NOTHING. That would be a §4.6 record loss caused by a
// missing argument, so the placeholder branch withholds the tag tenant instead.
func TestDecide_TenantMismatchNeverFiresOnThePresenceMarker(t *testing.T) {
	t.Parallel()
	tagTenant := uuid.New()

	in := evidencedInput()
	in.TagTenantID = &tagTenant // ... and SessionTenantID deliberately left nil

	got := Decide(in)
	if got.Redirect != RedirectNone {
		t.Fatalf("a tap with a tag tenant but no session tenant redirected (sid %q): the presence placeholder "+
			"was compared as if it were a real tenant, and a redirect writes no record", got.MatchedSid)
	}
	if got.Verdict != VerdictOK {
		t.Fatalf("verdict = %q, want ok", got.Verdict)
	}
}

// TestDecide_NoSessionStillRedirectsWithTenantsPresent. sys:tenant-mismatch is
// ordered FIRST, ahead of sys:no-session — so a request with no session must
// still take the no-session branch rather than the mismatch one, because the
// mismatch guardrail requires BOTH ids and there is no session id to have.
func TestDecide_NoSessionStillRedirectsWithTenantsPresent(t *testing.T) {
	t.Parallel()
	tagTenant := uuid.New()

	in := evidencedInput()
	in.Employee = nil
	in.TagTenantID = &tagTenant

	got := Decide(in)
	if got.MatchedSid != "sys:no-session" {
		t.Fatalf("sid = %q, want sys:no-session", got.MatchedSid)
	}
	if got.Redirect != RedirectActivation {
		t.Fatalf("redirect = %q, want the activation redirect", got.Redirect)
	}
}

// TestDecide_OccurredAtBoundIsFedOnEveryTap is trap K1's structural half: the
// guardrail can only fire if the key is PRESENT, and a missing key never matches
// (M3-04's invariant) — which is how a guardrail goes silently dead. So the skew
// is in the context of EVERY tap, including the ordinary live one where it is 0.
func TestDecide_OccurredAtBoundIsFedOnEveryTap(t *testing.T) {
	t.Parallel()
	got := Decide(evidencedInput()) // no OccurredAt at all: the live case

	skew, ok := got.PolicyContext[policy.CtxTapOccurredAtSkewSeconds]
	if !ok {
		t.Fatal("tap:occurredAtSkewSeconds is missing: sys:occurred-at-bound would silently never fire")
	}
	if s, _ := skew.(float64); s != 0 {
		t.Fatalf("skew = %v on a live tap, want 0", skew)
	}
	queued, ok := got.PolicyContext[policy.CtxTapQueued]
	if !ok {
		t.Fatal("tap:queued is missing from the context")
	}
	if q, _ := queued.(bool); q {
		t.Fatal("a live tap was marked queued")
	}
	if got.Verdict != VerdictOK {
		t.Fatalf("verdict = %q, want ok: the live case must not be caught by the bound", got.Verdict)
	}
}

// TestDecide_OccurredAtBoundDeniesTheImplausible. A tap cannot have happened
// later than the moment the server is judging it, and one older than the tenant's
// tolerance is refused — as a RECORDED reject, which is the whole difference
// between a guardrail and an HTTP error.
func TestDecide_OccurredAtBoundDeniesTheImplausible(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		offset   time.Duration
		wantDeny bool
	}{
		{"an hour in the future", time.Hour, true},
		{"one second in the future", time.Second, true},
		{"exactly now", 0, false},
		{"an hour ago", -time.Hour, false},
		{"71 hours ago, inside the ceiling", -71 * time.Hour, false},
		{"73 hours ago, past the ceiling", -73 * time.Hour, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := evidencedInput()
			in.OccurredAt = in.Now.Add(tc.offset)
			in.OccurredAtFromClient = true

			got := Decide(in)
			denied := got.Verdict == VerdictReject && got.MatchedSid == "sys:occurred-at-bound"
			if denied != tc.wantDeny {
				t.Fatalf("verdict/sid = %q/%q, wantDeny = %v", got.Verdict, got.MatchedSid, tc.wantDeny)
			}
			if got.Verdict == VerdictReject && got.Redirect != RedirectNone {
				t.Fatal("a denied tap must still be RECORDED (§4.6), never redirected away")
			}
		})
	}
}

// TestDecide_QueuedIsDerivedNeverDeclared. tap:queued says "this came from an
// offline queue", and ADR 0004 §8 forbids a client declaring it — Input carries
// no queued flag at all, so the only way to set it is to have declared a time
// that is genuinely in the past.
func TestDecide_QueuedIsDerivedNeverDeclared(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		offset     time.Duration
		fromClient bool
		want       bool
	}{
		{"live tap, server clock", 0, false, false},
		{"server clock even if backdated somehow", -time.Hour, false, false},
		{"client declared the past", -time.Hour, true, true},
		{"client declared the present", 0, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := evidencedInput()
			in.OccurredAt = in.Now.Add(tc.offset)
			in.OccurredAtFromClient = tc.fromClient

			got := Decide(in)
			q, _ := got.PolicyContext[policy.CtxTapQueued].(bool)
			if q != tc.want {
				t.Fatalf("tap:queued = %v, want %v", q, tc.want)
			}
		})
	}
}

// TestDecide_ZeroOccurredAtMeansNowNot1970. Reading the zero value literally would
// give every caller that never set the field a fifty-six-year skew and a denied
// tap — the mirror image of the missing-key problem, and worse, because it turns
// a live tap into a reject.
func TestDecide_ZeroOccurredAtMeansNowNot1970(t *testing.T) {
	t.Parallel()
	in := evidencedInput()
	in.OccurredAt = time.Time{}

	got := Decide(in)
	if got.Verdict != VerdictOK {
		t.Fatalf("verdict = %q, want ok: a zero OccurredAt must read as 'now' (sid %q)", got.Verdict, got.MatchedSid)
	}
}

// TestDecide_PolicyContextIsTheEvaluatedSnapshot. migration 0008's policy_context
// exists so a past verdict can be replayed, which only works if what is stored is
// what was EVALUATED — not a reconstruction the write path assembles from Input,
// which is two computations free to drift apart.
//
// §4.7: it carries a DISTANCE and never a coordinate.
func TestDecide_PolicyContextIsTheEvaluatedSnapshot(t *testing.T) {
	t.Parallel()
	in := evidencedInput()
	here, venue := geo.Point{Lat: 35.9, Lng: 14.5}, geo.Point{Lat: 35.918, Lng: 14.489}
	in.GPS, in.LocationGPS = &here, &venue

	got := Decide(in)

	for _, key := range []policy.ContextKey{
		policy.CtxTapChannel, policy.CtxTapSunValid, policy.CtxTapIPMatch, policy.CtxTapGPSMatch,
		policy.CtxTapGPSConflict, policy.CtxTapCtrGap, policy.CtxTapOccurredAtSkewSeconds,
		policy.CtxTapQueued, policy.CtxTagStatus, policy.CtxEmployeeStatus, policy.CtxLocationID,
		policy.CtxTapGPSDistanceM,
	} {
		if _, ok := got.PolicyContext[key]; !ok {
			t.Fatalf("the snapshot is missing %q", key)
		}
	}
	if d, _ := got.PolicyContext[policy.CtxTapGPSDistanceM].(float64); d <= 0 {
		t.Fatalf("tap:gpsDistanceM = %v, want a real distance in metres", d)
	}
	// The raw position must not appear under any key.
	for key, v := range got.PolicyContext {
		if f, ok := v.(float64); ok && (f == 35.9 || f == 14.5 || f == 35.918 || f == 14.489) {
			t.Fatalf("the snapshot carries a raw coordinate under %q (§4.7)", key)
		}
	}
	// A tap with NO fix carries no distance rather than a misleading zero.
	noFix := evidencedInput()
	if _, ok := Decide(noFix).PolicyContext[policy.CtxTapGPSDistanceM]; ok {
		t.Fatal("a tap with no GPS reported a distance")
	}
}

// TestDecide_TapFreshnessIsFedAndCanFire is guardrail #6, which an audit found
// INERT: nothing in the product set tap:pageAgeSeconds, and a missing key never
// matches (M3-04's invariant), so sys:tap-freshness could not fire at any window
// a tenant chose. It was not weakened or mis-tuned — it was unreachable.
//
// The window is tightened here to 60 s because that is what makes the assertion
// mean something under policy.DefaultParams(): its 900 s fallback is the same
// number as the signed context's TTL, so at THAT value the signed context is
// refused at parse time first and the guardrail's band is exactly empty.
//
// 🔴 THAT IS NO LONGER THE SHIPPED STATE, and this comment said it was. M5-10
// landed TAPPA_FRESHNESS_SECONDS, default 180 s, carried into Params by
// checkin.New — so a deployment runs at 180 s and the 180..900 s band IS a
// recorded reject. Two corrections to the old sentence: the shipped value is 180
// and not 900, and the setting is per-DEPLOYMENT (an env var, like
// TAPPA_GPS_RADIUS_M and TAPPA_DEBOUNCE_SECONDS) and not per-tenant — no bounded
// param is stored per tenant today; that is M6-09's work. Feeding the key here is
// what lets any of those windows produce a RECORDED reject instead of an
// unrecorded error page.
func TestDecide_TapFreshnessIsFedAndCanFire(t *testing.T) {
	t.Parallel()

	tight := policy.DefaultParams()
	tight.FreshnessWindow = 60 * time.Second
	if err := tight.Validate(); err != nil {
		t.Fatalf("the tightened window is outside ADR 0004 §11: %v", err)
	}
	setWith := func(p policy.Params) policy.Set {
		s := testSet()
		s.Guardrails = policy.Guardrails(p)
		return s
	}

	// A page minted five minutes ago, under a one-minute window.
	stale := evidencedInput()
	stale.PageIssuedAt = stale.Now.Add(-5 * time.Minute)
	stale.PolicySet = setWith(tight)

	got := Decide(stale)
	if got.Verdict != VerdictReject || got.MatchedSid != "sys:tap-freshness" {
		t.Fatalf("verdict/sid = %q/%q, want reject/sys:tap-freshness — the guardrail is still inert",
			got.Verdict, got.MatchedSid)
	}
	if got.Redirect != RedirectNone {
		t.Fatal("a stale page must be a RECORDED reject (§4.6), never a redirect")
	}
	if age, ok := got.PolicyContext[policy.CtxTapPageAgeSeconds].(float64); !ok || age < 299 {
		t.Fatalf("tap:pageAgeSeconds = %v, want ~300", got.PolicyContext[policy.CtxTapPageAgeSeconds])
	}

	// A FRESH page under the same tight window is untouched.
	fresh := evidencedInput()
	fresh.PageIssuedAt = fresh.Now.Add(-5 * time.Second)
	fresh.PolicySet = setWith(tight)
	if got := Decide(fresh); got.Verdict != VerdictOK {
		t.Fatalf("a five-second-old page produced %q/%q", got.Verdict, got.MatchedSid)
	}

	// THE MUTATION, stated as a test rather than as a claim: with the key absent —
	// exactly the state the audit measured — the same five-minute-old page sails
	// through, because a missing key never matches.
	unfed := evidencedInput()
	unfed.PolicySet = setWith(tight) // PageIssuedAt deliberately left zero
	if got := Decide(unfed); got.Verdict != VerdictOK {
		t.Fatalf("without PageIssuedAt the guardrail produced %q; this test would no longer be measuring "+
			"the gap it claims to", got.Verdict)
	}

	// A QR arrival is NOT subject to it: QR is valid indefinitely (§5), and the
	// guardrail scopes itself to nfc.
	qr := evidencedInput()
	qr.Channel = ChannelQR
	qr.SUN = SUNResult{}
	qr.PageIssuedAt = qr.Now.Add(-5 * time.Minute)
	qr.PolicySet = setWith(tight)
	if got := Decide(qr); got.MatchedSid == "sys:tap-freshness" {
		t.Fatal("a QR tap was denied for page age; the guardrail is NFC-only (§5)")
	}
}

// --- M11 API-1b: two peers of one holding (ADR 0027 §5, §7) -------------------

// TestDecide_TenantMismatchRespectsSameHolding is CLAUDE.md §5's TestDecide_ rule for
// guardrail #1's peer arm (ADR 0027 §7): the session's tenant and the tag's differ,
// and the two are PEERS in one holding -- the tap is decided like any other (here,
// with the site's own network matched: ok, recorded) instead of redirected. The same
// pair NOT peers is today's redirect; and SameHolding beside the presence placeholder
// (no real session tenant) changes nothing -- there is no pair to be peers.
func TestDecide_TenantMismatchRespectsSameHolding(t *testing.T) {
	t.Parallel()
	site, employer := uuid.New(), uuid.New()

	peers := evidencedInput()
	peers.TagTenantID, peers.SessionTenantID, peers.SameHolding = &site, &employer, true
	got := Decide(peers)
	if got.Redirect != RedirectNone || got.Verdict != VerdictOK || got.MatchedSid == "sys:tenant-mismatch" {
		t.Fatalf("peers: %q/%q/%q, want an ok record and no redirect", got.Verdict, got.Redirect, got.MatchedSid)
	}

	strangers := evidencedInput()
	strangers.TagTenantID, strangers.SessionTenantID = &site, &employer
	if got := Decide(strangers); got.Redirect != RedirectActivation || got.MatchedSid != "sys:tenant-mismatch" {
		t.Fatalf("not peers: %q/%q, want the mismatch redirect", got.Redirect, got.MatchedSid)
	}

	placeholder := evidencedInput()
	placeholder.TagTenantID, placeholder.SameHolding = &site, true
	if got := Decide(placeholder); got.Verdict != VerdictOK || got.Redirect != RedirectNone {
		t.Fatalf("placeholder session: %q/%q, want the pre-N5 ok", got.Verdict, got.Redirect)
	}
}

// crossQRInput is a QR scan by an employer's session on a site's plaque, the two peers,
// with the site's ranges registered and the source address on them or not.
func crossQRInput(onSiteNet bool) Input {
	in := baseInput()
	site, employer := uuid.New(), uuid.New()
	in.TagTenantID, in.SessionTenantID, in.SameHolding = &site, &employer, true
	in.Channel, in.SUN = ChannelQR, SUNResult{}
	in.LocationIPs = []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
	in.SourceIP = netip.MustParseAddr("198.51.100.9")
	if onSiteNet {
		in.SourceIP = netip.MustParseAddr("203.0.113.5")
	}
	return in
}

// TestDecide_CrossTenantQRWithoutSiteIPIsFlagged is K-1g (ADR 0027 §7): a QR scan on a
// peer's plaque without the SITE's network is flagged by sys:cross-tenant-qr-site-ip --
// recorded, queued, never ok -- even with GPS matching the site's venue, and even when the
// employer's policy set has NO baseline (base:qr-requires-ip "switched off") and allows
// every tap at the venue. A list too wide to tell a venue apart is no match either.
func TestDecide_CrossTenantQRWithoutSiteIPIsFlagged(t *testing.T) {
	t.Parallel()
	here := geo.Point{Lat: 35.8989, Lng: 14.5146}
	allowAll := func(in Input) policy.Set {
		return policy.Set{Guardrails: policy.DefaultGuardrails(), Tenant: []policy.Policy{{VersionID: uuid.New(), Layer: policy.LayerTenant,
			Document: policy.Document{Version: policy.BaselineVersion, Name: "km allows", Statements: []policy.Statement{{
				Sid: "km-allow", Effect: policy.EffectAllow, Action: []policy.Action{policy.ActionTapRecord},
				Resource: []string{"location/" + in.Tag.Location.String()}, Reason: "the employer allows"}}}}}}
	}
	for _, c := range []struct {
		name   string
		mutate func(in *Input)
	}{
		{"no site IP", func(*Input) {}},
		{"no site IP, GPS at the venue", func(in *Input) { in.GPS, in.LocationGPS = &here, &here }},
		{"no site IP, the employer allows everything there and has no baseline", func(in *Input) { in.PolicySet = allowAll(*in) }},
		{"a site list too wide to be evidence", func(in *Input) {
			in.LocationIPs = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/1"), netip.MustParsePrefix("128.0.0.0/1")}
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := crossQRInput(false)
			c.mutate(&in)
			got := Decide(in)
			if got.Verdict != VerdictFlag || got.MatchedSid != "sys:cross-tenant-qr-site-ip" || got.Redirect != RedirectNone {
				t.Fatalf("%q/%q/%q, want flag by sys:cross-tenant-qr-site-ip", got.Verdict, got.MatchedSid, got.Redirect)
			}
			if got.Type == nil {
				t.Fatal("a flagged record carries a direction (it may be approved)")
			}
		})
	}
}

// TestDecide_CrossTenantQRWithSiteIPIsOK: the same scan from the SITE's own network is
// decided by the employer's policy as any QR with its IP -- ok with the production baseline
// (base:ip-or-gps-ok), trust 70 with GPS too.
func TestDecide_CrossTenantQRWithSiteIPIsOK(t *testing.T) {
	t.Parallel()
	got := Decide(crossQRInput(true))
	if got.Verdict != VerdictOK || got.MatchedSid == "sys:cross-tenant-qr-site-ip" || !got.IPMatch {
		t.Fatalf("%q/%q ip=%v, want ok from the employer's baseline with the site's IP matched", got.Verdict, got.MatchedSid, got.IPMatch)
	}
}

// TestDecide_PersonDebounceSpansBothTenantsPlaques is ADR 0027 §7's debounce rule (ADR 0006
// note; plan §5 API-1b (d)): the debounce is the PERSON's, held at the employer, so a tap on
// a peer's plaque 20 s after the person's last tap on the EMPLOYER's own plaque (that row is
// LastForPerson -- gather reads it at the employer whatever plaque it was) is ignored, and
// so is the reverse. The end-to-end proof is internal/domain/checkin's
// TestHoldingTap_PersonDebounceSpansBothTenantsPlaques.
func TestDecide_PersonDebounceSpansBothTenantsPlaques(t *testing.T) {
	t.Parallel()
	site, employer := uuid.New(), uuid.New()
	twentySecondsAgo := 20.0
	for _, c := range []struct {
		name string
		tag  *uuid.UUID
		peer bool
	}{
		{"a peer's plaque after the employer's own", &site, true},
		{"the employer's own plaque after a peer's", &employer, false},
	} {
		in := evidencedInput()
		in.TagTenantID, in.SessionTenantID, in.SameHolding = c.tag, &employer, c.peer
		in.LastForPerson = &Transaction{ID: uuid.New(), OccurredAt: in.Now.Add(-20 * time.Second), Direction: TypeIn}
		in.SecondsSinceLastRecordedTap = &twentySecondsAgo
		got := Decide(in)
		if got.Verdict != VerdictIgnored || got.MatchedSid != "sys:person-debounce" {
			t.Errorf("%s: %q/%q, want ignored by sys:person-debounce", c.name, got.Verdict, got.MatchedSid)
		}
	}
}
