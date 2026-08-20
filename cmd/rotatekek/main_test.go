package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Tests for the KEK rotation tool (M8-02 phase F).
//
// THE SHAPE OF THIS FILE FOLLOWS THE SHAPE OF THE RISK. A rotation tool is a
// VERIFICATION TOOL, and this project's own ledger says a verification tool is a
// code path with degenerate inputs of its own — the ones nobody pictures, because
// "a healthy system is EMPTY and a broken one is MISSING". So the table below is
// mostly not the happy path: it is empty input, one field short, a duplicate uid,
// a stale acknowledgement, two identical keys. The happy path gets one case and
// an end-to-end round trip.
//
// Every key here is FAKE and generated in-process (§4.7).

// testKEK returns a deterministic FAKE 32-byte KEK, base64 as the environment
// carries it. The seed byte makes each one distinct and traceable in a failure.
func testKEK(seed byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{seed}, 32))
}

// envOf turns a map into the getenv function run takes, so a test states its
// whole environment in one literal and nothing leaks between cases.
func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// seal wraps a fake per-tag key under a base64 KEK, INDEPENDENTLY of
// internal/sun: this file re-implements the ADR 0003 envelope so that a green
// test proves the tool agrees with the FORMAT and not merely with itself. A bug
// introduced into sun.Wrap and sun.Unwrap together would still be caught here.
func seal(t *testing.T, kekB64, uid string, key []byte) string {
	t.Helper()
	kek, err := base64.StdEncoding.DecodeString(kekB64)
	if err != nil {
		t.Fatalf("decoding the fixture KEK: %v", err)
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		t.Fatalf("aes: %v", err)
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	uidBytes, err := hex.DecodeString(uid)
	if err != nil {
		t.Fatalf("uid: %v", err)
	}
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("nonce: %v", err)
	}
	return hex.EncodeToString(g.Seal(nonce, nonce, key, uidBytes))
}

// open is seal's inverse, used by the round-trip test to prove the tool's output
// really does re-seal the SAME per-tag key.
func open(t *testing.T, kekB64, uid, refHex string) ([]byte, error) {
	t.Helper()
	kek, _ := base64.StdEncoding.DecodeString(kekB64)
	block, _ := aes.NewCipher(kek)
	g, _ := cipher.NewGCM(block)
	uidBytes, _ := hex.DecodeString(uid)
	ref, err := hex.DecodeString(refHex)
	if err != nil {
		return nil, err
	}
	if len(ref) != 44 {
		return nil, fmt.Errorf("not a 44-byte envelope: %d", len(ref))
	}
	return g.Open(nil, ref[:12], ref[12:], uidBytes)
}

const (
	uidA    = "04AC7E55000601"
	uidB    = "04AC7E55000602"
	tenantA = "10000000-0000-4000-8000-000000000001"
	tenantB = "20000000-0000-4000-8000-000000000002"
)

var fakeTagKey = bytes.Repeat([]byte{0xA5}, 16)

// line builds one COPY row exactly as psql emits it.
func line(uid, tenant, refHex string) string { return uid + "\t" + tenant + "\t" + refHex }

