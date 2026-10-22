package tap

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

// This file is ADR 0032 §B.3 and §B.4 on the engine (plan M15-10): the direction the
// person pressed (Input.RequestedDirection), who chose the recorded one
// (Decision.DirectionSource), the two anchors a choice leaves for Faz C (LeftOpen with
// LeftOpenEmployeeChoice, MissingIn), and PredictDirection — the one function the
// screen and the record share. Wall clocks are Malta's in July 2026 (mt; Monday is the
// 27th, CEST = UTC+2).

// ruleIP is the sentence base:ip-or-gps-ok writes on an on-site tap.
const ruleIP = "network proof of place: the source IP matches the location"

// dirPtr is a fresh *Type.
func dirPtr(d Type) *Type { return &d }

// requestCase is one of the requests the engine can be handed: none, the two
// directions, and a value that is neither (M15-11's parser never produces one, a
// hand-built Input can).
var requestCases = []struct {
	name string
	req  *Type
}{
	{"no_request", nil},
	{"want_in", dirPtr(TypeIn)},
	{"want_out", dirPtr(TypeOut)},
	{"want_sideways", dirPtr(Type("sideways"))},
}

// TestDecide_RequestedDirectionFourCells is §B.3's rule cell by cell — the engine's D
// (in: no open check-in; out: a three-hour-old one of this business day) × the
// request — on an `ok` and on a `flag`: the request is recorded where it differs from D
// and only then is the source "employee"; a request equal to D, no request and an
// unknown value are the engine's choice. The verdict, the trust score, the evidence
// and the deciding rule are the no-request decision's in every cell.
func TestDecide_RequestedDirectionFourCells(t *testing.T) {
	t.Parallel()
	verdicts := []struct {
		name    string
		input   func() Input
		verdict Verdict
	}{
		{"ok", onSiteInput, VerdictOK},
		{"flag", baseInput, VerdictFlag}, // no IP, no GPS: §5 row 7
	}
	for _, v := range verdicts {
		for _, d := range []Type{TypeIn, TypeOut} {
			for _, rc := range requestCases {
				v, d, rc := v, d, rc
				t.Run(v.name+"_engine_"+string(d)+"_"+rc.name, func(t *testing.T) {
					t.Parallel()
					in := v.input()
					in.DayCutoff = maltaCutoff(5 * time.Hour)
					if d == TypeOut {
						in.LastOpenIn = openIn(in.Now.Add(-3 * time.Hour))
					}
					base := Decide(in)
					if base.Verdict != v.verdict || base.Type == nil || *base.Type != d || base.DirectionSource != DirectionAuto {
						t.Fatalf("precondition: %q %v %q via %q, want %q %q auto", base.Verdict, base.Type,
							base.DirectionSource, base.MatchedSid, v.verdict, d)
					}
					in.RequestedDirection = rc.req
					got := Decide(in)

					wantDir, wantSource := d, DirectionAuto
					if rc.req != nil && (*rc.req == TypeIn || *rc.req == TypeOut) && *rc.req != d {
						wantDir, wantSource = *rc.req, DirectionEmployee
					}
					if got.Type == nil || *got.Type != wantDir || got.DirectionSource != wantSource {
						t.Fatalf("Type %v source %q, want %q %q", got.Type, got.DirectionSource, wantDir, wantSource)
					}
					if got.Verdict != base.Verdict || got.Trust != base.Trust || got.IPMatch != base.IPMatch ||
						got.GPSMatch != base.GPSMatch || got.MatchedSid != base.MatchedSid || got.Layer != base.Layer ||
						got.Security != base.Security {
						t.Errorf("the request moved the decision: %q/%d/%v/%v/%q, want %q/%d/%v/%v/%q",
							got.Verdict, got.Trust, got.IPMatch, got.GPSMatch, got.MatchedSid,
							base.Verdict, base.Trust, base.IPMatch, base.GPSMatch, base.MatchedSid)
					}
					if wantSource == DirectionAuto && !reflect.DeepEqual(got, base) {
						t.Errorf("the engine's own direction must be the no-request decision exactly:\n got %+v\nwant %+v", got, base)
					}
				})
			}
		}
	}
}

