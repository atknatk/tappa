package tenant

// rulebook.go -- the READ side of the POLICIES section (M6-09 phase A).
//
// 🔴 IT WRITES NOTHING, AND THAT IS THE ONE PROPERTY THIS FILE IS FOR. The other
// assembler of a tenant's policy layer -- internal/domain/checkin's policySets --
// MATERIALISES the managed baseline when it finds it missing, because a tap that
// cannot name a policy_versions row cannot be recorded at all (§4.6, and the
// measurement is in that file). A PANEL request has no such need and must not
// acquire one: rendering a screen would then INSERT a policy, a version and an
// attachment for every shipped baseline document into any tenant a curious admin
// looked at, and
// policy_versions is append-only, so those rows could never be taken back. So
// this file reads and only reads. TestRulebook_IssuesOnlySelects walks the store
// calls it names and reads each one's SQL;
// TestRulebookDB_RendersAVirginTenantWithoutWritingARow counts the three tables
// before and after a real read, with a positive control for the counter.
//
// 🔴 THE STATES OF A SHIPPED BASELINE DOCUMENT ARE TOLD APART, because several of
// them are the same picture from a distance and they call for different acts. (This
// paragraph said "THE FOUR" while listing five, and the constant block below said
// "five" while declaring six -- two counts of a set the code owns, both wrong, in
// one file. There is no count here now; the list is the list.)
//
//	StateActive          stored, switched on, its document parses -> it decides.
//	StateOff             stored, switched OFF by the tenant. A deliberate opt-out
//	                     (00007: disabling is never a delete), NOT an absence.
//	StateNotProvisioned  no policies row at all. M7-03's job, done today by the
//	                     first tap. A tenant in this state runs on guardrails plus
//	                     whatever the tap path materialises when somebody taps.
//	StateNoDocument      a policies row with NO policy_versions row -- a container
//	                     that decides nothing. Measured 2026-08-11 on the
//	                     development database: 2005 such rows exist.
//	StateUnreadable      stored, SWITCHED ON, and the document does not parse or
//	                     validate. The tap path treats this as missing and falls
//	                     back; the panel must not print it as if it were running.
//	                     Switched-on is part of the definition, not a detail --
//	                     see storedRule for what reading it the other way cost.
//	StateUnknown         not a stored state at all: the version read hit its cap,
//	                     so this reader cannot tell two of the above apart and
//	                     says so instead of picking the comfortable one.
//
// ListPolicySet's own comment says WHY the first two must not be conflated (a
// WHERE enabled would turn an opt-out into a permanent re-provisioning loop); the
// screen needs the same distinction for a different reason -- "you turned this
// off" and "this was never set up" are answered by different buttons.
//
// 🔴 §4.5 -- the tenant is never an input. Screen takes it from the caller, which
// takes it from a resolved admin session, and all three reads bind tenant_id
// explicitly beside RLS. §4.5's second defence over these queries is asserted by
// query_test.go in this package, which is the reason the file lives here rather
// than in a new one (see that file's header for what a fourth copy would cost).
//
// 🔴 §4.7 -- there is no field on any type below that a secret could travel in.
// The three queries select no token hash, no coordinate, no key reference and no
// email; a policy document is authored by the customer and carries operators,
// context keys and comparison values, none of which is a secret (validate.go
// makes the same argument about its error messages). The one identity that
// appears -- a version's author -- is a name this tenant already owns, resolved
// through RLS, and an id that does not resolve is reported as unresolvable rather
// than printed.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/store"
)

// policyRowLimit bounds the version-history read, and attachmentRowLimit the
// scope read.
//
// 🔴 THE CEILING THEY DEFEND AGAINST IS THE DECLARED ONE, NOT TODAY'S DATA. What a
// tenant may legally accumulate is internal/policy's DefaultMaxDocumentsPerTenant x
// DefaultMaxVersionsPerPolicy -- four orders of magnitude above anything in the
// development database today (measured 2026-08-11, `SELECT max(c) FROM (SELECT
// tenant_id, count(*) c FROM policy_versions GROUP BY 1) s` -> 10, and the same
// figure for policies; an earlier version of this line said "single figures" and
// was wrong by one on the day it was written) -- and policy_versions is
// append-only, so the gap only ever narrows. That is exactly why an unbounded read
// would look fine for a long time and then not. The two limits are named rather
// than quoted: they are constants the engine owns, and a copy of their values here
// would be one more thing to keep in step.
//
// THEY ARE NOT ONE CONSTANT because they bound different things: a tenant has one
// version row per edit and one attachment row per (policy, resource) pair, and a
// policy scoped to every branch of a nine-site chain has nine of the latter and
// none of the former. Tying them together would make a change to one silently
// move the other, which is the shape ledger.RosterPageSize records.
const (
	policyRowLimit     = 400
	attachmentRowLimit = 400
)

// versionsShownPerPolicy is how many versions of ONE policy the screen carries.
//
// The history is a strip under each rule rather than a page of its own, and a
// rule with two hundred versions would bury the rules. The newest are kept
// because the newest is the one deciding; the count is always reported, so a
// shortened strip says so rather than implying the rest do not exist.
const versionsShownPerPolicy = 5

// RuleState is what the tenant's stored layer says about one policy. See the file
// header for why these are told apart rather than collapsed into "on" and "off".
type RuleState string

const (
	StateActive         RuleState = "active"
	StateOff            RuleState = "off"
	StateNotProvisioned RuleState = "not-provisioned"
	StateNoDocument     RuleState = "no-document"
	StateUnreadable     RuleState = "unreadable"
	// StateUnknown is what a rule reports when the version read hit its cap and the
	// row that would have distinguished "never set up" from "set up with no rules"
	// may have been truncated. It is never a stored state; it is the reader
	// admitting it cannot tell. See the Capped branch in assemble.
	StateUnknown RuleState = "unknown"
)

