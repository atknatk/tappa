package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// adminreconcile_test.go -- migration 00082 (M14-06b, ADR 0031 §5.2, §5.8, §8) against real
// Postgres: identity_reconcile(), the maintenance pass that gives the e-mail groups born after the
// backfill their identity by 00073's own rule (linked_via = 'reconcile'); who may call it; its
// concurrency with the live application (T8); the file's role assertion, postcondition and Down.
//
// 🔴 NOTHING HERE COMMITS but the concurrency test's own fixture, which it takes back (00069's Down
// refuses while an identity exists). Every other case runs in a savepoint of bfTx's READ COMMITTED
// transaction (adminbackfill_test.go's reasons: the pass locks every committed candidate row of the
// shared database FOR UPDATE; a savepoint's row locks go with its rollback).

const rcFile = "00082_reconcile_admin_identities.sql"

// rcSections returns 00082's Up, its postcondition (the Up's last DO block) and its Down.
func rcSections(t *testing.T) (up, post, down string) {
	t.Helper()
	up, down = opMigrationSections(t, rcFile)
	i := strings.LastIndex(up, "DO $$")
	j := strings.LastIndex(up, "-- +goose StatementEnd")
	if i < 0 || j < i || !strings.Contains(up[i:j], "postcondition") {
		t.Fatalf("%s's Up does not end with the postcondition's DO block", rcFile)
	}
	return up, up[i:j], down
}

// rcRun calls the pass as the owner session (tappa_owner, its one caller) and returns how many rows
// it linked.
func rcRun(t *testing.T, ctx context.Context, q pgx.Tx) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT public.identity_reconcile()`)
}

// rcLinked: the row is linked by the pass, to an identity carrying its own address and digest,
// linked_at = the transaction's now(), nothing else set on the identity.
func rcLinked(t *testing.T, ctx context.Context, q pgx.Tx, id uuid.UUID) bool {
	t.Helper()
	s := bfRow(t, ctx, q, id)
	var email string
	if err := q.QueryRow(ctx, `SELECT email::text FROM admin_users WHERE id = $1`, id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return !strings.HasPrefix(s, "identity=-") &&
		strings.Contains(s, " via=reconcile at_now=true id_email="+email+" digest_copied=true untouched=true")
}

// rcShape is a row's outcome without the identity's id and with the link's way blanked: the backfill
// and the pass, run on one fixture, must leave the same shapes.
func rcShape(t *testing.T, ctx context.Context, q pgx.Tx, id uuid.UUID) string {
	t.Helper()
	s := bfRow(t, ctx, q, id)
	s = s[strings.Index(s, " via="):]
	return strings.NewReplacer(" via=backfill ", " via=* ", " via=reconcile ", " via=* ").Replace(s)
}

type rcCase struct {
	what   string
	id     uuid.UUID
	linked bool
}

// rcBackfillFixture is adminbackfill_test.go's card fixture (M14-06), case for case: the rule both
// files apply. It returns every row with whether the rule links it, and the addresses that must end
// with exactly one identity (shared) and with none (left alone).
func rcBackfillFixture(t *testing.T, ctx context.Context, tx pgx.Tx) (cases []rcCase, shared, none []string) {
	t.Helper()
	d := opFakeDigest
	add := func(what string, id uuid.UUID, linked bool) { cases = append(cases, rcCase{what, id, linked}) }

	add("the KF owner", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), bfEmail("kf-owner"), "active", 90, d("k")), true)
	add("the KM owner", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), bfEmail("km-owner"), "active", 90, d("m")), true)

	victimMail := bfEmail("victim")
	add("the older active row of a shared address (the victim)", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), victimMail, "active", 200, d("v")), true)
	add("a LATER active row carrying the victim's address (the attacker)",
		bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), strings.ToUpper(victimMail), "active", 10, d("x")), false)

	quietMail := bfEmail("quiet")
	add("an EARLIER disabled row of the address", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), quietMail, "disabled", 300, d("e")), false)
	add("the later ACTIVE row after an earlier disabled one", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), quietMail, "active", 100, d("l")), true)

	goneMail := bfEmail("gone")
	add("the oldest row of a group with no active row", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), goneMail, "disabled", 50, d("o")), true)
	add("the newer row of a group with no active row", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), goneMail, "disabled", 40, d("n")), false)

	tieMail := bfEmail("tie")
	t1, t2 := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	if t2.String() < t1.String() {
		t1, t2 = t2, t1
	}
	add("the lower tenant_id of two rows born at the same instant", bfAdmin(t, ctx, tx, t1, tieMail, "active", 30, d("f")), true)
	add("the higher tenant_id of two rows born at the same instant", bfAdmin(t, ctx, tx, t2, tieMail, "active", 30, d("s")), false)

	add("a row with no e-mail", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), nil, "active", 500, d("q")), false)
	add("a row with an empty e-mail", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), "", "active", 500, d("r")), false)
	add("a row with a blank e-mail", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), "   ", "active", 500, d("u")), false)

	ownMail := bfEmail("own")
	aiIdentity(t, ctx, tx, ownMail)
	add("a row whose address already has an identity", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), ownMail, "active", 400, d("a")), false)

	refusedMail := bfEmail("refused")
	refused := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), refusedMail, "active", 400, d("a"))
	aiExec(t, ctx, tx, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, refused)
	add("a refused row", refused, false)
	add("the sibling of a refused row", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), refusedMail, "active", 300, d("b")), false)

	linkedMail := bfEmail("linked")
	elsewhere := aiIdentity(t, ctx, tx, bfEmail("elsewhere"))
	linkedRow := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), linkedMail, "active", 400, d("a"))
	aiExec(t, ctx, tx, `UPDATE admin_users SET identity_id = $2, linked_via = 'account', linked_at = now() WHERE id = $1`, linkedRow, elsewhere)
	add("the sibling of a row linked under another address", bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), linkedMail, "active", 300, d("b")), false)

	return cases, []string{victimMail, quietMail, goneMail, tieMail}, []string{refusedMail, linkedMail}
}

// TestIdentityReconcile_TheBackfillsRuleCaseForCase (ADR 0031 §5.8: "aynı incumbent-first kuralı";
// the card: "backfill'in testleri uzlaştırma için de yeşil"): the backfill's card fixture, run once
// through 00073's Up and once through identity_reconcile(), each in its own savepoint, leaves every
// row in the SAME shape (linked or not, the identity's address and digest, linked_at) -- the two
// never diverge -- and the pass's own run links exactly the rows the rule names, linked_via =
// 'reconcile', returns their count, and leaves alone a group whose address already has an identity
// and a group holding a linked or a refused row. The trigger (00069) is in force over its links.
func TestIdentityReconcile_TheBackfillsRuleCaseForCase(t *testing.T) {
	ctx, tx := bfTx(t)
	up, _, _ := bfSections(t)
	cases, shared, none := rcBackfillFixture(t, ctx, tx)

	byBackfill := map[uuid.UUID]string{}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, up, "00073 Up")
		for _, c := range cases {
			byBackfill[c.id] = rcShape(t, ctx, sp, c.id)
		}
	})
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		n := rcRun(t, ctx, sp)
		want := 0
		for _, c := range cases {
			if got := rcShape(t, ctx, sp, c.id); got != byBackfill[c.id] {
				t.Errorf("%s: the pass left %q, the backfill %q", c.what, got, byBackfill[c.id])
			}
			switch {
			case c.linked && !rcLinked(t, ctx, sp, c.id):
				t.Errorf("%s is not linked by the pass: %s", c.what, bfRow(t, ctx, sp, c.id))
			case !c.linked && !bfUnlinked(t, ctx, sp, c.id):
				t.Errorf("%s is linked: %s", c.what, bfRow(t, ctx, sp, c.id))
			}
			if c.linked {
				want++
			}
		}
		// The call also links the shared database's own committed candidates (other packages'
		// fixtures), so its count is at least the fixture's.
		if n < int64(want) {
			t.Errorf("the pass reports %d linked rows, the fixture alone has %d", n, want)
		}
		if got := bfIdentities(t, ctx, sp, shared...); got != int64(len(shared)) {
			t.Errorf("the shared addresses have %d identities, want one each (%d)", got, len(shared))
		}
		if got := bfIdentities(t, ctx, sp, none...); got != 0 {
			t.Errorf("a group holding a refused or a linked row got %d identities, want 0", got)
		}
		victim := cases[2].id
		opWant(t, opTry(t, ctx, sp, `UPDATE admin_users SET password_hash = $2 WHERE id = $1`, victim, opFakeDigest("z")),
			sqlstateRestrictViolation, "the owner changing a reconciled row's password_hash")
	})
}

// TestIdentityReconcile_RowsBornAfterTheBackfill is the pass's own subject (ADR 0031 §8 step 3):
// after the backfill (00073's Up) ran,
//   - a row of a NEW address is linked ('reconcile'); a new group of two takes its oldest active row;
//   - THE M14-06 SECURITY NOTE: an earlier ACTIVE row of an address -- an attacker's business that
//     reached the backfill first -- birthed the identity; the victim's row of that address, born
//     after, is NOT joined to it (it stays unlinked; the identity keeps exactly its one link);
//   - a refused row (unlinked_at) whose address has no identity is never linked, in any run, and
//     neither is its group's other row;
//   - the backfill's links are untouched.
func TestIdentityReconcile_RowsBornAfterTheBackfill(t *testing.T) {
	ctx, tx := bfTx(t)
	up, _, _ := bfSections(t)
	d := opFakeDigest
	attackerMail := bfEmail("attacker-first")
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		attacker := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), attackerMail, "active", 30, d("x"))
		opRunSection(t, ctx, sp, up, "00073 Up (the expand)")
		if !bfLinked(t, ctx, sp, attacker) {
			t.Fatalf("PREMISE: the backfill did not link the earlier row: %s", bfRow(t, ctx, sp, attacker))
		}
		backfilled := bfRow(t, ctx, sp, attacker)

		// Born after the backfill (the previous pod's signups, the window before M14-17).
		victim := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), strings.ToUpper(attackerMail), "active", 0, d("v"))
		single := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), bfEmail("born-after"), "active", 0, d("s"))
		pairMail := bfEmail("born-after-pair")
		pairOld := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), pairMail, "active", 2, d("o"))
		pairNew := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), pairMail, "active", 1, d("n"))
		refusedMail := bfEmail("refused-alone")
		refused := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), refusedMail, "active", 3, d("r"))
		aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, refused)
		refusedSibling := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), refusedMail, "active", 1, d("q"))

		for run := 1; run <= 2; run++ {
			rcRun(t, ctx, sp)
			for _, c := range []struct {
				what string
				id   uuid.UUID
			}{{"a row of a new address", single}, {"the older row of a new pair", pairOld}} {
				if !rcLinked(t, ctx, sp, c.id) {
					t.Errorf("run %d: %s is not linked: %s", run, c.what, bfRow(t, ctx, sp, c.id))
				}
			}
			for _, c := range []struct {
				what string
				id   uuid.UUID
			}{
				{"the victim's row of an address an earlier active row birthed", victim},
				{"the newer row of a new pair", pairNew},
				{"a refused row", refused}, {"the sibling of a refused row", refusedSibling},
			} {
				if !bfUnlinked(t, ctx, sp, c.id) {
					t.Errorf("run %d: %s is linked: %s", run, c.what, bfRow(t, ctx, sp, c.id))
				}
			}
			if got := bfRow(t, ctx, sp, attacker); got != backfilled {
				t.Errorf("run %d: the backfill's link moved: %s, was %s", run, got, backfilled)
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE identity_id = (SELECT identity_id FROM admin_users WHERE id = $1)`, attacker); n != 1 {
				t.Errorf("run %d: the earlier row's identity holds %d links, want 1", run, n)
			}
			if n := bfIdentities(t, ctx, sp, refusedMail); n != 0 {
				t.Errorf("run %d: a group holding a refused row got %d identities", run, n)
			}
		}
	})
}

