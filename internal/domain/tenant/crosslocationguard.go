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
// 🔴 IT RUNS UNDER THE ROWS' LOCKS AND RE-READS AFTER TAKING THEM. The target is
// already held (GetPolicyForUpdate); every row on the OTHER side of the guard -- all
// old-allow policies when an evidence rule is switched off, all evidence rules when
// an old allow is switched on, enabled or not -- is locked in id order, and the
// rulebook is read again: READ COMMITTED gives that second read a fresh snapshot, so
// two owners pressing opposite switches at once cannot each see the other's old
// state and both succeed. Two such presses can deadlock instead; PostgreSQL aborts
// one, its transaction writes nothing and the screen says "unavailable" -- the safe
// direction, counted rather than engineered away.

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

// crossLocationGuard answers whether switching target to enabled is refused, or nil.
// It runs inside SetEnabled's transaction, after target is locked and known to change.
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
	var lock []uuid.UUID
	for _, r := range rows {
		if r.PolicyID != target.ID && other(kindOf(r)) {
			lock = append(lock, r.PolicyID)
		}
	}
	if len(lock) == 0 {
		return nil, nil
	}
	sort.Slice(lock, func(i, j int) bool { return lock[i].String() < lock[j].String() })
	for _, id := range lock {
		if _, err := q.GetPolicyForUpdate(ctx, store.GetPolicyForUpdateParams{TenantID: tenantID, ID: id}); err != nil {
			return nil, fmt.Errorf("lock a rule the cross-location guard reads: %w", err)
		}
	}
	if rows, err = q.ListPolicySet(ctx, tenantID); err != nil {
		return nil, fmt.Errorf("re-read the rulebook for the cross-location guard: %w", err)
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
