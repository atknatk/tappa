package main

// deploytrigger_test.go — backlog T140: .github/workflows/deploy.yml runs only for a green ci
// run that a PUSH to THIS repository's main started, or for a manual run from main.
//
// The hole it closes: `on: workflow_run: branches: [main]` filters on the triggering run's
// head_branch, a name the author of that run chooses. A pull request from a fork's own `main`
// runs ci (`on: pull_request`, under the PR's own ci.yml, still `name: ci`) with head_branch
// "main"; a green one used to start this workflow, which checks out head_sha — the fork's code
// — and builds it holding the repository's secrets. The job's `if:` now also requires
// event == push and head_repository == this repository, and the first step re-checks all of it
// in bash.
//
// THREAT MODEL, as in operatorsurface_test.go: these pins are against ACCIDENTAL drift — a
// condition dropped, a trigger added, the gate moved or made skippable, a `${{ }}` rendered
// into a script. The gate's body is RUN here against stub inputs; nothing contacts GitHub.

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const triggerGateStep = "Gate — this run was triggered by a push to this repository's main"

// deployJobIf is the deploy job's `if:`, whitespace-normalised. Every clause is argued at the
// `if:` in deploy.yml; any change to it — a clause dropped, `==` turned into `!=`, an `||`
// widened — is red here and must be argued again.
const deployJobIf = "(github.event_name == 'workflow_dispatch' && github.ref == 'refs/heads/main') || " +
	"(github.event_name == 'workflow_run' && " +
	"github.event.workflow_run.conclusion == 'success' && " +
	"github.event.workflow_run.event == 'push' && " +
	"github.event.workflow_run.head_repository.full_name == github.repository && " +
	"github.event.workflow_run.head_branch == 'main')"

// triggerGateEnv is the gate step's env: block. The bash body is run below against these
// names, so each name must map to the expression the job's `if:` reads.
var triggerGateEnv = []string{
	"EVENT_NAME: ${{ github.event_name }}",
	"REF: ${{ github.ref }}",
	"REPOSITORY: ${{ github.repository }}",
	"RUN_CONCLUSION: ${{ github.event.workflow_run.conclusion }}",
	"RUN_EVENT: ${{ github.event.workflow_run.event }}",
	"RUN_HEAD_REPOSITORY: ${{ github.event.workflow_run.head_repository.full_name }}",
	"RUN_HEAD_BRANCH: ${{ github.event.workflow_run.head_branch }}",
}

func deployWorkflowCode(t *testing.T) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "deploy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return codeLinesOf(string(b))
}

// childrenOf returns the non-blank lines strictly more indented than lines[i], up to the
// first line that dedents back to lines[i]'s indent or past it.
func childrenOf(lines []string, i int) []string {
	var out []string
	in := indentOf(lines[i])
	for _, l := range lines[i+1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if indentOf(l) <= in {
			break
		}
		out = append(out, l)
	}
	return out
}

// runBodies returns, per step, the text of its `run:` — the inline value, or every line of
// the block scalar under it.
func runBodies(steps []workflowStep) map[string][]string {
	out := map[string][]string{}
	for _, s := range steps {
		for i, l := range s.code {
			v, ok := strings.CutPrefix(strings.TrimSpace(l), "run:")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			if v != "" && !strings.HasPrefix(v, "|") && !strings.HasPrefix(v, ">") {
				out[s.name] = append(out[s.name], v)
				continue
			}
			out[s.name] = append(out[s.name], childrenOf(s.code, i)...)
		}
	}
	return out
}

