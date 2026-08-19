package main

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// scriptPath is the operator's entry point. The runbook calls it instead of
// carrying its body, because a runbook is a program in an unspecified language —
// the operator's shell — and pasting it was measured to execute backticks inside
// comments and to swallow lines after an odd apostrophe under zsh.
func scriptPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), "scripts", "rotate-kek.sh")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("scripts/rotate-kek.sh is missing: %v", err)
	}
	return p
}

// TestRotateScript_ParsesUnderBash — the shell is now SPECIFIED, so it can be
// checked. `sh -n` is deliberately not claimed: the script uses `set -o pipefail`
// and `[[ ]]`, and its shebang says bash.
func TestRotateScript_ParsesUnderBash(t *testing.T) {
	out, err := exec.Command("bash", "-n", scriptPath(t)).CombinedOutput()
	if err != nil {
		t.Fatalf("bash -n failed: %v\n%s", err, out)
	}
	b, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "#!/usr/bin/env bash") {
		t.Error("the script must declare bash in its shebang; it relies on pipefail and [[ ]]")
	}
	if !strings.Contains(string(b), "set -euo pipefail") {
		t.Error("the script must run under set -euo pipefail; a rotation that continues past an error " +
			"is the failure mode this whole procedure exists to prevent")
	}
}

// TestRotateScript_AlwaysPassesOnErrorStop — D6, structurally.
//
// 🔴 MEASURED BOTH WAYS on a 75 182-row copy. Without `-v ON_ERROR_STOP=1`, a guard
// that aborts leaves the transaction failed, the following COPY dies with "current
// transaction is aborted", and psql sends the remaining COPY DATA LINES AS SQL —
// putting 40 distinct wrapped refs (20 old, 20 new) into the server log while
// EXITING 0, so the operator's terminal shows success. With the flag: 0 refs,
// psql exits 3.
//
// The COPY redesign closes the leak only while the transaction fails closed. Moving
// the flag out of the operator's fingers and into this file is what makes that
// dependency structural instead of a habit.
func TestRotateScript_AlwaysPassesOnErrorStop(t *testing.T) {
	b, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	var applyLines []string
	for _, ln := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(ln)
		// psql as a COMMAND: the line starts with it. Anything else (a die message
		// that names "psql exit $PSQL_RC", a comment) is prose about psql, not a
		// call — an earlier version matched those and reported the fix as a defect.
		if strings.HasPrefix(trimmed, "psql ") {
			applyLines = append(applyLines, ln)
		}
	}
	if len(applyLines) < 3 {
		t.Fatalf("only %d psql invocations found; the scan has gone blind", len(applyLines))
	}
	for _, ln := range applyLines {
		if !strings.Contains(ln, "ON_ERROR_STOP=1") {
			t.Errorf("a psql invocation does not set ON_ERROR_STOP=1:\n  %s", strings.TrimSpace(ln))
		}
	}
}

// TestRotateScript_NeverPipesTheToolIntoPsql — the exit-code masking, structurally.
func TestRotateScript_NeverPipesTheToolIntoPsql(t *testing.T) {
	b, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		if strings.Contains(ln, "rotatekek") && strings.Contains(ln, "| psql") {
			t.Errorf("the script pipes the tool into psql; psql's status would hide the tool's and exit 1 "+
				"would be unreachable:\n  %s", strings.TrimSpace(ln))
		}
	}
	// POSITIVE: the tool's own status is captured and branched on.
	for _, want := range []string{"TOOL_RC=$?", `case "$TOOL_RC" in`, `exit "$TOOL_RC"`} {
		if !strings.Contains(text, want) {
			t.Errorf("the script does not capture and propagate the tool's exit code (missing %q)", want)
		}
	}
}

// TestRotateScript_RefusesDegenerateInputs runs the real script against the inputs
// that must stop it, so its guards are exercised rather than described.
//
// It never reaches a database: every case below is refused before the connection
// check, which is itself the point — the cheap refusals come first.
func TestRotateScript_RefusesDegenerateInputs(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script")
	}
	script := scriptPath(t)
	dir := t.TempDir()
	good := filepath.Join(dir, "good.b64")
	if err := os.WriteFile(good, []byte("dGhpcy1pcy1hLWZha2Uta2V5LWZvci10ZXN0aW5nLW9ubHk="), 0o600); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.b64")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		env  []string
		want string // substring of stderr
	}{
		{"no OWNER_DSN", []string{"TAPPA_TAG_KEK_FILE=" + good, "TAPPA_TAG_KEK_NEW_FILE=" + good}, "OWNER_DSN"},
		{"no old KEK file", []string{"OWNER_DSN=x", "TAPPA_TAG_KEK_NEW_FILE=" + good}, "TAPPA_TAG_KEK_FILE"},
		{"no new KEK file", []string{"OWNER_DSN=x", "TAPPA_TAG_KEK_FILE=" + good}, "TAPPA_TAG_KEK_NEW_FILE"},
		{"old KEK file missing on disk", []string{"OWNER_DSN=x",
			"TAPPA_TAG_KEK_FILE=" + filepath.Join(dir, "nope"), "TAPPA_TAG_KEK_NEW_FILE=" + good}, "cannot read"},
		{
			// THE ZERO-BYTE CASE IS THE ONE THAT MATTERS: a broken interactive read
			// leaves exactly this behind, and it looks like a stored key.
			name: "old KEK file is EMPTY",
			env:  []string{"OWNER_DSN=x", "TAPPA_TAG_KEK_FILE=" + empty, "TAPPA_TAG_KEK_NEW_FILE=" + good},
			want: "EMPTY",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", script)
			cmd.Env = append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + dir}, tc.env...)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("the script should have refused; it exited 0\n%s", out)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("stderr does not mention %q:\n%s", tc.want, out)
			}
			// §4.7: a refusal must not echo key material.
			if strings.Contains(string(out), "dGhpcy1pcy1hLWZha2Uta2V5") {
				t.Error("the refusal echoed the contents of a key file")
			}
		})
	}
}

// TestRotateScript_ProbesTheOverwritingDeleteRatherThanAssumingIt — D4.
//
// `rm -P` is a DOCUMENTED NO-OP on Darwin and still returns 0, so an
// `rm -P … || shred -u …` chain never reaches its fallback and a comment claiming
// "overwrite then delete" is simply false. Measured here: rc=0, file gone, not
// overwritten; shred absent. The script therefore PROBES and branches, and says so
// when it cannot overwrite.
func TestRotateScript_ProbesTheOverwritingDeleteRatherThanAssumingIt(t *testing.T) {
	b, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "command -v shred") {
		t.Error("the script does not probe for shred; it would assume a capability the platform may lack")
	}
	if !strings.Contains(text, "no overwriting delete on this platform") {
		t.Error("the script does not TELL the operator when it cannot overwrite; a silent downgrade from " +
			"'overwritten' to 'unlinked' is exactly the false claim this replaced")
	}
	// The chain that cannot work must not come back — checked on CODE lines only.
	//
	// ⚠️ THE FIRST VERSION OF THIS CHECK SCANNED THE WHOLE FILE AND FIRED ON THE
	// SCRIPT'S OWN COMMENT, which explains why `rm -P || shred` is broken. A
	// detector that cannot tell prose from code reports the explanation as the
	// defect — the same shape as the pipe matcher that fired on `||`.
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		if strings.Contains(ln, "rm -P") && strings.Contains(ln, "|| shred") {
			t.Errorf("the script still chains `rm -P || shred` in code: rm -P returns 0 on Darwin, so the "+
				"fallback is unreachable and nothing is ever overwritten:\n  %s", strings.TrimSpace(ln))
		}
	}
}