// Guardrail is one immutable system rule as the screen shows it.
//
// 🔴 THERE IS NO FIELD HERE A SWITCH COULD BE RENDERED FROM, AND THAT IS THE
// GUARANTEE RATHER THAN A CONVENTION. The M6-09 card's trap is drawing a
// guardrail as a control that happens to be disabled: the customer tries it,
// cannot move it, and stops believing the rest of the screen. A comment asking
// the next author not to add a toggle is worth nothing -- a struct with no
// Enabled, no Href and no On is worth something, because a template cannot render
// what the model does not carry. The same discipline ledger.Person uses to keep a
// token off a page (roster.go). TestGuardrail_CarriesNoSwitch is the mechanical
// half: it reflects over this type and refuses a field whose name or type could
// become one.
type Guardrail struct {
	// Order is the guardrail's 1-based position in policy.Guardrails() -- DERIVED
	// from the shipped slice, never typed out. §5 says first match wins, so the
	// order is a security boundary (ADR 0004 §5) and a hand-written copy of it on
	// this screen would be a second representation of the one thing that must not
	// drift. ADR 0007 moved four of these positions once already.
	Order int
	// Sid is the guardrail's identifier, the same string a record's matched_sid
	// carries -- which is what lets a manager holding a REJECTED docket find the
	// rule that produced it.
	Sid string
	// Effect is what it does when it matches (deny, redirect, ignore).
	Effect string
	// Reason is the sentence the employee sees, taken from the guardrail itself.
	Reason string
	// Rule is why it cannot be switched off, naming the red line it enforces.
	Rule string
	// Bounded is the tunable window this guardrail compares against, or nil for the
	// ones that have none. "Cannot be switched off but MAY be tuned within a
	// range" is ADR 0004 §11's pattern, and a screen that showed only the
	// unswitchable half would make Tappa look less configurable than it is.
	Bounded *BoundedParam
}

// BoundedParam is one tunable guardrail window: what it is set to now, and the
// range it may move inside (ADR 0004 §11).
//
// The CURRENT value is read off the deciding service (checkin.Service.Params), not
// rebuilt from configuration here -- see Rulebook's doc for why a second
// construction is the one thing this must not do.
type BoundedParam struct {
	Label   string
	Current string
	Min     string
	Max     string
}

// RuleCondition is one predicate of a statement, flattened for display:
// operator, context key, value.
type RuleCondition struct {
	Operator string
	Key      string
	Value    string
}

// RuleStatement is one statement of a policy document rendered in form terms
// rather than as JSON.
//
// ⚠️ WHAT THIS SHAPE CANNOT SHOW, stated because hiding it would be worse than
// the gap. policy_versions.document is jsonb and FREE-FORM at the database layer
// (00007, deliberately, for forward-version compatibility), so a stored document
// may hold a shape this binary's parser does not admit. Such a document is not
// rendered at all: it becomes StateUnreadable with the parser's own complaint
// attached, because a partial rendering of a rule is worse than none -- it would
// describe rules that are not the rules. A document that DOES parse is shown
// completely: Parse decodes strictly (DisallowUnknownFields), so every field that
// survived is one of the five below plus the document's version and name, which
// the Rule carries.
type RuleStatement struct {
	Sid        string
	Effect     string
	Actions    []string
	Resources  []string
	Conditions []RuleCondition
	Reason     string
}

// RuleVersion is one row of a policy's history: which version, when, by whom.
type RuleVersion struct {
	No int
	At time.Time
	// Author is the label the screen prints. See authorOf for the three cases and
	// why an unresolvable id is named as unresolvable rather than printed.
	Author string
	// Bytes is the stored document's size. It is the honest stand-in for "what
	// changed" until an older version's body can be read (M6-09 phase B): it is
	// enough to see that a version is a different document from its predecessor.
	Bytes int
	// Current marks the version the engine would use -- the highest version_no,
	// which is what ListPolicySet's DISTINCT ON picks.
	Current bool
}

// Rule is one policy as the screen shows it: what it is, whether it is running,
// what it says, where it is bound and how it got here.
type Rule struct {
	// PolicyID is the stored row's id, or the zero uuid when nothing is stored.
	PolicyID uuid.UUID
	Name     string
	// Managed is true for a document Tappa ships and maintains (the baseline) and
	// false for one the customer wrote. It decides which controls phase B offers:
	// a managed document can be switched off or superseded, never edited in place.
	Managed bool
	State   RuleState
	// VersionNo is the stored version the engine would use, or 0 when none is.
	VersionNo int
	// StoredStamp is the version stamp INSIDE the stored document and
	// ShippedStamp the one this binary ships. They differ when a tenant has not
	// accepted a newer baseline, which is the DESIGNED state (a silent upgrade
	// would rewrite why past taps were flagged -- policy.BaselineVersion), so the
	// screen says so rather than treating it as an error. ShippedStamp is empty
	// for a tenant-authored rule, which Tappa does not ship a version of.
	StoredStamp  string
	ShippedStamp string
	// Statements are the rules in force, read from the STORED document -- never
	// from the shipped one, even for a managed rule where both are to hand. The
	// record pins a policy_version_id, so showing the binary's document beside the
	// database's version number would put a reason on screen that is not the
	// reason a record would give (the same argument assembleBaseline makes).
	Statements []RuleStatement
	// Attachments are the resources this policy is BOUND to. What actually scopes
	// a statement is the Resource list inside it; see Screen for the distinction
	// and why both are shown.
	Attachments []string
	History     []RuleVersion
	// HistoryTotal is how many versions exist, which may exceed len(History).
	HistoryTotal int
	// Problem explains a StateUnreadable, in the parser's own words. It is safe to
	// show: a ValidationError names a statement index, a field and a
	// closed-vocabulary keyword, never a comparison value (validate.go).
	Problem string
}

