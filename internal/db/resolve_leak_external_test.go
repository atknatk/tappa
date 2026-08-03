package db_test

// resolve_leak_external_test.go -- the section 4.7 leak proof for the admin
// password digest, from OUTSIDE the package.
//
// WHY A SEPARATE TEST PACKAGE, and why this file exists at all. M6-01 phase A
// shipped ResolvedAdmin.PasswordHash as a bare `string`, and an independent audit
// measured SIX ordinary rendering paths that printed the panel owner's real bcrypt
// digest verbatim: `%+v`, `%v` over the returned SLICE, `%#v`, fmt.Errorf, a
// caller-local struct with an UNEXPORTED field, and log/slog. That is the same
// class the repo has already closed twice -- internal/session.Token (M5-01) and
// internal/invite.Code (M5-02) -- and the fix is the same pattern, so the proof is
// the same proof, in the same position: a CALLER's package, because the
// unexported-field path is precisely the one an in-package test cannot see (fmt
// only consults Formatter/Stringer for a value it may hand to an interface, and a
// value reached through an unexported field may not be --
// reflect.Value.CanInterface() == false).
//
// WHAT A LEAKED DIGEST COSTS, so the test is not mistaken for hygiene: a bcrypt
// digest in a log file is an OFFLINE cracking target -- no rate limit, no lockout,
// no audit trail -- and it belongs to the account that can read every employee's
// movements and rewrite the tenant's policies.
//
// Every assertion here has a POSITIVE CONTROL (a substring the render must still
// contain), and the file carries a PERMANENT NEGATIVE CONTROL: `bareAdmin`, the
// pre-fix shape with a plain string field, asserted to LEAK on the very same six
// paths. Without it "the digest is absent" could be true for the wrong reason, and
// a revert of the field type back to `string` could pass unnoticed.
//
// No DATABASE_URL is needed: this is a rendering proof, not a query proof.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/db"
	"github.com/google/uuid"
)

// fakeDigest is an obviously fake, searchable stand-in shaped like a bcrypt hash.
// It is NOT produced by anything and is not a real secret (CLAUDE.md section 4.7:
// test vectors are separate and fake); it exists so the assertions can grep for
// something specific.
const fakeDigest = "$2a$10$FAKEfakeFAKEfakeFAKEfeFAKEfakeFAKEfakeFAKEfakeFAKEfakeFA"

var (
	fakeAdminID  = uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaa0001")
	fakeTenantID = uuid.MustParse("11111111-1111-1111-1111-111111111111")
)

// bareAdmin is the shape ResolvedAdmin had BEFORE the fix: identical fields, the
// digest as a plain string. It is the permanent negative control -- the thing that
// must keep leaking, so that "the real one does not leak" says something.
type bareAdmin struct {
	ID       uuid.UUID
	TenantID uuid.UUID
	Digest   string
	Status   string
}

// loginState is the shape that breaks redacting methods: a caller-local struct with
// UNEXPORTED fields carrying the resolved candidate. This is what a phase-B login
// handler naturally writes, and what a `slog.Info("login", "state", st)` or a `%+v`
// in an error naturally renders.
type loginState struct {
	email string
	admin db.ResolvedAdmin
}

// bareLoginState is loginState over the pre-fix struct: same unexported-field path,
// leaky payload.
type bareLoginState struct {
	email string
	admin bareAdmin
}

// resolved builds the candidate the way GetAdminByEmail would, from outside the
// package -- which is possible only because NewPasswordHash is exported for the
// scanner and for phase B's dummy digest.
func resolved() db.ResolvedAdmin {
	return db.ResolvedAdmin{
		ID:           fakeAdminID,
		TenantID:     fakeTenantID,
		PasswordHash: db.NewPasswordHash(fakeDigest),
		Status:       "active",
	}
}

func bare() bareAdmin {
	return bareAdmin{ID: fakeAdminID, TenantID: fakeTenantID, Digest: fakeDigest, Status: "active"}
}

