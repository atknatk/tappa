package main

import (
	"net/url"
	"slices"
	"strings"
	"testing"
)

// verifyredirecthosts_test.go -- scripts/verify-deployment.sh's redirect-hosts mode (backlog T261),
// run for real through scriptRig (stub curl and sleep first on PATH, no network), and the deploy step
// that runs it.

// redirectGateTargets returns 43-redirect-ingress.yaml's hosts in file order and the host of the
// ConfigMap's TAPPA_BASE_URL: what the gate asks and where it wants them sent.
func redirectGateTargets(t *testing.T) (hosts []string, base string) {
	t.Helper()
	objs := ingressesIn(t, redirectIngressFile)
	if len(objs) != 1 {
		t.Fatalf("%s holds %d Ingress(es), want 1", redirectIngressFile, len(objs))
	}
	for _, r := range objs[0].rules {
		hosts = append(hosts, r.host)
	}
	u, err := url.Parse(configMapValues(t, readK8s(t, "05-config.yaml"))["TAPPA_BASE_URL"])
	if err != nil || u.Host == "" || len(hosts) == 0 {
		t.Fatalf("05-config.yaml's TAPPA_BASE_URL %v, 43's hosts %v", err, hosts)
	}
	return hosts, u.Host
}

// TestVerifyDeployment_TheRedirectHostsModeJudgesEveryAnswer: each host is asked for
// GET /activate?x=1 and passes only with a 301 to <base>/activate?x=1 and no cf-ray (the measured
// answer of www.taptime.mt, 2026-11-11). PART I, one host at a time: cf-ray and a 2xx are a VIOLATION
// at once; a 404 or a 3xx with another Location is asked again, 18 times in all, and is a VIOLATION
// when it never turns (the path-dropping ingress redirect's https://taptime.mt among them), a pass
// when it does; any other answer (a 503, no status line) is a VIOLATION at once; every curl failure
// -- a name that does not resolve included -- is UNCHECKED, exit 2. PART II: both hosts are asked
// whatever the first answered, and a VIOLATION outranks an UNCHECKED. PART III: with no arguments the
// mode asks exactly 43's hosts and wants the ConfigMap's base host, pausing 5 s between tries.
//
// MUTANTS (measured): the cf-ray check removed -> red; the 2xx case removed -> red; the Location
// comparison dropped (any 301 passes) -> red; a host that does not resolve passed (exit 0) -> red;
// `break` after the first host's violation -> red.
func TestVerifyDeployment_TheRedirectHostsModeJudgesEveryAnswer(t *testing.T) {
	hosts, base := redirectGateTargets(t)
	if len(hosts) < 2 {
		t.Fatalf("43 routes %v; this test needs its two hosts", hosts)
	}
	want := "https://" + base + "/activate?x=1"
	ask := func(h string) string { return "https://" + h + "/activate?x=1" }
	good := curlReply{"HTTP/2 301 \r\ncontent-type: text/html; charset=utf-8\r\nlocation: " + want +
		"\r\nstrict-transport-security: max-age=31536000; includeSubDomains\r\n\r\n", "0"}
	pathDropped := curlReply{"HTTP/2 301 \r\nlocation: https://" + base + "\r\n\r\n", "0"}
	notRouted := curlReply{"HTTP/2 404 \r\n\r\n", "0"}
	rig := newScriptRig(t)
	args := append([]string{"redirect-hosts", base}, hosts...)

	for _, tc := range []struct {
		name      string
		replies   []curlReply
		wantCode  int
		wantCalls int
		wantMark  string
	}{
		{"the application's redirect (measured)", []curlReply{good}, 0, 1, "301 to " + want + " (try 1)"},
		{"the same over HTTP/1.1", []curlReply{{"HTTP/1.1 301 Moved Permanently\r\nLocation: " + want + "\r\n\r\n", "0"}}, 0, 1, "(try 1)"},
		{"through Cloudflare", []curlReply{{good.headers[:len(good.headers)-2] + "server: cloudflare\r\ncf-ray: a5894f566f11ed9c-MXP\r\n\r\n", "0"}}, 1, 1, "THROUGH CLOUDFLARE"},
		{"a page instead of a redirect", []curlReply{{"HTTP/2 200 \r\ncontent-type: text/html\r\n\r\n", "0"}}, 1, 1, "SERVES A PAGE"},
		{"a 404, then the page", []curlReply{notRouted, {"HTTP/2 200 \r\n\r\n", "0"}}, 1, 2, "SERVES A PAGE"},
		{"the path dropped on every try", []curlReply{pathDropped}, 1, 18, "after 18 try(s)"},
		{"the path dropped, then kept", []curlReply{pathDropped, good}, 0, 2, "(try 2)"},
		{"not routed on every try", []curlReply{notRouted}, 1, 18, "'404' after 18"},
		{"not routed, then routed", []curlReply{notRouted, notRouted, good}, 0, 3, "(try 3)"},
		{"a 308 to the right place is not a GET's answer", []curlReply{{"HTTP/2 308 \r\nlocation: " + want + "\r\n\r\n", "0"}}, 1, 18, "'308'"},
		{"a 301 elsewhere", []curlReply{{"HTTP/2 301 \r\nlocation: https://evil.example/activate?x=1\r\n\r\n", "0"}}, 1, 18, "evil.example"},
		{"a 503 is final", []curlReply{{"HTTP/2 503 \r\n\r\n", "0"}}, 1, 1, "'503' after 1"},
		{"no status line", []curlReply{{"", "0"}}, 1, 1, "no status line"},
		{"the name does not resolve", []curlReply{{"curl: (6) Could not resolve host", "6"}}, 2, 1, "curl exit 6"},
		{"a timeout", []curlReply{{"curl: (28) Operation timed out", "28"}}, 2, 1, "NOT checked"},
	} {
		rig.curlAnswers(ask(hosts[0]), tc.replies...)
		rig.curlAnswers(ask(hosts[1]), good)
		stdout, stderr, code := rig.run(args...)
		if code != tc.wantCode || !strings.Contains(stdout+stderr, tc.wantMark) {
			t.Errorf("%s: exit %d; want exit %d and %q in\n%s%s", tc.name, code, tc.wantCode, tc.wantMark, stdout, stderr)
		}
		calls := rig.curlCalls()
		first := slices.DeleteFunc(slices.Clone(calls), func(c string) bool { return c != ask(hosts[0]) })
		if len(first) != tc.wantCalls {
			t.Errorf("%s: %s asked %d time(s), want %d", tc.name, ask(hosts[0]), len(first), tc.wantCalls)
		}
		if !slices.Contains(calls, ask(hosts[1])) || !strings.Contains(stdout, "redirect-hosts: "+hosts[1]) {
			t.Errorf("%s: the second host was not asked or did not pass (calls %v)\n%s", tc.name, calls, stdout)
		}
		if len(calls) != len(first)+1 {
			t.Errorf("%s: curl asked %v, want only the two hosts' /activate?x=1", tc.name, calls)
		}
	}

	// PART II: a VIOLATION on one host and an UNCHECKED on the other is exit 1, whatever the order.
	for _, order := range [][2]curlReply{
		{{"HTTP/2 200 \r\n\r\n", "0"}, {"curl: (28) Operation timed out", "28"}},
		{{"curl: (28) Operation timed out", "28"}, {"HTTP/2 200 \r\n\r\n", "0"}},
	} {
		rig.curlAnswers(ask(hosts[0]), order[0])
		rig.curlAnswers(ask(hosts[1]), order[1])
		if _, stderr, code := rig.run(args...); code != 1 || len(rig.curlCalls()) != 2 {
			t.Errorf("a violation and an unchecked host: exit %d after %v; want exit 1, both asked\n%s", code, rig.curlCalls(), stderr)
		}
	}

	// PART III: the defaults are 43's hosts and the ConfigMap's base host, 17 pauses of 5 s.
	for _, h := range hosts {
		rig.curlAnswers(ask(h), pathDropped)
	}
	if _, _, code := rig.run("redirect-hosts"); code != 1 {
		t.Errorf("with the defaults and the path dropped: exit %d, want 1", code)
	}
	got := slices.Compact(slices.Clone(rig.curlCalls()))
	wantCalls := []string{}
	for _, h := range hosts {
		wantCalls = append(wantCalls, ask(h))
	}
	if !slices.Equal(got, wantCalls) {
		t.Errorf("with no arguments the mode asked %v, want 43's hosts in order %v", got, wantCalls)
	}
	if sleeps := rig.sleepCalls(); len(sleeps) != 17*len(hosts) || slices.ContainsFunc(sleeps, func(a string) bool { return a != "5" }) {
		t.Errorf("with the defaults the probe paused %v; want 17 pauses of 5 (seconds) per host", sleeps)
	}
	for _, h := range hosts {
		rig.curlAnswers(ask(h), good)
	}
	if stdout, stderr, code := rig.run("redirect-hosts"); code != 0 {
		t.Errorf("with the defaults and the application's 301 to %s: exit %d, want 0 -- the default base host is not "+
			"the ConfigMap's\n%s%s", want, code, stdout, stderr)
	}
}