// Authority is one action of the engine's closed vocabulary: who the tenant's
// stored documents grant it to, and what happens when nothing matches.
type Authority struct {
	Action string
	// Grants are the stored ALLOW statements that grant this action, in the order
	// the documents declare them. Empty means nothing grants it, which -- with the
	// default below -- is the fail-closed case.
	Grants []AuthorityGrant
	// Default is what the engine does when NO statement matches. It is DERIVED by
	// asking the engine itself (see defaultEffectFor), not written down here.
	Default string
	// FailClosed is Default == deny -- i.e. this is an AUTHORIZATION action, and
	// nobody may do it unless a policy says so.
	//
	// 🔴 THE SPLIT IS DERIVED FROM THE DEFAULT, NOT FROM THE ACTION'S NAME. ADR
	// 0004 §3's rule is "tap RECORDING fails to review, every other action fails
	// closed", and the trap is the prefix: tap:approve carries tap: and is an
	// authorization action, which is a misreading the M3-04 card had to correct in
	// writing. Splitting on the answer the engine gives makes the screen unable to
	// repeat that mistake, and an eighth action lands in the right section on the
	// day it is added.
	FailClosed bool
	// GuardrailSid names a guardrail that constrains this action AND NO OTHER, or
	// "" when none does. Derived from the shipped guardrail slice.
	GuardrailSid string
}

// AuthorityGrant is one statement granting one action.
//
// 🔴 IT CARRIES THE SCOPE AND THE LEFTOVER CONDITIONS BECAUSE "role — sid" ALONE
// OVERSTATES. A grant is an IAM-shaped statement: it has a resource list and a
// condition block, and the role is only the part of the condition that keys on
// actor:role. A statement scoped to one branch and conditioned on
// tap:channel = qr would have printed as a flat "any role" grant of the whole
// action — a customer reading that would believe they had handed out an authority
// they had actually fenced twice. So the fence travels with the grant, and a
// condition this shape cannot name is COUNTED rather than dropped.
type AuthorityGrant struct {
	// Role is the panel role the statement keys on, or anyRole when its condition
	// does not mention actor:role at all.
	Role string
	Sid  string
	// Resources is the statement's own resource list -- what actually scopes it
	// (attachments do not; see ListPolicyAttachments).
	Resources []string
	// OtherConditions counts the predicates BESIDE the actor:role one. It is a
	// count rather than a rendering because this line is a summary and the
	// statement itself is printed in full further up the page; what the line must
	// not do is imply there are none.
	OtherConditions int
}

// PolicyScreen is everything the Policies section renders.
type PolicyScreen struct {
	// Queried is set only after the database answered, so an empty screen can be
	// told apart from a failed read (§4.6 -- the same gate the reports section
	// uses; "this tenant has no policies" is a claim).
	Queried bool
	// Guardrails are the immutable system rules in their normative order.
	Guardrails []Guardrail
	// Baseline is every document Tappa SHIPS, in the canonical order
	// internal/policy declares them -- present or not in this tenant's database.
	// A shipped rule that is not stored is the point of the list, so it is built
	// from the shipped set and filled in from the rows, never the other way round.
	Baseline []Rule
	// Tenant is the customer's own layer, ordered (name, id).
	Tenant []Rule
	// Authorities is the action vocabulary with its grants and defaults.
	Authorities []Authority
	// ShippedBaseline is the baseline version this binary carries.
	ShippedBaseline string
	// 🔴 TWO FLAGS AND NO COMBINED ONE, BECAUSE THE COMBINED ONE WENT DEAD THE MOMENT
	// THE SCREEN SPLIT ITS NOTICE. There was a `Capped` here meaning "either read was
	// truncated", documented as the thing "the screen says a list is short" from --
	// and once the two notices were separated nothing on the screen read it: only a
	// test did. A field whose comment describes a use it no longer has is the same
	// defect one layer down from the ones this file exists to avoid, so it is gone
	// rather than kept for symmetry.
	//
	// VersionsCapped is the one that matters to a rule's STATE: a truncated version
	// read can drop the container row that tells "never set up" from "set up with no
	// rules". AttachmentsCapped only shortens the "Bound to" lists -- and the screen
	// says so in its own words, because saying "a list is short" without naming the
	// list was measured telling a customer three false things.
	VersionsCapped    bool
	AttachmentsCapped bool

	// Unreadable names every SHIPPED baseline document whose stored version this
	// binary cannot parse, and GuardrailsOnly says what that costs.
	//
	// 🔴 ONE UNREADABLE DOCUMENT TAKES THE WHOLE POLICY LAYER DOWN, AND THE SCREEN
	// HAS TO SAY SO. internal/domain/checkin's forTenant reports an unparseable
	// stored baseline document as MISSING, tries to materialise it (an ON CONFLICT
	// DO NOTHING no-op, because the policy and its version already exist), re-reads,
	// still finds it missing -- and then returns the guardrails ALONE. Not the other
	// eight baseline documents, not the tenant's own policies: nothing. Every tap in
	// that business falls to the built-in default from then on.
	//
	// So a screen that flagged only the broken rule would be correct about that one
	// rule and WRONG ABOUT EVERY OTHER LINE ON THE PAGE: eight rules with an "In
	// force" chip that decide nothing, and a "who may do what" section listing
	// grants the engine is not applying. That is exactly the failure this file's own
	// storedRule comment names -- a customer believing a rule is in force while the
	// engine ignores it -- and the first version of this screen fixed it for the
	// broken rule and left the blast radius alone.
	//
	// WHY THIS STATE AND NOT THE OTHER THREE. StateOff is skipped by
	// assembleBaseline rather than reported missing, so it costs nothing.
	// StateNoDocument and StateNotProvisioned ARE reported missing, but the next tap
	// materialises them and the layer comes back -- they are self-healing. Only an
	// unreadable stored document survives materialisation, because
	// EnsureBaselinePolicyVersion conflicts on (tenant, policy, version_no) and does
	// nothing. The half of this claim that lives in the other package is already
	// pinned there, against real Postgres, by
	// checkin.TestPolicySetDB_ACorruptStoredDocumentFallsBackToGuardrailsAlone --
	// which asserts len(set.Baseline) == 0 with ONE unusable document and shows the
	// resulting decision is the fail-to-review default. If that behaviour ever
	// changes, that test goes red and this flag is retaken rather than left
	// describing a fallback that no longer happens.
	Unreadable     []string
	GuardrailsOnly bool
	// Zone is the business's own timezone, for the version stamps. Everything in
	// the database is UTC and conversion happens at render (§6); this is the render
	// layer being told WHICH zone. Nil means UTC, which is also what a zone the
	// runtime cannot load falls back to -- loudly, in the log, never silently.
	Zone *time.Location
}

