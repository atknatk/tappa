package operatorauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/atknatk/tappa/internal/db"
)

// nopStore is a Store for the tests below that never reach the database; every method
// fails the test if called, so a test that believes it stays in memory is checked.
type nopStore struct{ t *testing.T }

func (s nopStore) fail() error {
	s.t.Helper()
	s.t.Error("the store was called")
	return errors.New("nopStore")
}
func (s nopStore) OperatorByEmail(context.Context, string) (db.OperatorAccount, error) {
	return db.OperatorAccount{}, s.fail()
}
func (s nopStore) OperatorByID(context.Context, uuid.UUID) (db.OperatorAccount, error) {
	return db.OperatorAccount{}, s.fail()
}
func (s nopStore) RecordOperatorAuthEvent(context.Context, db.OperatorAuthEvent, string, uuid.UUID) error {
	return s.fail()
}
func (s nopStore) OpenOperatorSession(context.Context, uuid.UUID, string, int64) error {
	return s.fail()
}
func (s nopStore) CompleteOperatorEnrollment(context.Context, uuid.UUID, string, string, []byte, int64, string) error {
	return s.fail()
}
func (s nopStore) TouchOperatorSession(context.Context, string) (db.OperatorSession, error) {
	return db.OperatorSession{}, s.fail()
}
func (s nopStore) CloseOperatorSession(context.Context, string) error { return s.fail() }

// ------------------------------------------------------------------ password --

// TestPasswordPolicy_FourteenRunesSeventyTwoBytes is ADR 0020 §1 as a rule for a NEW
// password: at least 14 runes (so 14 two-byte runes count as 14, not 28), at most 72
// bytes (bcrypt's input limit), valid UTF-8, and no composition rule.
func TestPasswordPolicy_FourteenRunesSeventyTwoBytes(t *testing.T) {
	for _, c := range []struct {
		name string
		pw   string
		ok   bool
	}{
		{"13 ASCII runes", strings.Repeat("a", 13), false},
		{"14 ASCII runes", strings.Repeat("a", 14), true},
		{"14 two-byte runes (28 bytes)", strings.Repeat("ş", 14), true},
		{"13 two-byte runes (26 bytes)", strings.Repeat("ş", 13), false},
		{"72 bytes", strings.Repeat("b", 72), true},
		{"73 bytes", strings.Repeat("b", 73), false},
		{"36 two-byte runes (72 bytes)", strings.Repeat("ş", 36), true},
		{"37 two-byte runes (74 bytes)", strings.Repeat("ş", 37), false},
		{"invalid UTF-8", strings.Repeat("a", 14) + "\xff", false},
		{"lower case only, no digit -- no composition rule", "correct horse battery", true},
	} {
		err := checkPasswordPolicy(c.pw)
		if (err == nil) != c.ok {
			t.Errorf("%s: policy error=%v, want ok=%v", c.name, err, c.ok)
		}
		if err != nil && !errors.Is(err, ErrWeakPassword) {
			t.Errorf("%s: %v is not ErrWeakPassword", c.name, err)
		}
	}
}

// TestPassword_TheDigestAndTheDummyAreBothCostTwelve pins the COST half of "same
// time": the digest this package stores and the dummy an unknown address is compared
// against are both exactly Cost -- and Cost is 00026's floor, so the stored digest is
// one the column accepts.
func TestPassword_TheDigestAndTheDummyAreBothCostTwelve(t *testing.T) {
	// THE PRODUCTION CONSTRUCTOR, not the test helper: New makes its own dummy.
	a, err := New(nopStore{t}, Config{TOTPKEK: NewKey(testKey(t)), TokenHMACKey: NewKey(testKey(t)), Log: newTestLogger(&strings.Builder{})})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// The stored digest is made by the Authenticator's OWN digestFn -- the field
	// enrollment calls -- not by hashPassword directly: a production digestFn at another
	// cost is exactly the drift that breaks "same time" (a real operator's comparison
	// would then cost more than the dummy's: an address oracle). Measured (OP-6
	// verification, 3rd round): cost-13 and cost-14 digestFns stayed green while this
	// read hashPassword.
	d, err := a.digestFn(fixturePassphrase)
	if err != nil {
		t.Fatalf("digestFn: %v", err)
	}
	for name, digest := range map[string][]byte{"stored digest": []byte(d), "dummy": a.keys.dummyDigest.bytes()} {
		c, err := bcrypt.Cost(digest)
		if err != nil || c != Cost || Cost != 12 {
			t.Errorf("%s: cost %d (err %v), want %d = 12", name, c, err, Cost)
		}
	}
	if !regexp.MustCompile(`^\$2[aby]\$1[2-4]\$[./A-Za-z0-9]{53}$`).MatchString(d) {
		t.Errorf("the stored digest does not have the shape 00026's CHECK requires")
	}
	if !a.compare(d, fixturePassphrase) || a.compare(d, fixturePassphrase+"x") {
		t.Fatal("compare does not tell the right passphrase from a wrong one")
	}
}

// TestDummyDigest_IsNotTheDigestOfAKnownValue: the dummy digest is a bcrypt of 32
// bytes crypto/rand drew and nobody kept (m10-platform.md, OP-6 md. 18, "Tek liste
// (11. tur)", S6: the reason it is not searched), so it is not the digest of a value
// anyone knows -- not of the seed a read that fills nothing leaves (32 zero bytes, in
// the encoding newDummyDigest hashes), and not of the empty password. (12th round, the
// 11th auditor: with `rand.Read(nil)` the seed stayed zero and every test was green.)
// Two digests of the SAME seed differ anyway -- bcrypt salts each -- so comparing two
// dummies would prove nothing; the comparison against the known values does. The dummy
// is sharedDummy, newDummyDigest's product, so no further generation is paid; the two
// cost-12 comparisons run side by side.
func TestDummyDigest_IsNotTheDigestOfAKnownValue(t *testing.T) {
	d, err := sharedDummy()
	if err != nil {
		t.Fatalf("dummy digest: %v", err)
	}
	known := map[string][]byte{
		"the zero seed, encoded as newDummyDigest encodes it": []byte(base64.RawURLEncoding.EncodeToString(make([]byte, 32))),
		"the empty password": {},
	}
	for name, pw := range known {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if bcrypt.CompareHashAndPassword(d, pw) == nil {
				t.Errorf("the dummy digest is the digest of %s", name)
			}
		})
	}
}

