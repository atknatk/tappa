package handler

// brandactions_test.go -- the Account section's "Your brand" editor (M10 WL-7): the
// two small writes (the accent, taking a logo or an accent back), what the three brand
// routes share (the chain, the owner gate, the refusal row, the outcome words, the
// budget), and the editor's view -- the preview of the tap screen above all.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/web/templates/layout"
	"github.com/atknatk/tappa/web/templates/pages"
)

// brandPost posts form to path through b, as the editor's forms do.
func brandPost(b *browser, path string, form url.Values) *http.Response {
	return b.do(http.MethodPost, path, form).Result()
}

// --- the chain and the gate ----------------------------------------------------------

// TestBrandRoutes_CrossOriginIsRefusedBeforeTheResolver: a POST to each of the three
// routes from another origin (an Origin header naming it, and no Origin with
// Sec-Fetch-Site cross-site) is answered 303 to /admin by ProtectWriting's Origin check,
// with the session resolver asked 0 times, the writer 0 times and the decoder 0 times.
func TestBrandRoutes_CrossOriginIsRefusedBeforeTheResolver(t *testing.T) {
	for _, route := range []string{brandLogoHref, brandAccentHref, brandResetHref} {
		for _, shape := range []string{"origin", "sec-fetch-site"} {
			p := newBrandPanel(t, "owner", panelTestTenant)
			req, _ := http.NewRequest(http.MethodPost, route, strings.NewReader("accent=%23DA291C&what=logo"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			if shape == "origin" {
				req.Header.Set("Origin", "https://evil.example")
			} else {
				req.Header.Set("Sec-Fetch-Site", "cross-site")
			}
			req.AddCookie(panelCookie())
			rec := httptest.NewRecorder()
			p.router.ServeHTTP(rec, req)
			if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
				t.Errorf("%s (%s): %d %q, want 303 /admin", route, shape, rec.Code, rec.Header().Get("Location"))
			}
			if n := p.admins.verifiedCount(); n != 0 {
				t.Errorf("%s (%s): the resolver was asked %d times before the Origin check", route, shape, n)
			}
			if p.writer.writes() != 0 || p.gate.count() != 0 || p.trail.total() != 0 {
				t.Errorf("%s (%s): %d writes, %d decodes, %d trail rows", route, shape, p.writer.writes(), p.gate.count(), p.trail.total())
			}
		}
	}
}

// TestBrandRoutes_AManagerIsRefusedRecordedAndChangesNothing: a manager's POST to each
// route is answered 303 to the editor with not-permitted; each leaves exactly one
// tenant.brand_update_refused row whose detail has exactly the five keys (outcome,
// reason, field, role, required_role) with the role reason, the field and the roles; the
// writer and the decoder are reached 0 times. The upload's body is not even sent: the
// answer comes first.
func TestBrandRoutes_AManagerIsRefusedRecordedAndChangesNothing(t *testing.T) {
	p := newBrandPanel(t, "manager", panelTestTenant)
	b := p.browser(t)
	for _, tc := range []struct {
		route, field string
		form         url.Values
	}{
		{brandAccentHref, brandFieldAccent, url.Values{"accent": {"#DA291C"}}},
		{brandResetHref, brandFieldReset, url.Values{"what": {"logo"}}},
	} {
		res := brandPost(b, tc.route, tc.form)
		if got := outcomeOf(res); got != "not-permitted" {
			t.Errorf("%s: %d %q, want not-permitted", tc.route, res.StatusCode, res.Header.Get("Location"))
		}
	}
	body, ct := oneLogo(t, inkLogo(t))
	conn := rawUpload(t, p.server(t), ct, len(body), nil)
	res, err := rawAnswer(t, conn, 5e9)
	conn.Close()
	if err != nil || outcomeOf(res) != "not-permitted" {
		t.Fatalf("a manager's upload without its body: %v %v, want 303 not-permitted", res, err)
	}
	ev := p.trail.eventsSnapshot()
	if len(ev) != 3 {
		t.Fatalf("%d trail rows for three refused POSTs, want 3", len(ev))
	}
	for i, field := range []string{brandFieldAccent, brandFieldReset, brandFieldLogo} {
		e := ev[i]
		if e.Action != ActionBrandUpdateRefused || e.TenantID != panelTestTenant || e.ActorID == nil ||
			*e.ActorID != panelTestAdmin || e.Target != panelTestTenant.String() {
			t.Errorf("row %d: %+v", i, e)
		}
		raw, _ := json.Marshal(e.Detail)
		var keys map[string]string
		if err := json.Unmarshal(raw, &keys); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{"outcome": "refused", "reason": brandRefusedRole, "field": field,
			"role": "manager", "required_role": "owner"}
		if !mapsEqual(keys, want) {
			t.Errorf("row %d detail %v, want %v", i, keys, want)
		}
	}
	if p.writer.writes() != 0 || p.gate.count() != 0 {
		t.Errorf("%d writes, %d decodes; want 0 and 0", p.writer.writes(), p.gate.count())
	}
}

// roleSwitch resolves every session of p to the business's owner or to one of its
// managers, switchable between requests: the two people of ONE business behind ONE
// AdminAuth, so they share its budgets as they do in production.
func roleSwitch(p *brandPanel) func(role string) {
	var mu sync.Mutex
	current := "owner"
	manager := uuid.NewSHA1(uuid.Nil, []byte("wl7 manager"))
	p.admins.verify = func() (adminauth.Resolved, error) {
		mu.Lock()
		role := current
		mu.Unlock()
		r := adminauth.Resolved{SessionID: panelTestSession, TenantID: panelTestTenant,
			AdminUserID: panelTestAdmin, Role: role, FullName: "Maria Borg"}
		if role != "owner" {
			r.SessionID, r.AdminUserID = uuid.NewSHA1(uuid.Nil, manager[:]), manager
		}
		return r, nil
	}
	return func(role string) {
		mu.Lock()
		current = role
		mu.Unlock()
	}
}

// TestBrandRoutes_AManagersRefusalsCostTheOwnersBudgetsNothing: through one AdminAuth,
// a manager of the business posts to the three routes (each refused not-permitted, one
// trail row each); then the owner uploads ten logos and makes thirty accent changes --
// every one answered as itself, none 429 -- and only the eleventh upload and the
// thirty-first change are refused 429. The owner check is step 1 of each route,
// before its budget is charged: a manager cannot spend the owner's budget.
func TestBrandRoutes_AManagersRefusalsCostTheOwnersBudgetsNothing(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	as := roleSwitch(p)
	s := p.server(t)
	b := p.browser(t)
	logo, ct := oneLogo(t, inkLogo(t))
	as("manager")
	if res, _ := upload(t, s, logo, ct); outcomeOf(res) != "not-permitted" {
		t.Fatalf("the manager's upload: %d %q", res.StatusCode, res.Header.Get("Location"))
	}
	for _, tc := range []struct {
		route string
		form  url.Values
	}{
		{brandAccentHref, url.Values{"accent": {"#DA291C"}}},
		{brandResetHref, url.Values{"what": {"accent"}}},
	} {
		if res := brandPost(b, tc.route, tc.form); outcomeOf(res) != "not-permitted" {
			t.Fatalf("the manager's POST %s: %d %q", tc.route, res.StatusCode, res.Header.Get("Location"))
		}
	}
	if n := p.trail.count(ActionBrandUpdateRefused); n != 3 {
		t.Fatalf("PREMISE: %d refusal rows for the manager's three POSTs, want 3", n)
	}
	as("owner")
	for i := 1; i <= brandUploadLimit; i++ {
		if res, _ := upload(t, s, logo, ct); outcomeOf(res) != "logo-saved" {
			t.Fatalf("the owner's upload %d of %d: %d %q", i, brandUploadLimit, res.StatusCode, res.Header.Get("Location"))
		}
	}
	for i := 1; i <= brandWriteLimit; i++ {
		route, form, want := brandAccentHref, url.Values{"accent": {"#DA291C"}}, "accent-saved"
		if i%2 == 0 {
			route, form, want = brandResetHref, url.Values{"what": {"accent"}}, "accent-removed"
		}
		if res := brandPost(b, route, form); outcomeOf(res) != want {
			t.Fatalf("the owner's change %d of %d: %d %q", i, brandWriteLimit, res.StatusCode, res.Header.Get("Location"))
		}
	}
	// The budgets are live: the next of each is refused.
	if res, _ := upload(t, s, logo, ct); res.StatusCode != http.StatusTooManyRequests {
		t.Errorf("upload %d: %d, want 429", brandUploadLimit+1, res.StatusCode)
	}
	if res := brandPost(b, brandAccentHref, url.Values{"accent": {"#DA291C"}}); res.StatusCode != http.StatusTooManyRequests {
		t.Errorf("change %d: %d, want 429", brandWriteLimit+1, res.StatusCode)
	}
	if n := p.trail.count(ActionBrandUpdateRefused); n != 5 {
		t.Errorf("%d refusal rows, want 5 (the manager's three, one per crossed budget)", n)
	}
}

func mapsEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// --- the accent ----------------------------------------------------------------------

// TestBrandAccent_TheBoundaryReadsWhatTheColourInputSends drives the accent form with
// what a colour input sends (#rrggbb, lower case), what a person types (with or without
// '#', either case, with white space around it), and what neither should: each legible
// spelling is saved as the one colour, a typed code wins over the picker, and every other
// string -- a short code, a long one, a space inside, two '#', "0x", Unicode digits and
// letters -- is answered with the form and the "not a colour code" message, unsaved.
func TestBrandAccent_TheBoundaryReadsWhatTheColourInputSends(t *testing.T) {
	red := brand.Color{R: 0xDA, G: 0x29, B: 0x1C}
	for _, tc := range []struct {
		name          string
		picker, typed string
		saved         bool
	}{
		{"the colour input's own value", "#da291c", "", true},
		{"typed with #, upper case", "#000000", "#DA291C", true},
		{"typed bare, lower case", "#000000", "da291c", true},
		{"typed with spaces around", "#000000", "  DA291C\t", true},
		{"a typed code wins over the picker", "#ffffff", "DA291C", true},
		{"a short code", "#000000", "DA291", false},
		{"a long code", "#000000", "DA291CC", false},
		{"a space inside", "#000000", "DA 291C", false},
		{"two #", "#000000", "##DA291C", false},
		{"0x", "#000000", "0xDA291C", false},
		{"full-width digits", "#000000", "ＤＡ２９１Ｃ", false},
		{"Arabic-Indic digits", "#000000", "DA٢٩١C", false},
		{"a colour name", "#000000", "red", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newBrandPanel(t, "owner", panelTestTenant)
			res := brandPost(p.browser(t), brandAccentHref, url.Values{"accent": {tc.picker}, "accent_hex": {tc.typed}})
			saved := p.writer.savedAccents()
			if tc.saved {
				if outcomeOf(res) != "accent-saved" || len(saved) != 1 || saved[0].Accent != red ||
					saved[0].TenantID != panelTestTenant || saved[0].ActorID != panelTestAdmin {
					t.Fatalf("%d %q, saved %+v; want accent-saved with DA291C for the session", res.StatusCode, res.Header.Get("Location"), saved)
				}
				return
			}
			body, _ := io.ReadAll(res.Body)
			if res.StatusCode != http.StatusOK || len(saved) != 0 ||
				!strings.Contains(string(body), htmlText("That is not a colour code, so nothing was saved. Use six hex digits, like")+
					` <span class="font-mono">1F5C41</span>.</p>`) {
				t.Fatalf("%d, %d saves; want the form back with the message (its example code in mono) and nothing saved",
					res.StatusCode, len(saved))
			}
		})
	}
}

