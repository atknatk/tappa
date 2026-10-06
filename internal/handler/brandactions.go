package handler

// brandactions.go -- the Account section's "Your brand" editor (M10 WL-7; ADR 0023 §2,
// §3; ADR 0024 §6): the editor's view, the two small writes (the accent, and taking the
// logo or the accent back), and what all three brand routes share -- their addresses,
// their outcome words, their budgets and their refusal row. The third write, the logo
// upload, reads a multipart body and lives in brandupload.go on its own, so the file
// that reads that body is a file nothing else is in (the syntax pin there reads it).
//
//	POST /admin/account/brand/logo     brandupload.go
//	POST /admin/account/brand/accent   brandAccentSave
//	POST /admin/account/brand/reset    brandReset  (what=logo | what=accent)
//
// All three are mounted by mountWriting, so each runs ProtectWriting: the Origin check
// BEFORE the resolver, then the identity, then the session budget.
//
// 🔴 OWNER ONLY, ON THE SAME PREDICATE AS THE DETAILS FORM (mayEditAccount; ADR 0024
// §6). The role is checked before anything is read from the body and before a budget is
// charged; a refused attempt is answered 303 to the section with the word
// not-permitted and leaves a tenant.brand_update_refused row in the trail, written in
// its own transaction (a.record) because nothing else is being written. The domain asks
// the same question again inside its own transaction (internal/domain/tenant
// mayEditBrand) and its refusal gets the same answer and the same row here.
//
// 🔴 THE PREVIEW SHOWS WHAT IS SAVED. The editor's view is built from the chrome's own
// brand read (PanelChrome.Brand), so the preview, the stripe and the policy's img-src
// all come from one value; a colour that was tried and refused is shown only by the
// colour input (pages/brandeditorview.go).
//
// THE CLAIM, IN THREE PARTS, IS WRITTEN ONCE FOR THE THREE ROUTES at the head of
// brandupload.go.

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// panelBrandWriter is the slice of internal/domain/tenant.Brands the three routes need,
// declared at the consumer (section 7). It is a field of its own on AdminAuth, apart
// from brands (the chrome's reader): a page view holds no value with a Save method on
// it (WL-6's reason for tenant.BrandReader).
type panelBrandWriter interface {
	SaveAccent(ctx context.Context, c tenant.BrandAccentCommand) error
	ClearAccent(ctx context.Context, c tenant.BrandClearCommand) error
	SaveLogo(ctx context.Context, c tenant.BrandLogoCommand) error
	ClearLogo(ctx context.Context, c tenant.BrandClearCommand) error
}

// The three routes, under the section's own address (accountHref is read from the
// section table), so the forms and the routes are the same strings at compile time.
var (
	brandLogoHref   = accountHref + "/brand/logo"
	brandAccentHref = accountHref + "/brand/accent"
	brandResetHref  = accountHref + "/brand/reset"
)

// ActionBrandUpdateRefused is the trail's name for a brand write that was refused before
// it reached the database (ADR 0024 §6). The success row, tenant.brand_updated, is the
// domain's and shares the write's transaction.
const ActionBrandUpdateRefused = "tenant.brand_update_refused"

// The "field" a refusal row names: which of the three acts was attempted.
const (
	brandFieldLogo   = "logo"
	brandFieldAccent = "accent"
	brandFieldReset  = "reset"
)

// The reasons a refusal row carries. Fixed sentences: nothing posted is recorded
// (refusedAccountDetail's rule), so a refused request cannot append text of its choosing
// to an append-only table.
const (
	brandRefusedRole   = "changing this business's brand is reserved for an owner"
	brandRefusedDomain = "the brand writer found this admin is not an active owner of this business"
	brandRefusedBudget = "too many brand changes for this business in ten minutes"
)

// refusedBrandDetail is the refusal row's payload. Explicit keys, no omitempty
// (refusedAccountDetail's lesson): an absent key cannot be told from an empty value.
type refusedBrandDetail struct {
	Outcome      string `json:"outcome"`
	Reason       string `json:"reason"`
	Field        string `json:"field"`
	Role         string `json:"role"`
	RequiredRole string `json:"required_role"`
}

