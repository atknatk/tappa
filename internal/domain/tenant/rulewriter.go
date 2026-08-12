package tenant

// rulewriter.go -- the WRITE side of the POLICIES section (M6-09 phase B).
//
// 🔴 EVERY METHOD HERE ASKS THE POLICY ENGINE FIRST, AND THAT IS THE POINT OF THE
// FILE. `policy:edit` is owner-only (ADR 0004 K2, guardrail
// sys:policy-edit-owner-only) and the M6-09 card carried "the panel does not
// actually enforce it" as an unpaid debt across THREE tasks -- M6-07 phase B,
// M6-08 and M6-09 phase A -- each time for the same measured reason: getting a
// decision Set meant calling internal/domain/checkin's forTenant, which
// MATERIALISES the managed baseline, so a panel request would have written rows
// into an append-only table for any tenant somebody merely looked at.
//
// That objection is answered rather than argued with. checkin.Service.StoredSet
// reads the SAME rulebook the tap path decides on and writes NOTHING, so the gate
// costs one SELECT and cannot provision anybody. See authorise for the two stages
// and for why the first one exists.
//
// 🔴 THE GATE IS HERE AND NOT IN internal/handler, WHICH IS A STRUCTURAL CHOICE.
// A gate in the handler is a gate the NEXT handler can forget; a gate inside the
// only type that can reach these statements is one a caller cannot get past,
// because every exported method below takes an Actor and refuses before it opens a
// transaction. The panel ALSO hides the controls from a manager -- both halves are
// required and neither substitutes for the other, which is the argument
// locationactions.go's mayRemove makes for the one role check that already shipped.
//
// 🔴 §4.3 -- "EDITING A RULE" IS ALWAYS A NEW ROW. There is no UPDATE statement
// for policy_versions in db/queries/policies.sql and there cannot be one: 00007
// revokes UPDATE and DELETE from tappa_app AND installs a BEFORE UPDATE OR DELETE
// trigger that binds the table owner too. So append-only is a property of the
// database here, not of the care taken in this file.
//
// 🔴 §4.6 -- A POLICY IS NEVER DELETED. 00007 revokes DELETE on `policies`; the
// off switch is `enabled=false`, which keeps the version history a record decided
// under it can still be explained by. The ONE real DELETE in this file is on
// policy_attachments, which 00007 grants deliberately: a binding is where a policy
// applies TODAY and carries no decision history (a past decision is pinned by
// transactions.policy_version_id).
//
// 🔴 §4.5 -- the tenant is never an input. It arrives on the command from a
// resolved admin session; every statement carries it explicitly beside RLS, and
// db.(*DB).WithTenant's RLS WITH CHECK refuses a row for any other tenant.
//
// ✅ AND THE BELT NOW SEES THE INSERTS TOO. The derived scan in query_test.go used
// to walk WHERE clauses only, so an `INSERT ... VALUES` was invisible to it
// (backlog T20) -- which would have meant shipping this file's three INSERTs under a
// belt that could not read them. It reads them now: it lines the column list up with
// the value list and requires tenant_id to come from @tenant_id. The behavioural
// half is still measured separately, in rulewriter_db_test.go's cross-tenant cases,
// because a query that NAMES the parameter and a row that LANDS in the right tenant
// are two different claims.
//
// 🔴 §4.7 -- nothing below can carry a secret. The commands hold uuids, a closed
// vocabulary of resource strings and a policy name; the audit details hold the
// same. No token, no coordinate, no key reference, and no document body in a log
// line.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/store"
)

// The refusals this file can produce. They are values rather than sentences so a
// handler can answer each one differently; the words a customer reads live on the
// screen, not here.
var (
	// ErrNotPermitted is the engine's answer, not this package's opinion. See
	// authorise.
	ErrNotPermitted = errors.New("tenant: the policy engine refuses this actor the policy:edit action")
	// ErrUnknownPolicy covers "no such policy" AND "another business's policy" with
	// one answer, deliberately: telling those apart across a tenant boundary is a
	// probe.
	ErrUnknownPolicy = errors.New("tenant: no such policy in this organisation")
	// ErrWouldLockOut is the change that would have taken the actor's own authority
	// away, PUT BACK. See lockoutCheck.
	ErrWouldLockOut = errors.New("tenant: that change would take away your own authority to change the rules")
	// ErrLockoutStands is the same situation with the undo ALSO failing, and it is a
	// separate value because the sentence a customer reads is the opposite one.
	//
	// 🔴 ONE SENTINEL FOR BOTH WAS A LIE ON THE RARE PATH. The screen's message for
	// ErrWouldLockOut says "so it was put back" — true when the undo worked, and
	// exactly wrong when it did not: there the change STANDS and the owner really is
	// locked out. The log line was right and the customer's line was wrong, which is
	// the worse half of the pair to get right.
	ErrLockoutStands = errors.New("tenant: that change took away your own authority to change the rules and could not be undone")
	// ErrAlreadyBound / ErrNotBound: the button was pressed for something that was
	// already true. Saying so is better than reporting a change that did not happen.
	ErrAlreadyBound = errors.New("tenant: that policy is already bound to that resource")
	ErrNotBound     = errors.New("tenant: that policy is not bound to that resource")
	// ErrBadResource rejects a scope string the resource grammar does not admit.
	ErrBadResource = errors.New("tenant: that is not a resource a policy can be bound to")
	// ErrVersionRace is SQLSTATE 23505 on policy_versions_no_key, translated. See
	// AppendVersion.
	ErrVersionRace = errors.New("tenant: the rule changed while you were reading it")
	// ErrUnknownVersion is asked for by the bounded single-version read.
	ErrUnknownVersion = errors.New("tenant: no such version of that policy")
	// ErrNameTaken refuses a SECOND policy with a name this business already uses.
	//
	// 🔴 IT IS LOSS PREVENTION RATHER THAN TIDINESS (§4.6's direction). 00007 revokes
	// DELETE, so a duplicate would be PERMANENT: both rows decide, neither shows in the
	// other's history, and no route in this product could remove either. The panel
	// produced exactly that pair for a round, because its authoring form carried no
	// policy id and every save therefore created rather than appended.
	//
	// ⚠️ THIS COMMENT SAID "`policies` HAS NO UNIQUE (tenant_id, name)" IN THE PRESENT
	// TENSE AFTER 00015 ADDED ONE. It is scoped by layer -- UNIQUE (tenant_id, layer,
	// name) -- and it is what CARRIES this guarantee; the check that raises this error
	// early is a read-then-write and collapses under concurrency (measured). See
	// db/queries/policies.sql's PolicyNameTaken and the migration's header.
	ErrNameTaken = errors.New("tenant: this organisation already has a rule with that name")
	// ErrNotYours refuses an edit to a document Tappa ships and maintains. A managed
	// document may be switched off or superseded; it is never edited in place,
	// because the tenant did not write it and a "correction" to it would be
	// indistinguishable from an upgrade Tappa shipped (policy.BaselineVersion).
	ErrNotYours = errors.New("tenant: a rule Tappa maintains cannot be rewritten here")
)

