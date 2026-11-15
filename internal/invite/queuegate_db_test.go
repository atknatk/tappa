package invite

// queuegate_db_test.go -- the invitation queue's gate with ADR 0047 §11.3's grant arm (issue #37
// phase 3, YÜKSEK-1; §17 "Davet kuyruğu"): a business whose API Taptime never opened sends the
// invitations an app connected with employees:write asked for -- and only then.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

const queueGateRegistrar = "invite-gate-fixture@example.test"

// owner opens the owner connection (no RLS) for the fixtures no application path writes.
func (r *queueRig) owner() *pgx.Conn {
	r.t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		r.t.Skip("DATABASE_MIGRATE_URL is required")
	}
	c, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func hexHash(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}

// app registers an app (enabled unless disabled), returning its id.
func (r *queueRig) app(o *pgx.Conn, disabled bool) uuid.UUID {
	r.t.Helper()
	ctx := context.Background()
	var operator, id uuid.UUID
	if err := o.QueryRow(ctx, `
		WITH made AS (
		    INSERT INTO platform_admins (email, display_name, status)
		    SELECT $1, 'invite gate fixture', 'disabled'
		     WHERE NOT EXISTS (SELECT 1 FROM platform_admins WHERE email = $1)
		    RETURNING id)
		SELECT id FROM made UNION ALL SELECT id FROM platform_admins WHERE email = $1 LIMIT 1`, queueGateRegistrar).Scan(&operator); err != nil {
		r.t.Fatal(err)
	}
	if err := o.QueryRow(ctx, `INSERT INTO oauth_clients (name, redirect_uris, allowed_scopes, secret_hash, secret_hint, created_by)
	                           VALUES ('Gate App', '{https://gate.example/cb}', '{employees:write,timesheets:read}', $1, 'tt_cs_live_gate', $2) RETURNING id`,
		hexHash(r.t), operator).Scan(&id); err != nil {
		r.t.Fatal(err)
	}
	if disabled {
		if _, err := o.Exec(ctx, `UPDATE oauth_clients SET disabled_at = clock_timestamp() WHERE id = $1`, id); err != nil {
			r.t.Fatal(err)
		}
	}
	return id
}

// grant connects app to the rig's business with scopes and a live refresh token (live=false: none).
func (r *queueRig) grant(o *pgx.Conn, app uuid.UUID, scopes []string, live bool) uuid.UUID {
	r.t.Helper()
	return r.grantIn(o, r.tenant, app, scopes, live)
}

// grantIn is grant for any business -- the rig's, or another one (the gate is per business).
func (r *queueRig) grantIn(o *pgx.Conn, tenantID, app uuid.UUID, scopes []string, live bool) uuid.UUID {
	r.t.Helper()
	ctx := context.Background()
	var admin, id uuid.UUID
	if err := o.QueryRow(ctx, `SELECT id FROM admin_users WHERE tenant_id = $1 AND role = 'owner' LIMIT 1`, tenantID).Scan(&admin); err != nil {
		r.t.Fatal(err)
	}
	if err := o.QueryRow(ctx, `INSERT INTO oauth_grants (tenant_id, client_id, scopes, created_by) VALUES ($1, $2, $3, $4) RETURNING id`,
		tenantID, app, scopes, admin).Scan(&id); err != nil {
		r.t.Fatal(err)
	}
	if live {
		if _, err := o.Exec(ctx, `INSERT INTO oauth_tokens (tenant_id, grant_id, kind, token_hash, expires_at)
		                          VALUES ($1, $2, 'refresh', $3, now() + interval '2160 hours')`, tenantID, id, hexHash(r.t)); err != nil {
			r.t.Fatal(err)
		}
	}
	return id
}

// countingApps is the queue's AppResolver over the real one, counted.
type countingApps struct {
	mu    sync.Mutex
	n     int
	inner AppResolver
}

func (c *countingApps) GetOAuthClient(ctx context.Context, id uuid.UUID, h *string) (db.ResolvedOAuthClient, error) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
	return c.inner.GetOAuthClient(ctx, id, h)
}

func (c *countingApps) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// sendsFor runs the queue once for a fresh invited person and reports whether the invitation went.
func (r *queueRig) sendsFor(q *Queue) bool {
	r.t.Helper()
	p := r.hire(addr("gate"))
	r.dueNow(p)
	before := len(r.sink.calls())
	err := q.RunFor(context.Background(), r.tenant)
	var later *Later
	if err != nil && !errors.As(err, &later) {
		r.t.Fatal(err)
	}
	sent := len(r.sink.calls()) > before
	if !sent && (r.queued(p) != 1 || r.apiState(p) != tenant.InvitationQueued) {
		r.t.Fatalf("a closed gate dropped the request: %d queued, %q", r.queued(p), r.apiState(p))
	}
	return sent
}

