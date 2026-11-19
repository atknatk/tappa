package handler

// liveqrtap_db_test.go -- M16-21 (ADR 0045 §2.5; Q38 (a), Q39 (a)+(b), Q43 (b); the user's
// decision of 2026-11-19) end to end against real Postgres: a phone's camera opens GET /q?t=<a
// venue screen's token>, the page mints the signed context, the button POSTs /api/checkin, and
// the REAL checkin service decides and writes. Every assertion reads the rows the database holds.
//
// The screens are created and paired through internal/liveqr's own service (the panel's and the
// screen page's code), and their tokens are minted by it; an expired token is minted with the same
// keys at a past instant (the server's own stamp, as Screens.Token would have put on it then).
//
// Reads go through the OWNER connection (DATABASE_MIGRATE_URL, a superuser: it reads punch_events
// and site_punches across both businesses of a holding), so these tests skip without it.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/review"
	"github.com/atknatk/tappa/internal/liveqr"
	"github.com/atknatk/tappa/test/fixtures"
)

type liveHarness struct {
	*tapHarness
	owner   *pgx.Conn
	keys    *liveqr.Keys
	screens *liveqr.Screens
	kek     []byte
}

// newLiveHarness is the tap harness with GET /q turned on exactly as cmd/tappa's openLiveQR does
// (Tap.ServeLiveQR, then Mount), on a router of its own.
func newLiveHarness(t *testing.T) *liveHarness {
	t.Helper()
	h := newTapHarness(t)
	owner := holdingOwner(t)
	secret := sha256.Sum256([]byte("m16-21 live tap test secret, not a key"))
	keys, err := liveqr.NewKeys(secret[:])
	if err != nil {
		t.Fatal(err)
	}
	screens, err := liveqr.NewScreens(keys, h.data, h.trail)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.tap.ServeLiveQR(keys, h.data); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	h.tap.Mount(r)
	h.activation.Mount(r)
	h.router = r
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatal(err)
	}
	return &liveHarness{tapHarness: h, owner: owner, keys: keys, screens: screens, kek: kek}
}

// screenAt creates and pairs a live QR screen at venue, as the panel and the screen page do.
func (h *liveHarness) screenAt(t *testing.T, tenantID, venue uuid.UUID) liveqr.Screen {
	t.Helper()
	ctx := context.Background()
	created, err := h.screens.Create(ctx, liveqr.CreateCommand{TenantID: tenantID, ActorID: uuid.New(), LocationID: venue})
	if err != nil {
		t.Fatalf("create a screen: %v", err)
	}
	sc, _, err := h.screens.Pair(ctx, created.Code)
	if err != nil {
		t.Fatalf("pair the screen: %v", err)
	}
	return sc
}

// token is what the screen shows right now: Screens.Token's own mint -- this screen, its venue,
// the server's clock, TokenLifetime (Pair's answer does not carry the venue's state Token reads, so
// the mint is called the way Token calls it).
func (h *liveHarness) token(t *testing.T, sc liveqr.Screen) string {
	t.Helper()
	return h.tokenIssuedAgo(t, sc, 0)
}

