package adminauth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// linkrace_db_test.go -- T179 against a real PostgreSQL: a row LINKED to an identity while today's
// (unlinked-row) password change or reset consumption runs. Both write admin_users.password_hash
// directly; 00069's trigger refuses that on a linked row (restrict_violation), which used to reach
// the person as a 500. The statements now write unlinked rows only (`identity_id IS NULL`) and
// adminauth sends the 0 rows through the identity.
//
// THE INTERLEAVING IS DETERMINISTIC, NOT A SLEEP: beforeStatement runs the link immediately before
// the named sqlc statement is sent, inside the application's own transaction, in one of two timings:
//
//	committed  the link commits first -- the statement's snapshot already sees it;
//	waiting    the link holds the row's lock, uncommitted, while the statement is sent; it commits
//	           only once pg_locks shows the statement waiting on its transaction, so the
//	           statement re-reads the linked row version (READ COMMITTED's re-check) -- the
//	           shape a linker mid-transaction gives.
//
// THE LINKERS ARE WRITTEN AS THE OWNER, in the two shapes the definers write (the trigger looks at
// OLD.identity_id alone, so who links does not change what the write meets): `reconcile` -- the
// identity_reconcile / identity_link_legacy shape, the row's digest kept and its sessions alive;
// `email` -- identity_link_by_email's (00084), the identity's digest written over the row's and the
// row's sessions revoked; `other` -- a row linked to an identity whose password is NOT the row's,
// the identity's digest written over the row's (the dual-write) and the row's sessions kept alive:
// the shape that pins that the retry re-proves the current password against the IDENTITY.
//
// MUTATIONS (measured, T179): drop `AND identity_id IS NULL` from SetOwnAdminPassword
// (db/queries/admins.sql) or from ConsumePasswordResetAndSetPassword (db/queries/passwordresets.sql),
// sqlc generate -> all four cases of the matching test below are red with SQLSTATE 23001 (the
// trigger's restrict_violation); keep the predicates but drop changeRowPassword's "still active ->
// errRowLinkedMeanwhile" branch, or Consume's second identity_consume_row_reset -> all four red
// (ErrNoSuchAdmin / ErrResetUnusable).

// beforeStatement is a database whose WithTenant hands the callback a transaction that runs act
// immediately before the sqlc statement called name is sent -- once.
type beforeStatement struct {
	*db.DB
	name string
	once sync.Once
	act  func()
}

func (b *beforeStatement) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return b.DB.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, statementHookTx{Tx: tx, b: b})
	})
}

// statementHookTx is the transaction beforeStatement hands out: a :one statement goes through QueryRow.
type statementHookTx struct {
	pgx.Tx
	b *beforeStatement
}

func (h statementHookTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "-- name: "+h.b.name+" ") {
		h.b.once.Do(h.b.act)
	}
	return h.Tx.QueryRow(ctx, sql, args...)
}

// raceLinker links one row in the middle of a statement (see the file comment).
type raceLinker struct {
	t       *testing.T
	w       *idWorld
	row     uuid.UUID
	tenant  uuid.UUID
	shape   string // "reconcile", "email" or "other"
	waiting bool
	done    chan error
}

