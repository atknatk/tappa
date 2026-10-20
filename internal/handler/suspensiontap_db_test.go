package handler

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
)

// tapSeen is everything §5 decides about one tap, plus what §4.6 counts, from the
// PAGE to the record: the plaque page's status, redirect target and whether it minted
// a tap context; the check-in's status and target; the rows the step wrote, the
// security alerts it raised, and the decision of the last row -- verdict, trust,
// direction, note, rule, layer, practice, queue, channel. Pointers are flattened to text
// so two harnesses compare by value.
type tapSeen struct {
	PageStatus   int
	PageLocation string
	PageMinted   bool
	// PageBody and Body are the plaque page's and the check-in's answers as a person
	// sees them, normalised (pageText): a suspension that changed what the tap screen or
	// the result screen SAYS -- the venue, the network, the business's name or brand,
	// the button -- shows here even when every decision above is equal.
	PageBody string
	Body     string
	Status   int
	Location string
	Rows     int
	Alerts   int
	Verdict  string
	Trust    string
	Type     string
	Note     string
	Sid      string
	Layer    string
	Practice bool
	Queued   bool
	Channel  string
}

func flat[T any](p *T) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprint(*p)
}

// tapStep is one §5 situation, driven identically on any harness. ctr is the plaque's
// last spent counter value, shared across the steps of one harness.
type tapStep struct {
	name string
	run  func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen
}

// viaPage drives one tap the way a phone does (2nd round, F1): GET the plaque URL with
// the person's cookie, take the context the SERVER minted on that page, and POST it
// back from addr. It is the GET -> mint -> POST seam qr_db_test.go established, so a
// suspension read anywhere on the PAGE's path (internal/domain/tenant's
// Directory.TapPage, the tap handler) shows up here, not only one on the check-in's.
//
// stipulateCMAC is the one bit an NFC tap needs and the page cannot produce: the URL
// carries a fixed wrong MAC (no chip here), so the page's context says the MAC did not
// verify; the bit is flipped and the context re-signed, exactly as seedflow's tapNFC
// does. Without it the step IS §5 row 2.
//
// A page that mints no context posts nothing; the step then reports the page alone.
func viaPage(t *testing.T, h *tapHarness, employee uuid.UUID, cookie *http.Cookie, target string, stipulateCMAC bool, addr string, extra url.Values) tapSeen {
	t.Helper()
	rows, alerts := h.countFor(t, employee), h.auditCount(t, "tap.security_alert")
	page := h.get(t, target, cookie)
	s := tapSeen{PageStatus: page.Code, PageLocation: page.Header().Get("Location"), PageBody: pageText(h, page.Body.String())}
	if m := ctxFieldRE.FindStringSubmatch(page.Body.String()); m != nil {
		s.PageMinted = true
		signed := m[1]
		if stipulateCMAC {
			sid := h.sessionIDOf(t, cookie)
			decoded, err := h.tap.contexts.parse(signed, sid)
			if err != nil {
				t.Fatalf("GET %s: the page's own context did not parse: %v", target, err)
			}
			decoded.CMACVerified = true
			if signed, err = h.tap.contexts.mint(decoded, sid); err != nil {
				t.Fatalf("re-sign: %v", err)
			}
		}
		w := h.postSigned(t, signed, cookie, addr, extra)
		s.Status, s.Location, s.Body = w.Code, w.Header().Get("Location"), pageText(h, w.Body.String())
	}
	s.Rows, s.Alerts = h.countFor(t, employee)-rows, h.auditCount(t, "tap.security_alert")-alerts
	if s.Rows > 0 {
		r := h.lastRecord(t, employee)
		s.Verdict, s.Trust, s.Type, s.Note = r.Verdict, flat(r.Trust), flat(r.Type), flat(r.Note)
		s.Sid, s.Layer, s.Practice, s.Queued, s.Channel = flat(r.MatchedSid), flat(r.PolicyLayer), r.Practice, r.Queued, r.Channel
	}
	return s
}

