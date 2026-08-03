package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
)

// THE CENTRAL DESIGN DECISION OF M6-01 PHASE B: how the VERIFIED CANDIDATE SET
// travels from step 1 (email + password) to step 2 (which business?).
//
// Migration 00011's PHASE B OBLIGATION 5 lives entirely in this question. The
// picker posts a tenant id; the server must be able to prove, at that moment, that
// the tenant is one whose password ACTUALLY VERIFIED — not merely one the email
// resolves to. Get that wrong and an attacker who plants the victim's address in
// their own tenant, authenticates against their OWN row, and then chooses the
// victim's business gets a session in the victim's tenant. 00011 measured that
// attack end to end.
//
// FOUR MECHANISMS WERE ON THE TABLE. All four were weighed against this repo's own
// precedents rather than invented:
//
//	(a) A SERVER-SIGNED, TIME-LIMITED CONTEXT — the shape internal/handler/
//	    tapcontext.go already uses for GET /t -> POST /api/checkin. CHOSEN.
//	(b) A SERVER-SIDE PENDING-LOGIN ROW. Strictly the strongest — the set never
//	    leaves the server, and it can be single-use by DELETE. REJECTED: it needs a
//	    table, i.e. a migration, and this task was scoped "the schema is ready; no
//	    new migration is expected". It also puts a write on the failure path of an
//	    unauthenticated endpoint, which is the M5-02 lesson (an append-only table
//	    reachable by an anonymous caller is a denial-of-service against the trail).
//	    Worth revisiting only if a pending login ever needs to survive a restart,
//	    which it does not: the remedy for a lost picker is to log in again.
//	(c) ASK FOR THE PASSWORD AGAIN AT STEP 2. REJECTED on measurement: it doubles
//	    the bcrypt cost of every multi-business login (+~380 ms, adminauth.Cost),
//	    gives an attacker a second free comparison per attempt against the same
//	    budget, and — the part that decides it — it does not actually bind anything
//	    unless step 2 re-runs the whole resolution and re-derives the verified set,
//	    at which point step 2 IS step 1 and the picker is decoration.
//	(d) SKIP THE PICKER WHEN THERE IS EXACTLY ONE VERIFIED CANDIDATE. ADOPTED, as
//	    well as (a) rather than instead of it. It is not a binding mechanism; it is
//	    what keeps the binding mechanism off the ordinary path entirely. Every login
//	    in the product today resolves one business, so this blob is minted only in
//	    the case it exists for.
//
// WHY (a) IS SOUND HERE, stated as a property and not as a hope: the payload is
// the verified set, the MAC is over that payload, and POST /admin/login/choose
// accepts a tenant ONLY if it appears in the authenticated payload. A client
// cannot add a tenant without forging HMAC-SHA256 under a key it does not have.
// The claim the design does NOT make is that the blob is unforgeable-in-place:
// whoever holds it can spend it, which is why it is bound (below), short-lived and
// HttpOnly.
//
// WHERE IT LIVES: IN AN HttpOnly COOKIE, NOT IN A FORM FIELD — and that is the one
// place this design departs from tapcontext.go. Three gains, all concrete:
//
//  1. POST /admin/login can answer 303 instead of rendering. A rendered POST means
//     a refresh re-submits the PASSWORD; with the redirect, the picker is a plain
//     GET that can be refreshed all day.
//  2. The set never appears in the HTML, so it is not in "save page", not in the
//     bfcache-rendered DOM, and not readable by script.
//  3. It never appears in a URL, so it is not in history, the tab title or a
//     Referer.
//
// WHAT IT IS BOUND TO. The MAC covers the payload AND the `bind` half of the login
// cookie (adminLoginCookieName), which is 32 bytes of randomness that never enters
// any page. So a blob minted for one browser does not verify in another — the same
// property tapcontext.go gets by binding to the session id, obtained the same way
// (additional authenticated data that is never disclosed).
//
// ⚠️ WHAT THE BINDING DOES NOT BUY, so nobody reads more into it. An attacker who
// completes step 1 in their OWN browser holds a valid (login cookie, blob) PAIR and
// can plant BOTH in a victim's browser. That is login CSRF, it ends with the victim
// signed in as the ATTACKER, and the binding is not what stops it — the strict
// Origin check on POST /admin/login/choose is. The binding closes a different
// thing: a blob lifted from one browser (a shared machine, a copied profile)
// cannot be replayed in another without the matching login cookie.
//
// 🔴 AND WHAT IT CANNOT BUY AT ALL, named because it is the honest residual: this
// blob is a BEARER CREDENTIAL for the accounts inside it. Anyone holding both
// cookies can finish the login without the password — AS MANY TIMES AS THEY LIKE
// within the five minutes, not once.
//
// ⚠️ IT IS NOT SINGLE-USE, and an earlier version of this paragraph said "can
// finish the login" in the singular, which reads as if it were. MEASURED: after a
// completed sign-in, putting the two cookies back and posting a DIFFERENT business
// from the same signed set answers 303 and the session count goes 1 -> 2. What that
// buys an attacker is bounded by the thing that actually matters and is unchanged:
// the set never widens, so every extra session is in a business whose password
// ALREADY verified in this same attempt. Two sessions in businesses you just
// authenticated for is not an escalation — it is the ordinary "I picked the wrong
// one, let me go back" — which is why the fix here is the sentence and not a
// mechanism.
//
// MAKING IT SINGLE-USE WAS COSTED RATHER THAN WAVED AWAY: it needs server-side
// state the blob was designed to avoid (a table, i.e. a migration this task was
// scoped out of; or an in-process seen-set with TTL eviction, which is lost on
// restart and wrong the moment there are two app processes). It would buy nothing
// against a WIDER set — the HMAC and selectVerified own that — so it would be
// mechanism spent on a non-escalation. Written down so the next reader does not
// mistake the TTL for a use count.
//
// It is strictly narrower than the password (one browser, one window, and only the
// accounts whose hash already verified — never a wider set, which is the whole
// obligation) but it is not nothing, and it is why the TTL is minutes rather than
// the fifteen tapcontext.go can afford.