// TestRun_DegenerateInputs is the table the ledger asks for: every way the
// environment or the input can be wrong, and the exit code each produces.
//
// Exit codes are asserted rather than message text wherever possible, because the
// exit code is what the runbook's pipeline reacts to and text is free to improve.
func TestRun_DegenerateInputs(t *testing.T) {
	kekNew, kekOld := testKEK(1), testKEK(2)
	good := line(uidA, tenantA, seal(t, kekOld, uidA, fakeTagKey))

	tests := []struct {
		name     string
		env      map[string]string
		stdin    string
		wantExit int
		wantErr  string // substring of stderr; "" means do not check
	}{
		{
			name:     "the primary KEK is unset",
			env:      map[string]string{prevKEKEnv: kekOld},
			stdin:    good,
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  kekEnv + " is empty",
		},
		{
			name:     "the previous KEK is unset",
			env:      map[string]string{kekEnv: kekNew},
			stdin:    good,
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  prevKEKEnv + " is empty",
		},
		{
			name:     "the two KEKs are the same value",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekNew},
			stdin:    good,
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "SAME key",
		},
		{
			// The same key spelled with trailing whitespace must still be caught:
			// the comparison is on decoded bytes, not on the strings.
			name:     "the same KEK with stray whitespace is still the same KEK",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: "  " + kekNew + "\n"},
			stdin:    good,
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "SAME key",
		},
		{
			name:     "a 16-byte KEK (AES-128, silently weaker)",
			env:      map[string]string{kekEnv: base64.StdEncoding.EncodeToString(make([]byte, 16)), prevKEKEnv: kekOld},
			stdin:    good,
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "want 32 bytes, got 16",
		},
		{
			name:     "a 24-byte KEK",
			env:      map[string]string{kekEnv: base64.StdEncoding.EncodeToString(make([]byte, 24)), prevKEKEnv: kekOld},
			stdin:    good,
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "want 32 bytes, got 24",
		},
		{
			name:     "a 33-byte KEK",
			env:      map[string]string{kekEnv: base64.StdEncoding.EncodeToString(make([]byte, 33)), prevKEKEnv: kekOld},
			stdin:    good,
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "want 32 bytes, got 33",
		},
		{
			name:     "a KEK that is not base64",
			env:      map[string]string{kekEnv: "!!!not base64!!!", prevKEKEnv: kekOld},
			stdin:    good,
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "not valid base64",
		},
		{
			// THE FAIL-OPEN THIS TOOL EXISTS TO AVOID. Zero rows is what an empty
			// park looks like AND what tags looks like to a role RLS is filtering.
			// "0/0 rotated, done" would be a lie in the second case.
			name:     "zero rows: an empty park and an RLS-blinded read are indistinguishable",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    "",
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "ROW LEVEL SECURITY",
		},
		{
			name:     "a line with two fields",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    uidA + "\t" + tenantA,
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "want 3",
		},
		{
			name:     "a line with four fields",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    good + "\textra",
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "want 3",
		},
		{
			name:     "a uid of the wrong length",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    line("04AC7E5500", tenantA, "deadbeef"),
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "uid must be 14",
		},
		{
			name:     "a uid that is not hex",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    line("04AC7E5500060Z", tenantA, "deadbeef"),
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "uid is not hex",
		},
		{
			name:     "a tenant_id that is not a uuid",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    line(uidA, "not-a-uuid", "deadbeef"),
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "tenant_id must be",
		},
		{
			// Right length, wrong shape: the hyphens are the only structure a
			// 36-character check would otherwise miss.
			name:     "a tenant_id of the right length with the hyphens moved",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    line(uidA, "100000000-000-4000-8000-000000000001", "deadbeef"),
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "not a uuid",
		},
		{
			name:     "a wrapped ref that is not hex",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    line(uidA, tenantA, "zzzz"),
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "is not hex",
		},
		{
			name:     "a blank line inside the stream",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    good + "\n\n" + line(uidB, tenantB, seal(t, kekOld, uidB, fakeTagKey)),
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "is empty",
		},
		{
			// uid is the PRIMARY KEY, so the database cannot produce a duplicate:
			// a duplicate means the input was concatenated or replayed, and the
			// emitted statement joins on uid so it would be ambiguous.
			name:     "the same uid twice",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    good + "\n" + good,
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "already seen on line 1",
		},
		{
			// RED LINE 2: silently skipping these is exactly the failure mode.
			name:     "a row that opens under neither KEK, unacknowledged",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    line(uidA, tenantA, seal(t, testKEK(9), uidA, fakeTagKey)),
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "open under NEITHER KEK",
		},
		{
			name: "a stale acknowledgement (one too few)",
			env: map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld,
				ackEnv: "0"},
			stdin:    line(uidA, tenantA, seal(t, testKEK(9), uidA, fakeTagKey)),
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "currently 0",
		},
		{
			name: "an acknowledgement that is not a number",
			env: map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld,
				ackEnv: "lots"},
			stdin:    good,
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "non-negative whole number",
		},
		{
			name: "a negative acknowledgement",
			env: map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld,
				ackEnv: "-1"},
			stdin:    good,
			wantExit: 1, // LITERAL, not exitRefused — see TestExitCodes_AreTheNumbersTheRunbookPublishes
			wantErr:  "non-negative whole number",
		},
		{
			// The acknowledgement is EXACT, so the run proceeds — but the exit
			// code must NOT be success, because the park is not fully rotated and
			// the old KEK cannot be destroyed.
			name: "an exact acknowledgement proceeds with a DIFFERENT exit code",
			env: map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld,
				ackEnv: "1"},
			stdin:    line(uidA, tenantA, seal(t, testKEK(9), uidA, fakeTagKey)),
			wantExit: 3, // LITERAL — the runbook keys its stop rule on this number
			wantErr:  "CANNOT be destroyed yet",
		},
		{
			name:     "the ordinary case: one row moves from the old KEK to the new one",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    good,
			wantExit: 0,
		},
		{
			// Idempotence: a re-run after an interrupted rotation must recognise
			// finished rows instead of re-sealing them under a fresh nonce.
			name:     "a row already under the new KEK is recognised, not re-sealed",
			env:      map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld},
			stdin:    line(uidA, tenantA, seal(t, kekNew, uidA, fakeTagKey)),
			wantExit: 0,
			wantErr:  "already under the new KEK ....... 1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			got := run(strings.NewReader(tc.stdin), &out, &errOut, envOf(tc.env))
			if got != tc.wantExit {
				t.Errorf("exit = %d, want %d\nstderr: %s", got, tc.wantExit, errOut.String())
			}
			if tc.wantErr != "" && !strings.Contains(errOut.String(), tc.wantErr) {
				t.Errorf("stderr does not mention %q\ngot: %s", tc.wantErr, errOut.String())
			}
			// A REFUSAL MUST POISON THE STREAM. Emitting nothing would be
			// fail-closed only if the operator remembered `set -o pipefail`;
			// psql fed an empty file exits 0 and the refusal would read as a
			// successful rotation.
			if got == 1 {
				if !strings.Contains(out.String(), "RAISE EXCEPTION") {
					t.Error("a refusal must put an aborting statement on stdout, not nothing")
				}
				if strings.Contains(out.String(), "UPDATE tags") {
					t.Error("a refusal must not emit any rotation SQL")
				}
			}
		})
	}
}

