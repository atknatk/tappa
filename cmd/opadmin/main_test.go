package main

// main_test.go -- opadmin without a database: what it accepts, what it writes where,
// the shape of the SQL, and the two contracts it shares with the server (the link
// secret with internal/operatorauth, the host rule with internal/config). The
// database half is opadmin_db_test.go; the dependency closure is deps_test.go.

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/operatorauth"
)

// poison is what every refusal puts on stdout (refuse).
var poison = poisonStmt + "\n"

// linkRE reads the enrollment link out of the stderr report.
var linkRE = regexp.MustCompile(`https://([a-z0-9.-]+)/operator/enroll\?id=([0-9a-f-]{36})#([A-Za-z0-9_-]{43})`)

// base64urlRun finds any 43-character run of the link alphabet: what a link secret
// looks like wherever it might have landed.
var base64urlRun = regexp.MustCompile(`[A-Za-z0-9_-]{43}`)

func runCmd(t *testing.T, terminal bool, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut, terminal, time.Now())
	return code, out.String(), errOut.String()
}

// generated is one successful create or reset-mfa: the SQL and the link's parts.
type generated struct {
	sql, report      string
	host, id, secret string
}

func mustGenerate(t *testing.T, args ...string) generated {
	t.Helper()
	return generateAt(t, time.Now(), args...)
}

// generateAt runs a create or reset-mfa as if the generating machine's clock read now.
func generateAt(t *testing.T, now time.Time, args ...string) generated {
	t.Helper()
	var outB, errB bytes.Buffer
	code := run(args, &outB, &errB, true, now)
	out, errOut := outB.String(), errB.String()
	if code != exitOK {
		t.Fatalf("opadmin %v: exit %d, stderr:\n%s", args[0], code, errOut)
	}
	m := linkRE.FindStringSubmatch(errOut)
	if m == nil {
		t.Fatalf("opadmin %v: no enrollment link on stderr", args[0])
	}
	return generated{sql: out, report: errOut, host: m[1], id: m[2], secret: m[3]}
}

// stmtsOf splits the SQL this program writes back into its statements: the comment
// header, then one statement per line, except the COPY statement, which carries the
// data lines up to and including "\\.", and the DO block, which runs from "DO
// $opadmin$" to "$opadmin$;". It is a reader for THIS program's output only.
func stmtsOf(t *testing.T, sql string) []string {
	t.Helper()
	var stmts []string
	var cur []string
	inDo, inCopy := false, false
	for _, line := range strings.Split(strings.TrimSuffix(sql, "\n"), "\n") {
		switch {
		case inDo:
			cur = append(cur, line)
			if line == dollarTag+";" {
				stmts = append(stmts, strings.Join(cur, "\n"))
				cur, inDo = nil, false
			}
		case inCopy:
			cur = append(cur, line)
			if line == "\\." {
				stmts = append(stmts, strings.Join(cur, "\n"))
				cur, inCopy = nil, false
			}
		case strings.HasPrefix(line, "-- "):
			if len(stmts) > 0 {
				t.Fatalf("a comment line after the first statement: %q", line)
			}
		case line == "DO "+dollarTag:
			cur, inDo = []string{line}, true
		case strings.HasPrefix(line, "COPY ") && strings.HasSuffix(line, " FROM STDIN;"):
			cur, inCopy = []string{line}, true
		default:
			stmts = append(stmts, line)
		}
	}
	if inDo || inCopy {
		t.Fatal("the DO block or the COPY data is not closed")
	}
	return stmts
}

// copyParts splits a COPY entry of stmtsOf into the statement and its data lines
// (without the "\\." terminator).
func copyParts(st string) (stmt, data string, ok bool) {
	lines := strings.Split(st, "\n")
	if len(lines) < 3 || !strings.HasPrefix(lines[0], "COPY ") || lines[len(lines)-1] != "\\." {
		return "", "", false
	}
	return lines[0], strings.Join(lines[1:len(lines)-1], "\n"), true
}

// statementTexts are the texts of every statement of a script as the server receives
// them -- the COPY statement without its data lines.
func statementTexts(t *testing.T, sql string) []string {
	t.Helper()
	var out []string
	for _, st := range stmtsOf(t, sql) {
		if c, _, ok := copyParts(st); ok {
			out = append(out, c)
			continue
		}
		out = append(out, st)
	}
	return out
}

// ------------------------------------------------------------ refusals --

