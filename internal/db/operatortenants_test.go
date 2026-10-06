package db

// operatortenants_test.go -- migration 00029 (M10 OP-11, phase A): the operator reads the
// tenant list (with its search) and one tenant's overview through op_read_tenants and
// op_read_tenant_detail, the first op_* that read TENANT data. The catalogue half
// (signatures, grants, the Down, the precondition, the temp-table shadow) and the
// behaviour half (op_begin_read's two new kinds, the two reads, the Go accessors) of
// ADR 0021 §6's list, again for the two new op_read_* (m10-platform.md, OP-4 block,
// item "OP-11").
//
// HOW EACH TEST TOUCHES THE SHARED DATABASE -- operatorlegal_test.go's three shapes:
//   - most tests run in opTx: one rolled-back REPEATABLE READ transaction as the owner,
//     identities switched with SET LOCAL SESSION AUTHORIZATION, the operator-tables lock
//     taken EXCLUSIVE. Every tenant, location, employee, plaque and admin they need is
//     written inside that transaction and goes with it;
//   - the read side is reached WITHOUT a commit through a ticket the owner writes with
//     created_xact naming a transaction that really committed (opCommittedXact) and a
//     hash computed HERE, in Go, from the canonical text the functions hash
//     (opTenantsTicketHash, opDetailTicketHash) -- so a change in what the functions
//     hash turns a test red instead of following along;
//   - two tests need a COMMIT because a commit is their subject
//     (TestOpReadTenants_TwoPhaseLifecycle, TestTenantList_OnThePoolTheTwoPhasesAreTwoTransactions).
//     They take the lock SHARED through opLiveFixture, write their tenant fixtures only
//     inside transactions that are rolled back, and leave, per run, by construction:
//     one disabled account each, its revoked sessions, and the 'read' rows they
//     committed (the lifecycle two, the pool test five -- three through the accessors,
//     and since OP-11 phase B two more through *OperatorDB's methods) -- operator_audit_log is
//     append-only and its foreign keys keep the account and the sessions those rows
//     name. No tenant row is committed.
//
// THE DEVELOPMENT DATABASE HOLDS HUNDREDS OF THOUSANDS OF TENANTS (the suite's own
// residue: 586 538 on 2026-10-03). Every fixture tenant here therefore carries a random
// token in its name and a created_at a day in the FUTURE, so a search for the token
// finds exactly the fixtures and the newest-first list puts them on page one whatever
// else the database holds.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The two functions 00029 creates and the one it replaces, by their exact catalogue
// identity. op_begin_read's identity is 00027's: the replacement keeps it.
var op00029Functions = map[string]struct{ args, result string }{
	"op_begin_read":         {"p_session text, p_kind text, p_params jsonb", "text"},
	"op_read_tenants":       {"p_session text, p_ticket text, p_query text, p_page_number integer, p_page_size integer", "TABLE(tenant_id uuid, tenant_name text, created_at timestamp with time zone, plan text)"},
	"op_read_tenant_detail": {"p_session text, p_ticket text, p_tenant_id uuid", "TABLE(tenant_id uuid, tenant_name text, created_at timestamp with time zone, plan text, business_type text, location_count bigint, active_employee_count bigint, active_plaque_count bigint, active_admin_count bigint)"},
}

const (
	tenantsRefusal      = "op_read_tenants: read refused"
	tenantDetailRefusal = "op_read_tenant_detail: read refused"
	op00029File         = "00029_read_tenants_from_the_operator.sql"
)

// ------------------------------------------------------------------ helpers --

// opToken is a random lower-case word that no tenant name in the database carries.
func opToken(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return "op11tok" + hex.EncodeToString(b)
}

// opPGJSONString is PostgreSQL's escape_json for a string, as jsonb's text output writes
// it: the six short escapes, \u00XX for the other control characters, everything else
// verbatim (non-ASCII included). TestOpBeginRead_TheTenantKindsBindEveryParameterAndAuditNoTerm
// compares it with the database's own output for the awkward characters, so a hash
// written with it is the hash the functions compute.
func opPGJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// opTenantsCanonical is jsonb's text of the list's parameter object: keys shorter-first
// (query, page_size, page_number).
func opTenantsCanonical(query string, number, size int) string {
	return fmt.Sprintf(`{"query": %s, "page_size": %d, "page_number": %d}`, opPGJSONString(query), size, number)
}

func opTenantsTicketHash(raw, query string, number, size int) string {
	sum := sha256.Sum256([]byte(raw + opTenantsCanonical(query, number, size)))
	return hex.EncodeToString(sum[:])
}

// opDetailCanonical is jsonb's text of the overview's parameter object: the uuid in its
// canonical lower-case text.
func opDetailCanonical(id uuid.UUID) string {
	return fmt.Sprintf(`{"tenant_id": "%s"}`, id.String())
}

func opDetailTicketHash(raw string, id uuid.UUID) string {
	sum := sha256.Sum256([]byte(raw + opDetailCanonical(id)))
	return hex.EncodeToString(sum[:])
}

// opTenantsParams is the list's parameter object as JSON text (Go's spelling; the
// database rebuilds the hashed text itself).
func opTenantsParams(query string, number, size int) string {
	b, _ := json.Marshal(map[string]any{"page_number": number, "page_size": size, "query": query})
	return string(b)
}

func opDetailParams(id string) string {
	b, _ := json.Marshal(map[string]any{"tenant_id": id})
	return string(b)
}

// opForgeRead writes, as the owner, a ticket the read side treats as one an EARLIER,
// COMMITTED op_begin_read made: its audit row ('read', scope = kind, the tenant for a
// detail), and the ticket with the given hash, kind and a created_xact naming a
// committed transaction. It returns the ticket's id.
func opForgeRead(t *testing.T, ctx context.Context, tx pgx.Tx, session, admin uuid.UUID, kind, hash string, tenant *uuid.UUID, xact, ttl string) uuid.UUID {
	t.Helper()
	var auditID, ticketID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_scope, target_tenant_id)
	                            VALUES ('read', $1, $2, $3, $4) RETURNING id`, session, admin, kind, tenant).Scan(&auditID); err != nil {
		t.Fatalf("forge the ticket's audit row: %v", err)
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO public.operator_read_tickets (ticket_hash, session_id, kind, target_tenant_id, audit_id, expires_at, created_xact)
		VALUES ($1, $2, $3, $4, $5, clock_timestamp() + $6::interval, $7::xid8)
		RETURNING id`, hash, session, kind, tenant, auditID, ttl, xact).Scan(&ticketID); err != nil {
		t.Fatalf("forge the ticket: %v", err)
	}
	return ticketID
}

// opForgeTenants forges a list ticket bound to (query, page) and returns the raw ticket.
func opForgeTenants(t *testing.T, ctx context.Context, tx pgx.Tx, session, admin uuid.UUID, query string, number, size int, xact string) string {
	t.Helper()
	raw := opRandHex(t)
	opForgeRead(t, ctx, tx, session, admin, tenantsReadKind, opTenantsTicketHash(raw, query, number, size), nil, xact, "30 seconds")
	return raw
}

// opForgeDetail forges an overview ticket bound to the tenant id and returns the raw ticket.
func opForgeDetail(t *testing.T, ctx context.Context, tx pgx.Tx, session, admin, tenant uuid.UUID, xact string) string {
	t.Helper()
	raw := opRandHex(t)
	opForgeRead(t, ctx, tx, session, admin, tenantDetailReadKind, opDetailTicketHash(raw, tenant), &tenant, xact, "30 seconds")
	return raw
}

// opScanTenants runs the shipped list statement and scans it, the database's error
// untouched (operatorErr would turn 28000 into a sentinel; these tests want the SQLSTATE).
func opScanTenants(ctx context.Context, q opQuerier, hash, ticket, query, number, size any) ([]TenantSummary, error) {
	rows, err := q.Query(ctx, readTenantsSQL, hash, ticket, query, number, size)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TenantSummary
	for rows.Next() {
		var s TenantSummary
		if err := rows.Scan(&s.ID, &s.Name, &s.CreatedAt, &s.Plan); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// opScanDetail runs the shipped overview statement and scans every row it returns.
func opScanDetail(ctx context.Context, q opQuerier, hash, ticket, tenant any) ([]TenantOverview, error) {
	rows, err := q.Query(ctx, readTenantDetailSQL, hash, ticket, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TenantOverview
	for rows.Next() {
		var o TenantOverview
		if err := rows.Scan(&o.ID, &o.Name, &o.CreatedAt, &o.Plan, &o.BusinessType,
			&o.Locations, &o.ActiveEmployees, &o.ActivePlaques, &o.ActiveAdmins); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// opReadTenants / opReadDetail run the two reads as tappa_operator inside a savepoint
// of tx; on success the savepoint is released, so the consumption stays in tx.
func opReadTenants(t *testing.T, ctx context.Context, tx pgx.Tx, hash, ticket, query string, number, size int) ([]TenantSummary, error) {
	t.Helper()
	var out []TenantSummary
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		out, e = opScanTenants(ctx, sp, hash, ticket, query, number, size)
		return e
	})
	return out, err
}

func opReadDetail(t *testing.T, ctx context.Context, tx pgx.Tx, hash, ticket string, tenant uuid.UUID) ([]TenantOverview, error) {
	t.Helper()
	var out []TenantOverview
	err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		var e error
		out, e = opScanDetail(ctx, sp, hash, ticket, tenant)
		return e
	})
	return out, err
}

// opFixtureTenant is one tenant written by the owner inside the test's transaction, with
// what the overview counts. Every row the counts must NOT include is written as well.
type opFixtureTenant struct {
	id                                  uuid.UUID
	name                                string
	locations                           int
	activeEmp, invitedEmp, deactivated  int
	activeTags, retiredTags, spareTags  int
	activeAdmins, disabledAdmins        int
	adminEmail, disabledEmail, empEmail string
}

// opUID is a canonical plaque uid: 14 upper-case hex digits.
func opUID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 7)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return strings.ToUpper(hex.EncodeToString(b))
}

// opNewTenant writes a tenant named name, created `ahead` after the wall clock plus a
// day (future, so it heads the newest-first list), as the owner.
func opNewTenant(t *testing.T, ctx context.Context, tx pgx.Tx, name string, ahead time.Duration) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenants (id, name, vat_number, business_type, structure, plan, created_at)
		VALUES ($1, $2, $3, 'restaurant', 'multi', 'standard',
		        clock_timestamp() + interval '1 day' + make_interval(secs => $4))`,
		id, name, "VAT-OP11-"+id.String(), ahead.Seconds()); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	return id
}

// opPopulate writes f's rows for an existing tenant f.id.
func opPopulate(t *testing.T, ctx context.Context, tx pgx.Tx, f *opFixtureTenant) {
	t.Helper()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("populate %s: %s: %v", f.name, sql[:40], err)
		}
	}
	var loc uuid.UUID
	for i := 0; i < f.locations; i++ {
		loc = uuid.New()
		exec(`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, $3)`, loc, f.id, fmt.Sprintf("op11 venue %d", i))
	}
	if f.locations == 0 {
		t.Fatalf("PREMISE: %s needs a location for its people and plaques", f.name)
	}
	for status, n := range map[string]int{"active": f.activeEmp, "invited": f.invitedEmp, "deactivated": f.deactivated} {
		for i := 0; i < n; i++ {
			exec(`INSERT INTO employees (tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, $4)`,
				f.id, loc, fmt.Sprintf("op11 person %s %d", status, i), status)
		}
	}
	if f.empEmail != "" {
		exec(`INSERT INTO employees (tenant_id, location_id, full_name, email, status) VALUES ($1, $2, 'op11 mailed person', $3, 'invited')`,
			f.id, loc, f.empEmail)
		f.invitedEmp++
	}
	for i := 0; i < f.activeTags; i++ {
		exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 'active')`,
			opUID(t), f.id, loc)
	}
	for i := 0; i < f.retiredTags; i++ {
		exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 'retired')`,
			opUID(t), f.id, loc)
	}
	for i := 0; i < f.spareTags; i++ {
		exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status) VALUES ($1, $2, NULL, decode(repeat('dead', 22), 'hex'), 'unassigned')`,
			opUID(t), f.id)
	}
	for i := 0; i < f.activeAdmins; i++ {
		email := fmt.Sprintf("op11-%s-%d@example.test", f.id.String()[:8], i)
		if i == 0 && f.adminEmail != "" {
			email = f.adminEmail
		}
		exec(`INSERT INTO admin_users (tenant_id, full_name, email, password_hash, role, status) VALUES ($1, 'op11 admin', $2, $3, $4, 'active')`,
			f.id, email, opFakeDigest("o"), map[bool]string{true: "owner", false: "manager"}[i == 0])
	}
	for i := 0; i < f.disabledAdmins; i++ {
		email := fmt.Sprintf("op11-%s-off%d@example.test", f.id.String()[:8], i)
		if i == 0 && f.disabledEmail != "" {
			email = f.disabledEmail
		}
		exec(`INSERT INTO admin_users (tenant_id, full_name, email, password_hash, role, status) VALUES ($1, 'op11 former admin', $2, $3, 'manager', 'disabled')`,
			f.id, email, opFakeDigest("o"))
	}
}