// TestRun_RoundTripThroughTheEmittedSQL is the criterion the card actually asks
// for, at unit level: after the tool runs, the value it wrote opens under the NEW
// KEK and yields THE SAME per-tag key, and does NOT open under the OLD one.
//
// The wrapped values are parsed back out of the generated SQL rather than taken
// from an internal variable, so what is checked is what psql would receive.
func TestRun_RoundTripThroughTheEmittedSQL(t *testing.T) {
	kekNew, kekOld := testKEK(1), testKEK(2)
	keyA := bytes.Repeat([]byte{0xA5}, 16)
	keyB := bytes.Repeat([]byte{0x5A}, 16)

	stdin := strings.Join([]string{
		line(uidA, tenantA, seal(t, kekOld, uidA, keyA)),
		line(uidB, tenantB, seal(t, kekOld, uidB, keyB)),
	}, "\n")

	var out, errOut bytes.Buffer
	if code := run(strings.NewReader(stdin), &out, &errOut, envOf(map[string]string{
		kekEnv: kekNew, prevKEKEnv: kekOld,
	})); code != 0 {
		t.Fatalf("exit = %d, want 0\nstderr: %s", code, errOut.String())
	}

	refs := parseNewRefs(t, out.String())
	if len(refs) != 2 {
		t.Fatalf("the SQL carries %d re-sealed rows, want 2", len(refs))
	}
	for uid, want := range map[string][]byte{uidA: keyA, uidB: keyB} {
		newRef, ok := refs[uid]
		if !ok {
			t.Fatalf("%s is missing from the emitted SQL", uid)
		}
		got, err := open(t, kekNew, uid, newRef)
		if err != nil {
			t.Fatalf("%s does not open under the NEW KEK: %v", uid, err)
		}
		if !bytes.Equal(got, want) {
			// Lengths, never bytes (§4.7).
			t.Fatalf("%s re-sealed a DIFFERENT key (%d bytes recovered)", uid, len(got))
		}
		if _, err := open(t, kekOld, uid, newRef); err == nil {
			t.Fatalf("%s still opens under the OLD KEK; rotating away from a leaked key would be pointless", uid)
		}
	}
}

// TestWriteSQL_CarriesBothPostConditions.
//
// The two guards are the whole reason the emitted SQL is more than a list of
// UPDATEs, and neither is visible from the tool's own exit code — they run in the
// database, in the writing session. If a refactor drops one, the tool still exits
// 0 and the operator still sees a clean report, so nothing else would notice.
func TestWriteSQL_CarriesBothPostConditions(t *testing.T) {
	kekNew, kekOld := testKEK(1), testKEK(2)
	var out, errOut bytes.Buffer
	stdin := strings.Join([]string{
		line(uidA, tenantA, seal(t, kekOld, uidA, fakeTagKey)),
		line(uidB, tenantB, seal(t, kekOld, uidB, fakeTagKey)),
	}, "\n")
	if code := run(strings.NewReader(stdin), &out, &errOut, envOf(map[string]string{
		kekEnv: kekNew, prevKEKEnv: kekOld,
	})); code != 0 {
		t.Fatalf("exit = %d: %s", code, errOut.String())
	}
	sql := out.String()

	for _, want := range []string{
		"BEGIN;",
		"COMMIT;",
		// A mechanical ceiling on how long a live tap can queue behind the
		// rotation's row locks. Without it the wait is a property of machine load:
		// measured between 0.76 s and 13.19 s across rounds, with no bound at any
		// layer. SET LOCAL so it cannot leak into a pooled session (§6).
		//
		// 🔴 THE VALUE IS PART OF THE ASSERTION, AND IT DID NOT USED TO BE (backlog
		// T48 item 2, closed M8-04 FAZ B3). This line read "SET LOCAL lock_timeout"
		// and asserted only that a ceiling EXISTS — measured: changing '5s' to
		// '5000s' left the whole suite green, i.e. a ceiling of eighty-three minutes
		// satisfied a check written to bound a tap's wait to five seconds. A number
		// that no gate holds is a number that drifts, and this one is published to
		// operators twice in deploy/README.md (see the pin below).
		"SET LOCAL lock_timeout = '5s';",
		// Post-condition 1: the reader saw the whole park. This is the ONLY thing
		// that catches an RLS-narrowed COPY, which the tool itself cannot see —
		// a partial read looks exactly like a small park.
		"SELECT count(*) INTO park_now FROM tags;",
		"IF park_now <> 2 THEN",
		// Post-condition 2: every planned row was matched, so a row changed
		// between the read and the write aborts instead of being overwritten.
		"IF applied <> 2 THEN",
		// The join carries the OLD ref, which is what makes post-condition 2
		// able to detect a concurrent change at all.
		"t.aes_key_ref = decode(v.oldref_hex, 'hex')",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("the emitted SQL is missing %q", want)
		}
	}

	// PL/pgSQL's RAISE uses % as its own placeholder, and Go's formatter uses it
	// too. An earlier draft shipped `%!m(MISSING)` into the SQL from exactly that
	// collision, so the absence of Go's error marker is asserted rather than
	// assumed.
	if strings.Contains(sql, "%!") {
		t.Error("the emitted SQL contains a Go format error marker")
	}

	// 🔴 AND THE OPERATOR DOCUMENT IS HELD TO THE SAME NUMBER (backlog T48 item 2).
	// deploy/README.md publishes this ceiling to whoever runs a rotation, in two
	// places — a prose paragraph and the failure table's row — and both quote the
	// literal. Pinning only the emitted SQL would leave the published figure free
	// to drift away from what the tool actually emits, which is the one-sided
	// change nobody notices (the class TestObservability_AlertSignalNames exists
	// for, applied here).
	runbook, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "README.md"))
	if err != nil {
		t.Fatalf("read the runbook: %v", err)
	}
	const published = "SET LOCAL lock_timeout = '5s'"
	if n := strings.Count(string(runbook), published); n < 2 {
		t.Errorf("deploy/README.md quotes %q %d time(s), want at least 2 — the tool emits "+
			"that ceiling and the runbook is where an operator reads it. If the value "+
			"changed, change it in BOTH, in this edit.", published, n)
	}
}

