package main

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"testing"
)

// zeroing_test.go — backlog T47(b): the test withUnwrapped's own comment claimed
// existed and did not.
//
// 🔴 WHAT THE COMMENT SAID AND WHAT WAS TRUE. withUnwrapped's doc reads "the wipe
// is a defer inside the only function that ever holds a plaintext key, so a caller
// cannot forget it and A TEST CAN WATCH IT HAPPEN (the buffer fn saw is all zeroes
// once this returns)". There was no such test: an audit turned sun.Zero into a
// no-op and the whole cmd/rotatekek suite stayed green. A §4.7 discipline was
// being DECLARED and nothing held it — the same shape the comment itself criticises
// two sentences earlier about the per-call-site wipes it replaced.
//
// This file is the missing half. It is the ONLY test in this package that looks at
// the plaintext key buffer at all.

// TestWithUnwrapped_WipesThePlaintextKeyOnEveryPath is the mutation gate: make
// sun.Zero a no-op, or delete the `defer sun.Zero(key)` line, and this turns red.
//
// It watches the SAME BACKING ARRAY the callback was handed rather than a copy,
// because that is what the claim is about — the memory the per-tag key lived in,
// after withUnwrapped has returned.
func TestWithUnwrapped_WipesThePlaintextKeyOnEveryPath(t *testing.T) {
	kekB64 := testKEK(7)
	kek, err := base64.StdEncoding.DecodeString(kekB64)
	if err != nil {
		t.Fatalf("kek: %v", err)
	}
	uidBytes, err := hex.DecodeString(uidA)
	if err != nil {
		t.Fatalf("uid: %v", err)
	}
	refHex := seal(t, kekB64, uidA, fakeTagKey)
	ref, err := hex.DecodeString(refHex)
	if err != nil {
		t.Fatalf("ref: %v", err)
	}

	cases := []struct {
		name string
		fn   func([]byte) error
		// wantOK is what withUnwrapped reports for a ref that DOES open.
		wantOK  bool
		wantErr bool
	}{
		{
			name:   "the callback succeeds",
			fn:     func([]byte) error { return nil },
			wantOK: true,
		},
		{
			// 🔴 THE PATH THAT MATTERS MOST. A wipe written as a plain statement
			// after fn(key) would be SKIPPED here, and this is the arm that would
			// still be green under that mistake if only the happy path were tested.
			name:    "the callback fails",
			fn:      func([]byte) error { return errProbe },
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var seen []byte // the callback's own slice, kept past the call
			ok, err := withUnwrapped(kek, uidBytes, ref, func(key []byte) error {
				// Positive control INSIDE the callback: if this is already zero,
				// the assertion after the call proves nothing.
				if !bytes.Equal(key, fakeTagKey) {
					t.Fatalf("the callback was handed %x, want the sealed key — "+
						"this test would pass vacuously", key)
				}
				seen = key
				return tc.fn(key)
			})

			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if seen == nil {
				t.Fatal("the callback never ran")
			}
			for i, b := range seen {
				if b != 0 {
					t.Fatalf("the plaintext per-tag key SURVIVED withUnwrapped: byte %d is non-zero. "+
						"§4.7 says the unwrapped key lives for one operation and is then wiped, and "+
						"withUnwrapped's own doc says a test can watch it happen — this is that test.", i)
				}
			}
		})
	}
}

// TestWithUnwrapped_ARefThatDoesNotOpenIsNotAnError pins the other half of the
// contract the file header states, so a change that "fixes" the wipe by making a
// failed unwrap an error is caught: a ref that opens under neither KEK is an
// ORDINARY outcome the tool counts and reports, not a failure.
func TestWithUnwrapped_ARefThatDoesNotOpenIsNotAnError(t *testing.T) {
	wrongKEK, err := base64.StdEncoding.DecodeString(testKEK(9))
	if err != nil {
		t.Fatalf("kek: %v", err)
	}
	uidBytes, _ := hex.DecodeString(uidA)
	ref, _ := hex.DecodeString(seal(t, testKEK(7), uidA, fakeTagKey))

	called := false
	ok, err := withUnwrapped(wrongKEK, uidBytes, ref, func([]byte) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("err = %v, want nil — a ref that does not authenticate is an outcome, not an error", err)
	}
	if ok {
		t.Fatal("ok = true for a ref sealed under a different KEK")
	}
	if called {
		t.Fatal("the callback ran on a ref that did not authenticate; there was no plaintext key to hand it")
	}
}

// errProbe is the callback failure used above. It is a package-level value so the
// test does not depend on error text.
var errProbe = probeError("probe: deliberate callback failure")

type probeError string

func (e probeError) Error() string { return string(e) }
