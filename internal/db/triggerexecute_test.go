package db

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/test/fixtures"
)

// triggerexecute_test.go -- T138 (00061): the nine pre-M11 trigger functions lost PostgreSQL's
// default EXECUTE to PUBLIC. The one thing that could make that REVOKE a regression is a trigger
// that stops firing for a role that may no longer EXECUTE its function -- §4.3's append-only
// guard, the counter's monotonicity, the encode marker, the review check. PostgreSQL checks a
// trigger function's EXECUTE when the trigger is CREATED and never when it FIRES (measured on 17.10
// before 00061 was written); these tests pin that, for each of the nine, after the chain.

const op00061File = "00061_keep_the_trigger_functions_to_their_owner.sql"

// t138Functions are the nine, by the signature has_function_privilege reads.
var t138Functions = []string{
	"public.tappa_audit_at_is_the_wall_clock()",
	"public.tappa_check_transaction_review()",
	"public.tappa_forbid_app_key_ref_rewrite()",
	"public.tappa_forbid_counter_rewind()",
	"public.tappa_forbid_encoded_at_at_insert()",
	"public.tappa_forbid_encoded_at_rewrite()",
	"public.tappa_forbid_mutation()",
	"public.tappa_forbid_revocation_reset()",
	"public.tappa_forbid_unencoded_activation()",
}

// t138World is one tenant's rows, written by the owner inside the test's transaction (rolled
// back): a flagged transaction, a revoked admin session and three plaques -- one live with a
// counter at 5, one stamped and carrying an application key ref, one in stock and never stamped.
type t138World struct {
	tenant, location, employee, tx, adminSession uuid.UUID
	live, stamped, stock                         string
	operator                                     uuid.UUID
}

func t138NewWorld(t *testing.T, ctx context.Context, tx pgx.Tx) t138World {
	t.Helper()
	w := t138World{
		tenant: uuid.New(), location: uuid.New(), employee: uuid.New(), tx: uuid.New(), adminSession: uuid.New(),
		live: randUID(t), stamped: randUID(t), stock: randUID(t),
	}
	admin := uuid.New()
	for _, s := range []struct {
		sql  string
		args []any
	}{
		{`SELECT set_config('app.tenant_id', $1::text, true)`, []any{w.tenant}},
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 't138', $2, 'bar', 'single')`,
			[]any{w.tenant, "VAT-" + w.tenant.String()}},
		{`INSERT INTO locations (id, tenant_id, name, static_ips, gps_lat, gps_lng) VALUES ($1, $2, 'loc', '{203.0.113.0/24}', 35.9, 14.5)`,
			[]any{w.location, w.tenant}},
		{`INSERT INTO employees (id, tenant_id, location_id, full_name, status) VALUES ($1, $2, $3, 'Emp', 'active')`,
			[]any{w.employee, w.tenant, w.location}},
		{`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
		  VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 5, 'active')`, []any{w.live, w.tenant, w.location}},
		{`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, status, app_key_ref)
		  VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 'active', $4)`,
			[]any{w.stamped, w.tenant, w.location, bytes.Repeat([]byte{0xBE, 0xEF}, 22)}},
		{`UPDATE tags SET encoded_at = clock_timestamp() WHERE uid = $1`, []any{w.stamped}},
		{`INSERT INTO tags (uid, tenant_id, aes_key_ref, status) VALUES ($1, $2, decode(repeat('dead', 22), 'hex'), 'unassigned')`,
			[]any{w.stock, w.tenant}},
		{`INSERT INTO transactions (id, tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel)
		  VALUES ($1, $2, $3, $4, $5, now(), 'flag', 'nfc')`, []any{w.tx, w.tenant, w.employee, w.location, w.live}},
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 't138', $3, $4, 'owner')`,
			[]any{admin, w.tenant, "t138-" + admin.String() + "@example.test", fixtures.UnusablePasswordHash}},
		{`INSERT INTO admin_sessions (id, tenant_id, admin_user_id, token_hash, revoked_at) VALUES ($1, $2, $3, $4, clock_timestamp())`,
			[]any{w.adminSession, w.tenant, admin, "t138-" + uuid.NewString()}},
	} {
		if _, err := tx.Exec(ctx, s.sql, s.args...); err != nil {
			t.Fatalf("world: %s: %v", s.sql, err)
		}
	}
	w.operator = opNewActive(t, ctx, tx).id
	return w
}

