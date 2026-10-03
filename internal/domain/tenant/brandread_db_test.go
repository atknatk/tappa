package tenant

// brandread_db_test.go -- M10 WL-6's brand READS against real Postgres, as tappa_app
// (CLAUDE.md §8): the logo routes' byte read and a page's hasLogo. What is measured
// here is what a fake database would agree with whatever the code did -- that a digest
// held by ANOTHER business finds nothing in this one, and that "no brand row" and "a
// row whose fields are all NULL" come back as the same answer.
//
// These run the PRODUCTION read path (WithTenant + the query's own tenant predicate);
// the isolation test that reads WITHOUT a WHERE, to prove RLS on its own, is WL-1's
// (internal/db/branding_test.go). Fixtures are not cleaned up, for brand_db_test.go's
// reason: tappa_app holds no DELETE on tenant_branding and audit_log is append-only.
// The last three tests need no database: the seam where GetTenantBrand's answer
// becomes "no logo", the nil-tenant refusal, and where this read's belt comes from.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

func newBrandReaderOn(t *testing.T, data Database) *BrandReader {
	t.Helper()
	r, err := NewBrandReader(data)
	if err != nil {
		t.Fatalf("NewBrandReader: %v", err)
	}
	return r
}

// unstoredDigest is the sha256 of fresh random bytes: a correctly spelled digest
// nobody stored.
func unstoredDigest(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// assertNotFound requires the ONE refusal value -- the same value and the same text
// for every way of not finding a logo, so the routes can turn it into one 404.
func assertNotFound(t *testing.T, what string, logo StoredLogo, err error) {
	t.Helper()
	// The VALUE itself is the contract: one sentinel, unwrapped, so its text is the
	// same for every miss.
	if err != ErrLogoNotFound {
		t.Fatalf("%s: err = %v, want ErrLogoNotFound itself (unwrapped, so its text is the same for every miss)", what, err)
	}
	if logo.Data != nil || logo.MIME != "" {
		t.Fatalf("%s: a refusal carried a logo (%d bytes, %q)", what, len(logo.Data), logo.MIME)
	}
}

func TestBrandReadDB_ALogoIsFoundOnlyInItsOwnBusiness(t *testing.T) {
	f := newBrandFixture(t)
	ctx := context.Background()
	r := newBrandReaderOn(t, f.data)

	mine := f.pngLogo(t, 48, 32, 11)
	theirs := f.jpegLogo(t, 40, 40, 77)
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: f.tenant, ActorID: f.owner, Logo: mine}); err != nil {
		t.Fatalf("seed this business's logo: %v", err)
	}
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: f.foreign, ActorID: f.foreignOwner, Logo: theirs}); err != nil {
		t.Fatalf("seed the other business's logo: %v", err)
	}
	foreignBefore := f.row(t, f.foreign)

	// This business's own digest: the stored bytes and the stored type.
	got, err := r.Logo(ctx, f.tenant, mine.SHA256)
	if err != nil {
		t.Fatalf("own digest: %v", err)
	}
	if !slices.Equal(got.Data, mine.Data) || got.MIME != "image/png" {
		t.Fatalf("own digest: %d bytes %q, want the stored %d bytes image/png", len(got.Data), got.MIME, len(mine.Data))
	}

	// POSITIVE CONTROL for the refusal below: the other business's row is real and its
	// digest IS found -- in its own business. Without this, "not found" could mean the
	// fixture never stored it.
	if got, err := r.Logo(ctx, f.foreign, theirs.SHA256); err != nil || !slices.Equal(got.Data, theirs.Data) || got.MIME != "image/jpeg" {
		t.Fatalf("control: the other business's own read: %v (%d bytes %q)", err, len(got.Data), got.MIME)
	}

	// The other business's digest and a digest nobody stored: the same value.
	foreignLogo, foreignErr := r.Logo(ctx, f.tenant, theirs.SHA256)
	assertNotFound(t, "the other business's digest", foreignLogo, foreignErr)
	unknownLogo, unknownErr := r.Logo(ctx, f.tenant, unstoredDigest(t))
	assertNotFound(t, "a digest nobody stored", unknownLogo, unknownErr)
	if foreignErr.Error() != unknownErr.Error() {
		t.Fatalf("the two misses read differently: %q vs %q", foreignErr, unknownErr)
	}
	// A spelling no stored digest has (the routes refuse it before calling; the read
	// still finds nothing).
	upper, upperErr := r.Logo(ctx, f.tenant, "A"+mine.SHA256[1:])
	assertNotFound(t, "an upper-case spelling of the own digest", upper, upperErr)

	// Reading wrote nothing in the other business.
	if after := f.row(t, f.foreign); after != foreignBefore {
		t.Fatalf("the other business's row changed under reads: %+v -> %+v", foreignBefore, after)
	}

	// A replaced logo's old digest is gone from its own business too.
	next := f.pngLogo(t, 48, 32, 12)
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: f.tenant, ActorID: f.owner, Logo: next}); err != nil {
		t.Fatalf("replace the logo: %v", err)
	}
	old, oldErr := r.Logo(ctx, f.tenant, mine.SHA256)
	assertNotFound(t, "the replaced logo's digest", old, oldErr)
	if got, err := r.Logo(ctx, f.tenant, next.SHA256); err != nil || !slices.Equal(got.Data, next.Data) {
		t.Fatalf("the new logo: %v", err)
	}

	// THE SAME BYTES IN TWO BUSINESSES HOLD ONE DIGEST, and each read still finds only
	// its own row: once this business clears its logo, the digest it shares with the
	// other one is not found HERE while it still is THERE.
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: f.foreign, ActorID: f.foreignOwner, Logo: next}); err != nil {
		t.Fatalf("the other business uploads the same bytes: %v", err)
	}
	if err := f.brands.ClearLogo(ctx, BrandClearCommand{TenantID: f.tenant, ActorID: f.owner}); err != nil {
		t.Fatalf("clear this business's logo: %v", err)
	}
	shared, sharedErr := r.Logo(ctx, f.tenant, next.SHA256)
	assertNotFound(t, "a digest the other business holds and this one cleared", shared, sharedErr)
	if got, err := r.Logo(ctx, f.foreign, next.SHA256); err != nil || !slices.Equal(got.Data, next.Data) {
		t.Fatalf("control: the other business still holds the shared digest: %v", err)
	}

}

