package egress_test

// webhook_worker_db_test.go -- M11 API-10's sender (internal/webhook/delivery) end to end, against REAL
// Postgres and through a REAL egress.Client: the in-process DNS server answers the endpoint names, a
// httptest TLS server on the loopback stands in for the owner's systems (the seam client allows that one
// address; the blocked list and the registry still refuse every other), and the deliveries are written
// by the SHIPPED punch-feed triggers (00049) from rows inserted as tappa_app in the employer's context --
// the shape the tap path writes. NOTHING LEAVES THE MACHINE: every DNS answer is a loopback, private or
// link-local address or NXDOMAIN; no public address is ever answered (the brief's rule -- a public
// answer once sent a TCP attempt to the internet).
//
// The tests live in this directory because the dial seams are closed to every other package
// (export_test.go). Fixtures are committed and never cleaned (audit_log and the feed are append-only;
// tags are never deleted) except the holdings, which the world removes at the end.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/review"
	"github.com/atknatk/tappa/internal/egress"
	"github.com/atknatk/tappa/internal/webhook"
	"github.com/atknatk/tappa/internal/webhook/delivery"
	"github.com/atknatk/tappa/internal/webhook/endpoints"
	"github.com/atknatk/tappa/test/fixtures"
)

// ---------------------------------------------------------------------------------------------------
// the receiver: one TLS server for every endpoint name (the Host header tells them apart)
// ---------------------------------------------------------------------------------------------------

type hit struct {
	host, path string
	body       []byte
	header     http.Header
}

type receiver struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits []hit
	// status answers a request (default 204); it may block. during runs while the request is in
	// flight, before the answer.
	status func(r *http.Request) int
	during func(r *http.Request)
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	rc := &receiver{}
	rc.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rc.mu.Lock()
		rc.hits = append(rc.hits, hit{host: r.Host, path: r.URL.Path, body: b, header: r.Header.Clone()})
		status, during := rc.status, rc.during
		rc.mu.Unlock()
		if during != nil {
			during(r)
		}
		code := http.StatusNoContent
		if status != nil {
			code = status(r)
		}
		w.WriteHeader(code)
	}))
	rc.srv.Config.ErrorLog = log.New(io.Discard, "", 0)
	rc.srv.StartTLS()
	t.Cleanup(rc.srv.Close)
	return rc
}

func (rc *receiver) set(status func(*http.Request) int, during func(*http.Request)) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.status, rc.during = status, during
}

func (rc *receiver) all() []hit {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return slices.Clone(rc.hits)
}

func (rc *receiver) to(host string) []hit {
	var out []hit
	for _, h := range rc.all() {
		if h.host == host {
			out = append(out, h)
		}
	}
	return out
}

func (rc *receiver) port(t *testing.T) uint16 {
	t.Helper()
	_, p, err := net.SplitHostPort(rc.srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return uint16(n)
}

func (rc *receiver) roots() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(rc.srv.Certificate())
	return p
}

// ---------------------------------------------------------------------------------------------------
// the world
// ---------------------------------------------------------------------------------------------------

var loopback = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}

type wbiz struct {
	id, owner, manager, venue, emp uuid.UUID
	code, tag, email               string
	host, path                     string
	endpoint                       uuid.UUID
	key                            interface{ Sign([]byte) string }
}

type world struct {
	t      *testing.T
	ctx    context.Context
	app    *db.DB
	kek    []byte
	svc    *endpoints.Service
	rcv    *receiver
	dns    *egress.FakeDNS
	canary string

	mu    sync.Mutex
	names map[string]func(n int) []netip.Addr

	// own is the world's one owner pool, opened on first use and closed with the test. A connection
	// per owner() call, closed only at the test's end, held one server slot per call: T130's
	// full-batch test ages 101 rows and reached max_connections (53300 too many clients), failing the
	// packages that ran beside it.
	ownOnce sync.Once
	own     *pgxpool.Pool
}

func randHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

func appDB(t *testing.T, appName string) *db.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" || os.Getenv("DATABASE_MIGRATE_URL") == "" {
		t.Skip("DATABASE_URL / DATABASE_MIGRATE_URL not set; skipping (real Postgres required)")
	}
	if appName != "" {
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		dsn += sep + "application_name=" + appName
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// ownerConns bounds the world's owner pool: no helper holds more than one owner connection at a time,
// and a test's goroutines that reach the owner are few -- a wait here is a test's, never a server slot.
const ownerConns = 4

func (w *world) owner() *pgxpool.Pool {
	w.t.Helper()
	var err error
	w.ownOnce.Do(func() {
		var cfg *pgxpool.Config
		if cfg, err = pgxpool.ParseConfig(os.Getenv("DATABASE_MIGRATE_URL")); err != nil {
			return
		}
		cfg.MaxConns, cfg.MinConns = ownerConns, 0
		if w.own, err = pgxpool.NewWithConfig(w.ctx, cfg); err != nil {
			return
		}
		w.t.Cleanup(w.own.Close)
	})
	if err != nil {
		w.t.Fatalf("owner pool: %v", err)
	}
	if w.own == nil {
		w.t.Fatal("owner pool: not opened")
	}
	return w.own
}

func (w *world) exec(sql string, args ...any) {
	w.t.Helper()
	c := w.owner()
	if _, err := c.Exec(w.ctx, sql, args...); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
}

type nopSender struct{}

func (nopSender) Post(context.Context, egress.Endpoint, []byte, http.Header) egress.Result {
	return egress.Result{Status: 204}
}

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{t: t, ctx: context.Background(), app: appDB(t, ""), rcv: newReceiver(t), names: map[string]func(int) []netip.Addr{},
		canary: "CANARY" + strings.ToUpper(randHex(t, 6))}
	w.kek = []byte(randHex(t, 16))
	trail, err := audit.New(w.app)
	if err != nil {
		t.Fatal(err)
	}
	// The endpoint service creates, switches and resends; its own sender (the ping's) is not used here.
	if w.svc, err = endpoints.New(w.kek, w.app, trail, nopSender{}); err != nil {
		t.Fatal(err)
	}
	w.dns = egress.NewFakeDNS(func(name string, _ uint16, n int) ([]netip.Addr, bool) {
		w.mu.Lock()
		f, ok := w.names[name]
		w.mu.Unlock()
		if !ok {
			return nil, true
		}
		return f(n), false
	})
	return w
}

// answer makes name resolve to addrs (every time).
func (w *world) answer(name string, addrs ...string) {
	var as []netip.Addr
	for _, a := range addrs {
		as = append(as, netip.MustParseAddr(a))
	}
	w.answerFunc(name, func(int) []netip.Addr { return as })
}

func (w *world) answerFunc(name string, f func(n int) []netip.Addr) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.names[name] = f
}

// client is a real egress.Client against the receiver.
func (w *world) client(timeout time.Duration, observe func(netip.AddrPort, bool)) *egress.Client {
	return egress.SeamClient(loopback, w.rcv.port(w.t), w.dns, w.rcv.roots(), timeout, observe)
}

func (w *world) job(c delivery.Sender, d *db.DB, log *slog.Logger) *delivery.Job {
	w.t.Helper()
	if d == nil {
		d = w.app
	}
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	j, err := delivery.New(w.kek, d, c, log)
	if err != nil {
		w.t.Fatal(err)
	}
	return j
}

