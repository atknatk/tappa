package operator_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/oauth"
)

// testSecrets is a client secret minter under a fixed, fake TAPPA_OAUTH_HMAC_KEY.
var testSecrets = func() *oauth.Hasher {
	h, err := oauth.NewHasher(bytes.Repeat([]byte("q"), 32))
	if err != nil {
		panic(err)
	}
	return h
}()

// noApps is an app registry store that refuses every call (the tests that do not reach the screen).
type noApps struct{}

func (noApps) OAuthClients(context.Context, string, int32) ([]db.OAuthClientRow, error) {
	return nil, db.ErrOperatorRefused
}
func (noApps) RegisterOAuthClient(context.Context, string, string, []string, []string, string, string) (uuid.UUID, error) {
	return uuid.Nil, db.ErrOperatorRefused
}
func (noApps) RotateOAuthClientSecret(context.Context, string, uuid.UUID, string, string) error {
	return db.ErrOperatorRefused
}
func (noApps) SetOAuthClientRedirects(context.Context, string, uuid.UUID, []string) error {
	return db.ErrOperatorRefused
}
func (noApps) DisableOAuthClient(context.Context, string, uuid.UUID) error {
	return db.ErrOperatorRefused
}

// fakeApps is the registry the way 00107's op_* behave for these tests: it keeps rows, records every
// argument it was handed (the hash, never a secret), and answers a write on a disabled or unknown app
// with nil and no change (ADR 0021 §2 v 7).
type fakeApps struct {
	mu     sync.Mutex
	rows   []db.OAuthClientRow
	hashes map[uuid.UUID]string
	args   []string
	fail   error
}

func (f *fakeApps) OAuthClients(context.Context, string, int32) ([]db.OAuthClientRow, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	out := make([]db.OAuthClientRow, len(f.rows))
	for i := range f.rows {
		out[len(f.rows)-1-i] = f.rows[i]
	}
	return out, nil
}

func (f *fakeApps) RegisterOAuthClient(_ context.Context, _ string, name string, redirects, scopes []string, hash, hint string) (uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := uuid.New()
	f.rows = append(f.rows, db.OAuthClientRow{ClientID: id, Name: name, RedirectURIs: redirects, AllowedScopes: scopes, SecretHint: hint, CreatedAt: time.Now()})
	if f.hashes == nil {
		f.hashes = map[uuid.UUID]string{}
	}
	f.hashes[id] = hash
	f.args = append(f.args, name, strings.Join(redirects, " "), strings.Join(scopes, " "), hash, hint)
	return id, nil
}

func (f *fakeApps) find(id uuid.UUID) *db.OAuthClientRow {
	for i := range f.rows {
		if f.rows[i].ClientID == id {
			return &f.rows[i]
		}
	}
	return nil
}

func (f *fakeApps) RotateOAuthClientSecret(_ context.Context, _ string, id uuid.UUID, hash, hint string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.args = append(f.args, hash, hint)
	if r := f.find(id); r != nil && r.DisabledAt == nil {
		r.SecretHint = hint
		f.hashes[id] = hash
	}
	return nil
}

func (f *fakeApps) SetOAuthClientRedirects(_ context.Context, _ string, id uuid.UUID, redirects []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.find(id); r != nil && r.DisabledAt == nil {
		r.RedirectURIs = redirects
	}
	return nil
}

func (f *fakeApps) DisableOAuthClient(_ context.Context, _ string, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.find(id); r != nil && r.DisabledAt == nil {
		now := time.Now()
		r.DisabledAt = &now
	}
	return nil
}

// secretOn lifts the one-time secret off a page (a whole tt_cs_ value -- the hint is four characters),
// or "".
func secretOn(body string) string {
	return regexp.MustCompile(`tt_cs_` + oauth.Env + `_[A-Za-z0-9_-]{43}`).FindString(body)
}

