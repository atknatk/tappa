package operator_test

// op15_db_test.go -- M10 OP-15 phase C end to end against PostgreSQL: the operator signs in
// through the surface, opens a FIXTURE tenant's overview (op_begin_read + op_read_tenant_detail,
// 00063's columns), suspends it through op_suspend_tenant and lifts the suspension through
// op_reinstate_tenant, on newLegalE2E's committed rig (op10_db_test.go: one owner connection
// switched to tappa_operator, each statement its own committed transaction, as production's pool).
//
// WHAT ONE RUN LEAVES BEHIND. TestE2E_OperatorSuspendsAndReinstatesAFixtureTenant COMMITS a
// fixture tenant -- on tappa_app's pool, in the tenant's own context: 1 tenant ('FAKE op15c …',
// a made-up VAT number 'FAKE-OP15C-<hex>', never a real business's) -- and the two rows its run
// writes into that tenant's own audit_log (append-only, 00005): one 'tenant.suspended' and one
// 'tenant.reinstated'. 🔴 THE FIXTURE ENDS NOT SUSPENDED: the run lifts its suspension, and a
// t.Cleanup lifts it again through the surface if a failure left it suspended (ADR 0025, K15-6:
// 00063's Down refuses while any tenant is suspended). NO OTHER TENANT IS SUSPENDED OR WRITTEN: the
// run's third write names an id no tenant has, which writes no tenant row (B14). newLegalE2E
// leaves, by construction, one operator account (op10b-…, disabled at cleanup), its sessions
// (revoked at cleanup) and its operator_audit_log rows -- login, password_ok, and the run's own:
// the 'read' rows of its overview, list and audit-log views, two 'tenant_suspended' rows (the
// fixture's and the unknown id's) and one 'tenant_reinstated' row; read tickets are deleted at
// cleanup. The class is the backlog's T81.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
)

