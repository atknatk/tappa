// Package operatorpages is the platform operator's screens (M10 OP-8; ADR 0020 §4):
// sign-in, the TOTP step, enrollment, the console's front page and its problem page,
// (OP-10) the legal texts screen, (OP-11) the tenant list and a tenant's overview, (OP-13) a
// tenant's plaques, (OP-14) the operator's own audit log, (OP-12) a tenant's billing months and
// (OP-16) a tenant's VAT number with its re-check, in the "TAPTIME OPERATOR" chrome.
//
// IT IS NOT web/templates/pages, ON PURPOSE. pages is what the customer product renders
// and internal/handler imports it. Measured with `go list` (non-test imports, the build
// being run): internal/handler/operator is the module package that imports this
// package; of the module, this package imports web/templates/layout -- the document head
// with the no-referrer, robots and single-stylesheet decisions (layout.documentHead) --
// and web/templates/components (the problem page's notice block)
// (internal/handler/operator's TestOperatorPages_ImportedOnlyByTheSurfaceAndSharingOnlyTheShell).
//
// skill tappa-brand governs the classes here: palette tokens, Space Grotesk for display,
// IBM Plex Mono for data, touch targets at least 44px (min-h-11), no dark theme. The chrome is set apart from the restaurant panel by WHERE the palette is
// used, not by a new colour -- see bar in chrome.templ.
package operatorpages

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/a-h/templ"
)

// TenantName is the name of the tenant a cross-tenant screen is showing (ADR 0020 §9:
// "tenant-ötesi her ekran girdiği tenant'ı başlıkta ADIYLA gösterir"; M9-08 kabul 2).
//
// ITS ZERO VALUE IS NOT A NAME. The fields are unexported, so outside this package a name
// is made with NewTenantName, which refuses an empty or blank one, or -- for a tenant
// whose stored name shows nothing (tenants.name has no CHECK against that) --
// UnnamedTenant, which names it by its id and refuses an empty one; TenantScreen refuses
// the zero value at render time. A screen rendered through TenantScreen carries the
// tenant's name in its banner and title -- TestTenantScreen_RefusesToRenderWithoutAName
// measures the refusals and a named render, TestTenantOverview_AnUnnamedTenantIsNamedByItsID
// the placeholder. Which pages show a tenant's data, and whether each renders through
// TenantScreen, is code review's; no test pins it. OP-11's overview (TenantOverview) is
// the first that does, OP-13's plaques (TenantPlaques) the second, OP-12's billing months
// (TenantBilling) the third and OP-16's VAT number (TenantVAT) the fourth; the tenant LIST
// shows many tenants and names none in a banner.
type TenantName struct {
	v string
	// byID marks the placeholder: v is the tenant's id, and the banner says "Unnamed
	// tenant" above it.
	byID bool
}

// ErrNoTenantName is NewTenantName's and TenantScreen's refusal.
var ErrNoTenantName = errors.New("operatorpages: a tenant screen needs the tenant's name")

// NewTenantName wraps a tenant's name. A name that is empty or white space alone is
// refused: it would render a heading that names nobody.
func NewTenantName(name string) (TenantName, error) {
	if strings.TrimSpace(name) == "" {
		return TenantName{}, ErrNoTenantName
	}
	return TenantName{v: name}, nil
}

// UnnamedTenant is the banner name of a tenant whose stored name shows nothing: the
// words "Unnamed tenant" and its id. An empty id is refused -- the placeholder would
// name nobody either.
func UnnamedTenant(id string) (TenantName, error) {
	if strings.TrimSpace(id) == "" {
		return TenantName{}, ErrNoTenantName
	}
	return TenantName{v: id, byID: true}, nil
}

// title is the name as the document title spells it.
func (n TenantName) title() string {
	if n.byID {
		return unnamedTenant + " " + n.v
	}
	return n.v
}

// unnamedTenant is the placeholder's words, on the banner, in the title and on a list
// row whose tenant shows no name.
const unnamedTenant = "Unnamed tenant"

