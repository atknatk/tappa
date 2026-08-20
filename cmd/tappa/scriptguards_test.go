package main

// scriptguards_test.go — the gate behind the two SHELL SCRIPTS that carry
// operational risk and had nothing holding them (backlog T55, closed M8-04 FAZ B3).
//
// 🔴 WHY THIS FILE EXISTS, MEASURED RATHER THAN ARGUED. Before it, the repository
// contained exactly one test that executed a shell script — cmd/rotatekek/
// script_test.go, and it targets rotate-kek.sh alone. The other two were unread by
// anything:
//
//	rg -n 'db-reset.sh|pg-restore-verify' --glob '*_test.go'   -> zero
//	make check = fmt gen lint test                              -> reads no .sh
//	CI                                                          -> runs neither
//	scripts/redline-check.sh                                    -> exempts one BY NAME
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
	"os"
	"os/exec"
	"path/filepath"
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
	text := readScript(t, "db-reset.sh")
	code := codeOf(text)

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
			t.Errorf("scripts/db-reset.sh no longer contains %q. That pin is the only thing "+
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
		t.Errorf("scripts/db-reset.sh:%d calls compose without going through ${COMPOSE[@]}:\n  %s\n"+
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
			t.Errorf("scripts/db-reset.sh no longer contains %q — the locality probe's chain is "+
				"broken, or the destructive statement it guards has moved and this test is now "+
				"guarding nothing", want)
		}
	}

	// 🔴 ORDER MATTERS AND IS ASSERTED. A probe that ran AFTER `down --volumes`
	// would be a post-mortem. This is the one property a literal check can hold
	// about sequencing, and it is cheap.
	if strings.Index(code, "cmp -s scripts/db-init/01-roles.sql") > strings.Index(code, "down --volumes") {
		t.Error("scripts/db-reset.sh runs its locality probe AFTER the volume is removed; " +
			"a guard that fires after the deletion is not a guard")
	}

	// THE OPERATOR INSTRUCTION IS BUILT FROM THE PIN, NOT RETYPED (backlog T56 item
	// 2). The file's own comment above `project=` says the name is held in ONE
	// variable "and printed from the SAME one" — and one line 80 rows further down
	// hand-wrote `docker compose -p tappa logs db`, missing -f and repeating the
	// project name, which is exactly the fault that paragraph diagnoses.
	if strings.Contains(code, "docker compose -p tappa logs") {
		t.Error("scripts/db-reset.sh tells the operator to run a HAND-WRITTEN compose command " +
			"(`docker compose -p tappa logs`). It repeats the project name the file holds in " +
			"$project and omits -f, so it points at whatever COMPOSE_FILE says and stops " +
			"matching the moment $project changes — the file's own diagnosis, one screen down.")
	}
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
			cmd.Env = append(os.Environ(), "DOCKER_HOST="+tc.host)
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

	// THE SIX TABLES ARE THE ONES MIGRATION 00021 GUARDS. A list that shrank would
	// make the script's PASS sentence ("all 6 tables") false while it still printed.
	for _, table := range []string{
		"transactions", "audit_log", "transaction_reviews",
		"billing_periods", "policy_versions", "legal_documents",
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