// TestTriggerExecute00061_EachOfTheNineFiresForARoleThatMayNotExecuteIt: for each of the nine, a
// write by a role that is neither the owner nor a superuser and may NOT execute the function
// (has_function_privilege false, read in the same savepoint, after any grant the case needs) fires
// the trigger: the refusal is the trigger's own -- its SQLSTATE AND its message, so a privilege
// refusal (42501) or another constraint cannot pass for it -- or, for the clock, its effect.
//
// A case whose role lacks the TABLE privilege for the write (tappa_app holds no UPDATE or DELETE on
// transactions, no UPDATE of tags.app_key_ref; the definer no INSERT of operator_audit_log.at)
// grants exactly that, in its savepoint and rolled back -- never the function's EXECUTE. Every
// other case is a write the role makes in production. The TRUNCATE guards (00021, statement-level,
// bound to tappa_forbid_mutation) are not driven here: a TRUNCATE takes ACCESS EXCLUSIVE on a table
// every other package writes; the function, and the rule, are the row triggers' own.
func TestTriggerExecute00061_EachOfTheNineFiresForARoleThatMayNotExecuteIt(t *testing.T) {
	ctx, tx := opTx(t)
	w := t138NewWorld(t, ctx, tx)
	marker := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	cases := []struct {
		fn, role string
		grant    string // a table privilege the write needs and the role lacks; "" for none
		sql      string
		args     []any
		code     string // the trigger's SQLSTATE; "" for the clock, read by its effect
		msg      string // a fragment of the trigger's own message
	}{
		{"public.tappa_forbid_mutation()", "tappa_app", `GRANT UPDATE, DELETE ON public.transactions TO tappa_app`,
			`UPDATE transactions SET note = 'rewritten' WHERE id = $1`, []any{w.tx},
			sqlstateRestrictViolation, "append-only table transactions: UPDATE is forbidden"},
		{"public.tappa_forbid_mutation()", "tappa_app", `GRANT UPDATE, DELETE ON public.transactions TO tappa_app`,
			`DELETE FROM transactions WHERE id = $1`, []any{w.tx},
			sqlstateRestrictViolation, "append-only table transactions: DELETE is forbidden"},
		// The line breaks are tagsinventory_test.go's house style (TestTags00013_CounterIsMonotonicButRetireIsNot
		// says why): R4 is line-local, and this rewind is meant to be refused by the trigger.
		{"public.tappa_forbid_counter_rewind()", "tappa_app", "",
			`UPDATE tags
			 SET last_ctr = 1
			 WHERE uid = $1`, []any{w.live},
			sqlstateRestrictViolation, "read counter is monotonic on tags"},
		{"public.tappa_forbid_revocation_reset()", "tappa_app", "",
			`UPDATE admin_sessions SET revoked_at = NULL WHERE id = $1`, []any{w.adminSession},
			sqlstateRestrictViolation, "revocation is monotonic on admin_sessions"},
		{"public.tappa_forbid_app_key_ref_rewrite()", "tappa_app", `GRANT UPDATE (app_key_ref) ON public.tags TO tappa_app`,
			`UPDATE tags SET app_key_ref = NULL WHERE uid = $1`, []any{w.stamped},
			sqlstateRestrictViolation, "application master key ref is write-once on tags"},
		{"public.tappa_forbid_encoded_at_at_insert()", "tappa_app", "",
			`INSERT INTO tags (uid, tenant_id, aes_key_ref, status, encoded_at) VALUES ($1, $2, decode(repeat('dead', 22), 'hex'), 'unassigned', clock_timestamp())`,
			[]any{randUID(t), w.tenant},
			sqlstateRestrictViolation, "the encoded marker is not settable at INSERT on tags"},
		{"public.tappa_forbid_encoded_at_rewrite()", "tappa_app", "",
			`UPDATE tags SET encoded_at = NULL WHERE uid = $1`, []any{w.stamped},
			sqlstateRestrictViolation, "encoded marker is write-once on tags"},
		{"public.tappa_forbid_unencoded_activation()", "tappa_app", "",
			`UPDATE tags SET status = 'active', location_id = $2 WHERE uid = $1`, []any{w.stock, w.location},
			sqlstateRestrictViolation, "cannot go into service on tags"},
		{"public.tappa_check_transaction_review()", "tappa_app", "",
			`INSERT INTO transaction_reviews (id, tenant_id, transaction_id, reviewer_id, outcome) VALUES ($1, $2, $3, $4, 'approved')`,
			[]any{uuid.New(), w.tenant, w.tx, w.employee},
			sqlstateCheckViolation, "self-review forbidden"},
		{"public.tappa_audit_at_is_the_wall_clock()", "tappa_opdefiner", `GRANT INSERT (at) ON public.operator_audit_log TO tappa_opdefiner`,
			`INSERT INTO operator_audit_log (kind, target_admin_id, at) VALUES ('login_failed', $1, $2)`, []any{w.operator, marker},
			"", ""},
	}

	var covered []string
	for _, c := range cases {
		what := c.fn + " as " + c.role + ": " + c.sql
		holdingSavepoint(t, tx, false, func(sp pgx.Tx) {
			if c.grant != "" {
				if _, err := sp.Exec(ctx, c.grant); err != nil {
					t.Fatalf("%s: the case's table grant: %v", what, err)
				}
			}
			var may, super bool
			if err := sp.QueryRow(ctx, `SELECT has_function_privilege($1, $2, 'EXECUTE'),
			                                   (SELECT rolsuper FROM pg_roles WHERE rolname = $1)`, c.role, c.fn).Scan(&may, &super); err != nil {
				t.Fatal(err)
			}
			if may || super {
				t.Fatalf("PREMISE (%s): the role may EXECUTE the function (%v) or is a superuser (%v) -- 00061 did not hold", what, may, super)
			}
			err := opExecAs(t, ctx, sp, c.role, c.sql, c.args...)
			if c.code == "" {
				if err != nil {
					t.Fatalf("%s: %v", what, err)
				}
				var at time.Time
				if err := sp.QueryRow(ctx, `SELECT at FROM operator_audit_log WHERE target_admin_id = $1`, w.operator).Scan(&at); err != nil {
					t.Fatal(err)
				}
				if at.Equal(marker) {
					t.Errorf("%s: the row keeps the written at %v -- the clock trigger did not fire", what, at)
				}
				return
			}
			code, msg := opCode(err)
			if code != c.code || !strings.Contains(msg, c.msg) {
				t.Errorf("%s: %s %q (%v), want the trigger's %s %q", what, code, msg, err, c.code, c.msg)
			}
		})
		covered = append(covered, c.fn)
	}

	// Every one of the nine is driven, and each is bound to at least one trigger -- so a tenth
	// function cannot hide behind this list, nor a name in it be a dead one.
	var bound []string
	rows, err := tx.Query(ctx, `SELECT DISTINCT t.tgfoid::regprocedure::text FROM pg_trigger t
	                             WHERE NOT t.tgisinternal AND t.tgfoid::regprocedure::text = ANY($1)`,
		t138Bare(t138Functions))
	if err != nil {
		t.Fatal(err)
	}
	if bound, err = pgx.CollectRows(rows, pgx.RowTo[string]); err != nil {
		t.Fatal(err)
	}
	for _, fn := range t138Functions {
		if !slices.Contains(covered, fn) {
			t.Errorf("%s has no case here", fn)
		}
		if !slices.Contains(bound, strings.TrimPrefix(fn, "public.")) {
			t.Errorf("%s is bound to no trigger", fn)
		}
	}
}