// opNames returns the names of the rows of a list page, in order.
func opNames(rows []TenantSummary) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Name)
	}
	return out
}

// opRunSection runs a migration section's text on q's connection, as goose would.
func opRunSection(t *testing.T, ctx context.Context, q pgx.Tx, sql, what string) {
	t.Helper()
	if _, err := q.Conn().PgConn().Exec(ctx, sql).ReadAll(); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// opMigrationSections returns the Up and Down sections of a migration file.
func opMigrationSections(t *testing.T, file string) (up, down string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", file))
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	src := string(b)
	iu, id := strings.Index(src, "-- +goose Up"), strings.Index(src, "-- +goose Down")
	if iu < 0 || id < iu {
		t.Fatalf("%s does not carry the goose markers in order", file)
	}
	return src[iu:id], src[id:]
}

// ---------------------------------------------------------------- catalogue --

// TestOperator00029_TheFunctionsAndTheirExactSignatures pins what ADR 0021 §6's generic
// pins leave open for the three functions 00029 creates or replaces: the exact argument
// list (the reads take the session, the RAW ticket and their own parameters; no actor),
// the exact result -- §2 ii's fixed column list, which is the whole of what a read can
// return, so a key or hash column cannot join it without this test turning red -- the
// owner, one overload per name, a clean forward and frozen-clock scan, and that the
// replaced op_begin_read is still the one function of that name.
func TestOperator00029_TheFunctionsAndTheirExactSignatures(t *testing.T) {
	ctx, tx := opTx(t)
	for name, want := range op00029Functions {
		var args, result, owner string
		var retset, secdef bool
		var config []string
		if err := tx.QueryRow(ctx, `
			SELECT pg_get_function_identity_arguments(p.oid), pg_get_function_result(p.oid),
			       pg_get_userbyid(p.proowner), p.proretset, p.prosecdef, p.proconfig
			  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			 WHERE n.nspname = 'public' AND p.proname = $1`, name).Scan(&args, &result, &owner, &retset, &secdef, &config); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if args != want.args {
			t.Errorf("%s(%s), want (%s)", name, args, want.args)
		}
		if result != want.result {
			t.Errorf("%s returns %s, want %s", name, result, want.result)
		}
		if owner != "tappa_opdefiner" || !secdef {
			t.Errorf("%s: owner %s, SECURITY DEFINER %v; want tappa_opdefiner, true", name, owner, secdef)
		}
		if len(config) != 1 || config[0] != "search_path=pg_catalog, pg_temp" {
			t.Errorf("%s: proconfig %v", name, config)
		}
		if retset != strings.HasPrefix(name, "op_read_") {
			t.Errorf("%s proretset = %v", name, retset)
		}
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		                             WHERE n.nspname = 'public' AND p.proname = $1`, name); n != 1 {
			t.Errorf("%d functions named %s; an overload would be a second door", n, name)
		}
		for _, who := range []string{"public", "tappa_app", "tappa_resolver"} {
			var may bool
			if err := tx.QueryRow(ctx, `SELECT has_function_privilege($1, p.oid, 'EXECUTE') FROM pg_proc p
			                             JOIN pg_namespace n ON n.oid = p.pronamespace
			                            WHERE n.nspname = 'public' AND p.proname = $2`, who, name).Scan(&may); err != nil {
				t.Fatal(err)
			}
			if may {
				t.Errorf("%s may EXECUTE %s", who, name)
			}
		}
	}
	findings, names, err := opForwardFindings(ctx, tx)
	if err != nil {
		t.Fatalf("forward scan: %v", err)
	}
	for name := range op00029Functions {
		if !slices.Contains(names, name) {
			t.Errorf("anti-vacuity: the forward scan did not walk %s", name)
		}
		for _, f := range findings {
			if strings.Contains(f, name+"(") {
				t.Error(f)
			}
		}
	}
	clock, _, err := opFrozenClockFindings(ctx, tx)
	if err != nil {
		t.Fatalf("frozen-clock scan: %v", err)
	}
	for _, f := range clock {
		t.Error(f)
	}
	// The consumption source pin (operatorlegal_test.go) reads every op_read_* by name;
	// both new reads are among what it read, and it raises nothing about them.
	consumption, read, err := opReadConsumptionFindings(ctx, tx)
	if err != nil {
		t.Fatalf("consumption scan: %v", err)
	}
	for _, name := range []string{"op_read_tenants", "op_read_tenant_detail"} {
		if !slices.Contains(read, name) {
			t.Errorf("anti-vacuity: the consumption scan did not read %s (it read %v)", name, read)
		}
		for _, f := range consumption {
			if strings.Contains(f, name+"(") {
				t.Error(f)
			}
		}
	}
	var kinds string
	if err := tx.QueryRow(ctx, `SELECT pg_get_constraintdef(oid) FROM pg_constraint
	                            WHERE conrelid = 'public.operator_read_tickets'::regclass
	                              AND conname = 'operator_read_tickets_kind_check'`).Scan(&kinds); err != nil {
		t.Fatal(err)
	}
	// The database is at HEAD, not at 00029: a later migration widens the closed set
	// (00030 added 'tenant_plaques'), so this pin is "a closed set (no pattern) that holds
	// 00029's three kinds"; the exact set at HEAD is the newest migration's own pin
	// (TestOperator00030_TheFunctionAndItsExactSignature), and 00029's exact set after
	// 00030's Down is TestOperator00030_DownGivesBack00029AndUpTakesItAgain's.
	for _, kind := range []string{legalVersionsReadKind, tenantsReadKind, tenantDetailReadKind} {
		if !strings.Contains(kinds, "'"+kind+"'::text") || strings.Contains(kinds, "~") {
			t.Errorf("operator_read_tickets_kind_check is %s, want a closed set holding %q", kinds, kind)
		}
	}
}

// opSecretShaped derives, from the live catalogue, every column of a TENANT table (one
// with a tenant_id) that is shaped like a secret or a credential: a name with hash, key,
// secret, sealed, token, cmac, password or sha in it, or a bytea. Derived, not listed,
// so a column a later migration adds in that shape is covered the day it exists.
func opSecretShaped(t *testing.T, ctx context.Context, q opQuerier) []string {
	t.Helper()
	rows, err := q.Query(ctx, `
		SELECT c.relname || '.' || a.attname
		  FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid
		 WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
		   AND a.attnum > 0 AND NOT a.attisdropped
		   AND EXISTS (SELECT 1 FROM pg_attribute s WHERE s.attrelid = c.oid AND s.attname = 'tenant_id' AND NOT s.attisdropped)
		   AND (a.attname ~ '(hash|key|secret|sealed|token|cmac|password|sha)' OR a.atttypid = 'bytea'::regtype)
		 ORDER BY 1`)
	if err != nil {
		t.Fatalf("derive the secret-shaped columns: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// TestOperator00029_TheDefinerReadsNamedColumnsAndNoSecret is OP-11's acceptance on the
// catalogue and as statements:
//   - tappa_opdefiner may SELECT none of the secret-shaped columns of any tenant table
//     (derived from the catalogue and logged -- nine on 2026-10-03: ADR 0021 §1's seven
//     "asla" columns and tenant_branding's logo and its digest), and reading three of
//     them as tappa_opdefiner (both plaque keys, the admin digest) is 42501 -- a function
//     body runs as that role, so a changed body cannot return them either;
//   - tappa_operator holds no privilege on the five tables the reads touch, and its
//     direct SELECT of each is 42501 -- the rows reach it through the two functions only;
//   - tappa_app may EXECUTE none of the three functions (calling them is 42501), and its
//     own access is what it was: the same SELECT columns on tenants, and row level
//     security still showing it its own tenant only (two tenants written by the owner,
//     one visible in the application's context -- no tenant filter in the statement, so
//     the 1 is the policy's; the owner's count of the same statement is the control).
func TestOperator00029_TheDefinerReadsNamedColumnsAndNoSecret(t *testing.T) {
	ctx, tx := opTx(t)
	derived := opSecretShaped(t, ctx, tx)
	for _, must := range []string{"tags.aes_key_ref", "tags.app_key_ref", "admin_users.password_hash", "sessions.token_hash",
		"admin_sessions.token_hash", "password_resets.token_hash", "employee_invites.code_hash"} {
		if !slices.Contains(derived, must) {
			t.Errorf("anti-vacuity: the derivation does not find %s (it found %v)", must, derived)
		}
	}
	t.Logf("%d secret-shaped tenant-table columns: %v", len(derived), derived)
	for _, col := range derived {
		table, column, _ := strings.Cut(col, ".")
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_column_privilege('tappa_opdefiner', 'public.' || $1, $2, 'SELECT')`, table, column).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may {
			t.Errorf("tappa_opdefiner may SELECT %s", col)
		}
	}
	for _, probe := range []string{
		`SELECT aes_key_ref FROM public.tags LIMIT 1`,
		`SELECT app_key_ref FROM public.tags LIMIT 1`,
		`SELECT password_hash FROM public.admin_users LIMIT 1`,
	} {
		opWant(t, opExecAs(t, ctx, tx, "tappa_opdefiner", probe), sqlstateInsufficientPrivi, "tappa_opdefiner: "+probe)
	}
	// POSITIVE CONTROL: the definer does read what 00029 granted.
	if err := opExecAs(t, ctx, tx, "tappa_opdefiner", `SELECT count(*) FROM public.tags WHERE tenant_id = $1 AND status = 'active'`, uuid.New()); err != nil {
		t.Fatalf("CONTROL: tappa_opdefiner cannot read tags.tenant_id/status (%v); the refusals above would not be the missing column grant", err)
	}

	for _, table := range []string{"tenants", "locations", "employees", "tags", "admin_users"} {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE"} {
			if got := opColumns(t, ctx, tx, "tappa_operator", table, priv); got != "" {
				t.Errorf("tappa_operator %s on %s = (%s), want ()", priv, table, got)
			}
		}
		opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `SELECT count(*) FROM public.`+table), sqlstateInsufficientPrivi,
			"tappa_operator reading "+table+" directly")
	}

	for _, call := range []struct {
		name string
		sql  string
		args []any
	}{
		{"op_begin_read", beginOperatorReadSQL, []any{opRandHex(t), tenantsReadKind, opTenantsParams("", 1, 10)}},
		{"op_read_tenants", readTenantsSQL, []any{opRandHex(t), opRandHex(t), "", 1, 10}},
		{"op_read_tenant_detail", readTenantDetailSQL, []any{opRandHex(t), opRandHex(t), uuid.New()}},
	} {
		opWant(t, opExecAs(t, ctx, tx, "tappa_app", call.sql, call.args...), sqlstateInsufficientPrivi, "tappa_app calling "+call.name)
	}
	if got, want := opColumns(t, ctx, tx, "tappa_app", "tenants", "SELECT"),
		"id,name,vat_number,business_type,structure,plan,timezone,created_at,price_per_employee_month,vat_verified,vat_checked_at"; got != want {
		t.Errorf("tappa_app SELECT on tenants = (%s), want (%s) -- 00029 must not touch it", got, want)
	}
	a := opNewTenant(t, ctx, tx, "op11 rls a", 0)
	b := opNewTenant(t, ctx, tx, "op11 rls b", 0)
	var seen int64
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, a.String()); err != nil {
			return err
		}
		return sp.QueryRow(ctx, `SELECT count(*) FROM public.tenants WHERE id IN ($1, $2)`, a, b).Scan(&seen)
	}); err != nil {
		t.Fatalf("tappa_app in tenant A's context: %v", err)
	}
	if seen != 1 {
		t.Errorf("tappa_app in tenant A's context sees %d of the two tenants, want 1 (row level security)", seen)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM public.tenants WHERE id IN ($1, $2)`, a, b); n != 2 {
		t.Fatalf("CONTROL: the owner sees %d of the two tenants, want 2", n)
	}
}

// TestOperator00029_DownRestoresTheLegalOnlyReadAndUpTakesItAgain runs 00029's Down and
// Up from the migration file inside the test's transaction, after taking that transaction
// to 00029 (opAtVersion: every later migration's Down first). Down removes the two reads,
// gives op_begin_read back 00027's body VERBATIM (compared with 00027's file), takes
// every privilege on the five tables away from tappa_opdefiner, and puts the ticket kind
// CHECK back to 00027's set -- NOT VALID when a ticket of either new kind exists (branch
// 1: a 'tenants' ticket; branch 3: a 'tenant_detail' ticket alone), VALIDATED when none
// does (branch 2). Up takes it all again.
func TestOperator00029_DownRestoresTheLegalOnlyReadAndUpTakesItAgain(t *testing.T) {
	ctx, tx := opTx(t)
	// The database is at HEAD; 00029's Down is measured from 00029's own state, which the
	// transaction reaches by running every later Down first (00030's since OP-13).
	opAtVersion(t, ctx, tx, 29, legalVersionsReadKind, tenantsReadKind, tenantDetailReadKind)
	up, down := opMigrationSections(t, op00029File)
	b27, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "00027_move_legal_publishing_to_the_operator.sql"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)CREATE FUNCTION public\.op_begin_read\(.*?\nAS \$\$(.*?)\$\$;`).FindStringSubmatch(string(b27))
	if m == nil {
		t.Fatal("00027's op_begin_read body was not found")
	}
	body27 := m[1]

	type state struct {
		reads      int64
		definer    bool
		kinds      string
		validated  bool
		beginIs27  bool
		appExecute bool
	}
	read := func(q opQuerier) state {
		t.Helper()
		var s state
		var src string
		if err := q.QueryRow(ctx, `
			SELECT (SELECT count(*) FROM pg_proc WHERE proname IN ('op_read_tenants', 'op_read_tenant_detail')),
			       (SELECT bool_or(has_any_column_privilege('tappa_opdefiner', ('public.' || t)::regclass, p))
			          FROM unnest(ARRAY['tenants', 'locations', 'employees', 'tags', 'admin_users']) AS t,
			               unnest(ARRAY['SELECT', 'INSERT', 'UPDATE']) AS p),
			       (SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'operator_read_tickets_kind_check'),
			       (SELECT convalidated FROM pg_constraint WHERE conname = 'operator_read_tickets_kind_check'),
			       (SELECT prosrc FROM pg_proc WHERE oid = 'public.op_begin_read(text, text, jsonb)'::regprocedure),
			       has_function_privilege('tappa_app', 'public.op_begin_read(text, text, jsonb)', 'EXECUTE')`).
			Scan(&s.reads, &s.definer, &s.kinds, &s.validated, &src, &s.appExecute); err != nil {
			t.Fatalf("read the state: %v", err)
		}
		s.beginIs27 = src == body27
		return s
	}
	const kinds29 = `CHECK ((kind = ANY (ARRAY['legal_versions'::text, 'tenants'::text, 'tenant_detail'::text])))`
	const kinds27 = `CHECK ((kind = 'legal_versions'::text))`
	at29 := func(s state, when string) {
		t.Helper()
		if s.reads != 2 || !s.definer || s.kinds != kinds29 || !s.validated || s.beginIs27 || s.appExecute {
			t.Errorf("%s: %+v; want both reads, the definer's grants, the widened CHECK validated, the replaced op_begin_read, no EXECUTE for tappa_app", when, s)
		}
	}
	at29(read(tx), "PREMISE before Down")

	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)

	// Branch 1: a 'tenants' ticket exists -> the restored CHECK is NOT VALID.
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	opForgeRead(t, ctx, sp, session, a.id, tenantsReadKind, opRandHex(t), nil, opCommittedXact(t, ctx), "30 seconds")
	opRunSection(t, ctx, sp, down, "00029 Down with a 'tenants' ticket present")
	s := read(sp)
	if s.reads != 0 || s.definer || s.kinds != kinds27+" NOT VALID" || s.validated || !s.beginIs27 || s.appExecute {
		t.Errorf("after Down (a 'tenants' ticket present): %+v; want no reads, no definer grant, 00027's CHECK NOT VALID, 00027's op_begin_read body", s)
	}
	// What op_begin_read now does: the legal read only.
	if _, err := opBegin(t, ctx, sp, hash, legalVersionsReadKind, opLegalParams(1, 5)); err != nil {
		t.Errorf("after Down the legal first phase is refused: %v", err)
	}
	_, err = opBegin(t, ctx, sp, hash, tenantsReadKind, opTenantsParams("", 1, 5))
	opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "after Down, a 'tenants' first phase")
	opWant(t, opTry(t, ctx, sp, `INSERT INTO operator_read_tickets (ticket_hash, session_id, kind, audit_id, expires_at)
	                              SELECT $1, $2, 'tenants', a.id, clock_timestamp() + interval '30 seconds'
	                                FROM operator_audit_log a WHERE a.session_id = $2 LIMIT 1`, opRandHex(t), session),
		sqlstateCheckViolation, "a NEW 'tenants' ticket after Down (NOT VALID still binds new rows)")
	opRunSection(t, ctx, sp, up, "00029 Up again")
	at29(read(sp), "after Up again")
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// Branch 2: no such ticket -> VALIDATED.
	sp, err = tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `DELETE FROM operator_read_tickets WHERE kind IN ('tenants', 'tenant_detail')`); err != nil {
		t.Fatalf("clear the new kinds' tickets inside the transaction: %v", err)
	}
	opRunSection(t, ctx, sp, down, "00029 Down with no 'tenants'/'tenant_detail' ticket")
	if s := read(sp); !s.validated || s.kinds != kinds27 || !s.beginIs27 {
		t.Errorf("after Down with no ticket of the two kinds: %+v; want 00027's CHECK VALIDATED and 00027's body", s)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	// Branch 3: a 'tenant_detail' ticket ALONE -> NOT VALID. Branch 1 holds only a
	// 'tenants' ticket, so a Down whose condition names 'tenants' alone passed it (a review
	// measured that mutation green); this branch is the one it cannot pass.
	sp, err = tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sp.Exec(ctx, `DELETE FROM operator_read_tickets WHERE kind IN ('tenants', 'tenant_detail')`); err != nil {
		t.Fatalf("clear the new kinds' tickets inside the transaction: %v", err)
	}
	detailTenant := uuid.New()
	opForgeRead(t, ctx, sp, session, a.id, tenantDetailReadKind, opRandHex(t), &detailTenant, opCommittedXact(t, ctx), "30 seconds")
	opRunSection(t, ctx, sp, down, "00029 Down with a 'tenant_detail' ticket alone")
	if s := read(sp); s.validated || s.kinds != kinds27+" NOT VALID" {
		t.Errorf("after Down with a 'tenant_detail' ticket alone: %+v; want 00027's CHECK NOT VALID", s)
	}
	if err := sp.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestOperator00029_PreconditionRefusesAWrongCluster: 00029's first statement refuses
// the role shapes 00026's and 00027's refuse (absent, over-privileged, joined by
// membership in either direction), with SQLSTATE 55000 naming 00029, and passes the
// cluster this suite runs on.
func TestOperator00029_PreconditionRefusesAWrongCluster(t *testing.T) {
	ctx, tx := opTx(t)
	up, _ := opMigrationSections(t, op00029File)
	i, j := strings.Index(up, "DO $$"), strings.Index(up, "-- +goose StatementEnd")
	if i < 0 || j < i {
		t.Fatal("00029's Up does not open with the precondition DO block")
	}
	pre := up[i:j]
	cases := []struct {
		what  string
		setup []string
		want  string
	}{
		{"roles present", nil, ""},
		{"both roles absent", []string{`ALTER ROLE tappa_operator RENAME TO zz_op11_was_operator`,
			`ALTER ROLE tappa_opdefiner RENAME TO zz_op11_was_opdefiner`}, "needs the cluster role(s) tappa_opdefiner, tappa_operator"},
		{"tappa_opdefiner is a superuser", []string{`ALTER ROLE tappa_opdefiner SUPERUSER`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner can log in", []string{`ALTER ROLE tappa_opdefiner LOGIN`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner without BYPASSRLS", []string{`ALTER ROLE tappa_opdefiner NOBYPASSRLS`}, "tappa_opdefiner must be"},
		{"tappa_operator bypasses RLS", []string{`ALTER ROLE tappa_operator BYPASSRLS`}, "tappa_operator must be"},
		{"tappa_opdefiner has a member", []string{`GRANT tappa_opdefiner TO tappa_app`}, "has members"},
		{"tappa_opdefiner is a member", []string{`GRANT tappa_owner TO tappa_opdefiner`}, "role tappa_opdefiner is a member of another role"},
		{"tappa_operator is a member", []string{`GRANT tappa_resolver TO tappa_operator`}, "role tappa_operator is a member of another role"},
		{"tappa_operator has a member", []string{`GRANT tappa_operator TO tappa_app`}, "role tappa_operator has members"},
	}
	for _, c := range cases {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		for _, s := range c.setup {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: setup %q: %v", c.what, s, err)
			}
		}
		_, runErr := sp.Conn().PgConn().Exec(ctx, pre).ReadAll()
		code, msg := opCode(runErr)
		switch {
		case c.want == "" && runErr != nil:
			t.Errorf("%s: the precondition refuses a correct cluster: %v", c.what, runErr)
		case c.want != "" && (code != sqlstatePrerequisiteState || !strings.Contains(msg, c.want) || !strings.Contains(msg, "00029")):
			t.Errorf("%s: precondition answered %q %q, want %s naming 00029 and containing %q", c.what, code, msg, sqlstatePrerequisiteState, c.want)
		}
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("%s: rollback: %v", c.what, err)
		}
	}
}

