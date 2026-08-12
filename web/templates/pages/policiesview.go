package pages

// The POLICIES section's view model (M6-09 -- the read side AND the controls).
//
// ⚠️ THIS HEADER DESCRIBED A MODEL THAT NO LONGER EXISTED. It said the type carries
// "no form target and no per-rule action href" while PoliciesView.Action and
// PolicyRuleView.Action sit in this same file, put there by phase B. That absence was
// real in phase A and is not the guarantee any more; leaving the sentence would teach
// the next author that a control cannot be built from this type, which is the opposite
// of what it now says.
//
// 🔴 ONE ABSENCE DOES SURVIVE PHASE B, AND IT IS THE ONE THAT MATTERED:
// PolicyGuardrailView carries nothing a toggle could be built from, because a
// guardrail has no off switch anywhere in the product. The M6-09 card's first trap
// is drawing one as a control that happens to be disabled -- the customer tries it,
// cannot move it, and stops believing the rest of the screen. A type with no field
// for it cannot be persuaded otherwise later.
// TestPolicyGuardrailView_CarriesNoSwitch is the mechanical half.
//
// 🔴 §4.7 -- NOTHING BELOW COULD CARRY A SECRET. The queries behind this screen select
// no token hash, no coordinate, no key reference and no email (a count sat here saying
// "three" while the section read six -- the property is what they select, not how many
// there are), the domain types have no field for them, and neither does anything here. What a
// policy document holds -- operators, context keys and comparison values -- is
// authored by the customer and is not secret (validate.go argues the same about
// its own error messages). The one identity on the page is a version's author, a
// name this tenant already owns; an id that resolves to nobody is reported as
// unresolvable and the id itself is not printed.
//
// EVERY VALUE IS ALREADY A STRING. The handler formats once, against the zone the
// database gave for this tenant (§6: the database is UTC, the render layer
// converts). A template doing its own arithmetic would be a second implementation
// of the thing the section is about.

// PolicyGuardrailView is one immutable system rule.
//
// 🔴 NO FIELD HERE IS A CONTROL. Not a bool, not an href, not a form name -- see
// the file header. The tunable window is three STRINGS: what it is set to and the
// two ends of its range.
//
// ⚠️ IT USED TO SAY "PHASE B's EDITOR FOR IT WILL BE A FORM ON ITS OWN" -- future
// tense about the phase that shipped. There is no editor: those windows come from
// configuration and the schema holds no per-tenant value for them, which the card
// records as an unmet criterion rather than as a plan. What has not changed is that
// an editor for them, if one is ever built, still would not be an on/off switch:
// ADR 0004 §11's pattern is "cannot be switched off, MAY be tuned inside a range"
// and the first half is the product.
type PolicyGuardrailView struct {
	// Order is the guardrail's position in the engine's own list, as a string
	// because it is printed rather than counted. The engine derives it; the panel
	// never numbers these itself (ADR 0007 renumbered four of them once, and a
	// hand-written copy here would still be showing the old order).
	Order string
	Sid   string
	// Effect is the engine's keyword (deny, redirect, ignore) and Outcome is the
	// same fact in words. Both, because the keyword is what a REJECTED docket
	// carries and the sentence is what a manager reads.
	Effect  string
	Outcome string
	// Reason is the sentence the employee sees when this fires.
	Reason string
	// Rule is why it cannot be switched off, naming the red line it enforces.
	Rule string
	// Tunable, Current and Range describe the bounded window, or are empty for a
	// guardrail with none. Tunable != "" is what the template tests, so there is no
	// bool to mistake for a state.
	Tunable string
	Current string
	Range   string
}

// PolicyStatementView is one statement of a document, rendered as fields rather
// than as JSON. The raw-JSON editor is M9-07 and deliberately out of v1 (Q22).
type PolicyStatementView struct {
	Sid        string
	Effect     string
	Outcome    string
	Actions    []string
	Resources  []string
	Conditions []string
	Reason     string
}

// PolicyVersionView is one row of a rule's history: which version, when, by whom,
// how big.
type PolicyVersionView struct {
	No     string
	At     string
	Author string
	Bytes  string
	// Marker labels the version the engine is using, in WORDS, and MarkerTone picks
	// its chip colour. Both are strings rather than a bool because both are printed:
	// skill tappa-brand's rule is that status is never carried by colour alone, and
	// a chip with no text means nothing to a screen reader.
	//
	// 🔴 THE MARKER IS NOT ALWAYS "IN USE", AND ASSUMING IT WAS SURVIVED THE FIRST
	// FIX OF THE UNREADABLE-DOCUMENT DEFECT. On a page where the engine has dropped
	// the whole layer the chips said "In force" (fixed) while the history strips
	// went on saying "in use" in tappa-green, nine times, about versions nothing was
	// using. Same class of claim, one element further down.
	Marker     string
	MarkerTone string
	// Href is the address of THIS version's own page, empty when there is none.
	//
	// 🔴 IT IS PER ROW BECAUSE THE CARD ASKS FOR "AN OLDER VERSION CAN BE LOOKED AT",
	// AND THE FIRST PHASE-B ATTEMPT ONLY REACHED THE NEWEST. There was one link per
	// rule, built from the rule's CURRENT version number, under a paragraph saying an
	// earlier version's rules are on a page of their own — so the page declared a
	// capability that needed a hand-edited URL. Announcing a guarantee the screen does
	// not provide is the class this section has now paid for five times.
	Href string
}

