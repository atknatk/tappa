package adminauth

import (
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/config"
)

// reset_test.go -- the IN-PACKAGE half of the reset credential's proof: the parts
// that need the unexported hash and key derivation. The §4.7 rendering proof lives in
// resetleak_external_test.go, from outside, for the M5-01 reason.

// resetKey is a fixed 32-byte key. It is a test key and protects nothing.
func resetKey() []byte {
	k := make([]byte, hmacKeyLen)
	for i := range k {
		k[i] = byte(i + 1)
	}
	return k
}

// tokenHashShape is the constraint migration 00019 puts on password_resets.token_hash.
//
// 🔴 IT IS TYPED OUT HERE FROM THE MIGRATION RATHER THAN READ OUT OF IT. A test that
// derived the pattern from the same place the database does would prove the two agree
// with themselves. This literal is the assertion: if 00019's CHECK and this package's
// output ever diverge, one of the two files has to change and a human has to notice.
var tokenHashShape = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TestResetToken_HashHasTheShapeTheColumnDemands is the joint between the Go side and
// migration 00019: the column refuses anything that is not 64 lower-case hex, so a
// generator that produced anything else would make every reset fail at INSERT time
// with a check violation instead of at review time.
func TestResetToken_HashHasTheShapeTheColumnDemands(t *testing.T) {
	for i := 0; i < 16; i++ {
		tok, err := newResetToken()
		if err != nil {
			t.Fatalf("newResetToken: %v", err)
		}
		h, err := tok.hash(resetKey())
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		if !tokenHashShape.MatchString(h) {
			t.Fatalf("hash = %q, want ^[0-9a-f]{64}$ (00019's CHECK)", h)
		}
	}
}

// TestResetToken_EntropyIsTheDefence pins the constant migration 00019 depends on and
// cannot check. That migration carries NO lockout state for password_resets, which is
// sound only while the token is >= 128 bits of cryptographic randomness; the hash of a
// six-digit code is 64 hex characters exactly like this one's, so no CHECK can notice
// the difference.
func TestResetToken_EntropyIsTheDefence(t *testing.T) {
	if resetTokenBytes*8 < 128 {
		t.Fatalf("resetTokenBytes = %d (%d bits): migration 00019 assumes >= 128 bits and carries no lockout state",
			resetTokenBytes, resetTokenBytes*8)
	}
	tok, err := newResetToken()
	if err != nil {
		t.Fatalf("newResetToken: %v", err)
	}
	raw := tok.reveal()
	if len(raw) != resetTokenLen {
		t.Fatalf("token length = %d, want %d", len(raw), resetTokenLen)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("token is not base64url: %v", err)
	}
	if len(decoded) != resetTokenBytes {
		t.Fatalf("decoded %d bytes, want %d", len(decoded), resetTokenBytes)
	}

	// Two draws must differ. A generator wired to a constant would satisfy every
	// assertion above.
	other, err := newResetToken()
	if err != nil {
		t.Fatalf("newResetToken: %v", err)
	}
	if other.reveal() == raw {
		t.Fatal("two draws produced the same token")
	}
}

// TestResetAndSessionHashes_Differ is the whole point of resetTokenKeyLabel: the SAME
// 43-character string must hash to a DIFFERENT value for a reset link than for a panel
// cookie.
//
// WHY IT MATTERS STRUCTURALLY. Without the separate label the two hashing functions
// would be identical, and a value that is a valid admin_sessions.token_hash would also
// be a valid password_resets.token_hash lookup key. The separation of the two
// credentials would then rest entirely on the right function being called against the
// right table -- discipline, where the schema (two tables, two resolvers) is trying to
// give structure.
func TestResetAndSessionHashes_Differ(t *testing.T) {
	sessionKey := make([]byte, hmacKeyLen)
	for i := range sessionKey {
		sessionKey[i] = byte(0xA0 + i)
	}
	rk, err := deriveResetKey(sessionKey)
	if err != nil {
		t.Fatalf("deriveResetKey: %v", err)
	}
	sk, err := deriveKey(sessionKey)
	if err != nil {
		t.Fatalf("deriveKey: %v", err)
	}
	if string(rk) == string(sk) {
		t.Fatal("the reset key equals the session key: the labels are not separating anything")
	}

	// The same raw value through both credential types.
	raw := strings.Repeat("A", resetTokenLen)
	resetHash, err := ParseResetToken(raw).hash(rk)
	if err != nil {
		t.Fatalf("reset hash: %v", err)
	}
	sessionHash, err := wrap(raw).hash(sk)
	if err != nil {
		t.Fatalf("session hash: %v", err)
	}
	if resetHash == sessionHash {
		t.Fatal("a reset token and a panel cookie with the same value hash to the same thing")
	}

	// AND THE SAME KEY MUST NOT BE REACHABLE BY ACCIDENT: hashing a reset token under
	// the SESSION key must give yet another value, so a call site that grabbed the
	// wrong key produces a hash that matches no row rather than one that matches the
	// wrong row.
	crossed, err := ParseResetToken(raw).hash(sk)
	if err != nil {
		t.Fatalf("crossed hash: %v", err)
	}
	if crossed == resetHash {
		t.Fatal("the reset hash does not depend on the reset key")
	}
}