// TestDecide_RequestedDirectionNoneOnUndirectedVerdicts: reject, ignored and the
// no-session redirect drop the request — no direction, no source, no business day, no
// anchor, no note — and are the no-request decision exactly.
func TestDecide_RequestedDirectionNoneOnUndirectedVerdicts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(in *Input)
	}{
		{"reject_lost_tag", func(in *Input) { in.Tag.Status = TagLost }},
		{"reject_sun_invalid", func(in *Input) { in.SUN = SUNResult{Valid: false} }},
		{"reject_deactivated", func(in *Input) { in.Employee.Status = EmployeeDeactivated }},
		{"ignored_debounce", func(in *Input) {
			in.LastForPerson = &Transaction{OccurredAt: in.Now.Add(-10 * time.Second), Direction: TypeIn}
		}},
		{"redirect_no_session", func(in *Input) { in.Employee = nil }},
	}
	for _, c := range cases {
		for _, open := range []bool{false, true} {
			for _, rc := range requestCases[1:] {
				c, open, rc := c, open, rc
				name := c.name + "_" + rc.name
				if open {
					name += "_over_an_open_check_in"
				}
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					in := onSiteInput()
					in.DayCutoff = maltaCutoff(5 * time.Hour)
					if open {
						in.LastOpenIn = openIn(in.Now.Add(-2 * time.Hour))
					}
					c.mutate(&in)
					base := Decide(in)
					in.RequestedDirection = rc.req
					got := Decide(in)
					if got.Type != nil || got.DirectionSource != "" || got.BusinessDayStart != nil ||
						got.LeftOpen != nil || got.MissingIn != "" {
						t.Fatalf("an undirected decision carries Type %v source %q day %v left %+v missing %q",
							got.Type, got.DirectionSource, got.BusinessDayStart, got.LeftOpen, got.MissingIn)
					}
					if !reflect.DeepEqual(got, base) {
						t.Errorf("the request changed an undirected decision:\n got %+v\nwant %+v", got, base)
					}
				})
			}
		}
	}
}

// TestDecide_RequestedDirectionLeavesThePolicyContextAlone: the request is no policy
// input (§5: direction is arithmetic, not policy; ADR 0032 §B.4). Over shapes that
// reach every effect, every request leaves PolicyContext — the frozen snapshot — the
// same map with the same key set, and the verdict, trust and rule the same.
func TestDecide_RequestedDirectionLeavesThePolicyContextAlone(t *testing.T) {
	t.Parallel()
	shapes := []struct {
		name  string
		input func() Input
	}{
		{"ok_nfc", onSiteInput},
		{"flag_no_evidence", baseInput},
		{"flag_qr_without_ip", func() Input { in := baseInput(); in.Channel = ChannelQR; in.SUN = SUNResult{}; return in }},
		{"ok_open_check_in", func() Input { in := onSiteInput(); in.LastOpenIn = openIn(in.Now.Add(-time.Hour)); return in }},
		{"ok_declared_instant", func() Input {
			in := onSiteInput()
			in.OccurredAt, in.OccurredAtFromClient = in.Now.Add(-30*time.Second), true
			return in
		}},
		{"reject_lost", func() Input { in := onSiteInput(); in.Tag.Status = TagLost; return in }},
		{"ignored", func() Input {
			in := onSiteInput()
			in.LastForPerson = &Transaction{OccurredAt: in.Now.Add(-5 * time.Second), Direction: TypeIn}
			return in
		}},
	}
	for _, sh := range shapes {
		sh := sh
		t.Run(sh.name, func(t *testing.T) {
			t.Parallel()
			in := sh.input()
			in.DayCutoff = maltaCutoff(5 * time.Hour)
			base := Decide(in)
			for _, rc := range requestCases[1:] {
				in.RequestedDirection = rc.req
				got := Decide(in)
				if !reflect.DeepEqual(got.PolicyContext, base.PolicyContext) {
					t.Errorf("%s: PolicyContext %v, want %v", rc.name, got.PolicyContext, base.PolicyContext)
				}
				if len(got.PolicyContext) != len(base.PolicyContext) {
					t.Errorf("%s: %d context keys, want %d", rc.name, len(got.PolicyContext), len(base.PolicyContext))
				}
				if got.Verdict != base.Verdict || got.Trust != base.Trust || got.MatchedSid != base.MatchedSid ||
					!reflect.DeepEqual(got.PolicyVersionID, base.PolicyVersionID) {
					t.Errorf("%s: %q/%d/%q, want %q/%d/%q", rc.name, got.Verdict, got.Trust, got.MatchedSid,
						base.Verdict, base.Trust, base.MatchedSid)
				}
			}
		})
	}
}

