package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The POLICIES SECTION — M6-09: the rulebook, read here and written in policyactions.go.
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5). It comes from httpx.AdminOf(r), which the
// Protect chain resolved from a signed session cookie against the database. This
// section takes NO parameter at all — there is no week to choose, no page to walk and
// no id to look up — so the request carries nothing a caller could bend.
//
// 🔴 §4.3 / §4.6 — THIS FILE READS; THE WRITES ARE policyactions.go's. Every clause of
// this paragraph was false for two rounds after phase B shipped: there ARE two entries
// in mountWriting for this section, they DO take the ProtectWriting chain, they DO
// carry a synchronizer token (confirmField), and they DO write audit_log rows —
// including for a refusal. What is true of THIS file is that the section's GET renders
// and asks the engine one question (panelScribe.May) and changes nothing. The point
// worth stating is WHY rendering had to stay that way, and the gate reads through
// checkin.Service.StoredSet — the non-materialising path — so even a REFUSED write
// provisions nobody: the other assembler of a tenant's policy layer (internal/domain/checkin)
// MATERIALISES the managed baseline when it finds it missing — a policy, a version and
// an attachment for every shipped document — because a tap that cannot name a policy_versions row
// cannot be recorded. A panel screen that reused it would write those rows for any
// tenant somebody merely looked at, into a table that is append-only. So this section
// reads through internal/domain/tenant's Rulebook, whose store calls are all SELECTs
// and which is held to that by a test that reads the SQL rather than the names.
//
// 🔴 §4.6 — A FAILED READ IS AN ERROR, NEVER AN EMPTY RULEBOOK. If the database does
// not answer this renders a problem page. It must not fall through to a page saying
// "you have no policies": that is a claim about a business, and a timeout is not
// evidence for it.
//
// 🔴 §4.7 — NOTHING SENSITIVE PASSES THROUGH THIS FILE, and there is nothing to strip
// by hand. The queries behind this screen select no token hash, no coordinate and no
// key reference (the count is deliberately not written here: it was "three" while the
// section read six); the domain types have no field for them; pages.PoliciesView has
// none either. The one
// identity rendered is a policy version's author, a name this tenant already owns.
//
// WHO MAY READ IT: any signed-in admin of the business, owner or manager. Editing is
// owner-only and always will be — the guardrail sys:policy-edit-owner-only says so and
// no policy can widen it — but READING the rules you work under is not editing them,
// and a manager who cannot see why a tap was flagged cannot do the job the review
// queue gives them. The screen says which of the two it is.

// panelRules is the policies section's read side, declared HERE at the consumer (§7).
//
// 🔴 IT RETURNS A SCREEN RATHER THAN TAKING A COMMAND. There is no Enable, no Disable,
// no Save and no Materialise on this interface, and the concrete type behind it
// (internal/domain/tenant.Rulebook) makes only SELECT calls, which its own test proves
// by reading the SQL.
//
// ⚠️ IT USED TO SAY A HANDLER IN THIS PACKAGE "CANNOT REACH A WRITE ON THE POLICY
// TABLES EVEN BY ACCIDENT", AND THAT WAS TRUE ONLY WHILE THIS WAS THE ONLY INTERFACE.
// It also predicted, in the future tense, that "phase B's writer will be a SECOND
// field on AdminAuth". Phase B shipped: policyactions.go declares panelScribe and
// AdminAuth carries it, so a write IS reachable from this package — through a second,
// equally narrow interface whose every method takes an Actor and whose concrete type
// asks the policy engine before it opens a transaction. The prediction was right and
// keeping it in the future tense taught the next reader that the wall is still there.
// Two fields for the reason `queue` and `reviewer` are two: reading the rulebook and
// rewriting it are different authorities and must not travel behind one value.
type panelRules interface {
	Screen(ctx context.Context, tenantID uuid.UUID) (tenant.PolicyScreen, error)
	// Version reads ONE stored version's body — phase A's counted gap, closed in
	// phase B. It is on the READ interface because it reads: the bound is the shape
	// of the query (one version is at most policy.DefaultLimits' byte ceiling) rather
	// than a LIMIT, which is why a page of versions still does not carry documents.
	Version(ctx context.Context, tenantID, policyID uuid.UUID, versionNo int) (tenant.VersionBody, error)
}

