package handler

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// THE PINS FOR WHAT THE 2026-09-12 RESTYLE ADDED TO THE LANDING PAGE.
//
// 🔴 THIS FILE EXISTS BECAUSE THE USER'S OWN DRAFT OF THE PAGE WAS MOSTLY MADE OF
// SENTENCES THE PRODUCT DOES NOT KEEP — an API, a file import, a live headcount,
// plaques nobody can yet order, a mis-stated decision rule — and the four REDs on
// this page's record all came from exactly that class. The restyle carried the
// draft's look and voice; every sentence it carried had to arrive with a pin.
//
// THE TWO INVARIANTS ARE marketing_claims_test.go's, deliberately the same two:
// every line REACHES A VISITOR (text-matching is legitimate here: the question is
// "was it rendered", not "is it true"), and every line RESTS ON A PRODUCT FACT that
// is still there. pages.Fact is the third vocabulary beside Anchor and Source, and
// landingview.go records why a third one was needed: the two older ones are closed
// by tests this task may not edit. Where a Fact IS a fact those vocabularies already
// derive, the derivation below DELEGATES to theirs — one reading of the product,
// not two that could disagree.
//
// ⚠️ THE LIMIT IS THE ONE WRITTEN ON pages.Anchor, WORD FOR WORD: naming a Fact
// does not make a sentence true. This is a ratchet against drift. Three of the
// facts are TRIPWIRES FOR AN ABSENCE (no import, no API, no edit of a record):
// they fail the day the capability appears, which is the day the sentence that
// says "there is none" has to be rewritten.

// --- the derivations ---------------------------------------------------------

// multipartUploadRE matches the three ways non-test Go reads a file upload.
var multipartUploadRE = regexp.MustCompile(`\.(?:FormFile|MultipartReader|ParseMultipartForm)\(`)

// apiLiteralRE matches a "/api…" route literal in Go source: a path, which is
// what a registration carries, and not a quoted sentence that happens to start
// with one (ratelimit.go quotes a card's Turkish sentence about /api/activate).
var apiLiteralRE = regexp.MustCompile(`"(/api(?:/[A-Za-z0-9_.{}-]+)*/?)"`)

// goLineCommentRE strips `//` comments, line-locally, before a scan — a scan a
// comment can satisfy pins nothing (the rule goSwitchArm records).
var goLineCommentRE = regexp.MustCompile(`(?m)//.*$`)

// tapFormAPIRoutes are the two POSTs the tap surface itself makes. Anything else
// under /api would be an integration API, and the page says there is none.
var tapFormAPIRoutes = map[string]bool{"/api/checkin": true, "/api/activate": true}

// transactionsGrantRE matches the GRANT that gives the application role its
// privileges on transactions. §4.3: SELECT and INSERT, nothing else.
var transactionsGrantRE = regexp.MustCompile(`(?m)^\s*GRANT\s+([A-Z, ]+?)\s+ON\s+transactions\s+TO\s+tappa_app\s*;`)

// createTableRE and forceRLSRE are the two halves of "every table forces RLS".
var (
	createTableRE = regexp.MustCompile(`(?im)^\s*CREATE TABLE (?:IF NOT EXISTS )?(\w+)\s*\(`)
	forceRLSRE    = regexp.MustCompile(`(?im)^\s*ALTER TABLE (\w+)\s+FORCE ROW LEVEL SECURITY\s*;`)
)

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

// grantIsAppendOnly reports whether a GRANT ... ON transactions names only SELECT
// and INSERT. Package level so the negative control can prove it says no.
func grantIsAppendOnly(migration string) (bool, string) {
	m := transactionsGrantRE.FindStringSubmatch(migration)
	if m == nil {
		return false, "no GRANT ... ON transactions TO tappa_app in a shape this scan can read"
	}
	for _, p := range strings.Split(m[1], ",") {
		switch strings.TrimSpace(p) {
		case "SELECT", "INSERT":
		default:
			return false, "the application role is granted " + strings.TrimSpace(p) + " on transactions"
		}
	}
	return true, ""
}

