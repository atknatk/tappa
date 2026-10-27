package db

// identityaccount_test.go -- M14-18 (migration 00079; ADR 0031 §5.2's note of 2026-10-27, §5.8): the
// confirmation window's reader and "Keep all", against real Postgres as tappa_app, everything rolled
// back (opTx). The signatures and the belt are identitycatalog_test.go's / identitybelt_test.go's.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const ia00079File = "00079_list_and_keep_the_confirmation_window.sql"

// iaWindow reads identity_confirm_window as tappa_app through the Go caller's own SQL.
func iaWindow(t *testing.T, ctx context.Context, tx pgx.Tx, session string) []ConfirmWindowRow {
	t.Helper()
	var out []ConfirmWindowRow
	err := opAs(t, ctx, tx, "tappa_app", func(sp pgx.Tx) error {
		rows, err := sp.Query(ctx, confirmWindowSQL, session)
		if err != nil {
			return err
		}
		out, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (ConfirmWindowRow, error) {
			var c ConfirmWindowRow
			err := r.Scan(&c.Verified, &c.Scope, &c.OpenUntil, &c.TenantID, &c.TenantName, &c.Role, &c.Refused, &c.LastActiveOwner)
			return c, err
		})
		return err
	})
	if err != nil {
		t.Fatalf("identity_confirm_window as tappa_app: %v", err)
	}
	return out
}

// iaListed is the window's list (the rows after the header) as tenant -> refused.
func iaListed(t *testing.T, rows []ConfirmWindowRow) map[uuid.UUID]ConfirmWindowRow {
	t.Helper()
	out := map[uuid.UUID]ConfirmWindowRow{}
	for i, r := range rows {
		if i == 0 {
			if r.TenantID != nil {
				t.Fatalf("the first row is not the header: %+v", r)
			}
			continue
		}
		if r.TenantID == nil {
			t.Fatalf("row %d after the header names no business", i)
		}
		out[*r.TenantID] = r
	}
	return out
}

// iaWorld is one person: memberships in A (backfill; the session's), B (password), C (claim), a row
// in D refused in the window, a row in E of ANOTHER address refused at the same moment, and a row in
// F linked AFTER the window opened (reconcile).
type iaWorld struct {
	p                ipPerson
	a, b, c, d, e, f uuid.UUID
	ma, mb, mc, md   uuid.UUID
	session          string
}

func iaNewWorld(t *testing.T, ctx context.Context, tx pgx.Tx, scope string) iaWorld {
	t.Helper()
	w := iaWorld{p: ipNewPerson(t, ctx, tx)}
	w.a, w.b, w.c = idTenant(t, ctx, tx, "window A"), idTenant(t, ctx, tx, "window B"), idTenant(t, ctx, tx, "window C")
	w.d, w.e, w.f = idTenant(t, ctx, tx, "window D"), idTenant(t, ctx, tx, "window E"), idTenant(t, ctx, tx, "window F")
	w.ma, w.mb, w.mc = ipMember(t, ctx, tx, w.a, w.p.email, w.p.digest), ipMember(t, ctx, tx, w.b, w.p.email, w.p.digest), ipMember(t, ctx, tx, w.c, w.p.email, w.p.digest)
	ipLink(t, ctx, tx, w.ma, w.p.id, "backfill", "3 days")
	ipLink(t, ctx, tx, w.mb, w.p.id, "password", "3 days")
	ipLink(t, ctx, tx, w.mc, w.p.id, "claim", "3 days")
	// The window opens a minute ago; D was refused inside it, E (another address) too.
	aiExec(t, ctx, tx, `UPDATE admin_identities SET email_verified_at = now() - interval '1 minute', confirm_opened_at = now() - interval '1 minute',
	                           confirm_open_until = now() + interval '7 days', confirm_scope = $2 WHERE id = $1`, w.p.id, scope)
	w.md = ipMember(t, ctx, tx, w.d, w.p.email, w.p.digest)
	aiExec(t, ctx, tx, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, w.md)
	other := ipMember(t, ctx, tx, w.e, ipEmail(), w.p.digest)
	aiExec(t, ctx, tx, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, other)
	mf := ipMember(t, ctx, tx, w.f, w.p.email, w.p.digest)
	ipLink(t, ctx, tx, mf, w.p.id, "reconcile", "0 seconds")
	w.session = ipSession(t, ctx, tx, w.a, w.ma, ipStale)
	return w
}