// policiesHref is the section's own URL, READ FROM THE SECTION TABLE rather than
// written out again. If the section moves, this moves with it; if the tab is removed
// altogether this panics at startup rather than silently pointing at nothing.
//
// ⚠️ NOTHING LINKS TO IT FROM INSIDE THIS FILE — the tab bar builds the link from
// pages.PanelSections. It exists so mountSections and any future link are reading the
// same fact, and so a section that moves cannot leave a stale literal behind.
var policiesHref = mustSectionHref(pages.TabPolicies)

// policiesSection renders the whole section.
//
// 🔴 IT LOADS NO SCRIPT. Everything on this page is text, so the panel's widened
// Content-Security-Policy stays pinned to exactly one url — the cardinality
// TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty asserts.
func (a *AdminAuth) policiesSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)

	screen, err := a.rules.Screen(r.Context(), id.TenantID())
	if err != nil {
		// §4.6: say the read failed. Do NOT fall through to a page with no rules on it.
		a.log.Error("panel: could not read the policy layer", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	v := pages.PoliciesView{PanelChrome: a.chrome(r, pages.TabPolicies)}
	// 🔴 THE ENGINE IS ASKED ONCE PER RENDER, AND IT IS THE SAME QUESTION THE WRITE
	// PATH ASKS. panelScribe.May evaluates `policy:edit` for this admin against the
	// tenant's STORED rulebook (the non-materialising read), so the controls a page
	// offers and the acts the server will accept cannot disagree. Hiding a control is
	// the courtesy; tenant.RuleWriter.authorise is the guarantee, and it runs again on
	// every POST.
	//
	// ⚠️ IT COSTS ONE EXTRA SELECT — the same ListPolicySet the screen already ran, in
	// its own transaction. That is counted rather than optimised away: the alternative
	// is re-deriving the answer from the screen's own parsed rules, which would be a
	// second representation of the engine's decision on the one page whose job is to
	// show what the engine decides.
	v.MayEdit = a.scribe.May(r.Context(), id.TenantID(), actorOf(id))
	v.ProblemHeading, v.Problem = policyProblem(r.URL.Query().Get("problem"))
	fillPoliciesView(&v, screen)
	a.render(w, r, http.StatusOK, pages.AdminPolicies(v))
}

// policyProblemSentence turns a query word into the sentence the page prints, or ""
// when there is nothing to say.
//
// 🔴 THE WORD IS MATCHED AGAINST A CLOSED LIST AND THE SENTENCE IS OURS, so nothing
// a caller can type reaches the page. An unknown word produces no notice at all
// rather than a generic one: a banner that fires for any junk in the query string is
// a banner somebody can make a customer see.
func policyProblem(raw string) (heading, sentence string) {
	word := oneOfWords(strings.TrimSpace(raw), policyProblemWords...)
	sentence = policyProblemSentence(word)
	if sentence == "" {
		return "", ""
	}
	// 🔴 ONE WORD IS NOT A REFUSAL, AND ITS HEADING MUST NOT SAY IT IS.
	// `lockout-stands` means the change WAS made, took the author's authority away, and
	// could not be put back. "That change was not made" over that body is the page's
	// largest sentence contradicting its own paragraph — and the reason
	// tenant.ErrLockoutStands is a separate sentinel in the first place.
	if word == "lockout-stands" {
		return "That change was made, and it locked you out", sentence
	}
	return "That change was not made", sentence
}

func policyProblemSentence(word string) string {
	switch word {
	case "not-permitted":
		// 🔴 IT DOES NOT NAME A ROLE, AND THE SIBLING SENTENCE ON THIS SCREEN ALREADY
		// KNEW WHY. policyIntro's read-only arm was corrected in an earlier round for
		// exactly this: "telling an owner 'only owners may change these' when they ARE
		// the owner would send them looking for the wrong problem". This one kept the
		// role sentence, and the state that reaches it is real — checkin's stored-set
		// read falls back to the guardrails alone when ONE stored baseline document
		// cannot be parsed, which drops the authorization document with it, so an OWNER
		// is refused and lands here. The page then said three different things at once:
		// a document cannot be read (top), there are two reasons (intro), and this is
		// reserved for owners (here).
		return "The rules engine does not grant you the policy:edit authority right now, so " +
			"nothing was changed. There are two reasons that happens and the rules below tell " +
			"them apart: your business's rules may not be set up yet, or they are and this " +
			"authority is not yours — only an organisation owner is ever granted it. If every " +
			"rule below reads “Not set up yet”, or the page says one of them cannot be read, " +
			"that is the reason rather than your role. The attempt has been written to your " +
			"audit trail."
	case "unknown":
		return "That rule is not one of this organisation's, so nothing was changed."
	case "confirm-required", "confirm-stale":
		return "The confirmation step was not completed, or it was left too long — nothing was " +
			"changed. Start again and the warning will be shown afresh."
	case "bad-scope":
		return "That is not a place a rule can be applied to, so nothing was changed."
	case "already-bound":
		return "That rule was already recorded as applying there, so nothing changed."
	case "not-bound":
		return "That rule was not recorded as applying there, so there was nothing to remove."
	case "raced":
		return "Somebody else saved a change to that rule while you were reading it, so yours " +
			"was not applied — two versions with the same number cannot both exist. Look at the " +
			"rule as it is now and decide again."
	case "name-taken":
		return "You already have a rule with that name, and nothing was written. Two rules " +
			"with one name cannot be told apart afterwards and neither can be removed — a " +
			"rule is switched off, never deleted. Either pick a different name, or choose " +
			"“a new version of” that rule instead of “a new rule”."
	case "not-yours":
		return "That is a rule Tappa maintains, so it cannot be rewritten in place. Switch it " +
			"off and write your own version instead — that way its history stays readable for " +
			"the records it decided."
	case "lockout-stands":
		// 🔴 THE OPPOSITE SENTENCE, AND IT HAS TO BE, because on this path the change was
		// NOT put back. Saying "it was put back" here is the one message that would send
		// an owner away believing the reverse of what happened to their rulebook.
		return "That change took away your own authority to change the rules, and putting it " +
			"back FAILED — so it is still in force and you may not be able to change anything " +
			"else from this screen. Nothing here can repair it: a rule cannot be deleted and a " +
			"version cannot be edited. Tell us, with the time you pressed it."
	case "lockout":
		return "That change would have taken away your own authority to change the rules, and " +
			"there is no way back from that through this product — so it was put back. A rule " +
			"cannot be deleted and a version cannot be edited, which is exactly why."
	case "no-such-version":
		return "There is no such version of that rule."
	case "unreadable":
		return "That request could not be read, so nothing was changed."
	case "unavailable":
		return "Something went wrong on our side and nothing was changed. Please try again."
	default:
		return ""
	}
}

// fillPoliciesView maps the domain screen onto the view.
//
// TIMES ON THE SECTION'S OWN PAGE ARE CONVERTED HERE, against the zone the database
// supplied for this business (§6: the database is UTC and the render layer converts).
//
// ⚠️ IT SAID "AND ONLY HERE", WHICH IS NOT TRUE OF THE SECTION AS A WHOLE.
// tenant.Rulebook.Version converts in the DOMAIN, because that read answers a single
// version's page and carries its timestamp already localised. Both use the same zone,
// read in the same transaction as the rows; what §6 forbids is the DATABASE storing
// local time, not a domain read formatting one. The claim is narrowed rather than the
// code moved: moving it would mean the version page's handler re-reading the tenant
// clock for one field.
func fillPoliciesView(v *pages.PoliciesView, s tenant.PolicyScreen) {
	zone := s.Zone
	if zone == nil {
		zone = time.UTC
	}
	v.Queried = s.Queried
	// TWO FLAGS, NOT THEIR OR. See pages.PoliciesView: one sentence for both
	// ceilings was measured telling a customer three false things.
	v.VersionsCapped = s.VersionsCapped
	v.AttachmentsCapped = s.AttachmentsCapped
	v.ShippedBaseline = s.ShippedBaseline
	v.GuardrailsOnly = s.GuardrailsOnly
	v.Unreadable = s.Unreadable
	for _, r := range s.Baseline {
		if r.State == tenant.StateActive {
			v.BaselineInForce = true
			break
		}
	}
	// 🔴 NOTHING IS "IN FORCE" WHEN THE ENGINE HAS DROPPED THE LAYER. See
	// pages.PoliciesView.GuardrailsOnly: one unreadable document silences the
	// baseline AND the tenant layer, so a rule that is stored, enabled and valid is
	// still deciding nothing. The chip has to say that, because the chip is what a
	// reader takes away from a page of nine rules.
	if s.GuardrailsOnly {
		v.BaselineInForce = false
	}

	v.Guardrails = make([]pages.PolicyGuardrailView, 0, len(s.Guardrails))
	for _, g := range s.Guardrails {
		row := pages.PolicyGuardrailView{
			Order:   strconv.Itoa(g.Order),
			Sid:     g.Sid,
			Effect:  g.Effect,
			Outcome: effectSentence(g.Effect),
			Reason:  g.Reason,
			Rule:    g.Rule,
		}
		if g.Bounded != nil {
			row.Tunable = g.Bounded.Label
			row.Current = g.Bounded.Current
			row.Range = g.Bounded.Min + " and " + g.Bounded.Max
		}
		v.Guardrails = append(v.Guardrails, row)
	}

	// THE CONTROLS ARE BUILT ONCE AND HANDED TO EVERY RULE, so the scope picker and
	// the label resolver are one construction rather than one per rule.
	ed := policyEditor{may: v.MayEdit, action: policyChangeHref}
	v.ScopeCapped = s.ScopeCapped
	ed.names = map[string]string{"*": "every venue and department"}
	for _, t := range s.ScopeTargets {
		option := pages.PolicyScopeOptionView{Value: t.Resource(), Label: t.Name + " (" + t.Kind + ")"}
		ed.names[option.Value] = option.Label
		ed.scopes = append(ed.scopes, option)
		if t.Kind == "location" {
			v.ScopeTargets = append(v.ScopeTargets, pages.PolicyScopeOptionView{
				Value: t.ID.String(), Label: t.Name,
			})
		}
	}
	// "*" IS OFFERED FIRST because it is the binding every shipped rule already has,
	// and a picker that only listed venues would make tenant-wide look unreachable.
	ed.scopes = append([]pages.PolicyScopeOptionView{
		{Value: "*", Label: ed.names["*"]},
	}, ed.scopes...)
	if v.MayEdit {
		// THE CUSTOMER'S OWN RULES, offered as "a new version of this one". A rule with
		// no stored id cannot be superseded, and a MANAGED rule must not be (that is
		// ErrNotYours in the domain), so the list is the tenant layer with an id.
		for _, r := range s.Tenant {
			if r.PolicyID != uuid.Nil {
				v.Supersedable = append(v.Supersedable, pages.PolicyScopeOptionView{
					Value: r.PolicyID.String(), Label: "A new version of “" + r.Name + "”",
				})
			}
		}
		// 🔴 THE SECTION'S OWN FORM TARGET. Leaving it empty was a REAL DEFECT and the
		// route net caught it: a <form> with an empty action posts to the CURRENT url,
		// so the authoring form would have posted to GET /admin/policies and answered
		// 405 — a button that looks like it works and does nothing, which is the shape
		// this section's tests exist for.
		v.Action = policyChangeHref
		for _, doc := range tenant.AuthorableRules() {
			v.Authorable = append(v.Authorable, pages.PolicyScopeOptionView{
				Value: doc.Name, Label: doc.Name,
			})
		}
	}
	for _, set := range s.Settings {
		v.Settings = append(v.Settings, pages.PolicySettingView{
			Label: set.Label, Current: set.Current, Range: set.Min + " and " + set.Max, Uses: set.Uses,
		})
	}

	v.Baseline = policyRuleViews(s.Baseline, zone, s.GuardrailsOnly, ed)
	v.Tenant = policyRuleViews(s.Tenant, zone, s.GuardrailsOnly, ed)

	for _, auth := range s.Authorities {
		row := pages.PolicyAuthorityView{
			Action:       auth.Action,
			Default:      auth.Default,
			DefaultLabel: defaultSentence(auth.Default),
			Guardrail:    auth.GuardrailSid,
		}
		for _, g := range auth.Grants {
			row.Grants = append(row.Grants, grantLine(g))
		}
		if auth.FailClosed {
			v.Authorities = append(v.Authorities, row)
			continue
		}
		v.Recording = append(v.Recording, row)
	}
}

// grantLine renders one grant, WITH its fence.
//
// 🔴 IT USED TO BE "role — sid" AND THAT OVERSTATED. A grant is an IAM statement:
// it has a resource list and a condition block, and the role is only the part of
// the condition that keys on actor:role. A statement scoped to one branch and
// conditioned on a channel printed as a flat grant of the whole action, so a
// customer reading it would believe they had handed out an authority they had
// actually fenced twice. The scope travels with the line, and a condition this
// summary cannot name is counted rather than dropped — the full statement is
// printed further up the page, so the count is a pointer, not a substitute.
func grantLine(g tenant.AuthorityGrant) string {
	line := g.Role + " — " + g.Sid
	if len(g.Resources) > 0 {
		line += " · where: " + joinDot(g.Resources)
	}
	if g.OtherConditions > 0 {
		line += " · " + plural(g.OtherConditions, "further condition", "further conditions")
	}
	return line
}

func joinDot(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += " · "
		}
		out += s
	}
	return out
}

