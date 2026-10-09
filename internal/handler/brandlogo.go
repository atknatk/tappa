package handler

// brandlogo.go -- the two logo routes (M10 WL-6; ADR 0024 §5) and the img-src half of a
// page's Content-Security-Policy (ADR 0023 §4).
//
//	GET /admin/brand/logo/{sha}   the panel's read chain (AdminAuth.Protect)
//	GET /t/logo/{sha}             the tap surface's chain (Tap.chain): a live employee
//	                              session is required
//
// A THIRD LOGO ROUTE LIVES IN activationbrand.go (M10 WL-13): GET /activate/logo/{sha},
// keyed by the invitation in the activation cookie and gated on VIES, for the wizard's
// four steps, which a browser sees before it holds a session. It answers through
// serveLogo below, so its 200, its 304 rule and its refusal are these routes' own; its
// claim is written there.
//
// TWO ROUTES, BECAUSE TWO SURFACES. The panel cookie is Path=/admin and never reaches
// /t/…, so a tap-side route cannot see a manager; and each surface has its own
// resolver and its own budgets. Each route reads ONLY its own surface's identity: the
// employee cookie is Path=/ and does arrive at /admin/…, but the panel chain does not
// read it, so an employee cookie without a panel session gets the panel's signed-out
// answer (303 to the sign-in page) and no bytes.
//
// 🔴 THE TENANT COMES FROM THE RESOLVED SESSION AND FROM NOWHERE ELSE. The URL carries
// a digest, which is not an authority (two businesses that upload the same bytes hold
// the same one). tenant.BrandReader.Logo reads the row of THE SESSION'S business with
// that digest, so another business's digest finds nothing.
//
// 🔴 THE NOT-FOUND ANSWERS LISTED HERE ARE ONE RESPONSE (ADR 0024 §5, Iddia D). On a
// path the route matches, another business's digest, a digest nobody stored, a {sha}
// segment that is not 64 lower-case hex digits and a tap request without a live
// session all get writeLogoNotFound: the same status, the same headers and the same
// body. A malformed segment never reaches the logo read. These are not all of the
// routes' refusals, and the others are different answers: the panel chain answers a
// request without a panel session with its 303 to the sign-in page, the budgets
// answer 429, an outage or an unresolved tap identity answers 500, and a path the
// route does not match at all -- an empty segment, a raw "/" inside it -- never
// reaches this file: the router answers it with its own 404, from the path's shape
// alone (TestLogoRoutes_ShapesTheRouteDoesNotMatchGetTheRoutersAnswer). None of those
// depends on what another business stores.
//
// THE CLAIM, IN THREE PARTS.
//
// PART I -- THE SHIPPED CODE, MEASURED (brandlogo_test.go against fakes,
// brandlogo_db_test.go against the dev Postgres; every response is read from
// rec.Result(), and the classes named below also from a real server):
//   - the 200 carries exactly the header set ADR 0024 §5 lists plus Content-Length; the
//     Content-Type is the stored type (bytes that sniff as GIF, stored as image/png, are
//     served as image/png); the file name follows it, logo.png or logo.jpg; a query and
//     request headers naming a type, a file name or another business change no byte
//     (TestLogoRoutes_TheHeadersAreExactlyTheADRs);
//   - through httpx.NewRouter on a real server, configured WITHOUT an operator host,
//     the 200, the 404 and the 304 carry those headers plus net/http's Date and nothing
//     else -- the global middleware of that configuration adds no response header
//     (TestLogoRoutes_TheWireCarriesWhatTheHandlerSets);
//   - session A asking for B's digest and for a digest nobody stored get byte-identical
//     404s -- status, every header and body -- on both routes, through the real reader
//     and the real Postgres (TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest);
//   - the eight malformed digests the test names, a query naming another business, and
//     on the tap route no cookie, a cookie naming no session, a revoked session and a
//     panel cookie alone get that same 404; the malformed ones and the session-less ones
//     never reach the reader, and every read runs under the session's business
//     (TestLogoRoutes_EveryRefusalIsTheSameNotFound);
//   - an employee cookie without a panel session gets 303 to the sign-in page from the
//     panel route with no resolver call and no read, and a panel cookie whose session
//     the resolver refuses gets the same 303 and no read; with both cookies for two
//     businesses, each route serves its own surface's business
//     (TestLogoRoutes_EachRouteReadsOnlyItsOwnSurfacesSession);
//   - If-None-Match naming the digest answers 304 only after the business's own row was
//     found; on another business's digest and on an unknown one the 404 is unchanged
//     (TestLogoRoutes_IfNoneMatchIsAnsweredOnlyForTheSessionsOwnLogo);
//   - HEAD, POST, PUT, DELETE, PATCH, OPTIONS and TRACE get the router's 405 with
//     `Allow: GET`, a method it does not know (FOO) the 405 without an Allow header,
//     the same for the own digest and another business's; paths the routes do not
//     match get the router's 404; none of these resolves a panel session or reads
//     (TestLogoRoutes_ShapesTheRouteDoesNotMatchGetTheRoutersAnswer);
//   - each logo request is charged once to its surface's address and session budgets,
//     the same buckets the pages spend; anonymous tap logo requests resolve no session
//     and read nothing (TestLogoRoutes_ALogoRequestSpendsItsSurfacesBudget);
//   - a reader error is a 500 with no bytes, not the 404
//     (TestLogoRoutes_AReadFailureIsNotARefusal); a handler reached without its chain
//     answers 500 (tap) or the 404 (panel) and does not read
//     (TestLogoRoutes_AnUnresolvedIdentityIsNotNoSession).
//
// PART II -- NAMED PINS: the tests above. Each mutation that was run, and the tests it
// turned red, is listed in ADR 0024's WL-6 note.
//
// PART III -- No completeness claim: any change the table does not list is the subject
// of code review.

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
)

