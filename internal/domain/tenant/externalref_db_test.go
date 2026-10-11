package tenant

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// externalref_db_test.go -- M11 API-1's panel writes (K-18) and K-24's address rule through the
// domain, against real Postgres (staffFixture / venueFixture, tappa_app in the tenant's own
// context).

func newRef(label string) string {
	return label + "-" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:10])
}

func (f *staffFixture) externalRefOf(t *testing.T, tenantID, employee uuid.UUID) *string {
	t.Helper()
	var ref *string
	if err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT external_ref FROM employees WHERE tenant_id = $1 AND id = $2`, tenantID, employee).Scan(&ref)
	}); err != nil {
		t.Fatalf("read external ref: %v", err)
	}
	return ref
}

// TestExternalRef_TheBoundaryIsTheSchemasPattern: ExternalRef trims, maps "" to "none" and
// accepts exactly migration 00037's pattern -- URL-safe, a letter or digit first, at most 64.
func TestExternalRef_TheBoundaryIsTheSchemasPattern(t *testing.T) {
	for _, c := range []struct {
		in   string
		want string // "" = nil (no ref); "!" = refused
	}{
		{"", ""}, {"   ", ""}, {" EMP-1001 ", "EMP-1001"}, {"a", "a"}, {"0.x_y~z-1", "0.x_y~z-1"},
		{strings.Repeat("x", MaxExternalRefLength), strings.Repeat("x", MaxExternalRefLength)},
		{strings.Repeat("x", MaxExternalRefLength+1), "!"}, {".hidden", "!"}, {"..", "!"}, {"-x", "!"},
		{"a/b", "!"}, {"a b", "!"}, {"çay", "!"}, {"a\x00", "!"}, {"a?b", "!"}, {"a%2Fb", "!"},
	} {
		got, err := ExternalRef(c.in)
		switch {
		case c.want == "!":
			if !errors.Is(err, ErrExternalRef) {
				t.Errorf("ExternalRef(%q) = %v, %v; want ErrExternalRef", c.in, got, err)
			}
		case c.want == "":
			if err != nil || got != nil {
				t.Errorf("ExternalRef(%q) = %v, %v; want no ref", c.in, got, err)
			}
		default:
			if err != nil || got == nil || *got != c.want {
				t.Errorf("ExternalRef(%q) = %v, %v; want %q", c.in, got, err, c.want)
			}
		}
	}
}

// TestStaffDB_ExternalRefIsBoundTrailedAndRefusedByName: the owner's bind writes the ref and ONE
// trail row naming the ref before and after; the same ref again is ErrSameExternalRef with no
// row; a ref a LIVE colleague holds is ErrExternalRefTaken (by the index's name), one a
// DEACTIVATED colleague holds is free; unbinding somebody with no home venue is
// ErrExternalRefRequired and changes nothing; another tenant's employee is ErrUnknownEmployee.
func TestStaffDB_ExternalRefIsBoundTrailedAndRefusedByName(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	ref := newRef("EMP")
	cmd := func(employee uuid.UUID, ref string) ExternalRefCommand {
		return ExternalRefCommand{TenantID: f.tenantID, SubjectID: employee, ActorID: f.actorID, ExternalRef: ref}
	}

	change, err := f.staff.ChangeExternalRef(ctx, cmd(f.employeeID, " "+ref+" "))
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if change.From != "" || change.To != ref {
		t.Errorf("the change reads %q -> %q, want \"\" -> %q", change.From, change.To, ref)
	}
	if got := f.externalRefOf(t, f.tenantID, f.employeeID); got == nil || *got != ref {
		t.Errorf("stored ref %v, want %q", got, ref)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeExternalRefChanged, f.employeeID); n != 1 {
		t.Errorf("%d trail rows after one bind, want 1", n)
	}
	if detail, _ := f.auditDetail(t, f.tenantID, ActionEmployeeExternalRefChanged, f.employeeID); !containsJSONField(detail, "to_external_ref", ref) ||
		!containsJSONField(detail, "from_external_ref", "") {
		t.Errorf("the trail row's detail is %s, want from \"\" to %q", detail, ref)
	}

	if _, err := f.staff.ChangeExternalRef(ctx, cmd(f.employeeID, ref)); !errors.Is(err, ErrSameExternalRef) {
		t.Errorf("the same ref again answered %v, want ErrSameExternalRef", err)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeExternalRefChanged, f.employeeID); n != 1 {
		t.Errorf("a refused same-ref wrote a trail row (%d rows)", n)
	}

	colleague := f.seedColleague(t, f.tenantID, f.locationID, nil)
	if _, err := f.staff.ChangeExternalRef(ctx, cmd(colleague, ref)); !errors.Is(err, ErrExternalRefTaken) {
		t.Errorf("a live colleague's ref answered %v, want ErrExternalRefTaken", err)
	}
	if _, err := f.staff.Deactivate(ctx, DeactivateCommand{TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID}); err != nil {
		t.Fatalf("deactivate the holder: %v", err)
	}
	if _, err := f.staff.ChangeExternalRef(ctx, cmd(colleague, ref)); err != nil {
		t.Errorf("a DEACTIVATED holder's ref was refused to a live colleague: %v (a re-hire keeps the ref)", err)
	}

	for _, bad := range []string{"a/b", ".x", strings.Repeat("x", 65)} {
		if _, err := f.staff.ChangeExternalRef(ctx, cmd(colleague, bad)); !errors.Is(err, ErrExternalRef) {
			t.Errorf("the ref %q answered %v, want ErrExternalRef", bad, err)
		}
	}

	// K-14: an API-managed person with no home venue keeps the ref -- it is their only anchor.
	venueless := uuid.New()
	vref := newRef("API")
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO employees (id, tenant_id, full_name, status, external_ref) VALUES ($1, $2, 'API person', 'invited', $3)`,
			venueless, f.tenantID, vref)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.staff.ChangeExternalRef(ctx, cmd(venueless, "")); !errors.Is(err, ErrExternalRefRequired) {
		t.Errorf("unbinding a person with no home venue answered %v, want ErrExternalRefRequired", err)
	}
	if got := f.externalRefOf(t, f.tenantID, venueless); got == nil || *got != vref {
		t.Errorf("the refused unbind changed the ref to %v", got)
	}
	if _, err := f.staff.ChangeExternalRef(ctx, cmd(venueless, newRef("API"))); err != nil {
		t.Errorf("CONTROL: re-binding the venue-less person to another ref was refused: %v", err)
	}
	if _, err := f.staff.ChangeExternalRef(ctx, ExternalRefCommand{TenantID: f.tenantID, SubjectID: f.foreignEmployee, ActorID: f.actorID, ExternalRef: newRef("X")}); !errors.Is(err, ErrUnknownEmployee) {
		t.Errorf("another tenant's employee answered %v, want ErrUnknownEmployee", err)
	}
	if got := f.externalRefOf(t, f.foreignTenant, f.foreignEmployee); got != nil {
		t.Errorf("another tenant's employee was given the ref %q", *got)
	}
}

