package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The ACCOUNT SECTION — M7-05, the READ side. The one write is in accountactions.go.
//
// 🔴 WHAT THIS SECTION IS NOT, BECAUSE THE CARD ASKED FOR IT AND THE MEASUREMENT SAID
// NO. The M7-05 card's first criterion reads "firma bilgileri, fatura verileri, plan
// görünümü". The middle two are ALREADY SHIPPED: /admin/billing (M6-12 phase B) prints
// the business name, the month, the timezone, the plan, the headcount, the unit price,
// the amount and the founding warning — and it does so behind an OWNER-ONLY gate that
// covers the READ as well as the write, because migration 00016 made the commercial
// terms the operator's rather than the customer's. Re-printing any of them here would
// undo that gate for every manager who can open a settings page. So this screen carries
// the business's IDENTITY and links to the money, and the link is drawn only for a
// reader who may follow it.
//
// 🔴 THE READ IS OPEN TO BOTH ROLES AND THE WRITE IS NOT, WHICH IS A DIFFERENT SHAPE
// FROM BILLING'S AND IS ARGUED RATHER THAN COPIED. Nothing on this page is a commercial
// term: a VAT number is public data anyone can put into VIES (migration 00001 says so
// where the column is declared), a business name is on every screen in the panel
// already, and the TIMEZONE is the one fact a manager most needs to be able to check —
// it is the zone every date they read is worked out in. What is closed is the SAVE,
// because each of the three editable fields changes something outside this screen:
// the invoice's addressee, the wall clock every lateness verdict is measured against,
// and the sentence every employee reads after a tap.
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5). It comes from httpx.AdminOf(r), resolved by
// the Protect chain from a signed session cookie against admin_users. There is no
// parameter on this screen a client can narrow or widen with — the whole page is one
// row, and which row is not theirs to choose.
//
// 🔴 §4.7 — NO SECRET AND NO PERSON REACHES THIS SCREEN. internal/domain/tenant's
// Settings has no field a token, a key, a coordinate or an employee could travel in,
// and pages.AccountView has none either.
//
// 🔴 §4.6 — A FAILED READ IS A PROBLEM PAGE, NEVER AN EMPTY FORM. A settings form
// pre-filled with blanks is an invitation to save blanks over a row that was read
// perfectly well a minute ago.
//
// ⚠️ `structure` IS NOT ON THIS SCREEN, AND THE FIRST DRAFT PUT IT THERE. It was shown
// read-only, with a sentence saying it decides nothing;
// TestSignupStructure_DecidesNothingAfterSignUp turned RED, and the tripwire is right —
// it bans READING the column outside the sign-up path, because the wizard's wording
// promises the answer stops mattering once the row is written, and a settings screen
// printing it would make this package its first reader. There is nothing to show
// either: the answer gates no feature, and the panel offers "Add a department"
// whichever it says.

// accountHref is the section's own URL, READ FROM THE SECTION TABLE rather than
// written out again — the rule every other section follows. If the tab moves, the form
// and every redirect move with it; if the tab is removed altogether this panics at
// startup rather than silently building links to nowhere.
var accountHref = mustSectionHref(pages.TabAccount)

// panelAccounts is the slice of internal/domain/tenant this section needs, declared
// HERE at the consumer (§7).
//
// ⚠️ IT IS A SIXTH FIELD ON internal/domain/tenant AND THAT IS STATED RATHER THAN
// SMUGGLED. staff, venues, plaques, rules and scribe are already five, and each states
// the same reason: one wide interface over the package would let a change to one
// section break another's wiring with no compile error at the call site. This one adds
// the argument that matters most here — it is the only value in the panel that can
// write to `tenants`, the row every other tenant-scoped query is anchored to.
type panelAccounts interface {
	Settings(ctx context.Context, tenantID uuid.UUID) (tenant.Settings, error)
	Save(ctx context.Context, c tenant.AccountCommand) (tenant.Settings, error)
}

// accountZoneSuggestions is what the browser offers under the timezone box.
//
// 🔴 IT IS A SUGGESTION LIST AND NOT A VOCABULARY, and the distinction is enforced by
// the server rather than by this comment: internal/domain/tenant.validZone accepts any
// name time.LoadLocation resolves, so a business in a zone that is not on this list
// types it and it is stored. A closed list would be a second representation of the
// IANA database, which gains entries every year.
//
// THE FOUR ARE THE MARKET'S: Malta is where this ships, Rome shares its offset, London
// is the nearest zone that does not, and UTC is the one a customer who wants no local
// zone at all would type. They are ordered by how likely they are rather than
// alphabetically.
var accountZoneSuggestions = []string{"Europe/Malta", "Europe/Rome", "Europe/London", "UTC"}

