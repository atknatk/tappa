package main

// scriptguards_test.go — the gate behind the two SHELL SCRIPTS that carry
// operational risk and had nothing holding them (backlog T55, closed M8-04 FAZ B3).
//
// 🔴 WHY THIS FILE EXISTS, MEASURED RATHER THAN ARGUED. Before it, the repository
// contained exactly one test that executed a shell script — cmd/rotatekek/
// script_test.go, and it targets rotate-kek.sh alone. The other two were unread by
// anything. Measured at the commit before this file (239d427):
//
//	git grep -ln 'exec.Command("bash"' 239d427 -- '*_test.go'
//	  -> cmd/rotatekek/script_test.go          (rotate-kek.sh, and only it)
//
//	git grep -n 'db-reset.sh\|pg-restore-verify' 239d427 -- '*_test.go'
//	  -> internal/db/viewsecurity_test.go:17   PROSE in a comment
//	  -> internal/db/viewsecurity_test.go:20   PROSE in a comment
//
//	make check = fmt gen lint test              -> reads no .sh
//	CI                                          -> runs neither
//	scripts/redline-check.sh                    -> exempts one BY NAME
//
// ⚠️ THE SECOND COMMAND USED TO BE PUBLISHED HERE AS ANSWERING "zero", AND IT DOES
// NOT. An audit re-ran it and got the two lines above (M8-04 FAZ B3, correction
// round 2). The CLAIM was right — neither script was executed by any test — but it
// was evidenced with a command that does not produce the stated output, which is the
// same defect as an unheld number: the next reader runs it, gets something else, and
// has no way to tell which half was wrong. Both commands are now printed with the
// output they actually give, and the distinction they turn on (a NAME in a comment
// is not a RUN) is written out rather than implied.
//
// And one of them DELETES DATA. The consequence was measured during M6: with the
// `-f`/`-p` pin removed from scripts/db-reset.sh, the script stopped a FOREIGN
// compose project and destroyed its volume while its own banner still said
// "pinned" — and make check, make audit, redline-check and CI would all have
// stayed green.
//
// 🔴 THE SHAPE IS THE ONE THE PRECEDENT ALREADY PROVED, not a new invention.
// cmd/rotatekek/script_test.go closes the same class for the same kind of file:
// parse the script under bash, assert the LITERAL guards that make it safe, and
// RUN the refusals so they are exercised rather than described. The builder's usual
// defence — "standing up a rig is out of scope" — is not available here, because
// the rig is 1 566 lines long and already in this tree.
//
// ⚠️ WHAT THIS FILE DOES NOT DO, said rather than discovered later:
//
//   - It never runs db-reset.sh's DESTRUCTIVE path. Every execution below is one the
//     script REFUSES, and the refusals used all return before `down --volumes`.
//     There is no safe way to exercise the destructive branch from a test, and a
//     test that found one would be a test that empties a developer's database.
//   - It is a LITERAL check on the script text, so it holds a guard being DELETED
//     and not a guard being subtly weakened in place. That is the same counted limit
//     internal/sun/verify_mac_test.go and cmd/tappa/constanttime_test.go write down
//     about source-reading tests: they read text, not semantics.
//   - It pins STRUCTURE, not PROSE. Three of the mutations T55 lists are edits to
//     comments ("⚠️ NOT MEASURED" flipped to "✅ MEASURED", a falsified figure put
//     back). Those stay uncovered ON PURPOSE: a test that pinned sentences would be
//     a change detector whose natural repair is to update the expected sentence,
//     which teaches the opposite of the lesson. What is pinned is the machinery a
//     sentence would be lying ABOUT.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// scriptPathIn returns an absolute path under scripts/ and fails if it is gone —
// a renamed carrier script must fail loudly here rather than make every assertion
// below vacuous.
func scriptPathIn(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(repoRoot, "scripts", name)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("scripts/%s is not in the tree (%v). If it was renamed, rename it HERE too: "+
			"this file is the only thing reading it.", name, err)
	}
	return p
}

func readScript(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(scriptPathIn(t, name))
	if err != nil {
		t.Fatalf("read scripts/%s: %v", name, err)
	}
	return string(b)
}

// codeOf strips whole-line comments.
//
// 🔴 IT EXISTS BECAUSE THE FIRST DRAFT OF THIS FILE MADE THE PROSE-VERSUS-CODE
// MISTAKE, which is the one cmd/rotatekek/script_test.go records making three times.
// Two checks below went red on scripts/db-reset.sh's own HEADER: the ordering check
// found `down --volumes` inside a paragraph describing what would have happened on
// another machine, and the hand-written-command check found the very command the
// header quotes in order to say it was removed. Both scripts document themselves at
// length, so a scan of the raw text measures the documentation, not the script.
//
// ⚠️ IT IS A LINE RULE, NOT A LEXER: a trailing comment on a code line is kept, and
// a `#` inside a string is not a comment. Both are the safe direction here — keeping
// too much can only produce a false RED, which a reader investigates; dropping too
// much produces a false GREEN, which nobody sees.
func codeOf(text string) string {
	var b strings.Builder
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "#") {
			b.WriteByte('\n')
			continue
		}
		b.WriteString(ln)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestCarrierScripts_ParseUnderBash is the cheapest of the three and the one whose
