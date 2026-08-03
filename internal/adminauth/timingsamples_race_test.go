//go:build race

package adminauth

// timingSamples and timingSpreadGate UNDER -race — i.e. what `make test` runs.

// timingSamples is 3 under the race detector. One cost-12 comparison is ~11 s
// there against ~0.6 s without, so 20 rounds per arm would cost ~680 s and exceed
// Go's per-package test timeout. The test is NOT skipped (a skipped test does not
// count as passed in this repo); it runs with a smaller sample and logs it.
const timingSamples = 3

// timingSpreadGate is 2.5x, DERIVED FROM MEASURED NOISE rather than chosen.
//
// 🔴 IT WAS 1.50x AND IT WENT RED ON A CLEAN TREE, 1 full-suite run in 5. At n=3,
// with 14 packages running concurrently under -race, the median of three samples
// measures the scheduler as much as it measures bcrypt.
//
// THE TWO ENDS, BOTH MEASURED ON THIS MACHINE:
//
//	NOISE (what must NOT trip the gate)
//	  10 consecutive full-suite runs, -race, this build:
//	      median spread  min 1.00x · median 1.01x · max 1.07x
//	      intra-arm max/min over all 30 arms: median 1.07x · max 1.56x
//	  an independent audit, on a more loaded machine:
//	      median spread 1.59x (the run that failed) · intra-arm swing 2.3x
//	  -> worst noise ever observed: 1.59x
//
//	SIGNAL (what the gate must still catch)
//	  dummy REMOVED entirely      515594x   (234 ns vs ~200 ms)
//	  dummy at cost 10 vs rows 12    4.04x  (50.5 ms vs 204 ms)
//	  dummy at cost 11 vs rows 12    1.91x  <- BELOW this gate; see the limit
//
// 2.5x sits above the worst observed noise with 1.57x of headroom.
//
// ⚠️ AN EARLIER VERSION SAID "1.62x below the NEAREST signal", naming 4.04x. That
// is wrong and the same file contradicted it four lines down: the nearest signal is
// the one-cost-step case at 1.91x, and it is BELOW this gate — deliberately given
// up, not caught. The honest phrasing is: 2.5x sits above all observed noise and
// below the nearest signal it still CATCHES (4.04x), having conceded the 1.91x one
// to the exact structural check.
//
// ⚠️ THE LIMIT, STATED RATHER THAN IMPLIED: this gate can no longer see a timing
// leak smaller than 2.5x. Concretely it would MISS a dummy one bcrypt cost step
// off (1.91x, measured above). That is acceptable ONLY because the cost mismatch
// is caught EXACTLY and without statistics by TestCost_MatchesTheDummyDigest and
// TestSeedDigests_UseTheDeclaredCost, which compare integers read out of the
// digests themselves. What is genuinely unguarded is a leak below 2.5x that is
// neither a missing dummy nor a cost mismatch — a third shape nobody has modelled.
// The !race build keeps a 1.50x gate and does catch the 1.91x case.
const timingSpreadGate = 2.5

// worstObservedNoise is the highest median spread NOISE ALONE has produced in THIS
// build. Under -race with 14 packages running concurrently: 1.59x, seen by an
// independent audit on a loaded machine (10 local full-suite runs peaked at 1.07x).
// TestTimingThresholds_AreBetweenTheNoiseAndTheSignal keeps the gate above it.
const worstObservedNoise = 1.59
