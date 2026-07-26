package tap

import (
	"net/netip"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/geo"
	"github.com/google/uuid"
)

// This file is the M4-02 TYPE contract test: it proves Input, Decision and the
// helper types compile and are usable, and that Decide has the fixed pure
// signature. It deliberately does NOT call Decide — the body is M4-03 and the
// stub panics; the §5 decision-table cases (TestDecide_…) live in M4-07.

// Decide must be exactly func(Input) Decision — a value assignment fails to
// compile if the signature ever drifts (e.g. gains a context.Context, which would
// break purity).
var _ func(Input) Decision = Decide

// TestInputDecision_construct exercises every field of Input and Decision and
// every enum constant, so a renamed/removed field or a mistyped enum is caught at
// compile time. It asserts nothing about decision LOGIC (that is M4-07).
func TestInputDecision_construct(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 7, 26, 18, 5, 0, 0, time.UTC)
	loc := uuid.New()
	profileLoc := uuid.New()
	dept := uuid.New()

	in := Input{
		Now:     now,
		Channel: ChannelNFC,
		SUN:     SUNResult{Valid: true, CtrGap: 0},
		Tag:     &Tag{Status: TagActive, Location: loc},
		Employee: &Employee{
			Status:      EmployeeActive,
			Location:    &profileLoc,
			Department:  &dept,
			ActivatedAt: now.Add(-24 * time.Hour),
		},
		SourceIP:    netip.MustParseAddr("203.0.113.7"),
		LocationIPs: []netip.Prefix{netip.MustParsePrefix("203.0.113.0/24")},
		GPS:         &geo.Point{Lat: 35.8989, Lng: 14.5146},
		LocationGPS: &geo.Point{Lat: 35.8990, Lng: 14.5147},
		GPSRadiusM:  150,
		LastForPerson: &Transaction{
			ID:         uuid.New(),
			OccurredAt: now.Add(-8 * time.Hour),
			Direction:  TypeOut,
			Practice:   false,
		},
		LastOpenIn: &Transaction{
			ID:         uuid.New(),
			OccurredAt: now.Add(-1 * time.Hour),
			Direction:  TypeIn,
		},
		Shift: &Shift{
			Start:     18 * time.Hour,
			End:       2 * time.Hour,
			Overnight: true,
			Timezone:  "Europe/Malta",
		},
		Debounce: 60 * time.Second,
	}

	if in.Tag.Status != TagActive || in.Employee.Status != EmployeeActive {
		t.Fatalf("enum wiring broken: tag=%q employee=%q", in.Tag.Status, in.Employee.Status)
	}

	dir := TypeIn
	dec := Decision{
		Verdict:  VerdictOK,
		Type:     &dir,
		Trust:    50,
		IPMatch:  true,
		GPSMatch: false,
		Note:     "verified via GPS",
		Practice: false,
		Redirect: RedirectNone,
		Security: false,
	}
	if dec.Verdict != VerdictOK || *dec.Type != TypeIn {
		t.Fatalf("decision wiring broken: verdict=%q type=%v", dec.Verdict, dec.Type)
	}

	// The full closed vocabularies must be distinct, non-empty strings (guards
	// against a copy-paste collision between constants).
	verdicts := map[Verdict]struct{}{VerdictOK: {}, VerdictFlag: {}, VerdictReject: {}, VerdictIgnored: {}}
	if len(verdicts) != 4 {
		t.Fatalf("verdict constants collide: %v", verdicts)
	}
	channels := map[Channel]struct{}{ChannelNFC: {}, ChannelQR: {}, ChannelManual: {}}
	if len(channels) != 3 {
		t.Fatalf("channel constants collide: %v", channels)
	}
	redirects := map[Redirect]struct{}{RedirectNone: {}, RedirectActivation: {}}
	if len(redirects) != 2 {
		t.Fatalf("redirect constants collide: %v", redirects)
	}
	// RedirectNone is the zero value so a default Decision means "no redirect".
	if (Decision{}).Redirect != RedirectNone {
		t.Fatalf("RedirectNone must be the zero value, got %q", (Decision{}).Redirect)
	}
	// A reject/ignored/redirect decision carries no direction: nil Type is valid.
	if (Decision{Verdict: VerdictReject}).Type != nil {
		t.Fatalf("zero Decision.Type must be nil")
	}
}