// TestOperator00029_CallersTempTableIsNeverRead is ADR 0021 §6's temp-table shadow for
// the two reads and the replaced first phase: the caller creates temp tables with every
// name they touch (the five tenant tables and the four operator tables), fills them with
// forged rows, and GRANTs them to tappa_opdefiner (the step without which a broken
// search_path would fail with "permission denied" and look refused); the functions still
// read and write the real ones.
func TestOperator00029_CallersTempTableIsNeverRead(t *testing.T) {
	ctx, tx := opTx(t)
	tok := opToken(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	real := &opFixtureTenant{id: opNewTenant(t, ctx, tx, "Real "+tok, 0), name: "Real " + tok, locations: 1, activeEmp: 2, activeTags: 1, activeAdmins: 1}
	opPopulate(t, ctx, tx, real)
	shadowID := uuid.New()
	forgedSession := opRandHex(t)

	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		for _, s := range []string{
			`CREATE TEMP TABLE tenants (id uuid, name text, created_at timestamptz, plan text, business_type text, structure text)`,
			`CREATE TEMP TABLE locations (id uuid, tenant_id uuid)`,
			`CREATE TEMP TABLE employees (tenant_id uuid, status text)`,
			`CREATE TEMP TABLE tags (tenant_id uuid, status text)`,
			`CREATE TEMP TABLE admin_users (tenant_id uuid, email text, status text)`,
			`CREATE TEMP TABLE platform_sessions (id uuid DEFAULT gen_random_uuid(), admin_id uuid, token_hash text,
			     created_at timestamptz DEFAULT clock_timestamp(), mfa_verified_at timestamptz,
			     last_used_at timestamptz DEFAULT clock_timestamp(), revoked_at timestamptz)`,
			`CREATE TEMP TABLE platform_admins (id uuid, email text, display_name text, status text)`,
			`CREATE TEMP TABLE operator_audit_log (id uuid DEFAULT gen_random_uuid(), at timestamptz DEFAULT clock_timestamp(),
			     kind text, session_id uuid, actor_admin_id uuid, target_admin_id uuid, target_tenant_id uuid,
			     target_scope text, page_number integer, page_size integer, detail jsonb DEFAULT '{}')`,
			`CREATE TEMP TABLE operator_read_tickets (id uuid DEFAULT gen_random_uuid(), ticket_hash text, session_id uuid,
			     kind text, target_tenant_id uuid, audit_id uuid, created_at timestamptz DEFAULT clock_timestamp(),
			     created_xact xid8 DEFAULT '3'::xid8, expires_at timestamptz, consumed_at timestamptz)`,
			`GRANT ALL ON pg_temp.tenants, pg_temp.locations, pg_temp.employees, pg_temp.tags, pg_temp.admin_users,
			     pg_temp.platform_sessions, pg_temp.platform_admins, pg_temp.operator_audit_log,
			     pg_temp.operator_read_tickets TO tappa_opdefiner`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				return fmt.Errorf("%s: %w", s[:40], err)
			}
		}
		for _, s := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO pg_temp.tenants VALUES ($1, $2, clock_timestamp() + interval '2 days', 'founding', 'bar', 'single')`, []any{shadowID, "Shadow " + tok}},
			{`INSERT INTO pg_temp.tenants VALUES ($1, 'SHADOW name of the real id', clock_timestamp(), 'founding', 'bar', 'single')`, []any{real.id}},
			{`INSERT INTO pg_temp.locations SELECT gen_random_uuid(), $1::uuid FROM generate_series(1, 7)`, []any{real.id}},
			{`INSERT INTO pg_temp.employees SELECT $1::uuid, 'active' FROM generate_series(1, 7)`, []any{real.id}},
			{`INSERT INTO pg_temp.tags SELECT $1::uuid, 'active' FROM generate_series(1, 7)`, []any{real.id}},
			{`INSERT INTO pg_temp.admin_users VALUES ($1, $2, 'active')`, []any{shadowID, "shadow-" + tok + "@example.test"}},
			{`INSERT INTO pg_temp.platform_sessions (admin_id, token_hash, mfa_verified_at) VALUES ($1, $2, clock_timestamp())`, []any{a.id, forgedSession}},
		} {
			if _, err := sp.Exec(ctx, s.sql, s.args...); err != nil {
				return fmt.Errorf("%s: %w", s.sql[:40], err)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("build the shadow as the caller: %v", err)
	}
	// CONTROL: the shadow is reachable by the definer role.
	var reachable int64
	if err := opAs(t, ctx, tx, "tappa_opdefiner", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.tenants) + (SELECT count(*) FROM pg_temp.locations)
		                              + (SELECT count(*) FROM pg_temp.admin_users) + (SELECT count(*) FROM pg_temp.platform_sessions)`).Scan(&reachable)
	}); err != nil {
		t.Fatalf("the definer role cannot read the shadow (%v); the GRANT step is what makes this test mean anything", err)
	}
	if reachable != 11 {
		t.Fatalf("the definer role sees %d forged rows, want 11", reachable)
	}

	// The forged SESSION exists only in the shadow.
	_, err := opBegin(t, ctx, tx, forgedSession, tenantsReadKind, opTenantsParams(tok, 1, 5))
	opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_begin_read with a session that exists only in the caller's temp table")
	// The real session writes its 'read' row into the REAL log.
	audit0 := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, session)
	if _, err := opBegin(t, ctx, tx, hash, tenantDetailReadKind, opDetailParams(real.id.String())); err != nil {
		t.Fatalf("op_begin_read with the real session: %v", err)
	}
	if d := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, session) - audit0; d != 1 {
		t.Errorf("%d 'read' row(s) reached the REAL log, want 1", d)
	}
	xact := opCommittedXact(t, ctx)
	// The list: the search for the token finds the REAL tenant and not the shadow's, by
	// name and by the shadow's admin address alike.
	for _, term := range []string{tok, "shadow-" + tok + "@example.test", shadowID.String()} {
		rows, err := opReadTenants(t, ctx, tx, hash, opForgeTenants(t, ctx, tx, session, a.id, term, 1, 5, xact), term, 1, 5)
		if err != nil {
			t.Fatalf("op_read_tenants %q: %v", term, err)
		}
		for _, r := range rows {
			if r.ID == shadowID || strings.HasPrefix(r.Name, "Shadow") || strings.HasPrefix(r.Name, "SHADOW") {
				t.Errorf("op_read_tenants %q returned a row of the caller's temp table: %+v", term, r)
			}
		}
		if term == tok && (len(rows) != 1 || rows[0].ID != real.id) {
			t.Errorf("op_read_tenants %q = %v, want the real tenant only", term, opNames(rows))
		}
	}
	// The overview: the real name and the real counts, not the shadow's seven of each.
	rows, err := opReadDetail(t, ctx, tx, hash, opForgeDetail(t, ctx, tx, session, a.id, real.id, xact), real.id)
	if err != nil {
		t.Fatalf("op_read_tenant_detail: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != real.name || rows[0].Locations != 1 || rows[0].ActiveEmployees != 2 ||
		rows[0].ActivePlaques != 1 || rows[0].ActiveAdmins != 1 {
		t.Errorf("op_read_tenant_detail read %+v, want the real tenant's name and counts 1/2/1/1", rows)
	}
	var shadowAudit, shadowTickets int64
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.operator_audit_log), (SELECT count(*) FROM pg_temp.operator_read_tickets)`).
			Scan(&shadowAudit, &shadowTickets)
	}); err != nil {
		t.Fatal(err)
	}
	if shadowAudit != 0 || shadowTickets != 0 {
		t.Errorf("the CALLER's temp tables were written: audit=%d tickets=%d, want 0 and 0", shadowAudit, shadowTickets)
	}
}

// ------------------------------------------------------------ op_begin_read --

// TestOpBeginRead_TheTenantKindsBindEveryParameterAndAuditNoTerm: the two kinds 00029
// adds to the first phase.
//   - 'tenants': one 'read' row -- scope 'tenants', the page as plain integers, detail
//     exactly {"search": <class>} (the key set is asserted, not scanned for forbidden
//     words) -- and one ticket whose stored hash is sha256(raw ticket || the canonical
//     {query, page_size, page_number}); the TERM is in neither row, raw or hashed (an
//     address-shaped term, so a leak would be personal data). The class follows 00029
//     section 3's rules in their order for each of the eleven terms below -- every class,
//     and the edges between them: a single space is 'text' (only the empty term is 'none'), a uuid
//     with one more character, in braces or without its hyphens is 'text' (only the
//     whole hyphenated form is 'id', the one form op_read_tenants' id branch takes), a
//     uuid with an '@' is 'address', '@' alone is 'address';
//   - 'tenant_detail': scope 'tenant_detail', the tenant in target_tenant_id on the row and
//     on the ticket, no page, detail {}; an upper-case id binds the same canonical
//     lower-case text;
//   - 22023 and no row for every parameter object the kinds do not name (listed below),
//     28000 and no row for the six dead sessions; the controls at the bounds are accepted.
//
// CONTROL for the Go side of the hash: opTenantsCanonical's text equals the database's
// jsonb text for terms with a quote, a backslash, control characters and non-ASCII.
func TestOpBeginRead_TheTenantKindsBindEveryParameterAndAuditNoTerm(t *testing.T) {
	ctx, tx := opTx(t)
	for _, term := range []string{"", `Ta' "quoted" \ back`, "tab\tnl\ncr\rbs\bff\f\x01\x1f", "Żebbuġ ħanut", "🍢 kebab"} {
		var dbText string
		if err := tx.QueryRow(ctx, `SELECT jsonb_build_object('page_number', 3, 'page_size', 25, 'query', $1::text)::text`, term).Scan(&dbText); err != nil {
			t.Fatal(err)
		}
		if got := opTenantsCanonical(term, 3, 25); got != dbText {
			t.Fatalf("CONTROL: Go's canonical text %q differs from the database's %q", got, dbText)
		}
	}
	id := uuid.New()
	var detailText string
	if err := tx.QueryRow(ctx, `SELECT jsonb_build_object('tenant_id', $1::uuid)::text`, strings.ToUpper(id.String())).Scan(&detailText); err != nil {
		t.Fatal(err)
	}
	if detailText != opDetailCanonical(id) {
		t.Fatalf("CONTROL: Go's canonical detail text %q differs from the database's %q", opDetailCanonical(id), detailText)
	}

	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	rowsOf := func() (audit, tickets int64) {
		return opAudit(t, ctx, tx), opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets`)
	}
	type auditRow struct {
		kind, scope, keys, detail, text string
		tenant                          *uuid.UUID
		number, size                    *int
	}
	lastAudit := func() (auditRow, string, *uuid.UUID, string) {
		t.Helper()
		var r auditRow
		var ticketHash, ticketText string
		var ticketTenant *uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT l.kind, l.target_scope, coalesce((SELECT string_agg(k, ',' ORDER BY k) FROM jsonb_object_keys(l.detail) AS k), ''),
			       l.detail::text, row_to_json(l)::text, l.target_tenant_id, l.page_number, l.page_size,
			       k.ticket_hash, k.target_tenant_id, row_to_json(k)::text
			  FROM operator_audit_log l JOIN operator_read_tickets k ON k.audit_id = l.id
			 WHERE l.session_id = $1 ORDER BY l.at DESC, l.id DESC LIMIT 1`, session).
			Scan(&r.kind, &r.scope, &r.keys, &r.detail, &r.text, &r.tenant, &r.number, &r.size, &ticketHash, &ticketTenant, &ticketText); err != nil {
			t.Fatalf("read the last audit row and its ticket: %v", err)
		}
		return r, ticketHash, ticketTenant, ticketText
	}

	// 'tenants' with an address-shaped term.
	term := "Owner." + opToken(t) + "@Example.test"
	a0, k0 := rowsOf()
	raw, err := opBegin(t, ctx, tx, hash, tenantsReadKind, opTenantsParams(term, 2, 25))
	if err != nil {
		t.Fatalf("op_begin_read 'tenants': %v", err)
	}
	if a1, k1 := rowsOf(); a1-a0 != 1 || k1-k0 != 1 {
		t.Fatalf("op_begin_read 'tenants' wrote %d audit row(s) and %d ticket(s), want 1 and 1", a1-a0, k1-k0)
	}
	r, ticketHash, ticketTenant, ticketText := lastAudit()
	if r.kind != "read" || r.scope != tenantsReadKind || r.keys != "search" || r.detail != `{"search": "address"}` ||
		r.tenant != nil || r.number == nil || *r.number != 2 || r.size == nil || *r.size != 25 || ticketTenant != nil {
		t.Errorf("the 'tenants' audit row: kind=%s scope=%s detail keys=%q detail=%s tenant=%v page=%v/%v ticket tenant=%v",
			r.kind, r.scope, r.keys, r.detail, r.tenant, r.number, r.size, ticketTenant)
	}
	if ticketHash != opTenantsTicketHash(raw, term, 2, 25) {
		t.Error("the stored hash is not sha256(raw ticket || canonical {query, page_size, page_number})")
	}
	lower := strings.ToLower(r.text + ticketText)
	for _, leak := range []string{strings.ToLower(term), strings.ToLower(term[:strings.Index(term, "@")]), raw} {
		if strings.Contains(lower, strings.ToLower(leak)) {
			t.Errorf("the audit row or the ticket row carries the term or the raw ticket (%d characters)", len(leak))
		}
	}
	termSum := sha256.Sum256([]byte(term))
	if strings.Contains(lower, hex.EncodeToString(termSum[:])) {
		t.Error("the term's own hash is stored")
	}
	// The class of every kind of term, and the edges between the rules.
	someID := uuid.New()
	for _, c := range []struct{ term, class string }{
		{"", "none"},
		{" ", "text"}, // not '' -- and a blank matches every name with a space in it
		{"Valletta Grill " + opToken(t), "text"},
		{"ŻEBBUĠ ħanut", "text"},
		{someID.String() + "x", "text"},
		{"{" + someID.String() + "}", "text"},                  // PostgreSQL's uuid input takes braces; the class does not
		{strings.ReplaceAll(someID.String(), "-", ""), "text"}, // nor a uuid without hyphens
		{strings.ToUpper(someID.String()), "id"},
		{"owner." + opToken(t) + "@example.test", "address"},
		{someID.String() + "@x", "address"},
		{"@", "address"},
	} {
		if _, err := opBegin(t, ctx, tx, hash, tenantsReadKind, opTenantsParams(c.term, 1, 10)); err != nil {
			t.Fatalf("op_begin_read 'tenants' with a %s term: %v", c.class, err)
		}
		r, _, _, ticketText := lastAudit()
		if r.keys != "search" || r.detail != `{"search": "`+c.class+`"}` {
			t.Errorf("term %q: detail %s (keys %q), want {\"search\": %q}", c.term, r.detail, r.keys, c.class)
		}
		if len(c.term) > 2 && strings.Contains(strings.ToLower(r.text+ticketText), strings.ToLower(c.term)) {
			t.Errorf("a %s term's audit row or ticket row carries the term (%d characters)", c.class, len(c.term))
		}
	}

	// 'tenant_detail', the id sent upper-case.
	a0, k0 = rowsOf()
	raw, err = opBegin(t, ctx, tx, hash, tenantDetailReadKind, opDetailParams(strings.ToUpper(id.String())))
	if err != nil {
		t.Fatalf("op_begin_read 'tenant_detail': %v", err)
	}
	if a1, k1 := rowsOf(); a1-a0 != 1 || k1-k0 != 1 {
		t.Fatalf("op_begin_read 'tenant_detail' wrote %d audit row(s) and %d ticket(s), want 1 and 1", a1-a0, k1-k0)
	}
	r, ticketHash, ticketTenant, _ = lastAudit()
	if r.kind != "read" || r.scope != tenantDetailReadKind || r.detail != "{}" || r.tenant == nil || *r.tenant != id ||
		r.number != nil || r.size != nil || ticketTenant == nil || *ticketTenant != id {
		t.Errorf("the 'tenant_detail' audit row: kind=%s scope=%s detail=%s tenant=%v page=%v/%v ticket tenant=%v",
			r.kind, r.scope, r.detail, r.tenant, r.number, r.size, ticketTenant)
	}
	if ticketHash != opDetailTicketHash(raw, id) {
		t.Error("the stored hash is not sha256(raw ticket || canonical {tenant_id}) with the id in lower case")
	}

	// Refusals: 22023, no row, nothing of what was sent echoed.
	long := strings.Repeat("ż", MaxTenantSearchRunes+1)
	for _, c := range []struct {
		name   string
		kind   any
		params string
	}{
		{"tenants: no query key", tenantsReadKind, opLegalParams(1, 10)},
		{"tenants: query a number", tenantsReadKind, `{"page_number": 1, "page_size": 10, "query": 7}`},
		{"tenants: query null", tenantsReadKind, `{"page_number": 1, "page_size": 10, "query": null}`},
		{"tenants: query an array", tenantsReadKind, `{"page_number": 1, "page_size": 10, "query": ["x"]}`},
		{"tenants: query one character too long", tenantsReadKind, opTenantsParams(long, 1, 10)},
		{"tenants: an extra key", tenantsReadKind, `{"page_number": 1, "page_size": 10, "query": "", "tenant_id": "x"}`},
		{"tenants: page_size 201", tenantsReadKind, opTenantsParams("", 1, 201)},
		{"tenants: page_number 0", tenantsReadKind, opTenantsParams("", 0, 10)},
		{"tenants: page_number -1", tenantsReadKind, opTenantsParams("", -1, 10)},
		{"tenants: page_number 10 digits", tenantsReadKind, opTenantsParams("", 1000000000, 10)},
		{"tenant_detail: no key", tenantDetailReadKind, `{}`},
		{"tenant_detail: an extra key", tenantDetailReadKind, `{"tenant_id": "` + id.String() + `", "page_number": 1}`},
		{"tenant_detail: the list's keys", tenantDetailReadKind, opTenantsParams("", 1, 10)},
		{"tenant_detail: not a uuid", tenantDetailReadKind, opDetailParams("op11 not a uuid")},
		{"tenant_detail: a uuid in braces", tenantDetailReadKind, opDetailParams("{" + id.String() + "}")},
		{"tenant_detail: a uuid without hyphens", tenantDetailReadKind, opDetailParams(strings.ReplaceAll(id.String(), "-", ""))},
		{"tenant_detail: a uuid with a trailing newline", tenantDetailReadKind, opDetailParams(id.String() + "\n")},
		{"tenant_detail: a number", tenantDetailReadKind, `{"tenant_id": 7}`},
		{"tenant_detail: null", tenantDetailReadKind, `{"tenant_id": null}`},
		{"a kind outside the closed set", "tenant", opTenantsParams("", 1, 10)},
	} {
		a0, k0 := rowsOf()
		_, err := opBegin(t, ctx, tx, hash, c.kind, c.params)
		opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "op_begin_read, "+c.name, hash, c.params)
		if a1, k1 := rowsOf(); a1 != a0 || k1 != k0 {
			t.Errorf("op_begin_read, %s: %d audit row(s) and %d ticket(s) written", c.name, a1-a0, k1-k0)
		}
	}
	// Dead sessions: 28000 and no row, for both kinds.
	for _, d := range opDeadSessions(t, ctx, tx) {
		for _, c := range []struct{ kind, params string }{
			{tenantsReadKind, opTenantsParams(term, 1, 10)},
			{tenantDetailReadKind, opDetailParams(id.String())},
		} {
			a0, k0 := rowsOf()
			_, err := opBegin(t, ctx, tx, d.hash, c.kind, c.params)
			opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_begin_read "+c.kind+", "+d.name, d.hash, term)
			if a1, k1 := rowsOf(); a1 != a0 || k1 != k0 {
				t.Errorf("op_begin_read %s, %s: %d audit row(s) and %d ticket(s) written", c.kind, d.name, a1-a0, k1-k0)
			}
		}
	}
	// CONTROLS at the bounds.
	for _, p := range []string{opTenantsParams(strings.Repeat("ż", MaxTenantSearchRunes), 999999999, 200),
		`{"query": "x", "page_size": 1, "page_number": 1}`} {
		if _, err := opBegin(t, ctx, tx, hash, tenantsReadKind, p); err != nil {
			t.Errorf("CONTROL: op_begin_read 'tenants' refused a parameter object at its bounds: %v", err)
		}
	}
}

