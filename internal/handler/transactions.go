package handler

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The TRANSACTIONS SECTION — M6-03. The first panel section with data behind it.
//
// 🔴 THE TENANT IS NEVER AN INPUT (§4.5). It comes from httpx.AdminOf(r), which
// the Protect chain resolved from a signed session cookie against the database.
// Nothing below reads a tenant from the query string, a header or a form, and the
// domain layer this calls has no way to be handed one from anywhere else. The
// filters a client DOES control (a location, a department, a person) are narrowing
// predicates INSIDE that tenant: a foreign id simply matches nothing, because the
// tenant predicate it is ANDed with is not one the client contributed to.
//
// 🔴 §4.7 — NO COORDINATE PASSES THROUGH THIS FILE. There is nothing to guard
// against by hand: ListPanelTransactions does not select gps_lat/gps_lng/source_ip,
// ledger.Record has no field for them, and components.DocketView has none either.
// This handler maps the second onto the third and could not carry one if it tried.
//
// 🔴 §4.3 — READ ONLY. Every route here is a GET. There is no approve, no edit and
// no delete: transactions is immutable, corrections are a new row (M6-08), and the
// flagged review queue writes to transaction_reviews (M6-04).
//
// §4.6 — A FAILED READ IS AN ERROR, NEVER AN EMPTY DAY. If the database does not
// answer, this renders a problem page. It must not fall through to a list with no
// records in it, because that sentence — "nobody tapped a plaque on this day" — is
// a claim about the world, and the product may not make one it has not measured.

// panelLedger is the slice of internal/domain/ledger this handler needs, declared
// here because CLAUDE.md §7 puts an interface on the consumer's side.
//
// 🔴 M6-05 ADDED Roster TO IT RATHER THAN DECLARING A THIRD INTERFACE, and the
// reason is the one M6-04 gave for splitting panelQueue off, applied honestly.
// panelQueue is separate because it names a DIFFERENT PACKAGE — the decision write
// lives in internal/domain/review, and keeping the two apart is what makes "no store
// call in internal/domain/ledger is not a SELECT" a fact grep can check. The roster
// is the same package and the same concrete *ledger.Reader, and NewAdminAuth already
// passes that one value twice; a third identical argument in the same position is
// the shape where somebody eventually passes the wrong one. See
// internal/handler/employees.go.
type panelLedger interface {
	Screen(ctx context.Context, tenantID uuid.UUID, f ledger.Filter) (ledger.Screen, error)
	Day(ctx context.Context, tenantID uuid.UUID, f ledger.Filter) (ledger.Page, error)
	Roster(ctx context.Context, tenantID uuid.UUID, f ledger.RosterFilter) (ledger.RosterScreen, error)
	// Hours is M6-07's, and it is here for the same reason Roster is: same package,
	// same concrete reader, and one interface is what stops a second identical
	// argument appearing in the constructor.
	Hours(ctx context.Context, tenantID uuid.UUID, f ledger.ReportFilter) (ledger.ReportScreen, error)
}

// docketFragmentPath is the URL the HTMX paging request goes to.
//
// IT IS NOT /admin/transactions, and the reason is written in pages.PanelSections:
// Transactions IS /admin, so a second URL under that name would be a second
// address for the same section. This one returns a LIST OF CARDS and nothing else,
// and it is named after what it returns.
const docketFragmentPath = "/admin/dockets"

// panelVerdicts and panelChannels are the closed vocabularies, and they are the
// ONLY copy.
//
// 🔴 THE DROPDOWN AND THE VALIDATOR READ THE SAME SLICE, which is the point. Two
// lists — one rendered, one checked — is the shape where a filter is offered that
// the validator silently drops, and the manager sees an unfiltered page and
// believes it. Here the option cannot be offered unless it is accepted, and cannot
// be accepted unless it is offered.
//
// The values themselves are migration 0005's CHECK vocabularies; the labels are
// the words §5 and the brand use for them.
var (
	panelVerdicts = []components.OptionView{
		{Value: "ok", Label: "Approved"},
		{Value: "flag", Label: "Flagged"},
		{Value: "reject", Label: "Rejected"},
		{Value: "ignored", Label: "Ignored"},
	}
	panelChannels = []components.OptionView{
		{Value: "nfc", Label: "Plaque tap (NFC)"},
		{Value: "qr", Label: "QR scan"},
		{Value: "manual", Label: "Entered by a manager"},
	}
)

