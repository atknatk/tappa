package handler

// applive_db_test.go -- M16-22: the native app reads a venue screen's live QR code (ADR 0045 §2.5,
// Q36 (c): the system camera reads it, the Universal Link / App Link opens the app, and the app
// hands POST /app/v1/tap/start the URL's query as `liveQR`). The same cases as the browser's
// liveqrtap_db_test.go, against real Postgres, through the real router, with a device-signed
// request: tap/start opens the token with GET /q's reader and mints the version-2 context bound to
// the device, the press is the ordinary POST /app/v1/tap, and the REAL checkin service decides and
// writes. Every assertion reads the rows the database holds (owner connection: it reads across a
// holding's two businesses, so these tests skip without DATABASE_MIGRATE_URL).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/appdevice"
	"github.com/atknatk/tappa/internal/liveqr"
	"github.com/atknatk/tappa/internal/sun"
)

// appLiveHarness is the app harness with the live QR read turned on exactly as cmd/tappa's
// openLiveQR does (App.ServeLiveQR beside Tap.ServeLiveQR, one set of keys), and the browser's live
// harness helpers (screens, tokens, the owner's reads) over the same database.
type appLiveHarness struct {
	*appHarness
	live *liveHarness
}

func newAppLiveHarness(t *testing.T) *appLiveHarness {
	t.Helper()
	h := newAppHarness(t)
	owner := holdingOwner(t)
	secret := sha256.Sum256([]byte("m16-22 app live tap test secret, not a key"))
	keys, err := liveqr.NewKeys(secret[:])
	if err != nil {
		t.Fatal(err)
	}
	screens, err := liveqr.NewScreens(keys, h.data, h.trail)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.ServeLiveQR(keys, h.data); err != nil {
		t.Fatal(err)
	}
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	return &appLiveHarness{appHarness: h, live: &liveHarness{tapHarness: h.tapHarness, owner: owner, keys: keys, screens: screens, kek: kek}}
}

// liveQuery is the query of the URL the screen shows: what the camera's link hands the app.
func liveQuery(tok string) string { return liveqr.TokenParam + "=" + url.QueryEscape(tok) }