// PolicyRuleView is one policy: what it is, whether it is running, what it says.
type PolicyRuleView struct {
	Name string
	// State is the machine word and StateLabel the sentence; StateTone picks the
	// chip's colour from the brand's fixed mapping. The label always carries the
	// meaning, so the colour only reinforces it.
	State      string
	StateLabel string
	StateTone  string
	// Managed says who owns the document, in words.
	Managed bool
	// Version is "version 3" or empty when nothing is stored.
	Version string
	// Stamp is the version stamp inside the STORED document, and Outdated is the
	// sentence shown when it differs from the one this binary ships. Existing
	// tenants are deliberately never auto-updated (a silent baseline change would
	// rewrite why past taps were flagged), so this is a designed state rather than
	// an error, and it says so.
	Stamp    string
	Outdated string
	// Problem explains an unreadable document in the parser's own words.
	Problem     string
	Statements  []PolicyStatementView
	Attachments []string
	History     []PolicyVersionView
	// HistoryNote is "showing 5 of 23" when the strip is short, else empty.
	HistoryNote string

	// --- M6-09 phase B: the controls -------------------------------------------
	//
	// 🔴 EVERY ONE OF THEM IS EMPTY WHEN THE SERVER WOULD REFUSE THE ACT, AND THAT IS
	// THE POINT OF PUTTING THEM HERE RATHER THAN BEHIND A `v.MayEdit` TEST IN THE
	// TEMPLATE. A template that decided for itself which rule gets a switch would be a
	// second representation of the engine's answer; these are filled in by the handler
	// from ONE question asked once (panelScribe.May), so the page cannot offer a
	// control the writer's gate would turn away. Hiding the control is the courtesy;
	// tenant.RuleWriter.authorise is the guarantee.

	// ID is the stored policy's id, as a string because it is a form value. It is
	// empty for a shipped rule this business has never had materialised -- which is
	// exactly the case that gets no controls, because there is no row to change.
	ID string
	// Action is where this rule's controls post: the CONFIRMATION step, never the
	// write. It is carried per rule rather than read from a package variable in the
	// template so that the address lives in one place (the handler, which reads it
	// from the section's own route) and a template cannot invent a second one.
	Action string
	// Switch is the on/off control, or the zero value when there must not be one.
	Switch PolicySwitchView
	// Unbind is one control per existing binding. It is SEPARATE from Attachments
	// (which is the printed list) because the two have different lengths in the one
	// case that matters: a manager sees the bindings and gets no buttons.
	Unbind []PolicyBindingView
	// Bindable is the scope picker's options, empty when the actor may not bind.
	Bindable []PolicyScopeOptionView
}

// PolicySwitchView is one rule's on/off control.
//
// 🔴 IT IS A LABEL AND AN OPERATION, NOT A BOOLEAN, so a template cannot render it
// as a toggle whose meaning depends on a colour. Label == "" means NO CONTROL AT
// ALL -- not a disabled one, which is the M6-09 card's first trap. It is also why
// PolicyGuardrailView does not and will never have one of these: a guarantee has no
// off switch anywhere in the product.
type PolicySwitchView struct {
	// Op is the word the form posts: the section's closed vocabulary, never a
	// sentence.
	Op string
	// Label is what the button says. It names the OUTCOME ("Switch this rule off"),
	// because a button labelled with a state is one people press to reach the state
	// it is already in.
	Label string
	// Note is the one-line consequence shown beside it.
	Note string
}

// PolicyBindingView is one existing scope binding, with the value that would remove
// it.
type PolicyBindingView struct {
	// Resource is the engine's own string ("*", "location/<uuid>").
	Resource string
	// Label is the same thing in the customer's words -- a venue's NAME where the
	// resource is a uuid. An id that resolves to nothing keeps its raw form and says
	// so, because a binding to a venue that was removed is a real state and hiding it
	// would leave a rule scoped to nowhere with nothing on screen about it.
	Label string
}

// PolicyScopeOptionView is one option of the scope picker.
type PolicyScopeOptionView struct {
	Value string
	Label string
}

