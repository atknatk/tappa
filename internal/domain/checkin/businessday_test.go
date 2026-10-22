package checkin

import (
	"fmt"
	"net/netip"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
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

// TestInsertParams_WritesWhoChoseTheDirectionOnlyOnADirectedTap is the mapping of
// direction_source (ADR 0032 §B.4, M15-11), the same rule as the business day's above: the
// decision's source on a DIRECTED nfc or qr decision, and NULL -- forced in code -- for an
// undirected one, for the manual channel and for a source the column does not admit, EVEN
// WHEN the decision carries one. A value there would be refused by 00065's CHECKs with 23514
// (the record lost, §4.6) or, on a manual row, a claim nobody made. Result.DirectionSource is
// the same function's answer, so the screen and the row cannot disagree.
func TestInsertParams_WritesWhoChoseTheDirectionOnlyOnADirectedTap(t *testing.T) {
	at := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	in, out := tap.TypeIn, tap.TypeOut
	tests := []struct {
		name    string
		channel tap.Channel
		verdict tap.Verdict
		dir     *tap.Type
		source  tap.DirectionSource
		want    string // "" = NULL
	}{
		{"a directed nfc ok of the engine's", tap.ChannelNFC, tap.VerdictOK, &in, tap.DirectionAuto, "auto"},
		{"a directed nfc ok the person chose", tap.ChannelNFC, tap.VerdictOK, &out, tap.DirectionEmployee, "employee"},
		{"a directed nfc flag the person chose", tap.ChannelNFC, tap.VerdictFlag, &in, tap.DirectionEmployee, "employee"},
		{"a directed qr flag of the engine's", tap.ChannelQR, tap.VerdictFlag, &in, tap.DirectionAuto, "auto"},
		{"a directed qr ok the person chose", tap.ChannelQR, tap.VerdictOK, &out, tap.DirectionEmployee, "employee"},
		{"an undirected nfc reject is NULL", tap.ChannelNFC, tap.VerdictReject, nil, "", ""},
		{"an undirected qr ignored is NULL even with a stray source", tap.ChannelQR, tap.VerdictIgnored, nil, tap.DirectionEmployee, ""},
		{"a directed manual record is NULL", tap.ChannelManual, tap.VerdictOK, &out, tap.DirectionAuto, ""},
		{"a directed manual record is NULL with an employee source too", tap.ChannelManual, tap.VerdictOK, &out, tap.DirectionEmployee, ""},
		{"a source the column does not admit is NULL, never written", tap.ChannelNFC, tap.VerdictOK, &in, "manager", ""},
		{"a directed decision without a source is NULL", tap.ChannelNFC, tap.VerdictOK, &in, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dec := tap.Decision{Verdict: tc.verdict, Type: tc.dir, DirectionSource: tc.source, Trust: 20}
			p, err := (&Service{}).insertParams(Request{Channel: tc.channel}, db.ResolvedTag{UID: "04AABBCCDDEEFF"}, tapFacts{}, dec, at)
			if err != nil {
				t.Fatal(err)
			}
			got := ""
			if p.DirectionSource != nil {
				got = *p.DirectionSource
				if got == "" {
					t.Fatal("direction_source is an empty string, not NULL")
				}
			}
			if got != tc.want {
				t.Fatalf("direction_source = %q, want %q (\"\" = NULL)", got, tc.want)
			}
			res := ""
			if r := directionSourceColumn(tc.channel, dec); r != nil {
				res = string(*r)
			}
			if res != got {
				t.Errorf("Result.DirectionSource would say %q and the row %q", res, got)
			}
		})
	}
}

