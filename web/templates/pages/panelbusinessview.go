package pages

// The business a panel session is in, named in the shell, and the way to another one (M14-12;
// ADR 0031 §5.3, §8, K-5).
//
// A person may hold several businesses under one sign-in (ADR 0031), so every section says WHICH
// business it is showing: the shell carries the business's name on every section of every
// business -- branded or not -- and, while TAPPA_ADMIN_IDENTITY is on, a "Switch business" link to
// /admin/switch, where the person's businesses are listed (M14-13 designs that page).
//
// WHERE THE NAME COMES FROM. internal/handler fills Name from adminauth.Resolved.TenantName -- the
// tenants.name TouchAdminSession already returns with the session (ADR 0031 §5.3) -- so naming the
// business costs no read. A branded business's header already prints its name (panelBrandHeader,
// from the brand read the chrome makes anyway); there the block below draws the link alone, so the
// name is never printed twice.
//
// THE LINK IS ALWAYS THERE WHILE THE FLAG IS ON -- for one business or many, linked row or not.
// The alternative, a membership count stamped when the session opens or switches, has nowhere to
// live without a schema change (admin_sessions has no such column and the application may insert
// only id, tenant_id, admin_user_id and token_hash into it; identity_open_session and
// identity_switch_session return no count -- 00071), and a stamp goes stale the moment an
// invitation is accepted or a membership removed in another business. The switch page says when
// there is only one business.
//
// THE ZERO VALUE DRAWS NOTHING, and the shell is then byte for byte what it was before M14-12
// (TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8's corpus renders it).

// PanelBusiness is the shell's business block.
type PanelBusiness struct {
	// Name is the business's name ("" draws none). internal/handler sets it for every panel
	// request (adminauth.SurfaceShellBusinessName is open whatever the flag says), with
	// businessName's fallback for an empty tenants.name.
	Name string
	// SwitchHref is the switch page's path; "" draws no link. internal/handler sets it only
	// while adminauth.SurfaceShellSwitchLink is open, i.e. while TAPPA_ADMIN_IDENTITY is on.
	SwitchHref string
}

// businessName is the name the business block prints: none on a branded business, whose header
// prints it already.
func (c PanelChrome) businessName() string {
	if c.Brand.Branded() {
		return ""
	}
	return c.Business.Name
}

// showsBusiness reports whether the shell draws the business block at all.
func (c PanelChrome) showsBusiness() bool {
	return c.businessName() != "" || c.Business.SwitchHref != ""
}
