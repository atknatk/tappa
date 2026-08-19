package tenant

// plaque_db_test.go -- M6-06 phase B's plaque path against REAL Postgres.
//
// 🔴 A FAKE CANNOT TEST ANY OF THIS, and the properties below are exactly the ones
// a mock would agree with unconditionally:
//
//   - the RETIRE and the BIND share ONE TRANSACTION. Split across two, a failure in
//     between leaves an entrance whose old plaque is retired and whose new one is
//     still in a box -- every tap there rejected, nothing on any screen saying why.
//     Measured by making the SECOND half fail and reading the FIRST half back.
//   - the audit rows share that same transaction. `tags` has no updated_at, so a
//     trail that can be lost while the change survives means the plaque every tap
//     at that door is authenticated by changed with no record of who did it.
//   - the OLD ROW IS NOT DELETED and cannot be: 00004 revokes DELETE from
//     tappa_app. That is the card's own criterion and it is a GRANT, not a habit.
//   - section 4.5. A foreign uid writes nothing, and it is REFUSED rather than
//     erroring, because the explicit tenant predicate and the RLS policy both bite.
//   - section 4.7. No query on this path selects aes_key_ref, and no type on it can
//     hold one. The reflection test at the bottom is the wall; the SQL assertion
//     beside it is what stops a future query from widening it.
//
// FIXTURES ARE NOT CLEANED UP, for the reason venue_db_test.go states: audit_log is
// append-only (00005) and `tags` cannot be deleted at all by this role. Fresh random
// ids keep runs from colliding, and `make db-reset` clears the development database.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/test/fixtures"
)

// plaqueFixture is one tenant with two venues, plus a SECOND tenant to attack it
// from.
type plaqueFixture struct {
	data    *db.DB
	plaques *Plaques
	trail   *audit.Recorder

	tenantID   uuid.UUID
	locationID uuid.UUID
	otherWall  uuid.UUID
	employeeID uuid.UUID
	actorID    uuid.UUID

	foreignTenant uuid.UUID
	foreignVenue  uuid.UUID
}

func newPlaqueFixture(t *testing.T) *plaqueFixture {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping plaque tests (real Postgres required)")
	}
	data, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)

	trail, err := audit.New(data)
	if err != nil {
		t.Fatalf("audit.New: %v", err)
	}
	plaques, err := NewPlaques(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewPlaques: %v", err)
	}
	f := &plaqueFixture{
		data: data, plaques: plaques, trail: trail,
		tenantID: uuid.New(), locationID: uuid.New(), otherWall: uuid.New(),
		employeeID: uuid.New(), actorID: uuid.New(),
		foreignTenant: uuid.New(), foreignVenue: uuid.New(),
	}
	f.seedTenant(t, f.tenantID, f.locationID, f.otherWall)
	f.seedTenant(t, f.foreignTenant, f.foreignVenue, uuid.New())
	return f
}

func (f *plaqueFixture) seedTenant(t *testing.T, tenantID, wall, second uuid.UUID) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'Kebab Factory Ltd', $2, 'restaurant', 'multi')`,
			// THE FULL UUID, NOT ITS FIRST EIGHT HEX DIGITS. vat_number is UNIQUE and a
			// 32-bit slice of it collided about once every 89 suite runs against this
			// development database (measured in the data-layer round); the whole uuid is
			// 122 random bits.
			tenantID, "VAT-"+tenantID.String()); e != nil {
			return e
		}
		for _, id := range []uuid.UUID{wall, second} {
			if _, e := tx.Exec(ctx,
				`INSERT INTO locations (id, tenant_id, name, static_ips)
				 VALUES ($1, $2, 'St Julians', '{192.168.1.0/24}')`, id, tenantID); e != nil {
				return e
			}
		}
		if tenantID != f.tenantID {
			return nil
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, 'Maria Borg', 'active', now())`,
			f.employeeID, tenantID, wall)
		return e
	})
	if err != nil {
		t.Fatalf("seed tenant: %v", err)
	}
}

// load inserts a plaque the way TAPPA'S LOADER would -- a two-byte placeholder in
// aes_key_ref, this tenant, and the caller's status and wall.
//
// 🔴 THERE IS NO PRODUCT QUERY THAT DOES THIS, which is the inventory model as a
// fixture: db/queries/tags.sql ships no INSERT over `tags`, because creating a
// plaque means holding its key. This stands in for the M8-05 runbook.
//
// wall == uuid.Nil produces STOCK: location_id NULL. The status and the wall travel
// together because 00013's CHECKs bind them, so a fixture cannot build the state the
// schema forbids.
func (f *plaqueFixture) load(t *testing.T, tenantID uuid.UUID, status string, wall uuid.UUID) string {
	t.Helper()
	return f.loadUID(t, tenantID, newPlaqueUID(t), status, wall)
}

func (f *plaqueFixture) loadUID(t *testing.T, tenantID uuid.UUID, uid, status string, wall uuid.UUID) string {
	t.Helper()
	var location *uuid.UUID
	if wall != uuid.Nil {
		w := wall
		location = &w
	}
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
			 VALUES ($1, $2, $3, decode(repeat('dead', 22), 'hex'), 41, $4)`,
			uid, tenantID, location, status)
		return e
	})
	if err != nil {
		t.Fatalf("load plaque (%s): %v", status, err)
	}
	return uid
}

func newPlaqueUID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 7)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	// UPPER CASE, which is the only spelling migration 00013 accepts for a NEW row.
	return strings.ToUpper(hex.EncodeToString(b))
}

// row reads a plaque straight out of the table, bypassing the domain, so an
// assertion about what was STORED is not an assertion about what was returned.
func (f *plaqueFixture) row(t *testing.T, uid string) (status string, wall *uuid.UUID, retiredAt *time.Time, replacedBy *string) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT status, location_id, retired_at, replaced_by FROM tags
			 WHERE tenant_id = $1 AND uid = $2`, f.tenantID, uid).
			Scan(&status, &wall, &retiredAt, &replacedBy)
	})
	if err != nil {
		t.Fatalf("read plaque row: %v", err)
	}
	return status, wall, retiredAt, replacedBy
}

func (f *plaqueFixture) auditRows(t *testing.T, action, target string) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM audit_log
			 WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			f.tenantID, action, target).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

func (f *plaqueFixture) auditDetail(t *testing.T, action, target string) string {
	t.Helper()
	var detail string
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT detail::text FROM audit_log
			 WHERE tenant_id = $1 AND action = $2 AND target = $3
			 ORDER BY at DESC LIMIT 1`, f.tenantID, action, target).Scan(&detail)
	})
	if err != nil {
		t.Fatalf("read audit detail: %v", err)
	}
	return detail
}

// seedTap writes one transaction for a plaque so "last seen" has something to find.
func (f *plaqueFixture) seedTap(t *testing.T, uid string, ago time.Duration) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		// THE COLUMN SET IS checkin_db_test.go's seedAgedRecord, not one invented here:
		// `transactions` carries transactions_ok_has_direction and
		// transactions_policy_decision_consistent, so a hand-built row is refused unless
		// the verdict, the direction and the policy columns agree.
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, type,
			                           occurred_at, created_at, verdict, channel, sun_valid, trust)
			 VALUES ($1, $2, $3, $4, 'in', now() - $5::interval, now() - $5::interval,
			         'ok', 'nfc', true, 100)`,
			f.tenantID, f.employeeID, f.locationID, uid, ago.String())
		return e
	})
	if err != nil {
		t.Fatalf("seed tap: %v", err)
	}
}

// --- the list ------------------------------------------------------------------

