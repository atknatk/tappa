package operator_test

// holdings_db_test.go -- M11 API-1a end to end against PostgreSQL: the operator signs in through
// the surface and, on /operator/holdings, creates a holding (op_create_holding), adds businesses
// to it (op_add_holding_member) and removes them (op_remove_holding_member), each write followed
// by the list its 303 lands on (op_begin_read + op_read_holdings, migration 00042) -- on
// newLegalE2E's committed rig (op10_db_test.go: one owner connection switched to tappa_operator,
// each statement its own committed transaction; the read's two phases are two transactions, as
// on the operator's pool). The store is internal/db's production accessors on that connection
// (liveStore), the functions *db.OperatorDB delegates to verbatim.
//
// WHAT ONE RUN LEAVES BEHIND. TestE2E_HoldingsCreateAddReadRemoveThroughTheDefiners COMMITS, on
// tappa_app's pool in each tenant's own context, three fixture tenants ('FAKE api1a …', made-up
// IE-shaped VAT numbers, never a real business's) -- two in Europe/Malta, one in Europe/London --
// and, through the operator's definers, one holding ('FAKE api1a group …') with three ENDED
// memberships (the run removes every business it added; holding_members' rows are history and
// are never deleted), the tenants' own audit rows of those memberships (tenant.holding_joined,
// tenant.holding_member_joined, tenant.holding_left, tenant.holding_member_left -- append-only)
// and the operator's rows: eight 'read' rows (scope holdings), one 'holding_created', four
// 'holding_member_added' and two 'holding_member_removed'. newLegalE2E leaves, by construction,
// one operator account (disabled at cleanup), its sessions (revoked at cleanup) and its
// login and password_ok rows; read tickets are deleted at cleanup. The class is the backlog's
// T81 (test rows accumulating in the development database).

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// holdingFixtureTenant is one committed tenant of the run.
type holdingFixtureTenant struct {
	id       uuid.UUID
	name     string
	timezone string
}