// Actor is WHO is asking, and it is one value rather than two parameters so that a
// call site cannot pass an id without a role.
//
// 🔴 BOTH FIELDS COME FROM THE RESOLVED SESSION AND NEITHER FROM A FORM. Role is
// admin_users.role, a CLOSED vocabulary at the schema level
// (CHECK (role IN ('owner','manager')), 00006), read during session resolution --
// so this costs no extra query and a caller cannot supply it. If it ever could,
// the guardrail below would be reading an attacker's own claim about themselves.
type Actor struct {
	ID   uuid.UUID
	Role string
}

// PolicyAuthority is the rulebook this package asks before it writes, declared
// HERE at the consumer (§7).
//
// ONE METHOD, AND IT IS THE ONE THAT DOES NOT WRITE. The concrete type behind it
// (checkin.Service) has a forTenant that materialises; that method is not on this
// interface and cannot be reached through it, which is what keeps "a refused
// request writes nothing" a property of the wiring rather than of the care taken
// at the call site.
type PolicyAuthority interface {
	StoredSet(ctx context.Context, tenantID uuid.UUID) policy.Set
}

// Audit actions for the rulebook. They follow the `<subject>.<past participle>`
// shape the trail already uses (location.deleted, employee.moved) and the
// `<subject>.<verb>_<refusal>` shape for the one that did NOT happen -- the split
// locationactions.go argues for, so a reader scanning `action LIKE 'policy.%'`
// does not have to look twice to tell an act from an attempt.
const (
	ActionPolicyEnabled  = "policy.enabled"
	ActionPolicyDisabled = "policy.disabled"
	ActionPolicyCreated  = "policy.created"
	ActionPolicyVersion  = "policy.version_appended"
	ActionPolicyBound    = "policy.bound"
	ActionPolicyUnbound  = "policy.unbound"
	// ActionPolicyEditRefused records an attempt the engine refused. It is written
	// for the reason location.delete_refused is: a structured log line has no tenant
	// boundary and no reader outside the operator, and the person who most needs to
	// know a manager has been trying to rewrite the rules is the OWNER.
	ActionPolicyEditRefused = "policy.edit_refused"
)

// policyScopeRowLimit bounds the scope-target read (venues + departments).
//
// IT IS ITS OWN CONSTANT rather than policyRowLimit, for the reason the two
// existing limits are two: they bound different populations. A nine-site chain has
// nine venues and could have four hundred versions of one rule; tying the numbers
// together would make a change to one silently move the other.
const policyScopeRowLimit = 400

// RuleWriter changes a business's rulebook. Every method gates on the engine
// first; nothing here is reachable without an Actor.
type RuleWriter struct {
	data  Database
	trail Trail
	// authority is the stored rulebook the gate evaluates against. It is a
	// dependency rather than a construction, so this type cannot come to hold a
	// SECOND opinion about what the rules are -- which is the failure the panel's
	// own screen exists to make visible.
	authority PolicyAuthority
	// params are the bounded guardrail windows the deciding service holds. The gate
	// builds guardrails from them, so the guardrail that refuses a manager here is
	// the same object with the same windows as the one that refuses a tap.
	params policy.Params
	log    *slog.Logger
}