// TestPassword_AnOverlongPasswordNeverMatches is the 72-byte truncation (Q03): a digest
// of a 72-byte password and a 100-byte password sharing those 72 bytes MATCH in
// bcrypt's comparer, and must not match here -- while the comparison is still paid.
func TestPassword_AnOverlongPasswordNeverMatches(t *testing.T) {
	a, _ := newAuth(t, nopStore{t}, time.Now())
	pw72 := strings.Repeat("p", 72)
	d, err := hashPassword(pw72)
	if err != nil {
		t.Fatalf("hashPassword: %v", err)
	}
	long := pw72 + strings.Repeat("q", 28)
	if bcrypt.CompareHashAndPassword([]byte(d), []byte(long)) != nil {
		t.Fatal("PREMISE: bcrypt's comparer no longer truncates; this test's reason is gone")
	}
	calls := 0
	a.compareFn = func(dg, pw []byte) error { calls++; return bcrypt.CompareHashAndPassword(dg, pw) }
	if a.compare(d, long) {
		t.Fatal("a 100-byte password authenticated against the digest of its 72-byte prefix")
	}
	if calls != 1 {
		t.Fatalf("the over-long password paid %d comparisons, want 1 (same time as any other)", calls)
	}
	calls = 0
	a.compareDummy(long)
	if calls != 1 {
		t.Fatalf("the dummy arm paid %d comparisons, want 1", calls)
	}
}

// --------------------------------------------------------------------- token --

