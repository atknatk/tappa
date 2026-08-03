package pages

// The PANEL screens' view models (M6-01 phase B).
//
// SAME RULE AS THE REST OF THIS PACKAGE, and here it does more work than usual:
// a template cannot accidentally render a password, a digest or a session token,
// because no view below has a field one could travel in. The login form takes an
// email and a password from the visitor and gives NEITHER back — not even the
// email, which is why a failed sign-in clears the box.
//
// ⚠️ THE EMAIL IS DELIBERATELY NOT ECHOED, and it is worth saying why, since
// re-filling the field is the friendlier thing to do. Two reasons, in order of
// weight: a reflected value is a value that has to be escaped correctly forever,
// on the one form in the product that an unauthenticated stranger can post to; and
// an operator who mistyped their address should retype it rather than be shown a
// form that looks right and is not. The cost is one extra field to type after a
// failed attempt, on a screen people see once a day.

// AdminLoginView is the panel sign-in form.
//
// TWO FIELDS, AND THE ABSENCE OF A THIRD IS THE SPEC. There is no Error string,
// no Reason and no ErrorKind: migration 00011's PHASE B OBLIGATION 1 requires an
// unknown email, a wrong password and a disabled account to be indistinguishable,
// and a view model with a message field is how they stop being. Failed is a BOOL —
// it can say THAT something failed and has no way to say WHAT.
type AdminLoginView struct {
	// CSRFToken is the synchronizer token echoed by the form. It is rendered ON
	// PURPOSE and is not a section 4.7 secret: it exists precisely so that a page
	// an attacker cannot read is the only page that can produce a valid submission.
	CSRFToken string
	// Failed re-renders the form after a refused sign-in, with one fixed sentence
	// that names no cause.
	Failed bool
}

// AdminBusiness is one row of the "which business?" picker.
//
// IT CARRIES A TENANT ID AND THAT IS SAFE HERE, which is worth stating because
// ADR 0002 madde 7 spends a page on why a tenant must never be an INPUT. It is not
// one: the id posted back is checked for membership of the server-signed,
// password-verified set before it authorises anything (internal/handler's
// selectVerified). What is rendered is an opaque database id the operator has
// already authenticated against — not a claim the server will believe.
type AdminBusiness struct {
	TenantID   string
	TenantName string
	// Role is 'owner' or 'manager' — this operator's role IN THIS BUSINESS. Shown
	// because the same person can be an owner of one and a manager of another, and
	// that is exactly the situation the picker exists for.
	Role string
}

// AdminChooseView is the picker: which of the businesses this password verified
// against should this session belong to.
//
// 🔴 EVERY ROW HERE HAS ALREADY VERIFIED — and it is worth being exact about HOW,
// because an earlier version of this comment was not.
//
// It said the handler builds Businesses from adminauth.Authentication.Verified().
// That is true of the FIRST step only. On the picker's own request the identities
// come back from adminChoices.parse — the signed cookie — and internal/adminauth's
// Verified type carries an explicit warning against restating this as "nothing else
// can build one", because an audit refuted exactly that wording. Round 2 corrected
// the sentence in manager.go and did not sweep the file that RENDERS the picker.
//
// THE GUARANTEE, in the two legs it actually stands on (PHASE B OBLIGATION 5):
//  1. only a PASSWORD COMPARISON can create an identity, and Verified() ANDs
//     "matched" with "active" in one place;
//  2. the picker's set is reconstructed from a payload THIS DEPLOYMENT signed
//     (HMAC-SHA256), and POST /admin/login/choose re-proves membership of it
//     (internal/handler's selectVerified) before anything is issued.
//
// A view model can enforce neither; this comment only points at where they live.
type AdminChooseView struct {
	CSRFToken  string
	Businesses []AdminBusiness
}

// AdminHomeView is the protected placeholder behind the gate.
//
// 🔴 IT IS NOT THE DASHBOARD. M6-02 owns the layout and the docket components,
// M6-03 the transactions view. Two fields, both about WHO is signed in, so that
// this screen can prove the gate works without starting the next task inside this
// one. Adding a count, a chart or a filter here is M6-02's work.
type AdminHomeView struct {
	FullName string
	Role     string
}