// NewRuleWriter wires a RuleWriter. Every dependency is required: a nil authority
// would mean a gate that cannot ask anything, and the only safe behaviour then is
// to refuse everything -- which is a service nobody can use, discovered at run
// time instead of at start-up.
func NewRuleWriter(data Database, trail Trail, authority PolicyAuthority, params policy.Params, log *slog.Logger) (*RuleWriter, error) {
	switch {
	case data == nil:
		return nil, errors.New("tenant: nil database")
	case trail == nil:
		return nil, errors.New("tenant: nil audit recorder")
	case authority == nil:
		return nil, errors.New("tenant: nil policy authority")
	}
	if err := params.Validate(); err != nil {
		return nil, fmt.Errorf("tenant: rule writer: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	return &RuleWriter{data: data, trail: trail, authority: authority, params: params, log: log}, nil
}

// May reports whether this actor may edit this business's rulebook, WITHOUT
// writing anything and without pretending to be the gate.
//
// It exists so the screen can hide controls it would refuse. The gate is
// authorise, which every write calls for itself: hiding a button is a courtesy and
// refusing the request is the guarantee, and a screen that had only the first
// would be a lock made of CSS.
func (w *RuleWriter) May(ctx context.Context, tenantID uuid.UUID, actor Actor) bool {
	return w.authorise(ctx, tenantID, actor) == nil
}

// policyEditContext is the ONE place the authorisation question is phrased, so the
// gate and the "may I show this button" read cannot ask different questions.
//
// 🔴 THE RESOURCE IS REQUIRED AND AN EMPTY LIST WOULD FAIL CLOSED SILENTLY.
// policy.Evaluate matches a statement only if one of its resource PATTERNS matches
// one of the request's CONCRETE resources (statementSpecificity), so a Context with
// no Resources matches nothing at all -- including the shipped owner grant, whose
// pattern is "*". The owner would then be denied by the built-in default and the
// screen would say the engine refuses the owner their own rulebook. Measured: with
// Resources nil, Evaluate returns the fail-closed default for an owner.
//
// WHY THE RULEBOOK AND NOT THE POLICY. The act being authorised is "change this
// business's rules", and a CREATE has no policy id to name -- inventing one for the
// gate would make the same authority get asked two different questions depending on
// which button was pressed. One concrete resource per business is uniform, matches
// the shipped "*" grant, and leaves room for a future statement scoped to exactly
// it.
func policyEditContext(tenantID uuid.UUID, actor Actor) policy.Context {
	return policy.Context{
		Action:    policy.ActionPolicyEdit,
		Resources: []string{"policy/" + tenantID.String()},
		Keys:      map[policy.ContextKey]any{policy.CtxActorRole: actor.Role},
	}
}

// authorise is the gate. TWO STAGES, and the first one is not a duplicate of the
// second.
//
// STAGE 1 -- GUARDRAILS ALONE, NO DATABASE. policy.Evaluate walks the guardrails
// first and returns on the first match (evaluate.go step 1), so a guardrail deny is
// TERMINAL: it is the same deny under any stored set whatsoever. Asking the
// guardrails on their own therefore answers a non-owner completely, and it answers
// them without a tenant-scoped read -- which means a manager's POST costs no
// database work at all, and is refused even for a business whose rulebook cannot be
// read. The terminal property is not assumed: TestRuleWriter_AGuardrailDenyIsTerminalWhateverIsStored
// re-evaluates it against stored sets that would otherwise allow.
//
// 🔴 STAGE 1 CANNOT BE THE WHOLE GATE, AND THE ENGINE SAYS SO IN WRITING.
// sys:policy-edit-owner-only only DENIES non-owners; the OWNER's permission lives in
// the baseline document (internal/policy/baseline.go's authzDoc, whose own comment
// calls this out: "without an owner allow here the owner's policy:edit would fall to
// the fail-closed default deny and the owner could never configure anything -- a
// lockout"). So an owner MUST be answered by the stored rulebook, and a gate that
// stopped at stage 1 would lock the owner out of their own screen. That is exactly
// the mistake a naive reading of rulebook.go:defaultEffectFor would produce, since
// that call evaluates an EMPTY set on purpose (to print a default) and returns deny
// for every action including this one.
//
// STAGE 2 -- THE STORED RULEBOOK, READ WITHOUT MATERIALISING. See
// checkin.Service.StoredSet.
//
// THE SENTINEL IS DERIVED, NOT SPELLED. "a guardrail actually fired" is told from
// "nothing matched" by comparing the matched sid with the sid an EMPTY set returns
// for the same action -- the same trick rulebook.go's defaultEffectFor uses. Writing
// "default" here would be a second copy of a string internal/policy owns.
func (w *RuleWriter) authorise(ctx context.Context, tenantID uuid.UUID, actor Actor) error {
	if w.refusalFor(ctx, tenantID, actor) != nil {
		return ErrNotPermitted
	}
	return nil
}

// refusal is WHY the gate said no, in the engine's own terms. nil means allowed.
//
// 🔴 IT EXISTS BECAUSE THE TRAIL WAS WRITING A FIXED SENTENCE AND THE SENTENCE WAS
// SOMETIMES FALSE. The first version of RecordRefusal wrote
// `required_role: "owner"` and "the engine did not allow policy:edit" for every
// refusal — so an OWNER of a business whose rulebook has never been materialised got
// a permanent row saying the required role was owner while the actor's role WAS
// owner. audit_log takes no UPDATE and no DELETE (00005), so that row can never be
// corrected, and this file's own SetEnabled comment is the argument against it: a
// trail that records something that did not happen is worse than no trail, because
// it is believed.
//
// EVERY FIELD IS ASKED FOR RATHER THAN WRITTEN. The stage is which of the two
// evaluations refused; the sid is the engine's own MatchedSid; the stored count is
// what the non-materialising read actually returned. No sentence here claims a
// required role: the sid names the rule, and the rule is on the screen this section
// renders.
type refusal struct {
	// Stage is "guardrail" or "rulebook".
	Stage string
	// Sid is what the engine matched. For the rulebook stage with nothing matching it
	// is the engine's own default sentinel, which is why it is read off a Decision
	// rather than spelled here.
	Sid string
	// Stored is how many baseline + tenant policies the stored set carried. Zero is
	// the state the panel cannot repair from here (M7-03 owns provisioning), and it
	// is a different problem from "stored, and none of it grants you this".
	Stored int
}

func (w *RuleWriter) refusalFor(ctx context.Context, tenantID uuid.UUID, actor Actor) *refusal {
	if tenantID == uuid.Nil || actor.ID == uuid.Nil {
		return &refusal{Stage: "guardrail", Sid: "", Stored: 0}
	}
	in := policyEditContext(tenantID, actor)
	noPolicyAtAll := policy.Evaluate(policy.Set{}, in)
	if d := policy.Evaluate(policy.Set{Guardrails: policy.Guardrails(w.params)}, in); d.MatchedSid != noPolicyAtAll.MatchedSid {
		// A guardrail matched. Terminal, so no stored set can widen it.
		return &refusal{Stage: "guardrail", Sid: d.MatchedSid}
	}
	set := w.authority.StoredSet(ctx, tenantID)
	if d := policy.Evaluate(set, in); d.Effect != policy.EffectAllow {
		return &refusal{
			Stage:  "rulebook",
			Sid:    d.MatchedSid,
			Stored: len(set.Baseline) + len(set.Tenant),
		}
	}
	return nil
}

// sentence says, in words, what the refusal was. It is derived from the refusal's
// own fields so it cannot describe a state that did not happen.
func (r *refusal) sentence() string {
	switch {
	case r == nil:
		return ""
	case r.Stage == "guardrail":
		return "a guardrail refused this actor the policy:edit action; no stored rule can widen it"
	case r.Stored == 0:
		// 🔴 IT DOES NOT SAY "NEVER SET UP", BECAUSE THE GATE CANNOT KNOW THAT. Stored
		// counts USABLE rules, and the stored-set read is all-or-nothing: a business with
		// nine documents of which ONE cannot be parsed reports zero, exactly like a
		// business with no rows at all. This sentence claimed the second for a round, and
		// the state that reaches it was produced against real Postgres
		// (TestRuleWriterDB_AnOwnerIsRefusedWhenOneStoredDocumentCannotBeRead). Telling
		// the two apart needs a second query; the SCREEN already tells them apart, rule
		// by rule, so the trail points there instead of guessing.
		return "no stored rule is usable, so nothing grants policy:edit — either this " +
			"organisation's rulebook has never been set up, or one of its documents cannot " +
			"be read and the engine has dropped the whole layer. The state of each rule on " +
			"the policies screen tells those two apart"
	default:
		return "no stored rule grants this actor the policy:edit action"
	}
}

// RefusalDetail is the audit payload for an attempt the engine refused.
//
// 🔴 EXPLICIT EMPTIES, NO omitempty -- refusedRemovalDetail's lesson. A key that is
// absent cannot be told apart from a value that was empty, and this row exists so
// somebody can reconstruct what was attempted.
//
// §4.7: no token, no cookie, no session id. The role is the actor's own attribute
// and the act is a word from a closed list.
type RefusalDetail struct {
	Outcome string `json:"outcome"`
	// Reason is derived from the refusal (see refusal.sentence), never a constant.
	Reason string `json:"reason"`
	Act    string `json:"act"`
	// Role is the actor's own attribute, which is not a secret and is the whole
	// substance of a guardrail refusal.
	Role string `json:"role"`
	// Stage and Sid are the ENGINE's answer: which of the gate's two evaluations
	// refused, and what it matched. A reader can look the sid up on the very screen
	// this section renders.
	//
	// 🔴 THERE IS NO required_role FIELD ANY MORE. It used to be the constant "owner",
	// which produced a permanent, uncorrectable row reading role=owner /
	// required_role=owner for every owner of a business whose rulebook was never
	// materialised — a row that contradicted itself and hid the real cause.
	Stage string `json:"stage"`
	Sid   string `json:"matched_sid"`
	// StoredRules is how many stored policies the engine had to work with. 0 says
	// "never provisioned", which needs a different act from "provisioned and does not
	// grant you this".
	StoredRules int `json:"stored_rules"`
}

// RecordRefusal writes the trail row for an attempt the gate refused.
//
// 🔴 IT IS ITS OWN TRANSACTION (audit.Record semantics, not RecordTx) BECAUSE THERE
// IS NO OTHER -- nothing was written, which is the point. This is the case
// audit.Record's own doc describes: the caller who most needs a trail row is the one
// whose act did NOT happen.
//
// A FAILED TRAIL WRITE DOES NOT CHANGE THE ANSWER. The refusal already happened; the
// caller logs and carries on, exactly as refuseRemovalByRole does.
func (w *RuleWriter) RecordRefusal(ctx context.Context, tenantID uuid.UUID, actor Actor, act, target string) error {
	// 🔴 THE REASON IS RE-ASKED RATHER THAN PASSED IN, AND THE EXTRA READ IS THE POINT.
	// A caller that handed this method a reason could hand it the wrong one, and the
	// row it writes can never be corrected. Asking the gate again costs one
	// non-materialising SELECT on a path that is, by definition, rare.
	why := w.refusalFor(ctx, tenantID, actor)
	if why == nil {
		// The gate now ALLOWS this actor. Writing "refused" would be recording something
		// that is not true at the moment of writing, so it is refused instead — loudly,
		// because a caller reaching here has a bug.
		w.log.Error("a policy refusal was recorded for an actor the engine now allows",
			"tenant_id", tenantID, "actor_id", actor.ID, "act", act)
		return errors.New("tenant: refusal recorded for an actor the engine allows")
	}
	return w.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, err := w.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: tenantID,
			ActorID:  &actor.ID,
			Action:   ActionPolicyEditRefused,
			Target:   target,
			Detail: RefusalDetail{
				Outcome:     "refused",
				Reason:      why.sentence(),
				Act:         act,
				Role:        actor.Role,
				Stage:       why.Stage,
				Sid:         why.Sid,
				StoredRules: why.Stored,
			},
		})
		return err
	})
}