// TestWriteSQL_NothingToDoStillChecksTheParkSize.
//
// A re-run of a finished rotation has no UPDATE to emit — and that is precisely
// when it is most tempting to emit nothing at all. It must still verify it saw
// the whole park, otherwise "nothing to do" would be the answer to an
// RLS-blinded read as well as to a finished one.
func TestWriteSQL_NothingToDoStillChecksTheParkSize(t *testing.T) {
	kekNew, kekOld := testKEK(1), testKEK(2)
	var out, errOut bytes.Buffer
	stdin := line(uidA, tenantA, seal(t, kekNew, uidA, fakeTagKey))
	if code := run(strings.NewReader(stdin), &out, &errOut, envOf(map[string]string{
		kekEnv: kekNew, prevKEKEnv: kekOld,
	})); code != 0 {
		t.Fatalf("exit = %d: %s", code, errOut.String())
	}
	sql := out.String()
	if !strings.Contains(sql, "IF park_now <> 1 THEN") {
		t.Error("a no-op run must still assert it read the whole park")
	}
	if strings.Contains(sql, "UPDATE tags") {
		t.Error("a no-op run must not emit an UPDATE")
	}
}

// TestReport_NeverPrintsKeyMaterial — §4.7, mechanically.
//
// The report is the thing an operator pastes into an incident channel, so it is
// the most likely place for a secret to escape. It may carry counts, tenant ids
// and lengths; it may not carry a KEK, a per-tag key, a wrapped ref or a uid.
func TestReport_NeverPrintsKeyMaterial(t *testing.T) {
	kekNew, kekOld := testKEK(1), testKEK(2)
	strayRef := seal(t, testKEK(9), uidA, fakeTagKey)
	stdin := strings.Join([]string{
		line(uidA, tenantA, strayRef),
		line(uidB, tenantB, seal(t, kekOld, uidB, fakeTagKey)),
	}, "\n")

	var out, errOut bytes.Buffer
	run(strings.NewReader(stdin), &out, &errOut, envOf(map[string]string{
		kekEnv: kekNew, prevKEKEnv: kekOld, ackEnv: "1",
	}))
	report := errOut.String()

	for name, secret := range map[string]string{
		"the new KEK":       kekNew,
		"the previous KEK":  kekOld,
		"a per-tag key":     hex.EncodeToString(fakeTagKey),
		"a wrapped ref":     strayRef,
		"a plaque's uid":    uidA,
		"the other uid":     uidB,
		"base64 of the key": base64.StdEncoding.EncodeToString(fakeTagKey),
	} {
		if strings.Contains(report, secret) {
			t.Errorf("the report contains %s", name)
		}
	}
	// POSITIVE CONTROL: the scan is looking at something real. If the report were
	// empty, the loop above would pass against anything.
	if !strings.Contains(report, tenantA) {
		t.Fatal("CONTROL FAILED: the report does not carry the tenant breakdown it is supposed to")
	}
	if !strings.Contains(report, "open under NEITHER KEK") {
		t.Fatal("CONTROL FAILED: the report is not the report")
	}
}