// biz is a business with an open API under code, a venue, a plaque, an employee, an owner and a
// manager; with an endpoint (resolving to the receiver) unless noEndpoint.
func (w *world) biz(code string, noEndpoint bool) wbiz {
	w.t.Helper()
	b := wbiz{id: uuid.New(), owner: uuid.New(), manager: uuid.New(), code: code}
	err := w.app.WithTenant(w.ctx, b.id, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
		                          VALUES ($1, 'API-10 fixture', $2, 'restaurant', 'multi')`, b.id, "VAT-API10-"+b.id.String()); e != nil {
			return e
		}
		for _, a := range []struct {
			id   uuid.UUID
			role string
		}{{b.owner, "owner"}, {b.manager, "manager"}} {
			if _, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			                          VALUES ($1, $2, 'API-10 Admin', $3, $4, $5, 'active')`,
				a.id, b.id, "api10-"+a.id.String()+"@m11.example", fixtures.UnusablePasswordHash, a.role); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		w.t.Fatalf("new business: %v", err)
	}
	w.exec(`UPDATE tenants SET api_code = $2, api_enabled_at = now() WHERE id = $1`, b.id, code)
	c := w.owner()
	if err := c.QueryRow(w.ctx, `INSERT INTO locations (tenant_id, name, external_ref) VALUES ($1, $2, $3) RETURNING id`,
		b.id, code+" Venue", code+"-LOC").Scan(&b.venue); err != nil {
		w.t.Fatal(err)
	}
	b.tag = strings.ToUpper(randHex(w.t, 7))
	w.exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 1000000, 'active')`,
		b.tag, b.id, b.venue, []byte(randHex(w.t, 22)))
	b.emp = uuid.New()
	b.email = strings.ToLower(w.canary) + "." + strings.ToLower(code) + "@api10.example.test"
	w.exec(`INSERT INTO employees (id, tenant_id, external_ref, employee_code, full_name, email, status, invited_at)
	        VALUES ($1, $2, $3, $4, 'Hook Staff', $5, 'active', now())`, b.emp, b.id, code+"-EMP", code+"-C1", b.email)
	if noEndpoint {
		return b
	}
	b.host = strings.ToLower(code) + randHex(w.t, 4) + ".example.com"
	b.path = "/hooks/" + strings.ToLower(code) + "-" + randHex(w.t, 3)
	w.answer(b.host, "127.0.0.1")
	w.endpoint(&b, "https://"+b.host+b.path)
	return b
}

// endpoint creates b's endpoint at address (the owner's act) and keeps its id and key.
func (w *world) endpoint(b *wbiz, address string) {
	w.t.Helper()
	created, err := w.svc.Create(w.ctx, endpoints.CreateCommand{TenantID: b.id, ActorID: b.owner, Address: address})
	if err != nil {
		w.t.Fatalf("create the endpoint: %v", err)
	}
	b.endpoint, b.key = created.ID, created.Key
}

func (w *world) holding(members ...wbiz) {
	w.t.Helper()
	var h uuid.UUID
	c := w.owner()
	if err := c.QueryRow(w.ctx, `INSERT INTO holdings (name) VALUES ($1) RETURNING id`, "api10 "+randHex(w.t, 4)).Scan(&h); err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() {
		bg := context.Background()
		c, err := pgx.Connect(bg, os.Getenv("DATABASE_MIGRATE_URL"))
		if err != nil {
			w.t.Errorf("cleanup connect: %v", err)
			return
		}
		defer c.Close(bg)
		if _, err := c.Exec(bg, `DELETE FROM holding_members WHERE holding_id = $1`, h); err != nil {
			w.t.Errorf("cleanup memberships: %v", err)
		}
		if _, err := c.Exec(bg, `DELETE FROM holdings WHERE id = $1`, h); err != nil {
			w.t.Errorf("cleanup holding: %v", err)
		}
	})
	for _, m := range members {
		w.exec(`INSERT INTO holding_members (holding_id, member_tenant_id, joined_at) VALUES ($1, $2, clock_timestamp() - interval '10 minutes')`, h, m.id)
	}
}

// tap commits one record of employer's employee -- at site's plaque when site is set (a holding
// punch), at the employer's own otherwise -- as tappa_app in the employer's context. The evidence
// columns carry the world's canary.
func (w *world) tap(employer wbiz, site *wbiz, verdict string) uuid.UUID {
	w.t.Helper()
	return w.tapChosen(employer, site, verdict, nil)
}

// tapChosen is tap with transactions.direction_source set (M15-09): nil is NULL, every row before
// M15-11.
func (w *world) tapChosen(employer wbiz, site *wbiz, verdict string, source *string) uuid.UUID {
	w.t.Helper()
	var id uuid.UUID
	err := w.app.WithTenant(w.ctx, employer.id, func(ctx context.Context, tx pgx.Tx) error {
		var location, siteTenant, siteLocation *uuid.UUID
		var tag, siteTag *string
		var ctr *int32
		var sun *bool
		if site != nil {
			siteTenant, siteLocation, siteTag = &site.id, &site.venue, &site.tag
			c, s := int32(100), true
			ctr, sun = &c, &s
		} else {
			location, tag = &employer.venue, &employer.tag
		}
		return tx.QueryRow(ctx, `
			INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type, practice,
			                          site_tenant_id, site_location_id, site_tag_uid, ctr, sun_valid,
			                          note, source_ip, gps_lat, gps_lng, trust, policy_layer, matched_sid, policy_context, direction_source)
			VALUES ($1, $2, $3, $4, date_trunc('milliseconds', clock_timestamp()) - interval '1 minute', $5, 'nfc', 'in', false,
			        $6, $7, $8, $9, $10, $11, '198.51.100.213'::inet, 35.987654, 14.123456, 77, 'guardrail', $12, $13::jsonb, $14)
			RETURNING id`,
			employer.id, employer.emp, location, tag, verdict, siteTenant, siteLocation, siteTag, ctr, sun,
			w.canary+"-note", "sys:"+strings.ToLower(w.canary), `{"canary":"`+w.canary+`-context"}`, source).Scan(&id)
	})
	if err != nil {
		w.t.Fatalf("tap: %v", err)
	}
	return id
}

type drow struct {
	id, endpoint uuid.UUID
	seq          int64
	state        string
	attempt      int
	status       *int32
	class        *string
	leased       bool
	dueIn        float64 // seconds from the database's now to next_attempt_at
}

func (w *world) deliveries(b wbiz) []drow {
	w.t.Helper()
	var out []drow
	err := w.app.WithTenant(w.ctx, b.id, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, endpoint_id, punch_event_seq, state, attempt, last_status, last_error_class,
		                                   lease_until IS NOT NULL AND lease_until > clock_timestamp(),
		                                   extract(epoch FROM next_attempt_at - clock_timestamp())::float8
		                            FROM webhook_deliveries WHERE tenant_id = $1 ORDER BY punch_event_seq`, b.id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var d drow
			if err := rows.Scan(&d.id, &d.endpoint, &d.seq, &d.state, &d.attempt, &d.status, &d.class, &d.leased, &d.dueIn); err != nil {
				return err
			}
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return out
}

// makeDue moves a delivery's next try to just past (the database's clock is the job's only clock).
func (w *world) makeDue(b wbiz, id uuid.UUID) {
	w.exec(`UPDATE webhook_deliveries SET next_attempt_at = clock_timestamp() - interval '1 second' WHERE tenant_id = $1 AND id = $2`, b.id, id)
}

