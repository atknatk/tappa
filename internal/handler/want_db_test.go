package handler

// want_db_test.go -- M15-11 through the REAL router, the REAL orchestrator and real Postgres (ADR
// 0032 §B.3, §B.5): the form's want, as a browser without JavaScript sends it, in every shape --
// in a body that parses none of them is a 400 and each is exactly one row; a body ParseForm
// refuses is a 400 with no row and no counter spent -- the switch's two closed modes, two
// presses at once by one person, and a stale page pressed across the business day's start.
//
// Every case is a fresh employee of the harness's tenant (h.newEmployee), so the per-person
// debounce of one case cannot reach another, and every employee has a business day far from now
// (fixtures.GiveFarDayCutoff), so the engine's D is the one the case states whatever the hour.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/store"
)

// wantWarning is the one line Checkin writes for a want it cannot read.
const wantWarning = "checkin: the want field is conflicting or unknown; the engine chooses the direction"

// sourceOf reads a record's direction_source (nil = NULL).
func (h *tapHarness) sourceOf(t *testing.T, id uuid.UUID) *string {
	t.Helper()
	var s *string
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT direction_source FROM transactions WHERE tenant_id = $1 AND id = $2`, h.tenantID, id).Scan(&s)
	}); err != nil {
		t.Fatalf("read direction_source: %v", err)
	}
	return s
}

// captureTapLog points the tap handler's logger at a JSON buffer for the test.
func (h *tapHarness) captureTapLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := h.tap.log
	h.tap.log = slog.New(slog.NewJSONHandler(&buf, nil))
	t.Cleanup(func() { h.tap.log = prev })
	return &buf
}

// logLines decodes the captured JSON lines.
func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("log line %q: %v", l, err)
		}
		out = append(out, m)
	}
	return out
}

// TestCheckinDB_EveryWantShapeIsOneRecordAndNoneIsA400 is the card's JS-less criterion (ADR 0032
// §B.3, "asla 400 değil"): the engine's D is `in` (a person with no open check-in), and the form
// carries want the way a browser without JavaScript sends it -- the pressed button's one value, an
// empty one, a repeat, two that conflict, an unknown word. Each is a 200 and EXACTLY one row: the
// readable ones the pressed direction (and 'employee' where it differs from D), the unreadable ones
// the engine's in, 'auto', and one warning line that does not carry the value. A want in the URL
// is not a press.
func TestCheckinDB_EveryWantShapeIsOneRecordAndNoneIsA400(t *testing.T) {
	h := newTapHarness(t)
	logs := h.captureTapLog(t)
	const canary = "sideways-CANARY-7f3a"
	cases := []struct {
		name   string
		want   []string // nil: no field
		query  string
		dir    string
		source string
		warned bool
	}{
		{"no want", nil, "", "in", "auto", false},
		{"want out, the one button", []string{"out"}, "", "out", "employee", false},
		{"want in, the engine's own", []string{"in"}, "", "in", "auto", false},
		{"want empty", []string{""}, "", "in", "auto", false},
		{"want out twice", []string{"out", "out"}, "", "out", "employee", false},
		{"want empty beside out", []string{"", "out"}, "", "out", "employee", false},
		{"want in and out", []string{"in", "out"}, "", "in", "auto", true},
		{"want sideways", []string{canary}, "", "in", "auto", true},
		{"want OUT in capitals", []string{"OUT"}, "", "in", "auto", true},
		{"want in the URL only", nil, "?want=out", "in", "auto", false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs.Reset()
			emp := h.newEmployee(t, "active")
			cookie := h.cookieForEmployee(t, emp)
			signed, err := h.tap.contexts.mint(h.nfcContext(uint32(h.startCtr)+1+uint32(i)), h.sessionIDOf(t, cookie))
			if err != nil {
				t.Fatal(err)
			}
			form := url.Values{"ctx": {signed}}
			if tc.want != nil {
				form["want"] = tc.want
			}
			req := httptestPost(t, "/api/checkin"+tc.query, form, cookie)
			w := serve(h, req)
			if w.Code != http.StatusOK {
				t.Fatalf("status %d, want 200: in a body that parses, no value of want is refused", w.Code)
			}
			if n := h.countFor(t, emp); n != 1 {
				t.Fatalf("%d rows, want exactly one", n)
			}
			rec := h.lastRecord(t, emp)
			src := h.sourceOf(t, rec.ID)
			if rec.Verdict != "ok" || deref(rec.Type) != tc.dir || src == nil || *src != tc.source {
				t.Fatalf("recorded %s/%s/%v, want ok/%s/%s", rec.Verdict, deref(rec.Type), src, tc.dir, tc.source)
			}
			var warned int
			for _, l := range logLines(t, logs) {
				if l["msg"] == wantWarning {
					warned++
					for k := range l {
						switch k {
						case "time", "level", "msg", "employee_id":
						default:
							t.Errorf("the warning carries %q=%v; it names the person and nothing the client sent", k, l[k])
						}
					}
				}
			}
			if (warned == 1) != tc.warned || warned > 1 {
				t.Errorf("%d warning line(s), want warned=%v", warned, tc.warned)
			}
			if strings.Contains(logs.String(), canary) {
				t.Errorf("the log repeats the want value: %s", logs.String())
			}
		})
	}
}

// TestCheckinDB_WantIsIgnoredWhileTheSwitchIsOff is the card's negative test (ADR 0032 §B.5,
// security lens F-1) through the real router: with the business-day switch unset, and with it set
// to an instant after the server's clock, want=out over the engine's `in` is the engine's `in`,
// 'auto', exactly one row. CONTROL: the harness's own service (switch on, far in the past) records
// the same press as `out`, 'employee'. The modes are injected as the orchestrator's config; the
// process environment is not touched.
func TestCheckinDB_WantIsIgnoredWhileTheSwitchIsOff(t *testing.T) {
	h := newTapHarness(t)
	future := time.Now().Add(time.Hour)
	modes := []struct {
		name   string
		from   *time.Time
		on     bool
		dir    string
		source string
	}{
		{"switch unset", nil, false, "in", "auto"},
		{"the server's clock before the switch", &future, false, "in", "auto"},
		{"CONTROL: the switch on", h.cfg.BusinessDayFrom, true, "out", "employee"},
	}
	for i, m := range modes {
		t.Run(m.name, func(t *testing.T) {
			if !m.on {
				cfg := *h.cfg
				cfg.BusinessDayFrom = m.from
				svc, err := checkin.New(h.data, h.trail, &cfg, slog.New(slog.DiscardHandler))
				if err != nil {
					t.Fatal(err)
				}
				prev := h.tap.checkins
				h.tap.checkins = svc
				t.Cleanup(func() { h.tap.checkins = prev })
			}
			emp := h.newEmployee(t, "active")
			cookie := h.cookieForEmployee(t, emp)
			w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1+uint32(i)), cookie, url.Values{"want": {"out"}})
			if w.Code != http.StatusOK {
				t.Fatalf("status %d, want 200", w.Code)
			}
			if n := h.countFor(t, emp); n != 1 {
				t.Fatalf("%d rows, want exactly one", n)
			}
			rec := h.lastRecord(t, emp)
			if src := h.sourceOf(t, rec.ID); deref(rec.Type) != m.dir || src == nil || *src != m.source {
				t.Fatalf("recorded %s/%v, want %s/%s", deref(rec.Type), src, m.dir, m.source)
			}
		})
	}
}

// TestCheckinDB_TwoPressesAtOnceOneIsIgnoredAndTheSourceMatchesTheEngine is ADR 0006 with a
// request (run with -race): one person, two POSTs at once -- one with want=out, one with no want
// -- on the QR channel (no counter, so the debounce under the person's lock is the only brake).
// Whichever is decided first is recorded and the other is `ignored`: a want cannot carry a press
// past the debounce. The recorded one's source is consistent with D = in: `out` is 'employee',
// `in` is 'auto'; the ignored one has no direction and no source. Ten rounds, a fresh person each,
// so both orders are likely to be met.
func TestCheckinDB_TwoPressesAtOnceOneIsIgnoredAndTheSourceMatchesTheEngine(t *testing.T) {
	h := newTapHarness(t)
	seen := map[string]int{}
	for round := range 10 {
		emp := h.newEmployee(t, "active")
		cookie := h.cookieForEmployee(t, emp)
		sid := h.sessionIDOf(t, cookie)
		forms := make([]url.Values, 2)
		for i := range forms {
			signed, err := h.tap.contexts.mint(h.qrContext(), sid)
			if err != nil {
				t.Fatal(err)
			}
			forms[i] = url.Values{"ctx": {signed}}
		}
		forms[0]["want"] = []string{"out"}
		codes := make([]int, 2)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range forms {
			req := httptestPost(t, "/api/checkin", forms[i], cookie)
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				codes[i] = serve(h, req).Code
			}()
		}
		close(start)
		wg.Wait()
		if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
			t.Fatalf("round %d: statuses %v, want 200 and 200", round, codes)
		}
		rows := h.rowsOf(t, emp)
		if len(rows) != 2 {
			t.Fatalf("round %d: %d rows, want 2 (§4.6: the ignored press is recorded too)", round, len(rows))
		}
		var recorded, ignored int
		for _, r := range rows {
			switch {
			case r.verdict == "ignored":
				ignored++
				if r.kind != nil || r.source != nil {
					t.Errorf("round %d: the ignored press carries direction %v, source %v", round, r.kind, r.source)
				}
			case r.kind != nil && r.source != nil && ((*r.kind == "out") == (*r.source == "employee")):
				recorded++
				seen[*r.kind]++
			default:
				t.Errorf("round %d: a %s row %v/%v -- 'employee' must hold exactly when the direction is not D (in)", round, r.verdict, r.kind, r.source)
			}
		}
		if recorded != 1 || ignored != 1 {
			t.Fatalf("round %d: %d recorded, %d ignored, want one of each", round, recorded, ignored)
		}
	}
	t.Logf("the recorded press over ten rounds: %v", seen)
}

// httptestPost is a POST of form to target from inside the fixture location's network
// (203.0.113.0/24), carrying cookie -- built before it is served, so a test can start several at
// once.
func httptestPost(t *testing.T, target string, form url.Values, cookie *http.Cookie) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.5:41234"
	req.AddCookie(cookie)
	return req
}

// serve runs req through the harness's real router.
func serve(h *tapHarness, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	return w
}

// pageCtxValueRE is the tap page's signed context field.
var pageCtxValueRE = regexp.MustCompile(`name="ctx" value="([^"]+)"`)

