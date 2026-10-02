package operator_test

import (
	"bytes"
	"context"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// The searched GROUPS. Each is bound to the never-log items below (or, G15, to none).
const (
	gPassword    = "G1 password"
	gCode        = "G2 TOTP code"
	gCodeNear    = "G3 TOTP code, ±1 step"
	gSecret      = "G4 TOTP secret"
	gLinkToken   = "G5 enrollment token"
	gLinkHash    = "G6 enrollment token hash"
	gSession     = "G7 session token"
	gSessionHash = "G8 session token hash"
	gChallenge   = "G9 login challenge"
	gBlob        = "G10 pending blob"
	gEnvelope    = "G11 stored TOTP envelope"
	gDigest      = "G12 password digest"
	gEmail       = "G13 operator address"
	gKeys        = "G14 server key (TOTP KEK, token HMAC key)"
	gAddr        = "G15 client address" // this package's own claim (routes.go rateKey); on no never-log list
)

// neverLog is the CLOSED criterion: each never-log item of CLAUDE.md §7, ADR 0020 §5 and
// ADR 0021 §3.5 that exists on this surface, with the group(s) standing for it. The test
// requires at least one member in EACH group of EACH item, and the table's length is a
// pinned literal. Items not on this surface, by name: CMAC, the invite code, a full GPS
// coordinate, the read ticket (OP-10).
var neverLog = []struct {
	item   string
	groups []string
}{
	{"N1 operator session token (CLAUDE.md §7; ADR 0020 §5)", []string{gSession}},
	{"N2 its hash (ADR 0020 §5)", []string{gSessionHash}},
	{"N3 TOTP code, the current one and the ±1 window's (CLAUDE.md §7; ADR 0020 §5; ADR 0021 §3.5)", []string{gCode, gCodeNear}},
	{"N4 TOTP secret (CLAUDE.md §7; ADR 0020 §5)", []string{gSecret}},
	{"N5 enrollment token (CLAUDE.md §7; ADR 0020 §5; ADR 0021 §3.5)", []string{gLinkToken}},
	{"N6 its hash (ADR 0020 §5; ADR 0021 §3.5)", []string{gLinkHash}},
	{"N7 password, old and new (ADR 0020 §5)", []string{gPassword}},
	{"N8 password digest (ADR 0020 §5)", []string{gDigest}},
	{"N9 TOTP envelope: the stored one and the page's pending blob (ADR 0020 §5)", []string{gEnvelope, gBlob}},
	{"N10 key material: the TOTP KEK (CLAUDE.md §7 'AES anahtarı') and the token HMAC key (ADR 0020 §2)", []string{gKeys}},
	{"N11 login challenge: a post-password bearer credential (operatorauth/challenge.go)", []string{gChallenge}},
	{"N12 operator address (ADR 0020 §5: an operator is named by id, not address)", []string{gEmail}},
}

const neverLogItems = 12

// minNeedle is the shortest needle searched: the six-digit codes. No text this surface
// writes carries a run of six digits of its own (its numbers are statuses, sizes and
// durations of at most four digits), so a code found was put there.
const minNeedle = 6

// renderings are the SEARCHED forms, numbered; each turns a member into ONE needle. Each
// is paired BY NAME with an independent builder in leakBuilders -- the form produced by a
// different construction (fmt's verb, the encoder's streaming writer, net/url's form
// encoder, the operator pages' own templ renderer) -- and every needle must occur in its builder's text for every
// member, or the test is red. A rendering emptied, made nil or reduced to the raw value is
// caught by the control members whose forms differ from the raw value (G1's special
// password; the hex and base64 forms of every member).
var renderings = []struct {
	name string
	fn   func(v string) string
}{
	{"R1 raw", func(v string) string { return v }},
	{"R2 %q inside", func(v string) string { q := strconv.Quote(v); return q[1 : len(q)-1] }},
	{"R3 JSON string inside", func(v string) string { j, _ := json.Marshal(v); return string(j[1 : len(j)-1]) }},
	{"R4 URL query-escaped", func(v string) string { return url.QueryEscape(v) }},
	{"R5 HTML-escaped", func(v string) string { return html.EscapeString(v) }},
	{"R6 hex", func(v string) string { return hex.EncodeToString([]byte(v)) }},
	{"R7 base64 std, padded", func(v string) string { return base64.StdEncoding.EncodeToString([]byte(v)) }},
	{"R8 base64 std, raw", func(v string) string { return base64.RawStdEncoding.EncodeToString([]byte(v)) }},
	{"R9 base64 url, padded", func(v string) string { return base64.URLEncoding.EncodeToString([]byte(v)) }},
	{"R10 base64 url, raw", func(v string) string { return base64.RawURLEncoding.EncodeToString([]byte(v)) }},
}

// leakBuilders are the independent constructions, by rendering name.
func leakBuilders(t *testing.T) map[string]func(v string) string {
	t.Helper()
	stream := func(e *base64.Encoding, v string) string {
		var b bytes.Buffer
		w := base64.NewEncoder(e, &b)
		if _, err := io.WriteString(w, v); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	return map[string]func(v string) string{
		"R1 raw":       func(v string) string { return fmt.Sprintf("<%s>", v) },
		"R2 %q inside": func(v string) string { return fmt.Sprintf("%q", v) },
		"R3 JSON string inside": func(v string) string {
			var b bytes.Buffer
			if err := json.NewEncoder(&b).Encode(v); err != nil {
				t.Fatal(err)
			}
			return b.String()
		},
		"R4 URL query-escaped": func(v string) string { return url.Values{"k": {v}}.Encode() },
		// R5's builder is the page renderer itself: the value as a problem page's title,
		// written by the templ-generated code of the operator screens.
		"R5 HTML-escaped": func(v string) string {
			var b bytes.Buffer
			if err := operatorpages.Problem(operatorpages.ProblemView{Title: v, Message: "m"}).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			return b.String()
		},
		"R6 hex":                func(v string) string { return fmt.Sprintf("%x", []byte(v)) },
		"R7 base64 std, padded": func(v string) string { return stream(base64.StdEncoding, v) },
		"R8 base64 std, raw":    func(v string) string { return stream(base64.RawStdEncoding, v) },
		"R9 base64 url, padded": func(v string) string { return stream(base64.URLEncoding, v) },
		"R10 base64 url, raw":   func(v string) string { return stream(base64.RawURLEncoding, v) },
	}
}

// leakSet is the searched members.
type leakSet struct {
	members []leakMember
}

type leakMember struct{ group, value string }

func (s *leakSet) add(group string, vals ...string) {
	for _, v := range vals {
		if v != "" {
			s.members = append(s.members, leakMember{group: group, value: v})
		}
	}
}

func (s *leakSet) inGroup(g string) bool {
	for _, m := range s.members {
		if m.group == g {
			return true
		}
	}
	return false
}

type leakHit struct {
	group, value, rendering string
}

// hits is every member found in text under any rendering.
func (s *leakSet) hits(text string) []leakHit {
	var out []leakHit
	for _, m := range s.members {
		for _, r := range renderings {
			if n := r.fn(m.value); len(n) >= minNeedle && strings.Contains(text, n) {
				out = append(out, leakHit{group: m.group, value: m.value, rendering: r.name})
			}
		}
	}
	return out
}

// leakResult is one arm's four surfaces.
type leakResult struct {
	w               *httptest.ResponseRecorder
	process, access string
}

// leakSurfaces are the SCANNED surfaces, numbered. checkSurfaces holds the list to its
// four names, and the surface control requires a canary on each to be found by scanArm.
var leakSurfaces = []struct {
	name string
	text func(r leakResult) string
}{
	{"S1 process log", func(r leakResult) string { return r.process }},
	{"S2 access log", func(r leakResult) string { return r.access }},
	{"S3 response body", func(r leakResult) string { return r.w.Body.String() }},
	{"S4 response headers", func(r leakResult) string {
		// The headers AT WriteHeader -- the recorder's snapshot (equal to the wire's header
		// section on the three classes op8r5_test.go compares; a trailer and a hijacked
		// connection are not in it). 5th round, F1: the live map missed a header deleted
		// after WriteHeader.
		var b strings.Builder
		for k, vs := range r.w.Result().Header {
			for _, v := range vs {
				b.WriteString(k + ": " + v + "\n")
			}
		}
		return b.String()
	}},
}

type surfaceHit struct {
	surface string
	leakHit
}

// scanArm searches every surface of one arm.
func (s *leakSet) scanArm(r leakResult) []surfaceHit {
	var out []surfaceHit
	for _, surf := range leakSurfaces {
		for _, h := range s.hits(surf.text(r)) {
			out = append(out, surfaceHit{surface: surf.name, leakHit: h})
		}
	}
	return out
}

// hashRecorder is the fake store, recording -- method by method -- what it is handed
// (session hashes, raw link tokens, addresses), which the search adds to its needles. The
// harvest is pinned by COUNT and ARITY per method (harvestWant), not by content.
type hashRecorder struct {
	*fakeStore
	hmu sync.Mutex
	got map[string][][]string
}

func (h *hashRecorder) record(method string, vals ...string) {
	h.hmu.Lock()
	defer h.hmu.Unlock()
	if h.got == nil {
		h.got = map[string][][]string{}
	}
	h.got[method] = append(h.got[method], vals)
}

func (h *hashRecorder) OperatorByEmail(ctx context.Context, email string) (db.OperatorAccount, error) {
	h.record("OperatorByEmail", email)
	return h.fakeStore.OperatorByEmail(ctx, email)
}

func (h *hashRecorder) RecordOperatorAuthEvent(ctx context.Context, kind db.OperatorAuthEvent, email string, admin uuid.UUID) error {
	h.record("RecordOperatorAuthEvent", email)
	return h.fakeStore.RecordOperatorAuthEvent(ctx, kind, email, admin)
}

func (h *hashRecorder) OpenOperatorSession(ctx context.Context, admin uuid.UUID, s string, step int64) error {
	h.record("OpenOperatorSession", s)
	return h.fakeStore.OpenOperatorSession(ctx, admin, s, step)
}

func (h *hashRecorder) TouchOperatorSession(ctx context.Context, s string) (db.OperatorSession, error) {
	h.record("TouchOperatorSession", s)
	return h.fakeStore.TouchOperatorSession(ctx, s)
}

func (h *hashRecorder) CloseOperatorSession(ctx context.Context, s string) error {
	h.record("CloseOperatorSession", s)
	return h.fakeStore.CloseOperatorSession(ctx, s)
}

func (h *hashRecorder) CompleteOperatorEnrollment(ctx context.Context, admin uuid.UUID, raw, d string, sealed []byte, step int64, s string) error {
	h.record("CompleteOperatorEnrollment", raw, d, string(sealed), s)
	return h.fakeStore.CompleteOperatorEnrollment(ctx, admin, raw, d, sealed, step, s)
}

// harvestWant pins the harvest: calls per method and the arity of each call's record.
// The counts are the arms' own, derived where each arm is driven (comments at the arms).
var harvestWant = map[string]struct{ calls, arity int }{
	"OperatorByEmail":            {calls: 9, arity: 1},    // A2 A3 A4 A20 A20b A23 A26b A28 A30b
	"RecordOperatorAuthEvent":    {calls: 7, arity: 1},    // A2 A3 A6 A14 A15 A17 A28
	"OpenOperatorSession":        {calls: 5, arity: 1},    // A7 A20b A24 A26b A30b
	"TouchOperatorSession":       {calls: 104, arity: 1},  // A8 A9 A21, A27 x 101
	"CloseOperatorSession":       {calls: 3002, arity: 1}, // A10 A22, A30a x 3000 (A30 is refused first)
	"CompleteOperatorEnrollment": {calls: 3, arity: 4},    // A16 A17 A25
}

// TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor -- THE CONTRACT (M10 OP-8; the
// shape is internal/operatorauth's
// TestLeak_NoInputInAnyErrorOrLogLine, m10-platform.md OP-4 block, OP-8 list):
//
// No member of the GROUPS G1-G15 (constants above) occurs, in any of the RENDERINGS
// R1-R10 (renderings), on any of the SURFACES S1-S4 (leakSurfaces; S4 is the response
// headers AT WriteHeader, the recorder's Result().Header), in any of the 30 numbered ARMS
// A1-A30 below -- EXCEPT the DESIGNED EGRESS D1-D5, each of which is
// pinned the other way: the value IS on its surface in its arm. The groups are measured
// against the CLOSED list neverLog (12 items, a pinned literal): every group of every item
// has a member. G15 (the client address) is bound to no item -- the package's own claim.
// The fake store's harvest -- session hashes, raw link tokens, digests, envelopes,
// addresses it was handed -- is searched too (G5, G8, G12, G11, G13) and pinned method by
// method by COUNT and ARITY (harvestWant), not by content.
//
// POSITIVE CONTROLS, each independent of what it checks:
//   - RENDERINGS: every needle of every member must occur in the text its rendering's
//     own builder produces (leakBuilders: fmt, the streaming encoders, net/url's form
//     encoder, the templ renderer of the pages themselves) -- an emptied, nil or raw-reduced rendering is red,
//     because G1 holds a password whose %q, JSON, URL and HTML forms differ from it and
//     every member's hex and base64 forms do;
//   - SURFACES: a canary put on each surface of a synthetic arm, ONE surface at a time,
//     must be found there by scanArm -- a surface dropped from the scan is red; and every
//     surface carried text in some real arm;
//   - DESIGNED EGRESS D1-D5: present where it is meant to be (the allowed table below):
//     D1 the login challenge in its Set-Cookie (S4; A4, A23 and the password steps of
//     A20b, A26b, A30b), D2 the session token in its Set-Cookie (S4; A7, A16 and the code
//     steps of A20b, A26b, A30b), D3 the enrollment page's TOTP key, base32 and URI (S3; A11), D4 the
//     pending blob in the form (S3; A11-A14), D5 the link token echoed into the
//     re-rendered enrollment form (S3; A12-A14).
//
// THE ARMS (handler order, then the faults and ceilings):
//
//	A1 sign-in page · A2 unknown address · A3 wrong password (G1's control password) ·
//	A4 right password (D1) · A5 code page · A6 wrong code · A7 right code (D2) ·
//	A8 console · A9 junk session cookie · A10 sign-out · A11 enrollment page (D3, D4) ·
//	A12 passwords differ (D4, D5) · A13 weak password (D4, D5) · A14 wrong first code
//	(D4, D5) · A15 malformed link token · A16 enrollment completed (D2) · A17 the SAME
//	link again -- refused by the database, the ErrEnrollment branch with a real token ·
//	A18 cross-origin sign-in · A19 oversized form · A20 the lookup fails · A21 the session
//	check fails · A22 the sign-out fails · A23 the password step for A24 (D1) · A24 opening
//	the session fails · A25 completing the enrollment fails · A26 the flood ceiling ·
//	A27 the session budget · A28 credentials in the query · A29 a link token in the query ·
//	A30 the sign-out ceiling (3 000 cookie-bearing sign-outs, then the operator's)
//
// NOT CLAIMED, BY NAME: split or partial values; renderings not on the list (base32,
// %X, a case-folded value, ...); what operatorauth's own types print (its
// TestLeak_NoSecretOnAnyPrintingPath); the ingress's log, the browser's history and
// anything outside this process; arms not numbered here (a failing crypto/rand, a cookie
// setter refusing an empty value, a failing templ render other than the tenant refusal).
func TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor(t *testing.T) {
	var process, access bytes.Buffer
	plog, alog := debugCapture(&process), debugCapture(&access)
	kek, tokenKey := randBytes(t, 32), randBytes(t, 32)
	now := time.Unix(1_900_000_005, 0)
	store := &hashRecorder{fakeStore: newFakeStore()}
	auth, err := operatorauth.New(store, operatorauth.Config{
		TOTPKEK: operatorauth.NewKey(kek), TokenHMACKey: operatorauth.NewKey(tokenKey),
		Now: func() time.Time { return now }, Log: plog,
	})
	if err != nil {
		t.Fatal(err)
	}
	surface, err := operator.New(auth, opHost, opBase, plog)
	if err != nil {
		t.Fatal(err)
	}
	h := httpx.NewRouter(&config.Config{OperatorHost: opHost, BaseURL: opBase}, alog, surface)

	// The account.
	const pass = "FAKEop8leakPASSPHRASEfake"
	id := uuid.New()
	email := "fake-op8-leak-" + id.String()[:8] + "@example.test"
	key := randBytes(t, 20)
	sealed, err := sun.Seal(kek, id[:], key)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	acc := db.OperatorAccount{ID: id, Digest: db.NewPasswordHash(string(digest)), Sealed: db.NewSealedSecret(sealed)}
	store.accounts[email], store.byID[id] = acc, acc
	pending := uuid.New()
	linkToken := base64.RawURLEncoding.EncodeToString(randBytes(t, 32))
	store.tokens[pending] = linkToken

	set := &leakSet{}
	// G1's control password: its %q, JSON, URL and HTML forms all differ from it.
	const special = "FAKE\"pass\\word <&> 'x'+=ü\tend"
	newPass, queryPass := "FAKEop8leakNEWpassphrase", "FAKEop8leakQUERYpassword"
	unknown := "fake-op8-nobody@example.test"
	junk := base64.RawURLEncoding.EncodeToString(randBytes(t, 32))
	queryToken := base64.RawURLEncoding.EncodeToString(randBytes(t, 32))
	set.add(gPassword, pass, special, newPass, queryPass)
	right, wrong := totpAt(key, now), wrongCodeAt(key, now)
	set.add(gCode, right, wrong)
	set.add(gCodeNear, totpAt(key, now.Add(-30*time.Second)), totpAt(key, now.Add(30*time.Second)))
	set.add(gSecret, string(key), base32.StdEncoding.EncodeToString(key))
	set.add(gLinkToken, linkToken, queryToken, "FAKEshortLinkToken")
	set.add(gLinkHash, operatorauth.EnrollmentTokenHash(linkToken))
	set.add(gSession, junk)
	set.add(gEnvelope, string(sealed))
	set.add(gDigest, string(digest))
	set.add(gEmail, email, unknown)
	set.add(gKeys, string(kek), string(tokenKey))
	const remote, remote2, remote3, remote4 = "198.51.100.23", "198.51.100.24", "198.51.100.25", "198.51.100.26"
	for _, a := range []string{remote, remote2, remote3, remote4} {
		set.add(gAddr, a, httpx.RateKey(netip.MustParseAddr(a)))
	}

	results := map[string]leakResult{}
	var order []string
	do := func(arm string, r req) *httptest.ResponseRecorder {
		t.Helper()
		process.Reset()
		access.Reset()
		var body io.Reader = strings.NewReader("")
		if r.form != nil {
			body = strings.NewReader(r.form.Encode())
		}
		hr := httptest.NewRequest(r.method, "http://"+opHost+r.path, body)
		hr.Host = opHost
		hr.RemoteAddr = remote + ":5000"
		if r.remote != "" {
			hr.RemoteAddr = r.remote + ":5000"
		}
		if r.form != nil {
			hr.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if r.origin != "" {
			hr.Header.Set("Origin", r.origin)
		}
		for k, v := range r.header {
			hr.Header.Set(k, v)
		}
		for _, c := range r.cookies {
			hr.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, hr)
		if _, seen := results[arm]; !seen {
			order = append(order, arm)
		}
		results[arm] = leakResult{w: w, process: process.String(), access: access.String()}
		return w
	}
	post := func(arm, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		return do(arm, req{method: http.MethodPost, path: path, form: form, origin: opOrigin, cookies: cookies})
	}
	get := func(arm, path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		return do(arm, req{method: http.MethodGet, path: path, cookies: cookies, header: map[string]string{"Sec-Fetch-Site": "same-origin"}})
	}
	fail := func(method string, err error) {
		store.mu.Lock()
		if err == nil {
			delete(store.fail, method)
		} else {
			store.fail[method] = err
		}
		store.mu.Unlock()
	}
	// signIn signs the account in from addr (an address whose flood budget is not spent).
	signIn := func(arm, addr, codeAt string) *http.Cookie {
		t.Helper()
		w := do(arm+" (password)", req{method: http.MethodPost, path: "/operator/login", origin: opOrigin, remote: addr,
			form: url.Values{"email": {email}, "password": {pass}}})
		ch := cookie(w, operatorauth.ChallengeCookieName)
		if ch == nil {
			t.Fatalf("PREMISE: %s minted no challenge (%d)", arm, w.Code)
		}
		set.add(gChallenge, ch.Value)
		w = do(arm+" (code)", req{method: http.MethodPost, path: "/operator/login/totp", origin: opOrigin, remote: addr,
			form: url.Values{"code": {codeAt}}, cookies: []*http.Cookie{ch}})
		s := cookie(w, operatorauth.SessionCookieName)
		if s == nil {
			t.Fatalf("PREMISE: %s issued no session (%d)", arm, w.Code)
		}
		set.add(gSession, s.Value)
		return s
	}

	// THE ARMS. Harvest arithmetic in [brackets]: OperatorByEmail (E), auth rows (R),
	// opens (O), touches (T), closes (C), completes (P).
	get("A1 sign-in page", "/operator/login")
	post("A2 unknown address", "/operator/login", url.Values{"email": {unknown}, "password": {pass}})   // [E1 R1]
	post("A3 wrong password", "/operator/login", url.Values{"email": {email}, "password": {special}})   // [E2 R2]
	w := post("A4 right password", "/operator/login", url.Values{"email": {email}, "password": {pass}}) // [E3]
	ch := cookie(w, operatorauth.ChallengeCookieName)
	if ch == nil {
		t.Fatal("PREMISE: A4 minted no challenge")
	}
	set.add(gChallenge, ch.Value)
	get("A5 code page", "/operator/login/totp", ch)
	post("A6 wrong code", "/operator/login/totp", url.Values{"code": {wrong}}, ch)     // [R3: totp_failed]
	w = post("A7 right code", "/operator/login/totp", url.Values{"code": {right}}, ch) // [O1]
	sess := cookie(w, operatorauth.SessionCookieName)
	if sess == nil {
		t.Fatal("PREMISE: A7 issued no session")
	}
	set.add(gSession, sess.Value)
	get("A8 console", "/operator", sess)                                                                        // [T1]
	get("A9 junk session cookie", "/operator", &http.Cookie{Name: operatorauth.SessionCookieName, Value: junk}) // [T2]
	post("A10 sign-out", "/operator/logout", url.Values{}, sess)                                                // [C1]
	w = get("A11 enrollment page", "/operator/enroll?id="+pending.String())
	pageKey := regexp.MustCompile(`<p class="op-key">([A-Z2-7 ]+)</p>`).FindStringSubmatch(w.Body.String())
	pageBlob := regexp.MustCompile(`name="blob" value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if pageKey == nil || pageBlob == nil {
		t.Fatal("PREMISE: A11's page carries no key or blob")
	}
	pk := strings.ReplaceAll(pageKey[1], " ", "")
	rawPK, err := base32.StdEncoding.DecodeString(pk)
	if err != nil {
		t.Fatal(err)
	}
	blob := html.UnescapeString(pageBlob[1])
	set.add(gSecret, pk, pageKey[1], string(rawPK))
	set.add(gBlob, blob)
	pageCode := totpAt(rawPK, now)
	set.add(gCode, pageCode, wrongCodeAt(rawPK, now))
	enroll := func(token, p1, p2, code string) url.Values {
		return url.Values{"id": {pending.String()}, "token": {token}, "blob": {blob}, "password": {p1}, "password_again": {p2}, "code": {code}}
	}
	post("A12 passwords differ", "/operator/enroll", enroll(linkToken, newPass, newPass+"x", pageCode))
	post("A13 weak password", "/operator/enroll", enroll(linkToken, "short", "short", pageCode))
	post("A14 wrong first code", "/operator/enroll", enroll(linkToken, newPass, newPass, wrongCodeAt(rawPK, now))) // [R4]
	post("A15 malformed link token", "/operator/enroll", enroll("FAKEshortLinkToken", newPass, newPass, pageCode)) // [R5]
	w = post("A16 enrollment completed", "/operator/enroll", enroll(linkToken, newPass, newPass, pageCode))        // [P1]
	enrolled := cookie(w, operatorauth.SessionCookieName)
	if enrolled == nil {
		t.Fatalf("PREMISE: A16 issued no session (%d)", w.Code)
	}
	set.add(gSession, enrolled.Value)
	w = post("A17 the same link again", "/operator/enroll", enroll(linkToken, newPass, newPass, pageCode)) // [P2 R6]
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "This setup link does not work") {
		t.Fatalf("PREMISE: A17 is not the ErrEnrollment branch (%d)", w.Code)
	}
	do("A18 cross-origin sign-in", req{method: http.MethodPost, path: "/operator/login", form: url.Values{"email": {email}, "password": {pass}},
		origin: "https://taptime.mt", header: map[string]string{"Sec-Fetch-Site": "same-site"}})
	post("A19 oversized form", "/operator/login", url.Values{"email": {email}, "password": {pass + strings.Repeat("p", 20<<10)}})
	fail("OperatorByEmail", errFakeDB)
	post("A20 the lookup fails", "/operator/login", url.Values{"email": {email}, "password": {pass}}) // [E4]
	fail("OperatorByEmail", nil)
	live := signIn("A20b sign-in for A21-A22", remote, right) // [E5 O2]
	fail("TouchOperatorSession", errFakeDB)
	get("A21 the session check fails", "/operator", live) // [T3]
	fail("TouchOperatorSession", nil)
	fail("CloseOperatorSession", errFakeDB)
	post("A22 the sign-out fails", "/operator/logout", url.Values{}, live) // [C2]
	fail("CloseOperatorSession", nil)
	w = post("A23 password step for A24", "/operator/login", url.Values{"email": {email}, "password": {pass}}) // [E6]
	ch2 := cookie(w, operatorauth.ChallengeCookieName)
	set.add(gChallenge, ch2.Value)
	fail("OpenOperatorSession", errFakeDB)
	post("A24 opening the session fails", "/operator/login/totp", url.Values{"code": {totpAt(key, now.Add(30*time.Second))}}, ch2) // [O3]
	fail("OpenOperatorSession", nil)
	fail("CompleteOperatorEnrollment", errFakeDB)
	post("A25 completing the enrollment fails", "/operator/enroll", enroll(linkToken, newPass, newPass, pageCode)) // [P3]
	fail("CompleteOperatorEnrollment", nil)
	do("A28 credentials in the query", req{method: http.MethodPost, // [E8 R7]: the empty body's empty address
		path: "/operator/login?email=" + url.QueryEscape(email) + "&password=" + url.QueryEscape(queryPass), form: url.Values{}, origin: opOrigin})
	get("A29 a link token in the query", "/operator/enroll?id="+pending.String()+"&token="+queryToken)
	for i := 0; i < 301; i++ {
		get("A26 flood ceiling", "/operator/login")
	}
	if results["A26 flood ceiling"].w.Code != http.StatusTooManyRequests {
		t.Fatalf("PREMISE: A26 was not throttled (%d)", results["A26 flood ceiling"].w.Code)
	}
	live2 := signIn("A26b sign-in for A27", remote2, totpAt(key, now.Add(-30*time.Second))) // [E7 O4]
	for i := 0; i < 101; i++ {                                                              // [T4..T104 = 101]
		do("A27 session budget", req{method: http.MethodGet, path: "/operator", cookies: []*http.Cookie{live2}, remote: remote2,
			header: map[string]string{"Sec-Fetch-Site": "same-origin"}})
	}
	if results["A27 session budget"].w.Code != http.StatusTooManyRequests ||
		!strings.Contains(results["A27 session budget"].process, "operator session budget reached") {
		t.Fatalf("PREMISE: A27 was not refused by the session budget (%d)", results["A27 session budget"].w.Code)
	}
	// A30: 3 000 cookie-bearing sign-outs from a third address spend its ceiling [C3..C3002 =
	// 3000] -- and, metered, its flood budget, so the operator signs in from a fourth; then
	// the operator's own sign-out from the third address is the ceiling's 429, which clears
	// the cookie and makes no CloseOperatorSession call (harvestWant: A30 is refused first).
	for i := 0; i < 3000; i++ {
		do("A30a sign-out ceiling, spent", req{method: http.MethodPost, path: "/operator/logout", form: url.Values{}, origin: opOrigin,
			remote: remote3, cookies: []*http.Cookie{{Name: operatorauth.SessionCookieName, Value: junk}}})
	}
	live3 := signIn("A30b sign-in for A30", remote4, totpAt(key, now.Add(30*time.Second))) // [E9 O5]
	w = do("A30 the sign-out ceiling", req{method: http.MethodPost, path: "/operator/logout", form: url.Values{}, origin: opOrigin,
		remote: remote3, cookies: []*http.Cookie{live3}})
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("PREMISE: A30 was not refused by the sign-out ceiling (%d)", w.Code)
	}
	// The harvest, searched.
	store.hmu.Lock()
	for _, v := range store.got["OperatorByEmail"] {
		set.add(gEmail, v[0])
	}
	for _, v := range store.got["RecordOperatorAuthEvent"] {
		set.add(gEmail, v[0])
	}
	for _, m := range []string{"OpenOperatorSession", "TouchOperatorSession", "CloseOperatorSession"} {
		for _, v := range store.got[m] {
			set.add(gSessionHash, v[0])
		}
	}
	for _, v := range store.got["CompleteOperatorEnrollment"] {
		set.add(gLinkToken, v[0])
		set.add(gDigest, v[1])
		set.add(gEnvelope, v[2])
		set.add(gSessionHash, v[3])
	}
	// THE HARVEST PIN: count and arity, method by method.
	for m, want := range harvestWant {
		calls := store.got[m]
		if len(calls) != want.calls {
			t.Errorf("harvest: %s was called %d time(s), the arms make %d", m, len(calls), want.calls)
		}
		for i, c := range calls {
			if len(c) != want.arity {
				t.Errorf("harvest: %s call %d recorded %d value(s), want %d", m, i, len(c), want.arity)
			}
		}
	}
	if len(store.got) != len(harvestWant) {
		t.Errorf("harvest: %d method(s) recorded, %d pinned", len(store.got), len(harvestWant))
	}
	store.hmu.Unlock()

	// THE CLOSED CRITERION.
	if len(neverLog) != neverLogItems {
		t.Errorf("the never-log table has %d item(s); it is a pinned literal of %d", len(neverLog), neverLogItems)
	}
	for _, it := range neverLog {
		for _, g := range it.groups {
			if !set.inGroup(g) {
				t.Errorf("never-log item %q: group %q has no member", it.item, g)
			}
		}
	}
	if !set.inGroup(gAddr) {
		t.Errorf("group %q has no member", gAddr)
	}

	// THE DESIGNED EGRESS, positively.
	allowed := map[string]map[string][]string{ // arm -> surface -> values allowed there
		"A4 right password":         {"S4 response headers": {ch.Value}},
		"A7 right code":             {"S4 response headers": {sess.Value}},
		"A11 enrollment page":       {"S3 response body": {pk, pageKey[1], blob}},
		"A12 passwords differ":      {"S3 response body": {blob, linkToken}},
		"A13 weak password":         {"S3 response body": {blob, linkToken}},
		"A14 wrong first code":      {"S3 response body": {blob, linkToken}},
		"A16 enrollment completed":  {"S4 response headers": {enrolled.Value}},
		"A23 password step for A24": {"S4 response headers": {ch2.Value}},
	}
	for arm := range results {
		if strings.HasSuffix(arm, " (password)") {
			c := cookie(results[arm].w, operatorauth.ChallengeCookieName)
			allowed[arm] = map[string][]string{"S4 response headers": {c.Value}}
		}
		if strings.HasSuffix(arm, " (code)") {
			c := cookie(results[arm].w, operatorauth.SessionCookieName)
			allowed[arm] = map[string][]string{"S4 response headers": {c.Value}}
		}
	}
	for arm, bySurface := range allowed {
		res, ok := results[arm]
		if !ok {
			t.Fatalf("PREMISE: designed egress for an arm that did not run: %s", arm)
		}
		for surf, vals := range bySurface {
			var text string
			for _, s := range leakSurfaces {
				if s.name == surf {
					text = s.text(res)
				}
			}
			for _, v := range vals {
				if !strings.Contains(text, v) && !strings.Contains(text, html.EscapeString(v)) {
					t.Errorf("DESIGNED EGRESS: %s's value is not on %s (length %d)", arm, surf, len(v))
				}
			}
		}
	}

	// THE SEARCH.
	for _, arm := range order {
		for _, hit := range set.scanArm(results[arm]) {
			if containsValue(allowed[arm][hit.surface], hit.value) {
				continue
			}
			t.Errorf("%s: a %s member (%s, length %d) is on %s", arm, hit.group, hit.rendering, len(hit.value), hit.surface)
		}
	}

	// POSITIVE CONTROL -- RENDERINGS.
	builders := leakBuilders(t)
	if len(builders) != len(renderings) {
		t.Fatalf("POSITIVE CONTROL: %d builder(s) for %d rendering(s)", len(builders), len(renderings))
	}
	for _, r := range renderings {
		build, ok := builders[r.name]
		if !ok {
			t.Fatalf("POSITIVE CONTROL: rendering %q has no builder", r.name)
		}
		differs := 0
		for _, m := range set.members {
			n := r.fn(m.value)
			if len(n) < minNeedle {
				t.Errorf("POSITIVE CONTROL: %s gives a %s member no searched needle (%d bytes)", r.name, m.group, len(n))
				continue
			}
			if !strings.Contains(build(m.value), n) {
				t.Errorf("POSITIVE CONTROL: %s's needle for a %s member is not in its builder's text", r.name, m.group)
			}
			if n != m.value {
				differs++
			}
		}
		if r.name != "R1 raw" && differs == 0 {
			t.Errorf("POSITIVE CONTROL: no member's %s differs from its raw value -- a rendering reduced to the raw value would pass", r.name)
		}
	}
	// POSITIVE CONTROL -- SURFACES: a canary on one surface at a time is found there.
	canary := set.members[0]
	wantSurfaces := []string{"S1 process log", "S2 access log", "S3 response body", "S4 response headers"}
	if len(leakSurfaces) != len(wantSurfaces) {
		t.Fatalf("POSITIVE CONTROL: %d surface(s) scanned, %d pinned", len(leakSurfaces), len(wantSurfaces))
	}
	for _, surf := range wantSurfaces {
		res := leakResult{w: httptest.NewRecorder()}
		switch surf {
		case "S1 process log":
			res.process = "x " + canary.value + " y"
		case "S2 access log":
			res.access = "x " + canary.value + " y"
		case "S3 response body":
			res.w.Body.WriteString("x " + canary.value + " y")
		case "S4 response headers":
			res.w.Header().Set("X-Canary", canary.value)
			res.w.WriteHeader(http.StatusOK) // the snapshot S4 reads is taken here
		}
		found := false
		for _, hit := range set.scanArm(res) {
			if hit.surface == surf && hit.value == canary.value {
				found = true
			}
		}
		if !found {
			t.Errorf("POSITIVE CONTROL: a canary on %s is not found by the scan", surf)
		}
	}
	// And every surface carried text in some real arm.
	carried := map[string]bool{}
	for _, r := range results {
		for _, s := range leakSurfaces {
			if s.text(r) != "" {
				carried[s.name] = true
			}
		}
	}
	for _, s := range wantSurfaces {
		if !carried[s] {
			t.Errorf("PREMISE: no arm put any text on %s", s)
		}
	}
}

func containsValue(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
