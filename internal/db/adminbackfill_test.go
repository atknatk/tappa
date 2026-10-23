package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// adminbackfill_test.go -- migration 00073 (M14-06, ADR 0031 §5.8) against real Postgres: the
// backfill that gives every filled e-mail group of admin_users one identity and links the group's
// oldest ACTIVE row (else its oldest) to it; its role assertion, its closing assertion with the
// empty-database exemption (R11), its digest guard, its idempotency and its Down.
//
// 🔴 NOTHING HERE COMMITS. Every case runs the file's sections in a savepoint of bfTx's transaction
// and rolls it back -- 00069's Down refuses while an identity exists, so a committed fixture would
// lock every development database's Down walk. The savepoints are kept short on purpose: the
// backfill locks every candidate row of the shared database FOR UPDATE (committed rows of other
// packages included), and a row lock taken in a savepoint is released when the savepoint rolls
// back.
//
// bfTx is READ COMMITTED, not opTx's REPEATABLE READ: the backfill locks committed rows FOR UPDATE,
// and under REPEATABLE READ a row another package's test updated after this transaction's snapshot
// would answer 40001 -- a flake that says nothing about the file.

const bfFile = "00073_backfill_admin_identities.sql"

func bfTx(t *testing.T) (context.Context, pgx.Tx) {
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
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		t.Fatalf("BEGIN: %v", err)
	}
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

// bfSections returns 00073's Up, its closing assertion (the Up's last DO block, as goose runs it)
// and its Down.
func bfSections(t *testing.T) (up, post, down string) {
	t.Helper()
	up, down = opMigrationSections(t, bfFile)
	i := strings.LastIndex(up, "DO $$")
	j := strings.LastIndex(up, "-- +goose StatementEnd")
	if i < 0 || j < i || !strings.Contains(up[i:j], "postcondition") {
		t.Fatalf("%s's Up does not end with the closing assertion's DO block", bfFile)
	}
	return up, up[i:j], down
}

