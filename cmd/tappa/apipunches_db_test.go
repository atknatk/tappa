package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

// TestAPIWiring_ThePunchFeedRunsThroughTheShippedSurface is GET /v1/punches (M11 API-7) measured
// through the surface AS THE COMMAND BUILDS IT -- openAPISurface over the real pool, the panel's key
// service behind httpx.NewRouter -- rather than through a test's own api.New: a tap written as
// tappa_app in the business's context (the shipped trigger writes its event) is answered on the API
// host to the panel's key, in both scopes, as pch_<the record's id>, with the business's code and the
// venue's ref; nextCursor names the feed's position; the same GET on the main host is 404; K-19
// noticed the key's tenant.
//
// MUTANT (measured, API-7 report): openAPISurface handing api.New no Punches -- New refuses, the
// surface is not built.
func TestAPIWiring_ThePunchFeedRunsThroughTheShippedSurface(t *testing.T) {
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
	staff, err := tenant.NewStaff(data, trail, quiet)
	if err != nil {
		t.Fatal(err)
	}
	work := &noticed{}
	surface, err := openAPISurface(cfg, data, work, venues, staff, quiet)
	if err != nil || surface == nil {
		t.Fatalf("openAPISurface: %v %v", surface, err)
	}
	h := httpx.NewRouter(cfg, quiet, surface)

	biz, admin, emp := uuid.New(), uuid.New(), uuid.New()
	uidBytes := make([]byte, 7)
	if _, err := rand.Read(uidBytes); err != nil {
		t.Fatal(err)
	}
	uid := strings.ToUpper(hex.EncodeToString(uidBytes))
	keyRef := make([]byte, 44)
	if _, err := rand.Read(keyRef); err != nil {
		t.Fatal(err)
	}
	var venue uuid.UUID
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'API-7 wiring', $2, 'restaurant', 'multi')`, []any{biz, "VAT-API7-" + biz.String()}},
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status) VALUES ($1, $2, 'API-7 Owner', $3, $4, 'owner', 'active')`,
			[]any{admin, biz, "api7-" + admin.String() + "@m11.example", fixtures.UnusablePasswordHash}},
		{`UPDATE tenants SET api_code = 'APISEVEN', api_enabled_at = now() WHERE id = $1`, []any{biz}},
		{`INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at) VALUES ($1, $2, 'W7-EMP', 'Wiring Staff', 'active', now())`, []any{emp, biz}},
	} {
		if _, err := owner.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	if err := owner.QueryRow(ctx, `INSERT INTO locations (tenant_id, name, external_ref) VALUES ($1, 'Wiring Venue', 'W7-LOC') RETURNING id`, biz).Scan(&venue); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 1000, 'active')`,
		uid, biz, venue, keyRef); err != nil {
		t.Fatal(err)
	}
	var punch uuid.UUID
	if err := data.WithTenant(ctx, biz, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type)
		                         VALUES ($1, $2, $3, $4, now(), 'ok', 'nfc', 'in') RETURNING id`, biz, emp, venue, uid).Scan(&punch)
	}); err != nil {
		t.Fatal(err)
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
	get := func(host, scope string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/v1/punches?scope="+scope, nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := get("taptime.mt", "employer"); w.Code != http.StatusNotFound {
		t.Fatalf("the GET on the main host: %d, want 404", w.Code)
	}
	for _, scope := range []string{"employer", "location"} {
		w := get(apiHost, scope)
		if w.Code != http.StatusOK {
			t.Fatalf("scope=%s on the API host: %d %s", scope, w.Code, w.Body.String())
		}
		var page struct {
			Data []struct {
				ID       string
				Employee struct {
					ExternalRef *string
					Company     *string
				}
				Location struct{ ExternalRef *string }
				TagID    *string
			}
			NextCursor string
			HasMore    bool
		}
		if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Data) != 1 || page.Data[0].ID != "pch_"+punch.String() || page.NextCursor != "cur_1" || page.HasMore ||
			page.Data[0].Employee.Company == nil || *page.Data[0].Employee.Company != "APISEVEN" ||
			page.Data[0].Location.ExternalRef == nil || *page.Data[0].Location.ExternalRef != "W7-LOC" ||
			page.Data[0].TagID == nil || *page.Data[0].TagID != "tag_"+uid {
			t.Fatalf("scope=%s: %s", scope, w.Body.String())
		}
	}
	if len(work.tenants) == 0 || work.tenants[0] != biz {
		t.Fatalf("K-19 noticed %v", work.tenants)
	}
}
