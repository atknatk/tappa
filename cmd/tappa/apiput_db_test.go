package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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

// noticed records K-19's notices.
type noticed struct{ tenants []uuid.UUID }

func (n *noticed) Notice(t uuid.UUID) { n.tenants = append(n.tenants, t) }

// TestAPIWiring_ThePutOfALocationRunsThroughTheShippedSurface is the first production write of /v1
// (M11 API-5) measured through the surface AS THE COMMAND BUILDS IT -- openAPISurface, with the real
// pool, the panel's key service and the panel's venue domain behind httpx.NewRouter -- rather than
// through a test's own api.New:
//
//	the chain    a key the panel issues (apikey.NewKeys, LivePrefix, the same HMAC key) is accepted
//	             on the API host; last_used_at is written; K-19 notices the key's tenant
//	the runner   with an Idempotency-Key the PUT commits ONE ledger row in the key's tenant, status
//	             201 and result_ref the venue's id, in the SAME transaction as the venue and its trail
//	             row; the retry is a replay (Idempotent-Replayed, same id, no second trail row)
//	the gate     the shipped checkpoint (api.NoSuspensionColumn) admitted the write -- a write that
//	             skipped it would not have committed (TestWrite_ACommitWithoutTheCheckpointIsRefused)
//	K-23         the trail row's actor_id is the key's id and its detail says via "api"
//	the hosts    the same PUT on the main host is 404 and writes nothing
//
// MUTANT (measured, API-5 report): the PUT's work skipping Gate.Admit -> 503, nothing committed.
func TestAPIWiring_ThePutOfALocationRunsThroughTheShippedSurface(t *testing.T) {
	appDSN, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if appDSN == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL and DATABASE_MIGRATE_URL are both required (real Postgres; the owner opens K-22's gate)")
	}
	ctx := context.Background()
	mac := make([]byte, 32)
	if _, err := rand.Read(mac); err != nil {
		t.Fatal(err)
	}
	const apiHost = "api.taptime.mt"
	cfg := &config.Config{DatabaseURL: appDSN, APIHost: apiHost, APIKeyHMACKey: mac}
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
	work := &noticed{}
	surface, err := openAPISurface(cfg, data, work, venues, quiet)
	if err != nil || surface == nil {
		t.Fatalf("openAPISurface: %v %v", surface, err)
	}
	h := httpx.NewRouter(cfg, quiet, surface)

	biz, admin := uuid.New(), uuid.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'API-5 wiring', $2, 'restaurant', 'multi')`, []any{biz, "VAT-API5-" + biz.String()}},
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status) VALUES ($1, $2, 'API-5 Owner', $3, $4, 'owner', 'active')`,
			[]any{admin, biz, "api5-" + admin.String() + "@m11.example", fixtures.UnusablePasswordHash}},
		{`UPDATE tenants SET api_code = 'APIFIVE', api_enabled_at = now() WHERE id = $1`, []any{biz}},
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

	ref, idem := "WIRE-"+uuid.NewString()[:8], "wire-"+uuid.NewString()
	put := func(host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPut, "http://"+host+"/v1/locations/"+ref, strings.NewReader(`{"name":"Birgu","active":true}`))
		req.Header.Set("Authorization", "Bearer "+raw)
		req.Header.Set("Idempotency-Key", idem)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := put("taptime.mt"); w.Code != http.StatusNotFound {
		t.Fatalf("the PUT on the main host: %d, want 404", w.Code)
	}
	first := put(apiHost)
	if first.Code != http.StatusCreated {
		t.Fatalf("the PUT on the API host: %d %s", first.Code, first.Body.String())
	}
	var body struct{ ID string }
	if err := json.Unmarshal(first.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	venue, err := uuid.Parse(strings.TrimPrefix(body.ID, "loc_"))
	if err != nil || !strings.HasPrefix(body.ID, "loc_") {
		t.Fatalf("the id %q", body.ID)
	}
	again := put(apiHost)
	if again.Code != http.StatusCreated || again.Header().Get("Idempotent-Replayed") != "true" || again.Body.String() != first.Body.String() {
		t.Fatalf("the retry: %d %v %s", again.Code, again.Header(), again.Body.String())
	}

	var status int16
	var resultRef uuid.UUID
	if err := owner.QueryRow(ctx, `SELECT status, result_ref FROM api_idempotency WHERE tenant_id = $1 AND key_id = $2 AND idem_key = $3`,
		biz, created.ID, idem).Scan(&status, &resultRef); err != nil || status != 201 || resultRef != venue {
		t.Fatalf("the ledger row: %d %s (%v), want 201 and the venue %s", status, resultRef, err, venue)
	}
	var rows, trails int
	var actor uuid.UUID
	var via string
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM locations WHERE tenant_id = $1 AND external_ref = $2`, biz, ref).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("%d venues for the ref (%v)", rows, err)
	}
	if err := owner.QueryRow(ctx, `SELECT count(*), min(actor_id::text)::uuid, min(detail->>'via') FROM audit_log WHERE tenant_id = $1 AND target = $2`,
		biz, venue.String()).Scan(&trails, &actor, &via); err != nil || trails != 1 || actor != created.ID || via != "api" {
		t.Fatalf("the trail: %d rows, actor %s, via %q (%v); want one row by the key %s via api", trails, actor, via, err, created.ID)
	}
	var used *string
	if err := owner.QueryRow(ctx, `SELECT last_used_at::text FROM api_keys WHERE id = $1`, created.ID).Scan(&used); err != nil || used == nil {
		t.Fatalf("last_used_at: %v %v", used, err)
	}
	if len(work.tenants) == 0 || work.tenants[0] != biz {
		t.Fatalf("K-19 noticed %v", work.tenants)
	}
}
