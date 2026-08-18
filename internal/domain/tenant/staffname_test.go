package tenant

// staffname_test.go -- the pins on what a stored employee name may be (M6-13, round 2).
//
// 🔴 THESE RUN WITHOUT A DATABASE, DELIBERATELY. personName is a pure function and the
// properties below are properties OF THAT FUNCTION, so binding them to a Postgres
// fixture would make them skip silently on a machine with no DATABASE_URL -- which is
// exactly how a guard comes to be unmeasured. The database-side behaviour is pinned
// separately in staffadd_db_test.go.

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

// 🔴 THE BOUND ITSELF IS PINNED, WITH A LITERAL, BECAUSE THE SYMBOL IS NOT A PIN.
// An audit changed MaxEmployeeNameRunes from 120 to 1200 and the ENTIRE SUITE stayed
// green: every existing assertion was written as `MaxEmployeeNameRunes+1`, so the
// tests slid along with the constant and measured nothing about its value. That is
// lesson 383's exact shape -- a constant carrying an argument that nothing pins.
//
// 🔴 AND THIS CONSTANT IS THE ONLY GUARD THERE IS. Measured (2026-08-17): employees
// has NO CHECK constraint on full_name, so a 5 000-character name inserted directly is
// accepted by the database. Nothing below this boundary refuses one.
//
// THE FAILURE MESSAGE NAMES THE CONSEQUENCE RATHER THAN THE VALUE, because "want 120"
// teaches a future reader to update the number and move on.
func TestPersonName_TheBoundIsExactlyOneHundredAndTwentyRunes(t *testing.T) {
	if MaxEmployeeNameRunes != 120 {
		t.Fatalf("MaxEmployeeNameRunes = %d, but this boundary is the ONLY thing that "+
			"bounds employees.full_name -- the column is unbounded `text` with no CHECK. "+
			"Widening it re-opens the defect a security audit measured on this very column: "+
			"a 605-rune name on a roster page boundary served page one forever and made "+
			"everybody behind it unreachable by browsing.", MaxEmployeeNameRunes)
	}

	// THE BEHAVIOUR AT THE LITERAL BOUNDARY, both sides. Written as 120 and 121 rather
	// than as the constant +/- 1, which is the whole point of this test.
	if _, err := personName(strings.Repeat("a", 120)); err != nil {
		t.Errorf("a 120-rune name was refused (%v); 120 must be storable", err)
	}
	if _, err := personName(strings.Repeat("a", 121)); !errors.Is(err, ErrEmployeeName) {
		t.Errorf("a 121-rune name was accepted (err=%v); the bound is not enforced", err)
	}

	// AND IN RUNES, NOT BYTES: 120 Maltese ħ is 240 bytes and must still be accepted.
	name := strings.Repeat("ħ", 120)
	if utf8.RuneCountInString(name) != 120 || len(name) == 120 {
		t.Fatalf("fixture is wrong: %d runes, %d bytes", utf8.RuneCountInString(name), len(name))
	}
	if _, err := personName(name); err != nil {
		t.Errorf("120 Maltese runes (%d bytes) were refused (%v) -- the bound has become a "+
			"byte bound, which refuses ordinary Maltese names at four fifths of its length",
			len(name), err)
	}
}

