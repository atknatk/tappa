package ledger

// holding_db_test.go -- M11 API-1c: the EMPLOYER's nine readers name a row taken at a holding
// PEER's venue (ADR 0027 §9, "İşverenin okuyucuları"), and the SITE's report lists the peer's
// staff without hours. Real Postgres: the holding and its memberships are written by the owner
// (only op_* and the owner write them -- ADR 0027 §1), every reader runs on the application pool
// as tappa_app in the reading tenant's context, exactly as the panel does.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/store"
)

// hWorld is KF and KM, peers in H; C and D in H2. Each of KF and KM has a venue with a 09:00-17:00
// shift, a plaque there and three employees.
type hWorld struct {
	t     *testing.T
	ctx   context.Context
	data  *db.DB
	owner *pgx.Conn
	r     *Reader

	kf, km, c, d uuid.UUID
	h, h2        uuid.UUID
	venue        map[uuid.UUID]uuid.UUID // tenant -> its venue
	venueName    map[uuid.UUID]string
	plaque       map[uuid.UUID]string // tenant -> its active plaque
	staff        map[uuid.UUID][3]uuid.UUID
	admin        map[uuid.UUID]uuid.UUID
}

func newHWorld(t *testing.T) *hWorld {
	t.Helper()
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres; the holding is written by the owner -- ADR 0027 §1)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	data, err := db.New(ctx, &config.Config{DatabaseURL: appDSN})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	r, err := NewReader(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := &hWorld{t: t, ctx: ctx, data: data, owner: owner, r: r,
		venue: map[uuid.UUID]uuid.UUID{}, venueName: map[uuid.UUID]string{}, plaque: map[uuid.UUID]string{},
		staff: map[uuid.UUID][3]uuid.UUID{}, admin: map[uuid.UUID]uuid.UUID{}}
	w.kf, w.km, w.c, w.d = w.tenant("KF"), w.tenant("KM"), w.tenant("C"), w.tenant("D")
	w.h, w.h2 = w.holding(), w.holding()
	t.Cleanup(func() {
		bg := context.Background()
		if _, err := owner.Exec(bg, `DELETE FROM holding_members WHERE holding_id = ANY($1)`, []uuid.UUID{w.h, w.h2}); err != nil {
			t.Errorf("cleanup memberships: %v", err)
		}
		if _, err := owner.Exec(bg, `DELETE FROM holdings WHERE id = ANY($1)`, []uuid.UUID{w.h, w.h2}); err != nil {
			t.Errorf("cleanup holdings: %v", err)
		}
	})
	w.exec(`INSERT INTO holding_members (holding_id, member_tenant_id) VALUES ($1, $2), ($1, $3), ($4, $5), ($4, $6)`,
		w.h, w.kf, w.km, w.h2, w.c, w.d)
	for _, tn := range []uuid.UUID{w.kf, w.km} {
		name := "api1c venue " + uuid.NewString()[:6]
		var v uuid.UUID
		if err := owner.QueryRow(ctx, `INSERT INTO locations (tenant_id, name, static_ips, shift_start, shift_end)
		                               VALUES ($1, $2, '{203.0.113.0/24}', '09:00', '17:00') RETURNING id`, tn, name).Scan(&v); err != nil {
			t.Fatal(err)
		}
		w.venue[tn], w.venueName[tn] = v, name
		w.plaque[tn] = w.tag(tn, v)
		w.staff[tn] = [3]uuid.UUID{w.employee(tn), w.employee(tn), w.employee(tn)}
		w.admin[tn] = w.newAdmin(tn)
	}
	return w
}

// exec runs one statement as the owner. A transactions row is written in its tenant's context
// (the first argument) -- the product's shape: since M11 API-2 the own-tenant feed trigger
// (migration 00049, punch_feed) asserts that an event-worthy row's tenant is the context's.
func (w *hWorld) exec(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.inContext(sql, args, func(tx pgx.Tx) error {
		_, err := tx.Exec(w.ctx, sql, args...)
		return err
	}); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
}

// inContext runs fn in a transaction of the owner's, with app.tenant_id set to args[0] when sql
// writes transactions.
func (w *hWorld) inContext(sql string, args []any, fn func(tx pgx.Tx) error) (bool, error) {
	tx, err := w.owner.Begin(w.ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if id, ok := args[0].(uuid.UUID); ok && strings.Contains(sql, "INSERT INTO transactions") {
		if _, err := tx.Exec(w.ctx, `SELECT set_config('app.tenant_id', $1, true)`, id.String()); err != nil {
			return false, err
		}
	}
	if err := fn(tx); err != nil {
		return false, err
	}
	return true, tx.Commit(w.ctx)
}

func (w *hWorld) tenant(name string) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	w.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure, api_code) VALUES ($1, $2, $3, 'restaurant', 'multi', $4)`,
		id, "api1c "+name, "VAT-API1C-"+id.String(), name+strings.ToUpper(id.String()[:4]))
	return id
}

func (w *hWorld) holding() uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `INSERT INTO holdings (name) VALUES ($1) RETURNING id`, "api1c "+uuid.NewString()[:8]).Scan(&id); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *hWorld) tag(tenant, venue uuid.UUID) string {
	w.t.Helper()
	uid := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:14])
	w.exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 1000, 'active')`,
		uid, tenant, venue, make([]byte, 44))
	return uid
}

func (w *hWorld) employee(tenant uuid.UUID) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	w.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, $3, $4, 'active', now())`,
		id, tenant, "ext-"+id.String()[:8], "Staff "+id.String()[:6])
	return id
}

func (w *hWorld) newAdmin(tenant uuid.UUID) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Owner', $3, $4, 'owner')`,
		id, tenant, "owner-"+id.String()[:12]+"@example.test", "$2a$12$"+strings.Repeat("h", 53))
	return id
}

// hRow is one employer row on the site's plaque at the site's venue (or a manual record there).
type hRow struct {
	employer, site, employee uuid.UUID
	at                       time.Time
	verdict, kind, channel   string
	ipMatch, gpsMatch        *bool
	trust                    *int16
	context                  string
}

func (w *hWorld) row(r hRow) uuid.UUID {
	w.t.Helper()
	if r.channel == "" {
		r.channel = "nfc"
	}
	var tag any = w.plaque[r.site]
	var sun any = true
	if r.channel == "manual" {
		tag, sun = nil, nil
	}
	var kind any
	if r.kind != "" {
		kind = r.kind
	}
	var pc any
	if r.context != "" {
		pc = r.context
	}
	var id uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `
		INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, ip_match, gps_match, trust,
		                          queued, policy_context, site_tenant_id, site_location_id, site_tag_uid)
		VALUES ($1, $2, $3, $4, $5, $6, $7, CASE WHEN $5 = 'nfc' THEN 1000 END, $8, $9, $10, $4 = 'flag', $11::jsonb, $12, $13, $14)
		RETURNING id`,
		r.employer, r.employee, r.at, r.verdict, r.channel, kind, sun, r.ipMatch, r.gpsMatch, r.trust, pc,
		r.site, w.venue[r.site], tag).Scan(&id); err != nil {
		w.t.Fatalf("the employer's row: %v", err)
	}
	return id
}

func hBool(b bool) *bool    { return &b }
func hInt16(n int16) *int16 { return &n }

