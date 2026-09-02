package session

import (
	"errors"
	"net/http"
	"strings"

	"github.com/atknatk/tappa/internal/config"
)

// The cookie half of "proof of person". Deliberately plain net/http: this file
// writes and reads one http.Cookie and nothing else. internal/session must not
// import internal/handler — the handler layer calls into here, never the reverse.

const (
	// CookieName is the single session cookie. Exported because the middleware
	// (M5-03) and the tests assert on it; the VALUE is never exported.
	//
	// The __Host- prefix (which would additionally forbid a Domain attribute and
	// pin Path=/) was considered and NOT adopted: browsers require Secure for
	// __Host-, so it would either break http://localhost development or force the
	// cookie name to differ between environments — a name that changes per
	// environment is its own class of bug. Revisit at deployment (M8).
	//
	// 🔴 REVISITED AT M8-04 (2026-08-19, backlog T3): STILL NOT ADOPTED, and the
	// three measurements behind that are written out in internal/handler/cookies.go,
	// beside the shadowing limit they belong to. The measurement that once made the
	// risk REAL — the product served from a subdomain of a registrable domain it did
	// not own alone — was closed by the 2026-09-02 cutover to taptime.mt, an apex the
	// product owns entirely; the residual is a deployment constraint (add no untrusted
	// subdomain under taptime.mt), recorded on that side. This line is the pointer so
	// the two sides cannot drift apart.
	CookieName = "tappa_session"

	// cookieMaxAgeSeconds is ~1 year (the card's "uzun Max-Age"). The whole
	// product promise is "the phone remembers you"; a short session would push
	// employees back through activation and train them to re-activate, which is
	// exactly the habit an attacker wants.
	//
	// This is the SERVER's request. What a browser actually honours is Q11
	// (iOS Safari / ITP), which can only be measured on a real iPhone and is
	// still OPEN. Nothing in this file depends on the answer.
	cookieMaxAgeSeconds = 365 * 24 * 60 * 60
)

// errEmptyCookie guards against writing a session cookie with no value, which
// would look like a successful activation and behave like no session at all.
var errEmptyCookie = errors.New("session: refusing to set an empty session cookie")

// Cookies writes, clears and reads the session cookie with attributes fixed at
// construction.
//
// THE FIELD'S POLARITY IS THE SECURITY MECHANISM. It is `insecure`, not
// `secure`, so the ZERO VALUE MEANS SECURE. That is not a style preference; it
// is the only way the guarantee can hold. An earlier version stored `secure bool`
// and this comment claimed no caller could produce a non-Secure cookie in
// production — which was FALSE and an independent audit proved it. Go forbids
// another package from NAMING an unexported field; it does not forbid obtaining
// the zero value, and there are at least three ordinary ways to do so:
//
//	var c session.Cookies                       // declaration
//	c := session.Cookies{}                      // empty composite literal
//	h := handler{data: d}                       // struct field simply left out —
//	                                            // NOT a compile error in Go
//
// The third is the realistic one: M5-02/M5-03 will hold a Cookies in a handler
// struct, and forgetting one field in a keyed literal compiles silently. With the
// old polarity that shipped a session cookie without Secure in production, so any
// plain-http request to the site would make the browser send the session value in
// clear — handing an on-path attacker the whole "who" evidence of §5. The value is
// a §4.7 secret and this flag is its only transport protection.
//
// Flipping the polarity was chosen over the alternative (make Set/Clear reject an
// unconfigured codec) because it needs no sentinel field, no error path on a
// method that otherwise cannot fail, and no discipline: the dangerous state
// simply stops being representable by accident. The relaxation must now be asked
// for explicitly, and only NewCookies can ask.
//
// What is true, and now actually tested from a caller's position
// (leak_external_test.go): a Config whose Env is exactly "prod" always yields
// Secure, the zero Cookies value is Secure, and the ONLY producer of a
// non-Secure cookie is NewCookies on a Config whose Env is not "prod" AND whose
// BaseURL is not https.
//
// Read "Env is not prod" literally: it covers a MISSPELLED or MISSING Env too,
// since this function compares strings and does not re-enforce config.Load's
// enum. NewCookies' own comment lists those rows; they are the residual gap, and
// it is operational, not structural.
type Cookies struct {
	// insecure downgrades the cookie for local http development. Unexported and
	// negative on purpose: see the type comment.
	insecure bool
}

