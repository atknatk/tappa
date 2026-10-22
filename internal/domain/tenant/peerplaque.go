package tenant

// peerplaque.go -- the tap screen's and the logo route's half of a tap on a PEER's plaque (M11
// API-1b; ADR 0027 §5, §10; the user's decision K-1c): an employee of one member of a holding
// (KM) in front of a plaque of another member (KF) sees KF's venue and KF's brand -- the logo
// and the accent on the tap screen, the logo on the result screen -- because the plaque is KF's.
//
// 🔴 THE ONLY WAY TO A PEER'S ROW IS ADR 0027 §2's CLOSED LIST, ON THE SESSION'S CONTEXT. Both
// reads below run in WithTenant(the SESSION's tenant) and call holding_tap_site / holding_logo,
// which take the caller's tenant from app.tenant_id and answer only for a plaque of a PEER --
// strictly (left_at > now), with the logo only for a business VIES verified. There is no
// WithTenant(site) here (ADR 0016 §2). A plaque of a tenant that is not a peer answers nothing:
// the page is then the ordinary foreign-plaque page (no venue, no brand) and the POST's
// sys:tenant-mismatch refuses it.
//
// K-39: holding_tap_site's static_ips and GPS are read in the same row and DROPPED here -- the
// page carries a name, a venue and a brand, never evidence (db.HoldingSite redacts itself from
// logs and fmt as well).

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

// PeerTapPage loads the tap screen's facts for the plaque uid when it belongs to a PEER of the
// session's business: the employee's greeting (the session's own row), the peer's venue name
// and the peer's brand. site is the peer business holding_tap_site named -- uuid.Nil is "not a
// peer, or no such plaque", the ordinary foreign-plaque page. It is an id to compare (K-35: is
// this the pending invitation's business?), never one to render.
//
// The brand follows the screen's rules for a business's brand (ADR 0023 §2, §3): only for a
// plaque on a wall; the accent through the read-side gate (accentOf); the logo only when
// holding_tap_site gave one (VIES verified -- ADR 0027 §10). The logo is served by the tap
// route's PEER form, which asks holding_logo for this plaque's business (PageBrand.PeerTag).
// A brand that cannot be read never costs the page: the facts come back with a zero Brand and
// ErrBrandUnread wrapping the cause, peer still true.
//
// THE PREDICTION IS THE EMPLOYER'S CHAIN (ADR 0032 §B.2, ADR 0027 §7): read on the SESSION's
// context like everything about the person -- the employer's cutoff, zone and open check-in --
// and only for a peer's plaque on a wall, when predict is set (TapPage's rule). A failed
// prediction read costs nothing else: Prediction nil, PredictionErr the cause.
func (d *Directory) PeerTapPage(ctx context.Context, tenantID, employeeID uuid.UUID, uid string, predict bool) (TapPageFacts, uuid.UUID, error) {
	if tenantID == uuid.Nil || employeeID == uuid.Nil || uid == "" {
		return TapPageFacts{}, uuid.Nil, errors.New("tenant: peer tap page: tenant, employee and plaque are required")
	}
	var (
		out      TapPageFacts
		site     db.HoldingSite
		peer     bool
		brandErr error
		predErr  error
	)
	err := d.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		emp, err := q.GetEmployeeActivationContext(ctx, store.GetEmployeeActivationContextParams{
			TenantID: tenantID, EmployeeID: employeeID,
		})
		if err != nil {
			return fmt.Errorf("load employee: %w", err)
		}
		out.EmployeeName = emp.FullName
		if site, peer, err = db.HoldingOn(tx).TapSite(ctx, uid); err != nil || !peer || site.LocationID == nil || !predict {
			return err
		}
		if out.Prediction, predErr = d.predictTap(ctx, q, tenantID, employeeID); predErr != nil {
			return errPredictionRollback
		}
		return nil
	})
	if errors.Is(err, errPredictionRollback) {
		out.Prediction, out.PredictionErr = nil, fmt.Errorf("tenant: peer tap page: %w", predErr)
		err = nil
	}
	if err != nil {
		return TapPageFacts{}, uuid.Nil, fmt.Errorf("tenant: peer tap page: %w", err)
	}
	if !peer {
		return out, uuid.Nil, nil
	}
	if site.LocationID == nil {
		return out, site.SiteTenantID, nil
	}
	out.LocationName = site.LocationName
	out.Brand, brandErr = peerBrandOf(site, uid)
	if brandErr != nil {
		// TapPageFacts' rule: a page without its brand has no prediction either.
		out.Brand, out.Prediction = PageBrand{}, nil
		return out, site.SiteTenantID, fmt.Errorf("%w: %w", ErrBrandUnread, brandErr)
	}
	return out, site.SiteTenantID, nil
}

// peerBrandOf is a peer plaque's brand as the tap screen draws it: the accent through the
// read-side gate, and the logo with its alt when holding_tap_site gave all four of its fields
// (it gives them together or not at all, and only for a business VIES verified).
func peerBrandOf(site db.HoldingSite, uid string) (PageBrand, error) {
	accent, hasAccent, refused, err := accentOf(site.Accent)
	if err != nil {
		return PageBrand{}, err
	}
	out := PageBrand{Accent: accent, HasAccent: hasAccent, AccentRefused: refused}
	set := 0
	for _, present := range []bool{site.LogoSHA256 != nil, site.LogoMIME != nil, site.LogoWidth != nil, site.LogoHeight != nil} {
		if present {
			set++
		}
	}
	switch set {
	case 0:
		return out, nil
	case 4:
	default:
		return PageBrand{}, errors.New("the peer's logo is only partly described")
	}
	out.Logo = LogoRef{SHA256: *site.LogoSHA256, MIME: *site.LogoMIME, Width: int(*site.LogoWidth), Height: int(*site.LogoHeight)}
	out.HasLogo, out.PeerTag = true, uid
	if site.BusinessName != nil {
		out.Name = *site.BusinessName
	}
	return out, nil
}

// PeerLogo reads the logo bytes of the business whose plaque uid is, when that business is a
// PEER of tenantID's (the session's), VIES verified, and holds exactly digest now -- holding_logo,
// on the session's context. Every other case -- not a peer, an old digest, another business's
// digest, an unverified business, no such plaque -- is ErrLogoNotFound, one answer (ADR 0024 §5's
// byte-identical 404). The caller validates the spelling of uid and digest at the HTTP boundary.
func (r *BrandReader) PeerLogo(ctx context.Context, tenantID uuid.UUID, uid, digest string) (StoredLogo, error) {
	if tenantID == uuid.Nil {
		return StoredLogo{}, errors.New("tenant: peer logo: no tenant")
	}
	var (
		out   StoredLogo
		found bool
	)
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		l, ok, err := db.HoldingOn(tx).Logo(ctx, uid, digest)
		if err != nil {
			return err
		}
		if ok && (l.MIME == "" || len(l.Data) == 0) {
			// Unreachable while migration 00028's all-or-none CHECK stands; an error, as in Logo.
			return errors.New("the peer's stored logo has no type or no bytes")
		}
		out, found = StoredLogo{Data: l.Data, MIME: l.MIME}, ok
		return nil
	})
	if err != nil {
		return StoredLogo{}, fmt.Errorf("tenant: peer logo: %w", err)
	}
	if !found {
		return StoredLogo{}, ErrLogoNotFound
	}
	return out, nil
}
