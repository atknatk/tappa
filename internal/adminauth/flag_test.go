package adminauth

import (
	"os"
	"strings"
	"testing"
)

// adrSurfaceRows maps the first cell of each row of ADR 0031 §8's "Bayrağın kapattığı yüzeyler"
// table to its Surface.
var adrSurfaceRows = map[string]Surface{
	"Giriş":                    SurfaceLoginIdentity,
	"`GET/POST /admin/switch`": SurfaceSwitch,
	`Kabukta "Switch business" bağlantısı`:       SurfaceShellSwitchLink,
	"Kabukta işletme adı (§5.3)":                 SurfaceShellBusinessName,
	"Yönetici daveti gönderme ve kabul (M14-15)": SurfaceAdminInvites,
	"Üyelik yönetimi (M14-16)":                   SurfaceMembershipManagement,
	"Kimlik başına sıfırlama talebi (M14-14)":    SurfaceResetPerIdentity,
	"Kayıt doğrulaması (M14-17)":                 SurfaceSignupVerification,
	"Tenant bağlı formlar (M14-10a)":             SurfaceTenantBoundForms,
	"Dual-write, bağlı satır tetikleyicisi":      SurfaceDualWrite,
	`Hesap: "Your businesses", "Verify your e-mail", onay listesi, "Link a business", "Sign out everywhere"`: SurfaceAccountIdentity,
}

// adrCellOpen reads one off / on cell of the ADR's table by its FIRST word: "yok", "404" and "eski"
// (the old path) are closed, "açık", "var" and "kimlik" (the identity path) are open; anything else
// is a cell this test cannot read -- a new wording must be classified here, not guessed.
func adrCellOpen(t *testing.T, cell string) bool {
	t.Helper()
	first := strings.Fields(strings.Trim(strings.TrimSpace(cell), "*"))
	if len(first) == 0 {
		t.Fatalf("an empty cell in ADR 0031 §8's table")
	}
	switch w := strings.Trim(first[0], "*"); w {
	case "yok", "404", "eski":
		return false
	case "açık", "var", "kimlik":
		return true
	default:
		t.Fatalf("ADR 0031 §8's table has a cell this test does not read: %q", cell)
		return false
	}
}

// TestIdentityGate_IsADR0031Section8sTable: the gate's table is the ADR's, row for row -- read from
// the ADR's own markdown, so a row added there (or a cell changed) without the gate following is red,
// and a Surface the ADR does not have is red too. Every row is asked of both gates, off and on.
func TestIdentityGate_IsADR0031Section8sTable(t *testing.T) {
	src, err := os.ReadFile("../../docs/adr/0031-tek-kimlik-cok-isletme.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(src)
	start := strings.Index(doc, "**Bayrağın kapattığı yüzeyler**")
	end := strings.Index(doc, "Kabul (M14-08, M14-19)")
	if start < 0 || end < start {
		t.Fatalf("ADR 0031 §8's table is not where it was (start %d, end %d)", start, end)
	}
	seen := map[Surface]bool{}
	rows := 0
	for _, line := range strings.Split(doc[start:end], "\n") {
		if !strings.HasPrefix(line, "| ") || strings.HasPrefix(line, "| Yüzey") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 3 {
			t.Fatalf("a row of %d cells: %q", len(cells), line)
		}
		rows++
		name := strings.TrimSpace(cells[0])
		s, ok := adrSurfaceRows[name]
		if !ok {
			t.Errorf("ADR 0031 §8 has a row the gate does not: %q", name)
			continue
		}
		seen[s] = true
		off, on := adrCellOpen(t, cells[1]), adrCellOpen(t, cells[2])
		if got := NewIdentityGate(false).Open(s); got != off {
			t.Errorf("%s with the flag off: open=%v, the ADR says %v (%q)", s, got, off, strings.TrimSpace(cells[1]))
		}
		if got := NewIdentityGate(true).Open(s); got != on {
			t.Errorf("%s with the flag on: open=%v, the ADR says %v (%q)", s, got, on, strings.TrimSpace(cells[2]))
		}
	}
	if rows != len(identitySurfaces) || len(seen) != len(identitySurfaces) {
		t.Errorf("the ADR's table has %d rows (%d mapped), the gate %d", rows, len(seen), len(identitySurfaces))
	}
	for s := range identitySurfaces {
		if !seen[s] {
			t.Errorf("the gate has a row the ADR does not: %s", s)
		}
	}
}

// TestIdentityGate_FailsClosed: a Surface no row names is closed in both states, and the zero gate is
// the flag off.
func TestIdentityGate_FailsClosed(t *testing.T) {
	for _, g := range []IdentityGate{NewIdentityGate(false), NewIdentityGate(true), {}} {
		if g.Open(Surface("no-such-surface")) || g.Open("") {
			t.Errorf("gate %+v opens a surface its table does not name", g)
		}
	}
	var zero IdentityGate
	if zero.On() || zero.Open(SurfaceSwitch) || !zero.Open(SurfaceDualWrite) {
		t.Error("the zero gate is not the flag off")
	}
}
