package tenant

// brand.go -- a business's brand: saving and clearing its accent colour and its logo
// (M10 WL-4; ADR 0023 §1, ADR 0024 §4 and §6). Four writes -- SaveAccent, ClearAccent,
// SaveLogo, ClearLogo -- and the four run the one function below, write.
//
// 🔴 write IS ONE TRANSACTION, IN THIS ORDER (db/queries/branding.sql's header; the
// two waiting orders were measured by WL-1 and again here):
//
//  1. the actor is read from admin_users in this tenant's context and must be an
//     active owner (mayEditBrand);
//  2. EnsureTenantBrand        -- the row exists from here on;
//  3. GetTenantBrandForUpdate  -- locks it and returns the value being replaced;
//  4. the Set or Clear statement -- one row changed, or the write fails;
//  5. GetTenantBrand           -- the value the database now holds;
//  6. the audit_log row, through the same transaction (Trail.RecordTx).
//
// Steps 2 and 3 are why the trail's "before" is the stored value when two owners save
// at once: the second waits on the first and then reads what it committed. Where it
// waits depends on how far the first has got, and
// TestBrandDB_TheSecondOfTwoConcurrentSavesRecordsTheFirstAsItsBefore asserts the
// statement in each of its four cases: EnsureTenantBrand on a new row, and on an
// existing row once the first has run its UPDATE; GetTenantBrandForUpdate on an
// existing row before the first's UPDATE. Step 5 is why the trail's "after" is the
// database's value and not this file's argument: WL-1 measured that the stored accent
// can differ from the text a statement was given (char(6) trims trailing spaces).
// Step 6 shares the transaction because tenant_branding keeps no history of its own
// (an UPDATE overwrites it; ADR 0023 §1 puts the history in audit_log): a change whose
// trail row were lost would leave updated_at and updated_by on the row as its trace,
// until the next write overwrote them.
//
// 🔴 A BRAND IS NOT A LEGAL RECORD (ADR 0023 §1): it is UPDATEd in place and a clear
// is an UPDATE to NULL. tappa_app holds no DELETE on the table (migration 00028;
// TestTenantBranding_AppPrivileges).
//
// 🔴 IT DOES NOT SPEAK HTTP. It takes values the handler has already parsed -- a
// brand.Color from brand.NormalizeAccent, a brand.Logo from brand.LogoGate.Normalize
// -- and re-checks two properties itself: the colour's legibility (brand.Check;
// ADR 0023 §3 asks the same gate on the writing and the serving side) and the logo's
// provenance (brand.Logo.Normalized; ADR 0024 §3 stores the re-encoder's output).
//
// THE CLAIM, IN THREE PARTS.
//
// PART I -- THE SHIPPED CODE, MEASURED against the dev Postgres as tappa_app, on the
// inputs the tests name (brand_db_test.go):
//   - the sequence SaveAccent, SaveLogo (PNG), SaveLogo (JPEG), ClearAccent, ClearLogo
//     stores each value, sets updated_by to the acting owner, and adds one
//     tenant.brand_updated row per write with the expected detail; after ClearLogo the
//     five logo columns read NULL one by one
//     (TestBrandDB_EachWriteStoresItsValueAndOneTrailRow);
//   - the row each of the four writes adds has exactly the keys field, before, after,
//     bytes, width, height (TestBrandDB_TheDetailHasExactlyTheSixKeys);
//   - with a trail that returns an error, the four writes on a tenant without a row and
//     on a seeded one leave the row as it was (and create no row); with a trail that
//     writes its row and then returns an error, neither the change nor that row
//     survives; a trail that reads the brand row through the transaction it is handed
//     sees each write's own result
//     (TestBrandDB_TheChangeAndItsTrailRowShareOneTransaction);
//   - when the Set or Clear statement returns an error, or changes no row, the four
//     writes leave the row and the trail as they were
//     (TestBrandDB_AFailedWriteLeavesNoTrailRow);
//   - two illegible accents, four logo values that are not an unchanged Normalize
//     output, a nil actor and a nil tenant (each on the four writes) are refused
//     without a transaction being opened, and the row and the trail stay as they were
//     (TestBrandDB_RefusalsBeforeTheDatabaseWriteNothing);
//   - a manager, a disabled owner, the other tenant's owner on this tenant, this
//     tenant's owner on the other tenant and an id that is no admin get the
//     ErrBrandNotPermitted value itself, with one error text for the five, on the four
//     writes, on two tenants with a brand row and on two without; neither tenant's row
//     or trail changes and no row is created
//     (TestBrandDB_OnlyAnActiveOwnerOfThisTenantMayWrite);
//   - ClearAccent and ClearLogo on a tenant without a brand row create the row with
//     every brand field NULL and write a row whose before, after, bytes, width and
//     height are null (TestBrandDB_ClearingWithoutABrandRowCreatesTheRowAndRecordsIt);
//   - the action and the two field values are spelled as ADR 0024 §6 names them, and
//     the tests read the trail by the literal action
//     (TestBrandTrail_TheActionAndFieldNamesAreTheADRs);
//   - of two concurrent SaveAccent calls, in the four cases new or existing row × first
//     writer held before or after its UPDATE, the second waits in the statement named
//     above and its "before" is the first's committed "after"
//     (TestBrandDB_TheSecondOfTwoConcurrentSavesRecordsTheFirstAsItsBefore);
//   - saving the stored accent again writes the UPDATE (updated_at and updated_by move)
//     and a row whose "before" equals its "after"
//     (TestBrandDB_SavingTheSameAccentAgainIsRecorded).
//
// PART II -- NAMED PINS: TestStaffQueries_CarryAnExplicitTenantPredicate
// (query_test.go) derives its query list from this package's store calls, and since
// this file it lists the eight this file makes; it reads each statement's text. The
// mutations each test above and that pin turned red are the WL-4 mutation table of
// ADR 0023 (the WL-4 note); the table lists every mutation that was run, the red tests
// of each, and the two that stayed green and why.
//
// PART III -- No completeness claim: any change the table does not list is the subject
// of code review.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/brand"
	"github.com/atknatk/tappa/internal/store"
)

