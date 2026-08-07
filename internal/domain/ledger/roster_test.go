package ledger

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/store"
)

// TestPerson_TakesTheStampThatMATCHESTheStatus.
//
// 🔴 THE ROW CARRIES ALL THREE STAMPS AND ONLY ONE OF THEM IS THE ANSWER. A person
// who was invited, activated and later deactivated has three non-NULL timestamps,
// and a mapper that reached for "whichever is set" or "the most recent" would print
// a date that contradicts the word beside it -- "INVITED, since 4 August" for
// somebody deactivated on the 4th and invited in January.
func TestPerson_TakesTheStampThatMATCHESTheStatus(t *testing.T) {
	invited := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	activated := time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)
	deactivated := time.Date(2026, 8, 4, 9, 0, 0, 0, time.UTC)

	// EVERY ROW CARRIES EVERY STAMP, which is what makes this a test rather than a
	// tautology: if only the matching one were set, "return whichever is set" would
	// pass too.
	full := store.ListPanelEmployeesRow{
		InvitedAt: &invited, ActivatedAt: &activated, DeactivatedAt: &deactivated,
	}

	tests := []struct {
		status string
		want   *time.Time
	}{
		{StatusInvited, &invited},
		{StatusActive, &activated},
		{StatusDeactivated, &deactivated},
		// NOT A STATUS THE SCHEMA ADMITS. Unreachable from the database (00003's
		// CHECK), and the answer is "no date" rather than a fallback: a state this
		// code does not understand has no date it can honestly print.
		{"suspended", nil},
	}
	for _, tc := range tests {
		t.Run(tc.status, func(t *testing.T) {
			row := full
			row.Status = tc.status
			got := person(row).Since
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("status %q took the %v stamp; an unknown state has no date", tc.status, *got)
			case tc.want != nil && got == nil:
				t.Errorf("status %q took no stamp; want %v", tc.status, *tc.want)
			case tc.want != nil && got != nil && !got.Equal(*tc.want):
				t.Errorf("status %q took %v, want %v", tc.status, *got, *tc.want)
			}
		})
	}
}

// personFields is the CLOSED SET of what a roster row may carry, with the reason
// each one is safe to render.
//
// 🔴 IT IS §4.7's TYPE WALL, and it is closed in both directions on purpose. A field
// added to Person is a field a screen can print, so adding one has to be an edit HERE
// too, carrying the argument for why it is safe.
//
// 🔴 AND IT IS THE ONLY THING COVERING A WHOLE CLASS OF LEAK — stated here because
// the obvious reading is that the page scan covers it and the obvious reading is
// wrong. internal/handler/employees_db_test.go puts a real token_hash and a real
// device label into Postgres and asserts neither STRING appears in the response.
// That catches a secret rendered WHOLE. It does not catch a secret rendered
// TRANSFORMED, and the gap was measured rather than imagined: a mutation adding
// `left(s.token_hash, 8)` to the query and printing it beside the device count
// leaked eight characters of a live credential, and no substring search for the
// full hash could ever have found it. What went red was THIS test — because the
// mutation had to put a field on Person to carry the value, and the set below is
// CLOSED, so it refuses a new field whatever it is called and whatever shape the
// value has. A prefix, a re-hash, a base64 or a first-and-last-four would all land
// here. That is the cover; the page scan is not.
var personFields = map[string]string{
	"ID":             "an opaque row id; phase B needs it to address a person",
	"Name":           "what the card is for",
	"Status":         "the lifecycle chip; migration 00003's CHECK vocabulary",
	"LocationName":   "where they work; a name, not an id",
	"DepartmentName": "same, and empty for a business with no departments",
	"LiveDevices":    "a COUNT of sessions -- never a session, an id or a label",
	"LastUsed":       "when the newest live device was last used; no address, no place",
	"Since":          "the lifecycle stamp that matches Status",
}

// TestPerson_CarriesNothingItShouldNotAndMapsEverythingItDoes.
//
// TWO PROPERTIES IN ONE PASS, and neither is a list of expected values:
//
//  1. the field SET of Person is exactly personFields -- so a token, a device label,
//     an email or an invitation code cannot be added without an argument;
//  2. every one of those fields is FILLED by the mapper from a row whose every
//     column is non-zero -- so a column that stops being mapped renders blank and
//     turns this red, which is the failure mapper_test.go exists to catch on the
//     other two mappers.
func TestPerson_CarriesNothingItShouldNotAndMapsEverythingItDoes(t *testing.T) {
	var row store.ListPanelEmployeesRow
	fillStruct(t, &row, 20260805)
	// fillStruct writes a random string into Status, and lifecycleStamp answers nil
	// for a status the schema does not admit -- which would leave Since empty and
	// make property (2) fail for the wrong reason. A real status is what the
	// database would supply.
	row.Status = StatusActive

	p := person(row)
	rv := reflect.ValueOf(p)
	rt := rv.Type()

	seen := map[string]bool{}
	for i := 0; i < rt.NumField(); i++ {
		name := rt.Field(i).Name
		seen[name] = true
		if _, ok := personFields[name]; !ok {
			t.Errorf("ledger.Person carries a field this test does not know about: %q.\n"+
				"§4.7: a field on a roster row is a field a template can render. The "+
				"things it must never carry are a session token, a device label, an "+
				"email address and an invitation code. If %q is none of those, add it to "+
				"personFields WITH the reason.", name, name)
			continue
		}
		if rv.Field(i).IsZero() {
			t.Errorf("person() left %q at its zero value while every column of the row "+
				"was set. Either the mapper stopped reading a column or the query "+
				"stopped selecting one; on screen that is a blank cell rather than an "+
				"error.", name)
		}
	}
	for name := range personFields {
		if !seen[name] {
			t.Errorf("personFields names %q, which ledger.Person no longer has", name)
		}
	}
}