// TestPlaquesDB_ScreenCarriesTheFiveColumnsAndNeverInventsALastSeen is the card's
// first criterion -- uid, status, bound location, last_ctr, last seen -- plus the
// one thing that criterion is easy to get wrong.
//
// 🔴 "NEVER TAPPED" IS THE ABSENCE OF A STAMP, NOT A ZERO ONE. A zero time.Time is
// orderable, formattable and wrong; it is the twin of reading a NULL location as
// uuid.Nil, which is the defect this milestone inherited. ListTagLastSeen returns no
// row at all for an untapped plaque, and this asserts the nil survives the merge.
func TestPlaquesDB_ScreenCarriesTheFiveColumnsAndNeverInventsALastSeen(t *testing.T) {
	f := newPlaqueFixture(t)
	mounted := f.load(t, f.tenantID, PlaqueActive, f.locationID)
	stock := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)
	f.seedTap(t, mounted, 2*time.Hour)

	screen, err := f.plaques.Screen(context.Background(), f.tenantID)
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}
	byUID := map[string]Plaque{}
	for _, p := range screen.Plaques {
		byUID[p.UID] = p
	}
	got, ok := byUID[mounted]
	if !ok {
		t.Fatalf("the mounted plaque is missing from the screen")
	}
	if got.Status != PlaqueActive {
		t.Errorf("status = %q, want active", got.Status)
	}
	if got.LocationID == nil || *got.LocationID != f.locationID {
		t.Errorf("location = %v, want %s", got.LocationID, f.locationID)
	}
	if got.LastCtr != 41 {
		t.Errorf("last_ctr = %d, want 41", got.LastCtr)
	}
	if got.LastSeen == nil {
		t.Errorf("last_seen = nil for a plaque that HAS been tapped")
	}

	box, ok := byUID[stock]
	if !ok {
		t.Fatalf("the stock plaque is missing from the screen")
	}
	if box.LocationID != nil {
		t.Errorf("a plaque in stock has location = %v, want nil (that is what stock IS)", box.LocationID)
	}
	if box.LastSeen != nil {
		t.Errorf("last_seen = %v for a plaque that has NEVER been tapped; the absence "+
			"must stay an absence", box.LastSeen)
	}
	if !box.InStock() {
		t.Errorf("InStock() = false for status %q", box.Status)
	}
	if screen.Zone == nil {
		t.Errorf("Zone = nil; every stamp on the screen is rendered in it")
	}
}

// TestPlaquesDB_TheReplacementChainReadsBOTHWays. The schema stores it forwards
// (replaced_by); the backward step is derived in Go from the same list. Both ends
// are the card's "tag history is visible" criterion.
func TestPlaquesDB_TheReplacementChainReadsBOTHWays(t *testing.T) {
	f := newPlaqueFixture(t)
	old := f.load(t, f.tenantID, PlaqueActive, f.locationID)
	fresh := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)

	if _, err := f.plaques.Replace(context.Background(), ReplaceCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		RetiringUID: old, SuccessorUID: CanonicalUID(fresh), VenueName: "St Julians",
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	screen, err := f.plaques.Screen(context.Background(), f.tenantID)
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}
	byUID := map[string]Plaque{}
	for _, p := range screen.Plaques {
		byUID[p.UID] = p
	}
	if got := byUID[old].ReplacedBy; got != fresh {
		t.Errorf("old.ReplacedBy = %q, want %s", got, fresh)
	}
	if got := byUID[fresh].Replaces; got != old {
		t.Errorf("new.Replaces = %q, want %s -- the backward half of the chain is "+
			"derived in Go and this is the only thing that proves it", got, old)
	}
}

// TestPlaquesDB_HistoryAnswersWHOTookThePlaqueOffTheWall is the audit half of the
// card's "tag history is visible (audit)" criterion.
//
// 🔴 IT IS THE HALF THE `tags` ROW CANNOT ANSWER. The row carries created_at,
// retired_at and the replaced_by chain, so the docket can already say what happened
// and when; it has no actor column, so "who did this?" — the question a manager
// arrives with when a door has stopped working — is answerable only from the trail.
func TestPlaquesDB_HistoryAnswersWHOTookThePlaqueOffTheWall(t *testing.T) {
	f := newPlaqueFixture(t)
	f.namedAdmin(t, f.actorID, "Rita Camilleri")
	old := f.load(t, f.tenantID, PlaqueActive, f.locationID)
	fresh := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)

	if _, err := f.plaques.Replace(context.Background(), ReplaceCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		RetiringUID: old, SuccessorUID: CanonicalUID(fresh), VenueName: "St Julians",
	}); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	retired, err := f.plaques.History(context.Background(), f.tenantID, old)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(retired) != 1 {
		t.Fatalf("the retired plaque has %d trail entries, want 1", len(retired))
	}
	if retired[0].Action != ActionPlaqueRetired {
		t.Errorf("action = %q, want %q", retired[0].Action, ActionPlaqueRetired)
	}
	if retired[0].ActorName != "Rita Camilleri" {
		t.Errorf("actor = %q, want the admin's name JOINED from admin_users — a name "+
			"stored in the trail would be the name at write time and would drift",
			retired[0].ActorName)
	}
	if retired[0].At.IsZero() {
		t.Errorf("the entry carries no time")
	}

	mounted, err := f.plaques.History(context.Background(), f.tenantID, fresh)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(mounted) != 1 || mounted[0].Action != ActionPlaqueMounted {
		t.Fatalf("the successor's trail = %v, want one plaque.mounted entry", mounted)
	}

	// 🔴 §4.5: ANOTHER BUSINESS'S PLAQUE HAS NO HISTORY HERE. The uid is a global
	// primary key printed on a wall, so this is the one identifier an attacker can
	// simply read.
	theirs := f.load(t, f.foreignTenant, PlaqueActive, f.foreignVenue)
	if got, err := f.plaques.History(context.Background(), f.tenantID, theirs); err != nil || len(got) != 0 {
		t.Fatalf("History for another business's plaque = (%v, %v), want empty", got, err)
	}
}

