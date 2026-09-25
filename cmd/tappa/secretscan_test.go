package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unicode/utf16"
)

// secretscan_test.go — M10 F0-5 (Olay A-0): redline-check.sh R7d and the pre-push
// hook share ONE classifier, scripts/secretscan.sh. These tests pin it so that an
// edit to a pattern cannot silently turn the net off — the redline rules before it
// were only ever checked by hand-run mutations recorded in comments.
//
// 🔴 EVERY SYNTHETIC VALUE BELOW IS ASSEMBLED FROM FRAGMENTS AT RUN TIME. R7d scans
// this file like every other committed file; a literal fake secret here would be
// caught by the very rule it tests (and waiving it would teach the table to forgive
// test files). Each fragment is too short or too plain to fire on its own, and no
// source line carries both a trigger word and a quoted value — which is why a
// redline-check run over this file stays clean (measured).
//
// The classifier runs under LC_ALL=C, so BSD awk (macOS) and mawk (Ubuntu CI) read
// the same bytes the same way; these tests hold that on whichever awk runs them.
var (
	r7dPW   = "Xq7" + "!mR" + "2#vT" + "9pL"
	r7dPW3  = "xq7" + "!mr" + "2#vt" + "9pl" // exactly THREE classes: no upper case
	r7dAKIA = "AK" + "IA" + strings.Repeat("Q7", 8)
	r7dPEM  = strings.Repeat("-", 5) + "BEGIN OPENSSH PRIVATE" + " KEY" + strings.Repeat("-", 5)
	r7dBC   = "$2" + "b$10$" + strings.Repeat("aB3.", 13) + "x"
	r7dTrig = "par" + "ola"
	r7dBT   = "`"
	// 36 hex digits: the shape of `openssl rand -hex 18`, two character classes only.
	r7dHex = hex.EncodeToString([]byte("r7d synthetic hex!"))
	// Provider-prefixed tokens; each body has more than four distinct characters.
	r7dBody = strings.Repeat("aB3dE7", 7)
	r7dGH   = "gh" + "p_" + r7dBody[:36]
	r7dPAT  = "github" + "_pat_" + r7dBody[:30]
	r7dSK   = "sk" + "_live_" + r7dBody[:24]
	r7dXOX  = "xo" + "xb-" + "1234567890-" + r7dBody[:12]
	r7dGL   = "gl" + "pat-" + r7dBody[:20]
	r7dAIza = "AI" + "za" + r7dBody[:35]
	r7dJWT  = "ey" + "J" + r7dBody[:12] + "." + "ey" + "J" + r7dBody[:14] + "." + r7dBody[:20]
	// 44 letters and digits with no symbol: the shape of an SES SMTP password (or any
	// base64 key without `+`/`/`), which an identifier filter used to wave through.
	r7dAlnum = base62(44)
)

// base62 returns n pseudo-random letters and digits (a fixed walk, so the test is
// deterministic) whose first eight characters do not recur — i.e. not a fill pattern.
func base62(n int) string {
	const a = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	b := make([]byte, n)
	x := 7
	for i := range b {
		x = (x*31 + 17) % 61
		b[i] = a[(x+i*7)%62]
	}
	return string(b)
}

// rec builds one classifier record: path US line US text (secretscan.sh R7D_SEP).
func rec(path string, no int, text string) string {
	return path + "\x1f" + strconv.Itoa(no) + "\x1f" + text
}

// r7dSelect feeds records to r7d_select and returns stdout. A non-zero exit is a
// test failure, never an empty (clean-looking) answer.
func r7dSelect(t *testing.T, mode string, lines ...string) string {
	t.Helper()
	out, errOut, code := r7dRun(mode, lines...)
	if code != 0 {
		t.Fatalf("r7d_select %s exited %d\nstderr: %s", mode, code, errOut)
	}
	return out
}

func r7dRun(mode string, lines ...string) (stdout, stderr string, code int) {
	cmd := exec.Command("bash", "-c", `. scripts/secretscan.sh && r7d_select "$1"`, "r7d", mode)
	cmd.Dir = repoRoot
	cmd.Stdin = strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		code = -1
	}
	return out.String(), errb.String(), code
}

// assertNoValue is the "bulgu metni basilmaz" half: whatever R7d reports, it reports
// a location and a class — never the value it found.
func assertNoValue(t *testing.T, out string) {
	t.Helper()
	for _, v := range []string{r7dPW, r7dPW3, r7dAKIA, r7dBC, r7dHex, r7dGH, r7dJWT, "BEGIN OPENSSH"} {
		if strings.Contains(out, v) {
			t.Fatalf("R7d printed the matched value; its output must be `path:line: [class]` only:\n%s", out)
		}
	}
}

func TestSecretScan_EveryClassFiresOnASyntheticValue(t *testing.T) {
	dq := func(s string) string { return `"` + s + `"` }
	cases := []struct{ name, path, text, class string }{
		{"A-0 shape, Turkish word, backticks", "docs/plan/x.md",
			"operatör " + r7dTrig + "sı " + r7dBT + r7dPW + r7dBT + " — rotate edilmeli", "a0-token"},
		{"A-0 shape, upper case word, double quotes", "docs/plan/x.md",
			strings.ToUpper(r7dTrig) + " " + dq(r7dPW), "a0-token"},
		// N5: every other vector has all four classes, so a 3 -> 4 threshold mutation
		// survived. This one has exactly three.
		{"A-0 shape with exactly three classes", "docs/plan/x.md",
			r7dTrig + " " + r7dBT + r7dPW3 + r7dBT, "a0-token"},
		// N4: mawk lower-cases ASCII only; the dotted capital I stays as it is.
		{"Turkish upper case word (dotted capital I)", "docs/plan/x.md",
			"Ş" + "İFRE: " + dq(r7dPW), "a0-token"},
		// N1: a path with a colon used to be dropped by the `path:line:` parser.
		{"path with a colon", "docs/plan/a:b.md",
			r7dTrig + " " + r7dBT + r7dPW + r7dBT, "a0-token"},
		// 3rd round (a): >=32 letters+digits with all three of upper, lower, digit.
		{"long letters-and-digits value", "docs/plan/x.md",
			"SMTP " + r7dTrig + "sı " + r7dBT + r7dAlnum + r7dBT, "a0-token"},
		{"long hex value (two classes, own sub-shape)", "docs/plan/x.md",
			"DB " + r7dTrig + "sı " + r7dBT + r7dHex + r7dBT, "a0-token"},
		{"URL credential, the F0-5 card's own mutation", "docs/plan/state.md",
			"postgres://u" + ":p@h/db", "url-cred"},
		{"AWS access key id", "internal/x/a.go", "k := " + r7dAKIA, "aws-key"},
		{"PEM private key header", "notes.txt", r7dPEM, "pem-key"},
		{"full bcrypt digest", "db/x.sql", "'" + r7dBC + "'", "bcrypt"},
		{"GitHub token", "docs/x.md", "t " + r7dGH, "known-token"},
		{"GitHub fine-grained token", "docs/x.md", "t " + r7dPAT, "known-token"},
		{"Stripe live key", "docs/x.md", "t " + r7dSK, "known-token"},
		{"Slack token", "docs/x.md", "t " + r7dXOX, "known-token"},
		{"GitLab token", "docs/x.md", "t " + r7dGL, "known-token"},
		{"Google API key", "docs/x.md", "t " + r7dAIza, "known-token"},
		{"JWT", "docs/x.md", "Authorization: Bearer " + r7dJWT, "known-token"},
		{"PGPASSWORD assignment", "scripts/x.sh", "PGPASS" + "WORD=" + r7dPW + " psql", "pw-assign"},
		{"db_pass assignment", "scripts/x.sh", "export DB" + "_PASS=" + r7dPW3, "pw-assign"},
		{"api_key assignment", "scripts/x.sh", "API" + "_KEY=" + r7dPW3, "pw-assign"},
		{"auth-token assignment", "scripts/x.sh", "--auth" + "-token=" + r7dPW3, "pw-assign"},
		{"SQL PASSWORD literal", "db/x.sql", "ALTER ROLE r LOGIN PASS" + "WORD '" + "Zq9m2k';", "pw-assign"},
		{"SQL IDENTIFIED BY literal", "db/x.sql", "CREATE USER r IDENTI" + "FIED BY '" + "Zq9m2k';", "pw-assign"},
		{"curl -u credential", "scripts/x.sh", "cu" + "rl -u ops:" + r7dPW3 + " https://h/x", "pw-assign"},
		{"YAML password key", "deploy/x.yaml", "  DB_PASS" + "WORD: s3cr3tval", "pw-assign"},
		{"YAML hyphenated key", "deploy/x.yaml", "  db-pass" + "word: s3cr3tval", "pw-assign"},
		{"YAML api-key", "deploy/x.yml", "  api" + "-key: " + r7dPW3, "pw-assign"},
		{"markdown key: value", "docs/x.md", "- pass" + "word: " + r7dPW3, "pw-assign"},
		// Quoted after a trigger word, so the A-0 shape fires on the same line too.
		{"toml key = value", "config/x.toml", "pass" + "word = " + dq(r7dPW3), "pw-assign" + "," + "a0-token"},
		// 4th round, N2: a long letters-and-digits value is tried before the code-shape
		// filter, and a quoted value in YAML/markdown is a value.
		{"SMTP_PASS long value", "scripts/x.sh", "export SMTP" + "_PASS=" + r7dAlnum, "pw-assign"},
		{"API_KEY long value", "scripts/x.sh", "API" + "_KEY=" + r7dAlnum, "pw-assign"},
		{"AWS secret key, unquoted", "scripts/x.sh", "AWS_SECRET_ACCESS" + "_KEY=" + r7dAlnum[:40], "pw-assign"},
		{"YAML smtp_pass long value", "deploy/x.yaml", "  smtp" + "_pass: " + r7dAlnum, "pw-assign"},
		{"TOML api_key long value", "config/x.toml", "api" + "_key = " + r7dAlnum, "pw-assign"},
		{"YAML quoted api_key", "deploy/x.yml", "api" + "_key: \"Zq9w8" + "Lm3Pq\"", "pw-assign"},
		{"markdown quoted api_key", "docs/x.md", "api" + "_key: \"Zq9w8" + "Lm3Pq\"", "pw-assign"},
		{"markdown unquoted long value", "docs/x.md", "- smtp" + "_pass: " + r7dAlnum, "pw-assign"},
		{"commit message line (pre-push)", "commit-msg@0123456789ab",
			"rotate tok" + "en " + r7dBT + r7dPW + r7dBT, "a0-token"},
		{"tag message line (pre-push)", "tag-msg@0123456789ab",
			"rotate tok" + "en " + r7dBT + r7dPW + r7dBT, "a0-token"},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := r7dSelect(t, "fail", rec(tc.path, i+1, tc.text))
			want := fmt.Sprintf("%s:%d: [%s]", tc.path, i+1, tc.class)
			if strings.TrimSpace(got) != want {
				t.Fatalf("got %q, want %q", strings.TrimSpace(got), want)
			}
			assertNoValue(t, got)
		})
	}
}

