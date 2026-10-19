package main

// readme_blocks_test.go — backlog T136 (OPS-1, 2026-10-18). The user's shell is zsh, where
// interactive_comments is OFF: a `#` line pasted into it is a COMMAND named `#` (a `>` in it
// creates a file, a `;` or `|` runs what follows, an apostrophe opens quote> and swallows the
// next line), and the words of a comment at the end of a command become that command's
// arguments. deploy/README.md's pasteable blocks carried 258 comment lines and 61 inline comments
// in 79 of 128 blocks (32 blocks did not even parse under `zsh -f -i -n`); only operator step 5
// was held to anything. The comments were moved out of the blocks into the prose around them —
// the commands themselves were not touched (the rounds' reports carry the proof) — and this
// file holds the whole file to it. (OPS-1's first round counted 119 blocks: its fence pattern
// knew indents but not blockquotes, so the 9 blocks inside a `> ` quote — rollback, backup,
// outage triage — went unscanned and 8 of them kept their comments. The fix round taught
// markdownBlocks the `> ` prefix and added the control that at least one such block is seen.)
//
// THE RULE, ONE FOR BOTH SHELLS: a ```bash block of deploy/README.md carries NO comment — no `#`
// line and no `# …` after a command. An explanation lives in the text, an expected output in a
// ```text block. The fences that are not for the shell (bare, text, sql, ini, yaml) are not
// held; any other language — sh, zsh, console, or a word nobody listed — is red, so a pasteable
// block cannot slip past the rule under another name.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// readmeBlock is one fenced block of a markdown file: its 1-based opening line, its body prefix
// (the indent, any blockquote markers, and the width of any list marker, before the fence), its
// info string and its body with that prefix removed.
type readmeBlock struct {
	line   int
	indent string
	quoted bool
	info   string
	body   []string
}

// lang is the first word of the info string (`bash title="x"` -> `bash`), the word a renderer
// and a reader take for the block's language; "" for a bare fence.
func (b readmeBlock) lang() string {
	if f := strings.Fields(b.info); len(f) > 0 {
		return f[0]
	}
	return ""
}

// listMarker matches a list item marker and the spaces after it: `-`, `*`, `+`, `N.` or `N)`.
var listMarker = regexp.MustCompile(`^(?:[-*+]|[0-9]{1,9}[.)]) +`)

// markdownFence reads one line as a fence. The prefix is any nesting of spaces, `>` blockquote
// markers (each with an optional space) and list item markers (`> - ```bash`, `1. ```bash`,
// `  > > ```bash`); the fence is 3+ backticks or 3+ tildes. cont is the prefix the block's other
// lines carry — a list marker's width becomes spaces. A backtick fence's info string holds no
// backtick (CommonMark: "```bash``` is inline code"). OPS-1's first round matched only spaces
// (9 quoted blocks unscanned, 8 with comments); its re-audit added `~~~` and the list marker,
// which the backtick-and-spaces pattern read as prose.
func markdownFence(line string) (cont string, ch byte, n int, info string, ok bool) {
	var pre []byte
	i := 0
prefix:
	for i < len(line) {
		switch {
		case line[i] == ' ':
			pre = append(pre, ' ')
			i++
		case line[i] == '>':
			pre = append(pre, '>')
			i++
			if i < len(line) && line[i] == ' ' {
				pre = append(pre, ' ')
				i++
			}
		default:
			m := listMarker.FindString(line[i:])
			if m == "" {
				break prefix
			}
			pre = append(pre, strings.Repeat(" ", len(m))...)
			i += len(m)
		}
	}
	if i >= len(line) || (line[i] != '`' && line[i] != '~') {
		return "", 0, 0, "", false
	}
	ch = line[i]
	for n = 0; i+n < len(line) && line[i+n] == ch; n++ {
	}
	info = line[i+n:]
	if n < 3 || (ch == '`' && strings.ContainsRune(info, '`')) {
		return "", 0, 0, "", false
	}
	return string(pre), ch, n, strings.TrimSpace(info), true
}