// Rulebook answers "what rules is this business running under". It is a READER:
// every store call it makes is a SELECT.
type Rulebook struct {
	data Database
	// params are the bounded guardrail windows THE DECIDING SERVICE holds
	// (checkin.Service.Params). They are passed in rather than rebuilt from
	// configuration, because rebuilding them is how a screen comes to print a
	// window no guardrail compares against -- hand-off N3 was that exact defect
	// one layer down, where TAPPA_DEBOUNCE_SECONDS was range-checked at startup
	// and then never reached the guardrail.
	params policy.Params
	log    *slog.Logger
}

// NewRulebook wires a Rulebook. A nil database is refused, and so is a Params
// outside the ADR 0004 §11 ranges: a screen that printed an out-of-range window
// would be describing a service that cannot legally run, and the constructor is
// the last place that can tell.
func NewRulebook(data Database, params policy.Params, log *slog.Logger) (*Rulebook, error) {
	if data == nil {
		return nil, errors.New("tenant: nil database")
	}
	if err := params.Validate(); err != nil {
		return nil, fmt.Errorf("tenant: rulebook: %w", err)
	}
	if log == nil {
		log = slog.Default()
	}
	return &Rulebook{data: data, params: params, log: log}, nil
}

// loadZone resolves the business's timezone, falling back to UTC LOUDLY.
//
// IT IS Plaques.loadZone, AND THE THIRD COPY IS NAMED RATHER THAN HIDDEN -- for
// the reason that one gives about the second: sharing it would mean either
// importing a read package from a write one or a new package for eight lines. What
// every copy must keep is that the fallback is logged: a zone the runtime cannot
// load must not cost a manager their page, and it must not silently move every
// timestamp on it either.
func (r *Rulebook) loadZone(name string) *time.Location {
	if name == "" {
		r.log.Warn("tenant: this business has no timezone; showing policy history in UTC",
			"fallback", "UTC")
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		r.log.Error("tenant: cannot load the business timezone; showing policy history in UTC",
			"zone", name, "fallback", "UTC", "err", err)
		return time.UTC
	}
	return loc
}

// Screen reads the tenant's whole policy layer and assembles the section.
//
// THE THREE READS SHARE ONE TRANSACTION so the history and the attachments belong
// to the same policies the rules were read from; separate round trips could
// straddle a write and show a version strip under a rule that is no longer there.
func (r *Rulebook) Screen(ctx context.Context, tenantID uuid.UUID) (PolicyScreen, error) {
	if tenantID == uuid.Nil {
		return PolicyScreen{}, errors.New("tenant: tenant id is required")
	}
	var (
		set      []store.ListPolicySetRow
		versions []store.ListPolicyVersionsRow
		attached []store.ListPolicyAttachmentsRow
		zone     *time.Location
	)
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		var e error
		// ListPolicySet is the SAME query the tap path builds its decision Set from
		// (internal/domain/checkin). That is deliberate: the panel is the face of the
		// engine, and reading through a different query would let the screen and the
		// engine disagree about which version is current or which policies exist --
		// the disagreement a customer would only find out about from a payslip.
		//
		// ⚠️ IT HAS NO LIMIT, AND THAT IS A COUNTED EXPOSURE RATHER THAN AN OVERSIGHT.
		// The argument for keeping documents out of the HISTORY read -- a row cap is
		// not a byte cap -- applies here too, and harder, because this query returns
		// the document of every policy: at internal/policy's declared ceiling that is
		// DefaultMaxDocumentsPerTenant documents of up to DefaultMaxDocumentBytes each
		// in one request. What stops it being fixed HERE is that this is the TAP
		// path's query: adding a LIMIT would silently truncate the decision set, and a
		// partial baseline is a DIFFERENT policy rather than a weaker one (the
		// argument policyset.go opens with). So the panel inherits an exposure the tap
		// path already carries, and bounding it is a change to the decision path --
		// M7-03/phase B, not a line here. Today's data is nowhere near it (measured
		// 2026-08-11: the largest single tenant's policy count is in double figures).
		if set, e = q.ListPolicySet(ctx, tenantID); e != nil {
			return fmt.Errorf("list policy set: %w", e)
		}
		// ROW LIMITS ARE ASKED FOR AS limit+1 so the cap is detectable without a
		// second COUNT -- the venue section's pattern, for the same reason.
		if versions, e = q.ListPolicyVersions(ctx, store.ListPolicyVersionsParams{
			TenantID: tenantID, RowLimit: policyRowLimit + 1,
		}); e != nil {
			return fmt.Errorf("list policy versions: %w", e)
		}
		if attached, e = q.ListPolicyAttachments(ctx, store.ListPolicyAttachmentsParams{
			TenantID: tenantID, RowLimit: attachmentRowLimit + 1,
		}); e != nil {
			return fmt.Errorf("list policy attachments: %w", e)
		}
		// THE ZONE IS READ IN THE SAME TRANSACTION as the rows it renders, so a
		// business whose zone is changed mid-request cannot produce stamps in one zone
		// and a label in another. GetTenantClock is an EXISTING query (M6-03's).
		clock, e := q.GetTenantClock(ctx, tenantID)
		if e != nil {
			return fmt.Errorf("read tenant clock: %w", e)
		}
		zone = r.loadZone(clock.Timezone)
		return nil
	})
	if err != nil {
		return PolicyScreen{}, fmt.Errorf("tenant: policy screen: %w", err)
	}
	out := r.assemble(set, versions, attached)
	out.Zone = zone
	return out, nil
}