// TestBrandAccent_AnIllegibleColourComesBackWithItsSuggestion: for four colours the gate
// refuses (ADR 0023 §3's two, and two from the band's two sides), the form comes back 200
// with nothing saved: the colour that was tried in the picker, the message naming
// brand.Suggest's colour, and a separate form whose one value is that colour; posting it
// saves exactly Suggest's colour.
func TestBrandAccent_AnIllegibleColourComesBackWithItsSuggestion(t *testing.T) {
	for _, hex := range []string{"808080", "E0457B", "1E93A0", "7D5BEC"} {
		t.Run(hex, func(t *testing.T) {
			c, err := brand.ParseAccent(hex)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := brand.Check(c); err == nil {
				t.Fatalf("PREMISE: %s passes the gate", hex)
			}
			s := brand.Suggest(c)
			p := newBrandPanel(t, "owner", panelTestTenant)
			b := p.browser(t)
			res := brandPost(b, brandAccentHref, url.Values{"accent": {"#" + strings.ToLower(hex)}})
			body, _ := io.ReadAll(res.Body)
			page := string(body)
			if res.StatusCode != http.StatusOK || p.writer.writes() != 0 {
				t.Fatalf("%d, %d writes; want 200 and none", res.StatusCode, p.writer.writes())
			}
			if !strings.Contains(page, `id="brand-accent" type="color" name="accent" value="#`+strings.ToLower(hex)+`"`) {
				t.Errorf("the picker does not hold the colour that was tried")
			}
			if !strings.Contains(page, `darker, can: <span class="font-mono">#`+s.Hex()+`</span>.</p>`) {
				t.Errorf("the message does not name the suggestion #%s, in mono", s.Hex())
			}
			if !strings.Contains(page, `<input type="hidden" name="accent_hex" value="`+s.Hex()+`">`) {
				t.Errorf("no form carries the suggestion %s", s.Hex())
			}
			res = brandPost(b, brandAccentHref, url.Values{"accent_hex": {s.Hex()}})
			saved := p.writer.savedAccents()
			if outcomeOf(res) != "accent-saved" || len(saved) != 1 || saved[0].Accent != s {
				t.Errorf("posting the suggestion: %q, saved %+v; want %s", res.Header.Get("Location"), saved, s.Hex())
			}
		})
	}
}

