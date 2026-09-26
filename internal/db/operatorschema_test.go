package db

// operatorschema_test.go -- the CATALOGUE proofs for migration 00026 (M10 OP-5): the
// roles, the grants, the op_* contract read from pg_proc, the frozen-clock scan, the
// append-only audit, the temp-table shadow and the precondition that refuses a cluster
// without the two roles. The behaviour of the five functions is in
// operatorfuncs_test.go; the helpers both files use are here.
//
// ADR 0021 §6 is the normative list; every check below cites the line it pins.
//
// 🔴 EVERYTHING RUNS INSIDE ONE BEGIN ... ROLLBACK PER TEST, as tappa_owner, and
// switches identity with SET LOCAL SESSION AUTHORIZATION inside a savepoint. SET
// ROLE is not used: on a superuser session it is the wrong probe (ADR 0021 "Bağlam",
// the security audit measured it). Both roles are born NOLOGIN (01-roles.sql), and
// tappa_opdefiner stays that way, so impersonation from the owner session is the one
// way to be them that works on every database this suite meets -- and it is exactly
// the identity PostgreSQL checks.
//
// 🔴 THE PROPERTY IS CHECKED, THE LIST IS ONLY A FLOOR. The forward and reverse pins
// walk EVERY function owned by (or named like) the operator surface, so an op_* added
// by OP-10 or later is covered the day it lands without editing this file. The five
// names born in 00026 are an anti-vacuity floor, not the definition of the set.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// SQLSTATEs this file and operatorfuncs_test.go name. 28000 is what every op_* raises
// on refusal (00026's header says why it is that code and not a commoner one).
const (
	sqlstateInvalidAuthorization = "28000" // op_* refusal
	sqlstateInvalidParameter     = "22023" // op_record_auth_event: kind outside the closed set
	sqlstatePrerequisiteState    = "55000" // 00026's precondition: the roles are missing or wrong
	sqlstateNotNullViolation     = "23502"
)

// op00026Functions is the ANTI-VACUITY FLOOR of the catalogue pins: the five born in
// 00026 must be present. The pins themselves read the whole owned set.
var op00026Functions = []string{
	"op_close_session", "op_complete_enrollment", "op_open_session",
	"op_record_auth_event", "op_touch_session",
}

// opSessionless are ADR 0021 §2 (i)'s three named exceptions: no session argument.
var opSessionless = map[string]bool{
	"op_record_auth_event": true, "op_open_session": true, "op_complete_enrollment": true,
}

// opTables are the four tables 00026 creates.
var opTables = []string{"platform_admins", "platform_sessions", "operator_audit_log", "operator_read_tickets"}

// opFrozenClockRE is ADR 0021 §2 (vii)'s ban list, word-bounded (\m \M) so a literal
// like 'now' is caught and not only the call form now(). 'tomorrow' and 'yesterday'
// are derived from the current date the same way 'today' is.
const opFrozenClockRE = `(?i)\m(now|current_timestamp|transaction_timestamp|statement_timestamp|localtimestamp|localtime|current_time|current_date|today|tomorrow|yesterday)\M`

type opQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// ------------------------------------------------------------------ helpers --

// opOwnerDSN is the migration-role URL, or a skip. Test-only owner access: redline R5
// forbids that variable in production code and excludes _test.go.
func opOwnerDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the operator tests impersonate NOLOGIN roles from the owner session (real Postgres required -- CLAUDE.md §8)")
	}
	return dsn
}

