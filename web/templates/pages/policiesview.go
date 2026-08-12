package pages

// The POLICIES section's view model (M6-09 phase A -- the READ side).
//
// 🔴 THERE IS NO SWITCH ON THIS SCREEN, AND THE MODEL IS WHERE THAT IS DECIDED.
// Phase A reads; phase B writes. So there is no CSRFToken here, no form target and
// no per-rule action href -- a template cannot offer a control whose destination
// the view has no field to carry, which is the same wall ReportsView describes.
// One of those absences is more than staging, and it survives phase B:
// PolicyGuardrailView carries nothing a toggle could be built from, because a
// guardrail has no off switch anywhere in the product. The M6-09 card's first trap
// is drawing one as a control that happens to be disabled -- the customer tries it,
// cannot move it, and stops believing the rest of the screen. A type with no field
// for it cannot be persuaded otherwise later.
// TestPolicyGuardrailView_CarriesNoSwitch is the mechanical half.
//
// 🔴 §4.7 -- NOTHING BELOW COULD CARRY A SECRET. The three queries behind this
// screen select no token hash, no coordinate, no key reference and no email, the
// domain types have no field for them, and neither does anything here. What a
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
// two ends of its range. Phase B's editor for it will be a form on its own, and it
// will still not be an on/off switch, because ADR 0004 §11's pattern is "cannot be
// switched off, MAY be tuned inside a range" and the first half is the product.
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
