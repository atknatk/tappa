package invite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"testing"
)

// TestPairingCode_PrintsRedactedEverywhereButDigits: the pairing code is kept out of every log line
// and audit detail (M16-23, §7) -- every printing interface answers the placeholder, and Digits,
// the one egress, the six digits.
func TestPairingCode_PrintsRedactedEverywhereButDigits(t *testing.T) {
	p := ParsePairingCode("482913")
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Info("x", "code", p, "approval", Approval{PairingCode: p})
	j, _ := json.Marshal(struct{ C PairingCode }{p})
	for name, out := range map[string]string{
		"%v": fmt.Sprintf("%v", p), "%s": fmt.Sprintf("%s", p), "%#v": fmt.Sprintf("%#v", p), "%d": fmt.Sprintf("%d", p),
		"Sprint": fmt.Sprint(p), "slog": buf.String(), "json": string(j),
		"in a struct": fmt.Sprintf("%+v", Approval{PairingCode: p}),
	} {
		if strings.Contains(out, "482913") {
			t.Errorf("%s prints the code: %s", name, out)
		}
	}
	if p.Digits() != "482913" || (PairingCode{}).Digits() != "" {
		t.Fatal("Digits is not the code's egress")
	}
}

// TestNewPairingCode_IsSixDigits: the code is six ASCII digits (00115's CHECK), drawn uniformly.
func TestNewPairingCode_IsSixDigits(t *testing.T) {
	seen := map[string]bool{}
	for range 200 {
		p, err := newPairingCode()
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(p.Digits()) {
			t.Fatalf("code %q is not six digits", p.Digits())
		}
		seen[p.Digits()] = true
	}
	if len(seen) < 190 {
		t.Fatalf("200 draws gave %d distinct codes", len(seen))
	}
}