// absence is least excusable: neither script is executed by any test, any make
// target or CI, so before this file a SYNTAX ERROR in either one would have shipped.
func TestCarrierScripts_ParseUnderBash(t *testing.T) {
	t.Parallel()
	// 🔴 EACH SCRIPT IS PARSED BY THE INTERPRETER IT DECLARES, and that is not
	// pedantry: db-reset.sh is `#!/usr/bin/env bash` and uses [[ ]] and arrays;
	// pg-restore-verify.sh is `#!/bin/sh` on purpose, so it runs inside the postgres
	// image, which has no bash. Checking both with `bash -n` would pass a
	// bashism that the sh script's real interpreter would reject at run time —
	// the same "the check does not see what the consumer sees" shape
	// docs/plan/agent-brief.md records being defeated three times.
	scripts := map[string]string{
		"db-reset.sh":          "bash",
		"pg-restore-verify.sh": "sh",
		// T45: the two halves of the nightly CronJob had no parse gate either; both
		// run under busybox ash (postgres:17-alpine, rclone/rclone).
		"pg-backup.sh":      "sh",
		"pg-backup-ship.sh": "sh",
	}
	for name, shell := range scripts {
		path := scriptPathIn(t, name)
		text := readScript(t, name)

		// The declared interpreter must BE the one this test parses with, or the
		// pairing above is a guess that goes stale on the first shebang edit.
		firstLine, _, _ := strings.Cut(text, "\n")
		if !strings.HasSuffix(firstLine, "/"+shell) && !strings.HasSuffix(firstLine, " "+shell) {
			t.Errorf("scripts/%s declares %q but this test parses it with %s. Change BOTH in the "+
				"same edit, or the parse check stops matching the interpreter that will really "+
				"run it.", name, firstLine, shell)
			continue
		}
		if out, err := exec.Command(shell, "-n", path).CombinedOutput(); err != nil {
			t.Errorf("%s -n scripts/%s failed: %v\n%s", shell, name, err, out)
		}
		if !strings.Contains(text, "set -eu") {
			t.Errorf("scripts/%s does not set -eu. A carrier script that continues past an "+
				"error is how a half-done restore and a half-done reset both look like success",
				name)
		}
	}
}

// TestDbReset_KeepsItsStructuralGuards holds the literals that make the destructive
// target survivable. Each one is named with the failure it prevents, because a
// literal with no reason beside it is the change detector this repository refuses.
func TestDbReset_KeepsItsStructuralGuards(t *testing.T) {
	t.Parallel()
	for _, problem := range dbResetStructuralProblems(readScript(t, "db-reset.sh")) {
		t.Error(problem)
	}
}

// dbResetStructuralProblems returns one sentence per structural guard missing from
// db-reset.sh, and returns nothing when the script is intact.
//
// 🔴 IT IS A FUNCTION RATHER THAN A TEST BODY BECAUSE TWO TESTS NEED IT, AND ONE OF
// THEM RUNS THE SCRIPT. TestDbReset_RefusesADaemonOnAnotherMachine executes
// db-reset.sh — safely, on paths the script refuses — and the reason those paths ARE
// safe is that the guards below sit above `down --volumes`. Ordering was doing that
// job and doing it by accident: the behavioural test does not call t.Parallel() and
// so runs in the serial phase, while the test asserting the guards DOES call it and
// is therefore deferred until the serial phase ends. Every `make check` and every CI
// run executed the script twice before anything had checked what it would do.
//
// Nothing was broken by it: the locality probe and the schema gate both return
// before the destructive statement, measured. But an ordering that holds by
// coincidence stops holding under `-run`, under `-shuffle`, and the moment somebody
// adds t.Parallel() to the other test. Making the precondition a CALL removes the
// dependency on order entirely.
func dbResetStructuralProblems(text string) []string {
	code := codeOf(text)
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	// 🔴 THE PIN, AND THE MEASURED FAILURE IT PREVENTS. Without `-f`/`-p`, compose
	// reads COMPOSE_FILE and COMPOSE_PROJECT_NAME out of the ambient environment —
	// which the Makefile's `-include .env` + bare `export` puts there — and the
	// script deleted a foreign project's volume from this repository's own
	// directory. `cd` is no protection: an ABSOLUTE COMPOSE_FILE ignores it.
	for _, want := range []string{
		`COMPOSE=(docker compose -f docker-compose.yml -p "$project")`,
		`project=tappa`,
	} {
		if !strings.Contains(text, want) {
			add("scripts/db-reset.sh no longer contains %q. That pin is the only thing "+
				"between this target and another compose project's volumes (measured: without "+
				"it, a foreign project's data volume was destroyed while the banner still said "+
				"'pinned').", want)
		}
	}

	// EVERY compose call goes through the array. A bare `docker compose` anywhere in
	// the file is a call that escaped the pin — which is the whole reason the pin
	// lives in one variable instead of being written out at each site.
	for i, ln := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "#") || !strings.Contains(trimmed, "docker compose ") {
			continue
		}
		if strings.Contains(trimmed, "COMPOSE=(") {
			continue
		}
		add("scripts/db-reset.sh:%d calls compose without going through ${COMPOSE[@]}:\n  %s\n"+
			"An unpinned call reads COMPOSE_FILE/COMPOSE_PROJECT_NAME from the environment.",
			i+1, trimmed)
	}

	// THE LOCALITY PROBE AND ITS COMPARISON. The scheme check alone accepts a unix
	// socket forwarded from another machine (measured: `ssh -L /tmp/x.sock:...`),
	// so what establishes locality is making the daemon hand back this checkout's
	// own file and comparing the BYTES.
	for _, want := range []string{
		`docker context inspect --format '{{.Endpoints.docker.Host}}'`,
		`cmp -s scripts/db-init/01-roles.sql`,
		`down --volumes`,
	} {
		if !strings.Contains(text, want) {
			add("scripts/db-reset.sh no longer contains %q — the locality probe's chain is "+
				"broken, or the destructive statement it guards has moved and this test is now "+
				"guarding nothing", want)
		}
	}

	// 🔴 ORDER MATTERS AND IS ASSERTED. A probe that ran AFTER `down --volumes`
	// would be a post-mortem. This is the one property a literal check can hold
	// about sequencing, and it is cheap.
	if strings.Index(code, "cmp -s scripts/db-init/01-roles.sql") > strings.Index(code, "down --volumes") {
		add("scripts/db-reset.sh runs its locality probe AFTER the volume is removed; " +
			"a guard that fires after the deletion is not a guard")
	}

	// THE OPERATOR INSTRUCTION IS BUILT FROM THE PIN, NOT RETYPED (backlog T56 item
	// 2). The file's own comment above `project=` says the name is held in ONE
	// variable "and printed from the SAME one" — and one line 80 rows further down
	// hand-wrote `docker compose -p tappa logs db`, missing -f and repeating the
	// project name, which is exactly the fault that paragraph diagnoses.
	if strings.Contains(code, "docker compose -p tappa logs") {
		add("scripts/db-reset.sh tells the operator to run a HAND-WRITTEN compose command " +
			"(`docker compose -p tappa logs`). It repeats the project name the file holds in " +
			"$project and omits -f, so it points at whatever COMPOSE_FILE says and stops " +
			"matching the moment $project changes — the file's own diagnosis, one screen down.")
	}

	return problems
}

