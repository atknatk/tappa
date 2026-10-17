package main

// operatorsurface_test.go — backlog T119: the operator host (ops.taptime.mt) is routed only
// to a server that gates it. NewRouter mounts the customer half of the host gate
// (internal/httpx operatorHostOnly) only when TAPPA_OPERATOR_HOST is set, and
// deploy/k8s/41-operator-ingress.yaml sends that host to the same Service as taptime.mt; so
// .github/workflows/deploy.yml applies 41 only behind a preflight that reads the SIZE of
// TAPPA_OPERATOR_HOST in tappa-secrets — never a value (scripts/verify-deployment.sh
// operator-surface, `kubectl describe`) — after the rollout wait, and then asks the host what
// it serves (operator-host). The cloudflare gate runs on both hosts.
//
// THREAT MODEL, stated once: these pins are against ACCIDENTAL drift — the condition
// dropped or inverted, the apply moved before the wait, a gate pointed at the wrong host, a
// script that echoes what kubectl printed, a word the deploy cannot read. Deliberate
// circumvention is code review's subject. The cluster is not read here: the script runs
// against stub `kubectl` and `curl` binaries, and what the real kubectl describe prints is the
// scratch-module measurement against k8s.io/kubectl the T119 card records (describeOutput
// below reproduces it).

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/config"
)

// ---------------------------------------------------------------- deploy.yml --

// workflowStep is one `- name:` step of deploy.yml with its code lines (whole-line
// comments dropped by codeLinesOf).
type workflowStep struct {
	name, id string
	code     []string
}

func deploySteps(t *testing.T) []workflowStep {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "deploy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var steps []workflowStep
	for _, l := range codeLinesOf(string(b)) {
		tl := strings.TrimSpace(l)
		if name, ok := strings.CutPrefix(tl, "- name: "); ok {
			steps = append(steps, workflowStep{name: name})
			continue
		}
		if len(steps) == 0 {
			continue
		}
		cur := &steps[len(steps)-1]
		if id, ok := strings.CutPrefix(tl, "id: "); ok && cur.id == "" {
			cur.id = id
		}
		cur.code = append(cur.code, l)
	}
	if len(steps) < 15 {
		t.Fatalf("CONTROL FAILED: deploy.yml parsed into %d steps; the step scan has gone blind", len(steps))
	}
	return steps
}

// stepsWithLine returns the indices of the steps carrying a code line whose trimmed text
// is exactly line.
func stepsWithLine(steps []workflowStep, line string) []int {
	var out []int
	for i, s := range steps {
		for _, l := range s.code {
			if strings.TrimSpace(l) == line {
				out = append(out, i)
				break
			}
		}
	}
	return out
}

// TestPackaging_TheOperatorIngressFollowsTheOperatorSurface pins T119's deploy-side shape:
//
//   - a preflight step, before anything but the namespace is applied, assigns
//     `surface=$(scripts/verify-deployment.sh operator-surface <41's namespace>)`
//     under `set -euo pipefail` and writes it to $GITHUB_OUTPUT under its own step id;
//   - "Roll out the server" reads that output as OPERATOR_SURFACE, waits for the rollout,
//     and only THEN reaches a `case "$OPERATOR_SURFACE"` whose `on)` arm applies 41, whose
//     `off)` arm runs no kubectl and whose `*)` arm exits 1 — no other arm;
//   - in "Roll out the server" OPERATOR_SURFACE appears exactly twice — the env line and the
//     case line — so nothing can set or default it in between;
//   - a step after the rollout runs `operator-host "$OPERATOR_SURFACE" <41's host>` with
//     OPERATOR_SURFACE taken from the same preflight output;
//   - the cloudflare gate runs, before anything is applied, once for TAPPA_BASE_URL's host
//     and once for 41's host.
//
// The hosts are read from 41 and 05-config.yaml, not written here, so a host that moves in
// one file and not the other is red.
func TestPackaging_TheOperatorIngressFollowsTheOperatorSurface(t *testing.T) {
	console, _ := operatorIngresses(t)
	opHost, ns := console.rules[0].host, console.namespace
	base, err := url.Parse(configMapValues(t, readK8s(t, "05-config.yaml"))["TAPPA_BASE_URL"])
	if err != nil || base.Host == "" || opHost == "" || ns == "" {
		t.Fatalf("CONTROL FAILED: base URL %v (%v), operator host %q, namespace %q", base, err, opHost, ns)
	}
	steps := deploySteps(t)

	firstApply := -1
	for i, s := range steps {
		for _, l := range s.code {
			for _, m := range deployApplyLine.FindAllStringSubmatch(l, -1) {
				if m[1] != "00-namespace.yaml" && firstApply < 0 {
					firstApply = i
				}
			}
		}
	}
	rollout := slices.IndexFunc(steps, func(s workflowStep) bool { return s.name == "Roll out the server" })
	if firstApply < 0 || rollout < 0 {
		t.Fatalf("CONTROL FAILED: first apply at step %d, \"Roll out the server\" at %d", firstApply, rollout)
	}

	// The preflight.
	call := "surface=$(scripts/verify-deployment.sh operator-surface " + ns + ")"
	pre := stepsWithLine(steps, call)
	if len(pre) != 1 {
		t.Fatalf("deploy.yml has %d step(s) running %q, want exactly one: the decision whether 41 may be applied", len(pre), call)
	}
	p := steps[pre[0]]
	if p.id == "" {
		t.Errorf("step %q has no id: \"Roll out the server\" cannot read its word", p.name)
	}
	if pre[0] >= firstApply {
		t.Errorf("step %q is step %d, the first apply is step %d: the decision must come before anything is applied, "+
			"so a red one stops the deploy before a pod without the gate can start", p.name, pre[0], firstApply)
	}
	for _, want := range []string{"set -euo pipefail", `echo "surface=$surface" >> "$GITHUB_OUTPUT"`} {
		if len(stepsWithLine(steps[pre[0]:pre[0]+1], want)) != 1 {
			t.Errorf("step %q lacks the line %q", p.name, want)
		}
	}

	// The rollout reads the word, waits, and only then decides.
	r := steps[rollout]
	if env := "OPERATOR_SURFACE: ${{ steps." + p.id + ".outputs.surface }}"; len(stepsWithLine(steps[rollout:rollout+1], env)) != 1 {
		t.Errorf("\"Roll out the server\" does not take %q", env)
	}
	wait, caseAt, esac := -1, -1, -1
	for i, l := range r.code {
		switch tl := strings.TrimSpace(l); {
		case strings.Contains(tl, "rollout status deployment/tappa") && wait < 0:
			wait = i
		case tl == `case "$OPERATOR_SURFACE" in`:
			caseAt = i
		case tl == "esac" && caseAt >= 0 && esac < 0:
			esac = i
		}
	}
	envLine := "OPERATOR_SURFACE: ${{ steps." + p.id + ".outputs.surface }}"
	var uses []string
	for _, l := range r.code {
		for range strings.Count(l, "OPERATOR_SURFACE") {
			uses = append(uses, strings.TrimSpace(l))
		}
	}
	if len(uses) != 2 || uses[0] != envLine || uses[1] != `case "$OPERATOR_SURFACE" in` {
		t.Errorf("in \"Roll out the server\" OPERATOR_SURFACE appears in %q; want exactly the env line and the case "+
			"line, so nothing sets or defaults it in between", uses)
	}
	if wait < 0 || caseAt < 0 || esac < 0 || caseAt < wait {
		t.Fatalf("in \"Roll out the server\": rollout wait at %d, case at %d, esac at %d; want a case on "+
			"$OPERATOR_SURFACE after the wait (41 reaches only pods that started with the gate)", wait, caseAt, esac)
	}
	arms := map[string]string{}
	var order []string
	for _, l := range r.code[caseAt+1 : esac] {
		tl := strings.TrimSpace(l)
		pat, body, ok := strings.Cut(tl, ")")
		if !ok || !strings.HasSuffix(body, ";;") {
			t.Errorf("case arm %q is not one line of `pattern) commands ;;`; this pin reads one-line arms only", tl)
			continue
		}
		arms[pat] = body
		order = append(order, pat)
	}
	if strings.Join(order, ",") != "on,off,*" {
		t.Errorf("the case on $OPERATOR_SURFACE has the arms %v, want exactly on, off, *", order)
	}
	apply41 := "kubectl apply -f deploy/k8s/" + operatorIngressFile
	if !strings.Contains(arms["on"], apply41) {
		t.Errorf("the on) arm is %q; it must apply 41", arms["on"])
	}
	if strings.Contains(arms["off"], "kubectl") {
		t.Errorf("the off) arm is %q; with the surface off nothing may route the operator host", arms["off"])
	}
	if !strings.Contains(arms["*"], "exit 1") {
		t.Errorf("the *) arm is %q; a word that is neither on nor off must stop the deploy", arms["*"])
	}
	for i, s := range steps {
		for _, l := range s.code {
			if strings.Contains(l, apply41) && (i != rollout || !strings.HasPrefix(strings.TrimSpace(l), "on)")) {
				t.Errorf("step %q applies 41 outside the on) arm: %q", s.name, strings.TrimSpace(l))
			}
		}
	}

	// The probe after the rollout, judged against the same word.
	probe := stepsWithLine(steps, `run: scripts/verify-deployment.sh operator-host "$OPERATOR_SURFACE" `+opHost)
	if len(probe) != 1 || probe[0] <= rollout {
		t.Errorf("steps running operator-host %s: %v, want exactly one after \"Roll out the server\" (step %d)", opHost,
			probe, rollout)
	} else if len(stepsWithLine(steps[probe[0]:probe[0]+1], envLine)) != 1 {
		t.Errorf("step %q does not take %q: the probe would judge without the preflight's word", steps[probe[0]].name,
			envLine)
	}

	// The cloudflare gate, on both hosts, before anything is applied.
	for _, h := range []string{base.Host, opHost} {
		got := stepsWithLine(steps, "run: scripts/verify-deployment.sh cloudflare "+h)
		if len(got) != 1 || got[0] >= firstApply {
			t.Errorf("steps running the cloudflare gate on %s: %v, want exactly one before the first apply (step %d)", h,
				got, firstApply)
		}
	}
}

