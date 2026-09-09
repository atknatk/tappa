package handler

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The PINS FOR THE REST OF THE LANDING PAGE'S CLAIM-CARRYING BLOCKS.
//
// 🔴 THIS FILE EXISTS BECAUSE A FALSE SENTENCE SHIPPED THROUGH THE GAP IT CLOSES.
// pages/landingview.go declares seven claim-carrying values — LandingSteps,
// LandingTapFlow, LandingRules, LandingEvidence, LandingComparison, LandingFAQ and
// LandingAudiences — and until now only two of them were held against anything:
// LandingComparison (TestLanding_ComparesMechanismsAndDeclaresNoWinner) and
// LandingAudiences (the two TestLandingAudiences_* tests in marketing_test.go). The
// other five could gain a line, lose a line or stop being rendered at all with the
// package green. LandingRules line three did exactly that: it read "it is the one
// path in the product that writes no record", which is false — sys:tenant-mismatch
// writes none either, and so do three more tap refusals — and nothing was red,
// because nothing read the slice.
//
// 🔴 THE TWO INVARIANTS ARE THE ONES marketing_test.go ALREADY ESTABLISHED FOR
// LandingAudiences, and they are deliberately the same two:
//
//   - EVERY LINE ACTUALLY REACHES A VISITOR. Text-matching is legitimate here — the
//     question is "did this exact sentence get rendered", not "is it true" — and it
//     is what catches a block that was edited in Go and dropped from the template,
//     or a template that renders a stale subset.
//   - EVERY LINE RESTS ON A PRODUCT FACT THAT IS STILL THERE. pages.Source is the
//     vocabulary for the two mechanism blocks (LandingRules, LandingTapFlow), the
//     counterpart of pages.Anchor for LandingAudiences. Each Source is DERIVED from
//     the policy engine, the generated query source, the schema or reflection — never
//     compared against expected copy, because a hand-kept list of expected sentences
//     agrees with whatever is written beside it, including a lie.
//
// ⚠️ THE LIMIT IS THE ONE WRITTEN ON pages.Anchor, WORD FOR WORD. Naming a Source
// does not make a sentence true and does not check that the sentence is ABOUT the
// fact it names. This is a RATCHET AGAINST DRIFT: it fails when a capability
// DISAPPEARS or a line stops being rendered. It is structurally blind to a sentence
// bolted onto an unrelated but healthy Source. A new sentence still has to be
// checked by a person.
//
// ⚠️ AND THREE OF THE FIVE GET ONLY THE FIRST INVARIANT. LandingSteps,
// LandingEvidence and LandingFAQ carry no Sources field — adding one is an edit to
// landingview.go, which this task does not own — so they are pinned for RENDERING
// only. That is stated rather than hidden: a step whose product basis vanishes is
// still not caught here.

// --- the shared render helper ----------------------------------------------

// renderedLandingText is the landing page as a visitor perceives it: fetched
// through the REAL router and flattened by the same screenText every other screen
// test in this package uses, so text smuggled into an attribute still counts and
// markup never does.
func renderedLandingText(t *testing.T) string {
	t.Helper()
	return screenText(t, mustFetchMarketing(t, marketingRouter(t), "/"))
}

// renderedInOrder asserts that every line in want appears in text, once the block
// has started, IN THE DECLARED ORDER, and returns where each one landed (-1 for a
// line that is missing).
//
// 🔴 THE ORDER IS CHECKED AND NOT JUST THE PRESENCE, because for LandingRules the
// order IS the claim: the page prints "the first one that matches wins" over a
// numbered list, so a template that rendered the same seven lines shuffled would be
// making a different and wrong statement while a Contains-only test stayed green.
func renderedInOrder(t *testing.T, text, what string, want []string) []int {
	t.Helper()
	at := make([]int, len(want))
	from := 0
	for i, w := range want {
		idx := indexFrom(text, w, from)
		if idx < 0 {
			at[i] = -1
			// Say WHICH failure it is: not on the page at all, or on the page in the
			// wrong place. They have different fixes.
			if strings.Contains(text, w) {
				t.Errorf("%s: %q is rendered, but BEFORE the line that is supposed to precede "+
					"it. The order of this block is part of what it claims.", what, w)
			} else {
				t.Errorf("%s: %q is declared in pages and is NOT rendered, so nothing that pins "+
					"it guards anything.", what, w)
			}
			continue
		}
		at[i] = idx
		from = idx + len(w)
	}
	return at
}

// --- LandingRules: rendering ------------------------------------------------

// landingRulesEndMarker is the first sentence AFTER the decision list. It bounds
// the last rule's segment so the stamp scan below does not run on into the FAQ,
// which legitimately contains the word FLAGGED in prose.
const landingRulesEndMarker = "What nobody can switch off"

// landingStampWords maps every stamp in the closed vocabulary to the word the
// template prints for it. It is the test's copy of landingVerdict's switch, and
// that duplication is the point: the switch is what would be deleted.
var landingStampWords = map[pages.Stamp]string{
	pages.StampApproved: "APPROVED",
	pages.StampFlagged:  "FLAGGED",
	pages.StampRejected: "REJECTED",
	pages.StampIgnored:  "IGNORED",
	pages.StampNone:     "no record",
}

