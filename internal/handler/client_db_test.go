package handler

// client_db_test.go -- M16-04 (migration 00109, ADR 0045 §2.4): transactions.client is written by
// the Go write path, not by the schema. 00109's CHECKs admit NULL on purpose (a "tap must carry a
// client" CHECK would refuse the previous binary's taps during the rollout and lose them, §4.6), so
// the proof that a new tap row carries its client is here:
//   - the tap page (POST /api/checkin, through the real router) writes 'browser' on every row it
//     records -- ok, flag, reject and ignored, nfc and qr -- because the client does not depend on
//     the decision;
//   - nothing in the request can choose it: a form field naming 'app' changes nothing;
//   - the service forces NULL on the manual channel even when a caller names a client, and records
//     NULL for the zero value (the API sandbox's simulated tap).
// The manual ENTRY screen's statement (InsertManualTransaction) is manualentry_db_test.go's
// TestManualDB_WritesExactlyTheColumnsItMayAndNULLsTheREST.

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/tap"
)

// clientOf reads transactions.client of one row back, as the tenant (RLS on).
func (h *tapHarness) clientOf(t *testing.T, id uuid.UUID) *string {
	t.Helper()
	var c *string
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT client FROM transactions WHERE tenant_id = $1 AND id = $2`, h.tenantID, id).Scan(&c)
	})
	if err != nil {
		t.Fatalf("read back the client: %v", err)
	}
	return c
}

// clientText is a client column for a message: its value, or NULL.
func clientText(c *string) string {
	if c == nil {
		return "NULL"
	}
	return "'" + *c + "'"
}

// TestCheckinDB_TheTapPageRecordsTheBrowserOnEveryVerdict: every row POST /api/checkin records
// carries 'browser' -- a reject, an ok and an ignored repeat on nfc, and a flagged qr tap.
func TestCheckinDB_TheTapPageRecordsTheBrowserOnEveryVerdict(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)

	unverified := h.nfcContext(uint32(h.startCtr) + 3)
	unverified.CMACVerified = false
	steps := []struct {
		name    string
		post    func() int
		verdict string
	}{
		// The order matters: the ok opens the person's 60 s window, the repeat lands inside it, and
		// the unverified MAC is §5 row 2 -- decided before the debounce, so a reject inside the window.
		{"an nfc ok", func() int { return h.postTap(t, h.nfcContext(uint32(h.startCtr)+1), cookie, nil).Code }, "ok"},
		{"an ignored repeat within the window", func() int { return h.postTap(t, h.nfcContext(uint32(h.startCtr)+2), cookie, nil).Code }, "ignored"},
		{"an nfc reject (the MAC did not verify)", func() int { return h.postTap(t, unverified, cookie, nil).Code }, "reject"},
	}
	for _, s := range steps {
		before := h.countFor(t, h.employeeID)
		if code := s.post(); code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", s.name, code)
		}
		if after := h.countFor(t, h.employeeID); after != before+1 {
			t.Fatalf("%s: rows went %d -> %d, want +1", s.name, before, after)
		}
		rec := h.lastRecord(t, h.employeeID)
		if rec.Verdict != s.verdict {
			t.Fatalf("%s: verdict = %q (sid %q), want %q -- the fixture moved", s.name, rec.Verdict, deref(rec.MatchedSid), s.verdict)
		}
		if c := h.clientOf(t, rec.ID); c == nil || *c != string(tap.ClientBrowser) {
			t.Errorf("%s: client = %v, want 'browser' -- POST /api/checkin is the tap page", s.name, clientText(c))
		}
	}

	t.Run("a flagged qr tap from off the venue", func(t *testing.T) {
		h := newTapHarness(t)
		cookie := h.sessionCookieFor(t)
		if code := h.postFrom(t, h.qrContext(), cookie, "198.51.100.9:5000", nil).Code; code != http.StatusOK {
			t.Fatalf("status = %d, want 200", code)
		}
		rec := h.lastRecord(t, h.employeeID)
		if rec.Channel != "qr" || rec.Verdict != "flag" {
			t.Fatalf("channel/verdict = %s/%s, want qr/flag -- the fixture moved", rec.Channel, rec.Verdict)
		}
		if c := h.clientOf(t, rec.ID); c == nil || *c != string(tap.ClientBrowser) {
			t.Errorf("client = %v, want 'browser'", clientText(c))
		}
	})
}

// TestCheckinDB_TheClientIsNotReadFromTheRequest: the client is the endpoint's, so a body field
// that names the app changes nothing -- the row says 'browser' (ADR 0045 §2.4: no body, no header,
// no user agent; the handler reads no header for it either -- its Request literal names a constant).
func TestCheckinDB_TheClientIsNotReadFromTheRequest(t *testing.T) {
	h := newTapHarness(t)
	cookie := h.sessionCookieFor(t)
	sid := h.sessionIDOf(t, cookie)
	signed, err := h.tap.contexts.mint(h.nfcContext(uint32(h.startCtr)+1), sid)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	w := h.postSigned(t, signed, cookie, "203.0.113.5:41234", url.Values{"client": {"app"}})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	rec := h.lastRecord(t, h.employeeID)
	if c := h.clientOf(t, rec.ID); c == nil || *c != string(tap.ClientBrowser) {
		t.Errorf("with client=app in the body the row says %v, want 'browser'", clientText(c))
	}
}

// TestCheckinDB_TheServiceWritesNoClientOnAManualRecordOrAZeroClient goes to the service directly
// (the manual channel has no HTTP route into Record): a manual record carries NULL even when the
// caller names a client -- a manager's record is not a tap -- and a tap with the zero Client records
// NULL ("no client", the sandbox's case) rather than a guess. Each row is also written, so the NULL
// is the write path's answer and not a refused INSERT.
func TestCheckinDB_TheServiceWritesNoClientOnAManualRecordOrAZeroClient(t *testing.T) {
	h := newTapHarness(t)
	svc := h.checkinService(t)
	author := uuid.New()
	tests := []struct {
		name    string
		channel tap.Channel
		by      *uuid.UUID
		client  tap.Client
		want    *string
	}{
		{"manual, a client named", tap.ChannelManual, &author, tap.ClientBrowser, nil},
		{"manual, the app named", tap.ChannelManual, &author, tap.ClientApp, nil},
		{"qr, the zero client", tap.ChannelQR, nil, "", nil},
		{"qr, a client outside the vocabulary", tap.ChannelQR, nil, tap.Client("ios"), nil},
		{"qr, the app", tap.ChannelQR, nil, tap.ClientApp, strptr(string(tap.ClientApp))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A fresh person per case: the previous case's row would otherwise make this one an
			// ignored repeat, which is still recorded -- but the case should not lean on it.
			person := h.newEmployee(t, "active")
			req := checkinRequestForTest(h, tc.channel, tc.by)
			req.EmployeeID = person
			req.Client = tc.client
			res, err := svc.Record(context.Background(), req)
			if err != nil {
				t.Fatalf("Record: %v", err)
			}
			if res.TransactionID == uuid.Nil {
				t.Fatalf("no row was written (outcome %s)", res.Outcome)
			}
			got := h.clientOf(t, res.TransactionID)
			if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
				t.Errorf("client = %v, want %v", clientText(got), clientText(tc.want))
			}
		})
	}
}
