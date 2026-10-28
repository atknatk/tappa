package adminauth

// membership_db_test.go -- a business's memberships (M14-16, ADR 0031 §5.12) against real Postgres,
// through Memberships on the APPLICATION pool (tappa_app), the worlds written and cleaned up as the
// owner (idWorld). Committed: the last-owner race needs two real transactions.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// touches reports whether TouchAdminSession -- the panel's per-request authority -- accepts session.
func (w *memberWorld) touches(tenant, session uuid.UUID) bool {
	w.t.Helper()
	ok := false
	err := testDB(w.t).WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).TouchAdminSession(ctx, store.TouchAdminSessionParams{ID: session, TenantID: tenant})
		if errors.Is(e, pgx.ErrNoRows) {
			return nil
		}
		ok = e == nil
		return e
	})
	if err != nil {
		w.t.Fatalf("TouchAdminSession: %v", err)
	}
	return ok
}

// memberWorld is one business with one owner (owner), around an idWorld (a person with an identity).
type memberWorld struct {
	*idWorld
	s      *Memberships
	tenant uuid.UUID
	owner1 uuid.UUID
}

func newMemberWorld(t *testing.T) *memberWorld {
	t.Helper()
	d := testDB(t)
	trail, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewMemberships(d, trail)
	if err != nil {
		t.Fatal(err)
	}
	w := &memberWorld{idWorld: newIDWorld(t, ownerConn(t), true), s: s}
	w.tenant = w.tenant2("M14-16 Members Co")
	w.owner1 = w.person(w.tenant, "owner")
	return w
}

// tenant2 makes a business whose audit rows are deleted at cleanup (the tenant row stays, as every
// world in this package leaves it).
func (w *memberWorld) tenant2(name string) uuid.UUID {
	return w.idWorld.tenant(name)
}