// newHoldingFixtureTenants commits one tenant per timezone given (the file header says what it
// leaves). Each VAT number is IE-shaped and made for the run (tenants.vat_number is UNIQUE across
// every tenant; a taken one is drawn again).
func newHoldingFixtureTenants(t *testing.T, l *legalE2E, timezones ...string) []holdingFixtureTenant {
	t.Helper()
	app, err := db.New(l.ctx, &config.Config{DatabaseURL: os.Getenv("DATABASE_URL")})
	if err != nil {
		t.Fatalf("tappa_app's pool: %v", err)
	}
	t.Cleanup(app.Close)
	var out []holdingFixtureTenant
	for _, zone := range timezones {
		f := holdingFixtureTenant{id: uuid.New(), timezone: zone}
		f.name = "FAKE api1a " + f.id.String()[:8]
		var number string
		for attempt := 0; ; attempt++ {
			b := randBytes(t, 5)
			number = fmt.Sprintf("IE%d%c%05d%c", b[0]%10, 'A'+rune(b[1]%26), (int(b[2])<<8|int(b[3]))%100000, 'A'+rune(b[4]%26))
			if l.ownerInt(`SELECT count(*)::int FROM tenants WHERE vat_number = $1`, number) == 0 {
				break
			}
			if attempt == 5 {
				t.Fatal("PREMISE: six made-up numbers were all taken")
			}
		}
		err := app.WithTenant(l.ctx, f.id, func(ctx context.Context, tx pgx.Tx) error {
			_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
			                      VALUES ($1, $2, $3, 'restaurant', 'single', $4)`, f.id, f.name, number, f.timezone)
			return e
		})
		if err != nil {
			t.Fatalf("commit a holding fixture tenant: %v", err)
		}
		out = append(out, f)
	}
	return out
}

// TestE2E_HoldingsCreateAddReadRemoveThroughTheDefiners is API-1a's screen acceptance on
// PostgreSQL (the file header says what it commits). Each write is a 303 to the list with what it
// asked, and the list the 303 lands on -- one committed 'read' row, scope holdings, each -- says
// what the database did:
//
//  1. the list is a read: one 'read' row naming the scope holdings;
//  2. a holding is created: 303 with the id the database returned; the holding is in holdings
//     with the posted name, the operator's 'holding_created' row names it; the list says
//     "Created." and shows the name;
//  3. business A (Europe/Malta) is added: "Added."; A has one live membership of the holding,
//     its own 'tenant.holding_joined' row, and the operator's 'holding_member_added' row names it;
//     the list shows A, its timezone and its Remove button;
//  4. business C (Europe/London) is added: the write is ACCEPTED -- 303, and the operator's row
//     records the ask -- and the list says "Not added -- nothing changed." with the rules: C has
//     no membership and no row of its own (K-36, read from the database, not the handler);
//  5. business B (Europe/Malta) is added: "Added."; A's log gets 'tenant.holding_member_joined';
//  6. A is removed: "Removed."; A's membership has its left_at, A's log 'tenant.holding_left',
//     B's 'tenant.holding_member_left';
//  7. A is added again at once: "Not added" -- the 60-second exit window, the database's own rule;
//     A still has its one, ended, membership;
//  8. B is removed (the run ends with no live membership); the run made eight list reads.
func TestE2E_HoldingsCreateAddReadRemoveThroughTheDefiners(t *testing.T) {
	l := newLegalE2E(t)
	fx := newHoldingFixtureTenants(t, l, "Europe/Malta", "Europe/London", "Europe/Malta")
	a, c, b := fx[0], fx[1], fx[2]
	sess := l.signIn(l.f)
	reads := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1
		                   AND target_scope = 'holdings'`, l.f.id)
	}
	opRows := func(kind string, tenant *uuid.UUID, holding uuid.UUID) int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = $1 AND actor_admin_id = $2
		                   AND target_tenant_id IS NOT DISTINCT FROM $3 AND detail ->> 'holding_id' = $4`,
			kind, l.f.id, tenant, holding.String())
	}
	tenantRows := func(tenant uuid.UUID, action string) int {
		return l.ownerInt(`SELECT count(*)::int FROM audit_log WHERE tenant_id = $1 AND action = $2`, tenant, action)
	}
	memberships := func(holding, tenant uuid.UUID) (live, ended int) {
		live = l.ownerInt(`SELECT count(*)::int FROM holding_members WHERE holding_id = $1 AND member_tenant_id = $2 AND left_at IS NULL`,
			holding, tenant)
		ended = l.ownerInt(`SELECT count(*)::int FROM holding_members WHERE holding_id = $1 AND member_tenant_id = $2 AND left_at IS NOT NULL`,
			holding, tenant)
		return live, ended
	}
	// follow sends a write, holds its 303 to the list with what it asked, and returns the list.
	follow := func(what string, path string, form url.Values, wantLoc string) string {
		t.Helper()
		w := l.post(path, form, sess)
		loc := w.Result().Header.Get("Location")
		if w.Code != http.StatusSeeOther || (wantLoc != "" && loc != wantLoc) {
			t.Fatalf("%s = %d to %q, want 303 to %q; log: %s", what, w.Code, loc, wantLoc, l.logs.String())
		}
		r := l.get(loc, sess)
		if r.Code != http.StatusOK {
			t.Fatalf("the list after %s = %d; log: %s", what, r.Code, l.logs.String())
		}
		return r.Body.String()
	}
	saysOnly := func(what, body, heading string) {
		t.Helper()
		if hs := headingsOn(body); len(hs) != 1 || hs[0] != heading {
			t.Errorf("after %s the list says %v, want [%s]", what, hs, heading)
		}
	}

	// 1. The list is a read.
	if w := l.get("/operator/holdings", sess); w.Code != http.StatusOK || reads() != 1 {
		t.Fatalf("the list = %d with %d 'read' row(s); want 200 and one; log: %s", w.Code, reads(), l.logs.String())
	}

	// 2. Create.
	name := "FAKE api1a group " + a.id.String()[:8]
	w := l.post("/operator/holdings", url.Values{"name": {"  " + name + "  "}}, sess)
	loc := w.Result().Header.Get("Location")
	if w.Code != http.StatusSeeOther || !strings.HasPrefix(loc, "/operator/holdings?done=created&holding=") {
		t.Fatalf("the create = %d to %q; log: %s", w.Code, loc, l.logs.String())
	}
	holding, err := uuid.Parse(strings.TrimPrefix(loc, "/operator/holdings?done=created&holding="))
	if err != nil {
		t.Fatalf("the create's Location carries no holding id: %q", loc)
	}
	var stored string
	if err := l.owner.QueryRow(l.ctx, `SELECT name FROM holdings WHERE id = $1`, holding).Scan(&stored); err != nil || stored != name {
		t.Fatalf("the holding is stored as %q (%v), want the trimmed name %q", stored, err, name)
	}
	if n := opRows("holding_created", nil, holding); n != 1 {
		t.Errorf("%d 'holding_created' row(s) naming the holding, want 1", n)
	}
	body := l.get(loc, sess).Body.String()
	saysOnly("the create", body, headingCreated)
	if !strings.Contains(body, "<bdi>"+name+"</bdi>") {
		t.Error("the list after the create does not show the holding's name")
	}
	members := membersPathOf(holding.String())

	// 3. A joins.
	body = follow("adding A", members, url.Values{"tenant_id": {a.id.String()}}, resultPathOf("add", holding.String(), a.id.String()))
	saysOnly("adding A", body, headingAdded)
	if live, _ := memberships(holding, a.id); live != 1 || tenantRows(a.id, "tenant.holding_joined") != 1 ||
		opRows("holding_member_added", &a.id, holding) != 1 {
		t.Errorf("adding A: %d live membership(s), %d joined row(s), %d operator row(s); want 1 each", live,
			tenantRows(a.id, "tenant.holding_joined"), opRows("holding_member_added", &a.id, holding))
	}
	if !strings.Contains(body, "<bdi>"+a.name+"</bdi>") || !strings.Contains(body, `Timezone <span class="font-mono">Europe/Malta</span>`) ||
		!strings.Contains(body, `action="`+removePathOf(holding.String(), a.id.String())+`"`) {
		t.Error("the list after adding A does not show A, its timezone and its Remove button")
	}

	// 4. C, another timezone: accepted, and not added.
	body = follow("adding C", members, url.Values{"tenant_id": {c.id.String()}}, resultPathOf("add", holding.String(), c.id.String()))
	saysOnly("adding C", body, headingNotAdded)
	if live, ended := memberships(holding, c.id); live+ended != 0 || tenantRows(c.id, "tenant.holding_joined") != 0 ||
		opRows("holding_member_added", &c.id, holding) != 1 {
		t.Errorf("adding C (Europe/London): %d membership(s), %d joined row(s), %d operator row(s); want 0, 0 and the ask's 1",
			live+ended, tenantRows(c.id, "tenant.holding_joined"), opRows("holding_member_added", &c.id, holding))
	}
	if !strings.Contains(body, "<li>"+ruleTimezone+"</li>") || strings.Contains(body, c.name) {
		t.Error("the list after adding C does not list the rules, or shows C")
	}

	// 5. B joins.
	body = follow("adding B", members, url.Values{"tenant_id": {b.id.String()}}, resultPathOf("add", holding.String(), b.id.String()))
	saysOnly("adding B", body, headingAdded)
	if n := tenantRows(a.id, "tenant.holding_member_joined"); n != 1 {
		t.Errorf("A's log holds %d 'tenant.holding_member_joined' row(s) after B joined, want 1", n)
	}

	// 6. A leaves.
	body = follow("removing A", removePathOf(holding.String(), a.id.String()), url.Values{},
		resultPathOf("remove", holding.String(), a.id.String()))
	saysOnly("removing A", body, headingRemoved)
	if live, ended := memberships(holding, a.id); live != 0 || ended != 1 || tenantRows(a.id, "tenant.holding_left") != 1 ||
		tenantRows(b.id, "tenant.holding_member_left") != 1 || opRows("holding_member_removed", &a.id, holding) != 1 {
		t.Errorf("removing A: %d live and %d ended membership(s), A left %d, B told %d, operator %d; want 0, 1, 1, 1, 1",
			live, ended, tenantRows(a.id, "tenant.holding_left"), tenantRows(b.id, "tenant.holding_member_left"),
			opRows("holding_member_removed", &a.id, holding))
	}
	if strings.Contains(body, `action="`+removePathOf(holding.String(), a.id.String())+`"`) {
		t.Error("the list after removing A still offers A's Remove button")
	}

	// 7. A again, inside the exit window.
	body = follow("adding A again", members, url.Values{"tenant_id": {a.id.String()}}, resultPathOf("add", holding.String(), a.id.String()))
	saysOnly("adding A again", body, headingNotAdded)
	if live, ended := memberships(holding, a.id); live != 0 || ended != 1 || opRows("holding_member_added", &a.id, holding) != 2 {
		t.Errorf("adding A again inside 60 s: %d live and %d ended membership(s), %d ask(s); want 0, 1, 2", live, ended,
			opRows("holding_member_added", &a.id, holding))
	}

	// 8. B leaves; eight reads in all.
	body = follow("removing B", removePathOf(holding.String(), b.id.String()), url.Values{},
		resultPathOf("remove", holding.String(), b.id.String()))
	saysOnly("removing B", body, headingRemoved)
	if live := l.ownerInt(`SELECT count(*)::int FROM holding_members WHERE holding_id = $1 AND left_at IS NULL`, holding); live != 0 {
		t.Errorf("the run leaves %d live membership(s), want 0", live)
	}
	if n := reads(); n != 8 {
		t.Errorf("%d 'read' row(s) naming the holdings, want 8 (one per view of the list)", n)
	}
}
