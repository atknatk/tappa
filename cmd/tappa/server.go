package main

import (
	"net/http"
	"time"
)

// serverTimeouts are the http.Server's connection bounds. A type so that a test can
// drive applyTimeouts with the same shape at a scale it can wait for.
type serverTimeouts struct {
	readHeader time.Duration
	read       time.Duration
	idle       time.Duration
}

// productionTimeouts are what the process serves with.
//
// 🔴 read (ReadTimeout) WAS MISSING UNTIL BACKLOG T91. With only ReadHeaderTimeout, a
// client that sent its headers and then stopped sending the body held its connection
// and the handler goroutine blocked in Body.Read for as long as it liked: no context
// reaches that read (httpx.RequestTimeout cancels a context, it does not unblock a
// socket read), and only the logo upload set a deadline of its own. ReadTimeout is the
// CONNECTION's deadline from the first byte of the request to the last of its body, so
// it ends that read whatever the handler is doing.
//
// WHY 30 s, and what it is measured against:
//
//   - THE LARGEST BODY IT GOVERNS is the operator's legal text: 256 KiB in the handler
//     (maxLegalBody), "320k" at the operator's Ingress (deploy/k8s/41-operator-ingress.yaml).
//     After the worst header allowance (readHeader, 10 s) 20 s are left: the handler's
//     256 KiB needs about 13 KB/s, the Ingress's 320 KiB about 16 KB/s. The panel's forms
//     are at most 64 KiB (the sign-up form) and the API's bodies 80 KiB
//     (42-api-ingress.yaml).
//   - THE LARGEST BODY OF ALL, THE LOGO (1 MiB, brandUploadMaxBody), IS NOT GOVERNED BY
//     IT. The upload gate sets its own connection deadline, now + 15 s, once it has
//     admitted the request, and a later SetReadDeadline REPLACES the server's in either
//     direction -- measured in TestServer_AHandlersReadDeadlineReplacesTheServers: a
//     longer one lets a body arrive after ReadTimeout, a shorter one cuts it before. So
//     the logo keeps exactly the window M10 WL-7 chose (1 MiB in 15 s, about 70 KB/s),
//     and this value only has to cover the time until the gate runs: the headers and
//     the panel's resolver.
//   - IT DOES NOT CUT A LONG HANDLER. Once the body is read the deadline only bounds
//     the server's background read; measured, a handler still running after ReadTimeout
//     sees no cancelled context and writes its whole answer
//     (TestServer_AHandlerOutlivingTheReadTimeoutIsNotCut). That is what keeps a tap's
//     late confirmation and the long CSV export intact -- the same reason there is no
//     WriteTimeout below.
//   - It equals httpx.RequestTimeout: a request has 30 s to arrive and then 30 s to be
//     answered. A slow-body client holds a goroutine for at most this long.
//
// WHAT IT GUARDS IN PRODUCTION: THE PATH AROUND THE INGRESS. The live controller
// buffers request bodies (read by the orchestrator with nginx -T, 2026-11-07:
// proxy_request_buffering on, client_body_timeout 60s, global), so a slow client on the
// public hosts meets the controller's 60 s and the pod reads the body at cluster
// speed. What this bound covers is everything that reaches the pod directly:
// deploy/k8s/12-networkpolicy.yaml has no ingress policy for the application pod (read
// in the T21/T91 review), so any pod or node in the cluster can open a connection to
// it and hold a body open -- without this, indefinitely.
var productionTimeouts = serverTimeouts{
	readHeader: 10 * time.Second,
	read:       30 * time.Second,
	idle:       90 * time.Second,
}

// applyTimeouts sets t on srv. run() calls it on its one server with productionTimeouts;
// the tests call it on a loopback server with scaled ones.
func applyTimeouts(srv *http.Server, t serverTimeouts) {
	srv.ReadHeaderTimeout = t.readHeader
	srv.ReadTimeout = t.read
	srv.IdleTimeout = t.idle
}