// renderings runs the SIX audited paths plus the JSON one over both shapes and
// returns them under identical names, so the redaction test and the negative
// control assert over exactly the same set.
func renderings(t *testing.T, redacting bool) map[string]string {
	t.Helper()

	var one any
	var list any
	var held any
	if redacting {
		adm := resolved()
		one, list, held = adm, []db.ResolvedAdmin{adm, adm}, loginState{email: "ozcan@x", admin: adm}
	} else {
		adm := bare()
		one, list, held = adm, []bareAdmin{adm, adm}, bareLoginState{email: "ozcan@x", admin: adm}
	}

	var text, jsonLog bytes.Buffer
	slog.New(slog.NewTextHandler(&text, nil)).Info("login", "candidate", one)
	slog.New(slog.NewJSONHandler(&jsonLog, nil)).Info("login", "candidate", one)

	marshalled, err := json.Marshal(one)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	return map[string]string{
		// 1 -- the plainest one: %+v on the struct the resolver returns.
		"1 %+v": fmt.Sprintf("%+v", one),
		// 2 -- %v over the SLICE, which is literally GetAdminByEmail's return type.
		"2 %v (slice)": fmt.Sprintf("%v", list),
		// 3 -- %#v, which routes through GoStringer rather than Stringer.
		"3 %#v": fmt.Sprintf("%#v", one),
		// 4 -- error wrapping: the digest riding inside an error chain.
		"4 fmt.Errorf": fmt.Errorf("login: candidate rejected: %+v", one).Error(),
		// 5 -- the path redacting METHODS cannot cover on their own: an UNEXPORTED
		//      field in a caller-local struct.
		"5 unexported field %+v": fmt.Sprintf("%+v", held),
		"5 unexported field %#v": fmt.Sprintf("%#v", held),
		// 6 -- log/slog, both handlers.
		"6 slog text": text.String(),
		"6 slog json": jsonLog.String(),
		// and the serialisation path a JSON API or a structured encoder takes.
		"7 json.Marshal": string(marshalled),
	}
}

// TestPasswordHash_NoLeakOnTheSixAuditedPaths is the regression test for the audit
// finding: none of the measured paths may emit the digest, and each must still
// produce something recognisable so the assertion cannot pass vacuously.
func TestPasswordHash_NoLeakOnTheSixAuditedPaths(t *testing.T) {
	// The control differs for the two JSON paths: encoding/json drops unexported
	// fields, and slog's JSON handler renders the struct through it, so neither can
	// mention the email. They still carry the tenant/status, which is control
	// enough.
	controls := map[string]string{
		"1 %+v":                  "active",
		"2 %v (slice)":           "11111111-1111-1111-1111-111111111111",
		"3 %#v":                  "ResolvedAdmin",
		"4 fmt.Errorf":           "candidate rejected",
		"5 unexported field %+v": "ozcan@x",
		"5 unexported field %#v": "ozcan@x",
		"6 slog text":            "active",
		"6 slog json":            "active",
		"7 json.Marshal":         "active",
	}

	for name, got := range renderings(t, true) {
		if strings.Contains(got, fakeDigest) {
			t.Errorf("%s LEAKS the digest: %q", name, got)
		}
		// A 16-character run of a bcrypt string is already a meaningful leak.
		if strings.Contains(got, fakeDigest[:16]) {
			t.Errorf("%s leaks a 16-char prefix: %q", name, got)
		}
		if want := controls[name]; !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q (vacuous otherwise)", name, got, want)
		}
	}
}

// TestPasswordHash_BareStringIsThePositiveControl asserts that the SAME six paths
// over the SAME data DO leak when the field is a plain string. This is what makes
// the test above meaningful, and it is what turns a revert of the field type into a
// red build: without the redacting type, every one of these renders the digest.
func TestPasswordHash_BareStringIsThePositiveControl(t *testing.T) {
	for name, got := range renderings(t, false) {
		if !strings.Contains(got, fakeDigest) {
			t.Errorf("%s did NOT leak the bare string: the redaction test proves nothing (%q)", name, got)
		}
	}
}