// TestDecide_RequestedDirectionSplitShift is the product lens's Ü-O2 case (the card's
// criterion): Kebab Factory 10:00–22:00, 11:00 `in`, the 15:00 checkout forgotten,
// 18:00 "Not right? Clock in". The engine's D is `out` (it would close 11:00, a
// same-business-day entry seven hours old); the record is `in`, chosen by the
// employee, and 11:00 stays open as the checkout claim's anchor for the reason
// "employee choice". The chosen `in` is a check-in, so lateness is measured: 480
// minutes against 10:00.
func TestDecide_RequestedDirectionSplitShift(t *testing.T) {
	t.Parallel()
	in := onSiteInput()
	in.Now = mt(t, 27, 18, 0)
	in.DayCutoff = maltaCutoff(5 * time.Hour)
	in.Shift = &Shift{Start: 10 * time.Hour, End: 22 * time.Hour, Timezone: "Europe/Malta"}
	in.LastOpenIn = openIn(mt(t, 27, 11, 0))
	if p := PredictDirection(in.Now, in.LastOpenIn, in.DayCutoff); p.Direction != TypeOut || p.LeftOpen != nil {
		t.Fatalf("precondition: the engine's D is %q (left %+v), want out", p.Direction, p.LeftOpen)
	}
	in.RequestedDirection = dirPtr(TypeIn)
	got := Decide(in)
	if got.Verdict != VerdictOK || got.Type == nil || *got.Type != TypeIn || got.DirectionSource != DirectionEmployee {
		t.Fatalf("got %q %v %q, want ok in employee", got.Verdict, got.Type, got.DirectionSource)
	}
	if got.LeftOpen == nil || got.LeftOpen.ID != in.LastOpenIn.ID || got.LeftOpen.Reason != LeftOpenEmployeeChoice {
		t.Fatalf("LeftOpen = %+v, want {%s %s}", got.LeftOpen, in.LastOpenIn.ID, LeftOpenEmployeeChoice)
	}
	if got.MissingIn != "" {
		t.Errorf("MissingIn = %q on an `in`", got.MissingIn)
	}
	if want := ruleIP + "; " + LeftOpenNote + "; " + DirectionChosenNote; got.Note != want {
		t.Errorf("Note = %q, want %q", got.Note, want)
	}
	if want := ruleIP + "; " + LeftOpenEmployeeNote + "; " + ChoseClockInEmployeeNote; got.EmployeeNote != want {
		t.Errorf("EmployeeNote = %q, want %q", got.EmployeeNote, want)
	}
	wantLate(t, got.MinutesLate, 8*60)
	if s := mt(t, 27, 5, 0); got.BusinessDayStart == nil || !got.BusinessDayStart.Equal(s) {
		t.Errorf("BusinessDayStart = %v, want %s", got.BusinessDayStart, s)
	}
	// Read back: the 11:00 entry's next directed event is this `in`, not a closing `out`
	// — so it is open (ADR 0032 §A.6), which is what the anchor says.
	if c := ClassifyCheckout(&Prev{Direction: TypeIn, At: mt(t, 27, 11, 0)}, true, mt(t, 27, 18, 0), got.BusinessDayStart); c != CheckoutCloses {
		t.Fatalf("control: an `out` at 18:00 would have closed 11:00 (%q) — the choice is what leaves it open", c)
	}
}