// runScript executes the real script with a stubbed psql on PATH, so exit-code
// behaviour is measured by RUNNING it rather than by scanning its text.
//
// 🔴 THE STATIC SCANS ABOVE ARE NOT ENOUGH AND THAT WAS MEASURED: the check that
// the script does not pipe into psql is a text scan, so the defect where psql's own
// exit code leaked out AS THE SCRIPT'S was invisible to the suite. Behaviour needs
// behaviour.
func runScript(t *testing.T, stubRC string, env []string, args ...string) (int, string) {
	t.Helper()
	dir := t.TempDir()
	stub := filepath.Join(dir, "psql")
	marker := filepath.Join(dir, "APPLIED")
	// The stub stands in for psql. It records its own environment (so a KEK leak to
	// a child is observable), honours -X the way psql does (only reading the psqlrc
	// when the flag is ABSENT), can fail the RLS-bypass probe on demand, and marks
	// the file when it is asked to apply.
	body := strings.Join([]string{
		"#!/usr/bin/env bash",
		`if [[ -n "${DUMP_ENV_TO:-}" ]]; then env > "$DUMP_ENV_TO"; fi`,
		`case " $* " in *" -X "*) :;; *) if [[ -r "${PSQLRC_HOME:-/nonexistent}/.psqlrc" ]]; then bash "${PSQLRC_HOME}/.psqlrc" >/dev/null 2>&1 || true; fi;; esac`,
		`for a in "$@"; do case "$a" in -f) : > ` + marker + `; exit ${STUB_RC:-0};; esac; done`,
		`case "$*" in`,
		`  *rolsuper*) if [[ -n "${PROBE_RC:-}" ]]; then exit $PROBE_RC; fi; echo t; exit 0;;`,
		`  *"COPY (SELECT uid"*) printf '04AC7E55000601\t10000000-0000-4000-8000-000000000001\t%s\n' "$(printf 'aa%.0s' {1..44})"; exit 0;;`,
		"esac",
		"exit 0",
		"",
	}, "\n")
	if err := os.WriteFile(stub, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	kek := filepath.Join(dir, "old.b64")
	kekNew := filepath.Join(dir, "new.b64")
	// FAKE but STRUCTURALLY VALID: base64 of exactly 32 bytes, or the tool refuses
	// on length and every case below would "pass" for the wrong reason.
	_ = os.WriteFile(kek, []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, 32))), 0o600)
	_ = os.WriteFile(kekNew, []byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x22}, 32))), 0o600)

	cmd := exec.Command("bash", append([]string{scriptPath(t)}, args...)...)
	// HOME is NOT overridden: doing so sent Go's module cache into the temp dir and
	// turned a 2-second test into a 137-second one with an unremovable cache.
	cmd.Env = append([]string{
		"PATH=" + dir + ":" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"STUB_RC=" + stubRC,
		// A URI DSN, because the script now REFUSES a keyword DSN (T49): a bare
		// `dbname=` form takes its host and user from the environment and from
		// PGSERVICE, so the whole run could be redirected to another server and
		// pass every guard there. TestRotateScript_RefusesANonURIDSN drives that.
		"OWNER_DSN=postgres://stub@127.0.0.1:5432/stub",
		"TAPPA_TAG_KEK_FILE=" + kek,
		"TAPPA_TAG_KEK_NEW_FILE=" + kekNew,
		"TAPPA_ROTATE_ALLOW_UNOPENABLE=1",
	}, env...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("running the script: %v\n%s", err, out)
		}
		code = ee.ExitCode()
	}
	applied := false
	if _, err := os.Stat(marker); err == nil {
		applied = true
	}
	return code, string(out) + boolMarker(applied)
}

// boolMarker appends a token the caller can look for, so "did it reach the apply
// step" is answered by a FILE the stub created rather than by an exit code the
// script legitimately remaps.
func boolMarker(applied bool) string {
	if applied {
		return "\n<<<REACHED-APPLY>>>"
	}
	return "\n<<<NO-APPLY>>>"
}

// TestRotateScript_UnrecognisedArgvNeverSelectsTheDestructiveBranch.
//
// 🔴 THE WORST DEFECT THIS TASK PRODUCED. Argument handling was one full-string
// comparison, so every input that was not exactly "--dry-run" took the branch that
// rewrites the whole park. Measured on a throwaway 75 662-row copy with a park
// checksum before and after: --dryrun, --help, -n, --dry-run=true and
// "x --dry-run" ALL exited 0 and ALL REWROTE THE PARK. `--help` rotated production.
//
// The fix inverts the default: writing requires --apply. Both readings were weighed
// (see the script). They leave the same single destructive input; they differ in
// where MISTAKES land, and for an operation that cannot be undone without the old
// key, mistakes must land on the safe side.
func TestRotateScript_UnrecognisedArgvNeverSelectsTheDestructiveBranch(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script")
	}
	// Every one of these must reach the apply step NEVER — proven by the stub: it
	// exits 9 if -f is ever passed, so a run that applies cannot exit 0/1/3 quietly.
	for _, args := range [][]string{
		{}, {"--dry-run"}, {"--dryrun"}, {"-n"}, {"--dry-run=true"},
		{"x", "--dry-run"}, {"--apply", "extra"}, {"--APPLY"}, {"-"}, {"--"},
	} {
		name := strings.Join(args, " ")
		if name == "" {
			name = "(no arguments)"
		}
		t.Run(name, func(t *testing.T) {
			_, out := runScript(t, "9", nil, args...)
			if strings.Contains(out, "<<<REACHED-APPLY>>>") {
				t.Fatalf("argv %v REACHED THE APPLY STEP; the destructive branch must be opt-in\n%s", args, out)
			}
			if strings.Contains(out, "rotate-kek: applied.") {
				t.Fatalf("argv %v applied the rotation\n%s", args, out)
			}
		})
	}
	// POSITIVE CONTROL: --apply DOES reach the apply step. Without this the loop
	// above would pass on a script that never applies anything at all.
	if _, out := runScript(t, "0", nil, "--apply"); !strings.Contains(out, "<<<REACHED-APPLY>>>") {
		t.Fatalf("--apply did not reach the apply step; this test would prove nothing\n%s", out)
	}
	// --help is a question, not a run.
	if code, out := runScript(t, "9", nil, "--help"); code != 0 || !strings.Contains(out, "usage:") {
		t.Errorf("--help should print usage and exit 0, got %d\n%s", code, out)
	}
}

// TestRotateScript_MapsPsqlExitCodeInsteadOfLeakingIt.
//
// 🔴 3 IS A PUBLISHED MEANING: "applied, but the park is NOT fully rotated; the old
// KEK cannot be destroyed", and deploy/README.md turns it into a stop rule. The
// apply step was the one psql call without a guard, so a psql that exited 3 made
// the SCRIPT exit 3 — while nothing had been applied at all. Measured with a stub:
// psql 3 -> script 3, psql 2 -> script 2, no explanation either time.
//
// Anything psql reports at that step means the transaction did not commit, so it
// maps to 1.
func TestRotateScript_MapsPsqlExitCodeInsteadOfLeakingIt(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script")
	}
	for _, rc := range []string{"3", "2", "1"} {
		t.Run("psql exits "+rc, func(t *testing.T) {
			code, out := runScript(t, rc, nil, "--apply")
			if code != 1 {
				t.Errorf("psql exit %s produced script exit %d; a failed apply must be 1 (nothing "+
					"applied), never 3 (applied and incomplete)\n%s", rc, code, out)
			}
			if !strings.Contains(out, "psql exit "+rc) {
				t.Errorf("the failure does not name psql's own status, so the operator cannot diagnose it:\n%s", out)
			}
		})
	}
	// POSITIVE: a clean psql leaves the tool's own code intact.
	if code, _ := runScript(t, "0", nil, "--apply"); code != 3 {
		t.Errorf("with psql succeeding, the script should propagate the TOOL's code (3 for a partial "+
			"rotation), got %d", code)
	}
}

// TestRotateScript_KeepsItsStructuralGuards — the four protections whose removal an
// auditor measured as invisible.
func TestRotateScript_KeepsItsStructuralGuards(t *testing.T) {
	b, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, tc := range []struct{ wantText, why string }{
		{`WORK="$(mktemp -d)"`, "a predictable build path lets a planted symlink, directory or stale " +
			"binary survive `go build -o` (which returns 0 for all three) and then be RUN with both KEKs " +
			"in the environment, with its stdout fed to a superuser psql"},
		{"trap cleanup EXIT", "without it a multi-megabyte rotate.sql full of wrapped refs stays in $TMPDIR"},
		{`[[ "$BYPASS" == "t" ]] || die`, "without the early refusal a non-bypassing role gets a late, " +
			"unreadable abort from the generated SQL instead of a clear one here"},
		{`*) log "rotate-kek: unknown argument`, "an unknown argument must be REFUSED, not ignored"},
	} {
		if !strings.Contains(text, tc.wantText) {
			t.Errorf("the script lost %q — %s", tc.wantText, tc.why)
		}
	}
}

