package checkin

// holding_db_test.go -- M11 API-1b end to end through Service.Record, against real Postgres: an
// employee of KM tapping a plaque of KF, KF and KM peers in holding H (ADR 0027 §5-§7; plan §5
// API-1b (a)-(g)). The database half (the functions, the gate, the CHECKs, the M1/M2/M3 mutants)
// is internal/db's holdingtap_test.go; this file is the TAP PATH: which statement advances, what
// the decision reads, what row is written where.
//
// THE WORLD (newHoldingWorld), COMMITTED: tenants KF, KM, C, D (Europe/Malta -- K-36); holding H
// = {KF, KM}, holding H2 = {C, D} (K-33: C a member of ANOTHER holding); a KF venue on
// 203.0.113.0/24 at a known coordinate with a 09:00-17:00 shift, a KM venue and a C venue; KF
// plaques active (counter 100), lost, retired and unassigned, a KM plaque and a C plaque (100);
// one active employee per tenant and a deactivated one at KM; KF VIES verified with a logo. The
// memberships and holdings are written by the owner connection (tappa_app may not -- ADR 0027 §1)
// and removed at the end; the rest stays, like every committed fixture of this repository.
// Requires DATABASE_URL and DATABASE_MIGRATE_URL.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/geo"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/policy"
)

type holdingWorld struct {
	t     *testing.T
	ctx   context.Context
	data  *db.DB
	owner *pgx.Conn
	svc   *Service

	kf, km, c, d                      uuid.UUID
	h, h2                             uuid.UUID
	kfVenue, kmVenue, cVenue          uuid.UUID
	kfTag, kfLost, kfRetired, kfLoose string
	kmTag, cTag                       string
	kfEmp, kmEmp, kmGone, cEmp, cEmp2 uuid.UUID
	kfDigest                          string
	kfOnNet, elsewhere                netip.Addr
	kfPoint                           geo.Point
}

// newHoldingWorld commits the world and returns it with a real Service.
func newHoldingWorld(t *testing.T) *holdingWorld {
	t.Helper()
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres; the holding memberships are written by the owner -- ADR 0027 §1)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	cfg := &config.Config{
		Env: config.EnvDev, BaseURL: "http://localhost:8080", DatabaseURL: appDSN,
		SessionHMACKey: []byte("SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS"), InviteHMACKey: []byte("IIIIIIIIIIIIIIIIIIIIIIIIIIIIIIII"),
		TagKEK: make([]byte, 32), RetentionYears: 2, GPSRadiusMeters: 150, Debounce: 60 * time.Second, Freshness: 180 * time.Second,
	}
	data, err := db.New(ctx, cfg)
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(data, trail, cfg, quietLogger())
	if err != nil {
		t.Fatal(err)
	}
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	w := &holdingWorld{t: t, ctx: ctx, data: data, owner: owner, svc: svc,
		kfOnNet: netip.MustParseAddr("203.0.113.7"), elsewhere: netip.MustParseAddr("198.51.100.9"),
		kfPoint: geo.Point{Lat: 35.918, Lng: 14.489}}
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
	w.join(w.h, w.kf)
	w.join(w.h, w.km)
	w.join(w.h2, w.c)
	w.join(w.h2, w.d)
	w.kfVenue = w.venue(w.kf, "KF St Julians", `'{203.0.113.0/24}'`, "35.918", "14.489")
	w.kmVenue = w.venue(w.km, "KM Plant", `'{192.0.2.0/24}'`, "NULL", "NULL")
	w.cVenue = w.venue(w.c, "C Venue", `'{}'`, "NULL", "NULL")
	w.kfTag = w.tag(w.kf, &w.kfVenue, "active", 100)
	w.kfLost = w.tag(w.kf, &w.kfVenue, "lost", 100)
	w.kfRetired = w.tag(w.kf, &w.kfVenue, "retired", 100)
	w.kfLoose = w.tag(w.kf, nil, "unassigned", 0)
	w.kmTag = w.tag(w.km, &w.kmVenue, "active", 100)
	w.cTag = w.tag(w.c, &w.cVenue, "active", 100)
	w.kfEmp = w.employee(w.kf, "active")
	w.kmEmp = w.employee(w.km, "active")
	w.kmGone = w.employee(w.km, "deactivated")
	w.cEmp = w.employee(w.c, "active")
	w.cEmp2 = w.employee(w.c, "active")
	w.kfDigest = w.logo(w.kf)
	return w
}

func (w *holdingWorld) exec(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.owner.Exec(w.ctx, sql, args...); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
}

func (w *holdingWorld) tenant(name string) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	w.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, $2, $3, 'restaurant', 'multi')`,
		id, "api1b "+name, "VAT-API1B-"+id.String())
	return id
}

func (w *holdingWorld) holding() uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.owner.QueryRow(w.ctx, `INSERT INTO holdings (name) VALUES ($1) RETURNING id`, "api1b "+uuid.NewString()[:8]).Scan(&id); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *holdingWorld) join(h, tenant uuid.UUID) {
	w.t.Helper()
	w.exec(`INSERT INTO holding_members (holding_id, member_tenant_id, joined_at) VALUES ($1, $2, clock_timestamp() - interval '10 minutes')`, h, tenant)
}

// leave ends tenant's membership NOW (what op_remove_holding_member writes) -- on the owner
// connection, committed.
func (w *holdingWorld) leave(tenant uuid.UUID) {
	w.t.Helper()
	w.exec(`UPDATE holding_members SET left_at = clock_timestamp() WHERE member_tenant_id = $1 AND left_at IS NULL`, tenant)
}

func (w *holdingWorld) venue(tenant uuid.UUID, name, ips, lat, lng string) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	if err := w.owner.QueryRow(w.ctx, fmt.Sprintf(`INSERT INTO locations (tenant_id, name, static_ips, gps_lat, gps_lng, shift_start, shift_end)
	                                              VALUES ($1, $2, %s, %s, %s, '09:00', '17:00') RETURNING id`, ips, lat, lng), tenant, name).Scan(&id); err != nil {
		w.t.Fatal(err)
	}
	return id
}

func (w *holdingWorld) tag(tenant uuid.UUID, wall *uuid.UUID, status string, ctr int32) string {
	w.t.Helper()
	b := make([]byte, 7)
	key := make([]byte, 44)
	if _, err := rand.Read(b); err != nil {
		w.t.Fatal(err)
	}
	if _, err := rand.Read(key); err != nil {
		w.t.Fatal(err)
	}
	uid := strings.ToUpper(hex.EncodeToString(b))
	w.exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, $5, $6)`,
		uid, tenant, wall, key, ctr, status)
	return uid
}