// --- switching a rule on and off -----------------------------------------------

// EnableCommand is one press of a rule's on/off switch.
type EnableCommand struct {
	TenantID uuid.UUID
	Actor    Actor
	PolicyID uuid.UUID
	Enabled  bool
}

// SwitchResult is what actually happened, read back from the row rather than
// assumed from the command.
type SwitchResult struct {
	PolicyID uuid.UUID
	Name     string
	Layer    string
	Was      bool
	Now      bool
}

// switchDetail is the audit payload for a toggle.
type switchDetail struct {
	Name  string `json:"name"`
	Layer string `json:"layer"`
	Was   bool   `json:"was_enabled"`
	Now   bool   `json:"now_enabled"`
}

// SetEnabled switches one policy on or off (00007: disabling is never a delete).
//
// 🔴 IT DOES NOT MATERIALISE A MISSING BASELINE, AND THAT IS A COUNTED BOUND RATHER
// THAN AN OVERSIGHT. A shipped baseline document a business has never had
// materialised has no `policies` row, so there is nothing to switch and this
// answers ErrUnknownPolicy. Provisioning is M7-03's job and the tap path's
// first-need fallback; doing it from a panel POST would put a THIRD writer of the
// managed baseline in the product and would mean the id derivation
// (checkin.baselinePolicyID) had a second copy -- and if the two ever disagreed the
// business would end up with two rows carrying the same name and one of them
// deciding. The screen says "Not set up yet" and offers no switch for such a rule.
//
// 🔴 THE ROW IS LOCKED BEFORE IT IS READ (GetPolicyForUpdate). Two presses would
// otherwise both read `enabled=true` and both append a trail row claiming they were
// the change -- a trail that records something that never happened is worse than no
// trail, because it is believed.
//
// 🔴 A CHANGE THAT WOULD TAKE AWAY THE ACTOR'S OWN AUTHORITY IS REFUSED. See
// lockoutCheck: `policies` grants no DELETE and `policy_versions` no UPDATE, so an
// owner who switched off the document that grants them policy:edit would have no
// route back through this product at all.
func (w *RuleWriter) SetEnabled(ctx context.Context, c EnableCommand) (SwitchResult, error) {
	if err := w.authorise(ctx, c.TenantID, c.Actor); err != nil {
		return SwitchResult{}, err
	}
	if c.PolicyID == uuid.Nil {
		return SwitchResult{}, ErrUnknownPolicy
	}
	var out SwitchResult
	err := w.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		before, err := q.GetPolicyForUpdate(ctx, store.GetPolicyForUpdateParams{
			TenantID: c.TenantID, ID: c.PolicyID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownPolicy
			}
			return fmt.Errorf("read policy: %w", err)
		}
		if before.Enabled == c.Enabled {
			// Nothing to do, and NOTHING IS WRITTEN -- neither the row nor a trail entry.
			// An audit row for a change that did not happen is the same defect as a
			// wrong one.
			out = SwitchResult{PolicyID: before.ID, Name: before.Name, Layer: before.Layer,
				Was: before.Enabled, Now: before.Enabled}
			return nil
		}
		row, err := q.SetPolicyEnabled(ctx, store.SetPolicyEnabledParams{
			TenantID: c.TenantID, ID: c.PolicyID, Enabled: c.Enabled,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownPolicy
			}
			return fmt.Errorf("set policy enabled: %w", err)
		}
		action := ActionPolicyEnabled
		if !row.Enabled {
			action = ActionPolicyDisabled
		}
		if _, err := w.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.Actor.ID,
			Action:   action,
			Target:   row.ID.String(),
			Detail: switchDetail{
				Name: row.Name, Layer: row.Layer, Was: before.Enabled, Now: row.Enabled,
			},
		}); err != nil {
			return err
		}
		out = SwitchResult{PolicyID: row.ID, Name: row.Name, Layer: row.Layer,
			Was: before.Enabled, Now: row.Enabled}
		return nil
	})
	if err != nil {
		return SwitchResult{}, wrapRules("switch policy", err)
	}
	// 🔴 THE LOCKOUT CHECK RUNS AFTER THE TRANSACTION COMMITTED AND UNDOES ITSELF IF
	// IT FAILS -- see lockoutCheck for why it cannot run before. Only a change that
	// switched something OFF can take an authority away, so an enable never pays for
	// it.
	if !out.Now && out.Was {
		if err := w.lockoutCheck(ctx, c.TenantID, c.Actor, func(ctx context.Context) error {
			_, undo := w.restore(ctx, c, out)
			return undo
		}); err != nil {
			return SwitchResult{}, err
		}
	}
	w.log.Info("policy switched", "policy_id", out.PolicyID, "enabled", out.Now,
		"actor_id", c.Actor.ID)
	return out, nil
}

// restore puts a policy back the way it was after a lockout check refused the
// change. It is deliberately NOT a general "undo": it re-runs the same statement
// with the previous value and writes its own trail row, so the history reads as two
// changes -- which is what happened.
func (w *RuleWriter) restore(ctx context.Context, c EnableCommand, out SwitchResult) (SwitchResult, error) {
	return w.setEnabledUngated(ctx, c.TenantID, c.Actor, out.PolicyID, out.Was)
}

