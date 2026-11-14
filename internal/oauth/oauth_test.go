package oauth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// testKey is a fixed, fake TAPPA_OAUTH_HMAC_KEY for the pure tests.
var testKey = bytes.Repeat([]byte("o"), 32)

func mustHasher(t *testing.T) *Hasher {
	t.Helper()
	h, err := NewHasher(testKey)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// TestOAuthValue_MintShapeAndKind: every kind mints tt_<kind>_<env>_ + 43 canonical base64url
// characters, is recognised as its own kind and as no other, and a value of the other build's
// environment word, a truncated or non-canonical body, or an unknown kind is refused -- no database.
func TestOAuthValue_MintShapeAndKind(t *testing.T) {
	other := map[string]string{"live": "test", "test": "live"}[Env]
	for _, k := range kinds {
		v, err := Mint(k)
		if err != nil {
			t.Fatal(err)
		}
		raw := v.reveal()
		if !strings.HasPrefix(raw, "tt_"+string(k)+"_"+Env+"_") || len(raw) != len(Prefix(k))+43 {
			t.Fatalf("%s minted a value of the wrong shape (length %d)", k, len(raw))
		}
		got, ok := v.KindOf()
		if !ok || got != k {
			t.Fatalf("%s is read back as %q, %v", k, got, ok)
		}
		for _, j := range kinds {
			if j != k && v.Is(j) {
				t.Fatalf("a %s value has the shape of a %s", k, j)
			}
		}
		body := raw[len(Prefix(k)):]
		for _, bad := range []string{
			"tt_" + string(k) + "_" + other + "_" + body, // the other build's word
			raw[:len(raw)-1],                    // short
			raw + "A",                           // long
			Prefix(k) + strings.Repeat("*", 43), // not base64url
			Prefix(k) + body[:42] + "B",         // a non-canonical last character (when it differs)
		} {
			if bad == raw {
				continue
			}
			if ParseValue(bad).Is(k) {
				t.Errorf("%s accepted a refused shape", k)
			}
		}
	}
	if _, err := Mint(Kind("zz")); err == nil {
		t.Fatal("an unknown kind minted")
	}
}

// TestOAuthValue_PrintsNothingOfItself: a Value and a Digest print a placeholder through fmt, slog
// and encoding/json -- the value never reaches a log line or a JSON body by accident (ADR 0047 §7.2).
func TestOAuthValue_PrintsNothingOfItself(t *testing.T) {
	v, err := Mint(KindRefresh)
	if err != nil {
		t.Fatal(err)
	}
	d, err := mustHasher(t).Hash(KindRefresh, v)
	if err != nil {
		t.Fatal(err)
	}
	secret, hash := v.reveal(), d.reveal()
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("x", "v", v, "d", d, "s", struct{ V Value }{v})
	j, _ := json.Marshal(map[string]any{"v": v, "d": d})
	out := fmt.Sprintf("%v %+v %#v %s %q %x", v, v, v, d, d, struct{ D Digest }{d}) + buf.String() + string(j)
	for _, leak := range []string{secret, hash, secret[len(Prefix(KindRefresh)):]} {
		if strings.Contains(out, leak) {
			t.Fatal("a rendering path printed the value or its hash")
		}
	}
	if !strings.Contains(out, redacted) || !strings.Contains(out, redactedDigest) {
		t.Fatal("the placeholders are missing")
	}
}

// TestOAuthHasher_LabelsSeparateTheKinds: one string hashes differently per kind (the domain labels),
// the hash is the 64-hex shape every column CHECKs, and a short key is refused.
func TestOAuthHasher_LabelsSeparateTheKinds(t *testing.T) {
	h := mustHasher(t)
	v := ParseValue("tt_at_live_" + strings.Repeat("A", 43))
	seen := map[string]Kind{}
	for _, k := range kinds {
		d, err := h.Hash(k, v)
		if err != nil {
			t.Fatal(err)
		}
		if len(d.reveal()) != 64 || strings.Trim(d.reveal(), "0123456789abcdef") != "" {
			t.Fatalf("%s's hash is not 64 lower-case hex", k)
		}
		if prev, dup := seen[d.reveal()]; dup {
			t.Fatalf("%s and %s hash alike", prev, k)
		}
		seen[d.reveal()] = k
	}
	if _, err := NewHasher(make([]byte, 16)); err == nil {
		t.Fatal("a 16-byte key was accepted")
	}
}

// TestOAuthValue_SecretHint is oauth_clients.secret_hint's shape: the prefix and four characters.
func TestOAuthValue_SecretHint(t *testing.T) {
	v, _ := Mint(KindSecret)
	if h := v.SecretHint(); h != v.reveal()[:len(Prefix(KindSecret))+4] {
		t.Fatalf("the hint is %d characters", len(h))
	}
	a, _ := Mint(KindAccess)
	if a.SecretHint() != "" {
		t.Fatal("an access token has a secret hint")
	}
}

// pkcePair is a verifier and its S256 challenge.
func pkcePair(verifier string) (string, string) {
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// TestOAuthPKCE_S256 is RFC 7636's S256 with the RFC's own Appendix B vector, a mismatch, and the
// verifier's shape rule (43..128 of the unreserved set).
func TestOAuthPKCE_S256(t *testing.T) {
	// RFC 7636 Appendix B.
	if !VerifyS256("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk", "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM") {
		t.Fatal("RFC 7636 Appendix B's vector does not verify")
	}
	v, c := pkcePair(strings.Repeat("a", 43))
	tests := []struct {
		name                string
		verifier, challenge string
		want                bool
	}{
		{"match", v, c, true},
		{"another verifier", strings.Repeat("b", 43), c, false},
		{"a verifier of 42", strings.Repeat("a", 42), c, false},
		{"a verifier of 129", strings.Repeat("a", 129), c, false},
		{"a verifier with a space", strings.Repeat("a", 42) + " ", c, false},
		{"the challenge as the verifier (plain)", c, c, false},
		{"a malformed challenge", v, c[:42], false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := VerifyS256(tc.verifier, tc.challenge); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestOAuthScope_IsAClosedSet: a set (order and repeats do not matter), canonical, never empty,
// webhooks:manage and every word outside the five refused, no implied inclusion.
func TestOAuthScope_IsAClosedSet(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
		err  error
	}{
		{"one", "timesheets:read", []string{"timesheets:read"}, nil},
		{"order and repeats", "employees:write timesheets:read employees:write", []string{"employees:write", "timesheets:read"}, nil},
		{"extra spaces", "  punches:read  ", []string{"punches:read"}, nil},
		{"empty", "", nil, ErrEmptyScope},
		{"only spaces", "   ", nil, ErrEmptyScope},
		{"webhooks:manage", "webhooks:manage", nil, ErrUnknownScope},
		{"unknown beside known", "timesheets:read admin", nil, ErrUnknownScope},
		{"a tab is not a separator", "timesheets:read\temployees:read", nil, ErrUnknownScope},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseScope(tc.raw)
			if err != tc.err || strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("got %v, %v; want %v, %v", got, err, tc.want, tc.err)
			}
		})
	}
	if SubsetOf([]string{"employees:read"}, []string{"employees:write"}) {
		t.Fatal("employees:write implies employees:read")
	}
	for _, s := range KnownScopes {
		if ScopeLabel(s) == "" {
			t.Errorf("%s has no consent sentence", s)
		}
	}
}

// TestOAuthState_V7 is the state rule: up to 1024 VSCHAR bytes.
func TestOAuthState_V7(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  bool
	}{
		{"absent", "", true},
		{"1024", strings.Repeat("x", 1024), true},
		{"1025", strings.Repeat("x", 1025), false},
		{"space and tilde", " ~", true},
		{"a control byte", "a\x1fb", false},
		{"DEL", "a\x7f", false},
		{"non-ASCII", "ç", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if ValidState(tc.state) != tc.want {
				t.Fatalf("want %v", tc.want)
			}
		})
	}
}

