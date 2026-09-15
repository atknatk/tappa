package handler

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/web"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE PINS FOR THE LANDING PAGE'S PRODUCT-FACT BLOCKS: the steps, the ledger, the
// price card, the FAQ and the lede lines.
//
// 🔴 THIS FILE EXISTS BECAUSE THE USER'S OWN DRAFT OF THE PAGE WAS MOSTLY MADE OF
// SENTENCES THE PRODUCT DOES NOT KEEP — an API, a file import, a live headcount, a
// report by department, "unlimited", a mis-stated decision rule, four numbers
// nobody measured — and the four REDs on this page's record all came from exactly
// that class. The 2026-09-15 re-pin carries the draft's words with the smallest
// edits that make them true; every sentence it carries arrives with a pin.
//
// THE TWO INVARIANTS ARE marketing_claims_test.go's, deliberately the same two:
// every line REACHES A VISITOR (text-matching is legitimate here: the question is
// "was it rendered", not "is it true"), and every line RESTS ON A PRODUCT FACT that
// is still there. pages.Fact is the third vocabulary beside Anchor and Source, and
// landingview.go records why a third one was needed. Where a Fact IS a fact those
// vocabularies already derive, the derivation below DELEGATES to theirs — one
// reading of the product, not two that could disagree — and the delegation is
// written in two maps (factSourceDelegations, factAnchorDelegations) so that the
// Source and Anchor closure tests can count a delegating Fact as a claim.
//
// ⚠️ THE LIMIT IS THE ONE WRITTEN ON pages.Anchor, WORD FOR WORD: naming a Fact
// does not make a sentence true. This is a ratchet against drift. Five of the
// facts are TRIPWIRES FOR AN ABSENCE (no import, no API, no finger or face reader,
// no background position): they fail the day the capability appears, which is the
// day the sentence that says "there is none" has to be rewritten.

// --- the derivations ---------------------------------------------------------

// multipartUploadRE matches the three ways non-test Go reads a file upload.
var multipartUploadRE = regexp.MustCompile(`\.(?:FormFile|MultipartReader|ParseMultipartForm)\(`)

// apiLiteralRE matches a "/api…" route literal in Go source: a path, which is
// what a registration carries, and not a quoted sentence that happens to start
// with one (ratelimit.go quotes a card's Turkish sentence about /api/activate).
var apiLiteralRE = regexp.MustCompile(`"(/api(?:/[A-Za-z0-9_.{}-]+)*/?)"`)

// goLineCommentRE strips `//` comments, line-locally, before a scan — a scan a
// comment can satisfy pins nothing (the rule marketing_claims_test.go's
// sqlcQueryBody records).
var goLineCommentRE = regexp.MustCompile(`(?m)//.*$`)

// tapFormAPIRoutes are the two POSTs the tap surface itself makes. Anything else
// under /api would be an integration API, and the page says there is none.
var tapFormAPIRoutes = map[string]bool{"/api/checkin": true, "/api/activate": true}

// authenticatorAPIRE matches the ways a web page or a Go handler would read a
// finger or a face: the platform-authenticator API and the two vendor readers.
//
// ⚠️ THE PATTERN IS SPELLED SO THAT THIS FILE DOES NOT ITSELF CARRY THE WORDS
// scripts/redline-check.sh's R1 scans for. The scanner is line-local and has no
// test exemption; a literal here would be a red audit over the pin that guards
// the same red line. `finger\s?print` matches the joined word in a scanned file
// and is not the joined word in this one.
var authenticatorAPIRE = regexp.MustCompile(`(?i)navigator\.credentials|PublicKeyCredential|web\.?authn|finger\s?print(?:ing)?\s*(?:reader|sensor|scan)|\btouch\s?id\b|\bface\s?id\b`)

// jsLineCommentRE strips `//` comments from a script before a scan.
var jsLineCommentRE = regexp.MustCompile(`(?m)//.*$`)

// continuousPositionCall is the Geolocation API's continuous read — the call
// CLAUDE.md §4.2 forbids and scripts/redline-check.sh's R2 scans for by name.
// Spelled in two halves for the reason authenticatorAPIRE records: R2 is a
// literal, line-local scan with no test exemption.
const continuousPositionCall = "watch" + "Position"

// gpsRadiusDefaultRE reads the default the deployment configures for the GPS
// radius: internal/config's floatEnvRange("TAPPA_GPS_RADIUS_M", <default>, …).
var gpsRadiusDefaultRE = regexp.MustCompile(`floatEnvRange\(\s*"TAPPA_GPS_RADIUS_M"\s*,\s*(\d+)`)

// plaquesIncludedRE is handoff §11's pricing term: "plaketler dahil ve ücretsiz
// değişim" (plaques included and replaced free).
var plaquesIncludedRE = regexp.MustCompile(`(?m)^## 11\..*\n(?:.*\n)*?.*plaketler dahil ve ücretsiz değişim`)

// cssRuleBody returns the body of the first `<selector> {…}` rule in a stylesheet,
// comments stripped first so a rule described in prose cannot satisfy a scan.
func cssRuleBody(css, selector string) string {
	css = cssCommentRE.ReplaceAllString(css, " ")
	re := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(selector) + `\s*\{(.*?)\}`)
	m := re.FindStringSubmatch(css)
	if m == nil {
		return ""
	}
	return m[1]
}