// refuseBrand records one refused brand write, in its own transaction, and logs the
// refusal by its class. A failed audit write does not change the answer (a.record logs
// it): the refusal already happened.
func (a *AdminAuth) refuseBrand(r *http.Request, id httpx.AdminIdentity, field, reason string) {
	a.log.Warn("panel brand change refused", "tenant_id", id.TenantID(),
		"actor_id", id.Admin.AdminUserID, "role", id.Admin.Role, "field", field, "reason", reason)
	a.record(r.Context(), audit.Event{
		TenantID: id.TenantID(),
		ActorID:  ptr(id.Admin.AdminUserID),
		Action:   ActionBrandUpdateRefused,
		// The business itself, as the success row's target (tenant.Brands.write).
		Target: id.TenantID().String(),
		Detail: refusedBrandDetail{
			Outcome:      "refused",
			Reason:       reason,
			Field:        field,
			Role:         id.Admin.Role,
			RequiredRole: adminRoleOwner,
		},
	})
}

// The brand budgets, keyed on the BUSINESS (its tenant id), not the session: an owner
// with two sessions, or two owners, share one.
//
//	upload  brandUploadLimit logo uploads per brandUploadPeriod, charged per ATTEMPT,
//	        before the body is read (ADR 0024 §6, WL-3's correction): each attempt can
//	        cost a 1 MiB read and one full decode, so charging only the ones that
//	        succeed would make every refusal that reaches the decoder free. Ten
//	        attempts bound one business to 10 MiB read, ten decodes (the worst measured
//	        about 1.7 s each, ADR 0024's WL-3 note) and at most 2.5 MiB of stored logo
//	        per window. An owner trying a few files in a row stays well under it.
//	write   brandWriteLimit accent saves and removals per brandWritePeriod. Each is an
//	        UPDATE and a permanent tenant.brand_updated row (saving the same colour
//	        again is recorded too -- WL-4's decision), so the panel's 300 requests per
//	        session would be 300 rows per session; thirty is far more than a person
//	        choosing a colour presses Save.
//
// Past either, the answer is 429 and nothing is written. The refusal row is written
// once per window and business -- on the request that crosses the line (FirstOverLimit,
// the mechanism M5-02 measured) -- so the budget also bounds its own trail.
const (
	brandUploadLimit  = 10
	brandUploadPeriod = 10 * time.Minute
	brandWriteLimit   = 30
	brandWritePeriod  = 10 * time.Minute
)

// brandFormMaxBody bounds the two small forms' bodies: a colour, a typed code or one
// word. A bare ParseForm would inherit net/http's 10 MB (maxAccountBody's argument).
const brandFormMaxBody = 2 << 10

// problemBrandTooMany is the 429 page for a business past its brand budget.
var problemBrandTooMany = pages.ProblemView{
	Title:   "Too many brand changes",
	Message: "This business has changed its logo or colour many times in the last few minutes, so this change was not made. Nothing was changed.",
	Hint:    "Wait ten minutes and try again.",
}

// chargeBrand charges one attempt to the business's budget and reports whether it is
// still within it. Past it, the refusal row is written on the crossing request only.
func (a *AdminAuth) chargeBrand(r *http.Request, id httpx.AdminIdentity, l *limiter, limit int, field string) bool {
	n := l.Charge(id.TenantID().String())
	if n <= limit {
		return true
	}
	if l.FirstOverLimit(n) {
		a.refuseBrand(r, id, field, brandRefusedBudget)
	}
	return false
}

// --- the outcome words ---------------------------------------------------------------

// brandParam is the query parameter the three routes send the section back with. It is
// separate from the details form's done/problem pair so a brand notice is drawn inside
// the brand editor, beside what it is about.
const brandParam = "brand"

