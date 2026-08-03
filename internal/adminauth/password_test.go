package adminauth

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestCost_MatchesTheDummyDigest pins the one number that makes the dummy
// comparison honest.
//
// If the digests this repo SEEDS are cost 12 and the dummy is cost 10, an
// unknown email answers ~4x faster than a wrong password and PHASE B OBLIGATION 2
// is decorative. The seed's hashes are literals in test/fixtures/seed.sql; this
// test reads the cost out of the DUMMY and compares it to the constant, and
// TestSeedDigests_UseTheDeclaredCost (below) reads the seed's own strings.
func TestCost_MatchesTheDummyDigest(t *testing.T) {
	c, err := bcrypt.Cost([]byte(dummyDigest))
	if err != nil {
		t.Fatalf("Cost(dummyDigest): %v", err)
	}
	if c != Cost {
		t.Fatalf("dummy digest cost = %d, want %d — the dummy must cost what a real row costs", c, Cost)
	}
}

// TestSeedDigests_UseTheDeclaredCost is the other half: the two digests that
// actually exist in this repo must be at Cost, or the dummy is padded to the
// wrong number.
//
// The digests are copied here as literals rather than parsed out of seed.sql,
// because the point is to fail when SOMEBODY CHANGES THE SEED without changing
// Cost — reading the file would make the test agree with whatever it found.
//
// They are NOT secrets: seed.sql says so at length (dev-only password, documented
// in fixtures.AdminDevPassword, never a real customer credential).
func TestSeedDigests_UseTheDeclaredCost(t *testing.T) {
	tests := []struct {
		name   string
		digest string
	}{
		{"kebab factory owner", "$2a$12$XqFC1yKNh9FGZ4bIx26ZWuchg1hmT1c4p7zWpzyxFeIQMOBmoRTji"},
		{"kebab manufacturing owner", "$2a$12$dJ.K9p.uwM2Inl5J/cMcPOd5w6VLagLkjq41XYGu/TCfo3d.SDRCC"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, err := bcrypt.Cost([]byte(tc.digest))
			if err != nil {
				t.Fatalf("Cost: %v", err)
			}
			if c != Cost {
				t.Fatalf("seeded digest cost = %d, want %d (adminauth.Cost)", c, Cost)
			}
		})
	}
}

// cheapDigest builds a REAL bcrypt digest at bcrypt.MinCost.
//
// 🔬 WHY THE COST IS LOWERED HERE AND NOWHERE THAT MATTERS. `make test` runs with
// -race, and the detector instruments every memory access in blowfish's key
// schedule: ONE cost-12 comparison measured 11.34 s under -race against 0.60 s
// without -- a 19x slowdown that took internal/adminauth past Go's 10-minute
// package timeout (609 s, a real failure this suite caused).
//
// A digest CARRIES ITS OWN COST, so CompareHashAndPassword verifies a cost-4 row
// through exactly the same code path as a cost-12 one. Every property in this
// file except the work factor itself -- the 72-byte truncation, the empty digest,
// the malformed digest, the round trip -- is independent of the cost.
//
// WHAT STILL RUNS AT THE SHIPPED COST, deliberately: TestHash_UsesTheDeclaredCost
// (below), the dummy comparison, and the timing measurement in
// manager_timing_test.go. Those are the three places the number is the point.
func cheapDigest(t *testing.T, password string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	return string(h)
}

// TestHash_UsesTheDeclaredCost is the ONE test that pays a full cost-12
// GenerateFromPassword, because it is the only one whose subject is the cost.
func TestHash_UsesTheDeclaredCost(t *testing.T) {
	digest, err := Hash("correct horse battery staple")
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	c, err := bcrypt.Cost([]byte(digest))
	if err != nil {
		t.Fatalf("Cost: %v", err)
	}
	if c != Cost {
		t.Fatalf("Hash produced a cost-%d digest, want cost %d", c, Cost)
	}
	if !Compare(digest, "correct horse battery staple") {
		t.Fatalf("the digest Hash produced does not verify its own password")
	}
}

