package pages

// The BILLING section's view models — M6-12 phase B.
//
// SAME RULE AS THE REST OF THIS PACKAGE: a template can only render what a view
// carries, and here that rule does a specific job. §4.7 keeps a person out of an
// invoice, and internal/domain/billing's package comment states the wall on the
// domain side — the queries touch `employees` as a COUNT and never as a row, and
// billing.Draft has no field a name could travel in. These types have none either:
// the only NAME below is the BUSINESS's, which is who the document is addressed to.
//
// 🔴 THERE IS NO FIELD, AND NO LINK, THAT COULD OFFER AN ITEMISATION. A closed
// period freezes the NUMBER and not WHO WAS COUNTED — migration 0016 states why
// storing the ids was rejected rather than forgotten, and billing.Draft.EmployeeCount
// repeats it — so "show me the people behind this figure" is a promise the data
// cannot keep for an old period: anybody anonymised under GDPR since no longer
// resolves, and after retention expiry the question has no answer at all. A screen
// that offered the button would be making the promise. What the screen does instead
// is SAY SO, in one sentence, which is BillingView.Itemisation.
//
// 🔴 EVERY MONEY VALUE ARRIVES AS A PRE-RENDERED STRING. billing.Money is exact
// integer minor units and its own file forbids float64; formatting it is the render
// layer's job (Money.String is symbol-free and grouping-free on purpose), so the
// handler does that once and this package carries the result. A view model holding a
// number a template had to format would be the second place money is rendered.

// BillingView is the section: one month's figures, what may be done to them, and the
// frozen history beside them.
type BillingView struct {
	PanelChrome

	// Queried is §4.6's anti-fabrication flag, the same one ReportsView carries. A
	// zero-amount draft and a draft nobody could read must not render identically:
	// "this month costs nothing" is a claim about a business, and a failed read is
	// not evidence for it. The handler renders a problem page on a failed read, so
	// this being false means the body is not drawn at all.
	Queried bool

	// --- the month being looked at, and the ways to move ---------------------

	// MonthValue is the ISO year-month the picker echoes ("2026-07"), and MonthLabel
	// is the same month in words ("July 2026").
	MonthValue string
	MonthLabel string
	// ZoneLabel is the IANA zone the month's boundaries were resolved in. It is
	// printed beside the label for §6's reason: "July" is two different intervals in
	// two zones, and an overnight shift lives in the difference.
	ZoneLabel string
	// FromUTC and ToUTC are those boundaries as ISO 8601 instants, To EXCLUSIVE. They
	// are shown because a frozen row keeps the boundary it was COUNTED over even if
	// the tenant later changes zone, so the two can legitimately disagree with what
	// ZoneLabel would produce today.
	FromUTC string
	ToUTC   string

	SectionHref string
	PrevHref    string
	NextHref    string
	LatestHref  string
	ExportHref  string

	// --- the draft itself -----------------------------------------------------

	// Frozen says which of two DIFFERENT documents this is, and it is the field this
	// whole task exists for. A frozen figure was READ from billing_periods and nothing
	// in it was recomputed; a draft was computed from today's roster and today's price
	// and WILL move if either does. billing.Draft's own comment calls rendering them
	// identically "the failure this whole task exists to prevent".
	Frozen bool
	// Stamp and StampClass are the rubber stamp that carries Frozen to the eye. The
	// WORD is what says it (skill tappa-brand: status is never colour alone); the
	// class only tints the frame.
	Stamp      string
	StampClass string
	// StateLine is Frozen in a sentence, because a stamp is a label and this is a
	// claim about whether the number can still change.
	StateLine string

	TenantName string
	// PlanLabel is 'founding' or 'standard' in words.
	PlanLabel string
	// EmployeeCount is the billable headcount, already rendered.
	EmployeeCount string
	// CountLine explains what was counted, in the definition's own terms. The M6-12
	// card asks for the definition to be visible on the screen and not only in a
	// migration.
	CountLine string
	// UnitPrice and Amount are money, rendered with a symbol by the handler.
	UnitPrice string
	Amount    string
	// FreeLine is non-empty when the month falls inside the founding offer's free
	// window, and says the amount is zero BECAUSE of the offer rather than because
	// nobody worked.
	FreeLine string
	// ClosedLine is non-empty only on a frozen period: when it was closed.
	ClosedLine string

	// --- the honesty block ------------------------------------------------------

	// Unstamped is non-empty when billing_periods.unstamped_employees is above zero,
	// and it is the sentence EmployeeCount is a FLOOR by. See BillingView's own
	// handler for the wording and for the upper-bound caveat that must travel with it.
	Unstamped string
	// UnstampedCount is the raw figure beside that sentence.
	UnstampedCount string

	// Founding is the founding-offer warning: non-empty once the current month has
	// reached the first chargeable one, so the free period cannot end quietly.
	Founding string
	// FoundingHeading is the notice's own heading, because a Notice carries its
	// meaning in the heading rather than in the tone.
	FoundingHeading string

	// Itemisation is the sentence that says what a frozen figure does NOT keep. It is
	// a field rather than fixed template prose because the two documents say it
	// differently: a frozen period cannot be itemised at all, a live draft could be
	// re-counted today but will not be able to be later.
	Itemisation string

	// --- the one irreversible control ------------------------------------------

	// CanClose is true only when every condition holds: the month has ended, it is
	// not before signup, it is not already frozen, and this admin may do it. It is
	// the SCREEN's copy of conditions the statement enforces itself and is never the
	// authority — internal/domain/billing.Close carries all three inside the SQL.
	CanClose bool
	// CloseHref is the confirmation step's address; CloseLabel is the button.
	CloseHref  string
	CloseLabel string
	// WhyNotClose is why the button is absent, in one sentence. It is shown whenever
	// CanClose is false and the month is not already frozen: a control that silently
	// disappears tells a manager nothing.
	WhyNotClose string

	// --- the history ------------------------------------------------------------

	History []BillingHistoryRow
	// HistoryCapped is true when there are more frozen periods than one read carries,
	// and HistoryCapLabel names the cap. The pair is M6-07's and M6-11's: a bounded
	// list must say it is bounded.
	HistoryCapped   bool
	HistoryCapLabel string
	// HistoryEmpty is the sentence for a business that has never frozen a month. It
	// is a measured claim rather than a blank space.
	HistoryEmpty string

	// Problem is a word from a closed vocabulary, echoed from the query string after
	// a refused close. The handler maps it to a sentence; nothing free-text is
	// reflected here.
	Problem string
}

