package oauthapi_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/handler/oauthapi"
	"github.com/atknatk/tappa/internal/oauth"
)

// fakeAuthority counts the resolver calls and answers what the test says.
type fakeAuthority struct {
	mu       sync.Mutex
	calls    atomic.Int64
	accept   bool
	failWith error
	deadline time.Duration
	exchange func() (oauth.Tokens, error)
	revoked  int
}

func (f *fakeAuthority) AuthenticateClient(ctx context.Context, id uuid.UUID, _ oauth.Value) (oauth.Client, error) {
	f.calls.Add(1)
	if dl, ok := ctx.Deadline(); ok {
		f.mu.Lock()
		f.deadline = time.Until(dl)
		f.mu.Unlock()
	}
	if f.failWith != nil {
		return oauth.Client{}, f.failWith
	}
	if !f.accept {
		return oauth.Client{}, oauth.ErrInvalidClient
	}
	return oauth.Client{ID: id}, nil
}

func (f *fakeAuthority) ExchangeCode(context.Context, context.Context, oauth.Client, oauth.Value, string, *string) (oauth.Tokens, error) {
	if f.exchange != nil {
		return f.exchange()
	}
	return oauth.Tokens{}, oauth.ErrInvalidGrant
}

func (f *fakeAuthority) Refresh(context.Context, context.Context, oauth.Client, oauth.Value, []string) (oauth.Tokens, error) {
	return oauth.Tokens{}, oauth.ErrInvalidGrant
}

func (f *fakeAuthority) Revoke(context.Context, oauth.Client, oauth.Value) error {
	f.mu.Lock()
	f.revoked++
	f.mu.Unlock()
	return nil
}

