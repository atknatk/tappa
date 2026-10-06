package operator_test

// op11_db_test.go -- M10 OP-11 phase B end to end against PostgreSQL: the operator signs
// in through the surface and reads the tenant list, three searches and one tenant's
// overview through op_begin_read + op_read_tenants / op_read_tenant_detail (00029), on
// newLegalE2E's committed rig (op10_db_test.go: one owner connection switched to
// tappa_operator, each statement its own committed transaction -- a read's second phase
// refuses a ticket whose transaction has not committed).
//
// WHICH TENANT. No tenant row is written here. The overview reads the demo seed's Kebab
// Manufacturing tenant (test/fixtures/seed.sql; CI loads the seed) and, on a database
// without it, the oldest tenant there is; a database with no tenant at all skips the test.
//
// WHAT ONE RUN LEAVES BEHIND (the class op10_db_test.go's header counts; append-only
// tables and the foreign keys that point at them), measured around one run on the
// development database (2026-10-03, owner connection, read-only transaction): one operator
// account (op10b-…, newLegalE2E's; disabled at cleanup), its five sessions (the signed-in
// one and four planted; revoked at cleanup), and nine audit rows -- login, logout and seven
// 'read' rows: five of scope tenants (the list, three searches, the planted live session's
// list) and two of tenant_detail (the overview, the unknown id); the overview adds one
// more per retry when another test changes the tenant's counts mid-read. Read tickets are
// deleted at cleanup; no tenant row is written.

