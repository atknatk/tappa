package handler

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE PINS FOR THE LANDING PAGE'S ENGINE-FACT BLOCK — the four security cards —
// and the rendering invariant for the steps, the cards and the FAQ.
//
// 🔴 THIS FILE EXISTS BECAUSE A FALSE SENTENCE SHIPPED THROUGH THE GAP IT CLOSES.
// The 2026-09-07 page declared seven claim-carrying values and only two of them
// were held against anything; a decision-table line that read "it is the one path
// in the product that writes no record" was false and nothing was red, because
// nothing read the slice. The two invariants written then are the ones every
// block on the page is held to now:
//
//   - EVERY LINE ACTUALLY REACHES A VISITOR. Text-matching is legitimate here — the
//     question is "did this exact sentence get rendered", not "is it true" — and it
//     is what catches a block that was edited in Go and dropped from the template,
//     or a template that renders a stale subset. The 2026-09-14 port did exactly
//     that to every block at once, and this file went red on all of them.
//   - EVERY LINE RESTS ON A PRODUCT FACT THAT IS STILL THERE. pages.Source is the
//     vocabulary for the engine — the policy statements, the counter guard, the
//     verifier — and each Source is DERIVED from the policy engine, the generated
//     query source, the schema or reflection, never compared against expected copy.
//
// ⚠️ THE LIMIT IS THE ONE WRITTEN ON pages.Anchor, WORD FOR WORD. Naming a Source
// does not make a sentence true and does not check that the sentence is ABOUT the
// fact it names. This is a RATCHET AGAINST DRIFT.
//
// 🔴 WHAT WAS DELETED ON 2026-09-15, AND WHY IT IS NOT A LOSS OF COVERAGE. The
// user's design has no decision ladder and no tap-flow strip, so LandingRules and
// LandingTapFlow are gone from pages, and with them the two Sources only they
// claimed (sys:person-debounce, and the foreign-tenant arm scanner that held line
// three's second sentence). A pin with nothing to hold is a comment. The
// BEHAVIOUR those pins described is still tested where it lives:
// TestCheckinDB_ForeignTenantTapIsRefusedAndWritesNOTHING and the debounce tests
// in internal/domain/tap and internal/handler.

// --- the shared render helper ----------------------------------------------

// renderedLandingText is the landing page as a visitor perceives it: fetched
// through the REAL router and flattened by the same screenText every other screen
// test in this package uses, so text smuggled into an attribute still counts and
// markup never does.
func renderedLandingText(t *testing.T) string {
	t.Helper()
	return screenText(t, mustFetchMarketing(t, marketingRouter(t), "/"))
}

// asRendered is a declared sentence the way screenText would flatten it: runs of
// whitespace — a no-break space included, which is what "150 m" carries — become
// one ordinary space. A declared line is compared in this form and no other.
func asRendered(s string) string { return strings.Join(strings.Fields(s), " ") }

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
		w = asRendered(w)
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

// --- the Source derivations -------------------------------------------------