// TestLandingRules_EveryLineIsRenderedInOrderWithItsStamp is the pin the block that
// shipped a false sentence did not have.
//
// It checks four things: the list is the size the page says it is, every line's
// condition and body reach the visitor, the lines arrive in the declared order, and
// each line's own verdict — and no other — is stamped beside it.
func TestLandingRules_EveryLineIsRenderedInOrderWithItsStamp(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)

	// ANTI-VACUITY, AND ALSO A CLAIM IN ITS OWN RIGHT: the section's printed
	// subtitle says "Seven questions, in order". A block that grew or shrank without
	// that sentence being reconsidered would be a page that miscounts itself.
	if n := len(pages.LandingRules); n != 7 {
		t.Fatalf("LandingRules carries %d line(s); CLAUDE.md §5's decision order has seven "+
			"and the page prints the word \"Seven\" over the list. Change both or neither.", n)
	}
	if !strings.Contains(text, "Seven questions") {
		t.Error("the decision section no longer prints \"Seven questions\", so the count " +
			"asserted above is no longer the count the visitor is given")
	}

	when := make([]string, len(pages.LandingRules))
	for i, r := range pages.LandingRules {
		when[i] = r.When
	}
	at := renderedInOrder(t, text, "LandingRules", when)

	end := strings.Index(text, landingRulesEndMarker)
	if end < 0 {
		t.Fatalf("the sentence %q that closes the decision list is gone; the stamp scan below "+
			"has no upper bound and would read the rest of the page", landingRulesEndMarker)
	}

	for i, r := range pages.LandingRules {
		if want := strconv.Itoa(i + 1); r.Ordinal != want {
			t.Errorf("rule %d is printed with the ordinal %q; the list is numbered and the "+
				"number is part of the claim, so it must be %q", i+1, r.Ordinal, want)
		}
		if at[i] < 0 {
			continue // already reported
		}
		bodyAt := indexFrom(text, r.Body, at[i])
		if bodyAt < 0 {
			t.Errorf("rule %d's condition %q is rendered but its body is not:\n    %q\n"+
				"Half a line on the page is a claim with its limit removed.", i+1, r.When, r.Body)
			continue
		}
		// The stamp sits after the body and before the next line's condition (or
		// before the sentence that closes the list, for the last one).
		segEnd := end
		if i+1 < len(at) && at[i+1] >= 0 && at[i+1] < segEnd {
			segEnd = at[i+1]
		}
		segStart := bodyAt + len(r.Body)
		if segStart > segEnd {
			t.Errorf("rule %d's body ends after the next line begins; the page is not laid "+
				"out the way this scan assumes and the stamp check below means nothing", i+1)
			continue
		}
		seg := text[segStart:segEnd]

		wantWord, ok := landingStampWords[r.Verdict]
		if !ok {
			t.Errorf("rule %d ends in the verdict %q, which is not in the Stamp vocabulary "+
				"landingVerdict switches on — it would render as the default", i+1, r.Verdict)
			continue
		}
		if !strings.Contains(seg, wantWord) {
			t.Errorf("rule %d (%q) declares the verdict %q and the page does not stamp %q "+
				"beside it.\nGot: %q", i+1, r.When, r.Verdict, wantWord, seg)
		}
		// AND NO OTHER STAMP, which is what catches a switch that fell through to the
		// wrong branch rather than to none.
		for stamp, word := range landingStampWords {
			if stamp == r.Verdict {
				continue
			}
			if strings.Contains(seg, word) {
				t.Errorf("rule %d (%q) declares %q but the page also stamps %q beside it; a "+
					"line cannot end in two verdicts", i+1, r.When, wantWord, word)
			}
		}
	}
}

// --- LandingTapFlow: rendering ----------------------------------------------

// TestLandingTapFlow_EveryStageIsRenderedInOrder pins the other mechanism block.
//
// The four stages are a SEQUENCE — the chip writes a code, the phone opens a page,
// the server checks the signature, the counter refuses a repeat — so a stage that
// went missing, or arrived out of order, would describe a proof that does not hold
// together.
func TestLandingTapFlow_EveryStageIsRenderedInOrder(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)

	// ANTI-VACUITY: landingview.go's own comment enumerates the four stages 01-04
	// and the template lays them out four to a row.
	if n := len(pages.LandingTapFlow); n != 4 {
		t.Fatalf("LandingTapFlow carries %d stage(s); the block that describes how a tap "+
			"proves itself has four, and its doc comment names a source for each", n)
	}

	titles := make([]string, len(pages.LandingTapFlow))
	for i, s := range pages.LandingTapFlow {
		titles[i] = s.Title
	}
	at := renderedInOrder(t, text, "LandingTapFlow", titles)

	for i, s := range pages.LandingTapFlow {
		if want := fmt.Sprintf("%02d", i+1); s.Ordinal != want {
			t.Errorf("stage %d is printed with the ordinal %q, want %q — the stages are a "+
				"sequence and the number says so", i+1, s.Ordinal, want)
		}
		if at[i] < 0 {
			continue // already reported
		}
		if indexFrom(text, s.Body, at[i]) < 0 {
			t.Errorf("stage %d's title %q is rendered and its body is not:\n    %q",
				i+1, s.Title, s.Body)
		}
	}
}

// --- the Source derivations -------------------------------------------------

// sourceEffects is the effect each policy-backed Source CLAIMS the engine carries.
//
// 🔴 IT IS THE EXPECTED VALUE AND NOT A READBACK, which is the whole reason it
// catches anything. A derivation that read policy.Guardrails and asserted "the
// effect is whatever the engine says" would pass over a guardrail flipped from deny
// to allow. These are the effects the PAGE's stamps and sentences depend on, so a
// baseline or guardrail that changed one fails here with the sid still present —
// exactly what landingview.go's Source block promises.
var sourceEffects = map[pages.Source]policy.Effect{
	pages.SourceTagNotActive:        policy.EffectDeny,
	pages.SourceSUNInvalid:          policy.EffectDeny,
	pages.SourceNoSession:           policy.EffectRedirect,
	pages.SourceEmployeeDeactivated: policy.EffectDeny,
	pages.SourcePersonDebounce:      policy.EffectIgnore,
	pages.SourceIPOrGPSOK:           policy.EffectAllow,
	pages.SourceGPSOnlyAllow:        policy.EffectAllow,
	pages.SourceQRRequiresIP:        policy.EffectReview,
	pages.SourceNoEvidenceReview:    policy.EffectReview,
}