// TestSecretScan_CodeShapesAndPlaceholdersStayQuiet pins a sample of the measured
// false positives (scripts/secretscan.sh `codeish`, `placeholder`) so a tightening
// that brings them back is seen here rather than as a red CI run on docs.
func TestSecretScan_CodeShapesAndPlaceholdersStayQuiet(t *testing.T) {
	w := r7dTrig + " "
	lines := []string{
		rec("docs/x.md", 1, "postgres://user@host/db"),
		rec("docs/x.md", 2, "postgres://u:<pw>@h postgres://u:$DBPASS@h postgres://u:…@h postgres://u:REPLACE_ME@h"),
		rec("scripts/x.sh", 3, "PGPASS"+"WORD=\"$POSTGRES_PASSWORD\" psql"),
		rec("internal/x/a.go", 4, "\tpass"+"word := r.FormValue(\"pass"+"word\")"),
		rec("docs/x.md", 5, "MinPasswordRunes=12 and pass"+"word_hash=abc123XYZ"),
		rec("docs/x.md", 6, w+r7dBT+"ErrLogin_PasswordTooLong"+r7dBT+" "+r7dBT+"internal/x/y.go:12"+r7dBT),
		rec("docs/x.md", 7, w+r7dBT+"2026-09-25T10:00:00Z"+r7dBT+" "+r7dBT+"session.Token(redacted)"+r7dBT),
		rec("docs/x.md", 8, w+r7dBT+"ghcr.io/atknatk/tappa:sha-353897c6d5f6"+r7dBT+" "+r7dBT+"t73-admin-change-password"+r7dBT),
		rec("docs/x.md", 9, w+r7dBT+"Role/github-deployer"+r7dBT+" "+r7dBT+"SameSite=Lax"+r7dBT),
		// 55 characters of body is not a bcrypt digest (the tree's two FAKEfake constants).
		rec("db/x.sql", 10, "'"+"$2"+"a$12$"+strings.Repeat("F", 55)+"'"),
		// "sıra" (order) and "şirket" (company) are not the word for secret.
		rec("docs/x.md", 11, "sıra "+r7dBT+"Ab3!"+"efgh9"+r7dBT+" şirket"),
		rec("docs/x.md", 12, w+r7dBT+"["+r7dTrig+" repodan kaldırıldı]"+r7dBT),
		rec("deploy/x.yaml", 13, "  DB_PASS"+"WORD: \"REPLACE_DB_PASSWORD\""),
		// Hex padding and short hex are not the hex sub-shape.
		rec("docs/x.md", 14, w+`"`+strings.Repeat("A", 40)+`" `+r7dBT+strings.Repeat("0123456789abcdef", 2)+r7dBT),
		rec("docs/x.md", 15, w+r7dBT+"04A1B2C3D4E5F6"+r7dBT+" "+r7dBT+"0011223344556677"+r7dBT),
		// Shell redirections (a historical backlog line, found by a full-history run).
		rec("docs/x.md", 16, w+r7dBT+"2>/dev/null"+r7dBT+" "+r7dBT+"2>&1"+r7dBT),
		// `bypass=` is not `pass=`: the new keys count only as a whole _/- segment.
		rec("scripts/x.sh", 17, "BYPASS="+"Kx9pQ2wL rolbypassrls=t"),
		// Prose after a markdown key, and a placeholder value.
		rec("docs/x.md", 18, "Change pass"+"word: the form asks twice. pass"+"word: required"),
		rec("docs/x.md", 19, "pass"+"word = *** and api"+"_key: <your key>"),
		// A padded provider prefix (documentation example) is not a token.
		rec("docs/x.md", 20, "gh"+"p_"+strings.Repeat("x", 36)),
		rec("scripts/x.sh", 21, "cu"+"rl -u \"$USER:$PASS\" https://h/x"),
		// 3rd round, finding 3: the new keys need a value that looks like one.
		rec("scripts/x.sh", 23, "local pa"+"ss=0; PA"+"SS=false"),
		rec("docs/x.md", 24, "https://h/login?user=a&pa"+"ss=1"),
		rec("scripts/x.py", 25, "api"+"_key=None"),
		// A periodic fill pattern is not a random value.
		rec("docs/x.md", 26, w+`"`+strings.Repeat("FAKEfake", 5)+"123"+`"`),
		// A closing backtick right after `=` ends a code span; the value is empty.
		rec("docs/x.md", 22, "leave "+r7dBT+"DB"+"_PASS="+r7dBT+": the default applies"),
		// Same for the password-family key: a backtick ends a shell word.
		rec("docs/x.md", 27, "leave "+r7dBT+"PGPASS"+"WORD="+r7dBT+": unset by default"),
		// A closing double quote right after `=` ends a Go string, not a value (the
		// tree has this line in internal/db/role_test.go); without the quote parity the
		// shell-word reader would join the next strings into a "value".
		rec("internal/db/x_test.go", 28, "\tfor _, f := range []string{\"postgres://\", \"pass"+"word=\", \"DATABASE_URL=\"} {"),
		// The same for a SINGLE-quoted string (6th round: the enclosing-double-quote rule
		// covers the case above on its own; only the parity keeps this one quiet).
		rec("app/x.py", 29, "dsn = 'host=h pass"+"word=' + settings.db_password_value"),
	}
	if got := r7dSelect(t, "fail", lines...); got != "" {
		t.Fatalf("a code shape or placeholder was reported as a secret:\n%s", got)
	}
}

