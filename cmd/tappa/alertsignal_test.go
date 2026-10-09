package main

// alertsignal_test.go — T116 / Q28 (a): the two signals the cluster sends to the external
// dead man's switch (healthchecks.io), and the properties that make them safe to have.
//
//   deploy/k8s/55-heartbeat.yaml   CronJob tappa-heartbeat: every 5 min, GET
//                                  $TAPPA_BASE_URL/healthz, then "alive" or "/fail"
//   scripts/pg-backup-ship.sh      the nightly backup's EXIT trap: "alive" on exit 0,
//                                  "/fail" otherwise (50-backup.yaml injects the URL)
//
// THREAT MODEL, stated once: these pins are against ACCIDENTAL drift; a deliberate bypass
// is code review's subject. Three properties are load-bearing and each one is EXECUTED here
// rather than read:
//
//  1. The backup never depends on its alarm: with the Secret absent, the URL refused or the
//     signal endpoint failing, the ship script's exit status is the one it had without any
//     signal (TestBackupShip_TheSignalNeverChangesTheOutcome runs every case twice).
//  2. The signal URL is a secret (whoever knows it can send "alive" and mute the alarm), so
//     it is in no log line, no error text and — for the heartbeat — no argv. The stubs below
//     are LEAKY ON PURPOSE (the fake wget prints the URL on both streams), so a script that
//     stopped discarding its client's output goes red.
//  3. "Down" is told as "/fail" and "up" as the bare URL — never the other way round.
//
// The real images (curlimages/curl:8.22.0, rclone/rclone:1.71) were measured against a
// local fake server under the pods' own constraints; that measurement is the card's
// (T116), and these tests are what keeps it true when the files change. They run the
// scripts with the HOST's sh and curl, so the shell is not busybox: the busybox-specific
// facts (EXIT-trap status, case classes, `timeout`'s 143) are the card's measurements.

import (
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// pingMarker is the secret part of every fake signal URL below. Upper-case letters only, so
// it is not shaped like a credential to scripts/secretscan.sh — and distinctive, so a leak
// of the path into any output is unambiguous.
const pingMarker = "PINGMARKERTONESIXTEEN"

func readK8s(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", name))
	if err != nil {
		t.Fatalf("read deploy/k8s/%s: %v", name, err)
	}
	return string(b)
}

// ------------------------------------------------------------------ manifest parsing --

// envSource is what one env entry of one container is wired to.
type envSource struct {
	kind     string // "secretKeyRef", "configMapKeyRef", "value", or "" (no source at all)
	ref      string // the Secret's or ConfigMap's name
	key      string
	optional bool
}

// envSourcesOf parses the env: list of the container named `container`, looked up under
// BOTH containers: and initContainers: (an init container is still a reader of whatever
// it is handed). The shape is the one containerEnvEntries reads, extended with WHERE each
// value comes from, because "is it a Secret, which one, which key, optional or not" is the
// whole question for the two signal URLs.
func envSourcesOf(t *testing.T, manifest, container string) map[string]envSource {
	t.Helper()
	clean := stripYAMLComments(manifest)
	var item []string
	for _, list := range []string{"containers:", "initContainers:"} {
		if body := blockUnder(clean, list); body != nil {
			if it := listItemNamed(body, container); it != nil {
				item = it
				break
			}
		}
	}
	if item == nil {
		t.Fatalf("no container named %q under containers: or initContainers:", container)
	}
	out := map[string]envSource{}
	cur := ""
	for _, line := range blockUnder(strings.Join(item, "\n"), "env:") {
		txt := strings.TrimSpace(line)
		if strings.HasPrefix(txt, "- name: ") {
			cur = unquote(strings.TrimSpace(strings.TrimPrefix(txt, "- name: ")))
			out[cur] = envSource{}
			continue
		}
		if cur == "" {
			continue
		}
		e := out[cur]
		switch {
		case txt == "secretKeyRef:" || txt == "configMapKeyRef:":
			e.kind = strings.TrimSuffix(txt, ":")
		case strings.HasPrefix(txt, "value:"):
			e.kind = "value"
		case strings.HasPrefix(txt, "name: ") && e.kind != "":
			e.ref = unquote(strings.TrimSpace(strings.TrimPrefix(txt, "name: ")))
		case strings.HasPrefix(txt, "key: "):
			e.key = unquote(strings.TrimSpace(strings.TrimPrefix(txt, "key: ")))
		case strings.HasPrefix(txt, "optional:"):
			e.optional = strings.Contains(txt, "true")
		}
		out[cur] = e
	}
	return out
}

// scalarOnce returns the value of `key:` in the comment-stripped manifest and fails unless
// the key occurs EXACTLY once — a second occurrence would make "the" value a guess.
func scalarOnce(t *testing.T, manifest, key string) string {
	t.Helper()
	re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(key) + `:[ \t]*(.*?)[ \t]*$`)
	ms := re.FindAllStringSubmatch(stripYAMLComments(manifest), -1)
	if len(ms) != 1 {
		t.Fatalf("%s: occurs %d times in the manifest, want exactly 1", key, len(ms))
	}
	return unquote(ms[0][1])
}

// labelsUnder reads the `key: value` lines of the first labels:/matchLabels: block found
// under the given chain of keys (each looked up inside the previous one's block).
func labelsUnder(t *testing.T, manifest string, chain ...string) map[string]string {
	t.Helper()
	block := strings.Split(stripYAMLComments(manifest), "\n")
	for _, key := range chain {
		block = blockUnder(strings.Join(block, "\n"), key)
		if block == nil {
			t.Fatalf("no %q block along %v", key, chain)
		}
	}
	out := map[string]string{}
	for _, line := range block {
		k, v, ok := strings.Cut(strings.TrimSpace(line), ":")
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		out[strings.TrimSpace(k)] = unquote(strings.TrimSpace(v))
	}
	if len(out) == 0 {
		t.Fatalf("the block along %v carries no labels; the parse has gone blind", chain)
	}
	return out
}