// TestPackaging_TheDeployCarriesNoContinueOnError: a step that may fail without failing the
// job turns every gate above into a report — the preflight's red, the probe's VIOLATION and
// the rollout's `exit 1` would all leave the deploy green. deploy.yml has never carried the
// key (checked 2026-10-14, T119 round 3), so the pin is simply that it does not, anywhere in
// the file, comments included. PART III: a form not on this list (a `|| true`, an
// `if: always()` on a later step) is code review's subject — no completeness claim.
func TestPackaging_TheDeployCarriesNoContinueOnError(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "deploy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "- name: Roll out the server") {
		t.Fatal("CONTROL FAILED: deploy.yml was not read")
	}
	for i, l := range strings.Split(string(b), "\n") {
		if strings.Contains(l, "continue-on-error") {
			t.Errorf(".github/workflows/deploy.yml:%d carries continue-on-error: %q", i+1, strings.TrimSpace(l))
		}
	}
}

// ------------------------------------------------------------- the script, run --

// scriptRig runs scripts/verify-deployment.sh with stub `kubectl`, `curl` and `sleep` first on
// PATH. The stubs record every call and answer from files in dir: kubectl by the resource it
// is asked about (secret, ingress, exec), curl by URL — each, when a sequence is set, by the
// call's number — and sleep returns at once, recording its argument, so the probe's real
// default pause is exercised without waiting. env is added to the script's environment.
type scriptRig struct {
	t   *testing.T
	dir string
	env []string
}

const stubKubectl = `#!/bin/sh
{ for a in "$@"; do printf '%s\n' "$a"; done; printf '%s\n' '--'; } >> "$STUB_DIR/kubectl.calls"
case " $* " in
  *" secret "*) key=secret ;;
  *" ingress "*) key=ingress ;;
  *" exec "*) key=exec; cat >/dev/null ;;
  *) echo "stub kubectl: unexpected call: $*" >&2; exit 97 ;;
esac
n=$(( $(cat "$STUB_DIR/kubectl.$key.count" 2>/dev/null || echo 0) + 1 ))
printf '%s\n' "$n" > "$STUB_DIR/kubectl.$key.count"
if [ -f "$STUB_DIR/$key.$n.rc" ]; then
  if [ -f "$STUB_DIR/$key.$n.out" ]; then cat "$STUB_DIR/$key.$n.out"; fi
  exit "$(cat "$STUB_DIR/$key.$n.rc")"
fi
if [ -f "$STUB_DIR/$key.out" ]; then cat "$STUB_DIR/$key.out"; fi
if [ -f "$STUB_DIR/$key.rc" ]; then exit "$(cat "$STUB_DIR/$key.rc")"; fi
exit 0
`

const stubCurl = `#!/bin/sh
for a in "$@"; do url=$a; done
printf '%s\n' "$url" >> "$STUB_DIR/curl.calls"
key=$(printf '%s' "$url" | tr -c 'A-Za-z0-9' '_')
n=$(( $(cat "$STUB_DIR/curl.$key.count" 2>/dev/null || echo 0) + 1 ))
printf '%s\n' "$n" > "$STUB_DIR/curl.$key.count"
if [ -f "$STUB_DIR/curl.$key.$n.out" ]; then
  cat "$STUB_DIR/curl.$key.$n.out"
  exit "$(cat "$STUB_DIR/curl.$key.$n.rc")"
fi
if [ -f "$STUB_DIR/curl.$key.out" ]; then cat "$STUB_DIR/curl.$key.out"; fi
if [ -f "$STUB_DIR/curl.$key.rc" ]; then exit "$(cat "$STUB_DIR/curl.$key.rc")"; fi
exit 0
`

const stubSleep = `#!/bin/sh
printf '%s\n' "$*" >> "$STUB_DIR/sleep.calls"
exit 0
`