// transactionsSection renders the whole section: chrome, filter bar, first page.
func (a *AdminAuth) transactionsSection(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f := parseTransactionFilter(r)

	screen, err := a.ledger.Screen(r.Context(), id.TenantID(), f)
	if err != nil {
		// §4.6: say the read failed. Do NOT fall through to an empty list.
		a.log.Error("panel: could not read the day's transactions", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	v := pages.TransactionsView{
		PanelChrome: a.chrome(r, pages.TabTransactions),
	}
	fillTransactionsView(&v, screen.Page, f)
	v.Filters.Locations = optionViews(screen.Options.Locations)
	v.Filters.Departments = optionViews(screen.Options.Departments)

	// THE SCRIPTED POLICY IS NAMED HERE, on the one page that loads a script.
	a.renderScripted(w, r, http.StatusOK, pages.AdminTransactions(v))
}

// transactionDockets answers the HTMX paging request with the cards alone.
//
// IT USES THE BASE POLICY, NOT THE SCRIPTED ONE. The fragment is inserted into a
// document that already exists, so the document's policy is what governs it; and
// if somebody navigates here directly it becomes a document of its own, which
// loads no script and therefore should not be permitted one.
func (a *AdminAuth) transactionDockets(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	f := parseTransactionFilter(r)

	page, err := a.ledger.Day(r.Context(), id.TenantID(), f)
	if err != nil {
		a.log.Error("panel: could not read the next page of transactions", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemPanelUnavailable)
		return
	}

	var v pages.TransactionsView
	fillTransactionsView(&v, page, f)
	a.render(w, r, http.StatusOK, pages.TransactionDockets(v))
}

// fillTransactionsView maps a domain page onto the view: the cards, the echoed
// filter state, and the two "next page" URLs.
//
// EVERY TIME IS CONVERTED HERE AND ONLY HERE (§6: the database is UTC and the
// render layer converts), against the zone the database supplied for this tenant.
func fillTransactionsView(v *pages.TransactionsView, page ledger.Page, f ledger.Filter) {
	zone := page.Zone
	if zone == nil {
		zone = time.UTC
	}

	v.Queried = page.Queried
	v.Dockets = make([]components.DocketView, 0, len(page.Records))
	for _, rec := range page.Records {
		v.Dockets = append(v.Dockets, docketView(rec, zone))
	}

	day := page.Day
	v.DayLabel = time.Date(day.Year, day.Month, day.Day, 0, 0, 0, 0, zone).Format("Monday 2 January 2006")
	v.ZoneLabel = zone.String()

	v.Filters = components.FilterBarView{
		Action:       transactionsHref,
		Date:         day.String(),
		LocationID:   uuidParam(f.LocationID),
		DepartmentID: uuidParam(f.DepartmentID),
		EmployeeName: f.EmployeeName,
		Verdict:      f.Verdict,
		Channel:      f.Channel,
		Verdicts:     panelVerdicts,
		Channels:     panelChannels,
		Narrowed:     narrowed(f),
	}

	if page.Next != nil {
		q := filterQuery(f, day)
		// RFC3339 WITH NANOSECONDS, because the cursor is an equality boundary and
		// a truncated timestamp is a WRONG one: two taps in the same second would
		// put the second of them either back on the page it just left or past the
		// start of the next. The id breaks the remaining ties.
		q.Set("after_at", page.Next.At.UTC().Format(time.RFC3339Nano))
		q.Set("after_id", page.Next.ID.String())
		v.MoreHref = transactionsHref + "?" + q.Encode()
		v.MoreFragmentHref = docketFragmentPath + "?" + q.Encode()
	}
}

// docketView maps one record onto one card.
func docketView(rec ledger.Record, zone *time.Location) components.DocketView {
	d := components.DocketView{
		Venue:      rec.LocationName,
		Department: rec.DepartmentName,
		Who:        rec.EmployeeName,
		Direction:  rec.Direction,
		At:         rec.OccurredAt.In(zone).Format("15:04:05"),
		IPSign:     sign(rec.IPMatch),
		GPSSign:    sign(rec.GPSMatch),
		TagUID:     rec.TagUID,
		Verdict:    rec.Verdict,
		Practice:   rec.Practice,
		Manual:     rec.Manual,
		Queued:     rec.Queued,
		// THE ENGINE'S VERDICT AND THE MANAGER'S DECISION TRAVEL SEPARATELY (M6-04).
		// Verdict above still says what §5 decided at the tap; this says whether a
		// human has since approved or rejected it, read through a JOIN on
		// transaction_reviews. Neither overwrites the other, here or anywhere else.
		Review: rec.Review,
		// AND THE SENTENCE THAT CAME WITH IT (user decision, 2026-08-06). It is
		// rendered so that the 500-character boundary in reviewNote stops being a
		// silent cut — see maxReviewNote.
		ReviewNote: rec.ReviewNote,
		Note:       rec.Note,
	}
	if rec.Trust != nil {
		d.Trust = strconv.Itoa(int(*rec.Trust))
	}
	if rec.Ctr != nil {
		d.Ctr = strconv.Itoa(int(*rec.Ctr))
	}
	return d
}

// sign renders one piece of evidence as a WORD.
//
// 🔴 THREE ANSWERS, NOT TWO, and it is §4.7's shape as much as §5's. What the
// panel shows is whether the evidence MATCHED, never the address or the
// coordinate it was computed from — and nil is a third state that migration 0005
// keeps on purpose: "this channel was never assessed on this". A manual entry has
// no network evidence to have failed, and rendering nil as "no" would accuse it of
// failing a check nobody ran.
func sign(b *bool) string {
	switch {
	case b == nil:
		return "n/a"
	case *b:
		return "yes"
	default:
		return "no"
	}
}

// parseTransactionFilter reads the six filters and the paging cursor from the
// query string.
//
// 🔴 EVERY VALUE IS VALIDATED HERE, AT THE BOUNDARY (§7), so the domain sees data
// that is already good. Anything unrecognised is DROPPED rather than refused, and
// that is a deliberate choice with a cost: a stale bookmark or a hand-edited URL
// shows the unfiltered day instead of an error page. It is safe because dropping
// can only WIDEN the result within the tenant, never escape it — and it is visible
// because the filter bar echoes the values that actually took effect, so a manager
// sees at once that their date was not applied.
func parseTransactionFilter(r *http.Request) ledger.Filter {
	q := r.URL.Query()
	var f ledger.Filter

	// A date that does not parse falls back to the tenant's today, which the
	// resolved day is then echoed as.
	if d, err := ledger.ParseDate(q.Get("date")); err == nil {
		f.Date = d
	}
	f.LocationID = optionalUUID(q.Get("location"))
	f.DepartmentID = optionalUUID(q.Get("department"))
	f.EmployeeName = employeeNameFilter(q.Get("employee"))
	f.Verdict = oneOf(q.Get("verdict"), panelVerdicts)
	f.Channel = oneOf(q.Get("channel"), panelChannels)

	// BOTH HALVES OF THE CURSOR OR NEITHER. Half a keyset position is not a
	// position: taking the timestamp alone would skip every tap that shares its
	// second with the last one shown.
	at, atErr := time.Parse(time.RFC3339Nano, q.Get("after_at"))
	id, idErr := uuid.Parse(q.Get("after_id"))
	if atErr == nil && idErr == nil {
		f.After = &ledger.Cursor{At: at, ID: id}
	}
	return f
}

// filterQuery rebuilds the query string for a link that keeps the current
// filters. The cursor is NOT included — the caller adds its own.
func filterQuery(f ledger.Filter, day ledger.Date) url.Values {
	q := url.Values{}
	// The RESOLVED day, not the requested one: a "show more" link that carried an
	// empty date would re-resolve "today" on the next request, and a reader paging
	// through yesterday's records at one minute to midnight would silently jump.
	q.Set("date", day.String())
	setIf(q, "location", uuidParam(f.LocationID))
	setIf(q, "department", uuidParam(f.DepartmentID))
	setIf(q, "employee", f.EmployeeName)
	setIf(q, "verdict", f.Verdict)
	setIf(q, "channel", f.Channel)
	return q
}

// narrowed reports whether anything beyond the day is filtering the list. The
// empty state uses it to pick which claim it is making.
func narrowed(f ledger.Filter) bool {
	return f.LocationID != nil || f.DepartmentID != nil || f.EmployeeName != "" ||
		f.Verdict != "" || f.Channel != ""
}

// maxEmployeeNameFilter bounds what the name box may send. Long enough for any
// real name, short enough that the query string cannot be used to push a large
// payload through a filter.
const maxEmployeeNameFilter = 100

// employeeNameFilter validates the typed name (§7: the boundary validates, the
// domain sees data that is already good).
//
// 🔴 IT DOES NOT ESCAPE, AND THAT IS DELIBERATE AFTER GETTING IT WRONG ONCE. The
// first version escaped ILIKE's wildcards here, and the escaped string was then
// what the filter bar echoed into the box and what every paging link carried --
// so typing "100%" gave back "100\%", and re-submitting escaped the escape. The
// escaping is an artifact of the OPERATOR, so it belongs with the query
// (internal/domain/ledger), and what travels through the handler, the view and the
// URL is exactly what the manager typed.
//
// Whitespace-only input is "no filter" rather than "match everyone whose name
// contains a space", which is what a bare trim would have produced.
func employeeNameFilter(raw string) string {
	// 🔴 NUL IS REMOVED BEFORE ANYTHING ELSE. PostgreSQL text cannot contain a zero
	// byte -- it answers "invalid byte sequence for encoding UTF8" and the request
	// becomes a 500. Measured: "abc\x00def" in the box produced one.
	s := strings.ReplaceAll(raw, "\x00", "")
	s = strings.TrimSpace(s)

	// 🔴 TRUNCATION IS BY RUNE, NOT BY BYTE, AND THAT WAS A REAL 500. The first
	// version did s[:maxEmployeeNameFilter], which cuts in the MIDDLE of a
	// multi-byte character: 99 ASCII characters followed by "é" is 101 bytes, and
	// slicing at 100 left a half rune -- invalid UTF-8, rejected by the driver,
	// 500 to the manager. Maltese names carry ċ ġ ħ ż and the brand ships a
	// latin-ext subset precisely because employee names are not ASCII, so this is
	// the ordinary case rather than an adversarial one.
	//
	// The limit counts CHARACTERS now, which is also what a person typing a name
	// would expect it to count.
	if utf8.RuneCountInString(s) > maxEmployeeNameFilter {
		runes := []rune(s)
		s = string(runes[:maxEmployeeNameFilter])
	}
	// A cut can still leave a trailing space; and any invalid sequence the client
	// sent for its own reasons is dropped rather than forwarded.
	s = strings.TrimSpace(strings.ToValidUTF8(s, ""))
	return s
}

// optionViews adapts the domain's options to the component's.
func optionViews(in []ledger.Option) []components.OptionView {
	out := make([]components.OptionView, 0, len(in))
	for _, o := range in {
		out = append(out, components.OptionView{Value: o.ID.String(), Label: o.Label, Group: o.Group})
	}
	return out
}

// optionalUUID parses an id filter. Anything unparseable is "no filter".
func optionalUUID(s string) *uuid.UUID {
	if s == "" {
		return nil
	}
	id, err := uuid.Parse(s)
	if err != nil {
		return nil
	}
	return &id
}

// oneOf accepts s only if it is one of the offered options — the same slice the
// dropdown renders from.
func oneOf(s string, options []components.OptionView) string {
	for _, o := range options {
		if o.Value == s {
			return s
		}
	}
	return ""
}

func uuidParam(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}

func setIf(q url.Values, key, value string) {
	if value != "" {
		q.Set(key, value)
	}
}
