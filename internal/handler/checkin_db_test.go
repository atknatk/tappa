package handler

// POST /api/checkin against a REAL Postgres, through the REAL router, with the
// REAL decision engine, the REAL policy set and the REAL atomic counter.
//
// 🔴 THIS IS THE FILE THAT PROVES M3 AND M4 ARE WIRED IN. Until M5-05 they had
// never been reached by a request: the policy engine, the ten guardrails and
// tap.Decide were proven as pure functions with table-driven tests, and nothing
// had ever driven them from an HTTP request through a decision to a row. Every
// row of CLAUDE.md §5's decision table is exercised here end to end, and the
// assertion is never only "the right verdict came back" — it is also the ROW
// COUNT, because §4.6's promise is about records existing, not about verdicts.
//
// WHAT IS SIMULATED AND WHAT IS NOT, stated up front because it is the one place
// this file cannot use production's own value. The signed tap context is minted
// through the PRODUCTION mint function with the PRODUCTION key; the only thing a
// test chooses is what the CMAC check concluded (CMACVerified), which is exactly
// the single bit GET /t puts in it. Minting a genuine SDM MAC here would mean
// writing a second copy of the SV2/CMAC derivation outside internal/sun, which is
// the failure mode that package spends two files avoiding — and the genuine-MAC
// path is measured where the derivation lives (internal/sun, and independently by
// the M5-04 audit, which minted a real SUN URL from scratch and watched Verify
// accept it once and refuse the replay).
//
// Fixtures are NOT cleaned up: tappa_app has REVOKE DELETE on tags, transactions
// and audit_log. Fresh random ids per run keep them from colliding.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
)

// recordedTap is one transactions row, read back so a test asserts on what was
// PERSISTED rather than on what the handler said.
type recordedTap struct {
	ID              uuid.UUID
	Verdict         string
	Type            *string
	SunValid        *bool
	IPMatch         *bool
	GPSMatch        *bool
	Trust           *int16
	Channel         string
	Practice        bool
	Queued          bool
	MatchedSid      *string
	PolicyLayer     *string
	PolicyVersionID *uuid.UUID
	PolicyContext   []byte
	Ctr             *int32
	TagUID          *string
	LocationID      *uuid.UUID
	OccurredAt      time.Time
	Note            *string
}

// --- harness extensions ------------------------------------------------------

// postTap posts a signed context for the harness's employee. The context is
// minted the way GET /t mints one.
func (h *tapHarness) postTap(t *testing.T, c tapContext, cookie *http.Cookie, extra url.Values) *httptest.ResponseRecorder {
	t.Helper()
	sid := h.sessionIDOf(t, cookie)
	signed, err := h.tap.contexts.mint(c, sid)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	form := url.Values{"ctx": {signed}}
	for k, vs := range extra {
		form[k] = vs
	}
	req := httptest.NewRequest(http.MethodPost, "/api/checkin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.5:41234" // inside the fixture location's 203.0.113.0/24
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.router.ServeHTTP(w, req)
	return w
}

// postFrom is postTap with a chosen source address, for the IP-evidence cases.
func (h *tapHarness) postFrom(t *testing.T, c tapContext, cookie *http.Cookie, remoteAddr string, extra url.Values) *httptest.ResponseRecorder {
	t.Helper()
	sid := h.sessionIDOf(t, cookie)
	signed, err := h.tap.contexts.mint(c, sid)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
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

// sessionIDOf resolves a cookie to its session id the same way the middleware
// does, so a test signs a context for the session the handler will see.
func (h *tapHarness) sessionIDOf(t *testing.T, cookie *http.Cookie) uuid.UUID {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/t", nil)
	req.AddCookie(cookie)
	tok, err := h.cookies.Read(req)
	if err != nil {
		t.Fatalf("read cookie: %v", err)
	}
	res, err := h.sessions.Verify(context.Background(), tok)
	if err != nil {
		t.Fatalf("verify session: %v", err)
	}
	return res.ID
}

// nfcContext is a context for the harness's tag with a VERIFIED CMAC.
func (h *tapHarness) nfcContext(ctr uint32) tapContext {
	return tapContext{
		UID: h.tagUID, Ctr: ctr, Channel: sun.ChannelNFC, CMACVerified: true,
		TagTenantID: h.tenantID, LocationID: h.locationID,
	}
}

// qrContext is the SUN-less fallback: no counter, no MAC to verify.
func (h *tapHarness) qrContext() tapContext {
	return tapContext{
		UID: h.tagUID, Channel: sun.ChannelQR, CMACVerified: false,
		TagTenantID: h.tenantID, LocationID: h.locationID,
	}
}

// lastRecord reads back the most recent transaction for one employee.
func (h *tapHarness) lastRecord(t *testing.T, employeeID uuid.UUID) recordedTap {
	t.Helper()
	var r recordedTap
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id, verdict, type, sun_valid, ip_match, gps_match, trust, channel,
			       practice, queued, matched_sid, policy_layer, policy_version_id,
			       policy_context, ctr, tag_uid, location_id, occurred_at, note
			FROM transactions
			WHERE tenant_id = $1 AND employee_id = $2
			ORDER BY created_at DESC, occurred_at DESC LIMIT 1`,
			h.tenantID, employeeID).Scan(
			&r.ID, &r.Verdict, &r.Type, &r.SunValid, &r.IPMatch, &r.GPSMatch, &r.Trust, &r.Channel,
			&r.Practice, &r.Queued, &r.MatchedSid, &r.PolicyLayer, &r.PolicyVersionID,
			&r.PolicyContext, &r.Ctr, &r.TagUID, &r.LocationID, &r.OccurredAt, &r.Note)
	})
	if err != nil {
		t.Fatalf("read back the record: %v", err)
	}
	return r
}

// countFor counts one employee's rows, so concurrent runs of other tests cannot
// move the number under an assertion.
func (h *tapHarness) countFor(t *testing.T, employeeID uuid.UUID) int {
	t.Helper()
	var n int
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM transactions WHERE tenant_id = $1 AND employee_id = $2`,
			h.tenantID, employeeID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// newEmployee adds a second person to the harness's tenant, with a chosen status.
func (h *tapHarness) newEmployee(t *testing.T, status string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at, activated_at)
			 VALUES ($1, $2, $3, 'Joseph Camilleri', $4, now(), now())`,
			id, h.tenantID, h.locationID, status)
		return e
	})
	if err != nil {
		t.Fatalf("fixture employee: %v", err)
	}
	return id
}

// cookieForEmployee issues a session for any employee in the harness's tenant.
func (h *tapHarness) cookieForEmployee(t *testing.T, employeeID uuid.UUID) *http.Cookie {
	t.Helper()
	issued, err := h.sessions.Issue(context.Background(), session.IssueParams{
		TenantID: h.tenantID, EmployeeID: employeeID, DeviceInfo: "test",
	})
	if err != nil {
		t.Fatalf("session.Issue: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := h.cookies.Set(rec, issued.Token); err != nil {
		t.Fatalf("cookies.Set: %v", err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

// retireTag marks the harness's plaque lost or retired — §5 row 1.
func (h *tapHarness) retireTag(t *testing.T, uid, status string) {
	t.Helper()
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE tags SET status = $3 WHERE tenant_id = $1 AND uid = $2`,
			h.tenantID, uid, status)
		return e
	})
	if err != nil {
		t.Fatalf("retire tag: %v", err)
	}
}

// ---------------------------------------------------------------------------
// §5 — the seven rows, end to end.
// ---------------------------------------------------------------------------

// TestCheckinDB_Row1_DeadPlaqueIsRejectedAndRECORDED. §5 row 1: a retired or lost
// plaque is a reject — and the reject is WRITTEN (§4.6). A `lost` plaque also
// raises a security alert, because a tag reported lost being tapped is a fact
// somebody needs to see.
//
// It also proves the counter is NOT advanced for a dead plaque, which is the same
// short-circuit sun.Verify makes: a retired tag must never reach cryptography or
// the counter.
func TestCheckinDB_Row1_DeadPlaqueIsRejectedAndRECORDED(t *testing.T) {
	for _, status := range []string{"retired", "lost"} {
		t.Run(status, func(t *testing.T) {
			h := newTapHarness(t)
			h.retireTag(t, h.tagUID, status)
			cookie := h.sessionCookieFor(t)
			before := h.countFor(t, h.employeeID)
			alertsBefore := h.auditCount(t, "tap.security_alert")

			w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (a reject is a rendered outcome, not an HTTP error)", w.Code)
			}

			if after := h.countFor(t, h.employeeID); after != before+1 {
				t.Fatalf("rows went %d -> %d, want +1: §4.6 records a reject", before, after)
			}
			rec := h.lastRecord(t, h.employeeID)
			if rec.Verdict != "reject" {
				t.Fatalf("verdict = %q, want reject", rec.Verdict)
			}
			if rec.MatchedSid == nil || *rec.MatchedSid != "sys:tag-not-active" {
				t.Fatalf("matched_sid = %q, want sys:tag-not-active", deref(rec.MatchedSid))
			}
			if rec.PolicyLayer == nil || *rec.PolicyLayer != "guardrail" || rec.PolicyVersionID != nil {
				t.Fatalf("a guardrail decision must carry layer=guardrail and NO version id, got %v / %v",
					rec.PolicyLayer, rec.PolicyVersionID)
			}
			if rec.Type != nil {
				t.Fatalf("a reject carries no direction, got %q", *rec.Type)
			}
			if got := h.lastCtr(t, h.tagUID); got != h.startCtr {
				t.Fatalf("last_ctr = %d, want %d: a dead plaque must never move the counter", got, h.startCtr)
			}
			alerts := h.auditCount(t, "tap.security_alert") - alertsBefore
			if status == "lost" && alerts != 1 {
				t.Fatalf("a lost plaque produced %d security alerts, want 1", alerts)
			}
			if status == "retired" && alerts != 0 {
				t.Fatalf("a retired plaque produced %d security alerts, want 0 (routine lifecycle)", alerts)
			}
		})
	}
}