// sourceEffects is the effect each policy-backed Source CLAIMS the engine carries.
//
// 🔴 IT IS THE EXPECTED VALUE AND NOT A READBACK, which is the whole reason it
// catches anything. A derivation that read policy.Guardrails and asserted "the
// effect is whatever the engine says" would pass over a guardrail flipped from deny
// to allow. These are the effects the PAGE's sentences depend on, so a baseline or
// guardrail that changed one fails here with the sid still present — exactly what
// landingview.go's Source block promises.
var sourceEffects = map[pages.Source]policy.Effect{
	pages.SourceTagNotActive:        policy.EffectDeny,
	pages.SourceSUNInvalid:          policy.EffectDeny,
	pages.SourceNoSession:           policy.EffectRedirect,
	pages.SourceEmployeeDeactivated: policy.EffectDeny,
	pages.SourceIPOrGPSOK:           policy.EffectAllow,
	pages.SourceGPSOnlyAllow:        policy.EffectAllow,
	pages.SourceQRRequiresIP:        policy.EffectReview,
	pages.SourceNoEvidenceReview:    policy.EffectReview,
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

// sourceDerivations maps every pages.Source to a check that READS THE PRODUCT,
// returning "" when the fact is present or the reason it is not.
//
// 🔴 NOT ONE OF THEM COMPARES A SENTENCE — the same rule anchorDerivations is
// written under, for the same reason.
func sourceDerivations(t *testing.T) map[pages.Source]func() string {
	t.Helper()

	guardEffect := map[string]policy.Effect{}
	for _, g := range policy.Guardrails(policy.DefaultParams()) {
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
	if len(guardEffect) < 5 || len(baseEffect) < 4 {
		t.Fatalf("read %d guardrail(s) and %d baseline statement(s) out of internal/policy; "+
			"the engine has more of both, so this scan is not seeing it",
			len(guardEffect), len(baseEffect))
	}

	advance := sqlcQueryBody(t, repoFile(t, "db", "queries", "tags.sql"), "AdvanceTagCounter")
	tapSrc := repoFile(t, "internal", "handler", "tap.go")
	configSrc := repoFile(t, "internal", "config", "config.go")

	out := map[pages.Source]func() string{}

	// The policy-backed sources, built from one closure so a new sid cannot be
	// pinned by a derivation somebody forgot to write: a Source with no entry in
	// sourceEffects gets no derivation and the closure test below says so.
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

	out[pages.SourceGPSRadiusDefault] = func() string {
		m := gpsRadiusDefaultRE.FindStringSubmatch(configSrc)
		if m == nil {
			return "internal/config no longer reads TAPPA_GPS_RADIUS_M with a default in a shape " +
				"this scan can read, so \"within 150 m\" is pinned to nothing"
		}
		if m[1] != "150" {
			return "internal/config's default GPS radius is " + m[1] + " m and the page says 150 m"
		}
		return ""
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

// --- the four cards: the product half -----------------------------------------

// TestLandingEvidence_EveryCardRestsOnAProductSource is the second invariant for
// the security section: not "is this sentence rendered" but "is the mechanism it
// describes still in the engine and still ending the way the page says".
func TestLandingEvidence_EveryCardRestsOnAProductSource(t *testing.T) {
	t.Parallel()
	derivations := sourceDerivations(t)

	for i, e := range pages.LandingEvidence {
		if len(e.Sources) == 0 {
			t.Errorf("card %d (%q) rests on no product source. This block is the product's "+
				"security argument written for a stranger; a sentence in it that is held "+
				"against nothing is the shape of claim landingview.go's header refuses.",
				i+1, e.Label)
			continue
		}
		for _, src := range e.Sources {
			derive, ok := derivations[src]
			if !ok {
				t.Errorf("card %d (%q) names the source %q, which has no derivation here. "+
					"A source nothing derives is a comment, not a pin.", i+1, e.Label, src)
				continue
			}
			if why := derive(); why != "" {
				t.Errorf("the landing page claims:\n    %q — %q\n"+
					"and the product no longer supports it: %s\n(source %q)\n"+
					"Fix the product or delete the sentence.", e.Label, e.Body, why, src)
			}
		}
	}

	// ANTI-VACUITY. A block that lost its cards would satisfy every loop above.
	if n := len(pages.LandingEvidence); n < 4 {
		t.Fatalf("the security section carries %d card(s); it had four, so either it shrank "+
			"without this test being reconsidered or the scan is reading nothing", n)
	}
}

// --- the closure over the vocabulary ----------------------------------------

// TestLandingSources_EveryDeclaredSourceIsDerivedAndClaimed closes the Source
// vocabulary the way TestLandingAudiences_EveryClaimRestsOnAProductAnchor closes
// the Anchor one: a constant cannot be declared, pass, and pin nothing, and this
// file cannot carry a derivation for a fact the page no longer names.
//
// A Source is CLAIMED when a security card names it, or when a pages.Fact that a
// rendered sentence names delegates to it (factSourceDelegations) — the Fact is
// then the sentence's pin and the Source is what the pin is made of.
func TestLandingSources_EveryDeclaredSourceIsDerivedAndClaimed(t *testing.T) {
	t.Parallel()
	derivations := sourceDerivations(t)
	declared := declaredSources(t)
	claimed := claimedFacts()

	used := map[pages.Source]bool{}
	for _, e := range pages.LandingEvidence {
		for _, s := range e.Sources {
			used[s] = true
		}
	}
	for fact, srcs := range factSourceDelegations {
		if _, ok := claimed[fact]; !ok {
			continue // a delegation from a fact nothing claims claims nothing
		}
		for _, s := range srcs {
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
			t.Errorf("pages declares the source %s = %q and NO line rests on it, directly or "+
				"through a Fact. Either a sentence was deleted and its pin left behind, or the "+
				"pin was written for a sentence that was never added.", name, src)
		}
	}
	for src := range derivations {
		if _, ok := declared[src]; !ok {
			t.Errorf("sourceDerivations derives %q, which pages does not declare as a Source "+
				"constant", src)
		}
	}
	for _, srcs := range factSourceDelegations {
		for _, s := range srcs {
			if _, ok := declared[s]; !ok {
				t.Errorf("factSourceDelegations names %q, which pages does not declare as a Source", s)
			}
		}
	}
	if len(used) == 0 {
		t.Fatal("no line on the page names a source; the block that carries this page's " +
			"technical claims is unpinned")
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

	// AND EVERY LIVE SOURCE HOLDS TODAY, which is what makes the checks above a
	// control rather than a separate universe.
	for src, derive := range sourceDerivations(t) {
		if why := derive(); why != "" {
			t.Errorf("source %q does not hold: %s", src, why)
		}
	}
}

// --- the three blocks that get the rendering invariant here ------------------

// TestLandingSteps_EveryStepIsRenderedInOrder pins the three cards under "How it
// works": three of them, numbered as the reference numbers them, each with its
// icon, its title and every run of its body reaching the visitor in order.
//
// The product half is marketing_facts_test.go's
// TestLandingSteps_EveryStepRestsOnAProductFact.
func TestLandingSteps_EveryStepIsRenderedInOrder(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)

	if n := len(pages.LandingSteps); n != 3 {
		t.Fatalf("LandingSteps carries %d step(s); the user's design has three cards", n)
	}

	titles := make([]string, len(pages.LandingSteps))
	for i, s := range pages.LandingSteps {
		titles[i] = s.Title
	}
	at := renderedInOrder(t, text, "LandingSteps", titles)

	for i, s := range pages.LandingSteps {
		if want := "STEP " + strconv.Itoa(i+1); s.Ordinal != want {
			t.Errorf("step %d is printed with the ordinal %q, want %q", i+1, s.Ordinal, want)
		}
		if s.Icon == "" || !strings.Contains(text, s.Icon) {
			t.Errorf("step %d's icon %q is not on the page", i+1, s.Icon)
		}
		if at[i] < 0 {
			continue
		}
		if len(s.Body) == 0 {
			t.Errorf("step %d (%q) has an empty body", i+1, s.Title)
			continue
		}
		from := at[i]
		for j, run := range s.Body {
			idx := indexFrom(text, asRendered(run.Text), from)
			if idx < 0 {
				t.Errorf("step %d's title %q is rendered and run %d of its body is not, or not in order:\n    %q",
					i+1, s.Title, j+1, run.Text)
				break
			}
			from = idx + len(run.Text)
		}
	}
}

// TestLandingEvidence_EveryPieceIsRendered pins CLAUDE.md §5's four pieces of
// evidence as the four cards under "Four proofs on every single tap."
//
// EACH ENTRY IS THREE STRINGS AND ALL THREE ARE CHECKED, IN ORDER. The Body is
// where each card's LIMIT is written — that a code seen twice is refused, that a
// tap nothing can verify is queued rather than approved — and a limit that stops
// being rendered turns a careful sentence into a bare boast.
func TestLandingEvidence_EveryPieceIsRendered(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)

	if n := len(pages.LandingEvidence); n != 4 {
		t.Fatalf("LandingEvidence carries %d card(s); CLAUDE.md §5's four pieces of evidence "+
			"are four, and the page prints \"Four proofs\" over them", n)
	}
	if !strings.Contains(text, "Four proofs") {
		t.Error("the security section no longer prints \"Four proofs\", so the count " +
			"asserted above is no longer the count the visitor is given")
	}

	labels := make([]string, len(pages.LandingEvidence))
	for i, e := range pages.LandingEvidence {
		labels[i] = e.Label
	}
	at := renderedInOrder(t, text, "LandingEvidence", labels)

	for i, e := range pages.LandingEvidence {
		if at[i] < 0 {
			continue
		}
		if e.Body == "" {
			t.Errorf("card %d (%q) has an empty body", i+1, e.Label)
			continue
		}
		if indexFrom(text, asRendered(e.Body), at[i]) < 0 {
			t.Errorf("card %d's heading %q is rendered and its body is not:\n    %q\n"+
				"The body is where this piece of evidence's limit is stated, so a half-"+
				"rendered card is a promise with its qualification removed.", i+1, e.Label, e.Body)
		}
	}
}

// TestLandingFAQ_EveryQuestionIsRenderedWithItsAnswer pins the FAQ.
//
// The product half is marketing_facts_test.go's
// TestLandingFAQ_EveryAnswerRestsOnAProductFact.
func TestLandingFAQ_EveryQuestionIsRenderedWithItsAnswer(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)

	if n := len(pages.LandingFAQ); n < 5 {
		t.Fatalf("LandingFAQ carries %d question(s); it has five, so either the section shrank "+
			"without this test being reconsidered or the scan is reading nothing", n)
	}

	for i, q := range pages.LandingFAQ {
		if !strings.Contains(text, q.Q) {
			t.Errorf("FAQ %d's question is declared in pages and is NOT rendered:\n    %q",
				i+1, q.Q)
			continue
		}
		if !strings.Contains(text, asRendered(q.A)) {
			t.Errorf("FAQ %d asks:\n    %q\nand its answer is NOT rendered:\n    %q\n"+
				"A question on a page with no answer under it is worse than no question.",
				i+1, q.Q, q.A)
		}
	}
}