// policyEditor is what the write side contributes to a rule's view: whether the
// engine allows this admin to change anything, where the controls post, the scope
// picker's options and the id-to-name resolver for a binding.
//
// 🔴 `may` IS THE ENGINE'S ANSWER, ASKED ONCE. It is not a role and it is not
// recomputed per rule: a per-rule recomputation would be a second representation of
// one decision, and the two would disagree the moment a rule changed mid-render.
type policyEditor struct {
	may    bool
	action string
	scopes []pages.PolicyScopeOptionView
	names  map[string]string
}

// label resolves a resource to the customer's own words, keeping the RAW string when
// it resolves to nothing.
//
// 🔴 AN UNRESOLVABLE BINDING IS SHOWN, NOT HIDDEN. 00007 puts no foreign key on
// policy_attachments.resource (the column is free-form text on purpose), so a venue
// removed after a rule was bound to it leaves a binding that names nothing. Dropping
// it from the list would leave a rule scoped to a place that does not exist with
// nothing on screen about it.
func (e policyEditor) label(resource string) string {
	if name, ok := e.names[resource]; ok {
		return name
	}
	return resource + " (no longer in this organisation)"
}

func policyRuleViews(rules []tenant.Rule, zone *time.Location, guardrailsOnly bool, ed policyEditor) []pages.PolicyRuleView {
	out := make([]pages.PolicyRuleView, 0, len(rules))
	for _, r := range rules {
		out = append(out, policyRuleView(r, zone, guardrailsOnly, ed))
	}
	return out
}

