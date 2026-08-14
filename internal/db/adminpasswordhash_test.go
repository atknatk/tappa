package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/atknatk/tappa/test/fixtures"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

// adminpasswordhash_test.go -- migration 00018's proof.
//
// WHAT IS ACTUALLY BEING PROVEN, because "the column matches a regex" is not it.
// internal/adminauth/password.go measured that bcrypt rejects an empty, short,
// non-base64 or out-of-range digest in 150-200 NANOSECONDS, without ever building
// the key schedule, while a real digest costs ~214 ms (00018's reference run; the
// figure is load-dependent and spans 210-312 ms across machines). A value of the first
// kind therefore answers "is this address a panel administrator?" about 1.9 million
// times faster than an unregistered address -- a one-request enumeration oracle,
// pointing the opposite way from the one PHASE B OBLIGATION 2 closes.
//
// So the property is not a spelling. It is:
//
//	EVERY VALUE admin_users.password_hash CAN ACCEPT FROM NOW ON MAKES bcrypt PAY
//	THE KEY SCHEDULE.
//
// ⚠️ "CAN ACCEPT FROM NOW ON" AND "CAN HOLD" ARE NOT THE SAME CLAIM, and an earlier
// version of this header made the stronger one. Migration 00018 is NOT VALID, so the
// 20 232 fast-answering rows already in the development database are untouched: they
// still exist and still answer in nanoseconds. What is closed is the WRITE path --
// no role, tappa_app or owner, can add another. The product data is separately clean
// (both seeded rows are $2a$12$). 00018 states this at length; the two documents must
// not disagree about it, which is why this paragraph exists.
//
// TestPasswordHashCheck_EverythingItAdmitsIsProcessableByBcrypt below states the
// accept-side property -- its NAME is the precise claim -- and states it by ASKING
// BOTH SIDES: Postgres whether the constraint admits a value, x/crypto whether it
// would fast-path on it, rather than by re-writing the regex in Go and comparing it
// with itself.

// ⚠️ WHAT THIS FILE COMMITS. probeAdminDigest below never commits -- its INSERT is
// always unwound -- but the file as a whole is NOT read-only, and an earlier version
// of this header wrongly implied it was. probeTenant commits one tenant row per test,
// and TestPasswordHashCheck_BindsUpdateNotJustInsert commits one admin_users row,
// because a constraint that fires on UPDATE needs a row that outlives its INSERT.
// Both are throwaway fixtures with generated ids, in the same style as every other DB
// test here; they are named so the residue this suite leaves is accounted for rather
// than surprising (00018 counts that residue).

// probeAdminDigest reports whether the database ACCEPTS digest in
// admin_users.password_hash. The INSERT is always rolled back, so this particular
// probe leaves nothing behind whichever way it answers.
//
// It returns the SQLSTATE alongside, because "rejected" is not enough: this file's
// claims are about the CHECK constraint specifically, and a NOT NULL error or a
// permission error would otherwise read as success.
func probeAdminDigest(t *testing.T, d *DB, tenantID uuid.UUID, digest string) (accepted bool, sqlstate string) {
	t.Helper()
	// errRollback unwinds a transaction that has done what we wanted. It is a
	// sentinel, never a failure.
	errRollback := fmt.Errorf("probe: deliberate rollback")

	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
			 VALUES ($1, $2, 'Digest Probe', $3, $4, 'manager')`,
			uuid.New(), tenantID, "probe-"+uuid.NewString()+"@digest.example", digest)
		if e != nil {
			return e
		}
		return errRollback
	})
	switch {
	case err == nil:
		t.Fatal("probe: the transaction committed; it must always roll back")
		return false, ""
	case strings.Contains(err.Error(), "deliberate rollback"):
		return true, ""
	}
	if pg := asPgErr(err); pg != nil {
		return false, pg.Code
	}
	t.Fatalf("probe: error is not a Postgres error: %v", err)
	return false, ""
}

// probeTenant creates a committed tenant to hang probe rows on.
func probeTenant(t *testing.T, d *DB) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := d.WithTenant(context.Background(), id, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Digest Probe Tenant', $2, 'bar', 'single')`,
			id, "VAT-"+id.String())
		return e
	}); err != nil {
		t.Fatalf("probeTenant: %v", err)
	}
	return id
}