// heartbeatScript extracts the inline script — the one `- |` block scalar of
// 55-heartbeat.yaml — with its indentation removed, exactly as the kubelet hands it to sh.
func heartbeatScript(t *testing.T) string {
	t.Helper()
	lines := strings.Split(readK8s(t, "55-heartbeat.yaml"), "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "- |" {
			if start >= 0 {
				t.Fatal("55-heartbeat.yaml carries two block scalars; this extractor reads exactly one")
			}
			start = i
		}
	}
	if start < 0 {
		t.Fatal("55-heartbeat.yaml carries no `- |` block scalar: the inline script is gone")
	}
	marker := indentOf(lines[start])
	base := -1
	var body []string
	for _, l := range lines[start+1:] {
		if strings.TrimSpace(l) == "" {
			body = append(body, "")
			continue
		}
		ind := indentOf(l)
		if ind <= marker || (base >= 0 && ind < base) {
			break
		}
		if base < 0 {
			base = ind
		}
		body = append(body, l[base:])
	}
	script := strings.TrimRight(strings.Join(body, "\n"), "\n") + "\n"
	// CONTROL: the extraction found the script and not some other block.
	for _, want := range []string{"UPTIME_PING_URL", "TAPPA_BASE_URL", "-K -"} {
		if !strings.Contains(script, want) {
			t.Fatalf("the extracted heartbeat script does not mention %q; the extractor has gone blind", want)
		}
	}
	return script
}

// ------------------------------------------------------------------ the heartbeat --

