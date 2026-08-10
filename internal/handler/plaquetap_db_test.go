package handler

// A TAP ON A PLAQUE THAT IS NOT IN SERVICE, END TO END, OVER HTTP, AGAINST REAL
// POSTGRES (M6-06 phase B).
//
// 🔴 WHY THIS FILE EXISTS: §5 ROW 1 WAS PINNED EVERYWHERE EXCEPT ON THE WIRE. The
// guardrail is table-tested in internal/policy, the decision is table-tested in
// internal/domain/tap, and checkin_db_test.go drives a retired plaque through the
// SERVICE — but nothing drove one through GET /t and POST /api/checkin with a
// plaque the panel can actually produce. That gap is the shape the M5-05 audit
// named: a guarantee proven in package A, consumed in package B, with nothing
// pinning that B reaches it.
//
// 🔴 AND THE FOURTH STATUS MADE THE GAP CONCRETE. Migration 00013 added
// `unassigned` — a plaque Tappa encoded and loaded that nobody has mounted — and
// the guardrail was a DENY-LIST at the time (`retired || lost`), so a tap on a
// plaque still in its box matched NO guardrail. The data-layer round measured the
// chain and inverted the guardrail to an allow-list; what it could not do was prove
// the inversion holds THROUGH THE HTTP PATH, on BOTH channels. That is this file.
//
// 🔴 THE QR HALF IS THE ONE THAT WAS ACTUALLY OPEN. sys:sun-invalid is gated on
// channel = nfc, so a QR-shaped tap skipped it entirely: an employee who read the
// uid off a plaque in a box could produce, from anywhere, `flag` rows a manager is
// invited to approve. NFC was closed, but by the wrong rule and with the wrong
// sentence — internal/sun/preview.go short-circuits on a non-active status and
// reports CMACValid=false, which used to be attributed to a SIGNATURE failure.
//
// WHAT EACH TEST BELOW REQUIRES, so a reader can see why the assertions are what
// they are:
//
//	the row IS WRITTEN (§4.6)            a refusal that leaves no trace is the one
//	                                     outcome this product may never produce
//	verdict = reject                     §5 row 1
//	matched_sid = sys:tag-not-active     the rule that fired is about the PLAQUE'S
//	                                     STATE, not about a signature
//	the note says so in words            it is what the employee reads
//
// THE CMAC IN THESE URLS IS DELIBERATELY INVALID, which makes the assertion
// STRONGER rather than weaker and is the whole reason the positive control below
// exists. On an active plaque that same URL fires sys:sun-invalid (measured, in
// the control); on a plaque that is not in service, sys:tag-not-active fires FIRST
// because it is guardrail #2 and sun-invalid is #3. So the pair measures the ORDER,
// which is the property the inversion depends on. (Minting a valid CMAC here would
// mean reimplementing the SDM derivation outside internal/sun — the second-copy
// failure that package spends two files avoiding; tap_db_test.go states the same.)
//
// Fixtures are NOT cleaned up: tappa_app has REVOKE DELETE on tags and
// transactions. Fresh random ids per run keep them from colliding.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/sun"
)