// validSalt is the 53-character salt+digest tail of a REAL bcrypt hash, reused so
// the cases below vary ONE thing at a time (the cost, or the prefix) instead of
// varying the body too.
func validSalt(t *testing.T) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte("probe"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	return string(h)[7:]
}

// TestPasswordHashCheck_RefusesUnprocessableDigests is migration 00018's headline:
// the four arms password.go measured, plus the arm a NAIVE format check would have
// let straight back in.
func TestPasswordHashCheck_RefusesUnprocessableDigests(t *testing.T) {
	d := testDB(t)
	tenantID := probeTenant(t, d)
	tail := validSalt(t)

	tests := []struct {
		name   string
		digest string
		want   bool // want accepted
	}{
		// --- the arms password.go measured, in its own words -------------------
		{"empty string -- the 1 504 663x arm", "", false},
		{"malformed -- the 1 934 567x arm", "not-a-real-hash", false},
		{"the 'x' seven fixtures used to write", "x", false},

		// 🔴 THE CASE THAT DECIDED THE CONSTRAINT'S SHAPE. 60 characters, correct
		// prefix, correct alphabet, two digits of cost -- it passes every "format"
		// check one would write by eye, and x/crypto rejects it in 2 us ("cost 99 is
		// outside allowed range (4,31)"). A CHECK spelled [0-9]{2} would admit 72 of
		// the 100 two-digit costs that take the fast path.
		{"cost 99 -- shape-valid, still a 2 us fast path", "$2a$99$" + tail, false},
		{"cost 00 -- the same hole at the bottom", "$2a$00$" + tail, false},
		{"cost 03 -- one below bcrypt's floor", "$2a$03$" + tail, false},

		// --- shape ------------------------------------------------------------
		{"truncated -- the 29-char plaque fixture", "$2a$12$" + strings.Repeat("a", 22), false},
		{"61 characters", "$2a$04$" + tail + "x", false},
		{"59 characters", "$2a$04$" + tail[:52], false},
		{"'!' is outside bcrypt's base64 alphabet", "$2a$04$" + tail[:52] + "!", false},
		{"major version 3", "$3a$04$" + tail, false},
		{"no leading $", "2a$04$" + tail + "x", false},

		// --- POSITIVE CONTROLS. Without these the test passes when the constraint
		// --- rejects EVERYTHING, which is the classic vacuous form.
		{"a real cost-4 digest", "$2a$04$" + tail, true},
		{"the shared fixture placeholder", fixtures.UnusablePasswordHash, true},
		// --- the DoS ceiling (00018). bcrypt accepts up to 31; this column stops at
		// --- 14, because padding makes one slow row everyone-with-that-email's
		// --- problem (manager.go:475) and cost 31 is ~31 HOURS per comparison.
		{"cost 15 -- one above the ceiling", "$2a$15$" + tail, false},
		{"cost 31 -- bcrypt's max, ~31 h per compare", "$2a$31$" + tail, false},

		{"the $2b$ spelling", "$2b$04$" + tail, true},
		{"the $2y$ spelling", "$2y$04$" + tail, true},
		{"cost 14 -- the ceiling itself", "$2a$14$" + tail, true},
		{"cost 12 -- what Hash actually produces", "$2a$12$" + tail, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			accepted, sqlstate := probeAdminDigest(t, d, tenantID, tc.digest)
			if accepted != tc.want {
				t.Fatalf("digest %q: accepted=%v want=%v (sqlstate %q)",
					shape(tc.digest), accepted, tc.want, sqlstate)
			}
			// Rejection must come from THIS constraint. 23514 is check_violation; a
			// 23502 (not null) or 42501 (privilege) would mean the case proved
			// something else and the test would be reading a coincidence.
			if !accepted && sqlstate != "23514" {
				t.Fatalf("digest %q was rejected with sqlstate %q, want 23514 (check constraint); "+
					"the rejection did not come from admin_users_password_hash_is_bcrypt",
					shape(tc.digest), sqlstate)
			}
		})
	}
}