// TestDbReset_RefusesADaemonOnAnotherMachine is the BEHAVIOURAL half: the refusals
// are executed, not described.
//
// 🔴 EVERY CASE HERE IS ONE THE SCRIPT REFUSES, and the refusal returns BEFORE
// `down --volumes` — measured by running each of them against this machine and
// confirming the tappa volume was still present afterwards. There is deliberately
// no case that reaches the destructive branch.
func TestDbReset_RefusesADaemonOnAnotherMachine(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the script")
	}
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not on PATH; the script's first refusal is about docker itself")
	}

	// 🔴 THE PRECONDITION, CHECKED HERE RATHER THAN LEFT TO TEST ORDER. What makes it
	// safe to RUN this script is that its guards return before `down --volumes`; if a
	// guard has been deleted, the run below stops being a refusal and becomes a
	// deletion. TestDbReset_KeepsItsStructuralGuards asserts the same thing, but it
	// calls t.Parallel() and is therefore deferred past the whole serial phase — so
	// on every `make check` and every CI run the script executed twice BEFORE the
	// check that says the execution is safe. Calling it makes the dependency real
	// instead of positional, and survives `-run`, `-shuffle` and reordering.
	if problems := dbResetStructuralProblems(readScript(t, "db-reset.sh")); len(problems) > 0 {
		t.Fatalf("REFUSING TO EXECUTE scripts/db-reset.sh: %d structural guard(s) are "+
			"missing, so a run that is supposed to be refused may reach `down --volumes`:\n  %s",
			len(problems), strings.Join(problems, "\n  "))
	}

	script := scriptPathIn(t, "db-reset.sh")

	tests := []struct {
		name string
		host string
		want string
	}{
		{"a tcp endpoint is another machine", "tcp://198.51.100.7:2375", "NOT a local unix socket"},
		{"an ssh endpoint is another machine", "ssh://nobody@198.51.100.7", "NOT a local unix socket"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command("bash", script)
			cmd.Dir = repoRoot
			// DATABASE_URL is dropped: with it naming another database the script refuses
			// EARLIER (scripts/compose-db-only.sh), and this case is about the daemon.
			cmd.Env = append(envWithout("DATABASE_URL"), "DOCKER_HOST="+tc.host)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("the script EXITED 0 with DOCKER_HOST=%s. It was about to delete "+
					"volumes on another machine:\n%s", tc.host, out)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("the refusal does not say %q, so the operator cannot tell WHY:\n%s",
					tc.want, out)
			}
			// AND IT NEVER GOT AS FAR AS ANNOUNCING A DESTRUCTION. The banner lives
			// after both guards on purpose (the file says so); this proves it.
			if strings.Contains(string(out), "ABOUT TO BE DESTROYED") {
				t.Errorf("the script announced a destruction on a run it then refused; the "+
					"banner has moved above the guards:\n%s", out)
			}
		})
	}
}

// envWithout is this process's environment minus the named variables.
func envWithout(names ...string) []string {
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		keep := true
		for _, n := range names {
			if name == n {
				keep = false
			}
		}
		if keep {
			out = append(out, kv)
		}
	}
	return out
}

// TestComposeScripts_RefuseADatabaseURLThatNamesAnotherDatabase is scripts/compose-db-only.sh,
// executed. seed.sh and db-reset.sh reach the compose database whatever DATABASE_URL says, so with
// DATABASE_URL naming any other database each must refuse -- and refuse BEFORE docker.
//
// 🔴 NO RUN HERE CAN REACH A DATABASE. PATH holds dirname alone (the scripts' own `cd` needs it),
// so docker cannot resolve: a run the guard wrongly let through stops at the script's own "docker
// is not on PATH" check, having written and deleted nothing. That check is also what tells the two
// outcomes apart: a refusal never gets to it, and the controls -- the compose address, and no
// DATABASE_URL at all -- pass the guard and stop exactly there, so the refusals are the guard's.
func TestComposeScripts_RefuseADatabaseURLThatNamesAnotherDatabase(t *testing.T) {
	if testing.Short() {
		t.Skip("executes the scripts")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skipf("bash is not on PATH: %v", err)
	}
	dirname, err := exec.LookPath("dirname")
	if err != nil {
		t.Fatalf("dirname is not on PATH: %v", err)
	}
	bin := t.TempDir()
	if err := os.Symlink(dirname, filepath.Join(bin, "dirname")); err != nil {
		t.Fatal(err)
	}
	const refusal, dockerCheck = "your DATABASE_URL points elsewhere", "'docker'"
	for _, script := range []string{"seed.sh", "db-reset.sh"} {
		path := scriptPathIn(t, script)
		for _, c := range []struct {
			name, url string
			refused   bool
		}{
			{"another port", "postgres://app@127.0.0.1:55460/tappa", true},
			{"another host", "postgres://app@db.example.test:5432/tappa", true},
			{"a keyword DSN", "host=localhost port=5432 dbname=tappa", true},
			{"control: the compose address", "postgres://app@localhost:5432/tappa?sslmode=disable", false},
			{"control: no DATABASE_URL", "", false},
		} {
			t.Run(script+"/"+c.name, func(t *testing.T) {
				env := append(envWithout("PATH", "DATABASE_URL", "DOCKER_HOST", "DOCKER_CONTEXT"), "PATH="+bin)
				if c.url != "" {
					env = append(env, "DATABASE_URL="+c.url)
				}
				cmd := exec.Command(bash, path)
				cmd.Dir = repoRoot
				cmd.Env = env
				out, err := cmd.CombinedOutput()
				if err == nil {
					t.Fatalf("exited 0 with no docker on PATH:\n%s", out)
				}
				refused, reachedDocker := strings.Contains(string(out), refusal), strings.Contains(string(out), dockerCheck)
				if refused != c.refused || reachedDocker == c.refused {
					t.Errorf("refused=%v, reached the docker check=%v; want %v and %v:\n%s",
						refused, reachedDocker, c.refused, !c.refused, out)
				}
			})
		}
	}
}