// TestHeartbeat_RunsEveryFiveMinutesOneAtATime pins the CronJob's schedule and the four
// numbers that keep 288 runs a day from piling up or overlapping.
func TestHeartbeat_RunsEveryFiveMinutesOneAtATime(t *testing.T) {
	t.Parallel()
	m := readK8s(t, "55-heartbeat.yaml")
	for key, want := range map[string]string{
		"kind":              "CronJob",
		"name":              "tappa-heartbeat",
		"namespace":         "tappa",
		"schedule":          "*/5 * * * *",
		"concurrencyPolicy": "Forbid",
		"restartPolicy":     "Never",
		"backoffLimit":      "0",
	} {
		if key == "name" {
			// `name:` also names the container and the env refs; the CronJob's is the first.
			got := regexp.MustCompile(`(?m)^  name:[ \t]*(\S+)`).FindStringSubmatch(stripYAMLComments(m))
			if got == nil || got[1] != want {
				t.Errorf("metadata.name = %v, want %q", got, want)
			}
			continue
		}
		if got := scalarOnce(t, m, key); got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	num := func(key string) int {
		n, err := strconv.Atoi(scalarOnce(t, m, key))
		if err != nil {
			t.Fatalf("%s is not a number: %v", key, err)
		}
		return n
	}
	// A late run says nothing about the slot it was meant for, and the next slot is five
	// minutes away: the deadline must be set and shorter than the period.
	if n := num("startingDeadlineSeconds"); n <= 0 || n >= 300 {
		t.Errorf("startingDeadlineSeconds = %d, want 1..299 (shorter than the 5 minute period)", n)
	}
	if n := num("activeDeadlineSeconds"); n < 30 || n > 120 {
		t.Errorf("activeDeadlineSeconds = %d, want 30..120: a stuck probe must end well inside its own period", n)
	}
	if n := num("successfulJobsHistoryLimit"); n < 0 || n > 3 {
		t.Errorf("successfulJobsHistoryLimit = %d, want 0..3: 288 runs a day must not accumulate", n)
	}
	if n := num("failedJobsHistoryLimit"); n < 1 || n > 5 {
		t.Errorf("failedJobsHistoryLimit = %d, want 1..5: keep some failures as evidence, not all", n)
	}
}

// TestHeartbeat_TheScriptsWorstCaseFitsItsDeadline: the two curl calls together must stay
// under activeDeadlineSeconds, or a slow day turns every run into a DeadlineExceeded Job
// whose signal never left.
//
// 🔴 --retry-delay IS NOT A BOUND, AND THE FIRST VERSION OF THIS TEST SAID IT WAS. curl
// honours a server's Retry-After over --retry-delay: measured in curlimages/curl:8.22.0, a
// /healthz 503 with "Retry-After: 50" held the script 50.9 s, and the same on the signal
// 101 s — while this test, summing max-time and retry-delay, said 36 s. The only flag that
// bounds a retried call whatever the server says is --retry-max-time (a retry starts only
// inside it, and only if the server's wait fits in it), so a call that retries MUST carry
// it, and its bound is retry-max-time + max-time (the last attempt may start just inside
// the window). Measured after the fix, Retry-After: 50 on both: under 2 s, no retry.
func TestHeartbeat_TheScriptsWorstCaseFitsItsDeadline(t *testing.T) {
	t.Parallel()
	script := codeOf(heartbeatScript(t))
	deadline, err := strconv.Atoi(scalarOnce(t, readK8s(t, "55-heartbeat.yaml"), "activeDeadlineSeconds"))
	if err != nil {
		t.Fatal(err)
	}
	flag := func(line, name string) (int, bool) {
		m := regexp.MustCompile(`(?:^|\s)` + regexp.QuoteMeta(name) + `\s+(\d+)`).FindStringSubmatch(line)
		if m == nil {
			return 0, false
		}
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	calls, budget := 0, 0
	for _, line := range strings.Split(script, "\n") {
		if !regexp.MustCompile(`(^|[|(]\s*|\$\()curl\s`).MatchString(strings.TrimSpace(line)) {
			continue
		}
		calls++
		maxTime, ok := flag(line, "--max-time")
		if !ok {
			t.Errorf("a curl call carries no --max-time, so nothing bounds it: %s", strings.TrimSpace(line))
			continue
		}
		retry, _ := flag(line, "--retry")
		if retry == 0 {
			budget += maxTime
			continue
		}
		window, ok := flag(line, "--retry-max-time")
		if !ok || window == 0 {
			t.Errorf("a curl call retries without --retry-max-time, so a server's Retry-After decides how long "+
				"it waits (measured: 50 s): %s", strings.TrimSpace(line))
			continue
		}
		budget += window + maxTime
	}
	if calls != 2 {
		t.Fatalf("found %d curl calls in the heartbeat script, want 2 (the probe and the signal)", calls)
	}
	if budget >= deadline {
		t.Errorf("the two curl calls may take %d s together, not under activeDeadlineSeconds = %d", budget, deadline)
	}
}

// TestHeartbeat_ImageIsPinnedToAnExactVersion: an exact curl release AND its full index
// digest (64 hex), never :latest, a major-only tag or a tag alone, pulled IfNotPresent. The
// tag is for the reader; the digest is what the kubelet resolves.
func TestHeartbeat_ImageIsPinnedToAnExactVersion(t *testing.T) {
	t.Parallel()
	m := readK8s(t, "55-heartbeat.yaml")
	img := scalarOnce(t, m, "image")
	if !regexp.MustCompile(`^curlimages/curl:\d+\.\d+\.\d+@sha256:[0-9a-f]{64}$`).MatchString(img) {
		t.Errorf("image = %q, want curlimages/curl:<major>.<minor>.<patch>@sha256:<64 hex>: a tag can be "+
			"re-pushed, and a moving one changes the binary under a schedule nobody re-reads", img)
	}
	if got := scalarOnce(t, m, "imagePullPolicy"); got != "IfNotPresent" {
		t.Errorf("imagePullPolicy = %q, want IfNotPresent (every 5 minutes must not be a registry round trip)", got)
	}
}

// TestHeartbeat_TheSignalURLIsARequiredSecretAndTheTargetComesFromTheConfigMap.
//
// REQUIRED, unlike the backup's: a heartbeat that skipped its own signal when the Secret is
// missing is the one configuration that can never alert. The probe target is the
// ConfigMap's canonical origin and is never spelled in the script.
func TestHeartbeat_TheSignalURLIsARequiredSecretAndTheTargetComesFromTheConfigMap(t *testing.T) {
	t.Parallel()
	m := readK8s(t, "55-heartbeat.yaml")
	env := envSourcesOf(t, m, "heartbeat")
	want := map[string]envSource{
		"UPTIME_PING_URL": {kind: "secretKeyRef", ref: "tappa-alert-pings", key: "UPTIME_PING_URL"},
		"TAPPA_BASE_URL":  {kind: "configMapKeyRef", ref: "tappa-config", key: "TAPPA_BASE_URL"},
	}
	for name, w := range want {
		if got, ok := env[name]; !ok || got != w {
			t.Errorf("env %s = %+v (present=%v), want %+v", name, got, ok, w)
		}
	}
	if len(env) != len(want) {
		t.Errorf("the heartbeat container has %d env entries %v, want exactly %d: one value of the Secret, "+
			"never BACKUP_PING_URL", len(env), env, len(want))
	}
	clean := stripYAMLComments(m)
	for _, forbidden := range []string{"envFrom:", "BACKUP_PING_URL"} {
		if strings.Contains(clean, forbidden) {
			t.Errorf("55-heartbeat.yaml names %q outside a comment", forbidden)
		}
	}
	base := configMapValues(t, readK8s(t, "05-config.yaml"))["TAPPA_BASE_URL"]
	if !strings.HasPrefix(base, "https://") {
		t.Fatalf("tappa-config's TAPPA_BASE_URL = %q is not https; the probe would not cover TLS", base)
	}
	host := strings.TrimPrefix(base, "https://")
	if strings.Contains(heartbeatScript(t), host) {
		t.Errorf("the heartbeat script spells the canonical host %q itself; it must read TAPPA_BASE_URL, or the "+
			"two drift apart on the next domain change", host)
	}
}

// TestHeartbeat_PodStaysOutsideTheDatabaseAllowSet: 12-networkpolicy.yaml admits pods by
// label to Postgres' 5432. The heartbeat never talks to the database, so its pod must not
// carry that label set. CONTROL: the backup pod, which must reach the database, does.
func TestHeartbeat_PodStaysOutsideTheDatabaseAllowSet(t *testing.T) {
	t.Parallel()
	admitted := labelsUnder(t, readK8s(t, "12-networkpolicy.yaml"), "ingress:", "matchLabels:")
	superset := func(pod map[string]string) bool {
		for k, v := range admitted {
			if pod[k] != v {
				return false
			}
		}
		return true
	}
	if hb := labelsUnder(t, readK8s(t, "55-heartbeat.yaml"), "template:", "metadata:", "labels:"); superset(hb) {
		t.Errorf("the heartbeat pod's labels %v include the database allow-set %v; a pod that only talks to "+
			"the internet would be admitted to 5432", hb, admitted)
	}
	if bk := labelsUnder(t, readK8s(t, "50-backup.yaml"), "template:", "metadata:", "labels:"); !superset(bk) {
		t.Errorf("CONTROL FAILED: the backup pod's labels %v do not include the allow-set %v — either the parse "+
			"is blind or the backup can no longer reach the database", bk, admitted)
	}
}

// TestCronJobs_PodsAreLockedDown: the operator-applied CronJobs are not checked by any
// deploy gate, so their pod hardening is pinned here — per pod (no ServiceAccount token,
// non-root numeric uid, RuntimeDefault seccomp) and per container (no escalation,
// read-only root, every capability dropped, limits) — and the host-sharing switches,
// including shareProcessNamespace (it would show one container's argv to the others), are
// absent.
//
// 🔴 IT IS ALSO THE ADMISSION CONTRACT. 00-namespace.yaml enforces PodSecurity
// `restricted`, and the orchestrator measured on 2026-10-09 that a pod in that namespace
// is admitted only with it. restricted's pod-level demands (runAsNonRoot, a non-zero uid,
// seccomp RuntimeDefault, no host namespaces, no hostPath/hostPort, no sysctls/procMount)
// and container-level ones (allowPrivilegeEscalation false, capabilities drop ALL) are each
// a line below — so a template the API server would refuse is red here, not at the first
// 02:30 or the first five-minute slot. The namespace label is asserted too: if it ever
// stops being restricted, this test's reason changes and should be re-read.
func TestCronJobs_PodsAreLockedDown(t *testing.T) {
	t.Parallel()
	if ns := stripYAMLComments(readK8s(t, "00-namespace.yaml")); !strings.Contains(ns, "pod-security.kubernetes.io/enforce: restricted") {
		t.Error("00-namespace.yaml no longer enforces PodSecurity restricted; the contract this test mirrors moved")
	}
	for _, file := range []string{"50-backup.yaml", "55-heartbeat.yaml"} {
		src := readK8s(t, file)
		clean := stripYAMLComments(src)
		containers := len(podContainerNames(t, src))
		once := []string{"automountServiceAccountToken: false", "runAsNonRoot: true", "type: RuntimeDefault"}
		for _, s := range once {
			if n := strings.Count(clean, s); n != 1 {
				t.Errorf("%s: %q occurs %d times, want once (pod level)", file, s, n)
			}
		}
		uid, err := strconv.Atoi(scalarOnce(t, src, "runAsUser"))
		if err != nil || uid == 0 {
			t.Errorf("%s: runAsUser = %d (%v), want a non-zero numeric uid", file, uid, err)
		}
		perContainer := map[string]int{
			"allowPrivilegeEscalation: false": 0,
			"readOnlyRootFilesystem: true":    0,
			`drop: ["ALL"]`:                   0,
		}
		limits := 0
		for _, line := range strings.Split(clean, "\n") {
			txt := strings.TrimSpace(line)
			if _, ok := perContainer[txt]; ok {
				perContainer[txt]++
			}
			if strings.HasPrefix(txt, "limits:") && strings.Contains(txt, "cpu") && strings.Contains(txt, "memory") {
				limits++
			}
		}
		for s, n := range perContainer {
			if n != containers {
				t.Errorf("%s: %q on %d of %d containers", file, s, n, containers)
			}
		}
		if limits != containers {
			t.Errorf("%s: cpu+memory limits on %d of %d containers", file, limits, containers)
		}
		for _, forbidden := range []string{"hostNetwork:", "hostPID:", "hostIPC:", "shareProcessNamespace:", "privileged:",
			"hostPath:", "hostPort:", "procMount:", "sysctls:", "add:", "envFrom:"} {
			if strings.Contains(clean, forbidden) {
				t.Errorf("%s: names %q", file, forbidden)
			}
		}
	}
}

// xtraceGuard is the line that switches tracing off before a signal URL is expanded — the
// first line of the heartbeat script and of pg-backup-ship.sh's alert_ping
// (scripts/rotate-kek.sh's precedent, softened: a backup must not refuse over its alarm).
const xtraceGuard = "case $- in *x*) set +x ;; esac"

// xtraceOn matches a script line that turns tracing ON (`set -x`, `set -eux`, `set -o xtrace`).
var xtraceOn = regexp.MustCompile(`(?m)^\s*set\s+(-[a-zA-Z]*x|-o\s+xtrace)`)

// commandArrays returns every `command:` list of a manifest, in either YAML style the
// manifests use: flow (`command: ["/bin/sh", "/scripts/x.sh"]`) and block (`- /bin/sh`,
// `- -c`, `- |`). A block scalar's body is not an item and is not returned.
func commandArrays(t *testing.T, manifest string) [][]string {
	t.Helper()
	lines := strings.Split(stripYAMLComments(manifest), "\n")
	var out [][]string
	for i, line := range lines {
		txt := strings.TrimSpace(line)
		if !strings.HasPrefix(txt, "command:") {
			continue
		}
		if rest := strings.TrimSpace(strings.TrimPrefix(txt, "command:")); rest != "" {
			var items []string
			for _, it := range strings.Split(strings.Trim(rest, "[]"), ",") {
				items = append(items, unquote(strings.TrimSpace(it)))
			}
			out = append(out, items)
			continue
		}
		base, itemIndent := indentOf(line), -1
		var items []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				continue
			}
			ind := indentOf(next)
			if ind <= base {
				break
			}
			if itemIndent < 0 {
				itemIndent = ind
			}
			if ind == itemIndent && strings.HasPrefix(strings.TrimSpace(next), "- ") {
				items = append(items, unquote(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(next), "- "))))
			} else if ind < itemIndent {
				break
			}
		}
		out = append(out, items)
	}
	return out
}

