package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
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
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/atknatk/tappa/internal/webhook"
	"github.com/atknatk/tappa/test/fixtures"
)

// capturingSender stands where the per-mailbox cap and the relay stand in production
// (handler.NewEmailInvitations' sender): it keeps the rendered messages and sends nothing.
type capturingSender struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (c *capturingSender) Send(_ context.Context, _ string, m mail.Message) (mail.Receipt, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, m)
	return mail.Receipt{MessageID: "<wiring@relay.example.test>"}, nil
}

func (c *capturingSender) Capped(string, string) bool { return false }

func (c *capturingSender) Refusing() bool { return false }

func (c *capturingSender) messages() []mail.Message {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]mail.Message(nil), c.sent...)
}

// TestAPIWiring_AnEmployeeRunsThroughTheShippedSurfaceAndTheQueue is API-6 end to end AS THE COMMAND
// BUILDS IT: openAPISurface with the panel's employee domain (tenant.NewStaff) and K-19's worker,
// whose job is the invitation queue built as run() builds it -- invite.NewQueue around the panel's
// invite.Manager and the e-mail transport's QueueSink (handler.NewEmailInvitations), with a capturing
// sender where the cap and the relay stand. A PUT on the API host with a panel-issued key is 201; the
// write tells the worker after its commit; the worker mints and renders ONE e-mail to the address on
// the row whose link resolves (invite.Manager.Lookup) to a first activation of that employee with no
// home venue (K-14); GET answers activationEmail "sent" and no address. The same PUT on the main host
// is 404.
func TestAPIWiring_AnEmployeeRunsThroughTheShippedSurfaceAndTheQueue(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres; the owner opens K-22's gate)")
	}
	ctx := context.Background()
	mac, codeKey := make([]byte, 32), make([]byte, 32)
	for _, b := range [][]byte{mac, codeKey} {
		if _, err := rand.Read(b); err != nil {
			t.Fatal(err)
		}
	}
	const apiHost = "api.taptime.mt"
	cfg := &config.Config{DatabaseURL: appDSN, APIHost: apiHost, APIKeyHMACKey: mac, InviteHMACKey: codeKey, BaseURL: "https://taptime.mt"}
	data, err := db.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(data.Close)
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	quiet := slog.New(slog.DiscardHandler)
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	venues, err := tenant.NewVenues(data, trail, quiet)
	if err != nil {
		t.Fatal(err)
	}
	staff, err := tenant.NewStaff(data, trail, quiet)
	if err != nil {
		t.Fatal(err)
	}
	invites, err := invite.New(data, cfg)
	if err != nil {
		t.Fatal(err)
	}
	sender := &capturingSender{}
	invitations, err := handler.NewEmailInvitations(sender, cfg.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	queue, err := invite.NewQueue(invites, invitations.QueueSink(quiet), trail)
	if err != nil {
		t.Fatal(err)
	}
	work, err := webhook.NewWorker(quiet)
	if err != nil {
		t.Fatal(err)
	}
	if err := work.Add(queue); err != nil {
		t.Fatal(err)
	}
	surface, err := openAPISurface(cfg, data, work, venues, staff, quiet)
	if err != nil || surface == nil {
		t.Fatalf("openAPISurface: %v %v", surface, err)
	}
	if err := work.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = work.Drain(c)
	})
	h := httpx.NewRouter(cfg, quiet, surface)

	biz, admin := uuid.New(), uuid.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure, vat_verified) VALUES ($1, 'API-6 wiring', $2, 'restaurant', 'multi', true)`, []any{biz, "VAT-API6-" + biz.String()}},
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status) VALUES ($1, $2, 'API-6 Owner', $3, $4, 'owner', 'active')`,
			[]any{admin, biz, "api6-" + admin.String() + "@m11.example", fixtures.UnusablePasswordHash}},
		{`UPDATE tenants SET api_code = 'APISIX', api_enabled_at = now() WHERE id = $1`, []any{biz}},
	} {
		if _, err := owner.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := apikey.NewKeys(apikey.LivePrefix, mac, data, trail)
	if err != nil {
		t.Fatal(err)
	}
	created, err := keys.Create(ctx, apikey.CreateCommand{TenantID: biz, ActorID: admin, Name: "wiring"})
	if err != nil {
		t.Fatal(err)
	}
	raw := created.Key.RevealToTheOwnerOnce()
	ref, email := "WIRE-"+uuid.NewString()[:8], "wire."+strings.ToUpper(uuid.NewString()[:8])+"@api6.example.test"
	do := func(method, host, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "http://"+host+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+raw)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	body := `{"firstName":"Maria","lastName":"Borg","email":"` + email + `","active":true}`
	if w := do(http.MethodPut, "taptime.mt", "/v1/employees/"+ref, body); w.Code != http.StatusNotFound {
		t.Fatalf("the PUT on the main host: %d, want 404", w.Code)
	}
	w := do(http.MethodPut, apiHost, "/v1/employees/"+ref, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("the PUT on the API host: %d %s", w.Code, w.Body.String())
	}
	var got struct {
		ID              string
		ActivationEmail struct{ Status string }
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	id, err := uuid.Parse(strings.TrimPrefix(got.ID, "emp_"))
	if err != nil {
		t.Fatalf("the id %q", got.ID)
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(sender.messages()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the worker never sent the queued invitation (was it noticed after the commit?)")
		}
		time.Sleep(20 * time.Millisecond)
	}
	msgs := sender.messages()
	if len(msgs) != 1 || msgs[0].To != email {
		t.Fatalf("%d messages; want one, to the address on the row", len(msgs))
	}
	link := regexp.MustCompile(`https://taptime\.mt/activate\?code=[A-Za-z0-9_-]+`).FindString(msgs[0].Text)
	if link == "" {
		t.Fatal("the rendered e-mail carries no activation link")
	}
	lc, err := invites.Lookup(ctx, invite.ParseCode(link[strings.Index(link, "code=")+len("code="):]))
	if err != nil || lc.EmployeeID != id || !lc.FirstActivation() || lc.LocationID != uuid.Nil {
		t.Fatalf("the link resolves to %+v %v; want a first activation of the employee, no home venue", lc, err)
	}
	w = do(http.MethodGet, apiHost, "/v1/employees/"+ref, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"sent"`) || strings.Contains(strings.ToLower(w.Body.String()), strings.ToLower(email)) {
		t.Fatalf("GET after the send: %d %s", w.Code, w.Body.String())
	}
}