// TestOperatorApps_RegisterShowsTheSecretOnceAndStoresItsHash (ADR 0047 §10.2-§10.3, OA-7): a
// registration hands the op_* the secret's HMAC and hint -- never the secret -- and answers 200 with
// the client_id and the secret; the next GET of the list shows the hint and NOT the secret; no log line
// carries it; the secret authenticates against the stored hash.
func TestOperatorApps_RegisterShowsTheSecretOnceAndStoresItsHash(t *testing.T) {
	g := newRig(t)
	live := g.signIn(g.active())
	w := g.post("/operator/apps", url.Values{"name": {"Malta Calculator"},
		"redirect_uris": {"https://maltacalculator.com/api/integrations/taptime/callback\n"},
		"scope":         {"timesheets:read", "employees:read", "employees:write"}}, live)
	if w.Code != http.StatusOK {
		t.Fatalf("register: %d %s", w.Code, w.Body.String())
	}
	secret := secretOn(w.Body.String())
	if secret == "" || !strings.Contains(w.Body.String(), "app_") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("the register answer lacks the secret or the client_id, or is cacheable (%q)", w.Header().Get("Cache-Control"))
	}
	g.apps.mu.Lock()
	row := g.apps.rows[0]
	hash := g.apps.hashes[row.ClientID]
	args := strings.Join(g.apps.args, "|")
	g.apps.mu.Unlock()
	if strings.Contains(args, secret) {
		t.Fatal("the store was handed the secret")
	}
	d, _ := testSecrets.Hash(oauth.KindSecret, oauth.ParseValue(secret))
	if d.RevealToTheDatabase() != hash || row.SecretHint != secret[:len(oauth.Prefix(oauth.KindSecret))+4] {
		t.Fatal("the stored hash or hint is not the shown secret's")
	}
	if strings.Join(row.AllowedScopes, " ") != "employees:read employees:write timesheets:read" {
		t.Fatalf("the scopes stored: %v", row.AllowedScopes)
	}
	again := g.get("/operator/apps", live)
	if again.Code != 200 || strings.Contains(again.Body.String(), secret) || !strings.Contains(again.Body.String(), row.SecretHint) {
		t.Fatal("the list shows the secret again, or not its hint")
	}
	if strings.Contains(g.logs.String(), secret) || strings.Contains(g.logs.String(), hash) {
		t.Fatal("the secret or its hash reached the log")
	}
}

// TestOperatorApps_TheRegistrationRules (§10.2, §13): a name out of its rule, a redirect list the rule
// refuses (http, a query, an IP, a port, six URIs, a loopback URI in the production build), no scope,
// and webhooks:manage are refused with 422 and nothing is written.
func TestOperatorApps_TheRegistrationRules(t *testing.T) {
	g := newRig(t)
	live := g.signIn(g.active())
	good := func() url.Values {
		return url.Values{"name": {"App"}, "redirect_uris": {"https://a.example/cb"}, "scope": {"timesheets:read"}}
	}
	cases := map[string]func(url.Values){
		"no name":         func(v url.Values) { v.Set("name", "  ") },
		"a 65-char name":  func(v url.Values) { v.Set("name", strings.Repeat("x", 65)) },
		"a bidi override": func(v url.Values) { v.Set("name", "App\u202e") },
		"http":            func(v url.Values) { v.Set("redirect_uris", "http://a.example/cb") },
		"a query":         func(v url.Values) { v.Set("redirect_uris", "https://a.example/cb?x=1") },
		"an IP":           func(v url.Values) { v.Set("redirect_uris", "https://203.0.113.1/cb") },
		"port 8443":       func(v url.Values) { v.Set("redirect_uris", "https://a.example:8443/cb") },
		"six URIs": func(v url.Values) {
			v.Set("redirect_uris", strings.Repeat("https://a.example/x\n", 1)+"https://a.example/1\nhttps://a.example/2\nhttps://a.example/3\nhttps://a.example/4\nhttps://a.example/5")
		},
		"no URI":          func(v url.Values) { v.Set("redirect_uris", "\n") },
		"no scope":        func(v url.Values) { v.Del("scope") },
		"webhooks:manage": func(v url.Values) { v["scope"] = []string{"timesheets:read", "webhooks:manage"} },
	}
	if !oauth.LoopbackRedirects {
		cases["a loopback URI in the production build"] = func(v url.Values) { v.Set("redirect_uris", "http://localhost:3000/api/integrations/taptime/callback") }
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			v := good()
			edit(v)
			if w := g.post("/operator/apps", v, live); w.Code != http.StatusUnprocessableEntity || secretOn(w.Body.String()) != "" {
				t.Fatalf("%d", w.Code)
			}
			g.apps.mu.Lock()
			n := len(g.apps.rows)
			g.apps.mu.Unlock()
			if n != 0 {
				t.Fatal("a refused registration was written")
			}
		})
	}
	if w := g.post("/operator/apps", good(), live); w.Code != 200 {
		t.Fatalf("CONTROL: a good registration: %d", w.Code)
	}
}