// TestIdentityConfirmWindow00079_ListsEachScopesRowsAndTheRefusedOnes: the header first (verified,
// the scope, the end); first_verify lists the backfill / reconcile / signup links only (A, and F --
// reconcile, whatever its linked_at); reset lists every link made BEFORE the window opened (A, B and
// C -- claim included, T1 -- not F); both list D, refused in this window, as refused; neither lists
// E, refused at the same moment under another address. A stale session reads it (a read needs no
// fresh one).
func TestIdentityConfirmWindow00079_ListsEachScopesRowsAndTheRefusedOnes(t *testing.T) {
	cases := []struct {
		scope    string
		want     []string // the listed memberships, by label
		notWant  []string
		refused  string
		refusedN int
	}{
		{"first_verify", []string{"a", "f"}, []string{"b", "c", "e"}, "d", 1},
		{"reset", []string{"a", "b", "c"}, []string{"f", "e"}, "d", 1},
	}
	ctx, tx := opTx(t)
	for _, c := range cases {
		aiSavepoint(t, ctx, tx, func(tx pgx.Tx) {
			w := iaNewWorld(t, ctx, tx, c.scope)
			label := map[string]uuid.UUID{"a": w.a, "b": w.b, "c": w.c, "d": w.d, "e": w.e, "f": w.f}
			rows := iaWindow(t, ctx, tx, w.session)
			if len(rows) == 0 {
				t.Fatal("no rows for a live linked session with an open window")
			}
			head := rows[0]
			if !head.Verified || head.Scope == nil || *head.Scope != c.scope || head.OpenUntil == nil || !head.OpenUntil.After(time.Now()) {
				t.Fatalf("header %+v: want verified, scope %s, an end in the future", head, c.scope)
			}
			listed := iaListed(t, rows)
			for _, l := range c.want {
				r, ok := listed[label[l]]
				if !ok || r.Refused {
					t.Errorf("%s: membership %s not listed as a membership (%+v, %v)", c.scope, l, r, ok)
				}
			}
			for _, l := range c.notWant {
				if _, ok := listed[label[l]]; ok {
					t.Errorf("%s: %s is listed", c.scope, l)
				}
			}
			if r, ok := listed[label[c.refused]]; !ok || !r.Refused || r.LastActiveOwner {
				t.Errorf("%s: the row refused in this window: %+v, %v (want listed, refused, no owner warning)", c.scope, r, ok)
			}
			n := 0
			for _, r := range listed {
				if r.Refused {
					n++
				}
			}
			if n != c.refusedN {
				t.Errorf("%s: %d refused rows listed, want %d", c.scope, n, c.refusedN)
			}
		})
	}
}