// NewCookies derives the cookie hardening from configuration. It is the only
// producer of a non-Secure codec, and it does so in exactly one branch.
//
// EVERY combination, exhaustively — the earlier version of this table omitted the
// staging+http row, which is a real reachable state and the one most likely to
// surprise someone deploying:
//
//	nil config                    -> Secure (fail safe)
//	prod, any BaseURL             -> Secure (unconditional, before anything else)
//	dev     over https            -> Secure
//	staging over https            -> Secure
//	dev     over http             -> NOT Secure (so http://localhost:8080 works)
//	staging over http             -> NOT Secure  <-- a staging box served over
//	                                 plain http writes a session cookie without
//	                                 Secure. That follows from the rule, but say
//	                                 it out loud: serve staging over https, or
//	                                 accept that its sessions are sniffable.
//	non-prod, unparseable BaseURL -> NOT Secure (anything that is not an https
//	                                 prefix takes the relaxation branch)
//
// THE TABLE ABOVE ASSUMES A Config THAT CAME FROM config.Load. Load enforces
// TAPPA_ENV as a closed set, which is what makes "dev | staging | prod" the whole
// space. This function does NOT re-enforce it, so a Config that skipped or
// defeated Load reaches rows the table does not have — measured:
//
//	Env "production" / "PROD" / "prod " -> NOT Secure (Load would have rejected
//	                                       these, but NewCookies just sees a
//	                                       non-"prod" string)
//	Env "" or a zero Config             -> NOT Secure (Load would have applied the
//	                                       "dev" default; nothing is enforced here)
//
// Do not read that as "the zero value is unsafe": a Cookies obtained WITHOUT this
// constructor is Secure (see the type comment, and it is tested). The gap is
// narrower and worth naming precisely — a CONSTRUCTED but wrong Config is
// believed. In production the remaining defence is operational: set
// TAPPA_ENV=prod. config.IsProd carries the same note from its side, so neither
// file leans on the other to close the hole.
func NewCookies(cfg *config.Config) Cookies {
	if cfg == nil {
		return Cookies{}
	}
	if cfg.IsProd() {
		return Cookies{}
	}
	// The single relaxation in the codebase: non-prod AND not https.
	//
	// The scheme test is CASE-INSENSITIVE because URL schemes are (RFC 3986 §3.1)
	// and config.Load does not normalise or validate BaseURL. It used to be a
	// plain HasPrefix, so BaseURL="HTTPS://…" took the relaxation branch on a
	// dev/staging box — a typo silently buying a weaker cookie. The relaxation
	// must be asked for, never acquired by accident.
	//
	// LIMIT: this is a prefix test, not a URL parse. A BaseURL with leading
	// whitespace, or one that is not a URL at all, does not match and therefore
	// lands on NOT Secure — fail-OPEN for this one flag, which is why it is
	// confined to non-prod. Validating BaseURL's shape belongs to config.Load and
	// is deliberately not done here (see the note on that function).
	return Cookies{insecure: !strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://")}
}

// Secure reports the hardening this codec applies. Read-only accessor for tests
// and for a startup log line; there is no matching setter on purpose.
func (c Cookies) Secure() bool { return !c.insecure }

// Set writes the session cookie carrying the raw token. This is the ONE place in
// the codebase where the raw value leaves the process, and it leaves into a
// Set-Cookie header — never into a log, an error or a database column (§4.7).
//
// SameSite=Lax, NOT Strict (card trap): the tap flow is a cold cross-context
// navigation — the phone reads the wall plaque and the operating system hands
// the URL to a browser that may treat the request as cross-site. Strict would
// withhold the cookie on exactly that first navigation and every tap would land
// on the activation page. Lax still blocks the cross-site POST shapes that
// matter here.
//
// HttpOnly: no page script ever needs the value, so script access is removed.
// Path "/": the cookie must accompany /t, /api/checkin and the activation flow.
func (c Cookies) Set(w http.ResponseWriter, t Token) error {
	// reveal() is "" for the zero Token as well as for an empty one, so a single
	// check covers both and nothing is dereferenced blindly.
	v := t.reveal()
	if v == "" {
		return errEmptyCookie
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    v,
		Path:     "/",
		MaxAge:   cookieMaxAgeSeconds,
		HttpOnly: true,
		Secure:   c.Secure(),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Clear expires the session cookie in the browser. It is the client-side twin of
// Manager.Revoke and never a substitute for it: only the revoked_at stamp makes
// a stolen cookie useless, since a browser is free to ignore this header.
// Attributes must match Set exactly or the browser keeps the original cookie.
func (c Cookies) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.Secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

// Read lifts the session cookie off a request. A missing cookie is ErrNoSession,
// the same sentinel an unknown or malformed value produces, because §5 row 3
// treats them identically: serve the activation page and write NO record.
//
// The value is wrapped in a Token immediately, so the raw string exists as a
// bare string only inside this function.
func (c Cookies) Read(r *http.Request) (Token, error) {
	ck, err := r.Cookie(CookieName)
	if err != nil {
		return Token{}, ErrNoSession
	}
	if ck.Value == "" {
		return Token{}, ErrNoSession
	}
	return wrap(ck.Value), nil
}
