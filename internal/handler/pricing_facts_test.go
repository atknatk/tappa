package handler

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/atknatk/tappa/web/templates/pages"
)

// THE PRICING PAGE'S PINS (LP-06). The page's own facts -- how a month is counted,
// what it costs, what freezing it does and what the Billing screen shows -- are
// declared in web/templates/pages/pricingview.go and derived here from the code that
// does each thing: migration 00016's functions, the billing queries, the billing
// domain, the panel's billing handlers and its template. Like every derivation in
// this package, NONE OF THEM READS THE PAGE'S SENTENCE; each reads the product, and
// TestPricingFacts_SayNoWhenTheFactIsAbsent takes one piece of what it reads away and
// expects a no.
//
// The page's copy joins the shared closure through publicPagesCopy
// (publicpages_test.go) and its derivations through pageFactDerivations
// (marketing_facts_test.go).

// billingFactSources is everything the pricing page's derivations read, so the negative
// control can hand them a copy with one piece taken out.
type billingFactSources struct {
	Mig16    string // db/migrations/00016_add_billing_price_and_periods.sql
	Queries  string // db/queries/billing.sql
	Book     string // internal/domain/billing/billing.go
	Handler  string // internal/handler/billing.go
	Actions  string // internal/handler/billingactions.go
	CSV      string // internal/handler/billingcsv.go
	Routes   string // internal/handler/dashboard.go
	Template string // web/templates/pages/billing.templ
	// Later is every migration after 00016, by file name: one of them could redefine a
	// function 00016 declares, or give back the writes it takes away.
	Later map[string]string
}

