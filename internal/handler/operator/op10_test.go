package operator_test

// op10_test.go -- M10 OP-10 phase B: /operator/legal on the shipped router with the fake
// store and snapshot (rig_test.go). The header table of its response classes (C49-C66),
// the read's second unit (the read budget's since OP-13 phase B), the publication (the
// publisher is the session's; the snapshot is refreshed), the refusals before any store
// call, the escaping of a re-opened text, the version list's publisher, and the
// visible-text rule. Against PostgreSQL: op10_db_test.go.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/operatorauth"
)

func legalForm(slug, body string) url.Values { return url.Values{"slug": {slug}, "body": {body}} }

// TestOperatorHeaders_TheLegalClassesCarryThePolicy drives the eighteen response classes
// of /operator/legal (C49-C66) with the 40-class test's hostile drive and checks
// (runHeaderClasses), each held to its classRoutes entry.
//
// PART I -- measured on these eighteen, at WriteHeader (the recorder's Result().Header):
// the designed status, route, header names and values (designedHeaders); no hostile value
// (hostileValues) in a header value or in the body, raw or query-escaped -- every one of
// the eighteen's last request carries the hostile query (hostileDrive adds it on
// /operator/legal; C60's hand-built request gets it from hostileOn); no script; and the
// store counts below: C58 (cross-origin) and C66 (PUT) make no store call, C55 (the
// read refused by the read budget -- OP-13 phase B; the session budget's second unit
// before it) makes no LegalVersions call.
//
// PART II -- the list above, on these eighteen classes.
//
// PART III -- This test measures the eighteen classes and the raw and query-escaped forms
// only; anything else (examples: a class a later handler adds, a hostile value echoed
// HTML-escaped or base32-encoded) is code review's -- no completeness claim.
func TestOperatorHeaders_TheLegalClassesCarryThePolicy(t *testing.T) {
	g := newRig(t)
	ad := &addrs{}
	last := &driveLog{}
	send := func(r req) *httptest.ResponseRecorder {
		if r.remote == "" {
			r.remote = ad.next() + ":1"
		}
		r.host = opHost
		*last = driveLog{r.method, r.path}
		return g.do(hostileDrive(r))
	}
	sfs := map[string]string{"Sec-Fetch-Site": "same-origin"}
	get := func(c ...*http.Cookie) req {
		return req{method: http.MethodGet, path: "/operator/legal", cookies: c, header: sfs}
	}
	post := func(form url.Values, c ...*http.Cookie) req {
		return req{method: http.MethodPost, path: "/operator/legal", form: form, origin: opOrigin, cookies: c}
	}
	// signIn signs a fresh operator in (operatorauth's per-account budget allows ten code
	// attempts per window; the classes sign in more often than that).
	signIn := func() *http.Cookie {
		t.Helper()
		f := g.active()
		ch := cookie(send(req{method: http.MethodPost, path: "/operator/login", origin: opOrigin,
			form: url.Values{"email": {f.email}, "password": {f.password}}}), operatorauth.ChallengeCookieName)
		if ch == nil {
			t.Fatal("PREMISE: no challenge")
		}
		s := cookie(send(req{method: http.MethodPost, path: "/operator/login/totp", origin: opOrigin,
			form: url.Values{"code": {totpAt(f.key, g.now)}}, cookies: []*http.Cookie{ch}}), operatorauth.SessionCookieName)
		if s == nil {
			t.Fatal("PREMISE: no session")
		}
		return s
	}
	failing := func(method string, err error, r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			rq := r()
			g.store.mu.Lock()
			g.store.fail[method] = err
			g.store.mu.Unlock()
			defer func() { g.store.mu.Lock(); delete(g.store.fail, method); g.store.mu.Unlock() }()
			return send(rq)
		}
	}
	once := func(r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return send(r()) }
	}
	var storeBefore, versionsBefore int
	classes := []headerClass{
		{"C49 legal page", 200, once(func() req { return get(signIn()) })},
		{"C50 legal page without a cookie", 303, once(func() req { return get() })},
		{"C51 legal page with a dead cookie", 303, once(func() req {
			return get(&http.Cookie{Name: operatorauth.SessionCookieName, Value: strings.Repeat("D", 43)})
		})},
		{"C52 legal page, a same-site read", 303, once(func() req {
			r := get(signIn())
			r.header = map[string]string{"Sec-Fetch-Site": "same-site"}
			return r
		})},
		{"C53 legal page, the version list fails", 503, failing("LegalVersions", errFakeDB, func() req { return get(signIn()) })},
		{"C54 legal page, the version list's session is refused", 303,
			failing("LegalVersions", db.ErrOperatorRefused, func() req { return get(signIn()) })},
		{"C55 legal page, the read budget refused", 429, func() *httptest.ResponseRecorder {
			c := signIn()
			for i := 0; i < 60; i++ { // 60 legal views: the read budget (readLimit 60, OP-13) spent
				if w := send(get(c)); w.Code != http.StatusOK {
					t.Fatalf("PREMISE: legal view %d = %d", i+1, w.Code)
				}
			}
			versionsBefore = g.store.count("LegalVersions")
			return send(get(c)) // the 61st read: the gate's 61st unit of 100, the read budget's 61st of 60
		}},
		{"C56 publication", 303, once(func() req { return post(legalForm("privacy", "FAKE C56 text"), signIn()) })},
		{"C57 publication without a cookie", 303, once(func() req { return post(legalForm("privacy", "FAKE C57 text")) })},
		{"C58 publication, cross-origin", 403, func() *httptest.ResponseRecorder {
			c := signIn()
			storeBefore = g.store.total()
			r := post(legalForm("privacy", "FAKE C58 text"), c)
			r.origin, r.header = "https://taptime.mt", map[string]string{"Sec-Fetch-Site": "same-site"}
			w := send(r)
			if n := g.store.total() - storeBefore; n != 0 {
				t.Errorf("C58: a cross-origin publication made %d store call(s)", n)
			}
			return w
		}},
		{"C59 publication, an oversized text", 413, once(func() req {
			return post(legalForm("privacy", strings.Repeat("x", operator.MaxLegalBodyForTest)), signIn())
		})},
		{"C60 publication, an unreadable form", 400, func() *httptest.ResponseRecorder {
			c := signIn()
			r := httptest.NewRequest(http.MethodPost, "http://"+opHost+"/operator/legal", strings.NewReader("slug=privacy&body=%zz"))
			r.Host = opHost
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.Header.Set("Origin", opOrigin)
			r.AddCookie(c)
			hostileOn(r)
			r.RemoteAddr = ad.next() + ":1"
			*last = driveLog{r.Method, r.URL.Path}
			w := httptest.NewRecorder()
			g.h.ServeHTTP(w, r)
			return w
		}},
		{"C61 publication, a document that does not exist", 400, once(func() req { return post(legalForm("refunds", "FAKE C61 text"), signIn()) })},
		{"C62 publication, no visible text", 400, once(func() req { return post(legalForm("privacy", "\u200b \u2060\r\n"), signIn()) })},
		{"C63 publication, the database fails", 503,
			failing("PublishLegal", errFakeDB, func() req { return post(legalForm("privacy", "FAKE C63 text"), signIn()) })},
		{"C64 publication, the refresh fails", 303, func() *httptest.ResponseRecorder {
			r := post(legalForm("cookies", "FAKE C64 text"), signIn())
			g.texts.mu.Lock()
			g.texts.fail = errFakeDB
			g.texts.mu.Unlock()
			defer func() { g.texts.mu.Lock(); g.texts.fail = nil; g.texts.mu.Unlock() }()
			return send(r)
		}},
		{"C65 publication, the session is refused", 303,
			failing("PublishLegal", db.ErrOperatorRefused, func() req { return post(legalForm("imprint", "FAKE C65 text"), signIn()) })},
		{"C66 PUT on the legal page", 405, func() *httptest.ResponseRecorder {
			storeBefore = g.store.total()
			w := send(req{method: http.MethodPut, path: "/operator/legal", origin: opOrigin, form: url.Values{}})
			if n := g.store.total() - storeBefore; n != 0 {
				t.Errorf("C66: PUT made %d store call(s)", n)
			}
			return w
		}},
	}
	for i, c := range classes {
		if !strings.HasPrefix(c.name, "C"+strconv.Itoa(49+i)+" ") {
			t.Fatalf("PREMISE: class %d is named %q, want C%d", 49+i, c.name, 49+i)
		}
	}
	if scripted := runHeaderClasses(t, classes, last); len(scripted) != 0 {
		t.Errorf("classes %v loaded a script, want none", scripted)
	}
	if n := g.store.count("LegalVersions") - versionsBefore; n != 0 {
		t.Errorf("C55: the read refused by the read budget still called LegalVersions %d time(s)", n)
	}
}