// brandOutcomes is the CLOSED vocabulary of that parameter: a word that is not a key
// draws nothing (oneOfWords' rule), so nothing a caller puts in the URL reaches the
// page. A refusal or a warning says what happened and what to do; a change says where
// it shows. None blames (skill tappa-brand, voice).
var brandOutcomes = map[string]pages.BrandOutcome{
	"logo-saved": {Tone: components.ToneOK, Heading: "Logo saved",
		Sentence: "Your staff see it from their next tap, and it is at the top of this panel."},
	"logo-saved-light": {Tone: components.ToneWarn, Heading: "Logo saved, but it may be hard to see",
		Sentence: "Most of it is very light, and the tap screen behind it is light too. If your staff cannot make it out, upload a darker version."},
	"logo-removed": {Tone: components.ToneOK, Heading: "Logo removed",
		Sentence: "The tap screen and the screen after a tap show the Taptime name again."},
	"accent-saved": {Tone: components.ToneOK, Heading: "Colour saved",
		Sentence: "The tap button is this colour from your staff's next tap."},
	"accent-removed": {Tone: components.ToneOK, Heading: "Colour removed",
		Sentence: "The tap button is Taptime green again."},
	"not-permitted": {Tone: components.ToneAlert, Heading: "Not changed",
		Sentence: "Only an owner of this business can change its logo and colour, so nothing was changed. Ask an owner to make the change."},
	"unreadable": {Tone: components.ToneAlert, Heading: "Not changed",
		Sentence: "That could not be read, so nothing was changed. Choose the file or the colour again and send it once more."},
	"unavailable": {Tone: components.ToneAlert, Heading: "Not changed",
		Sentence: "That could not be done just now. Nothing was changed; please try again."},
	"logo-missing": {Tone: components.ToneAlert, Heading: "No logo uploaded",
		Sentence: "No file came with the form. Choose a PNG or JPEG file and upload it."},
	"logo-parts": {Tone: components.ToneAlert, Heading: "No logo uploaded",
		Sentence: "The upload carried something other than one logo file, so nothing was changed. Choose one file and upload it."},
	"logo-format": {Tone: components.ToneAlert, Heading: "No logo uploaded",
		Sentence: "That file is not a PNG or a JPEG, so nothing was changed. Save your logo as PNG or JPEG and upload that."},
	"logo-too-large": {Tone: components.ToneAlert, Heading: "No logo uploaded",
		Sentence: "That file is over 512 KB, so nothing was changed. Save a smaller version of your logo and upload that."},
	"logo-dimensions": {Tone: components.ToneAlert, Heading: "No logo uploaded",
		Sentence: "That image is too small or too large, so nothing was changed. Use one between 16 and 2048 pixels on each side."},
	"logo-scans": {Tone: components.ToneAlert, Heading: "No logo uploaded",
		Sentence: "That JPEG is saved in a way that takes too long to read, so nothing was changed. Save it again as a standard JPEG and upload that."},
	"logo-corrupt": {Tone: components.ToneAlert, Heading: "No logo uploaded",
		Sentence: "That image could not be read, so nothing was changed. Open it, save it again, and upload the new file."},
	"logo-simplify": {Tone: components.ToneAlert, Heading: "No logo uploaded",
		Sentence: "Even made smaller, that image is over 256 KB, so nothing was changed. A simpler logo works, and a photo-like one fits as a JPEG."},
	"logo-busy": {Tone: components.ToneAlert, Heading: "No logo uploaded",
		Sentence: "Another logo was being processed at the same moment, so nothing was changed. Try again in a moment."},
	"logo-slow": {Tone: components.ToneAlert, Heading: "No logo uploaded",
		Sentence: "The upload took too long to arrive, so nothing was changed. Try again, on a faster connection if you can."},
}

// brandReturn is the section's address with one outcome word, at the editor's fragment.
// A word outside brandOutcomes gives the bare section address: server-built from a
// closed list, so it cannot become an open redirect (accountReturn's construction).
func brandReturn(word string) string {
	if _, ok := brandOutcomes[word]; !ok {
		return accountHref + "#brand"
	}
	q := url.Values{}
	q.Set(brandParam, word)
	return accountHref + "?" + q.Encode() + "#brand"
}

// brandOutcomeOf is the notice for the section's brand parameter, or the zero value.
func brandOutcomeOf(r *http.Request) pages.BrandOutcome {
	return brandOutcomes[strings.TrimSpace(r.URL.Query().Get(brandParam))]
}

// --- the editor's view ---------------------------------------------------------------

