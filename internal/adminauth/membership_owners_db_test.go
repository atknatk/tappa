package adminauth

// membership_owners_db_test.go -- M14-16b (migration 00081; ADR 0031 §5.2 and §5.12, the notes of
// 2026-10-27) against real Postgres, committed, on the application pool (memberWorld):
//
//   - the ghost owner (backlog T169): an owner row the person refused is no owner for the last-owner
//     rule -- resolve_admin_owners_by_tenant does not return it;
//   - the race window (T170): a session opened beside a disable never outlives it, because
//     identity_open_session / identity_switch_session read the membership FOR NO KEY UPDATE.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// refuse stamps row as refused the way identity_unlink_backfilled leaves it before contract B: not
// linked, unlinked_at set, still 'active'. The cleanup clears the stamp again (the tenant and the row
// stay, as every world here leaves them).
func (w *memberWorld) refuse(row uuid.UUID) {
	w.t.Helper()
	w.exec(`UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL, unlinked_at = now() WHERE id = $1`, row)
	w.t.Cleanup(func() {
		if _, err := w.owner.Exec(context.Background(), `UPDATE admin_users SET unlinked_at = NULL WHERE id = $1`, row); err != nil {
			w.t.Errorf("cleanup: clear the refusal: %v", err)
		}
	})
}

// TestMemberships_ARefusedOwnerIsNoOwnerForTheLastOwnerRule is the card's ghost-owner test: the
// business has its real owner and an owner row the person REFUSED (role owner, status active,
// unlinked_at stamped -- tappa_app sees two active owners). The real owner cannot step down: it is the
// last owner who can sign in. The list marks the refused row and not the real one. The refused row can
// be made manager or disabled (it takes no owner who can sign in away). CONTROL: once a second real
// owner exists, the step-down goes through. MUTATION (measured): the rule counting the locked owners
// instead of db.SignInOwners -> the step-down goes through beside the ghost, red.
func TestMemberships_ARefusedOwnerIsNoOwnerForTheLastOwnerRule(t *testing.T) {
	w := newMemberWorld(t)
	ctx := context.Background()
	ghost := w.person(w.tenant, "owner")
	w.refuse(ghost)
	if n := w.int(`SELECT count(*) FROM admin_users WHERE tenant_id = $1 AND role = 'owner' AND status = 'active'`, w.tenant); n != 2 {
		t.Fatalf("PREMISE: %d active owner rows, want 2 (the real owner and the refused one)", n)
	}

	if _, err := w.s.Change(ctx, w.tenant, w.owner1, w.owner1, MemberMakeManager); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("the real owner stepping down beside a refused owner = %v, want ErrLastOwner", err)
	}
	if got := w.roleStatus(w.owner1); got != "owner/active" {
		t.Fatalf("after the refusal the real owner is %s", got)
	}
	if n := len(w.details(w.tenant, ActionMembershipDemoted)); n != 0 {
		t.Fatalf("the refused step-down wrote %d trail row(s)", n)
	}

	list, err := w.s.List(ctx, w.tenant)
	if err != nil {
		t.Fatal(err)
	}
	marked := map[uuid.UUID]bool{}
	for _, m := range list {
		marked[m.ID] = m.RefusedOwner
	}
	if !marked[ghost] || marked[w.owner1] {
		t.Errorf("the list marks the refused owner %v and the real one %v; want true, false", marked[ghost], marked[w.owner1])
	}

	// Taking the refused row's owner seat away takes no owner who can sign in away.
	if _, err := w.s.Change(ctx, w.tenant, w.owner1, ghost, MemberMakeManager); err != nil {
		t.Fatalf("making the refused owner a manager = %v, want it allowed", err)
	}
	if _, err := w.s.Change(ctx, w.tenant, w.owner1, ghost, MemberDisable); err != nil {
		t.Fatalf("disabling the refused row = %v, want it allowed", err)
	}
	list, err = w.s.List(ctx, w.tenant)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range list {
		if m.RefusedOwner {
			t.Errorf("a row that is no longer an active owner is marked: %+v", m.ID)
		}
	}

	// CONTROL: a second owner who can sign in -- the step-down goes through.
	heir := w.person(w.tenant, "manager")
	if _, err := w.s.Change(ctx, w.tenant, w.owner1, heir, MemberMakeOwner); err != nil {
		t.Fatalf("make the heir an owner: %v", err)
	}
	if _, err := w.s.Change(ctx, w.tenant, w.owner1, w.owner1, MemberMakeManager); err != nil {
		t.Fatalf("CONTROL: stepping down beside a real second owner = %v", err)
	}
}

// raceWorld is the race window's fixture: the person (w.ident) holds a manager membership in the
// business (target) and one in another business (from, the switch's source), and a blocker -- an
// owner connection holding an UNCOMMITTED session row with the token hash the opening will use, so the
// opening's INSERT waits on it after the function has read the membership: the exact window
// (00081's header).
type raceWorld struct {
	*memberWorld
	target, from uuid.UUID
	fromHash     string
}

func newRaceWorld(t *testing.T) *raceWorld {
	t.Helper()
	w := &raceWorld{memberWorld: newMemberWorld(t)}
	w.target = w.linked(w.tenant, "active", "backfill", 48*time.Hour)
	w.exec(`UPDATE admin_users SET role = 'manager' WHERE id = $1`, w.target)
	b := w.tenant2("M14-16b Source Co")
	w.from = w.linked(b, "active", "backfill", 48*time.Hour)
	w.fromHash = "m14-16b-" + uuid.NewString()
	w.exec(`INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3)`, b, w.from, w.fromHash)
	return w
}

