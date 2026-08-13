package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/signup"
)

// HOW A HALF-FINISHED REGISTRATION TRAVELS BETWEEN THE WIZARD'S STEPS.
//
// 🔴 THE PATTERN IS NOT INVENTED HERE. internal/handler/logincontext.go already
// carries a server-signed, time-limited, browser-bound value between two steps of a
// flow (the verified-candidate set of the tenant picker), and internal/handler/
// tapcontext.go does the same between GET /t and POST /api/checkin. This is the
// third application and it deliberately reuses their shape rather than growing a
// fourth: base64url(payload) "." base64url(mac), in an HttpOnly cookie, with the
// MAC bound to a random value that never enters a page, a version prefix, a length
// prefix in the MAC input and a TTL.
//
// THE ALTERNATIVE — A `signup_drafts` TABLE — WAS COSTED RATHER THAN WAVED AWAY,
// because it is the shape somebody will suggest:
//
//	WHAT IT COSTS  A new table means CLAUDE.md §6's five (tenant_id NOT NULL,
//	               ENABLE + FORCE RLS, a policy, an index, a GRANT) on a row that has
//	               NO TENANT — the tenant is what the flow is about to create — so
//	               the one column §6 requires cannot be filled and the isolation
//	               model has nothing to scope on. It also puts an INSERT reachable by
//	               an ANONYMOUS caller in front of every step, which is the M5-02
//	               lesson exactly ("an append-only table reachable by an anonymous
//	               caller is a denial-of-service against the trail"), and it needs an
//	               abandoned-draft sweeper, i.e. a scheduled DELETE in a product
//	               whose §4.6 rule is that rows do not get deleted.
//	WHAT IT BUYS   Nothing this flow needs. There is no server-side state worth
//	               protecting here: the payload is what the visitor typed, they can
//	               see it on the screen it came from, and forging it forges only
//	               their own registration.
//
// ⚠️ AND WHAT THE SIGNATURE IS *FOR*, SINCE THE PARAGRAPH ABOVE MIGHT READ AS "SO
// WHY SIGN IT AT ALL". Two things, both concrete. It stops a caller SKIPPING A STEP
// — the final POST is the one that runs bcrypt and writes rows, and without a valid
// signed state carrying steps one and two it cannot be reached at all, which is a
// third of this flow's bot defence (see signupratelimit.go). And it stops the
// VIES ANSWER being chosen by the caller: vat_verified is written from a value this
// server obtained, not from a form field, so nobody can register with a VAT number
// stamped "verified by the European Commission" that the Commission never saw.
//
// WHAT IS DELIBERATELY *NOT* IN THE PAYLOAD:
//
//   - THE PASSWORD. It is collected on the LAST step and used in the same request.
//     A password in a cookie — even a signed, HttpOnly one — is a password at rest
//     in a browser, and the whole reason the account step is last is so that it
//     never has to travel.
//   - THE OPERATOR'S NAME AND EMAIL, for the smaller version of the same reason:
//     they are on the last step too, so they never need to survive a redirect.
//
// So the signed value carries a company name, a VAT number, a business type, a
// structure, a venue list and one VIES verdict — the things the visitor typed into
// steps one and two, which are already on their screen.

