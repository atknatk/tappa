package operator_test

// op10_db_test.go -- M10 OP-10 phase B end to end against PostgreSQL: the operator signs
// in through the surface, publishes through /operator/legal (op_publish_legal), the public
// page /legal/privacy serves the new text after the refresh (internal/domain/legal.Store
// on tappa_app's pool, the customer product's real Marketing handler), the version list
// (op_begin_read + op_read_legal_versions) names the publisher, and undoing the
// publication is a publication -- a new row.
//
// WHY THIS FILE COMMITS. A version-list read is two transactions and the second refuses a
// ticket whose transaction has not committed (ADR 0021 §2 v), so the one-transaction
// rig of op8_db_test.go cannot drive it; and the public page reads the snapshot through
// tappa_app's own pool, which sees committed rows only. So every store call here runs on
// ONE owner connection switched to tappa_operator for the whole session (SET SESSION
// AUTHORIZATION; the identity is read back before use -- asOperator -- and again at the
// end), where each statement is its own committed transaction, the shape db.OperatorDB's
// pool gives production (TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions
// measures that pool). The operator-tables lock is newE2E's: SHARED, once per test.
//
// WHAT ONE RUN LEAVES BEHIND (the class internal/db's OP-10 tests leave, card OP-10A md.
// 12; append-only tables and the foreign keys that point at them): one operator account
// (disabled at cleanup), one session (revoked by the sign-out), its audit rows -- login,
// logout, two reads and two or four legal_publish -- and as many committed 'privacy'
// versions: TWO when a privacy text the form can carry existed before the run (the run's
// text, then that text again as the undo, so the public page ends where it started), FOUR
// when none did (the run's text, a FAKE earlier text, the run's text again, the earlier
// text again as the undo; the page ends on this test's own FAKE text). Read tickets are
// deleted at cleanup.
//
// A COUNTED LIMIT: another package's test that commits a 'privacy' version between this
// test's publication and its refresh (internal/domain/legal's tests commit versions)
// turns the public-page assertion red; the window is a statement long.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"html"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/legal"
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/internal/sun"
)

// liveStore is operatorauth.Store and operator.LegalStore on a connection that IS
// tappa_operator: each method is internal/db's production accessor, each statement its
// own committed transaction.
type liveStore struct {
	mu   sync.Mutex
	conn *pgx.Conn
}

func (s *liveStore) OperatorByEmail(ctx context.Context, email string) (db.OperatorAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return db.OperatorByEmail(ctx, s.conn, email)
}

func (s *liveStore) OperatorByID(ctx context.Context, id uuid.UUID) (db.OperatorAccount, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return db.OperatorByID(ctx, s.conn, id)
}

func (s *liveStore) RecordOperatorAuthEvent(ctx context.Context, kind db.OperatorAuthEvent, email string, admin uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return db.RecordOperatorAuthEvent(ctx, s.conn, kind, email, admin)
}

func (s *liveStore) OpenOperatorSession(ctx context.Context, admin uuid.UUID, h string, step int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return db.OpenOperatorSession(ctx, s.conn, admin, h, step)
}

func (s *liveStore) CompleteOperatorEnrollment(ctx context.Context, admin uuid.UUID, raw, digest string, sealed []byte, step int64, h string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return db.CompleteOperatorEnrollment(ctx, s.conn, admin, raw, digest, sealed, step, h)
}

func (s *liveStore) TouchOperatorSession(ctx context.Context, h string) (db.OperatorSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return db.TouchOperatorSession(ctx, s.conn, h)
}

func (s *liveStore) CloseOperatorSession(ctx context.Context, h string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return db.CloseOperatorSession(ctx, s.conn, h)
}

func (s *liveStore) LegalVersions(ctx context.Context, h string, page db.LegalVersionsPage) ([]db.LegalVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return db.LegalVersions(ctx, s.conn, h, page)
}

func (s *liveStore) PublishLegal(ctx context.Context, h, slug, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return db.PublishLegal(ctx, s.conn, h, slug, body)
}

