package adminauth

// flag.go -- THE SURFACES TAPPA_ADMIN_IDENTITY CLOSES (ADR 0031 §8's table, R4), in ONE place.
//
// WHY A TABLE AND NOT A BOOLEAN AT EVERY CALL SITE. ADR 0031 §8 decides surface by surface what the
// flag closes, and the rule behind every row is one (R4, option (a)): while the flag is off, a
// session linked to an identity and an unlinked one must not be told apart by anything they can see
// -- the shell, the account page and /admin/switch answer byte for byte the same. A scattered
// `if cfg.AdminIdentity` would make each later card (M14-09 … M14-18) re-decide its row, and one
// that decided wrong would open an enumeration oracle. Here the decision is DATA: a card names its
// Surface and asks IdentityGate.Open; the table below is the ADR's, row for row, and a test holds
// it to the ADR's text (TestIdentityGate_IsADR0031Section8sTable).
//
// FAIL CLOSED. A Surface the table does not know is closed in both states; the zero IdentityGate is
// the flag off.
//
// THE WRITE PATHS ARE ROWS TOO, open in both states, so that "the flag does not gate the
// dual-write" is written down where the gates are, not only remembered: a password change, a legacy
// reset link and a registration write the identity whatever the flag says (ADR 0031 §8;
// changepassword.go, reset.go, internal/domain/signup).

// Surface is one row of ADR 0031 §8's table.
type Surface string

// The rows, in the ADR's order. Each names the card that builds it.
const (
	// SurfaceLoginIdentity is the identity path of the sign-in (M14-09): closed = today's sign-in
	// for everyone.
	SurfaceLoginIdentity Surface = "login-identity-path"
	// SurfaceSwitch is GET/POST /admin/switch (M14-10): closed = 404, the same bytes as a path the
	// panel never had.
	SurfaceSwitch Surface = "admin-switch"
	// SurfaceShellSwitchLink is the shell's "Switch business" link (M14-12).
	SurfaceShellSwitchLink Surface = "shell-switch-link"
	// SurfaceShellBusinessName is the business's name in the shell (M14-12, §5.3): open in both
	// states -- every session shows it, linked or not.
	SurfaceShellBusinessName Surface = "shell-business-name"
	// SurfaceAccountIdentity is the account page's identity sections -- "Your businesses", "Verify your
	// e-mail", the confirmation list, "Sign out everywhere" (M14-11, M14-18) and, since M14-22, the
	// panel's "Another business uses your email" banner and its link by e-mail: closed = the page as it is
	// today.
	SurfaceAccountIdentity Surface = "account-identity-sections"
	// SurfaceAdminInvites is sending and accepting an administrator invitation (M14-15): closed = 404.
	SurfaceAdminInvites Surface = "admin-invites"
	// SurfaceMembershipManagement is managing a business's memberships (M14-16).
	SurfaceMembershipManagement Surface = "membership-management"
	// SurfaceResetPerIdentity is the per-identity reset request (M14-14): closed = today's per-row request.
	SurfaceResetPerIdentity Surface = "identity-reset-request"
	// SurfaceSignupVerification is the registration's e-mail verification (M14-17): open in both
	// states -- the same "Check your inbox" for a new and a known address.
	SurfaceSignupVerification Surface = "signup-verification"
	// SurfaceTenantBoundForms is the business binding of every panel form (M14-10a): open in both
	// states (a one-business session cannot mismatch).
	SurfaceTenantBoundForms Surface = "tenant-bound-forms"
	// SurfaceDualWrite is the dual-write and the linked-row trigger (M14-08, 00069): open in both
	// states.
	SurfaceDualWrite Surface = "dual-write"
)

// identitySurfaces is ADR 0031 §8's table: per Surface, whether it is open with the flag off and on.
var identitySurfaces = map[Surface]struct{ off, on bool }{
	SurfaceLoginIdentity:        {off: false, on: true},
	SurfaceSwitch:               {off: false, on: true},
	SurfaceShellSwitchLink:      {off: false, on: true},
	SurfaceShellBusinessName:    {off: true, on: true},
	SurfaceAccountIdentity:      {off: false, on: true},
	SurfaceAdminInvites:         {off: false, on: true},
	SurfaceMembershipManagement: {off: false, on: true},
	SurfaceResetPerIdentity:     {off: false, on: true},
	SurfaceSignupVerification:   {off: true, on: true},
	SurfaceTenantBoundForms:     {off: true, on: true},
	SurfaceDualWrite:            {off: true, on: true},
}

// IdentityGate answers, for one process, whether a surface of ADR 0031 §8's table is open. Built
// once from config.Config.AdminIdentity; the zero value is the flag off.
type IdentityGate struct{ on bool }

// NewIdentityGate is the gate for a flag value (config.Config.AdminIdentity).
func NewIdentityGate(on bool) IdentityGate { return IdentityGate{on: on} }

// On reports the flag itself -- for a start-up line or a test, never to decide a surface: that is
// Open's, by its row.
func (g IdentityGate) On() bool { return g.on }

// Open reports whether s is open under this gate's flag. A Surface the table does not name is
// closed (fail closed).
func (g IdentityGate) Open(s Surface) bool {
	row, ok := identitySurfaces[s]
	if !ok {
		return false
	}
	if g.on {
		return row.on
	}
	return row.off
}
