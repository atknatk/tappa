package operator

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// ONE TENANT'S BILLING MONTHS (M10 OP-12, phase B; ADR 0020 §4, §9; ADR 0021 §2 v, OP-12
// note). GET /operator/tenants/{id}/billing is the first page and POST
// /operator/tenants/{id}/billing a page the form names; both sit in the console's group
// (routes.go, mount): host gate, security headers, flood gate, same-origin gate (a GET a
// browser labels same-site or cross-site is the sign-in redirect), requireOperator,
// sessionGate. The overview links the screen; the console does not -- the months belong to
// one tenant. There is no platform-wide billing list (the orchestrator's K12-1 (a)), and no
// export (K12-4).
//
// EVERY VIEW IS ONE TWO-PHASE READ through BillingStore: op_begin_read commits the view's
// 'read' row (target_scope tenant_billing, the tenant and the page named), then
// op_read_tenant_billing consumes the ticket and returns the tenant's name, the fact that it
// exists and twelve of its months, newest first -- under the read's own time bound
// (db.TenantBillingReadTimeout: a slow read is a database error, the 503 below). The banner's
// name comes from that same read. The ticket never reaches this package and this file writes
// no SQL.
//
// THE PAGE IS SENT IN THE REQUEST'S BODY, NOT IN ITS URL -- the surface's one rule for a
// paged read (tenants.go): GET reads nothing from the URL but the path's id; POST carries the
// page in its form and answers 200 with it (no redirect: a reload is one more read, audited
// as one).
//
// 🔴 A FAILED READ IS A PROBLEM PAGE, NEVER A ZERO INVOICE (CLAUDE.md §4.6 in its money form;
// the tenant's own screen's rule). Every error but the two the read names -- an id no tenant
// has, a session refused -- is the 503 page, which carries no figure; a read whose answer is
// not the page asked for (another tenant, another page, not twelve months) is one too. A
// MONTH the screen cannot read as money -- a value MoneyFromNumeric refuses, or a row whose
// fields are not one of the documented shapes -- is drawn as unreadable with no figure, never
// as zero (billingRowOf).
//
// 🔴 MONEY STAYS EXACT, AND ITS ONE BRIDGE IS billing.MoneyFromNumeric. The read hands over
// pgtype.Numeric (a mantissa and a decimal exponent); this file turns it into billing.Money
// there and nowhere else, adds the symbol (the render layer's job -- billing.Money.String
// carries none), and multiplies nothing: unit price x count happened in PostgreSQL.
// TestBillingMoney_OneBridgeAndNoFloat pins, on the package's types, what it lists and no more:
// no float-typed expression and no *, / or % in this file but the page number's digits, no
// math/big here and no big.Float or big.Rat in the package, and a row's amount and price each
// moneyText's reading of the month's own figure, written once.

// BillingStore is the operator database's slice the billing screen needs, declared at the
// consumer (CLAUDE.md §7). *db.OperatorDB implements it by delegating to internal/db's
// TenantBilling (TestOperatorDB_EveryMethodDelegatesVerbatim), and that type's method set is
// derived from this interface, AuditStore, PlaqueStore, TenantStore, LegalStore and
// operatorauth.Store (TestOperatorDB_IsTheStoreAndNothingMore). sessionHash is operatorauth's
// SessionHash of the request's token -- on ADR 0020 §5's never-log list. The signature carries
// no read ticket: the method is both phases.
type BillingStore interface {
	// TenantBilling is op_begin_read + op_read_tenant_billing: the tenant's name and twelve of
	// its months, newest first, page 1 holding the month it is in. An id no tenant has is
	// db.ErrNoSuchTenant -- after the 'read' row naming it committed; a dead session is
	// db.ErrOperatorRefused; a page outside 1..db.MaxTenantBillingPage is a database error.
	TenantBilling(ctx context.Context, sessionHash string, tenantID uuid.UUID, page int32) (db.TenantBillingTimeline, error)
}

// maxBillingPage is the screen's last page: internal/db's own bound (op_begin_read refuses
// any other page), so the handler refuses exactly what the database would, before the store
// and before the read budget. Five pages of twelve are sixty months, the tenant's own history
// depth (billing.HistoryCap).
const maxBillingPage = db.MaxTenantBillingPage

