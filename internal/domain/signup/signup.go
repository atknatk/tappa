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
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/internal/store"
)

// ActionSignupCompleted is the audit action a completed registration writes. The
// vocabulary is free text by schema decision (00005), so this constant is the
// vocabulary.
//
// SINCE M14-17 THE ROW IS WRITTEN BY THE DATABASE, NOT BY THIS PACKAGE: identity_claim_signup
// (migration 00078) writes it in the registration's own transaction, with this package's
// detail key for key (vat_number, vat_check, business_type, structure, venues) and the new
// owner as its actor -- the same row whether the address was new or an existing identity's
// (ADR 0031 §5.2: a function that writes a tenant writes that tenant's trail). The constant
// stays the vocabulary.
const ActionSignupCompleted = "signup.completed"

// Database is the narrow slice of *db.DB this package needs, declared HERE at the
// consumer (§7).
//
// ONE METHOD, AND IT IS THE TENANT-SCOPED ONE. A registration knows which tenant it is
// creating because it is the one minting the id. Since M14-17 nothing here asks the
// database about an e-mail address at all: the address reaches this package only inside a
// draft its mailbox already confirmed (ADR 0031 §5.10), and the context-less resolver the
// sign-in reachability check used is gone with that check (signInBlocked retired -- the
// identity path has no candidate window to fall outside of).
type Database interface {
	WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error
}

