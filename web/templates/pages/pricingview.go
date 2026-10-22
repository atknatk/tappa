package pages

// THE PRICING PAGE (LP-06, 2026-10-22): /pricing, drawn from the Pricing / T-Pricing /
// M-Pricing boards (docs/design/landing-redesign/boards). It is the first of the five
// public pages beyond the landing, and the shape it uses is the one the other four
// reuse (README §7, "How a new page joins"):
//
//   - THE ROUTE IS A ROW OF PublicPages (landingview.go), the table internal/handler's
//     tests read to list the public surface; Mount registers PricingPath under its own
//     name and a test holds the two equal in both directions.
//   - EVERY SENTENCE IS A VALUE HERE with the facts it rests on (Line, Question), in the
//     same three vocabularies as the landing. A fact only this page claims is declared
//     in this file's own Fact block; the closure tests read every view file in this
//     package, so it is held exactly like a landing fact.
//   - EVERY NUMBER IS COMPUTED BY internal/handler from the published price and handed
//     in through PricingView, never typed here or in the template.
//
// The price card at the top is the landing's own (landing.templ priceCard), with the
// landing's lines; this page adds the worked examples, a drawn frozen month and the
// billing questions.

// PricingPath is the page's URL.
const PricingPath = "/pricing"

// PricingView is the pricing page.
type PricingView struct {
	// SignupHref and SignInHref are the chrome's two ways in, as on LandingView.
	SignupHref string
	SignInHref string
	// PricePerEmployeeMonth, FreeMonths, ExampleHeadcount and ExampleMonthly are the
	// price card's, exactly as LandingView carries them (the card is the same component).
	PricePerEmployeeMonth string
	FreeMonths            int
	ExampleHeadcount      int
	ExampleMonthly        string
	// Figures are the examples and the drawn month, computed by internal/handler from the
	// published price in integer cents. Empty Sizes means the price could not be read,
	// and the page then draws no table and no month rather than a wrong figure.
	Figures PricingFigures
}

// PricingFigures is what internal/handler computes for the page: one row per team size
// and the drawn frozen month.
type PricingFigures struct {
	Sizes   []PricingSize
	Receipt PricingReceipt
}

// PricingSize is one row of "What a month costs at your size": a headcount and what it
// costs a month at the published price, formatted without a currency sign ("37.50").
type PricingSize struct {
	People  int
	Monthly string
}

// PricingReceipt is the drawn frozen month: the people counted, the price and the total,
// each formatted the way the Billing screen prints money ("€90.00").
type PricingReceipt struct {
	People string
	Price  string
	Total  string
}

// The facts only this page claims. Each is derived from the product in
// internal/handler/pricing_facts_test.go; the closure test reads this block like the
// one in landingview.go.
const (
	// Somebody is counted for a month when their employment -- from the moment they
	// finished activation to the moment they were switched off -- overlaps it, and
	// somebody who never finished activation is not counted. Derived from migration
	// 00016's tappa_employee_is_billable (activated_at IS NOT NULL, activated_at < the
	// month's end, deactivated_at after its start) and from both billing statements
	// calling it over the month's own bounds (db/queries/billing.sql). README §10 U6:
	// the board's "on the books for even one day" was looser than this rule.
	FactBillingCountsWhoWasActive Fact = "sql:tappa_employee_is_billable([activated_at, deactivated_at) overlaps the month)"
	// A month's amount is the people counted times the price per person, and zero inside
	// the founding offer's free months. Derived from migration 00016's
	// tappa_billing_amount_due, the one function both the live figure and the frozen
	// column use.
	FactBillingAmountDue Fact = "sql:tappa_billing_amount_due(count × price, 0 when free)"
	// A frozen month is stored and is the answer from then on; until then the month is
	// worked out afresh on every read. Derived from billing.Book.Period (the frozen row
	// first, the live preview only when there is none) and from migration 00016 making
	// billing_periods append-only (UPDATE and DELETE revoked, and a trigger refusing
	// both). README §10 U4: the board's "correcting somebody's joining or leaving dates
	// can still move it" found no product path that corrects those dates, so the page
	// says the softer thing the freeze code does say.
	FactBillingFreezeIsFinal Fact = "billing:Period reads the frozen row first+billing_periods append-only"
	// Only an owner freezes a month, and only a month that is over. Derived from
	// mayManageBilling (the owner role) guarding the two close routes, and from
	// CloseBillingPeriod inserting nothing for a month whose end is still ahead.
	FactBillingFreezeIsOwnersOnceTheMonthIsOver Fact = "handler:mayManageBilling owner+sql:CloseBillingPeriod to_at <= now()"
	// The Billing screen downloads any month as CSV, frozen or not: the export reads the
	// month through the same Book.Period the screen does. Derived from the route, the
	// attachment, and the export's read.
	FactBillingCSVForAnyMonth Fact = "route:GET billingCSVHref over Book.Period"
	// The Billing screen names the first month that is charged for. Derived from the
	// plan card (pages/billing.templ) printing FirstCharged and internal/handler filling
	// it from the database's first chargeable month.
	FactBillingNamesTheFirstChargedMonth Fact = "templ:billing plan card first chargeable month"
	// The Billing screen lists a frozen month as a receipt stamped FROZEN, with its
	// people, price and total and a CSV link -- what the drawing on this page shows.
	// Derived from pages/billing.templ's billingReceipt and billingHistory.
	FactBillingShowsFrozenMonthsAsReceipts Fact = "templ:billingReceipt stamped FROZEN"
)