// TestCheckinDB_Row2_SunInvalidIsRejectedAndRECORDED. §5 row 2 has TWO shapes and
// both are here, because the contract is an AND of two halves:
//
//	CMAC did not verify   -> the counter is NOT advanced at all (advancing on an
//	                         unverified MAC is §4.4's denial of service: push
//	                         last_ctr past the chip and every real tap after it is
//	                         a replay).
//	counter did not move  -> a replay: a perfectly good CMAC for a pair that has
//	                         already been spent.
//
// A caller that read CMACVerified without advancing would record the second as
// `ok`; one that advanced without reading CMACVerified would record the first.
func TestCheckinDB_Row2_SunInvalidIsRejectedAndRECORDED(t *testing.T) {
	t.Run("cmac did not verify", func(t *testing.T) {
		h := newTapHarness(t)
		cookie := h.sessionCookieFor(t)
		before := h.countFor(t, h.employeeID)

		c := h.nfcContext(uint32(h.startCtr) + 1)
		c.CMACVerified = false
		w := h.postTap(t, c, cookie, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}

		if after := h.countFor(t, h.employeeID); after != before+1 {
			t.Fatalf("rows went %d -> %d, want +1", before, after)
		}
		rec := h.lastRecord(t, h.employeeID)
		if rec.Verdict != "reject" || rec.MatchedSid == nil || *rec.MatchedSid != "sys:sun-invalid" {
			t.Fatalf("verdict/sid = %q/%v, want reject/sys:sun-invalid", rec.Verdict, deref(rec.MatchedSid))
		}
		if rec.SunValid == nil || *rec.SunValid {
			t.Fatalf("sun_valid = %v, want false", rec.SunValid)
		}
		if got := h.lastCtr(t, h.tagUID); got != h.startCtr {
			t.Fatalf("last_ctr = %d, want %d UNCHANGED: an unverified MAC must not spend a counter (§4.4)",
				got, h.startCtr)
		}
	})

	t.Run("counter did not advance (replay)", func(t *testing.T) {
		h := newTapHarness(t)
		cookie := h.sessionCookieFor(t)
		fresh := uint32(h.startCtr) + 1

		// A genuine tap first, so the counter moves.
		if w := h.postTap(t, h.nfcContext(fresh), cookie, nil); w.Code != http.StatusOK {
			t.Fatalf("first tap status = %d", w.Code)
		}
		if got := h.lastCtr(t, h.tagUID); got != int32(fresh) {
			t.Fatalf("last_ctr = %d, want %d after a genuine tap", got, fresh)
		}
		before := h.countFor(t, h.employeeID)

		// The SAME (uid, ctr) again — a kept URL, replayed.
		w := h.postTap(t, h.nfcContext(fresh), cookie, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("replay status = %d, want 200", w.Code)
		}
		if after := h.countFor(t, h.employeeID); after != before+1 {
			t.Fatalf("rows went %d -> %d, want +1: a replay is REJECTED and RECORDED", before, after)
		}
		rec := h.lastRecord(t, h.employeeID)
		if rec.Verdict != "reject" || rec.MatchedSid == nil || *rec.MatchedSid != "sys:sun-invalid" {
			t.Fatalf("verdict/sid = %q/%v, want reject/sys:sun-invalid — the replay was not caught",
				rec.Verdict, deref(rec.MatchedSid))
		}
		if got := h.lastCtr(t, h.tagUID); got != int32(fresh) {
			t.Fatalf("last_ctr = %d, want %d: a replay must not move the counter", got, fresh)
		}
	})
}

// TestCheckinDB_Row3_NoSessionRedirectsAndWritesNOTHING. §5 row 3 is the ONE
// exception to "a tap is a record": there is no session, so there is no person to
// record against. The row count is the assertion.
func TestCheckinDB_Row3_NoSessionRedirectsAndWritesNOTHING(t *testing.T) {
	h := newTapHarness(t)
	before := h.transactionCount(t)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/checkin",
			strings.NewReader("ctx=whatever"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "203.0.113.5:41234"
		w := httptest.NewRecorder()
		h.router.ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("post %d: status = %d, want 303", i, w.Code)
		}
		if loc := w.Header().Get("Location"); loc != "/activate" {
			t.Fatalf("post %d: Location = %q", i, loc)
		}
	}

	if after := h.transactionCount(t); after != before {
		t.Fatalf("transactions went %d -> %d: §5 row 3 forbids a record for a session-less tap", before, after)
	}
	if got := h.lastCtr(t, h.tagUID); got != h.startCtr {
		t.Fatalf("last_ctr moved to %d on a session-less post", got)
	}
}

// TestCheckinDB_Row4_DeactivatedEmployeeIsRejectedLoggedAndAlerted. §5 row 4: a
// deactivated account with a LIVE session (deactivation deliberately does not
// revoke — M5-01) taps, and the attempt is rejected, RECORDED, and raised.
func TestCheckinDB_Row4_DeactivatedEmployeeIsRejectedLoggedAndAlerted(t *testing.T) {
	h := newTapHarness(t)
	emp := h.newEmployee(t, "deactivated")
	cookie := h.cookieForEmployee(t, emp)
	before := h.countFor(t, emp)
	alertsBefore := h.auditCount(t, "tap.security_alert")

	w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	if after := h.countFor(t, emp); after != before+1 {
		t.Fatalf("rows went %d -> %d, want +1: a deactivated attempt is RECORDED", before, after)
	}
	rec := h.lastRecord(t, emp)
	if rec.Verdict != "reject" || rec.MatchedSid == nil || *rec.MatchedSid != "sys:employee-deactivated" {
		t.Fatalf("verdict/sid = %q/%v, want reject/sys:employee-deactivated", rec.Verdict, deref(rec.MatchedSid))
	}
	if got := h.auditCount(t, "tap.security_alert") - alertsBefore; got != 1 {
		t.Fatalf("security alerts = %d, want 1", got)
	}
}

// TestCheckinDB_Row5_SamePersonWithinTheWindowIsIgnoredAndRECORDED. §5 row 5. The
// debounce is PER PERSON, and the second half of this test is the part the rule
// exists for: a queue of DIFFERENT people tapping one plaque back to back must
// each be recorded.
//
// The channel is QR here, deliberately: an NFC replay would be caught by
// sys:sun-invalid first (guardrail order), so the only way to reach the debounce
// with a second tap is a channel that has no counter to replay.
func TestCheckinDB_Row5_SamePersonWithinTheWindowIsIgnoredAndRECORDED(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)

	if w := h.postTap(t, h.qrContext(), cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("first tap status = %d", w.Code)
	}
	first := h.lastRecord(t, h.employeeID)
	if first.Verdict == "ignored" {
		t.Fatal("the FIRST tap was debounced: nil last-tap must never debounce")
	}
	before := h.countFor(t, h.employeeID)

	w := h.postTap(t, h.qrContext(), cookie, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("second tap status = %d", w.Code)
	}
	if after := h.countFor(t, h.employeeID); after != before+1 {
		t.Fatalf("rows went %d -> %d, want +1: an ignored duplicate is still RECORDED", before, after)
	}
	rec := h.lastRecord(t, h.employeeID)
	if rec.Verdict != "ignored" || rec.MatchedSid == nil || *rec.MatchedSid != "sys:person-debounce" {
		t.Fatalf("verdict/sid = %q/%v, want ignored/sys:person-debounce", rec.Verdict, deref(rec.MatchedSid))
	}
	if rec.Type != nil {
		t.Fatalf("an ignored tap carries no direction, got %q", *rec.Type)
	}

	// THE OTHER HALF OF ROW 5: the debounce is per PERSON, not per TAG. A second
	// person tapping the same plaque one second later is a queue, not a duplicate.
	other := h.newEmployee(t, "active")
	otherCookie := h.cookieForEmployee(t, other)
	if w := h.postTap(t, h.qrContext(), otherCookie, nil); w.Code != http.StatusOK {
		t.Fatalf("second person status = %d", w.Code)
	}
	otherRec := h.lastRecord(t, other)
	if otherRec.Verdict == "ignored" {
		t.Fatal("a DIFFERENT person was debounced against the first person's tap: the window is per person (§5)")
	}
}

