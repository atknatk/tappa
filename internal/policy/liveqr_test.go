package policy

import (
	"testing"

	"github.com/google/uuid"
)

// This file proves M16-21's policy half (ADR 0045 §2.5): the four live QR guardrails
// fire on their own condition and on no other channel, sys:tap-freshness reaches the
// live channel and still not the static one, and base:live-qr-requires-ip is the user's
// 2026-11-19 decision -- a live QR tap with GPS alone is reviewed, with IP it is let
// through, and switching the document off gives the GPS-only allow back.

// cleanLiveQR is a live QR tap that matches NO guardrail: a verified token, an active
// screen, a fresh page, a token seen inside its window, same-tenant session and screen.
func cleanLiveQR(tenant uuid.UUID) Context {
	c := cleanTap(tenant)
	delete(c.Keys, CtxTagStatus)
	c.Keys[CtxTapChannel] = channelQRLive
	c.Keys[CtxTapSunValid] = false
	c.Keys[CtxTapTokenVerified] = true
	c.Keys[CtxTapTokenOverdueSeconds] = float64(-5)
	c.Keys[CtxScreenStatus] = screenStatusActive
	return c
}

func TestGuardrails_LiveQRRulesFireOnTheirOwnCondition(t *testing.T) {
	tenant, peer := uuid.New(), uuid.New()
	cases := []struct {
		name    string
		build   func() Context
		wantSid string
		wantEff Effect
	}{
		{"a clean live QR tap matches no guardrail", func() Context { return cleanLiveQR(tenant) },
			"default", EffectReview},
		{"a token that did not verify -> reject", func() Context {
			c := cleanLiveQR(tenant)
			c.Keys[CtxTapTokenVerified] = false
			return c
		}, "sys:live-qr-unverified", EffectDeny},
		{"no token fact at all on a live QR tap -> reject (fail-closed)", func() Context {
			c := cleanLiveQR(tenant)
			delete(c.Keys, CtxTapTokenVerified)
			return c
		}, "sys:live-qr-unverified", EffectDeny},
		{"a removed screen -> reject", func() Context {
			c := cleanLiveQR(tenant)
			c.Keys[CtxScreenStatus] = "revoked"
			return c
		}, "sys:screen-not-active", EffectDeny},
		{"a screen at a switched-off venue -> reject", func() Context {
			c := cleanLiveQR(tenant)
			c.Keys[CtxScreenStatus] = "location_inactive"
			return c
		}, "sys:screen-not-active", EffectDeny},
		{"a screen whose state is unknown -> reject (allow-list)", func() Context {
			c := cleanLiveQR(tenant)
			delete(c.Keys, CtxScreenStatus)
			return c
		}, "sys:screen-not-active", EffectDeny},
		{"a live QR page past the freshness window -> reject", func() Context {
			c := cleanLiveQR(tenant)
			c.Keys[CtxTapPageAgeSeconds] = float64(DefaultParams().FreshnessWindow.Seconds() + 1)
			return c
		}, "sys:tap-freshness", EffectDeny},
		{"a peer's live QR screen -> reject", func() Context {
			c := cleanLiveQR(tenant)
			c.TagTenantID, c.SameHolding = &peer, true
			return c
		}, "sys:cross-tenant-live-qr", EffectDeny},
		{"a token first seen past window + grace -> review", func() Context {
			c := cleanLiveQR(tenant)
			c.Keys[CtxTapTokenOverdueSeconds] = float64(LiveQRNetworkGraceSeconds + 1)
			return c
		}, "sys:live-qr-expired", EffectReview},
		{"a token first seen at exactly window + grace is fresh", func() Context {
			c := cleanLiveQR(tenant)
			c.Keys[CtxTapTokenOverdueSeconds] = float64(LiveQRNetworkGraceSeconds)
			return c
		}, "default", EffectReview},
		{"the debounce beats an expired token (QR's one brake)", func() Context {
			c := cleanLiveQR(tenant)
			c.Keys[CtxTapTokenOverdueSeconds] = float64(LiveQRNetworkGraceSeconds + 1)
			gap := 5.0
			c.SecondsSincePersonLastTap = &gap
			return c
		}, "sys:person-debounce", EffectIgnore},
		{"the holding refusal beats an expired token", func() Context {
			c := cleanLiveQR(tenant)
			c.TagTenantID, c.SameHolding = &peer, true
			c.Keys[CtxTapTokenOverdueSeconds] = float64(LiveQRNetworkGraceSeconds + 1)
			return c
		}, "sys:cross-tenant-live-qr", EffectDeny},
		{"a forged token beats the debounce (reject, not ignored)", func() Context {
			c := cleanLiveQR(tenant)
			c.Keys[CtxTapTokenVerified] = false
			gap := 5.0
			c.SecondsSincePersonLastTap = &gap
			return c
		}, "sys:live-qr-unverified", EffectDeny},
		{"a deactivated person's forged token keeps the alert", func() Context {
			c := cleanLiveQR(tenant)
			c.Keys[CtxTapTokenVerified] = false
			c.Keys[CtxEmployeeStatus] = employeeDeactivated
			return c
		}, "sys:employee-deactivated", EffectDeny},
		{"outside a holding the screen of another business redirects (no record)", func() Context {
			c := cleanLiveQR(tenant)
			c.TagTenantID = &peer
			return c
		}, "sys:tenant-mismatch", EffectRedirect},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Evaluate(defaultSet(), tc.build())
			if d.MatchedSid != tc.wantSid || d.Effect != tc.wantEff {
				t.Fatalf("want %s / %s, got %s / %s", tc.wantEff, tc.wantSid, d.Effect, d.MatchedSid)
			}
		})
	}
}