// namedAdmin registers the actor in admin_users so the trail's LEFT JOIN has a name
// to find. Without it the join is exercised only on its NULL side, which is the
// arm that passes for the wrong reason.
func (f *plaqueFixture) namedAdmin(t *testing.T, id uuid.UUID, name string) {
	t.Helper()
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, $3, $4, $5, 'owner', 'active')`,
			id, f.tenantID, name, "plaque-"+id.String()+"@m6.example", fixtures.UnusablePasswordHash)
		return e
	}); err != nil {
		t.Fatalf("register the admin: %v", err)
	}
}

// TestPlaquesDB_TheTwoAuditPayloadsCarryEXACTLYTheseKeys is C4: the allow-list net
// phase A puts on location.delete_refused, applied to the two payloads this file
// writes.
//
// 🔴 A FORBIDDEN-STRING SCAN WAS BEATEN IN ONE EDIT ON THE OTHER PAYLOAD — a session
// id dropped into a free-text field passed a seven-word denylist, because a uuid
// contains none of those words. So the check is the KEY SET, exactly: a new field
// fails here until somebody adds it with a reason, which is the moment to ask what
// it carries. §4.7.
func TestPlaquesDB_TheTwoAuditPayloadsCarryEXACTLYTheseKeys(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want []string
	}{
		{"plaque.mounted", mountedDetail{}, []string{"from_status", "location_id", "name", "replaces"}},
		{"plaque.retired", retiredDetail{}, []string{"last_ctr", "location_id", "name"}},
		{"plaque.unmounted", unmountedDetail{}, []string{"from_status", "location_id", "name"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.val)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var got map[string]any
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			keys := make([]string, 0, len(got))
			for k := range got {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			if strings.Join(keys, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("%s detail keys = %v, want exactly %v", tc.name, keys, tc.want)
			}
			// 🔴 AND NO omitempty ON ANY OF THEM — the lesson deletedDetail records. The
			// zero value above is marshalled precisely so an ABSENT key shows up as a
			// missing one: a key that disappears when empty makes "there was none"
			// indistinguishable from "that version did not record it" (§4.6).
		})
	}
}

// --- mounting -------------------------------------------------------------------

// TestPlaquesDB_MountBindsStockAndWritesTheTrailInOneTransaction.
func TestPlaquesDB_MountBindsStockAndWritesTheTrailInOneTransaction(t *testing.T) {
	f := newPlaqueFixture(t)
	uid := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)

	got, err := f.plaques.Mount(context.Background(), MountCommand{
		TenantID: f.tenantID, ActorID: f.actorID, UID: uid,
		LocationID: f.locationID, VenueName: "St Julians",
	})
	if err != nil {
		t.Fatalf("Mount: %v", err)
	}
	if !got.OnAWall() {
		t.Fatalf("returned status = %q, want active", got.Status)
	}
	status, wall, _, _ := f.row(t, uid)
	if status != PlaqueActive || wall == nil || *wall != f.locationID {
		t.Fatalf("stored row = (%s, %v), want (active, %s)", status, wall, f.locationID)
	}
	if n := f.auditRows(t, ActionPlaqueMounted, uid); n != 1 {
		t.Fatalf("plaque.mounted rows = %d, want 1", n)
	}
	if d := f.auditDetail(t, ActionPlaqueMounted, uid); !strings.Contains(d, "St Julians") {
		t.Fatalf("trail detail = %s, want the venue name in `name` (the acknowledgement "+
			"reads it from THERE, never from the URL)", d)
	}
}

// TestPlaquesDB_MountingSomethingAlreadyOnAWallIsRefusedNotOverwritten. The
// precondition lives in the statement's own WHERE, so this is the race two managers
// produce -- and the answer must be a sentence, not a silent re-bind.
func TestPlaquesDB_MountingSomethingAlreadyOnAWallIsRefusedNotOverwritten(t *testing.T) {
	f := newPlaqueFixture(t)
	uid := f.load(t, f.tenantID, PlaqueActive, f.locationID)

	_, err := f.plaques.Mount(context.Background(), MountCommand{
		TenantID: f.tenantID, ActorID: f.actorID, UID: uid, LocationID: f.otherWall,
	})
	if !errors.Is(err, ErrPlaqueNotInStock) {
		t.Fatalf("Mount on an active plaque = %v, want ErrPlaqueNotInStock", err)
	}
	_, wall, _, _ := f.row(t, uid)
	if wall == nil || *wall != f.locationID {
		t.Fatalf("the wall moved to %v; a refused mount must change nothing", wall)
	}
	if n := f.auditRows(t, ActionPlaqueMounted, uid); n != 0 {
		t.Fatalf("plaque.mounted rows = %d after a refusal, want 0", n)
	}
}

// TestPlaquesDB_MountingOntoAnotherBusinessesVenueIsRefused is section 4.5 at the
// key rather than at the predicate: the composite FK (location_id, tenant_id) ->
// locations (id, tenant_id) is what makes a cross-tenant bind structurally
// impossible, and the domain turns the 23503 into a sentence rather than a 500.
func TestPlaquesDB_MountingOntoAnotherBusinessesVenueIsRefused(t *testing.T) {
	f := newPlaqueFixture(t)
	uid := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)

	_, err := f.plaques.Mount(context.Background(), MountCommand{
		TenantID: f.tenantID, ActorID: f.actorID, UID: uid, LocationID: f.foreignVenue,
	})
	if !errors.Is(err, ErrUnknownVenue) {
		t.Fatalf("Mount onto another business's venue = %v, want ErrUnknownVenue", err)
	}
	status, wall, _, _ := f.row(t, uid)
	if status != PlaqueUnassigned || wall != nil {
		t.Fatalf("row = (%s, %v) after a refused cross-tenant mount, want (unassigned, nil)",
			status, wall)
	}
}

// TestPlaquesDB_AnotherBusinessesPlaqueIsInvisibleAndUnwritable. The uid is a GLOBAL
// primary key and is printed on a wall, so it is the one identifier an attacker can
// simply read. Both the explicit tenant predicate and RLS refuse it.
func TestPlaquesDB_AnotherBusinessesPlaqueIsInvisibleAndUnwritable(t *testing.T) {
	f := newPlaqueFixture(t)
	theirs := f.load(t, f.foreignTenant, PlaqueUnassigned, uuid.Nil)

	if _, err := f.plaques.Plaque(context.Background(), f.tenantID, theirs); !errors.Is(err, ErrUnknownPlaque) {
		t.Fatalf("reading another business's plaque = %v, want ErrUnknownPlaque", err)
	}
	if _, err := f.plaques.Mount(context.Background(), MountCommand{
		TenantID: f.tenantID, ActorID: f.actorID, UID: theirs, LocationID: f.locationID,
	}); !errors.Is(err, ErrUnknownPlaque) {
		t.Fatalf("mounting another business's plaque = %v, want ErrUnknownPlaque", err)
	}
	screen, err := f.plaques.Screen(context.Background(), f.tenantID)
	if err != nil {
		t.Fatalf("Screen: %v", err)
	}
	for _, p := range screen.Plaques {
		if p.UID == theirs {
			t.Fatalf("another business's plaque appears in this business's list")
		}
	}
}

// TestPlaquesDB_UnmountPutsTheWrongDoorRightAndSPENDSNoSpare is D1: the act that
// makes "a mount needs no confirmation" a TRUE sentence.
//
// 🔴 IT EXISTS BECAUSE AN AUDIT MEASURED THE OPPOSITE OF TWO SHIPPED COMMENTS. A
// plaque mounted at the wrong entrance could not be moved, could not go back to
// stock, and with no spare in the box its card offered no action at all — while
// every tap at that door was judged against the WRONG venue's IP and coordinate,
// reaching §5 row 7 and the approval queue. The schema always permitted the reverse;
// there was no statement for it.
func TestPlaquesDB_UnmountPutsTheWrongDoorRightAndSPENDSNoSpare(t *testing.T) {
	f := newPlaqueFixture(t)
	f.namedAdmin(t, f.actorID, "Rita Camilleri")
	uid := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)

	// The mistake: mounted at the wrong door.
	if _, err := f.plaques.Mount(context.Background(), MountCommand{
		TenantID: f.tenantID, ActorID: f.actorID, UID: uid,
		LocationID: f.otherWall, VenueName: "The wrong door",
	}); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	// The undo.
	back, err := f.plaques.Unmount(context.Background(), UnmountCommand{
		TenantID: f.tenantID, ActorID: f.actorID, UID: uid, VenueName: "The wrong door",
	})
	if err != nil {
		t.Fatalf("Unmount: %v", err)
	}
	if !back.InStock() {
		t.Fatalf("returned status = %q, want unassigned", back.Status)
	}
	status, wall, retiredAt, replacedBy := f.row(t, uid)
	if status != PlaqueUnassigned || wall != nil {
		t.Fatalf("stored row = (%s, %v), want (unassigned, nil)", status, wall)
	}
	// 🔴 IT IS NOT A RETIREMENT. "When did this plaque leave the wall for good" must
	// still have exactly one answer, and no spare was spent to fix a mistake.
	if retiredAt != nil || replacedBy != nil {
		t.Fatalf("un-mount stamped retired_at=%v replaced_by=%v; it is not a retirement",
			retiredAt, replacedBy)
	}

	// And it can go up on the RIGHT door, which is the whole point.
	if _, err := f.plaques.Mount(context.Background(), MountCommand{
		TenantID: f.tenantID, ActorID: f.actorID, UID: uid,
		LocationID: f.locationID, VenueName: "St Julians",
	}); err != nil {
		t.Fatalf("re-mounting on the right door: %v", err)
	}
	if status, wall, _, _ := f.row(t, uid); status != PlaqueActive || wall == nil || *wall != f.locationID {
		t.Fatalf("row = (%s, %v), want (active, %s)", status, wall, f.locationID)
	}

	// THE TRAIL CARRIES ALL THREE ACTS, newest first, and ListPlaqueHistory picked the
	// new one up with no change because its filter is the PREFIX `plaque.%`.
	trail, err := f.plaques.History(context.Background(), f.tenantID, uid)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(trail) != 3 {
		t.Fatalf("trail has %d entries, want 3 (mounted, unmounted, mounted)", len(trail))
	}
	if trail[1].Action != ActionPlaqueUnmounted {
		t.Fatalf("the middle entry is %q, want %q", trail[1].Action, ActionPlaqueUnmounted)
	}
	if trail[1].ActorName != "Rita Camilleri" {
		t.Errorf("the un-mount names %q as the actor", trail[1].ActorName)
	}
	if d := f.auditDetail(t, ActionPlaqueUnmounted, uid); !strings.Contains(d, "The wrong door") {
		t.Errorf("the un-mount trail row = %s, want the wall it CAME OFF — the row's own "+
			"location_id is NULL from that moment, so this is the only surviving pointer", d)
	}
}

// TestPlaquesDB_TenConcurrentUnmountsProduceExactlyONEWinner is the race the
// statements' own WHERE clauses exist for, RUN rather than described.
//
// 🔴 IT EXISTS BECAUSE A COMMENT NAMED A RACE AND THE TEST BELOW IT WAS SEQUENTIAL.
// The property holds — a security audit measured it — but nothing in the suite would
// notice if somebody replaced `status = 'active'` in the statement with a SELECT
// first and an UPDATE after, which is the read-then-write hole §4.4 forbids by name
// wearing a different column. This repository's own rule: a defence whose removal no
// test notices is not a defence.
//
// TEN GOROUTINES, ONE PLAQUE, `-race`. Exactly one must succeed; the other nine must
// get the typed refusal, not an error and not a second success.
//
// COST, measured on this machine: about 0.2 s for the pair of tests below, against a
// `make test` that already runs ~190 s. The count is 10 rather than the 50 the
// counter race uses because what is being serialised here is a single-row UPDATE on
// its primary key, and the shape shows at 10 — the ctr test needs a wider fan-out
// because it is proving a monotonic sequence, not a single winner.
func TestPlaquesDB_TenConcurrentUnmountsProduceExactlyONEWinner(t *testing.T) {
	f := newPlaqueFixture(t)
	uid := f.load(t, f.tenantID, PlaqueActive, f.locationID)

	const n = 10
	var wg sync.WaitGroup
	var won, refused, failed int32
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := f.plaques.Unmount(context.Background(), UnmountCommand{
				TenantID: f.tenantID, ActorID: f.actorID, UID: uid,
			})
			switch {
			case err == nil:
				atomic.AddInt32(&won, 1)
			case errors.Is(err, ErrPlaqueNoWall):
				atomic.AddInt32(&refused, 1)
			default:
				atomic.AddInt32(&failed, 1)
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if won != 1 {
		t.Fatalf("%d of %d un-mounts succeeded, want exactly 1 — the precondition lives "+
			"in the statement's own WHERE precisely so this cannot be 2", won, n)
	}
	if refused != n-1 {
		t.Fatalf("refusals = %d, want %d (won=%d, failed=%d); a loser must get the typed "+
			"refusal, never an error", refused, n-1, won, failed)
	}
	if status, wall, retiredAt, _ := f.row(t, uid); status != PlaqueUnassigned ||
		wall != nil || retiredAt != nil {
		t.Fatalf("row = (%s, wall=%v, retired_at=%v), want (unassigned, nil, nil)",
			status, wall, retiredAt)
	}
	// AND ONE TRAIL ROW, NOT TEN. The audit row shares the write's transaction, so a
	// second winner would show up here even if the row somehow looked right.
	if n := f.auditRows(t, ActionPlaqueUnmounted, uid); n != 1 {
		t.Fatalf("plaque.unmounted rows = %d, want exactly 1", n)
	}
}

// TestPlaquesDB_TenConcurrentMountsProduceExactlyONEWinner is the race test that
// pins THE STATEMENT'S OWN PRECONDITION, and it is a separate one for a reason worth
// measuring rather than assuming.
//
// 🔴 THE UN-MOUNT RACE ABOVE CANNOT PIN IT — measured. Unmount READS the row before
// it writes (it needs the wall for the trail row, which the write destroys), so under
// READ COMMITTED a goroutine that starts after the winner commits is refused at that
// read and never reaches the UPDATE. A mutation that turned the statement's
// pgx.ErrNoRows into a fabricated success therefore stayed GREEN at n=10 and at n=30:
// the pre-read had already closed the window. That does not make the statement's
// WHERE decorative — inside the window (two transactions that both read before either
// commits) it is the ONLY guard — it means the un-mount race proves the OUTCOME and
// not the MECHANISM.
//
// MOUNT HAS NO PRE-READ. AssignTagToLocation is the whole act, so every goroutine
// reaches the UPDATE and the statement's `status = 'unassigned'` is the only thing
// that can decide. The same mutation here is RED, deterministically.
func TestPlaquesDB_TenConcurrentMountsProduceExactlyONEWinner(t *testing.T) {
	f := newPlaqueFixture(t)
	uid := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)

	const n = 10
	var wg sync.WaitGroup
	var won, refused int32
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// HALF AIM AT EACH WALL, so a second winner would also be a plaque in two
			// places — a state the row cannot hold and the count below would catch.
			wall := f.locationID
			if i%2 == 1 {
				wall = f.otherWall
			}
			<-start
			_, err := f.plaques.Mount(context.Background(), MountCommand{
				TenantID: f.tenantID, ActorID: f.actorID, UID: uid, LocationID: wall,
			})
			switch {
			case err == nil:
				atomic.AddInt32(&won, 1)
			case errors.Is(err, ErrPlaqueNotInStock):
				atomic.AddInt32(&refused, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if won != 1 {
		t.Fatalf("%d of %d mounts succeeded, want exactly 1 — `status = 'unassigned'` "+
			"lives in the statement's own WHERE precisely so two managers binding the "+
			"same plaque to two different entrances cannot both win", won, n)
	}
	if refused != n-1 {
		t.Fatalf("refusals = %d, want %d", refused, n-1)
	}
	if n := f.auditRows(t, ActionPlaqueMounted, uid); n != 1 {
		t.Fatalf("plaque.mounted rows = %d, want exactly 1 — the trail shares the "+
			"write's transaction, so a second winner shows up here too", n)
	}
}

// TestPlaquesDB_TenConcurrentReplacementsSpendExactlyONESpare is the same shape over
// the two-statement act, where a second winner would cost a physical plaque.
func TestPlaquesDB_TenConcurrentReplacementsSpendExactlyONESpare(t *testing.T) {
	f := newPlaqueFixture(t)
	old := f.load(t, f.tenantID, PlaqueActive, f.locationID)
	spareA := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)
	spareB := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)

	const n = 10
	var wg sync.WaitGroup
	var won, refused int32
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			successor := spareA
			if i%2 == 1 {
				successor = spareB
			}
			<-start
			_, err := f.plaques.Replace(context.Background(), ReplaceCommand{
				TenantID: f.tenantID, ActorID: f.actorID,
				RetiringUID: old, SuccessorUID: CanonicalUID(successor),
			})
			switch {
			case err == nil:
				atomic.AddInt32(&won, 1)
			case errors.Is(err, ErrPlaqueNotOnAWall), errors.Is(err, ErrPlaqueNotInStock):
				atomic.AddInt32(&refused, 1)
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if won != 1 {
		t.Fatalf("%d of %d replacements succeeded, want exactly 1 — two winners would "+
			"retire one plaque twice and spend two spares on one door", won, n)
	}
	if refused != n-1 {
		t.Fatalf("refusals = %d, want %d", refused, n-1)
	}
	// EXACTLY ONE SPARE IS SPENT. The other must still be in the box: a replacement
	// that half-succeeded would take a plaque out of stock and put it nowhere.
	inStock := 0
	for _, uid := range []string{spareA, spareB} {
		if status, wall, _, _ := f.row(t, uid); status == PlaqueUnassigned && wall == nil {
			inStock++
		}
	}
	if inStock != 1 {
		t.Fatalf("%d of the two spares are still in stock, want exactly 1", inStock)
	}
	if n := f.auditRows(t, ActionPlaqueRetired, old); n != 1 {
		t.Fatalf("plaque.retired rows = %d, want exactly 1", n)
	}
}

// TestPlaquesDB_UnmountingSomethingNotOnAWallIsRefused, in both shapes.
func TestPlaquesDB_UnmountingSomethingNotOnAWallIsRefused(t *testing.T) {
	f := newPlaqueFixture(t)
	box := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)
	retired := f.load(t, f.tenantID, PlaqueRetired, f.locationID)

	for _, tc := range []struct{ name, uid string }{
		{"already in stock", box},
		{"retired", retired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.plaques.Unmount(context.Background(), UnmountCommand{
				TenantID: f.tenantID, ActorID: f.actorID, UID: tc.uid,
			})
			if !errors.Is(err, ErrPlaqueNoWall) {
				t.Fatalf("Unmount = %v, want ErrPlaqueNoWall", err)
			}
			if n := f.auditRows(t, ActionPlaqueUnmounted, tc.uid); n != 0 {
				t.Fatalf("plaque.unmounted rows = %d after a refusal, want 0", n)
			}
		})
	}
	// §4.5: another business's plaque is not un-mountable either.
	theirs := f.load(t, f.foreignTenant, PlaqueActive, f.foreignVenue)
	if _, err := f.plaques.Unmount(context.Background(), UnmountCommand{
		TenantID: f.tenantID, ActorID: f.actorID, UID: theirs,
	}); !errors.Is(err, ErrUnknownPlaque) {
		t.Fatalf("un-mounting another business's plaque = %v, want ErrUnknownPlaque", err)
	}
}

// TestPlaquesDB_AnUnmountThatCannotWriteItsTrailRollsBack — the change must not
// outlive its record. `tags` has no updated_at, so a lost trail row means the plaque
// every tap at that door depends on moved with no record of who moved it.
func TestPlaquesDB_AnUnmountThatCannotWriteItsTrailRollsBack(t *testing.T) {
	f := newPlaqueFixture(t)
	uid := f.load(t, f.tenantID, PlaqueActive, f.locationID)

	broken, err := NewPlaques(f.data, brokenTrail{err: errors.New("trail down")}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewPlaques: %v", err)
	}
	if _, err := broken.Unmount(context.Background(), UnmountCommand{
		TenantID: f.tenantID, ActorID: f.actorID, UID: uid,
	}); err == nil {
		t.Fatal("Unmount with a broken trail returned nil")
	}
	if status, wall, _, _ := f.row(t, uid); status != PlaqueActive || wall == nil {
		t.Fatalf("row = (%s, %v) after a failed trail write, want it still on its wall",
			status, wall)
	}
}

// --- replacing -------------------------------------------------------------------

// TestPlaquesDB_ReplaceRetiresTheOldOneKeepsItAndBindsTheNewOne is the card's
// second criterion in one test: the new uid is registered on the wall, the old one
// is retired with retired_at and replaced_by, and the OLD ROW IS STILL THERE.
func TestPlaquesDB_ReplaceRetiresTheOldOneKeepsItAndBindsTheNewOne(t *testing.T) {
	f := newPlaqueFixture(t)
	old := f.load(t, f.tenantID, PlaqueActive, f.locationID)
	fresh := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)

	done, err := f.plaques.Replace(context.Background(), ReplaceCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		RetiringUID: old, SuccessorUID: CanonicalUID(fresh), VenueName: "St Julians",
	})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if done.Retired.UID != old || done.Mounted.UID != fresh {
		t.Fatalf("Replacement = (%s -> %s), want (%s -> %s)",
			done.Retired.UID, done.Mounted.UID, old, fresh)
	}

	status, wall, retiredAt, replacedBy := f.row(t, old)
	if status != PlaqueRetired {
		t.Errorf("old status = %q, want retired", status)
	}
	if retiredAt == nil {
		t.Errorf("retired_at is NULL; the card asks for the stamp")
	}
	if replacedBy == nil || *replacedBy != fresh {
		t.Errorf("replaced_by = %v, want %s", replacedBy, fresh)
	}
	// 🔴 THE RETIRED PLAQUE KEEPS ITS WALL, and 00013's
	// tags_retired_keeps_its_location is what makes that structural: "which door did
	// this plaque serve" is exactly the question a replacement history exists to
	// answer.
	if wall == nil || *wall != f.locationID {
		t.Errorf("retired plaque's wall = %v, want %s kept", wall, f.locationID)
	}

	newStatus, newWall, _, _ := f.row(t, fresh)
	if newStatus != PlaqueActive || newWall == nil || *newWall != f.locationID {
		t.Fatalf("successor = (%s, %v), want (active, %s) -- the SAME wall, read from "+
			"the plaque coming off rather than from the caller", newStatus, newWall, f.locationID)
	}
	if n := f.auditRows(t, ActionPlaqueRetired, old); n != 1 {
		t.Errorf("plaque.retired rows = %d, want 1", n)
	}
	if n := f.auditRows(t, ActionPlaqueMounted, fresh); n != 1 {
		t.Errorf("plaque.mounted rows = %d, want 1", n)
	}
	if d := f.auditDetail(t, ActionPlaqueMounted, fresh); !strings.Contains(d, old) {
		t.Errorf("the successor's trail row = %s, want it to name the plaque it replaced", d)
	}
}

// TestPlaquesDB_ReplaceREVALIDATESTheSuccessorItsTypeOnlyCLAIMSIsCanonical is N3.
//
// 🔴 THE LINE IT PINS WAS A DEFENCE NO TEST NOTICED. An audit deleted
// `PlaqueUID(string(c.SuccessorUID))` from Replace; it compiled and BOTH
// internal/domain/tenant and internal/handler stayed green — while the comment on
// that very line called it "THE REAL MECHANISM RATHER THAN THE TYPE … what refuses a
// bad value at RUN time". This repository's own rule: a defence whose removal no test
// notices is not a defence.
//
// 🔴 THE TYPE IS A CLAIM ABOUT PROVENANCE, NOT A PROOF OF CONTENT.
// CanonicalUID("b27188a6d32e49") is constructible by anything in this package — an
// audit built exactly that at the handler boundary and it compiled — so the run-time
// check is what stands between a lower-case successor and `replaced_by`.
//
// ⚠️ AND WITHOUT IT THE SQL WOULD STILL REFUSE THE WRITE, which is why the assertion
// is on the SENTENCE and not merely on the row count. RetireTagForReplacement carries
// `@replaced_by::text ~ '^[0-9A-F]{14}$'` on the UNCAST parameter, so the statement
// matches nothing — and the caller then gets an INTERNAL error about a retire that
// "matched no row" instead of "that is not a plaque id". Two layers, one of which
// tells the manager something true.
func TestPlaquesDB_ReplaceREVALIDATESTheSuccessorItsTypeOnlyCLAIMSIsCanonical(t *testing.T) {
	f := newPlaqueFixture(t)
	old := f.load(t, f.tenantID, PlaqueActive, f.locationID)

	_, err := f.plaques.Replace(context.Background(), ReplaceCommand{
		TenantID: f.tenantID, ActorID: f.actorID, RetiringUID: old,
		// A LOOKUP-legal, WRITE-illegal value, cast into the canonical type exactly as a
		// mistaken caller would.
		SuccessorUID: CanonicalUID("b27188a6d32e49"),
	})
	if !errors.Is(err, ErrPlaqueUID) {
		t.Fatalf("Replace with a lower-case successor = %v, want ErrPlaqueUID", err)
	}
	if status, _, _, replacedBy := f.row(t, old); status != PlaqueActive || replacedBy != nil {
		t.Fatalf("the plaque on the wall became (%s, replaced_by=%v); nothing may be "+
			"written when the successor is refused", status, replacedBy)
	}
	if n := f.auditRows(t, ActionPlaqueRetired, old); n != 0 {
		t.Fatalf("plaque.retired rows = %d after a refusal, want 0", n)
	}
	// THE POSITIVE CONTROL: the same call with a canonical successor goes through, so
	// the refusal above is about the VALUE and not about this fixture.
	fresh := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)
	if _, err := f.plaques.Replace(context.Background(), ReplaceCommand{
		TenantID: f.tenantID, ActorID: f.actorID, RetiringUID: old,
		SuccessorUID: CanonicalUID(fresh),
	}); err != nil {
		t.Fatalf("Replace with a canonical successor: %v", err)
	}
}

// TestPlaquesDB_ARetireThatCannotBindItsSuccessorWRITESNOTHING is the whole reason
// the two statements share a transaction.
//
// 🔴 THE FAILURE IS DRIVEN, NOT SIMULATED: the successor is already on a wall, so
// AssignTagToLocation's own `status = 'unassigned'` matches no row -- exactly what a
// second manager mounting that spare a moment earlier produces. If the halves ran
// separately, the door would now be served by a RETIRED plaque and every tap there
// would reject.
func TestPlaquesDB_ARetireThatCannotBindItsSuccessorWRITESNOTHING(t *testing.T) {
	f := newPlaqueFixture(t)
	old := f.load(t, f.tenantID, PlaqueActive, f.locationID)
	notSpare := f.load(t, f.tenantID, PlaqueActive, f.otherWall)

	_, err := f.plaques.Replace(context.Background(), ReplaceCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		RetiringUID: old, SuccessorUID: CanonicalUID(notSpare),
	})
	if !errors.Is(err, ErrPlaqueNotInStock) {
		t.Fatalf("Replace with a successor that is not stock = %v, want ErrPlaqueNotInStock", err)
	}
	status, wall, retiredAt, replacedBy := f.row(t, old)
	if status != PlaqueActive || retiredAt != nil || replacedBy != nil {
		t.Fatalf("the plaque on the wall became (%s, retired_at=%v, replaced_by=%v): the "+
			"retirement did NOT roll back, so a failed replacement left an entrance "+
			"nobody can tap at", status, retiredAt, replacedBy)
	}
	if wall == nil || *wall != f.locationID {
		t.Fatalf("wall = %v, want unchanged", wall)
	}
	if n := f.auditRows(t, ActionPlaqueRetired, old); n != 0 {
		t.Fatalf("plaque.retired rows = %d after a failed replacement, want 0 -- the "+
			"trail must share the write's fate", n)
	}
}

// TestPlaquesDB_ATrailFailureRollsTheWholeReplacementBack. The other direction of
// the same guarantee: the CHANGE must not survive without its record.
func TestPlaquesDB_ATrailFailureRollsTheWholeReplacementBack(t *testing.T) {
	f := newPlaqueFixture(t)
	old := f.load(t, f.tenantID, PlaqueActive, f.locationID)
	fresh := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)

	broken, err := NewPlaques(f.data, brokenTrail{err: errors.New("trail down")}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewPlaques: %v", err)
	}
	if _, err := broken.Replace(context.Background(), ReplaceCommand{
		TenantID: f.tenantID, ActorID: f.actorID,
		RetiringUID: old, SuccessorUID: CanonicalUID(fresh),
	}); err == nil {
		t.Fatal("Replace with a broken trail returned nil; the write must fail with it")
	}
	if status, _, _, _ := f.row(t, old); status != PlaqueActive {
		t.Fatalf("old plaque = %q after a failed trail write, want active -- `tags` has "+
			"no updated_at, so a change that outlives its trail leaves NO record anywhere", status)
	}
	if status, wall, _, _ := f.row(t, fresh); status != PlaqueUnassigned || wall != nil {
		t.Fatalf("successor = (%s, %v), want (unassigned, nil)", status, wall)
	}
}

// TestPlaquesDB_ReplacingSomethingThatIsNotOnAWallIsRefused, and a plaque cannot
// replace itself.
func TestPlaquesDB_ReplacingSomethingThatIsNotOnAWallIsRefused(t *testing.T) {
	f := newPlaqueFixture(t)
	box := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)
	spare := f.load(t, f.tenantID, PlaqueUnassigned, uuid.Nil)

	_, err := f.plaques.Replace(context.Background(), ReplaceCommand{
		TenantID: f.tenantID, ActorID: f.actorID, RetiringUID: box, SuccessorUID: CanonicalUID(spare),
	})
	if !errors.Is(err, ErrPlaqueNotOnAWall) {
		t.Fatalf("replacing a plaque that is in stock = %v, want ErrPlaqueNotOnAWall", err)
	}

	onWall := f.load(t, f.tenantID, PlaqueActive, f.locationID)
	_, err = f.plaques.Replace(context.Background(), ReplaceCommand{
		TenantID: f.tenantID, ActorID: f.actorID, RetiringUID: onWall, SuccessorUID: CanonicalUID(onWall),
	})
	if !errors.Is(err, ErrSamePlaque) {
		t.Fatalf("a plaque replacing itself = %v, want ErrSamePlaque", err)
	}
	if status, _, _, _ := f.row(t, onWall); status != PlaqueActive {
		t.Fatalf("status = %q after a self-replacement was refused, want active", status)
	}
}

// TestPlaquesDB_TheRowIsNeverDeletedByThisRole is the card's "the old row is not
// deleted" criterion asserted as a GRANT rather than as a habit: nothing in this
// package deletes, and the role could not if it tried.
func TestPlaquesDB_TheRowIsNeverDeletedByThisRole(t *testing.T) {
	f := newPlaqueFixture(t)
	uid := f.load(t, f.tenantID, PlaqueRetired, f.locationID)

	err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM tags WHERE tenant_id = $1 AND uid = $2`, f.tenantID, uid)
		return e
	})
	if err == nil {
		t.Fatal("tappa_app deleted a plaque row; historical transactions resolve through " +
			"tag_uid and must keep resolving (§4.3, §4.6)")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("DELETE failed with %v, want a privilege refusal", err)
	}
	if status, _, _, _ := f.row(t, uid); status != PlaqueRetired {
		t.Fatalf("row is gone or changed after the refused DELETE (status=%q)", status)
	}
}