// TestRun_RefusesTheEscapeTable is the escape table: each input is refused before
// anything is minted, with the poison statement on stdout, a refusal on stderr, and
// no link. The values are hostile on purpose (SQL metacharacters, NUL, invalid UTF-8,
// terminal escapes, bidi overrides, over-long values, URL parts in the host).
func TestRun_RefusesTheEscapeTable(t *testing.T) {
	long := func(n int, s string) string { return strings.Repeat(s, n) }
	okID := "0b9f1e2a-3c4d-4e5f-8a6b-7c8d9e0f1a2b"
	if len(email255) != 255 {
		t.Fatalf("the 255-byte case is %d bytes", len(email255))
	}
	tests := []struct {
		name string
		args []string
		code int
	}{
		{"no subcommand", nil, exitUsage},
		{"unknown subcommand", []string{"delete", "--id", okID}, exitUsage},
		{"help", []string{"-h"}, exitUsage},
		{"help after the subcommand", []string{"create", "-h"}, exitUsage},
		{"unknown flag", []string{"create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt", "--role", "x"}, exitUsage},
		{"a flag of another subcommand", []string{"disable", "--id", okID, "--email", "a@b.test", "--host", "ops.taptime.mt"}, exitUsage},
		{"missing --host", []string{"create", "--email", "a@b.test", "--name", "A"}, exitUsage},
		{"missing --email", []string{"disable", "--id", okID}, exitUsage},
		{"a flag twice", []string{"create", "--email", "a@b.test", "--email", "c@d.test", "--name", "A", "--host", "ops.taptime.mt"}, exitUsage},
		{"a positional argument", []string{"disable", "--id", okID, "--email", "a@b.test", "extra"}, exitUsage},

		{"email with a semicolon", []string{"create", "--email", "a;DROP@b.test", "--name", "A", "--host", "ops.taptime.mt"}, exitRefused},
		{"email with a space", []string{"create", "--email", "a b@b.test", "--name", "A", "--host", "ops.taptime.mt"}, exitRefused},
		{"email with NUL", []string{"create", "--email", "a\x00b@b.test", "--name", "A", "--host", "ops.taptime.mt"}, exitRefused},
		{"email not UTF-8", []string{"create", "--email", "a\xffb@b.test", "--name", "A", "--host", "ops.taptime.mt"}, exitRefused},
		{"email non-ASCII", []string{"create", "--email", "аdmin@b.test", "--name", "A", "--host", "ops.taptime.mt"}, exitRefused},
		{"email with a newline", []string{"create", "--email", "a@b.test\nDROP", "--name", "A", "--host", "ops.taptime.mt"}, exitRefused},
		{"email with two @", []string{"create", "--email", "a@b@c.test", "--name", "A", "--host", "ops.taptime.mt"}, exitRefused},
		{"email quoted local part", []string{"create", "--email", `"a b"@c.test`, "--name", "A", "--host", "ops.taptime.mt"}, exitRefused},
		{"email leading dot", []string{"create", "--email", ".a@c.test", "--name", "A", "--host", "ops.taptime.mt"}, exitRefused},
		{"email local part 65 bytes", []string{"create", "--email", long(65, "a") + "@c.test", "--name", "A", "--host", "ops.taptime.mt"}, exitRefused},
		{"email 255 bytes", []string{"create", "--email", email255, "--name", "A", "--host", "ops.taptime.mt"}, exitRefused},
		{"email domain with underscore", []string{"create", "--email", "a@b_c.test", "--name", "A", "--host", "ops.taptime.mt"}, exitRefused},

		{"name empty", []string{"create", "--email", "a@b.test", "--name", "", "--host", "ops.taptime.mt"}, exitRefused},
		{"name 201 characters", []string{"create", "--email", "a@b.test", "--name", long(201, "ş"), "--host", "ops.taptime.mt"}, exitRefused},
		{"name with a terminal escape", []string{"create", "--email", "a@b.test", "--name", "A\x1b[2J", "--host", "ops.taptime.mt"}, exitRefused},
		{"name with a bidi override", []string{"create", "--email", "a@b.test", "--name", "A\u202eB", "--host", "ops.taptime.mt"}, exitRefused},
		{"name with a zero-width space", []string{"create", "--email", "a@b.test", "--name", "A\u200bB", "--host", "ops.taptime.mt"}, exitRefused},
		{"name with a newline", []string{"create", "--email", "a@b.test", "--name", "A\nB", "--host", "ops.taptime.mt"}, exitRefused},
		{"name with a tab", []string{"create", "--email", "a@b.test", "--name", "A\tB", "--host", "ops.taptime.mt"}, exitRefused},
		{"name with NUL", []string{"create", "--email", "a@b.test", "--name", "A\x00B", "--host", "ops.taptime.mt"}, exitRefused},
		{"name not UTF-8", []string{"create", "--email", "a@b.test", "--name", "A\xc3", "--host", "ops.taptime.mt"}, exitRefused},
		{"name with a leading space", []string{"create", "--email", "a@b.test", "--name", " A", "--host", "ops.taptime.mt"}, exitRefused},
		{"name of one space", []string{"create", "--email", "a@b.test", "--name", " ", "--host", "ops.taptime.mt"}, exitRefused},
		{"name with a no-break space", []string{"create", "--email", "a@b.test", "--name", "A\u00a0B", "--host", "ops.taptime.mt"}, exitRefused},

		{"host with a scheme", []string{"create", "--email", "a@b.test", "--name", "A", "--host", "https://ops.taptime.mt"}, exitRefused},
		{"host with a port", []string{"create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt:443"}, exitRefused},
		{"host with a path", []string{"create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt/x"}, exitRefused},
		{"host with a user part", []string{"create", "--email", "a@b.test", "--name", "A", "--host", "u@ops.taptime.mt"}, exitRefused},
		{"host upper case", []string{"create", "--email", "a@b.test", "--name", "A", "--host", "OPS.taptime.mt"}, exitRefused},
		{"host trailing dot", []string{"create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt."}, exitRefused},
		{"host with a fragment", []string{"create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt#x"}, exitRefused},
		{"host empty", []string{"create", "--email", "a@b.test", "--name", "A", "--host", ""}, exitRefused},

		{"id upper case", []string{"disable", "--id", strings.ToUpper(okID), "--email", "a@b.test"}, exitRefused},
		{"id with braces", []string{"disable", "--id", "{" + okID + "}", "--email", "a@b.test"}, exitRefused},
		{"id without hyphens", []string{"disable", "--id", strings.ReplaceAll(okID, "-", ""), "--email", "a@b.test"}, exitRefused},
		{"id with a quote", []string{"disable", "--id", okID[:35] + "'", "--email", "a@b.test"}, exitRefused},
		{"id as SQL", []string{"reset-mfa", "--id", "x'::uuid; DROP TABLE platform_admins; --", "--email", "a@b.test", "--host", "ops.taptime.mt"}, exitRefused},
	}
	// main.go's PART I names this count; a case added or removed changes it there too.
	if len(tests) != 47 {
		t.Fatalf("%d cases; main.go's claim counts 47", len(tests))
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, out, errOut := runCmd(t, true, tc.args...)
			if code != tc.code {
				t.Errorf("exit %d, want %d; stderr:\n%s", code, tc.code, errOut)
			}
			if out != poison {
				t.Errorf("stdout is not exactly the poison statement:\n%s", out)
			}
			if !strings.Contains(errOut, "opadmin: REFUSED: ") {
				t.Errorf("stderr carries no refusal:\n%s", errOut)
			}
			if linkRE.MatchString(errOut) || base64urlRun.MatchString(errOut) {
				t.Errorf("a refusal printed a link or a link-shaped value:\n%s", errOut)
			}
		})
	}
}

