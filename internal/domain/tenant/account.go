package tenant

// account.go -- the BUSINESS'S OWN FACTS: what a customer may read and change
// about itself (M7-05). One read, one write, and the write is three columns.
//
// 🔴 WHY IT IS IN THIS PACKAGE. CLAUDE.md section 3 assigns "tenant / lokasyon /
// departman / calisan is kurallari" to internal/domain/tenant, and `tenants` is the
// first noun in that sentence. It also inherits this package's section 4.5 belt for
// free: query_test.go derives the queries it checks from THIS package's own calls
// with go/ast, so GetTenantAccount and UpdateTenantAccount joined the net the moment
// they were named here.
//
// 🔴 WHAT A CUSTOMER MAY CHANGE, AND WHY THE LIST IS THREE AND NOT FIVE. Migration
// 00016 grants tappa_app UPDATE on five columns of `tenants`
// (name, vat_number, business_type, structure, timezone). This file names three; the
// two absences were measured rather than assumed and db/queries/tenants.sql carries
// the measurements at the statement. In short:
//
//	name           EDITABLE. It is what the invoice, the CSV headers and the
//	               confirmation screens call this business.
//	business_type  EDITABLE, and it is the only field here that changes what an
//	               EMPLOYEE sees: web/templates/pages/result.templ switches on it to
//	               pick the sentence a good tap ends with. The sign-up wizard already
//	               promises this ("Pick the closest match -- you can change it
//	               later.", signup.go), and until this task there was no screen where
//	               that promise could be kept.
//	timezone       EDITABLE. Q01 put the zone on the tenant and
//	               internal/domain/signup.DefaultTimezone names this task as the place
//	               it becomes editable.
//	vat_number     NOT EDITABLE. Globally UNIQUE, so an edit reaches OUT of this
//	               tenant, and tappa_app cannot UPDATE the two columns that record
//	               what VIES said about it (migration 00017 granted INSERT only).
//	structure      NOT READ AT ALL, WHICH IS STRONGER THAN "not editable" AND IS A
//	               CORRECTION. The first draft of this file carried it as a read-only
//	               fact for the screen to print; TestSignupStructure_DecidesNothingAfterSignUp
//	               turned RED on that, and the tripwire is right. It bans READING the
//	               column anywhere outside the sign-up path, because the wizard's own
//	               wording assumes the answer decides nothing once the row exists -- and
//	               a settings screen printing it would make this package the first reader
//	               and put a sentence about it in front of the customer. There is nothing
//	               to show: the answer gates no feature, and the panel offers "Add a
//	               department" whichever it says.
//
// 🔴 WHAT CHANGING THE ZONE ACTUALLY MOVES, MEASURED RATHER THAN FEARED. This is the
// question the M7-05 card raises and it has a precise answer, in two halves:
//
//	a FROZEN billing month     UNAFFECTED. migration 00016 stores period_from,
//	                           period_to AND the zone that produced them ON the
//	                           billing_periods row, exactly so "a later timezone
//	                           change cannot silently redefine a period", and
//	                           internal/domain/billing reads the frozen row when one
//	                           exists.
//	an UNFROZEN month          MOVES. Its boundaries are computed from
//	                           tenants.timezone at read time, so both ends of every
//	                           month that has not been closed shift with the zone --
//	                           which internal/handler/billing.go's draftStateLine
//	                           already tells the owner in words.
//	the panel's "day", the     MOVE. Every screen resolves its day boundary through
//	  shift and lateness       GetTenantClock, and tap.Decide resolves a shift's
//	                           wall-clock start through this zone
//	                           (internal/domain/checkin passes emp.TenantTimezone).
//
// So the screen says it, and it says the half that is permanent: a month FROZEN while
// the zone is wrong keeps that zone forever, because nothing may UPDATE that table.
//
// 🔴 IT DOES NOT SPEAK HTTP AND DOES NOT RENDER. It returns facts and typed errors;
// internal/handler decides what a customer is told, and who is allowed to press Save.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/store"
)

// ActionAccountUpdated is the trail's name for this act.
//
// IT FOLLOWS THE `<subject>.<past participle>` SHAPE the trail already uses for
// something that HAPPENED (location.updated, employee.moved, review.recorded) rather
// than the `<subject>.<verb>_refused` shape reserved for something that did not. The
// refusal this screen can produce is written by internal/handler, which is where the
// role gate lives.
const ActionAccountUpdated = "tenant.account_updated"