// policyControls fills in one rule's write side, or leaves it empty.
//
// 🔴 A RULE WITH NO STORED ROW GETS NO CONTROLS, AND THAT IS A COUNTED BOUND. A
// shipped baseline document this business has never had materialised has no
// `policies` row, so there is nothing to switch — and the panel deliberately does
// NOT materialise one (see tenant.RuleWriter.SetEnabled: provisioning is M7-03's
// job and the tap path's fallback, and a second writer of the managed baseline would
// need a second copy of the id derivation). The rule reads "Not set up yet" and says
// so; it does not offer a button that would answer "no such policy".
func policyControls(row *pages.PolicyRuleView, r tenant.Rule, ed policyEditor) {
	if r.PolicyID == uuid.Nil {
		return
	}
	row.ID = r.PolicyID.String()
	if !ed.may {
		return
	}
	row.Action = ed.action
	// THE LABEL NAMES THE OUTCOME rather than the current state: a button labelled
	// with the state it is in is one people press to reach that state.
	if r.State == tenant.StateOff {
		row.Switch = pages.PolicySwitchView{
			Op: opEnable, Label: "Switch this rule back on",
			Note: "It takes part in every decision again, from the next tap.",
		}
	} else {
		row.Switch = pages.PolicySwitchView{
			Op: opDisable, Label: "Switch this rule off",
			Note: "It is not deleted — its history stays, so records decided under it can " +
				"still explain themselves.",
		}
	}
	bound := map[string]bool{}
	for _, a := range r.Attachments {
		bound[a] = true
		row.Unbind = append(row.Unbind, pages.PolicyBindingView{Resource: a, Label: ed.label(a)})
	}
	// AN OPTION ALREADY BOUND IS LEFT OUT: offering it would be offering a press whose
	// only outcome is "that was already true".
	for _, o := range ed.scopes {
		if !bound[o.Value] {
			row.Bindable = append(row.Bindable, o)
		}
	}
}