// TestBrandAccent_TheDomainsAnswersAreTheHandlersOwn: the writer's ErrBrandNotPermitted
// is not-permitted with the domain's refusal row; its ErrAccentIllegible (a boundary and
// domain that disagree) is the form back with the suggestion, logged at ERROR; any other
// error is unavailable.
func TestBrandAccent_TheDomainsAnswersAreTheHandlersOwn(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{tenant.ErrBrandNotPermitted, "not-permitted"},
		{brand.ErrAccentIllegible, ""},
		{io.ErrUnexpectedEOF, "unavailable"},
	} {
		p := newBrandPanel(t, "owner", panelTestTenant)
		p.writer.err = tc.err
		res := brandPost(p.browser(t), brandAccentHref, url.Values{"accent": {"#DA291C"}})
		if tc.want == "" {
			if res.StatusCode != http.StatusOK || !strings.Contains(p.logs.String(), "level=ERROR") {
				t.Errorf("%v: %d, want the form back and an ERROR line", tc.err, res.StatusCode)
			}
			continue
		}
		if got := outcomeOf(res); got != tc.want {
			t.Errorf("%v: %q, want %s", tc.err, res.Header.Get("Location"), tc.want)
		}
		refusals := p.trail.count(ActionBrandUpdateRefused)
		if want := map[bool]int{true: 1, false: 0}[tc.want == "not-permitted"]; refusals != want {
			t.Errorf("%v: %d refusal rows, want %d", tc.err, refusals, want)
		}
	}
}

// --- taking it back ------------------------------------------------------------------

// TestBrandReset_TakesBackOneInputAtATime: what=logo clears the logo and nothing else,
// what=accent the accent and nothing else, each for the session's business and actor;
// any other value (none, both, a capital, a list) clears nothing and answers unreadable.
func TestBrandReset_TakesBackOneInputAtATime(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	b := p.browser(t)
	// A body naming another business and another actor changes nothing: the command is
	// built from the session alone.
	other := url.Values{"tenant_id": {"b2b2b2b2-0000-4000-8000-00000000000b"}, "actor_id": {"b2b2b2b2-0000-4000-8000-0000000000a1"}}
	if got := outcomeOf(brandPost(b, brandResetHref, url.Values{"what": {"logo"}, "tenant_id": other["tenant_id"], "actor_id": other["actor_id"]})); got != "logo-removed" {
		t.Errorf("what=logo: %q", got)
	}
	if got := outcomeOf(brandPost(b, brandResetHref, url.Values{"what": {"accent"}})); got != "accent-removed" {
		t.Errorf("what=accent: %q", got)
	}
	accent, logo := p.writer.clears()
	want := tenant.BrandClearCommand{TenantID: panelTestTenant, ActorID: panelTestAdmin}
	if len(accent) != 1 || len(logo) != 1 || accent[0] != want || logo[0] != want {
		t.Fatalf("clears: accent %+v, logo %+v; want one each for the session", accent, logo)
	}
	for _, form := range []url.Values{{}, {"what": {"both"}}, {"what": {"LOGO"}}, {"what": {""}}, {"what": {"all"}}} {
		if got := outcomeOf(brandPost(b, brandResetHref, form)); got != "unreadable" {
			t.Errorf("%v: %q, want unreadable", form, got)
		}
	}
	if p.writer.writes() != 2 {
		t.Errorf("%d writes, want the two above", p.writer.writes())
	}
}