func TestBrandReadDB_NoRowAndAnAllNullRowAreTheSameNoLogo(t *testing.T) {
	f := newBrandFixture(t)
	ctx := context.Background()
	r := newBrandReaderOn(t, f.data)

	type answer struct {
		ref LogoRef
		has bool
	}
	read := func(what string, tenantID uuid.UUID) answer {
		t.Helper()
		ref, has, err := r.PageLogo(ctx, tenantID)
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
		return answer{ref, has}
	}

	// 1. No brand row at all.
	if f.row(t, f.tenant).exists {
		t.Fatal("fixture: the fresh business already has a brand row")
	}
	noRow := read("no brand row", f.tenant)

	// 2. A row whose every brand field is NULL: clearing a business that had no row
	// creates exactly that (WL-4, Karar 3).
	if err := f.brands.ClearLogo(ctx, BrandClearCommand{TenantID: f.tenant, ActorID: f.owner}); err != nil {
		t.Fatalf("clear without a row: %v", err)
	}
	row := f.row(t, f.tenant)
	if !row.exists || row.accent != "NULL" || row.sha != "NULL" || row.mime != "NULL" || row.width != "NULL" || row.height != "NULL" || !row.logoNull {
		t.Fatalf("fixture: the cleared row is not all-NULL: %+v", row)
	}
	allNull := read("an all-NULL row", f.tenant)

	if noRow != (answer{}) || allNull != (answer{}) {
		t.Fatalf("no row = %+v, all-NULL row = %+v; both must be the zero answer (no logo)", noRow, allNull)
	}

	// 3. An accent and no logo: still no logo.
	if err := f.brands.SaveAccent(ctx, BrandAccentCommand{TenantID: f.tenant, ActorID: f.owner, Accent: mustColour(t, "1F5C41")}); err != nil {
		t.Fatalf("save accent: %v", err)
	}
	if got := read("an accent and no logo", f.tenant); got != (answer{}) {
		t.Fatalf("an accent without a logo = %+v, want no logo", got)
	}

	// 4. POSITIVE CONTROL: a logo is reported with its digest, type and box. Without
	// it the three zero answers above would pass for a reader that never says yes.
	logo := f.jpegLogo(t, 40, 30, 5)
	if err := f.brands.SaveLogo(ctx, BrandLogoCommand{TenantID: f.tenant, ActorID: f.owner, Logo: logo}); err != nil {
		t.Fatalf("save logo: %v", err)
	}
	want := answer{LogoRef{SHA256: logo.SHA256, MIME: "image/jpeg", Width: logo.Width, Height: logo.Height}, true}
	if got := read("a logo", f.tenant); got != want {
		t.Fatalf("with a logo = %+v, want %+v", got, want)
	}
	// And the other business, which has no row, still reads "no logo".
	if got := read("the other business", f.foreign); got != (answer{}) {
		t.Fatalf("the other business = %+v, want no logo", got)
	}
}