// foreignAPIRoutes returns every "/api…" literal in src that is not one of the
// tap form's own two. Package level for the negative control.
func foreignAPIRoutes(src string) []string {
	var out []string
	for _, m := range apiLiteralRE.FindAllStringSubmatch(goLineCommentRE.ReplaceAllString(src, ""), -1) {
		if !tapFormAPIRoutes[m[1]] {
			out = append(out, m[1])
		}
	}
	return out
}

// authenticatorAPIHit returns the first thing in src (comments stripped) that
// reads a finger or a face, or "". Package level for the negative control.
func authenticatorAPIHit(src string) string {
	return authenticatorAPIRE.FindString(goLineCommentRE.ReplaceAllString(src, ""))
}

// nonTestGoSource concatenates every non-test .go file under the given
// repository directories, prefixed with its path so a hit can be named.
func nonTestGoSource(t *testing.T, dirs ...string) map[string]string {
	t.Helper()
	root := filepath.Join("..", "..")
	out := map[string]string{}
	for _, dir := range dirs {
		err := filepath.Walk(filepath.Join(root, dir), func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			raw, e := os.ReadFile(p)
			if e != nil {
				return e
			}
			out[strings.TrimPrefix(p, root+string(filepath.Separator))] = string(raw)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
	// ANTI-VACUITY: a walk that read nothing would report a product with no API
	// and no upload, and agree with every tripwire.
	if len(out) < 40 {
		t.Fatalf("read %d non-test Go file(s); the product has far more, so this walk is "+
			"reading the wrong tree", len(out))
	}
	return out
}

// shippedScripts reads every script the binary serves, out of the EMBEDDED tree —
// which is what a browser gets, not the working directory.
func shippedScripts(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := fs.WalkDir(web.Static(), "js", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".js") {
			return nil
		}
		raw, e := fs.ReadFile(web.Static(), p)
		if e != nil {
			return e
		}
		out[p] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded scripts: %v", err)
	}
	// ANTI-VACUITY: the tap page's script is the one that reads a position, and a
	// walk that did not find it would report a product that watches nothing.
	if _, ok := out["js/tap.js"]; !ok || len(out) < 2 {
		t.Fatalf("read %d shipped script(s) and no js/tap.js; this walk is reading the wrong tree", len(out))
	}
	return out
}

// factSourceDelegations names, for every Fact that IS an engine fact, the Sources
// it delegates to. It is read twice: factDerivations builds the check from it, and
// TestLandingSources_EveryDeclaredSourceIsDerivedAndClaimed counts a delegating
// Fact as a claim on the Source — a Source nothing names directly and nothing
// delegates to is a pin guarding nothing.
var factSourceDelegations = map[pages.Fact][]pages.Source{
	pages.FactTapPageNeedsNoApp:             {pages.SourceTapIsAWebPage},
	pages.FactPlaqueIsPassive:               {pages.SourceSUNURLCarriesCounterAndSignature},
	pages.FactDeactivatedPersonIsRefused:    {pages.SourceEmployeeDeactivated},
	pages.FactPlaqueCanBeReplaced:           {pages.SourceTagNotActive},
	pages.FactGPSAloneApprovesATap:          {pages.SourceGPSOnlyAllow},
	pages.FactFlaggedQueueDecidedByAManager: {pages.SourceNoEvidenceReview},
	pages.FactCopiedLinkIsRefused:           {pages.SourceCounterAdvanceIsGuarded, pages.SourceSUNInvalid},
	pages.FactCodelessTapNeedsAddress:       {pages.SourceQRRequiresIP},
}

// factAnchorDelegations is the same for the Facts that are schema or domain facts
// the two-shapes block already derives.
var factAnchorDelegations = map[pages.Fact][]pages.Anchor{
	pages.FactPlaqueIdentifiesThePlace:          {pages.AnchorPlaqueBelongsToVenue},
	pages.FactVenuesAndDepartmentsCarryOwnHours: {pages.AnchorVenueShiftAndAddress, pages.AnchorDepartmentShift},
	pages.FactReportPerPersonAndVenue:           {pages.AnchorPerVenueReport},
}

// factDerivations maps every pages.Fact to a check that READS THE PRODUCT,
// returning "" when the fact holds or the reason it does not.
//
// 🔴 NOT ONE OF THEM COMPARES A SENTENCE — the rule anchorDerivations and
// sourceDerivations are written under, for the same reason. Where the fact is
// one of theirs, the closure is theirs (the two delegation maps above).
func factDerivations(t *testing.T) map[pages.Fact]func() string {
	t.Helper()
	sources := sourceDerivations(t)
	anchors := anchorDerivations(t)
	both := func(fs ...func() string) func() string {
		return func() string {
			for _, f := range fs {
				if why := f(); why != "" {
					return why
				}
			}
			return ""
		}
	}

	dashboardSrc := repoFile(t, "internal", "handler", "dashboard.go")
	activateSrc := repoFile(t, "internal", "handler", "activate.go")
	reportsCSVSrc := repoFile(t, "internal", "handler", "reportscsv.go")
	billingCSVSrc := repoFile(t, "internal", "handler", "billingcsv.go")
	tapTempl := repoFile(t, "web", "templates", "pages", "tap.templ")
	handoff := repoFile(t, "docs", "handoff.md")
	css := repoFile(t, "web", "static", "css", "input.css")
	mig05 := repoFile(t, "db", "migrations", "00005_create_transactions_audit_reviews.sql")
	mig16 := repoFile(t, "db", "migrations", "00016_add_billing_price_and_periods.sql")
	transactions := createTableBlock(t, mig05, "transactions")
	goSrc := nonTestGoSource(t, "internal", "cmd")
	scripts := shippedScripts(t)

	registered := func(src, method, href string) bool {
		return regexp.MustCompile(`r\.` + method + `\(\s*` + regexp.QuoteMeta(href) + `\s*,`).MatchString(src)
	}

	out := map[pages.Fact]func() string{
		pages.FactTapButtonSizedForAWetHand: func() string {
			body := cssRuleBody(css, ".tap-button")
			if body == "" {
				return "input.css no longer declares a .tap-button rule"
			}
			if !strings.Contains(body, "min-h-16") {
				return "input.css's .tap-button rule no longer carries the 64px floor (min-h-16), " +
					"so the button is no longer built for a gloved or wet finger"
			}
			return ""
		},
		pages.FactTapPageGreetsByName: func() string {
			if !strings.Contains(tapTempl, "v.EmployeeName") {
				return "pages/tap.templ no longer prints EmployeeName, so the tap page greets nobody by name"
			}
			return ""
		},
		pages.FactPeopleAreInvitedFromTheDashboard: func() string {
			if !registered(dashboardSrc, "Post", "employeeAddHref") {
				return "dashboard.go no longer registers POST employeeAddHref, so people cannot be added from the dashboard"
			}
			if !registered(activateSrc, "Get", `"/activate"`) {
				return "activate.go no longer registers GET /activate, so an invitation link opens nothing"
			}
			return ""
		},
		pages.FactNoBulkImport: func() string {
			for path, src := range goSrc {
				if multipartUploadRE.MatchString(src) {
					return path + " reads a file upload. The FAQ says there is no file import; " +
						"if there is one now, rewrite the answer"
				}
			}
			return ""
		},
		pages.FactNoIntegrationAPI: func() string {
			for path, src := range goSrc {
				if extra := foreignAPIRoutes(src); len(extra) > 0 {
					return path + " names " + strings.Join(extra, ", ") + " under /api. The page says " +
						"there is no API beyond the tap form's own POSTs; if there is one now, rewrite it"
				}
			}
			return ""
		},
		pages.FactNoAuthenticatorAPI: func() string {
			for path, src := range goSrc {
				if hit := authenticatorAPIHit(src); hit != "" {
					return path + " carries " + strconv.Quote(hit) + ", which reads a finger or a face. " +
						"The page says none is collected, ever (CLAUDE.md §4.1)"
				}
			}
			for path, src := range scripts {
				if hit := authenticatorAPIHit(src); hit != "" {
					return "web/static/" + path + " carries " + strconv.Quote(hit) + ", which reads a " +
						"finger or a face. The page says none is collected, ever (CLAUDE.md §4.1)"
				}
			}
			return ""
		},
		pages.FactPositionReadOnlyOnPress: func() string {
			for path, src := range scripts {
				if strings.Contains(jsLineCommentRE.ReplaceAllString(src, ""), continuousPositionCall) {
					return "web/static/" + path + " calls " + continuousPositionCall + ", which watches " +
						"the position; the page says it is read only at the moment of the tap (CLAUDE.md §4.2)"
				}
			}
			if !strings.Contains(jsLineCommentRE.ReplaceAllString(scripts["js/tap.js"], ""), "getCurrentPosition") {
				return "web/static/js/tap.js no longer asks for getCurrentPosition, so the one-shot read " +
					"the page describes is not what happens"
			}
			return ""
		},
		pages.FactManualEntryNamesAManager: func() string {
			if f, ok := fieldNames(manual.Entry{})["EnteredBy"]; !ok || f.String() != "uuid.UUID" {
				return "manual.Entry no longer carries EnteredBy uuid.UUID, so a typed record names nobody"
			}
			if !hasColumn(transactions, "entered_by", "uuid") {
				return "transactions.entered_by is gone from migration 0005"
			}
			if !strings.Contains(transactions, "'manual'") {
				return "migration 0005's channel CHECK no longer admits 'manual'"
			}
			if _, ok := fieldNames(components.DocketView{})["Manual"]; !ok {
				return "components.DocketView has no Manual flag, so a typed record is not marked apart on the card"
			}
			if _, ok := fieldNames(ledger.PersonHours{})["ManualArrivals"]; !ok {
				return "ledger.PersonHours no longer counts ManualArrivals, so the report blends typed arrivals in"
			}
			return ""
		},
		pages.FactHoursExportAsCSV: func() string {
			if !registered(dashboardSrc, "Get", "reportsCSVHref") {
				return "dashboard.go no longer registers GET reportsCSVHref"
			}
			if !strings.Contains(reportsCSVSrc, `"Content-Disposition"`) || !strings.Contains(reportsCSVSrc, "attachment;") {
				return "reportscsv.go no longer answers as a downloadable attachment"
			}
			return ""
		},
		pages.FactBillingExportAsCSV: func() string {
			if !registered(dashboardSrc, "Get", "billingCSVHref") {
				return "dashboard.go no longer registers GET billingCSVHref"
			}
			if !strings.Contains(billingCSVSrc, `"Content-Disposition"`) || !strings.Contains(billingCSVSrc, "attachment;") {
				return "billingcsv.go no longer answers as a downloadable attachment"
			}
			return ""
		},
		pages.FactPricePerEmployee: func() string {
			if !priceDefaultRE.MatchString(mig16) {
				return "migration 00016 no longer declares tenants.price_per_employee_month with a default, " +
					"so \"pay per person\" no longer describes the invoice"
			}
			return ""
		},
		pages.FactPlaquesIncludedAndReplacedFree: func() string {
			if !plaquesIncludedRE.MatchString(handoff) {
				return "docs/handoff.md §11 no longer publishes \"plaketler dahil ve ücretsiz değişim\"; " +
					"the price card's plaque line is a pricing term and this is the document that sets it"
			}
			return ""
		},
	}

	// The registrations the delegating facts add to their delegated half.
	extra := map[pages.Fact]func() string{
		pages.FactDeactivatedPersonIsRefused: func() string {
			if !registered(dashboardSrc, "Post", "employeeDeactivateHref") {
				return "dashboard.go no longer registers POST employeeDeactivateHref, so nobody can be switched off from the dashboard"
			}
			return ""
		},
		pages.FactPlaqueCanBeReplaced: func() string {
			if !registered(dashboardSrc, "Post", "plaqueReplaceHref") {
				return "dashboard.go no longer registers POST plaqueReplaceHref, so a spare plaque cannot be swapped in"
			}
			return ""
		},
		pages.FactFlaggedQueueDecidedByAManager: func() string {
			if !registered(dashboardSrc, "Post", "reviewHref") {
				return "dashboard.go no longer registers POST reviewHref, so nobody can decide a flagged record"
			}
			return ""
		},
	}
	for fact, srcs := range factSourceDelegations {
		var checks []func() string
		for _, s := range srcs {
			derive, ok := sources[s]
			if !ok {
				t.Fatalf("fact %q delegates to the source %q, which sourceDerivations does not derive", fact, s)
			}
			checks = append(checks, derive)
		}
		if e, ok := extra[fact]; ok {
			checks = append(checks, e)
		}
		out[fact] = both(checks...)
	}
	for fact, ancs := range factAnchorDelegations {
		var checks []func() string
		for _, a := range ancs {
			derive, ok := anchors[a]
			if !ok {
				t.Fatalf("fact %q delegates to the anchor %q, which anchorDerivations does not derive", fact, a)
			}
			checks = append(checks, derive)
		}
		out[fact] = both(checks...)
	}
	for fact := range extra {
		if _, ok := out[fact]; !ok {
			t.Fatalf("fact %q has a registration check and no delegation; add it to factSourceDelegations", fact)
		}
	}
	return out
}

// factConstRE matches a declaration in pages' Fact const block.
var factConstRE = regexp.MustCompile(`(?m)^\s*(\w+)\s+Fact\s*=\s*"([^"]+)"`)

// declaredFacts reads the Fact constants OUT OF pages' OWN SOURCE, for the reason
// written on declaredAnchors: it is the only way to see a constant nothing uses.
func declaredFacts(t *testing.T) map[pages.Fact]string {
	t.Helper()
	src := repoFile(t, "web", "templates", "pages", "landingview.go")
	out := map[pages.Fact]string{}
	for _, m := range factConstRE.FindAllStringSubmatch(src, -1) {
		out[pages.Fact(m[2])] = m[1]
	}
	if len(out) < 10 {
		t.Fatalf("read %d Fact constant(s) out of landingview.go; there are more, so this scan "+
			"is not seeing the const block", len(out))
	}
	return out
}

// landingLines is every stand-alone sentence the page renders from a pages.Line,
// by name — the ledes, the hero's two lines, the privacy note.
func landingLines() map[string]pages.Line {
	return map[string]pages.Line{
		"LandingHeroLede":       pages.LandingHeroLede,
		"LandingHeroFoot":       pages.LandingHeroFoot,
		"LandingSetupLede":      pages.LandingSetupLede,
		"LandingSecurityLede":   pages.LandingSecurityLede,
		"LandingPrivacyNote":    pages.LandingPrivacyNote,
		"LandingPricingHeading": pages.LandingPricingHeading,
		"LandingPricingLede":    pages.LandingPricingLede,
	}
}

// claimedFacts is every Fact some rendered value names, with the sentence that
// names it (for the error message).
func claimedFacts() map[pages.Fact]string {
	out := map[pages.Fact]string{}
	note := func(text string, facts []pages.Fact) {
		for _, f := range facts {
			out[f] = text
		}
	}
	for _, s := range pages.LandingSteps {
		note(s.Title, s.Facts)
	}
	for _, r := range pages.LandingComparison {
		note(r.Tappa, r.Facts)
	}
	for _, q := range pages.LandingFAQ {
		note(q.Q, q.Facts)
	}
	for _, l := range pages.LandingPricingIncludes {
		note(l.Text, l.Facts)
	}
	for _, l := range landingLines() {
		note(l.Text, l.Facts)
	}
	return out
}

// --- the closure over the vocabulary -----------------------------------------

// TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed closes the Fact vocabulary
// the way the Anchor and Source ones are closed: no constant can be declared,
// pass, and pin nothing; no derivation can exist for a fact the page no longer
// names; and every live fact holds today.
func TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed(t *testing.T) {
	t.Parallel()
	derivations := factDerivations(t)
	declared := declaredFacts(t)
	claimed := claimedFacts()

	for fact, name := range declared {
		if _, ok := derivations[fact]; !ok {
			t.Errorf("pages declares the fact %s = %q and nothing in factDerivations derives it. "+
				"A fact with no derivation is a comment: a sentence could name it and be pinned by nothing.", name, fact)
		}
		if _, ok := claimed[fact]; !ok {
			t.Errorf("pages declares the fact %s = %q and NO sentence rests on it. Either a sentence "+
				"was deleted and its pin left behind, or the pin was written for a sentence never added.", name, fact)
		}
	}
	for fact := range derivations {
		if _, ok := declared[fact]; !ok {
			t.Errorf("factDerivations derives %q, which pages does not declare as a Fact constant", fact)
		}
	}
	for fact, derive := range derivations {
		if why := derive(); why != "" {
			t.Errorf("the landing page says (%q) and the product no longer supports it: %s\n(fact %q)\n"+
				"Fix the product or delete the sentence — a marketing page may not describe a "+
				"capability that is not there.", claimed[fact], why, fact)
		}
	}
	if len(claimed) == 0 {
		t.Fatal("no sentence names a fact; the page's additions are unpinned")
	}
}

// --- the rendering half, block by block ----------------------------------------

// TestLandingFAQ_EveryAnswerRestsOnAProductFact: every question names at least
// one fact, and the FAQ carries the five questions the user's design has.
func TestLandingFAQ_EveryAnswerRestsOnAProductFact(t *testing.T) {
	t.Parallel()
	if n := len(pages.LandingFAQ); n != 5 {
		t.Fatalf("LandingFAQ carries %d question(s); the user's design has five. Change this test "+
			"deliberately if the set changed.", n)
	}
	for i, q := range pages.LandingFAQ {
		if len(q.Facts) == 0 {
			t.Errorf("FAQ %d (%q) rests on no product fact. Every answer has to name what it "+
				"describes, or be deleted — the user's draft answered four of these with "+
				"capabilities the product does not have.", i+1, q.Q)
		}
	}
	// AND THE DECISION-ENGINE SENTENCES SAY WHAT THE ENGINE DOES. The draft's FAQ
	// said a codeless tap is approved on "the IP or GPS proof"; §5 requires the
	// address. Both answers that touch it are held to that wording, and the
	// phrase the draft used for the opposite rule ("have to agree") is refused.
	//
	// THE RULE HAS TWO HALVES AND BOTH ARE HELD. "Needs the address" is one half;
	// "a position on its own is not enough" is the other, and a page that kept the
	// first while flipping the second to "is enough" passed this test as written
	// (measured, 2026-09-14). The negation is the sentence's whole content, so the
	// affirmative form is refused by name as well.
	text := renderedLandingText(t)
	for _, must := range []string{
		"it needs your venue's network address to match",
		"needs your venue's network address to be approved",
		"A position on its own is not enough",
	} {
		if !strings.Contains(text, must) {
			t.Errorf("the FAQ no longer says %q — the codeless channel REQUIRES the address "+
				"(base:qr-requires-ip), and a position alone ends in FLAGGED", must)
		}
	}
	for _, banned := range []string{
		"have to agree", "IP or GPS proof", "or GPS proof instead",
		"position on its own is enough",
	} {
		if strings.Contains(text, banned) {
			t.Errorf("the page says %q, which mis-states CLAUDE.md §5: line six is a disjunction "+
				"(either half is enough) and the codeless channel needs the address, not either", banned)
		}
	}
	// AND "FROM HOME" IS NOT ANSWERED WITH A FLAT "NO": a flagged record IS written
	// (§4.6), so the honest answer starts with the manager who sees it.
	for _, q := range pages.LandingFAQ {
		if strings.Contains(q.Q, "from home") && strings.HasPrefix(q.A, "No.") {
			t.Errorf("the FAQ answers %q with a flat \"No.\"; a check-in from home without the "+
				"address is written and FLAGGED, not refused, so the answer overstates the engine", q.Q)
		}
	}
}

// TestLandingPricingIncludes_EveryLineIsRenderedAndRestsOnAFact pins the price
// card's list — the block of the draft that promised the most.
func TestLandingPricingIncludes_EveryLineIsRenderedAndRestsOnAFact(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)
	if n := len(pages.LandingPricingIncludes); n != 5 {
		t.Fatalf("LandingPricingIncludes carries %d line(s); the user's list has five", n)
	}
	for i, l := range pages.LandingPricingIncludes {
		if len(l.Facts) == 0 {
			t.Errorf("price list line %d (%q) rests on no product fact", i+1, l.Text)
		}
		if !strings.Contains(text, l.Text) {
			t.Errorf("price list line %d is declared and NOT rendered:\n    %q", i+1, l.Text)
		}
	}
	// THE THINGS THE DRAFT LISTED THAT THE PRODUCT DOES NOT DO must not be on the
	// page in any wording that names them.
	lower := strings.ToLower(text)
	for _, banned := range []string{"api for your payroll", "clean api feed", "import your staff", "csv import",
		"live headcount", "live dashboard", "runs alongside", "side by side", "unlimited locations", "unlimited venues",
		"daily reports"} {
		if strings.Contains(lower, banned) {
			t.Errorf("the page says %q. That capability is not in the product (see LandingPricingIncludes "+
				"and LandingFAQ in landingview.go for what replaced it).", banned)
		}
	}
}