// ActionBrandUpdated is the trail's name for an accepted brand write (ADR 0024 §6).
// A refused attempt is tenant.brand_update_refused, written where the role gate the
// customer meets lives (WL-7, the handler -- accountactions.go's precedent).
const ActionBrandUpdated = "tenant.brand_updated"

// The values of the detail's "field" key: which of the two brand inputs ADR 0023 §1
// names the row is about.
const (
	BrandFieldAccent = "accent"
	BrandFieldLogo   = "logo"
)

// The admin_users values mayEditBrand accepts. The vocabulary is migration 00006's
// CHECKs (role IN ('owner','manager'), status IN ('active','disabled')).
const (
	brandEditorRole   = "owner"
	brandEditorStatus = "active"
)

var (
	// ErrBrandNotPermitted: the actor is not an active owner of this business -- a
	// manager, a disabled admin, or an id that is not an admin of this tenant at all.
	// One error for the three, so the answer does not say which.
	ErrBrandNotPermitted = errors.New("tenant: changing the brand is reserved for an active owner")
	// ErrBrandLogoNotNormalized: the logo is not a value brand.LogoGate.Normalize
	// returned unchanged (brand.Logo.Normalized). ADR 0024 §3 stores the re-encoder's
	// output and not the uploaded bytes. The table does not tell the two apart: with
	// this check removed (WL-4 mutation M12a), a JPEG carrying an APP1 Exif segment,
	// put in a Logo literal with its own digest, was stored.
	ErrBrandLogoNotNormalized = errors.New("tenant: the logo is not the logo gate's output")
)

// Brands writes a business's brand. Dependencies are injected as struct fields
// (section 7).
type Brands struct {
	data  Database
	trail Trail
	log   *slog.Logger
}

// NewBrands wires it. A nil trail is refused for NewAccounts' reason: a Brands that
// could not write the trail would make changes without a trail row.
func NewBrands(data Database, trail Trail, log *slog.Logger) (*Brands, error) {
	switch {
	case data == nil:
		return nil, errors.New("tenant: nil database")
	case trail == nil:
		return nil, errors.New("tenant: nil audit trail")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Brands{data: data, trail: trail, log: log}, nil
}

// BrandAccentCommand saves an accent. The caller takes TenantID and ActorID from the
// resolved session and not from a form (section 4.5); Accent is the handler's
// brand.NormalizeAccent result.
type BrandAccentCommand struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
	Accent   brand.Color
}

// BrandLogoCommand saves a logo. Logo is brand.LogoGate.Normalize's return value,
// passed on unchanged.
type BrandLogoCommand struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
	Logo     brand.Logo
}

// BrandClearCommand clears one brand input.
type BrandClearCommand struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
}

