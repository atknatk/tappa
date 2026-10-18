package main

// readme_blocks_test.go — backlog T136 (OPS-1, 2026-10-18). The user's shell is zsh, where
// interactive_comments is OFF: a `#` line pasted into it is a COMMAND named `#` (a `>` in it
// creates a file, a `;` or `|` runs what follows, an apostrophe opens quote> and swallows the
// next line), and the words of a comment at the end of a command become that command's
// arguments. deploy/README.md's pasteable blocks carried 244 comment lines and 56 inline comments
// in 71 of 119 blocks (31 blocks did not even parse under `zsh -f -i -n`); only operator step 5
// was held to anything. The comments were moved out of the blocks into the prose around them —
// the commands themselves were not touched (the round's report carries the proof) — and this
// file holds the whole file to it.
//
// THE RULE, ONE FOR BOTH SHELLS: a ```bash block of deploy/README.md carries NO comment — no `#`
// line and no `# …` after a command. An explanation lives in the text, an expected output in a
// ```text block. Every other fence (bare, text, sql, ini) is not for the shell and is not held.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// readmeBlock is one fenced block of a markdown file: its 1-based opening line, its indent, its
// info string and its body with the indent removed.
type readmeBlock struct {
	line   int
	indent string
	info   string
	body   []string
}

var markdownFence = regexp.MustCompile("^( *)(`{3,})([^`]*)$")

// markdownBlocks returns every fenced block of text — at any indent (a block inside a list item
// is pasted like any other) and with any fence length (a ````bash block may show ``` in its body).
func markdownBlocks(text string) []readmeBlock {
	lines := strings.Split(text, "\n")
	var out []readmeBlock
	for i := 0; i < len(lines); i++ {
		m := markdownFence.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		ind, fence, info := m[1], m[2], strings.TrimSpace(m[3])
		j := i + 1
		for ; j < len(lines); j++ {
			if c := markdownFence.FindStringSubmatch(lines[j]); c != nil && len(c[2]) >= len(fence) && strings.TrimSpace(c[3]) == "" {
				break
			}
		}
		b := readmeBlock{line: i + 1, indent: ind, info: info}
		for _, l := range lines[i+1 : min(j, len(lines))] {
			b.body = append(b.body, strings.TrimPrefix(l, ind))
		}
		out = append(out, b)
		i = j
	}
	return out
}

type shellLineKind int

const (
	shBlank   shellLineKind = iota
	shComment               // the whole line is a comment
	shCode                  // a command line (it may end in an inline comment)
	shHeredoc               // inside a here-document, or its terminator
	shQuoted                // begins inside a quote opened on an earlier line
)

// shellLine is one classified line; inline is the byte offset of an inline comment, or -1.
type shellLine struct {
	kind   shellLineKind
	inline int
}

var heredocOperator = regexp.MustCompile(`^<<(-?)[ \t]*(?:'([^']+)'|"([^"]+)"|\\?([A-Za-z_][A-Za-z0-9_]*))`)