// TestPlaques_ACheckViolationIsASENTENCEnotACRASH pins the 23514 mapping, and it
// is a UNIT test on the classifier because the state it protects against can no
// longer be CREATED.
//
// 🔴 THE MEASUREMENT THAT DECIDED THE SHAPE OF THIS TEST, 2026-08-09. Migration
// 00013 shipped tags_uid_canonical_hex as NOT VALID, so 18 010 pre-existing
// lower-case rows survive and are FROZEN -- a CHECK is evaluated on every UPDATE,
// so any write to them answers 23514. But a CHECK binds EVERY role, so a new one
// cannot be inserted even as tappa_owner (measured: `INSERT ... 'abc...'` ->
// "new row for relation \"tags\" violates check constraint
// \"tags_uid_canonical_hex\" (SQLSTATE 23514)"). The frozen rows that exist belong
// to tenants a fresh fixture cannot see, and RLS is what stops this test reaching
// them -- correctly.
//
// So the state is REAL, UNREACHABLE FROM A FIXTURE, and the honest thing to pin is
// the MAPPING: without it a manager on a development database meets a 500 on a row
// their own list showed them. ListTagsForTenant returns those rows like any other.
func TestPlaques_ACheckViolationIsASENTENCEnotACRASH(t *testing.T) {
	violation := &pgconn.PgError{Code: "23514", ConstraintName: "tags_uid_canonical_hex"}

	// classifyMount and classifyRetire reach the store ONLY on the pgx.ErrNoRows
	// arm, so a nil Queries is safe here and keeps this a unit test.
	if err := classifyMount(context.Background(), nil, uuid.New(), "AABBCCDDEEFF01", violation); !errors.Is(err, ErrPlaqueFrozen) {
		t.Errorf("classifyMount(23514) = %v, want ErrPlaqueFrozen", err)
	}
	if err := classifyRetire(context.Background(), nil, uuid.New(), "AABBCCDDEEFF01", violation); !errors.Is(err, ErrPlaqueFrozen) {
		t.Errorf("classifyRetire(23514) = %v, want ErrPlaqueFrozen", err)
	}
	// ANTI-VACUITY: a classifier that answered ErrPlaqueFrozen for EVERYTHING would
	// pass the two assertions above and would be useless. A foreign-key violation is
	// a different sentence and must stay one.
	fk := &pgconn.PgError{Code: "23503", ConstraintName: "tags_location_fk"}
	if err := classifyMount(context.Background(), nil, uuid.New(), "AABBCCDDEEFF01", fk); !errors.Is(err, ErrUnknownVenue) {
		t.Errorf("classifyMount(23503) = %v, want ErrUnknownVenue", err)
	}
}

