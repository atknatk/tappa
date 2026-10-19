package tenant

// crosslocationguard.go -- the panel's guard for ADR 0029's old allow (user decision,
// 2026-10-19: "kapatmayı engelle").
//
// THE STATE IT GUARDS. base:cross-location-note became a note on 2026-10-18, but a
// business provisioned before that keeps its stored version 1 -- an ALLOW -- until it
// switches it off (nothing is auto-updated: policy.BaselineVersion), and a copy the
// authoring form made from it before that day is an allow too. Under that allow,
// switching one of the evidence rules off (policy.CrossLocationEvidenceSids: IP proof,
// GPS-only allowance, no-evidence review) changes the outcome at home and NOT at
// another location: the old allow still lets the tap through there (ADR 0029's
// inventory -- 1, 4 and 1 of 128 input shapes). That is backlog T125's hole, rebuilt
// by a customer's own switch.
//
// WHAT IT DOES. A switch that would put the rulebook in that state is refused:
//
//   - switching an evidence rule OFF while any enabled policy carries the old allow
//     (ErrCrossLocationFirst -- the screen says: switch the cross-location rule off
//     first; the product offers no accept step for the new version yet, so switching
//     off is the one move there is);
//   - switching a policy that carries the old allow back ON while an evidence rule is
//     off (ErrEvidenceRuleOff -- the same state, reached from the other side; without
//     it the guard is three presses deep: off the note, off the evidence rule, on the
//     note again).
//
// Switching an evidence rule ON, and switching the old allow OFF, are never refused:
// both move AWAY from the hole.
//
// ⚠️ WHAT IT DOES NOT DO. It changes no stored row: a business that already has an
// evidence rule off AND the old allow on (switched before this guard shipped; the
// main session counts production, ADR 0029's follow-up note) keeps that state until it
// switches the old allow off; the screen's announcement on the cross-location rule
// says what it changes, and this guard lets that switch through. Nor does it look at
// a tenant's own copy of an EVIDENCE rule: switching such a copy off returns that
// venue to the baseline rule, which this guard keeps on while the old allow stands.
//
// 🔴 IT RUNS UNDER THE ROWS' LOCKS, TAKEN IN ONE ORDER BEFORE THE TARGET'S. A switch
// of a guarded rule -- an evidence rule or a policy carrying the old allow -- first
// locks EVERY guarded row of the business, the target among them, in id order
// (crossLocationLocks), and only then takes the target (GetPolicyForUpdate, a no-op
// re-lock) and reads the rulebook (crossLocationGuard). READ COMMITTED gives that read
// a fresh snapshot, so two owners pressing opposite switches at once cannot each see
// the other's old state and both succeed: the second waits for the first to commit and
// is then refused by the guard, with its trail row.
//
// The FIRST version locked the target first and the other side after it, in id order.
// That was safe -- two opposite presses could not both land -- but each held one side
// and waited for the other, and PostgreSQL broke the cycle after deadlock_timeout: 15
// of 15 queued pairs and 58 of 60 racing pairs ended in 40P01, one second each, on a
// screen that said "unavailable" (measured, the 2026-10-19 low round). One lock order
// for both sides turns the cycle into a queue; the same measurement is 0 of 15 and 0 of
// 60 (TestCrossLocationGuardDB_QueuedOppositePressesNeverBothLand,
// TestCrossLocationGuardDB_RacingOppositePressesNeverBothLand).
//
// ⚠️ THE SET TO LOCK IS READ BEFORE IT IS LOCKED, AND THAT IS BOUNDED BY WHAT CAN MOVE.
// A row's side comes from its current document. The only change of document a panel
// can make is Author appending a version made from the SHIPPED cross-location rule --
// a note -- so a row can leave the old-allow side between the read and the lock, never
// join it (an over-locked row costs a wait, not a hole); baseline evidence documents
// are never rewritten here (ErrNotYours). The guard's own read is after the locks and
// decides on the rows as they are then.

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/store"
)

// guardRefusal is why crossLocationGuard said no: the sentinel and the rows that
// stand on the other side.
type guardRefusal struct {
	err      error
	reason   string
	blocking []store.ListPolicySetRow
}

// switchRefusedDetail is the trail row for a refused switch. §4.7: names and ids of
// this business's own rules, a closed reason word, two booleans -- nothing else.
type switchRefusedDetail struct {
	Name      string         `json:"name"`
	Layer     string         `json:"layer"`
	Was       bool           `json:"was_enabled"`
	Requested bool           `json:"requested_enabled"`
	Outcome   string         `json:"outcome"`
	Reason    string         `json:"reason"`
	Blocking  []blockingRule `json:"blocking"`
}

type blockingRule struct {
	PolicyID uuid.UUID `json:"policy_id"`
	Name     string    `json:"name"`
	Layer    string    `json:"layer"`
	Enabled  bool      `json:"enabled"`
}

// The closed reason words of a refused switch.
const (
	reasonOldAllowInForce = "old_cross_location_allow_in_force"
	reasonEvidenceRuleOff = "evidence_rule_off"
)