// TestSecretScan_AssignmentsNeedAStrongValue — 4th round. Three rounds in a row a
// waived dev value was extended by yet another grammar (a trailing character; a YAML
// separator, shell quote or doubled SQL quote; a value that does not end at the line
// break). The root cause was catching a 5-character single-class value and then
// forgiving it. Every pw-assign form now asks whether the VALUE is strong, the dev
// values are never caught, and their waivers are gone. What that buys and costs is
// pinned here: every earlier red case that is a strong value stays red — read with
// the right grammar (YAML plain scalar to end of line, shell word with its quotes
// joined, a doubled SQL quote as one quote) — and a weak value is silent on ANY path.
func TestSecretScan_AssignmentsNeedAStrongValue(t *testing.T) {
	compose := "      POSTGRES_PASS" + "WORD: tappa"
	pg := "PGPASS" + "WORD=tappa"
	sql := "PASS" + "WORD '" + "tappa"
	sq := "'"
	var red []string
	red = append(red, rec("scripts/x.sh", 1, "export DB"+"_PASS=Kx9!pQ2w"))
	red = append(red, rec("docker-compose.yml", 2, compose+"Zq9w"))
	red = append(red, rec("docs/plan/m8-deploy-pilot.md", 3, "`"+pg+"Zq9w psql`"))
	red = append(red, rec("commit-msg@fd56c289576f", 4, pg+"Zq9w"))
	for i, sep := range []string{" ", "\t", ",", ";", sq, `"`, "(", `\`} {
		red = append(red, rec("docker-compose.yml", 10+i, compose+sep+"Zq9w!x"))
	}
	for i, ext := range []string{sq + "Zq9w!x" + sq, `"Zq9w!x"`, `\Zq9w!x`} {
		red = append(red, rec("docs/plan/m8-deploy-pilot.md", 20+i, pg+ext+" psql"))
	}
	red = append(red, rec("scripts/db-init/02-dev-only-password.sh", 30, "\tALTER ROLE r LOGIN "+sql+sq+sq+"Zq9w!x';"))
	red = append(red, rec("docker-compose.yml", 31, "      POSTGRES_PASS"+"WORD: tappa1"))
	got := r7dSelect(t, "fail", red...)
	if n := strings.Count(got, "]"); n != len(red) {
		t.Fatalf("every strong (extended) value must FAIL (%d), got %d:\n%s", len(red), n, got)
	}
	assertNoValue(t, got)
	// Weak values are silent on any path — including paths no table names — and so
	// are the counted "different lines" continuations (a YAML plain scalar continued on
	// the next line, a SQL literal adjacent on the next line).
	quiet := []string{
		rec("docker-compose.yml", 1, compose),
		rec("deploy/other.yaml", 2, compose),
		rec("docs/any.md", 3, "`"+pg+" psql -U tappa_app`"),
		rec("commit-msg@0123456789ab", 4, pg),
		rec("db/any.sql", 5, "ALTER ROLE r LOGIN "+sql+"';"),
		rec("docs/any.md", 6, pg+"x psql"),
		rec("docker-compose.yml", 7, compose),
		rec("docker-compose.yml", 8, "        Zq9w!xLm3"),
		rec("db/any.sql", 9, "ALTER ROLE r LOGIN "+sql+"'"),
		rec("db/any.sql", 10, "  'Zq9w!xLm3';"),
	}
	if got := r7dSelect(t, "fail", quiet...); got != "" {
		t.Fatalf("weak values and counted line continuations must stay silent, got:\n%s", got)
	}
}

// waiverRow is one line of the R7d waiver table: <path ERE>;<sha256>;<description>.
type waiverRow struct{ re, hash, desc string }

// waiverTable reads the table out of scripts/secretscan.sh, so the tests cover every
// row the table holds — including rows added after they were written.
func waiverTable(t *testing.T) []waiverRow {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "secretscan.sh"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	const open, end = "<<'R7DW' || true\n", "\nR7DW\n"
	i := strings.Index(s, open)
	j := strings.Index(s[i+len(open):], end)
	if i < 0 || j < 0 {
		t.Fatal("the waiver table (R7DW heredoc) was not found in scripts/secretscan.sh")
	}
	var rows []waiverRow
	for _, ln := range strings.Split(s[i+len(open):i+len(open)+j], "\n") {
		if strings.TrimSpace(ln) == "" || strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		f := strings.SplitN(ln, ";", 3)
		if len(f) != 3 {
			t.Fatalf("waiver row is not <path ERE>;<sha256>;<description>: %q", ln)
		}
		rows = append(rows, waiverRow{f[0], f[1], f[2]})
	}
	if len(rows) < 20 {
		t.Fatalf("read only %d rows from the waiver table; the parser lost the table", len(rows))
	}
	return rows
}

// literalPath turns a row's anchored ERE back into the one path it names, and proves
// the ERE matches it.
func literalPath(t *testing.T, re string) string {
	t.Helper()
	p := strings.ReplaceAll(strings.TrimSuffix(strings.TrimPrefix(re, "^"), "$"), "[.]", ".")
	if strings.ContainsAny(p, "[]()*+?|\\^$") || !regexp.MustCompile(re).MatchString(p) {
		t.Fatalf("waiver path %q does not name exactly one literal path", re)
	}
	return p
}

func lineSHA(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// waivedLine is a tree line a table row names by its exact bytes (the line without
// its "\n"; a "\r" or trailing blank is part of it).
type waivedLine struct {
	path string
	no   int
	text string
	row  waiverRow
}

// waivedLines finds, for every row, the lines of its file whose sha256 is the row's.
// A row that names no line is stale — its line changed or went — and fails here.
func waivedLines(t *testing.T) []waivedLine {
	t.Helper()
	var out []waivedLine
	for _, r := range waiverTable(t) {
		p := literalPath(t, r.re)
		b, err := os.ReadFile(filepath.Join(repoRoot, p))
		if err != nil {
			t.Fatalf("waiver row names %s: %v", p, err)
		}
		lines := strings.Split(string(b), "\n")
		if strings.HasSuffix(string(b), "\n") {
			lines = lines[:len(lines)-1]
		}
		found := 0
		for i, l := range lines {
			if lineSHA(l) == r.hash {
				out = append(out, waivedLine{p, i + 1, l, r})
				found++
			}
		}
		if found == 0 {
			t.Errorf("stale waiver row: no line of %s has the sha256 the row names (%s)", p, r.desc)
		}
	}
	return out
}

// unnamed is the same file name in a directory no row names, so every path-dependent
// rule (extension, `$` context) reads the line exactly as on the named path.
func unnamed(p string) string {
	return filepath.ToSlash(filepath.Join(filepath.Dir(p), "zz-unnamed", filepath.Base(p)))
}

// sameAsUnnamed classifies each line on its named path and on an unnamed one: the
// reports must be identical line by line, and nothing may be waived.
func sameAsUnnamed(t *testing.T, what string, paths, lines []string) (reported int) {
	t.Helper()
	var named, other []string
	for i, l := range lines {
		named = append(named, rec(paths[i], i+1, l))
		other = append(other, rec(unnamed(paths[i]), i+1, l))
	}
	classesOf := func(out string) map[int]string {
		m := map[int]string{}
		for _, ln := range strings.Split(strings.TrimSpace(out), "\n") {
			if ln == "" {
				continue
			}
			head, cls, ok := strings.Cut(ln, ": [")
			k := strings.LastIndex(head, ":")
			no, err := strconv.Atoi(head[k+1:])
			if !ok || k < 0 || err != nil {
				t.Fatalf("unparseable report line %q", ln)
			}
			m[no] = cls
		}
		return m
	}
	gotNamed := r7dSelect(t, "fail", named...)
	a, b := classesOf(gotNamed), classesOf(r7dSelect(t, "fail", other...))
	for i := range lines {
		if a[i+1] != b[i+1] {
			t.Errorf("%s: line %d of %s is classified differently on its named path (%q) than on an unnamed one (%q)", what, i+1, paths[i], a[i+1], b[i+1])
		}
	}
	if w := r7dSelect(t, "waived", named...); w != "" {
		t.Errorf("%s: a changed line is still waived:\n%s", what, w)
	}
	assertNoValue(t, gotNamed)
	return len(a)
}

// insertPositions: the start, the end, both sides of every quote and separator, and
// ten pseudo-random positions. A line longer than 4 KiB (only the vendored htmx, 51 KB
// with 13 419 quotes and separators) keeps an evenly spaced sample of 30
// neighbourhoods: classifying one such line costs ~0.1 s, and the property does not
// depend on where in the line the bytes change.
func insertPositions(s string) []int {
	var near []int
	for i := 0; i < len(s); i++ {
		if strings.IndexByte("\"'`=,;|():{}[]<> \t+", s[i]) >= 0 {
			near = append(near, i, i+1)
		}
	}
	if len(s) > 4096 && len(near) > 30 {
		step := len(near) / 30
		var sample []int
		for i := 0; i < len(near); i += step {
			sample = append(sample, near[i])
		}
		near = sample
	}
	pos := append([]int{0, len(s)}, near...)
	x := len(s)*7 + 3
	for k := 0; k < 10; k++ {
		x = (x*1103515245 + 12345) & 0x7fffffff
		pos = append(pos, x%(len(s)+1))
	}
	seen := map[int]bool{}
	var out []int
	for _, p := range pos {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// TestSecretScan_EveryWaivedLineIsWaivedAsIs — 7th round, test B. Each row names at
// least one tree line; that line, byte for byte on its path, is waived (reported as
// `[muaf]`, never as FAIL); and the same line on an unnamed path FAILs — so every row
// is needed, and the path is part of the binding.
func TestSecretScan_EveryWaivedLineIsWaivedAsIs(t *testing.T) {
	wl := waivedLines(t)
	var named, other []string
	for i, w := range wl {
		named = append(named, rec(w.path, i+1, w.text))
		other = append(other, rec(unnamed(w.path), i+1, w.text))
	}
	if got := r7dSelect(t, "fail", named...); got != "" {
		t.Fatalf("a waived line, byte for byte on its path, was reported:\n%s", got)
	}
	waived := r7dSelect(t, "waived", named...)
	got := r7dSelect(t, "fail", other...)
	for i, w := range wl {
		if !strings.Contains(waived, fmt.Sprintf("%s:%d: [muaf]\n", w.path, i+1)) {
			t.Errorf("%s:%d is not reported as waived (WARN)", w.path, w.no)
		}
		if !strings.Contains(got, fmt.Sprintf("%s:%d: [", unnamed(w.path), i+1)) {
			t.Errorf("%s:%d does not FAIL on an unnamed path: the row is not needed (%s)", w.path, w.no, w.row.desc)
		}
	}
	assertNoValue(t, got)
	t.Logf("%d rows, %d waived tree lines", len(waiverTable(t)), len(wl))
}

// TestSecretScan_AWaiverIsBoundToTheWholeLine — 7th round, test A, the property that
// closes the class six rounds kept reopening (a prefix, a same-line separator, a line
// break, quote parity, a trigger inside the token, a LEFT extension): a strong value
// inserted ANYWHERE in a waived line makes it an ordinary line — classified exactly as
// the same line on an unnamed path, and not waived.
func TestSecretScan_AWaiverIsBoundToTheWholeLine(t *testing.T) {
	var paths, lines []string
	for _, w := range waivedLines(t) {
		for _, pos := range insertPositions(w.text) {
			paths = append(paths, w.path)
			lines = append(lines, w.text[:pos]+r7dPW+w.text[pos:])
		}
	}
	n := sameAsUnnamed(t, "insertion", paths, lines)
	t.Logf("%d insertions into waived lines, %d of them FAIL, all identical to the unnamed path", len(lines), n)
}

// TestSecretScan_OneByteBreaksTheWaiver — 7th round, test C: no normalisation. A
// trailing blank, a "\r", a leading blank, one byte fewer at either end, or one byte
// changed in the middle, and the line is no longer the waived one.
func TestSecretScan_OneByteBreaksTheWaiver(t *testing.T) {
	var paths, lines []string
	rowHashes := map[string]map[string]bool{}
	for _, r := range waiverTable(t) {
		p := literalPath(t, r.re)
		if rowHashes[p] == nil {
			rowHashes[p] = map[string]bool{}
		}
		rowHashes[p][r.hash] = true
	}
	twins := 0
	for _, w := range waivedLines(t) {
		s := w.text
		mid := []byte(s)
		k := len(mid) / 2
		switch c := mid[k]; {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			mid[k] = c ^ 0x20
		case c == 'x':
			mid[k] = 'y'
		default:
			mid[k] = 'x'
		}
		for _, v := range []string{s + " ", s + "\r", " " + s, s[:len(s)-1], s[1:], string(mid)} {
			// Dropping the first tab of a twice-indented line can give ANOTHER waived
			// line of the same file byte for byte; that line is waived, as it should be.
			if rowHashes[w.path][lineSHA(v)] {
				twins++
				continue
			}
			paths, lines = append(paths, w.path), append(lines, v)
		}
	}
	n := sameAsUnnamed(t, "one-byte change", paths, lines)
	t.Logf("%d one-byte variants, %d of them FAIL, all identical to the unnamed path; %d skipped as another waived line of the file", len(lines), n, twins)
}

// TestSecretScan_TheHashHelperPrintsARowNotTheLine: `bash scripts/secretscan.sh --hash
// <file>:<line>` prints the table row's head for that line — the path ERE and the
// sha256 the scanner computes — and never the line itself.
func TestSecretScan_TheHashHelperPrintsARowNotTheLine(t *testing.T) {
	helper := func(args ...string) (string, int) {
		cmd := exec.Command("bash", append([]string{"scripts/secretscan.sh"}, args...)...)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}
	done := map[string]bool{}
	for _, w := range waivedLines(t) {
		if done[w.row.re+w.row.hash] {
			continue
		}
		done[w.row.re+w.row.hash] = true
		out, code := helper("--hash", fmt.Sprintf("%s:%d", w.path, w.no))
		if code != 0 || !strings.HasPrefix(out, w.row.re+";"+w.row.hash+";") {
			t.Errorf("--hash %s:%d printed %q (exit %d), want the row head %s;%s;", w.path, w.no, out, code, w.row.re, w.row.hash)
		}
		if len(w.text) >= 8 && strings.Contains(out, strings.TrimSpace(w.text)) {
			t.Errorf("--hash %s:%d printed the line itself", w.path, w.no)
		}
	}
	for _, bad := range [][]string{{}, {"--hash"}, {"--hash", "Makefile"}, {"--hash", "no/such/file:1"}, {"--hash", "Makefile:0"}, {"--hash", "Makefile:99999"}, {"--hush", "Makefile:1"}} {
		if out, code := helper(bad...); code != 2 {
			t.Errorf("--hash %v: want exit 2, got %d: %s", bad, code, out)
		}
	}
}

// TestSecretScan_AMissingOrBrokenHasherFailsLoudly: without a working sha256 tool the
// table cannot match anything, which must not read as "nothing waived, all clean" —
// nor may a tool that hashes fewer files than it was given.
func TestSecretScan_AMissingOrBrokenHasherFailsLoudly(t *testing.T) {
	run := func(pathPrefix string) (string, int) {
		cmd := exec.Command("bash", "-c", `. scripts/secretscan.sh && r7d_select fail`)
		cmd.Dir = repoRoot
		cmd.Env = append(os.Environ(), "PATH="+pathPrefix+string(os.PathListSeparator)+os.Getenv("PATH"))
		cmd.Stdin = strings.NewReader(rec(".env.example", 1, "x") + "\n")
		out, err := cmd.CombinedOutput()
		if ee, ok := err.(*exec.ExitError); ok {
			return string(out), ee.ExitCode()
		}
		return string(out), 0
	}
	real := ""
	if p, err := exec.LookPath("sha256sum"); err == nil {
		real = p
	} else if p, err := exec.LookPath("shasum"); err == nil {
		real = p + " -a 256"
	} else {
		t.Skip("no sha256 tool on this machine")
	}
	broken := t.TempDir()
	for _, n := range []string{"sha256sum", "shasum"} {
		if err := os.WriteFile(filepath.Join(broken, n), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if out, code := run(broken); code != 2 || !strings.Contains(out, "sha256") {
		t.Fatalf("no working sha256 tool must fail the scan (exit 2), got %d: %s", code, out)
	}
	lossy := t.TempDir()
	body := "#!/bin/sh\nif [ $# -eq 0 ]; then exec " + real + "; fi\nexit 0\n"
	for _, n := range []string{"sha256sum", "shasum"} {
		if err := os.WriteFile(filepath.Join(lossy, n), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if out, code := run(lossy); code != 2 || !strings.Contains(out, "sha256") {
		t.Fatalf("a hasher that drops files must fail the scan (exit 2), got %d: %s", code, out)
	}
}

// TestSecretScan_SeventhRoundFindings pins the two non-blocking 6th-round findings:
//   - a double-quoted string CLOSES and the shell word goes on when a word or quote
//     character follows the closing quote (these five were silent);
//   - in a Kubernetes manifest only `$(NAME)` expands: `$X` and `${X}` are the value.
func TestSecretScan_SeventhRoundFindings(t *testing.T) {
	pg := "PGPASS" + "WORD"
	pw := "pass" + "word"
	dq, sq := `"`, "'"
	k8s := "deploy/k8s/05-config.yaml"
	cases := []struct {
		name, path, text string
		red              bool
	}{
		{"closed string + single-quoted part", "scripts/x.sh", "export " + dq + pg + "=tappa" + dq + sq + "Zq9w!x" + sq, true},
		{"closed string + bare word", "scripts/x.sh", "env " + dq + pg + "=tappa" + dq + "Zq9w!x psql", true},
		{"closed string + backslash escape", "scripts/x.sh", "env " + dq + pg + "=tappa" + dq + `\Zq9w!x psql`, true},
		{"closed string + another double-quoted part", "scripts/x.sh", "env " + dq + pg + "=tappa" + dq + dq + "Zq9w!x" + dq + " psql", true},
		{"docker -e with two parts", "scripts/x.sh", "docker run -e " + dq + pg + "=Zq9w" + dq + sq + "!xLm3" + sq + " img", true},
		{"control: closed string then a blank", "scripts/x.sh", "env " + dq + pg + "=tappa" + dq + " psql", false},
		{"k8s: $ + value is the value", k8s, "  DB_" + strings.ToUpper(pw) + ": $" + "Kx9!pQ2w", true},
		{"k8s: ${X} is the value", k8s, "  DB_" + strings.ToUpper(pw) + ": ${DB_" + strings.ToUpper(pw) + "}", true},
		{"control: k8s $(NAME) expands", k8s, "  DB_" + strings.ToUpper(pw) + ": $(DB_PASS)", false},
		{"control: a shell script under deploy/k8s stays shell", "deploy/k8s/postgres-init/x.sh", pg + "=" + dq + "$" + "Kx9!pQ2w" + dq + " psql", false},
	}
	var lines []string
	for i, tc := range cases {
		lines = append(lines, rec(tc.path, i+1, tc.text))
	}
	got := r7dSelect(t, "fail", lines...)
	for i, tc := range cases {
		if hit := strings.Contains(got, fmt.Sprintf("%s:%d: [", tc.path, i+1)); hit != tc.red {
			t.Errorf("%s: reported=%v, want %v", tc.name, hit, tc.red)
		}
	}
	assertNoValue(t, got)
}

// TestSecretScan_FifthRoundValueReaders pins the 5th-round findings in how a VALUE is
// read, one line each so a mutation names the case it breaks:
//   - N1: a YAML quoted scalar keeps its escapes (a doubled single quote, a
//     backslash) instead of ending at them;
//   - N3: a quoted value is a string literal, never a call, selector or ENV name, and
//     a YAML plain scalar has no calls at all;
//   - N2: `$`/`%` + a strong body that is not an identifier is a value, not a
//     reference; in a shell single-quoted word and a .sql literal `$` does not expand;
//     a leading `^` is a regex only when the rest looks like one;
//   - N4: YAML node properties (anchor, tag) are skipped and the value behind them read;
//   - N7: the quoted-key and list-item YAML forms, and trailing-blank trimming.
func TestSecretScan_FifthRoundValueReaders(t *testing.T) {
	sq, dq := "'", `"`
	pw := "pass" + "word"
	yml := "deploy/x.yaml"
	sig := "$" + "Zq9w!x" + "Lm3Kp7" // `$` + a strong body that is not an identifier
	idb := "$" + "Zq9wx" + "Lm3Kp7"  // `$` + an identifier-shaped body
	cases := []struct {
		name, path, text string
		red              bool
	}{
		// N1
		{"YAML single-quoted, doubled quote", yml, "  " + pw + ": " + sq + "Zq9w" + sq + sq + "!xLm3" + sq, true},
		{"YAML double-quoted, backslash escape", yml, "  " + pw + ": " + dq + "Zq9w\\" + dq + "!xLm3" + dq, true},
		{"YAML single-quoted, two classes", yml, "  " + pw + ": " + sq + "zq9w" + sq + sq + "xlm3k" + sq, true},
		// N3
		{"SQL literal with parentheses", "db/x.sql", "ALTER ROLE r LOGIN PASS" + "WORD " + sq + "Zq9w(x!L3)" + sq + ";", true},
		{"SQL literal shaped like an ENV name", "db/x.sql", "ALTER ROLE r LOGIN PASS" + "WORD " + sq + "ZQ9W_XLM3_KP7" + sq + ";", true},
		{"shell single-quoted with parentheses", "scripts/x.sh", "PGPASS" + "WORD=" + sq + "Zq9w(x!L3)" + sq + " psql", true},
		{"YAML double-quoted, selector-shaped", yml, "  " + pw + ": " + dq + "Zq9w.xLm3Kp" + dq, true},
		{"YAML plain scalar, call-shaped", "docker-compose.yml", "      POSTGRES_PASS" + "WORD: tappa(Zq9w!x)", true},
		{"control: Python keyword argument is a selector", "app/x.py", "connect(" + pw + "=settings.DB_PASS)", false},
		{"control: unquoted ENV-name value", yml, "  api" + "_key: YOUR_API_KEY", false},
		{"control: unquoted path value", yml, "  " + pw + ": /run/secrets/db", false},
		// N2
		{"shell unquoted $ + strong body", "scripts/x.sh", "PGPASS" + "WORD=" + sig + " psql", true},
		{"shell double-quoted $ + strong body", "scripts/x.sh", "PGPASS" + "WORD=" + dq + sig + dq + " psql", true},
		{"SQL literal $ + strong body (shell file)", "scripts/x.sh", "ALTER ROLE r PASS" + "WORD " + sq + sig + sq + ";", true},
		{"YAML plain $ + strong body", yml, "  " + pw + ": " + sig, true},
		{"TOML quoted $ + strong body", "config/x.toml", pw + " = " + dq + sig + dq, true},
		{"quoted $ + strong body next to a trigger", "docs/x.md", "the " + r7dTrig + " is " + dq + sig + dq, true},
		{"URL credential $ + strong body", "docs/x.md", "postgres://u:" + sig + "@h/db", true},
		{"curl $ + strong body", "scripts/x.sh", "cu" + "rl -u ops:" + sig + " https://h/x", true},
		{"markdown unquoted $ + strong body", "docs/x.md", "- " + pw + ": " + sig, true},
		{"Go string starting with ^", "internal/x/a.go", "\tpw := " + dq + "^Zq9w!xLm3" + dq + " // " + pw, true},
		{"shell single-quoted $ does not expand", "scripts/x.sh", "PGPASS" + "WORD=" + sq + idb + sq + " psql", true},
		{".sql literal $ does not expand", "db/x.sql", "ALTER ROLE r PASS" + "WORD " + sq + idb + sq + ";", true},
		{"control: $VAR in double quotes", "scripts/x.sh", "PGPASS" + "WORD=" + dq + idb + dq + " psql", false},
		{"control: ${VAR}", "scripts/x.sh", "PGPASS" + "WORD=${APP_DB_PASS" + "WORD} psql", false},
		{"control: SQL in a shell file expands $VAR", "scripts/x.sh", "ALTER ROLE r PASS" + "WORD " + sq + idb + sq + ";", false},
		{"control: Compose interpolates quoted ${VAR}", yml, "  " + pw + ": " + sq + "${DB_PASS" + "WORD}" + sq, false},
		{"control: Python %(name)s", "app/x.py", "q = " + dq + "PASS" + "WORD " + sq + "%(" + pw + ")s" + sq + dq, false},
		{"control: a regex after a trigger", "internal/x/a.go", "\tre := " + dq + "^[A-Za-z0-9]{8,}$" + dq + " // " + pw, false},
		// The full-history run found this one (deploy.yml, fd56c28): `$HOME` expands and
		// what follows it is a path, not a value.
		{"control: $VAR followed by a path", ".github/workflows/x.yml", "echo " + dq + "${{ sec" + "rets.KUBE }}" + dq + " | base64 -d > " + dq + "$HOME/.kube/config" + dq, false},
		{"control: $VAR followed by a file name", "scripts/x.sh", "# " + pw + " dump: " + dq + "$DB_NAME-backup-2026.sql" + dq, false},
		// N4
		{"YAML anchor before the value", yml, "  " + pw + ": &pw Zq9w!xLm3", true},
		{"YAML tag before the value", yml, "  " + pw + ": !!str Zq9w!xLm3", true},
		{"YAML anchor and tag before a quoted value", yml, "  " + pw + ": &pw !!str " + dq + "Zq9w!xLm3" + dq, true},
		{"YAML quoted value starting with &", yml, "  " + pw + ": " + dq + "&Zq9w!xLm3" + dq, true},
		{"YAML quoted value starting with !", yml, "  " + pw + ": " + sq + "!Zq9w!xLm3" + sq, true},
		{"YAML | not followed by a block indicator", yml, "  " + pw + ": |Zq9w!xLm3", true},
		{"control: YAML alias", yml, "  " + pw + ": *Zq9w!xLm3", false},
		{"control: YAML comment", yml, "  " + pw + ": #Zq9w!xLm3", false},
		{"control: YAML anchor then block scalar", yml, "  " + pw + ": &pw |", false},
		{"control: YAML block scalar with chomping", yml, "  " + pw + ": >-", false},
		{"control: YAML property alone (value on the next lines)", yml, "  " + pw + ": !!binary", false},
		// The property is skipped, not read as part of the value: a weak value or a
		// block indicator behind it stays what it is.
		{"control: YAML tag before a weak value", yml, "  " + pw + ": !!str tappa", false},
		{"control: YAML anchor before a weak quoted value", yml, "  " + pw + ": &dev_pw " + dq + "tappa" + dq, false},
		{"control: YAML long anchor before a block scalar", yml, "  " + pw + ": &default_db_pw |", false},
		// N7
		{"YAML double-quoted key", yml, "  \"db_" + pw + "\": Zq9w!xLm3", true},
		{"YAML list item", yml, "  - " + pw + ": Zq9w!xLm3", true},
		{"YAML double-quoted new key", yml, "  \"api" + "_key\": Zq9w!xLm3", true},
		{"YAML list item, new key", yml, "  - api" + "_key: Zq9w!xLm3", true},
		{"control: YAML trailing blanks are not the value", yml, "  " + pw + ": tappa   ", false},
		{"control: YAML CRLF is not the value", yml, "  " + pw + ": tappa\r", false},
	}
	var lines []string
	for i, tc := range cases {
		lines = append(lines, rec(tc.path, i+1, tc.text))
	}
	got := r7dSelect(t, "fail", lines...)
	for i, tc := range cases {
		hit := strings.Contains(got, fmt.Sprintf("%s:%d: [", tc.path, i+1))
		if hit != tc.red {
			t.Errorf("%s: reported=%v, want %v", tc.name, hit, tc.red)
		}
	}
	assertNoValue(t, got)
}

// TestSecretScan_AnUnparseableRecordFailsLoudly (2nd round, N1): only rg's binary
// notice may lack the separator; anything else is a scan that did not happen.
func TestSecretScan_AnUnparseableRecordFailsLoudly(t *testing.T) {
	if _, _, code := r7dRun("fail", "web/x.woff2: binary file matches (found \"\\0\" byte around offset 4)"); code != 0 {
		t.Fatalf("rg's binary notice must be skipped, got exit %d", code)
	}
	for _, bad := range []string{
		"docs/plan/a:b.md:3:" + r7dTrig + " " + r7dBT + r7dPW + r7dBT, // the old `:` format
		"docs/x.md\x1fnotanumber\x1ftext",
	} {
		_, errOut, code := r7dRun("fail", bad)
		if code != 2 || !strings.Contains(errOut, "ayristirilamadi") {
			t.Fatalf("an unparseable record must fail the scan (exit 2), got %d: %s", code, errOut)
		}
	}
}

// TestSecretScan_ABrokenWaiverTableFailsLoudly: a malformed row must not quietly match
// nothing (or everything). The classifier refuses the table instead (exit 2), and
// redline-check.sh turns a non-zero exit into its "TARAMA GUVENILIR DEGIL" exit 2
// rather than a clean-looking empty answer. The 6th round's token rows (`;;`) and
// types (`@class=`) are rows of the wrong shape now.
func TestSecretScan_ABrokenWaiverTableFailsLoudly(t *testing.T) {
	h := strings.Repeat("ab", 32)
	for _, table := range []string{
		"^x$;;", "^x$", "^x$;" + h, "x$;" + h + ";d", "^x;" + h + ";d",
		"^x$;" + h[:63] + ";d", "^x$;" + strings.ToUpper(h) + ";d", "^x$;" + h + ";", "^x$;" + h + "; ",
		"^x$;" + h + ";bu bir " + "par" + "ola notu", "^x$;" + h + ";d\n^x$;" + h + ";d",
		"^x$;;" + "postgres://u" + ":p@h/db", "^x$;;@class=a0-to" + "ken",
		// 8th round: a description may not carry a value even without a trigger word,
		// in any wrapping, nor a credential of another class.
		"^x$;" + h + ";eski panel degeri " + "Kx9!" + "pQ2wLm rotate",
		"^x$;" + h + ";eski deger (" + "Kx9!" + "pQ2wLm)",
		"^x$;" + h + ";eski deger " + `"` + "Kx9!" + "pQ2wLm" + `"`,
		"^x$;" + h + ";eski deger " + r7dBT + "Kx9!" + "pQ2wLm" + r7dBT,
		"^x$;" + h + ";eski deger '" + "Kx9!" + "pQ2wLm'",
		"^x$;" + h + ";eski DSN postgres://u" + ":p@h/db",
		"^x$;" + h + ";" + r7dAlnum,
		// 9th round (N3): a value split by a character the piece check splits on.
		"^x$;" + h + ";eski Kx9!pQ2(" + "wLm3Zq", "^x$;" + h + ";eski Kx9!pQ2," + "wLm3Zq",
		"^x$;" + h + ";eski Kx9!pQ2;" + "wLm3Zq", "^x$;" + h + ";eski Kx9!pQ2[" + "wLm3Zq]",
		"^x$;" + h + ";eski Kx9!pQ2{" + "wLm3Zq}", "^x$;" + h + ";eski Kx9!pQ2<" + "wLm3Zq",
		"^x$;" + h + ";eski Kx9!pQ2\\" + "wLm3Zq", "^x$;" + h + ";eski Kx9!pQ2ğ" + "wLm3Zq",
		// A value glued to a long run: the joined word is longer than any candidate, so
		// only the piece check (split on the separator) sees the value.
		"^x$;" + h + ";eski " + strings.Repeat("abcdefgh", 16) + "(" + "Kx9!pQ2wLm",
		// 8th round: the path names ONE file — no unescaped regex metacharacter.
		"^.*$;" + h + ";d", "^a.b$;" + h + ";d", "^a+$;" + h + ";d", "^(a|b)$;" + h + ";d",
		"^a$|^b$;" + h + ";d", "^a[.]b*$;" + h + ";d", "^a\\.b$;" + h + ";d",
	} {
		cmd := exec.Command("bash", "-c", `. scripts/secretscan.sh && R7D_WAIVERS="$1" && r7d_select fail`, "r7d", table)
		cmd.Dir = repoRoot
		cmd.Stdin = strings.NewReader(rec("docs/x.md", 1, "postgres://u"+":p@h/db") + "\n")
		out, err := cmd.CombinedOutput()
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 2 {
			t.Fatalf("waiver table %q: want exit 2, got err=%v\n%s", table, err, out)
		}
	}
}

// TestSecretScan_ADollarInPlainTextIsPartOfTheValue — 6th round, B3. Where nothing
// expands `$` (markdown, commit and tag messages, source-code strings, a URL's
// password) only `$NAME`, `${...}` and printf verbs are references; anything else is
// a value measured WHOLE, `$` included. Real passwords often start with `$`, and the
// A-0 shape with one was silent while the same value without `$` failed (measured).
// Where `$` does expand (shell, Compose/CI YAML, Makefile, .env) the 5th-round rule
// stays: a short tail after `$NAME` is a counted limit (#23).
func TestSecretScan_ADollarInPlainTextIsPartOfTheValue(t *testing.T) {
	dq := `"`
	pw := "pass" + "word"
	short := "$" + "Kx9!pQ2w"   // `$` + identifier + a short tail
	short2 := "$" + "Zq9w!xLm3" // the auditor's other value
	pct := "%" + "Zq9w!xLm3"
	cases := []struct {
		name, path, text string
		red              bool
	}{
		{"A-0 shape in state.md", "docs/plan/state.md", "operatör " + r7dTrig + "sı " + r7dBT + short + r7dBT + " — rotate edilmeli", true},
		{"markdown key: $value", "docs/x.md", "- " + pw + ": " + short2, true},
		{"markdown quoted %value", "docs/x.md", "the " + r7dTrig + " is " + dq + pct + dq, true},
		{"commit message assignment", "commit-msg@0123456789ab", "PGPASS" + "WORD=" + short2 + " psql", true},
		{"tag message backticks", "tag-msg@0123456789ab", "rotate tok" + "en " + r7dBT + short + r7dBT, true},
		{"URL password in markdown", "docs/x.md", "postgres://u:" + short + "@h/db", true},
		{"URL password in a shell file", "scripts/x.sh", "DATABASE_URL=postgres://u:" + short + "@h/db", true},
		{"Go string", "internal/x/a.go", "\tv := " + dq + short + dq + " // " + pw, true},
		{"control: $NAME in markdown", "docs/x.md", "the " + r7dTrig + " is " + r7dBT + "$POSTGRES_PASS" + "WORD" + r7dBT, false},
		{"control: ${NAME} in markdown", "docs/x.md", "the " + r7dTrig + " is " + r7dBT + "${DB_PASS" + "WORD}" + r7dBT, false},
		{"control: markdown key: $NAME", "docs/x.md", "- " + pw + ": $DB_PASS", false},
		{"control: commit message $NAME", "commit-msg@0123456789ab", "PGPASS" + "WORD=$PGPASS psql", false},
		{"control: URL ${NAME}", "docs/x.md", "postgres://u:${DB_PASS}@h/db", false},
		// The key sits INSIDE a Go string: its closing quote ends the value (shword).
		{"control: Go printf verb inside a string", "internal/x/a.go", "\tq := fmt.Sprintf(" + dq + "user=%s " + pw + "=%s" + dq + ", u, p)", false},
		{"control: printf verb with flags", "internal/x/a.go", "\tq := fmt.Sprintf(" + dq + "user=%s " + pw + "=%-12s" + dq + ", u, p)", false},
		// 8th round: the same printf verb with no blank in the string (a0 used to fire).
		{"control: KEY=%-20s with no blank", "internal/x/a.go", "\tq := fmt.Sprintf(" + dq + "PGPASS" + "WORD=%-20s" + dq + ", v)", false},
		{"control: KEY=%08d", "internal/x/a.go", "\tq := fmt.Sprintf(" + dq + "PGPASS" + "WORD=%08d" + dq + ", v)", false},
		{"control: KEY=%.2f", "internal/x/a.go", "\tq := fmt.Sprintf(" + dq + "PGPASS" + "WORD=%.2f" + dq + ", v)", false},
		{"control: KEY=%q and %v", "internal/x/a.go", "\tq := fmt.Sprintf(" + dq + "PGPASS" + "WORD=%q:%v" + dq + ", v, w)", false},
		{"control: KEY=%(name)s", "app/x.py", "q = " + dq + "PGPASS" + "WORD=%(" + pw + ")s" + dq + " % d", false},
		{"KEY=value%s is still a value", "internal/x/a.go", "\tq := fmt.Sprintf(" + dq + "PGPASS" + "WORD=Kx9!" + "pQ2w%s" + dq + ", v)", true},
		// The same two with a key that is not an assignment key, so only a0-token decides.
		{"control: a0-only key with a printf verb", "internal/x/a.go", "\tq := fmt.Sprintf(" + dq + "SESSION_TOK" + "EN=%-20s" + dq + ", v)", false},
		{"a0-only key: value%s is still a value", "internal/x/a.go", "\tq := fmt.Sprintf(" + dq + "SESSION_TOK" + "EN=Kx9!" + "pQ2w%s" + dq + ", v)", true},
		{"control: shell echo of an assignment", "docs/x.md", "echo " + dq + "PGPASS" + "WORD=$PGPASS" + dq + " >> ~/.profile", false},
		{"control: $HOME path in markdown", "docs/x.md", "kube " + r7dTrig + ": " + r7dBT + "$HOME/.kube/config" + r7dBT, false},
		{"counted #23: YAML short tail", "deploy/x.yaml", "  " + pw + ": " + short2, false},
		{"counted #23: shell double-quoted short tail", "scripts/x.sh", "PGPASS" + "WORD=" + dq + short + dq + " psql", false},
	}
	var lines []string
	for i, tc := range cases {
		lines = append(lines, rec(tc.path, i+1, tc.text))
	}
	got := r7dSelect(t, "fail", lines...)
	for i, tc := range cases {
		if hit := strings.Contains(got, fmt.Sprintf("%s:%d: [", tc.path, i+1)); hit != tc.red {
			t.Errorf("%s: reported=%v, want %v", tc.name, hit, tc.red)
		}
	}
	assertNoValue(t, got)
}

// hookRepo is a throwaway repository with the scanner and hook copied in. No remote,
// no push: the hook is driven the way git drives it (stdin: <local ref> <local sha>
// <remote ref> <remote sha>).
type hookRepo struct {
	t   *testing.T
	dir string
}

func newHookRepo(t *testing.T) *hookRepo {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		// The hook itself refuses to run without rg (exit 2, asserted below); CI
		// installs rg before `make check` and proves it with rg --version.
		t.Skip("rg is not installed; the pre-push hook needs it (CI installs it)")
	}
	r := &hookRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q")
	for _, rel := range []string{"scripts/secretscan.sh", "scripts/git-hooks/pre-push"} {
		b, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil {
			t.Fatal(err)
		}
		r.write(rel, string(b))
	}
	return r
}

func (r *hookRepo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.test",
		"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-c", "init.defaultBranch=main",
		"-c", "core.hooksPath=/dev/null", "-c", "advice.nestedTag=false"}, args...)...)
	cmd.Dir = r.dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *hookRepo) write(rel, body string) {
	r.t.Helper()
	p := filepath.Join(r.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *hookRepo) commit(msg ...string) string {
	r.t.Helper()
	r.git("add", "-A")
	args := []string{"commit", "-q"}
	for _, m := range msg {
		args = append(args, "-m", m)
	}
	r.git(args...)
	return r.git("rev-parse", "HEAD")
}

func (r *hookRepo) hook(ref, local, remote string, env ...string) (string, int) {
	r.t.Helper()
	cmd := exec.Command("bash", filepath.Join(r.dir, "scripts", "git-hooks", "pre-push"), "origin", "/dev/null")
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s %s %s %s\n", ref, local, ref, remote))
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		r.t.Fatalf("running the hook: %v", err)
	}
	return string(out), code
}

func (r *hookRepo) mustFail(what, out string, code int, want string) {
	r.t.Helper()
	if code != 1 || !strings.Contains(out, want) {
		r.t.Fatalf("%s must stop the push (exit 1, %q):\nexit=%d\n%s", what, want, code, out)
	}
	assertNoValue(r.t, out)
}

func a0Line() string { return r7dTrig + "sı " + r7dBT + r7dPW + r7dBT + "\n" }

var zeroSHA = strings.Repeat("0", 40)

// TestPrePush_ScansWhatAPushPublishes drives scripts/git-hooks/pre-push the way git
// does (stdin: <local ref> <local sha> <remote ref> <remote sha>) in a throwaway
// repository — no remote, no push. It pins what a tree scan cannot see: a value
// added and then removed inside the range, a value in a commit MESSAGE or an
// annotated TAG message, a merge (including an evil merge), a new branch (remote
// sha all zeros), a path with a colon, files a `-diff` attribute calls binary —
// and that every way the scan itself can break ends in exit 2, never exit 0.
func TestPrePush_ScansWhatAPushPublishes(t *testing.T) {
	realAwk, err := exec.LookPath("awk")
	if err != nil {
		t.Fatal(err)
	}
	r := newHookRepo(t)
	dir, git, write, commit, hook := r.dir, r.git, r.write, r.commit, r.hook
	mustFail := r.mustFail
	a0 := a0Line
	zero := zeroSHA
	base := commit("base")

	write("docs/plan/note.md", "a\n\noperatör "+a0())
	commit("add note")
	git("rm", "-q", "docs/plan/note.md")
	removed := commit("remove note")
	write("x.txt", "x\n")
	withMsg := commit("chore", "rotate tok"+"en "+r7dBT+r7dPW+r7dBT)
	write("y.txt", "y\n")
	clean := commit("clean")
	br := "refs/heads/b"

	out, code := hook(br, removed, base)
	mustFail("a value added then removed inside the range", out, code, "docs/plan/note.md:3: [a0-token]")
	out, code = hook(br, removed, zero)
	mustFail("a NEW branch (remote sha zero)", out, code, "docs/plan/note.md:3:")
	out, code = hook(br, withMsg, removed)
	mustFail("a value in a commit MESSAGE", out, code, "commit-msg@")
	if out, code := hook(br, clean, withMsg); code != 0 {
		t.Fatalf("a clean range must pass (exit 0):\nexit=%d\n%s", code, out)
	}

	// N1: a path with a colon.
	write("docs/plan/notes:12.md", a0())
	colon := commit("colon")
	out, code = hook(br, colon, clean)
	mustFail("a value in a file whose path has a colon", out, code, "docs/plan/notes:12.md:1: [a0-token]")

	// N2: a -diff attribute, committed and repo-local.
	write(".gitattributes", "*.md -diff\n")
	write("docs/plan/attr.md", a0())
	attr := commit("attr")
	out, code = hook(br, attr, colon)
	mustFail("a .md file a committed .gitattributes calls binary", out, code, "docs/plan/attr.md:1: [a0-token]")
	git("rm", "-q", ".gitattributes")
	noAttr := commit("drop attributes")
	write("docs/plan/info.md", a0())
	info := commit("info")
	write(filepath.Join(".git", "info", "attributes"), "*.md -diff\n")
	out, code = hook(br, info, noAttr)
	mustFail("a .md file .git/info/attributes calls binary", out, code, "docs/plan/info.md:1: [a0-token]")
	if err := os.Remove(filepath.Join(dir, ".git", "info", "attributes")); err != nil {
		t.Fatal(err)
	}
	// Control for `--text`: a file whose CONTENT has a NUL byte is binary (the same
	// rule rg applies to the tree) and is skipped, whatever its bytes look like —
	// the full-history run found a font whose random bytes read as an assignment.
	write("font.bin", "\x00\x01OTTO\nDB"+"_PASS="+r7dPW3+"\n\x00\n")
	bin := commit("binary")
	if out, code := hook(br, bin, info); code != 0 {
		t.Fatalf("a binary (NUL-bearing) file must be skipped like the tree scan skips it:\nexit=%d\n%s", code, out)
	}

	// B1 in the hook: the weak dev value passes (never caught since the 4th round);
	// extended into a strong one, it stops.
	write("docker-compose.yml", "    POSTGRES_PASS"+"WORD: tappa\n")
	devOK := commit("dev compose")
	if out, code := hook(br, devOK, bin); code != 0 {
		t.Fatalf("control: the weak dev value must pass the hook:\nexit=%d\n%s", code, out)
	}
	write("docker-compose.yml", "    POSTGRES_PASS"+"WORD: tappaXk9\n")
	devExt := commit("dev compose extended")
	out, code = hook(br, devExt, devOK)
	mustFail("an extended dev token (B1)", out, code, "docker-compose.yml:1: [pw-assign]")

	// N5: a merge whose second parent is unpushed, and an EVIL merge — a line that
	// is in neither parent and exists only in the merge commit.
	git("checkout", "-q", "-b", "side", devExt)
	write("side.txt", "k="+r7dAKIA+"\n")
	commit("side")
	git("checkout", "-q", "main")
	write("main.txt", "m\n")
	commit("main")
	git("merge", "-q", "--no-ff", "--no-commit", "side")
	write("evil.md", a0())
	evil := commit("merge side")
	out, code = hook(br, evil, devExt)
	mustFail("the unpushed side of a merge", out, code, "side.txt:1: [aws-key]")
	mustFail("an evil merge (a line only the merge commit adds)", out, code, "evil.md:1: [a0-token]")

	// N3: an annotated tag's message.
	git("tag", "-a", "v1", "-m", "release", "-m", "rotate tok"+"en "+r7dBT+r7dPW+r7dBT, clean)
	tagObj := git("rev-parse", "v1")
	out, code = hook("refs/tags/v1", tagObj, zero)
	mustFail("a value in an annotated TAG message", out, code, "tag-msg@"+tagObj[:12]+":3: [a0-token]")

	// Every way the scan can break is exit 2, never exit 0 (B2): a broken rg, and an
	// awk that crashes inside the pipeline (the dedupe step, the diff reader).
	shim := func(name, body string) string {
		t.Helper()
		d := t.TempDir()
		if err := os.WriteFile(filepath.Join(d, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return "PATH=" + d + string(os.PathListSeparator) + os.Getenv("PATH")
	}
	// 3rd round, finding 7: a temp file that cannot be made is exit 2 WITH a reason.
	out, code = hook(br, removed, base, "TMPDIR="+filepath.Join(dir, "no-such-dir"))
	// The hook's own sentence, not mktemp's: that one alone says nothing about a push.
	if code != 2 || !strings.Contains(out, "gecici dosya yaratilamadi") {
		t.Fatalf("an unusable TMPDIR must fail CLOSED with the hook's own reason:\nexit=%d\n%s", code, out)
	}
	out, code = hook(br, removed, base, shim("rg", "#!/bin/sh\nexit 2\n"))
	if code != 2 || !strings.Contains(out, "ripgrep (rg)") {
		t.Fatalf("with a broken rg the hook must fail CLOSED (exit 2, naming rg):\nexit=%d\n%s", code, out)
	}
	for _, crashOn := range []string{"seen[$0]++", "inhunk"} {
		env := shim("awk", "#!/bin/sh\ncase \"$*\" in *'"+crashOn+"'*) exit 3;; esac\nexec '"+realAwk+"' \"$@\"\n")
		out, code = hook(br, removed, base, env)
		if code != 2 {
			t.Fatalf("an awk crash in the pipeline (%s) must fail CLOSED (exit 2), not read as \"no hits\":\nexit=%d\n%s", crashOn, code, out)
		}
	}
}

// TestPrePush_ThirdRoundFindings pins what the third-round audit measured in the hook:
// a text file carrying a 0x01 byte used to be skipped as "binary"; the root commit was
// skipped under log.showRoot=false; a non-ASCII path must be reported as written (not
// octal-escaped); and a grammatically extended dev value must fail in the hook as it
// does in the tree scan.
func TestPrePush_ThirdRoundFindings(t *testing.T) {
	r := newHookRepo(t)
	r.git("config", "log.showRoot", "false")
	r.write("docs/plan/root.md", a0Line())
	root := r.commit("root")
	out, code := r.hook("refs/heads/b", root, zeroSHA)
	r.mustFail("a value in the ROOT commit (log.showRoot=false)", out, code, "docs/plan/root.md:1: [a0-token]")

	r.write("docs/plan/ctl.md", a0Line()+"\x01 control byte on its own line\n")
	ctl := r.commit("control byte")
	out, code = r.hook("refs/heads/b", ctl, root)
	r.mustFail("a text file that carries a 0x01 byte (no NUL)", out, code, "docs/plan/ctl.md:1: [a0-token]")

	r.write("docs/plan/şema.md", a0Line())
	nonASCII := r.commit("non-ascii path")
	out, code = r.hook("refs/heads/b", nonASCII, ctl)
	r.mustFail("a non-ASCII path, reported unescaped", out, code, "docs/plan/şema.md:1: [a0-token]")

	var compose strings.Builder
	for _, sep := range []string{" ", "\t", ",", ";", "'", `"`, "(", `\`} {
		compose.WriteString("    POSTGRES_PASS" + "WORD: tappa" + sep + "Zq9w!xLm3\n")
	}
	r.write("docker-compose.yml", compose.String())
	ext := r.commit("extended dev values")
	out, code = r.hook("refs/heads/b", ext, nonASCII)
	if code != 1 || strings.Count(out, "docker-compose.yml:") != 8 {
		t.Fatalf("all eight grammatically extended dev values must stop the push:\nexit=%d\n%s", code, out)
	}
	assertNoValue(t, out)
}

// TestPrePush_NestedTagDepthIsBounded pins the limit the hook states: up to eight
// nested annotated tags are read; a ninth level cannot be scanned and is exit 2 —
// never a silent pass. (The second round said "deeper than 8" and failed at 8.)
func TestPrePush_NestedTagDepthIsBounded(t *testing.T) {
	r := newHookRepo(t)
	base := r.commit("base")
	prev := base
	objs := map[int]string{}
	for level := 1; level <= 9; level++ {
		name := fmt.Sprintf("t%d", level)
		r.git("tag", "-a", name, "-m", "level "+strconv.Itoa(level), prev)
		prev = r.git("rev-parse", name)
		objs[level] = prev
	}
	if out, code := r.hook("refs/tags/t8", objs[8], zeroSHA); code != 0 {
		t.Fatalf("eight nested tags with clean messages must pass (exit 0):\nexit=%d\n%s", code, out)
	}
	if out, code := r.hook("refs/tags/t9", objs[9], zeroSHA); code != 2 {
		t.Fatalf("a ninth level must fail CLOSED (exit 2), not pass:\nexit=%d\n%s", code, out)
	}
}

// TestPrePush_ReplaceRefsDoNotHideWhatIsPushed — 5th round, N6. `git replace <bad>
// <clean>` makes `git log -p <bad>` show the clean twin, but `git push` sends the
// ORIGINAL objects. The hook reads what is pushed (GIT_NO_REPLACE_OBJECTS=1).
func TestPrePush_ReplaceRefsDoNotHideWhatIsPushed(t *testing.T) {
	r := newHookRepo(t)
	base := r.commit("base")
	r.write("docs/plan/r.md", a0Line())
	bad := r.commit("notes")
	r.git("checkout", "-q", "--detach", base)
	r.write("docs/plan/r.md", "nothing to see\n")
	clean := r.commit("notes")
	r.git("replace", bad, clean)
	if got := r.git("log", "-p", "--format=", bad, "--", "docs/plan/r.md"); !strings.Contains(got, "nothing to see") {
		t.Fatalf("setup: the replace ref is not in effect, so this test proves nothing:\n%s", got)
	}
	out, code := r.hook("refs/heads/b", bad, base)
	r.mustFail("a commit shown through a git replace ref", out, code, "docs/plan/r.md:1: [a0-token]")
}

// TestPrePush_AMessageLineStartingWithAControlByteIsScanned — 4th round, N5: the hook
// separates commit messages with 0x01 + sha, and a message LINE that merely starts with
// 0x01 used to be taken for a separator and skipped.
func TestPrePush_AMessageLineStartingWithAControlByteIsScanned(t *testing.T) {
	r := newHookRepo(t)
	base := r.commit("base")
	r.write("x.txt", "x\n")
	c := r.commit("chore", "\x01"+"rotate tok"+"en "+r7dBT+r7dPW+r7dBT)
	out, code := r.hook("refs/heads/b", c, base)
	r.mustFail("a message line that starts with 0x01", out, code, "commit-msg@")
}

// TestPrePush_AWaivedLineIsBoundToItsBytes — 7th round, end to end in the hook. The
// waived signup.go line, byte for byte on its path, passes; the 6th-round auditor's
// line next to it (a strong value glued to the example password: silent then, push
// rc=0) stops the push; and the waived line with one trailing blank is no longer
// the waived line.
func TestPrePush_AWaivedLineIsBoundToItsBytes(t *testing.T) {
	const p = "internal/domain/signup/signup.go"
	var line string
	for _, w := range waivedLines(t) {
		if w.path == p {
			line = w.text
		}
	}
	if line == "" {
		t.Fatalf("no waived line of %s in the table", p)
	}
	r := newHookRepo(t)
	base := r.commit("base")
	r.write(p, line+"\n")
	ok := r.commit("the waived line, byte for byte")
	if out, code := r.hook("refs/heads/b", ok, base); code != 0 {
		t.Fatalf("the waived line must pass the hook:\nexit=%d\n%s", code, out)
	}
	r.write(p, line+"\n"+"// example := \"Zq9w!xLm3Kp7="+"Pass"+"word1!"+"\"\n")
	second := r.commit("a second line")
	out, code := r.hook("refs/heads/b", second, ok)
	r.mustFail("the 6th-round end-to-end line", out, code, p+":2: [a0-token]")
	if !strings.Contains(out, "secretscan.sh --hash") {
		t.Fatalf("the hook's FAIL message must say how a harmless line is waived (--hash):\n%s", out)
	}
	r.write(p, line+" \n")
	blank := r.commit("one trailing blank")
	out, code = r.hook("refs/heads/b", blank, ok)
	r.mustFail("the waived line with one trailing blank", out, code, p+":1: [")
}

// TestPrePush_UserGitSettingsDoNotChangeWhatIsRead — 8th round. The hook parses
// `git log` output; a user's local git settings used to change that output: a
// destination prefix turned `deploy/k8s/…` into `X/deploy/k8s/…` (the `$` context
// fell back to shell, the value was silent, rc=0) and a UTF-16 log output encoding
// hid an A-0 commit message (rc=0). Every setting below must leave the result alone.
func TestPrePush_UserGitSettingsDoNotChangeWhatIsRead(t *testing.T) {
	for _, cfg := range [][2]string{
		{"diff.dstPrefix", "X/"}, {"diff.srcPrefix", "Y/"}, {"diff.noprefix", "true"},
		{"diff.mnemonicPrefix", "true"}, {"diff.relative", "true"}, {"diff.submodule", "log"},
		{"i18n.logOutputEncoding", "UTF-16LE"}, {"i18n.logOutputEncoding", "UTF-16"},
		{"log.showSignature", "true"}, {"log.decorate", "full"}, {"format.pretty", "oneline"},
		{"color.ui", "always"}, {"color.diff", "always"}, {"core.quotePath", "true"},
	} {
		t.Run(cfg[0]+"="+cfg[1], func(t *testing.T) {
			r := newHookRepo(t)
			r.git("config", cfg[0], cfg[1])
			base := r.commit("base")
			r.write("deploy/k8s/05-config.yaml", "  DB_PASS"+"WORD: $"+"Kx9!pQ2w\n")
			k8s := r.commit("k8s manifest")
			out, code := r.hook("refs/heads/b", k8s, base)
			r.mustFail("a literal $ value in a k8s manifest", out, code, "deploy/k8s/05-config.yaml:1: [pw-assign]")
			r.write("x.txt", "x\n")
			msg := r.commit("chore", "rotate tok"+"en "+r7dBT+r7dPW+r7dBT)
			out, code = r.hook("refs/heads/b", msg, k8s)
			r.mustFail("an A-0 commit message", out, code, "commit-msg@")
		})
	}
}

// TestSecretScan_TheHashHelperKeepsCRAndTrailingBlanks — 8th round. The helper must
// hash the same bytes the scanner hashes: a "\r" and trailing blanks are part of the
// line. Its row, pasted into a table, waives that line and only that line.
func TestSecretScan_TheHashHelperKeepsCRAndTrailingBlanks(t *testing.T) {
	dir := t.TempDir()
	dsn := "postgres://u" + ":p@h/db"
	lines := []string{dsn + "  \r", dsn + "\t", dsn + "\r", dsn + " "}
	body := "head\n" + strings.Join(lines, "\n") + "\n"
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "fixture.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	for i, l := range lines {
		no := i + 2
		cmd := exec.Command("bash", filepath.Join(repoRoot, "scripts", "secretscan.sh"), "--hash", fmt.Sprintf("docs/fixture.md:%d", no))
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("--hash: %v\n%s", err, out)
		}
		head := "^docs/fixture[.]md$;" + lineSHA(l) + ";"
		if !strings.HasPrefix(string(out), head) {
			t.Fatalf("line %d: the helper printed %q, want the head %q (the scanner's hash of the line bytes)", no, out, head)
		}
		table := head + "sentetik test satiri"
		run := func(mode, text string) string {
			c := exec.Command("bash", "-c", `. scripts/secretscan.sh && R7D_WAIVERS="$1" && r7d_select "$2"`, "r7d", table, mode)
			c.Dir = repoRoot
			c.Stdin = strings.NewReader(rec("docs/fixture.md", no, text) + "\n")
			o, err := c.CombinedOutput()
			if err != nil {
				t.Fatalf("r7d_select %s: %v\n%s", mode, err, o)
			}
			return string(o)
		}
		if got := run("waived", l); got != fmt.Sprintf("docs/fixture.md:%d: [muaf]\n", no) {
			t.Errorf("line %d: the helper's row does not waive its own line: %q", no, got)
		}
		if got := run("fail", strings.TrimRight(l, " \t\r")); !strings.Contains(got, "[url-cred]") {
			t.Errorf("line %d: the row also waives the line without its trailing bytes: %q", no, got)
		}
	}
}

// TestRedline_TheR7dFailMessageSaysHowToWaive — 8th round: a harmless edit of a
// waived line breaks CI (the waiver is bound to the line's bytes), so redline-check's
// R7d FAIL message, like the hook's, names the helper that prints the new row.
func TestRedline_TheR7dFailMessageSaysHowToWaive(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "redline-check.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, ln := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(ln, "report FAIL R7d ") {
			if !strings.Contains(ln, "bash scripts/secretscan.sh --hash <dosya>:<satir>") || !strings.Contains(ln, "R7D_WAIVERS") {
				t.Fatalf("the R7d FAIL message does not name the --hash helper and the table:\n%s", ln)
			}
			return
		}
	}
	t.Fatal("no `report FAIL R7d` line in scripts/redline-check.sh")
}

// TestSecretScan_ThePrintfRuleSilencesNoRealisticValue — 9th round, blocking. The 8th round's
// printf rule took `KEY=` + any mix of letters, digits and one `%<letter>` for code,
// and 13 values the 7th round caught went silent (measured). The rule now takes only
// printf verbs joined by non-alphanumeric separators. Pinned two ways: the 13 values
// are red, and on a SEEDED SAMPLE of realistic random values the rule changes nothing
// — the same lines are reported with the rule as with the rule cut out of a copy of
// the classifier. It says nothing beyond that sample: two shapes the rule does
// silence (a named verb around an identifier, a value made only of verbs) are counted
// in scripts/secretscan.sh as limit #27.
func TestSecretScan_ThePrintfRuleSilencesNoRealisticValue(t *testing.T) {
	tk, dq := "TOK"+"EN", `"`
	probes := []struct{ path, text string }{
		{"internal/x/a.go", "\tv := " + dq + "SESSION_" + tk + "=Ab3%d9Xz_Qp" + dq},
		{"internal/x/a.go", "\tq := fmt.Sprintf(" + dq + "SESSION_" + tk + "=Ab3%d9Xz_Qp" + dq + ", v)"},
		{"docs/x.md", r7dBT + "API_" + tk + "=Ab3%d9Xz_Qp" + r7dBT},
		{"docs/x.md", r7dBT + "SESSION_" + tk + "=Ab3%d9Xz_Qp" + r7dBT},
		{"docs/x.md", r7dBT + "DB_" + tk + "=Zq9%wxLm3Kp7" + r7dBT},
		{"docs/x.md", strings.ToLower(tk) + " " + r7dBT + "svc-ops:Kx9%pQ2w" + r7dBT},
		{"docs/x.md", r7dBT + tk + "=Kx9%pQ2wLm" + r7dBT},
		{"docs/x.md", r7dBT + tk + "=Ab3@x%sQ9w" + r7dBT},
		{"internal/x/a.go", "\tv := " + dq + "SESSION_" + tk + "=p4ss%20wordX" + dq},
		{"internal/x/a.go", "\tv := " + dq + "SESSION_" + tk + "=P4ss%20word" + dq},
		{"internal/x/a.go", "\tv := " + dq + "PGPASS" + "WORD=%sZq9wxLm3Kp7" + dq},
		{"docs/x.md", dq + "PGPASS" + "WORD=%sZq9wxLm3Kp7" + dq},
		{"internal/x/a.go", "\tv := " + dq + "SESSION_" + tk + "=%sZq9wxLm3Kp7" + dq},
	}
	var lines []string
	for i, p := range probes {
		lines = append(lines, rec(p.path, i+1, p.text))
	}
	got := r7dSelect(t, "fail", lines...)
	for i, p := range probes {
		if !strings.Contains(got, fmt.Sprintf("%s:%d: [", p.path, i+1)) {
			t.Errorf("probe %d (%s) is silent: a strong value next to a printf verb", i+1, p.path)
		}
	}
	assertNoValue(t, got)

	// The same classifier with the printf rule cut out, from a copy.
	src, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "secretscan.sh"))
	if err != nil {
		t.Fatal(err)
	}
	var kept []string
	cut := 0
	for _, ln := range strings.Split(string(src), "\n") {
		if strings.Contains(ln, "YALNIZ fiil + alfanumerik OLMAYAN ayrac") {
			cut++
			continue
		}
		kept = append(kept, ln)
	}
	if cut != 1 {
		t.Fatalf("found the printf rule %d times in scripts/secretscan.sh, want exactly once", cut)
	}
	off := filepath.Join(t.TempDir(), "secretscan.sh")
	if err := os.WriteFile(off, []byte(strings.Join(kept, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	selectWith := func(script string, recs []string) string {
		cmd := exec.Command("bash", "-c", `. "$1" && r7d_select fail`, "r7d", script)
		cmd.Dir = repoRoot
		cmd.Stdin = strings.NewReader(strings.Join(recs, "\n") + "\n")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("r7d_select with %s: %v\n%s", script, err, out)
		}
		return string(out)
	}
	for _, alphabet := range []string{
		"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789%",
		"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789%@-_.",
	} {
		x := uint32(20260925)
		var sample []string
		for i := 0; i < 300; i++ {
			b := make([]byte, 12)
			for j := range b {
				x = x*1664525 + 1013904223
				b[j] = alphabet[int(x>>8)%len(alphabet)]
			}
			sample = append(sample, rec("internal/x/a.go", i+1, "\tv := fmt.Sprintf("+dq+"JWT_SEC"+"RET="+string(b)+dq+", x)"))
		}
		on, offOut := selectWith(filepath.Join(repoRoot, "scripts", "secretscan.sh"), sample), selectWith(off, sample)
		if on != offOut {
			t.Errorf("alphabet %q: the printf rule silences values (%d reported with it, %d without)", alphabet, strings.Count(on, "\n"), strings.Count(offOut, "\n"))
		}
		t.Logf("alphabet %q: %d of 300 reported, with and without the printf rule", alphabet, strings.Count(on, "\n"))
	}
}

// TestSecretScan_ATableErrorDoesNotPrintTheRow — 9th round (N5): a value written into
// any field of a table row by mistake must not reach stderr (a CI log in a public
// repository). The error names the table line NUMBER and the broken field only.
func TestSecretScan_ATableErrorDoesNotPrintTheRow(t *testing.T) {
	h := strings.Repeat("ab", 32)
	v := "Kx9!" + "pQ2wLm3Zq"
	for _, table := range []string{
		"^x$;" + v + ";d", "^" + v + "$;" + h + ";d", "^x$;" + h + ";eski " + v,
		"^x$;" + h + ";" + "par" + "ola " + v, v, "^x$;" + h + ";d\n^x$;" + h + ";" + v,
	} {
		cmd := exec.Command("bash", "-c", `. scripts/secretscan.sh && R7D_WAIVERS="$1" && r7d_select fail`, "r7d", table)
		cmd.Dir = repoRoot
		cmd.Stdin = strings.NewReader(rec("docs/x.md", 1, "x") + "\n")
		out, err := cmd.CombinedOutput()
		ee, ok := err.(*exec.ExitError)
		if !ok || ee.ExitCode() != 2 {
			t.Fatalf("table with a value in a field: want exit 2, got err=%v", err)
		}
		if strings.Contains(string(out), v) || strings.Contains(string(out), "pQ2wLm3Zq") {
			t.Fatalf("the table error printed the value:\n%s", out)
		}
		if !strings.Contains(string(out), "satiri bozuk") {
			t.Fatalf("the table error does not name the line and the field:\n%s", out)
		}
	}
}

// hookFunc extracts one shell function from scripts/git-hooks/pre-push, so it can be
// driven on its own input.
func hookFunc(t *testing.T, name string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "git-hooks", "pre-push"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	i := strings.Index(s, "\n"+name+"() {\n")
	if i < 0 {
		t.Fatalf("%s() not found in the hook", name)
	}
	j := strings.Index(s[i+1:], "\n}\n")
	if j < 0 {
		t.Fatalf("%s() has no closing brace", name)
	}
	return s[i+1 : i+1+j+2]
}

// TestPrePush_ContextLinesKeepLineNumbers — 9th round (N2). `diff.interHunkContext`
// and `GIT_DIFF_OPTS=--unified=5` made git print context lines despite -U0, and the
// reported line number was wrong (detection was not affected). Both are neutralised in
// the hook, and diff_lines counts a context line anyway (defence in depth): each is
// pinned on its own here.
func TestPrePush_ContextLinesKeepLineNumbers(t *testing.T) {
	var base strings.Builder
	for i := 1; i <= 10; i++ {
		fmt.Fprintf(&base, "l%d\n", i)
	}
	lines := strings.Split(strings.TrimSuffix(base.String(), "\n"), "\n")
	next := strings.Join(lines[:2], "\n") + "\nx\n" + strings.Join(lines[2:6], "\n") + "\n" + strings.TrimSuffix(a0Line(), "\n") + "\n" + strings.Join(lines[6:], "\n") + "\n"
	for _, setting := range []struct {
		name string
		cfg  [2]string
		env  string
	}{
		{"diff.interHunkContext=10", [2]string{"diff.interHunkContext", "10"}, ""},
		{"GIT_DIFF_OPTS=--unified=5", [2]string{}, "GIT_DIFF_OPTS=--unified=5"},
	} {
		t.Run(setting.name, func(t *testing.T) {
			r := newHookRepo(t)
			if setting.cfg[0] != "" {
				r.git("config", setting.cfg[0], setting.cfg[1])
			}
			r.write("docs/plan/f.md", base.String())
			b := r.commit("base")
			r.write("docs/plan/f.md", next)
			c := r.commit("two lines")
			var env []string
			if setting.env != "" {
				env = append(env, setting.env)
			}
			out, code := r.hook("refs/heads/b", c, b, env...)
			r.mustFail("an A-0 line under "+setting.name, out, code, "docs/plan/f.md:8: [a0-token]")
		})
	}
	// diff_lines on its own, fed a hunk WITH context lines.
	in := "\x01" + strings.Repeat("a", 40) + "\ndiff --git a/f.md b/f.md\nindex 1111111..2222222 100644\n--- a/f.md\n+++ b/f.md\n@@ -1,10 +1,12 @@\n l1\n l2\n+x\n l3\n l4\n l5\n l6\n+y\n"
	cmd := exec.Command("bash", "-c", hookFunc(t, "diff_lines")+"\ndiff_lines /dev/null")
	cmd.Stdin = strings.NewReader(in)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("diff_lines: %v\n%s", err, out)
	}
	if want := "f.md\x1f3\x1fx\nf.md\x1f8\x1fy\n"; string(out) != want {
		t.Fatalf("diff_lines numbered a hunk with context lines wrong: %q, want %q", out, want)
	}
}