// TestBrandRead_NoRowAndAnAllNullRowAreOneBranch is the same rule at the seam itself,
// without a database: GetTenantBrand's two spellings of "no brand" come back as one
// value, and what is not "no brand" -- a read error, and each of the fourteen partial
// combinations of the four logo columns -- does not hide in it.
func TestBrandRead_NoRowAndAnAllNullRowAreOneBranch(t *testing.T) {
	sha, mime := "ab", "image/png"
	w, h := int32(4), int32(3)
	noRow := func() []any {
		ref, has, err := logoRefOf(store.GetTenantBrandRow{}, pgx.ErrNoRows)
		return []any{ref, has, err}
	}()
	allNull := func() []any {
		ref, has, err := logoRefOf(store.GetTenantBrandRow{UpdatedAt: time.Now(), UpdatedBy: uuid.New()}, nil)
		return []any{ref, has, err}
	}()
	accentOnly := func() []any {
		a := "1F5C41"
		ref, has, err := logoRefOf(store.GetTenantBrandRow{Accent: &a}, nil)
		return []any{ref, has, err}
	}()
	if !reflect.DeepEqual(noRow, allNull) || !reflect.DeepEqual(noRow, accentOnly) {
		t.Fatalf("no row %v, all-NULL row %v, accent only %v: want one answer", noRow, allNull, accentOnly)
	}
	if noRow[1].(bool) || noRow[2] != nil {
		t.Fatalf("no row = %v, want (zero, false, nil)", noRow)
	}

	boom := errors.New("connection reset")
	if _, has, err := logoRefOf(store.GetTenantBrandRow{}, boom); has || !errors.Is(err, boom) {
		t.Fatalf("a read error came back as (has=%v, err=%v); it must stay an error, not become 'no logo'", has, err)
	}

	// EVERY SUBSET OF THE FOUR LOGO COLUMNS, derived rather than listed: the empty set
	// is "no logo", the full set is the logo, and each of the fourteen others -- a type
	// without a box (mime set, width or height NULL), a box without a type (mime NULL),
	// a type and box without a digest -- is an error: never drawn with a guessed type or
	// a 0x0 box, never a silent "no logo". Migration 00028's all-or-none CHECK keeps such
	// rows out of the table; this is the reading side's own rule.
	columns := []string{"logo_sha256", "logo_mime", "logo_width", "logo_height"}
	visited, none, full, partial := 0, 0, 0, 0
	for mask := 0; mask < 1<<len(columns); mask++ {
		visited++
		var row store.GetTenantBrandRow
		var set []string
		if mask&1 != 0 {
			row.LogoSha256, set = &sha, append(set, columns[0])
		}
		if mask&2 != 0 {
			row.LogoMime, set = &mime, append(set, columns[1])
		}
		if mask&4 != 0 {
			row.LogoWidth, set = &w, append(set, columns[2])
		}
		if mask&8 != 0 {
			row.LogoHeight, set = &h, append(set, columns[3])
		}
		ref, has, err := logoRefOf(row, nil)
		switch len(set) {
		case 0:
			none++
			if has || err != nil || ref != (LogoRef{}) {
				t.Errorf("no logo column set = (%+v, %v, %v), want no logo", ref, has, err)
			}
		case len(columns):
			full++
			if err != nil || !has || ref != (LogoRef{SHA256: sha, MIME: mime, Width: 4, Height: 3}) {
				t.Errorf("all four set = (%+v, %v, %v), want the described logo", ref, has, err)
			}
		default:
			partial++
			if has || err == nil || ref != (LogoRef{}) {
				t.Errorf("only %v set = (%+v, has=%v, err=%v), want an error and no logo", set, ref, has, err)
			}
		}
	}
	// THE SWEEP ITSELF IS ASSERTED, NOT ONLY ITS MIDDLE: a loop that skipped the empty
	// set or the full set would still drive fourteen partial rows (the closing audit's
	// A07 / A10 / A08 mutations stayed green while only `partial` was counted). Every
	// one of the 2^4 subsets is visited exactly once.
	if visited != 1<<len(columns) || none != 1 || full != 1 || partial != 14 {
		t.Fatalf("visited %d subsets (none %d, full %d, partial %d); want 16 (1, 1, 14)",
			visited, none, full, partial)
	}
}