// Claim is the proof a registration draft was confirmed (internal/adminauth.ClaimProof: a
// re-entered password that matched the draft's digest, or the address's own panel session).
// It is declared here at the consumer (§7) and does exactly two things: hand over the draft
// it proves, and consume it INSIDE this package's transaction, writing the owner row.
//
// 🔴 THE ONLY IMPLEMENTATION THAT REACHES THE DATABASE IS adminauth.ClaimProof, whose fields
// are unexported -- so the claim cannot be made without the comparison or the session
// (ADR 0031 §5.2 S12; internal/adminauth's TestSignupClaimProof_HasItsProducersAndOneConsumer).
type Claim interface {
	Draft() []byte
	Claim(ctx context.Context, tx pgx.Tx, tenantID uuid.UUID) (uuid.UUID, error)
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
	// EIGHT, AND IT IS A FLOOR RATHER THAN A COMPOSITION RULE. There is no "one
	// upper case, one digit, one symbol" here on purpose: those rules measurably
	// push people toward `Password1!` and this product has a better lever — bcrypt
	// at cost 12 (internal/adminauth) makes an offline guess expensive, and the
	// panel's attempt budget makes an online one pointless.
	//
	// The floor was lowered from twelve to eight as a deliberate product call
	// ("keep it simple", user decision); the levers that carry the security -- bcrypt
	// cost 12, the attempt budget, per-account isolation -- did not move. See ADR 0019.
	MinPasswordRunes = 8

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
		errs.add("vat_number", "Taptime is for registered businesses, so a VAT number is required.")
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
		if isStorableText(name) {
			// The venue editor's cleaning (tenant.CleanName: the soft hyphen, the direction marks and
			// a BOM at either end go) -- the wizard writes the same column (API-5 review, fourth round).
			name = collapseSpaces(tenant.CleanName(name))
		}
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
		case tenant.NameHasHiddenCharacters(v.Name):
			// The venue editor's character rule (internal/domain/tenant): the wizard opens venues
			// too, and a name it let through would be exempt from the rule forever after (an
			// unchanged name on file is never refused).
			errs.add(fmt.Sprintf("venue_%d", i),
				"That name has a hidden character in it, such as a direction override or a zero-width space, usually from pasted text. Type it again.")
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
//
// 🔴 2026-10-26 (M14-08a, user decision — ADR 0031 §5.1's address rule): SHALLOW NO
// LONGER, ON ONE AXIS. The address births an identity (00074) and is the only place a
// reset can go, and citext folds U+0130 (LATIN CAPITAL LETTER I WITH DOT ABOVE) and
// U+212A (KELVIN SIGN) onto ASCII: a homoglyph registered first would squat the real
// mailbox's identity for good, because mail goes to printable-ASCII recipients alone. So the address must also pass
// mail.ValidRecipient — the rule internal/mail's Send applies, the same function and not
// a copy, so the "second source of truth" above does not arise: it is the truth of
// what we can SEND to. Migration 00076 holds the database to the same rule.
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
	case !asciiOnly(a.Email):
		errs.add("email", "That address has a letter we cannot send mail to — an accent or "+
			"a non-Latin character. Type it with plain a–z letters.")
	case !mail.ValidRecipient(a.Email):
		errs.add("email", "That does not look like an email address we can send mail to.")
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

// ------------------------------------------------------------ the draft --

// claimDraft is the registration draft as admin_signup_claims stores it: THE CLOSED KEY SET
// (ADR 0031 §5.2 and its 2026-10-26 user decision -- full_name, business_name, vat_number,
// business_type, structure, vat_check, venues), mirrored by 00078's CHECK. It carries NO
// password and NO address: the digest and the address are columns of their own.
type claimDraft struct {
	FullName     string   `json:"full_name"`
	BusinessName string   `json:"business_name"`
	VATNumber    string   `json:"vat_number"`
	BusinessType string   `json:"business_type"`
	Structure    string   `json:"structure"`
	VATCheck     string   `json:"vat_check"`
	Venues       []string `json:"venues"`
}

// ErrDraftInvalid means a draft did not validate (encoding) or did not decode to one that does.
var ErrDraftInvalid = errors.New("signup: the registration draft is not valid")

// EncodeDraft validates a complete registration (Validate -- the password included, though it
// is not stored here) and returns the draft's JSON, the address normalised as ValidateAccount
// leaves it. The password is the caller's to hash; the address is the caller's to store.
func EncodeDraft(d Draft) ([]byte, Draft, error) {
	v, errs := Validate(d)
	if errs.Any() {
		return nil, Draft{}, fmt.Errorf("%w: %d field(s)", ErrDraftInvalid, len(errs))
	}
	venues := make([]string, 0, len(v.Venues))
	for _, x := range v.Venues {
		venues = append(venues, x.Name)
	}
	b, err := json.Marshal(claimDraft{
		FullName: v.Account.FullName, BusinessName: v.Business.Name, VATNumber: v.Business.VATNumber,
		BusinessType: v.Business.BusinessType, Structure: v.Business.Structure,
		VATCheck: v.Business.VAT.String(), Venues: venues,
	})
	if err != nil {
		return nil, Draft{}, fmt.Errorf("signup: encode draft: %w", err)
	}
	return b, v, nil
}

// DecodeDraft reads a stored draft back -- unknown keys refused -- and validates it again
// (every rule but the password, which the draft does not carry): a draft that reached the
// table some other way still cannot skip a rule the form applied.
func DecodeDraft(b []byte) (Draft, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c claimDraft
	if err := dec.Decode(&c); err != nil {
		return Draft{}, fmt.Errorf("%w: %w", ErrDraftInvalid, err)
	}
	venues := make([]Venue, 0, len(c.Venues))
	for _, n := range c.Venues {
		venues = append(venues, Venue{Name: n})
	}
	d := Draft{
		Business: Business{Name: c.BusinessName, VATNumber: c.VATNumber, BusinessType: c.BusinessType,
			Structure: c.Structure, VAT: parseVATStatus(c.VATCheck)},
		Venues:  venues,
		Account: Account{FullName: c.FullName},
	}
	b2, be := ValidateBusiness(d.Business)
	v2, ve := ValidateVenues(b2.Structure, d.Venues)
	b2.VAT = d.Business.VAT
	a2 := d.Account
	a2.FullName = collapseSpaces(a2.FullName)
	if a2.FullName == "" || utf8.RuneCountInString(a2.FullName) > MaxPersonNameRunes || !isStorableText(a2.FullName) {
		return Draft{}, fmt.Errorf("%w: the name", ErrDraftInvalid)
	}
	if be.Any() || ve.Any() {
		return Draft{}, fmt.Errorf("%w: %d field(s)", ErrDraftInvalid, len(be)+len(ve))
	}
	return Draft{Business: b2, Venues: v2, Account: a2}, nil
}

// parseVATStatus is VATStatus.String's inverse; anything else is VATUnknown (the fail-closed
// verdict: "not checked", never "invalid").
func parseVATStatus(s string) VATStatus {
	switch s {
	case VATValid.String():
		return VATValid
	case VATInvalid.String():
		return VATInvalid
	}
	return VATUnknown
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
	// VATNumber is the registered number, for the done screen.
	VATNumber string
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
//
// SINCE M14-17 IT IS ANSWERED AT THE CONFIRMATION (ADR 0031 §5.10): a draft reserves
// nothing, so of two pending drafts with one VAT number the first confirmed gets the
// business and the second gets this -- today's sentence.
var ErrVATTaken = errors.New("signup: that VAT number is already registered")

// ErrClaimRefused means identity_claim_signup refused the confirmation (a spent or
// expired link, an address that now has an identity on the new address's path, a session
// that is not the address's person, a concurrent confirmation of the same address).
// Nothing was created: the business this transaction made was rolled back with it.
var ErrClaimRefused = errors.New("signup: the registration link could not be confirmed")

// Provisioner turns a confirmed draft into a business.
type Provisioner struct {
	data Database
	log  *slog.Logger
	// newID is the tenant/id source, injectable ONLY for tests that need a
	// collision. Production leaves it nil and uses uuid.NewRandom (crypto/rand).
	newID func() (uuid.UUID, error)
}

// NewProvisioner wires it. The trail is the database's own since M14-17 (identity_claim_signup
// writes signup.completed in this transaction), so it takes none.
func NewProvisioner(data Database, log *slog.Logger) (*Provisioner, error) {
	if data == nil {
		return nil, errors.New("signup: nil database")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Provisioner{data: data, log: log}, nil
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

// Provision creates the business a confirmed draft describes -- the tenant, its venues
// and, through the claim, its first operator -- IN ONE TRANSACTION.
//
// 🔴 ONE TRANSACTION IS THE WHOLE POINT AND IT IS M7-03's CRITERION HONOURED EARLY
// ("Başarısızlıkta yarım tenant kalmıyor (rollback)"). A half-provisioned business
// is the worst outcome available here: a `tenants` row with no operator cannot be
// signed into and holds the VAT number against the customer's own second attempt.
// db.WithTenant rolls back on any error and on a panic, so every failure below --
// identity_claim_signup's refusal included -- leaves nothing.
//
// 🔴 THE TENANT CONTEXT IS SET TO A ROW THAT DOES NOT EXIST YET. db/queries/tenants.sql
// argues it at length: the `tenants` policy scopes on `id`, so the INSERT can only
// satisfy WITH CHECK if the context already equals the id being written. The id is
// MINTED HERE from crypto/rand and reaches this function from nowhere else.
//
// 🔴 TOP-LEVEL, NEVER A SAVEPOINT (db.WithTenant, not db.WithTenantIn): identity_claim_signup
// claims only a business THIS transaction wrote (tenants.xmin = pg_current_xact_id(), S2/T7),
// and a row written under a savepoint carries the subtransaction's xid -- the claim would be
// refused (measured on 17.10, 00074/00078).
//
// ORDER: tenant, then venues, then the claim (the identity, the owner row, the draft spent and
// the signup.completed row -- the database's). The claim is last because its predicates read
// the business this transaction just wrote: no membership yet, born now, this xid, the draft's
// VAT number and name.
func (p *Provisioner) Provision(ctx context.Context, c Claim) (Result, error) {
	if c == nil {
		return Result{}, errors.New("signup: provision: no claim")
	}
	d, err := DecodeDraft(c.Draft())
	if err != nil {
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
			// its venue.
			//
			// ⚠️ static_ips IS AN EMPTY SLICE AND **NOT** nil: the column is
			// `cidr[] NOT NULL DEFAULT '{}'` (migration 00002) and this query NAMES it,
			// so a nil slice is sent as SQL NULL and the INSERT fails with 23502 --
			// measured.
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

		// THE CLAIM (ADR 0031 §5.10, migration 00078): the draft consumed, the identity (a new
		// address) or the session's identity (an existing one), the owner row linked via
		// `claim`, and the business's signup.completed row -- all the database's, in this
		// transaction. Its refusal rolls the business back.
		var e error
		adminID, e = c.Claim(ctx, tx, tenantID)
		if errors.Is(e, pgx.ErrNoRows) {
			return ErrClaimRefused
		}
		return e
	})
	if err != nil {
		if isUniqueViolation(err, "tenants_vat_number_key") {
			return Result{}, ErrVATTaken
		}
		if errors.Is(err, ErrClaimRefused) {
			return Result{}, ErrClaimRefused
		}
		return Result{}, fmt.Errorf("signup: provision: %w", err)
	}

	// The log line names the tenant and the VAT check, and NOT the operator's address
	// (§7) -- nor which path confirmed it (ADR 0031 §5.10: no surface says whether the
	// address was new).
	p.log.Info("a business registered",
		"tenant_id", tenantID, "venues", len(venues), "vat_check", d.Business.VAT.String())

	return Result{
		TenantID:     tenantID,
		AdminUserID:  adminID,
		BusinessName: d.Business.Name,
		VenueNames:   venues,
		VAT:          d.Business.VAT,
		VATNumber:    d.Business.VATNumber,
	}, nil
}

// checkedAt is the stamp that distinguishes "we never asked" from "we asked and got
// no answer" — migration 00017's four-state vocabulary. A registration that never
// reached VIES (a format failure cannot get this far, so in practice this is a
// deployment with no checker wired) stores NULL in both columns.
//
// ⚠️ SINCE M14-17 THE STAMP IS THE CONFIRMATION'S, NOT THE CHECK'S: VIES answered at step one
// and the draft carries only the verdict (its closed keys hold no instant), so the stored time is
// up to SignupClaimTTL (just under 24 hours) after the answer it dates. The verdict itself is the one received.
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

// asciiOnly reports whether every byte of s is below 0x80. It only picks the sentence:
// mail.ValidRecipient refuses the same addresses (and more), and is the rule.
func asciiOnly(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
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