// TestOperatorApps_RotateRedirectsAndDisable: a rotation shows a new secret once and replaces the
// hash; editing the redirect list replaces it (303 and the list's sentence); turning the app off is one
// way, and a rotation of a turned-off app shows no secret.
func TestOperatorApps_RotateRedirectsAndDisable(t *testing.T) {
	g := newRig(t)
	live := g.signIn(g.active())
	w := g.post("/operator/apps", url.Values{"name": {"App"}, "redirect_uris": {"https://a.example/cb"}, "scope": {"timesheets:read"}}, live)
	first := secretOn(w.Body.String())
	g.apps.mu.Lock()
	id := g.apps.rows[0].ClientID
	g.apps.mu.Unlock()
	w = g.post("/operator/apps/"+id.String()+"/rotate", nil, live)
	second := secretOn(w.Body.String())
	if w.Code != 200 || second == "" || second == first {
		t.Fatalf("rotate: %d", w.Code)
	}
	d, _ := testSecrets.Hash(oauth.KindSecret, oauth.ParseValue(second))
	g.apps.mu.Lock()
	if g.apps.hashes[id] != d.RevealToTheDatabase() {
		t.Fatal("the rotation did not store the new secret's hash")
	}
	g.apps.mu.Unlock()
	w = g.post("/operator/apps/"+id.String()+"/redirects", url.Values{"redirect_uris": {"https://a.example/new\nhttps://b.example/cb"}}, live)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/operator/apps?done=redirects&app="+id.String() {
		t.Fatalf("redirects: %d %q", w.Code, w.Header().Get("Location"))
	}
	if page := g.get(w.Header().Get("Location"), live); !strings.Contains(page.Body.String(), "are now the list below") || !strings.Contains(page.Body.String(), "https://b.example/cb") {
		t.Fatal("the redirect change's sentence or list")
	}
	if w := g.post("/operator/apps/"+id.String()+"/redirects", url.Values{"redirect_uris": {"https://a.example/cb#frag"}}, live); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a refused list: %d", w.Code)
	}
	if w := g.post("/operator/apps/"+id.String()+"/disable", url.Values{}, live); w.Code != http.StatusBadRequest {
		t.Fatalf("disable without the box: %d, want 400", w.Code)
	}
	g.apps.mu.Lock()
	stillLive := g.apps.rows[0].DisabledAt == nil
	g.apps.mu.Unlock()
	if !stillLive {
		t.Fatal("an unconfirmed disable turned the app off")
	}
	w = g.post("/operator/apps/"+id.String()+"/disable", url.Values{"confirm": {"yes"}}, live)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("disable: %d", w.Code)
	}
	if page := g.get(w.Header().Get("Location"), live); !strings.Contains(page.Body.String(), "is turned off") || strings.Contains(page.Body.String(), "/rotate") {
		t.Fatal("the turned-off app's sentence, or its forms are still drawn")
	}
	w = g.post("/operator/apps/"+id.String()+"/rotate", nil, live)
	if secretOn(w.Body.String()) != "" || !strings.Contains(w.Body.String(), "No secret was changed: the app is turned off") {
		t.Fatal("a turned-off app's rotation showed a secret")
	}
	if w := g.post("/operator/apps/not-an-id/disable", nil, live); w.Code != http.StatusNotFound {
		t.Fatalf("a malformed id: %d", w.Code)
	}
}

// TestOperatorApps_AFailedReadIs503AndSignedOutIsTheSignIn: the list's read failing is the fault
// page; a request without a session is the sign-in's 303 and reaches no store.
func TestOperatorApps_AFailedReadIs503AndSignedOutIsTheSignIn(t *testing.T) {
	g := newRig(t)
	if w := g.get("/operator/apps"); w.Code != http.StatusSeeOther || !strings.HasPrefix(w.Header().Get("Location"), "/operator/login") {
		t.Fatalf("signed out: %d %q", w.Code, w.Header().Get("Location"))
	}
	live := g.signIn(g.active())
	g.apps.mu.Lock()
	g.apps.fail = errors.New("db: read oauth clients: SQLSTATE 57014")
	g.apps.mu.Unlock()
	if w := g.get("/operator/apps", live); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("a failed read: %d", w.Code)
	}
}

// appHeaderApp is the app the header classes act on (a fixed id, so a designed Location names it).
const appHeaderApp = "71000000-0000-4000-8000-0000000000a1"