func newSurface(t *testing.T, a oauthapi.Authority, cfg oauthapi.Config) (http.Handler, *bytes.Buffer) {
	t.Helper()
	logs := &bytes.Buffer{}
	cfg.Authority = a
	cfg.Log = slog.New(slog.NewTextHandler(&lockedWriter{w: logs}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s, err := oauthapi.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	s.Mount(r)
	return r, logs
}

type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.String()
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func basic(id uuid.UUID, secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(oauth.ClientIDOf(id)+":"+secret))
}

func mint(t *testing.T, k oauth.Kind) oauth.Value {
	t.Helper()
	v, err := oauth.Mint(k)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// post sends a form to path with an optional Authorization header from one address.
func post(h http.Handler, path, auth string, form url.Values, addr string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	req.RemoteAddr = addr
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errorWord(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b struct {
		Error       string `json:"error"`
		Description string `json:"error_description"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil || b.Description == "" {
		t.Fatalf("not an error body (%d): %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("the error's headers: %v", rec.Header())
	}
	return b.Error
}

// TestOAuthToken_BasicAndBodyCombinations is ADR 0047 §5.1's table: Basic alone, Basic + the same body
// client_id, Basic + a different one, Basic + any body client_secret, no Basic.
func TestOAuthToken_BasicAndBodyCombinations(t *testing.T) {
	id := uuid.New()
	secret := mint(t, oauth.KindSecret).RevealToTheOperatorOnce()
	tests := []struct {
		name   string
		auth   string
		body   url.Values
		status int
		word   string
	}{
		{"Basic alone", basic(id, secret), url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}}, 400, "invalid_grant"},
		{"Basic and the same client_id", basic(id, secret), url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}, "client_id": {oauth.ClientIDOf(id)}}, 400, "invalid_grant"},
		{"Basic and another client_id", basic(id, secret), url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}, "client_id": {oauth.ClientIDOf(uuid.New())}}, 400, "invalid_request"},
		{"Basic and a body client_secret", basic(id, secret), url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}, "client_secret": {secret}}, 400, "invalid_request"},
		{"no Basic", "", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}, "client_id": {oauth.ClientIDOf(id)}, "client_secret": {secret}}, 401, "invalid_client"},
		{"a Bearer instead", "Bearer " + secret, url.Values{"grant_type": {"refresh_token"}}, 401, "invalid_client"},
		{"a secret of the wrong shape", basic(id, "tt_cs_"+oauth.Env+"_short"), url.Values{"grant_type": {"refresh_token"}}, 401, "invalid_client"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := &fakeAuthority{accept: true}
			h, _ := newSurface(t, a, oauthapi.Config{})
			rec := post(h, oauthapi.TokenPath, tc.auth, tc.body, "203.0.113.5:1")
			if rec.Code != tc.status || errorWord(t, rec) != tc.word {
				t.Fatalf("%d %q, want %d %q", rec.Code, rec.Body.String(), tc.status, tc.word)
			}
			if tc.status == 401 && rec.Header().Get("WWW-Authenticate") != `Basic realm="taptime"` {
				t.Fatal("a 401 without WWW-Authenticate: Basic")
			}
			if tc.word != "invalid_grant" && a.calls.Load() != 0 {
				t.Fatal("a refusal of the request's shape reached the resolver")
			}
		})
	}
}

// TestOAuthToken_TheFormsRules: a JSON body, a query string, a repeated parameter, a missing or
// unknown grant_type, a missing code or verifier, an empty or unknown scope.
func TestOAuthToken_TheFormsRules(t *testing.T) {
	id := uuid.New()
	auth := basic(id, mint(t, oauth.KindSecret).RevealToTheOperatorOnce())
	a := &fakeAuthority{accept: true}
	h, _ := newSurface(t, a, oauthapi.Config{})
	req := httptest.NewRequest(http.MethodPost, oauthapi.TokenPath, strings.NewReader(`{"grant_type":"refresh_token"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", auth)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 400 || errorWord(t, rec) != "invalid_request" {
		t.Fatalf("a JSON body: %d", rec.Code)
	}
	if rec := post(h, oauthapi.TokenPath+"?code=x", auth, url.Values{"grant_type": {"authorization_code"}}, "203.0.113.5:1"); rec.Code != 400 || errorWord(t, rec) != "invalid_request" {
		t.Fatalf("a query string: %d", rec.Code)
	}
	for name, c := range map[string]struct {
		body url.Values
		word string
	}{
		"a repeated parameter":   {url.Values{"grant_type": {"refresh_token", "refresh_token"}, "refresh_token": {"x"}}, "invalid_request"},
		"no grant_type":          {url.Values{"refresh_token": {"x"}}, "invalid_request"},
		"password":               {url.Values{"grant_type": {"password"}}, "unsupported_grant_type"},
		"client_credentials":     {url.Values{"grant_type": {"client_credentials"}}, "unsupported_grant_type"},
		"no code":                {url.Values{"grant_type": {"authorization_code"}, "code_verifier": {"v"}}, "invalid_request"},
		"no verifier":            {url.Values{"grant_type": {"authorization_code"}, "code": {"c"}}, "invalid_request"},
		"no refresh token":       {url.Values{"grant_type": {"refresh_token"}}, "invalid_request"},
		"an empty scope":         {url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}, "scope": {""}}, "invalid_scope"},
		"a scope of spaces":      {url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}, "scope": {"  "}}, "invalid_scope"},
		"an unknown scope":       {url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}, "scope": {"webhooks:manage"}}, "invalid_scope"},
		"a revoke with no token": {nil, "invalid_request"},
	} {
		t.Run(name, func(t *testing.T) {
			path := oauthapi.TokenPath
			if c.body == nil {
				path = oauthapi.RevokePath
			}
			rec := post(h, path, auth, c.body, "203.0.113.5:1")
			if rec.Code != 400 || errorWord(t, rec) != c.word {
				t.Fatalf("%d %q, want 400 %q", rec.Code, rec.Body.String(), c.word)
			}
		})
	}
}

