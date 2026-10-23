package handler

// formbinding_db_test.go -- M14-10a end to end against real Postgres, with the real
// adminauth.Manager: a page drawn in business A, submitted after the same browser moved to
// business B, writes nothing anywhere and answers 409 (ADR 0031 §5.5).
//
// The move is a second sign-in in the same cookie jar -- the shape of M14-10's switch
// (a NEW session in B, the old one's page left open). Every row this test writes is in
// two fresh tenants of its own, like every panelHarness test; no identity table is
// touched, so no shared lock is taken.

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// venueFormAction is the location form's action as the RAW page writes it.
var venueFormAction = regexp.MustCompile(`action="(` + regexp.QuoteMeta(venueSaveHref) + `\?tb=[A-Za-z0-9_-]{43})"`)

// rawPage is p.get WITHOUT the binding normalisation: the page exactly as the browser has it.
func (p *panelHarness) rawPage(t *testing.T, path string) string {
	t.Helper()
	res, err := p.client.Get(p.server.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: %d", path, res.StatusCode)
	}
	return readAll(t, res)
}

// submitAsIs posts form to the action exactly as a page wrote it (entities decoded).
func (p *panelHarness) submitAsIs(t *testing.T, action string, form url.Values) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, p.server.URL+strings.ReplaceAll(action, "&amp;", "&"), strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", p.server.URL)
	res, err := p.client.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", action, err)
	}
	readAll(t, res)
	return res
}

// addBusiness inserts a second business with its own owner, the harness's password.
func (p *panelHarness) addBusiness(t *testing.T) (tenantID uuid.UUID, email string) {
	t.Helper()
	tenantID, email = uuid.New(), "panel-formbind-"+uuid.NewString()+"@m14.example"
	if err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Form Binding B Ltd', $2, 'bar', 'single')`, tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'B Owner', $3, $4, 'owner', 'active')`, uuid.New(), tenantID, email, ownerDigest)
		return e
	}); err != nil {
		t.Fatalf("insert business B: %v", err)
	}
	return tenantID, email
}

// auditRowsIn is how many audit rows tenant holds.
func (p *panelHarness) auditRowsIn(t *testing.T, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	if err := p.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1`, tenantID).Scan(&n)
	}); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

// TestFormBinding_AStaleFormWritesNothingInTheOtherBusiness: the location form is drawn
// for business A; the browser signs in to business B; the A page's form is submitted.
// The answer is 409 and NEITHER business gains a location or an audit row -- also when B
// is suspended, where the refusal must come before the suspension ring writes its row.
// CONTROL: the same form drawn in B saves in B.
func TestFormBinding_AStaleFormWritesNothingInTheOtherBusiness(t *testing.T) {
	p := newPanelHarness(t)
	tenantA := p.tenantID
	p.signIn(t)
	stale := venueFormAction.FindStringSubmatch(p.rawPage(t, "/admin/locations?venue=new"))
	if stale == nil {
		t.Fatal("PREMISE: business A's locations page draws no bound location form")
	}

	tenantB, emailB := p.addBusiness(t)
	p.tenantID, p.email = tenantB, emailB
	p.signIn(t)
	fresh := venueFormAction.FindStringSubmatch(p.rawPage(t, "/admin/locations?venue=new"))
	if fresh == nil || fresh[1] == stale[1] {
		t.Fatal("PREMISE: business B's page carries no binding of its own")
	}

	countA := func() int {
		p.tenantID = tenantA
		defer func() { p.tenantID = tenantB }()
		return p.venuesNamed(t, "Stale Tab Venue")
	}
	auditA, auditB := p.auditRowsIn(t, tenantA), p.auditRowsIn(t, tenantB)

	res := p.submitAsIs(t, stale[1], url.Values{"name": {"Stale Tab Venue"}})
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("A's form submitted in B answered %d, want 409", res.StatusCode)
	}
	if b, a := p.venuesNamed(t, "Stale Tab Venue"), countA(); b != 0 || a != 0 {
		t.Fatalf("the stale form wrote %d location(s) in B and %d in A, want 0 and 0", b, a)
	}
	if a, b := p.auditRowsIn(t, tenantA), p.auditRowsIn(t, tenantB); a != auditA || b != auditB {
		t.Fatalf("the refusal wrote audit rows: A %d -> %d, B %d -> %d", auditA, a, auditB, b)
	}

	// B suspended: still 409, and no suspension refusal row in B.
	owner := ownerPoolForTest(t)
	suspendAsOwner(t, owner, tenantB, db.SuspensionNonPayment)
	if res := p.submitAsIs(t, stale[1], url.Values{"name": {"Stale Tab Venue"}}); res.StatusCode != http.StatusConflict {
		t.Fatalf("A's form submitted in a suspended B answered %d, want 409", res.StatusCode)
	}
	if n := len(p.refusalRows(t)); n != 0 {
		t.Fatalf("the stale form left %d suspension refusal row(s) in B, want 0", n)
	}
	liftAsOwner(t, owner, tenantB)

	if res := p.submitAsIs(t, fresh[1], url.Values{"name": {"Fresh Tab Venue"}}); res.StatusCode != http.StatusSeeOther {
		t.Fatalf("CONTROL: B's own form answered %d, want 303", res.StatusCode)
	} else if loc := res.Header.Get("Location"); strings.Contains(loc, "tb=") {
		t.Errorf("CONTROL: the save redirected to %q, which carries the binding", loc)
	}
	if n := p.venuesNamed(t, "Fresh Tab Venue"); n != 1 {
		t.Fatalf("CONTROL: B's own form saved %d location(s) in B, want 1", n)
	}
}