// TestOperatorHeaders_TheAppRegistryClassesCarryThePolicy drives the nine response classes of the app
// registry (C243-C251) with the 40-class test's hostile drive and checks (runHeaderClasses), each held
// to its classRoutes entry: the list, a registration and a rotation (200, the page -- the secret in the
// body and in no header), the redirect change and the turning off (303 to the list with the closed word
// and the path's id), and a wrong method on each of the four routes (405).
func TestOperatorHeaders_TheAppRegistryClassesCarryThePolicy(t *testing.T) {
	g := newRig(t)
	g.apps.rows = []db.OAuthClientRow{{ClientID: uuid.MustParse(appHeaderApp), Name: "FAKE Header App",
		RedirectURIs: []string{"https://header.example/cb"}, AllowedScopes: []string{"timesheets:read"}, SecretHint: "tt_cs_" + oauth.Env + "_Hdr0",
		CreatedAt: time.Now()}}
	g.apps.hashes = map[uuid.UUID]string{uuid.MustParse(appHeaderApp): "x"}
	ad := &addrs{}
	last := &driveLog{}
	send := func(r req) *httptest.ResponseRecorder {
		if r.remote == "" {
			r.remote = ad.next() + ":1"
		}
		r.host = opHost
		*last = driveLog{r.method, r.path}
		return g.do(hostileDrive(r))
	}
	live := g.signIn(g.active())
	app := "/operator/apps/" + appHeaderApp
	post := func(path string, form url.Values) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			return send(req{method: http.MethodPost, path: path, form: form, origin: opOrigin, cookies: []*http.Cookie{live}})
		}
	}
	wrong := func(method, path string) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			return send(req{method: method, path: path, origin: opOrigin, form: url.Values{}, cookies: []*http.Cookie{live}})
		}
	}
	classes := []headerClass{
		{"C243 the list", 200, func() *httptest.ResponseRecorder {
			return send(req{method: http.MethodGet, path: "/operator/apps", cookies: []*http.Cookie{live},
				header: map[string]string{"Sec-Fetch-Site": "same-origin"}})
		}},
		{"C244 register", 200, post("/operator/apps", url.Values{"name": {"FAKE Header New"}, "redirect_uris": {"https://new.example/cb"},
			"scope": {"employees:read"}})},
		{"C245 rotate", 200, post(app+"/rotate", url.Values{})},
		{"C246 redirects", 303, post(app+"/redirects", url.Values{"redirect_uris": {"https://header.example/new"}})},
		{"C247 disable", 303, post(app+"/disable", url.Values{"confirm": {"yes"}})},
		{"C248 PUT on the apps", 405, wrong(http.MethodPut, "/operator/apps")},
		{"C249 GET on rotate", 405, wrong(http.MethodGet, app+"/rotate")},
		{"C250 GET on redirects", 405, wrong(http.MethodGet, app+"/redirects")},
		{"C251 GET on disable", 405, wrong(http.MethodGet, app+"/disable")},
		{"C252 disable, the box not ticked", 400, post(app+"/disable", url.Values{})},
	}
	if scripted := runHeaderClasses(t, classes, last); len(scripted) != 0 {
		t.Errorf("classes %v loaded a script, want none", scripted)
	}
	for i, c := range classes {
		if !strings.HasPrefix(c.name, "C"+strconv.Itoa(243+i)+" ") {
			t.Fatalf("PREMISE: class %d is named %q", 243+i, c.name)
		}
	}
}

// TestOperatorApps_ARotationWhoseReadFailsSaysTheSecretMayHaveChanged (review L4): the rotation is
// written, and the list read after it fails -- the page says the secret MAY have been changed and to
// rotate again, never that nothing changed; and an app the read does not find is the same sentence.
func TestOperatorApps_ARotationWhoseReadFailsSaysTheSecretMayHaveChanged(t *testing.T) {
	g := newRig(t)
	live := g.signIn(g.active())
	w := g.post("/operator/apps", url.Values{"name": {"App"}, "redirect_uris": {"https://a.example/cb"}, "scope": {"timesheets:read"}}, live)
	if w.Code != 200 {
		t.Fatalf("register: %d", w.Code)
	}
	g.apps.mu.Lock()
	id := g.apps.rows[0].ClientID
	before := g.apps.hashes[id]
	g.apps.fail = errors.New("db: read oauth clients: SQLSTATE 57014")
	g.apps.mu.Unlock()
	w = g.post("/operator/apps/"+id.String()+"/rotate", url.Values{}, live)
	g.apps.mu.Lock()
	after := g.apps.hashes[id]
	g.apps.fail = nil
	g.apps.mu.Unlock()
	if before == after {
		t.Fatal("PREMISE: the rotation was not written")
	}
	body := w.Body.String()
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(body, "The secret may have been changed") ||
		strings.Contains(body, "No secret was changed") || secretOn(body) != "" {
		t.Fatalf("a rotation whose read failed: %d", w.Code)
	}
	// Not on the page (another app's id): the same honesty, no claim that nothing changed.
	w = g.post("/operator/apps/"+uuid.NewString()+"/rotate", url.Values{}, live)
	if !strings.Contains(w.Body.String(), "The secret may have been changed") || strings.Contains(w.Body.String(), "No secret was changed") {
		t.Fatal("an app not on the page: the sentence claims too much")
	}
}