// TestReadRows_TruncatedStreamIsNotAShortPark.
//
// A read that DIES must never look like a read that ENDED. This is the
// "connection dropped mid-run" path: a garbled or over-long line means the input
// is a truncated read of the park, and rotating the prefix while reporting
// success would leave the rest under the leaked KEK.
func TestReadRows_TruncatedStreamIsNotAShortPark(t *testing.T) {
	kekNew, kekOld := testKEK(1), testKEK(2)
	// A line longer than the scanner's ceiling: bufio.Scanner's DEFAULT behaviour
	// here is to stop early, which would silently truncate the park.
	huge := strings.Repeat("a", 2<<20)
	stdin := line(uidA, tenantA, seal(t, kekOld, uidA, fakeTagKey)) + "\n" + huge

	var out, errOut bytes.Buffer
	code := run(strings.NewReader(stdin), &out, &errOut, envOf(map[string]string{
		kekEnv: kekNew, prevKEKEnv: kekOld,
	}))
	if code != 1 {
		t.Fatalf("exit = %d, want 1 for a truncated read", code)
	}
	if !strings.Contains(errOut.String(), "TRUNCATED") {
		t.Errorf("the operator must be told the input was truncated, got: %s", errOut.String())
	}
}

// TestRun_ManyTenants: the park is 61 821 tenants wide, so "one tenant" is the
// unrepresentative case. This walks several shapes at once and checks the counts
// add up — read == rewrapped + already + unopenable is the invariant that makes
// the report trustworthy at all.
func TestRun_ManyTenants(t *testing.T) {
	kekNew, kekOld, kekLost := testKEK(1), testKEK(2), testKEK(9)

	tests := []struct {
		name    string
		tenants int
		perT    int
	}{
		{"a single tenant", 1, 3},
		{"one plaque each across many tenants", 200, 1},
		{"several plaques each", 50, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			var wantRewrap, wantLost int
			n := 0
			for ti := 0; ti < tc.tenants; ti++ {
				tenant := fmt.Sprintf("%08x-0000-4000-8000-000000000001", ti)
				for p := 0; p < tc.perT; p++ {
					uid := fmt.Sprintf("%014X", n)
					n++
					kek := kekOld
					if n%7 == 0 { // a scattering of rows nobody can open
						kek = kekLost
						wantLost++
					} else {
						wantRewrap++
					}
					if b.Len() > 0 {
						b.WriteString("\n")
					}
					b.WriteString(line(uid, tenant, seal(t, kek, uid, fakeTagKey)))
				}
			}
			var out, errOut bytes.Buffer
			code := run(strings.NewReader(b.String()), &out, &errOut, envOf(map[string]string{
				kekEnv: kekNew, prevKEKEnv: kekOld, ackEnv: fmt.Sprint(wantLost),
			}))
			wantCode := 3 // LITERAL: the runbook's stop rule is "exit 3 -> do not run step 3"
			if wantLost == 0 {
				wantCode = 0
			}
			if code != wantCode {
				t.Fatalf("exit = %d, want %d\n%s", code, wantCode, errOut.String())
			}
			for _, want := range []string{
				fmt.Sprintf("rows read from stdin ............ %d", n),
				fmt.Sprintf("re-sealed under the new KEK ..... %d", wantRewrap),
				fmt.Sprintf("open under NEITHER KEK .......... %d", wantLost),
			} {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("report is missing %q\n%s", want, errOut.String())
				}
			}
			if wantRewrap > 0 && !strings.Contains(out.String(), fmt.Sprintf("IF applied <> %d THEN", wantRewrap)) {
				t.Error("post-condition 2 does not match the number of rows planned")
			}
		})
	}
}

// parseNewRefs pulls (uid -> new wrapped ref) out of the generated VALUES list,
// so the round-trip test reads what psql would receive rather than an internal.
// parseNewRefs pulls (uid -> new wrapped ref) out of the COPY payload, so the
// round-trip test reads what psql would receive.
//
// The refs used to live in a VALUES list inside the DO block; they now travel as
// COPY DATA so that an aborted rotation cannot log them (see writeSQL). This
// parser follows that move deliberately: it reads only the lines between
// `COPY ... FROM STDIN;` and the `\.` terminator, which is exactly the region
// Postgres does NOT treat as statement text.
func parseNewRefs(t *testing.T, sql string) map[string]string {
	t.Helper()
	refs := map[string]string{}
	in := false
	for _, ln := range strings.Split(sql, "\n") {
		if strings.HasPrefix(ln, "COPY rotatekek_in") {
			in = true
			continue
		}
		if in && ln == `\.` {
			in = false
			continue
		}
		if !in || ln == "" {
			continue
		}
		f := strings.Split(ln, "\t")
		if len(f) != 4 {
			t.Fatalf("a COPY data line has %d fields, want 4: %q", len(f), ln)
		}
		refs[f[0]] = f[3]
	}
	return refs
}

// TestExitCodes_AreTheNumbersTheRunbookPublishes.
//
// The table above used to assert the constants against themselves
// (`wantExit: exitIncomplete`), which is not an assertion: an auditor changed
// exitIncomplete from 3 to 0 and the package stayed GREEN. Under that mutation a
// partial rotation — rows still under the leaked KEK — exits 0 and the runbook's
// stop rule silently stops firing. These numbers are a PUBLISHED INTERFACE.
func TestExitCodes_AreTheNumbersTheRunbookPublishes(t *testing.T) {
	for _, tc := range []struct {
		name string
		got  int
		want int
	}{
		{"success", 0, 0},
		{"refused, nothing written", exitRefused, 1},
		{"SQL written but the park is NOT fully rotated", exitIncomplete, 3},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: exit code is %d, but deploy/README.md publishes %d and the operator's "+
				"stop rules branch on it", tc.name, tc.got, tc.want)
		}
	}
	if exitRefused == 0 || exitIncomplete == 0 || exitRefused == exitIncomplete {
		t.Error("the three outcomes must have three distinct exit codes")
	}
}