// TestRun_RefusesALinkWhenStderrIsNotATerminal: create and reset-mfa need stderr on a
// terminal; disable prints no link and does not.
func TestRun_RefusesALinkWhenStderrIsNotATerminal(t *testing.T) {
	id := "0b9f1e2a-3c4d-4e5f-8a6b-7c8d9e0f1a2b"
	for _, args := range [][]string{
		{"create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt"},
		{"reset-mfa", "--id", id, "--email", "a@b.test", "--host", "ops.taptime.mt"},
	} {
		code, out, errOut := runCmd(t, false, args...)
		if code != exitRefused || out != poison || !strings.Contains(errOut, "stderr is not a terminal") {
			t.Errorf("%s with stderr not a terminal: exit %d, stdout %q, stderr %q", args[0], code, out, errOut)
		}
		if base64urlRun.MatchString(errOut) {
			t.Errorf("%s: a link-shaped value reached the redirected stderr", args[0])
		}
	}
	code, out, _ := runCmd(t, false, "disable", "--id", id, "--email", "a@b.test")
	if code != exitOK || !strings.Contains(out, "BEGIN;") {
		t.Errorf("disable with stderr not a terminal: exit %d; it prints no link and should run", code)
	}
	// POSITIVE CONTROL: the same create with a terminal produces SQL and a link.
	g := mustGenerate(t, "create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt")
	if !strings.Contains(g.sql, "BEGIN;") {
		t.Fatal("CONTROL FAILED: create with a terminal wrote no SQL")
	}
}

// ---------------------------------------------------------------- the SQL --

// TestSQL_IsOneTransactionAroundOneDoBlock: every subcommand writes the same seven
// statements -- BEGIN, the two settings, the payload table, the COPY with its one data
// line, one DO block, COMMIT -- and what run() writes is exactly the builder's script
// for the values it reported and the generation time it was given.
func TestSQL_IsOneTransactionAroundOneDoBlock(t *testing.T) {
	now := time.Date(2026, 10, 2, 12, 0, 0, 123456000, time.UTC)
	c := generateAt(t, now, "create", "--email", "Op@Example.test", "--name", "Ada Operator", "--host", "ops.taptime.mt")
	r := generateAt(t, now, "reset-mfa", "--id", c.id, "--email", "op@example.test", "--host", "ops.taptime.mt")
	var dB, dE bytes.Buffer
	if code := run([]string{"disable", "--id", c.id, "--email", "op@example.test"}, &dB, &dE, true, now); code != exitOK {
		t.Fatalf("disable: exit %d", code)
	}
	d := dB.String()

	for name, sql := range map[string]string{"create": c.sql, "reset-mfa": r.sql, "disable": d} {
		stmts := stmtsOf(t, sql)
		if len(stmts) != 7 {
			t.Fatalf("%s: %d statements, want 7: %q", name, len(stmts), stmts)
		}
		want := []string{"BEGIN;", "SET LOCAL search_path = pg_catalog, pg_temp;", "SET LOCAL lock_timeout = '5s';",
			"CREATE TEMP TABLE pg_temp.opadmin_in (payload text NOT NULL) ON COMMIT DROP;"}
		for i, w := range want {
			if stmts[i] != w {
				t.Errorf("%s: statement %d is %q, want %q", name, i+1, stmts[i], w)
			}
		}
		if cs, data, ok := copyParts(stmts[4]); !ok || cs != copyStmt || strings.Count(data, "\n") != 0 || !strings.HasPrefix(data, "-- ") {
			t.Errorf("%s: statement 5 is not the COPY with one data line", name)
		}
		if !strings.HasPrefix(stmts[5], "DO "+dollarTag+"\n") || !strings.HasSuffix(stmts[5], "\n"+dollarTag+";") ||
			strings.Count(stmts[5], dollarTag) != 2 {
			t.Errorf("%s: statement 6 is not one DO block", name)
		}
		if stmts[6] != "COMMIT;" {
			t.Errorf("%s: the last statement is %q, want COMMIT;", name, stmts[6])
		}
	}

	cmd := subcommand{name: "create", id: c.id, email: "Op@Example.test", displayName: "Ada Operator", host: "ops.taptime.mt", generated: now}
	if got := createScript(cmd, linkSecretHash(c.secret)).String(); got != c.sql {
		t.Error("create: stdout is not createScript's output for the reported id, the link's secret and the given time")
	}
	cmd = subcommand{name: "reset-mfa", id: c.id, email: "op@example.test", host: "ops.taptime.mt", generated: now}
	if got := resetMFAScript(cmd, linkSecretHash(r.secret)).String(); got != r.sql {
		t.Error("reset-mfa: stdout is not resetMFAScript's output for the given id, the link's secret and the given time")
	}
	if got := disableScript(subcommand{name: "disable", id: c.id, email: "op@example.test", generated: now}).String(); got != d {
		t.Error("disable: stdout is not disableScript's output")
	}
	if !strings.Contains(c.sql, "v_generated timestamptz := '2026-10-02T12:00:00.123456Z';") {
		t.Error("create: the generation time is not the one run was given")
	}
}

