package operator_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/handler/operator"
)

// bidiControls are the nine runes shownName drops, and pdiRLO the shape that walks out of a bdi
// (T97): U+2069 closes the isolate the element opened, U+202E then overrides what follows.
const (
	bidiControls = "\u202a\u202b\u202c\u202d\u202e\u2066\u2067\u2068\u2069"
	pdiRLO       = "\u2069\u202e"
)

func carriesBidiControl(s string) bool { return strings.ContainsAny(s, bidiControls) }

// TestOperatorScreens_ABidiControlInANameDoesNotReachThePage (T97) drives each screen that prints
// a name with one carrying PDI+RLO: the name reaches the page in its bdi (the assertion is not
// passing on a name that never rendered) with no control left in the whole body.
//
// PART I -- the screens below: the tenant list; a tenant's overview, plaques (banner and location),
// billing and VAT; the audit log's actor, account and tenant; the holdings' holding and member; the
// legal versions' publisher. PART II -- this list; a new screen printing a name is not caught by it.
func TestOperatorScreens_ABidiControlInANameDoesNotReachThePage(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	name := func(who string) string { return who + pdiRLO + " override" }
	shown := func(who string) string { return "<bdi>" + who + " override</bdi>" }
	at := time.Date(2026, 10, 7, 9, 30, 0, 0, time.UTC)

	overview := g.seedOverview(db.TenantOverview{Name: name("FAKE overview"), Plan: "founding"})
	g.store.mu.Lock()
	g.store.tenants = []db.TenantSummary{{ID: uuid.New(), Name: name("FAKE listed"), Plan: "founding"}}
	g.store.mu.Unlock()
	loc, locName := uuid.New(), name("FAKE location")
	plaques := g.seedPlaques(name("FAKE plaques"), plaqueAt("04A1B2C3D4E5F6", "active", &loc, &locName, &at))
	billing := g.seedBilling(name("FAKE billing"), billingBeforeSignupMonths(2026, time.September, 12)...)
	vat := g.seedVAT(uuid.Nil, name("FAKE vat"), vatNumberOK, nil, nil)
	member := g.seedHoldingTenant(uuid.Nil, name("FAKE member"), "Europe/Malta", nil)
	g.seedHolding(uuid.Nil, name("FAKE holding"), at, liveMembership(member, at))

	s, a, b, tn := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	actor, account, target := name("FAKE actor"), name("FAKE account"), name("FAKE target")
	e1 := auditEntry("read", 2, &s, &a, &actor)
	e1.Scope, e1.TargetTenantID, e1.TargetTenantName = ptr("tenant_detail"), &tn, &target
	e2 := auditEntry("login_failed", 1, nil, nil, nil)
	e2.TargetAdminID, e2.TargetAdminName = &b, &account
	g.seedAudit(e1, e2)

	publisher, admin := name("FAKE publisher"), uuid.New()
	g.store.mu.Lock()
	g.store.versions = []db.LegalVersion{{ID: uuid.New(), Slug: "privacy", PublishedAt: at, BodyBytes: 22,
		PublishedBy: db.LegalPublishedByOperator, PublisherID: &admin, PublisherName: &publisher, Current: true}}
	g.store.mu.Unlock()

	for _, tc := range []struct {
		screen, path string
		want         []string
	}{
		{"the tenant list", "/operator/tenants", []string{shown("FAKE listed")}},
		{"the overview", "/operator/tenants/" + overview.String(), []string{shown("FAKE overview")}},
		{"the plaques", plaquePath(plaques.String()), []string{shown("FAKE plaques"), shown("FAKE location")}},
		{"the billing", "/operator/tenants/" + billing.String() + "/billing", []string{shown("FAKE billing")}},
		{"the VAT number", vatPathOf(vat.String()), []string{shown("FAKE vat")}},
		{"the audit log", "/operator/audit", []string{shown("FAKE actor"), shown("FAKE account"), shown("FAKE target")}},
		{"the holdings", "/operator/holdings", []string{shown("FAKE holding"), shown("FAKE member")}},
		{"the legal versions", "/operator/legal", []string{"FAKE publisher override"}},
	} {
		body := g.get(tc.path, c).Body.String()
		for _, want := range tc.want {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not carry %q", tc.screen, want)
			}
		}
		if carriesBidiControl(body) {
			t.Errorf("%s carries a bidirectional control", tc.screen)
		}
	}
}

// TestOperatorScreens_ANameOfOnlyBidiControlsNamesTheRowByItsID: a name of controls and nothing else
// is the "Unnamed" row with its id, never an empty bdi. Not new behaviour (visibleText counts no
// format character, so the rule answers the same before and after the drop): it pins that the drop
// did not change it.
func TestOperatorScreens_ANameOfOnlyBidiControlsNamesTheRowByItsID(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	id := g.seedOverview(db.TenantOverview{Name: bidiControls, Plan: "founding"})
	body := g.get("/operator/tenants/"+id.String(), c).Body.String()
	if !strings.Contains(body, "Unnamed tenant") || !strings.Contains(body, id.String()) {
		t.Error("a name of controls alone is not the unnamed tenant, by its id")
	}
	if carriesBidiControl(body) {
		t.Error("the page carries a bidirectional control")
	}
}

// TestOperatorScreens_RightToLeftNamesAreLeftAsTyped: the drop is the controls and nothing else --
// an Arabic, a Hebrew and a Persian name (ZWNJ inside), one with the implicit marks, and an emoji
// sequence (ZWJ inside) come back byte for byte.
func TestOperatorScreens_RightToLeftNamesAreLeftAsTyped(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	for _, name := range []string{
		"مطعم الياسمين", "מסעדת השלום", "رستوران می\u200cخواهم", "\u200fמסעדה\u200e 7", "Chef \U0001F468\u200d\U0001F373 Bar",
	} {
		g.store.mu.Lock()
		g.store.tenants = []db.TenantSummary{{ID: uuid.New(), Name: name, Plan: "founding"}}
		g.store.mu.Unlock()
		if body := g.get("/operator/tenants", c).Body.String(); !strings.Contains(body, "<bdi>"+name+"</bdi>") {
			t.Errorf("the list does not carry %q as typed", name)
		}
	}
}

// TestShownName_TheSetIsTheDomains: shownName's set and the write rule's are one set over every
// code point, so what the screens drop is what the account screen and sign-up refuse.
// CONTROL: nine runes are in it.
func TestShownName_TheSetIsTheDomains(t *testing.T) {
	in := 0
	for r := rune(0); r <= 0x10FFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		a, b := operator.IsBidiControlForTest(r), tenant.NameHasBidiControl(string(r))
		if a != b {
			t.Errorf("U+%04X: the screens say %v, the writes %v", r, a, b)
		}
		if a {
			in++
		}
	}
	if in != len([]rune(bidiControls)) {
		t.Errorf("%d code points in the set, want %d", in, len([]rune(bidiControls)))
	}
}