// TestLegalPage_AReadCountsTwiceAgainstTheSessionBudget measures a legal page view's two
// units. THE NAME IS OP-10'S AND NO LONGER THE WHOLE RULE: until OP-13 phase B both units
// were the session budget's (sessionLimit, 200 since OP-11); since then the gate charges
// one unit of the session's REQUEST budget (sessionLimit, 100) and the handler one of its
// READ budget (readLimit, 60). The name stays because docs/plan/m10-platform.md and ADR
// 0021 cite it in records that are not rewritten after the fact (cmd/tappa's
// TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator keeps its OP-7 name for the same
// reason).
//
// PART I -- one session, 60 legal page views from 60 addresses: 60 x 200 and 60
// LegalVersions calls; the 61st view is 429 from the read budget -- the gate's predicate
// ran (TouchOperatorSession +1) and LegalVersions did not -- and the window's first read
// refusal is the read budget's WARN line. The same session then has 39 console views
// left (61 + 39 = 100 units of the request budget), all 200, and the next is 429 at the
// gate. CONTROL: another session reads the page.
//
// PART II -- the counts above. PART III -- This test measures these sequences only; a
// read added by a later screen is code review's (and its own test's) -- no completeness
// claim.
func TestLegalPage_AReadCountsTwiceAgainstTheSessionBudget(t *testing.T) {
	g := newRig(t)
	n := 0
	view := func(path string, c *http.Cookie) *httptest.ResponseRecorder {
		n++
		return g.do(req{method: http.MethodGet, host: opHost, path: path, cookies: []*http.Cookie{c},
			remote: fmt.Sprintf("198.18.%d.%d:1", n/200, n%200+1), header: map[string]string{"Sec-Fetch-Site": "same-origin"}})
	}
	a := g.signIn(g.active())
	for i := 0; i < 60; i++ {
		if w := view("/operator/legal", a); w.Code != http.StatusOK {
			t.Fatalf("legal view %d of one session = %d, want 200", i+1, w.Code)
		}
	}
	if got := g.store.count("LegalVersions"); got != 60 {
		t.Fatalf("PREMISE: %d read(s) for 60 views", got)
	}
	touches := g.store.count("TouchOperatorSession")
	if w := view("/operator/legal", a); w.Code != http.StatusTooManyRequests || g.store.count("LegalVersions") != 60 {
		t.Fatalf("the 61st legal view of one session = %d with %d read(s), want 429 and 60", w.Code, g.store.count("LegalVersions"))
	}
	if g.store.count("TouchOperatorSession") != touches+1 {
		t.Errorf("the view refused by the read budget: predicate +%d, want +1", g.store.count("TouchOperatorSession")-touches)
	}
	if !strings.Contains(g.logs.String(), "operator read budget reached") {
		t.Error("the read budget's first refusal wrote no WARN line")
	}
	for i := 0; i < 39; i++ {
		if w := view("/operator", a); w.Code != http.StatusOK {
			t.Fatalf("console view %d after 61 legal views = %d, want 200 -- the request budget is 100", i+1, w.Code)
		}
	}
	if w := view("/operator", a); w.Code != http.StatusTooManyRequests {
		t.Errorf("the 101st request of the session = %d, want 429 at the gate", w.Code)
	}
	if w := view("/operator/legal", g.signIn(g.active())); w.Code != http.StatusOK {
		t.Fatalf("CONTROL: another session's legal view = %d", w.Code)
	}
}

