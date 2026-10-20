package handler

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
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
	Status       int
	Location     string
	Rows         int
	Alerts       int
	Verdict      string
	Trust        string
	Type         string
	Note         string
	Sid          string
	Layer        string
	Practice     bool
	Queued       bool
	Channel      string
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
	s := tapSeen{PageStatus: page.Code, PageLocation: page.Header().Get("Location")}
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
		s.Status, s.Location = w.Code, w.Header().Get("Location")
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

	openCtr, suspendedCtr := uint32(open.startCtr), uint32(suspended.startCtr)
	outcomes := map[string]bool{}
	for _, step := range suspensionTapSteps() {
		a := step.run(t, open, &openCtr)
		b := step.run(t, suspended, &suspendedCtr)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s:\n  unsuspended %+v\n  suspended   %+v", step.name, a, b)
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
	for _, want := range []string{"ok", "flag", "ignored", "reject", "page minted", "redirect /activate?from=tap"} {
		if !outcomes[want] {
			t.Errorf("no step produced %q; the table is not exercising §5 the way it claims (%v)", want, outcomes)
		}
	}
	if openCtr != suspendedCtr {
		t.Errorf("the two plaques spent %d and %d counter values", openCtr, suspendedCtr)
	}
}

// TestSuspensionDB_TheActivationTapIsTheSameForASuspendedBusiness is ADR 0025 §4's
// activation half under ADR 0026: an invitation issued, consent recorded in the wizard,
// and the FIRST REAL NFC TAP -- the step that activates (no transactions row) -- on two
// businesses, one of them suspended. Both land on the same answer: the tap's status and
// "Activation complete", the person active, the invitation spent, one session issued, and
// no attendance row.
func TestSuspensionDB_TheActivationTapIsTheSameForASuspendedBusiness(t *testing.T) {
	type seen struct {
		Status, TxDelta int
		Complete        bool
	}
	run := func(suspend bool) seen {
		h := newHarness(t, "invited")
		if suspend {
			suspendAsOwner(t, ownerPoolForTest(t), h.tenantID, db.SuspensionSecurityHold)
		}
		c := h.client(t)
		h.consent(t, c, h.issue(t))
		before := h.transactionCount(t)
		resp, page := h.tapWith(t, c, h.nextTap(t))
		h.assertEmployeeStatus(t, "active")
		h.assertInviteConsumed(t, true)
		h.assertSessionCount(t, 1)
		return seen{Status: resp.StatusCode, TxDelta: h.transactionCount(t) - before, Complete: strings.Contains(page, "Activation complete")}
	}
	open, suspended := run(false), run(true)
	if open != suspended {
		t.Errorf("the activating tap: unsuspended %+v, suspended %+v", open, suspended)
	}
	if !open.Complete || open.Status != http.StatusOK || open.TxDelta != 0 {
		t.Errorf("anti-vacuity: the unsuspended activation is %+v, want 200, complete, no row", open)
	}
}
