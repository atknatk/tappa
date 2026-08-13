package signup

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The SERVER-SIDE rules, tested as rules.
//
// 🔴 THE M7-02 CARD'S CRITERION IS "sunucu tarafı doğrulama TAM; istemci
// doğrulaması yalnızca kolaylık", and a browser cannot be trusted to enforce
// anything: `required`, `minlength` and `type="email"` are removed by any client
// that wants to. So every rule the form displays is asserted HERE, where nothing a
// caller sends can skip it, and internal/handler drives the same rules over real
// HTTP with a client that validates nothing.

func TestValidateBusiness_ServerSide(t *testing.T) {
	t.Parallel()
	ok := Business{Name: "Kebab Factory Ltd", VATNumber: "MT12345678", BusinessType: "restaurant", Structure: "single"}
	tests := []struct {
		name  string
		in    Business
		field string // the field expected to carry an error, or "" for none
	}{
		{"a complete business", ok, ""},
		{"no name", withName(ok, ""), "name"},
		{"a name of only spaces", withName(ok, "   "), "name"},
		{"a name with no letter at all", withName(ok, "12345"), "name"},
		{"a name past the bound", withName(ok, strings.Repeat("a", MaxCompanyNameRunes+1)), "name"},
		{"a name at the bound", withName(ok, strings.Repeat("a", MaxCompanyNameRunes)), ""},
		{"Maltese letters are a name", withName(ok, "Ħaż-Żebbuġ Catering"), ""},
		{"no VAT number", withVAT(ok, ""), "vat_number"},
		{"a VAT number with no country prefix", withVAT(ok, "12345678"), "vat_number"},
		{"a VAT number from no country", withVAT(ok, "ZZ12345678"), "vat_number"},
		{"a Maltese number of the wrong length", withVAT(ok, "MT1234567"), "vat_number"},
		{"a VAT number with a sentence appended", withVAT(ok, "MT12345678 or so"), "vat_number"},
		{"a VAT number typed with spaces and dots", withVAT(ok, "mt 1234.5678"), ""},
		{"a business type outside the schema's list", withType(ok, "nightclub"), "business_type"},
		{"no business type", withType(ok, ""), "business_type"},
		{"a structure outside the schema's list", withStructure(ok, "franchise"), "structure"},
		{"no structure", withStructure(ok, ""), "structure"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, errs := ValidateBusiness(tc.in)
			if tc.field == "" {
				if errs.Any() {
					t.Fatalf("expected no error, got %v", map[string]string(errs))
				}
				return
			}
			if _, ok := errs[tc.field]; !ok {
				t.Fatalf("expected an error on %q, got %v", tc.field, map[string]string(errs))
			}
		})
	}
}

// TestValidateBusiness_NormalisesWhatItChecks — the ONE-REPRESENTATION rule.
//
// The value that comes back is the value that will be stored, compared against the
// UNIQUE constraint and sent to VIES. A function that checked one form and returned
// another is how a "validated" field ends up holding something else.
func TestValidateBusiness_NormalisesWhatItChecks(t *testing.T) {
	t.Parallel()
	b, errs := ValidateBusiness(Business{
		Name:         "  Kebab   Factory   Ltd  ",
		VATNumber:    " mt-1234.5678 ",
		BusinessType: "restaurant",
		Structure:    "multi",
	})
	if errs.Any() {
		t.Fatalf("unexpected errors: %v", map[string]string(errs))
	}
	if b.Name != "Kebab Factory Ltd" {
		t.Errorf("name came back %q; interior runs of whitespace must collapse or the same "+
			"business is two businesses on every screen", b.Name)
	}
	if b.VATNumber != "MT12345678" {
		t.Errorf("VAT number came back %q, want MT12345678", b.VATNumber)
	}
}