// brandEditorOf builds the editor from the chrome the page is drawn in (accountView).
//
// 🔴 ONE READ, ONE ANSWER. Everything the editor shows about what is saved comes from
// c.Brand, the chrome's brand read. The preview is not built here at all: the template
// draws it from the chrome itself (pages.PanelBrand.TapHeader -- the preview logo,
// drawn exactly where the chrome draws its own, so the page's img-src is the chrome's,
// and the theme the shell links, which the preview's button reads), so the editor's
// fields, which renderBrandAgain rewrites, cannot reach it. From the same read come the
// picker's starting colour and which "take it back" buttons are offered. A business
// whose brand could not be read, or whose saved accent the gate refuses today, is drawn
// as one with no brand -- the chrome's §4.6 fall-back (panelbrand.go), the same bytes as
// a business with none (TestPanelBrand_UnbrandedSectionsAreTheWordmarkChrome).
func brandEditorOf(id httpx.AdminIdentity, c pages.PanelChrome) pages.BrandEditor {
	b := c.Brand
	picker := brand.DefaultAccent().Hex()
	if b.Theme.Linked() {
		picker = b.Theme.Hex()
	}
	e := pages.BrandEditor{
		CanEdit:        mayEditAccount(id),
		HasLogo:        b.DrawsLogo(),
		HasAccent:      b.Theme.Linked(),
		Picker:         "#" + picker,
		LogoPostHref:   brandLogoHref,
		AccentPostHref: brandAccentHref,
		ResetPostHref:  brandResetHref,
		LogoMaxKB:      brand.LogoMaxInputBytes >> 10,
	}
	if !e.CanEdit {
		e.WhyNotEdit = "The logo and the colour are changed by an owner of the business rather " +
			"than by a manager. The picture shows what your staff see now."
	}
	return e
}

// brandRetry is what a refused accent save puts back on the form: the text that was
// typed, the colour that was tried (for the picker's own swatch) and the message with
// the code it ends on, with the suggestion when the colour was illegible.
type brandRetry struct {
	typed   string
	tried   *brand.Color
	message string
	code    string
	suggest *brand.Color
}

// renderBrandAgain re-renders the section with the accent form answering a refused
// save. It re-reads the account row as renderAccountFormAgain does (the facts around the
// form are not in the submission), and only the editor's form fields take the retry;
// the preview is drawn from the chrome, which the retry does not touch. 200, the precedent's answer: the response IS the form.
func (a *AdminAuth) renderBrandAgain(w http.ResponseWriter, r *http.Request, id httpx.AdminIdentity, retry brandRetry) {
	s, err := a.accounts.Settings(r.Context(), id.TenantID())
	if err != nil {
		a.log.Error("panel: could not re-read the business account after a refused brand save", "err", err)
		a.renderProblem(w, r, http.StatusInternalServerError, problemAccountUnreadable)
		return
	}
	v := a.accountView(r, id, s)
	v.Brand.Typed = retry.typed
	if retry.tried != nil {
		v.Brand.Picker = "#" + retry.tried.Hex()
	}
	v.Brand.AccentError = retry.message
	v.Brand.AccentErrorCode = retry.code
	if retry.suggest != nil {
		v.Brand.Suggest = "#" + retry.suggest.Hex()
		v.Brand.SuggestHex = retry.suggest.Hex()
	}
	a.renderPanel(w, r, http.StatusOK, v.PanelChrome, pages.AdminAccount(v))
}

// --- the accent ----------------------------------------------------------------------

// The accent form's two fields: the colour input, and the box for a typed code. A typed
// code wins over the picker (the help text says so): the picker always sends a value,
// so a non-empty box is the only way to tell that somebody typed one.
const (
	brandFieldPicker = "accent"
	brandFieldTyped  = "accent_hex"
)