// holdingStep is the holding cross-tap (M11, ADR 0027 §11: "askı dokunuşu durdurmaz --
// holding dokunuşu dahil"): the business under test joins a holding with two fresh
// peers, and two of its people tap -- through the page -- one plaque of each peer. The
// first peer is never suspended; the second MIRRORS the harness (suspended exactly when
// the business under test is), so on the suspended side both the tapping business and
// the plaque's business are suspended. The two taps are folded into one observation.
func holdingStep(t *testing.T, h *tapHarness, _ *uint32) tapSeen {
	t.Helper()
	owner := holdingOwner(t)
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	var mirrored bool
	if err := owner.QueryRow(context.Background(), `SELECT suspended_at IS NOT NULL FROM tenants WHERE id = $1`,
		h.tenantID).Scan(&mirrored); err != nil {
		t.Fatalf("read the harness's suspension: %v", err)
	}
	open, mirror := uuid.New(), uuid.New()
	openUID := h.newTag(t, kek, open, uuid.New(), 100)
	mirrorUID := h.newTag(t, kek, mirror, uuid.New(), 100)
	if mirrored {
		suspendAsOwner(t, ownerPoolForTest(t), mirror, db.SuspensionNonPayment)
	}
	holdingOf(t, owner, h.tenantID, open, mirror)
	var seen []tapSeen
	for _, uid := range []string{openUID, mirrorUID} {
		e := h.newEmployee(t, "active")
		seen = append(seen, viaPage(t, h, e, h.cookieForEmployee(t, e), nfcURL(uid, 101, zeroCMAC), true, onSiteAddr, nil))
	}
	if seen[0].Rows != 1 || seen[1].Rows != 1 {
		t.Errorf("anti-vacuity: the holding taps wrote %d and %d row(s), want 1 and 1", seen[0].Rows, seen[1].Rows)
	}
	return folded(seen[0], seen[1])
}

// The parts of a tap answer that differ between two identical businesses by
// construction, not by decision: the signed context (it carries ids and the session's
// MAC), the per-response CSP nonce, ids, plaque uids, counter values and clock
// readings. Everything else -- every word, every class, every link that is not one of
// those -- is compared verbatim.
var (
	pageCtxRE   = regexp.MustCompile(`name="ctx" value="[^"]*"`)
	pageNonceRE = regexp.MustCompile(`nonce="[^"]*"`)
	pageCSRFRE  = regexp.MustCompile(`name="csrf" value="[^"]*"`)
	pageUUIDRE  = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	pageUIDRE   = regexp.MustCompile(`\b[0-9A-F]{14}\b`)
	pageCtrRE   = regexp.MustCompile(`ctr=[0-9A-Fa-f]+`)
	pageClockRE = regexp.MustCompile(`\b[0-2]?[0-9]:[0-5][0-9](:[0-5][0-9])?\b`)
)

// pageText is a tap answer's body with the construction-dependent parts replaced by
// placeholders (above); the harness's own plaque uid is replaced by name too.
func pageText(h *tapHarness, body string) string {
	body = strings.ReplaceAll(body, h.tagUID, "<UID>")
	body = pageCtxRE.ReplaceAllString(body, `name="ctx" value="<CTX>"`)
	body = pageNonceRE.ReplaceAllString(body, `nonce="<NONCE>"`)
	body = pageCSRFRE.ReplaceAllString(body, `name="csrf" value="<CSRF>"`)
	body = pageUUIDRE.ReplaceAllString(body, "<ID>")
	body = pageUIDRE.ReplaceAllString(body, "<UID>")
	body = pageCtrRE.ReplaceAllString(body, "ctr=<CTR>")
	return pageClockRE.ReplaceAllString(body, "<CLOCK>")
}

// firstDifference shows where two normalised bodies first disagree: a short window of
// each around the first differing byte (a rendered page is often one line), or "same".
func firstDifference(a, b string) string {
	if a == b {
		return "same"
	}
	at := 0
	for at < len(a) && at < len(b) && a[at] == b[at] {
		at++
	}
	window := func(x string) string {
		lo, hi := max(at-60, 0), min(at+60, len(x))
		return x[lo:hi]
	}
	return fmt.Sprintf("at byte %d: unsuspended %q, suspended %q", at, window(a), window(b))
}

// nextCtr spends the plaque's next counter value.
func nextCtr(ctr *uint32) uint32 { *ctr++; return *ctr }

// folded is a two-tap step's two observations as one comparable value: the second tap's,
// carrying the first's counts and page facts, so a difference in EITHER tap shows.
func folded(first, second tapSeen) tapSeen {
	second.Rows += first.Rows * 100
	second.Alerts += first.Alerts * 100
	second.PageStatus += first.PageStatus * 1000
	second.PageMinted = second.PageMinted && first.PageMinted
	second.Status += first.Status * 1000
	if first.Verdict != second.Verdict {
		second.Verdict = first.Verdict + "+" + second.Verdict
	}
	second.PageBody = first.PageBody + "\n----\n" + second.PageBody
	second.Body = first.Body + "\n----\n" + second.Body
	return second
}