// TestSQL_TheValuesTravelAsCopyData: for the one hostile address and name used here, in
// each subcommand's script, the COPY data line is "-- ", the padding and the hex fields
// (the link secret's hash, the address, the name), and no statement text -- the part
// log_statement and log_min_error_statement write -- carries a hex field or one of the
// seven raw fragments listed.
func TestSQL_TheValuesTravelAsCopyData(t *testing.T) {
	email := "o'b$$r`{|}~%" + "@x-y.test"
	name := "Zqx' $opadmin$ ; -- \\ %s \"Ş\" $$ end"
	c := mustGenerate(t, "create", "--email", email, "--name", name, "--host", "ops.taptime.mt")
	r := mustGenerate(t, "reset-mfa", "--id", c.id, "--email", email, "--host", "ops.taptime.mt")
	_, d, _ := runCmd(t, true, "disable", "--id", c.id, "--email", email)
	cases := []struct {
		name, sql string
		fields    []string
	}{
		{"create", c.sql, []string{linkSecretHash(c.secret), hexOf(email), hexOf(name)}},
		{"reset-mfa", r.sql, []string{linkSecretHash(r.secret), hexOf(email)}},
		{"disable", d, []string{hexOf(email)}},
	}
	for _, tc := range cases {
		stmts := stmtsOf(t, tc.sql)
		_, data, ok := copyParts(stmts[4])
		if !ok || data != "-- "+copyPadding+" "+strings.Join(tc.fields, " ") {
			t.Errorf("%s: the COPY data line is not \"-- \", the padding and the %d hex fields", tc.name, len(tc.fields))
		}
		for _, text := range statementTexts(t, tc.sql) {
			for _, v := range append(append([]string{}, tc.fields...), "o'b", "$$r", "Zqx", "$opadmin$ ;", "\\ %s", "Ş", "x-y.test") {
				if strings.Contains(text, v) {
					t.Errorf("%s: a statement text carries a value (%d bytes)", tc.name, len(v))
				}
			}
		}
		if n := strings.Count(tc.sql, dollarTag); n != 2 {
			t.Errorf("%s: %s occurs %d times, want 2", tc.name, dollarTag, n)
		}
	}
	// POSITIVE CONTROL: the scan sees a value placed in a statement text.
	planted := strings.Replace(c.sql, "BEGIN;", "BEGIN; -- "+linkSecretHash(c.secret), 1)
	seen := false
	for _, text := range statementTexts(t, planted) {
		seen = seen || strings.Contains(text, linkSecretHash(c.secret))
	}
	if !seen {
		t.Fatal("CONTROL FAILED: a hash planted in a statement text was not seen")
	}
}