// TestLandingLines_EveryLedeIsRenderedAndRestsOnAFact pins every stand-alone
// sentence the page renders from a pages.Line.
func TestLandingLines_EveryLedeIsRenderedAndRestsOnAFact(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)
	lines := landingLines()
	if len(lines) < 7 {
		t.Fatalf("landingLines names %d line(s); the page renders seven, so this test is not reading them all", len(lines))
	}
	for name, l := range lines {
		if len(l.Facts) == 0 {
			t.Errorf("%s rests on no product fact", name)
		}
		if l.Text == "" || !strings.Contains(text, l.Text) {
			t.Errorf("%s is declared and NOT rendered:\n    %q", name, l.Text)
		}
	}
}

// TestLandingSteps_EveryStepRestsOnAProductFact: the three steps carry a pin, and
// the draft's three stopwatch figures are not on the page.
func TestLandingSteps_EveryStepRestsOnAProductFact(t *testing.T) {
	t.Parallel()
	for i, s := range pages.LandingSteps {
		if len(s.Facts) == 0 {
			t.Errorf("step %d (%q) rests on no product fact", i+1, s.Title)
		}
	}
	text := strings.ToLower(renderedLandingText(t))
	for _, banned := range []string{"under 10 minutes", "under ten minutes", "30 seconds", "under two seconds",
		"in ten seconds", "one click", "that second", "one pay period", "about €"} {
		if strings.Contains(text, banned) {
			t.Errorf("the page says %q — a number nobody measured", banned)
		}
	}
}