// TestLegalPublish_TheSessionPublishesAndThePublicSnapshotFollows: a publication goes to
// the store under the hash of the session that sent it -- the hash the fake's live
// session map keys to that operator -- whatever the form says about a publisher (it
// carries published_by, admin_id and session fields here, all naming someone else); it
// is answered 303 to the screen, the snapshot is refreshed once and serves the text,
// and the version list names the publisher and marks the version live. Undoing it is a
// publication: the earlier text published again is a third version, live, the second
// one no longer live. CONTROL: before any publication the snapshot has no privacy text.
func TestLegalPublish_TheSessionPublishesAndThePublicSnapshotFollows(t *testing.T) {
	g := newRig(t)
	f := g.active()
	c := g.signIn(f)
	if _, ok := g.texts.Published()["privacy"]; ok {
		t.Fatal("CONTROL: the snapshot has a privacy text before any publication")
	}
	other := uuid.New().String()
	publish := func(body string) *httptest.ResponseRecorder {
		form := legalForm("privacy", body)
		form.Set("published_by", other)
		form.Set("admin_id", other)
		form.Set("session", strings.Repeat("Z", 64))
		return g.post("/operator/legal", form, c)
	}
	const v1, v2 = "FAKE privacy policy, version one.", "FAKE privacy policy, version two -- longer."
	for _, body := range []string{v1, v2, v1} {
		w := publish("  " + body + "\r\n")
		if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/legal" {
			t.Fatalf("publication = %d %q, want 303 to the screen", w.Code, w.Result().Header.Get("Location"))
		}
	}
	g.store.mu.Lock()
	pubs := append([]fakePublication(nil), g.store.published...)
	var publishers []uuid.UUID
	for _, p := range pubs {
		publishers = append(publishers, g.store.live[p.hash].AdminID)
	}
	g.store.mu.Unlock()
	if len(pubs) != 3 {
		t.Fatalf("%d publication(s) reached the store, want 3", len(pubs))
	}
	for i, p := range pubs {
		if publishers[i] != f.id {
			t.Errorf("publication %d went under a hash that is not the signed-in operator's session", i+1)
		}
		if p.slug != "privacy" || (p.body != v1 && p.body != v2) {
			t.Errorf("publication %d: slug %q, body %q -- want the form's, trimmed", i+1, p.slug, p.body)
		}
	}
	if got := g.texts.refreshCount(); got != 3 {
		t.Errorf("%d refresh(es) for 3 publications", got)
	}
	if got := g.texts.Published()["privacy"].Body; got != v1 {
		t.Errorf("the snapshot serves %q after the undo, want version one again", got)
	}
	page := g.get("/operator/legal", c)
	if page.Code != http.StatusOK {
		t.Fatalf("legal page = %d", page.Code)
	}
	rows := regexp.MustCompile(`(?s)<li class="op-version">(.*?)</li>`).FindAllStringSubmatch(page.Body.String(), -1)
	if len(rows) != 3 {
		t.Fatalf("the version list has %d row(s), want 3", len(rows))
	}
	name := "Fake Operator " + f.id.String()[:4]
	for i, want := range []struct {
		bytes int
		live  bool
	}{{len(v1), true}, {len(v2), false}, {len(v1), false}} {
		row := rows[i][1]
		if !strings.Contains(row, strconv.Itoa(want.bytes)+" bytes") || !strings.Contains(row, name) ||
			strings.Contains(row, "Live") != want.live || !strings.Contains(row, "/legal/privacy") {
			t.Errorf("version row %d (newest first) = %q; want %d bytes, the publisher %q, live %v", i+1, row, want.bytes, name, want.live)
		}
	}
}