// TestDecide_RequestedDirectionChosenOutAnchorsAMissingCheckIn: a chosen `out` over the
// engine's `in` is the check-in claim's anchor exactly when the reader classifies it as
// closing nothing — (b) other business day or (c) orphan — and not when it is (a) too
// long or (d) unanchored. Each case gives the predecessor and I the way the reader sees
// the chain, so the engine's MissingIn is checked against ClassifyCheckout's own answer.
// A chosen `out` leaves nothing "left open": LeftOpen is nil and the left-open note is
// not on a checkout.
func TestDecide_RequestedDirectionChosenOutAnchorsAMissingCheckIn(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		cutoff   *DayCutoff
		open     time.Time // zero = none at the tap's instant
		now      time.Time
		prev     *Prev // the event before the out, as the reader finds it
		dayHasIn bool
		want     CheckoutClass // the reader's class
		anchor   bool
	}{
		// The break (5th version's HIGH): 09:00 in, 12:00 out, the return not tapped,
		// 17:00 "Not right? Clock out" -> (c), the 13:00-17:00 check-in is missing.
		{"break_is_an_orphan", maltaCutoff(5 * time.Hour), time.Time{}, mt(t, 27, 17, 0),
			&Prev{Direction: TypeOut, At: mt(t, 27, 12, 0)}, true, CheckoutOrphan, true},
		// Nothing before it at all.
		{"first_event_is_an_orphan", maltaCutoff(5 * time.Hour), time.Time{}, mt(t, 27, 17, 0),
			nil, false, CheckoutOrphan, true},
		// Monday 15:00 in, Tuesday 07:00 chosen out (S = Tuesday 05:00) -> (b).
		{"yesterdays_entry_is_another_day", maltaCutoff(5 * time.Hour), mt(t, 27, 15, 0), mt(t, 28, 7, 0),
			&Prev{Direction: TypeIn, At: mt(t, 27, 15, 0)}, false, CheckoutOtherDay, true},
		// 22.5 h inside one business day (00:00 cutoff) -> (a): no anchor.
		{"stale_same_day_is_too_long", maltaCutoff(0), mt(t, 28, 1, 0), mt(t, 28, 23, 30),
			&Prev{Direction: TypeIn, At: mt(t, 28, 1, 0)}, true, CheckoutTooLong, false},
		// No business day: 20 h -> (a); nothing open -> (d). Neither is an anchor.
		{"stale_without_a_day_is_too_long", nil, mt(t, 27, 13, 0), mt(t, 28, 9, 0),
			&Prev{Direction: TypeIn, At: mt(t, 27, 13, 0)}, false, CheckoutTooLong, false},
		{"no_day_is_unanchored", nil, time.Time{}, mt(t, 27, 17, 0),
			&Prev{Direction: TypeOut, At: mt(t, 27, 12, 0)}, true, CheckoutUnanchored, false},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := onSiteInput()
			in.Now = tc.now
			in.DayCutoff = tc.cutoff
			if !tc.open.IsZero() {
				in.LastOpenIn = openIn(tc.open)
			}
			if p := PredictDirection(in.Now, in.LastOpenIn, in.DayCutoff); p.Direction != TypeIn {
				t.Fatalf("precondition: the engine's D is %q, want in", p.Direction)
			}
			in.RequestedDirection = dirPtr(TypeOut)
			got := Decide(in)
			if got.Type == nil || *got.Type != TypeOut || got.DirectionSource != DirectionEmployee {
				t.Fatalf("got %v %q, want out employee", got.Type, got.DirectionSource)
			}
			if c := ClassifyCheckout(tc.prev, tc.dayHasIn, tc.now, got.BusinessDayStart); c != tc.want {
				t.Fatalf("control: the reader classifies it %q, the case says %q", c, tc.want)
			}
			wantMissing := CheckoutClass("")
			if tc.anchor {
				wantMissing = tc.want
			}
			if got.MissingIn != wantMissing {
				t.Errorf("MissingIn = %q, want %q", got.MissingIn, wantMissing)
			}
			if got.LeftOpen != nil {
				t.Errorf("LeftOpen = %+v on a checkout", got.LeftOpen)
			}
			if want := ruleIP + "; " + DirectionChosenNote; got.Note != want {
				t.Errorf("Note = %q, want %q", got.Note, want)
			}
			if want := ruleIP + "; " + ChoseClockOutEmployeeNote; got.EmployeeNote != want {
				t.Errorf("EmployeeNote = %q, want %q", got.EmployeeNote, want)
			}
			if got.MinutesLate != nil {
				t.Errorf("MinutesLate = %d on a checkout", *got.MinutesLate)
			}
		})
	}
}