// paysKeySchedule reports whether x/crypto actually runs blowfish's key schedule
// for a digest of this SHAPE, and it is the oracle the invariant test rests on.
//
// 🔴 IT IS NOT bcrypt.Cost, AND THE FIRST VERSION OF THIS FILE GOT THAT WRONG. The
// claim was that Cost and CompareHashAndPassword share newFromHash, "where every
// early error lives ... including salt decoding". SALT DECODING IS NOT THERE
// (x/crypto@v0.54.0/bcrypt/bcrypt.go): newFromHash COPIES the 22 salt bytes at
// lines 186-191 without looking at them, and base64Decode runs at line 218 inside
// expensiveBlowfishSetup. Measured:
//
//	"$2a$04$" + 22x'!' + 31x'c'   (60 chars)
//	  bcrypt.Cost -> cost=4, err=<nil>                     <- happy
//	  Compare     -> 21 us, "illegal base64 data ..."      <- DOES NOT pay
//
// So a Cost-based oracle is blind to an entire class of fast-path digest, which is
// exactly the class a future widening of the constraint's body character class would
// let in. The oracle therefore runs the REAL comparison and classifies the ERROR
// TYPE rather than the elapsed time:
//
//	nil                            -> paid (and matched)
//	ErrMismatchedHashAndPassword   -> paid (it did the work, then disagreed)
//	anything else                  -> FAST PATH, i.e. the oracle we must not admit
//
// Timing is deliberately NOT the discriminator: a threshold in wall-clock would be a
// flaky test on a loaded machine, while the error type is exact.
//
// WHY THE COST IS NORMALISED TO MinCost. A cost-31 comparison is ~2^19 times a
// cost-12 one -- days, not seconds -- so comparing candidates at their own cost is
// not runnable. The cost field's VALIDITY is checked separately below with
// bcrypt.Cost, which is what that function is genuinely sound for (decodeVersion +
// decodeCost are both inside newFromHash). What is left for this function is the
// SHAPE: prefix, version and salt bytes, none of which depend on the cost value. The
// two checks together cover every early return on both sides of the split.
func paysKeySchedule(digest string) (bool, error) {
	probe := digest
	if len(digest) >= 7 {
		probe = digest[:4] + fmt.Sprintf("%02d", bcrypt.MinCost) + digest[6:]
	}
	err := bcrypt.CompareHashAndPassword([]byte(probe), []byte("a password that will not match"))
	if err == nil || errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return true, nil
	}
	return false, err
}

// TestPasswordHashOracle_ClassifiesKnownArmsCorrectly guards the guard.
//
// paysKeySchedule depends on x/crypto INTERNALS staying where they are, and the
// invariant test below is only as good as this classifier. WHICH LIBRARY CHANGES THIS
// TEST ACTUALLY CATCHES -- measured by mutating each case rather than asserted, because
// the first draft of this comment claimed all three and one of them is silent:
//
//   - LOUD. If the mismatch error is renamed or starts arriving wrapped so that
//     errors.Is stops matching, paysKeySchedule reports "does not pay" for a genuine
//     digest and the first two cases below fail.
//   - LOUD. If the classifier is ever weakened to return true unconditionally, the
//     four negative cases fail.
//   - SILENT, AND HARMLESS -- THIS IS THE ONE THAT WAS OVERCLAIMED. If a future
//     x/crypto moved base64Decode INTO newFromHash, every case here would still pass
//     and the closing bcrypt.Cost assertion would simply not run (it is guarded by
//     `if err == nil`, and in that world Cost would correctly report an error). Nothing
//     reddens. That is safe rather than lucky: in that world bcrypt.Cost would once
//     again be a sound oracle, so the invariant test's guarantee is unchanged and this
//     file is merely carrying a redundant classifier. The failure mode this test exists
//     to prevent -- the invariant silently weakening -- does not occur there.
func TestPasswordHashOracle_ClassifiesKnownArmsCorrectly(t *testing.T) {
	real, err := bcrypt.GenerateFromPassword([]byte("probe"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword: %v", err)
	}
	body := string(real)[7:]

	tests := []struct {
		name   string
		digest string
		want   bool
	}{
		{"a genuine digest pays", string(real), true},
		{"a genuine body at another cost pays", "$2a$12$" + body, true},
		{"empty does not", "", false},
		{"malformed does not", "not-a-real-hash", false},
		{"a short body does not", "$2a$04$" + body[:40], false},
		{"major version 3 does not", "$3a$04$" + body, false},
		// 🔴 THE CASE THE OLD ORACLE MISSED. 60 characters, in-range cost, and
		// bcrypt.Cost reports no error -- but the salt is not base64.
		{"non-base64 salt does not", "$2a$04$" + strings.Repeat("!", 22) + strings.Repeat("c", 31), false},

		// ⚠️ THE COST FIELD IS DELIBERATELY NOT THIS FUNCTION'S JOB, and this case
		// pins that split rather than leaving it to the doc comment. paysKeySchedule
		// REWRITES the cost to MinCost before comparing (a cost-31 comparison takes
		// days), so an out-of-range cost with a good body reports PAID here. Its
		// validity is checked by bcrypt.Cost on the ORIGINAL digest, at the one call
		// site, and the invariant test does both in that order.
		{"an out-of-range cost is not judged here", "$2a$99$" + body, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, why := paysKeySchedule(tc.digest)
			if got != tc.want {
				t.Fatalf("paysKeySchedule(%s) = %v, want %v (err=%v)", shape(tc.digest), got, tc.want, why)
			}
		})
	}

	// The specific regression, stated as its own assertion so the failure names it.
	badSalt := "$2a$04$" + strings.Repeat("!", 22) + strings.Repeat("c", 31)
	if _, err := bcrypt.Cost([]byte(badSalt)); err == nil {
		if paid, _ := paysKeySchedule(badSalt); paid {
			t.Fatal("bcrypt.Cost accepts the non-base64 salt AND paysKeySchedule says it pays; " +
				"the two disagree with the measurement and the oracle is no longer sound")
		}
		t.Log("confirmed: bcrypt.Cost accepts a digest that does NOT pay the key schedule, " +
			"which is why this file does not use Cost as its oracle")
	}
}