// classifyShell is enough of a shell lexer to tell a comment from text that only looks like one:
// quotes ('…', "…", $'…', `…`) that span lines, here-documents (<<WORD, <<'WORD', <<-WORD) and
// backslash escapes. A `#` starts a comment only at the start of a word outside quotes — the same
// place bash (and zsh with interactive_comments on) reads one.
func classifyShell(lines []string) []shellLine {
	out := make([]shellLine, 0, len(lines))
	quote := ""
	var pending [][2]string // heredoc terminator, "-" when tabs are stripped
	var inDoc *[2]string
	for _, line := range lines {
		if inDoc != nil {
			out = append(out, shellLine{shHeredoc, -1})
			cand := line
			if inDoc[1] == "-" {
				cand = strings.TrimLeft(line, "\t")
			}
			if cand == inDoc[0] {
				inDoc = nil
				if len(pending) > 0 {
					inDoc, pending = &pending[0], pending[1:]
				}
			}
			continue
		}
		kind := shCode
		switch {
		case quote != "":
			kind = shQuoted
		case strings.TrimSpace(line) == "":
			out = append(out, shellLine{shBlank, -1})
			continue
		case strings.HasPrefix(strings.TrimLeft(line, " \t"), "#"):
			out = append(out, shellLine{shComment, -1})
			continue
		}
		inline := -1
	scan:
		for i := 0; i < len(line); i++ {
			c := line[i]
			switch quote {
			case "'":
				if c == '\'' {
					quote = ""
				}
				continue
			case "$'", `"`, "`":
				switch {
				case c == '\\':
					i++
				case quote == "$'" && c == '\'', quote == `"` && c == '"', quote == "`" && c == '`':
					quote = ""
				}
				continue
			}
			switch {
			case c == '\\':
				i++
			case c == '\'':
				quote = "'"
			case c == '$' && i+1 < len(line) && line[i+1] == '\'':
				quote = "$'"
				i++
			case c == '"':
				quote = `"`
			case c == '`':
				quote = "`"
			case c == '#' && (i == 0 || strings.IndexByte(" \t;|&()", line[i-1]) >= 0):
				inline = i
				break scan
			case strings.HasPrefix(line[i:], "<<<"):
				i += 2
			case strings.HasPrefix(line[i:], "<<"):
				if m := heredocOperator.FindStringSubmatch(line[i:]); m != nil {
					pending = append(pending, [2]string{m[2] + m[3] + m[4], m[1]})
					i += len(m[0]) - 1
				}
			}
		}
		out = append(out, shellLine{kind, inline})
		if quote == "" && len(pending) > 0 {
			inDoc, pending = &pending[0], pending[1:]
		}
	}
	return out
}

// TestClassifyShell is the lexer's own table: each row is a block and the kind of its LAST line.
func TestClassifyShell(t *testing.T) {
	for _, tc := range []struct {
		name   string
		block  string
		kind   shellLineKind
		inline bool
	}{
		{"a comment line", "# note", shComment, false},
		{"an indented comment line", "   # note", shComment, false},
		{"an inline comment", "kubectl get sc   # note", shCode, true},
		{"a tab before it", "echo a\t# note", shCode, true},
		{"after a semicolon", "echo a;# note", shCode, true},
		{"# glued to a word is not one", "echo a#b", shCode, false},
		{"${#x} is not one", `echo "${#x}" ${#y}`, shCode, false},
		{"inside double quotes", `echo "rc=$?   # 0 expected"`, shCode, false},
		{"inside single quotes", `grep -c '# start server x'`, shCode, false},
		{"escaped", `echo \# not`, shCode, false},
		{"a heredoc body", "cat <<'PY'\n# a Python comment\nPY", shHeredoc, false},
		{"after the heredoc ends", "cat <<EOF\nx\nEOF\n# a real comment", shComment, false},
		{"a <<- heredoc ends on a tab-indented word", "cat <<-EOF\n\tx\n\tEOF\n# after", shComment, false},
		{"a quote opened on an earlier line", "sh -c 'a \\\n  # inside the quote", shQuoted, false},
		{"after the quote closes", "sh -c 'a\n b'\n# after", shComment, false},
		{"a here-string is not a heredoc", "cat <<< x\n# after", shComment, false},
		{"blank", "echo\n", shBlank, false},
	} {
		got := classifyShell(strings.Split(tc.block, "\n"))
		last := got[len(got)-1]
		if last.kind != tc.kind || (last.inline >= 0) != tc.inline {
			t.Errorf("%s: last line %+v, want kind %d inline %v", tc.name, last, tc.kind, tc.inline)
		}
	}
}

// readmePlaceholder is a `<word>` the operator replaces before running a line (<HESAP_ID>,
// <damga>, sha-<12hex>); as written it is a redirection, so the parse probe neutralises it.
var readmePlaceholder = regexp.MustCompile(`<[^<>\s]+>`)