// TestExitCodes_TheCOMPILEDBinaryReallyDeliversThem runs the artifact the runbook
// runs. Pinning the constants pinned the wrong layer: they stayed correct while
// `go run` delivered 1 for a program that exited 3.
func TestExitCodes_TheCOMPILEDBinaryReallyDeliversThem(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	bin := filepath.Join(t.TempDir(), "rotatekek")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("building the tool: %v\n%s", err, out)
	}
	kekNew, kekOld := testKEK(1), testKEK(2)
	tests := []struct {
		name  string
		stdin string
		env   []string
		want  int
	}{
		{"a clean rotation delivers 0", line(uidA, tenantA, seal(t, kekOld, uidA, fakeTagKey)),
			[]string{kekEnv + "=" + kekNew, prevKEKEnv + "=" + kekOld}, 0},
		{"a refusal delivers 1", "",
			[]string{kekEnv + "=" + kekNew, prevKEKEnv + "=" + kekOld}, 1},
		{"a partial rotation delivers 3, NOT 1", line(uidA, tenantA, seal(t, testKEK(9), uidA, fakeTagKey)),
			[]string{kekEnv + "=" + kekNew, prevKEKEnv + "=" + kekOld, ackEnv + "=1"}, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bin)
			cmd.Stdin = strings.NewReader(tc.stdin)
			cmd.Env = append(os.Environ(), tc.env...)
			cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
			got := 0
			if err := cmd.Run(); err != nil {
				var ee *exec.ExitError
				if !errors.As(err, &ee) {
					t.Fatalf("running the binary: %v", err)
				}
				got = ee.ExitCode()
			}
			if got != tc.want {
				t.Errorf("the COMPILED binary delivered %d, want %d — deploy/README.md publishes this "+
					"number and the operator's stop rules branch on it", got, tc.want)
			}
		})
	}
}

// TestRunbook_InvokesTheToolSoEveryExitCodeIsReadable.
//
// 🔴 TWO LAYERS OF THE SAME DEFECT, FOUND ONE ROUND APART. First the runbook piped
// through `go run`, which collapsed 3 to 1. That was fixed — and the fix was still
// wrong, because the tool was piped STRAIGHT INTO psql: refuse() deliberately
// emits a poison statement, psql dies with 3, `pipefail` reports the RIGHTMOST
// non-zero status, and the tool's own 1 is masked. Measured: the tool alone
// returned 1 while the runbook's literal pipeline returned 3, so the published
// code for "REFUSED, nothing was written" was UNREACHABLE and three distinct
// outcomes collapsed onto one number.
//
// The fix is structural rather than another special case: don't pipe. Run the
// tool, gate ITS exit code, then hand the file to psql. Every code becomes
// readable and the poison statement stays as a second belt.
func TestRunbook_InvokesTheToolSoEveryExitCodeIsReadable(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "README.md"))
	if err != nil {
		t.Fatalf("reading deploy/README.md: %v", err)
	}
	text := string(readme)

	if strings.Contains(text, "go run ./cmd/rotatekek") {
		t.Error("deploy/README.md invokes the tool with `go run`, which collapses exit 3 to 1")
	}
	// POSITIVE half: the tool must be BUILT and run as a binary on the documented
	// path.
	//
	// ⚠️ THIS ASSERTION MOVED WITH THE BUILD. It used to require "go build -o" in
	// the README, which held only while the runbook carried its own build block —
	// a block later measured to be DEAD (nothing ran what it built) and to leak a
	// temp dir through a trap the next step overwrote. Keeping the assertion on
	// the README would have kept that dead code alive; the build lives in
	// scripts/rotate-kek.sh, so the requirement belongs there.
	scriptSrc, err := os.ReadFile(filepath.Join(repoRoot(t), "scripts", "rotate-kek.sh"))
	if err != nil {
		t.Fatalf("reading scripts/rotate-kek.sh: %v", err)
	}
	if !strings.Contains(string(scriptSrc), "go build -o") {
		t.Error("scripts/rotate-kek.sh never builds the rotation tool; the published exit-code table " +
			"cannot be delivered without a compiled binary (go run collapses 3 to 1)")
	}
	// THE PIPE INTO psql MUST BE GONE. Any `| psql` downstream of the tool re-masks
	// the tool's exit code behind psql's.
	//
	// ⚠️ THE FIRST VERSION OF THIS CHECK MATCHED THE BARE SUBSTRING "rotatekek |"
	// AND FIRED ON `go build -o "$WORK/rotatekek" ./cmd/rotatekek || …` — the `||`
	// of an error guard. A detector that cannot tell a shell pipe from a logical OR
	// reports the fix as the defect. It now requires a pipe whose downstream is
	// psql, across an optional line continuation.
	pipeToPsql := regexp.MustCompile(`rotatekek"?[^\n|]*\|\s*(?:\\\s*\n\s*)?psql`)
	if m := pipeToPsql.FindString(text); m != "" {
		t.Errorf("the runbook still pipes the tool into psql (%q): psql's status hides the tool's, "+
			"and exit 1 becomes unreachable", m)
	}
	// CONTROL: the matcher fires on the shape it is meant to catch, and NOT on the
	// error-guard shape that fooled its first version.
	if !pipeToPsql.MatchString(`x | /tmp/rotatekek | psql "$DSN"`) {
		t.Fatal("CONTROL FAILED: the pipe matcher does not match a real pipe into psql")
	}
	if pipeToPsql.MatchString(`go build -o "$WORK/rotatekek" ./cmd/rotatekek || { echo x; }`) {
		t.Fatal("CONTROL FAILED: the pipe matcher fires on `||`, which is an error guard, not a pipe")
	}
	// POSITIVE half: the tool's own status is captured and branched on.
	if !strings.Contains(text, "rc=$?") {
		t.Error("the runbook never captures the tool's exit status; the published table cannot be acted on")
	}
}