// SaveAccent stores the accent's canonical spelling (brand.Color.Hex, the format the
// accent CHECK of migration 00028 accepts). The colour is re-checked here: an
// illegible one returns brand.ErrAccentIllegible before a transaction is opened.
func (b *Brands) SaveAccent(ctx context.Context, c BrandAccentCommand) error {
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return err
	}
	if _, err := brand.Check(c.Accent); err != nil {
		return err
	}
	canonical := c.Accent.Hex()
	return b.write(ctx, c.TenantID, c.ActorID, BrandFieldAccent, nil,
		func(ctx context.Context, q *store.Queries) (int64, error) {
			return q.SetTenantAccent(ctx, store.SetTenantAccentParams{
				Accent: canonical, UpdatedBy: c.ActorID, TenantID: c.TenantID,
			})
		})
}

// ClearAccent sets the accent back to NULL: the tap button returns to the default.
func (b *Brands) ClearAccent(ctx context.Context, c BrandClearCommand) error {
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return err
	}
	return b.write(ctx, c.TenantID, c.ActorID, BrandFieldAccent, nil,
		func(ctx context.Context, q *store.Queries) (int64, error) {
			return q.ClearTenantAccent(ctx, store.ClearTenantAccentParams{
				UpdatedBy: c.ActorID, TenantID: c.TenantID,
			})
		})
}

// SaveLogo stores a logo. A Logo whose Normalized() is false returns
// ErrBrandLogoNotNormalized before a transaction is opened; the table checks the
// digest against the bytes, the type, the width and height and the byte length
// (migration 00028).
func (b *Brands) SaveLogo(ctx context.Context, c BrandLogoCommand) error {
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return err
	}
	if !c.Logo.Normalized() {
		return ErrBrandLogoNotNormalized
	}
	l := c.Logo
	size := len(l.Data)
	return b.write(ctx, c.TenantID, c.ActorID, BrandFieldLogo, &logoWritten{sha: l.SHA256, bytes: size},
		func(ctx context.Context, q *store.Queries) (int64, error) {
			return q.SetTenantLogo(ctx, store.SetTenantLogoParams{
				Logo:       l.Data,
				LogoSha256: l.SHA256,
				LogoMime:   l.MIME,
				LogoWidth:  int32(l.Width),
				LogoHeight: int32(l.Height),
				UpdatedBy:  c.ActorID,
				TenantID:   c.TenantID,
			})
		})
}

// ClearLogo sets all five logo columns back to NULL in one statement (the table's
// all-or-nothing CHECK refuses a partial logo).
func (b *Brands) ClearLogo(ctx context.Context, c BrandClearCommand) error {
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return err
	}
	return b.write(ctx, c.TenantID, c.ActorID, BrandFieldLogo, nil,
		func(ctx context.Context, q *store.Queries) (int64, error) {
			return q.ClearTenantLogo(ctx, store.ClearTenantLogoParams{
				UpdatedBy: c.ActorID, TenantID: c.TenantID,
			})
		})
}

// logoWritten is what SaveLogo knows about the bytes it wrote that the per-page read
// does not return: their digest, to confirm the read-back is this write's logo, and
// their count, for the detail's "bytes".
type logoWritten struct {
	sha   string
	bytes int
}

// write is the transaction the four writes run (the file header's six steps).
//
// A SAVE THAT CHANGES NOTHING STILL WRITES THE UPDATE AND ITS ROW, as
// Accounts.Save does: pressing Save is an act, updated_at and updated_by move, and
// the row's equal "before" and "after" say that the value did not
// (TestBrandDB_SavingTheSameAccentAgainIsRecorded).
func (b *Brands) write(ctx context.Context, tenantID, actorID uuid.UUID, field string,
	logo *logoWritten, change func(context.Context, *store.Queries) (int64, error)) error {
	err := b.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if err := mayEditBrand(ctx, q, tenantID, actorID); err != nil {
			return err
		}
		if err := q.EnsureTenantBrand(ctx, store.EnsureTenantBrandParams{
			TenantID: tenantID, UpdatedBy: actorID,
		}); err != nil {
			return fmt.Errorf("ensure the brand row: %w", err)
		}
		before, err := q.GetTenantBrandForUpdate(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("lock the brand row: %w", err)
		}
		n, err := change(ctx, q)
		if err != nil {
			return fmt.Errorf("write the %s: %w", field, err)
		}
		if n != 1 {
			return fmt.Errorf("write the %s: %d rows changed, want 1", field, n)
		}
		after, err := q.GetTenantBrand(ctx, tenantID)
		if err != nil {
			return fmt.Errorf("read the brand back: %w", err)
		}
		detail, err := brandDetailOf(field, before, after, logo)
		if err != nil {
			return err
		}
		_, err = b.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: tenantID,
			ActorID:  &actorID,
			Action:   ActionBrandUpdated,
			// The business itself, as Accounts.Save's row: the two events about one
			// business are found by the same lookup.
			Target: tenantID.String(),
			Detail: detail,
		})
		return err
	})
	if err != nil {
		if errors.Is(err, ErrBrandNotPermitted) {
			return ErrBrandNotPermitted
		}
		return fmt.Errorf("tenant: brand %s: %w", field, err)
	}
	// tenant_id, actor_id and field, and no value: the values are in the trail row
	// (tenant-scoped, retained), and ADR 0024 §6 keeps a logo's bytes and file name out
	// of the log.
	b.log.Info("tenant brand updated", "tenant_id", tenantID, "actor_id", actorID, "field", field)
	return nil
}