// TestPrePush_CommitEncodingDoesNotHideAMessage — 9th round (N1). A commit written
// under `i18n.commitEncoding` carries an `encoding` header; when it lies (UTF-8 bytes,
// header UTF-16LE/UTF-16/UTF-32/IBM037), `git log --encoding=UTF-8` turned the
// message into noise and the hook passed, while the raw object that is pushed carries
// the value. The hook now also reads every message raw (git cat-file --batch).
func TestPrePush_CommitEncodingDoesNotHideAMessage(t *testing.T) {
	for _, enc := range []string{"UTF-16LE", "UTF-16", "UTF-32", "IBM037"} {
		t.Run(enc, func(t *testing.T) {
			r := newHookRepo(t)
			base := r.commit("base")
			r.git("config", "i18n.commitEncoding", enc)
			r.write("x.txt", "x\n")
			c := r.commit("chore", "rotate tok"+"en "+r7dBT+r7dPW+r7dBT)
			out, code := r.hook("refs/heads/b", c, base)
			r.mustFail("an A-0 commit message under i18n.commitEncoding="+enc, out, code, "commit-msg@")
		})
	}
}

// writeObject stores raw object bytes with `git hash-object` (`--literally` when the
// bytes are not a well-formed object git would write itself) and returns its name.
func (r *hookRepo) writeObject(kind string, body []byte, literally bool) string {
	r.t.Helper()
	f := filepath.Join(r.t.TempDir(), "obj")
	if err := os.WriteFile(f, body, 0o644); err != nil {
		r.t.Fatal(err)
	}
	args := []string{"hash-object", "-t", kind, "-w"}
	if literally {
		args = append(args, "--literally")
	}
	return r.git(append(args, f)...)
}