const (
	// signupCookieName carries "<csrf>.<bind>": the synchronizer token every wizard
	// form echoes, and the value the state blob's MAC is bound to.
	//
	// TWO INDEPENDENT RANDOM VALUES, NOT ONE DERIVED FROM THE OTHER — the rule
	// cookies.go states from the activation flow's side and logincontext.go repeats:
	// an attacker who can plant a cookie knows their own value, so a token DERIVED
	// from the cookie is a token they can compute. The csrf half is rendered into
	// the page and the bind half never is.
	signupCookieName = "tappa_signup"

	// signupStateCookieName carries "<payload>.<mac>": steps one and two.
	//
	// SEPARATE FROM THE SYNCHRONIZER COOKIE so that clearing one never disturbs the
	// other, and so the first page load — which has a token but no answers yet —
	// sets only one.
	signupStateCookieName = "tappa_signup_state"

	// signupCookiePath scopes both cookies to the wizard.
	//
	// 🔴 IT IS **NOT** "/" AND THAT IS THE POINT. The panel's cookies are
	// Path=/admin (adminauth.CookiePath) precisely so they never reach the tap
	// surface; these are Path=/signup for the mirror reason — a half-finished
	// registration must not accompany a tap, a panel request or a marketing page
	// view. It also means the landing page stays byte-identical for every visitor
	// and therefore stays cacheable (handler.Marketing's whole premise), because no
	// request to / carries one of these.
	signupCookiePath = "/signup"

	// signupTTL bounds how long a half-finished registration may be presented.
	//
	// THIRTY MINUTES. It is NOT a credential lifetime — the value authorises
	// nothing, unlike the login choice blob, which completes an authentication and
	// therefore lives five — it is a FORM lifetime. The task on the other side is a
	// person finding their VAT number on a letterhead and typing the names of their
	// branches, which is minutes of work with interruptions in it. Shorter would
	// make "I went to look for the VAT certificate" mean starting again.
	signupTTL = 30 * time.Minute

	// signupCookieMaxAge is the browser-side twin of signupTTL, DERIVED rather than
	// written a second time. logincontext.go records what a second literal cost
	// there: two numbers held together by prose, which drifted.
	signupCookieMaxAge = int(signupTTL / time.Second)

	// signupFutureSkew tolerates a blob that appears to come from the future. Both
	// stamps are read from THIS process's clock and the value is authenticated, so
	// the only way here is the clock stepping backwards (an NTP correction, a
	// suspended VM). Beyond the tolerance the blob is treated as expired rather than
	// believed — the fail-closed direction.
	//
	// ⚠️ IT WIDENS THE ACCEPTANCE BAND: the honest window is signupTTL + this, i.e.
	// 31 minutes, not 30. logincontext.go had to correct exactly this sentence after
	// an audit measured its band, so it is written correctly the first time here.
	signupFutureSkew = time.Minute

	// signupStateVersion prefixes the payload; parse refuses any other value, so a
	// payload written under a DIFFERENT layout is rejected rather than misread. That
	// holds PROVIDED whoever changes the layout also bumps this — discipline, not
	// enforcement, exactly as tapcontext.go and logincontext.go say of theirs.
	signupStateVersion = 1

	// signupMACLabel is prefixed to the MAC input. Domain separation: this
	// deployment now derives FIVE things from HMAC-SHA256 (session token hashes,
	// invite code hashes, tap contexts, the login choice, and this), and without a
	// label a value minted for one could be presented to another.
	signupMACLabel = "tappa/signup-state/v1|"

	// signupKeyLabel derives this value's signing key from the session HMAC key, for
	// the reason tapContextKeyLabel and adminChoiceKeyLabel both give: nothing here
	// is STORED, so a new required environment variable would mean every deployment
	// and CI refusing to boot until an operator sets a key whose only job is to sign
	// a thirty-minute value. HMAC is one-way, so holding this key reveals nothing
	// about TAPPA_SESSION_HMAC_KEY; the implication runs the other way and that is
	// the stated limit.
	//
	// ⚠️ WHAT A LEAKED TAPPA_SESSION_HMAC_KEY COSTS *HERE*, since logincontext.go
	// had to add exactly this paragraph after an audit: a forger could sign a state
	// blob and thereby (a) skip the two form steps and (b) choose the vat_verified
	// value written for their own new tenant. Both are things they could achieve
	// anyway by filling the form in — the blob authorises no ACCESS to anything
	// existing, and there is no tenant in it. This is the WEAKEST of the five
	// derivations, which is why it can share a key.
	signupKeyLabel = "tappa/signup-state/v1/key-derivation"

	// signupMaxStateBytes bounds a hostile cookie before it is decoded.
	//
	// It is a PARSER bound, the same job adminChoiceMaxEntries does: it keeps a
	// cookie the server did not write from making this process allocate, and it
	// keeps a cookie the server DID write inside every browser's 4 KB per-cookie
	// limit. The payload's own contents are separately bounded by the domain's field
	// limits (signup.MaxCompanyNameRunes, MaxVenues x MaxVenueNameRunes, …), which
	// is where the arithmetic lives; this is the backstop for a value that never
	// went through them.
	signupMaxStateBytes = 4096
)

