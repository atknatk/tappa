package pages

// APIKeysView is the API keys section (M11 API-3, ADR 0028 §3): the owner's integration keys --
// issue one (shown once), list them, revoke one or all.
//
// 🔴 §4.7 -- ONE FIELD OF THIS VIEW EVER HOLDS A SECRET, AND ONLY ON ONE RESPONSE. NewKey is the
// raw key, filled by the handler on the response to a successful "create" and on no other: the
// list carries hints, never keys, and nothing reads a key back from the database (only its keyed
// hash is stored). The response that carries it is Cache-Control: no-store like every panel page.
type APIKeysView struct {
	PanelChrome

	// Available is false when this deployment was built without the keys service: the section
	// says so and offers nothing.
	Available bool
	// APIEnabled is K-22's gate: the operator has opened the integration API for this business.
	// Closed, no key can be created, and the existing keys are refused with 403.
	APIEnabled bool
	// ListFailed: the keys could not be read on this response (only the creation answer renders
	// with it -- the key it carries is shown anyway). Neither the gate's note nor the list nor
	// the form is drawn, since none of them would be true.
	ListFailed bool

	// NewKey is the raw key on the creation response, "" everywhere else.
	NewKey     string
	NewKeyName string

	Keys []APIKeyRow

	// BudgetAlerts is the owner's alert (ADR 0028 §12, K-25): keys that reached their hourly
	// deactivation budget in the last day, newest first.
	BudgetAlerts []APIKeyBudgetAlert

	// The create form.
	PostHref      string
	NameValue     string
	NameMaxLength int
	NameError     string

	RevokeHref    string
	RevokeAllHref string

	Done    string
	Problem string
}

// APIKeyBudgetAlert is one key that reached its deactivation budget: its name ("an unnamed key"
// when the list no longer has it) and when, in the business's time zone.
type APIKeyBudgetAlert struct {
	KeyName string
	At      string
	// Budget is the hourly limit the alert recorded; 0 when the row named none.
	Budget int
}

// APIKeyRow is one key in the list. The hint is the prefix and four characters -- not the key.
type APIKeyRow struct {
	ID         string
	Name       string
	PrefixHint string
	CreatedOn  string
	LastUsed   string
	RevokedOn  string
}

// Live reports whether the key still opens the API (subject to the business's gate).
func (r APIKeyRow) Live() bool { return r.RevokedOn == "" }

// AnyLive reports whether "revoke all" has anything to do.
func (v APIKeysView) AnyLive() bool {
	for _, k := range v.Keys {
		if k.Live() {
			return true
		}
	}
	return false
}
