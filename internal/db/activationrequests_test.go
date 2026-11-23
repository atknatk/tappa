package db

// activationrequests_test.go -- migration 00115 (M16-23, ADR 0026 Bedel 3, ADR 0045 §2.5):
// activation_requests, the manager-approved activation, and the eight queries of
// db/queries/activationrequests.sql, against real Postgres. Nothing here prints a pairing code or a
// binding digest: values are compared, never shown (§7).

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
)

const op00115File = "00115_create_activation_requests.sql"

// activationRequestsMigration is 00115's number.
const activationRequestsMigration = 115

// activationRequestsSetAsideSQL removes every request inside a walk's transaction so 00115's Down --
// which refuses while the table holds a row -- can run; the rollback brings them back. No trigger
// guards a delete on the table (tappa_app holds no DELETE), so the owner needs nothing switched off.
const activationRequestsSetAsideSQL = `DELETE FROM public.activation_requests;`

// arDownTables are the five tables 00115's Down LOCKs in one statement before it reads anything.
var arDownTables = []string{"activation_requests", "admin_users", "employee_invites", "employees", "tenants"}

// arPairingCode draws a six-digit pairing code (the CHECK's shape). Drawn, never a literal.
func arPairingCode(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return fmt.Sprintf("%06d", binary.BigEndian.Uint64(b[:])%1_000_000)
}

// arEmployee adds an active person to w's business as the owner (rolled back with the test). email
// and location may be nil (no address; no home venue, K-14 -- such a person carries an external
// reference, employees_home_or_external_ref).
func (w appDeviceWorld) arEmployee(t *testing.T, ctx context.Context, tx pgx.Tx, email, location any) uuid.UUID {
	t.Helper()
	id := uuid.New()
	var ref any
	if location == nil {
		ref = "M1623-" + id.String()[:8]
	}
	if _, err := tx.Exec(ctx, `INSERT INTO employees (id, tenant_id, location_id, full_name, email, status, external_ref)
	                           VALUES ($1, $2, $3, $4, $5, 'active', $6)`, id, w.tenant, location, "Person "+id.String()[:8], email, ref); err != nil {
		t.Fatalf("add an employee: %v", err)
	}
	return id
}

// arInvite gives employee a live invitation consented to by a fresh binding, as the owner, and
// returns the invitation and the binding digest.
func (w appDeviceWorld) arInvite(t *testing.T, ctx context.Context, tx pgx.Tx, employee uuid.UUID) (uuid.UUID, string) {
	t.Helper()
	id, binding := uuid.New(), opRandHex(t)
	if _, err := tx.Exec(ctx, `INSERT INTO employee_invites (id, tenant_id, employee_id, code_hash, expires_at, consented_at, consent_binding_hash)
	                           VALUES ($1, $2, $3, $4, now() + interval '7 days', now(), $5)`, id, w.tenant, employee, opRandHex(t), binding); err != nil {
		t.Fatalf("add an invitation: %v", err)
	}
	return id, binding
}