func TestValidateVenues_StructureDecidesTheShape(t *testing.T) {
	t.Parallel()
	many := make([]Venue, 0, MaxVenues+1)
	for i := 0; i <= MaxVenues; i++ {
		many = append(many, Venue{Name: "Door " + string(rune('A'+i%26)) + string(rune('a'+i/26))})
	}
	tests := []struct {
		name      string
		structure string
		in        []Venue
		wantErr   bool
		wantCount int
	}{
		{"single, one venue", StructureSingle, []Venue{{Name: "Front door"}}, false, 1},
		{"single, two venues", StructureSingle, []Venue{{Name: "A"}, {Name: "B"}}, true, 2},
		{"single, none", StructureSingle, nil, true, 0},
		{"multi, one venue is legitimate", StructureMulti, []Venue{{Name: "St Julians"}}, false, 1},
		{"multi, three venues", StructureMulti, []Venue{{Name: "A"}, {Name: "B"}, {Name: "C"}}, false, 3},
		{"multi, none", StructureMulti, nil, true, 0},
		{"blank rows are dropped, not refused", StructureMulti,
			[]Venue{{Name: "A"}, {Name: "   "}, {Name: "B"}}, false, 2},
		{"past the ceiling", StructureMulti, many, true, MaxVenues + 1},
		{"two venues with the same name", StructureMulti,
			[]Venue{{Name: "Front door"}, {Name: "front DOOR"}}, true, 2},
		{"a venue name past the bound", StructureMulti,
			[]Venue{{Name: strings.Repeat("x", MaxVenueNameRunes+1)}}, true, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, errs := ValidateVenues(tc.structure, tc.in)
			if errs.Any() != tc.wantErr {
				t.Fatalf("errors=%v, want error=%v", map[string]string(errs), tc.wantErr)
			}
			if len(out) != tc.wantCount {
				t.Fatalf("kept %d venue(s), want %d", len(out), tc.wantCount)
			}
		})
	}
}

func TestValidateAccount_ServerSide(t *testing.T) {
	t.Parallel()
	ok := Account{FullName: "Maria Borg", Email: "maria@kebabfactory.com.mt", Password: "correct horse battery"}
	tests := []struct {
		name  string
		in    Account
		field string
	}{
		{"a complete account", ok, ""},
		{"no name", withFullName(ok, " "), "full_name"},
		{"a name past the bound", withFullName(ok, strings.Repeat("n", MaxPersonNameRunes+1)), "full_name"},
		{"no email", withEmail(ok, ""), "email"},
		{"an email with no @", withEmail(ok, "maria.example.com"), "email"},
		{"an email with two @", withEmail(ok, "a@b@c.mt"), "email"},
		{"an email with nothing before the @", withEmail(ok, "@example.mt"), "email"},
		{"an email with no dot in the domain", withEmail(ok, "maria@localhost"), "email"},
		{"an email with a space in it", withEmail(ok, "mar ia@example.mt"), "email"},
		// The two the DATABASE refuses. Before this check they reached the driver and
		// came back as a 500 on an unauthenticated path — the shape
		// internal/adminauth measured and closed on the login side.
		{"an email carrying a NUL byte", withEmail(ok, "maria\x00@example.mt"), "email"},
		{"an email that is not valid UTF-8", withEmail(ok, "maria\xff@example.mt"), "email"},
		{"an email past the bound", withEmail(ok, strings.Repeat("a", MaxEmailRunes)+"@example.mt"), "email"},
		{"no password", withPassword(ok, ""), "password"},
		{"a password one rune under the floor", withPassword(ok, strings.Repeat("p", MinPasswordRunes-1)), "password"},
		{"a password at the floor", withPassword(ok, strings.Repeat("p", MinPasswordRunes)), ""},
		// BYTES, not runes: bcrypt's limit is a byte limit and its comparer silently
		// TRUNCATES past it, so a password over the line must be refused where the
		// error is cheap.
		{"a password past bcrypt's byte limit", withPassword(ok, strings.Repeat("q", MaxPasswordBytes+1)), "password"},
		{"a password at bcrypt's byte limit", withPassword(ok, strings.Repeat("q", MaxPasswordBytes)), ""},
		{"a multi-byte password inside the rune floor but past the byte limit",
			withPassword(ok, strings.Repeat("ż", MaxPasswordBytes/2+1)), "password"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, errs := ValidateAccount(tc.in)
			if tc.field == "" {
				if errs.Any() {
					t.Fatalf("expected no error, got %v", map[string]string(errs))
				}
				return
			}
			if _, ok := errs[tc.field]; !ok {
				t.Fatalf("expected an error on %q, got %v", tc.field, map[string]string(errs))
			}
		})
	}
}

