package main

// deploytailnet_test.go — backlog T134: the cluster API (144.76.158.60:6443) was closed to
// the internet on purpose (2026-10-17) and is reached only through the tailnet, at
// 10.43.0.1:443. .github/workflows/deploy.yml therefore checks the written kubeconfig offline
// (scripts/verify-deployment.sh kubeconfig-server), joins the tailnet with a SHA-pinned
// tailscale/github-action, measures what it joined as and whether the API answers
// (verify-deployment.sh tailnet), and only then makes its first call against the cluster.
//
// THREAT MODEL, as in operatorsurface_test.go: these pins are against ACCIDENTAL drift —
// the join deleted or moved, the pin loosened to a tag, an input dropped, a gate made
// skippable or turned into a warning, the status echoed, a secret spelled into a `run:`
// body. Deliberate circumvention is code review's subject. Nothing here contacts Tailscale or
// the cluster: the two modes run against a stub `tailscale`, a stub `timeout` that hands the
// real TCP connect on, a listener on 127.0.0.1 and the real `kubectl config view` on a
// kubeconfig written here, with a PATH that holds nothing else — so a tailscale CLI on the
// machine running the test is never reached. The action's input semantics were read from its
// source at the pinned commit (src/main.ts, src/logging.ts).
//
// 🔴 2ND ROUND: the gate used to be a shell body in deploy.yml and its test pinned the body's
// LINES. Seven mutants that changed what it does kept that test green (an `exit 1` turned into
// a warning, the TCP refusal deleted, `*10.43.0.1*)` ahead of the kubeconfig check, the join
// moved before the build, `use-cache: "true"`, `--accept-dns=false` dropped, the status
// echoed). The gate is now a script mode and is RUN here; the workflow keeps line pins only
// for what is configuration (the action's inputs, the step order).

import (
	"encoding/base64"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	tailscaleActionPrefix = "uses: tailscale/github-action@"
	tailnetJoinStep       = "Join the tailnet (the cluster API is reachable only through it)"
	tailnetGateStep       = "Gate — the runner joined the tailnet as tag:ci and reaches the cluster API"
	tailnetGateRun        = `run: scripts/verify-deployment.sh tailnet "$KUBE_API_HOST" "$KUBE_API_PORT"`
	kubeconfigServerLine  = `scripts/verify-deployment.sh kubeconfig-server "$KUBE_API_HOST" "$KUBE_API_PORT"`
)

// tailnetJoinInputs are the join step's `with:` lines besides the key, each argued at the step:
// no DNS takeover, no unchecked cached binary, no echoed status, a bounded wait.
var tailnetJoinInputs = []string{
	"args: --accept-dns=false",
	`use-cache: "false"`,
	"log-mode: quiet",
	"timeout: 45s",
	`retry: "3"`,
}

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
}

var (
	kubectlWord        = regexp.MustCompile(`\bkubectl\b`)
	clusterScriptModes = regexp.MustCompile(`scripts/verify-deployment\.sh (operator-surface|db-role|all)\b`)
)

// imageStepLine is a code line of a step that builds, proves, pushes or inspects an image.
var imageStepLine = regexp.MustCompile(`\bdocker (build|push|login|manifest)\b|scripts/verify-image\.sh`)

