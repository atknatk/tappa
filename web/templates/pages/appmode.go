package pages

import "net/url"

// THE NATIVE APP'S IN-APP BROWSER (M16-54, ADR 0051 §2.6). The staff app opens the sign-up
// wizard ("Create a business account") and the legal documents in its in-app browser with
// ?client=app, and those pages then draw without a road to the site, the panel or a price:
// App Store Guideline 3.1.1 asks that nothing inside the app lead to a way of buying outside
// it (ADR 0051, user decision 4). What stays is ADR 0051 §2.6's list: the wordmark (no longer a
// link), the footer's two red-line sentences, "Built by" (decision K-B), the Legal column and
// the document navigation (their links carry the flag), the wizard's own forms and Back, the
// links a published legal text carries, and the dev or sandbox installation's on-screen
// registration link.
//
// 🔴 IT IS PRESENTATION AND NOTHING ELSE. No check reads it: the wizard's Origin rule, its
// synchronizer token, its signed state and step order, its budgets and its bot defence answer
// the same with the flag and without it (internal/handler's [H54-c]). Without the flag every
// page is byte for byte what it was before M16-54 ([H54-b]).
//
// HOW IT TRAVELS is the handler's business: a legal page reads it from its own query (a
// document has no state to put it in, and its links carry it on); the wizard reads it from the
// query of its first step only and carries it in its signed state cookie from there
// (internal/handler/signupstate.go) -- never in a form field, a later query or the stored
// registration draft.

// The query the app adds. Written once here; the app writes the same two words
// (mobile/, M16-53).
const (
	AppClientParam = "client"
	AppClientValue = "app"
)

// AppClientRequested reports whether a query carries the app's flag.
func AppClientRequested(q url.Values) bool {
	return q.Get(AppClientParam) == AppClientValue
}

// AppHref is path with the app's flag: an in-app link that keeps the next page in app mode.
// path carries no query of its own (every caller passes a route constant).
func AppHref(path string) string {
	return path + "?" + AppClientParam + "=" + AppClientValue
}

// legalHref is a legal document's link: with the flag inside the app, the bare path outside it.
func legalHref(app bool, path string) string {
	if app {
		return AppHref(path)
	}
	return path
}