// bfAdmin inserts an admin_users row as the owner: email nil = NULL; createdDaysAgo before the
// transaction's now(); digest a bcrypt-shaped synthetic value.
func bfAdmin(t *testing.T, ctx context.Context, q pgx.Tx, tenant uuid.UUID, email any, status string, createdDaysAgo int, digest string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	aiExec(t, ctx, q, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status, created_at)
	                   VALUES ($1, $2, 'M14-06 Admin', $3, $4, 'owner', $5, now() - make_interval(days => $6))`,
		id, tenant, email, digest, status, createdDaysAgo)
	return id
}

func bfEmail(tag string) string {
	return "m14-06-" + tag + "-" + uuid.NewString() + "@backfill.example"
}

// bfRow is one admin_users row as the backfill leaves it: its link, and the identity it points at.
func bfRow(t *testing.T, ctx context.Context, q pgx.Tx, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := q.QueryRow(ctx, `
		SELECT format('identity=%s via=%s at_now=%s id_email=%s digest_copied=%s untouched=%s',
		              coalesce(a.identity_id::text, '-'), coalesce(a.linked_via, '-'),
		              coalesce((a.linked_at = now())::text, '-'), coalesce(i.email::text, '-'),
		              coalesce((a.password_hash = i.password_hash)::text, '-'),
		              coalesce((i.email_verified_at IS NULL AND i.confirm_opened_at IS NULL AND i.confirm_scope IS NULL
		                        AND i.last_login_at IS NULL AND i.last_used_tenant IS NULL)::text, '-'))
		  FROM admin_users a LEFT JOIN admin_identities i ON i.id = a.identity_id
		 WHERE a.id = $1`, id).Scan(&s); err != nil {
		t.Fatalf("read row %s: %v", id, err)
	}
	return s
}

// bfLinked reports whether a row is linked by the backfill, with its identity carrying the row's
// own address and digest, linked_at = the transaction's now(), and nothing else set on the identity.
func bfLinked(t *testing.T, ctx context.Context, q pgx.Tx, id uuid.UUID) bool {
	t.Helper()
	s := bfRow(t, ctx, q, id)
	var email string
	if err := q.QueryRow(ctx, `SELECT email::text FROM admin_users WHERE id = $1`, id).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return !strings.HasPrefix(s, "identity=-") &&
		strings.Contains(s, " via=backfill at_now=true id_email="+email+" digest_copied=true untouched=true")
}

func bfUnlinked(t *testing.T, ctx context.Context, q pgx.Tx, id uuid.UUID) bool {
	t.Helper()
	return strings.HasPrefix(bfRow(t, ctx, q, id), "identity=- via=- at_now=- id_email=-")
}

// bfIdentities counts the identities whose address is one of emails.
func bfIdentities(t *testing.T, ctx context.Context, q pgx.Tx, emails ...string) int64 {
	t.Helper()
	return opInt(t, ctx, q, `SELECT count(*) FROM admin_identities WHERE email = ANY ($1::citext[])`, emails)
}

// bfWantRefusal fails unless err is 55000 carrying want in its message and, when given, none of the
// addresses in its message, detail or hint (CLAUDE.md §7: an address never reaches a log).
func bfWantRefusal(t *testing.T, err error, want, what string, addresses ...string) {
	t.Helper()
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != sqlstatePrerequisiteState || !strings.Contains(pg.Message, want) {
		t.Fatalf("%s: %v, want 55000 carrying %q", what, err, want)
	}
	for _, a := range addresses {
		for _, part := range []string{pg.Message, pg.Detail, pg.Hint} {
			if strings.Contains(strings.ToLower(part), strings.ToLower(a)) {
				t.Errorf("%s: the refusal names an e-mail address", what)
			}
		}
	}
}

// TestAdminBackfill00073_OldestActiveAccountBirthsTheIdentity is the card's fixture (ADR 0031 §5.8):
//   - KF / KM owners (seed.sql's shape: one active owner each, distinct digests): each is linked,
//     its identity carries its address and digest, linked_via = 'backfill', linked_at = now(), the
//     identity is unverified with no window, login or last-used business; and when the database
//     carries seed.sql's own KF and KM owners unlinked (CI seeds after migrating), they are linked
//     too;
//   - the same address in two businesses (the case differs -- citext is one address): the OLDER
//     active row is linked, the LATER one -- an attacker's business carrying the victim's address --
//     is not;
//   - an EARLIER row of the address that is disabled does not birth the identity: the later ACTIVE
//     row does;
//   - a group with no active row falls back to its oldest; equal created_at goes by tenant_id;
//   - a row with no e-mail, an empty one and a blank one get nothing;
//   - a group whose address already has an identity, or that holds a linked or a refused row, is
//     left alone;
//   - the trigger (00069) is in force over the links the backfill made: the owner cannot change a
//     backfilled row's password, and the backfill itself was not refused by it.
func TestAdminBackfill00073_OldestActiveAccountBirthsTheIdentity(t *testing.T) {
	ctx, tx := bfTx(t)
	up, _, _ := bfSections(t)
	d := func(c string) string { return opFakeDigest(c) }

	kf, km := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	kfOwner := bfAdmin(t, ctx, tx, kf, bfEmail("kf-owner"), "active", 90, d("k"))
	kmOwner := bfAdmin(t, ctx, tx, km, bfEmail("km-owner"), "active", 90, d("m"))

	victimMail := bfEmail("victim")
	vt, at := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	victim := bfAdmin(t, ctx, tx, vt, victimMail, "active", 200, d("v"))
	laterAttacker := bfAdmin(t, ctx, tx, at, strings.ToUpper(victimMail), "active", 10, d("x"))

	quietMail := bfEmail("quiet")
	qa, qb := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	earlierDisabled := bfAdmin(t, ctx, tx, qa, quietMail, "disabled", 300, d("e"))
	laterActive := bfAdmin(t, ctx, tx, qb, quietMail, "active", 100, d("l"))

	goneMail := bfEmail("gone")
	ga, gb := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	oldestDisabled := bfAdmin(t, ctx, tx, ga, goneMail, "disabled", 50, d("o"))
	newerDisabled := bfAdmin(t, ctx, tx, gb, goneMail, "disabled", 40, d("n"))

	tieMail := bfEmail("tie")
	t1, t2 := aiTenant(t, ctx, tx), aiTenant(t, ctx, tx)
	if t2.String() < t1.String() {
		t1, t2 = t2, t1
	}
	tieFirst := bfAdmin(t, ctx, tx, t1, tieMail, "active", 30, d("f"))
	tieSecond := bfAdmin(t, ctx, tx, t2, tieMail, "active", 30, d("s"))

	nt := aiTenant(t, ctx, tx)
	noMail := bfAdmin(t, ctx, tx, nt, nil, "active", 500, d("q"))
	emptyMail := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), "", "active", 500, d("r"))
	blankMail := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), "   ", "active", 500, d("u"))

	// Groups the backfill must leave alone: an address that already has an identity (signup, M14-17),
	// a group holding a linked row, a group holding a refused row.
	ownMail := bfEmail("own")
	ownIdentity := aiIdentity(t, ctx, tx, ownMail)
	ownUnlinked := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), ownMail, "active", 400, d("a"))

	refusedMail := bfEmail("refused")
	refused := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), refusedMail, "active", 400, d("a"))
	aiExec(t, ctx, tx, `UPDATE admin_users SET unlinked_at = now() WHERE id = $1`, refused)
	refusedSibling := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), refusedMail, "active", 300, d("b"))

	// A group holding a row linked to an identity under ANOTHER address. No path in this tree makes
	// one yet (M14-18's "Link a business" may), so it is built as the owner: the row is unlinked when
	// it is linked, so the trigger does not fire, and it carries the identity's digest, so the
	// closing assertion's invariant holds.
	linkedMail := bfEmail("linked")
	elsewhereIdentity := aiIdentity(t, ctx, tx, bfEmail("elsewhere"))
	linkedRow := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), linkedMail, "active", 400, d("a"))
	aiExec(t, ctx, tx, `UPDATE admin_users SET identity_id = $2, linked_via = 'account', linked_at = now() WHERE id = $1`, linkedRow, elsewhereIdentity)
	linkedSibling := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), linkedMail, "active", 300, d("b"))

	var seedOwners []uuid.UUID
	rows, err := tx.Query(ctx, `SELECT id FROM admin_users
	                             WHERE id IN ('10000000-0000-4000-8000-000000000401', '20000000-0000-4000-8000-000000000401')
	                               AND identity_id IS NULL AND unlinked_at IS NULL`)
	if err != nil {
		t.Fatal(err)
	}
	seedOwners, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		t.Fatal(err)
	}

	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, up, "00073 Up")

		for _, c := range []struct {
			what string
			id   uuid.UUID
		}{
			{"the KF owner", kfOwner}, {"the KM owner", kmOwner},
			{"the older active row of a shared address (the victim)", victim},
			{"the later ACTIVE row after an earlier disabled one", laterActive},
			{"the oldest row of a group with no active row", oldestDisabled},
			{"the lower tenant_id of two rows born at the same instant", tieFirst},
		} {
			if !bfLinked(t, ctx, sp, c.id) {
				t.Errorf("%s is not linked by the backfill: %s", c.what, bfRow(t, ctx, sp, c.id))
			}
		}
		for i, id := range seedOwners {
			if !bfLinked(t, ctx, sp, id) {
				t.Errorf("seed.sql's owner %d is not linked by the backfill: %s", i, bfRow(t, ctx, sp, id))
			}
		}
		for _, c := range []struct {
			what string
			id   uuid.UUID
		}{
			{"a LATER active row carrying the victim's address (the attacker)", laterAttacker},
			{"an EARLIER disabled row of the address", earlierDisabled},
			{"the newer row of a group with no active row", newerDisabled},
			{"the higher tenant_id of two rows born at the same instant", tieSecond},
			{"a row with no e-mail", noMail}, {"a row with an empty e-mail", emptyMail}, {"a row with a blank e-mail", blankMail},
			{"a row whose address already has an identity", ownUnlinked},
			{"a refused row", refused}, {"the sibling of a refused row", refusedSibling},
			{"the sibling of a row linked under another address", linkedSibling},
		} {
			if !bfUnlinked(t, ctx, sp, c.id) {
				t.Errorf("%s is linked: %s", c.what, bfRow(t, ctx, sp, c.id))
			}
		}
		if n := bfIdentities(t, ctx, sp, victimMail, quietMail, goneMail, tieMail); n != 4 {
			t.Errorf("the four shared addresses have %d identities, want one each", n)
		}
		if n := bfIdentities(t, ctx, sp, refusedMail); n != 0 {
			t.Errorf("a group holding a refused row got %d identities, want 0", n)
		}
		if n := bfIdentities(t, ctx, sp, linkedMail); n != 0 {
			t.Errorf("a group holding a linked row got %d identities, want 0", n)
		}
		if got := opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE id = $1 AND identity_id = $2 AND linked_via = 'account'`, linkedRow, elsewhereIdentity); got != 1 {
			t.Error("the backfill moved a row linked by another way")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE id = $1 AND password_hash = $2`, ownIdentity, d("a")); n != 1 {
			t.Error("the backfill changed an identity it did not make")
		}
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE email = '' OR btrim(email::text) = ''`); n != 0 {
			t.Errorf("%d identities under an empty or blank address", n)
		}
		var victimDigest string
		if err := sp.QueryRow(ctx, `SELECT password_hash FROM admin_identities WHERE email = $1`, victimMail).Scan(&victimDigest); err != nil {
			t.Fatal(err)
		}
		if victimDigest != d("v") {
			t.Error("the shared address's identity does not carry the victim's digest")
		}

		// The trigger is in force over a backfilled link (00069): the owner cannot write its password.
		opWant(t, opTry(t, ctx, sp, `UPDATE admin_users SET password_hash = $2 WHERE id = $1`, victim, d("z")),
			sqlstateRestrictViolation, "the owner changing a backfilled row's password_hash")
	})
}