// rcSetAside empties the shared database's admin_users of filled e-mails inside the savepoint (rolled
// back with it): links undone first (00069's trigger keeps a linked row's e-mail), then every address
// NULL. A pass then sees only the rows the test writes after it -- other packages' committed rows and
// identities, which they may change between two calls (an identity a cleanup deletes makes its address
// a candidate again), are out of its reach. The identities stay: with no row of their address they are
// no group's (TestIdentityReconcile_AnEmptyDatabase deletes them itself).
func rcSetAside(t *testing.T, ctx context.Context, sp pgx.Tx) {
	t.Helper()
	aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL, unlinked_at = NULL
	                     WHERE identity_id IS NOT NULL OR unlinked_at IS NOT NULL OR linked_via IS NOT NULL`)
	aiExec(t, ctx, sp, `UPDATE admin_users SET email = NULL WHERE email IS NOT NULL`)
}

// TestIdentityReconcile_ItIsIdempotent (the card: "iki koşu aynı sonuç"): a second call links
// nothing, births no identity anywhere, and leaves every row as the first left it. The shared
// database is set aside first (rcSetAside), so both counts are exact: on CI a committed identity of
// another package, deleted by its cleanup between the two calls, once made the second call link a row
// that was not the test's (run 39935647526).
func TestIdentityReconcile_ItIsIdempotent(t *testing.T) {
	ctx, tx := bfTx(t)
	shared, single := bfEmail("rc-twice-shared"), bfEmail("rc-twice-single")
	var ids []uuid.UUID
	snapshot := func(q pgx.Tx) string {
		var b strings.Builder
		for _, id := range ids {
			b.WriteString(bfRow(t, ctx, q, id) + "\n")
		}
		fmt.Fprintf(&b, "identities=%d born_here=%d", bfIdentities(t, ctx, q, shared, single),
			opInt(t, ctx, q, `SELECT count(*) FROM admin_identities WHERE created_at = now()`))
		return b.String()
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		rcSetAside(t, ctx, sp)
		ids = []uuid.UUID{
			bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), shared, "active", 20, opFakeDigest("a")),
			bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), shared, "active", 10, opFakeDigest("b")),
			bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), single, "disabled", 5, opFakeDigest("c")),
			bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), nil, "active", 5, opFakeDigest("d")),
		}
		if n := rcRun(t, ctx, sp); n != 2 {
			t.Fatalf("PREMISE: the first run linked %d rows, want 2", n)
		}
		first := snapshot(sp)
		if !strings.Contains(first, "identities=2 ") {
			t.Fatalf("PREMISE: the first run made no identity per address:\n%s", first)
		}
		if n := rcRun(t, ctx, sp); n != 0 {
			t.Errorf("the second run linked %d rows, want 0", n)
		}
		if second := snapshot(sp); second != first {
			t.Errorf("the second run changed the state:\n%s\nwant\n%s", second, first)
		}
	})
}

// TestIdentityReconcile_AnEmptyDatabase (R11, the backfill's empty-database case): with no filled
// e-mail anywhere -- every address set aside in the savepoint, plus an e-mail-less, an empty and a
// blank row -- the pass links nothing and makes no identity, and the whole of 00082's Up (Down
// first, then Up: the call and the postcondition) passes.
func TestIdentityReconcile_AnEmptyDatabase(t *testing.T) {
	ctx, tx := bfTx(t)
	up, _, down := rcSections(t)
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		rcSetAside(t, ctx, sp)
		aiExec(t, ctx, sp, `DELETE FROM admin_identities`)
		bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), nil, "active", 3, opFakeDigest("a"))
		bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), "", "active", 3, opFakeDigest("b"))
		bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), " ", "active", 3, opFakeDigest("c"))
		if n := rcRun(t, ctx, sp); n != 0 {
			t.Errorf("the pass linked %d rows of no filled e-mail", n)
		}
		opRunSection(t, ctx, sp, down, "00082 Down")
		opRunSection(t, ctx, sp, up, "00082 Up over a database with no filled e-mail")
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities`); n != 0 {
			t.Errorf("%d identities born from no filled e-mail", n)
		}
	})
}

