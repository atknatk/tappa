package legal_test

import (
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/domain/legal"
)

// TestParagraphs_SplitsOnBlankLinesAndNothingElse.
//
// 🔴 THIS FUNCTION IS THE REASON templ.Raw IS NOT REACHED. It returns STRINGS, which
// the template loops over inside `{ }`, so every character of a legal text stays
// inside templ's escaping. Anything that made it return markup would move the
// product's one free-prose-into-a-public-page path into the escape hatch this
// repository has measured as a blind spot.
func TestParagraphs_SplitsOnBlankLinesAndNothingElse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"whitespace only", "   \n\t\n  ", nil},
		{"one line", "We keep attendance records for six years.", []string{"We keep attendance records for six years."}},
		{
			"two paragraphs",
			"First paragraph.\n\nSecond paragraph.",
			[]string{"First paragraph.", "Second paragraph."},
		},
		{
			// A single newline is a WRAPPED LINE, not a new thought — which is what a
			// textarea produces when somebody's window is narrow.
			"a single newline joins with a space",
			"A sentence that was\nwrapped by the box.",
			[]string{"A sentence that was wrapped by the box."},
		},
		{
			// 🔴 CRLF IS THE ONLY THING A BROWSER EVER SENDS. HTML's own form spec says a
			// textarea is normalised to CRLF on submit, so a splitter that only knew
			// "\n\n" would find NO paragraphs in the one input this function exists for.
			"CRLF from a real textarea",
			"First paragraph.\r\n\r\nSecond paragraph.",
			[]string{"First paragraph.", "Second paragraph."},
		},
		{"bare CR", "First.\r\rSecond.", []string{"First.", "Second."}},
		{
			"many blank lines are still one break",
			"First.\n\n\n\n\nSecond.",
			[]string{"First.", "Second."},
		},
		{
			"leading and trailing whitespace goes",
			"\n\n  Only paragraph.  \n\n",
			[]string{"Only paragraph."},
		},
		{
			// Markup is DATA here. It is neither stripped nor interpreted; the template
			// escapes it. A function that stripped it would silently edit somebody's
			// legal text.
			"markup is carried through as text",
			"<script>alert(1)</script>",
			[]string{"<script>alert(1)</script>"},
		},
		{
			"indentation inside a paragraph is collapsed, between paragraphs preserved as a break",
			"Clause 1.\n    Sub-clause.\n\nClause 2.",
			[]string{"Clause 1. Sub-clause.", "Clause 2."},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := legal.Paragraphs(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("Paragraphs(%q) = %q (%d), want %q (%d)", tc.in, got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("paragraph %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestParagraphs_NeverEmitsMarkupOfItsOwn is the property the whole design rests on,
// stated as a property rather than as cases.
//
// Whatever goes in, nothing comes out that was not in the input. In particular no
// "<p>", no "<br>" and no entity: the moment this function started producing markup
// it would need templ.Raw to be useful, and that is the door this design exists to
// keep shut.
func TestParagraphs_NeverEmitsMarkupOfItsOwn(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"plain",
		"one\n\ntwo\n\nthree",
		"a\r\nb\r\n\r\nc",
		strings.Repeat("word ", 500),
	}
	for _, in := range inputs {
		for _, p := range legal.Paragraphs(in) {
			for _, bad := range []string{"<p>", "</p>", "<br", "&lt;", "&amp;", "&gt;"} {
				if strings.Contains(p, bad) {
					t.Errorf("Paragraphs(%.20q...) produced %q, which contains %q. This function "+
						"must return the operator's own characters and nothing else; producing "+
						"markup here is what would make templ.Raw necessary.", in, p, bad)
				}
			}
		}
	}
	// POSITIVE CONTROL with an INDEPENDENT input: markup that WAS in the input must
	// come out, or the loop above is passing because nothing is being produced at all.
	got := legal.Paragraphs("before <p>kept</p> after")
	if len(got) != 1 || !strings.Contains(got[0], "<p>kept</p>") {
		t.Errorf("Paragraphs dropped markup that was in the input: %q. The scan above only "+
			"means something if this function is actually returning the text.", got)
	}
}

// TestValid_IsTheClosedSet.
func TestValid_IsTheClosedSet(t *testing.T) {
	t.Parallel()
	// The four, written out by hand rather than read from legal.Slugs — a test that
	// ranges over the subject to check the subject checks nothing.
	for _, ok := range []string{"privacy", "terms", "imprint", "cookies"} {
		if !legal.Valid(ok) {
			t.Errorf("Valid(%q) = false; that document has a page and a column value", ok)
		}
	}
	for _, no := range []string{"", " ", "Privacy", "PRIVACY", "privacy ", "admin", "../privacy", "privacy2"} {
		if legal.Valid(no) {
			t.Errorf("Valid(%q) = true. The set is closed because 00020's CHECK is closed; a "+
				"value this accepts and the column refuses is a 23514 the operator cannot act on.", no)
		}
	}
	if len(legal.Slugs) != 4 {
		t.Errorf("legal.Slugs has %d entries, want 4. If a fifth document is real it needs a "+
			"migration, a page and a line in this test — all three.", len(legal.Slugs))
	}
}

// TestNewStore_RefusesAMissingTrail — §4.3 is not optional.
//
// A Store that could be built without an audit sink is a Store whose ONE write path
// could publish a legal text with no record of who did it. The constructor refuses
// rather than degrading, which is the shape billing.NewBook already uses for the same
// reason.
func TestNewStore_RefusesAMissingTrail(t *testing.T) {
	t.Parallel()
	if _, err := legal.NewStore(nil, nil); err == nil {
		t.Error("NewStore accepted a nil database")
	}
	if _, err := legal.NewStore(stubDB{}, nil); err == nil {
		t.Error("NewStore accepted a nil audit trail. §4.3 requires every change to reach " +
			"audit_log, and this type's only write is the one that changes what every " +
			"visitor to /legal reads.")
	}
	// POSITIVE CONTROL: with both, it builds — otherwise the refusals above could be
	// a constructor that refuses everything.
	s, err := legal.NewStore(stubDB{}, stubTrail{})
	if err != nil {
		t.Fatalf("NewStore with both dependencies: %v", err)
	}
	// AND IT STARTS EMPTY, which is the state the pages rendered in before this
	// package existed: every document unpublished, every page honest about it.
	if n := len(s.Published()); n != 0 {
		t.Errorf("a fresh Store reports %d published documents. It must start empty — a "+
			"snapshot that claimed a text before one was read would put an unpublished "+
			"policy on a public page.", n)
	}
}