// policyRuleView maps one rule onto one docket.
func policyRuleView(r tenant.Rule, zone *time.Location, guardrailsOnly bool, ed policyEditor) pages.PolicyRuleView {
	label, tone := stateWords(r.State)
	// 🔴 THE ONE OVERRIDE ON THIS PAGE, AND IT IS THE BLOCKING CORRECTION. When one
	// stored document cannot be read the engine drops the WHOLE layer, so a rule
	// that is stored, enabled and valid is not deciding anything — and "In force"
	// beside it would be the page's most confident lie. The word changes; the state
	// underneath does not, because the STORAGE really is fine and phase B's controls
	// will still act on it.
	if guardrailsOnly && r.State == tenant.StateActive {
		label, tone = "Stored — but not deciding today", pages.PolicyToneRefusing
	}
	row := pages.PolicyRuleView{
		Name:        r.Name,
		State:       string(r.State),
		StateLabel:  label,
		StateTone:   tone,
		Managed:     r.Managed,
		Stamp:       r.StoredStamp,
		Problem:     r.Problem,
		Attachments: r.Attachments,
	}
	if r.VersionNo > 0 {
		row.Version = "version " + strconv.Itoa(r.VersionNo)
	}
	// 🔴 THE TWO STAMPS DIFFERING IS A DESIGNED STATE, NOT AN ERROR, so it is a
	// sentence rather than a warning. Existing businesses are deliberately never
	// auto-updated when a new baseline ships: a silent change would rewrite why past
	// taps were flagged, and every record pins the version that judged it. Saying
	// nothing at all would be worse — the customer would have no way to know a newer
	// default exists.
	if r.ShippedStamp != "" && r.StoredStamp != "" && r.ShippedStamp != r.StoredStamp {
		row.Outdated = "You are running the " + r.StoredStamp + " version of this rule; " +
			r.ShippedStamp + " has since shipped. Nothing changes until you accept it, on " +
			"purpose — a rule that changed under you would change what past taps meant."
	}
	for _, st := range r.Statements {
		s := pages.PolicyStatementView{
			Sid:       st.Sid,
			Effect:    st.Effect,
			Outcome:   effectSentence(st.Effect),
			Actions:   st.Actions,
			Resources: st.Resources,
			Reason:    st.Reason,
		}
		for _, c := range st.Conditions {
			s.Conditions = append(s.Conditions, c.Key+" "+conditionVerb(c.Operator)+" "+c.Value)
		}
		row.Statements = append(row.Statements, s)
	}
	for _, h := range r.History {
		version := pages.PolicyVersionView{
			No:     "version " + strconv.Itoa(h.No),
			At:     h.At.In(zone).Format("2 Jan 2006 15:04"),
			Author: h.Author,
			Bytes:  strconv.Itoa(h.Bytes) + " bytes",
		}
		// 🔴 EVERY VERSION GETS ITS OWN ADDRESS, NOT JUST THE NEWEST. Rulebook.Version
		// takes any version number, and the bound is the shape of the query (one row) —
		// so there is nothing to gain from offering only the current one, and everything
		// to lose: the strip's own sentence says an earlier version's rules are on a page
		// of their own, and until this line that page could only be reached by editing
		// the URL.
		if r.PolicyID != uuid.Nil {
			version.Href = policyVersionHref + "?policy=" + r.PolicyID.String() +
				"&no=" + strconv.Itoa(h.No)
		}
		if h.Current {
			// 🔴 "IN USE" IS A CLAIM ABOUT THE ENGINE, NOT ABOUT THE ROW. When one
			// document cannot be read the engine is using NO version of anything, so a
			// green "in use" chip beside every history line is the same false confidence
			// the "In force" label was -- one element further down, and it survived the
			// first fix because the test looked for one string.
			if guardrailsOnly {
				version.Marker, version.MarkerTone = "newest stored", pages.PolicyToneOff
			} else {
				version.Marker, version.MarkerTone = "in use", pages.PolicyToneRunning
			}
		}
		row.History = append(row.History, version)
	}
	if r.HistoryTotal > len(r.History) {
		row.HistoryNote = "Showing the " + strconv.Itoa(len(r.History)) + " newest of " +
			strconv.Itoa(r.HistoryTotal) + " versions. None of them can be deleted."
	}
	policyControls(&row, r, ed)
	return row
}

