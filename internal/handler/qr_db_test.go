package handler

// THE QR CHANNEL, END TO END, over HTTP, against a REAL Postgres (M5-08).
//
// WHY THIS FILE EXISTS WHEN "THE QR CHANNEL ALREADY WORKED". Before it, every
// piece of the QR path was proven and the PATH was not:
//
//	sun.Parse produces Channel=qr from a URL with no ctr/cmac   (internal/sun)
//	preview.go short-circuits QR before any cryptography         (internal/sun)
//	base:qr-requires-ip flags a QR tap with no IP match          (internal/policy)
//	tap.Decide honours it                                        (internal/domain/tap)
//	POST /api/checkin records channel=qr                         (checkin_db_test.go)
//
// The last of those posts a context a TEST built by hand (h.qrContext()). NOTHING
// drove GET /t with a real QR URL — measured: every /t?tag=… in this package
// carried &ctr=&cmac=. So the one seam that turns "a plaque URL with no SUN
// parameters" into "a recorded QR tap" — sun.Parse -> Preview -> mint -> POST —
// had no test on it at all, which is the shape the M5-05 audit named: a guarantee
// proven in package A and consumed in package B, with nothing pinning that B
// reaches it. Every test below starts at GET /t and carries the SERVER'S OWN
// minted context to the POST.
//
// Fixtures are NOT cleaned up (tappa_app has REVOKE DELETE on tags and
// transactions). Fresh random ids per run keep them from colliding.
//
// ON PRINTING w.Body.String() IN FAILURE MESSAGES, raised in review and MEASURED
// rather than argued: every such print here is guarded by `status != 200`, and the
// ONLY template that carries a signed tap context is pages.Tap, which renders 200.
// So no failure path in this file can put a context on the wire — the bodies that
// reach a t.Fatalf are Problem screens, which carry fixed copy and nothing else.
// (There is also precedent: checkin_db_test.go, tap_db_test.go and tour_db_test.go
// read or print bodies the same way.) Kept, because a 400 whose body is not shown
// costs a debugging round trip on the most consequential path in the product.

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/httpx"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/web/templates/pages"
)

// qrURLFor is a plaque URL with NO SUN parameters — what a printed QR code
// encodes. The chip rewrites the NFC URL on every read; nothing rewrites this
// one, which is the whole of the difference (ADR 0003 §1, skill tappa-sun).
func qrURLFor(uid string) string { return "/t?tag=" + uid }

// signedContextFrom renders GET target and returns the ctx field the page put in
// front of the person. The tests below post THAT value rather than one they
// minted, so the assertion covers the GET -> mint -> POST seam and not only the
// POST. (contextFieldOf is tap_test.go's reader; the field is the same field.)
func (h *tapHarness) signedContextFrom(t *testing.T, target string, cookie *http.Cookie) string {
	t.Helper()
	w := h.get(t, target, cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200\n%s", target, w.Code, w.Body.String())
	}
	return contextFieldOf(t, w.Body.String())
}

// postSigned posts an ALREADY-SIGNED context — the one the page rendered.
// remoteAddr chooses whether the source IP falls inside the fixture location's
// 203.0.113.0/24, which is the whole of the "where" evidence for a QR tap.
func (h *tapHarness) postSigned(t *testing.T, signed string, cookie *http.Cookie, remoteAddr string, extra url.Values) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"ctx": {signed}}
	for k, vs := range extra {
		form[k] = vs
	}
	req := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	return w
}

// Addresses used below. onSiteAddr is inside the fixture location's registered
// prefix; offSiteAddr is not, and is the mobile-data case a QR user on the street
// produces.
const (
	onSiteAddr  = "203.0.113.5:41234"
	offSiteAddr = "198.51.100.9:51000"
)

// venueFix is the fixture location's own coordinate (newTag inserts 35.918 /
// 14.489), so a fix at this point is INSIDE the 150 m radius.
func venueFix() url.Values { return url.Values{"lat": {"35.918"}, "lng": {"14.489"}} }

// --- criterion 1: the URL with no SUN really does become a QR tap ------------

// TestQRDB_PlaqueURLWithNoSUNBecomesARecordedQRTap is the acceptance criterion
// end to end: GET /t?tag=<uid> with no ctr and no cmac -> the tap page -> POST
// /api/checkin -> a row whose channel is 'qr' and whose sun_valid is false.
//
// It also asserts the two ABSENCES that distinguish this channel from NFC: the
// row carries no counter, and tags.last_ctr does not move. A QR scan has no
// counter to spend, which is the fact hand-off 6 (state.md, M5-05 audits) is
// about and which the repeat test below measures the consequences of.
func TestQRDB_PlaqueURLWithNoSUNBecomesARecordedQRTap(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)
	before := h.countFor(t, h.employeeID)
	ctrBefore := h.lastCtr(t, h.tagUID)

	signed := h.signedContextFrom(t, qrURLFor(h.tagUID), cookie)

	// THE SEAM: the channel in the context was derived by sun.Parse from the
	// ABSENCE of ctr/cmac in the URL, on the server, at page-build time. Decoding
	// it here proves GET /t produced it rather than the test supplying it.
	decoded, err := h.tap.contexts.parse(signed, h.sessionIDOf(t, cookie))
	if err != nil {
		t.Fatalf("the page's own context did not parse: %v", err)
	}
	if decoded.Channel != sun.ChannelQR {
		t.Fatalf("GET /t?tag=<uid> minted channel %q, want %q", decoded.Channel, sun.ChannelQR)
	}
	if decoded.CMACVerified {
		t.Fatal("a QR arrival carries no CMAC, so CMACVerified must be false")
	}
	if decoded.Ctr != 0 {
		t.Fatalf("a QR arrival carries no counter, got ctr = %d", decoded.Ctr)
	}

	w := h.postSigned(t, signed, cookie, onSiteAddr, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("POST status = %d, want 200\n%s", w.Code, w.Body.String())
	}
	if after := h.countFor(t, h.employeeID); after != before+1 {
		t.Fatalf("rows went %d -> %d, want +1", before, after)
	}

	rec := h.lastRecord(t, h.employeeID)
	if rec.Channel != "qr" {
		t.Fatalf("channel column = %q, want \"qr\"", rec.Channel)
	}
	if rec.SunValid == nil || *rec.SunValid {
		t.Fatalf("sun_valid = %v, want false: a QR tap carries no proof of moment", rec.SunValid)
	}
	if rec.Ctr != nil {
		t.Fatalf("ctr = %d, want NULL: there is no counter on this channel", *rec.Ctr)
	}
	// An IP match is the one thing base:qr-requires-ip asks for, so this is the
	// ordinary good QR tap.
	if rec.Verdict != "ok" || rec.MatchedSid == nil || *rec.MatchedSid != policy.SidIPOrGPSOK {
		t.Fatalf("verdict/sid = %q/%v, want ok/%s", rec.Verdict, deref(rec.MatchedSid), policy.SidIPOrGPSOK)
	}
	if rec.Trust == nil || *rec.Trust != 70 {
		t.Fatalf("trust = %v, want 70 (20 base + 50 IP)", rec.Trust)
	}
	if got := h.lastCtr(t, h.tagUID); got != ctrBefore {
		t.Fatalf("tags.last_ctr moved %d -> %d on a QR tap: there is no counter to advance", ctrBefore, got)
	}
}