// Failure modes, kept apart so the handler can tell an expired wizard (say "start
// again" — honest and actionable) from a value that was tampered with or was never
// ours (a generic refusal and a log line). The VISITOR is told the same thing
// either way; this distinction is for us.
var (
	errSignupMalformed = errors.New("handler: signup state is malformed")
	errSignupSignature = errors.New("handler: signup state signature does not match")
	errSignupExpired   = errors.New("handler: signup state has expired")
)

// signupLoginState is what the synchronizer cookie carries. NEITHER HALF IS A §4.7
// SECRET: the csrf half is rendered into the page on purpose — that is the whole
// mechanism — and the bind half is 32 bytes of randomness that identifies nothing
// and outlives nothing.
type signupLoginState struct {
	csrf string
	bind string
}

// csrfMatches compares the token the form sent with the one in the cookie, in
// constant time (redline R7: a comparison that decides a security outcome is never
// == or bytes.Equal). ConstantTimeCompare returns 0 for differing lengths, so an
// absent form field can never match.
func (st signupLoginState) csrfMatches(sent string) bool {
	if st.csrf == "" || sent == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(st.csrf), []byte(sent)) == 1
}

// signupDraft is the AUTHENTICATED payload: steps one and two, plus the two stamps
// the bot defence reads.
//
// THE JSON TAGS ARE SHORT because this travels in a cookie and a browser's 4 KB
// limit is shared with everything else the site sets on this path.
type signupDraft struct {
	Version int   `json:"v"`
	Issued  int64 `json:"t"`

	Name         string `json:"n"`
	VATNumber    string `json:"vat"`
	BusinessType string `json:"bt"`
	Structure    string `json:"st"`
	// VATCheck is what VIES said, as its stable string form. It is IN THE SIGNED
	// PAYLOAD rather than in a form field because the whole value of the check is
	// that the caller did not choose the answer.
	VATCheck string `json:"vc"`

	Venues []string `json:"loc,omitempty"`

	// Blocked is set only on the CONFIRMATION state (signupDoneStep): the account
	// that was just created cannot be reached by the sign-in path. It is carried
	// rather than recomputed because the measurement was taken once, at the moment of
	// the write, by the code that did the writing.
	Blocked bool `json:"b,omitempty"`

	// Step is the highest step this state has completed: 1 after the business step,
	// 2 after the venues step, signupDoneStep once the business EXISTS. A caller
	// cannot jump to the account form without having produced a state that says 2,
	// and cannot produce one without posting step two, which itself needs a state
	// that says 1.
	Step int `json:"s"`
}

// signupState mints and parses the signed draft.
//
// The key is derived once, at construction, and held privately, so a later mutation
// of the caller's config slice cannot silently change how blobs sign.
type signupState struct {
	key []byte
	ttl time.Duration
	// now is injectable for tests; production leaves it nil and reads the wall
	// clock.
	now func() time.Time
}

// newSignupState derives the signing key from the session HMAC key. It refuses a
// wrong-sized key rather than degrading: a short or empty key still produces
// plausible-looking output, so the failure would stay invisible until somebody
// noticed blobs were forgeable.
func newSignupState(cfg *config.Config) (signupState, error) {
	if cfg == nil {
		return signupState{}, errors.New("handler: nil config")
	}
	if len(cfg.SessionHMACKey) != 32 {
		return signupState{}, fmt.Errorf("handler: session hmac key must be 32 bytes, got %d",
			len(cfg.SessionHMACKey))
	}
	m := hmac.New(sha256.New, cfg.SessionHMACKey)
	_, _ = m.Write([]byte(signupKeyLabel)) // hash writes never fail (documented)
	return signupState{key: m.Sum(nil), ttl: signupTTL}, nil
}

func (s signupState) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// sign computes the MAC over the label, the payload and the BINDING value.
//
// 🔴 THE LENGTH PREFIX IS NOT DECORATION — IT REMOVES A CONCATENATION AMBIGUITY,
// and logincontext.go had to add it after an audit walked every separator boundary.
// Without it the MAC input is `label || payload || "|" || bind` with the attacker
// controlling both halves, so one byte string could split into a DIFFERENT
// (payload, bind) pair. Writing the payload's byte length first makes the encoding
// injective whatever shape the payload later takes — and this payload is JSON, i.e.
// a shape that grows fields, which is exactly the case that argument was about.
func (s signupState) sign(payload []byte, bind string) []byte {
	m := hmac.New(sha256.New, s.key)
	_, _ = m.Write([]byte(signupMACLabel))
	_, _ = m.Write([]byte(strconv.Itoa(len(payload))))
	_, _ = m.Write([]byte("|"))
	_, _ = m.Write(payload)
	_, _ = m.Write([]byte("|"))
	_, _ = m.Write([]byte(bind))
	return m.Sum(nil)
}