// TestPgRestoreVerify_KeepsTheTruncateGuardPredicates pins section 5, the part of
// the restore check that an audit had to correct twice.
//
// 🔴 THE PREDICATES ARE THE FINDING, NOT DECORATION. Each was added because the
// simpler question passed on a database that was NOT safe, and all three were
// measured on a real table inside BEGIN … ROLLBACK:
//
//	"is the trigger row there"     -> passed with tgenabled='D'; TRUNCATE SUCCEEDED
//	"is it not disabled"           -> passed with tgenabled='R'; TRUNCATE SUCCEEDED
//	"is the name right"            -> passed with the same name bound to a no-op
//	                                  function; TRUNCATE SUCCEEDED
//
// A restore is exactly how a database arrives in those states: `pg_restore
// --disable-triggers` issues ALTER TABLE … DISABLE TRIGGER ALL, and a restore
// killed halfway does not put it back. Losing any one of these turns the script's
// PASS into a sentence about nothing.
func TestPgRestoreVerify_KeepsTheTruncateGuardPredicates(t *testing.T) {
	t.Parallel()
	text := readScript(t, "pg-restore-verify.sh")

	for want, why := range map[string]string{
		"(g.tgtype & 32) <> 0": "TRUNCATE is neither UPDATE nor DELETE; without this bit the " +
			"section is reading some other trigger",
		"(g.tgtype & 2)  <> 0": "AFTER TRUNCATE cannot refuse anything — the rows are already gone",
		"g.tgenabled IN ('O', 'A')": "'D' (disabled by pg_restore --disable-triggers) and 'R' " +
			"(replica-only, which does not fire for an ordinary session) were both measured " +
			"letting TRUNCATE succeed while the catalog row was still present",
		"p.proname = 'tappa_forbid_mutation'": "a trigger of the right NAME bound to a no-op " +
			"function was measured passing; the name proves nothing, the function does",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("scripts/pg-restore-verify.sh section 5 no longer contains %q.\n%s", want, why)
		}
	}

	// THE ELEVEN TABLES ARE THE ONES MIGRATIONS 00021, 00026, 00046, 00048 AND 00068 GUARD. A list
	// that shrank would make the script's PASS sentence ("all 11 tables") false while it still
	// printed. ⚠️ strings.Contains cannot tell "audit_log" from "operator_audit_log";
	// TestAppendOnlyTablesAreNamedByBothScripts parses the list exactly and derives it
	// from db/migrations.
	for _, table := range []string{
		"transactions", "audit_log", "transaction_reviews",
		"billing_periods", "policy_versions", "legal_documents",
		"operator_audit_log", "site_punches", "punch_events",
		"punch_claims", "punch_claim_decisions",
	} {
		if !strings.Contains(text, table) {
			t.Errorf("scripts/pg-restore-verify.sh no longer names the append-only table %q; "+
				"its TRUNCATE guard would go unchecked by a restore that lost it", table)
		}
	}

	// AND IT MUST STILL FAIL CLOSED. `fails` is what turns the checks into a verdict;
	// a script that printed PASS unconditionally is the failure mode this whole file
	// guards against, one level up.
	if !strings.Contains(text, `if [ "$fails" -eq 0 ]; then`) {
		t.Error("scripts/pg-restore-verify.sh's final verdict is no longer gated on the failure " +
			"count; an operator reads PASS and puts the database into service")
	}
	if !strings.Contains(text, "do not put this database into service") {
		t.Error("scripts/pg-restore-verify.sh no longer tells the operator what a failure MEANS; " +
			"a check whose failure has no instruction is a check that gets overridden")
	}
}

