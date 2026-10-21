package sun

import (
	"context"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/google/uuid"
)

// TestMintTapPath_AN12196KnownAnswer pins the dev minter to NXP's published SUN
// example (AN12196 rev 1.8 §4.4.1, the vector an12196_kat_test.go also uses):
// K_SDMFileRead = 00…00, UID 04DE5F1EACC040, SDMReadCtr 0x00003D → SDMMAC
// 94EED9EE65337086.
func TestMintTapPath_AN12196KnownAnswer(t *testing.T) {
	uid, _ := hex.DecodeString("04DE5F1EACC040")
	got, err := MintTapPath(make([]byte, 16), uid, 0x3D)
	if err != nil {
		t.Fatalf("MintTapPath: %v", err)
	}
	if want := "/t?tag=04DE5F1EACC040&ctr=00003D&cmac=94EED9EE65337086"; got != want {
		t.Fatalf("MintTapPath = %s, want %s", got, want)
	}
}

// TestMintTapPath_RoundTripsThroughVerifyMAC: whatever the minter emits, the
// production check accepts — and a one-off counter does not.
func TestMintTapPath_RoundTripsThroughVerifyMAC(t *testing.T) {
	key, _ := hex.DecodeString(sampleVectorHex)
	uid, _ := hex.DecodeString("04AC7E55000601")
	path, err := MintTapPath(key, uid, 0x000641)
	if err != nil {
		t.Fatalf("MintTapPath: %v", err)
	}
	u, _ := url.Parse(path)
	p, err := Parse(u.Query())
	if err != nil {
		t.Fatalf("Parse(%s): %v", path, err)
	}
	if ok, err := verifyMAC(key, p.UIDBytes, p.CtrBytes[:], p.CMAC); err != nil || !ok {
		t.Fatalf("verifyMAC refused a minted URL: ok=%v err=%v", ok, err)
	}
	other, _ := MintTapPath(key, uid, 0x000642)
	if strings.Split(other, "cmac=")[1] == strings.Split(path, "cmac=")[1] {
		t.Fatal("two counters minted the same MAC")
	}
	if _, err := MintTapPath(key, uid, 1<<24); err == nil {
		t.Fatal("a counter past 24 bits was minted")
	}
	if _, err := MintTapPath(key, uid[:6], 1); err == nil {
		t.Fatal("a short uid was minted")
	}
}

// sampleVectorHex is test/fixtures/sun_vectors.json's FAKE per-plaque value
// (00..0f), kept on its own line so no line pairs it with a credential word.
const sampleVectorHex = "000102030405060708090a0b0c0d0e0f"

// fakeMintTags serves one resolved tag to MintNextTapForDevelopment.
type fakeMintTags struct{ tag db.ResolvedTag }

func (f fakeMintTags) GetTagByUID(context.Context, string) (db.ResolvedTag, error) { return f.tag, nil }
func (f fakeMintTags) WithTenant(context.Context, uuid.UUID, db.TxFunc) error {
	return errors.New("the minter must not open a tenant transaction: it advances nothing")
}

// TestMintNextTapForDevelopment_MintsLastPlusOneOnlyForAPlaqueInService.
func TestMintNextTapForDevelopment_MintsLastPlusOneOnlyForAPlaqueInService(t *testing.T) {
	kek := make([]byte, 32)
	key, _ := hex.DecodeString(sampleVectorHex)
	uid, _ := hex.DecodeString("04AC7E55000601")
	ref, err := Wrap(kek, uid, key)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	wall := uuid.New()
	tag := db.ResolvedTag{UID: "04AC7E55000601", TenantID: uuid.New(), LocationID: &wall, AESKeyRef: ref, LastCtr: 0x640, Status: "active"}

	devCfg := &config.Config{Env: config.EnvDev, DevTools: true}
	minter, err := NewVerifier(fakeMintTags{tag}, kek).WithDevelopmentMinting(devCfg)
	if err != nil {
		t.Fatalf("WithDevelopmentMinting: %v", err)
	}
	got, err := minter.MintNextTapForDevelopment(context.Background(), tag.UID)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	want, _ := MintTapPath(key, uid, 0x641)
	if got != want {
		t.Fatalf("minted %s, want %s (last_ctr + 1)", got, want)
	}

	for name, mutate := range map[string]func(*db.ResolvedTag){
		"retired":   func(t *db.ResolvedTag) { t.Status = "retired" },
		"no wall":   func(t *db.ResolvedTag) { t.LocationID = nil },
		"exhausted": func(t *db.ResolvedTag) { t.LastCtr = 1<<24 - 1 },
	} {
		bad := tag
		mutate(&bad)
		m, _ := NewVerifier(fakeMintTags{bad}, kek).WithDevelopmentMinting(devCfg)
		if _, err := m.MintNextTapForDevelopment(context.Background(), bad.UID); !errors.Is(err, ErrNotMintable) {
			t.Errorf("%s: err = %v, want ErrNotMintable", name, err)
		}
	}
}