// TestPlaques_TheTwoBoundariesAnswerTwoDIFFERENTQuestions.
//
// 🔴 THERE ARE TWO FUNCTIONS AND COLLAPSING THEM REOPENED PHASE A's DEFECT. The
// first version had one, which upper-cased — and it was measured making the section
// answer "that plaque id is not one of this business's" about a row the same page
// had just listed, because `tags.uid` is char(14), case-SENSITIVE, and the table
// still holds 18 010 lower-case rows that predate 00013.
//
//	PlaqueUID   may this be WRITTEN?   canonical only. db/queries/tags.sql's own
//	            predicate on @replaced_by is `~ '^[0-9A-F]{14}$'` on the UNCAST
//	            parameter, and 00013 demands the same of every new row.
//	PlaqueRef   may this be LOOKED UP?  either case, PRESERVED, because the row a
//	            manager just saw is the row the link must open.
//
// 🔴 BOTH STILL BOUND THE SHAPE, and that is the part neither may drop: measured in
// db/queries/tags.sql, `'AABBCCDDEEFF01ZZZZZZ'::char(14)` becomes 'AABBCCDDEEFF01'
// with NO error — so an unbounded value does not fail, it becomes a DIFFERENT,
// possibly real plaque, and the self-FK then accepts it.
func TestPlaques_TheTwoBoundariesAnswerTwoDIFFERENTQuestions(t *testing.T) {
	tests := []struct {
		name, in string
		// wantUID is what PlaqueUID answers ("" = refused); wantRef is PlaqueRef's.
		wantUID, wantRef string
	}{
		{name: "canonical", in: "AABBCCDDEEFF01",
			wantUID: "AABBCCDDEEFF01", wantRef: "AABBCCDDEEFF01"},
		{
			// 🔴 THE ROW THE PANEL COULD NOT OPEN. A lower-case uid may never be
			// WRITTEN — and must be LOOKABLE-UP, exactly as stored.
			name: "lower case: refused for writing, preserved for looking up",
			in:   "b27188a6d32e49", wantUID: "", wantRef: "b27188a6d32e49",
		},
		{name: "mixed case", in: "EF6A9A125C36Ab", wantUID: "", wantRef: "EF6A9A125C36Ab"},
		{name: "surrounding space", in: "  AABBCCDDEEFF01\n",
			wantUID: "AABBCCDDEEFF01", wantRef: "AABBCCDDEEFF01"},
		{name: "over-long: the truncation case", in: "AABBCCDDEEFF01ZZZZZZ"},
		{name: "over-long but all hex", in: "AABBCCDDEEFF0199"},
		{name: "too short", in: "AABBCCDDEEFF0"},
		{name: "not hex", in: "AABBCCDDEEFFZZ"},
		{name: "empty", in: ""},
		{name: "a uuid, not a plaque id", in: uuid.New().String()},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := PlaqueUID(tc.in)
			switch {
			case tc.wantUID == "":
				if !errors.Is(err, ErrPlaqueUID) {
					t.Errorf("PlaqueUID(%q) = (%q, %v), want ErrPlaqueUID", tc.in, got, err)
				}
			case err != nil || string(got) != tc.wantUID:
				t.Errorf("PlaqueUID(%q) = (%q, %v), want %q", tc.in, got, err, tc.wantUID)
			}

			ref, err := PlaqueRef(tc.in)
			switch {
			case tc.wantRef == "":
				if !errors.Is(err, ErrPlaqueUID) {
					t.Errorf("PlaqueRef(%q) = (%q, %v), want ErrPlaqueUID", tc.in, ref, err)
				}
			case err != nil || ref != tc.wantRef:
				t.Errorf("PlaqueRef(%q) = (%q, %v), want %q EXACTLY as typed", tc.in, ref, err, tc.wantRef)
			}
		})
	}
	// ANTI-VACUITY: the two really do differ somewhere, or this whole table is one
	// function tested twice.
	if _, err := PlaqueUID("b27188a6d32e49"); err == nil {
		t.Fatal("PlaqueUID accepted a lower-case uid; the two boundaries have collapsed")
	}
	if ref, err := PlaqueRef("b27188a6d32e49"); err != nil || ref != "b27188a6d32e49" {
		t.Fatalf("PlaqueRef folded or refused a lower-case uid (%q, %v); the row a "+
			"manager sees would be unopenable", ref, err)
	}
}

