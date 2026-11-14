package oauth

import (
	"errors"
	"net"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// THE REDIRECT URI RULE (ADR 0047 §10.2, §10.4) -- in Go here, and in SQL in 00104's
// oauth_clients_redirect_uris_shape CHECK and the two op_* writers' bodies (defence in depth).
//
// 🔴 THE GO RULE IS NEVER LOOSER THAN THE SQL ONE: registryPattern below is the SQL expression,
// character for character, and every URI must match it BEFORE the Go checks run; the Go checks only
// add (url.Parse agrees, a port of at most 65535, no repeats in a list) and the build adds the one
// rule SQL cannot express: a loopback URI is accepted only by the sandbox build (LoopbackRedirects).
// The SQL side takes both forms because the same migrations run under both builds (§10.2: a flag in
// the database would be a new writable root of trust).

// registryPattern is 00104's CHECK and 00107's op_* bodies' expression, verbatim.
const registryPattern = `^(https://([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?(:443)?|http://(localhost|127\.0\.0\.1):[1-9][0-9]{0,4})/[A-Za-z0-9._~!$&()*+,;=:%/-]*$`

var registryRule = regexp.MustCompile(registryPattern)

const (
	// MaxRedirectURIs is the list's bound (00104: 1..5).
	MaxRedirectURIs = 5
	// MaxRedirectURIBytes is one URI's bound (00104).
	MaxRedirectURIBytes = 512
)

// The registration refusals, each a fixed sentence the operator screen shows. None repeats the URI.
var (
	ErrRedirectCount     = errors.New("oauth: a client needs one to five redirect URIs")
	ErrRedirectTooLong   = errors.New("oauth: a redirect URI is longer than 512 bytes")
	ErrRedirectShape     = errors.New("oauth: a redirect URI must be https://<lower-case host name>/<path>, with no port but 443, no IP address, no user, no query and no fragment")
	ErrRedirectLoopback  = errors.New("oauth: a loopback redirect URI is accepted only by the sandbox build")
	ErrRedirectDuplicate = errors.New("oauth: a redirect URI is listed twice")
)

// isLoopbackForm reports whether u is the http://localhost|127.0.0.1:<port>/ form.
func isLoopbackForm(u string) bool {
	return strings.HasPrefix(u, "http://localhost:") || strings.HasPrefix(u, "http://127.0.0.1:")
}

// RegistrationURIError is the operator screen's rule for ONE redirect URI: nil, or the reason.
func RegistrationURIError(u string) error {
	if len(u) > MaxRedirectURIBytes {
		return ErrRedirectTooLong
	}
	if !registryRule.MatchString(u) {
		return ErrRedirectShape
	}
	p, err := url.Parse(u)
	if err != nil || p.User != nil || p.RawQuery != "" || p.ForceQuery || p.Fragment != "" || p.Opaque != "" ||
		!strings.HasPrefix(p.EscapedPath(), "/") {
		return ErrRedirectShape
	}
	host := p.Hostname()
	if isLoopbackForm(u) {
		if !LoopbackRedirects {
			return ErrRedirectLoopback
		}
		port, err := strconv.Atoi(p.Port())
		if err != nil || port < 1 || port > 65535 {
			return ErrRedirectShape
		}
		return nil
	}
	if p.Scheme != "https" || net.ParseIP(host) != nil || (p.Port() != "" && p.Port() != "443") {
		return ErrRedirectShape
	}
	return nil
}

// RegistrationURIsError is the rule for a whole list: one to five, each RegistrationURIError-clean,
// no URI twice.
func RegistrationURIsError(us []string) error {
	if len(us) == 0 || len(us) > MaxRedirectURIs {
		return ErrRedirectCount
	}
	for i, u := range us {
		if err := RegistrationURIError(u); err != nil {
			return err
		}
		if slices.Contains(us[:i], u) {
			return ErrRedirectDuplicate
		}
	}
	return nil
}

// AuthorizeURIAllowed is V2's build gate (ADR 0047 §4.1, §10.4), applied AFTER the byte-for-byte
// match with the client's registered list: in the production binary only an https:// URI is ever a
// redirect target -- whatever the registry row holds, so a loopback row somebody wrote with the
// operator's DSN sends no code anywhere. The sandbox binary also takes the loopback form.
func AuthorizeURIAllowed(u string) bool {
	if strings.HasPrefix(u, "https://") {
		return true
	}
	return LoopbackRedirects && isLoopbackForm(u)
}

// RedirectOrigin is the scheme://host[:port] of a registered redirect URI -- the consent page's
// form-action addition (§4.6, plan A). "" when it does not parse.
func RedirectOrigin(u string) string {
	p, err := url.Parse(u)
	if err != nil || p.Scheme == "" || p.Host == "" {
		return ""
	}
	return p.Scheme + "://" + p.Host
}

// RedirectHost is the host the consent page names ("You'll return to maltacalculator.com").
func RedirectHost(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return p.Hostname()
}

// WithParams is a registered redirect URI with the response's parameters appended -- the registered
// URI carries no query (the rule above), so they open one. Only the four response words are ever
// passed (code, state, iss, error); url.Values sorts and encodes them.
func WithParams(u string, v url.Values) string {
	return u + "?" + v.Encode()
}
