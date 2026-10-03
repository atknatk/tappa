package main

// operator_test.go -- M10 OP-7's wiring: the operator surface off and configured, what
// its start-up lines and refusals print, which code may hold the operator's pool, and
// the manifest's four optional keys.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/internal/sun"
)

// noStore satisfies operatorauth.Store, operator.LegalStore and operator.TenantStore
// without a database; building an Authenticator calls none of its methods, and nothing
// here sends a sign-in, a legal or a tenant request.
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

// noTexts is an empty legal snapshot whose refresh does nothing (operator.LegalTexts).
type noTexts struct{}

func (noTexts) Published() map[string]legal.Doc { return map[string]legal.Doc{} }
func (noTexts) Refresh(context.Context) error   { return nil }

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// shippedFormats are the two log handlers this command builds (logHandler), at DEBUG
// so nothing a line could carry is filtered out before the search.
func shippedLoggers(buf *bytes.Buffer) map[string]*slog.Logger {
	out := map[string]*slog.Logger{}
	for _, f := range []string{config.LogFormatText, config.LogFormatJSON} {
		out[f] = slog.New(logHandler(buf, &config.Config{LogLevel: "debug", LogFormat: f}))
	}
	return out
}

// TestOpenOperatorSurface_OffWhenNothingIsSet: with no operator configuration the
// command opens nothing, logs the OFF fact, and mounts a surface that answers 503 on
// /operator beside an unchanged health route.
func TestOpenOperatorSurface_OffWhenNothingIsSet(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(logHandler(&buf, &config.Config{LogLevel: "debug", LogFormat: config.LogFormatJSON}))
	surface, closeFn, err := openOperatorSurface(t.Context(), &config.Config{}, noTexts{}, log)
	if err != nil {
		t.Fatalf("an unconfigured process refused to start: %v", err)
	}
	defer closeFn()
	if surface.Configured() {
		t.Fatal("an unconfigured process built a configured surface")
	}
	if !strings.Contains(buf.String(), `"operator_surface":"off"`) {
		t.Errorf("the OFF fact was not logged: %q", buf.String())
	}
	srv := httptest.NewServer(httpx.NewRouter(nil, nil, surface))
	defer srv.Close()
	for path, want := range map[string]int{"/operator": 503, "/operator/login": 503, httpx.HealthPath: 200} {
		res, err := srv.Client().Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		b := new(bytes.Buffer)
		_, _ = b.ReadFrom(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("GET %s = %d, want %d", path, res.StatusCode, want)
		}
		// The OFF surface, not the unavailable one: the body says "not configured".
		if want == 503 && !strings.Contains(b.String(), "not configured") {
			t.Errorf("GET %s on an unconfigured process does not say the surface is not configured: %q", path, b.String())
		}
	}
}

// closedPortDSN is a tappa_operator DSN to a port nothing listens on, around a random
// password -- built at run time so no file carries a credential-shaped URL.
func closedPortDSN(t *testing.T) (dsn, password string) {
	t.Helper()
	password = hex.EncodeToString(randBytes(t, 16))
	u := url.URL{Scheme: "postgres", User: url.UserPassword("tappa_operator", password), Host: "127.0.0.1:1", Path: "/tappa"}
	return u.String() + "?sslmode=disable", password
}