// TestOAuthToken_TheAddressBucketsExactBound (ADR 0047 §8.7, ORTA-A, -race): 2 x limit concurrent
// requests with refused credentials from one address reach the resolver EXACTLY limit times; the rest
// are 429 temporarily_unavailable with Retry-After; and accepted credentials give their unit back --
// limit x 3 sequential successes from the same address never meet the address bucket.
func TestOAuthToken_TheAddressBucketsExactBound(t *testing.T) {
	const limit = 60
	id := uuid.New()
	auth := basic(id, mint(t, oauth.KindSecret).RevealToTheOperatorOnce())
	refused := &fakeAuthority{accept: false}
	h, _ := newSurface(t, refused, oauthapi.Config{})
	var wg sync.WaitGroup
	var tooMany, unauthorized atomic.Int64
	for i := 0; i < 2*limit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := post(h, oauthapi.TokenPath, auth, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}}, "198.51.100.7:1")
			switch rec.Code {
			case http.StatusTooManyRequests:
				if rec.Header().Get("Retry-After") == "" {
					t.Error("a 429 without Retry-After")
				}
				if w := errorWordNoT(rec); w != "temporarily_unavailable" {
					t.Errorf("the 429's word is %q", w)
				}
				tooMany.Add(1)
			case http.StatusUnauthorized:
				unauthorized.Add(1)
			default:
				t.Errorf("status %d", rec.Code)
			}
		}()
	}
	wg.Wait()
	if refused.calls.Load() != limit || tooMany.Load() != limit || unauthorized.Load() != limit {
		t.Fatalf("%d resolver calls, %d 429s, %d 401s; want %d each", refused.calls.Load(), tooMany.Load(), unauthorized.Load(), limit)
	}
	// The revocation endpoint shares the bucket: the address is full there too.
	if rec := post(h, oauthapi.RevokePath, auth, url.Values{"token": {"x"}}, "198.51.100.7:1"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the revocation from a full address: %d", rec.Code)
	}

	accepted := &fakeAuthority{accept: true}
	h2, _ := newSurface(t, accepted, oauthapi.Config{})
	for i := 0; i < 3*limit; i++ {
		if rec := post(h2, oauthapi.RevokePath, auth, url.Values{"token": {"x"}}, "198.51.100.8:1"); rec.Code != http.StatusOK {
			t.Fatalf("success %d answered %d: an accepted credential kept its address unit", i+1, rec.Code)
		}
	}
	if accepted.revoked != 3*limit {
		t.Fatalf("%d revocations ran", accepted.revoked)
	}
}

// TestOAuthToken_TheClientBucket: an accepted client past its own bucket is 429, from any address.
func TestOAuthToken_TheClientBucket(t *testing.T) {
	id := uuid.New()
	auth := basic(id, mint(t, oauth.KindSecret).RevealToTheOperatorOnce())
	h, _ := newSurface(t, &fakeAuthority{accept: true}, oauthapi.Config{ClientLimit: 5})
	for i := 0; i < 5; i++ {
		if rec := post(h, oauthapi.RevokePath, auth, url.Values{"token": {"x"}}, "198.51.100."+string(rune('1'+i))+":1"); rec.Code != 200 {
			t.Fatalf("request %d: %d", i+1, rec.Code)
		}
	}
	rec := post(h, oauthapi.RevokePath, auth, url.Values{"token": {"x"}}, "198.51.100.9:1")
	if rec.Code != http.StatusTooManyRequests || errorWord(t, rec) != "temporarily_unavailable" || rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("past the client bucket: %d %v", rec.Code, rec.Header())
	}
}

