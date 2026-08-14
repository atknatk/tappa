// Package signup is self-service registration: the rules a new business must
// satisfy and the ONE transaction that brings it into existence.
//
// 🔴 IT IS THE FIRST CODE IN THIS PRODUCT THAT CREATES A TENANT. Until M7-02,
// `INSERT INTO tenants` lived only in test/fixtures/seed.sql and in test helpers,
// and two shipped files built risk boundaries on that fact and said in writing that
// the boundary would expire here: migration 00011 ("no APPLICATION path creates a
// tenant ... M7-02 changes exactly that") and internal/adminauth/password.go
// ("the tasks that open a write path are named — M6-05, M7-04, M7-02"). Everything
// this package does is downstream of that sentence coming true.
//
// WHAT LIVES HERE AND WHAT DOES NOT. The rules (what a company name, a VAT number,
// a venue list and a first operator must look like) and the provisioning
// transaction live here. The wizard's steps, its cookies, its budgets, its bot
// challenge and its screens are internal/handler's — CLAUDE.md §3 draws that line
// and this package holds no HTTP type at all.
//
// THE BOUNDARY OF THIS TASK, STATED SO THE NEXT ONE IS NOT SURPRISED. M7-03 is
// "tenant provisioning" and its card names the same single transaction this file
// runs. The reading taken (and the measurement behind it) is recorded in the task
// report and in docs/plan/m7-portal.md: M7-02 WRITES — a wizard whose done screen
// carries an APPROVED stamp over nothing created would be exactly the "screen
// promising a capability that is not mounted" defect M7-01's card warns about — and
// M7-03 HARDENS: departments for a multi-venue business, the "waiting for a plaque"
// state, and the provisioning edge cases this file names as limits.
package signup

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/store"
)

// ActionSignupCompleted is the audit action a completed registration writes. The
// vocabulary is free text by schema decision (00005), so this constant is the
// vocabulary.
const ActionSignupCompleted = "signup.completed"

// ActionSignupUnreachable marks a registration whose account cannot be reached by the
// sign-in path — the address already resolves to more identities than one login
// compares, and this one sorts outside that window.
//
// 🔴 IT EXISTS BECAUSE THE CHECK THAT PRODUCES IT IS ITSELF A CHANNEL, AND THIS
// REPOSITORY'S RULE IS THAT A CHANNEL IS EITHER CLOSED OR COUNTED — NEVER SILENT. See
// Provisioner.signInBlocked for the measurement, the price and what an attacker can
// learn; this row is the "counted" half, in the same shape
// internal/handler.AdminAuth.recordCandidateProbe uses for the login side.
const ActionSignupUnreachable = "signup.sign_in_unreachable"

// Database is the narrow slice of *db.DB this package needs, declared HERE at the
// consumer (§7).
//
// ONE METHOD, AND IT IS THE TENANT-SCOPED ONE. This package never needs a
// context-less lookup: a registration knows which tenant it is creating because it
// is the one minting the id. That is worth stating because the two OTHER things
// this flow touches — an email that may exist elsewhere, a VAT number that may
// exist elsewhere — are questions this package deliberately does not ask. See
// Provision for what answers them instead and why asking would be worse.
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
	// GetAdminByEmail is the CONTEXT-LESS resolver (ADR 0002 madde 7, migration
	// 00011). It is used for exactly ONE thing here and the scope is the point — see
	// Provision's sign-in reachability check.
	GetAdminByEmail(ctx context.Context, email string) ([]db.ResolvedAdmin, error)
}

// Trail is the slice of audit.Recorder this package needs (§7).
//
// IT IS RecordTx AND NOT Record, which is the opposite of what most callers of
// internal/audit want and is the same choice internal/domain/manual and
// internal/domain/billing make. The reason is sharper here than for either: a
// registration is atomic — tenant, venues and first operator in one transaction —
// and an audit row written in its OWN transaction would survive a rollback and
// claim a business exists that does not.
type Trail interface {
	RecordTx(ctx context.Context, tx pgx.Tx, e audit.Event) (uuid.UUID, error)
	// Record appends in its OWN transaction. It is here for exactly ONE event — the
	// sign-in reachability observation — which is taken AFTER the provisioning
	// transaction has committed and therefore cannot share it. Everything else this
	// package writes uses RecordTx.
	Record(ctx context.Context, e audit.Event) (uuid.UUID, error)
}

// ---------------------------------------------------------------- the rules --

