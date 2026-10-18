package sandbox

// sandbox_db_test.go -- the simulation over the REAL chain, runner, SUN pre-check, tap recorder and
// database, on a pool configured as the sandbox (TAPPA_ENV=sandbox: the virtual-plaque gate on).
// Every fixture is committed, as everywhere in this repository; a plaque row is never deleted
// (the plaque key rule). The holdings and their memberships are written and removed by the owner
// connection (tappa_app may not -- ADR 0027 §1). Requires DATABASE_URL and DATABASE_MIGRATE_URL.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/handler/api"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/test/fixtures"
)

const apiHost = "api.sandbox.test"

// callerNet is httptest's RemoteAddr network (192.0.2.1): a venue given this range matches the
// simulated tap's address.
const callerNet = "192.0.2.0/24"

type rig struct {
	t     *testing.T
	ctx   context.Context
	d     *db.DB
	owner *pgx.Conn
	keys  *apikey.Keys
	kek   []byte
	sim   *Simulation
	h     http.Handler
	// notices is the simulation's Noticer (K-19): every Notice it was given, in order.
	notices *noticeLog
}

func newRig(t *testing.T) *rig { return newRigOn(t, 0) }

// newRigOn is newRig over a pool of at most poolMax connections (0: pgxpool's default).
func newRigOn(t *testing.T, poolMax int) *rig {
	t.Helper()
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres; the owner opens K-22's gate)")
	}
	if poolMax > 0 {
		sep := "?"
		if strings.Contains(appDSN, "?") {
			sep = "&"
		}
		appDSN += fmt.Sprintf("%spool_max_conns=%d", sep, poolMax)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	kek, mac := random(t, 32), random(t, 32)
	cfg := &config.Config{
		Env: config.EnvSandbox, DatabaseURL: appDSN, BaseURL: "https://sandbox.test", TagKEK: kek,
		GPSRadiusMeters: 150, Debounce: 60 * time.Second, Freshness: 180 * time.Second, RetentionYears: 2,
	}
	d, err := db.New(ctx, cfg)
	if err != nil {
		t.Fatalf("db.New (sandbox): %v", err)
	}
	t.Cleanup(d.Close)
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	quiet := slog.New(slog.DiscardHandler)
	trail := must(audit.New(d))
	keys := must(apikey.NewKeys(apikey.SandboxPrefix, mac, d, trail))
	chain := must(apikey.NewAuthenticator(apikey.SandboxPrefix, mac, d, d))
	notices := &noticeLog{}
	sim, err := New(Config{App: cfg, Data: d, Work: notices, Log: quiet})
	if err != nil {
		t.Fatal(err)
	}
	surface, err := api.New(api.Config{
		Host: apiHost, Keys: chain, Writes: must(apikey.NewWrites(d)), Suspension: api.NoSuspensionColumn{}, Work: noWork{},
		Locations: must(tenant.NewVenues(d, trail, quiet)), Employees: must(tenant.NewStaff(d, trail, quiet)),
		Punches: must(tenant.NewPunchFeed(d)), Directory: must(tenant.NewEmployeeDirectory(d)),
		Extensions: []api.Extension{sim}, Log: quiet,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &rig{t: t, ctx: ctx, d: d, owner: owner, keys: keys, kek: kek, sim: sim, notices: notices,
		h: httpx.NewRouter(&config.Config{APIHost: apiHost}, nil, surface)}
}

type noWork struct{}

func (noWork) Notice(uuid.UUID) {}

// noticeLog records every Notice; at is called (when set) with each one as it is given, so a test
// can look at the database at that moment.
type noticeLog struct {
	mu      sync.Mutex
	tenants []uuid.UUID
	at      func(uuid.UUID)
}

func (n *noticeLog) Notice(t uuid.UUID) {
	n.mu.Lock()
	n.tenants = append(n.tenants, t)
	at := n.at
	n.mu.Unlock()
	if at != nil {
		at(t)
	}
}

// take returns the notices given since the last take and forgets them.
func (n *noticeLog) take() []uuid.UUID {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := n.tenants
	n.tenants = nil
	return out
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func random(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func (r *rig) exec(sql string, args ...any) {
	r.t.Helper()
	if _, err := r.owner.Exec(r.ctx, sql, args...); err != nil {
		r.t.Fatalf("%s: %v", sql, err)
	}
}

// business is a tenant with an active owner and a key; open says whether the sandbox operator
// opened its API (K-22), with code as its company code.
func (r *rig) business(open bool, code string) (tenantID uuid.UUID, key string) {
	r.t.Helper()
	tenantID, owner := uuid.New(), uuid.New()
	r.exec(`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'API-11 sandbox', $2, 'restaurant', 'multi')`,
		tenantID, "VAT-API11-"+tenantID.String())
	r.exec(`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status) VALUES ($1, $2, 'API-11 Owner', $3, $4, 'owner', 'active')`,
		owner, tenantID, "api11-"+owner.String()+"@m11.example", fixtures.UnusablePasswordHash)
	// The owner's panel issues a key only while the API is open; a closed business is one the
	// operator opened and closed again (the API-4 rig's way).
	r.exec(`UPDATE tenants SET api_code = $2, api_enabled_at = now() WHERE id = $1`, tenantID, code)
	c, err := r.keys.Create(r.ctx, apikey.CreateCommand{TenantID: tenantID, ActorID: owner, Name: "k-" + uuid.NewString()[:8]})
	if err != nil {
		r.t.Fatal(err)
	}
	if !open {
		r.exec(`UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, tenantID)
	}
	return tenantID, c.Key.RevealToTheOwnerOnce()
}

// employee commits an employee of tenantID under ref, never activated (a sandbox has no plaque to
// activate at).
func (r *rig) employee(tenantID uuid.UUID, ref, status string) uuid.UUID {
	r.t.Helper()
	id := uuid.New()
	r.exec(`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, $3, 'Sandbox Staff', $4, now())`,
		id, tenantID, ref, status)
	return id
}

// venue is PUT /v1/locations/{ref} with key -- the integrator's own way to open one.
func (r *rig) venue(key, ref string) uuid.UUID {
	r.t.Helper()
	w := r.do(http.MethodPut, "/v1/locations/"+ref, key, `{"name":"Sandbox venue","active":true}`)
	if w.Code != http.StatusCreated {
		r.t.Fatalf("PUT /v1/locations/%s: %d %s", ref, w.Code, w.Body.String())
	}
	var b struct{ ID string }
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		r.t.Fatal(err)
	}
	return uuid.MustParse(strings.TrimPrefix(b.ID, "loc_"))
}

// evidence gives a venue the caller's address range, so a simulated tap there matches by address.
func (r *rig) evidence(venue uuid.UUID) {
	r.t.Helper()
	r.exec(`UPDATE locations SET static_ips = $2::cidr[] WHERE id = $1`, venue, []string{callerNet})
}

func (r *rig) do(method, path, key, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://"+apiHost+path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	return w
}

func (r *rig) tap(key, employeeRef, locationRef, channel string, hdr ...string) *httptest.ResponseRecorder {
	return r.do(http.MethodPost, "/v1/sandbox/taps", key,
		fmt.Sprintf(`{"employeeRef":%q,"locationRef":%q,"channel":%q}`, employeeRef, locationRef, channel), hdr...)
}

func decodeTap(t *testing.T, w *httptest.ResponseRecorder) tapBody {
	t.Helper()
	if w.Code != http.StatusCreated {
		t.Fatalf("a simulated tap: %d %s", w.Code, w.Body.String())
	}
	var b tapBody
	dec := json.NewDecoder(bytes.NewReader(w.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		t.Fatalf("the answer %s: %v", w.Body.String(), err)
	}
	return b
}

func (r *rig) count(sql string, args ...any) int {
	r.t.Helper()
	var n int
	if err := r.owner.QueryRow(r.ctx, sql, args...).Scan(&n); err != nil {
		r.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// record is the transactions row of a simulated tap, read by the owner.
type record struct {
	tenant, employee         uuid.UUID
	site                     *uuid.UUID
	verdict, channel, tagUID string
	sunValid                 *bool
	ctr                      *int32
	siteTag                  *string
}

func (r *rig) record(punchID string) record {
	r.t.Helper()
	var rec record
	var tag *string
	err := r.owner.QueryRow(r.ctx, `SELECT tenant_id, employee_id, site_tenant_id, verdict, channel, tag_uid, site_tag_uid, sun_valid, ctr
	                                FROM transactions WHERE id = $1`, uuid.MustParse(strings.TrimPrefix(punchID, "pch_"))).
		Scan(&rec.tenant, &rec.employee, &rec.site, &rec.verdict, &rec.channel, &tag, &rec.siteTag, &rec.sunValid, &rec.ctr)
	if err != nil {
		r.t.Fatalf("the record of %s: %v", punchID, err)
	}
	if tag != nil {
		rec.tagUID = strings.TrimSpace(*tag)
	}
	return rec
}

// feedHas reports whether key's feed in scope carries punchID.
func (r *rig) feedHas(key, scope, punchID string) bool {
	r.t.Helper()
	w := r.do(http.MethodGet, "/v1/punches?scope="+scope, key, "")
	if w.Code != http.StatusOK {
		r.t.Fatalf("GET /v1/punches?scope=%s: %d %s", scope, w.Code, w.Body.String())
	}
	return strings.Contains(w.Body.String(), `"id":"`+punchID+`"`)
}

// TestSandbox_ATapRunsTheRealPathFromThePlaqueOn (ADR 0028 §11): the first simulated tap at an own
// venue provisions its virtual plaque -- virtual UID, the 44-byte KEK envelope, encoded, mounted,
// with the panel's three trail rows attributed to the key -- and records through the SUN pre-check
// and the atomic advance (sun_valid true, counter 0 -> 1). The venue the API opened has no evidence:
// flag. With the caller's address range: ok, a punch in both scopes of the feed. QR with the address:
// ok (§5's QR rule); the same person again within 60 s: ignored, recorded. A replay with the same
// Idempotency-Key answers the first body and records nothing more. A deactivated employee: reject.
//
// MUTANTS (measured): Record's CMACVerified set to false (sun_valid false, verdict reject -- red); the
// minted counter last_ctr instead of last_ctr+1 (the advance refuses a replay -- red); provisioning
// without AssignTagToLocation (404 no_plaque -- red); the replay re-running the work (a second
// transactions row -- red).
func TestSandbox_ATapRunsTheRealPathFromThePlaqueOn(t *testing.T) {
	r := newRig(t)
	kf, key := r.business(true, "KF")
	venue := r.venue(key, "KF-VLT")
	r.employee(kf, "E-1", "invited")
	r.employee(kf, "E-2", "active")
	r.employee(kf, "E-3", "active")
	r.employee(kf, "E-GONE", "deactivated")

	first := decodeTap(t, r.tap(key, "E-1", "KF-VLT", "nfc"))
	uid := VirtualUID(venue)
	if first.Verdict != "flag" || first.InFeed || first.Channel != "nfc" || first.TagID != "tag_"+uid ||
		first.Location.ID != "loc_"+venue.String() || first.Location.ExternalRef != "KF-VLT" || first.Employee.ExternalRef != "E-1" {
		t.Fatalf("the first tap at a venue with no evidence: %+v", first)
	}
	var (
		status   string
		lastCtr  int32
		keyBytes int
		encoded  bool
		wall     *uuid.UUID
	)
	if err := r.owner.QueryRow(r.ctx, `SELECT status, last_ctr, length(aes_key_ref), encoded_at IS NOT NULL, location_id FROM tags WHERE uid = $1 AND tenant_id = $2`,
		uid, kf).Scan(&status, &lastCtr, &keyBytes, &encoded, &wall); err != nil {
		t.Fatalf("the virtual plaque: %v", err)
	}
	if status != "active" || lastCtr != 1 || keyBytes != sun.WrappedKeyLen || !encoded || wall == nil || *wall != venue || !db.IsVirtualTagUID(uid) {
		t.Fatalf("the virtual plaque: status %s ctr %d key %d bytes encoded %v wall %v", status, lastCtr, keyBytes, encoded, wall)
	}
	for _, action := range []string{tenant.ActionPlaqueLoaded, tenant.ActionPlaqueEncoded, tenant.ActionPlaqueMounted} {
		if n := r.count(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2 AND action = $3 AND actor_id IS NOT NULL AND detail->>'via' = 'api'`,
			kf, uid, action); n != 1 {
			t.Errorf("%s rows for the plaque, attributed to the key: %d, want 1", action, n)
		}
	}
	rec := r.record(first.PunchID)
	if rec.sunValid == nil || !*rec.sunValid || rec.ctr == nil || *rec.ctr != 1 || rec.tagUID != uid || rec.channel != "nfc" {
		t.Fatalf("the record: %+v", rec)
	}

	r.evidence(venue)
	ok := decodeTap(t, r.tap(key, "E-2", "KF-VLT", "nfc"))
	if ok.Verdict != "ok" || !ok.InFeed || ok.Direction == nil || *ok.Direction != "in" || ok.Trust == nil || *ok.Trust != 70 {
		t.Fatalf("a tap from inside the venue's range: %+v", ok)
	}
	if !r.feedHas(key, "employer", ok.PunchID) || !r.feedHas(key, "location", ok.PunchID) || r.feedHas(key, "employer", first.PunchID) {
		t.Errorf("the feed: the ok tap in both scopes, the flagged one in neither")
	}

	qr := decodeTap(t, r.tap(key, "E-3", "KF-VLT", "qr"))
	if qr.Verdict != "ok" || qr.Channel != "qr" {
		t.Fatalf("a QR tap with the address: %+v", qr)
	}
	if again := decodeTap(t, r.tap(key, "E-3", "KF-VLT", "nfc")); again.Verdict != "ignored" {
		t.Fatalf("the same person within 60 s: %+v, want ignored (recorded)", again)
	}
	if gone := decodeTap(t, r.tap(key, "E-GONE", "KF-VLT", "nfc")); gone.Verdict != "reject" {
		t.Fatalf("a deactivated employee: %+v, want reject", gone)
	}

	before := r.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1`, kf)
	idemKey := "sim-" + uuid.NewString()
	w1 := r.tap(key, "E-1", "KF-VLT", "qr", "Idempotency-Key", idemKey)
	decodeTap(t, w1)
	w2 := r.tap(key, "E-1", "KF-VLT", "qr", "Idempotency-Key", idemKey)
	decodeTap(t, w2)
	if w1.Header().Get("Idempotent-Replayed") != "" || w2.Header().Get("Idempotent-Replayed") != "true" {
		t.Errorf("Idempotent-Replayed: first %q, retry %q", w1.Header().Get("Idempotent-Replayed"), w2.Header().Get("Idempotent-Replayed"))
	}
	if !bytes.Equal(w1.Body.Bytes(), w2.Body.Bytes()) {
		t.Errorf("the replay answered another body:\n%s\n%s", w1.Body.String(), w2.Body.String())
	}
	if n := r.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1`, kf); n != before+1 {
		t.Errorf("a tap and its replay wrote %d rows, want 1", n-before)
	}

	// A plaque reported lost is still the venue's plaque: the touch is recorded and §5 row 1 refuses
	// it (its key is never opened -- tapPath).
	r.employee(kf, "E-4", "active")
	r.exec(`UPDATE tags SET status = 'lost' WHERE uid = $1`, uid)
	if lost := decodeTap(t, r.tap(key, "E-4", "KF-VLT", "nfc")); lost.Verdict != "reject" || lost.Rule == nil || *lost.Rule != "sys:tag-not-active" {
		t.Fatalf("a tap on a lost plaque: %+v", lost)
	}
}

// holding puts the tenants into one new holding, joined ten minutes ago, and removes it at the end.
func (r *rig) holding(members ...uuid.UUID) {
	r.t.Helper()
	var h uuid.UUID
	if err := r.owner.QueryRow(r.ctx, `INSERT INTO holdings (name) VALUES ($1) RETURNING id`, "api11 "+uuid.NewString()[:8]).Scan(&h); err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() {
		bg := context.Background()
		if _, err := r.owner.Exec(bg, `DELETE FROM holding_members WHERE holding_id = $1`, h); err != nil {
			r.t.Errorf("cleanup memberships: %v", err)
		}
		if _, err := r.owner.Exec(bg, `DELETE FROM holdings WHERE id = $1`, h); err != nil {
			r.t.Errorf("cleanup holding: %v", err)
		}
	})
	for _, m := range members {
		r.exec(`INSERT INTO holding_members (holding_id, member_tenant_id, joined_at) VALUES ($1, $2, clock_timestamp() - interval '10 minutes')`, h, m)
	}
}

// TestSandbox_SimulatesAHoldingTap (ADR 0028 §11: "simülasyon holding dokunuşunu da üretir"; ADR 0027
// §5): KF and KM are one group. A KM employee's simulated tap at KF's venue, before KF has a plaque
// there, is 404 no_plaque and writes nothing into either business -- the sandbox never provisions
// into another business. Once KF's own simulated tap there provisioned it, KM's key taps it: the
// record is KM's, its site is KF (the KF plaque in the site columns, the KF counter advanced through
// holding_advance_tag_counter), the verdict is judged on KF's venue's evidence (ok), and the punch
// is in KM's employer scope and in KF's location scope -- and not in KF's employer scope. KM's answer
// carries no trust and no rule: they were computed from KF's venue's evidence (K-39).
//
// MUTANTS (measured): venueByRef without the peer branch (404 unknown_location -- red); provisioning
// at a peer's venue too (the first group tap is no longer the 404 -- red).
func TestSandbox_SimulatesAHoldingTap(t *testing.T) {
	r := newRig(t)
	kf, kfKey := r.business(true, "KF")
	km, kmKey := r.business(true, "KM")
	r.holding(kf, km)
	venue := r.venue(kfKey, "KF-VLT")
	r.evidence(venue)
	r.employee(kf, "KF-1", "active")
	r.employee(km, "KM-1", "active")

	before := r.count(`SELECT count(*) FROM tags WHERE tenant_id = $1`, kf)
	w := r.tap(kmKey, "KM-1", "KF-VLT", "nfc")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"reason":"no_plaque"`) {
		t.Fatalf("a group tap at a peer venue with no plaque: %d %s", w.Code, w.Body.String())
	}
	if n := r.count(`SELECT count(*) FROM tags WHERE tenant_id = $1`, kf); n != before {
		t.Fatalf("KM's key provisioned a plaque into KF (%d -> %d)", before, n)
	}
	if n := r.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1 OR site_tenant_id = $1`, km); n != 0 {
		t.Fatalf("the refused group tap wrote %d rows", n)
	}

	decodeTap(t, r.tap(kfKey, "KF-1", "KF-VLT", "nfc")) // KF's own tap provisions its plaque
	group := decodeTap(t, r.tap(kmKey, "KM-1", "KF-VLT", "nfc"))
	if group.Verdict != "ok" || !group.InFeed || group.Location.ID != "loc_"+venue.String() || group.TagID != "tag_"+VirtualUID(venue) ||
		group.Trust != nil || group.Rule != nil {
		t.Fatalf("the group tap: %+v", group)
	}
	rec := r.record(group.PunchID)
	if rec.tenant != km || rec.site == nil || *rec.site != kf || rec.siteTag == nil || strings.TrimSpace(*rec.siteTag) != VirtualUID(venue) {
		t.Fatalf("the group record: %+v", rec)
	}
	var ctr int32
	if err := r.owner.QueryRow(r.ctx, `SELECT last_ctr FROM tags WHERE uid = $1`, VirtualUID(venue)).Scan(&ctr); err != nil || ctr != 2 {
		t.Fatalf("KF's plaque counter after two taps: %d %v, want 2", ctr, err)
	}
	if !r.feedHas(kmKey, "employer", group.PunchID) || !r.feedHas(kfKey, "location", group.PunchID) || r.feedHas(kfKey, "employer", group.PunchID) {
		t.Errorf("the group punch: in KM's employer scope and KF's location scope only")
	}
}