// TestBrandWrite_TheBusinessBudgetRefusesPastThirty: accent saves and removals share one
// budget per business; the thirty-first is answered 429 with one refusal row (on the
// crossing), the thirty-second with none more, and neither reaches the writer.
func TestBrandWrite_TheBusinessBudgetRefusesPastThirty(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	b := p.browser(t)
	for i := 1; i <= brandWriteLimit; i++ {
		route, form := brandAccentHref, url.Values{"accent": {"#DA291C"}}
		if i%2 == 0 {
			route, form = brandResetHref, url.Values{"what": {"accent"}}
		}
		if res := brandPost(b, route, form); res.StatusCode != http.StatusSeeOther {
			t.Fatalf("change %d: %d", i, res.StatusCode)
		}
	}
	for i := 1; i <= 2; i++ {
		if res := brandPost(b, brandAccentHref, url.Values{"accent": {"#DA291C"}}); res.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("change %d: %d, want 429", brandWriteLimit+i, res.StatusCode)
		}
	}
	if n := p.trail.count(ActionBrandUpdateRefused); n != 1 {
		t.Errorf("%d refusal rows, want 1", n)
	}
	if p.writer.writes() != brandWriteLimit {
		t.Errorf("%d writes, want %d", p.writer.writes(), brandWriteLimit)
	}
}

// TestBrandWrite_AFormOverItsBoundIsNotRead: the accent and reset forms' bodies are
// bounded at brandFormMaxBody (2 KiB): a body one byte over it -- a valid colour or
// input padded in a second field -- is answered unreadable and nothing is written; at
// the bound it is read.
func TestBrandWrite_AFormOverItsBoundIsNotRead(t *testing.T) {
	for _, tc := range []struct {
		route, field, value, done string
	}{
		{brandAccentHref, brandFieldTyped, "DA291C", "accent-saved"},
		{brandResetHref, brandFieldWhat, brandFieldAccent, "accent-removed"},
	} {
		for _, over := range []int{0, 1} {
			p := newBrandPanel(t, "owner", panelTestTenant)
			head := url.Values{tc.field: {tc.value}}.Encode() + "&pad="
			body := head + strings.Repeat("a", brandFormMaxBody-len(head)+over)
			res := p.browser(t).doRaw(http.MethodPost, tc.route, body).Result()
			want := tc.done
			if over == 1 {
				want = "unreadable"
			}
			if got := outcomeOf(res); got != want || (over == 1) != (p.writer.writes() == 0) {
				t.Errorf("%s, %d bytes: %q with %d writes, want %q", tc.route, len(body), got, p.writer.writes(), want)
			}
		}
	}
}

// --- the outcome words ---------------------------------------------------------------

// TestBrandOutcomes_EveryWordIsDrawnAndNoOtherText: each word of the closed vocabulary,
// put in the section's brand parameter, draws its heading and its sentence inside the
// editor and nowhere else on the page; a word outside the vocabulary -- markup, another
// word's prefix, the details form's word -- draws no notice; brandReturn builds the
// address only from the vocabulary.
func TestBrandOutcomes_EveryWordIsDrawnAndNoOtherText(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	b := p.browser(t)
	plain := htmlOf(t, b.do(http.MethodGet, accountHref, nil))
	for word, o := range brandOutcomes {
		page := htmlOf(t, b.do(http.MethodGet, brandReturnPath(word), nil))
		editor := page[strings.Index(page, `<section id="brand"`):]
		if !strings.Contains(editor, htmlText(o.Sentence)) || !strings.Contains(editor, htmlText(o.Heading)) {
			t.Errorf("%s: the editor does not draw its notice", word)
		}
		if withoutBrandEditor(t, accountHref, page) != withoutBrandEditor(t, accountHref, plain) {
			t.Errorf("%s: the page changed outside the editor", word)
		}
	}
	for _, w := range []string{"<script>", "logo", "saved", "not-permitted-x", "LOGO-SAVED"} {
		page := htmlOf(t, b.do(http.MethodGet, accountHref+"?brand="+url.QueryEscape(w), nil))
		if page != plain {
			t.Errorf("%q drew something", w)
		}
	}
	if got := brandReturn("<script>"); got != accountHref+"#brand" {
		t.Errorf("brandReturn of an unknown word = %q", got)
	}
}

// --- the view ------------------------------------------------------------------------

// previewOpen is how the preview's block opens: the page's one inert element.
const previewOpen = `<div inert class="`

// inertRE is any inert attribute on the page.
var inertRE = regexp.MustCompile(`<[a-z]+[^>]*\sinert[\s>=]`)

