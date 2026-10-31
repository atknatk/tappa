package adminauth

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
)

// identity_timing_db_test.go -- the clock-based half of the identity sign-in's flat timing (ADR 0031
// §5.6, R14, S15; M14-07), on a real PostgreSQL. identity_shape_test.go holds the exact half (every
// exit's comparison count and call sequence); these two measure DURATIONS, because S15's point is that
// two exits with the same round trips can still differ in what the database does inside them.
//
// THE TECHNIQUE IS TestPanelE2E_TimingIsFlatOverHTTP's and this package's: interleaved rounds so load
// moves every arm together, a ratio gate pinned between measured noise and a measured mutation, and a
// skip under -short (a wall-clock sample, never the only proof). The estimators are the decoy test's
// medians and the five-case test's same-round ratios (pairedSpread; T176 retired its minima). The fixtures are cheap (bcrypt.MinCost, testManager's
// cheap dummy), so the database's share of a sign-in is LARGE here -- the opposite of production,
// where nine cost-12 comparisons dwarf it -- which is what makes a database-side difference visible.

// minOf is the smallest observation.
func minOf(d []time.Duration) time.Duration {
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[0]
}

func medianOf(d []time.Duration) time.Duration {
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

// decoyGate bounds the ratio of the decoy's and the open's medians in
// TestIdentityDecoy_TakesWhatTheOpenTakes, both ways. Measured on the reference stack (17.10 in
// Docker, fsync on, 40 interleaved pairs after eight warm-up pairs), with the decoy's deadline wait
// (identity.go, waitUntil):
//
//	noise    plain 1.00-1.34x (6 runs) · -race 1.02-1.10x (6) · -race beside three other -race
//	         packages 1.02-1.50x (6; the 1.50x run: the open's median rose from ~5 to ~48 ms within
//	         the run and the meter's 32-sample median followed late)
//	signal   the wait removed, the round trip alone: -race 2.59-2.72x (3), loaded 2.46-7.83x (4)
//
// History, so the gate is not widened by the next reader: a plain sleep (no deadline) measured 1.57x
// and 1.60x on CI's loaded runner -- the timer woke ~0.9 ms late and the decoy was the SLOWER arm;
// subtracting a learned overshoot (c40a8fbc) did not absorb it either. The deadline wait is the fix.
// 2.0x sits above the worst measured noise (1.50x) and below the weakest signal (2.46x).
const decoyGate = 2.0

// The anchors decoyGate and fiveCaseGate are pinned between (the gate test below).
const (
	decoyWorstNoise     = 1.50
	decoyWeakestSignal  = 2.46
	fiveCaseWorstNoise  = 1.30
	fiveCaseCaughtShape = 1.98
)

// TestIdentityDecoy_TakesWhatTheOpenTakes (S15): the decoy a failed sign-in sends instead of
// identity_open_session takes, measured, what the open takes. Interleaved: an open of a real identity
// (a real session, the business's audit row) and a decoy, forty times, after eight opens that fill the
// meter. MUTATION: remove the decoy's wait -> it takes the bare round trip and this is red.
func TestIdentityDecoy_TakesWhatTheOpenTakes(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: skipping the decoy-vs-open wall-clock sample; the decoy's PLACE in every exit " +
			"is TestAuthenticatePerson_EveryFailureIsOneShape's, exactly")
	}
	d := testDB(t)
	m := testManager(t, d)
	w := newIDWorld(t, ownerConn(t), true)
	w.linked(w.tenant("M14-07 Decoy Co"), "active", "backfill", 48*time.Hour)
	rows, err := d.ResolveIdentityByEmail(context.Background(), w.email)
	if err != nil {
		t.Fatal(err)
	}
	v := verifiedIdentity{id: rows.ID, digest: rows.PasswordHash}
	ctx := context.Background()
	for i := 0; i < 8; i++ {
		if _, _, err := m.openIdentitySession(ctx, v, uuid.Nil); err != nil {
			t.Fatalf("warm-up open: %v", err)
		}
		if err := m.decoy(ctx); err != nil {
			t.Fatalf("warm-up decoy: %v", err)
		}
	}
	var opens, decoys []time.Duration
	for i := 0; i < 40; i++ {
		start := time.Now()
		if _, _, err := m.openIdentitySession(ctx, v, uuid.Nil); err != nil {
			t.Fatalf("open: %v", err)
		}
		opens = append(opens, time.Since(start))
		start = time.Now()
		if err := m.decoy(ctx); err != nil {
			t.Fatalf("decoy: %v", err)
		}
		decoys = append(decoys, time.Since(start))
	}
	o, dc := medianOf(opens), medianOf(decoys)
	ratio := float64(o) / float64(dc)
	if ratio < 1 {
		ratio = 1 / ratio
	}
	t.Logf("identity_open_session median %v (min %v) · decoy median %v (min %v) · spread %.2fx (gate %.2fx)",
		o, minOf(opens), dc, minOf(decoys), ratio, decoyGate)
	if ratio >= decoyGate {
		t.Fatalf("the decoy (%v) and identity_open_session (%v) differ by %.2fx; a failed sign-in must "+
			"take what an opening one takes in its open step (ADR 0031 §5.6, S15)", dc, o, ratio)
	}
}

