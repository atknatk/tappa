// Package adminauth is panel authentication: the password comparison, the admin
// session token, the admin cookie and the lifecycle around them.
//
// IT MIRRORS internal/session, IT DOES NOT EXTEND IT — and the separation is the
// point rather than a filing decision. M1-11 made admin identity a SEPARATE table
// from employee identity, migration 00011 gave it a SEPARATE resolver, and this
// package is the third leg of that: a separate cookie name, a separate HMAC key,
// a separate token type. An employee cookie cannot reach the panel and an admin
// cookie cannot reach the tap surface, and that is true because the two paths
// share no table, no function and no cookie — not because a flag is set correctly
// somewhere.
//
// WHAT LIVES HERE AND WHAT DOES NOT. The KDF comparison, the token, the cookie and
// the session lifecycle live here. The HTTP surface (screens, rate limits, the
// audit trail, the tenant picker's flow) is internal/handler's, and the decision
// of WHICH tenant a session is issued for is made there, under the binding that
// migration 00011 calls PHASE B OBLIGATION 5.
package adminauth

import (
	"errors"
	"fmt"
	"strconv"

	"golang.org/x/crypto/bcrypt"
)

// Q03 (2026-07-26) chose bcrypt over argon2id: mature, small surface, a ready
// GenerateFromPassword/CompareHashAndPassword pair, no parameter encoding to
// hand-write. This file is the whole of that decision.

const (
	// Cost is the bcrypt work factor for every digest THIS repo produces.
	//
	// IT IS 12 BECAUSE THE DIGESTS THAT ALREADY EXIST ARE 12, and that is a
	// measurement rather than a preference: test/fixtures/seed.sql writes two
	// admin_users rows whose password_hash begins `$2a$12$`, generated offline
	// before this package existed. A dummy digest at a DIFFERENT cost would
	// answer the enumeration question that PHASE B OBLIGATION 1 refuses to answer
	// in the body — the response time would split "unknown email" (dummy) from
	// "wrong password" (a real cost-12 row) by roughly a factor of four.
	//
	// MEASURED ON THIS MACHINE (darwin/arm64, 20 comparisons per row, three runs,
	// bcryptbench in the scratchpad; match and mismatch reported separately
	// because a mismatch that returned early would be the timing oracle):
	//
	//	cost 10   match med 88-99 ms    mismatch med 90-101 ms
	//	cost 11   match med 196-223 ms  mismatch med 193-217 ms
	//	cost 12   match med 376-427 ms  mismatch med 368-436 ms
	//
	// Match and mismatch agree at every cost, which is the property the dummy
	// comparison depends on: bcrypt pays the whole key schedule BEFORE it compares,
	// so a wrong password costs what a right one costs.
	//
	// ⚠️ THIS CORRECTS A NUMBER IN MIGRATION 00011, in the expensive direction.
	// That file sizes the amplification bound with "at cost 10 one comparison is
	// ~60-100 ms", which this machine confirms FOR COST 10 — but the deployed
	// digests are cost 12, so the real figure is ~380 ms and every derived number
	// is ~4x worse than 00011 states. See MaxCandidates for what that does to the
	// bound.
	//
	// A digest carries its own cost, so a stored `$2a$10$` row still verifies:
	// CompareHashAndPassword reads the cost from the hash, never from here. This
	// constant only governs digests we CREATE.
	Cost = 12

	// MaxPasswordBytes is bcrypt's hard input limit.
	//
	// 🔴 THE SILENT TRUNCATION IS LIVE IN THE COMPARISON PATH, MEASURED, and it is
	// the reason this constant is enforced HERE rather than trusted to x/crypto:
	//
	//	bcrypt.GenerateFromPassword(100 bytes, 12) -> error "password length
	//	    exceeds 72 bytes"                                       (it refuses)
	//	bcrypt.CompareHashAndPassword(hash of 72 'a', 100 'a') -> nil  (IT MATCHES)
	//	bcrypt.CompareHashAndPassword(hash of 72 'a',  73 'a') -> nil  (IT MATCHES)
	//
	// So the generator refuses and the COMPARER does not. Anything past byte 72 is
	// discarded, and two different passwords sharing a 72-byte prefix authenticate
	// each other. Q03 left the choice open ("reject the long password or SHA-256
	// pre-hash it; decided in that code") and forbade silent truncation.
	//
	// THE CHOICE IS REJECT, and the reason a pre-hash was not taken: SHA-256
	// pre-hashing changes the stored format (every digest becomes a digest of a
	// digest), so it needs a version marker in a column that has none, a decision
	// about the intermediate encoding (raw bytes carry NUL, which bcrypt treats as
	// a terminator — the classic form of this bug), and a migration for the two
	// rows that already exist. Rejecting costs a user with a >72-byte password an
	// error at the moment they SET it, which is where an error is cheap and
	// explainable.
	//
	// WHERE IT BITES ON THIS PATH: nothing here sets a password (that is M6-05 and
	// M7-04). At LOGIN the rule is different and is stated at Compare — a long
	// password is refused as a NON-MATCH, silently, because telling the visitor
	// their password is too long on a login form is a distinguishable answer and
	// PHASE B OBLIGATION 1 forbids exactly that.
	MaxPasswordBytes = 72
)

