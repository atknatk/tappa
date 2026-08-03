package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
)

// logincontext_test.go -- the signed verified-candidate set, the mechanism PHASE B
// OBLIGATION 5 rests on. Everything here is deterministic and needs no database:
// the question is whether a value this server did not mint, or minted for another
// browser, or minted too long ago, can ever be believed.

func newTestChoices(t *testing.T, now func() time.Time) adminChoices {
	t.Helper()
	c, err := newAdminChoices(adminTestConfig())
	if err != nil {
		t.Fatalf("newAdminChoices: %v", err)
	}
	c.now = now
	return c
}

func twoVerified() []adminauth.Verified {
	return []adminauth.Verified{
		{AdminUserID: uuid.MustParse("aaaaaaaa-0000-4000-8000-000000000001"), TenantID: uuid.MustParse("11111111-0000-4000-8000-000000000001")},
		{AdminUserID: uuid.MustParse("bbbbbbbb-0000-4000-8000-000000000002"), TenantID: uuid.MustParse("22222222-0000-4000-8000-000000000002")},
	}
}

// TestAdminChoices_RoundTrip is the happy path plus the vacuity guard: the value
// must actually carry the identities, not merely verify.
func TestAdminChoices_RoundTrip(t *testing.T) {
	c := newTestChoices(t, nil)
	want := twoVerified()
	blob, err := c.mint(want, "the-binding-value")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	got, err := c.parse(blob, "the-binding-value")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d identities, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("identity %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	// The blob must not be readable as plain text: it is base64, and the ids are
	// inside the payload half. This is not a confidentiality claim (base64 is not
	// encryption) — it is a check that the format is the one documented.
	if !strings.Contains(blob, ".") {
		t.Fatalf("blob has no signature separator: %q", blob)
	}
}

// TestAdminChoices_RefusesEverythingItDidNotMint is the table the whole mechanism
// exists for.
func TestAdminChoices_RefusesEverythingItDidNotMint(t *testing.T) {
	c := newTestChoices(t, nil)
	valid, err := c.mint(twoVerified(), "the-binding-value")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	payload, sig, _ := strings.Cut(valid, ".")

	// A payload that names a THIRD tenant, encoded correctly, signed with the
	// signature of the REAL one. This is the attack shape: an attacker who sees a
	// blob and edits the tenant list.
	forgedPayload := base64.RawURLEncoding.EncodeToString(
		[]byte("1|" + timestampOf(t, payload) + "|" +
			"cccccccc-0000-4000-8000-000000000003:33333333-0000-4000-8000-000000000003"))

	tests := []struct {
		name  string
		blob  string
		bind  string
		wantE error
	}{
		{"an edited payload with the original signature", forgedPayload + "." + sig, "the-binding-value", errChoiceSignature},
		{"a different binding value (another browser)", valid, "some-other-binding", errChoiceSignature},
		{"an empty binding value", valid, "", errChoiceSignature},
		{"no signature at all", payload, "the-binding-value", errChoiceMalformed},
		{"an empty signature", payload + ".", "the-binding-value", errChoiceMalformed},
		{"an empty payload", "." + sig, "the-binding-value", errChoiceMalformed},
		{"a truncated signature", payload + "." + sig[:len(sig)-4], "the-binding-value", errChoiceSignature},
		{"a signature that is not base64", payload + ".!!!!", "the-binding-value", errChoiceMalformed},
		{"a payload that is not base64", "!!!!." + sig, "the-binding-value", errChoiceMalformed},
		{"nothing at all", "", "the-binding-value", errChoiceMalformed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.parse(tc.blob, tc.bind)
			if !errors.Is(err, tc.wantE) {
				t.Fatalf("err = %v, want %v", err, tc.wantE)
			}
			if len(got) != 0 {
				t.Fatalf("parse returned %d identities alongside an error", len(got))
			}
		})
	}

	// POSITIVE CONTROL: the untouched blob with the right binding still verifies,
	// so the refusals above are about the mutations and not about parse being
	// broken.
	if _, err := c.parse(valid, "the-binding-value"); err != nil {
		t.Fatalf("control: the untouched blob failed to parse: %v", err)
	}
}