// TestSessionToken_HashIsKeyedLowerHexOverTheString: 64 lower-case hex characters (the
// 00026 CHECK), a function of the key (another key, another hash), over the token's
// STRING, and the shape gate refuses every non-token before any HMAC.
func TestSessionToken_HashIsKeyedLowerHexOverTheString(t *testing.T) {
	k1, k2 := testKey(t), testKey(t)
	tok, err := newSessionToken()
	if err != nil {
		t.Fatalf("newSessionToken: %v", err)
	}
	h1, err := tok.hash(k1)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	h2, _ := tok.hash(k2)
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(h1) || h1 == h2 {
		t.Fatalf("hash shape ok=%v, key-dependent=%v", regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(h1), h1 != h2)
	}
	if again, _ := wrapSessionToken(tok.reveal()).hash(k1); again != h1 {
		t.Fatal("the same token hashed twice under one key gave two values")
	}
	for _, bad := range []string{"", "short", strings.Repeat("a", 42), strings.Repeat("a", 44), strings.Repeat("!", 43)} {
		if _, err := wrapSessionToken(bad).hash(k1); !errors.Is(err, errMalformed) {
			t.Errorf("value of length %d: %v, want errMalformed", len(bad), err)
		}
	}
	if _, err := tok.hash(k1[:31]); err == nil {
		t.Fatal("a 31-byte key was accepted")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(tok.reveal())
	if len(tok.reveal()) != 43 || len(raw) != 32 {
		t.Fatalf("a session token is %d characters over %d bytes, want 43 over 32 (256 bits)", len(tok.reveal()), len(raw))
	}

	// OVER THE STRING, NOT THE DECODED BYTES (token.go's claim, measured rather than
	// argued -- OP-6 verification, 2026-09-30: hashing the decoded bytes stayed green
	// before this existed). 32 bytes leave two unused low bits in the last base64
	// character, and wellFormed decodes leniently, so a second spelling of the SAME bytes
	// passes the shape gate. Hashed over the string it is another hash -- no session;
	// hashed over the bytes it would be a second valid cookie for the same session.
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	v := tok.reveal()
	idx := strings.IndexByte(alphabet, v[len(v)-1])
	if idx < 0 || idx&3 != 0 {
		t.Fatalf("PREMISE: the canonical last character has non-zero padding bits")
	}
	variant := v[:len(v)-1] + string(alphabet[idx|1])
	vb, err := base64.RawURLEncoding.DecodeString(variant)
	if err != nil || string(vb) != string(raw) || !wellFormed(variant) {
		t.Fatalf("PREMISE: the variant does not decode to the same bytes through the shape gate (%v)", err)
	}
	if hv, err := wrapSessionToken(variant).hash(k1); err != nil || hv == h1 {
		t.Fatalf("a non-canonical spelling of the same token bytes hashed to the same value (err %v); the map cookie -> hash is not injective", err)
	}
}

// ----------------------------------------------------------------- challenge --

// TestChallenge_BindsTheAccountAndExpires: a challenge verifies for the account it
// names until challengeTTL has passed and not after; a minute's backwards clock is
// tolerated and more is not; any changed byte, a version it does not know and another
// Authenticator's key all refuse. The clock is injected.
func TestChallenge_BindsTheAccountAndExpires(t *testing.T) {
	// The lifetimes are decisions (challenge.go, enrollment.go argue them), so they are
	// pinned as LITERALS; the boundaries below are then read relative to them.
	if challengeTTL != 5*time.Minute || challengeFutureSkew != time.Minute || pendingTTL != 30*time.Minute {
		t.Fatalf("challenge TTL %s, skew %s, pending blob TTL %s; decided 5m / 1m / 30m", challengeTTL, challengeFutureSkew, pendingTTL)
	}
	now := time.Unix(1_800_000_000, 0)
	clock := now
	a, _ := newAuth(t, nopStore{t}, now)
	a.now = func() time.Time { return clock }
	id := uuid.New()
	c, err := a.mintChallenge(id)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	for _, at := range []struct {
		d  time.Duration
		ok bool
	}{
		{0, true}, {challengeTTL - time.Second, true}, {challengeTTL, false}, {time.Hour, false},
		{-challengeFutureSkew, true}, {-challengeFutureSkew - time.Second, false},
	} {
		clock = now.Add(at.d)
		got, err := a.verifyChallenge(c)
		if at.ok && (err != nil || got != id) {
			t.Errorf("at %+v: %v (id match %v), want accepted", at.d, err, got == id)
		}
		if !at.ok && err == nil {
			t.Errorf("at %+v: accepted, want refused", at.d)
		}
	}
	clock = now
	v := c.reveal()
	for i := 0; i < len(v); i++ {
		if v[i] == '.' {
			continue
		}
		b := []byte(v)
		b[i] ^= 0x01
		if _, err := a.verifyChallenge(wrapChallenge(string(b))); err == nil {
			t.Fatalf("a challenge with byte %d changed was accepted", i)
		}
	}
	// A NON-CANONICAL SPELLING of the same bytes, built deliberately: both fields are
	// 41 and 32 bytes (length % 3 == 2), so the last character of each carries two
	// unused low bits. Setting one gives another string that the lenient decoder maps to
	// the SAME bytes (premise checked) -- and it must be refused.
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	dot := strings.IndexByte(v, '.')
	for _, end := range []int{dot - 1, len(v) - 1} {
		idx := strings.IndexByte(alphabet, v[end])
		if idx < 0 || idx&3 != 0 {
			t.Fatalf("PREMISE: the canonical last character %q has non-zero padding bits", v[end])
		}
		variant := v[:end] + string(alphabet[idx|1]) + v[end+1:]
		start := 0
		if end > dot {
			start = dot + 1
		}
		fieldEnd := dot
		if end > dot {
			fieldEnd = len(v)
		}
		a1, _ := base64.RawURLEncoding.DecodeString(v[start:fieldEnd])
		a2, err := base64.RawURLEncoding.DecodeString(variant[start:fieldEnd])
		if err != nil || string(a1) != string(a2) {
			t.Fatalf("PREMISE: the lenient decoder does not map the variant to the same bytes (%v)", err)
		}
		if _, err := a.verifyChallenge(wrapChallenge(variant)); err == nil {
			t.Fatalf("a non-canonical spelling of the same challenge (character %d) was accepted", end)
		}
	}
	other, _ := newAuth(t, nopStore{t}, now)
	if _, err := other.verifyChallenge(c); err == nil {
		t.Fatal("a challenge signed under one token key verified under another")
	}
	for _, bad := range []string{"", ".", "abc", v + "x", strings.Repeat("A", 300) + "." + strings.Repeat("A", 43)} {
		if _, err := a.verifyChallenge(wrapChallenge(bad)); err == nil {
			t.Errorf("malformed challenge %q accepted", bad)
		}
	}
	// A payload with another version, correctly signed, is refused: the version is read
	// only from an authenticated payload and must be the one this code writes.
	payload, _ := base64.RawURLEncoding.DecodeString(v[:strings.IndexByte(v, '.')])
	payload[0] = challengeVersion + 1
	forged := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(a.challengeMAC(payload))
	if _, err := a.verifyChallenge(wrapChallenge(forged)); err == nil {
		t.Fatal("a signed challenge of an unknown version was accepted")
	}
}

// TestChallenge_KeyIsDerivedNotTheSessionKey: the challenge MAC key is a labelled
// derivation of the operator token key -- not the key itself (a MAC over a challenge is
// never a session hash) and not anything a customer key could produce.
//
// The second half drives the Authenticator the constructor builds: a challenge re-signed
// under the RAW token key must be refused, and one re-signed under the derived key
// accepted. Measured (OP-6 verification, 2026-09-30): with only the first half, a build
// that signed challenges with the raw key stayed green -- the function was pinned, its
// use was not.
func TestChallenge_KeyIsDerivedNotTheSessionKey(t *testing.T) {
	k := testKey(t)
	d := deriveChallengeKey(k)
	if len(d) != 32 || string(d) == string(k) {
		t.Fatalf("derived key length %d, equal to the source %v", len(d), string(d) == string(k))
	}
	if string(deriveChallengeKey(testKey(t))) == string(d) {
		t.Fatal("two token keys derived the same challenge key")
	}
	// The derivation label as a LITERAL: the external leak test restates it to compute
	// the challenge MAC key as a search-set member (its group G13), so the two texts
	// must agree -- a moved label would leave that member searching for another key.
	lm := hmac.New(sha256.New, k)
	_, _ = lm.Write([]byte("taptime/operator/login-challenge/v1/key-derivation"))
	if string(lm.Sum(nil)) != string(d) {
		t.Fatal("deriveChallengeKey no longer uses the label the leak test restates")
	}

	a, _ := newAuth(t, nopStore{t}, time.Now())
	c, err := a.mintChallenge(uuid.New())
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	v := c.reveal()
	payload, err := strictB64.DecodeString(v[:strings.IndexByte(v, '.')])
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	signed := func(key []byte) Challenge {
		m := hmac.New(sha256.New, key)
		_, _ = m.Write([]byte(challengeMACLabel))
		_, _ = m.Write(payload)
		return wrapChallenge(v[:strings.IndexByte(v, '.')] + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil)))
	}
	if _, err := a.verifyChallenge(signed(a.keys.tokenKey.bytes())); err == nil {
		t.Error("a challenge signed under the RAW session HMAC key verified; the Authenticator does not sign with the derived key")
	}
	if _, err := a.verifyChallenge(signed(deriveChallengeKey(a.keys.tokenKey.bytes()))); err != nil {
		t.Errorf("a challenge signed under deriveChallengeKey(token key) was refused (%v); the Authenticator signs with something else", err)
	}

	// THE DOMAIN-SEPARATION LABEL, as a literal: the helper above signs with the
	// constant, so an emptied constant signs and verifies consistently -- measured (OP-6
	// verification, 2026-09-30): challengeMACLabel = "" stayed green. A MAC over the
	// payload WITHOUT the label must not verify; with the label spelled here, it must.
	sign := func(label string) Challenge {
		m := hmac.New(sha256.New, deriveChallengeKey(a.keys.tokenKey.bytes()))
		_, _ = m.Write([]byte(label))
		_, _ = m.Write(payload)
		return wrapChallenge(v[:strings.IndexByte(v, '.')] + "." + base64.RawURLEncoding.EncodeToString(m.Sum(nil)))
	}
	if _, err := a.verifyChallenge(sign("taptime/operator/login-challenge/v1|")); err != nil {
		t.Errorf("a challenge MAC'd under the literal label was refused (%v); the label moved", err)
	}
	if _, err := a.verifyChallenge(sign("")); err == nil {
		t.Error("a challenge MAC'd WITHOUT the domain-separation label verified")
	}

	// A correctly signed challenge that names the nil operator id is refused (the one
	// shape check after the MAC that nothing else reached).
	nilc, err := a.mintChallenge(uuid.Nil)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, err := a.verifyChallenge(nilc); err == nil {
		t.Error("a signed challenge for the nil operator id verified")
	}
}