// setEnabledUngated is SetEnabled's statement WITHOUT the gate and WITHOUT the
// lockout check. It exists for exactly one caller (restore), and it is unexported
// so that the gate cannot be skipped from outside this file: the actor has ALREADY
// been authorised by the call that got here.
func (w *RuleWriter) setEnabledUngated(ctx context.Context, tenantID uuid.UUID, actor Actor, policyID uuid.UUID, enabled bool) (SwitchResult, error) {
	var out SwitchResult
	err := w.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		before, err := q.GetPolicyForUpdate(ctx, store.GetPolicyForUpdateParams{TenantID: tenantID, ID: policyID})
		if err != nil {
			return fmt.Errorf("read policy: %w", err)
		}
		row, err := q.SetPolicyEnabled(ctx, store.SetPolicyEnabledParams{
			TenantID: tenantID, ID: policyID, Enabled: enabled,
		})
		if err != nil {
			return fmt.Errorf("set policy enabled: %w", err)
		}
		action := ActionPolicyEnabled
		if !row.Enabled {
			action = ActionPolicyDisabled
		}
		if _, err := w.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: tenantID, ActorID: &actor.ID, Action: action, Target: row.ID.String(),
			Detail: switchDetail{Name: row.Name, Layer: row.Layer, Was: before.Enabled, Now: row.Enabled},
		}); err != nil {
			return err
		}
		out = SwitchResult{PolicyID: row.ID, Name: row.Name, Layer: row.Layer, Was: before.Enabled, Now: row.Enabled}
		return nil
	})
	return out, err
}

// lockoutCheck re-asks the gate AFTER a change and rolls the change back if the
// actor has just removed their own authority.
//
// 🔴 IT CANNOT RUN BEFORE THE WRITE, AND THAT IS A PROPERTY OF THE ENGINE RATHER
// THAN A SHORTCUT. Deciding "would this change deny me?" in advance means
// evaluating a rulebook that does not exist yet -- i.e. simulating, which is M6-10,
// which Q22 deferred to M9-06. What CAN be done without a simulator is to make the
// change, ask the engine the same question it was asked before, and put it back if
// the answer flipped. The cost is honest: the rollback is a SECOND change with its
// own trail row, not an erasure, because nothing in this product erases.
//
// 🔴 WHY IT IS WORTH THE COST. `policies` grants no DELETE and `policy_versions` no
// UPDATE, so there is no route back through the product from a rulebook that denies
// its owner policy:edit: not a button, not a form, not a support flow that does not
// involve a DBA. This is the one change in the panel that can make the panel
// unusable, and it is one press away.
//
// ⚠️ IT IS NOT A SIMULATOR AND MUST NOT BE READ AS ONE. It answers ONE question
// about ONE actor -- "can you still get back in" -- and says nothing about what the
// change does to taps. That question needs M9-06 and the screen says so.
func (w *RuleWriter) lockoutCheck(ctx context.Context, tenantID uuid.UUID, actor Actor, undo func(context.Context) error) error {
	if err := w.authorise(ctx, tenantID, actor); err == nil {
		return nil
	}
	w.log.Warn("policy change would have locked its author out; putting it back",
		"tenant_id", tenantID, "actor_id", actor.ID)
	if err := undo(ctx); err != nil {
		// The change STANDS and the actor is now locked out. It is a different sentinel
		// from the one above precisely because the screen must not say "it was put
		// back": that is the one message that would send somebody away believing the
		// opposite of what happened.
		w.log.Error("could not undo a policy change that locked its author out",
			"tenant_id", tenantID, "actor_id", actor.ID, "err", err)
		return fmt.Errorf("%w: %w", ErrLockoutStands, err)
	}
	return ErrWouldLockOut
}

// --- binding a policy to a scope ------------------------------------------------

// BindCommand is one press of "apply this rule here" / "stop applying it here".
type BindCommand struct {
	TenantID uuid.UUID
	Actor    Actor
	PolicyID uuid.UUID
	Resource string
}

// bindDetail is the audit payload for a binding change.
type bindDetail struct {
	Name     string `json:"name"`
	Layer    string `json:"layer"`
	Resource string `json:"resource"`
}

// Bind adds one scope binding.
//
// ⚠️ A BINDING DOES NOT DECIDE ANYTHING TODAY, AND THIS METHOD DOES NOT PRETEND
// OTHERWISE. policy.Evaluate scopes a statement by the Resource patterns INSIDE its
// document, never by these rows (db/queries/policies.sql says so at
// EnsurePolicyAttachment and ListPolicyAttachments). What they record is where a
// policy was BOUND, which is what provisioning and the panel read; the screen prints
// both and says which one the engine reads. Writing them anyway is the same decision
// the tap path made: a materialised policy with no attachment row is a
// half-provisioned policy.
func (w *RuleWriter) Bind(ctx context.Context, c BindCommand) error {
	return w.rebind(ctx, c, true)
}

// Unbind removes one scope binding -- a real DELETE, which 00007 grants on this
// table and on no other. See db/queries/policies.sql for why that is not a §4.6
// violation.
func (w *RuleWriter) Unbind(ctx context.Context, c BindCommand) error {
	return w.rebind(ctx, c, false)
}

func (w *RuleWriter) rebind(ctx context.Context, c BindCommand, add bool) error {
	if err := w.authorise(ctx, c.TenantID, c.Actor); err != nil {
		return err
	}
	if c.PolicyID == uuid.Nil {
		return ErrUnknownPolicy
	}
	if !ValidResource(c.Resource) {
		return ErrBadResource
	}
	err := w.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		// THE PARENT IS READ AND LOCKED FIRST, for two reasons that both matter: it is
		// how a policy belonging to ANOTHER business becomes ErrUnknownPolicy rather
		// than a foreign-key error, and it is what the trail row's name and layer come
		// from -- an audit entry that says only "policy <uuid>" is one nobody can read.
		parent, err := q.GetPolicyForUpdate(ctx, store.GetPolicyForUpdateParams{
			TenantID: c.TenantID, ID: c.PolicyID,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownPolicy
			}
			return fmt.Errorf("read policy: %w", err)
		}
		action := ActionPolicyBound
		if add {
			if _, err := q.AttachPolicyResource(ctx, store.AttachPolicyResourceParams{
				TenantID: c.TenantID, PolicyID: c.PolicyID, Resource: c.Resource,
			}); err != nil {
				if isUniqueViolation(err) {
					return ErrAlreadyBound
				}
				return fmt.Errorf("attach policy: %w", err)
			}
		} else {
			action = ActionPolicyUnbound
			if _, err := q.DetachPolicyResource(ctx, store.DetachPolicyResourceParams{
				TenantID: c.TenantID, PolicyID: c.PolicyID, Resource: c.Resource,
			}); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrNotBound
				}
				return fmt.Errorf("detach policy: %w", err)
			}
		}
		if _, err := w.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.Actor.ID,
			Action:   action,
			Target:   c.PolicyID.String(),
			Detail:   bindDetail{Name: parent.Name, Layer: parent.Layer, Resource: c.Resource},
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return wrapRules("bind policy", err)
	}
	return nil
}

