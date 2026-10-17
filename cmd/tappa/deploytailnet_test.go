package main

// deploytailnet_test.go — backlog T134: the cluster API (144.76.158.60:6443) was closed to
// the internet on purpose (2026-10-17) and is reached only through the tailnet, at
// 10.43.0.1:443. .github/workflows/deploy.yml therefore joins the tailnet with a
// SHA-pinned tailscale/github-action, measures what it joined as and whether the API
// answers, and only then makes its first call against the cluster.
//
// THREAT MODEL, as in operatorsurface_test.go: these pins are against ACCIDENTAL drift —
// the join deleted or moved after the first cluster call, the pin loosened to a tag, the
// gate made skippable, a secret spelled into a `run:` body. Deliberate circumvention is code
// review's subject. Nothing here contacts Tailscale or the cluster; the gate's shell was run
// against stub `tailscale`/`timeout` binaries and a local listener when it was written (T134
// builder report), and the action's input semantics were read from its source at the pinned
// commit (src/main.ts).

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	tailscaleActionPrefix = "uses: tailscale/github-action@"
	tailnetJoinStep       = "Join the tailnet (the cluster API is reachable only through it)"
	tailnetGateStep       = "Gate — the runner joined the tailnet as tag:ci and reaches the cluster API"
)

// tailscaleActionLine is the only accepted shape of the action's `uses:`: a full 40-hex
// commit, optionally followed by the tag it was read from as a comment.
var tailscaleActionLine = regexp.MustCompile(`^uses: tailscale/github-action@[0-9a-f]{40}( # v[0-9][0-9.]*)?$`)

// localKubectlLines are the code lines of deploy.yml that name kubectl without contacting
// the cluster: installing the binary and reading the kubeconfig that was just written.
// Every other line naming kubectl — and every verify-deployment.sh mode that runs kubectl —
// is a cluster call, and the tailnet must be joined before the first one.
var localKubectlLines = []string{
	`curl -fsSLo kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl"`,
	`echo "${KUBECTL_SHA256}  kubectl" | sha256sum -c -`,
	`chmod +x kubectl`,
	`sudo mv kubectl /usr/local/bin/kubectl`,
	`kubectl version --client`,
	`kubectl config current-context`,
	`server=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')`,
}

var (
	kubectlWord        = regexp.MustCompile(`\bkubectl\b`)
	clusterScriptModes = regexp.MustCompile(`scripts/verify-deployment\.sh (operator-surface|db-role|all)\b`)
)

