package tenant

// holdingsite_db_test.go -- M11 API-1c: the SITE's two readers in this package (ADR 0027 §9) -- a
// plaque's "last seen" fed by a holding peer's staff's taps (by the SERVER's clock), and a venue
// delete that counts the peer's taps from the site's projection (with the peer's RESTRICT key as
// the last belt). Real Postgres; the holding is written by the owner (ADR 0027 §1).

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

type siteFixture struct {
	t      *testing.T
	ctx    context.Context
	data   *db.DB
	owner  *pgx.Conn
	trail  *audit.Recorder
	kf, km uuid.UUID
	holdID uuid.UUID
	admin  uuid.UUID // KF's
	emp    uuid.UUID // KM's
}

func newSiteFixture(t *testing.T) *siteFixture {
	t.Helper()
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres; the holding is written by the owner)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	data, err := db.New(ctx, &config.Config{DatabaseURL: appDSN})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(data.Close)
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	f := &siteFixture{t: t, ctx: ctx, data: data, owner: owner, trail: trail, kf: uuid.New(), km: uuid.New(), admin: uuid.New(), emp: uuid.New()}
	for _, id := range []uuid.UUID{f.kf, f.km} {
		f.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'api1c site', $2, 'restaurant', 'multi')`,
			id, "VAT-API1C-T-"+id.String())
	}
	if err := owner.QueryRow(ctx, `INSERT INTO holdings (name) VALUES ('api1c tenant') RETURNING id`).Scan(&f.holdID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = owner.Exec(bg, `DELETE FROM holding_members WHERE holding_id = $1`, f.holdID)
		_, _ = owner.Exec(bg, `DELETE FROM holdings WHERE id = $1`, f.holdID)
	})
	f.exec(`INSERT INTO holding_members (holding_id, member_tenant_id) VALUES ($1, $2), ($1, $3)`, f.holdID, f.kf, f.km)
	f.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Owner', $3, $4, 'owner')`,
		f.admin, f.kf, "owner-"+f.admin.String()[:12]+"@example.test", "$2a$12$"+strings.Repeat("h", 53))
	f.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, $3, 'KM Staff', 'active', now())`,
		f.emp, f.km, "ext-"+f.emp.String()[:8])
	return f
}

func (f *siteFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.owner.Exec(f.ctx, sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

func (f *siteFixture) venue(name string) uuid.UUID {
	f.t.Helper()
	var id uuid.UUID
	if err := f.owner.QueryRow(f.ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, $2) RETURNING id`, f.kf, name).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *siteFixture) plaque(venue uuid.UUID) string {
	f.t.Helper()
	uid := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:14])
	f.exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 1000, 'active')`,
		uid, f.kf, venue, make([]byte, 44))
	return uid
}

// tap writes KM's employee's row at KF's venue / plaque, as the owner (the gate and the projection
// fire for every role), declaring occurredAt.
func (f *siteFixture) tap(venue uuid.UUID, uid, verdict string, occurredAt time.Time) {
	f.t.Helper()
	var kind any = "in"
	if verdict == "ignored" || verdict == "reject" {
		kind = nil
	}
	f.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, site_tenant_id, site_location_id, site_tag_uid)
	        VALUES ($1, $2, $3, $4, 'nfc', $5, true, 1000, $6, $7, $8)`, f.km, f.emp, occurredAt, verdict, kind, f.kf, venue, uid)
}

// TestSitePunches_LastSeenIgnoresDeclaredTime (ADR 0027 §9, 11th round: "tek bir sahte URL 'son
// görülme'yi kalıcı olarak 2099'a çekebiliyordu"; plan §5 API-1c): a KM employee's tap on KF's
// plaque DECLARES 2099 -- KF's plaque screen says it was last seen NOW (the server's clock,
// site_punches.created_at), and a second tap declaring 2001 does not move it back either.
// MUTANT (measured on the projection's own column): ordering by occurred_at would print 2099.
func TestSitePunches_LastSeenIgnoresDeclaredTime(t *testing.T) {
	f := newSiteFixture(t)
	v := f.venue("api1c last seen")
	uid := f.plaque(v)
	f.tap(v, uid, "ok", time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC))
	f.tap(v, uid, "ok", time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
	plaques, err := NewPlaques(f.data, f.trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	screen, err := plaques.Screen(f.ctx, f.kf)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range screen.Plaques {
		if p.UID != uid {
			continue
		}
		if p.LastSeen == nil {
			t.Fatal("KF's plaque a peer's staff tapped has no 'last seen'")
		}
		if d := time.Since(*p.LastSeen); d < -time.Minute || d > time.Hour {
			t.Errorf("last seen %v -- a declared time reached the screen, not the server's clock", *p.LastSeen)
		}
		return
	}
	t.Fatal("KF's plaque is not on its screen")
}

// TestLocationDelete_CountsHoldingReferences (ADR 0027 §9 "Lokasyon silme"; plan §5 API-1c
// "lokasyon silme holding referansını sayar"): KF's venue where a KM employee tapped counts the tap
// in its references (HoldingTaps, from KF's projection -- KF cannot read KM's row) and the delete
// is refused as in use, the venue kept. A second venue whose only peer row is an 'ignored' repeat
// (the projection does not carry one) counts 0 -- and the delete is STILL refused: the peer's row
// holds the venue by its RESTRICT key, the last belt (ErrVenueInUse, not an error).
func TestLocationDelete_CountsHoldingReferences(t *testing.T) {
	f := newSiteFixture(t)
	venues, err := NewVenues(f.data, f.trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	counted := f.venue("api1c counted")
	f.tap(counted, f.plaque(counted), "ok", time.Now())
	refs, err := venues.VenueReferences(f.ctx, f.kf, counted)
	if err != nil {
		t.Fatal(err)
	}
	if refs.HoldingTaps != 1 || !refs.InUse() {
		t.Errorf("references %+v, want 1 holding tap and in use", refs)
	}
	ignored := f.venue("api1c ignored only")
	f.tap(ignored, f.plaque(ignored), "ignored", time.Now())
	for name, v := range map[string]uuid.UUID{"a counted tap": counted, "an ignored repeat only": ignored} {
		// The plaque is the venue's own reference; it is moved to stock first so that only the
		// peer's row holds the venue.
		f.exec(`UPDATE tags SET location_id = NULL, status = 'unassigned' WHERE location_id = $1`, v)
		err := venues.DeleteVenue(f.ctx, DeleteCommand{TenantID: f.kf, ID: v, ActorID: f.admin})
		if !errors.Is(err, ErrVenueInUse) {
			t.Errorf("%s: delete gave %v, want ErrVenueInUse", name, err)
		}
		var n int
		if err := f.owner.QueryRow(f.ctx, `SELECT count(*) FROM locations WHERE id = $1`, v).Scan(&n); err != nil || n != 1 {
			t.Errorf("%s: the venue is gone (%d, %v)", name, n, err)
		}
	}
}