// TestLegalPublish_AClientThatLeavesStillGetsThePublicationAndTheRefresh measures
// publishLegal's detached context (legal.go, legalWriteTimeout): a browser that goes away
// after pressing Publish does not stop the publication or the refresh after it.
//
// PART I -- (a) the request's context is cancelled BEFORE the handler runs: the
// publication is stored and the snapshot serves it; (b) the context is cancelled by the
// store right AFTER it recorded the version (the fake's afterPublish -- the moment a
// commit has happened and the refresh has not): the snapshot serves the new text. In both
// the answer is the 303, each refresh's context carried a deadline no later than
// legalWriteTimeout -- itself pinned to ten seconds, so the bound is not the constant
// measured against itself -- and was not cancelled. CONTROLS, each independent: the request
// context of (a) and (b) is cancelled when the handler returns (so the test is not green
// because nothing was cancelled); the fake's PublishLegal and Refresh refuse a cancelled
// context (so it is not green because the fakes ignore one).
//
// PART II -- the two moments above. PART III -- This test measures the handler's context
// only; what the database driver does on a cancel mid-statement, and an ingress that
// drops the 303, are not measured here -- no completeness claim.
func TestLegalPublish_AClientThatLeavesStillGetsThePublicationAndTheRefresh(t *testing.T) {
	if operator.LegalWriteTimeoutForTest != 10*time.Second {
		t.Fatalf("legalWriteTimeout = %v, want 10s (the card's decision 7(a), ADR 0020 §7 (d)); the deadline checks below compare with it",
			operator.LegalWriteTimeoutForTest)
	}
	g := newRig(t)
	c := g.signIn(g.active())
	dead, kill := context.WithCancel(context.Background())
	kill()
	if err := g.store.PublishLegal(dead, "any", "privacy", "FAKE"); err == nil {
		t.Fatal("CONTROL: the fake store publishes on a cancelled context")
	}
	if err := g.texts.Refresh(dead); err == nil {
		t.Fatal("CONTROL: the fake snapshot refreshes on a cancelled context")
	}
	refreshesBefore := g.texts.refreshCount()

	send := func(ctx context.Context, body string) *httptest.ResponseRecorder {
		return g.do(req{method: http.MethodPost, host: opHost, path: "/operator/legal", form: legalForm("privacy", body),
			origin: opOrigin, cookies: []*http.Cookie{c}, ctx: ctx})
	}
	// (a) gone before the handler ran.
	gone, leave := context.WithCancel(context.Background())
	leave()
	const before, after = "FAKE privacy text, the client left before.", "FAKE privacy text, the client left after the commit."
	if w := send(gone, before); w.Code != http.StatusSeeOther {
		t.Fatalf("(a) = %d, want 303", w.Code)
	}
	if got := g.texts.Published()["privacy"].Body; got != before {
		t.Fatalf("(a) the snapshot serves %q, want the text published after the client left", got)
	}
	// (b) gone between the commit and the refresh.
	leaving, leaveNow := context.WithCancel(context.Background())
	defer leaveNow()
	g.store.mu.Lock()
	g.store.afterPublish = leaveNow
	g.store.mu.Unlock()
	if w := send(leaving, after); w.Code != http.StatusSeeOther {
		t.Fatalf("(b) = %d, want 303", w.Code)
	}
	if leaving.Err() == nil {
		t.Fatal("CONTROL: (b)'s request context was not cancelled by the store's hook")
	}
	if got := g.texts.Published()["privacy"].Body; got != after {
		t.Fatalf("(b) the snapshot serves %q, want the text committed before the client left", got)
	}
	g.store.mu.Lock()
	n := len(g.store.published)
	g.store.mu.Unlock()
	if n != 2 {
		t.Fatalf("%d publication(s) stored, want 2", n)
	}
	g.texts.mu.Lock()
	deadlines := append([]time.Duration(nil), g.texts.deadlines[refreshesBefore:]...)
	g.texts.mu.Unlock()
	if len(deadlines) != 2 {
		t.Fatalf("%d refresh(es) for 2 publications", len(deadlines))
	}
	for i, d := range deadlines {
		if d <= 0 || d > operator.LegalWriteTimeoutForTest {
			t.Errorf("refresh %d ran with %v to its deadline; want a deadline within %v", i+1, d, operator.LegalWriteTimeoutForTest)
		}
	}
}

// TestLegalPublish_AFailedRefreshRedirectsAndTheScreenSaysThePageIsBehind measures the
// screen when the public page is behind the version list (legal.go: publishLegal's PRG,
// legalPage's heal, legalView's Behind).
//
// PART I -- a publication whose refresh fails is answered 303 to the screen (POST ->
// 303 -> GET: a reload re-reads the screen, it does not publish again) and the store has
// the version; the screen then (refresh still failing) is 200, tries one refresh, and
// says once that the public page is behind -- inside the notice (components.Notice, the
// saffron block) the live version's publication time and not the snapshot's, the fake's
// versions a minute apart so the two print differently -- while the editor holds the
// text the page serves; when the refresh works again, the next
// view heals the snapshot (one refresh) and the warning is gone; a view after that makes
// no refresh. CONTROL: before the failure, a view of a current snapshot shows no warning
// and makes no refresh -- the warning is not printed on every page.
//
// PART II -- the sequence above, on one document. PART III -- This test measures a
// snapshot behind the list as the fake shapes it (a version newer than the snapshot); a
// version older than the list's page (legalVersionsLimit) is not compared -- no
// completeness claim.
func TestLegalPublish_AFailedRefreshRedirectsAndTheScreenSaysThePageIsBehind(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	const warning = "The public page is behind"
	const v1, v2 = "FAKE cookie notice, version one.", "FAKE cookie notice, version two."
	if w := g.post("/operator/legal", legalForm("cookies", v1), c); w.Code != http.StatusSeeOther {
		t.Fatalf("PREMISE: the first publication = %d", w.Code)
	}
	refreshes := g.texts.refreshCount()
	if body := g.get("/operator/legal", c).Body.String(); strings.Contains(body, warning) || g.texts.refreshCount() != refreshes {
		t.Fatalf("CONTROL: a current snapshot shows the warning = %v, refreshes +%d; want false, +0",
			strings.Contains(body, warning), g.texts.refreshCount()-refreshes)
	}
	g.texts.mu.Lock()
	g.texts.fail = errFakeDB
	g.texts.mu.Unlock()
	w := g.post("/operator/legal", legalForm("cookies", v2), c)
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/legal" {
		t.Fatalf("a publication whose refresh fails = %d %q, want 303 to the screen", w.Code, w.Result().Header.Get("Location"))
	}
	g.store.mu.Lock()
	stored, liveAt, snapAt := len(g.store.published), g.store.published[len(g.store.published)-1].at, g.store.published[0].at
	g.store.mu.Unlock()
	const minute = "2006-01-02 15:04 UTC"
	if liveAt.Format(minute) == snapAt.Format(minute) {
		t.Fatalf("CONTROL: the two versions print the same time (%s); the warning's time cannot be told apart", liveAt.Format(minute))
	}
	if stored != 2 || g.texts.Published()["cookies"].Body != v1 {
		t.Fatalf("PREMISE: %d publication(s) stored and the snapshot serves %q; want 2 and version one",
			stored, g.texts.Published()["cookies"].Body)
	}
	refreshes = g.texts.refreshCount()
	page := g.get("/operator/legal", c)
	body := page.Body.String()
	notices := warnNotice.FindAllStringSubmatch(body, -1)
	if page.Code != http.StatusOK || strings.Count(body, warning) != 1 || len(notices) != 1 {
		t.Fatalf("the screen while the page is behind = %d with the warning %d time(s) in %d notice(s); want 200, once, one",
			page.Code, strings.Count(body, warning), len(notices))
	}
	if n := notices[0][1]; !strings.Contains(n, liveAt.Format(minute)) || strings.Contains(n, snapAt.Format(minute)) {
		t.Errorf("the warning names the live version's time %v and the snapshot's %v; want true, false",
			strings.Contains(n, liveAt.Format(minute)), strings.Contains(n, snapAt.Format(minute)))
	}
	if !strings.Contains(body, v1) || strings.Contains(body, v2) {
		t.Error("the editor does not hold the text the public page serves")
	}
	if g.texts.refreshCount() != refreshes+1 {
		t.Errorf("the screen made %d refresh(es) of a snapshot it saw behind, want 1", g.texts.refreshCount()-refreshes)
	}
	g.texts.mu.Lock()
	g.texts.fail = nil
	g.texts.mu.Unlock()
	refreshes = g.texts.refreshCount()
	body = g.get("/operator/legal", c).Body.String()
	if strings.Contains(body, warning) || g.texts.Published()["cookies"].Body != v2 || g.texts.refreshCount() != refreshes+1 {
		t.Fatalf("the screen once the refresh works: warning %v, snapshot %q, refreshes +%d; want false, version two, +1",
			strings.Contains(body, warning), g.texts.Published()["cookies"].Body, g.texts.refreshCount()-refreshes)
	}
	refreshes = g.texts.refreshCount()
	if body = g.get("/operator/legal", c).Body.String(); strings.Contains(body, warning) || g.texts.refreshCount() != refreshes {
		t.Errorf("the view after the heal: warning %v, refreshes +%d; want false, +0", strings.Contains(body, warning), g.texts.refreshCount()-refreshes)
	}
}

