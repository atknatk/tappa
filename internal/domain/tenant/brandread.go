package tenant

// brandread.go -- the READ side of a business's brand (M10 WL-6; ADR 0024 §5): the
// logo's bytes for the two logo routes, and whether a page has a logo to draw.
//
// IT IS A TYPE OF ITS OWN, NOT A METHOD ON Brands. Brands is the writer (WL-4) and
// carries the audit trail; a page view and an image request need neither. Keeping the
// reader behind its own constructor means the handlers that serve images hold no value
// with a Save method on it -- the split internal/handler already makes between a
// section's reader and its writer (adminlogin.go, the rules / scribe fields).
//
// 🔴 THE TENANT IS AN ARGUMENT, AND THE CALLER TAKES IT FROM A RESOLVED SESSION. Both
// reads run inside db.WithTenant for that tenant AND name it in their WHERE (section
// 4.5, belt and braces). A digest is not an authority: two businesses that upload the
// same bytes hold the same digest (ADR 0024 §4), and each read finds only its own row.
//
// THE CLAIM, IN THREE PARTS.
//
// PART I -- THE SHIPPED CODE, MEASURED against the dev Postgres as tappa_app
// (brandread_db_test.go):
//   - Logo returns the stored bytes and type for this business's digest; for the other
//     business's digest (whose own read finds it), for a digest nobody stored, for an
//     upper-case spelling, for this business's previous digest after a new logo
//     replaced it, and for a digest both businesses held once this one cleared its logo,
//     it returns ErrLogoNotFound itself -- one value, one text -- and the reads change
//     nothing in the other business's row
//     (TestBrandReadDB_ALogoIsFoundOnlyInItsOwnBusiness);
//   - PageLogo answers "no logo" without an error for a business with no brand row,
//     for one whose row has every brand field NULL, and for one with an accent and no
//     logo; and the digest, type and box for one with a logo
//     (TestBrandReadDB_NoRowAndAnAllNullRowAreTheSameNoLogo); logoRefOf gives the
//     no-row, all-NULL and accent-only answers as one value, and keeps a read error and
//     each of the fourteen partial combinations of the four logo columns as errors --
//     the reading side's own all-or-none rule, for rows 00028's CHECK keeps out of the
//     table (TestBrandRead_NoRowAndAnAllNullRowAreOneBranch);
//   - a nil tenant is refused before a transaction is opened, and a failing database
//     is an error, never ErrLogoNotFound (TestBrandRead_ANilTenantOpensNoTransaction);
//   - (WL-8) PanelBrand returns each business's own name, accent and logo in one
//     transaction with one statement (TestBrandReadDB_PanelBrandIsOneStatementForItsOwnBusiness);
//     no row, an all-NULL row and an accent the gate refuses without a logo are the
//     zero value -- the last with AccentRefused -- and a refused accent beside a logo is
//     the logo with the name (TestBrandReadDB_PanelBrandUnbrandedShapesAreTheZeroValue);
//     panelBrandOf keeps a read error, each of the fourteen half-described logos and
//     each malformed stored accent an error, and gives the name only to a branded
//     answer (TestBrandRead_PanelBrandOfKeepsEveryNonBrandAnError); a nil tenant opens
//     no transaction and a failing database is an error
//     (TestBrandRead_PanelBrandNeedsATenantAndReportsAFailingDatabase);
//     TestBrandRead_TheBeltSeesThePanelRead fails if GetTenantPanelBrand leaves this
//     package's derived belt.
//
// PART II -- NAMED PINS: the tests above, and TestStaffQueries_CarryAnExplicitTenantPredicate
// (query_test.go), which derives its query list from this package's store calls and so,
// since this file, lists GetTenantLogo beside GetTenantBrand and reads each statement's
// text for its @tenant_id predicate; TestBrandRead_TheBeltSeesBothReads fails if either
// call leaves this package. TestBrandReadDB_ALogoIsFoundOnlyInItsOwnBusiness and
// internal/handler's TestLogoRoutesDB_AnotherBusinessesDigestIsAnUnknownDigest stay
// green when GetTenantLogo's predicate is removed or OR-ed with true (RLS hides the
// other business's row); the belt is what turns red. Each mutation that was run is in
// ADR 0024's WL-6 note.
//
// PART III -- No completeness claim: any change the table does not list is the subject
// of code review.

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/store"
)

// ErrLogoNotFound: this business holds no logo with that digest. It is the ONE answer
// for another business's digest, a digest nobody stored and a logo that has since been
// replaced (ADR 0024 §5, Iddia D): GetTenantLogo finds no row in all three, and the
// logo routes turn this value into one 404.
var ErrLogoNotFound = errors.New("tenant: no such logo")