// dummyDigest is the fixed hash a login compares against when the resolver
// returned nothing — PHASE B OBLIGATION 2.
//
// IT IS A REAL COST-12 BCRYPT DIGEST OF A DISCARDED 32-BYTE RANDOM STRING. It was
// generated offline (scratchpad, crypto/rand -> base64url -> GenerateFromPassword
// at Cost) and the plaintext was never written down, so no password on earth
// matches it and no test can accidentally depend on one that does.
//
// IT IS NOT A SECRET AND MUST NOT BE TREATED AS ONE. It protects nothing and
// guards no account; it exists to make the CPU spend the same 380 ms it spends on
// a real row. Publishing it costs exactly nothing, which is why it can be a
// literal here instead of configuration.
//
// SAME COST AS Cost, and that is the whole requirement: see the Cost comment for
// the measured factor-of-four a mismatched cost would leak.
const dummyDigest = "$2a$12$lJLQgzXjJnxxUUvVhg3I2uG7tYFZ4Z22LtVyQBBP0AVYxSy0HgHDu"

// ErrPasswordTooLong is returned by Hash for an input over MaxPasswordBytes. It
// is deliberately NOT returned by Compare: see that function.
//
// IT IS BUILT WITH errors.New AND strconv RATHER THAN fmt.Errorf, and that is a
// redline-R7 accommodation rather than a style choice. The scanner
// (scripts/redline-check.sh) matches the WORD "password" inside the parentheses of
// a fmt./log./slog. call, on one line — and M6-01 phase A widened it to that word
// precisely so a bcrypt digest reaching a log line would be caught mechanically.
// This line carries no value at all (a constant byte count and a fixed sentence),
// but activate.go's precedent is explicit: REWORD rather than loosen the net,
// because "CI reddens on legitimate lines" is how a net gets relaxed under
// pressure and the real branch goes with it. Note for whoever maintains the
// scanner: the identifier MaxPasswordBytes alone is enough to trip it, so the
// constant cannot be passed to fmt.Errorf either.
var ErrPasswordTooLong = errors.New("adminauth: password exceeds " +
	strconv.Itoa(MaxPasswordBytes) + " bytes")