// The bounds every field is held to.
//
// THEY ARE BOUNDS ON AN UNAUTHENTICATED FORM, so each one is a memory statement as
// well as a product statement: past these, a stranger is choosing how much this
// process allocates and how much a row costs forever.
const (
	// MaxCompanyNameRunes bounds the business name. Malta's registry allows long
	// company names ("… Holdings and Catering Services Limited"), so this is
	// generous rather than tight.
	MaxCompanyNameRunes = 120

	// MaxVenueNameRunes is tenant.MaxVenueNameRunes, NOT a second literal. The
	// wizard writes rows into the same `locations` table the panel's venue editor
	// writes, so a name this flow accepts and that screen refuses would be a venue
	// nobody can edit.
	MaxVenueNameRunes = tenant.MaxVenueNameRunes

	// MaxPersonNameRunes bounds the first operator's name.
	MaxPersonNameRunes = 120

	// MaxEmailRunes bounds the address. RFC 5321 caps a path at 256 octets; this is
	// runes and is deliberately looser than the RFC rather than an attempt to
	// re-state it — the authority on what admin_users.email accepts is the column,
	// and internal/adminauth already refuses the two things Postgres itself rejects.
	MaxEmailRunes = 254

	// MinPasswordRunes is the floor for the first operator's password.
	//
	// TWELVE, AND IT IS A FLOOR RATHER THAN A COMPOSITION RULE. There is no "one
	// upper case, one digit, one symbol" here on purpose: those rules measurably
	// push people toward `Password1!` and this product has a better lever — bcrypt
	// at cost 12 (internal/adminauth) makes an offline guess expensive, and the
	// panel's attempt budget makes an online one pointless.
	MinPasswordRunes = 12

	// MaxPasswordBytes is adminauth's hard limit, mirrored so this package can
	// refuse over-long input where an error is cheap and explainable (at the moment
	// somebody SETS a password) rather than at the moment they try to use it. It is
	// DERIVED, not a second literal: adminauth.Hash would refuse anyway, and the
	// two must agree.
	MaxPasswordBytes = adminauth.MaxPasswordBytes

	// MaxVenues bounds how many venues one registration may create.
	//
	// TWENTY-FIVE. The largest design-partner business is Kebab Factory with NINE
	// locations (skill tappa-seed), so this is nearly three times the biggest real
	// customer. It is a bound on an unauthenticated write: without it a single
	// registration decides how many rows this INSERT makes, and the wizard's signed
	// state — which carries the list between steps — decides how big a cookie a
	// browser must hold.
	//
	// A CHAIN THAT OUTGROWS IT IS NOT LOCKED OUT: venues are added from the panel
	// afterwards (M6-06 shipped that screen), so the ceiling costs a very large new
	// customer some clicks rather than the ability to register.
	MaxVenues = 25
)

// BusinessTypes is the chip set the wizard offers, in the order it offers them.
//
// 🔴 IT IS THE SCHEMA'S OWN VOCABULARY AND MUST STAY THAT WAY. Migration 00001's
// CHECK on tenants.business_type is the authority; a value this slice offers and
// that CHECK refuses is a registration that fails at the last statement, after the
// customer has typed everything. TestBusinessTypes_AreExactlyTheSchemasVocabulary
// re-reads the migration and compares, so the two cannot drift.
//
// THE LABELS ARE THE WIZARD'S, THE VALUES ARE THE DATABASE'S. handoff §9 names the
// chips (Restaurant/Café/Bar/Kiosk/Retail/Hotel/Production/Other) and the column
// stores them lower-case.
var BusinessTypes = []BusinessType{
	{Value: "restaurant", Label: "Restaurant"},
	{Value: "cafe", Label: "Café"},
	{Value: "bar", Label: "Bar"},
	{Value: "kiosk", Label: "Kiosk"},
	{Value: "retail", Label: "Retail"},
	{Value: "hotel", Label: "Hotel"},
	{Value: "production", Label: "Production"},
	{Value: "other", Label: "Other"},
}

// BusinessType is one chip.
type BusinessType struct {
	Value string
	Label string
}

// ValidBusinessType reports whether v is one of the eight.
func ValidBusinessType(v string) bool {
	for _, t := range BusinessTypes {
		if t.Value == v {
			return true
		}
	}
	return false
}

// The two structures a business can have — migration 00001's other CHECK list.
//
// WHAT IT DECIDES IS HOW MANY VENUES STEP TWO ACCEPTS, and nothing else. `single`
// takes exactly one; `multi` takes one to MaxVenues. ValidateVenues is the only
// function in this package that reads it.
//
// 🔴 IT DECIDES NOTHING AFTER THE ROW IS WRITTEN, AND THIS COMMENT USED TO CLAIM
// THE OPPOSITE — "it decides the venue/department model for the rest of the
// tenant's life ... `multi` is several venues, and departments under them". Three
// measurements retired that (M7-03 phase B):
//
//   - the product's own demo data goes the other way. Kebab Factory is `multi`
//     with nine venues and ZERO departments; Kebab Manufacturing is `single` with
//     one venue and FIVE, each carrying its own shift. Reproduce from the fixture
//     alone, no database (an earlier version of this line offered
//     `grep -c … -A20`, which prints "1" because -c swallows -A and shows none of
//     the facts above):
//
//     which tenant is which shape:
//     grep -A6 'INSERT INTO tenants' test/fixtures/seed.sql | grep -E "Kebab|'(single|multi)'"
//     how many departments carry a shift, and whose they are:
//     awk '/INSERT INTO departments/,/ON CONFLICT/' test/fixtures/seed.sql | grep -cE "TIME '[0-9]"
//     awk '/INSERT INTO departments/,/ON CONFLICT/' test/fixtures/seed.sql | grep -oE "'[0-9]0000000-0000-4000-8000-000000000001'" | sort | uniq -c
//
//     They print, in order: KF=multi / KM=single; 5; and "5 '20000000-…-0001'" —
//     every department row belongs to KM, the `single` tenant.
//
//   - CreateTenant is the only statement in the repo that names the column, and no
//     screen, query or policy reads it back. Pinned by
//     TestSignupStructure_DecidesNothingAfterSignUp.
//
//   - the panel offers "Add a department" to both shapes — measured on two tenants
//     provisioned end to end through this wizard.
//
// A wizard step gated on `multi` would therefore have aimed the feature at the half
// of our customers that, in the only data we have, does not use it.
const (
	StructureSingle = "single"
	StructureMulti  = "multi"
)

// ValidStructure reports whether v is one of the two.
func ValidStructure(v string) bool { return v == StructureSingle || v == StructureMulti }

// DefaultTimezone is the zone a new tenant gets.
//
// IT MATCHES migration 00001's COLUMN DEFAULT ('Europe/Malta') and is written here
// anyway rather than omitted from the INSERT, because CreateTenant names the column
// and a named column takes the value passed. Q01 put the zone on the tenant; the
// wizard does not ask for it (a Maltese market, one zone) and M7-05's account
// settings is where it becomes editable. TestDefaultTimezone_MatchesTheSchema pins
// the two together.
const DefaultTimezone = "Europe/Malta"