// The two routes. {sha} is the logo's sha256 in 64 lower-case hex digits -- the
// spelling migration 00028 stores (tenant_branding_logo_sha256_hex).
const (
	adminLogoRoute = "/admin/brand/logo/{sha}"
	tapLogoRoute   = "/t/logo/{sha}"
)

// adminLogoHref is the panel route's path for one digest: the <img src> the panel
// chrome draws (M10 WL-8). It is built from adminLogoRoute, so the image and the route
// are one string at compile time.
func adminLogoHref(digest string) string {
	return strings.Replace(adminLogoRoute, "{sha}", digest, 1)
}

// The logo response's policy headers (ADR 0024 §5).
//
//	Content-Security-Policy  default-src 'none'; sandbox -- the URL opened directly
//	                         in a tab is a document that may load nothing and run
//	                         nothing.
//	Cross-Origin-Resource-Policy  same-origin -- another site cannot embed it.
//	Cache-Control            private, max-age=31536000, immutable -- the URL is the
//	                         content's digest, so it never changes; private keeps it
//	                         out of shared caches because the answer depends on the
//	                         session.
const (
	logoPolicy       = "default-src 'none'; sandbox"
	logoResource     = "same-origin"
	logoCacheControl = "private, max-age=31536000, immutable"
)

// logoNotFoundBody is the body of every refusal on the two routes.
const logoNotFoundBody = "Not found.\n"

// logoImgSrc is the one directive a page names when it draws the business's logo: the
// logo routes are on our own origin, so 'self' is the whole widening.
const logoImgSrc = "img-src 'self'"