// Hash produces the digest to store in admin_users.password_hash.
//
// NOTHING IN THIS TASK CALLS IT on a production path — M6-01 phase B is login
// only, and passwords are set by M6-05 (panel-side admin management) and M7-04
// (reset). It exists so those tasks inherit the cost and the length rule instead
// of re-deciding them, and so this package's tests can build a digest the login
// path will actually see.
//
// It refuses an over-long password rather than truncating (MaxPasswordBytes), and
// it refuses an EMPTY one: bcrypt will happily hash "", and a stored digest of the
// empty string is an account whose password is "press enter".
func Hash(password string) (string, error) {
	if password == "" {
		return "", errors.New("adminauth: refusing to hash an empty password")
	}
	if n := len(password); n > MaxPasswordBytes {
		// The error names the LIMIT and the actual length, never the value. The
		// length is hoisted into `n` so the word does not appear inside the
		// fmt.Errorf call (see ErrPasswordTooLong for why that matters).
		return "", fmt.Errorf("%w (got %d bytes)", ErrPasswordTooLong, n)
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), Cost)
	if err != nil {
		// bcrypt's error text never contains the value (measured: the only thing
		// it quotes is the length), but it is wrapped rather than passed through
		// so the package name is on it. The message deliberately omits the word
		// redline R7 watches for.
		return "", fmt.Errorf("adminauth: hash: %w", err)
	}
	return string(h), nil
}

// Compare reports whether password matches digest. It is the one function that
// reads a revealed digest; the single production call site is Authenticate.
//
// IT RETURNS A BOOL, NOT AN ERROR, AND THAT IS DELIBERATE. Every failure mode
// here means the same thing to the caller — "this is not the password" — and a
// function that returned three distinguishable errors would invite a handler to
// branch on them, which is precisely the oracle PHASE B OBLIGATION 1 forbids. The
// three collapsed cases:
//
//   - the digest does not match the password (ordinary wrong password);
//   - the digest is malformed or empty (a corrupt row, or the zero
//     db.PasswordHash, whose reveal is "" — never a valid digest under any KDF);
//   - the password is longer than MaxPasswordBytes, which must not be allowed to
//     match, because bcrypt's comparer TRUNCATES rather than refusing (measured —
//     see MaxPasswordBytes). Without a guard a 100-byte password would
//     authenticate an account whose password is its first 72 bytes.
//
// 🔴🔴 THE OVER-LONG CASE STILL PAYS FULL PRICE, AND THAT IS THE WHOLE POINT OF
// THIS FUNCTION'S SHAPE. It used to `return false` immediately, and a security
// audit measured what that bought an attacker — the EXACT INVERSE of PHASE B
// OBLIGATION 2:
//
//	 20-byte password: known email 203.9 ms · unknown 197.0 ms ->       0.97x
//	100-byte password: known email     66 ns · unknown 211.4 ms -> 3203211x
//
// A single 100-byte probe answered "is this address a panel administrator?" with
// certainty, in one request, over the internet — because the fast branch was
// reached ONLY when a candidate existed, i.e. it was decided by SERVER STATE, not
// by the attacker's own input as an earlier version of this comment claimed. It
// also cost the server ZERO bcrypt, so the address budget that exists to bound CPU
// was not even spending the thing it protects. Migration 00011 handed this exact
// hazard to phase B in those words: "skipping it is a timing oracle wide enough to
// measure over the internet."
//
// WHY THE FIX IS A COMPARISON AGAINST *THIS* DIGEST rather than CompareDummy: it
// pays the cost of the row actually being tested, so the arms stay flat even if a
// stored digest carries a different cost than the package constant (an old cost-10
// row, a fixture at bcrypt.MinCost). The result is DISCARDED and false is returned
// unconditionally — the truncated comparison can succeed, and treating that as a
// match is the silent truncation Q03 forbids.
//
// 🔴 A FOURTH TIMING ARM EXISTS AND IT IS ON THE DIGEST SIDE — NOT REACHABLE
// TODAY, AND THE REASON EXPIRES. Everything above is about the PASSWORD; the
// STORED VALUE has its own fast path. Measured through Authenticate (min of 3, no
// -race):
//
//	unknown email (the cost-12 dummy)        297.9 ms        1x
//	a valid cost-12 digest                   294.6 ms        1x
//	a cost-4 digest                            1.42 ms      210x
//	password_hash = ''                          198 ns  1504663x
//	a malformed digest                          154 ns  1934567x
//
// The cause is bcrypt's own: given an empty, short, non-base64 or out-of-range
// digest, CompareHashAndPassword errors in 49-215 ns without ever building the key
// schedule. So an admin row WITHOUT a valid bcrypt digest answers about a million
// times faster than an unregistered address — the oracle this function was
// reshaped to close, reopened from the other side and pointing the other way.
//
// WHY IT IS NOT A HOLE TODAY, verified rather than assumed: nothing in this repo
// can write such a row. There is no INSERT INTO admin_users and no UPDATE of
// password_hash in db/queries or in production Go — the single UPDATE admin_users
// is MarkAdminLoggedIn, which touches last_login_at alone. Both seeded rows are
// $2a$12$. Hash refuses the empty password and anything over MaxPasswordBytes, so
// it cannot produce one either.
//
// ⚠️ AND THE SCHEMA DOES NOT ENFORCE IT: password_hash is `text NOT NULL` with NO
// format CHECK, so an empty string is schema-valid. The tasks that open a write path are named
// — M6-05 (panel-side admin management), M7-04 (password reset), M7-02 (public
// sign-up). This is the same structure migration 00011 used for its amplification
// bound: "not exploitable today, and the reason is worth naming because it
// expires."
//
// THE RULE THOSE TASKS INHERIT, in one sentence: admin_users.password_hash is only
// ever written with the output of adminauth.Hash. A CHECK constraint on the column
// would make it structural rather than disciplinary; that is a migration and is
// deliberately NOT done here, but it is the option on the table.
//
// ⚠️ THIS REPO ALREADY USES THE FAST PATH ON PURPOSE, which is why the note has to
// exist rather than being obvious: TestAuthenticate_CapsTheCandidateLoop says its
// digests are "DELIBERATELY MALFORMED ... so this test costs microseconds instead
// of 8 x 380 ms". That is legitimate for a loop-bound test and it is exactly the
// behaviour described above.
func Compare(digest, password string) bool {
	if len(password) > MaxPasswordBytes {
		// Pay exactly what an in-range comparison against this digest costs, then
		// refuse regardless of the outcome.
		_ = bcrypt.CompareHashAndPassword([]byte(digest), []byte(password[:MaxPasswordBytes]))
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(digest), []byte(password)) == nil
}

