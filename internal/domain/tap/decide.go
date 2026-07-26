package tap

// Decide turns one tap's evidence (Input) into a single explainable Decision. It
// is the tap engine's ONLY entry point and it is PURE: a deterministic function of
// its Input, with no clock, no randomness, no DB and no HTTP (see the types.go
// package doc). It writes NO record — a Decision only describes what should happen;
// persisting it (and the §4.6 write-and-flag path) is the M5 caller's job.
//
// SIGNATURE IS FIXED HERE (M4-02). The BODY is M4-03: it will build a
// policy.Context from the evidence, call policy.Evaluate, map the returned effect
// to a Verdict (allow->ok, review->flag, deny->reject, ignore->ignored; the
// no-session guardrail -> RedirectActivation with no record), and compute the
// arithmetic that is NOT policy — direction (toggle off the last open check-in),
// trust (20 + 50·IP + 30·GPS) and lateness (against the resolved shift). Each row
// of CLAUDE.md §5 becomes a TestDecide_… case in M4-07.
//
// Until M4-03 wires the body, Decide PANICS rather than returning a zero Decision:
// a zero value would be a silent VerdictOK-less, no-direction record — exactly the
// "silent approval" §4.6 forbids — whereas a panic makes an accidental call in an
// unfinished build loud and impossible to miss.
func Decide(in Input) Decision {
	// M4-03: build policy.Context, call policy.Evaluate, map effect->verdict, and
	// compute direction / trust / lateness. See decide.go doc above and §5.
	_ = in
	panic("tap.Decide: decision logic not implemented yet (M4-03)")
}