// suspensionFixture commits the fixture tenant (the file header says what it leaves) and returns
// its id and name.
func suspensionFixture(t *testing.T, l *legalE2E) (uuid.UUID, string) {
	t.Helper()
	app, err := db.New(l.ctx, &config.Config{DatabaseURL: os.Getenv("DATABASE_URL")})
	if err != nil {
		t.Fatalf("tappa_app's pool: %v", err)
	}
	t.Cleanup(app.Close)
	id := uuid.New()
	name := "FAKE op15c E2E " + id.String()[:8]
	number := fmt.Sprintf("FAKE-OP15C-%x", randBytes(t, 6))
	err = app.WithTenant(l.ctx, id, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
		                      VALUES ($1, $2, $3, 'restaurant', 'single', 'Europe/Malta')`, id, name, number)
		return e
	})
	if err != nil {
		t.Fatalf("commit the suspension fixture: %v", err)
	}
	return id, name
}

// TestE2E_OperatorSuspendsAndReinstatesAFixtureTenant is OP-15 C's end-to-end acceptance on
// PostgreSQL (ADR 0025 "Güvenlik iddiası" PART I, *C fazında yazılacak*; the file header says what
// it commits):
//
//  0. the operator screen's reason labels are the CHECK's closed set, read from the catalog;
//  1. the fixture's overview says Active and offers the suspend form alone;
//  2. suspend (security_hold, the box ticked): 303 to the overview; the tenant's two columns are
//     the database's wall clock (between the request's start and end) and 'security_hold'; ONE
//     'tenant_suspended' operator row -- the session, the operator, the tenant, detail {} and
//     nothing else; ONE 'tenant.suspended' row in the tenant's own audit_log -- the operator as
//     actor, the tenant as target, detail exactly {actor_kind: operator, reason: security_hold}, its
//     `at` the suspension's start;
//  3. the overview the redirect lands on says Suspended, since that start's minute (UTC), "Security
//     hold", and offers the reinstate form alone; the tenant list's search by its id draws the
//     Suspended chip on its row; the audit log filtered to the kind shows "Tenant suspension
//     recorded" linking the tenant by its name;
//  4. reinstate (the box ticked): 303; both columns NULL; ONE 'tenant_reinstated' operator row of
//     the same shape; ONE 'tenant.reinstated' tenant row, detail exactly {actor_kind: operator}; the
//     overview says Active again;
//  5. suspend an id no tenant has: the SAME 303 to that id's overview, which is the no-such-tenant
//     404; one 'tenant_suspended' operator row naming the id; no tenant, no tenant audit row and no
//     suspension anywhere for it; the audit log shows the row as "A tenant id no tenant has".
func TestE2E_OperatorSuspendsAndReinstatesAFixtureTenant(t *testing.T) {
	l := newLegalE2E(t)
	id, name := suspensionFixture(t, l)
	overview := overviewPathOf(id.String())

	// 0. The labels are the CHECK's set.
	var def string
	if err := l.owner.QueryRow(l.ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
	                                   WHERE conrelid = 'public.tenants'::regclass AND conname = 'tenants_suspension_reason_check'`).Scan(&def); err != nil {
		t.Fatalf("read the reason CHECK: %v", err)
	}
	var labelled []string
	for r := range operator.SuspensionWordsForTest() {
		labelled = append(labelled, string(r))
		if !strings.Contains(def, "'"+string(r)+"'::text") {
			t.Errorf("the screen labels %q, which the CHECK does not name", r)
		}
	}
	if strings.Count(def, "'::text") != len(labelled) || len(labelled) != 4 {
		t.Errorf("the CHECK names %d reasons and the screen labels %d (%v)", strings.Count(def, "'::text"), len(labelled), labelled)
	}

	sess := l.signIn(l.f)
	state := func() (*time.Time, *string) {
		t.Helper()
		var at *time.Time
		var reason *string
		if err := l.owner.QueryRow(l.ctx, `SELECT suspended_at, suspension_reason FROM tenants WHERE id = $1`, id).Scan(&at, &reason); err != nil {
			t.Fatal(err)
		}
		return at, reason
	}
	t.Cleanup(func() {
		if at, _ := state(); at != nil {
			l.post(reinstatePathOf(id.String()), reinstateForm(), sess)
			if at, _ := state(); at != nil {
				t.Errorf("CLEANUP: the fixture tenant %s is still suspended; lift it before a migration's Down (K15-6)", id)
			}
		}
	})
	opRows := func(kind string, target uuid.UUID) int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = $1 AND target_tenant_id = $2 AND actor_admin_id = $3
		                   AND session_id IS NOT NULL AND detail = '{}'::jsonb AND target_scope IS NULL AND target_admin_id IS NULL
		                   AND page_number IS NULL AND page_size IS NULL`, kind, target, l.f.id)
	}
	opAll := func(kind string, target uuid.UUID) int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = $1 AND target_tenant_id = $2`, kind, target)
	}
	type tenantRow struct {
		detail map[string]any
		at     time.Time
	}
	tenantRows := func(action string, tenant uuid.UUID) []tenantRow {
		t.Helper()
		rows, err := l.owner.Query(l.ctx, `SELECT detail::text, actor_id, target, at FROM audit_log
		                                   WHERE tenant_id = $1 AND action = $2 ORDER BY at, id`, tenant, action)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []tenantRow
		for rows.Next() {
			var detail, target string
			var actor uuid.UUID
			var r tenantRow
			if err := rows.Scan(&detail, &actor, &target, &r.at); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(detail), &r.detail); err != nil {
				t.Fatalf("a tenant row's detail is not JSON: %v", err)
			}
			if actor != l.f.id || target != tenant.String() {
				t.Errorf("a %s row names actor %s and target %q, want the operator and the tenant", action, actor, target)
			}
			out = append(out, r)
		}
		return out
	}

	// 1. Active, the suspend form alone.
	w := l.get(overview, sess)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), chipActive) ||
		!strings.Contains(w.Body.String(), `action="`+suspendPathOf(id.String())+`"`) ||
		strings.Contains(w.Body.String(), `action="`+reinstatePathOf(id.String())+`"`) {
		t.Fatalf("the fixture's overview = %d, or it is not Active with the suspend form alone; log: %s", w.Code, l.logs.String())
	}

	// 2. Suspend.
	start := time.Now()
	w = l.post(suspendPathOf(id.String()), suspendForm("security_hold"), sess)
	end := time.Now()
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != overview {
		t.Fatalf("the suspension = %d to %q, want 303 to the overview; log: %s", w.Code, w.Result().Header.Get("Location"), l.logs.String())
	}
	at, reason := state()
	if at == nil || reason == nil || *reason != "security_hold" || at.Before(start.Add(-2*time.Second)) || at.After(end.Add(2*time.Second)) {
		t.Fatalf("the suspension stored %v / %v, want security_hold at the wall clock of the request", at, reason)
	}
	if n := opRows("tenant_suspended", id); n != 1 || opAll("tenant_suspended", id) != 1 {
		t.Errorf("%d tenant_suspended row(s) of the session's, the operator's and the tenant's shape (%d in all), want 1", n, opAll("tenant_suspended", id))
	}
	rows := tenantRows("tenant.suspended", id)
	if len(rows) != 1 || len(rows[0].detail) != 2 || rows[0].detail["actor_kind"] != "operator" || rows[0].detail["reason"] != "security_hold" ||
		!rows[0].at.Equal(*at) {
		t.Fatalf("the tenant's audit row is not {actor_kind: operator, reason: security_hold} at the suspension's start: %+v", rows)
	}

	// 3. What the operator reads.
	body := l.get(overview, sess).Body.String()
	if !strings.Contains(body, chipSuspended) || !strings.Contains(body, at.UTC().Format("2006-01-02 15:04")+" UTC") ||
		!strings.Contains(body, wordSecurityHold) || !strings.Contains(body, `action="`+reinstatePathOf(id.String())+`"`) ||
		strings.Contains(body, `action="`+suspendPathOf(id.String())+`"`) {
		t.Errorf("the overview after the suspension does not say Suspended, its start, the reason and offer the reinstate form alone")
	}
	list := l.post("/operator/tenants", url.Values{"q": {id.String()}}, sess).Body.String()
	if !strings.Contains(list, chipSuspended) || !strings.Contains(list, id.String()) {
		t.Errorf("the list's row of the suspended tenant does not carry the chip")
	}
	audit := l.post("/operator/audit", url.Values{"kind": {"tenant_suspended"}}, sess).Body.String()
	if !strings.Contains(audit, "Tenant suspension recorded") || !strings.Contains(audit, `href="`+overview+`"`) || !strings.Contains(audit, name) {
		t.Errorf("the audit log does not show the suspension's row with the tenant's name")
	}

	// 4. Reinstate.
	start = time.Now()
	w = l.post(reinstatePathOf(id.String()), reinstateForm(), sess)
	end = time.Now()
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != overview {
		t.Fatalf("the reinstatement = %d to %q, want 303 to the overview", w.Code, w.Result().Header.Get("Location"))
	}
	if at, reason := state(); at != nil || reason != nil {
		t.Fatalf("the reinstatement left %v / %v, want both NULL", at, reason)
	}
	if n := opRows("tenant_reinstated", id); n != 1 || opAll("tenant_reinstated", id) != 1 {
		t.Errorf("%d tenant_reinstated row(s) of the expected shape (%d in all), want 1", n, opAll("tenant_reinstated", id))
	}
	rows = tenantRows("tenant.reinstated", id)
	if len(rows) != 1 || len(rows[0].detail) != 1 || rows[0].detail["actor_kind"] != "operator" ||
		rows[0].at.Before(start.Add(-2*time.Second)) || rows[0].at.After(end.Add(2*time.Second)) {
		t.Errorf("the tenant's reinstatement row is not {actor_kind: operator} at the wall clock: %+v", rows)
	}
	if body := l.get(overview, sess).Body.String(); !strings.Contains(body, chipActive) || strings.Contains(body, chipSuspended) {
		t.Errorf("the overview after the reinstatement does not say Active")
	}

	// 5. An id no tenant has.
	unknown := uuid.New()
	w = l.post(suspendPathOf(unknown.String()), suspendForm("non_payment"), sess)
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != overviewPathOf(unknown.String()) {
		t.Fatalf("the suspension of an unknown id = %d to %q, want the same 303 to its overview", w.Code, w.Result().Header.Get("Location"))
	}
	if w := l.get(overviewPathOf(unknown.String()), sess); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "There is no tenant with that id") {
		t.Errorf("the unknown id's overview = %d, want the no-such-tenant 404", w.Code)
	}
	if n := opRows("tenant_suspended", unknown); n != 1 {
		t.Errorf("%d tenant_suspended row(s) naming the unknown id, want 1", n)
	}
	if n := l.ownerInt(`SELECT (SELECT count(*) FROM tenants WHERE id = $1) + (SELECT count(*) FROM audit_log WHERE tenant_id = $1 OR target = $2)
	                    + (SELECT count(*) FROM tenants WHERE suspended_at IS NOT NULL AND id = $1)`, unknown, unknown.String()); n != 0 {
		t.Errorf("the unknown id has %d tenant, tenant audit or suspension row(s), want none", n)
	}
	audit = l.post("/operator/audit", url.Values{"kind": {"tenant_suspended"}}, sess).Body.String()
	if !strings.Contains(audit, "A tenant id no tenant has") || !strings.Contains(audit, unknown.String()) {
		t.Errorf("the audit log does not show the unknown id's row as an id no tenant has")
	}
}