// TestCheckinDB_Row6_EvidenceMakesItOK. §5 row 6: an IP match, or a GPS fix
// inside the radius, is proof of place -> ok. Both branches are here, and so is
// the trust arithmetic (20 + 50 IP + 30 GPS).
//
// It is ALSO the test that proves the baseline was materialised: an `ok` is a
// BASELINE decision, so the row must carry a real policy_version_id — which is
// exactly what could not be written before this task (measured: 23503).
func TestCheckinDB_Row6_EvidenceMakesItOK(t *testing.T) {
	t.Run("ip match", func(t *testing.T) {
		h := newTapHarness(t)
		cookie := h.sessionCookieFor(t)
		before := h.countFor(t, h.employeeID)

		w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		if after := h.countFor(t, h.employeeID); after != before+1 {
			t.Fatalf("rows went %d -> %d, want +1", before, after)
		}
		rec := h.lastRecord(t, h.employeeID)
		if rec.Verdict != "ok" {
			t.Fatalf("verdict = %q, want ok (sid %q)", rec.Verdict, deref(rec.MatchedSid))
		}
		if rec.Type == nil || *rec.Type != "in" {
			t.Fatalf("direction = %v, want in (no open check-in yet)", rec.Type)
		}
		if rec.IPMatch == nil || !*rec.IPMatch {
			t.Fatal("ip_match was not recorded")
		}
		if rec.Trust == nil || *rec.Trust != 70 {
			t.Fatalf("trust = %v, want 70 (20 base + 50 IP)", rec.Trust)
		}
		if rec.SunValid == nil || !*rec.SunValid {
			t.Fatal("sun_valid = false on a verified CMAC with an advancing counter")
		}
		// 🔴 THE BASELINE FIDELITY CHECK (hand-off N4): a baseline decision MUST
		// carry a real, resolvable version id. Before this task there was none to
		// carry and the insert failed with 23503.
		if rec.PolicyLayer == nil || *rec.PolicyLayer != "baseline" {
			t.Fatalf("policy_layer = %v, want baseline", rec.PolicyLayer)
		}
		if rec.PolicyVersionID == nil || *rec.PolicyVersionID == uuid.Nil {
			t.Fatalf("policy_version_id = %v: a baseline decision must pin a real version", rec.PolicyVersionID)
		}
		if got := h.lastCtr(t, h.tagUID); got != h.startCtr+1 {
			t.Fatalf("last_ctr = %d, want %d: the genuine tap did not advance the counter", got, h.startCtr+1)
		}
		if rec.Queued {
			t.Fatal("an ok record was queued for approval")
		}
	})

	t.Run("gps only", func(t *testing.T) {
		h := newTapHarness(t)
		cookie := h.sessionCookieFor(t)

		// Off the venue's network, but standing at its coordinates.
		w := h.postFrom(t, h.nfcContext(uint32(h.startCtr)+1), cookie, "198.51.100.9:5000",
			url.Values{"lat": {"35.918"}, "lng": {"14.489"}})
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		rec := h.lastRecord(t, h.employeeID)
		if rec.Verdict != "ok" {
			t.Fatalf("verdict = %q, want ok (sid %q)", rec.Verdict, deref(rec.MatchedSid))
		}
		if rec.IPMatch == nil || *rec.IPMatch {
			t.Fatal("ip_match should be false off the venue network")
		}
		if rec.GPSMatch == nil || !*rec.GPSMatch {
			t.Fatal("gps_match should be true at the venue's coordinates")
		}
		if rec.Trust == nil || *rec.Trust != 50 {
			t.Fatalf("trust = %v, want 50 (20 base + 30 GPS)", rec.Trust)
		}
		if rec.Note == nil || !strings.Contains(*rec.Note, "GPS") {
			t.Fatalf("note = %v, want the GPS-only explanation (§5 row 6)", rec.Note)
		}
	})
}

// TestCheckinDB_Row7_NoEvidenceIsFlaggedAndRECORDED. §5 row 7: neither IP nor GPS
// could place the tap, so it is FLAGGED — written, queued for approval, never
// silently approved and never dropped (§4.6).
func TestCheckinDB_Row7_NoEvidenceIsFlaggedAndRECORDED(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)
	before := h.countFor(t, h.employeeID)

	// Mobile data, no fix.
	w := h.postFrom(t, h.nfcContext(uint32(h.startCtr)+1), cookie, "198.51.100.9:5000", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	if after := h.countFor(t, h.employeeID); after != before+1 {
		t.Fatalf("rows went %d -> %d, want +1", before, after)
	}
	rec := h.lastRecord(t, h.employeeID)
	if rec.Verdict != "flag" {
		t.Fatalf("verdict = %q, want flag (sid %q)", rec.Verdict, deref(rec.MatchedSid))
	}
	if !rec.Queued {
		t.Fatal("a flagged record was not queued for approval")
	}
	if rec.Type == nil {
		t.Fatal("a flagged record carries a direction — it joins the in/out chain once approved")
	}
	if rec.Trust == nil || *rec.Trust != 20 {
		t.Fatalf("trust = %v, want 20 (base only)", rec.Trust)
	}
	if rec.PolicyLayer == nil || *rec.PolicyLayer != "baseline" || rec.PolicyVersionID == nil {
		t.Fatalf("a baseline flag must pin a version: layer=%v version=%v", rec.PolicyLayer, rec.PolicyVersionID)
	}
}

// ---------------------------------------------------------------------------
// §4.4 — the atomic counter, under real concurrency.
// ---------------------------------------------------------------------------

// TestCheckinDB_ConcurrentTapsAdvanceTheCounterExactlyOnce. N goroutines post the
// SAME (uid, ctr) at once. Exactly ONE may be SUN-valid; the rest are replays.
//
// WHAT MAKES THIS A REAL TEST rather than a slow one: the winner is decided by a
// single UPDATE with the comparison inside it, so the losers lose in the
// DATABASE. A read-then-compare-then-write in Go would let several through, and
// this is the shape that catches it — a negative control that does exactly that
// was run by hand while writing this and produced 5 winners out of 12 (reverted).
//
// EVERY LOSER IS STILL RECORDED (§4.6): a replay is a reject with a row, not a
// dropped request. So the row count goes up by N, not by 1.
func TestCheckinDB_ConcurrentTapsAdvanceTheCounterExactlyOnce(t *testing.T) {
	h := newTapHarness(t)
	const racers = 12

	// One employee per goroutine: the debounce is per person, and 12 taps by ONE
	// person would be swallowed by it before the counter was ever reached. Each
	// person is a separate session, which is also what the session-keyed rate
	// limit expects.
	cookies := make([]*http.Cookie, racers)
	employees := make([]uuid.UUID, racers)
	for i := range cookies {
		employees[i] = h.newEmployee(t, "active")
		cookies[i] = h.cookieForEmployee(t, employees[i])
	}
	fresh := uint32(h.startCtr) + 1
	before := h.transactionCount(t)

	var wg sync.WaitGroup
	codes := make([]int, racers)
	verdicts := make([]string, racers)
	var mu sync.Mutex
	// A start barrier, so the racers arrive TOGETHER rather than in the order the
	// scheduler happened to start them. Without it the requests tend to serialise
	// on the pool and a broken (read-then-write) implementation would still pass.
	start := make(chan struct{})
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			w := h.postTap(t, h.nfcContext(fresh), cookies[i], nil)
			mu.Lock()
			codes[i] = w.Code
			mu.Unlock()
		}(i)
	}
	close(start)
	wg.Wait()

	for i, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("racer %d: status = %d, want 200", i, c)
		}
	}
	winners := 0
	for i := range employees {
		rec := h.lastRecord(t, employees[i])
		verdicts[i] = rec.Verdict
		if rec.SunValid != nil && *rec.SunValid {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("%d of %d concurrent taps were SUN-valid, want exactly 1 (§4.4): %v", winners, racers, verdicts)
	}
	if got := h.lastCtr(t, h.tagUID); got != int32(fresh) {
		t.Fatalf("last_ctr = %d, want %d: the counter moved more than once", got, fresh)
	}
	if after := h.transactionCount(t); after != before+racers {
		t.Fatalf("rows went %d -> %d, want +%d: every loser is still RECORDED (§4.6)", before, after, racers)
	}
}

// ---------------------------------------------------------------------------
// §4.5 / hand-off N5 — the cross-tenant hole.
// ---------------------------------------------------------------------------

