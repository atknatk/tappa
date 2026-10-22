package checkin

import (
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
)

// TestInsertParams_FreezesTheBusinessDayOnlyOnADirectedTap is ADR 0032 §A.5's first safety, at
// the mapping itself: business_day_start is the decision's S for a DIRECTED nfc or qr decision
// and NULL — forced in code — for an undirected one and for the manual channel, EVEN WHEN the
// decision carries an S. Decide does not hand one to an undirected decision today; this line
// must not rely on it, because the column's CHECK (… IS NULL OR type IS NOT NULL) would refuse
// the INSERT with 23514 and lose the record (§4.6).
func TestInsertParams_FreezesTheBusinessDayOnlyOnADirectedTap(t *testing.T) {
	s := time.Date(2026, 10, 20, 3, 0, 0, 0, time.UTC)
	in := tap.TypeIn
	tests := []struct {
		name    string
		channel tap.Channel
		dir     *tap.Type
		want    bool
	}{
		{"a directed nfc tap freezes it", tap.ChannelNFC, &in, true},
		{"a directed qr tap freezes it", tap.ChannelQR, &in, true},
		{"an undirected nfc decision is NULL", tap.ChannelNFC, nil, false},
		{"an undirected qr decision is NULL", tap.ChannelQR, nil, false},
		{"a directed manual record is NULL", tap.ChannelManual, &in, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dec := tap.Decision{Verdict: tap.VerdictOK, Type: tc.dir, BusinessDayStart: &s, Trust: 20}
			if tc.dir == nil {
				dec.Verdict = tap.VerdictReject
			}
			p, err := (&Service{}).insertParams(Request{Channel: tc.channel}, db.ResolvedTag{UID: "04AABBCCDDEEFF"}, tapFacts{}, dec, s)
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case tc.want && (p.BusinessDayStart == nil || !p.BusinessDayStart.Equal(s)):
				t.Fatalf("business_day_start = %v, want %v", p.BusinessDayStart, s)
			case !tc.want && p.BusinessDayStart != nil:
				t.Fatalf("business_day_start = %v, want NULL", *p.BusinessDayStart)
			}
		})
	}
}

// TestInsertParams_WritesNoDirectionSourceBeforeTheEngineChooses is M15-09's writer decision
// (ADR 0032 §B.4): until M15-10 gives the decision a source and M15-11 maps it, the tap path
// writes direction_source NULL on EVERY row -- directed or not, nfc, qr or manual, ok or flag --
// so the column carries no claim the engine did not make, and 00065's Down stays free until the
// first real choice is recorded. On a directed nfc/qr row the NULL reads as the engine's choice
// (tap.RecordedDirectionSource), which is what every tap this binary decides is.
func TestInsertParams_WritesNoDirectionSourceBeforeTheEngineChooses(t *testing.T) {
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	in, out := tap.TypeIn, tap.TypeOut
	tests := []struct {
		name    string
		channel tap.Channel
		verdict tap.Verdict
		dir     *tap.Type
	}{
		{"a directed nfc ok", tap.ChannelNFC, tap.VerdictOK, &in},
		{"a directed nfc flag", tap.ChannelNFC, tap.VerdictFlag, &out},
		{"a directed qr flag", tap.ChannelQR, tap.VerdictFlag, &in},
		{"an undirected nfc reject", tap.ChannelNFC, tap.VerdictReject, nil},
		{"an undirected qr ignored", tap.ChannelQR, tap.VerdictIgnored, nil},
		{"a directed manual record", tap.ChannelManual, tap.VerdictOK, &out},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dec := tap.Decision{Verdict: tc.verdict, Type: tc.dir, Trust: 20}
			p, err := (&Service{}).insertParams(Request{Channel: tc.channel}, db.ResolvedTag{UID: "04AABBCCDDEEFF"}, tapFacts{}, dec, at)
			if err != nil {
				t.Fatal(err)
			}
			if p.DirectionSource != nil {
				t.Fatalf("direction_source = %q, want NULL until M15-11", *p.DirectionSource)
			}
		})
	}
}