// TestOAuthClientID_OneSpelling: app_ + a canonical lower-case uuid and nothing else.
func TestOAuthClientID_OneSpelling(t *testing.T) {
	id := uuid.New()
	if got, ok := ParseClientID(ClientIDOf(id)); !ok || got != id {
		t.Fatal("the canonical client_id does not parse")
	}
	for _, bad := range []string{
		id.String(), "app_" + strings.ToUpper(id.String()), "APP_" + id.String(), "app_" + id.String() + " ",
		"app_{" + id.String() + "}", "app_" + strings.ReplaceAll(id.String(), "-", ""), "",
	} {
		if _, ok := ParseClientID(bad); ok {
			t.Errorf("%q parsed", bad)
		}
	}
}

// TestOAuthBasic_ParsesOneShape: Basic base64(client_id:secret), the scheme in any case, the values
// form-decoded; anything else is not a credential.
func TestOAuthBasic_ParsesOneShape(t *testing.T) {
	id := uuid.New()
	secret, _ := Mint(KindSecret)
	enc := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	good := ClientIDOf(id) + ":" + secret.reveal()
	tests := []struct {
		name   string
		header string
		ok     bool
	}{
		{"canonical", "Basic " + enc(good), true},
		{"lower-case scheme", "basic " + enc(good), true},
		{"form-encoded values", "Basic " + enc(url.QueryEscape(ClientIDOf(id))+":"+url.QueryEscape(secret.reveal())), true},
		{"bearer", "Bearer " + enc(good), false},
		{"no colon", "Basic " + enc(ClientIDOf(id)), false},
		{"empty secret", "Basic " + enc(ClientIDOf(id)+":"), false},
		{"not base64", "Basic !!!", false},
		{"a bare uuid as the id", "Basic " + enc(id.String()+":"+secret.reveal()), false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, v, ok := ParseBasic(tc.header)
			if ok != tc.ok {
				t.Fatalf("ok %v, want %v", ok, tc.ok)
			}
			if ok && (got != id || v.reveal() != secret.reveal()) {
				t.Fatal("the parsed credential is not the one sent")
			}
		})
	}
}