// markdownBlocks returns every fenced block of text — at any indent and inside a list item (a
// block there is pasted like any other), inside a blockquote (its `> ` markers are not part of
// the command), with either fence character and any fence length (a ````bash block may show ```
// in its body). A block closes on a bare fence of the same character, at least as long.
func markdownBlocks(text string) []readmeBlock {
	lines := strings.Split(text, "\n")
	var out []readmeBlock
	for i := 0; i < len(lines); i++ {
		ind, ch, n, info, ok := markdownFence(lines[i])
		if !ok {
			continue
		}
		j := i + 1
		for ; j < len(lines); j++ {
			if _, cch, cn, cinfo, cok := markdownFence(lines[j]); cok && cch == ch && cn >= n && cinfo == "" {
				break
			}
		}
		b := readmeBlock{line: i + 1, indent: ind, quoted: strings.Contains(ind, ">"), info: info}
		for _, l := range lines[i+1 : min(j, len(lines))] {
			// An empty line of a quoted block is written `>` (the marker without its space).
			if b.quoted && !strings.HasPrefix(l, ind) && strings.HasPrefix(l, strings.TrimRight(ind, " ")) {
				l = ind + strings.TrimPrefix(l, strings.TrimRight(ind, " "))
			}
			b.body = append(b.body, strings.TrimPrefix(l, ind))
		}
		out = append(out, b)
		i = j
	}
	return out
}

// TestMarkdownBlocks is the reader's own table: each row is a document holding ONE block, and
// the language, body prefix and first body line the reader must find in it.
func TestMarkdownBlocks(t *testing.T) {
	for _, tc := range []struct {
		name, doc, lang, indent, body string
	}{
		{"a backtick fence", "```bash\necho a\n```", "bash", "", "echo a"},
		{"a tilde fence", "~~~bash\necho a\n~~~", "bash", "", "echo a"},
		{"a tilde fence is not closed by backticks", "~~~bash\n```\necho a\n~~~", "bash", "", "```"},
		{"a longer fence is not closed by a shorter one", "````bash\n```\n````", "bash", "", "```"},
		{"an info string's first word", "```bash title=\"x\"\necho a\n```", "bash", "", "echo a"},
		{"a bare fence", "```\nout\n```", "", "", "out"},
		{"an indented fence", "   ```bash\n   echo a\n   ```", "bash", "   ", "echo a"},
		{"a list item's fence", "1. ```bash\n   echo a\n   ```", "bash", "   ", "echo a"},
		{"a `N)` list item's fence", "2) ```bash\n   echo a\n   ```", "bash", "   ", "echo a"},
		{"a bullet's fence", "- ```bash\n  echo a\n  ```", "bash", "  ", "echo a"},
		{"a quoted fence", "> ```bash\n> echo a\n> ```", "bash", "> ", "echo a"},
		{"a list item in a quote", "> - ```bash\n>   echo a\n>   ```", "bash", ">   ", "echo a"},
		{"a quote in a list item", "- > ```bash\n  > echo a\n  > ```", "bash", "  > ", "echo a"},
		{"inline code is not a fence", "```bash``` is inline\n- ```x```\n```sh\necho a\n```", "sh", "", "echo a"},
	} {
		got := markdownBlocks(tc.doc)
		if len(got) != 1 {
			t.Errorf("%s: %d blocks, want 1: %+v", tc.name, len(got), got)
			continue
		}
		b := got[0]
		if b.lang() != tc.lang || b.indent != tc.indent || len(b.body) == 0 || b.body[0] != tc.body {
			t.Errorf("%s: lang %q indent %q body %q, want %q %q %q", tc.name, b.lang(), b.indent, b.body, tc.lang, tc.indent, tc.body)
		}
	}
}

// readmeBlockLangs is every fence language deploy/README.md may use, and whether the block is
// pasted into a shell. MEASURED (OPS-1 re-audit, 2026-10-18): 184 blocks — 167 bash, 8 text,
// 2 sql, 1 ini, 6 bare. yaml is in no block today; it is listed because it is a manifest, not
// a shell. Every other word is RED: a block this table does not know is a block nobody decided
// about, and sh/shell/zsh/console are pasteable shells under another name — the rule above
// would not see them.
var readmeBlockLangs = map[string]bool{"bash": true, "text": false, "sql": false, "ini": false, "yaml": false, "": false}