// brandAccentSave saves the tap button's colour.
//
// The value is read at this boundary (section 7) with brand.NormalizeAccent -- one
// optional '#', upper or lower case, six hex digits, and surrounding white space
// trimmed first -- and asked the gate (brand.Check) BEFORE the domain is called. An
// illegible colour re-renders the form with brand.Suggest's darker version of the same
// colour; nothing is written. The domain asks the gate again (WL-4).
func (a *AdminAuth) brandAccentSave(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !mayEditAccount(id) {
		a.refuseBrand(r, id, brandFieldAccent, brandRefusedRole)
		a.redirect(w, brandReturn("not-permitted"))
		return
	}
	if !a.chargeBrand(r, id, a.brandWriteLimiter, brandWriteLimit, brandFieldAccent) {
		a.renderProblem(w, r, http.StatusTooManyRequests, problemBrandTooMany)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, brandFormMaxBody)
	if err := r.ParseForm(); err != nil {
		// The parse error is classified, never printed: net/url quotes the input.
		a.log.Warn("panel brand accent refused: the form could not be read", "tenant_id", id.TenantID())
		a.redirect(w, brandReturn("unreadable"))
		return
	}
	typed := strings.TrimSpace(r.PostFormValue(brandFieldTyped))
	value := typed
	if value == "" {
		value = strings.TrimSpace(r.PostFormValue(brandFieldPicker))
	}
	c, err := brand.NormalizeAccent(value)
	if err != nil {
		a.renderBrandAgain(w, r, id, brandRetry{typed: typed,
			message: "That is not a colour code, so nothing was saved. Use six hex digits, like",
			code:    brand.DefaultAccent().Hex()})
		return
	}
	if _, err := brand.Check(c); err != nil {
		a.renderBrandAgain(w, r, id, illegibleRetry(typed, c))
		return
	}
	err = a.brandWriter.SaveAccent(r.Context(), tenant.BrandAccentCommand{
		TenantID: id.TenantID(), ActorID: id.Admin.AdminUserID, Accent: c,
	})
	switch {
	case err == nil:
		a.redirect(w, brandReturn("accent-saved"))
	case errors.Is(err, tenant.ErrBrandNotPermitted):
		a.refuseBrand(r, id, brandFieldAccent, brandRefusedDomain)
		a.redirect(w, brandReturn("not-permitted"))
	case errors.Is(err, brand.ErrAccentIllegible):
		// The boundary passed a colour the domain's gate refused: the two disagree.
		a.log.Error("panel brand accent: the domain refused a colour the boundary accepted", "tenant_id", id.TenantID())
		a.renderBrandAgain(w, r, id, illegibleRetry(typed, c))
	default:
		a.log.Error("panel brand accent: the save failed", "tenant_id", id.TenantID(), "err", err)
		a.redirect(w, brandReturn("unavailable"))
	}
}

// illegibleRetry is the form's answer to a colour the gate refuses: the colour that was
// tried stays in the picker, and the suggestion is brand.Suggest's -- the same hue,
// darkened until the word on the button reaches 4.5:1 (ADR 0023 §3; the orchestrator's
// WL-7 decision keeps the darkening direction).
func illegibleRetry(typed string, tried brand.Color) brandRetry {
	s := brand.Suggest(tried)
	return brandRetry{
		typed:   typed,
		tried:   &tried,
		message: "The word on the tap button cannot be read on that colour, so it was not saved. The same colour, darker, can:",
		code:    "#" + s.Hex(),
		suggest: &s,
	}
}

// --- taking it back ------------------------------------------------------------------

// The reset form's one field and its two values: which brand input to take back.
const brandFieldWhat = "what"

// brandReset takes the logo or the accent back to Taptime's. One input per request,
// so each act is one domain write: one transaction and one trail row (WL-4's
// ClearLogo / ClearAccent). A value that is neither is refused before anything is
// written.
func (a *AdminAuth) brandReset(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !mayEditAccount(id) {
		a.refuseBrand(r, id, brandFieldReset, brandRefusedRole)
		a.redirect(w, brandReturn("not-permitted"))
		return
	}
	if !a.chargeBrand(r, id, a.brandWriteLimiter, brandWriteLimit, brandFieldReset) {
		a.renderProblem(w, r, http.StatusTooManyRequests, problemBrandTooMany)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, brandFormMaxBody)
	if err := r.ParseForm(); err != nil {
		a.log.Warn("panel brand reset refused: the form could not be read", "tenant_id", id.TenantID())
		a.redirect(w, brandReturn("unreadable"))
		return
	}
	cmd := tenant.BrandClearCommand{TenantID: id.TenantID(), ActorID: id.Admin.AdminUserID}
	var (
		err  error
		done string
	)
	switch r.PostFormValue(brandFieldWhat) {
	case brandFieldLogo:
		err, done = a.brandWriter.ClearLogo(r.Context(), cmd), "logo-removed"
	case brandFieldAccent:
		err, done = a.brandWriter.ClearAccent(r.Context(), cmd), "accent-removed"
	default:
		a.log.Warn("panel brand reset refused: no known brand input named", "tenant_id", id.TenantID())
		a.redirect(w, brandReturn("unreadable"))
		return
	}
	switch {
	case err == nil:
		a.redirect(w, brandReturn(done))
	case errors.Is(err, tenant.ErrBrandNotPermitted):
		a.refuseBrand(r, id, brandFieldReset, brandRefusedDomain)
		a.redirect(w, brandReturn("not-permitted"))
	default:
		a.log.Error("panel brand reset: the write failed", "tenant_id", id.TenantID(), "err", err)
		a.redirect(w, brandReturn("unavailable"))
	}
}