// PolicySettingView is a bounded number the product runs on that is NOT a policy and
// NOT per business.
//
// 🔴 IT CARRIES NO CONTROL, AND THAT IS HONEST ABOUT THESE FOUR NUMBERS ONLY. The
// GPS ring and the three guardrail windows come from configuration -- config.go
// range-checks them at start-up -- and the schema has no column to hold a per-tenant
// value for any of them, so a box here would be a box the server ignores, which is
// the mistake this repository pays for most.
//
// ⚠️ IT IS NOT A STATEMENT ABOUT THE M6-09 CRITERION, AND IT USED TO BE ONE. This
// comment said the criterion "an out-of-range value is refused in the interface too"
// CANNOT be met "because there is nothing behind" such a field. An audit refuted it:
// internal/policy's boundedParams binds three document context keys to ranges,
// validate.go refuses an out-of-range numeric predicate in a TENANT DOCUMENT at write
// time, and the shipped "queued tap window" document carries one -- which
// tenant.copyOfShipped copies into a customer's rule. The storage and the validation
// are there; the form field is not. The criterion is unmet because it was not built.
type PolicySettingView struct {
	Label   string
	Current string
	Range   string
	Uses    string
}

// PolicyAuthorityView is one action of the engine's vocabulary: who may do it and
// what happens when no policy says.
type PolicyAuthorityView struct {
	Action string
	// Grants is one line per granting statement. Each line carries the role, the
	// sid, the statement's SCOPE and a count of the conditions the line does not
	// spell out — because "owner — base:authz-owner" on its own describes a wider
	// authority than a statement fenced to one branch actually gives.
	Grants []string
	// Default is the engine's own answer when nothing matches, and DefaultLabel
	// the same in words.
	Default      string
	DefaultLabel string
	// Guardrail names a guardrail that gates this action in CODE, or "" when none
	// does. It is the strongest statement on this section: a rule in this column
	// cannot be widened by any policy at all.
	Guardrail string
}