// TestOpenOperatorSurface_UnreachableKeepsTheProductUpARefusalStopsTheBoot pins which
// failures stop the boot (openOperatorSurface's doc comment): a database that cannot be
// REACHED leaves the customer product up and the surface unavailable (503, an ERROR
// line); a malformed DSN and the role gate's refusal of what WAS reached stop it.
func TestOpenOperatorSurface_UnreachableKeepsTheProductUpARefusalStopsTheBoot(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	operatorCfg := func(dsn string) *config.Config {
		return &config.Config{OperatorDatabaseURL: dsn, OperatorTOTPKEK: randBytes(t, 32),
			OperatorTokenHMACKey: randBytes(t, 32), OperatorHost: "ops.taptime.mt"}
	}

	t.Run("unreachable: boots, surface unavailable", func(t *testing.T) {
		dsn, _ := closedPortDSN(t)
		var buf bytes.Buffer
		log := slog.New(logHandler(&buf, &config.Config{LogLevel: "info", LogFormat: config.LogFormatJSON}))
		surface, closeFn, err := openOperatorSurface(ctx, operatorCfg(dsn), noTexts{}, log)
		if err != nil {
			t.Fatalf("an unreachable operator database stopped the boot: %v", err)
		}
		defer closeFn()
		if surface.Configured() {
			t.Fatal("an unreachable operator database produced a configured surface")
		}
		if !strings.Contains(buf.String(), `"operator_surface":"unavailable"`) || !strings.Contains(buf.String(), `"level":"ERROR"`) {
			t.Errorf("the UNAVAILABLE fact was not logged as an ERROR: %q", buf.String())
		}
		srv := httptest.NewServer(httpx.NewRouter(nil, nil, surface))
		defer srv.Close()
		res, err := srv.Client().Get(srv.URL + "/operator")
		if err != nil {
			t.Fatal(err)
		}
		b := new(bytes.Buffer)
		_, _ = b.ReadFrom(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != 503 || !strings.Contains(b.String(), "unavailable") || strings.Contains(b.String(), "not configured") {
			t.Errorf("GET /operator on an unavailable surface = %d %q", res.StatusCode, b.String())
		}
	})
	t.Run("malformed DSN: the boot stops", func(t *testing.T) {
		_, _, err := openOperatorSurface(ctx, operatorCfg("postgres://tappa_operator@127.0.0.1:notaport/tappa"), noTexts{}, slog.New(slog.DiscardHandler))
		if err == nil || errors.Is(err, db.ErrOperatorUnreachable) {
			t.Fatalf("a malformed DSN: err %v, want a refusal that is not unreachability", err)
		}
	})
	t.Run("a server that refused: the boot stops", func(t *testing.T) {
		app := os.Getenv("DATABASE_URL")
		if app == "" {
			t.Skip("DATABASE_URL not set (real Postgres required)")
		}
		// The customer DSN asking to become tappa_operator: the server answers 42501.
		// It was reached; it refused. (OP-7 2nd round, B1.)
		forged := app + map[bool]string{true: "&", false: "?"}[strings.Contains(app, "?")] + "role=tappa_operator"
		s, _, err := openOperatorSurface(ctx, operatorCfg(forged), noTexts{}, slog.New(slog.DiscardHandler))
		if err == nil || s != nil || errors.Is(err, db.ErrOperatorUnreachable) || !strings.Contains(err.Error(), "SQLSTATE 42501") {
			t.Fatalf("a server that refused (42501): err %v, surface %v; want a boot refusal naming the code", err, s)
		}
	})
	t.Run("the role gate: the boot stops", func(t *testing.T) {
		owner := os.Getenv("DATABASE_MIGRATE_URL")
		if owner == "" {
			t.Skip("DATABASE_MIGRATE_URL not set (real Postgres required)")
		}
		s, _, err := openOperatorSurface(ctx, operatorCfg(owner), noTexts{}, slog.New(slog.DiscardHandler))
		if err == nil || s != nil || errors.Is(err, db.ErrOperatorUnreachable) {
			t.Fatalf("the owner's DSN: err %v, surface %v; want a boot refusal that is not unreachability", err, s)
		}
	})
}

// TestOpenOperatorSurface_APartialStructIsNeverSilentlyOff: a Config built without Load
// and carrying only ONE operator field never yields the OFF surface. The host alone or a
// key alone stops the boot (no DSN to open); the DSN alone to a closed port is the
// unavailable surface with its ERROR line -- configured, and saying so. This is what
// config.OperatorSurfaceConfigured's "any field" reading buys at the command (B5c).
func TestOpenOperatorSurface_APartialStructIsNeverSilentlyOff(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	dsn, _ := closedPortDSN(t)
	for name, c := range map[string]*config.Config{
		"only the host":      {OperatorHost: "ops.taptime.mt"},
		"only the TOTP KEK":  {OperatorTOTPKEK: randBytes(t, 32)},
		"only the token key": {OperatorTokenHMACKey: randBytes(t, 32)},
		"only the DSN":       {OperatorDatabaseURL: dsn},
	} {
		var buf bytes.Buffer
		log := slog.New(logHandler(&buf, &config.Config{LogLevel: "debug", LogFormat: config.LogFormatJSON}))
		s, closeFn, err := openOperatorSurface(ctx, c, noTexts{}, log)
		if closeFn != nil {
			closeFn()
		}
		if strings.Contains(buf.String(), `"operator_surface":"off"`) {
			t.Errorf("%s: the surface went OFF -- a partial configuration disappeared without a word", name)
		}
		if err == nil && !strings.Contains(buf.String(), `"operator_surface":"unavailable"`) {
			t.Errorf("%s: no refusal and no unavailable line (surface configured=%v)", name, s.Configured())
		}
	}
}

// TestConfiguredSurface_HoldsTheAuthenticatorAndAnnouncesIt drives the configured path
// with a store that needs no database: the keys reach operatorauth.New (a key of the
// wrong size is refused there), and the CONFIGURED fact is logged with the host.
func TestConfiguredSurface_HoldsTheAuthenticatorAndAnnouncesIt(t *testing.T) {
	cfg := &config.Config{
		OperatorTOTPKEK: randBytes(t, 32), OperatorTokenHMACKey: randBytes(t, 32), OperatorHost: "ops.taptime.mt",
		BaseURL: "https://taptime.mt",
	}
	var buf bytes.Buffer
	log := slog.New(logHandler(&buf, &config.Config{LogLevel: "info", LogFormat: config.LogFormatJSON}))
	surface, err := configuredSurface(noStore{}, noTexts{}, cfg, log)
	if err != nil {
		t.Fatalf("configuredSurface: %v", err)
	}
	if !surface.Configured() {
		t.Fatal("configuredSurface built an unconfigured surface")
	}
	if !strings.Contains(buf.String(), `"operator_surface":"configured"`) || !strings.Contains(buf.String(), `"operator_host":"ops.taptime.mt"`) {
		t.Errorf("the CONFIGURED fact was not logged with its host: %q", buf.String())
	}
	for _, short := range []func(*config.Config){
		func(c *config.Config) { c.OperatorTOTPKEK = c.OperatorTOTPKEK[:16] },
		func(c *config.Config) { c.OperatorTokenHMACKey = nil },
	} {
		c := *cfg
		short(&c)
		if _, err := configuredSurface(noStore{}, noTexts{}, &c, log); err == nil {
			t.Error("a key of the wrong size reached a configured surface; the keys do not reach operatorauth.New")
		}
	}
}

// enrollStore records what an enrollment hands the database: the sealed TOTP secret and
// the new session's hash.
type enrollStore struct {
	noStore
	sealed      []byte
	sessionHash string
}

func (s *enrollStore) CompleteOperatorEnrollment(_ context.Context, _ uuid.UUID, _, _ string, sealed []byte, _ int64, h string) error {
	s.sealed = append([]byte(nil), sealed...)
	s.sessionHash = h
	return nil
}

// totpCode is RFC 6238 (HMAC-SHA1, 30 s, 6 digits), written out here so the test does
// not borrow the code it checks.
func totpCode(secret []byte, at time.Time) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(at.Unix()/30))
	m := hmac.New(sha1.New, secret)
	_, _ = m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	return fmt.Sprintf("%06d", (binary.BigEndian.Uint32(sum[off:off+4])&0x7fffffff)%1000000)
}

