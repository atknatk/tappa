package adminauth

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// TestProofHash_PrintsNoHash (backlog T209): a draft's or a legacy session's keyed hash is the proof an
// identity function takes, and ClaimProof, SignupClaim and proofSession held it as a plain string.
// Printed whole, alone, or from a caller's unexported field, none carries it now.
//
// MUTANT (measured, 2026-11-08, reverted): proofHash.Format writing reveal() -- red.
func TestProofHash_PrintsNoHash(t *testing.T) {
	const hash = "c0ffee0123456789abcdefcanaryhash"
	h := newProofHash(hash)
	type held struct{ v any }
	values := map[string]any{
		"hash": h, "claim proof": ClaimProof{hash: h}, "signup claim": SignupClaim{hash: h},
		"proof session": proofSession{hash: h},
	}
	for name, v := range values {
		for _, form := range []any{v, &v, held{v}, []any{v}} {
			var texts []string
			for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q", "%x", "%d", "%p"} {
				texts = append(texts, fmt.Sprintf(verb, form))
			}
			var b strings.Builder
			slog.New(slog.NewJSONHandler(&b, nil)).Info("probe", "v", form)
			texts = append(texts, b.String())
			if j, err := json.Marshal(form); err == nil {
				texts = append(texts, string(j))
			}
			for i, text := range texts {
				if strings.Contains(text, hash) || strings.Contains(text, fmt.Sprintf("%x", hash)) {
					t.Errorf("%s, rendering #%d, carries the hash: %s", name, i, text)
				}
			}
		}
	}
	if h.reveal() != hash || newProofHash("").reveal() != "" || (proofHash{}).reveal() != "" {
		t.Error("reveal does not hand back what newProofHash wrapped")
	}
}