// warnNotice is a ToneWarn components.Notice block and its contents.
var warnNotice = regexp.MustCompile(`(?s)<section class="border-l-4 border-saffron[^"]*">(.*?)</section>`)

// liveHash is the hash of one of the fake's live sessions -- the publisher of a version a
// test commits behind the screen's back.
func liveHash(t *testing.T, g *rig) string {
	t.Helper()
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	for h := range g.store.live {
		return h
	}
	t.Fatal("PREMISE: no live session in the fake")
	return ""
}

// TestLegalPage_AVersionPublishedBetweenTheTwoReadsIsNotCalledBehind measures the order of
// legalPage's two reads (legal.go: the snapshot before the list).
//
// PART I -- version one is published and live; then, during a view, another operator
// publishes version two and its refresh lands right after the screen read the list (the
// fake's afterVersions). That view is 200 with no warning, makes no refresh of its own
// (only the other operator's), and its editor holds version one -- the version its list
// shows live. CONTROL: the next view holds version two, no warning, no refresh.
//
// PART II -- this sequence. PART III -- a publication landing between the list's read and
// the heal's refresh is the next test's; other interleavings are code review's -- no
// completeness claim.
func TestLegalPage_AVersionPublishedBetweenTheTwoReadsIsNotCalledBehind(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	const v1, v2 = "FAKE cookie notice, version one.", "FAKE cookie notice, version two, by someone else."
	if w := g.post("/operator/legal", legalForm("cookies", v1), c); w.Code != http.StatusSeeOther {
		t.Fatalf("PREMISE: the first publication = %d", w.Code)
	}
	other := liveHash(t, g)
	g.store.mu.Lock()
	g.store.afterVersions = func() {
		g.store.mu.Lock()
		g.store.afterVersions = nil
		g.store.mu.Unlock()
		if err := g.store.PublishLegal(context.Background(), other, "cookies", v2); err != nil {
			t.Errorf("PREMISE: the other publication: %v", err)
		}
		if err := g.texts.Refresh(context.Background()); err != nil {
			t.Errorf("PREMISE: the other publication's refresh: %v", err)
		}
	}
	g.store.mu.Unlock()
	refreshes := g.texts.refreshCount()
	page := g.get("/operator/legal", c)
	body := page.Body.String()
	if page.Code != http.StatusOK || strings.Contains(body, "The public page is behind") {
		t.Fatalf("the view a publication overtook = %d, warning %v; want 200 and none", page.Code, strings.Contains(body, "The public page is behind"))
	}
	if g.texts.Published()["cookies"].Body != v2 {
		t.Fatal("PREMISE: the other publication's refresh did not land")
	}
	if got := g.texts.refreshCount() - refreshes; got != 1 {
		t.Errorf("%d refresh(es) during the view, want 1 (the other publication's; the view's own would be a second)", got)
	}
	if !strings.Contains(body, v1) || strings.Contains(body, v2) {
		t.Errorf("the editor holds version two = %v, version one = %v; want the version the list shows live (one)",
			strings.Contains(body, v2), strings.Contains(body, v1))
	}
	refreshes = g.texts.refreshCount()
	body = g.get("/operator/legal", c).Body.String()
	if !strings.Contains(body, v2) || strings.Contains(body, "The public page is behind") || g.texts.refreshCount() != refreshes {
		t.Errorf("CONTROL: the next view holds version two = %v, warns = %v, refreshes +%d; want true, false, +0",
			strings.Contains(body, v2), strings.Contains(body, "The public page is behind"), g.texts.refreshCount()-refreshes)
	}
}