// commitHeader is the header of a hand-built commit object on top of parent.
func (r *hookRepo) commitHeader(parents ...string) string {
	h := "tree " + r.git("rev-parse", parents[0]+"^{tree}") + "\n"
	for _, p := range parents {
		h += "parent " + p + "\n"
	}
	return h + "author t <t@example.test> 1700000000 +0000\ncommitter t <t@example.test> 1700000000 +0000\n"
}

// TestPrePush_AMergedSignedTagMessageIsScanned — 10th round (B2). Merging a SIGNED tag
// (`git merge --no-ff -m "Merge release" v3`) embeds the whole tag object, message
// included, in the merge commit's `mergetag` header, and the push publishes it; with
// a custom merge message the value lived only there and the hook passed (rc=0,
// measured with an ssh-signed tag). The raw pass now reads the header's continuation
// lines. The commit is built by hand (what git merge writes), so no signing key is
// needed.
func TestPrePush_AMergedSignedTagMessageIsScanned(t *testing.T) {
	r := newHookRepo(t)
	base := r.commit("base")
	r.git("checkout", "-q", "-b", "rel")
	r.write("r.txt", "r\n")
	rel := r.commit("release")
	r.git("checkout", "-q", "main")
	r.write("m.txt", "m\n")
	main := r.commit("main")
	obj := r.commitHeader(main, rel) +
		"mergetag object " + rel + "\n type commit\n tag v3\n tagger t <t@example.test> 1700000000 +0000\n \n release notes\n \n" +
		" rotate tok" + "en " + r7dBT + r7dPW + r7dBT + "\n\nMerge release\n"
	merge := r.writeObject("commit", []byte(obj), false)
	out, code := r.hook("refs/heads/b", merge, base)
	r.mustFail("a value in a merged tag's embedded message", out, code, "mergetag@"+merge[:12]+":")
}