// Business is step one of the wizard: who is registering.
type Business struct {
	Name string
	// VATNumber is ALWAYS the normalised form (NormaliseVAT). Nothing downstream
	// normalises again — see vat.go for why there is exactly one representation.
	VATNumber    string
	BusinessType string
	Structure    string
	// VAT is what VIES said, or VATUnknown. It is carried rather than re-asked so
	// the answer that reaches the database is the answer that was actually received
	// (and so the wizard makes ONE outbound call per registration, not one per
	// step).
	VAT VATStatus
}

// Venue is one entrance the business wants recorded. A NAME AND NOTHING ELSE, and
// that is the whole design of this step: the evidence a venue is judged on — its IP
// ranges, its coordinate, its shift, its Wi-Fi name — is configured from the panel
// afterwards, where somebody can look it up. Asking a stranger for a CIDR block in
// a sign-up form would produce wrong data at the one moment nobody can check it.
type Venue struct {
	Name string
}

// Account is step three: the first operator. There is exactly one, and their role
// is `owner` — a business with no owner cannot be administered, and a second
// administrator is an invitation (M7-04) rather than a sign-up field.
type Account struct {
	FullName string
	Email    string
	// Password is the only §4.7-adjacent value in this package. It exists for the
	// length of one request: Provision hands it to adminauth.Hash and never stores,
	// logs or returns it. It is deliberately NOT carried in the wizard's signed
	// state — internal/handler/signupstate.go says the same from its side, and it is
	// why the account step is LAST.
	Password string
}

// Draft is a complete registration, ready to be provisioned.
type Draft struct {
	Business Business
	Venues   []Venue
	Account  Account
}

// Errors maps a form field name to the ONE sentence shown beside it.
//
// A MAP RATHER THAN A LIST so a re-rendered form can put each message next to the
// input it belongs to; the brand's rule is that an error says what to do rather
// than blaming, and a message floating at the top of a form cannot say which field
// it means.
type Errors map[string]string

// Any reports whether validation found anything.
func (e Errors) Any() bool { return len(e) > 0 }

func (e Errors) add(field, msg string) {
	if _, seen := e[field]; !seen {
		e[field] = msg
	}
}

// ValidateBusiness checks step one. It returns the NORMALISED business alongside
// the errors, so the caller stores what was checked rather than what was typed.
//
// 🔴 THE VAT FORMAT CHECK IS HERE AND IT IS MANDATORY (the M7-02 card: "VAT
// zorunlu"; "sunucu tarafı doğrulama tam"). The browser may do the same check for
// convenience; nothing on this path trusts it, and TestSignupHandler_* drives every
// field with a browser that validates nothing.
func ValidateBusiness(b Business) (Business, Errors) {
	errs := Errors{}
	b.Name = collapseSpaces(b.Name)
	switch {
	case b.Name == "":
		errs.add("name", "Tell us the business name as it appears on your invoices.")
	case utf8.RuneCountInString(b.Name) > MaxCompanyNameRunes:
		errs.add("name", fmt.Sprintf("That is longer than %d characters.", MaxCompanyNameRunes))
	case !hasLetter(b.Name):
		errs.add("name", "A business name needs at least one letter.")
	case !isStorableText(b.Name):
		// See isStorableText: the database refuses these bytes, so without this the
		// registration fails at the last statement and answers 500.
		errs.add("name", "That name contains a character we cannot store. Retype it.")
	}

	b.VATNumber = NormaliseVAT(b.VATNumber)
	switch {
	case b.VATNumber == "":
		errs.add("vat_number", "Tappa is for registered businesses, so a VAT number is required.")
	case !ValidVATFormat(b.VATNumber):
		// The message names the SHAPE rather than the country list: a customer who
		// mistyped needs to look at their own number, and a customer outside the EU
		// VAT area needs to know that is the problem.
		errs.add("vat_number", "That does not look like an EU VAT number. It starts with two "+
			"letters for the country — MT for Malta — followed by the number itself.")
	}

	if !ValidBusinessType(b.BusinessType) {
		errs.add("business_type", "Pick the closest match — you can change it later.")
	}
	if !ValidStructure(b.Structure) {
		errs.add("structure", "Choose one place or several.")
	}
	return b, errs
}

// ValidateVenues checks step two against the structure chosen in step one.
//
// THE STRUCTURE DECIDES THE VENUE SHAPE. The M7-02 card asks for more than that
// ("`structure` (`single|multi`) seçimi lokasyon/departman modelini belirliyor")
// and the department half of it is wrong — see the const block above; the card was
// corrected in docs/plan/m7-portal.md rather than obeyed.
//
//	single  EXACTLY ONE venue. More than one is not a typo to be trimmed — it means
//	        the earlier answer was wrong, and silently keeping the first would give
//	        the business a model it did not choose.
//	multi   ONE OR MORE, up to MaxVenues. One is legitimate here: a chain that opens
//	        its second branch next month registers as multi with one venue today.
//
// ⚠️ IT IS THE ONLY THING THE STRUCTURE DECIDES. This line used to end "and
// departments (M7-03) are the other thing multi unlocks"; departments are open to
// both shapes and always were — see the const block above for the three
// measurements that retired the claim.
func ValidateVenues(structure string, venues []Venue) ([]Venue, Errors) {
	errs := Errors{}
	out := make([]Venue, 0, len(venues))
	for _, v := range venues {
		name := collapseSpaces(v.Name)
		if name == "" {
			// A blank row is somebody who pressed "add" and changed their mind, not
			// an error. Dropping it is the only interpretation that is not annoying.
			continue
		}
		out = append(out, Venue{Name: name})
	}

	switch {
	case len(out) == 0:
		errs.add("venues", "Name at least one place — the door your team will tap at.")
	case len(out) > MaxVenues:
		errs.add("venues", fmt.Sprintf("Start with up to %d places; you can add the rest "+
			"from your dashboard.", MaxVenues))
	case structure == StructureSingle && len(out) > 1:
		errs.add("venues", "You chose one place. Go back and choose several if you have more "+
			"than one.")
	}
	for i, v := range out {
		switch {
		case utf8.RuneCountInString(v.Name) > MaxVenueNameRunes:
			errs.add(fmt.Sprintf("venue_%d", i), fmt.Sprintf("That is longer than %d characters.",
				MaxVenueNameRunes))
		case !isStorableText(v.Name):
			errs.add(fmt.Sprintf("venue_%d", i),
				"That name contains a character we cannot store. Retype it.")
		}
	}
	// Two venues with the same name are not refused by the schema and are refused
	// here, because the panel identifies a venue to a manager BY ITS NAME (M6-06's
	// screen, and the docket motif prints it). Two identical rows would be
	// indistinguishable on every screen in the product.
	seen := make(map[string]bool, len(out))
	for i, v := range out {
		key := strings.ToLower(v.Name)
		if seen[key] {
			errs.add(fmt.Sprintf("venue_%d", i), "You have already named this place.")
		}
		seen[key] = true
	}
	return out, errs
}