func (w *holdingWorld) employee(tenant uuid.UUID, status string) uuid.UUID {
	w.t.Helper()
	id := uuid.New()
	w.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, $3, 'Holding Staff', $4, now())`,
		id, tenant, "ext-"+id.String()[:8], status)
	return id
}

func (w *holdingWorld) logo(tenant uuid.UUID) string {
	w.t.Helper()
	w.exec(`UPDATE tenants SET vat_verified = true WHERE id = $1`, tenant)
	admin := uuid.New()
	w.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role) VALUES ($1, $2, 'Owner', $3, $4, 'owner')`,
		admin, tenant, "o-"+admin.String()[:12]+"@example.test", "$2a$12$"+strings.Repeat("h", 53))
	logo := make([]byte, 24)
	if _, err := rand.Read(logo); err != nil {
		w.t.Fatal(err)
	}
	sum := sha256.Sum256(logo)
	digest := hex.EncodeToString(sum[:])
	w.exec(`INSERT INTO tenant_branding (tenant_id, accent, logo, logo_sha256, logo_mime, logo_width, logo_height, updated_by)
	        VALUES ($1, '1F5C41', $2, $3, 'image/png', 24, 24, $4)`, tenant, logo, digest, admin)
	return digest
}

func (w *holdingWorld) lastCtr(uid string) int64 {
	w.t.Helper()
	var n int64
	if err := w.owner.QueryRow(w.ctx, `SELECT last_ctr FROM tags WHERE uid = $1`, uid).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// rows counts transactions rows of tenant (the owner sees every tenant).
func (w *holdingWorld) rows(tenant uuid.UUID) int64 {
	w.t.Helper()
	var n int64
	if err := w.owner.QueryRow(w.ctx, `SELECT count(*) FROM transactions WHERE tenant_id = $1 OR site_tenant_id = $1`, tenant).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

// holdingRecord is a written row as the tests read it.
type holdingRecord struct {
	tenant                          uuid.UUID
	verdict, channel                string
	sid, kind                       *string
	sunValid                        *bool
	ctr                             *int32
	location, siteTenant, siteVenue *uuid.UUID
	tagUID, siteTag                 *string
}

func (w *holdingWorld) record(id uuid.UUID) holdingRecord {
	w.t.Helper()
	var r holdingRecord
	if err := w.owner.QueryRow(w.ctx, `SELECT tenant_id, verdict, channel, matched_sid, type, sun_valid, ctr, location_id, site_tenant_id, site_location_id, tag_uid, site_tag_uid
	                                    FROM transactions WHERE id = $1`, id).Scan(&r.tenant, &r.verdict, &r.channel, &r.sid, &r.kind, &r.sunValid, &r.ctr,
		&r.location, &r.siteTenant, &r.siteVenue, &r.tagUID, &r.siteTag); err != nil {
		w.t.Fatalf("read the record %s: %v", id, err)
	}
	return r
}

// nfc is a KM-shaped NFC request: the session's tenant and employee, a plaque, a counter,
// a verified CMAC (the page's half of SUN validity) and a source address.
func (w *holdingWorld) nfc(session, employee uuid.UUID, uid string, plaqueTenant uuid.UUID, ctr uint32, from netip.Addr) Request {
	return Request{SessionTenantID: session, EmployeeID: employee, TagUID: uid, Ctr: ctr, Channel: tap.ChannelNFC,
		CMACVerified: true, TagTenantID: plaqueTenant, SourceIP: from}
}

func (w *holdingWorld) qr(session, employee uuid.UUID, uid string, plaqueTenant uuid.UUID, from netip.Addr) Request {
	return Request{SessionTenantID: session, EmployeeID: employee, TagUID: uid, Channel: tap.ChannelQR,
		TagTenantID: plaqueTenant, SourceIP: from}
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func isSite(r holdingRecord, w *holdingWorld, uid string, venue *uuid.UUID) error {
	switch {
	case r.tenant != w.km:
		return fmt.Errorf("the row is in tenant %s, want the employer KM", r.tenant)
	case r.siteTenant == nil || *r.siteTenant != w.kf:
		return fmt.Errorf("site_tenant_id %v, want KF", r.siteTenant)
	case str(r.siteTag) != uid:
		return fmt.Errorf("site_tag_uid %q, want %q", str(r.siteTag), uid)
	case (venue == nil) != (r.siteVenue == nil) || (venue != nil && *venue != *r.siteVenue):
		return fmt.Errorf("site_location_id %v, want %v", r.siteVenue, venue)
	case r.location != nil || r.tagUID != nil:
		return fmt.Errorf("the local columns are set (location %v, tag %q) on a site row", r.location, str(r.tagUID))
	}
	return nil
}

// ------------------------------------------------------------------- (a) --

// TestHoldingTap_RecordLandsAtTheEmployerWithSiteColumns is plan §5 API-1b (a), K-30 and K-1a:
// KM's employee touches KF's active plaque from KF's own network at counter 101. The counter
// moves through holding_advance_tag_counter (KF's last_ctr 101), the decision is KM's (KM's
// baseline, base:ip-or-gps-ok -- ok, trust 70 with the GPS) on KF's evidence, and ONE row is
// written: in KM, site_tenant_id KF, site_location_id KF's venue, site_tag_uid the plaque,
// location_id and tag_uid NULL, sun_valid true, ctr 101. KF holds no row of it. The result names
// KF's venue and carries KF's logo (VIES verified).
func TestHoldingTap_RecordLandsAtTheEmployerWithSiteColumns(t *testing.T) {
	w := newHoldingWorld(t)
	req := w.nfc(w.km, w.kmEmp, w.kfTag, w.kf, 101, w.kfOnNet)
	req.GPS = &w.kfPoint
	res, err := w.svc.Record(w.ctx, req)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if res.Outcome != OutcomeRecorded || res.Decision.Verdict != tap.VerdictOK || res.Decision.Trust != 100 {
		t.Fatalf("outcome %s verdict %s trust %d sid %s, want a recorded ok at trust 100", res.Outcome, res.Decision.Verdict, res.Decision.Trust, res.Decision.MatchedSid)
	}
	r := w.record(res.TransactionID)
	if err := isSite(r, w, w.kfTag, &w.kfVenue); err != nil {
		t.Fatal(err)
	}
	if r.sunValid == nil || !*r.sunValid || r.ctr == nil || *r.ctr != 101 || r.channel != "nfc" {
		t.Errorf("sun_valid %v ctr %v channel %s, want true / 101 / nfc", r.sunValid, r.ctr, r.channel)
	}
	if got := w.lastCtr(w.kfTag); got != 101 {
		t.Errorf("KF's last_ctr %d, want 101", got)
	}
	var atKF int64
	if err := w.owner.QueryRow(w.ctx, `SELECT count(*) FROM transactions WHERE tenant_id = $1`, w.kf).Scan(&atKF); err != nil {
		t.Fatal(err)
	}
	if atKF != 0 {
		t.Errorf("KF holds %d rows; the record is the employer's (K-1a)", atKF)
	}
	if res.LocationName != "KF St Julians" {
		t.Errorf("the result names %q, want KF's venue (K-1c)", res.LocationName)
	}
	if res.SiteLogo == nil || res.SiteLogo.SHA256 != w.kfDigest || res.SiteLogo.TagUID != w.kfTag || res.SiteLogo.BusinessName == "" {
		t.Errorf("the result's site logo is %+v, want KF's digest, its name and the plaque", res.SiteLogo)
	}
	var ctxJSON []byte
	if err := w.owner.QueryRow(w.ctx, `SELECT policy_context FROM transactions WHERE id = $1`, res.TransactionID).Scan(&ctxJSON); err != nil {
		t.Fatal(err)
	}
	var keys map[string]any
	if err := json.Unmarshal(ctxJSON, &keys); err != nil {
		t.Fatal(err)
	}
	if keys["tap:ipMatch"] != true || keys["tap:gpsMatch"] != true {
		t.Errorf("policy_context %s: the decision did not read KF's evidence", ctxJSON)
	}
}

// ------------------------------------------------------------------- (b) --

// TestHoldingTap_OutsiderSessionOnAPeerPlaqueIsRefusedAndCounterUnchanged is K-33's first
// direction (plan §5 API-1b (b); ADR 0027 §12.1), C a member of H2: C's employee on KF's plaque
// gets sys:tenant-mismatch -- OutcomeForeignTenant, the 403 -- no row in ANY tenant, and KF's
// counter does not move (holding_advance_tag_counter answered not_peer in the same statement
// that would have moved it). By NFC and by QR.
func TestHoldingTap_OutsiderSessionOnAPeerPlaqueIsRefusedAndCounterUnchanged(t *testing.T) {
	w := newHoldingWorld(t)
	for _, req := range []Request{
		w.nfc(w.c, w.cEmp, w.kfTag, w.kf, 150, w.kfOnNet),
		w.qr(w.c, w.cEmp2, w.kfTag, w.kf, w.kfOnNet),
	} {
		res, err := w.svc.Record(w.ctx, req)
		if err != nil {
			t.Fatalf("%s: %v", req.Channel, err)
		}
		if res.Outcome != OutcomeForeignTenant || res.TransactionID != uuid.Nil {
			t.Errorf("%s: outcome %s, record %s; want the foreign-tenant refusal and no record", req.Channel, res.Outcome, res.TransactionID)
		}
	}
	if got := w.lastCtr(w.kfTag); got != 100 {
		t.Errorf("KF's last_ctr %d, want 100 (unmoved)", got)
	}
	if n := w.rows(w.c) + w.rows(w.kf); n != 0 {
		t.Errorf("%d rows in C or naming KF", n)
	}
}

// TestHoldingTap_PeerSessionOnAnOutsiderPlaqueLeavesItsCounter is K-33's second direction: KM's
// employee on C's plaque (C in ANOTHER holding) -- the refusal, no row anywhere, and C's
// last_ctr unchanged: the peer condition is in the advance's prev AND its UPDATE.
func TestHoldingTap_PeerSessionOnAnOutsiderPlaqueLeavesItsCounter(t *testing.T) {
	w := newHoldingWorld(t)
	res, err := w.svc.Record(w.ctx, w.nfc(w.km, w.kmEmp, w.cTag, w.c, 150, w.elsewhere))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeForeignTenant {
		t.Errorf("outcome %s, want the foreign-tenant refusal", res.Outcome)
	}
	if got := w.lastCtr(w.cTag); got != 100 {
		t.Errorf("C's last_ctr %d, want 100 (unmoved)", got)
	}
	if n := w.rows(w.km) + w.rows(w.c); n != 0 {
		t.Errorf("%d rows in KM or C", n)
	}
}

// ------------------------------------------------------------------- (c) --

// TestHoldingTap_ConcurrentSameCounterExactlyOneSUNValid is CLAUDE.md §4.4/§8 through the
// product path (plan §5 API-1b (c)): ONE KF plaque, ONE counter, twenty Record calls at once --
// HALF by KF's own employees (the single-tenant statement), HALF by KM's (the holding statement)
// -- and exactly ONE record carries sun_valid true; every other is a recorded sys:sun-invalid
// reject (§4.6: none lost). Distinct people, so the debounce plays no part. -race.
func TestHoldingTap_ConcurrentSameCounterExactlyOneSUNValid(t *testing.T) {
	w := newHoldingWorld(t)
	const racers = 20
	type racer struct {
		req Request
	}
	var rs []racer
	for i := 0; i < racers; i++ {
		if i%2 == 0 {
			rs = append(rs, racer{w.nfc(w.kf, w.employee(w.kf, "active"), w.kfTag, w.kf, 777, w.kfOnNet)})
		} else {
			rs = append(rs, racer{w.nfc(w.km, w.employee(w.km, "active"), w.kfTag, w.kf, 777, w.kfOnNet)})
		}
	}
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		valid    int
		byTenant = map[uuid.UUID]int{}
		failed   []error
		ids      []uuid.UUID
		start    = make(chan struct{})
	)
	for _, r := range rs {
		wg.Add(1)
		go func(req Request) {
			defer wg.Done()
			<-start
			res, err := w.svc.Record(w.ctx, req)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed = append(failed, err)
				return
			}
			ids = append(ids, res.TransactionID)
			if sv, _ := res.Decision.PolicyContext[policy.CtxTapSunValid].(bool); sv {
				valid++
				byTenant[req.SessionTenantID]++
			}
		}(r.req)
	}
	close(start)
	wg.Wait()
	for _, e := range failed {
		t.Errorf("a racer failed: %v", e)
	}
	if valid != 1 {
		t.Errorf("%d of %d racers were SUN-valid on one counter (KF path %d, holding path %d), want exactly 1", valid, racers, byTenant[w.kf], byTenant[w.km])
	}
	if len(ids) != racers {
		t.Errorf("%d records for %d taps (§4.6: every decided tap is a row)", len(ids), racers)
	}
	if got := w.lastCtr(w.kfTag); got != 777 {
		t.Errorf("KF's last_ctr %d, want 777", got)
	}
}

// ------------------------------------------------------------------- (d) --

// TestHoldingTap_PersonDebounceSpansBothTenantsPlaques is ADR 0027 §7 (ADR 0006's note; plan §5
// API-1b (d)): the debounce is the PERSON's and lives at the employer, so KM's employee touching
// KM's own plaque and then, inside the window, KF's -- and the other way round -- is ignored the
// second time, recorded at KM (the KF one with its site columns).
func TestHoldingTap_PersonDebounceSpansBothTenantsPlaques(t *testing.T) {
	w := newHoldingWorld(t)
	for _, c := range []struct {
		name          string
		first, second Request
	}{
		{"KM's plaque, then KF's", w.nfc(w.km, w.kmEmp, w.kmTag, w.km, 101, w.elsewhere), w.nfc(w.km, w.kmEmp, w.kfTag, w.kf, 101, w.kfOnNet)},
	} {
		first, err := w.svc.Record(w.ctx, c.first)
		if err != nil || first.Decision.Verdict == tap.VerdictIgnored {
			t.Fatalf("%s: the first tap %v / %s", c.name, err, first.Decision.Verdict)
		}
		second, err := w.svc.Record(w.ctx, c.second)
		if err != nil {
			t.Fatal(err)
		}
		if second.Decision.Verdict != tap.VerdictIgnored || second.Decision.MatchedSid != "sys:person-debounce" {
			t.Errorf("%s: the second tap %s by %s, want ignored by sys:person-debounce", c.name, second.Decision.Verdict, second.Decision.MatchedSid)
		}
		if err := isSite(w.record(second.TransactionID), w, w.kfTag, &w.kfVenue); err != nil {
			t.Errorf("%s: the ignored row: %v", c.name, err)
		}
	}
	other := w.employee(w.km, "active")
	first, err := w.svc.Record(w.ctx, w.nfc(w.km, other, w.kfTag, w.kf, 102, w.kfOnNet))
	if err != nil || first.Decision.Verdict == tap.VerdictIgnored {
		t.Fatalf("KF's plaque first: %v / %s", err, first.Decision.Verdict)
	}
	second, err := w.svc.Record(w.ctx, w.nfc(w.km, other, w.kmTag, w.km, 102, w.elsewhere))
	if err != nil {
		t.Fatal(err)
	}
	if second.Decision.Verdict != tap.VerdictIgnored {
		t.Errorf("KF's plaque, then KM's: the second tap %s by %s, want ignored", second.Decision.Verdict, second.Decision.MatchedSid)
	}
}

// seedSiteIn writes, as tappa_app in KM's context, KM's employee's check-in on KF's plaque
// `ago` in the past (occurred_at and created_at) -- a row of exactly the shape Record writes, through
// the same gate and CHECKs; the server clock cannot be moved through Record itself.
func (w *holdingWorld) seedSiteIn(employee uuid.UUID, ago time.Duration) {
	w.t.Helper()
	err := w.data.WithTenant(w.ctx, w.km, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, occurred_at, created_at, verdict, channel, type, sun_valid, ctr,
		                                                 site_tenant_id, site_location_id, site_tag_uid)
		                      VALUES ($1, $2, now() - $3::interval, now() - $3::interval, 'flag', 'nfc', 'in', true, 100, $4, $5, $6)`,
			w.km, employee, fmt.Sprintf("%d seconds", int(ago.Seconds())), w.kf, w.kfVenue, w.kfTag)
		return e
	})
	if err != nil {
		w.t.Fatalf("seed a check-in on KF's plaque: %v", err)
	}
}

// TestHoldingTap_DirectionChainStaysAtTheEmployer is ADR 0027 §7's direction rule (ADR 0008
// note; plan §5 API-1b (d)): the in/out chain is the employer's. A check-in on KF's plaque
// (a site row at KM) and, later, a tap on KM's own plaque: the second is OUT. And the other way:
// a KM check-in, later a tap on KF's plaque: OUT, with the site columns.
func TestHoldingTap_DirectionChainStaysAtTheEmployer(t *testing.T) {
	w := newHoldingWorld(t)
	w.seedSiteIn(w.kmEmp, 5*time.Minute)
	res, err := w.svc.Record(w.ctx, w.nfc(w.km, w.kmEmp, w.kmTag, w.km, 101, w.elsewhere))
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.Type == nil || *res.Decision.Type != tap.TypeOut {
		t.Errorf("in on KF's plaque, then KM's: %v (%s), want out", res.Decision.Type, res.Decision.Verdict)
	}
	other := w.employee(w.km, "active")
	err = w.data.WithTenant(w.ctx, w.km, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, occurred_at, created_at, verdict, channel, type, sun_valid, ctr, location_id, tag_uid)
		                      VALUES ($1, $2, now() - interval '5 minutes', now() - interval '5 minutes', 'flag', 'nfc', 'in', true, 100, $3, $4)`,
			w.km, other, w.kmVenue, w.kmTag)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err = w.svc.Record(w.ctx, w.nfc(w.km, other, w.kfTag, w.kf, 103, w.kfOnNet))
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.Type == nil || *res.Decision.Type != tap.TypeOut {
		t.Errorf("in on KM's plaque, then KF's: %v (%s), want out", res.Decision.Type, res.Decision.Verdict)
	}
	if err := isSite(w.record(res.TransactionID), w, w.kfTag, &w.kfVenue); err != nil {
		t.Error(err)
	}
}