// arAdmin adds a panel account (a manager) to w's business as the owner, inside the test's opTx
// transaction -- which holds the operator-tables lock EXCLUSIVE and is rolled back: nothing commits.
func (w appDeviceWorld) arAdmin(t *testing.T, ctx context.Context, tx pgx.Tx, email string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
	                           VALUES ($1, $2, 'M16-23 Manager', $3, $4, 'manager')`, id, w.tenant, email, fixtures.UnusablePasswordHash); err != nil {
		t.Fatalf("add a panel account: %v", err)
	}
	return id
}

// arEmail is a fresh address.
func arEmail() string { return "m1623-" + uuid.NewString() + "@example.test" }

// arQ runs fn against the product's queries as tappa_app in w's context.
func (w appDeviceWorld) arQ(t *testing.T, ctx context.Context, tx pgx.Tx, fn func(q *store.Queries) error) error {
	t.Helper()
	return w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error { return fn(store.New(sp)) })
}

// arExec runs one statement as tappa_app in w's context.
func (w appDeviceWorld) arExec(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) error {
	t.Helper()
	return w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, sql, args...)
		return err
	})
}

// arCreate opens a request through the product's query, as tappa_app.
func (w appDeviceWorld) arCreate(t *testing.T, ctx context.Context, tx pgx.Tx, invite uuid.UUID, binding string, ttl int32) (store.CreateActivationRequestRow, error) {
	t.Helper()
	var row store.CreateActivationRequestRow
	err := w.arQ(t, ctx, tx, func(q *store.Queries) error {
		var e error
		row, e = q.CreateActivationRequest(ctx, store.CreateActivationRequestParams{
			Client: "browser", BindingHash: binding, PairingCode: arPairingCode(t), TtlSeconds: ttl, TenantID: w.tenant, InviteID: invite,
		})
		return e
	})
	return row, err
}

// arOpen is arCreate that must succeed; it returns the request's id.
func (w appDeviceWorld) arOpen(t *testing.T, ctx context.Context, tx pgx.Tx, invite uuid.UUID, binding string) uuid.UUID {
	t.Helper()
	row, err := w.arCreate(t, ctx, tx, invite, binding, 600)
	if err != nil {
		t.Fatalf("open an activation request: %v", err)
	}
	return row.ID
}

// arDecide decides a request through the product's query, as tappa_app.
func (w appDeviceWorld) arDecide(t *testing.T, ctx context.Context, tx pgx.Tx, id, admin uuid.UUID, decision string) (store.DecideActivationRequestRow, error) {
	t.Helper()
	var row store.DecideActivationRequestRow
	err := w.arQ(t, ctx, tx, func(q *store.Queries) error {
		var e error
		row, e = q.DecideActivationRequest(ctx, store.DecideActivationRequestParams{Decision: decision, DecidedBy: &admin, TenantID: w.tenant, ID: id})
		return e
	})
	return row, err
}

// arCollect collects an approval through the product's query, as tappa_app.
func (w appDeviceWorld) arCollect(t *testing.T, ctx context.Context, tx pgx.Tx, invite uuid.UUID, binding string) (store.CollectActivationApprovalRow, error) {
	t.Helper()
	var row store.CollectActivationApprovalRow
	err := w.arQ(t, ctx, tx, func(q *store.Queries) error {
		var e error
		row, e = q.CollectActivationApproval(ctx, store.CollectActivationApprovalParams{TenantID: w.tenant, InviteID: invite, BindingHash: binding})
		return e
	})
	return row, err
}

// arForge runs statements as the owner with every trigger off (session_replication_role = replica:
// the one-way triggers and the keys' RI triggers do not fire; every CHECK still does), inside a
// savepoint, and puts the role back before the savepoint is released. It writes the states the
// one-way triggers make unreachable inside one transaction -- a decision or a pairing in the past.
func arForge(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) {
	t.Helper()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		sql  string
		args []any
	}{{`SET LOCAL session_replication_role = replica`, nil}, {sql, args}, {`SET LOCAL session_replication_role = origin`, nil}} {
		if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("forge %q: %v", s.sql, err)
		}
	}
	if err := sp.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestActivationRequestsCatalog measures, in the live catalog, the RLS five on the table, the keys
// and the one-open UNIQUE, tappa_app's reach column by column (ADR 0028 §1's `arwd` rule: SELECT
// every column, INSERT the seven, UPDATE the five state columns, no DELETE, TRUNCATE, REFERENCES or
// TRIGGER), that no other application role reaches the table at all (tappa_resolver included -- no
// resolver reads it), and the trigger function: INVOKER, EXECUTE in its owner alone, bound by its two
// triggers, refusing any other table (TG_RELID).
func TestActivationRequestsCatalog(t *testing.T) {
	ctx, tx := opTx(t)

	var enabled, forced, notNull bool
	if err := tx.QueryRow(ctx, `SELECT c.relrowsecurity, c.relforcerowsecurity, a.attnotnull
	                              FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'tenant_id'
	                             WHERE c.oid = 'public.activation_requests'::regclass`).Scan(&enabled, &forced, &notNull); err != nil {
		t.Fatal(err)
	}
	if !enabled || !forced || !notNull {
		t.Errorf("activation_requests: RLS ENABLE %v FORCE %v, tenant_id NOT NULL %v; want all three", enabled, forced, notNull)
	}
	var policy string
	if err := tx.QueryRow(ctx, `SELECT pg_get_expr(polqual, polrelid) || ' / ' || pg_get_expr(polwithcheck, polrelid)
	                              FROM pg_policy WHERE polrelid = 'public.activation_requests'::regclass`).Scan(&policy); err != nil {
		t.Fatalf("the one policy: %v", err)
	}
	if strings.Count(policy, "NULLIF") != 2 || strings.Count(policy, "(tenant_id = ") != 2 || strings.Contains(strings.ToUpper(policy), " OR ") {
		t.Errorf("policy %q, want tenant_id = the NULLIF form in USING and WITH CHECK and no OR branch", policy)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_policy WHERE polrelid = 'public.activation_requests'::regclass`); n != 1 {
		t.Errorf("activation_requests holds %d policies, want 1 (a second permissive policy ORs into the first)", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
	                            WHERE i.indrelid = 'public.activation_requests'::regclass AND a.attname = 'tenant_id'`); n < 1 {
		t.Error("activation_requests: no index leads with tenant_id")
	}
	for _, c := range []struct{ name, def string }{
		{"activation_requests_pkey", "PRIMARY KEY (id)"},
		{"activation_requests_employee_fk", "FOREIGN KEY (employee_id, tenant_id) REFERENCES employees(id, tenant_id) ON DELETE RESTRICT"},
		{"activation_requests_decided_by_fk", "FOREIGN KEY (decided_by, tenant_id) REFERENCES admin_users(id, tenant_id) ON DELETE RESTRICT"},
		{"activation_requests_invite_id_fkey", "FOREIGN KEY (invite_id) REFERENCES employee_invites(id) ON DELETE RESTRICT"},
		{"activation_requests_tenant_id_fkey", "FOREIGN KEY (tenant_id) REFERENCES tenants(id) ON DELETE RESTRICT"},
	} {
		var def string
		if err := tx.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
		                             WHERE conrelid = 'public.activation_requests'::regclass AND conname = $1`, c.name).Scan(&def); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if def != c.def {
			t.Errorf("%s is %q, want %q", c.name, def, c.def)
		}
	}
	var unique string
	if err := tx.QueryRow(ctx, `SELECT pg_get_indexdef('public.activation_requests_one_open_key'::regclass)`).Scan(&unique); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unique, "CREATE UNIQUE INDEX") || !strings.Contains(unique, "(invite_id) WHERE ((decision IS NULL) AND (withdrawn_at IS NULL))") {
		t.Errorf("the one-open key is %q, want UNIQUE (invite_id) over the undecided, unwithdrawn rows", unique)
	}

	for _, c := range []struct{ priv, want string }{
		{"SELECT", "id,tenant_id,employee_id,invite_id,client,binding_hash,pairing_code,created_at,expires_at,decision,decided_at,decided_by,withdrawn_at,collected_at"},
		{"INSERT", "tenant_id,employee_id,invite_id,client,binding_hash,pairing_code,expires_at"},
		{"UPDATE", "decision,decided_at,decided_by,withdrawn_at,collected_at"},
		{"REFERENCES", ""},
	} {
		if got := opColumns(t, ctx, tx, "tappa_app", "activation_requests", c.priv); got != c.want {
			t.Errorf("tappa_app %s on activation_requests: %q, want %q", c.priv, got, c.want)
		}
	}
	for _, priv := range []string{"DELETE", "TRUNCATE", "TRIGGER", "REFERENCES"} {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_table_privilege('tappa_app', 'public.activation_requests', $1)`, priv).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may {
			t.Errorf("tappa_app holds %s on activation_requests; a request is never removed", priv)
		}
	}
	// Every other non-superuser application role: nothing, table-wide or by column.
	rows, err := tx.Query(ctx, `
		SELECT r.rolname FROM pg_roles r
		 WHERE r.rolname ~ '^tappa_' AND NOT r.rolsuper AND r.rolname <> 'tappa_app'
		   AND (has_any_column_privilege(r.oid, 'public.activation_requests'::regclass, 'SELECT')
		     OR has_any_column_privilege(r.oid, 'public.activation_requests'::regclass, 'INSERT')
		     OR has_any_column_privilege(r.oid, 'public.activation_requests'::regclass, 'UPDATE')
		     OR has_any_column_privilege(r.oid, 'public.activation_requests'::regclass, 'REFERENCES')
		     OR has_table_privilege(r.oid, 'public.activation_requests'::regclass, 'DELETE')
		     OR has_table_privilege(r.oid, 'public.activation_requests'::regclass, 'TRUNCATE')
		     OR has_table_privilege(r.oid, 'public.activation_requests'::regclass, 'TRIGGER'))
		 ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	reach, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	if len(reach) > 0 {
		t.Errorf("roles other than tappa_app reach activation_requests: %v (tappa_resolver above all: no resolver reads it)", reach)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_roles WHERE rolname = 'tappa_resolver'`); n != 1 {
		t.Error("PREMISE: tappa_resolver is not a role here; the line above proved nothing about it")
	}

	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proname = 'activation_requests_state_is_one_way'
	                            AND NOT prosecdef AND proacl::text = '{tappa_owner=X/tappa_owner}'`); n != 1 {
		t.Error("activation_requests_state_is_one_way is not an INVOKER function with EXECUTE in its owner alone")
	}
	for _, who := range []string{"public", "tappa_app", "tappa_resolver", "tappa_operator"} {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_function_privilege($1, 'public.activation_requests_state_is_one_way()', 'EXECUTE')`, who).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may {
			t.Errorf("%s may EXECUTE the trigger function", who)
		}
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_trigger WHERE tgrelid = 'public.activation_requests'::regclass AND NOT tgisinternal
	                            AND tgfoid = 'public.activation_requests_state_is_one_way()'::regprocedure
	                            AND tgname IN ('activation_requests_born_open', 'activation_requests_state_one_way')`); n != 2 {
		t.Errorf("the state function is bound by %d of its two triggers (born open, state one way)", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_trigger WHERE tgrelid = 'public.activation_requests'::regclass AND NOT tgisinternal`); n != 2 {
		t.Errorf("activation_requests carries %d user triggers, want exactly the two", n)
	}
	for _, s := range []string{
		`CREATE TEMP TABLE zz_m1623_bound (decision text, decided_at timestamptz, decided_by uuid, withdrawn_at timestamptz, collected_at timestamptz)`,
		`CREATE TRIGGER zz_m1623 BEFORE UPDATE ON zz_m1623_bound FOR EACH ROW EXECUTE FUNCTION public.activation_requests_state_is_one_way()`,
		`INSERT INTO zz_m1623_bound VALUES (NULL, NULL, NULL, NULL, NULL)`,
	} {
		if _, err := tx.Exec(ctx, s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	opWant(t, opTry(t, ctx, tx, `UPDATE zz_m1623_bound SET withdrawn_at = now()`), sqlstatePrerequisiteState,
		"the activation request trigger function on a table that is not activation_requests")
}

// TestRLS_ActivationRequests_TenantIsolation is CLAUDE.md §8's RLS test for the new table, in the
// FILTERLESS shape (§6): as tappa_app in B's context a bare `WHERE id = <A's request>` (and a bare
// `WHERE invite_id = <A's invitation>`) is 0 rows, the positive control in A's context is 1 and no
// context is 0 -- so the 0 is RLS's, not a WHERE's. B cannot withdraw, decide or collect A's request
// (bare UPDATEs: 0 rows); B's INSERT stamped with A's tenant is refused -- by the born-open trigger's
// invitation check first (23503: BEFORE triggers run before the policy), and with that trigger set
// aside by WITH CHECK itself (42501); B cannot open a request of its own on A's invitation (23503).
//
// CONTROL: with RLS off on the table B's bare read sees A's request -- the probe reaches the row.
func TestRLS_ActivationRequests_TenantIsolation(t *testing.T) {
	ctx, tx := opTx(t)
	a, b := newAppDeviceWorld(t, ctx, tx), newAppDeviceWorld(t, ctx, tx)
	inviteA, bindingA := a.arInvite(t, ctx, tx, a.employee)
	reqA := a.arOpen(t, ctx, tx, inviteA, bindingA)
	inviteB, bindingB := b.arInvite(t, ctx, tx, b.employee)
	count := func(ctxTenant uuid.UUID, sql string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := a.asApp(t, ctx, tx, ctxTenant, func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, sql, args...).Scan(&n)
		}); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return n
	}
	for _, c := range []struct {
		sql string
		arg any
	}{
		{`SELECT count(*) FROM activation_requests WHERE id = $1`, reqA},
		{`SELECT count(*) FROM activation_requests WHERE invite_id = $1`, inviteA},
	} {
		if n := count(b.tenant, c.sql, c.arg); n != 0 {
			t.Fatalf("B's context: %q = %d, want 0", c.sql, n)
		}
		if n := count(a.tenant, c.sql, c.arg); n != 1 {
			t.Fatalf("POSITIVE CONTROL: A's context: %q = %d, want 1", c.sql, n)
		}
		if n := count(uuid.Nil, c.sql, c.arg); n != 0 {
			t.Fatalf("no context: %q = %d, want 0", c.sql, n)
		}
	}
	adminB := b.arAdmin(t, ctx, tx, arEmail())
	for _, sql := range []string{
		`WITH u AS (UPDATE activation_requests SET withdrawn_at = now() WHERE id = $1 RETURNING 1) SELECT count(*) FROM u`,
		`WITH u AS (UPDATE activation_requests SET decision = 'approved', decided_at = now(), decided_by = '` + adminB.String() + `' WHERE id = $1 RETURNING 1) SELECT count(*) FROM u`,
		`WITH u AS (UPDATE activation_requests SET collected_at = now() WHERE id = $1 RETURNING 1) SELECT count(*) FROM u`,
	} {
		if n := count(b.tenant, sql, reqA); n != 0 {
			t.Fatalf("B's context: %q touched %d of A's requests, want 0", sql, n)
		}
	}
	insert := `INSERT INTO activation_requests (tenant_id, employee_id, invite_id, client, binding_hash, pairing_code, expires_at)
	           VALUES ($1, $2, $3, 'browser', $4, $5, now() + interval '10 minutes')`
	opWant(t, b.arExec(t, ctx, tx, insert, a.tenant, a.employee, inviteA, bindingA, arPairingCode(t)),
		sqlstateForeignKeyViolation, "B inserting a request stamped with A's tenant (the born-open trigger's invitation check, first)")
	// WITH CHECK itself: the born-open trigger set aside inside a savepoint (its ACCESS EXCLUSIVE goes
	// with the rollback to it), B's INSERT stamped with A's tenant meets the policy.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `ALTER TABLE activation_requests DISABLE TRIGGER activation_requests_born_open`); err != nil {
		t.Fatal(err)
	}
	opWant(t, b.asApp(t, ctx, sp, b.tenant, func(as pgx.Tx) error {
		_, err := as.Exec(ctx, insert, a.tenant, a.employee, inviteA, bindingA, arPairingCode(t))
		return err
	}), sqlstateInsufficientPrivi, "B inserting a request stamped with A's tenant, the trigger aside (WITH CHECK)")
	if err := b.asApp(t, ctx, sp, b.tenant, func(as pgx.Tx) error {
		_, err := as.Exec(ctx, insert, b.tenant, b.employee, inviteB, bindingB, arPairingCode(t))
		return err
	}); err != nil {
		t.Fatalf("POSITIVE CONTROL: B inserting its own request with the trigger aside: %v", err)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	opWant(t, b.arExec(t, ctx, tx, insert, b.tenant, b.employee, inviteA, bindingA, arPairingCode(t)),
		sqlstateForeignKeyViolation, "B opening a request of its own on A's invitation")
	if err := b.arQ(t, ctx, tx, func(q *store.Queries) error {
		_, err := q.CreateActivationRequest(ctx, store.CreateActivationRequestParams{
			Client: "browser", BindingHash: bindingA, PairingCode: arPairingCode(t), TtlSeconds: 600, TenantID: a.tenant, InviteID: inviteA,
		})
		return err
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("B opening a request on A's invitation through the product's query, naming A's tenant: %v, want pgx.ErrNoRows", err)
	}
	if n := count(a.tenant, `SELECT count(*) FROM activation_requests WHERE id = $1 AND decision IS NULL AND withdrawn_at IS NULL AND collected_at IS NULL`, reqA); n != 1 {
		t.Fatal("A's request was touched by B's attempts")
	}

	sp, err = tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{`ALTER TABLE activation_requests NO FORCE ROW LEVEL SECURITY`, `ALTER TABLE activation_requests DISABLE ROW LEVEL SECURITY`} {
		if _, err := sp.Exec(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	var n int64
	if err := b.asApp(t, ctx, sp, b.tenant, func(as pgx.Tx) error {
		return as.QueryRow(ctx, `SELECT count(*) FROM activation_requests WHERE id = $1`, reqA).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("CONTROL: with RLS off B's bare read sees %d of A's requests, want 1 -- the probe is not reaching the row", n)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestActivationRequests_StateIsOneWay holds the trigger's history rules, through tappa_app (the
// product's role) and, where a row shape tappa_app cannot write, through the owner:
//   - born open: an INSERT carrying a decision, a withdrawal or a collection is refused (23001, the
//     owner -- tappa_app holds no INSERT on those columns, 42501), and tappa_app cannot write id or
//     created_at;
//   - the invitation is the request's own: another employee's, another tenant's -- 23503;
//   - a decision is made once: approved -> rejected, a re-stamp and another decider are refused;
//   - a withdrawn request is not decided, and a withdrawal is stamped once;
//   - a collection is stamped once and never undone;
//   - every stamp is the moment it happens: back-dated and future stamps are refused (23001).
func TestActivationRequests_StateIsOneWay(t *testing.T) {
	ctx, tx := opTx(t)
	w, other := newAppDeviceWorld(t, ctx, tx), newAppDeviceWorld(t, ctx, tx)
	admin, admin2 := w.arAdmin(t, ctx, tx, arEmail()), w.arAdmin(t, ctx, tx, arEmail())
	exec := func(sql string, args ...any) error { return w.arExec(t, ctx, tx, sql, args...) }
	both := func(what, sql string, args ...any) {
		t.Helper()
		opWant(t, exec(sql, args...), sqlstateRestrictViolation, "tappa_app: "+what)
		opWant(t, opTry(t, ctx, tx, sql, args...), sqlstateRestrictViolation, "the owner: "+what)
	}

	// Born open.
	invite, binding := w.arInvite(t, ctx, tx, w.employee)
	for _, c := range []struct{ what, cols, vals string }{
		{"a decision", ", decision, decided_at, decided_by", ", 'approved', now(), '" + admin.String() + "'"},
		{"a withdrawal", ", withdrawn_at", ", now()"},
		{"a collection", ", collected_at", ", now()"},
	} {
		sql := `INSERT INTO activation_requests (tenant_id, employee_id, invite_id, client, binding_hash, pairing_code, expires_at` + c.cols + `)
		        VALUES ($1, $2, $3, 'app', $4, $5, now() + interval '10 minutes'` + c.vals + `)`
		opWant(t, exec(sql, w.tenant, w.employee, invite, binding, arPairingCode(t)), sqlstateInsufficientPrivi, "tappa_app inserting a request with "+c.what)
		opWant(t, opTry(t, ctx, tx, sql, w.tenant, w.employee, invite, binding, arPairingCode(t)), sqlstateRestrictViolation, "the owner inserting a request born with "+c.what)
	}
	for _, c := range []struct{ what, col, val string }{
		{"created_at", "created_at", "now() - interval '1 hour'"},
		{"id", "id", "'" + uuid.NewString() + "'"},
	} {
		opWant(t, exec(`INSERT INTO activation_requests (tenant_id, employee_id, invite_id, client, binding_hash, pairing_code, expires_at, `+c.col+`)
		                VALUES ($1, $2, $3, 'app', $4, $5, now() + interval '10 minutes', `+c.val+`)`, w.tenant, w.employee, invite, binding, arPairingCode(t)),
			sqlstateInsufficientPrivi, "tappa_app writing "+c.what)
	}

	// The invitation is the request's own tenant's and employee's.
	someoneElse := w.arEmployee(t, ctx, tx, nil, w.venue)
	otherInvite, otherBinding := other.arInvite(t, ctx, tx, other.employee)
	raw := `INSERT INTO activation_requests (tenant_id, employee_id, invite_id, client, binding_hash, pairing_code, expires_at)
	        VALUES ($1, $2, $3, 'browser', $4, $5, now() + interval '10 minutes')`
	opWant(t, exec(raw, w.tenant, someoneElse, invite, binding, arPairingCode(t)), sqlstateForeignKeyViolation, "tappa_app: a request naming another employee's invitation")
	opWant(t, opTry(t, ctx, tx, raw, w.tenant, someoneElse, invite, binding, arPairingCode(t)), sqlstateForeignKeyViolation, "the owner: a request naming another employee's invitation")
	opWant(t, exec(raw, w.tenant, w.employee, otherInvite, otherBinding, arPairingCode(t)), sqlstateForeignKeyViolation, "tappa_app: a request naming another tenant's invitation")
	opWant(t, opTry(t, ctx, tx, raw, w.tenant, w.employee, otherInvite, otherBinding, arPairingCode(t)), sqlstateForeignKeyViolation, "the owner: a request naming another tenant's invitation")
	if err := exec(raw, w.tenant, w.employee, invite, binding, arPairingCode(t)); err != nil {
		t.Fatalf("POSITIVE CONTROL: tappa_app opening a request on its own invitation by hand: %v", err)
	}
	if n, err := withdraw(t, ctx, tx, w, invite); err != nil || n != 1 {
		t.Fatalf("withdraw the hand-made request: %d (%v)", n, err)
	}

	// A decision is made once.
	approved := w.arOpen(t, ctx, tx, invite, binding)
	if _, err := w.arDecide(t, ctx, tx, approved, admin, "approved"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	both("an approval turned into a rejection", `UPDATE activation_requests SET decision = 'rejected' WHERE id = $1`, approved)
	both("a decision re-stamped", `UPDATE activation_requests SET decided_at = clock_timestamp() WHERE id = $1`, approved)
	both("a decision handed to another manager", `UPDATE activation_requests SET decided_by = $2 WHERE id = $1`, approved, admin2)
	both("a decision undone", `UPDATE activation_requests SET decision = NULL, decided_at = NULL, decided_by = NULL WHERE id = $1`, approved)

	// A collection is stamped once and never undone; a back-dated or future one is refused.
	inv2, bind2 := w.arInvite(t, ctx, tx, someoneElse)
	collectable := w.arOpen(t, ctx, tx, inv2, bind2)
	if _, err := w.arDecide(t, ctx, tx, collectable, admin, "approved"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	opWant(t, exec(`UPDATE activation_requests SET collected_at = now() - interval '1 second' WHERE id = $1`, collectable),
		sqlstateRestrictViolation, "a back-dated collection")
	opWant(t, exec(`UPDATE activation_requests SET collected_at = clock_timestamp() + interval '1 minute' WHERE id = $1`, collectable),
		sqlstateRestrictViolation, "a collection stamped in the future")
	if _, err := w.arCollect(t, ctx, tx, inv2, bind2); err != nil {
		t.Fatalf("collect: %v", err)
	}
	both("a collection re-stamped", `UPDATE activation_requests SET collected_at = clock_timestamp() WHERE id = $1`, collectable)
	both("a collection undone", `UPDATE activation_requests SET collected_at = NULL WHERE id = $1`, collectable)

	// A withdrawn request is not decided; a withdrawal is stamped once.
	invite3, binding3 := w.arInvite(t, ctx, tx, w.arEmployee(t, ctx, tx, nil, w.venue))
	first := w.arOpen(t, ctx, tx, invite3, binding3)
	if n, err := withdraw(t, ctx, tx, w, invite3); err != nil || n != 1 {
		t.Fatalf("withdraw: %d (%v)", n, err)
	}
	both("a withdrawn request approved", `UPDATE activation_requests SET decision = 'approved', decided_at = now(), decided_by = $2 WHERE id = $1`, first, admin)
	if _, err := w.arDecide(t, ctx, tx, first, admin, "approved"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("the product deciding a withdrawn request: %v, want pgx.ErrNoRows", err)
	}
	both("a withdrawal re-stamped", `UPDATE activation_requests SET withdrawn_at = clock_timestamp() WHERE id = $1`, first)
	both("a withdrawal undone", `UPDATE activation_requests SET withdrawn_at = NULL WHERE id = $1`, first)

	// Every stamp is the moment it happens.
	open := w.arOpen(t, ctx, tx, invite3, binding3)
	for _, c := range []struct{ what, set string }{
		{"a back-dated decision", `decision = 'approved', decided_at = now() - interval '1 second', decided_by = $2`},
		{"a decision stamped in the future", `decision = 'approved', decided_at = clock_timestamp() + interval '1 minute', decided_by = $2`},
		{"a back-dated withdrawal", `withdrawn_at = now() - interval '1 second'`},
		{"a withdrawal stamped in the future", `withdrawn_at = clock_timestamp() + interval '1 minute'`},
	} {
		args := []any{open}
		if strings.Contains(c.set, "$2") {
			args = append(args, admin)
		}
		opWant(t, exec(`UPDATE activation_requests SET `+c.set+` WHERE id = $1`, args...), sqlstateRestrictViolation, "tappa_app: "+c.what)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM activation_requests WHERE id = $1 AND decision IS NULL AND withdrawn_at IS NULL`, open); n != 1 {
		t.Fatal("a refused stamp changed the open request")
	}
	// The positive control: a decision stamped now, by the product's statement.
	if row, err := w.arDecide(t, ctx, tx, open, admin, "rejected"); err != nil || row.Decision == nil || *row.Decision != "rejected" || row.DecidedAt == nil {
		t.Fatalf("POSITIVE CONTROL: rejecting the open request: %+v (%v)", row, err)
	}
}

// withdraw runs WithdrawOpenActivationRequests for invite as tappa_app.
func withdraw(t *testing.T, ctx context.Context, tx pgx.Tx, w appDeviceWorld, invite uuid.UUID) (int64, error) {
	t.Helper()
	var n int64
	err := w.arQ(t, ctx, tx, func(q *store.Queries) error {
		var e error
		n, e = q.WithdrawOpenActivationRequests(ctx, store.WithdrawOpenActivationRequestsParams{TenantID: w.tenant, InviteID: invite})
		return e
	})
	return n, err
}

// TestActivationRequests_Checks holds the row's shape (23514, each CHECK by name): a lifetime over an
// hour or of none; a decision that is not the whole triple, or not approved/rejected, or after the
// expiry; a withdrawn decided request; a collection of an open or a rejected request, or after the
// expiry; a pairing code that is not six digits; a binding that is not 64 lower-case hex; a client
// outside browser/app.
func TestActivationRequests_Checks(t *testing.T) {
	ctx, tx := opTx(t)
	arCheckShapes(t, ctx, tx)
}

// arCheckShapes is TestActivationRequests_Checks' body, on whatever activation_requests tx sees: the
// installed table there, and the one 00115's Up builds in the Down/Up round trip -- so the file's
// CHECKs are held to the same cases as the database's.
func arCheckShapes(t *testing.T, ctx context.Context, tx pgx.Tx) {
	t.Helper()
	w := newAppDeviceWorld(t, ctx, tx)
	admin := w.arAdmin(t, ctx, tx, arEmail())
	exec := func(sql string, args ...any) error { return w.arExec(t, ctx, tx, sql, args...) }
	invite, binding := w.arInvite(t, ctx, tx, w.employee)

	for _, ttl := range []int32{3601, 0} {
		_, err := w.arCreate(t, ctx, tx, invite, binding, ttl)
		opWantConstraint(t, err, "activation_requests_lifetime", fmt.Sprintf("a request living %d s", ttl))
	}
	if _, err := w.arCreate(t, ctx, tx, invite, binding, 3600); err != nil {
		t.Fatalf("POSITIVE CONTROL: a request living exactly the hour ceiling: %v", err)
	}
	if n, err := withdraw(t, ctx, tx, w, invite); err != nil || n != 1 {
		t.Fatalf("withdraw: %d (%v)", n, err)
	}

	raw := `INSERT INTO activation_requests (tenant_id, employee_id, invite_id, client, binding_hash, pairing_code, expires_at)
	        VALUES ($1, $2, $3, $4, $5, $6, now() + interval '10 minutes')`
	for _, c := range []struct{ what, client, binding, code, constraint string }{
		{"a client outside browser/app", "ios", binding, arPairingCode(t), "activation_requests_client_known"},
		{"an upper-case binding", "app", strings.ToUpper(binding), arPairingCode(t), "activation_requests_binding_hash_shape"},
		{"a short binding", "app", binding[:63], arPairingCode(t), "activation_requests_binding_hash_shape"},
		{"a five-digit code", "app", binding, "12345", "activation_requests_pairing_code_shape"},
		{"a seven-digit code", "app", binding, "1234567", "activation_requests_pairing_code_shape"},
		{"a code with a letter", "app", binding, "12345a", "activation_requests_pairing_code_shape"},
	} {
		opWantConstraint(t, exec(raw, w.tenant, w.employee, invite, c.client, c.binding, c.code), c.constraint, c.what)
	}

	req := w.arOpen(t, ctx, tx, invite, binding)
	for _, c := range []struct{ what, set, constraint string }{
		{"a decision without its stamp and decider", `decision = 'approved'`, "activation_requests_decision_whole"},
		{"a decider without a decision", `decided_by = $2`, "activation_requests_decision_whole"},
		{"a decision outside approved/rejected", `decision = 'maybe', decided_at = now(), decided_by = $2`, "activation_requests_decision_known"},
		{"a collection of an open request", `collected_at = now()`, "activation_requests_collected_approved"},
	} {
		args := []any{req}
		if strings.Contains(c.set, "$2") {
			args = append(args, admin)
		}
		opWantConstraint(t, exec(`UPDATE activation_requests SET `+c.set+` WHERE id = $1`, args...), c.constraint, "tappa_app: "+c.what)
	}
	// A decision after the expiry: the owner moves the expiry to a microsecond after the
	// transaction's start; a decision stamped by the wall clock is then past it.
	if _, err := tx.Exec(ctx, `UPDATE activation_requests SET expires_at = now() + interval '1 microsecond' WHERE id = $1`, req); err != nil {
		t.Fatal(err)
	}
	opWantConstraint(t, exec(`UPDATE activation_requests SET decision = 'approved', decided_at = clock_timestamp(), decided_by = $2 WHERE id = $1`, req, admin),
		"activation_requests_decided_while_open", "tappa_app: a decision after the expiry")
	if _, err := tx.Exec(ctx, `UPDATE activation_requests SET expires_at = now() + interval '10 minutes' WHERE id = $1`, req); err != nil {
		t.Fatal(err)
	}

	if _, err := w.arDecide(t, ctx, tx, req, admin, "approved"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	opWantConstraint(t, exec(`UPDATE activation_requests SET withdrawn_at = now() WHERE id = $1`, req),
		"activation_requests_withdrawn_undecided", "tappa_app: withdrawing a decided request")
	if _, err := tx.Exec(ctx, `UPDATE activation_requests SET expires_at = now() + interval '1 microsecond' WHERE id = $1`, req); err != nil {
		t.Fatal(err)
	}
	opWantConstraint(t, exec(`UPDATE activation_requests SET collected_at = clock_timestamp() WHERE id = $1`, req),
		"activation_requests_collected_approved", "tappa_app: a collection after the expiry")
	if _, err := tx.Exec(ctx, `UPDATE activation_requests SET expires_at = now() + interval '10 minutes' WHERE id = $1`, req); err != nil {
		t.Fatal(err)
	}

	inv2, bind2 := w.arInvite(t, ctx, tx, w.arEmployee(t, ctx, tx, nil, w.venue))
	rejected := w.arOpen(t, ctx, tx, inv2, bind2)
	if _, err := w.arDecide(t, ctx, tx, rejected, admin, "rejected"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	opWantConstraint(t, exec(`UPDATE activation_requests SET collected_at = now() WHERE id = $1`, rejected),
		"activation_requests_collected_approved", "tappa_app: a collection of a rejection")
	if _, err := w.arCollect(t, ctx, tx, invite, binding); err != nil {
		t.Fatalf("POSITIVE CONTROL: collecting the live approval: %v", err)
	}
}

// TestActivationRequests_CreateOnlyForTheCurrentConsent: CreateActivationRequest writes a row only
// for the invitation's CURRENT consent binding while the invitation is live -- another binding, an
// invitation nobody consented to, a used, a cancelled and an expired one each answer pgx.ErrNoRows and
// write nothing. The row it writes carries the code it was given, the database's clock and the TTL.
func TestActivationRequests_CreateOnlyForTheCurrentConsent(t *testing.T) {
	ctx, tx := opTx(t)
	w := newAppDeviceWorld(t, ctx, tx)
	invite, binding := w.arInvite(t, ctx, tx, w.employee)
	before := opInt(t, ctx, tx, `SELECT count(*) FROM activation_requests WHERE tenant_id = $1`, w.tenant)

	if _, err := w.arCreate(t, ctx, tx, invite, opRandHex(t), 600); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another binding: %v, want pgx.ErrNoRows", err)
	}
	if _, err := w.arCreate(t, ctx, tx, uuid.New(), binding, 600); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("an unknown invitation: %v, want pgx.ErrNoRows", err)
	}
	for _, c := range []struct{ what, sql string }{
		{"a used invitation", `UPDATE employee_invites SET used_at = now() WHERE id = $1`},
		{"a cancelled invitation", `UPDATE employee_invites SET cancelled_at = now() WHERE id = $1`},
		{"an expired invitation", `UPDATE employee_invites SET created_at = now() - interval '2 days', expires_at = now() - interval '1 second' WHERE id = $1`},
		{"an invitation nobody consented to", `UPDATE employee_invites SET consented_at = NULL, consent_binding_hash = NULL WHERE id = $1`},
	} {
		inv, bind := w.arInvite(t, ctx, tx, w.arEmployee(t, ctx, tx, nil, w.venue))
		if _, err := tx.Exec(ctx, c.sql, inv); err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		if _, err := w.arCreate(t, ctx, tx, inv, bind, 600); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("%s: %v, want pgx.ErrNoRows", c.what, err)
		}
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM activation_requests WHERE tenant_id = $1`, w.tenant); n != before {
		t.Fatalf("the refused opens wrote %d row(s)", n-before)
	}

	code := arPairingCode(t)
	var row store.CreateActivationRequestRow
	if err := w.arQ(t, ctx, tx, func(q *store.Queries) error {
		var e error
		row, e = q.CreateActivationRequest(ctx, store.CreateActivationRequestParams{
			Client: "app", BindingHash: binding, PairingCode: code, TtlSeconds: 300, TenantID: w.tenant, InviteID: invite,
		})
		return e
	}); err != nil {
		t.Fatalf("POSITIVE CONTROL: the current binding: %v", err)
	}
	if row.PairingCode != code || row.ExpiresAt.Sub(row.CreatedAt).Seconds() != 300 {
		t.Fatalf("the request carries another code or a lifetime of %v, want the given code and 300 s", row.ExpiresAt.Sub(row.CreatedAt))
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM activation_requests WHERE id = $1 AND tenant_id = $2 AND employee_id = $3 AND invite_id = $4
	                            AND client = 'app' AND binding_hash = $5 AND created_at = now()`, row.ID, w.tenant, w.employee, invite, binding); n != 1 {
		t.Fatal("the row does not carry the invitation's tenant, employee and binding, the client and the database's clock")
	}
	// The phone's read: its binding finds it; another binding does not.
	if err := w.arQ(t, ctx, tx, func(q *store.Queries) error {
		got, e := q.GetActivationRequestForBinding(ctx, store.GetActivationRequestForBindingParams{TenantID: w.tenant, InviteID: invite, BindingHash: binding})
		if e == nil && (got.ID != row.ID || got.PairingCode != code || got.Expired || got.Decision != nil) {
			e = fmt.Errorf("the phone's read answered another request or state (expired %v)", got.Expired)
		}
		return e
	}); err != nil {
		t.Fatalf("the phone's read: %v", err)
	}
	if err := w.arQ(t, ctx, tx, func(q *store.Queries) error {
		_, e := q.GetActivationRequestForBinding(ctx, store.GetActivationRequestForBindingParams{TenantID: w.tenant, InviteID: invite, BindingHash: opRandHex(t)})
		return e
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another binding's read: %v, want pgx.ErrNoRows", err)
	}
}

// TestActivationRequests_OneOpenPerInvitation: a second open request on the same invitation without
// withdrawing the first is refused by the partial UNIQUE (23505, activation_requests_one_open_key);
// WithdrawOpenActivationRequests withdraws exactly the open one (and touches neither another
// invitation's nor a decided one), after which the new request opens and is the one the phone reads.
func TestActivationRequests_OneOpenPerInvitation(t *testing.T) {
	ctx, tx := opTx(t)
	w := newAppDeviceWorld(t, ctx, tx)
	admin := w.arAdmin(t, ctx, tx, arEmail())
	invite, binding := w.arInvite(t, ctx, tx, w.employee)
	otherInvite, otherBinding := w.arInvite(t, ctx, tx, w.arEmployee(t, ctx, tx, nil, w.venue))
	first := w.arOpen(t, ctx, tx, invite, binding)
	otherOpen := w.arOpen(t, ctx, tx, otherInvite, otherBinding)

	_, err := w.arCreate(t, ctx, tx, invite, binding, 600)
	opWant(t, err, sqlstateUniqueViolation, "a second open request on one invitation")
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.ConstraintName != "activation_requests_one_open_key" {
		t.Fatalf("the second open request was refused by %v, want activation_requests_one_open_key", err)
	}
	if n, err := withdraw(t, ctx, tx, w, invite); err != nil || n != 1 {
		t.Fatalf("withdraw: %d (%v), want 1", n, err)
	}
	second := w.arOpen(t, ctx, tx, invite, binding)
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM activation_requests WHERE id = $1 AND withdrawn_at IS NOT NULL`, first); n != 1 {
		t.Fatal("the first request is not withdrawn")
	}
	// Inside one test transaction both requests carry the same now(); in the product they are opened
	// by two requests, so the first is older. The owner gives it that past (created_at is no state
	// column) so the phone's newest-first read has an order to follow.
	if _, err := tx.Exec(ctx, `UPDATE activation_requests SET created_at = now() - interval '1 minute' WHERE id = $1`, first); err != nil {
		t.Fatal(err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM activation_requests WHERE id = $1 AND withdrawn_at IS NULL`, otherOpen); n != 1 {
		t.Fatal("the withdrawal touched another invitation's request")
	}
	if err := w.arQ(t, ctx, tx, func(q *store.Queries) error {
		got, e := q.GetActivationRequestForBinding(ctx, store.GetActivationRequestForBindingParams{TenantID: w.tenant, InviteID: invite, BindingHash: binding})
		if e == nil && (got.ID != second || got.WithdrawnAt != nil) {
			e = errors.New("the phone's read answered the withdrawn request")
		}
		return e
	}); err != nil {
		t.Fatalf("the phone's read after a re-open: %v", err)
	}
	// A decided request does not block a new one, and the withdrawal leaves it alone.
	if _, err := w.arDecide(t, ctx, tx, second, admin, "rejected"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if n, err := withdraw(t, ctx, tx, w, invite); err != nil || n != 0 {
		t.Fatalf("withdrawing with only a decided request: %d (%v), want 0", n, err)
	}
	w.arOpen(t, ctx, tx, invite, binding)
	if n, err := withdraw(t, ctx, tx, w, invite); err != nil || n != 1 {
		t.Fatalf("withdraw the third: %d (%v), want 1", n, err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM activation_requests WHERE id = $1 AND decision = 'rejected' AND withdrawn_at IS NULL`, second); n != 1 {
		t.Fatal("the withdrawal touched the decided request")
	}
}

// TestActivationRequests_CollectOnlyAnUnexpiredApproval: CollectActivationApproval stamps only the
// approved, uncollected, unexpired request of this invitation and binding -- an open request, a
// rejection, another binding's and an expired approval each answer pgx.ErrNoRows; the approval is
// collected once (a second collection: pgx.ErrNoRows) and hands back its decider.
func TestActivationRequests_CollectOnlyAnUnexpiredApproval(t *testing.T) {
	ctx, tx := opTx(t)
	w := newAppDeviceWorld(t, ctx, tx)
	admin := w.arAdmin(t, ctx, tx, arEmail())

	invite, binding := w.arInvite(t, ctx, tx, w.employee)
	req := w.arOpen(t, ctx, tx, invite, binding)
	if _, err := w.arCollect(t, ctx, tx, invite, binding); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("collecting an open request: %v, want pgx.ErrNoRows", err)
	}
	if _, err := w.arDecide(t, ctx, tx, req, admin, "approved"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if _, err := w.arCollect(t, ctx, tx, invite, opRandHex(t)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("another binding collecting the approval: %v, want pgx.ErrNoRows", err)
	}
	row, err := w.arCollect(t, ctx, tx, invite, binding)
	if err != nil {
		t.Fatalf("POSITIVE CONTROL: collecting the approval: %v", err)
	}
	if row.ID != req || row.DecidedBy == nil || *row.DecidedBy != admin {
		t.Fatalf("the collection answered %+v, want the request and its decider", row)
	}
	if _, err := w.arCollect(t, ctx, tx, invite, binding); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a second collection: %v, want pgx.ErrNoRows", err)
	}

	inv2, bind2 := w.arInvite(t, ctx, tx, w.arEmployee(t, ctx, tx, nil, w.venue))
	rej := w.arOpen(t, ctx, tx, inv2, bind2)
	if _, err := w.arDecide(t, ctx, tx, rej, admin, "rejected"); err != nil {
		t.Fatalf("reject: %v", err)
	}
	if _, err := w.arCollect(t, ctx, tx, inv2, bind2); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("collecting a rejection: %v, want pgx.ErrNoRows", err)
	}

	// An approval that ran out: decided in the past, expired before now -- a history the one-way
	// trigger cannot be walked through inside one transaction, so the owner writes it with the
	// triggers off (every CHECK still holds it).
	inv3, bind3 := w.arInvite(t, ctx, tx, w.arEmployee(t, ctx, tx, nil, w.venue))
	var emp3 uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT employee_id FROM employee_invites WHERE id = $1`, inv3).Scan(&emp3); err != nil {
		t.Fatal(err)
	}
	arForge(t, ctx, tx, `INSERT INTO activation_requests (tenant_id, employee_id, invite_id, client, binding_hash, pairing_code,
	                         created_at, expires_at, decision, decided_at, decided_by)
	                     VALUES ($1, $2, $3, 'browser', $4, $5, now() - interval '2 hours', now() - interval '90 minutes',
	                             'approved', now() - interval '100 minutes', $6)`, w.tenant, emp3, inv3, bind3, arPairingCode(t), admin)
	if _, err := w.arCollect(t, ctx, tx, inv3, bind3); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("collecting an expired approval: %v, want pgx.ErrNoRows", err)
	}
	if err := w.arQ(t, ctx, tx, func(q *store.Queries) error {
		got, e := q.GetActivationRequestForBinding(ctx, store.GetActivationRequestForBindingParams{TenantID: w.tenant, InviteID: inv3, BindingHash: bind3})
		if e == nil && !got.Expired {
			e = errors.New("the phone's read of an expired approval says it is not expired")
		}
		return e
	}); err != nil {
		t.Fatalf("the phone's read of an expired approval: %v", err)
	}
}

// TestActivationRequests_DecideOnceByItsOwnBusiness: DecideActivationRequest decides an open request
// once (a second decision: pgx.ErrNoRows, and the first stands); it does not decide a withdrawn or an
// expired request (pgx.ErrNoRows); a decider from another business is refused by the composite key
// (23503), and the request stays open.
func TestActivationRequests_DecideOnceByItsOwnBusiness(t *testing.T) {
	ctx, tx := opTx(t)
	w, other := newAppDeviceWorld(t, ctx, tx), newAppDeviceWorld(t, ctx, tx)
	admin, foreign := w.arAdmin(t, ctx, tx, arEmail()), other.arAdmin(t, ctx, tx, arEmail())

	invite, binding := w.arInvite(t, ctx, tx, w.employee)
	req := w.arOpen(t, ctx, tx, invite, binding)
	_, err := w.arDecide(t, ctx, tx, req, foreign, "approved")
	opWant(t, err, sqlstateForeignKeyViolation, "a manager of another business deciding")
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.ConstraintName != "activation_requests_decided_by_fk" {
		t.Fatalf("the foreign decider was refused by %v, want activation_requests_decided_by_fk", err)
	}
	row, err := w.arDecide(t, ctx, tx, req, admin, "approved")
	if err != nil || row.ID != req || row.EmployeeID != w.employee || row.Decision == nil || *row.Decision != "approved" || row.DecidedAt == nil {
		t.Fatalf("POSITIVE CONTROL: approving: %+v (%v)", row, err)
	}
	if _, err := w.arDecide(t, ctx, tx, req, admin, "rejected"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("a second decision: %v, want pgx.ErrNoRows", err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM activation_requests WHERE id = $1 AND decision = 'approved' AND decided_by = $2`, req, admin); n != 1 {
		t.Fatal("the second decision moved the first")
	}

	inv2, bind2 := w.arInvite(t, ctx, tx, w.arEmployee(t, ctx, tx, nil, w.venue))
	withdrawn := w.arOpen(t, ctx, tx, inv2, bind2)
	if n, err := withdraw(t, ctx, tx, w, inv2); err != nil || n != 1 {
		t.Fatalf("withdraw: %d (%v)", n, err)
	}
	if _, err := w.arDecide(t, ctx, tx, withdrawn, admin, "approved"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deciding a withdrawn request: %v, want pgx.ErrNoRows", err)
	}

	inv3, bind3 := w.arInvite(t, ctx, tx, w.arEmployee(t, ctx, tx, nil, w.venue))
	expired := w.arOpen(t, ctx, tx, inv3, bind3)
	if _, err := tx.Exec(ctx, `UPDATE activation_requests SET created_at = now() - interval '2 hours', expires_at = now() - interval '90 minutes' WHERE id = $1`, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := w.arDecide(t, ctx, tx, expired, admin, "approved"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deciding an expired request: %v, want pgx.ErrNoRows", err)
	}
	if _, err := w.arDecide(t, ctx, tx, uuid.New(), admin, "approved"); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("deciding an unknown request: %v, want pgx.ErrNoRows", err)
	}
}

// TestActivationRequests_LockForDecisionSeesSelfApproval: LockActivationRequestForDecision's self is
// true when the deciding account's address is the person's -- compared without case (both citext)
// -- and false for another address, for a person with no address, and for an account of another
// business carrying the same address (its row is not joined). invite_usable and expired follow the
// invitation and the request; an unknown request is pgx.ErrNoRows.
func TestActivationRequests_LockForDecisionSeesSelfApproval(t *testing.T) {
	ctx, tx := opTx(t)
	w, other := newAppDeviceWorld(t, ctx, tx), newAppDeviceWorld(t, ctx, tx)
	local := "m1623-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	person := w.arEmployee(t, ctx, tx, "Self."+local+"@Example.TEST", w.venue)
	selfAdmin := w.arAdmin(t, ctx, tx, "self."+strings.ToUpper(local)+"@example.test")
	otherAdmin := w.arAdmin(t, ctx, tx, arEmail())
	foreignTwin := other.arAdmin(t, ctx, tx, "self."+local+"@example.test")
	noAddress := w.arEmployee(t, ctx, tx, nil, w.venue)

	invite, binding := w.arInvite(t, ctx, tx, person)
	req := w.arOpen(t, ctx, tx, invite, binding)
	inv2, bind2 := w.arInvite(t, ctx, tx, noAddress)
	reqNoAddress := w.arOpen(t, ctx, tx, inv2, bind2)

	lock := func(id, admin uuid.UUID) (store.LockActivationRequestForDecisionRow, error) {
		t.Helper()
		var row store.LockActivationRequestForDecisionRow
		err := w.arQ(t, ctx, tx, func(q *store.Queries) error {
			var e error
			row, e = q.LockActivationRequestForDecision(ctx, store.LockActivationRequestForDecisionParams{AdminUserID: admin, TenantID: w.tenant, ID: id})
			return e
		})
		return row, err
	}
	for _, c := range []struct {
		what      string
		id, admin uuid.UUID
		self      bool
	}{
		{"the person's own address, in another case", req, selfAdmin, true},
		{"another address", req, otherAdmin, false},
		{"another business's account with the same address", req, foreignTwin, false},
		{"an unknown account", req, uuid.New(), false},
		{"a person with no address", reqNoAddress, selfAdmin, false},
	} {
		row, err := lock(c.id, c.admin)
		if err != nil {
			t.Fatalf("%s: %v", c.what, err)
		}
		if row.Self != c.self {
			t.Errorf("%s: self = %v, want %v", c.what, row.Self, c.self)
		}
		if row.ID != c.id || !row.InviteUsable || row.Expired || row.Decision != nil || row.WithdrawnAt != nil || row.Client != "browser" {
			t.Errorf("%s: the lock answered %+v, want the open, live request with a usable invitation", c.what, row)
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE employee_invites SET consent_binding_hash = $2 WHERE id = $1`, invite, opRandHex(t)); err != nil {
		t.Fatal(err)
	}
	if row, err := lock(req, selfAdmin); err != nil || row.InviteUsable {
		t.Fatalf("after the consent moved: invite_usable %v (%v), want false", row.InviteUsable, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE activation_requests SET created_at = now() - interval '2 hours', expires_at = now() - interval '90 minutes' WHERE id = $1`, reqNoAddress); err != nil {
		t.Fatal(err)
	}
	if row, err := lock(reqNoAddress, selfAdmin); err != nil || !row.Expired {
		t.Fatalf("an expired request: expired %v (%v), want true", row.Expired, err)
	}
	if _, err := lock(uuid.New(), selfAdmin); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("an unknown request: %v, want pgx.ErrNoRows", err)
	}
}

// TestActivationRequests_ListShowsOnlyWhatAnApprovalCouldComplete: ListOpenActivationRequests shows
// the business's open, unexpired requests whose invitation the asking binding can still spend --
// and hides a withdrawn, a decided and an expired request, one whose invitation was used, one whose
// consent moved to another binding, one whose person was deactivated, and another business's. A
// person with no home venue is listed with no venue name (K-14).
func TestActivationRequests_ListShowsOnlyWhatAnApprovalCouldComplete(t *testing.T) {
	ctx, tx := opTx(t)
	w, other := newAppDeviceWorld(t, ctx, tx), newAppDeviceWorld(t, ctx, tx)
	admin := w.arAdmin(t, ctx, tx, arEmail())
	open := func(location any) (uuid.UUID, uuid.UUID, uuid.UUID) {
		t.Helper()
		emp := w.arEmployee(t, ctx, tx, nil, location)
		inv, bind := w.arInvite(t, ctx, tx, emp)
		return w.arOpen(t, ctx, tx, inv, bind), emp, inv
	}
	shown, shownEmp, _ := open(w.venue)
	homeless, _, _ := open(nil)

	withdrawn, _, wInv := open(w.venue)
	if n, err := withdraw(t, ctx, tx, w, wInv); err != nil || n != 1 {
		t.Fatalf("withdraw: %d (%v)", n, err)
	}
	decided, _, _ := open(w.venue)
	if _, err := w.arDecide(t, ctx, tx, decided, admin, "approved"); err != nil {
		t.Fatal(err)
	}
	expired, _, _ := open(w.venue)
	used, _, usedInv := open(w.venue)
	moved, _, movedInv := open(w.venue)
	gone, goneEmp, _ := open(w.venue)
	for _, s := range []struct {
		sql string
		arg uuid.UUID
	}{
		{`UPDATE activation_requests SET created_at = now() - interval '2 hours', expires_at = now() - interval '90 minutes' WHERE id = $1`, expired},
		{`UPDATE employee_invites SET used_at = now() WHERE id = $1`, usedInv},
		{`UPDATE employee_invites SET consent_binding_hash = '` + opRandHex(t) + `' WHERE id = $1`, movedInv},
		{`UPDATE employees SET status = 'deactivated', deactivated_at = now() WHERE id = $1`, goneEmp},
	} {
		if _, err := tx.Exec(ctx, s.sql, s.arg); err != nil {
			t.Fatalf("%s: %v", s.sql, err)
		}
	}
	oInv, oBind := other.arInvite(t, ctx, tx, other.employee)
	foreign := other.arOpen(t, ctx, tx, oInv, oBind)

	var list []store.ListOpenActivationRequestsRow
	if err := w.arQ(t, ctx, tx, func(q *store.Queries) error {
		var e error
		list, e = q.ListOpenActivationRequests(ctx, w.tenant)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	var got []uuid.UUID
	for _, r := range list {
		got = append(got, r.ID)
		switch r.ID {
		case shown:
			if r.EmployeeID != shownEmp || r.LocationName == nil || *r.LocationName != "m16-03 venue" || r.Client != "browser" || len(r.PairingCode) != 6 ||
				!strings.HasPrefix(r.FullName, "Person ") {
				t.Errorf("the open request is listed as %+v", r)
			}
		case homeless:
			if r.LocationName != nil {
				t.Errorf("a person with no home venue is listed at %q", *r.LocationName)
			}
		}
	}
	want := []uuid.UUID{shown, homeless}
	slices.SortFunc(got, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	slices.SortFunc(want, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	if !slices.Equal(got, want) {
		named := map[uuid.UUID]string{shown: "open", homeless: "no home venue", withdrawn: "withdrawn", decided: "decided", expired: "expired",
			used: "invitation used", moved: "consent moved", gone: "deactivated", foreign: "another business"}
		var names []string
		for _, id := range got {
			names = append(names, named[id])
		}
		t.Fatalf("the list shows %v, want exactly the open request and the one with no home venue", names)
	}
}

// TestActivationRequests_CountLiveQRScreensForApproval holds the gate's count: a screen RUNS when it
// is paired, not revoked, its session younger than session_lifetime_seconds, seen within
// seen_within_seconds, at an active venue. Counted: the fresh screen at the venue (and, with no venue
// given, the fresh screen at another venue of the business). Not counted: a waiting screen, a revoked
// one, a paired one never seen, one seen too long ago, one whose session ran out, one at an inactive
// venue, another business's.
func TestActivationRequests_CountLiveQRScreensForApproval(t *testing.T) {
	ctx, tx := opTx(t)
	w, other := newAppDeviceWorld(t, ctx, tx), newAppDeviceWorld(t, ctx, tx)
	second, closed := uuid.New(), uuid.New()
	for _, id := range []uuid.UUID{second, closed} {
		if _, err := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'm16-23 venue')`, id, w.tenant); err != nil {
			t.Fatal(err)
		}
	}
	at := func(world appDeviceWorld, venue uuid.UUID) appDeviceWorld { world.venue = venue; return world }
	running := func(world appDeviceWorld) uuid.UUID {
		t.Helper()
		id, code := world.addScreen(t, ctx, tx)
		if _, err := world.pairScreen(t, ctx, tx, id, code); err != nil {
			t.Fatalf("pair: %v", err)
		}
		var n int64
		if err := world.asApp(t, ctx, tx, world.tenant, func(sp pgx.Tx) error {
			var e error
			n, e = store.New(sp).TouchLiveQRDisplay(ctx, store.TouchLiveQRDisplayParams{TenantID: world.tenant, ID: id})
			return e
		}); err != nil || n != 1 {
			t.Fatalf("touch: %d (%v)", n, err)
		}
		return id
	}
	running(w)              // counted
	running(at(w, second))  // counted with no venue given
	running(at(w, closed))  // its venue is closed below
	running(other)          // another business's
	w.addScreen(t, ctx, tx) // waiting
	revoked := running(w)   // revoked below
	neverSeen, code := w.addScreen(t, ctx, tx)
	if _, err := w.pairScreen(t, ctx, tx, neverSeen, code); err != nil {
		t.Fatal(err)
	}
	stale, sessionOver := running(w), running(w)
	if err := w.asApp(t, ctx, tx, w.tenant, func(sp pgx.Tx) error {
		_, e := store.New(sp).RevokeLiveQRDisplay(ctx, store.RevokeLiveQRDisplayParams{TenantID: w.tenant, ID: revoked})
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE locations SET active = false WHERE id = $1`, closed); err != nil {
		t.Fatal(err)
	}
	// Paired 29 minutes ago and seen 25 minutes ago (its session alive, its poll stale); paired almost
	// three hours ago and seen now (its session over). The pairing history is past what the screen
	// trigger lets anyone write inside one transaction: the owner, triggers off.
	arForge(t, ctx, tx, `UPDATE live_qr_displays SET created_at = now() - interval '30 minutes', pairing_expires_at = now() - interval '20 minutes',
	                         paired_at = now() - interval '29 minutes', last_seen_at = now() - interval '25 minutes' WHERE id = $1`, stale)
	arForge(t, ctx, tx, `UPDATE live_qr_displays SET created_at = now() - interval '3 hours', pairing_expires_at = now() - interval '150 minutes',
	                         paired_at = now() - interval '170 minutes', last_seen_at = now() WHERE id = $1`, sessionOver)

	count := func(location *uuid.UUID, lifetime, seen int32) int32 {
		t.Helper()
		var n int32
		if err := w.arQ(t, ctx, tx, func(q *store.Queries) error {
			var e error
			n, e = q.CountLiveQRScreensForApproval(ctx, store.CountLiveQRScreensForApprovalParams{
				TenantID: w.tenant, LocationID: location, SessionLifetimeSeconds: lifetime, SeenWithinSeconds: seen,
			})
			return e
		}); err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	venue := w.venue
	if n := count(&venue, 3600, 600); n != 1 {
		t.Errorf("the venue counts %d running screens, want 1 (waiting, revoked, never seen, stale and session-over excluded)", n)
	}
	if n := count(&second, 3600, 600); n != 1 {
		t.Errorf("the second venue counts %d, want 1", n)
	}
	if n := count(&closed, 3600, 600); n != 0 {
		t.Errorf("an inactive venue counts %d, want 0", n)
	}
	if n := count(nil, 3600, 600); n != 2 {
		t.Errorf("the whole business counts %d, want 2 (the venue's and the second venue's)", n)
	}
	// CONTROLS: each excluded paired screen is excluded by its own criterion alone -- the stale one
	// joins with a longer seen window, the session-over one with a longer session lifetime.
	if n := count(&venue, 3600, 3600); n != 2 {
		t.Errorf("with an hour's seen window the venue counts %d, want 2 (the stale screen joins)", n)
	}
	if n := count(&venue, 4*3600, 600); n != 2 {
		t.Errorf("with a four-hour session lifetime the venue counts %d, want 2 (the session-over screen joins)", n)
	}
}

// arHoldTablesLock takes the operator-tables test lock EXCLUSIVE on a dedicated owner connection for
// the rest of the test (opTx's lock, without its transaction): the committed fixture below writes an
// admin_users row, and that write happens under the holder.
func arHoldTablesLock(t *testing.T) {
	t.Helper()
	lockCtx, cancel := context.WithTimeout(context.Background(), opLockWait)
	defer cancel()
	conn, err := pgx.Connect(lockCtx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Logf("close the lock connection: %v", err)
		}
	})
	if _, err := conn.Exec(lockCtx, `SELECT pg_advisory_lock(hashtext($1))`, operatorTablesTestLock); err != nil {
		t.Fatalf("operator-tables test lock: %v", err)
	}
}

// TestActivationRequests_ConcurrentCollectionExactlyOnce is CLAUDE.md §8's replay test for the
// approval: N goroutines, each in its own WithTenant transaction on its own pooled connection,
// collect the SAME approval at once -- exactly one RETURNING row, every other one pgx.ErrNoRows, and
// the row is collected once. Run with -race. The rows are committed (they must be visible to other
// connections) and stay inert, the rls_test.go fixture convention; activationRequestsSetAsideSQL
// takes them out of a walk's way.
func TestActivationRequests_ConcurrentCollectionExactlyOnce(t *testing.T) {
	arHoldTablesLock(t)
	d := appDB(t)
	tenantID, venue := m11Tenant(t, d)
	employee, invite, admin := uuid.New(), uuid.New(), uuid.New()
	binding := opRandHex(t)
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'M16-23 Race', 'active')`,
			[]any{employee, tenantID, venue}},
		{`INSERT INTO employee_invites (id, tenant_id, employee_id, code_hash, expires_at, consented_at, consent_binding_hash)
		  VALUES ($1, $2, $3, $4, now() + interval '1 hour', now(), $5)`, []any{invite, tenantID, employee, opRandHex(t), binding}},
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		  VALUES ($1, $2, 'M16-23 Race Manager', $3, $4, 'manager', 'active')`, []any{admin, tenantID, arEmail(), fixtures.UnusablePasswordHash}},
	} {
		if err := m11Exec(t, d, tenantID, s.sql, s.args...); err != nil {
			t.Fatalf("fixture: %v", err)
		}
	}
	var req uuid.UUID
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		row, e := q.CreateActivationRequest(ctx, store.CreateActivationRequestParams{
			Client: "app", BindingHash: binding, PairingCode: arPairingCode(t), TtlSeconds: 600, TenantID: tenantID, InviteID: invite,
		})
		if e != nil {
			return e
		}
		req = row.ID
		_, e = q.DecideActivationRequest(ctx, store.DecideActivationRequestParams{Decision: "approved", DecidedBy: &admin, TenantID: tenantID, ID: req})
		return e
	}); err != nil {
		t.Fatalf("fixture: open and approve: %v", err)
	}

	const n = 32
	var (
		wg            sync.WaitGroup
		mu            sync.Mutex
		wins, refused int
		others        []error
		start         = make(chan struct{})
	)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
				_, e := store.New(tx).CollectActivationApproval(ctx, store.CollectActivationApprovalParams{TenantID: tenantID, InviteID: invite, BindingHash: binding})
				return e
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				wins++
			case errors.Is(err, pgx.ErrNoRows):
				refused++
			default:
				others = append(others, err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if len(others) > 0 {
		t.Fatalf("%d collection(s) failed for another reason, first: %v", len(others), others[0])
	}
	if wins != 1 || refused != n-1 {
		t.Fatalf("%d goroutines collected one approval: %d succeeded, %d were refused; want exactly 1 and %d", n, wins, refused, n-1)
	}
	var collected int64
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM activation_requests WHERE tenant_id = $1 AND id = $2 AND collected_at IS NOT NULL`,
			tenantID, req).Scan(&collected)
	}); err != nil || collected != 1 {
		t.Fatalf("the request after the race: %d collected rows (%v), want 1", collected, err)
	}
	t.Logf("%d concurrent collections of one approval: %d succeeded, %d refused", n, wins, refused)
}

// TestActivationRequests00115_DownRefusesWithRowsAndRoundTrips: the Down refuses while the table
// holds a row (55000) and changes nothing; with it empty (the rows set aside inside the transaction)
// it takes the table and its function away, taking no strong lock outside the five tables its LOCK
// TABLE names; the Up builds the same objects and grants again, under the same five.
func TestActivationRequests00115_DownRefusesWithRowsAndRoundTrips(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, op00115File)
	tables := downTables(down)
	slices.Sort(tables)
	if tables = slices.Compact(tables); !slices.Equal(tables, arDownTables) {
		t.Fatalf("PREMISE: downTables reads %v in 00115's Down, want %v", tables, arDownTables)
	}
	lockForDowns(t, ctx, tx, arDownTables)
	objects := `SELECT (SELECT count(*) FROM pg_class WHERE relname = 'activation_requests' AND relnamespace = 'public'::regnamespace)
	                 + (SELECT count(*) FROM pg_proc WHERE proname = 'activation_requests_state_is_one_way')`
	if n := opInt(t, ctx, tx, objects); n != 2 {
		t.Fatalf("%d of 00115's two objects at HEAD", n)
	}
	w := newAppDeviceWorld(t, ctx, tx)
	invite, binding := w.arInvite(t, ctx, tx, w.employee)
	w.arOpen(t, ctx, tx, invite, binding)
	err := holdingTryDown(t, ctx, tx, down)
	opWant(t, err, sqlstatePrerequisiteState, "00115's Down while a request exists")
	if _, msg := opCode(err); !strings.Contains(msg, "migration 00115 Down") {
		t.Fatalf("the refusal does not name 00115's Down: %q", msg)
	}
	if n := opInt(t, ctx, tx, objects); n != 2 {
		t.Fatalf("the refused Down changed something: %d objects", n)
	}
	if _, err := tx.Exec(ctx, activationRequestsSetAsideSQL); err != nil {
		t.Fatalf("set the rows aside inside the transaction: %v", err)
	}
	opRunSection(t, ctx, tx, down, "00115 Down")
	if n := opInt(t, ctx, tx, objects); n != 0 {
		t.Fatalf("%d of 00115's objects survive the Down", n)
	}
	if extra := strongTableLocksOutside(t, ctx, tx, arDownTables); len(extra) > 0 {
		t.Errorf("00115's Down took a strong lock on %v, outside the five it names", extra)
	}
	opRunSection(t, ctx, tx, up, "00115 Up again")
	if n := opInt(t, ctx, tx, objects); n != 2 {
		t.Fatalf("%d of 00115's objects after the Up, want 2", n)
	}
	if extra := strongTableLocksOutside(t, ctx, tx, arDownTables); len(extra) > 0 {
		t.Errorf("00115's Up took a strong lock on %v, outside the five", extra)
	}
	for _, c := range []struct{ priv, want string }{
		{"SELECT", "id,tenant_id,employee_id,invite_id,client,binding_hash,pairing_code,created_at,expires_at,decision,decided_at,decided_by,withdrawn_at,collected_at"},
		{"INSERT", "tenant_id,employee_id,invite_id,client,binding_hash,pairing_code,expires_at"},
		{"UPDATE", "decision,decided_at,decided_by,withdrawn_at,collected_at"},
	} {
		if got := opColumns(t, ctx, tx, "tappa_app", "activation_requests", c.priv); got != c.want {
			t.Errorf("after the Up tappa_app %s on activation_requests: %q, want %q", c.priv, got, c.want)
		}
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class c WHERE c.oid = 'public.activation_requests'::regclass
	                            AND c.relrowsecurity AND c.relforcerowsecurity`); n != 1 {
		t.Error("after the Up the table is not ENABLE + FORCE")
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_trigger WHERE tgrelid = 'public.activation_requests'::regclass AND NOT tgisinternal`); n != 2 {
		t.Errorf("after the Up the table carries %d triggers, want 2", n)
	}
	// The file's own CHECKs, as the Up just built them, held to the installed table's cases.
	arCheckShapes(t, ctx, tx)
}