// --- criterion 2: IP is mandatory on this channel (Q15) ----------------------

// TestQRDB_WithoutAnIPMatchGPSAloneIsNotEnough is Q15 measured on both channels
// with the SAME evidence, which is the only way to show that the difference is
// the channel and not the fix.
//
// IT IS ALSO THE MEASUREMENT THAT REWROTE A CARD CRITERION. The M5-08 card used
// to say "QR never reaches the same TRUST LEVEL as NFC"; these three subtests
// showed the literal reading is false — the trust SCORE is identical in both
// halves (§5: 20 + 50 IP + 30 GPS, no channel term, deliberately: trust measures
// EVIDENCE, not outcome). What differs is the EVIDENCE CEILING: with the same
// GPS-only fix a QR tap can never be an `ok` without a human, and an NFC tap can.
// Product decision 2026-08-01: §5's formula stands, the criterion was reworded to
// the ceiling reading, and surfacing transactions.channel next to trust is M6's
// (M6-01 / M6-04) — otherwise a manager reads 100 and assumes NFC.
func TestQRDB_WithoutAnIPMatchGPSAloneIsNotEnough(t *testing.T) {
	t.Run("qr: GPS only -> flag, and the row exists", func(t *testing.T) {
		h := newTapHarness(t)
		cookie := h.sessionCookieFor(t)
		before := h.countFor(t, h.employeeID)

		signed := h.signedContextFrom(t, qrURLFor(h.tagUID), cookie)
		w := h.postSigned(t, signed, cookie, offSiteAddr, venueFix())
		if w.Code != http.StatusOK {
			t.Fatalf("POST status = %d, want 200\n%s", w.Code, w.Body.String())
		}
		if after := h.countFor(t, h.employeeID); after != before+1 {
			t.Fatalf("rows went %d -> %d, want +1: a flag is RECORDED (§4.6)", before, after)
		}
		rec := h.lastRecord(t, h.employeeID)
		if rec.Channel != "qr" {
			t.Fatalf("channel = %q, want \"qr\"", rec.Channel)
		}
		if rec.Verdict != "flag" || rec.MatchedSid == nil || *rec.MatchedSid != policy.SidQRRequiresIP {
			t.Fatalf("verdict/sid = %q/%v, want flag/%s", rec.Verdict, deref(rec.MatchedSid), policy.SidQRRequiresIP)
		}
		if rec.IPMatch == nil || *rec.IPMatch {
			t.Fatalf("ip_match = %v, want false", rec.IPMatch)
		}
		if rec.GPSMatch == nil || !*rec.GPSMatch {
			t.Fatalf("gps_match = %v, want true: the fix IS inside the radius, and it still is not enough", rec.GPSMatch)
		}
		if rec.Trust == nil || *rec.Trust != 50 {
			t.Fatalf("trust = %v, want 50 (20 base + 30 GPS)", rec.Trust)
		}
	})

	// POSITIVE CONTROL, and the honest reading of "QR never reaches NFC's trust".
	// Identical evidence — same venue fix, same off-site address, same employee —
	// on the NFC channel is ALLOWED, by base:gps-only-allow, with the SAME trust
	// score of 50. The number does not separate the channels; the verdict does.
	t.Run("nfc: the same GPS-only evidence is ok, at the same trust score", func(t *testing.T) {
		h := newTapHarness(t)
		cookie := h.sessionCookieFor(t)

		signed := h.signedContextFrom(t, tapURLFor(h.tagUID), cookie)
		// tapURLFor's CMAC is deliberately wrong, so the page's context says
		// CMACVerified=false and the tap would be a sun-invalid reject. Mint the
		// NFC context the way a GENUINE chip URL would produce it, keeping every
		// other fact identical to the QR half.
		nfc := h.nfcContext(uint32(h.startCtr) + 1)
		if _, err := h.tap.contexts.parse(signed, h.sessionIDOf(t, cookie)); err != nil {
			t.Fatalf("the NFC page's own context did not parse: %v", err)
		}
		w := h.postFrom(t, nfc, cookie, offSiteAddr, venueFix())
		if w.Code != http.StatusOK {
			t.Fatalf("POST status = %d, want 200\n%s", w.Code, w.Body.String())
		}
		rec := h.lastRecord(t, h.employeeID)
		if rec.Channel != "nfc" {
			t.Fatalf("channel = %q, want \"nfc\"", rec.Channel)
		}
		if rec.Verdict != "ok" || rec.MatchedSid == nil || *rec.MatchedSid != policy.SidGPSOnlyAllow {
			t.Fatalf("verdict/sid = %q/%v, want ok/%s", rec.Verdict, deref(rec.MatchedSid), policy.SidGPSOnlyAllow)
		}
		if rec.Trust == nil || *rec.Trust != 50 {
			t.Fatalf("trust = %v, want 50: the trust formula (§5) carries NO channel term", rec.Trust)
		}
	})

	// The other end of the same measurement: with IP AND GPS, a QR tap scores
	// exactly what an NFC tap scores. 100 == 100. Whoever reads a trust score
	// cannot tell the channels apart from it; transactions.channel is what does.
	t.Run("full evidence: qr and nfc score identically", func(t *testing.T) {
		h := newTapHarness(t)
		cookie := h.sessionCookieFor(t)

		signed := h.signedContextFrom(t, qrURLFor(h.tagUID), cookie)
		if w := h.postSigned(t, signed, cookie, onSiteAddr, venueFix()); w.Code != http.StatusOK {
			t.Fatalf("qr POST status = %d", w.Code)
		}
		qr := h.lastRecord(t, h.employeeID)

		other := h.newEmployee(t, "active")
		otherCookie := h.cookieForEmployee(t, other)
		if w := h.postFrom(t, h.nfcContext(uint32(h.startCtr)+1), otherCookie, onSiteAddr, venueFix()); w.Code != http.StatusOK {
			t.Fatalf("nfc POST status = %d", w.Code)
		}
		nfc := h.lastRecord(t, other)

		if qr.Trust == nil || nfc.Trust == nil || *qr.Trust != 100 || *nfc.Trust != 100 {
			t.Fatalf("trust qr=%v nfc=%v, want 100 and 100", qr.Trust, nfc.Trust)
		}
		if qr.Channel != "qr" || nfc.Channel != "nfc" {
			t.Fatalf("channels = %q/%q, want qr/nfc", qr.Channel, nfc.Channel)
		}
	})
}

