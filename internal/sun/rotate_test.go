package sun

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// Tests for the KEK ROTATION WINDOW (M8-02 phase F): UnwrapAny and the
// two-KEK Verifier.
//
// WHAT IS BEING PINNED, and why it is worth a file of its own. Nothing in the
// schema records which KEK a row is sealed under, so while cmd/rotatekek re-seals
// the park it is MIXED. A process holding one KEK answers 500 to every tap on the
// other half — the record is not flagged, it is never taken, which is §4.6 failing
// underneath the product. These tests are the pin on "both halves are readable",
// in BOTH directions, plus the §4.7 pin that the fallback never says WHICH key
// worked.
//
// All key material here is FAKE. fakeKEKHex, fakeKEK2Hex, fakeWrapKeyHex,
// fakeUIDHex and hexBytes come from the sibling test files in this package.

// fakeKEK3Hex is a THIRD fake 32-byte KEK: needed for the "opens under neither"
// case, which cannot be built from the two keys the round-trip tests use.
const fakeKEK3Hex = "0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f0f"

// TestUnwrapAny_TableOfRotationStates walks every state a row can be in during a
// rotation, against every key list a process can hold. One case per row of the
// matrix, which is what makes "the mixed park is readable in BOTH directions"
// a measurement rather than a claim.
func TestUnwrapAny_TableOfRotationStates(t *testing.T) {
	kekNew := hexBytes(t, fakeKEKHex)
	kekOld := hexBytes(t, fakeKEK2Hex)
	kekOther := hexBytes(t, fakeKEK3Hex)
	uid := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeWrapKeyHex)

	sealedNew, err := Wrap(kekNew, uid, key)
	if err != nil {
		t.Fatalf("Wrap under the new KEK: %v", err)
	}
	sealedOld, err := Wrap(kekOld, uid, key)
	if err != nil {
		t.Fatalf("Wrap under the old KEK: %v", err)
	}
	sealedOther, err := Wrap(kekOther, uid, key)
	if err != nil {
		t.Fatalf("Wrap under an unrelated KEK: %v", err)
	}

	// during is the ordering the runbook mandates while a rotation is live: the
	// key rows are moving TO comes first, the key they are moving FROM second.
	during := [][]byte{kekNew, kekOld}

	tests := []struct {
		name    string
		keks    [][]byte
		ref     []byte
		wantOK  bool
		wantKey []byte
	}{
		{
			name:    "steady state: one KEK, row already under it",
			keks:    [][]byte{kekNew},
			ref:     sealedNew,
			wantOK:  true,
			wantKey: key,
		},
		{
			name:   "the failure this whole window exists to prevent: one KEK, row under the other",
			keks:   [][]byte{kekNew},
			ref:    sealedOld,
			wantOK: false,
		},
		{
			name:    "rotation window, row ALREADY re-sealed (primary wins)",
			keks:    during,
			ref:     sealedNew,
			wantOK:  true,
			wantKey: key,
		},
		{
			name:    "rotation window, row NOT YET re-sealed (fallback wins)",
			keks:    during,
			ref:     sealedOld,
			wantOK:  true,
			wantKey: key,
		},
		{
			// The rollback direction. If the operator has to undo a rotation, the
			// two variables swap and the park is mixed the OTHER way round; a list
			// that only worked forwards would strand the rollback.
			name:    "rollback window, order reversed, row under what is now the fallback",
			keks:    [][]byte{kekOld, kekNew},
			ref:     sealedNew,
			wantOK:  true,
			wantKey: key,
		},
		{
			name:   "row sealed under a key nobody holds",
			keks:   during,
			ref:    sealedOther,
			wantOK: false,
		},
		{
			name:   "no KEK configured at all",
			keks:   nil,
			ref:    sealedNew,
			wantOK: false,
		},
		{
			name:   "length is still checked: a ref that was never an envelope",
			keks:   during,
			ref:    []byte{0xDE, 0xAD},
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := UnwrapAny(tc.keks, uid, tc.ref)
			if !tc.wantOK {
				if err == nil {
					t.Fatal("want an error, got a key")
				}
				if got != nil {
					t.Fatal("a failed unwrap must not return a key")
				}
				return
			}
			if err != nil {
				t.Fatalf("want the key, got error: %v", err)
			}
			if string(got) != string(tc.wantKey) {
				// The values are NOT printed (§4.7): the assertion is equality,
				// and a length is the most a failure message may carry.
				t.Fatalf("recovered a DIFFERENT key (%d bytes) than the one that was sealed", len(got))
			}
		})
	}
}