// -------------------------------------------------------------------- cookies --

// TestCookies_AreHostPrefixedStrictAndSecure is ADR 0020 §2 on the wire: both cookies
// carry the __Host- prefix and every attribute the prefix demands (Secure, Path=/, NO
// Domain), plus HttpOnly and SameSite=Strict; the session lives 8 h and the challenge
// its TTL; a clear has the same attributes with Max-Age 0 (net/http's spelling of -1).
// Read on the Set-Cookie TEXT, because that is what a browser enforces.
func TestCookies_AreHostPrefixedStrictAndSecure(t *testing.T) {
	tok, _ := newSessionToken()
	a, _ := newAuth(t, nopStore{t}, time.Now())
	ch, _ := a.mintChallenge(uuid.New())

	// The line is split into its name=value pair and its attributes, and the attribute
	// SET is compared for equality: a substring check (the first version) passed
	// `Path=/operator` for "Path=/" and `Max-Age=288000` for "Max-Age=28800" -- measured
	// (OP-6 verification, 2026-09-30). A __Host- cookie with any Path but "/" is
	// refused by the browser.
	check := func(name string, write func(w http.ResponseWriter), wantName, wantValue, maxAge string) {
		t.Helper()
		rec := httptest.NewRecorder()
		write(rec)
		lines := rec.Result().Header.Values("Set-Cookie")
		if len(lines) != 1 {
			t.Fatalf("%s: %d Set-Cookie lines", name, len(lines))
		}
		parts := strings.Split(lines[0], "; ")
		if parts[0] != wantName+"="+wantValue {
			t.Errorf("%s: the cookie is not %s with the expected value (name and value are not printed)", name, wantName)
		}
		got := append([]string(nil), parts[1:]...)
		want := []string{"Path=/", "Max-Age=" + maxAge, "HttpOnly", "Secure", "SameSite=Strict"}
		sort.Strings(got)
		sort.Strings(want)
		if strings.Join(got, "; ") != strings.Join(want, "; ") {
			t.Errorf("%s: attributes %q, want exactly %q (no Domain, Path exactly /)", name, got, want)
		}
		if !strings.HasPrefix(wantName, "__Host-") {
			t.Errorf("%s: %s is not a __Host- cookie", name, wantName)
		}
	}
	check("session", func(w http.ResponseWriter) {
		if err := SetSessionCookie(w, tok); err != nil {
			t.Fatal(err)
		}
	}, SessionCookieName, tok.reveal(), "28800")
	check("session clear", ClearSessionCookie, SessionCookieName, "", "0")
	check("challenge", func(w http.ResponseWriter) {
		if err := SetChallengeCookie(w, ch); err != nil {
			t.Fatal(err)
		}
	}, ChallengeCookieName, ch.reveal(), "300")
	check("challenge clear", ClearChallengeCookie, ChallengeCookieName, "", "0")

	if SessionCookieName != "__Host-taptime_op" || ChallengeCookieName != "__Host-taptime_op_login" {
		t.Fatalf("the cookies are %q and %q; ADR 0020 §2 and its OP-6 note name __Host-taptime_op and __Host-taptime_op_login", SessionCookieName, ChallengeCookieName)
	}
	if err := SetSessionCookie(httptest.NewRecorder(), SessionToken{}); err == nil {
		t.Fatal("an empty session token was written into a cookie")
	}
	if err := SetChallengeCookie(httptest.NewRecorder(), Challenge{}); err == nil {
		t.Fatal("an empty challenge was written into a cookie")
	}
}