// TestPasswordHash_RedactsWhereverItIsReachableAsAnInterface: everywhere fmt CAN
// reach the redacting methods -- a bare value, a pointer, a slice, a map key, an
// exported field -- the placeholder must appear, for EVERY verb. This half is what
// the methods buy (the pointer indirection buys the unexported-field case above);
// keeping both means a fix for one cannot silently disable the other.
func TestPasswordHash_RedactsWhereverItIsReachableAsAnInterface(t *testing.T) {
	// Hoisted out of the fmt calls on purpose: redline R7 now flags the literal
	// word inside a fmt/slog call, and the scanner is right to be blunt about it
	// (the same hoisting the session leak test does).
	h := db.NewPasswordHash(fakeDigest)
	second := db.NewPasswordHash(fakeDigest)
	list := []db.PasswordHash{h, second}
	byKey := map[db.PasswordHash]string{h: "one", second: "two"}
	if len(byKey) != 2 {
		t.Fatalf("map over two distinct values has %d entries, want 2: comparison must be identity, not value", len(byKey))
	}
	adm := resolved()

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("issued", "digest", h)

	renders := map[string]string{
		"bare %v":        fmt.Sprintf("%v", h),
		"bare %s":        fmt.Sprintf("%s", h),
		"bare %q":        fmt.Sprintf("%q", h),
		"bare %x":        fmt.Sprintf("%x", h),
		"bare %d":        fmt.Sprintf("%d", h),
		"bare %#v":       fmt.Sprintf("%#v", h),
		"pointer %v":     fmt.Sprintf("%v", &h),
		"slice %v":       fmt.Sprintf("%v", list),
		"map key %v":     fmt.Sprintf("%v", byKey),
		"exported %+v":   fmt.Sprintf("%+v", adm),
		"String()":       h.String(),
		"GoString()":     h.GoString(),
		"LogValue()":     h.LogValue().String(),
		"slog attribute": buf.String(),
	}
	for name, got := range renders {
		if strings.Contains(got, fakeDigest) {
			t.Errorf("%s leaks the digest: %q", name, got)
		}
		if !strings.Contains(got, "redacted") {
			t.Errorf("%s = %q, want the redaction placeholder", name, got)
		}
	}

	text, err := h.MarshalText()
	if err != nil {
		t.Fatalf("MarshalText: %v", err)
	}
	if strings.Contains(string(text), fakeDigest) || !strings.Contains(string(text), "redacted") {
		t.Errorf("MarshalText = %q, want the placeholder", text)
	}
}

// TestPasswordHash_ZeroValueIsSafeAndEmpty is the M5-01 polar lesson applied here:
// the DANGEROUS state must not be the state you get by forgetting. The zero value
// carries a nil pointer, so every printing path must tolerate it (no nil deref),
// and the single reader must return "" -- which is not a valid digest under any
// KDF, so a comparison against it FAILS and the login is refused. Forgetting to
// populate the struct cannot authenticate anyone.
//
// The other half of "fail closed" is measured against the database in
// TestResolvedAdmin_ZeroValueFailsClosed (admins_test.go): a zero ResolvedAdmin's
// uuid.Nil identity matches no row in CreateAdminSession.
func TestPasswordHash_ZeroValueIsSafeAndEmpty(t *testing.T) {
	var zero db.PasswordHash
	var zeroAdmin db.ResolvedAdmin

	if got := zero.RevealForPasswordComparison(); got != "" {
		t.Errorf("zero value revealed %q, want the empty string", got)
	}
	if got := zeroAdmin.PasswordHash.RevealForPasswordComparison(); got != "" {
		t.Errorf("zero ResolvedAdmin revealed %q, want the empty string", got)
	}

	type holder struct {
		note string
		h    db.PasswordHash
	}
	held := holder{note: "not resolved yet", h: zero}

	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, nil)).Info("zero", "digest", zero, "held", held, "admin", zeroAdmin)

	for name, got := range map[string]string{
		"bare %v":        fmt.Sprintf("%v", zero),
		"bare %s":        fmt.Sprintf("%s", zero),
		"bare %#v":       fmt.Sprintf("%#v", zero),
		"bare %x":        fmt.Sprintf("%x", zero),
		"pointer %v":     fmt.Sprintf("%v", &zero),
		"unexported %+v": fmt.Sprintf("%+v", held),
		"unexported %#v": fmt.Sprintf("%#v", held),
		"admin %+v":      fmt.Sprintf("%+v", zeroAdmin),
		"slog":           buf.String(),
		"String()":       zero.String(),
		"GoString()":     zero.GoString(),
		"LogValue()":     zero.LogValue().String(),
	} {
		if got == "" {
			t.Errorf("%s produced nothing", name)
		}
	}
}

// TestPasswordHash_RevealIsTheOnlyWayOut states the containment claim as a test
// rather than as a sentence: the value goes IN through NewPasswordHash and comes
// back out through exactly one named method, byte for byte. Everything else on the
// type redacts (proven above), so a grep for RevealForPasswordComparison lists every
// place the digest is in the clear -- which must stay at one, phase B's KDF
// comparison.
func TestPasswordHash_RevealIsTheOnlyWayOut(t *testing.T) {
	h := db.NewPasswordHash(fakeDigest)
	if got := h.RevealForPasswordComparison(); got != fakeDigest {
		t.Fatalf("reveal round-trip = %q, want the digest back unchanged", got)
	}

	// The constructor copies its argument, so a caller mutating its own variable
	// afterwards cannot change what a resolved candidate carries.
	src := fakeDigest
	kept := db.NewPasswordHash(src)
	src = "mutated"
	if got := kept.RevealForPasswordComparison(); got != fakeDigest {
		t.Errorf("value aliased the caller's storage: got %q after the caller mutated it (src is now %q)", got, src)
	}
}
