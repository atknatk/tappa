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
// does not make a sentence true. This is a ratchet against drift. Three of the
// facts are TRIPWIRES FOR AN ABSENCE (no import, no finger or face reader, no
// background position): they fail the day the capability appears, which is the day
// the sentence that says "there is none" has to be rewritten.
//
// 🔴 A TRIPWIRE IS ONLY AS WIDE AS WHAT IT SCANS. The fourth, "no integration API",
// read "/api…" literals; M11 mounted the API at /v1 and it stayed green for the whole
// milestone over a FAQ that said "There is no API." It is retired, the API's three
// facts are derived from /v1 itself, and TestLanding_ClaimsNoAbsentAPIWhileV1IsMounted
// reads the PAGE for the sentence rather than the code for one route spelling.

// --- the derivations ---------------------------------------------------------

// multipartUploadRE matches the three ways non-test Go reads a file upload.
var multipartUploadRE = regexp.MustCompile(`\.(?:FormFile|MultipartReader|ParseMultipartForm)\(`)

// logoUploadFile is the ONE file allowed to read a multipart body (M10 WL-7, ADR 0024
// §6): the brand logo handler, as a path from the repository root -- the spelling
// nonTestGoSource keys its map by. The fact the FAQ states is about a file IMPORT of
// people or records; a business's logo is not one, and the derivation was rewritten in
// the WL-7 change to say so -- the sentence on the page did not change.
var logoUploadFile = filepath.Join("internal", "handler", "brandupload.go")

// bulkImportViolation is FactNoBulkImport's derivation over a set of non-test Go
// sources: "" when no file but the logo handler reads a multipart body, and the logo
// handler reads it only through the multipart reader; otherwise the reason. Package
// level so TestFactMechanisms_SayNoWhenTheFactIsAbsent can drive it with sources it
// writes itself.
func bulkImportViolation(goSrc map[string]string) string {
	for path, src := range goSrc {
		hits := multipartUploadRE.FindAllString(src, -1)
		if len(hits) == 0 {
			continue
		}
		if path != logoUploadFile {
			return path + " reads a file upload. The FAQ says there is no file import; " +
				"if there is one now, rewrite the answer"
		}
		for _, h := range hits {
			if h != ".MultipartReader(" {
				return path + " reads the logo upload through " + strings.TrimSuffix(h, "(") +
					" rather than the multipart stream ADR 0024 §6 requires"
			}
		}
	}
	return ""
}

// goLineCommentRE strips `//` comments, line-locally, before a scan — a scan a
// comment can satisfy pins nothing (the rule marketing_claims_test.go's
// sqlcQueryBody records).
var goLineCommentRE = regexp.MustCompile(`(?m)//.*$`)

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

// --- the integration API (M11) ---------------------------------------------------

// apiFactSources is what the three FactAPI* derivations read, one field per file (or
// per package, for the api surface), so the negative control can take one piece away
// and watch the derivation say no. Go sources are held with `//` comments stripped:
// a scan a comment can satisfy pins nothing.
type apiFactSources struct {
	API      string // every non-test .go file of internal/handler/api, joined
	APIHost  string // internal/httpx/apihost.go: APIPrefix
	Cmd      string // cmd/tappa/main.go + api.go: the wiring
	Delivery string // internal/webhook/delivery/delivery.go: the webhook body
	Auth     string // internal/apikey/auth.go: the closed-API refusal
	Mig38    string // api_enabled_at, and the guard that tappa_app cannot write it
	Mig39    string // op_enable_tenant_api
	Mig49    string // punch_feed(), which queues the webhook delivery
	// Migrations is every migration by file name, to see that nothing after 00049
	// redefines punch_feed(): a later CREATE OR REPLACE would make 00049 stale.
	Migrations map[string]string
}