// The refusals a caller must be able to tell apart, because each is a different
// sentence to a customer (section 4.6: no silent failure).
var (
	// ErrUnknownTenantAccount: no row matched the id in this transaction's context.
	// Under RLS that covers "does not exist" and "belongs to somebody else"
	// together, and the boundary reports it as OUR failure rather than as a verdict
	// -- a signed session whose own tenant is invisible is a broken invariant, not a
	// thing the customer did.
	ErrUnknownTenantAccount = errors.New("tenant: no such business")
	// ErrAccountName: the name is empty or longer than the screen can carry.
	ErrAccountName = errors.New("tenant: business name is not usable")
	// ErrBusinessType: a value outside the schema's closed vocabulary.
	//
	// 🔴 THE SCHEMA IS THE AUTHORITY AND THIS PACKAGE KEEPS NO SECOND LIST.
	// migration 00001's CHECK names the eight values and its own comment says so;
	// internal/domain/signup.BusinessTypes is the ONE Go copy, pinned to that CHECK by
	// TestBusinessTypes_AreExactlyTheSchemasVocabulary. A third list here would be a
	// third thing to keep in step, so this package refuses only what the DATABASE
	// refuses: a 23514 on the tenants business_type constraint becomes this error.
	ErrBusinessType = errors.New("tenant: business type is not one this product knows")
	// ErrTimezone: a zone name this binary cannot load.
	//
	// 🔴 IT IS REFUSED HERE RATHER THAN STORED AND FALLEN BACK FROM. Every reader of
	// this column already falls back to UTC when the name will not load (ledger,
	// plaque, rulebook, billing) and each one logs it -- which is the right behaviour
	// for a value that is ALREADY in the database, and the wrong one for a value
	// somebody is typing now. Accepting "Europe/Maltaa" would silently move this
	// business's whole panel, every shift boundary and every unfrozen invoice month
	// to UTC, and the only trace would be a warning in the operator's log.
	ErrTimezone = errors.New("tenant: timezone is not a zone this product can load")

	// ErrTimezoneMovesBilling: the zone is a valid one, but adopting it would move
	// the first month this business can be billed for. See the guard in Save.
	ErrTimezoneMovesBilling = errors.New("tenant: that timezone would move the first chargeable month")
)

// VATState is what the product knows about this business's VAT number.
//
// FOUR VALUES, MATCHING MIGRATION 00017's FOUR-STATE VOCABULARY, and the type exists
// so a screen cannot collapse them: "we never asked" and "the register says no" are
// different facts and exactly one of them is the customer's problem.
type VATState string

const (
	// VATNeverChecked: vat_checked_at IS NULL.
	VATNeverChecked VATState = "never-checked"
	// VATNoAnswer: a check was stamped but produced no verdict.
	VATNoAnswer VATState = "no-answer"
	// VATValid: VIES confirmed the number.
	VATValid VATState = "valid"
	// VATInvalid: VIES answered, and the answer was no.
	VATInvalid VATState = "invalid"
)

// VATCheck is the pair of columns migration 00017 added, as read.
type VATCheck struct {
	// Verified is nil when there is no verdict. A *bool rather than a bool for the
	// reason the column is NULLABLE: a boolean would have to write an outage down as
	// "this VAT number is not valid".
	Verified *bool
	// CheckedAt is nil when nothing has ever asked.
	CheckedAt *time.Time
}

// State collapses the pair into one of the four.
//
// ⚠️ ONE OF THE FOUR IS NOT REACHABLE FROM THE PRODUCT TODAY, AND THAT IS MEASURED
// RATHER THAN GLOSSED. internal/domain/signup.checkedAt returns nil for VATUnknown --
// its own comment says it collapses "asked, no answer" into "never asked" because the
// checker cannot tell a timeout from an unwired checker -- so a registration stores
// either both columns or neither. VATNoAnswer is therefore a state the SCHEMA allows
// and the wizard never writes; it is kept here because the column pair can still
// arrive that way (a fixture, a repair, or the day the checker learns to tell the two
// apart), and a screen that had no branch for it would print the wrong sentence.
//
// The mirror case -- a verdict with no date -- is also unreachable for the same
// reason (Verified() and checkedAt() are non-nil for exactly the same two statuses)
// and is answered as the verdict, because that is the half that carries meaning; the
// render layer says the date is unknown rather than inventing one.
func (v VATCheck) State() VATState {
	switch {
	case v.Verified == nil && v.CheckedAt == nil:
		return VATNeverChecked
	case v.Verified == nil:
		return VATNoAnswer
	case *v.Verified:
		return VATValid
	default:
		return VATInvalid
	}
}