// TestAdminBackfill00073_ItIsIdempotent: a second run of the Up changes nothing -- the same rows
// linked to the same identities with the same linked_at, no new identity, the closing assertion
// green again.
func TestAdminBackfill00073_ItIsIdempotent(t *testing.T) {
	ctx, tx := bfTx(t)
	up, _, _ := bfSections(t)
	shared, single := bfEmail("twice-shared"), bfEmail("twice-single")
	ids := []uuid.UUID{
		bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), shared, "active", 20, opFakeDigest("a")),
		bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), shared, "active", 10, opFakeDigest("b")),
		bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), single, "disabled", 5, opFakeDigest("c")),
		bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), nil, "active", 5, opFakeDigest("d")),
	}
	snapshot := func(q pgx.Tx) string {
		var b strings.Builder
		for _, id := range ids {
			b.WriteString(bfRow(t, ctx, q, id) + "\n")
		}
		// Every identity born in this transaction (created_at = its now()), not only the fixture's: the
		// second run must birth none anywhere. Another session's identity has another now().
		fmt.Fprintf(&b, "identities=%d born_here=%d", bfIdentities(t, ctx, q, shared, single),
			opInt(t, ctx, q, `SELECT count(*) FROM admin_identities WHERE created_at = now()`))
		return b.String()
	}
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		opRunSection(t, ctx, sp, up, "00073 Up")
		first := snapshot(sp)
		if !strings.Contains(first, "identities=2 ") {
			t.Fatalf("PREMISE: the first run made no identity per address:\n%s", first)
		}
		opRunSection(t, ctx, sp, up, "00073 Up, again")
		if second := snapshot(sp); second != first {
			t.Errorf("the second run changed the state:\n%s\nwant\n%s", second, first)
		}
	})
}