// PoliciesView is the whole section.
type PoliciesView struct {
	PanelChrome

	// Queried is set ONLY after the database has answered, and it gates the body.
	//
	// 🔴 IT IS THE ANTI-FABRICATION FLAG the other sections carry, and here the
	// claim it guards is unusually load-bearing: a zero PoliciesView renders as "you
	// have no policies", which is precisely the state a customer would act on. A
	// failed read renders a problem page in the handler and never reaches this
	// template.
	Queried bool

	// 🔴 TWO CEILINGS, TWO NOTICES, BECAUSE THEY TRUNCATE DIFFERENT LISTS AND ONE
	// SENTENCE FOR BOTH WAS MEASURED SAYING THREE FALSE THINGS. With only the
	// ATTACHMENT read truncated, the page said "your policy HISTORY is longer than
	// one page reads at once", "so some VERSIONS are not listed below" and "a rule
	// whose row fell outside the page says so instead of guessing" -- all three
	// wrong: the version read was COMPLETE, no rule was reported unknown, and the
	// list actually cut short was "Bound to", which was printed as if it were whole.
	// The domain split the flag (VersionsCapped / AttachmentsCapped); this is the
	// same split arriving on the screen, which is where a customer reads it.
	VersionsCapped    bool
	AttachmentsCapped bool

	// 🔴 GuardrailsOnly IS THE MOST IMPORTANT FIELD ON THIS TYPE. When it is set,
	// ONE stored baseline document cannot be read and the tap engine has therefore
	// dropped the WHOLE policy layer — baseline and tenant — and is deciding on the
	// Tappa guarantees alone. Every other rule on the page is stored, valid and
	// NOT DECIDING ANYTHING, and every grant in the authority section is one the
	// engine is not applying. A page that showed the broken rule in red and left the
	// rest saying "In force" would be telling a customer, on eight lines, the exact
	// thing ADR 0004 calls the most dangerous failure there is.
	//
	// Unreadable names the documents responsible, so the notice can point at them.
	GuardrailsOnly bool
	Unreadable     []string

	// BaselineInForce is whether ANY baseline rule is actually deciding. It exists
	// so the "you have written no policies of your own" empty state does not
	// reassure a business that it is "running on the baseline above" when the page
	// directly above says every baseline rule is not set up (the state a tenant that
	// has never been provisioned is in — measured on the seeded Kebab Manufacturing
	// tenant, which has no policy rows at all).
	BaselineInForce bool

	// ShippedBaseline is the baseline version this binary carries.
	ShippedBaseline string

	Guardrails []PolicyGuardrailView
	Baseline   []PolicyRuleView
	Tenant     []PolicyRuleView

	// Authorities are the actions that fail CLOSED, Recording the ones that fail to
	// review. The split is the engine's own answer, not a reading of the names --
	// tap:approve carries the tap: prefix and belongs with the first group (ADR
	// 0004 §3, a misreading the M3-04 card had to correct in writing).
	Authorities []PolicyAuthorityView
	Recording   []PolicyAuthorityView

	// --- M6-09 phase B ------------------------------------------------------------

	// MayEdit is the ENGINE's answer to `policy:edit` for this signed-in admin,
	// asked once per render through the writer's own gate
	// (tenant.RuleWriter.May -> policy.Evaluate over the stored rulebook).
	//
	// 🔴 IT IS NOT A ROLE TEST AND MUST NOT BECOME ONE. The guardrail
	// sys:policy-edit-owner-only only DENIES non-owners; the OWNER's permission lives
	// in the tenant's stored baseline document, so "is this person an owner" is a
	// DIFFERENT question from "may this person edit the rules" -- an owner whose
	// rulebook has never been materialised is refused, and the page has to say so
	// rather than showing controls that would fail.
	MayEdit bool
	// Action is where the controls post: the confirmation step, never the write.
	Action string
	// ProblemHeading is the notice's TITLE for a refused change, and it is a field
	// rather than a constant for one measured reason.
	//
	// 🔴 THE HEADING WAS "That change was not made" FOR EVERY WORD, INCLUDING THE ONE
	// WHERE THE CHANGE *WAS* MADE. `lockout-stands` means the change took the author's
	// own authority away AND putting it back failed — it stands, and there is no route
	// back through this product. The body said so; the largest sentence on the page
	// said the opposite. That is exactly why tenant.ErrLockoutStands exists as its own
	// sentinel ("the customer's line was wrong, which is the worse half of the pair to
	// get right") — the split was made in the body and forgotten in the title.
	ProblemHeading string
	// Problem is the sentence for a refused change, already chosen by the handler
	// from a closed vocabulary. It is a SENTENCE rather than a code because the page
	// prints it; the code never reaches the template, so there is nothing here to
	// escape and nothing a caller can put words into.
	Problem string

	// ScopeTargets are this business's venues, for the authoring form's picker, and
	// ScopeCapped says the list was truncated. A THIRD ceiling with its own notice --
	// the split the two above already paid for, because a single sentence for several
	// ceilings was measured telling a customer three false things.
	ScopeTargets []PolicyScopeOptionView
	ScopeCapped  bool

	// Supersedable are the customer's OWN rules, offered as "write a new version of
	// this one" beside "a new rule".
	//
	// 🔴 WITHOUT IT THE FORM COULD ONLY EVER CREATE, AND CREATING TWICE IS PERMANENT.
	// The first version of this form carried no policy id at all, so every save took
	// the CREATE branch: pressing it twice left two rows with the same name, both
	// deciding, neither in the other's history — and `policies` grants no DELETE, so
	// nothing in this product can remove either. It also meant the section had no U in
	// its CRUD: the domain has appended versions since day one (measured), and the
	// screen had no way to ask for one.
	Supersedable []PolicyScopeOptionView

	// Authorable are the shipped rules a customer may base their own on. The list is
	// DERIVED (tenant.AuthorableRules asks the engine which actions fail closed), so
	// the authorization document -- the one that grants policy:edit -- is not in it
	// and a customer cannot generate a statement about their own authority.
	Authorable []PolicyScopeOptionView

	// Settings are the bounded numbers that are not policies. See PolicySettingView.
	Settings []PolicySettingView
}

// The chip tones, mirroring the brand's fixed status-to-colour mapping. They are
// the class SUFFIXES the template joins onto `tally--`, so every name here appears
// literally in web/static/css/input.css -- which is what the Tailwind standalone
// CLI scans for.
//
// 🔴 A GUARDRAIL'S CHIP IS ITS OWN TONE AND THAT IS A BRAND DECISION, NOT A SPARE
// COLOUR. The lock language and the on/off language have to be tellable apart at a
// glance, or a customer reads the guardrail block as another list of things they
// could switch. `locked` is ink -- the text colour, the one that is not a status --
// so it reads as structure rather than as a state.
const (
	// PolicyToneLocked is the guardrail chip. It is written into the template
	// LITERALLY rather than through this constant (the Tailwind standalone CLI
	// scans templates as raw text and would not follow a Go identifier); the
	// constant exists so a test can name it and so the family is declared in one
	// place. TestPolicyTones_AreClassesTheStylesheetDeclares keeps all five honest.
	PolicyToneLocked = "locked"
	// The four rule states, mapped onto the brand's FIXED status vocabulary:
	// in force is tappa-green, switched off is the neutral line, waiting to be set
	// up is saffron, unreadable is tomato.
	PolicyToneRunning  = "active"
	PolicyToneOff      = "deactivated"
	PolicyToneWaiting  = "queued"
	PolicyToneRefusing = "rejected"
)