// --- criterion 3: the channel is the server's word, not the caller's ---------

// TestQRDB_TheChannelIsServerDerivedOnEveryDeclarationSurface drives hand-off N2
// to the COLUMN. The existing coverage (TestCheckin_TagFactsComeFromTheSignedContextOnly)
// checks ONE form field against a FAKE service; this tries five surfaces in BOTH
// directions and reads transactions.channel back from Postgres.
//
// Both directions matter and they buy different things. nfc -> qr is the exploit
// hand-off N2 names: a caller who could relabel a failed SUN as a QR tap would
// shed sys:sun-invalid, which is NFC-only. qr -> nfc is the mirror: a caller who
// could claim nfc would shed base:qr-requires-ip and turn a GPS-only scan from a
// flag into an ok.
//
// THE CASE-VARIANT SURFACE IS NOT PADDING. Three separate audits in this repo
// found a check and its consumer looking at different REPRESENTATIONS of one
// value (state.md: HTTPS:// , Sec-Fetch-Site: Cross-Site, ::ffff:0.0.0.0/96), so
// "channel" is also sent as "Channel" and "CHANNEL".
func TestQRDB_TheChannelIsServerDerivedOnEveryDeclarationSurface(t *testing.T) {
	surfaces := []struct {
		name  string
		apply func(req *http.Request, form url.Values, claim string)
	}{
		{"form field", func(_ *http.Request, form url.Values, claim string) {
			form.Set("channel", claim)
		}},
		{"form field, other cases", func(_ *http.Request, form url.Values, claim string) {
			form.Set("Channel", claim)
			form.Set("CHANNEL", claim)
			form.Set("channel", strings.ToUpper(claim))
		}},
		{"query string", func(req *http.Request, _ url.Values, claim string) {
			req.URL.RawQuery = "channel=" + claim + "&sun_valid=true"
		}},
		{"header", func(req *http.Request, _ url.Values, claim string) {
			req.Header.Set("X-Tappa-Channel", claim)
			req.Header.Set("Channel", claim)
		}},
	}

	for _, arrival := range []struct {
		name    string
		url     func(uid string) string
		want    string
		claimed string
	}{
		{"an NFC page relabelled qr", tapURLFor, "nfc", "qr"},
		{"a QR page relabelled nfc", qrURLFor, "qr", "nfc"},
	} {
		for _, s := range surfaces {
			t.Run(arrival.name+" via "+s.name, func(t *testing.T) {
				h := newTapHarness(t)
				cookie := h.sessionCookieFor(t)
				signed := h.signedContextFrom(t, arrival.url(h.tagUID), cookie)

				form := url.Values{"ctx": {signed}}
				spec := httptest.NewRequest(http.MethodPost, "/api/checkin", http.NoBody)
				s.apply(spec, form, arrival.claimed)

				w := httptest.NewRecorder()
				h.router.ServeHTTP(w, buildRequest(t, spec, form, cookie))
				if w.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200\n%s", w.Code, w.Body.String())
				}
				if got := h.lastRecord(t, h.employeeID).Channel; got != arrival.want {
					t.Fatalf("channel column = %q, want %q: a client declaration reached the record", got, arrival.want)
				}
			})
		}
	}

	// TWO MORE SURFACES FAIL DIFFERENTLY, AND THE DIFFERENCE WAS MEASURED RATHER
	// THAN ASSUMED. The first version of this test posted the multipart case
	// expecting "served, declaration ignored" and got 400 instead. The mechanism:
	// Checkin calls r.ParseForm() first, which leaves r.PostForm NON-NIL AND EMPTY
	// for a multipart body — and PostFormValue only reaches ParseMultipartForm
	// when PostForm is nil. So a multipart POST loses `ctx` too and is refused as
	// an unusable context; a JSON body never had a `ctx` field to begin with.
	//
	// Both are recorded as an ASSERTED SHAPE, not as a defence: neither writes a
	// row, so neither can carry a declaration into one. If this endpoint ever
	// learns to read multipart, these two cases must move up into the table above.
	for _, refused := range []struct {
		name        string
		contentType string
		body        func(signed string) string
	}{
		{"multipart body", "", nil}, // content type is filled in below
		{"json body", "application/json", func(signed string) string {
			return `{"ctx":"` + signed + `","channel":"nfc","sun_valid":true}`
		}},
	} {
		t.Run(refused.name+" is refused and writes nothing", func(t *testing.T) {
			h := newTapHarness(t)
			cookie := h.sessionCookieFor(t)
			signed := h.signedContextFrom(t, qrURLFor(h.tagUID), cookie)
			before := h.countFor(t, h.employeeID)

			var body, contentType string
			if refused.body != nil {
				body, contentType = refused.body(signed), refused.contentType
			} else {
				body, contentType = multipartBody(t, url.Values{"ctx": {signed}, "channel": {"nfc"}})
			}
			req := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader(body))
			req.Header.Set("Content-Type", contentType)
			req.RemoteAddr = onSiteAddr
			req.AddCookie(cookie)
			w := httptest.NewRecorder()
			h.router.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", w.Code)
			}
			if after := h.countFor(t, h.employeeID); after != before {
				t.Fatalf("rows went %d -> %d, want unchanged", before, after)
			}
		})
	}
}

// buildRequest turns the spec request (headers and query only) plus a form into
// the urlencoded request that is actually served.
func buildRequest(t *testing.T, spec *http.Request, form url.Values, cookie *http.Cookie) *http.Request {
	t.Helper()
	target := "/api/checkin"
	if spec.URL.RawQuery != "" {
		target += "?" + spec.URL.RawQuery
	}
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header = spec.Header.Clone()
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = onSiteAddr
	req.AddCookie(cookie)
	return req
}