// TestDecide_RequestedDirectionLatenessFollowsTheRecordedDirection: lateness is the
// RECORDED direction's (card: "geç kalma kaydedilen yöne göre"). Shift 09:00-17:00, tap
// 10:00: a chosen `in` over the engine's `out` is 60 minutes late; a chosen `out` over
// the engine's `in` is not a check-in and has none.
func TestDecide_RequestedDirectionLatenessFollowsTheRecordedDirection(t *testing.T) {
	t.Parallel()
	shift := &Shift{Start: 9 * time.Hour, End: 17 * time.Hour, Timezone: "Europe/Malta"}
	chosenIn := withShift(mt(t, 27, 10, 0), shift)
	chosenIn.DayCutoff = maltaCutoff(5 * time.Hour)
	chosenIn.LastOpenIn = openIn(mt(t, 27, 8, 0))
	chosenIn.RequestedDirection = dirPtr(TypeIn)
	got := Decide(chosenIn)
	if got.Type == nil || *got.Type != TypeIn || got.DirectionSource != DirectionEmployee {
		t.Fatalf("precondition: %v %q, want in employee", got.Type, got.DirectionSource)
	}
	wantLate(t, got.MinutesLate, 60)

	chosenOut := withShift(mt(t, 27, 10, 0), shift)
	chosenOut.DayCutoff = maltaCutoff(5 * time.Hour)
	chosenOut.RequestedDirection = dirPtr(TypeOut)
	if got := Decide(chosenOut); got.Type == nil || *got.Type != TypeOut || got.MinutesLate != nil {
		t.Fatalf("chosen out: Type %v MinutesLate %v, want out and none", got.Type, got.MinutesLate)
	}
	chosenOut.RequestedDirection = nil
	if got := Decide(chosenOut); got.Type == nil || *got.Type != TypeIn {
		t.Fatalf("control: the engine's own direction is %v, want in", got.Type)
	} else {
		wantLate(t, got.MinutesLate, 60)
	}
}

