package handler

// activationbrand.go -- a business's brand on the activation family (M10 WL-13; ADR
// 0023's WL-13 note, ADR 0024 §5's WL-13 note) and the wizard's own logo route.
//
// THE USER'S DECISIONS (2026-10-09, §9). (1) The wizard's four steps, "Activation
// complete" and "already set up" take the tap screen's shape: the header (a logo in the
// 24 px slot over "taptime · punchless") and the accent. (2) The logo only for a
// business VIES verified; the accent and the Taptime wordmark either way. (3) The accent
// fills the wizard's primary buttons; beside it the wizard's other green marks turn ink.
// The failure screens and the "phone already in use" confirmation stay Taptime's: they
// take no brand (pages.Problem, pages.Confirm), because a business's look is known only
// from a usable invitation and would tell a live link from a dead one.
//
// TWO LOGO ROUTES ACROSS THE SIX SCREENS, AND WHY:
//
//	wizard, steps 1-4         GET /activate/logo/{sha} (this file). The browser holds an
//	                          invitation in the activation cookie and NO session, so the
//	                          tap route -- which answers only a live employee session --
//	                          would 404 it. This route resolves the business from that
//	                          invitation (invite.Lookup, the same resolver the page used).
//	"Activation complete",    GET /t/logo/{sha} (brandlogo.go). The activating tap has
//	"already set up"          spent the invitation and cleared its cookie, and the
//	                          browser now holds a live session; the tap route serves that
//	                          session's business, which is the business these two screens
//	                          name. A cookie-keyed route would have nothing to resolve.
//
// THE VIES GATE IS IN THE READS, NOT IN EITHER ROUTE'S HANDLER: tenant.BrandReader's
// ActivationBrand gives no logo for a business not verified, so none of the six screens
// draws an <img> for one (and none names img-src); ActivationLogo's statement carries
// vat_verified IS TRUE in its WHERE, so this route has no bytes for one. The tap route is
// NOT gated, deliberately: it serves only a live session, and that session sees the
// same logo on the tap screen at its next tap (ADR 0023 §2 draws it for every business);
// a gate there would hide nothing and would change WL-6's route.
//
// THE CLAIM FOR GET /activate/logo/{sha}, IN THREE PARTS. These pins are against
// ACCIDENTAL drift; a deliberate bypass is the subject of code review.
//
// PART I -- THE SHIPPED CODE, MEASURED (activationbrand_test.go against fakes,
// activationbrand_db_test.go against the dev Postgres as tappa_app; every response read
// from rec.Result()):
//   - a usable invitation in the cookie, a business VIES verified and that business's
//     current digest answer 200 with exactly the logo routes' header set (the stored
//     type, the file name it implies, private immutable caching, the ETag, sandboxing
//     policy, same-origin resource policy, nosniff, Content-Length) and the stored bytes
//     (TestActivationLogo_AVerifiedBusinessesLogoIsServedWithTheLogoHeaders);
//   - every refusal is ONE response, byte for byte -- status, every header, body --
//     and it is the WL-6 routes' 404: no cookie, a session cookie alone, a cookie that
//     does not parse, an unknown code, an expired, a spent and a cancelled invitation,
//     an employee who may not activate, a business not verified, a digest nobody
//     stored, another business's digest, a malformed digest; the malformed digest and
//     the requests without a usable cookie never reach the invitation resolver, and no
//     refusal reads the logo of any business but the invitation's
//     (TestActivationLogo_EveryRefusalIsTheSameNotFound); the business's previous digest
//     after a new logo is the same miss at the read (TestActivationBrandDB_TheVIESGateHoldsOnBothReads);
//   - If-None-Match naming the digest answers 304 only for the business's own logo;
//     for a business not verified and for another business's digest the 404 is unchanged
//     (TestActivationLogo_IfNoneMatchIsAnsweredOnlyForTheInvitationsOwnLogo);
//   - a resolver or reader failure is a 500 with no bytes, never the 404 and never a
//     logo (TestActivationLogo_AFailureIsNotARefusal);
//   - every request is charged once to the activation flow's flood ceiling, the budget
//     the wizard's pages spend, BEFORE the resolver runs; past the ceiling the route
//     answers 429 and resolves nothing (TestActivationLogo_ARequestSpendsTheFloodCeiling);
//   - the route writes nothing: no audit_log row and no charge to the invitation's
//     window, for a served logo and for thirty refusals (TestActivationLogo_WritesNothing);
//   - against Postgres: a verified business's logo is served, the same business with
//     vat_verified false and with it NULL gets the 404, and a second business's digest
//     presented with the first business's invitation gets the 404 byte for byte
//     (TestActivationLogoDB_OnlyAVerifiedBusinessesOwnLogoIsServed).
//
// PART II -- NAMED PINS: the tests above, and the mutations in ADR 0023's WL-13 note,
// each with the tests it turned red.
//
// PART III -- Any shape not listed above is the subject of code review -- no completeness claim.

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/web/templates/layout"
)