// fiveCaseGate bounds the five failure cases' spread as pairedSpread measures it: for every pair of
// cases, the median over the rounds of their SAME-ROUND ratio; the spread is the largest of those.
//
// IT WAS THE RATIO OF THE CASES' MINIMA, AND THAT IS WHAT T176 REPLACED (the gate itself unchanged).
// Each minimum is one sample taken at its own instant, so a calm moment that happened to fall on one
// case's sign-in -- shorter than a round under a loaded -race runner -- set that case's minimum alone:
// CI run 40533075489 failed at 1.66x while the pairs' medians were ~1.19x. A same-round ratio
// compares two sign-ins taken one after the other, and its median ignores the rounds a pause or a
// calm moment hit. The measurements below are pairedSpread's (the minima's, on the reference stack
// unloaded: noise 1.02-1.09x, the first signal 2.58x).
//
// Measured with pairedSpread (cheap fixtures, 15 interleaved rounds) on a 16-core workstation already
// at load average ~25, beside 16 CPU burners -- the event row of the three known-address cases
// INCLUDED (it is inside the measurement, S15):
//
//	noise    plain 1.06-1.30x (10 runs) · -race 1.06-1.25x (10)
//	         (the minima under the same load: -race 1.05-1.68x, 1 run in 10 over the gate -- 1.68x
//	         with the five medians within 1.15x of each other, CI's 1.66x reproduced)
//
// Signals, each a mutation of identity.go:
//
//	has_active_membership read BEFORE the comparisons (the fifth case refused      plain 1.98-2.06x (5)
//	early, its decoy and event step kept), pairedSpread, loaded as above            -race 18.8-22.5x (5)  caught
//	the legacy window left unpadded (1-2 comparisons instead of 9), minima          1.29x  NOT caught
//	the decoy removed from every failure (uniform, so the five stay together)       1.03x  NOT caught
//
// The two it cannot see are exact elsewhere: TestAuthenticatePerson_EveryFailureIsOneShape counts
// the comparisons and the calls of every exit, and TestIdentityDecoy_TakesWhatTheOpenTakes times the
// decoy against the open. At these cheap fixtures one comparison is ~1 ms against ~5 ms of database
// work; at the shipped cost the padding's signal is ~9x, which this gate would see -- but a
// cost-12 sample here would cost minutes under -race for a property already held exactly.
const fiveCaseGate = 1.5

// pairedSpread is fiveCaseGate's estimator: d[i][r] is case i's sample in round r. For every pair of
// cases it takes the median of their same-round ratios d[i][r]/d[j][r], and it answers the largest,
// as a factor of at least 1, with the slower case first.
func pairedSpread(d [][]time.Duration) (spread float64, slower, faster int) {
	for i := range d {
		for j := i + 1; j < len(d); j++ {
			r := make([]float64, 0, len(d[i]))
			for k := range d[i] {
				r = append(r, float64(d[i][k])/float64(d[j][k]))
			}
			sort.Float64s(r)
			m, a, b := r[len(r)/2], i, j
			if m < 1 {
				m, a, b = 1/m, j, i
			}
			if m > spread {
				spread, slower, faster = m, a, b
			}
		}
	}
	return spread, slower, faster
}

// TestIdentityTimingGates_AreBetweenTheNoiseAndTheSignal pins both gates as RELATIONS
// (TestHTTPTimingGate_IsBetweenTheNoiseAndTheSignal's reason: the tempting repair for a flaky timing
// test is a wider gate, and a widened gate is a disarmed one).
func TestIdentityTimingGates_AreBetweenTheNoiseAndTheSignal(t *testing.T) {
	for _, g := range []struct {
		name                string
		gate, noise, signal float64
	}{
		{"decoyGate", decoyGate, decoyWorstNoise, decoyWeakestSignal},
		{"fiveCaseGate", fiveCaseGate, fiveCaseWorstNoise, fiveCaseCaughtShape},
	} {
		if g.gate <= g.noise || g.gate >= g.signal {
			t.Errorf("%s is %.2fx; it must sit above the worst measured noise (%.2fx) and below the signal it exists to catch (%.2fx)",
				g.name, g.gate, g.noise, g.signal)
		}
	}
}