// TestAdminBackfill00073_TheRunnerMustSeeEveryRow is the role assertion (ADR 0031 §5.8): a runner
// that is neither superuser nor BYPASSRLS -- here a role that inherits tappa_owner's ownership, so
// nothing but FORCE ROW LEVEL SECURITY stands between it and the rows -- is refused before it
// reads a row (without the assertion it would see 0 rows, link nothing and pass); a BYPASSRLS
// runner that is not superuser back-fills; the Down refuses the same runner.
func TestAdminBackfill00073_TheRunnerMustSeeEveryRow(t *testing.T) {
	ctx, tx := bfTx(t)
	up, _, down := bfSections(t)
	mail := bfEmail("runner")
	row := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), mail, "active", 7, opFakeDigest("a"))

	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		aiExec(t, ctx, sp, `CREATE ROLE zz_m14_06_rls_runner NOLOGIN NOSUPERUSER NOBYPASSRLS`)
		aiExec(t, ctx, sp, `GRANT tappa_owner TO zz_m14_06_rls_runner`)
		aiExec(t, ctx, sp, `CREATE ROLE zz_m14_06_bypass_runner NOLOGIN NOSUPERUSER BYPASSRLS`)
		aiExec(t, ctx, sp, `GRANT tappa_owner TO zz_m14_06_bypass_runner`)

		aiExec(t, ctx, sp, `SET LOCAL SESSION AUTHORIZATION zz_m14_06_rls_runner`)
		// PREMISE: the runner reaches the table and sees nothing in it -- the silent zero.
		if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_users WHERE id = $1`, row); n != 0 {
			t.Fatalf("PREMISE: a runner that obeys RLS sees the row (%d)", n)
		}
		bfWantRefusal(t, holdingNestedRun(t, ctx, sp, up), "neither superuser nor BYPASSRLS", "the Up as a runner that obeys RLS", mail)
		bfWantRefusal(t, holdingNestedRun(t, ctx, sp, down), "neither superuser nor BYPASSRLS", "the Down as a runner that obeys RLS")
		aiExec(t, ctx, sp, `SET LOCAL SESSION AUTHORIZATION DEFAULT`)
		if !bfUnlinked(t, ctx, sp, row) {
			t.Fatalf("the refused run linked the row: %s", bfRow(t, ctx, sp, row))
		}

		aiExec(t, ctx, sp, `SET LOCAL SESSION AUTHORIZATION zz_m14_06_bypass_runner`)
		opRunSection(t, ctx, sp, up, "00073 Up as a BYPASSRLS runner that is not superuser")
		aiExec(t, ctx, sp, `SET LOCAL SESSION AUTHORIZATION DEFAULT`)
		if !bfLinked(t, ctx, sp, row) {
			t.Errorf("a BYPASSRLS runner did not link the row: %s", bfRow(t, ctx, sp, row))
		}
	})
}

// TestAdminBackfill00073_TheClosingAssertion is R11 and §5.1's invariants, each branch:
//   - a database with no filled e-mail (every address set aside in the savepoint, plus an e-mail-
//     less, an empty and a blank row): the whole Up passes and makes no identity;
//   - rows with an e-mail and none linked: the closing assertion alone refuses;
//   - a linked row whose digest is not its identity's: refused;
//   - an identity linked through two backfill rows: refused.
func TestAdminBackfill00073_TheClosingAssertion(t *testing.T) {
	ctx, tx := bfTx(t)
	up, post, _ := bfSections(t)
	setAside := func(sp pgx.Tx) {
		// Links and identities other packages committed, and every address, set aside in the
		// savepoint; the rollback restores them.
		aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL, unlinked_at = NULL
		                     WHERE identity_id IS NOT NULL OR unlinked_at IS NOT NULL OR linked_via IS NOT NULL`)
		aiExec(t, ctx, sp, `DELETE FROM admin_identities`)
	}

	t.Run("no filled e-mail: the Up passes and makes nothing", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			setAside(sp)
			aiExec(t, ctx, sp, `UPDATE admin_users SET email = NULL WHERE email IS NOT NULL`)
			bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), nil, "active", 3, opFakeDigest("a"))
			bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), "", "active", 3, opFakeDigest("b"))
			bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), " ", "active", 3, opFakeDigest("c"))
			opRunSection(t, ctx, sp, up, "00073 Up over a database with no filled e-mail")
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities`); n != 0 {
				t.Errorf("%d identities born from no filled e-mail", n)
			}
		})
	})

	t.Run("filled e-mail, none linked: refused", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			setAside(sp)
			mail := bfEmail("closing")
			bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), mail, "active", 3, opFakeDigest("a"))
			bfWantRefusal(t, holdingNestedRun(t, ctx, sp, post), "none of them is linked", "the closing assertion over unlinked rows", mail)
			opRunSection(t, ctx, sp, up, "00073 Up")
			if err := holdingNestedRun(t, ctx, sp, post); err != nil {
				t.Errorf("the closing assertion after the backfill: %v", err)
			}
		})
	})

	t.Run("a linked row without its identity's digest: refused", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			row := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), bfEmail("digest"), "active", 3, opFakeDigest("a"))
			opRunSection(t, ctx, sp, up, "00073 Up")
			aiExec(t, ctx, sp, `UPDATE admin_identities SET password_hash = $2
			                     WHERE id = (SELECT identity_id FROM admin_users WHERE id = $1)`, row, opFakeDigest("b"))
			bfWantRefusal(t, holdingNestedRun(t, ctx, sp, post), "does not carry its identity's password digest", "the closing assertion over a digest mismatch")
		})
	})

	t.Run("an identity linked through two backfill rows: refused", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			mail := bfEmail("twice")
			row := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), mail, "active", 3, opFakeDigest("a"))
			other := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), bfEmail("elsewhere"), "active", 1, opFakeDigest("a"))
			opRunSection(t, ctx, sp, up, "00073 Up")
			aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL WHERE id = $1`, other)
			aiExec(t, ctx, sp, `UPDATE admin_users SET identity_id = (SELECT identity_id FROM admin_users WHERE id = $1),
			                                           linked_via = 'backfill', linked_at = now()
			                     WHERE id = $2`, row, other)
			bfWantRefusal(t, holdingNestedRun(t, ctx, sp, post), "more than one backfill row", "the closing assertion over a doubled link")
		})
	})
}

