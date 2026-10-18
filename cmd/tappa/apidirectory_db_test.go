package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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

// TestAPIWiring_TheDirectoryRunsThroughTheShippedSurface is GET /v1/directory (M11 API-8) measured
// through the surface AS THE COMMAND BUILDS IT -- openAPISurface over the real pool, the panel's key
// service behind httpx.NewRouter: a business's employee is answered on the API host to the panel's
// key as emp_<id>, with the business's company code, its ref, its names and active, and the walk ends
// on that page (nextCursor null, an asOf); the same GET on the main host is 404.
//
// MUTANT (measured, API-8 report): openAPISurface handing api.New no Directory -- New refuses, the
// surface is not built.
func TestAPIWiring_TheDirectoryRunsThroughTheShippedSurface(t *testing.T) {
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
	surface, err := openAPISurface(cfg, data, &noticed{}, venues, staff, nil, quiet)
	if err != nil || surface == nil {
		t.Fatalf("openAPISurface: %v %v", surface, err)
	}
	h := httpx.NewRouter(cfg, quiet, surface)

	biz, admin, emp := uuid.New(), uuid.New(), uuid.New()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'API-8 wiring', $2, 'restaurant', 'multi')`, []any{biz, "VAT-API8-" + biz.String()}},
		{`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status) VALUES ($1, $2, 'API-8 Owner', $3, $4, 'owner', 'active')`,
			[]any{admin, biz, "api8-" + admin.String() + "@m11.example", fixtures.UnusablePasswordHash}},
		{`UPDATE tenants SET api_code = 'APIEIGHT', api_enabled_at = now() WHERE id = $1`, []any{biz}},
		{`INSERT INTO employees (id, tenant_id, external_ref, first_name, last_name, full_name, status, invited_at)
		  VALUES ($1, $2, 'W8-EMP', 'Wiring', 'Staff', 'Wiring Staff', 'active', now())`, []any{emp, biz}},
	} {
		if _, err := owner.Exec(ctx, q.sql, q.args...); err != nil {
			t.Fatal(err)
		}
	}
	keys, err := apikey.NewKeys(apiKeyPrefix, mac, data, trail)
	if err != nil {
		t.Fatal(err)
	}
	created, err := keys.Create(ctx, apikey.CreateCommand{TenantID: biz, ActorID: admin, Name: "wiring"})
	if err != nil {
		t.Fatal(err)
	}
	raw := created.Key.RevealToTheOwnerOnce()
	get := func(host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "http://"+host+"/v1/directory", nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := get("taptime.mt"); w.Code != http.StatusNotFound {
		t.Fatalf("the GET on the main host: %d, want 404", w.Code)
	}
	w := get(apiHost)
	var page struct {
		Data []struct {
			ID          string
			Company     *string
			ExternalRef *string
			FirstName   *string
			Active      bool
		}
		NextCursor *string
		HasMore    bool
		AsOf       string
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &page) != nil {
		t.Fatalf("on the API host: %d %s", w.Code, w.Body.String())
	}
	if len(page.Data) != 1 || page.Data[0].ID != "emp_"+emp.String() || page.Data[0].Company == nil || *page.Data[0].Company != "APIEIGHT" ||
		page.Data[0].ExternalRef == nil || *page.Data[0].ExternalRef != "W8-EMP" || page.Data[0].FirstName == nil || *page.Data[0].FirstName != "Wiring" ||
		!page.Data[0].Active || page.NextCursor != nil || page.HasMore || page.AsOf == "" {
		t.Fatalf("the directory: %s", w.Body.String())
	}
}