const (
	// adminLoginCookieName carries "<csrf>.<bind>": the synchronizer token echoed
	// by every panel auth form, and the value the choice blob's MAC is bound to.
	//
	// TWO INDEPENDENT RANDOM VALUES, NOT ONE DERIVED FROM THE OTHER. cookies.go
	// states the reason from the activation flow's side: an attacker who can plant
	// a cookie knows their own value, so a token DERIVED from the cookie is a token
	// they can compute and the measure is decorative. Here the csrf half is
	// rendered into the page and the bind half never is, so knowing one tells you
	// nothing about the other.
	adminLoginCookieName = "tappa_admin_login"

	// adminChoiceCookieName carries "<payload>.<mac>": the VERIFIED candidate set.
	// Separate from the login cookie so that clearing one never disturbs the other,
	// and so that the ordinary single-business login never sets it at all.
	adminChoiceCookieName = "tappa_admin_choice"

	// adminLoginCookieMaxAge is 15 minutes: long enough to read a login form and
	// type a password on a laptop, short enough that a synchronizer token left on a
	// shared machine goes stale on its own.
	adminLoginCookieMaxAge = 15 * 60

	// adminChoiceCookieMaxAge is the browser-side twin of adminChoiceTTL: the blob
	// should not sit in a browser looking usable after the server would refuse it.
	//
	// 🔴 IT IS DERIVED, NOT A SECOND LITERAL. It used to read `= 5 * 60` beside a
	// comment saying it was "deliberately equal" to adminChoiceTTL — two independent
	// numbers held together by prose, which is exactly the shape that let
	// adminauth.MaxCandidates and adminChoiceMaxEntries drift apart in this same
	// task. A family scan over every constant this feature introduced found it.
	// Deriving it means "equal" is held by the compiler.
	adminChoiceCookieMaxAge = int(adminChoiceTTL / time.Second)

	// adminChoiceVersion prefixes the payload; parse refuses any other value, so a
	// payload written under a DIFFERENT layout is rejected rather than misread.
	// That holds PROVIDED whoever changes the layout also bumps this — discipline,
	// not enforcement, exactly as tapcontext.go says of its own version byte.
	adminChoiceVersion = "1"

	// adminChoiceMACLabel is prefixed to the MAC input. Domain separation: this
	// deployment now derives FOUR things from HMAC-SHA256 (session token hashes,
	// invite code hashes, tap contexts, and this), and without a label a value
	// minted for one could be presented to another.
	//
	// ⚠️ AN EARLIER VERSION CLAIMED "the label ends in a byte that cannot occur in
	// the payload's alphabet, so label||payload is unambiguous". THAT IS FALSE and
	// an audit caught it: the label ends in '|', which is exactly the payload's own
	// field separator (see payload()). The claim was harmless because it was not
	// what made the encoding unambiguous — the LENGTH PREFIX in sign() is, and the
	// four derivations use four DIFFERENT keys, so a value minted for one cannot
	// verify under another whatever the labels look like. The sentence is corrected
	// rather than the label changed: changing it would buy nothing and invalidate
	// every in-flight blob.
	adminChoiceMACLabel = "tappa/admin-login-choice/v1|"

	// adminChoiceKeyLabel derives this value's signing key from the session HMAC
	// key, for the reason tapContextKeyLabel gives: nothing here is STORED — the
	// blob lives for minutes inside one cookie — so a new required environment
	// variable would mean every deployment and CI refusing to boot until an
	// operator sets a key whose only job is to sign a five-minute value. HMAC is
	// one-way, so holding this key reveals nothing about TAPPA_SESSION_HMAC_KEY.
	// The implication runs the other way, and that is the stated limit.
	//
	// 🔴 WHAT THAT LIMIT ACTUALLY COSTS, MEASURED — the direction was written down
	// and the CONSEQUENCE was not. A security audit signed a forged payload with the
	// real derived key: the picker offered a tenant the password never verified
	// against (200), and the POST issued a session in that tenant (303, one row in
	// the victim's admin_sessions). So a leaked TAPPA_SESSION_HMAC_KEY is not merely
	// "employee sessions become forgeable" — it is a PASSWORD-FREE CROSS-TENANT
	// PANEL SESSION.
	//
	// The precondition keeps it LOW rather than critical, and it is worth stating
	// exactly: the forger must also know the victim's admin_user_id AND tenant_id,
	// two random UUIDs — 244 bits of unguessable material that appear in no page,
	// no URL and no error. The obligation-5 binding is unaffected: it refuses
	// everything the SERVER did not sign, and this attack presupposes the ability
	// to sign as the server.
	adminChoiceKeyLabel = "tappa/admin-login-choice/v1/key-derivation"

	// adminChoiceTTL bounds how long a minted choice may be presented.
	//
	// FIVE MINUTES. It is a credential lifetime for a bearer value (see the header),
	// and the only thing a person does with it is read a list of two or three
	// business names and click one. tapcontext.go can afford fifteen because its
	// blob authorises nothing on its own — the counter advance does — whereas this
	// one completes an authentication.
	adminChoiceTTL = 5 * time.Minute

	// adminChoiceFutureSkew tolerates a blob that appears to come from the future.
	// Both stamps are read from THIS process's clock and the value is
	// authenticated, so the only way here is the clock stepping backwards (an NTP
	// correction, a suspended VM). Beyond the tolerance the blob is treated as
	// expired rather than believed — the fail-closed direction.
	//
	// ⚠️ IT WIDENS THE BEARER WINDOW, AND THE REAL FIGURE IS 6 MINUTES, NOT 5. An
	// audit measured the acceptance band directly: a blob minted at T is accepted
	// for reader-clock offsets [-1m0s .. 5m0s], i.e. a total window of 6m1s. Every
	// other sentence in this file says "five minutes", which is adminChoiceTTL
	// alone. The worst case a threat model should use is TTL + skew.
	adminChoiceFutureSkew = time.Minute

	// adminChoiceMaxEntries caps how many identities one blob may carry.
	//
	// 🔴 IT IS NOW DERIVED FROM THE BCRYPT CAP, NOT A SECOND LITERAL. This line used
	// to read `= 8` beside a comment claiming it "equals the bcrypt candidate cap by
	// construction". An audit measured that FALSE: two independent unexported
	// literals in two packages, cross-referenced by one comment. The consequence was
	// concrete — raising adminauth's cap alone gave a legitimate operator with nine
	// businesses HTTP 500 at the picker (mint refuses, adminlogin.go answers 500)
	// while every test stayed green.
	//
	// "By construction" is true only when the CONSTRUCTION provides it. Now it does:
	// the compiler does, and a verified set genuinely cannot be larger than the set
	// that was compared.
	//
	// It stays a PARSER bound rather than a policy: it keeps a hostile cookie from
	// making the server allocate, and it keeps the cookie inside every browser's
	// 4 KB limit (8 entries is ~600 bytes before base64).
	//
	// ⚠️ THE TWO RATIONALES ARE NOW COUPLED, which deriving the constant bought and
	// also created. The cookie-size guarantee rides on a number chosen for a CPU
	// reason: raise MaxCandidates for a DoS argument and the cookie budget moves
	// with it, silently (64 entries would be ~4.7 KB, past every browser's limit).
	// It cannot drift today — the value is pinned by
	// TestMaxCandidates_IsTheMeasuredCPUBound — but whoever changes that number owes
	// this sentence a second look, because nothing here will complain.
	adminChoiceMaxEntries = adminauth.MaxCandidates
)

