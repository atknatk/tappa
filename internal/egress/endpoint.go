package egress

import (
	"encoding"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"strings"
)

// THE URL RULE (ADR 0028 §6): only https, port 443, no IP literal, no userinfo, no query string, no
// fragment. It is the first fence, not the defence -- the defence is the dial-time gate, which never
// looks at the URL (a public name may resolve to a private address). The rule exists so that what an
// owner types can carry nothing a log or an error might repeat (a password, a token in a query) and
// names one host the gate then judges.
//
// 🔴 AN ENDPOINT IS NEVER LOGGED (ADR 0028 §3, §6; CLAUDE.md §7). The address an owner types is their
// integration's private address -- a path may carry a token. Endpoint prints a placeholder through
// every rendering path, the way apikey.Token does, and the errors below are fixed sentinels that
// never repeat the input. The ONE place the address is shown is the owner's own panel
// (ShowToTheOwner).

// The refusals of ParseEndpoint. Each is a fixed sentence: none carries the address.
var (
	ErrNotHTTPS    = errors.New("egress: the address must start with https://")
	ErrPort        = errors.New("egress: the address must use port 443 (write no port, or :443)")
	ErrIPLiteral   = errors.New("egress: the address must name a host, not an IP address")
	ErrUserinfo    = errors.New("egress: the address must carry no user name or password")
	ErrQuery       = errors.New("egress: the address must carry no query string (no ?)")
	ErrFragment    = errors.New("egress: the address must carry no fragment (no #)")
	ErrHost        = errors.New("egress: the host must be a plain DNS name such as hooks.example.com")
	ErrCharacters  = errors.New("egress: the address holds a space, a control character or a non-ASCII character")
	ErrTooLong     = errors.New("egress: the address is longer than 2048 characters")
	ErrNotAnswered = errors.New("egress: no address was given")
)

// MaxEndpointLength is webhook_endpoints_url_shape's bound (migration 00048).
const MaxEndpointLength = 2048

// refusedSuffixes are top-level labels that are never on the public internet (RFC 6761/6762/8375/7686 and the
// Kubernetes cluster domain). A belt only: the dial-time gate refuses what such a name resolves to;
// refusing the name says so to the owner before anything is resolved.
var refusedSuffixes = []string{"localhost", "local", "internal", "arpa", "invalid", "onion", "test"}

// Endpoint is a delivery address that passed the URL rule, in its one canonical spelling
// ("https://" + lower-case host + escaped path, the default port dropped). The field is a pointer for
// apikey.Token's reason: fmt reaches an unexported field by reflection, and a pointer prints an
// address there, never the text.
type Endpoint struct{ v *string }

// Compile-time proof that every redaction interface is implemented.
var (
	_ fmt.Formatter          = Endpoint{}
	_ fmt.Stringer           = Endpoint{}
	_ fmt.GoStringer         = Endpoint{}
	_ slog.LogValuer         = Endpoint{}
	_ encoding.TextMarshaler = Endpoint{}
)

const redactedEndpoint = "egress.Endpoint(redacted)"

// IsZero reports whether e is the zero Endpoint (no address).
func (e Endpoint) IsZero() bool { return e.v == nil }

func (e Endpoint) reveal() string {
	if e.v == nil {
		return ""
	}
	return *e.v
}

// ShowToTheOwner is the canonical address, for the one place it is printed: the owner's panel, which
// shows the owner the address they typed. Every other path renders the placeholder.
func (e Endpoint) ShowToTheOwner() string { return e.reveal() }

// StoredForm is the canonical address, for the one place it is written: webhook_endpoints.url.
func (e Endpoint) StoredForm() string { return e.reveal() }

// Format implements fmt.Formatter for every verb.
func (Endpoint) Format(f fmt.State, _ rune) { _, _ = f.Write([]byte(redactedEndpoint)) }

// String implements fmt.Stringer.
func (Endpoint) String() string { return redactedEndpoint }

// GoString covers %#v.
func (Endpoint) GoString() string { return redactedEndpoint }

// LogValue implements slog.LogValuer.
func (Endpoint) LogValue() slog.Value { return slog.StringValue(redactedEndpoint) }

// MarshalText implements encoding.TextMarshaler, which encoding/json prefers.
func (Endpoint) MarshalText() ([]byte, error) { return []byte(redactedEndpoint), nil }