// TestAppendOnlyTablesAreNamedByBothScripts closes the "a seventh is added, update two
// places" sentence that scripts/redline-check.sh carried: it DERIVES the append-only
// tables from db/migrations -- every table a ROW-level trigger binds to
// tappa_forbid_mutation() -- and requires, in both directions, that
// the set equals pg-restore-verify.sh's `trunc_tables` list and redline-check.sh's
// APPEND_ONLY alternation, and that every one of them also carries a BEFORE TRUNCATE
// guard in some migration.
//
// It exists because the list grew from six to seven with migration 00026
// (operator_audit_log) and the first draft of that change updated neither script:
// pg-restore-verify.sh would have printed "all 6 tables carry a guard" over a restore
// that had lost the seventh, and redline caught grants on the new table only because
// `[^ ]*audit_log` happens to match its name.
//
// The lists are parsed exactly (whitespace-split, `|`-split) rather than searched with
// strings.Contains, which cannot tell audit_log from operator_audit_log.
//
// 🔴 THE DERIVATION READS TRIGGER STATEMENTS, NOT ONE SPELLING OF THEM (round-3 audit).
// Its first version matched `BEFORE UPDATE OR DELETE ... FOR EACH ROW EXECUTE FUNCTION`
// literally; a migration that wrote `BEFORE DELETE OR UPDATE` was invisible to it and an
// eighth append-only table spelled that way left this test green -- measured with a
// temporary eighth migration (kept out of the tree). appendOnlyTriggers now splits each
// migration's Up into statements and classifies every CREATE [OR REPLACE] [CONSTRAINT]
// TRIGGER bound to tappa_forbid_mutation by its parts -- the target after ON, ROW or
// STATEMENT level, and whether TRUNCATE is among the events -- whatever their order,
// case, line breaks or the optional EACH / PROCEDURE / public. / quotes.
// A STATIC CREATE TRIGGER inside a DO block or a function body is seen too (round-4
// audit: the first classifier wanted the statement at the start of its ;-fragment and
// missed it). WHAT IT STILL CANNOT SEE, measured by reading rather than assumed away: a
// trigger created by DYNAMIC SQL (EXECUTE format(...) -- the table name is not in the
// text), a trigger bound to some OTHER function that also refuses mutation, and a table
// made append-only by privileges alone. Each of those is a new mechanism, and the review
// that introduces it has to name it here.
// AND THE OPPOSITE LIMIT, which the unanchored search bought (round-5 audit, measured on a
// copy): it counts statement TEXT that never runs -- a CREATE TRIGGER under `IF false`
// in a DO block, in the body of a function nobody calls, or inside a string literal all
// classify like a real one. On the row side that fails CLOSED (the test demands a table
// be listed that is not append-only). On the TRUNCATE side it fails OPEN: dead text can
// satisfy "this table has a TRUNCATE guard" for a table that has none. The backstop for
// that direction is not this test but the catalogue: scripts/pg-restore-verify.sh
// section 5 reads the guards from pg_trigger, enabled and bound to tappa_forbid_mutation.
// No migration in the tree has such dead text today.
func TestAppendOnlyTablesAreNamedByBothScripts(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(repoRoot, "db", "migrations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read db/migrations: %v", err)
	}
	appendOnly, truncGuarded := map[string]bool{}, map[string]bool{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		up := string(raw)
		if i := strings.Index(strings.ToLower(up), "-- +goose down"); i >= 0 {
			up = up[:i]
		}
		row, trunc := appendOnlyTriggers(stripSQLComments(up))
		for _, table := range row {
			appendOnly[table] = true
		}
		for _, table := range trunc {
			truncGuarded[table] = true
		}
	}
	// ANTI-VACUITY: seven when this was written (00005 x3, 00007, 00016, 00020, 00026).
	if len(appendOnly) < 7 {
		t.Fatalf("only %d append-only tables derived from db/migrations (%v); the derivation has gone blind", len(appendOnly), appendOnly)
	}

	restore := readScript(t, "pg-restore-verify.sh")
	m := regexp.MustCompile(`(?m)^trunc_tables="([^"]*)"`).FindStringSubmatch(restore)
	if m == nil {
		t.Fatal("scripts/pg-restore-verify.sh has no trunc_tables=\"...\" line")
	}
	inRestore := map[string]bool{}
	for _, name := range strings.Fields(m[1]) {
		inRestore[name] = true
	}
	redline := readScript(t, "redline-check.sh")
	m = regexp.MustCompile(`(?m)^APPEND_ONLY='\(([^)]*)\)'`).FindStringSubmatch(redline)
	if m == nil {
		t.Fatal("scripts/redline-check.sh has no APPEND_ONLY='(...)' line")
	}
	inRedline := map[string]bool{}
	for _, name := range strings.Split(m[1], "|") {
		inRedline[name] = true
	}

	for table := range appendOnly {
		if !truncGuarded[table] {
			t.Errorf("%s is append-only (row trigger) but no migration gives it a BEFORE TRUNCATE guard (00021's lesson: TRUNCATE is neither UPDATE nor DELETE)", table)
		}
		if !inRestore[table] {
			t.Errorf("%s is append-only but scripts/pg-restore-verify.sh's trunc_tables does not name it: a restore that lost its TRUNCATE guard would pass", table)
		}
		if !inRedline[table] {
			t.Errorf("%s is append-only but scripts/redline-check.sh's APPEND_ONLY does not name it", table)
		}
	}
	for name := range inRestore {
		if !appendOnly[name] {
			t.Errorf("scripts/pg-restore-verify.sh names %q, which no migration makes append-only", name)
		}
	}
	for name := range inRedline {
		if !appendOnly[name] {
			t.Errorf("scripts/redline-check.sh's APPEND_ONLY names %q, which no migration makes append-only", name)
		}
	}
}