// logoImagePolicy is a page policy plus img-src 'self' when the page draws the
// business's logo, and the policy byte for byte when it does not (ADR 0023 §4; the
// landingCSPFor precedent, marketing.go). tapCSPFor (tap.go) and adminCSPFor
// (adminlogin.go) are this function over their surfaces' policies.
//
// 🔴 hasLogo MEANS "THIS RESPONSE CONTAINS THE <img>", NOT "THIS BUSINESS HAS A LOGO".
// A page that names img-src without drawing an image widens its policy for nothing, and
// a business with a logo has pages that do not draw it (a problem screen, a section the
// logo is not on). tenant.BrandReader.PageLogo answers the second question; the page
// that renders the <img> turns it into the first. No page renders the logo yet (WL-8,
// WL-9), so every render passes false today, and
// TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage pins the correspondence over
// the renders it lists.
func logoImagePolicy(policy string, hasLogo bool) string {
	if !hasLogo {
		return policy
	}
	return policy + "; " + logoImgSrc
}

// logoReader is the slice of tenant.BrandReader the routes need, declared at the
// consumer (section 7).
type logoReader interface {
	Logo(ctx context.Context, tenantID uuid.UUID, digest string) (tenant.StoredLogo, error)
}

// BrandLogos serves the two logo routes.
//
// IT IS GIVEN THE PANEL AND THE TAP HANDLERS RATHER THAN MIDDLEWARE, and that is the
// point: the chain each route runs is chosen HERE, from AdminAuth.Protect and
// Tap.chain, so a caller of NewBrandLogos has no argument through which to hand the
// routes a different chain -- Mount always puts its surface's chain in front. That
// is what this shape guarantees; it does not stop another route being registered
// elsewhere for the same path. Sharing the two values is also what makes a logo
// request spend the SAME budgets the pages spend (ADR 0024 §5): Tap.chain carries the
// one TapLimiter GET /t and POST /api/checkin are metered by, and Protect carries the
// panel's flood and session limiters.
type BrandLogos struct {
	logos logoReader
	panel *AdminAuth
	tap   *Tap
	log   *slog.Logger
}

// NewBrandLogos wires the routes. Every dependency is required: a nil reader would
// answer every logo with a panic, and a nil surface would mount a route with no chain.
func NewBrandLogos(logos logoReader, panel *AdminAuth, tap *Tap, log *slog.Logger) (*BrandLogos, error) {
	switch {
	case isNil(logos):
		return nil, errors.New("handler: nil logo reader")
	case panel == nil:
		return nil, errors.New("handler: nil panel")
	case tap == nil:
		return nil, errors.New("handler: nil tap surface")
	}
	if log == nil {
		log = slog.Default()
	}
	return &BrandLogos{logos: logos, panel: panel, tap: tap, log: log}, nil
}

// Mount registers the two routes, each in its own surface's chain. GET only: the
// methods TestLogoRoutes_ShapesTheRouteDoesNotMatchGetTheRoutersAnswer sends (HEAD
// included) get the router's 405, decided before either chain runs and the same for
// every digest.
func (b *BrandLogos) Mount(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(b.panel.Protect())
		r.Get(adminLogoRoute, b.panelLogo)
	})
	r.Group(func(r chi.Router) {
		r.Use(b.tap.chain()...)
		r.Get(tapLogoRoute, b.tapLogo)
	})
}

// panelLogo serves GET /admin/brand/logo/{sha}. Protect has already answered every
// request without a live panel session (303 to the sign-in page), so the identity here
// is live; a request that arrives without one is a wiring fault and gets the 404 --
// no bytes.
func (b *BrandLogos) panelLogo(w http.ResponseWriter, r *http.Request) {
	id := httpx.AdminOf(r)
	if !id.Live() {
		b.log.ErrorContext(r.Context(), "panel logo: no live panel identity on the request",
			"hint", "mount AdminAuth.Protect in front of the panel logo route")
		writeLogoNotFound(w, b.log)
		return
	}
	b.serve(w, r, id.TenantID())
}