// TestIdentityConfirmWindow00079_WarnsOnlyForTheLastActiveOwner: A's owner row is its business's only
// active owner -> the warning; B holds another active owner -> none; C's other owner is disabled ->
// the warning again; a manager row -> none. CONTROL: B's other owner refused (unlinked_at) does not
// count either.
func TestIdentityConfirmWindow00079_WarnsOnlyForTheLastActiveOwner(t *testing.T) {
	ctx, tx := opTx(t)
	w := iaNewWorld(t, ctx, tx, "reset")
	other := ipMember(t, ctx, tx, w.b, ipEmail(), w.p.digest)
	disabled := ipMember(t, ctx, tx, w.c, ipEmail(), w.p.digest)
	aiExec(t, ctx, tx, `UPDATE admin_users SET status = 'disabled' WHERE id = $1`, disabled)
	listed := iaListed(t, iaWindow(t, ctx, tx, w.session))
	for _, c := range []struct {
		name   string
		tenant uuid.UUID
		want   bool
	}{{"the only owner", w.a, true}, {"another active owner", w.b, false}, {"the other owner disabled", w.c, true}} {
		if got := listed[c.tenant].LastActiveOwner; got != c.want {
			t.Errorf("%s: last_active_owner = %v, want %v", c.name, got, c.want)
		}
	}
	aiExec(t, ctx, tx, `UPDATE admin_users SET role = 'manager' WHERE id = $1`, w.ma)
	if listed := iaListed(t, iaWindow(t, ctx, tx, w.session)); listed[w.a].LastActiveOwner {
		t.Error("a manager row carries the owner warning")
	}
	aiExec(t, ctx, tx, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, other)
	if listed := iaListed(t, iaWindow(t, ctx, tx, w.session)); !listed[w.b].LastActiveOwner {
		t.Error("CONTROL: B's refused owner still counted as an active owner")
	}
}

// TestIdentityConfirmWindow00079_ShowsNoListOutsideAnOpenWindow: a closed window (past its end), an
// unverified address and a window kept are a header alone (scope and end NULL); an unlinked
// session, a revoked one, and an unknown hash get nothing.
func TestIdentityConfirmWindow00079_ShowsNoListOutsideAnOpenWindow(t *testing.T) {
	ctx, tx := opTx(t)
	w := iaNewWorld(t, ctx, tx, "reset")
	headerOnly := func(name string, verified bool) {
		t.Helper()
		rows := iaWindow(t, ctx, tx, w.session)
		if len(rows) != 1 || rows[0].TenantID != nil || rows[0].Scope != nil || rows[0].OpenUntil != nil || rows[0].Verified != verified {
			t.Errorf("%s: %d rows %+v, want the header alone (verified=%v)", name, len(rows), rows, verified)
		}
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `UPDATE admin_identities SET confirm_opened_at = now() - interval '8 days', confirm_open_until = now() - interval '1 day' WHERE id = $1`, w.p.id)
		headerOnly("a window past its end", true)
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `UPDATE admin_identities SET email_verified_at = NULL WHERE id = $1`, w.p.id)
		headerOnly("an unverified address", false)
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		if n := ipRows(t, ctx, sp, keepAllSQL, w.session); n != 1 {
			t.Fatalf("keep all: %d rows", n)
		}
		headerOnly("a window kept", true)
	})
	plain := ipMember(t, ctx, tx, w.a, ipEmail(), w.p.digest)
	for _, c := range []struct{ name, hash string }{
		{"an unlinked row's session", ipSession(t, ctx, tx, w.a, plain, ipFresh)},
		{"an unknown hash", opRandHex(t)},
	} {
		if rows := iaWindow(t, ctx, tx, c.hash); len(rows) != 0 {
			t.Errorf("%s: %d rows, want none", c.name, len(rows))
		}
	}
	aiExec(t, ctx, tx, `UPDATE admin_sessions SET revoked_at = now() WHERE token_hash = $1`, w.session)
	if rows := iaWindow(t, ctx, tx, w.session); len(rows) != 0 {
		t.Errorf("a revoked session: %d rows, want none", len(rows))
	}
}

// TestIdentityConfirmWindow00079_ListsAtMostFiftyRows: 60 listed memberships -> the header and 49
// rows, 50 in all.
func TestIdentityConfirmWindow00079_ListsAtMostFiftyRows(t *testing.T) {
	ctx, tx := opTx(t)
	w := iaNewWorld(t, ctx, tx, "reset")
	for i := 0; i < 60; i++ {
		m := ipMember(t, ctx, tx, idTenant(t, ctx, tx, "window many"), w.p.email, w.p.digest)
		ipLink(t, ctx, tx, m, w.p.id, "invite", "2 days")
	}
	if rows := iaWindow(t, ctx, tx, w.session); len(rows) != 50 {
		t.Errorf("%d rows, want 50 (the header and 49)", len(rows))
	}
}