// hLocal is a wall clock in Malta (the fixture tenants' zone, 00008's default).
func hLocal(t *testing.T, day, clock string) time.Time {
	t.Helper()
	zone, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	at, err := time.ParseInLocation("2006-01-02 15:04:05", day+" "+clock, zone)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// TestEmployerReaders_NineReadersNameTheSite is ADR 0027 §9's nine readers (plan §5 API-1c:
// "dokuz okuyucunun her biri saha satırını doğru mekân/vardiya ile (her yönde) gösterir"), each a
// case of its own, in BOTH directions (KM's staff at KF's venue, KF's at KM's). The rows sit in a
// fixed past week, Wednesday 16 and Thursday 17 September 2026 (Malta):
//
//	e1  in 09:30 (late against the SITE's 09:00 shift; ip no, gps yes; tap:ctrGap 5), out 17:00
//	e1  in 09:00 on the 17th, e2 in 09:00:05 on the 17th and 09:30:10 on the 16th (a pair, 2 days)
//	e3  flagged in 10:00 (waiting for review, and open)
//
// and each reader is asked as the EMPLOYER: the venue is the site's, by name, its shift is the
// site's, and the site's evidence is not described (K-39). Readers 7 and 8 name the site's plaque
// NOWHERE at the employer (the site sees it from its projection -- the site half is asserted
// too), reader 9 is the site's: its venue's references count the peer's taps. M12's Pass readers
// follow as cases 10 onward (the panel redesign's D3 reads): each names the site's venue for the
// employer, carries no evidence, and the site's own reading does not hold the employer's staff.
func TestEmployerReaders_NineReadersNameTheSite(t *testing.T) {
	w := newHWorld(t)
	day := Date{Year: 2026, Month: time.September, Day: 16}
	for _, dir := range []struct {
		name           string
		employer, site uuid.UUID
	}{{"KM staff at KF", w.km, w.kf}, {"KF staff at KM", w.kf, w.km}} {
		e := w.staff[dir.employer]
		in := w.row(hRow{employer: dir.employer, site: dir.site, employee: e[0], at: hLocal(t, "2026-09-16", "09:30:00"), verdict: "ok", kind: "in",
			ipMatch: hBool(false), gpsMatch: hBool(true), trust: hInt16(50), context: `{"tap:ctrGap": 5, "tap:gpsConflict": true}`})
		w.row(hRow{employer: dir.employer, site: dir.site, employee: e[0], at: hLocal(t, "2026-09-16", "17:00:00"), verdict: "ok", kind: "out",
			ipMatch: hBool(true), gpsMatch: hBool(true), trust: hInt16(100)})
		w.row(hRow{employer: dir.employer, site: dir.site, employee: e[1], at: hLocal(t, "2026-09-16", "09:30:10"), verdict: "ok", kind: "in", trust: hInt16(70)})
		w.row(hRow{employer: dir.employer, site: dir.site, employee: e[0], at: hLocal(t, "2026-09-17", "09:00:00"), verdict: "ok", kind: "in", trust: hInt16(70)})
		w.row(hRow{employer: dir.employer, site: dir.site, employee: e[1], at: hLocal(t, "2026-09-17", "09:00:05"), verdict: "ok", kind: "in", trust: hInt16(70)})
		flagged := w.row(hRow{employer: dir.employer, site: dir.site, employee: e[2], at: hLocal(t, "2026-09-16", "10:00:00"), verdict: "flag", kind: "in", trust: hInt16(20)})
		venue, plaque := w.venueName[dir.site], w.plaque[dir.site]

		t.Run(dir.name+"/1 ListPanelTransactions", func(t *testing.T) {
			page, err := w.r.Day(w.ctx, dir.employer, Filter{Date: day})
			if err != nil {
				t.Fatal(err)
			}
			for _, rec := range page.Records {
				if rec.ID != in {
					continue
				}
				if rec.LocationName != venue || !rec.AtPartnerVenue || rec.TagUID != plaque {
					t.Errorf("the record names %q (partner %v, plaque %q), want %q at the partner's plaque %s", rec.LocationName, rec.AtPartnerVenue, rec.TagUID, venue, plaque)
				}
				if rec.IPMatch != nil || rec.GPSMatch != nil || rec.Trust != nil {
					t.Errorf("K-39: the site's evidence is described (ip %v, gps %v, trust %v)", rec.IPMatch, rec.GPSMatch, rec.Trust)
				}
				return
			}
			t.Error("the row is not on its day's page")
		})
		t.Run(dir.name+"/2 ListFlaggedForReview", func(t *testing.T) {
			q, err := w.r.Queue(w.ctx, dir.employer, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, rec := range q.Records {
				if rec.ID == flagged {
					if rec.LocationName != venue || !rec.AtPartnerVenue || rec.Trust != nil {
						t.Errorf("the queued record names %q (partner %v, trust %v), want %q and no score", rec.LocationName, rec.AtPartnerVenue, rec.Trust, venue)
					}
					return
				}
			}
			t.Error("the flagged row is not in the queue")
		})
		anomalies, err := w.r.Anomalies(w.ctx, dir.employer, ReportFilter{Day: day})
		if err != nil {
			t.Fatal(err)
		}
		t.Run(dir.name+"/3 ListOpenCheckIns", func(t *testing.T) {
			for _, o := range anomalies.Open {
				if o.At.Equal(hLocal(t, "2026-09-16", "10:00:00")) {
					if o.LocationName != venue {
						t.Errorf("the open check-in names %q, want %q", o.LocationName, venue)
					}
					return
				}
			}
			t.Errorf("the flagged open check-in is not listed (%d open)", len(anomalies.Open))
		})
		t.Run(dir.name+"/4 ListWorkedShiftEvents + resolveShift", func(t *testing.T) {
			hours, err := w.r.Hours(w.ctx, dir.employer, ReportFilter{Day: day})
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, v := range hours.Venues {
				if v.Name == venue {
					found = true
					if v.LocationID != w.venue[dir.site] || v.Worked != 7*time.Hour+30*time.Minute {
						t.Errorf("the site's bucket: %v worked at %s, want 7h30m at %s", v.Worked, v.LocationID, w.venue[dir.site])
					}
				}
				if v.LocationID == uuid.Nil {
					t.Errorf("a nameless bucket (%q, %v) -- the site row fell into uuid.Nil", v.Name, v.Worked)
				}
			}
			if !found {
				t.Errorf("no bucket names the site's venue: %+v", hours.Venues)
			}
			for _, p := range hours.People {
				if p.EmployeeID == e[0] && p.LateShifts == 0 {
					t.Error("e1 arrived 09:30 against the site's 09:00 shift and is not late -- the site's shift was not resolved")
				}
			}
		})
		t.Run(dir.name+"/5 ListAnomalyVenues", func(t *testing.T) {
			for _, v := range anomalies.Venues {
				if v.Name == venue {
					if !v.Partner || v.DistanceOnly != 0 || v.OutsideVenue != 0 || v.Records < 6 {
						t.Errorf("the site's venue row: partner %v, distance-only %d, outside %d, records %d; want a partner row of 6 records with no evidence figure (K-39)",
							v.Partner, v.DistanceOnly, v.OutsideVenue, v.Records)
					}
					return
				}
			}
			t.Errorf("no venue row names the site's venue: %+v", anomalies.Venues)
		})
		t.Run(dir.name+"/6 ListTapsTakenTogether", func(t *testing.T) {
			for _, p := range anomalies.Pairs {
				if p.Venue == venue {
					return
				}
			}
			t.Errorf("e1 and e2 tapped the site's plaque together on two days and no pair names its venue: %+v", anomalies.Pairs)
		})
		t.Run(dir.name+"/7 ListAnomalyPlaques", func(t *testing.T) {
			for _, p := range anomalies.Plaques {
				if p.TagUID == plaque {
					t.Errorf("the site's plaque is on the EMPLOYER's gap list (ADR 0027 §9: not the employer's anomaly)")
				}
			}
			site, err := w.r.Anomalies(w.ctx, dir.site, ReportFilter{})
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range site.Plaques {
				if p.TagUID == plaque {
					if p.LargestGap < 5 {
						t.Errorf("the site's gap list shows its plaque with a largest gap %d, want the 5 the employer's tap revealed", p.LargestGap)
					}
					return
				}
			}
			t.Error("the site does not see the gap its plaque showed at the peer's tap (K-1i)")
		})
		t.Run(dir.name+"/8 ListTagLastSeen", func(t *testing.T) {
			var own []store.ListTagLastSeenRow
			var site []store.ListSiteTagLastSeenRow
			if err := w.data.WithTenant(w.ctx, dir.employer, func(ctx context.Context, tx pgx.Tx) error {
				var err error
				own, err = store.New(tx).ListTagLastSeen(ctx, dir.employer)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			for _, r := range own {
				if r.TagUid != nil && *r.TagUid == plaque {
					t.Error("the site's plaque has a 'last seen' at the EMPLOYER")
				}
			}
			if err := w.data.WithTenant(w.ctx, dir.site, func(ctx context.Context, tx pgx.Tx) error {
				var err error
				site, err = store.New(tx).ListSiteTagLastSeen(ctx, dir.site)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			for _, r := range site {
				if r.TagUid == plaque {
					if time.Since(r.LastSeen) > time.Hour {
						t.Errorf("the site's last seen is %v -- the declared occurred_at, not the server's clock", r.LastSeen)
					}
					return
				}
			}
			t.Error("the site has no 'last seen' for its plaque the peer's staff tapped")
		})
		t.Run(dir.name+"/9 CountLocationReferences", func(t *testing.T) {
			var refs store.CountLocationReferencesRow
			if err := w.data.WithTenant(w.ctx, dir.site, func(ctx context.Context, tx pgx.Tx) error {
				var err error
				refs, err = store.New(tx).CountLocationReferences(ctx, store.CountLocationReferencesParams{TenantID: dir.site, ID: w.venue[dir.site]})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if refs.HoldingCount < 6 {
				t.Errorf("the site's venue counts %d holding references, want the peer's 6 taps", refs.HoldingCount)
			}
		})
		// THE PASS'S READERS (M12-13 ...), asked at 12:30 on the 17th: e1 and e2 are on the floor at
		// the site's venue (their 17th check-ins are open), e3's flagged check-in of the 16th is
		// older than tap.StaleOpenIn and is not.
		pass := readerAt(t, w.data, hLocal(t, "2026-09-17", "12:30:00"), nil)
		employerPass, err := pass.Screen(w.ctx, dir.employer, Filter{})
		if err != nil {
			t.Fatal(err)
		}
		sitePass, err := pass.Screen(w.ctx, dir.site, Filter{})
		if err != nil {
			t.Fatal(err)
		}
		t.Run(dir.name+"/10 CountOnTheFloorByVenue", func(t *testing.T) {
			f := employerPass.Floor
			if f.State != Measured || f.People != 2 || len(f.Venues) != 1 {
				t.Fatalf("the employer's floor: %+v, want 2 people at one venue", f)
			}
			if v := f.Venues[0]; v.Name != venue || !v.Partner || v.LocationID != w.venue[dir.site] || v.People != 2 {
				t.Errorf("the employer's floor names %+v, want the site's venue %q as a partner venue with 2 people", v, venue)
			}
			// The site's own floor does not hold the employer's staff: those records are the
			// employer's, and RLS keeps them there.
			if sitePass.Floor.State != Measured {
				t.Fatalf("the site's floor is %v, want Measured", sitePass.Floor.State)
			}
			for _, v := range sitePass.Floor.Venues {
				if v.LocationID == w.venue[dir.site] {
					t.Errorf("the site's floor counts %d people at its own venue -- the employer's staff", v.People)
				}
			}
		})
		t.Run(dir.name+"/11 ListWorkedShiftEvents (the Pass's tape)", func(t *testing.T) {
			tp := employerPass.Tape
			if tp.State != Measured || len(tp.Hours) != 13 || len(tp.Venues) != 1 {
				t.Fatalf("the employer's tape: %v, %d hours, %d venues; want Measured, 13 hours, the site's venue alone", tp.State, len(tp.Hours), len(tp.Venues))
			}
			v := tp.Venues[0]
			if v.Name != venue || !v.Partner || v.LocationID != w.venue[dir.site] {
				t.Errorf("the employer's tape names %q (partner %v, %s), want the site's venue %q", v.Name, v.Partner, v.LocationID, venue)
			}
			for h, n := range v.People {
				want := 0
				if h >= 9 {
					want = 2 // e1 and e2 from their 09:00 check-ins; e3's entry is past tap.StaleOpenIn
				}
				if n != want {
					t.Errorf("the employer's tape at %02d:00 counts %d, want %d (%v)", h, n, want, v.People)
				}
			}
			if sitePass.Tape.State != Measured {
				t.Fatalf("the site's tape is %v, want Measured", sitePass.Tape.State)
			}
			for _, v := range sitePass.Tape.Venues {
				if v.LocationID == w.venue[dir.site] {
					t.Errorf("the site's tape has a row for its own venue -- the employer's staff: %v", v.People)
				}
			}
		})
		t.Run(dir.name+"/12 ListOpenCheckIns (the Pass's left open)", func(t *testing.T) {
			// Yesterday is the 16th: e1's entry was closed at 17:00, e2's and e3's (flagged) were not.
			want := LeftOpen{State: Measured, N: 2, Day: day}
			if employerPass.LeftOpen != want {
				t.Errorf("the employer's left open: %+v, want %+v", employerPass.LeftOpen, want)
			}
			// The site counts its OWN staff's records only. KF's staff have none yet when KF is the
			// site (the KM-staff direction runs first); KM's have the first direction's two.
			wantSite := 0
			if dir.site == w.km {
				wantSite = 2
			}
			if sitePass.LeftOpen.State != Measured || sitePass.LeftOpen.N != wantSite {
				t.Errorf("the site's left open: %+v, want %d (its own staff's only)", sitePass.LeftOpen, wantSite)
			}
		})
	}
}

// TestSiteReport_HoldingStaffHaveNoHoursAtTheSite (ADR 0027 §9, 10th round; plan §5 API-1c "KF→KM
// ve KM→KF ardışık iki vardiya, aynı gün ve iki güne yayılan → KF'de 0 sa ve dokunuş listesi"): a
// KM employee works KF→KM (in at KF's plaque, out at KM's own) and KM→KF (in at KM, out at KF),
// the second across midnight -- at KF the week's hours are ZERO and the four KF-side taps are
// LISTED, each "at their employer". KM's own report pays both shifts (the chain is the employer's).
func TestSiteReport_HoldingStaffHaveNoHoursAtTheSite(t *testing.T) {
	w := newHWorld(t)
	day := Date{Year: 2026, Month: time.September, Day: 9}
	e := w.staff[w.km][0]
	ownKM := func(at time.Time, kind string) {
		w.exec(`INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type, sun_valid, ctr, trust)
		        VALUES ($1, $2, $3, $4, $5, 'ok', 'nfc', $6, true, 1000, 70)`, w.km, e, w.venue[w.km], w.plaque[w.km], at, kind)
	}
	kfIn := w.row(hRow{employer: w.km, site: w.kf, employee: e, at: hLocal(t, "2026-09-09", "08:00:00"), verdict: "ok", kind: "in"})
	ownKM(hLocal(t, "2026-09-09", "16:00:00"), "out")
	ownKM(hLocal(t, "2026-09-10", "20:00:00"), "in")
	kfOut := w.row(hRow{employer: w.km, site: w.kf, employee: e, at: hLocal(t, "2026-09-11", "04:00:00"), verdict: "ok", kind: "out"})

	site, err := w.r.Hours(w.ctx, w.kf, ReportFilter{Day: day})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range site.People {
		if p.EmployeeID == e {
			t.Errorf("KF's report carries hours for KM's employee: %+v", p)
		}
	}
	if site.Totals.Worked != 0 || site.Totals.Shifts != 0 || site.Totals.Open != 0 {
		t.Errorf("KF's totals carry the partner's shifts: %v in %d shift(s), %d open -- want 0 h", site.Totals.Worked, site.Totals.Shifts, site.Totals.Open)
	}
	if !site.Partners.Queried || len(site.Partners.Taps) != 2 {
		t.Fatalf("KF lists %d partner taps, want its 2 (the KF-side in and out)", len(site.Partners.Taps))
	}
	for _, p := range site.Partners.Taps {
		if p.Outcome != "counted" || p.Venue != w.venueName[w.kf] {
			t.Errorf("a partner tap: %+v, want counted at KF's venue", p)
		}
	}
	_ = kfIn
	_ = kfOut
	employer, err := w.r.Hours(w.ctx, w.km, ReportFilter{Day: day})
	if err != nil {
		t.Fatal(err)
	}
	if employer.Totals.Shifts != 2 || employer.Totals.Worked != 8*time.Hour+8*time.Hour {
		t.Errorf("KM pays %v in %d shift(s), want 16h in 2 (08-16 and 20-04, the chain across both businesses)", employer.Totals.Worked, employer.Totals.Shifts)
	}
}

// TestSiteReport_OwnStaffHoursUnchanged (ADR 0027 §9: "KF'nin kendi çalışanlarının süreleri
// değişmez"): KF's own employee's 09:00-13:00 shift is 4 h at KF whether or not a KM employee tapped
// KF's plaques that day (a KM in at 08:00 and an out at 12:00 on the same plaque, between KF's two
// taps) -- the ledger reads KF's own transactions alone.
func TestSiteReport_OwnStaffHoursUnchanged(t *testing.T) {
	w := newHWorld(t)
	day := Date{Year: 2026, Month: time.August, Day: 12}
	kfStaff := w.staff[w.kf][0]
	w.exec(`INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type, sun_valid, ctr, trust)
	        VALUES ($1, $2, $3, $4, $5, 'ok', 'nfc', 'in', true, 1000, 70), ($1, $2, $3, $4, $6, 'ok', 'nfc', 'out', true, 1000, 70)`,
		w.kf, kfStaff, w.venue[w.kf], w.plaque[w.kf], hLocal(t, "2026-08-12", "09:00:00"), hLocal(t, "2026-08-12", "13:00:00"))
	w.row(hRow{employer: w.km, site: w.kf, employee: w.staff[w.km][0], at: hLocal(t, "2026-08-12", "08:00:00"), verdict: "ok", kind: "in"})
	w.row(hRow{employer: w.km, site: w.kf, employee: w.staff[w.km][0], at: hLocal(t, "2026-08-12", "12:00:00"), verdict: "ok", kind: "out"})
	site, err := w.r.Hours(w.ctx, w.kf, ReportFilter{Day: day})
	if err != nil {
		t.Fatal(err)
	}
	if site.Totals.Worked != 4*time.Hour || site.Totals.Shifts != 1 || len(site.People) != 1 || site.People[0].EmployeeID != kfStaff {
		t.Errorf("KF's own staff: %v in %d shift(s), %d people -- want 4h in 1, KF's employee alone", site.Totals.Worked, site.Totals.Shifts, len(site.People))
	}
	if len(site.Partners.Taps) != 2 {
		t.Errorf("KF lists %d partner taps, want 2", len(site.Partners.Taps))
	}
}

// TestSiteReport_ApprovedFlagFoldsToItsLastOutcome (ADR 0027 §9 "transaction_id başına son satır";
// plan §5 API-1c "onaylı flag transaction_id başına tek satır, sahte açık kayıt yok"): KM's flagged
// tap at KF, then KM's approval -- KF lists ONE row for it, 'approved', and invents no open
// check-in (KF's report has no open entry for it). And a pending one stays "pending_review".
func TestSiteReport_ApprovedFlagFoldsToItsLastOutcome(t *testing.T) {
	w := newHWorld(t)
	day := Date{Year: 2026, Month: time.September, Day: 2}
	e := w.staff[w.km]
	approved := w.row(hRow{employer: w.km, site: w.kf, employee: e[0], at: hLocal(t, "2026-09-02", "09:00:00"), verdict: "flag", kind: "in"})
	w.row(hRow{employer: w.km, site: w.kf, employee: e[1], at: hLocal(t, "2026-09-02", "09:10:00"), verdict: "flag", kind: "in"})
	if err := w.data.WithTenant(w.ctx, w.km, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome) VALUES ($1, $2, $3, 'approved')`,
			w.km, approved, w.admin[w.km])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	site, err := w.r.Hours(w.ctx, w.kf, ReportFilter{Day: day})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, p := range site.Partners.Taps {
		got[p.Outcome]++
	}
	if len(site.Partners.Taps) != 2 || got["approved"] != 1 || got["pending_review"] != 1 {
		t.Errorf("KF lists %v, want one approved and one pending_review -- one row per record, its last outcome", got)
	}
	if len(site.Open) != 0 || site.Totals.Open != 0 {
		t.Errorf("KF's report shows %d open check-in(s) for the partner's taps, want none", len(site.Open))
	}
}

// TestSiteReport_EmployerManualSeparateAndExcluded (K-1a; plan §5 API-1c "employer_manual ayrı ve
// toplamdan hariç"): KM's manager types a record at KF's venue -- KF lists it APART (Manual), not
// among the taps, and no total at KF moves.
func TestSiteReport_EmployerManualSeparateAndExcluded(t *testing.T) {
	w := newHWorld(t)
	day := Date{Year: 2026, Month: time.August, Day: 26}
	w.row(hRow{employer: w.km, site: w.kf, employee: w.staff[w.km][0], at: hLocal(t, "2026-08-26", "09:00:00"), verdict: "ok", kind: "in", channel: "manual"})
	w.row(hRow{employer: w.km, site: w.kf, employee: w.staff[w.km][0], at: hLocal(t, "2026-08-26", "15:00:00"), verdict: "ok", kind: "out", channel: "manual"})
	site, err := w.r.Hours(w.ctx, w.kf, ReportFilter{Day: day})
	if err != nil {
		t.Fatal(err)
	}
	if len(site.Partners.Manual) != 2 || len(site.Partners.Taps) != 0 {
		t.Errorf("KF lists %d manual and %d tap(s), want the 2 manual records apart", len(site.Partners.Manual), len(site.Partners.Taps))
	}
	for _, m := range site.Partners.Manual {
		if m.Outcome != "employer_manual" {
			t.Errorf("a manual record reads %q, want employer_manual", m.Outcome)
		}
	}
	if site.Totals.Worked != 0 || site.Totals.Shifts != 0 || len(site.People) != 0 {
		t.Errorf("KF's totals moved: %v in %d shifts, %d people", site.Totals.Worked, site.Totals.Shifts, len(site.People))
	}
}

// TestSitePanel_LostPlaqueAlertAtTheSite (plan §5 API-1c "kayıp KF plaketi uyarısı KF panelinde";
// with TestSitePunches_LostPlaqueRepeatsGiveOneRowAndOneAlert's alert half): a KM employee touches
// KF's LOST plaque three times with one URL -- KM refuses and records each; KF's anomalies screen
// carries ONE lost-plaque alert for that plaque (one refusal reached it), and KM's carries none.
func TestSitePanel_LostPlaqueAlertAtTheSite(t *testing.T) {
	w := newHWorld(t)
	lost := w.tag(w.kf, w.venue[w.kf])
	w.exec(`UPDATE tags SET status = 'lost' WHERE uid = $1`, lost)
	for _, declared := range []string{"2099-01-01T00:00:00Z", "2001-01-01T00:00:00Z", "2099-06-01T00:00:00Z"} {
		at, _ := time.Parse(time.RFC3339, declared)
		if _, err := w.owner.Exec(w.ctx, `
			INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, sun_valid, ctr, matched_sid,
			                          site_tenant_id, site_location_id, site_tag_uid)
			VALUES ($1, $2, $3, 'reject', 'nfc', false, 1500, 'sys:tag-not-active', $4, $5, $6)`,
			w.km, w.staff[w.km][0], at, w.kf, w.venue[w.kf], lost); err != nil {
			t.Fatal(err)
		}
	}
	site, err := w.r.Anomalies(w.ctx, w.kf, ReportFilter{})
	if err != nil {
		t.Fatal(err)
	}
	var found []AnomalyLostPlaque
	for _, l := range site.LostPlaques {
		if l.TagUID == lost {
			found = append(found, l)
		}
	}
	if len(found) != 1 || found[0].Refusals != 1 || found[0].LocationName != w.venueName[w.kf] {
		t.Fatalf("KF's alerts for its lost plaque: %+v, want one, one refusal, at its venue", found)
	}
	employer, err := w.r.Anomalies(w.ctx, w.km, ReportFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range employer.LostPlaques {
		if l.TagUID == lost {
			t.Error("the EMPLOYER's panel carries the site's lost-plaque alert")
		}
	}
}

// TestSitePanel_CannotReview (ADR 0027 §9 "KF onay vermez"; plan §8): KF's manager cannot decide
// KM's flagged tap at KF's venue -- the review statement builds its row FROM a flagged record in
// the REVIEWER's tenant, and KM's record is not one (RLS and the explicit predicate): no row, no
// review, KF's projection still 'pending_review' alone. KF's own review queue does not list it
// either. CONTROL: KM's manager decides it.
func TestSitePanel_CannotReview(t *testing.T) {
	w := newHWorld(t)
	flagged := w.row(hRow{employer: w.km, site: w.kf, employee: w.staff[w.km][0], at: hLocal(t, "2026-08-19", "09:00:00"), verdict: "flag", kind: "in"})
	review := func(tenant, reviewer uuid.UUID) error {
		return w.data.WithTenant(w.ctx, tenant, func(ctx context.Context, tx pgx.Tx) error {
			_, err := store.New(tx).InsertTransactionReview(ctx, store.InsertTransactionReviewParams{
				TenantID: tenant, TransactionID: flagged, ReviewerID: reviewer, Outcome: "approved"})
			return err
		})
	}
	if err := review(w.kf, w.admin[w.kf]); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("KF's review of KM's record: %v, want no row (pgx.ErrNoRows -- review.ErrNotReviewable)", err)
	}
	q, err := w.r.Queue(w.ctx, w.kf, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range q.Records {
		if rec.ID == flagged {
			t.Error("KM's flagged record is in KF's review queue")
		}
	}
	outcomes := func() string {
		var s string
		if err := w.owner.QueryRow(w.ctx, `SELECT string_agg(outcome, ',' ORDER BY outcome) FROM site_punches WHERE transaction_id = $1`, flagged).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if got := outcomes(); got != "pending_review" {
		t.Errorf("KF's projection after KF's attempt: %s, want pending_review alone", got)
	}
	if err := review(w.km, w.admin[w.km]); err != nil {
		t.Fatalf("CONTROL: KM's own review: %v", err)
	}
	if got := outcomes(); got != "approved,pending_review" {
		t.Errorf("KF's projection after KM's approval: %s", got)
	}
}

// TestSitePanel_LostPlaqueAlertUsesTheServerClock (ADR 0027 §9: "KF'nin kayıp plaket uyarısı ...
// site_punches.created_at ile beslenir"; plan §5 API-1c): a KM employee's refusal on KF's lost
// plaque DECLARES 2099 -- the alert is in THIS week's anomalies (the server's week), its time the
// server's, and the 2099 week and the declared week of a 2001 refusal carry nothing. The declared
// time moves the alert neither forward nor back.
func TestSitePanel_LostPlaqueAlertUsesTheServerClock(t *testing.T) {
	w := newHWorld(t)
	lost := w.tag(w.kf, w.venue[w.kf])
	w.exec(`UPDATE tags SET status = 'lost' WHERE uid = $1`, lost)
	for _, declared := range []time.Time{time.Date(2099, 6, 3, 9, 0, 0, 0, time.UTC), time.Date(2001, 6, 6, 9, 0, 0, 0, time.UTC)} {
		w.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, sun_valid, ctr, matched_sid,
		                                  site_tenant_id, site_location_id, site_tag_uid)
		        VALUES ($1, $2, $3, 'reject', 'nfc', false, 1500, 'sys:tag-not-active', $4, $5, $6)`,
			w.km, w.staff[w.km][1], declared, w.kf, w.venue[w.kf], lost)
	}
	alertIn := func(f ReportFilter) *AnomalyLostPlaque {
		t.Helper()
		a, err := w.r.Anomalies(w.ctx, w.kf, f)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range a.LostPlaques {
			if l.TagUID == lost {
				return &l
			}
		}
		return nil
	}
	now := alertIn(ReportFilter{})
	if now == nil {
		t.Fatal("this week (the server's) carries no alert for the refusals")
	}
	if d := time.Since(now.Last); d < -time.Minute || d > time.Hour {
		t.Errorf("the alert's time is %v -- a declared time, not the server's", now.Last)
	}
	for _, d := range []Date{{Year: 2099, Month: time.June, Day: 3}, {Year: 2001, Month: time.June, Day: 6}} {
		if a := alertIn(ReportFilter{Day: d}); a != nil {
			t.Errorf("the declared week %v carries the alert -- the declared time moved it", d)
		}
	}
}

// partnerNoteCase is one note on a row at a partner venue, the rule that decided it, and whether
// the employer's lists may show it as written.
type partnerNoteCase struct {
	sid, note string
	manual    bool
	shown     bool   // false: replaced by PartnerVenueNote
	want      string // what is shown when replaced ("" = PartnerVenueNote)
}

// partnerNoteCases: the engine's EVIDENCE sentences (K-39 -- the five baseline reasons that describe
// the peer's network or coordinate, a tenant's copy of one, the cross-tenant QR guardrail's) are
// replaced; the manager's own words on a manual record, a lost plaque's reason, a non-evidence rule
// (the counter gap K-1i hands the employer) and the direction's annotation are kept (the second
// review's ORTA-B).
var partnerNoteCases = []partnerNoteCase{
	{sid: "base:ip-or-gps-ok", note: "network proof of place: the source IP matches the location"},
	{sid: "base:gps-only-allow", note: "verified via GPS only — no network proof of place"},
	{sid: "base:no-evidence-review", note: "neither IP nor GPS could place this tap"},
	{sid: "base:qr-requires-ip", note: "QR tap without an IP match: GPS alone is not sufficient proof of place"},
	{sid: "base:gps-conflict-review", note: "GPS was read and puts the device outside this location's radius"},
	{sid: "sys:cross-tenant-qr-site-ip", note: "a QR scan at another business of the group needs that venue's own network; sent for approval"},
	{sid: "own:gps-only-allow", note: "verified via GPS only — no network proof of place"},
	{sid: "base:gps-only-allow", note: "verified via GPS only — no network proof of place; " + tap.StaleOpenInNote,
		want: PartnerVenueNote + "; " + tap.StaleOpenInNote},
	// A manual record carries no rule in the product; an evidence sid here pins that the
	// CHANNEL exempts it -- the manager's words describe no evidence whatever the sid says.
	{sid: "base:no-evidence-review", note: "MANUAL-CANARY phone was flat, worked the KF lunch shift", manual: true, shown: true},
	{sid: "sys:tag-not-active", note: "this tag is not in service", shown: true},
	{sid: "base:ctr-gap-review", note: "the tag counter jumped: reads happened that the server never saw", shown: true},
	{sid: "base:cross-location-note", note: "different branch; " + tap.StaleOpenInNote, shown: true},
}

// TestEmployerReaders_APartnerVenueRowCarriesNoEvidenceSentence is K-39 in words (ADR 0027 §13 item
// 5; the review's G-ORTA-1 and the second review's ORTA-B): each partnerNoteCases row, as an ok (or
// typed) row on the day list and a flagged row in the review queue -- every evidence sentence is
// replaced by PartnerVenueNote (the direction's annotation kept), every other note is shown as
// written. CONTROL: an evidence sentence on a row at the employer's OWN venue is shown as written.
// MUTANTS (measured, site.go): the manual exemption removed -> the manual canary is lost; the sid
// set narrowed (no gps-conflict-review) -> its sentence leaks; widened (every base: sid) -> the
// counter gap's sentence is lost; the own: namespace dropped -> the tenant copy leaks.
func TestEmployerReaders_APartnerVenueRowCarriesNoEvidenceSentence(t *testing.T) {
	w := newHWorld(t)
	day := Date{Year: 2026, Month: time.July, Day: 22}
	insert := func(c partnerNoteCase, verdict string, site bool, clock string) uuid.UUID {
		t.Helper()
		at := hLocal(t, "2026-07-22", clock)
		var id uuid.UUID
		var siteTenant, siteVenue, siteTag, venue, tag any
		channel := "nfc"
		var sun, ctr any = true, 1000
		if site {
			siteTenant, siteVenue, siteTag = w.kf, w.venue[w.kf], w.plaque[w.kf]
		} else {
			venue, tag = w.venue[w.km], w.plaque[w.km]
		}
		if c.manual {
			channel, sun, ctr, siteTag, tag = "manual", nil, nil, nil, nil
		}
		const insertRow = `
			INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, trust, queued, note,
			                          policy_layer, matched_sid, location_id, tag_uid, site_tenant_id, site_location_id, site_tag_uid)
			VALUES ($1, $2, $3, $4, $5, 'in', $6, $7, 50, $4 = 'flag', $8, CASE WHEN $9 <> '' THEN 'guardrail' END, NULLIF($9, ''),
			        $10, $11, $12, $13, $14) RETURNING id`
		args := []any{w.km, w.staff[w.km][0], at, verdict, channel, sun, ctr, c.note, c.sid, venue, tag, siteTenant, siteVenue, siteTag}
		if _, err := w.inContext(insertRow, args, func(tx pgx.Tx) error {
			return tx.QueryRow(w.ctx, insertRow, args...).Scan(&id)
		}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	cases := map[uuid.UUID]partnerNoteCase{}
	for i, c := range partnerNoteCases {
		cases[insert(c, "ok", true, fmt.Sprintf("09:%02d:00", i))] = c
		if !c.manual {
			cases[insert(c, "flag", true, fmt.Sprintf("10:%02d:00", i))] = c
		}
	}
	own := insert(partnerNoteCases[1], "ok", false, "12:00:00")

	page, err := w.r.Day(w.ctx, w.km, Filter{Date: day})
	if err != nil {
		t.Fatal(err)
	}
	queue, err := w.r.Queue(w.ctx, w.km, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, rec := range append(page.Records, queue.Records...) {
		if rec.ID == own {
			if rec.Note != partnerNoteCases[1].note {
				t.Errorf("CONTROL: the own-venue row's note is %q, want the engine's sentence", rec.Note)
			}
			continue
		}
		c, ok := cases[rec.ID]
		if !ok {
			continue
		}
		seen++
		want := c.note
		if !c.shown {
			want = c.want
			if want == "" {
				want = PartnerVenueNote
			}
		}
		if rec.Note != want {
			t.Errorf("%s (%s): the note reads %q, want %q", c.sid, rec.Verdict, rec.Note, want)
		}
	}
	queued := 0
	for _, c := range partnerNoteCases {
		if !c.manual {
			queued++
		}
	}
	if seen != len(cases)+queued {
		t.Errorf("read %d partner-venue records across the two lists, want %d (every row on the day, every flag in the queue)", seen, len(cases)+queued)
	}
}

// TestSitePanel_PlaqueGapCountsTheTapOnce (the API-1c review's E-D4): KM's flagged tap on KF's
// plaque that revealed a gap of 7, then KM's approval of it -- KF's gap list shows that plaque with
// ONE record and one gap: the approval is a later row about the same tap, not a second tap.
// MUTANT (measured: records as count(*)): 2 records.
func TestSitePanel_PlaqueGapCountsTheTapOnce(t *testing.T) {
	w := newHWorld(t)
	uid := w.tag(w.kf, w.venue[w.kf])
	var id uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `
		INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, queued, policy_context,
		                          site_tenant_id, site_location_id, site_tag_uid)
		VALUES ($1, $2, now(), 'flag', 'nfc', 'in', true, 1000, true, '{"tap:ctrGap": 7}', $3, $4, $5) RETURNING id`,
		w.km, w.staff[w.km][0], w.kf, w.venue[w.kf], uid).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := w.data.WithTenant(w.ctx, w.km, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome) VALUES ($1, $2, $3, 'approved')`, w.km, id, w.admin[w.km])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	site, err := w.r.Anomalies(w.ctx, w.kf, ReportFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range site.Plaques {
		if p.TagUID == uid {
			if p.Records != 1 || p.CounterGaps != 1 || p.LargestGap != 7 {
				t.Errorf("KF's gap row: %d records, %d gaps, largest %d; want 1, 1, 7", p.Records, p.CounterGaps, p.LargestGap)
			}
			return
		}
	}
	t.Error("KF's gap list does not show its plaque")
}

// TestAnomalies_APartnerVenueRowAddsNoEvidenceFigure is K-39 on the anomalies screen (the second
// review's ORTA-A): two KM employees each with ONE tap that week, at KF's venue, judged GPS-only and
// outside the radius (gps yes, ip no, tap:gpsConflict), decided by base:gps-only-allow and
// base:no-evidence-review -- and the screen counts them as records (Partner 2) but in NO evidence
// figure: not in the totals' judged / distance-only / outside-venue, not on the people list's
// evidence, and the rule breakdown has one "partner" bucket of 2 and no row naming either rule.
// CONTROL: the same shape at KM's OWN venue by a third employee is counted everywhere. MUTANTS
// (measured, anomalies.sql): the site filter taken out of the people query's distance_only -> the
// two partner-only employees are listed; out of the totals' judged -> judged counts them.
func TestAnomalies_APartnerVenueRowAddsNoEvidenceFigure(t *testing.T) {
	w := newHWorld(t)
	day := Date{Year: 2026, Month: time.July, Day: 8}
	insert := func(employee uuid.UUID, site bool, sid, clock string) {
		t.Helper()
		var siteTenant, siteVenue, siteTag, venue, tag any
		if site {
			siteTenant, siteVenue, siteTag = w.kf, w.venue[w.kf], w.plaque[w.kf]
		} else {
			venue, tag = w.venue[w.km], w.plaque[w.km]
		}
		w.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, ip_match, gps_match, trust,
		                                  policy_layer, matched_sid, policy_context, location_id, tag_uid, site_tenant_id, site_location_id, site_tag_uid)
		        VALUES ($1, $2, $3, 'ok', 'nfc', 'in', true, 1000, false, true, 50, 'guardrail', $4, '{"tap:gpsConflict": true, "tap:ctrGap": 0}',
		                $5, $6, $7, $8, $9)`,
			w.km, employee, hLocal(t, "2026-07-08", clock), sid, venue, tag, siteTenant, siteVenue, siteTag)
	}
	e := w.staff[w.km]
	insert(e[0], true, "base:gps-only-allow", "09:00:00")
	insert(e[1], true, "base:no-evidence-review", "09:30:00")
	insert(e[2], false, "base:gps-only-allow", "10:00:00")
	a, err := w.r.Anomalies(w.ctx, w.km, ReportFilter{Day: day})
	if err != nil {
		t.Fatal(err)
	}
	if a.Totals.Records != 3 || a.Totals.Partner != 2 || a.Totals.Judged != 1 || a.Totals.DistanceOnly != 1 || a.Totals.OutsideVenue != 1 {
		t.Errorf("totals: records %d partner %d judged %d distance-only %d outside %d; want 3, 2, 1, 1, 1 (the partner rows in no evidence figure)",
			a.Totals.Records, a.Totals.Partner, a.Totals.Judged, a.Totals.DistanceOnly, a.Totals.OutsideVenue)
	}
	for _, p := range a.People {
		switch p.EmployeeID {
		case e[0], e[1]:
			t.Errorf("a partner-only employee is on the people list: %+v", p)
		case e[2]:
			if p.DistanceOnly != 1 || p.OutsideVenue != 1 {
				t.Errorf("CONTROL: the own-venue employee: %+v, want 1 and 1", p)
			}
		}
	}
	partner := 0
	for _, r := range a.Policies {
		if r.Partner {
			partner += r.Records
			continue
		}
		if r.SID == "base:no-evidence-review" || (r.SID == "base:gps-only-allow" && r.Records != 1) {
			t.Errorf("the rule breakdown names a rule of a partner-venue row: %+v", r)
		}
	}
	if partner != 2 {
		t.Errorf("the partner bucket holds %d records, want 2", partner)
	}
}

// TestAnomalies_ThePartnerBucketSaysNothingByItsPlace is the third review's ORTA (K-39): the rule
// breakdown's partner bucket must not say which rule decided the partner rows by WHERE it sits. KM's
// own two no-evidence rows beside two partner rows, once decided by gps-only-allow / ip-or-gps-ok and
// once (another week) by qr-requires-ip / the cross-tenant QR guardrail: the two breakdowns are
// identical -- KM's rule, then the bucket of 2, LAST. And the partner rows do not spend the row
// limit: 60 partner rows under 60 distinct rules beside KM's three rules leave the listing whole
// (three rules, the bucket of 60, nothing shortened). MUTANTS (measured): the query's site filter
// removed; the bucket put first; the bucket counted from the listed groups instead of the totals.
func TestAnomalies_ThePartnerBucketSaysNothingByItsPlace(t *testing.T) {
	w := newHWorld(t)
	insert := func(day, clock, sid string, site bool) {
		t.Helper()
		var siteTenant, siteVenue, siteTag, venue, tag any
		if site {
			siteTenant, siteVenue, siteTag = w.kf, w.venue[w.kf], w.plaque[w.kf]
		} else {
			venue, tag = w.venue[w.km], w.plaque[w.km]
		}
		w.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, trust,
		                                  policy_layer, matched_sid, location_id, tag_uid, site_tenant_id, site_location_id, site_tag_uid)
		        VALUES ($1, $2, $3, 'ok', 'nfc', 'in', true, 1000, 50, 'guardrail', $4, $5, $6, $7, $8, $9)`,
			w.km, w.staff[w.km][0], hLocal(t, day, clock), sid, venue, tag, siteTenant, siteVenue, siteTag)
	}
	breakdown := func(day Date) ([]AnomalyPolicyRule, bool) {
		t.Helper()
		a, err := w.r.Anomalies(w.ctx, w.km, ReportFilter{Day: day})
		if err != nil {
			t.Fatal(err)
		}
		return a.Policies, a.MorePolicies
	}
	for _, c := range []struct {
		day   string
		date  Date
		rules [2]string
	}{
		{"2026-06-03", Date{Year: 2026, Month: time.June, Day: 3}, [2]string{"base:gps-only-allow", "base:ip-or-gps-ok"}},
		{"2026-06-10", Date{Year: 2026, Month: time.June, Day: 10}, [2]string{"base:qr-requires-ip", "sys:cross-tenant-qr-site-ip"}},
	} {
		insert(c.day, "09:00:00", "base:no-evidence-review", false)
		insert(c.day, "09:10:00", "base:no-evidence-review", false)
		insert(c.day, "09:20:00", c.rules[0], true)
		insert(c.day, "09:30:00", c.rules[1], true)
		got, more := breakdown(c.date)
		want := []AnomalyPolicyRule{{SID: "base:no-evidence-review", Layer: "guardrail", Records: 2}, {Partner: true, Records: 2}}
		if more || !slices.Equal(got, want) {
			t.Errorf("partner rules %v: the breakdown is %+v (more %v), want %+v -- the same for any partner rule", c.rules, got, more, want)
		}
	}
	day := "2026-06-17"
	for i, sid := range []string{"base:ip-or-gps-ok", "base:ctr-gap-review", "base:no-evidence-review"} {
		insert(day, fmt.Sprintf("08:0%d:00", i), sid, false)
	}
	for i := range 60 {
		insert(day, fmt.Sprintf("10:%02d:00", i), fmt.Sprintf("sys:zz-partner-%02d", i), true)
	}
	got, more := breakdown(Date{Year: 2026, Month: time.June, Day: 17})
	if more || len(got) != 4 || !got[3].Partner || got[3].Records != 60 {
		t.Errorf("60 partner rules beside 3 own: %d rows (more %v), last %+v -- want 3 rules and the bucket of 60, whole", len(got), more, got[len(got)-1])
	}
}

// TestAnomalies_EveryEvidenceFigureLeavesThePartnerOut pins each evidence figure's filter on its
// own (the third review's D-2: five SQL mutants stayed green). One week, KM:
//
//	Aaa  own: GPS alone + outside the radius
//	Zzz  own: the same as Aaa's; partner: GPS alone + outside three times (more GPS-alone rows at
//	     the partner venue than at the own one, and more than Aaa's, so a ranking by them would
//	     flip both orders), and ip yes + outside FIVE times (on the partner's network)
//	own venue: Aaa's and Zzz's own rows; partner venue: their partner rows and a typed partner row
//
// Each person's figures are their own-venue ones (1 and 1, never 3 or 6); Aaa sorts before Zzz (the
// partner rows do not move the order); the own venue sorts before the partner venue; the totals'
// on-network figure is 0 (only partner rows were on the network); AnswerableOwn is the own rows
// with a snapshot (the typed partner row has none and is counted as the partner's, not as the
// business's unanswerable). MUTANTS (measured, anomalies.sql, one at a time): the filter out of
// outside_venue_on_network, out of the person select's distance_only, out of its outside_venue, out
// of the person ORDER, out of the venue ORDER, partner_answerable without its snapshot condition --
// each red.
func TestAnomalies_EveryEvidenceFigureLeavesThePartnerOut(t *testing.T) {
	w := newHWorld(t)
	aaa, zzz := w.staff[w.km][0], w.staff[w.km][1]
	w.exec(`UPDATE employees SET full_name = 'Aaa ' || left(id::text, 4) WHERE id = $1`, aaa)
	w.exec(`UPDATE employees SET full_name = 'Zzz ' || left(id::text, 4) WHERE id = $1`, zzz)
	n := 0
	insert := func(employee uuid.UUID, site, ipMatch bool, channel string) {
		t.Helper()
		n++
		var siteTenant, siteVenue, siteTag, venue, tag any
		if site {
			siteTenant, siteVenue, siteTag = w.kf, w.venue[w.kf], w.plaque[w.kf]
		} else {
			venue, tag = w.venue[w.km], w.plaque[w.km]
		}
		var sun, ctr, ip, gps, ctx any = true, 1000, ipMatch, true, `{"tap:gpsConflict": true, "tap:ctrGap": 0}`
		if channel == "manual" {
			sun, ctr, ip, gps, ctx, siteTag, tag = nil, nil, nil, nil, nil, nil, nil
		}
		w.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, ip_match, gps_match, trust,
		                                  policy_context, location_id, tag_uid, site_tenant_id, site_location_id, site_tag_uid)
		        VALUES ($1, $2, $3, 'ok', $4, 'in', $5, $6, $7, $8, 50, $9::jsonb, $10, $11, $12, $13, $14)`,
			w.km, employee, hLocal(t, "2026-05-13", fmt.Sprintf("%02d:%02d:00", 6+n/60, n%60)), channel, sun, ctr, ip, gps, ctx,
			venue, tag, siteTenant, siteVenue, siteTag)
	}
	insert(aaa, false, false, "nfc")
	insert(zzz, false, false, "nfc")
	for range 3 {
		insert(zzz, true, false, "nfc")
	}
	for range 5 {
		insert(zzz, true, true, "nfc")
	}
	insert(zzz, true, false, "manual")
	a, err := w.r.Anomalies(w.ctx, w.km, ReportFilter{Day: Date{Year: 2026, Month: time.May, Day: 13}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Totals.OutsideVenueOnNetwork != 0 {
		t.Errorf("on-network outside the venue: %d, want 0 (only the partner's rows were on its network)", a.Totals.OutsideVenueOnNetwork)
	}
	if a.Totals.AnswerableOwn != 2 || a.Totals.Partner != 9 {
		t.Errorf("answerable own %d, partner %d; want 2 and 9", a.Totals.AnswerableOwn, a.Totals.Partner)
	}
	var order []uuid.UUID
	for _, p := range a.People {
		if p.EmployeeID != aaa && p.EmployeeID != zzz {
			continue
		}
		order = append(order, p.EmployeeID)
		if p.DistanceOnly != 1 || p.OutsideVenue != 1 {
			t.Errorf("%s: distance-only %d, outside %d; want 1 and 1 (own venue only)", p.Name, p.DistanceOnly, p.OutsideVenue)
		}
	}
	if len(order) != 2 || order[0] != aaa {
		t.Errorf("the people list's order is %v, want Aaa then Zzz -- the partner rows moved it", order)
	}
	own, partner := -1, -1
	for i, v := range a.Venues {
		switch {
		case v.LocationID == w.venue[w.km]:
			own = i
		case v.LocationID == w.venue[w.kf]:
			partner = i
		}
	}
	if own < 0 || partner < 0 || own > partner {
		t.Errorf("venue order: own at %d, partner at %d -- want the own venue first (the partner's rows do not rank it)", own, partner)
	}
}

// TestAnomalies_ThePolicyListIsCutInSQLAndItsOrderIsTotal pins what the fourth review found
// unpinned (D-b, D-c, D-d). One week, KM: 52 own rules of one record each beside 3 partner rows --
// the list is cut by the QUERY (AnomalyRowCap rows and "more"), and the partner bucket, last, is 3:
// the totals' partner count, not the records the listed rules leave over (55 - 50 = 5). Another
// week: one sid under two layers, NULL and guardrail, one record each -- the NULL layer sorts
// first, so the order is total and does not hang on the plan. MUTANTS (measured): the bucket
// counted as Records minus the listed rules; the read limit one smaller (no row past the cap, so
// no "more"); policy_layer's NULLS FIRST removed -- each red.
func TestAnomalies_ThePolicyListIsCutInSQLAndItsOrderIsTotal(t *testing.T) {
	w := newHWorld(t)
	n := 0
	insert := func(day, sid string, layer any, site bool) {
		t.Helper()
		n++
		var siteTenant, siteVenue, siteTag, venue, tag any
		if site {
			siteTenant, siteVenue, siteTag = w.kf, w.venue[w.kf], w.plaque[w.kf]
		} else {
			venue, tag = w.venue[w.km], w.plaque[w.km]
		}
		w.exec(`INSERT INTO transactions (tenant_id, employee_id, occurred_at, verdict, channel, type, sun_valid, ctr, trust,
		                                  policy_layer, matched_sid, location_id, tag_uid, site_tenant_id, site_location_id, site_tag_uid)
		        VALUES ($1, $2, $3, 'ok', 'nfc', 'in', true, 1000, 50, $4, $5, $6, $7, $8, $9, $10)`,
			w.km, w.staff[w.km][0], hLocal(t, day, fmt.Sprintf("%02d:%02d:00", 6+n/60, n%60)), layer, sid,
			venue, tag, siteTenant, siteVenue, siteTag)
	}
	for i := range AnomalyRowCap + 2 {
		insert("2026-04-08", fmt.Sprintf("own:rule-%02d", i), "guardrail", false)
	}
	for range 3 {
		insert("2026-04-08", "base:gps-only-allow", "guardrail", true)
	}
	a, err := w.r.Anomalies(w.ctx, w.km, ReportFilter{Day: Date{Year: 2026, Month: time.April, Day: 8}})
	if err != nil {
		t.Fatal(err)
	}
	if !a.MorePolicies || len(a.Policies) != AnomalyRowCap+1 {
		t.Fatalf("52 own rules: %d rows listed (more %v), want %d and the bucket, and more", len(a.Policies), a.MorePolicies, AnomalyRowCap)
	}
	if last := a.Policies[AnomalyRowCap]; !last.Partner || last.Records != 3 {
		t.Errorf("the last row is %+v, want the partner bucket of 3 (the totals' partner count)", last)
	}
	for _, r := range a.Policies[:AnomalyRowCap] {
		if r.Partner || !strings.HasPrefix(r.SID, "own:rule-") {
			t.Errorf("a listed rule is %+v, want KM's own", r)
		}
	}

	insert("2026-04-15", "base:no-evidence-review", "guardrail", false)
	insert("2026-04-15", "base:no-evidence-review", nil, false)
	a, err = w.r.Anomalies(w.ctx, w.km, ReportFilter{Day: Date{Year: 2026, Month: time.April, Day: 15}})
	if err != nil {
		t.Fatal(err)
	}
	want := []AnomalyPolicyRule{{SID: "base:no-evidence-review", Records: 1}, {SID: "base:no-evidence-review", Layer: "guardrail", Records: 1}}
	if !slices.Equal(a.Policies, want) {
		t.Errorf("one sid under two layers: %+v, want %+v (the NULL layer first)", a.Policies, want)
	}
}