// tapLogo serves GET /t/logo/{sha}. Only a LIVE employee session sees a logo: no
// cookie, an unknown one and a revoked one get the 404. An unresolved identity means
// the chain was not mounted or resolution failed (BySession answers the second with
// 500 before this runs), so it is logged and answered 500 rather than read as "no
// session" -- the distinction tap.go draws for the same state.
func (b *BrandLogos) tapLogo(w http.ResponseWriter, r *http.Request) {
	id := httpx.IdentityOf(r)
	switch id.State {
	case httpx.SessionLive:
		b.serve(w, r, id.TenantID())
	case httpx.SessionUnresolved:
		b.log.ErrorContext(r.Context(), "tap logo: no resolved identity on the request",
			"hint", "mount Tap.chain in front of the tap logo route", "err", id.Err)
		writeLogoUnavailable(w, b.log)
	default:
		writeLogoNotFound(w, b.log)
	}
}

// serve answers for the business tenantID, which the caller took from a live session.
func (b *BrandLogos) serve(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	serveLogo(w, r, b.log, tenantID, b.logos.Logo)
}

// logoRead is one business's logo bytes by digest: tenant.BrandReader.Logo for the two
// routes above, tenant.BrandReader.ActivationLogo for the activation wizard's (M10
// WL-13). Each answers tenant.ErrLogoNotFound for every digest the business does not
// hold -- and ActivationLogo for every digest of a business VIES did not verify.
type logoRead func(ctx context.Context, tenantID uuid.UUID, digest string) (tenant.StoredLogo, error)