// TestSQL_ThirtyMinutesFromOneDatabaseClockRead: the link lifetime is ADR 0020 §3's
// 30 minutes, and both issuance columns come from the one v_now the SQL reads from
// clock_timestamp() -- in create and in reset-mfa. The database test measures the
// written values; this pins the text that writes them.
func TestSQL_ThirtyMinutesFromOneDatabaseClockRead(t *testing.T) {
	if enrollTTL != "30 minutes" {
		t.Fatalf("enrollTTL is %q; ADR 0020 §3 says 30 minutes", enrollTTL)
	}
	c := mustGenerate(t, "create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt")
	r := mustGenerate(t, "reset-mfa", "--id", c.id, "--email", "a@b.test", "--host", "ops.taptime.mt")
	for name, sql := range map[string]string{"create": c.sql, "reset-mfa": r.sql} {
		if strings.Count(sql, "v_now := pg_catalog.clock_timestamp();") != 1 {
			t.Errorf("%s: v_now is not read exactly once from clock_timestamp()", name)
		}
		for _, frozen := range []string{"now()", "current_timestamp", "statement_timestamp", "transaction_timestamp", "localtimestamp"} {
			if strings.Contains(strings.ToLower(sql), frozen) {
				t.Errorf("%s: the SQL uses %s", name, frozen)
			}
		}
	}
	if !strings.Contains(c.sql, "                v_hash, v_now, v_now + interval '30 minutes');") {
		t.Error("create: enroll_issued_at and enroll_expires_at are not v_now and v_now + 30 minutes")
	}
	if !strings.Contains(r.sql, "enroll_issued_at   = v_now,\n") ||
		!strings.Contains(r.sql, "enroll_expires_at  = v_now + interval '30 minutes',\n") {
		t.Error("reset-mfa: enroll_issued_at and enroll_expires_at are not v_now and v_now + 30 minutes")
	}
}

// ------------------------------------------------- secret, hash, id and host --