// 🔴 THE INVISIBLE-NAME INVENTORY. `strings.Fields` splits on unicode.IsSpace, which
// correctly refuses NBSP and tab -- and is blind to the much larger class of runes
// that occupy no visible space and are NOT spaces. An audit measured six of these
// being accepted and stored end to end (a name of three U+200B got a 303 and created a
// row); probing while fixing it found four more.
//
// THE CONSEQUENCE IS SPECIFIC, which is why this is a refusal and not a tidy-up: the
// roster is ORDER BY full_name ASC, so a blank-looking name sorts to the TOP of the
// list, and two of them cannot be told apart by anybody reading the screen.
//
// ⚠️ THE LIST IS MEASURED, NOT DERIVED, and two entries prove why that distinction is
// not pedantic: U+3164 HANGUL FILLER is a LETTER and U+2800 BRAILLE PATTERN BLANK is a
// SYMBOL, so a rule written from Unicode categories alone -- the obvious
// implementation -- accepts both. Unicode does not mark "renders blank" as a property.
func TestPersonName_RefusesNamesThatRenderBlank(t *testing.T) {
	blank := []struct{ name, in string }{
		{"zero width space", "\u200b\u200b\u200b"},
		{"zero width joiner", "\u200d\u200d"},
		{"zero width non-joiner", "\u200c"},
		{"soft hyphen", "\u00ad\u00ad"},
		{"byte order mark", "\ufeff"},
		{"combining marks only", "\u0301\u0302"},
		{"right-to-left override", "\u202e"},
		{"left-to-right isolate", "\u2066"},
		{"hangul filler (a LETTER that renders blank)", "\u3164"},
		{"braille blank (a SYMBOL that renders blank)", "\u2800"},
		{"hangul choseong filler", "\u115f"},
		{"halfwidth hangul filler", "\uffa0"},
		// 🔴 THE UNASSIGNED CLASS, which no enumeration could ever close and which
		// !unicode.IsGraphic closes in one predicate. Both were ACCEPTED AND STORED
		// until round 6, and both render as nothing.
		{"unassigned U+2065", "\u2065"},
		{"unassigned U+FFF0", "\ufff0"},
		{"unassigned U+0590", "\u0590"},
		{"private use U+E000", "\ue000"},
		{"mixed unassigned and format", "\u2065\u200b\ufff0"},
		{"mixed invisibles", "\u200b\u00ad\ufeff\u0301"},
		// The two that were ALREADY refused, kept as controls so this test would notice
		// if the space handling regressed while the new rule was added.
		{"non-breaking space", "\u00a0\u00a0"},
		{"tab and space", "\t "},
	}
	for _, tc := range blank {
		t.Run(tc.name, func(t *testing.T) {
			got, err := personName(tc.in)
			if !errors.Is(err, ErrEmployeeName) {
				t.Fatalf("a name of %s was ACCEPTED and stored as %q -- it renders as nothing, "+
					"so it sorts to the top of the roster and is indistinguishable from any "+
					"other blank name", tc.name, got)
			}
		})
	}
}

// 🔴 BIDI OVERRIDES ARE REFUSED EVEN INSIDE AN OTHERWISE REAL NAME, because what they
// do is not local: U+202E reverses the visual order of everything after it, so one in
// a roster cell rewrites how the rest of the row reads. It is a REFUSAL rather than a
// silent strip, for the reason optionalEmail gives about case: storing something other
// than what the manager typed is its own defect.
func TestPersonName_RefusesBidiOverridesInsideARealName(t *testing.T) {
	for _, in := range []string{
		"\u202eMaria Borg",
		"Maria \u202dBorg",
		"Maria\u2066 Borg",
		"Maria Borg\u202c",
	} {
		if got, err := personName(in); !errors.Is(err, ErrEmployeeName) {
			t.Errorf("a name carrying a bidi control was accepted as %q -- it can reorder how "+
				"the whole roster row reads", got)
		}
	}
}

