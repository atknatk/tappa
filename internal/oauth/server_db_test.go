package oauth

// server_db_test.go -- the authorization server's database work (ADR 0047 §4.5, §5.2, §5.3, §6) against
// REAL Postgres: the app pool as tappa_app (RLS on), the owner connection only to arrange what no
// application path can (an app in the registry, a backdated row, a suspended or operator-closed
// business) and to read every row the test asserts on. Fixtures are committed with fresh random ids
// and left behind (inert: a random-secret app authenticates nobody), like internal/db's OAuth tests.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
)

const serverDBOperatorEmail = "oauth-server-fixture@example.test"

type serverWorld struct {
	t       *testing.T
	data    *db.DB
	owner   *pgx.Conn
	srv     *Server
	logs    *bytes.Buffer
	tenant  uuid.UUID
	admin   uuid.UUID
	manager uuid.UUID
	client  Client
	secret  Value
	other   Client // a second registered app of another registrar
}

func newServerWorld(t *testing.T) *serverWorld {
	t.Helper()
	dsn, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL / DATABASE_MIGRATE_URL not set; skipping the authorization server's database tests")
	}
	ctx := context.Background()
	data, err := db.New(ctx, &config.Config{DatabaseURL: config.NewDSN(dsn)})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(data.Close)
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	trail, err := audit.New(data)
	if err != nil {
		t.Fatal(err)
	}
	w := &serverWorld{t: t, data: data, owner: owner, logs: &bytes.Buffer{}, tenant: uuid.New(), admin: uuid.New(), manager: uuid.New()}
	if w.srv, err = NewServer(testKey, data, trail, slog.New(slog.NewTextHandler(w.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))); err != nil {
		t.Fatal(err)
	}
	if err := data.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
		                           VALUES ($1, 'OAuth Server Fixture Ltd', $2, 'restaurant', 'multi')`, w.tenant, "VAT-OAS-"+w.tenant.String()); err != nil {
			return err
		}
		for _, a := range []struct {
			id   uuid.UUID
			role string
		}{{w.admin, "owner"}, {w.manager, "manager"}} {
			if _, err := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			                           VALUES ($1, $2, 'OAuth Admin', $3, $4, $5, 'active')`,
				a.id, w.tenant, "oauth-srv-"+a.id.String()+"@m11.example", fixtures.UnusablePasswordHash, a.role); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("the business: %v", err)
	}
	w.client, w.secret = w.registerApp("OAuth Server Fixture App", []string{"https://app.example/cb"}, []string{ScopeEmployeesRead, ScopeEmployeesWrite, ScopeTimesheetsRead})
	w.other, _ = w.registerApp("Another App", []string{"https://other.example/cb"}, []string{ScopeEmployeesRead, ScopeEmployeesWrite, ScopeTimesheetsRead})
	return w
}