// TestLinkSecret_Shape: 256 bits as 43 characters of base64url, canonical (re-encoding
// the decoded bytes gives the same text), in the alphabet OP-8's page accepts, and
// different every time.
func TestLinkSecret_Shape(t *testing.T) {
	page := regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`) // web/static/js/operator/enroll.js
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		v, err := newLinkSecret()
		if err != nil {
			t.Fatal(err)
		}
		b, err := base64.RawURLEncoding.DecodeString(v)
		if len(v) != linkSecretLen || !page.MatchString(v) || err != nil || len(b) != linkSecretBytes ||
			base64.RawURLEncoding.EncodeToString(b) != v {
			t.Fatalf("draw %d is not a canonical 43-character base64url value of 32 bytes", i)
		}
		if seen[v] {
			t.Fatalf("draw %d repeated an earlier value", i)
		}
		seen[v] = true
	}
}

// TestLinkSecret_HashIsOperatorauthsHash: the hash written into the SQL is
// operatorauth.EnrollmentTokenHash of the same text -- the function 00026's
// encode(sha256(convert_to(…, 'UTF8')), 'hex') is pinned to on the live server
// (operatorauth's TestEnrollmentTokenHash_IsTheDatabasesHash) -- on minted values and
// on a fixed set that includes non-ASCII and the empty string.
func TestLinkSecret_HashIsOperatorauthsHash(t *testing.T) {
	ins := []string{"", "x", "a b", "ünïcödé-ş-İ", strings.Repeat("z", 200)}
	for i := 0; i < 100; i++ {
		v, err := newLinkSecret()
		if err != nil {
			t.Fatal(err)
		}
		ins = append(ins, v)
	}
	if len(ins) != 105 {
		t.Fatalf("%d inputs; main.go's claim counts 105", len(ins))
	}
	for i, in := range ins {
		if got, want := linkSecretHash(in), operatorauth.EnrollmentTokenHash(in); got != want {
			t.Errorf("input %d: opadmin and operatorauth hash it differently", i)
		}
	}
	// And the hash run() writes is the hash of the link's own secret.
	g := mustGenerate(t, "create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt")
	if _, data, ok := copyParts(stmtsOf(t, g.sql)[4]); !ok || !strings.Contains(data, operatorauth.EnrollmentTokenHash(g.secret)) {
		t.Error("the COPY data does not carry operatorauth's hash of the link's secret")
	}
}

// TestAccountID_IsARandomV4UUID: canonical lower-case text, version 4, RFC variant,
// and different every time.
func TestAccountID_IsARandomV4UUID(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		s, err := newAccountID()
		if err != nil {
			t.Fatal(err)
		}
		u, err := uuid.Parse(s)
		if err != nil || u.String() != s || u.Version() != 4 || u.Variant() != uuid.RFC4122 || !isCanonicalUUID(s) {
			t.Fatalf("draw %d: %q is not a canonical version-4 uuid", i, s)
		}
		if seen[s] {
			t.Fatalf("draw %d repeated an earlier id", i)
		}
		seen[s] = true
	}
}

// TestHost_IsConfigsRule: isDNSHostName here and in internal/config have the same
// body (printed without comments), so --host accepts exactly what
// TAPPA_OPERATOR_HOST accepts; and the shared rule behaves as documented on a table.
func TestHost_IsConfigsRule(t *testing.T) {
	body := func(path string) string {
		t.Helper()
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == "isDNSHostName" {
				var b bytes.Buffer
				if err := printer.Fprint(&b, token.NewFileSet(), fd.Type); err != nil {
					t.Fatal(err)
				}
				b.WriteString(" ")
				if err := printer.Fprint(&b, token.NewFileSet(), fd.Body); err != nil {
					t.Fatal(err)
				}
				return b.String()
			}
		}
		t.Fatalf("%s declares no isDNSHostName", path)
		return ""
	}
	mine, theirs := body("main.go"), body(filepath.Join("..", "..", "internal", "config", "config.go"))
	if mine != theirs {
		t.Errorf("isDNSHostName differs from internal/config's:\n--- here\n%s\n--- internal/config\n%s", mine, theirs)
	}
	table := map[string]bool{
		"ops.taptime.mt": true, "ops.localhost": true, "localhost": true, "a-b.c": true,
		"OPS.taptime.mt": false, "ops.taptime.mt.": false, ".ops": false, "ops..mt": false, "-ops.mt": false,
		"ops-.mt": false, "ops.taptime.mt:443": false, "https://ops.taptime.mt": false, "ops_x.mt": false,
		"": false, strings.Repeat("a", 64) + ".mt": false, strings.Repeat("a.", 127) + "mt": false,
	}
	if len(table) != 16 {
		t.Fatalf("%d cases; main.go's claim counts 16", len(table))
	}
	for host, want := range table {
		if got := isDNSHostName(host); got != want {
			t.Errorf("isDNSHostName(%q) = %v, want %v", host, got, want)
		}
	}
}

// TestLink_IsTheShapeTheOperatorPageReads: the link parses as an https URL on the
// given host, path /operator/enroll, a query of one key (id), the secret
// as the fragment and nowhere else in the URL.
func TestLink_IsTheShapeTheOperatorPageReads(t *testing.T) {
	g := mustGenerate(t, "create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt")
	u, err := url.Parse(linkRE.FindString(g.report))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Scheme != "https" || u.Host != "ops.taptime.mt" || u.Path != "/operator/enroll" || len(q) != 1 ||
		q.Get("id") != g.id || u.Fragment != g.secret {
		t.Errorf("the link is not https://<host>/operator/enroll?id=<id>#<secret>: scheme %q host %q path %q query keys %d",
			u.Scheme, u.Host, u.Path, len(q))
	}
	if strings.Contains(u.RequestURI(), g.secret) {
		t.Error("the secret is in the part of the URL a client sends")
	}
}

// --------------------------------------------------------------- leaks --

// captureDefaultLoggers points slog's default logger (and through it the log
// package) at buf for the rest of the test, at Debug, in the given form.
func captureDefaultLoggers(t *testing.T, buf *bytes.Buffer, json bool) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	opts := &slog.HandlerOptions{Level: slog.LevelDebug}
	if json {
		slog.SetDefault(slog.New(slog.NewJSONHandler(buf, opts)))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(buf, opts)))
	}
}

// encodings are the forms a 32-byte value could be written in.
func encodings(secret string) map[string]string {
	raw, _ := base64.RawURLEncoding.DecodeString(secret)
	return map[string]string{
		"the text":          secret,
		"hex":               hex.EncodeToString(raw),
		"upper-case hex":    strings.ToUpper(hex.EncodeToString(raw)),
		"std base64":        base64.StdEncoding.EncodeToString(raw),
		"padded base64url":  base64.URLEncoding.EncodeToString(raw),
		"query-escaped":     url.QueryEscape(secret),
		"the first 16 text": secret[:16],
		"the last 16 text":  secret[len(secret)-16:],
	}
}

// TestLeak_TheSecretIsOnlyInTheLinkOnStderr: for create and reset-mfa, the link secret
// is in the stderr report exactly once (inside the link) and in none of the measured
// forms in stdout -- the SQL -- nor in the default slog/log output at Debug, text and
// JSON. The capture's positive control shows the loggers were listening.
func TestLeak_TheSecretIsOnlyInTheLinkOnStderr(t *testing.T) {
	id := "0b9f1e2a-3c4d-4e5f-8a6b-7c8d9e0f1a2b"
	for _, form := range []string{"text", "json"} {
		for _, args := range [][]string{
			{"create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt"},
			{"reset-mfa", "--id", id, "--email", "a@b.test", "--host", "ops.taptime.mt"},
		} {
			t.Run(form+"/"+args[0], func(t *testing.T) {
				var logs bytes.Buffer
				captureDefaultLoggers(t, &logs, form == "json")
				g := mustGenerate(t, args...)
				if len(encodings(g.secret)) != 8 {
					t.Fatal("main.go's claim counts eight encodings")
				}
				if n := strings.Count(g.report, g.secret); n != 1 {
					t.Errorf("stderr holds the link secret %d times, want 1 (inside the link)", n)
				}
				for what, v := range encodings(g.secret) {
					if strings.Contains(g.sql, v) {
						t.Errorf("stdout (the SQL) holds the link secret as %s", what)
					}
					if strings.Contains(logs.String(), v) {
						t.Errorf("the default logger received the link secret as %s", what)
					}
				}
				if !strings.Contains(g.sql, linkSecretHash(g.secret)) {
					t.Error("the SQL does not hold the hash of the link's secret, so this run measured the wrong value")
				}
				// POSITIVE CONTROL: the capture sees what is written to both loggers.
				slog.Debug("opadmin leak probe", "marker", "zz-slog-"+form)
				log.Print("zz-log-" + form)
				if !strings.Contains(logs.String(), "zz-slog-"+form) || !strings.Contains(logs.String(), "zz-log-"+form) {
					t.Fatal("CONTROL FAILED: the capture did not see a Debug slog line and a log.Print line")
				}
			})
		}
	}
}

// failingWriter accepts nothing.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// TestLeak_AFailedWritePrintsNoLink: when writing the SQL fails, the run is a
// refusal and stderr carries no link and no link-shaped value.
func TestLeak_AFailedWritePrintsNoLink(t *testing.T) {
	for _, args := range [][]string{
		{"create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt"},
		{"reset-mfa", "--id", "0b9f1e2a-3c4d-4e5f-8a6b-7c8d9e0f1a2b", "--email", "a@b.test", "--host", "ops.taptime.mt"},
	} {
		var errOut bytes.Buffer
		code := run(args, failingWriter{}, &errOut, true, time.Now())
		if code != exitRefused || !strings.Contains(errOut.String(), "No link was printed") {
			t.Errorf("%s with a failing stdout: exit %d, stderr %q", args[0], code, errOut.String())
		}
		if linkRE.MatchString(errOut.String()) || base64urlRun.MatchString(errOut.String()) {
			t.Errorf("%s with a failing stdout printed a link or a link-shaped value", args[0])
		}
	}
}

// TestRun_TheReportSaysWhatTheSQLDoes: the stderr report of each subcommand names the
// id, the address and the resulting state; disable prints no link.
func TestRun_TheReportSaysWhatTheSQLDoes(t *testing.T) {
	g := mustGenerate(t, "create", "--email", "a@b.test", "--name", "Ada Ş", "--host", "ops.taptime.mt")
	for _, want := range []string{"account id   " + g.id, "email        a@b.test", "name         Ada Ş", "becomes      pending"} {
		if !strings.Contains(g.report, want) {
			t.Errorf("the create report lacks %q", want)
		}
	}
	code, out, errOut := runCmd(t, true, "disable", "--id", g.id, "--email", "a@b.test")
	if code != exitOK || !strings.Contains(errOut, "becomes      disabled") || linkRE.MatchString(errOut) ||
		base64urlRun.MatchString(errOut) || !strings.HasPrefix(out, "-- GENERATED by cmd/opadmin disable") {
		t.Errorf("disable: exit %d, report %q", code, errOut)
	}
}

// TestMain_StderrIsTerminalReadsTheMode: the fact main passes in is the descriptor's
// mode -- here a pipe, which is not a character device.
func TestMain_StderrIsTerminalReadsTheMode(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close(); _ = w.Close() }()
	saved := os.Stderr
	os.Stderr = w
	got := stderrIsTerminal()
	os.Stderr = saved
	if got {
		t.Error("a pipe on stderr was taken for a terminal")
	}
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = null.Close() }()
	os.Stderr = null
	got = stderrIsTerminal()
	os.Stderr = saved
	if !got {
		t.Error("CONTROL FAILED: the null device (a character device) was not taken for one")
	}
}

// email255 and email254 are addresses of exactly 255 and 254 bytes: a 64-byte local
// part and labels of at most 63.
var (
	email254 = strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 61)
	email255 = strings.Repeat("a", 64) + "@" + strings.Repeat("b", 63) + "." + strings.Repeat("c", 63) + "." + strings.Repeat("d", 62)
)

// TestRun_AcceptsTheBoundaries: a 254-byte address (64-byte local part, 63-byte labels)
// and a 200-character name are accepted; TestRun_RefusesTheEscapeTable refuses 255
// bytes, a 65-byte local part and 201 characters.
func TestRun_AcceptsTheBoundaries(t *testing.T) {
	if len(email254) != maxEmailBytes || maxEmailBytes != 254 {
		t.Fatalf("the boundary address is %d bytes, maxEmailBytes %d; 00026's CHECK is 254", len(email254), maxEmailBytes)
	}
	g := mustGenerate(t, "create", "--email", email254, "--name", strings.Repeat("ş", 200), "--host", "ops.taptime.mt")
	if !strings.Contains(g.sql, hexOf(email254)) {
		t.Error("the 254-byte address is not in the COPY data")
	}
}

// TestSQL_ResetRechecksUnderTheRowLock: reset-mfa reads the account FOR UPDATE, and its
// UPDATE repeats the status, hash and issue-time conditions and requires one row, so a
// pre-check and the write see the same row.
func TestSQL_ResetRechecksUnderTheRowLock(t *testing.T) {
	r := mustGenerate(t, "reset-mfa", "--id", "0b9f1e2a-3c4d-4e5f-8a6b-7c8d9e0f1a2b", "--email", "a@b.test", "--host", "ops.taptime.mt")
	for _, want := range []string{
		"       AND a.email OPERATOR(public.=) v_email::public.citext\n       FOR UPDATE;\n",
		"           AND a.status <> 'disabled'\n",
		"           AND a.enroll_token_hash IS DISTINCT FROM v_hash\n",
		"           AND (a.enroll_issued_at IS NULL OR a.enroll_issued_at < v_generated);\n",
		"        GET DIAGNOSTICS v_rows = ROW_COUNT;\n        IF v_rows <> 1 THEN\n",
		"    IF v_issued IS NOT NULL AND v_issued >= v_generated THEN\n",
	} {
		if !strings.Contains(r.sql, want) {
			t.Errorf("reset-mfa's SQL lacks %q", want)
		}
	}
}

// TestLeak_AFailedReportWithholdsTheCommit: when writing the report (with the link) to
// stderr fails, stdout holds every statement but COMMIT followed by the poison
// statement, and the run is a refusal. The database test applies that output and
// finds no row (TestApply_AFailureAnywhereLeavesNoRow).
func TestLeak_AFailedReportWithholdsTheCommit(t *testing.T) {
	for _, args := range [][]string{
		{"create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt"},
		{"reset-mfa", "--id", "0b9f1e2a-3c4d-4e5f-8a6b-7c8d9e0f1a2b", "--email", "a@b.test", "--host", "ops.taptime.mt"},
		{"disable", "--id", "0b9f1e2a-3c4d-4e5f-8a6b-7c8d9e0f1a2b", "--email", "a@b.test"},
	} {
		var out bytes.Buffer
		code := run(args, &out, failingWriter{}, true, time.Now())
		stmts := stmtsOf(t, out.String())
		if code != exitRefused || len(stmts) != 7 || stmts[6] != poisonStmt || strings.Contains(out.String(), "\nCOMMIT;") {
			t.Errorf("%s with a failing stderr: exit %d, %d statements, last %q", args[0], code, len(stmts), stmts[len(stmts)-1])
		}
	}
}

// TestMain_TheBinaryRefusesARedirectedStderr: the compiled command, with stderr
// redirected to a file, refuses create (exit 1, the poison statement on stdout, the
// reason in the file); with stderr on the null device -- a character device -- it
// writes the SQL, whose generation time is within 5 s of the moment it ran. This
// measures main's wiring of stderrIsTerminal and of the clock, which run's tests take
// as parameters.
func TestMain_TheBinaryRefusesARedirectedStderr(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "opadmin")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	args := []string{"create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt"}

	errFile, err := os.Create(filepath.Join(dir, "stderr.txt"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, args...)
	var stdout bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, errFile
	runErr := cmd.Run()
	_ = errFile.Close()
	written, err := os.ReadFile(filepath.Join(dir, "stderr.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var ee *exec.ExitError
	if !errors.As(runErr, &ee) || ee.ExitCode() != exitRefused || stdout.String() != poison ||
		!strings.Contains(string(written), "stderr is not a terminal") || base64urlRun.Match(written) {
		t.Errorf("stderr to a file: %v, stdout %q, stderr %q", runErr, stdout.String(), string(written))
	}

	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = null.Close() }()
	cmd = exec.Command(bin, args...)
	stdout.Reset()
	cmd.Stdout, cmd.Stderr = &stdout, null
	before := time.Now()
	if err := cmd.Run(); err != nil || !strings.HasSuffix(stdout.String(), "\nCOMMIT;\n") {
		t.Fatalf("CONTROL FAILED: stderr on the null device: %v, stdout ends %q", err, tail(stdout.String()))
	}
	// main hands run this machine's clock: the generation time in the SQL is now.
	m := regexp.MustCompile(`v_generated timestamptz := '([0-9T:.\-]+Z)';`).FindStringSubmatch(stdout.String())
	if m == nil {
		t.Fatal("the SQL carries no generation time")
	}
	gen, err := time.Parse("2006-01-02T15:04:05.000000Z", m[1])
	if err != nil {
		t.Fatal(err)
	}
	if d := gen.Sub(before); d < -5*time.Second || d > 5*time.Second {
		t.Errorf("the compiled command's generation time is %v from the moment it ran; main does not pass the clock", d)
	}
}

func tail(s string) string {
	if len(s) > 40 {
		return s[len(s)-40:]
	}
	return s
}

// TestSQL_TheFirst100BytesOfTheDataLineHoldNoValue: a COPY error's CONTEXT carries the
// first 100 bytes of the data line (TestCopy_ADataErrorShowsThePaddingNotTheValues
// measures that); in each subcommand's line every value starts after byte 100. POSITIVE
// CONTROL: the same line without the padding has its first value inside them.
func TestSQL_TheFirst100BytesOfTheDataLineHoldNoValue(t *testing.T) {
	if len(copyPadding) < 100 {
		t.Fatalf("copyPadding is %d bytes", len(copyPadding))
	}
	c := mustGenerate(t, "create", "--email", "a@b.test", "--name", "A", "--host", "ops.taptime.mt")
	r := mustGenerate(t, "reset-mfa", "--id", c.id, "--email", "a@b.test", "--host", "ops.taptime.mt")
	_, d, _ := runCmd(t, true, "disable", "--id", c.id, "--email", "a@b.test")
	for name, sql := range map[string]string{"create": c.sql, "reset-mfa": r.sql, "disable": d} {
		_, data, ok := copyParts(stmtsOf(t, sql)[4])
		if !ok {
			t.Fatalf("%s: no COPY data", name)
		}
		all := strings.Fields(data)
		if len(all) < 3 || all[0] != "--" || all[1] != copyPadding {
			t.Fatalf("%s: the data line is not \"--\", the padding and the values", name)
		}
		fields := all[2:]
		for _, v := range fields {
			if i := strings.Index(data, v); i < 100 {
				t.Errorf("%s: a value starts at byte %d of the data line", name, i)
			}
		}
		bare := "-- " + strings.Join(fields, " ")
		if strings.Index(bare, fields[0]) >= 100 {
			t.Fatalf("CONTROL FAILED: %s without the padding still has its first value after byte 100", name)
		}
	}
}
