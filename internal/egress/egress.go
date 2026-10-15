// Package egress is the SSRF-safe outbound HTTP client of the integration API's webhooks (M11 API-9,
// ADR 0028 §7, §13). Egress from the cluster is open (plan §6, §8: no egress NetworkPolicy), so THIS
// PACKAGE IS THE ONLY DEFENCE between an owner-typed address and the cluster's inside: Postgres, the
// Kubernetes API server, the node's loopback, the cloud metadata service, Taptime's own ingress.
//
// THE SHAPE OF THE DEFENCE (ADR 0028 §7):
//   - the URL rule (endpoint.go) -- https, port 443, a DNS name, nothing a log could leak;
//   - THE DIAL-TIME GATE (allow.go): net.Dialer.ControlContext runs once for every connection
//     attempt, with the address the socket is about to connect to -- every address of a DNS answer,
//     both families of a Happy Eyeballs race, every dial of every delivery (keep-alive is off, so
//     there is no pooled connection that skipped it). A name that resolves to a public address once
//     and to a private one the next time (DNS rebinding) is refused on the second dial, because the
//     check is on the connection, never on an earlier answer;
//   - no proxy (Transport.Proxy is nil: HTTPS_PROXY in the environment is not obeyed -- a proxy would
//     move the dial, and the gate with it, to the proxy's address);
//   - no redirect (the 3xx is the answer; a Location to an internal host is never followed);
//   - 10 s for the whole exchange, a bounded response header, and the body NOT read (at most 1 KiB is
//     drained and thrown away): a remote cannot hold a worker, fill memory or have its answer read.
//
// 🔴 NOTHING LEAVES BUT A CLASS AND A STATUS. Post returns a Result -- one of six closed classes
// (ADR 0028 §6: timeout, tls, refused, http_4xx, http_5xx, blocked_address) and the HTTP status when
// there was one. Go's error texts name the URL, the host and the address (a *url.Error keeps a query
// string; a *net.OpError names the IP): they are classified here and dropped. This package has no
// logger (TestEgress_LogsNothingAndReturnsNoText pins the import list).
//
// 🔴 TLS IS GO'S DEFAULT VERIFICATION. No InsecureSkipVerify, no private root: a Client New builds
// has RootCAs nil (the system's). A test may hand its own test server's root (export_test.go).
package egress

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"syscall"
	"time"
)

// Class is the closed set a delivery's failure is reported as (ADR 0028 §6; webhook_deliveries'
// webhook_deliveries_error_class CHECK). The empty Class is success.
type Class string

// The six classes, spelled exactly as migration 00048's CHECK.
const (
	ClassTimeout Class = "timeout"
	ClassTLS     Class = "tls"
	ClassRefused Class = "refused"
	ClassHTTP4xx Class = "http_4xx"
	ClassHTTP5xx Class = "http_5xx"
	ClassBlocked Class = "blocked_address"
)

// Classes is the closed set, in the CHECK's order.
var Classes = []Class{ClassTimeout, ClassTLS, ClassRefused, ClassHTTP4xx, ClassHTTP5xx, ClassBlocked}

// Result is all a caller learns about one Post: a class ("" when the remote answered 2xx) and the
// HTTP status (0 when no response arrived). It holds no text.
type Result struct {
	Class  Class
	Status int
}

// Delivered reports a 2xx answer.
func (r Result) Delivered() bool { return r.Class == "" && r.Status >= 200 && r.Status <= 299 }

// The budgets (ADR 0028 §7).
const (
	// TotalTimeout bounds one exchange end to end: the dial, TLS, the request and the response header.
	TotalTimeout = 10 * time.Second
	// MaxResponseHeaderBytes bounds what a remote's response header may cost.
	MaxResponseHeaderBytes = 16 << 10
	// drainLimit is the most of a response body ever read -- and only to discard it.
	drainLimit = 1 << 10
	// MaxRequestBody bounds what a caller may send: a punch event is a few hundred bytes.
	MaxRequestBody = 64 << 10
)

// Options builds a Client.
type Options struct {
	// Blocked are the node's own public addresses and the ingress's (ADR 0028 §7:
	// TAPPA_WEBHOOK_BLOCKED_ADDRESSES). Refused before anything else.
	Blocked []netip.Prefix
	// DevelopmentWithoutBlockedList allows an EMPTY Blocked -- for TAPPA_ENV=dev alone (a laptop
	// has no ingress, and the registry rules already refuse its loopback and private ranges). A
	// production or staging deployment never sets it: config refuses to start there without the
	// list, and New refuses it here without this flag (ADR 0028 §7: "liste boşsa prod ve sandbox
	// başlamaz").
	DevelopmentWithoutBlockedList bool
}

// ErrNoBlockedList is New's refusal of an empty node/ingress list outside development.
var ErrNoBlockedList = errors.New("egress: the node and ingress addresses are required (TAPPA_WEBHOOK_BLOCKED_ADDRESSES); " +
	"without them a webhook could be pointed back at Taptime's own ingress")