// TestLegalPage_ASnapshotNewerThanTheListAfterTheHealIsNotCalledBehind measures the
// direction of the warning (legal.go's compare: a snapshot OLDER than the list's live
// version warns, a newer one does not).
//
// PART I -- version one is live; version two is published and its refresh fails, so the
// snapshot is behind; at the next view, the heal's refresh finds a third version committed
// in the meantime (the fake's beforeRefresh) and installs it. That view is 200 with no
// warning -- the public page serves a text newer than the list -- and the snapshot serves
// version three. CONTROL: before the heal, the same snapshot against the same list is
// called behind (one warning, on the view where the refresh still fails).
//
// PART II -- this sequence. PART III -- see the previous test -- no completeness claim.
func TestLegalPage_ASnapshotNewerThanTheListAfterTheHealIsNotCalledBehind(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	const v1, v2, v3 = "FAKE terms, version one.", "FAKE terms, version two.", "FAKE terms, version three, by someone else."
	if w := g.post("/operator/legal", legalForm("terms", v1), c); w.Code != http.StatusSeeOther {
		t.Fatalf("PREMISE: the first publication = %d", w.Code)
	}
	g.texts.mu.Lock()
	g.texts.fail = errFakeDB
	g.texts.mu.Unlock()
	if w := g.post("/operator/legal", legalForm("terms", v2), c); w.Code != http.StatusSeeOther {
		t.Fatalf("PREMISE: the second publication = %d", w.Code)
	}
	if body := g.get("/operator/legal", c).Body.String(); strings.Count(body, "The public page is behind") != 1 {
		t.Fatal("CONTROL: a snapshot older than the list is not called behind")
	}
	other := liveHash(t, g)
	g.texts.mu.Lock()
	g.texts.fail = nil
	g.texts.beforeRefresh = func() {
		g.texts.beforeRefresh = nil // under the fake's lock: Refresh holds it while the hook runs
		if err := g.store.PublishLegal(context.Background(), other, "terms", v3); err != nil {
			t.Errorf("PREMISE: the third publication: %v", err)
		}
	}
	g.texts.mu.Unlock()
	page := g.get("/operator/legal", c)
	body := page.Body.String()
	if g.texts.Published()["terms"].Body != v3 {
		t.Fatal("PREMISE: the heal did not install the third version")
	}
	if page.Code != http.StatusOK || strings.Contains(body, "The public page is behind") {
		t.Errorf("the view whose heal installed a version newer than its list = %d, warning %v; want 200 and none",
			page.Code, strings.Contains(body, "The public page is behind"))
	}
}

// TestLegalPage_ShowsWhoPublishedEachVersion: the version list names an operator's
// version by the operator's name and an older version (published_by not an operator's)
// as "tenant admin (legacy)" (ADR 0020 §7); the live version of each document carries
// the chip, the others do not; the list shows no text. A full page (the limit) says older
// versions are not shown; a shorter one does not.
func TestLegalPage_ShowsWhoPublishedEachVersion(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	name, admin := "Ada FAKE Operator", uuid.New()
	g.store.mu.Lock()
	g.store.versions = []db.LegalVersion{
		{ID: uuid.New(), Slug: "terms", PublishedAt: time.Date(2026, 8, 14, 9, 30, 0, 0, time.UTC), BodyBytes: 11,
			PublishedBy: db.LegalPublishedByLegacy, Current: true},
		{ID: uuid.New(), Slug: "privacy", PublishedAt: time.Date(2026, 10, 3, 7, 5, 0, 0, time.FixedZone("x", 2*3600)), BodyBytes: 22,
			PublishedBy: db.LegalPublishedByOperator, PublisherID: &admin, PublisherName: &name, Current: true},
	}
	g.store.mu.Unlock()
	body := g.get("/operator/legal", c).Body.String()
	rows := regexp.MustCompile(`(?s)<li class="op-version">(.*?)</li>`).FindAllStringSubmatch(body, -1)
	if len(rows) != 2 {
		t.Fatalf("%d row(s), want 2", len(rows))
	}
	if r := rows[0][1]; !strings.Contains(r, name) || !strings.Contains(r, "2026-10-03 05:05 UTC") || !strings.Contains(r, "22 bytes") || !strings.Contains(r, "Live") {
		t.Errorf("the operator's version row = %q", r)
	}
	if r := rows[1][1]; !strings.Contains(r, "tenant admin (legacy)") || !strings.Contains(r, "/legal/terms") || strings.Contains(r, name) {
		t.Errorf("the legacy version row = %q", r)
	}
	if strings.Contains(body, admin.String()) {
		t.Error("the page prints the operator's id")
	}
	if strings.Contains(body, "Older versions are not shown") {
		t.Error("a two-row list says older versions are not shown")
	}
	// A full page.
	g.store.mu.Lock()
	g.store.versions = nil
	for i := 0; i < operator.LegalVersionsLimitForTest+5; i++ {
		g.store.versions = append(g.store.versions, db.LegalVersion{ID: uuid.New(), Slug: "cookies", PublishedAt: time.Unix(int64(1_900_000_000+i), 0),
			BodyBytes: 1, PublishedBy: db.LegalPublishedByLegacy})
	}
	g.store.mu.Unlock()
	body = g.get("/operator/legal", c).Body.String()
	if n := len(regexp.MustCompile(`<li class="op-version">`).FindAllString(body, -1)); n != operator.LegalVersionsLimitForTest ||
		!strings.Contains(body, "Older versions are not shown") {
		t.Errorf("a full page lists %d row(s) and says older ones are not shown = %v; want %d and true", n,
			strings.Contains(body, "Older versions are not shown"), operator.LegalVersionsLimitForTest)
	}
	g.store.mu.Lock()
	pages := append([]db.LegalVersionsPage(nil), g.store.pages...)
	g.store.mu.Unlock()
	for _, p := range pages {
		if p.Number != 1 || int(p.Size) != operator.LegalVersionsLimitForTest {
			t.Errorf("the screen asked for page %+v, want number 1, size %d", p, operator.LegalVersionsLimitForTest)
		}
	}
}