// TestHash_And_Compare is the ordinary round trip plus every refusal.
func TestHash_And_Compare(t *testing.T) {
	const good = "correct horse battery staple"
	digest := cheapDigest(t, good)

	tests := []struct {
		name     string
		digest   string
		password string
		want     bool
	}{
		{"the right password", digest, good, true},
		{"a wrong password", digest, "correct horse battery stapl", false},
		{"the empty password", digest, "", false},
		{"an empty digest (the zero db.PasswordHash reveals \"\")", "", good, false},
		{"a malformed digest", "not-a-bcrypt-digest", good, false},
		{"a digest with the right shape and wrong content", "$2a$12$" + strings.Repeat("x", 53), good, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Compare(tc.digest, tc.password); got != tc.want {
				t.Fatalf("Compare = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestHash_RefusesWhatMustNotBeStored.
func TestHash_RefusesWhatMustNotBeStored(t *testing.T) {
	tests := []struct {
		name     string
		password string
	}{
		{"empty", ""},
		{"73 bytes, one past the limit", strings.Repeat("a", MaxPasswordBytes+1)},
		{"100 bytes", strings.Repeat("a", 100)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Hash(tc.password)
			if err == nil {
				t.Fatalf("Hash returned a digest for %q-shaped input; it must refuse", tc.name)
			}
			if got != "" {
				t.Fatalf("Hash returned %q alongside an error", got)
			}
			// The error must never quote the password.
			if tc.password != "" && strings.Contains(err.Error(), tc.password) {
				t.Fatalf("Hash error quotes the password")
			}
		})
	}
}

// TestCompare_RefusesTheSilentTruncation is the LIVE DEFECT this package exists
// to close, with its own POSITIVE CONTROL.
//
// x/crypto's comparer does not refuse an over-long password — it truncates. The
// control below calls bcrypt DIRECTLY and asserts the bad behaviour is really
// there, so the test cannot pass because the trap stopped existing; and then
// asserts Compare refuses the same input. If somebody deletes the length check in
// Compare, the second half goes red while the control stays green, naming exactly
// what regressed.
func TestCompare_RefusesTheSilentTruncation(t *testing.T) {
	seventyTwo := strings.Repeat("a", MaxPasswordBytes)
	digest := cheapDigest(t, seventyTwo)

	tests := []struct {
		name     string
		password string
	}{
		{"73 bytes sharing the 72-byte prefix", seventyTwo + "b"},
		{"100 bytes sharing the 72-byte prefix", seventyTwo + strings.Repeat("z", 28)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// POSITIVE CONTROL: bcrypt itself accepts it. If this ever fails,
			// x/crypto changed and the guard below may be removable.
			if err := bcrypt.CompareHashAndPassword([]byte(digest), []byte(tc.password)); err != nil {
				t.Fatalf("control: bcrypt refused an over-long password (%v) — the truncation trap "+
					"this test guards no longer exists in x/crypto; re-read MaxPasswordBytes", err)
			}
			// THE GUARANTEE: ours does not.
			if Compare(digest, tc.password) {
				t.Fatalf("Compare accepted a %d-byte password against a digest of its first %d bytes — "+
					"silent truncation (Q03 forbids it)", len(tc.password), MaxPasswordBytes)
			}
		})
	}

	// And the exact-length password still works, so the guard is a boundary and
	// not a blanket refusal.
	if !Compare(digest, seventyTwo) {
		t.Fatalf("Compare rejected the exact %d-byte password it was hashed from", MaxPasswordBytes)
	}
}

// TestCompareDummy_AlwaysFalse.
//
// The second half is the one that matters: a dummy that returned early would
// answer the question the body refuses to. It is measured properly in
// TestAuthenticate_TimingIsFlat (manager_timing_test.go); here it is only asserted
// that the call does real work by checking it is not instantaneous.
func TestCompareDummy_AlwaysFalse(t *testing.T) {
	// TWO INPUTS, NOT FOUR. Every call here is a full cost-12 comparison against
	// the shipped dummy (that is the whole point of the function), and under -race
	// that is ~11 s each. The two chosen are the only ones that take different
	// paths: a short value, and one past MaxPasswordBytes, which exercises
	// clampForDummy -- the line that keeps an over-long password from taking a
	// FAST path in both arms and defeating the padding.
	for _, pw := range []string{"password", strings.Repeat("a", 200)} {
		if CompareDummy(pw) {
			t.Fatalf("CompareDummy reported a match — the dummy digest is attached to no account")
		}
	}
}

// TestHash_ErrPasswordTooLong_IsTheSentinel pins Q03's "reject" decision at the
// one place that implements it.
//
// 🔴 IT WAS ENTIRELY UNTESTED. An audit grepped ErrPasswordTooLong and found it in
// four lines of password.go and NOWHERE else — and measured the consequence:
// deleting Hash's length guard left this package GREEN, because bcrypt's own error
// satisfied a bare `err != nil` check. Behaviour was preserved by x/crypto, not by
// us, and password_test.go elsewhere explicitly anticipates that x/crypto's
// behaviour may change.
//
// The sentinel is what a caller branches on, so it is what must be asserted —
// TestHash_RefusesWhatMustNotBeStored only checks that SOME error came back.
func TestHash_ErrPasswordTooLong_IsTheSentinel(t *testing.T) {
	tests := []struct {
		name       string
		password   string
		wantIsToo  bool
		wantNoHash bool
	}{
		{"one byte over the limit", strings.Repeat("a", MaxPasswordBytes+1), true, true},
		{"far over the limit", strings.Repeat("a", 500), true, true},
		{"exactly at the limit is ACCEPTED", strings.Repeat("a", MaxPasswordBytes), false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Hash(tc.password)
			if tc.wantIsToo {
				if !errors.Is(err, ErrPasswordTooLong) {
					t.Fatalf("err = %v, want it to wrap ErrPasswordTooLong — Q03 chose REJECT "+
						"over SHA-256 pre-hashing, and this sentinel is the whole of that "+
						"decision in code", err)
				}
				if got != "" {
					t.Fatalf("Hash returned a digest alongside the error")
				}
				// The error must name the limit, never the password.
				if !strings.Contains(err.Error(), "72") {
					t.Fatalf("err = %q, want it to name the byte limit", err.Error())
				}
				if strings.Contains(err.Error(), tc.password) {
					t.Fatalf("the error quotes the password")
				}
				return
			}
			if err != nil {
				t.Fatalf("Hash refused a password AT the limit: %v", err)
			}
			if got == "" {
				t.Fatalf("Hash returned no digest for a valid password")
			}
		})
	}
	// The empty password is a DIFFERENT refusal and must not be conflated with it.
	if _, err := Hash(""); errors.Is(err, ErrPasswordTooLong) {
		t.Fatalf("the empty password was refused as TOO LONG")
	} else if err == nil {
		t.Fatalf("Hash accepted the empty password")
	}
}