// tracingArgs returns the shell arguments of a command list that would turn xtrace on:
// an option cluster carrying x (`-x`, `-xc`, `-ceux`) or `-o xtrace`.
func tracingArgs(items []string) []string {
	var bad []string
	for i, it := range items {
		if regexp.MustCompile(`^-[a-zA-Z]*x[a-zA-Z]*$`).MatchString(it) ||
			(it == "-o" && i+1 < len(items) && items[i+1] == "xtrace") {
			bad = append(bad, it)
		}
	}
	return bad
}

// TestHeartbeat_ScriptParses: `sh -n` on the extracted script; its FIRST line is the
// xtrace guard; nothing in it turns tracing on; and no command list of either CronJob
// starts its shell traced (`- -c` drifting to `- -xc`). CONTROL: the command-list parser
// finds the three lists of 50-backup.yaml and the heartbeat's `-c`.
func TestHeartbeat_ScriptParses(t *testing.T) {
	t.Parallel()
	script := heartbeatScript(t)
	f := filepath.Join(t.TempDir(), "heartbeat.sh")
	if err := os.WriteFile(f, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("sh", "-n", f).CombinedOutput(); err != nil {
		t.Errorf("sh -n on the heartbeat script: %v\n%s", err, out)
	}
	if first, _, _ := strings.Cut(script, "\n"); strings.TrimSpace(first) != xtraceGuard {
		t.Errorf("the heartbeat script's first line is %q, want %q: the guard must run before the signal URL "+
			"is ever expanded", first, xtraceGuard)
	}
	if xtraceOn.MatchString(script) {
		t.Error("the heartbeat script turns on xtrace, which prints every expanded word — the signal URL included")
	}
	lists := 0
	for _, file := range []string{"50-backup.yaml", "55-heartbeat.yaml"} {
		for _, items := range commandArrays(t, readK8s(t, file)) {
			lists++
			if bad := tracingArgs(items); len(bad) > 0 {
				t.Errorf("%s: command %v starts its shell traced (%v)", file, items, bad)
			}
		}
	}
	if lists != 4 {
		t.Errorf("CONTROL FAILED: found %d command lists in 50-backup.yaml + 55-heartbeat.yaml, want 4", lists)
	}
	hb := commandArrays(t, readK8s(t, "55-heartbeat.yaml"))
	if len(hb) != 1 || strings.Join(hb[0], " ") != "/bin/sh -c |" {
		t.Errorf("CONTROL FAILED: the heartbeat's command list parsed as %v, want [/bin/sh -c |]", hb)
	}
	// The rule's own table: a mutation of tracingArgs would otherwise pass unseen.
	for _, tc := range []struct {
		items []string
		want  int
	}{
		{[]string{"/bin/sh", "-c", "|"}, 0},
		{[]string{"sh", "-ceu", "|"}, 0},
		{[]string{"/bin/sh", "-xc", "|"}, 1},
		{[]string{"/bin/sh", "-x", "/scripts/a.sh"}, 1},
		{[]string{"sh", "-ceux", "|"}, 1},
		{[]string{"sh", "-o", "xtrace", "-c", "|"}, 1},
	} {
		if got := len(tracingArgs(tc.items)); got != tc.want {
			t.Errorf("tracingArgs(%v) = %d findings, want %d", tc.items, got, tc.want)
		}
	}
}

// fakeEndpoint is a TLS server that answers a fixed status and records the paths it saw.
type fakeEndpoint struct {
	srv   *httptest.Server
	mu    sync.Mutex
	paths []string
}

func newFakeEndpoint(t *testing.T, status int) *fakeEndpoint {
	t.Helper()
	return newFakeEndpointRetryAfter(t, status, "")
}

// newFakeEndpointRetryAfter also sends `Retry-After: <retryAfter>` when it is not empty.
func newFakeEndpointRetryAfter(t *testing.T, status int, retryAfter string) *fakeEndpoint {
	t.Helper()
	f := &fakeEndpoint{}
	f.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.paths = append(f.paths, r.URL.Path)
		f.mu.Unlock()
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		w.WriteHeader(status)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeEndpoint) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

// closedURL is an https URL on a port nothing listens on.
func closedURL(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return "https://" + addr
}

// caBundle writes the fake servers' certificates where curl's CURL_CA_BUNDLE reads them.
func caBundle(t *testing.T, eps ...*fakeEndpoint) string {
	t.Helper()
	var b bytes.Buffer
	for _, e := range eps {
		b.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: e.srv.Certificate().Raw}))
	}
	f := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(f, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func hostCurl(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("curl")
	if err != nil {
		t.Fatalf("these tests run the scripts against real HTTP and need curl on PATH: %v", err)
	}
	return p
}

func writeExec(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func countPaths(paths []string, want string) int {
	n := 0
	for _, p := range paths {
		if p == want {
			n++
		}
	}
	return n
}

// heartbeatCase is one run of the extracted heartbeat script against two local TLS
// endpoints standing in for taptime.mt and hc-ping.com.
type heartbeatCase struct {
	name           string
	appStatus      int    // 0: the app is unreachable
	appRetryAfter  string // a Retry-After the app sends with its status
	baseScheme     string // "" = https
	pingStatus     int
	pingRetryAfter string
	pingURL        string // "" = the fake endpoint + marker; "-" = unset
	traced         bool   // run as `sh -xc` (the xtrace guard must switch it off)
	wantExit       string // "0" or "nonzero"
	wantAlive      int    // requests to the bare signal path (min; exact when retry cannot fire)
	wantFail       int
	noRequests     bool
	within         time.Duration // 0: no bound on the wall-clock time
}

// runHeartbeatCase runs the case with the host's sh and curl. A curl shim on PATH records
// every argv; the signal URL's secret part must appear in no argv, no stdout and no stderr
// — and the shim's own record of the PROBE's argv is the control that it saw the calls.
func runHeartbeatCase(t *testing.T, script, realCurl string, tc heartbeatCase) {
	t.Helper()
	signal, failSignal := "/"+pingMarker, "/"+pingMarker+"/fail"
	app := newFakeEndpointRetryAfter(t, tc.appStatus, tc.appRetryAfter)
	ping := newFakeEndpointRetryAfter(t, tc.pingStatus, tc.pingRetryAfter)
	dir := t.TempDir()
	argvLog := filepath.Join(dir, "argv.log")
	shim := filepath.Join(dir, "bin")
	if err := os.Mkdir(shim, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(shim, "curl"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \""+argvLog+"\"\nexec \""+realCurl+"\" \"$@\"\n")

	base := app.srv.URL
	if tc.appStatus == 0 {
		base = closedURL(t)
	}
	if tc.baseScheme == "http" {
		base = "http://" + strings.TrimPrefix(base, "https://")
	}
	env := []string{
		"PATH=" + shim + string(os.PathListSeparator) + os.Getenv("PATH"),
		"CURL_CA_BUNDLE=" + caBundle(t, app, ping),
		"TAPPA_BASE_URL=" + base,
	}
	switch tc.pingURL {
	case "":
		env = append(env, "UPTIME_PING_URL="+ping.srv.URL+"/"+pingMarker)
	case "-":
	case "NEWLINE":
		env = append(env, "UPTIME_PING_URL="+ping.srv.URL+"/"+pingMarker+"\n")
	default:
		env = append(env, "UPTIME_PING_URL="+tc.pingURL)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	flags := "-c"
	if tc.traced {
		flags = "-xc"
	}
	cmd := exec.CommandContext(ctx, "sh", flags, script)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running the heartbeat script: %v", err)
	}
	out := stdout.String() + stderr.String()
	switch {
	case tc.wantExit == "0" && code != 0:
		t.Errorf("exit %d, want 0\n%s", code, out)
	case tc.wantExit == "nonzero" && code == 0:
		t.Errorf("exit 0, want non-zero\n%s", out)
	}
	if tc.within > 0 && elapsed > tc.within {
		t.Errorf("the run took %v, want under %v: a server's Retry-After is deciding how long the heartbeat "+
			"waits, and activeDeadlineSeconds will kill it before its signal leaves", elapsed.Round(100*time.Millisecond), tc.within)
	}

	seen := ping.seen()
	if tc.noRequests {
		if len(seen) != 0 || len(app.seen()) != 0 {
			t.Errorf("requests were sent (signal %v, app %v); want none", seen, app.seen())
		}
	} else {
		alive, fail := countPaths(seen, signal), countPaths(seen, failSignal)
		if alive < tc.wantAlive || (tc.wantAlive == 0 && alive != 0) {
			t.Errorf("alive signals = %d, want %d (seen %v)", alive, tc.wantAlive, seen)
		}
		if fail < tc.wantFail || (tc.wantFail == 0 && fail != 0) {
			t.Errorf("fail signals = %d, want %d (seen %v)", fail, tc.wantFail, seen)
		}
		if tc.wantAlive == 1 && tc.pingStatus == 200 && alive != 1 {
			t.Errorf("alive signals = %d, want exactly 1 for one healthy moment", alive)
		}
		if alive+fail != len(seen) {
			t.Errorf("the signal endpoint saw paths that are neither alive nor fail: %v", seen)
		}
	}

	if strings.Contains(out, pingMarker) {
		t.Errorf("the signal URL's secret part reached the pod log:\n%s", out)
	}
	argv, _ := os.ReadFile(argvLog)
	if strings.Contains(string(argv), pingMarker) {
		t.Errorf("the signal URL's secret part reached a curl argv:\n%s", argv)
	}
	// CONTROL: the shim saw the probe's argv whenever a probe was made.
	if !tc.noRequests && tc.baseScheme == "" && !strings.Contains(string(argv), "/healthz") {
		t.Errorf("CONTROL FAILED: the curl shim recorded no probe argv (%q); the argv check is blind", argv)
	}
	// CONTROL: a traced run really was traced until the guard switched it off.
	if tc.traced && !strings.Contains(out, "set +x") {
		t.Errorf("CONTROL FAILED: a run started with `sh -xc` shows no trace of the guard's `set +x`; the "+
			"xtrace check is blind\n%s", out)
	}
}

// TestHeartbeat_TellsUpAsAliveAndDownAsFail: 200 is told as the bare signal URL, anything
// else as "/fail" with a non-zero exit; a missing, http or whitespace-carrying signal URL
// sends nothing. A Retry-After from the server cannot stretch the run (--retry-max-time).
func TestHeartbeat_TellsUpAsAliveAndDownAsFail(t *testing.T) {
	t.Parallel()
	script := heartbeatScript(t)
	realCurl := hostCurl(t)
	for _, tc := range []heartbeatCase{
		{name: "healthz 200: one alive signal, exit 0", appStatus: 200, pingStatus: 200, wantExit: "0", wantAlive: 1},
		{name: "healthz 503: fail signal, non-zero", appStatus: 503, pingStatus: 200, wantExit: "nonzero", wantFail: 1},
		{name: "healthz 500: fail signal, non-zero", appStatus: 500, pingStatus: 200, wantExit: "nonzero", wantFail: 1},
		{name: "app unreachable: fail signal, non-zero", appStatus: 0, pingStatus: 200, wantExit: "nonzero", wantFail: 1},
		{name: "base URL is not https: nothing probed, fail signal", appStatus: 200, baseScheme: "http", pingStatus: 200, wantExit: "nonzero", wantFail: 1},
		{name: "signal endpoint answers 500: non-zero, never a fail signal", appStatus: 200, pingStatus: 500, wantExit: "nonzero", wantAlive: 1},
		{name: "no signal URL: nothing is sent at all", appStatus: 200, pingStatus: 200, pingURL: "-", wantExit: "nonzero", noRequests: true},
		{name: "signal URL is http: refused, nothing sent", appStatus: 200, pingStatus: 200, pingURL: "http://127.0.0.1:9/" + pingMarker, wantExit: "nonzero", noRequests: true},
		{name: "signal URL carries a pasted newline: refused, nothing sent", appStatus: 200, pingStatus: 200, pingURL: "NEWLINE", wantExit: "nonzero", noRequests: true},
		{name: "healthz 503 with Retry-After 50: fail signal in seconds, not 50", appStatus: 503, appRetryAfter: "50", pingStatus: 200, wantExit: "nonzero", wantFail: 1, within: 20 * time.Second},
		{name: "signal 503 with Retry-After 50: non-zero in seconds, not 50", appStatus: 200, pingStatus: 503, pingRetryAfter: "50", wantExit: "nonzero", wantAlive: 1, within: 20 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runHeartbeatCase(t, script, realCurl, tc)
		})
	}
}

// TestHeartbeat_XtraceCannotPrintTheSignalURL runs the script as `sh -xc` — a manifest edit
// away (`- -c` → `- -xc`) — and the URL must still reach no output. Measured in
// curlimages/curl:8.22.0 without the guard: three trace lines carried it (the
// `ping_url=` assignment, `target=`, and the `echo 'url = …'` fed to curl); with it, none.
// The guard is the script's first line (TestHeartbeat_ScriptParses).
func TestHeartbeat_XtraceCannotPrintTheSignalURL(t *testing.T) {
	t.Parallel()
	script := heartbeatScript(t)
	realCurl := hostCurl(t)
	for _, tc := range []heartbeatCase{
		{name: "traced, up", traced: true, appStatus: 200, pingStatus: 200, wantExit: "0", wantAlive: 1},
		{name: "traced, down", traced: true, appStatus: 503, pingStatus: 200, wantExit: "nonzero", wantFail: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runHeartbeatCase(t, script, realCurl, tc)
		})
	}
}

// ------------------------------------------------------------------ the backup --

// TestBackup_TheSignalURLIsOptionalAndOnlyTheShipContainerHasIt.
//
// OPTIONAL, the opposite of the heartbeat's: a required key would stop the kubelet from
// creating the ship container when secret/tappa-alert-pings is missing, i.e. the backup
// would fail because its alarm is not configured. Only `ship` gets it, from that Secret,
// under that key; the dump never sees it, and the backup never sees the uptime URL.
func TestBackup_TheSignalURLIsOptionalAndOnlyTheShipContainerHasIt(t *testing.T) {
	t.Parallel()
	m := readK8s(t, "50-backup.yaml")
	ship := envSourcesOf(t, m, "ship")
	want := envSource{kind: "secretKeyRef", ref: "tappa-alert-pings", key: "BACKUP_PING_URL", optional: true}
	if got := ship["BACKUP_PING_URL"]; got != want {
		t.Errorf("ship env BACKUP_PING_URL = %+v, want %+v", got, want)
	}
	// CONTROL: the parse reads `optional` per entry — BACKUP_REMOTE, a few lines above, is
	// required, so a parser that called everything optional is caught here.
	if got := ship["BACKUP_REMOTE"]; got.kind != "secretKeyRef" || got.optional {
		t.Errorf("CONTROL FAILED: ship env BACKUP_REMOTE = %+v, want a required secretKeyRef", got)
	}
	for _, c := range []string{"dump-and-verify", "wait-for-postgres"} {
		for name, e := range envSourcesOf(t, m, c) {
			if e.ref == "tappa-alert-pings" || name == "BACKUP_PING_URL" {
				t.Errorf("container %s reads %s from %q; only ship sends the signal", c, name, e.ref)
			}
		}
	}
	if strings.Contains(stripYAMLComments(m), "UPTIME_PING_URL") {
		t.Error("50-backup.yaml names UPTIME_PING_URL outside a comment; the backup could then report the uptime check")
	}
}

// shipStubs writes the three commands the ship script reaches for, PATH-first:
//
//   - rclone: a crypt remote that proves itself encrypted and accepts every write, except
//     the dump's upload when STUB_FAIL_UPLOAD=1;
//   - wget: LEAKY ON PURPOSE — it prints the full URL on stdout AND stderr before doing
//     the request with curl, then speaks busybox's own error sentences (measured);
//   - timeout: drops its duration and runs the rest (busybox's 143 is the card's).
//
// With STUB_WGET_LINGER=<file> the wget stub plays the measured busybox shape instead: it
// leaves a child (`sleep 20 &`) that INHERITS its stderr and outlives it — busybox wget's
// ssl_client does exactly that — writes the child's pid to <file>, says "download timed
// out" and exits 1.
func shipStubs(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeExec(t, filepath.Join(dir, "rclone"), `#!/bin/sh
case "$1" in
  config) printf '[%s]\ntype = crypt\nremote = under:base\n' "$3" ;;
  cryptdecode) printf '%s\tqvopaquestoredname\n' "$4" ;;
  cat) printf 'RCLONE\000\000' ;;
  copyto)
    case "$3" in
      *.sql.gz) if [ "${STUB_FAIL_UPLOAD:-}" = 1 ]; then echo "stub rclone: upload refused" >&2; exit 1; fi ;;
    esac ;;
  lsf) echo "20261009T023000Z/" ;;
esac
exit 0
`)
	writeExec(t, filepath.Join(dir, "wget"), `#!/bin/sh
for a in "$@"; do url="$a"; done
echo "stub wget fetching $url"
echo "stub wget: GET $url" >&2
if [ -n "${STUB_WGET_LINGER:-}" ]; then
  sleep 20 &
  echo $! > "$STUB_WGET_LINGER"
  echo "wget: download timed out" >&2
  exit 1
fi
code=$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 "$url")
if [ $? -ne 0 ]; then
  echo "wget: can't connect to remote host (127.0.0.1): Connection refused" >&2
  exit 1
fi
case "$code" in 2??) exit 0 ;; esac
echo "wget: server returned error: HTTP/1.1 $code Whatever" >&2
exit 1
`)
	writeExec(t, filepath.Join(dir, "timeout"), "#!/bin/sh\nshift\nexec \"$@\"\n")
	return dir
}

// runShip runs the REAL scripts/pg-backup-ship.sh with the stubs, a staged dump and the
// given extra environment; it returns the exit code and everything the script printed.
func runShip(t *testing.T, stubs, ca string, extra ...string) (int, string) {
	t.Helper()
	return runShipWith(t, nil, stubs, ca, extra...)
}

// runShipWith is runShip with flags for the shell itself (`-x`).
func runShipWith(t *testing.T, shFlags []string, stubs, ca string, extra ...string) (int, string) {
	t.Helper()
	dir := t.TempDir()
	stage := filepath.Join(dir, "staging")
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{
		filepath.Join(stage, "tappa-20261009T023000Z.sql.gz"):   "not really gzip",
		filepath.Join(stage, "tappa-20261009T023000Z.manifest"): "sha256 0\n",
		filepath.Join(dir, "rclone.conf"):                       "[tappa-backup]\ntype = crypt\n",
	} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tmp := filepath.Join(dir, "tmp")
	if err := os.Mkdir(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	env := []string{
		"PATH=" + stubs + string(os.PathListSeparator) + os.Getenv("PATH"),
		"CURL_CA_BUNDLE=" + ca,
		"BACKUP_STAGE_DIR=" + stage,
		"RCLONE_CONFIG=" + filepath.Join(dir, "rclone.conf"),
		"BACKUP_REMOTE=tappa-backup:prod",
		"BACKUP_RETENTION_DAYS=30",
		"TMPDIR=" + tmp,
	}
	for _, e := range extra {
		if name, ok := strings.CutSuffix(e, "=<unset>"); ok {
			kept := env[:0]
			for _, have := range env {
				if !strings.HasPrefix(have, name+"=") {
					kept = append(kept, have)
				}
			}
			env = kept
			continue
		}
		env = append(env, e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	args := append(append([]string(nil), shFlags...), scriptPathIn(t, "pg-backup-ship.sh"))
	cmd := exec.CommandContext(ctx, "sh", args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), string(out)
	} else if err != nil {
		t.Fatalf("running pg-backup-ship.sh: %v", err)
	}
	return 0, string(out)
}

// TestBackupShip_TheSignalNeverChangesTheOutcome runs the real ship script, every case
// TWICE: once with the signal configured as the case says, once with BACKUP_PING_URL
// unset. The two exit codes must be equal — that equality IS "the backup never depends on
// its alarm" — and the signal endpoint must have been told exactly what happened.
func TestBackupShip_TheSignalNeverChangesTheOutcome(t *testing.T) {
	t.Parallel()
	hostCurl(t)
	stubs := shipStubs(t)
	signal, failSignal := "/"+pingMarker, "/"+pingMarker+"/fail"

	for _, tc := range []struct {
		name       string
		pingStatus int    // 0: unreachable
		pingURL    string // "" = the endpoint + marker; "-" = unset
		extra      []string
		// -1: only "equal to the unsignalled twin". The one such row is `${X:?}` aborting the
		// script: busybox, dash and bash 5 end 2/2/1, but macOS's bash 3.2 (this repository's
		// /bin/sh on a developer machine) ends 0 when an EXIT trap is set — measured, and true
		// of the script BEFORE T116 too. The exit status is the shell's; what this row pins is
		// that the signal is "/fail" even where the status lies (ship_complete).
		wantExit  int
		wantPaths []string
		wantClass string // a fragment the single "alert signal" line must carry
	}{
		{name: "shipped: alive signal", pingStatus: 200, wantExit: 0, wantPaths: []string{signal}, wantClass: "sent (success"},
		{name: "shipped, Secret absent: no signal, still green", pingStatus: 200, pingURL: "-", wantExit: 0, wantClass: "skipped"},
		{name: "shipped, endpoint 500: still green, one warning", pingStatus: 500, wantExit: 0, wantPaths: []string{signal}, wantClass: "HTTP 5xx"},
		{name: "shipped, endpoint 404: still green, one warning", pingStatus: 404, wantExit: 0, wantPaths: []string{signal}, wantClass: "HTTP 4xx"},
		{name: "shipped, endpoint unreachable: still green, one warning", pingStatus: 0, wantExit: 0, wantClass: "connect"},
		{name: "shipped, URL is http: refused, still green", pingStatus: 200, pingURL: "http://127.0.0.1:9/" + pingMarker, wantExit: 0, wantClass: "not an https URL"},
		{name: "upload failed: fail signal, red", pingStatus: 200, extra: []string{"STUB_FAIL_UPLOAD=1"}, wantExit: 1, wantPaths: []string{failSignal}, wantClass: "sent (failure"},
		{name: "upload failed, endpoint 500: still the same red", pingStatus: 500, extra: []string{"STUB_FAIL_UPLOAD=1"}, wantExit: 1, wantPaths: []string{failSignal}, wantClass: "HTTP 5xx"},
		{name: "config missing (set -u abort): fail signal", pingStatus: 200, extra: []string{"RCLONE_CONFIG=<unset>"}, wantExit: -1, wantPaths: []string{failSignal}, wantClass: "sent (failure"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ep := newFakeEndpoint(t, tc.pingStatus)
			ca := caBundle(t, ep)
			url := ep.srv.URL
			if tc.pingStatus == 0 {
				url = closedURL(t)
			}
			args := append([]string(nil), tc.extra...)
			switch tc.pingURL {
			case "":
				args = append(args, "BACKUP_PING_URL="+url+"/"+pingMarker)
			case "-":
			default:
				args = append(args, "BACKUP_PING_URL="+tc.pingURL)
			}
			code, out := runShip(t, stubs, ca, args...)
			twin, twinOut := runShip(t, stubs, ca, tc.extra...)

			if code != twin {
				t.Errorf("exit %d with the signal, %d without it: the alarm changed the backup's outcome\n%s\n--- twin:\n%s",
					code, twin, out, twinOut)
			}
			if tc.wantExit >= 0 && code != tc.wantExit {
				t.Errorf("exit %d, want %d\n%s", code, tc.wantExit, out)
			}
			if got := ep.seen(); strings.Join(got, ",") != strings.Join(tc.wantPaths, ",") {
				t.Errorf("signal endpoint saw %v, want %v", got, tc.wantPaths)
			}
			var lines []string
			for _, l := range strings.Split(out, "\n") {
				if strings.Contains(l, "alert signal") {
					lines = append(lines, l)
				}
			}
			if len(lines) != 1 || !strings.Contains(lines[0], tc.wantClass) {
				t.Errorf("want exactly one 'alert signal' line carrying %q, got %d: %q", tc.wantClass, len(lines), lines)
			}
			if strings.Contains(out, pingMarker) {
				t.Errorf("the signal URL's secret part reached the job log (the stub wget prints it on both streams; "+
					"the script must print neither):\n%s", out)
			}
			// The output contract of a successful run is unchanged: its last line before the
			// signal is still the retained count.
			if tc.wantExit == 0 && !strings.Contains(out, "pg-backup-ship: done: 1 backup(s) retained at the destination") {
				t.Errorf("a green run no longer reports the retained count:\n%s", out)
			}
		})
	}
}

// TestBackupShip_XtraceCannotPrintTheSignalURL runs the real ship script as `sh -x` — one
// manifest edit away (`["/bin/sh", "-x", …]`) — and the URL must reach no output. Measured
// in rclone/rclone:1.71 without the guard: three trace lines of a green run carried it (the
// assignment, the `[ -z` test, the wget command) and a fourth on a red one (the "/fail"
// assignment); with it, none. The guard is alert_ping's first line, BEFORE the URL is
// expanded; the structural half pins that order, the run pins the effect.
func TestBackupShip_XtraceCannotPrintTheSignalURL(t *testing.T) {
	t.Parallel()
	hostCurl(t)
	code := codeOf(readScript(t, "pg-backup-ship.sh"))
	if xtraceOn.MatchString(code) {
		t.Error("scripts/pg-backup-ship.sh turns xtrace on")
	}
	at := strings.Index(code, "alert_ping() {")
	if at < 0 {
		t.Fatal("scripts/pg-backup-ship.sh no longer defines alert_ping")
	}
	fn := code[at:]
	guard, assign := strings.Index(fn, xtraceGuard), strings.Index(fn, `_url="${BACKUP_PING_URL:-}"`)
	if guard < 0 || assign < 0 || guard > assign {
		t.Errorf("alert_ping must switch xtrace off (%q) BEFORE it first expands BACKUP_PING_URL "+
			"(guard at %d, assignment at %d)", xtraceGuard, guard, assign)
	}

	stubs := shipStubs(t)
	for _, tc := range []struct {
		name  string
		extra []string
		want  string
	}{
		{name: "traced, shipped", want: "/" + pingMarker},
		{name: "traced, upload failed", extra: []string{"STUB_FAIL_UPLOAD=1"}, want: "/" + pingMarker + "/fail"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ep := newFakeEndpoint(t, 200)
			args := append([]string{"BACKUP_PING_URL=" + ep.srv.URL + "/" + pingMarker}, tc.extra...)
			_, out := runShipWith(t, []string{"-x"}, stubs, caBundle(t, ep), args...)
			if strings.Contains(out, pingMarker) {
				t.Errorf("under `sh -x` the signal URL's secret part reached the job log:\n%s", out)
			}
			if got := ep.seen(); len(got) != 1 || got[0] != tc.want {
				t.Errorf("signal endpoint saw %v, want [%s]: tracing must not cost the signal", got, tc.want)
			}
			// CONTROL: the run really was traced up to the guard.
			traced := false
			for _, l := range strings.Split(out, "\n") {
				if strings.HasPrefix(l, "+") {
					traced = true
					break
				}
			}
			if !traced || !strings.Contains(out, "set +x") {
				t.Errorf("CONTROL FAILED: no trace lines, or no trace of the guard's `set +x`; the check is blind\n%s", out)
			}
		})
	}
}

// TestBackupShip_ALingeringClientChildCannotHoldTheBackup is the measured busybox failure
// as a test: wget's TLS helper inherits its stderr and outlives it, so a script that reads
// that stderr through a pipe (`$(...)`) waits for the helper — measured 63 s against a
// server answering after 60 s, and without bound against one that never closes, i.e. a
// backup held until activeDeadlineSeconds fails it. The stub leaves a 20 s child holding
// its stderr; the run must still end green within 10 s, with one "timeout" line.
//
// NOT t.Parallel, on purpose: the assertion is a wall-clock bound. Alone the run takes
// ~1.2 s (measured, three runs); among this file's parallel shell-spawning subtests it took
// 7.2 s, too close to 10 s to stay honest. A sequential test runs before the package's
// parallel ones are released, so the bound measures the script, not the scheduler.
func TestBackupShip_ALingeringClientChildCannotHoldTheBackup(t *testing.T) {
	hostCurl(t)
	ep := newFakeEndpoint(t, 200)
	pidFile := filepath.Join(t.TempDir(), "linger.pid")
	t.Cleanup(func() {
		b, err := os.ReadFile(pidFile)
		if err != nil {
			return
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
		if err != nil {
			t.Logf("linger pid file holds %q: %v", b, err)
			return
		}
		p, err := os.FindProcess(pid)
		if err != nil {
			t.Logf("find lingering child %d: %v", pid, err)
			return
		}
		if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Logf("kill lingering child %d: %v", pid, err)
		}
	})
	start := time.Now()
	code, out := runShip(t, shipStubs(t), caBundle(t, ep),
		"BACKUP_PING_URL="+ep.srv.URL+"/"+pingMarker, "STUB_WGET_LINGER="+pidFile)
	elapsed := time.Since(start)
	if _, err := os.Stat(pidFile); err != nil {
		t.Fatalf("CONTROL FAILED: the stub never left its lingering child (%v); the test is blind", err)
	}
	if elapsed > 10*time.Second {
		t.Errorf("the run took %v with a child holding the client's stderr, want under 10 s: the script is "+
			"waiting on a pipe the alarm's client left open", elapsed.Round(100*time.Millisecond))
	}
	if code != 0 {
		t.Errorf("exit %d, want 0: a hung signal must not turn the backup red\n%s", code, out)
	}
	if n := strings.Count(out, "alert signal"); n != 1 || !strings.Contains(out, "NOT delivered (timeout") {
		t.Errorf("want exactly one 'alert signal ... NOT delivered (timeout' line, got %d:\n%s", n, out)
	}
	if strings.Contains(out, pingMarker) {
		t.Errorf("the signal URL's secret part reached the job log:\n%s", out)
	}
}