// TestValidateAccount_NeverReturnsThePassword — the §4.7 shape of this package.
//
// ValidateAccount hands back a normalised Account, so it is the one function here
// that COULD leak a password by echoing it into something a caller logs. It carries
// the value through unchanged (Provision needs it) and this test is the tripwire on
// the other property that matters: no ERROR MESSAGE contains it.
func TestValidateAccount_NeverReturnsThePassword(t *testing.T) {
	t.Parallel()
	const secret = "hunter2-hunter2-hunter2"
	for _, in := range []Account{
		{FullName: "", Email: "not-an-email", Password: secret},
		{FullName: "Maria", Email: "maria@example.mt", Password: strings.Repeat(secret, 10)},
	} {
		_, errs := ValidateAccount(in)
		for field, msg := range errs {
			if strings.Contains(msg, secret) || strings.Contains(msg, secret[:8]) {
				t.Errorf("the message on %q carries the password", field)
			}
		}
	}
}

// TestBusinessTypes_AreExactlyTheSchemasVocabulary.
//
// 🔴 A CHIP THE DATABASE WOULD REFUSE IS A REGISTRATION THAT FAILS AT THE LAST
// STATEMENT, after the customer has typed everything. Migration 00001's CHECK is the
// authority and this test re-reads it rather than trusting the two lists to have been
// kept in step by hand.
func TestBusinessTypes_AreExactlyTheSchemasVocabulary(t *testing.T) {
	t.Parallel()
	src := migrationSource(t, "00001_create_tenants.sql")
	block := regexp.MustCompile(`(?s)business_type text NOT NULL CHECK \(business_type IN \((.*?)\)\)`).
		FindStringSubmatch(src)
	if block == nil {
		t.Fatal("migration 00001 no longer declares business_type's CHECK in a shape this test " +
			"can read; the chip set is unchecked until this is fixed")
	}
	want := map[string]bool{}
	for _, m := range regexp.MustCompile(`'([a-z]+)'`).FindAllStringSubmatch(block[1], -1) {
		want[m[1]] = true
	}
	if len(want) < 5 {
		t.Fatalf("read %d value(s) out of the CHECK; the schema has more, so this scan is "+
			"not reading it", len(want))
	}
	got := map[string]bool{}
	for _, bt := range BusinessTypes {
		got[bt.Value] = true
		if bt.Label == "" {
			t.Errorf("%q has no label, so the wizard would render an empty chip", bt.Value)
		}
		if !want[bt.Value] {
			t.Errorf("the wizard offers %q, which migration 00001's CHECK refuses", bt.Value)
		}
	}
	for v := range want {
		if !got[v] {
			t.Errorf("the schema allows %q and the wizard does not offer it", v)
		}
	}
	// The same, for the OTHER closed vocabulary.
	if !ValidStructure(StructureSingle) || !ValidStructure(StructureMulti) || ValidStructure("franchise") {
		t.Error("ValidStructure does not match migration 00001's structure CHECK")
	}
	if !strings.Contains(src, "structure     text NOT NULL CHECK (structure IN ('single', 'multi'))") {
		t.Error("migration 00001's structure CHECK changed shape; ValidStructure is now unchecked")
	}
}