// assemble turns three result sets into the screen. It is SEPARATE FROM THE READ
// and it is pure: no database, no clock, no tenant id.
//
// 🔴 THAT SEPARATION IS WHAT MAKES THE ORDERING RULE TESTABLE. The one property
// this assembly must have -- that the reading order comes from internal/policy and
// not from the rows -- can only be falsified by handing it rows in a DIFFERENT
// order and getting the same screen back, and a test that has to stand up Postgres
// to do that would be measuring the database's ORDER BY instead. With a pure
// function the shuffle is three lines
// (TestAssemble_KeepsTheShippedBaselineOrderWhateverTheRowsSay).
func (r *Rulebook) assemble(
	set []store.ListPolicySetRow,
	versions []store.ListPolicyVersionsRow,
	attached []store.ListPolicyAttachmentsRow,
) PolicyScreen {
	out := PolicyScreen{
		Guardrails:      r.guardrails(),
		ShippedBaseline: policy.BaselineVersion,
	}
	// 🔴 TWO CEILINGS, TWO FLAGS, AND COLLAPSING THEM COST A KNOWN ANSWER. One flag
	// meant that hitting the ATTACHMENT ceiling made every rule report "cannot be
	// determined" -- even though the only read that can distinguish those states
	// (versions) had come back COMPLETE. Measured: 401 attachment rows with a whole
	// versions read gave 9/9 unknown; 400 gave 9/9 "not set up yet". The direction
	// was safe (it admitted ignorance) but hiding an answer you have is a loss on the
	// one screen whose job is to give it.
	if len(versions) > policyRowLimit {
		out.VersionsCapped = true
		versions = versions[:policyRowLimit]
	}
	if len(attached) > attachmentRowLimit {
		out.AttachmentsCapped = true
		attached = attached[:attachmentRowLimit]
	}

	stored := indexByName(set)
	history := historyByPolicy(versions)
	scope := scopeByPolicy(attached)
	containers := containersByName(versions)

	// 🔴 THE BASELINE IS WALKED IN THE SHIPPED ORDER, NOT THE DATABASE'S.
	// ListPolicySet comes back ordered by a random uuid (its own comment says so),
	// so building the list from the rows would make the reading order depend on how
	// uuids happened to sort -- and would make a MISSING document invisible, since
	// a row that is not there cannot order itself into a list. Walking
	// policy.Baseline() gives both: a stable order and a slot for every shipped
	// document whether or not this tenant has it.
	for _, doc := range policy.Baseline() {
		out.Baseline = append(out.Baseline, r.baselineRule(doc, stored, containers, history, scope))
	}

	// The tenant's own layer, ordered (name, id) -- the same tiebreak
	// checkin.tenantPolicies applies, so the panel lists them in the order the
	// engine walks them.
	var own []store.ListPolicySetRow
	for _, row := range set {
		if policy.Layer(row.Layer) == policy.LayerTenant {
			own = append(own, row)
		}
	}
	sort.Slice(own, func(i, j int) bool {
		if own[i].Name != own[j].Name {
			return own[i].Name < own[j].Name
		}
		return own[i].PolicyID.String() < own[j].PolicyID.String()
	})
	for _, row := range own {
		out.Tenant = append(out.Tenant, r.storedRule(row, false, history, scope))
	}
	// A tenant-layer CONTAINER with no version at all is in neither list yet: it is
	// not a shipped document and ListPolicySet cannot see it. It is added here so a
	// half-written customer policy is visible rather than absent.
	for _, row := range versions {
		if policy.Layer(row.Layer) != policy.LayerTenant || row.VersionID != nil {
			continue
		}
		out.Tenant = append(out.Tenant, Rule{
			PolicyID:    row.PolicyID,
			Name:        row.Name,
			State:       StateNoDocument,
			Attachments: scope[row.PolicyID],
		})
	}

	// 🔴 THE BLAST RADIUS OF AN UNREADABLE DOCUMENT, COMPUTED BEFORE ANYTHING IS
	// LABELLED. See PolicyScreen.Unreadable for why one broken document silences the
	// whole layer, and why only THIS state does it.
	for _, r := range out.Baseline {
		if r.State == StateUnreadable {
			out.Unreadable = append(out.Unreadable, r.Name)
		}
	}
	out.GuardrailsOnly = len(out.Unreadable) > 0

	// 🔴 THE CAP CHANGES A STATE, SO A CAPPED READ MUST NOT REPORT THE STATE IT
	// CANNOT KNOW. ListPolicyVersions is limited over the WHOLE tenant, and a
	// container with no version is a single row in that stream -- so a truncated
	// read can drop it, and containersByName then has nothing, and the rule prints
	// as "never set up" when it is actually "set up with no rules in it". By this
	// file's own argument those two need different acts, so under a cap the honest
	// answer is that the state is unknown rather than the more comfortable of the
	// two. It is scoped to the states the cap can actually fabricate: a rule
	// ListPolicySet returned is unaffected (that query is not the capped one).
	if out.VersionsCapped {
		for i := range out.Baseline {
			if out.Baseline[i].State == StateNotProvisioned {
				out.Baseline[i].State = StateUnknown
			}
		}
	}

	out.Authorities = r.authorities(out.Baseline, out.Tenant)
	out.Queried = true
	return out
}

// guardrails renders the shipped guardrail slice.
//
// 🔴 THE ORDER AND THE MEMBERSHIP ARE BOTH DERIVED FROM policy.Guardrails(). This
// screen exists partly so a manager can see WHY a record was refused, which means
// the numbers beside these rules have to be the numbers the engine used. A
// hand-written list here would be a second representation of a security boundary
// that has already moved once (ADR 0007 renumbered four of them in one commit),
// and the copy would have gone on saying the old thing.
func (r *Rulebook) guardrails() []Guardrail {
	shipped := policy.Guardrails(r.params)
	out := make([]Guardrail, 0, len(shipped))
	for i, g := range shipped {
		out = append(out, Guardrail{
			Order:   i + 1,
			Sid:     g.Sid,
			Effect:  string(g.Effect),
			Reason:  g.Reason,
			Rule:    guardrailRules[g.Sid],
			Bounded: r.boundedFor(g.Sid),
		})
	}
	return out
}