// t138Bare drops the schema, as regprocedure::text spells a function on the search path.
func t138Bare(fns []string) []string {
	out := make([]string, len(fns))
	for i, f := range fns {
		out[i] = strings.TrimPrefix(f, "public.")
	}
	return out
}

// TestTriggerExecute00061_NoRoleButTheOwnerMayCallOrBindThem: what the REVOKE closes. For each
// of the nine, tappa_app's direct call is refused for the privilege (42501, before the function's
// own "can only be called as triggers"), and so is binding it to a temporary table of its own --
// the one path T137's review left (the trigger would run with tappa_app's own rights, harmless,
// but nothing needs it). CONTROL: with the function's EXECUTE granted in the savepoint, the same
// CREATE TRIGGER passes -- so EXECUTE, and nothing else in the statement, is what refuses it.
func TestTriggerExecute00061_NoRoleButTheOwnerMayCallOrBindThem(t *testing.T) {
	ctx, tx := opTx(t)
	for _, fn := range t138Functions {
		bind := `CREATE TEMP TABLE zz_t138 (x int);
		         CREATE TRIGGER zz_t138 BEFORE INSERT ON pg_temp.zz_t138 FOR EACH ROW EXECUTE FUNCTION ` + fn
		opWant(t, opExecAs(t, ctx, tx, "tappa_app", `SELECT `+fn), sqlstateInsufficientPrivi, "tappa_app calls "+fn)
		opWant(t, opExecAs(t, ctx, tx, "tappa_app", bind), sqlstateInsufficientPrivi, "tappa_app binds "+fn+" to its temporary table")
		holdingSavepoint(t, tx, false, func(sp pgx.Tx) {
			if _, err := sp.Exec(ctx, `GRANT EXECUTE ON FUNCTION `+fn+` TO tappa_app`); err != nil {
				t.Fatal(err)
			}
			if err := opExecAs(t, ctx, sp, "tappa_app", bind); err != nil {
				t.Errorf("CONTROL: with EXECUTE granted, tappa_app's bind of %s: %v", fn, err)
			}
		})
	}
}