// TestPasswordHashCheck_EverythingItAdmitsIsProcessableByBcrypt is THE invariant.
//
// It asks the two authorities independently -- Postgres "do you accept this value?"
// and x/crypto "do you actually run the key schedule for it?" -- and fails if the
// database ever admits something that would take a fast path. The direction matters:
// admitting a value bcrypt short-circuits REOPENS the oracle, while rejecting one it
// could process is merely conservative, so only the first direction is an error.
func TestPasswordHashCheck_EverythingItAdmitsIsProcessableByBcrypt(t *testing.T) {
	d := testDB(t)
	tenantID := probeTenant(t, d)
	tail := validSalt(t)

	var candidates []string
	// Every two-digit cost, valid prefix and body: this sweep is what caught the
	// [0-9]{2} draft.
	for c := 0; c <= 99; c++ {
		for _, minor := range []string{"a", "b", "y", "x"} {
			candidates = append(candidates, fmt.Sprintf("$2%s$%02d$%s", minor, c, tail))
		}
	}
	// Bodies that are wrong in ways a cost sweep cannot reach. The '!' and mixed
	// runs are the F1 class: 60 characters, in-range cost, non-base64 salt.
	for _, body := range []string{
		strings.Repeat("!", 22) + strings.Repeat("c", 31),
		strings.Repeat("!", 53),
		"!" + tail[1:],
		tail[:21] + "!" + tail[22:],
		strings.Repeat(" ", 53),
		strings.Repeat("=", 53),
		strings.Repeat("$", 53),
	} {
		candidates = append(candidates, "$2a$04$"+body)
	}
	// Shapes that are wrong in ways neither sweep reaches.
	candidates = append(candidates,
		"", "x", "not-a-real-hash",
		"$2a$12$"+strings.Repeat("a", 22),
		"$2a$04$"+tail+"x",
		"$2a$04$"+tail[:52],
		"$2a$04$"+tail[:52]+"!",
		"$2a$4$"+tail,
		"$3a$04$"+tail,
		"$2a$04"+tail,
		strings.Repeat("$", 60),
	)

	admitted := 0
	for _, digest := range candidates {
		accepted, _ := probeAdminDigest(t, d, tenantID, digest)
		if !accepted {
			continue
		}
		admitted++

		// (1) the cost field must be one bcrypt accepts -- newFromHash's own check.
		if _, err := bcrypt.Cost([]byte(digest)); err != nil {
			t.Errorf("🔴 THE CONSTRAINT ADMITS A DIGEST WHOSE COST bcrypt REJECTS: %s -> %v",
				shape(digest), err)
			continue
		}
		// (2) and the SHAPE must actually reach the key schedule.
		if paid, why := paysKeySchedule(digest); !paid {
			t.Errorf("🔴 THE CONSTRAINT ADMITS A VALUE THAT TAKES bcrypt'S FAST PATH: %s -> %v\n"+
				"Such a row is compared in ~150 ns while a real one costs ~214 ms, so it answers "+
				"\"is this address a panel administrator?\" about 1.9 million times faster than an "+
				"unregistered address. This is the oracle migration 00018 exists to close.",
				shape(digest), why)
		}
	}

	// POSITIVE CONTROL. If the constraint rejected every candidate, the loop above
	// would check nothing and pass in silence -- the vacuous shape this repo has
	// already been bitten by three times.
	if admitted == 0 {
		t.Fatal("the constraint admitted NOTHING, so the invariant was never exercised; " +
			"this test would pass against a column that rejects all writes")
	}
	t.Logf("%d of %d candidate digests admitted by the constraint", admitted, len(candidates))
}