// TestTenantList_TheSearchTermMeetsTheSameBoundInGoAndSQL holds MaxTenantSearchRunes and
// 00029's `char_length(query) > 254` together -- two copies of one bound, the first
// refusing before a round trip, the second for any caller that is not TenantList: a term
// of exactly MaxTenantSearchRunes characters is accepted by op_begin_read and one more is
// refused (22023), and through the accessor the longer one is ErrTenantSearchRefused
// WITHOUT A ROUND TRIP while the exact one is sent -- in two-byte characters, so the
// bound is CHARACTERS on both sides (Go counts runes, PostgreSQL char_length) and a byte
// bound in either would turn this red. And the other Go-side refusal: a term PostgreSQL
// cannot hold as text (invalid UTF-8, a NUL) is ErrTenantSearchRefused without a round
// trip too. "Without a round trip" is measured on a connection that counts every
// statement sent to it.
func TestTenantList_TheSearchTermMeetsTheSameBoundInGoAndSQL(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, _ := opNewSession(t, ctx, tx, a.id, true)
	at := strings.Repeat("ġ", MaxTenantSearchRunes)
	if len(at) != 2*MaxTenantSearchRunes {
		t.Fatalf("PREMISE: the term is %d bytes, want two per character", len(at))
	}
	if _, err := opBegin(t, ctx, tx, hash, tenantsReadKind, opTenantsParams(at, 1, 10)); err != nil {
		t.Errorf("a term of MaxTenantSearchRunes (%d) characters was refused: %v", MaxTenantSearchRunes, err)
	}
	_, err := opBegin(t, ctx, tx, hash, tenantsReadKind, opTenantsParams(at+"ġ", 1, 10))
	opWantClean(t, err, sqlstateInvalidParameter, beginParamsRefusal, "a term of MaxTenantSearchRunes+1 characters")
	// The same two terms through the accessor: MaxTenantSearchRunes+1 characters is
	// ErrTenantSearchRefused WITHOUT A ROUND TRIP (the term never reaches the server or
	// its statement log); MaxTenantSearchRunes characters is sent (the control).
	over := &opNoCallConn{}
	if _, err := TenantList(ctx, over, hash, TenantListQuery{Search: at + "ġ", Number: 1, Size: 10}); !errors.Is(err, ErrTenantSearchRefused) ||
		strings.Contains(err.Error(), "ġġ") {
		t.Errorf("TenantList with a term of MaxTenantSearchRunes+1 characters: %v, want ErrTenantSearchRefused (and no term in the text)", err)
	}
	if over.calls != 0 {
		t.Errorf("TenantList sent %d statement(s) for a term of MaxTenantSearchRunes+1 characters; it is refused before a round trip", over.calls)
	}
	at254 := &opNoCallConn{}
	if _, err := TenantList(ctx, at254, hash, TenantListQuery{Search: at, Number: 1, Size: 10}); errors.Is(err, ErrTenantSearchRefused) || at254.calls != 1 {
		t.Errorf("CONTROL: a term of MaxTenantSearchRunes characters: %v after %d statement(s), want the fake's error after 1", err, at254.calls)
	}

	for name, term := range map[string]string{"invalid UTF-8": "a\xffb", "a NUL": "a\x00b"} {
		c := &opNoCallConn{}
		if _, err := TenantList(ctx, c, hash, TenantListQuery{Search: term, Number: 1, Size: 10}); !errors.Is(err, ErrTenantSearchRefused) {
			t.Errorf("%s: %v, want ErrTenantSearchRefused", name, err)
		}
		if c.calls != 0 {
			t.Errorf("%s: the accessor sent %d statement(s); a term no text column can hold is answered without a round trip", name, c.calls)
		}
	}
	// CONTROL: the same fake connection DOES see a storable term's first phase.
	c := &opNoCallConn{}
	if _, err := TenantList(ctx, c, hash, TenantListQuery{Search: "fine", Number: 1, Size: 10}); err == nil || c.calls != 1 {
		t.Fatalf("CONTROL: a storable term reached the connection %d time(s) (err %v), want 1 and the fake's error", c.calls, err)
	}
}