// TestStaffDB_AnAddressLeftByADeactivatedPersonIsFree is K-24 through the panel's own two writes:
// adding somebody, or changing somebody's address, to the address of a LIVE colleague is
// ErrEmailTaken (the handler's sentence, never a 500) -- and to the address of a DEACTIVATED one
// succeeds.
func TestStaffDB_AnAddressLeftByADeactivatedPersonIsFree(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	address := uniqueAddress("k24")
	f.setStoredEmail(t, f.tenantID, f.employeeID, address)

	add := func(name, email string) (Person, error) {
		return f.staff.Add(ctx, AddCommand{TenantID: f.tenantID, ActorID: f.actorID, FullName: name, Email: email, LocationID: f.locationID})
	}
	if _, err := add("Joe Camilleri", address); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("adding a person with a LIVE colleague's address answered %v, want ErrEmailTaken", err)
	}
	other := f.seedColleague(t, f.tenantID, f.locationID, nil)
	if _, err := f.staff.ChangeEmail(ctx, EmailCommand{TenantID: f.tenantID, EmployeeID: other, ActorID: f.actorID, Email: address}); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("changing to a LIVE colleague's address answered %v, want ErrEmailTaken", err)
	}
	if _, err := f.staff.Deactivate(ctx, DeactivateCommand{TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID}); err != nil {
		t.Fatal(err)
	}
	rehired, err := add("Maria Borg", address)
	if err != nil {
		t.Fatalf("re-hiring with the address a DEACTIVATED person left was refused: %v", err)
	}
	if got := f.storedEmail(t, f.tenantID, rehired.ID); got == nil || *got != address {
		t.Errorf("the re-hire's address is %s, want %q", show(got), address)
	}
	if _, err := f.staff.ChangeEmail(ctx, EmailCommand{TenantID: f.tenantID, EmployeeID: other, ActorID: f.actorID, Email: address}); !errors.Is(err, ErrEmailTaken) {
		t.Errorf("the re-hire holds the address now; a change onto it answered %v, want ErrEmailTaken", err)
	}
}