// previewSpan is the preview's block on page: its content (between its own tag and its
// own closing tag, nested divs counted) and the block's byte range, opening tag to
// closing tag. The page has exactly one inert element and it is that block; ok is false
// when it has none.
func previewSpan(t *testing.T, page string) (inner string, start, end int, ok bool) {
	t.Helper()
	if n := len(inertRE.FindAllString(page, -1)); n > 1 || strings.Count(page, previewOpen) != n {
		t.Fatalf("%d inert elements, %d preview blocks; want at most one, and it the preview", n, strings.Count(page, previewOpen))
	}
	start = strings.Index(page, previewOpen)
	if start < 0 {
		return "", 0, 0, false
	}
	open := start + strings.Index(page[start:], ">") + 1
	depth := 1
	for i := open; i < len(page); {
		switch {
		case strings.HasPrefix(page[i:], "<div"):
			depth++
			i += len("<div")
		case strings.HasPrefix(page[i:], "</div>"):
			depth--
			if depth == 0 {
				return page[open:i], start, i + len("</div>"), true
			}
			i += len("</div>")
		default:
			i++
		}
	}
	t.Fatal("the preview block does not close")
	return "", 0, 0, false
}

// previewOf is the preview's content on page, failing the test when there is none.
func previewOf(t *testing.T, page string) string {
	t.Helper()
	inner, _, _, ok := previewSpan(t, page)
	if !ok {
		t.Fatal("no preview block")
	}
	return inner
}

// formDepthAt is how many <form> elements are open at byte offset i of page.
func formDepthAt(page string, i int) int {
	depth := 0
	for _, m := range regexp.MustCompile(`(?i)<(/?)form\b`).FindAllStringSubmatchIndex(page[:i], -1) {
		if page[m[2]:m[3]] == "/" {
			depth--
		} else {
			depth++
		}
	}
	return depth
}

// brandShapes are the editor's five saved states, as the chrome reads them.
func brandShapes(t *testing.T) map[string]tenant.PanelBrand {
	refused := wl8Brand(t, false, true)
	refused.AccentRefused = true
	return map[string]tenant.PanelBrand{
		"no brand":         {},
		"accent alone":     wl8Brand(t, true, false),
		"logo alone":       wl8Brand(t, false, true),
		"accent and logo":  wl8Brand(t, true, true),
		"a refused accent": refused,
	}
}

// TestBrandPreview_IsTheTapScreensOwnComponentsAndCannotSubmit renders the section for
// an owner and a manager under five saved brands. The preview block is the page's one
// inert element and holds, in order and EACH EXACTLY ONCE, the tap screen's three
// components as the brand calls for them -- layout.BrandHeader of TapBrand(the
// panel-route logo, the saved theme), TapHeading with the signed-in name,
// TapButtonFace(TapButtonPreview) -- and nothing else; it contains no "<form" and no
// "/api/checkin"; its one button is type="button" and is inside no <form> of the page;
// no element on the page carries a form= attribute; the rest of the editor draws no
// header (withoutBrandEditor); and every form of the editor posts to one of the three
// brand routes.
func TestBrandPreview_IsTheTapScreensOwnComponentsAndCannotSubmit(t *testing.T) {
	for _, role := range []string{"owner", "manager"} {
		for name, pb := range brandShapes(t) {
			p := newBrandPanel(t, role, panelTestTenant)
			p.brands.set(pb, nil)
			page := htmlOf(t, p.browser(t).do(http.MethodGet, accountHref, nil))
			where := role + ", " + name
			block, start, _, ok := previewSpan(t, page)
			if !ok {
				t.Fatalf("%s: no preview block", where)
			}
			inner := start + strings.Index(page[start:], ">") + 1
			withoutBrandEditor(t, accountHref, page)
			var logo layout.Logo
			var theme layout.Theme
			if pb.HasLogo {
				logo = layout.PreviewLogo(pb.Logo.SHA256, pb.Logo.Width, pb.Logo.Height, wl8Name)
			}
			if pb.HasAccent {
				theme = layout.ThemeOf(pb.Accent)
			}
			parts := []string{
				renderString(t, layout.BrandHeader(layout.TapBrand(logo, theme))),
				renderString(t, pages.TapHeading("Maria Borg", "")),
				renderString(t, pages.TapButtonFace(pages.TapButtonPreview)),
			}
			at := 0
			for _, part := range parts {
				// Once each: a second button or a second greeting is a different screen
				// (section 9: one screen, one button), and the order loop alone finds the
				// first of each.
				if n := strings.Count(block, part); n != 1 {
					t.Errorf("%s: the preview carries %q %d times, want once\nblock: %s", where, part, n, block)
				}
				i := strings.Index(block[at:], part)
				if i < 0 {
					t.Fatalf("%s: the preview does not carry, in order, %q\nblock: %s", where, part, block)
				}
				at += i + len(part)
			}
			if strings.TrimSpace(strings.Join(strings.Fields(strings.NewReplacer(parts[0], "", parts[1], "", parts[2], "").Replace(block)), "")) != "" {
				t.Errorf("%s: the preview carries more than the three components: %q", where, block)
			}
			for _, never := range []string{"<form", "/api/checkin", "data-tap"} {
				if strings.Contains(block, never) {
					t.Errorf("%s: the preview carries %q", where, never)
				}
			}
			if strings.Contains(page, "/api/checkin") || regexp.MustCompile(`\sform=`).MatchString(page) {
				t.Errorf("%s: the page names the tap form's route or carries a form= attribute", where)
			}
			btn := inner + strings.Index(block, parts[2])
			if d := formDepthAt(page, btn); d != 0 {
				t.Errorf("%s: the preview's button is inside %d form(s)", where, d)
			}
			for _, f := range regexp.MustCompile(`<form method="post" action="([^"]*)"`).FindAllStringSubmatch(page, -1) {
				if f[1] != "/admin/logout" && f[1] != accountHref && f[1] != accountPasswordHref &&
					!slices.Contains([]string{brandLogoHref, brandAccentHref, brandResetHref}, f[1]) {
					t.Errorf("%s: a form posts to %q", where, f[1])
				}
			}
			owned := strings.Contains(page, `action="`+brandAccentHref+`"`)
			if owned != (role == "owner") {
				t.Errorf("%s: the editor's forms are drawn: %v", where, owned)
			}
			// The picker starts on the SAVED colour, Taptime green when none passes.
			if owned {
				picker := "#1f5c41"
				if pb.HasAccent {
					picker = "#" + strings.ToLower(pb.Accent.Hex())
				}
				if !strings.Contains(page, `type="color" name="accent" value="`+picker+`"`) {
					t.Errorf("%s: the picker does not start on %s", where, picker)
				}
			}
		}
	}
}