// TestGuardrails_LiveQRRulesLeaveTheOtherChannelsAlone: the four rules key on qr_live, and
// the keys they read are absent from every other channel -- so an NFC, a static QR and a
// manual record carrying the WORST values of those keys reach none of them, and a stale
// static QR page is still not denied by freshness (TestCheckinDB_StaleQRPageIsNotDeniedByFreshness).
func TestGuardrails_LiveQRRulesLeaveTheOtherChannelsAlone(t *testing.T) {
	tenant, peer := uuid.New(), uuid.New()
	for _, ch := range []string{channelNFC, channelQR, "manual"} {
		c := cleanTap(tenant)
		c.Keys[CtxTapChannel] = ch
		c.Keys[CtxTapTokenVerified] = false
		c.Keys[CtxScreenStatus] = "revoked"
		c.Keys[CtxTapTokenOverdueSeconds] = float64(3600)
		if ch == channelQR {
			c.Keys[CtxTapSunValid] = false
			c.Keys[CtxTapPageAgeSeconds] = float64(DefaultParams().FreshnessWindow.Seconds() + 60)
		}
		d := Evaluate(defaultSet(), c)
		switch d.MatchedSid {
		case "sys:live-qr-unverified", "sys:screen-not-active", "sys:cross-tenant-live-qr", "sys:live-qr-expired", "sys:tap-freshness":
			t.Fatalf("%s: a live QR rule decided a %s tap: %s", ch, ch, d.MatchedSid)
		}
		// A peer's plaque on NFC is not the live QR holding refusal either.
		c.TagTenantID, c.SameHolding = &peer, true
		if d := Evaluate(defaultSet(), c); d.MatchedSid == "sys:cross-tenant-live-qr" {
			t.Fatalf("%s: the live QR holding refusal decided a %s tap", ch, ch)
		}
	}
}

