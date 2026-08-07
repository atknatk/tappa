package adminauth

import (
	"errors"
	"net/http"
	"strings"

	"github.com/atknatk/tappa/internal/config"
)

// The panel session cookie. Deliberately plain net/http: this file writes and
// reads one http.Cookie and nothing else.

const (
	// CookieName is the panel session cookie, and it is SEPARATE FROM
	// session.CookieName by requirement, not by taste. The M6-01 card: "Admin
	// oturumu calisan oturumundan ayri cerez/tablo; bir calisan cerezi panele
	// erisemiyor ve tersi."
	//
	// The name is only the first of three independent separations, and it is the
	// weakest one — a name is a string somebody can mistype. The other two are
	// structural: the token hashes under a DIFFERENT key (token.go), and it
	// resolves through a DIFFERENT SECURITY DEFINER function over a DIFFERENT table
	// (migration 00011). So even a raw token pasted into the other cookie's name
	// resolves to nothing, which is what TestPanelCookies_NeverReachTheTapSurface measures in both
	// directions.
	CookieName = "tappa_admin_session"

	// CookiePath scopes the panel cookie to the panel.
	//
	// THIS IS THE STRUCTURAL HALF OF "an admin cookie cannot reach the tap
	// surface": with Path=/admin the browser DOES NOT SEND this cookie to /t,
	// /api/checkin or /activate at all, so those handlers cannot read it even by
	// mistake. session.Cookies uses Path=/ because the employee cookie genuinely
	// must accompany /t, /api/checkin and the activation flow.
	//
	// THE CONSTRAINT IT IMPOSES, stated because breaking it is silent: every panel
	// route — pages AND any future /api endpoint the panel calls — must live under
	// /admin. A panel fetch to /api/something would arrive with no cookie and look
	// like a logged-out request. M6-02 onwards: mount under /admin.
	//
	// ⚠️ WHAT IT DOES NOT BUY. Path is not a security boundary in the same sense as
	// origin: script running on this origin can still read a Path=/admin cookie's
	// effects by navigating, and Path offers no protection against a same-origin
	// XSS. It is HttpOnly that removes script access, and the CSP on these screens
	// that reduces the chance of script at all. Path buys ambient non-transmission,
	// which is exactly the "cannot reach the tap surface" property and no more.
	CookiePath = "/admin"

	// cookieMaxAgeSeconds is TWELVE HOURS, not the employee cookie's year.
	//
	// The product promise "the phone remembers you" is about the plaque, not the
	// panel: an admin session reads every employee's movements and can rewrite the
	// tenant's policies, and it is held on a laptop in an office rather than on a
	// personal phone. A working day is the natural bound.
	//
	// 🔴 LIMIT, AND IT IS A REAL ONE: THIS IS A HINT, NOT AN EXPIRY. Max-Age is a
	// browser instruction. The SERVER has no way to expire an admin session,
	// because admin_sessions (migration 00006) carries created_at, last_used_at and
	// revoked_at but NO expires_at, and neither resolve_admin_session_by_token_hash
	// nor TouchAdminSession returns a timestamp that could be compared before the
	// update. So a client that simply keeps the cookie — curl, a scraped value, a
	// browser profile copied off a disk — stays authenticated indefinitely.
	//
	// WHAT ACTUALLY ENDS AN ADMIN SESSION TODAY: an explicit sign-out
	// (RevokeAdminSession), a "sign out everywhere" (RevokeAdminSessionsForAdmin),
	// or admin_users.status='disabled', which TouchAdminSession refuses on the very
	// next request. All three are deliberate acts by somebody.
	//
	// WHY IT IS NOT FIXED HERE: an absolute or idle expiry needs a column or a
	// returned timestamp, i.e. a migration, and this task was scoped explicitly as
	// "the schema is ready; no new migration is expected". It is written down as a
	// hand-off rather than left for the next reader to discover from a Max-Age that
	// looks like it means something. M6-05 / M7-04 own it.
	cookieMaxAgeSeconds = 12 * 60 * 60
)

// errEmptyCookie guards against writing a session cookie with no value, which
// would look like a successful login and behave like no session at all.
var errEmptyCookie = errors.New("adminauth: refusing to set an empty admin session cookie")