// TestCheckinDB_ForeignTenantTapIsRefusedAndWritesNOTHING is the hole state.md
// tracked as N5, closed.
//
// THE SETUP IS THE ONE THAT USED TO PRODUCE AN `ok`: an employee of tenant B is
// PHYSICALLY INSIDE tenant A — same network, so tenant A's IP evidence matches
// perfectly — and taps tenant A's plaque. The tag lookup is context-less (ADR
// 0002 item 7), so row-level security does not hide the plaque, and before
// tap.Input carried both tenants the decision engine had nothing to compare:
// evidence matched, session was valid, verdict was `ok`, written into tenant B.
//
// The record count is asserted in BOTH tenants: a mismatch REDIRECTS, so the tap
// lands in neither.
func TestCheckinDB_ForeignTenantTapIsRefusedAndWritesNOTHING(t *testing.T) {
	h := newTapHarness(t)
	// A second organisation with its own plaque, at its own location.
	otherTenant, otherLocation := uuid.New(), uuid.New()
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatalf("kek: %v", err)
	}
	foreignUID := h.newTag(t, kek, otherTenant, otherLocation, 500)

	cookie := h.sessionCookieFor(t) // a session in h.tenantID
	beforeOurs := h.transactionCount(t)
	beforeTheirs := countTransactionsIn(t, h, otherTenant)

	w := h.postTap(t, tapContext{
		UID: foreignUID, Ctr: 501, Channel: sun.ChannelNFC, CMACVerified: true,
		TagTenantID: otherTenant, LocationID: otherLocation,
	}, cookie, nil)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — a cross-tenant tap must not be recorded", w.Code)
	}
	if body := w.Body.String(); strings.Contains(body, "APPROVED") || strings.Contains(body, "All done") {
		t.Fatal("a cross-tenant tap was rendered as a successful check-in")
	}
	if after := h.transactionCount(t); after != beforeOurs {
		t.Fatalf("rows in the SESSION's tenant went %d -> %d: a mismatch writes nothing", beforeOurs, after)
	}
	if after := countTransactionsIn(t, h, otherTenant); after != beforeTheirs {
		t.Fatalf("rows in the TAG's tenant went %d -> %d: a mismatch writes nothing", beforeTheirs, after)
	}

	// POSITIVE CONTROL. The very same request shape, against this employee's OWN
	// plaque, IS recorded — so the assertion above is measuring the tenant check
	// and not some unrelated refusal.
	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("positive control: status = %d, want 200", w.Code)
	}
	if after := h.transactionCount(t); after != beforeOurs+1 {
		t.Fatalf("positive control: rows went %d -> %d, want +1", beforeOurs, after)
	}
}

// TestCheckinDB_TenantMismatchGuardrailIsNotDead is the MUTATION half of N5, and
// it is here because "the guardrail fires" is only meaningful if not feeding it
// makes the tap succeed. It drives tap.Decide directly with the same shape the
// cross-tenant request produces, once WITH the two tenant ids and once WITHOUT —
// which is exactly the state the engine was in before M5-05.
//
// WITHOUT the feed the tap is `ok`. That is the hole, reproduced.
func TestCheckinDB_TenantMismatchGuardrailIsNotDead(t *testing.T) {
	tenantA, tenantB := uuid.New(), uuid.New()
	locA := uuid.New()
	ip := netip.MustParseAddr("203.0.113.5")
	base := tap.Input{
		Now:      time.Now().UTC(),
		Channel:  tap.ChannelNFC,
		SUN:      tap.SUNResult{Valid: true},
		Tag:      &tap.Tag{Status: tap.TagActive, Location: locA},
		Employee: &tap.Employee{Status: tap.EmployeeActive},
		// Tenant A's own network: the evidence is perfect.
		SourceIP:    ip,
		LocationIPs: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")},
		GPSRadiusM:  150,
		PolicySet:   baselineSetForTest(t),
	}

	fed := base
	fed.TagTenantID, fed.SessionTenantID = &tenantA, &tenantB
	if got := tap.Decide(fed); got.Redirect == tap.RedirectNone || got.MatchedSid != "sys:tenant-mismatch" {
		t.Fatalf("with both tenants fed: verdict=%q sid=%q redirect=%q, want a sys:tenant-mismatch redirect",
			got.Verdict, got.MatchedSid, got.Redirect)
	}

	// THE MUTATION: remove the feed, which is the pre-M5-05 state.
	starved := base
	got := tap.Decide(starved)
	if got.Verdict != tap.VerdictOK {
		t.Fatalf("without the tenant feed the cross-tenant tap produced %q; this test is not measuring the hole "+
			"it claims to (it should be a plain `ok`)", got.Verdict)
	}
}