// Failure modes, kept apart so the handler can tell a stale picker (say "start
// again" — honest and actionable) from a value that was tampered with or was never
// ours (a generic refusal and a log line). The VISITOR is told the same thing
// either way; this distinction is for us.
var (
	errChoiceMalformed = errors.New("handler: login choice is malformed")
	errChoiceSignature = errors.New("handler: login choice signature does not match")
	errChoiceExpired   = errors.New("handler: login choice has expired")
)

// adminLoginState is what the login cookie carries.
//
// NEITHER HALF IS A SECTION 4.7 SECRET. The csrf half is rendered into the page on
// purpose — that is the whole mechanism — and the bind half is 32 bytes of
// randomness that identifies nothing and outlives nothing. There is no password,
// no digest and no session token here, which is why this type needs none of the
// redaction machinery adminauth.Token and db.PasswordHash carry.
type adminLoginState struct {
	csrf string
	bind string
}

// csrfMatches compares the token the form sent with the one in the cookie, in
// constant time (redline R7: a comparison that decides an authentication outcome is
// never == or bytes.Equal). ConstantTimeCompare returns 0 for differing lengths,
// so an absent form field can never match.
func (st adminLoginState) csrfMatches(sent string) bool {
	if st.csrf == "" || sent == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(st.csrf), []byte(sent)) == 1
}

