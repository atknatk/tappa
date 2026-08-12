package handler

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The POLICIES SECTION — M6-09 phase A: the rulebook, read only.
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5). It comes from httpx.AdminOf(r), which the
// Protect chain resolved from a signed session cookie against the database. This
// section takes NO parameter at all — there is no week to choose, no page to walk and
// no id to look up — so the request carries nothing a caller could bend.
//
// 🔴 §4.3 / §4.6 — READ ONLY, AND HERE THAT IS MORE THAN STAGING. There is no entry in
// mountWriting for this section, no ProtectWriting chain, no synchronizer token and no
// audit_log row, because nothing here writes. The point worth stating is WHY that
// needed care: the other assembler of a tenant's policy layer (internal/domain/checkin)
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
// by hand. The three queries select no token hash, no coordinate and no key reference;
// the domain types have no field for them; pages.PoliciesView has none either. The one
// identity rendered is a policy version's author, a name this tenant already owns.
//
// WHO MAY READ IT: any signed-in admin of the business, owner or manager. Editing is
// owner-only and always will be — the guardrail sys:policy-edit-owner-only says so and
// no policy can widen it — but READING the rules you work under is not editing them,
// and a manager who cannot see why a tap was flagged cannot do the job the review
// queue gives them. The screen says which of the two it is.

// panelRules is the policies section's read side, declared HERE at the consumer (§7).
//
// 🔴 ONE METHOD, AND IT RETURNS A SCREEN RATHER THAN TAKING A COMMAND. The interface
// is the narrowest statement of what this section may do: there is no Enable, no
// Disable, no Save and no Materialise on it, so a handler in this package cannot reach
// a write on the policy tables even by accident — and the concrete type behind it
// (internal/domain/tenant.Rulebook) makes only SELECT calls, which its own test proves
// by reading the SQL. Phase B's writer will be a SECOND field on AdminAuth, for the
// reason `queue` and `reviewer` are two: reading the rulebook and rewriting it are
// different authorities and should not travel behind one value.
type panelRules interface {
	Screen(ctx context.Context, tenantID uuid.UUID) (tenant.PolicyScreen, error)
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
	fillPoliciesView(&v, screen)
	a.render(w, r, http.StatusOK, pages.AdminPolicies(v))
}

// fillPoliciesView maps the domain screen onto the view.
//
// EVERY TIME IS CONVERTED HERE AND ONLY HERE (§6: the database is UTC and the render
// layer converts), against the zone the database supplied for this business.
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

	v.Baseline = policyRuleViews(s.Baseline, zone, s.GuardrailsOnly)
	v.Tenant = policyRuleViews(s.Tenant, zone, s.GuardrailsOnly)

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

func policyRuleViews(rules []tenant.Rule, zone *time.Location, guardrailsOnly bool) []pages.PolicyRuleView {
	out := make([]pages.PolicyRuleView, 0, len(rules))
	for _, r := range rules {
		out = append(out, policyRuleView(r, zone, guardrailsOnly))
	}
	return out
}

// policyRuleView maps one rule onto one docket.
func policyRuleView(r tenant.Rule, zone *time.Location, guardrailsOnly bool) pages.PolicyRuleView {
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
// The operator keyword is the document's own vocabulary (M3-03's closed list) and a
// customer editing a rule in phase B will meet it again, so the word is not hidden —
// it is translated. An operator this list does not know falls through as itself.
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
