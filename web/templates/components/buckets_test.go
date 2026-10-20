package components

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestPctOf_IsAWholePercentAndZeroSafe(t *testing.T) {
	tests := []struct {
		name    string
		n, of   int
		wantPct int
	}{
		{"nothing of something", 0, 7, 0},
		{"something of nothing is not a division by zero", 3, 0, 0},
		{"nothing of nothing", 0, 0, 0},
		{"a negative total", 3, -4, 0},
		{"a negative count", -3, 4, 0},
		{"the whole", 7, 7, 100},
		{"more than the whole is held to 100", 9, 7, 100},
		{"truncated, not rounded: 2 of 3 is 66", 2, 3, 66},
		{"1 of 3 is 33", 1, 3, 33},
		{"1 of 200 is 0", 1, 200, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := PctOf(tc.n, tc.of); got != tc.wantPct {
				t.Errorf("PctOf(%d, %d) = %d, want %d", tc.n, tc.of, got, tc.wantPct)
			}
		})
	}
}

func TestWidthPct_TakesTheNearestStepOfFiveAndHoldsTheRange(t *testing.T) {
	tests := []struct {
		name string
		p    int
		want string
	}{
		{"zero is no width", 0, "w-0"},
		{"100 is the whole track", 100, "w-full"},
		{"a negative percent is no width", -40, "w-0"},
		{"more than 100 is the whole track", 250, "w-full"},
		{"2 goes down to 0", 2, "w-0"},
		{"3 goes up to 5", 3, "w-[5%]"},
		{"5 stays", 5, "w-[5%]"},
		{"42 goes down to 40", 42, "w-[40%]"},
		{"43 goes up to 45", 43, "w-[45%]"},
		{"66 goes down to 65", 66, "w-[65%]"},
		{"97 goes down to 95", 97, "w-[95%]"},
		{"98 goes up to the whole track", 98, "w-full"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := WidthPct(tc.p); got != tc.want {
				t.Errorf("WidthPct(%d) = %q, want %q", tc.p, got, tc.want)
			}
		})
	}
}

// TestWidthPct_HasTwentyOneNamesOneForEachStep sweeps every input from below the
// range to above it: exactly 21 distinct names come back, step k*5 maps to its own
// name, and the names are the arbitrary-percent spelling between the two ends.
func TestWidthPct_HasTwentyOneNamesOneForEachStep(t *testing.T) {
	seen := map[string]bool{}
	for p := -10; p <= 110; p++ {
		seen[WidthPct(p)] = true
	}
	if len(seen) != 21 {
		t.Fatalf("WidthPct returns %d distinct names over -10..110, want 21: %v", len(seen), seen)
	}
	for step := 5; step <= 95; step += 5 {
		want := "w-[" + strconv.Itoa(step) + "%]"
		if got := WidthPct(step); got != want {
			t.Errorf("WidthPct(%d) = %q, want %q", step, got, want)
		}
	}
}

func TestSplitPct_SharesExactly100InStepsOfFive(t *testing.T) {
	tests := []struct {
		name   string
		counts []int
		want   []int
	}{
		{"one segment takes the whole", []int{7}, []int{100}},
		{"halves", []int{3, 3}, []int{50, 50}},
		{"thirds: the leftover units go to the earlier segments", []int{1, 1, 1}, []int{35, 35, 30}},
		{"the largest remainder wins, not the first segment", []int{1, 2}, []int{35, 65}},
		{"a zero count gets nothing and is not a division by zero", []int{0, 4, 0}, []int{0, 100, 0}},
		{"a negative count is treated as nothing", []int{-5, 1, 1}, []int{0, 50, 50}},
		{"a tiny share may round to 0 -- its count is printed beside it", []int{1, 99}, []int{0, 100}},
		{"seven equal parts", []int{1, 1, 1, 1, 1, 1, 1}, []int{15, 15, 15, 15, 15, 15, 10}},
		{"a skewed set", []int{10, 3, 2, 1}, []int{65, 20, 10, 5}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitPct(tc.counts)
			if !slices.Equal(got, tc.want) {
				t.Errorf("SplitPct(%v) = %v, want %v", tc.counts, got, tc.want)
			}
		})
	}
}