// person inserts a membership of a fresh address (no identity) with role.
func (w *memberWorld) person(tenant uuid.UUID, role string) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'M14-16 Person', $3, $4, $5)`,
		id, tenant, "m14-16-"+uuid.NewString()+"@identity.example", w.digest, role)
	return id
}

// liveSession inserts a live session for row; returns its id.
func (w *memberWorld) liveSession(tenant, row uuid.UUID) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.owner.QueryRow(context.Background(),
		`INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES ($1, $2, encode(gen_random_bytes(32), 'hex')) RETURNING id`,
		tenant, row).Scan(&id); err != nil {
		w.t.Fatalf("insert a session: %v", err)
	}
	return id
}

func (w *memberWorld) live(session uuid.UUID) bool {
	return w.int(`SELECT count(*) FROM admin_sessions WHERE id = $1 AND revoked_at IS NULL`, session) == 1
}

func (w *memberWorld) roleStatus(row uuid.UUID) string {
	w.t.Helper()
	var role, status string
	if err := w.owner.QueryRow(context.Background(), `SELECT role, status FROM admin_users WHERE id = $1`, row).Scan(&role, &status); err != nil {
		w.t.Fatal(err)
	}
	return role + "/" + status
}

// snapshot is a row as text, every column -- "untouched" means byte for byte.
func (w *memberWorld) snapshot(sql string, args ...any) string {
	w.t.Helper()
	var s string
	if err := w.owner.QueryRow(context.Background(), sql, args...).Scan(&s); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
	return s
}

// details are a business's trail rows of action as "actor|target|detail".
func (w *memberWorld) details(tenant uuid.UUID, action string) []string {
	w.t.Helper()
	rows, err := w.owner.Query(context.Background(),
		`SELECT coalesce(actor_id::text, '') || '|' || coalesce(target, '') || '|' || detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2 ORDER BY at, id`, tenant, action)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// TestMemberships_DisablingDropsOnlyThisBusinesssSessions is the card's two-business test (K-6): a
// person runs business A (manager) and business B (owner) through one identity, with live sessions in
// both. A's owner disables the person's membership in A: the person's two sessions in A are revoked,
// the session in B lives, A's owner's own session lives, the membership in B and the identity are byte
// for byte what they were, B's trail gained nothing, and A's trail holds one row naming the
// membership -- no identity, no other business, no address. Enabling again does not bring the revoked
// sessions back.
func TestMemberships_DisablingDropsOnlyThisBusinesssSessions(t *testing.T) {
	w := newMemberWorld(t)
	a := w.tenant
	b := w.tenant2("M14-16 Other Co")
	inA := w.linked(a, "active", "backfill", 48*time.Hour)
	w.exec(`UPDATE admin_users SET role = 'manager' WHERE id = $1`, inA)
	inB := w.linked(b, "active", "backfill", 48*time.Hour)
	sA1, sA2, sB := w.liveSession(a, inA), w.liveSession(a, inA), w.liveSession(b, inB)
	sOwner := w.liveSession(a, w.owner1)

	identityBefore := w.snapshot(`SELECT row_to_json(i)::text FROM admin_identities i WHERE id = $1`, w.ident)
	rowBBefore := w.snapshot(`SELECT row_to_json(u)::text FROM admin_users u WHERE id = $1`, inB)
	trailBBefore := w.int(`SELECT count(*) FROM audit_log WHERE tenant_id = $1`, b)

	ch, err := w.s.Change(context.Background(), a, w.owner1, inA, MemberDisable)
	if err != nil {
		t.Fatalf("Change(disable) = %v", err)
	}
	if ch.RevokedSessions != 2 || ch.Self {
		t.Errorf("the change = %+v, want 2 revoked sessions, not self", ch)
	}
	if w.live(sA1) || w.live(sA2) {
		t.Error("a session of the disabled membership in this business lives")
	}
	if !w.live(sB) {
		t.Error("the person's session in their OTHER business was revoked (K-6: per membership)")
	}
	if !w.live(sOwner) {
		t.Error("the owner's own session was revoked")
	}
	if got := w.roleStatus(inA); got != "manager/disabled" {
		t.Errorf("the membership is %s, want manager/disabled", got)
	}
	if got := w.snapshot(`SELECT row_to_json(i)::text FROM admin_identities i WHERE id = $1`, w.ident); got != identityBefore {
		t.Errorf("the identity changed:\n before %s\n after  %s", identityBefore, got)
	}
	if got := w.snapshot(`SELECT row_to_json(u)::text FROM admin_users u WHERE id = $1`, inB); got != rowBBefore {
		t.Errorf("the membership in the other business changed:\n before %s\n after  %s", rowBBefore, got)
	}
	if id, _ := w.link(inA); id != w.ident {
		t.Error("the disable touched the membership's link")
	}
	if n := w.int(`SELECT count(*) FROM audit_log WHERE tenant_id = $1`, b); n != trailBBefore {
		t.Errorf("the other business's trail gained %d row(s)", n-trailBBefore)
	}
	rows := w.details(a, ActionMembershipDisabled)
	want := w.owner1.String() + "|" + inA.String() + `|{"outcome": "ok", "revoked_sessions": 2}`
	if len(rows) != 1 || rows[0] != want {
		t.Fatalf("the trail holds %q, want exactly [%q]", rows, want)
	}
	for _, leak := range []string{w.ident.String(), b.String(), w.email, inB.String()} {
		if strings.Contains(rows[0], leak) {
			t.Errorf("the trail row names %q", leak)
		}
	}

	if _, err := w.s.Change(context.Background(), a, w.owner1, inA, MemberEnable); err != nil {
		t.Fatalf("Change(enable) = %v", err)
	}
	if got := w.roleStatus(inA); got != "manager/active" {
		t.Errorf("after enabling the membership is %s", got)
	}
	if w.live(sA1) || w.live(sA2) {
		t.Error("enabling brought a revoked session back")
	}
	if len(w.details(a, ActionMembershipEnabled)) != 1 {
		t.Error("the enable wrote no trail row")
	}
}

// TestMemberships_EnablingRevokesASessionThatLandedWhileDisabled is the security audit's ORTA, kept as
// the second layer: before 00081 a session opened beside a disable (identity_open_session /
// identity_switch_session read the row without a lock) could land live on the disabled row; 00081's row
// lock closed that (TestMemberships_ASessionOpenedBesideADisableDoesNotOutliveIt), and a session put on a
// disabled row by any other hand is modelled here by inserting one there directly. While the row
// is disabled TouchAdminSession refuses it; the enable revokes it in its own transaction, so it is not
// accepted afterwards either, and the trail row says one session ended. MUTATION (measured): the
// enable's revocation removed -> red (the old cookie passes TouchAdminSession).
func TestMemberships_EnablingRevokesASessionThatLandedWhileDisabled(t *testing.T) {
	w := newMemberWorld(t)
	mgr := w.person(w.tenant, "manager")
	if _, err := w.s.Change(context.Background(), w.tenant, w.owner1, mgr, MemberDisable); err != nil {
		t.Fatalf("disable: %v", err)
	}
	late := w.liveSession(w.tenant, mgr)
	if w.touches(w.tenant, late) {
		t.Fatal("CONTROL: a session of a disabled row passed TouchAdminSession")
	}
	ch, err := w.s.Change(context.Background(), w.tenant, w.owner1, mgr, MemberEnable)
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if w.touches(w.tenant, late) {
		t.Fatal("the session that landed while the row was disabled passes TouchAdminSession after the enable")
	}
	if w.live(late) || ch.RevokedSessions != 1 {
		t.Errorf("the late session live=%v, the enable revoked %d; want revoked, 1", w.live(late), ch.RevokedSessions)
	}
	fresh := w.liveSession(w.tenant, mgr)
	if !w.touches(w.tenant, fresh) {
		t.Error("CONTROL: a session opened after the enable is not accepted")
	}
	if got := w.details(w.tenant, ActionMembershipEnabled); len(got) != 1 ||
		got[0] != w.owner1.String()+"|"+mgr.String()+`|{"outcome": "ok", "revoked_sessions": 1}` {
		t.Errorf("the enable's trail = %q", got)
	}
}

// TestMemberships_OwnershipMovesInTwoAuditedSteps is the transfer (K-11): stepping down first is
// refused (the last owner); step one makes the manager an owner -- one admin.membership.promoted row;
// step two, the old owner steps down -- one admin.membership.demoted row marked self. The business
// ends with the new owner and the old one a manager; no step names an address.
func TestMemberships_OwnershipMovesInTwoAuditedSteps(t *testing.T) {
	w := newMemberWorld(t)
	heir := w.person(w.tenant, "manager")
	ctx := context.Background()

	if _, err := w.s.Change(ctx, w.tenant, w.owner1, w.owner1, MemberMakeManager); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("stepping down as the only owner = %v, want ErrLastOwner", err)
	}
	if n := len(w.details(w.tenant, ActionMembershipDemoted)); n != 0 {
		t.Fatalf("a refused step-down wrote %d trail row(s)", n)
	}

	if _, err := w.s.Change(ctx, w.tenant, w.owner1, heir, MemberMakeOwner); err != nil {
		t.Fatalf("step 1 (make owner) = %v", err)
	}
	if got := w.details(w.tenant, ActionMembershipPromoted); len(got) != 1 ||
		got[0] != w.owner1.String()+"|"+heir.String()+`|{"role": "owner", "outcome": "ok"}` {
		t.Fatalf("step 1's trail = %q", got)
	}
	if got := w.roleStatus(w.owner1) + " " + w.roleStatus(heir); got != "owner/active owner/active" {
		t.Fatalf("between the steps: %s, want two active owners", got)
	}

	ch, err := w.s.Change(ctx, w.tenant, w.owner1, w.owner1, MemberMakeManager)
	if err != nil || !ch.Self {
		t.Fatalf("step 2 (step down) = %+v, %v", ch, err)
	}
	if got := w.details(w.tenant, ActionMembershipDemoted); len(got) != 1 ||
		got[0] != w.owner1.String()+"|"+w.owner1.String()+`|{"role": "manager", "self": true, "outcome": "ok"}` {
		t.Fatalf("step 2's trail = %q", got)
	}
	if got := w.roleStatus(w.owner1) + " " + w.roleStatus(heir); got != "manager/active owner/active" {
		t.Fatalf("after the transfer: %s, want manager/active owner/active", got)
	}
	// The old owner acts no more.
	if _, err := w.s.Change(ctx, w.tenant, w.owner1, heir, MemberDisable); !errors.Is(err, ErrMemberNotOwner) {
		t.Errorf("the former owner disabling the new one = %v, want ErrMemberNotOwner", err)
	}
}

// TestMemberships_TheLastActiveOwnerCannotBeDisabledOrDemoted and the refusals around it: a business's
// only active owner cannot step down; nobody disables or enables themselves; a manager or a disabled
// owner cannot act; a role change on a disabled membership is "unchanged"; another business's
// membership is unknown here and untouched (tenant isolation: owner A never reaches B's row).
func TestMemberships_TheLastActiveOwnerCannotBeDisabledOrDemoted(t *testing.T) {
	w := newMemberWorld(t)
	ctx := context.Background()
	mgr := w.person(w.tenant, "manager")
	disabledOwner := w.person(w.tenant, "owner")
	w.exec(`UPDATE admin_users SET status = 'disabled' WHERE id = $1`, disabledOwner)
	other := w.tenant2("M14-16 Elsewhere Co")
	otherOwner := w.person(other, "owner")
	otherBefore := w.roleStatus(otherOwner)

	for _, c := range []struct {
		name          string
		actor, member uuid.UUID
		act           MembershipAct
		want          error
	}{
		{"the only active owner steps down", w.owner1, w.owner1, MemberMakeManager, ErrLastOwner},
		{"an owner disables themselves", w.owner1, w.owner1, MemberDisable, ErrMemberSelf},
		{"an owner enables themselves", w.owner1, w.owner1, MemberEnable, ErrMemberSelf},
		{"a manager acts", mgr, w.owner1, MemberDisable, ErrMemberNotOwner},
		{"a disabled owner acts", disabledOwner, mgr, MemberDisable, ErrMemberNotOwner},
		{"a disabled owner made manager", w.owner1, disabledOwner, MemberMakeManager, ErrMemberUnchanged},
		{"an active membership enabled", w.owner1, mgr, MemberEnable, ErrMemberUnchanged},
		{"another business's membership", w.owner1, otherOwner, MemberDisable, ErrMemberUnknown},
		{"another business's owner acting here", otherOwner, mgr, MemberDisable, ErrMemberNotOwner},
		{"an unknown act", w.owner1, mgr, MembershipAct("delete"), ErrMemberAct},
	} {
		if _, err := w.s.Change(ctx, w.tenant, c.actor, c.member, c.act); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.name, err, c.want)
		}
	}
	if got := w.roleStatus(w.owner1) + " " + w.roleStatus(mgr); got != "owner/active manager/active" {
		t.Errorf("after the refusals: %s", got)
	}
	if got := w.roleStatus(otherOwner); got != otherBefore {
		t.Errorf("the other business's membership changed: %s -> %s", otherBefore, got)
	}
	if n := w.int(`SELECT count(*) FROM audit_log WHERE tenant_id = ANY($1) AND action LIKE 'admin.membership.%'`, []uuid.UUID{w.tenant, other}); n != 0 {
		t.Errorf("the refusals wrote %d trail row(s)", n)
	}
}

// TestMemberships_TwoStepDownsRaceAndOneIsRefused is the card's concurrent test: a business with two
// active owners, both pressing "step down" at once -- exactly one does, the other is told it is the
// last owner, and the business keeps one active owner. beforeWrite holds the first change after its
// decision for up to 300 ms waiting for the second: without the owners' lock both would decide on
// two owners and both would write (the mutation this test exists for); with it the second is still
// waiting on the lock. Several rounds, under -race.
func TestMemberships_TwoStepDownsRaceAndOneIsRefused(t *testing.T) {
	w := newMemberWorld(t)
	owner2 := w.person(w.tenant, "owner")
	var arrived atomic.Int32
	w.s.beforeWrite = func() {
		arrived.Add(1)
		deadline := time.Now().Add(300 * time.Millisecond)
		for arrived.Load() < 2 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
	}
	for round := 0; round < 4; round++ {
		w.exec(`UPDATE admin_users SET role = 'owner', status = 'active' WHERE id = ANY($1)`, []uuid.UUID{w.owner1, owner2})
		arrived.Store(0)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		for i, who := range []uuid.UUID{w.owner1, owner2} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, errs[i] = w.s.Change(context.Background(), w.tenant, who, who, MemberMakeManager)
			}()
		}
		wg.Wait()
		ok, last := 0, 0
		for _, err := range errs {
			switch {
			case err == nil:
				ok++
			case errors.Is(err, ErrLastOwner):
				last++
			default:
				t.Fatalf("round %d: unexpected %v", round, err)
			}
		}
		if ok != 1 || last != 1 {
			t.Fatalf("round %d: %d step-downs went through and %d were refused as the last owner, want 1 and 1", round, ok, last)
		}
		if n := w.int(`SELECT count(*) FROM admin_users WHERE tenant_id = $1 AND role = 'owner' AND status = 'active'`, w.tenant); n != 1 {
			t.Fatalf("round %d: %d active owners, want 1", round, n)
		}
	}
}

// TestMemberships_TwoOwnersDisablingEachOtherRaceAndOneIsRefused: the same lock, the other shape -- two
// owners each disabling the other at once. One goes through; the other finds its actor is no longer
// an active owner (the belt re-read under the lock) and writes nothing.
func TestMemberships_TwoOwnersDisablingEachOtherRaceAndOneIsRefused(t *testing.T) {
	w := newMemberWorld(t)
	owner2 := w.person(w.tenant, "owner")
	// The first race test's hold: the first change waits after its decision for the second, so the two
	// cannot simply run one after the other -- the lock, not the timing, decides.
	var arrived atomic.Int32
	w.s.beforeWrite = func() {
		arrived.Add(1)
		deadline := time.Now().Add(300 * time.Millisecond)
		for arrived.Load() < 2 && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	pairs := [][2]uuid.UUID{{w.owner1, owner2}, {owner2, w.owner1}}
	for i, p := range pairs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = w.s.Change(context.Background(), w.tenant, p[0], p[1], MemberDisable)
		}()
	}
	wg.Wait()
	ok, refused := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrMemberNotOwner):
			refused++
		default:
			t.Fatalf("unexpected %v", err)
		}
	}
	if ok != 1 || refused != 1 {
		t.Fatalf("%d went through, %d refused; want 1 and 1", ok, refused)
	}
	if n := w.int(`SELECT count(*) FROM admin_users WHERE tenant_id = $1 AND role = 'owner' AND status = 'active'`, w.tenant); n != 1 {
		t.Fatalf("%d active owners, want 1", n)
	}
}