// --- section 4.7: the type wall --------------------------------------------------

// TestPlaquesDB_NoTypeOnThisPathCanCarryAKey requires that none of the types this
// package hands to the panel COULD hold a plaque's key.
//
// 🔴 IT IS TWO ALLOW-LISTS, NOT A DENYLIST, AND THE PREVIOUS VERSION WAS THE LATTER
// AND WAS BEATEN IN THREE SHAPES. That version failed on a `[]byte` and on a field
// whose NAME matched a forbidden word; an audit added, and it passed:
//
//	Material [44]byte        the envelope's EXACT length — an ARRAY, not a slice
//	Ref string               a neutral name, holding whatever a caller puts in it
//	Extras map[string][]byte the byte slice one indirection away, behind a map
//
// This repository's own lesson, already written down for audit_log.detail: a
// forbidden-string scan protects against what you thought of, an allow-list protects
// against what you did not. So:
//
//	THE FIELD NAMES are enumerated per type. A new field fails until somebody adds
//	it here, which is the moment to ask what it carries.
//	THE FIELD TYPES are enumerated by KIND, so a shape nobody named — an array, a
//	map, an interface, a channel — fails on the shape alone.
//
// ⚠️ WHAT IS STILL NOT COVERED, counted rather than implied:
//   - the RENDER types (web/templates/components, web/templates/pages). A domain
//     package cannot import the render layer, so its twin lives in internal/handler:
//     TestPlaqueViewModels_CannotCarryAKey.
//   - internal/sun, which legitimately reads the key through resolve_tag_by_uid to
//     verify a CMAC. It is on the far side of this wall by design.
//   - a caller reaching store.Queries directly. Nothing in the panel does, and the
//     SQL half below is what makes that safe rather than merely true today.
//   - a key ENCODED INTO A PERMITTED FIELD — hex in a string, base64 in a name.
//     No type system refuses that. What refuses it is that no query on this path
//     SELECTS the column at all (the test below), so there is nothing to encode.
func TestPlaquesDB_NoTypeOnThisPathCanCarryAKey(t *testing.T) {
	fields := map[string][]string{
		"tenant.Plaque": {"UID", "Status", "LocationID", "LastCtr", "RetiredAt",
			"ReplacedBy", "Replaces", "LastSeen", "CreatedAt", "Canonical"},
		"tenant.PlaqueScreen": {"Plaques", "Capped", "Zone"},
		"tenant.Replacement":  {"Retired", "Mounted"},
		"tenant.PlaqueEvent":  {"Action", "At", "ActorName", "BySystem"},
		// 🔴 THE COMMAND TYPES ARE ROOTS TOO (N4). An audit added `Aud4Key []byte` and
		// `Aud4Blob [44]byte` to UnmountCommand and the wall stayed green: the test was
		// called "NoTypeOnThisPath" while its roots were only the READ types. A command
		// travels the same path in the other direction — handler to domain — so a field
		// on one is exactly as reachable.
		"tenant.MountCommand":   {"TenantID", "ActorID", "UID", "LocationID", "VenueName"},
		"tenant.UnmountCommand": {"TenantID", "ActorID", "UID", "VenueName"},
		"tenant.ReplaceCommand": {"TenantID", "ActorID", "RetiringUID", "SuccessorUID", "VenueName"},
	}
	seen := map[string]bool{}
	for _, root := range []struct {
		name string
		ty   reflect.Type
	}{
		{"tenant.Plaque", reflect.TypeOf(Plaque{})},
		{"tenant.PlaqueScreen", reflect.TypeOf(PlaqueScreen{})},
		{"tenant.Replacement", reflect.TypeOf(Replacement{})},
		{"tenant.PlaqueEvent", reflect.TypeOf(PlaqueEvent{})},
		{"tenant.MountCommand", reflect.TypeOf(MountCommand{})},
		{"tenant.UnmountCommand", reflect.TypeOf(UnmountCommand{})},
		{"tenant.ReplaceCommand", reflect.TypeOf(ReplaceCommand{})},
	} {
		walkKeylessType(t, root.name, root.ty, fields, seen)
	}
	// ANTI-VACUITY: every named type must really have been visited, or the field
	// lists above are describing nothing.
	for name := range fields {
		if !seen[name] {
			t.Errorf("%s is described here but the walk never reached it", name)
		}
	}
}