// TestAdminBackfill00073_ADigestTheIdentityRefusesStopsItByCount: admin_users' bcrypt CHECK is NOT
// VALID (00018), admin_identities' is not. A chosen row older than 00018 with a digest of another
// shape stops the Up with 55000 and a COUNT -- not the INSERT's check_violation, whose detail would
// print the failing row and its address -- and nothing is written. (The CHECK is dropped inside the
// savepoint to write such a row; the rollback restores it.)
func TestAdminBackfill00073_ADigestTheIdentityRefusesStopsItByCount(t *testing.T) {
	ctx, tx := bfTx(t)
	up, _, _ := bfSections(t)
	mail := bfEmail("legacy")
	aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
		lockForDowns(t, ctx, sp, []string{"admin_users"})
		aiExec(t, ctx, sp, `ALTER TABLE admin_users DROP CONSTRAINT admin_users_password_hash_is_bcrypt`)
		row := bfAdmin(t, ctx, sp, aiTenant(t, ctx, sp), mail, "active", 3, "legacy-"+opRandHex(t))
		bfWantRefusal(t, holdingNestedRun(t, ctx, sp, up), "1 chosen admin_users row(s) carry a password digest that is not bcrypt-shaped",
			"the Up over a chosen row with a legacy digest", mail)
		if !bfUnlinked(t, ctx, sp, row) || bfIdentities(t, ctx, sp, mail) != 0 {
			t.Errorf("the refused Up wrote something: %s", bfRow(t, ctx, sp, row))
		}
	})
}

