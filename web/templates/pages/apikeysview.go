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

	// Webhook is the business's webhook endpoint (M11 API-9), drawn below the keys.
	Webhook WebhookView
}

// WebhookView is the webhook part of the API keys section (M11 API-9, ADR 0028 §6): the business's
// one endpoint -- its address, its switch, its key's age, the test ping and its bucket.
//
// 🔴 ONE FIELD HOLDS A SECRET, ON TWO RESPONSES ONLY: NewKey is the endpoint's signing key, filled on
// the answer to "create" and to "rotate the key", and on no other (the key is stored sealed and is
// never read back for display -- K-12: there is no "show"). Address is the owner's own address,
// printed to the owner on their own page and logged nowhere.
type WebhookView struct {
	// Configured is false when this deployment has no webhook KEK (dev only): the part says so and
	// offers nothing.
	Configured bool
	// APIEnabled is K-22's gate. Closed, nothing but switching off is offered.
	APIEnabled bool

	Exists         bool
	Address        string
	AddressRefused bool
	Active         bool
	CreatedOn      string
	RotatedOn      string

	// NewKey is the signing key on the create and rotate responses, "" everywhere else.
	NewKey string

	// The bucket the ping shares with API-10's "resend": spent of Limit this hour, and when the
	// next slot frees once it is full.
	Spent    int
	Limit    int
	NextSlot string

	// The last ping's result, from the redirect after it.
	PingShown     bool
	PingDelivered bool
	PingSentence  string

	// The address form (create and change).
	AddressValue     string
	AddressError     string
	AddressMaxLength int

	CreateHref  string
	AddressHref string
	RotateHref  string
	DisableHref string
	EnableHref  string
	PingHref    string
	ResendHref  string

	// The delivery log (M11 API-10): one page, newest first, and the links to the older page and
	// back to the newest ("" when there is none).
	Deliveries           []WebhookDeliveryRow
	DeliveriesOlderHref  string
	DeliveriesNewestHref string
	// LogKeepsDays is how long the log keeps a delivery after it arrived or failed (T130,
	// delivery.RetentionDays); 0 says nothing.
	LogKeepsDays int
}

// WebhookDeliveryRow is one delivery in the log. It names the punch and the outcome -- never the
// address, never the receiver's text (neither is stored).
type WebhookDeliveryRow struct {
	// ID is the delivery's id, the resend form's value.
	ID         string
	PunchID    string
	Queued     string
	State      string
	Attempts   int
	LastAnswer string
	NextTry    string
	Delivered  string
	Resendable bool
}

// CanResend reports whether the resend button is offered at all: the API open, the endpoint on, a
// slot of the bucket free.
func (v WebhookView) CanResend() bool { return v.APIEnabled && v.Active && !v.BucketFull() }

// BucketFull reports whether the hour's test messages are spent.
func (v WebhookView) BucketFull() bool { return v.Limit > 0 && v.Spent >= v.Limit }

// SlotsLeft is how many of the hour's test-message-or-resend slots are still free: Limit less
// Spent, never below zero (a full bucket reads "0 of 10 left", not a negative count).
func (v WebhookView) SlotsLeft() int { return max(v.Limit-v.Spent, 0) }

// ChangeValue is what the change-address field holds: the address the owner typed when the rule
// refused it, else the saved address (the owner's own, on the owner's own page -- "" when the
// saved one no longer passes the rule, which the page says in a sentence instead).
func (v WebhookView) ChangeValue() string {
	if v.AddressValue != "" {
		return v.AddressValue
	}
	return v.Address
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
