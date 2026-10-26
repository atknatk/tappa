package pages

// AdminSwitchView is GET /admin/switch (M14-10, ADR 0031 §5.4): the businesses of the person
// behind the session, each but the current one with its own switch form.
//
// IT IS THE ROUTE'S MINIMUM, NOT THE PAGE. The card that designs this page is M14-13 (the
// Choose screen's docket layout, the suspension mark, the copy); this view carries what the
// switch needs to be pressed at all: the session's synchronizer token and, per business, the
// target and the words a screen reader tells the buttons apart by.
//
// The token is a per-session credential: it is written into the form and nowhere else.
type AdminSwitchView struct {
	CSRFToken string
	// Current is the business the session is in now ("" when the list could not say).
	Current string
	// Others are the person's other businesses, each a switch target.
	Others []AdminSwitchBusiness
	// SwitchHref is where each form posts (the handler's route; the binding is added at render).
	SwitchHref string
	// BackHref is the way back into the panel.
	BackHref string
}

// AdminSwitchBusiness is one switch target. The tenant id is a hidden field the server does not
// believe: identity_switch_session re-proves the membership under the person's lock.
type AdminSwitchBusiness struct {
	TenantID   string
	TenantName string
	Role       string
}