// timestampOf digs the issued-at field out of a payload so the forged one can
// carry a fresh timestamp — otherwise the forgery would be refused for being
// expired and the test would prove the wrong thing.
func timestampOf(t *testing.T, encoded string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 {
		t.Fatalf("payload has %d parts, want 3", len(parts))
	}
	return parts[1]
}

// TestAdminChoices_Expiry: the blob is a bearer credential for the accounts inside
// it, so its window is minutes.
func TestAdminChoices_Expiry(t *testing.T) {
	base := time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		at      time.Time
		wantErr error
	}{
		{"immediately", base, nil},
		{"one second before the ttl", base.Add(adminChoiceTTL - time.Second), nil},
		{"one second after the ttl", base.Add(adminChoiceTTL + time.Second), errChoiceExpired},
		{"an hour later", base.Add(time.Hour), errChoiceExpired},
		{"inside the future skew (a clock step back)", base.Add(-adminChoiceFutureSkew + time.Second), nil},
		{"beyond the future skew", base.Add(-adminChoiceFutureSkew - time.Second), errChoiceExpired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			minted := newTestChoices(t, func() time.Time { return base })
			blob, err := minted.mint(twoVerified(), "the-binding-value")
			if err != nil {
				t.Fatalf("mint: %v", err)
			}
			at := tc.at
			read := newTestChoices(t, func() time.Time { return at })
			_, err = read.parse(blob, "the-binding-value")
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("parse: %v", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestAdminChoices_MintRefusesDegenerateInput.
func TestAdminChoices_MintRefusesDegenerateInput(t *testing.T) {
	c := newTestChoices(t, nil)
	tooMany := make([]adminauth.Verified, adminChoiceMaxEntries+1)
	for i := range tooMany {
		tooMany[i] = adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	}
	tests := []struct {
		name     string
		verified []adminauth.Verified
		bind     string
	}{
		{"an empty verified set", nil, "the-binding-value"},
		{"no binding value", twoVerified(), ""},
		{"more identities than the parser will read back", tooMany, "the-binding-value"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.mint(tc.verified, tc.bind); err == nil {
				t.Fatalf("mint accepted %s", tc.name)
			}
		})
	}
}

// TestAdminChoices_ZeroValueSignsNothing. A zero adminChoices holds no key, and a
// MAC under an empty key is one anybody can compute — so both directions must
// refuse rather than emit a decorative signature. (adminauth.Cookies can make its
// zero value SAFE because there is a safe default; there is no safe default key.)
func TestAdminChoices_ZeroValueSignsNothing(t *testing.T) {
	var c adminChoices
	if _, err := c.mint(twoVerified(), "bind"); err == nil {
		t.Fatalf("the zero adminChoices minted a blob")
	}
	if _, err := c.parse("anything.atall", "bind"); err == nil {
		t.Fatalf("the zero adminChoices parsed a blob")
	}
}

// TestNewAdminChoices_RefusesAWrongSizedKey. A short or empty key still produces
// plausible-looking output, so the failure must be at construction.
func TestNewAdminChoices_RefusesAWrongSizedKey(t *testing.T) {
	tests := []struct {
		name string
		cfg  *config.Config
	}{
		{"nil config", nil},
		{"no key", &config.Config{Env: config.EnvDev}},
		{"short key", &config.Config{Env: config.EnvDev, SessionHMACKey: []byte("too-short")}},
		{"long key", &config.Config{Env: config.EnvDev, SessionHMACKey: make([]byte, 64)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := newAdminChoices(tc.cfg); err == nil {
				t.Fatalf("newAdminChoices accepted %s", tc.name)
			}
		})
	}
}

// TestAdminChoices_KeyIsDerived. The blob is signed under a
// LABELLED derivation of the session HMAC key, so a value minted for one purpose
// cannot be presented to another (the domain-separation discipline
// internal/invite and tapcontext already apply).
func TestAdminChoices_KeyIsDerived(t *testing.T) {
	c, err := newAdminChoices(adminTestConfig())
	if err != nil {
		t.Fatalf("newAdminChoices: %v", err)
	}
	if string(c.key) == string(adminTestConfig().SessionHMACKey) {
		t.Fatalf("the choice blob is signed under the raw session key")
	}
	// A different session key must produce different signatures over identical
	// input, or the derivation is not keyed at all.
	other, err := newAdminChoices(&config.Config{
		Env: config.EnvDev, BaseURL: testBaseURL,
		SessionHMACKey: []byte("fedcba9876543210fedcba9876543210"),
	})
	if err != nil {
		t.Fatalf("newAdminChoices: %v", err)
	}
	a, err := c.mint(twoVerified(), "bind")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, err := other.parse(a, "bind"); !errors.Is(err, errChoiceSignature) {
		t.Fatalf("a blob signed under one key parsed under another: %v", err)
	}
}

// TestSelectVerified is the second half of OBLIGATION 5 in isolation: membership
// of the AUTHENTICATED set, and nothing else, decides which identity is used.
func TestSelectVerified(t *testing.T) {
	set := twoVerified()
	tests := []struct {
		name     string
		verified []adminauth.Verified
		tenantID uuid.UUID
		wantOK   bool
		wantID   uuid.UUID
	}{
		{"the first member", set, set[0].TenantID, true, set[0].AdminUserID},
		{"the second member", set, set[1].TenantID, true, set[1].AdminUserID},
		{"a tenant nobody verified", set, uuid.MustParse("99999999-0000-4000-8000-000000000009"), false, uuid.Nil},
		{"the nil uuid", set, uuid.Nil, false, uuid.Nil},
		{"an empty set", nil, set[0].TenantID, false, uuid.Nil},
		{
			"a duplicated tenant (impossible through the resolver, refused anyway)",
			[]adminauth.Verified{set[0], set[0]}, set[0].TenantID, false, uuid.Nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := selectVerified(tc.verified, tc.tenantID)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if got.AdminUserID != tc.wantID {
				t.Fatalf("admin = %v, want %v", got.AdminUserID, tc.wantID)
			}
		})
	}
}

// TestAdminLoginState_CSRFMatching.
func TestAdminLoginState_CSRFMatching(t *testing.T) {
	st := adminLoginState{csrf: "the-token", bind: "the-binding"}
	tests := []struct {
		name string
		sent string
		want bool
	}{
		{"the right token", "the-token", true},
		{"a wrong token of the same length", "the-tokeX", false},
		{"a prefix", "the-toke", false},
		{"empty", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := st.csrfMatches(tc.sent); got != tc.want {
				t.Fatalf("csrfMatches = %v, want %v", got, tc.want)
			}
		})
	}
	// The zero state matches nothing, so a missing cookie can never satisfy the
	// check by carrying two empty strings.
	if (adminLoginState{}).csrfMatches("") {
		t.Fatalf("the zero login state matched an empty token")
	}
}