// serveLogo is the answer of every logo route (WL-6's two and, since M10 WL-13, the
// activation wizard's): the business is tenantID, which the caller resolved from its
// surface's identity, and the bytes are read's. One function, so the three routes send
// the same 200, the same 304 rule and the same refusal.
func serveLogo(w http.ResponseWriter, r *http.Request, log *slog.Logger, tenantID uuid.UUID, read logoRead) {
	digest := chi.URLParam(r, "sha")
	if !isLogoDigest(digest) {
		writeLogoNotFound(w, log)
		return
	}
	logo, err := read(r.Context(), tenantID, digest)
	switch {
	case errors.Is(err, tenant.ErrLogoNotFound):
		writeLogoNotFound(w, log)
		return
	case err != nil:
		// The tenant and the error; not the digest and never the bytes (ADR 0024 §6's
		// rule for the upload's log, applied to the read).
		log.ErrorContext(r.Context(), "logo: reading the logo failed", "tenant_id", tenantID, "err", err)
		writeLogoUnavailable(w, log)
		return
	}
	name, ok := logoFileName(logo.MIME)
	if !ok {
		// Unreachable while migration 00028's type CHECK stands. Not served: a type
		// this file cannot name is a type it cannot vouch for.
		log.ErrorContext(r.Context(), "logo: the stored type is not one this route serves", "tenant_id", tenantID)
		writeLogoUnavailable(w, log)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", logoCacheControl)
	h.Set("ETag", `"`+digest+`"`)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", logoPolicy)
	h.Set("Cross-Origin-Resource-Policy", logoResource)
	// 🔴 If-None-Match IS EVALUATED HERE, AFTER THE ROW WAS FOUND, AND NOWHERE EARLIER.
	// RFC 9110 §13.2.1 orders it so: a response that would not be 2xx without the
	// precondition ignores it. So the header can change the answer only for the
	// session's own logo -- on another business's digest the lookup has already
	// answered 404, and that 404 does not depend on what the header says.
	if ifNoneMatchNames(r.Header.Values("If-None-Match"), h.Get("ETag")) {
		// Content-Type and Content-Disposition describe the payload a 304 does not
		// carry; net/http deletes Content-Type from a 304 on the wire anyway.
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", logo.MIME)
	h.Set("Content-Disposition", `inline; filename="`+name+`"`)
	h.Set("Content-Length", strconv.Itoa(len(logo.Data)))
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(logo.Data); err != nil {
		log.WarnContext(r.Context(), "logo: writing the response failed", "tenant_id", tenantID, "err", err)
	}
}

// isLogoDigest reports whether s is 64 lower-case hex digits: the only spelling a
// stored digest has. Checked before the logo read, so a malformed segment never
// reaches GetTenantLogo. It is NOT before every database round trip: the chains in
// front of serve have already resolved the session (Identify, requireAdmin) by the
// time this runs.
func isLogoDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// logoFileName is the file name a stored type is served under: the type decides it,
// not a constant (WL-0's correction -- a JPEG named logo.png is a wrong extension).
func logoFileName(mime string) (string, bool) {
	switch mime {
	case "image/png":
		return "logo.png", true
	case "image/jpeg":
		return "logo.jpg", true
	default:
		return "", false
	}
}

// ifNoneMatchNames reports whether the If-None-Match field lines name etag, by the
// weak comparison RFC 9110 §13.1.2 prescribes for this header, or are "*".
//
// The member scan is net/http's (fs.go, checkIfNoneMatch): a "*" or a matching tag met
// before anything malformed answers yes -- so `*garbage`, `"<etag>" junk` and
// `"<etag>", garbage` are a match -- and a malformed member stops the scan, so a match
// written AFTER it is not seen. Where this differs from net/http is the field LINES:
// net/http reads only the first line, this joins them all, so [`"x"`, `"<etag>"`] is a
// match here and not there. Both shapes are measured in
// TestIfNoneMatch_TheScanFollowsRFC9110; either way the answer can only turn the
// session's OWN logo into a 304, because serve calls this after that logo was found.
func ifNoneMatchNames(lines []string, etag string) bool {
	buf := strings.Join(lines, ",")
	for {
		buf = textproto.TrimString(buf)
		switch {
		case buf == "":
			return false
		case buf[0] == ',':
			buf = buf[1:]
			continue
		case buf[0] == '*':
			return true
		}
		tag, rest := scanEntityTag(buf)
		if tag == "" {
			return false
		}
		if strings.TrimPrefix(tag, "W/") == strings.TrimPrefix(etag, "W/") {
			return true
		}
		buf = rest
	}
}

// scanEntityTag reads one entity-tag (W/"…" or "…") from the front of s and returns it
// and what follows; "" when s does not start with one.
func scanEntityTag(s string) (tag, rest string) {
	start := 0
	if strings.HasPrefix(s, "W/") {
		start = 2
	}
	if len(s[start:]) < 2 || s[start] != '"' {
		return "", ""
	}
	for i := start + 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c == 0x21 || c >= 0x23 && c <= 0x7E || c >= 0x80:
		case c == '"':
			return s[:i+1], s[i+1:]
		default:
			return "", ""
		}
	}
	return "", ""
}

// writeLogoNotFound is the one refusal of the two routes. no-store, because the same
// URL answers 200 to a session that holds the logo, and a cached 404 would outlive the
// session that earned it.
func writeLogoNotFound(w http.ResponseWriter, log *slog.Logger) {
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(logoNotFoundBody)))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusNotFound)
	if _, err := io.WriteString(w, logoNotFoundBody); err != nil {
		log.Warn("logo: writing the refusal failed", "err", err)
	}
}

// writeLogoUnavailable answers a logo request the server could not serve. It is reached
// on three branches and none of them depends on another business: the logo read failed
// or returned a row this route cannot describe (both after a read under the session's
// own business), or the tap handler found no resolved identity on the request (no
// read at all -- tapLogo's SessionUnresolved branch;
// TestLogoRoutes_AnUnresolvedIdentityIsNotNoSession).
func writeLogoUnavailable(w http.ResponseWriter, log *slog.Logger) {
	const body = "Something went wrong on our side.\n"
	h := w.Header()
	h.Set("Content-Type", "text/plain; charset=utf-8")
	h.Set("Content-Length", strconv.Itoa(len(body)))
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusInternalServerError)
	if _, err := io.WriteString(w, body); err != nil {
		log.Warn("logo: writing the error failed", "err", err)
	}
}