// TestIdentityReconcile_OnlyTappaOwnerMayCallIt (ADR 0031 §5.2, ONAY note 1; the card: "tappa_app
// fonksiyonu çağıramaz"): tappa_app, tappa_operator, tappa_opdefiner and tappa_resolver are refused
// with 42501 and nothing is linked; tappa_owner -- the migration and the runbook -- may.
func TestIdentityReconcile_OnlyTappaOwnerMayCallIt(t *testing.T) {
	ctx, tx := bfTx(t)
	row := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), bfEmail("caller"), "active", 1, opFakeDigest("a"))
	for _, role := range []string{"tappa_app", "tappa_operator", "tappa_opdefiner", "tappa_resolver"} {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			opWant(t, opExecAs(t, ctx, sp, role, `SELECT public.identity_reconcile()`), sqlstateInsufficientPrivi, role+" calling identity_reconcile")
			if !bfUnlinked(t, ctx, sp, row) {
				t.Errorf("%s's refused call linked the row", role)
			}
		})
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		if err := opExecAs(t, ctx, sp, "tappa_owner", `SELECT public.identity_reconcile()`); err != nil {
			t.Fatalf("tappa_owner calling identity_reconcile: %v", err)
		}
		if !rcLinked(t, ctx, sp, row) {
			t.Errorf("tappa_owner's call did not link the row: %s", bfRow(t, ctx, sp, row))
		}
	})
}

// TestIdentityReconcile_ADigestTheIdentityRefusesStopsIt: a chosen row older than 00018 with a digest
// that is not bcrypt-shaped stops the WHOLE call with 55000 -- not the INSERT's check_violation, whose
// detail would print the address -- the message names no address, and nothing is written, not even
// another group's link made earlier in the same call. A group whose address ALREADY has an identity
// is skipped before its digest is looked at (the address check comes first): such a row does not
// stop the pass. (The CHECK is dropped inside the savepoint; the rollback restores it.)
func TestIdentityReconcile_ADigestTheIdentityRefusesStopsIt(t *testing.T) {
	ctx, tx := bfTx(t)
	legacyMail, fine, skippedMail := bfEmail("rc-legacy"), bfEmail("rc-a-fine"), bfEmail("rc-skipped-legacy")
	t.Run("a chosen legacy digest stops the call", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			lockForDowns(t, ctx, sp, []string{"admin_users"})
			aiExec(t, ctx, sp, `ALTER TABLE admin_users DROP CONSTRAINT admin_users_password_hash_is_bcrypt`)
			ok := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), fine, "active", 3, opFakeDigest("a"))
			row := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), legacyMail, "active", 3, "legacy-"+opRandHex(t))
			bfWantRefusal(t, holdingNestedRun(t, ctx, sp, `SELECT public.identity_reconcile()`),
				"carries a password digest that is not bcrypt-shaped", "the pass over a chosen legacy digest", legacyMail, fine)
			if !bfUnlinked(t, ctx, sp, row) || !bfUnlinked(t, ctx, sp, ok) || bfIdentities(t, ctx, sp, legacyMail, fine) != 0 {
				t.Errorf("the refused call wrote something: %s / %s", bfRow(t, ctx, sp, row), bfRow(t, ctx, sp, ok))
			}
		})
	})
	t.Run("a group whose address has an identity is skipped before its digest is read", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			lockForDowns(t, ctx, sp, []string{"admin_users"})
			aiExec(t, ctx, sp, `ALTER TABLE admin_users DROP CONSTRAINT admin_users_password_hash_is_bcrypt`)
			aiIdentity(t, ctx, sp, skippedMail)
			row := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), skippedMail, "active", 3, "legacy-"+opRandHex(t))
			ok := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), fine, "active", 3, opFakeDigest("a"))
			if err := holdingNestedRun(t, ctx, sp, `SELECT public.identity_reconcile()`); err != nil {
				t.Fatalf("a skipped group's legacy digest stopped the pass: %v", err)
			}
			rcRun(t, ctx, sp)
			if !bfUnlinked(t, ctx, sp, row) || !rcLinked(t, ctx, sp, ok) {
				t.Errorf("want the skipped row unlinked and the other linked: %s / %s", bfRow(t, ctx, sp, row), bfRow(t, ctx, sp, ok))
			}
		})
	})
}

// ------------------------------------------------------------------- the address rule (M14-08a) --

