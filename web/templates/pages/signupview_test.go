package pages

import "testing"

// TestSignupProgress_NamesEveryStep holds the progress docket's titles to the step
// count the counter prints ("Step N of 3"): a step added to one and not the other
// would make the docket and the counter disagree about where the visitor is.
func TestSignupProgress_NamesEveryStep(t *testing.T) {
	if len(signupStepTitles) != signupSteps {
		t.Fatalf("the progress docket names %d steps and the counter counts %d",
			len(signupStepTitles), signupSteps)
	}
}

// TestSignupProgress_EachStepHasOneWord walks every position the wizard can be in and
// asserts the words the docket prints: exactly one step is "now" on a form, none on
// the confirmation, and every step before the current one is "done".
func TestSignupProgress_EachStepHasOneWord(t *testing.T) {
	for step := 1; step <= signupSteps; step++ {
		p := signupProgress{Step: step}
		now := 0
		for i := 1; i <= signupSteps; i++ {
			want := "next"
			switch {
			case i < step:
				want = "done"
			case i == step:
				want = "now"
				now++
			}
			if got := p.state(i); got != want {
				t.Errorf("on step %d, step %d says %q, want %q", step, i, got, want)
			}
		}
		if now != 1 {
			t.Errorf("on step %d, %d steps are current", step, now)
		}
		if got := p.completed(); got != step-1 {
			t.Errorf("on step %d the bar reports %d steps behind, want %d", step, got, step-1)
		}
	}
	done := signupProgress{Done: true}
	for i := 1; i <= signupSteps; i++ {
		if got := done.state(i); got != "done" {
			t.Errorf("on the confirmation step %d says %q, want done", i, got)
		}
	}
	if done.completed() != signupSteps {
		t.Errorf("the confirmation's bar reports %d of %d", done.completed(), signupSteps)
	}
	if (signupProgress{}).shown() {
		t.Error("a screen that knows no step draws progress")
	}
	if got := (signupProgress{Step: 1}).name(); got != "New business" {
		t.Errorf("before step one is answered the docket names %q", got)
	}
}