// TestBrandPreview_TheLogoIsThePanelRoute mounts the panel, the tap surface and the two
// logo routes as cmd/tappa does, for business A with its logo: the preview's <img src> is
// /admin/brand/logo/<digest>, its box the STORED one (512 x 128, the tap header's
// attributes), its alt the business's name; requested with the same panel cookie it
// answers 200 with the logo's bytes.
func TestBrandPreview_TheLogoIsThePanelRoute(t *testing.T) {
	tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()},
		liveEmployee(logoTenantA), &fakeCheckins{}, noActivation{}, &fakeAudit{}, tapCfg(), discardLogger())
	if err != nil {
		t.Fatalf("NewTap: %v", err)
	}
	brands := &fakeBrands{brand: wl8Brand(t, true, true)}
	panel := newAdminRouterWithBrands(t, livePanel(logoTenantA), brands, discardLogger())
	logos, err := NewBrandLogos(newFakeLogoReader(), panel, tp, discardLogger())
	if err != nil {
		t.Fatalf("NewBrandLogos: %v", err)
	}
	r := chi.NewRouter()
	tp.Mount(r)
	panel.Mount(r)
	logos.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = panelCookie().Value
	page := getSection(t, b, accountHref)
	imgs := imgTagRE.FindAllString(previewOf(t, page.body), -1)
	if len(imgs) != 1 {
		t.Fatalf("the preview drew %d <img>", len(imgs))
	}
	for name, want := range map[string]string{"src": adminLogoHref(digestOf(logoPNGA)), "width": "512", "height": "128",
		"alt": htmlEscaper.Replace(wl8Name)} {
		if got, ok := attr(imgs[0], name); !ok || got != want {
			t.Errorf("the preview logo's %s is %q (%v), want %q", name, got, ok, want)
		}
	}
	if !strings.Contains(page.csp, "img-src 'self'") {
		t.Errorf("the page draws the preview's <img> under %q", page.csp)
	}
	src, _ := attr(imgs[0], "src")
	res := b.do(http.MethodGet, src, nil).Result()
	got, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || string(got) != string(logoPNGA) {
		t.Errorf("GET %s: %d, %d bytes; want 200 and business A's logo", src, res.StatusCode, len(got))
	}
}

// TestBrandPreview_ShowsOnlyTheSavedAccent: with DA291C saved, an illegible 808080
// tried, the page that comes back links app.css and the SAVED theme only; 808080 is in
// no stylesheet href, the stripe is drawn once (the saved accent's), and 808080 appears
// only in the picker's value and the typed box -- the colour input shows it, nothing
// else paints it.
func TestBrandPreview_ShowsOnlyTheSavedAccent(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	p.brands.set(wl8Brand(t, true, false), nil)
	res := brandPost(p.browser(t), brandAccentHref, url.Values{"accent": {"#000000"}, "accent_hex": {"808080"}})
	body, _ := io.ReadAll(res.Body)
	page := string(body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d", res.StatusCode)
	}
	if hrefs, inHead := stylesheetsOf(page); !slices.Equal(hrefs, []string{"/static/css/app.css", "/brand/theme/DA291C.css"}) || !inHead {
		t.Errorf("stylesheets %v, want app.css and the saved theme", hrefs)
	}
	if n := strings.Count(page, "panel-stripe"); n != 1 {
		t.Errorf("%d stripes, want 1", n)
	}
	if n := strings.Count(strings.ToLower(page), "808080"); n != 2 {
		t.Errorf("808080 appears %d times, want 2 (the picker and the typed box)", n)
	}
	if !strings.Contains(page, `value="#808080"`) || !strings.Contains(page, `value="808080"`) {
		t.Error("the tried colour is not in the picker and the box")
	}
}