// multipartBody encodes form as multipart/form-data and returns it with its
// content type.
func multipartBody(t *testing.T, form url.Values) (string, string) {
	t.Helper()
	var buf strings.Builder
	mw := multipart.NewWriter(&buf)
	for k, vs := range form {
		for _, v := range vs {
			if err := mw.WriteField(k, v); err != nil {
				t.Fatalf("multipart field: %v", err)
			}
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}
	return buf.String(), mw.FormDataContentType()
}

// --- the measurement hand-off 6 asked for -----------------------------------

// qrRepeatPosts is how many times one scanned QR context is spent below. It is
// not a threshold anything enforces — it is a sample size, chosen large enough
// that "the context is single-use" would be impossible to miss.
const qrRepeatPosts = 12

// TestQRDB_OneScanIsSpendableRepeatedlyWhileOneTouchIsNot is the number state.md
// hand-off 6 (M5-05 audits) asked M5-08 to produce, plus the NFC control that
// gives it meaning.
//
// 🔴 WHAT IT MEASURES, AND WHY IT DOES NOT FIX IT. §5 says "the next transaction
// already requires a new physical touch". On NFC that is STRUCTURAL: the atomic
// advance of tags.last_ctr spends the counter value, so the second POST of one
// context is a recorded replay reject. ON QR THERE IS NO COUNTER TO SPEND, so the
// sentence does not hold: within the signed context's TTL, every POST is served
// and every POST writes a row. The only thing between them is the 60 s
// person-debounce, which turns the extras into `ignored` — recorded, attributable,
// and not counted toward hours, but not refused either.
//
// 🔴 M5-10 DID NOT NARROW THIS, AND AN EARLIER VERSION OF THIS PARAGRAPH SAID IT
// WOULD ("M5-10 sets the window and the exposure shrinks with it"). It does not,
// and the reason is deliberate: sys:tap-freshness is NFC-ONLY. It reads
// tap:channel first and returns false for `qr`, because §5 says a photographed
// code has no touch to be stale relative to and is valid indefinitely — M3-05
// wrote the guardrail that way on purpose and M5-10 rightly left it. Measured by
// TestCheckinDB_StaleQRPageIsNotDeniedByFreshness: the same page age that is a
// recorded sys:tap-freshness reject on NFC is an ordinary `ok` on QR.
//
// SO THE QR CEILING IS STILL WHAT THIS TEST RECORDS: one scanned context stays
// postable for the signed context's full 15-minute TTL, and the brakes are
// base:qr-requires-ip, the 60 s person-debounce (extras become RECORDED
// `ignored`), and the per-session and per-address rate limits. Narrowing it is
// a §5 product decision about QR, not a freshness setting.
func TestQRDB_OneScanIsSpendableRepeatedlyWhileOneTouchIsNot(t *testing.T) {
	t.Run("qr: one scan, many rows", func(t *testing.T) {
		h := newTapHarness(t)
		cookie := h.sessionCookieFor(t)
		before := h.countFor(t, h.employeeID)
		ctrBefore := h.lastCtr(t, h.tagUID)

		signed := h.signedContextFrom(t, qrURLFor(h.tagUID), cookie)
		verdicts := map[string]int{}
		for i := 0; i < qrRepeatPosts; i++ {
			w := h.postSigned(t, signed, cookie, onSiteAddr, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("post %d status = %d, want 200: the context is still valid\n%s", i+1, w.Code, w.Body.String())
			}
			verdicts[h.lastRecord(t, h.employeeID).Verdict]++
		}
		if after := h.countFor(t, h.employeeID); after != before+qrRepeatPosts {
			t.Fatalf("rows went %d -> %d, want +%d: every accepted POST writes one row",
				before, after, qrRepeatPosts)
		}
		if verdicts["ok"] != 1 || verdicts["ignored"] != qrRepeatPosts-1 {
			t.Fatalf("verdicts = %v, want 1 ok and %d ignored", verdicts, qrRepeatPosts-1)
		}
		if got := h.lastCtr(t, h.tagUID); got != ctrBefore {
			t.Fatalf("tags.last_ctr moved %d -> %d: a QR scan has no counter", ctrBefore, got)
		}
	})

	// THE CONTROL. Identical loop, NFC channel, and the outcome is different in
	// kind: the first POST spends the counter and every later one is a recorded
	// replay reject. This is the guarantee QR structurally cannot offer.
	t.Run("nfc: one touch, one usable POST", func(t *testing.T) {
		h := newTapHarness(t)
		cookie := h.sessionCookieFor(t)
		ctrBefore := h.lastCtr(t, h.tagUID)
		nfc := h.nfcContext(uint32(h.startCtr) + 1)

		verdicts := map[string]int{}
		sids := map[string]int{}
		for i := 0; i < qrRepeatPosts; i++ {
			w := h.postFrom(t, nfc, cookie, onSiteAddr, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("post %d status = %d, want 200", i+1, w.Code)
			}
			rec := h.lastRecord(t, h.employeeID)
			verdicts[rec.Verdict]++
			sids[deref(rec.MatchedSid)]++
		}
		if verdicts["reject"] != qrRepeatPosts-1 || sids["sys:sun-invalid"] != qrRepeatPosts-1 {
			t.Fatalf("verdicts=%v sids=%v, want %d reject/sys:sun-invalid after the first",
				verdicts, sids, qrRepeatPosts-1)
		}
		if got := h.lastCtr(t, h.tagUID); got != ctrBefore+1 {
			t.Fatalf("tags.last_ctr = %d, want %d: exactly one advance for one touch", got, ctrBefore+1)
		}
	})
}

// backdatedRepeatPosts is the sample size for the measurement below. Nothing
// enforces it; it only has to be large enough that "the debounce brakes this"
// would be impossible to miss.
const backdatedRepeatPosts = 20

// TestQRDB_ABackdatedOccurredAtNoLongerDefeatsTheDebounce is ADR 0006's
// acceptance test, and it is the third answer this file has given to the same
// question — the first two are recorded because the arc is the finding.
//
//	v1  computed a ceiling: "TTL 900 s / debounce 60 s => at most 16
//	    direction-carrying rows from one scan". WRONG: it assumed the debounce
//	    spaces repeats out in WALL-CLOCK time.
//	v2  measured the truth: decide.go compared in.Now (server) against
//	    LastForPerson.OccurredAt (CLIENT-DECLARED), so a repeat declaring a past
//	    time reported a huge gap and sys:person-debounce NEVER FIRED. One scanned
//	    QR context posted 20 times produced 20 rows, ALL carrying a direction,
//	    zero ignored, in 0.51 s of wall clock.
//	v3  (here) the rule now takes the SMALLER of the declared distance and the
//	    distance to the previous row's SERVER-written created_at, so the same 20
//	    posts produce ONE counted row and 19 `ignored` ones.
//
// EVERY ROW IS STILL WRITTEN (§4.6): 20 posts, 20 rows. What changed is that 19
// of them are `ignored` instead of counting, which is what §5 row 5 always
// intended and what the client-declarable input was quietly undoing.
//
// 🔴 THE NFC HALF IS THE CONTROL AND IT IS UNCHANGED, because NFC was never
// exposed: the atomic counter advance turns the second POST of one context into a
// replay reject regardless of any timestamp. That asymmetry is precisely why this
// surfaced on the channel with no counter.
func TestQRDB_ABackdatedOccurredAtNoLongerDefeatsTheDebounce(t *testing.T) {
	t.Run("qr: one counted row, the rest ignored", func(t *testing.T) {
		h := newTapHarness(t)
		// 🔴 THE FIXTURE HAS HISTORY, and an audit is why. The first version used a
		// fresh employee with NO prior record, and it passed for that reason alone:
		// with nothing to sort against, a backdated row still becomes the newest by
		// occurred_at and the predecessor advances normally. Give the person one
		// ordinary tap first and the SELECTION hole opens — every backdated row
		// sorts BENEATH it, the predecessor freezes, and 20 posts produced 20
		// counted rows. A test whose name claims a general guarantee needs a
		// general fixture.
		emp := h.newEmployee(t, "active")
		h.seedTapAgedBy(t, emp, 30*time.Minute, "in", false)
		cookie := h.cookieForEmployee(t, emp)
		before := h.countFor(t, emp)
		ctrBefore := h.lastCtr(t, h.tagUID)
		signed := h.signedContextFrom(t, qrURLFor(h.tagUID), cookie)

		// 71 hours: inside the 72 h sys:occurred-at-bound ceiling, and BELOW the
		// person's newest row without needing to know anything about their history.
		past := time.Now().UTC().Add(-71 * time.Hour).Format(time.RFC3339)
		verdicts, directional := map[string]int{}, 0
		for i := 0; i < backdatedRepeatPosts; i++ {
			w := h.postSigned(t, signed, cookie, onSiteAddr, url.Values{"occurred_at": {past}})
			if w.Code != http.StatusOK {
				t.Fatalf("post %d status = %d, want 200", i+1, w.Code)
			}
			rec := h.lastRecord(t, emp)
			verdicts[rec.Verdict]++
			if rec.Type != nil {
				directional++
			}
		}
		// §4.6 first: nothing is refused, everything is recorded.
		if after := h.countFor(t, emp); after != before+backdatedRepeatPosts {
			t.Fatalf("rows went %d -> %d, want +%d: every POST is still RECORDED", before, after, backdatedRepeatPosts)
		}
		if directional != 1 {
			t.Fatalf("%d of %d rows carried a direction, want exactly 1: neither the declared distance "+
				"nor the declared ORDERING may switch the debounce off (ADR 0006)", directional, backdatedRepeatPosts)
		}
		if verdicts["ignored"] != backdatedRepeatPosts-1 {
			t.Fatalf("verdicts = %v, want %d ignored", verdicts, backdatedRepeatPosts-1)
		}
		if got := h.lastCtr(t, h.tagUID); got != ctrBefore {
			t.Fatalf("tags.last_ctr moved %d -> %d: a QR scan has no counter", ctrBefore, got)
		}
	})

	// 🔴 LAYER 3 — THE SIGN. One future-dated POST is REFUSED by
	// sys:occurred-at-bound and, because §4.6 records every decided tap, the
	// refusal is written WITH that future stamp. It then wins ORDER BY occurred_at
	// for as long as the future lasts, and `now − occurred_at` is NEGATIVE. The
	// guardrail matches on `gap >= 0 && gap < window`, so before ADR 0006 a single
	// poisoned row switched the debounce off for that person entirely — and the
	// taps that followed were not even flagged: measured, 20 honest taps declaring
	// NOTHING came back as 20 plain `ok` rows in 0.29 s. No manager ever sees that.
	t.Run("a future-dated row must not switch the debounce off", func(t *testing.T) {
		h := newTapHarness(t)
		emp := h.newEmployee(t, "active")
		cookie := h.cookieForEmployee(t, emp)
		signed := h.signedContextFrom(t, qrURLFor(h.tagUID), cookie)

		future := time.Now().UTC().Add(48 * time.Hour).Format(time.RFC3339)
		if w := h.postSigned(t, signed, cookie, onSiteAddr, url.Values{"occurred_at": {future}}); w.Code != http.StatusOK {
			t.Fatalf("poison POST status = %d, want 200 (a denied tap is RECORDED)", w.Code)
		}
		poison := h.lastRecord(t, emp)
		if poison.Verdict != "reject" || deref(poison.MatchedSid) != "sys:occurred-at-bound" {
			t.Fatalf("poison verdict/sid = %q/%q, want reject/sys:occurred-at-bound",
				poison.Verdict, deref(poison.MatchedSid))
		}
		if !poison.OccurredAt.After(time.Now().UTC().Add(24 * time.Hour)) {
			t.Fatalf("the recorded row does not carry the future stamp (%s): this case is not set up",
				poison.OccurredAt)
		}

		// Now ordinary, honest taps: no declared time at all, which is the shape
		// the tap page posts. They arrive within a second of each other and of the
		// poison, so §5 row 5 says every one of them is a duplicate.
		//
		// EXPECTED: ZERO counted rows, and every one `ignored` VIA THE DEBOUNCE.
		// The verdict matters as much as the count — asserting only "nothing
		// counted" would also pass if the direction had gone missing for some
		// unrelated reason. Before ADR 0006 this measured 20 counted `ok` rows.
		//
		// ⚠️ THE POISON ROW ITSELF COUNTS AS RECENT ACTIVITY, deliberately. It is a
		// REJECT, and the server leg does not filter on verdict — because a
		// rejected tap is still a physical touch by this person, and because
		// filtering it out would hand a caller a way to keep the leg empty by
		// generating rejects. See ADR 0006.
		verdicts, directional := map[string]int{}, 0
		debounced := 0
		for i := 0; i < backdatedRepeatPosts; i++ {
			if w := h.postSigned(t, signed, cookie, onSiteAddr, nil); w.Code != http.StatusOK {
				t.Fatalf("post %d status = %d, want 200", i+1, w.Code)
			}
			rec := h.lastRecord(t, emp)
			verdicts[rec.Verdict]++
			if rec.Type != nil {
				directional++
			}
			if deref(rec.MatchedSid) == "sys:person-debounce" {
				debounced++
			}
		}
		if directional != 0 {
			t.Fatalf("%d of %d honest taps carried a direction, want 0: one future-dated row "+
				"switched the guardrail off (ADR 0006, layer 3)", directional, backdatedRepeatPosts)
		}
		if debounced != backdatedRepeatPosts {
			t.Fatalf("verdicts=%v, %d of %d decided by sys:person-debounce, want all: the direction is "+
				"missing for some OTHER reason, so this case is not measuring the guardrail",
				verdicts, debounced, backdatedRepeatPosts)
		}
	})

	// THE CONTROL, and the reason this is a QR finding rather than a general one.
	t.Run("nfc: the same trick is stopped by the counter", func(t *testing.T) {
		h := newTapHarness(t)
		cookie := h.sessionCookieFor(t)
		ctrBefore := h.lastCtr(t, h.tagUID)
		nfc := h.nfcContext(uint32(h.startCtr) + 1)

		past := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
		sids, directional := map[string]int{}, 0
		for i := 0; i < backdatedRepeatPosts; i++ {
			w := h.postFrom(t, nfc, cookie, onSiteAddr, url.Values{"occurred_at": {past}})
			if w.Code != http.StatusOK {
				t.Fatalf("post %d status = %d, want 200", i+1, w.Code)
			}
			rec := h.lastRecord(t, h.employeeID)
			sids[deref(rec.MatchedSid)]++
			if rec.Type != nil {
				directional++
			}
		}
		if directional != 1 {
			t.Fatalf("%d rows carried a direction on NFC, want exactly 1: the counter is the brake", directional)
		}
		if sids["sys:sun-invalid"] != backdatedRepeatPosts-1 {
			t.Fatalf("sids = %v, want %d sys:sun-invalid", sids, backdatedRepeatPosts-1)
		}
		if got := h.lastCtr(t, h.tagUID); got != ctrBefore+1 {
			t.Fatalf("tags.last_ctr = %d, want %d", got, ctrBefore+1)
		}
	})
}

// TestQRDB_TheExposureCeilingIsTheSessionBudget pins what really bounds the reuse
// of one scanned QR, now that the debounce has been measured out of the answer.
//
// THE BUDGET IS A BURST, NOT A RATE, and that distinction is the whole number:
// httpx.Limiter is a FIXED WINDOW, so a session may spend its entire allowance
// immediately. Measured through the mounted router with GET /t (which writes no
// transactions row): 300 requests served back to back in ~1.2 s, and the 301st
// answered 429.
//
// So one scanned QR, inside the 15-minute context TTL, is bounded by
// httpx.TapSessionLimit() per 10-minute window — two windows fit inside the TTL,
// so the structural ceiling is about 600 requests, minus the one GET that minted
// the context. That is roughly 599 direction-carrying rows, not 16.
//
// AND M5-10 DOES NOT REDUCE IT AT ALL — not "to 4", and not by half either,
// which is what the second version of this paragraph guessed. sys:tap-freshness
// is NFC-ONLY (it returns false for tap:channel == "qr", because §5 makes a
// photographed code valid indefinitely), so the shipped 180 s window never
// touches a QR context. Measured: TestCheckinDB_StaleQRPageIsNotDeniedByFreshness
// posts a QR page at an age that is a recorded reject on NFC and gets `ok`.
//
// So the ~600 figure above stands as written, and the things that actually bound
// it are base:qr-requires-ip, the 60 s person-debounce and these limits. The
// debounce already measures against SERVER time (ADR 0006: the smaller of the
// declared gap and clock_timestamp() - created_at), so an earlier note here
// naming that as the outstanding fix is spent — it landed. What is left is a
// product decision about QR itself: a shorter context TTL, a single-use nonce
// per scan, or accepting the ceiling as ADR 0005 does.
func TestQRDB_TheExposureCeilingIsTheSessionBudget(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)

	if tapContextTTL != 15*time.Minute {
		t.Fatalf("tapContextTTL = %v, want 15m: the exposure window in the M5-08 card is derived from it", tapContextTTL)
	}
	// The shipped debounce is still 60 s. It is asserted so that the card's claim
	// "the debounce is NOT what bounds this" keeps naming a real number.
	if got := policy.DefaultParams().DebounceWindow; got != 60*time.Second {
		t.Fatalf("shipped debounce = %v, want 60s", got)
	}

	limit := httpx.TapSessionLimit()
	served, refused, firstRefusalAt := 0, 0, 0
	start := time.Now()
	for i := 1; i <= limit+3; i++ {
		w := h.get(t, qrURLFor(h.tagUID), cookie)
		switch w.Code {
		case http.StatusTooManyRequests:
			refused++
			if firstRefusalAt == 0 {
				firstRefusalAt = i
			}
		case http.StatusOK:
			served++
		default:
			t.Fatalf("request %d status = %d, want 200 or 429", i, w.Code)
		}
	}
	if served != limit || firstRefusalAt != limit+1 {
		t.Fatalf("served=%d refused=%d first429=%d, want %d served and the %dth refused",
			served, refused, firstRefusalAt, limit, limit+1)
	}
	// The burst half: the whole allowance was spendable far inside one window.
	if elapsed := time.Since(start); elapsed > 5*time.Minute {
		t.Fatalf("spending the budget took %s: it is being throttled as a rate, not a burst", elapsed)
	}
}