// TestPasswordHashCheck_BindsUpdateNotJustInsert covers the arm that was the loudest
// in the pre-migration measurement.
//
// As tappa_app, before 00018, `UPDATE admin_users SET password_hash = ”` returned
// UPDATE 158 -- one statement turning every administrator of a tenant into a
// nanosecond oracle. 00017 grants UPDATE(password_hash) to tappa_app and this
// migration does not touch that grant, so the UPDATE path is closed by the CHECK or
// it is not closed at all.
func TestPasswordHashCheck_BindsUpdateNotJustInsert(t *testing.T) {
	d := testDB(t)
	tenantID := probeTenant(t, d)

	adminID := uuid.New()
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role)
			 VALUES ($1, $2, 'Update Probe', $3, $4, 'owner')`,
			adminID, tenantID, "upd-"+uuid.NewString()+"@digest.example", fixtures.UnusablePasswordHash)
		return e
	}); err != nil {
		t.Fatalf("seed the probe admin: %v", err)
	}

	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`UPDATE admin_users SET password_hash = '' WHERE tenant_id = $1`, tenantID)
		return e
	})
	if err == nil {
		t.Fatal("UPDATE ... SET password_hash = '' succeeded; the CHECK binds INSERT only, " +
			"and the measured UPDATE 158 arm is still open")
	}
	pg := asPgErr(err)
	if pg == nil || pg.Code != "23514" {
		t.Fatalf("want 23514 check_violation on the UPDATE, got %v", err)
	}

	// POSITIVE CONTROL: a legal digest must still be writable, or the test above
	// would also pass against a column nothing can update.
	//
	// ⚠️ IT WRITES A DIFFERENT VALUE THAN THE ROW ALREADY HOLDS, deliberately. The
	// first version re-wrote fixtures.UnusablePasswordHash over itself, which Postgres
	// accepts without the new value ever differing from the old -- a control that
	// cannot distinguish "legal digests are writable" from "this exact byte string is".
	// A cost-12 digest exercises a different cost field and a different body.
	replacement := "$2a$12$" + validSalt(t)
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`UPDATE admin_users SET password_hash = $1 WHERE id = $2 AND tenant_id = $3`,
			replacement, adminID, tenantID)
		return e
	}); err != nil {
		t.Fatalf("positive control: a legal digest must remain writable: %v", err)
	}

	// And the change must have LANDED -- an UPDATE that matched zero rows would also
	// return no error.
	var stored string
	if err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT password_hash FROM admin_users WHERE id = $1 AND tenant_id = $2`,
			adminID, tenantID).Scan(&stored)
	}); err != nil {
		t.Fatalf("read the probe admin back: %v", err)
	}
	if stored != replacement {
		t.Fatalf("the positive control's UPDATE did not land: stored digest is %s, wrote %s",
			shape(stored), shape(replacement))
	}
}

// shape reports a digest's PREFIX and LENGTH for a failure message, never the whole
// value. Section 4.7 forbids a digest reaching a log line, and a test failure message
// is a log line. Seven characters of a bcrypt string are the version and the cost --
// which is the part these tests are about -- and carry nothing derived from a
// password.
func shape(digest string) string {
	if len(digest) > 7 {
		return digest[:7] + "..." + fmt.Sprint(len(digest))
	}
	return fmt.Sprintf("%q(len %d)", digest, len(digest))
}