// TestRosterFilter_ZeroValueNarrowsNothing is §4.6 at the type level: the default
// roster is EVERYBODY, including the people who have left.
func TestRosterFilter_ZeroValueNarrowsNothing(t *testing.T) {
	var f RosterFilter
	if f.Narrowed() {
		t.Error("the zero RosterFilter reports itself as narrowed")
	}
	if f.Status != "" {
		t.Errorf("the zero RosterFilter carries status %q; an empty status is what the "+
			"query reads as 'every status', and a default of 'active' would hide "+
			"everybody who has left", f.Status)
	}
	// AND EACH FIELD ON ITS OWN IS ENOUGH TO NARROW, so the empty state can never
	// claim "nobody works here" while a filter is applied.
	id := uuid.New()
	for name, narrowed := range map[string]RosterFilter{
		"name":       {Name: "Borg"},
		"status":     {Status: StatusDeactivated},
		"location":   {LocationID: &id},
		"department": {DepartmentID: &id},
	} {
		if !narrowed.Narrowed() {
			t.Errorf("a filter with only %s set does not report itself as narrowed", name)
		}
	}
	// A CURSOR IS NOT A FILTER: it is a POSITION, and folding it in here would make an
	// empty page two report filters the reader never set.
	//
	// ⚠️ WHAT THIS DOES NOT MEAN, since the sentence that used to be here got it
	// wrong: an empty paged view does NOT fall through to "nobody on the books". That
	// would be a claim about the business made from a claim about the position, and it
	// was a real rendered sentence until 2026-08-07. The empty state has a THIRD
	// branch for it (pages.emptyRosterHeading, gated on EmployeesView.Paged), which is
	// why Narrowed() can stay honest about what it means without the screen having to
	// lie. TestPanelEmployeesDB_ACursorPastTheEndDoesNotClaimTheBusinessIsEmpty holds
	// the other end.
	if (RosterFilter{AfterID: &id}).Narrowed() {
		t.Error("a paging cursor counts as a filter; it is a position, not a narrowing")
	}
}

// TestEmployeeStatuses_IsTheSchemaVocabulary. The list drives the panel's dropdown
// and its validator, so a value that is not in migration 00003's CHECK would be
// offered, accepted, and match nothing — and a value the CHECK admits but this list
// omits is a lifecycle state the panel can neither show nor filter, which for
// `deactivated` would be a §4.6 breach.
//
// 🔴 IT READS THE MIGRATION, AND IT DID NOT UNTIL AN AUDIT MEASURED THAT. The first
// version compared EmployeeStatuses against a hand-written literal while its own
// failure message said "the schema's CHECK admits %d" — so a fourth state added to
// 00003 would have left it green while claiming to have consulted the schema. Two
// copies of one fact, and the one that was checked was not the authority.
func TestEmployeeStatuses_IsTheSchemaVocabulary(t *testing.T) {
	want := employeeStatusCheck(t)
	if len(EmployeeStatuses) != len(want) {
		t.Fatalf("EmployeeStatuses holds %d value(s) %v, migration 00003's CHECK admits "+
			"%d %v", len(EmployeeStatuses), EmployeeStatuses, len(want), want)
	}
	// SET EQUALITY, NOT ORDER. The Go slice is ordered by LIFECYCLE on purpose (see
	// EmployeeStatuses) and the CHECK is ordered by whatever the migration author
	// typed; requiring them to agree on order would be a change detector.
	got := map[string]bool{}
	for _, s := range EmployeeStatuses {
		got[s] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("migration 00003's CHECK admits %q and EmployeeStatuses does not "+
				"carry it. The panel's dropdown and its validator both read that slice, "+
				"so a lifecycle state missing from it can be neither shown nor filtered.",
				w)
		}
		delete(got, w)
	}
	for extra := range got {
		t.Errorf("EmployeeStatuses carries %q, which migration 00003's CHECK does not "+
			"admit. The filter would offer it, the validator would accept it, and the "+
			"query would match nothing.", extra)
	}
}

// employeeStatusCheck reads the status vocabulary out of the migration that defines
// it.
//
// It parses the CHECK rather than the live database on purpose: this is a unit test
// with no DATABASE_URL, and the migration is the committed authority — a schema that
// drifted from it is a different failure, caught by the migration tests.
func employeeStatusCheck(t *testing.T) []string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "db", "migrations",
		"00003_create_employees_sessions.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	m := regexp.MustCompile(`(?is)CHECK\s*\(\s*status\s+IN\s*\(([^)]*)\)`).FindStringSubmatch(string(raw))
	if m == nil {
		t.Fatalf("no `CHECK (status IN (...))` found in %s; this test would otherwise "+
			"compare against nothing", path)
	}
	var out []string
	for _, v := range regexp.MustCompile(`'([^']+)'`).FindAllStringSubmatch(m[1], -1) {
		out = append(out, v[1])
	}
	if len(out) < 2 {
		t.Fatalf("parsed %d value(s) from the CHECK (%q); the vocabulary has more, so "+
			"the parse is wrong and every comparison below would be vacuous", len(out), m[1])
	}
	return out
}
