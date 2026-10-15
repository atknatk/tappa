package apikey_test

// harness_db_test.go -- the real database behind internal/apikey's tests: tappa_app through
// db.DB (DATABASE_URL), and the owner (DATABASE_MIGRATE_URL) only for what tappa_app cannot write
// -- the operator's API gate (tenants.api_enabled_at / api_code, K-22). Fixtures are committed and
// never cleaned (audit_log is append-only); fresh random ids keep runs apart.

import (
	"context"
	"crypto/rand"
	"os"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/test/fixtures"
)

func appDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping (real Postgres required)")
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// ownerExec runs one statement as the owner -- the operator's stand-in for K-22's gate.
func ownerExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; skipping (the owner opens the API gate)")
	}
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatalf("owner connect: %v", err)
	}
	defer func() { _ = c.Close(context.Background()) }()
	if _, err := c.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("owner: %v", err)
	}
}

// business is one tenant with an active owner, a manager and a disabled owner.
type business struct {
	tenant, owner, manager, disabledOwner uuid.UUID
}

func newBusiness(t *testing.T, d *db.DB) business {
	t.Helper()
	b := business{tenant: uuid.New(), owner: uuid.New(), manager: uuid.New(), disabledOwner: uuid.New()}
	err := d.WithTenant(context.Background(), b.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
		                          VALUES ($1, 'API-3 fixture', $2, 'restaurant', 'multi')`, b.tenant, "VAT-API3-"+b.tenant.String()); e != nil {
			return e
		}
		for _, a := range []struct {
			id           uuid.UUID
			role, status string
		}{{b.owner, "owner", "active"}, {b.manager, "manager", "active"}, {b.disabledOwner, "owner", "disabled"}} {
			if _, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			                          VALUES ($1, $2, 'API-3 Admin', $3, $4, $5, $6)`,
				a.id, b.tenant, "api3-"+a.id.String()+"@m11.example", fixtures.UnusablePasswordHash, a.role, a.status); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("new business: %v", err)
	}
	return b
}

// setAPI opens (a code and a moment) or closes (NULL) the business's integration API, as the
// operator's op_* would.
func setAPI(t *testing.T, tenantID uuid.UUID, open bool) {
	t.Helper()
	if open {
		ownerExec(t, `UPDATE tenants SET api_code = 'APITHREE', api_enabled_at = now() WHERE id = $1`, tenantID)
		return
	}
	ownerExec(t, `UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, tenantID)
}

func randomMACKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

// countingResolver counts every call that reaches the database.
type countingResolver struct {
	next  apikey.Resolver
	calls atomic.Int64
}

func (c *countingResolver) GetAPIKeyByHash(ctx context.Context, h string) (db.ResolvedAPIKey, error) {
	c.calls.Add(1)
	return c.next.GetAPIKeyByHash(ctx, h)
}

// service is the panel's service and the chain's blocks over one MAC key.
type service struct {
	keys     *apikey.Keys
	auth     *apikey.Authenticator
	resolver *countingResolver
	macKey   []byte
}

func newService(t *testing.T, d *db.DB, prefix string) service {
	t.Helper()
	trail, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	k := randomMACKey(t)
	keys, err := apikey.NewKeys(prefix, k, d, trail)
	if err != nil {
		t.Fatal(err)
	}
	r := &countingResolver{next: d}
	auth, err := apikey.NewAuthenticator(prefix, k, r, d)
	if err != nil {
		t.Fatal(err)
	}
	return service{keys: keys, auth: auth, resolver: r, macKey: k}
}

// authenticate runs the chain's blocks for one Authorization header value.
func (s service) authenticate(ctx context.Context, header string) (apikey.Principal, error) {
	tok, err := apikey.ParseBearer(header)
	if err != nil {
		return apikey.Principal{}, err
	}
	c, err := s.auth.Check(tok)
	if err != nil {
		return apikey.Principal{}, err
	}
	return s.auth.Resolve(ctx, c)
}

// auditRow is one trail row of a business.
type auditRow struct {
	action, target, detail string
	actor                  *uuid.UUID
}

func auditRows(t *testing.T, d *db.DB, tenantID uuid.UUID) []auditRow {
	t.Helper()
	var out []auditRow
	err := d.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action, coalesce(target, ''), detail::text, actor_id FROM audit_log WHERE tenant_id = $1 ORDER BY at, id`, tenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r auditRow
			if err := rows.Scan(&r.action, &r.target, &r.detail, &r.actor); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("audit rows: %v", err)
	}
	return out
}

func countAction(rows []auditRow, action, target string) int {
	n := 0
	for _, r := range rows {
		if r.action == action && (target == "" || r.target == target) {
			n++
		}
	}
	return n
}
