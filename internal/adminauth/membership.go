package adminauth

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// THE MEMBERSHIPS OF A BUSINESS (M14-16, ADR 0031 §5.12; K-6, K-11).
//
// An owner manages who runs THIS business in the panel: disables a membership (only that business's
// sessions of that row drop -- the person's other businesses are not touched, K-6), enables it again,
// and moves a membership between owner and manager. An OWNERSHIP TRANSFER is the last two of those,
// in two steps, each its own audit row: (1) make the new person an owner -- the business then has two
// and the old owner can still take it back; (2) the old owner steps down to manager -- which only the
// last-owner invariant can refuse. No single press both gives ownership away and removes one's own.
//
// 🔴 THE LAST-OWNER INVARIANT: a business keeps at least one ACTIVE OWNER WHO CAN SIGN IN through
// every change made here -- the last one can be neither disabled nor demoted. It holds under
// concurrency by a lock, not by a read: Change first locks every active owner row of the business in
// id order (FOR NO KEY UPDATE), so two changes of one business serialise on the owners, and the second
// re-reads what the first committed (READ COMMITTED re-checks a locked row's WHERE against its newest
// version). Two owners stepping down at once: exactly one of them does
// (TestMemberships_TwoStepDownsRaceAndOneIsRefused).
//
// 🔴 WHO COUNTS (M14-16b, ADR 0031 §5.2/§5.12 notes of 2026-10-27): the owners db.SignInOwners returns
// -- resolve_admin_owners_by_tenant (00081, tappa_resolver's), the locked owners minus every row the
// person REFUSED on the confirmation list. Such a row keeps role='owner', status='active' (before
// contract B the unlink leaves it 'active') with unlinked_at stamped, which tappa_app cannot read
// (R16); it can never get a session (every session-giving path filters unlinked_at IS NULL), so it is
// no owner for this rule. Counting it let a real owner step down beside it and leave the business with
// no owner who could sign in, and no way back in the product (backlog T169). The set is read AFTER the
// owners' lock: an unlink or a relink updates the row, so on a locked row unlinked_at cannot change
// until this transaction ends (TestMemberships_ARefusedOwnerIsNoOwnerForTheLastOwnerRule).
//
// 🔴 IT IS THE PANEL'S RULE, NOT A TRIGGER, ON PURPOSE. Two writers outside this file legitimately leave
// a business with no active owner, and a database trigger would have had to name both as exceptions:
// tappa_identitydefiner's identity_unlink_backfilled -- the person's own decision to refuse a
// membership, which after contract B disables the row (ADR 0031 §5.2, T1; "the same warning, allow
// it", ONAY note 4) -- and the operator's future hard lock (ADR 0025 §7, op_disable_tenant_admins).
// The definer's path is therefore untouched by construction; internal/db's
// TestAdminMembers_TheDefinersUnlinkIsNotStoppedByTheLastOwnerRule holds it.
//
// 🔴 A REFUSED ROW STAYS REFUSED. tappa_app cannot read unlinked_at (R16), so the panel can enable a row
// the person refused (a post-contract unlink leaves it 'disabled' with unlinked_at stamped). That does
// not REOPEN it: every path that GIVES a session filters unlinked_at IS NULL (identity_open_session,
// identity_switch_session, identity_memberships, resolve_admin_by_email), and the sessions it had were
// revoked by the unlink. internal/db's TestAdminMembers_EnablingARefusedRowGivesNoSession holds it.
//
// WHAT THE PANEL CAN TELL (M14-16b): the resolver answers for ACTIVE OWNER rows alone, so a refused row
// is recognisable only while it is an active owner (List marks it, Member.RefusedOwner). A refused
// manager row or a disabled one looks like any other, and the panel still offers to make it an owner
// or to enable it. That press is harmless: the row stays without a session (above), and an owner it
// becomes is not counted here.
//
// NOTHING HERE TOUCHES AN IDENTITY: no statement names identity_id or an identity table, and the
// session revocation is the row's own in this business (RevokeAdminSessionsForAdmin) -- not
// identity_revoke_sessions, which would sign the person out of every business.
//
// AUDIT: one row per change, in the change's own transaction (it is true only if the change is), in
// this business, naming the membership's id -- never an address, a name, an identity, another
// business or a count of memberships.

// The acts an owner may take on a membership.
type MembershipAct string

const (
	MemberDisable     MembershipAct = "disable"
	MemberEnable      MembershipAct = "enable"
	MemberMakeOwner   MembershipAct = "make-owner"
	MemberMakeManager MembershipAct = "make-manager"
)

// The trail's vocabulary for the four acts (each row's target is the membership's id).
const (
	ActionMembershipDisabled = "admin.membership.disabled"
	ActionMembershipEnabled  = "admin.membership.enabled"
	ActionMembershipPromoted = "admin.membership.promoted"
	ActionMembershipDemoted  = "admin.membership.demoted"
)

const (
	memberRoleOwner     = "owner"
	memberRoleManager   = "manager"
	memberStatusActive  = "active"
	memberStatusDisable = "disabled"
)