// walkKeylessType is the shared walker. It is duplicated in internal/handler over
// the render types, and the duplication is NAMED rather than hidden: sharing it
// would mean a domain package importing the render layer or a third package for
// forty lines.
//
// permitted names come from `fields`; a type with no entry is walked for SHAPE only,
// which is what lets the render copy walk a whole section view without enumerating
// somebody else's model.
func walkKeylessType(t *testing.T, path string, ty reflect.Type, fields map[string][]string, seen map[string]bool) {
	t.Helper()
	for ty.Kind() == reflect.Ptr {
		ty = ty.Elem()
	}
	// THE TWO TYPES THAT ARE PERMITTED BY IDENTITY, and uuid.UUID is the reason the
	// array rule below cannot be "no arrays at all": it IS a [16]byte. Permitting it
	// by identity rather than by shape is what keeps [44]byte — the envelope's exact
	// length — refused.
	if ty == reflect.TypeOf(uuid.UUID{}) || ty == reflect.TypeOf(time.Time{}) ||
		ty == reflect.TypeOf(time.Location{}) {
		return
	}
	switch ty.Kind() {
	case reflect.Bool, reflect.String,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return
	case reflect.Slice:
		if ty.Elem().Kind() == reflect.Uint8 {
			t.Errorf("%s is a []byte: a plaque's KEK-wrapped key is a []byte (§4.7)", path)
			return
		}
		walkKeylessType(t, path+"[]", ty.Elem(), fields, seen)
		return
	case reflect.Struct:
		// fall through
	default:
		// 🔴 EVERYTHING ELSE IS REFUSED BY SHAPE: an ARRAY (however long), a MAP, an
		// INTERFACE, a channel, a function, a uintptr. Each of those was, or could
		// carry, one of the three shapes that beat the denylist.
		t.Errorf("%s is a %s; only named scalars, structs and non-byte slices are "+
			"permitted on this path, because every other shape can carry bytes (§4.7)",
			path, ty.Kind())
		return
	}

	if want, ok := fields[path]; ok {
		seen[path] = true
		got := make([]string, 0, ty.NumField())
		for i := 0; i < ty.NumField(); i++ {
			got = append(got, ty.Field(i).Name)
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s has fields %v, want exactly %v — a field here is a deliberate "+
				"decision, so an addition must be argued for rather than inherited (§4.7)",
				path, got, want)
		}
	}
	for i := 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		walkKeylessType(t, path+"."+f.Name, f.Type, fields, seen)
	}
}