// appendOnlyTriggers returns, from one migration's comment-stripped Up section, the
// tables that carry a ROW-level trigger bound to tappa_forbid_mutation() (the
// append-only marker, whatever its events) and the tables that carry a BEFORE TRUNCATE
// statement-level trigger bound to it (00021's guard). It classifies each CREATE
// TRIGGER statement by its parts, so the event order and the optional words do not
// matter; see the limits in TestAppendOnlyTablesAreNamedByBothScripts' comment.
func appendOnlyTriggers(up string) (row, truncGuard []string) {
	// NOT anchored to the start of the fragment (round-4 audit): a static CREATE TRIGGER
	// inside a DO block or a function body ends up in the SAME ;-fragment as the
	// `DO $$ BEGIN` that precedes it, and an anchored pattern missed it (measured:
	// row="" trunc=""). The statement is read from wherever it starts -- which also
	// counts text that never executes; see the limit in
	// TestAppendOnlyTablesAreNamedByBothScripts' comment.
	createRE := regexp.MustCompile(`(?is)\bcreate\s+(?:or\s+replace\s+)?(?:constraint\s+)?trigger\s`)
	fnRE := regexp.MustCompile(`(?is)\sexecute\s+(?:function|procedure)\s+(?:"?public"?\s*\.\s*)?"?tappa_forbid_mutation"?\s*\(`)
	onRE := regexp.MustCompile(`(?is)\son\s+(?:only\s+)?(?:"?public"?\s*\.\s*)?"?([a-z_][a-z0-9_]*)"?`)
	rowRE := regexp.MustCompile(`(?is)\sfor\s+(?:each\s+)?row\s`)
	stmtRE := regexp.MustCompile(`(?is)\sfor\s+(?:each\s+)?statement\s`)
	truncRE := regexp.MustCompile(`(?is)\bbefore\b[^;]*?\btruncate\b[^;]*?\son\s`)
	for _, frag := range strings.Split(up, ";") {
		at := createRE.FindStringIndex(frag)
		if at == nil {
			continue
		}
		stmt := " " + frag[at[0]:]
		if !fnRE.MatchString(stmt) {
			continue
		}
		m := onRE.FindStringSubmatch(stmt)
		if m == nil {
			continue
		}
		table := strings.ToLower(m[1])
		switch {
		case rowRE.MatchString(stmt):
			row = append(row, table)
		case stmtRE.MatchString(stmt) && truncRE.MatchString(stmt):
			truncGuard = append(truncGuard, table)
		}
	}
	return row, truncGuard
}

// TestAppendOnlyTriggers_EverySpellingIsSeen pins the classifier on the spellings the
// first derivation missed and on the ones it must keep rejecting. A table appears in a
// result only through the rule it is named for; the negative cases are what keep a
// match-everything classifier from passing.
func TestAppendOnlyTriggers_EverySpellingIsSeen(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, sql  string
		row, trunc string
	}{
		{"canonical", "CREATE TRIGGER a BEFORE UPDATE OR DELETE ON t1 FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation()", "t1", ""},
		{"events reversed", "CREATE TRIGGER a BEFORE DELETE OR UPDATE ON t2 FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation()", "t2", ""},
		{"one event, lower case, line breaks", "create trigger a\n before delete\n on public.t3\n for row\n execute procedure public.tappa_forbid_mutation()", "t3", ""},
		{"update of a column, quoted names", `CREATE OR REPLACE TRIGGER "a" BEFORE UPDATE OF x OR DELETE ON "public"."t4" FOR EACH ROW EXECUTE FUNCTION "tappa_forbid_mutation"()`, "t4", ""},
		{"truncate guard", "CREATE TRIGGER a BEFORE TRUNCATE ON t5 FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation()", "", "t5"},
		{"another function", "CREATE TRIGGER a BEFORE UPDATE OR DELETE ON t6 FOR EACH ROW EXECUTE FUNCTION something_else()", "", ""},
		{"a statement trigger that is not TRUNCATE", "CREATE TRIGGER a BEFORE UPDATE ON t7 FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation()", "", ""},
		{"an AFTER truncate", "CREATE TRIGGER a AFTER TRUNCATE ON t8 FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation()", "", ""},
		{"not a CREATE TRIGGER", "DROP TRIGGER a ON t9", "", ""},
		{"static, inside a DO block", "DO $$ BEGIN CREATE TRIGGER a BEFORE UPDATE OR DELETE ON t10 FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation(); END $$", "t10", ""},
		{"static, inside a DO block, truncate guard", "DO $$ BEGIN\n  CREATE TRIGGER a BEFORE TRUNCATE ON t11 FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation(); END $$", "", "t11"},
	} {
		row, trunc := appendOnlyTriggers(c.sql + ";")
		if got := strings.Join(row, ","); got != c.row {
			t.Errorf("%s: row-level = %q, want %q", c.name, got, c.row)
		}
		if got := strings.Join(trunc, ","); got != c.trunc {
			t.Errorf("%s: truncate guard = %q, want %q", c.name, got, c.trunc)
		}
	}
}

// TestComposeFileDeclaresTheProjectName is the PREMISE of db-reset.sh's pin, and
// nothing else in the tree held it (backlog T55).
//
// scripts/db-reset.sh pins `-p tappa` and its header justifies the value by saying
// docker-compose.yml declares it, "so the pin here and what `make up` builds cannot
// drift apart when somebody renames the checkout". Delete `name: tappa` from the
// compose file and that justification becomes false silently: compose falls back to
// the DIRECTORY NAME, `make up` builds a differently-named project, and db-reset
// then removes a volume belonging to nothing.
func TestComposeFileDeclaresTheProjectName(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join(repoRoot, "docker-compose.yml"))
	if err != nil {
		t.Fatalf("read docker-compose.yml: %v", err)
	}
	if !strings.Contains(string(b), "\nname: tappa\n") {
		t.Error("docker-compose.yml no longer declares `name: tappa` at the top level. " +
			"scripts/db-reset.sh pins `-p tappa` and cites that declaration as the reason the " +
			"pin is not a guess; without it compose uses the directory name and the two " +
			"disagree the moment somebody renames the checkout.")
	}
}