// tablesWithoutForcedRLS returns every table CREATEd across the migrations that no
// migration FORCEs row-level security on. Package level for the negative control.
func tablesWithoutForcedRLS(migrations []string) []string {
	created := map[string]bool{}
	forced := map[string]bool{}
	for _, sql := range migrations {
		for _, m := range createTableRE.FindAllStringSubmatch(sql, -1) {
			created[strings.ToLower(m[1])] = true
		}
		for _, m := range forceRLSRE.FindAllStringSubmatch(sql, -1) {
			forced[strings.ToLower(m[1])] = true
		}
	}
	var out []string
	for t := range created {
		if !forced[t] {
			out = append(out, t)
		}
	}
	return out
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
	// and no upload, and agree with both tripwires.
	if len(out) < 40 {
		t.Fatalf("read %d non-test Go file(s); the product has far more, so this walk is "+
			"reading the wrong tree", len(out))
	}
	return out
}

// allMigrations reads every goose migration.
func allMigrations(t *testing.T) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join("..", "..", "db", "migrations", "*.sql"))
	if err != nil || len(paths) < 10 {
		t.Fatalf("found %d migration(s) (%v); the schema has more, so this glob is wrong", len(paths), err)
	}
	var out []string
	for _, p := range paths {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("reading %s: %v", p, err)
		}
		out = append(out, string(raw))
	}
	return out
}

// factDerivations maps every pages.Fact to a check that READS THE PRODUCT,
// returning "" when the fact holds or the reason it does not.
//
// 🔴 NOT ONE OF THEM COMPARES A SENTENCE — the rule anchorDerivations and
// sourceDerivations are written under, for the same reason. Where the fact is
// one of theirs, the closure is theirs.
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
	tapSrc := repoFile(t, "internal", "domain", "tap", "decide.go")
	poolSrc := repoFile(t, "internal", "db", "pool.go")
	css := repoFile(t, "web", "static", "css", "input.css")
	mig05 := repoFile(t, "db", "migrations", "00005_create_transactions_audit_reviews.sql")
	mig16 := repoFile(t, "db", "migrations", "00016_add_billing_price_and_periods.sql")
	transactions := createTableBlock(t, mig05, "transactions")
	goSrc := nonTestGoSource(t, "internal", "cmd")
	migrations := allMigrations(t)

	registered := func(src, method, href string) bool {
		return regexp.MustCompile(`r\.` + method + `\(\s*` + regexp.QuoteMeta(href) + `\s*,`).MatchString(src)
	}

	return map[pages.Fact]func() string{
		pages.FactTapPageNeedsNoApp: sources[pages.SourceTapIsAWebPage],
		pages.FactPlaqueIsPassive:   sources[pages.SourceSUNURLCarriesCounterAndSignature],
		pages.FactTapButtonSizedForAWetHand: func() string {
			body := cssRuleBody(css, ".tap-button")
			if body == "" {
				return "input.css no longer declares a .tap-button rule"
			}
			if !strings.Contains(body, "min-h-16") {
				return "input.css's .tap-button rule no longer carries the 64px floor (min-h-16), " +
					"so the button is no longer sized for a gloved or wet finger"
			}
			return ""
		},
		pages.FactDirectionTogglesOnLastOpenEntry: func() string {
			if _, ok := fieldNames(tap.Input{})["LastOpenIn"]; !ok {
				return "tap.Input no longer carries LastOpenIn, so direction cannot toggle against the last open entry"
			}
			if !strings.Contains(tapSrc, "func resolveDirection(") {
				return "internal/domain/tap no longer has resolveDirection; the direction rule the page describes is gone"
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
		pages.FactFlaggedQueueDecidedByAManager: both(sources[pages.SourceNoEvidenceReview], func() string {
			if !registered(dashboardSrc, "Post", "reviewHref") {
				return "dashboard.go no longer registers POST reviewHref, so nobody can decide a flagged record"
			}
			return ""
		}),
		pages.FactCopiedLinkIsRefused:     both(sources[pages.SourceCounterAdvanceIsGuarded], sources[pages.SourceSUNInvalid]),
		pages.FactCodelessTapNeedsAddress: sources[pages.SourceQRRequiresIP],
		pages.FactVenuesAndDepartmentsCarryOwnHours: both(anchors[pages.AnchorVenueShiftAndAddress],
			anchors[pages.AnchorDepartmentShift]),
		pages.FactPricePerEmployee: func() string {
			if !priceDefaultRE.MatchString(mig16) {
				return "migration 00016 no longer declares tenants.price_per_employee_month with a default, " +
					"so \"pay per person\" no longer describes the invoice"
			}
			return ""
		},
		pages.FactOpenEntriesAreListedNotClosed: func() string {
			f, ok := fieldNames(ledger.Report{})["Open"]
			if !ok || f.Kind() != reflect.Slice {
				return "ledger.Report no longer carries Open []OpenEntry, so open entries are not listed"
			}
			if !strings.Contains(dashboardSrc, "a.anomaliesSection") {
				return "dashboard.go no longer mounts the anomalies section"
			}
			return ""
		},
		pages.FactRecordsAreAppendOnly: func() string {
			if ok, why := grantIsAppendOnly(mig05); !ok {
				return why + " (CLAUDE.md §4.3)"
			}
			return ""
		},
		pages.FactEveryTableIsIsolated: func() string {
			if missing := tablesWithoutForcedRLS(migrations); len(missing) > 0 {
				return "these tables are created without FORCE ROW LEVEL SECURITY: " + strings.Join(missing, ", ")
			}
			if !strings.Contains(poolSrc, "rolbypassrls") {
				return "internal/db/pool.go no longer reads rolbypassrls, so a bypassing role would be accepted"
			}
			return ""
		},
		pages.FactReportPerPersonAndVenue: anchors[pages.AnchorPerVenueReport],
	}
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
	for _, q := range pages.LandingFAQ {
		note(q.Q, q.Facts)
	}
	for _, l := range pages.LandingPricingIncludes {
		note(l.Text, l.Facts)
	}
	note(pages.LandingSetupLede.Text, pages.LandingSetupLede.Facts)
	note(pages.LandingPricingHeading.Text, pages.LandingPricingHeading.Facts)
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
		t.Fatal("no sentence names a fact; the restyle's additions are unpinned")
	}
}

