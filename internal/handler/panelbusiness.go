package handler

import (
	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/pages"
)

// panelbusiness.go -- the business's name and the "Switch business" link in the panel shell (M14-12;
// ADR 0031 §5.3, §8, K-5; pages/panelbusinessview.go draws them).
//
// 🔴 NO READ OF ITS OWN. The name is adminauth.Resolved.TenantName: tenants.name as the statement
// that proved the session (TouchAdminSession) returned it. Nothing here touches the database, so
// every section names its business at the cost it already paid (ADR 0031 §5.3: the shell's name
// costs no extra read).
//
// 🔴 EACH PART IS ITS ROW OF THE FLAG TABLE (adminauth/flag.go), never the flag itself:
// SurfaceShellBusinessName is open in both states -- every session shows its business, linked or
// not, so the name tells nobody whether the row is linked (R4) -- and SurfaceShellSwitchLink is
// open only with TAPPA_ADMIN_IDENTITY on. With the flag off no section carries the link or the
// switch page's path.
//
// THE LINK IS SHOWN FOR ONE BUSINESS AS FOR MANY (M14-12's measured choice; panelbusinessview.go
// gives the measurement): a per-session membership count has nowhere to live without a schema
// change and would go stale; the switch page itself says when there is one business.
//
// The name is display data for the person's own page: it is never logged here.
func (a *AdminAuth) panelBusiness(id httpx.AdminIdentity) pages.PanelBusiness {
	var b pages.PanelBusiness
	if a.identityGate.Open(adminauth.SurfaceShellBusinessName) {
		b.Name = businessName(id.Admin.TenantName)
	}
	if a.identityGate.Open(adminauth.SurfaceShellSwitchLink) {
		b.SwitchHref = adminSwitchHref
	}
	return b
}
