package operator_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// customer is a stand-in for the customer product's features: a panel route, a tap
// route and the landing page. The surface must change none of their answers.
type customer struct{}

func (customer) Mount(r chi.Router) {
	ok := func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("customer")) }
	r.Get("/admin", ok)
	r.Get("/t", ok)
	r.Post("/api/checkin", ok)
	r.Get("/", ok)
}

func serve(t *testing.T, surface httpx.Mounter, method, path string) *http.Response {
	t.Helper()
	srv := httptest.NewServer(httpx.NewRouter(nil, nil, customer{}, surface))
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(method, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

func body(t *testing.T, res *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// operatorPaths are the prefix itself and paths under it; customerPaths are the routes
// the stand-in serves, and one path that merely STARTS with the prefix's letters.
var (
	operatorPaths = []string{"/operator", "/operator/", "/operator/login", "/operator/login/totp", "/operator/tenants/x/y"}
	methods       = []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete}
)

// TestSurface_OffAnswers503UnderThePrefixAndNowhereElse is the OP-7 acceptance "DSN
// yoksa /operator 503, panel etkilenmez": every method on the prefix and under it is 503
// with no-store, nosniff and a body that names the state's fault; the customer routes
// answer as they did; a path that only starts with the same letters is not the
// surface's. The unavailable state (configured, database unreachable) is the same 503.
func TestSurface_OffAnswers503UnderThePrefixAndNowhereElse(t *testing.T) {
	for _, s := range []struct {
		name          string
		surface       httpx.Mounter
		says, notSays string
	}{
		{"Off()", operator.Off(), "not configured", "unavailable:"},
		{"the zero value", &operator.Surface{}, "not configured", "unavailable:"},
		{"a nil *Surface", (*operator.Surface)(nil), "not configured", "unavailable:"},
		// Configured, but its database was not reachable at start-up: the same 503, a body
		// that does not claim "not configured".
		{"Unavailable()", operator.Unavailable(), "unavailable:", "not configured"},
	} {
		t.Run(s.name, func(t *testing.T) {
			for _, p := range operatorPaths {
				for _, m := range methods {
					res := serve(t, s.surface, m, p)
					if res.StatusCode != http.StatusServiceUnavailable {
						t.Errorf("%s %s = %d, want 503", m, p, res.StatusCode)
						continue
					}
					if res.Header.Get("Cache-Control") != "no-store" || res.Header.Get("X-Content-Type-Options") != "nosniff" ||
						!strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain") {
						t.Errorf("%s %s: headers %v", m, p, res.Header)
					}
					if m != http.MethodHead {
						if b := body(t, res); !strings.Contains(b, s.says) || strings.Contains(b, s.notSays) {
							t.Errorf("%s %s: the body does not name this state's fault: %q", m, p, b)
						}
					}
				}
			}
			for _, tc := range []struct {
				method, path string
				want         int
			}{
				{http.MethodGet, "/admin", 200}, {http.MethodGet, "/t", 200}, {http.MethodPost, "/api/checkin", 200},
				{http.MethodGet, "/", 200}, {http.MethodGet, httpx.HealthPath, 200}, {http.MethodGet, "/operatorx", 404},
			} {
				if res := serve(t, s.surface, tc.method, tc.path); res.StatusCode != tc.want {
					t.Errorf("%s %s = %d with the surface off, want %d", tc.method, tc.path, res.StatusCode, tc.want)
				}
			}
		})
	}
}

// noStore satisfies operatorauth.Store, operator.LegalStore and operator.TenantStore
// without a database:
// operatorauth.New calls no method, and nothing in this file sends a request that would
// reach one.
type noStore struct{}

func (noStore) OperatorByEmail(context.Context, string) (db.OperatorAccount, error) {
	return db.OperatorAccount{}, db.ErrNoOperator
}
func (noStore) OperatorByID(context.Context, uuid.UUID) (db.OperatorAccount, error) {
	return db.OperatorAccount{}, db.ErrNoOperator
}
func (noStore) RecordOperatorAuthEvent(context.Context, db.OperatorAuthEvent, string, uuid.UUID) error {
	return nil
}
func (noStore) OpenOperatorSession(context.Context, uuid.UUID, string, int64) error {
	return db.ErrOperatorRefused
}
func (noStore) CompleteOperatorEnrollment(context.Context, uuid.UUID, string, string, []byte, int64, string) error {
	return db.ErrOperatorRefused
}
func (noStore) TouchOperatorSession(context.Context, string) (db.OperatorSession, error) {
	return db.OperatorSession{}, db.ErrOperatorRefused
}
func (noStore) CloseOperatorSession(context.Context, string) error { return db.ErrOperatorRefused }
func (noStore) LegalVersions(context.Context, string, db.LegalVersionsPage) ([]db.LegalVersion, error) {
	return nil, db.ErrOperatorRefused
}
func (noStore) PublishLegal(context.Context, string, string, string) error {
	return db.ErrOperatorRefused
}
func (noStore) TenantList(context.Context, string, db.TenantListQuery) ([]db.TenantSummary, error) {
	return nil, db.ErrOperatorRefused
}
func (noStore) TenantDetail(context.Context, string, uuid.UUID) (db.TenantOverview, error) {
	return db.TenantOverview{}, db.ErrOperatorRefused
}

// noTexts is an empty legal snapshot whose refresh does nothing.
type noTexts struct{}

func (noTexts) Published() map[string]legal.Doc { return map[string]legal.Doc{} }
func (noTexts) Refresh(context.Context) error   { return nil }

func authenticator(t *testing.T) *operatorauth.Authenticator {
	t.Helper()
	key := func() operatorauth.Key {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
		return operatorauth.NewKey(b)
	}
	a, err := operatorauth.New(noStore{}, operatorauth.Config{TOTPKEK: key(), TokenHMACKey: key(), Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// TestSurface_ConfiguredAnswersOnlyOnTheOperatorHost (OP-8; it replaces OP-7's "a
// configured surface registers nothing"): through a router that knows NO operator host
// -- the shape of the customer-side tests' routers -- the configured surface still answers its
// paths with the router's own 404 bytes (its hostGate), and the customer routes are
// unchanged. CONTROL: the same surface on the operator host serves its sign-in.
func TestSurface_ConfiguredAnswersOnlyOnTheOperatorHost(t *testing.T) {
	s, err := operator.New(authenticator(t), noStore{}, noStore{}, noTexts{}, "ops.taptime.mt", "https://taptime.mt", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if !s.Configured() {
		t.Fatal("a surface built by New does not report itself configured")
	}
	want := serve(t, s, http.MethodGet, "/definitely-not-a-route")
	wantBody := body(t, want)
	for _, p := range operatorPaths {
		res := serve(t, s, http.MethodGet, p)
		if res.StatusCode != http.StatusNotFound || body(t, res) != wantBody {
			t.Errorf("GET %s off the operator host = %d, want the router's own 404", p, res.StatusCode)
		}
	}
	if res := serve(t, s, http.MethodGet, "/admin"); res.StatusCode != 200 {
		t.Errorf("GET /admin = %d beside a configured surface", res.StatusCode)
	}
	h := httpx.NewRouter(nil, nil, customer{}, s)
	r := httptest.NewRequest(http.MethodGet, "http://ops.taptime.mt/operator/login", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("CONTROL: GET /operator/login on the operator host = %d, want 200", w.Code)
	}
}

// TestSurface_NewRefusesWhatAConfiguredSurfaceNeeds: no Authenticator, no legal store,
// no tenant store (OP-11), no legal snapshot (OP-10), no host, no logger, a base URL the
// operator origin cannot be taken from -- each refused rather than degraded to Off.
func TestSurface_NewRefusesWhatAConfiguredSurfaceNeeds(t *testing.T) {
	a, log := authenticator(t), slog.New(slog.DiscardHandler)
	for name, c := range map[string]struct {
		auth      *operatorauth.Authenticator
		store     operator.LegalStore
		tenants   operator.TenantStore
		texts     operator.LegalTexts
		host, url string
		log       *slog.Logger
	}{
		"no Authenticator":             {nil, noStore{}, noStore{}, noTexts{}, "ops.taptime.mt", "https://taptime.mt", log},
		"no legal store":               {a, nil, noStore{}, noTexts{}, "ops.taptime.mt", "https://taptime.mt", log},
		"no tenant store":              {a, noStore{}, nil, noTexts{}, "ops.taptime.mt", "https://taptime.mt", log},
		"no legal snapshot":            {a, noStore{}, noStore{}, nil, "ops.taptime.mt", "https://taptime.mt", log},
		"no host":                      {a, noStore{}, noStore{}, noTexts{}, "", "https://taptime.mt", log},
		"no logger":                    {a, noStore{}, noStore{}, noTexts{}, "ops.taptime.mt", "https://taptime.mt", nil},
		"no base URL":                  {a, noStore{}, noStore{}, noTexts{}, "ops.taptime.mt", "", log},
		"a base URL with no scheme":    {a, noStore{}, noStore{}, noTexts{}, "ops.taptime.mt", "taptime.mt", log},
		"a base URL of another scheme": {a, noStore{}, noStore{}, noTexts{}, "ops.taptime.mt", "ftp://taptime.mt", log},
	} {
		if s, err := operator.New(c.auth, c.store, c.tenants, c.texts, c.host, c.url, c.log); err == nil || s != nil {
			t.Errorf("%s: New built a configured surface", name)
		}
	}
	if s, err := operator.New(a, noStore{}, noStore{}, noTexts{}, "ops.taptime.mt", "https://taptime.mt", log); err != nil || s == nil {
		t.Fatalf("CONTROL: a complete configuration was refused: %v", err)
	}
	if operator.Off().Configured() || operator.Unavailable().Configured() || (&operator.Surface{}).Configured() ||
		(*operator.Surface)(nil).Configured() {
		t.Error("an unconfigured surface reports itself configured")
	}
}

// TestSurface_ItsDesigned503IsNotAnAlertEvent (2c, the security auditor's finding): the
// off and unavailable 503 is the surface answering as built, so through the shipped
// router and access log it writes NO http.request record -- otherwise every stranger's
// GET /operator would be an ERROR record and deploy/README.md's rule 5 (status >= 500,
// more than 5 in 5 minutes) would page on six of them. CONTROLS: a customer route on the
// same router does write its record (the log is live), and the configured surface's 404
// is recorded as usual (the exemption is the 503's, not the prefix's).
func TestSurface_ItsDesigned503IsNotAnAlertEvent(t *testing.T) {
	configured, err := operator.New(authenticator(t), noStore{}, noStore{}, noTexts{}, "ops.taptime.mt", "https://taptime.mt", slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		surface    *operator.Surface
		wantStatus int
		wantRecord bool
	}{
		{"off", operator.Off(), http.StatusServiceUnavailable, false},
		{"unavailable", operator.Unavailable(), http.StatusServiceUnavailable, false},
		{"CONTROL: configured (404)", configured, http.StatusNotFound, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
			h := httpx.NewRouter(nil, log, customer{}, tc.surface)
			for _, p := range operatorPaths {
				for _, m := range methods {
					buf.Reset()
					w := httptest.NewRecorder()
					h.ServeHTTP(w, httptest.NewRequest(m, p, nil))
					if w.Code != tc.wantStatus {
						t.Fatalf("%s %s = %d, want %d", m, p, w.Code, tc.wantStatus)
					}
					if got := accessRecords(t, &buf); (len(got) > 0) != tc.wantRecord {
						t.Fatalf("%s %s: access records %v, want a record = %v", m, p, got, tc.wantRecord)
					}
				}
			}
			buf.Reset()
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/admin", nil))
			if got := accessRecords(t, &buf); len(got) != 1 || got[0]["level"] != "INFO" {
				t.Fatalf("CONTROL: GET /admin wrote %v, want one INFO record -- the log is not live", got)
			}
		})
	}
}

// accessRecords parses buf's JSON lines and keeps the http.request records.
func accessRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("not JSON: %q", line)
		}
		if m["msg"] == httpx.EventHTTPRequest {
			out = append(out, m)
		}
	}
	return out
}