// countTransactionsIn counts rows in ANOTHER tenant, under that tenant's own
// context, so RLS is doing the scoping.
func countTransactionsIn(t *testing.T, h *tapHarness, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	err := h.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM transactions WHERE tenant_id = $1`, tenantID).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count in tenant %s: %v", tenantID, err)
	}
	return n
}

// ---------------------------------------------------------------------------
// The record's own integrity.
// ---------------------------------------------------------------------------

// TestCheckinDB_PolicyContextCarriesADistanceAndNoCoordinate is §4.7 on the
// frozen decision snapshot: it must hold a DISTANCE IN METRES and never the raw
// position. A GPS fix is sent from far away so a distance is definitely computed.
func TestCheckinDB_PolicyContextCarriesADistanceAndNoCoordinate(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)

	// Valletta-ish, several kilometres from the fixture venue.
	const lat, lng = "35.8989", "14.5146"
	w := h.postFrom(t, h.nfcContext(uint32(h.startCtr)+1), cookie, "198.51.100.9:5000",
		url.Values{"lat": {lat}, "lng": {lng}})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	rec := h.lastRecord(t, h.employeeID)
	var snapshot map[string]any
	if err := json.Unmarshal(rec.PolicyContext, &snapshot); err != nil {
		t.Fatalf("policy_context is not readable json: %v", err)
	}
	dist, ok := snapshot["tap:gpsDistanceM"].(float64)
	if !ok {
		t.Fatalf("policy_context has no tap:gpsDistanceM: %s", rec.PolicyContext)
	}
	if dist < 100 {
		t.Fatalf("distance = %v m, expected a real separation", dist)
	}
	// The coordinate itself must not be in there in ANY shape.
	raw := string(rec.PolicyContext)
	for _, forbidden := range []string{lat, lng, "35.898", "14.514", "lat", "lng", "coordinate"} {
		if strings.Contains(raw, forbidden) {
			t.Fatalf("policy_context leaked a coordinate (%q): %s", forbidden, raw)
		}
	}
	// The keys the guardrails read must all be PRESENT: a missing key never
	// matches, which is how a guardrail goes silently dead (M3-04's invariant).
	for _, key := range []string{
		"tap:channel", "tap:sunValid", "tap:ipMatch", "tap:gpsMatch",
		"tap:ctrGap", "tap:occurredAtSkewSeconds", "tap:queued", "tap:pageAgeSeconds",
		"tag:status", "employee:status", "location:id",
	} {
		if _, ok := snapshot[key]; !ok {
			t.Fatalf("policy_context is missing %q — a guardrail or baseline reading it would silently not fire: %s",
				key, raw)
		}
	}
}

// TestCheckinDB_PracticeThenDirectionTogglesAgainstTheLastOpenCheckIn walks a
// person's first three taps, which is the shortest chain that shows BOTH §5 rules
// about the direction column at once.
//
//	tap 1  the PRACTICE tap: the first record after activation, TRAINING, and it
//	       must NOT hold the chain open — a training tap that counted as an open
//	       check-in would make the next real tap a check-OUT and quietly inflate
//	       hours (the M4-06 exploit).
//	tap 2  the real check-IN. It is an `in` precisely BECAUSE tap 1 does not
//	       count — an earlier draft of this test asserted `out` here and was
//	       measuring the practice rule without knowing it.
//	tap 3  the check-OUT, toggling on tap 2.
//
// 🔴 THE SHAPE OF THIS TEST CHANGED IN ADR 0006, AND THE OLD SHAPE IS WHY.
// It used to fire all three taps as real POSTs inside one second, each declaring
// a backdated occurred_at (-300 s, -150 s, none), and the comment here said the
// times were "chosen between two thresholds": past the person-debounce, inside
// the 72 h occurred-at ceiling. That worked only because the debounce compared
// against a CLIENT-DECLARED timestamp — the hole ADR 0006 closes. The debounce
// now also measures against transactions.created_at, so three real POSTs one
// second apart are three taps one second apart — whatever they declare, and
// whether they arrive one after another or all at once (ADR 0006 layers 1-4) —
// and the second and third are `ignored` exactly as §5 row 5 intends.
//
// So each step now gets its OWN employee, whose HISTORY is seeded aged (both
// stamps moved together, the way a row written minutes ago actually looks) and
// whose MEASURED tap is still a real POST through the real router and the real
// decision engine. Nothing about what is asserted was weakened; what changed is
// that the past is written rather than declared.
func TestCheckinDB_PracticeThenDirectionTogglesAgainstTheLastOpenCheckIn(t *testing.T) {
	h := newTapHarness(t)

	// tap 1 — the PRACTICE tap. No history at all, which is what makes it
	// practice, and also what keeps it clear of the debounce.
	first := h.newEmployee(t, "active") // activated_at is set, so this is practice
	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), h.cookieForEmployee(t, first), nil); w.Code != http.StatusOK {
		t.Fatalf("practice tap status = %d", w.Code)
	}
	one := h.lastRecord(t, first)
	if !one.Practice {
		t.Fatalf("the first record after activation is not marked practice (verdict %q)", one.Verdict)
	}
	if one.Type == nil || *one.Type != "in" {
		t.Fatalf("practice direction = %v, want in", deref(one.Type))
	}

	// tap 2 — the real check-IN, arriving on top of a practice tap. THE HEADLINE:
	// it is an `in`, not an `out`, because a training record must not hold the
	// chain open (the M4-06 hours-inflation exploit). And it is not practice,
	// because the run was already spent.
	second := h.newEmployee(t, "active")
	h.seedTapAgedBy(t, second, 300*time.Second, "in", true) // an aged PRACTICE tap
	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+2), h.cookieForEmployee(t, second), nil); w.Code != http.StatusOK {
		t.Fatalf("check-in status = %d", w.Code)
	}
	two := h.lastRecord(t, second)
	if two.Practice {
		t.Fatal("the SECOND tap was marked practice: only the first record after activation is training")
	}
	if two.Type == nil || *two.Type != "in" {
		t.Fatalf("check-in direction = %q, want in — a practice tap must not hold the chain open (§5)",
			deref(two.Type))
	}

	// tap 3 — the check-OUT, toggling on an ordinary open check-in.
	third := h.newEmployee(t, "active")
	h.seedTapAgedBy(t, third, 300*time.Second, "in", false)
	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+3), h.cookieForEmployee(t, third), nil); w.Code != http.StatusOK {
		t.Fatalf("check-out status = %d", w.Code)
	}
	three := h.lastRecord(t, third)
	if three.Type == nil || *three.Type != "out" {
		t.Fatalf("check-out direction = %q, want out (toggle on the open check-in)", deref(three.Type))
	}
}

// deref renders a nullable text column for an error message. Printing the
// pointer instead is how a failure says "want out, got 0x14000123456".
func deref(s *string) string {
	if s == nil {
		return "<none>"
	}
	return *s
}

// TestCheckinDB_OccurredAtBoundIsAGuardrailThatCannotBeSwitchedOff is trap K1:
// occurred_at is the ONE unverified client input and the most important field on
// the record. A FUTURE stamp is denied outright, and a stamp older than the
// tolerance is denied too — as a RECORDED reject, not a refusal (§4.6).
func TestCheckinDB_OccurredAtBoundIsAGuardrailThatCannotBeSwitchedOff(t *testing.T) {
	cases := []struct {
		name string
		when time.Duration
	}{
		{"an hour in the future", time.Hour},
		{"four days in the past (beyond the 72h ceiling)", -96 * time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newTapHarness(t)
			emp := h.newEmployee(t, "active")
			cookie := h.cookieForEmployee(t, emp)
			before := h.countFor(t, emp)

			when := time.Now().UTC().Add(tc.when).Format(time.RFC3339)
			w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, url.Values{occurredAtField: {when}})
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (a denied tap is RECORDED, not refused)", w.Code)
			}
			if after := h.countFor(t, emp); after != before+1 {
				t.Fatalf("rows went %d -> %d, want +1", before, after)
			}
			rec := h.lastRecord(t, emp)
			if rec.Verdict != "reject" || rec.MatchedSid == nil || *rec.MatchedSid != "sys:occurred-at-bound" {
				t.Fatalf("verdict/sid = %q/%v, want reject/sys:occurred-at-bound", rec.Verdict, deref(rec.MatchedSid))
			}
		})
	}
}

// TestCheckinDB_QueuedTapWithinToleranceIsFlaggedNotDenied is the other side of
// the same coin: a tap that really was queued offline, inside the guardrail's
// ceiling, is RECORDED and sent to review by base:queued-window — a tenant-tunable
// baseline, not a hard refusal.
func TestCheckinDB_QueuedTapWithinToleranceIsFlaggedNotDenied(t *testing.T) {
	h := newTapHarness(t)
	emp := h.newEmployee(t, "active")
	cookie := h.cookieForEmployee(t, emp)

	when := time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339)
	w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, url.Values{occurredAtField: {when}})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	rec := h.lastRecord(t, emp)
	if rec.Verdict != "flag" {
		t.Fatalf("verdict = %q, want flag (sid %v): a queued tap inside the ceiling is reviewed, not denied",
			rec.Verdict, deref(rec.MatchedSid))
	}
	var snapshot map[string]any
	if err := json.Unmarshal(rec.PolicyContext, &snapshot); err != nil {
		t.Fatalf("policy_context: %v", err)
	}
	if q, _ := snapshot["tap:queued"].(bool); !q {
		t.Fatalf("tap:queued = false for a client-declared past time: it is DERIVED from the skew, not declared: %s",
			rec.PolicyContext)
	}
	if skew, _ := snapshot["tap:occurredAtSkewSeconds"].(float64); skew < 1700 {
		t.Fatalf("skew = %v s, want ~1800", skew)
	}
}

// TestCheckinDB_NothingSecretReachesTheResponse. §4.7 on the wire: the page a
// person lands on after tapping carries no session token, no CMAC, no key, no
// invite code and no coordinate.
//
// POSITIVE CONTROL INCLUDED: the same body IS expected to contain the venue name
// and the verdict, so a test that simply found an empty body would fail.
func TestCheckinDB_NothingSecretReachesTheResponse(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)

	const lat, lng = "35.9181234", "14.4891234"
	w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, url.Values{"lat": {lat}, "lng": {lng}})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	body := w.Body.String()

	for _, forbidden := range []string{
		cookie.Value, // the raw session token
		lat, lng,     // the exact position
		"35.9181", "14.4891",
		tapFakeTagKey, // the per-tag AES key
		tapFakeKEK,    // the KEK
		h.tenantID.String(),
	} {
		if forbidden != "" && strings.Contains(body, forbidden) {
			t.Fatalf("the confirmation page leaked %q", forbidden)
		}
	}
	// Positive control: the page really did render the tap.
	if !strings.Contains(body, "APPROVED") || !strings.Contains(body, "St Julians") {
		t.Fatalf("the confirmation page did not render the tap at all: %s", body)
	}
	// And no button: the next action is a new physical touch (§9).
	if strings.Contains(body, "<button") || strings.Contains(body, "<form") {
		t.Fatalf("the confirmation screen grew a button: %s", body)
	}
	if !strings.Contains(body, "All done") {
		t.Fatal("the confirmation screen does not say the tap is done")
	}
}

// TestCheckinDB_UnknownPlaqueLeavesATrailAndNoRecord. A signed context naming a
// uid that no longer resolves cannot become an attendance record — there is no
// tag and no location to attribute it to — but the tenant IS known, so unlike the
// anonymous case the attempt leaves a mark in audit_log. The sentinel is not
// swallowed (the M2 hand-off).
func TestCheckinDB_UnknownPlaqueLeavesATrailAndNoRecord(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)
	before := h.transactionCount(t)
	trailBefore := h.auditCount(t, "tap.unknown_tag")

	ghost := make([]byte, 7)
	if _, err := rand.Read(ghost); err != nil {
		t.Fatalf("rand: %v", err)
	}
	w := h.postTap(t, tapContext{
		UID: strings.ToUpper(hex.EncodeToString(ghost)), Ctr: 1, Channel: sun.ChannelNFC,
		CMACVerified: true, TagTenantID: h.tenantID, LocationID: h.locationID,
	}, cookie, nil)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if after := h.transactionCount(t); after != before {
		t.Fatalf("rows went %d -> %d for a uid that resolves to nothing", before, after)
	}
	if got := h.auditCount(t, "tap.unknown_tag") - trailBefore; got != 1 {
		t.Fatalf("audit rows = %d, want 1: sun.ErrUnknownTag must not be swallowed", got)
	}
}

// TestCheckinDB_ManualChannelRefusesAMissingAuthor is the M4-06 hand-off: a
// `manual` record must name the manager who entered it, and a missing author is
// an ERROR rather than a silently accepted row (§7). It exercises the service
// directly because nothing can reach the HTTP endpoint with channel=manual today
// — the manual entry screen is M6-04, and this is the rule waiting for it.
func TestCheckinDB_ManualChannelRefusesAMissingAuthor(t *testing.T) {
	h := newTapHarness(t)
	svc := h.checkinService(t)
	before := h.transactionCount(t)

	_, err := svc.Record(context.Background(), checkinRequestForTest(h, tap.ChannelManual, nil))
	if err == nil {
		t.Fatal("a manual record with no author was accepted")
	}
	if after := h.transactionCount(t); after != before {
		t.Fatalf("rows went %d -> %d: the refused manual record was written anyway", before, after)
	}

	// POSITIVE CONTROL: with an author it is accepted, so the refusal above is
	// about entered_by and not about the manual channel being unsupported.
	author := uuid.New()
	if _, err := svc.Record(context.Background(), checkinRequestForTest(h, tap.ChannelManual, &author)); err != nil {
		t.Fatalf("a manual record WITH an author was refused: %v", err)
	}
	if after := h.transactionCount(t); after != before+1 {
		t.Fatalf("rows went %d -> %d, want +1 for the authored manual record", before, after)
	}
}

// --- helpers for the two tests that bypass HTTP ------------------------------

// checkinService is the harness's REAL orchestrator — the same instance the
// router serves POST /api/checkin with, so a test that calls it directly is
// still measuring production's wiring and not a second one built for the test
// (the M5-04 lesson: a test that configures the thing it audits audits nothing).
func (h *tapHarness) checkinService(t *testing.T) *checkin.Service {
	t.Helper()
	if h.checkins == nil {
		t.Fatal("the harness has no checkin service")
	}
	return h.checkins
}

// checkinRequestForTest is a well-formed request for the harness's fixtures,
// with the channel and author under the test's control. It is the ONLY way to
// reach the manual channel, which no HTTP route produces today (M6-04 owns the
// screen that will).
func checkinRequestForTest(h *tapHarness, channel tap.Channel, enteredBy *uuid.UUID) checkin.Request {
	return checkin.Request{
		SessionTenantID: h.tenantID,
		EmployeeID:      h.employeeID,
		TagUID:          h.tagUID,
		Channel:         channel,
		TagTenantID:     h.tenantID,
		SourceIP:        netip.MustParseAddr("203.0.113.5"),
		EnteredBy:       enteredBy,
	}
}

// baselineSetForTest is guardrails + the shipped baseline, with throwaway version
// ids. It is for the PURE mutation test only: nothing it decides is written, so
// the ids never have to resolve — which is precisely the difference between an
// in-memory evaluation and the write path that made this whole task necessary.
func baselineSetForTest(t *testing.T) policy.Set {
	t.Helper()
	ids := map[string]uuid.UUID{}
	return policy.Set{
		Guardrails: policy.DefaultGuardrails(),
		Baseline: policy.BaselinePolicies(func(name string) uuid.UUID {
			if id, ok := ids[name]; ok {
				return id
			}
			id := uuid.New()
			ids[name] = id
			return id
		}),
	}
}

// TestCheckinDB_ConfiguredDebounceWindowReachesTheGuardrail is hand-off N3 with
// teeth.
//
// 🔑 WHY IT EXISTS AT ALL: the wiring was already there and an audit still found
// it unfalsifiable. `params.DebounceWindow = cfg.Debounce` could be DELETED with
// the whole suite staying green, because the harness configured 60 s and
// policy.DefaultParams() hard-codes 60 s — the criterion could not fail. That is
// the M5-04 lesson in a second costume: there, a test configured the object it
// audited; here, a test configured a value indistinguishable from the fallback.
//
// The harness now runs at 120 s, and this drives the band BETWEEN the two: a tap
// 90 seconds after the previous one is `ignored` under the configured window and
// would be a perfectly good `out` under the shipped one. Delete the wiring line
// and this test goes red.
func TestCheckinDB_ConfiguredDebounceWindowReachesTheGuardrail(t *testing.T) {
	if harnessDebounce == 120*time.Second {
		// A guard on the guard: if somebody ever sets the harness back to the
		// shipped default, this test silently stops measuring anything. It is
		// cheaper to fail loudly here than to discover it in another audit.
		if policy.DefaultParams().DebounceWindow == harnessDebounce {
			t.Fatal("the harness debounce equals policy.DefaultParams(): this test cannot fail")
		}
	}
	h := newTapHarness(t)
	emp := h.newEmployee(t, "active")
	cookie := h.cookieForEmployee(t, emp)

	// A first tap, declared 90 s ago. 90 is chosen to sit strictly between the
	// shipped 60 s and the configured 120 s, and inside base:queued-window's 120 s
	// review threshold so the first tap is an ordinary `ok`.
	first := time.Now().UTC().Add(-90 * time.Second).Format(time.RFC3339)
	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie,
		url.Values{occurredAtField: {first}}); w.Code != http.StatusOK {
		t.Fatalf("first tap status = %d", w.Code)
	}
	if rec := h.lastRecord(t, emp); rec.Verdict == "ignored" {
		t.Fatalf("the FIRST tap was debounced (sid %q)", deref(rec.MatchedSid))
	}
	before := h.countFor(t, emp)

	// The second tap, live: the gap to the first is ~90 s.
	w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+2), cookie, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("second tap status = %d", w.Code)
	}
	if after := h.countFor(t, emp); after != before+1 {
		t.Fatalf("rows went %d -> %d, want +1: an ignored duplicate is still RECORDED", before, after)
	}
	rec := h.lastRecord(t, emp)
	if rec.Verdict != "ignored" || deref(rec.MatchedSid) != "sys:person-debounce" {
		t.Fatalf("verdict/sid = %q/%q at a 90 s gap, want ignored/sys:person-debounce — the guardrail is "+
			"comparing against the shipped 60 s default, not the configured %s (hand-off N3)",
			rec.Verdict, deref(rec.MatchedSid), harnessDebounce)
	}
}

// TestCheckinDB_ForeignTenantTapNeverTouchesTheOtherTenantsCounter is F1: a
// session for one organisation must not move another organisation's chip counter.
//
// MEASURED BEFORE THE FIX (by an audit, and reproduced here as a regression net):
// the foreign plaque's last_ctr went 900 -> 901 while BOTH tenants ended with zero
// transactions and zero audit rows — one tenant silently changing another's state
// with no trace on either side. The cause was order: the advance ran before the
// decision, inside WithTenant(tag's tenant), i.e. under the OTHER tenant's RLS
// context.
//
// The harm was narrow (the counter can only reach a value the chip really emitted,
// so it costs a physical touch) but real: it SPENDS that read out from under the
// owning tenant, so their employee holding an already-open page for the same read
// posts and is told their tap is a replay.
func TestCheckinDB_ForeignTenantTapNeverTouchesTheOtherTenantsCounter(t *testing.T) {
	h := newTapHarness(t)
	otherTenant, otherLocation := uuid.New(), uuid.New()
	kek, err := hex.DecodeString(tapFakeKEK)
	if err != nil {
		t.Fatalf("kek: %v", err)
	}
	const foreignStart = 900
	foreignUID := h.newTag(t, kek, otherTenant, otherLocation, foreignStart)
	cookie := h.sessionCookieFor(t) // a session in h.tenantID

	w := h.postTap(t, tapContext{
		UID: foreignUID, Ctr: foreignStart + 1, Channel: sun.ChannelNFC, CMACVerified: true,
		TagTenantID: otherTenant, LocationID: otherLocation,
	}, cookie, nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}

	if got := h.lastCtr(t, foreignUID); got != foreignStart {
		t.Fatalf("the FOREIGN tenant's last_ctr moved %d -> %d: a cross-tenant request changed another "+
			"organisation's state, and left no trace in either tenant (§4.5)", foreignStart, got)
	}

	// POSITIVE CONTROL: the counter is not simply frozen. The SAME plaque, tapped
	// by a session that really belongs to that tenant, advances normally — so the
	// assertion above is about the tenant check and not about the advance being
	// broken outright.
	otherEmployee := uuid.New()
	if err := h.data.WithTenant(context.Background(), otherTenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at)
			 VALUES ($1, $2, $3, 'Rita Zammit', 'active', now())`,
			otherEmployee, otherTenant, otherLocation)
		return e
	}); err != nil {
		t.Fatalf("fixture employee in the other tenant: %v", err)
	}
	issued, err := h.sessions.Issue(context.Background(), session.IssueParams{
		TenantID: otherTenant, EmployeeID: otherEmployee, DeviceInfo: "test",
	})
	if err != nil {
		t.Fatalf("session.Issue: %v", err)
	}
	rec := httptest.NewRecorder()
	if err := h.cookies.Set(rec, issued.Token); err != nil {
		t.Fatalf("cookies.Set: %v", err)
	}
	var ownCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == session.CookieName {
			ownCookie = c
		}
	}
	if w := h.postTap(t, tapContext{
		UID: foreignUID, Ctr: foreignStart + 1, Channel: sun.ChannelNFC, CMACVerified: true,
		TagTenantID: otherTenant, LocationID: otherLocation,
	}, ownCookie, nil); w.Code != http.StatusOK {
		t.Fatalf("positive control: status = %d, want 200", w.Code)
	}
	if got := h.lastCtr(t, foreignUID); got != foreignStart+1 {
		t.Fatalf("positive control: last_ctr = %d, want %d — the tenant's OWN tap must advance it",
			got, foreignStart+1)
	}
}