// TestPrePush_TheRawMessagePassIsPinned — 10th round (B4): the two guards of the raw
// message pass (9th round, N1) each have a case of their own.
//   - A truncated `git cat-file --batch` stream (the object shorter than its declared
//     size) is a scan that did not happen: exit 2, not a clean pass.
//   - A message whose bytes really are UTF-16LE (no encoding header, built with
//     `hash-object --literally`) is still read: its NULs are dropped.
func TestPrePush_TheRawMessagePassIsPinned(t *testing.T) {
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	r := newHookRepo(t)
	base := r.commit("base")
	r.write("x.txt", "x\n")
	c := r.commit("an ordinary message long enough to be cut")
	d := t.TempDir()
	shim := "#!/bin/sh\ncase \"$*\" in *cat-file*--batch*) '" + realGit + "' \"$@\" | head -c 60; exit 0;; esac\nexec '" + realGit + "' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(d, "git"), []byte(shim), 0o755); err != nil {
		t.Fatal(err)
	}
	out, code := r.hook("refs/heads/b", c, base, "PATH="+d+string(os.PathListSeparator)+os.Getenv("PATH"))
	if code != 2 {
		t.Fatalf("a truncated cat-file --batch stream must fail the scan (exit 2):\nexit=%d\n%s", code, out)
	}

	msg := "rotate tok" + "en " + r7dBT + r7dPW + r7dBT + "\n"
	u := utf16.Encode([]rune(msg))
	le := make([]byte, 2*len(u))
	for i, x := range u {
		binary.LittleEndian.PutUint16(le[2*i:], x)
	}
	obj := append([]byte(r.commitHeader(c)+"\n"), le...)
	utf := r.writeObject("commit", obj, true)
	out, code = r.hook("refs/heads/b", utf, c)
	r.mustFail("a message whose bytes are UTF-16LE", out, code, "commit-msg@"+utf[:12]+":")
}