// TestNewAdminLoginState_TwoIndependentValues. cookies.go's rule: a synchronizer
// token DERIVED from the cookie is a token an attacker who plants the cookie can
// compute, which makes the measure decorative.
func TestNewAdminLoginState_TwoIndependentValues(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 32; i++ {
		st, err := newAdminLoginState()
		if err != nil {
			t.Fatalf("newAdminLoginState: %v", err)
		}
		if st.csrf == "" || st.bind == "" {
			t.Fatalf("an empty half: csrf=%d bind=%d", len(st.csrf), len(st.bind))
		}
		if st.csrf == st.bind {
			t.Fatalf("the two halves are equal — the binding value is the rendered token")
		}
		if seen[st.csrf] || seen[st.bind] {
			t.Fatalf("a value repeated after %d draws", i)
		}
		seen[st.csrf], seen[st.bind] = true, true
	}
}

// TestAdminChoices_MACIsUnambiguousAcrossThePayloadBoundary is the net for the
// length prefix in sign().
//
// THE CONCATENATION TRAP: with a MAC input of `label || payload || "|" || bind`,
// an attacker who controls both halves can look for a DIFFERENT split of the same
// bytes. An audit walked every '|' boundary of a real blob and found none that
// verified — but only because parse demands exactly three payload fields, i.e. the
// safety came from a different function and would vanish when a fourth field is
// added.
//
// This test asserts the property DIRECTLY on the signing function, with no help
// from parse: for every way of moving bytes across the payload/bind boundary, the
// MAC must differ. It stays meaningful whatever shape the payload later takes.
func TestAdminChoices_MACIsUnambiguousAcrossThePayloadBoundary(t *testing.T) {
	c := newTestChoices(t, nil)

	// THE CASES ARE GENERATED FROM ONE STRING, NOT HAND-WRITTEN, and that is a
	// correction rather than a flourish. The first version of this table listed
	// pairs by hand and TWO of the five were not ambiguities at all: absorbing a
	// whole bind ("x|y", "") leaves a TRAILING separator, so the two inputs simply
	// differ. The vacuity guard caught them, which is what a vacuity guard is for —
	// but a hand-written table invites the same slip again.
	//
	// Splitting ONE string at each of its separators is exactly the set of
	// (payload, bind) pairs that concatenate to it, so every pair below is a
	// genuine ambiguity BY CONSTRUCTION and no future edit can smuggle in a
	// non-case. The string is shaped like a real payload plus a binding value.
	const whole = "1|100|aaaa:bbbb|cccc|the-binding-value"

	var splits [][2]string
	for i := 0; i < len(whole); i++ {
		if whole[i] == '|' {
			splits = append(splits, [2]string{whole[:i], whole[i+1:]})
		}
	}
	if len(splits) < 3 {
		t.Fatalf("the sample string has %d separators; the test needs at least 3", len(splits))
	}
	t.Logf("%d split points over %q — every ordered pair must sign differently", len(splits), whole)

	seen := map[string][2]string{}
	for _, s := range splits {
		name := fmt.Sprintf("payload=%q bind=%q", s[0], s[1])
		t.Run(name, func(t *testing.T) {
			// Vacuity guard, kept even though construction guarantees it: a future
			// edit to the generator must not be able to silence the test.
			if s[0]+"|"+s[1] != whole {
				t.Fatalf("split does not reconstruct the sample: %q + | + %q", s[0], s[1])
			}
			mac := string(c.sign(s[0], s[1]))
			if prev, clash := seen[mac]; clash {
				t.Fatalf("(%q, %q) and (%q, %q) produce the SAME MAC — the signing input is "+
					"ambiguous across the payload/bind boundary",
					prev[0], prev[1], s[0], s[1])
			}
			seen[mac] = s
		})
	}
	if len(seen) != len(splits) {
		t.Fatalf("%d distinct MACs over %d distinct splits", len(seen), len(splits))
	}
}

