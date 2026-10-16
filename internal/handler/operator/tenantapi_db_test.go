package operator_test

// tenantapi_db_test.go -- M11 API-13a end to end against PostgreSQL: the operator signs in and, on a
// business's overview, opens its integration API (op_enable_tenant_api), closes it
// (op_disable_tenant_api) and opens it again, each write followed by the overview its 303 lands on
// (op_begin_read + op_read_tenant_detail, migration 00057) -- on newLegalE2E's committed rig
// (op10_db_test.go), the store internal/db's production accessors on that connection (liveStore).
//
// WHAT ONE RUN LEAVES BEHIND: two fixture tenants ('FAKE api1a …', newHoldingFixtureTenants), one
// holding ('FAKE api13a group …') with two ENDED memberships, the tenants' own audit rows
// (tenant.api_enabled, tenant.api_disabled, the holding rows -- append-only), the first tenant's API
// open under its run code, and the operator's rows ('read' rows naming the tenant, scope
// tenant_detail; 'tenant_api_enabled' / 'tenant_api_disabled'; the holding kinds). The class is the
// backlog's T81.

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestE2E_TenantAPIOpenCloseOpenLeavesEveryTrail is API-13a's screen acceptance on PostgreSQL:
//
//  1. the overview is a read: one 'read' row naming the business (scope tenant_detail); it says
//     Closed;
//  2. open under the run's code: 303 with the code; "Opened."; tenants.api_enabled_at set and api_code
//     the code; the business's own 'tenant.api_enabled' row naming the code; the operator's
//     'tenant_api_enabled' row naming the business;
//  3. close: "Closed."; api_enabled_at NULL, the code kept; 'tenant.api_disabled' and
//     'tenant_api_disabled';
//  4. open again: "Opened." -- a NEW opening instant (the disable cleared the first);
//  5. a second business in the same holding asks for the first's code: the write is ACCEPTED (303,
//     the operator's row records the ask) and its overview says "Not opened — nothing changed.": it
//     has no code and no 'tenant.api_enabled' row (the database's own holding rule, 00042);
//  6. every view of an overview left exactly one 'read' row naming its business.
func TestE2E_TenantAPIOpenCloseOpenLeavesEveryTrail(t *testing.T) {
	l := newLegalE2E(t)
	fx := newHoldingFixtureTenants(t, l, "Europe/Malta", "Europe/Malta")
	a, b := fx[0], fx[1]
	code := "KQ" + strings.ToUpper(a.id.String()[:6])
	sess := l.signIn(l.f)
	reads := func(tenant uuid.UUID) int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1
		                   AND target_scope = 'tenant_detail' AND target_tenant_id = $2`, l.f.id, tenant)
	}
	opRows := func(kind string, tenant uuid.UUID) int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = $1 AND actor_admin_id = $2
		                   AND target_tenant_id = $3`, kind, l.f.id, tenant)
	}
	tenantRows := func(tenant uuid.UUID, action string) int {
		return l.ownerInt(`SELECT count(*)::int FROM audit_log WHERE tenant_id = $1 AND action = $2`, tenant, action)
	}
	state := func(tenant uuid.UUID) (open bool, stored string, at string) {
		var c *string
		var ts *string
		if err := l.owner.QueryRow(l.ctx, `SELECT api_code, api_enabled_at::text FROM tenants WHERE id = $1`, tenant).Scan(&c, &ts); err != nil {
			t.Fatal(err)
		}
		if c != nil {
			stored = *c
		}
		if ts != nil {
			at = *ts
		}
		return ts != nil, stored, at
	}
	follow := func(what, path string, form url.Values, wantLoc string) string {
		t.Helper()
		w := l.post(path, form, sess)
		if loc := w.Result().Header.Get("Location"); w.Code != http.StatusSeeOther || loc != wantLoc {
			t.Fatalf("%s = %d to %q, want 303 to %q; log: %s", what, w.Code, loc, wantLoc, l.logs.String())
		}
		r := l.get(wantLoc, sess)
		if r.Code != http.StatusOK {
			t.Fatalf("the overview after %s = %d; log: %s", what, r.Code, l.logs.String())
		}
		return r.Body.String()
	}
	says := func(what, body string, want ...string) {
		t.Helper()
		if hs := apiHeadingsOn(body); !slices.Equal(hs, want) {
			t.Errorf("after %s the overview says %v, want %v", what, hs, want)
		}
	}
	overviewA := "/operator/tenants/" + a.id.String()

	// 1.
	if w := l.get(overviewA, sess); w.Code != http.StatusOK || reads(a.id) != 1 ||
		!strings.Contains(w.Body.String(), `<span class="tally tally--deactivated">Closed</span>`) {
		t.Fatalf("the first view = %d with %d 'read' row(s); want 200, one, and Closed; log: %s", w.Code, reads(a.id), l.logs.String())
	}
	// 2.
	body := follow("opening", overviewA+"/integration", url.Values{"api_code": {code}}, overviewA+"?done=api-opened&api_code="+code)
	says("opening", body, headingAPIOpened)
	open, stored, first := state(a.id)
	if !open || stored != code || tenantRows(a.id, "tenant.api_enabled") != 1 || opRows("tenant_api_enabled", a.id) != 1 {
		t.Errorf("after opening: open %v, code %q, %d tenant row(s), %d operator row(s); want true, %q, 1, 1", open, stored,
			tenantRows(a.id, "tenant.api_enabled"), opRows("tenant_api_enabled", a.id), code)
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM audit_log WHERE tenant_id = $1 AND action = 'tenant.api_enabled'
	                    AND detail ->> 'api_code' = $2 AND detail ->> 'actor_kind' = 'operator'`, a.id, code); n != 1 {
		t.Errorf("%d 'tenant.api_enabled' row(s) naming the code and the operator, want 1", n)
	}
	// 3.
	body = follow("closing", overviewA+"/integration/close", url.Values{}, overviewA+"?done=api-closed")
	says("closing", body, headingAPIClosed)
	if open, stored, _ := state(a.id); open || stored != code || tenantRows(a.id, "tenant.api_disabled") != 1 ||
		opRows("tenant_api_disabled", a.id) != 1 {
		t.Errorf("after closing: open %v, code %q, %d tenant row(s), %d operator row(s); want false, the code kept, 1, 1", open, stored,
			tenantRows(a.id, "tenant.api_disabled"), opRows("tenant_api_disabled", a.id))
	}
	// 4.
	body = follow("opening again", overviewA+"/integration", url.Values{"api_code": {code}}, overviewA+"?done=api-opened&api_code="+code)
	says("opening again", body, headingAPIOpened)
	if open, _, again := state(a.id); !open || again == first || tenantRows(a.id, "tenant.api_enabled") != 2 {
		t.Errorf("after opening again: open %v at %s (first %s), %d enable row(s); want open at a NEW instant and 2", open, again, first,
			tenantRows(a.id, "tenant.api_enabled"))
	}
	// 5. One holding, then B asks for A's code.
	w := l.post("/operator/holdings", url.Values{"name": {"FAKE api13a group " + a.id.String()[:8]}}, sess)
	loc := w.Result().Header.Get("Location")
	holding, err := uuid.Parse(strings.TrimPrefix(loc, "/operator/holdings?done=created&holding="))
	if w.Code != http.StatusSeeOther || err != nil {
		t.Fatalf("create the holding = %d to %q", w.Code, loc)
	}
	for _, f := range fx {
		if w := l.post(membersPathOf(holding.String()), url.Values{"tenant_id": {f.id.String()}}, sess); w.Code != http.StatusSeeOther {
			t.Fatalf("add %s = %d", f.name, w.Code)
		}
	}
	t.Cleanup(func() {
		for _, f := range fx {
			_ = l.post(removePathOf(holding.String(), f.id.String()), url.Values{}, sess)
		}
	})
	if live := l.ownerInt(`SELECT count(*)::int FROM holding_members WHERE holding_id = $1 AND left_at IS NULL`, holding); live != 2 {
		t.Fatalf("PREMISE: %d live member(s), want both", live)
	}
	overviewB := "/operator/tenants/" + b.id.String()
	body = follow("B asking for A's code", overviewB+"/integration", url.Values{"api_code": {code}}, overviewB+"?done=api-opened&api_code="+code)
	says("B asking for A's code", body, headingAPINotOpened)
	if open, stored, _ := state(b.id); open || stored != "" || tenantRows(b.id, "tenant.api_enabled") != 0 || opRows("tenant_api_enabled", b.id) != 1 {
		t.Errorf("B: open %v, code %q, %d tenant row(s), %d operator row(s); want closed, no code, none, and the ask's one", open, stored,
			tenantRows(b.id, "tenant.api_enabled"), opRows("tenant_api_enabled", b.id))
	}
	// 6.
	if ra, rb := reads(a.id), reads(b.id); ra != 4 || rb != 1 {
		t.Errorf("%d and %d 'read' row(s) naming A and B, want 4 and 1 (one per view)", ra, rb)
	}
}