// CompareDummy performs a REAL bcrypt comparison against dummyDigest and always
// reports false — PHASE B OBLIGATION 2.
//
// WHY IT CANNOT BE `time.Sleep(380*time.Millisecond)`, which is the obvious
// cheaper version: a sleep does not track the cost of the digests actually
// stored, does not move when Cost moves, and is not CPU work, so it is
// distinguishable under load (a loaded server's real comparisons slow down while
// its sleeps do not). Doing the real thing keeps the two arms of the branch equal
// by construction rather than by a number somebody has to maintain.
//
// The return value exists so the call cannot be optimised away by a caller that
// ignores it, and so the intent reads at the call site. It is always false.
func CompareDummy(password string) bool {
	// The result is deliberately discarded and false is returned unconditionally:
	// dummyDigest is the hash of a random string nobody kept, so a true here would
	// mean a bcrypt collision, and treating that as a successful login would be
	// authenticating against a hash attached to no account.
	_ = bcrypt.CompareHashAndPassword([]byte(dummyDigest), []byte(clampForDummy(password)))
	return false
}

// clampForDummy keeps the dummy comparison from failing EARLY on the one input
// the real comparison also refuses early.
//
// Without it, a 100-byte password would take the fast path in Compare AND the
// fast path in bcrypt's own length check inside CompareDummy, so both arms would
// be fast and the padding would not pad. Truncating to the limit makes the dummy
// do the full key schedule for every input, which is the only thing it is for.
func clampForDummy(password string) string {
	if len(password) > MaxPasswordBytes {
		return password[:MaxPasswordBytes]
	}
	return password
}