// ValidResource reports whether a string is a resource a policy can be bound to.
//
// 🔴 THE GRAMMAR IS THE ENGINE'S, NOT A SECOND ONE. policy.Evaluate's patternMatch
// splits a resource on the FIRST "/" into a type and an id, and "*" is the
// tenant-wide pattern; 00007 deliberately puts no CHECK on the column so the
// vocabulary can grow without a migration. This admits exactly the three types the
// product has rows for plus "*", and requires the id to be a uuid -- because a
// binding to `location/rusty bar` is a binding to nothing, silently, forever.
//
// ⚠️ IT DOES NOT CHECK THAT THE ID EXISTS. A venue removed after a policy was bound
// to it leaves a binding that names nothing, and 00007 has no FK for it (the column
// is free-form text on purpose).
//
// 🔴 AND THE "Bound to" LIST PRINTS THE RAW RESOURCE STRING RATHER THAN A NAME — this
// comment used to claim the opposite, which was simply false about the shipped
// template. The behaviour is deliberate now that it has been looked at: resolving an
// arbitrary id to a name is an EXISTENCE ORACLE, and the ids in this column are
// free-form text a caller can put anything into. Printing `location/<uuid>` answers
// nothing about whether that uuid names a row in anybody's business. The UNBIND
// control does resolve, because its options come from this tenant's own list.
func ValidResource(s string) bool {
	if s == "*" {
		return true
	}
	kind, id, ok := strings.Cut(s, "/")
	if !ok {
		return false
	}
	switch kind {
	case "location", "department", "employee":
	default:
		return false
	}
	return uuid.Validate(id) == nil
}

// --- authoring a policy of your own ----------------------------------------------

// AuthorCommand asks for a customer-authored policy: a COPY of one shipped
// baseline document, scoped to the venues the customer chooses.
//
// 🔴 THE DOCUMENT IS GENERATED, NEVER POSTED, AND THAT IS THE WHOLE SAFETY
// ARGUMENT OF THE v1 EDITOR (Q22: form only; the raw-JSON editor is M9-07). What a
// customer supplies is a name, a shipped document to base it on and a set of
// venues; the statements come from policy.Baseline() bytes this binary already
// ships and has already validated. So a tenant document cannot name policy:edit,
// cannot carry an effect the form does not offer, and cannot be a shape the parser
// will later refuse -- three failure modes a free-text editor would have to defend
// against one at a time.
type AuthorCommand struct {
	TenantID uuid.UUID
	Actor    Actor
	// PolicyID names an EXISTING customer policy to append a new version to, or
	// uuid.Nil to create one.
	PolicyID uuid.UUID
	// Name is the customer's own label. It is bounded by the caller (the handler
	// cleans and truncates it) and by policy.DefaultLimits at validation.
	Name string
	// BasedOn is the NAME of a shipped baseline document (policy.Baseline()). It is
	// looked up rather than trusted: an unknown name is refused.
	BasedOn string
	// Venues are the location ids the copy applies at. Empty means "everywhere",
	// which is what the shipped document already says.
	Venues []uuid.UUID
}

// AuthoredRule is what was written.
type AuthoredRule struct {
	PolicyID  uuid.UUID
	Name      string
	VersionNo int
	Created   bool
}

// versionDetail is the audit payload for an appended version.
//
// IT CARRIES THE BYTE COUNT AND NOT THE DOCUMENT. The trail is read by people, and
// a jsonb blob in a detail column is neither readable nor bounded; the version
// number names the row that holds the document, and policy_versions cannot be
// rewritten, so the pointer is as good as the copy.
type versionDetail struct {
	Name      string   `json:"name"`
	BasedOn   string   `json:"based_on"`
	VersionNo int      `json:"version_no"`
	Bytes     int      `json:"document_bytes"`
	Resources []string `json:"resources"`
}