// accountDoneWords and accountProblemWords are the CLOSED vocabularies this section's
// query string may echo. A reflected parameter is a value somebody can put anything
// into, and the answer is to have nothing to escape (oneOfWords).
var (
	accountDoneWords    = []string{"saved"}
	accountProblemWords = []string{"unreadable", "not-permitted", "unknown-business", "unavailable"}
)

// accountDoneSentence and accountProblemSentence turn one of those words into the
// sentence a customer reads.
//
// EVERY BRANCH IS A SENTENCE ABOUT WHAT DID OR DID NOT HAPPEN, and the defaults are the
// honest ones: an unrecognised word cannot reach here (oneOfWords filters it) and, if
// it did, saying nothing is better than saying something untrue.
func accountDoneSentence(word string) string {
	if word == "saved" {
		return "These details are what Tappa now has on file for this business. " +
			"Everything on this page is read from the row that was just written, not " +
			"from what was typed."
	}
	return ""
}

func accountProblemSentence(word string) string {
	switch word {
	case "unreadable":
		return "That form could not be read, so nothing was changed."
	case "not-permitted":
		return "Only an owner of this business can change these details. Nothing was changed."
	case "unknown-business":
		return "This business's own record could not be read, so nothing was changed. That is our problem rather than yours."
	case "unavailable":
		return "That could not be done just now. Nothing was changed; please try again."
	default:
		return "That did not happen."
	}
}

// problemAccountUnreadable is what anybody sees when the row itself cannot be read.
//
// IT IS A SEPARATE VIEW FROM problemPanelUnavailable BECAUSE IT IS A DIFFERENT FACT. A
// signed session whose OWN tenant is invisible is a broken invariant rather than an
// outage, and telling somebody "try again in a minute" about it would send them round a
// loop that cannot end.
var problemAccountUnreadable = pages.ProblemView{
	Title:   "We could not read this business's own record",
	Message: "The panel is signed in but could not read the row describing this business, so nothing on this page would be true. Nothing has been changed.",
	Hint:    "This one is ours rather than yours — tell us and we will look.",
}

// accountSection renders the whole screen: the facts on file, the VAT register's
// answer, the form (or the sentence explaining its absence) and the staff sentences.
func (a *AdminAuth) accountSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	s, err := a.accounts.Settings(r.Context(), id.TenantID())
	if err != nil {
		// §4.6: say the read failed. Do NOT fall through to an empty form.
		a.log.Error("panel: could not read the business account", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAccountUnreadable)
		return
	}

	v := a.accountView(r, id, s)
	// THE ECHOED WORDS ARE FILTERED THROUGH THE CLOSED LISTS BEFORE THEY BECOME
	// SENTENCES, so a query string carrying anything at all produces no notice rather
	// than a reflected value.
	if word := oneOfWords(strings.TrimSpace(r.URL.Query().Get("done")), accountDoneWords...); word != "" {
		v.Saved = accountDoneSentence(word)
	}
	if word := oneOfWords(strings.TrimSpace(r.URL.Query().Get("problem")), accountProblemWords...); word != "" {
		v.Problem = accountProblemSentence(word)
	}
	a.render(w, r, http.StatusOK, pages.AdminAccount(v))
}