// activationLogoRoute is the wizard's logo route. {sha} is the logo's sha256 in 64
// lower-case hex digits, as on the WL-6 routes; layout.ActivationLogo writes the same
// prefix.
const activationLogoRoute = "/activate/logo/{sha}"

// logoRoute says which route a branded activation screen draws its logo from.
type logoRoute int

const (
	// wizardLogo: GET /activate/logo/{sha}, keyed by the invitation in the cookie.
	wizardLogo logoRoute = iota
	// sessionLogo: GET /t/logo/{sha}, keyed by the live employee session.
	sessionLogo
)

// lookFor is the activation family's brand for one business, as a page draws it.
//
// §4.6, AS ON THE TAP SCREEN: a brand that cannot be read never costs the page. The
// screen is drawn in Taptime's own look and the failure is ONE error line naming the
// business -- no colour, no digest, no code. An accent the gate refuses today (the
// palette moved after it was saved) is not a failure: the screen draws no accent, keeps
// the logo, and says so at WARN (tap.go's rule).
func (a *Activation) lookFor(ctx context.Context, tenantID uuid.UUID, route logoRoute) layout.Brand {
	b, err := a.brands.ActivationBrand(ctx, tenantID)
	if err != nil {
		a.log.Error("activation: reading the business's brand failed; drawing Taptime's",
			"tenant_id", tenantID, "err", err)
		return layout.Brand{}
	}
	if b.AccentRefused {
		a.log.Warn("activation: the stored accent does not pass the legibility gate today; the screen draws no accent",
			"tenant_id", tenantID)
	}
	var theme layout.Theme
	if b.HasAccent {
		theme = layout.ThemeOf(b.Accent)
	}
	var logo layout.Logo
	if b.HasLogo {
		switch route {
		case wizardLogo:
			logo = layout.ActivationLogo(b.Logo.SHA256, b.Logo.Width, b.Logo.Height, b.Name)
		default:
			logo = layout.TapLogo(b.Logo.SHA256, b.Logo.Width, b.Logo.Height, b.Name)
		}
	}
	return layout.ActivationBrand(logo, theme)
}

// Logo serves GET /activate/logo/{sha}: the logo of the business whose USABLE invitation
// this browser's activation cookie carries, if that business is VIES verified and {sha}
// is its current logo. Everything else is writeLogoNotFound -- one response.
//
// ORDER, and why:
//
//	· flood ceiling      before any database work (the page's own first gate).
//	· digest shape       no resolver for a segment that cannot be a digest.
//	· cookie             no cookie, no invitation: answered without the database.
//	· invite.Lookup      the page's resolver: the invitation must be usable now --
//	                     an expired, spent or cancelled one, or an employee who may no
//	                     longer activate, is the 404. A resolver failure is a 500.
//	· serveLogo          the bytes, read under the invitation's business with the VIES
//	                     gate in the statement (ActivationLogo); the WL-6 routes' 200,
//	                     304 rule and 404.
//
// IT WRITES NOTHING. An image request is not an attempt to activate: no audit_log row,
// no charge to the invitation's window, no log line for a refusal -- the flood ceiling
// is what bounds it, and the page the image sits on has already done the recording.
func (a *Activation) Logo(w http.ResponseWriter, r *http.Request) {
	if a.flooded(w, r, clientIP(r), "activate_logo") {
		return
	}
	if !isLogoDigest(chi.URLParam(r, "sha")) {
		writeLogoNotFound(w, a.log)
		return
	}
	st, ok := a.codes.read(r)
	if !ok {
		writeLogoNotFound(w, a.log)
		return
	}
	ictx, err := a.invites.Lookup(r.Context(), st.code)
	if err != nil {
		if _, classified := inviteFailureReason(err); classified || errors.Is(err, invite.ErrUnknownCode) {
			writeLogoNotFound(w, a.log)
			return
		}
		// Not "this link is dead": the resolver failed. A 500 with no bytes, as the WL-6
		// routes answer a failed read -- an outage is not a refusal.
		a.log.Error("activation logo: resolving the invitation failed", "err", err)
		writeLogoUnavailable(w, a.log)
		return
	}
	serveLogo(w, r, a.log, ictx.TenantID, a.brands.ActivationLogo)
}