// TestDefaultTimezone_MatchesTheSchema. CreateTenant NAMES the timezone column, so
// this value is written rather than defaulted — and a value that disagreed with the
// column DEFAULT would mean a tenant created by the wizard renders its days in a
// different zone from one created by the seed.
func TestDefaultTimezone_MatchesTheSchema(t *testing.T) {
	t.Parallel()
	src := migrationSource(t, "00001_create_tenants.sql")
	if !strings.Contains(src, "timezone      text NOT NULL DEFAULT '"+DefaultTimezone+"'") {
		t.Errorf("signup.DefaultTimezone is %q and migration 00001's column DEFAULT is not",
			DefaultTimezone)
	}
}

// TestPasswordBounds_AreAdminauthsOwn — no second literal.
func TestPasswordBounds_AreAdminauthsOwn(t *testing.T) {
	t.Parallel()
	src := repoFile(t, "internal", "adminauth", "password.go")
	if !strings.Contains(src, "MaxPasswordBytes = 72") {
		t.Fatal("internal/adminauth no longer declares MaxPasswordBytes = 72; the wizard's " +
			"byte bound is derived from it and this test is how that stays true")
	}
	if MaxPasswordBytes != 72 {
		t.Errorf("signup.MaxPasswordBytes is %d, adminauth's is 72", MaxPasswordBytes)
	}
}

// TestVenueNameBound_IsTheVenueEditorsOwn. The wizard writes into the same
// `locations` table the panel's venue editor writes, so a name this flow accepts and
// that screen refuses would be a venue nobody can edit.
func TestVenueNameBound_IsTheVenueEditorsOwn(t *testing.T) {
	t.Parallel()
	src := repoFile(t, "internal", "domain", "tenant", "venue.go")
	if !strings.Contains(src, "const MaxVenueNameRunes = 80") {
		t.Fatal("internal/domain/tenant no longer declares MaxVenueNameRunes = 80")
	}
	if MaxVenueNameRunes != 80 {
		t.Errorf("signup.MaxVenueNameRunes is %d, the venue editor's is 80", MaxVenueNameRunes)
	}
}

// --------------------------------------------------------------- VAT format --

