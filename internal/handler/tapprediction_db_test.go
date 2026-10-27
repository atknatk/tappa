package handler

// tapprediction_db_test.go -- M15-12 through the REAL router, the REAL directory, the REAL SUN
// preview and real Postgres (ADR 0032 §B.1-§B.3; security lens G-O3, F-1; K5): the tap screen says
// the engine's direction on both channels, refuses every other shape of the gate with the
// direction-neutral "Tap", asks the ONE switch the record asks, and on a peer's plaque says the
// EMPLOYER's chain. Every case is a fresh employee of the harness's tenant with a business day far
// from now (h.newEmployee, fixtures.GiveFarDayCutoff); the shared seed is not written.

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/web/templates/pages"
)

// nfcPage is a GENUINE NFC URL for uid (its CMAC verifies under the fixture's tag key), at a
// counter above the harness's start. The preview spends nothing, so one counter serves every GET.
func nfcPage(t *testing.T, uid string, ctr int32) string {
	t.Helper()
	return signedTapURL(t, tapFakeTagKey, uid, uint32(ctr)+1)
}

// pageFor GETs target with cookie and returns the body, failing on anything but a 200.
func (h *tapHarness) pageFor(t *testing.T, target string, cookie *http.Cookie) string {
	t.Helper()
	w := h.get(t, target, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d:\n%s", target, w.Code, w.Body.String())
	}
	return w.Body.String()
}