// TenantScreen is the chrome for a screen that shows ONE tenant's data: the operator bar,
// then a banner naming the tenant, then the page. For the zero TenantName it returns
// ErrNoTenantName before writing a byte (it checks first), and
// internal/handler/operator's render renders into a buffer, so the client gets a plain
// 500 (TestTenantScreen_RefusesToRenderWithoutAName).
func TenantScreen(title string, tenant TenantName) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		if tenant.v == "" {
			return ErrNoTenantName
		}
		return tenantScreen(title, tenant).Render(ctx, w)
	})
}

// SignInView is the password step. Failed selects the sentence a refused password step
// shows; it names no cause (ADR 0020 §3: unknown address, wrong password, a pending and a
// disabled account answer alike), and the view has no field for one. The typed address
// is not echoed back (the eight arms of
// TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike answer the same bytes).
type SignInView struct {
	Failed bool
	// Expired says the TOTP step's challenge ran out and the password step restarts.
	Expired bool
}

// CodeView is the TOTP step.
type CodeView struct {
	Rejected bool // the code was not accepted
	Locked   bool // too many wrong codes: the database's lock (ADR 0020 §3)
}

// EnrollView is the enrollment screen (ADR 0020 §3).
//
// ON FIRST LOAD it carries the new TOTP secret twice -- as grouped base32 for typing
// and as the otpauth:// URI for an app that takes one -- and the sealed pending blob
// for the form. Both are plaintext credentials; the response that carries them is
// no-store with no-referrer, and the handler zeroes the secret once it is rendered.
//
// ON A RE-RENDER after a refused attempt (passwords that differ, a password the
// rule refuses, a code the window does not accept) there is NO secret -- the handler
// cannot open the blob -- and the form carries the same blob, account id and link
// token it was posted with, so the person retries with the app they already set up.
type EnrollView struct {
	AccountID string
	Key       string // grouped base32 (first load)
	URI       string // otpauth:// URI (first load)
	Blob      string
	// Token is the link's token ECHOED from a post (re-render). On first load it is
	// empty: the token travels in the link's fragment, which a browser does not put in a
	// request, and the page's script moves it into the form
	// (web/static/js/operator/enroll.js).
	Token string

	Mismatch     bool // the two passwords differ
	WeakPassword bool // the password rule refused it
	CodeRejected bool // the first code was not accepted
}

// LegalView is /operator/legal (M10 OP-10; ADR 0020 §7): one editor per document and
// the version list. Every value is text the handler formatted; templ escapes each one.
type LegalView struct {
	Docs     []LegalDoc
	Versions []LegalVersionRow
	// Limit is the number of versions the list asks the database for (newest first).
	// When Versions holds that many, the page says older versions are not listed.
	Limit int
}

// LegalDoc is one document's editor.
type LegalDoc struct {
	// Slug is the form's hidden value -- one of internal/domain/legal.Slugs.
	Slug string
	// Path is the document's public path ("/legal/privacy"). It is shown as text, not
	// linked: the public pages are on the customer host, and a link there would be an
	// absolute URL (TestOperatorScreens_EveryActionAndLinkIsAMountedRoute counts them).
	Path string
	// Published, PublishedAt (UTC) and Body are the snapshot the public page serves;
	// Body re-opens the editor, so an edit starts from the live text.
	Published   bool
	PublishedAt string
	Body        string
	// Behind says the snapshot is OLDER than the version the list shows live for this
	// document (a refresh after a publication failed, or another process published): the
	// editor opened on the public page's text, and LiveAt (UTC) is when the live version
	// was published.
	Behind bool
	LiveAt string
}

// LegalVersionRow is one row of the version list (op_read_legal_versions): no text,
// its length in bytes.
type LegalVersionRow struct {
	Path        string
	PublishedAt string
	Bytes       string
	// Publisher is the operator's name, or "tenant admin (legacy)" for a version the
	// M7-06 panel (or the owner's SQL) wrote -- ADR 0020 §7.
	Publisher string
	// Current marks the version the public page serves (the newest of its document).
	Current bool
}