// Client posts to webhook endpoints. Safe for concurrent use.
type Client struct {
	gate    *gate
	http    *http.Client
	timeout time.Duration
}

// New builds the client. It refuses an empty Blocked list unless DevelopmentWithoutBlockedList.
func New(o Options) (*Client, error) {
	if len(o.Blocked) == 0 && !o.DevelopmentWithoutBlockedList {
		return nil, ErrNoBlockedList
	}
	g := &gate{port: deliveryPort}
	for _, p := range o.Blocked {
		if !p.IsValid() {
			return nil, errors.New("egress: an invalid blocked prefix")
		}
		// One spelling: a v4-mapped prefix is the IPv4 range it maps (addresses are Unmap()ed before
		// they are compared).
		if p.Addr().Is4In6() {
			p = netip.PrefixFrom(p.Addr().Unmap(), max(p.Bits()-96, 0))
		}
		g.blocked = append(g.blocked, p.Masked())
	}
	return build(g, nil, nil, TotalTimeout), nil
}

// build assembles the transport around a gate. resolver and roots are nil in every Client New
// builds (the system resolver and the system roots); a test passes its own.
func build(g *gate, resolver *net.Resolver, roots *x509.CertPool, timeout time.Duration) *Client {
	dialer := &net.Dialer{
		Timeout:  timeout,
		Resolver: resolver,
		// THE GATE. It runs for every connection attempt with the address being connected to.
		ControlContext: func(_ context.Context, network, address string, _ syscall.RawConn) error {
			if network != "tcp4" && network != "tcp6" {
				return errBlocked
			}
			ap, err := netip.ParseAddrPort(address)
			ok := err == nil && g.allowed(ap)
			if g.observe != nil {
				g.observe(ap, ok)
			}
			if !ok {
				return errBlocked
			}
			return nil
		},
	}
	port := strconv.Itoa(int(g.port))
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			// Every Endpoint is :443 (the URL rule); the dial names the gate's port so the two can
			// never disagree. The host part is the URL's host, resolved by the dialer above.
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, errBlocked
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
		TLSClientConfig:        &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    timeout,
		ResponseHeaderTimeout:  timeout,
		MaxResponseHeaderBytes: MaxResponseHeaderBytes,
		DisableKeepAlives:      true,
		DisableCompression:     true,
		MaxIdleConns:           0,
	}
	return &Client{
		gate:    g,
		timeout: timeout,
		http: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			// The redirect is the answer, never a second request.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Post sends body to the endpoint with the given headers (Content-Type and the signature are the
// caller's) and reports the outcome as a Result. It never returns an error and never logs: every
// failure is one of the six classes.
func (c *Client) Post(ctx context.Context, to Endpoint, body []byte, header http.Header) Result {
	if to.IsZero() || len(body) > MaxRequestBody {
		// A programming error, not a remote's answer; reported in the closed set, never as text.
		return Result{Class: ClassRefused}
	}
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, to.reveal(), bytes.NewReader(body))
	if err != nil {
		return Result{Class: ClassRefused}
	}
	for k, vs := range header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{Class: classify(err)}
	}
	// The body is not read: at most drainLimit bytes are discarded, then the connection is closed
	// (keep-alive is off, so nothing waits for the rest).
	_, _ = io.CopyN(io.Discard, resp.Body, drainLimit)
	_ = resp.Body.Close()
	return statusResult(resp.StatusCode)
}

// statusResult maps an answer to a Result. 2xx is success. A 3xx is not followed and is reported as
// http_4xx: the closed set has no class of its own for it, and what it asks of the owner is what a
// 4xx asks -- the address is not the one the receiver wants (the status is shown beside it).
func statusResult(status int) Result {
	switch {
	case status >= 200 && status <= 299:
		return Result{Status: status}
	case status >= 500:
		return Result{Class: ClassHTTP5xx, Status: status}
	default:
		return Result{Class: ClassHTTP4xx, Status: status}
	}
}

// classify maps a failed exchange to a class. The error itself goes no further.
func classify(err error) Class {
	if errors.Is(err, errBlocked) {
		return ClassBlocked
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ClassTimeout
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return ClassTimeout
	}
	var (
		verify   *tls.CertificateVerificationError
		unknown  x509.UnknownAuthorityError
		hostname x509.HostnameError
		invalid  x509.CertificateInvalidError
		record   tls.RecordHeaderError
		alert    tls.AlertError
	)
	// A server that answers plain HTTP on 443 does not speak TLS: net/http reports it as
	// ErrSchemeMismatch rather than as the record-header error underneath.
	if errors.Is(err, http.ErrSchemeMismatch) || errors.As(err, &verify) || errors.As(err, &unknown) || errors.As(err, &hostname) ||
		errors.As(err, &invalid) || errors.As(err, &record) || errors.As(err, &alert) {
		return ClassTLS
	}
	// Everything else -- a name that does not resolve, nothing listening on 443, a reset, a response
	// too large or malformed -- is "the endpoint could not be reached".
	return ClassRefused
}