// rcAddressRuleWorld writes, in q, the e-mail groups the address rule decides (ADR 0031 §5.8's
// M14-08a note) and returns the check of what a pass must leave: each failure is one line.
//   - U+0130 (citext folds it onto 'i'): the group's OLDEST ACTIVE row is the homoglyph spelling; the
//     identity is born with the ASCII row's spelling and digest, the homoglyph row stays unlinked;
//   - U+212A (the Kelvin sign, folded onto 'k'): an active homoglyph row and a DISABLED ASCII one --
//     "oldest active" counts rule-passing rows only, so the disabled ASCII row births the identity;
//   - a group of one homoglyph row with no ASCII twin, and a group of one ASCII row of the wrong shape
//     (a..b, which mail.ValidRecipient refuses): each births nothing (the named residues).
func rcAddressRuleWorld(t *testing.T, ctx context.Context, q pgx.Tx) func(q pgx.Tx) []string {
	t.Helper()
	d := opFakeDigest
	u := uuid.NewString()
	dotted, dottedH := "m14-06b-rc-"+u+"@corip.example", "m14-06b-rc-"+u+"@corİp.example"
	kelvin, kelvinH := "m14-06b-rc-"+u+"@park.example", "m14-06b-rc-"+u+"@parK.example"
	alone := "m14-06b-rc-alone-" + u + "@corİp.example"
	shape := "m14-06b-rc.." + u + "@corip.example"
	hI := bfAdmin(t, ctx, q, aiTenant(t, ctx, q), dottedH, "active", 30, d("h"))
	aI := bfAdmin(t, ctx, q, aiTenant(t, ctx, q), dotted, "active", 10, d("a"))
	hK := bfAdmin(t, ctx, q, aiTenant(t, ctx, q), kelvinH, "active", 30, d("k"))
	aK := bfAdmin(t, ctx, q, aiTenant(t, ctx, q), kelvin, "disabled", 5, d("b"))
	lone := bfAdmin(t, ctx, q, aiTenant(t, ctx, q), alone, "active", 20, d("c"))
	bad := bfAdmin(t, ctx, q, aiTenant(t, ctx, q), shape, "active", 20, d("s"))
	return func(q pgx.Tx) []string {
		var out []string
		for _, c := range []struct {
			what string
			id   uuid.UUID
		}{{"the ASCII row of a group whose oldest active row is U+0130", aI}, {"the disabled ASCII row beside an active U+212A row", aK}} {
			if !rcLinked(t, ctx, q, c.id) {
				out = append(out, c.what+" is not linked to an identity of its own spelling and digest: "+bfRow(t, ctx, q, c.id))
			}
		}
		for _, c := range []struct {
			what string
			id   uuid.UUID
		}{{"the U+0130 homoglyph row", hI}, {"the U+212A homoglyph row", hK}, {"a homoglyph row with no ASCII twin", lone}, {"an ASCII row of the wrong shape", bad}} {
			if !bfUnlinked(t, ctx, q, c.id) {
				out = append(out, c.what+" is linked: "+bfRow(t, ctx, q, c.id))
			}
		}
		if n := opInt(t, ctx, q, `SELECT count(*) FROM admin_identities WHERE email::text = ANY ($1::text[])`, []string{dottedH, kelvinH, alone, shape}); n != 0 {
			out = append(out, fmt.Sprintf("%d identities were born with a spelling mail.ValidRecipient refuses", n))
		}
		if n := bfIdentities(t, ctx, q, dotted, kelvin); n != 2 {
			out = append(out, fmt.Sprintf("the two folded groups hold %d identities, want one each", n))
		}
		return out
	}
}

// TestIdentityReconcile_TheAddressRuleChoosesTheASCIIRow (the card's "Adres kuralı (M14-08a)"
// paragraph: the merge's BEHAVIOUR test): on rcAddressRuleWorld's groups, two passes leave every
// group as the rule says -- the identity born with the ASCII spelling and that row's digest, never a
// homoglyph's, even when the homoglyph is the oldest active row; a group of rule-failing rows alone
// births nothing -- and the file's postcondition passes over them (its first check counts rule-passing
// rows only).
//
// MUTATION CONTROLS, in savepoints, each red: the body with its `IF (...) IS NOT TRUE THEN CONTINUE`
// refusal cut (redefined from pg_get_functiondef) chooses the homoglyph row -- the check above fails
// and the postcondition's last check refuses the homoglyph identity; and the postcondition's first
// check without the rule takes the rule-failing groups for a miss ("saw fewer rows").
func TestIdentityReconcile_TheAddressRuleChoosesTheASCIIRow(t *testing.T) {
	ctx, tx := bfTx(t)
	_, post, _ := rcSections(t)

	t.Run("the pass", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			check := rcAddressRuleWorld(t, ctx, sp)
			for run := 1; run <= 2; run++ {
				rcRun(t, ctx, sp)
				for _, f := range check(sp) {
					t.Errorf("run %d: %s", run, f)
				}
			}
			if err := holdingNestedRun(t, ctx, sp, post); err != nil {
				t.Fatalf("the postcondition over the rule-failing groups: %v", err)
			}
		})
	})

	t.Run("control/the body without its address refusal", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			var def string
			if err := sp.QueryRow(ctx, `SELECT pg_get_functiondef('public.identity_reconcile()'::regprocedure)`).Scan(&def); err != nil {
				t.Fatal(err)
			}
			refusal := regexp.MustCompile(`(?s)\n\s*IF \(pg_catalog\.octet_length\(r\.email::text\) <= 254.*?\) IS NOT TRUE THEN\s*CONTINUE;\s*END IF;`)
			if n := len(refusal.FindAllStringIndex(def, -1)); n != 1 {
				t.Fatalf("PREMISE: the body holds %d address refusals, want 1", n)
			}
			aiExec(t, ctx, sp, refusal.ReplaceAllString(def, ""))
			check := rcAddressRuleWorld(t, ctx, sp)
			rcRun(t, ctx, sp)
			if f := check(sp); len(f) == 0 {
				t.Fatal("MUTATION: without the refusal the pass still leaves every group as the rule says")
			} else {
				t.Logf("red without the refusal: %v", f)
			}
			bfWantRefusal(t, holdingNestedRun(t, ctx, sp, post), "mail.ValidRecipient refuses", "the postcondition over a homoglyph identity")
		})
	})

	t.Run("control/the postcondition's first check without the rule", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			rule := regexp.MustCompile(`(?m)^\s+AND pg_catalog\.octet_length\(a\.email::text\) <= 254\n\s+AND pg_catalog\.strpos\(a\.email::text, '=\?'\) = 0\n\s+AND a\.email::text ~ '[^\n]*'\n`)
			if n := len(rule.FindAllStringIndex(post, -1)); n != 1 {
				t.Fatalf("PREMISE: the postcondition holds the first check's rule %d times, want 1", n)
			}
			rcAddressRuleWorld(t, ctx, sp)
			rcRun(t, ctx, sp)
			bfWantRefusal(t, holdingNestedRun(t, ctx, sp, rule.ReplaceAllString(post, "")), "saw fewer rows than this runner",
				"the postcondition's first check without the rule")
		})
	})
}

// rcRunbookCounts returns deploy/README.md's two M14-19 precheck statements (the M14-06b section's
// runbook step): the identityless count with its residue, and the digest mismatch.
func rcRunbookCounts(t *testing.T) (identityless, mismatch string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	stmt := func(from string) string {
		doc := string(b)
		if n := strings.Count(doc, from); n != 1 {
			t.Fatalf("PREMISE: deploy/README.md holds %q %d times, want 1", from, n)
		}
		rest := doc[strings.Index(doc, from):]
		return rest[:strings.Index(rest, ";\n")]
	}
	return stmt("WITH identityless AS ("), stmt("SELECT count(*) AS linked_digest_mismatch")
}