// ValidateAccount checks step three.
//
// ⚠️ THE EMAIL CHECK IS DELIBERATELY SHALLOW, and that is a decision rather than
// laziness. internal/adminauth/manager.go states the rule from its side: "a Go-side
// notion of 'a valid email' is a second source of truth about what
// admin_users.email accepts". What is checked is what the SYSTEM needs — the two
// things Postgres itself rejects (a NUL byte, invalid UTF-8), a length bound, and
// the one structural fact without which the address cannot be delivered to at all
// (exactly one '@', with something either side of it). A pattern claiming to
// implement RFC 5322 would refuse genuine addresses and is the classic way to lose
// a customer at the last step.
func ValidateAccount(a Account) (Account, Errors) {
	errs := Errors{}
	a.FullName = collapseSpaces(a.FullName)
	switch {
	case a.FullName == "":
		errs.add("full_name", "Tell us your name — it is what your team sees beside a decision.")
	case utf8.RuneCountInString(a.FullName) > MaxPersonNameRunes:
		errs.add("full_name", fmt.Sprintf("That is longer than %d characters.", MaxPersonNameRunes))
	case !isStorableText(a.FullName):
		errs.add("full_name", "That name contains a character we cannot store. Retype it.")
	}

	a.Email = strings.TrimSpace(a.Email)
	switch {
	case a.Email == "":
		errs.add("email", "We need an address to sign you in with.")
	case utf8.RuneCountInString(a.Email) > MaxEmailRunes:
		errs.add("email", "That address is too long.")
	case !deliverableShape(a.Email):
		errs.add("email", "That does not look like an email address.")
	}

	switch n := utf8.RuneCountInString(a.Password); {
	case a.Password == "":
		errs.add("password", "Choose a password.")
	case n < MinPasswordRunes:
		errs.add("password", fmt.Sprintf("Use at least %d characters. A few words you will "+
			"remember beats a short one you will not.", MinPasswordRunes))
	case len(a.Password) > MaxPasswordBytes:
		// BYTES, not runes, and the difference is the point: bcrypt's limit is a byte
		// limit and its COMPARER silently truncates past it (internal/adminauth
		// measured that a 100-byte password authenticates an account whose password
		// is its first 72 bytes). Refusing here is where the error is cheap.
		errs.add("password", "That password is too long — keep it under 72 bytes.")
	}
	return a, errs
}

// Validate runs all three steps. The wizard checks each step as it is submitted;
// this is what Provision calls, so a Draft assembled any other way — a replayed
// cookie, a future API — cannot skip a rule the form applied.
func Validate(d Draft) (Draft, Errors) {
	errs := Errors{}
	b, be := ValidateBusiness(d.Business)
	v, ve := ValidateVenues(b.Structure, d.Venues)
	a, ae := ValidateAccount(d.Account)
	for _, m := range []Errors{be, ve, ae} {
		for k, val := range m {
			errs.add(k, val)
		}
	}
	return Draft{Business: b, Venues: v, Account: a}, errs
}

// ------------------------------------------------------------ provisioning --

// Result is what a completed registration produced. IT CARRIES NO PASSWORD, NO
// DIGEST AND NO EMAIL — there is no field for any of them, so a %+v of this value
// at any log level cannot leak one.
type Result struct {
	TenantID     uuid.UUID
	AdminUserID  uuid.UUID
	BusinessName string
	// VenueNames is what was created, in the order it was created. The done screen
	// prints them back so a customer can see the wizard understood them.
	VenueNames []string
	// VAT is what VIES said at sign-up time, carried so the done screen can be
	// honest about a check that did not complete.
	VAT VATStatus

	// SignInBlocked is true when this account was MEASURED to be unreachable by the
	// sign-in path: the address resolves to more identities than one login compares
	// (adminauth.MaxCandidates) and this one sorts outside that window.
	//
	// 🔴 IT EXISTS BECAUSE MIGRATION 00017 MADE A TRADE AND THE HALF THAT JUSTIFIED IT
	// WAS NOT BUILT. Ordering the resolver by created_at protects the INCUMBENT and,
	// deterministically, puts a NEW registration last — so somebody registering with
	// an address that already carries MaxCandidates rows gets an account they can
	// never sign into. 00017 and ADR 0013 defended that as acceptable because "a
	// customer who cannot complete a sign-up finds out immediately, at a moment when
	// they are on our page and can be told something". AN AUDIT MEASURED THAT IT WAS
	// NOT TRUE: the wizard answered 303 to a confirmation stamped APPROVED that said
	// "Sign in — use the email address and password you just chose", and the very next
	// request answered 401 with no explanation, on the one screen that must not give
	// one (00011's OBLIGATION 1).
	//
	// This field is that sentence made real. FALSE is the default and the answer
	// whenever the check could not be taken: telling a customer their brand-new
	// account is broken because OUR read failed would be the §4.6 error in the other
	// direction.
	SignInBlocked bool
}