// TestAuthenticatePerson_FiveFailuresTakeOneTime is ADR 0031 §5.6's five cases on the clock, the
// event-row write inside the measurement (S15; failLogin's pattern): an identity or none x an
// unlinked row or none (an unknown address included), and the right password on an identity whose
// every membership is disabled. Each returns ErrBadCredentials; their same-round ratios must agree
// within fiveCaseGate (pairedSpread).
func TestAuthenticatePerson_FiveFailuresTakeOneTime(t *testing.T) {
	if testing.Short() {
		t.Skip("-short: skipping the five-case wall-clock sample; each case's comparisons and calls are " +
			"TestAuthenticatePerson_EveryFailureIsOneShape's, exactly")
	}
	d := testDB(t)
	m := testManager(t, d)
	owner := ownerConn(t)
	ctx := context.Background()

	withRow := newIDWorld(t, owner, true)
	withRow.linked(withRow.tenant("M14-07 Five A"), "active", "backfill", 72*time.Hour)
	withBoth := newIDWorld(t, owner, true)
	withBoth.linked(withBoth.tenant("M14-07 Five B"), "active", "backfill", 72*time.Hour)
	withBoth.row(withBoth.tenant("M14-07 Five B2"), fixtureDigest(t, "the legacy password"), "active", time.Hour)
	legacyOnly := newIDWorld(t, owner, false)
	legacyOnly.row(legacyOnly.tenant("M14-07 Five C"), legacyOnly.digest, "active", time.Hour)
	disabled := newIDWorld(t, owner, true)
	disabled.linked(disabled.tenant("M14-07 Five E"), "disabled", "backfill", 72*time.Hour)

	// Fill the open-cost meter the way production does: with real opens.
	for i := 0; i < 8; i++ {
		if in, err := m.AuthenticatePerson(ctx, withRow.email, withRow.pw, uuid.Nil, generousBudget()); err != nil || in.Session == nil {
			t.Fatalf("warm-up sign-in: %+v, %v", in, err)
		}
	}

	cases := []struct {
		name, email, pw string
		d               []time.Duration
	}{
		{name: "identity, no unlinked row, wrong password", email: withRow.email, pw: "wrong"},
		{name: "identity and an unlinked row, wrong password", email: withBoth.email, pw: "wrong"},
		{name: "no identity, an unlinked row, wrong password", email: legacyOnly.email, pw: "wrong"},
		{name: "an unknown address", email: "nobody-" + uuid.NewString() + "@identity.example", pw: "wrong"},
		{name: "right password, every membership disabled", email: disabled.email, pw: disabled.pw},
	}
	budget := generousBudget()
	for round := 0; round < 15; round++ {
		for i := range cases {
			start := time.Now()
			in, err := m.AuthenticatePerson(ctx, cases[i].email, cases[i].pw, uuid.Nil, budget)
			cases[i].d = append(cases[i].d, time.Since(start))
			if !errors.Is(err, ErrBadCredentials) || in.Session != nil {
				t.Fatalf("%s: %+v, %v; want ErrBadCredentials", cases[i].name, in, err)
			}
		}
	}
	ds := make([][]time.Duration, len(cases))
	for i, c := range cases {
		ds[i] = c.d
		t.Logf("%-46s min %v  median %v", c.name, minOf(c.d), medianOf(c.d))
	}
	ratio, a, b := pairedSpread(ds)
	t.Logf("five-case spread %.2fx between %q and %q (gate %.2fx)", ratio, cases[a].name, cases[b].name, fiveCaseGate)
	if ratio >= fiveCaseGate {
		t.Fatalf("the five failure cases are distinguishable by time: %q takes %.2fx what %q takes (the median of %d "+
			"same-round ratios), gate %.2fx", cases[a].name, ratio, cases[b].name, len(ds[a]), fiveCaseGate)
	}
	if n := withRow.events("login_failed"); n != 15 {
		t.Errorf("the identity case wrote %d login_failed rows over 15 failures, want 15 -- the write is part of what was measured", n)
	}
}