// TestLegalPublish_RefusesASlugTheProductDoesNotHave (M7-06's name, the operator's
// screen since OP-10): a slug outside legal.Slugs -- empty, another case, padded, a fifth
// document, a path, a NUL -- is 400 with the fixed page, and no store call and no refresh
// is made. CONTROL: "privacy" publishes.
func TestLegalPublish_RefusesASlugTheProductDoesNotHave(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	for _, slug := range []string{"", "PRIVACY", "Privacy", "privacy ", " privacy", "refunds", "../privacy", "privacy\x00", "privacy/x"} {
		w := g.post("/operator/legal", legalForm(slug, "FAKE text"), c)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "That document does not exist") {
			t.Errorf("slug %q = %d, want 400 and the unknown-document page", slug, w.Code)
		}
	}
	if n := g.store.count("PublishLegal"); n != 0 || g.texts.refreshCount() != 0 {
		t.Fatalf("refused slugs made %d publication(s) and %d refresh(es)", n, g.texts.refreshCount())
	}
	if w := g.post("/operator/legal", legalForm("privacy", "FAKE text"), c); w.Code != http.StatusSeeOther {
		t.Fatalf("CONTROL: privacy = %d", w.Code)
	}
}

// invisibleBodies are the texts with no visible character the publication refuses:
// OP-10A's eight measured code points (each alone, each repeated, all eight together),
// white space, controls, format characters, a combining mark alone, the Hangul fillers,
// a variation selector, the soft hyphen, and the two blank symbols the round-2 review
// found (U+303F, U+1D159; legal.go's blankSymbol).
var invisibleBodies = map[string]string{
	"empty": "", "spaces": "   ", "line breaks and a tab": "\r\n\t\r\n",
	"U+180E": "\u180e", "U+200B": "\u200b", "U+200C": "\u200c", "U+200D": "\u200d",
	"U+2060": "\u2060", "U+2800": "\u2800", "U+3164": "\u3164", "U+FEFF": "\ufeff",
	"U+200B x3":            "\u200b\u200b\u200b",
	"the eight together":   "\u180e\u200b\u200c\u200d\u2060\u2800\u3164\ufeff",
	"the eight and spaces": " \u180e \u200b\n\u200c\t\u200d \u2060\r\n\u2800 \u3164 \ufeff ",
	"no-break spaces":      "\u00a0\u1680\u202f\u3000\u2028\u2029\u205f",
	"controls":             "\x01\x02\x7f\u0085",
	"a combining mark":     "\u0301",
	"Hangul fillers":       "\u115f\u1160\uffa0",
	"variation selector":   "\ufe0f",
	"soft hyphen":          "\u00ad",
	"bidi controls":        "\u202a\u202e\u2066\u2069",
	"U+303F":               "\u303f",
	"U+1D159":              "\U0001d159",
	"the blank symbols":    " \u2800\u303f\U0001d159 ",
}

// visibleBodies are texts the publication accepts: one visible character of each kind
// the rule names (letter, number, punctuation, symbol), alone and among invisible ones;
// and U+FFFC OBJECT REPLACEMENT CHARACTER, accepted by decision (legal.go's blankSymbol).
var visibleBodies = map[string]string{
	"a letter": "a", "a Maltese letter": "ħ", "a digit": "1", "a punctuation mark": ".", "a symbol": "€",
	"a letter among invisible ones": "\u200b\u3164A\u2800\ufeff", "a sentence": "We keep attendance records.",
	"U+FFFC (a decision)": "\ufffc",
}

// TestVisibleText_TheListedInvisibleBodiesAreRefused holds legal.go's visibleText to its
// two tables: each of invisibleBodies is not visible, each of visibleBodies is.
func TestVisibleText_TheListedInvisibleBodiesAreRefused(t *testing.T) {
	for name, b := range invisibleBodies {
		if operator.VisibleTextForTest(b) {
			t.Errorf("%s (%q) counts as visible text", name, b)
		}
	}
	for name, b := range visibleBodies {
		if !operator.VisibleTextForTest(b) {
			t.Errorf("CONTROL: %s (%q) does not count as visible text", name, b)
		}
	}
}

// TestLegalPublish_RefusesAnEmptyBodyAndSaysSo (M7-06's name, the operator's screen since
// OP-10; the column half of the same property is internal/domain/legal's
// TestLegalDB_AnEmptyBodyIsRefusedByTheColumnAndNotOnlyByGo): each text of
// invisibleBodies, posted, is 400 with the page that says there was no visible text, and
// no store call and no refresh is made. CONTROL: each text of visibleBodies publishes, and
// the stored text is the posted one with its white-space ends trimmed.
func TestLegalPublish_RefusesAnEmptyBodyAndSaysSo(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	for name, b := range invisibleBodies {
		w := g.post("/operator/legal", legalForm("privacy", b), c)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "There is no text to publish") {
			t.Errorf("%s = %d, want 400 and the no-text page", name, w.Code)
		}
	}
	if n := g.store.count("PublishLegal"); n != 0 || g.texts.refreshCount() != 0 {
		t.Fatalf("texts with no visible character made %d publication(s) and %d refresh(es)", n, g.texts.refreshCount())
	}
	for name, b := range visibleBodies {
		if w := g.post("/operator/legal", legalForm("privacy", " "+b+"\n"), c); w.Code != http.StatusSeeOther {
			t.Errorf("CONTROL: %s = %d, want 303", name, w.Code)
		}
	}
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	if len(g.store.published) != len(visibleBodies) {
		t.Fatalf("CONTROL: %d publication(s) of %d visible texts", len(g.store.published), len(visibleBodies))
	}
	for _, p := range g.store.published {
		if p.body != strings.TrimSpace(p.body) || p.body == "" {
			t.Errorf("CONTROL: the stored text %q is not the posted one trimmed", p.body)
		}
	}
}