// TestDecide_RequestedDirectionNotesInTwoLanguages is §B.4's split (ADR 0029's): the
// record's note says "direction chosen by employee" for the manager and the report,
// the screen's says it in the person's words, per direction — two different strings.
// The annotations keep the engine's order "<rule>; <policy note>; <left open>;
// <chosen>", and the screen still carries no note effect's sentence.
func TestDecide_RequestedDirectionNotesInTwoLanguages(t *testing.T) {
	t.Parallel()
	if DirectionChosenNote == ChoseClockInEmployeeNote || DirectionChosenNote == ChoseClockOutEmployeeNote ||
		ChoseClockInEmployeeNote == ChoseClockOutEmployeeNote {
		t.Fatal("the record's and the screen's sentences must be three different strings")
	}
	away := onSiteInput()
	elsewhere := uuid.New()
	away.Employee.Location = &elsewhere
	away.Now = mt(t, 27, 18, 0)
	away.DayCutoff = maltaCutoff(5 * time.Hour)
	away.LastOpenIn = openIn(mt(t, 27, 11, 0))
	away.RequestedDirection = dirPtr(TypeIn)
	got := Decide(away)
	if want := ruleIP + "; " + crossLocationReason(t) + "; " + LeftOpenNote + "; " + DirectionChosenNote; got.Note != want {
		t.Errorf("Note = %q, want %q", got.Note, want)
	}
	if want := ruleIP + "; " + LeftOpenEmployeeNote + "; " + ChoseClockInEmployeeNote; got.EmployeeNote != want {
		t.Errorf("EmployeeNote = %q, want %q", got.EmployeeNote, want)
	}

	out := onSiteInput()
	out.DayCutoff = maltaCutoff(5 * time.Hour)
	out.RequestedDirection = dirPtr(TypeOut)
	got = Decide(out)
	if got.Note == got.EmployeeNote {
		t.Fatalf("Note and EmployeeNote are one string %q", got.Note)
	}
	if got.Note != ruleIP+"; "+DirectionChosenNote || got.EmployeeNote != ruleIP+"; "+ChoseClockOutEmployeeNote {
		t.Errorf("Note %q, EmployeeNote %q", got.Note, got.EmployeeNote)
	}
	// The engine's own direction carries neither sentence.
	out.RequestedDirection = dirPtr(TypeIn)
	if got := Decide(out); got.Note != ruleIP || got.EmployeeNote != ruleIP {
		t.Errorf("a request equal to D wrote a note: %q / %q", got.Note, got.EmployeeNote)
	}
}

// TestDecide_PredictDirectionIsTheEnginesDirection is the card's property test: the
// prediction GET /t shows and the engine's direction are one answer. Swept over
// Malta's two DST weeks, Santiago's midnight gap and Lord Howe's 30-minute step, a
// set of cutoffs (none included), open check-ins from 0 to 30 h old (and none), live
// and declared instants: Decide's direction, business day and left-open entry with
// no request are PredictDirection's at the tap's instant; with each request the source
// is "employee" exactly when the record differs from that prediction, and a chosen
// record's anchor agrees with the reader's classification of it.
func TestDecide_PredictDirectionIsTheEnginesDirection(t *testing.T) {
	t.Parallel()
	windows := []struct {
		zone string
		from time.Time
	}{
		{"Europe/Malta", time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)},
		{"Europe/Malta", time.Date(2027, 3, 26, 0, 0, 0, 0, time.UTC)},
		{"America/Santiago", time.Date(2025, 9, 5, 0, 0, 0, 0, time.UTC)},
		{"Australia/Lord_Howe", time.Date(2025, 4, 4, 0, 0, 0, 0, time.UTC)},
	}
	cutoffs := []time.Duration{0, 2 * time.Hour, 5 * time.Hour, 14 * time.Hour, 23*time.Hour + 45*time.Minute}
	ages := []time.Duration{-1, 0, time.Hour, 8 * time.Hour, StaleOpenIn, StaleOpenIn + time.Second, 20 * time.Hour, 30 * time.Hour}
	declared := []time.Duration{-1, 0, 90 * time.Second}
	for _, w := range windows {
		w := w
		t.Run(w.zone+"_"+w.from.Format("2006-01"), func(t *testing.T) {
			t.Parallel()
			seen := map[string]int{}
			for now := w.from; now.Before(w.from.Add(5 * 24 * time.Hour)); now = now.Add(40 * time.Minute) {
				for ci := -1; ci < len(cutoffs); ci++ {
					var cutoff *DayCutoff
					if ci >= 0 {
						cutoff = &DayCutoff{Cutoff: cutoffs[ci], Timezone: w.zone}
					}
					for _, back := range declared {
						for _, age := range ages {
							in := onSiteInput()
							in.Now = now
							in.DayCutoff = cutoff
							if back >= 0 {
								in.OccurredAt, in.OccurredAtFromClient = now.Add(-back), true
							}
							at := tapInstant(in)
							if age >= 0 {
								in.LastOpenIn = openIn(at.Add(-age))
							}
							checkPredictionAgainstDecide(t, in, seen)
						}
					}
				}
			}
			for _, k := range []string{"auto_in", "auto_out", "employee_in", "employee_out",
				"missing_" + string(CheckoutOtherDay), "missing_" + string(CheckoutOrphan), "missing_none"} {
				if seen[k] == 0 {
					t.Errorf("the sweep never produced %s", k)
				}
			}
		})
	}
}

