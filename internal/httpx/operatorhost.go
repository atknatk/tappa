package httpx

import (
	"net"
	"net/http"
	"strings"
)

// THE OPERATOR HOST, AS THIS PACKAGE SEES IT (M10 OP-8; ADR 0020 §4).
//
// The platform operator's surface lives on a host of its own (TAPPA_OPERATOR_HOST,
// D-B: ops.taptime.mt) and the gate between that host and the customer product's
// hosts has two halves:
//
//   - operator routes answer 404 on a host that is not the operator's --
//     internal/handler/operator's hostGate, the first link of the operator chain;
//   - customer routes answer 404 on the operator host -- operatorHostOnly below, here,
//     because this package mounts the customer routes. A customer page rendered on the
//     operator host would run a script it carried in the SAME ORIGIN as the operator's
//     session cookie (ADR 0020 "Karar verilmedi": "operatör host'unda render edilen
//     herhangi bir müşteri sayfasındaki bir XSS, operatör çereziyle aynı origin'de
//     koşar").
//
// This file's imports are net, net/http and strings
// (TestOperatorHostFile_ImportsOnlyThreeStandardPackages catches a change to that
// list). ADR 0021 §3.6 keeps the customer panel from reaching the operator's packages,
// and internal/handler imports this package (m10-platform.md, OP-7 card correction md. 9
// (v)). What this file knows is a string -- the host, from internal/config -- and a
// path, OperatorPrefix, which internal/handler/operator declares as its own Prefix
// (TestSurface_ThePrefixIsTheRoutersPrefix).

// OperatorPrefix is the operator surface's path (ADR 0020 §4: /operator and everything
// under it). internal/handler/operator's Prefix IS this constant.
const OperatorPrefix = "/operator"

// staticPrefix is the asset route NewRouter registers ("/static/*"). The operator's
// screens load their stylesheet, the brand faces and their script from it; operatorHostOnly
// lets it through on the operator host.
const staticPrefix = "/static/"

// OnHost reports whether r was addressed to host. operatorHostOnly here and hostGate in
// internal/handler/operator call it, so the two halves classify a request the same way.
//
// host is internal/config's spelling (a lower-case DNS name, no port, no trailing dot --
// config.operatorHost refuses other forms). The request's Host is brought to that form
// before the comparison: a port is dropped, one trailing dot is dropped, and case is
// ignored -- measured on the rows of TestOnHost_ReducesTheRequestHostToTheConfiguredSpelling:
//
//   - THE PORT. Development serves ops.localhost:8080; behind the ingress a browser sends
//     no port. Cookies are not port-scoped, so a classification that kept the port would
//     treat one browser jar as two hosts.
//   - THE TRAILING DOT AND THE CASE. "ops.taptime.mt." and "OPS.TAPTIME.MT" name the same
//     DNS host as "ops.taptime.mt". Whether a request so spelled reaches this pod is the
//     ingress's decision and is not measured here. Classified as the operator host, such a
//     request is served the operator surface; a page under that spelling posts with
//     `Origin: null` + `Sec-Fetch-Site: same-origin` (the pages' referrer policy is
//     no-referrer; measured by the OP-8 auditor in headless Chrome, 2026-10-02), which
//     sameOriginGate's fallback accepts.
//
// The host read is r.Host as net/http parsed it -- for an absolute-URI request line, the
// URI's host, not the Host header (TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide
// drives both). OnHost does not read X-Forwarded-Host (that test's two X-Forwarded-Host
// rows; mutation M08 turned them red).
func OnHost(r *http.Request, host string) bool {
	if host == "" {
		return false
	}
	h := r.Host
	if hh, _, err := net.SplitHostPort(h); err == nil {
		h = hh
	}
	h = strings.TrimSuffix(h, ".")
	return strings.EqualFold(h, host)
}

// routingPath is the path chi routes on: the escaped form when the URL carries one
// (r.URL.RawPath), otherwise the decoded form, so the gate reads the string the router
// matches (/operator%2Flogin is under the prefix for neither). Mutation M09 (classifying by
// the decoded r.URL.Path) left the tests green: on their rows the two classify alike. It is
// a consistency choice.
func routingPath(r *http.Request) string {
	if r.URL.RawPath != "" {
		return r.URL.RawPath
	}
	return r.URL.Path
}

// operatorHostOnly is the CUSTOMER half of the two-way host gate. On the operator host
// (OnHost) it passes OperatorPrefix, the paths under it and staticPrefix's paths to the
// router, redirects GET and HEAD of "/" to the surface, and answers the other paths with
// http.NotFound -- the handler the router uses for a path it has no route for. When OnHost
// is false it passes the request on unchanged. Measured on the rows of
// TestOperatorHostOnly_EveryEscapeAttemptLandsOnOneSide.
//
// NewRouter mounts it when the configuration names an operator host (TAPPA_OPERATOR_HOST):
// the configured surface and the UNAVAILABLE one (whose configuration is complete). With
// no operator configuration NewRouter does not mount it (that test's CONTROL rows).
//
// It does not answer /operator on a customer host -- that is the operator surface's
// hostGate, which runs in the configured state, so the unavailable surface's 503 (OP-7)
// answers on each host the tests drive. The probes (/healthz, /readyz) are not served on
// the operator host: deploy/k8s/20-app.yaml's httpGet probes name no host, so the kubelet
// sends them with the pod address (read from the manifest; not measured in a cluster).
func operatorHostOnly(host string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !OnHost(r, host) {
				next.ServeHTTP(w, r)
				return
			}
			p := routingPath(r)
			switch {
			case p == OperatorPrefix, strings.HasPrefix(p, OperatorPrefix+"/"), strings.HasPrefix(p, staticPrefix):
				next.ServeHTTP(w, r)
			case p == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
				// Somebody typed the bare host. The landing page is a customer route, so
				// it is not served here; the operator's own front door is.
				w.Header().Set("Cache-Control", "no-store")
				http.Redirect(w, r, OperatorPrefix, http.StatusSeeOther)
			default:
				http.NotFound(w, r)
			}
		})
	}
}