// TestOAuthRedirect_RegistrationRule is the Go half of §10.2's rule, never looser than the SQL half
// (registryPattern), and the loopback form refused in the production build (the sandbox build's twin
// is redirect_sandbox_test.go).
func TestOAuthRedirect_RegistrationRule(t *testing.T) {
	tests := []struct {
		name string
		uri  string
		want error
	}{
		{"the Malta Calculator callback", "https://maltacalculator.com/api/integrations/taptime/callback", nil},
		{"port 443", "https://maltacalculator.com:443/cb", nil},
		{"root path", "https://a.example/", nil},
		{"http", "http://example.com/cb", ErrRedirectShape},
		{"an IP literal", "https://203.0.113.7/cb", ErrRedirectShape},
		{"an IPv6 literal", "https://[2001:db8::1]/cb", ErrRedirectShape},
		{"port 8443", "https://example.com:8443/cb", ErrRedirectShape},
		{"a query", "https://example.com/cb?x=1", ErrRedirectShape},
		{"an empty query", "https://example.com/cb?", ErrRedirectShape},
		{"a fragment", "https://example.com/cb#f", ErrRedirectShape},
		{"userinfo", "https://user@example.com/cb", ErrRedirectShape},
		{"upper-case host", "https://Example.com/cb", ErrRedirectShape},
		{"no path", "https://example.com", ErrRedirectShape},
		{"a numeric last label", "https://example.123/cb", ErrRedirectShape},
		{"a single label", "https://localhost/cb", ErrRedirectShape},
		{"too long", "https://example.com/" + strings.Repeat("a", 500), ErrRedirectTooLong},
	}
	if !LoopbackRedirects {
		tests = append(tests,
			struct {
				name string
				uri  string
				want error
			}{"loopback in production", "http://localhost:3000/api/integrations/taptime/callback", ErrRedirectLoopback},
			struct {
				name string
				uri  string
				want error
			}{"127.0.0.1 in production", "http://127.0.0.1:3000/cb", ErrRedirectLoopback})
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RegistrationURIError(tc.uri); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if tc.want == nil && !registryRule.MatchString(tc.uri) {
				t.Fatal("the Go rule accepted what the SQL rule refuses")
			}
		})
	}
	ok := "https://a.example/cb"
	if RegistrationURIsError(nil) != ErrRedirectCount || RegistrationURIsError([]string{ok, ok}) != ErrRedirectDuplicate ||
		RegistrationURIsError([]string{"https://a.example/1", "https://a.example/2", "https://a.example/3", "https://a.example/4",
			"https://a.example/5", "https://a.example/6"}) != ErrRedirectCount || RegistrationURIsError([]string{ok}) != nil {
		t.Fatal("the list rule")
	}
}