// ErrVATTaken means the VAT number already belongs to a tenant.
//
// IT IS A NAMED SENTINEL BECAUSE IT IS THE ONE FAILURE THE VISITOR CAN ACT ON. The
// UNIQUE constraint on tenants.vat_number (migration 00001) is what makes "one
// business, one tenant" true; hitting it means either this business is already
// registered — in which case the answer is to sign in — or the number is wrong.
//
// ⚠️ IT IS AN ENUMERATION SIGNAL AND IT IS ACCEPTED RATHER THAN HIDDEN, which is
// the opposite of the choice the LOGIN path makes about email addresses and
// therefore needs its reason stated. A VAT number is PUBLIC DATA: anybody can put
// one into VIES and be told whether it is registered, by the European Commission,
// for free. Refusing to say what a public register says would buy nothing and would
// cost a real customer the one sentence that tells them what to do. An email
// address is not public data, which is why 00011's OBLIGATION 1 exists and why this
// package asks the database nothing about email.
var ErrVATTaken = errors.New("signup: that VAT number is already registered")

// Provisioner turns a validated Draft into a business.
type Provisioner struct {
	data  Database
	trail Trail
	log   *slog.Logger
	// newID is the tenant/id source, injectable ONLY for tests that need a
	// collision. Production leaves it nil and uses uuid.NewRandom (crypto/rand).
	newID func() (uuid.UUID, error)
}