// TestRunbook_PasteableBlocksCarryNoShellHazardInComments.
//
// 🔴 THE CLASS DID NOT CLOSE WHEN THE SCRIPT WAS WRITTEN — IT FORKED. The rotation
// body moved into scripts/rotate-kek.sh, but the section kept fourteen pasteable
// bash blocks (secret entry, kubectl steps, verification), and their COMMENTS still
// carried the hazard: pasted into zsh, where interactive_comments is OFF by default,
// a `#` line runs its backticked text and a line with an odd apostrophe swallows the
// NEXT command. Reproduced on a real pty: a README-shaped comment executed its
// backticked payload and created a marker file.
//
// Worse, the section's own table declared this impossible "because nobody pastes"
// while the same section shipped fourteen things to paste.
//
// This pins the property that makes the remaining blocks safe to paste: their
// comments contain no backtick and no unbalanced apostrophe.
func TestRunbook_PasteableBlocksCarryNoShellHazardInComments(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)

	// 🔴 THE SCAN READS TWO SLICES, AND THE SECOND ONE IS A MEASURED HOLE BEING
	// CLOSED. Until M8-05 this test read ONLY the rotation section, so the whole
	// "Plaket encode" section — which sits BEFORE it in the same file — was outside
	// the net entirely. Proven by mutation, both directions, same file and same run:
	// a bash block whose comment carried `$( … )` and a backtick was PASS (13 blocks
	// scanned, 0 hazards) when planted in the encode section, and FAIL when planted
	// inside the rotation slice. A net whose edge nobody measured is a net with an
	// unmeasured edge.
	//
	// ⚠️ THE SIZE OF THAT SLICE IS NOT FROZEN IN THIS PROSE, AND IT USED TO BE. This
	// paragraph said "the 271-line Plaket encode section"; re-measuring gave 348
	// lines, so the sentence had already rotted into a false claim while the test
	// around it stayed green. A live number belongs in an assertion that recomputes
	// it every run, never in a comment — the only number about this slice now lives
	// in minLines below, where it is COMPARED.
	//
	// 🔴 AND THE SLICE'S EXISTENCE IS NOW ASSERTED, WHICH IT WAS NOT. Measured on
	// this tree: renaming the "## Plaket encode" HEADING made this test FAIL (the
	// locator fatals), but KEEPING the heading and deleting the 348 lines under it
	// left the test GREEN — because want == -1 pins no block count for that slice,
	// so "scan it" degenerated to "scan nothing" without a word of complaint. The
	// net was not verifying the existence of what it protects. minLines + anchors
	// below close that; see the mutation evidence in the M8-05 round notes.
	//
	// 🔴 WHY THREE SLICES AND NOT THE WHOLE FILE — MEASURED, NOT PREFERRED. Re-measure
	// any of these numbers with:
	//
	//	grep -c '^[`][`][`]bash' deploy/README.md          # total pasteable blocks
	//	go test ./cmd/rotatekek/ -run PasteableBlocks -v   # per-slice, from this test
	//
	// On this tree deploy/README.md carries 50 pasteable bash blocks in total; 13 are
	// in the rotation slice, 0 in the encode slice, 5 in the M8-03 observability
	// slice, and the remaining 32 live in the deploy/rollback, backup and
	// incident-response sections. Those 32 hold 43 comment lines, of which 21 carry
	// 26 hazard hits. Scanning the whole file would therefore go RED on 21
	// pre-existing lines owned by other cards — a runbook-wide rewrite smuggled into
	// a correction round (CLAUDE.md §10: no unrelated refactor in one commit).
	// THAT REMAINDER IS COUNTED, NOT CLOSED, and it is written down where a reader
	// will meet it: deploy/README.md → "Kabul edilmiş sınırlar".
	//
	// 🔴 THE THIRD SLICE IS HERE BECAUSE THE CLASS RECURRED WHILE THE NET WATCHED THE
	// WRONG TWO SECTIONS. M8-03 added five pasteable blocks to "## Gözlemlenebilirlik"
	// and two of their comments shipped with an unbalanced apostrophe — M8-02'de and
	// POD'U — each of which opens `quote>` in zsh and swallows the NEXT command. Both
	// slices this test already read were green through that whole round. A net is only
	// as wide as the sections somebody remembered to list, so a section that ships
	// paste-able operator commands joins the list in the same change.
	sections := []struct {
		name  string
		start string
		end   string
		// want is the pinned block count, or -1 for "scan it, do not pin it".
		// Pinning the encode slice at today's 0 would fight FAZ B: the moment a
		// real encode procedure ships a block there, the pin would fail for being
		// RIGHT. Scanning is the point; counting is the rotation slice's contract.
		want int
		// minLines is a FLOOR on the slice's size, not a pin — 0 disables it. It
		// exists because want == -1 makes a slice silently emptiable (measured, see
		// the comment above). A floor is the shape that survives FAZ B: the encode
		// section can only GROW when a real procedure ships, so a floor well under
		// today's measurement never fires for being right, while an emptied section
		// fails immediately.
		//
		// ⚠️ COUNTED LIMIT, STATED RATHER THAN HIDDEN: a floor tolerates partial
		// deletion. At 200 against today's 348, roughly 140 lines could still be
		// removed without tripping this alone. That is what the anchors are for —
		// the two mechanisms cover different halves of the same mutation, and
		// neither is claimed to cover all of it.
		minLines int
		// anchors are load-bearing sentences the slice must still carry. They are
		// NOT structural headings by accident: each one is a normative claim the
		// section exists to publish, so deleting the body without deleting them is
		// not a shape the mutation can take.
		anchors []string
	}{
		{
			name: "KEK rotation", start: "## KEK döndürme", end: "## Olay müdahalesi",
			want: pasteableBlockCount,
			// No floor or anchors here: this slice's block count is PINNED, so
			// emptying it already fails on `blocks != pasteableBlockCount`. Adding a
			// second mechanism where the first is exact would be noise.
		},
		{
			// The M8-03 observability slice. want == -1 for the same reason the encode
			// slice has it: this section is still growing (the pilot gate adds
			// measurement commands), so pinning today's 5 would fail for being RIGHT.
			// The floor is set well under today's 366 lines, and the anchors are the
			// three claims the section exists to publish.
			name: "M8-03 observability", start: "## Gözlemlenebilirlik", end: "## Kabul edilmiş sınırlar",
			want: -1, minLines: 250,
			anchors: []string{
				// The honesty claim: the rules compute, nothing delivers them (Q28a).
				"TESLİMAT KANALI YOK, VE BU DÜRÜSTÇE YAZILIYOR",
				// The precondition without which every rule below matches zero rows.
				"BU KURALLARIN HEPSİ `TAPPA_LOG_FORMAT=json` VARSAYAR",
				// The §4.6 trade that keeps a readiness outage visible after the
				// access record for a designed probe answer was dropped.
				"SAĞLIK SONDALARI `http.request` KAYDININ DIŞINDADIR",
			},
		},
		{
			name: "plaque encode", start: "## Plaket encode", end: "## KEK döndürme",
			want: -1, minLines: 200,
			anchors: []string{
				// ADR 0003 md. 2 — the encode setting the whole section is normative
				// about, and the one an operator is most likely to "fix".
				"SDMMACInputOffset == SDMMACOffset",
				// The trap warning that stops that fix (CLAUDE.md §5, ADR 0003 md. 2).
				"BOŞ MAC GİRDİSİNİ BİR BUG SANIP UID/ctr EKLEMEYİN",
				// The obligation list FAZ B inherits; losing it loses the handover.
				"### FAZ B'ye devredilenler",
			},
		},
	}

	blocks := 0
	hazards := 0
	scanned := 0
	for _, sec := range sections {
		start := strings.Index(text, sec.start)
		end := strings.Index(text, sec.end)
		if start < 0 || end < 0 || end <= start {
			t.Fatalf("could not locate the %s section (%q .. %q)", sec.name, sec.start, sec.end)
		}
		// Absolute file line of the slice's first line, so a failure names a line
		// number the reader can open rather than one relative to a moving slice.
		base := strings.Count(text[:start], "\n")
		slice := text[start:end]
		lines := strings.Count(slice, "\n")
		if sec.minLines > 0 && lines < sec.minLines {
			t.Errorf("the %s section is %d lines, below the floor of %d. Either it was gutted — in which "+
				"case this scan is protecting nothing and the failure is correct — or the section was "+
				"deliberately restructured, in which case lower the floor in the same commit and say why.",
				sec.name, lines, sec.minLines)
		}
		for _, a := range sec.anchors {
			if !strings.Contains(slice, a) {
				t.Errorf("the %s section no longer carries the anchor %q. It is a normative claim, not "+
					"decoration; if it genuinely moved, move the anchor with it.", sec.name, a)
			}
		}
		n, h := scanPasteableComments(t, sec.name, slice, base)
		scanned += n
		hazards += h
		if sec.want >= 0 {
			blocks = n
		}
		t.Logf("section %q: %d lines, %d pasteable bash blocks, %d hazards", sec.name, lines, n, h)
	}
	// CONTROL: blocks were actually found, so silence means safety and not blindness.
	// 🔴 THE COUNT IS COMPARED, NOT JUST COMPUTED. `blocks < 5` was green anywhere
	// from 13 down to 5, and three artefacts published "14" while this very test
	// logged 13 in the same run — the number went 14 -> 13 when round 14 deleted a
	// dead block, i.e. the class produced its seventh instance in the round that
	// declared it closed. The artefacts now derive their number from this one.
	if blocks != pasteableBlockCount {
		t.Errorf("the rotation section has %d pasteable bash blocks but pasteableBlockCount is %d. "+
			"Update the constant AND the three places that publish it (deploy/README.md's structural "+
			"table, the M8-02 card, and this file's own comment).", blocks, pasteableBlockCount)
	}
	for _, art := range []struct{ path, label string }{
		{filepath.Join(repoRoot(t), "deploy", "README.md"), "deploy/README.md"},
		{filepath.Join(repoRoot(t), "docs", "plan", "m8-deploy-pilot.md"), "the M8-02 card"},
	} {
		ab, err := os.ReadFile(art.path)
		if err != nil {
			t.Fatalf("reading %s: %v", art.label, err)
		}
		if !strings.Contains(string(ab), fmt.Sprintf("%d yapıştırıl", pasteableBlockCount)) {
			t.Errorf("%s does not publish the measured count (%d pasteable blocks); an artefact that "+
				"names a different number is the same drift this test exists to stop",
				art.label, pasteableBlockCount)
		}
	}
	t.Logf("%d pasteable bash blocks scanned across %d sections, %d hazards",
		scanned, len(sections), hazards)
}

