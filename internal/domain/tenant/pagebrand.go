package tenant

// pagebrand.go -- a business's brand as the tap and result screens draw it (M10 WL-9;
// ADR 0023 §2, §3, §6; ADR 0024 §5, §7). The reads are Directory's: the tap screen's
// runs inside TapPage's own transaction, the result screen's in ResultBrand.
//
// §4.6: A BRAND THAT CANNOT BE READ NEVER COSTS THE PAGE. Every failure of the brand
// read -- a database error, a logo row only partly described (logoRefOf's fourteen
// partial combinations), a stored accent that is not the canonical spelling -- turns
// into the zero PageBrand, which the screens draw as Taptime's own page, and into an
// error the caller logs. "Partly described" is NOT ErrLogoNotFound and is not read as
// "no logo": it is a read error (ADR 0024's WL-6 note, hand-off to WL-8/WL-9).
//
// AN ACCENT THE GATE REFUSES TODAY IS NOT A FAILURE, and the panel chrome draws the
// same conclusion from the same row (accentOf, brandread.go -- one read-side gate for
// both surfaces since the merge with M10 WL-8): the palette or the thresholds moved
// after the accent was saved, so the page draws no accent -- the theme route would
// 404 it anyway -- keeps the logo, and the caller logs AccentRefused at WARN.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/store"
)

// ErrBrandUnread reports that a page's facts were read but its business's brand was
// not. TapPage returns it WITH a populated TapPageFacts whose Brand is the zero value
// -- the ErrForeignLocation contract, for the same reason: the caller still serves
// the page, with Taptime's own look, and logs the cause wrapped beside this value.
var ErrBrandUnread = errors.New("tenant: the business's brand could not be read")

// errBrandRollback ends TapPage's transaction when the brand read failed. The reads
// before it succeeded and are kept; a failed statement aborts a Postgres transaction,
// so committing it would fail and turn a missing logo into a missing page.
var errBrandRollback = errors.New("tenant: brand read failed; rolling back the page's reads")

// PageBrand is a business's brand as a page draws it.
type PageBrand struct {
	// Name is the business's name: the logo's alt text. Set only with a logo.
	Name string
	// Logo is the logo to draw; HasLogo says whether there is one.
	Logo    LogoRef
	HasLogo bool
	// Accent is the stored accent, which passed brand.Check on THIS read (ADR 0023
	// §3: the same gate on the read side, because a palette change can turn a saved
	// accent illegible). HasAccent says whether there is one. ResultBrand never sets
	// it: D-C keeps the accent off the result screen.
	Accent    brand.Color
	HasAccent bool
	// AccentRefused: an accent is stored and brand.Check refuses it today (accentOf's
	// third answer). There is then no accent to draw; the caller logs it.
	AccentRefused bool
}

// pageBrandOf turns GetTenantBrand's answer into a page's brand: logoRefOf's
// all-or-none rule for the four logo columns, and accentOf's read-side gate for the
// accent (the panel chrome's, brandread.go). No row and an all-NULL row are the zero
// PageBrand with no error.
func pageBrandOf(row store.GetTenantBrandRow, err error) (PageBrand, error) {
	ref, has, err := logoRefOf(row, err)
	if err != nil {
		return PageBrand{}, err
	}
	accent, hasAccent, refused, err := accentOf(row.Accent)
	if err != nil {
		return PageBrand{}, err
	}
	return PageBrand{Logo: ref, HasLogo: has, Accent: accent, HasAccent: hasAccent, AccentRefused: refused}, nil
}

// ResultBrand reads what the result screen draws of a business's brand: its logo and
// its name for the logo's alt, and no accent (D-C). A business with no logo gets the
// zero PageBrand. It runs in a transaction of its own that only reads, AFTER the
// tap's record committed: a read inside the transaction that writes the attendance
// record would let a display read's failure abort that write (§4.6).
//
// The tenant is the SESSION's; the caller decides first that the plaque is on a wall
// of that business (ADR 0023 §2's mismatch rule).
//
// Every statement of the read runs under ctx, so the read ends when ctx does: the
// confirmation screen's bound on this read is a context (internal/handler,
// resultBrandWait), and a read under a context of its own would void it
// (TestResultBrandDB_TheReadEndsWhenItsContextDoes). The one statement that does not
// is WithTenant's ROLLBACK after a failure, which runs under context.Background() so
// that it is never skipped (internal/db/tenant.go).
func (d *Directory) ResultBrand(ctx context.Context, tenantID uuid.UUID) (PageBrand, error) {
	if tenantID == uuid.Nil {
		return PageBrand{}, errors.New("tenant: result brand: no tenant")
	}
	var out PageBrand
	err := d.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		b, err := pageBrandOf(q.GetTenantBrand(ctx, tenantID))
		if err != nil {
			return err
		}
		if !b.HasLogo {
			return nil
		}
		t, err := q.GetTenantClock(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("load the business's name: %w", err)
		}
		out = PageBrand{Name: t.Name, Logo: b.Logo, HasLogo: true}
		return nil
	})
	if err != nil {
		return PageBrand{}, fmt.Errorf("%w: %w", ErrBrandUnread, err)
	}
	return out, nil
}