// NewProvisioner wires it. Both dependencies are required: a nil recorder would
// silently drop the §4.6 trail for the one event that creates a customer.
func NewProvisioner(data Database, trail Trail, log *slog.Logger) (*Provisioner, error) {
	if data == nil {
		return nil, errors.New("signup: nil database")
	}
	if trail == nil {
		return nil, errors.New("signup: nil audit trail")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Provisioner{data: data, trail: trail, log: log}, nil
}

func (p *Provisioner) mintID() (uuid.UUID, error) {
	if p.newID != nil {
		return p.newID()
	}
	// uuid.NewRandom reads crypto/rand and RETURNS AN ERROR, unlike uuid.New which
	// panics. A registration failing because entropy is unavailable is a 500 with a
	// log line; a panic is a process the recoverer has to catch on an
	// unauthenticated route.
	return uuid.NewRandom()
}

// signupDetail is the audit payload. A PURPOSE-BUILT STRUCT, NOT A MAP
// (internal/audit's rule): there is no field here through which a password, a
// digest or an email could travel, so no later edit adds one by accident.
//
// THE VAT NUMBER IS IN IT AND THE EMAIL IS NOT, which is a deliberate split rather
// than an oversight. A VAT number is the business's public registration and is the
// single most useful thing an investigator has when asking "who registered this
// tenant"; an email address is personal data, and audit_log cannot be deleted from
// (00005), so a row carrying one is a row carrying it forever.
type signupDetail struct {
	VATNumber    string `json:"vat_number"`
	VATCheck     string `json:"vat_check"`
	BusinessType string `json:"business_type"`
	Structure    string `json:"structure"`
	Venues       int    `json:"venues"`
}

// Provision creates the tenant, its venues and its first operator IN ONE
// TRANSACTION.
//
// 🔴 ONE TRANSACTION IS THE WHOLE POINT AND IT IS M7-03's CRITERION HONOURED EARLY
// ("Başarısızlıkta yarım tenant kalmıyor (rollback)"). A half-provisioned business
// is the worst outcome available here: a `tenants` row with no operator cannot be
// signed into, is not removable by anything in this repository, and holds the VAT
// number against the customer's own second attempt. db.WithTenant rolls back on any
// error and on a panic, so every failure below leaves nothing.
//
// ⚠️ "NOT REMOVABLE" IS THE MEASURED WORDING, AND AN EARLIER VERSION GAVE THE WRONG
// REASON FOR IT. It said "00001 grants no path a customer can reach" — and 00001
// grants tappa_app TABLE-LEVEL DELETE on `tenants`, which no migration has ever
// revoked (measured: relacl is `tappa_app=rd/tappa_owner`). What actually holds is
// weaker and worth stating correctly: every child table references `tenants` ON
// DELETE RESTRICT, so a tenant with any location, employee, admin or audit row
// cannot be removed — and a tenant with NOTHING under it, which is exactly the
// half-provisioned row this paragraph is about, is the one shape that COULD be, by a
// statement nothing here writes. The conclusion stands; the mechanism named for it
// was false, and a false mechanism is what the next reader builds on.
//
// 🔴 THE TENANT CONTEXT IS SET TO A ROW THAT DOES NOT EXIST YET. That is the one
// genuinely unusual thing this function does and db/queries/tenants.sql argues it
// at length: the `tenants` policy scopes on `id`, so the INSERT can only satisfy
// WITH CHECK if the context already equals the id being written. What makes it safe
// rather than alarming is that the id is MINTED HERE from crypto/rand and reaches
// this function from nowhere else — no form field, no cookie, no query parameter —
// and that a context pointing at a non-existent tenant makes RLS show this
// transaction NOTHING. The wizard runs with strictly less reach than any
// authenticated panel request.
//
// ORDER: tenant, then venues, then the operator, then the trail. The operator comes
// after the venues so a venue name that violates something takes the whole
// registration down before a password digest has been written anywhere; the trail
// comes last because it describes what the other three did.
//
// ⚠️ WHAT IT DOES NOT DO, NAMED RATHER THAN LEFT TO BE DISCOVERED:
//
//   - NO DEPARTMENTS. `multi` unlocks them and the wizard does not ask; M7-03's card
//     carries them and the panel already has the screen (M6-06).
//   - NO PLAQUE. A tenant is created with no `tags` row, so a brand-new business has
//     venues nobody can tap at yet. That is correct — Tappa encodes and ships the
//     plaque (user decision 2026-08-08, cmd/tappa/main.go) — and M7-03's "waiting
//     for a plaque" state is what tells the customer so.
//   - NO POLICY ROWS. The baseline is materialised lazily by internal/domain/checkin
//     the first time a decision is made for the tenant, so a registration that wrote
//     policy rows would be a second materialiser.
func (p *Provisioner) Provision(ctx context.Context, d Draft) (Result, error) {
	d, errs := Validate(d)
	if errs.Any() {
		// Unreachable through the wizard, which validates every step as it is
		// submitted. It is checked anyway because THIS is the function that writes,
		// and a rule enforced only by the caller is a rule the next caller does not
		// inherit.
		return Result{}, fmt.Errorf("signup: provision: %d field(s) did not validate", len(errs))
	}

	// Hashing happens BEFORE the transaction opens. bcrypt at cost 12 is ~380 ms of
	// CPU (internal/adminauth measured it), and holding a pooled connection — and a
	// row lock on nothing — for that long would put an unauthenticated caller's CPU
	// budget inside the database's connection budget. The tap surface shares that
	// pool.
	digest, err := adminauth.Hash(d.Account.Password)
	if err != nil {
		// The error from adminauth names a length and never a value, and it is
		// wrapped rather than replaced so the package name is on it.
		return Result{}, fmt.Errorf("signup: provision: %w", err)
	}

	tenantID, err := p.mintID()
	if err != nil {
		return Result{}, fmt.Errorf("signup: provision: minting a tenant id: %w", err)
	}

	var (
		adminID uuid.UUID
		venues  = make([]string, 0, len(d.Venues))
	)
	err = p.data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)

		if _, e := q.CreateTenant(ctx, store.CreateTenantParams{
			ID:           tenantID,
			Name:         d.Business.Name,
			VatNumber:    d.Business.VATNumber,
			BusinessType: d.Business.BusinessType,
			Structure:    d.Business.Structure,
			Timezone:     DefaultTimezone,
			VatVerified:  d.Business.VAT.Verified(),
			VatCheckedAt: checkedAt(d.Business.VAT),
		}); e != nil {
			return e
		}

		for _, v := range d.Venues {
			// EVERY OPTIONAL COLUMN IS NIL AND NONE IS DEFAULTED TO A ZERO.
			// db/queries/locations.sql states the consequence of getting this wrong:
			// a shift of 00:00 makes every arrival late, and a coordinate of 0,0 is a
			// real point in the Gulf of Guinea that would put every tap 5 000 km from
			// its venue. A new venue has no proof of place configured, and that is a
			// state §5 rows 6-7 already handle (`flag`, recorded, queued for a
			// manager) rather than a gap.
			//
			// ⚠️ static_ips IS AN EMPTY SLICE AND **NOT** nil, and the difference is a
			// constraint violation rather than a style point. The column is
			// `cidr[] NOT NULL DEFAULT '{}'` (migration 00002) and this query NAMES it,
			// so a nil slice is sent as SQL NULL and the INSERT fails with 23502 —
			// measured, before this line said so. The empty set is the schema's own
			// representation of "no IP evidence configured", chosen there over NULL
			// because `<<= ANY('{}')` is cleanly FALSE while `ANY(NULL)` is NULL.
			row, e := q.CreateLocation(ctx, store.CreateLocationParams{
				TenantID:  tenantID,
				Name:      v.Name,
				StaticIps: []netip.Prefix{},
				Overnight: false,
			})
			if e != nil {
				return e
			}
			venues = append(venues, row.Name)
		}

		email := d.Account.Email
		row, e := q.CreateAdminUser(ctx, store.CreateAdminUserParams{
			TenantID:     tenantID,
			FullName:     d.Account.FullName,
			Email:        &email,
			PasswordHash: digest,
			// 'owner' rather than a constant from this package: migration 00006's
			// CHECK is the authority on the vocabulary and the first operator of a
			// business they just registered is its owner by definition.
			Role: "owner",
		})
		if e != nil {
			return e
		}
		adminID = row.ID

		// THE TRAIL SHARES THE TRANSACTION (see the Trail interface). ActorID is the
		// operator who was just created: they caused this, and they are the only
		// person this event can be attributed to.
		_, e = p.trail.RecordTx(ctx, tx, audit.Event{
			TenantID: tenantID,
			ActorID:  &adminID,
			Action:   ActionSignupCompleted,
			Target:   tenantID.String(),
			Detail: signupDetail{
				VATNumber:    d.Business.VATNumber,
				VATCheck:     d.Business.VAT.String(),
				BusinessType: d.Business.BusinessType,
				Structure:    d.Business.Structure,
				Venues:       len(d.Venues),
			},
		})
		return e
	})
	if err != nil {
		if isUniqueViolation(err, "tenants_vat_number_key") {
			return Result{}, ErrVATTaken
		}
		return Result{}, fmt.Errorf("signup: provision: %w", err)
	}

	blocked := p.signInBlocked(ctx, d.Account.Email, tenantID, adminID)

	// The log line names the tenant and the VAT check, and NOT the operator's
	// address (§4.7's spirit: this request was unauthenticated until this instant,
	// and a process log is not the place for personal data).
	p.log.Info("a business registered",
		"tenant_id", tenantID, "venues", len(venues), "vat_check", d.Business.VAT.String(),
		"sign_in_blocked", blocked)

	return Result{
		TenantID:      tenantID,
		AdminUserID:   adminID,
		BusinessName:  d.Business.Name,
		VenueNames:    venues,
		VAT:           d.Business.VAT,
		SignInBlocked: blocked,
	}, nil
}