import (
	"errors"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The seed's Kebab Manufacturing Co. Ltd. (test/fixtures/seed.sql).
const seedKM = "20000000-0000-4000-8000-000000000001"

// TestE2E_TenantScreensReadThroughTheDefinersAndAuditEachRead is OP-11's end-to-end
// acceptance on PostgreSQL (the file header says what it commits):
//
//  1. the list: 200, at most a page of rows, each linking an overview by its canonical
//     path; one committed 'read' row, scope tenants, page 1 of 50, detail {"search": "none"};
//  2. three searches, each a POST: the tenant's id in capitals finds exactly it (class id);
//     a random word finds nothing and says so (class text); a random address finds nothing
//     (class address) -- three more 'read' rows with those classes, and NONE of the three
//     terms in any audit row of this operator, in any read ticket of its sessions, or in
//     the process or access log;
//  3. the overview: 200; the banner carries the tenant's name as the owner reads it,
//     escaped; the four counts equal the owner's own counts of the tenant's locations and
//     of its active employees, plaques and panel accounts (read before and after the page;
//     retried while another test changes them); exactly one 'read' row naming the tenant
//     per view (a retry is a view and has its own -- OP-13 B's 2nd round, the sibling of its
//     B1: until then 1 to 3 rows were accepted, so a view that read twice passed);
//  4. an id no tenant has: 404 with its page, and a committed 'read' row naming that id;
//     a malformed id: 404 and no row;
//  5. sessions the predicate refuses -- never MFA-stamped, revoked, idle 31 minutes -- get
//     the sign-in's 303 on the list, a search and an overview, and leave no row; CONTROL: a
//     planted live session reads the list;
//  6. every ticket of the operator's sessions consumed, none left; the sign-out's row.
func TestE2E_TenantScreensReadThroughTheDefinersAndAuditEachRead(t *testing.T) {
	l := newLegalE2E(t)
	var tenant uuid.UUID
	var name string
	err := l.owner.QueryRow(l.ctx, `SELECT id, name FROM tenants WHERE id = $1`, seedKM).Scan(&tenant, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		err = l.owner.QueryRow(l.ctx, `SELECT id, name FROM tenants ORDER BY created_at, id LIMIT 1`).Scan(&tenant, &name)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("the database holds no tenant; the overview has nothing to read")
	}
	if err != nil {
		t.Fatalf("pick a tenant: %v", err)
	}
	sess := l.signIn(l.f)
	reads := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1
		                   AND target_scope IN ('tenants', 'tenant_detail')`, l.f.id)
	}
	lastRead := func() (scope, detail string, page, size *int, target *uuid.UUID) {
		l.t.Helper()
		if err := l.owner.QueryRow(l.ctx, `SELECT target_scope, detail::text, page_number, page_size, target_tenant_id
		                                    FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1
		                                    ORDER BY at DESC, id DESC LIMIT 1`, l.f.id).
			Scan(&scope, &detail, &page, &size, &target); err != nil {
			l.t.Fatalf("the last 'read' row: %v", err)
		}
		return
	}
	rowRe := regexp.MustCompile(`(?s)<li class="op-row">(.*?)</li>`)
	linkRe := regexp.MustCompile(`href="/operator/tenants/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})"`)

	// 1. The list.
	w := l.get("/operator/tenants", sess)
	rows := rowRe.FindAllStringSubmatch(w.Body.String(), -1)
	if w.Code != http.StatusOK || len(rows) == 0 || len(rows) > 50 {
		t.Fatalf("the list = %d with %d row(s); log: %s", w.Code, len(rows), l.logs.String())
	}
	for _, r := range rows {
		if !linkRe.MatchString(r[1]) {
			t.Errorf("a list row does not link an overview by its canonical path: %q", r[1])
		}
	}
	if n := reads(); n != 1 {
		t.Errorf("the list wrote %d 'read' row(s), want 1", n)
	}
	if scope, detail, page, size, _ := lastRead(); scope != "tenants" || detail != `{"search": "none"}` ||
		page == nil || *page != 1 || size == nil || *size != 50 {
		t.Errorf("the list's row: scope %s, detail %s, page %v/%v; want tenants, none, 1/50", scope, detail, page, size)
	}

	// 2. Three searches.
	word := "op11b-e2e-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16]
	address := word + "@example.test"
	for _, c := range []struct {
		term, class string
		found       []uuid.UUID
	}{
		{strings.ToUpper(tenant.String()), "id", []uuid.UUID{tenant}},
		{word, "text", nil},
		{address, "address", nil},
	} {
		before := reads()
		w := l.post("/operator/tenants", url.Values{"q": {c.term}}, sess)
		body := w.Body.String()
		var ids []uuid.UUID
		for _, r := range rowRe.FindAllStringSubmatch(body, -1) {
			if m := linkRe.FindStringSubmatch(r[1]); m != nil {
				ids = append(ids, uuid.MustParse(m[1]))
			}
		}
		if w.Code != http.StatusOK || len(ids) != len(c.found) || (len(ids) == 1 && ids[0] != c.found[0]) {
			t.Errorf("a %s search = %d, found %v; want %v", c.class, w.Code, ids, c.found)
		}
		if len(c.found) == 0 && !strings.Contains(body, "No tenant matches that search.") {
			t.Errorf("a %s search that found nothing does not say so", c.class)
		}
		if !strings.Contains(body, `name="q" value="`+html.EscapeString(c.term)+`"`) {
			t.Errorf("a %s search's page does not carry its term in the search box", c.class)
		}
		if n := reads(); n != before+1 {
			t.Errorf("a %s search wrote %d 'read' row(s), want 1", c.class, n-before)
		}
		if scope, detail, page, size, _ := lastRead(); scope != "tenants" || detail != `{"search": "`+c.class+`"}` ||
			page == nil || *page != 1 || size == nil || *size != 50 {
			t.Errorf("a %s search's row: scope %s, detail %s, page %v/%v", c.class, scope, detail, page, size)
		}
	}
	// The word and the address are in no audit row of the operator, no read ticket of its
	// sessions and no log line -- nor is the word inside the address (its first part).
	// (The id search's term is the tenant's id, which the overview's row below names by
	// design; the search's own row names no tenant, checked here.)
	for _, term := range []string{word, address} {
		for what, n := range map[string]int{
			"audit rows": l.ownerInt(`SELECT count(*)::int FROM operator_audit_log a WHERE a.actor_admin_id = $1
			                          AND strpos(lower(row_to_json(a)::text), lower($2)) > 0`, l.f.id, term),
			"read tickets": l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
			                            WHERE s.admin_id = $1 AND strpos(lower(row_to_json(k)::text), lower($2)) > 0`, l.f.id, term),
		} {
			if n != 0 {
				t.Errorf("a search term (%d characters) is in %d of the operator's %s", len(term), n, what)
			}
		}
		if strings.Contains(strings.ToLower(l.logs.String()), strings.ToLower(term)) {
			t.Errorf("a search term (%d characters) is in the process or access log", len(term))
		}
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE actor_admin_id = $1 AND target_scope = 'tenants'
	                    AND target_tenant_id IS NOT NULL`, l.f.id); n != 0 {
		t.Errorf("%d search row(s) name a tenant", n)
	}

	// 3. The overview.
	counts := func() [4]int {
		var c [4]int
		if err := l.owner.QueryRow(l.ctx, `SELECT
		        (SELECT count(*)::int FROM locations WHERE tenant_id = $1),
		        (SELECT count(*)::int FROM employees WHERE tenant_id = $1 AND status = 'active'),
		        (SELECT count(*)::int FROM tags WHERE tenant_id = $1 AND status = 'active'),
		        (SELECT count(*)::int FROM admin_users WHERE tenant_id = $1 AND status = 'active')`, tenant).
			Scan(&c[0], &c[1], &c[2], &c[3]); err != nil {
			l.t.Fatalf("the owner's counts: %v", err)
		}
		return c
	}
	figure := func(body, label string) int {
		m := regexp.MustCompile(`<dt class="docket-label">` + label + `</dt>\s*<dd class="mt-1 font-mono text-3xl font-bold">(\d+)</dd>`).
			FindStringSubmatch(body)
		if m == nil {
			l.t.Fatalf("the overview has no %s figure", label)
		}
		n, _ := strconv.Atoi(m[1])
		return n
	}
	detailsBefore := reads()
	var page string
	var settled bool
	views := 0
	for attempt := 0; attempt < 3 && !settled; attempt++ {
		before := counts()
		views++
		w := l.get("/operator/tenants/"+tenant.String(), sess)
		if w.Code != http.StatusOK {
			t.Fatalf("the overview = %d; log: %s", w.Code, l.logs.String())
		}
		page = w.Body.String()
		got := [4]int{figure(page, "Locations"), figure(page, "Active employees"), figure(page, "Active plaques"),
			figure(page, "Active panel accounts")}
		if after := counts(); before == after {
			settled = true
			if got != before {
				t.Errorf("the overview's counts %v, the owner's %v", got, before)
			}
		}
	}
	if !settled {
		t.Fatal("the tenant's counts changed under every one of three attempts; another test is writing to it")
	}
	banner := strings.Index(page, `<section class="op-tenant" aria-label="Tenant">`)
	if banner < 0 || !strings.Contains(page[banner:], html.EscapeString(name)) ||
		!strings.Contains(page, "<title>Tenant overview — "+html.EscapeString(name)+" — Taptime operator</title>") {
		t.Errorf("the overview's banner or title does not carry the tenant's name")
	}
	if n := reads() - detailsBefore; n != views {
		t.Errorf("the overview's %d view(s) wrote %d 'read' row(s), want exactly one per view", views, n)
	}
	if scope, detail, page, size, target := lastRead(); scope != "tenant_detail" || detail != `{}` || page != nil || size != nil ||
		target == nil || *target != tenant {
		t.Errorf("the overview's row: scope %s, detail %s, page %v/%v, target %v; want tenant_detail, {}, none, the tenant", scope, detail, page, size, target)
	}

	// 4. An id no tenant has; a malformed id.
	unknown := uuid.New()
	before := reads()
	if w := l.get("/operator/tenants/"+unknown.String(), sess); w.Code != http.StatusNotFound ||
		!strings.Contains(w.Body.String(), "There is no tenant with that id") {
		t.Errorf("an id no tenant has = %d, want 404 and its page", w.Code)
	}
	if n := reads(); n != before+1 {
		t.Errorf("an id no tenant has wrote %d 'read' row(s), want 1", n-before)
	}
	if _, _, _, _, target := lastRead(); target == nil || *target != unknown {
		t.Errorf("the unknown id's row names %v, want %v", target, unknown)
	}
	if w := l.get("/operator/tenants/"+strings.ReplaceAll(unknown.String(), "-", ""), sess); w.Code != http.StatusNotFound ||
		!strings.Contains(w.Body.String(), "That link does not name a tenant") {
		t.Errorf("a malformed id = %d, want 404 and its page", w.Code)
	}
	if n := reads(); n != before+1 {
		t.Errorf("a malformed id wrote %d 'read' row(s), want 0", n-before-1)
	}

	// 5. Dead sessions.
	before = reads()
	for name, c := range map[string]*http.Cookie{
		"never MFA-stamped": l.plantSession(false, ""),
		"revoked":           l.plantSession(true, "revoked_at = clock_timestamp()"),
		"idle 31 minutes":   l.plantSession(true, "last_used_at = clock_timestamp() - interval '31 minutes'"),
	} {
		for what, w := range map[string]interface{ Result() *http.Response }{
			"the list":    l.get("/operator/tenants", c),
			"a search":    l.post("/operator/tenants", url.Values{"q": {word}}, c),
			"an overview": l.get("/operator/tenants/"+tenant.String(), c),
		} {
			if res := w.Result(); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/operator/login" {
				t.Errorf("%s session: %s = %d %q, want the sign-in's 303", name, what, res.StatusCode, res.Header.Get("Location"))
			}
		}
	}
	if n := reads(); n != before {
		t.Errorf("the dead sessions wrote %d 'read' row(s)", n-before)
	}
	if w := l.get("/operator/tenants", l.plantSession(true, "")); w.Code != http.StatusOK {
		t.Fatalf("CONTROL: a planted live MFA-stamped session = %d on the list, want 200", w.Code)
	}

	// 6. Tickets; sign out.
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
	                    WHERE s.admin_id = $1 AND k.consumed_at IS NULL`, l.f.id); n != 0 {
		t.Errorf("%d unconsumed read ticket(s), want 0", n)
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
	                    WHERE s.admin_id = $1 AND k.consumed_at IS NOT NULL`, l.f.id); n != reads() {
		t.Errorf("%d consumed read ticket(s), want one per 'read' row (%d)", n, reads())
	}
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