// TestOAuthRedirect_ProductionV2RefusesLoopback: in the production build V2's gate refuses every URI
// that is not https:// -- even one byte-identical to a registered row (a loopback row written with
// the operator's DSN sends no code anywhere -- §10.4).
func TestOAuthRedirect_ProductionV2RefusesLoopback(t *testing.T) {
	if LoopbackRedirects {
		t.Skip("the sandbox build accepts loopback (redirect_sandbox_test.go)")
	}
	loop := "http://localhost:3000/api/integrations/taptime/callback"
	if AuthorizeURIAllowed(loop) || AuthorizeURIAllowed("http://127.0.0.1:3000/cb") {
		t.Fatal("the production V2 gate let a loopback URI through")
	}
	c := Client{ID: uuid.New(), RedirectURIs: []string{loop}, AllowedScopes: []string{ScopeTimesheetsRead}}
	_, ch := pkcePair(strings.Repeat("v", 43))
	q := url.Values{"client_id": {ClientIDOf(c.ID)}, "redirect_uri": {loop}, "response_type": {"code"},
		"code_challenge_method": {"S256"}, "code_challenge": {ch}, "scope": {ScopeTimesheetsRead}}
	a, err := Authorize(context.Background(), q.Encode(), func(context.Context, uuid.UUID) (Client, error) { return c, nil })
	if err != nil || a.Verdict != VerdictFatal {
		t.Fatalf("a registered loopback URI in production: verdict %v, %v; want fatal (no redirect)", a.Verdict, err)
	}
}

// authorizeFixture is a client and a valid request's query.
func authorizeFixture() (Client, url.Values) {
	c := Client{ID: uuid.New(), Name: "Malta Calculator", RedirectURIs: []string{"https://maltacalculator.com/api/integrations/taptime/callback"},
		AllowedScopes: []string{ScopeEmployeesRead, ScopeEmployeesWrite, ScopeTimesheetsRead}}
	_, ch := pkcePair(strings.Repeat("v", 43))
	return c, url.Values{"client_id": {ClientIDOf(c.ID)}, "redirect_uri": {c.RedirectURIs[0]}, "response_type": {"code"},
		"code_challenge_method": {"S256"}, "code_challenge": {ch}, "scope": {"timesheets:read employees:read"}, "state": {"s-1"}}
}

