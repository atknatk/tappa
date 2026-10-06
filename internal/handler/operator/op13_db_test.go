package operator_test

// op13_db_test.go -- M10 OP-13 phase B end to end against PostgreSQL: the operator signs
// in through the surface and reads one tenant's plaque inventory through op_begin_read +
// op_read_tenant_plaques (00030), on newLegalE2E's committed rig (op10_db_test.go: one
// owner connection switched to tappa_operator, each statement its own committed
// transaction -- the read's second phase refuses a ticket whose transaction has not
// committed).
//
// WHICH TENANT. No tenant and no plaque row is written here (a plaque row is never
// deleted: agent-brief, 2026-09-26). The screen reads the demo seed's Kebab Factory tenant
// (test/fixtures/seed.sql; CI loads the seed) and, on a database without it, the tenant
// holding the most plaques; a database with no plaque at all skips the test.
//
// WHAT ONE RUN LEAVES BEHIND (the class op10_db_test.go's header counts; append-only tables
// and the foreign keys that point at them), by construction: one operator account
// (op10b-…, newLegalE2E's; disabled at cleanup), its five sessions (the signed-in one and
// four planted; revoked at cleanup), and its audit rows -- login, logout and four 'read'
// rows: three of scope tenant_plaques (the screen, the unknown id, the planted live
// session's read), one of tenant_detail (the overview that links the screen) and, per
// retry when another test changes the tenant's plaques mid-read, one more tenant_plaques.
// Read tickets are deleted at cleanup.

import (
	"errors"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
)

// The seed's Kebab Factory Ltd. (test/fixtures/seed.sql).
const seedKF = "10000000-0000-4000-8000-000000000001"