// --- what nobody says to the person tapping ---------------------------------

// TestQRScreens_SayNothingAboutTheQRRoute measures the state the M5-08 card's
// "QR is a permanent route, not a fallback" criterion is satisfied by TODAY: no
// user-visible string on any screen in this package mentions QR, scanning or a
// camera at all. Nothing frames the daily route of an employee whose phone cannot
// read a tag in the background as a failure, because nothing names it.
//
// 🔴 THIS TEST IS A TRIPWIRE, NOT AN OBJECTION TO THE COPY, AND IT HAS A KNOWN
// EXPIRY. If a screen ever gains a sentence about the printed code, this fails —
// and the failure is the point: the tap and activation screens are §9's sacred
// flow, so the wording is a product-owner decision measured against the card's
// "never second-class" rule. Turn this into a positive assertion at that moment;
// do not just delete it.
//
// WHEN THAT MOMENT IS, decided 2026-08-01: WHEN THE PLAQUES ARE ACTUALLY PRINTED
// WITH A QR (M8). The copy was deliberately NOT added now, because the code does
// not physically exist yet and a screen must not describe something that is not
// there. The measured defect it leaves standing is written down rather than
// fixed: tour slide 1 says "If your phone does not react, ask your manager", and
// for an iPhone X the phone never reacts — that sentence frames someone's DAILY
// route as a fault. Owner of the fix: M8, together with skill tappa-brand's
// plaque-print section, the three whitelists in result_test.go and the tour tests.
//
// ⚠️ MEASURED LIMIT, and it is the whole reason the word list is short: the flow
// ALREADY uses "fall back" legitimately, on pages.Activate, about GPS — "Without
// the network, taps fall back to your location". Forbidding failure vocabulary in
// general therefore produces a false red, so this checks the ONE vocabulary that
// can only be about this route.
//
// The screen list is the same MANUAL, per-screen list result_test.go's three
// whitelists keep (their stated limit): a template added later is not covered
// until somebody writes it here too.
func TestQRScreens_SayNothingAboutTheQRRoute(t *testing.T) {
	t.Parallel()
	// Lower-cased. `\bqr\b` so it cannot collide with an ordinary word; the rest
	// are the phrases a sentence about a printed code would have to use.
	forbidden := regexp.MustCompile(`\bqr\b|scan the|barcode|point your camera|with your camera`)

	problems := map[string]pages.ProblemView{
		"tapProblemBadURL": tapProblemBadURL, "tapProblemUnknownTag": tapProblemUnknownTag,
		"tapProblemServer": tapProblemServer, "tapProblemTooMany": tapProblemTooMany,
		"tapProblemStale": tapProblemStale, "tapProblemForeignTenant": tapProblemForeignTenant,
		"problemNoLink": problemNoLink, "problemBadLink": problemBadLink,
		"problemTooMany": problemTooMany, "problemNoSession": problemNoSession,
		"problemServer": problemServer,
	}
	screens := map[string]templ.Component{
		"pages.Tap": pages.Tap(pages.TapView{EmployeeName: "Maria Borg", LocationName: "St Julians", TapContext: "x.y"}),
		"pages.Activate": pages.Activate(pages.ActivateView{
			EmployeeName: "Maria Borg", EmployerName: "Kebab Factory Ltd", LocationName: "St Julians",
			WiFiSSID: "KF-Guest", RetentionYears: 2, CSRFToken: "t",
		}),
		"pages.Confirm": pages.Confirm(pages.ConfirmView{
			Code: "ABCD-EFGH", EmployeeName: "Maria Borg", EmployerName: "Kebab Factory Ltd",
		}),
		"pages.Done": pages.Done(pages.DoneView{
			EmployeeName: "Maria Borg", LocationName: "St Julians", WiFiSSID: "KF-Guest",
		}),
	}
	for step := 1; step <= pages.TourSteps; step++ {
		screens["pages.Tour step "+strconv.Itoa(step)] = pages.Tour(pages.TourView{Step: step})
	}
	for name, v := range problems {
		for _, retry := range []string{"", problemRetryURL} {
			view := v
			view.RetryURL = retry
			screens[name+" retry="+strconv.Quote(retry)] = pages.Problem(view)
		}
	}

	check := func(name, body string) {
		t.Helper()
		if m := forbidden.FindString(strings.ToLower(screenText(t, body))); m != "" {
			t.Errorf("%s now says %q. Nothing mentioned the printed code before, so this is new copy on a §9 screen: "+
				"get the wording approved and make sure it never reads as a lesser route (M5-08 card), then pin it here.", name, m)
		}
	}
	for name, c := range screens {
		var sb strings.Builder
		if err := c.Render(context.Background(), &sb); err != nil {
			t.Fatalf("render %s: %v", name, err)
		}
		check(name, sb.String())
	}
	// The confirmation screen in every shape it renders, driven the way
	// result_test.go drives it.
	for _, verdict := range []string{"ok", "flag", "reject", "ignored"} {
		for _, practice := range []bool{false, true} {
			check("pages.Result "+verdict, mustBody(t, checkin.Result{
				Outcome:      checkin.OutcomeRecorded,
				Decision:     withNote(decisionOf(verdict, "in", practice), "note"),
				LocationName: "St Julians", Timezone: "Europe/Malta", BusinessType: "restaurant",
			}))
		}
	}
}