// opTx opens a dedicated owner connection and a REPEATABLE READ transaction that is
// rolled back when the test ends. REPEATABLE READ so that every count this suite
// takes sees this transaction's own writes and nothing another session committed in
// the meantime: an audit-row delta of 0 then means "this call wrote nothing".
func opTx(t *testing.T) (context.Context, pgx.Tx) {
	t.Helper()
	dsn := opOwnerDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Logf("close owner connection: %v", err)
		}
	})
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
	// 🔴 ROLLBACK IS THE CONTRACT WITH THE SHARED DATABASE.
	t.Cleanup(func() {
		if err := tx.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Logf("rollback: %v", err)
		}
	})
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '10s'`); err != nil {
		t.Fatalf("set lock_timeout: %v", err)
	}
	return ctx, tx
}

// opAs runs fn as `role` inside a savepoint. On error the savepoint is rolled back --
// which also undoes the SET LOCAL -- and the error is returned for the caller to
// classify. On success the identity is put back BEFORE the savepoint is released,
// because a released SET LOCAL survives into the parent transaction.
func opAs(t *testing.T, ctx context.Context, tx pgx.Tx, role string, fn func(pgx.Tx) error) error {
	t.Helper()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	if _, err := sp.Exec(ctx, "SET LOCAL SESSION AUTHORIZATION "+pgx.Identifier{role}.Sanitize()); err != nil {
		t.Fatalf("become %s: %v", role, err)
	}
	if callErr := fn(sp); callErr != nil {
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("rollback to savepoint: %v", err)
		}
		return callErr
	}
	if _, err := sp.Exec(ctx, "SET LOCAL SESSION AUTHORIZATION DEFAULT"); err != nil {
		t.Fatalf("return to the owner: %v", err)
	}
	if err := sp.Commit(ctx); err != nil {
		t.Fatalf("release savepoint: %v", err)
	}
	return nil
}

// opExecAs is opAs for one statement.
func opExecAs(t *testing.T, ctx context.Context, tx pgx.Tx, role, sql string, args ...any) error {
	t.Helper()
	return opAs(t, ctx, tx, role, func(sp pgx.Tx) error {
		_, err := sp.Exec(ctx, sql, args...)
		return err
	})
}

// opTry runs one statement as the owner inside a savepoint, so a refusal does not
// abort the test's transaction.
func opTry(t *testing.T, ctx context.Context, tx pgx.Tx, sql string, args ...any) error {
	t.Helper()
	sp, err := tx.Begin(ctx)
	if err != nil {
		t.Fatalf("savepoint: %v", err)
	}
	_, execErr := sp.Exec(ctx, sql, args...)
	if execErr != nil {
		if err := sp.Rollback(ctx); err != nil {
			t.Fatalf("rollback to savepoint: %v", err)
		}
		return execErr
	}
	if err := sp.Commit(ctx); err != nil {
		t.Fatalf("release savepoint: %v", err)
	}
	return nil
}

// opCode returns the SQLSTATE and message of a Postgres error, or "" for anything else.
func opCode(err error) (code, msg string) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code, pg.Message
	}
	return "", ""
}

// opWant fails unless err carries exactly this SQLSTATE -- never "some error": a
// refusal by a missing grant (42501) must not satisfy a test that expects the
// function's own refusal (28000), and the other way round (M7-02's lesson).
func opWant(t *testing.T, err error, code, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: SUCCEEDED, want SQLSTATE %s", what, code)
	}
	got, msg := opCode(err)
	if got != code {
		t.Fatalf("%s: SQLSTATE %q (%s), want %s (error: %v)", what, got, msg, code, err)
	}
}

// opInt reads one integer as the owner.
func opInt(t *testing.T, ctx context.Context, q opQuerier, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := q.QueryRow(ctx, sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// opRandHex returns 32 random bytes as 64 lower-case hex characters: the shape of a
// session or ticket hash. Generated, never a literal.
func opRandHex(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

// opRandToken returns a raw enrollment token shaped like the panel's tokens (32
// random bytes, unpadded base64url) and its keyless SHA-256 hex -- the value 00026
// compares, computed over the token's text exactly as op_complete_enrollment does.
func opRandToken(t *testing.T) (raw, hash string) {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	sum := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(sum[:])
}

// opFakeDigest is a bcrypt-SHAPED synthetic value (cost 12, 53-character body) that
// passes 00026's shape CHECK. It is not the digest of anything; it is assembled at
// run time so no digest-shaped literal sits in the source.
func opFakeDigest(fill string) string { return "$2a$12$" + strings.Repeat(fill, 53) }

// opFakeSealed is a synthetic 48-byte "envelope" (00026 bounds it below at 44).
func opFakeSealed(fill byte) []byte {
	b := make([]byte, 48)
	for i := range b {
		b[i] = fill
	}
	return b
}

// opUpDown returns the Up and Down sections of 00026 as goose would run them.
func opUpDown(t *testing.T) (up, down string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "00026_create_platform_operator.sql"))
	if err != nil {
		t.Fatalf("read 00026: %v", err)
	}
	src := string(b)
	iu := strings.Index(src, "-- +goose Up")
	id := strings.Index(src, "-- +goose Down")
	if iu < 0 || id < 0 || id < iu {
		t.Fatalf("00026 does not carry the goose Up/Down markers in order (up=%d down=%d)", iu, id)
	}
	return src[iu:id], src[id:]
}

// ------------------------------------------------------ forward catalogue pin --

// opForwardFindings reads EVERY function owned by tappa_opdefiner and returns one line
// per breach of ADR 0021 §6 "Katalog — ileri yön", plus the names it looked at.
//
// It is a function so that the positive controls below drive THIS code rather than a
// copy of it (rlsforce_test.go's argument).
func opForwardFindings(ctx context.Context, q opQuerier) (findings, names []string, err error) {
	rows, err := q.Query(ctx, `
		SELECT p.proname,
		       n.nspname,
		       p.oid::regprocedure::text,
		       p.prosecdef,
		       p.provolatile::text,
		       coalesce(array_to_string(p.proconfig, '|'), ''),
		       has_function_privilege('public', p.oid, 'EXECUTE'),
		       has_function_privilege('tappa_app', p.oid, 'EXECUTE'),
		       has_function_privilege('tappa_operator', p.oid, 'EXECUTE'),
		       pg_get_function_identity_arguments(p.oid),
		       p.prorettype::regtype::text,
		       p.proretset
		  FROM pg_proc p
		  JOIN pg_namespace n ON n.oid = p.pronamespace
		 WHERE p.proowner = 'tappa_opdefiner'::regrole
		 ORDER BY 1`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, schema, sig, vol, config, args, ret string
		var secdef, public, app, operator, retset bool
		if err := rows.Scan(&name, &schema, &sig, &secdef, &vol, &config, &public, &app, &operator,
			&args, &ret, &retset); err != nil {
			return nil, nil, err
		}
		names = append(names, name)
		bad := func(f string, a ...any) { findings = append(findings, sig+": "+fmt.Sprintf(f, a...)) }
		if schema != "public" || !strings.HasPrefix(name, "op_") {
			bad("owned by tappa_opdefiner but not named public.op_* (ADR 0021: every tenant-crossing path is an op_*)")
		}
		if !secdef {
			bad("not SECURITY DEFINER")
		}
		if vol != "v" {
			bad("provolatile=%s, want v (every op_* writes; §2 v 8)", vol)
		}
		if config != "search_path=pg_catalog, pg_temp" {
			bad("proconfig=%q, want exactly search_path=pg_catalog, pg_temp (§2 iv)", config)
		}
		if public {
			bad("PUBLIC may EXECUTE it (REVOKE ALL ... FROM PUBLIC missing)")
		}
		if app {
			bad("tappa_app may EXECUTE it (ADR 0021 §1: tappa_app gets no new privilege)")
		}
		if !operator {
			bad("tappa_operator may NOT execute it")
		}
		argv := strings.Split(args, ", ")
		if !opSessionless[name] && (len(argv) == 0 || argv[0] != "p_session text") {
			bad("first argument is %q, want \"p_session text\" (§2 i; only the three named exceptions may omit it)", args)
		}
		if strings.HasPrefix(name, "op_read_") && (len(argv) < 2 || argv[1] != "p_ticket text") {
			bad("an op_read_* must take the RAW ticket as its second argument (\"p_ticket text\"), got %q (§2 v 2)", args)
		}
		exempt := strings.HasPrefix(name, "op_read_") || name == "op_begin_read" || name == "op_touch_session"
		if !exempt && (retset || (ret != "void" && ret != "uuid")) {
			bad("returns %s (setof=%v); a write returns void or the uuid it created (§2 v 7, the return pin)", ret, retset)
		}
	}
	return findings, names, rows.Err()
}

// TestOperator00026_ForwardCatalogPin is ADR 0021 §6 "Katalog — ileri yön" on the
// live catalogue, with a positive control per rule it could stop enforcing.
func TestOperator00026_ForwardCatalogPin(t *testing.T) {
	ctx, tx := opTx(t)

	findings, names, err := opForwardFindings(ctx, tx)
	if err != nil {
		t.Fatalf("forward scan: %v", err)
	}
	for _, f := range findings {
		t.Error(f)
	}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, want := range op00026Functions {
		if !have[want] {
			t.Errorf("anti-vacuity: %s is not owned by tappa_opdefiner (owned set: %v)", want, names)
		}
	}

	// POSITIVE CONTROLS: each probe is a function that breaks ONE rule, created and
	// owned by tappa_opdefiner inside a savepoint that is rolled back. The scan must
	// name it; a scan that stays silent here is a scan that would stay silent for a
	// real regression.
	probes := []struct {
		name, ddl, grants, wantFragment string
	}{
		{"search_path with public", `CREATE FUNCTION public.op_zz_probe(p_session text) RETURNS void
			LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, public AS 'SELECT 1'`,
			`GRANT EXECUTE ON FUNCTION public.op_zz_probe(text) TO tappa_operator`, "proconfig="},
		{"PUBLIC keeps EXECUTE", `CREATE FUNCTION public.op_zz_probe(p_session text) RETURNS void
			LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS 'SELECT 1'`,
			``, "PUBLIC may EXECUTE"},
		{"tappa_app granted", `CREATE FUNCTION public.op_zz_probe(p_session text) RETURNS void
			LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS 'SELECT 1'`,
			`REVOKE ALL ON FUNCTION public.op_zz_probe(text) FROM PUBLIC;
			 GRANT EXECUTE ON FUNCTION public.op_zz_probe(text) TO tappa_operator, tappa_app`, "tappa_app may EXECUTE"},
		{"no session argument", `CREATE FUNCTION public.op_zz_probe(p_other uuid) RETURNS void
			LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS 'SELECT 1'`,
			`REVOKE ALL ON FUNCTION public.op_zz_probe(uuid) FROM PUBLIC;
			 GRANT EXECUTE ON FUNCTION public.op_zz_probe(uuid) TO tappa_operator`, "first argument"},
		{"a write that returns a count", `CREATE FUNCTION public.op_zz_probe(p_session text) RETURNS bigint
			LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS 'SELECT 1::bigint'`,
			`REVOKE ALL ON FUNCTION public.op_zz_probe(text) FROM PUBLIC;
			 GRANT EXECUTE ON FUNCTION public.op_zz_probe(text) TO tappa_operator`, "return pin"},
		{"STABLE", `CREATE FUNCTION public.op_zz_probe(p_session text) RETURNS void
			LANGUAGE sql STABLE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS 'SELECT 1'`,
			`REVOKE ALL ON FUNCTION public.op_zz_probe(text) FROM PUBLIC;
			 GRANT EXECUTE ON FUNCTION public.op_zz_probe(text) TO tappa_operator`, "provolatile="},
		{"op_read_ without a raw ticket", `CREATE FUNCTION public.op_read_zz_probe(p_session text, p_page int) RETURNS TABLE (x int)
			LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS 'SELECT 1'`,
			`REVOKE ALL ON FUNCTION public.op_read_zz_probe(text, int) FROM PUBLIC;
			 GRANT EXECUTE ON FUNCTION public.op_read_zz_probe(text, int) TO tappa_operator`, "RAW ticket"},
	}
	for _, p := range probes {
		t.Run("control/"+p.name, func(t *testing.T) {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatalf("savepoint: %v", err)
			}
			defer func() {
				if err := sp.Rollback(ctx); err != nil {
					t.Fatalf("rollback probe: %v", err)
				}
			}()
			if _, err := sp.Exec(ctx, p.ddl); err != nil {
				t.Fatalf("create probe: %v", err)
			}
			sig := regexp.MustCompile(`public\.(op_[a-z_]+)\(([^)]*)\)`).FindStringSubmatch(p.ddl)
			argTypes := regexp.MustCompile(`p_[a-z]+ `).ReplaceAllString(sig[2], "")
			if _, err := sp.Exec(ctx, fmt.Sprintf(`ALTER FUNCTION public.%s(%s) OWNER TO tappa_opdefiner`, sig[1], argTypes)); err != nil {
				t.Fatalf("own probe: %v", err)
			}
			if p.grants != "" {
				if _, err := sp.Exec(ctx, p.grants); err != nil {
					t.Fatalf("grant probe: %v", err)
				}
			}
			got, _, err := opForwardFindings(ctx, sp)
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			hit := false
			for _, f := range got {
				if strings.Contains(f, "zz_probe") && strings.Contains(f, p.wantFragment) {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("the forward pin did not flag a probe that breaks %q; findings: %v", p.name, got)
			}
		})
	}
}

// ------------------------------------------------------ reverse catalogue pin --

// opReverseFindings is ADR 0021 §6 "Katalog — TERS yön": the direction the forward pin
// cannot see. A forgotten `ALTER FUNCTION ... OWNER TO tappa_opdefiner` leaves an op_*
// owned by the SUPERUSER migration role -- a total bypass -- and the forward pin, which
// walks what tappa_opdefiner owns, never looks at it.
func opReverseFindings(ctx context.Context, q opQuerier) ([]string, error) {
	var out []string
	collect := func(sql, label string) error {
		rows, err := q.Query(ctx, sql)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var what string
			if err := rows.Scan(&what); err != nil {
				return err
			}
			out = append(out, label+": "+what)
		}
		return rows.Err()
	}
	checks := []struct{ label, sql string }{
		{"an op_* not owned by tappa_opdefiner", `
			SELECT p.oid::regprocedure::text || ' owned by ' || pg_get_userbyid(p.proowner)
			  FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
			 WHERE n.nspname = 'public' AND p.proname LIKE 'op\_%'
			   AND p.proowner <> 'tappa_opdefiner'::regrole`},
		{"a SECURITY DEFINER owned outside {tappa_resolver, tappa_opdefiner} or by a superuser", `
			SELECT p.oid::regprocedure::text || ' owned by ' || r.rolname || ' rolsuper=' || r.rolsuper
			  FROM pg_proc p JOIN pg_roles r ON r.oid = p.proowner
			 WHERE p.prosecdef
			   AND (r.rolname NOT IN ('tappa_resolver', 'tappa_opdefiner') OR r.rolsuper)`},
		{"tappa_opdefiner has a member (one SET ROLE from BYPASSRLS)", `
			SELECT pg_get_userbyid(m.member) FROM pg_auth_members m
			 WHERE m.roleid = 'tappa_opdefiner'::regrole`},
		{"tappa_operator is a member of a role (ADR 0021 §1: member of nothing)", `
			SELECT pg_get_userbyid(m.roleid) FROM pg_auth_members m
			 WHERE m.member = 'tappa_operator'::regrole`},
		{"tappa_opdefiner is a member of a role (its op_* would inherit that role's reach)", `
			SELECT pg_get_userbyid(m.roleid) FROM pg_auth_members m
			 WHERE m.member = 'tappa_opdefiner'::regrole`},
		{"tappa_operator has a member (it would inherit EXECUTE on every op_*)", `
			SELECT pg_get_userbyid(m.member) FROM pg_auth_members m
			 WHERE m.roleid = 'tappa_operator'::regrole`},
		// S-1 (round 4): a ROLE-LEVEL setting on an operator role. The motivating case,
		// measured: log_parameter_max_length_on_error is a USER-context setting, and
		// tappa_operator itself can run ALTER ROLE tappa_operator SET ... = -1 -- after
		// which every rejected op_* call puts its bound parameters (raw enrollment token,
		// session hash, digest) into the server log and the caller's CONTEXT, on every
		// new pooled connection, surviving a password rotation. This catches such a row
		// on the databases the suite runs against; OP-7's connection-startup pin is the
		// runtime guard. ⚠️ ADR 0021 "Karar verilmedi" keeps a defence-in-depth option
		// that IS such a row (ALTER ROLE tappa_operator SET log_statement = 'all' plus
		// log_parameter_max_length = 0); if it is adopted, this check and the runbook's
		// step 2 change in the same change set and name the allowed row(s).
		{"an operator role carries a role-level setting (pg_db_role_setting)", `
			SELECT pg_get_userbyid(s.setrole) || ' ' || array_to_string(s.setconfig, ',')
			  FROM pg_db_role_setting s
			 WHERE s.setrole IN ('tappa_operator'::regrole, 'tappa_opdefiner'::regrole)`},
		{"a role has the wrong attributes (ADR 0021 §1)", `
			SELECT rolname || ' super=' || rolsuper || ' bypassrls=' || rolbypassrls || ' login=' || rolcanlogin
			  FROM pg_roles
			 WHERE (rolname = 'tappa_opdefiner' AND (rolsuper OR NOT rolbypassrls OR rolcanlogin OR rolcreaterole OR rolcreatedb))
			    OR (rolname = 'tappa_operator'  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb))`},
		{"a default privilege names an operator role (ADR 0021 §1: none)", `
			SELECT pg_get_userbyid(d.defaclrole) || ' ' || d.defaclobjtype::text || ' ' || d.defaclacl::text
			  FROM pg_default_acl d
			 WHERE d.defaclrole IN ('tappa_operator'::regrole, 'tappa_opdefiner'::regrole)
			    OR d.defaclacl::text ~ '(^|[{,])tappa_op(erator|definer)='`},
	}
	for _, c := range checks {
		if err := collect(c.sql, c.label); err != nil {
			return nil, fmt.Errorf("%s: %w", c.label, err)
		}
	}
	return out, nil
}

// TestOperator00026_ReverseCatalogPin: every op_* belongs to tappa_opdefiner, every
// definer in the database belongs to one of the two non-superuser definer roles,
// tappa_opdefiner has no member and tappa_operator is a member of nothing.
func TestOperator00026_ReverseCatalogPin(t *testing.T) {
	ctx, tx := opTx(t)

	findings, err := opReverseFindings(ctx, tx)
	if err != nil {
		t.Fatalf("reverse scan: %v", err)
	}
	for _, f := range findings {
		t.Error(f)
	}
	// ANTI-VACUITY: the SECURITY DEFINER set is not empty -- otherwise the second
	// check passes on a database nobody looked at.
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE prosecdef`); n < int64(len(op00026Functions))+6 {
		t.Fatalf("only %d SECURITY DEFINER functions in the catalogue; the six resolvers and the five op_* should all be there", n)
	}

	controls := []struct{ name, ddl, want string }{
		// The forgotten OWNER TO: owned by the superuser that ran the migration.
		{"op_* left with the migration role", `CREATE FUNCTION public.op_zz_probe(p_session text) RETURNS void
			LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS 'SELECT 1'`,
			"not owned by tappa_opdefiner"},
		{"a definer outside the two roles", `CREATE FUNCTION public.zz_probe_definer() RETURNS void
			LANGUAGE sql VOLATILE SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS 'SELECT 1'`,
			"SECURITY DEFINER owned outside"},
		{"a member of tappa_opdefiner", `GRANT tappa_opdefiner TO tappa_app`, "has a member"},
		{"tappa_operator made a member", `GRANT tappa_resolver TO tappa_operator`, "tappa_operator is a member of a role"},
		{"tappa_opdefiner made a member of the owner", `GRANT tappa_owner TO tappa_opdefiner`, "tappa_opdefiner is a member of a role"},
		{"tappa_app made a member of tappa_operator", `GRANT tappa_operator TO tappa_app`, "tappa_operator has a member"},
		{"tappa_operator sets its own log_parameter_max_length_on_error", `ALTER ROLE tappa_operator SET log_parameter_max_length_on_error = -1`, "role-level setting"},
		{"a role-level setting on tappa_opdefiner", `ALTER ROLE tappa_opdefiner SET search_path = public`, "role-level setting"},
		{"a superuser tappa_opdefiner", `ALTER ROLE tappa_opdefiner SUPERUSER`, "wrong attributes"},
		{"a default privilege for the operator", `ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
			GRANT SELECT ON TABLES TO tappa_operator`, "default privilege"},
	}
	for _, c := range controls {
		t.Run("control/"+c.name, func(t *testing.T) {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatalf("savepoint: %v", err)
			}
			defer func() {
				if err := sp.Rollback(ctx); err != nil {
					t.Fatalf("rollback probe: %v", err)
				}
			}()
			if _, err := sp.Exec(ctx, c.ddl); err != nil {
				t.Fatalf("apply probe: %v", err)
			}
			got, err := opReverseFindings(ctx, sp)
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			hit := false
			for _, f := range got {
				if strings.Contains(f, c.want) {
					hit = true
				}
			}
			if !hit {
				t.Fatalf("the reverse pin did not flag %q; findings: %v", c.name, got)
			}
		})
	}
}

