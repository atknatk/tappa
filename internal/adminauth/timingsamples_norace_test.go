//go:build !race

package adminauth

// timingSamples and timingSpreadGate WITHOUT -race.

// timingSamples is 20 — the full sample.
//
// 🔬 THE NUMBER IS SPLIT BY BUILD TAG BECAUSE THE RACE DETECTOR CHANGES THE COST
// OF THE THING BEING MEASURED BY A FACTOR OF NINETEEN. Measured on this machine:
// one cost-12 bcrypt comparison takes 0.60 s plain and 11.34 s under -race. At 3
// arms x 20 rounds that is ~36 s plain and ~680 s under -race — and `make test`
// runs with -race, so an early version of this suite took internal/adminauth past
// Go's 10-minute per-package timeout (609 s) and FAILED.
const timingSamples = 20

// timingSpreadGate is 1.5x here — TIGHTER than the race build's 2.5x, and the
// asymmetry is the measurement, not an oversight.
//
// This build runs one package at a time with no detector instrumentation, so the
// noise floor is far lower: measured median spread 1.00x on both observed runs
// (n=20 per arm, ~203 ms per comparison, arms agreeing to three significant
// figures). A 1.5x gate therefore keeps 1.5x of headroom AND still catches the
// nearest signal the race build has to give up:
//
//	dummy at cost 11 vs rows 12   1.91x   <- caught here, MISSED at 2.5x
//	dummy at cost 10 vs rows 12   4.04x   <- caught in both builds
//	dummy removed entirely      515594x   <- caught in both builds
//
// So `go test ./internal/adminauth` is the sharper instrument and `make test` is
// the robust one. Neither is the proof of PHASE B OBLIGATION 2 — the two exact
// structural tests named in manager_timing_test.go's header are.
const timingSpreadGate = 1.5

// worstObservedNoise for THIS build, and it is far lower than the race build's:
// one package at a time, no detector instrumentation, n=20 per arm. Measured 1.00x
// on every observed run (arms agreeing to three significant figures), and 1.06x
// with the two over-long arms added.
//
// ⚠️ THE FIGURE IS PER BUILD, AND THAT IS A CORRECTION. The first version of the
// threshold invariant compared THIS gate (1.5x) against the RACE build's noise
// (1.59x) and failed — correctly, because the comparison was meaningless: 1.59x is
// an artefact of the race detector plus concurrent packages, neither of which
// exists here. Reusing one noise number across two very different measurement
// environments is the M5-08 artefact trap in miniature.
const worstObservedNoise = 1.10