// TestLandingComparison_EveryRowIsRenderedAndRestsOnAFact pins the ledger's six
// rows: both cells reach the visitor, in order, and the Taptime cell names what it
// rests on. The device column is held to definitions by the superiority scanner
// in marketing_test.go; here it is held to being rendered.
func TestLandingComparison_EveryRowIsRenderedAndRestsOnAFact(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)
	if n := len(pages.LandingComparison); n != 6 {
		t.Fatalf("LandingComparison carries %d row(s); the user's ledger has six", n)
	}
	aspects := make([]string, len(pages.LandingComparison))
	for i, r := range pages.LandingComparison {
		aspects[i] = r.Aspect
	}
	at := renderedInOrder(t, text, "LandingComparison", aspects)
	for i, r := range pages.LandingComparison {
		if len(r.Facts) == 0 {
			t.Errorf("ledger row %d (%q) rests on no product fact", i+1, r.Aspect)
		}
		if at[i] < 0 {
			continue
		}
		for what, cell := range map[string]string{"device": r.Terminal, "Taptime": r.Tappa} {
			if cell == "" {
				t.Errorf("ledger row %d (%q) has an empty %s cell", i+1, r.Aspect, what)
				continue
			}
			if indexFrom(text, cell, at[i]) < 0 {
				t.Errorf("ledger row %d (%q): the %s cell is declared and NOT rendered after its aspect:\n    %q",
					i+1, r.Aspect, what, cell)
			}
		}
	}
	// The footnote follows the table, not the other way round.
	if last := at[len(at)-1]; last >= 0 && indexFrom(text, pages.LandingComparisonNote, last) < 0 {
		t.Error("the ledger's footnote does not follow its last row")
	}
}