// Settings is one business's own facts.
//
// 🔴 THERE IS NO `Plan` AND NO `Price` FIELD, AND THE ABSENCE IS STRUCTURAL RATHER
// THAN AN OMISSION. The commercial terms are the operator's (migration 00016) and
// M6-12 phase B closed even READING them to owners at /admin/billing. A field here
// would put them one careless template interpolation away from every manager who
// opens the settings screen, so the query does not select them and this type has
// nowhere to put them.
type Settings struct {
	Name string
	// VATNumber is read-only on this screen. It is carried because the customer must
	// be able to SEE what the invoice will say, and because the VAT verdict below is
	// meaningless without the number it is about.
	VATNumber    string
	BusinessType string
	Timezone     string
	VAT          VATCheck
	// CreatedAt is when the business registered.
	//
	// 🔴 IT IS AN IDENTITY FACT, LIKE THE NAME AND THE VAT NUMBER, AND AN EARLIER
	// VERSION OF THIS COMMENT GAVE THE WRONG REASON. It said the field is here because
	// the founding offer's free window is computed from it "so a customer looking at
	// their first invoice can check it" -- which grounds a field on a screen READABLE BY
	// A MANAGER in an invoice, i.e. in the commercial territory migration 00016 and
	// M6-12 phase B made the operator's. The field stays; the justification does not.
	// "Since when has Tappa had this business on file" names no plan, no price and no
	// amount, and is the same date whatever the business is charged.
	//
	// ⚠️ THE FOUNDING WINDOW IS INDEED COMPUTED FROM IT (migration 00016's
	// tappa_first_chargeable_month) AND IS STATED ON THE BILLING SCREEN, NOT ON THE
	// ACCOUNT ONE -- fillBillingFounding already prints which month starts being charged
	// and at what price, so a second telling would be both a commercial term on the
	// wrong page and a second representation. web/templates/pages/accountview.go carries
	// the same correction from the render side; the two said different things for one
	// round and this is the one that was wrong.
	//
	// Nothing may write it: `created_at` is absent from migration 00016's INSERT and
	// UPDATE grants, because a tenant able to move its own registration date could move
	// its own free months.
	CreatedAt time.Time
}

// AccountNameLimit is the ceiling on a business name.
//
// IT MATCHES venueNameLimit's REASONING AND NOT ITS NUMBER BY ACCIDENT: the column is
// unbounded `text`, so the bound exists to stop one field filling a page and a log
// line rather than to satisfy the schema. 120 runes is far above any registered
// company name in the two markets this ships to -- the longest in the seed fixture is
// 28 -- and it is counted in RUNES rather than bytes, because a Maltese name carrying
// ċ, ġ, ħ or ż would otherwise be cut at a different length from an English one.
//
// 🔴 IT IS EXPORTED SO THERE IS EXACTLY ONE NUMBER. internal/handler validates the
// same bound at the boundary (section 7) in order to name the FIELD in its refusal --
// a generic "cannot be stored" tells an owner nothing about which box to fix -- and an
// unexported constant here would have forced a second literal there, plus a test to
// keep the two in step. The DOMAIN is still the authority: it refuses on its own
// whatever the boundary let through.
const AccountNameLimit = 120

// Accounts performs a business's acts on its own row. Dependencies are injected as
// struct fields (section 7): no package-level state, no singleton.
type Accounts struct {
	data  Database
	trail Trail
	log   *slog.Logger
}

// NewAccounts wires it. A nil database or a nil trail is REFUSED rather than
// tolerated, for the reason NewVenues gives: an Accounts that cannot write the trail
// would be one whose changes leave no record, and `tenants` carries no updated_at at
// all (migration 00001) -- so without the audit row there would be NO TRACE ANYWHERE
// that the zone every shift is judged against had moved.
func NewAccounts(data Database, trail Trail, log *slog.Logger) (*Accounts, error) {
	switch {
	case data == nil:
		return nil, errors.New("tenant: nil database")
	case trail == nil:
		return nil, errors.New("tenant: nil audit trail")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Accounts{data: data, trail: trail, log: log}, nil
}

// Settings reads one business's own facts.
func (a *Accounts) Settings(ctx context.Context, tenantID uuid.UUID) (Settings, error) {
	if tenantID == uuid.Nil {
		return Settings{}, ErrUnknownTenantAccount
	}
	var out Settings
	err := a.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		row, err := store.New(tx).GetTenantAccount(ctx, tenantID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownTenantAccount
			}
			return fmt.Errorf("read account: %w", err)
		}
		out = settingsOf(row.Name, row.VatNumber, row.BusinessType,
			row.Timezone, row.VatVerified, row.VatCheckedAt, row.CreatedAt)
		return nil
	})
	if err != nil {
		return Settings{}, wrapAccount("read account", err)
	}
	return out, nil
}