// scanPasteableComments walks one README slice and reports every shell hazard in a
// comment inside a ```bash block. It returns the number of blocks seen and the
// number of hazards reported; base is the slice's zero-based offset in the file so
// reported line numbers are absolute.
func scanPasteableComments(t *testing.T, name, section string, base int) (blocks, hazards int) {
	t.Helper()
	inBlock := false
	for i, ln := range strings.Split(section, "\n") {
		if strings.HasPrefix(ln, "```bash") {
			inBlock, blocks = true, blocks+1
			continue
		}
		if strings.HasPrefix(ln, "```") {
			inBlock = false
			continue
		}
		if !inBlock || !strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		// 🔴 THE DETECTOR COVERS THE MEASURED CLASS, NOT A GUESS, AND ITS COVERAGE IS
		// STATED. In a real pty (`zsh -f -i`), pasting a `#` comment executes:
		//
		//	backtick   YES  (side-effect file created)
		//	$( ... )   YES  (side-effect file created)  <- was NOT covered; added here
		//	! ~ * ${}  NO   (measured, no side effect in this shape)
		//
		// So two constructs execute and both are refused; three were measured
		// harmless. COUNTED LIMIT: this is a denylist of what was measured on THIS
		// zsh. A construct nobody has tried is not covered, and the honest scope of
		// this test is "the two forms that were shown to run, plus the quoting form
		// that swallows the next command".
		for _, bad := range commentHazards(ln) {
			hazards++
			t.Errorf("deploy/README.md:%d (%s): a comment inside a pasteable block contains %q. Pasted "+
				"into zsh this EXECUTES the enclosed text (measured on a real pty):\n  %s",
				base+i+1, name, bad, strings.TrimSpace(ln))
		}
		if strings.Count(ln, "'")%2 == 1 {
			hazards++
			t.Errorf("deploy/README.md:%d (%s): a comment inside a pasteable block has an unbalanced "+
				"apostrophe. Pasted into zsh this opens quote> and swallows the NEXT command:\n  %s",
				base+i+1, name, strings.TrimSpace(ln))
		}
	}
	return blocks, hazards
}

// runbookInvocations extracts every executable `./scripts/rotate-kek.sh …` line
// from the rotation section of deploy/README.md, with its arguments.
func runbookInvocations(t *testing.T) []invocation {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	start := strings.Index(text, "## KEK döndürme")
	end := strings.Index(text, "## Olay müdahalesi")
	if start < 0 || end < 0 {
		t.Fatal("could not locate the rotation section")
	}
	var out []invocation
	inBlock := false
	for _, ln := range strings.Split(text[start:end], "\n") {
		if strings.HasPrefix(ln, "```bash") {
			inBlock = true
			continue
		}
		if strings.HasPrefix(ln, "```") {
			inBlock = false
			continue
		}
		trimmed := strings.TrimSpace(ln)
		if !inBlock || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(trimmed, "./scripts/rotate-kek.sh") {
			continue
		}
		// 🔴 THE TRAILING COMMENT IS KEPT, NOT STRIPPED. Stripping it was the hole:
		// the sentence that made the false claim — `# uygular` on a bare call — was
		// thrown away before any assertion saw it, so reverting ONE line stayed
		// green while the section-wide total was satisfied by the OTHER apply line.
		// The claim is now part of the case.
		cmd := trimmed
		comment := ""
		if i := strings.Index(cmd, "#"); i >= 0 {
			comment = strings.TrimSpace(cmd[i+1:])
			cmd = cmd[:i]
		}
		if i := strings.Index(cmd, ";"); i >= 0 {
			cmd = cmd[:i]
		}
		out = append(out, invocation{args: strings.Fields(strings.TrimSpace(cmd))[1:], comment: comment})
	}
	return out
}

// TestRunbook_ItsOwnCommandsAreRUNNotSCANNED.
//
// 🔴 THE LAST STRUCTURAL GAP IN THIS TASK, AND I CAUSED IT. Round 12 inverted the
// script's default so that writing requires --apply. The runbook was not updated:
// it still said `./scripts/rotate-kek.sh   # uygular`, and `--apply` appeared ZERO
// times in the whole file. Measured on a 77 215-row copy by running that exact
// line: rc=0, "DRY RUN — nothing applied", and all 77 215 rows still under the OLD
// KEK — while the runbook's own table read that 0 as "applied". An operator
// following it would proceed to step 3, drop TAPPA_TAG_KEK_PREVIOUS, and leave a
// server that holds only the new key for a park that is entirely under the old
// one: every tap 500, no attendance row written, and the leaked KEK the only
// working key.
//
// The class is thirteen rounds old: a fix in one artifact silently falsifies a
// sentence in another, with nothing mechanical between them (card↔README five
// times, manifest↔config once, code↔test-name twice, now README↔script). The cure
// is the one this task already found one layer down: RUN the thing, do not scan it.
//
// So this test EXECUTES the runbook's own invocations against the real script with
// a stub psql, and asserts which of them reach the destructive step. A README that
// documents an apply command which cannot apply fails here.
func TestRunbook_ItsOwnCommandsAreRUNNotSCANNED(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script")
	}
	invocations := runbookInvocations(t)
	if len(invocations) < 3 {
		t.Fatalf("only %d rotate-kek invocations extracted from the runbook; the extractor has gone blind",
			len(invocations))
	}

	applyCount := 0
	for _, inv := range invocations {
		args := inv.args
		name := strings.Join(args, " ")
		if name == "" {
			name = "(no arguments)"
		}
		_, out := runScript(t, "0", nil, args...)
		reached := strings.Contains(out, "<<<REACHED-APPLY>>>")
		hasApply := false
		for _, a := range args {
			if a == "--apply" {
				hasApply = true
			}
		}
		// 🔴 PER-LINE CONTRACT: what the comment CLAIMS must be what the line DOES.
		// This is the assertion that catches reverting a single line — the exact
		// defect this test was written for, which the section-wide total missed.
		claimsApply := strings.Contains(strings.ToUpper(inv.comment), "UYGULAR") ||
			strings.Contains(strings.ToLower(inv.comment), "apply")
		claimsDry := strings.Contains(strings.ToUpper(inv.comment), "DRY RUN") ||
			strings.Contains(strings.ToLower(inv.comment), "uygulamaz")
		if claimsApply && !reached {
			t.Errorf("the runbook line `./scripts/rotate-kek.sh %s` is commented %q — it CLAIMS to apply "+
				"— but it did not reach the apply step. This is exactly the round-12 regression: the "+
				"comment says one thing and the argv does another.", name, inv.comment)
		}
		if claimsDry && reached {
			t.Errorf("the runbook line `./scripts/rotate-kek.sh %s` is commented %q — it claims NOT to "+
				"apply — but it reached the apply step.", name, inv.comment)
		}
		if hasApply && !reached {
			t.Errorf("the runbook line `./scripts/rotate-kek.sh %s` carries --apply but did NOT reach the "+
				"apply step\n%s", name, out)
		}
		if !hasApply && reached {
			t.Errorf("the runbook line `./scripts/rotate-kek.sh %s` has no --apply but REACHED the apply "+
				"step; a documented dry run that writes is worse than no dry run\n%s", name, out)
		}
		if reached {
			applyCount++
		}
	}
	// THE POSITIVE OBLIGATION: a rotation runbook whose commands can never apply is
	// exactly the defect this test was written for. At least one documented
	// invocation must actually reach the write.
	if applyCount == 0 {
		t.Errorf("NONE of the %d documented rotate-kek invocations reaches the apply step. The runbook "+
			"cannot rotate anything — the card's criterion is \"written AND executable\", and the "+
			"written half no longer drives the executable half.", len(invocations))
	}
	t.Logf("%d runbook invocations executed; %d reach the apply step", len(invocations), applyCount)
}