// TestIdentityReconcile_TheRunbookCountsTheResidueApart: deploy/README.md's M14-19 precheck, run as it is
// written on rcAddressRuleWorld's groups (every other row's address set aside in the savepoint, so the
// counts are the world's): before the pass two addresses are identityless and four rows are the
// residue (the two homoglyph rows still beside identityless ASCII twins, the lone homoglyph, the a..b
// row); after it the identityless count is 0 and the residue is the two rows the rule keeps out for
// good -- never a stop; the digest mismatch is 0 throughout.
func TestIdentityReconcile_TheRunbookCountsTheResidueApart(t *testing.T) {
	ctx, tx := bfTx(t)
	identityless, mismatch := rcRunbookCounts(t)
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		rcSetAside(t, ctx, sp)
		rcAddressRuleWorld(t, ctx, sp)
		counts := func() string {
			var addresses, residue int64
			if err := sp.QueryRow(ctx, identityless).Scan(&addresses, &residue); err != nil {
				t.Fatalf("the runbook's identityless count: %v", err)
			}
			return fmt.Sprintf("%d|%d|%d", addresses, residue, opInt(t, ctx, sp, mismatch))
		}
		if got := counts(); got != "2|4|0" {
			t.Errorf("before the pass the runbook counts %s, want 2|4|0", got)
		}
		rcRun(t, ctx, sp)
		if got := counts(); got != "0|2|0" {
			t.Errorf("after the pass the runbook counts %s, want 0|2|0", got)
		}
	})
}

// ------------------------------------------------------------------- concurrency (T8) --

// rcConn opens one owner connection of its own (a second session of the shared database).
func rcConn(t *testing.T, ctx context.Context) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(ctx, opOwnerDSN(t))
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(context.Background()); err != nil {
			t.Logf("close: %v", err)
		}
	})
	return conn
}

// rcTakeBack runs one owner transaction at cleanup time on a connection of its own.
func rcTakeBack(t *testing.T, what string, fn func(ctx context.Context, tx pgx.Tx) error) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, opOwnerDSN(t))
		if err != nil {
			t.Errorf("take back %s: %v", what, err)
			return
		}
		defer func() { _ = conn.Close(ctx) }()
		if err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			// Bounded: a take-back must never wait forever behind a session a failed test left open.
			if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '30s'`); err != nil {
				return err
			}
			return fn(ctx, tx)
		}); err != nil {
			t.Errorf("take back %s: %v", what, err)
		}
	})
}

// rcInsertRow commits one unlinked active row of the address, born daysAgo, in a tenant of its own
// (other sessions must see it). It registers no cleanup: rcCommittedRow's take-back of the address
// takes it back too -- a row committed after rcStart must not register a cleanup that would run
// before the call's own (LIFO) and wait on its locks.
func rcInsertRow(t *testing.T, ctx context.Context, conn *pgx.Conn, email string, daysAgo int) uuid.UUID {
	t.Helper()
	var row uuid.UUID
	if err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
		row = bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), email, "active", daysAgo, opFakeDigest("a"))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return row
}

// rcCommittedRow is rcInsertRow plus, at the end, the take-back of the ADDRESS: its identities, the
// links to them and every row of it (00069's Down refuses while an identity exists). The tenants stay,
// as every package's committed fixtures do (their feed counters key them).
func rcCommittedRow(t *testing.T, ctx context.Context, conn *pgx.Conn, email string, daysAgo int) uuid.UUID {
	t.Helper()
	row := rcInsertRow(t, ctx, conn, email, daysAgo)
	rcTakeBack(t, "a committed address", func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL
		                            WHERE identity_id IN (SELECT id FROM admin_identities WHERE email = $1)`, email); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM admin_identities WHERE email = $1`, email); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM admin_users WHERE email = $1`, email)
		return err
	})
	return row
}

// rcCommittedIdentity commits an identity of another address and takes it back at the end, its links
// first (whatever order the row cleanups run in).
func rcCommittedIdentity(t *testing.T, ctx context.Context, conn *pgx.Conn) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := conn.QueryRow(ctx, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2) RETURNING id`,
		bfEmail("rc-elsewhere"), opFakeDigest("e")).Scan(&id); err != nil {
		t.Fatal(err)
	}
	rcTakeBack(t, "a committed identity", func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL WHERE identity_id = $1`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM admin_identities WHERE id = $1`, id)
		return err
	})
	return id
}

// rcCall is one identity_reconcile() running in a transaction of its own, in a goroutine.
type rcCall struct {
	tx   pgx.Tx
	pid  uint32
	done chan struct{}
	err  error // written before done is closed
}

func (c *rcCall) finished() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

// result waits (bounded) for the call and returns its error.
func (c *rcCall) result(t *testing.T) error {
	t.Helper()
	select {
	case <-c.done:
		return c.err
	case <-time.After(20 * time.Second):
		t.Fatal("the pass did not finish in 20 s")
		return nil
	}
}

// rcStart begins a transaction on conn and runs the pass in a goroutine; the transaction stays open.
// ONE cleanup, in this order (the M14-06b audit: a failing test must not roll back a transaction a
// goroutine is still using): roll back the holder the call may be waiting behind, wait for the call
// to finish, then roll back the call's transaction.
func rcStart(t *testing.T, ctx context.Context, conn *pgx.Conn, holder pgx.Tx) *rcCall {
	t.Helper()
	c := &rcCall{pid: conn.PgConn().PID(), done: make(chan struct{})}
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c.tx = tx
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '20s'`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if holder != nil {
			_ = holder.Rollback(context.Background())
		}
		select {
		case <-c.done:
		case <-time.After(25 * time.Second):
			t.Errorf("the pass was still running at cleanup")
			return
		}
		_ = tx.Rollback(context.Background())
	})
	go func() {
		_, err := tx.Exec(ctx, `SELECT public.identity_reconcile()`)
		c.err = err
		close(c.done)
	}()
	return c
}