// readFrom is a signed read from remoteAddr (the app's own network address).
func (h *appLiveHarness) readFrom(t *testing.T, p *appPhone, path, body, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	text, err := appdevice.CanonicalReadText(http.MethodPost, path, []byte(body), ts, p.id.String())
	if err != nil {
		t.Fatalf("canonical read: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set(appdevice.HeaderDeviceID, p.id.String())
	req.Header.Set(appdevice.HeaderTimestamp, ts)
	req.Header.Set(appdevice.HeaderSignature, p.sign(t, text))
	req.RemoteAddr = remoteAddr
	return h.serve(req)
}

// liveStart is tap/start for a live QR query, from remoteAddr.
func (h *appLiveHarness) liveStart(t *testing.T, p *appPhone, query, remoteAddr string) (appTapStartResponse, *httptest.ResponseRecorder) {
	t.Helper()
	w := h.readFrom(t, p, AppTapStartPath, jsonBody(t, appTapStartRequest{LiveQR: query}), remoteAddr)
	var out appTapStartResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return out, w
}

// press is the nonce-signed POST /app/v1/tap from remoteAddr.
func (h *appLiveHarness) press(t *testing.T, p *appPhone, signedCtx string, extra map[string]any, remoteAddr string) (appTapResponse, *httptest.ResponseRecorder) {
	t.Helper()
	in := map[string]any{"context": signedCtx}
	for k, v := range extra {
		in[k] = v
	}
	req := h.signedRequest(t, p, p.id.String(), h.nonce(t, p), AppTapPath, jsonBody(t, in))
	req.RemoteAddr = remoteAddr
	w := h.serve(req)
	var out appTapResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return out, w
}

// scan is the camera's link and the button: tap/start with the token's query, then the press, both
// from remoteAddr. tap/start must answer 200 (the screen renders for every token, as GET /q does).
func (h *appLiveHarness) scan(t *testing.T, p *appPhone, tok string, extra map[string]any, remoteAddr string) (appTapStartResponse, appTapResponse) {
	t.Helper()
	start, w := h.liveStart(t, p, liveQuery(tok), remoteAddr)
	if w.Code != http.StatusOK || start.Context == "" {
		t.Fatalf("tap/start with a live QR code = %d %s", w.Code, w.Body.String())
	}
	res, w := h.press(t, p, start.Context, extra, remoteAddr)
	if w.Code != http.StatusOK {
		t.Fatalf("the press = %d %s", w.Code, w.Body.String())
	}
	return start, res
}

// phoneFor registers a device for a fresh clone of the harness's employee (same venue, same
// business-day cutoff), so cases do not share one person's debounce.
func (h *appLiveHarness) phoneFor(t *testing.T) (uuid.UUID, *appPhone) {
	t.Helper()
	id, _ := h.live.clone(t)
	p := newAppPhone(t)
	h.register(t, p, id)
	return id, p
}

var appVenueFix = map[string]any{"lat": 35.918, "lng": 14.489}

// ---------------------------------------------------------------------------------------------

// TestAppLiveQRDB_AScanOnTheVenuesNetworkIsOk is the card's first case on the app path: a live QR
// at the person's own business, from the venue's network -> tap/start draws the browser's tap
// screen (the name, the SCREEN's venue, a predicted direction), writes nothing; the press records
// ok with channel qr_live and client app, at the screen's venue, no plaque, sun_valid false, and the
// feed gets the event. MUTANT (measured): tapStartLive mints a context without TokenVerified -- the
// row is a reject via sys:live-qr-unverified.
func TestAppLiveQRDB_AScanOnTheVenuesNetworkIsOk(t *testing.T) {
	h := newAppLiveHarness(t)
	sc := h.live.screenAt(t, h.tenantID, h.locationID)
	id, p := h.phoneFor(t)
	before := h.countFor(t, id)

	start, w := h.liveStart(t, p, liveQuery(h.live.token(t, sc)), onSiteAddr)
	if w.Code != http.StatusOK {
		t.Fatalf("tap/start = %d %s", w.Code, w.Body.String())
	}
	if start.EmployeeName != "Maria Borg" || start.LocationName != "St Julians" || start.Direction != "in" || start.QR || start.Context == "" {
		t.Fatalf("tap/start = %+v, want the browser's tap screen: Maria Borg at St Julians, Clock in, qr false", start)
	}
	if h.countFor(t, id) != before {
		t.Fatal("tap/start wrote a record")
	}
	res, w := h.press(t, p, start.Context, map[string]any{"want": "in"}, onSiteAddr)
	if w.Code != http.StatusOK || res.Verdict != "ok" || res.Outcome != "recorded" || res.Venue != "St Julians" {
		t.Fatalf("the press = %d %+v, want a recorded ok at St Julians", w.Code, res)
	}
	r := h.live.lastRow(t, id)
	for col, want := range map[string]string{
		"verdict": "ok", "channel": "qr_live", "client": "app", "location_id": h.locationID.String(),
		"tag_uid": "<nil>", "ctr": "<nil>", "site_tenant_id": "<nil>", "sun_valid": "false",
		"ip_match": "true", "trust": "70", "matched_sid": "base:ip-or-gps-ok", "type": "in",
	} {
		if got := str(r[col]); got != want {
			t.Errorf("%s = %s, want %s", col, got, want)
		}
	}
	if ev := h.live.row(t, "punch_events", "punch_id = $1", r["id"]); ev == nil || str(ev["channel"]) != "qr_live" {
		t.Errorf("the feed event of an ok app live QR tap: %v", ev)
	}
}

// TestAppLiveQRDB_GPSAloneIsFlagged: off the venue's network, inside the GPS radius -> a RECORDED
// flag via base:live-qr-requires-ip (the user's decision of 2026-11-19), exactly as in the browser.
func TestAppLiveQRDB_GPSAloneIsFlagged(t *testing.T) {
	h := newAppLiveHarness(t)
	sc := h.live.screenAt(t, h.tenantID, h.locationID)
	id, p := h.phoneFor(t)
	_, res := h.scan(t, p, h.live.token(t, sc), appVenueFix, offSiteAddr)
	r := h.live.lastRow(t, id)
	if res.Verdict != "flag" || str(r["verdict"]) != "flag" || str(r["matched_sid"]) != "base:live-qr-requires-ip" ||
		str(r["gps_match"]) != "true" || str(r["ip_match"]) != "false" || str(r["client"]) != "app" {
		t.Fatalf("GPS alone: %s via %s (gps %s, ip %s, client %s), want a flag via base:live-qr-requires-ip",
			str(r["verdict"]), str(r["matched_sid"]), str(r["gps_match"]), str(r["ip_match"]), str(r["client"]))
	}
}

// TestAppLiveQRDB_AForgedCodeIsARecordedRejectAndNothingLeaks is Q39 (a) and the log test on the
// app path: a value that is not a token, a genuine token with its tag changed, one signed by
// another key, and a query that does not parse each get the tap screen (no venue, no direction),
// and the press RECORDS a reject via sys:live-qr-unverified at the person's own business with no
// venue. No app log line, no response body and no audit_log detail carries a token, a fragment of
// one, or Open's error sentence; the log names the two kinds (CONTROL).
func TestAppLiveQRDB_AForgedCodeIsARecordedRejectAndNothingLeaks(t *testing.T) {
	h := newAppLiveHarness(t)
	sc := h.live.screenAt(t, h.tenantID, h.locationID)
	genuine := h.live.token(t, sc)
	flipped := []byte(genuine)
	if flipped[len(flipped)-1] == 'A' {
		flipped[len(flipped)-1] = 'B'
	} else {
		flipped[len(flipped)-1] = 'A'
	}
	otherSecret := sha256.Sum256([]byte("another deployment's live QR key, not a key"))
	otherKeys, err := liveqr.NewKeys(otherSecret[:])
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := otherKeys.Mint(sc.ID, sc.LocationID, time.Now(), liveqr.TokenLifetime)
	if err != nil {
		t.Fatal(err)
	}
	var bodies bytes.Buffer
	for name, query := range map[string]string{
		"not a token":      liveQuery("not-a-token"),
		"a changed tag":    liveQuery(string(flipped)),
		"another key":      liveQuery(foreign),
		"an unparseable q": "t=%zz" + genuine[:20],
	} {
		id, p := h.phoneFor(t)
		start, w := h.liveStart(t, p, query, onSiteAddr)
		bodies.WriteString(w.Body.String())
		if w.Code != http.StatusOK || start.Context == "" || start.LocationName != "" || start.Direction != "" {
			t.Fatalf("%s: tap/start = %d %+v, want the tap screen with no venue and no direction", name, w.Code, start)
		}
		res, w := h.press(t, p, start.Context, appVenueFix, onSiteAddr)
		bodies.WriteString(w.Body.String())
		r := h.live.lastRow(t, id)
		if w.Code != http.StatusOK || res.Verdict != "reject" || str(r["verdict"]) != "reject" ||
			str(r["matched_sid"]) != "sys:live-qr-unverified" || r["location_id"] != nil || str(r["channel"]) != "qr_live" || str(r["client"]) != "app" {
			t.Fatalf("%s: %d %s via %s at %s, want a recorded reject via sys:live-qr-unverified with no venue",
				name, w.Code, str(r["verdict"]), str(r["matched_sid"]), str(r["location_id"]))
		}
	}
	var trail bytes.Buffer
	rows, err := h.live.owner.Query(context.Background(), `SELECT coalesce(detail::text, '') FROM audit_log WHERE tenant_id = $1`, h.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			t.Fatal(err)
		}
		trail.WriteString(d)
	}
	rows.Close()
	logs := h.logs.String()
	for where, text := range map[string]string{"the app's log": logs, "a response body": bodies.String(), "audit_log": trail.String()} {
		for _, secret := range []string{genuine, string(flipped), foreign, genuine[10:40], foreign[20:50]} {
			if strings.Contains(text, secret) {
				t.Errorf("%s carries a token or a fragment of one", where)
			}
		}
		for _, sentence := range []string{liveqr.ErrSignature.Error(), liveqr.ErrMalformed.Error(), "signature does not verify"} {
			if strings.Contains(text, sentence) {
				t.Errorf("%s carries the verification error's text %q", where, sentence)
			}
		}
	}
	if !strings.Contains(logs, `"kind":"signature"`) || !strings.Contains(logs, `"kind":"malformed"`) {
		t.Errorf("CONTROL: the app's log does not name the two kinds -- is it capturing tap/start at all?\n%s", logs)
	}
}

// TestAppLiveQRDB_AnExpiredCodeIsAFlagWithItsNote is Q39 (b) on the app path: a genuine token 20 s
// past its expiry (beyond the 10 s grace) when tap/start opens it is a RECORDED flag via
// sys:live-qr-expired with its note, even on the venue's network; one inside the grace is ok. The
// context carries the token's expiry (checkin's validate refuses a verified code without it, and
// so does the context's own parse). MUTANT (measured): tapStartLive drops TokenExpiresAt -- the
// press answers 400 stale_context (parseLive refuses a verified payload with no expiry), nothing is
// written.
func TestAppLiveQRDB_AnExpiredCodeIsAFlagWithItsNote(t *testing.T) {
	h := newAppLiveHarness(t)
	sc := h.live.screenAt(t, h.tenantID, h.locationID)
	id, p := h.phoneFor(t)
	h.scan(t, p, h.live.tokenIssuedAgo(t, sc, 40*time.Second), nil, onSiteAddr)
	r := h.live.lastRow(t, id)
	if str(r["verdict"]) != "flag" || str(r["matched_sid"]) != "sys:live-qr-expired" || !strings.Contains(str(r["note"]), "run out") || str(r["client"]) != "app" {
		t.Fatalf("expired: %s via %s note %q, want a flag via sys:live-qr-expired with its note", str(r["verdict"]), str(r["matched_sid"]), str(r["note"]))
	}
	other, op := h.phoneFor(t)
	h.scan(t, op, h.live.tokenIssuedAgo(t, sc, 25*time.Second), nil, onSiteAddr)
	if r := h.live.lastRow(t, other); str(r["verdict"]) != "ok" {
		t.Fatalf("inside the grace: %s via %s, want ok", str(r["verdict"]), str(r["matched_sid"]))
	}
}

// TestAppLiveQRDB_ACodeNamingAnotherVenueIsARecordedReject: a GENUINE token (this deployment's key)
// naming a screen at one venue and claiming another -- the screen row's venue is not the token's --
// is unverified: the press records a reject via sys:live-qr-unverified with no venue. The CONTROL
// is the same screen's true token, which records ok. MUTANT (measured): liveQRTap.open without the
// venue comparison -- the forged venue's tap is recorded ok at that venue.
func TestAppLiveQRDB_ACodeNamingAnotherVenueIsARecordedReject(t *testing.T) {
	h := newAppLiveHarness(t)
	sc := h.live.screenAt(t, h.tenantID, h.locationID)
	elsewhere := uuid.New()
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name, static_ips) VALUES ($1, $2, 'Gzira', '{203.0.113.0/24}')`, elsewhere, h.tenantID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	lying, err := h.live.keys.Mint(sc.ID, elsewhere, time.Now(), liveqr.TokenLifetime)
	if err != nil {
		t.Fatal(err)
	}
	id, p := h.phoneFor(t)
	start, _ := h.scan(t, p, lying, nil, onSiteAddr)
	r := h.live.lastRow(t, id)
	if start.LocationName != "" || str(r["verdict"]) != "reject" || str(r["matched_sid"]) != "sys:live-qr-unverified" || r["location_id"] != nil {
		t.Fatalf("another venue: screen %q, %s via %s at %s, want a recorded reject via sys:live-qr-unverified with no venue",
			start.LocationName, str(r["verdict"]), str(r["matched_sid"]), str(r["location_id"]))
	}
	other, op := h.phoneFor(t)
	h.scan(t, op, h.live.token(t, sc), nil, onSiteAddr)
	if r := h.live.lastRow(t, other); str(r["verdict"]) != "ok" || str(r["location_id"]) != h.locationID.String() {
		t.Fatalf("CONTROL: the screen's true code: %s at %s, want ok at its venue", str(r["verdict"]), str(r["location_id"]))
	}
}

// TestAppLiveQRDB_ARemovedScreenIsARecordedReject is §5 row 1's twin on the app path: the screen is
// removed between tap/start and the press (the token still inside its window) -> the press records
// a reject via sys:screen-not-active (the row is re-read at the press).
func TestAppLiveQRDB_ARemovedScreenIsARecordedReject(t *testing.T) {
	h := newAppLiveHarness(t)
	sc := h.live.screenAt(t, h.tenantID, h.locationID)
	id, p := h.phoneFor(t)
	start, w := h.liveStart(t, p, liveQuery(h.live.token(t, sc)), onSiteAddr)
	if w.Code != http.StatusOK {
		t.Fatalf("tap/start = %d", w.Code)
	}
	if _, err := h.live.screens.Revoke(context.Background(), liveqr.RevokeCommand{TenantID: h.tenantID, ActorID: uuid.New(), DisplayID: sc.ID}); err != nil {
		t.Fatal(err)
	}
	h.press(t, p, start.Context, nil, onSiteAddr)
	if r := h.live.lastRow(t, id); str(r["verdict"]) != "reject" || str(r["matched_sid"]) != "sys:screen-not-active" {
		t.Fatalf("removed: %s via %s, want a recorded reject via sys:screen-not-active", str(r["verdict"]), str(r["matched_sid"]))
	}
}

// TestAppLiveQRDB_AHoldingScanIsRefusedAndRecorded is Q43 (b) on the app path: KM's employee (the
// harness's business) at a screen of KF, a peer -- refused AND recorded at the employer via
// sys:cross-tenant-live-qr, the site columns the screen's, no plaque, no venue; KF sees one
// `refused` site row. tap/start shows no venue and no brand of the other business. Outside a
// holding the same screen's code is foreign_tenant at the press and nothing is written (CONTROL).
func TestAppLiveQRDB_AHoldingScanIsRefusedAndRecorded(t *testing.T) {
	h := newAppLiveHarness(t)
	kf, kfVenue := uuid.New(), uuid.New()
	h.newTag(t, h.live.kek, kf, kfVenue, 100)
	sc := h.live.screenAt(t, kf, kfVenue)

	id, p := h.phoneFor(t)
	before := h.countFor(t, id)
	start, w := h.liveStart(t, p, liveQuery(h.live.token(t, sc)), onSiteAddr)
	if w.Code != http.StatusOK || start.LocationName != "" || start.Brand != (appBrand{}) {
		t.Fatalf("outside a holding: tap/start = %d %+v, want the tap screen with no venue and no brand", w.Code, start)
	}
	if _, w := h.press(t, p, start.Context, appVenueFix, onSiteAddr); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), string(appErrForeignTenant)) {
		t.Fatalf("outside a holding: the press = %d %s, want 403 %s", w.Code, w.Body.String(), appErrForeignTenant)
	}
	if n := h.countFor(t, id); n != before {
		t.Fatalf("outside a holding: rows %d -> %d, want none written", before, n)
	}

	holdingOf(t, h.live.owner, h.tenantID, kf)
	other, op := h.phoneFor(t)
	_, res := h.scan(t, op, h.live.token(t, sc), appVenueFix, onSiteAddr)
	r := h.live.lastRow(t, other)
	for col, want := range map[string]string{
		"verdict": "reject", "channel": "qr_live", "client": "app", "matched_sid": "sys:cross-tenant-live-qr",
		"site_tenant_id": kf.String(), "site_location_id": kfVenue.String(), "site_tag_uid": "<nil>",
		"location_id": "<nil>", "tag_uid": "<nil>", "sun_valid": "false",
	} {
		if got := str(r[col]); got != want {
			t.Errorf("%s = %s, want %s", col, got, want)
		}
	}
	if res.Verdict != "reject" {
		t.Errorf("the press answered %+v, want a reject", res)
	}
	if sp := h.live.row(t, "site_punches", "transaction_id = $1", r["id"]); sp == nil || str(sp["outcome"]) != "refused" || str(sp["channel"]) != "qr_live" {
		t.Errorf("KF's site row for the refusal: %v", sp)
	}
}

// TestAppLiveQRDB_RefusedBeforeAnything: tap/start answers bad_request and writes nothing for a body
// naming a plaque AND a live QR code, and for a live QR code on a server whose live QR is off
// (TAPPA_LIVE_QR_KEY unset: GET /q is not mounted there either). Neither answer carries the token.
func TestAppLiveQRDB_RefusedBeforeAnything(t *testing.T) {
	h := newAppLiveHarness(t)
	sc := h.live.screenAt(t, h.tenantID, h.locationID)
	tok := h.live.token(t, sc)
	id, p := h.phoneFor(t)
	before, ctr := h.countFor(t, id), h.lastCtr(t, h.tagUID)
	both := h.readFrom(t, p, AppTapStartPath, jsonBody(t, appTapStartRequest{SUN: h.nextSUN(t), LiveQR: liveQuery(tok)}), onSiteAddr)
	if both.Code != http.StatusBadRequest || !strings.Contains(both.Body.String(), string(appErrBadRequest)) {
		t.Fatalf("a plaque and a live QR code = %d %s, want 400 %s", both.Code, both.Body.String(), appErrBadRequest)
	}

	off := newAppHarness(t) // the same database, live QR never turned on (a business of its own)
	q := newAppPhone(t)
	off.register(t, q, off.employeeID)
	offBefore := off.countFor(t, off.employeeID)
	w := off.read(t, q, http.MethodPost, AppTapStartPath, jsonBody(t, appTapStartRequest{LiveQR: liveQuery(tok)}))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), string(appErrBadRequest)) {
		t.Fatalf("live QR off: tap/start = %d %s, want 400 %s", w.Code, w.Body.String(), appErrBadRequest)
	}
	for _, body := range []string{both.Body.String(), w.Body.String()} {
		if strings.Contains(body, tok[5:40]) {
			t.Fatal("an error body carries the token")
		}
	}
	if h.countFor(t, id) != before || h.lastCtr(t, h.tagUID) != ctr || off.countFor(t, off.employeeID) != offBefore {
		t.Fatal("a refused tap/start wrote a record or moved a counter")
	}
}

// TestAppDB_AStaticQRReadIsTheBrowsersQR measures the card's STATIC half on the app path: the
// plaque's printed QR is sun.PlaqueQRURL's "<base>/t?tag=<UID>" -- the plaque route itself, path
// /t, no ctr and no cmac -- so the app's existing /t link (Android's VIEW filter, iOS's AASA) opens
// it and hands tap/start the query as `sun`, as for an NFC read. tap/start says qr and moves no
// counter; the press records channel qr, client app: on the venue's network ok, off it with GPS a
// flag via base:qr-requires-ip (CLAUDE.md §5's QR rule) -- the browser's static QR results.
func TestAppDB_AStaticQRReadIsTheBrowsersQR(t *testing.T) {
	h := newAppLiveHarness(t)
	uid, err := hex.DecodeString(h.tagUID)
	if err != nil {
		t.Fatal(err)
	}
	printed, err := sun.PlaqueQRURL("https://taptime.mt"+TapPath, uid)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(printed)
	if err != nil {
		t.Fatal(err)
	}
	if u.Path != TapPath || u.Query().Get("ctr") != "" || u.Query().Get("cmac") != "" || u.Query().Get("tag") != h.tagUID {
		t.Fatalf("the printed QR is %s: want <base>%s?tag=<UID> and nothing else", printed, TapPath)
	}
	for name, tc := range map[string]struct {
		addr     string
		fix      map[string]any
		verdict  string
		ruleName string
	}{
		"on the venue's network": {onSiteAddr, nil, "ok", "base:ip-or-gps-ok"},
		"GPS alone":              {offSiteAddr, appVenueFix, "flag", "base:qr-requires-ip"},
	} {
		id, p := h.phoneFor(t)
		ctr := h.lastCtr(t, h.tagUID)
		start, w := func() (appTapStartResponse, *httptest.ResponseRecorder) {
			w := h.readFrom(t, p, AppTapStartPath, jsonBody(t, appTapStartRequest{SUN: u.RawQuery}), tc.addr)
			var out appTapStartResponse
			if w.Code == http.StatusOK {
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
			}
			return out, w
		}()
		if w.Code != http.StatusOK || !start.QR || start.LocationName != "St Julians" {
			t.Fatalf("%s: tap/start = %d %+v, want the QR tap screen at St Julians", name, w.Code, start)
		}
		res, w := h.press(t, p, start.Context, tc.fix, tc.addr)
		r := h.live.lastRow(t, id)
		if w.Code != http.StatusOK || res.Verdict != tc.verdict || str(r["verdict"]) != tc.verdict || str(r["matched_sid"]) != tc.ruleName ||
			str(r["channel"]) != "qr" || str(r["client"]) != "app" || h.lastCtr(t, h.tagUID) != ctr {
			t.Fatalf("%s: %d %s via %s (channel %s, client %s), want %s via %s, channel qr, client app, the counter unmoved",
				name, w.Code, str(r["verdict"]), str(r["matched_sid"]), str(r["channel"]), str(r["client"]), tc.verdict, tc.ruleName)
		}
	}
}
