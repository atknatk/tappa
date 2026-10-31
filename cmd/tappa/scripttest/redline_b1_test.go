package scripttest

import (
	"strings"
	"testing"
)

// TestRedline_B1_TheProductNameInWhatUsersRead pins redline-check.sh's B1 (backlog T73): the
// user-facing brand is Taptime, tappa is the internal code name, and the capitalised product name
// "Tappa" must not reach the text a user reads. Two trees, as T139 asks of these tests (one run of
// the script costs about a second): every leak side by side, each in a file of its own and each a
// surface B1 names, and every exemption side by side the same way.
//
// The red tree must exit non-zero with a B1 FAIL naming every file:line below; the green tree must
// exit 0 with no B1 FAIL at all.
func TestRedline_B1_TheProductNameInWhatUsersRead(t *testing.T) {
	t.Parallel()

	const tpl = "package pages\n\ntempl ZZ(row Row) {\n" // the body starts on line 4
	long := strings.Repeat("a long sentence ", 20)       // 320 characters before the name
	red := map[string]string{
		// .templ text, an expression's string, a possessive, and an attribute's value.
		"web/templates/pages/zz_text.templ": tpl + "\t<p>Welcome to Tappa</p>\n}\n",
		"web/templates/pages/zz_expr.templ": tpl + "\t<p>{ \"Tappa\" }</p>\n}\n",
		"web/templates/pages/zz_poss.templ": tpl + "\t<p>It is Tappa's plaque.</p>\n}\n",
		"web/templates/pages/zz_attr.templ": tpl + "\t<img alt=\"Tappa\"/>\n}\n",
		// A served surface has no comments: templ writes an HTML comment to the client, and
		// a `//` or `/* */` inside a text node is printed on the screen.
		"web/templates/pages/zz_htmlc.templ": tpl + "\t<!-- Tappa -->\n}\n",
		"web/templates/pages/zz_slash.templ": tpl + "\t<p>Fast // Tappa</p>\n}\n",
		"web/templates/pages/zz_block.templ": tpl + "\t<p>b /* Tappa */ c</p>\n}\n",
		// The Go regions of a .templ: their strings are read.
		"web/templates/pages/zz_gostr.templ": "package pages\n\nvar zz = \"Tappa\"\n",
		"web/templates/pages/zz_call.templ":  tpl + "\t@zzCell(Row{Name: \"Tappa\"})\n}\n",
		"web/templates/pages/zz_multi.templ": tpl + "\t@zzShell(\"x\",\n\t\t\"Tappa\") {\n\t\t<p>ok</p>\n\t}\n}\n",
		// A call's children are text again.
		"web/templates/pages/zz_kids.templ": tpl + "\t@zzShell(\"x\") {\n\t\t<p>Tappa</p>\n\t}\n}\n",
		// A line that starts with a Go keyword is TEXT unless it is templ's own control
		// line: templ prints these four (measured, templ v0.3.833 generate).
		"web/templates/pages/zz_for.templ":     tpl + "\t<p>\n\t\tfor every business, Tappa decides\n\t</p>\n}\n",
		"web/templates/pages/zz_default.templ": tpl + "\t<p>\n\t\tdefault · Tappa answers\n\t</p>\n}\n",
		"web/templates/pages/zz_case.templ":    tpl + "\t<p>\n\t\tcase closed, Tappa wins\n\t</p>\n}\n",
		"web/templates/pages/zz_else.templ":    tpl + "\t<p>\n\t\telse Tappa\n\t</p>\n}\n",
		// A real control line is Go: its strings are read.
		"web/templates/pages/zz_ctl.templ": tpl + "\tfor _, x := range zzList(\"Tappa\") {\n\t\t<p>{ x }</p>\n\t}\n" +
			"\tswitch row.Kind {\n\tcase \"Tappa\":\n\t\t<p>a</p>\n\tdefault:\n\t\t<p>b</p>\n\t}\n}\n",
		// Only Android XML is compiled into an app; any other .xml is served as it is.
		"web/static/zz.xml":                  "<!-- Tappa -->\n<x/>\n",
		"web/templates/pages/zz_view.go":     "package pages\n\nvar zz = \"One plaque, Tappa\"\n",
		"internal/handler/zz_sentence.go":    "package handler\n\nfunc zz() string {\n\treturn \"Tappa could not save that\"\n}\n",
		"internal/handler/zz_raw.go":         "package handler\n\nconst zz = `first line\nsecond line names Tappa`\n",
		"internal/handler/zz_long.go":        "package handler\n\nconst zz = \"" + long + "Tappa\"\n",
		"internal/mail/zz_subject.go":        "package mail\n\nconst zz = \"Your Tappa invite\"\n",
		"web/static/js/zz.js":                "const zz = 'Tappa says hi';\n",
		"web/static/zz.html":                 "<!doctype html><p>Tappa</p>\n",
		"web/static/zz_c.html":               "<!-- Tappa -->\n",
		"docs/api/zz.md":                     "# The Tappa API\n",
		"docs/api/zz_c.md":                   "<!-- Tappa -->\n",
		"internal/handler/api/zz.yaml":       "info:\n  title: Tappa feed\n",
		"internal/handler/api/zz_c.yaml":     "info:\n  title: Taptime # Tappa\n",
		"internal/handler/api/zz_blk.yaml":   "info:\n  description: |\n    one\n    # Tappa\n",
		"internal/sandbox/zz.json":           "{\"title\": \"Tappa sandbox\"}\n",
		"android/app/src/main/zz/Wording.kt": "package zz\n\nval zz = \"Tappa relay\"\n",
		"android/app/src/main/zz/Raw.kt":     "package zz\n\nval zz = \"\"\"\nfirst\nTappa second\n\"\"\"\n",
		"android/app/src/main/res/zz.xml":    "<resources>\n  <string name=\"zz\">Tappa</string>\n</resources>\n",
	}
	want := []string{
		"web/templates/pages/zz_text.templ:4:",
		"web/templates/pages/zz_expr.templ:4:",
		"web/templates/pages/zz_poss.templ:4:",
		"web/templates/pages/zz_attr.templ:4:",
		"web/templates/pages/zz_htmlc.templ:4:",
		"web/templates/pages/zz_slash.templ:4:",
		"web/templates/pages/zz_block.templ:4:",
		"web/templates/pages/zz_gostr.templ:3:",
		"web/templates/pages/zz_call.templ:4:",
		"web/templates/pages/zz_multi.templ:5:",
		"web/templates/pages/zz_kids.templ:5:",
		"web/templates/pages/zz_for.templ:5:",
		"web/templates/pages/zz_default.templ:5:",
		"web/templates/pages/zz_case.templ:5:",
		"web/templates/pages/zz_else.templ:5:",
		"web/templates/pages/zz_ctl.templ:4:",
		"web/templates/pages/zz_ctl.templ:8:",
		"web/static/zz.xml:1:",
		"web/templates/pages/zz_view.go:3:",
		"internal/handler/zz_sentence.go:4:",
		"internal/handler/zz_raw.go:3:",
		"internal/handler/zz_long.go:3:",
		"internal/mail/zz_subject.go:3:",
		"web/static/js/zz.js:1:",
		"web/static/zz.html:1:",
		"web/static/zz_c.html:1:",
		"docs/api/zz.md:1:",
		"docs/api/zz_c.md:1:",
		"internal/handler/api/zz.yaml:2:",
		"internal/handler/api/zz_c.yaml:2:",
		"internal/handler/api/zz_blk.yaml:4:",
		"internal/sandbox/zz.json:1:",
		"android/app/src/main/zz/Wording.kt:3:",
		"android/app/src/main/zz/Raw.kt:3:",
		"android/app/src/main/res/zz.xml:2:",
	}

	green := map[string]string{
		// The code name and the identifiers that carry it: a CSS token, a selector, an env name, a
		// role, a module path, a struct field and a longer identifier -- in a .templ's Go regions
		// (declarations, statements, expressions, call arguments) as in a .go file.
		"web/templates/pages/zz_ok.templ": "package pages\n\nimport \"github.com/atknatk/tappa/internal/brand\"\n\n" +
			"// Tappa draws this in its own colour.\n/* Tappa\n   again */\ntype zzRow struct { Tappa string }\n\n" +
			"templ ZZ(row Row) {\n" +
			"\t// Tappa, a templ comment\n\t/* Tappa,\n\t   a block one */\n" +
			"\tif row.Tappa != \"\" {\n\t\t<span class=\"text-tappa-green\">{ row.Tappa }</span>\n\t}\n" +
			"\t@zzCell(ComparisonRow{Tappa: \"x\"})\n\t<p>Taptime { zzLabel(Tappa) }</p>\n" +
			"\tfor _, x := range Tappa {\n\t\t<p>{ x }</p>\n\t}\n\tswitch Tappa {\n\tcase Tappa:\n\t\t<p>a</p>\n" +
			"\tdefault:\n\t\t<p>b</p>\n\t}\n\tif x {\n\t\t<p>c</p>\n\t} else if Tappa {\n\t\t<p>d</p>\n" +
			"\t} else {\n\t\t<p>e</p>\n\t}\n}\n\nfunc zz() string { return \"x\" } // Tappa\n",
		"internal/handler/zz_ok.go": "package handler\n\n// Tappa maintains the \"Tappa\" rule.\n/* the \"Tappa\"\n   plaque */\n" +
			"type Row struct{ Tappa string }\n\nfunc zz(r Row) string {\n\t_ = r.Tappa\n" +
			"\t_ = \"TAPPA_DEBOUNCE_SECONDS\" + \"tappa_app\" + \"TappaGreen\" + \"tappa\"\n" +
			"\treturn \"Taptime\" // Tappa\n}\n",
		"web/static/js/zz_ok.js":             "// the \"Tappa\" plaque\n/* 'Tappa' */\nconst zz = 'Taptime';\n",
		"web/static/css/zz_ok.css":           "/* kitchen docket: Tappa's motif */\n.zz { color: red; }\n",
		"android/app/src/main/zz/Ok.kt":      "package zz\n\n// the \"Tappa\" relay\n/* \"Tappa\" */\nval zz = \"Taptime\"\n",
		"android/app/src/main/res/zz_ok.xml": "<resources>\n  <!-- Tappa -->\n  <string name=\"zz\">Taptime</string>\n</resources>\n",
		// Outside B1's surfaces: tests, test data, generated output, third-party code, internal
		// documents, and the operator CLI.
		"internal/handler/zz_test.go":           "package handler\n\nconst zz = \"Tappa\"\n",
		"internal/handler/testdata/zz.golden":   "<p>Tappa</p>\n",
		"web/templates/pages/testdata/zz.templ": "<p>Tappa</p>\n",
		"web/templates/pages/zz_templ.go":       "package pages\n\nconst zz = \"Tappa\"\n",
		"internal/store/zz.go":                  "package store\n\nconst zz = \"Tappa\"\n",
		"web/static/vendor/zz.js":               "var zz = \"Tappa\";\n",
		"docs/plan/zz.md":                       "Tappa\n",
		"cmd/zz/main.go":                        "package main\n\nconst zz = \"Tappa\"\n",
	}

	var redOut, greenOut string
	var redCode, greenCode int
	redOut, redCode = redlineRun(t, red)
	greenOut, greenCode = redlineRun(t, green)

	redFail := failSection(redOut, "B1")
	if redCode == 0 {
		t.Errorf("a tree that writes Tappa on every user-facing surface exited 0:\n%s", redOut)
	}
	for _, w := range want {
		if !strings.Contains(redFail, w) {
			t.Errorf("B1 does not report %s; FAIL section:\n%s", w, redFail)
		}
	}
	// A long line prints the match's surroundings, not its first 200 characters.
	if at := strings.Index(redFail, "internal/handler/zz_long.go:3:"); at < 0 ||
		!strings.Contains(strings.SplitN(redFail[at:], "\n", 2)[0], "Tappa") {
		t.Errorf("the long line's finding does not show the name it found:\n%s", redFail)
	}
	if n := strings.Count(redFail, "\n    "); n != len(want) {
		t.Errorf("B1 reported %d finding(s), want %d (one per leak):\n%s", n, len(want), redFail)
	}
	if greenCode != 0 || failSection(greenOut, "B1") != "" {
		t.Errorf("the exemptions' tree: want exit 0 and no B1 FAIL, got exit %d:\n%s", greenCode, greenOut)
	}
}