// guardrailRules says, for each shipped guardrail, WHY it cannot be switched off.
//
// It is a map keyed by sid rather than an ordered list, so it cannot express an
// order and therefore cannot contradict the one above.
// TestGuardrailRules_CoverTheShippedSlice holds it to the shipped set in BOTH
// directions: a guardrail with no sentence would render a lock with no reason
// (the card's trap, one step removed), and a sentence for a guardrail that no
// longer ships is a rule a reader would believe is running.
var guardrailRules = map[string]string{
	"sys:tenant-mismatch": "§4.5 — one organisation's records can never be written into another's. " +
		"No record is written at all here, so neither business sees the other's people.",
	"sys:tag-not-active": "§5 row 1 — a plaque that is lost, retired or still in its box proves " +
		"nothing about a place, so a tap on one cannot stand as attendance.",
	"sys:sun-invalid": "§4.4 — the plaque's counter check is what stops a captured link being " +
		"spent twice. Loosening it would reopen replay.",
	"sys:no-session": "§5 row 3 — with nobody identified there is nobody to record. The tap " +
		"becomes an activation, not an attendance row.",
	"sys:employee-deactivated": "§5 row 4 — somebody who has left cannot clock in, and the attempt is " +
		"pushed to your managers rather than logged quietly.",
	"sys:tap-freshness": "ADR 0004 §11 — a tap page that has been kept for later is not a touch. " +
		"The window is yours to set; switching it off is not.",
	"sys:occurred-at-bound": "ADR 0004 §11 — the time a phone declares cannot be invented. The " +
		"tolerance is yours to set; accepting any time at all is not.",
	"sys:person-debounce": "§5 row 5 — one person's repeat inside the window is recorded and not " +
		"counted, so a double tap never becomes a double shift.",
	"sys:policy-edit-owner-only": "ADR 0004 K2 — only the owner may change the rules. A manager who could " +
		"widen a rule could widen their own pay.",
	"sys:no-self-review": "ADR 0004 Y-C — nobody approves their own record. This is the one " +
		"separation a small business cannot organise around.",
}

// boundedFor returns the tunable window a guardrail compares against, or nil.
//
// THE SET IS TIED TO policy.Params BY A TEST rather than by a comment:
// TestBoundedParams_CoverEveryWindowTheEngineHolds counts the fields of
// policy.Params, so a fourth bounded window added to the engine turns this red
// instead of quietly not appearing on the screen that exists to list them.
func (r *Rulebook) boundedFor(sid string) *BoundedParam {
	switch sid {
	case "sys:person-debounce":
		return &BoundedParam{
			Label:   "Repeat window for one person",
			Current: seconds(r.params.DebounceWindow),
			Min:     plainSeconds(policy.DebounceMinSeconds),
			Max:     plainSeconds(policy.DebounceMaxSeconds),
		}
	case "sys:tap-freshness":
		return &BoundedParam{
			Label:   "How long a tap page stays good for",
			Current: seconds(r.params.FreshnessWindow),
			Min:     plainSeconds(policy.FreshnessMinSeconds),
			Max:     plainSeconds(policy.FreshnessMaxSeconds),
		}
	case "sys:occurred-at-bound":
		return &BoundedParam{
			Label:   "How far back a declared tap time may sit",
			Current: seconds(r.params.OccurredAtSkewMax),
			Min:     plainSeconds(policy.OccurredAtSkewMinSeconds),
			Max:     plainSeconds(policy.OccurredAtSkewMaxSeconds),
		}
	}
	return nil
}

// baselineRule pairs one SHIPPED document with whatever this tenant has stored
// for it.
func (r *Rulebook) baselineRule(
	doc policy.BaselineDoc,
	stored map[string]store.ListPolicySetRow,
	containers map[string]store.ListPolicyVersionsRow,
	history map[uuid.UUID][]RuleVersion,
	scope map[uuid.UUID][]string,
) Rule {
	row, ok := stored[doc.Name]
	if !ok {
		// Nothing in ListPolicySet. Either there is no policies row at all, or there
		// is one with no version -- and the two are different problems, which is why
		// ListPolicyVersions outer-joins where ListPolicySet does not.
		if c, half := containers[doc.Name]; half {
			return Rule{
				PolicyID: c.PolicyID, Name: doc.Name, Managed: true,
				State: StateNoDocument, ShippedStamp: policy.BaselineVersion,
				Attachments: scope[c.PolicyID],
			}
		}
		return Rule{
			Name: doc.Name, Managed: true, State: StateNotProvisioned,
			ShippedStamp: policy.BaselineVersion,
		}
	}
	rule := r.storedRule(row, true, history, scope)
	rule.ShippedStamp = policy.BaselineVersion
	return rule
}

// storedRule renders one stored policy row.
func (r *Rulebook) storedRule(
	row store.ListPolicySetRow,
	managed bool,
	history map[uuid.UUID][]RuleVersion,
	scope map[uuid.UUID][]string,
) Rule {
	rule := Rule{
		PolicyID:    row.PolicyID,
		Name:        row.Name,
		Managed:     managed,
		VersionNo:   int(row.VersionNo),
		Attachments: scope[row.PolicyID],
	}
	all := history[row.PolicyID]
	rule.HistoryTotal = len(all)
	if len(all) > versionsShownPerPolicy {
		all = all[:versionsShownPerPolicy]
	}
	rule.History = all

	// 🔴 THE OFF SWITCH IS READ BEFORE THE DOCUMENT, AND THE ORDER IS THE WHOLE
	// POINT. It used to parse first, so a policy the tenant had SWITCHED OFF whose
	// document also failed to parse came back as StateUnreadable -- which sets
	// GuardrailsOnly and puts the page's loudest sentence on a screen where it is
	// FALSE. Measured: with a disabled unreadable document the panel reported
	// state=unreadable, GuardrailsOnly=true, while the engine reported baseline=8,
	// missing=0 -- eight rules genuinely deciding, and the page saying none of them
	// was.
	//
	// The fix is to CLASSIFY THE WAY THE ENGINE DOES rather than to special-case the
	// flag: checkin.assembleBaseline tests `!row.Enabled` and `continue`s BEFORE it
	// parses, so a disabled document is a deliberate absence that costs nothing and
	// its contents are never looked at. This function now makes the same cut in the
	// same order, which is what makes the sentence beside GuardrailsOnly true of the
	// code beside it.
	//
	// WHAT IS GIVEN UP, counted: a disabled rule whose document is unreadable reads
	// as "Switched off by you" and says nothing about the document. That matches
	// what it COSTS today (nothing), and phase B -- which owns the switch -- is where
	// turning one back ON has to check that its document still parses.
	if !row.Enabled {
		rule.State = StateOff
		return rule
	}
	layer := policy.LayerTenant
	if managed {
		layer = policy.LayerBaseline
	}
	doc, err := policy.ParseAndValidate(row.Document, layer, policy.DefaultLimits())
	if err != nil {
		// The tap path answers this by falling back (a partial baseline is a
		// DIFFERENT policy, not a weaker one). The panel answers it by SAYING so:
		// printing an unreadable document as if it were running is how a customer
		// comes to believe a rule is in force when the engine is ignoring it, which
		// ADR 0004 calls the most dangerous failure there is.
		rule.State = StateUnreadable
		rule.Problem = err.Error()
		return rule
	}
	rule.StoredStamp = doc.Version
	rule.Statements = statementsOf(doc)
	rule.State = StateActive
	return rule
}