// TestBackupAndRestoreVerify_ShareTheInventory holds T45's fix: the backup records the
// source's function and role inventory in the manifest, and the restore check compares
// the restored catalog with it. Measured before the fix, on a restored goose-34 schema:
// eight states that break the operator's SECURITY DEFINER boundary (op_touch_session
// owned by the superuser, search_path reset, EXECUTE to PUBLIC, EXECUTE to tappa_app,
// SECURITY DEFINER dropped, tappa_operator BYPASSRLS, tappa_opdefiner SUPERUSER,
// tappa_opdefiner a member of tappa_owner) — eight exit-0 PASSes. After it, eight
// exit-1s with the differing line named, and PASS again once each was undone.
//
// 🔴 THE TWO QUERIES MUST BE THE SAME BYTES. EXPECTED is produced by one script on one
// day and ACTUAL by the other on another; a predicate edited in only one of them makes
// every restore fail (false RED) or, worse, makes both sides blind to the same column
// in different ways. So the block is compared verbatim, not "both contain X".
func TestBackupAndRestoreVerify_ShareTheInventory(t *testing.T) {
	t.Parallel()
	const open, closing = "# >>> T45 INVENTORY", "# <<< T45 INVENTORY <<<"
	block := func(name string) string {
		text := readScript(t, name)
		if n := strings.Count(text, open); n != 1 {
			t.Fatalf("scripts/%s carries %d inventory blocks (%q), want exactly 1", name, n, open)
		}
		start := strings.Index(text, open)
		end := strings.Index(text[start:], closing)
		if end < 0 {
			t.Fatalf("scripts/%s opens the inventory block but never closes it with %q", name, closing)
		}
		return text[start : start+end]
	}
	backup, restore := block("pg-backup.sh"), block("pg-restore-verify.sh")
	if backup != restore {
		t.Errorf("the inventory block differs between scripts/pg-backup.sh and scripts/pg-restore-verify.sh. " +
			"The manifest's `function`/`role` lines are compared VERBATIM with the restore's own query, " +
			"so the two must be one text; edit both in the same change.")
	}

	// What makes the comparison mean something: the properties the eight measured states break.
	for want, why := range map[string]string{
		"pg_get_userbyid(p.proowner)": "a SECURITY DEFINER function runs as its owner; owned by the superuser it runs every caller as superuser",
		"p.prosecdef":                 "dropping SECURITY DEFINER changes which role's rights a function uses",
		"p.proconfig":                 "the pinned search_path = pg_catalog, pg_temp lives here",
		"acldefault('f', p.proowner)": "a NULL proacl means EXECUTE to PUBLIC; spelled out, it is compared rather than skipped",
		"o.rolsuper, o.rolbypassrls":  "a role behind the boundary that bypasses RLS is a boundary the source did not have",
		"pg_auth_members m JOIN pg_roles g ON g.oid = m.member": "a MEMBER of tappa_opdefiner runs with its BYPASSRLS — 00026 refuses that at migration time and a restore runs no migration",
		"d.deptype = 'e'": "extension functions (pgcrypto, citext) are the extension's, not this schema's",
		// The escaped spelling IS the pin: the SQL sits in a double-quoted shell string, and
		// the bare `COLLATE "C"` was measured reaching psql as COLLATE c — an error that
		// stopped the backup.
		`ORDER BY a::text COLLATE \"C\"`: "the source and the restore target may have different default collations; " +
			"a locale-ordered ACL list turns a correct restore red",
	} {
		if !strings.Contains(backup, want) {
			t.Errorf("the inventory block no longer contains %q: %s", want, why)
		}
	}

	bk := codeOf(readScript(t, "pg-backup.sh"))
	for _, want := range []string{
		`$PSQL -c "$INV_FUNCTIONS_SQL" > "$STAGE/.functions.$$"`,
		`$PSQL -c "$INV_ROLES_SQL" > "$STAGE/.roles.$$"`,
		`cat "$STAGE/.functions.$$" "$STAGE/.roles.$$"`,
		// 4f: a --no-owner dump restores every op_* function owned by the restoring superuser.
		`[ "$dump_fowner" -ge "$LIVE_FUNCS" ]`,
	} {
		if !strings.Contains(bk, want) {
			t.Errorf("scripts/pg-backup.sh no longer contains %q; the manifest would stop carrying the inventory or the owner check would be gone", want)
		}
	}
	rv := codeOf(readScript(t, "pg-restore-verify.sh"))
	for _, want := range []string{
		`inventory "functions" "function" "$INV_FUNCTIONS_SQL"`,
		`inventory "roles" "role" "$INV_ROLES_SQL"`,
		// A manifest without the lines must FAIL, not pass silently: "nothing to compare"
		// read as "nothing differs" is the blind PASS this test exists to prevent.
		`bad "the manifest carries no '$_p' lines`,
	} {
		if !strings.Contains(rv, want) {
			t.Errorf("scripts/pg-restore-verify.sh no longer contains %q", want)
		}
	}
}