// TestOAuthAuthorize_V1toV7 is §4.1's table: V1-V2 failures never redirect (an unknown or disabled
// client, a redirect URI that differs by a byte -- a trailing slash, case, www., a query, a port);
// V3-V7 failures redirect with the right error and the state reflected -- except V7's, whose state
// is not reflected; a repeated client_id/redirect_uri is fatal, any other repeated parameter
// invalid_request.
func TestOAuthAuthorize_V1toV7(t *testing.T) {
	c, base := authorizeFixture()
	disabled := Client{ID: uuid.New(), RedirectURIs: c.RedirectURIs, AllowedScopes: c.AllowedScopes, Disabled: true}
	lookup := func(_ context.Context, id uuid.UUID) (Client, error) {
		switch id {
		case c.ID:
			return c, nil
		case disabled.ID:
			return disabled, nil
		}
		return Client{}, ErrNoClient
	}
	with := func(edit func(url.Values)) string {
		q := url.Values{}
		for k, v := range base {
			q[k] = append([]string(nil), v...)
		}
		edit(q)
		return q.Encode()
	}
	set := func(k, v string) func(url.Values) { return func(q url.Values) { q.Set(k, v) } }
	del := func(k string) func(url.Values) { return func(q url.Values) { q.Del(k) } }
	add := func(k, v string) func(url.Values) { return func(q url.Values) { q.Add(k, v) } }
	reg := c.RedirectURIs[0]
	tests := []struct {
		name    string
		query   string
		verdict Verdict
		errWord string
		state   string
	}{
		{"valid", with(func(url.Values) {}), VerdictOK, "", "s-1"},
		{"valid without a state", with(del("state")), VerdictOK, "", ""},
		{"V1 missing", with(del("client_id")), VerdictFatal, "", ""},
		{"V1 unknown", with(set("client_id", ClientIDOf(uuid.New()))), VerdictFatal, "", ""},
		{"V1 disabled", with(set("client_id", ClientIDOf(disabled.ID))), VerdictFatal, "", ""},
		{"V1 a bare uuid", with(set("client_id", c.ID.String())), VerdictFatal, "", ""},
		{"V1 repeated", with(add("client_id", ClientIDOf(c.ID))), VerdictFatal, "", ""},
		{"V2 missing", with(del("redirect_uri")), VerdictFatal, "", ""},
		{"V2 trailing slash", with(set("redirect_uri", reg+"/")), VerdictFatal, "", ""},
		{"V2 case", with(set("redirect_uri", strings.Replace(reg, "maltacalculator", "MaltaCalculator", 1))), VerdictFatal, "", ""},
		{"V2 www.", with(set("redirect_uri", strings.Replace(reg, "https://", "https://www.", 1))), VerdictFatal, "", ""},
		{"V2 a query added", with(set("redirect_uri", reg+"?x=1")), VerdictFatal, "", ""},
		{"V2 a port added", with(set("redirect_uri", strings.Replace(reg, ".com/", ".com:443/", 1))), VerdictFatal, "", ""},
		{"V2 repeated", with(add("redirect_uri", reg)), VerdictFatal, "", ""},
		{"V3 token", with(set("response_type", "token")), VerdictRedirectError, ErrorUnsupportedResponseType, "s-1"},
		{"V3 missing", with(del("response_type")), VerdictRedirectError, ErrorInvalidRequest, "s-1"},
		{"V3 empty", with(set("response_type", "")), VerdictRedirectError, ErrorInvalidRequest, "s-1"},
		{"V4 plain", with(set("code_challenge_method", "plain")), VerdictRedirectError, ErrorInvalidRequest, "s-1"},
		{"V4 missing (plain by default)", with(del("code_challenge_method")), VerdictRedirectError, ErrorInvalidRequest, "s-1"},
		{"V5 short", with(set("code_challenge", "abc")), VerdictRedirectError, ErrorInvalidRequest, "s-1"},
		{"V5 missing", with(del("code_challenge")), VerdictRedirectError, ErrorInvalidRequest, "s-1"},
		{"V6 missing", with(del("scope")), VerdictRedirectError, ErrorInvalidScope, "s-1"},
		{"V6 empty", with(set("scope", " ")), VerdictRedirectError, ErrorInvalidScope, "s-1"},
		{"V6 webhooks:manage", with(set("scope", "webhooks:manage")), VerdictRedirectError, ErrorInvalidScope, "s-1"},
		{"V6 beyond the client's set", with(set("scope", "punches:read")), VerdictRedirectError, ErrorInvalidScope, "s-1"},
		{"V7 too long -- not reflected", with(set("state", strings.Repeat("x", 1025))), VerdictRedirectError, ErrorInvalidRequest, ""},
		{"V7 a control byte -- not reflected", with(set("state", "a\nb")), VerdictRedirectError, ErrorInvalidRequest, ""},
		{"V7 1024 bytes passes", with(set("state", strings.Repeat("x", 1024))), VerdictOK, "", strings.Repeat("x", 1024)},
		{"a repeated scope parameter", with(add("scope", "timesheets:read")), VerdictRedirectError, ErrorInvalidRequest, "s-1"},
		{"a repeated state -- not reflected", with(add("state", "s-2")), VerdictRedirectError, ErrorInvalidRequest, ""},
		{"a malformed query", "client_id=%zz", VerdictFatal, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, err := Authorize(context.Background(), tc.query, lookup)
			if err != nil {
				t.Fatal(err)
			}
			if a.Verdict != tc.verdict || a.Error != tc.errWord || a.State != tc.state {
				t.Fatalf("verdict %v error %q state %q; want %v %q %q", a.Verdict, a.Error, a.State, tc.verdict, tc.errWord, tc.state)
			}
			if a.Verdict != VerdictFatal && a.RedirectURI != reg {
				t.Fatal("the redirect target is not the registered URI")
			}
			if a.Verdict == VerdictOK && (a.Request.ClientID != c.ID || strings.Join(a.Request.Scopes, " ") != "employees:read timesheets:read" && tc.name == "valid") {
				t.Fatal("the request is not the one asked for")
			}
		})
	}
}