// TestAdminBackfill00073_DownUndoesOnlyItsOwnAndUpAgain is the card's Down criterion (bulgu 22):
//   - the Down unlinks the backfill's rows and deletes their identities -- a row linked by another
//     way (signup) and its identity stay; the trigger (00069) fires on the unlink and lets it pass
//     (no credential changes);
//   - the Up again links the same rows to identities with the same addresses and digests;
//   - the Down refuses, changing nothing, when an identity the backfill made has lived: another
//     row linked to it (password), a verified address, a login, an opened confirmation window, a
//     last-used business, a refused row of its address, a row of another table pointing at it; and
//     when a foreign key onto admin_identities would cascade.
func TestAdminBackfill00073_DownUndoesOnlyItsOwnAndUpAgain(t *testing.T) {
	ctx, tx := bfTx(t)
	up, _, down := bfSections(t)
	shared := bfEmail("down-shared")
	first := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), shared, "active", 20, opFakeDigest("a"))
	second := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), shared, "active", 10, opFakeDigest("b"))
	single := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), bfEmail("down-single"), "active", 5, opFakeDigest("c"))

	signupMail := bfEmail("down-signup")
	signupIdentity := aiIdentity(t, ctx, tx, signupMail)
	signupRow := bfAdmin(t, ctx, tx, aiTenant(t, ctx, tx), signupMail, "active", 1, opFakeDigest("a"))
	aiExec(t, ctx, tx, `UPDATE admin_users SET identity_id = $2, linked_via = 'signup', linked_at = now() WHERE id = $1`, signupRow, signupIdentity)
	signupBefore := bfRow(t, ctx, tx, signupRow)

	shape := func(q pgx.Tx, id uuid.UUID) string { // the row's link without the identity's id
		s := bfRow(t, ctx, q, id)
		return s[strings.Index(s, " via="):]
	}
	backfilled := func(q pgx.Tx) string {
		return shape(q, first) + "\n" + shape(q, second) + "\n" + shape(q, single)
	}
	identityOf := func(q pgx.Tx, id uuid.UUID) uuid.UUID {
		var i uuid.UUID
		if err := q.QueryRow(ctx, `SELECT identity_id FROM admin_users WHERE id = $1`, id).Scan(&i); err != nil {
			t.Fatal(err)
		}
		return i
	}

	t.Run("the Down undoes the backfill alone and the Up does it again", func(t *testing.T) {
		aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			opRunSection(t, ctx, sp, up, "00073 Up")
			after := backfilled(sp)
			born := []uuid.UUID{identityOf(sp, first), identityOf(sp, single)}
			if !bfLinked(t, ctx, sp, first) || !bfUnlinked(t, ctx, sp, second) || !bfLinked(t, ctx, sp, single) {
				t.Fatalf("PREMISE: the Up's links:\n%s", after)
			}
			opRunSection(t, ctx, sp, down, "00073 Down")
			for _, id := range []uuid.UUID{first, second, single} {
				if !bfUnlinked(t, ctx, sp, id) {
					t.Errorf("after the Down a backfilled row is still linked: %s", bfRow(t, ctx, sp, id))
				}
			}
			if n := opInt(t, ctx, sp, `SELECT count(*) FROM admin_identities WHERE id = ANY ($1)`, born); n != 0 {
				t.Errorf("after the Down %d of the backfill's identities remain", n)
			}
			if got := bfRow(t, ctx, sp, signupRow); got != signupBefore {
				t.Errorf("the Down touched a row linked by signup: %s, was %s", got, signupBefore)
			}
			opRunSection(t, ctx, sp, up, "00073 Up again")
			if again := backfilled(sp); again != after {
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
			aiExec(t, ctx, sp, `CREATE TABLE zz_m14_06_points (identity_id uuid NOT NULL REFERENCES admin_identities (id) ON DELETE RESTRICT)`)
			aiExec(t, ctx, sp, `INSERT INTO zz_m14_06_points VALUES ($1)`, identityOf(sp, single))
		}},
		{"a login", "has lived", func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `UPDATE admin_identities SET last_login_at = now() WHERE id = $1`, identityOf(sp, single))
		}},
		{"an opened confirmation window", "has lived", func(sp pgx.Tx) {
			// All three columns: 00069's confirm_window_whole CHECK.
			aiExec(t, ctx, sp, `UPDATE admin_identities SET confirm_opened_at = now(), confirm_open_until = now() + interval '7 days',
			                                                 confirm_scope = 'first_verify'
			                     WHERE id = $1`, identityOf(sp, single))
		}},
		{"a last-used business", "has lived", func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `UPDATE admin_identities SET last_used_tenant = (SELECT tenant_id FROM admin_users WHERE id = $2)
			                     WHERE id = $1`, identityOf(sp, single), single)
		}},
		{"a foreign key that cascades", "cascades or nulls a delete", func(sp pgx.Tx) {
			aiExec(t, ctx, sp, `CREATE TABLE zz_m14_06_cascades (identity_id uuid REFERENCES admin_identities (id) ON DELETE CASCADE)`)
		}},
	}
	for _, r := range refusals {
		t.Run("refused: "+r.what, func(t *testing.T) {
			aiSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				opRunSection(t, ctx, sp, up, "00073 Up")
				r.do(sp)
				before := backfilled(sp)
				bfWantRefusal(t, holdingNestedRun(t, ctx, sp, down), r.want, "the Down over "+r.what, shared)
				if after := backfilled(sp); after != before {
					t.Errorf("the refused Down changed the rows:\n%s\nwant\n%s", after, before)
				}
			})
		})
	}
}