// ---------------------------------------------------------- frozen-clock scan --

// opFrozenClockFindings is ADR 0021 §6 "donan saat taraması": the prosrc of every
// function tappa_opdefiner owns, and every DEFAULT on 00026's tables. A time column's
// DEFAULT must be exactly clock_timestamp(); no DEFAULT may name a frozen clock.
//
// ⚠️ ITS LIMIT, STATED AS THE ADR STATES IT: it is a list of names. A frozen clock
// behind an alias or a helper function is invisible to it; the behaviour tests in
// operatorfuncs_test.go (backdated rows, a sleeping transaction) carry that half.
func opFrozenClockFindings(ctx context.Context, q opQuerier) (findings []string, defaults int, err error) {
	rows, err := q.Query(ctx, `
		SELECT p.oid::regprocedure::text, substring(p.prosrc FROM $1)
		  FROM pg_proc p
		 WHERE p.proowner = 'tappa_opdefiner'::regrole
		   AND p.prosrc ~ $1`, opFrozenClockRE)
	if err != nil {
		return nil, 0, err
	}
	for rows.Next() {
		var sig, word string
		if err := rows.Scan(&sig, &word); err != nil {
			rows.Close()
			return nil, 0, err
		}
		findings = append(findings, fmt.Sprintf("%s: body names the frozen clock %q", sig, word))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	rows, err = q.Query(ctx, `
		SELECT c.relname, a.attname, format_type(a.atttypid, a.atttypmod),
		       pg_get_expr(d.adbin, d.adrelid)
		  FROM pg_attrdef d
		  JOIN pg_class c ON c.oid = d.adrelid
		  JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
		 WHERE c.relnamespace = 'public'::regnamespace AND c.relname = ANY($1)
		 ORDER BY 1, 2`, opTables)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	re := regexp.MustCompile(`(?i)\b(now|current_timestamp|transaction_timestamp|statement_timestamp|localtimestamp|localtime|current_time|current_date|today|tomorrow|yesterday)\b`)
	for rows.Next() {
		var table, col, typ, expr string
		if err := rows.Scan(&table, &col, &typ, &expr); err != nil {
			return nil, 0, err
		}
		if typ == "timestamp with time zone" {
			defaults++
			if expr != "clock_timestamp()" {
				findings = append(findings, fmt.Sprintf("%s.%s DEFAULT %s, want clock_timestamp()", table, col, expr))
			}
		}
		if m := re.FindString(expr); m != "" {
			findings = append(findings, fmt.Sprintf("%s.%s DEFAULT %s names the frozen clock %q", table, col, expr, m))
		}
	}
	return findings, defaults, rows.Err()
}

// TestOperator00026_NoFrozenClock: the scan is clean, it saw the time DEFAULTs it is
// for, and it catches each banned SPELLING (call, keyword, literal) and a DEFAULT.
func TestOperator00026_NoFrozenClock(t *testing.T) {
	ctx, tx := opTx(t)

	findings, defaults, err := opFrozenClockFindings(ctx, tx)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	for _, f := range findings {
		t.Error(f)
	}
	// admins.created_at, sessions.created_at, sessions.last_used_at, audit.at,
	// tickets.created_at: a scan that read fewer read the wrong tables.
	if defaults < 5 {
		t.Fatalf("anti-vacuity: the scan saw %d timestamptz DEFAULTs on 00026's tables, want >= 5", defaults)
	}

	controls := []struct{ name, ddl string }{
		{"now()", `CREATE FUNCTION public.op_zz_probe(p_session text) RETURNS void LANGUAGE plpgsql VOLATILE
			SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$ BEGIN PERFORM now(); END $$`},
		{"the 'now' literal", `CREATE FUNCTION public.op_zz_probe(p_session text) RETURNS void LANGUAGE plpgsql VOLATILE
			SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$ BEGIN PERFORM 'now'::timestamptz; END $$`},
		{"CURRENT_TIMESTAMP", `CREATE FUNCTION public.op_zz_probe(p_session text) RETURNS void LANGUAGE plpgsql VOLATILE
			SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$ BEGIN PERFORM CURRENT_TIMESTAMP; END $$`},
		{"statement_timestamp()", `CREATE FUNCTION public.op_zz_probe(p_session text) RETURNS void LANGUAGE plpgsql VOLATILE
			SECURITY DEFINER SET search_path = pg_catalog, pg_temp AS $$ BEGIN PERFORM statement_timestamp(); END $$`},
		{"a DEFAULT now()", `ALTER TABLE operator_audit_log ALTER COLUMN at SET DEFAULT now()`},
	}
	for _, c := range controls {
		t.Run("control/"+c.name, func(t *testing.T) {
			sp, err := tx.Begin(ctx)
			if err != nil {
				t.Fatalf("savepoint: %v", err)
			}
			defer func() {
				if err := sp.Rollback(ctx); err != nil {
					t.Fatalf("rollback probe: %v", err)
				}
			}()
			if _, err := sp.Exec(ctx, c.ddl); err != nil {
				t.Fatalf("apply probe: %v", err)
			}
			if strings.Contains(c.ddl, "op_zz_probe") {
				if _, err := sp.Exec(ctx, `ALTER FUNCTION public.op_zz_probe(text) OWNER TO tappa_opdefiner`); err != nil {
					t.Fatalf("own probe: %v", err)
				}
			}
			got, _, err := opFrozenClockFindings(ctx, sp)
			if err != nil {
				t.Fatalf("scan: %v", err)
			}
			if len(got) == 0 {
				t.Fatalf("the frozen-clock scan did not see %s", c.name)
			}
		})
	}
}

// ------------------------------------------------------------ privilege matrix --

// opColumns returns the columns of table on which role holds priv, in attnum order --
// the WHOLE list, compared as a whole, because a mis-written grant widens a list
// silently and only a whole-list comparison sees the extra column
// (tenantsgrant_test.go's argument). has_column_privilege is also true when the
// privilege is held table-wide, so a table-level grant shows up here as "every column".
func opColumns(t *testing.T, ctx context.Context, q opQuerier, role, table, priv string) string {
	t.Helper()
	var got string
	if err := q.QueryRow(ctx, `
		SELECT coalesce(string_agg(a.attname, ',' ORDER BY a.attnum), '')
		  FROM pg_attribute a
		 WHERE a.attrelid = ('public.' || $2)::regclass AND a.attnum > 0 AND NOT a.attisdropped
		   AND has_column_privilege($1, a.attrelid, a.attname, $3)`, role, table, priv).Scan(&got); err != nil {
		t.Fatalf("read %s's %s columns on %s: %v", role, priv, table, err)
	}
	return got
}

// TestOperator00026_PrivilegeMatrix is ADR 0021 §6 "Katalog — roller ve tablolar"
// under §1's measurement contract: SELECT/INSERT/UPDATE by column
// (has_column_privilege over every column, i.e. has_any_column_privilege and more),
// DELETE/TRUNCATE/REFERENCES/TRIGGER by table.
func TestOperator00026_PrivilegeMatrix(t *testing.T) {
	ctx, tx := opTx(t)

	type cell struct{ role, table, priv, want string }
	matrix := []cell{
		// tappa_operator: the login lookup and nothing else (§1).
		{"tappa_operator", "platform_admins", "SELECT", "id,email,display_name,status,password_hash,totp_secret_sealed,totp_locked_until"},
		{"tappa_operator", "platform_admins", "INSERT", ""},
		{"tappa_operator", "platform_admins", "UPDATE", ""},
		{"tappa_operator", "platform_sessions", "SELECT", ""},
		{"tappa_operator", "platform_sessions", "INSERT", ""},
		{"tappa_operator", "platform_sessions", "UPDATE", ""},
		{"tappa_operator", "operator_audit_log", "SELECT", ""},
		{"tappa_operator", "operator_audit_log", "INSERT", ""},
		{"tappa_operator", "operator_audit_log", "UPDATE", ""},
		{"tappa_operator", "operator_read_tickets", "SELECT", ""},
		{"tappa_operator", "operator_read_tickets", "INSERT", ""},
		{"tappa_operator", "operator_read_tickets", "UPDATE", ""},
		// tappa_opdefiner: reads what it decides on, writes credentials and state, never
		// SELECTs the digest or the sealed secret, never INSERTs an account.
		{"tappa_opdefiner", "platform_admins", "SELECT", "id,email,status,totp_last_step,totp_failures,totp_locked_until,enroll_token_hash,enroll_expires_at,enroll_used_at"},
		{"tappa_opdefiner", "platform_admins", "INSERT", ""},
		{"tappa_opdefiner", "platform_admins", "UPDATE", "status,password_hash,totp_secret_sealed,totp_last_step,totp_failures,totp_locked_until,last_login_at,enroll_used_at"},
		{"tappa_opdefiner", "platform_sessions", "SELECT", "id,admin_id,token_hash,created_at,mfa_verified_at,last_used_at,revoked_at"},
		{"tappa_opdefiner", "platform_sessions", "INSERT", "admin_id,token_hash,mfa_verified_at"},
		{"tappa_opdefiner", "platform_sessions", "UPDATE", "last_used_at,revoked_at"},
		{"tappa_opdefiner", "operator_audit_log", "SELECT", ""},
		{"tappa_opdefiner", "operator_audit_log", "INSERT", "kind,session_id,actor_admin_id,target_admin_id,target_tenant_id,target_scope,page_number,page_size,detail"},
		{"tappa_opdefiner", "operator_audit_log", "UPDATE", ""},
		{"tappa_opdefiner", "operator_read_tickets", "SELECT", "id,ticket_hash,session_id,kind,target_tenant_id,audit_id,created_xact,expires_at,consumed_at"},
		{"tappa_opdefiner", "operator_read_tickets", "INSERT", "ticket_hash,session_id,kind,target_tenant_id,audit_id,expires_at"},
		{"tappa_opdefiner", "operator_read_tickets", "UPDATE", "consumed_at"},
	}
	// tappa_app and tappa_resolver: NOTHING on any of the four (ADR 0021 §1; the
	// resolver's blast radius is its own grants, ADR 0002 md.7).
	for _, role := range []string{"tappa_app", "tappa_resolver"} {
		for _, table := range opTables {
			for _, priv := range []string{"SELECT", "INSERT", "UPDATE"} {
				matrix = append(matrix, cell{role, table, priv, ""})
			}
		}
	}
	for _, c := range matrix {
		if got := opColumns(t, ctx, tx, c.role, c.table, c.priv); got != c.want {
			t.Errorf("%s %s on %s = (%s)\n%s want (%s)", c.role, c.priv, c.table, got,
				strings.Repeat(" ", len(c.role)+len(c.priv)+len(c.table)+5), c.want)
		}
	}

	// The table-level verbs, which column privileges cannot express.
	for _, role := range []string{"tappa_app", "tappa_operator", "tappa_opdefiner", "tappa_resolver"} {
		for _, table := range opTables {
			for _, priv := range []string{"DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
				var may bool
				if err := tx.QueryRow(ctx, `SELECT has_table_privilege($1, 'public.' || $2, $3)`,
					role, table, priv).Scan(&may); err != nil {
					t.Fatalf("has_table_privilege: %v", err)
				}
				if may {
					t.Errorf("has_table_privilege(%s, %s, %s) = true, want false", role, table, priv)
				}
			}
		}
	}

	// The cells ADR 0021 §6 names one by one, asserted by name as well so a reader
	// can find them -- the whole-list comparison above already implies each.
	named := []struct {
		role, table, column, priv string
		want                      bool
	}{
		{"tappa_operator", "platform_sessions", "token_hash", "SELECT", false},
		{"tappa_operator", "platform_admins", "enroll_token_hash", "SELECT", false},
		{"tappa_opdefiner", "platform_admins", "password_hash", "SELECT", false},
		{"tappa_opdefiner", "platform_admins", "totp_secret_sealed", "SELECT", false},
		{"tappa_opdefiner", "platform_admins", "password_hash", "UPDATE", true},
		{"tappa_opdefiner", "platform_sessions", "token_hash", "SELECT", true}, // the named exception (§1)
		{"tappa_opdefiner", "operator_read_tickets", "created_xact", "UPDATE", false},
		{"tappa_opdefiner", "operator_read_tickets", "created_xact", "INSERT", false},
		{"tappa_opdefiner", "operator_read_tickets", "created_at", "INSERT", false},
		{"tappa_opdefiner", "operator_read_tickets", "consumed_at", "INSERT", false},
		{"tappa_opdefiner", "operator_audit_log", "at", "INSERT", false},
	}
	for _, n := range named {
		var got bool
		if err := tx.QueryRow(ctx, `SELECT has_column_privilege($1, 'public.' || $2, $3, $4)`,
			n.role, n.table, n.column, n.priv).Scan(&got); err != nil {
			t.Fatalf("has_column_privilege: %v", err)
		}
		if got != n.want {
			t.Errorf("has_column_privilege(%s, %s, %s, %s) = %v, want %v", n.role, n.table, n.column, n.priv, got, n.want)
		}
	}

	// tappa_operator holds no privilege on ANY tenant table (§6): every table in public
	// other than 00026's four.
	var leaks []string
	rows, err := tx.Query(ctx, `
		SELECT c.relname || ':' || p.priv
		  FROM pg_class c
		  CROSS JOIN (VALUES ('SELECT'), ('INSERT'), ('UPDATE')) AS p(priv)
		 WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p', 'v', 'm')
		   AND c.relname <> ALL ($1)
		   AND has_any_column_privilege('tappa_operator', c.oid, p.priv)
		UNION ALL
		SELECT c.relname || ':' || p.priv
		  FROM pg_class c
		  CROSS JOIN (VALUES ('DELETE'), ('TRUNCATE')) AS p(priv)
		 WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
		   AND c.relname <> ALL ($1)
		   AND has_table_privilege('tappa_operator', c.oid, p.priv)`, opTables)
	if err != nil {
		t.Fatalf("scan tenant tables: %v", err)
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan: %v", err)
		}
		leaks = append(leaks, s)
	}
	rows.Close()
	if len(leaks) > 0 {
		t.Errorf("tappa_operator holds privileges outside the operator tables: %v", leaks)
	}

	// D-4 (round 3) -- tappa_opdefiner's reach OUTSIDE the four operator tables.
	// ADR 0021 §6 makes a SELECT ban on the seven secret columns of the tenant tables
	// normative, and as of OP-5 the definer holds NOTHING on any other table or
	// sequence: no op_* born here touches tenant data. Measured before this block: a
	// mutant granting SELECT (aes_key_ref, app_key_ref) ON tags and (password_hash) ON
	// admin_users to tappa_opdefiner left every operator test green.
	//
	// HOW THIS GROWS (OP-10 and later add deliberate grants -- op_publish_legal needs
	// legal_documents, the A2 reads need tenant columns): each one is added to
	// opdefinerTenantGrants below as "table:PRIVILEGE" -> the EXACT column list in attnum
	// order (or "*" for a table-level verb), in the same change as its migration. The
	// seven secret columns can never be added for SELECT: the named check below does not
	// consult the allow-list, and the allow-list is itself checked against them.
	opdefinerTenantGrants := map[string]string{}
	neverSelect := [][2]string{
		{"tags", "aes_key_ref"}, {"tags", "app_key_ref"}, {"admin_users", "password_hash"},
		{"sessions", "token_hash"}, {"admin_sessions", "token_hash"},
		{"password_resets", "token_hash"}, {"employee_invites", "code_hash"},
	}
	for _, nc := range neverSelect {
		var may bool
		if err := tx.QueryRow(ctx, `SELECT has_column_privilege('tappa_opdefiner', 'public.' || $1, $2, 'SELECT')`,
			nc[0], nc[1]).Scan(&may); err != nil {
			t.Fatalf("has_column_privilege(tappa_opdefiner, %s.%s): %v", nc[0], nc[1], err)
		}
		if may {
			t.Errorf("tappa_opdefiner may SELECT %s.%s -- one of ADR 0021 §1's \"asla\" columns", nc[0], nc[1])
		}
		if cols, ok := opdefinerTenantGrants[nc[0]+":SELECT"]; ok && strings.Contains(","+cols+",", ","+nc[1]+",") {
			t.Errorf("opdefinerTenantGrants allows SELECT on %s.%s, an \"asla\" column", nc[0], nc[1])
		}
	}
	var others []string
	otherRows, err := tx.Query(ctx, `
		SELECT c.relname FROM pg_class c
		 WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p', 'v', 'm')
		   AND c.relname <> ALL ($1)
		 ORDER BY 1`, opTables)
	if err != nil {
		t.Fatalf("list the other relations: %v", err)
	}
	for otherRows.Next() {
		var n string
		if err := otherRows.Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		others = append(others, n)
	}
	otherRows.Close()
	if len(others) < 17 {
		t.Fatalf("anti-vacuity: only %d other relations in public (%v); the tenant tables alone were 17", len(others), others)
	}
	for _, table := range others {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE"} {
			if got, want := opColumns(t, ctx, tx, "tappa_opdefiner", table, priv), opdefinerTenantGrants[table+":"+priv]; got != want {
				t.Errorf("tappa_opdefiner %s on %s = (%s), want (%s): a grant outside opdefinerTenantGrants", priv, table, got, want)
			}
		}
		for _, priv := range []string{"DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
			var may bool
			if err := tx.QueryRow(ctx, `SELECT has_table_privilege('tappa_opdefiner', 'public.' || $1, $2)`, table, priv).Scan(&may); err != nil {
				t.Fatalf("has_table_privilege: %v", err)
			}
			if want := opdefinerTenantGrants[table+":"+priv] == "*"; may != want {
				t.Errorf("has_table_privilege(tappa_opdefiner, %s, %s) = %v, want %v", table, priv, may, want)
			}
		}
	}
	for _, role := range []string{"tappa_opdefiner", "tappa_operator"} {
		if n := opInt(t, ctx, tx, `
			SELECT count(*) FROM pg_class s
			 WHERE s.relnamespace = 'public'::regnamespace AND s.relkind = 'S'
			   AND (has_sequence_privilege($1, s.oid, 'USAGE') OR has_sequence_privilege($1, s.oid, 'SELECT')
			        OR has_sequence_privilege($1, s.oid, 'UPDATE'))`, role); n != 0 {
			t.Errorf("%s holds a privilege on %d sequence(s) in public", role, n)
		}
	}

	// No sequence belongs to these tables (uuid keys); a sequence would carry the
	// default ACL tappa_app=rU that a table REVOKE does not reach (ADR 0021 §1).
	if n := opInt(t, ctx, tx, `
		SELECT count(*) FROM pg_depend d JOIN pg_class s ON s.oid = d.objid AND s.relkind = 'S'
		 WHERE d.refobjid = ANY (SELECT ('public.' || t)::regclass::oid FROM unnest($1::text[]) AS t)`,
		opTables); n != 0 {
		t.Errorf("%d sequence(s) belong to 00026's tables; each needs REVOKE ALL ON SEQUENCE ... FROM tappa_app", n)
	}
}

