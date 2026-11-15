package oauthapi_test

// surface_db_test.go -- the token and revocation endpoints over real HTTP (httptest) and REAL Postgres,
// with the shipped oauth.Server behind them: the response's fields and headers (A3, RFC 6749 §5.1),
// refresh over the wire, RFC 7009's 200s -- and nothing of a credential in a log line or an error body.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/oauthapi"
	"github.com/atknatk/tappa/internal/oauth"
	"github.com/atknatk/tappa/test/fixtures"
)

type wire struct {
	t      *testing.T
	srv    *oauth.Server
	h      http.Handler
	logs   *lockedWriter
	owner  *pgx.Conn
	data   *db.DB
	tenant uuid.UUID
	admin  uuid.UUID
	client uuid.UUID
	secret oauth.Value
	auth   string
}

func newWire(t *testing.T) *wire {
	t.Helper()
	dsn, ownerDSN := os.Getenv("DATABASE_URL"), os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" || ownerDSN == "" {
		t.Skip("DATABASE_URL / DATABASE_MIGRATE_URL not set; skipping the token endpoint's database tests")
	}
	ctx := context.Background()
	data, err := db.New(ctx, &config.Config{DatabaseURL: config.NewDSN(dsn)})
	if err != nil {
		t.Fatal(err)
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
	w := &wire{t: t, owner: owner, data: data, tenant: uuid.New(), admin: uuid.New(), logs: &lockedWriter{w: &bytes.Buffer{}}}
	log := slog.New(slog.NewTextHandler(w.logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	key := []byte(strings.Repeat("w", 32))
	if w.srv, err = oauth.NewServer(key, data, trail, log); err != nil {
		t.Fatal(err)
	}
	s, err := oauthapi.New(oauthapi.Config{Authority: w.srv, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	s.Mount(r)
	w.h = r
	if err := data.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
		                           VALUES ($1, 'OAuth Wire Fixture Ltd', $2, 'restaurant', 'multi')`, w.tenant, "VAT-OAW-"+w.tenant.String()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		                        VALUES ($1, $2, 'OAuth Wire Owner', $3, $4, 'owner', 'active')`,
			w.admin, w.tenant, "oauth-wire-"+w.admin.String()+"@m11.example", fixtures.UnusablePasswordHash)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var operator uuid.UUID
	if err := owner.QueryRow(ctx, `
		WITH made AS (
		    INSERT INTO platform_admins (email, display_name, status)
		    SELECT $1, 'oauth wire fixture', 'disabled'
		     WHERE NOT EXISTS (SELECT 1 FROM platform_admins WHERE email = $1)
		    RETURNING id)
		SELECT id FROM made UNION ALL SELECT id FROM platform_admins WHERE email = $1 LIMIT 1`, "oauth-wire-fixture@example.test").Scan(&operator); err != nil {
		t.Fatal(err)
	}
	if w.secret, err = oauth.Mint(oauth.KindSecret); err != nil {
		t.Fatal(err)
	}
	hasher, _ := oauth.NewHasher(key)
	d, _ := hasher.Hash(oauth.KindSecret, w.secret)
	if err := owner.QueryRow(ctx, `INSERT INTO oauth_clients (name, redirect_uris, allowed_scopes, secret_hash, secret_hint, created_by)
	                               VALUES ('Wire App', '{https://wire.example/cb}', '{employees:read,timesheets:read}', $1, $2, $3) RETURNING id`,
		d.RevealToTheDatabase(), w.secret.SecretHint(), operator).Scan(&w.client); err != nil {
		t.Fatal(err)
	}
	w.auth = basic(w.client, w.secret.RevealToTheOperatorOnce())
	return w
}

// code is the owner's consent: a fresh code and its verifier.
func (w *wire) code() (oauth.Value, string) {
	w.t.Helper()
	verifier := strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "")
	sum := sha256.Sum256([]byte(verifier))
	code, err := w.srv.Approve(context.Background(), w.tenant, w.admin, oauth.Request{
		ClientID: w.client, RedirectURI: "https://wire.example/cb", ResponseType: "code", Method: "S256",
		Challenge: base64.RawURLEncoding.EncodeToString(sum[:]), Scopes: []string{"employees:read", "timesheets:read"},
	})
	if err != nil {
		w.t.Fatal(err)
	}
	return code, verifier
}

type tokenBody struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

func (w *wire) tokens(form url.Values) (tokenBody, int, string) {
	w.t.Helper()
	rec := post(w.h, oauthapi.TokenPath, w.auth, form, "203.0.113.70:1")
	var b tokenBody
	if rec.Code == 200 {
		if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
			w.t.Fatal(err)
		}
		if rec.Header().Get("Content-Type") != "application/json" || rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("Pragma") != "no-cache" {
			w.t.Fatalf("the success's headers: %v", rec.Header())
		}
		return b, rec.Code, ""
	}
	return b, rec.Code, errorWord(w.t, rec)
}

// TestOAuthTokenDB_TheWholeExchangeOverTheWire (A2, A3, §5.4, §6): code -> pair (every field, Bearer,
// expires_in from the row, the scope string); a second presentation of the code is invalid_grant; the
// refresh with a narrowed scope; the revocation of the refresh token ends the connection; a revoked
// refresh token is invalid_grant; and no code, verifier, secret or token is in the log or any body.
func TestOAuthTokenDB_TheWholeExchangeOverTheWire(t *testing.T) {
	w := newWire(t)
	code, verifier := w.code()
	b, status, word := w.tokens(url.Values{"grant_type": {"authorization_code"}, "code": {code.RevealInRedirect()}, "code_verifier": {verifier},
		"redirect_uri": {"https://wire.example/cb"}})
	if status != 200 {
		t.Fatalf("the exchange: %d %s", status, word)
	}
	if b.TokenType != "Bearer" || b.ExpiresIn < 3590 || b.ExpiresIn > 3600 || b.Scope != "employees:read timesheets:read" ||
		!strings.HasPrefix(b.AccessToken, "tt_at_"+oauth.Env+"_") || !strings.HasPrefix(b.RefreshToken, "tt_rt_"+oauth.Env+"_") {
		t.Fatalf("the token response: type %q, expires_in %d, scope %q", b.TokenType, b.ExpiresIn, b.Scope)
	}
	if _, status, word := w.tokens(url.Values{"grant_type": {"authorization_code"}, "code": {code.RevealInRedirect()}, "code_verifier": {verifier}}); status != 400 || word != "invalid_grant" {
		t.Fatalf("the code again: %d %s", status, word)
	}
	// The reuse ended that grant; connect again.
	code2, verifier2 := w.code()
	b, status, _ = w.tokens(url.Values{"grant_type": {"authorization_code"}, "code": {code2.RevealInRedirect()}, "code_verifier": {verifier2}})
	if status != 200 {
		t.Fatalf("the second exchange: %d", status)
	}
	r, status, word := w.tokens(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {b.RefreshToken}, "scope": {"timesheets:read"}})
	if status != 200 || r.Scope != "timesheets:read" || r.RefreshToken == b.RefreshToken {
		t.Fatalf("the refresh: %d %s scope %q", status, word, r.Scope)
	}
	if _, status, word := w.tokens(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {r.RefreshToken}, "scope": {"timesheets:read punches:read"}}); status != 400 || word != "invalid_scope" {
		t.Fatalf("a widening: %d %s", status, word)
	}
	if rec := post(w.h, oauthapi.RevokePath, w.auth, url.Values{"token": {r.RefreshToken}, "token_type_hint": {"access_token"}}, "203.0.113.70:1"); rec.Code != 200 || rec.Body.Len() != 0 {
		t.Fatalf("the revocation: %d %q", rec.Code, rec.Body.String())
	}
	if _, status, word := w.tokens(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {r.RefreshToken}}); status != 400 || word != "invalid_grant" {
		t.Fatalf("a revoked connection's refresh: %d %s", status, word)
	}
	if rec := post(w.h, oauthapi.RevokePath, w.auth, url.Values{"token": {"tt_rt_" + oauth.Env + "_" + strings.Repeat("A", 43)}}, "203.0.113.70:1"); rec.Code != 200 {
		t.Fatalf("an unknown token's revocation: %d", rec.Code)
	}
	logs := w.logs.String()
	for _, secret := range []string{code.RevealInRedirect(), code2.RevealInRedirect(), verifier, verifier2, w.secret.RevealToTheOperatorOnce(), b.AccessToken, b.RefreshToken, r.AccessToken, r.RefreshToken} {
		if strings.Contains(logs, secret) {
			t.Fatal("a credential reached the log")
		}
	}
}

// TestOAuthTokenDB_TheWrongSecretIs401AndKeepsItsUnit: the right client id with another secret is
// 401 invalid_client, WWW-Authenticate: Basic -- and the address unit is kept (60 of them fill it).
func TestOAuthTokenDB_TheWrongSecretIs401AndKeepsItsUnit(t *testing.T) {
	w := newWire(t)
	wrong := basic(w.client, mint(t, oauth.KindSecret).RevealToTheOperatorOnce())
	for i := 0; i < 60; i++ {
		rec := post(w.h, oauthapi.TokenPath, wrong, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}}, "203.0.113.71:1")
		if rec.Code != 401 || rec.Header().Get("WWW-Authenticate") != `Basic realm="taptime"` {
			t.Fatalf("wrong secret %d: %d", i+1, rec.Code)
		}
	}
	if rec := post(w.h, oauthapi.TokenPath, w.auth, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"x"}}, "203.0.113.71:1"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("the right secret from the flooded address: %d", rec.Code)
	}
}