// AccountCommand is one save.
//
// 🔴 NEITHER IDENTITY IS AN INPUT (section 4.5, section 4.7). TenantID and ActorID
// come from the session the Protect chain resolved; there is no form field for
// either, and audit_log.actor_id may hold nothing else.
type AccountCommand struct {
	TenantID uuid.UUID
	ActorID  uuid.UUID
	Name     string
	// BusinessType is one of the schema's eight. The BOUNDARY validates it against
	// internal/domain/signup's list (section 7: the domain sees data that is already
	// good); this package's own refusal comes from the database's CHECK, so there is
	// no second vocabulary to drift.
	BusinessType string
	// Timezone is an IANA name. It is validated HERE rather than only at the
	// boundary, because a zone that will not load degrades every screen in this
	// business to UTC silently -- see ErrTimezone.
	Timezone string
}

// Save rewrites the three editable facts and records that it did, in ONE transaction.
//
// 🔴 THE TRAIL SHARES THE TRANSACTION (RecordTx, not Record). `tenants` has no
// updated_at, so a change whose audit row failed would leave the panel's day
// boundary, the shift every lateness verdict is measured against and every unfrozen
// invoice month moved, with nothing anywhere saying when or by whom. That is section
// 4.3's argument one level out, and it is the same one venue.go makes for
// `locations`.
//
// 🔴 THE BEFORE STATE IS READ INSIDE THE SAME TRANSACTION, so the trail describes the
// change that actually happened rather than one read a moment earlier. audit_log is
// append-only (tappa_app holds SELECT and INSERT only), so a row written from a stale
// read could never be corrected.
//
// A SAVE THAT CHANGES NOTHING STILL WRITES ITS ROW. Pressing Save with no edit is a
// legitimate act and "somebody opened the settings and pressed Save" is a fact an
// owner may want; the row names WHICH fields moved, so a reader can tell the two
// apart without guessing from the values.
func (a *Accounts) Save(ctx context.Context, c AccountCommand) (Settings, error) {
	if err := requireActor(c.TenantID, c.ActorID); err != nil {
		return Settings{}, err
	}
	name := strings.TrimSpace(c.Name)
	if name == "" || utf8.RuneCountInString(name) > AccountNameLimit {
		return Settings{}, ErrAccountName
	}
	zone := strings.TrimSpace(c.Timezone)
	if err := validZone(zone); err != nil {
		return Settings{}, err
	}
	businessType := strings.TrimSpace(c.BusinessType)
	if businessType == "" {
		// An empty value would meet the CHECK's refusal below anyway; it is caught
		// here so the error is the same one the screen has a sentence for.
		return Settings{}, ErrBusinessType
	}

	var out Settings
	err := a.data.WithTenant(ctx, c.TenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		before, err := q.GetTenantAccount(ctx, c.TenantID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownTenantAccount
			}
			return fmt.Errorf("read account before save: %w", err)
		}
		// 🔴 A ZONE EDIT MAY NOT MOVE THE FIRST MONTH THIS BUSINESS CAN BE BILLED FOR
		// (backlog T37, closed 2026-08-19). tappa_first_chargeable_month reads
		// tenants.timezone, and PreviewBillingPeriod / CloseBillingPeriod derive
		// `free_period` as `month < first_chargeable` -- so for a business that signed
		// up within about fourteen hours of a local month boundary, retyping the zone
		// moved its first PAID month a month later. Measured before this guard, as
		// tappa_app, on created_at = 2026-01-31T23:30Z: UTC gives 2026-04-01 and
		// Europe/Malta gives 2026-05-01. billing_periods is append-only, so once the
		// shifted month is frozen the discount is permanent.
		//
		// IT IS CHECKED HERE RATHER THAN AT CLOSING TIME because closing is the wrong
		// end: by then the row is about to be frozen and the only honest options are
		// "refuse to close" or "bill something the customer will dispute". Here the
		// answer is a sentence on the form, before anything is written.
		//
		// ⚠️ WHAT IT DOES NOT DO, AND THE DIFFERENCE MATTERS. It does not FREEZE the
		// zone at signup -- that would be a stored signup_timezone column, i.e. a
		// migration, and this is the application layer. It removes the LEVER: the
		// first chargeable month is derived from a value that can no longer move it.
		// A business that genuinely relocates across a date line still can, unless it
		// is one of the boundary cases; those must ask Tappa, which is an operator
		// acting deliberately rather than a form field acting silently.
		//
		// ⚠️ IT IS DELIBERATELY NOT CONDITIONAL ON "the month is still open". Whether
		// a shift is harmless depends on which of the months between the old and new
		// answers are already frozen, and a guard whose safety depends on a second,
		// time-varying fact is a guard nobody can reason about. Refusing whenever the
		// answer MOVES over-refuses in a rare, already-closed case, and over-refusing
		// here costs a support conversation rather than an invoice.
		if zone != before.Timezone {
			shift, err := q.FirstChargeableMonthShift(ctx, store.FirstChargeableMonthShiftParams{
				TenantID: c.TenantID,
				Timezone: zone,
			})
			if err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return ErrUnknownTenantAccount
				}
				return fmt.Errorf("read the first chargeable month: %w", err)
			}
			// A NULL on either side means the function could not answer, which is not
			// the same as "they agree" -- it is refused rather than passed over.
			if !shift.CurrentMonth.Valid || !shift.ProposedMonth.Valid ||
				!shift.CurrentMonth.Time.Equal(shift.ProposedMonth.Time) {
				return ErrTimezoneMovesBilling
			}
		}
		row, err := q.UpdateTenantAccount(ctx, store.UpdateTenantAccountParams{
			TenantID:     c.TenantID,
			Name:         name,
			BusinessType: businessType,
			Timezone:     zone,
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrUnknownTenantAccount
			}
			if isBusinessTypeViolation(err) {
				return ErrBusinessType
			}
			return fmt.Errorf("save account: %w", err)
		}
		out = settingsOf(row.Name, row.VatNumber, row.BusinessType,
			row.Timezone, row.VatVerified, row.VatCheckedAt, row.CreatedAt)

		detail := accountDetail{
			Outcome:            "saved",
			Changed:            changedFields(before.Name, row.Name, before.BusinessType, row.BusinessType, before.Timezone, row.Timezone),
			NameBefore:         before.Name,
			NameAfter:          row.Name,
			BusinessTypeBefore: before.BusinessType,
			BusinessTypeAfter:  row.BusinessType,
			TimezoneBefore:     before.Timezone,
			TimezoneAfter:      row.Timezone,
		}
		if _, err := a.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: c.TenantID,
			ActorID:  &c.ActorID,
			Action:   ActionAccountUpdated,
			// THE TARGET IS THE BUSINESS ITSELF, which is the only subject this act
			// has. It is the tenant id rather than the word "account" so a reader
			// walking audit_log by target finds it the way they find every other row.
			Target: c.TenantID.String(),
			Detail: detail,
		}); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return Settings{}, wrapAccount("save account", err)
	}
	// 🔴 THE ZONE IS LOGGED AND THE NAME IS NOT. A zone is an operational fact the
	// operator needs during an incident ("why did this tenant's day move"); a business
	// name is customer data that belongs in audit_log -- a tenant-scoped,
	// access-controlled, retained table -- rather than in a process log nobody can
	// scope. venue.go draws the same line for the same reason.
	a.log.Info("business account updated",
		"tenant_id", c.TenantID, "actor_id", c.ActorID, "timezone", out.Timezone)
	return out, nil
}