// tokenIssuedAgo is the token the screen showed `ago` ago: the same keys, a past server stamp.
func (h *liveHarness) tokenIssuedAgo(t *testing.T, sc liveqr.Screen, ago time.Duration) string {
	t.Helper()
	tok, err := h.keys.Mint(sc.ID, sc.LocationID, time.Now().Add(-ago), liveqr.TokenLifetime)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func liveURL(tok string) string {
	return liveqr.LiveQRTapPath + "?" + liveqr.TokenParam + "=" + url.QueryEscape(tok)
}

// scan is the camera's GET /q and the button's POST, for cookie, from remoteAddr, with extra form
// fields (a fix, a declared time). It returns the POST's recorder.
func (h *liveHarness) scan(t *testing.T, tok string, cookie *http.Cookie, remoteAddr string, extra url.Values) *httptest.ResponseRecorder {
	t.Helper()
	return h.postSigned(t, h.signedContextFrom(t, liveURL(tok), cookie), cookie, remoteAddr, extra)
}

// clone adds an employee identical to the harness's own -- same venue, same business-day cutoff
// -- so two channels can be compared on two people who differ in nothing but their id (the card's
// "Yalıtım": one person tapping twice would debounce or flip the direction).
func (h *liveHarness) clone(t *testing.T) (uuid.UUID, *http.Cookie) {
	t.Helper()
	id := h.newEmployee(t, "active")
	if _, err := h.owner.Exec(context.Background(),
		`UPDATE employees SET day_cutoff = (SELECT day_cutoff FROM employees WHERE id = $2), full_name = 'Maria Borg'
		  WHERE id = $1`, id, h.employeeID); err != nil {
		t.Fatalf("clone the cutoff: %v", err)
	}
	return id, h.cookieForEmployee(t, id)
}

// row is one row of table as jsonb, read by the owner.
func (h *liveHarness) row(t *testing.T, table, where string, arg any) map[string]any {
	t.Helper()
	var raw []byte
	err := h.owner.QueryRow(context.Background(), `SELECT to_jsonb(x) FROM `+table+` x WHERE `+where+` LIMIT 1`, arg).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		t.Fatalf("read %s: %v", table, err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func (h *liveHarness) count(t *testing.T, q string, arg any) int {
	t.Helper()
	var n int
	if err := h.owner.QueryRow(context.Background(), q, arg).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// lastRow is the employee's newest transactions row, as jsonb.
func (h *liveHarness) lastRow(t *testing.T, employeeID uuid.UUID) map[string]any {
	t.Helper()
	var raw []byte
	if err := h.owner.QueryRow(context.Background(),
		`SELECT to_jsonb(x) FROM transactions x WHERE employee_id = $1 ORDER BY created_at DESC LIMIT 1`, employeeID).Scan(&raw); err != nil {
		t.Fatalf("the employee's last row: %v", err)
	}
	m := map[string]any{}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// approve is a manager's approval, through the review service the panel calls.
func (h *liveHarness) approve(t *testing.T, tenantID uuid.UUID, txn any) {
	t.Helper()
	id, err := uuid.Parse(txn.(string))
	if err != nil {
		t.Fatal(err)
	}
	admin := uuid.New()
	// The manager is COMMITTED: written under the operator-tables lock, held shared around the
	// write (holdPanelTablesLock; cmd/opadmin's TestTablesLock_AdminRowsAreWrittenUnderTheHolder).
	release := holdPanelTablesLock(t)
	defer release()
	if _, err := h.owner.Exec(context.Background(),
		`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		 VALUES ($1, $2, 'm16-21 manager', $3, $4, 'manager', 'active')`,
		admin, tenantID, "m1621-"+uuid.NewString()+"@iso.example", fixtures.UnusablePasswordHash); err != nil {
		t.Fatalf("a manager: %v", err)
	}
	rv, err := review.NewReviewer(h.data, h.trail, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := rv.Record(context.Background(), review.Decision{TenantID: tenantID, TransactionID: id, ReviewerID: admin, Outcome: review.Approved}); err != nil {
		t.Fatalf("approve: %v", err)
	}
}

func str(v any) string {
	if v == nil {
		return "<nil>"
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// ---------------------------------------------------------------------------------------------

// TestLiveQRDB_ALocalScanIsRecordedAtTheScreensVenue is the card's first DB test (yerel): a live QR
// scan at the person's own business's screen, on the venue's network -> ok, location_id the
// screen's venue, no plaque (tag_uid, ctr NULL), no site columns, sun_valid false (M16-19's
// hand-off), and every column a tap channel carries -- the business day, the direction's source,
// the shift snapshot, the client. The feed gets the event with the REAL channel (Q38 (a): the
// wire maps it). MUTANT (measured): checkin.validate's old "a tap needs a tag" for every non-manual
// channel -- the POST answers 500 and no row is written.
func TestLiveQRDB_ALocalScanIsRecordedAtTheScreensVenue(t *testing.T) {
	h := newLiveHarness(t)
	sc := h.screenAt(t, h.tenantID, h.locationID)
	cookie := h.sessionCookieFor(t)
	w := h.scan(t, h.token(t, sc), cookie, onSiteAddr, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("POST = %d:\n%s", w.Code, w.Body.String())
	}
	r := h.lastRow(t, h.employeeID)
	for col, want := range map[string]string{
		"verdict": "ok", "channel": "qr_live", "location_id": h.locationID.String(), "tag_uid": "<nil>", "ctr": "<nil>",
		"site_tenant_id": "<nil>", "site_location_id": "<nil>", "site_tag_uid": "<nil>", "sun_valid": "false",
		"ip_match": "true", "trust": "70", "matched_sid": "base:ip-or-gps-ok", "client": "browser",
		"direction_source": "auto", "type": "in", "shift_source": "none",
	} {
		if got := str(r[col]); got != want {
			t.Errorf("%s = %s, want %s", col, got, want)
		}
	}
	if r["business_day_start"] == nil {
		t.Error("business_day_start is NULL: a directed live QR tap freezes its business day")
	}
	ev := h.row(t, "punch_events", "punch_id = $1", r["id"])
	if ev == nil {
		t.Fatal("no punch_events row: the feed skipped an ok live QR tap (00112's gate)")
	}
	if str(ev["channel"]) != "qr_live" || ev["tag_uid"] != nil || str(ev["location_id"]) != h.locationID.String() {
		t.Errorf("the event: channel %s tag %s location %s", str(ev["channel"]), str(ev["tag_uid"]), str(ev["location_id"]))
	}
}

// TestLiveQRDB_AHoldingScanIsRefusedAndTheSiteSeesIt is the card's second DB test (holding, Q43
// (b)): KM's employee (the harness's business) at a screen of KF, a peer -- refused AND recorded
// at the employer: verdict reject via sys:cross-tenant-live-qr, site_tenant_id KF, site_location_id
// the screen's venue, site_tag_uid NULL, location_id and tag_uid NULL, sun_valid false; no feed
// event; KF sees ONE `refused` site_punches row, written without a CHECK failure. The same screen of
// a business OUTSIDE the holding is the tenant-mismatch page and nothing is written (CONTROL).
func TestLiveQRDB_AHoldingScanIsRefusedAndTheSiteSeesIt(t *testing.T) {
	h := newLiveHarness(t)
	kf, kfVenue := uuid.New(), uuid.New()
	h.newTag(t, h.kek, kf, kfVenue, 100)
	sc := h.screenAt(t, kf, kfVenue)
	cookie := h.sessionCookieFor(t)

	before := h.countFor(t, h.employeeID)
	if w := h.scan(t, h.token(t, sc), cookie, onSiteAddr, venueFix()); w.Code != http.StatusForbidden {
		t.Fatalf("outside a holding: POST = %d, want 403 (sys:tenant-mismatch)", w.Code)
	}
	if n := h.countFor(t, h.employeeID); n != before {
		t.Fatalf("outside a holding: rows %d -> %d, want none written", before, n)
	}

	holdingOf(t, h.owner, h.tenantID, kf)
	cookie2 := h.cookieForEmployee(t, h.employeeID) // a fresh session: the debounce never saw a row anyway
	w := h.scan(t, h.token(t, sc), cookie2, onSiteAddr, venueFix())
	if w.Code != http.StatusOK {
		t.Fatalf("holding: POST = %d:\n%s", w.Code, w.Body.String())
	}
	r := h.lastRow(t, h.employeeID)
	for col, want := range map[string]string{
		"verdict": "reject", "channel": "qr_live", "matched_sid": "sys:cross-tenant-live-qr", "policy_layer": "guardrail",
		"site_tenant_id": kf.String(), "site_location_id": kfVenue.String(), "site_tag_uid": "<nil>",
		"location_id": "<nil>", "tag_uid": "<nil>", "sun_valid": "false", "type": "<nil>",
	} {
		if got := str(r[col]); got != want {
			t.Errorf("%s = %s, want %s", col, got, want)
		}
	}
	if n := h.count(t, `SELECT count(*) FROM punch_events WHERE punch_id = $1`, r["id"]); n != 0 {
		t.Errorf("%d feed events for a refused holding live QR tap, want 0", n)
	}
	sp := h.row(t, "site_punches", "transaction_id = $1", r["id"])
	if sp == nil {
		t.Fatal("KF has no site_punches row for the refusal (K-1h: the site sees the outcome)")
	}
	for col, want := range map[string]string{"tenant_id": kf.String(), "outcome": "refused", "channel": "qr_live",
		"site_location_id": kfVenue.String(), "site_tag_uid": "<nil>", "sun_valid": "false"} {
		if got := str(sp[col]); got != want {
			t.Errorf("site_punches.%s = %s, want %s", col, got, want)
		}
	}
}

// TestLiveQRDB_GPSAloneIsFlagged is the user's decision of 2026-11-19 on the product path: a live QR
// scan whose network is not the venue's but whose GPS is inside the radius is RECORDED as a flag by
// base:live-qr-requires-ip -- the document an existing business receives on its next tap -- and the
// same scan on the venue's network is ok.
func TestLiveQRDB_GPSAloneIsFlagged(t *testing.T) {
	h := newLiveHarness(t)
	sc := h.screenAt(t, h.tenantID, h.locationID)
	w := h.scan(t, h.token(t, sc), h.sessionCookieFor(t), offSiteAddr, venueFix())
	if w.Code != http.StatusOK {
		t.Fatalf("POST = %d", w.Code)
	}
	r := h.lastRow(t, h.employeeID)
	if str(r["verdict"]) != "flag" || str(r["matched_sid"]) != "base:live-qr-requires-ip" || str(r["trust"]) != "50" || str(r["gps_match"]) != "true" {
		t.Fatalf("GPS alone: verdict %s via %s trust %s gps %s, want flag via base:live-qr-requires-ip, 50",
			str(r["verdict"]), str(r["matched_sid"]), str(r["trust"]), str(r["gps_match"]))
	}
	if r["policy_version_id"] == nil || str(r["policy_layer"]) != "baseline" {
		t.Error("the flag does not cite its stored baseline version")
	}
}

// TestLiveQRDB_ABackdatedLiveQRPredecessorStillDebounces is ADR 0006's server-clock leg for the new
// channel (SecondsSinceLastRecordedTap): a live QR tap that DECLARES a time ten minutes ago is
// written now; a second live QR tap a moment later is `ignored`, because the database's clock says
// the first was written seconds ago -- the declared leg alone (600 s) would let it through.
// MUTANT (measured): the query's channel list without qr_live -- the second tap is a new record.
func TestLiveQRDB_ABackdatedLiveQRPredecessorStillDebounces(t *testing.T) {
	h := newLiveHarness(t)
	sc := h.screenAt(t, h.tenantID, h.locationID)
	cookie := h.sessionCookieFor(t)
	declared := url.Values{occurredAtField: {time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)}}
	if w := h.scan(t, h.token(t, sc), cookie, onSiteAddr, declared); w.Code != http.StatusOK {
		t.Fatalf("first = %d:\n%s", w.Code, w.Body.String())
	}
	first := h.lastRow(t, h.employeeID)
	if str(first["channel"]) != "qr_live" || str(first["verdict"]) == "ignored" {
		t.Fatalf("PREMISE: the backdated predecessor is a recorded live QR tap, got %s / %s", str(first["channel"]), str(first["verdict"]))
	}
	if w := h.scan(t, h.token(t, sc), cookie, onSiteAddr, nil); w.Code != http.StatusOK {
		t.Fatalf("second = %d", w.Code)
	}
	second := h.lastRow(t, h.employeeID)
	if str(second["verdict"]) != "ignored" || str(second["matched_sid"]) != "sys:person-debounce" {
		t.Fatalf("second: %s via %s, want ignored via sys:person-debounce (the server-clock leg)", str(second["verdict"]), str(second["matched_sid"]))
	}
}

// TestLiveQRDB_ChannelsShareOnePersonsDebounce: an NFC tap and then a live QR scan by the same
// person inside the window -- the scan is `ignored`; and the other way round. The window spans
// channels because it is the PERSON's.
func TestLiveQRDB_ChannelsShareOnePersonsDebounce(t *testing.T) {
	h := newLiveHarness(t)
	sc := h.screenAt(t, h.tenantID, h.locationID)
	cookie := h.sessionCookieFor(t)
	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("nfc = %d", w.Code)
	}
	h.scan(t, h.token(t, sc), cookie, onSiteAddr, nil)
	if r := h.lastRow(t, h.employeeID); str(r["channel"]) != "qr_live" || str(r["verdict"]) != "ignored" {
		t.Fatalf("live QR after NFC: %s / %s, want an ignored qr_live row", str(r["channel"]), str(r["verdict"]))
	}
	other, oc := h.clone(t)
	h.scan(t, h.token(t, sc), oc, onSiteAddr, nil)
	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+2), oc, nil); w.Code != http.StatusOK {
		t.Fatalf("nfc = %d", w.Code)
	}
	if r := h.lastRow(t, other); str(r["channel"]) != "nfc" || str(r["verdict"]) != "ignored" {
		t.Fatalf("NFC after live QR: %s / %s, want an ignored nfc row", str(r["channel"]), str(r["verdict"]))
	}
}

// TestLiveQRDB_TwoPeopleOneTokenAreBothRecorded: the token is not single use; two people reading the
// same code a moment apart each get their record.
func TestLiveQRDB_TwoPeopleOneTokenAreBothRecorded(t *testing.T) {
	h := newLiveHarness(t)
	sc := h.screenAt(t, h.tenantID, h.locationID)
	tok := h.token(t, sc)
	other, oc := h.clone(t)
	h.scan(t, tok, h.sessionCookieFor(t), onSiteAddr, nil)
	h.scan(t, tok, oc, onSiteAddr, nil)
	for _, id := range []uuid.UUID{h.employeeID, other} {
		if r := h.lastRow(t, id); str(r["verdict"]) != "ok" || str(r["channel"]) != "qr_live" {
			t.Fatalf("%v: %s / %s, want an ok live QR row each", id, str(r["verdict"]), str(r["channel"]))
		}
	}
}

// TestLiveQRDB_AForgedTokenIsARecordedRejectAndNothingLeaks is Q39 (a) and the card's log test: a
// token that is not a token, a genuine one with its tag changed, and one signed by another key each
// render the ordinary tap page, and the press RECORDS a reject via sys:live-qr-unverified at the
// person's own business with no venue. No log line of either layer, no response body and no
// audit_log detail carries the token, a fragment of it, or Open's error sentence.
func TestLiveQRDB_AForgedTokenIsARecordedRejectAndNothingLeaks(t *testing.T) {
	h := newLiveHarness(t)
	sc := h.screenAt(t, h.tenantID, h.locationID)
	logBuf := h.captureTapLog(t)
	genuine := h.token(t, sc)
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
	for name, tok := range map[string]string{"not a token": "not-a-token", "a changed tag": string(flipped), "another key": foreign} {
		_, cookie := h.clone(t)
		page := h.get(t, liveURL(tok), cookie)
		bodies.WriteString(page.Body.String())
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), `name="ctx"`) {
			t.Fatalf("%s: GET /q = %d without the tap form", name, page.Code)
		}
		w := h.postSigned(t, contextFieldOf(t, page.Body.String()), cookie, onSiteAddr, venueFix())
		bodies.WriteString(w.Body.String())
		if w.Code != http.StatusOK {
			t.Fatalf("%s: POST = %d", name, w.Code)
		}
		var raw []byte
		if err := h.owner.QueryRow(context.Background(), `SELECT to_jsonb(x) FROM transactions x WHERE tenant_id = $1 AND channel = 'qr_live'
			ORDER BY created_at DESC LIMIT 1`, h.tenantID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		r := map[string]any{}
		_ = json.Unmarshal(raw, &r)
		if str(r["verdict"]) != "reject" || str(r["matched_sid"]) != "sys:live-qr-unverified" || r["location_id"] != nil || r["site_tenant_id"] != nil {
			t.Fatalf("%s: %s via %s at %s, want a recorded reject via sys:live-qr-unverified with no venue",
				name, str(r["verdict"]), str(r["matched_sid"]), str(r["location_id"]))
		}
	}
	var trail bytes.Buffer
	rows, err := h.owner.Query(context.Background(), `SELECT coalesce(detail::text, '') FROM audit_log WHERE tenant_id = $1`, h.tenantID)
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
	for where, text := range map[string]string{"the log": logBuf.String(), "a response body": bodies.String(), "audit_log": trail.String()} {
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
	if !strings.Contains(logBuf.String(), `"kind":"signature"`) || !strings.Contains(logBuf.String(), `"kind":"malformed"`) {
		t.Errorf("CONTROL: the log does not name the two kinds -- is it capturing the page at all?\n%s", logBuf.String())
	}
}

// TestLiveQRDB_AnExpiredTokenIsAFlagWithItsNote is Q39 (b): a genuine token first seen 20 s past its
// expiry -- beyond the 10 s grace -- is RECORDED as a flag via sys:live-qr-expired, with its note,
// even on the venue's network; one seen inside the grace is an ordinary ok.
func TestLiveQRDB_AnExpiredTokenIsAFlagWithItsNote(t *testing.T) {
	h := newLiveHarness(t)
	sc := h.screenAt(t, h.tenantID, h.locationID)
	// Issued 40 s ago, a 20 s life: 20 s past its expiry when the page opens it.
	h.scan(t, h.tokenIssuedAgo(t, sc, 40*time.Second), h.sessionCookieFor(t), onSiteAddr, nil)
	r := h.lastRow(t, h.employeeID)
	if str(r["verdict"]) != "flag" || str(r["matched_sid"]) != "sys:live-qr-expired" || !strings.Contains(str(r["note"]), "run out") || str(r["queued"]) != "true" {
		t.Fatalf("expired: %s via %s note %q queued %s, want a queued flag with its note", str(r["verdict"]), str(r["matched_sid"]), str(r["note"]), str(r["queued"]))
	}
	other, oc := h.clone(t)
	// Issued 25 s ago: 5 s past expiry, inside the 10 s grace.
	h.scan(t, h.tokenIssuedAgo(t, sc, 25*time.Second), oc, onSiteAddr, nil)
	if r := h.lastRow(t, other); str(r["verdict"]) != "ok" {
		t.Fatalf("inside the grace: %s via %s, want ok", str(r["verdict"]), str(r["matched_sid"]))
	}
}

// TestLiveQRDB_ARemovedScreenOrAClosedVenueIsARecordedReject is §5 row 1's twin: the screen is
// removed from the panel BETWEEN the page and the press -- the token is still inside its window --
// and the press records a reject via sys:screen-not-active (the row is re-read at the press). A
// switched-off venue answers the same.
func TestLiveQRDB_ARemovedScreenOrAClosedVenueIsARecordedReject(t *testing.T) {
	h := newLiveHarness(t)
	sc := h.screenAt(t, h.tenantID, h.locationID)
	cookie := h.sessionCookieFor(t)
	signed := h.signedContextFrom(t, liveURL(h.token(t, sc)), cookie)
	if _, err := h.screens.Revoke(context.Background(), liveqr.RevokeCommand{TenantID: h.tenantID, ActorID: uuid.New(), DisplayID: sc.ID}); err != nil {
		t.Fatal(err)
	}
	h.postSigned(t, signed, cookie, onSiteAddr, nil)
	if r := h.lastRow(t, h.employeeID); str(r["verdict"]) != "reject" || str(r["matched_sid"]) != "sys:screen-not-active" {
		t.Fatalf("removed: %s via %s, want a recorded reject via sys:screen-not-active", str(r["verdict"]), str(r["matched_sid"]))
	}

	venue := uuid.New()
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO locations (id, tenant_id, name, static_ips) VALUES ($1, $2, 'Gzira', '{203.0.113.0/24}')`, venue, h.tenantID)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	sc2 := h.screenAt(t, h.tenantID, venue)
	other, oc := h.clone(t)
	signed = h.signedContextFrom(t, liveURL(h.token(t, sc2)), oc)
	if _, err := h.owner.Exec(context.Background(), `UPDATE locations SET active = false WHERE id = $1`, venue); err != nil {
		t.Fatal(err)
	}
	h.postSigned(t, signed, oc, onSiteAddr, nil)
	if r := h.lastRow(t, other); str(r["verdict"]) != "reject" || str(r["matched_sid"]) != "sys:screen-not-active" {
		t.Fatalf("closed venue: %s via %s, want a recorded reject via sys:screen-not-active", str(r["verdict"]), str(r["matched_sid"]))
	}
}

// --- the parity matrix ----------------------------------------------------------------------

// parityExcluded are the known structural differences between a static QR row and a live QR row of
// the same evidence (the card's "Bilinen yapısal farklar"), fixed against the schema:
//
//	id, created_at, occurred_at                       identity and time (two presses, seconds apart; the
//	                                                  business day is NOT excluded -- the clones share a
//	                                                  cutoff far from now, so both freeze the same one)
//	employee_id                                       the clone's identity
//	channel                                           qr vs qr_live -- the point
//	tag_uid, site_tag_uid                             the plaque; a screen has none
//	policy_context                                    compared by its own rule below
//
// and, in the FLAG cell only (parityFlagExcluded), the deciding rule: since the user's decision of
// 2026-11-19 a GPS-only static QR is flagged by base:qr-requires-ip and a GPS-only live QR by its
// twin base:live-qr-requires-ip -- the same effect from two documents, so matched_sid,
// policy_version_id and the rule's sentence (note) differ by design.
var (
	parityExcluded     = []string{"id", "created_at", "occurred_at", "employee_id", "channel", "tag_uid", "site_tag_uid", "policy_context"}
	parityFlagExcluded = []string{"matched_sid", "policy_version_id", "note"}
	// The policy_context rule (decide.go, liveQRKeys): a live QR context has no tag:* keys and
	// carries screen:status and the token's two facts; tap:channel and tap:pageAgeSeconds (time)
	// differ by construction. Every other key -- tap:ipMatch, tap:gpsMatch, location:id included --
	// is equal.
	contextExcluded = []string{"tap:channel", "tap:pageAgeSeconds", "tag:status", "tag:locationId", "screen:status", "tap:tokenVerified", "tap:tokenOverdueSeconds"}
	// punch_events: the feed's own identity and time, the clone, the channel and the plaque.
	eventExcluded = []string{"seq", "punch_id", "punch_time", "recorded_at", "created_at", "employee_id", "channel", "tag_uid"}
)

func without(m map[string]any, keys ...[]string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	for _, ks := range keys {
		for _, k := range ks {
			delete(out, k)
		}
	}
	return out
}

func diffMaps(t *testing.T, what string, a, b map[string]any) {
	t.Helper()
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		if str(a[k]) != str(b[k]) {
			t.Errorf("%s.%s: static %s, live %s", what, k, str(a[k]), str(b[k]))
		}
	}
}

func contextOf(t *testing.T, r map[string]any) map[string]any {
	t.Helper()
	m, ok := r["policy_context"].(map[string]any)
	if !ok {
		t.Fatalf("policy_context is %T", r["policy_context"])
	}
	return m
}

// TestLiveQRDB_ParityWithTheStaticQR is the card's completeness proof: every hand-off list of the
// M16-19 inventory is a start, and THIS is where a place nobody named shows up. Each cell runs one
// static QR press and one live QR press with the same evidence, by two people identical but for
// their id, through the real GET -> signed context -> POST -> checkin -> row path, and compares
// what each wrote.
//
//	local   × ok             (00067:193, the feed's ok gate)    transactions + punch_events equal
//	local   × flag -> approve (00067:234, the approval gate)    transactions + punch_events equal
//	holding × ok             (00067:366)   NOT parity (Q43 (b)): the live QR scan is a recorded
//	holding × flag -> approve (00067:408)   refusal -- one transactions row with the site named, no
//	                                        feed event, one `refused` site_punches row -- while the
//	                                        static QR is counted / approved there (K-1g), which is
//	                                        why its cell is the reference for nothing but the path.
func TestLiveQRDB_ParityWithTheStaticQR(t *testing.T) {
	h := newLiveHarness(t)
	local := h.screenAt(t, h.tenantID, h.locationID)

	type press func(cookie *http.Cookie) *httptest.ResponseRecorder
	staticAt := func(uid, addr string, fix url.Values) press {
		return func(c *http.Cookie) *httptest.ResponseRecorder {
			return h.postSigned(t, h.signedContextFrom(t, qrURLFor(uid), c), c, addr, fix)
		}
	}
	liveAt := func(sc liveqr.Screen, addr string, fix url.Values) press {
		return func(c *http.Cookie) *httptest.ResponseRecorder { return h.scan(t, h.token(t, sc), c, addr, fix) }
	}
	pair := func(t *testing.T, s, l press) (map[string]any, map[string]any) {
		t.Helper()
		sID, sc := h.clone(t)
		lID, lc := h.clone(t)
		if w := s(sc); w.Code != http.StatusOK {
			t.Fatalf("static press = %d", w.Code)
		}
		if w := l(lc); w.Code != http.StatusOK {
			t.Fatalf("live press = %d", w.Code)
		}
		return h.lastRow(t, sID), h.lastRow(t, lID)
	}

	t.Run("local_ok", func(t *testing.T) {
		s, l := pair(t, staticAt(h.tagUID, onSiteAddr, nil), liveAt(local, onSiteAddr, nil))
		if str(s["verdict"]) != "ok" || str(l["verdict"]) != "ok" {
			t.Fatalf("PREMISE: both ok, got %s / %s", str(s["verdict"]), str(l["verdict"]))
		}
		diffMaps(t, "transactions", without(s, parityExcluded), without(l, parityExcluded))
		diffMaps(t, "policy_context", without(contextOf(t, s), contextExcluded), without(contextOf(t, l), contextExcluded))
		se, le := h.row(t, "punch_events", "punch_id = $1", s["id"]), h.row(t, "punch_events", "punch_id = $1", l["id"])
		if se == nil || le == nil {
			t.Fatalf("feed events: static %v, live %v -- both ok taps enter the feed", se != nil, le != nil)
		}
		diffMaps(t, "punch_events", without(se, eventExcluded, []string{"employee_ref", "employee_code"}), without(le, eventExcluded, []string{"employee_ref", "employee_code"}))
	})

	t.Run("local_flag_then_approval", func(t *testing.T) {
		s, l := pair(t, staticAt(h.tagUID, offSiteAddr, venueFix()), liveAt(local, offSiteAddr, venueFix()))
		if str(s["verdict"]) != "flag" || str(l["verdict"]) != "flag" {
			t.Fatalf("PREMISE: both flag, got %s / %s", str(s["verdict"]), str(l["verdict"]))
		}
		if str(s["matched_sid"]) != "base:qr-requires-ip" || str(l["matched_sid"]) != "base:live-qr-requires-ip" {
			t.Errorf("the deciding rules: %s / %s", str(s["matched_sid"]), str(l["matched_sid"]))
		}
		diffMaps(t, "transactions", without(s, parityExcluded, parityFlagExcluded), without(l, parityExcluded, parityFlagExcluded))
		diffMaps(t, "policy_context", without(contextOf(t, s), contextExcluded), without(contextOf(t, l), contextExcluded))
		for _, r := range []map[string]any{s, l} {
			if n := h.count(t, `SELECT count(*) FROM punch_events WHERE punch_id = $1`, r["id"]); n != 0 {
				t.Fatalf("a flag entered the feed before its approval (%d)", n)
			}
			h.approve(t, h.tenantID, r["id"])
		}
		se, le := h.row(t, "punch_events", "punch_id = $1", s["id"]), h.row(t, "punch_events", "punch_id = $1", l["id"])
		if se == nil || le == nil {
			t.Fatalf("after approval: static event %v, live event %v -- the approval gate (00067:234) skipped one", se != nil, le != nil)
		}
		diffMaps(t, "punch_events", without(se, eventExcluded, []string{"employee_ref", "employee_code"}), without(le, eventExcluded, []string{"employee_ref", "employee_code"}))
	})

	kf, kfVenue := uuid.New(), uuid.New()
	kfUID := h.newTag(t, h.kek, kf, kfVenue, 100)
	peer := h.screenAt(t, kf, kfVenue)
	holdingOf(t, h.owner, h.tenantID, kf)

	holdingCell := func(t *testing.T, addr string, fix url.Values, staticVerdict, siteOutcome string) {
		t.Helper()
		s, l := pair(t, staticAt(kfUID, addr, fix), liveAt(peer, addr, fix))
		if str(s["verdict"]) != staticVerdict || str(s["site_tenant_id"]) != kf.String() {
			t.Fatalf("PREMISE: the static QR at the peer is %s with the site named, got %s / %s", staticVerdict, str(s["verdict"]), str(s["site_tenant_id"]))
		}
		if staticVerdict == "flag" {
			h.approve(t, h.tenantID, s["id"])
		}
		if sp := h.row(t, "site_punches", "transaction_id = $1 ORDER BY created_at DESC", s["id"]); sp == nil || str(sp["outcome"]) != siteOutcome {
			t.Fatalf("PREMISE: the static QR's site row is %s, got %v", siteOutcome, sp)
		}
		// The live QR scan: a recorded refusal, the site named, no plaque, no feed event.
		for col, want := range map[string]string{"verdict": "reject", "channel": "qr_live", "matched_sid": "sys:cross-tenant-live-qr",
			"site_tenant_id": kf.String(), "site_location_id": kfVenue.String(), "site_tag_uid": "<nil>", "location_id": "<nil>",
			"tag_uid": "<nil>", "sun_valid": "false", "type": "<nil>"} {
			if got := str(l[col]); got != want {
				t.Errorf("live row %s = %s, want %s", col, got, want)
			}
		}
		if n := h.count(t, `SELECT count(*) FROM punch_events WHERE punch_id = $1`, l["id"]); n != 0 {
			t.Errorf("%d feed events for the refused live QR scan", n)
		}
		sp := h.row(t, "site_punches", "transaction_id = $1", l["id"])
		if sp == nil || str(sp["outcome"]) != "refused" || str(sp["channel"]) != "qr_live" || sp["site_tag_uid"] != nil {
			t.Errorf("the live QR refusal's site row: %v", sp)
		}
	}
	t.Run("holding_ok", func(t *testing.T) { holdingCell(t, onSiteAddr, nil, "ok", "counted") })
	t.Run("holding_flag_then_approval", func(t *testing.T) { holdingCell(t, offSiteAddr, venueFix(), "flag", "approved") })
}

// TestLiveQRDB_TheAppPathRefusesALiveQRURLBeforeAnything was M16-21's measurement for M16-22: a
// live QR URL's query (t=<token>) sent as a PLAQUE's (`sun`) is not one -- sun.Parse wants tag -- so
// tap/start answers 400 bad_request, writes no record and moves no counter. M16-22 opened the app's
// live QR read on its own field (`liveQR`, applive_db_test.go); this holds that the `sun` field
// still never reads a token -- the channel comes from the field the token is opened from, never
// from what a value looks like.
func TestLiveQRDB_TheAppPathRefusesALiveQRURLBeforeAnything(t *testing.T) {
	h := newAppHarness(t)
	p := newAppPhone(t)
	h.register(t, p, h.employeeID)
	secret := sha256.Sum256([]byte("m16-21 app path test secret, not a key"))
	keys, err := liveqr.NewKeys(secret[:])
	if err != nil {
		t.Fatal(err)
	}
	tok, err := keys.Mint(uuid.New(), h.locationID, time.Now(), liveqr.TokenLifetime)
	if err != nil {
		t.Fatal(err)
	}
	before, ctr := h.countFor(t, h.employeeID), h.lastCtr(t, h.tagUID)
	_, w := h.tapStart(t, p, liveqr.TokenParam+"="+url.QueryEscape(tok))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), string(appErrBadRequest)) {
		t.Fatalf("tap/start with a live QR query = %d %s, want 400 %s", w.Code, w.Body.String(), appErrBadRequest)
	}
	if strings.Contains(w.Body.String(), tok[5:40]) {
		t.Fatal("the error body carries the token")
	}
	if h.countFor(t, h.employeeID) != before || h.lastCtr(t, h.tagUID) != ctr {
		t.Fatal("tap/start with a live QR query wrote a record or moved a counter")
	}
}