// loadAPIFactSources reads the product for the three FactAPI* derivations.
func loadAPIFactSources(t *testing.T) apiFactSources {
	t.Helper()
	strip := func(s string) string { return goLineCommentRE.ReplaceAllString(s, "") }
	var api []string
	for path, src := range nonTestGoSource(t, "internal") {
		if filepath.Dir(path) == filepath.Join("internal", "handler", "api") {
			api = append(api, strip(src))
		}
	}
	if len(api) < 5 {
		t.Fatalf("read %d non-test file(s) of internal/handler/api; the surface has more, so "+
			"this is reading the wrong tree", len(api))
	}
	migs := map[string]string{}
	entries, err := os.ReadDir(filepath.Join("..", "..", "db", "migrations"))
	if err != nil {
		t.Fatalf("reading db/migrations: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			migs[e.Name()] = repoFile(t, "db", "migrations", e.Name())
		}
	}
	return apiFactSources{
		API:        strings.Join(api, "\n"),
		APIHost:    strip(repoFile(t, "internal", "httpx", "apihost.go")),
		Cmd:        strip(repoFile(t, "cmd", "tappa", "main.go") + "\n" + repoFile(t, "cmd", "tappa", "api.go")),
		Delivery:   strip(repoFile(t, "internal", "webhook", "delivery", "delivery.go")),
		Auth:       strip(repoFile(t, "internal", "apikey", "auth.go")),
		Mig38:      migs["00038_add_api_access_to_tenants.sql"],
		Mig39:      migs["00039_open_the_api_from_the_operator.sql"],
		Mig49:      migs["00049_write_the_punch_feed.sql"],
		Migrations: migs,
	}
}

// goStringConst reads the value of `name = "…"` (or `name T = "…"`) out of Go
// source, or "" when the constant is not declared with a string literal.
func goStringConst(src, name string) string {
	m := regexp.MustCompile(`(?m)\b` + regexp.QuoteMeta(name) + `(?:\s+\w+)?\s*=\s*"([^"]*)"`).FindStringSubmatch(src)
	if m == nil {
		return ""
	}
	return m[1]
}

// v1MountViolation is "" when /v1 is a surface this binary serves: the api package
// routes Prefix, Prefix is httpx.APIPrefix, APIPrefix is "/v1", and cmd/tappa builds
// the surface and hands it to the router. Otherwise the reason. Every FactAPI* fact,
// and the no-"no API" check, starts here.
func v1MountViolation(s apiFactSources) string {
	if !regexp.MustCompile(`\bconst\s+Prefix\s*=\s*httpx\.APIPrefix\b`).MatchString(s.API) {
		return "internal/handler/api no longer declares Prefix = httpx.APIPrefix"
	}
	if p := goStringConst(s.APIHost, "APIPrefix"); p != "/v1" {
		return "httpx.APIPrefix is " + strconv.Quote(p) + ", not \"/v1\""
	}
	if !strings.Contains(s.API, "r.Route(Prefix,") {
		return "internal/handler/api no longer routes Prefix, so /v1 is mounted by nothing"
	}
	if !strings.Contains(s.Cmd, "api.New(") {
		return "cmd/tappa no longer builds the api surface (api.New)"
	}
	if !regexp.MustCompile(`\bintegrationAPI, err := openAPISurface\(`).MatchString(s.Cmd) ||
		!regexp.MustCompile(`\bHandler:\s+appRouter\([^\n]*\bintegrationAPI\b`).MatchString(s.Cmd) {
		return "cmd/tappa no longer hands the api surface to the router"
	}
	return ""
}

// v1RouteViolation is "" when the api package registers method on the path constant
// pathConst and that constant is want; otherwise the reason.
func v1RouteViolation(s apiFactSources, method, pathConst, want string) string {
	if !regexp.MustCompile(`\br\.` + method + `\(\s*` + regexp.QuoteMeta(pathConst) + `\s*,`).MatchString(s.API) {
		return "internal/handler/api no longer registers " + strings.ToUpper(method) + " " + pathConst
	}
	if got := goStringConst(s.API, pathConst); got != want {
		return "internal/handler/api's " + pathConst + " is " + strconv.Quote(got) + ", not " + strconv.Quote(want)
	}
	return ""
}

// punchFeedBodyRE cuts 00049's punch_feed() out of the migration, up to its closing
// dollar quote.
var punchFeedBodyRE = regexp.MustCompile(`(?s)CREATE FUNCTION public\.punch_feed\(\).*?\n\$\$;`)