// loadedPlaque inserts a plaque the way TAPPA'S LOADER would: a real KEK-wrapped
// key, this tenant, and the caller's choice of status and wall.
//
// 🔴 IT IS AN INSERT AND THE PANEL HAS NO SUCH QUERY, which is the inventory model
// stated as a fixture: db/queries/tags.sql ships no INSERT over `tags` because
// creating a plaque means holding its key. This helper stands in for the M8-05
// runbook, not for anything the panel can do.
//
// wall == uuid.Nil produces a STOCK plaque: location_id NULL, status
// 'unassigned'. The two travel together because 00013's CHECKs bind them —
// tags_unassigned_has_no_location and tags_active_requires_location — so a fixture
// cannot accidentally build the state the schema forbids.
func (h *tapHarness) loadedPlaque(t *testing.T, status string, wall uuid.UUID) string {
	t.Helper()
	uidBytes := make([]byte, 7)
	if _, err := rand.Read(uidBytes); err != nil {
		t.Fatalf("rand: %v", err)
	}
	uid := strings.ToUpper(hex.EncodeToString(uidBytes))
	tagKey, err := hex.DecodeString(tapFakeTagKey)
	if err != nil {
		t.Fatalf("tag key: %v", err)
	}
	ref, err := sun.Wrap(h.cfg.TagKEK, uidBytes, tagKey)
	if err != nil {
		t.Fatalf("sun.Wrap: %v", err)
	}
	var location *uuid.UUID
	if wall != uuid.Nil {
		w := wall
		location = &w
	}
	err = h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status)
			 VALUES ($1, $2, $3, $4, 0, $5)`,
			uid, h.tenantID, location, ref, status)
		return e
	})
	if err != nil {
		t.Fatalf("load plaque (%s): %v", status, err)
	}
	return uid
}

// tapOnce drives the whole path for one plaque on one channel and returns the row
// the product wrote. It starts at GET /t so the SERVER'S OWN minted context is what
// the POST carries — the seam qr_db_test.go established is the only honest one.
func (h *tapHarness) tapOnce(t *testing.T, uid string, nfc bool, cookie *http.Cookie) recordedTap {
	t.Helper()
	target := "/t?tag=" + uid
	if nfc {
		target += "&ctr=000701&cmac=83c0104d8ac39077"
	}
	before := h.countFor(t, h.employeeID)
	signed := h.signedContextFrom(t, target, cookie)
	w := h.postSigned(t, signed, cookie, onSiteAddr, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("POST after %s: status = %d, want 200", target, w.Code)
	}
	if after := h.countFor(t, h.employeeID); after != before+1 {
		t.Fatalf("rows went %d -> %d for %s: §4.6 requires the refused attempt to be WRITTEN",
			before, after, target)
	}
	return h.lastRecord(t, h.employeeID)
}

// TestPlaqueTapDB_APlaqueNotInServiceIsRefusedOnBOTHChannelsAndTheAttemptIsRecorded
// is §5 row 1 over HTTP, for all three statuses the panel and the loader can
// produce, on both channels.
//
// 🔴 THE unassigned ROWS ARE THE POINT. The other two were closed before 00013;
// `unassigned` is the value that escaped a deny-list, and QR is the channel that
// had nothing else standing in its way.
func TestPlaqueTapDB_APlaqueNotInServiceIsRefusedOnBOTHChannelsAndTheAttemptIsRecorded(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)

	tests := []struct {
		name   string
		status string
		wall   uuid.UUID
	}{
		// The inventory state: encoded, loaded, never mounted. location_id NULL.
		{name: "unassigned (still in its box)", status: "unassigned", wall: uuid.Nil},
		// Replaced. It keeps its wall — tags_retired_keeps_its_location (00013) —
		// because the audit chain needs to answer "which door did it serve".
		{name: "retired (replaced)", status: "retired", wall: h.locationID},
		// Reported lost. Same refusal, plus a security alert the decision layer
		// raises; this file asserts the refusal, ADR 0007's family table the alert.
		{name: "lost", status: "lost", wall: h.locationID},
	}

	for _, tc := range tests {
		for _, nfc := range []bool{true, false} {
			channel := "qr"
			if nfc {
				channel = "nfc"
			}
			t.Run(tc.name+"/"+channel, func(t *testing.T) {
				uid := h.loadedPlaque(t, tc.status, tc.wall)
				rec := h.tapOnce(t, uid, nfc, cookie)

				if rec.Verdict != "reject" {
					t.Fatalf("verdict = %q, want reject (§5 row 1)", rec.Verdict)
				}
				if rec.MatchedSid == nil || *rec.MatchedSid != "sys:tag-not-active" {
					// 🔴 THE SID IS THE WHOLE ASSERTION AND "it was rejected" IS NOT ENOUGH.
					// A tap can be rejected for four different reasons and only one of them
					// is true here; the deny-list bug produced a `flag`, and a wrong-reason
					// reject would produce the sentence the data-layer round called out.
					t.Fatalf("matched_sid = %v, want sys:tag-not-active — the rule that fired "+
						"must be about the plaque's STATE, not about a signature", rec.MatchedSid)
				}
				if rec.Channel != channel {
					t.Fatalf("channel = %q, want %q (the server derives it, never the client)",
						rec.Channel, channel)
				}
				if rec.TagUID == nil || *rec.TagUID != uid {
					t.Fatalf("tag_uid = %v, want %s — the record must name the plaque", rec.TagUID, uid)
				}
				if rec.Note == nil || !strings.Contains(*rec.Note, "not in service") {
					// The sentence the employee reads. It used to say "no longer active",
					// which is false of a plaque that has never been active a day in its
					// life — M6-06 phase B's obligation 1.
					t.Fatalf("note = %v, want the state named in words", rec.Note)
				}
				if rec.Note != nil && strings.Contains(*rec.Note, "signature") {
					t.Fatalf("note = %q blames the signature; nothing is wrong with the "+
						"signature, the plaque is not in service", *rec.Note)
				}
			})
		}
	}
}

// TestPlaqueTapDB_TheSameURLOnAnActivePlaqueBlamesTheSignature is the POSITIVE
// CONTROL for the file, and without it every assertion above is unfalsifiable.
//
// 🔴 IT IS THE SAME INVALID CMAC AND A DIFFERENT PLAQUE STATE. If sys:tag-not-active
// were firing for some other reason — or if every reject in this harness carried
// that sid — this test would fail. It shows the two guardrails are genuinely
// distinguishable on the wire and that the ORDER is what decides which one a
// non-active plaque meets.
func TestPlaqueTapDB_TheSameURLOnAnActivePlaqueBlamesTheSignature(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)

	uid := h.loadedPlaque(t, "active", h.locationID)
	rec := h.tapOnce(t, uid, true, cookie)

	if rec.Verdict != "reject" {
		t.Fatalf("verdict = %q, want reject (a forged CMAC on an active plaque)", rec.Verdict)
	}
	if rec.MatchedSid == nil || *rec.MatchedSid != "sys:sun-invalid" {
		t.Fatalf("matched_sid = %v, want sys:sun-invalid — an ACTIVE plaque with a bad "+
			"CMAC must be refused for the signature, which is what makes the "+
			"not-in-service assertions above mean anything", rec.MatchedSid)
	}
}

// TestPlaqueTapDB_AStockPlaqueNeverMovesItsCounter. The atomic advance (§4.4) is
// reached only after a verified CMAC on an ACTIVE plaque, so a plaque in a box must
// leave its counter exactly where the loader put it — otherwise reading a uid off a
// box would be a way to burn counter values a real plaque will need.
func TestPlaqueTapDB_AStockPlaqueNeverMovesItsCounter(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)
	uid := h.loadedPlaque(t, "unassigned", uuid.Nil)

	for i := 0; i < 3; i++ {
		h.tapOnce(t, uid, true, cookie)
	}
	if got := h.lastCtr(t, uid); got != 0 {
		t.Fatalf("last_ctr = %d after three taps on a plaque in its box, want 0 unchanged", got)
	}
}