// TestOperatorAuthenticator_EachKeyIsInItsOwnSlot drives one enrollment through the
// Authenticator this command builds and checks where each configured key went: the TOTP
// secret it sealed OPENS with TAPPA_OPERATOR_TOTP_KEK (and not with the token key), and
// the session hash it stored IS HMAC-SHA256(TAPPA_OPERATOR_TOKEN_HMAC_KEY, the cookie's
// value). A swap, or one key in both slots, would sign people in all the same and undo
// the separation config.Load enforces; this is what sees it.
func TestOperatorAuthenticator_EachKeyIsInItsOwnSlot(t *testing.T) {
	cfg := &config.Config{OperatorTOTPKEK: randBytes(t, 32), OperatorTokenHMACKey: randBytes(t, 32)}
	store := &enrollStore{}
	auth, err := operatorAuthenticator(store, cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	pending, err := auth.BeginEnrollment(id)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(pending.Secret.Base32())
	if err != nil {
		t.Fatal(err)
	}
	link, err := operatorauth.NewEnrollmentToken()
	if err != nil {
		t.Fatal(err)
	}
	phrase := "correct horse battery staple"
	issued, err := auth.CompleteEnrollment(t.Context(), "192.0.2.1", id, link.RevealForLink(), pending.Blob, phrase, totpCode(secret, time.Now()))
	if err != nil {
		t.Fatalf("the enrollment did not complete: %v", err)
	}

	if _, err := sun.Open(cfg.OperatorTOTPKEK, id[:], store.sealed); err != nil {
		t.Error("the TOTP secret is not sealed under TAPPA_OPERATOR_TOTP_KEK")
	}
	if _, err := sun.Open(cfg.OperatorTokenHMACKey, id[:], store.sealed); err == nil {
		t.Error("the TOTP secret opens under the token HMAC key")
	}
	rec := httptest.NewRecorder()
	if err := operatorauth.SetSessionCookie(rec, issued.Token); err != nil {
		t.Fatal(err)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("%d cookies set, want the session cookie", len(cookies))
	}
	m := hmac.New(sha256.New, cfg.OperatorTokenHMACKey)
	_, _ = m.Write([]byte(cookies[0].Value))
	if store.sessionHash != hex.EncodeToString(m.Sum(nil)) {
		t.Error("the stored session hash is not HMAC-SHA256(TAPPA_OPERATOR_TOKEN_HMAC_KEY, the token)")
	}
}

// TestOperatorSurface_TheRunbookGrepMatchesTheShippedLine: deploy/README.md's operator
// surface runbook verifies the rollout by grepping the JSON log for the two facts. The
// tokens are taken from what the SHIPPED handler writes (05-config.yaml's level and
// format), so a renamed attribute or a demoted level turns this red instead of making
// the runbook read "nothing" for ever.
func TestOperatorSurface_TheRunbookGrepMatchesTheShippedLine(t *testing.T) {
	cfgSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "05-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	level := regexp.MustCompile(`(?m)^\s+TAPPA_LOG_LEVEL:\s*"?([a-zA-Z]+)"?`).FindStringSubmatch(string(cfgSrc))
	format := regexp.MustCompile(`(?m)^\s+TAPPA_LOG_FORMAT:\s*"?([a-zA-Z]+)"?`).FindStringSubmatch(string(cfgSrc))
	if level == nil || format == nil {
		t.Fatal("05-config.yaml does not set TAPPA_LOG_LEVEL and TAPPA_LOG_FORMAT; this test cannot know what ships")
	}
	shipped := &config.Config{LogLevel: level[1], LogFormat: format[1]}

	var off, on, unavailable bytes.Buffer
	if _, closeFn, err := openOperatorSurface(t.Context(), &config.Config{}, noTexts{}, slog.New(logHandler(&off, shipped))); err != nil {
		t.Fatal(err)
	} else {
		closeFn()
	}
	cfg := &config.Config{OperatorTOTPKEK: randBytes(t, 32), OperatorTokenHMACKey: randBytes(t, 32), OperatorHost: "ops.taptime.mt",
		BaseURL: "https://taptime.mt"}
	if _, err := configuredSurface(noStore{}, noTexts{}, cfg, slog.New(logHandler(&on, shipped))); err != nil {
		t.Fatal(err)
	}
	unreachable := *cfg
	unreachable.OperatorDatabaseURL, _ = closedPortDSN(t)
	if _, closeFn, err := openOperatorSurface(t.Context(), &unreachable, noTexts{}, slog.New(logHandler(&unavailable, shipped))); err != nil {
		t.Fatal(err)
	} else {
		closeFn()
	}
	readme, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	token := regexp.MustCompile(`"operator_surface":"[a-z]+"`)
	for name, line := range map[string]string{"off": off.String(), "configured": on.String(), "unavailable": unavailable.String()} {
		tok := token.FindString(line)
		if tok == "" {
			t.Errorf("at the SHIPPED level and format the %s line carries no operator_surface token: %q", name, line)
			continue
		}
		if !strings.Contains(string(readme), "grep -c '"+tok+"'") {
			t.Errorf("deploy/README.md does not grep for %s, the token the shipped %s line carries", tok, name)
		}
	}
}

// operatorSecrets is what a leaked operator value looks like in a log line.
func operatorSecrets(t *testing.T, dsn, password string, keys ...[]byte) map[string]string {
	t.Helper()
	out := map[string]string{"the DSN": dsn, "the DSN's password": password}
	for i, k := range keys {
		label := []string{"the TOTP KEK", "the token HMAC key"}[i]
		out[label+" (base64)"] = base64.StdEncoding.EncodeToString(k)
		out[label+" (hex)"] = hex.EncodeToString(k)
		// A []byte under %v, slog's text handler and the like: the form OP-6 measured
		// leaking ("ondalık bayt listesi").
		out[label+" (decimal list)"] = fmt.Sprint(k)
	}
	return out
}

// TestOpenOperatorSurface_PrintsNoValue is the leak half of the wiring (OP-7 brief: no
// new log line or error text prints a DSN, a password or a key). Everything this
// command writes on the operator path -- the OFF line, the CONFIGURED line, and every
// refusal as main() logs it (slog.Error("fatal", "err", err)) -- goes through the
// command's own handlers, text and JSON, at DEBUG, and is searched for the DSN, its
// password, and both keys as base64, as hex and as the decimal list fmt prints a []byte as.
//
// The failures: a DSN to a closed port (the dial fails: the UNAVAILABLE line logs the
// error), the owner's real DSN (the role gate refuses it -- searched for the DSN itself,
// since the role NAMES it prints share letters with a short development password), and
// keys of the wrong size.
// POSITIVE CONTROL: the same handlers DO carry each value when it is logged directly.
func TestOpenOperatorSurface_PrintsNoValue(t *testing.T) {
	password := hex.EncodeToString(randBytes(t, 16))
	u := url.URL{Scheme: "postgres", User: url.UserPassword("tappa_operator", password), Host: "127.0.0.1:1", Path: "/tappa"}
	dsn := u.String() + "?sslmode=disable"
	kek, hmacKey := randBytes(t, 32), randBytes(t, 32)
	secrets := operatorSecrets(t, dsn, password, kek, hmacKey)

	var buf bytes.Buffer
	loggers := shippedLoggers(&buf)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cfg := &config.Config{OperatorDatabaseURL: dsn, OperatorTOTPKEK: kek, OperatorTokenHMACKey: hmacKey, OperatorHost: "ops.taptime.mt",
		BaseURL: "https://taptime.mt"}
	refusals := 0
	for _, log := range loggers {
		// The dial failure: not a refusal to boot but the UNAVAILABLE line, which logs
		// the error itself.
		if s, _, err := openOperatorSurface(ctx, cfg, noTexts{}, log); err != nil || s.Configured() {
			t.Fatalf("a DSN to a closed port: err %v, configured %v; want an unavailable surface and no error", err, s.Configured())
		}
		refusals++
		// A key of the wrong size, refused by operatorauth.New.
		short := *cfg
		short.OperatorTOTPKEK = kek[:16]
		if _, err := configuredSurface(noStore{}, noTexts{}, &short, log); err == nil {
			t.Fatal("a 16-byte TOTP KEK built a configured surface")
		} else {
			log.Error("fatal", "err", err)
			refusals++
		}
		// The lines themselves.
		if _, _, err := openOperatorSurface(ctx, &config.Config{}, noTexts{}, log); err != nil {
			t.Fatal(err)
		}
		if _, err := configuredSurface(noStore{}, noTexts{}, cfg, log); err != nil {
			t.Fatal(err)
		}
	}
	if owner := os.Getenv("DATABASE_MIGRATE_URL"); owner != "" {
		for _, log := range loggers {
			_, _, err := openOperatorSurface(ctx, &config.Config{OperatorDatabaseURL: owner, OperatorTOTPKEK: kek,
				OperatorTokenHMACKey: hmacKey, OperatorHost: "ops.taptime.mt"}, noTexts{}, log)
			if err == nil {
				t.Fatal("the owner's DSN opened the operator surface")
			}
			log.Error("fatal", "err", err)
			refusals++
		}
		secrets["the owner's DSN"] = owner
	} else {
		t.Log("DATABASE_MIGRATE_URL not set: the role-gate refusal is not part of this run (real Postgres required)")
	}
	if refusals < 4 {
		t.Fatalf("only %d refusals were produced; the test is not exercising the paths it names", refusals)
	}
	out := buf.String()
	for label, s := range secrets {
		if strings.Contains(out, s) {
			t.Errorf("the operator path's log output carries %s", label)
		}
	}

	var control bytes.Buffer
	for _, log := range shippedLoggers(&control) {
		for _, s := range secrets {
			log.Debug("control", "v", s)
		}
	}
	for label, s := range secrets {
		if !strings.Contains(control.String(), s) {
			t.Fatalf("CONTROL FAILED: the shipped handlers do not carry %s even when it is logged directly", label)
		}
	}
}

// TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator reads this command's syntax
// trees (ADR 0021 §3.6: the operator's pool is given to the operator's side only).
//
// THE NAME IS OP-7'S AND NO LONGER THE WHOLE RULE. Since OP-10 phase B the pool reaches
// the Authenticator AND the operator surface's legal slot (operator.New's LegalStore),
// since OP-11 phase B also its tenant slot (operator.New's TenantStore), and the rules
// below say exactly that. It is not renamed because docs/plan/m10-platform.md
// cites it by this name three times (the OP-7 and OP-8 records) and ADR 0020 §7 once:
// a rename would leave those citations dangling (testnames_test.go's ratchet) in records
// that are not rewritten after the fact.
//
//   - db.NewOperatorDB is called exactly once in the command, inside openOperatorSurface;
//   - the value it returns is used only as configuredSurface's first argument and as
//     the receiver of Close, and openOperatorSurface returns no *db.OperatorDB;
//   - configuredSurface's store is used exactly three times: as operatorAuthenticator's
//     first argument and as operator.New's second and third (the LegalStore slot; the
//     TenantStore slot, OP-11) -- and that operatorAuthenticator's store only as
//     operatorauth.New's first argument;
//   - the legal snapshot travels the other way, from run() into the operator side:
//     openOperatorSurface's texts only as configuredSurface's second argument, and that
//     function's only as operator.New's fourth;
//   - it is ONE snapshot: legal.NewStore is named exactly once in the command, and run()
//     uses the value it binds exactly as the boot refresh's receiver, openOperatorSurface's
//     third argument and handler.NewMarketing's first -- so the store the operator's
//     screen refreshes is the store the public pages read (a second store for either side
//     would leave the public page on the boot text after every publication);
//   - run() receives the Surface and a closer, never the pool -- and passes the Surface
//     to httpx.NewRouter, so the surface is mounted rather than built and dropped.
//
// A call is named with its package qualifier as written (operator.New and
// operatorauth.New are told apart by it). The scan is syntactic and bounded to main.go
// and operator.go: the names are matched by spelling, and a value that leaves through
// another name (an alias, a struct field, a closure) is the uses function's "assigned to
// another name", "a composite literal's value" or "other" -- each of which fails the
// rules above. It is not a type-level or whole-program proof: a value that reached
// another package's global through a call these two files do not show is code review's.
func TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator(t *testing.T) {
	fset := token.NewFileSet()
	files := map[string]*ast.File{}
	for _, name := range []string{"main.go", "operator.go"} {
		f, err := parser.ParseFile(fset, filepath.Join(repoRoot, "cmd", "tappa", name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = f
	}
	funcs := map[string]*ast.FuncDecl{}
	calls, stores := 0, 0
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil {
				funcs[fd.Name.Name] = fd
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "NewOperatorDB" {
				calls++
			}
			if sel, ok := n.(*ast.SelectorExpr); ok && qualName(sel) == "legal.NewStore" {
				stores++
			}
			return true
		})
	}
	if calls != 1 {
		t.Fatalf("db.NewOperatorDB is named %d times in cmd/tappa, want once (openOperatorSurface)", calls)
	}
	if stores != 1 {
		t.Errorf("legal.NewStore is named %d times in cmd/tappa, want once (run): the operator's screen and the public pages share one snapshot", stores)
	}
	open, configured, run := funcs["openOperatorSurface"], funcs["configuredSurface"], funcs["run"]
	if open == nil || configured == nil || run == nil {
		t.Fatal("openOperatorSurface, configuredSurface or run is gone; this test reads them by name")
	}

	// uses describes every use of the NAME inside fn's body by its parent node: "arg<i>
	// of F" (an argument of a call to F), "recv of M" (the X of a selector .M -- a method
	// call or a method value), "returned", "assigned to another name", or "other
	// <type>". The name is matched by spelling, not by object (go/ast's object
	// resolution is deprecated and the command is not type-checked here), so the name
	// must be DEFINED exactly once in fn -- a shadowing redefinition would let a second
	// value travel under it, and fails the test instead.
	uses := func(fn *ast.FuncDecl, name string) []string {
		var out []string
		var stack []ast.Node
		defs := 0
		for _, f := range fn.Type.Params.List {
			for _, n := range f.Names {
				if n.Name == name {
					defs++
				}
			}
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if n == nil {
				stack = stack[:len(stack)-1]
				return false
			}
			if id, ok := n.(*ast.Ident); ok && id.Name == name && len(stack) > 0 {
				switch p := stack[len(stack)-1].(type) {
				case *ast.CallExpr:
					for i, a := range p.Args {
						if a == id {
							out = append(out, "arg"+strconv.Itoa(i)+" of "+qualName(p.Fun))
						}
					}
					if p.Fun == id {
						out = append(out, "called")
					}
				case *ast.SelectorExpr:
					if p.X == id {
						out = append(out, "recv of "+p.Sel.Name)
					}
				case *ast.KeyValueExpr:
					if p.Value == id {
						out = append(out, "a composite literal's value")
					}
				case *ast.AssignStmt:
					lhs := false
					for _, l := range p.Lhs {
						lhs = lhs || l == id
					}
					switch {
					case lhs && p.Tok == token.DEFINE:
						defs++
					case lhs:
						out = append(out, "reassigned")
					default:
						out = append(out, "assigned to another name")
					}
				case *ast.ValueSpec:
					defs++
				case *ast.ReturnStmt:
					out = append(out, "returned")
				default:
					out = append(out, fmt.Sprintf("other %T", p))
				}
			}
			stack = append(stack, n)
			return true
		})
		if defs != 1 {
			out = append(out, fmt.Sprintf("defined %d times", defs))
		}
		return out
	}
	// boundName is the name fn binds callee's first result to with := (callee as its
	// last identifier, or qualified as written: "legal.NewStore" is not encode.NewStore).
	boundName := func(fn *ast.FuncDecl, callee string) string {
		var name string
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			as, ok := n.(*ast.AssignStmt)
			if !ok || len(as.Rhs) != 1 {
				return true
			}
			if c, ok := as.Rhs[0].(*ast.CallExpr); ok && (exprName(c.Fun) == callee || qualName(c.Fun) == callee) {
				if id, ok := as.Lhs[0].(*ast.Ident); ok {
					name = id.Name
				}
			}
			return true
		})
		return name
	}

	pool := boundName(open, "NewOperatorDB")
	if pool == "" || pool == "_" {
		t.Fatal("openOperatorSurface does not bind db.NewOperatorDB's result to a name")
	}
	for _, u := range uses(open, pool) {
		if u != "arg0 of configuredSurface" && u != "recv of Close" {
			t.Errorf("openOperatorSurface uses the operator's pool as %q; it may only be configuredSurface's store and Close's receiver", u)
		}
	}
	for _, res := range open.Type.Results.List {
		if star, ok := res.Type.(*ast.StarExpr); ok && exprName(star.X) == "OperatorDB" {
			t.Error("openOperatorSurface returns a *db.OperatorDB: run() would hold the operator's pool")
		}
	}
	authFn := funcs["operatorAuthenticator"]
	if authFn == nil {
		t.Fatal("operatorAuthenticator is gone; this test reads it by name")
	}
	// Each parameter's uses, exactly (a use missing is as red as a use added: a store
	// that stopped reaching operator.New would leave the legal screen without its slot).
	for _, hop := range []struct {
		fn    *ast.FuncDecl
		param string
		want  []string
	}{
		{configured, "store", []string{"arg0 of operatorAuthenticator", "arg1 of operator.New", "arg2 of operator.New"}},
		{configured, "texts", []string{"arg3 of operator.New"}},
		{authFn, "store", []string{"arg0 of operatorauth.New"}},
		{open, "texts", []string{"arg1 of configuredSurface"}},
		{run, boundName(run, "legal.NewStore"), []string{"recv of Refresh", "arg2 of openOperatorSurface", "arg0 of handler.NewMarketing"}},
	} {
		got := uses(hop.fn, hop.param)
		sort.Strings(got)
		want := append([]string(nil), hop.want...)
		sort.Strings(want)
		if strings.Join(got, "; ") != strings.Join(want, "; ") {
			t.Errorf("%s uses its %s as %q; want exactly %q", hop.fn.Name.Name, hop.param, got, want)
		}
	}

	surface := boundName(run, "openOperatorSurface")
	if surface == "" || surface == "_" {
		t.Fatal("run() does not bind openOperatorSurface's result")
	}
	mounted := false
	for _, u := range uses(run, surface) {
		if strings.HasSuffix(u, " of httpx.NewRouter") {
			mounted = true
		}
	}
	if !mounted {
		t.Error("run() builds the operator surface and never passes it to httpx.NewRouter: /operator would answer 404, not 503")
	}
}

