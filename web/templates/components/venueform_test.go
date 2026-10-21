package components

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"
)

// TestShiftBar_IsWholeMinutes (M12-35): the 24-hour bar is drawn from the form's two
// "HH:MM" strings in whole minutes -- no fraction anywhere (§6) -- an overnight shift is
// two pieces, and a shift that does not parse draws nothing.
func TestShiftBar_IsWholeMinutes(t *testing.T) {
	tests := []struct {
		name       string
		start, end string
		overnight  bool
		want       []shiftRect
	}{
		{"a day shift", "10:00", "22:00", false, []shiftRect{{600, 720}}},
		{"odd minutes", "08:07", "16:59", false, []shiftRect{{487, 532}}},
		{"overnight", "18:00", "02:00", true, []shiftRect{{1080, 360}, {0, 120}}},
		{"overnight to midnight", "18:00", "00:00", true, []shiftRect{{1080, 360}}},
		{"an end before the start without the tick", "18:00", "02:00", false, nil},
		{"empty", "", "", false, nil},
		{"half filled", "10:00", "", false, nil},
		{"not a clock", "25:00", "26:00", false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shiftBarRects(tc.start, tc.end, tc.overnight)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("piece %d = %v, want %v", i, got[i], tc.want[i])
				}
			}
			var b bytes.Buffer
			if err := ShiftBar(tc.start, tc.end, tc.overnight).Render(context.Background(), &b); err != nil {
				t.Fatal(err)
			}
			html := b.String()
			if len(tc.want) == 0 {
				if html != "" {
					t.Errorf("a shift that does not parse drew %q", html)
				}
				return
			}
			rects := regexp.MustCompile(`<rect x="([^"]*)" y="0" width="([^"]*)"`).FindAllStringSubmatch(html, -1)
			if len(rects) != len(tc.want) {
				t.Fatalf("drew %d piece(s), want %d:\n%s", len(rects), len(tc.want), html)
			}
			for _, r := range rects {
				for _, n := range r[1:] {
					if !regexp.MustCompile(`^[0-9]+$`).MatchString(n) {
						t.Errorf("an attribute is %q, not a whole number of minutes", n)
					}
				}
			}
			if strings.Contains(html, "style=") {
				t.Error("the bar carries a style attribute; the panel's policy refuses it")
			}
		})
	}
}