// synthKey returns n deterministic pseudo-random bytes (a sha256 chain), the shape of
// a KEK or an NTAG AES key and nothing else.
func synthKey(seed string, n int) []byte {
	var out []byte
	h := sha256.Sum256([]byte(seed))
	for len(out) < n {
		out = append(out, h[:]...)
		h = sha256.Sum256(h[:])
	}
	return out[:n]
}

// TestSecretScan_KeyValuesAreCaughtInTheirOwnWords — 11th round (§4.7, the heart of
// it). A KEK or an NTAG AES key is not called a password: "canli KEK
// (TAPPA_TAG_KEK)", "plaket anahtari (key 1)". The security auditor measured six such
// shapes, all silent, and only two of the product's seven secret names were known to
// the assignment rule. Key context words now count, but ONLY next to a key-shaped
// value (>=32 hex, >=32 letters+digits, base64 of 32 or 16 bytes), and kek/hmac_key
// are assignment keys; names, references, placeholders and git hashes stay quiet.
func TestSecretScan_KeyValuesAreCaughtInTheirOwnWords(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString(synthKey("r7d kek", 32))
	b64r := base64.RawStdEncoding.EncodeToString(synthKey("r7d kek raw", 32))
	b64s := base64.StdEncoding.EncodeToString(synthKey("r7d aes-128", 16))
	hex16 := hex.EncodeToString(synthKey("r7d tag key", 16))
	hex32 := hex.EncodeToString(synthKey("r7d hmac", 32))
	sha1 := hex.EncodeToString(synthKey("r7d revision", 20))
	for _, v := range []string{b64, b64r, b64s} {
		if !regexp.MustCompile(`[A-Z]`).MatchString(v) || !regexp.MustCompile(`[a-z]`).MatchString(v) || !regexp.MustCompile(`[0-9]`).MatchString(v) {
			t.Fatalf("synthetic base64 %d chars lacks a character class; change its seed", len(v))
		}
	}
	bt, dq := r7dBT, `"`
	kek, key := "KEK", "k"+"ey"
	cases := []struct {
		name, path, text string
		red              bool
	}{
		{"live KEK in prose", "docs/x.md", "canlı " + kek + " (TAPPA_TAG_" + kek + "): " + bt + b64 + bt, true},
		{"plaque AES key in prose", "docs/x.md", "plaket anahtarı (" + key + " 1): " + bt + hex16 + bt, true},
		{"AES-128 key, 16-byte base64", "docs/x.md", "AES-128 plaket anahtarı: " + bt + b64s + bt, true},
		{"KEK, unpadded base64", "docs/x.md", strings.ToLower(kek) + " " + bt + b64r + bt, true},
		{"TAPPA_TAG_KEK assignment", "scripts/x.sh", "export TAPPA_TAG_" + kek + "=" + b64, true},
		{"HMAC key assignment in a script", "scripts/x.sh", "TAPPA_SESSION_HMAC_" + strings.ToUpper(key) + "=" + hex32 + " ./tappa", true},
		{"HMAC key in YAML", "deploy/x.yaml", "  TAPPA_INVITE_HMAC_" + strings.ToUpper(key) + ": " + b64, true},
		{"HMAC key in a YAML env list", "deploy/x.yaml", "  - TAPPA_SESSION_HMAC_" + strings.ToUpper(key) + "=" + hex32, true},
		// 12th round: each context word ALONE on its line (a mutation that drops one
		// word from R7D_KEYCTX, or kek from the markdown rule, is red here).
		{"only 'key'", "docs/x.md", "rotated " + key + ": " + bt + hex16 + bt, true},
		{"only 'anahtar' (Turkish, inflected)", "docs/x.md", "plaket anahtarı: " + bt + hex16 + bt, true},
		{"only 'aes'", "docs/x.md", "AES-128: " + bt + b64s + bt, true},
		{"only 'hmac'", "docs/x.md", "HMAC: " + bt + hex32 + bt, true},
		// (Unquoted markdown uses the code-shape filter too; a `=`-padded base64 with no
		// `+`/`/` reads as `name=` there — limit #13 — so these use a hex value.)
		{"only the markdown kek key, unquoted", "docs/x.md", "- TAPPA_TAG_" + kek + ": " + hex32, true},
		// 12th round: kek/hmac_key followed by a rotation suffix, in .sh, .md and k8s YAML.
		{"KEK with a _PREVIOUS suffix", "scripts/x.sh", "TAPPA_TAG_" + kek + "_PREVIOUS=" + b64, true},
		{"KEK with an _OLD suffix in markdown", "docs/x.md", "TAG_" + kek + "_OLD: " + hex32, true},
		{"KEK with an _OLD suffix in a k8s manifest", "deploy/k8s/05-config.yaml", "  TAG_" + kek + "_OLD: " + b64, true},
		{"HMAC key with an _OLD suffix", "scripts/x.sh", "TAPPA_SESSION_HMAC_" + strings.ToUpper(key) + "_OLD=" + hex32, true},
		// 12th round: the context word inside a camelCase name.
		{"camelCase tagKey", "internal/x/a.go", "\ttag" + "Key := " + dq + hex16 + dq, true},
		{"camelCase prodKEK", "internal/x/a.go", "\tprod" + kek + " = " + dq + b64 + dq, true},
		{"camelCase sessionHMACKey", "internal/x/a.go", "\tsessionHMAC" + "Key = " + dq + b64 + dq, true},
		{"camelCase plaqueKey", "internal/x/a.go", "\tplaque" + "Key: " + dq + hex16 + dq + ",", true},
		{"camelCase newKey", "internal/x/a.go", "\tnew" + "Key = " + dq + hex16 + dq, true},
		{"control: a log field named kek_*", "deploy/README.md", "logs | grep -c '" + strings.ToLower(kek) + "_rotation_window=open'     # 0", false},
		{"control: camelCase key names next to code", "internal/x/a.go", "\tcache" + "Key := " + dq + "sessions/by-user" + dq + "; hash" + "Key := sha256.Sum256(b); apiKeyName := " + dq + "X-Api-Key" + dq, false},
		{"control: the name in prose", "docs/x.md", bt + "TAPPA_TAG_" + kek + bt + " Secret'tan gelir; " + key + " rotation: " + bt + "docs/runbook.md" + bt, false},
		{"control: empty .env value", ".env.example", "TAPPA_TAG_" + kek + "=", false},
		{"control: placeholder .env value", ".env.example", "TAPPA_TAG_" + kek + "=REPLACE_ME", false},
		{"control: shell reference", "scripts/x.sh", "TAPPA_TAG_" + kek + "=${TAPPA_TAG_" + kek + ":?}", false},
		{"control: k8s secretKeyRef", "deploy/k8s/20-app.yaml", "            secret" + "KeyRef:", false},
		{"control: k8s key name", "deploy/k8s/20-app.yaml", "              " + key + ": TAPPA_TAG_" + kek, false},
		{"control: Go field reference", "internal/x/a.go", "\t" + strings.ToLower(kek) + " := cfg.Tag" + kek + " // tag " + key, false},
		{"control: Go env lookup", "internal/x/a.go", "\tv := os.Getenv(\"TAPPA_TAG_" + kek + "\") // " + key, false},
		{"control: git revision next to Key", "internal/x/a.go", "\t{" + "K" + "ey: \"vcs.revision\", Value: \"" + sha1 + "\"},", false},
		{"control: short code next to key", "docs/x.md", "the " + key + " " + bt + "ErrKeyNotFound" + bt + " and " + bt + "sha256" + bt, false},
		{"control: hex fill next to key", "docs/x.md", key + " " + bt + strings.Repeat("0123456789abcdef", 2) + bt, false},
		// base64-LENGTH shapes that are not keys: a 22-letter identifier (no digit) and a
		// periodic 43-character fill.
		{"control: 22-letter identifier next to key", "docs/x.md", "the " + key + " " + bt + "ErrKeyIsNotFoundHereXY" + bt, false},
		{"control: periodic fill next to key", "docs/x.md", key + " " + bt + strings.Repeat("Xy7Zq9Wv3", 5)[:43] + bt, false},
	}
	var lines []string
	for i, tc := range cases {
		lines = append(lines, rec(tc.path, i+1, tc.text))
	}
	got := r7dSelect(t, "fail", lines...)
	for i, tc := range cases {
		if hit := strings.Contains(got, fmt.Sprintf("%s:%d: [", tc.path, i+1)); hit != tc.red {
			t.Errorf("%s: reported=%v, want %v", tc.name, hit, tc.red)
		}
	}
	if strings.Contains(got, b64) || strings.Contains(got, hex16) || strings.Contains(got, hex32) {
		t.Fatalf("R7d printed a key value:\n%s", got)
	}
	// The tree scan and the hook only classify lines rg's prefilter lets through: every
	// red line must pass it (the prefilter carries the new context words too).
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg is not installed; the prefilter half needs it (CI installs it)")
	}
	var red []string
	for _, tc := range cases {
		if tc.red {
			red = append(red, tc.text)
		}
	}
	cmd := exec.Command("bash", "-c", `. scripts/secretscan.sh && rg --no-config -c -i -e "$R7D_PREFILTER"`)
	cmd.Dir = repoRoot
	cmd.Stdin = strings.NewReader(strings.Join(red, "\n") + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil || strings.TrimSpace(string(out)) != strconv.Itoa(len(red)) {
		t.Fatalf("the prefilter lets through %s of the %d red lines (err=%v)", strings.TrimSpace(string(out)), len(red), err)
	}
}