// BillingHistoryRow is one frozen month in the list.
//
// IT CARRIES NO ACTIONS AND NO LINK TO A BREAKDOWN, deliberately: billing_periods
// takes no UPDATE and no DELETE from anybody, so there is nothing to offer, and the
// figure behind it cannot be itemised (see the file header).
type BillingHistoryRow struct {
	MonthValue string
	MonthLabel string
	Employees  string
	UnitPrice  string
	Amount     string
	ClosedAt   string
	// Free marks a month the founding offer covered, so a zero row reads as "the offer
	// paid for it" rather than "nobody was employed".
	Free bool
	// Unstamped is non-empty when that FROZEN row carried a non-zero honesty column,
	// so a history line makes the same admission the headline figure does.
	Unstamped string
}

// BillingCloseView is the confirmation step for freezing a month.
//
// IT IS ITS OWN VIEW RATHER THAN A FLAG ON BillingView, for PolicyChangeView's
// reason: the warning screen and the section are different documents, and a section
// with a "now I am a warning" mode is a section that can render the warning by
// accident.
type BillingCloseView struct {
	PanelChrome

	Heading string
	Summary string
	// Detail is what will be frozen, spelled out — the same figures the section
	// showed, so the reader can check the screen against itself before pressing.
	Detail []BillingLine
	// Warning is the consequence, and it says the two things that are true: the row
	// can never be edited or removed, and the figures are re-counted at the instant of
	// the close rather than taken from this page.
	Warning string

	// Action is where "freeze it" posts. Month is echoed as a hidden input;
	// ConfirmField/ConfirmToken are the MAC that binds this warning to this month and
	// this session.
	Action       string
	Month        string
	ConfirmField string
	ConfirmToken string
	BackHref     string
}

// BillingLine is one labelled fact, the shape PolicyChangeLine already uses.
type BillingLine struct {
	Label string
	Value string
}