func (g *guardRefusal) detail(target store.GetPolicyForUpdateRow, requested bool) switchRefusedDetail {
	d := switchRefusedDetail{
		Name: target.Name, Layer: target.Layer, Was: target.Enabled, Requested: requested,
		Outcome: "refused", Reason: g.reason, Blocking: []blockingRule{},
	}
	for _, r := range g.blocking {
		d.Blocking = append(d.Blocking, blockingRule{PolicyID: r.PolicyID, Name: r.Name, Layer: r.Layer, Enabled: r.Enabled})
	}
	return d
}

// guardKind says which side of the guard a stored policy is on.
type guardKind struct {
	evidence bool // a managed evidence rule (policy.CrossLocationEvidenceSids)
	oldAllow bool // carries the cross-location rule's pre-2026-10-18 allow
}

func kindOf(r store.ListPolicySetRow) guardKind {
	layer := policy.Layer(r.Layer)
	doc, err := policy.ParseAndValidate(r.Document, layer, policy.DefaultLimits())
	if err != nil {
		// A document the engine cannot parse decides nothing (checkin drops it, or the
		// whole baseline with it), so it is on neither side.
		return guardKind{}
	}
	var k guardKind
	for _, st := range doc.Statements {
		if layer == policy.LayerBaseline && slices.Contains(policy.CrossLocationEvidenceSids(), st.Sid) {
			k.evidence = true
		}
		if policy.IsOldCrossLocationAllow(st) {
			k.oldAllow = true
		}
	}
	return k
}

// guarded reports whether a stored policy is on either side of the guard.
func (k guardKind) guarded() bool { return k.evidence || k.oldAllow }

// crossLocationLocks takes, in id order, the lock of every guarded policy of the
// business when the target is one of them; otherwise it takes none. It runs inside the
// switch's transaction BEFORE the target is locked, so every switch of a guarded rule
// asks for the same rows in the same order and two of them queue instead of
// deadlocking.
func crossLocationLocks(ctx context.Context, q *store.Queries, tenantID, targetID uuid.UUID) error {
	rows, err := q.ListPolicySet(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("read the rulebook for the cross-location guard: %w", err)
	}
	var lock []uuid.UUID
	targetGuarded := false
	for _, r := range rows {
		if !kindOf(r).guarded() {
			continue
		}
		lock = append(lock, r.PolicyID)
		if r.PolicyID == targetID {
			targetGuarded = true
		}
	}
	if !targetGuarded {
		return nil
	}
	sort.Slice(lock, func(i, j int) bool { return lock[i].String() < lock[j].String() })
	for _, id := range lock {
		if _, err := q.GetPolicyForUpdate(ctx, store.GetPolicyForUpdateParams{TenantID: tenantID, ID: id}); err != nil {
			return fmt.Errorf("lock a rule the cross-location guard reads: %w", err)
		}
	}
	return nil
}

// crossLocationGuard answers whether switching target to enabled is refused, or nil.
// It runs inside the switch's transaction, after crossLocationLocks and after target is
// locked and known to change, so the rulebook it reads is the one the write will join.
func crossLocationGuard(ctx context.Context, q *store.Queries, tenantID uuid.UUID,
	target store.GetPolicyForUpdateRow, enabled bool) (*guardRefusal, error) {
	rows, err := q.ListPolicySet(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("read the rulebook for the cross-location guard: %w", err)
	}
	var mine guardKind
	for _, r := range rows {
		if r.PolicyID == target.ID {
			mine = kindOf(r)
		}
	}
	// Which side the target is on decides what the other side is; a switch that moves
	// away from the hole is never guarded.
	var other func(guardKind) bool
	switch {
	case !enabled && mine.evidence:
		other = func(k guardKind) bool { return k.oldAllow }
	case enabled && mine.oldAllow:
		other = func(k guardKind) bool { return k.evidence }
	default:
		return nil, nil
	}
	g := &guardRefusal{}
	for _, r := range rows {
		if r.PolicyID == target.ID || !other(kindOf(r)) {
			continue
		}
		// An evidence rule is switched off under an ENABLED old allow; an old allow is
		// switched on while an evidence rule is DISABLED.
		if (!enabled && r.Enabled) || (enabled && !r.Enabled) {
			g.blocking = append(g.blocking, r)
		}
	}
	if len(g.blocking) == 0 {
		return nil, nil
	}
	sort.Slice(g.blocking, func(i, j int) bool {
		if g.blocking[i].Name != g.blocking[j].Name {
			return g.blocking[i].Name < g.blocking[j].Name
		}
		return g.blocking[i].PolicyID.String() < g.blocking[j].PolicyID.String()
	})
	if enabled {
		g.err, g.reason = ErrEvidenceRuleOff, reasonEvidenceRuleOff
	} else {
		g.err, g.reason = ErrCrossLocationFirst, reasonOldAllowInForce
	}
	return g, nil
}