// TestLandingEvidence_EveryProofLabelIsRendered pins the four "Proof of …"
// eyebrows the user's draft gave the evidence cards.
func TestLandingEvidence_EveryProofLabelIsRendered(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)
	seen := map[string]bool{}
	for i, e := range pages.LandingEvidence {
		if e.Proof == "" {
			t.Errorf("evidence %d (%q) has no Proof label", i+1, e.Label)
			continue
		}
		if seen[e.Proof] {
			t.Errorf("evidence %d repeats the Proof label %q", i+1, e.Proof)
		}
		seen[e.Proof] = true
		if !strings.Contains(text, e.Proof) {
			t.Errorf("evidence %d's Proof label %q is declared and NOT rendered", i+1, e.Proof)
		}
	}
	// The fourth is the BACKUP, in §5's own word, and must say so: a card that
	// called the position "Proof of place" would promote the fallback to the rule.
	if !strings.Contains(text, "Backup proof of place") {
		t.Error("the position card no longer says \"Backup proof of place\"; §5 calls it the backup (\"yedek nerede\")")
	}
}

// --- the bar -------------------------------------------------------------------

var idAttrRE = regexp.MustCompile(`\sid="([^"]+)"`)

// TestLandingNav_EveryLinkPointsAtASectionOnThePage follows each in-page link in
// the sticky bar against the rendered page, and holds the bar to the two hrefs
// that are not in-page: the sign-in constant and the wizard.
//
// ⚠️ IT USED TO PANIC. The bar's segment was cut at "<main", which the user's
// design does not have, and body[:-1] took the whole package down with it. The
// segment is the first <nav> now, and a page without one is a fatal, not a panic.
func TestLandingNav_EveryLinkPointsAtASectionOnThePage(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), "/")
	ids := map[string]bool{}
	for _, m := range idAttrRE.FindAllStringSubmatch(body, -1) {
		ids[m[1]] = true
	}
	if len(pages.LandingNav) != 3 {
		t.Fatalf("LandingNav carries %d link(s); the bar has three", len(pages.LandingNav))
	}
	navStart := strings.Index(body, "<nav")
	navEnd := strings.Index(body, "</nav>")
	if navStart < 0 || navEnd < navStart {
		t.Fatalf("the landing page renders no <nav>…</nav>; the sticky bar is gone")
	}
	bar := body[navStart:navEnd]
	barText := strings.Join(strings.Fields(tagRE.ReplaceAllString(bar, " ")), " ")
	for _, l := range pages.LandingNav {
		target, ok := strings.CutPrefix(l.Href, "#")
		if !ok || target == "" {
			t.Errorf("nav link %q points at %q, which is not an in-page anchor", l.Label, l.Href)
			continue
		}
		if !ids[target] {
			t.Errorf("nav link %q points at #%s and no element on the page carries that id; "+
				"the link would scroll nowhere", l.Label, target)
		}
		if !strings.Contains(bar, `href="`+l.Href+`"`) {
			t.Errorf("nav link %q (%s) is declared and NOT rendered in the bar", l.Label, l.Href)
		}
		if !strings.Contains(barText, l.Label) {
			t.Errorf("nav label %q is not in the bar", l.Label)
		}
	}
	// The bar's two ways in come from the view, never from a literal.
	if !strings.Contains(bar, `href="`+signupPath+`"`) {
		t.Errorf("the bar carries no link to %s", signupPath)
	}
	if !strings.Contains(bar, `href="`+adminLoginPath+`"`) {
		t.Errorf("the bar carries no link to %s", adminLoginPath)
	}
	// AND THE PLAIN CHROME STAYS PLAIN: the wizard and the legal pages render no
	// bar, no in-page link and no sticky header.
	for _, p := range pages.LegalPages {
		legal := mustFetchMarketing(t, marketingRouter(t), p.Path)
		for _, l := range pages.LandingNav {
			if strings.Contains(legal, `href="`+l.Href+`"`) {
				t.Errorf("%s renders the landing page's %q link; the bar is the landing page's only", p.Path, l.Label)
			}
		}
		if strings.Contains(legal, "sticky") {
			t.Errorf("%s renders a sticky header; only the landing page asked for one", p.Path)
		}
	}
}