// --- a hand-off M5-09 needs measured rather than assumed ---------------------

// TestQRDB_ABrokenKeyRefBlocksNFCButNotQR is the practical consequence of the
// short-circuit internal/sun's TestPreview_QRURLNeverUnwrapsTheTagKey pins, at
// the HTTP level — and it settles a question M5-09 ("simulate a day") walks into.
//
// state.md hand-off 5 (M5-04 audits) records that the seeded plaques carry an
// aes_key_ref that is NOT KEK-wrapped, so the NFC path answers 500 for them. What
// nobody had measured is whether that also kills the QR path. It does not: a QR
// URL never reaches the unwrap, so a plaque with an unopenable key still serves a
// tap page and still records a QR tap. Which means the seeded fixtures can drive
// the QR half of a full-day simulation TODAY, while the NFC half stays blocked
// until the seed wraps its keys properly.
func TestQRDB_ABrokenKeyRefBlocksNFCButNotQR(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)
	h.corruptKeyRef(t, h.tagUID)

	// NFC: the unwrap is on the path, so the page cannot be built.
	if w := h.get(t, tapURLFor(h.tagUID), cookie); w.Code != http.StatusInternalServerError {
		t.Fatalf("NFC GET status = %d, want 500 over an unopenable key ref", w.Code)
	}

	// QR: no unwrap, so the page renders and the tap records normally.
	before := h.countFor(t, h.employeeID)
	signed := h.signedContextFrom(t, qrURLFor(h.tagUID), cookie)
	if w := h.postSigned(t, signed, cookie, onSiteAddr, nil); w.Code != http.StatusOK {
		t.Fatalf("QR POST status = %d, want 200\n%s", w.Code, w.Body.String())
	}
	if after := h.countFor(t, h.employeeID); after != before+1 {
		t.Fatalf("rows went %d -> %d, want +1", before, after)
	}
	if rec := h.lastRecord(t, h.employeeID); rec.Channel != "qr" || rec.Verdict != "ok" {
		t.Fatalf("channel/verdict = %q/%q, want qr/ok", rec.Channel, rec.Verdict)
	}
}