// punchFeedRedefinedRE is any definition of the same trigger function.
var punchFeedRedefinedRE = regexp.MustCompile(`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+(?:public\.)?punch_feed\s*\(`)

// holdingFeedBodyRE cuts holding_feed() out of 00049's Up section. A holding punch's
// two events are written by the holding trigger, not punch_feed (K-31), so "every
// punch" reaches a webhook only if THIS function queues one for the employer too.
var holdingFeedBodyRE = regexp.MustCompile(`(?s)CREATE OR REPLACE FUNCTION public\.holding_feed\(\).*?\n\$\$;`)

// holdingFeedRedefinedRE is any definition of holding_feed(); 00046 wrote the first
// one, so only a migration after 00049 makes the reading below stale.
var holdingFeedRedefinedRE = regexp.MustCompile(`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+(?:public\.)?holding_feed\s*\(`)

// employerWebhookRE is the employer's webhook row inside holding_feed(): the punch
// belongs to the employer's employee, and it is the employer's HR system the FAQ
// says can receive it.
var employerWebhookRE = regexp.MustCompile(`INSERT INTO public\.webhook_deliveries\s*\([^)]*\)\s*VALUES\s*\(v_employer\b`)

// gooseUp is a migration's Up section: what the database runs, not what Down restores.
func gooseUp(mig string) string {
	up, _, _ := strings.Cut(mig, "-- +goose Down")
	return up
}

// apiWritesViolation is FactAPIWritesPeopleAndLocations' derivation over s. It and
// its two siblings are package level so the negative control can drive them with a
// source it took one piece out of.
func apiWritesViolation(s apiFactSources) string {
	if why := v1MountViolation(s); why != "" {
		return why
	}
	if why := v1RouteViolation(s, "Put", "locationPath", "/locations/{ref}"); why != "" {
		return why + ", so the API writes no locations"
	}
	if why := v1RouteViolation(s, "Put", "employeePath", "/employees/{ref}"); why != "" {
		return why + ", so the API writes no people"
	}
	return ""
}

// apiFeedViolation is FactAPIFeedsEveryPunch's: the feed route, the trigger that
// queues a webhook for each punch, the sender that is running, and its body.
func apiFeedViolation(s apiFactSources) string {
	if why := v1MountViolation(s); why != "" {
		return why
	}
	if why := v1RouteViolation(s, "Get", "punchesPath", "/punches"); why != "" {
		return why + ", so there is no punch feed to read"
	}
	body := punchFeedBodyRE.FindString(s.Mig49)
	if body == "" {
		return "migration 00049 no longer defines punch_feed()"
	}
	if !strings.Contains(body, "INSERT INTO public.webhook_deliveries") {
		return "00049's punch_feed() no longer queues a webhook delivery for a punch"
	}
	holding := holdingFeedBodyRE.FindString(gooseUp(s.Mig49))
	if holding == "" {
		return "migration 00049's Up no longer defines holding_feed()"
	}
	if !employerWebhookRE.MatchString(holding) {
		return "00049's holding_feed() no longer queues the employer's webhook delivery, so a holding " +
			"punch (K-31) never reaches the employer's HR system and \"every punch\" is false"
	}
	for name, src := range s.Migrations {
		if name != "00049_write_the_punch_feed.sql" && punchFeedRedefinedRE.MatchString(src) {
			return name + " redefines punch_feed(); re-derive the webhook half from that definition"
		}
		if name > "00049_write_the_punch_feed.sql" && holdingFeedRedefinedRE.MatchString(src) {
			return name + " redefines holding_feed(); re-derive the holding webhook half from that definition"
		}
	}
	if !regexp.MustCompile(`\bdeliveries, err := delivery\.New\(`).MatchString(s.Cmd) ||
		!strings.Contains(s.Cmd, "work.Add(deliveries)") {
		return "cmd/tappa no longer runs the webhook sender (delivery.New, then work.Add)"
	}
	if !regexp.MustCompile(`(?s)type Body struct \{[^}]*\bpunchwire\.Punch\b`).MatchString(s.Delivery) {
		return "the webhook body no longer carries punchwire.Punch, so a webhook is not the punch the feed answers"
	}
	return ""
}