// TestOperator00026_AppHoldsNothingUnderTheProductionDefaultACL re-runs 00026 against
// the WIDE default ACL production has (tappa_app=arwd on new tables, rU on sequences)
// and proves the migration's own REVOKEs leave tappa_app with nothing.
//
// 🔴 WHY IT RE-RUNS THE MIGRATION RATHER THAN READING THE CATALOGUE: the catalogue on
// a fresh CI database was built under the NARROW default (`ar`), so reading it says
// nothing about what the same file does in production -- the environment where a
// forgotten REVOKE would matter (01-roles.sql's own warning: "DOGRULAMA ORTAMI
// URETIMDEN KATI"). Down and Up are executed from the migration file inside this
// test's transaction and rolled back with it.
func TestOperator00026_AppHoldsNothingUnderTheProductionDefaultACL(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opUpDown(t)

	for _, stmt := range []string{
		`ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO tappa_app`,
		`ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO tappa_app`,
	} {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			t.Fatalf("widen the default ACL: %v", err)
		}
	}
	// POSITIVE CONTROL: the widening is really in force -- a table created now hands
	// tappa_app all four verbs. Without this, a default that failed to widen would make
	// everything below pass for the wrong reason.
	if _, err := tx.Exec(ctx, `CREATE TABLE zz_op5_default_probe (id int)`); err != nil {
		t.Fatalf("control table: %v", err)
	}
	var s, i, u, d bool
	if err := tx.QueryRow(ctx, `SELECT has_table_privilege('tappa_app', 'zz_op5_default_probe', 'SELECT'),
	                                   has_table_privilege('tappa_app', 'zz_op5_default_probe', 'INSERT'),
	                                   has_table_privilege('tappa_app', 'zz_op5_default_probe', 'UPDATE'),
	                                   has_table_privilege('tappa_app', 'zz_op5_default_probe', 'DELETE')`).Scan(&s, &i, &u, &d); err != nil {
		t.Fatalf("control read: %v", err)
	}
	if !(s && i && u && d) {
		t.Fatalf("the widened default did not reach a new table (select=%v insert=%v update=%v delete=%v); this test proves nothing", s, i, u, d)
	}

	if _, err := tx.Conn().PgConn().Exec(ctx, down).ReadAll(); err != nil {
		t.Fatalf("run 00026 Down inside the transaction: %v", err)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class WHERE relname = ANY($1) AND relnamespace = 'public'::regnamespace`, opTables); n != 0 {
		t.Fatalf("Down left %d of the four tables behind", n)
	}
	if _, err := tx.Conn().PgConn().Exec(ctx, up).ReadAll(); err != nil {
		t.Fatalf("run 00026 Up under the production default: %v", err)
	}

	for _, table := range opTables {
		for _, priv := range []string{"SELECT", "INSERT", "UPDATE"} {
			if got := opColumns(t, ctx, tx, "tappa_app", table, priv); got != "" {
				t.Errorf("under the production default ACL tappa_app holds %s on %s (%s); a REVOKE ALL is missing", priv, table, got)
			}
		}
		for _, priv := range []string{"DELETE", "TRUNCATE"} {
			var may bool
			if err := tx.QueryRow(ctx, `SELECT has_table_privilege('tappa_app', 'public.' || $1, $2)`, table, priv).Scan(&may); err != nil {
				t.Fatalf("has_table_privilege: %v", err)
			}
			if may {
				t.Errorf("under the production default ACL tappa_app holds %s on %s", priv, table)
			}
		}
	}
}

// TestOperator00026_RestoreResidueReadsNothing is the reason RLS is ENABLED on the four
// tables (00026's header): a pg_dump restore re-applies the default ACL to every CREATE
// TABLE and emits no REVOKE for it (M8-02 FAZ E, 01-roles.sql), so after a restore
// tappa_app can hold SELECT/INSERT/UPDATE/DELETE here. The residue is re-created by a
// GRANT inside this transaction; with RLS on and no policy for tappa_app it reads 0
// rows and cannot write. tappa_operator is covered the same way on the tables it has
// no business in, and sees only ACTIVE accounts on the one it reads.
func TestOperator00026_RestoreResidueReadsNothing(t *testing.T) {
	ctx, tx := opTx(t)

	// ENABLE and FORCE on all four -- and, schema-wide, no table with one and not the
	// other: scripts/pg-restore-verify.sh section 2 refuses a restored database whose
	// ENABLE and FORCE counts differ, so an ENABLE-only table would fail every disaster
	// recovery (00026's header).
	for _, table := range opTables {
		var enabled, forced bool
		if err := tx.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class
		                             WHERE oid = ('public.' || $1)::regclass`, table).Scan(&enabled, &forced); err != nil {
			t.Fatalf("read RLS flags of %s: %v", table, err)
		}
		if !enabled || !forced {
			t.Errorf("%s: relrowsecurity=%v relforcerowsecurity=%v, want both", table, enabled, forced)
		}
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_class
	                            WHERE relnamespace = 'public'::regnamespace AND relkind = 'r'
	                              AND relrowsecurity <> relforcerowsecurity`); n != 0 {
		t.Errorf("%d table(s) in public have ENABLE and FORCE row level security out of step; scripts/pg-restore-verify.sh would refuse a restore of this schema", n)
	}

	active := opNewActive(t, ctx, tx)
	disabled := opNewActive(t, ctx, tx)
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET status = 'disabled' WHERE id = $1`, disabled.id); err != nil {
		t.Fatalf("disable: %v", err)
	}
	_, sessionID := opNewSession(t, ctx, tx, active.id, true)
	if _, err := tx.Exec(ctx, `INSERT INTO operator_audit_log (kind, session_id, actor_admin_id) VALUES ('login', $1, $2)`,
		sessionID, active.id); err != nil {
		t.Fatalf("audit fixture: %v", err)
	}

	for _, table := range opTables {
		// POSITIVE CONTROL: the owner sees rows (tickets may be empty; the other three
		// are not), so "0 rows" below is RLS and not an empty table.
		if table != "operator_read_tickets" {
			if n := opInt(t, ctx, tx, `SELECT count(*) FROM public.`+table); n == 0 {
				t.Fatalf("fixture: %s is empty for the owner", table)
			}
		}
		if _, err := tx.Exec(ctx, `GRANT SELECT, INSERT, UPDATE, DELETE ON public.`+table+` TO tappa_app, tappa_operator`); err != nil {
			t.Fatalf("re-create the restore residue on %s: %v", table, err)
		}
	}

	for _, role := range []string{"tappa_app", "tappa_operator"} {
		for _, table := range []string{"platform_sessions", "operator_audit_log", "operator_read_tickets"} {
			var n int64
			if err := opAs(t, ctx, tx, role, func(sp pgx.Tx) error {
				return sp.QueryRow(ctx, `SELECT count(*) FROM public.`+table).Scan(&n)
			}); err != nil {
				t.Fatalf("%s reading %s with the residue grant: %v", role, table, err)
			}
			if n != 0 {
				t.Errorf("%s reads %d row(s) of %s through a restore-residue GRANT; row level security is not enabled there", role, n, table)
			}
		}
	}
	// tappa_app on platform_admins: nothing at all.
	var appAdmins int64
	if err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT count(*) FROM public.platform_admins`).Scan(&appAdmins)
	}); err != nil {
		t.Fatalf("tappa_app reading platform_admins: %v", err)
	}
	if appAdmins != 0 {
		t.Errorf("tappa_app reads %d operator account(s) (digests, sealed secrets) through a restore-residue GRANT", appAdmins)
	}
	err := opExecAs(t, ctx, tx, "tappa_app",
		`INSERT INTO public.platform_admins (email, display_name, status, enroll_token_hash, enroll_issued_at, enroll_expires_at)
		 VALUES ('residue@example.test', 'residue', 'pending', repeat('0', 64), clock_timestamp(), clock_timestamp() + interval '10 minutes')`)
	opWant(t, err, sqlstateInsufficientPrivi, "tappa_app minting an operator through a restore-residue INSERT grant (RLS WITH CHECK)")

	// tappa_operator's ONE policy: active accounts only.
	var sawActive, sawDisabled int64
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		if err := sp.QueryRow(ctx, `SELECT count(*) FROM public.platform_admins WHERE id = $1`, active.id).Scan(&sawActive); err != nil {
			return err
		}
		return sp.QueryRow(ctx, `SELECT count(*) FROM public.platform_admins WHERE id = $1`, disabled.id).Scan(&sawDisabled)
	}); err != nil {
		t.Fatalf("tappa_operator login lookup: %v", err)
	}
	if sawActive != 1 || sawDisabled != 0 {
		t.Errorf("tappa_operator sees active=%d (want 1) disabled=%d (want 0); the login lookup policy is not status = 'active'", sawActive, sawDisabled)
	}
}

// TestOperator00026_OperatorRoleIsNotPrivileged drives the SHIPPED boot probe
// (roleFactsQuery) as tappa_operator: the role must pass the production pool's refusal
// (ADR 0021 §1), and it must stop passing the moment it becomes a member of
// tappa_opdefiner -- which is the structural brake the ADR relies on.
func TestOperator00026_OperatorRoleIsNotPrivileged(t *testing.T) {
	ctx, tx := opTx(t)

	read := func() RoleFacts {
		t.Helper()
		var f RoleFacts
		if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
			return sp.QueryRow(ctx, roleFactsQuery).Scan(&f.User, &f.Super, &f.BypassRLS, &f.OwnsScopedTable, &f.InheritsPrivilege)
		}); err != nil {
			t.Fatalf("run the boot probe as tappa_operator: %v", err)
		}
		return f
	}
	f := read()
	if f.User != "tappa_operator" {
		t.Fatalf("the probe ran as %q, want tappa_operator", f.User)
	}
	if f.Privileged() {
		t.Fatalf("RoleFacts.Privileged() = true for tappa_operator (%+v); a production OperatorDB would refuse to open", f)
	}
	if roleRefusal(f, true) != nil {
		t.Fatalf("roleRefusal refuses tappa_operator in production: %v", roleRefusal(f, true))
	}

	// POSITIVE CONTROL: membership in the BYPASSRLS definer is one SET ROLE away from
	// every tenant's rows; the probe must see it.
	if _, err := tx.Exec(ctx, `GRANT tappa_opdefiner TO tappa_operator`); err != nil {
		t.Fatalf("grant membership: %v", err)
	}
	f = read()
	if !f.InheritsPrivilege || !f.Privileged() || roleRefusal(f, true) == nil {
		t.Fatalf("tappa_operator as a MEMBER of tappa_opdefiner reads %+v; the boot probe must refuse it", f)
	}
}

// TestOperator00026_AuditLogRefusesTheOwnerToo: operator_audit_log is append-only for
// every role, tappa_owner included (ADR 0020 §5; OP-5's acceptance). TRUNCATE is
// driven through CASCADE from BOTH parents, because a plain TRUNCATE of the audit
// table is refused earlier by its FK child (0A000) and would prove nothing about the
// trigger.
func TestOperator00026_AuditLogRefusesTheOwnerToo(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	if _, err := tx.Exec(ctx, `INSERT INTO operator_audit_log (kind, target_admin_id) VALUES ('login_failed', $1)`, a.id); err != nil {
		t.Fatalf("fixture row: %v", err)
	}
	for _, c := range []struct{ what, sql string }{
		{"owner UPDATE", `UPDATE operator_audit_log SET kind = 'locked' WHERE target_admin_id = '` + a.id.String() + `'`},
		{"owner DELETE", `DELETE FROM operator_audit_log WHERE target_admin_id = '` + a.id.String() + `'`},
		{"owner TRUNCATE ... CASCADE", `TRUNCATE operator_audit_log CASCADE`},
		{"owner TRUNCATE platform_admins CASCADE (reaches the log)", `TRUNCATE platform_admins CASCADE`},
	} {
		opWant(t, opTry(t, ctx, tx, c.sql), sqlstateRestrictViolation, c.what)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM operator_audit_log WHERE target_admin_id = $1`, a.id); n != 1 {
		t.Fatalf("the fixture row count is %d after the refused mutations, want 1", n)
	}
	// And the application role is refused before any trigger: no privilege at all.
	opWant(t, opExecAs(t, ctx, tx, "tappa_app", `DELETE FROM public.operator_audit_log`),
		sqlstateInsufficientPrivi, "tappa_app DELETE on operator_audit_log")
}

