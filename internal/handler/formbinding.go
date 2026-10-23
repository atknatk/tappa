package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/web/templates/components"
	"github.com/atknatk/tappa/web/templates/pages"
)

// formbinding.go -- every panel form is bound to the business it was rendered for
// (ADR 0031 §5.5, M14-10a; bulgu 3, R7).
//
// THE DEFECT. The panel is one cookie on one path. A tab drawn for business A and
// submitted after the browser moved to business B would write into B: the Origin check,
// the session and every role gate pass, because the session IS live and IS in B. Nothing
// on a panel write was tied to the business its page showed (adminlogin.go's writing
// chains, before this file).
//
// THE TIE. Each POST form's action carries ?tb=<binding> (components.Bound), the binding
// being the session tenant's MAC under the session's RAW token (adminauth.FormBinder).
// formBindingGate sits in BOTH writing chains -- ProtectWriting and
// protectWritingThroughSuspension -- right after the identity and the session budget and
// before the suspension ring, so a refused request writes nothing anywhere: not a record,
// not a suspension refusal row, not an audit line.
//
// 🔴 IT READS r.URL AND NEVER THE BODY (R7). A gate that read a form field would parse
// the body with net/http's default 10 MB / 32 MB ceilings BEFORE each handler's own
// http.MaxBytesReader (billingactions.go, brandactions.go, accountactions.go,
// brandupload.go set theirs before parsing). The query string is already parsed off the
// request line; a refused request's body is never read by this process's handlers.
//
// 🔴 IT IS READ ONLY TO REFUSE (ADR 0031 §3). The gate passes the SAME request context
// on; the tenant every handler acts in is still httpx.AdminOf's, i.e. the session row's.
// The binding cannot name a tenant: it is compared with the binding of the session's own
// tenant and either matches it or the request ends here.
//
// 🔴 IT LEAVES NO URL BEHIND (S14). After a match the parameter is removed from the
// request the handler sees, so no handler can build a redirect or a link that carries it;
// every bound write still ends in a 303 to a fixed panel path (POST-redirect-GET). The
// access log writes route patterns only (internal/httpx/requestlog.go), the panel sends
// no-referrer (layout.documentHead), and deploy/README.md records what the ingress's own
// access log does with a query string.
//
// THE ONE NAMED EXCEPTION: the plaque encode relay's three routes (encodeRelayRoutes).
// They are not forms. The Android relay (android/, ADR 0017) posts them with the panel
// cookie it reads from its WebView's CookieManager on EVERY request and renders no page,
// so there is no stale page to bind and it has no way to learn a binding: binding them
// would stop every plaque encoding. They keep the Origin check, the session and their own
// per-session ceiling (encodeGate). This is a decision outside ADR 0031 §5.5's text and
// is reported as such (M14-10a hand-off).

// formBindingGate refuses a panel write whose binding is missing or is not the binding
// of the session's tenant under the session's token.
func (a *AdminAuth) formBindingGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		route := matchedRoute(r)
		if encodeRelayRoutes[route] {
			next.ServeHTTP(w, r)
			return
		}
		if !a.formBound(r) {
			// The value itself is never logged: it is a per-session credential.
			a.log.Warn("panel write refused: the page was rendered for another business or session",
				"method", r.Method, "route", route)
			a.renderProblem(w, r, http.StatusConflict, problemFormElsewhere)
			return
		}
		next.ServeHTTP(w, withoutFormBinding(r))
	})
}

// formBound reports whether r carries exactly one binding and it is the session's.
// Fail closed: no live identity, no cookie, two values -- all are "not bound".
func (a *AdminAuth) formBound(r *http.Request) bool {
	id := httpx.AdminOf(r)
	if !id.Live() {
		return false
	}
	got := r.URL.Query()[components.FormBindingParam]
	if len(got) != 1 {
		return false
	}
	tok, err := a.cookies.Read(r)
	if err != nil {
		return false
	}
	return a.forms.Matches(tok, id.TenantID(), got[0])
}

// withoutFormBinding is r with the binding removed from its URL, so nothing downstream
// can echo it into a redirect, a link or a log. The context is r's own -- the gate adds
// nothing to it.
func withoutFormBinding(r *http.Request) *http.Request {
	u := *r.URL
	u.RawQuery = dropQueryParam(u.RawQuery, components.FormBindingParam)
	out := new(http.Request)
	*out = *r
	out.URL = &u
	out.RequestURI = u.RequestURI()
	return out
}

// dropQueryParam removes every name=… pair from a raw query, keeping the others and
// their order byte for byte.
func dropQueryParam(raw, name string) string {
	if raw == "" {
		return raw
	}
	kept := make([]string, 0, 2)
	for _, pair := range strings.Split(raw, "&") {
		key, _, _ := strings.Cut(pair, "=")
		if key == name {
			continue
		}
		kept = append(kept, pair)
	}
	return strings.Join(kept, "&")
}

// formBindingContext is the render context of a panel page: r's context carrying the
// binding of the session's tenant, so every POST form the page draws posts it. A request
// without a live identity or a readable cookie renders unbound forms -- which the gate
// refuses, the fail-closed direction.
func (a *AdminAuth) formBindingContext(r *http.Request) context.Context {
	ctx := r.Context()
	id := httpx.AdminOf(r)
	if !id.Live() {
		return ctx
	}
	tok, err := a.cookies.Read(r)
	if err != nil {
		return ctx
	}
	b, err := a.forms.Bind(tok, id.TenantID())
	if err != nil {
		return ctx
	}
	return components.WithFormBinding(ctx, b)
}

// problemFormElsewhere is the 409's page. The title is ADR 0031 §5.5's sentence; the
// message's claim is measured -- the gate runs before any write
// (TestFormBinding_AStaleFormWritesNothingInTheOtherBusiness).
var problemFormElsewhere = pages.ProblemView{
	Title:   "This page belongs to another business — reload it.",
	Message: "It was opened for a different business, or before you last signed in, so nothing was saved.",
	Hint:    "Reload the page, check which business you are in, and try again.",
}