// repoRoot resolves the repository root from this package's directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving the repository root: %v", err)
	}
	return abs
}

// TestWriteSQL_NoWrappedRefEverAppearsInSTATEMENTTEXT — the B1 mechanism, pinned.
//
// 🔴 WHAT THIS DEFENDS. Postgres logs the FULL TEXT of a statement that raises an
// error (log_min_error_statement defaults to 'error'). While the rotation was one
// ~19 MB DO block, every ABORTED run wrote the whole park's wrapped refs into the
// server log — including the refs under the NEW KEK, which stay sensitive after
// the rotation completes. Measured on a 20-row probe with log_statement='none'
// and log_min_duration_statement=-1 — the runbook's own precondition satisfied —
// 40 distinct 88-hex refs reached the log. Aborts are routine here; the runbook
// tells the operator to re-read and re-run.
//
// The fix had to be a MECHANISM, not a third value in a precondition an operator
// or a managed provider can fail to hold. So the refs travel as COPY DATA, which
// is not statement text. This asserts exactly that: every wrapped ref lies
// between `COPY ... FROM STDIN;` and its terminator, and no statement outside
// that region contains one.
func TestWriteSQL_NoWrappedRefEverAppearsInSTATEMENTTEXT(t *testing.T) {
	kekNew, kekOld := testKEK(1), testKEK(2)
	var b strings.Builder
	for i := 0; i < 5; i++ {
		uid := fmt.Sprintf("%014X", i)
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(line(uid, tenantA, seal(t, kekOld, uid, fakeTagKey)))
	}
	var out, errOut bytes.Buffer
	if code := run(strings.NewReader(b.String()), &out, &errOut, envOf(map[string]string{
		kekEnv: kekNew, prevKEKEnv: kekOld,
	})); code != 0 {
		t.Fatalf("exit = %d: %s", code, errOut.String())
	}
	sql := out.String()

	refRe := regexp.MustCompile(`[0-9a-f]{88}`)
	// CONTROL: there ARE refs in this output, so the scan is not vacuous.
	if len(refRe.FindAllString(sql, -1)) == 0 {
		t.Fatal("CONTROL FAILED: no wrapped refs in the generated SQL at all")
	}

	inCopy := false
	for i, ln := range strings.Split(sql, "\n") {
		if strings.HasPrefix(ln, "COPY rotatekek_in") {
			inCopy = true
			continue
		}
		if inCopy && ln == `\.` {
			inCopy = false
			continue
		}
		if inCopy {
			continue // COPY DATA: not statement text, never logged on error
		}
		if refRe.MatchString(ln) {
			t.Errorf("line %d is STATEMENT TEXT and contains a wrapped ref; an aborted rotation would "+
				"write it to the server log:\n  %.120s", i+1, ln)
		}
	}

	// DOLLAR-QUOTE TAGS MUST BALANCE. Renaming only the opening tag of a block
	// leaves syntactically invalid SQL that every content assertion still passes —
	// measured: a mutation that did exactly that stayed green, and psql would have
	// been handed an unterminated dollar-quoted string.
	for _, tag := range []string{"$guard$", "$rotate$"} {
		if n := strings.Count(sql, tag); n != 2 {
			t.Errorf("%s appears %d times, want exactly 2 (open and close); an unbalanced dollar-quote "+
				"tag makes the whole transaction unparseable", tag, n)
		}
	}

	// And the guards must precede the data, so the routine abort (park size
	// changed) happens before a single ref is transmitted.
	iGuard := strings.Index(sql, "$guard$")
	iCopy := strings.Index(sql, "COPY rotatekek_in")
	if iGuard < 0 || iCopy < 0 {
		t.Fatal("the guard block or the COPY is missing entirely")
	}
	if iGuard > iCopy {
		t.Error("the session/park guards run AFTER the data is sent; the most common abort would " +
			"transmit the whole park first")
	}
	for _, want := range []string{"app.tenant_id", "rolsuper OR rolbypassrls", "park_now <>"} {
		if idx := strings.Index(sql, want); idx < 0 || idx > iCopy {
			t.Errorf("guard %q does not run before the COPY payload", want)
		}
	}
}