// ParseEndpoint applies the URL rule and returns the address in its canonical spelling. A stored
// address is read back through it as well (EndpointFromStore), so a row written before a rule
// tightened is judged by today's rule before anything is sent to it.
func ParseEndpoint(raw string) (Endpoint, error) {
	if raw == "" {
		return Endpoint{}, ErrNotAnswered
	}
	if len(raw) > MaxEndpointLength {
		return Endpoint{}, ErrTooLong
	}
	for i := 0; i < len(raw); i++ {
		// Printable ASCII only: a space, a control character (CR/LF splitting a header, a NUL) or a
		// raw non-ASCII byte (an IDN typed as Unicode) is refused before the parser sees it.
		if c := raw[i]; c <= 0x20 || c >= 0x7f {
			return Endpoint{}, ErrCharacters
		}
	}
	// One spelling of the scheme: the parser lower-cases it, so "HTTPS://" is refused here rather
	// than silently becoming https.
	if !strings.HasPrefix(raw, "https://") {
		return Endpoint{}, ErrNotHTTPS
	}
	// A query or a fragment is refused even when empty ("https://h/?", "https://h/#"): the parser
	// would drop the empty one, and two spellings of one address is what this rule exists to stop.
	if strings.Contains(raw, "?") {
		return Endpoint{}, ErrQuery
	}
	if strings.Contains(raw, "#") {
		return Endpoint{}, ErrFragment
	}
	u, err := url.Parse(raw)
	if err != nil {
		// net/url's error quotes the input; it is classified, never returned.
		return Endpoint{}, ErrHost
	}
	if u.Scheme != "https" || u.Opaque != "" {
		return Endpoint{}, ErrNotHTTPS
	}
	if u.User != nil || strings.Contains(u.Host, "@") {
		return Endpoint{}, ErrUserinfo
	}
	if u.RawQuery != "" || u.ForceQuery {
		return Endpoint{}, ErrQuery
	}
	if u.Fragment != "" || u.RawFragment != "" {
		return Endpoint{}, ErrFragment
	}
	host, port := u.Hostname(), u.Port()
	if strings.HasPrefix(u.Host, "[") {
		// A bracketed host is an IPv6 literal (or a zone-carrying one) -- never a DNS name.
		return Endpoint{}, ErrIPLiteral
	}
	if port != "" && port != "443" {
		return Endpoint{}, ErrPort
	}
	if strings.HasSuffix(u.Host, ":") {
		// "https://h:/" -- an empty port is a second spelling of the default one.
		return Endpoint{}, ErrPort
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return Endpoint{}, ErrIPLiteral
	}
	host = strings.ToLower(host)
	if err := dnsName(host); err != nil {
		return Endpoint{}, err
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	canonical := "https://" + host + path
	if len(canonical) > MaxEndpointLength {
		return Endpoint{}, ErrTooLong
	}
	return Endpoint{v: &canonical}, nil
}

// EndpointFromStore reads webhook_endpoints.url back through the rule. A row the rule refuses today
// is not sent to (the caller treats it as "not deliverable"), never sent to on the strength of
// yesterday's rule.
func EndpointFromStore(stored string) (Endpoint, error) { return ParseEndpoint(stored) }

// dnsName is the host rule: two or more labels of [a-z0-9-], each 1 to 63 characters, neither
// starting nor ending with a hyphen, 253 characters at most, no empty label (a trailing dot is
// refused -- a second spelling), no punycode label (an IDN is refused in both of its spellings: the
// Unicode one is not ASCII, the xn-- one is caught here), a top-level label that is not all digits
// (so "127.1", "0x7f.1" and their kind are never a "name") and none of the never-public suffixes.
func dnsName(host string) error {
	if host == "" || len(host) > 253 {
		return ErrHost
	}
	labels := strings.Split(host, ".")
	if len(labels) < 2 {
		return ErrHost
	}
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return ErrHost
		}
		if strings.HasPrefix(l, "xn--") {
			return ErrHost
		}
		for i := 0; i < len(l); i++ {
			c := l[i]
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return ErrHost
			}
		}
	}
	tld := labels[len(labels)-1]
	allDigits := true
	for i := 0; i < len(tld); i++ {
		if tld[i] < '0' || tld[i] > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		return ErrIPLiteral
	}
	// A hexadecimal "label" like 0x7f is a number to some resolvers (inet_aton): refuse a host whose
	// every label is digits or 0x-hex.
	numeric := true
	for _, l := range labels {
		if !isNumericLabel(l) {
			numeric = false
			break
		}
	}
	if numeric {
		return ErrIPLiteral
	}
	for _, s := range refusedSuffixes {
		if tld == s {
			return ErrHost
		}
	}
	return nil
}

func isNumericLabel(l string) bool {
	s := l
	if strings.HasPrefix(s, "0x") {
		s = s[2:]
		if s == "" {
			return true
		}
		for i := 0; i < len(s); i++ {
			c := s[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				return false
			}
		}
		return true
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