// refusingDB counts the transactions a reader opens and opens none.
type refusingDB struct{ calls atomic.Int32 }

func (d *refusingDB) WithTenant(context.Context, uuid.UUID, db.TxFunc) error {
	d.calls.Add(1)
	return errors.New("refusingDB: no transaction")
}

func TestBrandRead_ANilTenantOpensNoTransaction(t *testing.T) {
	d := &refusingDB{}
	r := newBrandReaderOn(t, d)
	if _, err := r.Logo(context.Background(), uuid.Nil, unstoredDigest(t)); err == nil || errors.Is(err, ErrLogoNotFound) {
		t.Fatalf("Logo with a nil tenant = %v; want an error that is NOT the 404 value", err)
	}
	if _, _, err := r.PageLogo(context.Background(), uuid.Nil); err == nil {
		t.Fatal("PageLogo with a nil tenant returned no error")
	}
	if n := d.calls.Load(); n != 0 {
		t.Fatalf("a nil tenant opened %d transaction(s), want 0", n)
	}
	// Control: a real tenant does reach the database (and the refusing double) -- and a
	// database that fails is an ERROR, never the 404 value: an outage must not read as
	// "this business has no such logo".
	if _, err := r.Logo(context.Background(), uuid.New(), unstoredDigest(t)); err == nil || errors.Is(err, ErrLogoNotFound) {
		t.Fatalf("control: a failing database answered %v; want its error, not ErrLogoNotFound", err)
	}
	if _, has, err := r.PageLogo(context.Background(), uuid.New()); err == nil || has {
		t.Fatalf("control: PageLogo over a failing database = (has=%v, err=%v); want its error", has, err)
	}
	if n := d.calls.Load(); n != 2 {
		t.Fatalf("control: two reads for a real tenant opened %d transaction(s), want 2", n)
	}
	if _, err := NewBrandReader(nil); err == nil {
		t.Fatal("NewBrandReader(nil) was accepted")
	}
}

// TestBrandRead_TheBeltSeesBothReads pins WHERE the logo read's §4.5 belt comes from:
// TestStaffQueries_CarryAnExplicitTenantPredicate derives its list from THIS package's
// store calls, so the two reads are under it only while this package makes them (WL-1
// and WL-4 left the logo read's belt to whichever package the routes call). A move of
// either call to another package turns this red rather than quietly leaving the read
// without a belt.
func TestBrandRead_TheBeltSeesBothReads(t *testing.T) {
	names := storeQueryNames(t, declaredQueries(t))
	for _, want := range []string{"GetTenantLogo", "GetTenantBrand"} {
		if !slices.Contains(names, want) {
			t.Fatalf("this package's derived store calls %v do not include %s", names, want)
		}
	}
}