// TestIdentityKeepAll00079_NullsTheThreeWindowColumnsTogether is M14-18's inherited obligation 2: "Keep
// all" with a LIVE (stale) session closes the window -- confirm_opened_at, confirm_open_until and
// confirm_scope NULL, all three, in one statement -- keeps email_verified_at, writes ONE confirm_kept
// event whose detail is the closed scope alone, and touches no membership and no audit row. After it
// an unlink and an Undo are refused (the window is closed), and a second press answers 0 rows and
// writes nothing. A closed window, an unlinked session and an unknown hash answer 0 rows.
// The structure behind it is TestIdentityKeepAll00079_TheCheckRefusesTwoOfThree.
func TestIdentityKeepAll00079_NullsTheThreeWindowColumnsTogether(t *testing.T) {
	ctx, tx := opTx(t)
	w := iaNewWorld(t, ctx, tx, "reset")
	before := opInt(t, ctx, tx, `SELECT count(*) FROM audit_log WHERE at = now()`)
	if n := ipRows(t, ctx, tx, keepAllSQL, w.session); n != 1 {
		t.Fatalf("keep all: %d rows, want 1", n)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_identities WHERE id = $1 AND confirm_opened_at IS NULL
	                             AND confirm_open_until IS NULL AND confirm_scope IS NULL AND email_verified_at IS NOT NULL`, w.p.id); n != 1 {
		t.Fatal("the three window columns are not all NULL, or the address lost its verification")
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'confirm_kept'
	                             AND detail = '{"scope": "reset"}'::jsonb`, w.p.id); n != 1 {
		t.Errorf("%d confirm_kept events with the scope alone, want 1", n)
	}
	if after := opInt(t, ctx, tx, `SELECT count(*) FROM audit_log WHERE at = now()`); after != before {
		t.Errorf("keep all wrote %d audit rows", after-before)
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_users WHERE email = $1 AND identity_id = $2`, w.p.email, w.p.id); n != 4 {
		t.Errorf("%d memberships linked after keep all, want the 4 of the world", n)
	}
	fresh := ipSession(t, ctx, tx, w.a, w.ma, ipFresh)
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_unlink_backfilled($1, $2)`, fresh, w.b); n != 0 {
		t.Error("an unlink passed after the window was kept")
	}
	if n := ipRows(t, ctx, tx, `SELECT * FROM identity_relink_refused($1, $2)`, fresh, w.d); n != 0 {
		t.Error("an Undo passed after the window was kept")
	}
	if n := ipRows(t, ctx, tx, keepAllSQL, w.session); n != 0 {
		t.Error("a second keep all answered a row")
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'confirm_kept'`, w.p.id); n != 1 {
		t.Errorf("%d confirm_kept events after the second press, want 1", n)
	}
	plain := ipMember(t, ctx, tx, w.a, ipEmail(), w.p.digest)
	for _, c := range []struct{ name, hash string }{
		{"an unlinked row's session", ipSession(t, ctx, tx, w.a, plain, ipFresh)},
		{"an unknown hash", opRandHex(t)},
	} {
		if n := ipRows(t, ctx, tx, keepAllSQL, c.hash); n != 0 {
			t.Errorf("%s: %d rows", c.name, n)
		}
	}
}

// TestIdentityKeepAll00079_RefusesAWindowPastItsEnd: a window whose end has passed is not "kept" --
// 0 rows, the columns untouched.
func TestIdentityKeepAll00079_RefusesAWindowPastItsEnd(t *testing.T) {
	ctx, tx := opTx(t)
	w := iaNewWorld(t, ctx, tx, "first_verify")
	aiExec(t, ctx, tx, `UPDATE admin_identities SET confirm_opened_at = now() - interval '8 days', confirm_open_until = now() - interval '1 day' WHERE id = $1`, w.p.id)
	if n := ipRows(t, ctx, tx, keepAllSQL, w.session); n != 0 {
		t.Error("keep all closed a window that had already ended")
	}
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM admin_identities WHERE id = $1 AND confirm_scope IS NOT NULL`, w.p.id); n != 1 {
		t.Error("a refused keep all wrote the window")
	}
}