// --- the rendering half, block by block ----------------------------------------

// TestLandingFAQ_EveryAnswerRestsOnAProductFact: every question names at least
// one fact, and the FAQ carries the ten questions the 2026-09-12 restyle settled on
// (six from handoff §9, four of the user's).
func TestLandingFAQ_EveryAnswerRestsOnAProductFact(t *testing.T) {
	t.Parallel()
	if n := len(pages.LandingFAQ); n != 10 {
		t.Fatalf("LandingFAQ carries %d question(s); the restyle settled on ten. Change this test "+
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
	text := renderedLandingText(t)
	for _, must := range []string{
		"it needs your venue's network address to match",
		"needs your venue's network address to be approved",
	} {
		if !strings.Contains(text, must) {
			t.Errorf("the FAQ no longer says %q — the codeless channel REQUIRES the address "+
				"(base:qr-requires-ip), and a position alone ends in FLAGGED", must)
		}
	}
	for _, banned := range []string{"have to agree", "IP or GPS proof", "or GPS proof instead"} {
		if strings.Contains(text, banned) {
			t.Errorf("the page says %q, which mis-states CLAUDE.md §5: line six is a disjunction "+
				"(either half is enough) and the codeless channel needs the address, not either", banned)
		}
	}
}

// TestLandingPricingIncludes_EveryLineIsRenderedAndRestsOnAFact pins the price
// card's list — the block of the draft that promised the most.
func TestLandingPricingIncludes_EveryLineIsRenderedAndRestsOnAFact(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)
	if n := len(pages.LandingPricingIncludes); n < 3 {
		t.Fatalf("LandingPricingIncludes carries %d line(s); the list has four", n)
	}
	for i, l := range pages.LandingPricingIncludes {
		if len(l.Facts) == 0 {
			t.Errorf("price list line %d (%q) rests on no product fact", i+1, l.Text)
		}
		if !strings.Contains(text, l.Text) {
			t.Errorf("price list line %d is declared and NOT rendered:\n    %q", i+1, l.Text)
		}
	}
	// THE THREE THINGS THE DRAFT LISTED THAT THE PRODUCT DOES NOT DO must not be on
	// the page in any wording that names them.
	lower := strings.ToLower(text)
	for _, banned := range []string{"api for your payroll", "clean api feed", "import your staff", "csv import",
		"live headcount", "runs alongside", "side by side", "unlimited locations", "unlimited venues"} {
		if strings.Contains(lower, banned) {
			t.Errorf("the page says %q. That capability is not in the product (see LandingPricingIncludes "+
				"and LandingFAQ in landingview.go for what replaced it).", banned)
		}
	}
}

// TestLandingLines_EveryLedeIsRenderedAndRestsOnAFact pins the two sentences
// carried from the draft as headings and ledes.
func TestLandingLines_EveryLedeIsRenderedAndRestsOnAFact(t *testing.T) {
	t.Parallel()
	text := renderedLandingText(t)
	for name, l := range map[string]pages.Line{
		"LandingSetupLede":      pages.LandingSetupLede,
		"LandingPricingHeading": pages.LandingPricingHeading,
	} {
		if len(l.Facts) == 0 {
			t.Errorf("%s rests on no product fact", name)
		}
		if l.Text == "" || !strings.Contains(text, l.Text) {
			t.Errorf("%s is declared and NOT rendered:\n    %q", name, l.Text)
		}
	}
}

// TestLandingSteps_EveryStepRestsOnAProductFact closes the gap
// marketing_claims_test.go stated for LandingSteps: the three steps now carry a pin.
func TestLandingSteps_EveryStepRestsOnAProductFact(t *testing.T) {
	t.Parallel()
	for i, s := range pages.LandingSteps {
		if len(s.Facts) == 0 {
			t.Errorf("step %d (%q) rests on no product fact", i+1, s.Title)
		}
	}
	// The draft's numbers for these three steps are not on the page.
	text := strings.ToLower(renderedLandingText(t))
	for _, banned := range []string{"under 10 minutes", "under ten minutes", "30 seconds", "under two seconds", "in ten seconds"} {
		if strings.Contains(text, banned) {
			t.Errorf("the page says %q — a number nobody measured", banned)
		}
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
func TestLandingNav_EveryLinkPointsAtASectionOnThePage(t *testing.T) {
	t.Parallel()
	body := mustFetchMarketing(t, marketingRouter(t), "/")
	ids := map[string]bool{}
	for _, m := range idAttrRE.FindAllStringSubmatch(body, -1) {
		ids[m[1]] = true
	}
	if len(pages.LandingNav) < 3 {
		t.Fatalf("LandingNav carries %d link(s); the bar has three", len(pages.LandingNav))
	}
	text := screenText(t, body)
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
		if !strings.Contains(body, `href="`+l.Href+`"`) {
			t.Errorf("nav link %q (%s) is declared and NOT rendered", l.Label, l.Href)
		}
		if !strings.Contains(text, l.Label) {
			t.Errorf("nav label %q is not on the page", l.Label)
		}
	}
	// The bar's two ways in come from the view, never from a literal.
	head := body[:strings.Index(body, "<main")]
	if !strings.Contains(head, `href="`+signupPath+`"`) {
		t.Errorf("the bar carries no link to %s", signupPath)
	}
	if !strings.Contains(head, `href="`+adminLoginPath+`"`) {
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
// outside the palette; none of them may reach the template.
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

	// The append-only grant.
	for _, tc := range []struct {
		name, sql string
		want      bool
	}{
		{"select+insert", "GRANT SELECT, INSERT ON transactions TO tappa_app;", true},
		{"update added", "GRANT SELECT, INSERT, UPDATE ON transactions TO tappa_app;", false},
		{"delete added", "GRANT SELECT, INSERT, DELETE ON transactions TO tappa_app;", false},
		{"all", "GRANT ALL ON transactions TO tappa_app;", false},
		{"grant missing", "GRANT SELECT ON tags TO tappa_app;", false},
	} {
		if got, _ := grantIsAppendOnly(tc.sql); got != tc.want {
			t.Errorf("grantIsAppendOnly(%s) = %v, want %v — §4.3's whole pin is this scan", tc.name, got, tc.want)
		}
	}

	// Every table forces RLS.
	if missing := tablesWithoutForcedRLS([]string{
		"CREATE TABLE a (\n id uuid\n);\nALTER TABLE a FORCE ROW LEVEL SECURITY;",
		"CREATE TABLE IF NOT EXISTS b (\n id uuid\n);",
	}); len(missing) != 1 || missing[0] != "b" {
		t.Errorf("tablesWithoutForcedRLS reports %v; it must name exactly the table without FORCE", missing)
	}

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