// blockedBy waits for a backend blocked by pid and returns it -- or reports that done closed first.
func (w *raceWorld) blockedBy(pid int32, done <-chan struct{}) (int32, bool) {
	w.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return 0, true
		default:
		}
		var blocked int32
		err := w.owner.QueryRow(context.Background(),
			`SELECT pid FROM pg_stat_activity WHERE $1::int = ANY(pg_blocking_pids(pid)) ORDER BY pid LIMIT 1`, pid).Scan(&blocked)
		if err == nil {
			return blocked, false
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			w.t.Fatalf("pg_stat_activity: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.t.Fatalf("nothing was blocked by backend %d within 10 s", pid)
	return 0, false
}

// race runs open (an identity_open_session or identity_switch_session whose new session's hash is
// token) against a disable of w.target, in the order the window needs: the opening reads the membership
// and is held at its INSERT by the blocker; the disable starts then, and either finishes (no lock --
// the window is open) or waits on the opening (the lock); the blocker rolls back; both end.
func (w *raceWorld) race(open func(token string) error) (MembershipChange, string, error) {
	w.t.Helper()
	ctx := context.Background()
	newHash := "m14-16b-" + uuid.NewString()
	hold := ownerConn(w.t)
	var holder int32
	if err := hold.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&holder); err != nil {
		w.t.Fatal(err)
	}
	htx, err := hold.Begin(ctx)
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err := htx.Exec(ctx, `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3)`, w.tenant, w.owner1, newHash); err != nil {
		w.t.Fatal(err)
	}

	openDone, openErr := make(chan struct{}), make(chan error, 1)
	go func() {
		defer close(openDone)
		openErr <- open(newHash)
	}()
	opener, finished := w.blockedBy(holder, openDone)
	if finished {
		_ = htx.Rollback(ctx)
		w.t.Fatalf("PREMISE: the opening did not wait on the blocker (%v)", <-openErr)
	}

	changeDone := make(chan struct{})
	var ch MembershipChange
	var changeErr error
	go func() {
		defer close(changeDone)
		ch, changeErr = w.s.Change(ctx, w.tenant, w.owner1, w.target, MemberDisable)
	}()
	w.blockedBy(opener, changeDone) // the disable either finished or waits on the opening

	if err := htx.Rollback(ctx); err != nil {
		w.t.Fatal(err)
	}
	<-openDone
	<-changeDone
	if err := <-openErr; err != nil {
		w.t.Fatalf("the opening: %v", err)
	}
	return ch, newHash, changeErr
}

// liveOn counts the live sessions of row.
func (w *raceWorld) liveOn(row uuid.UUID) int {
	return w.int(`SELECT count(*) FROM admin_sessions WHERE admin_user_id = $1 AND revoked_at IS NULL`, row)
}

// TestMemberships_ASessionOpenedBesideADisableDoesNotOutliveIt is the race window of backlog T170,
// closed (00081): a sign-in (identity_open_session asking for the business) and a switch into it
// (identity_switch_session), each held right after its membership read -- the blocker holds its INSERT
// -- while the business's owner disables that membership. The disable waits on the opening's row lock,
// so its revocation sees the opened session: the disabled row has NO live session the moment both end,
// and after an enable the session still does not pass TouchAdminSession. Each opening's session is
// revoked by the disable itself (the disable's trail counts it), not by the enable.
//
// MUTATIONS (measured): (1) 00071's bodies back (no FOR NO KEY UPDATE) -> the disable finishes first,
// the session lands live on the disabled row: red for both openings; (2) the enable's revocation
// removed WITH the lock present -> still green (the lock alone closes the window; the enable's
// revocation is the second layer).
func TestMemberships_ASessionOpenedBesideADisableDoesNotOutliveIt(t *testing.T) {
	for _, c := range []struct {
		name string
		open func(w *raceWorld, token string) error
	}{
		{"sign-in", func(w *raceWorld, token string) error {
			s, err := testDB(w.t).OpenIdentitySession(context.Background(), w.ident, w.tenant, token, db.NewPasswordHash(w.digest))
			if err == nil && s.AdminUserID != w.target {
				return errors.New("the sign-in landed on another membership")
			}
			return err
		}},
		{"switch", func(w *raceWorld, token string) error {
			s, err := testDB(w.t).SwitchIdentitySession(context.Background(), w.fromHash, w.tenant, token)
			if err == nil && s.AdminUserID != w.target {
				return errors.New("the switch landed on another membership")
			}
			return err
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newRaceWorld(t)
			ch, token, err := w.race(func(token string) error { return c.open(w, token) })
			if err != nil {
				t.Fatalf("the disable: %v", err)
			}
			if got := w.roleStatus(w.target); got != "manager/disabled" {
				t.Fatalf("PREMISE: the membership is %s after the disable", got)
			}
			var opened uuid.UUID
			if err := w.owner.QueryRow(context.Background(),
				`SELECT id FROM admin_sessions WHERE admin_user_id = $1 AND token_hash = $2`, w.target, token).Scan(&opened); err != nil {
				t.Fatalf("PREMISE: the opening wrote no session: %v", err)
			}
			if n := w.liveOn(w.target); n != 0 {
				t.Fatalf("the disabled membership holds %d live session(s): the session opened beside the disable outlived it", n)
			}
			if ch.RevokedSessions != 1 {
				t.Errorf("the disable revoked %d session(s), want the opened one", ch.RevokedSessions)
			}
			if _, err := w.s.Change(context.Background(), w.tenant, w.owner1, w.target, MemberEnable); err != nil {
				t.Fatalf("enable: %v", err)
			}
			if w.touches(w.tenant, opened) {
				t.Fatal("after the enable the session opened beside the disable passes TouchAdminSession")
			}
		})
	}
}
