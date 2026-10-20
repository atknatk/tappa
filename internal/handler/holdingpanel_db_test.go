package handler

// holdingpanel_db_test.go -- M11 API-1c on the real panel (ADR 0027 §9): what a holding member's
// manager sees of the OTHER member's staff at its venues (the site's report and lost-plaque alert),
// what it sees of its OWN staff at the other member's venue (the docket, K-39), and the manual
// form's partner venues. The harness's business is the one signed in; its peer is written by the
// owner.

import (
	"context"
	"html"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
)

// TestHoldingPanel_PartnerViewsSayTheOutcomeNeverTheReason renders four screens of the signed-in
// business (call it KF) in a holding with KM:
//
//   - the reports week holding KM staff's tap at KF's venue lists it under "Partner staff at your
//     venues" -- the person, the outcome, "Hours: at their employer" -- and NOTHING of KM's row's
//     note, policy context or evidence (K-1h, K-1d: the projection has no such column; the canary
//     text written into KM's row appears nowhere on the page);
//   - the anomalies week carries the lost-plaque alert for KF's lost plaque a KM employee touched;
//   - KF's own employee's tap at KM's venue is a docket naming KM's venue whose evidence reads
//     "not shown at a partner venue" -- not the signs (K-39);
//   - the manual form offers KM's venue as a partner venue;
//   - The Spike's record in focus at KM's venue shows the outcome and the partner venue, never the
//     rule, the score, the signs or the evidence sentence (M12-28, K-39).
//
// And the CONTROLS: a business in no holding gets none of the four additions, and its anomalies
// screen names no partner business at all (no bucket, no sentence).
func TestHoldingPanel_PartnerViewsSayTheOutcomeNeverTheReason(t *testing.T) {
	p := newPanelHarness(t)
	p.signIn(t)
	owner := holdingOwner(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := owner.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	km := uuid.New()
	exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'api1c KM', $2, 'production', 'multi')`, km, "VAT-API1C-H-"+km.String())
	holdingOf(t, owner, p.tenantID, km)

	suffix := uuid.NewString()[:6]
	kfVenueName, kmVenueName := "KF Site "+suffix, "KM Plant "+suffix
	kfVenue := p.seedWall(t, kfVenueName)
	kfPlaque := p.seedPlaque(t, "active", kfVenue)
	kfLost := p.seedPlaque(t, "active", kfVenue)
	exec(`UPDATE tags SET status = 'lost' WHERE uid = $1`, kfLost)
	var kmVenue uuid.UUID
	if err := owner.QueryRow(ctx, `INSERT INTO locations (tenant_id, name, static_ips) VALUES ($1, $2, '{198.51.100.0/24}') RETURNING id`, km, kmVenueName).Scan(&kmVenue); err != nil {
		t.Fatal(err)
	}
	kmPlaque := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:14])
	exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 1000, 'active')`, kmPlaque, km, kmVenue, make([]byte, 44))
	kmStaff, kfStaff := uuid.New(), uuid.New()
	kmName, kfName := "Partner Person "+suffix, "Own Person "+suffix
	exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, $3, $4, 'active', now())`, kmStaff, km, "ext-"+suffix, kmName)
	exec(`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at) VALUES ($1, $2, $3, $4, 'active', now())`, kfStaff, p.tenantID, kfVenue, kfName)

	zone, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	day := time.Date(2026, 7, 1, 9, 0, 0, 0, zone)
	const canary = "CANARY-REASON-7731"
	// KM's employee at KF's venue -- a reason and evidence in KM's own row that KF must never see.
	exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, ip_match, gps_match, trust, note,
	                                matched_sid, policy_context, site_tenant_id, site_location_id, site_tag_uid)
	      VALUES ($1, $2, $3, 'ok', 'nfc', 'in', true, 1000, false, true, 50, $4, 'base:gps-only-allow', $5::jsonb, $6, $7, $8)`,
		km, kmStaff, day, canary+" note", `{"tap:ctrGap": 0, "tap:reason": "`+canary+`"}`, p.tenantID, kfVenue, kfPlaque)
	// KM's employee on KF's LOST plaque, now.
	exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, sun_valid, ctr, matched_sid, note, site_tenant_id, site_location_id, site_tag_uid)
	      VALUES ($1, $2, now(), 'reject', 'nfc', false, 1500, 'sys:tag-not-active', $3, $4, $5, $6)`,
		km, kmStaff, canary+" refusal", p.tenantID, kfVenue, kfLost)
	// KF's own employee at KM's venue, with evidence signs KF's panel must not print.
	exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, ip_match, gps_match, trust,
	                                note, policy_layer, matched_sid, site_tenant_id, site_location_id, site_tag_uid)
	      VALUES ($1, $2, $3, 'ok', 'nfc', 'in', true, 1000, false, true, 50, $4, 'guardrail', 'base:gps-only-allow', $5, $6, $7)`,
		p.tenantID, kfStaff, day.Add(time.Hour), "verified via GPS only — no network proof of place", km, kmVenue, kmPlaque)

	page := func(path string) string {
		t.Helper()
		res, body := p.get(t, path)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d", path, res.StatusCode)
		}
		return body
	}
	reports := page(weekHref(day))
	for _, want := range []string{"Partner staff at your venues", kmName, kfVenueName, "Counted by their employer", "Hours: at their employer"} {
		if !strings.Contains(reports, want) {
			t.Errorf("the site's report does not say %q", want)
		}
	}
	anomalies := page(anomaliesHref)
	if !strings.Contains(anomalies, "A lost plaque was tapped") || !strings.Contains(anomalies, kfVenueName) {
		t.Error("the site's anomalies screen carries no lost-plaque alert for its plaque a partner's staff touched")
	}
	for name, body := range map[string]string{"reports": reports, "anomalies": anomalies} {
		for _, leak := range []string{canary, "gps-only", "base:", "sys:"} {
			if strings.Contains(body, leak) {
				t.Errorf("the site's %s screen carries %q -- the employer's reason (K-1h)", name, leak)
			}
		}
	}
	transactions := page(transactionsHref + "?date=2026-07-01")
	if !strings.Contains(transactions, kmVenueName) || !strings.Contains(transactions, "not shown at a partner venue") {
		t.Error("the employer's docket does not name the partner venue and say its evidence is not shown")
	}
	if strings.Contains(transactions, "IP no · GPS yes") {
		t.Error("the employer's docket prints the evidence signs of a row at a partner venue (K-39)")
	}
	// G-ORTA-1: the engine's sentence says the same in words; the docket carries the stand-in.
	if strings.Contains(transactions, "no network proof of place") || !strings.Contains(transactions, "Decided by your rules at a partner venue") {
		t.Error("the employer's docket carries the engine's evidence sentence for a row at a partner venue (K-39)")
	}
	// M12-28: KF's own employee's FLAGGED tap at KM's venue, in The Spike's queue and in focus. The
	// record's own row carries signs, a score, an evidence rule and its sentence; the Spike shows
	// the outcome, the partner venue, PartnerVenueNote and one merged tile -- no rule sid, no trust,
	// no IP/GPS word, no evidence sentence (K-39, ADR 0027 §13 item 5 (e)).
	var flagged uuid.UUID
	if err := owner.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, ip_match, gps_match, trust,
	                                note, policy_layer, matched_sid, queued, site_tenant_id, site_location_id, site_tag_uid)
	      VALUES ($1, $2, $3, 'flag', 'nfc', 'in', true, 1001, false, false, 20, $4, 'guardrail', 'base:no-evidence-review', true, $5, $6, $7) RETURNING id`,
		p.tenantID, kfStaff, day.Add(2*time.Hour), "neither IP nor GPS could place this tap", km, kmVenue, kmPlaque).Scan(&flagged); err != nil {
		t.Fatal(err)
	}
	spike := html.UnescapeString(page(reviewHref + "?focus=" + flagged.String()))
	if ids := hiddenIDsOf(spike); len(ids) != 1 || ids[0] != flagged.String() {
		t.Fatalf("The Spike's form decides %v, want the partner-venue record %s", ids, flagged)
	}
	for _, want := range []string{kmVenueName + " · partner venue", "Network · Location", "not shown at a partner venue",
		ledger.PartnerVenueNote, "at a partner venue", "FLAGGED"} {
		if !strings.Contains(spike, want) {
			t.Errorf("The Spike's partner-venue record does not say %q", want)
		}
	}
	for _, leak := range []string{"base:", "no-evidence", "rule ", "Trust", "trust ", "IP ", "GPS",
		"neither IP nor GPS", "venue's range", "placed the tap", "no network, no location"} {
		if strings.Contains(spike, leak) {
			t.Errorf("The Spike's partner-venue record carries %q (K-39)", leak)
		}
	}

	manual := page(manualEntryHref + "?id=" + kfStaff.String())
	if !strings.Contains(manual, `name="site"`) || !strings.Contains(manual, kmVenueName) {
		t.Error("the manual form does not offer the partner venue")
	}

	// CONTROLS: a business in no holding.
	q := newPanelHarness(t)
	q.signIn(t)
	for _, c := range []struct{ path, absent string }{
		{weekHref(day), "Partner staff"}, {anomaliesHref, "A lost plaque was tapped"},
		// D-a (fourth review): no partner bucket and no partner sentence -- MUTANT (measured): the
		// bucket appended at Partner >= 0 put a 0-record bucket on this empty week -> red.
		{anomaliesHref, "partner business"},
		{transactionsHref + "?date=2026-07-01", "partner venue"},
		{reviewHref, "partner venue"},
	} {
		res, body := q.get(t, c.path)
		if res.StatusCode != http.StatusOK || strings.Contains(body, c.absent) {
			t.Errorf("CONTROL %s: %d, carries %q=%v", c.path, res.StatusCode, c.absent, strings.Contains(body, c.absent))
		}
	}
}