// Author creates a customer policy or appends a NEW VERSION of one.
//
// 🔴 THERE IS NO EDIT-IN-PLACE PATH AND THERE CANNOT BE ONE (§4.3). A transaction
// decided by a STORED rule pins the policy_version_id that decided it, so rewriting a
// version would rewrite why a past, immutable record was flagged. (It is "a stored
// rule" rather than "every transaction": migration 00008's
// transactions_policy_decision_consistent REQUIRES policy_version_id to be NULL when a
// guardrail or the built-in default decided, because both are code rather than rows —
// §5's first five lines are guardrail decisions.) 00007 makes that
// structural (REVOKE UPDATE/DELETE plus a trigger that binds the table owner), and
// this method's only statement against policy_versions is an INSERT.
//
// 🔴 A MANAGED DOCUMENT IS NEVER REWRITTEN HERE. Appending a version to a baseline
// policy would put a customer's document under a name Tappa maintains, and the next
// baseline upgrade (M7-03) could not tell the two apart. Switching it off and
// writing your own is the supported move, which is what the screen offers.
func (w *RuleWriter) Author(ctx context.Context, c AuthorCommand) (AuthoredRule, error) {
	if err := w.authorise(ctx, c.TenantID, c.Actor); err != nil {
		return AuthoredRule{}, err
	}
	name := strings.TrimSpace(c.Name)
	if name == "" {
		return AuthoredRule{}, fmt.Errorf("tenant: a rule needs a name: %w", ErrBadResource)
	}
	doc, err := copyOfShipped(c.BasedOn, name, c.Venues)
	if err != nil {
		return AuthoredRule{}, err
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return AuthoredRule{}, fmt.Errorf("tenant: render policy document: %w", err)
	}
	// 🔴 IT IS VALIDATED BEFORE IT IS STORED, WITH THE SAME PARSER THE TAP PATH USES.
	// 00007 keeps `document` free-form on purpose (forward compatibility), so the
	// database will accept anything well-formed -- and a document the engine cannot
	// parse is not inert, it takes the WHOLE policy layer down for that business
	// (checkin.assembleBaseline reports it missing and the fallback is guardrails
	// alone). Writing one from the panel would be the panel breaking the product.
	if _, err := policy.ParseAndValidate(body, policy.LayerTenant, policy.DefaultLimits()); err != nil {
		return AuthoredRule{}, fmt.Errorf("tenant: the generated document is not valid: %w", err)
	}

	var out AuthoredRule
	err = w.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		policyID := c.PolicyID
		if policyID == uuid.Nil {
			// 🔴 THE NAME IS CHECKED BEFORE A ROW IS CREATED, AND ONLY ON CREATE. Appending
			// a version to an existing policy keeps its name, so the check would refuse
			// every legitimate edit. On CREATE it is the last chance: after the INSERT the
			// duplicate cannot be removed by anything this product can do.
			taken, err := q.PolicyNameTaken(ctx, store.PolicyNameTakenParams{
				TenantID: c.TenantID, Name: name,
			})
			if err != nil {
				return fmt.Errorf("check policy name: %w", err)
			}
			if taken {
				return ErrNameTaken
			}
			row, err := q.CreateTenantPolicy(ctx, store.CreateTenantPolicyParams{
				TenantID: c.TenantID, Name: name, Enabled: true, CreatedBy: &c.Actor.ID,
			})
			if err != nil {
				// 🔴 THE CHECK ABOVE IS A LOOK AND THIS IS THE LOCK. PolicyNameTaken is a
				// read-then-write, so two concurrent saves can both read "not taken";
				// measured, 16 racers produced 16 rows before migration 00015 existed. What
				// refuses them now is UNIQUE (tenant_id, layer, name), and its 23505 has to
				// arrive as the SAME refusal the early check produces — otherwise the loser of
				// a race is told the panel is broken while the winner is told nothing happened.
				if isUniqueViolation(err) {
					return ErrNameTaken
				}
				return fmt.Errorf("create policy: %w", err)
			}
			policyID, out.Created = row.ID, true
			if _, err := w.trail.RecordTx(ctx, tx, audit.Event{
				TenantID: c.TenantID, ActorID: &c.Actor.ID, Action: ActionPolicyCreated,
				Target: policyID.String(),
				Detail: switchDetail{Name: row.Name, Layer: row.Layer, Was: false, Now: row.Enabled},
			}); err != nil {
				return err
			}
		} else {
			parent, err := q.GetPolicyForUpdate(ctx, store.GetPolicyForUpdateParams{
				TenantID: c.TenantID, ID: policyID,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrUnknownPolicy
				}
				return fmt.Errorf("read policy: %w", err)
			}
			if policy.Layer(parent.Layer) != policy.LayerTenant {
				return ErrNotYours
			}
			// 🔴 THE NAME IS WRITTEN, NOT DROPPED. The form requires it and the
			// confirmation prints it; appending only the version left the row's name as it
			// was, so the screen, the picker and the version page all went on showing the
			// old one — the exact failure this file's own confirmation contract names ("a
			// field the confirmation does not bind is a field the screen can be wrong
			// about"), with the field bound and then discarded.
			//
			// Only when it CHANGED: a rename is an UPDATE and an UPDATE that changes
			// nothing is still a row-level lock somebody else may be waiting on.
			if parent.Name != name {
				if _, err := q.RenameTenantPolicy(ctx, store.RenameTenantPolicyParams{
					TenantID: c.TenantID, ID: policyID, Name: name,
				}); err != nil {
					// The unique index protects a rename exactly as it protects a create, and
					// the customer gets the same sentence for the same reason.
					if isUniqueViolation(err) {
						return ErrNameTaken
					}
					return fmt.Errorf("rename policy: %w", err)
				}
			}
		}
		next, err := q.NextPolicyVersionNo(ctx, store.NextPolicyVersionNoParams{
			TenantID: c.TenantID, PolicyID: policyID,
		})
		if err != nil {
			return fmt.Errorf("next version number: %w", err)
		}
		version, err := q.AppendPolicyVersion(ctx, store.AppendPolicyVersionParams{
			TenantID: c.TenantID, PolicyID: policyID, VersionNo: next,
			Document: body, CreatedBy: &c.Actor.ID,
		})
		if err != nil {
			// 🔴 THE RACE 00007 LEAVES OPEN, TRANSLATED RATHER THAN LEAKED. version_no is
			// assigned by the application against UNIQUE (tenant_id, policy_id,
			// version_no), so two saves that read the same MAX both try N+1 and one gets
			// 23505. Fail-closed is right -- the loser's document must not land on top of
			// the winner's -- but a 500 would tell an owner the panel is broken when in
			// fact somebody else saved first.
			if isUniqueViolation(err) {
				return ErrVersionRace
			}
			return fmt.Errorf("append version: %w", err)
		}
		if _, err := w.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID, ActorID: &c.Actor.ID, Action: ActionPolicyVersion,
			Target: policyID.String(),
			Detail: versionDetail{
				Name: name, BasedOn: c.BasedOn, VersionNo: int(version.VersionNo),
				Bytes: len(body), Resources: resourcesOf(doc),
			},
		}); err != nil {
			return err
		}
		out.PolicyID, out.Name, out.VersionNo = policyID, name, int(version.VersionNo)
		return nil
	})
	if err != nil {
		return AuthoredRule{}, wrapRules("author policy", err)
	}
	w.log.Info("policy version appended", "policy_id", out.PolicyID,
		"version_no", out.VersionNo, "actor_id", c.Actor.ID)
	return out, nil
}

// AuthorableRules are the shipped documents a customer may base their own on: the
// TAP-evidence ones and nothing else.
//
// 🔴 THE AUTHORIZATION DOCUMENT IS EXCLUDED, AND EXCLUDING IT IS WHAT MAKES THE
// LOCKOUT UNREACHABLE FROM THIS FORM. policy.Baseline()'s last document is the
// role-based panel grant; a copy of it, scoped to a venue the owner does not tap
// at, would be a statement about policy:edit that the customer generated by
// pressing buttons. The filter is DERIVED rather than a name list: a document is
// authorable only if none of its statements mentions an action whose engine default
// is deny -- i.e. only the tap-recording rules, which is exactly ADR 0004 §3's own
// split and the same derivation the read side uses for its two authority groups.
func AuthorableRules() []policy.BaselineDoc {
	var out []policy.BaselineDoc
	for _, doc := range policy.Baseline() {
		if authorable(doc) {
			out = append(out, doc)
		}
	}
	return out
}

func authorable(doc policy.BaselineDoc) bool {
	for _, st := range doc.Document.Statements {
		for _, a := range st.Action {
			if policy.Evaluate(policy.Set{}, policy.Context{Action: a}).Effect == policy.EffectDeny {
				return false
			}
		}
	}
	return len(doc.Document.Statements) > 0
}

