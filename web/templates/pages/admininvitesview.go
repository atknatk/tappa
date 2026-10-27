package pages

// The MANAGER INVITATION screens' view models (M14-15, ADR 0031 §5.11).
//
// 🔴 NO VIEW HERE HAS A FIELD AN INVITATION LINK OR ITS TOKEN COULD TRAVEL IN on the OWNER's side:
// the link goes to the invitee's own mailbox and is never shown on the panel (M7-07's second
// criterion). On the invitee's page the token travels in an HttpOnly cookie, never in a view.
//
// 🔴 NOTHING ON THE OWNER'S SCREENS SAYS WHETHER AN ADDRESS HAS A TAPTIME SIGN-IN (R18): a row's state
// is open, accepted, cancelled or expired -- the same four for every address -- and no field could carry
// the difference.

// ManagersView is the owner's "Managers" page: the invitations of this business and the form to send
// one.
type ManagersView struct {
	PanelChrome
	// CanSend is false when this deployment mails no invitations (TAPPA_INVITE_DELIVERY is not email):
	// the page says so and draws no form -- a fact about the server, never about an address.
	CanSend bool
	// ListFailed: the invitations could not be read on this response.
	ListFailed bool
	Invites    []ManagerInviteRow
	InviteHref string
	CancelHref string
	// NameMaxLength is the name box's ceiling (adminauth.MaxInviteNameRunes).
	NameMaxLength int
	// Done and Problem are the outcome of the last press (a word in the redirect, a sentence here).
	Done, Problem string
}

// ManagerInviteRow is one invitation on the list.
type ManagerInviteRow struct {
	ID    string
	Email string
	Name  string
	// State is the word printed on the row: Open, Accepted, Cancelled, Expired.
	State string
	// Open draws the cancel button.
	Open   bool
	SentOn string
	EndsOn string
}

// InviteAcceptView is the page an invitation link opens.
type InviteAcceptView struct {
	// CSRFToken is the synchronizer value the two forms echo (not a §4.7 secret).
	CSRFToken string
	// Dead: the link cannot be used (unknown, spent, cancelled, expired) -- one screen for all.
	Dead bool
	// SignInHref is the sign-in page. (Both forms post to "/admin/invite", written in the template the
	// way the recovery forms write theirs -- internal/handler's adminInvitePath; a handler test holds
	// the two together.)
	SignInHref string
	// MinPasswordChars is the panel's password floor (internal/domain/signup's).
	MinPasswordChars int
	// Error is a sentence for the form that was just refused; ErrorFor says which ("session" or
	// "new").
	Error, ErrorFor string
}

// InviteAcceptedView is the answer to an accepted invitation. It names no business and no person.
type InviteAcceptedView struct {
	SignInHref string
	// WithSession: the person accepted with a session they hold in another business; they can switch.
	WithSession bool
	SwitchHref  string
}