func TestVATFormat(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want bool
	}{
		// Malta — the market.
		{"MT12345678", true},
		{"MT1234567", false},
		{"MT123456789", false},
		{"MT1234567A", false},
		// A sample of the rest of the table, one per shape.
		{"ATU12345678", true},
		{"AT12345678", false},
		{"NL123456789B01", true},
		{"NL123456789B0", false},
		{"IE1234567FA", true},
		{"CY12345678L", true},
		{"XIGD123", true},
		{"EL123456789", true},
		// Not a country this product can address.
		{"GB123456789", false},
		{"ZZ12345678", false},
		{"US12-3456789", false},
		// Shape failures.
		{"", false},
		{"MT", false},
		{"M", false},
		{"MT" + strings.Repeat("1", MaxVATRunes), false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := ValidVATFormat(tc.in); got != tc.want {
				t.Errorf("ValidVATFormat(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
	// ANTI-VACUITY: an empty pattern table would refuse everything and every
	// `want:false` case above would pass.
	if KnownVATCountries() < 20 {
		t.Fatalf("the VAT pattern table holds %d countr(ies); the EU has more, so the "+
			"negative cases above prove nothing", KnownVATCountries())
	}
}

// TestValidVATFormat_DoesNotNormalise. The boundary normalises once and everything
// downstream sees that; a check that silently repaired its input would let a caller
// store one value and check another.
func TestValidVATFormat_DoesNotNormalise(t *testing.T) {
	t.Parallel()
	if ValidVATFormat("mt12345678") {
		t.Error("ValidVATFormat accepted an un-normalised value, so a caller could check one " +
			"representation and store a different one")
	}
	if !ValidVATFormat(NormaliseVAT("mt 1234-5678")) {
		t.Error("NormaliseVAT's output does not satisfy ValidVATFormat")
	}
}

func TestNormaliseVAT(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"MT12345678", "MT12345678"},
		{" mt 1234 5678 ", "MT12345678"},
		{"mt-1234.5678", "MT12345678"},
		{"MT 1234 5678", "MT12345678"}, // pasted out of a PDF
		{"MT/12345678", "MT/12345678"}, // NOT repaired: it fails the format check instead
	} {
		if got := NormaliseVAT(tc.in); got != tc.want {
			t.Errorf("NormaliseVAT(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ------------------------------------------------------------------ helpers --

func withName(b Business, v string) Business      { b.Name = v; return b }
func withVAT(b Business, v string) Business       { b.VATNumber = v; return b }
func withType(b Business, v string) Business      { b.BusinessType = v; return b }
func withStructure(b Business, v string) Business { b.Structure = v; return b }

func withFullName(a Account, v string) Account { a.FullName = v; return a }
func withEmail(a Account, v string) Account    { a.Email = v; return a }
func withPassword(a Account, v string) Account { a.Password = v; return a }

// repoFile reads a file relative to the repository root.
func repoFile(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(append([]string{"..", "..", ".."}, parts...)...)
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("reading %s: %v", p, err)
	}
	return string(b)
}

func migrationSource(t *testing.T, name string) string {
	t.Helper()
	return repoFile(t, "db", "migrations", name)
}

// TestValidate_RefusesTheBytesPostgresRefuses — E2.
//
// 🔴 THE DEFECT THIS PINS SHIPPED, AND IT IS THE ONE internal/adminauth ALREADY CLOSED
// ON THE OTHER SIDE OF THE SAME PRODUCT. A NUL byte or invalid UTF-8 in any stored
// text field passed validation, reached the driver, and came back as
// SQLSTATE 22021 — which the boundary turned into HTTP 500 on an UNAUTHENTICATED
// WRITE path, while charging the attempt budget. adminauth's isLookupableEmail closed
// exactly this for the login's email and manager.go records why closing beats
// documenting: the surface stops answering 500 to strangers, the refusal joins the
// uniform one, and the request stops being free.
//
// EVERY STORED TEXT FIELD IS DRIVEN, not just the one that was reported. The venue
// name is where an audit found it; the company name and the operator's name reach the
// same driver through the same transaction.
func TestValidate_RefusesTheBytesPostgresRefuses(t *testing.T) {
	t.Parallel()
	const nul = "Front\x00door"
	badUTF8 := "Front" + string([]byte{0xff}) + "door"

	for _, bad := range []string{nul, badUTF8} {
		t.Run("company name", func(t *testing.T) {
			_, errs := ValidateBusiness(Business{
				Name: bad, VATNumber: "MT12345678", BusinessType: "bar", Structure: "single",
			})
			if _, ok := errs["name"]; !ok {
				t.Errorf("a company name carrying an unstorable byte validated: %v",
					map[string]string(errs))
			}
		})
		t.Run("venue name", func(t *testing.T) {
			_, errs := ValidateVenues(StructureSingle, []Venue{{Name: bad}})
			if !errs.Any() {
				t.Error("a venue name carrying an unstorable byte validated, so Provision " +
					"would fail with 22021 and the boundary would answer 500")
			}
		})
		t.Run("operator name", func(t *testing.T) {
			_, errs := ValidateAccount(Account{
				FullName: bad, Email: "m@example.mt", Password: "a passphrase here",
			})
			if _, ok := errs["full_name"]; !ok {
				t.Errorf("an operator name carrying an unstorable byte validated: %v",
					map[string]string(errs))
			}
		})
	}

	// AND THE CONTROL: text that is merely unusual must still be accepted. The check
	// is about what the DATABASE refuses, not about what a name should look like.
	for _, good := range []string{"Ħaż-Żebbuġ Catering", "Café Λόγος", "Bar 東京", "O'Brien & Sons"} {
		if _, errs := ValidateBusiness(Business{
			Name: good, VATNumber: "MT12345678", BusinessType: "bar", Structure: "single",
		}); errs.Any() {
			t.Errorf("%q was refused: %v", good, map[string]string(errs))
		}
	}
}