// TestAdminChoices_LengthPrefixIsWhatRemovesTheAmbiguity is the POSITIVE CONTROL
// for the test above: without a length prefix, those same pairs collide. It
// recomputes the OLD construction by hand, so a reader can see the defect the
// prefix removes rather than take it on trust.
func TestAdminChoices_LengthPrefixIsWhatRemovesTheAmbiguity(t *testing.T) {
	c := newTestChoices(t, nil)
	oldSign := func(payload, bind string) []byte {
		m := hmac.New(sha256.New, c.key)
		m.Write([]byte(adminChoiceMACLabel))
		m.Write([]byte(payload))
		m.Write([]byte("|"))
		m.Write([]byte(bind))
		return m.Sum(nil)
	}
	a := oldSign("1|100|aa", "b|c")
	b := oldSign("1|100|aa|b", "c")
	if string(a) != string(b) {
		t.Fatalf("control: the pre-fix construction did NOT collide, so the length prefix " +
			"removes nothing and this test proves the wrong thing")
	}
	// And the shipped construction does not.
	if string(c.sign("1|100|aa", "b|c")) == string(c.sign("1|100|aa|b", "c")) {
		t.Fatalf("the shipped signing function still collides")
	}
}

// TestAdminChoiceMaxEntries_TracksTheBcryptCap is the net for the cross-package
// equality that used to be a comment.
//
// 🔴 THE BUG IT CLOSES, measured by an audit: adminChoiceMaxEntries was its own
// literal 8 beside a claim that it "equals the bcrypt candidate cap by
// construction". Raising adminauth's cap alone gave
//
//	8 verified candidates -> 303, Location "/admin/login/choose"
//	9 verified candidates -> 500, Location ""
//
// i.e. a legitimate operator with nine businesses could not sign in, and no test
// noticed. The constant is now DERIVED, so this test is a tripwire for anyone who
// turns it back into a literal.
func TestAdminChoiceMaxEntries_TracksTheBcryptCap(t *testing.T) {
	if adminChoiceMaxEntries != adminauth.MaxCandidates {
		t.Fatalf("adminChoiceMaxEntries = %d but adminauth.MaxCandidates = %d. A verified set "+
			"can never be larger than the set that was compared, so these must not be two "+
			"independent numbers — derive one from the other",
			adminChoiceMaxEntries, adminauth.MaxCandidates)
	}
	// And a FULL-SIZE verified set really round-trips, so the boundary is exercised
	// rather than merely asserted.
	c := newTestChoices(t, nil)
	full := make([]adminauth.Verified, adminauth.MaxCandidates)
	for i := range full {
		full[i] = adminauth.Verified{AdminUserID: uuid.New(), TenantID: uuid.New()}
	}
	blob, err := c.mint(full, "the-binding-value")
	if err != nil {
		t.Fatalf("minting a blob at exactly the cap failed: %v — a legitimate operator with "+
			"%d businesses would get HTTP 500 at the picker", err, adminauth.MaxCandidates)
	}
	got, err := c.parse(blob, "the-binding-value")
	if err != nil {
		t.Fatalf("parsing a full-size blob failed: %v", err)
	}
	if len(got) != adminauth.MaxCandidates {
		t.Fatalf("round-tripped %d identities, want %d", len(got), adminauth.MaxCandidates)
	}
}