// TestPackaging_TheDeployReachesTheClusterOnlyThroughTheTailnet pins T134's deploy shape — the
// configuration; what the two gates DO is run by the TestVerifyDeployment_ tests below:
//
//   - exactly one step uses tailscale/github-action, by a full commit SHA (a tag or a branch is
//     red), with `authkey: ${{ secrets.TS_AUTHKEY }}`, each of tailnetJoinInputs and no `tags:`
//     (with an auth key the action ignores it: a line that looks like a guarantee);
//   - it comes AFTER the last image step (no repository code that ran during the build ever had
//     a route into the tailnet; CONTROL: image steps exist) and BEFORE the first step that
//     talks to the cluster (CONTROL: that step exists, and every local kubectl line is seen, so
//     a stale exemption is red);
//   - the step right after it is the tailnet gate, whose whole body is tailnetGateRun;
//   - neither carries an `if:` or `continue-on-error`;
//   - the job's KUBE_API_HOST is 10.43.0.1 and KUBE_API_PORT "443", and "Write kubeconfig" ends
//     in kubeconfigServerLine, before the first cluster call;
//   - every `${{ secrets.* }}` sits directly under an `env:`, or under the `with:` of a `uses:`
//     step — never in a `run:` body, where it would be written into the script.
//
// MUTANT (measured, T134 rounds 1 and 2): the join deleted, moved after "Preflight — the
// application Secret…", or moved (with the gate) before "Build both images" -> red;
// `@d1b6cd2…` replaced by `@v4` -> red; `use-cache: "true"`, `--accept-dns=false` dropped or
// `log-mode: quiet` dropped -> red; the gate deleted or `if: always()` on it -> red;
// `${{ secrets.TS_AUTHKEY }}` moved into a run body -> red; the kubeconfig check removed -> red.
func TestPackaging_TheDeployReachesTheClusterOnlyThroughTheTailnet(t *testing.T) {
	steps := deploySteps(t)

	// The first cluster call, and the last image step.
	firstCluster, lastImage := -1, -1
	seenLocal := map[string]int{}
	for i, s := range steps {
		for _, l := range s.code {
			tl := strings.TrimSpace(l)
			if imageStepLine.MatchString(tl) {
				lastImage = i
			}
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
	if firstCluster < 0 || lastImage < 0 {
		t.Fatalf("CONTROL FAILED: first cluster call at step %d, last image step at %d; the scan has gone blind",
			firstCluster, lastImage)
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
		tl := strings.TrimSpace(l)
		if strings.HasPrefix(tl, tailscaleActionPrefix) && !tailscaleActionLine.MatchString(tl) {
			t.Errorf("%q: a third-party action that holds the tailnet key is pinned by full commit SHA, not by a tag "+
				"or a branch (the exception to ci.yml's first-party rule is taken on that condition)", tl)
		}
		if strings.HasPrefix(tl, "tags:") {
			t.Errorf("step %q carries %q: with an auth key the action passes no --advertise-tags (src/main.ts), so the "+
				"line does nothing and reads like a guarantee; the gate measures the tag", j.name, tl)
		}
	}
	for _, want := range append([]string{"authkey: ${{ secrets.TS_AUTHKEY }}"}, tailnetJoinInputs...) {
		if len(stepsWithLine(steps[join:join+1], want)) != 1 {
			t.Errorf("step %q lacks the input line %q (each is argued at the step)", j.name, want)
		}
	}
	if join <= lastImage {
		t.Errorf("step %q is step %d, the last image step is step %d (%q): the runner joins the tailnet only after "+
			"every image is built, proven and pushed, so no code that ran during the build had a route into it",
			j.name, join, lastImage, steps[lastImage].name)
	}
	if join >= firstCluster {
		t.Errorf("step %q is step %d, the first cluster call is step %d (%q): the runner must be on the tailnet "+
			"before it talks to the API", j.name, join, firstCluster, steps[firstCluster].name)
	}

	// The gate, right after the join, and nothing but the script mode.
	if join+1 >= len(steps) || steps[join+1].name != tailnetGateStep {
		t.Fatalf("the step after %q is not %q: what the runner joined as must be measured before anything uses it",
			j.name, tailnetGateStep)
	}
	gate := steps[join+1]
	var gateCode []string
	for _, l := range gate.code {
		if tl := strings.TrimSpace(l); tl != "" {
			gateCode = append(gateCode, tl)
		}
	}
	if !slices.Equal(gateCode, []string{tailnetGateRun}) {
		t.Errorf("step %q is %q, want exactly %q: the gate's behaviour lives in the script, where "+
			"TestVerifyDeployment_TheTailnetModeMeasuresTheJoin runs it", gate.name, gateCode, tailnetGateRun)
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
	for _, want := range []string{"KUBE_API_HOST: 10.43.0.1", `KUBE_API_PORT: "443"`} {
		if n := countTrimmed(code, want); n != 1 {
			t.Errorf("deploy.yml sets %q %d time(s), want once (the job's env)", want, n)
		}
	}
	kc := slices.IndexFunc(steps, func(s workflowStep) bool { return s.name == "Write kubeconfig" })
	if kc < 0 {
		t.Fatal(`CONTROL FAILED: no "Write kubeconfig" step`)
	}
	last := ""
	for _, l := range steps[kc].code {
		if tl := strings.TrimSpace(l); tl != "" {
			last = tl
		}
	}
	if last != kubeconfigServerLine {
		t.Errorf(`"Write kubeconfig" does not end in %q: the kubeconfig's server, proxy and TLS check are not held to the `+
			`tailnet address`, kubeconfigServerLine)
	}
	if kc >= firstCluster {
		t.Errorf(`"Write kubeconfig" is step %d, after the first cluster call (step %d)`, kc, firstCluster)
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

// ------------------------------------------------------------- the two script modes --

// stubTailscale answers `version` with v1.94.2's shape and `status --json` from
// tailscale.status.out / .rc in STUB_DIR; any other call is refused. Every call is recorded.
const stubTailscale = `#!/bin/sh
printf '%s\n' "$*" >> "$STUB_DIR/tailscale.calls"
case $1 in
  version) printf '1.94.2\n  tailscale commit: 0000000\n' ;;
  status)
    if [ -f "$STUB_DIR/tailscale.status.out" ]; then cat "$STUB_DIR/tailscale.status.out"; fi
    if [ -f "$STUB_DIR/tailscale.status.rc" ]; then exit "$(cat "$STUB_DIR/tailscale.status.rc")"; fi ;;
  *) echo "stub tailscale: unexpected call: $*" >&2; exit 97 ;;
esac
exit 0
`

// stubTimeout records its whole argv and then runs the command it was given — the real TCP
// connect against the test's listener — unless STUB_TIMEOUT_RC is set, in which case it
// answers that code at once (124 is a timeout) and connects to nothing.
const stubTimeout = `#!/bin/sh
printf '%s\n' "$*" >> "$STUB_DIR/timeout.calls"
if [ -n "$STUB_TIMEOUT_RC" ]; then exit "$STUB_TIMEOUT_RC"; fi
shift
exec "$@"
`

// toolsDir returns a directory holding links to the named programs, found on the test's own
// PATH: the script's PATH is that directory and the stubs, and nothing else, so a tailscale
// CLI installed on the machine running the test is never reached (its status would print the
// tailnet's login names).
func toolsDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		p, err := exec.LookPath(n)
		if err != nil {
			t.Fatalf("%s is not on PATH: the deploy runner has it, and the gate's test needs it (%v)", n, err)
		}
		if err := os.Symlink(p, filepath.Join(dir, n)); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// tcpPorts returns a port with a listener on 127.0.0.1 (accepting and closing, for the test's
// life) and a port nothing listens on.
func tcpPorts(t *testing.T) (open, closed string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	gone, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := gone.Addr().(*net.TCPAddr).Port
	if err := gone.Close(); err != nil {
		t.Fatal(err)
	}
	return strconv.Itoa(ln.Addr().(*net.TCPAddr).Port), strconv.Itoa(closedPort)
}

// loginCanary stands for the tailnet's user login names (e-mail addresses) that a real
// `tailscale status --json` carries in User, Peer and Self; no output may carry it.
const loginCanary = "TAILNETLOGINCANARY"

// tailnetStatus is a `tailscale status --json` document with backend state backend and Self
// self (a JSON value, or "" for no Self), its User and Peer maps holding loginCanary.
func tailnetStatus(backend, self string) string {
	doc := `{"Version":"1.94.2"`
	if backend != "" {
		doc += `,"BackendState":` + backend
	}
	if self != "" {
		doc += `,"Self":` + self
	}
	return doc + `,"User":{"1":{"ID":1,"LoginName":"` + loginCanary + `@example.invalid","DisplayName":"` + loginCanary +
		`"}},"Peer":{"nodekey:0":{"HostName":"k8s-1","UserID":1,"Tags":["tag:ci"],"DNSName":"` + loginCanary + `.ts.net."}}}`
}

// tailnetSelf is a Self object with the given Tags (a JSON value, "" for none) and Ephemeral
// ("" for none).
func tailnetSelf(tags, ephemeral string) string {
	self := `{"HostName":"github-runner","UserID":1,"DNSName":"` + loginCanary + `-runner.ts.net."`
	if tags != "" {
		self += `,"Tags":` + tags
	}
	if ephemeral != "" {
		self += `,"Ephemeral":` + ephemeral
	}
	return self + "}"
}

// TestVerifyDeployment_TheTailnetModeMeasuresTheJoin runs `verify-deployment.sh tailnet` against
// a stub tailscale whose status the rows write, a stub timeout that hands the TCP connect on to
// bash, and a real listener (open port) or a port nobody listens on (closed). PATH holds the
// stubs and bash/jq/sed/cat only.
//
// PART I — the rows: RED (exit 1, VIOLATION) for every Tags that is not an array holding the
// exact string "tag:ci" — absent, [], null, "tag:cix", "tag:CI", " tag:ci", "tag:ci-old", the
// string "tag:ci" — and for a closed port; RED (exit 2, UNCHECKED) for no tailscale CLI, a
// failing status, empty output, broken JSON, a JSON array, NeedsLogin, no BackendState, no
// Self, a Self that is not an object; GREEN (exit 0) for ["tag:ci"] and ["tag:other","tag:ci"]
// with an open port, Ephemeral true (no warning), false and absent (a warning each).
// PART II — pinned, on every row: the exit code and the message; that no output carries
// loginCanary (the status is never printed); how many TCP attempts were made (0 before the
// tag is proven, 1 on an open port, 6 on a closed one), each through `timeout 5`, with 5 s
// pauses between them and none after the last; the client version printed. The defaults
// (no host, no port) dial 10.43.0.1:443 — read off the stub timeout's argv, which then answers
// 124 without connecting anywhere.
// PART III — a form not on this list is code review's subject; no completeness claim.
func TestVerifyDeployment_TheTailnetModeMeasuresTheJoin(t *testing.T) {
	rig := newScriptRig(t)
	tools := toolsDir(t, "bash", "jq", "sed", "cat")
	open, closed := tcpPorts(t)
	tagged := tailnetStatus(`"Running"`, tailnetSelf(`["tag:ci"]`, ""))
	for _, tc := range []struct {
		name       string
		status     string
		statusRC   string
		noCLI      bool
		port       string
		wantCode   int
		wantMark   string
		wantTCP    int
		wantNoMark string
	}{
		// The tag.
		{name: "Tags absent", status: tailnetStatus(`"Running"`, tailnetSelf("", "")), wantCode: 1, wantMark: "WITHOUT tag:ci (tags: none)"},
		{name: "Tags []", status: tailnetStatus(`"Running"`, tailnetSelf(`[]`, "")), wantCode: 1, wantMark: "WITHOUT tag:ci"},
		{name: "Tags null", status: tailnetStatus(`"Running"`, tailnetSelf(`null`, "")), wantCode: 1, wantMark: "WITHOUT tag:ci"},
		{name: "tag:cix", status: tailnetStatus(`"Running"`, tailnetSelf(`["tag:cix"]`, "")), wantCode: 1, wantMark: "WITHOUT tag:ci (tags: tag:cix)"},
		{name: "tag:CI", status: tailnetStatus(`"Running"`, tailnetSelf(`["tag:CI"]`, "")), wantCode: 1, wantMark: "WITHOUT tag:ci (tags: tag:CI)"},
		{name: "a space before tag:ci", status: tailnetStatus(`"Running"`, tailnetSelf(`[" tag:ci"]`, "")), wantCode: 1, wantMark: "WITHOUT tag:ci (tags: none)"},
		{name: "tag:ci-old", status: tailnetStatus(`"Running"`, tailnetSelf(`["tag:ci-old"]`, "")), wantCode: 1, wantMark: "WITHOUT tag:ci"},
		{name: "Tags a string", status: tailnetStatus(`"Running"`, tailnetSelf(`"tag:ci"`, "")), wantCode: 1, wantMark: "WITHOUT tag:ci"},
		// What cannot be read.
		{name: "no tailscale CLI", noCLI: true, wantCode: 2, wantMark: "no tailscale CLI"},
		{name: "status fails", status: tagged, statusRC: "1", wantCode: 2, wantMark: "tailscale status --json failed"},
		{name: "empty output", status: "", wantCode: 2, wantMark: "did not print the JSON object"},
		{name: "broken JSON", status: `{"BackendState":"Running","Self":{"Tags":["tag:ci"]` + loginCanary, wantCode: 2, wantMark: "did not print the JSON object"},
		{name: "a JSON array", status: `["tag:ci"]`, wantCode: 2, wantMark: "did not print the JSON object"},
		{name: "NeedsLogin", status: tailnetStatus(`"NeedsLogin"`, tailnetSelf(`["tag:ci"]`, "")), wantCode: 2, wantMark: "backend state 'NeedsLogin'"},
		{name: "no BackendState", status: tailnetStatus("", tailnetSelf(`["tag:ci"]`, "")), wantCode: 2, wantMark: "backend state 'absent'"},
		{name: "no Self", status: tailnetStatus(`"Running"`, ""), wantCode: 2, wantMark: "carries no Self"},
		{name: "Self not an object", status: tailnetStatus(`"Running"`, `"`+loginCanary+`"`), wantCode: 2, wantMark: "carries no Self"},
		// The route.
		{name: "tag:ci, the port closed", status: tagged, port: closed, wantCode: 1, wantMark: "is not reachable over the tailnet (6 TCP attempts", wantTCP: 6},
		// Green.
		{name: "tag:ci, the port open", status: tagged, wantCode: 0, wantMark: "(attempt 1)", wantTCP: 1},
		{name: "tag:other and tag:ci, ephemeral", status: tailnetStatus(`"Running"`, tailnetSelf(`["tag:other","tag:ci"]`, "true")),
			wantCode: 0, wantMark: "tags tag:other,tag:ci, ephemeral true", wantTCP: 1, wantNoMark: "::warning::"},
		{name: "tag:ci, not ephemeral", status: tailnetStatus(`"Running"`, tailnetSelf(`["tag:ci"]`, "false")),
			wantCode: 0, wantMark: "::warning::this runner's tailnet device is NOT ephemeral", wantTCP: 1},
		{name: "tag:ci, ephemerality not reported", status: tagged, wantCode: 0, wantMark: "does not report whether this device is ephemeral", wantTCP: 1},
	} {
		rig.set("tailscale.status.out", tc.status)
		rig.clear("tailscale.status.rc", "tailscale")
		if tc.statusRC != "" {
			rig.set("tailscale.status.rc", tc.statusRC)
		}
		if !tc.noCLI {
			if err := os.WriteFile(filepath.Join(rig.dir, "tailscale"), []byte(stubTailscale), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		port := tc.port
		if port == "" {
			port = open
		}
		rig.env = []string{"PATH=" + rig.dir + string(os.PathListSeparator) + tools}
		stdout, stderr, code := rig.run("tailnet", "127.0.0.1", port)
		out := stdout + stderr
		if code != tc.wantCode || !strings.Contains(out, tc.wantMark) || (tc.wantNoMark != "" && strings.Contains(out, tc.wantNoMark)) {
			t.Errorf("%s: exit %d; want exit %d, %q in the output (and not %q):\n%s", tc.name, code, tc.wantCode, tc.wantMark,
				tc.wantNoMark, out)
		}
		if strings.Contains(out, loginCanary) {
			t.Errorf("%s: the output carries the status's login names — the status JSON was printed:\n%s", tc.name, out)
		}
		if !tc.noCLI && tc.statusRC == "" && tc.wantCode != 2 && !strings.Contains(out, "tailnet: tailscale 1.94.2, backend Running") {
			t.Errorf("%s: the summary line does not name the client version and the backend:\n%s", tc.name, out)
		}
		calls := rig.lines("timeout.calls")
		wantCall := `5 bash -c exec 3<>"/dev/tcp/$0/$1" 127.0.0.1 ` + port
		if len(calls) != tc.wantTCP || slices.ContainsFunc(calls, func(c string) bool { return c != wantCall }) {
			t.Errorf("%s: TCP attempts %q, want %d of %q", tc.name, calls, tc.wantTCP, wantCall)
		}
		if sleeps := rig.sleepCalls(); len(sleeps) != max(tc.wantTCP-1, 0) || slices.ContainsFunc(sleeps, func(a string) bool { return a != "5" }) {
			t.Errorf("%s: pauses %v, want %d of 5 s (between attempts, none after the last)", tc.name, sleeps, max(tc.wantTCP-1, 0))
		}
	}

	// The defaults: 10.43.0.1:443.
	rig.set("tailscale.status.out", tagged)
	rig.clear("tailscale.status.rc")
	rig.env = []string{"PATH=" + rig.dir + string(os.PathListSeparator) + tools, "STUB_TIMEOUT_RC=124"}
	stdout, stderr, code := rig.run("tailnet")
	calls := rig.lines("timeout.calls")
	if code != 1 || !strings.Contains(stderr, "10.43.0.1:443 is not reachable") || len(calls) != 6 ||
		!strings.HasSuffix(calls[0], " 10.43.0.1 443") {
		t.Errorf("no host and port: exit %d, TCP attempts %q; want exit 1 after 6 attempts at 10.43.0.1 443\n%s%s",
			code, calls, stdout, stderr)
	}
}

// lines returns the lines of a file the stubs wrote in the rig's directory.
func (r *scriptRig) lines(name string) []string {
	b, err := os.ReadFile(filepath.Join(r.dir, name))
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// kubeconfigMarker stands for the token and the CA data: neither may be printed.
const kubeconfigMarker = "KUBECONFIGVALUEMARKER"

// kubeconfigFor writes a kubeconfig with two clusters — "tailnet" (server, plus extra YAML lines
// under its `cluster:`) listed FIRST and "public" (the closed public endpoint) — a context for
// each, and current set as the current context ("" for none).
func kubeconfigFor(t *testing.T, server, extra, current string) string {
	t.Helper()
	doc := "apiVersion: v1\nkind: Config\nclusters:\n- name: tailnet\n  cluster:\n    server: " + server + "\n" +
		"    certificate-authority-data: " + base64.StdEncoding.EncodeToString([]byte(kubeconfigMarker)) + "\n"
	for _, l := range strings.Split(extra, "\n") {
		if l != "" {
			doc += "    " + l + "\n"
		}
	}
	doc += "- name: public\n  cluster:\n    server: https://144.76.158.60:6443\n" +
		"contexts:\n- name: tappa\n  context: {cluster: tailnet, user: github-deployer, namespace: tappa}\n" +
		"- name: old\n  context: {cluster: public, user: github-deployer, namespace: tappa}\n" +
		"users:\n- name: github-deployer\n  user: {token: " + kubeconfigMarker + "}\n"
	if current != "" {
		doc += "current-context: " + current + "\n"
	}
	p := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestVerifyDeployment_TheKubeconfigServerModeHoldsTheTailnetAddress runs `verify-deployment.sh
// kubeconfig-server 10.43.0.1 443` — the deploy's own arguments — with the REAL `kubectl config
// view` (offline) on kubeconfigs written here; PATH holds kubectl and bash only.
//
// PART I — the rows: RED (exit 1) for the public endpoint, the tailnet host on :6443,
// 10.43.0.10, 10.43.0.1.nip.io, http://, a path, two trailing slashes, userinfo in the URL, a
// proxy-url, insecure-skip-tls-verify: true, and a current context that names the public
// cluster while the tailnet one is listed first; with port 6443 asked for, the bare host is red
// too. RED (exit 2) for no current context and a current context that does not exist. GREEN
// (exit 0) for https://10.43.0.1:443, https://10.43.0.1, each with a trailing "/", and an
// explicit insecure-skip-tls-verify: false.
// PART II — pinned: the exit code and the message; the token and CA data never printed, nor
// a server that carries userinfo. With no arguments the mode holds 10.43.0.1:443.
// PART III — a form not on this list is code review's subject; no completeness claim.
func TestVerifyDeployment_TheKubeconfigServerModeHoldsTheTailnetAddress(t *testing.T) {
	if _, err := exec.LookPath("kubectl"); err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("kubectl is not on PATH in CI: the runner image ships it, and this test needs it (%v)", err)
		}
		t.Skipf("kubectl is not on PATH (%v); CI runs this test", err)
	}
	path := toolsDir(t, "kubectl", "bash")
	run := func(cfg string, args ...string) (string, int) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "bash", append([]string{scriptPathIn(t, "verify-deployment.sh"), "kubeconfig-server"}, args...)...)
		cmd.Env = []string{"PATH=" + path, "KUBECONFIG=" + cfg, "HOME=" + t.TempDir()}
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		return string(out), code
	}
	for _, tc := range []struct {
		name, server, extra, current string
		port                         string
		wantCode                     int
		wantMark                     string
	}{
		{name: "the public endpoint", server: "https://144.76.158.60:6443", wantCode: 1, wantMark: "names the server 'https://144.76.158.60:6443'"},
		{name: "the tailnet host on :6443", server: "https://10.43.0.1:6443", wantCode: 1, wantMark: "want https://10.43.0.1:443"},
		{name: "10.43.0.10", server: "https://10.43.0.10:443", wantCode: 1, wantMark: "want https://10.43.0.1:443"},
		{name: "10.43.0.1.nip.io", server: "https://10.43.0.1.nip.io:443", wantCode: 1, wantMark: "want https://10.43.0.1:443"},
		{name: "10.43.0.1.nip.io, no port", server: "https://10.43.0.1.nip.io", wantCode: 1, wantMark: "want https://10.43.0.1:443"},
		{name: "http://", server: "http://10.43.0.1:443", wantCode: 1, wantMark: "want https://10.43.0.1:443"},
		{name: "a path", server: "https://10.43.0.1:443/api", wantCode: 1, wantMark: "want https://10.43.0.1:443"},
		{name: "two trailing slashes", server: "https://10.43.0.1:443//", wantCode: 1, wantMark: "want https://10.43.0.1:443"},
		{name: "userinfo", server: "https://" + kubeconfigMarker + "@10.43.0.1:443", wantCode: 1, wantMark: "<not shown: not an address>"},
		{name: "a proxy-url", server: "https://10.43.0.1:443", extra: "proxy-url: socks5://127.0.0.1:1080", wantCode: 1, wantMark: "sets proxy-url"},
		{name: "insecure-skip-tls-verify: true", server: "https://10.43.0.1:443", extra: "insecure-skip-tls-verify: true", wantCode: 1, wantMark: "sets insecure-skip-tls-verify"},
		{name: "the current context names the public cluster", server: "https://10.43.0.1:443", current: "old", wantCode: 1, wantMark: "names the server 'https://144.76.158.60:6443'"},
		{name: "the bare host when 6443 is asked for", server: "https://10.43.0.1", port: "6443", wantCode: 1, wantMark: "want https://10.43.0.1:6443"},
		{name: "no current context", server: "https://10.43.0.1:443", current: "-", wantCode: 2, wantMark: "current context could not be read"},
		{name: "a current context that does not exist", server: "https://10.43.0.1:443", current: "gone", wantCode: 2, wantMark: "current context could not be read"},
		{name: "https://10.43.0.1:443", server: "https://10.43.0.1:443", wantCode: 0, wantMark: "kubeconfig-server: https://10.43.0.1:443 (the tailnet)"},
		{name: "https://10.43.0.1:443/", server: "https://10.43.0.1:443/", wantCode: 0, wantMark: "(the tailnet)"},
		{name: "https://10.43.0.1", server: "https://10.43.0.1", wantCode: 0, wantMark: "(the tailnet)"},
		{name: "https://10.43.0.1/", server: "https://10.43.0.1/", wantCode: 0, wantMark: "(the tailnet)"},
		{name: "insecure-skip-tls-verify: false", server: "https://10.43.0.1:443", extra: "insecure-skip-tls-verify: false", wantCode: 0, wantMark: "certificate verified"},
	} {
		current := tc.current
		switch current {
		case "":
			current = "tappa"
		case "-":
			current = ""
		}
		port := tc.port
		if port == "" {
			port = "443"
		}
		out, code := run(kubeconfigFor(t, tc.server, tc.extra, current), "10.43.0.1", port)
		if code != tc.wantCode || !strings.Contains(out, tc.wantMark) {
			t.Errorf("%s: exit %d; want exit %d and %q in:\n%s", tc.name, code, tc.wantCode, tc.wantMark, out)
		}
		if strings.Contains(out, kubeconfigMarker) || strings.Contains(out, base64.StdEncoding.EncodeToString([]byte(kubeconfigMarker))) {
			t.Errorf("%s: the output carries the token, the CA data or a server's userinfo:\n%s", tc.name, out)
		}
	}
	if out, code := run(kubeconfigFor(t, "https://10.43.0.1:443", "", "tappa")); code != 0 {
		t.Errorf("no arguments, the tailnet server: exit %d, want 0\n%s", code, out)
	}
	if out, code := run(kubeconfigFor(t, "https://10.43.0.10:443", "", "tappa")); code != 1 || !strings.Contains(out, "want https://10.43.0.1:443") {
		t.Errorf("no arguments, 10.43.0.10: exit %d, want 1 naming https://10.43.0.1:443\n%s", code, out)
	}
}