// apiGateViolation is FactAPIEnabledPerBusinessByTaptime's: a closed business's key
// is refused 403 api_disabled, the business's own role cannot open it, and the
// operator's function is what does.
func apiGateViolation(s apiFactSources) string {
	if why := v1MountViolation(s); why != "" {
		return why
	}
	if !regexp.MustCompile(`case\s+k\.APIEnabledAt\s*==\s*nil:\s*return\s+Principal\{\},\s*ErrAPIClosed\b`).MatchString(s.Auth) {
		return "apikey's Resolve no longer refuses a key whose business's API is closed (api_enabled_at NULL)"
	}
	if !regexp.MustCompile(`\bErrAPIClosed\s*=\s*&AuthError\{http\.StatusForbidden\b`).MatchString(s.Auth) {
		return "apikey.ErrAPIClosed is no longer a 403"
	}
	if !regexp.MustCompile(`case\s+http\.StatusForbidden:\s*writeProblem\(w,\s*problemAPIDisabled\)`).MatchString(s.API) {
		return "the api chain no longer answers a 403 from the resolver with api_disabled"
	}
	if goStringConst(s.API, "CodeAPIDisabled") != "api_disabled" {
		return "the api package's CodeAPIDisabled is no longer \"api_disabled\""
	}
	if !strings.Contains(s.Mig38, "has_column_privilege('tappa_app', 'public.tenants', 'api_enabled_at', 'UPDATE')") {
		return "migration 00038 no longer refuses a tappa_app that could open its own API, so " +
			"\"once Taptime enables it\" is no longer what holds the gate"
	}
	if !strings.Contains(s.Mig39, "CREATE FUNCTION public.op_enable_tenant_api(") {
		return "migration 00039 no longer defines op_enable_tenant_api, the operator's write"
	}
	return ""
}

// noAPIClaimRE matches a sentence that says the product has no API: "no API", "not /
// isn't an API", "without an API", "do / does / did not (or don't, doesn't, didn't)
// have / offer / provide / expose / ship an API" — straight or curly apostrophe, and
// any one adjective before the noun ("no web API"). What follows the match is checked
// separately (noAPIClaimHit), because RE2 has no lookahead.
var noAPIClaimRE = regexp.MustCompile(`(?i)\b(?:no|not\s+(?:an?|any)|isn['’]t\s+(?:an?|any)|without\s+(?:an?|any)|` +
	`(?:do|does|did)(?:\s+not|n['’]t)\s+(?:have|offer|provide|expose|ship)\s+(?:an?|any))` +
	`\s+(?:[a-z]+\s+)?APIs?\b`)

// noAPINoneRE is the label form: "API: none", "APIs — no".
var noAPINoneRE = regexp.MustCompile(`(?i)\bAPIs?\s*[:—–-]\s*(?:none|no)\b`)

// noAPINounTailRE is what turns "no API" into a phrase about something else: a noun
// ("no API key", "No API code") or a hyphenated compound ("not an API-first product").
var noAPINounTailRE = regexp.MustCompile(`(?i)^(?:-|\s+(?:keys?|hosts?|codes?|calls?|requests?)\b)`)