// TestVenuesDB_ExternalRefIsBoundTrailedAndRefusedByName: the venue's bind writes the ref and one
// trail row (from/to); the same ref again is ErrSameExternalRef; another venue's ref is
// ErrExternalRefTaken; another tenant's venue is ErrUnknownVenue and is not touched; the read
// side carries the ref (Venue and Screen).
func TestVenuesDB_ExternalRefIsBoundTrailedAndRefusedByName(t *testing.T) {
	f := newVenueFixture(t)
	ctx := context.Background()
	ref := newRef("LOC")
	cmd := func(venue uuid.UUID, ref string) ExternalRefCommand {
		return ExternalRefCommand{TenantID: f.tenantID, SubjectID: venue, ActorID: f.actorID, ExternalRef: ref}
	}
	if _, err := f.venues.ChangeExternalRef(ctx, cmd(f.locationID, ref)); err != nil {
		t.Fatalf("bind: %v", err)
	}
	ven, err := f.venues.Venue(ctx, f.tenantID, f.locationID)
	if err != nil || ven.ExternalRef != ref {
		t.Errorf("Venue reads the ref %q (%v), want %q", ven.ExternalRef, err, ref)
	}
	screen, err := f.venues.Screen(ctx, f.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range screen.Venues {
		found = found || (v.ID == f.locationID && v.ExternalRef == ref)
	}
	if !found {
		t.Error("the venue screen does not carry the ref")
	}
	var rows int
	var detail string
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*), max(detail::text) FROM audit_log WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			f.tenantID, ActionLocationExternalRefChanged, f.locationID.String()).Scan(&rows, &detail)
	}); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || !strings.Contains(detail, `"to_external_ref": "`+ref+`"`) {
		t.Errorf("%d trail rows, detail %s; want one naming the new ref", rows, detail)
	}
	if _, err := f.venues.ChangeExternalRef(ctx, cmd(f.locationID, ref)); !errors.Is(err, ErrSameExternalRef) {
		t.Errorf("the same ref again answered %v, want ErrSameExternalRef", err)
	}
	second, err := f.venues.SaveVenue(ctx, VenueCommand{TenantID: f.tenantID, ActorID: f.actorID, Name: "Sliema " + uuid.NewString()[:6]})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.venues.ChangeExternalRef(ctx, cmd(second.ID, ref)); !errors.Is(err, ErrExternalRefTaken) {
		t.Errorf("another venue's ref answered %v, want ErrExternalRefTaken", err)
	}
	if _, err := f.venues.ChangeExternalRef(ctx, cmd(f.foreignVenue, newRef("LOC"))); !errors.Is(err, ErrUnknownVenue) {
		t.Errorf("another tenant's venue answered %v, want ErrUnknownVenue", err)
	}
	if _, err := f.venues.ChangeExternalRef(ctx, cmd(f.locationID, "")); err != nil {
		t.Errorf("unbinding a venue was refused: %v", err)
	}
}