// TestCheckinDB_PageAgeIsCoveredByTheContextTTLToday MEASURES the boundary F2 is
// really about, instead of asserting it.
//
// Two mechanisms bound the age of a tap page and they produce DIFFERENT kinds of
// answer:
//
//	the signed context's TTL   15 minutes, fixed, refuses at parse time ->
//	                           an UNRECORDED 400 ("tap again")
//	sys:tap-freshness          a bounded guardrail param (60..900 s) -> a
//	                           RECORDED reject (§4.6 wants the record)
//
// With the SHIPPED defaults the two thresholds are the SAME number (tapContextTTL
// == FreshnessMaxSeconds == 900 s), so the guardrail's band is empty and every
// over-age page is answered by the TTL. That is measured below by driving the
// clock across the boundary, and it is why feeding tap:pageAgeSeconds changes no
// behaviour TODAY while making the guardrail reachable the moment M5-10 lets a
// tenant narrow the window (1-15 min, default 3) — which is the whole point:
// inside that narrower band the answer becomes a record instead of an error page.
func TestCheckinDB_PageAgeIsCoveredByTheContextTTLToday(t *testing.T) {
	h := newTapHarness(t)
	emp := h.newEmployee(t, "active")
	cookie := h.cookieForEmployee(t, emp)
	sid := h.sessionIDOf(t, cookie)

	minted := time.Now().UTC()
	post := func(age time.Duration, ctr uint32) *httptest.ResponseRecorder {
		// Mint at `minted`, present it `age` later. Both halves go through the
		// production codec; only the clock moves.
		h.tap.contexts.now = func() time.Time { return minted }
		signed, err := h.tap.contexts.mint(h.nfcContext(ctr), sid)
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		h.tap.contexts.now = func() time.Time { return minted.Add(age) }
		defer func() { h.tap.contexts.now = nil }()

		req := httptest.NewRequest(http.MethodPost, "/api/checkin",
			strings.NewReader(url.Values{"ctx": {signed}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.RemoteAddr = "203.0.113.5:41234"
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.router.ServeHTTP(w, req)
		return w
	}

	before := h.countFor(t, emp)
	if w := post(14*time.Minute+59*time.Second, uint32(h.startCtr)+1); w.Code != http.StatusOK {
		t.Fatalf("a page just inside the TTL: status = %d, want 200", w.Code)
	}
	if after := h.countFor(t, emp); after != before+1 {
		t.Fatalf("rows went %d -> %d, want +1 for the in-TTL tap", before, after)
	}

	before = h.countFor(t, emp)
	w := post(15*time.Minute+1*time.Second, uint32(h.startCtr)+2)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a page past the TTL: status = %d, want 400", w.Code)
	}
	if after := h.countFor(t, emp); after != before {
		t.Fatalf("rows went %d -> %d: the TTL refusal is UNRECORDED, which is exactly the difference "+
			"from the guardrail's recorded reject", before, after)
	}
	if got := h.lastCtr(t, h.tagUID); got != h.startCtr+1 {
		t.Fatalf("last_ctr = %d: a context refused at parse time must not reach the counter", got)
	}
}

// TestCheckinDB_TheZeroInstantIsADeclarationNotSilence is the F-round finding
// about a sentinel that a caller can land on by writing a valid timestamp.
//
// MEASURED BEFORE THE FIX, one second apart, opposite outcomes:
//
//	occurred_at=0001-01-01T00:00:00Z  ->  ok / base:ip-or-gps-ok, and the row
//	                                      stored the SERVER's clock
//	occurred_at=0001-01-01T00:00:01Z  ->  reject / sys:occurred-at-bound, row
//	                                      stored 0001-01-01
//
// The harm ran toward the attacker and the record was still written, so nothing
// was lost — but the handler claimed "a declared time is never silently stamped
// with another one" and for exactly this value that was false. The guard is an
// explicit nil now, which no form field can contain, so both values are judged
// the same way.
func TestCheckinDB_TheZeroInstantIsADeclarationNotSilence(t *testing.T) {
	h := newTapHarness(t)
	for i, raw := range []string{"0001-01-01T00:00:00Z", "0001-01-01T00:00:01Z", "0001-01-01T01:00:00+01:00"} {
		t.Run(raw, func(t *testing.T) {
			emp := h.newEmployee(t, "active")
			cookie := h.cookieForEmployee(t, emp)
			before := h.countFor(t, emp)

			w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1+uint32(i)), cookie,
				url.Values{occurredAtField: {raw}})
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (a denied tap is RECORDED)", w.Code)
			}
			if after := h.countFor(t, emp); after != before+1 {
				t.Fatalf("rows went %d -> %d, want +1", before, after)
			}
			rec := h.lastRecord(t, emp)
			if rec.Verdict != "reject" || deref(rec.MatchedSid) != "sys:occurred-at-bound" {
				t.Fatalf("verdict/sid = %q/%q, want reject/sys:occurred-at-bound — a declared time landed on "+
					"the 'nothing was declared' guard", rec.Verdict, deref(rec.MatchedSid))
			}
			if rec.OccurredAt.Year() != 1 {
				t.Fatalf("stored occurred_at = %v, want the DECLARED year 1: the server stamped its own clock "+
					"over a time the caller stated", rec.OccurredAt)
			}
		})
	}

	// POSITIVE CONTROL: with the field absent, the server really does time it, and
	// the tap is an ordinary one — so the assertions above are about the sentinel
	// and not about year-1 timestamps being special.
	emp := h.newEmployee(t, "active")
	cookie := h.cookieForEmployee(t, emp)
	if w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+9), cookie, nil); w.Code != http.StatusOK {
		t.Fatalf("control status = %d", w.Code)
	}
	rec := h.lastRecord(t, emp)
	if rec.Verdict == "reject" {
		t.Fatalf("an ordinary live tap was rejected (sid %q)", deref(rec.MatchedSid))
	}
	if rec.OccurredAt.Year() < 2000 {
		t.Fatalf("stored occurred_at = %v, want the server clock", rec.OccurredAt)
	}
}