// TestCookies_EachCredentialOnlyUnderItsOwnName: reading returns the value from its own
// name only; a missing cookie is the error the gate expects; and a value crossed into
// the other cookie resolves to nothing -- a challenge presented as a session fails the
// token shape gate (no database is asked), a session token presented as a challenge
// fails the MAC.
func TestCookies_EachCredentialOnlyUnderItsOwnName(t *testing.T) {
	a, _ := newAuth(t, nopStore{t}, time.Now())
	tok, _ := newSessionToken()
	ch, _ := a.mintChallenge(uuid.New())

	r := httptest.NewRequest(http.MethodGet, "/operator", nil)
	if _, err := ReadSessionCookie(r); !errors.Is(err, ErrNoSession) {
		t.Fatalf("no session cookie: %v", err)
	}
	if _, err := ReadChallengeCookie(r); !errors.Is(err, ErrChallenge) {
		t.Fatalf("no challenge cookie: %v", err)
	}
	crossed := httptest.NewRequest(http.MethodGet, "/operator", nil)
	crossed.AddCookie(&http.Cookie{Name: SessionCookieName, Value: ch.reveal()})
	crossed.AddCookie(&http.Cookie{Name: ChallengeCookieName, Value: tok.reveal()})
	st, err := ReadSessionCookie(crossed)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := a.Verify(context.Background(), st); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a challenge presented as the session: %v, want ErrNoSession without a database call", err)
	}
	sc, err := ReadChallengeCookie(crossed)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, err := a.TOTP(context.Background(), sc, "123456"); !errors.Is(err, ErrChallenge) {
		t.Fatalf("a session token presented as the challenge: %v, want ErrChallenge", err)
	}
	// And other cookie NAMES are never read: the panel's and the employee's.
	other := httptest.NewRequest(http.MethodGet, "/operator", nil)
	other.AddCookie(&http.Cookie{Name: "tappa_admin_session", Value: tok.reveal()})
	other.AddCookie(&http.Cookie{Name: "taptime_op", Value: tok.reveal()})
	if _, err := ReadSessionCookie(other); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a value under another cookie name was read as the operator session: %v", err)
	}
}

// ------------------------------------------------------------------- budgets --

// TestBudget_FixedWindowWithTheInjectedClock: the limit is the limit, the count keeps
// climbing past it, the window restarts after its period by the injected clock, and
// keys are independent.
func TestBudget_FixedWindowWithTheInjectedClock(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := newBudget(3, time.Minute, func() time.Time { return now })
	for i := 1; i <= 3; i++ {
		if !b.spend("k") {
			t.Fatalf("event %d refused within a limit of 3", i)
		}
	}
	if b.spend("k") || b.charge("k") != 5 {
		t.Fatal("the fourth event passed, or the count stopped climbing")
	}
	if !b.spend("other") {
		t.Fatal("another key shares the first key's window")
	}
	now = now.Add(time.Minute)
	if !b.spend("k") {
		t.Fatal("the window did not restart after its period")
	}
}

// TestBudgets_TheShippedNumbersArePinned: the numbers are the decision (limits.go
// argues each), so they are pinned as literals -- a change is a visible edit here with
// the arithmetic next to it, not a constant that every test moves with (the panel's
// TestPanelConstants_ShippedValuesArePinned lesson).
func TestBudgets_TheShippedNumbersArePinned(t *testing.T) {
	a, _ := newAuth(t, nopStore{t}, time.Now())
	for name, c := range map[string]struct {
		b      *budget
		limit  int
		period time.Duration
	}{
		"flood (per address, every request)":        {a.limits.flood, 300, 10 * time.Minute},
		"work (per address, every bcrypt)":          {a.limits.work, 20, 10 * time.Minute},
		"account (per operator, every TOTP try)":    {a.limits.account, 10, 10 * time.Minute},
		"audit cap (process, password-less rows)":   {a.limits.auditCap, 30, 10 * time.Minute},
		"enrollment (process, every bcrypt + seal)": {a.limits.enroll, 10, 10 * time.Minute},
		"enrollment share (per address, 12c)":       {a.limits.enrollAddr, 3, 10 * time.Minute},
	} {
		if c.b.limit != c.limit || c.b.period != c.period {
			t.Errorf("%s: %d per %s, pinned %d per %s", name, c.b.limit, c.b.period, c.limit, c.period)
		}
	}
	if !a.AllowRequest("192.0.2.1") {
		t.Fatal("the first request of an address was refused")
	}
}

// TestAllowRequest_RefusesPastTheFloodLimitPerAddress is the flood budget's behaviour
// through the entry point OP-8's floodGate will call, on an Authenticator from the
// constructor path (build) with the shipped numbers: an address gets exactly floodLimit
// requests per window, the next is refused, another address is unaffected, and the
// window restarts after its period. The numbers themselves are pinned as literals by
// TestBudgets_TheShippedNumbersArePinned; this pins that AllowRequest spends them.
// Measured (OP-6 verification, 2026-09-30): an AllowRequest that always said yes stayed
// green before this test existed.
func TestAllowRequest_RefusesPastTheFloodLimitPerAddress(t *testing.T) {
	clock := time.Unix(1_800_000_000, 0)
	dummy, err := sharedDummy()
	if err != nil {
		t.Fatalf("dummy digest: %v", err)
	}
	a := build(nopStore{t}, Config{
		TOTPKEK: NewKey(testKey(t)), TokenHMACKey: NewKey(testKey(t)),
		Now: func() time.Time { return clock }, Log: newTestLogger(&strings.Builder{}),
	}, dummy)
	for i := 1; i <= floodLimit; i++ {
		if !a.AllowRequest("192.0.2.1") {
			t.Fatalf("request %d of an address was refused within the flood limit", i)
		}
	}
	if a.AllowRequest("192.0.2.1") {
		t.Fatal("a request past the flood limit was allowed")
	}
	if !a.AllowRequest("192.0.2.2") {
		t.Fatal("another address shares the first address's flood window")
	}
	clock = clock.Add(floodPeriod)
	if !a.AllowRequest("192.0.2.1") {
		t.Fatal("the flood window did not restart after its period")
	}
}

