package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// adminflag_test.go -- migration 00083 (M14-19, ADR 0031 §8 step 5, §5.8; the plan's T9, S5, S18)
// against real Postgres: the deploy that turns TAPPA_ADMIN_IDENTITY on moves the END of every
// confirmation window opened while the flag was off to the flag's day + 7 days (and nothing else of
// it), and takes tappa_app's EXECUTE on identity_create_for_signup -- which no binary calls -- away.
//
// 🔴 NOTHING HERE COMMITS. Every case runs in a savepoint of bfTx's READ COMMITTED transaction: the
// Up's UPDATE reaches every committed window of the shared database (other packages' fixtures
// included), and bfTx holds the operator-tables lock EXCLUSIVE, so no other package writes an
// identity meanwhile; the savepoint's row locks go with its rollback.

const afFile = "00083_prepare_the_admin_identity_flag.sql"

// afRun runs a migration section's text in a nested savepoint of sp, released on success (its effects
// stay for the case to read) and rolled back on failure; it returns the failure.
func afRun(t *testing.T, ctx context.Context, sp pgx.Tx, sql string) error {
	t.Helper()
	nested, err := sp.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, runErr := nested.Conn().PgConn().Exec(ctx, sql).ReadAll(); runErr != nil {
		if err := nested.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		return runErr
	}
	if err := nested.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return nil
}

// afWindow is one identity's window as text: scope|opened_at|open_until ("||" for no window).
func afWindow(t *testing.T, ctx context.Context, q pgx.Tx, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := q.QueryRow(ctx, `SELECT concat_ws('|', coalesce(confirm_scope, ''), coalesce(confirm_opened_at::text, ''), coalesce(confirm_open_until::text, ''))
	                             FROM admin_identities WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read the window: %v", err)
	}
	return s
}

// afIdentityWithWindow births an identity whose window opened `opened` ago (a negative interval: in the
// future) and ends `until` from now -- written as the openers write it (00072 / 00074 / 00077 / 00078:
// the three columns together, the address verified).
func afIdentityWithWindow(t *testing.T, ctx context.Context, q pgx.Tx, scope, opened, until string) uuid.UUID {
	t.Helper()
	id := aiIdentity(t, ctx, q, aiEmail())
	aiExec(t, ctx, q, `UPDATE admin_identities
	                      SET email_verified_at = now() - $2::interval, confirm_scope = $4,
	                          confirm_opened_at = now() - $2::interval, confirm_open_until = now() + $3::interval
	                    WHERE id = $1`, id, opened, until, scope)
	return id
}

// afWorld is the four shapes of the window (ADR 0031 §5.8) and a reset list around the open one.
type afWorld struct {
	open, expired, later, none uuid.UUID
	before                     map[uuid.UUID]string
	listed                     int64
}

// afNewWorld writes the world in q and records each window as it stands.
//
//	open     reset scope, opened two days ago, ends in five days (its event + 7): the flag-off window
//	         the person has not seen; two rows linked to it, one BEFORE the opening (on the reset
//	         list: linked_at < confirm_opened_at) and one after it (not on it).
//	expired  first_verify, opened ten days ago, ended three days ago: opened while off, never shown.
//	later    a window that ends AFTER the flag's day + 7 (opened an hour after this file's instant, as
//	         one opened once the flag is on would be): not shortened.
//	none     no window ("Keep all" pressed, or never opened): the three columns NULL.
func afNewWorld(t *testing.T, ctx context.Context, q pgx.Tx) afWorld {
	t.Helper()
	w := afWorld{
		open:    afIdentityWithWindow(t, ctx, q, "reset", "2 days", "5 days"),
		expired: afIdentityWithWindow(t, ctx, q, "first_verify", "10 days", "-3 days"),
		later:   afIdentityWithWindow(t, ctx, q, "reset", "-1 hour", "7 days 1 hour"),
		none:    aiIdentity(t, ctx, q, aiEmail()),
	}
	early := aiAdmin(t, ctx, q, aiTenant(t, ctx, q), aiEmail())
	late := aiAdmin(t, ctx, q, aiTenant(t, ctx, q), aiEmail())
	aiExec(t, ctx, q, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now() - interval '30 days' WHERE id = $1`, early, w.open)
	aiExec(t, ctx, q, `UPDATE admin_users SET identity_id = $2, linked_via = 'password', linked_at = now() - interval '1 day' WHERE id = $1`, late, w.open)
	w.listed = afListed(t, ctx, q, w.open)
	if w.listed != 1 {
		t.Fatalf("PREMISE: the open window's reset list holds %d rows, want 1", w.listed)
	}
	w.before = map[uuid.UUID]string{}
	for _, id := range []uuid.UUID{w.open, w.expired, w.later, w.none} {
		w.before[id] = afWindow(t, ctx, q, id)
	}
	return w
}