// TestInsertParams_EmployeeExactlyWhereTheRecordedDirectionIsNotTheEnginesD is the column's
// invariant (ADR 0032 §B.3; the M15-09 security lens's note 1) over the engine's real
// decisions rather than hand-written ones: every combination of channel, open check-in,
// request, evidence and plaque state is decided by tap.Decide and mapped by insertParams. On
// every directed nfc/qr row the column is NOT NULL -- a NULL there could not be told from "written
// before M15-11" -- and it is 'employee' exactly when the recorded direction differs from the
// D tap.PredictDirection gives for the same facts. Undirected and manual rows are NULL.
func TestInsertParams_EmployeeExactlyWhereTheRecordedDirectionIsNotTheEnginesD(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	wall := func(day, hh, mm int) time.Time { return time.Date(2026, 10, day, hh, mm, 0, 0, loc).UTC() }
	cutoff := &tap.DayCutoff{Cutoff: 5 * time.Hour, Timezone: "Europe/Malta"}
	venue := uuid.New()
	onNet := netip.MustParseAddr("203.0.113.7")
	set := policy.Set{Guardrails: policy.DefaultGuardrails(), Baseline: policy.BaselinePolicies(func(string) uuid.UUID { return uuid.New() })}
	// The scenes of the business day (Malta, 05:00 cutoff): D is in, out, in-leaving-open.
	opens := []struct {
		name    string
		now, at time.Time // at zero: no open check-in
	}{
		{"no open check-in", wall(20, 9, 0), time.Time{}},
		{"open three hours ago", wall(20, 9, 0), wall(20, 6, 0)},
		{"open in the previous business day", wall(20, 9, 0), wall(19, 16, 0)},
		{"open longer than StaleOpenIn in this business day", wall(20, 23, 30), wall(20, 5, 10)},
	}
	in, out, sideways := tap.TypeIn, tap.TypeOut, tap.Type("sideways")
	requests := []*tap.Type{nil, &in, &out, &sideways}
	counted := map[string]int{}
	for _, channel := range []tap.Channel{tap.ChannelNFC, tap.ChannelQR, tap.ChannelManual} {
		for _, o := range opens {
			now := o.now
			for _, req := range requests {
				for _, onSite := range []bool{true, false} {
					for _, status := range []tap.TagStatus{tap.TagActive, tap.TagLost} {
						input := tap.Input{
							Now: now, OccurredAt: now, Channel: channel,
							SUN:        tap.SUNResult{Valid: channel == tap.ChannelNFC},
							Tag:        &tap.Tag{Status: status, Location: venue},
							Employee:   &tap.Employee{Status: tap.EmployeeActive, Location: &venue, ActivatedAt: now.Add(-48 * time.Hour)},
							GPSRadiusM: 150, Debounce: 60 * time.Second, PolicySet: set,
							DayCutoff: cutoff, RequestedDirection: req,
						}
						if onSite {
							input.SourceIP, input.LocationIPs = onNet, []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")}
						}
						if !o.at.IsZero() {
							input.LastOpenIn = &tap.Transaction{ID: uuid.New(), OccurredAt: o.at, Direction: tap.TypeIn}
						}
						dec := tap.Decide(input)
						p, err := (&Service{}).insertParams(Request{Channel: channel}, db.ResolvedTag{UID: "04AABBCCDDEEFF"}, tapFacts{}, dec, now)
						if err != nil {
							t.Fatal(err)
						}
						name := fmt.Sprintf("%s/%s/request %v/on site %v/%s", channel, o.name, req, onSite, status)
						directedTap := p.Type != nil && channel != tap.ChannelManual
						if !directedTap {
							if p.DirectionSource != nil {
								t.Errorf("%s: an undirected or manual row carries direction_source %q", name, *p.DirectionSource)
							}
							counted["null"]++
							continue
						}
						if p.DirectionSource == nil {
							t.Errorf("%s: a directed %s row has a NULL direction_source", name, channel)
							continue
						}
						d := tap.PredictDirection(now, input.LastOpenIn, cutoff).Direction
						if employee := *p.DirectionSource == string(tap.DirectionEmployee); employee != (*p.Type != string(d)) {
							t.Errorf("%s: recorded %s, D %s, direction_source %q -- 'employee' must hold exactly when they differ",
								name, *p.Type, d, *p.DirectionSource)
						}
						counted[*p.DirectionSource]++
					}
				}
			}
		}
	}
	// ANTI-VACUITY: the grid reaches every arm.
	if counted["auto"] == 0 || counted["employee"] == 0 || counted["null"] == 0 {
		t.Fatalf("the grid reached auto %d, employee %d, NULL %d times; each must be reached", counted["auto"], counted["employee"], counted["null"])
	}
	t.Logf("rows: %v", counted)
}