// TestCheckinDB_BrandMessageComesFromTheTenantRow closes the gap the unit tests
// cannot see (M5-06).
//
// 🔴 IT PINS THE WHOLE CHAIN, and that is the point. The screen tests in
// result_test.go drive the real router and the real template but hand the domain
// a checkin.Result they built themselves — so every one of them would stay green
// if db/queries/employees.sql stopped selecting business_type, if gather() stopped
// copying it, or if Result stopped carrying it. That is exactly the shape this
// session keeps paying for: a guarantee proven in one package and merely assumed
// in the next. Here the value starts as a column in `tenants`, on real Postgres,
// and is asserted as a sentence in the HTML.
//
// The tenant is UPDATEd rather than the fixture re-parameterised because
// newTapHarness commits the row before a test can reach it; each harness makes a
// fresh random tenant, so nothing else sees the change.
func TestCheckinDB_BrandMessageComesFromTheTenantRow(t *testing.T) {
	tests := []struct {
		name         string
		businessType string
		want         string
		wantNot      string
	}{
		{"restaurant gets the kitchen line", "restaurant",
			"Have a great shift — keep those kebabs rolling! 🌯", "stay safe on the floor"},
		{"production gets the factory line", "production",
			"Have a productive shift — stay safe on the floor! 🏭", "kebabs rolling"},
		{"any other type gets the neutral line", "hotel",
			"Have a great shift!", "kebabs rolling"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTapHarness(t)
			h.setBusinessType(t, tc.businessType)
			cookie := h.sessionCookieFor(t)

			w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil)
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", w.Code)
			}
			// The record must actually be an `ok` — a flag or an ignored renders no
			// brand line at all, and "the sentence is missing" would then be true for
			// the wrong reason.
			if rec := h.lastRecord(t, h.employeeID); rec.Verdict != "ok" {
				t.Fatalf("verdict = %q, want ok: this row cannot test the send-off", rec.Verdict)
			}
			body := w.Body.String()
			if !strings.Contains(body, tc.want) {
				t.Fatalf("business_type %q did not produce %q", tc.businessType, tc.want)
			}
			if strings.Contains(body, tc.wantNot) {
				t.Fatalf("business_type %q also produced %q", tc.businessType, tc.wantNot)
			}
			// The category chooses a sentence and is never printed (§4.7 adjacent:
			// the screen shows a verdict, a venue and a clock).
			if strings.Contains(body, tc.businessType) {
				t.Fatalf("the business type %q was printed on the screen", tc.businessType)
			}
		})
	}
}

// setBusinessType rewrites the harness tenant's category. UPDATE is granted on
// `tenants` (migration 00001) — unlike transactions, which is immutable by
// REVOKE (§4.3) and which no test may rewrite.
func (h *tapHarness) setBusinessType(t *testing.T, businessType string) {
	t.Helper()
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		tag, e := tx.Exec(ctx, `UPDATE tenants SET business_type = $2 WHERE id = $1`, h.tenantID, businessType)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			t.Fatalf("UPDATE tenants affected %d rows, want 1", tag.RowsAffected())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("setting business_type: %v", err)
	}
}