// afListed counts the rows on an identity's reset list (00079's test, `linked_at < confirm_opened_at`).
func afListed(t *testing.T, ctx context.Context, q pgx.Tx, id uuid.UUID) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT count(*) FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id
	                          WHERE i.id = $1 AND a.linked_at < i.confirm_opened_at`, id)
}

// afFindings is what the Up got wrong about the world: each window's end, opening and scope, and the
// reset list.
func afFindings(t *testing.T, ctx context.Context, q pgx.Tx, w afWorld) []string {
	t.Helper()
	var out []string
	flagDay := func(id uuid.UUID) bool {
		return opInt(t, ctx, q, `SELECT count(*) FROM admin_identities WHERE id = $1 AND confirm_open_until = now() + interval '7 days'`, id) == 1
	}
	keptButEnd := func(name string, id uuid.UUID) {
		b, a := strings.Split(w.before[id], "|"), strings.Split(afWindow(t, ctx, q, id), "|")
		if b[0] != a[0] || b[1] != a[1] {
			out = append(out, name+": the scope or the opening changed ("+w.before[id]+" -> "+strings.Join(a, "|")+")")
		}
	}
	for name, id := range map[string]uuid.UUID{"open": w.open, "expired": w.expired} {
		if !flagDay(id) {
			out = append(out, name+": the end is not the flag's day + 7 days ("+afWindow(t, ctx, q, id)+")")
		}
		keptButEnd(name, id)
	}
	if got := afWindow(t, ctx, q, w.later); got != w.before[w.later] {
		out = append(out, "later: a window ending after the flag's day + 7 was changed ("+w.before[w.later]+" -> "+got+")")
	}
	if got := afWindow(t, ctx, q, w.none); got != "||" {
		out = append(out, "none: an identity with no window has one now ("+got+")")
	}
	if n := afListed(t, ctx, q, w.open); n != w.listed {
		out = append(out, "open: the reset list changed")
	}
	return out
}

// TestAdminFlag00083_OnlyTheWindowsEndMovesToTheFlagsDay is job 1 (S5, T9): on a world of the four
// shapes -- with the Up taken back first (its Down leaves the windows alone) -- the Up gives the open
// and the expired window (both opened while the flag was off) the end now() + 7 days with their
// opening and scope byte for byte, the reset list unchanged; a window ending later keeps its end; an
// identity with no window gets none.
//
// MUTATIONS, in the test (each in a savepoint, the Up's text cut): the opening moved too -> red; a
// bare now() + 7 in place of GREATEST -> the later window shortened, red; the predicate narrowed to
// windows still open -> the expired one is not moved (the Up's own postcondition refuses it, or the
// findings show it); the predicate removed -> GREATEST(NULL, x) gives a window-less identity an end,
// 00069's admin_identities_confirm_window_whole refuses the Up.
func TestAdminFlag00083_OnlyTheWindowsEndMovesToTheFlagsDay(t *testing.T) {
	ctx, tx := bfTx(t)
	up, down := opMigrationSections(t, afFile)

	t.Run("the four shapes", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			opRunSection(t, ctx, sp, down, "00083 Down")
			w := afNewWorld(t, ctx, sp)
			if err := afRun(t, ctx, sp, up); err != nil {
				t.Fatalf("00083 Up: %v", err)
			}
			for _, f := range afFindings(t, ctx, sp, w) {
				t.Error(f)
			}
		})
	})

	const set = "   SET confirm_open_until = GREATEST(confirm_open_until, now() + interval '7 days')\n"
	const where = " WHERE confirm_opened_at IS NOT NULL;\n"
	for _, m := range []struct{ name, cut, put string }{
		{"the opening moved too", set, "   SET confirm_open_until = GREATEST(confirm_open_until, now() + interval '7 days'), confirm_opened_at = now() - interval '1 second'\n"},
		{"a bare now() + 7 days", set, "   SET confirm_open_until = now() + interval '7 days'\n"},
		{"only the windows still open", where, " WHERE confirm_opened_at IS NOT NULL AND confirm_open_until > now();\n"},
		{"the predicate removed", where, ";\n"},
	} {
		t.Run("mutation/"+m.name, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				if strings.Count(up, m.cut) != 1 {
					t.Fatalf("PREMISE: the Up holds %q %d times", m.cut, strings.Count(up, m.cut))
				}
				opRunSection(t, ctx, sp, down, "00083 Down")
				w := afNewWorld(t, ctx, sp)
				err := afRun(t, ctx, sp, strings.Replace(up, m.cut, m.put, 1))
				if err == nil && len(afFindings(t, ctx, sp, w)) == 0 {
					t.Fatal("the mutated Up passed and left the world as the true one does: the test does not see it")
				}
				t.Logf("caught: Up error %v", err)
			})
		})
	}
}

// TestAdminFlag00083_TappaAppCannotCallTheSignupDoor is job 2 (S18): at HEAD tappa_app holds no
// EXECUTE on identity_create_for_signup and a call as tappa_app is 42501; the function is still there
// and still its owner's (DROP is contract B's); the Down gives the grant back and the Up takes it again.
// MUTATIONS: the Up without its REVOKE -> its postcondition refuses it (55000); without the REVOKE and
// the postcondition -> tappa_app keeps EXECUTE, which this test's own reading flags.
func TestAdminFlag00083_TappaAppCannotCallTheSignupDoor(t *testing.T) {
	ctx, tx := bfTx(t)
	up, down := opMigrationSections(t, afFile)
	const fn = `'public.identity_create_for_signup(uuid,uuid)'::regprocedure`
	can := func(q pgx.Tx, role string) bool {
		return opInt(t, ctx, q, `SELECT count(*) FROM pg_proc WHERE oid = `+fn+` AND has_function_privilege($1, oid, 'EXECUTE')`, role) == 1
	}
	if can(tx, "tappa_app") {
		t.Fatal("tappa_app holds EXECUTE on identity_create_for_signup at HEAD")
	}
	if !can(tx, "tappa_identitydefiner") {
		t.Fatal("the function is gone or its owner cannot call it: its DROP is contract B's, not M14-19's")
	}
	err := opAs(t, ctx, tx, "tappa_app", func(app pgx.Tx) error {
		_, err := app.Exec(ctx, `SELECT identity_create_for_signup($1, $2)`, uuid.New(), uuid.New())
		return err
	})
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" {
		t.Fatalf("a call as tappa_app answered %v, want 42501", err)
	}

	t.Run("Down gives it back, Up takes it again", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			opRunSection(t, ctx, sp, down, "00083 Down")
			if !can(sp, "tappa_app") {
				t.Fatal("the Down did not give tappa_app its EXECUTE back")
			}
			opRunSection(t, ctx, sp, up, "00083 Up")
			if can(sp, "tappa_app") {
				t.Fatal("the Up left tappa_app its EXECUTE")
			}
		})
	})

	const revoke = "REVOKE EXECUTE ON FUNCTION public.identity_create_for_signup(uuid, uuid) FROM tappa_app;\n"
	t.Run("mutation/the REVOKE removed", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			if strings.Count(up, revoke) != 1 {
				t.Fatal("PREMISE: the Up's REVOKE is not where the test cuts it")
			}
			opRunSection(t, ctx, sp, down, "00083 Down")
			bfWantRefusal(t, afRun(t, ctx, sp, strings.Replace(up, revoke, "", 1)), "still callable by a role other than its owner", "the Up without its REVOKE")
		})
	})
	t.Run("mutation/the REVOKE and the postcondition removed", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			i := strings.Index(up, "-- 3. POSTCONDITION")
			if i < 0 {
				t.Fatal("PREMISE: the Up has no postcondition section")
			}
			opRunSection(t, ctx, sp, down, "00083 Down")
			if err := afRun(t, ctx, sp, strings.Replace(up[:i], revoke, "", 1)); err != nil {
				t.Fatalf("the cut Up: %v", err)
			}
			if !can(sp, "tappa_app") {
				t.Fatal("CONTROL: without the REVOKE tappa_app should still hold EXECUTE")
			}
		})
	})
}

// TestAdminFlag00083_ARoleThatCannotSeeTheWindowsIsRefused: the Up run by a role that is neither
// superuser nor BYPASSRLS (tappa_app here) is refused by its first statement, with 00082's reason --
// under FORCE ROW LEVEL SECURITY it would move no window and say nothing.
func TestAdminFlag00083_ARoleThatCannotSeeTheWindowsIsRefused(t *testing.T) {
	ctx, tx := bfTx(t)
	up, _ := opMigrationSections(t, afFile)
	err := opAs(t, ctx, tx, "tappa_app", func(app pgx.Tx) error {
		_, err := app.Conn().PgConn().Exec(ctx, up).ReadAll()
		return err
	})
	bfWantRefusal(t, err, "neither superuser nor BYPASSRLS", "the Up as tappa_app")
}

// TestIdentityCreateForSignup_NoBinaryCallsIt pins S18's premise -- the reason 00083 may take the
// grant in the deploy that ships it (ADR 0031 §8: never REVOKE what the previous binary calls): no
// non-test Go file, no templ file and no sqlc query of the module names identity_create_for_signup, so
// neither this binary nor the old pod of its rollout can call it. (main 8d94a95e, the binary before
// this one, was read the same way when M14-19 was built: its one mention was an unreferenced constant
// the linker drops -- the built cmd/tappa, cmd/opadmin and sandbox binaries carry no such string.)
// CONTROL: the same walk over the test files finds this package's own test of the function, so the walk
// reads what it says it reads. A name built at run time would pass the scan; what closes that is the
// privilege itself (TestAdminFlag00083_TappaAppCannotCallTheSignupDoor).
func TestIdentityCreateForSignup_NoBinaryCallsIt(t *testing.T) {
	root := filepath.Join("..", "..")
	const name = "identity_create_for_signup"
	var hits, controls []string
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", ".tools", "node_modules", "migrations":
				return filepath.SkipDir
			}
			return nil
		}
		code := strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".templ") ||
			(strings.HasSuffix(path, ".sql") && strings.Contains(filepath.ToSlash(path), "db/queries/"))
		if !code {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(path, "_test.go") {
			if strings.Contains(string(b), name) {
				controls = append(controls, path)
			}
			return nil
		}
		scanned++
		if strings.Contains(string(b), name) {
			hits = append(hits, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 100 || len(controls) == 0 {
		t.Fatalf("CONTROL: the walk read %d non-test files and found the name in %d test files -- it is not reading the module", scanned, len(controls))
	}
	for _, h := range hits {
		t.Errorf("%s names %s: a caller would need the EXECUTE 00083 took (M14-19, S18); the function waits for contract B's DROP", h, name)
	}
}

// afRunbookStatement returns the SQL statement of deploy/README.md's "M14-19 — bayrak" section that
// holds marker: from the SELECT that opens it to its semicolon. The marker must be in the document
// exactly once.
func afRunbookStatement(t *testing.T, marker string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	if n := strings.Count(doc, marker); n != 1 {
		t.Fatalf("PREMISE: deploy/README.md holds %q %d times, want 1", marker, n)
	}
	at := strings.Index(doc, marker)
	from := strings.LastIndex(doc[:at+len(marker)], "SELECT ")
	end := strings.Index(doc[at:], ";\n")
	if from < 0 || end < 0 {
		t.Fatalf("PREMISE: %q is not inside a SELECT ... ; statement", marker)
	}
	return doc[from : at+end]
}

// TestAdminFlag_TheRunbookCountsAreRunAsWritten runs the "M14-19 — bayrak" section's statements as the
// document writes them, on a world written in a savepoint, and reads each count's CHANGE (the shared
// database's own rows stay where they are): the S19 precheck, the windows 00083 will move, the
// post-deploy reading (goose at the newest file, the door shut to tappa_app and kept by its owner, the
// windows ending early) and the three weekly counts.
func TestAdminFlag_TheRunbookCountsAreRunAsWritten(t *testing.T) {
	ctx, tx := bfTx(t)
	s19 := afRunbookStatement(t, "AS linked_but_refused")
	toMove := afRunbookStatement(t, "AS windows_to_move,")
	door := afRunbookStatement(t, "AS app_calls_signup_door,")
	goose := afRunbookStatement(t, "AS goose_version FROM goose_db_version")
	early := afRunbookStatement(t, "AS windows_ending_early")
	unlinked := afRunbookStatement(t, "AS unlinked_without_email")
	owners := afRunbookStatement(t, "AS tenants_single_owner_unlinked")
	refused := afRunbookStatement(t, "AS tenants_with_refused_rows")

	read := func(q pgx.Tx, sql string) string {
		rows, err := q.Query(ctx, sql)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		vals, err := pgx.CollectExactlyOneRow(rows, func(r pgx.CollectableRow) ([]any, error) { return r.Values() })
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		parts := make([]string, len(vals))
		for i, v := range vals {
			parts[i] = strings.TrimSpace(strings.ReplaceAll(fmt.Sprint(v), "\n", " "))
		}
		return strings.Join(parts, "|")
	}
	delta := func(before, after string) string {
		b, a := strings.Split(before, "|"), strings.Split(after, "|")
		out := make([]string, len(a))
		for i := range a {
			var x, y int64
			if _, err := fmt.Sscan(b[i], &x); err != nil {
				t.Fatalf("not a count: %q", before)
			}
			if _, err := fmt.Sscan(a[i], &y); err != nil {
				t.Fatalf("not a count: %q", after)
			}
			out[i] = fmt.Sprint(y - x)
		}
		return strings.Join(out, "|")
	}

	newest := 0
	entries, err := os.ReadDir(filepath.Join("..", "..", "db", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		var n int
		if _, err := fmt.Sscanf(e.Name(), "%05d_", &n); err == nil && n > newest {
			newest = n
		}
	}
	if got := read(tx, goose); got != fmt.Sprint(newest) {
		t.Errorf("the runbook's goose reading is %s, the newest migration file is %05d", got, newest)
	}
	if got := read(tx, door); got != "false|true" {
		t.Errorf("the runbook's door reading is %s, want false|true (shut to tappa_app, kept by its owner)", got)
	}

	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		counts := []string{s19, toMove, early, unlinked, owners, refused}
		before := make([]string, len(counts))
		for i, c := range counts {
			before[i] = read(sp, c)
		}
		owner := func(tenant uuid.UUID, email any, status string) uuid.UUID {
			id := uuid.New()
			aiExec(t, ctx, sp, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			                    VALUES ($1, $2, 'M14-19', $3, $4, 'owner', $5)`, id, tenant, email, opFakeDigest("a"), status)
			return id
		}
		link := func(row uuid.UUID) {
			aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now() WHERE id = $1`,
				row, aiIdentity(t, ctx, sp, aiEmail()))
		}
		// A: one active owner, unlinked. B: two active owners, both unlinked. C: a linked active owner
		// beside an unlinked one, and a disabled row with no address. D: its one active owner refused.
		// E: a linked row that also carries a refusal stamp (S19's inconsistency).
		a, b, c, d, e := aiTenant(t, ctx, sp), aiTenant(t, ctx, sp), aiTenant(t, ctx, sp), aiTenant(t, ctx, sp), aiTenant(t, ctx, sp)
		owner(a, aiEmail(), "active")
		owner(b, aiEmail(), "active")
		owner(b, aiEmail(), "active")
		link(owner(c, aiEmail(), "active"))
		owner(c, aiEmail(), "active")
		owner(c, nil, "disabled")
		aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, owner(d, aiEmail(), "active"))
		odd := owner(e, aiEmail(), "active")
		link(odd)
		aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, odd)
		// Windows: one open (its event + 7), one expired, one ending in a day -- early by the reading.
		afIdentityWithWindow(t, ctx, sp, "reset", "2 days", "5 days")
		afIdentityWithWindow(t, ctx, sp, "first_verify", "10 days", "-3 days")
		afIdentityWithWindow(t, ctx, sp, "reset", "6 days", "1 day")

		want := []struct{ what, delta string }{
			{"S19 linked_but_refused", "1"},
			{"windows_to_move|windows_expired", "3|1"},
			{"windows|windows_ending_early", "3|3"},
			// A's one, B's two and C's unlinked owner are active; C's disabled row has no address. D's
			// refused row and E's linked one are neither.
			{"unlinked_active_rows|unlinked_rows|unlinked_without_email", "4|5|1"},
			// A, B, D and E have no active owner that is linked and not refused (E's link carries a
			// refusal); A, D and E have one active owner. C's linked owner keeps C out.
			{"tenants_no_linked_active_owner|tenants_single_owner_unlinked", "4|3"},
			{"refused_rows|tenants_with_refused_rows", "2|2"},
		}
		for i, c := range counts {
			if got := delta(before[i], read(sp, c)); got != want[i].delta {
				t.Errorf("%s moved by %s, want %s", want[i].what, got, want[i].delta)
			}
		}
	})
}