// TestPgBackup_ManifestCarriesTheDumpsFeedCounters is the API-2 security review's D1 on the backup
// side: scripts/pg-backup.sh's FEED COUNTERS block -- run here as the script runs it, with sh, on
// a plain-format dump excerpt -- writes one `feed_counter <tenant> <last_seq>` line per row of the
// dump's COPY public.feed_counters section and nothing for any other table (a punch_events row, a
// COPY of another table whose name starts the same), and the manifest carries the file it wrote.
// scripts/pg-restore-verify.sh section 6 compares every restored counter to those values
// (internal/db's TestRestoreVerify_CountersAheadOfEvents runs its SQL).
func TestPgBackup_ManifestCarriesTheDumpsFeedCounters(t *testing.T) {
	src := readScript(t, "pg-backup.sh")
	const begin, end = "# >>> FEED COUNTERS >>>", "# <<< FEED COUNTERS <<<"
	i, j := strings.Index(src, begin), strings.Index(src, end)
	if i < 0 || j < i {
		t.Fatal("scripts/pg-backup.sh carries no FEED COUNTERS block")
	}
	for _, want := range []string{`cat "$STAGE/.feedcounters.$$"`, `"$STAGE/.feedcounters.$$"`} {
		if !strings.Contains(src[j:], want) {
			t.Errorf("scripts/pg-backup.sh's manifest does not carry %s", want)
		}
	}
	dir := t.TempDir()
	dump := "COPY public.feed_counters_archive (tenant_id, last_seq) FROM stdin;\n" +
		"99999999-9999-4999-8999-999999999999\t7\n\\.\n" +
		"COPY public.feed_counters (tenant_id, last_seq) FROM stdin;\n" +
		"aaaaaaaa-0000-4000-8000-000000000001\t12\n" +
		"bbbbbbbb-0000-4000-8000-000000000002\t0\n\\.\n" +
		"COPY public.punch_events (tenant_id, seq) FROM stdin;\n" +
		"aaaaaaaa-0000-4000-8000-000000000001\t12\n\\.\n"
	if err := os.WriteFile(filepath.Join(dir, "d.sql"), []byte(dump), 0o600); err != nil {
		t.Fatal(err)
	}
	script := "set -eu\nSQL=" + filepath.Join(dir, "d.sql") + "\nSTAGE=" + dir + "\n" + src[i:j+len(end)] + "\ncat \"$STAGE/.feedcounters.$$\"\n"
	out, err := exec.Command("sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("the FEED COUNTERS block: %v\n%s", err, out)
	}
	want := "feed_counter aaaaaaaa-0000-4000-8000-000000000001 12\nfeed_counter bbbbbbbb-0000-4000-8000-000000000002 0\nfeed_counters 2\n"
	if string(out) != want {
		t.Errorf("the block wrote\n%s\nwant\n%s", out, want)
	}
	// A database with no tenant yet: the section is there and empty -- the header says 0 (the
	// API-2 review: without it the manifest looked like an old pg-backup.sh's). A dump from before
	// 00048: no section, no header.
	for _, c := range []struct{ what, dump, want string }{
		{"an empty feed_counters", "COPY public.feed_counters (tenant_id, last_seq) FROM stdin;\n\\.\n", "feed_counters 0\n"},
		{"no feed_counters", "COPY public.tenants (id) FROM stdin;\n\\.\n", ""},
	} {
		if err := os.WriteFile(filepath.Join(dir, "d.sql"), []byte(c.dump), 0o600); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command("sh", "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("the FEED COUNTERS block on %s: %v\n%s", c.what, err, out)
		}
		if string(out) != c.want {
			t.Errorf("%s: the block wrote %q, want %q", c.what, out, c.want)
		}
	}
}

// TestRestoreVerify_FeedManifestGate runs scripts/pg-restore-verify.sh section 6's shell -- as the
// script runs it, with sh -- with the database stubbed (feed present; FEED_COUNTERS_SQL answers
// "0 0 0 0 0", a database with no tenant) against the manifests it must tell apart:
//   - the header `feed_counters 0` and no line: PASS (the API-2 review: this restore was refused
//     forever, a new backup included);
//   - no header (a pg-backup.sh older than M11 API-2): FAIL, naming deploy/README.md's manual way;
//   - a header that does not count its lines, or two headers: FAIL, nothing compared;
//   - the header and its lines: the SQL runs and its answer decides.
func TestRestoreVerify_FeedManifestGate(t *testing.T) {
	src := readScript(t, "pg-restore-verify.sh")
	i := strings.Index(src, "FEED_RESTORE_GAP=1000000")
	j := strings.Index(src, "\necho\nif [ \"$fails\" -eq 0 ]; then")
	if i < 0 || j < i {
		t.Fatal("scripts/pg-restore-verify.sh section 6 is not where this test cuts it")
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "psql")
	// -c: the feed-present question; -f: the counters' SQL, whose file must hold one INSERT per line.
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncase \"$1\" in\n-c) echo t ;;\n-f) echo \"inserts $(grep -c '^INSERT INTO zz_feed_dumped' \"$2\")\" >&2; echo '0 0 0 0 0' ;;\nesac\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	const tenant = "aaaaaaaa-0000-4000-8000-000000000001"
	for _, c := range []struct {
		what, manifest string
		pass           bool
		say            string
	}{
		{"the header 0 and no line", "table x 1 1\nfeed_counters 0\n", true, "inserts 0"},
		{"no header", "table x 1 1\n", false, "Eski manifest"},
		{"no header, lines", "feed_counter " + tenant + " 3\n", false, "no 'feed_counters' header"},
		{"a header that does not count its lines", "feed_counter " + tenant + " 3\nfeed_counters 2\n", false, "says 2 counter lines and it carries 1"},
		{"two headers", "feed_counters 0\nfeed_counters 0\n", false, "not compared"},
		{"a header that is not a count", "feed_counters x\n", false, "not compared"},
		{"the header and its line", "feed_counter " + tenant + " 3\nfeed_counters 1\n", true, "inserts 1"},
	} {
		manifest := filepath.Join(dir, "manifest")
		if err := os.WriteFile(manifest, []byte(c.manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		script := "set -eu\nfails=0\nbad()  { echo \"  FAIL  $*\" >&2; fails=$((fails + 1)); }\nok()   { echo \"  ok    $*\"; }\n" +
			"note() { echo \"  --    $*\"; }\ndie()  { echo \"die: $*\" >&2; exit 2; }\n" +
			"MANIFEST=" + manifest + "\nTMP=" + dir + "\nOWNER_PSQL=" + stub + "\nPGDATABASE=x\n" + src[i:j] + "\necho \"fails=$fails\"\n"
		out, err := exec.Command("sh", "-c", script).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: section 6: %v\n%s", c.what, err, out)
		}
		passed := strings.Contains(string(out), "fails=0")
		if passed != c.pass || !strings.Contains(string(out), c.say) {
			t.Errorf("%s: pass %v, want %v, saying %q:\n%s", c.what, passed, c.pass, c.say, out)
		}
	}
}