// TestPackaging_TheDeployRunsOnlyForAPushToThisRepositorysMain pins T140's configuration:
//
//   - `on:` holds exactly workflow_run (workflows ["ci"], types [completed], branches [main],
//     nothing else) and a bare workflow_dispatch — no pull_request, pull_request_target, push,
//     schedule or any other trigger, and no dispatch inputs;
//   - the job's `if:` is deployJobIf;
//   - the first step is triggerGateStep, before actions/checkout and every other step, with
//     exactly triggerGateEnv and no `if:` or `continue-on-error`;
//   - no `run:` body anywhere in the file contains `${{` — every value reaches a script
//     through env: (CONTROL: run bodies are seen, and the gate's env does carry `${{`).
//
// MUTANT (measured, T140): each of the five `if:` clauses removed in turn -> red;
// `pull_request_target:` added under `on:` -> red; conclusion `== 'success'` replaced by
// `!= 'cancelled'` -> red; the gate moved after "Checkout that exact commit" -> red;
// `${{ github.event.workflow_run.head_sha }}` put back into "Resolve the commit to deploy"'s
// body -> red; `continue-on-error: true` on the gate -> red; the gate's RUN_EVENT mapped to
// github.event_name -> red. The gate body's own mutants are the next test's.
func TestPackaging_TheDeployRunsOnlyForAPushToThisRepositorysMain(t *testing.T) {
	code := deployWorkflowCode(t)

	// on:
	on := slices.IndexFunc(code, func(l string) bool { return l == "on:" })
	if on < 0 {
		t.Fatal("CONTROL FAILED: deploy.yml has no top-level `on:`")
	}
	var triggers []string
	for _, l := range childrenOf(code, on) {
		if indentOf(l) == 2 {
			triggers = append(triggers, strings.TrimSpace(l))
		}
	}
	if !slices.Equal(triggers, []string{"workflow_run:", "workflow_dispatch:"}) {
		t.Errorf("deploy.yml's triggers are %q, want exactly [workflow_run: workflow_dispatch:]: any other trigger "+
			"(pull_request, pull_request_target, push, schedule…) runs this file's secrets on input main did not choose",
			triggers)
	}
	for i := on + 1; i < len(code) && (strings.TrimSpace(code[i]) == "" || indentOf(code[i]) > 0); i++ {
		var got []string
		for _, l := range childrenOf(code, i) {
			got = append(got, strings.TrimSpace(l))
		}
		switch strings.TrimSpace(code[i]) {
		case "workflow_run:":
			slices.Sort(got)
			want := []string{"branches: [main]", "types: [completed]", `workflows: ["ci"]`}
			if !slices.Equal(got, want) {
				t.Errorf("workflow_run carries %q, want exactly %q", got, want)
			}
		case "workflow_dispatch:":
			if len(got) != 0 {
				t.Errorf("workflow_dispatch carries %q, want nothing: an input could choose what is deployed", got)
			}
		}
	}

	// The job's if:
	jobIf := -1
	for i, l := range code {
		if indentOf(l) == 4 && strings.HasPrefix(strings.TrimSpace(l), "if:") {
			if jobIf >= 0 {
				t.Errorf("deploy.yml has a second job-level `if:` at line %d", i+1)
			}
			jobIf = i
		}
	}
	if jobIf < 0 {
		t.Fatal("deploy.yml's deploy job has no `if:`: every finished ci run, on any branch, from any fork, would deploy")
	}
	expr := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(code[jobIf]), "if:"))
	if expr == ">-" || expr == ">" {
		var parts []string
		for _, l := range childrenOf(code, jobIf) {
			parts = append(parts, strings.TrimSpace(l))
		}
		expr = strings.Join(parts, " ")
	}
	expr = strings.Join(strings.Fields(expr), " ")
	if expr != deployJobIf {
		t.Errorf("the deploy job's `if:` is\n  %s\nwant\n  %s\na green ci run from a pull request (a fork's `main` "+
			"included) or a manual run from another branch must not deploy", expr, deployJobIf)
	}

	// The gate: first, before checkout, unskippable, with exactly its env.
	steps := deploySteps(t)
	if steps[0].name != triggerGateStep {
		t.Errorf("the first step is %q, want %q: nothing may run before the trigger is re-checked", steps[0].name,
			triggerGateStep)
	}
	gate := slices.IndexFunc(steps, func(s workflowStep) bool { return s.name == triggerGateStep })
	checkout := slices.IndexFunc(steps, func(s workflowStep) bool {
		return slices.ContainsFunc(s.code, func(l string) bool {
			return strings.HasPrefix(strings.TrimSpace(l), "uses: actions/checkout@")
		})
	})
	if gate < 0 || checkout < 0 {
		t.Fatalf("CONTROL FAILED: gate step %d, checkout step %d", gate, checkout)
	}
	if gate > checkout {
		t.Errorf("the trigger gate is step %d, after the checkout (step %d): the run would hold the untrusted tree first",
			gate, checkout)
	}
	var env []string
	for i, l := range steps[gate].code {
		tl := strings.TrimSpace(l)
		if strings.HasPrefix(tl, "if:") || strings.HasPrefix(tl, "continue-on-error:") {
			t.Errorf("the trigger gate carries %q: a gate that can be skipped is not one", tl)
		}
		if tl == "env:" {
			for _, c := range childrenOf(steps[gate].code, i) {
				env = append(env, strings.TrimSpace(c))
			}
		}
	}
	if !slices.Equal(env, triggerGateEnv) {
		t.Errorf("the trigger gate's env is %q, want exactly %q", env, triggerGateEnv)
	}

	// No ${{ }} in a run body.
	bodies := runBodies(steps)
	if len(bodies) < 20 || len(bodies[triggerGateStep]) < 5 {
		t.Fatalf("CONTROL FAILED: %d run bodies seen, the gate's has %d lines; the body scan has gone blind",
			len(bodies), len(bodies[triggerGateStep]))
	}
	if !slices.ContainsFunc(env, func(l string) bool { return strings.Contains(l, "${{ github.event.") }) {
		t.Error("CONTROL FAILED: the gate's env carries no `${{ github.event.`; the scan below would prove nothing")
	}
	for name, body := range bodies {
		for _, l := range body {
			if strings.Contains(l, "${{") {
				t.Errorf("step %q renders %q into its script: pass the value through env: (head_branch, a PR title "+
					"and the like are text the run's author wrote)", name, strings.TrimSpace(l))
			}
		}
	}
}

