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
// moves every arm together, the MINIMUM per arm as the estimator (scheduler noise only adds), a
// ratio gate pinned between measured noise and a measured mutation, and a skip under -short (a
// wall-clock sample, never the only proof). The fixtures are cheap (bcrypt.MinCost, testManager's
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

// decoyGate bounds |decoy / open| in TestIdentityDecoy_TakesWhatTheOpenTakes, medians, both ways.
// Measured on the reference stack (17.10 in Docker, fsync on, 40 interleaved pairs, five runs plain
// and under -race): noise 1.06-1.16x (the decoy a little SLOWER -- it waits for the open's median and
// a timer overshoots); the signal -- the wait removed, the round trip alone -- 2.42x (two runs).
const decoyGate = 1.5

// The anchors decoyGate and fiveCaseGate are pinned between (the gate test below).
const (
	decoyWorstNoise     = 1.16
	decoyWeakestSignal  = 2.42
	fiveCaseWorstNoise  = 1.09
	fiveCaseCaughtShape = 2.58
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

// fiveCaseGate bounds the spread of the five failure cases' minima.
// Measured on the reference stack (cheap fixtures, 15 interleaved rounds, five runs plain and under
// -race): noise 1.02-1.09x, the event row of the three known-address cases INCLUDED (it is inside the
// measurement, S15). Signals, each a mutation of identity.go:
//
//	has_active_membership read BEFORE the comparisons (the fifth case refused early)   2.58x  caught
//	the legacy window left unpadded (1-2 comparisons instead of 9)                     1.29x  NOT caught
//	the decoy removed from every failure (uniform, so the five stay together)          1.03x  NOT caught
//
// The two it cannot see are exact elsewhere: TestAuthenticatePerson_EveryFailureIsOneShape counts
// the comparisons and the calls of every exit, and TestIdentityDecoy_TakesWhatTheOpenTakes times the
// decoy against the open. At these cheap fixtures one comparison is ~1 ms against ~5 ms of database
// work; at the shipped cost the padding's signal is ~9x, which this gate would see -- but a
// cost-12 sample here would cost minutes under -race for a property already held exactly.
const fiveCaseGate = 1.5

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
// every membership is disabled. Each returns ErrBadCredentials; their minima must agree within
// fiveCaseGate.
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
	var slowest, fastest time.Duration
	for i, c := range cases {
		lo := minOf(c.d)
		t.Logf("%-46s min %v  median %v", c.name, lo, medianOf(c.d))
		if i == 0 || lo > slowest {
			slowest = lo
		}
		if i == 0 || lo < fastest {
			fastest = lo
		}
	}
	ratio := float64(slowest) / float64(fastest)
	t.Logf("five-case spread %.2fx (gate %.2fx)", ratio, fiveCaseGate)
	if ratio >= fiveCaseGate {
		t.Fatalf("the five failure cases are distinguishable by time: spread %.2fx, gate %.2fx", ratio, fiveCaseGate)
	}
	if n := withRow.events("login_failed"); n != 15 {
		t.Errorf("the identity case wrote %d login_failed rows over 15 failures, want 15 -- the write is part of what was measured", n)
	}
}