// TenantsView is /operator/tenants (M10 OP-11): a page of the tenant list, newest first,
// and the search it is a page of. Every value is text the handler formatted; templ
// escapes each one.
type TenantsView struct {
	// Search is the term as it was searched -- trimmed -- or "" for every tenant. It is
	// shown back in the search box, in the "matching" line and in the pager's hidden
	// fields: the page is the answer to a POST and the term is its own (this page's links
	// and form actions carry no term; the forms post it).
	Search string
	// Page is the page number, from 1.
	Page int
	Rows []TenantRow
	// HasNext offers the next page: the handler sets it on a full page short of its
	// last.
	HasNext bool
}

// TenantRow is one tenant of the list: a link to its overview, and the facts that tell
// it from another (op_read_tenants' four columns).
type TenantRow struct {
	ID string
	// Path is the overview's path, /operator/tenants/<id>.
	Path string
	// Name is the tenant's name, or "" when it shows nothing -- the row then says
	// "Unnamed tenant" and the id beside it names the tenant.
	Name      string
	CreatedAt string // UTC
	Plan      string
}

// TenantOverviewView is /operator/tenants/{id} (M10 OP-11): one tenant's identity and
// four counts of what is live, rendered through TenantScreen.
type TenantOverviewView struct {
	Name         TenantName
	ID           string
	CreatedAt    string // UTC
	Plan         string
	BusinessType string
	// The counts, formatted: every location; employees, plaques and panel accounts
	// whose status is active.
	Locations, ActiveEmployees, ActivePlaques, ActiveAdmins string
	// PlaquesPath is the tenant's plaque screen, /operator/tenants/<id>/plaques (OP-13);
	// BillingPath its billing months, /operator/tenants/<id>/billing (OP-12); VATPath its VAT
	// number, /operator/tenants/<id>/vat (OP-16).
	PlaquesPath, BillingPath, VATPath string
}

// TenantPlaquesView is /operator/tenants/{id}/plaques (M10 OP-13): one tenant's plaques,
// read-only, rendered through TenantScreen. Every value is text the handler formatted;
// templ escapes each one.
type TenantPlaquesView struct {
	Name TenantName
	ID   string
	// OverviewPath is the tenant's overview, /operator/tenants/<id> -- the way back.
	OverviewPath string
	// Total is every plaque the tenant holds and Shown the rows below (the read returns
	// 200 at most); Truncated says Shown is fewer than Total. Noun is "plaque" or
	// "plaques", for Total.
	Total, Shown string
	Truncated    bool
	Noun         string
	Rows         []PlaqueRow
}

// PlaqueRow is one plaque. Label, Sentence and Tone are the handler's reading of the
// plaque's state; StoredStatus is set only for a state that reading does not know, and
// is the stored status quoted. Location is the location's name, or "" when the plaque has
// no location or the name shows nothing (LocationID then names it); the times are UTC,
// "" for a time the plaque does not have.
type PlaqueRow struct {
	UID                   string
	Label, Sentence       string
	Tone                  PlaqueTone
	StoredStatus          string
	Location, LocationID  string
	EncodedAt, CreatedAt  string
	RetiredAt, ReplacedBy string
	LastCtr               string
}

// PlaqueTone is the chip a plaque's state is drawn with -- the brand's fixed status
// mapping, where the word on the chip carries the meaning and the tone repeats it. Its
// zero value is PlaqueToneUnknown, so a row whose tone was never set draws the
// unrecognised chip, not an in-service one.
type PlaqueTone int

const (
	// PlaqueToneUnknown: a state the reading does not know -- the ink tone, which is no
	// status.
	PlaqueToneUnknown PlaqueTone = iota
	// PlaqueToneInService: on a wall with its encode recorded -- the tone of an active
	// lifecycle state.
	PlaqueToneInService
	// PlaqueToneStock: in stock and ready -- the neutral tone.
	PlaqueToneStock
	// PlaqueToneAttention: no encode recorded -- the warning tone.
	PlaqueToneAttention
	// PlaqueToneOut: retired or lost, taps on it rejected -- the rejection tone.
	PlaqueToneOut
)