// act is beforeStatement's act: the link, committed now or committed once the statement waits on it.
// It holds the operator-tables lock through its own owner connection (ownerConn), as every writer of
// a committed admin_users row here does.
func (l *raceLinker) act() {
	ctx := context.Background()
	conn := ownerConn(l.t)
	var stmts []string
	switch l.shape {
	case "reconcile":
		stmts = []string{`UPDATE admin_users SET identity_id = $2, linked_via = 'reconcile', linked_at = now() WHERE id = $1 AND tenant_id = $3`}
	case "email":
		stmts = []string{
			`UPDATE admin_users SET identity_id = $2, linked_via = 'email', linked_at = now(),
			        password_hash = (SELECT password_hash FROM admin_identities WHERE id = $2) WHERE id = $1 AND tenant_id = $3`,
			`UPDATE admin_sessions SET revoked_at = now() WHERE admin_user_id = $1 AND tenant_id = $3 AND revoked_at IS NULL AND $2::uuid IS NOT NULL`,
		}
	case "other":
		stmts = []string{`UPDATE admin_users SET identity_id = $2, linked_via = 'reconcile', linked_at = now(),
		        password_hash = (SELECT password_hash FROM admin_identities WHERE id = $2) WHERE id = $1 AND tenant_id = $3`}
	default:
		l.done <- errors.New("unknown linker shape " + l.shape)
		return
	}
	if !l.waiting {
		for _, s := range stmts {
			if _, err := conn.Exec(ctx, s, l.row, l.w.ident, l.tenant); err != nil {
				l.done <- err
				return
			}
		}
		l.done <- nil
		return
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		l.done <- err
		return
	}
	for _, s := range stmts {
		if _, err := tx.Exec(ctx, s, l.row, l.w.ident, l.tenant); err != nil {
			l.done <- errors.Join(err, tx.Rollback(ctx))
			return
		}
	}
	var xid string
	if err := tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&xid); err != nil {
		l.done <- errors.Join(err, tx.Rollback(ctx))
		return
	}
	// The statement is sent after act returns; the commit waits until it is blocked on this
	// transaction. The probe runs on the world's owner connection, not this one.
	go func() {
		deadline := time.Now().Add(15 * time.Second)
		for {
			var waiters int
			if err := l.w.owner.QueryRow(context.Background(),
				`SELECT count(*) FROM pg_locks WHERE locktype = 'transactionid' AND transactionid = $1::text::xid AND NOT granted`,
				xid).Scan(&waiters); err != nil {
				l.done <- errors.Join(err, tx.Rollback(context.Background()))
				return
			}
			if waiters > 0 {
				break
			}
			if time.Now().After(deadline) {
				l.done <- errors.Join(errors.New("the statement never waited on the linker"), tx.Rollback(context.Background()))
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		l.done <- tx.Commit(context.Background())
	}()
}

// wait is the linker's own outcome; a link that did not happen makes the case meaningless.
func (l *raceLinker) wait() {
	l.t.Helper()
	select {
	case err := <-l.done:
		if err != nil {
			l.t.Fatalf("the linker (%s, waiting=%v): %v", l.shape, l.waiting, err)
		}
	case <-time.After(30 * time.Second):
		l.t.Fatalf("the linker (%s, waiting=%v) never ran -- the statement it waits for was not sent", l.shape, l.waiting)
	}
	if ident, _ := l.w.link(l.row); ident != l.w.ident {
		l.t.Fatalf("PREMISE: the row is not linked after the linker ran")
	}
}

func newRaceLinker(t *testing.T, w *idWorld, tenant, row uuid.UUID, shape string, waiting bool) *raceLinker {
	return &raceLinker{t: t, w: w, row: row, tenant: tenant, shape: shape, waiting: waiting, done: make(chan error, 1)}
}

// TestChangeOwnPassword_ARowLinkedMeanwhileGoesThroughTheIdentity (T179): an unlinked row's
// session changes its password and the row is linked between the identity check and
// SetOwnAdminPassword.
//
//	reconcile  the change is made through the identity: Recorded (the definer wrote the business
//	           its row), the identity and the row take the new digest, this session survives;
//	email      the link ended this session and gave the row the identity's password:
//	           ErrPasswordChangeSessionGone, nothing changed;
//	other      the row is linked to an identity with ANOTHER password, its session kept alive: the
//	           retry re-proves the typed (the row's old) password against the identity's digest --
//	           ErrWrongCurrentPassword, the identity's digest unchanged -- and a change typed with
//	           the identity's password then goes through.
//
// Each in both timings. Never the trigger's restrict_violation.
//
// MUTATION (measured): changeIdentityPassword's compare of the current password skipped -> the
// two `other` cases red (the row's old password rewrote the identity's).
func TestChangeOwnPassword_ARowLinkedMeanwhileGoesThroughTheIdentity(t *testing.T) {
	d := testDB(t)
	owner := ownerConn(t)
	for _, c := range []struct {
		shape   string
		waiting bool
	}{
		{"reconcile", false}, {"reconcile", true}, {"email", false}, {"email", true}, {"other", false}, {"other", true},
	} {
		name := c.shape + "/committed"
		if c.waiting {
			name = c.shape + "/waiting"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			w := newIDWorld(t, owner, true)
			tenant := w.tenant("T179 change " + name)
			rowPW, rowDigest := w.pw, w.digest
			if c.shape == "email" || c.shape == "other" {
				// The identity has a password of its own; the row has another.
				rowPW = "row-" + uuid.NewString()
				rowDigest = fixtureDigest(t, rowPW)
			}
			row := w.row(tenant, rowDigest, "active", time.Hour)
			base := cheapManager(t, testManager(t, d))
			s, _, err := w.signInOff(base, rowPW, tenant)
			if err != nil {
				t.Fatal(err)
			}
			other, _, err := w.signInOff(base, rowPW, tenant)
			if err != nil {
				t.Fatal(err)
			}

			l := newRaceLinker(t, w, tenant, row, c.shape, c.waiting)
			hook := &beforeStatement{DB: d, name: "SetOwnAdminPassword", act: l.act}
			m := cheapManager(t, testManager(t, d))
			m.data = hook
			identityBefore := w.int(`SELECT count(*) FROM admin_identities WHERE id = $1 AND password_hash = $2`, w.ident, w.digest)
			changed := w.audit(tenant, "admin.password_changed")
			next := "next-" + uuid.NewString()

			ch, err := m.ChangeOwnPassword(ctx, tenant, row, s.Session.ID, s.Token, rowPW, next)
			l.wait()
			if identityBefore != 1 {
				t.Fatal("PREMISE: the identity's digest is not the world's")
			}

			switch c.shape {
			case "reconcile":
				if err != nil || !ch.Recorded {
					t.Fatalf("ChangeOwnPassword on a row linked meanwhile = %+v, %v; want the change through the identity", ch, err)
				}
				if got := w.audit(tenant, "admin.password_changed") - changed; got != 1 {
					t.Errorf("%d admin.password_changed rows, want the definer's one", got)
				}
				var identDigest string
				if err := owner.QueryRow(ctx, `SELECT password_hash FROM admin_identities WHERE id = $1`, w.ident).Scan(&identDigest); err != nil {
					t.Fatal(err)
				}
				if !Compare(identDigest, next) {
					t.Error("the identity's digest does not verify the new password")
				}
				w.digestInvariant()
				if _, err := m.Verify(ctx, s.Token); err != nil {
					t.Errorf("the session that made the change was revoked: %v", err)
				}
				if _, err := m.Verify(ctx, other.Token); !errors.Is(err, ErrNoSession) {
					t.Errorf("the row's other session survived the change: %v", err)
				}
			case "email":
				if !errors.Is(err, ErrPasswordChangeSessionGone) {
					t.Fatalf("ChangeOwnPassword whose session the link ended = %+v, %v; want ErrPasswordChangeSessionGone", ch, err)
				}
				if got := w.audit(tenant, "admin.password_changed") - changed; got != 0 {
					t.Errorf("%d admin.password_changed rows for a change that did not happen", got)
				}
				if n := w.int(`SELECT count(*) FROM admin_identities WHERE id = $1 AND password_hash = $2`, w.ident, w.digest); n != 1 {
					t.Error("the identity's digest changed; nothing may have been written")
				}
				w.digestInvariant()
			case "other":
				if !errors.Is(err, ErrWrongCurrentPassword) {
					t.Fatalf("ChangeOwnPassword with the row's old password on a row linked meanwhile = %+v, %v; want ErrWrongCurrentPassword", ch, err)
				}
				if n := w.int(`SELECT count(*) FROM admin_identities WHERE id = $1 AND password_hash = $2`, w.ident, w.digest); n != 1 {
					t.Fatal("the row's old password rewrote the identity's digest")
				}
				if got := w.audit(tenant, "admin.password_changed") - changed; got != 0 {
					t.Errorf("%d admin.password_changed rows for a refused change", got)
				}
				w.digestInvariant()
				if _, err := m.Verify(ctx, s.Token); err != nil {
					t.Fatalf("PREMISE: the session is not alive after the link: %v", err)
				}
				ch, err = m.ChangeOwnPassword(ctx, tenant, row, s.Session.ID, s.Token, w.pw, next)
				if err != nil || !ch.Recorded {
					t.Fatalf("ChangeOwnPassword with the identity's password = %+v, %v; want the change through the identity", ch, err)
				}
				var identDigest string
				if err := owner.QueryRow(ctx, `SELECT password_hash FROM admin_identities WHERE id = $1`, w.ident).Scan(&identDigest); err != nil {
					t.Fatal(err)
				}
				if !Compare(identDigest, next) {
					t.Error("the identity's digest does not verify the new password")
				}
				w.digestInvariant()
			}
		})
	}
}

