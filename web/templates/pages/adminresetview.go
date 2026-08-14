package pages

// The PANEL RECOVERY screens' view models (M7-04 phase B).
//
// SAME RULE AS THE REST OF THIS PACKAGE, and on this flow it carries the most
// weight it ever has: NO VIEW BELOW HAS A FIELD A RESET LINK OR ITS TOKEN COULD
// TRAVEL IN. The token moves between the request that opens the link and the
// request that spends it in an HttpOnly cookie (internal/handler/adminreset.go
// gives the four reasons), so nothing about it needs to reach a template — and
// because there is no field, no later edit can put it in a page by accident. That
// is the same argument ActivateView makes, with one difference worth naming:
// ConfirmView.Code is that flow's ONE declared exception, and this flow has NONE.
//
// ⚠️ AND NO VIEW BELOW CARRIES THE ADDRESS EITHER. A recovery form that echoed the
// address back would be reflecting an unauthenticated stranger's input on the one
// screen whose entire job is to say nothing about that address — and it would put
// somebody's email into a page that a shared browser keeps in its back/forward
// cache. AdminLoginView already refuses to echo the address for the same two
// reasons; this is the same decision on the same surface.

// AdminResetRequestView is the "I cannot get in" form: one address, one button.
type AdminResetRequestView struct {
	// CSRFToken is the synchronizer token echoed by the form. It is rendered ON
	// PURPOSE and is not a §4.7 secret: it exists precisely so that a page an
	// attacker cannot read is the only page that can produce a valid submission.
	CSRFToken string

	// CanDeliver is false when this deployment has no way to send a recovery link
	// (config.ResetDelivery, open question Q02).
	//
	// 🔴 IT IS A FACT ABOUT THE SERVER AND NEVER ABOUT THE ADDRESS, which is what
	// makes it safe to render. The form has not been submitted yet and no address
	// has been typed, so a visitor reading "this deployment cannot send the link"
	// learns nothing whatsoever about any account — and they learn it BEFORE
	// spending their time, which is the §4.6 shape (the alternative is a form that
	// accepts an address and quietly does nothing).
	CanDeliver bool

	// SignInHref points back at the sign-in form. It is passed in rather than
	// written here so the route and the link are the same string at compile time
	// (internal/handler's adminLoginPath), which is why that constant exists.
	SignInHref string
}

// AdminResetSentView is what EVERY submitted address gets.
//
// 🔴 ONE FIELD DECIDES THE WORDING AND IT IS NOT ABOUT THE ADDRESS. There is
// deliberately no Found, no Sent, no AddressKnown and no Count: the fifth M7-04
// acceptance criterion requires a registered and an unregistered address to receive
// the same status, the same body and the same wording, and a view model with a
// field for the difference is how they stop being the same. CanDeliver varies with
// the SERVER's configuration, which is identical for every address in a given
// deployment — so the two arms of this type are two deployments, never two
// visitors.
type AdminResetSentView struct {
	CanDeliver bool
	SignInHref string
}

// title is the document title, and it VARIES WITH THE PAGE — which is a fix rather
// than a refinement.
//
// 🔴 THE TITLE WAS A CONSTANT OUTSIDE THE BRANCH AND IT PROMISED THE ONE THING THE
// PAGE EXISTS TO REFUSE TO PROMISE. Measured on the running product with no delivery
// channel: <title>Check your email — Tappa</title> and <h1>Nothing was sent</h1> in
// the SAME document. A title is not decoration — it is what the browser tab, the
// history entry, the bookmark and a shared link all show, and every one of those was
// telling somebody to go and look for mail that was never sent. The template two
// lines above says "THE PAGE PROMISES NOTHING IT CANNOT DELIVER"; the title was
// outside the sentence's reach.
//
// IT IS A METHOD RATHER THAN A SECOND TEMPLATE BRANCH so the body stays one block:
// duplicating the whole page to vary six words is how two pages drift apart.
func (v AdminResetSentView) title() string {
	if v.CanDeliver {
		return "Check your email — Tappa"
	}
	return "Nothing was sent — Tappa"
}

// AdminResetNewView is the form a recovery link opens: two password boxes.
//
// NO ADDRESS, NO NAME, NO BUSINESS. Naming whose account this link belongs to would
// turn possession of a link into a disclosure about an account — and, worse, it
// would need a database lookup on a page anybody can open, which is the "does this
// link exist" oracle internal/adminauth/reset.go keeps the digest computation ahead
// of the lookup to avoid.
type AdminResetNewView struct {
	CSRFToken string
	// MinPasswordChars is the floor the form states up front, passed in from
	// internal/domain/signup rather than written here: the rule belongs to the
	// package that decides what a Tappa password may be, and a literal in a template
	// is a second representation of it.
	MinPasswordChars int
	// Error is the one sentence a refused submission shows. It says what to do next
	// and it describes only what the VISITOR typed — never anything about the link,
	// the account or the server.
	Error string
}