// checkPredictionAgainstDecide is one point of the property sweep.
func checkPredictionAgainstDecide(t *testing.T, in Input, seen map[string]int) {
	t.Helper()
	at := tapInstant(in)
	p := PredictDirection(at, in.LastOpenIn, in.DayCutoff)
	got := Decide(in)
	if got.Type == nil || *got.Type != p.Direction || got.DirectionSource != DirectionAuto ||
		!reflect.DeepEqual(got.BusinessDayStart, p.BusinessDayStart) || !reflect.DeepEqual(got.LeftOpen, p.LeftOpen) {
		t.Fatalf("at %s, cutoff %+v, open %+v: Decide %v %q day %v left %+v, PredictDirection %+v",
			at, in.DayCutoff, in.LastOpenIn, got.Type, got.DirectionSource, got.BusinessDayStart, got.LeftOpen, p)
	}
	seen["auto_"+string(p.Direction)]++
	for _, req := range []Type{TypeIn, TypeOut} {
		in.RequestedDirection = dirPtr(req)
		got := Decide(in)
		if got.Type == nil || *got.Type != req {
			t.Fatalf("at %s: a request for %q recorded %v", at, req, got.Type)
		}
		if (got.DirectionSource == DirectionEmployee) != (req != p.Direction) {
			t.Fatalf("at %s: recorded %q, predicted %q, source %q", at, req, p.Direction, got.DirectionSource)
		}
		if req == p.Direction {
			continue
		}
		seen["employee_"+string(req)]++
		if req == TypeIn {
			if got.LeftOpen == nil || got.LeftOpen.Reason != LeftOpenEmployeeChoice || got.LeftOpen.ID != in.LastOpenIn.ID {
				t.Fatalf("at %s: a chosen in over a closable entry left %+v", at, got.LeftOpen)
			}
			continue
		}
		// The reader's view of a chosen out: the open entry D left open is the event
		// before it; with none, the event before is not an `in`.
		var prev *Prev
		if p.LeftOpen != nil {
			prev = &Prev{Direction: TypeIn, At: in.LastOpenIn.OccurredAt}
		}
		class := ClassifyCheckout(prev, false, at, got.BusinessDayStart)
		want := CheckoutClass("")
		if class == CheckoutOtherDay || class == CheckoutOrphan {
			want = class
		}
		if got.MissingIn != want || got.LeftOpen != nil {
			t.Fatalf("at %s: chosen out classifies %q, MissingIn %q (left %+v)", at, class, got.MissingIn, got.LeftOpen)
		}
		if want == "" {
			seen["missing_none"]++
		} else {
			seen["missing_"+string(want)]++
		}
	}
	in.RequestedDirection = nil
}

// TestDecide_ResolveDirectionCallsPredictDirection pins the shape the property test
// samples: resolveDirection does not compute a direction of its own, it calls
// PredictDirection (ADR 0032 §B.2: "resolveDirection de aynı fonksiyonu çağırır"), and
// PredictDirection is the only function in decide.go that reads LeftOpenStale or
// LeftOpenPreviousBusinessDay — the two reasons D is decided by.
func TestDecide_ResolveDirectionCallsPredictDirection(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "decide.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]bool{}
	readers := map[string]bool{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CallExpr:
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "PredictDirection" {
					calls[fn.Name.Name] = true
				}
			case *ast.Ident:
				if x.Name == "LeftOpenStale" || x.Name == "LeftOpenPreviousBusinessDay" {
					readers[fn.Name.Name] = true
				}
			}
			return true
		})
	}
	if !calls["resolveDirection"] {
		t.Error("resolveDirection does not call PredictDirection")
	}
	if len(readers) != 1 || !readers["PredictDirection"] {
		t.Errorf("the direction's reasons are read by %v, want PredictDirection alone", readers)
	}
}