// TestBrandPreview_ARefusedColourLeavesThePreviewAsSaved: under each of the five saved
// brands, an owner's accent save that is refused -- an illegible colour picked, one
// typed, one from the band's other side, a code that is no colour, and a legible colour
// the domain's gate refuses (the boundary and the domain disagreeing) -- comes back 200
// with the error drawn, and its preview block is BYTE-EQUAL to the same business's plain
// load: the header (with decision K-2a's ink wordmark, or not) and the button are what
// is saved. The page outside the editor is byte-equal too, so the candidate reaches
// neither the preview nor the chrome (the stylesheet, the stripe, the header).
func TestBrandPreview_ARefusedColourLeavesThePreviewAsSaved(t *testing.T) {
	shapes := brandShapes(t)
	// PREMISE: the comparison can see the K-2a switch -- the plain previews of a
	// business with no brand and of one with an accent alone draw different wordmarks.
	green, ink := `text-tappa-green">taptime</span>`, `text-ink">taptime</span>`
	for name, want := range map[string]string{"no brand": green, "accent alone": ink} {
		p := newBrandPanel(t, "owner", panelTestTenant)
		p.brands.set(shapes[name], nil)
		if block := previewOf(t, htmlOf(t, p.browser(t).do(http.MethodGet, accountHref, nil))); !strings.Contains(block, want) {
			t.Fatalf("PREMISE: %s: the plain preview does not draw %q\n%s", name, want, block)
		}
	}
	for name, pb := range shapes {
		for _, tc := range []struct {
			name   string
			form   url.Values
			domain error
		}{
			{"illegible, picked", url.Values{"accent": {"#808080"}}, nil},
			{"illegible, typed", url.Values{"accent": {"#1f5c41"}, "accent_hex": {"808080"}}, nil},
			{"illegible, the band's other side", url.Values{"accent_hex": {"E0457B"}}, nil},
			{"not a colour", url.Values{"accent_hex": {"zz12"}}, nil},
			{"legible, refused by the domain", url.Values{"accent_hex": {"DA291C"}}, brand.ErrAccentIllegible},
		} {
			where := name + ", " + tc.name
			p := newBrandPanel(t, "owner", panelTestTenant)
			p.brands.set(pb, nil)
			p.writer.err = tc.domain
			b := p.browser(t)
			plain := htmlOf(t, b.do(http.MethodGet, accountHref, nil))
			res := brandPost(b, brandAccentHref, tc.form)
			body, _ := io.ReadAll(res.Body)
			page := string(body)
			if res.StatusCode != http.StatusOK || !strings.Contains(page, `id="brand-accent-error"`) {
				t.Fatalf("%s: %d, want 200 with the form's error", where, res.StatusCode)
			}
			if got, want := previewOf(t, page), previewOf(t, plain); got != want {
				t.Errorf("%s: the preview after the refused save is not the plain load's\n got: %s\nwant: %s", where, got, want)
			}
			if withoutBrandEditor(t, accountHref, page) != withoutBrandEditor(t, accountHref, plain) {
				t.Errorf("%s: the page outside the editor changed", where)
			}
		}
	}
}

// TestPanelBrandView_ThePreviewLogoIsDrawnOnlyWhereTheChromesIs: over digests and boxes
// at and past the table's bounds, panelBrandView's preview logo is drawn only when the
// chrome's logo is (so the page's img-src, decided by the chrome, covers it), and for
// every box the table can hold (1..512) the two are drawn together.
func TestPanelBrandView_ThePreviewLogoIsDrawnOnlyWhereTheChromesIs(t *testing.T) {
	good := digestOf(logoPNGA)
	for _, sha := range []string{good, strings.ToUpper(good), good[:63], ""} {
		for _, w := range []int{-1, 0, 1, 192, 512, 513} {
			for _, h := range []int{-1, 0, 1, 32, 512, 513} {
				b := tenant.PanelBrand{Name: "x", HasLogo: true, Logo: tenant.LogoRef{SHA256: sha, MIME: "image/png", Width: w, Height: h}}
				v := panelBrandView(b)
				if v.Preview.Drawn() && !v.DrawsLogo() {
					t.Errorf("%q %dx%d: the preview draws a logo the chrome does not", sha, w, h)
				}
				stored := sha == good && w >= 1 && w <= 512 && h >= 1 && h <= 512
				if stored && v.Preview.Drawn() != v.DrawsLogo() {
					t.Errorf("%q %dx%d: preview %v, chrome %v", sha, w, h, v.Preview.Drawn(), v.DrawsLogo())
				}
			}
		}
	}
}

// TestBrandEditor_EveryControlIsATouchTarget: on the owner's editor with a logo and an
// accent saved and an illegible colour tried (every control drawn: the file input, the
// picker, the box, the suggestion's swatch and the five buttons), every visible form
// control and every button or link carries a class of panelTouchTargets (44 px, or the
// tap button's 64) -- the markup half of TestPanelScreens_TouchTargetClassesReserve44px.
func TestBrandEditor_EveryControlIsATouchTarget(t *testing.T) {
	p := newBrandPanel(t, "owner", panelTestTenant)
	p.brands.set(wl8Brand(t, true, true), nil)
	res := brandPost(p.browser(t), brandAccentHref, url.Values{"accent_hex": {"808080"}})
	body, _ := io.ReadAll(res.Body)
	page := string(body)
	editor := page[strings.Index(page, `<section id="brand"`):]
	controls, buttons := 0, 0
	for _, m := range regexp.MustCompile(`(?is)<(input|select|textarea)\b([^>]*)>`).FindAllStringSubmatch(editor, -1) {
		if regexp.MustCompile(`(?i)type\s*=\s*["']?hidden`).MatchString(m[2]) {
			continue
		}
		controls++
		c, _ := attr(" "+m[2], "class")
		if !hasTouchTargetClass(c) {
			t.Errorf("<%s%s> carries no touch-target class", m[1], m[2])
		}
	}
	for _, tgt := range pressTargetsOf(editor) {
		buttons++
		if !hasTouchTargetClass(tgt.classes) {
			t.Errorf("<%s class=%q> carries no touch-target class", tgt.tag, tgt.classes)
		}
	}
	if controls < 4 || buttons < 5 {
		t.Fatalf("PREMISE: %d controls and %d press targets; the editor draws at least 4 and 5", controls, buttons)
	}
}