// TestRunbook_ExitCodeTableRowsAreProducedByRealRuns.
//
// Each row of the published table is matched to a run that actually produces that
// code, so the table cannot drift from the program the way `# uygular` did.
//
// ⚠️ COUNTED LIMIT: only the codes reachable WITHOUT a database are produced here —
// 1 (refusal) and 3 (partial rotation, via the stub). Code 0 needs a park in which
// every row opens, which belongs to the rehearsal on a throwaway copy, not to the
// unit suite; that row is verified there and is NOT covered by this test.
func TestRunbook_ExitCodeTableRowsAreProducedByRealRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script")
	}
	b, err := os.ReadFile(filepath.Join(repoRoot(t), "deploy", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	for _, code := range []string{"`0`", "`1`", "`3`"} {
		if !strings.Contains(text, code) {
			t.Errorf("the runbook no longer publishes exit code %s", code)
		}
	}
	// 1: a refusal (no KEK files).
	if code, out := runScript(t, "0", []string{"TAPPA_TAG_KEK_FILE=/nonexistent"}, "--apply"); code != 1 {
		t.Errorf("a refusal produced %d, but the table publishes 1\n%s", code, out)
	}
	// 3: applied, park not fully rotated (the stub feeds one unopenable row).
	if code, out := runScript(t, "0", nil, "--apply"); code != 3 {
		t.Errorf("a partial rotation produced %d, but the table publishes 3\n%s", code, out)
	}
}

// TestRotateScript_MapsTheBypassProbeExitCodeToo.
//
// 🔴 THE APPLY STEP WAS FIXED AND ITS COMMENT CLAIMED IT WAS "the last invocation
// without a guard" — while the RLS-bypass probe fifty-nine lines above leaked the
// same way. Measured with a stub: probe exit 3 -> script 3, 2 -> 2, 4 -> 4,
// 127 -> 127, none explained. Exit 3 is published as "applied, but the park is NOT
// fully rotated; the old KEK cannot be destroyed", and this failure happens before
// the park is even read.
//
// The old stub answered *rolsuper* with an unconditional `echo t; exit 0`, so the
// failure path was never driven at all — the protection was untestable by
// construction.
func TestRotateScript_MapsTheBypassProbeExitCodeToo(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script")
	}
	for _, rc := range []string{"3", "2", "4", "127"} {
		t.Run("probe psql exits "+rc, func(t *testing.T) {
			code, out := runScript(t, "0", []string{"PROBE_RC=" + rc}, "--apply")
			if code != 1 {
				t.Errorf("a failed RLS-bypass probe produced script exit %d; nothing was read or applied, "+
					"so it must be 1 — never 3, which means the rotation wrote and is incomplete\n%s", code, out)
			}
			if !strings.Contains(out, "psql exit "+rc) {
				t.Errorf("the failure does not name psql's own status, so it cannot be diagnosed:\n%s", out)
			}
			if strings.Contains(out, "<<<REACHED-APPLY>>>") {
				t.Errorf("a failed probe still reached the apply step\n%s", out)
			}
		})
	}
	// POSITIVE CONTROL: with the probe succeeding the run proceeds, so the cases
	// above are not passing because the script refuses everything.
	if _, out := runScript(t, "0", nil, "--apply"); !strings.Contains(out, "<<<REACHED-APPLY>>>") {
		t.Fatalf("CONTROL FAILED: with a healthy probe the script should reach the apply step\n%s", out)
	}
}

// commentHazards returns the substitution constructs in a comment line that a real
// zsh EXECUTES when the line is pasted.
//
// 🔴 IT IS A FUNCTION SO IT CAN BE TESTED. While the list lived inline in the test
// above, removing an entry stayed GREEN — the README happens to contain no example
// today, so a detector with nothing to detect proves nothing about its coverage.
// The predicate now has its own table.
//
// MEASURED ON A REAL PTY (zsh -f -i), pasting `# … <construct> …`:
//
//	backtick EXECUTES · $( ) EXECUTES · <( ) EXECUTES · >( ) EXECUTES ·
//	=( ) EXECUTES  ·  ! ~ * ${}  no side effect
//
// ⚠️ THE DECLARED COUNT WAS WRONG AND MEASURABLY SO: this said "two constructs
// execute and both are refused" while FIVE execute. Process substitution and zsh's
// =( ) were never tried. The number is now the measured one.
func commentHazards(line string) []string {
	var found []string
	for _, bad := range []string{"`", "$(", "<(", ">(", "=("} {
		if strings.Contains(line, bad) {
			found = append(found, bad)
		}
	}
	return found
}

// TestCommentHazards is the predicate's own table — the watchman's watchman.
func TestCommentHazards(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
		want int
	}{
		{"plain prose", "# nothing dangerous here", 0},
		{"backtick", "# see `umask` above", 1},
		{"command substitution", "# run $(touch /tmp/PWNED) here", 1},
		{"process substitution, input", "# diff <(a) b", 1},
		{"process substitution, output", "# tee >(a)", 1},
		{"zsh =( ) substitution", "# see =(cmd)", 1},
		{"both", "# `a` and $(b)", 2},
		{"a lone dollar is not a substitution", "# costs $5", 0},
		{"braces alone were measured harmless", "# ${HOME} is fine", 0},
		{"bang alone was measured harmless", "# careful! really", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := len(commentHazards(tc.line)); got != tc.want {
				t.Errorf("commentHazards(%q) found %d, want %d", tc.line, got, tc.want)
			}
		})
	}
}

// TestRotateScript_IsImmuneToAHostilePsqlrc — B1, pinned by RUNNING.
//
// 🔴 THE FLAG WAS IN THE FILE; ITS EFFECT WAS IN THE OPERATOR'S ENVIRONMENT.
// psql reads ~/.psqlrc AFTER the command line, so `\set ON_ERROR_STOP off` there
// overrides `-v ON_ERROR_STOP=1`. Measured against the real database:
//
//	psqlrc override, no -X : rc=0   (the flag is defeated)
//	same, with -X          : rc=3   (fail-closed restored)
//
// With the flag defeated the script reached "rotate-kek: applied." and exited on
// the tool's code while NOTHING had been written — the operator proceeds to step 3,
// drops TAPPA_TAG_KEK_PREVIOUS, and the whole park is left under the old KEK: every
// tap 500, no attendance row (§4.6). It also re-opened the §4.7 leak the COPY
// redesign was supposed to close, because psql then sends the COPY data lines as
// statements and the wrapped refs land in the server log.
//
// A second mouth: a `\!` line in psqlrc EXECUTES — measured, on a plain `psql -c` —
// and the first call used to run with both KEKs exported into its environment.
//
// The existing check for this was a TEXT SCAN for the flag, i.e. it measured that
// the flag was WRITTEN, not that it was IN FORCE — which is exactly the doctrine
// this file states two hundred lines above and did not follow here.
func TestRotateScript_IsImmuneToAHostilePsqlrc(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script")
	}
	b, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)

	// Every psql invocation must disable the startup file.
	// ⚠️ THE SCAN LOOKS FOR `psql ` ANYWHERE ON A NON-COMMENT LINE, not just at the
	// start: one of the four calls lives inside a command substitution
	// (BYPASS="$(psql …)"), and a prefix-only scan found three and called the
	// fourth clean — the same scanner-shape mistake this file has made before.
	calls := 0
	for _, ln := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(ln)
		// AN INVOCATION PASSES THE DSN. Matching bare "psql " caught the die
		// messages that NAME psql's exit status — prose, not calls. This file has
		// made the prose-vs-code mistake three times now; the rule is the argument.
		if strings.HasPrefix(trimmed, "#") ||
			!strings.Contains(trimmed, "psql ") ||
			!strings.Contains(trimmed, `"$OWNER_DSN"`) {
			continue
		}
		calls++
		if !strings.Contains(trimmed, "-X") && !strings.Contains(trimmed, "--no-psqlrc") {
			t.Errorf("a psql invocation does not pass -X/--no-psqlrc, so ~/.psqlrc can override "+
				"ON_ERROR_STOP and run \\! commands:\n  %s", trimmed)
		}
	}
	if calls < 4 {
		t.Fatalf("only %d psql invocations found; the scan has gone blind", calls)
	}

	// AND THE BEHAVIOUR: a hostile psqlrc must not be able to make the script claim
	// success. HOME points at a directory holding a psqlrc that both turns off
	// ON_ERROR_STOP and runs a command; the stub psql is the real thing's stand-in
	// for everything else.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".psqlrc"),
		[]byte("\\set ON_ERROR_STOP off\n\\! touch "+filepath.Join(dir, "PSQLRC_RAN")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// The stub honours -X the way psql does: it only "reads" the psqlrc when -X is
	// absent, so this exercises the script's argument list rather than psql itself.
	code, out := runScriptWithHome(t, dir, "0", nil, "--apply")
	if _, err := os.Stat(filepath.Join(dir, "PSQLRC_RAN")); err == nil {
		t.Errorf("a \\! line in ~/.psqlrc EXECUTED during the run (exit %d); the script must pass -X\n%s", code, out)
	}
}