// TestTriggerExecute00061_DownKeepsTheRevokeAndUpIsIdempotent: 00061's Down does not give PUBLIC
// its EXECUTE back (its header says why) -- after it the catalogue is still exactly the allowlist,
// the nine `execute=-` lines included -- and an Up over that state passes again and changes
// nothing. The Up refuses (55000, nothing changed) when one of the nine is missing or is a
// DEFINER function (precondition), or when a role still holds an explicit EXECUTE a REVOKE … FROM
// PUBLIC does not reach (postcondition); the Down refuses when one is missing.
func TestTriggerExecute00061_DownKeepsTheRevokeAndUpIsIdempotent(t *testing.T) {
	ctx, tx := opTx(t)
	privCatalogPremise(t, ctx, tx)
	up, down := opMigrationSections(t, op00061File)
	sameAsTheAllowlist := func(q pgx.Tx, when string) {
		t.Helper()
		if u, m := privCatalogDiff(privCatalogSnapshot(t, ctx, q)); len(u)+len(m) != 0 {
			t.Errorf("%s: the catalogue differs from the allowlist: unexpected %q, missing %q", when, u, m)
		}
	}
	sameAsTheAllowlist(tx, "at HEAD")
	holdingSavepoint(t, tx, false, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, down, "00061 Down")
		sameAsTheAllowlist(sp, "after the Down")
		opRunSection(t, ctx, sp, up, "00061 Up again")
		sameAsTheAllowlist(sp, "after the Up again")

		for _, c := range []struct{ what, setup, section string }{
			{"Up with a function missing", `ALTER FUNCTION public.tappa_forbid_counter_rewind() RENAME TO zz_t138_gone`, up},
			{"Up with a DEFINER function", `ALTER FUNCTION public.tappa_forbid_encoded_at_rewrite() SECURITY DEFINER`, up},
			{"Up with an explicit grant to tappa_app", `GRANT EXECUTE ON FUNCTION public.tappa_forbid_mutation() TO tappa_app`, up},
			{"Down with a function missing", `ALTER FUNCTION public.tappa_check_transaction_review() RENAME TO zz_t138_gone`, down},
		} {
			opWant(t, holdingNestedRun(t, ctx, sp, c.setup+`;`+c.section), sqlstatePrerequisiteState, c.what)
		}
	})
}