// accountDetail is the trail's payload.
//
// 🔴 EXPLICIT EMPTIES, NO omitempty -- the lesson refusedRemovalDetail records: a key
// that is absent cannot be told apart from a value that was empty, and this row exists
// precisely so somebody can reconstruct what changed.
//
// 🔴 section 4.7: THERE IS NO FIELD HERE A SECRET COULD TRAVEL IN. No token, no
// session id, no coordinate, no key -- and no VAT number either, which is this
// screen's own version of the rule: the number is not editable, so recording it on a
// change event would be recording something that did not change.
type accountDetail struct {
	Outcome string `json:"outcome"`
	// Changed names the fields that actually moved, so a reader can tell a real edit
	// from somebody pressing Save without typing. It is DERIVED from the before and
	// after values below rather than from what was posted.
	Changed            []string `json:"changed"`
	NameBefore         string   `json:"name_before"`
	NameAfter          string   `json:"name_after"`
	BusinessTypeBefore string   `json:"business_type_before"`
	BusinessTypeAfter  string   `json:"business_type_after"`
	TimezoneBefore     string   `json:"timezone_before"`
	TimezoneAfter      string   `json:"timezone_after"`
}

// changedFields names the fields whose value moved. An empty slice is written as an
// EMPTY SLICE and never as nil, so the trail says "nothing changed" rather than
// "not recorded".
func changedFields(nameBefore, nameAfter, typeBefore, typeAfter, zoneBefore, zoneAfter string) []string {
	out := []string{}
	if nameBefore != nameAfter {
		out = append(out, "name")
	}
	if typeBefore != typeAfter {
		out = append(out, "business_type")
	}
	if zoneBefore != zoneAfter {
		out = append(out, "timezone")
	}
	return out
}

