package tenant

// staffadd_db_test.go -- M6-13's CREATE path against REAL Postgres.
//
// 🔴 A FAKE CANNOT TEST ANY OF THIS, and the list of things it would agree with
// unconditionally is longer here than anywhere else in this package:
//
//	the schema DEFAULT       Add never writes `status`, so the only thing that makes
//	                         a new person 'invited' is the column's DEFAULT. A fake
//	                         returns whatever it was told to.
//	the COMPOSITE keys       employees_location_fk is (location_id, tenant_id). Only
//	                         a real database refuses a cross-tenant placement, and
//	                         §4.5 asks for that to be PROVEN with a real foreign id.
//	the PARTIAL unique index  UNIQUE (tenant_id, email) WHERE email IS NOT NULL is the
//	                         reason "" and NULL are different values here. No mock has
//	                         a partial index.
//	citext                   case folding is the COLUMN's behaviour, not ours.
//
// FIXTURES ARE NOT CLEANED UP: tappa_app has REVOKE DELETE on employees (00003) and
// audit_log is append-only (00005). Fresh random ids keep runs from colliding.

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// addCmd is a valid command with one venue and no optional fields, which every test
// below varies from.
func (f *staffFixture) addCmd(name string) AddCommand {
	return AddCommand{
		TenantID:   f.tenantID,
		ActorID:    f.actorID,
		FullName:   name,
		LocationID: f.locationID,
	}
}

// stampsOf reads the three lifecycle stamps plus the status, in the tenant's own
// context so RLS scopes the read exactly as production's does.
func (f *staffFixture) stampsOf(t *testing.T, employeeID uuid.UUID) (status string, invited, activated, deactivated *string) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT status, invited_at::text, activated_at::text, deactivated_at::text
			   FROM employees WHERE tenant_id = $1 AND id = $2`,
			f.tenantID, employeeID).Scan(&status, &invited, &activated, &deactivated)
	})
	if err != nil {
		t.Fatalf("read stamps: %v", err)
	}
	return status, invited, activated, deactivated
}

// emailOf reads the stored address, or nil.
func (f *staffFixture) emailOf(t *testing.T, tenantID, employeeID uuid.UUID) *string {
	t.Helper()
	var email *string
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT email::text FROM employees WHERE tenant_id = $1 AND id = $2`,
			tenantID, employeeID).Scan(&email)
	})
	if err != nil {
		t.Fatalf("read email: %v", err)
	}
	return email
}

