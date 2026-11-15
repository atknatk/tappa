package oauth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------------------- PKCE --

// challengeShape is V5 (ADR 0047 §4.1) and oauth_codes_code_challenge_shape: the base64url of a
// SHA-256, 43 characters.
var challengeShape = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// verifierShape is RFC 7636 §4.1: 43 to 128 characters of the unreserved set.
var verifierShape = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

// MethodS256 is the one code_challenge_method this server accepts (V4: "plain" and a missing method
// -- whose default is "plain" -- are refused).
const MethodS256 = "S256"

// ValidChallenge is V5.
func ValidChallenge(c string) bool { return challengeShape.MatchString(c) }

// ValidVerifier is RFC 7636 §4.1's shape for code_verifier.
func ValidVerifier(v string) bool { return verifierShape.MatchString(v) }

// VerifyS256 reports whether BASE64URL(SHA256(verifier)) is the challenge -- compared in constant
// time (ADR 0047 §5.2 step 5). A verifier of the wrong shape never matches.
func VerifyS256(verifier, challenge string) bool {
	if !ValidVerifier(verifier) || !ValidChallenge(challenge) {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	got := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(got), []byte(challenge)) == 1
}

// --------------------------------------------------------------------------------------- state --

// MaxStateBytes is V7's bound (ADR 0047 §4.1).
const MaxStateBytes = 1024

// ValidState is V7: at most MaxStateBytes, every byte RFC 6749 Appendix A's VSCHAR (%x20-7E). An
// empty state is no state.
func ValidState(s string) bool {
	if len(s) > MaxStateBytes {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

// ------------------------------------------------------------------------------------ the scopes --

// The closed scope set (ADR 0047 §8.2). webhooks:manage is NOT in it in this delivery (§13): the
// registry's CHECK, op_register_oauth_client and this set all refuse it. No implied inclusion:
// employees:write does not include employees:read.
const (
	ScopeTimesheetsRead = "timesheets:read"
	ScopeEmployeesRead  = "employees:read"
	ScopeEmployeesWrite = "employees:write"
	ScopeLocationsWrite = "locations:write"
	ScopePunchesRead    = "punches:read"
)

// KnownScopes is the closed set, sorted (the canonical order every stored scope array takes).
var KnownScopes = []string{ScopeEmployeesRead, ScopeEmployeesWrite, ScopeLocationsWrite, ScopePunchesRead, ScopeTimesheetsRead}

// scopeLabels names each scope in words for the consent page (§4.3) -- what the business allows, not
// the wire word.
var scopeLabels = map[string]string{
	ScopeTimesheetsRead: "Read the hours your employees worked",
	ScopeEmployeesRead:  "See your employees",
	ScopeEmployeesWrite: "Add, change and deactivate your employees, and send their activation invitations",
	ScopeLocationsWrite: "Add and change your locations",
	ScopePunchesRead:    "Read your employees' clock-ins and clock-outs",
}

// ScopeLabel is the consent page's sentence for one scope; "" for a word outside the set.
func ScopeLabel(s string) string { return scopeLabels[s] }

// Known reports whether s is in the closed set.
func Known(s string) bool { _, ok := scopeLabels[s]; return ok }

var (
	// ErrEmptyScope is a scope parameter with no word in it (invalid_scope).
	ErrEmptyScope = errors.New("oauth: the scope is empty")
	// ErrUnknownScope is a word outside the closed set (invalid_scope).
	ErrUnknownScope = errors.New("oauth: a scope is not one this server knows")
)

// ParseScope reads a scope parameter (RFC 6749 §3.3): words separated by spaces, read as a SET --
// order does not matter and a repeated word counts once. The answer is canonical: sorted, unique.
// No word -> ErrEmptyScope; a word outside the closed set -> ErrUnknownScope.
func ParseScope(raw string) ([]string, error) {
	var out []string
	for _, w := range strings.Split(raw, " ") {
		if w == "" {
			continue
		}
		if !Known(w) {
			return nil, ErrUnknownScope
		}
		if !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		return nil, ErrEmptyScope
	}
	slices.Sort(out)
	return out, nil
}

// SubsetOf reports whether every word of s is in of.
func SubsetOf(s, of []string) bool {
	for _, w := range s {
		if !slices.Contains(of, w) {
			return false
		}
	}
	return true
}

// Canonical is s sorted and without repeats (a copy).
func Canonical(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return slices.Compact(out)
}

// ScopeString is the space-separated form of a canonical set -- the token response's scope (§5.4).
func ScopeString(s []string) string { return strings.Join(Canonical(s), " ") }

// -------------------------------------------------------------------------------- the client id --

// clientIDPrefix is the public client_id's prefix (ADR 0047 §7.1): app_ + the registry row's uuid.
const clientIDPrefix = "app_"

// ClientIDOf is the public client_id of a registry id.
func ClientIDOf(id uuid.UUID) string { return clientIDPrefix + id.String() }

// ParseClientID reads a client_id: app_ and a uuid in its canonical lower-case hyphenated form, and
// nothing else -- a second spelling of one client would be a second string to match.
func ParseClientID(s string) (uuid.UUID, bool) {
	if !strings.HasPrefix(s, clientIDPrefix) || len(s) != len(clientIDPrefix)+36 {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(s[len(clientIDPrefix):])
	if err != nil || id.String() != s[len(clientIDPrefix):] {
		return uuid.Nil, false
	}
	return id, true
}

// ---------------------------------------------------------------------------- HTTP Basic (§5.1) --

// ParseBasic reads `Authorization: Basic base64(client_id:client_secret)` (RFC 6749 §2.3.1): the
// scheme in any case, one space, standard base64 of the two values joined by the first colon, each
// form-decoded (ours never need it -- both are base64url -- but a conforming client may encode). The
// answer is the client's registry id and the secret as a Value; false for anything else. The header
// is never echoed.
func ParseBasic(header string) (uuid.UUID, Value, bool) {
	const scheme = "basic "
	if len(header) <= len(scheme) || !strings.EqualFold(header[:len(scheme)], scheme) {
		return uuid.Nil, Value{}, false
	}
	raw, err := base64.StdEncoding.DecodeString(header[len(scheme):])
	if err != nil {
		return uuid.Nil, Value{}, false
	}
	user, pass, ok := strings.Cut(string(raw), ":")
	if !ok {
		return uuid.Nil, Value{}, false
	}
	if user, err = url.QueryUnescape(user); err != nil {
		return uuid.Nil, Value{}, false
	}
	if pass, err = url.QueryUnescape(pass); err != nil {
		return uuid.Nil, Value{}, false
	}
	id, ok := ParseClientID(user)
	if !ok || pass == "" {
		return uuid.Nil, Value{}, false
	}
	return id, ParseValue(pass), true
}
