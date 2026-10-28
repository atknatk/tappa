package adminauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
)

// TestProof_BindsTheRowAndTheDigest (M14-07): a proof holds for exactly the (admin, tenant, digest)
// it was made for -- not for another row carrying the same digest (an identity's linked rows all
// do), not for another digest of the same row (a password change, a reset, a refusal's sentinel) --
// and not under another deployment's key; the zero proof holds for nothing.
func TestProof_BindsTheRowAndTheDigest(t *testing.T) {
	m, err := New(&fakeResolver{}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	admin, tenant := uuid.New(), uuid.New()
	// Any string: the proof binds bytes, it does not parse them.
	digest := "a stand-in digest " + uuid.NewString()
	p := m.proofOf(admin, tenant, digest)
	if !m.proofHolds(p, admin, tenant, digest) {
		t.Fatal("control: a proof does not hold for what it was made for")
	}
	for name, ok := range map[string]bool{
		"another admin":  m.proofHolds(p, uuid.New(), tenant, digest),
		"another tenant": m.proofHolds(p, admin, uuid.New(), digest),
		"another digest": m.proofHolds(p, admin, tenant, digest+"x"),
		"the zero proof": m.proofHolds(Proof{}, admin, tenant, digest),
	} {
		if ok {
			t.Errorf("the proof holds for %s", name)
		}
	}
	otherKey := &config.Config{Env: config.EnvDev, BaseURL: "https://panel.example.test", SessionHMACKey: []byte(strings.Repeat("k", 32))}
	other, err := New(&fakeResolver{}, otherKey)
	if err != nil {
		t.Fatal(err)
	}
	if other.proofHolds(p, admin, tenant, digest) {
		t.Error("a proof made under one key holds under another")
	}
	if bytes.Equal(m.proofKey, m.hmacKey) {
		t.Error("the proof key equals the session token key; the labels must separate them")
	}
}

// TestProof_WireFormRoundTripsAndRefusesEverythingElse: Encode / ParseProof are inverse; "" is the
// zero proof; any other shape is refused rather than repaired.
func TestProof_WireFormRoundTripsAndRefusesEverythingElse(t *testing.T) {
	m, err := New(&fakeResolver{}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	p := m.proofOf(uuid.New(), uuid.New(), "d")
	back, err := ParseProof(p.Encode())
	if err != nil || back != p || back.IsZero() {
		t.Fatalf("round trip = %v, %v", back.Encode(), err)
	}
	if z, err := ParseProof(""); err != nil || !z.IsZero() || z.Encode() != "" {
		t.Fatalf("the empty wire form is not the zero proof: %v", err)
	}
	for _, bad := range []string{"ab", strings.Repeat("a", 63), strings.Repeat("a", 65), strings.Repeat("AB", 32), strings.Repeat("zz", 32), " " + strings.Repeat("a", 63)} {
		if _, err := ParseProof(bad); err == nil {
			t.Errorf("ParseProof accepted %q", bad)
		}
	}
}

// TestProof_PrintsRedacted: every printing path -- fmt verbs, slog, JSON through MarshalText --
// writes the placeholder, never the MAC.
func TestProof_PrintsRedacted(t *testing.T) {
	m, err := New(&fakeResolver{}, testConfig())
	if err != nil {
		t.Fatal(err)
	}
	p := m.proofOf(uuid.New(), uuid.New(), "d")
	mac := p.Encode()
	var logged bytes.Buffer
	slog.New(slog.NewJSONHandler(&logged, nil)).Info("x", "proof", p, "verified", Verified{Proof: p})
	js, err := json.Marshal(Verified{Proof: p})
	if err != nil {
		t.Fatal(err)
	}
	for path, got := range map[string]string{
		"%v": fmt.Sprintf("%v", p), "%+v": fmt.Sprintf("%+v", Verified{Proof: p}), "%#v": fmt.Sprintf("%#v", Verified{Proof: p}),
		"%s": fmt.Sprintf("%s", p), "%x": fmt.Sprintf("%x", p), "slog": logged.String(), "json": string(js),
	} {
		if strings.Contains(got, mac) || strings.Contains(got, mac[:16]) {
			t.Errorf("%s leaks the proof: %q", path, got)
		}
		if !strings.Contains(got, proofRedacted) {
			t.Errorf("%s does not print the placeholder: %q", path, got)
		}
	}
}