// countEmployees counts a tenant's roster, so a test can assert that a REFUSAL wrote
// nothing rather than merely that it returned an error.
func (f *staffFixture) countEmployees(t *testing.T, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM employees WHERE tenant_id = $1`, tenantID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count employees: %v", err)
	}
	return n
}

// 🔴 THE CARD'S FIRST CRITERION, AND THE TRAP UNDER IT. A new employee must be born
// 'invited' -- being born 'active' would skip activation and break §5's session and
// practice-tap chain. Nothing in Add names the column: this asserts the SCHEMA's
// default is what produces the value, and that no lifecycle stamp is invented.
func TestStaffDB_AddCreatesAnInvitedPersonWithNoStamps(t *testing.T) {
	f := newStaffFixture(t)

	person, err := f.staff.Add(context.Background(), f.addCmd("Joseph Camilleri"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if person.Status != statusInvited {
		t.Fatalf("returned status = %q, want %q", person.Status, statusInvited)
	}

	status, invited, activated, deactivated := f.stampsOf(t, person.ID)
	if status != statusInvited {
		t.Fatalf("stored status = %q, want %q -- a new employee must not skip activation", status, statusInvited)
	}
	// 🔴 invited_at STAYS NULL. It stamps "an invitation was sent" and adding somebody
	// sends nothing; filling it would put a date on screen for an invitation that does
	// not exist. No statement in db/queries writes this column at all.
	if invited != nil {
		t.Fatalf("invited_at = %v, want NULL -- adding somebody does not send an invitation", *invited)
	}
	if activated != nil {
		t.Fatalf("activated_at = %v, want NULL -- nobody has activated", *activated)
	}
	if deactivated != nil {
		t.Fatalf("deactivated_at = %v, want NULL", *deactivated)
	}
}

// 🔴 THE CARD'S HEADLINE CRITERION: THE ADD -> INVITE CHAIN CLOSES. Somebody added
// must be invitable IMMEDIATELY, with no intermediate state to reach first. This
// asserts the domain half against the SAME predicate the consuming statement uses;
// the HTTP half (that /admin/employees/invite does not answer `not-invitable`) is
// asserted end to end in internal/handler.
func TestStaffDB_AddedPeopleAreImmediatelyInvitable(t *testing.T) {
	f := newStaffFixture(t)

	person, err := f.staff.Add(context.Background(), f.addCmd("Rita Vella"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if !person.Invitable() {
		t.Fatalf("a person just added is not invitable (status %q) -- the add->invite chain is broken", person.Status)
	}
	// Read them back the way the panel does, rather than trusting the returned value:
	// the button is drawn from THIS read.
	got, err := f.staff.Person(context.Background(), f.tenantID, person.ID)
	if err != nil {
		t.Fatalf("Person: %v", err)
	}
	if !got.Invitable() {
		t.Fatalf("re-read person is not invitable (status %q)", got.Status)
	}
}

// 🔴 §4.5 PROVEN WITH A REAL CROSS-TENANT ID RATHER THAN ASSERTED. The venue belongs
// to another business; the statement's predicate refuses it AND the composite foreign
// key would refuse it even if the predicate were deleted. Both halves are checked:
// the refusal is a SENTENCE (not a 500), and NOTHING was written in either tenant.
func TestStaffDB_AddRefusesAnotherTenantsVenue(t *testing.T) {
	f := newStaffFixture(t)
	before := f.countEmployees(t, f.tenantID)
	beforeForeign := f.countEmployees(t, f.foreignTenant)

	cmd := f.addCmd("Intruder")
	cmd.LocationID = f.foreignVenue // a real venue, of a real OTHER tenant

	_, err := f.staff.Add(context.Background(), cmd)
	if !errors.Is(err, ErrUnknownPlacement) {
		t.Fatalf("Add with a foreign venue = %v, want ErrUnknownPlacement", err)
	}
	if got := f.countEmployees(t, f.tenantID); got != before {
		t.Fatalf("this tenant's roster grew from %d to %d on a refused add", before, got)
	}
	if got := f.countEmployees(t, f.foreignTenant); got != beforeForeign {
		t.Fatalf("🔴 the OTHER tenant's roster grew from %d to %d -- a cross-tenant write",
			beforeForeign, got)
	}
}

// 🔴 THE COMPOSITE KEY IS THE SECOND DEFENCE AND IT IS PROVEN SEPARATELY, because the
// statement's own predicate would hide it. This bypasses the predicate entirely and
// asks the DATABASE directly: with the tenant forged to match, does the key still
// refuse a venue that belongs to somebody else? §4.5's braces, on their own.
func TestStaffDB_TheCompositeKeyRefusesAForeignVenueOnItsOwn(t *testing.T) {
	f := newStaffFixture(t)

	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			// No SELECT, no join, no predicate -- the placement is ASSIGNED, which is
			// exactly what CreateEmployee refuses to do. The only thing left to refuse
			// it is employees_location_fk.
			`INSERT INTO employees (tenant_id, location_id, full_name)
			 VALUES ($1, $2, 'forged')`,
			f.tenantID, f.foreignVenue)
		return e
	})
	if err == nil {
		t.Fatal("🔴 the database ACCEPTED an employee whose venue belongs to another tenant -- " +
			"employees_location_fk is not doing its job")
	}
	if !isForeignKeyViolation(err) {
		t.Fatalf("refused, but not by the foreign key: %v", err)
	}
}

// 🔴 THE PARTIAL UNIQUE INDEX, IN THE DIRECTION THAT IS LEGAL. `UNIQUE (tenant_id,
// email) WHERE email IS NOT NULL` skips rows with no address, so a business may have
// any number of people without one -- which is the ordinary case for shift workers.
//
// THIS IS WHAT PINS THE ""-TO-NULL FOLD. Measured directly against this schema: two
// rows with email ” collide on that index, two with NULL do not. If optionalEmail
// ever stops folding, the SECOND person added without an address is refused as a
// duplicate of the first, naming an address neither of them has.
func TestStaffDB_TwoPeopleWithNoEmailAreLegal(t *testing.T) {
	f := newStaffFixture(t)

	for _, name := range []string{"No Address One", "No Address Two"} {
		cmd := f.addCmd(name)
		cmd.Email = "" // the empty form field, exactly as a browser posts it
		person, err := f.staff.Add(context.Background(), cmd)
		if err != nil {
			t.Fatalf("Add(%q) with no email: %v -- two people without an address must be legal", name, err)
		}
		if got := f.emailOf(t, f.tenantID, person.ID); got != nil {
			t.Fatalf("stored email = %q, want NULL -- an empty field must not reach the index as ''", *got)
		}
	}

	// The whitespace-only address is the same case wearing a different hat, and it
	// reaches the boundary from any manager who tabs through the field.
	cmd := f.addCmd("No Address Three")
	cmd.Email = "   "
	person, err := f.staff.Add(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Add with a whitespace-only email: %v", err)
	}
	if got := f.emailOf(t, f.tenantID, person.ID); got != nil {
		t.Fatalf("stored email = %q, want NULL", *got)
	}
}

// 🔴 THE OTHER DIRECTION: A DUPLICATE ADDRESS IS A SENTENCE, NOT A 500 -- AND CASE
// MAKES NO DIFFERENCE, because the column is citext. The second half is the one that
// surprises managers, and it is why the screen says so.
func TestStaffDB_ADuplicateEmailIsRefusedEvenInADifferentCase(t *testing.T) {
	f := newStaffFixture(t)

	first := f.addCmd("First Holder")
	first.Email = "shared.address@example.mt"
	if _, err := f.staff.Add(context.Background(), first); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	before := f.countEmployees(t, f.tenantID)

	for _, variant := range []string{
		"shared.address@example.mt", // the same address
		"SHARED.ADDRESS@EXAMPLE.MT", // citext: the SAME row
		"Shared.Address@Example.Mt",
	} {
		second := f.addCmd("Second Holder")
		second.Email = variant
		_, err := f.staff.Add(context.Background(), second)
		if !errors.Is(err, ErrEmailTaken) {
			t.Fatalf("Add with duplicate email %q = %v, want ErrEmailTaken (a sentence, never a 500)", variant, err)
		}
	}
	if got := f.countEmployees(t, f.tenantID); got != before {
		t.Fatalf("the roster grew from %d to %d across three refused adds", before, got)
	}
}

// THE UNIQUENESS IS TENANT-SCOPED, and that is a §4.5 property rather than a
// convenience: the same person may work for two businesses on this platform, and this
// must never answer "that address is taken" about somebody else's payroll -- which
// would be an existence oracle over another tenant's data.
func TestStaffDB_TheSameEmailInAnotherBusinessIsFine(t *testing.T) {
	f := newStaffFixture(t)

	mine := f.addCmd("Works Two Jobs")
	mine.Email = "two.jobs@example.mt"
	if _, err := f.staff.Add(context.Background(), mine); err != nil {
		t.Fatalf("Add in my tenant: %v", err)
	}

	theirs := AddCommand{
		TenantID:   f.foreignTenant,
		ActorID:    uuid.New(),
		FullName:   "Works Two Jobs",
		Email:      "two.jobs@example.mt",
		LocationID: f.foreignVenue,
	}
	if _, err := f.staff.Add(context.Background(), theirs); err != nil {
		t.Fatalf("Add of the same address in ANOTHER business = %v, want success -- "+
			"uniqueness is scoped to a tenant", err)
	}
}

// 🔴 A DEPARTMENT MUST BELONG TO THE CHOSEN VENUE. This is §5 correctness rather than
// §4.5: lateness is computed from the department's shift, and a policy can be scoped
// to a department, so a mismatched pair judges somebody against another branch's rules
// from their very first day. The fixture's department sits in otherVenue, so pairing
// it with locationID is the mismatch.
func TestStaffDB_AddRefusesADepartmentOfAnotherVenue(t *testing.T) {
	f := newStaffFixture(t)
	before := f.countEmployees(t, f.tenantID)

	cmd := f.addCmd("Mismatched")
	cmd.LocationID = f.locationID // St Julians
	cmd.DepartmentID = &f.departmentID
	// the fixture's Kitchen belongs to the OTHER venue
	if _, err := f.staff.Add(context.Background(), cmd); !errors.Is(err, ErrUnknownPlacement) {
		t.Fatalf("Add with a department of another venue = %v, want ErrUnknownPlacement", err)
	}
	if got := f.countEmployees(t, f.tenantID); got != before {
		t.Fatalf("roster grew from %d to %d on a refused placement", before, got)
	}

	// THE POSITIVE CONTROL. The same department paired with ITS OWN venue must work,
	// or the test above would pass for the wrong reason -- a refusal that refuses
	// everything proves nothing.
	ok := f.addCmd("Correctly Placed")
	ok.LocationID = f.otherVenue
	ok.DepartmentID = &f.departmentID
	person, err := f.staff.Add(context.Background(), ok)
	if err != nil {
		t.Fatalf("Add with a department of its OWN venue: %v", err)
	}
	loc, dep := f.placementOf(t, person.ID)
	if loc != f.otherVenue || dep == nil || *dep != f.departmentID {
		t.Fatalf("stored placement = (%v, %v), want (%v, %v)", loc, dep, f.otherVenue, f.departmentID)
	}
}

// A DEPARTMENT THAT BELONGS TO ANOTHER TENANT ENTIRELY, which is the §4.5 half of the
// same field. It must be refused as a placement, never silently dropped to "no
// department" -- the LEFT JOIN would do exactly that without the guard in the WHERE.
func TestStaffDB_AddRefusesForeignAndUnknownDepartments(t *testing.T) {
	f := newStaffFixture(t)
	unknown := uuid.New()

	for _, tc := range []struct {
		name string
		dep  uuid.UUID
	}{
		{"an invented department", unknown},
		{"another tenant's department", f.foreignDepartment(t)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := f.addCmd("Wrong Department")
			cmd.DepartmentID = &tc.dep
			_, err := f.staff.Add(context.Background(), cmd)
			if !errors.Is(err, ErrUnknownPlacement) {
				t.Fatalf("Add = %v, want ErrUnknownPlacement -- an unknown department must be "+
					"REFUSED, never quietly stored as 'no department'", err)
			}
		})
	}
}

// foreignDepartment finds the other tenant's department id.
func (f *staffFixture) foreignDepartment(t *testing.T) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := f.data.WithTenant(context.Background(), f.foreignTenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT id FROM departments WHERE tenant_id = $1 LIMIT 1`, f.foreignTenant).Scan(&id)
	})
	if err != nil {
		t.Fatalf("read foreign department: %v", err)
	}
	return id
}

// 🔴 THE HIRE AND ITS TRAIL SHARE ONE TRANSACTION (§4.6). Breaking the trail must
// roll the employee back: a person on the roster with no audit row is an
// unattributable act, and audit_log cannot be corrected afterwards.
func TestStaffDB_AddAndItsTrailShareOneTransaction(t *testing.T) {
	f := newStaffFixture(t)
	before := f.countEmployees(t, f.tenantID)

	broken, err := NewStaff(f.data, brokenTrail{err: errors.New("trail down")}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	if _, err := broken.Add(context.Background(), f.addCmd("Rolled Back")); err == nil {
		t.Fatal("Add with a failing trail returned nil -- the two writes are not one transaction")
	}
	if got := f.countEmployees(t, f.tenantID); got != before {
		t.Fatalf("🔴 the employee SURVIVED a failed audit write: roster went %d -> %d", before, got)
	}

	// The positive control: the same command through a working trail writes BOTH.
	person, err := f.staff.Add(context.Background(), f.addCmd("Rolled Back"))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeAdded, person.ID); n != 1 {
		t.Fatalf("audit rows for the hire = %d, want 1", n)
	}
}

// 🔴 §4.7 -- THE TRAIL RECORDS THE ACT WITHOUT COPYING THE PERSON. audit_log is
// append-only and a GDPR erasure is an UPDATE over `employees` that would never reach
// a jsonb column, so a name or an address written here could never be corrected or
// removed. The booleans answer the investigator's question without storing the answer.
func TestStaffDB_TheAddTrailCarriesNoNameAndNoEmail(t *testing.T) {
	f := newStaffFixture(t)

	cmd := f.addCmd("Grazzja Farrugia")
	cmd.Email = "grazzja.farrugia@example.mt"
	cmd.Role = "Sous chef"
	person, err := f.staff.Add(context.Background(), cmd)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}

	detail, actor := f.auditDetail(t, f.tenantID, ActionEmployeeAdded, person.ID)
	if actor == nil || *actor != f.actorID {
		t.Fatalf("audit actor = %v, want %v -- an administrative act must be attributable", actor, f.actorID)
	}
	for _, secret := range []string{"Grazzja", "Farrugia", "grazzja.farrugia@example.mt", "example.mt"} {
		if strings.Contains(strings.ToLower(detail), strings.ToLower(secret)) {
			t.Fatalf("🔴 the audit detail contains %q: %s", secret, detail)
		}
	}
	// What it DOES carry: the placement, the two booleans and the status it was born
	// with. The last is the point of the row -- evidence that the statement did not
	// start naming a column it must not.
	if !containsJSONField(detail, "status", statusInvited) {
		t.Fatalf(`audit detail does not record status=%q: %s`, statusInvited, detail)
	}
	// ⚠️ THE BOOLEAN FORMS ARE BOTH ACCEPTED BECAUSE POSTGRES CHOOSES THE SPACING.
	// jsonb re-renders what it stored, and it prints `"has_email": true` with a space
	// -- the first version of this assertion looked only for the compact form and went
	// red against a perfectly correct row. The defect was in the CHECK, not the code,
	// which is the shape this repository has been caught by repeatedly.
	if !containsJSONBool(detail, "has_email", true) {
		t.Fatalf("audit detail does not record that there was an address: %s", detail)
	}
	if !containsJSONBool(detail, "has_role", true) {
		t.Fatalf("audit detail does not record that there was a role: %s", detail)
	}
}

// 🔴 DEGENERATE INPUT, COUNTED AND RUN RATHER THAN REASONED ABOUT. "A healthy system
// is EMPTY, a broken one is INCOMPLETE" is this repository's most expensive lesson,
// and every row below is a value a real form can produce: an empty box, a tabbed-through
// box, a paste from another application, a name in a script the author does not read.
//
// THE ACCEPTED ROWS MATTER AS MUCH AS THE REFUSED ONES. A validator that refuses
// everything passes every refusal test and breaks the product, so the Maltese, Arabic
// and emoji names are POSITIVE controls: this product ships to Malta, and a bound that
// refused ħ or ż would be a defect, not safety.
func TestStaffDB_AddDegenerateInput(t *testing.T) {
	f := newStaffFixture(t)

	tests := []struct {
		name    string
		mutate  func(*AddCommand)
		wantErr error
		// stored is what full_name must become when the command is ACCEPTED.
		stored string
	}{
		{"empty name", func(c *AddCommand) { c.FullName = "" }, ErrEmployeeName, ""},
		{"whitespace-only name", func(c *AddCommand) { c.FullName = "   \t\n  " }, ErrEmployeeName, ""},
		{"NUL-only name", func(c *AddCommand) { c.FullName = "\x00\x00" }, ErrEmployeeName, ""},
		{"invalid UTF-8 only", func(c *AddCommand) { c.FullName = "\xff\xfe" }, ErrEmployeeName, ""},
		{
			"name one rune over the bound",
			func(c *AddCommand) { c.FullName = strings.Repeat("a", MaxEmployeeNameRunes+1) },
			ErrEmployeeName, "",
		},
		{
			"name exactly at the bound is ACCEPTED",
			func(c *AddCommand) { c.FullName = strings.Repeat("a", MaxEmployeeNameRunes) },
			nil, strings.Repeat("a", MaxEmployeeNameRunes),
		},
		{
			// 🔴 THE BOUND IS RUNES, NOT BYTES. Each ħ is two bytes, so a byte bound
			// would refuse this at half the stated length -- and Maltese names are the
			// reason the brand ships a latin-ext subset at all.
			"Maltese name at the bound in RUNES is ACCEPTED",
			func(c *AddCommand) { c.FullName = strings.Repeat("ħ", MaxEmployeeNameRunes) },
			nil, strings.Repeat("ħ", MaxEmployeeNameRunes),
		},
		{"Maltese name", func(c *AddCommand) { c.FullName = "Ċikku Żammit Ħaġa" }, nil, "Ċikku Żammit Ħaġa"},
		{"right-to-left name", func(c *AddCommand) { c.FullName = "محمد الأمين" }, nil, "محمد الأمين"},
		{"emoji in a name", func(c *AddCommand) { c.FullName = "Chef 🌯 Borg" }, nil, "Chef 🌯 Borg"},
		{
			"interior whitespace is COLLAPSED, not refused",
			func(c *AddCommand) { c.FullName = "  Maria    Borg \t " }, nil, "Maria Borg",
		},
		{
			"NUL inside a name is stripped, the rest stored",
			func(c *AddCommand) { c.FullName = "Ma\x00ria Borg" }, nil, "Maria Borg",
		},
		{"zero venue", func(c *AddCommand) { c.LocationID = uuid.Nil }, ErrUnknownPlacement, ""},
		{"unknown venue", func(c *AddCommand) { c.LocationID = uuid.New() }, ErrUnknownPlacement, ""},
		{
			"zero department is not 'no department'",
			func(c *AddCommand) { z := uuid.Nil; c.DepartmentID = &z }, ErrUnknownPlacement, "",
		},
		{"no actor", func(c *AddCommand) { c.ActorID = uuid.Nil }, nil /* checked below */, ""},
		{"email with no @", func(c *AddCommand) { c.Email = "not-an-address" }, ErrEmployeeEmail, ""},
		{"email with two @", func(c *AddCommand) { c.Email = "a@b@c.mt" }, ErrEmployeeEmail, ""},
		{"email with nothing before @", func(c *AddCommand) { c.Email = "@example.mt" }, ErrEmployeeEmail, ""},
		{"email with nothing after @", func(c *AddCommand) { c.Email = "someone@" }, ErrEmployeeEmail, ""},
		{"email with a space", func(c *AddCommand) { c.Email = "some one@example.mt" }, ErrEmployeeEmail, ""},
		{
			"email over the bound",
			func(c *AddCommand) { c.Email = strings.Repeat("a", MaxEmployeeEmailRunes) + "@example.mt" },
			ErrEmployeeEmail, "",
		},
		{
			"role over the bound",
			func(c *AddCommand) { c.Role = strings.Repeat("r", MaxEmployeeRoleRunes+1) },
			ErrEmployeeRole, "",
		},
		{
			"role exactly at the bound is ACCEPTED",
			func(c *AddCommand) { c.Role = strings.Repeat("r", MaxEmployeeRoleRunes) }, nil, "Valid Name",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := f.addCmd("Valid Name")
			tc.mutate(&cmd)

			before := f.countEmployees(t, f.tenantID)
			person, err := f.staff.Add(context.Background(), cmd)

			if tc.name == "no actor" {
				// An act with no actor writes an unattributable audit row, so it is
				// refused -- but by requireActor rather than by one of the four form
				// sentinels, so it is asserted separately rather than shoehorned in.
				if err == nil {
					t.Fatal("Add with no actor succeeded -- an administrative act must be attributable")
				}
				if got := f.countEmployees(t, f.tenantID); got != before {
					t.Fatalf("roster grew from %d to %d on an actorless add", before, got)
				}
				return
			}

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("Add = %v, want %v", err, tc.wantErr)
				}
				// 🔴 A REFUSAL MUST WRITE NOTHING. Asserting only the error would let a
				// version through that stored the row and then complained about it.
				if got := f.countEmployees(t, f.tenantID); got != before {
					t.Fatalf("🔴 roster grew from %d to %d on a REFUSED add", before, got)
				}
				return
			}

			if err != nil {
				t.Fatalf("Add = %v, want success -- this input is legitimate", err)
			}
			var stored string
			readErr := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx,
					`SELECT full_name FROM employees WHERE tenant_id = $1 AND id = $2`,
					f.tenantID, person.ID).Scan(&stored)
			})
			if readErr != nil {
				t.Fatalf("read name: %v", readErr)
			}
			if stored != tc.stored {
				t.Fatalf("stored name = %q, want %q", stored, tc.stored)
			}
		})
	}
}

// TWO IDENTICAL SUBMISSIONS PRODUCE TWO PEOPLE, and that is CORRECT rather than a
// missing guard. Two real people can share a name -- it is common enough in a small
// country -- and there is no unique index on full_name to say otherwise. The product's
// answer to a double submission is POST-redirect-GET at the HTTP boundary, not a
// silent refusal here that would make hiring two brothers impossible.
//
// It is written down because the opposite behaviour looks like a safety feature and
// would be the wrong one.
func TestStaffDB_TwoPeopleMayShareAName(t *testing.T) {
	f := newStaffFixture(t)

	first, err := f.staff.Add(context.Background(), f.addCmd("John Borg"))
	if err != nil {
		t.Fatalf("first Add: %v", err)
	}
	second, err := f.staff.Add(context.Background(), f.addCmd("John Borg"))
	if err != nil {
		t.Fatalf("second Add: %v -- two people may share a name", err)
	}
	if first.ID == second.ID {
		t.Fatal("the two adds returned the same id")
	}
}

// containsJSONBool is containsJSONField for an unquoted boolean, accepting either
// spacing Postgres may render.
func containsJSONBool(detail, field string, want bool) bool {
	lit := "false"
	if want {
		lit = "true"
	}
	return containsAny(detail,
		`"`+field+`": `+lit,
		`"`+field+`":`+lit,
	)
}

// 🔴 §4.7 PINNED WHERE THE LOGGER ACTUALLY IS. This package's Add writes a process log
// line, and the comment above it claims "NEITHER THE NAME NOR THE ADDRESS IS LOGGED".
// Until round 4 nothing held that claim: an audit added `"full_name", out.Name,
// "email", c.Email` to it and THREE packages stayed green.
//
// 🔴 THE HANDLER'S TEST CANNOT COVER THIS, WHICH IS THE WHOLE REASON THIS ONE EXISTS.
// TestEmployeeAdd_NoOutcomeLogsAnythingTheCallerSent drives the boundary through `fakeStaff`, so
// the request never reaches THIS package and never touches THIS logger. Two loggers,
// two claims, two nets -- the same split employeeactions.go already records for the
// invite link ("NOT LOGGED is two claims and each has its own net").
//
// WHY IT MATTERS MORE HERE THAN FOR MOST LINES: a process log has no retention story
// and no tenant boundary (§7). A person's name and email in one are personal data in
// the one place the product cannot later correct, redact or erase -- which is the same
// argument addDetail makes for refusing to put them in audit_log.
//
// THE SENTINEL VALUES ARE DISTINCTIVE ON PURPOSE. A common name would collide with an
// id, a timestamp or another field's bytes and make this assertion accidentally true.
func TestStaffDB_TheDomainLogsNeitherTheNameNorTheEmail(t *testing.T) {
	f := newStaffFixture(t)

	var logged strings.Builder
	staff, err := NewStaff(f.data, f.trail, slog.New(slog.NewTextHandler(&logged,
		&slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}

	const (
		sentinelName  = "Zzyzx-Sentinel-Name-9f1c"
		sentinelEmail = "zzyzx-sentinel-9f1c@example.mt"
		sentinelRole  = "Zzyzx-Sentinel-Role-9f1c"
	)
	cmd := f.addCmd(sentinelName)
	cmd.Email = sentinelEmail
	cmd.Role = sentinelRole
	if _, err := staff.Add(context.Background(), cmd); err != nil {
		t.Fatalf("Add: %v", err)
	}

	written := logged.String()
	// THE POSITIVE CONTROL FIRST. Without it a logger that wrote NOTHING would satisfy
	// every assertion below, and this test would pass against a silent product.
	if !strings.Contains(written, "employee added") {
		t.Fatalf("the domain logged nothing for a successful add, so the absences below prove "+
			"nothing:\n%s", written)
	}
	for _, secret := range []string{sentinelName, sentinelEmail, sentinelRole} {
		if strings.Contains(written, secret) {
			t.Errorf("🔴 §4.7: the domain log contains %q. A process log has no retention story "+
				"and no tenant boundary; personal data written there cannot be corrected or "+
				"erased.\nline: %s", secret, written)
		}
	}
	// AND THE LINE STILL CARRIES WHAT A READER NEEDS: the booleans, not the values.
	if !strings.Contains(written, "has_email=true") {
		t.Errorf("the log no longer records WHETHER there was an address: %s", written)
	}
}

// 🔴 U-6: THE TRAIL'S BOOLEANS DESCRIBE THIS HIRE, NOT EVERY HIRE. `has_email` and
// `has_role` are the only record of what a new employee arrived with -- audit_log is
// append-only and the address itself is deliberately NOT copied in -- so a constant
// `true` would make an investigator believe every person added had an address on file.
// An audit hard-wired it and the suite stayed green.
//
// BOTH DIRECTIONS ARE DRIVEN, which is the whole net: a constant `true` fails the
// second case and a constant `false` fails the first.
//
// 🔴 U-5, IN THE SAME TEST AND WITH AN HONEST LIMIT ATTACHED. The trail's `status` is
// supposed to be READ BACK from the row rather than assumed, and an audit replaced
// `row.Status` with the constant `statusInvited` to green. That mutation is
// BEHAVIOUR-EQUIVALENT TODAY and it is worth saying so plainly rather than pretending
// otherwise: CreateEmployee does not name `status`, so RETURNING status is always the
// schema default, and no test can distinguish the two while that holds.
//
// What IS pinned here is the invariant that makes them diverge the moment it stops
// holding: the trail's status must equal the status actually stored on the row. So a
// statement that started writing a status while the trail kept assuming one fails HERE
// as well as at TestStaffDB_AddCreatesAnInvitedPersonWithNoStamps -- measured by
// applying both mutations together.
func TestStaffDB_TheTrailDescribesThisHireAndNotAGenericOne(t *testing.T) {
	f := newStaffFixture(t)

	cases := []struct {
		name             string
		email, role      string
		wantEmail, wantR bool
	}{
		{"with an address and a role", "has.both@example.mt", "Chef", true, true},
		{"with neither", "", "", false, false},
		{"address only", "address.only@example.mt", "", true, false},
		{"role only", "", "Waiter", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := f.addCmd("Trail Subject " + tc.name)
			cmd.Email, cmd.Role = tc.email, tc.role
			person, err := f.staff.Add(context.Background(), cmd)
			if err != nil {
				t.Fatalf("Add: %v", err)
			}

			detail, _ := f.auditDetail(t, f.tenantID, ActionEmployeeAdded, person.ID)
			if !containsJSONBool(detail, "has_email", tc.wantEmail) {
				t.Errorf("🔴 has_email is not %v for a hire %s -- the trail is describing a "+
					"generic hire rather than this one: %s", tc.wantEmail, tc.name, detail)
			}
			if !containsJSONBool(detail, "has_role", tc.wantR) {
				t.Errorf("🔴 has_role is not %v for a hire %s: %s", tc.wantR, tc.name, detail)
			}

			// U-5: the trail's status must be the row's status, whatever that becomes.
			status, _, _, _ := f.stampsOf(t, person.ID)
			if !containsJSONField(detail, "status", status) {
				t.Errorf("🔴 the trail records a status that is not the one stored on the row "+
					"(row=%q): %s\nThe audit row is append-only, so a status it assumed rather "+
					"than read can never be corrected.", status, detail)
			}
		})
	}
}