// runScriptWithHome is runScript with a chosen HOME, so a hostile ~/.psqlrc can be
// placed in the run's path.
func runScriptWithHome(t *testing.T, home, stubRC string, env []string, args ...string) (int, string) {
	t.Helper()
	// GOMODCACHE/GOCACHE stay pointed at the real ones. Overriding HOME alone sends
	// Go's module cache into the temp dir, which is read-only and makes t.TempDir's
	// cleanup fail — the assertions pass and the test still reports FAIL. This bit
	// once already when HOME was overridden in runScript.
	extra := []string{"HOME=" + home, "PSQLRC_HOME=" + home}
	for _, k := range []string{"GOMODCACHE", "GOCACHE", "GOPATH"} {
		if v := goEnv(t, k); v != "" {
			extra = append(extra, k+"="+v)
		}
	}
	return runScript(t, stubRC, append(env, extra...), args...)
}

// goEnv reads one value from `go env`, so the child toolchain keeps using the real
// caches rather than writing into a throwaway HOME.
func goEnv(t *testing.T, key string) string {
	t.Helper()
	out, err := exec.Command("go", "env", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// TestRotateScript_DoesNotExportTheKEKsToEveryChild — B2, pinned by RUNNING.
//
// 🔴 BOTH KEYS WERE `export`ed, so every child inherited them: the Go toolchain
// invoked by `go build`, and all four psql processes. Combined with the psqlrc
// finding above that was a read path for the keys — a `\!` line in a stray psqlrc
// runs with both KEKs in its environment (measured).
//
// Only the one process that needs them gets them, via an assignment prefix. This is
// checked by RUNNING the script with a stub that records its own environment.
func TestRotateScript_DoesNotExportTheKEKsToEveryChild(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script")
	}
	b, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if strings.Contains(text, "export TAPPA_TAG_KEK") {
		t.Error("the script exports the KEKs; every child process — the Go toolchain and all four psql " +
			"invocations — would inherit both keys")
	}
	// The tool must still receive them, or the rotation cannot work at all.
	if !strings.Contains(text, `TAPPA_TAG_KEK="$KEK_NEW_VALUE"`) ||
		!strings.Contains(text, `TAPPA_TAG_KEK_PREVIOUS="$KEK_OLD_VALUE"`) {
		t.Error("the tool is not given the KEKs via an assignment prefix; the rotation cannot run")
	}
	// BEHAVIOUR: a psql child must not see either key.
	dir := t.TempDir()
	code, out := runScriptWithHome(t, dir, "0", []string{"DUMP_ENV_TO=" + filepath.Join(dir, "env.txt")}, "--apply")
	if data, err := os.ReadFile(filepath.Join(dir, "env.txt")); err == nil {
		// ⚠️ THE ASSERTION IS ON THE ASSIGNMENT AND ON THE VALUE, NOT ON THE NAME
		// PREFIX: "TAPPA_TAG_KEK" also matches TAPPA_TAG_KEK_FILE, which the
		// procedure legitimately passes (it is a PATH, not a key), so a prefix
		// match reported the correct design as a leak.
		for _, forbidden := range []string{"TAPPA_TAG_KEK=", "TAPPA_TAG_KEK_PREVIOUS="} {
			if strings.Contains(string(data), forbidden) {
				t.Errorf("a psql child inherited %s (exit %d)\n%s", forbidden, code, out)
			}
		}
		// And no key VALUE, under any name at all.
		for _, v := range []string{
			base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, 32)),
			base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x22}, 32)),
		} {
			if strings.Contains(string(data), v) {
				t.Errorf("a psql child's environment carries a KEK VALUE (exit %d)\n%s", code, out)
			}
		}
	}
}

// TestRotateScript_RefusesToRunUnderXtrace.
//
// `bash -x`, or an inherited SHELLOPTS=xtrace, prints every expanded command to
// stderr — including the two that read the KEK files — and the runbook tells the
// operator to KEEP that stderr. Measured: both keys appeared in plaintext. There is
// no legitimate reason to trace a key-handling script, so it refuses.
func TestRotateScript_RefusesToRunUnderXtrace(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script")
	}
	dir := t.TempDir()
	kek := filepath.Join(dir, "k.b64")
	if err := os.WriteFile(kek, []byte("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-x", scriptPath(t), "--apply")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + dir, "OWNER_DSN=postgres://stub@127.0.0.1:5432/stub",
		"TAPPA_TAG_KEK_FILE=" + kek, "TAPPA_TAG_KEK_NEW_FILE=" + kek,
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the script ran under `bash -x`; tracing prints the KEKs in plaintext\n%s", out)
	}
	if !strings.Contains(string(out), "REFUSING to run under xtrace") {
		t.Errorf("the refusal does not name xtrace as the reason:\n%s", out)
	}
	// AND the key must not appear in what it did print before refusing.
	if strings.Contains(string(out), "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=") {
		t.Error("the KEK value reached stderr before the refusal")
	}
}

// pasteableBlockCount is the MEASURED number of pasteable bash blocks in the
// rotation section. Three artefacts publish it and this test compares all of them
// against the section itself, so the number cannot drift silently the way "14" did
// when a dead block was deleted.
const pasteableBlockCount = 13

// invocation is one documented rotate-kek call: its arguments AND the comment that
// tells the operator what it does. Both are asserted, because the round-12 defect
// lived entirely in the gap between them.
type invocation struct {
	args    []string
	comment string
}

// TestRotateScript_ClearsTheLoaderInjectionChannels — backlog T49, half one.
//
// 🔴 WHAT WAS WRONG. The script cleared five libpq variables and headed the block
// "TWO CHANNELS SURVIVE -X" — a COUNT, presented as an enumeration, that had not
// counted the strongest channel in the list. LD_PRELOAD reaches `go build` and, far
// worse, reaches the ONE process in the whole procedure that holds BOTH KEKS in its
// environment ($WORK/rotatekek). A preloaded object there reads the old key and the
// new key out of a live process — strictly stronger than the PGOPTIONS channel that
// WAS being closed, and free.
//
// DYLD_INSERT_LIBRARIES was dismissed because this machine's OS drops it for
// protected binaries. That is a property of the MEASURER's platform, not the
// operator's: on Linux there is no such protection.
//
// This test asserts the clearing BY RUNNING: the stub psql dumps its own
// environment, and neither variable may survive into it.
func TestRotateScript_ClearsTheLoaderInjectionChannels(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script")
	}
	b, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	// The claim that was wrong must not come back.
	if strings.Contains(string(b), "TWO CHANNELS SURVIVE") {
		t.Error("the script again counts the surviving channels; the count was wrong once " +
			"(it omitted LD_PRELOAD, the only one that reaches the two-KEK process) and a number " +
			"doing the work of a guarantee is what backlog T49 was about")
	}

	dir := t.TempDir()
	envDump := filepath.Join(dir, "env.txt")
	const preloadProbe = "/probe/rotate-kek-t49.so"
	code, out := runScriptWithHome(t, dir, "0", []string{
		"DUMP_ENV_TO=" + envDump,
		"LD_PRELOAD=" + preloadProbe,
		"DYLD_INSERT_LIBRARIES=" + preloadProbe,
		"DYLD_LIBRARY_PATH=/probe/lib",
	}, "--apply")

	data, err := os.ReadFile(envDump)
	if err != nil {
		t.Fatalf("the stub psql never recorded its environment (exit %d)\n%s", code, out)
	}
	// POSITIVE CONTROL: the dump really is this run's environment. Without it, an
	// empty or stale file would make every absence below meaningless.
	if !strings.Contains(string(data), "DUMP_ENV_TO="+envDump) {
		t.Fatalf("the environment dump is not from this run; the absences below prove nothing:\n%s", data)
	}
	for _, name := range []string{"LD_PRELOAD=", "DYLD_INSERT_LIBRARIES=", "DYLD_LIBRARY_PATH="} {
		if strings.Contains(string(data), name) {
			t.Errorf("%s survived into a child process (exit %d). It reaches `go build` and the "+
				"rotatekek process, which is the only one holding both KEKs.", name, code)
		}
	}
	if strings.Contains(string(data), preloadProbe) {
		t.Errorf("the preload path survived under some other name (exit %d)", code)
	}
}

