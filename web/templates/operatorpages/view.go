// Package operatorpages is the platform operator's screens (M10 OP-8; ADR 0020 §4):
// sign-in, the TOTP step, enrollment, the console's front page and its problem page,
// and (OP-10) the legal texts screen, in the "TAPTIME OPERATOR" chrome.
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
// ITS ZERO VALUE IS NOT A NAME. The field is unexported, so outside this package a name
// is made with NewTenantName, which refuses an empty or blank one; TenantScreen refuses
// the zero value at render time. A screen rendered through TenantScreen carries the
// tenant's name in its banner and title -- TestTenantScreen_RefusesToRenderWithoutAName
// measures both refusals and a named render. Which pages show a tenant's data, and
// whether each renders through TenantScreen, is code review's (OP-11); no test pins it.
//
// No tenant screen exists in OP-8 (the first ones are OP-11's); the slot is here so
// the first one is born inside it.
type TenantName struct{ v string }

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
		return tenantScreen(title, tenant.v).Render(ctx, w)
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

// ProblemView is a refusal or a fault the operator surface answers with a page.
type ProblemView struct {
	Title   string
	Message string
	// Back is a same-origin path offered as the way on ("" for none). The links of the
	// ten pages internal/handler/operator's problemPages lists are held to mounted routes
	// by TestProblemPages_LinkOnlyToMountedRoutes.
	Back      string
	BackLabel string
	// SignedIn renders the sign-out control in the bar.
	SignedIn bool
}