func loadBillingFactSources(t *testing.T) billingFactSources {
	t.Helper()
	const mig16 = "00016_add_billing_price_and_periods.sql"
	dir := filepath.Join("..", "..", "db", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	later := map[string]string{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") || e.Name() <= mig16 {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		later[e.Name()] = string(b)
	}
	if len(later) < 40 {
		t.Fatalf("PREMISE: read %d migration(s) after %s; the directory holds more", len(later), mig16)
	}
	return billingFactSources{
		Mig16:    repoFile(t, "db", "migrations", mig16),
		Queries:  repoFile(t, "db", "queries", "billing.sql"),
		Book:     repoFile(t, "internal", "domain", "billing", "billing.go"),
		Handler:  repoFile(t, "internal", "handler", "billing.go"),
		Actions:  repoFile(t, "internal", "handler", "billingactions.go"),
		CSV:      repoFile(t, "internal", "handler", "billingcsv.go"),
		Routes:   repoFile(t, "internal", "handler", "dashboard.go"),
		Template: repoFile(t, "web", "templates", "pages", "billing.templ"),
		Later:    later,
	}
}

// sqlFunctionBody is one SQL function's text in src, from its CREATE FUNCTION line to the
// end of its body ($$;), or "" when it is not declared there.
func sqlFunctionBody(src, name string) string {
	at := strings.Index(src, "CREATE FUNCTION "+name+"(")
	if at < 0 {
		return ""
	}
	rest := src[at:]
	if end := strings.Index(rest, "$$;"); end >= 0 {
		return rest[:end+3]
	}
	return ""
}

// goFuncBody is one Go function's text in src, from decl (its signature's start) to the
// next top-level func, or "" when there is no such function.
func goFuncBody(src, decl string) string {
	at := strings.Index(src, decl)
	if at < 0 {
		return ""
	}
	rest := src[at:]
	if end := strings.Index(rest[1:], "\nfunc "); end >= 0 {
		return rest[:end+1]
	}
	return rest
}

// redefinedLater names a later migration that redefines (or drops) the SQL function
// name, or "".
func redefinedLater(later map[string]string, name string) string {
	re := regexp.MustCompile(`(?i)(?:CREATE\s+OR\s+REPLACE\s+FUNCTION|DROP\s+FUNCTION(?:\s+IF\s+EXISTS)?)\s+(?:public\.)?` +
		regexp.QuoteMeta(name) + `\b`)
	for file, src := range later {
		if re.MatchString(gooseUp(src)) {
			return file
		}
	}
	return ""
}

// The shapes the derivations read.
var (
	billableCallRE   = regexp.MustCompile(`tappa_employee_is_billable\(\s*e\.status,\s*e\.activated_at,\s*e\.deactivated_at,\s*b\.from_at,\s*b\.to_at\)`)
	monthFromRE      = regexp.MustCompile(`tappa_local_month_start\(s\.month,\s*s\.timezone\)\s+AS\s+from_at`)
	monthToRE        = regexp.MustCompile(`tappa_local_month_start\(\(s\.month \+ interval '1 month'\)::date,\s*s\.timezone\)\s+AS\s+to_at`)
	amountColumnRE   = regexp.MustCompile(`GENERATED ALWAYS AS\s*\(\s*tappa_billing_amount_due\(\s*free_period,\s*unit_price,\s*employee_count\s*\)\s*\)`)
	writeBackRE      = regexp.MustCompile(`(?is)GRANT\s[^;]*\b(?:UPDATE|DELETE)\b[^;]*\bON\s+(?:TABLE\s+)?(?:public\.)?billing_periods\b`)
	readsEmployeesRE = regexp.MustCompile(`(?i)(?:\bFROM|\bJOIN|,)\s*(?:public\.)?employees\b|tappa_employee_is_billable`)
	triggerDropRE    = regexp.MustCompile(`(?i)DROP\s+TRIGGER\s+(?:IF\s+EXISTS\s+)?billing_periods_no_mutation\b`)
)

// billableViolation is FactBillingCountsWhoWasActive's derivation: a person is counted
// for a month when they finished activation and their employment overlaps the month's
// own bounds, in both billing statements.
func billableViolation(s billingFactSources) string {
	fn := sqlFunctionBody(s.Mig16, "tappa_employee_is_billable")
	if fn == "" {
		return "migration 00016 no longer declares tappa_employee_is_billable"
	}
	for _, want := range []string{
		"p_activated_at IS NOT NULL",
		"p_activated_at < p_to",
		"(p_deactivated_at IS NULL OR p_deactivated_at > p_from)",
	} {
		if !strings.Contains(fn, want) {
			return "tappa_employee_is_billable no longer carries " + want + ", so \"active at any point in " +
				"the month\" and \"somebody who never finished activation is not counted\" are not its rule"
		}
	}
	if file := redefinedLater(s.Later, "tappa_employee_is_billable"); file != "" {
		return file + " redefines tappa_employee_is_billable after 00016; re-read the rule there"
	}
	for _, q := range []string{"PreviewBillingPeriod", "CloseBillingPeriod"} {
		block := queryBlock(s.Queries, q)
		if block == "" {
			return "db/queries/billing.sql no longer has " + q
		}
		if !billableCallRE.MatchString(block) {
			return q + " no longer counts people with tappa_employee_is_billable over each person's own " +
				"activated_at and deactivated_at and the month's bounds"
		}
		if !monthFromRE.MatchString(block) || !monthToRE.MatchString(block) {
			return q + " no longer bounds the month by its own local start and the next month's, so a " +
				"finished month is not counted over its own dates"
		}
	}
	return ""
}

// amountDueViolation is FactBillingAmountDue's derivation: the amount is the count times
// the price, zero in a free month, from the one function the live figure and the frozen
// column share.
func amountDueViolation(s billingFactSources) string {
	fn := sqlFunctionBody(s.Mig16, "tappa_billing_amount_due")
	if fn == "" {
		return "migration 00016 no longer declares tappa_billing_amount_due"
	}
	if !strings.Contains(fn, "WHEN p_free_period THEN 0") {
		return "tappa_billing_amount_due no longer answers 0 in a free month"
	}
	if !strings.Contains(fn, "ELSE p_unit_price * p_employee_count") {
		return "tappa_billing_amount_due no longer multiplies the price by the people counted"
	}
	if file := redefinedLater(s.Later, "tappa_billing_amount_due"); file != "" {
		return file + " redefines tappa_billing_amount_due after 00016; re-read the rule there"
	}
	if !amountColumnRE.MatchString(s.Mig16) {
		return "billing_periods.amount_due is no longer generated by tappa_billing_amount_due, so a frozen " +
			"total could follow another rule"
	}
	if !strings.Contains(queryBlock(s.Queries, "PreviewBillingPeriod"), "tappa_billing_amount_due(") {
		return "PreviewBillingPeriod no longer computes the live amount with tappa_billing_amount_due"
	}
	return ""
}

// freezeFinalViolation is FactBillingFreezeIsFinal's derivation: a month's frozen row is
// the answer once it exists (Book.Period reads it before it previews anything), the row
// is read without recomputing, and billing_periods takes no UPDATE and no DELETE.
func freezeFinalViolation(s billingFactSources) string {
	period := goFuncBody(s.Book, "func (b *Book) Period(")
	get, prev := strings.Index(period, "q.GetBillingPeriod("), strings.Index(period, "preview(ctx, q,")
	if get < 0 || prev < 0 || prev < get {
		return "billing.Book.Period no longer reads the frozen row first and previews only when there is " +
			"none, so a frozen month is not the answer from then on"
	}
	if readsEmployeesRE.MatchString(sqlLineCommentRE.ReplaceAllString(queryBlock(s.Queries, "GetBillingPeriod"), "")) {
		return "GetBillingPeriod reads employees, so a frozen month is recomputed when it is read"
	}
	if !strings.Contains(s.Mig16, "REVOKE UPDATE, DELETE ON billing_periods FROM tappa_app;") {
		return "migration 00016 no longer revokes UPDATE and DELETE on billing_periods from the application"
	}
	if !regexp.MustCompile(`BEFORE UPDATE OR DELETE ON billing_periods\s+FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation\(\)`).MatchString(s.Mig16) {
		return "migration 00016's trigger no longer refuses UPDATE and DELETE on billing_periods"
	}
	for file, src := range s.Later {
		up := gooseUp(src)
		if writeBackRE.MatchString(up) || triggerDropRE.MatchString(up) {
			return file + " gives billing_periods back a write or drops its trigger, so a frozen month can change"
		}
	}
	return ""
}

// ownerOnceOverViolation is FactBillingFreezeIsOwnersOnceTheMonthIsOver's derivation.
func ownerOnceOverViolation(s billingFactSources) string {
	if !strings.Contains(goFuncBody(s.Actions, "func mayManageBilling("), "id.Admin.Role == adminRoleOwner") {
		return "mayManageBilling no longer asks for the owner role"
	}
	for _, h := range []string{"billingCloseReview", "billingCloseApply"} {
		if !strings.Contains(goFuncBody(s.Actions, "func (a *AdminAuth) "+h+"("), "if !mayManageBilling(id) {") {
			return h + " no longer refuses a non-owner"
		}
	}
	for _, reg := range []string{"r.Post(billingCloseHref, a.billingCloseReview)", "r.Post(billingFreezeHref, a.billingCloseApply)"} {
		if !strings.Contains(s.Routes, reg) {
			return "dashboard.go no longer registers " + reg + ", so freezing goes by another route"
		}
	}
	if !strings.Contains(queryBlock(s.Queries, "CloseBillingPeriod"), "AND b.to_at <= now()") {
		return "CloseBillingPeriod no longer refuses a month that has not ended"
	}
	return ""
}

// csvAnyMonthViolation is FactBillingCSVForAnyMonth's derivation: the export answers the
// month through billingMonth, and billingMonth through Book.Period -- the frozen row
// when there is one, the live figure otherwise.
func csvAnyMonthViolation(s billingFactSources) string {
	if !strings.Contains(s.Routes, "r.Get(billingCSVHref, a.billingExport)") {
		return "dashboard.go no longer registers GET billingCSVHref to billingExport"
	}
	if !strings.Contains(goFuncBody(s.CSV, "func (a *AdminAuth) billingExport("), "a.billingMonth(") {
		return "billingExport no longer reads the month through billingMonth"
	}
	if !strings.Contains(goFuncBody(s.Handler, "func (a *AdminAuth) billingMonth("), "a.books.Period(") {
		return "billingMonth no longer reads Book.Period"
	}
	return freezeFinalViolation(s)
}

// firstChargedViolation is FactBillingNamesTheFirstChargedMonth's derivation.
func firstChargedViolation(s billingFactSources) string {
	plan := templBody(s.Template, "billingPlan")
	if !strings.Contains(plan, "First month that is charged for") || !strings.Contains(plan, "{ v.FirstCharged }") {
		return "the Billing screen's plan card no longer prints the first month that is charged for"
	}
	if !strings.Contains(goFuncBody(s.Handler, "func fillBillingPlan("), "v.FirstCharged = monthLabel(d.FirstChargeableMonth)") {
		return "fillBillingPlan no longer fills FirstCharged from the database's first chargeable month"
	}
	if !strings.Contains(queryBlock(s.Queries, "PreviewBillingPeriod"), "tappa_first_chargeable_month(") {
		return "PreviewBillingPeriod no longer reads tappa_first_chargeable_month"
	}
	return ""
}

// receiptViolation is FactBillingShowsFrozenMonthsAsReceipts' derivation: the history
// hangs one receipt per frozen month, stamped FROZEN in the product's approved frame,
// with its people, price and total and a CSV link.
func receiptViolation(s billingFactSources) string {
	if !strings.Contains(templBody(s.Template, "billingHistory"), "@billingReceipt(h)") {
		return "the Billing screen's frozen months are no longer drawn as receipts"
	}
	receipt := templBody(s.Template, "billingReceipt")
	for _, want := range []string{
		`<span class="stamp stamp--approved flex-none">FROZEN</span>`,
		"{ h.Employees }", "{ h.UnitPrice }", "TOTAL", "{ h.Amount }", "Download CSV",
	} {
		if !strings.Contains(receipt, want) {
			return "the Billing screen's receipt no longer carries " + want
		}
	}
	return ""
}

// pricingFactDerivations derives every fact pricingview.go declares.
func pricingFactDerivations(t *testing.T) map[pages.Fact]func() string {
	t.Helper()
	s := loadBillingFactSources(t)
	return map[pages.Fact]func() string{
		pages.FactBillingCountsWhoWasActive:               func() string { return billableViolation(s) },
		pages.FactBillingAmountDue:                        func() string { return amountDueViolation(s) },
		pages.FactBillingFreezeIsFinal:                    func() string { return freezeFinalViolation(s) },
		pages.FactBillingFreezeIsOwnersOnceTheMonthIsOver: func() string { return ownerOnceOverViolation(s) },
		pages.FactBillingCSVForAnyMonth:                   func() string { return csvAnyMonthViolation(s) },
		pages.FactBillingNamesTheFirstChargedMonth:        func() string { return firstChargedViolation(s) },
		pages.FactBillingShowsFrozenMonthsAsReceipts:      func() string { return receiptViolation(s) },
	}
}

// TestPricingFacts_SayNoWhenTheFactIsAbsent is the negative control for every
// derivation above: each holds over the real sources and says no when one piece of what
// it reads is taken away. A mutation is applied inside the one function, query or
// component it is about.
func TestPricingFacts_SayNoWhenTheFactIsAbsent(t *testing.T) {
	t.Parallel()
	real := loadBillingFactSources(t)
	within := func(src, whole, old, new string) string {
		return strings.Replace(src, whole, strings.ReplaceAll(whole, old, new), 1)
	}
	later := func(sql string) func(*billingFactSources) {
		return func(s *billingFactSources) {
			l := map[string]string{}
			for k, v := range s.Later {
				l[k] = v
			}
			l["00099_x.sql"] = "-- +goose Up\n" + sql + "\n-- +goose Down\nSELECT 1;\n"
			s.Later = l
		}
	}
	for _, tc := range []struct {
		name   string
		derive func(billingFactSources) string
		mutate func(*billingFactSources)
	}{
		{"an invitation is counted", billableViolation, func(s *billingFactSources) {
			s.Mig16 = within(s.Mig16, sqlFunctionBody(s.Mig16, "tappa_employee_is_billable"), "AND p_activated_at IS NOT NULL", "")
		}},
		{"a person who left before the month is counted", billableViolation, func(s *billingFactSources) {
			s.Mig16 = within(s.Mig16, sqlFunctionBody(s.Mig16, "tappa_employee_is_billable"), "p_deactivated_at > p_from", "p_deactivated_at > p_from - interval '1 year'")
		}},
		{"the preview counts another way", billableViolation, func(s *billingFactSources) {
			s.Queries = within(s.Queries, queryBlock(s.Queries, "PreviewBillingPeriod"), "tappa_employee_is_billable(", "tappa_employee_is_listed(")
		}},
		{"the close is counted over today", billableViolation, func(s *billingFactSources) {
			s.Queries = within(s.Queries, queryBlock(s.Queries, "CloseBillingPeriod"), "b.from_at, b.to_at)", "b.from_at, now())")
		}},
		{"a later migration redefines the rule", billableViolation, later("CREATE OR REPLACE FUNCTION public.tappa_employee_is_billable(text) RETURNS boolean")},
		{"a free month is charged", amountDueViolation, func(s *billingFactSources) {
			s.Mig16 = within(s.Mig16, sqlFunctionBody(s.Mig16, "tappa_billing_amount_due"), "WHEN p_free_period THEN 0", "WHEN false THEN 0")
		}},
		{"the amount is not count times price", amountDueViolation, func(s *billingFactSources) {
			s.Mig16 = within(s.Mig16, sqlFunctionBody(s.Mig16, "tappa_billing_amount_due"), "p_unit_price * p_employee_count", "p_unit_price")
		}},
		{"the frozen column follows another rule", amountDueViolation, func(s *billingFactSources) {
			s.Mig16 = strings.Replace(s.Mig16, "tappa_billing_amount_due(free_period, unit_price, employee_count)",
				"other_amount(free_period, unit_price, employee_count)", 1)
		}},
		{"Period previews before it reads the frozen row", freezeFinalViolation, func(s *billingFactSources) {
			s.Book = within(s.Book, goFuncBody(s.Book, "func (b *Book) Period("), "q.GetBillingPeriod(", "q.GetOtherPeriod(")
		}},
		{"the frozen read recomputes", freezeFinalViolation, func(s *billingFactSources) {
			s.Queries = within(s.Queries, queryBlock(s.Queries, "GetBillingPeriod"), "FROM billing_periods b", "FROM billing_periods b, employees e")
		}},
		{"the application may update a frozen month", freezeFinalViolation, func(s *billingFactSources) {
			s.Mig16 = strings.Replace(s.Mig16, "REVOKE UPDATE, DELETE ON billing_periods FROM tappa_app;", "", 1)
		}},
		{"a later migration gives the update back", freezeFinalViolation, later("GRANT SELECT, UPDATE ON billing_periods TO tappa_app;")},
		{"a later migration drops the trigger", freezeFinalViolation, later("DROP TRIGGER IF EXISTS billing_periods_no_mutation ON billing_periods;")},
		{"a manager may freeze", ownerOnceOverViolation, func(s *billingFactSources) {
			s.Actions = within(s.Actions, goFuncBody(s.Actions, "func mayManageBilling("), "id.Admin.Role == adminRoleOwner", "id.Live()")
		}},
		{"the write step skips the gate", ownerOnceOverViolation, func(s *billingFactSources) {
			s.Actions = within(s.Actions, goFuncBody(s.Actions, "func (a *AdminAuth) billingCloseApply("), "if !mayManageBilling(id) {", "if false {")
		}},
		{"a running month can be frozen", ownerOnceOverViolation, func(s *billingFactSources) {
			s.Queries = within(s.Queries, queryBlock(s.Queries, "CloseBillingPeriod"), "AND b.to_at <= now()", "")
		}},
		{"the export reads another month", csvAnyMonthViolation, func(s *billingFactSources) {
			s.CSV = within(s.CSV, goFuncBody(s.CSV, "func (a *AdminAuth) billingExport("), "a.billingMonth(", "a.frozenMonth(")
		}},
		{"billingMonth reads another source", csvAnyMonthViolation, func(s *billingFactSources) {
			s.Handler = within(s.Handler, goFuncBody(s.Handler, "func (a *AdminAuth) billingMonth("), "a.books.Period(", "a.books.Preview(")
		}},
		{"the plan card names no first month", firstChargedViolation, func(s *billingFactSources) {
			s.Template = within(s.Template, templBody(s.Template, "billingPlan"), "{ v.FirstCharged }", "{ v.PlanLabel }")
		}},
		{"the first month is not the database's", firstChargedViolation, func(s *billingFactSources) {
			s.Handler = within(s.Handler, goFuncBody(s.Handler, "func fillBillingPlan("), "monthLabel(d.FirstChargeableMonth)", "monthLabel(d.Month)")
		}},
		{"the receipt carries another stamp", receiptViolation, func(s *billingFactSources) {
			s.Template = within(s.Template, templBody(s.Template, "billingReceipt"), ">FROZEN<", ">CLOSED<")
		}},
		{"the history draws no receipt", receiptViolation, func(s *billingFactSources) {
			s.Template = within(s.Template, templBody(s.Template, "billingHistory"), "@billingReceipt(h)", "")
		}},
	} {
		if why := tc.derive(real); why != "" {
			t.Errorf("control %q: the derivation does not hold over the real sources: %s", tc.name, why)
			continue
		}
		m := real
		tc.mutate(&m)
		if reflect.DeepEqual(m, real) {
			t.Errorf("control %q: the mutation changed nothing in the sources", tc.name)
			continue
		}
		if tc.derive(m) == "" {
			t.Errorf("control %q: the derivation still holds over a source without it", tc.name)
		}
	}

	// The handoff reader for "pricing locked for life" is scoped to §11 and to the
	// founding offer's line.
	for doc, ok := range map[string]bool{
		"## 11. Fiyatlandırma & GTM\n\n- **€1.50**\n- **Founding offer:** ilk 3 ay ücretsiz + ömür boyu fiyat sabit\n": true,
		"## 4. Marka\n- Founding offer: ömür boyu fiyat sabit\n## 11. Fiyat\n- x\n":                                    false,
		"## 11. Fiyat\n- x\n## 12. Sonra\n- Founding offer: ömür boyu fiyat sabit\n":                                   false,
		"## 11. Fiyat\n- **Founding offer:** ilk 3 ay ücretsiz\n- fiyat değişebilir\n":                                 false,
	} {
		if priceLockedForLifeRE.MatchString(doc) != ok {
			t.Errorf("priceLockedForLifeRE on %q = %v, want %v", doc, !ok, ok)
		}
	}
}

// pricingLines is every stand-alone sentence the pricing page renders from a pages.Line:
// the landing's price card (heading, lede, the founding chip's term and the five
// included lines, as this page draws the same card) and the page's own three.
func pricingLines() map[string]pages.Line {
	out := map[string]pages.Line{
		"LandingPricingHeading":     pages.LandingPricingHeading,
		"LandingPricingLede":        pages.LandingPricingLede,
		"LandingPriceLockedForLife": pages.LandingPriceLockedForLife,
		"PricingExamplesLede":       pages.PricingExamplesLede,
		"PricingFreeNote":           pages.PricingFreeNote,
		"PricingReceiptCaption":     pages.PricingReceiptCaption,
	}
	for i, l := range pages.LandingPricingIncludes {
		out["LandingPricingIncludes["+strconv.Itoa(i)+"]"] = l
	}
	return out
}

// TestPricingFAQ_SaysWhatTheBillingCodeDoes holds the billing questions to the wording
// README §10 asked for: the board's two sentences the product does not keep (U4, U6) are
// not on the page, the six questions are there and the first is open.
func TestPricingFAQ_SaysWhatTheBillingCodeDoes(t *testing.T) {
	t.Parallel()
	if n := len(pages.PricingFAQ); n != 6 {
		t.Fatalf("PricingFAQ carries %d question(s); the Pricing board has six. Change this test "+
			"deliberately if the set changed.", n)
	}
	body := mustFetchMarketing(t, marketingRouter(t), pages.PricingPath)
	text := screenText(t, body)
	for _, banned := range []string{
		// U6: billing counts employment that overlaps the month, not a roster.
		"on the books",
		// U4: no product path corrects somebody's dates.
		"joining or leaving dates can still move it",
		// The webhook waits on the API's gate; the page names only the gate it pins.
		"integration API and webhooks are switched on",
		// O1's word, on every public page.
		"guarantee",
	} {
		if strings.Contains(text, banned) {
			t.Errorf("/pricing says %q, which the billing code does not keep (README §10)", banned)
		}
	}
	for _, must := range []string{
		"Somebody invited who never finished activation is not counted.",
		"Only an owner can freeze a month, and only once the month is over.",
	} {
		if !strings.Contains(text, must) {
			t.Errorf("/pricing no longer says %q", must)
		}
	}
	billing := between(body, `<section id="billing"`, "</section>")
	for _, p := range faqDisclosureProblems(billing, len(pages.PricingFAQ)) {
		t.Errorf("/pricing's billing questions: %s", p)
	}
}