// noAPIClaimHit returns the first "no API" claim in text, or "".
func noAPIClaimHit(text string) string {
	for _, loc := range noAPIClaimRE.FindAllStringIndex(text, -1) {
		if !noAPINounTailRE.MatchString(text[loc[1]:]) {
			return text[loc[0]:loc[1]]
		}
	}
	return noAPINoneRE.FindString(text)
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
	apiSrc := loadAPIFactSources(t)
	panelSrc := loadPanelFactSources(t)

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
			// "No multipart reader outside the brand logo handler" (M10 WL-7): the one
			// exempt file is named, not matched.
			return bulkImportViolation(goSrc)
		},
		pages.FactAPIWritesPeopleAndLocations:    func() string { return apiWritesViolation(apiSrc) },
		pages.FactAPIFeedsEveryPunch:             func() string { return apiFeedViolation(apiSrc) },
		pages.FactAPIEnabledPerBusinessByTaptime: func() string { return apiGateViolation(apiSrc) },
		// The panel section's three (LP-05; landingpanel_test.go).
		pages.FactPassShowsTheDaysDocketsNewestFirst: func() string { return passRailViolation(panelSrc) },
		pages.FactReviewIsApproveOrReject:            func() string { return reviewOutcomeViolation(panelSrc) },
		pages.FactRollIsTheWeekPerPerson:             both(func() string { return rollWeekViolation(panelSrc) }, rollWeekStartsOnMonday),
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
		// The panel section (LP-05): its heading is a Line too, because it is a claim.
		"LandingPanelHeading":    pages.LandingPanelHeading,
		"LandingPanelLede":       pages.LandingPanelLede,
		"LandingPanelPass.Line":  pages.LandingPanelPass.Line,
		"LandingPanelSpike.Line": pages.LandingPanelSpike.Line,
		"LandingPanelRoll.Line":  pages.LandingPanelRoll.Line,
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
	if len(lines) < 12 {
		t.Fatalf("landingLines names %d line(s); the page renders twelve, so this test is not reading them all", len(lines))
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
	// The shared public chrome (LP-02) renders on this page and is held to the same rule.
	for _, name := range []string{"landing.templ", "publicchrome.templ"} {
		src := repoFile(t, "web", "templates", "pages", name)
		if hit := rawHexRE.FindString(src); hit != "" {
			t.Errorf("%s carries the raw colour %q; the palette is tokens only (skill tappa-brand)", name, hit)
		}
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

// landingFAQAnswerBeforeM11 is "Where does the data go?" as the page answered it until
// 2026-10-21, long after M11 had mounted /v1. The negative control puts it back.
const landingFAQAnswerBeforeM11 = "Into your dashboard: hours per person and per site, and the monthly headcount " +
	"you're billed on — both as CSV exports for your payroll. There is no API. " +
	"Taptime is the source of truth for in/out times — it doesn't try to replace your payroll."

// noAPIClaimViolation is "" unless text — a public page's screen text — says there is
// no API while the product mounts /v1. Package level so the negative control can put
// the old sentence back without touching the shared page data.
func noAPIClaimViolation(url, text string, s apiFactSources) string {
	if v1MountViolation(s) != "" {
		// No /v1, and "there is no API" would be true; the FactAPI* facts go red on
		// their own, which is where the FAQ's API sentences get rewritten.
		return ""
	}
	if hit := noAPIClaimHit(text); hit != "" {
		return url + " says " + strconv.Quote(hit) + " while internal/handler/api mounts /v1. The " +
			"integration API exists (per business, once Taptime enables it); say that, or nothing"
	}
	return ""
}

// TestLanding_ClaimsNoAbsentAPIWhileV1IsMounted reads every public page for a "no
// API" sentence. It exists because the derivation that used to guard that sentence
// scanned "/api…" route literals, and the API arrived at /v1: the FAQ said "There is
// no API." on a product that served one, and every test was green. This one reads
// what the visitor reads, so a new route spelling cannot walk round it.
func TestLanding_ClaimsNoAbsentAPIWhileV1IsMounted(t *testing.T) {
	t.Parallel()
	api := loadAPIFactSources(t)
	if why := v1MountViolation(api); why != "" {
		t.Fatalf("the product no longer mounts /v1 (%s); this check would pass vacuously. If the API "+
			"is really gone, the FactAPI* facts are red too — rewrite the FAQ and this test together", why)
	}
	r := marketingRouter(t)
	for _, url := range marketingURLs() {
		if why := noAPIClaimViolation(url, screenText(t, mustFetchMarketing(t, r, url)), api); why != "" {
			t.Error(why)
		}
	}
}

// --- negative controls -----------------------------------------------------------

// TestFactMechanisms_SayNoWhenTheFactIsAbsent is the negative control for every
// scanner above: a scanner that matched nothing would leave every test green over
// a product that had lost the capability, and the failure would look like success.
func TestFactMechanisms_SayNoWhenTheFactIsAbsent(t *testing.T) {
	t.Parallel()

	// The integration API's three facts: each holds over the real sources and says no
	// when one piece of what it reads is taken away.
	api := loadAPIFactSources(t)
	type field func(*apiFactSources) *string
	var (
		apiPkg   field = func(s *apiFactSources) *string { return &s.API }
		apiHost  field = func(s *apiFactSources) *string { return &s.APIHost }
		cmd      field = func(s *apiFactSources) *string { return &s.Cmd }
		delivery field = func(s *apiFactSources) *string { return &s.Delivery }
		auth     field = func(s *apiFactSources) *string { return &s.Auth }
		mig38    field = func(s *apiFactSources) *string { return &s.Mig38 }
		mig39    field = func(s *apiFactSources) *string { return &s.Mig39 }
		mig49    field = func(s *apiFactSources) *string { return &s.Mig49 }
	)
	for _, tc := range []struct {
		name     string
		derive   func(apiFactSources) string
		in       field
		old, new string
	}{
		{"/v1 is not routed", apiWritesViolation, apiPkg, "r.Route(Prefix,", "r.Group("},
		{"APIPrefix is not /v1", apiFeedViolation, apiHost, `APIPrefix = "/v1"`, `APIPrefix = "/v2"`},
		{"cmd does not hand the surface to the router", apiGateViolation, cmd, "integrationAPI, devTools)", "devTools)"},
		{"no PUT locations", apiWritesViolation, apiPkg, "r.Put(locationPath,", "r.Get(locationPath,"},
		{"no PUT employees", apiWritesViolation, apiPkg, "r.Put(employeePath,", "r.Get(employeePath,"},
		{"locations moved", apiWritesViolation, apiPkg, `locationPath = "/locations/{ref}"`, `locationPath = "/sites/{ref}"`},
		{"no GET punches", apiFeedViolation, apiPkg, "r.Get(punchesPath,", "r.Head(punchesPath,"},
		{"the trigger queues no webhook", apiFeedViolation, mig49, "INSERT INTO public.webhook_deliveries", "INSERT INTO public.nothing"},
		{"a holding punch queues no employer webhook", apiFeedViolation, mig49,
			"INSERT INTO public.webhook_deliveries (tenant_id, endpoint_id, punch_event_seq) VALUES (v_employer",
			"INSERT INTO public.nothing (tenant_id, endpoint_id, punch_event_seq) VALUES (v_employer"},
		{"no webhook sender", apiFeedViolation, cmd, "work.Add(deliveries)", "_ = deliveries"},
		{"the webhook body is not the punch", apiFeedViolation, delivery, "\tpunchwire.Punch\n", "\tX int\n"},
		{"a closed API is not refused", apiGateViolation, auth, "case k.APIEnabledAt == nil:", "case false:"},
		{"the refusal is not a 403", apiGateViolation, auth, `&AuthError{http.StatusForbidden, "the integration`, `&AuthError{http.StatusUnauthorized, "the integration`},
		{"a 403 is not api_disabled", apiGateViolation, apiPkg, "writeProblem(w, problemAPIDisabled)", "writeProblem(w, problemUnauthorized)"},
		{"tappa_app could open its own API", apiGateViolation, mig38, "'api_enabled_at', 'UPDATE'", "'api_code', 'SELECT'"},
		{"no operator write", apiGateViolation, mig39, "CREATE FUNCTION public.op_enable_tenant_api(", "CREATE FUNCTION public.op_other("},
	} {
		if why := tc.derive(api); why != "" {
			t.Errorf("API control %q: the derivation does not hold over the real sources: %s", tc.name, why)
			continue
		}
		m := api
		p := tc.in(&m)
		if !strings.Contains(*p, tc.old) {
			t.Errorf("API control %q: %q is gone from the source, so the mutation would change nothing", tc.name, tc.old)
			continue
		}
		*p = strings.Replace(*p, tc.old, tc.new, 1)
		if tc.derive(m) == "" {
			t.Errorf("API control %q: the derivation still holds over a source without it", tc.name)
		}
	}
	redefined := api
	redefined.Migrations = map[string]string{"00099_x.sql": "CREATE OR REPLACE FUNCTION public.punch_feed() RETURNS trigger"}
	if apiFeedViolation(redefined) == "" {
		t.Error("apiFeedViolation does not notice a later migration redefining punch_feed()")
	}
	redefined.Migrations = map[string]string{"00099_x.sql": "CREATE OR REPLACE FUNCTION public.holding_feed() RETURNS trigger"}
	if apiFeedViolation(redefined) == "" {
		t.Error("apiFeedViolation does not notice a later migration redefining holding_feed()")
	}
	redefined.Migrations = map[string]string{"00046_x.sql": "CREATE OR REPLACE FUNCTION public.holding_feed() RETURNS trigger"}
	if why := apiFeedViolation(redefined); why != "" {
		t.Errorf("apiFeedViolation refuses holding_feed()'s definition BEFORE 00049, which 00049 replaces: %s", why)
	}

	// The panel section's three facts (LP-05): each holds over the real sources and says no
	// when one piece of what it reads is taken away. A mutation is applied inside the one
	// component or query it is about, so it cannot land on a twin elsewhere in the file.
	panel := loadPanelFactSources(t)
	within := func(src, whole, old, new string) string {
		return strings.Replace(src, whole, strings.ReplaceAll(whole, old, new), 1)
	}
	for _, tc := range []struct {
		name   string
		derive func(panelFactSources) string
		mutate func(*panelFactSources)
	}{
		{"the panel's records are oldest first", passRailViolation, func(s *panelFactSources) {
			s.Queries = within(s.Queries, queryBlock(s.Queries, "ListPanelTransactions"), "ORDER BY t.occurred_at DESC", "ORDER BY t.occurred_at ASC")
		}},
		{"the panel's query is not read", passRailViolation, func(s *panelFactSources) {
			s.Ledger = strings.ReplaceAll(s.Ledger, ".ListPanelTransactions(", ".ListOtherTransactions(")
		}},
		{"The Pass hangs no rail", passRailViolation, func(s *panelFactSources) {
			s.Transactions = within(s.Transactions, templBody(s.Transactions, "passBody"), "@rail(v)", "")
		}},
		{"the rail hangs another docket", passRailViolation, func(s *panelFactSources) {
			s.Transactions = within(s.Transactions, templBody(s.Transactions, "rail"), "@components.RailDocket(d)", "@components.TransactionDocket(d)")
		}},
		{"the rail docket prints no stamp", passRailViolation, func(s *panelFactSources) {
			s.Docket = within(s.Docket, templBody(s.Docket, "RailDocket"), "@VerdictStamp(d.Verdict)", "")
		}},
		{"the stamp says another word", passRailViolation, func(s *panelFactSources) {
			s.Docket = within(s.Docket, templBody(s.Docket, "VerdictStamp"), `stamp--flagged">FLAGGED<`, `stamp--flagged">HELD<`)
		}},
		{"a third outcome", reviewOutcomeViolation, func(s *panelFactSources) {
			s.Mig05 = strings.Replace(s.Mig05, "IN ('approved', 'rejected')", "IN ('approved', 'rejected', 'deferred')", 1)
		}},
		{"the form cannot reject", reviewOutcomeViolation, func(s *panelFactSources) {
			s.Review = strings.ReplaceAll(s.Review, `name="outcome" value="rejected"`, `name="outcome" value="deferred"`)
		}},
		{"a report is a fortnight", rollWeekViolation, func(s *panelFactSources) {
			s.Report = strings.Replace(s.Report, "const ReportDays = 7", "const ReportDays = 14", 1)
		}},
		{"The Roll is per venue", rollWeekViolation, func(s *panelFactSources) {
			s.Reports = within(s.Reports, templBody(s.Reports, "reportPeople"), "range v.People", "range v.Venues")
		}},
		{"The Roll has no day cells", rollWeekViolation, func(s *panelFactSources) {
			s.Reports = within(s.Reports, templBody(s.Reports, "reportPeople"), "range v.DayHeadings", "range v.Totals")
		}},
	} {
		if why := tc.derive(panel); why != "" {
			t.Errorf("panel control %q: the derivation does not hold over the real sources: %s", tc.name, why)
			continue
		}
		m := panel
		tc.mutate(&m)
		if m == panel {
			t.Errorf("panel control %q: the mutation changed nothing in the sources", tc.name)
			continue
		}
		if tc.derive(m) == "" {
			t.Errorf("panel control %q: the derivation still holds over a source without it", tc.name)
		}
	}

	// The no-"no API" scanner sees the sentence the FAQ carried until 2026-10-21, and
	// the other ways of saying it, and not a noun phrase about something else.
	for _, s := range []string{
		landingFAQAnswerBeforeM11, "We have no integration API.", "Taptime is not an API product",
		"works without an API", "No APIs, no fuss.",
		"There isn't an API.", "There isn’t an API.", "We don't have an API.", "We don’t have an API.",
		"Taptime does not offer an API.", "doesn't provide an API", "it doesn’t expose any API",
		"we did not ship an API", "API: none.", "no web API",
	} {
		if noAPIClaimHit(s) == "" {
			t.Errorf("the no-API scanner would not see %q", s)
		}
	}
	for _, s := range []string{
		"the integration API can write your people and locations once Taptime enables it for you",
		"No API code", "a business with no API key yet", "no API host is configured", "a rapid answer",
		"Taptime is not an API-first product", "we don't have an API key for you yet",
	} {
		if hit := noAPIClaimHit(s); hit != "" {
			t.Errorf("the no-API scanner matches %q in %q", hit, s)
		}
	}
	// AND THE PAGE CHECK ITSELF GOES RED with the old sentence put back.
	if why := noAPIClaimViolation("/", landingFAQAnswerBeforeM11, api); why == "" {
		t.Error("noAPIClaimViolation accepts \"There is no API.\" on the landing page while /v1 is mounted")
	}
	// And with today's API sentence swapped for another way of denying it.
	const apiSentence = "Once Taptime enables the integration API for you, your HR or payroll system " +
		"can also read every punch from a feed or receive it by webhook."
	swapped := false
	for _, q := range pages.LandingFAQ {
		if strings.Contains(q.A, apiSentence) {
			swapped = true
			if noAPIClaimViolation("/", strings.Replace(q.A, apiSentence, "We don't have an API.", 1), api) == "" {
				t.Error("noAPIClaimViolation accepts \"We don't have an API.\" in place of the FAQ's API sentence")
			}
		}
	}
	if !swapped {
		t.Errorf("no FAQ answer carries %q any more; the end-to-end mutation would change nothing", apiSentence)
	}
	for _, q := range pages.LandingFAQ {
		if why := noAPIClaimViolation("/", q.A, api); why != "" {
			t.Errorf("noAPIClaimViolation refuses today's answer to %q: %s", q.Q, why)
		}
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
	// M10 WL-7: the one exemption is the logo handler's file, by its path, and only for
	// the multipart stream. A reader anywhere else -- the employees' actions, the case
	// the WL-7 card names -- or a form helper in the logo handler itself is a violation.
	employees := filepath.Join("internal", "handler", "employeeactions.go")
	for _, tc := range []struct {
		name string
		src  map[string]string
		ok   bool
	}{
		{"the logo handler's stream alone", map[string]string{logoUploadFile: `mr, err := r.MultipartReader()`, employees: `x := 1`}, true},
		{"FormFile in the employees' actions", map[string]string{logoUploadFile: `mr, err := r.MultipartReader()`, employees: `f, _, err := r.FormFile("staff")`}, false},
		{"a stream in the employees' actions", map[string]string{employees: `mr, err := r.MultipartReader()`}, false},
		{"FormFile in the logo handler", map[string]string{logoUploadFile: `f, _, err := r.FormFile("logo")`}, false},
		{"ParseMultipartForm in the logo handler", map[string]string{logoUploadFile: `mr, _ := r.MultipartReader(); r.ParseMultipartForm(64 << 10)`}, false},
		{"the same file name in another directory", map[string]string{filepath.Join("internal", "brandupload.go"): `mr, err := r.MultipartReader()`}, false},
	} {
		if got := bulkImportViolation(tc.src) == ""; got != tc.ok {
			t.Errorf("bulkImportViolation, %s: holds %v, want %v", tc.name, got, tc.ok)
		}
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