// ------------------------------------------------------------------- (e) --

// TestHoldingTap_SiteLocationExceptionsDoNotApply is K-1a (ADR 0027 §7; plan §5 API-1b (e)): KF
// has its OWN exception at its venue -- a tenant-layer allow on location/<KF venue>, the Rusty
// Bar shape -- and it decides KF's employee's evidence-less tap there (ok, by KF's rule); KM's
// employee's identical tap is decided by KM's policy set, where no such rule exists (flag, by
// KM's baseline). The site's rules never reach a peer's employee.
func TestHoldingTap_SiteLocationExceptionsDoNotApply(t *testing.T) {
	w := newHoldingWorld(t)
	doc := policy.Document{Version: policy.BaselineVersion, Name: "KF venue exception", Statements: []policy.Statement{{
		Sid: "kf-venue-allow", Effect: policy.EffectAllow, Action: []policy.Action{policy.ActionTapRecord},
		Resource: []string{"location/" + w.kfVenue.String()}, Reason: "KF trusts every tap at this venue"}}}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	err = w.data.WithTenant(w.ctx, w.kf, func(ctx context.Context, tx pgx.Tx) error {
		var id uuid.UUID
		if e := tx.QueryRow(ctx, `INSERT INTO policies (tenant_id, name, layer) VALUES ($1, 'KF venue exception', 'tenant') RETURNING id`, w.kf).Scan(&id); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO policy_versions (tenant_id, policy_id, version_no, document) VALUES ($1, $2, 1, $3)`, w.kf, id, raw)
		return e
	})
	if err != nil {
		t.Fatalf("seed KF's exception: %v", err)
	}
	kfRes, err := w.svc.Record(w.ctx, w.nfc(w.kf, w.kfEmp, w.kfTag, w.kf, 101, w.elsewhere))
	if err != nil {
		t.Fatal(err)
	}
	if kfRes.Decision.MatchedSid != "kf-venue-allow" || kfRes.Decision.Verdict != tap.VerdictOK {
		t.Fatalf("CONTROL: KF's own employee: %s by %s, want ok by KF's exception", kfRes.Decision.Verdict, kfRes.Decision.MatchedSid)
	}
	kmRes, err := w.svc.Record(w.ctx, w.nfc(w.km, w.kmEmp, w.kfTag, w.kf, 102, w.elsewhere))
	if err != nil {
		t.Fatal(err)
	}
	if kmRes.Decision.MatchedSid == "kf-venue-allow" || kmRes.Decision.Verdict != tap.VerdictFlag {
		t.Errorf("KM's employee at the same venue: %s by %s, want flag by KM's own set", kmRes.Decision.Verdict, kmRes.Decision.MatchedSid)
	}
}

// ------------------------------------------------------------------- (f) --

// TestHoldingTap_CrossTenantQRNeedsTheSiteIP is K-1g end to end (plan §5 API-1b (f)): KM's
// employee scanning KF's QR from KF's network is ok (KM's baseline, the site's IP); from anywhere
// else -- even with GPS at KF's venue -- it is flagged by sys:cross-tenant-qr-site-ip, recorded at
// KM with the site columns.
func TestHoldingTap_CrossTenantQRNeedsTheSiteIP(t *testing.T) {
	w := newHoldingWorld(t)
	on, err := w.svc.Record(w.ctx, w.qr(w.km, w.kmEmp, w.kfTag, w.kf, w.kfOnNet))
	if err != nil {
		t.Fatal(err)
	}
	if on.Decision.Verdict != tap.VerdictOK {
		t.Errorf("from KF's network: %s by %s, want ok", on.Decision.Verdict, on.Decision.MatchedSid)
	}
	other := w.employee(w.km, "active")
	off := w.qr(w.km, other, w.kfTag, w.kf, w.elsewhere)
	off.GPS = &w.kfPoint
	res, err := w.svc.Record(w.ctx, off)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.Verdict != tap.VerdictFlag || res.Decision.MatchedSid != "sys:cross-tenant-qr-site-ip" {
		t.Errorf("from elsewhere, GPS at the venue: %s by %s, want flag by sys:cross-tenant-qr-site-ip", res.Decision.Verdict, res.Decision.MatchedSid)
	}
	if err := isSite(w.record(res.TransactionID), w, w.kfTag, &w.kfVenue); err != nil {
		t.Error(err)
	}
}

// ------------------------------------------------------------------- (g) --

// TestHoldingAdvance_EqualCounterIsReplayWithSiteColumns (plan §5 API-1b (g)): KM's employee on
// KF's plaque at counter 100 -- equal to last_ctr -- is holding_advance_tag_counter's replay: a
// RECORDED sys:sun-invalid reject at KM with the site columns, sun_valid false, KF's counter
// unmoved. Not a 403.
func TestHoldingAdvance_EqualCounterIsReplayWithSiteColumns(t *testing.T) {
	w := newHoldingWorld(t)
	res, err := w.svc.Record(w.ctx, w.nfc(w.km, w.kmEmp, w.kfTag, w.kf, 100, w.kfOnNet))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeRecorded || res.Decision.Verdict != tap.VerdictReject || res.Decision.MatchedSid != "sys:sun-invalid" {
		t.Fatalf("%s / %s by %s, want a recorded sys:sun-invalid reject", res.Outcome, res.Decision.Verdict, res.Decision.MatchedSid)
	}
	r := w.record(res.TransactionID)
	if err := isSite(r, w, w.kfTag, &w.kfVenue); err != nil {
		t.Error(err)
	}
	if r.sunValid == nil || *r.sunValid {
		t.Errorf("sun_valid %v, want false", r.sunValid)
	}
	if got := w.lastCtr(w.kfTag); got != 100 {
		t.Errorf("KF's last_ctr %d, want 100", got)
	}
}

// TestHoldingTap_LostPlaqueRefusalRecordedAtEmployerSeenAtSite (plan §5 API-1b (g); ADR 0027 §5):
// KM's employee on KF's LOST plaque -- the advance is not attempted (a dead plaque never reaches
// the counter), SameHolding comes from the strict holding_tap_site, and the refusal is §5 row 1's
// at KM: sys:tag-not-active with the lost-plaque alert, the site columns set, KF's counter unmoved,
// and the alert in KM's OWN audit trail (no cross-tenant audit -- ADR 0027 §5). The same for KF's
// UNASSIGNED plaque (no wall: site_location_id NULL, K-30). "Seen at the site" -- KF's
// refused_tag_not_active row in site_punches and its panel alert -- is API-1c's: the projection
// is born there, from these rows.
func TestHoldingTap_LostPlaqueRefusalRecordedAtEmployerSeenAtSite(t *testing.T) {
	w := newHoldingWorld(t)
	res, err := w.svc.Record(w.ctx, w.nfc(w.km, w.kmEmp, w.kfLost, w.kf, 150, w.kfOnNet))
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.Verdict != tap.VerdictReject || res.Decision.MatchedSid != "sys:tag-not-active" || !res.Decision.Security {
		t.Fatalf("lost: %s by %s security=%v, want a reject by sys:tag-not-active with its alert", res.Decision.Verdict, res.Decision.MatchedSid, res.Decision.Security)
	}
	if err := isSite(w.record(res.TransactionID), w, w.kfLost, &w.kfVenue); err != nil {
		t.Error(err)
	}
	if got := w.lastCtr(w.kfLost); got != 100 {
		t.Errorf("the lost plaque's counter moved to %d", got)
	}
	var alerts int64
	if err := w.owner.QueryRow(w.ctx, `SELECT count(*) FROM audit_log WHERE action = $1 AND target = $2`, ActionTapSecurityAlert, res.TransactionID.String()).Scan(&alerts); err != nil {
		t.Fatal(err)
	}
	var atKM int64
	if err := w.owner.QueryRow(w.ctx, `SELECT count(*) FROM audit_log WHERE action = $1 AND target = $2 AND tenant_id = $3`, ActionTapSecurityAlert, res.TransactionID.String(), w.km).Scan(&atKM); err != nil {
		t.Fatal(err)
	}
	if alerts != 1 || atKM != 1 {
		t.Errorf("%d security alert rows (%d in KM), want exactly one, in KM's own trail", alerts, atKM)
	}
	other := w.employee(w.km, "active")
	loose, err := w.svc.Record(w.ctx, w.nfc(w.km, other, w.kfLoose, w.kf, 1, w.kfOnNet))
	if err != nil {
		t.Fatal(err)
	}
	if loose.Decision.Verdict != tap.VerdictReject || loose.Decision.MatchedSid != "sys:tag-not-active" {
		t.Errorf("unassigned: %s by %s", loose.Decision.Verdict, loose.Decision.MatchedSid)
	}
	if err := isSite(w.record(loose.TransactionID), w, w.kfLoose, nil); err != nil {
		t.Errorf("unassigned: %v", err)
	}
}

// TestHoldingAdvance_InactivePlaqueIsPeerInactiveNot403 (plan §5 API-1b (g)): the resolver read
// KF's plaque active, and by the advance it is RETIRED (a hook commits the change between the two
// -- the race staged, not waited for): holding_advance_tag_counter answers peer_inactive, the two
// tenants are still peers, and the refusal is RECORDED at KM -- sys:tag-not-active, the site
// columns -- not the 403 a not_peer would be. MUTANT (measured, ADR 0027's API-1b note):
// peer_inactive read as "not a peer" turns this into OutcomeForeignTenant and no row.
func TestHoldingAdvance_InactivePlaqueIsPeerInactiveNot403(t *testing.T) {
	w := newHoldingWorld(t)
	uid := w.tag(w.kf, &w.kfVenue, "active", 100)
	w.svc.hooks.afterResolve = func(context.Context) {
		w.exec(`UPDATE tags SET status = 'retired', retired_at = now() WHERE uid = $1`, uid)
	}
	res, err := w.svc.Record(w.ctx, w.nfc(w.km, w.kmEmp, uid, w.kf, 101, w.kfOnNet))
	w.svc.hooks = recordHooks{}
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeRecorded || res.Decision.Verdict != tap.VerdictReject || res.Decision.MatchedSid != "sys:tag-not-active" {
		t.Fatalf("%s / %s by %s, want a recorded reject by sys:tag-not-active", res.Outcome, res.Decision.Verdict, res.Decision.MatchedSid)
	}
	if err := isSite(w.record(res.TransactionID), w, uid, &w.kfVenue); err != nil {
		t.Error(err)
	}
	if got := w.lastCtr(uid); got != 100 {
		t.Errorf("the retired plaque's counter moved to %d", got)
	}
}

// TestHoldingAdvance_DecideSeesTheCurrentStatus (plan §5 API-1b (g); ADR 0027 §5: "bayat
// tagRow.Status değil"): the plaque is active at the resolver and LOST by the advance (the hook,
// as above). The decision reads the status the advance took at its lock -- sys:tag-not-active WITH
// the lost-plaque alert -- not the resolver's stale "active", which would have made it a plain
// sys:sun-invalid (the counter did not move) and lost the alert. MUTANT (measured): Decide given
// tagRow.Status -> sys:sun-invalid, no alert.
func TestHoldingAdvance_DecideSeesTheCurrentStatus(t *testing.T) {
	w := newHoldingWorld(t)
	uid := w.tag(w.kf, &w.kfVenue, "active", 100)
	w.svc.hooks.afterResolve = func(context.Context) {
		w.exec(`UPDATE tags SET status = 'lost' WHERE uid = $1`, uid)
	}
	res, err := w.svc.Record(w.ctx, w.nfc(w.km, w.kmEmp, uid, w.kf, 101, w.kfOnNet))
	w.svc.hooks = recordHooks{}
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision.MatchedSid != "sys:tag-not-active" || !res.Decision.Security {
		t.Errorf("%s by %s security=%v, want sys:tag-not-active with the lost-plaque alert", res.Decision.Verdict, res.Decision.MatchedSid, res.Decision.Security)
	}
}

// TestHoldingAdvance_CounterGapReachesTheEmployersReview is the user's decision K-1i (2026-10-14;
// ADR 0027 §2, the third-eye audit's measurement made a regression): KF's plaque is at 100 and KM's
// employee touches it at 150 from KF's own network. holding_advance_tag_counter moves the counter
// AND reports the 49 reads that never reached the server (150 - 100 - 1, the expression
// store.AdvanceTagCounter uses for an own plaque), so KM's baseline judges the holding tap as it
// judges any tap: base:ctr-gap-review flags it, though the address alone would have been ok. The
// row carries tap:ctrGap 49 in its policy_context -- the gap's one durable trace, the source
// API-1c's KF projection reads (ADR 0027 §13).
//
// THEN KF's own employee touches the same plaque at 151: store.AdvanceTagCounter's gap is 0 -- the
// 49 reads were consumed by the holding tap, the old last_ctr is gone after that statement, and
// the single-tenant path has nothing to see again. That is CORRECT, not a loss: the signal was
// caught once, at the tap that skipped the reads, by the employer who decided it (K-1i: "URL
// toplama sinyali karar veren işverene ulaşsın"). MUTANT (measured, ADR 0027's API-1b note):
// ctr_gap always 0 (in 00045's statement or in advanceAtAPeer) turns the first tap into ok by
// base:ip-or-gps-ok.
func TestHoldingAdvance_CounterGapReachesTheEmployersReview(t *testing.T) {
	w := newHoldingWorld(t)
	gapOf := func(id uuid.UUID) any {
		t.Helper()
		var raw []byte
		if err := w.owner.QueryRow(w.ctx, `SELECT policy_context FROM transactions WHERE id = $1`, id).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var keys map[string]any
		if err := json.Unmarshal(raw, &keys); err != nil {
			t.Fatal(err)
		}
		return keys["tap:ctrGap"]
	}
	res, err := w.svc.Record(w.ctx, w.nfc(w.km, w.kmEmp, w.kfTag, w.kf, 150, w.kfOnNet))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeRecorded || res.Decision.Verdict != tap.VerdictFlag || res.Decision.MatchedSid != policy.SidCtrGapReview {
		t.Fatalf("KM at 150 on KF's 100: %s / %s by %s, want a recorded flag by %s", res.Outcome, res.Decision.Verdict, res.Decision.MatchedSid, policy.SidCtrGapReview)
	}
	if err := isSite(w.record(res.TransactionID), w, w.kfTag, &w.kfVenue); err != nil {
		t.Error(err)
	}
	if g := gapOf(res.TransactionID); g != float64(49) {
		t.Errorf("tap:ctrGap %v in the holding row's policy_context, want 49", g)
	}
	if got := w.lastCtr(w.kfTag); got != 150 {
		t.Errorf("KF's last_ctr %d, want 150", got)
	}

	own, err := w.svc.Record(w.ctx, w.nfc(w.kf, w.kfEmp, w.kfTag, w.kf, 151, w.kfOnNet))
	if err != nil {
		t.Fatal(err)
	}
	if own.Outcome != OutcomeRecorded || own.Decision.Verdict != tap.VerdictOK || own.Decision.MatchedSid == policy.SidCtrGapReview {
		t.Errorf("KF at 151: %s / %s by %s, want an ok -- the gap was already caught at KM's tap", own.Outcome, own.Decision.Verdict, own.Decision.MatchedSid)
	}
	if g := gapOf(own.TransactionID); g != float64(0) {
		t.Errorf("tap:ctrGap %v on KF's own tap at 151, want 0", g)
	}
}

// TestHoldingWindow_LeftBetweenAdvanceAndGatherFlagsWithoutSiteEvidence is K-29's "advanced, then
// the strict read finds nothing" (plan §5 API-1b (g)): KM's employee touches KF's plaque from KF's
// network, the counter advances (a peer), and a hook ends KM's membership BEFORE the gather. The
// strict holding_tap_site then answers nothing, so the decision has NO site evidence -- flag,
// trust 20, though the address was KF's -- and the row is WRITTEN (the gate's tolerant arm (i):
// sun_valid, the counter reached), its site venue NULL. Not an error.
func TestHoldingWindow_LeftBetweenAdvanceAndGatherFlagsWithoutSiteEvidence(t *testing.T) {
	w := newHoldingWorld(t)
	w.svc.hooks.afterAdvance = func(context.Context) { w.leave(w.km) }
	res, err := w.svc.Record(w.ctx, w.nfc(w.km, w.kmEmp, w.kfTag, w.kf, 101, w.kfOnNet))
	w.svc.hooks = recordHooks{}
	if err != nil {
		t.Fatalf("Record: %v -- a membership ending after the advance must cost the evidence, not the record", err)
	}
	if res.Outcome != OutcomeRecorded || res.Decision.Verdict != tap.VerdictFlag || res.Decision.IPMatch || res.Decision.Trust != tap.TrustBase {
		t.Errorf("%s / %s by %s ip=%v trust=%d, want a recorded flag with no site evidence", res.Outcome, res.Decision.Verdict, res.Decision.MatchedSid, res.Decision.IPMatch, res.Decision.Trust)
	}
	r := w.record(res.TransactionID)
	if err := isSite(r, w, w.kfTag, nil); err != nil {
		t.Error(err)
	}
	if r.sunValid == nil || !*r.sunValid || w.lastCtr(w.kfTag) != 101 {
		t.Errorf("sun_valid %v, last_ctr %d; want true, 101", r.sunValid, w.lastCtr(w.kfTag))
	}
	if res.SiteLogo != nil {
		t.Error("a site logo was carried for a tap whose site read found nothing")
	}
}

// TestHoldingWindow_AdvancedBeforeLeftWrittenAfterIsRecorded (plan §5 API-1b (g); K-29 arm (i)):
// the counter advances while KM is a member, and KM leaves between the decision and the INSERT
// (the hook) -- the row is written, the decision's ok intact.
func TestHoldingWindow_AdvancedBeforeLeftWrittenAfterIsRecorded(t *testing.T) {
	w := newHoldingWorld(t)
	w.svc.hooks.beforeWrite = func(context.Context) { w.leave(w.km) }
	res, err := w.svc.Record(w.ctx, w.nfc(w.km, w.kmEmp, w.kfTag, w.kf, 101, w.kfOnNet))
	w.svc.hooks = recordHooks{}
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if res.Outcome != OutcomeRecorded || res.Decision.Verdict != tap.VerdictOK {
		t.Errorf("%s / %s, want a recorded ok", res.Outcome, res.Decision.Verdict)
	}
	if err := isSite(w.record(res.TransactionID), w, w.kfTag, &w.kfVenue); err != nil {
		t.Error(err)
	}
}

// TestHoldingWindow_RefusalsSurviveTheLeftRace (plan §5 API-1b (g); K-29 arm (ii)): three
// refusals decided while KM is a member -- a replay (an equal counter), peer_inactive (the plaque
// retired by the advance) and a lost plaque -- each with KM's membership ending between the
// decision and the INSERT. Every one is WRITTEN (§4.6).
func TestHoldingWindow_RefusalsSurviveTheLeftRace(t *testing.T) {
	for _, c := range []struct {
		name  string
		build func(w *holdingWorld) (Request, func(context.Context))
		sid   string
	}{
		{"replay", func(w *holdingWorld) (Request, func(context.Context)) {
			return w.nfc(w.km, w.kmEmp, w.kfTag, w.kf, 100, w.kfOnNet), nil
		}, "sys:sun-invalid"},
		{"peer_inactive", func(w *holdingWorld) (Request, func(context.Context)) {
			uid := w.tag(w.kf, &w.kfVenue, "active", 100)
			return w.nfc(w.km, w.kmEmp, uid, w.kf, 101, w.kfOnNet), func(context.Context) {
				w.exec(`UPDATE tags SET status = 'retired', retired_at = now() WHERE uid = $1`, uid)
			}
		}, "sys:tag-not-active"},
		{"lost plaque", func(w *holdingWorld) (Request, func(context.Context)) {
			return w.nfc(w.km, w.kmEmp, w.kfLost, w.kf, 150, w.kfOnNet), nil
		}, "sys:tag-not-active"},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newHoldingWorld(t)
			req, afterResolve := c.build(w)
			w.svc.hooks = recordHooks{afterResolve: afterResolve, beforeWrite: func(context.Context) { w.leave(w.km) }}
			res, err := w.svc.Record(w.ctx, req)
			w.svc.hooks = recordHooks{}
			if err != nil {
				t.Fatalf("Record: %v -- a refusal must survive the membership ending before its INSERT", err)
			}
			if res.Outcome != OutcomeRecorded || res.Decision.Verdict != tap.VerdictReject || res.Decision.MatchedSid != c.sid {
				t.Errorf("%s / %s by %s, want a recorded reject by %s", res.Outcome, res.Decision.Verdict, res.Decision.MatchedSid, c.sid)
			}
		})
	}
}

// TestHoldingWindow_QRRefusalSurvivesTheLeftRace (plan §5 API-1b (g)): a DEACTIVATED KM
// employee's QR scan of KF's plaque -- §5 row 4, the reject with its security alert -- with KM's
// membership ending between the decision and the INSERT: written (arm (ii)), the alert in KM's trail.
func TestHoldingWindow_QRRefusalSurvivesTheLeftRace(t *testing.T) {
	w := newHoldingWorld(t)
	w.svc.hooks.beforeWrite = func(context.Context) { w.leave(w.km) }
	res, err := w.svc.Record(w.ctx, w.qr(w.km, w.kmGone, w.kfTag, w.kf, w.kfOnNet))
	w.svc.hooks = recordHooks{}
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if res.Decision.Verdict != tap.VerdictReject || res.Decision.MatchedSid != "sys:employee-deactivated" || !res.Decision.Security {
		t.Errorf("%s by %s security=%v, want the deactivated reject with its alert", res.Decision.Verdict, res.Decision.MatchedSid, res.Decision.Security)
	}
	if err := isSite(w.record(res.TransactionID), w, w.kfTag, &w.kfVenue); err != nil {
		t.Error(err)
	}
}

// TestHoldingWindow_QRDecisionAfterLeftIs403 (plan §5 API-1b (g); K-29: QR is strict): KM left H
// 10 seconds ago -- inside the 60-second window the gate grants refusals and advanced taps --
// and a QR scan of KF's plaque is decided not a peer: OutcomeForeignTenant, no record. And the
// named race (ADR 0027 §6): decided while KM is a member, KM leaving before the INSERT -- the
// row is refused, ErrSiteMembershipEnded, nothing written; the retry is the 403.
func TestHoldingWindow_QRDecisionAfterLeftIs403(t *testing.T) {
	w := newHoldingWorld(t)
	w.svc.hooks.beforeWrite = func(context.Context) { w.leave(w.km) }
	_, err := w.svc.Record(w.ctx, w.qr(w.km, w.kmEmp, w.kfTag, w.kf, w.kfOnNet))
	w.svc.hooks = recordHooks{}
	if !errors.Is(err, ErrSiteMembershipEnded) {
		t.Fatalf("the race: %v, want ErrSiteMembershipEnded", err)
	}
	if n := w.rows(w.km); n != 0 {
		t.Errorf("%d rows after the refused INSERT", n)
	}
	res, err := w.svc.Record(w.ctx, w.qr(w.km, w.kmEmp, w.kfTag, w.kf, w.kfOnNet))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeForeignTenant || w.rows(w.km) != 0 {
		t.Errorf("the retry after left_at: %s, %d rows; want the foreign-tenant refusal and none", res.Outcome, w.rows(w.km))
	}
}

// TestHoldingWindow_ToleranceIsAtLeastTwiceTheRequestTimeout (ADR 0027 §6): the 60 seconds the
// gate tolerates after left_at -- every interval holding_peer's body writes -- is at least twice
// httpx.RequestTimeout, so a tap whose request ran to its deadline still lands inside the window.
// A longer timeout turns this red.
func TestHoldingWindow_ToleranceIsAtLeastTwiceTheRequestTimeout(t *testing.T) {
	w := newHoldingWorld(t)
	var src string
	if err := w.owner.QueryRow(w.ctx, `SELECT prosrc FROM pg_proc WHERE oid = 'public.holding_peer(uuid, uuid, text)'::regprocedure`).Scan(&src); err != nil {
		t.Fatal(err)
	}
	n := strings.Count(src, "interval '60 seconds'")
	if n == 0 {
		t.Fatal("anti-vacuity: holding_peer names no interval '60 seconds'")
	}
	if strings.Count(src, "interval '") != n {
		t.Errorf("holding_peer names an interval other than 60 seconds")
	}
	if tolerance := 60 * time.Second; tolerance < 2*httpx.RequestTimeout {
		t.Errorf("the tolerance %v is under twice the request timeout %v", tolerance, httpx.RequestTimeout)
	}
}