// TestLegalPublish_RefusesABodyBiggerThanTheCeiling (M7-06's name, the operator's screen
// since OP-10): a request body of maxLegalBody (256 KiB) bytes publishes, its text whole;
// one byte more is 413 with the too-long page and no store call. CONTROL: the encoded form
// is the size asked for.
func TestLegalPublish_RefusesABodyBiggerThanTheCeiling(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	if operator.MaxLegalBodyForTest != 256<<10 {
		t.Fatalf("maxLegalBody = %d, want 256 KiB (ADR 0016 §6, op_publish_legal's octet_length > 262144)", operator.MaxLegalBodyForTest)
	}
	raw := func(n int) (string, string) {
		prefix := "slug=terms&body="
		text := strings.Repeat("a", n-len(prefix))
		return prefix + text, text
	}
	send := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://"+opHost+"/operator/legal", strings.NewReader(body))
		r.Host = opHost
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", opOrigin)
		r.AddCookie(c)
		r.RemoteAddr = "192.0.2.10:4000"
		w := httptest.NewRecorder()
		g.h.ServeHTTP(w, r)
		return w
	}
	over, _ := raw(operator.MaxLegalBodyForTest + 1)
	if len(over) != operator.MaxLegalBodyForTest+1 {
		t.Fatalf("CONTROL: the oversized body is %d bytes", len(over))
	}
	if w := send(over); w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), "That text is too long") {
		t.Fatalf("a body of 256 KiB + 1 = %d, want 413 and the too-long page", w.Code)
	}
	if n := g.store.count("PublishLegal"); n != 0 {
		t.Fatalf("an oversized body made %d publication(s)", n)
	}
	exact, text := raw(operator.MaxLegalBodyForTest)
	if w := send(exact); w.Code != http.StatusSeeOther {
		t.Fatalf("a body of exactly 256 KiB = %d, want 303", w.Code)
	}
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	if len(g.store.published) != 1 || g.store.published[0].body != text {
		t.Fatalf("the 256 KiB publication stored %d text(s), whole = %v", len(g.store.published),
			len(g.store.published) == 1 && g.store.published[0].body == text)
	}
}

// TestLegalScreen_ReopensOnPastedMarkupWithoutExecutingIt (M7-06's name, the operator's
// screen since OP-10): a live text that closes the textarea and opens a script re-opens
// the editor escaped -- the raw sequence is not in the page, its escaped form is.
func TestLegalScreen_ReopensOnPastedMarkupWithoutExecutingIt(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	const attack = `</textarea><script>alert(1)</script>`
	g.texts.put("terms", attack)
	body := g.get("/operator/legal", c).Body.String()
	if strings.Contains(body, "</textarea><script>") || strings.Contains(body, "<script") {
		t.Fatal("the editor was closed by the text it re-opened on")
	}
	if !strings.Contains(body, "&lt;/textarea&gt;&lt;script&gt;") {
		t.Error("the escaped text is absent, so the assertion above may pass because the text never rendered")
	}
}

// TestLegalScreen_TheTextClearsAA recomputes, from tailwind.config.js's palette, the
// contrast of the legal screen's text colours on their grounds (WCAG 2.1 relative
// luminance, sRGB; ink at 70% composited on its ground): ink on paper (the cards, the
// docket, the list), ink at 70% on paper (the docket label, the status and help lines),
// ink on green-lite (the Live chip), paper on tappa-green (the publish button), and the
// behind warning's ink heading and ink-at-85% text on saffron-lite (components.Notice,
// ToneWarn) -- each against AA's 4.5:1 (14px and smaller text is not large text).
func TestLegalScreen_TheTextClearsAA(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "tailwind.config.js"))
	if err != nil {
		t.Fatal(err)
	}
	hex := func(token string) [3]float64 {
		m := regexp.MustCompile(`'?` + regexp.QuoteMeta(token) + `'?:\s*'#([0-9A-Fa-f]{6})'`).FindSubmatch(b)
		if m == nil {
			t.Fatalf("tailwind.config.js has no %s", token)
		}
		var c [3]float64
		for i := 0; i < 3; i++ {
			v, err := strconv.ParseUint(string(m[1][2*i:2*i+2]), 16, 8)
			if err != nil {
				t.Fatal(err)
			}
			c[i] = float64(v)
		}
		return c
	}
	over := func(fg [3]float64, alpha float64, bg [3]float64) [3]float64 {
		var c [3]float64
		for i := range c {
			c[i] = alpha*fg[i] + (1-alpha)*bg[i]
		}
		return c
	}
	ink, paper, green, greenLite, saffronLite := hex("ink"), hex("paper"), hex("tappa-green"), hex("green-lite"), hex("saffron-lite")
	for _, c := range []struct {
		what   string
		fg, bg [3]float64
	}{
		{"ink on paper", ink, paper},
		{"ink at 70% on paper", over(ink, 0.7, paper), paper},
		{"ink on green-lite", ink, greenLite},
		{"paper on tappa-green", paper, green},
		{"ink on saffron-lite", ink, saffronLite},
		{"ink at 85% on saffron-lite", over(ink, 0.85, saffronLite), saffronLite},
	} {
		if got := contrast(c.fg, c.bg); got < 4.5 {
			t.Errorf("%s = %.2f:1, want at least 4.5:1", c.what, got)
		} else {
			t.Logf("%s = %.2f:1", c.what, got)
		}
	}
}