// errBillingNotThePageAsked is the screen's refusal of a read whose answer is not the page
// it asked for: another tenant, another page, or not db.TenantBillingMonthsPerPage months.
// internal/db cannot return one (the statement filters on the id; readTenantBilling refuses a
// row of another tenant); this is the handler's copy of that rule, because the banner would
// otherwise name a tenant whose months were never asked for.
var errBillingNotThePageAsked = errors.New("operator: the billing read is not the page asked for")

// tenantBilling is GET /operator/tenants/{id}/billing: the first page.
//
// The refusals, in order: the session (storeSession); an id that is not the 36-character
// hyphenated form (tenantID) -- 404 with no store call and no unit of the read budget. Then
// readBilling.
func (s *Surface) tenantBilling(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	tenant, ok := tenantID(chi.URLParam(r, "id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemTenantNotAnID)
		return
	}
	s.readBilling(w, r, id, hash, tenant, 1)
}

// pageTenantBilling is POST /operator/tenants/{id}/billing: the page the form names.
//
// The refusals, each before any store call and before the read budget is charged, each a
// fixed page that says nothing was read: an id that is not the hyphenated form (404), a body
// over maxFormBytes (413), an unreadable form (400, readForm), a page outside
// 1..maxBillingPage (400).
func (s *Surface) pageTenantBilling(w http.ResponseWriter, r *http.Request) {
	id, hash, ok := s.storeSession(w, r)
	if !ok {
		return
	}
	tenant, ok := tenantID(chi.URLParam(r, "id"))
	if !ok {
		s.problem(w, r, http.StatusNotFound, problemTenantNotAnID)
		return
	}
	if !s.readForm(w, r, maxFormBytes, problemBillingFormTooLarge) {
		return
	}
	page, ok := billingPage(r.PostForm.Get("page"))
	if !ok {
		s.problem(w, r, http.StatusBadRequest, problemBillingPageRefused)
		return
	}
	s.readBilling(w, r, id, hash, tenant, page)
}

// readBilling charges the read's unit of the read budget, reads the page and renders it.
//
// An id no tenant has is a 404 after the 'read' row naming it committed; a session the read
// refuses is the sign-in's 303; any other error -- the read's time bound included, and an
// answer that is not the page asked for -- is the 503 page, whose log line names the tenant's
// id and the page and not the session's hash.
func (s *Surface) readBilling(w http.ResponseWriter, r *http.Request, id operatorauth.Identity, hash string,
	tenant uuid.UUID, page int32) {
	if !s.spendRead(w, r, id) {
		return
	}
	tl, err := s.billingStore.TenantBilling(r.Context(), hash, tenant, page)
	if err == nil && (tl.TenantID != tenant || tl.Page != page || len(tl.Months) != db.TenantBillingMonthsPerPage) {
		err = errBillingNotThePageAsked
	}
	switch {
	case errors.Is(err, db.ErrNoSuchTenant):
		s.problem(w, r, http.StatusNotFound, problemNoSuchTenant)
		return
	case errors.Is(err, db.ErrOperatorRefused):
		// The session predicate passed in sessionGate and refused now: the session ended in
		// between. The sign-in, as the gate would answer.
		s.redirect(w, pathSignIn)
		return
	case err != nil:
		// internal/db's error is the call and a SQLSTATE (operatorErr) -- 57014 for the time
		// bound; the hash is not an argument of this line.
		s.log.ErrorContext(r.Context(), "operator: a tenant's billing could not be read",
			"tenant_id", tenant.String(), "page", int(page), "err", err)
		s.problem(w, r, http.StatusServiceUnavailable, problemBillingUnreadable)
		return
	}
	name, err := tenantBanner(tl.TenantID, tl.TenantName)
	if err != nil {
		// Not reached: tenantBanner names a tenant without a visible name by its id. Were it
		// reached, the zero name makes TenantScreen refuse and render answers a plain 500 --
		// this line says why first.
		s.log.ErrorContext(r.Context(), "operator: a tenant could not be named on its billing screen", "tenant_id", tenant.String(), "err", err)
	}
	v := operatorpages.TenantBillingView{
		Name:          name,
		ID:            tl.TenantID.String(),
		OverviewPath:  pathTenants + "/" + tl.TenantID.String(),
		BillingPath:   billingPath(tl.TenantID),
		Page:          int(page),
		LastPage:      maxBillingPage,
		MonthsPerPage: db.TenantBillingMonthsPerPage,
	}
	for _, m := range tl.Months {
		row, why := billingRowOf(m)
		if why != "" {
			// The reason is one of billingRowOf's fixed phrases; the row's values are not
			// repeated here.
			s.log.ErrorContext(r.Context(), "operator: a billing month could not be read", "tenant_id", tenant.String(),
				"month", row.Month, "why", why)
		}
		v.Rows = append(v.Rows, row)
	}
	s.render(w, r, http.StatusOK, operatorpages.TenantBilling(v))
}

// billingPage is the posted page number -- the tenant pager's rule (tenantPage), bounded by
// maxBillingPage: absent or empty is the first page; otherwise decimal digits only,
// 1..maxBillingPage. No sign, no space, no exponent.
func billingPage(raw string) (int32, bool) {
	if raw == "" {
		return 1, true
	}
	if len(raw) > len(strconv.Itoa(maxBillingPage)) {
		return 0, false
	}
	n := 0
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > maxBillingPage {
		return 0, false
	}
	return int32(n), true
}

// billingPath is a tenant's billing screen, /operator/tenants/<id>/billing -- the overview's
// link to it and its pager's action.
func billingPath(tenant uuid.UUID) string {
	return pathTenants + "/" + tenant.String() + "/billing"
}

// billingWord is how the screen says one month's state: the chip's label, the sentence
// beside it and the chip's tone.
type billingWord struct {
	label, sentence string
	tone            operatorpages.BillingTone
}

// The screen's five words. Each states what the read says and nothing more (B3 of the OP-12
// card): "Ended, not closed by the business" is said only of a month the read returns as not
// frozen, ended and after sign-up, and it is a FACT about the record -- the product holds no
// payment data, so no word here claims a month is owed, late or settled.
// TestBillingScreen_EveryStateSaysItsWordAndNoVerdict holds the five and their conditions.
var (
	billingFrozen = billingWord{"Frozen",
		"Closed by the business; every figure was read from the frozen record, and nothing recomputes it.",
		operatorpages.BillingToneFrozen}
	billingRunning = billingWord{"Live — month running",
		"A live count, worked out afresh on each view: it moves until the month ends and the business closes it.",
		operatorpages.BillingToneRunning}
	billingUnclosed = billingWord{"Ended, not closed by the business",
		"This month is over and the business has not closed it, so its figures are still a live count, worked out afresh on each view.",
		operatorpages.BillingToneUnclosed}
	billingBeforeSignup = billingWord{"Before sign-up",
		"This month is before the business signed up; there is nothing to count for it.",
		operatorpages.BillingToneBeforeSignup}
	billingUnreadable = billingWord{"Unreadable",
		"This month's row could not be read as a billing month, so no figure is shown for it — not even a zero.",
		operatorpages.BillingToneUnreadable}
)

// billingRowOf is one month as the screen says it, and -- for a row it cannot read -- why,
// in a fixed phrase for the log. The read documents three shapes (db.TenantBillingMonth):
// FROZEN (after sign-up, ended, closed, a currency, every figure), LIVE (after sign-up, not
// closed, every figure, a first charged month; no currency -- billing.DefaultCurrency
// applies) and BEFORE SIGN-UP (not frozen, no figure at all). A row of any other shape, or one
// whose money MoneyFromNumeric refuses, is UNREADABLE: drawn with no figure, never as zero.
func billingRowOf(m db.TenantBillingMonth) (operatorpages.BillingRow, string) {
	unreadable := func(month, why string) (operatorpages.BillingRow, string) {
		w := billingUnreadable
		return operatorpages.BillingRow{Month: month, Label: w.label, Sentence: w.sentence, Tone: w.tone}, why
	}
	if !m.Month.Valid {
		return unreadable("A month with no date", "the month is not a date")
	}
	month := m.Month.Time.Format("January 2006")
	if !m.AfterSignup {
		if m.Frozen || m.EmployeeCount != nil || m.UnstampedEmployees != nil || m.Free != nil || m.UnitPrice.Valid ||
			m.AmountDue.Valid || m.Currency != nil || m.ClosedAt != nil {
			return unreadable(month, "a month before sign-up carries a figure")
		}
		w := billingBeforeSignup
		return operatorpages.BillingRow{Month: month, Label: w.label, Sentence: w.sentence, Tone: w.tone}, ""
	}
	if m.HasEnded == nil || m.EmployeeCount == nil || m.UnstampedEmployees == nil || m.Free == nil || m.Plan == nil ||
		m.Zone == nil || m.From == nil || m.To == nil {
		return unreadable(month, "a month after sign-up lacks a figure")
	}
	currency := billing.DefaultCurrency
	var w billingWord
	switch {
	case m.Frozen:
		if !*m.HasEnded || m.ClosedAt == nil || m.Currency == nil || m.FirstChargeableMonth.Valid {
			return unreadable(month, "a frozen month is not shaped as one")
		}
		currency, w = *m.Currency, billingFrozen
	default:
		if m.ClosedAt != nil || !m.FirstChargeableMonth.Valid {
			return unreadable(month, "a live month is not shaped as one")
		}
		if m.Currency != nil {
			currency = *m.Currency
		}
		w = billingRunning
		if *m.HasEnded {
			w = billingUnclosed
		}
	}
	price, err := moneyText(m.UnitPrice, currency)
	if err != nil {
		return unreadable(month, "the unit price is not an amount of money")
	}
	amount, err := moneyText(m.AmountDue, currency)
	if err != nil {
		return unreadable(month, "the amount is not an amount of money")
	}
	row := operatorpages.BillingRow{
		Month: month, Label: w.label, Sentence: w.sentence, Tone: w.tone, HasFigures: true,
		Plan:      *m.Plan,
		People:    strconv.Itoa(int(*m.EmployeeCount)),
		UnitPrice: price,
		Amount:    amount,
		Free:      *m.Free,
		Period:    localStamp(*m.From, *m.Zone, false) + " to " + localStamp(*m.To, *m.Zone, true),
	}
	if m.Frozen {
		row.ClosedAt = localStamp(*m.ClosedAt, *m.Zone, true)
	} else {
		row.FirstCharged = m.FirstChargeableMonth.Time.Format("January 2006")
	}
	if n := *m.UnstampedEmployees; n > 0 {
		row.Floor = strconv.Itoa(int(n))
	}
	return row, ""
}

// moneyText is an exact amount as the screen prints it: billing.MoneyFromNumeric -- the
// repository's one bridge from a numeric to money, which refuses NULL, NaN, an infinity and
// more than two decimal places rather than round -- then the symbol, the render layer's job
// (billing.Money.String carries none): the euro sign for EUR, and for a currency this screen
// has no symbol for, the code after the digits. A refused value is an error, which the caller
// draws as an unreadable month and never as zero.
func moneyText(n pgtype.Numeric, currency string) (string, error) {
	m, err := billing.MoneyFromNumeric(n, currency)
	if err != nil {
		return "", err
	}
	if m.Currency() == "EUR" {
		return "€" + m.String(), nil
	}
	return m.String() + " " + m.Currency(), nil
}

// localStamp is an instant in the month's own zone, to the minute (CLAUDE.md §6: the database
// holds UTC, the render layer turns it local), with the zone named when withZone is set. A
// zone this binary cannot load (it embeds tzdata, so a name the database accepted but Go does
// not) prints in UTC and says so.
func localStamp(t time.Time, zone string, withZone bool) string {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return t.UTC().Format("2006-01-02 15:04") + " UTC"
	}
	s := t.In(loc).Format("2006-01-02 15:04")
	if withZone {
		s += " " + zone
	}
	return s
}