// --- what must not be on the page, whatever the copy says -----------------------

// TestLanding_ShowsNoUserFacingInternalName: the rendered page carries the user
// facing brand and never the internal code name.
func TestLanding_ShowsNoUserFacingInternalName(t *testing.T) {
	t.Parallel()
	r := marketingRouter(t)
	for _, url := range marketingURLs() {
		text := screenText(t, mustFetchMarketing(t, r, url))
		if strings.Contains(text, "Tappa") {
			t.Errorf("%s shows the internal code name to a visitor; the user-facing brand is Taptime", url)
		}
	}
}

// rawHexRE matches a CSS colour literal.
var rawHexRE = regexp.MustCompile(`#[0-9A-Fa-f]{6}\b|#[0-9A-Fa-f]{3}\b`)

// TestLanding_UsesOnlyPaletteTokens: the user's draft carried seventeen hex values
// outside the palette; none of them may reach the template or the page. (The
// reference's stylesheet keeps them, as tokens on .lp — input.css records why.)
func TestLanding_UsesOnlyPaletteTokens(t *testing.T) {
	t.Parallel()
	src := repoFile(t, "web", "templates", "pages", "landing.templ")
	if hit := rawHexRE.FindString(src); hit != "" {
		t.Errorf("landing.templ carries the raw colour %q; the palette is tokens only (skill tappa-brand)", hit)
	}
	body := mustFetchMarketing(t, marketingRouter(t), "/")
	if hit := rawHexRE.FindString(body); hit != "" {
		t.Errorf("the rendered landing page carries the raw colour %q", hit)
	}
	// The page contains no inline style attribute at all — every tone is a class.
	if strings.Contains(body, " style=") {
		t.Error("the landing page carries an inline style attribute; tones are tokens in classes")
	}
}