var (
	// ErrMemberUnknown: no such membership in this business (another business's id is one of these).
	ErrMemberUnknown = errors.New("adminauth: no such membership in this business")
	// ErrMemberNotOwner: the actor is not an active owner of the business at the moment of the change
	// (the belt under the panel's policy gate -- a concurrent change may have just demoted them).
	ErrMemberNotOwner = errors.New("adminauth: the actor is not an active owner of the business")
	// ErrMemberSelf: disabling or enabling one's own membership (an owner steps down instead).
	ErrMemberSelf = errors.New("adminauth: an owner cannot disable or enable their own membership")
	// ErrLastOwner: the change would leave the business without an active owner.
	ErrLastOwner = errors.New("adminauth: the business would be left without an active owner")
	// ErrMemberUnchanged: the membership already is what the act asks (or a role change on a disabled
	// membership).
	ErrMemberUnchanged = errors.New("adminauth: the membership already is that")
	// ErrMemberAct: an act this service does not know.
	ErrMemberAct = errors.New("adminauth: unknown membership act")
)

// Member is one membership of the business as the owner's page shows it.
type Member struct {
	ID          uuid.UUID
	FullName    string
	Email       string
	Role        string
	Status      string
	CreatedAt   time.Time
	LastLoginAt *time.Time
	// RefusedOwner: an active owner row that can get no session -- the person refused it on the
	// confirmation list (M14-16b; resolve_admin_owners_by_tenant does not return it). It is not counted
	// as an owner. False for every row that is not an active owner: the panel cannot tell those.
	RefusedOwner bool
}

// Active reports whether the membership is enabled.
func (m Member) Active() bool { return m.Status == memberStatusActive }

// Owner reports whether the membership holds the owner role.
func (m Member) Owner() bool { return m.Role == memberRoleOwner }

// MembershipChange is what one act did.
type MembershipChange struct {
	Member uuid.UUID
	// Self: the actor acted on their own membership (a step-down).
	Self bool
	// RevokedSessions is how many of the membership's live sessions in this business a disable ended.
	RevokedSessions int
}

// MembershipDatabase is the slice of *db.DB the service needs, declared at the consumer (§7).
type MembershipDatabase interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

var _ MembershipDatabase = (*db.DB)(nil)

// membershipTrail writes the change's row inside the change's transaction (audit.Recorder.RecordTx).
type membershipTrail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
}

var _ membershipTrail = (*audit.Recorder)(nil)

// Memberships lists and changes a business's memberships.
type Memberships struct {
	data  MembershipDatabase
	trail membershipTrail
	// beforeWrite runs after the invariant was decided and before the write (tests only: it holds two
	// changes side by side so the race test can show the lock, not the timing, decides).
	beforeWrite func()
}

// NewMemberships builds the service.
func NewMemberships(data MembershipDatabase, trail *audit.Recorder) (*Memberships, error) {
	if data == nil {
		return nil, errors.New("adminauth: nil database")
	}
	if trail == nil {
		return nil, errors.New("adminauth: nil audit recorder")
	}
	return &Memberships{data: data, trail: trail}, nil
}

// List is the business's memberships, owners first, each active owner marked when it can get no
// session (RefusedOwner).
func (s *Memberships) List(ctx context.Context, tenantID uuid.UUID) ([]Member, error) {
	var out []Member
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := store.New(tx).ListAdminMembers(ctx, tenantID)
		if e != nil {
			return e
		}
		live, e := db.SignInOwners(ctx, tx, tenantID)
		if e != nil {
			return e
		}
		out = make([]Member, 0, len(rows))
		for _, r := range rows {
			m := Member{ID: r.ID, FullName: r.FullName, Role: r.Role, Status: r.Status, CreatedAt: r.CreatedAt, LastLoginAt: r.LastLoginAt}
			if r.Email != nil {
				m.Email = *r.Email
			}
			m.RefusedOwner = m.Owner() && m.Active() && !slices.Contains(live, m.ID)
			out = append(out, m)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("adminauth: list memberships: %w", err)
	}
	return out, nil
}