// withSwitch points the harness's directory AND its orchestrator at the business-day switch from
// (nil: unset) for the rest of the test -- the two surfaces cmd/tappa wires from the one config
// field. The process environment is not touched.
func (h *tapHarness) withSwitch(t *testing.T, from *time.Time) {
	t.Helper()
	dir, err := tenant.NewDirectory(h.data, from)
	if err != nil {
		t.Fatal(err)
	}
	cfg := *h.cfg
	cfg.BusinessDayFrom = from
	svc, err := checkin.New(h.data, h.trail, &cfg, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	prevDir, prevSvc := h.tap.directory, h.tap.checkins
	h.tap.directory, h.tap.checkins = dir, svc
	t.Cleanup(func() { h.tap.directory, h.tap.checkins = prevDir, prevSvc })
}

// TestTapDB_SaysTheEnginesDirectionOnBothChannels is the card's positive criterion (K5): a valid
// QR context and a verified NFC URL, an active plaque, an active person -- with no open check-in the
// page says "Clock in" and "Not right? Clock out"; with an open check-in an hour old, "Clock out"
// and "Not right? Clock in". The pages write no record and spend no counter.
func TestTapDB_SaysTheEnginesDirectionOnBothChannels(t *testing.T) {
	h := newTapHarness(t)
	emp := h.newEmployee(t, "active")
	cookie := h.cookieForEmployee(t, emp)
	targets := map[string]string{"NFC": nfcPage(t, h.tagUID, h.startCtr), "QR": qrURLFor(h.tagUID)}
	before, ctr := h.transactionCount(t), h.lastCtr(t, h.tagUID)

	for _, target := range targets {
		assertDirection(t, h.pageFor(t, target, cookie), pages.TapClockIn)
	}
	h.seedTapAgedBy(t, emp, time.Hour, "in", false)
	for channel, target := range targets {
		body := h.pageFor(t, target, cookie)
		if !strings.Contains(body, clockOutButton+correctToIn) {
			t.Errorf("%s with an open check-in: not \"Clock out\" + \"Not right? Clock in\"", channel)
		}
		assertDirection(t, body, pages.TapClockOut)
	}
	if after := h.transactionCount(t); after != before+1 {
		t.Errorf("transactions %d -> %d: only the seeded check-in may have been written, never by GET /t", before, after)
	}
	if got := h.lastCtr(t, h.tagUID); got != ctr {
		t.Errorf("last_ctr %d -> %d: the page spent the chip's counter (§4.4)", ctr, got)
	}
}

// TestTapDB_EveryOtherShapeOfTheGateIsTheUndirectedTap is the card's negative criteria (G-O3,
// F-1), each against a person with an open check-in -- so a leak would say "Clock out": a CMAC
// that does not verify; a retired, lost and unassigned plaque on NFC (a genuine URL for it) and on
// QR; a deactivated person on NFC and on QR; the switch unset, and set an hour after the server's
// clock. Each is the direction-neutral "Tap" with no want and nothing under it.
func TestTapDB_EveryOtherShapeOfTheGateIsTheUndirectedTap(t *testing.T) {
	h := newTapHarness(t)
	emp := h.newEmployee(t, "active")
	cookie := h.cookieForEmployee(t, emp)
	h.seedTapAgedBy(t, emp, time.Hour, "in", false)
	// CONTROL: the same person, plaque and history say "Clock out" through the open gate.
	assertDirection(t, h.pageFor(t, nfcPage(t, h.tagUID, h.startCtr), cookie), pages.TapClockOut)

	t.Run("a CMAC that does not verify", func(t *testing.T) {
		assertDirection(t, h.pageFor(t, tapURLFor(h.tagUID), cookie), pages.TapUndirected)
	})
	for _, status := range []string{"retired", "lost", "unassigned"} {
		wall := h.locationID
		if status == "unassigned" {
			wall = uuid.Nil
		}
		uid := h.loadedPlaque(t, status, wall)
		t.Run("a "+status+" plaque, NFC", func(t *testing.T) {
			assertDirection(t, h.pageFor(t, nfcPage(t, uid, 0), cookie), pages.TapUndirected)
		})
		t.Run("a "+status+" plaque, QR", func(t *testing.T) {
			assertDirection(t, h.pageFor(t, qrURLFor(uid), cookie), pages.TapUndirected)
		})
	}
	gone := h.newEmployee(t, "deactivated")
	goneCookie := h.cookieForEmployee(t, gone)
	h.seedTapAgedBy(t, gone, time.Hour, "in", false)
	t.Run("a deactivated person, NFC", func(t *testing.T) {
		assertDirection(t, h.pageFor(t, nfcPage(t, h.tagUID, h.startCtr), goneCookie), pages.TapUndirected)
	})
	t.Run("a deactivated person, QR", func(t *testing.T) {
		assertDirection(t, h.pageFor(t, qrURLFor(h.tagUID), goneCookie), pages.TapUndirected)
	})
	future := time.Now().Add(time.Hour)
	for name, from := range map[string]*time.Time{"the switch unset": nil, "the server's clock before the switch": &future} {
		t.Run(name, func(t *testing.T) {
			h.withSwitch(t, from)
			for _, target := range []string{nfcPage(t, h.tagUID, h.startCtr), qrURLFor(h.tagUID)} {
				assertDirection(t, h.pageFor(t, target, cookie), pages.TapUndirected)
			}
		})
	}
}

// TestTapDB_ThePageAndTheRecordAskTheOneSwitch is the card's single-gate criterion through both
// surfaces: in each of the switch's three modes the page's prediction and the POST's want are
// judged by the one gate (tap.SwitchedDayCutoff, asked by the directory and by the orchestrator).
// The person has no open check-in (the engine's D is `in`) and presses "Not right? Clock out" on
// the page's own signed context: with the switch on the page is directed and the record is `out`,
// 'employee'; unset or ahead of the server's clock, the page is the undirected "Tap" and the same
// want is the engine's `in`, 'auto'. Closing the gate in one place turns both halves of the "on"
// row red together.
func TestTapDB_ThePageAndTheRecordAskTheOneSwitch(t *testing.T) {
	h := newTapHarness(t)
	future := time.Now().Add(time.Hour)
	for i, m := range []struct {
		name   string
		from   *time.Time
		page   pages.TapDirection
		dir    string
		source string
	}{
		{"on", h.cfg.BusinessDayFrom, pages.TapClockIn, "out", "employee"},
		{"unset", nil, pages.TapUndirected, "in", "auto"},
		{"ahead of the server's clock", &future, pages.TapUndirected, "in", "auto"},
	} {
		t.Run(m.name, func(t *testing.T) {
			h.withSwitch(t, m.from)
			emp := h.newEmployee(t, "active")
			cookie := h.cookieForEmployee(t, emp)
			target := nfcPage(t, h.tagUID, h.startCtr+int32(i))
			body := h.pageFor(t, target, cookie)
			assertDirection(t, body, m.page)
			w := h.postSigned(t, contextFieldOf(t, body), cookie, onSiteAddr, url.Values{"want": {"out"}})
			if w.Code != http.StatusOK {
				t.Fatalf("POST = %d", w.Code)
			}
			rec := h.lastRecord(t, emp)
			if src := h.sourceOf(t, rec.ID); deref(rec.Type) != m.dir || src == nil || *src != m.source {
				t.Fatalf("recorded %s/%v, want %s/%s", deref(rec.Type), src, m.dir, m.source)
			}
		})
	}
}

// TestTapDB_OnAPeersPlaqueThePredictionIsTheEmployers is the card's peer-plaque criterion (ADR 0032
// §B.2, ADR 0027 §7): the harness's person (the employer's) holds an open check-in in the
// EMPLOYER's records and stands in front of a verified NFC plaque of a peer business -- the page
// names the peer's venue and says "Clock out": the employer's chain, read on the session's
// context. CONTROL: before the two are peers the page is the foreign-plaque page and says "Tap".
func TestTapDB_OnAPeersPlaqueThePredictionIsTheEmployers(t *testing.T) {
	h := newTapHarness(t)
	owner := holdingOwner(t)
	kek := h.cfg.TagKEK
	kf, kfVenue := uuid.New(), uuid.New()
	kfUID := h.newTag(t, kek, kf, kfVenue, 100)
	if _, err := owner.Exec(t.Context(), `UPDATE locations SET name = 'KF Sliema' WHERE id = $1`, kfVenue); err != nil {
		t.Fatal(err)
	}
	cookie := h.sessionCookieFor(t)
	h.openCheckInAgedBy(t, time.Hour)
	target := nfcPage(t, kfUID, 100)

	foreign := h.pageFor(t, target, cookie)
	if strings.Contains(foreign, "KF Sliema") {
		t.Fatal("anti-vacuity: a plaque of a business that is not a peer named its venue")
	}
	assertDirection(t, foreign, pages.TapUndirected)

	holdingOf(t, owner, h.tenantID, kf)
	body := h.pageFor(t, target, cookie)
	if !strings.Contains(body, "KF Sliema") {
		t.Fatalf("the peer's venue is not on the page:\n%s", body)
	}
	assertDirection(t, body, pages.TapClockOut)
}