// eventsOf is the business's feed rows as (seq -> punch id, roles).
func (w *world) eventsOf(b wbiz) map[int64][2]string {
	w.t.Helper()
	out := map[int64][2]string{}
	err := w.app.WithTenant(w.ctx, b.id, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT seq, punch_id::text, array_to_string(roles, ',') FROM punch_events WHERE tenant_id = $1`, b.id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var seq int64
			var id, roles string
			if err := rows.Scan(&seq, &id, &roles); err != nil {
				return err
			}
			out[seq] = [2]string{id, roles}
		}
		return rows.Err()
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return out
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("a body that is not JSON: %v", err)
	}
	return m
}

func field(m map[string]any, path ...string) any {
	var cur any = m
	for _, p := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = mm[p]
	}
	return cur
}

// ---------------------------------------------------------------------------------------------------
// the tests
// ---------------------------------------------------------------------------------------------------

// TestWebhookRouting_FourShapes is ADR 0028 §6's routing end to end: KF and KM (holding peers, each with
// an endpoint), X (a peer WITHOUT an endpoint) and C (an outsider with an endpoint). Five recorded taps
// -- KF@KF, KM@KM, KM@KF, KF@KM, X@KF -- write, through the shipped triggers, 1 · 1 · 2 · 2 · 1
// deliveries (X's own share: none -- no endpoint, no row); the real worker (K-19's set and loop, the
// delivery job, a real egress client) is handed the three businesses and sends each to its OWN
// endpoint: KF's host receives KF@KF, KM@KF (location -- the employee fields name KM's employee) and
// KF@KM (employer -- the location fields name KM's venue) and X@KF (location); KM's host the mirror;
// C's host nothing. Every body is signed with its business's key and names its delivery and punch.
func TestWebhookRouting_FourShapes(t *testing.T) {
	w := newWorld(t)
	kf, km, x, c := w.biz("KF", false), w.biz("KM", false), w.biz("XN", true), w.biz("CC", false)
	w.holding(kf, km, x)

	kfkf := w.tap(kf, nil, "ok")
	kmkm := w.tap(km, nil, "ok")
	kmkf := w.tap(km, &kf, "ok")
	kfkm := w.tap(kf, &km, "ok")
	xkf := w.tap(x, &kf, "ok")

	if n := len(w.deliveries(kf)); n != 4 {
		t.Fatalf("KF holds %d deliveries, want 4 (KF@KF, KM@KF, KF@KM, X@KF)", n)
	}
	if n := len(w.deliveries(km)); n != 3 {
		t.Fatalf("KM holds %d deliveries, want 3 (KM@KM, KM@KF, KF@KM)", n)
	}
	if n := len(w.deliveries(x)); n != 0 {
		t.Fatalf("X (no endpoint) holds %d deliveries, want 0", n)
	}
	if n := len(w.deliveries(c)); n != 0 {
		t.Fatalf("C holds %d deliveries, want 0", n)
	}

	job := w.job(w.client(5*time.Second, nil), nil, nil)
	worker, err := webhook.NewWorker(slog.New(slog.NewTextHandler(io.Discard, nil)), job)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worker.Drain(context.Background()) }()
	for _, b := range []wbiz{kf, km, x, c} {
		worker.Notice(b.id)
	}
	deadline := time.Now().Add(15 * time.Second)
	for len(w.rcv.all()) < 7 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(200 * time.Millisecond) // nothing more may arrive

	type want struct {
		punch    uuid.UUID
		roles    string
		empCo    string
		locCo    string
		locVenue uuid.UUID
	}
	check := func(b wbiz, wants []want) {
		t.Helper()
		got := w.rcv.to(b.host)
		if len(got) != len(wants) {
			t.Fatalf("%s's endpoint received %d deliveries, want %d", b.code, len(got), len(wants))
		}
		rows := map[string]drow{}
		for _, d := range w.deliveries(b) {
			if d.state != delivery.StateDelivered || d.attempt != 1 || d.endpoint != b.endpoint {
				t.Errorf("%s's delivery %s: state %s, attempt %d, endpoint ours %v", b.code, d.id, d.state, d.attempt, d.endpoint == b.endpoint)
			}
			rows["dlv_"+d.id.String()] = d
		}
		events := w.eventsOf(b)
		for _, h := range got {
			if h.path != b.path || h.header.Get(webhook.SignatureHeader) != b.key.Sign(h.body) ||
				h.header.Get("Content-Type") != "application/json" || h.header.Get("User-Agent") != "Taptime-Webhooks/1" {
				t.Errorf("%s: a delivery at %s with a wrong path, signature or header", b.code, h.path)
			}
			m := decode(t, h.body)
			d, ok := rows[fmt.Sprint(m["deliveryId"])]
			if m["event"] != delivery.EventPunch || !ok {
				t.Errorf("%s: event %v, delivery %v not one of the business's", b.code, m["event"], m["deliveryId"])
				continue
			}
			ev := events[d.seq]
			found := false
			for _, wn := range wants {
				if m["id"] != "pch_"+wn.punch.String() || ev[0] != wn.punch.String() {
					continue
				}
				found = true
				if ev[1] != wn.roles || field(m, "employee", "company") != wn.empCo || field(m, "location", "company") != wn.locCo ||
					field(m, "location", "id") != "loc_"+wn.locVenue.String() {
					t.Errorf("%s: punch %s roles %s, employee company %v, location %v/%v", b.code, wn.punch, ev[1],
						field(m, "employee", "company"), field(m, "location", "company"), field(m, "location", "id"))
				}
			}
			if !found {
				t.Errorf("%s received punch %v, which is not one of its own", b.code, m["id"])
			}
		}
	}
	check(kf, []want{
		{kfkf, "employer,location", "KF", "KF", kf.venue},
		{kmkf, "location", "KM", "KF", kf.venue},
		{kfkm, "employer", "KF", "KM", km.venue},
		{xkf, "location", "XN", "KF", kf.venue},
	})
	check(km, []want{
		{kmkm, "employer,location", "KM", "KM", km.venue},
		{kmkf, "employer", "KM", "KF", kf.venue},
		{kfkm, "location", "KF", "KM", km.venue},
	})
	if n := len(w.rcv.to(c.host)); n != 0 {
		t.Fatalf("C's endpoint received %d deliveries", n)
	}
	if total := len(w.rcv.all()); total != 7 {
		t.Fatalf("%d requests in all, want 7", total)
	}
}

// TestWebhookWorker_DisabledEndpointGetsNothing is K-26 at send time (ADR 0028 §6): a delivery queued
// while the endpoint was on is NOT sent once the owner switches it off -- the run sends nothing, the
// row stays pending with its try unspent and no lease, and the run says "come back later"; the same for
// the operator's closed gate (K-22) and for a stored address the URL rule refuses. Switched on (and
// reopened) again, the waiting delivery goes, once.
func TestWebhookWorker_DisabledEndpointGetsNothing(t *testing.T) {
	w := newWorld(t)
	b := w.biz("OF", false)
	w.tap(b, nil, "ok")
	job := w.job(w.client(5*time.Second, nil), nil, nil)

	held := func(why string) {
		t.Helper()
		err := job.RunFor(context.Background(), b.id)
		var later *delivery.Later
		if !errors.As(err, &later) || later.DueIn() < time.Minute {
			t.Fatalf("%s: RunFor answered %v, want a later run", why, err)
		}
		d := w.deliveries(b)
		if len(w.rcv.to(b.host)) != 0 || len(d) != 1 || d[0].state != delivery.StatePending || d[0].attempt != 0 || d[0].leased {
			t.Fatalf("%s: %d sent, row %+v", why, len(w.rcv.to(b.host)), d)
		}
	}

	if _, err := w.svc.Disable(w.ctx, b.id, b.owner); err != nil {
		t.Fatal(err)
	}
	held("switched off")
	if _, err := w.svc.Enable(w.ctx, b.id, b.owner); err != nil {
		t.Fatal(err)
	}
	w.exec(`UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, b.id)
	held("the API closed (K-22)")
	w.exec(`UPDATE tenants SET api_enabled_at = now() WHERE id = $1`, b.id)
	w.exec(`UPDATE webhook_endpoints SET url = 'https://10.0.0.1/hook' WHERE tenant_id = $1`, b.id)
	held("a stored address the rule refuses")
	w.exec(`UPDATE webhook_endpoints SET url = $2 WHERE tenant_id = $1`, b.id, "https://"+b.host+b.path)

	if err := job.RunFor(context.Background(), b.id); err != nil {
		t.Fatalf("reopened: %v", err)
	}
	if d := w.deliveries(b); len(w.rcv.to(b.host)) != 1 || d[0].state != delivery.StateDelivered || d[0].attempt != 1 {
		t.Fatalf("reopened: %d sent, row %+v", len(w.rcv.to(b.host)), d)
	}
}

// TestWebhookWorker_TwoPodsSendOnce: two jobs on two pools (two pods) run the SAME business at the same
// time over 30 queued deliveries, the receiver answering slowly so their claims interleave; the
// receiver counts every delivery id exactly once (the lease -- ADR 0028 §6).
func TestWebhookWorker_TwoPodsSendOnce(t *testing.T) {
	w := newWorld(t)
	b := w.biz("TP", false)
	const n = 30
	for range n {
		w.tap(b, nil, "ok")
	}
	w.rcv.set(func(*http.Request) int { time.Sleep(15 * time.Millisecond); return 204 }, nil)
	senders := []*countingSender{{inner: w.client(5*time.Second, nil)}, {inner: w.client(5*time.Second, nil)}}
	pods := []*delivery.Job{
		w.job(senders[0], appDB(t, ""), nil),
		w.job(senders[1], appDB(t, ""), nil),
	}
	settled := func() bool {
		for _, d := range w.deliveries(b) {
			if d.state != delivery.StateDelivered {
				return false
			}
		}
		return true
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, j := range pods {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			deadline := time.Now().Add(20 * time.Second)
			for !settled() && time.Now().Before(deadline) {
				_ = j.RunFor(context.Background(), b.id)
			}
		}()
	}
	close(start)
	wg.Wait()
	if senders[0].n.Load() == 0 || senders[1].n.Load() == 0 {
		t.Fatalf("CONTROL: the pods sent %d and %d -- they did not both work", senders[0].n.Load(), senders[1].n.Load())
	}
	seen := map[string]int{}
	for _, h := range w.rcv.to(b.host) {
		seen[fmt.Sprint(decode(t, h.body)["deliveryId"])]++
	}
	if len(seen) != n {
		t.Fatalf("%d distinct deliveries arrived, want %d", len(seen), n)
	}
	for id, k := range seen {
		if k != 1 {
			t.Errorf("delivery %s arrived %d times", id, k)
		}
	}
	for _, d := range w.deliveries(b) {
		if d.state != delivery.StateDelivered || d.attempt != 1 {
			t.Errorf("delivery %s: %s after %d tries", d.id, d.state, d.attempt)
		}
	}
}

// countingSender counts what one pod sent.
type countingSender struct {
	inner delivery.Sender
	n     atomic.Int32
}

func (c *countingSender) Post(ctx context.Context, to egress.Endpoint, body []byte, h http.Header) egress.Result {
	c.n.Add(1)
	return c.inner.Post(ctx, to, body, h)
}