// stateWords turns a rule state into the sentence AND the chip tone.
//
// 🔴 THE LABEL CARRIES THE MEANING AND THE TONE ONLY REINFORCES IT (skill
// tappa-brand): status is never told by colour alone. The default arm is a
// deliberate fail-closed: an unknown state must not render as "running", because
// the one thing this screen must never do is show a rule as in force when it is
// not (ADR 0004 calls that the most dangerous failure there is).
func stateWords(s tenant.RuleState) (label, tone string) {
	switch s {
	case tenant.StateActive:
		return "In force", pages.PolicyToneRunning
	case tenant.StateOff:
		return "Switched off by you", pages.PolicyToneOff
	case tenant.StateNotProvisioned:
		return "Not set up yet", pages.PolicyToneWaiting
	case tenant.StateNoDocument:
		return "Set up, but has no rules yet", pages.PolicyToneWaiting
	case tenant.StateUnknown:
		// The version read hit its cap, so the row that tells "never set up" from
		// "set up with no rules" may have been truncated. Those two need different
		// acts, so the honest answer is that this reader cannot tell — not the more
		// comfortable of the two.
		return "Cannot be determined on this page", pages.PolicyToneWaiting
	case tenant.StateUnreadable:
		return "Stored, but cannot be read", pages.PolicyToneRefusing
	default:
		return "Not in force", pages.PolicyToneRefusing
	}
}