// TestPackaging_TheDeployReachesTheClusterOnlyThroughTheTailnet pins T134's deploy shape:
//
//   - exactly one step uses tailscale/github-action, by a full commit SHA (a tag or a
//     branch is red), with `authkey: ${{ secrets.TS_AUTHKEY }}`;
//   - the step right after it is the tailnet gate, which checks tag:ci, the backend state
//     and a TCP connect to $KUBE_API_HOST:$KUBE_API_PORT, under `set -euo pipefail`;
//   - both come before the first step that talks to the cluster (CONTROL: that step exists,
//     and every local kubectl line is seen, so a stale exemption is red), and neither carries
//     an `if:` or `continue-on-error`;
//   - the job's KUBE_API_HOST is 10.43.0.1 and "Write kubeconfig" compares the kubeconfig's
//     server against it;
//   - every `${{ secrets.* }}` sits directly under an `env:`, or under the `with:` of a
//     `uses:` step — never in a `run:` body, where it would be written into the script.
//
// MUTANT (measured, T134): the join step deleted -> red; the join moved after "Preflight —
// the application Secret…" -> red; `@d1b6cd2…` replaced by `@v4` -> red; the gate deleted ->
// red; `if: always()` on the gate -> red; `${{ secrets.TS_AUTHKEY }}` moved into a run body
// -> red; the server check removed from "Write kubeconfig" -> red.
func TestPackaging_TheDeployReachesTheClusterOnlyThroughTheTailnet(t *testing.T) {
	steps := deploySteps(t)

	// The first cluster call.
	firstCluster := -1
	seenLocal := map[string]int{}
	for i, s := range steps {
		for _, l := range s.code {
			tl := strings.TrimSpace(l)
			if slices.Contains(localKubectlLines, tl) {
				seenLocal[tl]++
				continue
			}
			if (kubectlWord.MatchString(tl) || clusterScriptModes.MatchString(tl)) && firstCluster < 0 {
				firstCluster = i
			}
		}
	}
	for _, l := range localKubectlLines {
		if seenLocal[l] != 1 {
			t.Errorf("CONTROL: the local kubectl line %q appears %d time(s), want once; a stale exemption hides nothing "+
				"today but would hide a cluster call that took its spelling", l, seenLocal[l])
		}
	}
	if firstCluster < 0 {
		t.Fatal("CONTROL FAILED: no step of deploy.yml talks to the cluster; the scan has gone blind")
	}

	// The join.
	var joins []int
	for i, s := range steps {
		for _, l := range s.code {
			if strings.HasPrefix(strings.TrimSpace(l), tailscaleActionPrefix) {
				joins = append(joins, i)
			}
		}
	}
	if len(joins) != 1 {
		t.Fatalf("deploy.yml uses tailscale/github-action in %d step(s), want exactly one: the cluster API is reachable "+
			"only through the tailnet (backlog T134)", len(joins))
	}
	join := joins[0]
	j := steps[join]
	if j.name != tailnetJoinStep {
		t.Errorf("the step using tailscale/github-action is %q, want %q", j.name, tailnetJoinStep)
	}
	for _, l := range j.code {
		if tl := strings.TrimSpace(l); strings.HasPrefix(tl, tailscaleActionPrefix) && !tailscaleActionLine.MatchString(tl) {
			t.Errorf("%q: a third-party action that holds the tailnet key is pinned by full commit SHA, not by a tag "+
				"or a branch (the exception to ci.yml's first-party rule is taken on that condition)", tl)
		}
	}
	if len(stepsWithLine(steps[join:join+1], "authkey: ${{ secrets.TS_AUTHKEY }}")) != 1 {
		t.Errorf("step %q does not take `authkey: ${{ secrets.TS_AUTHKEY }}`", j.name)
	}
	if join >= firstCluster {
		t.Errorf("step %q is step %d, the first cluster call is step %d (%q): the runner must be on the tailnet "+
			"before it talks to the API", j.name, join, firstCluster, steps[firstCluster].name)
	}

	// The gate, right after the join.
	if join+1 >= len(steps) || steps[join+1].name != tailnetGateStep {
		t.Fatalf("the step after %q is not %q: what the runner joined as must be measured before anything uses it",
			j.name, tailnetGateStep)
	}
	gate := steps[join+1]
	for _, want := range []string{
		"set -euo pipefail",
		"st=$(tailscale status --json)",
		`case ",$tags," in`,
		"*,tag:ci,*) ;;",
		`if [ "$state" != "Running" ]; then`,
		`if timeout 5 bash -c 'exec 3<>"/dev/tcp/$0/$1"' "$KUBE_API_HOST" "$KUBE_API_PORT" 2>/dev/null; then`,
	} {
		if len(stepsWithLine([]workflowStep{gate}, want)) != 1 {
			t.Errorf("step %q lacks the line %q", gate.name, want)
		}
	}
	if join+1 >= firstCluster {
		t.Errorf("step %q is step %d, the first cluster call is step %d", gate.name, join+1, firstCluster)
	}
	for _, s := range []workflowStep{j, gate} {
		for _, l := range s.code {
			if tl := strings.TrimSpace(l); strings.HasPrefix(tl, "if:") || strings.HasPrefix(tl, "continue-on-error:") {
				t.Errorf("step %q carries %q: a join or a gate that can be skipped is not one", s.name, tl)
			}
		}
	}

	// The address, and the kubeconfig held to it.
	b, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "deploy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	code := codeLinesOf(string(b))
	if n := countTrimmed(code, "KUBE_API_HOST: 10.43.0.1"); n != 1 {
		t.Errorf("deploy.yml sets `KUBE_API_HOST: 10.43.0.1` %d time(s), want once (the job's env)", n)
	}
	kc := slices.IndexFunc(steps, func(s workflowStep) bool { return s.name == "Write kubeconfig" })
	if kc < 0 {
		t.Fatal(`CONTROL FAILED: no "Write kubeconfig" step`)
	}
	if len(stepsWithLine(steps[kc:kc+1], `"https://$KUBE_API_HOST:$KUBE_API_PORT" | "https://$KUBE_API_HOST") echo "kubeconfig server: $server (tailnet)" ;;`)) != 1 {
		t.Errorf(`"Write kubeconfig" does not hold the kubeconfig's server to $KUBE_API_HOST`)
	}

	// Every secret under env: or a uses step's with:.
	secrets := 0
	for i, l := range code {
		if !strings.Contains(l, "${{ secrets.") {
			continue
		}
		secrets++
		parent, step := parentKey(code, i), stepOf(code, i)
		switch {
		case parent == "env:":
		case parent == "with:" && stepUses(code, step):
		default:
			t.Errorf("deploy.yml: %q sits under %q: a secret is handed over only through `env:` (or an action's `with:`), "+
				"never expanded where a script is rendered", strings.TrimSpace(l), parent)
		}
	}
	if secrets < 6 {
		t.Errorf("CONTROL FAILED: %d `${{ secrets.` line(s) seen, want at least 6 (Docker Hub ×4, KUBE_CONFIG, TS_AUTHKEY)",
			secrets)
	}
}

func countTrimmed(lines []string, want string) int {
	n := 0
	for _, l := range lines {
		if strings.TrimSpace(l) == want {
			n++
		}
	}
	return n
}

// parentKey returns the trimmed text of the nearest non-blank line above i that is less
// indented than line i: the YAML key (or block scalar header) the line belongs to.
func parentKey(lines []string, i int) string {
	in := indentOf(lines[i])
	for k := i - 1; k >= 0; k-- {
		if strings.TrimSpace(lines[k]) == "" {
			continue
		}
		if indentOf(lines[k]) < in {
			return strings.TrimSpace(lines[k])
		}
	}
	return ""
}

// stepOf returns the index of the `- name:` line that opens the step holding line i, or -1.
func stepOf(lines []string, i int) int {
	for k := i; k >= 0; k-- {
		if strings.HasPrefix(strings.TrimSpace(lines[k]), "- name: ") {
			return k
		}
	}
	return -1
}

// stepUses reports whether the step opened at line s carries a `uses:` key.
func stepUses(lines []string, s int) bool {
	if s < 0 {
		return false
	}
	for k := s + 1; k < len(lines); k++ {
		tl := strings.TrimSpace(lines[k])
		if strings.HasPrefix(tl, "- name: ") {
			break
		}
		if strings.HasPrefix(tl, "uses: ") {
			return true
		}
	}
	return false
}
