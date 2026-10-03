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
//     is an error, never ErrLogoNotFound (TestBrandRead_ANilTenantOpensNoTransaction).
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