// effectSentence says, in words, what an effect does.
//
// The keyword is shown beside it because that is what a docket carries (a REJECTED
// card names the sid and the layer), so a manager holding a refused record can find
// the rule. The sentence is for everyone else. An unknown keyword falls through as
// itself rather than being dropped: a word nobody translated is better than a blank.
//
// 🔴 THE SENTENCES SAY "IT", NOT "THE TAP", AND THAT IS A CORRECTION MADE ON THE
// RENDERED PAGE RATHER THAN A STYLE CHOICE. The first version read "Refuses the tap"
// for every deny, and the page then told a customer that sys:policy-edit-owner-only
// and sys:no-self-review — the two guardrails that have nothing to do with a tap —
// refuse taps. The engine is general-purpose (its actions are namespaced for exactly
// that reason), so a sentence that assumes the action is false on two of the ten
// guardrails and on every authorization statement. What "it" is is never left to
// guessing: each statement prints its own "Applies to" line beside this.
func effectSentence(effect string) string {
	switch effect {
	case "allow":
		return "Lets it through."
	case "review":
		return "Records it and sends it to your review queue."
	case "deny":
		return "Refuses it. The attempt is still written down."
	case "ignore":
		return "Records it but counts nothing — a duplicate."
	case "redirect":
		return "Writes no record and sends the person to the activation page."
	default:
		return effect
	}
}

// defaultSentence says what happens when no policy matches.
func defaultSentence(effect string) string {
	switch effect {
	case "deny":
		return "Refused unless granted"
	case "review":
		return "Sent for review"
	default:
		return effect
	}
}

// conditionVerb turns an operator into a readable comparison.
//
// The operator keyword is the document's own vocabulary (M3-03's closed list), so the
// word is translated rather than hidden. An operator this list does not know falls
// through as itself.
//
// ⚠️ IT USED TO SAY A CUSTOMER "editing a rule in phase B WILL meet it again". Phase B
// shipped and they do not: its form offers a name, a source document and a set of
// venues, and the operators come from the copied document. A raw editor where they
// would meet one is M9-07.
func conditionVerb(op string) string {
	switch op {
	case "StringEquals", "NumericEquals", "Bool":
		return "is"
	case "StringNotEquals":
		return "is not"
	case "StringIn":
		return "is one of"
	case "NumericLessThan":
		return "is below"
	case "NumericGreaterThan":
		return "is above"
	case "IpInPrefix":
		return "is inside"
	case "Exists":
		return "is present:"
	case "TimeBetween":
		return "is between"
	default:
		return op
	}
}