// failingWriter fails after letting n bytes through, so the tool's own write path
// can be exercised without depending on a platform device.
type failingWriter struct {
	n   int
	err error
}

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, f.err
	}
	if len(p) > f.n {
		k := f.n
		f.n = 0
		return k, f.err
	}
	f.n -= len(p)
	return len(p), nil
}

// TestRun_AFailedWriteIsARefusalNotASuccess.
//
// 🔴 THIS TEST WAS WRITTEN, THEN DESTROYED BY MY OWN EDIT, AND ITS COMMENT SURVIVED
// TO KEEP CLAIMING IT. A later refactor of this file replaced everything after one
// function, deleting this test along with two others, while writeAll's doc comment
// went on naming it as the thing that exercised the path. An auditor then made
// writeAll return nil on failure and the whole suite stayed green: the hole this
// test exists to close could be reopened silently.
//
// The claim it holds: refuse() poisons the SQL stream because "emitting nothing" is
// not fail-closed — without `set -o pipefail`, psql fed empty input exits 0 and a
// refusal reads as a completed rotation. The SUCCESS path had exactly that hole
// (`_, _ = io.WriteString`), printing "EXIT 0: every row read is accounted for"
// after writing nothing.
func TestRun_AFailedWriteIsARefusalNotASuccess(t *testing.T) {
	kekNew, kekOld := testKEK(1), testKEK(2)
	stdin := line(uidA, tenantA, seal(t, kekOld, uidA, fakeTagKey))

	for _, tc := range []struct {
		name  string
		after int
	}{
		{"the write fails immediately", 0},
		{"the write fails part way through the transaction", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var errOut bytes.Buffer
			out := &failingWriter{n: tc.after, err: errors.New("no space left on device")}
			code := run(strings.NewReader(stdin), out, &errOut, envOf(map[string]string{
				kekEnv: kekNew, prevKEKEnv: kekOld,
			}))
			if code != 1 {
				t.Fatalf("exit = %d, want 1: a rotation whose SQL never reached the pipe is not a success", code)
			}
			if strings.Contains(errOut.String(), "EXIT 0") {
				t.Error("stderr still claims success after the SQL could not be written")
			}
			if !strings.Contains(errOut.String(), "TRUNCATED") {
				t.Errorf("the operator is not told the transaction may be truncated: %s", errOut.String())
			}
		})
	}
}

// TestReadAck_NeverEchoesTheRawValue — §4.7, and a protection whose removal was
// measured to be completely invisible.
//
// readAck used to print %q of the raw environment value. Exporting a KEK into this
// variable by mistake put the whole base64 key on stderr — the stderr the runbook
// tells the operator to KEEP as the only record of the run. Reverting the fix left
// every test green, so the fix was decoration until this existed.
func TestReadAck_NeverEchoesTheRawValue(t *testing.T) {
	secret := "omAfxiWEwnollFiosY/ykdPOo2BXdn59WNPzTmqkWeE="
	for _, bad := range []string{secret, "abc", "-1", "12x"} {
		_, _, err := readAck(func(k string) string {
			if k == ackEnv {
				return bad
			}
			return ""
		})
		if err == nil {
			t.Fatalf("%q should not parse as a count", bad)
		}
		if strings.Contains(err.Error(), bad) {
			t.Errorf("the error echoes the raw value back (%q); §4.7 allows a LENGTH, never the content", bad)
		}
	}
	// POSITIVE: a valid value is still accepted, so this is not passing because
	// readAck refuses everything.
	n, set, err := readAck(func(k string) string {
		if k == ackEnv {
			return "42"
		}
		return ""
	})
	if err != nil || !set || n != 42 {
		t.Fatalf("a valid count must parse: n=%d set=%v err=%v", n, set, err)
	}
}

// TestWriteSQL_TempTableIsDroppedOnCommit.
//
// ON COMMIT DROP is what keeps the wrapped refs from outliving the transaction in
// a table anyone with the session can read. Deleting it left every test green.
func TestWriteSQL_TempTableIsDroppedOnCommit(t *testing.T) {
	kekNew, kekOld := testKEK(1), testKEK(2)
	var out, errOut bytes.Buffer
	if code := run(strings.NewReader(line(uidA, tenantA, seal(t, kekOld, uidA, fakeTagKey))),
		&out, &errOut, envOf(map[string]string{kekEnv: kekNew, prevKEKEnv: kekOld})); code != 0 {
		t.Fatalf("exit = %d: %s", code, errOut.String())
	}
	sql := out.String()
	if !strings.Contains(sql, "CREATE TEMP TABLE") {
		t.Fatal("the staging table is not TEMP; its rows would be ordinary table data, WAL-logged and " +
			"visible to other sessions")
	}
	if !strings.Contains(sql, "ON COMMIT DROP") {
		t.Error("the staging table is not ON COMMIT DROP: the park's wrapped refs would survive the " +
			"transaction inside pg_temp for the rest of the session")
	}
}