// readmeShellLangs are the words a pasteable shell block is written with instead of `bash`.
var readmeShellLangs = map[string]bool{"sh": true, "shell": true, "zsh": true, "console": true, "shell-session": true}

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
// deploy/README.md (any indent, inside a blockquote, any fence length): (a) no line is a comment and no command line
// ends in one (classifyShell: a `#` inside a quote, a heredoc or a word is not a comment); (b)
// where zsh is installed, the block — placeholders <…> neutralised — is parsed by `zsh -f -i -n`
// (interactive, so interactive_comments is OFF, and -n, so nothing runs) in an empty directory
// with PATH=/nonexistent: exit 0, no "parse error", no "unmatched", and no file created.
// A fence whose language is not in readmeBlockLangs is red, sh/shell/zsh/console included.
// CONTROLS: at least 165 blocks, at least 4 of them indented, at least 1 inside a blockquote, a
// heredoc and a quoted continuation line were seen (the lexer was exercised); the zsh probe refuses a block whose comment carries
// an apostrophe and accepts the same block without it (it can see what it exists for).
// PART II — pinned: (a) everywhere; (b) where zsh is installed (skipped with a reason otherwise).
// PART III — what a block DOES is not judged here; operator step 5's blocks are also RUN
// (TestReadme_TheTailnetStepBlocksPasteIntoZsh). Fences other than bash are not for the shell.
//
// MUTANT (measured, OPS-1): a `# x` line put back into any block, an inline `# x` after a command
// in an indented block, a ```bash block whose comment holds an apostrophe -> red. Fix round:
// `> # it's a note -> Settings` put into a quoted block -> red twice (the comment rule and the
// zsh parse); markdownFence reverted to spaces only -> red (CONTROL: 0 inside a blockquote).
// Re-audit round (each appended to the file's end, each with a `# x` line unless said): a ~~~bash
// block, a ```bash title="x" block, a `1. ```bash` and a `> - ```bash` block -> red (comment
// line); ```sh / ```zsh / ```console, and ```sh with no comment -> red (shell under another
// name); ```python -> red (unknown language). In the reader: tilde fences, list markers, the
// same-character close or the info's first word removed -> TestMarkdownBlocks red.
func TestRunbook_EveryPasteableBlockCarriesNoComment(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	var blocks []readmeBlock
	indented, inQuote, heredoc, quoted, lines := 0, 0, 0, 0, 0
	all := markdownBlocks(string(b))
	langs := map[string]int{}
	for _, blk := range all {
		lang := blk.lang()
		langs[lang]++
		shell, known := readmeBlockLangs[lang]
		switch {
		case readmeShellLangs[lang]:
			t.Errorf("deploy/README.md:%d: a ```%s block is pasted into a shell but is not held to the no-comment "+
				"rule; write it as ```bash (and keep comments in the text around it)", blk.line, lang)
			continue
		case !known:
			t.Errorf("deploy/README.md:%d: unknown fence language %q. If the block is pasted into a shell, write "+
				"```bash; if it is output, write ```text; if it is neither, add the word to readmeBlockLangs "+
				"with the reason", blk.line, lang)
			continue
		case !shell:
			continue
		}
		blocks = append(blocks, blk)
		switch {
		case blk.quoted:
			inQuote++
		case blk.indent != "":
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
	if len(blocks) < 165 || indented < 4 || inQuote < 1 || heredoc == 0 || quoted == 0 {
		t.Fatalf("CONTROL FAILED: %d bash blocks (%d indented, %d inside a blockquote), %d heredoc and %d "+
			"quoted-continuation lines; want at least 165 blocks, 4 indented, 1 inside a blockquote, and both "+
			"kinds of line seen", len(blocks), indented, inQuote, heredoc, quoted)
	}
	t.Logf("%d fenced blocks %v; %d pasteable bash blocks (%d indented, %d inside a blockquote), %d lines, no comment",
		len(all), langs, len(blocks), indented, inQuote, lines)

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

// readCall finds a `read` in command position — at the start of a line or after `;`, `&`, `|`,
// `(`, `{`, a backquote, `$(`, a `-c '` / `-c "` (a shell started on a string), or the keywords
// do, then, else, while, until, if and `!`, with any `VAR=value` assignments in front (`IFS=`).
// Group 1 is its option words (` -rs`). The word `read` in a message ("did not read $I") is not
// in command position, so it is not a call; whatever prints the prompt (printf '…', printf "…",
// echo, nothing) does not matter.
var readCall = regexp.MustCompile("(?:^|[;&|({`]|\\$\\(|-c\\s+['\"]|(?:^|\\s)(?:do|then|else|while|until|if|!)\\s)" +
	`\s*(?:[A-Za-z_][A-Za-z0-9_]*=\S*\s+)*read((?:\s+-[A-Za-z]+)*)(?:\s|;|$)`)

// readmeReadsThatShow is every `read` of a pasteable block that may run without `-s`, each named
// by its WHOLE line (not a line number: an edit of the line drops it from the list and the read is
// held again) and the reason its input may show. An entry no unhidden read matches is stale and
// red — the list cannot outlive what it excuses.
var readmeReadsThatShow = map[string]string{
	// SigNoz alert rules: stdin is a pipe from a file in this repository, not a terminal, and the
	// rules are not secret (they are the file itself).
	`jq -c '.[]' deploy/observability/signoz-alert-rules.json | while IFS= read -r RULE; do`: "reads the repository's own alert rules from a pipe",
	// The Harbor user is a name (robot$tappa-ci or an admin's login), not a credential; its
	// secret is read on the next line with -s.
	`printf 'Harbor user (robot$tappa-ci or a project admin): '; read -r U`: "reads a user name; its secret is the next read, with -s",
	// The old KEK: echo is turned off by stty around the read, because `read -s -p` means
	// "read from the coprocess" in zsh (measured in the KEK rotation section).
	`stty -echo; IFS= read -r K; stty echo; printf '\n'`: "stty -echo hides the input (POSIX, the same in bash and zsh)",
}

// TestRunbook_EveryReadInAPasteableBlockHidesWhatIsTyped (OPS-2 low round; the auditor's LOW 2).
// Over EVERY ```bash block of deploy/README.md (markdownBlocks: any indent, list item or
// blockquote), every `read` in command position (readCall — whatever prints the prompt, single
// or double quoted, echo or nothing) carries `-s`, or its whole line is in readmeReadsThatShow
// with its reason. The earlier pin saw only a `printf '…'; read` whose prompt named a secret:
// a `printf "secret: "; read S` or an `echo token:; read T` went past it. Stdin in the RUN tests
// is a pipe, where an echoing read shows nothing — only a static pin sees a dropped `-s`.
// CONTROLS: readCall's own table (a message holding the word `read` is not a call; a read after
// a double-quoted prompt, after echo, in a while loop and inside `sh -c '…'` are); at least 17
// reads seen, at least 14 of them with -s (measured: 17, 14); every allow-list entry used exactly once.
//
// MUTANT (measured, OPS-2 low round): `printf "k8s secret: "; read S` added to a bash block ->
// red; `echo 'token:'; read T` -> red; an entry deleted from readmeReadsThatShow -> red (its read
// is unexcused); an entry for a line the README does not have -> red (stale).
func TestRunbook_EveryReadInAPasteableBlockHidesWhatIsTyped(t *testing.T) {
	for _, tc := range []struct {
		line  string
		calls []string // the option words of each call found
	}{
		{`printf 'secret: '; read -rs S; echo`, []string{" -rs"}},
		{`printf "secret: "; read S`, []string{""}},
		{`echo 'token:'; read T`, []string{""}},
		{`echo "did not read $I, or $V failed"`, nil},
		{`jq -c . f | while IFS= read -r RULE; do`, []string{" -r"}},
		{`sh -c 'read -rs X; printf %s "$X"'`, []string{" -rs"}},
		{`stty -echo; IFS= read -r K; stty echo`, []string{" -r"}},
		{`X=$(read -r Y)`, []string{" -r"}},
		{`echo spread; reader x; read`, []string{""}},
	} {
		var got []string
		for _, m := range readCall.FindAllStringSubmatch(tc.line, -1) {
			got = append(got, m[1])
		}
		if fmt.Sprint(got) != fmt.Sprint(tc.calls) {
			t.Errorf("CONTROL FAILED: readCall on %q found %q, want %q", tc.line, got, tc.calls)
		}
	}

	b, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	used := map[string]int{}
	reads, hidden := 0, 0
	for _, blk := range markdownBlocks(string(b)) {
		if lang := blk.lang(); !readmeBlockLangs[lang] && !readmeShellLangs[lang] {
			continue
		}
		for i, sl := range classifyShell(blk.body) {
			if sl.kind == shComment || sl.kind == shBlank {
				continue
			}
			line := strings.TrimSpace(blk.body[i])
			for _, m := range readCall.FindAllStringSubmatch(line, -1) {
				reads++
				if strings.Contains(m[1], "s") {
					hidden++
					continue
				}
				if _, ok := readmeReadsThatShow[line]; ok {
					used[line]++
					continue
				}
				t.Errorf("deploy/README.md:%d: a read without -s (what is typed or pasted shows on the screen). Write "+
					"read -rs; if the value is not secret, add the whole line to readmeReadsThatShow with the reason:\n  %s",
					blk.line+1+i, line)
			}
		}
	}
	for line, why := range readmeReadsThatShow {
		if used[line] != 1 {
			t.Errorf("readmeReadsThatShow: the entry %q (%s) excuses %d unhidden read(s) in deploy/README.md, want "+
				"exactly 1 — a stale entry is removed, not kept", line, why, used[line])
		}
	}
	if reads < 17 || hidden < 14 {
		t.Errorf("CONTROL FAILED: %d read(s) seen in the pasteable blocks, %d with -s; want at least 17 and 14", reads, hidden)
	}
	t.Logf("%d read(s) in the pasteable blocks: %d with -s, %d excused by readmeReadsThatShow", reads, hidden, len(used))
}