func newScriptRig(t *testing.T) *scriptRig {
	t.Helper()
	path := scriptPathIn(t, "verify-deployment.sh")
	if out, err := exec.CommandContext(t.Context(), "bash", "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("bash -n scripts/verify-deployment.sh: %v\n%s", err, out)
	}
	r := &scriptRig{t: t, dir: t.TempDir()}
	for name, body := range map[string]string{"kubectl": stubKubectl, "curl": stubCurl, "sleep": stubSleep} {
		if err := os.WriteFile(filepath.Join(r.dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

func (r *scriptRig) set(name, content string) {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.dir, name), []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *scriptRig) clear(names ...string) {
	for _, n := range names {
		_ = os.Remove(filepath.Join(r.dir, n))
	}
}

var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// curlReply is one answer of the curl stub: what it prints and its exit code.
type curlReply struct{ headers, rc string }

// curlAnswer sets what the curl stub prints and returns for every call to url.
func (r *scriptRig) curlAnswer(url, headers, rc string) { r.curlAnswers(url, curlReply{headers, rc}) }

// curlAnswers sets the curl stub's answers for url, one per call in order; the last one
// answers every call after it.
func (r *scriptRig) curlAnswers(url string, replies ...curlReply) {
	r.t.Helper()
	key := nonAlnum.ReplaceAllString(url, "_")
	old, err := filepath.Glob(filepath.Join(r.dir, "curl."+key+".*"))
	if err != nil {
		r.t.Fatal(err)
	}
	for _, f := range old {
		_ = os.Remove(f)
	}
	for i, c := range replies {
		r.set(fmt.Sprintf("curl.%s.%d.out", key, i+1), c.headers)
		r.set(fmt.Sprintf("curl.%s.%d.rc", key, i+1), c.rc)
	}
	last := replies[len(replies)-1]
	r.set("curl."+key+".out", last.headers)
	r.set("curl."+key+".rc", last.rc)
}

// run executes the script. KUBECONFIG names a file that does not exist, so even a real
// kubectl that somehow came first on PATH could reach no cluster.
func (r *scriptRig) run(args ...string) (stdout, stderr string, code int) {
	r.t.Helper()
	r.clear("kubectl.calls", "curl.calls", "sleep.calls")
	counts, err := filepath.Glob(filepath.Join(r.dir, "*.count"))
	if err != nil {
		r.t.Fatal(err)
	}
	for _, f := range counts {
		_ = os.Remove(f)
	}
	cmd := exec.CommandContext(r.t.Context(), "bash", append([]string{scriptPathIn(r.t, "verify-deployment.sh")}, args...)...)
	cmd.Env = append(append(os.Environ(),
		"PATH="+r.dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"STUB_DIR="+r.dir,
		"KUBECONFIG="+filepath.Join(r.dir, "no-such-kubeconfig")), r.env...)
	var out, errOut strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &errOut
	var exit *exec.ExitError
	switch err := cmd.Run(); {
	case errors.As(err, &exit):
		code = exit.ExitCode()
	case err != nil:
		r.t.Fatalf("running the script: %v", err)
	}
	return out.String(), errOut.String(), code
}

// kubectlCalls returns the recorded kubectl invocations, one argument list each.
func (r *scriptRig) kubectlCalls() [][]string {
	b, _ := os.ReadFile(filepath.Join(r.dir, "kubectl.calls"))
	var calls [][]string
	var cur []string
	for _, l := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
		if l == "--" {
			calls = append(calls, cur)
			cur = nil
			continue
		}
		if l != "" {
			cur = append(cur, l)
		}
	}
	return calls
}

// sleepCalls returns the arguments the probe's sleeps were given, one per call.
func (r *scriptRig) sleepCalls() []string {
	b, _ := os.ReadFile(filepath.Join(r.dir, "sleep.calls"))
	return strings.Fields(string(b))
}

func (r *scriptRig) curlCalls() []string {
	b, _ := os.ReadFile(filepath.Join(r.dir, "curl.calls"))
	return strings.Fields(string(b))
}

// describeOutput is `kubectl describe secret tappa-secrets` as k8s.io/kubectl v0.35.4 prints
// it — measured by running its SecretDescriber against a fake clientset (the T119 card,
// round 2): a header, then "Data", "====" and one "KEY:<padding>N bytes" line per key in
// sorted order, the padding tabwriter's (two spaces past the longest "KEY:"). annotation,
// when not empty, stands for whatever an annotation line could carry.
func describeOutput(sizes map[string]int, annotation string) string {
	keys := make([]string, 0, len(sizes))
	width := 0
	for k := range sizes {
		keys = append(keys, k)
		width = max(width, len(k)+1)
	}
	slices.Sort(keys)
	if annotation == "" {
		annotation = "<none>"
	}
	var b strings.Builder
	b.WriteString("Name:         tappa-secrets\nNamespace:    tappa\nLabels:       <none>\n")
	b.WriteString("Annotations:  " + annotation + "\n\nType:  Opaque\n\nData\n====\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "%-*s%d bytes\n", width+2, k+":", sizes[k])
	}
	return b.String()
}

// valueMarker stands for a value or anything else that must not be printed: upper-case
// letters only, so it is not shaped like a credential to scripts/secretscan.sh, and
// distinctive, so a leak is plain.
const valueMarker = "VALUEMARKERNOTFORANYLOG"

// operatorHostVariable is internal/config's own name for the operator host's variable.
func operatorHostVariable(t *testing.T) string {
	t.Helper()
	for _, n := range config.OperatorSurfaceVariables() {
		if strings.HasSuffix(n, "_HOST") {
			return n
		}
	}
	t.Fatal("internal/config's operator variables name no host")
	return ""
}

// TestVerifyDeployment_OperatorSurfaceDecidesFromTheSecretAndTheIngresses runs the
// operator-surface mode against a stubbed kubectl whose describe output is the measured
// shape. PART I: the rows are the states the mode names — internal/config's host variable
// with more than 0 bytes (with and without 41's objects), absent or 0 bytes (with none, one
// or both objects), a key whose name only starts with the variable's — and the reads that
// fail or do not parse (describe exits 1, no Data section, a size that is not "N bytes", the
// Ingresses unreadable). PART II, pinned: the exit code (0 decided / 1 VIOLATION / 2
// UNCHECKED), stdout carrying exactly `on` or `off` and nothing on any other exit (it is the
// deploy's `surface=$(...)`), the call being `describe secret tappa-secrets` in the given
// namespace, and the default namespace being 41's. PART III: a form not on this list is
// code review's subject — no completeness claim.
func TestVerifyDeployment_OperatorSurfaceDecidesFromTheSecretAndTheIngresses(t *testing.T) {
	console, _ := operatorIngresses(t)
	ns := console.namespace
	hostVar := operatorHostVariable(t)
	rig := newScriptRig(t)

	// CONTROL — the fake is the cluster's output. The orchestrator ran describe on the live
	// Secret with the deploy identity (2026-10-14, kubectl v1.36.1; `grep -nE … | cat -vet`):
	// line 8 `Data$`, line 9 `====$`, `TAPPA_OPERATOR_HOST:            14 bytes$` — no
	// trailing blank, padding of spaces. Given the operator's four keys (the longest is the
	// token key, so the padding is the measured twelve spaces), the fake prints those lines
	// byte for byte.
	live := map[string]int{"DATABASE_URL": 61}
	for _, n := range config.OperatorSurfaceVariables() {
		live[n] = 44
	}
	live[hostVar] = 14
	fake := strings.Split(describeOutput(live, ""), "\n")
	if len(fake) < 9 || fake[7] != "Data" || fake[8] != "====" ||
		!slices.Contains(fake, "TAPPA_OPERATOR_HOST:            14 bytes") {
		t.Fatalf("CONTROL FAILED: the fake describe is not the measured one:\n%s", strings.Join(fake, "\n"))
	}

	both := "ingress.networking.k8s.io/tappa-operator\ningress.networking.k8s.io/tappa-operator-legal\n"
	one := "ingress.networking.k8s.io/tappa-operator-legal\n"
	withHost := func(n int) map[string]int {
		return map[string]int{"DATABASE_URL": 61, "TAPPA_TAG_KEK": 44, hostVar: n}
	}
	noHost := map[string]int{"DATABASE_URL": 61, "TAPPA_TAG_KEK": 44}
	for _, tc := range []struct {
		name             string
		describe, objs   string
		descRC, ingRC    string
		wantCode         int
		wantOut, wantErr string
	}{
		{"the host set, a first apply", describeOutput(withHost(14), ""), "", "0", "0", 0, "on", ""},
		{"the live Secret's shape (measured)", describeOutput(live, ""), "", "0", "0", 0, "on", ""},
		{"the host set, the objects exist", describeOutput(withHost(14), ""), both, "0", "0", 0, "on", ""},
		{"no key and no objects: a fresh cluster", describeOutput(noHost, ""), "", "0", "0", 0, "off", "::notice::"},
		{"a 0-byte key counts as none", describeOutput(withHost(0), ""), "", "0", "0", 0, "off", "::notice::"},
		{"a longer name is not the key", describeOutput(map[string]int{hostVar + "_X": 14}, ""), "", "0", "0", 0, "off", "::notice::"},
		{"no key, both objects outlive the surface", describeOutput(noHost, ""), both, "0", "0", 1, "", "delete ingress tappa-operator tappa-operator-legal"},
		{"no key, one object outlives the surface", describeOutput(noHost, ""), one, "0", "0", 1, "", "::error::"},
		{"a 0-byte key, the objects outlive the surface", describeOutput(withHost(0), ""), both, "0", "0", 1, "", "::error::"},
		{"describe fails", "", "", "1", "0", 2, "", "::error::"},
		{"no Data section", "Name:         tappa-secrets\n", "", "0", "0", 2, "", "shape"},
		{"the key line carries no size", strings.Replace(describeOutput(withHost(14), ""), "14 bytes", "ops", 1), "", "0", "0", 2, "", "shape"},
		{"the Ingresses cannot be read", describeOutput(withHost(14), ""), "", "0", "1", 2, "", "::error::"},
	} {
		rig.set("secret.out", tc.describe)
		rig.set("secret.rc", tc.descRC)
		rig.set("ingress.out", tc.objs)
		rig.set("ingress.rc", tc.ingRC)
		stdout, stderr, code := rig.run("operator-surface", ns)
		if code != tc.wantCode || strings.TrimSpace(stdout) != tc.wantOut || !strings.Contains(stderr, tc.wantErr) {
			t.Errorf("%s: exit %d, stdout %q; want exit %d, stdout %q and %q in\n%s", tc.name, code, stdout, tc.wantCode,
				tc.wantOut, tc.wantErr, stderr)
		}
		calls := rig.kubectlCalls()
		if len(calls) == 0 || strings.Join(calls[0], " ") != "-n "+ns+" describe secret tappa-secrets" {
			t.Errorf("%s: the first kubectl call was %v, want exactly `-n %s describe secret tappa-secrets`", tc.name, calls, ns)
		}
	}

	rig.set("secret.out", describeOutput(withHost(14), ""))
	rig.clear("secret.rc", "ingress.out", "ingress.rc")
	if stdout, _, code := rig.run("operator-surface"); code != 0 || strings.TrimSpace(stdout) != "on" {
		t.Errorf("with no arguments: exit %d, stdout %q; want 0, on", code, stdout)
	}
	if calls := rig.kubectlCalls(); len(calls) == 0 || !slices.Contains(calls[0], ns) {
		t.Errorf("with no arguments the calls were %v; want namespace %s, 41's", calls, ns)
	}
}

// TestVerifyDeployment_OperatorSurfacePrintsNoValue: what kubectl prints for tappa-secrets
// goes into the parse and nowhere else. PART I: describe output that carries a marker — in an
// annotation line while the key is set, in a Data line of another key, on a failing describe
// (exit 1, the shape of round 1's go-template dump), in a key line whose size is not a size,
// and on a describe that prints the marker alone — runs through the mode. PART II, pinned:
// in every row the marker appears in neither stdout nor stderr, and the rows the parse
// cannot read exit 2 with no word. PART III: a form not on this list is code review's
// subject — no completeness claim.
func TestVerifyDeployment_OperatorSurfacePrintsNoValue(t *testing.T) {
	console, _ := operatorIngresses(t)
	ns := console.namespace
	hostVar := operatorHostVariable(t)
	rig := newScriptRig(t)
	rig.clear("ingress.out", "ingress.rc")
	set := map[string]int{"DATABASE_URL": 61, hostVar: 14}
	for _, tc := range []struct {
		name, out, rc string
		wantCode      int
	}{
		{"a marker in an annotation, the key set", describeOutput(set, "example.com/note: "+valueMarker), "0", 0},
		{"a marker as another key's name", describeOutput(map[string]int{hostVar: 14, valueMarker: 3}, ""), "0", 0},
		{"a failing describe that printed the marker", "Error executing template: raw data was: " + valueMarker, "1", 2},
		{"the key line carrying the marker", strings.Replace(describeOutput(set, ""), "14 bytes", valueMarker, 1), "0", 2},
		{"the marker alone, exit 0", valueMarker + "\n", "0", 2},
	} {
		rig.set("secret.out", tc.out)
		rig.set("secret.rc", tc.rc)
		stdout, stderr, code := rig.run("operator-surface", ns)
		if code != tc.wantCode || (code != 0 && stdout != "") {
			t.Errorf("%s: exit %d, stdout %q; want exit %d (and no word unless 0)", tc.name, code, stdout, tc.wantCode)
		}
		if strings.Contains(stdout+stderr, valueMarker) {
			t.Errorf("%s: the script printed what kubectl printed:\n%s%s", tc.name, stdout, stderr)
		}
	}
}

// ---------------------------------------- what may name the Secret: a whitelist --

// secretCallWhitelist is EVERY kubectl line, per file, that may name a secret (in any case
// or quoting), normalised (whole-line comments dropped, continuations joined, blanks
// collapsed), each with why it is legitimate (T119, round 4: a list of bad spellings was
// beaten twice — `get "secret/…"`, `get Secret`, `-oJSON`, `view-last-applied` — so the
// good lines are listed instead, and everything else that names a secret is refused).
var secretCallWhitelist = map[string]map[string]string{
	"scripts/verify-deployment.sh": {
		`if ! size=$(kubectl -n "$ns" describe secret tappa-secrets | awk '`: "operator-surface's one read of " +
			"tappa-secrets: describe prints key names and byte counts, never a value (k8s.io/kubectl v0.35.4 " +
			"SecretDescriber, measured; the live output measured by the orchestrator), and its stdout goes straight " +
			"into awk, which prints a number",
	},
	".github/workflows/deploy.yml": {
		`if ! kubectl -n tappa get secret tappa-secrets >/dev/null 2>&1; then`: "the Secret preflight's existence " +
			"check: a bare get prints the default table (name, type, key count, age) and no value, and even that " +
			"goes to /dev/null",
	},
}

// logicalLines drops whole-line comments and then joins every backslash-newline
// continuation, so a command split over lines is read as the one line the shell runs.
func logicalLines(src string) []string {
	return strings.Split(strings.ReplaceAll(strings.Join(codeLinesOf(src), "\n"), "\\\n", " "), "\n")
}

// outsideQuotes returns l with its quoted text removed, and whether a double-quoted part
// holds a command substitution — text the shell RUNS although it is quoted.
func outsideQuotes(l string) (outside string, runsQuoted bool) {
	var b strings.Builder
	state := byte(0) // 0 outside, '\'' or '"' inside
	for i := 0; i < len(l); i++ {
		c := l[i]
		switch state {
		case 0:
			switch c {
			case '\\':
				b.WriteByte(c)
				if i+1 < len(l) {
					i++
					b.WriteByte(l[i])
				}
			case '\'', '"':
				state = c
			default:
				b.WriteByte(c)
			}
		case '\'':
			if c == '\'' {
				state = 0
			}
		case '"':
			switch {
			case c == '\\':
				i++
			case c == '"':
				state = 0
			case c == '`', c == '$' && i+1 < len(l) && l[i+1] == '(':
				runsQuoted = true
			}
		}
	}
	return b.String(), runsQuoted
}

var (
	// secretMention is a secret named anywhere on the line, quoted or not, in any case:
	// secret, Secret, secrets, secret/NAME, tappa-secrets.
	secretMention = regexp.MustCompile(`(?i)\bsecrets?\b`)
	// messageCommand is an echo or printf at the start of a command.
	messageCommand = regexp.MustCompile(`^((then|else|do|\{)\s+)?(echo|printf)(\s|$)`)
	// kubectlHidden is kubectl held in a variable or an alias (by name or by its looked-up
	// path), or a wrapper that hands it arguments it does not name: the call it makes later
	// carries no `kubectl` to see.
	kubectlHidden = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*=["']?kubectl\b|kubectl\s+"?\$\{?[@*]|` +
		`(command\s+-v|which|type\s+-[pP])\s+kubectl\b`)
)

// pureMessage reports whether l is an echo/printf whose every kubectl is quoted text the
// shell prints and does not run: no command substitution in its quotes, and outside them
// no kubectl, no pipe, no second command, no eval.
func pureMessage(l string) bool {
	out, runs := outsideQuotes(l)
	t := strings.TrimSpace(out)
	return !runs && messageCommand.MatchString(t) && !strings.Contains(t, "kubectl") &&
		!strings.ContainsAny(t, "|;`") && !strings.Contains(t, "&&") && !strings.Contains(t, "$(") &&
		!strings.Contains(t, "eval")
}

// secretCallFindings reads src as logical lines: a line is a kubectl CALL when it carries
// kubectl and is not a pure message (kubectl outside quotes, `sh -c '…'`, `"$(kubectl …)"`,
// `echo "…" | sh`, `eval "…"`, a heredoc's body line: all calls). A call that names a secret
// must be in allowed (its count goes into seen); kubectl held in a variable or behind an
// argument passthrough is refused outright, wherever it is.
func secretCallFindings(src string, allowed map[string]string) (findings []string, seen map[string]int) {
	seen = map[string]int{}
	for i, l := range logicalLines(src) {
		if !strings.Contains(l, "kubectl") || pureMessage(l) {
			continue
		}
		if kubectlHidden.MatchString(l) {
			findings = append(findings, fmt.Sprintf("logical line %d hides kubectl (a variable, an alias or an "+
				"argument passthrough): %q", i+1, strings.TrimSpace(l)))
		}
		if !secretMention.MatchString(l) {
			continue
		}
		norm := strings.Join(strings.Fields(l), " ")
		if _, ok := allowed[norm]; ok {
			seen[norm]++
			continue
		}
		findings = append(findings, fmt.Sprintf("logical line %d is a kubectl call on the guarded resource kind "+
			"and is not on the whitelist: %q", i+1, norm))
	}
	return findings, seen
}

// TestVerifyDeployment_NothingReadsTheSecretsValues (T119; round 4: a whitelist). tappa-secrets
// holds TAPPA_TAG_KEK (CLAUDE.md §4.7), the repository and its Actions logs are public, and
// round 1 measured that kubectl's go-template printer dumps every value of the object on a
// template error — so no value is read, and the pin is a WHITELIST. PART I — each of
// scripts/verify-deployment.sh and .github/workflows/deploy.yml is read as logical lines
// (whole-line comments dropped, continuations joined); a line carrying kubectl that is not
// a pure echo/printf message is a call; a call that names a secret — `(?i)\bsecrets?\b`,
// quoted or not — must be one of secretCallWhitelist's lines for that file, each of which
// must be seen exactly once (CONTROL: a stale entry is red); kubectl in a variable, an alias
// or behind "$@" is refused. PART II — those, pinned, for both files. PART III — a form not
// on this list is code review's subject (examples: a call whose kubectl is spelled in
// pieces, `$x get secret …` on a line after the variable was set through another command,
// a value read by curl against the API) — no completeness claim; the script's branches are
// also run by TestVerifyDeployment_EveryBranchTouchesTheSecretOnlyByDescribe, which sees
// spelling-independent calls.
func TestVerifyDeployment_NothingReadsTheSecretsValues(t *testing.T) {
	for file, allowed := range secretCallWhitelist {
		b, err := os.ReadFile(filepath.Join(repoRoot, file))
		if err != nil {
			t.Fatal(err)
		}
		findings, seen := secretCallFindings(string(b), allowed)
		for _, f := range findings {
			t.Errorf("%s: %s", file, f)
		}
		for line := range allowed {
			if seen[line] != 1 {
				t.Errorf("CONTROL: %s carries the whitelisted line %d time(s), want once: %q", file, seen[line], line)
			}
		}
	}
}

// TestVerifyDeployment_TheSecretCallScanClassifiesEveryShape is the whitelist scan's own
// table (round 4): every shape the 3rd eye and this round used to read a value — split
// lines, any quoting, any case, -oJSON, view-last-applied, kubectl run through sh -c, eval,
// a pipe into a shell, a substitution inside an echo, a heredoc, a variable, an alias, a
// passthrough wrapper — is refused, and the CONTROL lines (the two whitelisted calls, an
// ingress read, a message that names kubectl and the Secret, a comment) pass. Two rows are
// stated blind spots: `$x get secret …` on its own line (the variable set elsewhere) and
// kubectl spelled in pieces carry no `kubectl` to see, so only the run-time test sees them,
// and only on a branch it runs. PART III: a form not on this list is code review's subject
// — no completeness claim.
func TestVerifyDeployment_TheSecretCallScanClassifiesEveryShape(t *testing.T) {
	script := secretCallWhitelist["scripts/verify-deployment.sh"]
	for _, tc := range []struct {
		name, src string
		refused   bool
	}{
		{"N6: a quoted secret/NAME", `kubectl -n tappa get "secret/tappa-secrets" -o yaml >&2 || true` + "\n", true},
		{"N7: the kind in capitals", `kubectl -n tappa get Secret "tappa-secrets" -o yaml` + "\n", true},
		{"N8: -oJSON", "kubectl -n tappa get secret tappa-secrets -oJSON\n", true},
		{"N9: view-last-applied", "kubectl -n tappa apply view-last-applied secret/tappa-secrets\n", true},
		{"C: split over a continuation", "kubectl -n tappa get secret tappa-secrets \\\n  -o jsonpath='{.data.host}' | base64 -d\n", true},
		{"C2: a continuation and a quoted yaml", "kubectl -n \"$ns\" get secret tappa-secrets \\\n  -o \"yaml\"\n", true},
		{"a plain get with an output", "kubectl -n tappa get secret tappa-secrets -o name\n", true},
		{"the plural", "kubectl get secrets\n", true},
		{"sh -c, double quotes", "sh -c \"kubectl get secret tappa-secrets -o json\"\n", true},
		{"sh -c, single quotes", "sh -c 'kubectl get secret tappa-secrets -o yaml'\n", true},
		{"eval", "eval \"kubectl get secret tappa-secrets -o yaml\"\n", true},
		{"an echo piped into a shell", "echo \"kubectl get secret tappa-secrets -o yaml\" | sh\n", true},
		{"a substitution inside an echo", "echo \"$(kubectl get secret tappa-secrets -o yaml)\"\n", true},
		{"a heredoc's body", "bash <<'EOF'\nkubectl get secret tappa-secrets -o yaml\nEOF\n", true},
		{"a variable and its use on one line", "x=kubectl; $x get secret tappa-secrets -o yaml\n", true},
		{"kubectl held in a variable", "x=kubectl\n", true},
		{"an alias", "alias k=kubectl\n", true},
		{"a passthrough wrapper", "k() { kubectl \"$@\"; }\n", true},
		{"its path in a variable", "k=$(command -v kubectl)\n", true},
		{"its path through which", "k=\"$(which kubectl)\"\n", true},
		{"BLIND SPOT (PART III): kubectl spelled in pieces", "\"ku\"\"bectl\" get secret tappa-secrets -o yaml\n", false},
		{"BLIND SPOT (PART III): the variable's use alone", "$x get secret tappa-secrets -o yaml\n", false},
		{"CONTROL: the whitelisted describe", "  if ! size=$(kubectl -n \"$ns\" describe secret tappa-secrets | awk '\n", false},
		{"CONTROL: an ingress read by name", "kubectl -n \"$ns\" get ingress tappa-operator --ignore-not-found -o name\n", false},
		{"CONTROL: a message naming kubectl and the Secret",
			"echo \"::error::kubectl describe secret tappa-secrets -o json was not run ('x')\" >&2\n", false},
		{"CONTROL: a commented-out read", "# kubectl get secret tappa-secrets -o yaml\n", false},
	} {
		findings, _ := secretCallFindings(tc.src, script)
		if got := len(findings) > 0; got != tc.refused {
			t.Errorf("%s: refused = %v, want %v (%v)", tc.name, got, tc.refused, findings)
		}
	}
	if findings, _ := secretCallFindings("if ! kubectl -n tappa get secret tappa-secrets >/dev/null 2>&1; then\n",
		secretCallWhitelist[".github/workflows/deploy.yml"]); len(findings) != 0 {
		t.Errorf("CONTROL: the workflow's existence check is refused: %v", findings)
	}
}

// TestVerifyDeployment_EveryBranchTouchesTheSecretOnlyByDescribe (T119, round 4) is the
// run-time half: every mode of the script — cloudflare, operator-surface, operator-host,
// api-host (M11, merged in API-13b), db-role, all, and an unknown one — is run against the stub kubectl through every branch
// (each pass, VIOLATION, UNCHECKED, notice and warning; the 404 loop's end; db-role's two
// "did not run" exits), and the stub records every argv. PART I — the runs, then two
// readings: every recorded call whose arguments mention a secret (any case) is exactly
// `-n <ns> describe secret tappa-secrets` (CONTROL: at least one is), whatever spelling
// produced it; and every annotation the script can print (each `echo "::error::…"`,
// `::warning::`, `::notice::` line, read from the script) was printed by some run — a
// branch added without a row here is red, because an unrun branch is where round 3's
// regression sat. PART II — those, pinned. PART III — a form not on this list is code
// review's subject (a branch that prints no annotation and makes no call this list drives;
// a call through a binary that is not kubectl) — no completeness claim.
func TestVerifyDeployment_EveryBranchTouchesTheSecretOnlyByDescribe(t *testing.T) {
	console, _ := operatorIngresses(t)
	opHost, ns := console.rules[0].host, console.namespace
	hostVar := operatorHostVariable(t)
	base, err := url.Parse(configMapValues(t, readK8s(t, "05-config.yaml"))["TAPPA_BASE_URL"])
	if err != nil || base.Host == "" {
		t.Fatalf("05-config.yaml's TAPPA_BASE_URL: %v", err)
	}
	custHealth, opHealth, opRoot := "https://"+base.Host+"/healthz", "https://"+opHost+"/healthz", "https://"+opHost+"/"
	// M11 API-13a's api-host mode (merged with T119 in API-13b): its curl writes "<status> <content type>".
	apiHost := configMapValues(t, readK8s(t, "05-config.yaml"))["TAPPA_API_HOST"]
	if apiHost == "" {
		t.Fatal("05-config.yaml has no TAPPA_API_HOST")
	}
	apiGuide, apiRoot := "https://"+apiHost+"/v1/guide.md", "https://"+apiHost+"/"
	gate := curlReply{"HTTP/2 303 \r\nlocation: /operator\r\n\r\n", "0"}
	notRouted := curlReply{"HTTP/2 404 \r\n\r\n", "0"}
	product := curlReply{"HTTP/2 200 \r\n\r\n", "0"}
	unresolved := curlReply{"curl: (6) Could not resolve host", "6"}
	timeout := curlReply{"curl: (28) Operation timed out", "28"}
	on := describeOutput(map[string]int{"DATABASE_URL": 61, hostVar: 14}, "")
	off := describeOutput(map[string]int{"DATABASE_URL": 61}, "")
	both := "ingress.networking.k8s.io/tappa-operator\ningress.networking.k8s.io/tappa-operator-legal\n"

	rig := newScriptRig(t)
	base4 := append([]string(nil), rig.env...)
	var printed strings.Builder
	var calls [][]string
	healthy := func() {
		rig.env = append(append([]string(nil), base4...), "OPERATOR_HOST_PROBE_TRIES=2")
		rig.curlAnswer(custHealth, "HTTP/2 200 \r\n\r\n", "0")
		rig.curlAnswer(opHealth, notRouted.headers, "0")
		rig.curlAnswers(opRoot, gate)
		rig.curlAnswer(apiGuide, "200 text/markdown; charset=utf-8", "0")
		rig.curlAnswer(apiRoot, "404", "0")
		rig.set("secret.out", on)
		rig.set("ingress.out", "")
		rig.clear("secret.rc", "ingress.rc", "exec.out", "exec.rc", "exec.1.out", "exec.1.rc", "exec.2.out", "exec.2.rc")
	}
	for _, r := range []struct {
		name  string
		setup func()
		args  []string
	}{
		{"cloudflare: pass", func() {}, []string{"cloudflare", base.Host}},
		{"cloudflare: unresolvable", func() { rig.curlAnswers(custHealth, unresolved) }, []string{"cloudflare", base.Host}},
		{"cloudflare: unreachable", func() { rig.curlAnswers(custHealth, timeout) }, []string{"cloudflare", base.Host}},
		{"cloudflare: proxied", func() { rig.curlAnswer(custHealth, "HTTP/2 200 \r\ncf-ray: 0-MXP\r\n\r\n", "0") }, []string{"cloudflare", base.Host}},
		{"operator-surface: on", func() {}, []string{"operator-surface", ns}},
		{"operator-surface: off", func() { rig.set("secret.out", off) }, []string{"operator-surface", ns}},
		{"operator-surface: off, objects left", func() { rig.set("secret.out", off); rig.set("ingress.out", both) }, []string{"operator-surface", ns}},
		{"operator-surface: describe fails", func() { rig.set("secret.rc", "1") }, []string{"operator-surface", ns}},
		{"operator-surface: unparseable", func() { rig.set("secret.out", "Name: x\n") }, []string{"operator-surface", ns}},
		{"operator-surface: ingresses unreadable", func() { rig.set("ingress.rc", "1") }, []string{"operator-surface", ns}},
		{"operator-host: a bad try count", func() { rig.env = append(rig.env, "OPERATOR_HOST_PROBE_TRIES=x") }, []string{"operator-host", "on", opHost}},
		{"operator-host: on, the gate", func() {}, []string{"operator-host", "on", opHost}},
		{"operator-host: on, 404 to the end of the loop", func() { rig.curlAnswers(opRoot, notRouted) }, []string{"operator-host", "on", opHost}},
		{"operator-host: on, the customer product", func() { rig.curlAnswers(opRoot, product) }, []string{"operator-host", "on", opHost}},
		{"operator-host: on, an unknown answer", func() { rig.curlAnswer(opRoot, "HTTP/2 503 \r\n\r\n", "0") }, []string{"operator-host", "on", opHost}},
		{"operator-host: on, unresolvable", func() { rig.curlAnswers(opRoot, unresolved) }, []string{"operator-host", "on", opHost}},
		{"operator-host: unreachable", func() { rig.curlAnswers(opRoot, timeout) }, []string{"operator-host", "on", opHost}},
		{"operator-host: off, not routed", func() { rig.curlAnswers(opRoot, notRouted) }, []string{"operator-host", "off", opHost}},
		{"operator-host: off, unresolvable", func() { rig.curlAnswers(opRoot, unresolved) }, []string{"operator-host", "off", opHost}},
		{"operator-host: off, the gate", func() {}, []string{"operator-host", "off", opHost}},
		{"operator-host: no word", func() {}, []string{"operator-host", "", opHost}},
		{"api-host: pass", func() {}, []string{"api-host", apiHost}},
		{"api-host: unresolvable", func() { rig.curlAnswers(apiGuide, unresolved) }, []string{"api-host", apiHost}},
		{"api-host: unreachable", func() { rig.curlAnswers(apiGuide, timeout) }, []string{"api-host", apiHost}},
		{"api-host: the guide in another type", func() { rig.curlAnswer(apiGuide, "200 text/html; charset=utf-8", "0") }, []string{"api-host", apiHost}},
		{"api-host: the root unreachable", func() { rig.curlAnswers(apiRoot, timeout) }, []string{"api-host", apiHost}},
		{"api-host: the root answers", func() { rig.curlAnswer(apiRoot, "200", "0") }, []string{"api-host", apiHost}},
		{"db-role: pass", func() {}, []string{"db-role", ns}},
		{"db-role: an offender", func() { rig.set("exec.out", "tappa_owner from 10.42.0.9\n") }, []string{"db-role", ns}},
		{"db-role: the query does not run", func() { rig.set("exec.rc", "3") }, []string{"db-role", ns}},
		{"db-role: the summary does not run", func() {
			rig.set("exec.1.out", "")
			rig.set("exec.1.rc", "0")
			rig.set("exec.2.out", "")
			rig.set("exec.2.rc", "3")
		}, []string{"db-role", ns}},
		{"all: healthy", func() {}, []string{"all"}},
		{"all: every gate unhappy", func() {
			rig.curlAnswers(custHealth, timeout)
			rig.curlAnswers(opHealth, unresolved)
			rig.set("secret.out", off)
			rig.set("ingress.out", both)
			rig.curlAnswers(opRoot, product)
			rig.set("exec.rc", "3")
		}, []string{"all"}},
		{"an unknown mode", func() {}, []string{"no-such-mode"}},
	} {
		healthy()
		r.setup()
		stdout, stderr, _ := rig.run(r.args...)
		printed.WriteString(stdout + stderr)
		calls = append(calls, rig.kubectlCalls()...)
	}

	want := "-n " + ns + " describe secret tappa-secrets"
	describes := 0
	for _, c := range calls {
		j := strings.Join(c, " ")
		if !secretMention.MatchString(j) {
			continue
		}
		if j != want {
			t.Errorf("a run called kubectl %q; the only call that may name a secret is %q", j, want)
			continue
		}
		describes++
	}
	if describes == 0 {
		t.Error("CONTROL FAILED: no run described the Secret; the stub was not reached")
	}

	annotation := regexp.MustCompile(`echo "::(error|warning|notice)::([^"]*)`)
	// One level of nesting, as in ${location:+ with Location ${location}}.
	expansion := regexp.MustCompile(`\$\{(?:[^{}]|\$\{[^}]*\})*\}|\$[A-Za-z_][A-Za-z0-9_]*`)
	branches := 0
	for _, l := range logicalLines(readScript(t, "verify-deployment.sh")) {
		m := annotation.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		branches++
		longest := ""
		for _, part := range expansion.Split(m[2], -1) {
			if len(part) > len(longest) {
				longest = part
			}
		}
		if len(longest) > 40 {
			longest = longest[:40]
		}
		if !strings.Contains(printed.String(), longest) {
			t.Errorf("no run printed the %s annotation containing %q: a branch this test does not drive", m[1], longest)
		}
	}
	if branches < 15 {
		t.Errorf("CONTROL FAILED: %d annotation lines read from the script; the scan has gone blind", branches)
	}
}

// TestVerifyDeployment_OperatorHostReadsTheRootOfTheHost runs the operator-host mode against
// a stubbed curl, judged against the surface word (T119 round 3: a 404 right after 41 is
// first created is the controller not having configured it yet, so with `on` it is
// retried and is never a pass). PART I: the measured answers of `HEAD /` (2026-10-14: the
// customer host 200, the operator host 303 to /operator, an unrouted name 404) and their
// neighbours under `on`, `off` and no word; a 404 that turns into the 303 (or a 200) on a
// later try; curl exit 6 and 28. PART II, pinned: `on` passes only the 303 to /operator
// and retries a 404 exactly OPERATOR_HOST_PROBE_TRIES times (default 18) before going red;
// `off` passes a 404 and an unresolvable name; a 2xx is a VIOLATION at once under every
// word; every other answer exits 1; the request is always for `/` of 41's host — never
// /healthz, which this cluster's ingress default server answers with 200 for a name
// nothing routes. PART III: a form not on this list is code review's subject — no
// completeness claim.
func TestVerifyDeployment_OperatorHostReadsTheRootOfTheHost(t *testing.T) {
	console, _ := operatorIngresses(t)
	opHost := console.rules[0].host
	root := "https://" + opHost + "/"
	gate := curlReply{"HTTP/2 303 \r\nlocation: /operator\r\ncache-control: no-store\r\n\r\n", "0"}
	notRouted := curlReply{"HTTP/2 404 \r\ncontent-type: text/html\r\n\r\n", "0"}
	product := curlReply{"HTTP/2 200 \r\ncontent-type: text/html; charset=utf-8\r\n\r\n", "0"}
	rig := newScriptRig(t)
	rig.env = append(rig.env, "OPERATOR_HOST_PROBE_TRIES=3")
	for _, tc := range []struct {
		name, surface string
		replies       []curlReply
		wantCode      int
		wantCalls     int
		wantMark      string // in stdout+stderr
	}{
		// on: only the gate passes.
		{"on: the gate (measured)", "on", []curlReply{gate}, 0, 1, "303 to /operator"},
		{"on: the gate over HTTP/1.1", "on", []curlReply{{"HTTP/1.1 303 See Other\r\nLocation: /operator\r\n\r\n", "0"}}, 0, 1, "303 to /operator"},
		{"on: 404 on every try is red", "on", []curlReply{notRouted}, 1, 3, "404 on all 3 tries"},
		{"on: 404, then the gate", "on", []curlReply{notRouted, gate}, 0, 2, "try 2"},
		{"on: 404, 404, then the gate on the last try", "on", []curlReply{notRouted, notRouted, gate}, 0, 3, "try 3"},
		{"on: the customer product, at once", "on", []curlReply{product}, 1, 1, "CUSTOMER"},
		{"on: 404, then the customer product", "on", []curlReply{notRouted, product}, 1, 2, "CUSTOMER"},
		{"on: a 303 elsewhere", "on", []curlReply{{"HTTP/2 303 \r\nlocation: /admin\r\n\r\n", "0"}}, 1, 1, "does not know"},
		{"on: a 303 with no Location", "on", []curlReply{{"HTTP/2 303 \r\n\r\n", "0"}}, 1, 1, "does not know"},
		{"on: a 503", "on", []curlReply{{"HTTP/2 503 \r\n\r\n", "0"}}, 1, 1, "does not know"},
		{"on: no status line", "on", []curlReply{{"", "0"}}, 1, 1, "does not know"},
		{"on: the name does not resolve", "on", []curlReply{{"curl: (6) Could not resolve host", "6"}}, 0, 1, "::warning::"},
		{"on: a timeout", "on", []curlReply{{"curl: (28) Operation timed out", "28"}}, 1, 1, "::error::"},
		// off: not routed passes.
		{"off: not routed (measured)", "off", []curlReply{notRouted}, 0, 1, "as the surface is off"},
		{"off: the name does not resolve", "off", []curlReply{{"curl: (6) Could not resolve host", "6"}}, 0, 1, "surface is off"},
		{"off: the customer product", "off", []curlReply{product}, 1, 1, "CUSTOMER"},
		{"off: the gate is not what off expects", "off", []curlReply{gate}, 1, 1, "does not know"},
		// no word: only a 2xx is judged.
		{"no word: the customer product", "", []curlReply{product}, 1, 1, "CUSTOMER"},
		{"no word: the gate", "", []curlReply{gate}, 1, 1, "no surface word"},
		{"a word that is not one: the gate", "yes", []curlReply{gate}, 1, 1, "no surface word"},
	} {
		rig.curlAnswers(root, tc.replies...)
		stdout, stderr, code := rig.run("operator-host", tc.surface, opHost)
		if code != tc.wantCode || !strings.Contains(stdout+stderr, tc.wantMark) {
			t.Errorf("%s: exit %d; want exit %d and %q in\n%s%s", tc.name, code, tc.wantCode, tc.wantMark, stdout, stderr)
		}
		calls := rig.curlCalls()
		if len(calls) != tc.wantCalls || slices.ContainsFunc(calls, func(c string) bool { return c != root }) {
			t.Errorf("%s: curl was asked for %v, want %s %d time(s)", tc.name, calls, root, tc.wantCalls)
		}
	}

	// The defaults: 41's host, 18 tries, and 17 pauses of 5 s between them (the stub sleep
	// records each argument, so the default pause is pinned without waiting for it).
	rig.env = nil
	rig.curlAnswer(root, notRouted.headers, "0")
	if _, _, code := rig.run("operator-host", "on"); code != 1 || len(rig.curlCalls()) != 18 {
		t.Errorf("with the defaults and a 404 that never turns: exit %d after %d tries; want exit 1 after 18 (about "+
			"90 s at the default 5 s apart)", code, len(rig.curlCalls()))
	}
	if sleeps := rig.sleepCalls(); len(sleeps) != 17 || slices.ContainsFunc(sleeps, func(a string) bool { return a != "5" }) {
		t.Errorf("with the defaults the probe paused %v; want 17 pauses of 5 (seconds) between 18 tries", sleeps)
	}
	if calls := rig.curlCalls(); len(calls) == 0 || calls[0] != root {
		t.Errorf("with no host: curl asked for %v, want 41's host, %s", calls, root)
	}
	rig.env = append(rig.env, "OPERATOR_HOST_PROBE_TRIES=0")
	if _, stderr, code := rig.run("operator-host", "on"); code != 1 || len(rig.curlCalls()) != 0 {
		t.Errorf("a try count of 0: exit %d, %d calls; want exit 1 and no call\n%s", code, len(rig.curlCalls()), stderr)
	}
}

// TestVerifyDeployment_AllRunsEveryGate: `all` with no arguments runs the cloudflare gate on
// TAPPA_BASE_URL's host and on 41's host, operator-surface, operator-host on 41's host judged
// against operator-surface's word, and db-role — every one, whatever an earlier one said —
// and its exit code follows the words (1 on a VIOLATION, else 2 on an UNCHECKED, else 0).
// PART I: a healthy deployment with the surface on, one with it off and not routed, then one
// fault at a time (the operator host proxied, its root the customer product, the surface off
// with the objects left, the operator name unresolvable, the Secret unreadable). PART II:
// the summary line and exit code of each, pinned; every gate ran. PART III: a form not on
// this list is code review's subject — no completeness claim.
func TestVerifyDeployment_AllRunsEveryGate(t *testing.T) {
	console, _ := operatorIngresses(t)
	opHost := console.rules[0].host
	hostVar := operatorHostVariable(t)
	base, err := url.Parse(configMapValues(t, readK8s(t, "05-config.yaml"))["TAPPA_BASE_URL"])
	if err != nil || base.Host == "" {
		t.Fatalf("05-config.yaml's TAPPA_BASE_URL: %v", err)
	}
	custHealth, opHealth, opRoot := "https://"+base.Host+"/healthz", "https://"+opHost+"/healthz", "https://"+opHost+"/"
	rig := newScriptRig(t)
	rig.env = append(rig.env, "OPERATOR_HOST_PROBE_TRIES=2")
	healthy := func() {
		rig.curlAnswer(custHealth, "HTTP/2 200 \r\nstrict-transport-security: max-age=31536000\r\n\r\n", "0")
		rig.curlAnswer(opHealth, "HTTP/2 404 \r\n\r\n", "0")
		rig.curlAnswer(opRoot, "HTTP/2 303 \r\nlocation: /operator\r\n\r\n", "0")
		rig.set("secret.out", describeOutput(map[string]int{hostVar: 14}, ""))
		rig.set("ingress.out", "ingress.networking.k8s.io/tappa-operator\n")
		rig.clear("secret.rc", "ingress.rc", "exec.out", "exec.rc")
	}
	line := func(cf, ocf, osw, oh, db string) string {
		return "all: cloudflare[" + base.Host + "]=" + cf + " cloudflare[" + opHost + "]=" + ocf + " operator-surface=" + osw +
			" operator-host=" + oh + " db-role=" + db
	}
	for _, tc := range []struct {
		name     string
		fault    func()
		wantLine string
		wantCode int
	}{
		{"healthy, the surface on", func() {}, line("pass", "pass", "pass", "pass", "pass"), 0},
		{"healthy, the surface off and not routed", func() {
			rig.set("secret.out", describeOutput(map[string]int{"DATABASE_URL": 61}, ""))
			rig.clear("ingress.out")
			rig.curlAnswer(opRoot, "HTTP/2 404 \r\n\r\n", "0")
		}, line("pass", "pass", "pass", "pass", "pass"), 0},
		{"the operator host proxied", func() {
			rig.curlAnswer(opHealth, "HTTP/2 404 \r\nserver: cloudflare\r\ncf-ray: 0000000000000000-MXP\r\n\r\n", "0")
		}, line("pass", "VIOLATION", "pass", "pass", "pass"), 1},
		{"the operator host serves the customer product", func() {
			rig.curlAnswer(opRoot, "HTTP/2 200 \r\n\r\n", "0")
		}, line("pass", "pass", "pass", "VIOLATION", "pass"), 1},
		{"the surface on, the name never routed", func() {
			rig.curlAnswer(opRoot, "HTTP/2 404 \r\n\r\n", "0")
		}, line("pass", "pass", "pass", "UNCHECKED", "pass"), 2},
		{"the surface off, the objects left", func() {
			rig.set("secret.out", describeOutput(map[string]int{"DATABASE_URL": 61}, ""))
		}, line("pass", "pass", "VIOLATION", "UNCHECKED", "pass"), 1},
		{"the operator name unresolvable", func() {
			rig.curlAnswer(opHealth, "curl: (6) Could not resolve host", "6")
			rig.curlAnswer(opRoot, "curl: (6) Could not resolve host", "6")
		}, line("pass", "UNCHECKED", "pass", "UNCHECKED", "pass"), 2},
		{"the Secret unreadable", func() {
			rig.set("secret.rc", "1")
		}, line("pass", "pass", "UNCHECKED", "UNCHECKED", "pass"), 2},
	} {
		healthy()
		tc.fault()
		stdout, stderr, code := rig.run("all")
		var got string
		for _, l := range strings.Split(stdout, "\n") {
			if strings.HasPrefix(l, "all: ") {
				got = l
			}
		}
		if got != tc.wantLine || code != tc.wantCode {
			t.Errorf("%s: %q, exit %d; want %q, exit %d\n%s", tc.name, got, code, tc.wantLine, tc.wantCode, stderr)
		}
		calls := slices.Compact(slices.Sorted(slices.Values(rig.curlCalls())))
		want := []string{custHealth, opRoot, opHealth}
		slices.Sort(want)
		if !slices.Equal(calls, want) {
			t.Errorf("%s: curl was asked for %v, want %v (every gate runs)", tc.name, calls, want)
		}
		var kinds []string
		for _, c := range rig.kubectlCalls() {
			for _, k := range []string{"secret", "ingress", "exec"} {
				if slices.Contains(c, k) {
					kinds = append(kinds, k)
				}
			}
		}
		if !slices.Contains(kinds, "secret") || !slices.Contains(kinds, "exec") {
			t.Errorf("%s: kubectl calls %v; want the operator-surface read and the db-role exec", tc.name, kinds)
		}
	}
}