// accountView maps stored settings onto the screen.
//
// EVERY RENDERING DECISION IS HERE AND ONLY HERE: the VAT wording, the structure's
// words, the date's format, and whether the form and the billing link are drawn.
func (a *AdminAuth) accountView(r *http.Request, id httpx.AdminIdentity, s tenant.Settings) pages.AccountView {
	// 🔴 THE ZONE IS RESOLVED ONCE AND EVERY DATE ON THIS SCREEN GOES THROUGH IT. The
	// first version printed the registration date with `.UTC()` while the line three
	// rows below it says `Europe/Malta`, both under a heading reading "On file" —
	// measured: a business that registered at 00:30 Malta on 2 February
	// (2026-02-01T23:30:00Z) was told "1 February 2026", a day it was not yet a
	// customer. CLAUDE.md §6 names the class ("gece vardiyası bug'larının kaynağı
	// budur") and it is the same defect this session already fixed once in a ledger
	// test; `s.Timezone` was in the same struct the whole time.
	//
	// THE HOUSE RULE, MEASURED RATHER THAN ASSUMED: eleven customer-facing panel dates
	// already render through In(zone) (employees, plaques, policies, review,
	// transactions, reports, anomalies, manual entry, the CSV header, the tap result).
	// The ones printed in UTC either carry an explicit UTC stamp or are TAPPA's own
	// dates, not the customer's.
	zone := a.accountZone(s.Timezone)
	v := pages.AccountView{
		PanelChrome:     a.chrome(r, pages.TabAccount),
		VATNumber:       s.VATNumber,
		RegisteredOn:    s.CreatedAt.In(zone).Format("2 January 2006"),
		Types:           businessTypeOptions(),
		ZoneSuggestions: accountZoneSuggestions,
		PostHref:        accountHref,
		NameMaxLength:   tenant.AccountNameLimit,
		CanEdit:         mayEditAccount(id),
		// 🔴 BOTH FIELDS ARE FILLED FROM THE STORED ROW HERE, AND ONLY Form IS EVER
		// OVERWRITTEN AFTERWARDS (renderAccountFormAgain). That asymmetry is what makes
		// "on file" a fact rather than an echo: OnFile has exactly one source in this
		// package and it is the database.
		Form: pages.AccountForm{
			Name:         s.Name,
			BusinessType: s.BusinessType,
			Timezone:     s.Timezone,
		},
		OnFile: pages.AccountForm{
			Name:         s.Name,
			BusinessType: s.BusinessType,
			Timezone:     s.Timezone,
		},
		BrandNote: "These two lines are the send-off after a tap that counted. They " +
			"follow the kind of business above rather than being written per business, " +
			"so every restaurant on Tappa reads the same one — writing your own is not " +
			"built yet. They are never shown on a training tap, or on a tap that is " +
			"waiting for a manager.",
	}
	if !v.CanEdit {
		v.WhyNotEdit = "These details are changed by an owner of the business rather " +
			"than by a manager. Nothing is wrong with your account, and everything above " +
			"is the whole of what is on file — the name, the kind of business and the " +
			"timezone are the three that can move."
	}
	// THE BILLING LINK IS DRAWN ONLY FOR SOMEBODY WHO MAY OPEN IT. mayManageBilling is
	// the same predicate /admin/billing refuses on, read rather than restated.
	if mayManageBilling(id) {
		v.BillingHref = billingHref
	}
	fillAccountVAT(&v, s.VAT, zone)
	return v
}

// accountZone resolves the business's own zone for the render layer, falling back to
// UTC LOUDLY.
//
// 🔴 IT FALLS BACK RATHER THAN REFUSING THE PAGE, WHICH IS WHAT EVERY OTHER READER OF
// THIS COLUMN DOES (ledger.loadZone, tenant.Plaques.loadZone, tenant.Rulebook.loadZone,
// billingMonth). A zone this binary cannot load is a stored value somebody has to be
// able to SEE in order to correct — and this is the one screen where they can correct
// it. Refusing to render would lock the customer out of the fix.
//
// ⚠️ THE FALLBACK IS UNREACHABLE THROUGH THIS SCREEN'S OWN WRITE PATH and is here for
// what the row may ALREADY hold. internal/domain/tenant.ValidTimezone refuses anything
// time.LoadLocation cannot resolve, so nothing saved here can produce it; a row written
// before that gate existed, or by an operator's SQL, still can.
//
// IT IS NEVER SWALLOWED (§7): the failure is logged with the name that would not load.
func (a *AdminAuth) accountZone(name string) *time.Location {
	if name == "" {
		a.log.Warn("panel: this business has no timezone; showing its dates in UTC")
		return time.UTC
	}
	zone, err := time.LoadLocation(name)
	if err != nil {
		a.log.Error("panel: cannot load the business timezone; showing its dates in UTC",
			"zone", name, "err", err)
		return time.UTC
	}
	return zone
}