// Cookies writes, clears and reads the panel session cookie.
//
// THE FIELD'S POLARITY IS THE SECURITY MECHANISM — `insecure`, not `secure`, so
// the ZERO VALUE MEANS SECURE. This is the M5-01 round-3 audit finding applied to
// the third cookie codec in the codebase, and the finding was precise: Go forbids
// another package from NAMING an unexported field, not from obtaining the zero
// value. `var c Cookies`, `Cookies{}` and — the realistic one — a handler struct
// literal that leaves the field out all compile. With `secure bool` every one of
// those ships a credential-bearing cookie without Secure; with this polarity they
// all ship Secure and only NewCookies can ask for the relaxation, in one branch.
type Cookies struct {
	// insecure downgrades the cookie for local http development. Unexported and
	// negative on purpose: see the type comment.
	insecure bool
}

// NewCookies derives the hardening from configuration, mirroring
// session.NewCookies row for row (prod is always Secure; a non-prod deployment
// served over plain http relaxes so http://localhost works).
//
// The same residual gap applies and is not papered over: a Config that skipped
// config.Load (a struct literal, a misspelled Env) takes the non-prod branch and
// is believed. config.IsProd carries the note from its side.
func NewCookies(cfg *config.Config) Cookies {
	if cfg == nil {
		return Cookies{}
	}
	if cfg.IsProd() {
		return Cookies{}
	}
	// The scheme test is CASE-INSENSITIVE: URL schemes are (RFC 3986 3.1) and
	// M5-01 shipped a bug where "HTTPS://" bought a weaker cookie through a
	// case-sensitive prefix test. The relaxation must be asked for, never acquired
	// by accident.
	return Cookies{insecure: !strings.HasPrefix(strings.ToLower(cfg.BaseURL), "https://")}
}

// Secure reports the hardening this codec applies. Read-only; there is no setter
// on purpose.
func (c Cookies) Secure() bool { return !c.insecure }

// Set writes the panel session cookie carrying the raw token. This is the ONE
// place in the codebase where an admin token leaves the process, and it leaves
// into a Set-Cookie header — never into a log, an error or a database column.
//
// SameSite=Lax rather than Strict. Strict is defensible for a panel and was
// considered; Lax is chosen because a link to the dashboard from a mail client
// (an alert, an invitation, a bookmark synced from another device) is an ordinary
// arrival, and under Strict every one of those lands logged-out and needs a
// second navigation to work. Lax still withholds the cookie from the cross-site
// POST shapes that matter, and the panel's state-changing endpoints additionally
// check Origin BEFORE resolving the session, so the cookie is not the only thing
// standing between a forged POST and a write.
//
// ⚠️ THIS USED TO SAY "and carry a synchronizer token" AND THAT IS NOT TRUE OF THE
// WHOLE PANEL. The token belongs to the sign-in flow (internal/handler's
// beginPost); POST /admin/review, added by M6-04, carries a record id, an outcome
// and an optional note and no token at all. The Origin check is what defends it,
// and a Lax cookie is what keeps a true cross-SITE page from having one to send.
// The sentence is corrected rather than deleted because the Lax-over-Strict
// decision leans on what the endpoints actually do, and leaning on a defence that
// is not there is how the decision stops being reviewable.
func (c Cookies) Set(w http.ResponseWriter, t Token) error {
	// reveal() is "" for the zero Token as well as an empty one, so one check
	// covers both and nothing is dereferenced blindly.
	v := t.reveal()
	if v == "" {
		return errEmptyCookie
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    v,
		Path:     CookiePath,
		MaxAge:   cookieMaxAgeSeconds,
		HttpOnly: true,
		Secure:   c.Secure(),
		SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// Clear expires the panel cookie in the browser. It is the client-side twin of
// Manager.Revoke and NEVER a substitute for it: only the revoked_at stamp makes a
// stolen cookie useless, since a browser is free to ignore this header.
// Attributes must match Set exactly or the browser keeps the original cookie.
func (c Cookies) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     CookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   c.Secure(),
		SameSite: http.SameSiteLaxMode,
	})
}

// Read lifts the panel cookie off a request. A missing cookie yields
// ErrNoSession, the same sentinel an unknown or malformed value produces: to the
// panel all three mean "go to the login page".
//
// The value is wrapped in a Token immediately, so the raw string exists as a bare
// string only inside this function.
func (c Cookies) Read(r *http.Request) (Token, error) {
	ck, err := r.Cookie(CookieName)
	if err != nil || ck.Value == "" {
		return Token{}, ErrNoSession
	}
	return wrap(ck.Value), nil
}