// TestRunbook_EveryPasteableBlockCarriesNoComment (T136). PART I — over EVERY ```bash block of
// deploy/README.md (any indent, any fence length): (a) no line is a comment and no command line
// ends in one (classifyShell: a `#` inside a quote, a heredoc or a word is not a comment); (b)
// where zsh is installed, the block — placeholders <…> neutralised — is parsed by `zsh -f -i -n`
// (interactive, so interactive_comments is OFF, and -n, so nothing runs) in an empty directory
// with PATH=/nonexistent: exit 0, no "parse error", no "unmatched", and no file created.
// CONTROLS: at least 150 blocks, at least 4 of them indented, a heredoc and a quoted continuation
// line were seen (the lexer was exercised); the zsh probe refuses a block whose comment carries
// an apostrophe and accepts the same block without it (it can see what it exists for).
// PART II — pinned: (a) everywhere; (b) where zsh is installed (skipped with a reason otherwise).
// PART III — what a block DOES is not judged here; operator step 5's blocks are also RUN
// (TestReadme_TheTailnetStepBlocksPasteIntoZsh). Fences other than bash are not for the shell.
//
// MUTANT (measured, OPS-1): a `# x` line put back into any block, an inline `# x` after a command
// in an indented block, a ```bash block whose comment holds an apostrophe -> red.
func TestRunbook_EveryPasteableBlockCarriesNoComment(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var blocks []readmeBlock
	indented, heredoc, quoted, lines := 0, 0, 0, 0
	for _, blk := range markdownBlocks(string(b)) {
		if blk.info != "bash" {
			continue
		}
		blocks = append(blocks, blk)
		if blk.indent != "" {
			indented++
		}
		for i, sl := range classifyShell(blk.body) {
			lines++
			at := fmt.Sprintf("deploy/README.md:%d", blk.line+1+i)
			switch {
			case sl.kind == shHeredoc:
				heredoc++
			case sl.kind == shQuoted:
				quoted++
			case sl.kind == shComment:
				t.Errorf("%s: a comment line inside a pasteable block; in zsh it is a command named `#`. Move it "+
					"into the text around the block:\n  %s", at, strings.TrimSpace(blk.body[i]))
			case sl.kind == shCode && sl.inline >= 0:
				t.Errorf("%s: a command ends in a comment; in zsh its words become the command's arguments. "+
					"Move it into the text around the block:\n  %s", at, strings.TrimSpace(blk.body[i]))
			}
		}
	}
	if len(blocks) < 150 || indented < 4 || heredoc == 0 || quoted == 0 {
		t.Fatalf("CONTROL FAILED: %d bash blocks (%d indented), %d heredoc and %d quoted-continuation lines; "+
			"want at least 150 blocks, 4 indented, and both kinds of line seen", len(blocks), indented, heredoc, quoted)
	}
	t.Logf("%d pasteable bash blocks (%d indented), %d lines, no comment", len(blocks), indented, lines)

	zsh, err := exec.LookPath("zsh")
	if err != nil {
		t.Skipf("zsh is not installed (%v): the comment rule above ran; the zsh parse did not", err)
	}
	parses := func(body string) (bool, string) {
		dir := t.TempDir()
		cmd := exec.CommandContext(t.Context(), zsh, "-f", "-i", "-n")
		cmd.Stdin = strings.NewReader(body)
		cmd.Env = []string{"PATH=/nonexistent", "HOME=" + t.TempDir(), "TERM=dumb"}
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		left, _ := os.ReadDir(dir)
		bad := err != nil || len(left) > 0
		for _, w := range []string{"parse error", "unmatched", "bad pattern"} {
			bad = bad || strings.Contains(string(out), w)
		}
		return !bad, string(out)
	}
	if ok, out := parses("echo ok\n"); !ok {
		t.Fatalf("CONTROL FAILED: zsh -f -i -n refuses a plain `echo ok`:\n%s", out)
	}
	if ok, _ := parses("# it's a note\necho ok\n"); ok {
		t.Fatal("CONTROL FAILED: zsh -f -i -n parsed a comment holding an apostrophe; the probe cannot see what it exists for")
	}
	for _, blk := range blocks {
		body := readmePlaceholder.ReplaceAllString(strings.Join(blk.body, "\n"), "PH") + "\n"
		if ok, out := parses(body); !ok {
			t.Errorf("deploy/README.md:%d: zsh (interactive_comments off) does not parse this block:\n%s", blk.line, out)
		}
	}
}
