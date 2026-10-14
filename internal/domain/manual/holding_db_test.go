package manual

// holding_db_test.go -- M11 API-1c: a manager's record at a holding PEER's venue (ADR 0027 §9
// "Manuel kayıt"; K-1a, K-33). Real Postgres; the holding is written by the owner (ADR 0027 §1).

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

type manualWorld struct {
	t                 *testing.T
	ctx               context.Context
	owner             *pgx.Conn
	rec               *Recorder
	km, kf, c, d      uuid.UUID
	h, h2             uuid.UUID
	kfVenue, cVenue   uuid.UUID
	kmAdmin, employee uuid.UUID
}

func newManualWorld(t *testing.T) *manualWorld {
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
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := NewRecorder(data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	w := &manualWorld{t: t, ctx: ctx, owner: owner, rec: rec, km: uuid.New(), kf: uuid.New(), c: uuid.New(), d: uuid.New(),
		kmAdmin: uuid.New(), employee: uuid.New()}
	for _, id := range []uuid.UUID{w.km, w.kf, w.c, w.d} {
		w.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'api1c manual', $2, 'restaurant', 'multi')`,
			id, "VAT-API1C-M-"+id.String())
	}
	for _, h := range []*uuid.UUID{&w.h, &w.h2} {
		if err := owner.QueryRow(ctx, `INSERT INTO holdings (name) VALUES ('api1c manual') RETURNING id`).Scan(h); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = owner.Exec(bg, `DELETE FROM holding_members WHERE holding_id = ANY($1)`, []uuid.UUID{w.h, w.h2})
		_, _ = owner.Exec(bg, `DELETE FROM holdings WHERE id = ANY($1)`, []uuid.UUID{w.h, w.h2})
	})
	w.exec(`INSERT INTO holding_members (holding_id, member_tenant_id) VALUES ($1, $2), ($1, $3), ($4, $5), ($4, $6)`, w.h, w.km, w.kf, w.h2, w.c, w.d)
	for _, v := range []struct {
		tenant uuid.UUID
		dst    *uuid.UUID
	}{{w.kf, &w.kfVenue}, {w.c, &w.cVenue}} {
		if err := owner.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'api1c manual venue') RETURNING id`, v.tenant).Scan(v.dst); err != nil {
			t.Fatal(err)
		}
	}
	var home uuid.UUID
	if err := owner.QueryRow(ctx, `INSERT INTO locations (tenant_id, name) VALUES ($1, 'api1c KM home') RETURNING id`, w.km).Scan(&home); err != nil {
		t.Fatal(err)
	}
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Owner', $3, $4, 'owner')`,
		w.kmAdmin, w.km, "owner-"+w.kmAdmin.String()[:12]+"@example.test", "$2a$12$"+strings.Repeat("h", 53))
	w.exec(`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at) VALUES ($1, $2, $3, 'KM Staff', 'active', now())`,
		w.employee, w.km, home)
	return w
}

func (w *manualWorld) exec(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.owner.Exec(w.ctx, sql, args...); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
}

func (w *manualWorld) entry(site uuid.UUID) Entry {
	return Entry{TenantID: w.km, EmployeeID: w.employee, EnteredBy: w.kmAdmin, Direction: In,
		At: time.Now().Add(-2 * time.Hour), SiteLocationID: site}
}

// TestHoldingManual_RecordAtAPeersVenue (ADR 0027 §9; plan §5 API-1c "manuel saha kaydı"): KM's
// manager files a record at KF's venue -- the form offers it (Subject.PartnerVenues: KF's venue,
// never C's), the row is KM's, names KF and the venue, carries no local venue and no plaque, the
// trail says partner_venue -- and KF sees it as employer_manual.
func TestHoldingManual_RecordAtAPeersVenue(t *testing.T) {
	w := newManualWorld(t)
	s, err := w.rec.Subject(w.ctx, w.km, w.employee)
	if err != nil {
		t.Fatal(err)
	}
	if s.PartnerVenueName(w.kfVenue) == "" || s.PartnerVenueName(w.cVenue) != "" {
		t.Errorf("the partner venues offered: %+v -- want KF's and never C's (K-33)", s.PartnerVenues)
	}
	got, err := w.rec.Record(w.ctx, w.entry(w.kfVenue))
	if err != nil {
		t.Fatalf("record at KF's venue: %v", err)
	}
	var (
		tenant, site, venue uuid.UUID
		local               *uuid.UUID
		tag                 *string
		channel, outcome    string
		partner             bool
	)
	if err := w.owner.QueryRow(w.ctx, `SELECT tenant_id, site_tenant_id, site_location_id, location_id, site_tag_uid, channel FROM transactions WHERE id = $1`, got.ID).
		Scan(&tenant, &site, &venue, &local, &tag, &channel); err != nil {
		t.Fatal(err)
	}
	if tenant != w.km || site != w.kf || venue != w.kfVenue || local != nil || tag != nil || channel != "manual" {
		t.Errorf("the row: tenant %s site %s venue %s local %v tag %v channel %s", tenant, site, venue, local, tag, channel)
	}
	if err := w.owner.QueryRow(w.ctx, `SELECT outcome FROM site_punches WHERE transaction_id = $1 AND tenant_id = $2`, got.ID, w.kf).Scan(&outcome); err != nil || outcome != "employer_manual" {
		t.Errorf("KF's projection: %q, %v -- want employer_manual", outcome, err)
	}
	if err := w.owner.QueryRow(w.ctx, `SELECT (detail ->> 'partner_venue')::boolean FROM audit_log WHERE id = $1`, got.AuditID).Scan(&partner); err != nil || !partner {
		t.Errorf("the trail does not say partner_venue: %v %v", partner, err)
	}
}

// TestHoldingManual_PeerManagerCannotNameAnOutsiderLocationThroughThePanel is K-33 on the panel's
// path (plan §5 API-1c; the database half is TestHoldingManual_PeerManagerCannotNameAnOutsiderLocation
// and TestSitePunches_OutsiderSeesNothing): KM's record naming C's venue -- C is in H2 -- is
// ErrVenue, nothing written, C's projection empty. And after KM LEFT the holding, KF's venue is
// ErrVenue too (manual is strict -- ADR 0027 §6): the domain's read finds no peer venue.
func TestHoldingManual_PeerManagerCannotNameAnOutsiderLocationThroughThePanel(t *testing.T) {
	w := newManualWorld(t)
	before := w.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1`, w.km)
	if _, err := w.rec.Record(w.ctx, w.entry(w.cVenue)); !errors.Is(err, ErrVenue) {
		t.Errorf("C's venue: %v, want ErrVenue", err)
	}
	if n := w.count(`SELECT count(*) FROM site_punches WHERE tenant_id = $1`, w.c); n != 0 {
		t.Errorf("C's projection holds %d rows", n)
	}
	w.exec(`UPDATE holding_members SET left_at = clock_timestamp() WHERE member_tenant_id = $1 AND left_at IS NULL`, w.km)
	if _, err := w.rec.Record(w.ctx, w.entry(w.kfVenue)); !errors.Is(err, ErrVenue) {
		t.Errorf("KF's venue after KM left: %v, want ErrVenue", err)
	}
	if n := w.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1`, w.km); n != before {
		t.Errorf("%d rows written by refused records", n-before)
	}
}

func (w *manualWorld) count(sql string, args ...any) int64 {
	w.t.Helper()
	var n int64
	if err := w.owner.QueryRow(w.ctx, sql, args...).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}