// TestOAuthAuthorize_ErrorRedirectCarriesIssAndOnlyAValidState: the error redirect is the registered
// URI with error, iss and -- only when given -- state; a code redirect is code, state, iss.
func TestOAuthAuthorize_ErrorRedirectCarriesIssAndOnlyAValidState(t *testing.T) {
	got, _ := url.Parse(ErrorRedirect("https://a.example/cb", ErrorAccessDenied, "st & x", "https://taptime.mt"))
	q := got.Query()
	if got.Path != "/cb" || q.Get("error") != ErrorAccessDenied || q.Get("state") != "st & x" || q.Get("iss") != "https://taptime.mt" {
		t.Fatalf("the error redirect is %v", got)
	}
	none, _ := url.Parse(ErrorRedirect("https://a.example/cb", ErrorInvalidRequest, "", "https://taptime.mt"))
	if _, ok := none.Query()["state"]; ok {
		t.Fatal("an absent state was reflected")
	}
	code, _ := Mint(KindCode)
	cr, _ := url.Parse(CodeRedirect("https://a.example/cb", code, "s", "https://taptime.mt"))
	if cr.Query().Get("code") != code.reveal() || cr.Query().Get("state") != "s" || cr.Query().Get("iss") != "https://taptime.mt" || len(cr.Query()) != 3 {
		t.Fatalf("the code redirect carries %v", cr.Query())
	}
}