// TestBaseline_LiveQRRequiresIP is the user's decision of 2026-11-19 under the FULL
// default baseline: live QR + IP -> ok, live QR + GPS alone -> review via
// base:live-qr-requires-ip, live QR + nothing -> review; and a static QR is decided
// exactly as before.
func TestBaseline_LiveQRRequiresIP(t *testing.T) {
	set := Set{Guardrails: DefaultGuardrails(), Baseline: fullBaseline()}
	live := func(ip, gps bool) Context {
		c := cleanLiveQR(uuid.New())
		c.Keys[CtxTapIPMatch], c.Keys[CtxTapGPSMatch] = ip, gps
		return c
	}
	cases := []struct {
		name    string
		ctx     Context
		wantEff Effect
		wantSid string
	}{
		{"live QR + IP -> allow", live(true, false), EffectAllow, SidIPOrGPSOK},
		{"live QR + IP + GPS -> allow", live(true, true), EffectAllow, SidIPOrGPSOK},
		{"live QR + GPS only -> review (the 2026-11-19 decision)", live(false, true), EffectReview, SidLiveQRRequiresIP},
		{"live QR + nothing -> review", live(false, false), EffectReview, SidNoEvidenceReview},
		{"static QR + GPS only -> review, unchanged", benignTap(map[ContextKey]any{
			CtxTapChannel: channelQR, CtxTapSunValid: false, CtxTapIPMatch: false, CtxTapGPSMatch: true}),
			EffectReview, SidQRRequiresIP},
		{"static QR + IP -> allow, unchanged", benignTap(map[ContextKey]any{
			CtxTapChannel: channelQR, CtxTapSunValid: false, CtxTapIPMatch: true}),
			EffectAllow, SidIPOrGPSOK},
		{"NFC + GPS only -> allow, unchanged", benignTap(map[ContextKey]any{
			CtxTapIPMatch: false, CtxTapGPSMatch: true}),
			EffectAllow, SidGPSOnlyAllow},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := Evaluate(set, tc.ctx)
			if d.Effect != tc.wantEff {
				t.Fatalf("effect %q via %q, want %q", d.Effect, d.MatchedSid, tc.wantEff)
			}
			// no-evidence-review and the live rule both match "nothing"; the document
			// order names the first, and either is the same review.
			if tc.wantSid != SidNoEvidenceReview && d.MatchedSid != tc.wantSid {
				t.Fatalf("decided by %q, want %q", d.MatchedSid, tc.wantSid)
			}
		})
	}
}

// TestBaseline_TheTenantCanSwitchTheLiveQRRuleOff: the decision says the rule is the
// tenant's to change, like base:qr-requires-ip. With the document left out (a stored row
// switched off is not loaded -- checkin's tenantPolicies), a live QR GPS-only tap is
// let through by base:gps-only-allow; and a tenant review at one venue tightens it there.
func TestBaseline_TheTenantCanSwitchTheLiveQRRuleOff(t *testing.T) {
	var without []Policy
	for _, p := range fullBaseline() {
		if p.Document.Statements[0].Sid != SidLiveQRRequiresIP {
			without = append(without, p)
		}
	}
	c := cleanLiveQR(uuid.New())
	c.Keys[CtxTapIPMatch], c.Keys[CtxTapGPSMatch] = false, true
	d := Evaluate(Set{Guardrails: DefaultGuardrails(), Baseline: without}, c)
	if d.Effect != EffectAllow || d.MatchedSid != SidGPSOnlyAllow {
		t.Fatalf("switched off: %q via %q, want allow via %s", d.Effect, d.MatchedSid, SidGPSOnlyAllow)
	}
	// A venue-scoped tenant allow beats the location/* review (ADR 0004 §7) -- the
	// Rusty Bar lever works on this rule as on every baseline statement.
	override := []Policy{pol(LayerTenant, Statement{Sid: "own:live-qr-gps-ok", Effect: EffectAllow,
		Action: []Action{ActionTapRecord}, Resource: []string{"location/rusty-bar"},
		Condition: Condition{OpStringEquals: {CtxTapChannel: channelQRLive}, OpBool: {CtxTapGPSMatch: true}},
		Reason:    "our venue has no fixed line"})}
	d = Evaluate(Set{Guardrails: DefaultGuardrails(), Baseline: fullBaseline(), Tenant: override}, c)
	if d.Effect != EffectAllow || d.MatchedSid != "own:live-qr-gps-ok" {
		t.Fatalf("venue override: %q via %q, want allow via the tenant rule", d.Effect, d.MatchedSid)
	}
	// No tenant document reaches a guardrail: a forged token stays a reject under it.
	c.Keys[CtxTapTokenVerified] = false
	if d = Evaluate(Set{Guardrails: DefaultGuardrails(), Baseline: fullBaseline(), Tenant: override}, c); d.MatchedSid != "sys:live-qr-unverified" {
		t.Fatalf("a tenant allow loosened a guardrail: %q", d.MatchedSid)
	}
}