// legalE2E is one test's committed rig.
type legalE2E struct {
	*e2e
	owner    *pgx.Conn
	op       *pgx.Conn
	texts    *legal.Store
	f        fixture
	name     string
	tokenKey []byte // the Authenticator's session-token HMAC key, to plant a session row
}

// newLegalE2E takes newE2E's lock and clock, then opens the committed rig: an owner
// connection, a connection switched to tappa_operator, tappa_app's pool for the public
// snapshot (boot-read here), an active operator committed by the owner (disabled at
// cleanup), and the operator surface beside the customer product's Marketing handler on
// the shipped router (g.h is replaced: newE2E's surface runs on its own transaction).
func newLegalE2E(t *testing.T) *legalE2E {
	t.Helper()
	g := newE2E(t)
	appDSN := os.Getenv("DATABASE_URL")
	if appDSN == "" {
		t.Skip("DATABASE_URL not set; the public snapshot reads through tappa_app's pool (CLAUDE.md §8)")
	}
	connect := func() *pgx.Conn {
		t.Helper()
		c, err := pgx.Connect(g.ctx, os.Getenv("DATABASE_MIGRATE_URL"))
		if err != nil {
			t.Fatalf("connect as the owner: %v", err)
		}
		t.Cleanup(func() { _ = c.Close(context.Background()) })
		return c
	}
	l := &legalE2E{e2e: g, owner: connect(), op: connect()}
	if _, err := l.op.Exec(g.ctx, `SET SESSION AUTHORIZATION tappa_operator`); err != nil {
		t.Fatalf("switch to tappa_operator: %v", err)
	}
	if err := asOperator(g.ctx, l.op); err != nil {
		t.Fatalf("harness: %v", err)
	}
	app, err := db.New(g.ctx, &config.Config{DatabaseURL: appDSN})
	if err != nil {
		t.Fatalf("tappa_app's pool: %v", err)
	}
	t.Cleanup(app.Close)
	if l.texts, err = legal.NewStore(app); err != nil {
		t.Fatal(err)
	}
	if err := l.texts.Refresh(g.ctx); err != nil { // the boot read
		t.Fatalf("the boot read of the snapshot: %v", err)
	}
	digest, err := e2eDigest()
	if err != nil {
		t.Fatal(err)
	}
	l.f = fixture{id: uuid.New(), password: e2ePassphrase, key: randBytes(t, 20)}
	l.f.email = "op10b-" + l.f.id.String()[:12] + "@example.test"
	l.name = "op10b e2e " + l.f.id.String()[:8]
	sealed, err := sun.Seal(g.kek, l.f.id[:], l.f.key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.owner.Exec(g.ctx, `INSERT INTO platform_admins (id, email, display_name, status, password_hash, totp_secret_sealed)
	                                  VALUES ($1, $2, $3, 'active', $4, $5)`, l.f.id, l.f.email, l.name, digest, sealed); err != nil {
		t.Fatalf("commit the operator: %v", err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		for _, s := range []struct{ what, sql string }{
			{"delete the read tickets", `DELETE FROM operator_read_tickets k USING platform_sessions s
			                             WHERE k.session_id = s.id AND s.admin_id = $1`},
			{"revoke the sessions", `UPDATE platform_sessions SET revoked_at = coalesce(revoked_at, clock_timestamp()) WHERE admin_id = $1`},
			{"disable the operator", `UPDATE platform_admins SET status = 'disabled' WHERE id = $1`},
		} {
			if _, err := l.owner.Exec(ctx, s.sql, l.f.id); err != nil {
				t.Errorf("cleanup: %s: %v", s.what, err)
			}
		}
	})
	log := debugCapture(&g.logs)
	l.tokenKey = randBytes(t, 32)
	auth, err := operatorauth.New(&liveStore{conn: l.op}, operatorauth.Config{
		TOTPKEK: operatorauth.NewKey(g.kek), TokenHMACKey: operatorauth.NewKey(l.tokenKey),
		Now: func() time.Time { return g.now }, Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := operator.New(auth, &liveStore{conn: l.op}, l.texts, opHost, opBase, log)
	if err != nil {
		t.Fatal(err)
	}
	g.auth = auth
	g.h = httpx.NewRouter(&config.Config{OperatorHost: opHost, BaseURL: opBase}, log, s, handler.NewMarketing(l.texts, log))
	return l
}

// ownerInt is one integer the owner reads, committed state.
func (l *legalE2E) ownerInt(sql string, args ...any) int {
	l.t.Helper()
	var n int
	if err := l.owner.QueryRow(l.ctx, sql, args...).Scan(&n); err != nil {
		l.t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// publicPrivacy is the customer host's /legal/privacy body.
func (l *legalE2E) publicPrivacy() string {
	l.t.Helper()
	w := l.do(req{method: http.MethodGet, host: custHost, path: "/legal/privacy"})
	if w.Code != http.StatusOK {
		l.t.Fatalf("GET /legal/privacy on the customer host = %d", w.Code)
	}
	return w.Body.String()
}

// TestE2E_LegalPublishRefreshesThePublicPageAndTheListNamesThePublisher is OP-10's
// end-to-end acceptance on PostgreSQL (the file header says what it commits):
//
//  1. a text with no visible character is refused before the database: 400, no version,
//     no legal_publish row for the operator;
//  2. the operator publishes a new privacy text: 303 to the screen; one committed version
//     whose published_by is the operator and one legal_publish row whose detail names the
//     slug and the byte length and not the text; the public page /legal/privacy (the
//     Marketing handler, the snapshot on tappa_app's pool) shows the text -- the refresh;
//  3. the version list (two committed transactions: one 'read' row with the
//     legal_versions scope) lists the version first, live, with the operator's name and
//     its byte count, and the screen does not say the public page is behind -- on
//     PostgreSQL's own types the snapshot is not OLDER than the list's live version by
//     compare's rule. That is all the assertion holds: a snapshot newer than the list
//     (its published_at a microsecond later) does not warn either, so EQUALITY of the two
//     times is not what this measures (the warning is one-directional since round 3);
//  4. undo = publish the earlier text again (the text the page showed before the run, or
//     a FAKE earlier text published first when there was none the form can carry): a NEW
//     row -- the earlier text's version count grows by one, the published text's stays --
//     the snapshot and the public page show the earlier text, and the list's first row
//     is it, live, by the operator, the row after it no longer live, with no warning;
//  5. the sign-out writes its logout row; the identity of the operator connection is
//     still tappa_operator at the end.
func TestE2E_LegalPublishRefreshesThePublicPageAndTheListNamesThePublisher(t *testing.T) {
	l := newLegalE2E(t)
	sess := l.signIn(l.f)
	publishRows := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'legal_publish' AND actor_admin_id = $1`, l.f.id)
	}
	versionsOf := func(body string) int {
		return l.ownerInt(`SELECT count(*)::int FROM legal_documents WHERE slug = 'privacy' AND body = $1`, body)
	}

	// 1. Refused before the database.
	if w := l.post("/operator/legal", url.Values{"slug": {"privacy"}, "body": {"\u200b \r\n\u2060"}}, sess); w.Code != http.StatusBadRequest {
		t.Fatalf("a text with no visible character = %d, want 400", w.Code)
	}
	if n := publishRows(); n != 0 {
		t.Fatalf("a refused text wrote %d legal_publish row(s)", n)
	}

	// 2. Publish.
	before, hadBefore := l.texts.Published()["privacy"]
	text := "FAKE OP-10 end-to-end privacy text, " + uuid.NewString() + " -- not a policy."
	w := l.post("/operator/legal", url.Values{"slug": {"privacy"}, "body": {text}}, sess)
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/legal" {
		t.Fatalf("publication = %d %q, want 303 to the screen; log: %s", w.Code, w.Result().Header.Get("Location"), l.logs.String())
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM legal_documents WHERE slug = 'privacy' AND body = $1 AND published_by = $2`, text, l.f.id); n != 1 {
		t.Fatalf("%d committed version(s) of the text published by the operator, want 1", n)
	}
	var detail string
	if err := l.owner.QueryRow(l.ctx, `SELECT detail::text FROM operator_audit_log
	                                    WHERE kind = 'legal_publish' AND actor_admin_id = $1`, l.f.id).Scan(&detail); err != nil {
		t.Fatalf("the legal_publish row: %v", err)
	}
	if !strings.Contains(detail, `"slug": "privacy"`) || !strings.Contains(detail, `"bytes": `+strconv.Itoa(len(text))) || strings.Contains(detail, text) {
		t.Errorf("the legal_publish detail is %s; want the slug and the byte length, not the text", detail)
	}
	if got := l.texts.Published()["privacy"].Body; got != text {
		t.Fatalf("the snapshot after the publication serves another text (%d bytes); the refresh did not install it", len(got))
	}
	if page := l.publicPrivacy(); !strings.Contains(page, html.EscapeString(text)) {
		t.Fatal("/legal/privacy does not show the published text")
	}

	// 3. The version list.
	reads := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log
		                   WHERE kind = 'read' AND target_scope = 'legal_versions' AND actor_admin_id = $1`, l.f.id)
	}
	rowRe := regexp.MustCompile(`(?s)<li class="op-version">(.*?)</li>`)
	page := l.get("/operator/legal", sess)
	if page.Code != http.StatusOK {
		t.Fatalf("the legal screen = %d; log: %s", page.Code, l.logs.String())
	}
	if n := reads(); n != 1 {
		t.Errorf("one screen view wrote %d 'read' row(s), want 1", n)
	}
	rows := rowRe.FindAllStringSubmatch(page.Body.String(), -1)
	if len(rows) == 0 {
		t.Fatal("the version list is empty")
	}
	first := rows[0][1]
	if !strings.Contains(first, html.EscapeString(l.name)) || !strings.Contains(first, strconv.Itoa(len(text))+" bytes") ||
		!strings.Contains(first, "Live") || !strings.Contains(first, "/legal/privacy") {
		t.Errorf("the newest version row = %q; want the operator's name, %d bytes, live", first, len(text))
	}
	if strings.Contains(page.Body.String(), behindWarning) || strings.Contains(l.logs.String(), "could not refresh") {
		t.Errorf("the screen after a publication says the public page is behind (or logged a failed refresh); log: %s", l.logs.String())
	}

	// 4. Undo: the earlier text, published again. The text the page showed before the run
	// when there was one the form can carry; otherwise a FAKE earlier text is published
	// first, and the run's text again after it.
	earlier, fromBefore := strings.TrimSpace(before.Body), hadBefore
	if fromBefore && (len(url.Values{"slug": {"privacy"}, "body": {earlier}}.Encode()) > operator.MaxLegalBodyForTest ||
		!operator.VisibleTextForTest(earlier)) {
		fromBefore = false
	}
	if !fromBefore {
		earlier = "FAKE OP-10 end-to-end earlier privacy text, " + uuid.NewString() + "."
		for _, b := range []string{earlier, text} {
			if w := l.post("/operator/legal", url.Values{"slug": {"privacy"}, "body": {b}}, sess); w.Code != http.StatusSeeOther {
				t.Fatalf("a publication before the undo = %d", w.Code)
			}
		}
	}
	earlierVersions, textVersions := versionsOf(earlier), versionsOf(text)
	if w := l.post("/operator/legal", url.Values{"slug": {"privacy"}, "body": {earlier}}, sess); w.Code != http.StatusSeeOther {
		t.Fatalf("the undo = %d", w.Code)
	}
	if got := versionsOf(earlier); got != earlierVersions+1 {
		t.Errorf("the undo left %d version(s) of the earlier text, want %d -- undoing is a new row", got, earlierVersions+1)
	}
	if got := versionsOf(text); got != textVersions {
		t.Errorf("the undo changed the published text's versions from %d to %d -- the table is append-only", textVersions, got)
	}
	if got := l.texts.Published()["privacy"].Body; got != earlier {
		t.Errorf("the snapshot after the undo serves another text (%d bytes, want %d)", len(got), len(earlier))
	}
	if paras := legal.Paragraphs(earlier); len(paras) == 0 || !strings.Contains(l.publicPrivacy(), html.EscapeString(paras[0])) {
		t.Error("/legal/privacy does not show the earlier text after the undo")
	}
	page = l.get("/operator/legal", sess)
	rows = rowRe.FindAllStringSubmatch(page.Body.String(), -1)
	if page.Code != http.StatusOK || len(rows) < 2 {
		t.Fatalf("the legal screen after the undo = %d with %d row(s)", page.Code, len(rows))
	}
	if r := rows[0][1]; !strings.Contains(r, strconv.Itoa(len(earlier))+" bytes") || !strings.Contains(r, "Live") ||
		!strings.Contains(r, html.EscapeString(l.name)) {
		t.Errorf("after the undo the newest row = %q; want the earlier text's %d bytes, live, by the operator", r, len(earlier))
	}
	if r := rows[1][1]; strings.Contains(r, "Live") {
		t.Errorf("after the undo the published version is still live: %q", r)
	}
	if strings.Contains(page.Body.String(), behindWarning) {
		t.Error("the screen after the undo says the public page is behind")
	}
	if n := reads(); n != 2 {
		t.Errorf("two screen views wrote %d 'read' row(s), want 2", n)
	}
	// Each view was its own two-phase read: two tickets, both consumed (a ticket is used
	// once, inside internal/db; the screen's LegalStore carries none).
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
	                    WHERE s.admin_id = $1 AND k.consumed_at IS NOT NULL`, l.f.id); n != 2 {
		t.Errorf("%d consumed read ticket(s) after two views, want 2", n)
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
	                    WHERE s.admin_id = $1 AND k.consumed_at IS NULL`, l.f.id); n != 0 {
		t.Errorf("%d unconsumed read ticket(s) after two views, want 0", n)
	}

	// 5. Sign out; the operator connection's identity at the end.
	if w := l.post("/operator/logout", nil, sess); w.Code != http.StatusSeeOther {
		t.Fatalf("sign-out = %d", w.Code)
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'logout' AND actor_admin_id = $1`, l.f.id); n != 1 {
		t.Errorf("the sign-out wrote %d logout row(s)", n)
	}
	if err := asOperator(l.ctx, l.op); err != nil {
		t.Errorf("harness at the end: %v", err)
	}
}

// behindWarning is the heading the legal screen shows when the snapshot is behind the
// version list (legal.templ).
const behindWarning = "The public page is behind"

// plantSession commits a session row for the operator under a fresh cookie value (the
// Authenticator's hash of it), shaped by extra -- a SQL fragment of SET clauses applied
// after the insert -- and returns the cookie.
func (l *legalE2E) plantSession(mfa bool, set string) *http.Cookie {
	l.t.Helper()
	tok := base64.RawURLEncoding.EncodeToString(randBytes(l.t, 32))
	m := hmac.New(sha256.New, l.tokenKey)
	_, _ = m.Write([]byte(tok))
	hash := hex.EncodeToString(m.Sum(nil))
	var verified any
	if mfa {
		verified = time.Now()
	}
	var id uuid.UUID
	if err := l.owner.QueryRow(l.ctx, `INSERT INTO platform_sessions (admin_id, token_hash, mfa_verified_at)
	                                    VALUES ($1, $2, CASE WHEN $3::timestamptz IS NULL THEN NULL ELSE clock_timestamp() END)
	                                    RETURNING id`, l.f.id, hash, verified).Scan(&id); err != nil {
		l.t.Fatalf("plant a session: %v", err)
	}
	if set != "" {
		if _, err := l.owner.Exec(l.ctx, `UPDATE platform_sessions SET `+set+` WHERE id = $1`, id); err != nil {
			l.t.Fatalf("shape the planted session: %v", err)
		}
	}
	return &http.Cookie{Name: operatorauth.SessionCookieName, Value: tok}
}

// TestE2E_LegalPublishRefusesDeadSessionsAndBadTextsWritingNothing (escape attempts
// against PostgreSQL): a publication sent with a session the predicate refuses -- one
// never MFA-stamped, a revoked one, one idle past 30 minutes, one past its 8 hours --
// is the sign-in's 303 (the session gate); then, signed in, a text with a NUL byte
// (PostgreSQL refuses it: 503, the fixed page -- which does not claim nothing was written,
// since an error does not prove that, and sends the operator to the version list), a text
// over 256 KiB (413), a document
// that does not exist (400) and a text with no visible character (400). After all of
// them: no version published_by the operator and no legal_publish row for it; the
// process log does not carry the NUL text. CONTROL: the planted shapes are what the
// predicate refuses -- a planted MFA-stamped live session reaches the screen (200).
func TestE2E_LegalPublishRefusesDeadSessionsAndBadTextsWritingNothing(t *testing.T) {
	l := newLegalE2E(t)
	form := func(slug, body string) url.Values { return url.Values{"slug": {slug}, "body": {body}} }
	for name, c := range map[string]*http.Cookie{
		"never MFA-stamped": l.plantSession(false, ""),
		"revoked":           l.plantSession(true, "revoked_at = clock_timestamp()"),
		"idle 31 minutes":   l.plantSession(true, "last_used_at = clock_timestamp() - interval '31 minutes'"),
		"older than 8 h": l.plantSession(true, "created_at = clock_timestamp() - interval '9 hours', "+
			"last_used_at = clock_timestamp() - interval '1 minute', mfa_verified_at = clock_timestamp() - interval '9 hours'"),
	} {
		w := l.post("/operator/legal", form("privacy", "FAKE text from a dead session"), c)
		if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/login" {
			t.Errorf("%s: publication = %d %q, want the sign-in's 303", name, w.Code, w.Result().Header.Get("Location"))
		}
	}
	if w := l.get("/operator/legal", l.plantSession(true, "")); w.Code != http.StatusOK {
		t.Fatalf("CONTROL: a planted live MFA-stamped session = %d on the screen, want 200", w.Code)
	}
	sess := l.signIn(l.f)
	const nulText = "FAKE text with a NUL \x00 byte, OP-10B"
	for _, c := range []struct {
		name string
		form url.Values
		want int
		says []string
	}{
		{"a NUL byte", form("privacy", nulText), http.StatusServiceUnavailable,
			[]string{"The publication was not confirmed", "Check the version list", `href="/operator/legal"`}},
		{"over 256 KiB", form("privacy", strings.Repeat("y", operator.MaxLegalBodyForTest)), http.StatusRequestEntityTooLarge,
			[]string{"Nothing was published"}},
		{"a document that does not exist", form("PRIVACY", "FAKE text"), http.StatusBadRequest, []string{"Nothing was published"}},
		{"no visible character", form("privacy", " \u200b\u3164 "), http.StatusBadRequest, []string{"Nothing was published"}},
	} {
		w := l.post("/operator/legal", c.form, sess)
		if w.Code != c.want {
			t.Errorf("%s = %d, want %d", c.name, w.Code, c.want)
		}
		for _, s := range c.says {
			if !strings.Contains(w.Body.String(), s) {
				t.Errorf("%s: the page does not say %q", c.name, s)
			}
		}
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM legal_documents WHERE published_by = $1`, l.f.id); n != 0 {
		t.Errorf("%d version(s) published_by the operator after refusals only", n)
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'legal_publish' AND actor_admin_id = $1`, l.f.id); n != 0 {
		t.Errorf("%d legal_publish row(s) after refusals only", n)
	}
	if strings.Contains(l.logs.String(), "FAKE text with a NUL") || !strings.Contains(l.logs.String(), "a legal text could not be published") {
		t.Errorf("the process log carries the refused text, or no line for the database's refusal")
	}
	if w := l.post("/operator/logout", nil, sess); w.Code != http.StatusSeeOther {
		t.Fatalf("sign-out = %d", w.Code)
	}
}
