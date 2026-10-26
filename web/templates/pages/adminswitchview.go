package pages

// AdminSwitchView is GET /admin/switch (ADR 0031 §5.4; M14-10 the route, M14-13 the page): the
// businesses of the person behind the session, each but the current one with its own switch form.
//
// EVERYTHING HERE IS THE PERSON'S OWN. The list comes from identity_memberships keyed by the
// session's token (adminauth.Manager.Memberships): the businesses this person is an active member
// of, nothing about anyone else's. The page prints names, roles and the suspension mark (K-10) of
// those, and no count, no other person and no business the person is not in.
//
// The token is a per-session credential: it is written into the forms and nowhere else.
type AdminSwitchView struct {
	CSRFToken string
	// Current is the business the session is in now ("" when the list could not say).
	Current string
	// CurrentSuspended marks the current business as suspended (K-10).
	CurrentSuspended bool
	// Others are the person's other businesses, each a switch target. Empty: the person has one
	// business on this sign-in, and the page says so rather than drawing an empty rail.
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
	// Suspended marks the docket (ADR 0031 K-10): switching in is allowed, writing there is not.
	Suspended bool
}