// TestResetToken_ShapeGateRunsBeforeAnyHMAC covers the cheap refusals: a junk URL
// parameter must cost nothing and must never reach the database.
func TestResetToken_ShapeGateRunsBeforeAnyHMAC(t *testing.T) {
	for name, v := range map[string]string{
		"empty":                 "",
		"too short":             strings.Repeat("a", resetTokenLen-1),
		"too long":              strings.Repeat("a", resetTokenLen+1),
		"right length, bad set": strings.Repeat("!", resetTokenLen),
		"a hex hash, not a token": "0123456789abcdef0123456789abcdef0123456789abcdef" +
			"0123456789abcdef",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseResetToken(v).hash(resetKey()); err == nil {
				t.Fatal("a malformed value was hashed")
			}
		})
	}
	// The zero value must fail closed rather than panic.
	if _, err := (ResetToken{}).hash(resetKey()); err == nil {
		t.Fatal("the zero ResetToken produced a hash")
	}
	// POSITIVE CONTROL: a real token still hashes. The input is produced by the
	// generator, not by the gate, so this cannot be satisfied by a gate that rejects
	// everything.
	tok, err := newResetToken()
	if err != nil {
		t.Fatalf("newResetToken: %v", err)
	}
	if _, err := tok.hash(resetKey()); err != nil {
		t.Fatalf("a real token was refused: %v -- every refusal above is then vacuous", err)
	}
}

// TestResetToken_RefusesAWrongSizedKey. A short or empty key still produces
// plausible-looking hex, so a silent degradation here would stay invisible until
// somebody noticed reset links were forgeable.
func TestResetToken_RefusesAWrongSizedKey(t *testing.T) {
	tok, err := newResetToken()
	if err != nil {
		t.Fatalf("newResetToken: %v", err)
	}
	for _, n := range []int{0, 1, hmacKeyLen - 1, hmacKeyLen + 1} {
		if _, err := tok.hash(make([]byte, n)); err == nil {
			t.Fatalf("a %d-byte key produced a hash", n)
		}
	}
	if _, err := deriveResetKey(make([]byte, hmacKeyLen-1)); err == nil {
		t.Fatal("deriveResetKey accepted a short session key")
	}
	if _, err := NewResets(nil, &config.Config{SessionHMACKey: make([]byte, hmacKeyLen)}); err == nil {
		t.Fatal("NewResets accepted a nil database")
	}
}

// TestResetTTL_FitsUnderTheSchemaCeiling pins the relationship between the product TTL
// and migration 00019's CHECK. A ResetTTL past the ceiling would not be a slow bug: it
// would make EVERY reset request fail with a check violation at INSERT time.
//
// ⚠️ THE SLACK IS ALSO THE CLOCK-SKEW BUDGET. expires_at comes from THIS process's
// clock while created_at defaults to Postgres's now(), so the two must agree to within
// (ceiling - ResetTTL). The number below is that budget, stated so a future TTL change
// is made with it in view.
func TestResetTTL_FitsUnderTheSchemaCeiling(t *testing.T) {
	const schemaCeiling = 24 * time.Hour // migration 00019, password_resets_ttl_ceiling
	if ResetTTL <= 0 {
		t.Fatalf("ResetTTL = %v, want a positive duration", ResetTTL)
	}
	if ResetTTL > schemaCeiling {
		t.Fatalf("ResetTTL = %v exceeds the schema ceiling %v: every reset would fail with SQLSTATE 23514",
			ResetTTL, schemaCeiling)
	}
	if slack := schemaCeiling - ResetTTL; slack < time.Hour {
		t.Fatalf("clock-skew budget = %v, want at least an hour between the product TTL and the schema ceiling", slack)
	}
}