// statementsOf flattens a parsed document for display.
func statementsOf(doc policy.Document) []RuleStatement {
	out := make([]RuleStatement, 0, len(doc.Statements))
	for _, st := range doc.Statements {
		s := RuleStatement{
			Sid:       st.Sid,
			Effect:    string(st.Effect),
			Resources: append([]string(nil), st.Resource...),
			Reason:    st.Reason,
		}
		for _, a := range st.Action {
			s.Actions = append(s.Actions, string(a))
		}
		s.Conditions = conditionsOf(st.Condition)
		out = append(out, s)
	}
	return out
}

// conditionsOf flattens a condition block into a stable, sorted list.
//
// 🔴 IT SORTS, BECAUSE THE SOURCE IS TWO NESTED Go MAPS. Go randomises map
// iteration, so rendering them in encounter order would reshuffle a customer's
// rules on every page load -- and worse, would make any test that reads the page
// flaky in a way that invites the wrong repair. The evaluator has the same problem
// and solves it the same way (M3-04's trap: "Go'da map iterasyonu rastgeledir").
func conditionsOf(cond policy.Condition) []RuleCondition {
	var out []RuleCondition
	for op, args := range cond {
		for key, value := range args {
			out = append(out, RuleCondition{
				Operator: string(op),
				Key:      string(key),
				Value:    displayValue(value),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Operator != out[j].Operator {
			return out[i].Operator < out[j].Operator
		}
		if out[i].Key != out[j].Key {
			return out[i].Key < out[j].Key
		}
		return out[i].Value < out[j].Value
	})
	return out
}

// displayValue turns a condition argument into a string a manager can read.
//
// A number arrives from encoding/json as a float64 whatever it was written as, so
// 120 must not print as "1.2e+02" or "120.000000"; strconv with 'f' and -1
// precision gives the shortest form that round-trips. A list (StringIn,
// IpInPrefix) is joined rather than printed as a Go slice.
func displayValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, displayValue(e))
		}
		return strings.Join(parts, ", ")
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

// authorities walks the engine's action vocabulary and reports, per action, who
// the STORED documents grant it to and what happens when nothing matches.
//
// 🔴 THE VOCABULARY IS policy.Actions(), NOT A LIST WRITTEN HERE. The point of
// this section is that a customer can see every authority the engine knows about;
// a hand-written list would show every authority somebody remembered, and the
// newest one -- the one nobody has reviewed -- is exactly the one that would be
// missing.
//
// 🔴 THE GRANTS COME FROM THE STORED DOCUMENTS, NOT FROM policy.Baseline(). A
// tenant that has switched the authorization document off, or never had it
// materialised, is in the fail-closed state for every action, and reading the
// shipped baseline would show them a set of grants they do not have. Rules whose
// document did not parse contribute nothing, which is the same thing the engine
// does with them.
func (r *Rulebook) authorities(baseline, own []Rule) []Authority {
	guardrailed := r.actionScopedGuardrails()
	running := make([]Rule, 0, len(baseline)+len(own))
	running = append(running, baseline...)
	running = append(running, own...)

	actions := policy.Actions()
	out := make([]Authority, 0, len(actions))
	for _, action := range actions {
		effect := defaultEffectFor(action)
		auth := Authority{
			Action:       string(action),
			Default:      effect,
			FailClosed:   effect == string(policy.EffectDeny),
			GuardrailSid: guardrailed[string(action)],
		}
		seen := map[string]bool{}
		for _, rule := range running {
			if rule.State != StateActive {
				continue
			}
			for _, st := range rule.Statements {
				if st.Effect != string(policy.EffectAllow) || !contains(st.Actions, string(action)) {
					continue
				}
				role, others := roleOf(st)
				if seen[role+"|"+st.Sid] {
					continue
				}
				seen[role+"|"+st.Sid] = true
				auth.Grants = append(auth.Grants, AuthorityGrant{
					Role:            role,
					Sid:             st.Sid,
					Resources:       append([]string(nil), st.Resources...),
					OtherConditions: others,
				})
			}
		}
		out = append(out, auth)
	}
	return out
}

// actionScopedGuardrails reports, per action, the guardrail that applies to THAT
// ACTION AND NO OTHER.
//
// 🔴 IT ASKS THE GUARDRAILS RATHER THAN READING THEIR NAMES. The obvious
// implementation is to look for ":" in a sid or to keep a list; both are second
// representations of a set the code already owns, and both would be silently
// wrong the day a guardrail is renamed. This runs each guardrail's own Match over
// an otherwise EMPTY context, once per action: a guardrail that fires for exactly
// one action is scoped to it, and one that fires for several (or for none) is not
// an action gate and is left out. The empty context is what makes the test
// meaningful -- there is no tag, no session, no counter and no clock in it, so
// the only thing left that can decide is the action.
//
// TestActionScopedGuardrails_AreTheOnesTheEngineScopesByAction pins today's
// answer AND its shape, so a guardrail that becomes action-scoped shows up here
// rather than being quietly absent from the screen that lists authorities.
func (r *Rulebook) actionScopedGuardrails() map[string]string {
	actions := policy.Actions()
	out := map[string]string{}
	for _, g := range policy.Guardrails(r.params) {
		if g.Match == nil {
			continue
		}
		var fired []policy.Action
		for _, a := range actions {
			if g.Match(policy.Context{Action: a}) {
				fired = append(fired, a)
			}
		}
		if len(fired) != 1 {
			continue
		}
		if _, taken := out[string(fired[0])]; !taken {
			out[string(fired[0])] = g.Sid
		}
	}
	return out
}