// mint stamps a draft with the server clock and returns the cookie value.
//
// THE ISSUED STAMP IS **NOT** REFRESHED BY LATER STEPS, and that is load-bearing
// twice over. It is what makes signupTTL a bound on the WHOLE wizard rather than on
// each step (otherwise a caller could hold a session open indefinitely by posting a
// step every twenty-nine minutes), and it is what the minimum-fill-time gate reads
// (signupratelimit.go). mint therefore takes the stamp from the CALLER for every
// step after the first.
func (s signupState) mint(d signupDraft, bind string) (string, error) {
	if len(s.key) == 0 {
		// A zero signupState would sign under an empty key — a MAC anybody can
		// compute. Refuse rather than emit a decorative signature.
		return "", errors.New("handler: signup state was not configured")
	}
	if bind == "" {
		// Binding to nothing would produce a blob any browser could spend, which is
		// the one property this signature exists to deny.
		return "", errors.New("handler: refusing to sign a signup state with no binding")
	}
	d.Version = signupStateVersion
	if d.Issued == 0 {
		d.Issued = s.clock().Truncate(time.Second).Unix()
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return "", fmt.Errorf("handler: encoding the signup state: %w", err)
	}
	if len(raw) > signupMaxStateBytes {
		// Unreachable through the wizard, whose fields are all bounded by
		// internal/domain/signup. Refusing rather than emitting an oversized cookie
		// means a browser silently dropping it can never be the first sign.
		return "", fmt.Errorf("handler: signup state is %d bytes, max %d", len(raw), signupMaxStateBytes)
	}
	return base64.RawURLEncoding.EncodeToString(raw) + "." +
		base64.RawURLEncoding.EncodeToString(s.sign(raw, bind)), nil
}

// parse verifies a blob against bind and returns the draft it carries.
//
// ORDER: shape, then SIGNATURE, then expiry, then field decoding. The signature
// comes before anything that interprets the payload, so no field of an
// unauthenticated value is ever acted on — and expiry comes before decoding for the
// same reason in miniature. This is tapcontext.go's and logincontext.go's order,
// kept deliberately identical so the three are read as one pattern.
func (s signupState) parse(v, bind string) (signupDraft, error) {
	if len(s.key) == 0 {
		return signupDraft{}, errors.New("handler: signup state was not configured")
	}
	if bind == "" {
		return signupDraft{}, errSignupSignature
	}
	encoded, sig, ok := strings.Cut(v, ".")
	if !ok || encoded == "" || sig == "" {
		return signupDraft{}, errSignupMalformed
	}
	// The bound is applied to the ENCODED form, before any decoding allocates.
	// base64 is 4/3 of the raw size, so this is the raw bound with room.
	if len(encoded) > signupMaxStateBytes*2 {
		return signupDraft{}, errSignupMalformed
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return signupDraft{}, errSignupMalformed
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return signupDraft{}, errSignupMalformed
	}
	// Constant time, never == or bytes.Equal (redline R7). ConstantTimeCompare
	// returns 0 for differing lengths, so a truncated signature cannot match.
	if subtle.ConstantTimeCompare(got, s.sign(raw, bind)) != 1 {
		return signupDraft{}, errSignupSignature
	}

	var d signupDraft
	if err := json.Unmarshal(raw, &d); err != nil {
		// Authenticated but unreadable: minted by this deployment under a different
		// layout. Treated as malformed, never repaired.
		return signupDraft{}, errSignupMalformed
	}
	if d.Version != signupStateVersion {
		return signupDraft{}, errSignupMalformed
	}
	issuedAt := time.Unix(d.Issued, 0).UTC()
	now := s.clock()
	if now.Sub(issuedAt) > s.ttl || issuedAt.Sub(now) > signupFutureSkew {
		return signupDraft{}, errSignupExpired
	}
	if d.Step < 1 || d.Step > signupDoneStep {
		return signupDraft{}, errSignupMalformed
	}
	if len(d.Venues) > signup.MaxVenues {
		// A bound the domain also applies. It is repeated HERE because this is the
		// parser: the domain's bound protects a WRITE, and this one protects the
		// allocation that happens before any domain code runs.
		return signupDraft{}, errSignupMalformed
	}
	return d, nil
}