// effectStamp maps a policy effect to the stamp a page may print for it. It is
// ADR 0004 §2's five effects against landingVerdict's five branches.
var effectStamp = map[policy.Effect]pages.Stamp{
	policy.EffectAllow:    pages.StampApproved,
	policy.EffectReview:   pages.StampFlagged,
	policy.EffectDeny:     pages.StampRejected,
	policy.EffectIgnore:   pages.StampIgnored,
	policy.EffectRedirect: pages.StampNone,
}

// policySid returns the sid a Source names and whether it is a policy Source at
// all. The "policy:" prefix is the vocabulary landingview.go writes.
func policySid(s pages.Source) (string, bool) {
	sid, ok := strings.CutPrefix(string(s), "policy:")
	return sid, ok
}

// counterGuardRE is the strict comparison that makes a replayed link lose
// (CLAUDE.md §4.4). Written to match `<` and NOT `<=`: with `<=` the SAME counter
// passes twice, which is replay itself.
var counterGuardRE = regexp.MustCompile(`old_ctr\s*<\s*@ctr`)

// counterGuardIsStrict reports whether a query body advances the counter under a
// strict comparison. Package level so the negative control can prove it says no.
func counterGuardIsStrict(query string) bool { return counterGuardRE.MatchString(query) }

// tapRouteRE is the registration that makes the tap page a web page at a route.
var tapRouteRE = regexp.MustCompile(`r\.Get\(\s*TapPath\s*,`)

// foreignTenantIsRefused reports why the landing page's line three can no longer
// say what it says about a phone signed in to ANOTHER organisation, or "" when it
// still holds. Package level so the negative control can prove it says no.
//
// 🔴 IT IS THE PIN FOR THE HALF OF LINE THREE THAT WAS FALSE AND UNHELD. The line
// used to read "turned away the same way", which equated two paths the handler
// pulls apart on purpose: sys:no-session ends in the activation redirect, and
// sys:tenant-mismatch must NOT, because sending a stranger's phone to activate is
// how one tenant's venue becomes visible to another tenant's employee (§4.5). The
// sid the line named said nothing about the second path, so nothing was red.
//
// ⚠️ WHAT IT DOES NOT DO. It reads the handler's SOURCE, so it bites when the arm
// is rewritten and not when the arm stops being reached; and "words that name
// neither employer" is derived only in the sense that the arm renders a FIXED
// screen declared in this package, which is what makes the words the same for
// everyone. Behaviour end to end is checked by
// TestCheckinDB_ForeignTenantTapIsRefusedAndWritesNOTHING, which needs a database.
func foreignTenantIsRefused(foreignArm, activationArm string) string {
	switch {
	case strings.Contains(foreignArm, "redirectToActivation"):
		return "internal/handler now answers a tap on ANOTHER organisation's plaque with " +
			"the activation redirect. That is the behaviour CLAUDE.md §4.5 refuses and the " +
			"exact sentence line three was corrected for; fix the handler or delete the line"
	case !strings.Contains(foreignArm, "http.StatusForbidden"):
		return "the foreign-tenant arm no longer answers with a refusal status, so \"the " +
			"tap is refused at the button\" is not what the visitor gets"
	case !strings.Contains(foreignArm, "tapProblemForeignTenant"):
		return "the foreign-tenant arm no longer renders the fixed refusal screen, so the " +
			"words are no longer the same for every tenant and the page cannot claim they " +
			"name neither employer"
	case !strings.Contains(activationArm, "redirectToActivation"):
		return "the activation outcome no longer redirects to the activation page, so the " +
			"contrast line three draws between the two record-less paths has gone"
	}
	return ""
}

