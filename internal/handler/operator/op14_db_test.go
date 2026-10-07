package operator_test

// op14_db_test.go -- M10 OP-14 phase B end to end against PostgreSQL: the operator signs in
// through the surface and reads its own audit log through op_begin_read + op_read_audit
// (00031), on newLegalE2E's committed rig (op10_db_test.go: one owner connection switched to
// tappa_operator, each statement its own committed transaction -- a read's second phase
// refuses a ticket whose transaction has not committed); and the screen's words are held to
// the database's closed sets, read from its catalog.
//
// WHAT ONE RUN LEAVES BEHIND (the class op10_db_test.go's header counts; append-only tables
// and the foreign keys that point at them), by construction: three operator accounts --
// op10b-… (newLegalE2E's; disabled at cleanup) and two this test commits, one pending and
// one disabled (both disabled at cleanup) --, the first one's five sessions (the signed-in
// one and four planted; revoked at cleanup), and the audit rows: the sign-in's password_ok
// (OP-14 C's writer; the two refused attempts write none), login, logout, two unknown_email
// rows naming the pending and the disabled account, one 'read' row of scope tenants (the
// search) and five of scope operator_audit (four views and the planted live session's).
// Read tickets are deleted at cleanup. No tenant row is written.

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"html"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
)

// TestE2E_AuditScreenReadsTheLogThroughTheDefinerAndAuditsEachView is OP-14's end-to-end
// acceptance on PostgreSQL (the file header says what it commits):
//
//  1. a tenant search by a random word (class text), and a sign-in attempt with the address
//     of a pending and of a disabled account this test commits -- the log's raw material;
//  2. the first page (GET): 200; the view's own 'read' row is on it -- scope this audit log,
//     every kind, page 1 of 50, the viewing session's first eight digits -- and every row
//     above it is dated no earlier (ADR 0021 limits L1, L2: the screen does not assume it is
//     first); its time is the committed row's own, to the second, in UTC; exactly one
//     committed 'read' row for the view, scope operator_audit, page 1 of 50, detail
//     {"filter": "all"};
//  3. filtered to login: every row is a sign-in, and one is this operator's -- by its name,
//     under its session; the view's row records {"filter": "login"};
//  4. filtered to read: every row is a read, and one is the search of step 1 -- under the
//     session, of the tenant list, searched by name -- while the WORD is on no audit page,
//     in no audit row of the operator and in no log line;
//  5. filtered to unknown_email: the rows of step 1 name the pending and the disabled
//     account by their names (ADR 0021 limit L7: the definer reads every status);
//  6. on none of the pages: the session's cookie value, its hash, its whole id, the TOTP
//     code of the sign-in, or any of the three accounts' addresses;
//  7. sessions the predicate refuses -- never MFA-stamped, revoked, idle 31 minutes -- get
//     the sign-in's 303 on a GET and a POST and leave no row; CONTROL: a planted live
//     session reads the log;
//  8. one committed 'read' row per view and one consumed ticket per 'read' row, of kind
//     operator_audit for each view; none left unconsumed; the sign-out's row.
func TestE2E_AuditScreenReadsTheLogThroughTheDefinerAndAuditsEachView(t *testing.T) {
	l := newLegalE2E(t)
	sess := l.signIn(l.f)
	code := totpAt(l.f.key, l.now)
	m := hmac.New(sha256.New, l.tokenKey)
	_, _ = m.Write([]byte(sess.Value))
	hash := hex.EncodeToString(m.Sum(nil))
	var session uuid.UUID
	if err := l.owner.QueryRow(l.ctx, `SELECT id FROM platform_sessions WHERE admin_id = $1 AND token_hash = $2`, l.f.id, hash).
		Scan(&session); err != nil {
		t.Fatalf("the signed-in session: %v", err)
	}
	prefix := `<span class="text-sm">Session <span class="font-mono">` + session.String()[:8] + `</span></span>`
	auditReads := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1
		                   AND target_scope = 'operator_audit'`, l.f.id)
	}
	lastAuditRead := func() (detail string, page, size *int) {
		l.t.Helper()
		if err := l.owner.QueryRow(l.ctx, `SELECT detail::text, page_number, page_size FROM operator_audit_log
		                                    WHERE kind = 'read' AND actor_admin_id = $1 AND target_scope = 'operator_audit'
		                                    ORDER BY at DESC, id DESC LIMIT 1`, l.f.id).Scan(&detail, &page, &size); err != nil {
			l.t.Fatalf("the last operator_audit 'read' row: %v", err)
		}
		return
	}
	var pages []string
	view := func(what string, w interface {
		Result() *http.Response
	}, body string) []string {
		l.t.Helper()
		if w.Result().StatusCode != http.StatusOK {
			l.t.Fatalf("%s = %d; log: %s", what, w.Result().StatusCode, l.logs.String())
		}
		pages = append(pages, body)
		var rows []string
		for _, r := range auditRowRe.FindAllStringSubmatch(body, -1) {
			rows = append(rows, r[1])
		}
		return rows
	}

	// 1. The raw material.
	word := "op14b-e2e-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	if w := l.post("/operator/tenants", url.Values{"q": {word}}, sess); w.Code != http.StatusOK {
		t.Fatalf("the search = %d", w.Code)
	}
	type other struct {
		id          uuid.UUID
		email, name string
	}
	pending, disabled := other{id: uuid.New()}, other{id: uuid.New()}
	pending.email, pending.name = "op14b-p-"+pending.id.String()[:12]+"@example.test", "op14b pending "+pending.id.String()[:8]
	disabled.email, disabled.name = "op14b-d-"+disabled.id.String()[:12]+"@example.test", "op14b disabled "+disabled.id.String()[:8]
	tokenHash := hex.EncodeToString(randBytes(t, 32))
	if _, err := l.owner.Exec(l.ctx, `WITH c AS (SELECT clock_timestamp() AS now)
	        INSERT INTO platform_admins (id, email, display_name, status, enroll_token_hash, enroll_issued_at, enroll_expires_at)
	        SELECT $1, $2, $3, 'pending', $4, c.now, c.now + interval '30 minutes' FROM c`,
		pending.id, pending.email, pending.name, tokenHash); err != nil {
		t.Fatalf("commit the pending account: %v", err)
	}
	if _, err := l.owner.Exec(l.ctx, `INSERT INTO platform_admins (id, email, display_name, status) VALUES ($1, $2, $3, 'disabled')`,
		disabled.id, disabled.email, disabled.name); err != nil {
		t.Fatalf("commit the disabled account: %v", err)
	}
	t.Cleanup(func() {
		if _, err := l.owner.Exec(context.Background(), `UPDATE platform_admins SET status = 'disabled' WHERE id = ANY($1)`,
			[]uuid.UUID{pending.id, disabled.id}); err != nil {
			t.Errorf("cleanup: disable the two accounts: %v", err)
		}
	})
	for _, o := range []other{pending, disabled} {
		if w := l.post("/operator/login", url.Values{"email": {o.email}, "password": {"op14b not the password"}}); w.Code != http.StatusUnauthorized {
			t.Fatalf("a sign-in with an inactive account's address = %d, want 401", w.Code)
		}
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'unknown_email' AND target_admin_id = ANY($1)`,
		[]uuid.UUID{pending.id, disabled.id}); n != 2 {
		t.Fatalf("PREMISE: %d unknown_email row(s) name the two accounts, want 2", n)
	}

	// 2. The first page.
	before := auditReads()
	w := l.get("/operator/audit", sess)
	rows := view("the first page", w, w.Body.String())
	if n := auditReads() - before; n != 1 {
		t.Errorf("the first page wrote %d operator_audit 'read' row(s), want 1", n)
	}
	if detail, page, size := lastAuditRead(); detail != `{"filter": "all"}` || page == nil || *page != 1 || size == nil || *size != 50 {
		t.Errorf("the first page's row: detail %s, page %v/%v; want all, 1/50", detail, page, size)
	}
	var ownAt string
	if err := l.owner.QueryRow(l.ctx, `SELECT to_char(at AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:MI:SS') || ' UTC' FROM operator_audit_log
	                                    WHERE kind = 'read' AND session_id = $1 AND target_scope = 'operator_audit'
	                                    ORDER BY at DESC, id DESC LIMIT 1`, session).Scan(&ownAt); err != nil {
		t.Fatalf("the view's own row: %v", err)
	}
	timeRe := regexp.MustCompile(`^\s*<span class="font-mono font-bold">([^<]*)</span>`)
	own := -1
	for i, r := range rows {
		if strings.Contains(r, prefix) && strings.Contains(r, `<span class="text-sm">Of this audit log</span>`) &&
			strings.Contains(r, `<span class="text-sm">Filter: Every kind</span>`) {
			own = i
			break
		}
	}
	if own < 0 {
		t.Fatalf("the first page (%d rows) does not list the view's own 'read' row", len(rows))
	}
	if m := timeRe.FindStringSubmatch(rows[own]); m == nil || m[1] != ownAt {
		t.Errorf("the view's own row is dated %v on the page, %s in the log", m, ownAt)
	}
	for i := 0; i < own; i++ {
		if m := timeRe.FindStringSubmatch(rows[i]); m == nil || m[1] < ownAt {
			t.Errorf("row %d above the view's own row is dated earlier (%v < %s)", i+1, m, ownAt)
		}
	}
	t.Logf("the view's own row is row %d of %d on the first page", own+1, len(rows))

	// 3. Filtered to login.
	w = l.post("/operator/audit", auditForm("login", ""), sess)
	rows = view("the login page", w, w.Body.String())
	found := false
	for _, r := range rows {
		if !strings.Contains(r, `<span class="font-display font-bold">Signed in</span>`) {
			t.Errorf("the login filter lists a row that is not a sign-in")
		}
		found = found || (strings.Contains(r, prefix) && strings.Contains(r, `By <bdi>`+html.EscapeString(l.name)+`</bdi>`))
	}
	if !found {
		t.Error("the login filter does not list this operator's sign-in by its name and session")
	}
	if detail, _, _ := lastAuditRead(); detail != `{"filter": "login"}` {
		t.Errorf("the login view's row records %s", detail)
	}

	// 4. Filtered to read.
	w = l.post("/operator/audit", auditForm("read", ""), sess)
	rows = view("the read page", w, w.Body.String())
	found = false
	for _, r := range rows {
		if !strings.Contains(r, `<span class="font-display font-bold">Read</span>`) {
			t.Errorf("the read filter lists a row that is not a read")
		}
		found = found || (strings.Contains(r, prefix) && strings.Contains(r, `<span class="text-sm">Of the tenant list</span>`) &&
			strings.Contains(r, `<span class="text-sm">Searched by name</span>`))
	}
	if !found {
		t.Error("the read filter does not list the search under its session, its scope and its class")
	}

	// 5. Filtered to unknown_email: the pending and the disabled account by name.
	w = l.post("/operator/audit", auditForm("unknown_email", ""), sess)
	body := w.Body.String()
	view("the unknown_email page", w, body)
	for _, o := range []other{pending, disabled} {
		if !strings.Contains(body, `<span class="text-sm">Account <bdi>`+html.EscapeString(o.name)+`</bdi></span>`) {
			t.Errorf("the unknown_email filter does not name the account %q", o.name)
		}
	}

	// 6. What is on none of the pages.
	for i, p := range pages {
		for what, needle := range map[string]string{"the cookie": sess.Value, "the session hash": hash, "the whole session id": session.String(),
			"the TOTP code": code, "the search word": word, "the operator's address": l.f.email, "the pending address": pending.email,
			"the disabled address": disabled.email} {
			if strings.Contains(p, needle) {
				t.Errorf("view %d carries %s", i+1, what)
			}
		}
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_audit_log a WHERE a.actor_admin_id = $1
	                    AND strpos(lower(row_to_json(a)::text), lower($2)) > 0`, l.f.id, word); n != 0 {
		t.Errorf("the search word is in %d of the operator's audit rows", n)
	}
	if strings.Contains(strings.ToLower(l.logs.String()), strings.ToLower(word)) {
		t.Error("the search word is in the process or access log")
	}

	// 7. Dead sessions.
	n := l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1`, l.f.id)
	for name, c := range map[string]*http.Cookie{
		"never MFA-stamped": l.plantSession(false, ""),
		"revoked":           l.plantSession(true, "revoked_at = clock_timestamp()"),
		"idle 31 minutes":   l.plantSession(true, "last_used_at = clock_timestamp() - interval '31 minutes'"),
	} {
		for what, res := range map[string]*http.Response{
			"GET":  l.get("/operator/audit", c).Result(),
			"POST": l.post("/operator/audit", auditForm("read", ""), c).Result(),
		} {
			if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/operator/login" {
				t.Errorf("%s session: %s = %d %q, want the sign-in's 303", name, what, res.StatusCode, res.Header.Get("Location"))
			}
		}
	}
	if got := l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1`, l.f.id); got != n {
		t.Errorf("the dead sessions wrote %d 'read' row(s)", got-n)
	}
	if w := l.get("/operator/audit", l.plantSession(true, "")); w.Code != http.StatusOK {
		t.Fatalf("CONTROL: a planted live MFA-stamped session = %d on the log, want 200", w.Code)
	}

	// 8. One row per view, one ticket per row; sign out.
	if got := auditReads() - before; got != 5 {
		t.Errorf("the log's five views (four and the planted session's) wrote %d operator_audit 'read' row(s)", got)
	}
	allReads := l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1`, l.f.id)
	if got := l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
	                      WHERE s.admin_id = $1 AND k.consumed_at IS NULL`, l.f.id); got != 0 {
		t.Errorf("%d unconsumed read ticket(s), want 0", got)
	}
	if got := l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
	                      WHERE s.admin_id = $1 AND k.consumed_at IS NOT NULL`, l.f.id); got != allReads {
		t.Errorf("%d consumed read ticket(s), want one per 'read' row (%d)", got, allReads)
	}
	if got := l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
	                      WHERE s.admin_id = $1 AND k.kind = 'operator_audit'`, l.f.id); got != auditReads() {
		t.Errorf("%d operator_audit ticket(s), want one per operator_audit 'read' row (%d)", got, auditReads())
	}
	if w := l.post("/operator/logout", nil, sess); w.Code != http.StatusSeeOther {
		t.Fatalf("sign-out = %d", w.Code)
	}
	if err := asOperator(l.ctx, l.op); err != nil {
		t.Errorf("harness at the end: %v", err)
	}
}

// TestE2E_AuditWordsCoverEveryScopeAndSearchClassTheDatabaseReturns holds the audit screen's
// closed maps to the database's own closed sets, read from the catalog (one read-only owner
// transaction, rolled back):
//
//   - the scopes: the words' keys equal the kinds of operator_read_tickets_kind_check and the
//     scope list op_read_audit returns (its `target_scope IN (...)` CASE) -- so a read kind
//     a later migration adds is red here until the screen has a word for it (00032's
//     tenant_billing has had one since the 3rd round; the half without a database is
//     TestAuditWords_NameEveryScopeTheNewestMigrationReturns);
//   - the search classes: the words' keys equal op_read_audit's list for a 'tenants' row;
//   - the audit kinds: the words' keys equal the kinds of operator_audit_log_kind_check (the
//     go/types half is TestAuditWords_NameEveryKindAndNothingElse).
//
// CONTROL: each list read from the catalog is not empty and holds a known member.
func TestE2E_AuditWordsCoverEveryScopeAndSearchClassTheDatabaseReturns(t *testing.T) {
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the catalog is PostgreSQL's (CLAUDE.md §8)")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()
	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	read := func(sql string) string {
		t.Helper()
		var s string
		if err := tx.QueryRow(ctx, sql).Scan(&s); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return s
	}
	quoted := regexp.MustCompile(`'([a-z_]+)'`)
	words := func(text string) []string {
		var out []string
		for _, m := range quoted.FindAllStringSubmatch(text, -1) {
			if !slices.Contains(out, m[1]) {
				out = append(out, m[1])
			}
		}
		slices.Sort(out)
		return out
	}
	keys := func(m map[string]string) []string {
		var out []string
		for k := range m {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	ticketKinds := words(read(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'operator_read_tickets_kind_check'`))
	auditKinds := words(read(`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'operator_audit_log_kind_check'`))
	src := read(`SELECT prosrc FROM pg_proc WHERE proname = 'op_read_audit' AND pronamespace = 'public'::regnamespace`)
	scopeList := regexp.MustCompile(`CASE WHEN s\.target_scope IN \(([^)]*)\)`).FindStringSubmatch(src)
	searchList := regexp.MustCompile(`\(p\.detail ->> 'search'\) IN \(([^)]*)\)`).FindStringSubmatch(src)
	if scopeList == nil || searchList == nil || !slices.Contains(ticketKinds, "operator_audit") || !slices.Contains(auditKinds, "password_ok") {
		t.Fatalf("CONTROL: the catalog's lists were not read (ticket kinds %v, audit kinds %v, scope list %v, search list %v)",
			ticketKinds, auditKinds, scopeList != nil, searchList != nil)
	}
	scopes, searches := words(scopeList[1]), words(searchList[1])
	if got := keys(operator.AuditScopeWordsForTest()); !slices.Equal(got, ticketKinds) || !slices.Equal(got, scopes) {
		t.Errorf("the screen's scope words name %v; the ticket CHECK names %v and op_read_audit returns %v", got, ticketKinds, scopes)
	}
	if got := keys(operator.AuditSearchWordsForTest()); !slices.Equal(got, searches) {
		t.Errorf("the screen's search words name %v; op_read_audit reads %v", got, searches)
	}
	var kinds []string
	for k := range operator.AuditKindWordsForTest() {
		kinds = append(kinds, string(k))
	}
	slices.Sort(kinds)
	if !slices.Equal(kinds, auditKinds) {
		t.Errorf("the screen's kind words name %v; the audit kind CHECK names %v", kinds, auditKinds)
	}
	var listed []string
	for _, k := range db.OperatorAuditKinds() {
		listed = append(listed, string(k))
	}
	slices.Sort(listed)
	if !slices.Equal(listed, auditKinds) {
		t.Errorf("PREMISE: db.OperatorAuditKinds is %v, the CHECK %v (internal/db's own pin is red too)", listed, auditKinds)
	}
}