// TestUnwrapAny_ErrorNamesNoKEKAndNoPosition — §4.7, and the reason the fallback
// keeps GCM's fixed error rather than composing a helpful one.
//
// A message like "primary failed, previous succeeded" would tell a caller which
// half of a rotation a given plaque is in, and a message that quoted a key would
// be catastrophic. The error must say only "did not authenticate".
func TestUnwrapAny_ErrorNamesNoKEKAndNoPosition(t *testing.T) {
	kekNew := hexBytes(t, fakeKEKHex)
	kekOld := hexBytes(t, fakeKEK2Hex)
	uid := hexBytes(t, fakeUIDHex)
	key := hexBytes(t, fakeWrapKeyHex)

	sealedOther, err := Wrap(hexBytes(t, fakeKEK3Hex), uid, key)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	_, err = UnwrapAny([][]byte{kekNew, kekOld}, uid, sealedOther)
	if err == nil {
		t.Fatal("a ref under an unheld key must not open")
	}
	msg := err.Error()

	// No key material, in any encoding this package could plausibly emit.
	for _, forbidden := range []string{
		string(kekNew), string(kekOld), string(key),
		fakeKEKHex, fakeKEK2Hex, fakeWrapKeyHex,
	} {
		if strings.Contains(msg, forbidden) {
			t.Fatal("the error message contains key material")
		}
	}
	// No positional tell.
	for _, forbidden := range []string{"primary", "previous", "second", "fallback", "first key", "keks[0]"} {
		if strings.Contains(strings.ToLower(msg), forbidden) {
			t.Fatalf("the error names WHICH key failed (%q); a caller must learn only that none authenticated", forbidden)
		}
	}
	// 🔴 AND NO NUMERIC POSITION EITHER. The word list above was a DENYLIST and it
	// had the hole every denylist has: an auditor's mutation formatted the index
	// instead of a word ("KEK #0: …") and passed it untouched. GCM's fixed error
	// carries no digits at all, so the absence of digits is a property rather than
	// another word to remember.
	if d := regexp.MustCompile(`[0-9]`).FindString(msg); d != "" {
		t.Fatalf("the error contains a digit (%q) — a position index tells a caller which KEK was tried "+
			"and how far it got: %q", d, msg)
	}
	// And it is still the fixed authentication failure, so callers that already
	// match on it keep working.
	if !strings.Contains(msg, "authentication failed") {
		t.Fatalf("want GCM's fixed authentication error, got %q", msg)
	}
}

// TestUnwrapAny_EmptyListIsAnErrorNotASilentMiss.
//
// A caller that lost its keys must NOT be indistinguishable from a caller holding
// a corrupt ref: the first is a deployment fault to fix, the second is a data
// fault to investigate, and merging them sends the operator to the wrong place.
func TestUnwrapAny_EmptyListIsAnErrorNotASilentMiss(t *testing.T) {
	uid := hexBytes(t, fakeUIDHex)
	ref, err := Wrap(hexBytes(t, fakeKEKHex), uid, hexBytes(t, fakeWrapKeyHex))
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	_, err = UnwrapAny(nil, uid, ref)
	if err == nil {
		t.Fatal("an empty KEK list must be an error")
	}
	if !strings.Contains(err.Error(), "no KEK configured") {
		t.Fatalf("the error should say the KEK list is empty, got %q", err)
	}
}