// goSwitchArm returns the STATEMENTS of one `case <label>:` arm of a Go switch,
// with line comments removed and blank lines dropped.
//
// 🔴 THE COMMENTS COME OFF FOR THE REASON sqlcQueryBody RECORDS, and the arm read
// here is the case that makes it concrete: checkin.go's foreign-tenant branch
// carries a paragraph naming the very identifiers this scan looks for. A scan
// satisfied by a comment ABOUT the code pins nothing.
//
// ⚠️ THE STRIP IS LINE-LOCAL AND KNOWS NOTHING ABOUT STRING LITERALS, exactly as
// sqlcQueryBody's is. A `//` inside a quoted string would be cut as a comment;
// neither arm read here holds one, and the alternative is go/parser for two calls.
func goSwitchArm(t *testing.T, src, label string) string {
	t.Helper()
	start := regexp.MustCompile(`(?m)^[ \t]*case[ \t]+` + regexp.QuoteMeta(label) + `[ \t]*:[ \t]*$`).
		FindStringIndex(src)
	if start == nil {
		t.Fatalf("no `case %s:` arm in internal/handler/checkin.go; the source built on it "+
			"cannot be derived", label)
	}
	rest := src[start[1]:]
	if next := regexp.MustCompile(`(?m)^[ \t]*(case[ \t]|default[ \t]*:|\})`).
		FindStringIndex(rest); next != nil {
		rest = rest[:next[0]]
	}
	var kept []string
	for _, line := range strings.Split(rest, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	arm := strings.Join(kept, "\n")
	// ANTI-VACUITY: an empty arm satisfies no scan, so it would report a healthy
	// handler as broken — or, on a scan phrased the other way, agree with anything.
	if arm == "" {
		t.Fatalf("the `case %s:` arm extracted to NOTHING; the scan built on it is reading "+
			"nothing", label)
	}
	return arm
}

// sqlcQueryBody returns the EXECUTABLE body of one named sqlc query: from its
// `-- name:` line to the statement's terminating semicolon, with every SQL comment
// removed.
//
// IT IS SCOPED TO ONE QUERY ON PURPOSE: db/queries/tags.sql holds a dozen
// statements, and a whole-file scan for a strict comparison would be satisfied by
// any of them.
//
// 🔴 AND THE COMMENTS ARE STRIPPED BECAUSE LEAVING THEM IN MADE THE SCAN VACUOUS,
// MEASURED. AdvanceTagCounter's header explains the guard in prose and quotes it:
// "On replay (equal or smaller ctr) prev.old_ctr < @ctr is false". Changing the
// real `<` to `<=` in the statement left that sentence untouched, so the first
// version of this scan stayed GREEN over a query that had lost CLAUDE.md §4.4's
// replay defence. A scanner satisfied by a comment ABOUT the code is the exact
// failure mode every scanner in marketing_test.go carries a negative control for.
//
// 🔴 THE COMMENTS COME OFF BEFORE THE TERMINATOR IS LOOKED FOR, AND THAT ORDER IS
// THE SECOND BUG THIS FUNCTION HAD. Cutting at the first `;` and stripping comments
// afterwards ended the body INSIDE AdvanceTagCounter's header, which contains a
// semicolon in prose ("under the statement-start snapshot; the UPDATE joins it
// back") — leaving an EMPTY body that satisfies no scan and fails on a healthy
// query. Comments first, statement second, and an empty result is fatal.
//
// ⚠️ THE STRIP IS LINE-LOCAL AND KNOWS NOTHING ABOUT STRING LITERALS. A `--` inside
// a quoted string would be cut as a comment. There is none in db/queries, and the
// alternative is an SQL parser for one comparison.
func sqlcQueryBody(t *testing.T, sql, name string) string {
	t.Helper()
	start := regexp.MustCompile(`(?m)^--\s*name:\s*` + regexp.QuoteMeta(name) + `\s+:\w+\s*$`).
		FindStringIndex(sql)
	if start == nil {
		t.Fatalf("no sqlc query named %s in db/queries/tags.sql; the source built on it "+
			"cannot be derived", name)
	}
	rest := sql[start[1]:]
	if next := regexp.MustCompile(`(?m)^--\s*name:`).FindStringIndex(rest); next != nil {
		rest = rest[:next[0]]
	}
	var kept []string
	for _, line := range strings.Split(rest, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	body := strings.Join(kept, "\n")
	if i := strings.Index(body, ";"); i >= 0 {
		body = body[:i]
	}
	// ANTI-VACUITY: an empty body satisfies no scan, so it would report a healthy
	// query as broken — or, on a scan phrased the other way, agree with anything.
	if strings.TrimSpace(body) == "" {
		t.Fatalf("the sqlc query %s extracted to an EMPTY statement; the scan built on it "+
			"is reading nothing", name)
	}
	return body
}

// sourceDerivations maps every pages.Source to a check that READS THE PRODUCT,
// returning "" when the fact is present or the reason it is not.
//
// 🔴 NOT ONE OF THEM COMPARES A SENTENCE — the same rule anchorDerivations is
// written under, for the same reason.
func sourceDerivations(t *testing.T) map[pages.Source]func() string {
	t.Helper()

	guardOrder := map[string]int{}
	guardEffect := map[string]policy.Effect{}
	for i, g := range policy.Guardrails(policy.DefaultParams()) {
		guardOrder[g.Sid] = i
		guardEffect[g.Sid] = g.Effect
	}
	baseEffect := map[string]policy.Effect{}
	for _, doc := range policy.Baseline() {
		for _, st := range doc.Document.Statements {
			baseEffect[st.Sid] = st.Effect
		}
	}
	// ANTI-VACUITY: an engine that returned nothing would make every derivation
	// below report "the sid is gone", which is a red test for the wrong reason and
	// would be read as a landing-page defect.
	if len(guardOrder) < 5 || len(baseEffect) < 4 {
		t.Fatalf("read %d guardrail(s) and %d baseline statement(s) out of internal/policy; "+
			"the engine has more of both, so this scan is not seeing it",
			len(guardOrder), len(baseEffect))
	}

	advance := sqlcQueryBody(t, repoFile(t, "db", "queries", "tags.sql"), "AdvanceTagCounter")
	tapSrc := repoFile(t, "internal", "handler", "tap.go")
	checkinSrc := repoFile(t, "internal", "handler", "checkin.go")
	foreignArm := goSwitchArm(t, checkinSrc, "checkin.OutcomeForeignTenant")
	activationArm := goSwitchArm(t, checkinSrc, "checkin.OutcomeActivation")

	out := map[pages.Source]func() string{}

	// The nine policy-backed sources, built from one closure so a new sid cannot be
	// pinned by a derivation somebody forgot to write: a Source with no entry in
	// sourceEffects gets no derivation and the test below says so.
	for src, wantEffect := range sourceEffects {
		sid, ok := policySid(src)
		if !ok {
			t.Fatalf("sourceEffects carries %q, which is not a policy: source", src)
		}
		got, isGuard := guardEffect[sid]
		if !isGuard {
			got, ok = baseEffect[sid]
			if !ok {
				got = ""
			}
		}
		out[src] = func() string {
			if got == "" {
				return "internal/policy no longer carries a statement with the sid " + sid +
					", so the line that rests on it describes a rule that is not in the engine"
			}
			if got != wantEffect {
				return "the rule " + sid + " is now `" + string(got) + "` rather than `" +
					string(wantEffect) + "`, so what the page says this line ends in is no " +
					"longer what the engine does"
			}
			return ""
		}
	}

	out[pages.SourceSUNURLCarriesCounterAndSignature] = func() string {
		f := fieldNames(sun.Params{})
		ctr, hasCtr := f["Ctr"]
		cmac, hasCMAC := f["CMAC"]
		switch {
		case !hasCtr, !hasCMAC:
			return "sun.Params no longer carries both Ctr and CMAC, so the plaque's link no " +
				"longer carries both a counter and a signature"
		case ctr.Kind() != reflect.Uint32:
			return "sun.Params.Ctr is a " + ctr.String() + " rather than the chip's counter"
		case cmac.Kind() != reflect.Slice:
			return "sun.Params.CMAC is a " + cmac.String() + " rather than signature bytes"
		}
		return ""
	}

	out[pages.SourceTapIsAWebPage] = func() string {
		if !strings.HasPrefix(TapPath, "/") {
			return "handler.TapPath is " + strconv.Quote(TapPath) + ", which is not a path a " +
				"browser can open, so \"the browser opens the page\" is no longer true"
		}
		if !tapRouteRE.MatchString(tapSrc) {
			return "internal/handler/tap.go no longer registers TapPath with a GET, so the " +
				"plaque's link does not open a page"
		}
		return ""
	}

	out[pages.SourceSignatureIsVerified] = func() string {
		// Reflection rather than a call: a call needs a tag, a key and a database,
		// and the claim is only that the server is where the signature is checked.
		if _, ok := reflect.TypeOf((*sun.Verifier)(nil)).MethodByName("Verify"); !ok {
			return "sun.Verifier has no Verify method; the one place CLAUDE.md §3 allows the " +
				"cryptography to live no longer checks anything"
		}
		return ""
	}

	out[pages.SourceCounterAdvanceIsGuarded] = func() string {
		if !counterGuardIsStrict(advance) {
			return "AdvanceTagCounter no longer advances the counter under a strict `old_ctr " +
				"< @ctr`, so a second copy of one link no longer loses (CLAUDE.md §4.4)"
		}
		return ""
	}

	out[pages.SourceForeignTenantIsRefusedNotActivated] = func() string {
		return foreignTenantIsRefused(foreignArm, activationArm)
	}

	return out
}

// sourceConstRE matches a declaration in pages' Source const block.
var sourceConstRE = regexp.MustCompile(`(?m)^\s*(\w+)\s+Source\s*=\s*"([^"]+)"`)

// declaredSources reads the Source constants OUT OF pages' OWN SOURCE, returning
// value -> constant name.
//
// 🔴 IT READS THE SOURCE RATHER THAN REFLECTING, for the reason written on
// declaredAnchors: Go has no runtime enumeration of a package's constants, so
// "declared but claimed by nothing" is only a detectable state if the declaration
// itself is read. An audit walked through exactly that hole on the Anchor side by
// adding a constant no test file mentioned.
func declaredSources(t *testing.T) map[pages.Source]string {
	t.Helper()
	src := repoFile(t, "web", "templates", "pages", "landingview.go")
	out := map[pages.Source]string{}
	for _, m := range sourceConstRE.FindAllStringSubmatch(src, -1) {
		out[pages.Source(m[2])] = m[1]
	}
	// ANTI-VACUITY: a regexp that stopped matching would report a package with no
	// sources and agree with everything built on it.
	if len(out) < 10 {
		t.Fatalf("read %d Source constant(s) out of landingview.go; there are at least ten, "+
			"so this scan is not seeing the const block and every check built on it would "+
			"pass over anything", len(out))
	}
	return out
}

// --- LandingRules: the product half -----------------------------------------

// TestLandingRules_EveryLineRestsOnAProductSource is the second invariant for the
// decision table: not "is this sentence rendered" but "is the rule it describes
// still in the engine, still at this layer and still ending the way the page says".
//
// 🔴 IT ALSO CHECKS THE ORDER OF THE GUARDRAILS, which no other check here can. The
// page prints a numbered list over the sentence "the first one that matches wins",
// so the numbers are a claim about internal/policy's evaluation order — and
// reordering two guardrails is a change that leaves every sentence on the page true
// in isolation and the page as a whole wrong.
func TestLandingRules_EveryLineRestsOnAProductSource(t *testing.T) {
	t.Parallel()
	derivations := sourceDerivations(t)

	guardOrder := map[string]int{}
	for i, g := range policy.Guardrails(policy.DefaultParams()) {
		guardOrder[g.Sid] = i
	}

	prevGuard := -1
	for i, r := range pages.LandingRules {
		if len(r.Sources) == 0 {
			t.Errorf("rule %d (%q) rests on no product source.\n"+
				"Every line of the decision table has to name the engine rule it describes, or "+
				"be deleted. A line with no source is how \"it is the one path in the product "+
				"that writes no record\" shipped on a page that sells the product.", i+1, r.When)
			continue
		}
		verdictIsSupported := false
		for _, s := range r.Sources {
			derive, ok := derivations[s]
			if !ok {
				t.Errorf("rule %d (%q) names the source %q, which has no derivation here. "+
					"A source nothing derives is a comment, not a pin.", i+1, r.When, s)
				continue
			}
			if why := derive(); why != "" {
				t.Errorf("the landing page's decision table says:\n    %q — %q\n"+
					"and the product no longer supports it: %s\n(source %q)\n"+
					"Fix the product or fix the line — a marketing page may not describe a "+
					"decision the engine does not make.", r.When, r.Body, why, s)
				continue
			}
			// The verdict printed beside the line has to be the stamp of at least one
			// of the rules it rests on. Row six rests on three (an allow and two
			// reviews) and is stamped for the allow, which is the branch its sentence
			// is about; every other row rests on exactly one, so this is strict there.
			if eff, ok := sourceEffects[s]; ok && effectStamp[eff] == r.Verdict {
				verdictIsSupported = true
			}
			// The five guardrail lines must arrive in the engine's own order.
			if sid, isPolicy := policySid(s); isPolicy {
				if pos, isGuard := guardOrder[sid]; isGuard {
					if pos <= prevGuard {
						t.Errorf("rule %d (%q) rests on the guardrail %s, which internal/policy "+
							"evaluates BEFORE one printed above it. The page numbers this list "+
							"and says the first match wins, so the printed order is a claim "+
							"about the engine's order.", i+1, r.When, sid)
					}
					prevGuard = pos
				}
			}
		}
		if !verdictIsSupported {
			t.Errorf("rule %d (%q) is stamped %q and not one of the rules it rests on ends "+
				"that way. The stamp is the visitor's summary of the line; it cannot come "+
				"from nowhere.", i+1, r.When, r.Verdict)
		}
	}

	// ANTI-VACUITY. A block that lost its lines would satisfy every loop above.
	if n := len(pages.LandingRules); n < 7 {
		t.Fatalf("the decision table carries %d line(s); it had seven, so either the section "+
			"shrank without this test being reconsidered or the scan is reading nothing", n)
	}
}

// --- LandingTapFlow: the product half ---------------------------------------

// TestLandingTapFlow_EveryStageRestsOnAProductSource is the same invariant for the
// proof block: every stage of "how a tap proves itself" names a fact in this
// repository, and every one of those facts is still there.
func TestLandingTapFlow_EveryStageRestsOnAProductSource(t *testing.T) {
	t.Parallel()
	derivations := sourceDerivations(t)

	for i, s := range pages.LandingTapFlow {
		if len(s.Sources) == 0 {
			t.Errorf("stage %d (%q) rests on no product source. This block is the product's "+
				"security argument written for a stranger; a sentence in it that is held "+
				"against nothing is the shape of claim landingview.go's header refuses.",
				i+1, s.Title)
			continue
		}
		for _, src := range s.Sources {
			derive, ok := derivations[src]
			if !ok {
				t.Errorf("stage %d (%q) names the source %q, which has no derivation here. "+
					"A source nothing derives is a comment, not a pin.", i+1, s.Title, src)
				continue
			}
			if why := derive(); why != "" {
				t.Errorf("the landing page claims:\n    %q — %q\n"+
					"and the product no longer supports it: %s\n(source %q)\n"+
					"Fix the product or delete the sentence.", s.Title, s.Body, why, src)
			}
		}
	}

	if n := len(pages.LandingTapFlow); n < 4 {
		t.Fatalf("the proof block carries %d stage(s); it had four, so either it shrank "+
			"without this test being reconsidered or the scan is reading nothing", n)
	}
}

// --- the closure over the vocabulary ----------------------------------------

// TestLandingSources_EveryDeclaredSourceIsDerivedAndClaimed closes the Source
// vocabulary the way TestLandingAudiences_EveryClaimRestsOnAProductAnchor closes
// the Anchor one: a constant cannot be declared, pass, and pin nothing, and this
// file cannot carry a derivation for a fact the page no longer names.
func TestLandingSources_EveryDeclaredSourceIsDerivedAndClaimed(t *testing.T) {
	t.Parallel()
	derivations := sourceDerivations(t)
	declared := declaredSources(t)

	used := map[pages.Source]bool{}
	for _, r := range pages.LandingRules {
		for _, s := range r.Sources {
			used[s] = true
		}
	}
	for _, st := range pages.LandingTapFlow {
		for _, s := range st.Sources {
			used[s] = true
		}
	}

	for src, name := range declared {
		if _, ok := derivations[src]; !ok {
			t.Errorf("pages declares the source %s = %q and nothing in sourceDerivations "+
				"derives it. A source with no derivation is a comment: a line could name it "+
				"and be pinned by nothing.", name, src)
		}
		if !used[src] {
			t.Errorf("pages declares the source %s = %q and NO line rests on it. Either a "+
				"sentence was deleted and its pin left behind, or the pin was written for a "+
				"sentence that was never added.", name, src)
		}
	}
	for src := range derivations {
		if _, ok := declared[src]; !ok {
			t.Errorf("sourceDerivations derives %q, which pages does not declare as a Source "+
				"constant", src)
		}
	}
	if len(used) == 0 {
		t.Fatal("no line in either mechanism block names a source; the two blocks that carry " +
			"this page's technical claims are unpinned")
	}
}

// TestSourceMechanisms_SayNoWhenTheFactIsAbsent is the negative control for the
// mechanisms every Source is derived from.
//
// 🔴 A SCANNER THAT MATCHED NOTHING WOULD LEAVE EVERY TEST ABOVE GREEN OVER A
// PRODUCT THAT HAD LOST THE CAPABILITY, and the failure would look exactly like
// success — the rule every scanner in marketing_test.go is written under. Two of
// these cannot be mutation-tested the ordinary way for the same reason two anchors
// could not: removing sun.Params.Ctr or sun.Verifier.Verify breaks the build rather
// than a test, which is a stronger gate but leaves the derivation itself never
// having said no. So they are exercised against synthetic inputs here.
func TestSourceMechanisms_SayNoWhenTheFactIsAbsent(t *testing.T) {
	t.Parallel()

	// The replay guard's scanner. `<=` and `>=` both let the SAME counter through
	// twice, which is replay itself, and a scan that could not tell them from `<`
	// would pin nothing.
	for _, tc := range []struct {
		name, query string
		want        bool
	}{
		{"strict", "WHERE t.uid = prev.old_uid AND prev.old_ctr < @ctr", true},
		{"strict, no spaces", "AND prev.old_ctr<@ctr", true},
		{"non-strict", "AND prev.old_ctr <= @ctr", false},
		{"reversed", "AND @ctr > prev.old_ctr", false},
		{"guard removed", "WHERE t.uid = prev.old_uid", false},
		{"compared with something else", "AND prev.old_ctr < t.last_ctr", false},
	} {
		if got := counterGuardIsStrict(tc.query); got != tc.want {
			t.Errorf("counterGuardIsStrict(%s) = %v, want %v — CLAUDE.md §4.4's whole "+
				"replay defence is this comparison", tc.name, got, tc.want)
		}
	}

	// The route scanner.
	if !tapRouteRE.MatchString("r.Get(TapPath, t.Page)") {
		t.Error("the route scanner cannot see the registration it exists to see")
	}
	if tapRouteRE.MatchString("r.Get(TapPathPreview, t.Page)") {
		t.Error("the route scanner matches a DIFFERENT route whose name merely starts the " +
			"same way, so it would pass over TapPath being unmounted")
	}
	if tapRouteRE.MatchString("// r.Get(TapPath was here") {
		t.Error("the route scanner matches prose rather than a call")
	}

	// The reflection halves, against a struct and a type that do not carry them.
	type withoutCtr struct{ CMAC []byte }
	if _, ok := fieldNames(withoutCtr{})["Ctr"]; ok {
		t.Error("fieldNames reports a field that is NOT on the struct, so the counter " +
			"source would pass over a removed capability")
	}
	type noVerify struct{}
	if _, ok := reflect.TypeOf((*noVerify)(nil)).MethodByName("Verify"); ok {
		t.Error("MethodByName reports a method that does not exist, so the signature " +
			"source would pass over a removed verifier")
	}

	// The query extractor is scoped to ONE query.
	//
	// 🔴 THE SECOND STATEMENT IS SPLIT ACROSS TWO PHYSICAL LINES AND THAT IS NOT
	// FORMATTING. scripts/redline-check.sh's R4 is LINE-LOCAL by a measured decision
	// recorded in the script itself, and unlike R3 it carries no `**/*_test.go`
	// exemption — so an unguarded counter update written whole on one line here made
	// `make audit` FAIL over a FIXTURE, which ci.yml runs without continue-on-error.
	// A red audit hides real findings, so the fixture gives the scanner nothing to
	// match on any single line while the VALUE stays byte for byte what it was: this
	// control still proves the extractor is scoped to one query. The alternative was
	// a `-- redline:` exemption, which would blind the scanner to this file for good.
	//
	// ⚠️ AND THAT GOES FOR THIS PARAGRAPH TOO — MEASURED. Describing the offending
	// statement by quoting it put the scanner's own pattern back in the file and the
	// audit stayed red, on a comment. R4 does not know what a comment is.
	const twoQueries = "-- name: Other :one\nSELECT 1 WHERE old_ctr < @ctr;\n" +
		"-- name: AdvanceTagCounter :one\n" +
		"UPDATE tags " +
		"SET last_ctr = @ctr;\n"
	if body := sqlcQueryBody(t, twoQueries, "AdvanceTagCounter"); counterGuardIsStrict(body) {
		t.Errorf("sqlcQueryBody leaked a NEIGHBOURING query's text into %s, so the replay "+
			"guard could be satisfied by any statement in the file", "AdvanceTagCounter")
	}

	// AND IT DOES NOT ACCEPT THE COMMENT THAT EXPLAINS THE GUARD, which is the
	// mistake this control was written after making. The real query's header quotes
	// `prev.old_ctr < @ctr` in prose, so a scan that kept comments passed over the
	// statement being changed to `<=`.
	//
	// The header also carries a SEMICOLON in prose, which is the second thing that
	// went wrong: cutting the statement at the first `;` before removing comments
	// ended the body inside the paragraph and extracted nothing at all.
	const commentedAway = "-- name: AdvanceTagCounter :one\n" +
		"-- On replay prev.old_ctr < @ctr is false; no row updates.\n" +
		"UPDATE tags t SET last_ctr = @ctr FROM prev WHERE prev.old_ctr <= @ctr;\n"
	if body := sqlcQueryBody(t, commentedAway, "AdvanceTagCounter"); counterGuardIsStrict(body) {
		t.Errorf("counterGuardIsStrict is satisfied by a COMMENT quoting the guard while the "+
			"statement itself uses `<=`. A scan a comment can satisfy pins nothing: the "+
			"same counter would pass twice, which is replay itself (CLAUDE.md §4.4).\n"+
			"Extracted: %q", body)
	}

	// The foreign-tenant scanner, against the three ways line three's second sentence
	// can stop being true. The first case is the DEFECT ITSELF: the line used to say
	// a phone signed in to another organisation is "turned away the same way" as a
	// phone with no session, which reads as "it is offered an invitation to activate"
	// — the §4.5 behaviour the handler deliberately does not have.
	const refusedArm = "t.renderProblem(w, r, http.StatusForbidden, tapProblemForeignTenant)"
	const redirectArm = "t.redirectToActivation(w, r)"
	if why := foreignTenantIsRefused(refusedArm, redirectArm); why != "" {
		t.Errorf("the foreign-tenant scan says no to the handler it was written for: %s", why)
	}
	if foreignTenantIsRefused(redirectArm, redirectArm) == "" {
		t.Error("the foreign-tenant scan is satisfied by an arm that sends a phone signed in " +
			"to ANOTHER organisation to the activation page. That is the sentence this " +
			"source exists to hold, so a scan that accepts it pins nothing (§4.5).")
	}
	if foreignTenantIsRefused(refusedArm, "t.render(w, r, http.StatusOK, nil)") == "" {
		t.Error("the foreign-tenant scan passes over the ACTIVATION outcome no longer " +
			"redirecting, so the contrast line three draws between the two record-less " +
			"paths could vanish with the page still claiming it")
	}

	// AND IT DOES NOT ACCEPT THE PARAGRAPH THAT EXPLAINS THE BRANCH, which is the
	// same mistake sqlcQueryBody was written after making. checkin.go's arm carries a
	// comment naming renderProblem and tapProblemForeignTenant; a scan that kept
	// comments would read that explanation and pass over the branch beneath it having
	// been changed to render an ordinary result.
	const explainedAway = "case checkin.OutcomeForeignTenant:\n" +
		"\t\t// Was renderProblem(w, r, http.StatusForbidden, tapProblemForeignTenant).\n" +
		"\t\tt.render(w, r, http.StatusOK, pages.Result(resultView(res)))\n" +
		"\tcase checkin.OutcomeActivation:\n"
	if arm := goSwitchArm(t, explainedAway, "checkin.OutcomeForeignTenant"); foreignTenantIsRefused(arm, redirectArm) == "" {
		t.Errorf("foreignTenantIsRefused is satisfied by a COMMENT quoting the refusal while "+
			"the branch itself renders an ordinary result. A scan a comment can satisfy pins "+
			"nothing.\nExtracted: %q", arm)
	}

	// AND EVERY LIVE SOURCE HOLDS TODAY, which is what makes the checks above a
	// control rather than a separate universe.
	for src, derive := range sourceDerivations(t) {
		if why := derive(); why != "" {
			t.Errorf("source %q does not hold: %s", src, why)
		}
	}
}

// --- the three blocks that get the rendering invariant only ------------------

// TestLandingSteps_EveryStepIsRenderedInOrder pins handoff §9's three steps.
//
// ⚠️ RENDERING ONLY, AND THE GAP IS STATED. Step carries no Sources field, so
// nothing here checks that the activation flow or the direction rule these
// sentences describe still exists; adding that pin means adding a field to
// pages.Step, which is not this task's to change. What this catches is the other
// half: a step edited in Go and never rendered, or rendered out of order.
func TestLandingSteps_EveryStepIsRenderedInOrder(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)

	if n := len(pages.LandingSteps); n != 3 {
		t.Fatalf("LandingSteps carries %d step(s); handoff §9 has three and the section's "+
			"printed subtitle says \"Three things happen\"", n)
	}
	if !strings.Contains(text, "Three things happen") {
		t.Error("the setup section no longer prints \"Three things happen\", so the count " +
			"asserted above is no longer the count the visitor is given")
	}

	titles := make([]string, len(pages.LandingSteps))
	for i, s := range pages.LandingSteps {
		titles[i] = s.Title
	}
	at := renderedInOrder(t, text, "LandingSteps", titles)

	for i, s := range pages.LandingSteps {
		if want := fmt.Sprintf("%02d", i+1); s.Ordinal != want {
			t.Errorf("step %d is printed with the ordinal %q, want %q", i+1, s.Ordinal, want)
		}
		if at[i] < 0 {
			continue
		}
		if indexFrom(text, s.Body, at[i]) < 0 {
			t.Errorf("step %d's title %q is rendered and its body is not:\n    %q",
				i+1, s.Title, s.Body)
		}
	}
}

// TestLandingEvidence_EveryPieceIsRendered pins CLAUDE.md §5's four pieces of
// evidence.
//
// EACH ENTRY IS THREE STRINGS AND ALL THREE ARE CHECKED. The Answers line is what
// the piece of evidence proves ("who tapped"), the Body is where its LIMIT is
// written — that the position is read once, on a press, and shown on no screen —
// and a limit that stops being rendered turns a careful sentence into a bare boast.
//
// ⚠️ RENDERING ONLY: Evidence carries no Sources field either.
func TestLandingEvidence_EveryPieceIsRendered(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)

	if n := len(pages.LandingEvidence); n != 4 {
		t.Fatalf("LandingEvidence carries %d piece(s); CLAUDE.md §5's four pieces of evidence "+
			"are four, and the page prints the heading \"Four pieces of evidence\" over them", n)
	}
	if !strings.Contains(text, "Four pieces of evidence") {
		t.Error("the proof section no longer prints \"Four pieces of evidence\", so the " +
			"count asserted above is no longer the count the visitor is given")
	}

	for i, e := range pages.LandingEvidence {
		for what, line := range map[string]string{
			"label":   e.Label,
			"answers": e.Answers,
			"body":    e.Body,
		} {
			if line == "" {
				t.Errorf("evidence %d has an empty %s", i+1, what)
				continue
			}
			if !strings.Contains(text, line) {
				t.Errorf("evidence %d's %s is declared in pages and is NOT rendered:\n    %q\n"+
					"The body is where this piece of evidence's limit is stated, so a half-"+
					"rendered entry is a promise with its qualification removed.", i+1, what, line)
			}
		}
	}
}

// TestLandingFAQ_EveryQuestionIsRenderedWithItsAnswer pins handoff §9's FAQ.
//
// ⚠️ RENDERING ONLY: Question carries no Sources field. What each answer rests on
// is written in LandingFAQ's doc comment and held by tests elsewhere in the
// repository (the append-only record, the QR rule, the tenant isolation), not by
// a pin on the sentence.
func TestLandingFAQ_EveryQuestionIsRenderedWithItsAnswer(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)

	if n := len(pages.LandingFAQ); n < 6 {
		t.Fatalf("LandingFAQ carries %d question(s); it had six, so either the section shrank "+
			"without this test being reconsidered or the scan is reading nothing", n)
	}

	for i, q := range pages.LandingFAQ {
		if !strings.Contains(text, q.Q) {
			t.Errorf("FAQ %d's question is declared in pages and is NOT rendered:\n    %q",
				i+1, q.Q)
			continue
		}
		if !strings.Contains(text, q.A) {
			t.Errorf("FAQ %d asks:\n    %q\nand its answer is NOT rendered:\n    %q\n"+
				"A question on a page with no answer under it is worse than no question.",
				i+1, q.Q, q.A)
		}
	}
}
