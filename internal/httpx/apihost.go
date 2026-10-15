package httpx

import (
	"net/http"
	"strings"
)

// THE INTEGRATION API'S HOST, AS THIS PACKAGE SEES IT (M11 API-4; ADR 0028 §13, K-9).
//
// The /v1 surface lives on a host of its own (TAPPA_API_HOST, api.taptime.mt) and the gate between
// that host and the rest of the product has two halves -- operatorHostOnly's shape, generalised to a
// second separate host:
//
//   - /v1 answers 404 on a host that is not the API's -- internal/handler/api's hostGate, the first
//     link of the /v1 chain;
//   - everything that is not /v1 answers 404 on the API host -- apiHostOnly below, here, because
//     this package mounts the other routes. A panel, tap or operator page served on the API host
//     would be a cookie-bearing, form-posting surface on the one origin that has neither (§13: "API'de
//     çerez yoktur, CSRF yüzeyi yoktur"), and an HTML route there is a 404 by the ADR's own words.
//
// The request's host is classified by OnHost (a port and one trailing dot dropped, case ignored;
// X-Forwarded-Host never read) and the path by routingPath, so the two halves and the operator gate
// classify a request the same way. TestApiHost_TwoWayGate drives both halves through the shipped
// router.

// APIPrefix is the integration API's path (ADR 0028 §8: /v1 and everything under it).
// internal/handler/api's Prefix IS this constant.
const APIPrefix = "/v1"

// OnAPIPath reports whether r's routing path is APIPrefix or under it. "/v1x" and "/v1%2F…" are
// not: the router would not route them to the /v1 surface either.
func OnAPIPath(r *http.Request) bool {
	p := routingPath(r)
	return p == APIPrefix || strings.HasPrefix(p, APIPrefix+"/")
}

// apiHostOnly is the NON-API half of the two-way gate. On the API host (OnHost) it passes APIPrefix
// and the paths under it to the router and answers every other path -- the panel, the tap, the
// landing page, the static assets, the probes, the operator surface -- with http.NotFound, the
// router's own 404. Off the API host it passes the request on unchanged (the /v1 half of the gate
// is the surface's hostGate). NewRouter mounts it when TAPPA_API_HOST is set.
func apiHostOnly(host string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if OnHost(r, host) && !OnAPIPath(r) {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
