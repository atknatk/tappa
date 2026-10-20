package handler

// group_db_test.go -- GRP-1 on the real panel: the real router, the real *ledger.Reader, the real
// holding_group (migration 00062). The harness's business (call it KF) is signed in; its peers are
// written by the owner connection.

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestGroupDB_TheTabAndThePageFollowTheLiveMembership walks one business through its membership:
//
//   - in no group: no Group tab on Transactions, and /admin/group says "not in a group" -- and
//     nothing else of the panel changed (the same bytes on Reports before and after a peer that is
//     not this business's joins a holding of its own);
//   - a live member of H with KM (code KM) -- and L, who left H a minute ago: the tab on every
//     section, and the page names H, KF ("This business") and KM with its code, never L; lists KM's
//     employee at KF's venue with the outcome and never the reason (K-1h: the canary written into
//     KM's row appears nowhere); counts KF's own record at KM's venue; and carries no VAT number,
//     no e-mail of KM's;
//   - a manager of KF sees the same page;
//   - after KF leaves H: the tab is gone and the page says "not in a group" again.
func TestGroupDB_TheTabAndThePageFollowTheLiveMembership(t *testing.T) {
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
	page := func(path string) string {
		t.Helper()
		res, body := p.get(t, path)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("GET %s: %d", path, res.StatusCode)
		}
		return body
	}

	// In no group.
	if groupTabDrawn(page(transactionsHref)) {
		t.Error("a business in no group is drawn the Group tab")
	}
	if body := page(groupHref); !strings.Contains(body, "This business is not in a group") || strings.Contains(body, "Your group") {
		t.Error("a business in no group is not told so on the Group page")
	}
	reportsBefore := page(reportsHref)

	suffix := uuid.NewString()[:6]
	km, l := uuid.New(), uuid.New()
	kmVAT := "VAT-GRP1-KM-" + suffix
	exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure, api_code) VALUES ($1, $2, $3, 'production', 'multi', 'KM')`, km, "GRP1 KM "+suffix, kmVAT)
	exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, $2, $3, 'restaurant', 'multi')`, l, "GRP1 Left "+suffix, "VAT-GRP1-L-"+suffix)
	// A holding KF is NOT in, first: KF's panel must not change.
	other := holdingOf(t, owner, km)
	if got := page(reportsHref); got != reportsBefore {
		t.Error("a holding this business is not in changed its Reports page")
	}
	if groupTabDrawn(page(transactionsHref)) {
		t.Error("a holding this business is not in drew it the Group tab")
	}
	exec(`UPDATE holding_members SET left_at = clock_timestamp() WHERE holding_id = $1`, other)

	h := holdingOf(t, owner, p.tenantID, km)
	var hName string
	if err := owner.QueryRow(ctx, `SELECT name FROM holdings WHERE id = $1`, h).Scan(&hName); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO holding_members (holding_id, member_tenant_id, joined_at, left_at) VALUES ($1, $2, clock_timestamp() - interval '9 days', clock_timestamp() - interval '1 minute')`, h, l)

	kfVenue := p.seedWall(t, "GRP1 KF Site "+suffix)
	kfPlaque := p.seedPlaque(t, "active", kfVenue)
	var kmVenue uuid.UUID
	if err := owner.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, $2) RETURNING id`, km, "GRP1 KM Plant "+suffix).Scan(&kmVenue); err != nil {
		t.Fatal(err)
	}
	kmPlaque := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:14])
	exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 1000, 'active')`, kmPlaque, km, kmVenue, make([]byte, 44))
	kmStaff, kfStaff := uuid.New(), uuid.New()
	kmName := "GRP1 Partner " + suffix
	kmMail := "grp1-km-" + suffix + "@example.test"
	exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, email, status, invited_at) VALUES ($1, $2, $3, $4, $5, 'active', now())`, kmStaff, km, "ext-"+suffix, kmName, kmMail)
	exec(`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at) VALUES ($1, $2, $3, $4, 'active', now())`, kfStaff, p.tenantID, kfVenue, "GRP1 Own "+suffix)
	const canary = "CANARY-GRP1-REASON"
	at := time.Now().Add(-2 * time.Hour)
	exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, ip_match, gps_match, trust, note,
	                                matched_sid, policy_context, site_tenant_id, site_location_id, site_tag_uid)
	      VALUES ($1, $2, $3, 'ok', 'nfc', 'in', true, 1000, false, true, 50, $4, 'base:gps-only-allow', $5::jsonb, $6, $7, $8)`,
		km, kmStaff, at, canary+" note", `{"tap:ctrGap": 0, "tap:reason": "`+canary+`"}`, p.tenantID, kfVenue, kfPlaque)
	exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, ip_match, gps_match, trust,
	                                matched_sid, site_tenant_id, site_location_id, site_tag_uid)
	      VALUES ($1, $2, $3, 'ok', 'nfc', 'in', true, 1000, false, true, 50, 'base:gps-only-allow', $4, $5, $6)`,
		p.tenantID, kfStaff, at, km, kmVenue, kmPlaque)

	member := func(role string) {
		t.Helper()
		for _, path := range []string{transactionsHref, reportsHref, groupHref} {
			if !groupTabDrawn(page(path)) {
				t.Errorf("%s: GET %s draws no Group tab for a live member", role, path)
			}
		}
		body := page(groupHref)
		for _, want := range []string{hName, "This business", "GRP1 KM " + suffix, ">KM<", kmName, "Counted by their employer",
			"GRP1 KF Site " + suffix, "Your staff at partner venues", ">1<", "What the group shares"} {
			if !strings.Contains(body, want) {
				t.Errorf("%s: the Group page does not say %q", role, want)
			}
		}
		for _, never := range []string{"GRP1 Left " + suffix, canary, "gps-only", "base:", kmVAT, kmMail, "not in a group"} {
			if strings.Contains(body, never) {
				t.Errorf("%s: the Group page carries %q", role, never)
			}
		}
	}
	member("owner")
	if _, err := owner.Exec(ctx, `UPDATE admin_users SET role = 'manager' WHERE id = $1`, p.adminID); err != nil {
		t.Logf("the harness's admin cannot be made a manager here (%v); the manager's view is group_test.go's", err)
	} else {
		member("manager")
		exec(`UPDATE admin_users SET role = 'owner' WHERE id = $1`, p.adminID)
	}

	// KF leaves H.
	exec(`UPDATE holding_members SET left_at = clock_timestamp() WHERE holding_id = $1 AND member_tenant_id = $2 AND left_at IS NULL`, h, p.tenantID)
	if groupTabDrawn(page(transactionsHref)) {
		t.Error("a business that left its group is still drawn the Group tab")
	}
	if body := page(groupHref); !strings.Contains(body, "This business is not in a group") || strings.Contains(body, hName) {
		t.Error("a business that left its group still reads it")
	}
}