// TestNew_RefusesWhatItCannotUse: no store, no logger, a key of the wrong size.
func TestNew_RefusesWhatItCannotUse(t *testing.T) {
	good := Config{TOTPKEK: NewKey(testKey(t)), TokenHMACKey: NewKey(testKey(t)), Log: newTestLogger(&strings.Builder{})}
	if _, err := New(nil, good); err == nil {
		t.Error("a nil store was accepted")
	}
	c := good
	c.Log = nil
	if _, err := New(nopStore{t}, c); err == nil {
		t.Error("a nil logger was accepted")
	}
	for _, zero := range []func(*Config){func(c *Config) { c.TOTPKEK = Key{} }, func(c *Config) { c.TokenHMACKey = Key{} }} {
		c := good
		zero(&c)
		if _, err := New(nopStore{t}, c); err == nil {
			t.Error("a zero Key was accepted")
		}
	}
	for _, n := range []int{0, 16, 31, 33} {
		c := good
		c.TOTPKEK = NewKey(make([]byte, n))
		if _, err := New(nopStore{t}, c); err == nil {
			t.Errorf("a %d-byte TOTP KEK was accepted", n)
		}
		c = good
		c.TokenHMACKey = NewKey(make([]byte, n))
		if _, err := New(nopStore{t}, c); err == nil {
			t.Errorf("a %d-byte token key was accepted", n)
		}
	}
}

// TestRedaction_EveryMethodOnEveryType calls the five printing interfaces DIRECTLY on a
// populated value of each redacting type of THIS package: the external leak test
// reaches them through fmt and slog, which route %v to Format and slog to LogValue, so
// String, GoString and MarshalText would otherwise only be proven by the compile-time
// assertions. The table is pinned to the source: its names are exactly the types that
// declare a Format method (7th round: the table held 5 of the then 7, by hand).
func TestRedaction_EveryMethodOnEveryType(t *testing.T) {
	type printer interface {
		String() string
		GoString() string
		MarshalText() ([]byte, error)
	}
	sec, _ := NewSecret()
	enr, _ := NewEnrollmentToken()
	fakeKey := []byte("FAKEkeyFAKEkeyFAKEkeyFAKEkeyFAKE")
	cfg := Config{TOTPKEK: NewKey(fakeKey), TokenHMACKey: NewKey(fakeKey), Log: newTestLogger(&strings.Builder{})}
	table := map[string]struct {
		v   printer
		raw string
	}{
		"SessionToken":    {wrapSessionToken("FAKEvalueFAKEvalueFAKEvalueFAKEvalueFAKEval"), "FAKEvalue"},
		"Challenge":       {wrapChallenge("FAKEchallengeFAKE.FAKE"), "FAKEchallenge"},
		"Secret":          {sec, sec.Base32()},
		"EnrollmentToken": {enr, enr.RevealForLink()},
		"PendingBlob":     {PendingBlobFromForm("FAKEblobFAKEblob"), "FAKEblob"},
		"Key":             {NewKey(fakeKey), "FAKEkey"},
		"Config":          {cfg, "FAKEkey"},
		"Authenticator":   {*build(nopStore{t}, cfg, []byte("FAKEdummyFAKEdummy")), "FAKEkey"},
	}
	fset, files := parsePackage(t, "", "")
	formatting := map[string]bool{}
	for _, f := range files {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Format" {
				continue
			}
			rt := fn.Recv.List[0].Type
			if st, ok := rt.(*ast.StarExpr); ok {
				rt = st.X
			}
			if id, ok := rt.(*ast.Ident); ok {
				formatting[id.Name] = true
			} else {
				t.Errorf("%s: a Format method on a receiver this test cannot name", fset.Position(fn.Pos()))
			}
		}
	}
	for name := range formatting {
		if _, ok := table[name]; !ok {
			t.Errorf("%s declares Format and is not in the table", name)
		}
	}
	for name := range table {
		if !formatting[name] {
			t.Errorf("the table names %s, which declares no Format", name)
		}
	}
	for name, c := range table {
		text, _ := c.v.MarshalText()
		for path, got := range map[string]string{"String": c.v.String(), "GoString": c.v.GoString(), "MarshalText": string(text)} {
			if strings.Contains(got, c.raw) || !strings.Contains(got, name) {
				t.Errorf("%s.%s = %q: leaks, or does not name the type", name, path, got)
			}
		}
	}
	if (SessionToken{}).reveal() != "" || (Challenge{}).reveal() != "" || (Secret{}).reveal() != nil ||
		(EnrollmentToken{}).RevealForLink() != "" || (PendingBlob{}).RevealForForm() != "" || (Key{}).bytes() != nil {
		t.Fatal("a zero value revealed something")
	}
	(Secret{}).Zero() // must not panic on the zero value
}

// TestKey_CopiesInAndOut: NewKey holds its own copy -- the caller wiping its slice does
// not change the key -- and bytes hands out a copy (TestSealedSecret_CopiesInAndOut's
// precedent in internal/db). 8th round, the 7th auditor: a NewKey that aliased the
// caller's bytes (unsafe.String) stayed green.
func TestKey_CopiesInAndOut(t *testing.T) {
	in := []byte("0123456789abcdef0123456789abcdef")
	k := NewKey(in)
	clear(in)
	out := k.bytes()
	if string(out) != "0123456789abcdef0123456789abcdef" {
		t.Fatal("wiping the caller's slice changed the key")
	}
	clear(out)
	if string(k.bytes()) != "0123456789abcdef0123456789abcdef" || k.size() != 32 {
		t.Fatal("wiping a copy bytes handed out changed the key")
	}
}