// TestNewVerifier_DropsEmptyPreviousKEKs pins the contract main.go depends on:
// cfg.TagKEKPrevious is nil in the steady state and is passed straight through
// without a branch, so NewVerifier must not turn that nil into a key.
//
// A nil entry left in the list would be a REAL fault, not a cosmetic one:
// UnwrapAny would call Unwrap with a zero-length KEK on every single tap, and the
// wasted attempt would be silent.
func TestNewVerifier_DropsEmptyPreviousKEKs(t *testing.T) {
	kek := hexBytes(t, fakeKEKHex)
	prev := hexBytes(t, fakeKEK2Hex)

	tests := []struct {
		name     string
		previous [][]byte
		wantLen  int
	}{
		{"steady state: no previous argument at all", nil, 1},
		{"steady state: an unset config value passed straight through", [][]byte{nil}, 1},
		{"steady state: an empty (non-nil) slice", [][]byte{{}}, 1},
		{"rotation window: a real previous KEK", [][]byte{prev}, 2},
		{"a real one and an unset one", [][]byte{prev, nil}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := NewVerifier(&fakeResolver{}, kek, tc.previous...)
			if len(v.keks) != tc.wantLen {
				t.Fatalf("verifier holds %d KEKs, want %d", len(v.keks), tc.wantLen)
			}
			if string(v.keks[0]) != string(kek) {
				t.Fatal("the primary KEK must be first: order decides which key is tried first")
			}
			for i, k := range v.keks {
				if len(k) == 0 {
					t.Fatalf("KEK %d is empty; every tap would spend an attempt on it", i)
				}
			}
		})
	}
}

// TestVerify_TapOnANotYetRotatedPlaqueStillWorks is the END-TO-END statement of
// the window, at the level the product actually experiences it: a real Verify
// call against a plaque whose stored ref is still under the OLD KEK.
//
// Without the fallback this returns an error and the handler answers 500, so the
// tap is never recorded at all. The negative control below is the same tap
// against a one-KEK verifier, and it must fail — otherwise this test would pass
// even if the fallback were deleted.
func TestVerify_TapOnANotYetRotatedPlaqueStillWorks(t *testing.T) {
	kekNew := hexBytes(t, fakeKEKHex)
	kekOld := hexBytes(t, fakeKEK2Hex)
	uidBytes := hexBytes(t, fakeUIDHex)
	tagKey := hexBytes(t, fakeWrapKeyHex)

	// A plaque the rotation has NOT reached yet: still sealed under the old KEK.
	notYetRotated := fakeActiveTag(t, kekOld, uidBytes, tagKey, "active")
	params := mustParse(t, fakeUIDHex, "000065", "83c0104d8ac39077")

	t.Run("during the rotation window the tap is verified", func(t *testing.T) {
		f := &fakeResolver{tag: notYetRotated}
		ver := NewVerifier(f, kekNew, kekOld)
		res, err := ver.Verify(context.Background(), params)
		if err != nil {
			t.Fatalf("a tap on a not-yet-rotated plaque must not error: %v", err)
		}
		// The CMAC in this fixture is not the genuine one for this key, so the
		// outcome is an ordinary SUN reject — which is the POINT: the flow got
		// past unwrapping and reached the MAC comparison instead of dying at the
		// envelope. A 500 and a reject are different products.
		if res.SUNValid {
			t.Fatal("fixture sanity: this CMAC is not genuine, so SUNValid must be false")
		}
	})

	t.Run("NEGATIVE CONTROL: without the fallback the same tap is an error", func(t *testing.T) {
		f := &fakeResolver{tag: notYetRotated}
		ver := NewVerifier(f, kekNew)
		if _, err := ver.Verify(context.Background(), params); err == nil {
			t.Fatal("with only the new KEK this tap MUST fail; if it does not, the test above proves nothing")
		}
	})
}