// TestOAuthToken_OurFailureIs503AndTheWorkRunsUnderD: a database failure is 503 temporarily_unavailable
// (the address unit given back); a panic is 503 too; the authority is handed a context bounded by D.
func TestOAuthToken_OurFailureIs503AndTheWorkRunsUnderD(t *testing.T) {
	id := uuid.New()
	auth := basic(id, mint(t, oauth.KindSecret).RevealToTheOperatorOnce())
	failing := &fakeAuthority{failWith: context.DeadlineExceeded}
	h, logs := newSurface(t, failing, oauthapi.Config{AddressLimit: 1})
	for i := 0; i < 3; i++ {
		rec := post(h, oauthapi.TokenPath, auth, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}}, "192.0.2.1:1")
		if rec.Code != http.StatusServiceUnavailable || errorWord(t, rec) != "temporarily_unavailable" {
			t.Fatalf("our failure %d: %d", i+1, rec.Code)
		}
	}
	if !strings.Contains(logs.String(), "authenticating the client failed") {
		t.Fatal("the failure was not logged")
	}
	if d := failing.deadline; d <= 0 || d > oauthapi.Deadline {
		t.Fatalf("the work's deadline is %v away, want within D (%v)", d, oauthapi.Deadline)
	}
	panicking := &fakeAuthority{accept: true, exchange: func() (oauth.Tokens, error) { panic("boom") }}
	h2, _ := newSurface(t, panicking, oauthapi.Config{})
	rec := post(h2, oauthapi.TokenPath, auth, url.Values{"grant_type": {"authorization_code"}, "code": {"c"}, "code_verifier": {"v"}}, "192.0.2.2:1")
	if rec.Code != http.StatusServiceUnavailable || errorWord(t, rec) != "temporarily_unavailable" {
		t.Fatalf("a panic: %d", rec.Code)
	}
	ended := &fakeAuthority{accept: true, exchange: func() (oauth.Tokens, error) { return oauth.Tokens{}, oauth.ErrEnded }}
	h3, _ := newSurface(t, ended, oauthapi.Config{})
	if rec := post(h3, oauthapi.TokenPath, auth, url.Values{"grant_type": {"authorization_code"}, "code": {"c"}, "code_verifier": {"v"}}, "192.0.2.3:1"); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("a rolled-back exchange: %d", rec.Code)
	}
}

func errorWordNoT(rec *httptest.ResponseRecorder) string {
	var b struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	return b.Error
}

// TestOAuthAuthorize_TheFrontDoorRedirectsAndTouchesNothing (ADR 0047 §3, O-3): GET /oauth/authorize is
// a 303 to /admin/oauth/authorize with the same query, no cookie written, no authority asked.
func TestOAuthAuthorize_TheFrontDoorRedirectsAndTouchesNothing(t *testing.T) {
	a := &fakeAuthority{}
	h, _ := newSurface(t, a, oauthapi.Config{})
	q := "client_id=app_x&state=a%20b&scope=timesheets%3Aread"
	req := httptest.NewRequest(http.MethodGet, oauthapi.AuthorizePath+"?"+q, nil)
	req.AddCookie(&http.Cookie{Name: "tappa_admin_session", Value: "x"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != oauthapi.ConsentPath+"?"+q || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %q", rec.Code, rec.Header().Get("Location"))
	}
	if len(rec.Result().Cookies()) != 0 || a.calls.Load() != 0 {
		t.Fatal("the front door wrote a cookie or asked the authority")
	}
}

// TestOAuthToken_TheAuthorizationHeaderIsDeletedFirst (§5.1): on both endpoints the Authorization header
// is gone from the request once the handler returns -- also when the body was refused before the
// client was looked at (a JSON body, a query string) -- so nothing that runs after it can carry it.
func TestOAuthToken_TheAuthorizationHeaderIsDeletedFirst(t *testing.T) {
	id := uuid.New()
	auth := basic(id, mint(t, oauth.KindSecret).RevealToTheOperatorOnce())
	h, _ := newSurface(t, &fakeAuthority{accept: true}, oauthapi.Config{})
	for _, c := range []struct{ path, ctype, body string }{
		{oauthapi.TokenPath, "application/x-www-form-urlencoded", "grant_type=refresh_token&refresh_token=x"},
		{oauthapi.TokenPath, "application/json", `{"grant_type":"refresh_token"}`},
		{oauthapi.TokenPath + "?x=1", "application/x-www-form-urlencoded", "grant_type=refresh_token"},
		{oauthapi.RevokePath, "application/x-www-form-urlencoded", "token=x"},
		{oauthapi.RevokePath, "text/plain", "token=x"},
	} {
		req := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(c.body))
		req.Header.Set("Content-Type", c.ctype)
		req.Header.Set("Authorization", auth)
		req.RemoteAddr = "192.0.2.40:1"
		var seen []string
		after := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h.ServeHTTP(w, r)
			seen = r.Header.Values("Authorization")
		})
		after.ServeHTTP(httptest.NewRecorder(), req)
		if len(seen) != 0 || req.Header.Get("Authorization") != "" {
			t.Errorf("%s (%s): the Authorization header survived the handler", c.path, c.ctype)
		}
	}
}