// opNoCallConn is an OperatorConn that counts the statements sent to it and fails each.
type opNoCallConn struct{ calls int }

var errNoCall = errors.New("op11 test: the connection was used")

func (c *opNoCallConn) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	c.calls++
	return pgconn.CommandTag{}, errNoCall
}
func (c *opNoCallConn) QueryRow(context.Context, string, ...any) pgx.Row {
	c.calls++
	return opErrRow{}
}
func (c *opNoCallConn) Query(context.Context, string, ...any) (pgx.Rows, error) {
	c.calls++
	return nil, errNoCall
}

type opErrRow struct{}

func (opErrRow) Scan(...any) error { return errNoCall }

// ------------------------------------------------------------ the two reads --

// TestOpReadTenants_SearchMatchesNameAddressAndIDOnly: one term against three things.
//   - the NAME, as a case-insensitive substring (Maltese capitals included: 'Ż' finds
//     'ż');
//   - an ADMIN'S ADDRESS, EXACTLY and case-insensitively -- the address of tenant A's
//     owner finds A and no other tenant (the correlation by value: an uncorrelated
//     subquery would return every tenant), a disabled admin's address finds its tenant,
//     a PART of an address finds nothing, an EMPLOYEE's address finds nothing;
//   - the tenant's ID, whole and hyphenated (either case); a part of it, the id in braces
//     and the id without hyphens find nothing -- the one form op_begin_read classes 'id';
//   - a single space is a name match, not the whole list (the class says 'text');
//   - the empty term lists every tenant: the fixtures (created a day ahead) head the
//     first page;
//   - AND NOTHING ELSE ("Only"): the fixtures' plan, business type, structure and time
//     zone, one fixture's VAT number, and an admin address without '@' (the address branch
//     runs only for a term with '@' -- the predicate op_begin_read classes 'address' by)
//     each find no fixture, and every row they do return carries the term in its NAME.
//     The fixtures head the newest-first list, so a term matched against any of those
//     columns would put them on the page.
func TestOpReadTenants_SearchMatchesNameAddressAndIDOnly(t *testing.T) {
	ctx, tx := opTx(t)
	tok := opToken(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	mk := func(name string, ahead time.Duration, f opFixtureTenant) opFixtureTenant {
		f.id, f.name = opNewTenant(t, ctx, tx, name, ahead), name
		opPopulate(t, ctx, tx, &f)
		return f
	}
	ta := mk("Żebbuġ Grill "+tok, 4*time.Second, opFixtureTenant{locations: 1, activeAdmins: 1, adminEmail: "owner-a-" + tok + "@example.test"})
	tb := mk("Valletta Bar "+tok, 3*time.Second, opFixtureTenant{locations: 1, activeAdmins: 1, disabledAdmins: 1, disabledEmail: "gone-b-" + tok + "@example.test"})
	tc := mk("Sliema Café "+tok, 2*time.Second, opFixtureTenant{locations: 1, activeAdmins: 1, empEmail: "staff-c-" + tok + "@example.test"})
	td := mk("Mdina Kiosk", 1*time.Second, opFixtureTenant{locations: 1, activeAdmins: 1})
	noAt := "op11noat-" + tok // an admin address without '@' (sign-up refuses one; the owner can write it)
	te := mk("Gozo Deli", 0, opFixtureTenant{locations: 1, activeAdmins: 1, adminEmail: noAt})

	search := func(term string, size int) []TenantSummary {
		t.Helper()
		rows, err := opReadTenants(t, ctx, tx, hash, opForgeTenants(t, ctx, tx, session, a.id, term, 1, size, xact), term, 1, size)
		if err != nil {
			t.Fatalf("op_read_tenants %q: %v", term, err)
		}
		return rows
	}
	ids := func(rows []TenantSummary) []uuid.UUID {
		var out []uuid.UUID
		for _, r := range rows {
			out = append(out, r.ID)
		}
		return out
	}
	for _, c := range []struct {
		name, term string
		want       []uuid.UUID
	}{
		{"the token, newest first", tok, []uuid.UUID{ta.id, tb.id, tc.id}},
		{"the token upper-cased", strings.ToUpper(tok), []uuid.UUID{ta.id, tb.id, tc.id}},
		{"a Maltese capital against its small letter", "żEBBUĠ GRILL " + strings.ToUpper(tok), []uuid.UUID{ta.id}},
		{"A's owner's address", "owner-a-" + tok + "@example.test", []uuid.UUID{ta.id}},
		{"A's owner's address, upper-cased", strings.ToUpper("owner-a-" + tok + "@example.test"), []uuid.UUID{ta.id}},
		{"B's disabled admin's address", "gone-b-" + tok + "@example.test", []uuid.UUID{tb.id}},
		{"a part of A's address", "owner-a-" + tok + "@example", nil},
		{"C's employee's address", "staff-c-" + tok + "@example.test", nil},
		{"D's id", td.id.String(), []uuid.UUID{td.id}},
		{"D's id upper-cased", strings.ToUpper(td.id.String()), []uuid.UUID{td.id}},
		{"a part of D's id", td.id.String()[:18], nil},
		// The id branch takes the whole HYPHENATED uuid only -- the form op_begin_read
		// classes 'id'; braces and a hyphen-less spelling are 'text' there and find nothing
		// here (PostgreSQL's uuid input would take both).
		{"D's id in braces", "{" + td.id.String() + "}", nil},
		{"D's id without hyphens", strings.ReplaceAll(td.id.String(), "-", ""), nil},
	} {
		if got := ids(search(c.term, 50)); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// '' lists every tenant: the four fixtures, created a day ahead, head page one.
	if got := ids(search("", 4)); !slices.Equal(got, []uuid.UUID{ta.id, tb.id, tc.id, td.id}) {
		t.Errorf("the empty term's first page of four: %v, want the four fixtures newest first", got)
	}
	// A single space is a term ('text', not 'none'): a NAME match -- every row carries a
	// space in its name, and the five fixtures, whose names all have one, head the page.
	blank := search(" ", 5)
	for _, r := range blank {
		if !strings.Contains(r.Name, " ") {
			t.Errorf("the term \" \" returned %q, which has no space in its name", r.Name)
		}
	}
	if got := ids(blank); !slices.Equal(got, []uuid.UUID{ta.id, tb.id, tc.id, td.id, te.id}) {
		t.Errorf("the term \" \": %v, want the five fixtures (each name has a space) newest first", got)
	}
	// NOTHING ELSE: the values of the fixtures' other columns, as terms.
	fixtures := []uuid.UUID{ta.id, tb.id, tc.id, td.id, te.id}
	for _, c := range []struct{ name, term string }{
		{"the plan", "standard"},
		{"the business type", "restaurant"},
		{"the structure", "multi"},
		{"the time zone", "Europe/Malta"},
		{"A's VAT number", "VAT-OP11-" + ta.id.String()},
		{"E's admin address without '@'", noAt},
	} {
		for _, r := range search(c.term, 200) {
			if slices.Contains(fixtures, r.ID) {
				t.Errorf("%s (%q) found the fixture %q, whose name does not carry it", c.name, c.term, r.Name)
			}
			if !strings.Contains(strings.ToLower(r.Name), strings.ToLower(c.term)) {
				t.Errorf("%s (%q) returned %q, whose name does not carry the term", c.name, c.term, r.Name)
			}
		}
	}
	// CONTROL for the last row: the same address WITH an '@' in it is found -- the gate,
	// not the address, is what kept it out.
	if _, err := tx.Exec(ctx, `UPDATE admin_users SET email = $2 WHERE tenant_id = $1`, te.id, noAt+"@example.test"); err != nil {
		t.Fatal(err)
	}
	if got := ids(search(noAt+"@example.test", 50)); !slices.Equal(got, []uuid.UUID{te.id}) {
		t.Errorf("CONTROL: E's address with an '@' found %v, want E", got)
	}
	// The reads themselves write no audit row (phase one did, for each).
	before := opAudit(t, ctx, tx)
	raw := opForgeTenants(t, ctx, tx, session, a.id, tok, 1, 5, xact)
	afterForge := opAudit(t, ctx, tx)
	if _, err := opReadTenants(t, ctx, tx, hash, raw, tok, 1, 5); err != nil {
		t.Fatal(err)
	}
	if d := opAudit(t, ctx, tx) - afterForge; d != 0 || afterForge-before != 1 {
		t.Errorf("op_read_tenants wrote %d audit row(s), want 0 (the one row of a read is phase one's)", d)
	}
}

// TestOpReadTenants_LikeMetacharactersAreLiteral: the name match is strpos, not LIKE, so
// '%', '_' and '\' are characters like any other: each term finds the names that CONTAIN
// it and no other. The fixture DISTINGUISHES the two (computed here, so the control is not
// an assumption): for each term the database's own unescaped ILIKE '%' || term || '%'
// over the same four names answers differently from strpos -- for '%' and '_' it matches
// all four (the plain name heads the list, so it would be on the page), for '\' it
// matches none (the backslash escapes the closing '%', so the pattern wants a name
// ENDING in '%'), so the name containing the backslash would be missing.
func TestOpReadTenants_LikeMetacharactersAreLiteral(t *testing.T) {
	ctx, tx := opTx(t)
	tok := opToken(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	names := map[string]string{
		"%":  tok + " 100% halal",
		"_":  tok + " under_score",
		`\`:  tok + ` back\slash`,
		"--": tok + " plain",
	}
	ids := map[string]uuid.UUID{}
	ahead := 4 * time.Second
	for _, k := range []string{"--", "%", "_", `\`} { // the plain name newest
		ids[names[k]] = opNewTenant(t, ctx, tx, names[k], ahead)
		ahead -= time.Second
	}
	all := []string{names["%"], names["_"], names[`\`], names["--"]}
	for _, meta := range []string{"%", "_", `\`} {
		var viaLike, viaStrpos int64
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE n ILIKE '%' || $2 || '%'),
		                                   count(*) FILTER (WHERE strpos(lower(n), lower($2)) > 0)
		                              FROM unnest($1::text[]) AS n`, all, meta).Scan(&viaLike, &viaStrpos); err != nil {
			t.Fatal(err)
		}
		if viaLike == viaStrpos || viaStrpos != 1 {
			t.Fatalf("CONTROL: for %q ILIKE matches %d of the fixture names and strpos %d; want strpos 1 and ILIKE different", meta, viaLike, viaStrpos)
		}
		rows, err := opReadTenants(t, ctx, tx, hash, opForgeTenants(t, ctx, tx, session, a.id, meta, 1, 200, xact), meta, 1, 200)
		if err != nil {
			t.Fatalf("op_read_tenants %q: %v", meta, err)
		}
		found := false
		for _, r := range rows {
			if !strings.Contains(r.Name, meta) {
				t.Errorf("the term %q returned %q, which does not contain it", meta, r.Name)
			}
			found = found || r.ID == ids[names[meta]]
		}
		if !found {
			t.Errorf("the term %q did not find %q", meta, names[meta])
		}
	}
}

// TestOpReadTenants_PagesAreCappedAndOrdered: ADR 0021 §2 iii's ceiling lives in the
// read's body -- a ticket for 300 rows (which op_begin_read would refuse, so the owner
// writes it) reads 200 of the 210 fixtures; the order is created_at DESC, id DESC (two
// fixtures share a created_at and are ordered by id); OFFSET follows the page number, so
// page 2 starts where page 1 ended.
func TestOpReadTenants_PagesAreCappedAndOrdered(t *testing.T) {
	ctx, tx := opTx(t)
	tok := opToken(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	if _, err := tx.Exec(ctx, `
		INSERT INTO tenants (name, vat_number, business_type, structure, created_at)
		SELECT $1 || ' #' || g, 'VAT-OP11-' || $1 || '-' || g, 'cafe', 'single',
		       clock_timestamp() + interval '1 day' + make_interval(secs => g)
		  FROM generate_series(1, 208) AS g`, tok); err != nil {
		t.Fatalf("208 tenants: %v", err)
	}
	// Two more with the SAME created_at, newer than every other fixture.
	var at time.Time
	if err := tx.QueryRow(ctx, `SELECT clock_timestamp() + interval '2 days'`).Scan(&at); err != nil {
		t.Fatal(err)
	}
	twin := []uuid.UUID{uuid.New(), uuid.New()}
	for i, id := range twin {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure, created_at)
		                           VALUES ($1, $2, $3, 'bar', 'single', $4)`, id, fmt.Sprintf("%s twin %d", tok, i), "VAT-OP11-"+id.String(), at); err != nil {
			t.Fatal(err)
		}
	}
	slices.SortFunc(twin, func(x, y uuid.UUID) int { return -strings.Compare(x.String(), y.String()) })
	xact := opCommittedXact(t, ctx)
	read := func(number, size int) []TenantSummary {
		t.Helper()
		raw := opForgeTenants(t, ctx, tx, session, a.id, tok, number, size, xact)
		var rows []TenantSummary
		if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			var e error
			rows, e = readTenants(ctx, sp, hash, readTicket{v: &raw}, TenantListQuery{Search: tok, Number: int32(number), Size: int32(size)})
			return e
		}); err != nil {
			t.Fatalf("read page %d/%d: %v", number, size, err)
		}
		return rows
	}
	if got := len(read(1, 300)); got != 200 {
		t.Errorf("a 300-row ticket read %d rows, want the body's ceiling of 200", got)
	}
	first, second := read(1, 3), read(2, 3)
	if len(first) != 3 || len(second) != 3 {
		t.Fatalf("pages of 3 read %d and %d rows", len(first), len(second))
	}
	if first[0].ID != twin[0] || first[1].ID != twin[1] {
		t.Errorf("the two tenants created at the same instant are not first, in id DESC order: %v", opNames(first[:2]))
	}
	all := append(first, second...)
	for i := 1; i < len(all); i++ {
		if all[i].CreatedAt.After(all[i-1].CreatedAt) {
			t.Errorf("row %d is newer than row %d; the list is newest first", i, i-1)
		}
		if all[i].ID == all[i-1].ID {
			t.Errorf("rows %d and %d are the same tenant; page 2 does not start where page 1 ended", i-1, i)
		}
	}
	if got := read(71, 3); len(got) != 0 {
		t.Errorf("page 71 of 3 (past the 210 fixtures) read %d rows", len(got))
	}
}

// TestOpReadTenantDetail_CountsOnlyTheNamedTenantsRows is the belt test ADR 0021 §6
// ("kemer") asks of a tenant-naming op_*: no row level security applies inside it, so the
// tenant filter in each of its five queries of tenant data (the row and its four counts)
// is the ONLY barrier. Two tenants of
// different sizes, each with rows of every status the counts must skip; the overview of
// each is exactly its own (a count without its filter would add the other tenant's rows
// -- and every other tenant's in the database). An unknown id reads ZERO rows, which is
// not a refusal (28000) and not an argument error (22023): "no such tenant" is told apart.
func TestOpReadTenantDetail_CountsOnlyTheNamedTenantsRows(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	tok := opToken(t)
	one := &opFixtureTenant{name: "Small " + tok, locations: 2, activeEmp: 3, invitedEmp: 1, deactivated: 1,
		activeTags: 2, retiredTags: 1, spareTags: 1, activeAdmins: 2, disabledAdmins: 1}
	two := &opFixtureTenant{name: "Large " + tok, locations: 3, activeEmp: 5, invitedEmp: 2, deactivated: 2,
		activeTags: 4, retiredTags: 2, spareTags: 3, activeAdmins: 1, disabledAdmins: 2}
	for _, f := range []*opFixtureTenant{one, two} {
		f.id = opNewTenant(t, ctx, tx, f.name, 0)
		opPopulate(t, ctx, tx, f)
	}
	for _, f := range []*opFixtureTenant{one, two} {
		rows, err := opReadDetail(t, ctx, tx, hash, opForgeDetail(t, ctx, tx, session, a.id, f.id, xact), f.id)
		if err != nil {
			t.Fatalf("op_read_tenant_detail %s: %v", f.name, err)
		}
		if len(rows) != 1 {
			t.Fatalf("op_read_tenant_detail %s read %d rows, want 1", f.name, len(rows))
		}
		o := rows[0]
		if o.ID != f.id || o.Name != f.name || o.Plan != "standard" || o.BusinessType != "restaurant" ||
			o.CreatedAt.IsZero() {
			t.Errorf("%s: identity %+v", f.name, o)
		}
		if o.Locations != int64(f.locations) || o.ActiveEmployees != int64(f.activeEmp) || o.ActivePlaques != int64(f.activeTags) ||
			o.ActiveAdmins != int64(f.activeAdmins) {
			t.Errorf("%s: counts %d/%d/%d/%d, want %d/%d/%d/%d (locations, active employees, active plaques, active admins)",
				f.name, o.Locations, o.ActiveEmployees, o.ActivePlaques, o.ActiveAdmins, f.locations, f.activeEmp, f.activeTags, f.activeAdmins)
		}
	}
	unknown := uuid.New()
	rows, err := opReadDetail(t, ctx, tx, hash, opForgeDetail(t, ctx, tx, session, a.id, unknown, xact), unknown)
	if err != nil || len(rows) != 0 {
		t.Errorf("an unknown tenant: %d row(s), err %v; want 0 rows and no error", len(rows), err)
	}
	// Through the accessor's own scan: ErrNoSuchTenant, distinct from the refusal.
	raw := opForgeDetail(t, ctx, tx, session, a.id, unknown, xact)
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		_, e := readTenantDetail(ctx, sp, hash, readTicket{v: &raw}, unknown)
		if !errors.Is(e, ErrNoSuchTenant) || errors.Is(e, ErrOperatorRefused) {
			return fmt.Errorf("readTenantDetail of an unknown tenant: %v, want ErrNoSuchTenant", e)
		}
		return nil
	}); err != nil {
		t.Error(err)
	}
}

// TestOpReadTenants_ATicketFromThisTransactionIsRefused is ADR 0021 §2 v 3's rows A1/A2
// for both reads: a ticket op_begin_read made in THIS transaction -- at the top level or
// in a savepoint -- is refused (28000), because its created_xact is the top-level id and
// that transaction has not committed. CONTROL: the same session, kind and parameters
// through a ticket whose transaction committed are read.
func TestOpReadTenants_ATicketFromThisTransactionIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	tenant := uuid.New()
	readBoth := func(q pgx.Tx, listTicket, detailTicket string) (error, error) {
		t.Helper()
		_, e1 := opReadTenants(t, ctx, q, hash, listTicket, "x", 1, 5)
		_, e2 := opReadDetail(t, ctx, q, hash, detailTicket, tenant)
		return e1, e2
	}
	begin := func(q pgx.Tx) (string, string) {
		t.Helper()
		l, err := opBegin(t, ctx, q, hash, tenantsReadKind, opTenantsParams("x", 1, 5))
		if err != nil {
			t.Fatal(err)
		}
		d, err := opBegin(t, ctx, q, hash, tenantDetailReadKind, opDetailParams(tenant.String()))
		if err != nil {
			t.Fatal(err)
		}
		return l, d
	}
	l, d := begin(tx)
	e1, e2 := readBoth(tx, l, d)
	opWantClean(t, e1, sqlstateInvalidAuthorization, tenantsRefusal, "A1: op_read_tenants, a ticket of this transaction's top level", l)
	opWantClean(t, e2, sqlstateInvalidAuthorization, tenantDetailRefusal, "A1: op_read_tenant_detail, a ticket of this transaction's top level", d)
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	l, d = begin(sp)
	e1, e2 = readBoth(sp, l, d)
	opWantClean(t, e1, sqlstateInvalidAuthorization, tenantsRefusal, "A2: op_read_tenants, a ticket of an open savepoint", l)
	opWantClean(t, e2, sqlstateInvalidAuthorization, tenantDetailRefusal, "A2: op_read_tenant_detail, a ticket of an open savepoint", d)
	if err := sp.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	e1, e2 = readBoth(tx, l, d)
	opWantClean(t, e1, sqlstateInvalidAuthorization, tenantsRefusal, "A2: op_read_tenants, a ticket of a released savepoint", l)
	opWantClean(t, e2, sqlstateInvalidAuthorization, tenantDetailRefusal, "A2: op_read_tenant_detail, a ticket of a released savepoint", d)

	xact := opCommittedXact(t, ctx)
	e1, e2 = readBoth(tx, opForgeTenants(t, ctx, tx, session, a.id, "x", 1, 5, xact), opForgeDetail(t, ctx, tx, session, a.id, tenant, xact))
	if e1 != nil || e2 != nil {
		t.Fatalf("CONTROL: tickets whose transaction COMMITTED were refused: %v / %v", e1, e2)
	}
}

// TestOpReadTenants_AForgedTicketIsRefused: a ticket is bound to its session, its KIND and
// every parameter, for both reads. Refused (28000, ticket not consumed): another session
// of the same operator; another term, another page; another tenant; a ticket no
// op_begin_read issued, and NULL; and -- the kind
// condition's own case, now that three kinds exist -- a ticket whose HASH is exactly what
// the read computes but whose kind is the other read's (a 'tenant_detail' ticket shown to
// the list, a 'tenants' ticket shown to the overview). CONTROL: the same hash under the
// right kind is read.
func TestOpReadTenants_AForgedTicketIsRefused(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	otherHash, _ := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	tenant, other := uuid.New(), uuid.New()
	consumed := func() int64 {
		return opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, session)
	}
	before := consumed()

	list := opForgeTenants(t, ctx, tx, session, a.id, "term", 1, 5, xact)
	for _, c := range []struct {
		what, hash, query string
		number, size      int
	}{
		{"another session", otherHash, "term", 1, 5},
		{"another term", hash, "terms", 1, 5},
		{"another page_number", hash, "term", 2, 5},
		{"another page_size", hash, "term", 1, 6},
	} {
		_, err := opReadTenants(t, ctx, tx, c.hash, list, c.query, c.number, c.size)
		opWantClean(t, err, sqlstateInvalidAuthorization, tenantsRefusal, "op_read_tenants, "+c.what, list)
	}
	detail := opForgeDetail(t, ctx, tx, session, a.id, tenant, xact)
	_, err := opReadDetail(t, ctx, tx, otherHash, detail, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantDetailRefusal, "op_read_tenant_detail, another session", detail)
	_, err = opReadDetail(t, ctx, tx, hash, detail, other)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantDetailRefusal, "op_read_tenant_detail, another tenant", detail)
	// No ticket at all: a value no op_begin_read issued, and NULL.
	for _, ticket := range []any{opRandHex(t), nil} {
		err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			_, e := opScanTenants(ctx, sp, hash, ticket, "term", 1, 5)
			return e
		})
		opWantClean(t, err, sqlstateInvalidAuthorization, tenantsRefusal, fmt.Sprintf("op_read_tenants with a ticket never issued (%T)", ticket))
		err = opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			_, e := opScanDetail(ctx, sp, hash, ticket, tenant)
			return e
		})
		opWantClean(t, err, sqlstateInvalidAuthorization, tenantDetailRefusal, fmt.Sprintf("op_read_tenant_detail with a ticket never issued (%T)", ticket))
	}
	if n := consumed() - before; n != 0 {
		t.Errorf("%d ticket(s) consumed by refused reads", n)
	}

	// Kind confusion: the hash each read computes, under the OTHER read's kind.
	raw := opRandHex(t)
	opForgeRead(t, ctx, tx, session, a.id, tenantDetailReadKind, opTenantsTicketHash(raw, "term", 1, 5), nil, xact, "30 seconds")
	_, err = opReadTenants(t, ctx, tx, hash, raw, "term", 1, 5)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantsRefusal, "op_read_tenants, its own hash under kind 'tenant_detail'", raw)
	raw2 := opRandHex(t)
	opForgeRead(t, ctx, tx, session, a.id, tenantsReadKind, opDetailTicketHash(raw2, tenant), &tenant, xact, "30 seconds")
	_, err = opReadDetail(t, ctx, tx, hash, raw2, tenant)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantDetailRefusal, "op_read_tenant_detail, its own hash under kind 'tenants'", raw2)
	raw3 := opRandHex(t)
	opForgeRead(t, ctx, tx, session, a.id, legalVersionsReadKind, opTenantsTicketHash(raw3, "term", 1, 5), nil, xact, "30 seconds")
	_, err = opReadTenants(t, ctx, tx, hash, raw3, "term", 1, 5)
	opWantClean(t, err, sqlstateInvalidAuthorization, tenantsRefusal, "op_read_tenants, its own hash under kind 'legal_versions'", raw3)
	// CONTROL: the same shapes under the right kind.
	raw4 := opRandHex(t)
	opForgeRead(t, ctx, tx, session, a.id, tenantsReadKind, opTenantsTicketHash(raw4, "term", 1, 5), nil, xact, "30 seconds")
	if _, err := opReadTenants(t, ctx, tx, hash, raw4, "term", 1, 5); err != nil {
		t.Errorf("CONTROL: the list's hash under kind 'tenants' was refused: %v", err)
	}
	if _, err := opReadDetail(t, ctx, tx, hash, detail, tenant); err != nil {
		t.Errorf("CONTROL: the overview's ticket with its own session and tenant was refused: %v", err)
	}
}

// TestOpReadTenants_RefusesEveryDeadSession: both reads resolve the session through
// op_touch_session before anything else, so each of the six dead sessions is the touch
// refusal (28000) and no ticket is consumed -- even a ticket that would otherwise match.
func TestOpReadTenants_RefusesEveryDeadSession(t *testing.T) {
	ctx, tx := opTx(t)
	xact := opCommittedXact(t, ctx)
	tenant := uuid.New()
	for _, d := range opDeadSessions(t, ctx, tx) {
		if d.id == uuid.Nil {
			_, err := opReadTenants(t, ctx, tx, d.hash, opRandHex(t), "", 1, 5)
			opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_read_tenants, "+d.name, d.hash)
			_, err = opReadDetail(t, ctx, tx, d.hash, opRandHex(t), tenant)
			opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_read_tenant_detail, "+d.name, d.hash)
			continue
		}
		var admin uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT admin_id FROM platform_sessions WHERE id = $1`, d.id).Scan(&admin); err != nil {
			t.Fatal(err)
		}
		list := opForgeTenants(t, ctx, tx, d.id, admin, "", 1, 5, xact)
		detail := opForgeDetail(t, ctx, tx, d.id, admin, tenant, xact)
		_, err := opReadTenants(t, ctx, tx, d.hash, list, "", 1, 5)
		opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_read_tenants, "+d.name, d.hash, list)
		_, err = opReadDetail(t, ctx, tx, d.hash, detail, tenant)
		opWantClean(t, err, sqlstateInvalidAuthorization, touchRefusal00026, "op_read_tenant_detail, "+d.name, d.hash, detail)
		if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, d.id); n != 0 {
			t.Errorf("%s: %d ticket(s) consumed", d.name, n)
		}
	}
}