// BrandReader reads a business's brand. Dependencies are injected as struct fields
// (section 7).
type BrandReader struct {
	data Database
}

// NewBrandReader wires it. A nil database is refused rather than deferred to a nil
// dereference on the first image request.
func NewBrandReader(data Database) (*BrandReader, error) {
	if data == nil {
		return nil, errors.New("tenant: nil database")
	}
	return &BrandReader{data: data}, nil
}

// StoredLogo is a logo as the logo routes serve it: the re-encoded bytes WL-3 produced
// and the type they were stored under. The type is the stored column, never a sniff
// of the bytes and never anything a request said (ADR 0024 §5).
type StoredLogo struct {
	Data []byte
	MIME string
}

// Logo reads the bytes of this business's logo whose sha256 is digest. The caller
// validates the digest's spelling at the HTTP boundary (section 7); a digest this
// business does not hold, spelled correctly or not, returns ErrLogoNotFound.
func (r *BrandReader) Logo(ctx context.Context, tenantID uuid.UUID, digest string) (StoredLogo, error) {
	if tenantID == uuid.Nil {
		return StoredLogo{}, errors.New("tenant: logo: no tenant")
	}
	var out StoredLogo
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetTenantLogo(ctx, store.GetTenantLogoParams{
			TenantID: tenantID, LogoSha256: digest,
		})
		if err != nil {
			return err
		}
		// Unreachable while migration 00028's all-or-none CHECK stands (a row found by
		// its digest has its type and bytes); answered as an error rather than as a
		// 404, so an inconsistent row is a log line and not a silent miss.
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
		return StoredLogo{}, fmt.Errorf("tenant: logo: %w", err)
	}
	return out, nil
}

// LogoRef is what a page needs to draw the logo: its digest (the image URL), its type
// and its box (width and height on the <img>, so the page does not shift; ADR 0023 §2).
type LogoRef struct {
	SHA256 string
	MIME   string
	Width  int
	Height int
}

// PageLogo reports whether this business has a logo a page should draw, and which.
// It is the hasLogo a page passes to its Content-Security-Policy (tapCSPFor /
// adminCSPFor, internal/handler): img-src is named only by a page that draws one.
//
// A BUSINESS WITH NO BRAND ROW AND ONE WHOSE ROW HAS EVERY FIELD NULL GET THE SAME
// ANSWER (WL-1's hand-off): a clear is an UPDATE to NULL that leaves the row, and
// clearing a business that had no row creates one (WL-4, Karar 3), so "no row" and
// "all NULL" are two spellings of one state. An accent without a logo is "no logo"
// too: this reads the logo half only.
func (r *BrandReader) PageLogo(ctx context.Context, tenantID uuid.UUID) (LogoRef, bool, error) {
	if tenantID == uuid.Nil {
		return LogoRef{}, false, errors.New("tenant: page logo: no tenant")
	}
	var (
		ref LogoRef
		has bool
	)
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetTenantBrand(ctx, tenantID)
		ref, has, err = logoRefOf(row, err)
		return err
	})
	if err != nil {
		return LogoRef{}, false, fmt.Errorf("tenant: page logo: %w", err)
	}
	return ref, has, nil
}

// logoRefOf turns GetTenantBrand's answer into the page's answer. pgx.ErrNoRows and a
// row whose four logo columns (digest, type, width, height) are all NULL are the same
// "no logo"; any other error is an error.
//
// ALL OR NONE, AS THE READING SIDE'S OWN RULE: four set is a logo; any other count --
// a type without a box, a box without a type, a type and box without a digest -- is
// an error, never a logo with a guessed type or a 0x0 box, and never a silent "no
// logo". Migration 00028's all-or-none CHECK (tenant_branding_logo_all_or_none) keeps
// such a row out of the table, so these branches are unreachable while it stands; the
// rule is here so that the page does not depend on the CHECK to avoid drawing half a
// logo. TestBrandRead_NoRowAndAnAllNullRowAreOneBranch drives all fourteen partial
// combinations.
func logoRefOf(row store.GetTenantBrandRow, err error) (LogoRef, bool, error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return LogoRef{}, false, nil
	case err != nil:
		return LogoRef{}, false, err
	}
	set := 0
	for _, present := range []bool{row.LogoSha256 != nil, row.LogoMime != nil, row.LogoWidth != nil, row.LogoHeight != nil} {
		if present {
			set++
		}
	}
	switch set {
	case 0:
		return LogoRef{}, false, nil
	case 4:
	default:
		return LogoRef{}, false, errors.New("the stored logo is only partly described")
	}
	return LogoRef{
		SHA256: *row.LogoSha256,
		MIME:   *row.LogoMime,
		Width:  int(*row.LogoWidth),
		Height: int(*row.LogoHeight),
	}, true, nil
}

