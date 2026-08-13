package signup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// VIES — the European Commission's VAT Information Exchange System, called BEST
// EFFORT (Q09, orchestrator decision 2026-08-13).
//
// 🔴 THE ONE RULE THIS FILE EXISTS TO KEEP: A VIES FAILURE NEVER STOPS A
// REGISTRATION. The M7-02 card states it as an acceptance criterion ("servis
// kesintisi kayıt akışını durdurmuyor") and Q09 gives the reason — a VIES outage is
// the EU's fault, not the customer's, and a synchronous gate in front of it would
// lose a paying customer to a service nobody here operates. So every failure mode
// below returns Unknown rather than an error the caller could turn into a refusal,
// and the caller has no branch that could refuse on it.
//
// AND THE OTHER HALF, WHICH IS §4.6: "no answer" is stored as a RECORD, never as a
// silent approval and never as an accusation. tenants.vat_verified is NULLABLE for
// exactly this — migration 00017 spells out the four states. A boolean would have
// had to write an outage down as "this VAT number is not valid".
//
// NO NEW DEPENDENCY (CLAUDE.md §1). This is net/http, encoding/json and a URL built
// from two constants. The SOAP endpoint VIES is better known for would have wanted
// an XML envelope; the REST endpoint answers the same question in JSON and needs no
// library at all.
//
// ⚠️ IT IS AN OUTBOUND CALL FROM AN UNAUTHENTICATED ENDPOINT, which is a shape this
// repository has good reason to be careful about. Four things bound it and they are
// named rather than assumed:
//
//  1. THE URL IS BUILT FROM CONSTANTS AND A VALIDATED VAT NUMBER. viesBaseURL is a
//     literal; the two path segments are the output of SplitVAT over a value that
//     has ALREADY passed ValidVATFormat, i.e. two letters from a fixed table and a
//     string matching an anchored pattern of digits and upper-case letters. Nothing
//     a caller types can redirect this at another host — there is no SSRF surface
//     because there is no caller-supplied host, port, scheme or path.
//  2. IT IS ONLY REACHED FOR A NUMBER THAT PASSED THE FORMAT CHECK, so garbage
//     costs zero outbound requests.
//  3. IT CARRIES ITS OWN TIMEOUT, short, on top of the request context.
//  4. THE ENDPOINT THAT CALLS IT IS RATE LIMITED per address
//     (internal/handler/signupratelimit.go), which is what bounds how many
//     outbound requests one caller can make us issue.
//
// 🔴 REDIRECTS ARE REFUSED RATHER THAN FOLLOWED, which closes the one way (1) could
// be undone by somebody else: a 30x from the remote host is a host WE did not
// choose, and net/http follows up to ten of them by default. CheckRedirect below
// turns the first one into an error, i.e. into Unknown.

// VATStatus is what a VIES lookup established.
//
// THREE VALUES, NOT A BOOL, and it is the same argument migration 00017 makes for
// the column: "we could not ask" and "the register says no" are different facts and
// only one of them is the customer's problem.
type VATStatus int

const (
	// VATUnknown means no answer: a timeout, a network failure, a 5xx, an
	// unparseable body, or a member state's own register being down (VIES reports
	// that per country and it is common).
	VATUnknown VATStatus = iota
	// VATValid means VIES confirmed the number.
	VATValid
	// VATInvalid means VIES answered and does not know the number.
	VATInvalid
)

func (s VATStatus) String() string {
	switch s {
	case VATValid:
		return "valid"
	case VATInvalid:
		return "invalid"
	default:
		return "unknown"
	}
}

// Verified renders the status as the three-state value migration 00017 stores. A
// nil result is the honest representation of "we asked and got no answer".
func (s VATStatus) Verified() *bool {
	switch s {
	case VATValid:
		t := true
		return &t
	case VATInvalid:
		f := false
		return &f
	default:
		return nil
	}
}

const (
	// viesBaseURL is the European Commission's REST check endpoint. A CONSTANT, so
	// there is no configuration through which a deployment could be pointed at
	// somebody else's server and no request parameter that could.
	viesBaseURL = "https://ec.europa.eu/taxation_customs/vies/rest-api/ms"

	// viesTimeout is how long one lookup may take.
	//
	// THREE SECONDS, AND THE NUMBER IS A PRODUCT DECISION RATHER THAN A GUESS AT
	// VIES'S LATENCY. It is spent on the FIRST step of a three-step wizard (see
	// Checker.Check's caller), so a customer who waits the full timeout still has
	// two more form pages in front of them — the delay lands where somebody is
	// mid-task rather than at the end where they are waiting for a result. What it
	// must not be is long enough to hold a connection from the pool-free
	// unauthenticated surface for a noticeable fraction of the router's 30 second
	// timeout, and 3 s is a tenth of it.
	//
	// ⚠️ IT IS A CEILING ON OUR PATIENCE, NOT A MEASUREMENT OF VIES. Nothing in this
	// repository has measured that service's real latency distribution, and this
	// comment deliberately does not invent one. What is measured is the consequence
	// of the ceiling being reached: Unknown, stored as NULL, shown in the panel as
	// "we could not check this yet" — never a refusal.
	viesTimeout = 3 * time.Second

	// viesMaxBody bounds the response we will read. The real body is a few hundred
	// bytes; the bound exists so a hostile or broken upstream cannot make this
	// process allocate.
	viesMaxBody = 16 << 10
)

