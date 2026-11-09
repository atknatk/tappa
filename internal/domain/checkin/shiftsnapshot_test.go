package checkin

import (
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
)

// TestInsertParams_RecordsTheShiftOnlyOnADirectedTap is 00095's first safety (ADR 0042), at the
// mapping itself: the shift the engine was handed is copied onto a DIRECTED nfc or qr decision —
// with its source, or 'none' when there was no shift — and nothing is written for an undirected
// decision, for the manual channel, or for a source the CHECK does not admit, EVEN WHEN the facts
// carry a shift. Each of those would be refused by transactions_shift_snapshot_shape with 23514
// and lose the record (§4.6); on a manual row it would also be a claim about the past nobody made.
func TestInsertParams_RecordsTheShiftOnlyOnADirectedTap(t *testing.T) {
	at := time.Date(2026, 10, 20, 7, 0, 0, 0, time.UTC)
	in, out := tap.TypeIn, tap.TypeOut
	night := &tap.Shift{Start: 18 * time.Hour, End: 2 * time.Hour, Overnight: true, Timezone: "Europe/Malta"}
	type want struct {
		source    string // "" = NULL
		start     time.Duration
		end       time.Duration
		overnight bool
		zone      string
	}
	tests := []struct {
		name    string
		channel tap.Channel
		dir     *tap.Type
		shift   *tap.Shift
		source  tap.ShiftSource
		want    want
	}{
		{"a directed nfc check-in with the venue's shift", tap.ChannelNFC, &in, night, tap.ShiftSourceLocation,
			want{"location", 18 * time.Hour, 2 * time.Hour, true, "Europe/Malta"}},
		{"a directed qr checkout with the department's shift", tap.ChannelQR, &out, night, tap.ShiftSourceDepartment,
			want{"department", 18 * time.Hour, 2 * time.Hour, true, "Europe/Malta"}},
		{"a directed tap with no shift is 'none'", tap.ChannelNFC, &in, nil, "", want{source: "none"}},
		{"a directed tap with no shift ignores a stale source", tap.ChannelQR, &out, nil, tap.ShiftSourceDepartment, want{source: "none"}},
		{"an undirected nfc decision records nothing", tap.ChannelNFC, nil, night, tap.ShiftSourceLocation, want{}},
		{"an undirected qr decision records nothing", tap.ChannelQR, nil, nil, "", want{}},
		{"a directed manual record records nothing", tap.ChannelManual, &in, night, tap.ShiftSourceLocation, want{}},
		{"a shift with a source the CHECK refuses records nothing", tap.ChannelNFC, &in, night, "venue", want{}},
		{"a shift with no source records nothing", tap.ChannelNFC, &in, night, "", want{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dec := tap.Decision{Verdict: tap.VerdictOK, Type: tc.dir, Trust: 20}
			if tc.dir == nil {
				dec.Verdict = tap.VerdictReject
			}
			f := tapFacts{shift: tc.shift, shiftSource: tc.source}
			p, err := (&Service{}).insertParams(Request{Channel: tc.channel}, db.ResolvedTag{UID: "04AABBCCDDEEFF"}, f, dec, at)
			if err != nil {
				t.Fatal(err)
			}
			got := want{}
			if p.ShiftSource != nil {
				got.source = *p.ShiftSource
			}
			if p.ShiftStart.Valid {
				got.start = time.Duration(p.ShiftStart.Microseconds) * time.Microsecond
			}
			if p.ShiftEnd.Valid {
				got.end = time.Duration(p.ShiftEnd.Microseconds) * time.Microsecond
			}
			if p.ShiftOvernight != nil {
				got.overnight = *p.ShiftOvernight
			}
			if p.ShiftTimezone != nil {
				got.zone = *p.ShiftTimezone
			}
			if got != tc.want {
				t.Fatalf("shift columns %+v, want %+v", got, tc.want)
			}
			// The half-filled shapes the CHECK refuses: a value without its source, or a source
			// whose values are not all there.
			full := p.ShiftStart.Valid && p.ShiftEnd.Valid && p.ShiftOvernight != nil && p.ShiftTimezone != nil
			empty := !p.ShiftStart.Valid && !p.ShiftEnd.Valid && p.ShiftOvernight == nil && p.ShiftTimezone == nil
			switch {
			case p.ShiftSource == nil && !empty, p.ShiftSource != nil && *p.ShiftSource == "none" && !empty,
				p.ShiftSource != nil && *p.ShiftSource != "none" && !full:
				t.Fatalf("a shape transactions_shift_snapshot_shape refuses: %+v", p)
			}
		})
	}
}