// fillAccountVAT turns migration 00017's two columns into a word, a frame and a
// sentence.
//
// 🔴 FOUR STATES AND FOUR SENTENCES, WHICH IS WHY THE COLUMN IS NULLABLE. "We never
// asked", "we asked and got no answer", "the register says yes" and "the register says
// no" are four different facts and only one of them is a problem for the customer. A
// screen that collapsed them into a tick or a cross would be writing an EU outage down
// as an accusation — the exact thing internal/domain/signup/vies.go exists to prevent.
//
// ⚠️ ONE OF THE FOUR IS UNREACHABLE FROM THE PRODUCT TODAY AND THE BRANCH IS KEPT
// ANYWAY. internal/domain/signup.checkedAt stores NULL in both columns for VATUnknown —
// its own comment records that it cannot tell a timeout from an unwired checker — so a
// registration writes either both or neither, and VATNoAnswer is a shape the schema
// allows and the wizard never produces. tenant.VATCheck.State carries the same
// measurement. Deleting the branch would make the screen print "never asked" for a row
// that says otherwise, the first day anything writes one.
//
// 🔴 THE COLOUR IS THE FIXED MAPPING AND CARRIES NOTHING ON ITS OWN (skill
// tappa-brand): every state prints its own word, and the text is ink in all four.
//
// 🔴 THIS FUNCTION SETS A STATE TOKEN AND NEVER A CLASS NAME, AND THE FIRST DRAFT DID
// THE OPPOSITE AND WAS MEASURED BROKEN. Tailwind scans .templ and .js, never Go, so
// `bg-line/10` assembled here compiled to nothing at all in app.css — the "not checked"
// marker lost its tint while the other three states survived by borrowing rules other
// templates had already caused to exist. The classes now live in account.templ's
// accountVATMarker, where the tool can see them; view.go records the same failure
// costing the tap screen every one of its stamps.
func fillAccountVAT(v *pages.AccountView, c tenant.VATCheck, zone *time.Location) {
	// 🔴 IT IS THE CUSTOMER'S WALL CLOCK, NOT RFC3339. This was the one raw machine
	// timestamp the panel showed a customer: "Asked 2026-05-03T11:15:00Z". It was
	// HONEST — the Z said which zone — but honesty is not the bar for a date somebody
	// reads, and it is the same family as the registration date above: a business
	// reading its own record should read it in its own clock. The minute is kept
	// because "when did you ask" is a moment; the seconds are dropped because nobody
	// reconciles a VIES lookup to the second.
	if c.CheckedAt != nil {
		v.VATCheckedAt = c.CheckedAt.In(zone).Format("2 January 2006, 15:04")
	}
	// THE TOKEN IS THE DOMAIN'S OWN VALUE, so the template's switch and the domain's
	// four states cannot drift into three and five.
	v.VATState = string(c.State())
	switch c.State() {
	case tenant.VATValid:
		v.VATStateWord = "Confirmed"
		v.VATLine = "When this business registered, the European Commission's VAT " +
			"register (VIES) confirmed this number. Nothing has asked again since, and " +
			"nothing needs to: the number itself cannot be changed from this page."
	case tenant.VATInvalid:
		v.VATStateWord = "Not found"
		v.VATLine = "When this business registered, the European Commission's VAT " +
			"register (VIES) did not recognise this number. That did not stop the " +
			"registration and it does not affect anything the product does — but an " +
			"invoice made out to a number the register does not know is a problem for " +
			"your accountant, so tell us and we will correct it."
	case tenant.VATNoAnswer:
		v.VATStateWord = "No answer"
		v.VATLine = "The VAT register was asked about this number and did not answer — " +
			"an outage at their end, not a verdict about your business. Nothing follows " +
			"from it either way."
	default:
		// 🔴 THIS BRANCH COVERS TWO EVENTS AND THE FIRST VERSION NAMED ONLY ONE. It said
		// "Nothing has asked the European Commission's VAT register about this number" —
		// and for most businesses in this state that is FALSE. internal/domain/signup's
		// checkedAt stores NULL in BOTH columns for VATUnknown (its own comment records
		// the collapse: it cannot tell a timeout from an unwired checker), so a customer
		// whose sign-up screen said "we could not reach the EU VAT register just now"
		// lands in exactly this branch. Two screens then described one event, one saying
		// "we asked" and the other "nobody asked".
		//
		// THE SENTENCE NOW NAMES THE COHORT THE WAY THE SIGN-UP SCREEN DOES and admits
		// what the record cannot distinguish, rather than picking the half that happens
		// to be wrong more often.
		v.VATStateWord = "Not checked"
		v.VATLine = "There is no answer from the European Commission's VAT register on " +
			"file for this number. Two things look the same here and this record cannot " +
			"tell them apart: nobody asked, and we asked when this business registered " +
			"and could not reach the register — if your sign-up screen said we could not " +
			"reach it, this is that. Either way it is not a judgement about the number: " +
			"the check is a courtesy at registration and it never blocks anything."
	}
}