// TestInviteQueue_TheGateOpensForAWritingAppsLiveGrantAlone is ADR 0047 §11.3's table, on a business
// whose API Taptime never opened (both K-22 columns NULL):
//
//	no grant                                         closed
//	another business's live employees:write grant    closed (the gate is the business's own)
//	a timesheets:read grant                          closed (the gate needs employees:write)
//	an employees:write grant of a DISABLED app       closed
//	an employees:write grant with no live refresh    closed (a dead grant -- the "Uygulama notları")
//	two apps, the first (client_id order) disabled,  OPEN -- no app is skipped
//	the second enabled
//	the operator then opens and closes the API       closed although the grant lives (§11.4: the
//	(api_code set, api_enabled_at NULL)              operator's closing outranks a grant)
//	the API open                                     open, and the grant query does not run (0 lookups)
//
// MUTANT (measured, phase 3 report): the api_code check of the gate removed -> the operator-closed
// business sends through the grant, red (K-22's precedence).
func TestInviteQueue_TheGateOpensForAWritingAppsLiveGrantAlone(t *testing.T) {
	r := newQueueRig(t)
	o := r.owner()
	ownerExecQ(t, `UPDATE tenants SET api_enabled_at = NULL, api_code = NULL WHERE id = $1`, r.tenant)
	apps := &countingApps{inner: r.d}
	q, err := NewQueue(r.m, r.sink, r.rec, apps)
	if err != nil {
		t.Fatal(err)
	}
	if r.sendsFor(q) {
		t.Fatal("no grant: the invitation was sent")
	}
	// ANOTHER business's live employees:write grant of an enabled app opens THAT business's gate, never
	// this one's (the gate reads the business's own grants: WithTenant + the explicit filter).
	other := newMailFixture(t, r.d, "Other Gate Ltd", ptrBool(true)).tenantID
	ownerExecQ(t, `UPDATE tenants SET api_enabled_at = NULL, api_code = NULL WHERE id = $1`, other)
	r.grantIn(o, other, r.app(o, false), []string{"employees:write"}, true)
	if r.sendsFor(q) {
		t.Fatal("another business's writing grant opened this business's gate")
	}
	r.grant(o, r.app(o, false), []string{"timesheets:read"}, true)
	if r.sendsFor(q) {
		t.Fatal("a timesheets:read grant opened the gate")
	}
	r.grant(o, r.app(o, true), []string{"employees:write"}, true)
	if r.sendsFor(q) {
		t.Fatal("a disabled app's grant opened the gate")
	}
	dead := r.app(o, false)
	r.grant(o, dead, []string{"employees:write"}, false)
	if r.sendsFor(q) {
		t.Fatal("a grant with no live refresh token opened the gate")
	}
	// Two writing apps, enabled and disabled, in an order where the disabled one comes first.
	a, b := r.app(o, false), r.app(o, false)
	first, second := a, b
	if b.String() < a.String() {
		first, second = b, a
	}
	if _, err := o.Exec(context.Background(), `UPDATE oauth_clients SET disabled_at = clock_timestamp() WHERE id = $1`, first); err != nil {
		t.Fatal(err)
	}
	r.grant(o, first, []string{"employees:write"}, true)
	r.grant(o, second, []string{"employees:write", "timesheets:read"}, true)
	if !r.sendsFor(q) {
		t.Fatal("an enabled writing app's live grant did not open the gate (the disabled one before it hid it)")
	}
	// The operator opens and closes the API: api_code stays -> closed whatever grant there is.
	ownerExecQ(t, `UPDATE tenants SET api_code = 'GATE', api_enabled_at = now() WHERE id = $1`, r.tenant)
	ownerExecQ(t, `UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, r.tenant)
	if r.sendsFor(q) {
		t.Fatal("the operator's closing did not outrank the grant")
	}
	// Open: the key's gate as before, and the apps are not even asked.
	ownerExecQ(t, `UPDATE tenants SET api_enabled_at = now() WHERE id = $1`, r.tenant)
	before := apps.calls()
	if !r.sendsFor(q) {
		t.Fatal("an open API did not send")
	}
	if apps.calls() != before {
		t.Fatalf("an open API asked the registry %d times, want 0", apps.calls()-before)
	}
}

// TestInviteQueue_ADisconnectKeepsTheRequestUntilTheGateOpensAgain (ADR 0047 §11.3 "Kapanışta bekleyen
// davetlerin kaderi"): the only writing grant ends -> the request is NOT deleted, it waits (queued);
// a new grant within its lifetime -> it is sent.
func TestInviteQueue_ADisconnectKeepsTheRequestUntilTheGateOpensAgain(t *testing.T) {
	r := newQueueRig(t)
	o := r.owner()
	ownerExecQ(t, `UPDATE tenants SET api_enabled_at = NULL, api_code = NULL WHERE id = $1`, r.tenant)
	app := r.app(o, false)
	g := r.grant(o, app, []string{"employees:write"}, true)
	if _, err := o.Exec(context.Background(), `UPDATE oauth_grants SET revoked_at = clock_timestamp(), revoked_reason = 'owner' WHERE id = $1`, g); err != nil {
		t.Fatal(err)
	}
	p := r.hire(addr("wait"))
	r.dueNow(p)
	var later *Later
	if err := r.q.RunFor(context.Background(), r.tenant); !errors.As(err, &later) {
		t.Fatalf("a disconnected business's run: %v, want *Later", err)
	}
	if r.queued(p) != 1 || len(r.sink.calls()) != 0 || r.apiState(p) != tenant.InvitationQueued {
		t.Fatalf("after Disconnect: %d queued, %d sends, %q", r.queued(p), len(r.sink.calls()), r.apiState(p))
	}
	r.grant(o, app, []string{"employees:write"}, true)
	r.dueNow(p)
	if err := r.q.RunFor(context.Background(), r.tenant); !settled(err) {
		t.Fatal(err)
	}
	if len(r.sink.calls()) != 1 || r.apiState(p) != tenant.InvitationSent {
		t.Fatalf("a new grant within the lifetime: %d sends, %q", len(r.sink.calls()), r.apiState(p))
	}
}
