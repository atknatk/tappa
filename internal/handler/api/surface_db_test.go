package api

// surface_db_test.go -- the /v1 surface over the REAL chain, runner and database: K-22 and
// revocation decided on every request (no cache), another business's key sees nothing of this one,
// Idempotency-Key through HTTP under concurrency, last_used_at and K-19's notice.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/test/fixtures"
)

type dbRig struct {
	t      *testing.T
	d      *db.DB
	owner  *pgx.Conn
	keys   *apikey.Keys
	trail  *audit.Recorder
	work   *workSet
	venues *tenant.Venues
	staff  *tenant.Staff
	writes *apikey.Writes
	s      *Surface
	h      http.Handler
	// The suspension is OP-15's column, read by the shipped tenant.WriteSuspension (API-4b), and a
	// test suspends a business the way the operator does -- suspension_db_test.go's suspend.
	op operatorFixture
	// afterAdmit, when set, runs inside the /_probe write right after its checkpoint passed -- the
	// write's transaction open, before its effect (suspension_db_test.go's in-flight write).
	afterAdmit atomic.Pointer[func()]
}

func newDBRig(t *testing.T, opts ...func(*Config)) *dbRig {
	t.Helper()
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres; the owner opens K-22's gate)")
	}
	ctx := context.Background()
	d, err := db.New(ctx, &config.Config{DatabaseURL: appDSN})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	mac := make([]byte, 32)
	if _, err := rand.Read(mac); err != nil {
		t.Fatal(err)
	}
	trail, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := apikey.NewKeys(apikey.LivePrefix, mac, d, trail)
	if err != nil {
		t.Fatal(err)
	}
	chain, err := apikey.NewAuthenticator(apikey.LivePrefix, mac, d, d)
	if err != nil {
		t.Fatal(err)
	}
	writes, err := apikey.NewWrites(d)
	if err != nil {
		t.Fatal(err)
	}
	work := &workSet{}
	venues, err := tenant.NewVenues(d, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	staff, err := tenant.NewStaff(d, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	punches, err := tenant.NewPunchFeed(d)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := tenant.NewEmployeeDirectory(d)
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{Host: apiHost, Keys: chain, Writes: writes, Suspension: tenant.WriteSuspension{}, Work: work,
		Locations: venues, Employees: staff, Punches: punches, Directory: directory, Log: slog.New(slog.DiscardHandler)}
	for _, o := range opts {
		o(&cfg)
	}
	s, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := &dbRig{t: t, d: d, owner: owner, keys: keys, trail: trail, work: work, venues: venues, staff: staff,
		writes: writes, s: s}
	withTestRoutes(s, func(rt chi.Router) {
		// A read in the key's tenant: an api_keys row by id, WithTenant + an explicit filter -- the
		// shape every API-5..8 read takes.
		rt.Get("/_keys/{id}", func(w http.ResponseWriter, req *http.Request) {
			p, _ := PrincipalOf(req)
			id, err := uuid.Parse(chi.URLParam(req, "id"))
			if err != nil {
				writeProblem(w, problemNotFound)
				return
			}
			var name string
			err = d.WithTenant(req.Context(), p.TenantID, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT name FROM api_keys WHERE tenant_id = $1 AND id = $2`, p.TenantID, id).Scan(&name)
			})
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				writeProblem(w, problemNotFound)
			case err != nil:
				writeProblem(w, problemUnavailable)
			default:
				_, _ = fmt.Fprintf(w, "%s", name)
			}
		})
		// A write with a committed effect: an audit row naming the request's body, through the runner.
		rt.Put("/_probe/{ref}", func(w http.ResponseWriter, req *http.Request) {
			body, prob := readBody(req)
			if prob != nil {
				writeProblem(w, prob)
				return
			}
			p, _ := PrincipalOf(req)
			out, _, ok := s.write(w, req, body, func(ctx context.Context, tx pgx.Tx, g *Gate) (apikey.Outcome, error) {
				if err := g.Admit(ctx, WriteLocationPut); err != nil {
					return apikey.Outcome{}, err
				}
				if hook := r.afterAdmit.Load(); hook != nil {
					(*hook)()
				}
				id, err := trail.RecordTx(ctx, tx, audit.Event{TenantID: p.TenantID, ActorID: p.Actor(), Action: "api.surface_probe",
					Target: chi.URLParam(req, "ref"), Detail: p.Attribution()})
				if err != nil {
					return apikey.Outcome{}, err
				}
				return apikey.Outcome{Status: http.StatusCreated, Ref: &id}, nil
			})
			if ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(out.Status)
				_ = json.NewEncoder(w).Encode(map[string]string{"id": out.Ref.String()})
			}
		})
		// A write nobody classified (the zero WriteClass, ADR 0028 §10: "tabloda adı geçmeyen her M11
		// yazması askıda kapalı") with a committed effect: an audit row, through the runner.
		rt.Put("/_unclassified/{ref}", func(w http.ResponseWriter, req *http.Request) {
			body, prob := readBody(req)
			if prob != nil {
				writeProblem(w, prob)
				return
			}
			p, _ := PrincipalOf(req)
			out, _, ok := s.write(w, req, body, func(ctx context.Context, tx pgx.Tx, g *Gate) (apikey.Outcome, error) {
				var unclassified WriteClass
				if err := g.Admit(ctx, unclassified); err != nil {
					return apikey.Outcome{}, err
				}
				id, err := trail.RecordTx(ctx, tx, audit.Event{TenantID: p.TenantID, ActorID: p.Actor(), Action: "api.surface_unclassified",
					Target: chi.URLParam(req, "ref"), Detail: p.Attribution()})
				if err != nil {
					return apikey.Outcome{}, err
				}
				return apikey.Outcome{Status: http.StatusCreated, Ref: &id}, nil
			})
			if ok {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(out.Status)
				_ = json.NewEncoder(w).Encode(map[string]string{"id": out.Ref.String()})
			}
		})
	})
	r.h = httpx.NewRouter(&config.Config{APIHost: apiHost}, nil, s)
	return r
}

// business is a tenant with an active owner; open says whether the operator opened its API.
func (r *dbRig) business(open bool) (tenant, owner uuid.UUID) {
	r.t.Helper()
	tenant, owner = uuid.New(), uuid.New()
	err := r.d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'API-4 fixture', $2, 'restaurant', 'multi')`,
			tenant, "VAT-API4-"+tenant.String()); e != nil {
			return e
		}
		_, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status) VALUES ($1, $2, 'API-4 Owner', $3, $4, 'owner', 'active')`,
			owner, tenant, "api4-"+owner.String()+"@m11.example", fixtures.UnusablePasswordHash)
		return e
	})
	if err != nil {
		r.t.Fatal(err)
	}
	r.setAPI(tenant, true)
	if !open {
		r.setAPI(tenant, false)
	}
	return tenant, owner
}

func (r *dbRig) setAPI(tenant uuid.UUID, open bool) {
	r.t.Helper()
	q := `UPDATE tenants SET api_code = 'APIFOUR', api_enabled_at = now() WHERE id = $1`
	if !open {
		q = `UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`
	}
	if _, err := r.owner.Exec(context.Background(), q, tenant); err != nil {
		r.t.Fatal(err)
	}
}

// key issues a key from the owner's panel service.
func (r *dbRig) key(tenant, owner uuid.UUID) (raw string, id uuid.UUID) {
	r.t.Helper()
	c, err := r.keys.Create(context.Background(), apikey.CreateCommand{TenantID: tenant, ActorID: owner, Name: "k-" + uuid.NewString()[:8]})
	if err != nil {
		r.t.Fatal(err)
	}
	return c.Key.RevealToTheOwnerOnce(), c.ID
}

func (r *dbRig) do(method, path, raw string, body string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://"+apiHost+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+raw)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	return w
}

// TestApiSurface_RevocationAndClosingTakeEffectOnTheNextRequest (ADR 0028 §3, K-22; plan §5 API-4
// "K-22 / 401-403, önbelleksiz"): a live key on an open API is 200 and its last_used_at is written;
// the operator closes the API -> the NEXT request is 403 api_disabled; reopened -> 200; the owner
// revokes the key -> the next request is 401, and a closed API does not turn a revoked key into
// 403 (revoked outranks closed). Nothing in between was cached.
func TestApiSurface_RevocationAndClosingTakeEffectOnTheNextRequest(t *testing.T) {
	r := newDBRig(t)
	tenant, owner := r.business(true)
	raw, keyID := r.key(tenant, owner)
	path := "/v1/_keys/" + keyID.String()
	if w := r.do(http.MethodGet, path, raw, ""); w.Code != 200 {
		t.Fatalf("a live key: %d %s", w.Code, w.Body.String())
	}
	var used *time.Time
	if err := r.owner.QueryRow(context.Background(), `SELECT last_used_at FROM api_keys WHERE id = $1`, keyID).Scan(&used); err != nil || used == nil {
		t.Fatalf("last_used_at after a request: %v %v", used, err)
	}
	if got := r.work.got(); len(got) != 1 || got[0] != tenant {
		t.Fatalf("K-19 noticed %v, want the key's tenant", got)
	}
	r.setAPI(tenant, false)
	if w := r.do(http.MethodGet, path, raw, ""); w.Code != 403 || problemOf(t, w).Code != CodeAPIDisabled {
		t.Fatalf("after closing: %d %s, want 403 api_disabled", w.Code, w.Body.String())
	}
	r.setAPI(tenant, true)
	if w := r.do(http.MethodGet, path, raw, ""); w.Code != 200 {
		t.Fatalf("after reopening: %d", w.Code)
	}
	if _, err := r.keys.Revoke(context.Background(), apikey.RevokeCommand{TenantID: tenant, ActorID: owner, KeyID: keyID}); err != nil {
		t.Fatal(err)
	}
	if w := r.do(http.MethodGet, path, raw, ""); w.Code != 401 || problemOf(t, w).Code != CodeUnauthorized {
		t.Fatalf("after revoking: %d %s, want 401", w.Code, w.Body.String())
	}
	r.setAPI(tenant, false)
	if w := r.do(http.MethodGet, path, raw, ""); w.Code != 401 {
		t.Fatalf("revoked AND closed: %d, want 401 (revoked outranks closed)", w.Code)
	}
}

// TestApiSurface_CKeyCannotSeeAnotherTenant (plan §5 API-4: "C'nin anahtarı başka tenant'ı göremez";
// ADR 0028 §2): C's key reading A's key row by its id is 404 -- the request's tenant is the KEY's,
// never one the request names, and the read is WithTenant(C) + an explicit filter; CONTROL: A's key
// reads it. C's write with an Idempotency-Key A recorded is C's own first request, never A's result
// (a fresh id, no Idempotent-Replayed).
func TestApiSurface_CKeyCannotSeeAnotherTenant(t *testing.T) {
	r := newDBRig(t)
	a, aOwner := r.business(true)
	c, cOwner := r.business(true)
	aRaw, aKey := r.key(a, aOwner)
	cRaw, _ := r.key(c, cOwner)
	if w := r.do(http.MethodGet, "/v1/_keys/"+aKey.String(), cRaw, ""); w.Code != 404 {
		t.Fatalf("C's key reading A's key row: %d %s, want 404", w.Code, w.Body.String())
	}
	if w := r.do(http.MethodGet, "/v1/_keys/"+aKey.String(), aRaw, ""); w.Code != 200 {
		t.Fatalf("CONTROL: A's own key: %d", w.Code)
	}
	idem := "shared-" + uuid.NewString()
	wa := r.do(http.MethodPut, "/v1/_probe/x", aRaw, `{"n":1}`, "Idempotency-Key", idem)
	wc := r.do(http.MethodPut, "/v1/_probe/x", cRaw, `{"n":1}`, "Idempotency-Key", idem)
	if wa.Code != 201 || wc.Code != 201 || wc.Header().Get("Idempotent-Replayed") != "" || wa.Body.String() == wc.Body.String() {
		t.Fatalf("A then C with one idempotency key: A %d %s, C %d %s %v; want two first requests", wa.Code, wa.Body.String(), wc.Code, wc.Body.String(), wc.Header())
	}
}

// TestApiIdempotency_OverHTTP: 12 concurrent PUTs with one Idempotency-Key and one body ->
// exactly one 201 that did the work and 11 replays of its id (Idempotent-Replayed: true), ONE audit
// row; the same key with another body -> 422 idempotency_key_reused; without the header, two PUTs
// are two writes. Under -race.
func TestApiIdempotency_OverHTTP(t *testing.T) {
	r := newDBRig(t)
	tenant, owner := r.business(true)
	raw, _ := r.key(tenant, owner)
	idem, ref := "order-"+uuid.NewString(), "ref-"+uuid.NewString()
	const n = 12
	type answer struct {
		code     int
		replayed bool
		body     string
	}
	answers := make(chan answer, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := r.do(http.MethodPut, "/v1/_probe/"+ref, raw, `{"a":1}`, "Idempotency-Key", idem)
			answers <- answer{w.Code, w.Header().Get("Idempotent-Replayed") == "true", w.Body.String()}
		}()
	}
	close(start)
	wg.Wait()
	close(answers)
	fresh, replays, bodies := 0, 0, map[string]bool{}
	for a := range answers {
		if a.code != 201 {
			t.Fatalf("a request answered %d %s", a.code, a.body)
		}
		if a.replayed {
			replays++
		} else {
			fresh++
		}
		bodies[a.body] = true
	}
	if fresh != 1 || replays != n-1 || len(bodies) != 1 {
		t.Fatalf("%d fresh, %d replays, %d distinct ids; want 1, %d, 1", fresh, replays, len(bodies), n-1)
	}
	if got := r.auditCount(tenant, ref); got != 1 {
		t.Fatalf("%d committed effects, want 1", got)
	}
	if w := r.do(http.MethodPut, "/v1/_probe/"+ref, raw, `{"a":2}`, "Idempotency-Key", idem); w.Code != 422 || problemOf(t, w).Code != CodeIdempotencyKeyReused {
		t.Fatalf("another body under the key: %d %s", w.Code, w.Body.String())
	}
	plain := "plain-" + uuid.NewString()
	for range 2 {
		if w := r.do(http.MethodPut, "/v1/_probe/"+plain, raw, `{}`); w.Code != 201 {
			t.Fatalf("a write without the header: %d", w.Code)
		}
	}
	if got := r.auditCount(tenant, plain); got != 2 {
		t.Fatalf("two writes without the header made %d effects, want 2", got)
	}
}

func (r *dbRig) auditCount(tenant uuid.UUID, target string) (n int64) {
	r.t.Helper()
	err := r.d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'api.surface_probe' AND target = $2`, tenant, target).Scan(&n)
	})
	if err != nil {
		r.t.Fatal(err)
	}
	return n
}