// PanelBrand is the business's brand as the panel chrome draws it (M10 WL-8; ADR 0023
// §2): the business's name, its accent, and its logo. The zero value is "nothing to
// draw", and the chrome then renders as it did before WL-8.
type PanelBrand struct {
	// Name is the business's name. It is set only when Branded is true: the chrome
	// prints it beside a brand and nowhere else.
	Name string
	// Accent is the stored accent; it means something only when HasAccent is true.
	Accent brand.Color
	// HasAccent: an accent is stored AND brand.Check passes it today.
	HasAccent bool
	// AccentRefused: an accent is stored and brand.Check refuses it today -- the palette
	// or the gate's thresholds changed after it was saved (ADR 0023 §3: the database
	// keeps the shape, the code re-checks the colour on the read side too). The page
	// draws no accent, as the theme route would serve none (it 404s the same colour);
	// the caller says so in its log.
	AccentRefused bool
	// Logo is the stored logo's digest, type and box; it means something only when
	// HasLogo is true.
	Logo    LogoRef
	HasLogo bool
}

// Branded reports whether the chrome has anything of the business's to draw: an
// accent that passes the gate, or a logo.
func (b PanelBrand) Branded() bool { return b.HasAccent || b.HasLogo }

// PanelBrand reads the business's brand for the panel chrome: ONE statement,
// GetTenantPanelBrand, by primary key (the chrome's "+1 read" -- m10 WL-8's
// acceptance; EXPLAIN ANALYZE on the WL-8 card).
//
// No brand row, a row whose brand fields are all NULL, and a row whose only accent the
// gate refuses and which has no logo are the zero value (with AccentRefused set for
// the last), not an error. An error is an error: a failed read, a stored accent that
// is not six upper-case hex digits, and a half-described logo (logoRefOf's rule) --
// never quietly "no brand", so the caller logs it (section 4.6; ADR 0024's WL-6 note,
// the hand-off "a half row is to be handled apart").
func (r *BrandReader) PanelBrand(ctx context.Context, tenantID uuid.UUID) (PanelBrand, error) {
	if tenantID == uuid.Nil {
		return PanelBrand{}, errors.New("tenant: panel brand: no tenant")
	}
	var out PanelBrand
	err := r.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetTenantPanelBrand(ctx, tenantID)
		out, err = panelBrandOf(row, err)
		return err
	})
	if err != nil {
		return PanelBrand{}, fmt.Errorf("tenant: panel brand: %w", err)
	}
	return out, nil
}

// panelBrandOf turns GetTenantPanelBrand's answer into the chrome's. The logo half is
// logoRefOf's rule, unchanged (the four logo columns all or none); the accent half is
// accentOf's.
func panelBrandOf(row store.GetTenantPanelBrandRow, err error) (PanelBrand, error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return PanelBrand{}, nil
	case err != nil:
		return PanelBrand{}, err
	}
	ref, hasLogo, err := logoRefOf(store.GetTenantBrandRow{
		LogoSha256: row.LogoSha256, LogoMime: row.LogoMime,
		LogoWidth: row.LogoWidth, LogoHeight: row.LogoHeight,
	}, nil)
	if err != nil {
		return PanelBrand{}, err
	}
	accent, hasAccent, refused, err := accentOf(row.Accent)
	if err != nil {
		return PanelBrand{}, err
	}
	out := PanelBrand{
		Accent: accent, HasAccent: hasAccent, AccentRefused: refused,
		Logo: ref, HasLogo: hasLogo,
	}
	if out.Branded() {
		out.Name = row.Name
	}
	return out, nil
}

// accentOf is the READ side of ADR 0023 §3's gate for a stored accent: none stored is
// (zero, false, false, nil); a stored accent brand.Check passes is (it, true, false,
// nil); one it refuses is (zero, false, true, nil) -- no accent, and the caller logs;
// a stored value that is not the canonical spelling (migration 00028's CHECK keeps it
// out of the table) is an error.
func accentOf(stored *string) (c brand.Color, ok, refused bool, err error) {
	if stored == nil {
		return brand.Color{}, false, false, nil
	}
	c, err = brand.ParseAccent(*stored)
	if err != nil {
		return brand.Color{}, false, false, fmt.Errorf("the stored accent: %w", err)
	}
	if _, err := brand.Check(c); err != nil {
		if errors.Is(err, brand.ErrAccentIllegible) {
			return brand.Color{}, false, true, nil
		}
		return brand.Color{}, false, false, fmt.Errorf("the stored accent: %w", err)
	}
	return c, true, false, nil
}