// corruptKeyRef flips one byte of a plaque's wrapped key so the KEK can no longer
// open it. The value is already opaque ciphertext and nothing is printed.
//
// 🔴 IT RUNS AS tappa_owner, AND IT HAD TO CHANGE FOR MIGRATION 00013 (backlog
// T9). Until then tappa_app held table-wide UPDATE on `tags`, so this helper --
// the application role -- could rewrite aes_key_ref. That is precisely the
// capability T9 measured and named: overwrite a plaque's wrapped key and every
// tap on it answers 500, silently, forever. 00013 revokes the table privilege and
// grants back only (location_id, last_ctr, status, retired_at, replaced_by), so
// this statement now fails with 42501 as tappa_app.
//
// The failure of this helper IS the proof the grant works end to end, so the fix
// is NOT to widen the grant: the test wants to simulate a CORRUPTED row, which is
// an act of the database and not of the application, and the role that can do it
// is the migration owner. If a future change makes this pass as tappa_app again,
// T9 has been reopened.
func (h *tapHarness) corruptKeyRef(t *testing.T, uid string) {
	t.Helper()
	owner := ownerPoolForTest(t)
	_, err := owner.Exec(context.Background(),
		`UPDATE tags SET aes_key_ref = set_byte(aes_key_ref, 0, get_byte(aes_key_ref, 0) # 255)
		 WHERE tenant_id = $1 AND uid = $2`, h.tenantID, uid)
	if err != nil {
		t.Fatalf("corrupt key ref: %v", err)
	}
}