// signedContextOn is the signed context a rendered tap page carries, as a browser submits it.
func signedContextOn(t *testing.T, page string) string {
	t.Helper()
	m := pageCtxValueRE.FindStringSubmatch(page)
	if len(m) != 2 {
		t.Fatalf("the page carries no signed context:\n%s", page)
	}
	return m[1]
}

// rowFacts is one row's verdict, direction and source.
type rowFacts struct {
	verdict      string
	kind, source *string
}

// rowsOf is every row of one employee, oldest first.
func (h *tapHarness) rowsOf(t *testing.T, employee uuid.UUID) []rowFacts {
	t.Helper()
	var out []rowFacts
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT verdict, type, direction_source FROM transactions
		                             WHERE tenant_id = $1 AND employee_id = $2 ORDER BY created_at, id`, h.tenantID, employee)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r rowFacts
			if err := rows.Scan(&r.verdict, &r.kind, &r.source); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("read the rows: %v", err)
	}
	return out
}

// TestDevTapDB_ASimulatedTapCarriesTheChosenDirection: the dev simulator (ADR 0026) ends on GET /t
// and the press is the tap page's own POST, so a want reaches POST /api/checkin on the dev path by
// the same field as on a real tap -- simulated tap, the page's signed context, want=out over the
// engine's `in` -> `out`, 'employee', one row.
func TestDevTapDB_ASimulatedTapCarriesTheChosenDirection(t *testing.T) {
	h := newHarness(t, "invited")
	h.activation.EnableDevTools(h.cfg)
	h.tap.EnableDevTools(h.cfg)
	// A fresh server AFTER the switch, as TestDevTapDB_ASimulatedTapActivatesThroughTheRealPath
	// does (the race detector does not model happens-before through sockets).
	h.server.Close()
	h.server = httptest.NewServer(h.router)
	t.Cleanup(h.server.Close)
	c := h.client(t)
	h.consent(t, c, h.issue(t))
	if resp, page := h.devTapPost(t, c); resp.StatusCode != http.StatusOK || !strings.Contains(page, "Activation complete") {
		t.Fatalf("PREMISE: the first simulated tap activates; landed on %s with %d", resp.Request.URL, resp.StatusCode)
	}
	resp, page := h.devTapPost(t, c)
	if resp.StatusCode != http.StatusOK || !strings.Contains(page, "data-tap-button") {
		t.Fatalf("PREMISE: the second simulated tap lands on the tap page; got %d at %s", resp.StatusCode, resp.Request.URL)
	}
	signed := signedContextOn(t, page)
	before := h.countFor(t, h.employeeID)
	post, err := c.PostForm(h.server.URL+"/api/checkin", url.Values{"ctx": {signed}, "want": {"out"}})
	if err != nil {
		t.Fatal(err)
	}
	if body(t, post); post.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/checkin from the simulated tap's page: %d", post.StatusCode)
	}
	if n := h.countFor(t, h.employeeID) - before; n != 1 {
		t.Fatalf("%d rows, want one", n)
	}
	rec := h.lastRecord(t, h.employeeID)
	if src := h.sourceOf(t, rec.ID); deref(rec.Type) != "out" || src == nil || *src != "employee" {
		t.Fatalf("recorded %s/%v, want out/employee", deref(rec.Type), src)
	}
}

// TestCheckinDB_AnUnparsableBodyIsA400BeforeWantIsRead pins where "no value of want is a 400"
// stops (the M15-11 lenses' DÜŞÜK-1): it holds in a form body that PARSES. A body ParseForm
// refuses -- over checkinMaxBody (900 × want=out), or a broken percent-encoding (want=%zz) -- is
// the ordinary unusable-body 400, answered before the tap context is read: no row and the
// plaque's counter not spent. CONTROL: a large repeat under the limit (500 × want=out) parses,
// is one request, and is recorded as the press -- out, 'employee' over the engine's in.
func TestCheckinDB_AnUnparsableBodyIsA400BeforeWantIsRead(t *testing.T) {
	h := newTapHarness(t)
	repeat := func(n int) string { return strings.Repeat("&want=out", n) }
	cases := []struct {
		name   string
		tail   string
		status int
	}{
		{"a body over checkinMaxBody", repeat(900), http.StatusBadRequest},
		{"a broken percent-encoding", "&want=%zz", http.StatusBadRequest},
		{"CONTROL: 500 × want=out under the limit", repeat(500), http.StatusOK},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			emp := h.newEmployee(t, "active")
			cookie := h.cookieForEmployee(t, emp)
			ctr := uint32(h.startCtr) + 1 + uint32(i)
			signed, err := h.tap.contexts.mint(h.nfcContext(ctr), h.sessionIDOf(t, cookie))
			if err != nil {
				t.Fatal(err)
			}
			raw := "ctx=" + url.QueryEscape(signed) + tc.tail
			if over := len(raw) > checkinMaxBody; over != (tc.name == cases[0].name) {
				t.Fatalf("PREMISE: the body is %d bytes against checkinMaxBody %d", len(raw), checkinMaxBody)
			}
			before := h.lastCtr(t, h.tagUID)
			req := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader(raw))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.RemoteAddr = "203.0.113.5:41234"
			req.AddCookie(cookie)
			w := serve(h, req)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d", w.Code, tc.status)
			}
			if tc.status != http.StatusOK {
				if n := h.countFor(t, emp); n != 0 {
					t.Errorf("%d rows written for a body ParseForm refused, want none", n)
				}
				if got := h.lastCtr(t, h.tagUID); got != before {
					t.Errorf("last_ctr %d -> %d: a refused body spent the counter", before, got)
				}
				return
			}
			if n := h.countFor(t, emp); n != 1 {
				t.Fatalf("%d rows, want one", n)
			}
			rec := h.lastRecord(t, emp)
			if src := h.sourceOf(t, rec.ID); deref(rec.Type) != "out" || src == nil || *src != "employee" {
				t.Fatalf("recorded %s/%v, want out/employee: a repeat is one request", deref(rec.Type), src)
			}
			if got := h.lastCtr(t, h.tagUID); got != int32(ctr) {
				t.Errorf("last_ctr %d, want %d", got, ctr)
			}
		})
	}
}

// TestCheckinDB_AStalePagePressedAfterTheCutoffIsTheChoiceAndListedAsCheckInMissing is the card's
// stale-page criterion through the real router (the general lens's DÜŞÜK-2, its own experiment):
// the person checked in two hours ago, the page was minted 90 s ago -- when that check-in was
// still of this business day and the screen would have said "Clock out" -- and the person's
// business day began a minute or so ago. The press, want=out, arrives after the boundary: D is now
// `in` (the check-in is of the previous business day), so the row is the person's `out`,
// 'employee', `ok` on the venue's network (QR -- an NFC counter gap would flag it), and M15-06's
// "check-in missing" list names it as after an earlier day (b). The cutoff is the test's own
// employee's person tier; the shared seed tenant is not touched.
func TestCheckinDB_AStalePagePressedAfterTheCutoffIsTheChoiceAndListedAsCheckInMissing(t *testing.T) {
	h := newTapHarness(t)
	emp := h.newEmployee(t, "active")
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE employees e SET day_cutoff = to_char((now() AT TIME ZONE t.timezone) - interval '1 minute', 'HH24:MI')::time
		                            FROM tenants t WHERE t.id = e.tenant_id AND e.tenant_id = $1 AND e.id = $2`, h.tenantID, emp)
		if err == nil && tag.RowsAffected() != 1 {
			err = fmt.Errorf("%d rows", tag.RowsAffected())
		}
		return err
	}); err != nil {
		t.Fatalf("the employee's cutoff a minute ago: %v", err)
	}
	h.seedTapAgedBy(t, emp, 2*time.Hour, "in", false)
	cookie := h.cookieForEmployee(t, emp)

	w := h.postAgedTap(t, h.qrContext(), cookie, 90*time.Second, url.Values{"want": {"out"}})
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}
	rec := h.lastRecord(t, emp)
	src := h.sourceOf(t, rec.ID)
	if rec.Verdict != "ok" || deref(rec.Type) != "out" || src == nil || *src != "employee" {
		t.Fatalf("recorded %s/%s/%v via %s, want ok/out/employee", rec.Verdict, deref(rec.Type), src, deref(rec.MatchedSid))
	}
	var listed, earlier bool
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := store.New(tx).ListCheckoutsWithoutCheckIn(ctx, store.ListCheckoutsWithoutCheckInParams{
			TenantID: h.tenantID, FromAt: time.Now().Add(-24 * time.Hour), ToAt: time.Now().Add(time.Hour), RowLimit: 500})
		for _, r := range rows {
			if r.EmployeeID != nil && *r.EmployeeID == emp && r.OccurredAt.Equal(rec.OccurredAt) {
				listed, earlier = true, r.AfterEarlierDay
			}
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if !listed || !earlier {
		t.Errorf("on the check-in-missing list %v, after an earlier day %v; want both: the chosen out is (b)", listed, earlier)
	}
}