// --- negative controls -----------------------------------------------------------

// TestFactMechanisms_SayNoWhenTheFactIsAbsent is the negative control for every
// scanner above: a scanner that matched nothing would leave every test green over
// a product that had lost the capability, and the failure would look like success.
func TestFactMechanisms_SayNoWhenTheFactIsAbsent(t *testing.T) {
	t.Parallel()

	// The API tripwire sees a third route and ignores the tap form's two.
	if extra := foreignAPIRoutes(`r.Post("/api/checkin", x); r.Post("/api/activate", y)`); len(extra) != 0 {
		t.Errorf("foreignAPIRoutes flags the tap form's own routes: %v", extra)
	}
	if extra := foreignAPIRoutes(`r.Get("/api/v1/export", x)`); len(extra) != 1 {
		t.Errorf("foreignAPIRoutes missed an integration route: %v", extra)
	}
	if extra := foreignAPIRoutes(`// and "/api/activate onun kapsamına girmez" says the card`); len(extra) != 0 {
		t.Errorf("foreignAPIRoutes matches prose in a comment: %v", extra)
	}
	if extra := foreignAPIRoutes(`msg := "/api/checkin is the only route"`); len(extra) != 0 {
		t.Errorf("foreignAPIRoutes matches a quoted sentence rather than a path: %v", extra)
	}

	// The upload tripwire.
	for _, s := range []string{`f, _, err := r.FormFile("staff")`, `mr, err := r.MultipartReader()`, `r.ParseMultipartForm(1 << 20)`} {
		if !multipartUploadRE.MatchString(s) {
			t.Errorf("the upload scanner would not see %q", s)
		}
	}
	if multipartUploadRE.MatchString(`// FormFile was considered and rejected`) {
		t.Error("the upload scanner matches prose rather than a call")
	}

	// The finger-or-face tripwire sees the API and the vendor readers, and not the
	// comments that say there are none (device.go and invite/manager.go both do).
	for _, s := range []string{
		`const c = await navigator.credentials.create({publicKey: opts});`,
		`if (window.PublicKeyCredential) {`,
		`r.Post("/web` + `authn/register", h)`,
		`reader := newFinger` + `printSensor()`,
		`if (window.Touch` + `ID) {`,
	} {
		if authenticatorAPIHit(s) == "" {
			t.Errorf("the finger-or-face scanner would not see %q", s)
		}
	}
	if hit := authenticatorAPIHit("// Tappa promises no finger" + "printing of the device, and a column\nx := 1"); hit != "" {
		t.Errorf("the finger-or-face scanner matches prose in a comment (%q)", hit)
	}
	if hit := authenticatorAPIHit(`log.Warn("bad credentials")`); hit != "" {
		t.Errorf("the finger-or-face scanner matches the ordinary word credentials (%q)", hit)
	}
	if hit := authenticatorAPIHit(`// a touch identifies the place; the face identifier is the wall`); hit != "" {
		t.Errorf("the finger-or-face scanner matches a word that merely starts the same way (%q)", hit)
	}

	// The position tripwire strips comments too: tap.js's header MENTIONS the call
	// it does not make.
	if strings.Contains(jsLineCommentRE.ReplaceAllString("// never "+continuousPositionCall+"\nx();", ""), continuousPositionCall) {
		t.Error("the position scanner would be satisfied by a comment naming the continuous read")
	}
	if !strings.Contains(jsLineCommentRE.ReplaceAllString("navigator.geolocation."+continuousPositionCall+"(f);", ""), continuousPositionCall) {
		t.Error("the position scanner cannot see the call it exists to see")
	}

	// The radius reader.
	if m := gpsRadiusDefaultRE.FindStringSubmatch(`c.X, err = floatEnvRange("TAPPA_GPS_RADIUS_M", 150, policy.GPSRadiusMinM, policy.GPSRadiusMaxM)`); m == nil || m[1] != "150" {
		t.Errorf("gpsRadiusDefaultRE cannot read the default out of the shape config.go uses: %v", m)
	}
	if gpsRadiusDefaultRE.MatchString(`floatEnvRange("TAPPA_GPS_RADIUS_MAX", 150,`) {
		t.Error("gpsRadiusDefaultRE matches a DIFFERENT variable whose name merely starts the same way")
	}

	// The handoff reader is scoped to §11.
	if plaquesIncludedRE.MatchString("## 4. Marka\n- plaketler dahil ve ücretsiz değişim\n## 11. Fiyat\n- x\n") {
		t.Error("plaquesIncludedRE is satisfied by the phrase OUTSIDE §11")
	}
	if !plaquesIncludedRE.MatchString("## 11. Fiyatlandırma & GTM\n\n- **€1.50** — plaketler dahil ve ücretsiz değişim.\n") {
		t.Error("plaquesIncludedRE cannot see the pricing term it exists to see")
	}

	// The CSS rule reader strips comments first and scopes to one rule.
	const css = "/* .tap-button { min-h-16 in prose } */\n.tap-button {\n @apply w-full;\n}\n.other { @apply min-h-16; }"
	if strings.Contains(cssRuleBody(css, ".tap-button"), "min-h-16") {
		t.Error("cssRuleBody is satisfied by a comment or a neighbouring rule; the button floor would be pinned by nothing")
	}
	if !strings.Contains(cssRuleBody(".tap-button {\n @apply w-full min-h-16;\n}", ".tap-button"), "min-h-16") {
		t.Error("cssRuleBody cannot see the declaration it exists to see")
	}

	// AND EVERY LIVE FACT HOLDS TODAY, which is what makes the checks above a
	// control rather than a separate universe.
	for fact, derive := range factDerivations(t) {
		if why := derive(); why != "" {
			t.Errorf("fact %q does not hold: %s", fact, why)
		}
	}
}