// rcWaitFor waits until the call waits on a lock and returns its wait_event ('advisory',
// 'transactionid', 'tuple' ...); "" when it finished without waiting.
func rcWaitFor(t *testing.T, ctx context.Context, observer *pgx.Conn, c *rcCall) string {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if c.finished() {
			return ""
		}
		var kind, event string
		if err := observer.QueryRow(ctx, `SELECT coalesce(wait_event_type, ''), coalesce(wait_event, '') FROM pg_stat_activity WHERE pid = $1`, c.pid).Scan(&kind, &event); err != nil {
			t.Fatal(err)
		}
		if kind == "Lock" {
			return event
		}
		if time.Now().After(deadline) {
			t.Fatalf("PREMISE: the pass neither waited on a lock nor finished in 15 s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// rcHolder begins a transaction on a connection of its own and runs stmts in it, uncommitted.
func rcHolder(t *testing.T, ctx context.Context, stmts ...[]any) pgx.Tx {
	t.Helper()
	tx, err := rcConn(t, ctx).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	for _, s := range stmts {
		if _, err := tx.Exec(ctx, s[0].(string), s[1:]...); err != nil {
			t.Fatalf("holder: %s: %v", s[0], err)
		}
	}
	return tx
}

// TestIdentityReconcile_ConcurrentWritersOfOneAddress (T8: the pass runs while the application is
// live; the card: "uzlaştırma ile eşzamanlı bir kayıt -> 500 yok"). Two sessions, committed rows of
// one address; the pass under test (B) is rolled back at the end, so the shared database's other
// candidates are never linked for real.
//   - TWO PASSES: A runs the pass and holds its transaction; B's pass QUEUES ON THE ADDRESS'S ADVISORY
//     LOCK ('admin-identity-email:' || lower(email)) -- not on the row: the lock is what serialises it
//     with a signup's identity INSERT, which locks no existing row. A rolls back; B links the row,
//     one identity, no error.
//   - A SIGNUP UNDER THE LOCK (identity_create_for_signup's shape, M14-17): A takes the address's
//     lock and inserts the address's identity; B queues on the lock; A commits; B returns WITHOUT
//     ERROR and leaves the row unlinked -- the address has an identity now -- one identity.
//   - A WRITER OUTSIDE THE LOCK (the second belt): A inserts the address's identity without the lock;
//     B reaches its INSERT and waits on A's transaction; A commits; B's ON CONFLICT DO NOTHING inserts
//     nothing -- no 23505, no 500 -- the row stays unlinked, one identity; a group B linked earlier in
//     the same call does not lend it its identity.
//   - A LINK COMMITTED WHILE THE PASS WAITS ON THE GROUP'S ROWS (the M14-06b audit; a linker that takes
//     no address lock -- identity_link_legacy's shape): A links the group's older row to an identity of
//     ANOTHER address and holds; B waits on that row; A commits; B leaves the group alone -- its other
//     row stays unlinked, no identity under the address (the linked-or-refused check reads after the
//     row locks).
//   - A ROW COMMITTED WHILE THE PASS WAITS (the M14-06b audit): A holds the group's only row (a login's
//     last_login_at); B waits on it; an OLDER active row of the address is committed; A commits; B
//     links the row it LOCKED, not the newcomer it never locked (the choice reads the locked ids only).
//
// Run with -race.
func TestIdentityReconcile_ConcurrentWritersOfOneAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	lockKey := func(email string) string {
		return `SELECT pg_advisory_xact_lock(hashtextextended('admin-identity-email:' || lower('` + email + `'), 0))`
	}
	linkedVia := func(q pgx.Tx, id uuid.UUID) string {
		var via string
		if err := q.QueryRow(ctx, `SELECT coalesce(linked_via, '-') FROM admin_users WHERE id = $1`, id).Scan(&via); err != nil {
			t.Fatal(err)
		}
		return via
	}

	t.Run("two passes: the second queues on the address's lock", func(t *testing.T) {
		email := bfEmail("rc-two-passes")
		setup, observer := rcConn(t, ctx), rcConn(t, ctx)
		row := rcCommittedRow(t, ctx, setup, email, 1)
		a := rcHolder(t, ctx, []any{`SELECT public.identity_reconcile()`})
		b := rcStart(t, ctx, rcConn(t, ctx), a)
		if event := rcWaitFor(t, ctx, observer, b); event != "advisory" {
			t.Fatalf("B's pass did not queue on the address's advisory lock: wait event %q", event)
		}
		if err := a.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if err := b.result(t); err != nil {
			t.Fatalf("B's pass after A: %v", err)
		}
		if via, n := linkedVia(b.tx, row), bfIdentities(t, ctx, b.tx, email); via != "reconcile" || n != 1 {
			t.Errorf("after two passes: row linked via %q, identities %d; want reconcile and 1", via, n)
		}
	})

	t.Run("a signup under the lock commits the identity first", func(t *testing.T) {
		email := bfEmail("rc-signup-first")
		setup, observer := rcConn(t, ctx), rcConn(t, ctx)
		row := rcCommittedRow(t, ctx, setup, email, 1)
		a := rcHolder(t, ctx, []any{lockKey(email)},
			[]any{`INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2)`, email, opFakeDigest("s")})
		b := rcStart(t, ctx, rcConn(t, ctx), a)
		if event := rcWaitFor(t, ctx, observer, b); event != "advisory" {
			t.Fatalf("the pass did not queue on the address's advisory lock behind the signup: wait event %q", event)
		}
		if err := a.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := b.result(t); err != nil {
			t.Fatalf("the pass after a signup of the same address: %v (want no error)", err)
		}
		if !bfUnlinked(t, ctx, b.tx, row) || bfIdentities(t, ctx, b.tx, email) != 1 {
			t.Errorf("want the row unlinked and one identity: %s, %d", bfRow(t, ctx, b.tx, row), bfIdentities(t, ctx, b.tx, email))
		}
	})

	t.Run("a writer outside the lock: ON CONFLICT DO NOTHING", func(t *testing.T) {
		email := bfEmail("rc-outside-lock")
		setup, observer := rcConn(t, ctx), rcConn(t, ctx)
		row := rcCommittedRow(t, ctx, setup, email, 1)
		// A group the same call links FIRST (its address sorts before): the conflicting group after it
		// must not reuse that group's identity (RETURNING INTO of no row assigns NULL).
		before := rcCommittedRow(t, ctx, setup, "m14-06-rc-0-before-"+uuid.NewString()+"@backfill.example", 1)
		a := rcHolder(t, ctx, []any{`INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2)`, email, opFakeDigest("w")})
		b := rcStart(t, ctx, rcConn(t, ctx), a)
		if event := rcWaitFor(t, ctx, observer, b); event != "transactionid" {
			t.Fatalf("the pass did not wait on the other writer's identity INSERT: wait event %q", event)
		}
		if err := a.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := b.result(t); err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == sqlstateUniqueViolation {
				t.Fatalf("the pass answered 23505 (a 500 in a live request): ON CONFLICT DO NOTHING is missing: %v", err)
			}
			t.Fatalf("the pass after an identity committed outside the lock: %v", err)
		}
		if !bfUnlinked(t, ctx, b.tx, row) || bfIdentities(t, ctx, b.tx, email) != 1 {
			t.Errorf("want the row unlinked and one identity: %s, %d", bfRow(t, ctx, b.tx, row), bfIdentities(t, ctx, b.tx, email))
		}
		if !rcLinked(t, ctx, b.tx, before) {
			t.Errorf("PREMISE: the group before it is not linked in the same call: %s", bfRow(t, ctx, b.tx, before))
		}
	})

	t.Run("a link committed while the pass waits on the group's rows", func(t *testing.T) {
		email := bfEmail("rc-linked-meanwhile")
		setup, observer := rcConn(t, ctx), rcConn(t, ctx)
		older := rcCommittedRow(t, ctx, setup, email, 2)
		other := rcCommittedRow(t, ctx, setup, email, 1)
		elsewhere := rcCommittedIdentity(t, ctx, setup)
		a := rcHolder(t, ctx, []any{`UPDATE admin_users SET identity_id = $2, linked_via = 'account', linked_at = now(),
		                                    password_hash = (SELECT password_hash FROM admin_identities WHERE id = $2)
		                              WHERE id = $1`, older, elsewhere})
		b := rcStart(t, ctx, rcConn(t, ctx), a)
		if event := rcWaitFor(t, ctx, observer, b); event == "" {
			t.Fatal("PREMISE: the pass did not wait on the row the linker holds")
		}
		if err := a.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := b.result(t); err != nil {
			t.Fatalf("the pass after a link committed meanwhile: %v", err)
		}
		if !bfUnlinked(t, ctx, b.tx, other) || bfIdentities(t, ctx, b.tx, email) != 0 {
			t.Errorf("the group holding a row linked meanwhile was taken: %s, %d identities", bfRow(t, ctx, b.tx, other), bfIdentities(t, ctx, b.tx, email))
		}
	})

	t.Run("a row committed while the pass waits is not chosen unlocked", func(t *testing.T) {
		email := bfEmail("rc-born-meanwhile")
		setup, observer := rcConn(t, ctx), rcConn(t, ctx)
		held := rcCommittedRow(t, ctx, setup, email, 1)
		a := rcHolder(t, ctx, []any{`UPDATE admin_users SET last_login_at = now() WHERE id = $1`, held})
		b := rcStart(t, ctx, rcConn(t, ctx), a)
		if event := rcWaitFor(t, ctx, observer, b); event == "" {
			t.Fatal("PREMISE: the pass did not wait on the row the login holds")
		}
		newcomer := rcInsertRow(t, ctx, setup, email, 30) // older, active: the rule would prefer it
		if err := a.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := b.result(t); err != nil {
			t.Fatalf("the pass: %v", err)
		}
		if !rcLinked(t, ctx, b.tx, held) || !bfUnlinked(t, ctx, b.tx, newcomer) {
			t.Errorf("want the locked row linked and the unlocked newcomer not: %s / %s", bfRow(t, ctx, b.tx, held), bfRow(t, ctx, b.tx, newcomer))
		}
	})
}

// ------------------------------------------------------------------- the file --

// TestIdentityReconcile00082_TheRunnerAndThePostcondition:
//   - the role assertion: a runner that is neither superuser nor BYPASSRLS (a role inheriting
//     tappa_owner's ownership, nothing but FORCE RLS between it and the rows) is refused by the Up
//     and the Down before anything changes;
//   - the postcondition: with tappa_identitydefiner stripped of BYPASSRLS the pass sees 0 rows and
//     links nothing, and the Up REFUSES ("saw fewer rows") instead of recording 00082 as done;
//   - a row linked 'reconcile' without its identity's digest, and an identity linked through two
//     reconcile rows, are refused by the postcondition alone.
func TestIdentityReconcile00082_TheRunnerAndThePostcondition(t *testing.T) {
	ctx, tx := bfTx(t)
	up, post, down := rcSections(t)
	mail := bfEmail("rc-runner")
	row := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), mail, "active", 7, opFakeDigest("a"))

	t.Run("a runner that obeys RLS is refused", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			opRunSection(t, ctx, sp, down, "00082 Down")
			aiExec(t, ctx, sp, `CREATE ROLE zz_m14_06b_rls_runner NOLOGIN NOSUPERUSER NOBYPASSRLS`)
			aiExec(t, ctx, sp, `GRANT tappa_owner TO zz_m14_06b_rls_runner`)
			aiExec(t, ctx, sp, `SET LOCAL SESSION AUTHORIZATION zz_m14_06b_rls_runner`)
			bfWantRefusal(t, holdingNestedRun(t, ctx, sp, up), "neither superuser nor BYPASSRLS", "the Up as a runner that obeys RLS", mail)
			bfWantRefusal(t, holdingNestedRun(t, ctx, sp, down), "neither superuser nor BYPASSRLS", "the Down as a runner that obeys RLS")
			aiExec(t, ctx, sp, `SET LOCAL SESSION AUTHORIZATION DEFAULT`)
			if !bfUnlinked(t, ctx, sp, row) {
				t.Fatalf("the refused run linked the row: %s", bfRow(t, ctx, sp, row))
			}
		})
	})

	t.Run("a definer that lost BYPASSRLS: the Up refuses", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			opRunSection(t, ctx, sp, down, "00082 Down")
			aiExec(t, ctx, sp, `ALTER ROLE tappa_identitydefiner NOBYPASSRLS`)
			bfWantRefusal(t, holdingNestedRun(t, ctx, sp, up), "saw fewer rows than this runner", "the Up with a definer that obeys RLS", mail)
			aiExec(t, ctx, sp, `ALTER ROLE tappa_identitydefiner BYPASSRLS`)
			opRunSection(t, ctx, sp, up, "00082 Up")
			if !rcLinked(t, ctx, sp, row) {
				t.Errorf("the Up did not link the row: %s", bfRow(t, ctx, sp, row))
			}
		})
	})

	t.Run("a reconcile link without its identity's digest: refused", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			rcRun(t, ctx, sp)
			if err := holdingNestedRun(t, ctx, sp, post); err != nil {
				t.Fatalf("PREMISE: the postcondition after the pass: %v", err)
			}
			aiExec(t, ctx, sp, `UPDATE admin_identities SET password_hash = $2
			                     WHERE id = (SELECT identity_id FROM admin_users WHERE id = $1)`, row, opFakeDigest("b"))
			bfWantRefusal(t, holdingNestedRun(t, ctx, sp, post), "does not carry its identity's password digest", "the postcondition over a digest mismatch")
		})
	})

	t.Run("an identity linked through two reconcile rows: refused", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			other := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), bfEmail("rc-elsewhere"), "active", 1, opFakeDigest("a"))
			rcRun(t, ctx, sp)
			aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL WHERE id = $1`, other)
			aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = (SELECT identity_id FROM admin_users WHERE id = $1),
			                                           linked_via = 'reconcile', linked_at = now()
			                     WHERE id = $2`, row, other)
			bfWantRefusal(t, holdingNestedRun(t, ctx, sp, post), "more than one reconcile row", "the postcondition over a doubled link")
		})
	})
}