// TestOpReadTenants_ExpiryIsTheWallClock is ADR 0021 §6 "bilet suresi" for both reads: an
// expired ticket is refused, and a ticket that expires WHILE the reading transaction is
// open is refused -- through savepoints rolled back three times, and through three
// exception sub-transactions of ONE DO statement whose sleep is INSIDE it (so a frozen
// statement_timestamp() would still call the ticket alive). CONTROL first: the same
// tickets, unexpired, are read (in a savepoint that is rolled back, so they stay
// unconsumed).
func TestOpReadTenants_ExpiryIsTheWallClock(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	hash, session := opNewSession(t, ctx, tx, a.id, true)
	xact := opCommittedXact(t, ctx)
	tenant := uuid.New()
	list := opForgeTenants(t, ctx, tx, session, a.id, "t", 1, 5, xact)
	detail := opForgeDetail(t, ctx, tx, session, a.id, tenant, xact)
	expireIn := func(interval string) {
		t.Helper()
		if _, err := tx.Exec(ctx, `UPDATE operator_read_tickets SET expires_at = clock_timestamp() + $2::interval WHERE session_id = $1`, session, interval); err != nil {
			t.Fatalf("set the expiry: %v", err)
		}
	}
	readThenUndo := func() (error, error) {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, e1 := opReadTenants(t, ctx, sp, hash, list, "t", 1, 5)
		_, e2 := opReadDetail(t, ctx, sp, hash, detail, tenant)
		if err := sp.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return e1, e2
	}
	if e1, e2 := readThenUndo(); e1 != nil || e2 != nil {
		t.Fatalf("CONTROL: the unexpired tickets were refused: %v / %v", e1, e2)
	}
	expireIn("-1 second")
	e1, e2 := readThenUndo()
	opWantClean(t, e1, sqlstateInvalidAuthorization, tenantsRefusal, "op_read_tenants, an expired ticket", list)
	opWantClean(t, e2, sqlstateInvalidAuthorization, tenantDetailRefusal, "op_read_tenant_detail, an expired ticket", detail)

	expireIn("1 second")
	if _, err := tx.Exec(ctx, `SELECT pg_sleep(1.5)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		e1, e2 := readThenUndo()
		opWantClean(t, e1, sqlstateInvalidAuthorization, tenantsRefusal, fmt.Sprintf("op_read_tenants, savepoint %d after the ticket expired in the open transaction", i+1), list)
		opWantClean(t, e2, sqlstateInvalidAuthorization, tenantDetailRefusal, fmt.Sprintf("op_read_tenant_detail, savepoint %d after the ticket expired in the open transaction", i+1), detail)
	}

	expireIn("1 second")
	var result string
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		if _, err := sp.Exec(ctx, `SELECT set_config('tappa_test.session', $1, true), set_config('tappa_test.list', $2, true),
		                                  set_config('tappa_test.detail', $3, true), set_config('tappa_test.tenant', $4, true)`,
			hash, list, detail, tenant.String()); err != nil {
			return err
		}
		if _, err := sp.Exec(ctx, `
			DO $d$
			DECLARE
			    n_data    integer := 0;
			    n_refused integer := 0;
			BEGIN
			    PERFORM pg_sleep(1.5);
			    FOR i IN 1..3 LOOP
			        BEGIN
			            PERFORM * FROM public.op_read_tenants(current_setting('tappa_test.session'),
			                                                  current_setting('tappa_test.list'), 't', 1, 5);
			            n_data := n_data + 1;
			        EXCEPTION WHEN invalid_authorization_specification THEN
			            n_refused := n_refused + 1;
			        END;
			        BEGIN
			            PERFORM * FROM public.op_read_tenant_detail(current_setting('tappa_test.session'),
			                                                        current_setting('tappa_test.detail'),
			                                                        current_setting('tappa_test.tenant')::uuid);
			            n_data := n_data + 1;
			        EXCEPTION WHEN invalid_authorization_specification THEN
			            n_refused := n_refused + 1;
			        END;
			    END LOOP;
			    PERFORM set_config('tappa_test.result', n_data || '/' || n_refused, true);
			END
			$d$`); err != nil {
			return err
		}
		return sp.QueryRow(ctx, `SELECT current_setting('tappa_test.result')`).Scan(&result)
	}); err != nil {
		t.Fatalf("the DO block: %v", err)
	}
	if result != "0/6" {
		t.Errorf("inside one DO statement whose sleep outlived the tickets: data/refused = %s, want 0/6 (a frozen clock reads the tickets alive)", result)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, session); n != 0 {
		t.Errorf("%d expired ticket(s) consumed", n)
	}
}

// --------------------------------------------------------- with real commits --

// TestOpReadTenants_TwoPhaseLifecycle is ADR 0021 §2 v 3's table on the shipped functions
// with REAL commits, for both reads: phase one is committed and its 'read' row is
// permanent; another term, page, tenant or session is refused; the read returns the rows
// (fixtures written inside the read's own transaction, rolled back); a read transaction
// that is ROLLED BACK leaves the row and -- ADR 0021 limit 4 -- lets the same ticket read
// again; once a read has COMMITTED the ticket is refused; and through all of it each read
// has exactly ONE 'read' row (OP-11's "her cagri tam 1 operator audit satiri", read as ADR
// 0021 Sonuclar says: one row per accepted read, written by phase one).
func TestOpReadTenants_TwoPhaseLifecycle(t *testing.T) {
	ctx, f := opLiveFixture(t)
	otherHash, otherSession := f.newSession(t, ctx)
	conn := f.connect(t, ctx)
	tok := opToken(t)
	tenant := uuid.New()

	tx := asOperatorTx(t, ctx, conn)
	var list, detail string
	if err := tx.QueryRow(ctx, beginOperatorReadSQL, f.hash, tenantsReadKind, opTenantsParams(tok, 1, 4)).Scan(&list); err != nil {
		t.Fatalf("phase one (list): %v", err)
	}
	if err := tx.QueryRow(ctx, beginOperatorReadSQL, f.hash, tenantDetailReadKind, opDetailParams(tenant.String())).Scan(&detail); err != nil {
		t.Fatalf("phase one (detail): %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit phase one: %v", err)
	}
	scoped := func(scope string) int64 {
		return opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE kind = 'read' AND session_id = $1 AND target_scope = $2`, f.session, scope)
	}
	if scoped(tenantsReadKind) != 1 || scoped(tenantDetailReadKind) != 1 {
		t.Fatalf("after the committed phase one: %d 'tenants' and %d 'tenant_detail' row(s), want 1 and 1", scoped(tenantsReadKind), scoped(tenantDetailReadKind))
	}

	refused := func(what string, fn func(q pgx.Tx) error, msg, ticket string) {
		t.Helper()
		rtx := asOperatorTx(t, ctx, conn)
		opWantClean(t, fn(rtx), sqlstateInvalidAuthorization, msg, what, ticket)
		if err := rtx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
	}
	listAs := func(hash, query string, number, size int) func(q pgx.Tx) error {
		return func(q pgx.Tx) error { _, e := opScanTenants(ctx, q, hash, list, query, number, size); return e }
	}
	detailAs := func(hash string, id uuid.UUID) func(q pgx.Tx) error {
		return func(q pgx.Tx) error { _, e := opScanDetail(ctx, q, hash, detail, id); return e }
	}
	refused("the list ticket with another term", listAs(f.hash, tok+"x", 1, 4), tenantsRefusal, list)
	refused("the list ticket with another page", listAs(f.hash, tok, 2, 4), tenantsRefusal, list)
	refused("the list ticket from another session of the same operator", listAs(otherHash, tok, 1, 4), tenantsRefusal, list)
	refused("the detail ticket for another tenant", detailAs(f.hash, uuid.New()), tenantDetailRefusal, detail)
	refused("the detail ticket from another session", detailAs(otherHash, tenant), tenantDetailRefusal, detail)

	// THE READS, inside transactions that are rolled back (the fixture tenant is written
	// there too) -- then once each with a COMMIT and no fixture.
	read := func(commit bool) ([]TenantSummary, []TenantOverview) {
		t.Helper()
		rtx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rtx.Rollback(context.Background()) }()
		if !commit {
			for _, s := range []struct {
				sql  string
				args []any
			}{
				{`INSERT INTO tenants (id, name, vat_number, business_type, structure, created_at)
				  VALUES ($1, $2, $3, 'hotel', 'single', clock_timestamp() + interval '1 day')`, []any{tenant, "Lifecycle " + tok, "VAT-OP11-" + tenant.String()}},
				{`INSERT INTO locations (tenant_id, name) VALUES ($1, 'op11 lifecycle venue')`, []any{tenant}},
			} {
				if _, err := rtx.Exec(ctx, s.sql, s.args...); err != nil {
					t.Fatalf("fixture: %v", err)
				}
			}
		}
		if _, err := rtx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
			t.Fatal(err)
		}
		rows, err := opScanTenants(ctx, rtx, f.hash, list, tok, 1, 4)
		if err != nil {
			t.Fatalf("the list read (commit=%v): %v", commit, err)
		}
		overview, err := opScanDetail(ctx, rtx, f.hash, detail, tenant)
		if err != nil {
			t.Fatalf("the detail read (commit=%v): %v", commit, err)
		}
		if commit {
			if err := rtx.Commit(ctx); err != nil {
				t.Fatalf("commit the reads: %v", err)
			}
		}
		return rows, overview
	}
	rows, overview := read(false)
	if len(rows) != 1 || rows[0].ID != tenant || len(overview) != 1 || overview[0].Name != "Lifecycle "+tok || overview[0].Locations != 1 {
		t.Errorf("the reads returned list %v and overview %+v; want the fixture tenant in both, with its one location", opNames(rows), overview)
	}
	if scoped(tenantsReadKind) != 1 || scoped(tenantDetailReadKind) != 1 {
		t.Errorf("after rolled-back reads: %d / %d 'read' rows, want the 1 / 1 committed by phase one", scoped(tenantsReadKind), scoped(tenantDetailReadKind))
	}
	// ADR 0021 limit 4, measured: the rolled-back reads un-consumed the tickets.
	rows, overview = read(true)
	if len(rows) != 0 || len(overview) != 0 {
		t.Errorf("the committed reads (no fixture) returned %d and %d row(s)", len(rows), len(overview))
	}
	if n := opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_read_tickets WHERE session_id = $1 AND consumed_at IS NOT NULL`, f.session); n != 2 {
		t.Errorf("after the committed reads %d ticket(s) of the session are consumed, want 2", n)
	}
	refused("the list ticket after a COMMITTED read consumed it", listAs(f.hash, tok, 1, 4), tenantsRefusal, list)
	refused("the detail ticket after a COMMITTED read consumed it", detailAs(f.hash, tenant), tenantDetailRefusal, detail)
	if scoped(tenantsReadKind) != 1 || scoped(tenantDetailReadKind) != 1 {
		t.Errorf("at the end: %d / %d 'read' rows, want exactly 1 / 1", scoped(tenantsReadKind), scoped(tenantDetailReadKind))
	}
	if n := f.liveReads(t, ctx, otherSession); n != 0 {
		t.Errorf("the other session has %d 'read' row(s)", n)
	}
}

// TestTenantList_OnThePoolTheTwoPhasesAreTwoTransactions: on a pool built by the
// production constructor (*pgxpool.Pool -- what *OperatorDB holds) the accessors' two
// phases are two transactions, and the committed 'read' rows say what was read: the
// list's page and size and the term's class (the term itself nowhere); the overview's
// tenant id -- for an id that names NO tenant, whose answer is ErrNoSuchTenant AFTER that
// row committed. Inside one transaction both accessors are ErrOperatorRefused; an unknown
// session is ErrOperatorRefused and writes nothing. A term with capitals and Maltese
// letters passes BOTH phases -- the accessor sends phase one and phase two the same text
// (a term changed on its way to either would miss the ticket and be refused) -- and no
// error the accessor returns carries the term (a refused session, a refused page, a term
// over the bound). OP-11 phase B: the same two reads through *OperatorDB's METHODS -- the
// ones the /operator/tenants screens call -- on that pool: the list returns, the overview
// of an unknown id is ErrNoSuchTenant, and each commits one more 'read' row. It commits
// five 'read' rows (file header).
func TestTenantList_OnThePoolTheTwoPhasesAreTwoTransactions(t *testing.T) {
	ctx, f := opLiveFixture(t)
	o, err := openOperatorDB(ctx, f.dsn, asOperator)
	if err != nil {
		t.Fatalf("open the operator pool: %v", err)
	}
	defer o.Close()
	tok := opToken(t)

	rows, err := TenantList(ctx, o.pool, f.hash, TenantListQuery{Search: tok, Number: 2, Size: 7})
	if err != nil {
		t.Fatalf("TenantList on the pool: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("a token no tenant carries found %d tenant(s)", len(rows))
	}
	var pageNumber, pageSize int
	var detail, text string
	if err := f.owner.QueryRow(ctx, `SELECT page_number, page_size, detail::text, row_to_json(l)::text FROM operator_audit_log l
	                                  WHERE kind = 'read' AND session_id = $1 AND target_scope = 'tenants'`, f.session).
		Scan(&pageNumber, &pageSize, &detail, &text); err != nil {
		t.Fatalf("read the committed 'tenants' row: %v", err)
	}
	if pageNumber != 2 || pageSize != 7 || detail != `{"search": "text"}` || strings.Contains(text, tok) {
		t.Errorf("the committed 'tenants' row: page %d/%d detail %s term present=%v; want 2/7, {\"search\": \"text\"}, no term",
			pageNumber, pageSize, detail, strings.Contains(text, tok))
	}

	unknown := uuid.New()
	if _, err := TenantDetail(ctx, o.pool, f.hash, unknown); !errors.Is(err, ErrNoSuchTenant) {
		t.Errorf("TenantDetail of an unknown tenant on the pool: %v, want ErrNoSuchTenant", err)
	}
	if n := opInt(t, ctx, f.owner, `SELECT count(*) FROM operator_audit_log WHERE kind = 'read' AND session_id = $1
	                                 AND target_scope = 'tenant_detail' AND target_tenant_id = $2`, f.session, unknown); n != 1 {
		t.Errorf("the overview of an unknown tenant committed %d 'read' row(s) naming it, want 1", n)
	}

	tx := asOperatorTx(t, ctx, f.connect(t, ctx))
	if _, err := TenantList(ctx, tx, f.hash, TenantListQuery{Search: tok, Number: 1, Size: 5}); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("TenantList inside ONE transaction: %v, want ErrOperatorRefused", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	tx = asOperatorTx(t, ctx, f.connect(t, ctx))
	if _, err := TenantDetail(ctx, tx, f.hash, unknown); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("TenantDetail inside ONE transaction: %v, want ErrOperatorRefused", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := TenantList(ctx, o.pool, opRandHex(t), TenantListQuery{Number: 1, Size: 5}); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("TenantList with an unknown session: %v, want ErrOperatorRefused", err)
	}
	if _, err := TenantDetail(ctx, o.pool, opRandHex(t), unknown); !errors.Is(err, ErrOperatorRefused) {
		t.Errorf("TenantDetail with an unknown session: %v, want ErrOperatorRefused", err)
	}
	if n := f.liveReads(t, ctx, f.session); n != 2 {
		t.Errorf("the session has %d committed 'read' row(s), want 2 (one per accepted read; the refused calls none)", n)
	}
	// A page outside the database's bounds: a 22023 database error, not a sentinel, no
	// PgError reachable -- and no row.
	_, err = TenantList(ctx, o.pool, f.hash, TenantListQuery{Number: 1, Size: 201})
	var pg *pgconn.PgError
	if err == nil || errors.Is(err, ErrOperatorRefused) || errors.Is(err, ErrTenantSearchRefused) || errors.As(err, &pg) ||
		!strings.Contains(err.Error(), "SQLSTATE 22023") {
		t.Errorf("a page of 201: %v, want a 22023 database error that is no sentinel and no reachable PgError", err)
	}
	if n := f.liveReads(t, ctx, f.session); n != 2 {
		t.Errorf("the refused page left %d 'read' row(s), want 2", n)
	}

	// Capitals and Maltese letters through BOTH phases, then the error texts.
	mixed := "ĦAMRUN Żebbuġ ĊAFÈ " + strings.ToUpper(tok)
	if _, err := TenantList(ctx, o.pool, f.hash, TenantListQuery{Search: mixed, Number: 1, Size: 5}); err != nil {
		t.Errorf("TenantList with a term of capitals and Maltese letters: %v; both phases must send the same text", err)
	}
	if n := f.liveReads(t, ctx, f.session); n != 3 {
		t.Errorf("the session has %d committed 'read' row(s) after the mixed-case read, want 3", n)
	}
	for _, c := range []struct {
		what string
		call func() error
	}{
		{"an unknown session", func() error {
			_, e := TenantList(ctx, o.pool, opRandHex(t), TenantListQuery{Search: mixed, Number: 1, Size: 5})
			return e
		}},
		{"a page of 201", func() error {
			_, e := TenantList(ctx, o.pool, f.hash, TenantListQuery{Search: mixed, Number: 1, Size: 201})
			return e
		}},
		{"a term over the bound", func() error {
			_, e := TenantList(ctx, o.pool, f.hash, TenantListQuery{Search: mixed + strings.Repeat("ż", MaxTenantSearchRunes), Number: 1, Size: 5})
			return e
		}},
	} {
		err := c.call()
		if err == nil {
			t.Errorf("%s with the mixed term: no error", c.what)
			continue
		}
		for _, part := range []string{mixed, strings.ToLower(mixed), strings.ToUpper(tok), tok} {
			if strings.Contains(err.Error(), part) {
				t.Errorf("%s: the error text carries the term (%d characters of it)", c.what, len(part))
			}
		}
	}
	if n := f.liveReads(t, ctx, f.session); n != 3 {
		t.Errorf("the three refused calls left %d 'read' row(s), want 3", n)
	}

	// Through the methods (OP-11 phase B), on the production-built pool: each is the two
	// phases as two transactions -- inside one, phase two would refuse the ticket and the
	// method would answer ErrOperatorRefused.
	if rows, err := o.TenantList(ctx, f.hash, TenantListQuery{Search: tok, Number: 1, Size: 5}); err != nil || len(rows) != 0 {
		t.Errorf("(*OperatorDB).TenantList on its pool: %d row(s), %v; want none and no error", len(rows), err)
	}
	if n := f.liveReads(t, ctx, f.session); n != 4 {
		t.Errorf("the list method's read left the session with %d committed 'read' row(s), want 4", n)
	}
	if _, err := o.TenantDetail(ctx, f.hash, unknown); !errors.Is(err, ErrNoSuchTenant) {
		t.Errorf("(*OperatorDB).TenantDetail of an unknown tenant on its pool: %v, want ErrNoSuchTenant", err)
	}
	if n := f.liveReads(t, ctx, f.session); n != 5 {
		t.Errorf("the overview method's read left the session with %d committed 'read' row(s), want 5", n)
	}
}