// TestQRDB_AManualPredecessorDoesNotDebounceARealTap pins ADR 0006's ONE
// exemption, from both sides, against real Postgres.
//
// 🔴 WHY THE EXEMPTION EXISTS. The server leg asks "when was this person's last
// tap WRITTEN". On a manual row that column is when a MANAGER TYPED IT, which
// carries no claim about where the employee was — so counting it would swallow an
// employee's genuine tap moments after a manager entered a backdated shift, and
// would debounce the second row of a bulk manual entry against the first.
//
// 🔴 AND WHY IT IS NOT A WAY OUT FOR A CALLER: channel is server-derived (no
// client surface can set it — five are measured elsewhere in this file) and a
// manual row additionally requires entered_by, a manager id, at the write
// boundary. A tapping client cannot produce a manual predecessor for itself.
//
// The control is the same row on the nfc channel: identical clocks, opposite
// outcome. Without it, "the manual row was ignored" would also be true of a
// server leg that had simply stopped working.
func TestQRDB_AManualPredecessorDoesNotDebounceARealTap(t *testing.T) {
	for _, tc := range []struct {
		name        string
		channel     string
		wantIgnored bool
	}{
		{"manual: a manager's keystroke is not this person's tap", "manual", false},
		{"nfc: the same split clocks on a real tap DO debounce", "nfc", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newTapHarness(t)
			emp := h.newEmployee(t, "active")
			// Declared yesterday, written one second ago — the shape the exemption
			// is about.
			h.seedRecordWithSplitClocks(t, emp, 24*time.Hour, time.Second, tc.channel, "in")
			cookie := h.cookieForEmployee(t, emp)
			signed := h.signedContextFrom(t, qrURLFor(h.tagUID), cookie)

			if w := h.postSigned(t, signed, cookie, onSiteAddr, nil); w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			rec := h.lastRecord(t, emp)
			gotIgnored := rec.Verdict == "ignored"
			if gotIgnored != tc.wantIgnored {
				t.Fatalf("ignored = %v, want %v (verdict %q via %q)",
					gotIgnored, tc.wantIgnored, rec.Verdict, deref(rec.MatchedSid))
			}
		})
	}
}

// --- layer 4: concurrency ----------------------------------------------------

// TestQRDB_SimultaneousTapsByOnePersonAreSerialised is ADR 0006's fourth layer,
// and it is the one the first three fixes did not touch.
//
// 🔴 THE DEBOUNCE IS A READ-THEN-DECIDE, and it lost the way CLAUDE.md §4.4 says
// a counter check loses. `gather` and `write` used to be SEPARATE transactions,
// so N simultaneous requests all read BEFORE any of them had written: every one
// saw the same old predecessor, and not one of them was a duplicate. Measured
// before the fix, -race, one employee with ordinary history:
//
//	50 simultaneous POSTs -> 51 counted rows, ZERO ignored, 0.41 s
//	 2 simultaneous POSTs ->  2 counted     (no exotic concurrency required)
//
// The fix is a per-person advisory lock taken as the first statement of the one
// transaction that now gathers, decides and writes. §4.6 is unchanged and is
// asserted here first: every POST still produces a row — the extras are
// `ignored`, not refused.
func TestQRDB_SimultaneousTapsByOnePersonAreSerialised(t *testing.T) {
	for _, n := range []int{2, 20} {
		t.Run(fmt.Sprintf("%d at once", n), func(t *testing.T) {
			h := newTapHarness(t)
			emp := h.newEmployee(t, "active")
			// Ordinary history, so this is not the "first tap ever" special case.
			h.seedTapAgedBy(t, emp, 30*time.Minute, "in", false)
			cookie := h.cookieForEmployee(t, emp)
			signed := h.signedContextFrom(t, qrURLFor(h.tagUID), cookie)
			before := h.countFor(t, emp)

			var wg sync.WaitGroup
			start := make(chan struct{})
			wg.Add(n)
			for i := 0; i < n; i++ {
				go func() {
					defer wg.Done()
					<-start
					if w := h.postSigned(t, signed, cookie, onSiteAddr, nil); w.Code != http.StatusOK {
						t.Errorf("status = %d, want 200", w.Code)
					}
				}()
			}
			close(start)
			wg.Wait()

			// §4.6 FIRST: nothing is refused under contention.
			if got := h.countFor(t, emp) - before; got != n {
				t.Fatalf("%d rows written for %d simultaneous POSTs, want %d: a tap was LOST", got, n, n)
			}
			// The seeded row is counted too, so exactly one NEW row may count.
			v := h.verdictBreakdown(t, emp)
			if v["__directional"] != 2 {
				t.Fatalf("%d counted rows (seed + new), want 2 — %d simultaneous taps produced more than "+
					"one attendance record for one person (ADR 0006, layer 4); breakdown %v",
					v["__directional"], n, v)
			}
			if v["ignored"] != n-1 {
				t.Fatalf("ignored = %d, want %d; breakdown %v", v["ignored"], n-1, v)
			}
		})
	}

	// N3 FROM THE AUDIT: the same race also multiplied the PRACTICE run. An
	// employee with no tap at all had every simultaneous row marked practice=true
	// (measured: 20 of 20), because each request read "no prior tap". Harmless for
	// hours — practice never counts — but the same read-then-decide shape, and the
	// lock closes it too.
	t.Run("the practice run is spent exactly once", func(t *testing.T) {
		const n = 20
		h := newTapHarness(t)
		emp := h.newEmployee(t, "active") // activated_at set, no taps yet
		cookie := h.cookieForEmployee(t, emp)
		signed := h.signedContextFrom(t, qrURLFor(h.tagUID), cookie)

		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(n)
		for i := 0; i < n; i++ {
			go func() { defer wg.Done(); <-start; h.postSigned(t, signed, cookie, onSiteAddr, nil) }()
		}
		close(start)
		wg.Wait()

		v := h.verdictBreakdown(t, emp)
		if v["__practice"] != 1 {
			t.Fatalf("%d of %d rows were marked practice, want exactly 1: the practice run is a "+
				"read-then-decide too; breakdown %v", v["__practice"], n, v)
		}
	})

	// THE COST, MEASURED RATHER THAN ASSUMED: different people do NOT wait on each
	// other, and the pool survives. Without this, "we serialised the debounce"
	// could mean "we serialised the product".
	t.Run("different people never wait on each other", func(t *testing.T) {
		const n = 30
		h := newTapHarness(t)
		type person struct {
			emp    uuid.UUID
			cookie *http.Cookie
			signed string
		}
		people := make([]person, n)
		for i := range people {
			e := h.newEmployee(t, "active")
			c := h.cookieForEmployee(t, e)
			people[i] = person{e, c, h.signedContextFrom(t, qrURLFor(h.tagUID), c)}
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Add(n)
		for i := range people {
			go func(i int) {
				defer wg.Done()
				<-start
				if w := h.postSigned(t, people[i].signed, people[i].cookie, onSiteAddr, nil); w.Code != http.StatusOK {
					t.Errorf("person %d status = %d, want 200: one person's lock refused another's tap", i, w.Code)
				}
			}(i)
		}
		close(start)
		wg.Wait()
		for i, p := range people {
			if got := h.verdictBreakdown(t, p.emp)["__directional"]; got != 1 {
				t.Fatalf("person %d has %d counted rows, want 1: distinct keys must not serialise", i, got)
			}
		}
	})
}