// signInBlocked reports whether the account just created sorts OUTSIDE the window a
// login compares, i.e. whether this customer can ever sign in.
//
// 🔴 IT RUNS AFTER THE COMMIT AND ONLY THERE, WHICH IS WHAT KEEPS IT FROM BEING AN
// ORACLE. internal/handler/signup.go states the flow's rule — it never asks the
// database whether an email exists, because that is the question 00011's OBLIGATION 1
// spends a dummy bcrypt refusing to answer. This does ask, and the reason it is not
// the same thing is the PRICE and the SCOPE:
//
//   - IT IS REACHABLE ONLY BY COMPLETING A REGISTRATION. A caller must create a
//     tenant — a real row, a real VAT number, against signupCreateLimit — before this
//     runs at all. There is no pre-registration probe here.
//   - IT BRANCHES ON ONE BIT AND NOT ON THE LIST. The only thing that leaves this
//     function is "is the row I just wrote inside the window".
//
// 🔴 AND THAT BIT IS A CHANNEL, WHICH FIVE PLACES IN THIS REPOSITORY USED TO DENY. They
// said "an address with one, two or seven existing accounts answers exactly as an
// unknown one does" — TRUE ONLY WHILE THE ATTACKER PLANTS NOTHING, and planting is the
// premise of every other control in this area. Measured against real Postgres: plant
// adminauth.MaxCandidates-1 rows for an address, then complete ONE MORE registration.
// Unknown address -> 8 identities total -> false. One existing account -> 9 -> true.
// This package's own table test is that measurement (7 -> false, 8 -> true, where the
// 8 is the attacker's seven plus the victim's one).
//
// ⚠️ AND IT IS NOT ONE BIT IF THE ATTACKER VARIES THE PLANT COUNT. Registering at
// several plant counts and observing where the flag flips reveals k, the number of
// identities that were already there, exactly — not merely whether k > 0.
//
// THE SHAPE IS THE ONE MIGRATION 00017 REJECTED AS CLOSURE #2 ("the identical oracle
// reappears at the new bound"), and it is what the withdrawn general statement
// described: a bound the caller can saturate reports itself at the bound. The
// difference from the picker count is that this is not a DISPLAY cap that a narrower
// one could absorb — it is the comparison window's edge, reported.
//
// WHY IT IS KEPT ANYWAY, PRICED: the probe costs adminauth.MaxCandidates COMPLETED
// registrations per address, each needing its own globally UNIQUE VAT number and a
// unit of signupCreateLimit (3 per hour per address) — about three hours from one
// address, i.e. roughly EIGHT TIMES the price of the timing channel that was closed.
// Removing the check restores a defect an audit measured end to end: a real customer
// gets an APPROVED stamp and then a bare 401 on the one screen forbidden to explain
// itself. A certain loss to a paying customer is worse than an expensive, counted,
// observable signal.
//   - THE CHEAP VERSION OF THE SAME QUESTION IS CLOSED ELSEWHERE. adminauth pads every
//     login to a constant number of comparisons (M7-02 round 4), so timing no longer
//     answers it, and the picker is capped so its row count no longer answers it.
//
// A FAILED READ IS NOT A BLOCKED ACCOUNT. The error is logged and the answer is
// false: the business exists either way, and an outage on our side must not tell a
// customer their account is broken.
func (p *Provisioner) signInBlocked(ctx context.Context, email string, tenantID, adminID uuid.UUID) bool {
	rows, err := p.data.GetAdminByEmail(ctx, email)
	if err != nil {
		// Never swallowed (§7). It does not change the outcome of the registration,
		// which has already committed.
		p.log.Error("signup: could not check whether the new account is reachable by sign-in",
			"err", err)
		return false
	}
	if len(rows) <= adminauth.MaxCandidates {
		// The window holds everything, so position cannot matter.
		return false
	}
	for _, r := range rows[:adminauth.MaxCandidates] {
		if r.ID == adminID {
			return false
		}
	}
	p.observeUnreachable(ctx, tenantID, adminID, len(rows))
	return true
}

// observeUnreachable leaves a trail for the one channel this package could not close.
//
// 🔴 IT IS THE "COUNTED" HALF OF signInBlocked, in the shape
// internal/handler.AdminAuth.recordCandidateProbe uses on the login side: a channel
// that is not closed must at least be visible, or nobody can tell it was used.
//
// WHERE THE ROW GOES: into the tenant this registration just CREATED, attributed to
// the operator it just created. That is the caller's own tenant by construction —
// there is no id here that came from a request, so this cannot be aimed at a third
// party's undeletable audit_log, which is the line recordCandidateProbe draws.
//
// IT IS ALREADY BOUNDED AND NEEDS NO BUDGET OF ITS OWN. This runs only after a
// registration COMMITS, and completed registrations are metered at signupCreateLimit
// (3 per hour per address). No new mechanism is introduced; the existing create budget
// is the ceiling.
//
// §4.7: the row carries NO email and no digest — the address is the thing being asked
// about, and audit_log cannot be deleted from. What it carries is two counts and the
// window they were compared against, which is what an investigator needs to see that
// an address was stuffed.
//
// A FAILED WRITE DOES NOT FAIL THE REGISTRATION. The business exists; the trail is the
// observation, not the act. It is never swallowed (§7).
func (p *Provisioner) observeUnreachable(ctx context.Context, tenantID, adminID uuid.UUID, resolved int) {
	if _, err := p.trail.Record(ctx, audit.Event{
		TenantID: tenantID,
		ActorID:  &adminID,
		Action:   ActionSignupUnreachable,
		Target:   tenantID.String(),
		Detail: unreachableDetail{
			Resolved: resolved,
			Compared: adminauth.MaxCandidates,
			Reason: "the address already resolves to more identities than one sign-in " +
				"compares, and this registration sorts outside that window",
		},
	}); err != nil {
		p.log.Error("audit write failed", "action", ActionSignupUnreachable, "err", err)
	}
	// The address is NOT in this line either, for the reason adminlogin.go gives about
	// its own unauthenticated paths: a flood of attempts would otherwise write a list
	// of addresses into the process log.
	p.log.Warn("signup: the new account cannot be reached by sign-in",
		"tenant_id", tenantID, "resolved", resolved, "compared", adminauth.MaxCandidates)
}

