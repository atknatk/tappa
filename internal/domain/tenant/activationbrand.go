package tenant

// activationbrand.go -- a business's brand as the ACTIVATION FAMILY draws it (M10 WL-13;
// ADR 0023's WL-13 note, ADR 0024 §5's WL-13 note): the wizard's four steps, "Activation
// complete" and "already set up".
//
// THE USER'S DECISIONS (2026-10-09), and where each one is kept:
//
//  1. Those six screens take the tap screen's shape: the header (a logo in the 24 px
//     slot over the co-brand line) and the accent -- the layout's job.
//  2. THE LOGO ONLY FOR A BUSINESS VIES VERIFIED (tenants.vat_verified IS TRUE; FALSE and
//     NULL, never asked or no answer, are both "not verified"). The accent and the
//     Taptime wordmark stay either way. This is EM-7B's decision for the invitation
//     e-mail, applied to the screens the same e-mail opens: a stranger's activation link
//     is where a look-alike logo would do its work (ADR 0005 risk 9). KEPT HERE, in the
//     two reads below: ActivationBrand drops the logo of an unverified business before a
//     page sees it (activationBrandOf), and ActivationLogo's statement carries the gate
//     in its WHERE, so the wizard's logo route has no bytes to serve for one.
//  3. The accent fills the wizard's primary button; beside it the wizard's other green
//     marks turn ink -- the template's job.
//
// §4.6 AS ON THE TAP SCREEN: an error from ActivationBrand never costs the page. The
// caller draws Taptime's own page and logs the tenant id (internal/handler, activate.go).

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// ActivationBrand reads what the activation family draws of a business's brand: the
// logo (only for a business VIES verified), its name for the logo's alt (only beside a
// logo), and the accent through the read-side gate (accentOf: one the gate refuses
// today is AccentRefused, not an error). ONE statement, GetTenantActivationBrand.
//
// No brand row and a row whose brand fields are all NULL are the zero PageBrand. An
// error is an error -- a failed read, a stored accent that is not the canonical
// spelling, a logo only partly described (logoRefOf's rule; for an unverified business
// too, because a half row is a fault whoever owns it) -- and never quietly "no brand".
func (r *BrandReader) ActivationBrand(ctx context.Context, tenantID uuid.UUID) (PageBrand, error) {
	if tenantID == uuid.Nil {
		return PageBrand{}, errors.New("tenant: activation brand: no tenant")
	}
	var out PageBrand
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetTenantActivationBrand(ctx, tenantID)
		out, err = activationBrandOf(row, err)
		return err
	})
	if err != nil {
		return PageBrand{}, fmt.Errorf("tenant: activation brand: %w", err)
	}
	return out, nil
}

// activationBrandOf turns GetTenantActivationBrand's answer into the page's brand. The
// logo half is logoRefOf's all-or-none rule, then THE VIES GATE: a logo of a business
// whose vat_verified is not TRUE is not drawn, and its name (the logo's alt) is not
// carried either. The accent half is accentOf's, unchanged by the gate.
func activationBrandOf(row store.GetTenantActivationBrandRow, err error) (PageBrand, error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return PageBrand{}, nil
	case err != nil:
		return PageBrand{}, err
	}
	ref, hasLogo, err := logoRefOf(store.GetTenantBrandRow{
		LogoSha256: row.LogoSha256, LogoMime: row.LogoMime,
		LogoWidth: row.LogoWidth, LogoHeight: row.LogoHeight,
	}, nil)
	if err != nil {
		return PageBrand{}, err
	}
	accent, hasAccent, refused, err := accentOf(row.Accent)
	if err != nil {
		return PageBrand{}, err
	}
	out := PageBrand{Accent: accent, HasAccent: hasAccent, AccentRefused: refused}
	if hasLogo && row.VatVerified {
		out.Logo, out.HasLogo, out.Name = ref, true, row.Name
	}
	return out, nil
}

// ActivationLogo reads the bytes of this business's logo whose sha256 is digest, for
// the activation wizard's logo route -- and ONLY for a business VIES verified: the
// statement's WHERE carries vat_verified IS TRUE beside the tenant and the digest. So a
// business not verified, another business's digest, a digest nobody stored and a logo
// since replaced are one answer, ErrLogoNotFound itself, as on the logo routes of WL-6.
//
// The caller validates the digest's spelling at the HTTP boundary (section 7) and takes
// tenantID from the invitation it has just resolved -- never from the request.
func (r *BrandReader) ActivationLogo(ctx context.Context, tenantID uuid.UUID, digest string) (StoredLogo, error) {
	if tenantID == uuid.Nil {
		return StoredLogo{}, errors.New("tenant: activation logo: no tenant")
	}
	var out StoredLogo
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetTenantActivationLogo(ctx, store.GetTenantActivationLogoParams{
			TenantID: tenantID, LogoSha256: digest,
		})
		if err != nil {
			return err
		}
		// Unreachable while migration 00028's all-or-none CHECK stands; an error rather
		// than a 404, as in Logo.
		if row.LogoMime == nil || len(row.Logo) == 0 {
			return errors.New("the stored logo has no type or no bytes")
		}
		out = StoredLogo{Data: row.Logo, MIME: *row.LogoMime}
		return nil
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredLogo{}, ErrLogoNotFound
	}
	if err != nil {
		return StoredLogo{}, fmt.Errorf("tenant: activation logo: %w", err)
	}
	return out, nil
}
