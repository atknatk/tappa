package operator_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// bannerOf is the tenant banner's section of a page, or "".
func bannerOf(body string) string {
	i := strings.Index(body, `<section class="op-tenant" aria-label="Tenant">`)
	if i < 0 {
		return ""
	}
	j := strings.Index(body[i:], "</section>")
	if j < 0 {
		return ""
	}
	return body[i : i+j]
}

// TestTenantBanner_TwoTenantsTheScreenNamesAlikeAreToldApartByTheirID (T216): tenants.name is not
// unique, and shownName makes "Rusty" RLO "Bar" print as "RustyBar". Two tenants -- that one and one
// stored as "RustyBar" -- get the same name in their banners and, under it, mono, each its own id's
// first eight digits; on all four of a tenant's screens. CONTROL: the two prefixes differ (the ids are
// chosen so), and the name the two banners print is the same.
func TestTenantBanner_TwoTenantsTheScreenNamesAlikeAreToldApartByTheirID(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	override := g.seedOverview(db.TenantOverview{Name: "Rusty" + string(rune(0x202e)) + "Bar", Plan: "founding"})
	plain := g.seedOverview(db.TenantOverview{Name: "RustyBar", Plan: "founding"})
	if override.String()[:8] == plain.String()[:8] {
		t.Skip("PREMISE: two random ids share a prefix (1 in 2^32); run again")
	}
	for _, id := range []uuid.UUID{override, plain} {
		banner := bannerOf(g.get("/operator/tenants/"+id.String(), c).Body.String())
		if !strings.Contains(banner, "<bdi>RustyBar</bdi>") {
			t.Errorf("%s: the banner does not print the name as the screens show it:\n%s", id, banner)
		}
		if want := `<p class="font-mono text-sm">ID ` + id.String()[:8] + `</p>`; !strings.Contains(banner, want) {
			t.Errorf("%s: the banner does not carry %q:\n%s", id, want, banner)
		}
		other := plain
		if id == plain {
			other = override
		}
		if strings.Contains(banner, other.String()[:8]) {
			t.Errorf("%s: the banner carries the other tenant's prefix", id)
		}
	}
	plaques := g.seedPlaques("Rusty" + string(rune(0x202e)) + "Bar")
	billing := g.seedBilling("RustyBar", billingBeforeSignupMonths(2026, 9, 12)...)
	vat := g.seedVAT(uuid.Nil, "RustyBar", vatNumberOK, nil, nil)
	for path, id := range map[string]uuid.UUID{
		plaquePath(plaques.String()):                         plaques,
		"/operator/tenants/" + billing.String() + "/billing": billing,
		vatPathOf(vat.String()):                              vat,
	} {
		banner := bannerOf(g.get(path, c).Body.String())
		if want := `<p class="font-mono text-sm">ID ` + id.String()[:8] + `</p>`; !strings.Contains(banner, want) {
			t.Errorf("%s: the banner does not carry %q:\n%s", path, want, banner)
		}
	}
}

// TestNewTenantName_RefusesAnIDThatIsNotATenants: a named banner needs a canonical uuid, the form
// uuid.String writes -- an empty or short id, an upper-case one, one without hyphens, in braces or
// with a non-hex digit is ErrNoTenantID, so no banner prints a name with nothing beside it. CONTROL:
// a canonical id is accepted.
func TestNewTenantName_RefusesAnIDThatIsNotATenants(t *testing.T) {
	for _, id := range []string{
		"", "10000000", "10000000-0000-4000-8000-00000000000", "10000000-0000-4000-8000-0000000000011",
		"1000000A-0000-4000-8000-000000000001", "10000000000040008000000000000001", "{10000000-0000-4000-8000-000000000001}",
		"1000000g-0000-4000-8000-000000000001", "10000000-0000-4000-8000-00000000000g", "10000000_0000-4000-8000-000000000001",
	} {
		if _, err := operatorpages.NewTenantName(id, "Kebab Factory Ltd"); !errors.Is(err, operatorpages.ErrNoTenantID) {
			t.Errorf("NewTenantName(%q) = %v, want ErrNoTenantID", id, err)
		}
	}
	if _, err := operatorpages.NewTenantName(uuid.NewString(), "Kebab Factory Ltd"); err != nil {
		t.Errorf("CONTROL: a canonical id was refused: %v", err)
	}
}