// TestPrePush_AnInterruptedScanLeavesNothing — 11th/12th round (§4.7). The hook's temp
// files and the classifier's temp directory hold the FULL text of pushed lines. A
// scan cut by Ctrl-C (SIGINT), a closed terminal (SIGHUP) or SIGTERM used to leave
// the classifier's directory in TMPDIR (measured). The scan is held in the middle — a
// sha256 tool that waits on the hashing call — and the process group is signalled.
//
// One run per signal. The hook's own HUP trap is pinned only PROBABILISTICALLY here:
// without it Ubuntu bash 5.2 left the hook's files in 12 of 20 runs (0 of 20 with it;
// macOS bash 3.2 left none either way), so this case goes red in about 60% of Linux
// runs when that trap is missing. Repeating it inside the test was measured to make it
// flaky instead: in 1 of 250 SIGHUP runs WITH every trap the classifier's directory was
// left (a race; counted in scripts/secretscan.sh, limit #31).
func TestPrePush_AnInterruptedScanLeavesNothing(t *testing.T) {
	realHash := ""
	if p, err := exec.LookPath("sha256sum"); err == nil {
		realHash = p
	} else if p, err := exec.LookPath("shasum"); err == nil {
		realHash = p + " -a 256"
	} else {
		t.Skip("no sha256 tool on this machine")
	}
	const p = "internal/domain/signup/signup.go"
	var line string
	for _, w := range waivedLines(t) {
		if w.path == p {
			line = w.text
		}
	}
	if line == "" {
		t.Fatalf("no waived line of %s in the table", p)
	}
	// Signal 0: the scan runs to its end (the hashing step is not held) — the baseline
	// that nothing is left on a normal exit either.
	for _, sig := range []syscall.Signal{0, syscall.SIGINT, syscall.SIGHUP, syscall.SIGTERM} {
		name, reps := "normal exit", 1 // one run each; see the note above
		if sig != 0 {
			name = sig.String()
		}
		t.Run(name, func(t *testing.T) {
			r := newHookRepo(t)
			base := r.commit("base")
			r.write(p, line+"\n")
			c := r.commit("a line on a waived path, so the classifier hashes it")
			for rep := 1; rep <= reps; rep++ {
				shims, tmp, ready := t.TempDir(), t.TempDir(), filepath.Join(t.TempDir(), "ready")
				// `exec`: the waiting process is the classifier's direct child.
				hold := "exec sleep 30"
				if sig == 0 {
					hold = ":"
				}
				body := "#!/bin/sh\nif [ $# -gt 0 ]; then : > '" + ready + "'; " + hold + "; fi\nexec " + realHash + " \"$@\"\n"
				for _, n := range []string{"sha256sum", "shasum"} {
					if err := os.WriteFile(filepath.Join(shims, n), []byte(body), 0o755); err != nil {
						t.Fatal(err)
					}
				}
				cmd := exec.Command("bash", filepath.Join(r.dir, "scripts", "git-hooks", "pre-push"), "origin", "/dev/null")
				cmd.Dir = r.dir
				cmd.Env = append(os.Environ(), "TMPDIR="+tmp, "PATH="+shims+string(os.PathListSeparator)+os.Getenv("PATH"))
				cmd.Stdin = strings.NewReader(fmt.Sprintf("refs/heads/b %s refs/heads/b %s\n", c, base))
				cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
				if err := cmd.Start(); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(30 * time.Second)
				for {
					if _, err := os.Stat(ready); err == nil {
						break
					}
					if time.Now().After(deadline) {
						if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
							t.Logf("killing the stuck hook: %v", err)
						}
						t.Fatal("the scan never reached the hashing step")
					}
					time.Sleep(50 * time.Millisecond)
				}
				if sig != 0 {
					if err := syscall.Kill(-cmd.Process.Pid, sig); err != nil {
						t.Fatal(err)
					}
				}
				done := make(chan error, 1)
				go func() { done <- cmd.Wait() }()
				select {
				case <-done:
				case <-time.After(30 * time.Second):
					if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
						t.Logf("killing the stuck hook: %v", err)
					}
					t.Fatal("the hook did not stop on " + name)
				}
				left, err := filepath.Glob(filepath.Join(tmp, "tappa-*"))
				if err != nil {
					t.Fatal(err)
				}
				if len(left) != 0 {
					var names []string
					for _, l := range left {
						names = append(names, filepath.Base(l))
					}
					t.Fatalf("a scan (%s, run %d) left %d temp entries holding line text in TMPDIR: %s", name, rep, len(left), strings.Join(names, " "))
				}
			}
		})
	}
}

// TestMakefile_AuditSaysTheScanCouldNotRun — 11th round (cosmetic): redline-check's
// exit 2 no longer means only "no rg" (a broken waiver table, no sha256 tool, mktemp
// can each cause it), so `make audit` names the outcome, not one cause.
func TestMakefile_AuditSaysTheScanCouldNotRun(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `redword="SKIPPED(scan could not run) exit=2"`) || strings.Contains(string(b), "SKIPPED(no rg)") {
		t.Fatal("make audit must label redline-check's exit 2 as a scan that could not run")
	}
}