// age reports how long ago this draft was first minted. The minimum-fill-time gate
// reads it; see signupratelimit.go for what that buys and what it does not.
func (s signupState) age(d signupDraft) time.Duration {
	return s.clock().Sub(time.Unix(d.Issued, 0))
}

// business rebuilds the domain value from the authenticated payload.
func (d signupDraft) business() signup.Business {
	return signup.Business{
		Name:         d.Name,
		VATNumber:    d.VATNumber,
		BusinessType: d.BusinessType,
		Structure:    d.Structure,
		VAT:          vatStatusOf(d.VATCheck),
	}
}

// venues rebuilds the venue list from the authenticated payload.
func (d signupDraft) venues() []signup.Venue {
	out := make([]signup.Venue, 0, len(d.Venues))
	for _, n := range d.Venues {
		out = append(out, signup.Venue{Name: n})
	}
	return out
}

// vatStatusOf maps the stored string back to the domain's three-state value.
//
// AN UNKNOWN STRING BECOMES VATUnknown, WHICH IS THE FAIL-SAFE DIRECTION IN BOTH
// SENSES: it never claims the European Commission confirmed something it did not,
// and it never accuses a customer's number of being invalid. The payload is
// authenticated, so this can only be reached by a deployment whose layout changed.
func vatStatusOf(s string) signup.VATStatus {
	switch s {
	case signup.VATValid.String():
		return signup.VATValid
	case signup.VATInvalid.String():
		return signup.VATInvalid
	default:
		return signup.VATUnknown
	}
}

// signupCookies writes, reads and clears the two wizard cookies.
//
// THE FIELD'S POLARITY IS THE SECURITY MECHANISM — `insecure`, not `secure`, so the
// ZERO VALUE MEANS SECURE. Fifth application of the M5-01 round-3 finding in this
// repo; the reasoning is in internal/session/cookie.go and is not repeated.
type signupCookies struct {
	insecure bool
}

func newSignupCookies(cfg *config.Config) signupCookies {
	if cfg == nil || cfg.IsProd() {
		return signupCookies{}
	}
	// Case-insensitive: URL schemes are (RFC 3986 3.1), and M5-01 shipped a bug
	// where "HTTPS://" bought a weaker cookie through a case-sensitive test.
	return signupCookies{insecure: !strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://")}
}

func (c signupCookies) secure() bool { return !c.insecure }

func (c signupCookies) set(w http.ResponseWriter, name, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     signupCookiePath,
		MaxAge:   signupCookieMaxAge,
		HttpOnly: true,
		Secure:   c.secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

// clear expires a cookie. Attributes must match set exactly or the browser keeps
// the original.
func (c signupCookies) clear(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     signupCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

func (c signupCookies) setLogin(w http.ResponseWriter, st signupLoginState) {
	c.set(w, signupCookieName, st.csrf+"."+st.bind)
}

// readLogin lifts the synchronizer state off a request. The second result is false
// when the cookie is absent, empty or malformed.
//
// A cookie that does not split into exactly two non-empty parts is treated as
// ABSENT rather than repaired: a value in that shape was not written by setLogin,
// and guessing at what half of it means is how a parser becomes an attack surface.
func (c signupCookies) readLogin(r *http.Request) (signupLoginState, bool) {
	ck, err := r.Cookie(signupCookieName)
	if err != nil || ck.Value == "" {
		return signupLoginState{}, false
	}
	csrf, bind, ok := strings.Cut(ck.Value, ".")
	if !ok || csrf == "" || bind == "" {
		return signupLoginState{}, false
	}
	return signupLoginState{csrf: csrf, bind: bind}, true
}

// newSignupLoginState mints a fresh pair of independent random values.
func newSignupLoginState() (signupLoginState, error) {
	csrf, err := newCSRFToken()
	if err != nil {
		return signupLoginState{}, err
	}
	bind, err := newCSRFToken()
	if err != nil {
		return signupLoginState{}, err
	}
	return signupLoginState{csrf: csrf, bind: bind}, nil
}
