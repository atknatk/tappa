package api

// surface_test.go -- the /v1 surface without a database: the chain's order and buckets with the
// REAL apikey.Authenticator over a counting resolver, the host gate through the shipped router, the
// error contract, the body bound, the limit, the checkpoint's default, the OpenAPI document. The
// database half (the runner, revocation and K-22 through HTTP, another tenant's key) is
// surface_db_test.go.

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/httpx"
)

const apiHost = "api.taptime.mt"

// liveKey is a well-formed tt_live_ key, built at run time (redline R7d reads a key-shaped literal
// in source as a leak).
func liveKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return apikey.LivePrefix + base64.RawURLEncoding.EncodeToString(b)
}

// countingResolver stands in for the seventh resolver: it counts every lookup (a lookup IS a
// database round trip in production), remembers the hashes it was asked for (the canary test
// looks for them in the output), and answers what answer says.
type countingResolver struct {
	mu     sync.Mutex
	calls  int
	hashes []string
	answer func(hash string) (db.ResolvedAPIKey, error)
}

func (c *countingResolver) GetAPIKeyByHash(_ context.Context, h string) (db.ResolvedAPIKey, error) {
	c.mu.Lock()
	c.calls++
	c.hashes = append(c.hashes, h)
	c.mu.Unlock()
	return c.answer(h)
}

func (c *countingResolver) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// touchCounter is the Authenticator's Database: Touch's WithTenant, counted and not run; fail makes
// it fail the way an unreachable database would.
type touchCounter struct {
	n    atomic.Int64
	fail atomic.Bool
}

func (t *touchCounter) WithTenant(context.Context, uuid.UUID, db.TxFunc) error {
	t.n.Add(1)
	if t.fail.Load() {
		return errors.New("db: begin tx: connection refused")
	}
	return nil
}

// workSet records K-19's notices.
type workSet struct {
	mu      sync.Mutex
	tenants []uuid.UUID
}

func (w *workSet) Notice(t uuid.UUID) {
	w.mu.Lock()
	w.tenants = append(w.tenants, t)
	w.mu.Unlock()
}

func (w *workSet) got() []uuid.UUID {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.tenants)
}

// rig is a surface over the real chain with fakes behind it.
type rig struct {
	s        *Surface
	resolver *countingResolver
	touches  *touchCounter
	work     *workSet
	macKey   []byte
	logs     *bytes.Buffer
	h        http.Handler
}

type rigOption func(*Config)