// 🔴 THE POSITIVE CONTROLS, AND THEY MATTER AS MUCH AS THE REFUSALS. A validator that
// refuses everything passes every test above and breaks the product. This product
// ships to Malta, so Maltese must work; and the two joiners are semantically
// load-bearing in emoji sequences and Indic conjuncts, so a name that CONTAINS one
// alongside visible characters must survive intact.
func TestPersonName_AcceptsRealNamesIncludingTheAwkwardOnes(t *testing.T) {
	tests := []struct{ name, in, want string }{
		{"Maltese", "Ċikku Żammit Ħaġa", "Ċikku Żammit Ħaġa"},
		{"Arabic", "محمد الأمين", "محمد الأمين"},
		{"Chinese", "李小龍", "李小龍"},
		// 🔴 APOSTROPHES, BOTH SPELLINGS. An audit noted the product handles these
		// correctly and nothing held the behaviour. The straight one is ASCII; the
		// curly one is U+2019, which word processors and phone keyboards substitute
		// automatically -- so a manager typing an Irish name on an iPad produces the
		// second, not the first. Both are punctuation, both are visible, both must
		// survive unchanged.
		{"straight apostrophe and hyphen", "O'Brien-Smith", "O'Brien-Smith"},
		{"curly apostrophe U+2019", "O\u2019Brien-Smith", "O\u2019Brien-Smith"},
		{"curly apostrophe alone", "D\u2019Angelo", "D\u2019Angelo"},
		{"emoji in a name", "Chef 🌯 Borg", "Chef 🌯 Borg"},
		{
			"emoji ZWJ sequence survives (the joiner is NOT stripped)",
			"Chef 👨\u200d🍳 Borg", "Chef 👨\u200d🍳 Borg",
		},
		{"Devanagari with ZWJ", "क\u200dष", "क\u200dष"},
		{"interior whitespace collapsed", "  Maria    Borg \t ", "Maria Borg"},
		{"NUL stripped, rest kept", "Ma\x00ria Borg", "Maria Borg"},
		{
			// 🔴 THE ARTEFACT IS REMOVED SO THE TWO NAMES BECOME ONE ROW. A soft hyphen
			// or a BOM arrives from a paste out of a document or a mail client and is
			// invisible; leaving it in would make "Maria Borg" and "Maria Borg" two
			// different employees that look identical on the roster.
			"soft hyphen inside a real name is dropped",
			"Ma\u00adria Borg", "Maria Borg",
		},
		{"zero width space inside a real name is dropped", "Maria\u200bBorg", "MariaBorg"},
		{"BOM at the front is dropped", "\ufeffMaria Borg", "Maria Borg"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := personName(tc.in)
			if err != nil {
				t.Fatalf("personName(%q) = %v, want it accepted -- this is a legitimate name", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("stored %q, want %q", got, tc.want)
			}
		})
	}
}

// 🔴 U-4: isEmailTaken READS THE INDEX NAME, AND THE WHOLE POINT IS THE 23505s IT MUST
// NOT CLAIM. `employees` carries three unique indexes. A bare SQLSTATE test would
// report a PRIMARY KEY collision -- a real fault worth a 500 and an alert -- to a
// manager as "that address is taken", sending them to fix something that is not wrong.
//
// An audit replaced the body with `return true` and the suite stayed green: the
// function's entire justification was unmeasured. This drives it directly, because the
// non-email 23505s cannot be produced through the product (ids are gen_random_uuid()).
func TestIsEmailTaken_OnlyTheEmailIndexCounts(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			"the partial unique index on (tenant_id, email)",
			&pgconn.PgError{Code: "23505", ConstraintName: "employees_tenant_email_key"},
			true,
		},
		{
			// 🔴 THE ONE THAT MATTERS. A duplicate primary key is a fault, not a
			// manager's typo, and must not be dressed up as one.
			"a primary key collision is NOT an address clash",
			&pgconn.PgError{Code: "23505", ConstraintName: "employees_pkey"},
			false,
		},
		{
			"the (id, tenant_id) uniqueness is NOT an address clash",
			&pgconn.PgError{Code: "23505", ConstraintName: "employees_id_tenant_key"},
			false,
		},
		{
			"a unique violation with no constraint name is not assumed to be the email",
			&pgconn.PgError{Code: "23505"},
			false,
		},
		{
			"a foreign key violation is a placement problem, not an address one",
			&pgconn.PgError{Code: "23503", ConstraintName: "employees_location_fk"},
			false,
		},
		{"a plain error is not a unique violation", errors.New("connection reset"), false},
		{"no error at all", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEmailTaken(tc.err); got != tc.want {
				t.Fatalf("isEmailTaken = %v, want %v -- reporting the wrong 23505 as an address "+
					"clash sends a manager to correct an address that is not the problem", got, tc.want)
			}
		})
	}

	// AND IT SURVIVES WRAPPING, because the real call site sees the driver's error
	// through pgx and this package's own fmt.Errorf chains.
	wrapped := fmt.Errorf("create employee: %w",
		&pgconn.PgError{Code: "23505", ConstraintName: "employees_tenant_email_key"})
	if !isEmailTaken(wrapped) {
		t.Error("a wrapped email clash is not recognised, so the refusal would surface as a 500")
	}
}