// TestCheckinDB_ScreenTextIsWhatProductionRenders closes the gap that made the
// first whole-screen test worthless (M5-06, round 4).
//
// 🔴 THE EXPECTED SCREEN MUST COME FROM PRODUCTION, NOT FROM A KEYBOARD. An
// earlier version of the whitelist was assembled by hand with an empty
// Decision.Note — a shape the product never renders, because Note is the deciding
// rule's Reason and EVERY guardrail, baseline rule and the no-match fallback
// carries one. The consequence was measured: the real ignored <main> was 1061
// bytes against a 971-byte expectation, the note paragraph was outside what the
// test looked at, and a "Your earlier tap stands." hidden inside that paragraph
// passed the whole suite. The repo's own lesson, restated in this file's terms: a
// verification probe is built from the PRODUCT'S REAL INPUT, never from the
// minimum the tool will accept.
//
// So this test drives four verdicts through the real engine, on real Postgres,
// and asserts the SAME strings the unit table asserts. If a policy reason is
// reworded, this fails first and names the new wording, which is the correct
// order: the employee-facing screen changed, and a test says so.
//
// The wall clock is the one thing normalised away — it is `now`.
func TestCheckinDB_ScreenTextIsWhatProductionRenders(t *testing.T) {
	clockRE := regexp.MustCompile(`\d{2}:\d{2}:\d{2}`)
	// The SAME constant the unit table uses, not a second copy: two shell strings
	// drift, and the point of this test is that both layers assert one screen.
	shell := shellText

	t.Run("ok on the venue network", func(t *testing.T) {
		h := newTapHarness(t)
		w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), h.sessionCookieFor(t), nil)
		got := clockRE.ReplaceAllString(screenText(t, w.Body.String()), "<clock>")
		want := shell + " St Julians Tapped in <clock> APPROVED Trust 70 " + noteIPMatched +
			" Have a great shift — keep those kebabs rolling! 🌯 All done — you can close this page."
		if got != want {
			t.Fatalf("production ok screen:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("flag off the network with no fix", func(t *testing.T) {
		h := newTapHarness(t)
		w := h.postFrom(t, h.nfcContext(uint32(h.startCtr)+1), h.sessionCookieFor(t), "198.51.100.9:5000", nil)
		got := clockRE.ReplaceAllString(screenText(t, w.Body.String()), "<clock>")
		want := shell + " St Julians Tapped in <clock> FLAGGED Trust 20 " + noteNoPlace +
			" Recorded — it needs your manager's approval before it counts toward your hours." +
			" Tell them if that looks wrong. You can close this page."
		if got != want {
			t.Fatalf("production flag screen — this is the §4.6 branch:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("reject on a retired plaque", func(t *testing.T) {
		h := newTapHarness(t)
		h.retireTag(t, h.tagUID, "retired")
		w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), h.sessionCookieFor(t), nil)
		got := clockRE.ReplaceAllString(screenText(t, w.Body.String()), "<clock>")
		want := shell + " St Julians Not counted <clock> REJECTED Trust 70 " + noteDeadTag +
			" This tap was recorded but not counted. Tell your manager if that looks wrong." +
			" You can close this page."
		if got != want {
			t.Fatalf("production reject screen — the row EXISTS, the screen must not deny it:\n got: %s\nwant: %s",
				got, want)
		}
	})

	t.Run("ok out closing a stale check-in: the two-part note", func(t *testing.T) {
		// THE FIFTH NOTE CONSTANT, driven from production. tap.Decide's appendNote
		// joins the deciding rule's reason to the direction annotation with "; ", and
		// until this case existed noteStaleOpenIn was a hand-typed copy of
		// internal/domain/tap's staleOpenInNote bound to nothing: rewording the
		// engine's string left every test green while the screen table went on
		// pinning a sentence production no longer emitted.
		h := newTapHarness(t)
		h.openCheckInAgedBy(t, 19*time.Hour)
		w := h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), h.sessionCookieFor(t), nil)
		rec := h.lastRecord(t, h.employeeID)
		if rec.Verdict != "ok" || rec.Type == nil || *rec.Type != "out" {
			t.Fatalf("verdict/type = %q/%v, want ok/out: this case is not testing the stale-open-in note",
				rec.Verdict, rec.Type)
		}
		got := clockRE.ReplaceAllString(screenText(t, w.Body.String()), "<clock>")
		want := shell + " St Julians Tapped out <clock> APPROVED Trust 70 " +
			noteIPMatched + "; " + noteStaleOpenIn +
			" Great work today. See you next shift! 👋 All done — you can close this page."
		if got != want {
			t.Fatalf("production stale-check-out screen:\n got: %s\nwant: %s", got, want)
		}
	})

	t.Run("ignored: the debounced duplicate", func(t *testing.T) {
		h := newTapHarness(t)
		cookie := h.sessionCookieFor(t)
		if w := h.postTap(t, h.qrContext(), cookie, nil); w.Code != http.StatusOK {
			t.Fatalf("first tap status = %d", w.Code)
		}
		w := h.postTap(t, h.qrContext(), cookie, nil)
		if rec := h.lastRecord(t, h.employeeID); rec.Verdict != "ignored" {
			t.Fatalf("verdict = %q, want ignored: this case is not testing the debounce screen", rec.Verdict)
		}
		got := clockRE.ReplaceAllString(screenText(t, w.Body.String()), "<clock>")
		want := shell + " St Julians Already tapped <clock> IGNORED Trust 70 " + noteDebounce +
			" You tapped a moment ago, so this one was not counted. Tell your manager if that looks" +
			" wrong. You can close this page."
		if got != want {
			t.Fatalf("production ignored screen — the predecessor's verdict is UNKNOWN here:\n got: %s\nwant: %s",
				got, want)
		}
	})
}

// openCheckInAgedBy commits an OPEN check-in that occurred `age` ago, so the next
// real tap resolves to a check-OUT against it.
//
// It INSERTs directly rather than tapping, and that is the only way to reach the
// shape: staleOpenInThreshold is 18 hours (internal/domain/tap), and a test cannot
// wait. The row is otherwise an ordinary decided tap — type 'in', verdict 'ok' —
// so GetLastOpenTransaction finds it exactly as it would find a real one.
func (h *tapHarness) openCheckInAgedBy(t *testing.T, age time.Duration) {
	t.Helper()
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		// created_at IS AGED TOO, and that is not decoration (ADR 0006). The
		// debounce also measures the SERVER-side age of this person's last recorded
		// tap (ADR 0006), so a row
		// that CLAIMS to be 19 hours old while having been WRITTEN a millisecond
		// ago is a shape production cannot produce — and seeding it would debounce
		// the very tap this fixture exists to enable. Both stamps move together,
		// which is what a row written 19 hours ago actually looks like.
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, type,
			                           occurred_at, created_at, verdict, channel, sun_valid, trust)
			 VALUES ($1, $2, $3, $4, 'in', now() - $5::interval, now() - $5::interval, 'ok', 'nfc', true, 100)`,
			h.tenantID, h.employeeID, h.locationID, h.tagUID, age.String())
		return e
	})
	if err != nil {
		t.Fatalf("seeding an open check-in: %v", err)
	}
}

// seedTapAgedBy commits a decided tap for one employee, aged by `age` in BOTH
// timestamps, and returns nothing because callers only need it to exist.
//
// 🔴 WHY THIS EXISTS AT ALL, AND WHAT IT REPLACED (ADR 0006). Several tests used
// to simulate a multi-tap day inside one second by declaring a backdated
// occurred_at on each POST — `tapAgo(900)`, `at(-300s)` — and the comments said
// so plainly ("it still clears the 120 s person-debounce, because tap one
// declared itself 600 s ago"). That worked only because the debounce compared
// against a CLIENT-DECLARED timestamp, which is the hole ADR 0006 closes. With
// the second leg in place, a sequence of real POSTs by one person cannot be
// separated in time without actually waiting — so a fixture that needs an OLD
// predecessor has to write one, with both stamps moved together, which is what a
// row written `age` ago genuinely looks like.
//
// It is a SETUP tool. Every assertion about how production DECIDES a tap is still
// made against a real POST; this only supplies the history such a tap arrives on
// top of.
func (h *tapHarness) seedTapAgedBy(t *testing.T, employeeID uuid.UUID, age time.Duration, direction string, practice bool) {
	t.Helper()
	h.seedAgedRecord(t, employeeID, age, &direction, "ok", practice)
}

// seedAgedRecord is seedTapAgedBy with the verdict and a nullable direction
// spelled out, for the cases that need a REJECT in the history (a reject carries
// no direction, which is itself part of what a caller may be measuring).
func (h *tapHarness) seedAgedRecord(t *testing.T, employeeID uuid.UUID, age time.Duration, direction *string, verdict string, practice bool) {
	t.Helper()
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, type,
			                           occurred_at, created_at, verdict, channel, sun_valid, trust, practice)
			 VALUES ($1, $2, $3, $4, $5, now() - $6::interval, now() - $6::interval, $7, 'nfc', true, 100, $8)`,
			h.tenantID, employeeID, h.locationID, h.tagUID, direction, age.String(), verdict, practice)
		return e
	})
	if err != nil {
		t.Fatalf("seeding an aged record: %v", err)
	}
}

// seedRecordWithSplitClocks commits a record whose DECLARED and WRITTEN times are
// deliberately far apart — occurred_at aged by `occurredAgo`, created_at by
// `createdAgo` — on a chosen channel.
//
// It is what makes ADR 0006's manual exemption testable. That exemption lives in
// SecondsSinceLastRecordedTap's `channel IN ('nfc','qr')` predicate, and the shape it
// protects is exactly this one: a row DECLARING yesterday that was WRITTEN a
// moment ago. Production makes that shape two ways — a manager typing a forgotten
// shift (channel manual, exempt) and a queued tap syncing late (channel nfc, not
// exempt) — and no HTTP flow can produce the manual one yet (M6-04).
func (h *tapHarness) seedRecordWithSplitClocks(t *testing.T, employeeID uuid.UUID, occurredAgo, createdAgo time.Duration, channel, direction string) {
	t.Helper()
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		enteredBy := &employeeID // a manual row must name who entered it
		if channel != "manual" {
			enteredBy = nil
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, type,
			                           occurred_at, created_at, verdict, channel, trust, entered_by)
			 VALUES ($1, $2, $3, $4, $5, now() - $6::interval, now() - $7::interval, 'ok', $8, 100, $9)`,
			h.tenantID, employeeID, h.locationID, h.tagUID, direction,
			occurredAgo.String(), createdAgo.String(), channel, enteredBy)
		return e
	})
	if err != nil {
		t.Fatalf("seeding a split-clock record: %v", err)
	}
}

// verdictBreakdown counts one employee's rows by verdict, plus two synthetic
// keys: __directional (rows carrying a type) and __practice.
func (h *tapHarness) verdictBreakdown(t *testing.T, employeeID uuid.UUID) map[string]int {
	t.Helper()
	out := map[string]int{}
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx,
			`SELECT verdict, type IS NOT NULL, practice FROM transactions
			 WHERE tenant_id = $1 AND employee_id = $2`, h.tenantID, employeeID)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var v string
			var directional, practice bool
			if e := rows.Scan(&v, &directional, &practice); e != nil {
				return e
			}
			out[v]++
			if directional {
				out["__directional"]++
			}
			if practice {
				out["__practice"]++
			}
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("verdict breakdown: %v", err)
	}
	return out
}