// TestOperator00026_CallersTempTableIsNeverRead is ADR 0021 §6 "geçici tablo gölgesi":
// the caller opens temp tables with the definer's table names, fills them with forged
// rows, and -- the step two auditors measured as MANDATORY -- GRANTs them to
// tappa_opdefiner, so a body that resolved to pg_temp could really read and write them.
// Every function that reads or writes those tables must still use the real ones:
// op_touch_session and op_close_session (the forged SESSION), op_record_auth_event (the
// audit row), op_open_session (a forged copy of a REAL account whose shadow has no step
// history -- read from the shadow, a replayed code would open a session and the real
// totp_last_step would never move) and op_complete_enrollment (a forged pending copy of a
// REAL account carrying a token the caller chose).
func TestOperator00026_CallersTempTableIsNeverRead(t *testing.T) {
	ctx, tx := opTx(t)
	a := opNewActive(t, ctx, tx)
	realHash, realSession := opNewSession(t, ctx, tx, a.id, true)
	forged := opRandHex(t)
	p, rawP := opNewPending(t, ctx, tx, 0, 30*time.Minute)
	forgedRaw, forgedTokenHash := opRandToken(t)
	cur := opStep(t, ctx, tx)
	// A used this step already: the REAL row refuses the same step again, the shadow
	// (totp_last_step 0) would accept it.
	if _, err := tx.Exec(ctx, `UPDATE platform_admins SET totp_last_step = $2 WHERE id = $1`, a.id, cur); err != nil {
		t.Fatalf("set A's real step: %v", err)
	}

	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		for _, s := range []string{
			`CREATE TEMP TABLE platform_sessions (id uuid DEFAULT gen_random_uuid(), admin_id uuid, token_hash text,
			     created_at timestamptz DEFAULT clock_timestamp(), mfa_verified_at timestamptz,
			     last_used_at timestamptz DEFAULT clock_timestamp(), revoked_at timestamptz)`,
			`CREATE TEMP TABLE platform_admins (id uuid, email text, status text, password_hash text,
			     totp_secret_sealed bytea, totp_last_step bigint, totp_failures integer,
			     totp_locked_until timestamptz, last_login_at timestamptz, enroll_token_hash text,
			     enroll_expires_at timestamptz, enroll_used_at timestamptz)`,
			`CREATE TEMP TABLE operator_audit_log (id uuid DEFAULT gen_random_uuid(), at timestamptz DEFAULT clock_timestamp(),
			     kind text, session_id uuid, actor_admin_id uuid, target_admin_id uuid, target_tenant_id uuid,
			     target_scope text, page_number integer, page_size integer, detail jsonb DEFAULT '{}')`,
			`GRANT ALL ON pg_temp.platform_sessions, pg_temp.platform_admins, pg_temp.operator_audit_log TO tappa_opdefiner`,
		} {
			if _, err := sp.Exec(ctx, s); err != nil {
				return fmt.Errorf("%s: %w", s[:40], err)
			}
		}
		if _, err := sp.Exec(ctx, `INSERT INTO pg_temp.platform_sessions (admin_id, token_hash, mfa_verified_at) VALUES ($1, $2, clock_timestamp())`,
			a.id, forged); err != nil {
			return err
		}
		// forged copies of two REAL accounts: A with no step history, P with the
		// caller's own token.
		if _, err := sp.Exec(ctx, `INSERT INTO pg_temp.platform_admins VALUES ($1, 'shadow-a@example.test', 'active', NULL, NULL, 0, 0, NULL, NULL, NULL, NULL, NULL)`, a.id); err != nil {
			return err
		}
		_, err := sp.Exec(ctx, `INSERT INTO pg_temp.platform_admins VALUES ($1, 'shadow-p@example.test', 'pending', NULL, NULL, 0, 0, NULL, NULL, $2, clock_timestamp() + interval '30 minutes', NULL)`,
			p.id, forgedTokenHash)
		return err
	}); err != nil {
		t.Fatalf("build the shadow as the caller: %v", err)
	}

	// CONTROL: the shadow is REACHABLE by the definer role -- so a refusal below is the
	// qualification working, not a permission error hiding it.
	var reachable int64
	if err := opAs(t, ctx, tx, "tappa_opdefiner", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.platform_sessions WHERE token_hash = $1)
		                              + (SELECT count(*) FROM pg_temp.platform_admins WHERE id IN ($2, $3))`,
			forged, a.id, p.id).Scan(&reachable)
	}); err != nil {
		t.Fatalf("the definer role cannot read the shadow (%v); the GRANT step is what makes this test mean anything", err)
	}
	if reachable != 3 {
		t.Fatalf("the definer role sees %d forged rows in the shadow, want 3", reachable)
	}

	// touch / close: the forged session does not exist.
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_touch_session($1)`, forged),
		sqlstateInvalidAuthorization, "op_touch_session with a hash that exists only in the caller's temp table")
	opWant(t, opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_close_session($1)`, forged),
		sqlstateInvalidAuthorization, "op_close_session with a hash that exists only in the caller's temp table")
	// open: the step A already spent is refused by the REAL row; the next one works and
	// moves the REAL totp_last_step.
	opWant(t, opOpen(t, ctx, tx, a.id, opRandHex(t), cur), sqlstateInvalidAuthorization,
		"op_open_session with a step the real account already used (the shadow copy has none)")
	if err := opOpen(t, ctx, tx, a.id, opRandHex(t), cur+1); err != nil {
		t.Fatalf("op_open_session with a fresh step: %v", err)
	}
	if got := opInt(t, ctx, tx, `SELECT totp_last_step FROM public.platform_admins WHERE id = $1`, a.id); got != cur+1 {
		t.Errorf("the REAL totp_last_step is %d after a login with step %d: the step advanced somewhere else", got, cur+1)
	}
	// enrollment: the caller's token is not the real account's.
	opWant(t, opEnroll(t, ctx, tx, p.id, forgedRaw, opFakeDigest("s"), opFakeSealed(0x55), cur, opRandHex(t)),
		sqlstateInvalidAuthorization, "op_complete_enrollment with a token that exists only in the caller's temp table")
	if err := opEnroll(t, ctx, tx, p.id, rawP, opFakeDigest("s"), opFakeSealed(0x55), cur, opRandHex(t)); err != nil {
		t.Fatalf("op_complete_enrollment with the real token: %v", err)
	}

	// close / record on the real rows: their audit rows land in the REAL table.
	realBefore := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, realSession)
	if err := opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_close_session($1)`, realHash); err != nil {
		t.Fatalf("op_close_session on the real session: %v", err)
	}
	if err := opExecAs(t, ctx, tx, "tappa_operator", `SELECT public.op_record_auth_event('login_failed', NULL, $1)`, a.id); err != nil {
		t.Fatalf("op_record_auth_event: %v", err)
	}
	if got := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE session_id = $1`, realSession) - realBefore; got != 1 {
		t.Errorf("the logout row reached the REAL audit table %d time(s), want 1", got)
	}
	if got := opInt(t, ctx, tx, `SELECT count(*) FROM public.operator_audit_log WHERE kind IN ('login', 'enrollment') AND actor_admin_id IN ($1, $2)`, a.id, p.id); got != 2 {
		t.Errorf("%d login/enrollment origin row(s) reached the REAL audit table, want 2", got)
	}
	var shadowAudit, shadowSessions, shadowTouched int64
	if err := opAs(t, ctx, tx, "tappa_operator", func(sp pgx.Tx) error {
		return sp.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_temp.operator_audit_log),
		                                (SELECT count(*) FROM pg_temp.platform_sessions),
		                                (SELECT count(*) FROM pg_temp.platform_admins
		                                  WHERE totp_last_step <> 0 OR status <> CASE WHEN id = $1 THEN 'active' ELSE 'pending' END)`,
			a.id).Scan(&shadowAudit, &shadowSessions, &shadowTouched)
	}); err != nil {
		t.Fatalf("read the shadow: %v", err)
	}
	if shadowAudit != 0 || shadowSessions != 1 || shadowTouched != 0 {
		t.Errorf("the CALLER's temp tables were written: audit rows=%d (want 0), sessions=%d (want the 1 forged), touched admin rows=%d (want 0) -- an audit-less or state-less write (ADR 0021 §2 iv)",
			shadowAudit, shadowSessions, shadowTouched)
	}
}