// TestIdentityKeepAll00079_TheCheckRefusesTwoOfThree is the structure behind obligation 2: 00069's
// admin_identities_confirm_window_whole refuses an UPDATE that NULLs two of the window's columns and
// leaves the third (here as the owner, who bypasses no CHECK).
func TestIdentityKeepAll00079_TheCheckRefusesTwoOfThree(t *testing.T) {
	ctx, tx := opTx(t)
	w := iaNewWorld(t, ctx, tx, "reset")
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		_, err := sp.Exec(ctx, `UPDATE admin_identities SET confirm_opened_at = NULL, confirm_open_until = NULL WHERE id = $1`, w.p.id)
		if code, _ := opCode(err); code != "23514" {
			t.Fatalf("leaving confirm_scope behind: %v, want 23514 (admin_identities_confirm_window_whole)", err)
		}
	})
}

// TestIdentity00079_DownRefusesWhileAKeepAllIsRecordedAndOtherwiseWalksBackAndUp: with a confirm_kept
// row the Down is 55000 and changes nothing; without, it drops the two functions and gives the event
// CHECK 00072's set back -- and REVOKEs nothing (the definer's column grants are the same before and
// after); the Up gives both back.
func TestIdentity00079_DownRefusesWhileAKeepAllIsRecordedAndOtherwiseWalksBackAndUp(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, ia00079File)
	lockForDowns(t, ctx, tx, []string{"admin_identity_events"})
	grants := func(q pgx.Tx) string {
		var s string
		if err := q.QueryRow(ctx, `SELECT coalesce(string_agg(table_name || '.' || column_name || ':' || privilege_type, ',' ORDER BY 1), '')
		                             FROM information_schema.column_privileges WHERE grantee = 'tappa_identitydefiner'`).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	head := grants(tx)
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		w := iaNewWorld(t, ctx, sp, "reset")
		if n := ipRows(t, ctx, sp, keepAllSQL, w.session); n != 1 {
			t.Fatalf("keep all: %d", n)
		}
		aiSavepoint(t, ctx, sp, func(sp2 pgx.Tx) {
			_, err := sp2.Conn().PgConn().Exec(ctx, down).ReadAll()
			if code, _ := opCode(err); code != "55000" {
				t.Fatalf("the Down with a confirm_kept row: %v, want 55000", err)
			}
		})
	})
	opRunSection(t, ctx, tx, down, "00079 Down")
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proname IN ('identity_confirm_window', 'identity_keep_all')`); n != 0 {
		t.Errorf("%d of the two functions survive the Down", n)
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		p := ipNewPerson(t, ctx, sp)
		_, err := sp.Exec(ctx, `INSERT INTO admin_identity_events (identity_id, event) VALUES ($1, 'confirm_kept')`, p.id)
		if code, _ := opCode(err); code != "23514" {
			t.Fatalf("confirm_kept below 00079: %v, want 23514", err)
		}
	})
	if got := grants(tx); got != head {
		t.Errorf("the Down changed the definer's column grants:\n%s\nwant\n%s", got, head)
	}
	opRunSection(t, ctx, tx, up, "00079 Up")
	if n := opInt(t, ctx, tx, `SELECT count(*) FROM pg_proc WHERE proname IN ('identity_confirm_window', 'identity_keep_all') AND prosecdef`); n != 2 {
		t.Errorf("%d definer functions after the Up, want 2", n)
	}
}