// settingsOf maps a stored row onto the domain shape. Both queries return the same
// columns, and this is the ONE place they are interpreted.
func settingsOf(name, vat, businessType, zone string, verified *bool, checkedAt *time.Time, createdAt time.Time) Settings {
	return Settings{
		Name:         name,
		VATNumber:    vat,
		BusinessType: businessType,
		Timezone:     zone,
		VAT:          VATCheck{Verified: verified, CheckedAt: checkedAt},
		CreatedAt:    createdAt,
	}
}

// ValidTimezone reports whether a zone name may be stored: one this binary can
// resolve, minus two it CAN resolve and must not keep.
//
// 🔴 "Local" IS REFUSED THOUGH time.LoadLocation ACCEPTS IT, and the reason is the
// whole point of storing a zone at all. It means THE SERVER'S zone, which
// db/queries/tenants.sql's GetTenantClock header already rules out in words: using it
// would make the same business see a different day depending on where the VPS runs.
// Stored, it would be a value whose meaning changes when we move machines.
//
// "" IS REFUSED THOUGH LoadLocation MAPS IT TO UTC, because a blank field is somebody
// clearing the box rather than somebody asking for UTC -- and the column is NOT NULL
// with a real default, so an empty string would be the one value in it that no reader
// can distinguish from a mistake. A customer who wants UTC types UTC.
//
// EVERYTHING ELSE IS THE RUNTIME'S DECISION, not a list here. The IANA database is
// embedded in the binary (internal/domain/tap/tzdata.go), so LoadLocation is the same
// authority every reader of this column already uses -- and a curated list of "zones
// we allow" would be a second representation of a database that gains entries every
// year.
//
// 🔴 IT IS EXPORTED FOR THE REASON AccountNameLimit IS: internal/handler validates the
// same input at the boundary (section 7) so it can name the FIELD in its refusal, and
// a second time.LoadLocation call over there would be a second rule -- one that could
// start accepting "Local" the day somebody simplified it. The DOMAIN still refuses on
// its own whatever the boundary let through.
func ValidTimezone(name string) bool {
	if name == "" || name == "Local" {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

// validZone is Save's own gate, in the error shape the rest of this file speaks.
func validZone(name string) error {
	if !ValidTimezone(name) {
		return ErrTimezone
	}
	return nil
}

// isBusinessTypeViolation reports whether Postgres refused the statement because the
// value is not one of the eight (SQLSTATE 23514 on the tenants business_type CHECK).
//
// 🔴 IT MATCHES THE CODE AND THE CONSTRAINT NAME, NOT THE MESSAGE, for the reason
// isForeignKeyViolation gives: an error STRING is a locale- and version-dependent
// sentence. The constraint name is checked as well as the code because `tenants`
// carries more than one CHECK (the plan vocabulary and the price floor, migration
// 00016) and this statement must not report one of those as a bad business type.
func isBusinessTypeViolation(err error) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23514" {
		return false
	}
	return strings.Contains(pg.ConstraintName, "business_type")
}

// wrapAccount keeps the typed refusals typed and wraps everything else with context
// (section 7). It is this file's copy of wrapVenue's rule: a caller must be able to
// tell "you cannot do that" from "we could not do that".
func wrapAccount(op string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrUnknownTenantAccount),
		errors.Is(err, ErrAccountName),
		errors.Is(err, ErrBusinessType),
		errors.Is(err, ErrTimezone),
		errors.Is(err, ErrTimezoneMovesBilling):
		return err
	default:
		return fmt.Errorf("%s: %w", op, err)
	}
}