// TestBudget_AFullMapFailsOpenAndStaysBounded: past budgetMaxKeys distinct keys the map
// is pruned (and, if nothing has expired, reset) rather than grown -- httpx.Limiter's
// fail-OPEN choice, so a caller rotating addresses cannot lock anybody out, and memory
// stays bounded.
func TestBudget_AFullMapFailsOpenAndStaysBounded(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := newBudget(1, time.Minute, func() time.Time { return now })
	b.spend("victim")
	if b.spend("victim") {
		t.Fatal("PREMISE: the victim's budget of 1 is not spent")
	}
	for i := 0; i < budgetMaxKeys; i++ {
		b.charge(itoa(i))
	}
	if len(b.windows) > budgetMaxKeys {
		t.Fatalf("the map grew to %d, past its bound %d", len(b.windows), budgetMaxKeys)
	}
	// The reset forgot the victim's window: the fail-OPEN direction, stated in limits.go.
	if !b.spend("victim") {
		t.Fatal("after the reset the victim's key is still refused; the documented direction is fail-open")
	}
	// Expired windows are PRUNED in preference to a reset: half the map is filled, the
	// clock passes the period, the other half is filled; one more key must drop the
	// expired half and keep the live half's counts.
	b = newBudget(1, time.Minute, func() time.Time { return now })
	for i := 0; i < budgetMaxKeys/2; i++ {
		b.charge("old-" + itoa(i))
	}
	now = now.Add(time.Minute)
	for i := 0; i < budgetMaxKeys-budgetMaxKeys/2; i++ {
		b.charge("live-" + itoa(i))
	}
	b.charge("one more")
	_, oldKept := b.windows["old-0"]
	live, liveKept := b.windows["live-0"]
	if oldKept || !liveKept || live.count != 1 || len(b.windows) != budgetMaxKeys-budgetMaxKeys/2+1 {
		t.Fatalf("after the prune: expired kept=%v, live kept=%v, map %d; want the expired half gone and the live half intact",
			oldKept, liveKept, len(b.windows))
	}
}

// ------------------------------------------------------------------- audit rows --

// auditRowViolations reads the package's own source and reports every write of a
// pre-session audit row that does not respect the split the process-wide audit cap
// rests on (limits.go, auditCapLimit): the three PASSWORD-LESS kinds (unknown_email,
// login_failed, enrollment_failed) are written ONLY through recordPasswordless -- the
// one place the cap is charged -- and the two kinds a password holder produces
// (totp_failed, locked) ONLY directly, outside it (m10-platform.md, OP-6 md. 9). A
// kind passed through a variable is resolved to every value the enclosing function
// assigns it.
func auditRowViolations(fset *token.FileSet, files []*ast.File) []string {
	passwordless := map[string]bool{"OperatorUnknownEmail": true, "OperatorLoginFailed": true, "OperatorEnrollmentFailed": true}
	direct := map[string]bool{"OperatorTOTPFailed": true, "OperatorLocked": true}
	var out []string
	seen := map[string]int{}
	capCalls := 0
	// A METHOD VALUE escapes the call checks below (`rec := a.store.RecordOperatorAuthEvent;
	// rec(...)` -- 4th audit, measured: a write of this shape stayed green). So every
	// RecordOperatorAuthEvent selector that is not the Fun of a call is itself flagged.
	called := map[*ast.SelectorExpr]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if sel, ok := c.Fun.(*ast.SelectorExpr); ok {
					called[sel] = true
				}
			}
			return true
		})
	}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "RecordOperatorAuthEvent" && !called[sel] {
				out = append(out, fmt.Sprintf("%s: the store's row writer used as a value, out of the checks' sight", fset.Position(sel.Pos())))
			}
			return true
		})
	}
	// EVERY declaration of every file is read, not only function bodies (5th audit,
	// measured: a package-level `var f = func(...) { ... RecordOperatorAuthEvent(...) }`
	// stayed out of sight while the walk visited FuncDecl bodies alone). A package-level
	// declaration is its own scope, named as such.
	type scope struct {
		name string
		body ast.Node
	}
	var scopes []scope
	for _, f := range files {
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body != nil {
					scopes = append(scopes, scope{d.Name.Name, d.Body})
				}
			case *ast.GenDecl:
				scopes = append(scopes, scope{"a package-level declaration", d})
			}
		}
	}
	for _, fn := range scopes {
		kinds := func(e ast.Expr) []string {
			if sel, ok := e.(*ast.SelectorExpr); ok {
				if x, ok := sel.X.(*ast.Ident); ok && x.Name == "db" {
					return []string{sel.Sel.Name}
				}
			}
			id, ok := e.(*ast.Ident)
			if !ok {
				return []string{"?"}
			}
			var vals []string
			ast.Inspect(fn.body, func(n ast.Node) bool {
				as, ok := n.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for i, l := range as.Lhs {
					li, ok := l.(*ast.Ident)
					if !ok || li.Name != id.Name || i >= len(as.Rhs) {
						continue
					}
					v := "?"
					if sel, ok := as.Rhs[i].(*ast.SelectorExpr); ok {
						if x, ok := sel.X.(*ast.Ident); ok && x.Name == "db" {
							v = sel.Sel.Name
						}
					}
					vals = append(vals, v)
				}
				return true
			})
			if len(vals) == 0 {
				return []string{"?"}
			}
			return vals
		}
		ast.Inspect(fn.body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			pos := fset.Position(call.Pos())
			switch sel.Sel.Name {
			case "RecordOperatorAuthEvent":
				if fn.name == "recordPasswordless" {
					capCalls++
					return true
				}
				for _, k := range kinds(call.Args[1]) {
					if !direct[k] {
						out = append(out, fmt.Sprintf("%s: %s: a %s row written straight to the store, around the audit cap", pos, fn.name, k))
					}
				}
			case "recordPasswordless":
				for _, k := range kinds(call.Args[1]) {
					seen[k]++
					if !passwordless[k] {
						out = append(out, fmt.Sprintf("%s: %s: a %s row routed through the capped writer", pos, fn.name, k))
					}
				}
			}
			return true
		})
	}
	if capCalls != 1 {
		out = append(out, fmt.Sprintf("the capped writer calls the store %d time(s), want exactly once", capCalls))
	}
	for k := range passwordless {
		if seen[k] == 0 {
			out = append(out, fmt.Sprintf("no %s row goes through the capped writer", k))
		}
	}
	return out
}