// AuditLogView is /operator/audit (M10 OP-14): a page of the operator's own audit log,
// newest first, and the kind it is filtered to. It renders through the plain screen chrome:
// the log is every operator's and names many tenants, so no tenant banner heads it -- each
// row names its tenant (ADR 0020 §9, the OP-14 note). Every value is text the handler
// formatted; templ escapes each one.
type AuditLogView struct {
	// Kind is the filter as posted -- "" for every kind, or one audit kind -- carried by the
	// pager's hidden fields; Filter is its word (the docket's heading).
	Kind, Filter string
	// Options are the filter's choices: every kind, then each audit kind with its word.
	Options []AuditKindOption
	// Page is the page number, from 1; HasNext offers the next one (set on a full page short
	// of the last); LastPage says this is the last page the screen can show, and Reach how
	// many entries of one kind that is.
	Page     int
	HasNext  bool
	LastPage bool
	Reach    string
	Rows     []AuditRow
}

// AuditKindOption is one choice of the audit log's filter.
type AuditKindOption struct {
	Value, Label string
	Selected     bool
}

// AuditWord is a value of a closed set as the audit log says it: Text is the screen's word,
// or -- when Unknown -- the raw value, which the handler fills only when it has the shape of
// a closed set's member (it may be ""). The zero AuditWord is "nothing to say".
type AuditWord struct {
	Text    string
	Unknown bool
}

// Shown reports whether w has something to say.
func (w AuditWord) Shown() bool { return w.Text != "" || w.Unknown }

// factLabel is an audit fact's label followed by the space before its value, or "" for a
// fact with no label.
func factLabel(label string) string {
	if label == "" {
		return ""
	}
	return label + " "
}

// AuditRow is one entry of the audit log. At is UTC to the second. Kind is the row's kind.
// ActorID is the operator who acted ("" before sign-in) and Actor its name ("" when it shows
// nothing: the id names it); AccountID and Account the same for the account a pre-session
// row is about; TenantID the tenant a read named, Tenant its name ("" when it shows nothing)
// and TenantPath its overview ("" when no tenant has that id). Scope is a read's kind,
// ScopeHidden says a read's scope was not one the log returns. Page and PageSize are a
// read's page; Search, Filter, Legal and LegalBytes what the log reads out of the row's
// detail; DetailHidden says the detail had a shape the log does not show. Session is the
// first eight digits of the session's id ("" before sign-in). ByOwner says the row is one the
// platform owner's opadmin SQL wrote (M10 OP-14 D): no operator acted, so the row says who did
// instead of "Before sign-in".
type AuditRow struct {
	At                 string
	Kind               AuditWord
	ByOwner            bool
	ActorID, Actor     string
	AccountID, Account string
	TenantID, Tenant   string
	TenantPath         string
	Scope              AuditWord
	ScopeHidden        bool
	Page, PageSize     string
	Search, Filter     AuditWord
	Legal              AuditWord
	LegalBytes         string
	DetailHidden       bool
	Session            string
}

// TenantBillingView is /operator/tenants/{id}/billing (M10 OP-12): a page of one tenant's
// billing months, newest first, rendered through TenantScreen. Every value is text the
// handler formatted -- every amount already carries its currency (billing.Money's digits,
// the symbol added in the handler) -- and templ escapes each one. It has no field a person
// could travel in: the read returns counts, and the screen lists no one.
type TenantBillingView struct {
	Name TenantName
	ID   string
	// OverviewPath is the tenant's overview, /operator/tenants/<id> -- the way back;
	// BillingPath this screen's own path, which its pager posts to.
	OverviewPath, BillingPath string
	// Page is the page number, 1..LastPage; MonthsPerPage the months on each page.
	Page, LastPage, MonthsPerPage int
	Rows                          []BillingRow
}

// BillingRow is one month. Label, Sentence and Tone are the handler's reading of the month's
// state; HasFigures says the month carries figures (a frozen or a live month) and the fields
// after it are set -- a month before sign-up, or one the handler could not read, carries none
// and draws none. Free marks a month inside the founding offer's free window. FirstCharged is
// set on a live month only (a frozen row keeps the decision, not the rule) and ClosedAt on a
// frozen one only. Floor is the count of employee records that disagree with their own dates,
// "" when there are none. Period and ClosedAt are in the month's own zone, named in them.
type BillingRow struct {
	Month           string
	Label, Sentence string
	Tone            BillingTone
	HasFigures      bool
	Plan            string
	People          string
	UnitPrice       string
	Amount          string
	Free            bool
	FirstCharged    string
	ClosedAt        string
	Period          string
	Floor           string
}