// qualName is a call's function expression as written, qualifier included (pkg.F ->
// "pkg.F", F -> "F"); "" for any other shape.
func qualName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name + "." + x.Sel.Name
		}
	}
	return ""
}

// exprName is the last identifier of a call's function expression (pkg.F -> F, F -> F).
func exprName(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}

// TestPackaging_TheOperatorSurfaceIsOneOptionalSecretSet pins the three facts
// 20-app.yaml's comment calls load-bearing, for each variable in internal/config's OWN
// list of the four: it is an env entry of the serving container, from a secretKeyRef
// to tappa-secrets under its own name, marked `optional: true`; and it is NOT a
// ConfigMap key (a key there would be re-applied by every deploy -- the partial set).
func TestPackaging_TheOperatorSurfaceIsOneOptionalSecretSet(t *testing.T) {
	appSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "20-app.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfgSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "05-config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	item := listItemNamed(blockUnder(stripYAMLComments(string(appSrc)), "containers:"), servingContainer)
	if item == nil {
		t.Fatalf("container %q not found", servingContainer)
	}
	envBlock := strings.Join(blockUnder(strings.Join(item, "\n"), "env:"), "\n")
	entry := func(name string) string {
		i := strings.Index(envBlock, "- name: "+name+"\n")
		if i < 0 {
			return ""
		}
		rest := envBlock[i+1:]
		if j := strings.Index(rest, "- name: "); j >= 0 {
			rest = rest[:j]
		}
		return rest
	}
	cm := configMapKeys(t, string(cfgSrc))
	names := config.OperatorSurfaceVariables()
	if len(names) != 4 {
		t.Fatalf("PREMISE: internal/config lists %d operator variables, want 4", len(names))
	}
	for _, name := range names {
		e := entry(name)
		switch {
		case e == "":
			t.Errorf("%s is not an env entry of the serving container", name)
			continue
		case !strings.Contains(e, "secretKeyRef:"):
			t.Errorf("%s does not come from a secretKeyRef", name)
		case !regexp.MustCompile(`(?m)^\s+name: tappa-secrets$`).MatchString(e):
			t.Errorf("%s does not come from tappa-secrets", name)
		case !regexp.MustCompile(`(?m)^\s+key: ` + name + `$`).MatchString(e):
			t.Errorf("%s does not read the Secret key of its own name", name)
		case !regexp.MustCompile(`(?m)^\s+optional: true$`).MatchString(e):
			t.Errorf("%s is not `optional: true`: every deploy before the operator runbook would stall on a missing key", name)
		}
		if cm[name] {
			t.Errorf("%s is a ConfigMap key: every deploy would re-apply it, and with the keys absent that is the partial set", name)
		}
	}
	// CONTROL: the slicing isolates ONE entry -- a required neighbour is not optional.
	if e := entry("TAPPA_TAG_KEK"); e == "" || strings.Contains(e, "optional: true") {
		t.Fatal("CONTROL FAILED: the entry slicer does not isolate TAPPA_TAG_KEK as a required entry")
	}
}