// TestRotateScript_ClearsTheGoToolchainInjectionChannels — M8-03 round 4, S4.
//
// 🔴 THE LIST ABOVE FAILED ITS OWN CRITERION. It declared LD_PRELOAD "the one that
// matters most" and gave as the reason that it reaches `go build`. GOFLAGS reaches
// the same `go build`, the same way, and was not cleared. Measured on this tree:
//
//	GOFLAGS=-toolexec=<script> go build -a -o <work>/rotatekek ./cmd/rotatekek
//	  -> the script ran 303 times, and all 303 invocations saw the rotation's
//	     environment (TAPPA_TAG_KEK_FILE was readable in every one).
//
// It is strictly easier to use than LD_PRELOAD: no compiled object, identical on
// Linux and Darwin, unaffected by SIP. GOTOOLCHAIN redirects which toolchain binary
// runs at all, and GOENV names a config file that can set GOFLAGS — so clearing
// GOFLAGS alone would leave the same channel one level down.
//
// THIS TEST IS THE AUDITOR'S PROBE, RUN AGAINST THE REAL SCRIPT: the toolexec
// recorder must not fire even once, and the same three variables must not survive
// into a child's environment either.
func TestRotateScript_ClearsTheGoToolchainInjectionChannels(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script, which builds cmd/rotatekek")
	}

	dir := t.TempDir()
	log := filepath.Join(dir, "toolexec.log")
	recorder := filepath.Join(dir, "toolexec.sh")
	body := "#!/bin/sh\n" +
		"echo invoked >> " + log + "\n" +
		"exec \"$@\"\n"
	if err := os.WriteFile(recorder, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}

	// GOCACHEPROG, added in round 5 alongside GOROOT: the go command EXECUTES this
	// as its build-cache backend, so it is the same channel with a different name.
	// It gets its own recorder because the two must be told apart in a failure.
	cacheLog := filepath.Join(dir, "cacheprog.log")
	cacheProg := filepath.Join(dir, "cacheprog.sh")
	if err := os.WriteFile(cacheProg,
		[]byte("#!/bin/sh\necho invoked >> "+cacheLog+"\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 🔴 POSITIVE CONTROL FOR THAT ONE, because "it never ran" is also what a typo in
	// the variable name looks like. With GOCACHEPROG honoured, a bare `go build`
	// executes it — measured here rather than asserted.
	ctlCmd := exec.Command("go", "build", "-o", filepath.Join(dir, "ctl.bin"), repoRoot(t)+"/cmd/rotatekek")
	ctlCmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GOCACHEPROG=" + cacheProg,
	}
	ctlOut, _ := ctlCmd.CombinedOutput()
	if invocationCount(t, cacheLog) == 0 {
		t.Fatalf("GOCACHEPROG did not run even when it WAS honoured; the probe is dead and the "+
			"absence asserted below would mean nothing\n%s", ctlOut)
	}
	if err := os.Remove(cacheLog); err != nil {
		t.Fatal(err)
	}

	envDump := filepath.Join(dir, "env.txt")
	code, out := runScriptWithHome(t, dir, "0", []string{
		"DUMP_ENV_TO=" + envDump,
		"GOFLAGS=-toolexec=" + recorder,
		"GOTOOLCHAIN=go1.0.0",
		"GOENV=" + filepath.Join(dir, "hostile-goenv"),
		"GOCACHEPROG=" + cacheProg,
	}, "--apply")

	// 🔴 THE SHARP HALF: the recorder must never have run. Its absence is what says
	// the injection did not reach the toolchain, rather than that it reached it and
	// was harmless.
	if b, err := os.ReadFile(log); err == nil && len(b) > 0 {
		t.Errorf("GOFLAGS=-toolexec survived into `go build`: the recorder ran %d time(s) "+
			"(exit %d). Every invocation sees the rotation's environment.\n%s",
			bytes.Count(b, []byte("invoked")), code, out)
	}
	if n := invocationCount(t, cacheLog); n != 0 {
		t.Errorf("GOCACHEPROG survived into `go build`: the go command executed it %d time(s) "+
			"(exit %d). It sees the rotation's environment and it decides what the build links.\n%s",
			n, code, out)
	}

	// POSITIVE CONTROL on the environment dump, for the reason its sibling above
	// gives: without it an empty or stale file makes every absence meaningless.
	data, err := os.ReadFile(envDump)
	if err != nil {
		t.Fatalf("the stub psql never recorded its environment (exit %d)\n%s", code, out)
	}
	if !strings.Contains(string(data), "DUMP_ENV_TO="+envDump) {
		t.Fatalf("the environment dump is not from this run; the absences below prove nothing:\n%s", data)
	}
	for _, name := range []string{"GOFLAGS=", "GOTOOLCHAIN=", "GOENV=", "GOCACHEPROG="} {
		if strings.Contains(string(data), name) {
			t.Errorf("%s survived into a child process (exit %d). It reaches `go build`, whose "+
				"output is the ONE process that holds both KEKs.", name, code)
		}
	}

	// GOTOOLCHAIN=go1.0.0 is the second control: had it survived, the `go` command
	// would have tried to fetch a toolchain that does not exist and the build would
	// have failed rather than succeeded quietly.
	if strings.Contains(out, "go1.0.0") {
		t.Errorf("the run mentions the hostile GOTOOLCHAIN, so it was consulted (exit %d):\n%s", code, out)
	}
}

// fakeGoroot builds a GOROOT whose ONLY non-symlink is pkg/tool/<plat>/compile,
// replaced by a recorder that appends one line per invocation and then execs the
// real compiler, so the build still succeeds. It returns the tree and the log path.
//
// Everything else is a symlink into the real GOROOT, which is what makes this a
// realistic attack rather than a broken toolchain: `compile -V=full` reports the
// REAL compiler's version, so the build cache does not even notice.
func fakeGoroot(t *testing.T, dir string) (root, log string) {
	t.Helper()
	realRoot := goEnv(t, "GOROOT")
	if realRoot == "" {
		t.Skip("go env GOROOT is empty; nothing to shadow")
	}
	plat := goEnv(t, "GOOS") + "_" + goEnv(t, "GOARCH")
	root = filepath.Join(dir, "goroot")
	log = filepath.Join(dir, "compile-invocations.log")

	toolDir := filepath.Join(root, "pkg", "tool", plat)
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Mirror one directory level, symlinking every entry except the one name that
	// has to be traversed further.
	mirror := func(realDir, fakeDir, except string) {
		entries, err := os.ReadDir(realDir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() == except {
				continue
			}
			if err := os.Symlink(filepath.Join(realDir, e.Name()), filepath.Join(fakeDir, e.Name())); err != nil {
				t.Fatal(err)
			}
		}
	}
	mirror(realRoot, root, "pkg")
	mirror(filepath.Join(realRoot, "pkg"), filepath.Join(root, "pkg"), "tool")
	mirror(filepath.Join(realRoot, "pkg", "tool"), filepath.Join(root, "pkg", "tool"), plat)
	mirror(filepath.Join(realRoot, "pkg", "tool", plat), toolDir, "compile")

	body := "#!/bin/sh\n" +
		"echo invoked >> " + log + "\n" +
		"exec " + filepath.Join(realRoot, "pkg", "tool", plat, "compile") + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(toolDir, "compile"), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, log
}

// TestRotateScript_AHostileGOROOTNeverReachesTheBuild — M8-03 round 5.
//
// 🔴 THE SAME DEFECT AS ROUND 4'S, ONE ROUND LATER. The list above declares its own
// criterion — "clear every inherited variable that can inject code or arguments into
// a process this script starts" — and GOROOT was in neither the list nor the
// knowingly-left block; `grep -c GOROOT scripts/rotate-kek.sh` returned 0.
//
// MEASURED against the real script, after its own unset line, with the tree
// fakeGoroot builds: the attacker's `compile` ran 213 times, all 213 saw
// TAPPA_TAG_KEK_FILE, and `go build` SUCCEEDED. It is worse than -toolexec in one
// specific way: -toolexec wraps the toolchain, whereas replacing `compile` puts the
// attacker's code INTO the produced binary — the one process that holds BOTH KEKs.
//
// ⚠️ THE MARGINAL CAPABILITY IS ~ZERO AND THAT IS SAID PLAINLY: `go` is resolved
// through PATH, PATH is knowingly left, and anyone who can set GOROOT can set PATH.
// The finding is about the LIST failing its own criterion for a third round running,
// which is why the fix is not only this variable but the enumeration test below.
func TestRotateScript_AHostileGOROOTNeverReachesTheBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script, which builds cmd/rotatekek")
	}

	dir := t.TempDir()
	root, log := fakeGoroot(t, dir)

	// 🔴 POSITIVE CONTROL FIRST, so a green result cannot mean "the probe is dead".
	// A throwaway package with a unique constant cannot be in the build cache, so
	// its compile MUST go through the shadowed tool.
	ctl := filepath.Join(dir, "ctl")
	if err := os.MkdirAll(ctl, 0o755); err != nil {
		t.Fatal(err)
	}
	unique := "rotatekek-goroot-probe-" + filepath.Base(dir)
	if err := os.WriteFile(filepath.Join(ctl, "go.mod"), []byte("module rotatekekprobe\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctl, "main.go"),
		[]byte("package main\n\nconst unique = \""+unique+"\"\n\nfunc main() { println(unique) }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctlCmd := exec.Command("go", "build", "-o", filepath.Join(dir, "ctl.bin"), ".")
	ctlCmd.Dir = ctl
	ctlCmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GOROOT=" + root,
		"GOCACHE=" + goEnv(t, "GOCACHE"),
		"GOMODCACHE=" + goEnv(t, "GOMODCACHE"),
		"GOPATH=" + goEnv(t, "GOPATH"),
		"GOFLAGS=-mod=mod",
	}
	if ctlOut, err := ctlCmd.CombinedOutput(); err != nil {
		t.Fatalf("the positive control could not build at all, so this test proves nothing: %v\n%s", err, ctlOut)
	}
	before := invocationCount(t, log)
	if before == 0 {
		t.Fatalf("the shadowed compiler did not run even when GOROOT WAS honoured; the probe is "+
			"dead and the absence asserted below would mean nothing (control package %s)", unique)
	}
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}

	// AND NOW THE SCRIPT, with the same hostile GOROOT.
	envDump := filepath.Join(dir, "env.txt")
	code, out := runScriptWithHome(t, dir, "0", []string{
		"DUMP_ENV_TO=" + envDump,
		"GOROOT=" + root,
	}, "--apply")

	if after := invocationCount(t, log); after != 0 {
		t.Errorf("a hostile GOROOT survived into `go build`: the attacker's compiler ran %d time(s) "+
			"(control: %d, exit %d). Replacing `compile` puts attacker code INTO $WORK/rotatekek, "+
			"the one process that holds both KEKs.\n%s", after, before, code, out)
	}

	data, err := os.ReadFile(envDump)
	if err != nil {
		t.Fatalf("the stub psql never recorded its environment (exit %d)\n%s", code, out)
	}
	if !strings.Contains(string(data), "DUMP_ENV_TO="+envDump) {
		t.Fatalf("the environment dump is not from this run; the absence below proves nothing:\n%s", data)
	}
	if strings.Contains(string(data), "GOROOT=") {
		t.Errorf("GOROOT survived into a child process (exit %d)", code)
	}
}

