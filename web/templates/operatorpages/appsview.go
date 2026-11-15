package operatorpages

// AppsView is /operator/apps (issue #37, ADR 0047 §10.3): the app registry, newest first, the
// registration form, and -- on the answer to a register or a rotate -- the one-time secret.
type AppsView struct {
	Apps []AppRow
	// Scopes are the registration form's checkboxes: the closed scope set (webhooks:manage is not in
	// it -- ADR 0047 §13).
	Scopes []AppScopeOption
	// Notice is the sentence above the list ("" Text: none).
	Notice AppNotice
	// Secret is shown on the register or rotate answer and nowhere else; nil on every other render.
	Secret *AppSecretView
}

// AppRow is one registered app as op_read_oauth_clients returns it (the secret's hash never; its hint).
type AppRow struct {
	ID, ClientID, Name string
	RedirectURIs       []string
	Scopes             []string
	SecretHint         string
	CreatedAt          string
	Disabled           bool
	DisabledAt         string
	// The three forms' actions.
	RotatePath, RedirectsPath, DisablePath string
}

// AppScopeOption is one checkbox of the registration form.
type AppScopeOption struct {
	Word, Label string
}

// AppNotice is the list's sentence: Alert for a refusal or an unconfirmed write.
type AppNotice struct {
	Alert bool
	Text  string
}

// AppSecretView is the one-time display of a client's credentials.
type AppSecretView struct {
	Heading  string
	ClientID string
	// Secret is the raw client secret -- in this response's body and nowhere else, ever.
	Secret string
	// ExpectHint is the handler's check that a rotation took (never rendered).
	ExpectHint string
}