// TestSplitPct_AlwaysSumsTo100AndEveryPartIsAStep is the property over many shapes:
// any set with a positive count sums to exactly 100, every part is a multiple of five
// inside 0..100 (so WidthPct draws it unmoved), and a part never belongs to a
// segment whose count was not positive.
func TestSplitPct_AlwaysSumsTo100AndEveryPartIsAStep(t *testing.T) {
	shapes := 0
	for a := -1; a <= 9; a++ {
		for b := 0; b <= 9; b++ {
			for c := 0; c <= 13; c++ {
				for _, d := range []int{0, 1, 7, 1000} {
					counts := []int{a, b, c, d}
					got := SplitPct(counts)
					positive, sum := false, 0
					for i, p := range got {
						sum += p
						if p%5 != 0 || p < 0 || p > 100 {
							t.Fatalf("SplitPct(%v)[%d] = %d, not a step of five in 0..100", counts, i, p)
						}
						if counts[i] <= 0 && p != 0 {
							t.Fatalf("SplitPct(%v)[%d] = %d for a count that is not positive", counts, i, p)
						}
						if pctStep(p) != p {
							t.Fatalf("SplitPct(%v)[%d] = %d, which WidthPct would move", counts, i, p)
						}
						positive = positive || counts[i] > 0
					}
					switch {
					case positive && sum != 100:
						t.Fatalf("SplitPct(%v) = %v sums to %d, want 100", counts, got, sum)
					case !positive && sum != 0:
						t.Fatalf("SplitPct(%v) = %v sums to %d with nothing counted, want 0", counts, got, sum)
					}
					shapes++
				}
			}
		}
	}
	if got := SplitPct(nil); len(got) != 0 {
		t.Errorf("SplitPct(nil) = %v, want an empty split", got)
	}
	t.Logf("shapes checked: %d", shapes)
}

func TestTapeBar_NamesTenBucketsAndNoBarForNothing(t *testing.T) {
	tests := []struct {
		name   string
		bucket int
		want   string
	}{
		{"0 draws no bar", 0, ""},
		{"a negative bucket draws no bar", -3, ""},
		{"the lowest", 1, "tape-bar--1"},
		{"the middle", 5, "tape-bar--5"},
		{"the tallest", 10, "tape-bar--10"},
		{"more than 10 is the tallest", 37, "tape-bar--10"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := TapeBar(tc.bucket); got != tc.want {
				t.Errorf("TapeBar(%d) = %q, want %q", tc.bucket, got, tc.want)
			}
		})
	}
	for k := 1; k <= 10; k++ {
		if got, want := TapeBar(k), "tape-bar--"+strconv.Itoa(k); got != want {
			t.Errorf("TapeBar(%d) = %q, want %q", k, got, want)
		}
	}
}

// TestBuckets_EveryNameIsCompiled is the half the unit tests cannot see: a name that
// is returned but not compiled draws nothing. It reads the compiled stylesheet and
// wants a rule for each of the 21 width names and the 10 tape names, which also
// catches a name built from pieces instead of written out in buckets.templ.
//
// ⚠️ IT NEEDS web/static/css/app.css, which is gitignored: CI builds it before the
// tests (make css); a local run without it SKIPs, and a skip is not a pass.
func TestBuckets_EveryNameIsCompiled(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "static", "css", "app.css"))
	if err != nil {
		t.Skipf("no compiled stylesheet to read (%v) -- run `make css`. THIS IS NOT A PASS.", err)
	}
	css := string(raw)
	names := map[string]bool{}
	for p := 0; p <= 100; p++ {
		names[WidthPct(p)] = true
	}
	for k := 1; k <= 10; k++ {
		names[TapeBar(k)] = true
	}
	if len(names) != 31 {
		t.Fatalf("collected %d names, want 31 (21 widths, 10 tape heights)", len(names))
	}
	escape := strings.NewReplacer("[", `\[`, "]", `\]`, "%", `\%`)
	for name := range names {
		rule := regexp.MustCompile(regexp.QuoteMeta("."+escape.Replace(name)) + `\{(?:width|height):`)
		if !rule.MatchString(css) {
			t.Errorf("app.css has no width or height rule for .%s", name)
		}
	}
}
