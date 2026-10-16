package db

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// operatorapidetail_test.go -- migration 00057 (M11 API-13a): op_read_tenant_detail ends its row with
// the tenant's integration API state, read through the SHIPPED statement (readTenantDetailSQL) and
// internal/db's own phase two, readTenantDetail; its Down gives 00029's function back verbatim and its
// Up takes the state again; its precondition refuses a function another migration changed.

const op00057File = "00057_show_a_tenants_api_to_the_operator.sql"

// opReadOverview runs the shipped phase two (readTenantDetail) as tappa_operator inside a savepoint of
// tx; on success the consumption stays in tx.
func opReadOverview(t *testing.T, ctx context.Context, tx pgx.Tx, hash, ticket string, tenant uuid.UUID) (TenantOverview, error) {
	t.Helper()
	var o TenantOverview
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		o, e = readTenantDetail(ctx, sp, hash, readTicket{v: &ticket}, tenant)
		return e
	})
	return o, err
}

// TestOperator00057_TheOverviewReadsTheAPIState: through the shipped statement, a tenant whose API was
// never opened reads (nil, nil); after op_enable_tenant_api 'KF' it reads the instant the database
// wrote into api_enabled_at and KF; after op_disable_tenant_api (nil, KF) -- the code kept; the counts
// beside are 00029's. A second tenant's state is not the first's (the belt: the API columns come from
// the row named by its id). The 'read' row of each view names the tenant.
//
// MUTANT (measured): 00057's two API columns read from a tenants row other than the one the read
// names (a scalar subquery over every tenant, the most recently opened first) -> a new tenant reads
// another tenant's instant and code, red.
func TestOperator00057_TheOverviewReadsTheAPIState(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenant := opNewTenant(t, ctx, tx, "m11 api13a overview "+opToken(t), 0)
	other := opNewTenant(t, ctx, tx, "m11 api13a other "+opToken(t), 0)
	xact := opCommittedXact(t, ctx)
	read := func(id uuid.UUID) TenantOverview {
		t.Helper()
		o, err := opReadOverview(t, ctx, tx, hash, opForgeDetail(t, ctx, tx, session, a.id, id, xact), id)
		if err != nil {
			t.Fatalf("the overview of %s: %v", id, err)
		}
		if o.ID != id {
			t.Fatalf("the overview of %s names %s", id, o.ID)
		}
		return o
	}
	if o := read(tenant); o.APIEnabledAt != nil || o.APICode != nil || o.Locations != 0 {
		t.Fatalf("a new tenant reads (%v, %v), %d locations; want (nil, nil) and none", o.APIEnabledAt, o.APICode, o.Locations)
	}
	if err := opEnableAPI(t, ctx, tx, hash, tenant, "KF"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	stored := opAPIOf(t, ctx, tx, tenant)
	o := read(tenant)
	if o.APIEnabledAt == nil || stored.enabledAt == nil || !o.APIEnabledAt.Equal(*stored.enabledAt) || o.APICode == nil || *o.APICode != "KF" {
		t.Fatalf("after the enable the overview reads (%v, %v), want (%v, KF)", o.APIEnabledAt, o.APICode, stored.enabledAt)
	}
	if o := read(other); o.APIEnabledAt != nil || o.APICode != nil {
		t.Errorf("ANOTHER tenant reads (%v, %v) after the first's enable, want (nil, nil)", o.APIEnabledAt, o.APICode)
	}
	if err := opDisableAPI(t, ctx, tx, hash, tenant); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if o := read(tenant); o.APIEnabledAt != nil || o.APICode == nil || *o.APICode != "KF" {
		t.Errorf("after the disable the overview reads (%v, %v), want (nil, KF): the code is kept", o.APIEnabledAt, o.APICode)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE kind = 'read' AND session_id = $1
	                            AND target_scope = 'tenant_detail' AND target_tenant_id = $2`, session, tenant); n != 3 {
		t.Errorf("%d 'read' row(s) naming the tenant, want one per view (3)", n)
	}
}

// TestOperator00057_DownGivesBack00029AndUpTakesItAgain: 00057's Down leaves op_read_tenant_detail
// exactly as 00029 made it -- its result, owner, ACL and body (md5(prosrc) is the digest 00057's
// precondition names) -- and its Up gives it the two API columns again, owner and ACL unchanged; the
// Up's own precondition passes over the Down. Inside the test's transaction.
func TestOperator00057_DownGivesBack00029AndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00057File)
	type state struct{ result, owner, acl, md5 string }
	read := func() state {
		t.Helper()
		var s state
		if err := tx.QueryRow(ctx, `SELECT pg_get_function_result(p.oid), pg_get_userbyid(p.proowner), p.proacl::text, md5(p.prosrc)
		                              FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                             WHERE n.nspname = 'public' AND p.proname = 'op_read_tenant_detail'`).
			Scan(&s.result, &s.owner, &s.acl, &s.md5); err != nil {
			t.Fatalf("read the function: %v", err)
		}
		return s
	}
	head := read()
	if !strings.HasSuffix(head.result, "active_admin_count bigint, api_enabled_at timestamp with time zone, api_code text)") {
		t.Fatalf("PREMISE: HEAD's result is %s", head.result)
	}
	opAtVersion(t, ctx, tx, 56, opKindsAt42...)
	at56 := read()
	want56 := state{result: op00029Functions["op_read_tenant_detail"].result, owner: "tappa_opdefiner", acl: head.acl,
		md5: "86d4081dbdae53b128a701784b8f3deb"}
	want56.result = strings.Replace(want56.result, ", api_enabled_at timestamp with time zone, api_code text)", ")", 1)
	if at56 != want56 {
		t.Errorf("after 00057's Down the function is %+v,\n want 00029's %+v", at56, want56)
	}
	if !strings.Contains(up, "IS DISTINCT FROM '86d4081dbdae53b128a701784b8f3deb'") || !strings.Contains(down, "DROP FUNCTION IF EXISTS public.op_read_tenant_detail") {
		t.Fatal("PREMISE: the Up's precondition does not name 00029's digest, or the Down does not drop the function")
	}
	opRunSection(t, ctx, tx, up, "00057 Up over its Down")
	if again := read(); again != head {
		t.Errorf("after Down and Up the function is %+v, want HEAD's %+v", again, head)
	}
}

// TestOperator00057_PreconditionRefusesAChangedFunction: 00057's precondition DO block, run at 00056's
// state, passes; it refuses (SQLSTATE 55000, naming 00057) a function whose body another migration
// changed, one whose result is not 00029's, a cluster where tappa_opdefiner lost SELECT on api_code,
// and a tappa_opdefiner that can log in. Each inside its own savepoint.
func TestOperator00057_PreconditionRefusesAChangedFunction(t *testing.T) {
	ctx, tx := opTx(t)
	up, _ := opMigrationSections(t, op00057File)
	i, j := strings.Index(up, "DO $$"), strings.Index(up, "-- +goose StatementEnd")
	if i < 0 || j < i {
		t.Fatal("00057's Up does not open with the precondition DO block")
	}
	pre := up[i:j]
	opAtVersion(t, ctx, tx, 56, opKindsAt42...)
	body29 := `CREATE OR REPLACE FUNCTION public.op_read_tenant_detail(p_session text, p_ticket text, p_tenant_id uuid)
	    RETURNS TABLE (tenant_id uuid, tenant_name text, created_at timestamptz, plan text, business_type text,
	                   location_count bigint, active_employee_count bigint, active_plaque_count bigint,
	                   active_admin_count bigint)
	    LANGUAGE sql SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $f$ SELECT NULL::uuid, NULL, NULL::timestamptz,
	    NULL, NULL, 0::bigint, 0::bigint, 0::bigint, 0::bigint WHERE false $f$`
	for _, c := range []struct {
		what  string
		setup []string
		want  string
	}{
		{"00056's state", nil, ""},
		{"a changed body", []string{body29}, "replaces op_read_tenant_detail as migration 00029 left it"},
		{"another result", []string{`DROP FUNCTION public.op_read_tenant_detail(text, text, uuid)`,
			`CREATE FUNCTION public.op_read_tenant_detail(p_session text, p_ticket text, p_tenant_id uuid) RETURNS TABLE (x int)
			 LANGUAGE sql AS 'SELECT 1'`, `ALTER FUNCTION public.op_read_tenant_detail(text, text, uuid) OWNER TO tappa_opdefiner`},
			"replaces op_read_tenant_detail as migration 00029 left it"},
		{"no SELECT on api_code", []string{`REVOKE SELECT (api_code) ON tenants FROM tappa_opdefiner`}, "a grant is missing"},
		{"tappa_opdefiner can log in", []string{`ALTER ROLE tappa_opdefiner LOGIN`}, "tappa_opdefiner must be"},
	} {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range c.setup {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: setup %q: %v", c.what, s, err)
			}
		}
		_, err = sp.Exec(ctx, pre)
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: the precondition refused: %v", c.what, err)
		case c.want != "" && (err == nil || !strings.Contains(err.Error(), "migration 00057") || !strings.Contains(err.Error(), c.want) ||
			!strings.Contains(err.Error(), "55000")):
			t.Errorf("%s: %v, want a 55000 naming 00057 and %q", c.what, err, c.want)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