// mayEditBrand is the domain's role gate: the actor must be an active owner of this
// tenant, read from admin_users inside the write's own transaction and tenant context
// (GetAdminByID: "this is where role comes from"). ADR 0024 §6 names the owner; the
// handler gates first (mayEditAccount, WL-7) and writes the refusal row, and this gate
// does not take the handler's word for it. Another tenant's admin id finds no row
// here (RLS and the query's tenant predicate). With this gate removed (WL-4 mutation
// M13a) that id was refused by the composite foreign key on updated_by instead
// (23503 tenant_branding_updated_by_fk, migration 00028) and both rows stayed as they
// were, while a manager and a disabled owner wrote.
//
// IT RUNS BEFORE EnsureTenantBrand, and it returns the bare sentinel (write passes it
// through unwrapped), so the five refused actors get one value and one text. Moved
// after Ensure (mutation A01), on a tenant without a brand row the three ids that are
// not an admin of the tenant got 23503 from Ensure's INSERT and the manager got
// ErrBrandNotPermitted: the answer said which.
func mayEditBrand(ctx context.Context, q *store.Queries, tenantID, actorID uuid.UUID) error {
	admin, err := q.GetAdminByID(ctx, store.GetAdminByIDParams{ID: actorID, TenantID: tenantID})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrBrandNotPermitted
	}
	if err != nil {
		return fmt.Errorf("read the actor: %w", err)
	}
	if admin.Role != brandEditorRole || admin.Status != brandEditorStatus {
		return ErrBrandNotPermitted
	}
	return nil
}

// brandDetail is the trail's payload: ADR 0024 §6's six keys, none with omitempty, so
// a NULL is written as null and its key stays (accountDetail's rule: an absent key
// cannot be told from an empty value; an omitempty key going missing is WL-4 mutation
// M07b). null means "none": an accent row's bytes, width and height; a clear's
// "after"; a first write's "before".
//
// brandDetailOf fills the six from the field constant, the accent or logo_sha256
// column, the count of bytes written and logo_width / logo_height; it fills none of
// them from the logo's bytes, a file name or a client Content-Type (ADR 0024 §6,
// Iddia G).
type brandDetail struct {
	Field  string  `json:"field"`
	Before *string `json:"before"`
	After  *string `json:"after"`
	Bytes  *int    `json:"bytes"`
	Width  *int32  `json:"width"`
	Height *int32  `json:"height"`
}

// brandDetailOf builds the row from the locked "before" and the read-back "after".
// For a logo save, "bytes" is the length of the bytes written; it is reported only
// when the read-back digest is the digest of those bytes (the table's
// tenant_branding_logo_sha256_matches_logo CHECK binds the stored digest to the stored
// bytes), and a read-back without the written digest is an error. That error branch is
// in the code and not measured: with the primary key and the CHECK in place the tests
// do not reach it, and removing it left them green (WL-4 mutation A15).
func brandDetailOf(field string, before store.GetTenantBrandForUpdateRow, after store.GetTenantBrandRow, logo *logoWritten) (brandDetail, error) {
	switch field {
	case BrandFieldAccent:
		return brandDetail{Field: field, Before: before.Accent, After: after.Accent}, nil
	case BrandFieldLogo:
		d := brandDetail{Field: field, Before: before.LogoSha256, After: after.LogoSha256}
		if logo != nil {
			if after.LogoSha256 == nil || *after.LogoSha256 != logo.sha {
				return brandDetail{}, errors.New("read the brand back: the stored logo is not the one written")
			}
			n := logo.bytes
			d.Bytes = &n
		}
		if after.LogoSha256 != nil {
			d.Width, d.Height = after.LogoWidth, after.LogoHeight
		}
		return d, nil
	default:
		return brandDetail{}, fmt.Errorf("tenant: brand: unknown field %q", field)
	}
}