// adminCookies writes, reads and clears the two short-lived panel auth cookies.
//
// THE FIELD'S POLARITY IS THE SECURITY MECHANISM — `insecure`, not `secure`, so
// the ZERO VALUE MEANS SECURE. Fourth application of the M5-01 round-3 finding in
// this repo; the reasoning is in internal/session/cookie.go and is not repeated.
type adminCookies struct {
	insecure bool
}

func newAdminCookies(cfg *config.Config) adminCookies {
	if cfg == nil || cfg.IsProd() {
		return adminCookies{}
	}
	// Case-insensitive: URL schemes are (RFC 3986 3.1), and M5-01 shipped a bug
	// where "HTTPS://" bought a weaker cookie through a case-sensitive test.
	return adminCookies{insecure: !strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://")}
}

func (c adminCookies) secure() bool { return !c.insecure }

// set writes one of the two cookies. Both are scoped to adminauth.CookiePath, so
// neither is ever sent to the tap surface — the same structural separation the
// panel session cookie relies on.
func (c adminCookies) set(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     adminauth.CookiePath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   c.secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

// clear expires a cookie. Attributes must match set exactly or the browser keeps
// the original.
func (c adminCookies) clear(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     adminauth.CookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

// setLogin writes the synchronizer token and the binding value.
func (c adminCookies) setLogin(w http.ResponseWriter, st adminLoginState) {
	c.set(w, adminLoginCookieName, st.csrf+"."+st.bind, adminLoginCookieMaxAge)
}

// readLogin lifts the login state off a request. The second result is false when
// the cookie is absent, empty or malformed; the caller mints a fresh one and shows
// the form again.
//
// A cookie that does not split into exactly two non-empty parts is treated as
// ABSENT rather than repaired: a value in that shape was not written by setLogin,
// and guessing at what half of it means is how a parser becomes an attack surface.
func (c adminCookies) readLogin(r *http.Request) (adminLoginState, bool) {
	ck, err := r.Cookie(adminLoginCookieName)
	if err != nil || ck.Value == "" {
		return adminLoginState{}, false
	}
	csrf, bind, ok := strings.Cut(ck.Value, ".")
	if !ok || csrf == "" || bind == "" {
		return adminLoginState{}, false
	}
	return adminLoginState{csrf: csrf, bind: bind}, true
}

// newAdminLoginState mints a fresh pair of independent random values.
func newAdminLoginState() (adminLoginState, error) {
	csrf, err := newCSRFToken()
	if err != nil {
		return adminLoginState{}, err
	}
	bind, err := newCSRFToken()
	if err != nil {
		return adminLoginState{}, err
	}
	return adminLoginState{csrf: csrf, bind: bind}, nil
}

// adminChoices mints and parses the signed verified-candidate set.
//
// The key is derived once, at construction, and held privately, so a later
// mutation of the caller's config slice cannot silently change how blobs sign.
type adminChoices struct {
	key []byte
	ttl time.Duration
	// now is injectable for tests; production leaves it nil and reads the wall
	// clock.
	now func() time.Time
}

// newAdminChoices derives the signing key from the session HMAC key. It refuses a
// wrong-sized key rather than degrading: a short or empty key still produces
// plausible-looking output, so the failure would stay invisible until somebody
// noticed blobs were forgeable.
func newAdminChoices(cfg *config.Config) (adminChoices, error) {
	if cfg == nil {
		return adminChoices{}, errors.New("handler: nil config")
	}
	if len(cfg.SessionHMACKey) != 32 {
		return adminChoices{}, fmt.Errorf("handler: session hmac key must be 32 bytes, got %d", len(cfg.SessionHMACKey))
	}
	m := hmac.New(sha256.New, cfg.SessionHMACKey)
	_, _ = m.Write([]byte(adminChoiceKeyLabel)) // hash writes never fail (documented)
	return adminChoices{key: m.Sum(nil), ttl: adminChoiceTTL}, nil
}

func (c adminChoices) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// payload renders the authenticated fields.
//
// The separators are '|', ',' and ':', none of which can occur in any field: the
// version is one digit, the timestamp is decimal, and the identities are UUIDs
// (hex and '-'). So splitting back is unambiguous with no escaping.
func (c adminChoices) payload(issuedAt time.Time, verified []adminauth.Verified) string {
	parts := make([]string, 0, len(verified))
	for _, v := range verified {
		parts = append(parts, v.AdminUserID.String()+":"+v.TenantID.String())
	}
	return strings.Join([]string{
		adminChoiceVersion,
		strconv.FormatInt(issuedAt.Unix(), 10),
		strings.Join(parts, ","),
	}, "|")
}

// sign computes the MAC over the label, the payload and the BINDING value. The
// binding never enters the payload, so the blob is usable only from the browser
// that holds the matching login cookie while the value itself stays out of the
// page.
//
// 🔴 THE LENGTH PREFIX IS NOT DECORATION — IT REMOVES A CONCATENATION AMBIGUITY.
// Without it the MAC input is `label || payload || "|" || bind`, and an attacker
// controls BOTH halves, so the same byte string can be split at any '|' into a
// DIFFERENT (payload, bind) pair. An audit walked every '|' boundary and found no
// exploit — but only because parse insists on exactly three payload fields, so
// every legitimate payload has a fixed shape. That is a guarantee held by a
// DIFFERENT function, and it would evaporate silently the day a fourth field is
// added to the payload.
//
// Writing the payload's byte length first makes the encoding injective: decoding
// `len || "|" || payload || "|" || bind` recovers exactly one (payload, bind) pair
// for any input, whatever shape the payload later takes. It costs a few bytes of
// hash input and it means the next person to add a field does not have to know
// about this.
func (c adminChoices) sign(payload, bind string) []byte {
	m := hmac.New(sha256.New, c.key)
	_, _ = m.Write([]byte(adminChoiceMACLabel))
	// Length prefix first: see the note above. hash writes never fail (documented).
	_, _ = m.Write([]byte(strconv.Itoa(len(payload))))
	_, _ = m.Write([]byte("|"))
	_, _ = m.Write([]byte(payload))
	_, _ = m.Write([]byte("|"))
	_, _ = m.Write([]byte(bind))
	return m.Sum(nil)
}

// mint stamps the verified set with the server clock and returns the cookie value.
//
// IT REFUSES AN EMPTY SET. A blob carrying no identity would parse, verify and
// then authorise nothing — but it would also be a value that says "this browser
// completed step 1", and the only honest answer to "which of zero businesses?" is
// to send the person back to the login form.
func (c adminChoices) mint(verified []adminauth.Verified, bind string) (string, error) {
	if len(c.key) == 0 {
		// A zero adminChoices would sign under an empty key — a MAC anybody can
		// compute. Refuse rather than emit a decorative signature. (Where
		// adminauth.Cookies made its zero value SAFE, this type cannot: there is no
		// safe key to default to, so the zero value is an ERROR instead, checked in
		// both mint and parse.)
		return "", errors.New("handler: login choices were not configured")
	}
	if bind == "" {
		// Binding to nothing would produce a blob any browser could spend, which is
		// the one property this signature exists to deny.
		return "", errors.New("handler: refusing to sign a login choice with no binding")
	}
	if len(verified) == 0 {
		return "", errors.New("handler: refusing to sign an empty login choice")
	}
	if len(verified) > adminChoiceMaxEntries {
		return "", fmt.Errorf("handler: login choice carries %d identities, max %d", len(verified), adminChoiceMaxEntries)
	}
	p := c.payload(c.clock().Truncate(time.Second), verified)
	return base64.RawURLEncoding.EncodeToString([]byte(p)) + "." +
		base64.RawURLEncoding.EncodeToString(c.sign(p, bind)), nil
}

// parse verifies a blob against bind and returns the identities it carries.
//
// ORDER: shape, then SIGNATURE, then expiry, then field decoding. The signature
// comes before anything that interprets the payload, so no field of an
// unauthenticated value is ever acted on — and expiry comes before decoding for
// the same reason in miniature. This is tapcontext.go's order, kept deliberately
// identical so the two are read as one pattern.
func (c adminChoices) parse(v, bind string) ([]adminauth.Verified, error) {
	if len(c.key) == 0 {
		return nil, errors.New("handler: login choices were not configured")
	}
	if bind == "" {
		return nil, errChoiceSignature
	}
	encoded, sig, ok := strings.Cut(v, ".")
	if !ok || encoded == "" || sig == "" {
		return nil, errChoiceMalformed
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errChoiceMalformed
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return nil, errChoiceMalformed
	}
	// Constant time, never == or bytes.Equal (redline R7). ConstantTimeCompare
	// returns 0 for differing lengths, so a truncated signature cannot match.
	if subtle.ConstantTimeCompare(got, c.sign(string(raw), bind)) != 1 {
		return nil, errChoiceSignature
	}

	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 || parts[0] != adminChoiceVersion {
		// Authenticated but unreadable: minted by this deployment under a different
		// layout. Treated as malformed, never repaired.
		return nil, errChoiceMalformed
	}
	issued, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, errChoiceMalformed
	}
	issuedAt := time.Unix(issued, 0).UTC()
	now := c.clock()
	if now.Sub(issuedAt) > c.ttl || issuedAt.Sub(now) > adminChoiceFutureSkew {
		return nil, errChoiceExpired
	}

	entries := strings.Split(parts[2], ",")
	if len(entries) == 0 || len(entries) > adminChoiceMaxEntries {
		return nil, errChoiceMalformed
	}
	out := make([]adminauth.Verified, 0, len(entries))
	for _, e := range entries {
		adminID, tenantID, ok := strings.Cut(e, ":")
		if !ok {
			return nil, errChoiceMalformed
		}
		a, err := uuid.Parse(adminID)
		if err != nil {
			return nil, errChoiceMalformed
		}
		t, err := uuid.Parse(tenantID)
		if err != nil {
			return nil, errChoiceMalformed
		}
		if a == uuid.Nil || t == uuid.Nil {
			return nil, errChoiceMalformed
		}
		out = append(out, adminauth.Verified{AdminUserID: a, TenantID: t})
	}
	return out, nil
}

// selectVerified returns the identity for tenantID, but ONLY if that tenant is in
// the AUTHENTICATED set.
//
// 🔴 THIS FUNCTION IS THE SECOND HALF OF PHASE B OBLIGATION 5, and it is the exact
// point the attack 00011 measured would have to pass through. The picker POSTs a
// tenant id; a client can post any tenant id it likes; this is what re-proves that
// the id belongs to a candidate whose stored digest actually verified. `verified`
// comes only from parse, i.e. only from a payload this server signed.
//
// THE COMPARISON IS CONSTANT TIME, which is defence in depth rather than a
// necessity — the values are opaque database ids, not secrets, and an attacker who
// guessed a tenant id would still fail membership. It is written this way because
// redline R7's rule is "a comparison that decides an authentication outcome", and
// this decides one.
func selectVerified(verified []adminauth.Verified, tenantID uuid.UUID) (adminauth.Verified, bool) {
	if tenantID == uuid.Nil {
		return adminauth.Verified{}, false
	}
	want := tenantID.String()
	var (
		found adminauth.Verified
		hit   int
	)
	for _, v := range verified {
		if subtle.ConstantTimeCompare([]byte(v.TenantID.String()), []byte(want)) == 1 {
			found = v
			hit++
		}
	}
	// hit > 1 cannot happen through the ordinary path — resolve_admin_by_email
	// returns at most one row per tenant, structurally (migration 00011's partial
	// UNIQUE index) — so a duplicate means the payload was built by something other
	// than Authenticate. Refuse rather than pick one.
	if hit != 1 {
		return adminauth.Verified{}, false
	}
	return found, true
}