// TestDeployTrigger_TheGateAcceptsOnlyAPushToMainOrAManualRunFromMain runs the gate step's bash
// body, extracted from deploy.yml, with the env names triggerGateEnv pins. Accepted: a manual
// run from main; a green push run from this repository's main. Everything else exits 1, prints
// no input value, and runs none of it (an input carrying `$(…)` creates no file).
//
// MUTANT (measured, T140): the RUN_EVENT check, the REF check or the repository check deleted
// from the body, or the `*)` arm made to pass -> red.
func TestDeployTrigger_TheGateAcceptsOnlyAPushToMainOrAManualRunFromMain(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("no bash")
	}
	steps := deploySteps(t)
	body := runBodies(steps)[triggerGateStep]
	if len(body) < 5 {
		t.Fatalf("CONTROL FAILED: the gate's run body has %d lines", len(body))
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "gate.sh")
	in := indentOf(body[0])
	var src strings.Builder
	for _, l := range body {
		if len(l) >= in {
			l = l[in:]
		}
		src.WriteString(l + "\n")
	}
	if err := os.WriteFile(script, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	const repo = "atknatk/tappa"
	pushRun := map[string]string{
		"EVENT_NAME": "workflow_run", "REPOSITORY": repo, "REF": "refs/heads/main",
		"RUN_CONCLUSION": "success", "RUN_EVENT": "push", "RUN_HEAD_REPOSITORY": repo, "RUN_HEAD_BRANCH": "main",
	}
	with := func(base map[string]string, kv ...string) map[string]string {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for i := 0; i+1 < len(kv); i += 2 {
			if kv[i+1] == "<unset>" {
				delete(m, kv[i])
			} else {
				m[kv[i]] = kv[i+1]
			}
		}
		return m
	}
	dispatch := map[string]string{"EVENT_NAME": "workflow_dispatch", "REPOSITORY": repo, "REF": "refs/heads/main"}
	marker := filepath.Join(dir, "ran")

	tests := []struct {
		name string
		env  map[string]string
		ok   bool
	}{
		{"a green push run from this repository's main", pushRun, true},
		{"a manual run from main", dispatch, true},
		{"a manual run from a feature branch", with(dispatch, "REF", "refs/heads/feature"), false},
		{"a manual run from a tag named main", with(dispatch, "REF", "refs/tags/main"), false},
		{"a manual run with no ref", with(dispatch, "REF", "<unset>"), false},
		{"a failed ci run", with(pushRun, "RUN_CONCLUSION", "failure"), false},
		{"a cancelled ci run", with(pushRun, "RUN_CONCLUSION", "cancelled"), false},
		{"a ci run with no conclusion", with(pushRun, "RUN_CONCLUSION", ""), false},
		{"a pull_request run from a fork's main", with(pushRun, "RUN_EVENT", "pull_request",
			"RUN_HEAD_REPOSITORY", "someone/tappa"), false},
		{"a pull_request run from this repository's main", with(pushRun, "RUN_EVENT", "pull_request"), false},
		{"a pull_request_target run", with(pushRun, "RUN_EVENT", "pull_request_target"), false},
		{"a push run from a fork", with(pushRun, "RUN_HEAD_REPOSITORY", "someone/tappa"), false},
		{"a push run whose repository is not known", with(pushRun, "RUN_HEAD_REPOSITORY", "", "REPOSITORY", ""), false},
		{"a push run from another branch", with(pushRun, "RUN_HEAD_BRANCH", "feature"), false},
		{"a push run with no branch", with(pushRun, "RUN_HEAD_BRANCH", "<unset>"), false},
		{"a branch that only starts with main", with(pushRun, "RUN_HEAD_BRANCH", "main-x"), false},
		{"a branch carrying a command", with(pushRun, "RUN_HEAD_BRANCH", "main$(touch "+marker+")"), false},
		{"a repository carrying a command", with(pushRun, "RUN_HEAD_REPOSITORY", repo+"`touch "+marker+"`"), false},
		{"a push event", with(pushRun, "EVENT_NAME", "push"), false},
		{"a pull_request_target event", with(pushRun, "EVENT_NAME", "pull_request_target"), false},
		{"no event", with(pushRun, "EVENT_NAME", "<unset>"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command("bash", script)
			cmd.Env = []string{"PATH=" + os.Getenv("PATH")}
			for k, v := range tt.env {
				cmd.Env = append(cmd.Env, k+"="+v)
			}
			out, err := cmd.CombinedOutput()
			if tt.ok && err != nil {
				t.Fatalf("refused (%v), want accepted: %s", err, out)
			}
			if !tt.ok {
				ee, isExit := err.(*exec.ExitError)
				if !isExit || ee.ExitCode() != 1 {
					t.Fatalf("got %v, want exit 1: %s", err, out)
				}
				if !strings.Contains(string(out), "::error::not deploying: ") {
					t.Errorf("the refusal does not say why: %s", out)
				}
				for _, k := range []string{"RUN_HEAD_BRANCH", "RUN_HEAD_REPOSITORY", "REF"} {
					if v := tt.env[k]; v != "" && v != "main" && v != repo && strings.Contains(string(out), v) {
						t.Errorf("the refusal prints %s's value %q", k, v)
					}
				}
			}
			if _, err := os.Stat(marker); err == nil {
				t.Fatal("an input value was executed")
			}
		})
	}
}
