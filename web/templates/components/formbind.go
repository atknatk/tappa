package components

import (
	"context"
	"strings"
)

// formbind.go -- where a panel form's action picks up its tenant binding (ADR 0031 §5.5,
// M14-10a).
//
// internal/handler computes the binding for the request it is answering (the session's
// tenant under the session's raw token) and places it on the RENDER context, the way
// WithDevTools marks a development render. Every panel POST form writes its action as
// templ.URL(components.Bound(ctx, href)), so the value never passes through a view
// struct: the views stay data (the §4.7 walls walk them by type) and a page cannot
// forget the binding without the template scan in internal/handler seeing it
// (TestPanelForms_EveryPostFormCarriesTheTenantBinding).
//
// A render without a binding -- every test that renders with a plain context, every page
// outside the panel -- writes the href unchanged, so the goldens are untouched.
//
// THE BINDING IS IN THE QUERY, NOT THE BODY (ADR 0031 §5.5, R7): the gate in front of the
// handlers reads r.URL and never touches the body, so a handler's own MaxBytesReader is
// still the first thing to read it.

// FormBindingParam is the query parameter the binding travels in.
const FormBindingParam = "tb"

type formBindingKey struct{}

// WithFormBinding returns ctx carrying the binding the panel's forms are to post.
func WithFormBinding(ctx context.Context, binding string) context.Context {
	if binding == "" {
		return ctx
	}
	return context.WithValue(ctx, formBindingKey{}, binding)
}

// Bound is href with the render's binding appended as ?tb=… (or &tb=… when href already
// has a query). Without a binding on ctx it is href.
//
// The binding is unpadded base64url, so it needs no escaping in a query.
func Bound(ctx context.Context, href string) string {
	b, _ := ctx.Value(formBindingKey{}).(string)
	if b == "" {
		return href
	}
	sep := "?"
	if strings.Contains(href, "?") {
		sep = "&"
	}
	return href + sep + FormBindingParam + "=" + b
}