// TestE2E_PlaqueScreenReadsThroughTheDefinerAndAuditsEachRead is OP-13's end-to-end
// acceptance on PostgreSQL (the file header says what it commits):
//
//  1. the overview links the tenant's plaque screen by its canonical path;
//  2. the plaque screen: 200; the banner carries the tenant's name as the owner reads it,
//     escaped; its rows are the owner's own list of the tenant's plaques -- the same uids
//     in the same order (location NULLS FIRST, uid), the first 200 -- and each row's chip
//     and sentence are the screen's words for the shape the owner's columns make (status,
//     location, encode stamp; db.TenantPlaque.Shape); the count sentence carries the
//     owner's count (read before and after the page; retried while another test changes
//     the tenant's plaques); exactly ONE committed 'read' row per view (a retry is a view
//     and has its own) -- scope tenant_plaques, the tenant named, no page, detail {} -- and
//     no row of another scope (no overview read names the banner);
//  3. THE KEYS: the owner reads aes_key_ref and app_key_ref of every plaque the page lists,
//     and none of them is on the page in any of keyNeedles' eight forms, and the page has
//     no run of 32 or more hex digits (the values are compared in memory and never printed:
//     a failure names a count);
//  4. an id no tenant has: 404 with its page, and a committed 'read' row naming that id; a
//     malformed id: 404 and no row;
//  5. sessions the predicate refuses -- never MFA-stamped, revoked, idle 31 minutes -- get
//     the sign-in's 303 and leave no row; CONTROL: a planted live session reads the screen;
//  6. every ticket of the operator's sessions consumed -- one per 'read' row -- none left;
//     the sign-out's row.
func TestE2E_PlaqueScreenReadsThroughTheDefinerAndAuditsEachRead(t *testing.T) {
	l := newLegalE2E(t)
	var tenant uuid.UUID
	var name string
	err := l.owner.QueryRow(l.ctx, `SELECT t.id, t.name FROM tenants t WHERE t.id = $1 AND EXISTS (SELECT 1 FROM tags g WHERE g.tenant_id = t.id)`,
		seedKF).Scan(&tenant, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		err = l.owner.QueryRow(l.ctx, `SELECT t.id, t.name FROM tenants t JOIN (SELECT tenant_id, count(*) c FROM tags GROUP BY tenant_id) g
		                                ON g.tenant_id = t.id ORDER BY g.c DESC, t.id LIMIT 1`).Scan(&tenant, &name)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		t.Skip("the database holds no plaque; the plaque screen has nothing to read")
	}
	if err != nil {
		t.Fatalf("pick a tenant: %v", err)
	}
	sess := l.signIn(l.f)
	reads := func(scope string) int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1
		                   AND target_scope = $2`, l.f.id, scope)
	}
	allReads := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1`, l.f.id)
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
	path := "/operator/tenants/" + tenant.String() + "/plaques"

	// 1. The overview links the screen.
	if w := l.get("/operator/tenants/"+tenant.String(), sess); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), `href="`+path+`"`) {
		t.Fatalf("the overview = %d, or it does not link %s", w.Code, path)
	}

	// 2. The screen.
	type ownRow struct {
		uid, status      string
		mounted, encoded bool
		aes, app         []byte
	}
	owners := func() (rows []ownRow, total int) {
		l.t.Helper()
		total = l.ownerInt(`SELECT count(*)::int FROM tags WHERE tenant_id = $1`, tenant)
		r, err := l.owner.Query(l.ctx, `SELECT uid::text, status, location_id IS NOT NULL, encoded_at IS NOT NULL,
		                                       coalesce(aes_key_ref, ''::bytea), coalesce(app_key_ref, ''::bytea)
		                                  FROM tags WHERE tenant_id = $1 ORDER BY location_id NULLS FIRST, uid LIMIT 200`, tenant)
		if err != nil {
			l.t.Fatalf("the owner's list: %v", err)
		}
		defer r.Close()
		for r.Next() {
			var x ownRow
			if err := r.Scan(&x.uid, &x.status, &x.mounted, &x.encoded, &x.aes, &x.app); err != nil {
				l.t.Fatalf("the owner's list: %v", err)
			}
			rows = append(rows, x)
		}
		if err := r.Err(); err != nil {
			l.t.Fatalf("the owner's list: %v", err)
		}
		return rows, total
	}
	sameUIDs := func(a, b []ownRow) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i].uid != b[i].uid || a[i].status != b[i].status || a[i].mounted != b[i].mounted || a[i].encoded != b[i].encoded {
				return false
			}
		}
		return true
	}
	before, allBefore := reads("tenant_plaques"), allReads()
	var page string
	var own []ownRow
	var total int
	settled, views := false, 0
	for attempt := 0; attempt < 3 && !settled; attempt++ {
		b, bt := owners()
		views++
		w := l.get(path, sess)
		if w.Code != http.StatusOK {
			t.Fatalf("the plaque screen = %d; log: %s", w.Code, l.logs.String())
		}
		page = w.Body.String()
		a, at := owners()
		if sameUIDs(a, b) && at == bt {
			settled, own, total = true, a, at
		}
	}
	if !settled {
		t.Fatal("the tenant's plaques changed under every one of three attempts; another test is writing to them")
	}
	banner := strings.Index(page, `<section class="op-tenant" aria-label="Tenant">`)
	if banner < 0 || !strings.Contains(page[banner:], html.EscapeString(name)) ||
		!strings.Contains(page, "<title>Plaques — "+html.EscapeString(name)+" — Taptime operator</title>") {
		t.Error("the plaque screen's banner or title does not carry the tenant's name")
	}
	rows := plaqueRowRe.FindAllStringSubmatch(page, -1)
	if len(rows) != len(own) {
		t.Fatalf("the screen lists %d plaque(s), the owner reads %d (of %d)", len(rows), len(own), total)
	}
	words := operator.PlaqueWordsForTest()
	uidRe := regexp.MustCompile(`^\s*<span class="font-mono font-bold">([^<]*)</span>`)
	for i, o := range own {
		m := uidRe.FindStringSubmatch(rows[i][1])
		if m == nil || m[1] != o.uid {
			t.Errorf("row %d is not plaque %s (the owner's order)", i+1, o.uid)
			continue
		}
		p := db.TenantPlaque{Status: o.status}
		if o.mounted {
			p.LocationID = &uuid.UUID{}
		}
		if o.encoded {
			p.EncodedAt = &time.Time{}
		}
		w := words[p.Shape()]
		if !strings.Contains(rows[i][1], ">"+html.EscapeString(w.Label)+"</span>") ||
			!strings.Contains(rows[i][1], `<span class="w-full text-sm">`+html.EscapeString(w.Sentence)+`</span>`) {
			t.Errorf("row %d (%s, %s): the chip or the sentence is not the screen's words for %s", i+1, o.uid, o.status, p.Shape())
		}
	}
	mono := func(s string) string { return `<span class="font-mono">` + s + `</span>` }
	switch {
	case total > len(own):
		if !strings.Contains(page, "The first "+mono(strconv.Itoa(len(own)))+" of "+mono(strconv.Itoa(total))+" plaques are listed") {
			t.Errorf("the count sentence does not say the first %d of %d", len(own), total)
		}
	default:
		if !strings.Contains(page, mono(strconv.Itoa(total))+" plaque") {
			t.Errorf("the count sentence does not carry the owner's count %d", total)
		}
	}
	// One row per view, exactly: a retry is a view of its own and writes its own row, and a
	// view that read twice (a second TenantPlaques after a good one) would write two.
	if n := reads("tenant_plaques") - before; n != views {
		t.Errorf("the screen's %d view(s) wrote %d 'read' row(s), want exactly one per view", views, n)
	}
	if n, all := reads("tenant_plaques")-before, allReads()-allBefore; all != n {
		t.Errorf("the screen's view(s) wrote %d 'read' row(s) of another scope -- the screen reads op_read_tenant_plaques only", all-n)
	}
	if scope, detail, page, size, target := lastRead(); scope != "tenant_plaques" || detail != `{}` || page != nil || size != nil ||
		target == nil || *target != tenant {
		t.Errorf("the screen's row: scope %s, detail %s, page %v/%v, target %v; want tenant_plaques, {}, none, the tenant",
			scope, detail, page, size, target)
	}

	// 3. The keys.
	keysSeen, keysTried := 0, 0
	for _, o := range own {
		for _, k := range [][]byte{o.aes, o.app} {
			if len(k) < 8 { // a short or empty dev value would match by chance; a real ref is longer
				continue
			}
			keysTried++
			for _, needle := range keyNeedles(k) {
				if strings.Contains(page, needle) {
					keysSeen++
				}
			}
		}
	}
	if keysTried == 0 {
		t.Error("PREMISE: no plaque of the tenant has a key reference of eight bytes or more; the key half measured nothing")
	}
	if keysSeen != 0 {
		t.Errorf("the page carries a plaque key reference %d time(s)", keysSeen)
	}
	if m := keyShaped.FindAllString(page, -1); len(m) != 0 {
		t.Errorf("the page carries %d run(s) of 32 or more hex digits", len(m))
	}

	// 4. An id no tenant has; a malformed id.
	unknown := uuid.New()
	n := reads("tenant_plaques")
	if w := l.get("/operator/tenants/"+unknown.String()+"/plaques", sess); w.Code != http.StatusNotFound ||
		!strings.Contains(w.Body.String(), "There is no tenant with that id") {
		t.Errorf("an id no tenant has = %d, want 404 and its page", w.Code)
	}
	if got := reads("tenant_plaques"); got != n+1 {
		t.Errorf("an id no tenant has wrote %d 'read' row(s), want 1", got-n)
	}
	if _, _, _, _, target := lastRead(); target == nil || *target != unknown {
		t.Errorf("the unknown id's row names %v, want %v", target, unknown)
	}
	if w := l.get("/operator/tenants/"+strings.ReplaceAll(unknown.String(), "-", "")+"/plaques", sess); w.Code != http.StatusNotFound ||
		!strings.Contains(w.Body.String(), "That link does not name a tenant") {
		t.Errorf("a malformed id = %d, want 404 and its page", w.Code)
	}
	if got := reads("tenant_plaques"); got != n+1 {
		t.Errorf("a malformed id wrote %d 'read' row(s), want 0", got-n-1)
	}

	// 5. Dead sessions.
	n = allReads()
	for name, c := range map[string]*http.Cookie{
		"never MFA-stamped": l.plantSession(false, ""),
		"revoked":           l.plantSession(true, "revoked_at = clock_timestamp()"),
		"idle 31 minutes":   l.plantSession(true, "last_used_at = clock_timestamp() - interval '31 minutes'"),
	} {
		if res := l.get(path, c).Result(); res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != "/operator/login" {
			t.Errorf("%s session: the plaque screen = %d %q, want the sign-in's 303", name, res.StatusCode, res.Header.Get("Location"))
		}
	}
	if got := allReads(); got != n {
		t.Errorf("the dead sessions wrote %d 'read' row(s)", got-n)
	}
	if w := l.get(path, l.plantSession(true, "")); w.Code != http.StatusOK {
		t.Fatalf("CONTROL: a planted live MFA-stamped session = %d on the plaque screen, want 200", w.Code)
	}

	// 6. Tickets; sign out.
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
	                    WHERE s.admin_id = $1 AND k.consumed_at IS NULL`, l.f.id); n != 0 {
		t.Errorf("%d unconsumed read ticket(s), want 0", n)
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
	                    WHERE s.admin_id = $1 AND k.consumed_at IS NOT NULL`, l.f.id); n != allReads() {
		t.Errorf("%d consumed read ticket(s), want one per 'read' row (%d)", n, allReads())
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_read_tickets k JOIN platform_sessions s ON s.id = k.session_id
	                    WHERE s.admin_id = $1 AND k.kind = 'tenant_plaques'`, l.f.id); n != reads("tenant_plaques") {
		t.Errorf("%d tenant_plaques ticket(s), want one per tenant_plaques 'read' row (%d)", n, reads("tenant_plaques"))
	}
	if w := l.post("/operator/logout", nil, sess); w.Code != http.StatusSeeOther {
		t.Fatalf("sign-out = %d", w.Code)
	}
	if err := asOperator(l.ctx, l.op); err != nil {
		t.Errorf("harness at the end: %v", err)
	}
}