// TestPackaging_TheDeployGatesTheRedirectHosts (backlog T261): deploy.yml runs
// `verify-deployment.sh redirect-hosts <TAPPA_BASE_URL's host> <43's hosts>` exactly once, in a
// step after the one that applies 43-redirect-ingress.yaml (the gate measures the routed hosts),
// with neither an `if:` nor `continue-on-error` (a gate that can be skipped is not one).
//
// MUTANTS (measured): the step deleted -> red; moved ahead of the route step -> red; `if: always()`
// added -> red; a host dropped from its line -> red.
func TestPackaging_TheDeployGatesTheRedirectHosts(t *testing.T) {
	hosts, base := redirectGateTargets(t)
	steps := deploySteps(t)
	route := slices.IndexFunc(steps, func(s workflowStep) bool {
		return slices.ContainsFunc(s.code, func(l string) bool {
			return strings.TrimSpace(l) == "kubectl apply -f deploy/k8s/"+redirectIngressFile
		})
	})
	if route < 0 {
		t.Fatalf("CONTROL FAILED: no deploy.yml step applies %s", redirectIngressFile)
	}
	line := "run: scripts/verify-deployment.sh redirect-hosts " + base + " " + strings.Join(hosts, " ")
	gate := stepsWithLine(steps, line)
	if len(gate) != 1 {
		t.Fatalf("deploy.yml runs %q in %d step(s), want exactly one", line, len(gate))
	}
	if gate[0] < route {
		t.Errorf("the redirect-hosts gate (step %d) runs before %s is applied (step %d)", gate[0], redirectIngressFile, route)
	}
	for _, l := range steps[gate[0]].code {
		if tl := strings.TrimSpace(l); strings.HasPrefix(tl, "if:") || strings.HasPrefix(tl, "continue-on-error:") {
			t.Errorf("the redirect-hosts gate %q carries %q: a gate that can be skipped is not one", steps[gate[0]].name, tl)
		}
	}
}