// TestWebhookWorker_NoOpenTransactionDuringHTTP: while the request is in flight, (a) the job's own pool
// (its application_name) has NO session inside a transaction -- pg_stat_activity, read by the same
// role, which sees its own sessions' xact_start; CONTROL: the pool's sessions are visible -- and (b)
// the delivery's row can be locked NOWAIT from another connection (the claim committed).
func TestWebhookWorker_NoOpenTransactionDuringHTTP(t *testing.T) {
	w := newWorld(t)
	b := w.biz("NT", false)
	w.tap(b, nil, "ok")
	app := "api10-notx-" + randHex(t, 4)
	pool := appDB(t, app)
	var (
		inTx, visible int
		lockErr       error
		probed        atomic.Bool
	)
	w.rcv.set(nil, func(*http.Request) {
		probed.Store(true)
		_ = w.app.WithTenant(context.Background(), b.id, func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE xact_start IS NOT NULL), count(*) FROM pg_stat_activity WHERE application_name = $1`, app).
				Scan(&inTx, &visible); err != nil {
				return err
			}
			_, lockErr = tx.Exec(ctx, `SELECT 1 FROM webhook_deliveries WHERE tenant_id = $1 FOR UPDATE NOWAIT`, b.id)
			return lockErr
		})
	})
	if err := w.job(w.client(5*time.Second, nil), pool, nil).RunFor(context.Background(), b.id); err != nil {
		t.Fatal(err)
	}
	if !probed.Load() || visible == 0 {
		t.Fatalf("CONTROL: probed %v, %d of the job's sessions visible", probed.Load(), visible)
	}
	if inTx != 0 || lockErr != nil {
		t.Fatalf("during the request: %d of the job's sessions inside a transaction, row lock %v", inTx, lockErr)
	}
}

// TestWebhookWorker_DrainsWithinShutdownBudget: the receiver holds a delivery's request open; the
// worker's Drain (cmd/tappa runs it beside the HTTP server's Shutdown, ADR 0028 §6) returns within
// 2 s -- the request is cancelled -- and NOTHING is recorded: the row is pending, its try unspent, its
// lease kept. When the lease has run out (moved to the past -- the database's clock is the job's) the
// next run sends it again: the receiver sees the same delivery id twice (it deduplicates -- plan §2
// item 16).
func TestWebhookWorker_DrainsWithinShutdownBudget(t *testing.T) {
	w := newWorld(t)
	b := w.biz("SD", false)
	w.tap(b, nil, "ok")
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	inFlight := make(chan struct{}, 1)
	w.rcv.set(func(*http.Request) int {
		select {
		case inFlight <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-time.After(30 * time.Second):
		}
		return 204
	}, nil)
	worker, err := webhook.NewWorker(slog.New(slog.NewTextHandler(io.Discard, nil)), w.job(w.client(30*time.Second, nil), nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	worker.Notice(b.id)
	select {
	case <-inFlight:
	case <-time.After(10 * time.Second):
		t.Fatal("the delivery never reached the receiver")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	began := time.Now()
	if err := worker.Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Fatalf("the drain took %v with a request in flight", took)
	}
	d := w.deliveries(b)
	if len(d) != 1 || d[0].state != delivery.StatePending || d[0].attempt != 0 || !d[0].leased {
		t.Fatalf("after the drain: %+v (want pending, no try spent, lease kept)", d)
	}
	close(release)
	w.rcv.set(nil, nil)
	w.exec(`UPDATE webhook_deliveries SET lease_until = clock_timestamp() - interval '1 second' WHERE tenant_id = $1`, b.id)
	if err := w.job(w.client(5*time.Second, nil), nil, nil).RunFor(context.Background(), b.id); err != nil {
		t.Fatal(err)
	}
	hits := w.rcv.to(b.host)
	if d := w.deliveries(b); len(hits) != 2 || d[0].state != delivery.StateDelivered ||
		fmt.Sprint(decode(t, hits[0].body)["deliveryId"]) != fmt.Sprint(decode(t, hits[1].body)["deliveryId"]) {
		t.Fatalf("after the lease: %d requests, row %+v", len(hits), d)
	}
}

// TestWebhookWorker_TheScheduleIsTheADRsAndResendRevivesTheDead: a receiver answering 503. Each run
// spends one try and sets the next 1 min, 5 min, 30 min, 2 h, 6 h after it (the database's clock; the
// test moves the due time to the past between runs); the sixth failure is dead. The run's answer is a
// later run as long as the delivery waits. A resend (the owner's, one bucket slot, a trail row) makes the
// dead delivery due: ONE try, still 503 -> dead again (seven tries); a second resend with the receiver
// back -> delivered.
func TestWebhookWorker_TheScheduleIsTheADRsAndResendRevivesTheDead(t *testing.T) {
	w := newWorld(t)
	b := w.biz("BO", false)
	w.tap(b, nil, "ok")
	w.rcv.set(func(*http.Request) int { return 503 }, nil)
	job := w.job(w.client(5*time.Second, nil), nil, nil)
	waits := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}
	for try := 1; try <= 6; try++ {
		err := job.RunFor(context.Background(), b.id)
		d := w.deliveries(b)[0]
		if d.attempt != try || d.class == nil || *d.class != "http_5xx" || d.status == nil || *d.status != 503 || d.leased {
			t.Fatalf("try %d: %+v", try, d)
		}
		if try == 6 {
			if d.state != delivery.StateDead || err != nil {
				t.Fatalf("the sixth failure: %s, run %v", d.state, err)
			}
			break
		}
		wait := waits[try-1]
		var later *delivery.Later
		if d.state != delivery.StatePending || d.dueIn > wait.Seconds() || d.dueIn < wait.Seconds()-10 ||
			!errors.As(err, &later) || later.DueIn() > wait || later.DueIn() < wait-10*time.Second {
			t.Fatalf("try %d: %s, next in %.0f s (want %v), run %v", try, d.state, d.dueIn, wait, err)
		}
		w.makeDue(b, d.id)
	}
	if len(w.rcv.to(b.host)) != 6 {
		t.Fatalf("%d requests for six tries", len(w.rcv.to(b.host)))
	}
	id := w.deliveries(b)[0].id
	if err := w.svc.Resend(w.ctx, b.id, b.owner, id); err != nil {
		t.Fatal(err)
	}
	if err := job.RunFor(context.Background(), b.id); err != nil {
		t.Fatal(err)
	}
	if d := w.deliveries(b)[0]; d.state != delivery.StateDead || d.attempt != 7 {
		t.Fatalf("a revived delivery that failed again: %+v", d)
	}
	w.rcv.set(nil, nil)
	if err := w.svc.Resend(w.ctx, b.id, b.owner, id); err != nil {
		t.Fatal(err)
	}
	if err := job.RunFor(context.Background(), b.id); err != nil {
		t.Fatal(err)
	}
	if d := w.deliveries(b)[0]; d.state != delivery.StateDelivered || d.attempt != 8 || d.class != nil {
		t.Fatalf("a revived delivery that arrived: %+v", d)
	}
	if n := countTrail(t, w, b, endpoints.ActionResend); n != 2 {
		t.Fatalf("%d resend slots, want 2", n)
	}
}

func countTrail(t *testing.T, w *world, b wbiz, action string) int {
	t.Helper()
	var n int
	if err := w.owner().QueryRow(w.ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2`, b.id, action).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestWebhookWorker_SSRFRefusedThroughTheWorker is ADR 0028 §7's gate on the worker's path: an
// endpoint whose name resolves to the cloud metadata address, and one to a private address, are
// blocked_address with NO request at the receiver and every dial refused at the gate; a name that
// answers the (allowed) loopback first and a private address the next time (DNS rebinding) delivers
// the first and is refused the second; with HTTPS_PROXY pointing at a recording proxy the proxy sees
// nothing and the delivery goes direct.
func TestWebhookWorker_SSRFRefusedThroughTheWorker(t *testing.T) {
	w := newWorld(t)
	var refused, allowed atomic.Int32
	observe := func(_ netip.AddrPort, ok bool) {
		if ok {
			allowed.Add(1)
		} else {
			refused.Add(1)
		}
	}
	job := w.job(w.client(3*time.Second, observe), nil, nil)

	for _, target := range []string{"169.254.169.254", "10.0.0.9"} {
		b := w.biz("SS", false)
		w.answer(b.host, target)
		w.tap(b, nil, "ok")
		refused.Store(0)
		_ = job.RunFor(context.Background(), b.id)
		d := w.deliveries(b)
		if len(w.rcv.to(b.host)) != 0 || d[0].class == nil || *d[0].class != "blocked_address" || refused.Load() == 0 {
			t.Fatalf("%s: %d requests, row %+v, %d refused dials", target, len(w.rcv.to(b.host)), d[0], refused.Load())
		}
	}

	rb := w.biz("RB", false)
	w.answerFunc(rb.host, func(n int) []netip.Addr {
		if n == 0 {
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}
		}
		return []netip.Addr{netip.MustParseAddr("10.0.0.9")}
	})
	w.tap(rb, nil, "ok")
	w.tap(rb, nil, "ok")
	_ = job.RunFor(context.Background(), rb.id)
	d := w.deliveries(rb)
	if len(w.rcv.to(rb.host)) != 1 || d[0].state != delivery.StateDelivered || d[1].class == nil || *d[1].class != "blocked_address" {
		t.Fatalf("rebinding: %d requests, rows %+v / %+v", len(w.rcv.to(rb.host)), d[0], d[1])
	}

	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxied.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()
	t.Setenv("HTTPS_PROXY", proxy.URL)
	t.Setenv("https_proxy", proxy.URL)
	px := w.biz("PX", false)
	w.tap(px, nil, "ok")
	if err := w.job(w.client(3*time.Second, nil), nil, nil).RunFor(context.Background(), px.id); err != nil {
		t.Fatal(err)
	}
	if proxied.Load() != 0 || len(w.rcv.to(px.host)) != 1 {
		t.Fatalf("with HTTPS_PROXY set: %d at the proxy, %d at the receiver", proxied.Load(), len(w.rcv.to(px.host)))
	}
}

// TestWebhookWorker_URLAndURLErrorNeverReachLogOrAudit is CLAUDE.md §7 on the worker's path with REAL
// network failures -- a name that does not resolve, a certificate that does not name the host, a
// receiver that does not answer in time, nothing listening, a blocked address, 4xx and 5xx -- through
// K-19's worker and the delivery job, the process log (slog's default too) captured: the log and the
// business's trail (a resend's slot row included) hold no address, host, path, key, nor any of Go's
// error words; each row holds its closed class.
func TestWebhookWorker_URLAndURLErrorNeverReachLogOrAudit(t *testing.T) {
	w := newWorld(t)
	var logs bytes.Buffer
	var logMu sync.Mutex
	logger := slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		logMu.Lock()
		defer logMu.Unlock()
		return logs.Write(p)
	}), &slog.HandlerOptions{Level: slog.LevelDebug}))
	prev := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(prev) })

	type scenario struct {
		b     wbiz
		class string
	}
	var all []scenario
	add := func(code, class string, prepare func(b *wbiz)) {
		b := w.biz(code, false)
		if prepare != nil {
			prepare(&b)
		}
		w.tap(b, nil, "ok")
		all = append(all, scenario{b, class})
	}
	add("NX", "refused", func(b *wbiz) {
		w.mu.Lock()
		delete(w.names, b.host) // NXDOMAIN
		w.mu.Unlock()
	})
	add("TL", "tls", func(b *wbiz) {
		// A two-label name under example.com: the test certificate's *.example.com does not cover it.
		host := "deep." + b.host
		w.answer(host, "127.0.0.1")
		w.exec(`UPDATE webhook_endpoints SET url = $2 WHERE tenant_id = $1`, b.id, "https://"+host+b.path)
		b.host = host
	})
	add("TO", "timeout", nil)
	add("BL", "blocked_address", func(b *wbiz) { w.answer(b.host, "10.0.0.7") })
	add("FF", "http_4xx", nil)
	add("FX", "http_5xx", nil)
	slow, four, five := all[2].b.host, all[4].b.host, all[5].b.host
	release := make(chan struct{})
	defer close(release)
	w.rcv.set(func(r *http.Request) int {
		switch r.Host {
		case slow:
			select {
			case <-release:
			case <-time.After(3 * time.Second):
			}
			return 204
		case four:
			return 404
		case five:
			return 502
		}
		return 204
	}, nil)

	// Nothing listening: a port that was open a moment ago and is closed now.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, closedPort, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	cp, _ := strconv.Atoi(closedPort)
	rf := w.biz("RF", false)
	w.tap(rf, nil, "ok")

	run := func(c delivery.Sender, bs ...wbiz) {
		worker, err := webhook.NewWorker(logger, w.job(c, nil, logger))
		if err != nil {
			t.Fatal(err)
		}
		if err := worker.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		for _, b := range bs {
			worker.Notice(b.id)
		}
		deadline := time.Now().Add(20 * time.Second)
		for time.Now().Before(deadline) {
			done := true
			for _, b := range bs {
				if d := w.deliveries(b); len(d) != 1 || d[0].attempt == 0 {
					done = false
				}
			}
			if done {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if err := worker.Drain(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	var bs []wbiz
	for _, s := range all {
		bs = append(bs, s.b)
	}
	run(w.client(500*time.Millisecond, nil), bs...)
	run(egress.SeamClient(loopback, uint16(cp), w.dns, w.rcv.roots(), 2*time.Second, nil), rf)
	all = append(all, scenario{rf, "refused"})

	if err := w.svc.Resend(w.ctx, all[5].b.id, all[5].b.owner, w.deliveries(all[5].b)[0].id); err != nil {
		t.Fatalf("resend: %v", err)
	}

	logMu.Lock()
	logText := logs.String()
	logMu.Unlock()
	forbidden := []string{"example.com", "https://", "/hooks/", "tt_whsec_", "127.0.0.1", "10.0.0.7", "dial ", "lookup",
		"x509", "tls:", "no such host", "connection refused", "certificate", `Post "`, "deadline exceeded", ":" + closedPort}
	for _, s := range all {
		d := w.deliveries(s.b)
		if len(d) != 1 || d[0].class == nil || *d[0].class != s.class {
			t.Errorf("%s: row %+v, want class %s", s.b.code, d, s.class)
		}
		var trail strings.Builder
		rows, err := w.owner().Query(w.ctx, `SELECT action || ' ' || coalesce(target, '') || ' ' || coalesce(detail::text, '') FROM audit_log WHERE tenant_id = $1`, s.b.id)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				t.Fatal(err)
			}
			trail.WriteString(line + "\n")
		}
		rows.Close()
		for where, text := range map[string]string{"the process log": logText, "the trail": trail.String()} {
			for _, leak := range append(forbidden, s.b.host, s.b.path) {
				if strings.Contains(text, leak) {
					t.Errorf("%s holds %q (%s)", where, leak, s.b.code)
				}
			}
		}
	}
	if !strings.Contains(logText, "webhook: a delivery failed") || !strings.Contains(logText, "class=timeout") ||
		!strings.Contains(logText, "class=refused") {
		t.Fatal("CONTROL: the log does not hold the failures' lines -- it was not captured")
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// TestWebhookPayload_CarriesNothingButTheFeedItem is K-1f/K-39 on the body: the evidence columns of the
// recorded rows carry a canary (note, address, coordinates, policy context, rule) and the employee an
// e-mail address; no body holds any of them, nor the words verdict, policy or gps; every body's keys are
// exactly the feed item's (punchwire.Punch) plus event and deliveryId.
func TestWebhookPayload_CarriesNothingButTheFeedItem(t *testing.T) {
	w := newWorld(t)
	kf, km := w.biz("PK", false), w.biz("PM", false)
	w.holding(kf, km)
	w.tap(kf, nil, "ok")
	w.tap(km, &kf, "ok")
	job := w.job(w.client(5*time.Second, nil), nil, nil)
	for _, b := range []wbiz{kf, km} {
		if err := job.RunFor(context.Background(), b.id); err != nil {
			t.Fatal(err)
		}
	}
	got := w.rcv.all()
	if len(got) != 3 {
		t.Fatalf("%d bodies, want 3", len(got))
	}
	// directionSource: M15-13 (00067) -- the feed item gained it, so the body carries it too.
	top := []string{"channel", "deliveryId", "direction", "directionSource", "employee", "event", "id", "location", "punchTime", "recordedAt", "status", "tagId", "version"}
	for _, h := range got {
		body := string(h.body)
		for _, leak := range []string{w.canary, strings.ToLower(w.canary), "198.51.100.213", "35.98", "14.12", "verdict", "policy",
			"gps", "note", "matched", "@api10.example.test", "trust"} {
			if strings.Contains(body, leak) {
				t.Errorf("a body holds %q: %s", leak, body)
			}
		}
		m := decode(t, h.body)
		var keys []string
		for k := range m {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		if !slices.Equal(keys, top) {
			t.Errorf("body keys %v, want %v", keys, top)
		}
		for _, sub := range []struct {
			name string
			want []string
		}{{"employee", []string{"company", "employeeCode", "externalRef", "id"}}, {"location", []string{"company", "externalRef", "id", "name"}}} {
			mm, _ := m[sub.name].(map[string]any)
			var ks []string
			for k := range mm {
				ks = append(ks, k)
			}
			slices.Sort(ks)
			if !slices.Equal(ks, sub.want) {
				t.Errorf("%s keys %v, want %v", sub.name, ks, sub.want)
			}
		}
	}
}

// TestWebhookPayload_CarriesTheRecordsDirectionSource (M15-13; ADR 0032 §B.6): a body's
// directionSource is the event's, not a constant -- an employee's choice arrives as "employee" in
// the employer's body AND in the site's body of a holding punch, and a row with no stored source (the
// engine's choice) as "auto".
func TestWebhookPayload_CarriesTheRecordsDirectionSource(t *testing.T) {
	w := newWorld(t)
	kf, km := w.biz("PK", false), w.biz("PM", false)
	w.holding(kf, km)
	employee := "employee"
	chosen := w.tapChosen(kf, nil, "ok", &employee)
	engine := w.tapChosen(kf, nil, "ok", nil)
	across := w.tapChosen(km, &kf, "ok", &employee)
	job := w.job(w.client(5*time.Second, nil), nil, nil)
	for _, b := range []wbiz{kf, km} {
		if err := job.RunFor(context.Background(), b.id); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string]string{"pch_" + chosen.String(): "employee", "pch_" + engine.String(): "auto", "pch_" + across.String(): "employee"}
	seen := map[string]int{}
	for _, h := range w.rcv.all() {
		m := decode(t, h.body)
		id, _ := m["id"].(string)
		got, _ := m["directionSource"].(string)
		seen[id]++
		if got != want[id] {
			t.Errorf("the body of %s carries directionSource %v, want %q", id, m["directionSource"], want[id])
		}
	}
	// KF hears its two taps and the site's row of KM's; KM the employer's row of it.
	if len(seen) != 3 || seen["pch_"+across.String()] != 2 || seen["pch_"+chosen.String()] != 1 || seen["pch_"+engine.String()] != 1 {
		t.Errorf("bodies per punch %v, want each own tap once and the holding punch twice", seen)
	}
}

// recorder is a K-19 set that remembers what it was told.
type recorder struct {
	mu   sync.Mutex
	seen []uuid.UUID
}

func (r *recorder) Notice(t uuid.UUID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, t)
}

func (r *recorder) got() []uuid.UUID {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.seen)
}

// TestWebhookWorker_AnApprovalNoticesBothBusinesses is K-19's approval entry (ADR 0028 §6; API-4 note
// item 9, devolved to API-10): a manager's approval of a holding flag (KM's employee at KF's plaque)
// notices KM AND KF after its commit, and their deliveries then go; approving a flag at KM's own plaque
// notices KM alone; a rejection notices nobody (it writes no event).
func TestWebhookWorker_AnApprovalNoticesBothBusinesses(t *testing.T) {
	w := newWorld(t)
	kf, km := w.biz("AF", false), w.biz("AM", false)
	w.holding(kf, km)
	trail, err := audit.New(w.app)
	if err != nil {
		t.Fatal(err)
	}
	rev, err := review.NewReviewer(w.app, trail, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	rev.NoticeWorkTo(rec)

	cross := w.tap(km, &kf, "flag")
	own := w.tap(km, nil, "flag")
	rejected := w.tap(km, nil, "flag")
	if err := rev.Record(w.ctx, review.Decision{TenantID: km.id, TransactionID: rejected, ReviewerID: km.owner, Outcome: review.Rejected}); err != nil {
		t.Fatal(err)
	}
	if got := rec.got(); len(got) != 0 {
		t.Fatalf("a rejection noticed %v", got)
	}
	if err := rev.Record(w.ctx, review.Decision{TenantID: km.id, TransactionID: own, ReviewerID: km.owner, Outcome: review.Approved}); err != nil {
		t.Fatal(err)
	}
	if got := rec.got(); !slices.Equal(got, []uuid.UUID{km.id}) {
		t.Fatalf("an approval at the business's own plaque noticed %v, want KM alone", got)
	}
	if err := rev.Record(w.ctx, review.Decision{TenantID: km.id, TransactionID: cross, ReviewerID: km.owner, Outcome: review.Approved}); err != nil {
		t.Fatal(err)
	}
	got := rec.got()[1:]
	if !slices.Equal(got, []uuid.UUID{km.id, kf.id}) {
		t.Fatalf("a holding approval noticed %v, want KM then KF", got)
	}
	job := w.job(w.client(5*time.Second, nil), nil, nil)
	for _, b := range got {
		if err := job.RunFor(context.Background(), b); err != nil {
			t.Fatal(err)
		}
	}
	if len(w.rcv.to(km.host)) != 2 || len(w.rcv.to(kf.host)) != 1 {
		t.Fatalf("after the approvals: KM received %d (want 2), KF %d (want 1)", len(w.rcv.to(km.host)), len(w.rcv.to(kf.host)))
	}
}

// TestWebhookResend_TheGatesTheBucketAndTheNotice: "resend" (ADR 0028 §6) refuses a delivered delivery,
// another business's (and an unknown) id, one in flight, a closed API, a switched-off endpoint and a
// manager -- each spending NO slot; a full bucket (ten pings and resends this hour) refuses the
// eleventh; an accepted resend writes one webhook_delivery.resend slot naming the delivery (no address)
// and notices the business (K-19).
func TestWebhookResend_TheGatesTheBucketAndTheNotice(t *testing.T) {
	w := newWorld(t)
	b, other := w.biz("RS", false), w.biz("RO", false)
	rec := &recorder{}
	w.svc.NoticeWorkTo(rec)
	defer w.svc.NoticeWorkTo(nil)
	w.tap(b, nil, "ok")
	w.tap(b, nil, "ok")
	w.tap(other, nil, "ok")
	job := w.job(w.client(5*time.Second, nil), nil, nil)
	w.rcv.set(func(*http.Request) int { return 500 }, nil)
	if err := job.RunFor(context.Background(), other.id); err == nil {
		t.Log("other: a later run expected; nil is fine too")
	}
	rows := w.deliveries(b)
	// Deliver the first; leave the second pending.
	w.rcv.set(nil, nil)
	w.exec(`UPDATE webhook_deliveries SET next_attempt_at = clock_timestamp() + interval '1 hour' WHERE tenant_id = $1 AND id = $2`, b.id, rows[1].id)
	if err := job.RunFor(context.Background(), b.id); err == nil {
		t.Log("b: one pending later")
	}
	rows = w.deliveries(b)
	delivered, pending := rows[0].id, rows[1].id
	if rows[0].state != delivery.StateDelivered || rows[1].state != delivery.StatePending {
		t.Fatalf("setup: %+v", rows)
	}
	slots := func() int { return countTrail(t, w, b, endpoints.ActionResend) }

	for name, c := range map[string]struct {
		actor, id uuid.UUID
		want      error
		prepare   func()
		undo      func()
	}{
		"delivered":          {b.owner, delivered, endpoints.ErrDelivered, nil, nil},
		"another business's": {b.owner, w.deliveries(other)[0].id, endpoints.ErrNoDelivery, nil, nil},
		"unknown":            {b.owner, uuid.New(), endpoints.ErrNoDelivery, nil, nil},
		"manager":            {b.manager, pending, endpoints.ErrNotPermitted, nil, nil},
		"in flight": {b.owner, pending, endpoints.ErrInFlight,
			func() {
				w.exec(`UPDATE webhook_deliveries SET lease_until = clock_timestamp() + interval '1 minute' WHERE tenant_id = $1 AND id = $2`, b.id, pending)
			},
			func() {
				w.exec(`UPDATE webhook_deliveries SET lease_until = NULL WHERE tenant_id = $1 AND id = $2`, b.id, pending)
			}},
		"closed API": {b.owner, pending, endpoints.ErrAPIClosed,
			func() { w.exec(`UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, b.id) },
			func() { w.exec(`UPDATE tenants SET api_enabled_at = now() WHERE id = $1`, b.id) }},
		"switched off": {b.owner, pending, endpoints.ErrDisabled,
			func() { _, _ = w.svc.Disable(w.ctx, b.id, b.owner) },
			// Switching on notices the business itself (TestWebhookEndpoints_ReopeningNoticesTheBusiness);
			// this test counts the resend's notices only.
			func() { w.svc.NoticeWorkTo(nil); _, _ = w.svc.Enable(w.ctx, b.id, b.owner); w.svc.NoticeWorkTo(rec) }},
	} {
		if c.prepare != nil {
			c.prepare()
		}
		err := w.svc.Resend(w.ctx, b.id, c.actor, c.id)
		if c.undo != nil {
			c.undo()
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", name, err, c.want)
		}
	}
	if slots() != 0 || len(rec.got()) != 0 {
		t.Fatalf("refused resends spent %d slots, noticed %v", slots(), rec.got())
	}

	if err := w.svc.Resend(w.ctx, b.id, b.owner, pending); err != nil {
		t.Fatal(err)
	}
	if got := rec.got(); !slices.Equal(got, []uuid.UUID{b.id}) || slots() != 1 {
		t.Fatalf("an accepted resend: noticed %v, %d slots", got, slots())
	}
	var target, detail string
	if err := w.owner().QueryRow(w.ctx, `SELECT target, detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2`, b.id, endpoints.ActionResend).
		Scan(&target, &detail); err != nil {
		t.Fatal(err)
	}
	if target != pending.String() || strings.Contains(detail, b.host) || !strings.Contains(detail, b.endpoint.String()) {
		t.Fatalf("the slot row: %s %s", target, detail)
	}
	if d := w.deliveries(b)[1]; d.state != delivery.StatePending || d.dueIn > 1 {
		t.Fatalf("after the resend: %+v (want due now)", d)
	}
	// Nine more slots (pings) fill the bucket; the next resend is refused and spends nothing.
	w.exec(`INSERT INTO audit_log (tenant_id, actor_id, action, target, detail)
	        SELECT $1, $2, $3, gen_random_uuid()::text, '{"via":"panel"}' FROM generate_series(1, 9)`, b.id, b.owner, endpoints.ActionPing)
	var full *endpoints.BucketFull
	if err := w.svc.Resend(w.ctx, b.id, b.owner, pending); !errors.As(err, &full) || slots() != 1 {
		t.Fatalf("the eleventh: %v, %d resend slots", err, slots())
	}
}

// TestWebhookDeliveries_PagesNewestFirstAndNamesNoAddress: the owner's log reads 30 deliveries as 25 +
// 5, newest first, no row twice, the second page's Next zero; each row names its punch; a manager is
// refused.
func TestWebhookDeliveries_PagesNewestFirstAndNamesNoAddress(t *testing.T) {
	w := newWorld(t)
	b := w.biz("LG", false)
	var punches []uuid.UUID
	for range 30 {
		punches = append(punches, w.tap(b, nil, "ok"))
	}
	first, err := w.svc.Deliveries(w.ctx, b.id, b.owner, endpoints.DeliveryPosition{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Deliveries) != endpoints.DeliveryPageSize || first.Next.IsZero() {
		t.Fatalf("first page: %d rows, next %v", len(first.Deliveries), first.Next)
	}
	second, err := w.svc.Deliveries(w.ctx, b.id, b.owner, first.Next)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Deliveries) != 5 || !second.Next.IsZero() {
		t.Fatalf("second page: %d rows, next %v", len(second.Deliveries), second.Next)
	}
	all := append(first.Deliveries, second.Deliveries...)
	seen := map[uuid.UUID]bool{}
	for i, d := range all {
		if seen[d.ID] {
			t.Fatalf("delivery %s twice", d.ID)
		}
		seen[d.ID] = true
		if i > 0 && (d.CreatedAt.After(all[i-1].CreatedAt)) {
			t.Fatal("not newest first")
		}
		if d.PunchID != punches[len(punches)-1-i] || d.State != "pending" || !d.Resendable() {
			t.Errorf("row %d: punch %s (want %s), %s", i, d.PunchID, punches[len(punches)-1-i], d.State)
		}
	}
	if _, err := w.svc.Deliveries(w.ctx, b.id, b.manager, endpoints.DeliveryPosition{}); !errors.Is(err, endpoints.ErrNotPermitted) {
		t.Fatalf("a manager's read: %v", err)
	}

	// A TIE: every delivery queued at one instant (the owner role re-dates them -- tappa_app cannot).
	// The id alone then orders the walk: 25 + 5, every delivery once.
	w.exec(`UPDATE webhook_deliveries SET created_at = '2026-10-15 12:00:00+00' WHERE tenant_id = $1`, b.id)
	seen = map[uuid.UUID]bool{}
	pos := endpoints.DeliveryPosition{}
	for pages := 0; ; pages++ {
		if pages > 3 {
			t.Fatal("the tied walk does not end")
		}
		p, err := w.svc.Deliveries(w.ctx, b.id, b.owner, pos)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range p.Deliveries {
			if seen[d.ID] {
				t.Fatalf("tied: delivery %s twice", d.ID)
			}
			seen[d.ID] = true
		}
		if p.Next.IsZero() {
			break
		}
		pos = p.Next
	}
	if len(seen) != 30 {
		t.Fatalf("tied: %d deliveries walked, want 30", len(seen))
	}
}

// TestWebhookWorker_AClaimInProgressDoesNotStallAnother is what SKIP LOCKED buys (the lease is what
// keeps a delivery from being sent twice -- TestWebhookWorker_TwoPodsSendOnce): while one worker's claim
// transaction holds the oldest delivery's row, a second worker takes the NEXT one at once instead of
// waiting on the lock; the held one is untouched.
func TestWebhookWorker_AClaimInProgressDoesNotStallAnother(t *testing.T) {
	w := newWorld(t)
	b := w.biz("SK", false)
	w.tap(b, nil, "ok")
	w.tap(b, nil, "ok")
	rows := w.deliveries(b)
	held, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- w.app.WithTenant(context.Background(), b.id, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT 1 FROM webhook_deliveries WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, b.id, rows[0].id); err != nil {
				return err
			}
			close(held)
			<-release
			return errors.New("roll back")
		})
	}()
	<-held
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := w.job(w.client(2*time.Second, nil), nil, nil).RunFor(ctx, b.id)
	close(release)
	<-done
	got := w.deliveries(b)
	if ctx.Err() != nil || len(w.rcv.to(b.host)) != 1 || got[0].state != delivery.StatePending || got[0].attempt != 0 ||
		got[1].state != delivery.StateDelivered {
		t.Fatalf("with the oldest row locked: run %v (ctx %v), %d sent, rows %s/%s", err, ctx.Err(), len(w.rcv.to(b.host)), got[0].state, got[1].state)
	}
}

// ---------------------------------------------------------------------------------------------------
// the review's round (M11 API-10 review: ORTA-1, ORTA-2, D-1, D-2)
// ---------------------------------------------------------------------------------------------------

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// countingDB counts the job's transactions (every claim, result and next-due read is one WithTenant).
type countingDB struct {
	inner *db.DB
	n     atomic.Int64
}

func (c *countingDB) WithTenant(ctx context.Context, id uuid.UUID, fn db.TxFunc) error {
	c.n.Add(1)
	return c.inner.WithTenant(ctx, id, fn)
}

// TestWebhookWorker_AHeldBusinessHasOneRecheck (ORTA-1) is the closed gate's recheck through the real
// worker and the real job: a business whose one delivery waits at a switched-off endpoint is noticed 50
// times (taps, /v1 requests), each notice run -- and afterwards the job runs about ONCE per recheck
// period (one claim transaction, rolled back; nothing sent), not once per notice. GateRecheck is
// shortened with Pace.
//
// Measured before the fix (a timer per answer): 50 notices -> ~50 claim transactions per period, growing
// with every notice until a restart. MUTANT: the worker's per-tenant timer removed -> red.
//
// Under load (M11 API-10, DÜŞÜK round) a rate's LOWER bound is not a measurement: a starved timer runs
// late, and 10 periods once held fewer than 5 claims. The upper bound stays a rate (load only slows
// the timer, it cannot add runs); the control -- the recheck comes back -- waits for five claims caused
// by nothing but the timer.
func TestWebhookWorker_AHeldBusinessHasOneRecheck(t *testing.T) {
	const period = 200 * time.Millisecond
	w := newWorld(t)
	b := w.biz("HR", false)
	w.tap(b, nil, "ok")
	if _, err := w.svc.Disable(w.ctx, b.id, b.owner); err != nil {
		t.Fatal(err)
	}
	cdb := &countingDB{inner: w.app}
	j, err := delivery.New(w.kek, cdb, w.client(time.Second, nil), discard())
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Pace(delivery.RunBudget, period); err != nil {
		t.Fatal(err)
	}
	worker, err := webhook.NewWorker(discard(), j)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worker.Drain(context.Background()) }()
	for range 50 {
		before := cdb.n.Load()
		worker.Notice(b.id)
		deadline := time.Now().Add(5 * time.Second)
		for cdb.n.Load() == before && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		time.Sleep(period / 50)
	}
	before := cdb.n.Load()
	time.Sleep(10 * period)
	per := float64(cdb.n.Load()-before) / 10
	t.Logf("50 notices of a held business -> %.1f claim transactions per recheck period, %d requests", per, len(w.rcv.to(b.host)))
	if per > 2 {
		t.Fatalf("%.1f claim transactions per recheck period after 50 notices (want ~1): the rechecks multiplied", per)
	}
	deadline := time.Now().Add(10 * time.Second)
	for cdb.n.Load()-before < 5 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := cdb.n.Load() - before; n < 5 || len(w.rcv.to(b.host)) != 0 {
		t.Fatalf("CONTROL: %d claim transactions since the notices stopped, %d requests -- the recheck does not come back, or the gate let one through",
			n, len(w.rcv.to(b.host)))
	}
}

// standIn is a second job in the loop -- API-6's invitation queue's place: it records when it was handed
// the business it watches, and what seen read at that moment (once).
type standIn struct {
	watch uuid.UUID
	seen  func() int
	at    atomic.Int64
	saw   atomic.Int64
}

func (s *standIn) RunFor(_ context.Context, tenant uuid.UUID) error {
	if tenant == s.watch && s.at.Load() == 0 {
		if s.seen != nil {
			s.saw.Store(int64(s.seen()))
		}
		s.at.Store(time.Now().UnixNano())
	}
	return nil
}

// TestWebhookWorker_ASlowReceiverDoesNotHoldTheOthers (ORTA-2): K-19's loop is one goroutine. A business
// with 50 due deliveries to a receiver that (a) never answers, (b) answers each slowly is being run when
// a second business is noticed; that business's delivery AND the loop's other job (the invitation
// queue's place) run within the run budget + one exchange (+ the database's slack) -- the slow run
// stops claiming at its budget and goes back into the set BEHIND the business noticed meanwhile. Budget
// and timeouts are shortened (Pace, the client's timeout).
//
// Measured before the fix (no budget, the set in map order): (a) ~15 s, (b) ~5 s -- 50 exchanges.
// MUTANT: the budget check removed -> red (both).
//
// THE BOUND IS COUNTED, NOT TIMED (M11 API-10, DÜŞÜK round: under CPU load the database's slack passed
// the second the time bound allowed). What the budget guarantees is how many of the slow business's
// exchanges can START after the fast business was noticed and before it is served: the run in flight
// keeps claiming only while its budget lasts, so at most budget/exchange + 1 more (the budget and the
// exchange are both wall clock -- load makes fewer exchanges fit, never more). Both the fast delivery
// (read by the receiver when its request arrives) and the other job (read when it runs) are held to
// that count; the times are logged.
func TestWebhookWorker_ASlowReceiverDoesNotHoldTheOthers(t *testing.T) {
	const budget = 300 * time.Millisecond
	w := newWorld(t)
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	for _, c := range []struct {
		name      string
		timeout   time.Duration
		answersIn time.Duration
	}{
		{"never answers", 300 * time.Millisecond, time.Minute},
		{"answers slowly", 2 * time.Second, 100 * time.Millisecond},
	} {
		slow, fast := w.biz("SL", false), w.biz("FA", false)
		for range 50 {
			w.tap(slow, nil, "ok")
		}
		w.tap(fast, nil, "ok")
		inSlow := make(chan struct{}, 1)
		var slowAtFast atomic.Int64
		slowAtFast.Store(-1)
		w.rcv.set(func(r *http.Request) int {
			if r.Host == fast.host {
				slowAtFast.CompareAndSwap(-1, int64(len(w.rcv.to(slow.host))))
			}
			if r.Host == slow.host {
				select {
				case inSlow <- struct{}{}:
				default:
				}
				select {
				case <-time.After(c.answersIn):
				case <-release:
				}
			}
			return 204
		}, nil)
		j := w.job(w.client(c.timeout, nil), nil, nil)
		if err := j.Pace(budget, delivery.GateRecheck); err != nil {
			t.Fatal(err)
		}
		other := &standIn{watch: fast.id, seen: func() int { return len(w.rcv.to(slow.host)) }}
		worker, err := webhook.NewWorker(discard(), j, other)
		if err != nil {
			t.Fatal(err)
		}
		if err := worker.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		worker.Notice(slow.id)
		<-inSlow
		began := time.Now()
		atNotice := len(w.rcv.to(slow.host)) // the one in flight
		worker.Notice(fast.id)
		var delivered time.Duration
		for time.Since(began) < 30*time.Second {
			if d := w.deliveries(fast); d[0].state == delivery.StateDelivered && other.at.Load() != 0 {
				delivered = time.Since(began)
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		ranOther := time.Duration(other.at.Load() - began.UnixNano())
		_ = worker.Drain(context.Background())
		exchange := min(c.timeout, c.answersIn)
		limit := int64(budget/exchange) + 1
		ahead, aheadOther := slowAtFast.Load()-int64(atNotice), other.saw.Load()-int64(atNotice)
		t.Logf("%s: the fast business's delivery waited %v (%d slow exchanges began meanwhile), the other job %v (%d); budget %v, one exchange %v, at most %d",
			c.name, delivered, ahead, ranOther, aheadOther, budget, exchange, limit)
		if delivered == 0 || other.at.Load() == 0 || slowAtFast.Load() < 0 {
			t.Errorf("%s: the fast business's delivery or the other job never ran within 30 s behind a slow receiver", c.name)
		} else if ahead > limit || aheadOther > limit {
			t.Errorf("%s: %d (delivery) and %d (other job) of the slow business's exchanges began after the fast business was noticed; want at most %d",
				c.name, ahead, aheadOther, limit)
		}
		if n := len(w.rcv.to(slow.host)); n >= 50 {
			t.Errorf("%s: the slow receiver got all %d requests before the fast one went", c.name, n)
		}
	}
}

// TestWebhookWorker_AStaleResultIsDropped (D-1) pins RecordWebhookDeliveryResult's `lease_until =
// @lease_until`: worker A's request hangs past its lease; worker B claims the delivery again and is IN
// FLIGHT; A's late answer (2xx) arrives -- and is dropped: B's claim is untouched (pending, no try
// spent, still leased). B's answer (503) is then recorded as the one try.
//
// MUTANT (measured): the lease condition made vacuous (`@lease_until IS NOT NULL`) -> A's stale result
// writes `delivered`, B's real one is then dropped -> red.
func TestWebhookWorker_AStaleResultIsDropped(t *testing.T) {
	w := newWorld(t)
	b := w.biz("SR", false)
	w.tap(b, nil, "ok")
	var calls atomic.Int32
	aIn, bIn := make(chan struct{}), make(chan struct{})
	releaseA, releaseB := make(chan struct{}), make(chan struct{})
	var onceA, onceB sync.Once
	openA, openB := func() { onceA.Do(func() { close(releaseA) }) }, func() { onceB.Do(func() { close(releaseB) }) }
	t.Cleanup(func() { openA(); openB() }) // a red run must not leave the receiver's handlers waiting
	w.rcv.set(func(*http.Request) int {
		switch calls.Add(1) {
		case 1:
			close(aIn)
			<-releaseA
			return 204
		default:
			close(bIn)
			<-releaseB
			return 503
		}
	}, nil)
	run := func() chan error {
		done := make(chan error, 1)
		go func() { done <- w.job(w.client(10*time.Second, nil), nil, nil).RunFor(context.Background(), b.id) }()
		return done
	}
	aDone := run()
	<-aIn
	w.exec(`UPDATE webhook_deliveries SET lease_until = clock_timestamp() - interval '1 second' WHERE tenant_id = $1`, b.id)
	bDone := run()
	<-bIn
	openA()
	if err := <-aDone; err != nil {
		var later *delivery.Later
		if !errors.As(err, &later) {
			t.Fatalf("A: %v", err)
		}
	}
	if d := w.deliveries(b)[0]; d.state != delivery.StatePending || d.attempt != 0 || !d.leased || d.class != nil {
		t.Fatalf("A's stale answer overwrote B's claim: %+v", d)
	}
	openB()
	<-bDone
	if d := w.deliveries(b)[0]; d.state != delivery.StatePending || d.attempt != 1 || d.class == nil || *d.class != "http_5xx" {
		t.Fatalf("B's answer: %+v (want pending, one try, http_5xx)", d)
	}
}

// TestWebhookEndpoints_ReopeningNoticesTheBusiness (D-2): the owner's acts that may open a gate a
// waiting delivery is held at -- switching the endpoint on, re-addressing it, rotating its key -- put the
// business into K-19's set after their commit; switching it off and a refused switch-on (the API
// closed) do not. End to end with the real worker (the recheck an hour away): a delivery held at a
// switched-off endpoint goes at once when it is switched on.
//
// MUTANT (measured): Enable's notice removed -> red (the recorder, and the delivery waits the hour).
func TestWebhookEndpoints_ReopeningNoticesTheBusiness(t *testing.T) {
	w := newWorld(t)
	b := w.biz("RN", false)
	rec := &recorder{}
	w.svc.NoticeWorkTo(rec)
	t.Cleanup(func() { w.svc.NoticeWorkTo(nil) })
	step := func(what string, act func() error, want int) {
		t.Helper()
		before := len(rec.got())
		_ = act()
		if got := len(rec.got()) - before; got != want {
			t.Fatalf("%s noticed the business %d times, want %d", what, got, want)
		}
	}
	step("switching off", func() error { _, err := w.svc.Disable(w.ctx, b.id, b.owner); return err }, 0)
	w.exec(`UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, b.id)
	step("a refused switch-on", func() error { _, err := w.svc.Enable(w.ctx, b.id, b.owner); return err }, 0)
	w.exec(`UPDATE tenants SET api_enabled_at = now() WHERE id = $1`, b.id)
	step("switching on", func() error { _, err := w.svc.Enable(w.ctx, b.id, b.owner); return err }, 1)
	step("re-addressing", func() error { _, err := w.svc.Readdress(w.ctx, b.id, b.owner, "https://"+b.host+b.path); return err }, 1)
	step("rotating", func() error { _, err := w.svc.Rotate(w.ctx, b.id, b.owner); return err }, 1)
	for _, id := range rec.got() {
		if id != b.id {
			t.Fatalf("noticed another business: %s", id)
		}
	}

	j := w.job(w.client(5*time.Second, nil), nil, nil)
	if err := j.Pace(delivery.RunBudget, time.Hour); err != nil {
		t.Fatal(err)
	}
	worker, err := webhook.NewWorker(discard(), j)
	if err != nil {
		t.Fatal(err)
	}
	if err := worker.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = worker.Drain(context.Background()) }()
	w.svc.NoticeWorkTo(worker)
	w.tap(b, nil, "ok") // queued while on (a switched-off endpoint gets no row -- 00049)
	if _, err := w.svc.Disable(w.ctx, b.id, b.owner); err != nil {
		t.Fatal(err)
	}
	worker.Notice(b.id)
	time.Sleep(300 * time.Millisecond)
	if n := len(w.rcv.to(b.host)); n != 0 {
		t.Fatalf("CONTROL: %d sent through a switched-off endpoint", n)
	}
	if _, err := w.svc.Enable(w.ctx, b.id, b.owner); err != nil {
		t.Fatal(err)
	}
	// The receiver counts the request when it ARRIVES; the result is written after it answered -- so the
	// wait is for the row (M11 API-10, DÜŞÜK round: read on arrival, it was still pending under load).
	deadline := time.Now().Add(5 * time.Second)
	for w.deliveries(b)[0].state != delivery.StateDelivered && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if d := w.deliveries(b); len(w.rcv.to(b.host)) != 1 || d[0].state != delivery.StateDelivered {
		t.Fatalf("switched on: %d sent, row %+v (want the held delivery sent at once)", len(w.rcv.to(b.host)), d)
	}
}