// anyRole is the label for a grant that names no role at all -- an allow whose
// condition does not key on actor:role, which applies to every panel role.
const anyRole = "any role"

// roleOf reports which panel role a statement's condition restricts it to, AND how
// many of its other predicates this summary does not name.
//
// The second return value is the honest half: a statement may be fenced by
// conditions that have nothing to do with a role, and a grant line that mentioned
// only the role would describe a wider authority than the statement gives.
func roleOf(st RuleStatement) (role string, otherConditions int) {
	role = anyRole
	for _, c := range st.Conditions {
		if role == anyRole && c.Operator == string(policy.OpStringEquals) && c.Key == string(policy.CtxActorRole) {
			role = c.Value
			continue
		}
		otherConditions++
	}
	return role, otherConditions
}

// defaultEffectFor asks the ENGINE what it does when nothing matches, rather than
// stating it here.
//
// 🔴 IT IS A DOCUMENTATION READ, NOT A GATE, AND THE DIFFERENCE MATTERS TO THE
// NEXT READER. Nothing on this screen authorises anything: it evaluates an EMPTY
// Set -- no guardrails, no baseline, no tenant policies, no request context -- so
// the only thing the engine can answer with is its built-in default. That is
// precisely the quantity the card asks to be printed ("the default is fail-closed,
// on the screen"), and asking the engine is the only way to print it that cannot
// drift from what the engine actually does. ADR 0004 §3's rule is subtle enough to
// get wrong by hand: tap:approve carries the tap: prefix and still defaults to
// DENY, which is the misreading the M3-04 card had to correct in writing.
func defaultEffectFor(action policy.Action) string {
	return string(policy.Evaluate(policy.Set{}, policy.Context{Action: action}).Effect)
}

// indexByName keys the stored set by policy name, which is how a shipped baseline
// document finds its row (policies has no UNIQUE (tenant_id, name), so the id is
// derived from the name -- checkin.baselinePolicyID -- and the name is the join).
func indexByName(rows []store.ListPolicySetRow) map[string]store.ListPolicySetRow {
	out := make(map[string]store.ListPolicySetRow, len(rows))
	for _, r := range rows {
		if policy.Layer(r.Layer) == policy.LayerBaseline {
			out[r.Name] = r
		}
	}
	return out
}

// containersByName keys the baseline-layer policy rows that have NO version at
// all -- the half-provisioned state ListPolicySet cannot report.
func containersByName(rows []store.ListPolicyVersionsRow) map[string]store.ListPolicyVersionsRow {
	out := map[string]store.ListPolicyVersionsRow{}
	for _, r := range rows {
		if r.VersionID == nil && policy.Layer(r.Layer) == policy.LayerBaseline {
			out[r.Name] = r
		}
	}
	return out
}

// historyByPolicy groups the version rows, newest first, and marks the current
// one.
//
// THE QUERY ALREADY ORDERS BY version_no DESC WITHIN A POLICY, and this does not
// re-sort: the first row of a group is the highest version_no, which is the same
// row ListPolicySet's DISTINCT ON returns, so "current" here and "current" in the
// engine are the same fact rather than two computations of it.
func historyByPolicy(rows []store.ListPolicyVersionsRow) map[uuid.UUID][]RuleVersion {
	out := map[uuid.UUID][]RuleVersion{}
	for _, r := range rows {
		if r.VersionID == nil || r.VersionNo == nil {
			continue
		}
		at := time.Time{}
		if r.VersionCreatedAt != nil {
			at = *r.VersionCreatedAt
		}
		out[r.PolicyID] = append(out[r.PolicyID], RuleVersion{
			No:      int(*r.VersionNo),
			At:      at,
			Author:  authorOf(r),
			Bytes:   int(r.DocumentBytes),
			Current: len(out[r.PolicyID]) == 0,
		})
	}
	return out
}

// AuthorSystem and AuthorUnresolved are the two labels for a version nobody in
// this tenant signed.
//
// 🔴 THE SECOND ONE IS A §4.5 OUTCOME AND IT IS NAMED RATHER THAN BLANK.
// policy_versions.created_by is an FK-LESS uuid (00007 defers the admin FK to
// M6/M7), so nothing in the schema stops an id that names no admin this tenant
// can see -- and RLS means a cross-tenant id CANNOT be resolved here, which is the
// system working. Leaving the cell blank would read as "nobody", and "nobody
// decided this" is a different claim from "we cannot tell you who did". The id
// itself is not printed: it identifies no person to the reader and answers no
// question a manager has, and a support question is where it belongs.
const (
	AuthorSystem     = "Tappa (system provisioning)"
	AuthorUnresolved = "not resolvable in this organisation"
)

func authorOf(r store.ListPolicyVersionsRow) string {
	if r.CreatedBy == nil {
		return AuthorSystem
	}
	if r.AuthorName == nil || strings.TrimSpace(*r.AuthorName) == "" {
		return AuthorUnresolved
	}
	return *r.AuthorName
}

// scopeByPolicy groups the attachment rows.
func scopeByPolicy(rows []store.ListPolicyAttachmentsRow) map[uuid.UUID][]string {
	out := map[uuid.UUID][]string{}
	for _, r := range rows {
		out[r.PolicyID] = append(out[r.PolicyID], r.Resource)
	}
	return out
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// seconds and plainSeconds print a window. Durations are integer seconds here by
// construction (config parses whole seconds and the ranges are whole seconds), so
// there is no rounding decision to get wrong.
func seconds(d time.Duration) string { return plainSeconds(int(d / time.Second)) }

func plainSeconds(s int) string {
	switch {
	case s >= 3600 && s%3600 == 0:
		return strconv.Itoa(s/3600) + " h"
	case s >= 60 && s%60 == 0:
		return strconv.Itoa(s/60) + " min"
	default:
		return strconv.Itoa(s) + " s"
	}
}