// suspensionTapSteps are §5's seven rows on both channels and the holding cross-tap,
// each for a FRESH person so
// one step's debounce or direction never reaches the next, every one of them through
// the plaque's page.
func suspensionTapSteps() []tapStep {
	person := func(t *testing.T, h *tapHarness, status string) (uuid.UUID, *http.Cookie) {
		t.Helper()
		e := h.newEmployee(t, status)
		return e, h.cookieForEmployee(t, e)
	}
	where := func(onSite, gps bool) (string, url.Values) {
		addr, extra := offSiteAddr, url.Values(nil)
		if onSite {
			addr = onSiteAddr
		}
		if gps {
			extra = venueFix()
		}
		return addr, extra
	}
	nfc := func(onSite, gps bool) func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen {
		return func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen {
			e, c := person(t, h, "active")
			addr, extra := where(onSite, gps)
			return viaPage(t, h, e, c, nfcURL(h.tagUID, nextCtr(ctr), zeroCMAC), true, addr, extra)
		}
	}
	qr := func(onSite, gps bool) func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen {
		return func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen {
			e, c := person(t, h, "active")
			addr, extra := where(onSite, gps)
			return viaPage(t, h, e, c, qrURLFor(h.tagUID), false, addr, extra)
		}
	}
	return []tapStep{
		{"row 6: NFC, IP", nfc(true, false)},
		{"row 6: NFC, IP and GPS", nfc(true, true)},
		{"row 6: NFC, GPS only", nfc(false, true)},
		{"row 7: NFC, no evidence", nfc(false, false)},
		{"row 6: QR, IP", qr(true, false)},
		{"QR, GPS only (base:qr-requires-ip)", qr(false, true)},
		{"row 7: QR, no evidence", qr(false, false)},
		{"row 5: NFC, the same person again", func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen {
			e, c := person(t, h, "active")
			first := viaPage(t, h, e, c, nfcURL(h.tagUID, nextCtr(ctr), zeroCMAC), true, onSiteAddr, nil)
			return folded(first, viaPage(t, h, e, c, nfcURL(h.tagUID, nextCtr(ctr), zeroCMAC), true, onSiteAddr, nil))
		}},
		{"row 5: QR, the same person again", func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen {
			e, c := person(t, h, "active")
			first := viaPage(t, h, e, c, qrURLFor(h.tagUID), false, onSiteAddr, nil)
			return folded(first, viaPage(t, h, e, c, qrURLFor(h.tagUID), false, onSiteAddr, nil))
		}},
		{"row 4: a deactivated person", func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen {
			e, c := person(t, h, "deactivated")
			return viaPage(t, h, e, c, nfcURL(h.tagUID, nextCtr(ctr), zeroCMAC), true, onSiteAddr, nil)
		}},
		{"row 2: CMAC did not verify", func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen {
			e, c := person(t, h, "active")
			// No stipulation: the page's own verdict on the wrong MAC goes to the POST, and
			// the counter it names is not spent (§4.4), so the next step may use it.
			return viaPage(t, h, e, c, nfcURL(h.tagUID, *ctr+1, zeroCMAC), false, onSiteAddr, nil)
		}},
		{"row 2: a replayed counter", func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen {
			e, c := person(t, h, "active")
			return viaPage(t, h, e, c, nfcURL(h.tagUID, *ctr, zeroCMAC), true, onSiteAddr, nil)
		}},
		{"row 1: a lost plaque, NFC and QR", func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen {
			lost := h.loadedPlaque(t, "lost", h.locationID)
			e, c := person(t, h, "active")
			first := viaPage(t, h, e, c, nfcURL(lost, 1, zeroCMAC), true, onSiteAddr, nil)
			return folded(first, viaPage(t, h, e, c, qrURLFor(lost), false, onSiteAddr, nil))
		}},
		{"holding: a peer's plaque, and a suspended peer's", holdingStep},
		{"row 3: no session, the page", func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen {
			before := h.transactionCount(t)
			page := h.get(t, nfcURL(h.tagUID, *ctr+1, zeroCMAC))
			return tapSeen{PageStatus: page.Code, PageLocation: page.Header().Get("Location"),
				PageMinted: ctxFieldRE.MatchString(page.Body.String()), Rows: h.transactionCount(t) - before}
		}},
		{"row 3: no session, the check-in", func(t *testing.T, h *tapHarness, ctr *uint32) tapSeen {
			before := h.transactionCount(t)
			req := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader("ctx=whatever"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.RemoteAddr = onSiteAddr
			w := httptest.NewRecorder()
			h.router.ServeHTTP(w, req)
			return tapSeen{Status: w.Code, Location: w.Header().Get("Location"), Rows: h.transactionCount(t) - before}
		}},
	}
}

// TestSuspensionDB_TapsAreDecidedAsIfThereWereNoSuspension is ADR 0025 §4's measure,
// CLAUDE.md §4.6 end to end: two identical businesses -- same plaque counter, same
// venue, same configuration -- one of them suspended; every row of §5 on both channels
// is driven through the plaque's PAGE (GET /t, the server's own context) and POST
// /api/checkin on each, and each step must leave the two exactly alike: the page's
// status, redirect and whether it minted a context; the check-in's status and redirect;
// rows written; security alerts; and the last row's verdict, trust, direction, note,
// rule id, layer, practice, queue and channel.
//
// The table is not vacuous in either direction: the suspended business's columns are
// asserted set, and the steps produce ok, flag, ignored, reject, a page that mints and
// row 3's redirect to activation (checked below), so a suspension that changed ANY
// outcome -- on the page or on the check-in -- would show. A tap path that read the
// suspension (a guardrail, a refused page, a different note) turns this red, whether or
// not it NAMES the suspension (TestSuspension_TheTapPathNeverNamesASuspension is the
// structural half, and sees only names).
func TestSuspensionDB_TapsAreDecidedAsIfThereWereNoSuspension(t *testing.T) {
	owner := ownerPoolForTest(t)
	open := newTapHarness(t)
	suspended := newTapHarness(t)
	suspendAsOwner(t, owner, suspended.tenantID, db.SuspensionSecurityHold)
	var isSuspended bool
	if err := owner.QueryRow(context.Background(),
		`SELECT suspended_at IS NOT NULL AND suspension_reason IS NOT NULL FROM tenants WHERE id = $1`,
		suspended.tenantID).Scan(&isSuspended); err != nil || !isSuspended {
		t.Fatalf("control: the second business is not suspended (%v)", err)
	}

	// BOTH BUSINESSES ARE BRANDED (logo + accent), so the tap and result screens draw
	// the business's name, logo and accent -- the screen content a suspension must not
	// change (§4, CLAUDE.md §9) is on the page to be compared.
	for _, h := range []*tapHarness{open, suspended} {
		h.brandBusiness(t, h.tenantID, 31, "DA291C")
	}

	openCtr, suspendedCtr := uint32(open.startCtr), uint32(suspended.startCtr)
	outcomes := map[string]bool{}
	for _, step := range suspensionTapSteps() {
		a := step.run(t, open, &openCtr)
		b := step.run(t, suspended, &suspendedCtr)
		if !reflect.DeepEqual(a, b) {
			pa, pb, ra, rb := a.PageBody, b.PageBody, a.Body, b.Body
			a.PageBody, b.PageBody, a.Body, b.Body = "", "", "", ""
			t.Errorf("%s:\n  unsuspended %+v\n  suspended   %+v\n  tap page: %s\n  result:   %s",
				step.name, a, b, firstDifference(pa, pb), firstDifference(ra, rb))
		}
		t.Logf("%-38s page %d minted=%v -> %d %-8s trust=%s sid=%s rows=%d alerts=%d",
			step.name, a.PageStatus, a.PageMinted, a.Status, a.Verdict, a.Trust, a.Sid, a.Rows, a.Alerts)
		for _, v := range strings.Split(a.Verdict, "+") {
			outcomes[v] = true
		}
		if a.PageMinted {
			outcomes["page minted"] = true
		}
		for _, loc := range []string{a.Location, a.PageLocation} {
			if loc != "" {
				outcomes["redirect "+loc] = true
			}
		}
	}
	// ANTI-VACUITY for the screen comparison: the first step's tap page carries the
	// business's name, its logo, its accent, the venue and the one button, and its
	// result screen the confirmation -- all compared verbatim above.
	first := suspensionTapSteps()[0].run(t, open, &openCtr)
	for _, want := range []string{"Kebab Factory Ltd", "/t/logo/", "/brand/theme/DA291C.css", "Joseph", "St Julians", ">Tap<"} {
		if !strings.Contains(first.PageBody, want) {
			t.Errorf("anti-vacuity: the compared tap page lacks %q", want)
		}
	}
	if !strings.Contains(first.Body, "All done") {
		t.Errorf("anti-vacuity: the compared result screen lacks the confirmation")
	}
	for _, want := range []string{"ok", "flag", "ignored", "reject", "page minted", "redirect /activate?from=tap"} {
		if !outcomes[want] {
			t.Errorf("no step produced %q; the table is not exercising §5 the way it claims (%v)", want, outcomes)
		}
	}
	if openCtr != suspendedCtr+1 {
		t.Errorf("the two plaques spent %d and %d counter values", openCtr, suspendedCtr)
	}
}

// TestSuspensionDB_TheActivationTapIsTheSameForASuspendedBusiness is ADR 0025 §4's
// activation half under ADR 0026: an invitation issued, the wizard walked (welcome,
// the privacy notice, consent), and the FIRST REAL NFC TAP -- the step that activates
// (no transactions row) -- on two businesses, one of them suspended. Both land on the
// same answer: the tap's status, the person active, the invitation spent, one session
// issued, no attendance row -- and the SAME SCREENS, compared normalised (pageText):
// the employer's name in the GDPR notice, the venue's network on step 3, the
// "Activation complete" page.
func TestSuspensionDB_TheActivationTapIsTheSameForASuspendedBusiness(t *testing.T) {
	type seen struct {
		Status, TxDelta int
		Screens         [4]string
	}
	run := func(suspend bool) seen {
		h := newHarness(t, "invited")
		if suspend {
			suspendAsOwner(t, ownerPoolForTest(t), h.tenantID, db.SuspensionSecurityHold)
		}
		c := h.client(t)
		code := h.issue(t)
		var out seen
		welcome, err := c.Get(h.server.URL + "/activate?code=" + url.QueryEscape(code))
		if err != nil {
			t.Fatalf("open link: %v", err)
		}
		out.Screens[0] = pageText(h.tapHarness, body(t, welcome))
		privacy := h.getBody(t, c, "/activate?step=2")
		out.Screens[1] = pageText(h.tapHarness, privacy)
		resp, err := c.PostForm(h.server.URL+"/api/activate", url.Values{"consent": {"yes"}, "csrf": {formToken(t, privacy)}})
		if err != nil {
			t.Fatalf("consent: %v", err)
		}
		out.Screens[2] = pageText(h.tapHarness, body(t, resp))
		before := h.transactionCount(t)
		tapResp, page := h.tapWith(t, c, h.nextTap(t))
		out.Screens[3] = pageText(h.tapHarness, page)
		h.assertEmployeeStatus(t, "active")
		h.assertInviteConsumed(t, true)
		h.assertSessionCount(t, 1)
		out.Status, out.TxDelta = tapResp.StatusCode, h.transactionCount(t)-before
		return out
	}
	open, suspended := run(false), run(true)
	if open.Status != suspended.Status || open.TxDelta != suspended.TxDelta {
		t.Errorf("the activating tap: unsuspended %d / %d row(s), suspended %d / %d row(s)",
			open.Status, open.TxDelta, suspended.Status, suspended.TxDelta)
	}
	for i, name := range []string{"welcome", "privacy notice", "consent landing (step 3)", "activation complete"} {
		if open.Screens[i] != suspended.Screens[i] {
			t.Errorf("the %s screen differs for a suspended business: %s", name, firstDifference(open.Screens[i], suspended.Screens[i]))
		}
	}
	// ANTI-VACUITY: what the comparison is about is on the screens.
	for i, want := range []string{"Maria Borg", "Kebab Factory Ltd", "KF-StJulians-Staff", "Activation complete"} {
		if !strings.Contains(open.Screens[i], want) {
			t.Errorf("anti-vacuity: screen %d lacks %q", i, want)
		}
	}
	if open.Status != http.StatusOK || open.TxDelta != 0 {
		t.Errorf("anti-vacuity: the unsuspended activation is %d / %d row(s), want 200 / 0", open.Status, open.TxDelta)
	}
}