// registerApp writes a registry row as the owner, with a secret this test knows.
func (w *serverWorld) registerApp(name string, redirects, scopes []string) (Client, Value) {
	w.t.Helper()
	ctx := context.Background()
	var operator uuid.UUID
	if err := w.owner.QueryRow(ctx, `
		WITH made AS (
		    INSERT INTO platform_admins (email, display_name, status)
		    SELECT $1, 'oauth server fixture', 'disabled'
		     WHERE NOT EXISTS (SELECT 1 FROM platform_admins WHERE email = $1)
		    RETURNING id)
		SELECT id FROM made UNION ALL SELECT id FROM platform_admins WHERE email = $1 LIMIT 1`, serverDBOperatorEmail).Scan(&operator); err != nil {
		w.t.Fatalf("the registrar: %v", err)
	}
	secret, err := Mint(KindSecret)
	if err != nil {
		w.t.Fatal(err)
	}
	h, _ := w.srv.hasher.Hash(KindSecret, secret)
	var id uuid.UUID
	if err := w.owner.QueryRow(ctx, `INSERT INTO oauth_clients (name, redirect_uris, allowed_scopes, secret_hash, secret_hint, created_by)
	                                 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		name, redirects, Canonical(scopes), h.reveal(), secret.SecretHint(), operator).Scan(&id); err != nil {
		w.t.Fatalf("the app: %v", err)
	}
	c, err := w.srv.AuthenticateClient(ctx, id, secret)
	if err != nil {
		w.t.Fatalf("the fixture app does not authenticate: %v", err)
	}
	return c, secret
}

// exec runs a statement as the owner.
func (w *serverWorld) exec(sql string, args ...any) {
	w.t.Helper()
	if _, err := w.owner.Exec(context.Background(), sql, args...); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
}

// count reads one count as the owner.
func (w *serverWorld) count(sql string, args ...any) int64 {
	w.t.Helper()
	var n int64
	if err := w.owner.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		w.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// request is a valid pending request for the fixture app with a fresh verifier.
func (w *serverWorld) request(scopes ...string) (Request, string) {
	if len(scopes) == 0 {
		scopes = []string{ScopeEmployeesWrite, ScopeTimesheetsRead}
	}
	v, ch := pkcePair(strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", ""))
	return Request{ClientID: w.client.ID, RedirectURI: w.client.RedirectURIs[0], ResponseType: "code", Method: "S256",
		Challenge: ch, Scopes: Canonical(scopes), State: "st"}, v
}

// approve is the owner's consent: a fresh code and its verifier.
func (w *serverWorld) approve(scopes ...string) (Value, string) {
	w.t.Helper()
	r, v := w.request(scopes...)
	code, err := w.srv.Approve(context.Background(), w.tenant, w.admin, r)
	if err != nil {
		w.t.Fatalf("approve: %v", err)
	}
	return code, v
}

// connect is approve + exchange: a live grant's first pair.
func (w *serverWorld) connect(scopes ...string) Tokens {
	w.t.Helper()
	code, v := w.approve(scopes...)
	tok, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, v, nil)
	if err != nil {
		w.t.Fatalf("exchange: %v", err)
	}
	return tok
}

// resolve is the /v1 resolver's view of a token.
func (w *serverWorld) resolve(k Kind, v Value) db.ResolvedOAuthToken {
	w.t.Helper()
	h, _ := w.srv.hasher.Hash(k, v)
	r, err := w.data.GetOAuthTokenByHash(context.Background(), h.reveal())
	if err != nil {
		w.t.Fatalf("resolve: %v", err)
	}
	return r
}

// liveGrants counts the business's live grants to the fixture app.
func (w *serverWorld) liveGrants() int64 {
	return w.count(`SELECT count(*) FROM oauth_grants WHERE tenant_id = $1 AND client_id = $2 AND revoked_at IS NULL`, w.tenant, w.client.ID)
}

// liveTokens counts a grant's live refresh and access tokens.
func (w *serverWorld) liveTokens(grant uuid.UUID) (refresh, access int64) {
	refresh = w.count(`SELECT count(*) FROM oauth_tokens WHERE grant_id = $1 AND kind = 'refresh' AND used_at IS NULL
	                   AND revoked_at IS NULL AND expires_at > clock_timestamp()`, grant)
	access = w.count(`SELECT count(*) FROM oauth_tokens WHERE grant_id = $1 AND kind = 'access'
	                  AND revoked_at IS NULL AND expires_at > clock_timestamp()`, grant)
	return refresh, access
}

// trail is the business's audit rows of one action, as text.
func (w *serverWorld) trail(action string) []string {
	w.t.Helper()
	rows, err := w.owner.Query(context.Background(), `SELECT target || ' ' || detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2`, w.tenant, action)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			w.t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func is(err, want error) bool { return errors.Is(err, want) }

// TestOAuthServerDB_TheHappyPathAndNothingOfItLeaks: consent -> code -> exchange -> a grant with the
// code's scopes, a pair whose access token carries them and whose refresh token carries none,
// expires_in from the row (3599..3600), the code bound to its grant, the grant_created row naming the
// approver -- and no value, verifier or hash in the trail or the log.
func TestOAuthServerDB_TheHappyPathAndNothingOfItLeaks(t *testing.T) {
	w := newServerWorld(t)
	code, verifier := w.approve()
	tok, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, verifier, nil)
	if err != nil {
		t.Fatal(err)
	}
	if tok.ExpiresIn < 3590 || tok.ExpiresIn > 3600 || ScopeString(tok.Scopes) != "employees:write timesheets:read" {
		t.Fatalf("expires_in %d, scope %q", tok.ExpiresIn, ScopeString(tok.Scopes))
	}
	at, rt := w.resolve(KindAccess, tok.Access), w.resolve(KindRefresh, tok.Refresh)
	if at.Kind != "access" || ScopeString(at.TokenScopes) != "employees:write timesheets:read" || rt.Kind != "refresh" || rt.TokenScopes != nil ||
		at.GrantID != rt.GrantID || at.TenantID != w.tenant || at.ClientID != w.client.ID {
		t.Fatalf("the pair: %+v / %+v", at, rt)
	}
	if n := w.count(`SELECT count(*) FROM oauth_codes WHERE tenant_id = $1 AND grant_id = $2 AND consumed_at IS NOT NULL`, w.tenant, at.GrantID); n != 1 {
		t.Fatalf("%d codes bound to the grant", n)
	}
	rows := w.trail(ActionGrantCreated)
	if len(rows) != 1 || !strings.Contains(rows[0], at.GrantID.String()) || !strings.Contains(rows[0], ClientIDOf(w.client.ID)) {
		t.Fatalf("grant_created rows: %v", rows)
	}
	if n := w.count(`SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = $2 AND actor_id = $3`, w.tenant, ActionGrantCreated, w.admin); n != 1 {
		t.Fatal("the grant_created row does not name the approver")
	}
	all := strings.Join(w.trail(ActionGrantCreated), "\n") + w.logs.String()
	for _, v := range []Value{code, tok.Access, tok.Refresh, w.secret} {
		if strings.Contains(all, v.reveal()) {
			t.Fatal("a value reached the trail or the log")
		}
	}
	if strings.Contains(all, verifier) {
		t.Fatal("the verifier reached the trail or the log")
	}
}

// TestOAuthServerDB_PKCEMismatchSpendsTheCode (ADR 0047 §5.2 step 3, §17): a wrong verifier is
// invalid_grant AND spends the code -- the right verifier afterwards is invalid_grant too; no grant.
func TestOAuthServerDB_PKCEMismatchSpendsTheCode(t *testing.T) {
	w := newServerWorld(t)
	code, verifier := w.approve()
	if _, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, strings.Repeat("z", 43), nil); !is(err, ErrInvalidGrant) {
		t.Fatalf("the wrong verifier: %v", err)
	}
	if _, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, verifier, nil); !is(err, ErrInvalidGrant) {
		t.Fatalf("the right verifier after a miss: %v", err)
	}
	if w.liveGrants() != 0 {
		t.Fatal("a grant was born")
	}
}

// TestOAuthServerDB_TheRedirectURIIsOptionalButExact (§5.2, ORTA-3 (a)): absent -> accepted; sent and
// differing by one byte -> invalid_grant (and the code is spent).
func TestOAuthServerDB_TheRedirectURIIsOptionalButExact(t *testing.T) {
	w := newServerWorld(t)
	code, v := w.approve()
	wrong := w.client.RedirectURIs[0] + "/"
	if _, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, v, &wrong); !is(err, ErrInvalidGrant) {
		t.Fatalf("a differing redirect_uri: %v", err)
	}
	code, v = w.approve()
	same := w.client.RedirectURIs[0]
	if _, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, v, &same); err != nil {
		t.Fatalf("the same redirect_uri: %v", err)
	}
}

// TestOAuthServerDB_CodeReuseRevokesTheGrantButOnlyForItsOwnClient: after a successful exchange,
// the same client presenting the code again is invalid_grant and the grant ends (code_reuse, with its
// trail row) -- RFC 6749 §4.1.2. ANOTHER registered client presenting the same code, before or after
// the exchange, is invalid_grant and touches nothing: the code is not spent, the grant lives (the
// 1st phase's security finding).
func TestOAuthServerDB_CodeReuseRevokesTheGrantButOnlyForItsOwnClient(t *testing.T) {
	w := newServerWorld(t)
	code, v := w.approve()
	// Before the exchange: the other app cannot spend it.
	if _, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.other, code, v, nil); !is(err, ErrInvalidGrant) {
		t.Fatalf("another app's exchange: %v", err)
	}
	if n := w.count(`SELECT count(*) FROM oauth_codes WHERE tenant_id = $1 AND consumed_at IS NOT NULL`, w.tenant); n != 0 {
		t.Fatal("another app's presentation spent the code")
	}
	tok, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, v, nil)
	if err != nil {
		t.Fatalf("the owner app's exchange after the other's attempt: %v", err)
	}
	grant := w.resolve(KindAccess, tok.Access).GrantID
	// After: the other app replaying it revokes nothing.
	if _, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.other, code, v, nil); !is(err, ErrInvalidGrant) {
		t.Fatalf("another app's replay: %v", err)
	}
	if w.liveGrants() != 1 || len(w.trail(ActionCodeReuseDetected)) != 0 {
		t.Fatal("another app's replay of a leaked code ended the grant")
	}
	// The same app replaying it: the grant ends.
	if _, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, v, nil); !is(err, ErrInvalidGrant) {
		t.Fatalf("the replay: %v", err)
	}
	if w.liveGrants() != 0 {
		t.Fatal("the replayed code's grant is still live")
	}
	if rows := w.trail(ActionCodeReuseDetected); len(rows) != 1 || !strings.Contains(rows[0], grant.String()) || !strings.Contains(rows[0], `"code_reuse"`) {
		t.Fatalf("code_reuse rows: %v", rows)
	}
}

// TestOAuthServerDB_ConcurrentPresentationsOfOneCodeAreNotAReuse (the "Uygulama notları", -race): N
// goroutines exchange one code while the winner holds the row lock -- exactly one succeeds, the
// others are invalid_grant without touching anything, and the grant lives.
//
// DETERMINISTIC, NOT TIMED (phase 4's make-check round). The winner's hold (HoldTheConsumedCode, inside
// its transaction right after the consuming UPDATE) lasts until every other presentation has RETURNED
// -- so each of them met the row locked (SKIP LOCKED: refused, not a reuse), whatever the scheduler
// did; a 400 ms sleep let a late loser arrive after the commit, see a spent code that bore a grant and
// revoke it. And the code is checked FRESH before the race: a code's 60 seconds start at its
// transaction's now(), and the full suite's second make-check run answered "0 succeeded and 8 were
// refused" after 72 s (measured) -- every presentation found the code expired, which only a commit
// that waited ~60 s explains (inferred, not measured: internal/db's exclusive lock of admin_users,
// which the code's FK check meets). A stale code is minted again (at most three times), never raced.
func TestOAuthServerDB_ConcurrentPresentationsOfOneCodeAreNotAReuse(t *testing.T) {
	w := newServerWorld(t)
	var code Value
	var v string
	for attempt := 1; ; attempt++ {
		code, v = w.approve()
		left := w.count(`SELECT floor(extract(epoch FROM expires_at - clock_timestamp()))::bigint FROM oauth_codes
		                 WHERE tenant_id = $1 AND consumed_at IS NULL ORDER BY created_at DESC LIMIT 1`, w.tenant)
		if left >= 40 {
			break
		}
		if attempt == 3 {
			t.Fatalf("three codes in a row were born with under 40 of their 60 seconds left (%d): the database is stalled", left)
		}
	}
	const n = 8
	var started, losers sync.WaitGroup
	started.Add(n)
	losers.Add(n - 1)
	release := make(chan struct{})
	go func() { losers.Wait(); close(release) }()
	w.srv.HoldTheConsumedCode(func() {
		select {
		case <-release:
		case <-time.After(30 * time.Second):
			t.Error("the other presentations did not return within 30 s while the winner held the code")
		}
	})
	var ok, refused atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			started.Done()
			started.Wait()
			_, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, v, nil)
			switch {
			case err == nil:
				ok.Add(1)
			case is(err, ErrInvalidGrant):
				refused.Add(1)
				losers.Done()
			default:
				t.Errorf("exchange: %v", err)
				losers.Done()
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 || refused.Load() != n-1 {
		t.Fatalf("%d succeeded and %d were refused, want 1 and %d", ok.Load(), refused.Load(), n-1)
	}
	if w.liveGrants() != 1 || len(w.trail(ActionCodeReuseDetected)) != 0 {
		t.Fatal("a concurrent presentation was taken for a reuse")
	}
}

// TestOAuthServerDB_AnExpiredCodeIsInvalidGrant: a code past its 60 seconds (written backdated by the
// owner) is invalid_grant and bears nothing.
func TestOAuthServerDB_AnExpiredCodeIsInvalidGrant(t *testing.T) {
	w := newServerWorld(t)
	r, v := w.request()
	code, _ := Mint(KindCode)
	h, _ := w.srv.hasher.Hash(KindCode, code)
	w.exec(`INSERT INTO oauth_codes (tenant_id, code_hash, client_id, created_by, redirect_uri, scopes, code_challenge, created_at, expires_at)
	        VALUES ($1, $2, $3, $4, $5, $6, $7, now() - interval '61 seconds', now() - interval '1 second')`,
		w.tenant, h.reveal(), w.client.ID, w.admin, r.RedirectURI, r.Scopes, r.Challenge)
	if _, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, v, nil); !is(err, ErrInvalidGrant) {
		t.Fatalf("an expired code: %v", err)
	}
	if w.liveGrants() != 0 {
		t.Fatal("an expired code bore a grant")
	}
}

// demoteApprover makes the world's owner a manager through the product's own membership statement,
// in the business's context.
func (w *serverWorld) demoteApprover() {
	w.t.Helper()
	if err := w.data.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, err := store.New(tx).SetAdminMemberRole(ctx, store.SetAdminMemberRoleParams{Role: "manager", TenantID: w.tenant, ID: w.admin})
		return err
	}); err != nil {
		w.t.Fatalf("demote the approver: %v", err)
	}
}

// TestOAuthServerDB_TheExchangeRereadsTheBusinessAndTheApprover (§5.2 step 6, §11.2, §11.4): a code
// whose business was suspended, whose API the operator closed, or whose approver is no longer an
// active owner -- between consent and exchange -- is invalid_grant.
func TestOAuthServerDB_TheExchangeRereadsTheBusinessAndTheApprover(t *testing.T) {
	cases := []struct {
		name         string
		arrange, end string
	}{
		{"suspended", `UPDATE tenants SET suspended_at = now(), suspension_reason = 'non_payment' WHERE id = $1`,
			`UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL WHERE id = $1`},
		{"operator-closed", `UPDATE tenants SET api_code = 'OAX', api_enabled_at = NULL WHERE id = $1`, ""},
		// The demotion is the panel's own statement (SetAdminMemberRole, tappa_app's column grant), not a
		// raw write: cmd/opadmin's TestTablesLock_AdminRowsAreWrittenUnderTheHolder allows this directory
		// plain INSERTs of fresh admin rows alone.
		{"approver demoted", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newServerWorld(t)
			code, v := w.approve()
			if c.arrange == "" {
				w.demoteApprover()
			} else {
				w.exec(c.arrange, w.tenant)
			}
			if _, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, v, nil); !is(err, ErrInvalidGrant) {
				t.Fatalf("got %v, want invalid_grant", err)
			}
			if w.liveGrants() != 0 {
				t.Fatal("a grant was born")
			}
			if c.end != "" {
				w.exec(c.end, w.tenant)
			}
		})
	}
}

// TestOAuthServerDB_ApproveIsTheActiveOwnersAndRefusesASuspendedOrClosedBusiness (§4.4, §11.2,
// §11.4): a manager's consent, a suspended business's and an operator-closed business's are refused
// and write no code.
func TestOAuthServerDB_ApproveIsTheActiveOwnersAndRefusesASuspendedOrClosedBusiness(t *testing.T) {
	w := newServerWorld(t)
	r, _ := w.request()
	if _, err := w.srv.Approve(context.Background(), w.tenant, w.manager, r); !is(err, ErrNotOwner) {
		t.Fatalf("a manager: %v", err)
	}
	w.exec(`UPDATE tenants SET suspended_at = now(), suspension_reason = 'non_payment' WHERE id = $1`, w.tenant)
	if _, err := w.srv.Approve(context.Background(), w.tenant, w.admin, r); !is(err, ErrSuspended) {
		t.Fatalf("suspended: %v", err)
	}
	w.exec(`UPDATE tenants SET suspended_at = NULL, suspension_reason = NULL, api_code = 'OAY', api_enabled_at = NULL WHERE id = $1`, w.tenant)
	if _, err := w.srv.Approve(context.Background(), w.tenant, w.admin, r); !is(err, ErrIntegrationsOff) {
		t.Fatalf("operator-closed: %v", err)
	}
	if n := w.count(`SELECT count(*) FROM oauth_codes WHERE tenant_id = $1`, w.tenant); n != 0 {
		t.Fatalf("%d codes written", n)
	}
	// Opened again (api_enabled_at set): consent works.
	w.exec(`UPDATE tenants SET api_enabled_at = now() WHERE id = $1`, w.tenant)
	if _, err := w.srv.Approve(context.Background(), w.tenant, w.admin, r); err != nil {
		t.Fatalf("reopened: %v", err)
	}
}

// TestOAuthServerDB_ASecondConsentSupersedesTheFirst (O-8, §5.2 step 7): one live grant per (business,
// app); the old grant is 'superseded' and its tokens stop resolving as live.
func TestOAuthServerDB_ASecondConsentSupersedesTheFirst(t *testing.T) {
	w := newServerWorld(t)
	first := w.connect()
	second := w.connect()
	if w.liveGrants() != 1 {
		t.Fatalf("%d live grants", w.liveGrants())
	}
	old := w.resolve(KindAccess, first.Access)
	if old.GrantRevokedAt == nil || w.resolve(KindAccess, second.Access).GrantRevokedAt != nil {
		t.Fatal("the first grant is not the superseded one")
	}
	if n := w.count(`SELECT count(*) FROM oauth_grants WHERE id = $1 AND revoked_reason = 'superseded'`, old.GrantID); n != 1 {
		t.Fatal("the old grant's reason is not superseded")
	}
}

// TestOAuthServerDB_RefreshRotatesToleratesAndThenRevokes (§5.3, OA-5 (b)): R0 rotates to R1; R0 again
// within 30 s is a new pair with every other live token revoked and R0 itself left used and
// unrevoked; a third presentation in the window is a new pair too; a used token whose first use is
// past 30 s ends the grant (refresh_reuse, its trail row) -- invalid_grant; a revoked refresh token is
// invalid_grant with the grant alive.
func TestOAuthServerDB_RefreshRotatesToleratesAndThenRevokes(t *testing.T) {
	w := newServerWorld(t)
	t0 := w.connect()
	grant := w.resolve(KindRefresh, t0.Refresh).GrantID
	ctx := context.Background()
	t1, err := w.srv.Refresh(ctx, ctx, w.client, t0.Refresh, nil)
	if err != nil {
		t.Fatalf("rotation: %v", err)
	}
	if ScopeString(t1.Scopes) != "employees:write timesheets:read" {
		t.Fatalf("the rotation's scope: %q", ScopeString(t1.Scopes))
	}
	if w.resolve(KindAccess, t0.Access).TokenRevokedAt != nil {
		t.Fatal("a normal rotation revoked the old access token (it lives out its hour)")
	}
	t2, err := w.srv.Refresh(ctx, ctx, w.client, t0.Refresh, nil)
	if err != nil {
		t.Fatalf("the tolerance: %v", err)
	}
	if w.resolve(KindRefresh, t1.Refresh).TokenRevokedAt == nil || w.resolve(KindAccess, t1.Access).TokenRevokedAt == nil ||
		w.resolve(KindAccess, t0.Access).TokenRevokedAt == nil {
		t.Fatal("the tolerance left another live token")
	}
	r0 := w.resolve(KindRefresh, t0.Refresh)
	if r0.UsedAt == nil || r0.TokenRevokedAt != nil {
		t.Fatal("the tolerance touched R0")
	}
	if _, err := w.srv.Refresh(ctx, ctx, w.client, t0.Refresh, nil); err != nil {
		t.Fatalf("a third presentation within the window: %v", err)
	}
	if rf, ac := w.liveTokens(grant); rf != 1 || ac != 1 {
		t.Fatalf("%d live refresh and %d live access tokens, want 1 and 1", rf, ac)
	}
	// A revoked refresh token (T2's, revoked by the third presentation): invalid_grant, grant alive.
	if _, err := w.srv.Refresh(ctx, ctx, w.client, t2.Refresh, nil); !is(err, ErrInvalidGrant) {
		t.Fatalf("a revoked refresh token: %v", err)
	}
	if w.liveGrants() != 1 {
		t.Fatal("a revoked refresh token ended the grant")
	}
	// Past the window: a used refresh token whose first use was 31 seconds ago (written by the owner).
	aged, _ := Mint(KindRefresh)
	h, _ := w.srv.hasher.Hash(KindRefresh, aged)
	w.exec(`INSERT INTO oauth_tokens (tenant_id, grant_id, kind, token_hash, created_at, expires_at, used_at)
	        VALUES ($1, $2, 'refresh', $3, now() - interval '1 hour', now() - interval '1 hour' + interval '2160 hours', clock_timestamp() - interval '31 seconds')`,
		w.tenant, grant, h.reveal())
	if _, err := w.srv.Refresh(ctx, ctx, w.client, aged, nil); !is(err, ErrInvalidGrant) {
		t.Fatalf("past the window: %v", err)
	}
	if w.liveGrants() != 0 {
		t.Fatal("the reuse past the window did not end the grant")
	}
	if rows := w.trail(ActionRefreshReuse); len(rows) != 1 || !strings.Contains(rows[0], grant.String()) {
		t.Fatalf("refresh_reuse rows: %v", rows)
	}
	if _, err := w.srv.Refresh(ctx, ctx, w.client, t0.Refresh, nil); !is(err, ErrInvalidGrant) {
		t.Fatalf("R0 after the grant ended: %v", err)
	}
}

// TestOAuthServerDB_ConcurrentRefreshesAllSucceedAndLeaveOnePair (§5.3 "Eşzamanlılık kanıtı", -race):
// N goroutines present one refresh token -- the grant lock orders them; every one gets a pair, and
// exactly one live refresh token and one live access token remain.
func TestOAuthServerDB_ConcurrentRefreshesAllSucceedAndLeaveOnePair(t *testing.T) {
	w := newServerWorld(t)
	t0 := w.connect()
	grant := w.resolve(KindRefresh, t0.Refresh).GrantID
	const n = 6
	var wg, start sync.WaitGroup
	start.Add(n)
	var ok atomic.Int32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			start.Done()
			start.Wait()
			if _, err := w.srv.Refresh(context.Background(), context.Background(), w.client, t0.Refresh, nil); err != nil {
				t.Errorf("refresh: %v", err)
				return
			}
			ok.Add(1)
		}()
	}
	wg.Wait()
	if ok.Load() != n {
		t.Fatalf("%d of %d succeeded", ok.Load(), n)
	}
	if rf, ac := w.liveTokens(grant); rf != 1 || ac != 1 {
		t.Fatalf("%d live refresh and %d live access tokens, want 1 and 1", rf, ac)
	}
}

// TestOAuthServerDB_RefreshScopeNarrowsTheAccessTokenOnly (§5.3, ORTA-3 (d)): no scope -> the grant's;
// a subset (any order, repeats) -> the access token carries the subset in oauth_tokens.scopes and the
// new refresh token carries NULL (the grant's); a wider set -> invalid_scope and nothing rotated.
func TestOAuthServerDB_RefreshScopeNarrowsTheAccessTokenOnly(t *testing.T) {
	w := newServerWorld(t)
	t0 := w.connect()
	ctx := context.Background()
	narrow, _ := ParseScope("timesheets:read timesheets:read")
	t1, err := w.srv.Refresh(ctx, ctx, w.client, t0.Refresh, narrow)
	if err != nil {
		t.Fatal(err)
	}
	if ScopeString(t1.Scopes) != "timesheets:read" || ScopeString(w.resolve(KindAccess, t1.Access).TokenScopes) != "timesheets:read" ||
		w.resolve(KindRefresh, t1.Refresh).TokenScopes != nil {
		t.Fatal("the narrowing")
	}
	t2, err := w.srv.Refresh(ctx, ctx, w.client, t1.Refresh, nil)
	if err != nil || ScopeString(t2.Scopes) != "employees:write timesheets:read" {
		t.Fatalf("no scope after a narrowing: %v %q", err, ScopeString(t2.Scopes))
	}
	wide, _ := ParseScope("timesheets:read punches:read")
	if _, err := w.srv.Refresh(ctx, ctx, w.client, t2.Refresh, wide); !is(err, ErrInvalidScope) {
		t.Fatalf("a widening: %v", err)
	}
	if w.resolve(KindRefresh, t2.Refresh).UsedAt != nil {
		t.Fatal("a refused widening used the refresh token")
	}
}

// TestOAuthServerDB_ARefreshTokenOfAnotherAppIsInvalidGrantAndUntouched: the token is c's grant's, the
// other app presents it -- invalid_grant, the token unused.
func TestOAuthServerDB_ARefreshTokenOfAnotherAppIsInvalidGrantAndUntouched(t *testing.T) {
	w := newServerWorld(t)
	t0 := w.connect()
	if _, err := w.srv.Refresh(context.Background(), context.Background(), w.other, t0.Refresh, nil); !is(err, ErrInvalidGrant) {
		t.Fatalf("another app: %v", err)
	}
	if w.resolve(KindRefresh, t0.Refresh).UsedAt != nil {
		t.Fatal("another app's presentation used the token")
	}
}

// TestOAuthServerDB_TheDeadlineMintsNothing (§5.3 "D", ORTA-2): when the client's context has ended
// before the commit, an exchange and a refresh are rolled back -- no grant, no token, no used_at --
// and the same calls succeed afterwards.
func TestOAuthServerDB_TheDeadlineMintsNothing(t *testing.T) {
	w := newServerWorld(t)
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	code, v := w.approve()
	if _, err := w.srv.ExchangeCode(context.Background(), gone, w.client, code, v, nil); !is(err, ErrEnded) {
		t.Fatalf("an exchange for a client that left: %v", err)
	}
	if w.liveGrants() != 0 || w.count(`SELECT count(*) FROM oauth_codes WHERE tenant_id = $1 AND consumed_at IS NOT NULL`, w.tenant) != 0 {
		t.Fatal("the rolled-back exchange left a grant or a spent code")
	}
	t0, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, v, nil)
	if err != nil {
		t.Fatalf("the exchange afterwards: %v", err)
	}
	grant := w.resolve(KindRefresh, t0.Refresh).GrantID
	before := w.count(`SELECT count(*) FROM oauth_tokens WHERE grant_id = $1`, grant)
	if _, err := w.srv.Refresh(context.Background(), gone, w.client, t0.Refresh, nil); !is(err, ErrEnded) {
		t.Fatalf("a refresh for a client that left: %v", err)
	}
	if w.resolve(KindRefresh, t0.Refresh).UsedAt != nil || w.count(`SELECT count(*) FROM oauth_tokens WHERE grant_id = $1`, grant) != before {
		t.Fatal("the rolled-back refresh wrote used_at or a token")
	}
	expired, cancel2 := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel2()
	<-expired.Done()
	if _, err := w.srv.Refresh(context.Background(), expired, w.client, t0.Refresh, nil); !is(err, ErrEnded) {
		t.Fatalf("D elapsed: %v", err)
	}
	if _, err := w.srv.Refresh(context.Background(), context.Background(), w.client, t0.Refresh, nil); err != nil {
		t.Fatalf("the refresh afterwards: %v", err)
	}
}

// TestOAuthServerDB_RevokeSemantics (§6, RFC 7009): an access token ends alone (the grant lives); a
// refresh token ends the grant ('client', its trail row); another app's token, an unknown token and a
// code are answered nil and change nothing.
func TestOAuthServerDB_RevokeSemantics(t *testing.T) {
	w := newServerWorld(t)
	tok := w.connect()
	ctx := context.Background()
	if err := w.srv.Revoke(ctx, w.other, tok.Refresh); err != nil || w.liveGrants() != 1 {
		t.Fatalf("another app's revocation: %v, %d live", err, w.liveGrants())
	}
	unknown, _ := Mint(KindRefresh)
	code, _ := Mint(KindCode)
	for _, v := range []Value{unknown, code, ParseValue("junk")} {
		if err := w.srv.Revoke(ctx, w.client, v); err != nil {
			t.Fatalf("an unknown token: %v", err)
		}
	}
	if err := w.srv.Revoke(ctx, w.client, tok.Access); err != nil {
		t.Fatal(err)
	}
	if w.resolve(KindAccess, tok.Access).TokenRevokedAt == nil || w.liveGrants() != 1 {
		t.Fatal("an access token's revocation")
	}
	if _, err := w.srv.Refresh(ctx, ctx, w.client, tok.Refresh, nil); err != nil {
		t.Fatalf("the connection after an access revocation: %v", err)
	}
	t2 := w.connect()
	if err := w.srv.Revoke(ctx, w.client, t2.Refresh); err != nil {
		t.Fatal(err)
	}
	if w.liveGrants() != 0 {
		t.Fatal("a refresh token's revocation did not end the grant")
	}
	rows := w.trail(ActionGrantRevoked)
	found := false
	for _, r := range rows {
		found = found || strings.Contains(r, `"client"`)
	}
	if !found {
		t.Fatalf("no 'client' revocation row: %v", rows)
	}
	if err := w.srv.Revoke(ctx, w.client, t2.Refresh); err != nil {
		t.Fatal("a second revocation is not 200")
	}
}

// TestOAuthServerDB_ClientAuthentication (§5.1): the right secret authenticates; a wrong one, another
// app's, a malformed one and a disabled app are one answer -- invalid_client.
func TestOAuthServerDB_ClientAuthentication(t *testing.T) {
	w := newServerWorld(t)
	ctx := context.Background()
	wrong, _ := Mint(KindSecret)
	for name, c := range map[string]struct {
		id     uuid.UUID
		secret Value
	}{
		"a wrong secret":    {w.client.ID, wrong},
		"a malformed one":   {w.client.ID, ParseValue("tt_cs_live_short")},
		"an access token":   {w.client.ID, mustMint(t, KindAccess)},
		"an unknown client": {uuid.New(), w.secret},
		"the other's id":    {w.other.ID, w.secret},
	} {
		if _, err := w.srv.AuthenticateClient(ctx, c.id, c.secret); !is(err, ErrInvalidClient) {
			t.Errorf("%s: %v", name, err)
		}
	}
	w.exec(`UPDATE oauth_clients SET disabled_at = clock_timestamp() WHERE id = $1`, w.client.ID)
	if _, err := w.srv.AuthenticateClient(ctx, w.client.ID, w.secret); !is(err, ErrInvalidClient) {
		t.Fatalf("a disabled app: %v", err)
	}
	c, err := w.srv.LookupClient(ctx, w.client.ID)
	if err != nil || !c.Disabled {
		t.Fatalf("the lookup of a disabled app: %v %v", err, c.Disabled)
	}
	if _, err := w.srv.LookupClient(ctx, uuid.New()); !is(err, ErrNoClient) {
		t.Fatalf("an unknown app: %v", err)
	}
}

func mustMint(t *testing.T, k Kind) Value {
	t.Helper()
	v, err := Mint(k)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestOAuthServerDB_AReplayPastTheWindowEndsTheGrantWhateverItsScope (the 2nd phase's review, L5): the
// scope is checked after the reuse branches, so a used refresh token presented 31 seconds after its
// first use ends the grant even when it asks for a scope wider than the grant's.
func TestOAuthServerDB_AReplayPastTheWindowEndsTheGrantWhateverItsScope(t *testing.T) {
	w := newServerWorld(t)
	t0 := w.connect(ScopeTimesheetsRead)
	grant := w.resolve(KindRefresh, t0.Refresh).GrantID
	aged, _ := Mint(KindRefresh)
	h, _ := w.srv.hasher.Hash(KindRefresh, aged)
	w.exec(`INSERT INTO oauth_tokens (tenant_id, grant_id, kind, token_hash, created_at, expires_at, used_at)
	        VALUES ($1, $2, 'refresh', $3, now() - interval '1 hour', now() - interval '1 hour' + interval '2160 hours', clock_timestamp() - interval '31 seconds')`,
		w.tenant, grant, h.reveal())
	wide, _ := ParseScope("timesheets:read punches:read")
	if _, err := w.srv.Refresh(context.Background(), context.Background(), w.client, aged, wide); !is(err, ErrInvalidGrant) {
		t.Fatalf("a replay past the window with a wide scope: %v, want invalid_grant", err)
	}
	if w.liveGrants() != 0 || len(w.trail(ActionRefreshReuse)) != 1 {
		t.Fatal("the replay with a wide scope left the grant alive")
	}
}

// TestOAuthServerDB_ExpiresInIsTheRowsNotAConstant (§5.4, the "Uygulama notları"; review L6): expires_in
// is expires_at minus the database's clock at the insert, rounded down. The exchange is held two
// seconds inside its transaction (after the code is spent, before the pair is inserted): expires_at
// is now() -- the transaction's start -- plus an hour, so expires_in is at most 3598. A constant 3600
// is red here.
func TestOAuthServerDB_ExpiresInIsTheRowsNotAConstant(t *testing.T) {
	w := newServerWorld(t)
	code, v := w.approve()
	w.srv.HoldTheConsumedCode(func() { time.Sleep(2100 * time.Millisecond) })
	tok, err := w.srv.ExchangeCode(context.Background(), context.Background(), w.client, code, v, nil)
	if err != nil {
		t.Fatal(err)
	}
	var left int64
	if err := w.owner.QueryRow(context.Background(), `SELECT floor(extract(epoch FROM expires_at - created_at))::bigint FROM oauth_tokens WHERE id = $1`,
		w.resolve(KindAccess, tok.Access).TokenID).Scan(&left); err != nil {
		t.Fatal(err)
	}
	if left != 3600 || tok.ExpiresIn > 3598 || tok.ExpiresIn < 3590 {
		t.Fatalf("the row's lifetime is %d s and expires_in %d; want 3600 and at most 3598 after a 2 s hold", left, tok.ExpiresIn)
	}
}
