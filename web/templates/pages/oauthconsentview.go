package pages

// OAuthConsentState is which of the consent page's four shapes the session sees (ADR 0047 §4.2's
// table, §11.4). Only OAuthConsentAsk draws "Allow"; every state draws the way back to the app (the
// refusal POST: error=access_denied).
type OAuthConsentState int

const (
	// OAuthConsentAsk: an active owner of a business that is open -- the question and two buttons.
	OAuthConsentAsk OAuthConsentState = iota
	// OAuthConsentOwnersOnly: a manager -- "Connecting apps is the owner's".
	OAuthConsentOwnersOnly
	// OAuthConsentSuspended: the business is suspended.
	OAuthConsentSuspended
	// OAuthConsentIntegrationsOff: the operator turned the business's integrations off (§11.4).
	OAuthConsentIntegrationsOff
)

// OAuthConsentView is GET /admin/oauth/authorize and /continue (issue #37, ADR 0047 §4.3).
//
// EVERYTHING ON IT IS EITHER THE SESSION'S OR THE REGISTRY'S: the business's name (the session's
// tenant), the app's name and the host it returns to (the operator's registration -- the consent's
// trust comes from these two, O-17: no logo in this delivery), the scopes in words (a closed map). The
// request's state, challenge and URI never reach the page; the forms carry Digest (rd) alone.
type OAuthConsentView struct {
	State OAuthConsentState
	// AppName is the registered app's name; ReturnHost the host of the redirect URI the request matched.
	AppName    string
	ReturnHost string
	// BusinessName is the session's business -- the one the consent is given for.
	BusinessName string
	// Scopes are the requested scopes, in their canonical order, each in words.
	Scopes []OAuthScopeLine
	// AllowHref and DenyHref are where the two forms post (the binding is added at render).
	AllowHref string
	DenyHref  string
	// Digest is rd: the digest of the pending request this page was drawn from.
	Digest string
	// Others are the person's other businesses where they are an owner and that are not suspended --
	// each a switch form (POST /admin/switch, its own synchronizer token). Empty: no list.
	Others     []AdminSwitchBusiness
	SwitchHref string
	SwitchCSRF string
	// Script is the arming delay's file.
	Script string
}

// OAuthScopeLine is one requested scope: the wire word (shown small, mono) and its sentence.
type OAuthScopeLine struct {
	Word  string
	Label string
}