// unreachableDetail is the audit payload. A purpose-built struct with no field an
// address, a password or a digest could travel in (internal/audit's rule).
type unreachableDetail struct {
	Resolved int    `json:"resolved"`
	Compared int    `json:"compared"`
	Reason   string `json:"reason"`
}

// checkedAt is the stamp that distinguishes "we never asked" from "we asked and got
// no answer" — migration 00017's four-state vocabulary. A registration that never
// reached VIES (a format failure cannot get this far, so in practice this is a
// deployment with no checker wired) stores NULL in both columns.
func checkedAt(s VATStatus) *time.Time {
	if s == VATUnknown {
		// ⚠️ THIS COLLAPSES TWO OF THE FOUR STATES AND THE LIMIT IS STATED. "Asked,
		// no answer" and "never asked" both store NULL here, because this package
		// cannot tell them apart: Checker.Check returns VATUnknown for a timeout AND
		// for a nil checker, deliberately (vies.go: there is no error value a caller
		// could turn into a refusal). Distinguishing them needs a fourth VATStatus,
		// which would buy a panel message nobody has asked for yet. The two states
		// that MATTER — "the register says no" and "the register says yes" — are
		// distinct and stored.
		return nil
	}
	now := time.Now().UTC()
	return &now
}

// isUniqueViolation reports whether err is a Postgres 23505 on the named
// constraint.
//
// IT CHECKS THE CONSTRAINT NAME, not just the code. A registration can only violate
// one unique constraint today, but `admin_users_tenant_email_key` is one statement
// away from being the second, and a handler that told a customer "that VAT number
// is taken" because their EMAIL collided would be worse than an unexplained failure.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}

// ------------------------------------------------------------------ helpers --

// collapseSpaces trims and squeezes runs of whitespace to single spaces.
//
// IT IS NORMALISATION, NOT VALIDATION, and it runs before every check so the value
// that is checked is the value that is stored — the same one-representation rule
// NormaliseVAT is written under. Without it, "Kebab  Factory" and "Kebab Factory"
// are two different businesses on every screen in the product.
func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// isStorableText reports whether a value can be sent to Postgres at all.
//
// 🔴 IT CLOSES THE SAME DEFECT internal/adminauth CLOSED FOR THE EMAIL, ON THE SAME
// TWO BYTE CLASSES, AND M7-02 SHIPPED IT OPEN ON ITS OWN WRITE PATH. adminauth's
// isLookupableEmail checks a NUL byte and invalid UTF-8 because Postgres refuses both
// outright ("invalid byte sequence for encoding UTF8", SQLSTATE 22021), and
// manager.go states the reason the fix is a CHECK rather than a note: "the panel stops
// answering 500 on an unauthenticated path", the response joins the single uniform
// refusal, and the request stops being free.
//
// MEASURED HERE BEFORE THE FIX: ValidateVenues(single, {Name: "Front\x00door"})
// returned NO error, Provision then failed with 22021, and the boundary turned that
// into HTTP 500 while charging the attempt budget. Every consequence adminauth names
// applied, on the one unauthenticated surface in this product that WRITES.
//
// IT IS DELIBERATELY NARROW. It checks what the DATABASE refuses, not what a name
// "should" look like: unusual scripts, emoji, punctuation and length are somebody
// else's business (the length bounds above, and the column). A Go-side notion of a
// valid business name would be a second source of truth about what the column accepts
// — the same argument isLookupableEmail makes for addresses.
func isStorableText(s string) bool {
	if strings.ContainsRune(s, 0) {
		return false
	}
	return utf8.ValidString(s)
}

// hasLetter reports whether s contains at least one letter, in any script.
//
// unicode.IsLetter rather than an ASCII range: Maltese uses ċ ġ ħ ż, the brand
// faces are shipped with the latin-ext subset for exactly that reason (skill
// tappa-brand), and a business called "Ħaż-Żebbuġ Catering" must be able to
// register. The check exists to refuse "123" and "-----", not to police a script.
func hasLetter(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

// deliverableShape reports the minimum structure an address needs to be delivered
// to at all: exactly one '@', something before it, something with a dot after it,
// no whitespace, no NUL and valid UTF-8.
//
// THE LAST TWO ARE THE DATABASE'S OWN REFUSALS, mirrored from
// internal/adminauth.isLookupableEmail — Postgres rejects a NUL byte and invalid
// UTF-8 outright, and an address carrying either would reach the driver and come
// back as a 500 on an unauthenticated path.
func deliverableShape(s string) bool {
	if strings.ContainsRune(s, 0) || !utf8.ValidString(s) {
		return false
	}
	if strings.ContainsFunc(s, unicode.IsSpace) {
		return false
	}
	local, domain, ok := strings.Cut(s, "@")
	if !ok || local == "" || domain == "" {
		return false
	}
	if strings.Contains(domain, "@") {
		return false
	}
	// A dot with something either side of it. This refuses "a@localhost", which is
	// deliverable on a network and is never what somebody meant to type into a
	// sign-up form on the public internet.
	dot := strings.LastIndex(domain, ".")
	return dot > 0 && dot < len(domain)-1
}