// BillingTone is the chip a billing month's state is drawn with -- the brand's fixed status
// mapping, the word on the chip carrying the meaning and the tone repeating it. Its zero
// value is BillingToneUnreadable, so a row whose tone was never set draws the unreadable
// chip, not a frozen or a live one.
type BillingTone int

const (
	// BillingToneUnreadable: a row the handler could not read as a billing month -- the ink
	// tone, which is no status.
	BillingToneUnreadable BillingTone = iota
	// BillingToneFrozen: closed by the business, its figures read from the frozen record --
	// the settled tone.
	BillingToneFrozen
	// BillingToneRunning: the month is still running, a live count -- the neutral tone.
	BillingToneRunning
	// BillingToneUnclosed: the month has ended and the business has not closed it -- the
	// waiting tone (a fact for a person to look at, not a verdict).
	BillingToneUnclosed
	// BillingToneBeforeSignup: before the business signed up -- the neutral tone.
	BillingToneBeforeSignup
)

// TenantVATView is /operator/tenants/{id}/vat (M10 OP-16): one tenant's VAT number, the verdict
// on file in migration 00017's four states, and -- when the number is in the format VIES takes
// -- the form that asks VIES again, rendered through TenantScreen. Every value is text the
// handler formatted; templ escapes each one.
type TenantVATView struct {
	Name TenantName
	ID   string
	// OverviewPath is the tenant's overview, /operator/tenants/<id> -- the way back; VATPath
	// this screen's own path, which its form posts to.
	OverviewPath, VATPath string
	// Number is the VAT number on file, as stored. The form does not carry it: the server
	// sends VIES the number its own read returned.
	Number string
	// Label, Sentence and Tone are the handler's reading of the verdict on file -- its word,
	// the sentence beside it, the chip.
	Label, Sentence string
	Tone            VATTone
	// AskedAt is the time on the record (UTC), "" when it holds none. Answered says whether a
	// verdict stands beside it: then it is VIES's last ANSWER ("Last answer" -- an ask it did not
	// answer writes nothing, so the record cannot say when it was last asked); otherwise it is
	// an ask that got no answer ("Asked").
	AskedAt  string
	Answered bool
	// Askable offers the form: the number is in the format VIES accepts for its country. When
	// it is not, the page says so in the form's place.
	Askable bool
	// NoticeHeading and Notice are the answer to a POST that changed nothing -- VIES did not
	// answer, the number could not be sent, the session's VIES budget -- above the record; ""
	// for none.
	NoticeHeading, Notice string
}

// VATTone is the chip the verdict on file is drawn with -- the brand's fixed status mapping,
// the word on the chip carrying the meaning and the tone repeating it, as on the customer's
// account screen: a confirmation is the active tone, a refusal the rejection tone, no answer
// the waiting tone, never asked the neutral one. Its zero value is VATToneUnknown, so a view
// whose tone was never set draws the unrecognised chip, not one of the four.
type VATTone int

const (
	// VATToneUnknown: a tone the handler never set -- the ink tone, which is no status.
	VATToneUnknown VATTone = iota
	// VATToneNotChecked: no answer on file and no time of asking -- the neutral tone.
	VATToneNotChecked
	// VATToneNoAnswer: asked, and the register did not answer -- the waiting tone.
	VATToneNoAnswer
	// VATToneConfirmed: the register confirmed the number -- the active tone.
	VATToneConfirmed
	// VATToneNotFound: the register does not know the number -- the rejection tone.
	VATToneNotFound
)

// ProblemView is a refusal or a fault the operator surface answers with a page.
type ProblemView struct {
	Title   string
	Message string
	// Back is a same-origin path offered as the way on ("" for none). The links of the
	// pages internal/handler/operator's problemPages lists are held to mounted routes by
	// TestProblemPages_LinkOnlyToMountedRoutes.
	Back      string
	BackLabel string
	// SignedIn renders the sign-out control in the bar.
	SignedIn bool
}
