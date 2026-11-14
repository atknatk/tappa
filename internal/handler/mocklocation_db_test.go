package handler

// mocklocation_db_test.go -- M16-06 (ADR 0045 §2.4): the app's mock-location report and the
// server-derived client reach the RECORD through the real write path -- the decision, the sid and
// the frozen policy_context -- and a browser tap, which carries no report, records no
// tap:mockLocation key and is decided as before. The app's own endpoint is M16-07; these go to the
// service directly, the way the manual-channel test above does.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
)

func TestCheckinDB_TheMockLocationReportReachesTheRecord(t *testing.T) {
	h := newTapHarness(t)
	svc := h.checkinService(t)
	yes, no := true, false
	tests := []struct {
		name     string
		client   tap.Client
		mock     *bool
		verdict  string
		sid      string
		wantMock any // nil = the key is absent
	}{
		{"the app reports a mock position", tap.ClientApp, &yes, "flag", policy.SidMockLocationReview, true},
		{"the app reports a real position", tap.ClientApp, &no, "ok", policy.SidIPOrGPSOK, false},
		{"the browser, which cannot report", tap.ClientBrowser, nil, "ok", policy.SidIPOrGPSOK, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			person := h.newEmployee(t, "active")
			req := checkinRequestForTest(h, tap.ChannelQR, nil) // the venue's network: an IP match
			req.EmployeeID = person
			req.Client = tc.client
			req.MockLocation = tc.mock
			if _, err := svc.Record(context.Background(), req); err != nil {
				t.Fatalf("Record: %v", err)
			}
			rec := h.lastRecord(t, person)
			if rec.Verdict != tc.verdict || deref(rec.MatchedSid) != tc.sid {
				t.Fatalf("verdict/sid = %s/%s, want %s/%s", rec.Verdict, deref(rec.MatchedSid), tc.verdict, tc.sid)
			}
			if rec.Trust == nil || *rec.Trust != 70 {
				t.Fatalf("trust = %v, want 70 (20 + 50 for the IP; the report is not in the formula)", rec.Trust)
			}
			var ctx map[string]any
			if err := json.Unmarshal(rec.PolicyContext, &ctx); err != nil {
				t.Fatalf("policy_context: %v", err)
			}
			if got, ok := ctx[string(policy.CtxTapMockLocation)]; (tc.wantMock == nil) == ok || (ok && got != tc.wantMock) {
				t.Fatalf("policy_context tap:mockLocation = %v (present %v), want %v", got, ok, tc.wantMock)
			}
			if got := ctx[string(policy.CtxTapClient)]; got != string(tc.client) {
				t.Fatalf("policy_context tap:client = %v, want %q -- the same value the client column records", got, tc.client)
			}
			if c := h.clientOf(t, rec.ID); c == nil || *c != string(tc.client) {
				t.Fatalf("client column = %v, want %q", clientText(c), tc.client)
			}
		})
	}
}