// copyOfShipped builds the customer's document from a shipped one.
//
// THE STATEMENTS ARE COPIED WHOLE -- effect, action, condition and reason -- and the
// ONLY thing that changes is the resource list and the sid, which is namespaced
// into the customer's own space so a docket can never report a `base:` sid for a
// document Tappa did not write. The version stamp is the shipped one, so the
// document says which release it was copied from.
func copyOfShipped(basedOn, name string, venues []uuid.UUID) (policy.Document, error) {
	var src policy.BaselineDoc
	for _, doc := range AuthorableRules() {
		if doc.Name == basedOn {
			src = doc
			break
		}
	}
	if src.Name == "" {
		return policy.Document{}, fmt.Errorf("tenant: %q is not a rule you can base your own on: %w",
			basedOn, ErrBadResource)
	}
	resources := []string{"location/*"}
	if len(venues) > 0 {
		resources = resources[:0]
		for _, v := range venues {
			if v == uuid.Nil {
				return policy.Document{}, ErrBadResource
			}
			resources = append(resources, "location/"+v.String())
		}
	}
	out := policy.Document{Version: src.Document.Version, Name: name}
	for _, st := range src.Document.Statements {
		copied := st
		copied.Sid = tenantSid(st.Sid)
		copied.Resource = append([]string(nil), resources...)
		out.Statements = append(out.Statements, copied)
	}
	return out, nil
}

// tenantSid moves a shipped sid into the customer's namespace.
//
// 🔴 A `base:` SID ON A CUSTOMER DOCUMENT WOULD BE A LIE ON EVERY DOCKET IT
// DECIDED. A record carries matched_sid so a manager can find the rule that
// produced it; if a tenant copy kept the shipped sid, the manager would look it up
// in the Tappa baseline and read a rule with a different scope. validate.go's sid
// grammar is what bounds the shape of the replacement.
func tenantSid(sid string) string {
	if _, rest, ok := strings.Cut(sid, ":"); ok {
		return "own:" + rest
	}
	return "own:" + sid
}

func resourcesOf(doc policy.Document) []string {
	// EXPLICITLY EMPTY RATHER THAN nil: `[]` in the trail reads as "it was bound to
	// nothing", `null` reads as "not recorded" (deletedDetail's lesson).
	out := []string{}
	seen := map[string]bool{}
	for _, st := range doc.Statements {
		for _, r := range st.Resource {
			if !seen[r] {
				seen[r] = true
				out = append(out, r)
			}
		}
	}
	return out
}

// --- reading one version's body --------------------------------------------------

// VersionBody is one stored version, rendered the way the section renders a rule.
type VersionBody struct {
	PolicyID   uuid.UUID
	PolicyName string
	Layer      string
	Enabled    bool
	VersionNo  int
	At         time.Time
	Author     string
	Bytes      int
	Statements []RuleStatement
	// Problem explains a body this release cannot parse, in the parser's own words.
	// An OLD version that no longer parses is not an error to hide: it is what the
	// records decided under it were judged by, and the page says so.
	Problem string
	// Current is whether this is the version the engine would use.
	Current bool
}

// Version reads ONE stored version's body -- the bounded route phase A said it did
// not have, and said so on the screen.
//
// 🔴 IT IS A READ ON THE WRITER'S SIBLING RATHER THAN ON THE WRITER: it lives on
// Rulebook (the reader) so the write interface stays "one method per act". The
// bound is the shape of the query rather than a LIMIT: one version is at most
// DefaultMaxDocumentBytes, where a PAGE of versions is bounded only by a row count
// that says nothing about bytes (ListPolicyVersions' own argument).
func (r *Rulebook) Version(ctx context.Context, tenantID, policyID uuid.UUID, versionNo int) (VersionBody, error) {
	if tenantID == uuid.Nil || policyID == uuid.Nil || versionNo < 1 {
		return VersionBody{}, ErrUnknownVersion
	}
	var (
		row     store.GetPolicyVersionDocumentRow
		highest int32
		zone    *time.Location
	)
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		var e error
		if row, e = q.GetPolicyVersionDocument(ctx, store.GetPolicyVersionDocumentParams{
			TenantID: tenantID, PolicyID: policyID, VersionNo: int32(versionNo),
		}); e != nil {
			if errors.Is(e, pgx.ErrNoRows) {
				return ErrUnknownVersion
			}
			return fmt.Errorf("read policy version: %w", e)
		}
		// "IS THIS THE ONE DECIDING" IS ANSWERED BY THE SAME QUERY THE ENGINE USES, not
		// by comparing timestamps: ListPolicySet's DISTINCT ON takes the highest
		// version_no, so that is what "current" has to mean here too.
		set, e := q.ListPolicySet(ctx, tenantID)
		if e != nil {
			return fmt.Errorf("list policy set: %w", e)
		}
		for _, s := range set {
			if s.PolicyID == policyID {
				highest = s.VersionNo
			}
		}
		clock, e := q.GetTenantClock(ctx, tenantID)
		if e != nil {
			return fmt.Errorf("read tenant clock: %w", e)
		}
		zone = r.loadZone(clock.Timezone)
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrUnknownVersion) {
			return VersionBody{}, ErrUnknownVersion
		}
		return VersionBody{}, fmt.Errorf("tenant: policy version: %w", err)
	}
	out := VersionBody{
		PolicyID: policyID, PolicyName: row.PolicyName, Layer: row.Layer,
		Enabled: row.Enabled, VersionNo: int(row.VersionNo),
		At:     row.CreatedAt.In(zone),
		Author: authorOfVersion(row.CreatedBy, row.AuthorName),
		Bytes:  int(row.DocumentBytes), Current: row.VersionNo == highest,
	}
	layer := policy.LayerTenant
	if policy.Layer(row.Layer) == policy.LayerBaseline {
		layer = policy.LayerBaseline
	}
	doc, err := policy.ParseAndValidate(row.Document, layer, policy.DefaultLimits())
	if err != nil {
		out.Problem = err.Error()
		return out, nil
	}
	out.Statements = statementsOf(doc)
	return out, nil
}

// authorOfVersion is authorOf over the single-version row. The three outcomes are
// the same three ListPolicyVersions documents, and an id that resolves to nobody is
// NAMED as unresolvable rather than left blank -- "nobody decided this" is a
// different claim from "we cannot tell you who did".
func authorOfVersion(createdBy *uuid.UUID, name *string) string {
	if createdBy == nil {
		return AuthorSystem
	}
	if name == nil || strings.TrimSpace(*name) == "" {
		return AuthorUnresolved
	}
	return *name
}

// --- shared plumbing --------------------------------------------------------------

// wrapRules keeps a refusal a refusal and wraps everything else.
func wrapRules(op string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotPermitted), errors.Is(err, ErrUnknownPolicy),
		errors.Is(err, ErrAlreadyBound), errors.Is(err, ErrNotBound),
		errors.Is(err, ErrBadResource), errors.Is(err, ErrVersionRace),
		errors.Is(err, ErrNotYours), errors.Is(err, ErrWouldLockOut), errors.Is(err, ErrNameTaken),
		errors.Is(err, ErrLockoutStands):
		return err
	default:
		return fmt.Errorf("tenant: %s: %w", op, err)
	}
}

// isUniqueViolation reports whether Postgres refused the statement because a
// unique index already held the row (SQLSTATE 23505).
//
// IT MATCHES THE CODE, NOT THE MESSAGE -- isForeignKeyViolation's argument: an
// error STRING is a locale- and version-dependent sentence.
func isUniqueViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}