// TestConsume_ARowLinkedMeanwhileSpendsTheLinkThroughTheIdentity (T179): an unlinked row's per-row
// link is spent and the row is linked between identity_consume_row_reset (0 rows: not linked yet)
// and ConsumePasswordResetAndSetPassword. The link is spent through the identity once more --
// Recorded, the identity and the row take the new digest, every session of the row ends -- and
// it is spent exactly once. Both shapes, both timings; never the trigger's restrict_violation.
func TestConsume_ARowLinkedMeanwhileSpendsTheLinkThroughTheIdentity(t *testing.T) {
	d := testDB(t)
	owner := ownerConn(t)
	for _, c := range []struct {
		shape   string
		waiting bool
	}{
		{"reconcile", false}, {"reconcile", true}, {"email", false}, {"email", true},
	} {
		name := c.shape + "/committed"
		if c.waiting {
			name = c.shape + "/waiting"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			w := newIDWorld(t, owner, true)
			tenant := w.tenant("T179 reset " + name)
			row := w.row(tenant, w.digest, "active", time.Hour)
			m := cheapManager(t, testManager(t, d))
			s, _, err := w.signInOff(m, w.pw, tenant)
			if err != nil {
				t.Fatal(err)
			}
			plain := cheapResets(t, d)
			issued, err := plain.issue(ctx, tenant, row)
			if err != nil {
				t.Fatal(err)
			}

			l := newRaceLinker(t, w, tenant, row, c.shape, c.waiting)
			r := cheapResets(t, &beforeStatement{DB: d, name: "ConsumePasswordResetAndSetPassword", act: l.act})
			completed := w.audit(tenant, "admin.recovery.completed")
			next := "reset-" + uuid.NewString()

			got, _, err := r.Consume(ctx, issued.Token, next)
			l.wait()
			if err != nil || !got.Recorded || got.TenantID != tenant || got.AdminUserID != row {
				t.Fatalf("Consume of a link whose row was linked meanwhile = %+v, %v; want it spent through the identity", got, err)
			}
			if n := w.audit(tenant, "admin.recovery.completed") - completed; n != 1 {
				t.Errorf("%d admin.recovery.completed rows, want the definer's one", n)
			}
			var identDigest string
			if err := owner.QueryRow(ctx, `SELECT password_hash FROM admin_identities WHERE id = $1`, w.ident).Scan(&identDigest); err != nil {
				t.Fatal(err)
			}
			if !Compare(identDigest, next) {
				t.Error("the identity's digest does not verify the new password")
			}
			w.digestInvariant()
			if _, err := m.Verify(ctx, s.Token); !errors.Is(err, ErrNoSession) {
				t.Errorf("the row's session survived the reset: %v", err)
			}
			if _, _, err := plain.Consume(ctx, issued.Token, "again-"+uuid.NewString()); !errors.Is(err, ErrResetUnusable) {
				t.Errorf("the link spent again: %v, want ErrResetUnusable", err)
			}
		})
	}
}