// invocationCount reports how many lines the recorder wrote, treating "no file" as
// zero — the recorder only creates it when it actually runs.
func invocationCount(t *testing.T, log string) int {
	t.Helper()
	b, err := os.ReadFile(log)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0
		}
		t.Fatal(err)
	}
	return bytes.Count(b, []byte("invoked"))
}

// TestRotateScript_AccountsForEveryGoToolchainVariable — M8-03 round 5, the part
// that is meant to end the series rather than add one more name to it.
//
// 🔴 THREE ROUNDS IN A ROW THE UNSET LIST WAS SHORT, AND EACH TIME IT WAS FOUND BY
// A HUMAN RUNNING ONE GREP: round 3/4 found GOFLAGS (`grep -c GOFLAGS` -> 0), round
// 5 found GOROOT and, once someone looked at the toolchain's own list rather than at
// their memory, GOCACHEPROG as well. The defect is not any single variable; it is
// that the criterion was a SENTENCE and nothing executed it.
//
// So this executes it. The names come from `go env` — the installed toolchain's own
// enumeration — and every one of them must be MENTIONED in scripts/rotate-kek.sh,
// either in the unset list or in the block that says why it is knowingly left. A new
// Go release that invents a variable turns this red until somebody decides which it
// is, which is exactly the review that did not happen three times.
//
// ⚠️ WHAT IT DOES NOT CLAIM. "Mentioned" is not "cleared", and `go env` is not the
// set of all variables the toolchain reads (nothing here would catch a psql or bash
// variable). It converts SILENCE into a failure; the behavioural probes above are
// what show a specific channel is shut.
func TestRotateScript_AccountsForEveryGoToolchainVariable(t *testing.T) {
	t.Parallel()

	out, err := exec.Command("go", "env").Output()
	if err != nil {
		t.Skipf("go env unavailable: %v", err)
	}
	b, err := os.ReadFile(scriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)

	// A name counts as mentioned only on a whole-word match: without the boundary,
	// GOTELEMETRY would be "found" inside GOTELEMETRYDIR and a genuine gap would
	// read as covered.
	mentions := func(name string) bool {
		for i := 0; ; {
			j := strings.Index(text[i:], name)
			if j < 0 {
				return false
			}
			j += i
			startOK := j == 0 || !isNameByte(text[j-1])
			end := j + len(name)
			endOK := end == len(text) || !isNameByte(text[end])
			if startOK && endOK {
				return true
			}
			i = j + 1
		}
	}

	// CONTROL: the matcher must be able to say no, or every "yes" below is noise.
	if mentions("GOTOTALLYFICTIONALVARIABLE") {
		t.Fatal("the matcher reports a name that cannot be in the file; it is measuring nothing")
	}

	var names, missing []string
	for _, line := range strings.Split(string(out), "\n") {
		k, _, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || k == "" {
			continue
		}
		names = append(names, k)
		if !mentions(k) {
			missing = append(missing, k)
		}
	}
	if len(names) < 20 {
		t.Fatalf("`go env` yielded only %d names; the enumeration has gone blind", len(names))
	}
	if len(missing) > 0 {
		t.Errorf("scripts/rotate-kek.sh does not mention %d of the %d variables `go env` reports: %s\n"+
			"Each must be either in the unset list or in the block that records why it is knowingly "+
			"left. This is the check that a human ran by hand in rounds 3, 4 and 5, and it found a "+
			"hole every time.", len(missing), len(names), strings.Join(missing, " "))
	}
}

// isNameByte reports whether c can appear inside an environment variable name.
func isNameByte(c byte) bool {
	return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
}

// TestRotateScript_RefusesANonURIDSN — backlog T49, half two.
//
// 🔴 WHY A KEYWORD DSN IS NOT A STYLE QUESTION. `dbname=tappa` omits host, port and
// user, so libpq takes them from PGHOST/PGPORT/PGUSER — and from PGSERVICE, whose
// redirection the script's own comment measures as effective against EXACTLY this
// shape and ineffective against a URI. So an inherited environment could send the
// entire run to a different server; the bypass probe, the park-size guard and both
// post-conditions would all pass THERE, the script would report success, and the
// real park would never have been touched.
//
// The refusal must happen BEFORE anything is read, so the failure is "nothing
// happened" rather than "something happened somewhere else".
func TestRotateScript_RefusesANonURIDSN(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script")
	}
	dir := t.TempDir()
	good := filepath.Join(dir, "kek.b64")
	if err := os.WriteFile(good,
		[]byte(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0x33}, 32))), 0o600); err != nil {
		t.Fatal(err)
	}

	// ⚠️ THE EMPTY STRING IS NOT IN THIS LIST, and leaving it out is measured
	// rather than lazy: `: "${OWNER_DSN:?...}"` refuses an unset/empty DSN sixty
	// lines EARLIER, so an empty case never reaches the URI guard and would fail
	// this test for the wrong reason. TestRotateScript_RefusesDegenerateInputs
	// covers it where it is actually enforced.
	for _, dsn := range []string{"dbname=tappa", "stub", "host=/tmp dbname=tappa"} {
		t.Run("dsn="+dsn, func(t *testing.T) {
			cmd := exec.Command("bash", scriptPath(t), "--apply")
			cmd.Env = []string{
				"PATH=" + os.Getenv("PATH"), "HOME=" + dir,
				"OWNER_DSN=" + dsn,
				"TAPPA_TAG_KEK_FILE=" + good,
				"TAPPA_TAG_KEK_NEW_FILE=" + good,
			}
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("a non-URI DSN was accepted; the run could be redirected by the environment\n%s", out)
			}
			// 🔴 THE ASSERTION IS ON THE URI REFUSAL SPECIFICALLY, NOT ON "it failed".
			// Measured: with the guard mutated away, all four cases STILL exited
			// non-zero — the connection probe fails when no stub psql is on PATH —
			// and its die message happens to contain the string "OWNER_DSN". So both
			// of the obvious assertions ("non-zero exit", "mentions OWNER_DSN")
			// passed on a script with NO guard at all. This one does not.
			if !strings.Contains(string(out), "must be a URI") {
				t.Errorf("the run failed, but NOT at the URI guard — so nothing here shows the "+
					"guard exists. Output:\n%s", out)
			}
			// And it must refuse BEFORE reaching the database, or the failure is
			// "something happened somewhere else" rather than "nothing happened".
			if strings.Contains(string(out), "read ") || strings.Contains(string(out), "rows") {
				t.Errorf("the script got as far as reading the park before refusing:\n%s", out)
			}
		})
	}

	// AND THE POSITIVE CONTROL: a URI DSN is not refused for this reason. Without
	// it, a guard that rejected everything would pass the loop above.
	code, out := runScriptWithHome(t, t.TempDir(), "0", nil, "--apply")
	if strings.Contains(out, "must be a URI") {
		t.Errorf("a URI DSN was refused by the URI guard (exit %d)\n%s", code, out)
	}
}