// parsePackage parses this package's non-test sources, with one file's text replaced
// when override is given (the positive control's mutants).
func parsePackage(t *testing.T, overrideFile, overrideSrc string) (*token.FileSet, []*ast.File) {
	t.Helper()
	names, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, n := range names {
		if strings.HasSuffix(n, "_test.go") {
			continue
		}
		var src any
		if n == overrideFile {
			src = overrideSrc
		}
		f, err := parser.ParseFile(fset, n, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", n, err)
		}
		files = append(files, f)
	}
	if len(files) < 5 {
		t.Fatalf("parsed %d source file(s); the read has gone blind", len(files))
	}
	return fset, files
}

// TestAuditRows_EveryPasswordlessKindGoesThroughTheCap pins, in the source, that every
// write of a password-less pre-session row is charged against the process-wide audit
// cap and every totp_failed/locked row is not. The behaviour tests drive the arms they
// can reach; this reads ALL of them. Measured (OP-6 verification, 4th round): the rows
// of the enrollment's foreign-page, wrong-first-code and database-refusal arms written
// straight to the store left the package green, because the behaviour test drove the
// cap through the malformed-token arm only.
//
// The positive control applies five mutants to flow.go's text and requires the reader
// to flag each: an enrollment_failed row written around the cap, a totp_failed row
// routed through it, the refused-code arm's variable kind assigned a password-less
// value, the store's row writer taken as a method value and called through it, and the
// row written around the cap from a package-level func literal.
//
// COUNTED LIMITS: (1) the read is by NAME -- a new Store method that writes audit rows
// under another name is not seen; (2) a call through reflection is not seen; (3) a
// variable kind is resolved through the assignments in its own function or
// package-level declaration only, and anything it cannot resolve is flagged as "?"
// (fail-closed, not silence). The behaviour tests
// (TestLimits_ARefusedRequestWritesNoRowAndMovesNoCounter) drive the arms they reach.
// (The list is m10-platform.md, OP-6 card correction, md. 18, "Tek liste (11. tur)", S10.)
func TestAuditRows_EveryPasswordlessKindGoesThroughTheCap(t *testing.T) {
	fset, files := parsePackage(t, "", "")
	if v := auditRowViolations(fset, files); len(v) != 0 {
		t.Fatalf("audit rows that bypass or misuse the cap:\n%s", strings.Join(v, "\n"))
	}
	orig, err := os.ReadFile("flow.go")
	if err != nil {
		t.Fatalf("read flow.go: %v", err)
	}
	for name, m := range map[string][2]string{
		"enrollment_failed around the cap":  {`a.recordPasswordless(ctx, db.OperatorEnrollmentFailed, "", id)`, `a.store.RecordOperatorAuthEvent(ctx, db.OperatorEnrollmentFailed, "", id)`},
		"totp_failed through the cap":       {`a.store.RecordOperatorAuthEvent(ctx, db.OperatorTOTPFailed, "", id)`, `a.recordPasswordless(ctx, db.OperatorTOTPFailed, "", id)`},
		"a variable kind made login_failed": {`kind, out := db.OperatorTOTPFailed, ErrCodeRejected`, `kind, out := db.OperatorLoginFailed, ErrCodeRejected`},
		"the row writer as a method value": {`a.recordPasswordless(ctx, db.OperatorEnrollmentFailed, "", id)`,
			`func() error { rec := a.store.RecordOperatorAuthEvent; return rec(ctx, db.OperatorEnrollmentFailed, "", id) }()`},
		// The E4 arm alone, so the capped writer still carries enrollment_failed rows from
		// the other arms and only the package-level walk can see this one (a mutant that
		// removed the capped call entirely was flagged by the "no kind through the cap"
		// check instead -- measured: the walk reverted to function bodies stayed green).
		"a package-level func literal around the cap": {"\tkey, err := a.openPending(id, b)\n\tif err != nil {\n\t\treturn Issued{}, a.enrollmentRefused(ctx, id, ErrEnrollment)\n\t}",
			"\tkey, err := a.openPending(id, b)\n\tif err != nil {\n\t\tif rerr := recordAround(ctx, a, id); rerr != nil {\n\t\t\treturn Issued{}, rerr\n\t\t}\n\t\treturn Issued{}, ErrEnrollment\n\t}"},
	} {
		if strings.Count(string(orig), m[0]) != 1 {
			t.Fatalf("POSITIVE CONTROL %q: the text to mutate is not in flow.go exactly once", name)
		}
		src := strings.Replace(string(orig), m[0], m[1], 1)
		if strings.Contains(m[1], "recordAround(") {
			src += "\nvar recordAround = func(ctx context.Context, a *Authenticator, id uuid.UUID) error {\n\treturn a.store.RecordOperatorAuthEvent(ctx, db.OperatorEnrollmentFailed, \"\", id)\n}\n"
		}
		fset, files := parsePackage(t, "flow.go", src)
		if len(auditRowViolations(fset, files)) == 0 {
			t.Errorf("POSITIVE CONTROL %q: the mutant was not flagged", name)
		}
	}
}