// TestSandbox_K19NoticesTheBusinessesAfterTheCommit (K-19; API-11 low round): a recorded simulation
// tells the webhook worker which businesses have work -- an own tap the key's business, a group tap
// the key's business AND the venue's (the peer's notice goes from here alone: without it the peer's
// delivery waits for the peer's next event). THE CONTRACT IS "AFTER THE COMMIT", measured as: at the
// moment of each notice the record is already visible on another connection (the owner's) -- the
// worker the notice wakes reads committed rows, and a notice before the commit would find none. A
// refused request (404, rolled back) and an Idempotency-Key replay (nothing new) notify nobody.
//
// MUTANTS (measured): the key's business's Notice deleted (the own tap notifies nobody -- red); the
// venue's business's Notice deleted (the group tap notifies KM only -- red); both Notices moved into
// the runner's work, before the commit (the owner sees 0 rows at the notice -- red).
func TestSandbox_K19NoticesTheBusinessesAfterTheCommit(t *testing.T) {
	r := newRig(t)
	kf, kfKey := r.business(true, "KF")
	km, kmKey := r.business(true, "KM")
	r.holding(kf, km)
	venue := r.venue(kfKey, "KF-VLT")
	r.evidence(venue)
	r.employee(kf, "KF-1", "active")
	r.employee(kf, "KF-2", "active")
	r.employee(km, "KM-1", "active")
	r.notices.take()
	visible := map[uuid.UUID]int{}
	r.notices.at = func(tenant uuid.UUID) {
		visible[tenant] = r.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1 OR site_tenant_id = $1`, tenant)
	}
	same := func(got, want []uuid.UUID) bool { return fmt.Sprint(got) == fmt.Sprint(want) }

	decodeTap(t, r.tap(kfKey, "KF-1", "KF-VLT", "nfc"))
	if got := r.notices.take(); !same(got, []uuid.UUID{kf}) || visible[kf] != 1 {
		t.Fatalf("an own tap: notices %v (want [KF %s]), KF's committed rows at its notice %d (want 1)", got, kf, visible[kf])
	}

	decodeTap(t, r.tap(kmKey, "KM-1", "KF-VLT", "nfc"))
	if got := r.notices.take(); !same(got, []uuid.UUID{km, kf}) || visible[km] != 1 || visible[kf] != 2 {
		t.Fatalf("a group tap: notices %v (want [KM %s, KF %s]), committed rows at the notices KM %d (want 1), KF %d (want 2)",
			got, km, kf, visible[km], visible[kf])
	}

	if w := r.tap(kmKey, "KM-NOBODY", "KF-VLT", "nfc"); w.Code != http.StatusNotFound {
		t.Fatalf("an unknown employee: %d %s", w.Code, w.Body.String())
	}
	if got := r.notices.take(); len(got) != 0 {
		t.Fatalf("a refused request notified %v", got)
	}

	idem := "k19-" + uuid.NewString()
	decodeTap(t, r.tap(kfKey, "KF-2", "KF-VLT", "qr", "Idempotency-Key", idem))
	if got := r.notices.take(); !same(got, []uuid.UUID{kf}) {
		t.Fatalf("the first of a keyed pair: notices %v, want [KF]", got)
	}
	decodeTap(t, r.tap(kfKey, "KF-2", "KF-VLT", "qr", "Idempotency-Key", idem))
	if got := r.notices.take(); len(got) != 0 {
		t.Fatalf("a replay notified %v", got)
	}
}

// TestSandbox_KeyWithoutOperatorIs403 (ADR 0028 §11, K-22: "API, webhook ve holding yalnız sandbox
// operatörü açınca -- operatörsüz anahtar 403"): a live tt_test_ key of a business the sandbox
// operator has not opened is 403 api_disabled on the simulation, and nothing is provisioned or
// recorded. The operator opens it: the same request is 201 (control).
//
// MUTANT (measured): the simulation mounted on the public group instead of the authenticated one ->
// the closed business's request reaches the work -- red.
func TestSandbox_KeyWithoutOperatorIs403(t *testing.T) {
	r := newRig(t)
	closed, key := r.business(false, "CLOSED")
	r.employee(closed, "E-1", "active")
	var venue uuid.UUID
	if err := r.owner.QueryRow(r.ctx, `INSERT INTO locations (tenant_id, name, external_ref) VALUES ($1, 'Closed venue', 'V-1') RETURNING id`, closed).Scan(&venue); err != nil {
		t.Fatal(err)
	}
	w := r.tap(key, "E-1", "V-1", "nfc")
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"code":"api_disabled"`) {
		t.Fatalf("a key of a business the operator has not opened: %d %s", w.Code, w.Body.String())
	}
	if n := r.count(`SELECT count(*) FROM tags WHERE tenant_id = $1`, closed); n != 0 {
		t.Fatalf("a refused simulation provisioned %d plaques", n)
	}
	if n := r.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1`, closed); n != 0 {
		t.Fatalf("a refused simulation recorded %d taps", n)
	}
	r.exec(`UPDATE tenants SET api_enabled_at = now() WHERE id = $1`, closed)
	if b := decodeTap(t, r.tap(key, "E-1", "V-1", "nfc")); b.Verdict != "flag" {
		t.Fatalf("CONTROL: the opened business's tap: %+v", b)
	}
}

// TestSandbox_TheBodyAndTheNamesAreTheContracts: every refusal is the error contract's, and none
// writes anything.
func TestSandbox_TheBodyAndTheNamesAreTheContracts(t *testing.T) {
	r := newRig(t)
	kf, key := r.business(true, "KF")
	r.venue(key, "KF-VLT")
	r.employee(kf, "E-1", "active")
	for _, tc := range []struct {
		name, body, query string
		status            int
		want              string
	}{
		{"unknown field", `{"employeeRef":"E-1","locationRef":"KF-VLT","channel":"nfc","gps":1}`, "", 422, `"reason":"unknown_field"`},
		{"a field twice", `{"employeeRef":"E-1","employeeRef":"E-1","locationRef":"KF-VLT","channel":"nfc"}`, "", 422, `"reason":"duplicate_field"`},
		{"no channel", `{"employeeRef":"E-1","locationRef":"KF-VLT"}`, "", 422, `"field":"channel"`},
		{"a channel of another case", `{"employeeRef":"E-1","locationRef":"KF-VLT","channel":"NFC"}`, "", 422, `"field":"channel"`},
		{"manual is no channel here", `{"employeeRef":"E-1","locationRef":"KF-VLT","channel":"manual"}`, "", 422, `"field":"channel"`},
		{"a ref outside the pattern", `{"employeeRef":"E 1","locationRef":"KF-VLT","channel":"nfc"}`, "", 422, `"field":"employeeRef"`},
		{"a number for a ref", `{"employeeRef":1,"locationRef":"KF-VLT","channel":"nfc"}`, "", 422, `"reason":"type"`},
		{"not an object", `[]`, "", 400, `"reason":"malformed_json"`},
		{"unknown employee", `{"employeeRef":"NOBODY","locationRef":"KF-VLT","channel":"nfc"}`, "", 404, `"reason":"unknown_employee"`},
		{"unknown location", `{"employeeRef":"E-1","locationRef":"NOWHERE","channel":"nfc"}`, "", 404, `"reason":"unknown_location"`},
		{"a query string", `{"employeeRef":"E-1","locationRef":"KF-VLT","channel":"nfc"}`, "?dryRun=1", 400, `"code":"invalid_request"`},
	} {
		w := r.do(http.MethodPost, "/v1/sandbox/taps"+tc.query, key, tc.body)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), tc.want) || w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("%s: %d %s, want %d with %s", tc.name, w.Code, w.Body.String(), tc.status, tc.want)
		}
	}
	if n := r.count(`SELECT count(*) FROM transactions WHERE tenant_id = $1`, kf); n != 0 {
		t.Errorf("the refusals recorded %d taps", n)
	}
	if n := r.count(`SELECT count(*) FROM tags WHERE tenant_id = $1`, kf); n != 0 {
		t.Errorf("the refusals provisioned %d plaques", n)
	}
	// tt_live_ is another build's key: 401 at the shape check.
	if w := r.tap(apikey.LivePrefix+strings.Repeat("A", 43), "E-1", "KF-VLT", "nfc"); w.Code != http.StatusUnauthorized {
		t.Errorf("a tt_live_ key on the sandbox: %d, want 401", w.Code)
	}
	if w := r.do(http.MethodGet, "/v1/sandbox/openapi.json", "", ""); w.Code != http.StatusOK || !json.Valid(w.Body.Bytes()) {
		t.Errorf("the sandbox document without a key: %d", w.Code)
	}
	if w := r.do(http.MethodGet, "/v1/sandbox/openapi.json?x=1", "", ""); w.Code != http.StatusBadRequest {
		t.Errorf("the sandbox document with a query: %d, want 400", w.Code)
	}
}

// TestSandbox_AVirtualUIDHeldByAnotherBusinessIs404 (API-11 fix 1, the third eye's finding 3): the
// venue's virtual UID is already another business's plaque (the five-byte collision, or a UID taken
// first). The simulation at the venue is 404 no_plaque: its provisioning INSERT is refused by the
// primary key inside a savepoint (the runner's transaction goes on), the resolved row is held to
// the venue and is not its plaque, and nothing is written -- no plaque for the venue's business, no
// record in either business, the other business's plaque and counter untouched.
//
// The tenant half of that hold cannot be reached by a row the schema admits (tags_location_fk);
// TestPlaqueAt_HoldsTheResolvedRowToTheVenue pins it.
func TestSandbox_AVirtualUIDHeldByAnotherBusinessIs404(t *testing.T) {
	r := newRig(t)
	kf, key := r.business(true, "KF")
	other, _ := r.business(true, "OTHER")
	r.employee(kf, "E-1", "active")
	venue, theirs := uuid.New(), uuid.New()
	r.exec(`INSERT INTO locations (id, tenant_id, name, external_ref) VALUES ($1, $2, 'KF venue', 'KF-VLT')`, venue, kf)
	r.exec(`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'Their venue')`, theirs, other)
	uid := VirtualUID(venue)
	r.exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 7, 'active')`,
		uid, other, theirs, random(t, sun.WrappedKeyLen))

	w := r.tap(key, "E-1", "KF-VLT", "nfc")
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"reason":"no_plaque"`) {
		t.Fatalf("a simulation at a venue whose virtual UID another business holds: %d %s", w.Code, w.Body.String())
	}
	if n := r.count(`SELECT count(*) FROM tags WHERE tenant_id = $1`, kf); n != 0 {
		t.Errorf("the venue's business got %d plaques", n)
	}
	if n := r.count(`SELECT count(*) FROM transactions WHERE tenant_id = ANY($1) OR site_tenant_id = ANY($1)`, []uuid.UUID{kf, other}); n != 0 {
		t.Errorf("the refused simulation recorded %d taps", n)
	}
	var (
		owner uuid.UUID
		ctr   int32
	)
	if err := r.owner.QueryRow(r.ctx, `SELECT tenant_id, last_ctr FROM tags WHERE uid = $1`, uid).Scan(&owner, &ctr); err != nil || owner != other || ctr != 7 {
		t.Errorf("the other business's plaque: tenant %v counter %d (%v), want %v and 7", owner, ctr, err, other)
	}
	if n := r.count(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2`, kf, uid); n != 0 {
		t.Errorf("the refused provisioning left %d trail rows", n)
	}
}