// TestAdminChoices_ParseRefusesAWrongFieldCount pins the strictness of parse's
// `len(parts) != 3`.
//
// 🔴 IT WAS UNNETTED: an audit relaxed it to `< 3` and the whole handler package
// stayed GREEN. The relaxation is not reachable as an attack today — the length
// prefix in sign() makes the MAC input injective, so a payload with a fourth field
// cannot be smuggled past the signature — and THAT defence is pinned
// (TestAdminChoices_MACIsUnambiguousAcrossThePayloadBoundary). But the strict count
// is the second half of the pair sign() explicitly relies on, and nothing held it.
//
// It is cheap: the blob is minted with the real key, then its PAYLOAD is rewritten
// with an extra field and re-signed with the same key, so the signature is VALID
// and only the field count is wrong. That is the exact shape a future author adding
// a fourth field would produce.
func TestAdminChoices_ParseRefusesAWrongFieldCount(t *testing.T) {
	c := newTestChoices(t, nil)
	stamp := strconv.FormatInt(time.Now().Truncate(time.Second).Unix(), 10)
	entry := "aaaaaaaa-0000-4000-8000-000000000001:11111111-0000-4000-8000-000000000001"

	tests := []struct {
		name    string
		payload string
	}{
		{"four fields — a future author adding one", "1|" + stamp + "|" + entry + "|extra"},
		{"two fields — a field removed", "1|" + stamp},
		{"one field", "1"},
		{"five fields", "1|" + stamp + "|" + entry + "|a|b"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Signed with the REAL key: only the field count is wrong.
			blob := base64.RawURLEncoding.EncodeToString([]byte(tc.payload)) + "." +
				base64.RawURLEncoding.EncodeToString(c.sign(tc.payload, "the-binding-value"))
			got, err := c.parse(blob, "the-binding-value")
			if !errors.Is(err, errChoiceMalformed) {
				t.Fatalf("parse accepted a %d-field payload (err = %v). sign()'s length prefix "+
					"and parse's exact field count are a PAIR — see the note on sign()",
					strings.Count(tc.payload, "|")+1, err)
			}
			if len(got) != 0 {
				t.Fatalf("parse returned %d identities alongside an error", len(got))
			}
		})
	}

	// POSITIVE CONTROL: the correct three-field payload, signed the same way,
	// parses — so the refusals above are about the COUNT and not about the
	// hand-built blob being unreadable.
	ok := "1|" + stamp + "|" + entry
	blob := base64.RawURLEncoding.EncodeToString([]byte(ok)) + "." +
		base64.RawURLEncoding.EncodeToString(c.sign(ok, "the-binding-value"))
	if _, err := c.parse(blob, "the-binding-value"); err != nil {
		t.Fatalf("control: a correctly shaped hand-built blob failed to parse: %v", err)
	}
}