func newRig(t *testing.T, answer func(hash string) (db.ResolvedAPIKey, error), opts ...rigOption) *rig {
	t.Helper()
	mac := make([]byte, 32)
	if _, err := rand.Read(mac); err != nil {
		t.Fatal(err)
	}
	res := &countingResolver{answer: answer}
	touches := &touchCounter{}
	chain, err := apikey.NewAuthenticator(apikey.LivePrefix, mac, res, touches)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(&syncWriter{w: logs}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	work := &workSet{}
	c := Config{Host: apiHost, Keys: chain, Writes: refuseWrites{}, Suspension: NoSuspensionColumn{}, Work: work,
		Locations: refuseLocations{}, Employees: refuseEmployees{}, Punches: refusePunches{}, Directory: refuseDirectory{}, Log: log}
	for _, o := range opts {
		o(&c)
	}
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{s: s, resolver: res, touches: touches, work: work, macKey: mac, logs: logs}
	withTestRoutes(s, func(r chi.Router) {
		r.Get("/_whoami", func(w http.ResponseWriter, r *http.Request) {
			p, _ := PrincipalOf(r)
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"tenant":%q}`, p.TenantID)
		})
		r.Post("/_echo", func(w http.ResponseWriter, r *http.Request) {
			b, prob := readBody(r)
			if prob != nil {
				writeProblem(w, prob)
				return
			}
			_, _ = fmt.Fprintf(w, "%d", len(b))
		})
		r.Put("/_write/{class}", func(w http.ResponseWriter, r *http.Request) {
			body, prob := readBody(r)
			if prob != nil {
				writeProblem(w, prob)
				return
			}
			class := WriteClass(0)
			_, _ = fmt.Sscan(chi.URLParam(r, "class"), &class)
			skip := r.URL.Query().Get("skip") == "1"
			out, _, ok := s.write(w, r, body, func(ctx context.Context, tx pgx.Tx, g *Gate) (apikey.Outcome, error) {
				if !skip {
					if err := g.Admit(ctx, class); err != nil {
						return apikey.Outcome{}, err
					}
				}
				ref := uuid.New()
				return apikey.Outcome{Status: http.StatusCreated, Ref: &ref}, nil
			})
			if ok {
				w.WriteHeader(out.Status)
			}
		})
	})
	r.h = httpx.NewRouter(&config.Config{APIHost: apiHost}, log, r.s)
	return r
}

// syncWriter serialises the log writes of concurrent requests.
type syncWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *syncWriter) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// refuseWrites is a Writer for tests that write nothing: it fails the test's request loudly.
type refuseWrites struct{}

func (refuseWrites) Run(context.Context, apikey.WriteRequest, apikey.WorkFunc) (apikey.Outcome, bool, error) {
	return apikey.Outcome{}, false, errors.New("this rig does not write")
}

// refuseLocations is a Locations for tests that write no venue.
type refuseLocations struct{}

func (refuseLocations) PutVenueTx(context.Context, pgx.Tx, tenant.VenuePut) (tenant.VenuePutResult, error) {
	return tenant.VenuePutResult{}, errors.New("this rig writes no venue")
}

func (refuseLocations) APIVenue(context.Context, uuid.UUID, uuid.UUID) (tenant.APIVenue, error) {
	return tenant.APIVenue{}, errors.New("this rig reads no venue")
}

// refuseEmployees is an Employees for tests that write no employee.
type refuseEmployees struct{}

func (refuseEmployees) PutEmployeeTx(context.Context, pgx.Tx, tenant.EmployeePut, tenant.Admit) (tenant.EmployeePutResult, error) {
	return tenant.EmployeePutResult{}, errors.New("this rig writes no employee")
}

func (refuseEmployees) DeleteEmployeeTx(context.Context, pgx.Tx, tenant.EmployeeDelete) (tenant.EmployeePutResult, error) {
	return tenant.EmployeePutResult{}, errors.New("this rig writes no employee")
}

func (refuseEmployees) ResendActivationTx(context.Context, pgx.Tx, tenant.EmployeeResend) (tenant.EmployeePutResult, error) {
	return tenant.EmployeePutResult{}, errors.New("this rig writes no employee")
}

func (refuseEmployees) APIEmployeeByRef(context.Context, uuid.UUID, string) (tenant.APIEmployee, error) {
	return tenant.APIEmployee{}, errors.New("this rig reads no employee")
}

func (refuseEmployees) APIEmployeeByID(context.Context, uuid.UUID, uuid.UUID) (tenant.APIEmployee, error) {
	return tenant.APIEmployee{}, errors.New("this rig reads no employee")
}

func (refuseEmployees) ListAPIEmployees(context.Context, uuid.UUID, *uuid.UUID, int) ([]tenant.APIEmployee, error) {
	return nil, errors.New("this rig reads no employee")
}

func (refuseEmployees) RecordDeactivationBudgetExceeded(context.Context, uuid.UUID, uuid.UUID, string, int) error {
	return errors.New("this rig writes no trail")
}

type refusePunches struct{}

func (refusePunches) PunchPage(context.Context, uuid.UUID, tenant.FeedRole, int64, int) (tenant.PunchPage, error) {
	return tenant.PunchPage{}, errors.New("this rig reads no punch")
}

func (refusePunches) HistoryPage(context.Context, uuid.UUID, tenant.FeedRole, tenant.HistoryQuery, int) (tenant.HistoryPage, error) {
	return tenant.HistoryPage{}, errors.New("this rig reads no punch")
}

type refuseDirectory struct{}

func (refuseDirectory) DirectoryPage(context.Context, uuid.UUID, tenant.DirectoryQuery, int) (tenant.DirectoryPage, error) {
	return tenant.DirectoryPage{}, errors.New("this rig reads no directory")
}

func known(tenant, key uuid.UUID) func(string) (db.ResolvedAPIKey, error) {
	now := time.Now()
	return func(string) (db.ResolvedAPIKey, error) {
		return db.ResolvedAPIKey{TenantID: tenant, KeyID: key, APIEnabledAt: &now}, nil
	}
}

func unknown(string) (db.ResolvedAPIKey, error) { return db.ResolvedAPIKey{}, pgx.ErrNoRows }

// do serves one request on the API host from addr.
func (r *rig) do(method, path, auth, addr string, body io.Reader, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://"+apiHost+path, body)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Add(hdr[i], hdr[i+1])
	}
	if addr == "" {
		addr = "192.0.2.10"
	}
	req.RemoteAddr = addr + ":40000"
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	return w
}

// problemOf decodes an error body and checks its shape: exactly code, message and details, an
// object, the code's own status, JSON, no-store.
func problemOf(t *testing.T, w *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("status %d: the body is not JSON: %q", w.Code, w.Body.String())
	}
	keys := make([]string, 0, len(raw))
	for k := range raw {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !slices.Equal(keys, []string{"code", "details", "message"}) {
		t.Fatalf("the error body's fields are %v, want code, details, message", keys)
	}
	var b errorBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil || b.Details == nil {
		t.Fatalf("the error body %q: details must be an object (%v)", w.Body.String(), err)
	}
	if StatusOf(b.Code) != w.Code {
		t.Fatalf("code %q answered with %d, its status is %d", b.Code, w.Code, StatusOf(b.Code))
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type %q", ct)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("an error response is cacheable")
	}
	return b
}

// TestApiAuth_IPBucketBeforeResolver is ADR 0028 §3's order, measured with the real chain and a
// counting resolver (every count is a database round trip in production):
//
//	no header, two headers, not Bearer, a key of the other build's prefix (tt_test_), a key one
//	character short, a non-canonical last character -- 401, ZERO lookups (no database)
//	a flood of well-shaped UNKNOWN keys from one address -- the address bucket (limit 3) lets 3
//	lookups through and answers the rest 429 with Retry-After BEFORE the resolver: 3 lookups for
//	20 requests; another address is unaffected (its own bucket)
//	a known key over the KEY bucket (limit 2), from fresh addresses each time -- 429 after 2, and
//	the throttled request is not touched (Touch comes after the key bucket)
//	K-19: only an authenticated request notices its tenant
//
// MUTANT (measured, API-4 report): the address bucket moved after the resolver -> 20 lookups.
func TestApiAuth_IPBucketBeforeResolver(t *testing.T) {
	t.Run("the shape is checked before the database", func(t *testing.T) {
		r := newRig(t, unknown)
		k := liveKey(t)
		body := k[len(apikey.LivePrefix):]
		last := body[len(body)-1]
		// 32 bytes end in 4 data bits and 2 zero bits: a last character whose alphabet index is not a
		// multiple of 4 (B, C) decodes to the same bytes only under the lax decoder.
		nonCanonical := body[:len(body)-1] + string(map[bool]byte{true: 'C', false: 'B'}[last == 'B'])
		for name, auth := range map[string][]string{
			"no header":           nil,
			"two headers":         {"Bearer " + k, "Bearer " + k},
			"not bearer":          {"Basic " + base64.StdEncoding.EncodeToString([]byte("a:b"))},
			"the sandbox prefix":  {"Bearer " + apikey.SandboxPrefix + body},
			"one character short": {"Bearer " + k[:len(k)-1]},
			"non-canonical":       {"Bearer " + apikey.LivePrefix + nonCanonical},
			"the key alone":       {k},
		} {
			req := httptest.NewRequest(http.MethodGet, "http://"+apiHost+"/v1/_whoami", nil)
			for _, a := range auth {
				req.Header.Add("Authorization", a)
			}
			w := httptest.NewRecorder()
			r.h.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized || problemOf(t, w).Code != CodeUnauthorized {
				t.Errorf("%s: %d %s, want 401 unauthorized", name, w.Code, w.Body.String())
			}
			if w.Header().Get("WWW-Authenticate") == "" {
				t.Errorf("%s: a 401 without WWW-Authenticate", name)
			}
		}
		if _, err := base64.RawURLEncoding.Strict().DecodeString(nonCanonical); err == nil {
			t.Fatal("CONTROL: the non-canonical row decodes strictly")
		}
		if n := r.resolver.count(); n != 0 {
			t.Fatalf("%d lookups for keys that are not this deployment's shape, want 0", n)
		}
	})
	t.Run("the address bucket stands before the resolver", func(t *testing.T) {
		r := newRig(t, unknown, func(c *Config) { c.AddressLimit = 3; c.AddressPeriod = time.Minute })
		var limited int
		for range 20 {
			w := r.do(http.MethodGet, "/v1/_whoami", "Bearer "+liveKey(t), "198.51.100.7", nil)
			switch w.Code {
			case http.StatusUnauthorized:
			case http.StatusTooManyRequests:
				limited++
				if problemOf(t, w).Code != CodeRateLimited || w.Header().Get("Retry-After") != "60" {
					t.Fatalf("429 without the code or Retry-After: %v %s", w.Header(), w.Body.String())
				}
			default:
				t.Fatalf("status %d", w.Code)
			}
		}
		if n := r.resolver.count(); n != 3 || limited != 17 {
			t.Fatalf("20 junk keys from one address: %d lookups and %d 429s, want 3 and 17", n, limited)
		}
		if w := r.do(http.MethodGet, "/v1/_whoami", "Bearer "+liveKey(t), "198.51.100.8", nil); w.Code != http.StatusUnauthorized {
			t.Fatalf("CONTROL: another address got %d, want its own bucket (401)", w.Code)
		}
		if len(r.work.got()) != 0 {
			t.Fatal("an unauthenticated request noticed a tenant")
		}
	})
	t.Run("the key bucket stands after it", func(t *testing.T) {
		tenant, key := uuid.New(), uuid.New()
		r := newRig(t, known(tenant, key), func(c *Config) { c.KeyLimit = 2 })
		k := liveKey(t)
		var codes []int
		for i := range 4 {
			codes = append(codes, r.do(http.MethodGet, "/v1/_whoami", "Bearer "+k, fmt.Sprintf("198.51.100.%d", 20+i), nil).Code)
		}
		if !slices.Equal(codes, []int{200, 200, 429, 429}) {
			t.Fatalf("one key from four addresses: %v, want 200 200 429 429", codes)
		}
		if n := r.touches.n.Load(); n != 2 {
			t.Fatalf("%d touches, want 2 -- a throttled request is not a use", n)
		}
		if got := r.work.got(); len(got) != 2 || got[0] != tenant {
			t.Fatalf("K-19 noticed %v, want the key's tenant twice", got)
		}
	})
}

// TestChain_ResolverAnswersAreTheContract: unknown and revoked are the SAME 401 (the body is no
// oracle for which keys once existed), a closed API is 403 api_disabled, a database failure is 503
// with the cause in the log alone.
func TestChain_ResolverAnswersAreTheContract(t *testing.T) {
	now := time.Now()
	tenant, key := uuid.New(), uuid.New()
	for _, c := range []struct {
		name   string
		answer func(string) (db.ResolvedAPIKey, error)
		status int
		code   Code
	}{
		{"unknown", unknown, 401, CodeUnauthorized},
		{"revoked", func(string) (db.ResolvedAPIKey, error) {
			return db.ResolvedAPIKey{TenantID: tenant, KeyID: key, RevokedAt: &now, APIEnabledAt: &now}, nil
		}, 401, CodeUnauthorized},
		{"closed", func(string) (db.ResolvedAPIKey, error) { return db.ResolvedAPIKey{TenantID: tenant, KeyID: key}, nil }, 403, CodeAPIDisabled},
		{"database down", func(string) (db.ResolvedAPIKey, error) {
			return db.ResolvedAPIKey{}, errors.New("FATAL: SELECT secret_relation FROM pg_stat_activity tenant 1234")
		}, 503, CodeUnavailable},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t, c.answer)
			w := r.do(http.MethodGet, "/v1/_whoami", "Bearer "+liveKey(t), "", nil)
			if w.Code != c.status || problemOf(t, w).Code != c.code {
				t.Fatalf("%d %s, want %d %s", w.Code, w.Body.String(), c.status, c.code)
			}
			if strings.Contains(w.Body.String(), "SELECT") || strings.Contains(w.Body.String(), "1234") {
				t.Fatalf("the response leaks the cause: %s", w.Body.String())
			}
		})
	}
	unk := newRig(t, unknown).do(http.MethodGet, "/v1/_whoami", "Bearer "+liveKey(t), "", nil)
	rev := newRig(t, func(string) (db.ResolvedAPIKey, error) {
		return db.ResolvedAPIKey{TenantID: tenant, KeyID: key, RevokedAt: &now, APIEnabledAt: &now}, nil
	}).do(http.MethodGet, "/v1/_whoami", "Bearer "+liveKey(t), "", nil)
	if unk.Body.String() != rev.Body.String() || unk.Header().Get("WWW-Authenticate") != rev.Header().Get("WWW-Authenticate") {
		t.Fatalf("unknown and revoked answer differently: %q vs %q", unk.Body.String(), rev.Body.String())
	}
}

// TestChain_NothingOfTheKeyReachesALogOrAResponse is the canary test (ADR 0028 §3, CLAUDE.md §7):
// a request with a known key, one with an unknown key, one the database fails, one throttled, and a
// write that fails -- through the shipped router with its access log on the surface's JSON logger.
// Neither the key, its body, its hash (the resolver's own input, captured) nor the Authorization
// header's value appears in any log record or any response; the request id is not the key; and a
// handler after the chain cannot read the header any more (it was deleted).
func TestChain_NothingOfTheKeyReachesALogOrAResponse(t *testing.T) {
	tenant, keyID := uuid.New(), uuid.New()
	var fail atomic.Bool
	now := time.Now()
	r := newRig(t, func(string) (db.ResolvedAPIKey, error) {
		if fail.Load() {
			return db.ResolvedAPIKey{}, errors.New("connection reset")
		}
		return db.ResolvedAPIKey{TenantID: tenant, KeyID: keyID, APIEnabledAt: &now}, nil
	}, func(c *Config) { c.KeyLimit = 3 })
	var seenAfterChain atomic.Value
	shipped := r.s.testRoutes
	withTestRoutes(r.s, func(rt chi.Router) {
		shipped(rt)
		rt.Get("/_header", func(w http.ResponseWriter, req *http.Request) {
			seenAfterChain.Store(req.Header.Get("Authorization"))
			w.WriteHeader(http.StatusNoContent)
		})
		rt.Put("/_fail", func(w http.ResponseWriter, req *http.Request) {
			r.s.write(w, req, nil, func(context.Context, pgx.Tx, *Gate) (apikey.Outcome, error) { return apikey.Outcome{}, nil })
		})
	})
	r.h = httpx.NewRouter(&config.Config{APIHost: apiHost}, slog.New(slog.NewJSONHandler(&syncWriter{w: r.logs}, nil)), r.s)
	k := liveKey(t)
	var responses bytes.Buffer
	keep := func(w *httptest.ResponseRecorder) {
		fmt.Fprintf(&responses, "%d %v %s\n", w.Code, w.Header(), w.Body.String())
	}
	keep(r.do(http.MethodGet, "/v1/_header", "Bearer "+k, "", nil, "X-Request-Id", k))
	keep(r.do(http.MethodPut, "/v1/_fail", "Bearer "+k, "", nil))
	for range 3 {
		keep(r.do(http.MethodGet, "/v1/_whoami", "Bearer "+k, "", nil))
	}
	keep(r.do(http.MethodGet, "/v1/_whoami", "Bearer "+liveKey(t), "", nil))
	fail.Store(true)
	keep(r.do(http.MethodGet, "/v1/_whoami", "Bearer "+liveKey(t), "", nil))
	if v, _ := seenAfterChain.Load().(string); v != "" {
		t.Fatal("a handler after the chain could still read the Authorization header")
	}
	m := hmac.New(sha256.New, r.macKey)
	m.Write([]byte("tappa/apikey/v1|" + k))
	ownHash := hex.EncodeToString(m.Sum(nil))
	canaries := []string{k, k[len(apikey.LivePrefix):], "Bearer " + k, ownHash}
	r.resolver.mu.Lock()
	canaries = append(canaries, r.resolver.hashes...)
	r.resolver.mu.Unlock()
	if !slices.Contains(canaries[4:], ownHash) {
		t.Fatal("CONTROL: the recomputed hash is not the one the resolver was asked for")
	}
	logs := r.logs.String()
	if !strings.Contains(logs, `"status":503`) || !strings.Contains(logs, "connection reset") {
		t.Fatalf("CONTROL: the log did not record the requests and the failure: %s", logs)
	}
	for _, c := range canaries {
		if strings.Contains(logs, c) {
			t.Fatalf("a log record carries the credential or its hash (%d chars)", len(c))
		}
		if strings.Contains(responses.String(), c) {
			t.Fatalf("a response carries the credential or its hash (%d chars)", len(c))
		}
	}
}

// TestApiHost_TwoWayGate is ADR 0028 §13's gate (K-9), through the shipped router with the operator
// host configured as well:
//
//	on the API host     /v1 is served; the panel, the tap, the landing page, the legal pages,
//	                    the static assets, the probes and /operator are the router's 404
//	off the API host    /v1 is the router's 404 -- the same bytes as a path nobody registered --
//	                    on the main host, on the operator host and on a sibling of the API host
//	host games          a port, capitals and one trailing dot are the API host; two dots, a
//	                    sibling and a look-alike are not; X-Forwarded-Host is never believed (real-ip's
//	                    precedent: no forwarded header names anything here); an absolute-URI request
//	                    line is classified by its own host
//	paths               /v1x, /V1, /v1%2F..., //v1 are not /v1
//
// MUTANTS (measured, API-4 report): apiHostOnly not mounted -> the panel answers on the API host;
// the surface's hostGate removed -> /v1 answers on the main host.
func TestApiHost_TwoWayGate(t *testing.T) {
	r := newRig(t, unknown)
	h := httpx.NewRouter(&config.Config{APIHost: apiHost, OperatorHost: "ops.taptime.mt"}, nil, customerStub{}, r.s)
	serve := func(raw string) *httptest.ResponseRecorder {
		req, err := http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		req.RemoteAddr = "192.0.2.1:1234"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	where := func(w *httptest.ResponseRecorder) string {
		switch {
		case w.Code == http.StatusOK && w.Body.String() == "customer":
			return "customer"
		case w.Code == http.StatusOK && w.Body.String() == "operator":
			return "operator"
		case w.Code == http.StatusOK && strings.HasPrefix(w.Body.String(), "openapi: 3.1.0"):
			return "api"
		case w.Code == http.StatusOK:
			return "static"
		case w.Code == http.StatusNotFound && w.Header().Get("Content-Type") == "application/json":
			return "api-404"
		case w.Code == http.StatusNotFound:
			return "404"
		case w.Code == http.StatusSeeOther:
			return "303"
		default:
			return http.StatusText(w.Code)
		}
	}
	const doc = "/v1/openapi.yaml"
	for _, c := range []struct{ name, raw, want string }{
		{"the document on the API host", "GET " + doc + " HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "api"},
		{"an unknown /v1 path on the API host", "GET /v1/nope HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "api-404"},
		{"/v1 itself on the API host", "GET /v1 HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "api-404"},
		{"a wrong method on the API host", "DELETE " + doc + " HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "api-404"},
		{"the panel on the API host", "GET /admin HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"under the panel on the API host", "GET /admin/login HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"the tap on the API host", "GET /t?tag=04AABBCCDDEE80 HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"a check-in on the API host", "POST /api/checkin HTTP/1.1\r\nHost: api.taptime.mt\r\nContent-Length: 0\r\n\r\n", "404"},
		{"the landing page on the API host", "GET / HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"a legal page on the API host", "GET /legal/cookies HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"a static asset on the API host", "GET /static/css/app.css HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"the probe on the API host", "GET /healthz HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"the operator on the API host", "GET /operator HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"a look-alike path on the API host", "GET /v1x HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"capital V on the API host", "GET /V1/openapi.yaml HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"an escaped slash on the API host", "GET /v1%2Fopenapi.yaml HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"a doubled slash on the API host", "GET //v1/openapi.yaml HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"the API host with a port", "GET " + doc + " HTTP/1.1\r\nHost: api.taptime.mt:443\r\n\r\n", "api"},
		{"the API host in capitals", "GET " + doc + " HTTP/1.1\r\nHost: API.TAPTIME.MT\r\n\r\n", "api"},
		{"the API host with a trailing dot", "GET " + doc + " HTTP/1.1\r\nHost: api.taptime.mt.\r\n\r\n", "api"},
		{"the panel on the API host with a port", "GET /admin HTTP/1.1\r\nHost: api.taptime.mt:8080\r\n\r\n", "404"},
		{"the document on the main host", "GET " + doc + " HTTP/1.1\r\nHost: taptime.mt\r\n\r\n", "404"},
		{"/v1 on the main host", "GET /v1 HTTP/1.1\r\nHost: taptime.mt\r\n\r\n", "404"},
		{"the document on the operator host", "GET " + doc + " HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "404"},
		{"the document on a sibling", "GET " + doc + " HTTP/1.1\r\nHost: evil.api.taptime.mt\r\n\r\n", "404"},
		{"the document on a look-alike", "GET " + doc + " HTTP/1.1\r\nHost: api.taptime.mt.evil\r\n\r\n", "404"},
		{"two trailing dots", "GET " + doc + " HTTP/1.1\r\nHost: api.taptime.mt..\r\n\r\n", "404"},
		{"no Host (HTTP/1.0)", "GET " + doc + " HTTP/1.0\r\n\r\n", "404"},
		{"a forwarded API host is not believed", "GET " + doc + " HTTP/1.1\r\nHost: taptime.mt\r\nX-Forwarded-Host: api.taptime.mt\r\n\r\n", "404"},
		{"a forwarded main host is not believed", "GET /admin HTTP/1.1\r\nHost: api.taptime.mt\r\nX-Forwarded-Host: taptime.mt\r\n\r\n", "404"},
		{"an absolute URI naming the API host", "GET http://api.taptime.mt" + doc + " HTTP/1.1\r\nHost: taptime.mt\r\n\r\n", "api"},
		{"an absolute URI naming the main host", "GET http://taptime.mt" + doc + " HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "404"},
		{"CONTROL: the panel on the main host", "GET /admin HTTP/1.1\r\nHost: taptime.mt\r\n\r\n", "customer"},
		{"CONTROL: the operator on its host", "GET /operator HTTP/1.1\r\nHost: ops.taptime.mt\r\n\r\n", "operator"},
	} {
		if got := where(serve(c.raw)); got != c.want {
			t.Errorf("%s: landed on %q, want %q", c.name, got, c.want)
		}
	}
	// Off the API host /v1 is the router's own 404, byte for byte.
	gate := serve("GET " + doc + " HTTP/1.1\r\nHost: taptime.mt\r\n\r\n")
	none := serve("GET /no-such-route HTTP/1.1\r\nHost: taptime.mt\r\n\r\n")
	if gate.Body.String() != none.Body.String() || gate.Header().Get("Content-Type") != none.Header().Get("Content-Type") {
		t.Errorf("/v1 off the API host is not the router's 404: %q vs %q", gate.Body.String(), none.Body.String())
	}
	// And no response of the API host sets a cookie (§13: no cookie, no CSRF surface).
	for _, raw := range []string{"GET " + doc + " HTTP/1.1\r\nHost: api.taptime.mt\r\n\r\n", "GET /v1/x HTTP/1.1\r\nHost: api.taptime.mt\r\nCookie: tappa_session=x\r\n\r\n"} {
		if w := serve(raw); len(w.Result().Cookies()) != 0 {
			t.Errorf("a /v1 response sets a cookie: %v", w.Result().Cookies())
		}
	}
}

// wildcard is chi's catch-all. It is spelled apart from its slash because cmd/tappa's citation
// scanner strips "/" followed by "*" as the start of a block comment.
const wildcard = "*"

// customerStub stands in for the customer routes and the operator surface (no host gate of its own).
type customerStub struct{}

func (customerStub) Mount(r chi.Router) {
	cust := func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "customer") }
	r.Get("/", cust)
	r.Get("/admin", cust)
	r.Get("/admin/"+wildcard, cust)
	r.Get("/t", cust)
	r.Post("/api/checkin", cust)
	r.Get("/legal/{slug}", cust)
	op := func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "operator") }
	r.Handle(httpx.OperatorPrefix, http.HandlerFunc(op))
	r.Handle(httpx.OperatorPrefix+"/"+wildcard, http.HandlerFunc(op))
}

// TestBody_The64KiBBound (ADR 0028 §8): exactly 65536 bytes is read; 65537 is 400 body_too_large --
// declared (refused before a byte is read), chunked (cut by MaxBytesReader) and with a Content-Length
// that lies low (the server reads what was declared, so the excess is never seen as body); a
// gzip-encoded body is refused whatever its size -- the API never inflates.
func TestBody_The64KiBBound(t *testing.T) {
	tenant, key := uuid.New(), uuid.New()
	r := newRig(t, known(tenant, key))
	k := liveKey(t)
	post := func(body io.Reader, size int64, hdr ...string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "http://"+apiHost+"/v1/_echo", body)
		req.ContentLength = size
		req.Header.Set("Authorization", "Bearer "+k)
		for i := 0; i+1 < len(hdr); i += 2 {
			req.Header.Set(hdr[i], hdr[i+1])
		}
		w := httptest.NewRecorder()
		r.h.ServeHTTP(w, req)
		return w
	}
	at := bytes.Repeat([]byte("a"), MaxBody)
	over := bytes.Repeat([]byte("a"), MaxBody+1)
	if w := post(bytes.NewReader(at), int64(len(at))); w.Code != 200 || w.Body.String() != "65536" {
		t.Fatalf("exactly 64 KiB: %d %q, want 200 65536", w.Code, w.Body.String())
	}
	if w := post(bytes.NewReader(at), -1); w.Code != 200 || w.Body.String() != "65536" {
		t.Fatalf("exactly 64 KiB, chunked: %d %q", w.Code, w.Body.String())
	}
	if w := post(bytes.NewReader(over), int64(len(over))); w.Code != 400 || problemOf(t, w).Code != CodeBodyTooLarge {
		t.Fatalf("one byte over, declared: %d %s", w.Code, w.Body.String())
	}
	if w := post(bytes.NewReader(over), -1); w.Code != 400 || problemOf(t, w).Code != CodeBodyTooLarge {
		t.Fatalf("one byte over, chunked: %d %s", w.Code, w.Body.String())
	}
	if w := post(io.MultiReader(bytes.NewReader(over), bytes.NewReader(over)), -1); w.Code != 400 {
		t.Fatalf("far over, chunked: %d", w.Code)
	}
	if w := post(bytes.NewReader([]byte{0x1f, 0x8b}), 2, "Content-Encoding", "gzip"); w.Code != 400 || problemOf(t, w).Details["header"] != "Content-Encoding" {
		t.Fatalf("a gzip body: %d %s", w.Code, w.Body.String())
	}
	if w := post(bytes.NewReader(at), int64(len(at)), "Content-Encoding", "identity"); w.Code != 200 {
		t.Fatalf("CONTROL: identity encoding: %d", w.Code)
	}
	// Through a real listener: a Content-Length that lies low leaves the excess unread as body.
	srv := httptest.NewServer(r.h)
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	raw := "POST /v1/_echo HTTP/1.1\r\nHost: " + apiHost + "\r\n" + "Authorization: " + "Bear" + "er " + k +
		"\r\nContent-Length: 10\r\nConnection: close\r\n\r\n" + string(over)
	if _, err := conn.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || string(got) != "10" {
		t.Fatalf("a low Content-Length: %d %q, want 200 \"10\"", resp.StatusCode, got)
	}
}

// TestLimit_DefaultAndTheBound (ADR 0028 §8; API-7 and API-8 read it): absent 500; 1 and 1000 read;
// 1001, 0, a sign, a blank, a fraction, a second value, an overflow -- 400 invalid_request naming
// the parameter.
func TestLimit_DefaultAndTheBound(t *testing.T) {
	for q, want := range map[string]int{"": 500, "limit=1": 1, "limit=1000": 1000, "limit=500": 500, "limit=0007": 7} {
		v, _ := url.ParseQuery(q)
		if n, p := parseLimit(v); p != nil || n != want {
			t.Errorf("%q: %d %v, want %d", q, n, p, want)
		}
	}
	for _, q := range []string{"limit=1001", "limit=0", "limit=-1", "limit=+5", "limit=", "limit=%201", "limit=1.5", "limit=1&limit=2",
		"limit=99999999999999999999", "limit=abc", "limit=10000"} {
		v, _ := url.ParseQuery(q)
		if _, p := parseLimit(v); p == nil || p.Code != CodeInvalidRequest || p.Details["parameter"] != "limit" {
			t.Errorf("%q: %v, want invalid_request on limit", q, p)
		}
	}
}

// TestErrors_TheCodeSetIsSection8: every code answers with one of §8's statuses, the body is exactly
// {code, message, details} with details an object, 429 carries Retry-After, and no fixed message
// carries a placeholder for input.
func TestErrors_TheCodeSetIsSection8(t *testing.T) {
	seen := map[Code]bool{}
	for _, c := range Codes() {
		if seen[c] {
			t.Fatalf("code %q twice", c)
		}
		seen[c] = true
		if !slices.Contains(Section8Statuses(), StatusOf(c)) {
			t.Errorf("code %q answers %d, outside §8's set", c, StatusOf(c))
		}
		w := httptest.NewRecorder()
		writeProblem(w, &Problem{Code: c, Message: "m"})
		b := problemOf(t, w)
		if b.Code != c {
			t.Errorf("code %q written as %q", c, b.Code)
		}
	}
	if StatusOf("no_such_code") != http.StatusServiceUnavailable {
		t.Error("a code outside the set is not 503")
	}
	w := httptest.NewRecorder()
	writeProblem(w, rateLimited(90*time.Second))
	if w.Header().Get("Retry-After") != "90" || problemOf(t, w).Details["retryAfterSeconds"] != float64(90) {
		t.Fatalf("429: Retry-After %q, details %v", w.Header().Get("Retry-After"), w.Body.String())
	}
	for _, p := range []*Problem{problemUnauthorized, problemAPIDisabled, problemTenantSuspended, problemNotFound, problemBodyTooLarge,
		problemEncoding, problemIdempotencyKey, problemIdempotencyReused, problemUnavailable, problemLimit} {
		if strings.ContainsAny(p.Message, "%{") {
			t.Errorf("message %q looks like a template", p.Message)
		}
	}
}

// fakeWrites runs the work with no transaction, the way the runner would, and reports what it saw.
type fakeWrites struct {
	err       error // returned instead of running the work
	replay    bool
	committed atomic.Int64
	req       apikey.WriteRequest
}

func (f *fakeWrites) Run(ctx context.Context, req apikey.WriteRequest, work apikey.WorkFunc) (apikey.Outcome, bool, error) {
	f.req = req
	if f.err != nil {
		return apikey.Outcome{}, false, f.err
	}
	if f.replay {
		ref := uuid.New()
		return apikey.Outcome{Status: 201, Ref: &ref}, true, nil
	}
	out, err := work(ctx, nil)
	if err != nil {
		return apikey.Outcome{}, false, err
	}
	f.committed.Add(1)
	return out, false, nil
}

// suspendedSwitch is a Suspension a test turns on.
type suspendedSwitch struct{ on bool }

func (s suspendedSwitch) Suspended(context.Context, pgx.Tx, uuid.UUID) (bool, error) {
	return s.on, nil
}

// TestWrite_ACommitWithoutTheCheckpointIsRefused (ADR 0028 §10, the checkpoint's mechanics -- the
// per-cell table is API-4b's, on OP-15's column): a write whose work never called Gate.Admit does
// not commit (503); while the business is suspended a closed class and the UNNAMED class are 403
// tenant_suspended and an open class passes; with no suspension every class passes.
//
// MUTANT (measured, API-4 report): write's `!g.admitted` check removed -> the skipping write commits.
func TestWrite_ACommitWithoutTheCheckpointIsRefused(t *testing.T) {
	tenant, key := uuid.New(), uuid.New()
	for _, c := range []struct {
		name      string
		suspended bool
		class     WriteClass
		skip      bool
		status    int
	}{
		{"no checkpoint", false, WriteLocationPut, true, 503},
		{"not suspended, closed class", false, WriteLocationPut, false, 201},
		{"not suspended, unnamed", false, writeUnnamed, false, 201},
		{"suspended, location put", true, WriteLocationPut, false, 403},
		{"suspended, employee change", true, WriteEmployeeChange, false, 403},
		{"suspended, employee delete", true, WriteEmployeeDelete, false, 403},
		{"suspended, unnamed", true, writeUnnamed, false, 403},
		{"suspended, a class nobody declared", true, WriteClass(200), false, 403},
		{"suspended, employee create", true, WriteEmployeeCreate, false, 201},
		{"suspended, resend activation", true, WriteResendActivation, false, 201},
	} {
		t.Run(c.name, func(t *testing.T) {
			fw := &fakeWrites{}
			r := newRig(t, known(tenant, key), func(cfg *Config) { cfg.Writes = fw; cfg.Suspension = suspendedSwitch{on: c.suspended} })
			path := fmt.Sprintf("/v1/_write/%d", c.class)
			if c.skip {
				path += "?skip=1"
			}
			w := r.do(http.MethodPut, path, "Bearer "+liveKey(t), "", strings.NewReader("{}"))
			if w.Code != c.status {
				t.Fatalf("%d %s, want %d", w.Code, w.Body.String(), c.status)
			}
			if c.status == 403 && problemOf(t, w).Code != CodeTenantSuspended {
				t.Fatalf("403 with %s", w.Body.String())
			}
			if wantCommit := int64(map[bool]int{true: 1}[c.status == 201]); fw.committed.Load() != wantCommit {
				t.Fatalf("%d commits, want %d", fw.committed.Load(), wantCommit)
			}
		})
	}
}

// TestWrite_IdempotencyKeyThroughHTTP: the header's grammar (one header of 1-255 visible ASCII; a
// second header, a blank, 256 characters and non-ASCII are 400 naming the header); the runner gets
// the key and a request hash that differs by method, path, query and body and is stable otherwise;
// the runner's mismatch is 422 idempotency_key_reused; a replay is answered with
// Idempotent-Replayed: true.
func TestWrite_IdempotencyKeyThroughHTTP(t *testing.T) {
	tenant, key := uuid.New(), uuid.New()
	fw := &fakeWrites{}
	r := newRig(t, known(tenant, key), func(c *Config) { c.Writes = fw })
	k := liveKey(t)
	for name, vals := range map[string][]string{
		"two headers": {"a", "b"}, "a blank": {"a b"}, "256 characters": {strings.Repeat("k", 256)}, "non-ASCII": {"sipariş"}, "empty": {""},
	} {
		hdr := []string{}
		for _, v := range vals {
			hdr = append(hdr, "Idempotency-Key", v)
		}
		w := r.do(http.MethodPut, "/v1/_write/1", "Bearer "+k, "", strings.NewReader("{}"), hdr...)
		if w.Code != 400 || problemOf(t, w).Details["header"] != "Idempotency-Key" {
			t.Errorf("%s: %d %s", name, w.Code, w.Body.String())
		}
	}
	hashOf := func(method, path, body string) string {
		w := r.do(method, path, "Bearer "+k, "", strings.NewReader(body), "Idempotency-Key", "order-1")
		if w.Code != 201 && w.Code != 405 && w.Code != 404 {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		if fw.req.IdempotencyKey != "order-1" {
			t.Fatalf("the runner got idempotency key %q", fw.req.IdempotencyKey)
		}
		return fw.req.RequestHash
	}
	base := hashOf(http.MethodPut, "/v1/_write/1", `{"a":1}`)
	if base != hashOf(http.MethodPut, "/v1/_write/1", `{"a":1}`) {
		t.Fatal("the same request hashed twice differently")
	}
	for name, h := range map[string]string{
		"another body":  hashOf(http.MethodPut, "/v1/_write/1", `{"a":2}`),
		"another path":  hashOf(http.MethodPut, "/v1/_write/3", `{"a":1}`),
		"another query": hashOf(http.MethodPut, "/v1/_write/1?x=1", `{"a":1}`),
	} {
		if h == base {
			t.Errorf("%s hashes like the original", name)
		}
	}
	fw.err = apikey.ErrIdempotencyMismatch
	if w := r.do(http.MethodPut, "/v1/_write/1", "Bearer "+k, "", strings.NewReader("{}"), "Idempotency-Key", "order-1"); w.Code != 422 || problemOf(t, w).Code != CodeIdempotencyKeyReused {
		t.Fatalf("a mismatch: %d %s", w.Code, w.Body.String())
	}
	fw.err, fw.replay = nil, true
	if w := r.do(http.MethodPut, "/v1/_write/1", "Bearer "+k, "", strings.NewReader("{}"), "Idempotency-Key", "order-1"); w.Code != 201 || w.Header().Get("Idempotent-Replayed") != "true" {
		t.Fatalf("a replay: %d %v", w.Code, w.Header())
	}
}

// TestOpenAPI_PathsAreTheRoutersAndTheErrorIsSection8: the embedded document's paths and methods are
// EXACTLY the /v1 routes the shipped surface registers (chi.Walk over a router it is mounted on --
// no extra, no missing); the Error schema requires code, message and details, and its code enum is
// Codes(), both ways; the document is OpenAPI 3.1 and is what GET /v1/openapi.yaml serves, without
// a key.
func TestOpenAPI_PathsAreTheRoutersAndTheErrorIsSection8(t *testing.T) {
	r := newRig(t, unknown)
	r.s.testRoutes = nil // the shipped surface's routes only
	mux := chi.NewRouter()
	r.s.Mount(mux)
	var routes []string
	if err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(strings.ReplaceAll(route, "/"+wildcard+"/", "/"), "/"+wildcard)
		if route != Prefix && route != Prefix+"/" {
			routes = append(routes, strings.ToLower(method)+" "+route)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(routes)
	doc := string(OpenAPI())
	documented := openAPIOperations(t, doc)
	if !slices.Equal(routes, documented) {
		t.Fatalf("the router's /v1 routes %v, the document's %v", routes, documented)
	}
	if len(routes) == 0 {
		t.Fatal("anti-vacuity: no route")
	}
	if !strings.HasPrefix(doc, "openapi: 3.1.0\n") {
		t.Fatal("the document is not OpenAPI 3.1")
	}
	required, enum := openAPIErrorSchema(t, doc)
	if !slices.Equal(required, []string{"code", "message", "details"}) {
		t.Errorf("the Error schema requires %v", required)
	}
	var codes []string
	for _, c := range Codes() {
		codes = append(codes, string(c))
	}
	if !slices.Equal(enum, codes) {
		t.Errorf("the Error schema's code enum %v, the surface's codes %v", enum, codes)
	}
	w := r.do(http.MethodGet, "/v1/openapi.yaml", "", "", nil)
	if w.Code != 200 || w.Body.String() != doc || w.Header().Get("Content-Type") != "application/yaml" {
		t.Fatalf("GET /v1/openapi.yaml without a key: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
}

// openAPIOperations reads "method path" pairs from the document's paths section: a path is a line of
// two spaces and "/", a method a line of four spaces and an HTTP verb.
func openAPIOperations(t *testing.T, doc string) []string {
	t.Helper()
	var out []string
	in, path := false, ""
	for _, line := range strings.Split(doc, "\n") {
		switch {
		case line == "paths:":
			in = true
			continue
		case in && line != "" && !strings.HasPrefix(line, " "):
			in = false
		}
		if !in {
			continue
		}
		if strings.HasPrefix(line, "  /") && strings.HasSuffix(line, ":") {
			path = strings.TrimSuffix(strings.TrimSpace(line), ":")
			continue
		}
		if strings.HasPrefix(line, "    ") && !strings.HasPrefix(line, "     ") {
			m := strings.TrimSuffix(strings.TrimSpace(line), ":")
			if slices.Contains([]string{"get", "put", "post", "delete", "patch", "head", "options"}, m) {
				out = append(out, m+" "+path)
			}
		}
	}
	sort.Strings(out)
	return out
}

// openAPIErrorSchema reads the Error schema's required list and its code enum (flow style, one line).
func openAPIErrorSchema(t *testing.T, doc string) (required, enum []string) {
	t.Helper()
	i := strings.Index(doc, "\n    Error:\n")
	if i < 0 {
		t.Fatal("no Error schema")
	}
	block := doc[i+1:]
	list := func(key string) []string {
		k := strings.Index(block, key+": [")
		if k < 0 {
			t.Fatalf("the Error schema has no %s list", key)
		}
		rest := block[k+len(key)+3:]
		end := strings.Index(rest, "]")
		var out []string
		for _, f := range strings.Split(rest[:end], ",") {
			out = append(out, strings.TrimSpace(f))
		}
		return out
	}
	return list("required"), list("enum")
}

// TestPackage_ImportsNoPanelAndNoPanelImportsIt (ADR 0028 §13): internal/handler/api does not depend
// on internal/handler (the panel, the tap) and internal/handler does not depend on it -- measured on
// `go list -deps`, both ways. And no non-test file of this package reads or sets a cookie.
func TestPackage_ImportsNoPanelAndNoPanelImportsIt(t *testing.T) {
	const self, panel = "github.com/atknatk/tappa/internal/handler/api", "github.com/atknatk/tappa/internal/handler"
	deps := func(pkg string) []string {
		out, err := exec.Command("go", "list", "-deps", "-f", "{{.ImportPath}}", pkg).Output()
		if err != nil {
			t.Fatalf("go list -deps %s: %v", pkg, err)
		}
		return strings.Fields(string(out))
	}
	if d := deps(self); slices.Contains(d, panel) || !slices.Contains(d, "github.com/atknatk/tappa/internal/apikey") {
		t.Errorf("internal/handler/api depends on the panel (or the list is wrong): %v", slices.Contains(d, panel))
	}
	if slices.Contains(deps(panel), self) {
		t.Error("the panel depends on internal/handler/api")
	}
	files, err := exec.Command("go", "list", "-f", `{{join .GoFiles " "}}`, self).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range strings.Fields(string(files)) {
		raw, err := os.ReadFile(f)
		src := string(raw)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{".Cookie(", ".Cookies()", "SetCookie", "http.Cookie"} {
			if strings.Contains(src, banned) {
				t.Errorf("%s uses %s: the API host carries no cookie", f, banned)
			}
		}
	}
}

// TestChain_ATouchThatFailsIs503 (API-4 review D2): when last_used_at cannot be written the request
// is 503 unavailable -- the cause in the log, not in the response -- and the handler behind the chain
// does not run; K-19 is not noticed. MUTANT (measured): Touch's error ignored -> 200.
func TestChain_ATouchThatFailsIs503(t *testing.T) {
	tenant, key := uuid.New(), uuid.New()
	r := newRig(t, known(tenant, key))
	r.touches.fail.Store(true)
	w := r.do(http.MethodGet, "/v1/_whoami", "Bearer "+liveKey(t), "", nil)
	if w.Code != http.StatusServiceUnavailable || problemOf(t, w).Code != CodeUnavailable {
		t.Fatalf("a failing Touch: %d %s, want 503 unavailable", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "connection refused") || !strings.Contains(r.logs.String(), "connection refused") {
		t.Fatal("the cause belongs in the log and not in the response")
	}
	if len(r.work.got()) != 0 {
		t.Fatal("a request that failed in the chain noticed its tenant")
	}
}

// failingSuspension cannot read the suspension.
type failingSuspension struct{}

func (failingSuspension) Suspended(context.Context, pgx.Tx, uuid.UUID) (bool, error) {
	return false, errors.New("ERROR: canceling statement due to lock timeout")
}

// TestWrite_ACheckpointThatCannotReadIsClosed (API-4 review D2): when the suspension cannot be read,
// every class -- the two open while suspended included -- is 503 and nothing commits: an unread
// suspension is never a pass. MUTANT (measured): Admit treating the read error as "not suspended"
// -> 201 and a commit.
func TestWrite_ACheckpointThatCannotReadIsClosed(t *testing.T) {
	tenant, key := uuid.New(), uuid.New()
	for _, class := range []WriteClass{WriteLocationPut, WriteEmployeeCreate, WriteResendActivation, writeUnnamed} {
		fw := &fakeWrites{}
		r := newRig(t, known(tenant, key), func(c *Config) { c.Writes = fw; c.Suspension = failingSuspension{} })
		w := r.do(http.MethodPut, fmt.Sprintf("/v1/_write/%d", class), "Bearer "+liveKey(t), "", strings.NewReader("{}"))
		if w.Code != http.StatusServiceUnavailable || problemOf(t, w).Code != CodeUnavailable || fw.committed.Load() != 0 {
			t.Fatalf("class %d with an unreadable suspension: %d %s, %d commits; want 503 and none", class, w.Code, w.Body.String(), fw.committed.Load())
		}
	}
}

// TestGuard_PanicAndDeadlineAreSection8 (API-4 review, both lenses): through the shipped router (its
// Recoverer and its middleware.Timeout outside the surface), a /v1 handler that panics answers 503
// unavailable as JSON -- not the Recoverer's bodiless 500 -- and the panic is logged with its stack
// and without the key; a handler that returns having written nothing after the request's deadline
// answers 503 unavailable -- not Timeout's bodiless 504; a handler that wrote its own status keeps it;
// http.ErrAbortHandler still aborts. MUTANT (measured): the guard not mounted -> 500 and 504.
func TestGuard_PanicAndDeadlineAreSection8(t *testing.T) {
	tenant, key := uuid.New(), uuid.New()
	r := newRig(t, known(tenant, key))
	shipped := r.s.testRoutes
	withTestRoutes(r.s, func(rt chi.Router) {
		shipped(rt)
		rt.Get("/_panic", func(http.ResponseWriter, *http.Request) { panic("boom in a handler") })
		rt.Get("/_late", func(_ http.ResponseWriter, req *http.Request) { <-req.Context().Done() })
		rt.Get("/_wrote", func(w http.ResponseWriter, req *http.Request) {
			w.WriteHeader(http.StatusTeapot)
			<-req.Context().Done()
		})
		rt.Get("/_abort", func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })
	})
	r.h = httpx.NewRouter(&config.Config{APIHost: apiHost}, slog.New(slog.NewJSONHandler(&syncWriter{w: r.logs}, nil)), r.s)
	k := liveKey(t)
	w := r.do(http.MethodGet, "/v1/_panic", "Bearer "+k, "", nil)
	if w.Code != http.StatusServiceUnavailable || problemOf(t, w).Code != CodeUnavailable {
		t.Fatalf("a panic: %d %q %q, want 503 unavailable JSON", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	logs := r.logs.String()
	if !strings.Contains(logs, "boom in a handler") || !strings.Contains(logs, "runtime/debug.Stack") {
		t.Fatalf("the panic was not logged with its stack: %s", logs)
	}
	if strings.Contains(logs, k) || strings.Contains(logs, k[len(apikey.LivePrefix):]) {
		t.Fatal("the panic's log carries the key")
	}
	late := func(path string) *httptest.ResponseRecorder {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		req := httptest.NewRequest(http.MethodGet, "http://"+apiHost+path, nil).WithContext(ctx)
		req.Header.Set("Authorization", "Bearer "+k)
		rec := httptest.NewRecorder()
		r.h.ServeHTTP(rec, req)
		return rec
	}
	if w := late("/v1/_late"); w.Code != http.StatusServiceUnavailable || problemOf(t, w).Code != CodeUnavailable {
		t.Fatalf("a missed deadline: %d %q, want 503 unavailable JSON", w.Code, w.Body.String())
	}
	if w := late("/v1/_wrote"); w.Code != http.StatusTeapot {
		t.Fatalf("CONTROL: a handler that wrote its status before the deadline: %d", w.Code)
	}
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Fatalf("ErrAbortHandler was swallowed: %v", v)
		}
	}()
	r.do(http.MethodGet, "/v1/_abort", "Bearer "+k, "", nil)
	t.Fatal("ErrAbortHandler did not abort")
}