// TestPlaquesDB_NoShippedTagQuerySelectsTheKey is the OTHER half of the wall, and
// it reads db/queries/tags.sql rather than trusting the types: a query that selected
// aes_key_ref would put a wrapped key into template data, log lines and HTTP
// responses for a value no pixel renders.
//
// 🔴 THE RESOLVER IS THE ONE PATH THAT LEGITIMATELY READS IT, and it is NOT in this
// file -- it is a SECURITY DEFINER function in migration 00004, reached only by
// internal/sun. That separation is why this assertion can be absolute here.
func TestPlaquesDB_NoShippedTagQuerySelectsTheKey(t *testing.T) {
	raw, err := os.ReadFile("../../../db/queries/tags.sql")
	if err != nil {
		t.Fatalf("read db/queries/tags.sql: %v", err)
	}
	sql := string(raw)
	// ANTI-VACUITY: the file must be the one this package uses.
	for _, name := range []string{"ListTagsForTenant", "GetTagForTenant",
		"AssignTagToLocation", "RetireTagForReplacement", "ListTagLastSeen"} {
		if !strings.Contains(sql, name) {
			t.Fatalf("db/queries/tags.sql does not declare %s; this test is reading the wrong file", name)
		}
	}
	for _, line := range strings.Split(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue // a comment may NAME the column; that is how the rule is explained
		}
		if strings.Contains(trimmed, "aes_key_ref") {
			t.Fatalf("a shipped tag query selects aes_key_ref: %q", trimmed)
		}
	}
}