// TestIdentityReconcile00082_DownUndoesOnlyItsOwnAndUpAgain (ADR 0031 §8: a Down removes only what
// its file added -- 00073's Down for 'backfill', this one for 'reconcile'):
//   - the Down unlinks the pass's rows and deletes their identities; a backfilled row and its identity
//     stay; the function is gone, 00074's two grants it used stay; the Up again links the same rows
//     the same way;
//   - the Down refuses, changing nothing, when an identity the pass made has lived: another row linked
//     to it, a verified address, a login, a window, a last-used business, a refused row of its address,
//     a row of another table pointing at it; and when a foreign key onto admin_identities cascades.
func TestIdentityReconcile00082_DownUndoesOnlyItsOwnAndUpAgain(t *testing.T) {
	ctx, tx := bfTx(t)
	bfUp, _, _ := bfSections(t)
	up, _, down := rcSections(t)
	backfilled := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), bfEmail("rc-down-backfilled"), "active", 30, opFakeDigest("k"))
	shared := bfEmail("rc-down-shared")
	var first, second, single uuid.UUID

	identityOf := func(q pgx.Tx, id uuid.UUID) uuid.UUID {
		var i uuid.UUID
		if err := q.QueryRow(ctx, `SELECT identity_id FROM admin_users WHERE id = $1`, id).Scan(&i); err != nil {
			t.Fatal(err)
		}
		return i
	}
	shape := func(q pgx.Tx) string {
		return rcShape(t, ctx, q, first) + "\n" + rcShape(t, ctx, q, second) + "\n" + rcShape(t, ctx, q, single) + "\n" + rcShape(t, ctx, q, backfilled)
	}
	world := func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, bfUp, "00073 Up")
		first = bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), shared, "active", 2, opFakeDigest("a"))
		second = bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), shared, "active", 1, opFakeDigest("b"))
		single = bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), bfEmail("rc-down-single"), "active", 1, opFakeDigest("c"))
		rcRun(t, ctx, sp)
		if !bfLinked(t, ctx, sp, backfilled) || !rcLinked(t, ctx, sp, first) || !bfUnlinked(t, ctx, sp, second) || !rcLinked(t, ctx, sp, single) {
			t.Fatalf("PREMISE: the links before the Down:\n%s", shape(sp))
		}
	}

	t.Run("the Down undoes the pass alone and the Up does it again", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			world(sp)
			after := shape(sp)
			born := []uuid.UUID{identityOf(sp, first), identityOf(sp, single)}
			backfilledRow := bfRow(t, ctx, sp, backfilled)
			opRunSection(t, ctx, sp, down, "00082 Down")
			for _, id := range []uuid.UUID{first, second, single} {
				if !bfUnlinked(t, ctx, sp, id) {
					t.Errorf("after the Down a reconciled row is still linked: %s", bfRow(t, ctx, sp, id))
				}
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE id = ANY ($1)`, born); n != 0 {
				t.Errorf("after the Down %d of the pass's identities remain", n)
			}
			if got := bfRow(t, ctx, sp, backfilled); got != backfilledRow {
				t.Errorf("the Down touched a backfilled row: %s, was %s", got, backfilledRow)
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM pg_proc WHERE proname = 'identity_reconcile'`); n != 0 {
				t.Error("after the Down identity_reconcile exists")
			}
			// 00082 grants nothing: the two cells its body needs beyond 00071/00072's are 00074's
			// (identity_create_for_signup's), and its Down leaves them to 00074's Down.
			for _, c := range []struct{ table, col, priv string }{{"admin_users", "password_hash", "SELECT"}, {"admin_identities", "email", "INSERT"}, {"admin_identities", "password_hash", "INSERT"}} {
				var held bool
				if err := sp.QueryRow(ctx, `SELECT has_column_privilege('tappa_identitydefiner', $1::regclass, $2, $3)`, c.table, c.col, c.priv).Scan(&held); err != nil {
					t.Fatal(err)
				}
				if !held {
					t.Errorf("after the Down tappa_identitydefiner lost %s (%s) on %s, which is 00074's", c.priv, c.col, c.table)
				}
			}
			opRunSection(t, ctx, sp, up, "00082 Up again")
			if again := shape(sp); again != after {
				t.Errorf("the Up again:\n%s\nwant\n%s", again, after)
			}
		})
	})

	refusals := []struct {
		what, want string
		do         func(sp pgx.Tx)
	}{
		{"another row linked to the identity by password", "has lived", func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = $2, linked_via = 'password', linked_at = now(), password_hash = $3 WHERE id = $1`,
				second, identityOf(sp, first), opFakeDigest("a"))
		}},
		{"a verified address", "has lived", func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `UPDATE admin_identities SET email_verified_at = now() WHERE id = $1`, identityOf(sp, single))
		}},
		{"a refused row of the identity's address", "has lived", func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, second)
		}},
		{"a row of another table pointing at the identity", "a row of another table points", func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `CREATE TABLE zz_m14_06b_points (identity_id uuid NOT NULL REFERENCES admin_identities (id) ON DELETE RESTRICT)`)
			aiExec(t, ctx, sp, `INSERT INTO zz_m14_06b_points VALUES ($1)`, identityOf(sp, single))
		}},
		{"a login", "has lived", func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `UPDATE admin_identities SET last_login_at = now() WHERE id = $1`, identityOf(sp, single))
		}},
		{"an opened confirmation window", "has lived", func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `UPDATE admin_identities SET confirm_opened_at = now(), confirm_open_until = now() + interval '7 days',
			                                                 confirm_scope = 'first_verify'
			                     WHERE id = $1`, identityOf(sp, single))
		}},
		{"a last-used business", "has lived", func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `UPDATE admin_identities SET last_used_tenant = (SELECT tenant_id FROM admin_users WHERE id = $2)
			                     WHERE id = $1`, identityOf(sp, single), single)
		}},
		{"a foreign key that cascades", "cascades or nulls a delete", func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `CREATE TABLE zz_m14_06b_cascades (identity_id uuid REFERENCES admin_identities (id) ON DELETE CASCADE)`)
		}},
	}
	for _, r := range refusals {
		t.Run("refused: "+r.what, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				world(sp)
				r.do(sp)
				before := shape(sp)
				bfWantRefusal(t, holdingNestedRun(t, ctx, sp, down), r.want, "the Down over "+r.what, shared)
				if after := shape(sp); after != before {
					t.Errorf("the refused Down changed the rows:\n%s\nwant\n%s", after, before)
				}
			})
		})
	}
}

// TestIdentityReconcile00082_TheDownLocksTheIdentitiesBeforeItReadsThem pins in the Down's text what
// a suite cannot exercise without committing an identity (00073's test, the same reason): the pass's
// identities are taken FOR UPDATE after `theirs` is filled and BEFORE the first check that reads them.
func TestIdentityReconcile00082_TheDownLocksTheIdentitiesBeforeItReadsThem(t *testing.T) {
	_, _, down := rcSections(t)
	var b strings.Builder
	for _, line := range strings.Split(down, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line + "\n")
	}
	code := b.String()
	filled := regexp.MustCompile(`(?i)\bINTO\s+reconciled\s*,\s*theirs\b`).FindStringIndex(code)
	lock := regexp.MustCompile(`(?i)\bPERFORM\s+1\s+FROM\s+public\.admin_identities\s+WHERE\s+id\s*=\s*ANY\s*\(\s*theirs\s*\)\s+ORDER\s+BY\s+id\s+FOR\s+UPDATE\s*;`).FindStringIndex(code)
	if filled == nil || lock == nil {
		t.Fatalf("the Down does not fill theirs (%v) or does not lock the pass's identities (%v)", filled, lock)
	}
	firstCheck := -1
	for _, m := range regexp.MustCompile(`(?is)\bIF\s+EXISTS\b(.*?)\bTHEN\b`).FindAllStringSubmatchIndex(code, -1) {
		if regexp.MustCompile(`\btheirs\b`).MatchString(code[m[2]:m[3]]) {
			firstCheck = m[0]
			break
		}
	}
	if firstCheck < 0 || !(filled[1] <= lock[0] && lock[1] <= firstCheck) {
		t.Errorf("the identity lock is at %d, want after theirs is filled (%d) and before the first check that reads them (%d)", lock[0], filled[1], firstCheck)
	}
}