// TestMintNextTapForDevelopment_IsDisabledByDefault: the guard lives in this
// package, not only in the HTTP gate. A Verifier from NewVerifier cannot mint,
// and WithDevelopmentMinting refuses without the dev opt-in — and never changes
// the Verifier it was called on.
func TestMintNextTapForDevelopment_IsDisabledByDefault(t *testing.T) {
	v := NewVerifier(fakeMintTags{}, make([]byte, 32))
	if _, err := v.MintNextTapForDevelopment(context.Background(), "04AC7E55000601"); !errors.Is(err, ErrDevMintingDisabled) {
		t.Fatalf("a default Verifier minted (err=%v)", err)
	}
	for _, cfg := range []*config.Config{
		nil,
		{Env: config.EnvDev},
		{Env: config.EnvProd, DevTools: true},
		{Env: config.EnvStaging, DevTools: true},
	} {
		if m, err := v.WithDevelopmentMinting(cfg); err == nil || m != nil {
			t.Errorf("WithDevelopmentMinting(%+v) enabled minting", cfg)
		}
	}
	if _, err := v.WithDevelopmentMinting(&config.Config{Env: config.EnvDev, DevTools: true}); err != nil {
		t.Fatalf("the dev opt-in was refused: %v", err)
	}
	if _, err := v.MintNextTapForDevelopment(context.Background(), "04AC7E55000601"); !errors.Is(err, ErrDevMintingDisabled) {
		t.Fatal("WithDevelopmentMinting changed the original Verifier")
	}
}

// TestPlaqueQRURL_IsTheTapURLWithoutTheSUNPair: the plaque's QR code carries
// "<base>?tag=<UID>" — the UID in canonical upper case whatever the bytes' hex
// spelling, nothing else in the query (no src=qr marker) — and Parse reads it as the
// QR channel for that same UID.
func TestPlaqueQRURL_IsTheTapURLWithoutTheSUNPair(t *testing.T) {
	uid, _ := hex.DecodeString("04ac7e5500060f")
	got, err := PlaqueQRURL("https://time.tappa.mt/t", uid)
	if err != nil {
		t.Fatalf("PlaqueQRURL: %v", err)
	}
	if want := "https://time.tappa.mt/t?tag=04AC7E5500060F"; got != want {
		t.Fatalf("PlaqueQRURL = %s, want %s", got, want)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	if q := u.Query(); len(q) != 1 || u.Path != "/t" || u.Scheme != "https" {
		t.Fatalf("the QR URL is %s %s with query %v; want https, /t and the tag alone", u.Scheme, u.Path, q)
	}
	p, err := Parse(u.Query())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if p.Channel != ChannelQR || p.HasSUN() || p.UID != "04AC7E5500060F" {
		t.Fatalf("Parse read channel %s, UID %s; want qr for 04AC7E5500060F", p.Channel, p.UID)
	}
}

// TestPlaqueQRURL_NamesTheNDEFTemplatesHost: for the same base, the QR URL is the
// chip's template URL with the UID mirrored in and the SUN pair cut off — one host,
// one route, one parameter name for both halves of the plaque.
func TestPlaqueQRURL_NamesTheNDEFTemplatesHost(t *testing.T) {
	uid, _ := hex.DecodeString("04DE5F1EACC040")
	for _, base := range []string{"https://time.tappa.mt/t", "https://tap.example.com/t", "https://localhost:8443/t"} {
		ndef, err := BuildTapNDEF(base)
		if err != nil {
			t.Fatalf("%s: BuildTapNDEF: %v", base, err)
		}
		cut := strings.Index(ndef.URL, "&"+paramCtr+"=")
		if cut < 0 {
			t.Fatalf("PREMISE: %s: the template carries no ctr", base)
		}
		want := strings.Replace(ndef.URL[:cut], strings.Repeat("0", uidHexLen), "04DE5F1EACC040", 1)
		got, err := PlaqueQRURL(base, uid)
		if err != nil {
			t.Fatalf("%s: PlaqueQRURL: %v", base, err)
		}
		if got != want {
			t.Errorf("%s: QR URL %s, the template with the UID mirrored is %s", base, got, want)
		}
	}
}

// TestPlaqueQRURL_RefusesWhatThePlaqueCannotCarry: a base BuildTapNDEF refuses is
// refused here too (a plain-http QR would send a tap's session cookie in clear), and
// a UID that is not 7 bytes is refused before anything is written.
func TestPlaqueQRURL_RefusesWhatThePlaqueCannotCarry(t *testing.T) {
	uid, _ := hex.DecodeString("04DE5F1EACC040")
	for _, tc := range []struct {
		name string
		base string
		uid  []byte
	}{
		{"plain http", "http://time.tappa.mt/t", uid},
		{"no host", "https://", uid},
		{"a query", "https://time.tappa.mt/t?src=qr", uid},
		{"a fragment", "https://time.tappa.mt/t#x", uid},
		{"a space", "https://time tappa.mt/t", uid},
		{"too long for the chip", "https://" + strings.Repeat("a", 80) + ".mt/t", uid},
		{"short uid", "https://time.tappa.mt/t", uid[:6]},
		{"long uid", "https://time.tappa.mt/t", append(append([]byte(nil), uid...), 0x01)},
		{"no uid", "https://time.tappa.mt/t", nil},
	} {
		if got, err := PlaqueQRURL(tc.base, tc.uid); err == nil || got != "" {
			t.Errorf("%s: PlaqueQRURL = %q, %v; want a refusal and no URL", tc.name, got, err)
		}
	}
}