// TestBrandEditor_TheFormsSendWhatTheRoutesRead: on the owner's editor with every form
// drawn (a logo and an accent saved, an illegible colour tried), each form's action,
// encoding and SENT fields -- a disabled control sends nothing -- are exactly what its
// route reads, from the handler's own constants: the upload's one file input named
// logoPartName, multipart; "Remove the logo"'s one hidden brandFieldWhat valued
// brandFieldLogo; the accent form's picker (brandFieldPicker) and box
// (brandFieldTyped); the suggestion's one hidden brandFieldTyped carrying Suggest's
// code; "Back to Taptime green"'s one hidden brandFieldWhat valued brandFieldAccent. A
// field renamed on one side alone fails here, where the page's own form would otherwise
// fail at its route without a test seeing it. With nothing saved, the two "take it
// back" forms are not drawn: only the upload and the accent form.
func TestBrandEditor_TheFormsSendWhatTheRoutesRead(t *testing.T) {
	tried, err := brand.ParseAccent("808080")
	if err != nil {
		t.Fatal(err)
	}
	p := newBrandPanel(t, "owner", panelTestTenant)
	p.brands.set(wl8Brand(t, true, true), nil)
	res := brandPost(p.browser(t), brandAccentHref, url.Values{brandFieldTyped: {tried.Hex()}})
	body, _ := io.ReadAll(res.Body)
	page := string(body)
	want := []string{
		brandLogoHref + " multipart/form-data [file " + logoPartName + "=]",
		brandResetHref + "  [hidden " + brandFieldWhat + "=" + brandFieldLogo + "]",
		brandAccentHref + "  [color " + brandFieldPicker + "=, text " + brandFieldTyped + "=]",
		brandAccentHref + "  [hidden " + brandFieldTyped + "=" + brand.Suggest(tried).Hex() + "]",
		brandResetHref + "  [hidden " + brandFieldWhat + "=" + brandFieldAccent + "]",
	}
	if got := editorForms(t, page); !slices.Equal(got, want) {
		t.Errorf("the editor's forms send\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	p = newBrandPanel(t, "owner", panelTestTenant)
	want = []string{want[0], want[2]}
	if got := editorForms(t, htmlOf(t, p.browser(t).do(http.MethodGet, accountHref, nil))); !slices.Equal(got, want) {
		t.Errorf("with nothing saved, the editor's forms send\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

// editorForms is each form of the brand editor on page as "action enctype [type
// name=value, ...]": the fields a submission sends (named, not disabled), with the
// value of a hidden one.
func editorForms(t *testing.T, page string) []string {
	t.Helper()
	start := strings.Index(page, `<section id="brand"`)
	if start < 0 {
		t.Fatal("no brand editor")
	}
	editor := page[start : start+len(page)-len(withoutBrandEditor(t, accountHref, page))]
	disabled := regexp.MustCompile(`\sdisabled[\s>/=]`)
	var got []string
	for _, f := range regexp.MustCompile(`(?s)<form\b([^>]*)>(.*?)</form>`).FindAllStringSubmatch(editor, -1) {
		action, _ := attr(" "+f[1], "action")
		enctype, _ := attr(" "+f[1], "enctype")
		var sent []string
		for _, in := range regexp.MustCompile(`(?is)<(input|select|textarea|button)\b([^>]*)>`).FindAllStringSubmatch(f[2], -1) {
			name, named := attr(" "+in[2], "name")
			if !named || disabled.MatchString(in[2]+">") {
				continue
			}
			typ, _ := attr(" "+in[2], "type")
			value := ""
			if typ == "hidden" {
				value, _ = attr(" "+in[2], "value")
			}
			sent = append(sent, typ+" "+name+"="+value)
		}
		got = append(got, action+" "+enctype+" ["+strings.Join(sent, ", ")+"]")
	}
	return got
}

// TestBrandRoutes_TheWriterIsRequired: NewAdminAuth refuses a nil brand writer, untyped
// and typed, as it refuses a nil brand reader (TestPanelBrand_TheReaderIsRequired) -- a
// panel built without one would answer the three routes with a nil dereference.
func TestBrandRoutes_TheWriterIsRequired(t *testing.T) {
	records := newFakeLedger()
	for name, writer := range map[string]panelBrandWriter{"nil": nil, "typed nil": (*fakeBrandWriter)(nil)} {
		_, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
			&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
			newFakeAccount(), newFakeBrands(), writer, nil, &fakeNotices{}, adminTestConfig(), discardLogger())
		if err == nil {
			t.Errorf("a %s brand writer was accepted", name)
		}
	}
	if _, err := NewAdminAuth(&fakeAdmins{}, &fakeTrail{}, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(), newFakeScribe(), newFakeBooks(),
		newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{}, adminTestConfig(), discardLogger()); err != nil {
		t.Fatalf("PREMISE: the same call with a writer fails: %v", err)
	}
}

// TestBrandEditor_AManagerSeesThePreviewAndNoForm: a manager's section draws the preview
// and the reason, and none of the editor's fields.
func TestBrandEditor_AManagerSeesThePreviewAndNoForm(t *testing.T) {
	p := newBrandPanel(t, "manager", panelTestTenant)
	p.brands.set(wl8Brand(t, true, true), nil)
	page := htmlOf(t, p.browser(t).do(http.MethodGet, accountHref, nil))
	for _, field := range []string{`name="logo"`, `name="accent"`, `name="accent_hex"`, `name="what"`} {
		if strings.Contains(page, field) {
			t.Errorf("a manager is shown %s", field)
		}
	}
	if _, _, _, ok := previewSpan(t, page); !ok || !strings.Contains(page, htmlText("changed by an owner of the business")) {
		t.Error("a manager is not shown the preview and the reason")
	}
}