// PricingExamplesLede sits under "What a month costs at your size.".
var PricingExamplesLede = Line{
	Text:  "People counted in the month, times the price per person.",
	Facts: []Fact{FactBillingCountsWhoWasActive, FactBillingAmountDue, FactPricePerEmployee},
}

// PricingFreeNote sits under the table of team sizes.
var PricingFreeNote = Line{
	Text:  "Inside the founding offer's free months, the amount is zero.",
	Facts: []Fact{FactBillingAmountDue},
}

// PricingReceiptCaption sits under the drawn frozen month. The drawing's month, its
// frozen time and its figures are sample data; the caption says so.
var PricingReceiptCaption = Line{
	Text:  "A frozen month on the Billing screen — sample figures.",
	Facts: []Fact{FactBillingShowsFrozenMonthsAsReceipts, FactBillingFreezeIsFinal},
}

// The drawn month's sample furniture. The month and the moment it was frozen are
// invented for the drawing (frozen on the 1st of the next month, after it ended, as
// CloseBillingPeriod requires); its people, price and total come from PricingReceipt.
const (
	PricingReceiptMonth    = "September 2026"
	PricingReceiptFrozenAt = "2026-10-01T08:12:40Z"
)

// PricingFAQ is "How the month is counted." -- the Pricing board's six questions, each
// answer rewritten where the board said more than the product does:
//
//   - "on the books for even one day of it" (U6): the count is over employment that
//     overlaps the month, and an invitation never finished is not employment.
//   - "correcting somebody's joining or leaving dates can still move it" (U4): no
//     product path corrects those dates, so the answer says what the code does say --
//     an unfrozen month is worked out afresh each time it is read.
//   - "Only the owner": a business can have more than one owner, so "an owner".
//   - "the integration API and webhooks": the webhook waits for the API, but the pin
//     here is the API's gate, so the answer names the API alone.
var PricingFAQ = []Question{
	{
		Q: "Who counts as a person?",
		A: "Everybody who was active at any point in the month — from the moment they finished " +
			"activation until they were switched off. Somebody invited who never finished " +
			"activation is not counted.",
		Facts: []Fact{FactBillingCountsWhoWasActive},
	},
	{
		Q: "Does hiring or letting someone go change a past month?",
		A: "No. A finished month is counted over its own dates, not over today's list of people. " +
			"Until it is frozen, its figures are worked out afresh each time they are read.",
		Facts: []Fact{FactBillingCountsWhoWasActive, FactBillingFreezeIsFinal},
	},
	{
		Q: "What does freezing a month do?",
		A: "It fixes that month's figures: nothing recomputes them afterwards, and it can't be " +
			"undone. Only an owner can freeze a month, and only once the month is over.",
		Facts: []Fact{FactBillingFreezeIsFinal, FactBillingFreezeIsOwnersOnceTheMonthIsOver},
	},
	{
		Q:     "Can we check the figure ourselves?",
		A:     "Yes. Every month downloads as CSV from the Billing screen, frozen or not.",
		Facts: []Fact{FactBillingCSVForAnyMonth, FactBillingExportAsCSV},
	},
	{
		Q: "When does the founding offer start charging?",
		A: "The Billing screen names the first month that is charged for, so the end of the " +
			"free months is never a guess.",
		Facts: []Fact{FactBillingNamesTheFirstChargedMonth},
	},
	{
		Q: "How do we get the integration API?",
		A: "Ask Taptime: the integration API is switched on for a business by Taptime, not " +
			"from your dashboard.",
		Facts: []Fact{FactAPIEnabledPerBusinessByTaptime},
	},
}