// Checker asks VIES about one VAT number.
//
// IT IS A STRUCT WITH AN INJECTED *http.Client rather than a package function over
// http.DefaultClient, for two reasons that are both about what tests can do. A
// package-level default client is process-wide state (CLAUDE.md §7 forbids it), and
// a test that could not point this at an httptest server would either have to reach
// the real European Commission — which would make `make test` depend on the
// internet and on a third party's uptime — or would leave this code untested.
type Checker struct {
	client  *http.Client
	baseURL string
}

// NewChecker builds the production checker.
//
// THE CLIENT IS THIS PACKAGE'S OWN, NOT http.DefaultClient. Sharing the default
// client means sharing its transport, its connection pool and — the part that
// matters — anything another package sets on it. This one carries the timeout, and
// the redirect refusal described at the top of the file.
func NewChecker() *Checker {
	return &Checker{
		client: &http.Client{
			Timeout: viesTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return errors.New("signup: vies: refusing to follow a redirect")
			},
			Transport: &http.Transport{
				// A bounded dial and TLS handshake, so a host that accepts a
				// connection and then says nothing cannot consume the whole
				// timeout before the request is even sent.
				DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
				TLSHandshakeTimeout:   2 * time.Second,
				ResponseHeaderTimeout: viesTimeout,
				// No idle connections are kept: a registration happens once per
				// customer, so a pooled connection to a host we speak to that
				// rarely is memory held for nothing.
				DisableKeepAlives: true,
			},
		},
		baseURL: viesBaseURL,
	}
}

// newCheckerAt is the test seam: the same client behaviour pointed at a local
// server. It is unexported, so no production path can move the base URL.
func newCheckerAt(base string, c *http.Client) *Checker {
	return &Checker{client: c, baseURL: base}
}

// viesResponse is the subset of the REST answer this product reads.
//
// ONE FIELD. VIES also returns the trader's registered name and address, and this
// type deliberately has nowhere to put them: we asked whether the number exists,
// not who it belongs to, and a field that exists is a field something eventually
// stores. The name on the invoice is the one the customer typed.
type viesResponse struct {
	IsValid bool `json:"isValid"`
}

// Check asks VIES about a NORMALISED, FORMAT-VALID VAT number.
//
// IT RETURNS NO ERROR. That is the file's rule expressed in the signature: there is
// no error value for a caller to propagate into a refusal, so "VIES was down" cannot
// become "your registration failed" by anybody forgetting a branch. The reason for
// an Unknown is reported to the process log by the caller if it wants one; it is
// never reported to the visitor, who can do nothing with it.
//
// A nil or zero Checker answers Unknown, which is the correct behaviour for a
// deployment that has not wired one: no check happened, and that is what gets
// stored.
func (c *Checker) Check(ctx context.Context, normalisedVAT string) VATStatus {
	if c == nil || c.client == nil || c.baseURL == "" {
		return VATUnknown
	}
	country, number, ok := SplitVAT(normalisedVAT)
	if !ok || !ValidVATFormat(normalisedVAT) {
		// Unreachable through the boundary, which checks the format first. Answering
		// Unknown rather than Invalid is the fail-open direction this file requires:
		// an internal mistake must not become an accusation against a customer.
		return VATUnknown
	}

	// The timeout rides on the CALLER's context as well as on the client, so a
	// visitor who closes the tab stops this call too.
	ctx, cancel := context.WithTimeout(ctx, viesTimeout)
	defer cancel()

	url := fmt.Sprintf("%s/%s/vat/%s", c.baseURL, country, number)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return VATUnknown
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return VATUnknown
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// 4xx from this endpoint means "I could not address that", not "that number
		// is invalid"; 5xx means the register is down. Neither is an answer.
		return VATUnknown
	}
	var out viesResponse
	// The reader is bounded (see viesMaxBody). json.Decoder stops at the first
	// value, so trailing rubbish does not fail a well-formed answer.
	if err := json.NewDecoder(&limitedReader{r: resp.Body, n: viesMaxBody}).Decode(&out); err != nil {
		return VATUnknown
	}
	if out.IsValid {
		return VATValid
	}
	return VATInvalid
}

// limitedReader is io.LimitedReader with one difference that matters here: hitting
// the bound is an ERROR rather than a clean EOF.
//
// io.LimitReader would make a truncated body look like a complete one, so a
// half-received answer could decode into `{"isValid":false}` shaped garbage and be
// reported as VATInvalid — an accusation built out of a network failure. This
// returns an error instead, which becomes Unknown.
type limitedReader struct {
	r interface{ Read([]byte) (int, error) }
	n int
}

func (l *limitedReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		return 0, errors.New("signup: vies: response body is larger than expected")
	}
	if len(p) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= n
	return n, err
}