// TestOAuthRequest_CookieSealsAndRefusesTampering: the pending request round-trips; a changed byte,
// another key, an expired or future cookie, a field outside its rule, and an extra field are refused;
// the digest (rd) is per cookie value.
func TestOAuthRequest_CookieSealsAndRefusesTampering(t *testing.T) {
	h := mustHasher(t)
	_, ch := pkcePair(strings.Repeat("v", 43))
	now := time.Unix(1_900_000_000, 0)
	r := Request{ClientID: uuid.New(), RedirectURI: "https://a.example/cb", ResponseType: "code", Method: "S256", Challenge: ch,
		Scopes: []string{"employees:read", "timesheets:read"}, State: "st", Created: now.Unix()}
	v, err := h.SealRequest(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := h.OpenRequest(v, now.Add(9*time.Minute))
	if err != nil || got.ClientID != r.ClientID || got.State != "st" {
		t.Fatalf("round trip: %v", err)
	}
	if _, err := h.OpenRequest(v, now.Add(11*time.Minute)); err != ErrRequestExpired {
		t.Fatalf("an 11-minute cookie: %v", err)
	}
	if _, err := h.OpenRequest(v, now.Add(-2*time.Minute)); err != ErrRequestExpired {
		t.Fatalf("a cookie from the future: %v", err)
	}
	other, _ := NewHasher(bytes.Repeat([]byte("p"), 32))
	if _, err := other.OpenRequest(v, now); err != ErrRequestInvalid {
		t.Fatalf("another key: %v", err)
	}
	body, sig, _ := strings.Cut(v, ".")
	raw, _ := base64.RawURLEncoding.DecodeString(body)
	forged := strings.Replace(string(raw), "https://a.example/cb", "https://b.example/cb", 1)
	if _, err := h.OpenRequest(base64.RawURLEncoding.EncodeToString([]byte(forged))+"."+sig, now); err != ErrRequestInvalid {
		t.Fatalf("a changed redirect: %v", err)
	}
	for name, edit := range map[string]func(*Request){
		"plain":             func(r *Request) { r.Method = "plain" },
		"token":             func(r *Request) { r.ResponseType = "token" },
		"a short challenge": func(r *Request) { r.Challenge = "x" },
		"unsorted scopes":   func(r *Request) { r.Scopes = []string{"timesheets:read", "employees:read"} },
		"no scopes":         func(r *Request) { r.Scopes = nil },
		"a bad state":       func(r *Request) { r.State = "a\nb" },
	} {
		bad := r
		edit(&bad)
		sealed, _ := h.SealRequest(bad)
		if _, err := h.OpenRequest(sealed, now); err != ErrRequestInvalid {
			t.Errorf("%s: %v", name, err)
		}
	}
	if h.RequestDigest(v) == h.RequestDigest(v+"x") || !h.SameRequest(v, h.RequestDigest(v)) || h.SameRequest(v, h.RequestDigest(v)[1:]) {
		t.Fatal("the digest")
	}
}

// TestOAuthRequest_TheCookieHasASizeBound (the 2nd phase's review, L3): the pending request is encoded
// without HTML escaping (a state of 1024 '<' costs 1024 bytes, not 6144) and a sealed value that would
// pass MaxRequestCookieBytes with its name is ErrRequestTooLarge. The worst case V2 and V7 admit fits;
// one past V7's bound does not.
func TestOAuthRequest_TheCookieHasASizeBound(t *testing.T) {
	h := mustHasher(t)
	_, ch := pkcePair(strings.Repeat("v", 43))
	long := "https://a.example/" + strings.Repeat("p", 512-len("https://a.example/"))
	r := Request{ClientID: uuid.New(), RedirectURI: long, ResponseType: "code", Method: "S256", Challenge: ch,
		Scopes: []string{"employees:read", "timesheets:read"}, Created: time.Now().Unix()}
	r.State = strings.Repeat("<", MaxStateBytes)
	v, err := h.SealRequest(r)
	if err != nil {
		t.Fatalf("a 1024-byte state of '<' (not HTML-escaped) is refused: %v", err)
	}
	if len(RequestCookieName)+1+len(v) > MaxRequestCookieBytes {
		t.Fatal("PREMISE: the sealed value is over the bound")
	}
	if got, err := h.OpenRequest(v, time.Now()); err != nil || got.State != r.State {
		t.Fatalf("the round trip of an unescaped state: %v", err)
	}
	// The worst case V2 and V7 admit -- a 512-byte URI and 1024 bytes JSON doubles -- fits (measured:
	// about 3745 bytes with the name); the bound is the guard that keeps it so if a rule widens.
	r.State = strings.Repeat(`"`, MaxStateBytes)
	if v, err := h.SealRequest(r); err != nil || len(RequestCookieName)+1+len(v) > MaxRequestCookieBytes {
		t.Fatalf("the worst admitted case: %v", err)
	}
	r.State = strings.Repeat(`"`, MaxStateBytes+100)
	if _, err := h.SealRequest(r); err != ErrRequestTooLarge {
		t.Fatalf("past the bound: %v, want ErrRequestTooLarge", err)
	}
}