// Change applies act to the membership memberID of tenantID on behalf of actorID -- one transaction:
// the owners' lock, the actor's and the invariant's checks, the write, a disable's session revocation
// and the trail row. The caller (the panel) has already decided the actor may manage memberships.
func (s *Memberships) Change(ctx context.Context, tenantID, actorID, memberID uuid.UUID, act MembershipAct) (MembershipChange, error) {
	if tenantID == uuid.Nil || actorID == uuid.Nil || memberID == uuid.Nil {
		return MembershipChange{}, ErrMemberUnknown
	}
	switch act {
	case MemberDisable, MemberEnable, MemberMakeOwner, MemberMakeManager:
	default:
		return MembershipChange{}, ErrMemberAct
	}
	out := MembershipChange{Member: memberID, Self: memberID == actorID}
	err := s.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		owners, err := q.LockActiveAdminOwners(ctx, tenantID)
		if err != nil {
			return err
		}
		if !slices.Contains(owners, actorID) {
			return ErrMemberNotOwner
		}
		// The target row's lock (the one a session-giving path takes on it -- 00081) and its existence here.
		_, err = q.LockAdminMember(ctx, store.LockAdminMemberParams{TenantID: tenantID, ID: memberID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMemberUnknown
		}
		if err != nil {
			return err
		}
		if out.Self && (act == MemberDisable || act == MemberEnable) {
			return ErrMemberSelf
		}
		// The owners who can sign in, read under the owners' lock (WHO COUNTS, above). A target outside
		// that set -- a manager, a disabled row, a refused owner -- takes no such owner away.
		live, err := db.SignInOwners(ctx, tx, tenantID)
		if err != nil {
			return err
		}
		takesAnOwnerAway := act == MemberDisable || act == MemberMakeManager
		if takesAnOwnerAway && slices.Contains(live, memberID) && len(live) <= 1 {
			return ErrLastOwner
		}
		if s.beforeWrite != nil {
			s.beforeWrite()
		}
		ev := audit.Event{TenantID: tenantID, ActorID: &actorID, Target: memberID.String()}
		switch act {
		case MemberDisable:
			if _, err := q.SetAdminMemberStatus(ctx, store.SetAdminMemberStatusParams{Status: memberStatusDisable, TenantID: tenantID, ID: memberID}); err != nil {
				return unchanged(err)
			}
			// The row's own sessions in THIS business -- not the person's other businesses (K-6).
			// TouchAdminSession refuses a disabled row from now on. A session another transaction is
			// opening at this moment cannot slip past this revocation: identity_open_session and
			// identity_switch_session read the membership FOR NO KEY UPDATE (00081, M14-16b), so either
			// they read it first and this transaction waited above (LockAdminMember) until their session
			// was committed -- this later statement sees it -- or they read it after this commit and find
			// it disabled (TestMemberships_ASessionOpenedBesideADisableDoesNotOutliveIt). The legacy
			// CreateAdminSession takes the same lock (M14-07).
			revoked, err := q.RevokeAdminSessionsForAdmin(ctx, store.RevokeAdminSessionsForAdminParams{TenantID: tenantID, AdminUserID: memberID})
			if err != nil {
				return err
			}
			out.RevokedSessions = len(revoked)
			ev.Action, ev.Detail = ActionMembershipDisabled, membershipDetail{Outcome: "ok", RevokedSessions: len(revoked)}
		case MemberEnable:
			if _, err := q.SetAdminMemberStatus(ctx, store.SetAdminMemberStatusParams{Status: memberStatusActive, TenantID: tenantID, ID: memberID}); err != nil {
				return unchanged(err)
			}
			// 🔴 EVERY SESSION THE ROW HOLDS WHILE DISABLED IS REVOKED HERE, in the transaction that
			// re-enables it (it holds the row's lock). A session of a disabled row is never usable
			// (TouchAdminSession), so nothing legitimate is lost. Since 00081 no session-giving path
			// can land one beside the disable (see the disable's comment); this stays as the second
			// layer -- a session a future writer, or a hand, put on a disabled row does not become
			// usable again now. The person signs in afresh.
			revoked, err := q.RevokeAdminSessionsForAdmin(ctx, store.RevokeAdminSessionsForAdminParams{TenantID: tenantID, AdminUserID: memberID})
			if err != nil {
				return err
			}
			out.RevokedSessions = len(revoked)
			ev.Action, ev.Detail = ActionMembershipEnabled, membershipDetail{Outcome: "ok", RevokedSessions: len(revoked)}
		case MemberMakeOwner:
			if _, err := q.SetAdminMemberRole(ctx, store.SetAdminMemberRoleParams{Role: memberRoleOwner, TenantID: tenantID, ID: memberID}); err != nil {
				return unchanged(err)
			}
			ev.Action, ev.Detail = ActionMembershipPromoted, membershipDetail{Outcome: "ok", Role: memberRoleOwner}
		case MemberMakeManager:
			if _, err := q.SetAdminMemberRole(ctx, store.SetAdminMemberRoleParams{Role: memberRoleManager, TenantID: tenantID, ID: memberID}); err != nil {
				return unchanged(err)
			}
			ev.Action, ev.Detail = ActionMembershipDemoted, membershipDetail{Outcome: "ok", Role: memberRoleManager, Self: out.Self}
		}
		_, err = s.trail.RecordTx(ctx, tx, ev)
		return err
	})
	if err != nil {
		for _, known := range []error{ErrMemberUnknown, ErrMemberNotOwner, ErrMemberSelf, ErrLastOwner, ErrMemberUnchanged} {
			if errors.Is(err, known) {
				return MembershipChange{}, known
			}
		}
		return MembershipChange{}, fmt.Errorf("adminauth: change membership: %w", err)
	}
	return out, nil
}

// membershipDetail is the trail row's detail: fixed words and one session count, nothing else.
type membershipDetail struct {
	Outcome         string `json:"outcome"`
	Role            string `json:"role,omitempty"`
	Self            bool   `json:"self,omitempty"`
	RevokedSessions int    `json:"revoked_sessions,omitempty"`
}

// unchanged turns a guarded UPDATE's "no row" into ErrMemberUnchanged.
func unchanged(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMemberUnchanged
	}
	return err
}