// ------------------------------------------------ precondition and runbook pins --

// opRolesBlock returns the OPERATOR ROLES block of scripts/db-init/01-roles.sql exactly
// as the runbook's sed extracts it (markers included).
func opRolesBlock(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "scripts", "db-init", "01-roles.sql"))
	if err != nil {
		t.Fatalf("read 01-roles.sql: %v", err)
	}
	src := string(b)
	const begin, end = "-- >>> OPERATOR ROLES (M10 OP-5) >>>", "-- <<< OPERATOR ROLES (M10 OP-5) <<<"
	i := strings.Index(src, begin)
	j := strings.Index(src, end)
	if i < 0 || j < i {
		t.Fatalf("01-roles.sql does not carry the OPERATOR ROLES markers in order (begin=%d end=%d)", i, j)
	}
	return src[i : j+len(end)]
}

// TestOperator00026_PreconditionRefusesAClusterWithoutTheRoles pins the deploy-order
// guard: 00026's first statement refuses, with the runbook's name in the MESSAGE, a
// cluster where the roles are absent, over-privileged or joined by membership -- and
// the runbook section it names exists, extracts the same block this test runs, and the
// block creates correct roles from nothing and is a no-op when they exist.
func TestOperator00026_PreconditionRefusesAClusterWithoutTheRoles(t *testing.T) {
	ctx, tx := opTx(t)
	up, _ := opUpDown(t)
	i := strings.Index(up, "DO $$")
	j := strings.Index(up, "-- +goose StatementEnd")
	if i < 0 || j < i {
		t.Fatalf("00026's Up does not open with the precondition DO block")
	}
	pre := up[i:j]

	msgRE := regexp.MustCompile(`section "([^"]+)"`)
	m := msgRE.FindStringSubmatch(pre)
	if m == nil {
		t.Fatalf("the precondition's message does not name a runbook section")
	}
	section := m[1]
	readme, err := os.ReadFile(filepath.Join("..", "..", "deploy", "README.md"))
	if err != nil {
		t.Fatalf("read deploy/README.md: %v", err)
	}
	if !regexp.MustCompile(`(?m)^#+ .*` + regexp.QuoteMeta(section)).Match(readme) {
		t.Errorf("deploy/README.md has no heading containing %q -- the migration's refusal points an operator at a section that does not exist", section)
	}
	block := opRolesBlock(t)
	for _, marker := range []string{"-- >>> OPERATOR ROLES (M10 OP-5) >>>", "-- <<< OPERATOR ROLES (M10 OP-5) <<<"} {
		if !strings.Contains(string(readme), strings.TrimPrefix(marker, "-- ")) {
			t.Errorf("deploy/README.md's runbook does not extract the block by the marker %q", marker)
		}
	}

	run := func(sp pgx.Tx, sql string) error {
		_, err := sp.Conn().PgConn().Exec(ctx, sql).ReadAll()
		return err
	}
	inSavepoint := func(what string, setup []string, body func(sp pgx.Tx)) {
		t.Helper()
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		defer func() {
			if err := sp.Rollback(ctx); err != nil {
				t.Fatalf("%s: rollback: %v", what, err)
			}
		}()
		for _, s := range setup {
			if _, err := sp.Exec(ctx, s); err != nil {
				t.Fatalf("%s: setup %q: %v", what, s, err)
			}
		}
		body(sp)
	}

	// On this database the roles exist and are right: the precondition passes.
	inSavepoint("roles present", nil, func(sp pgx.Tx) {
		if err := run(sp, pre); err != nil {
			t.Fatalf("the precondition refuses a correct cluster: %v", err)
		}
	})

	refusals := []struct {
		what  string
		setup []string
		want  string
	}{
		{"both roles absent", []string{
			`ALTER ROLE tappa_operator RENAME TO zz_op5_was_operator`,
			`ALTER ROLE tappa_opdefiner RENAME TO zz_op5_was_opdefiner`}, "needs the cluster role(s) tappa_opdefiner, tappa_operator"},
		{"tappa_opdefiner is a superuser", []string{`ALTER ROLE tappa_opdefiner SUPERUSER`}, "tappa_opdefiner must be"},
		{"tappa_opdefiner can log in", []string{`ALTER ROLE tappa_opdefiner LOGIN`}, "tappa_opdefiner must be"},
		{"tappa_operator bypasses RLS", []string{`ALTER ROLE tappa_operator BYPASSRLS`}, "tappa_operator must be"},
		{"tappa_opdefiner has a member", []string{`GRANT tappa_opdefiner TO tappa_app`}, "has members"},
		{"tappa_operator is a member", []string{`GRANT tappa_resolver TO tappa_operator`}, "role tappa_operator is a member of another role"},
		// D1: the definer's OWN membership. Measured before the check existed: this grant
		// let the precondition pass and gave tappa_opdefiner SELECT on tags.aes_key_ref
		// and admin_users.password_hash.
		{"tappa_opdefiner is a member of the owner", []string{`GRANT tappa_owner TO tappa_opdefiner`}, "role tappa_opdefiner is a member of another role"},
		// D-3 (round 3): tappa_operator's own MEMBERS. Measured before the check existed:
		// this grant passed the precondition and tappa_app called op_record_auth_event.
		{"tappa_operator has a member", []string{`GRANT tappa_operator TO tappa_app`}, "role tappa_operator has members"},
	}
	for _, r := range refusals {
		inSavepoint(r.what, r.setup, func(sp pgx.Tx) {
			err := run(sp, pre)
			code, msg := opCode(err)
			if code != sqlstatePrerequisiteState || !strings.Contains(msg, r.want) {
				t.Errorf("%s: precondition answered %q %q, want %s containing %q", r.what, code, msg, sqlstatePrerequisiteState, r.want)
			}
			if !strings.Contains(msg, section) && strings.Contains(r.want, "must be") {
				t.Errorf("%s: the refusal does not name the runbook section %q", r.what, section)
			}
		})
	}
	// The absent-roles refusal must carry the runbook's name (it is the deploy-order
	// case: the only one an operator meets on a first deploy).
	inSavepoint("absent roles name the runbook", refusals[0].setup, func(sp pgx.Tx) {
		_, msg := opCode(run(sp, pre))
		if !strings.Contains(msg, section) || !strings.Contains(msg, "scripts/db-init/01-roles.sql") {
			t.Errorf("the absent-roles refusal does not tell the operator where to go: %q", msg)
		}
	})

	// The block, from nothing: create the two roles with ADR 0021 §1's attributes.
	inSavepoint("roles block creates", refusals[0].setup, func(sp pgx.Tx) {
		if err := run(sp, block); err != nil {
			t.Fatalf("the OPERATOR ROLES block fails on a cluster without the roles: %v", err)
		}
		var opLogin, opSuper, opBypass, defLogin, defSuper, defBypass, opPw bool
		if err := sp.QueryRow(ctx, `
			SELECT o.rolcanlogin, o.rolsuper, o.rolbypassrls, d.rolcanlogin, d.rolsuper, d.rolbypassrls,
			       o.rolpassword IS NOT NULL
			  FROM pg_authid o, pg_authid d
			 WHERE o.rolname = 'tappa_operator' AND d.rolname = 'tappa_opdefiner'`).Scan(
			&opLogin, &opSuper, &opBypass, &defLogin, &defSuper, &defBypass, &opPw); err != nil {
			t.Fatalf("read the created roles: %v", err)
		}
		if opLogin || opSuper || opBypass || opPw || defLogin || defSuper || !defBypass {
			t.Errorf("created roles: operator login=%v super=%v bypassrls=%v password=%v; opdefiner login=%v super=%v bypassrls=%v -- want operator NOLOGIN, no password, NOSUPERUSER, NOBYPASSRLS and opdefiner NOLOGIN NOSUPERUSER BYPASSRLS",
				opLogin, opSuper, opBypass, opPw, defLogin, defSuper, defBypass)
		}
		if err := run(sp, pre); err != nil {
			t.Errorf("the precondition refuses the roles the block just created: %v", err)
		}
	})
	// The block, again, on the roles that exist: a no-op that also normalises a wrong
	// attribute back -- and never takes LOGIN away from a live operator role.
	inSavepoint("roles block is idempotent", []string{`ALTER ROLE tappa_opdefiner LOGIN`, `ALTER ROLE tappa_operator LOGIN`}, func(sp pgx.Tx) {
		for k := 0; k < 2; k++ {
			if err := run(sp, block); err != nil {
				t.Fatalf("run %d of the block on existing roles: %v", k+1, err)
			}
		}
		var defLogin, opLogin bool
		if err := sp.QueryRow(ctx, `SELECT (SELECT rolcanlogin FROM pg_roles WHERE rolname = 'tappa_opdefiner'),
		                                   (SELECT rolcanlogin FROM pg_roles WHERE rolname = 'tappa_operator')`).Scan(&defLogin, &opLogin); err != nil {
			t.Fatalf("read: %v", err)
		}
		if defLogin {
			t.Error("the block left tappa_opdefiner able to log in")
		}
		if !opLogin {
			t.Error("the block took LOGIN away from tappa_operator; re-running it on a live cluster would close the operator surface")
		}
	})
}
