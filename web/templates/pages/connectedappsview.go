package pages

// ConnectedAppsView is the Developers section's "Connected apps" card (issue #37, ADR 0047 §12): the
// apps the business's owners connected through OAuth -- each with its scopes in plain words, when and
// by whom it was connected, its last use -- and the Disconnect that ends one. Owner-only, like the
// rest of the section. Nothing here is a secret: a grant has no value to show.
type ConnectedAppsView struct {
	PanelChrome

	// Available is false when the panel was built without the authorization server.
	Available bool
	// IntegrationsOff is ADR 0047 §11.4's banner: the operator closed this business's integration
	// API, which stops every connected app's requests (403) while the connections stay listed.
	IntegrationsOff bool

	Apps []ConnectedAppRow

	DisconnectHref string
	KeysHref       string

	Done    string
	Problem string
}

// ConnectedAppRow is one connection.
type ConnectedAppRow struct {
	// GrantID is the Disconnect form's value -- looked up in the SESSION's business, never trusted.
	GrantID string
	// AppName is the registry's name ("An app Taptime cannot name right now" when unresolved).
	AppName string
	// AppDisabled: "This app was turned off by Taptime" (ADR 0047 §11.4).
	AppDisabled bool
	// Expired: no live refresh token -- "Expired — reconnect from the app".
	Expired bool
	// Scopes are the scopes in plain words, one per line (oauth.ScopeLabel).
	Scopes []string
	// ConnectedOn and LastUsed are rendered in the business's zone; LastUsed is "Never" without one.
	ConnectedOn string
	LastUsed    string
	// ApprovedBy is the approving membership's name; ApproverDisabled draws the "Approver disabled"
	// pill (the connection is the business's -- ADR 0047 §12, DÜŞÜK-7).
	ApprovedBy       string
	ApproverDisabled bool
}