// statementScript is beforeStatement for a sequence: each step's act runs immediately before the
// next statement of its name is sent, in order, each once.
type statementScript struct {
	*db.DB
	mu    sync.Mutex
	steps []scriptStep
}

type scriptStep struct {
	name string
	act  func()
}

func (s *statementScript) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return s.DB.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, scriptTx{Tx: tx, s: s})
	})
}

type scriptTx struct {
	pgx.Tx
	s *statementScript
}

func (h scriptTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	h.s.mu.Lock()
	var act func()
	if len(h.s.steps) > 0 && strings.HasPrefix(sql, "-- name: "+h.s.steps[0].name+" ") {
		act = h.s.steps[0].act
		h.s.steps = h.s.steps[1:]
	}
	h.s.mu.Unlock()
	if act != nil {
		act()
	}
	return h.Tx.QueryRow(ctx, sql, args...)
}

// TestChangeOwnPassword_ARowDisabledAndEnabledAgainIsNotCalledLinked (T179, the security review): an
// UNLINKED row is disabled just before SetOwnAdminPassword (0 rows) and enabled again just before
// the re-read (active) -- the shape errRowLinkedMeanwhile cannot tell from a link. No identity is
// behind the session, so the session decides:
//
//	alive    ErrNoSuchAdmin (today's "no active admin"), never ErrPasswordChangeSessionGone;
//	revoked  ErrPasswordChangeSessionGone (the disable ended it).
//
// Either way the digest is unchanged and the row is still unlinked.
//
// MUTATION (measured): ChangeOwnPassword's session check replaced by an unconditional
// ErrPasswordChangeSessionGone -> `alive` red.
func TestChangeOwnPassword_ARowDisabledAndEnabledAgainIsNotCalledLinked(t *testing.T) {
	d := testDB(t)
	owner := ownerConn(t)
	for _, revoke := range []bool{false, true} {
		name := "alive"
		if revoke {
			name = "revoked"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			w := newIDWorld(t, owner, false)
			tenant := w.tenant("T179 flip " + name)
			row := w.row(tenant, w.digest, "active", time.Hour)
			base := cheapManager(t, testManager(t, d))
			s, _, err := w.signInOff(base, w.pw, tenant)
			if err != nil {
				t.Fatal(err)
			}
			script := &statementScript{DB: d, steps: []scriptStep{
				{"GetAdminPasswordHashByID", nil},
				{"SetOwnAdminPassword", func() {
					w.exec(`UPDATE admin_users SET status = 'disabled' WHERE id = $1 AND tenant_id = $2`, row, tenant)
					if revoke {
						w.exec(`UPDATE admin_sessions SET revoked_at = now() WHERE admin_user_id = $1 AND tenant_id = $2 AND revoked_at IS NULL`, row, tenant)
					}
				}},
				{"GetAdminPasswordHashByID", func() {
					w.exec(`UPDATE admin_users SET status = 'active' WHERE id = $1 AND tenant_id = $2`, row, tenant)
				}},
			}}
			m := cheapManager(t, testManager(t, d))
			m.data = script
			_, err = m.ChangeOwnPassword(ctx, tenant, row, s.Session.ID, s.Token, w.pw, "next-"+uuid.NewString())
			if len(script.steps) != 0 {
				t.Fatalf("PREMISE: %d scripted step(s) never ran", len(script.steps))
			}
			want := ErrNoSuchAdmin
			if revoke {
				want = ErrPasswordChangeSessionGone
			}
			if !errors.Is(err, want) {
				t.Fatalf("ChangeOwnPassword = %v, want %v", err, want)
			}
			if n := w.int(`SELECT count(*) FROM admin_users WHERE id = $1 AND password_hash = $2 AND identity_id IS NULL`, row, w.digest); n != 1 {
				t.Error("the row's digest moved, or the row is linked")
			}
		})
	}
}
