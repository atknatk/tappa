package main

// deployregistry_test.go — OPS-2: the deploy pushes to a PRIVATE Harbor project
// (registry.everva.com.tr/tappa), the pods pull with the operator's secret/harbor-pull, and
// every secret the deploy job reads comes from the GitHub `production` environment.
//
// THREAT MODEL, as in operatorsurface_test.go: these pins are against ACCIDENTAL drift — a
// registry name that moves in one file and not another, a gate deleted, moved or made
// skippable, an imagePullSecrets dropped, a secret that is no longer named by the preflight,
// a Docker Hub line left behind. Deliberate circumvention is code review's subject. Nothing
// here contacts Harbor, Docker Hub, GitHub or the cluster: the two gates run against a stub
// `docker` and the scriptRig's stub `kubectl`, and the preflight's body is RUN under bash
// with the environment a missing secret produces.

import (
	"encoding/base64"
	"errors"
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
	harborRegistry = "registry.everva.com.tr"
	harborProject  = harborRegistry + "/tappa"
	pullSecretName = "harbor-pull"

	deploySecretsStep = "Preflight — the production environment's secrets must exist"
	registryLoginStep = "Log in to the registry (Harbor, the CI robot)"
	registryLoginRun  = `run: printf '%s' "$HARBOR_CI_ROBOT_SECRET" | docker login "$REGISTRY" -u "$HARBOR_CI_ROBOT_NAME" --password-stdin`
	accessGateStep    = "Gate — both images must be pullable WITH the CI credential and NOT without one"
	accessGateRun     = `run: scripts/verify-image.sh --access "$APP_IMAGE:sha-$SHORT" "$MIGRATE_IMAGE:sha-$SHORT"`
	registryLogout    = "Log out of the registry"
	registryLogoutRun = `run: docker logout "$REGISTRY"`
	pullSecretStep    = "Preflight — the registry pull Secret is the operator's and must already exist"
	pullSecretRun     = `run: scripts/verify-deployment.sh pull-secret tappa harbor-pull "$REGISTRY"`
	appSecretStep     = "Preflight — the application Secret is the operator's and must already exist"
)

// deploySecrets are the only secrets deploy.yml may name, all of them environment secrets of
// `production` (OPS-2).
var deploySecrets = []string{"HARBOR_CI_ROBOT_NAME", "HARBOR_CI_ROBOT_SECRET", "TS_OAUTH_CLIENT_ID", "TS_OAUTH_SECRET", "KUBE_CONFIG"}

// stepIndex returns the index of the step named name, or fails.
func stepIndex(t *testing.T, steps []workflowStep, name string) int {
	t.Helper()
	i := slices.IndexFunc(steps, func(s workflowStep) bool { return s.name == name })
	if i < 0 {
		t.Fatalf("deploy.yml has no step %q", name)
	}
	return i
}

// trimmedCode returns a step's non-blank code lines, trimmed.
func trimmedCode(s workflowStep) []string {
	var out []string
	for _, l := range s.code {
		if tl := strings.TrimSpace(l); tl != "" {
			out = append(out, tl)
		}
	}
	return out
}

// TestPackaging_TheDeployPushesToThePrivateRegistry pins OPS-2's deploy shape:
//
//   - the job runs in `environment: production` (once, at the job's level), and the only secrets
//     the file names are deploySecrets;
//   - REGISTRY, APP_IMAGE and MIGRATE_IMAGE are the Harbor project's, once each; no code line of
//     deploy.yml, the Dockerfile or any manifest names docker.io/atknatk, and deploy.yml's code
//     names no DOCKERHUB_ secret, no TS_AUTHKEY and no Docker Hub login;
//   - the first step is the secrets preflight; the registry login (the CI robot, the secret piped)
//     comes before "Build both images"; the access gate is the step right after "Push", and the
//     logout the step right after the gate, both before "Install kubectl" and every cluster call;
//   - the pull-Secret preflight is the step right after the application Secret's, before the
//     operator-surface preflight and every apply but the namespace's;
//   - none of those steps carries an `if:` or `continue-on-error`.
//
// MUTANT (measured, OPS-2): `environment: production` deleted; APP_IMAGE set back to
// docker.io/atknatk/tappa; the access gate moved after "Install kubectl"; the logout deleted; the
// pull-Secret preflight moved after "Apply configuration and database"; `if: always()` on the
// access gate; a `DOCKERHUB_TOKEN` secret added to the login -> each red.
func TestPackaging_TheDeployPushesToThePrivateRegistry(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "deploy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	code := codeLinesOf(string(b))
	if n := countLines(code, "    environment: production"); n != 1 {
		t.Errorf("deploy.yml carries `environment: production` at the job's level %d time(s), want once: every secret "+
			"the job reads comes from that environment (backlog T135 (4), OPS-2)", n)
	}
	for _, want := range []string{"REGISTRY: " + harborRegistry, "APP_IMAGE: " + harborProject + "/tappa",
		"MIGRATE_IMAGE: " + harborProject + "/tappa-migrate"} {
		if n := countTrimmed(code, want); n != 1 {
			t.Errorf("deploy.yml sets %q %d time(s), want once (the job's env)", want, n)
		}
	}
	named := map[string]bool{}
	for _, m := range regexp.MustCompile(`\$\{\{ secrets\.([A-Za-z0-9_]+) \}\}`).FindAllStringSubmatch(strings.Join(code, "\n"), -1) {
		named[m[1]] = true
	}
	for n := range named {
		if !slices.Contains(deploySecrets, n) {
			t.Errorf("deploy.yml reads the secret %s, which is not one of the production environment's %v", n, deploySecrets)
		}
	}
	for _, n := range deploySecrets {
		if !named[n] {
			t.Errorf("CONTROL: deploy.yml never reads the secret %s", n)
		}
	}
	for _, never := range []string{"DOCKERHUB_", "TS_AUTHKEY", "docker.io/atknatk", "docker login docker.io", "authkey:"} {
		for i, l := range code {
			if strings.Contains(l, never) {
				t.Errorf("deploy.yml code line %q (code line %d) names %q: OPS-2 moved the images off Docker Hub and the "+
					"join off the auth key", strings.TrimSpace(l), i+1, never)
			}
		}
	}
	for _, f := range manifestAndDockerfilePaths(t) {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range codeLinesOf(string(src)) {
			if strings.Contains(l, "docker.io/atknatk") {
				rel, _ := filepath.Rel(repoRoot, f)
				t.Errorf("%s still names docker.io/atknatk in code: %q", rel, strings.TrimSpace(l))
			}
		}
	}

	steps := deploySteps(t)
	if steps[0].name != deploySecretsStep {
		t.Errorf("the first step is %q, want %q: a missing secret is named before anything is built", steps[0].name, deploySecretsStep)
	}
	login, build := stepIndex(t, steps, registryLoginStep), stepIndex(t, steps, "Build both images")
	if login >= build {
		t.Errorf("%q is step %d, after %q (step %d)", registryLoginStep, login, "Build both images", build)
	}
	if got := trimmedCode(steps[login]); !slices.Equal(got, []string{"env:",
		"HARBOR_CI_ROBOT_NAME: ${{ secrets.HARBOR_CI_ROBOT_NAME }}",
		"HARBOR_CI_ROBOT_SECRET: ${{ secrets.HARBOR_CI_ROBOT_SECRET }}", registryLoginRun}) {
		t.Errorf("step %q is %q: the CI robot logs in to $REGISTRY with its secret piped, nothing else", registryLoginStep, got)
	}
	push, gate := stepIndex(t, steps, "Push"), stepIndex(t, steps, accessGateStep)
	logout, kubectl := stepIndex(t, steps, registryLogout), stepIndex(t, steps, "Install kubectl (pinned, checksum-verified)")
	if gate != push+1 || logout != gate+1 || logout >= kubectl {
		t.Errorf("Push %d, the access gate %d, the logout %d, Install kubectl %d: want the gate right after the push and the "+
			"logout right after the gate, both before kubectl exists", push, gate, logout, kubectl)
	}
	for _, c := range []struct {
		i    int
		want []string
	}{
		{gate, []string{"env:", "SHORT: ${{ steps.sha.outputs.short }}", accessGateRun}},
		{logout, []string{registryLogoutRun}},
	} {
		if got := trimmedCode(steps[c.i]); !slices.Equal(got, c.want) {
			t.Errorf("step %q is %q, want %q", steps[c.i].name, got, c.want)
		}
	}
	app, pull := stepIndex(t, steps, appSecretStep), stepIndex(t, steps, pullSecretStep)
	surface := slices.IndexFunc(steps, func(s workflowStep) bool { return s.id == "opsurface" })
	apply := stepIndex(t, steps, "Apply configuration and database")
	if pull != app+1 || surface < 0 || pull >= surface || pull >= apply {
		t.Errorf("the application Secret's preflight %d, the pull Secret's %d, operator-surface %d, the first apply %d: "+
			"want the pull Secret's right after the application Secret's and before anything else is applied", app, pull, surface, apply)
	}
	if got := trimmedCode(steps[pull]); !slices.Equal(got, []string{pullSecretRun}) {
		t.Errorf("step %q is %q, want exactly %q", pullSecretStep, got, pullSecretRun)
	}
	for _, i := range []int{0, login, gate, logout, pull} {
		for _, l := range trimmedCode(steps[i]) {
			if strings.HasPrefix(l, "if:") || strings.HasPrefix(l, "continue-on-error:") {
				t.Errorf("step %q carries %q: a gate or a credential step that can be skipped is not one", steps[i].name, l)
			}
		}
	}
}

func countLines(lines []string, want string) int {
	return len(slices.DeleteFunc(slices.Clone(lines), func(l string) bool { return l != want }))
}

// manifestAndDockerfilePaths returns the Dockerfile and every YAML under deploy/k8s and
// deploy/sandbox.
func manifestAndDockerfilePaths(t *testing.T) []string {
	t.Helper()
	paths := []string{filepath.Join(repoRoot, "Dockerfile")}
	for _, dir := range []string{"k8s", "sandbox"} {
		m, err := filepath.Glob(filepath.Join(repoRoot, "deploy", dir, "*.yaml"))
		if err != nil || len(m) < 5 {
			t.Fatalf("CONTROL FAILED: deploy/%s holds %d manifests (%v)", dir, len(m), err)
		}
		paths = append(paths, m...)
	}
	return paths
}

// TestPackaging_ThePodsPullFromHarborWithTheOperatorsSecret: the four pod specs that run a
// product image — the server and the migration Job, in production and in the sandbox — name
// the Harbor project's image and `imagePullSecrets: [{name: harbor-pull}]`, at the pod spec's
// level (the indentation of its `containers:`), once. 01-rbac.yaml lets the deploy identity
// `get` that Secret by name and nothing more (the preflight reads it; CI never writes it).
//
// MUTANT (measured, OPS-2): the imagePullSecrets of 30-migrate-job.yaml deleted; the name changed
// to `harbor`; the block indented under the container; harbor-pull dropped from 01-rbac.yaml's
// resourceNames; `create` added to that rule -> each red.
func TestPackaging_ThePodsPullFromHarborWithTheOperatorsSecret(t *testing.T) {
	for _, c := range []struct{ file, image string }{
		{"k8s/20-app.yaml", harborProject + "/tappa:deploy-placeholder"},
		{"k8s/30-migrate-job.yaml", harborProject + "/tappa-migrate:deploy-placeholder"},
		{"sandbox/20-app.yaml", harborProject + "/tappa-sandbox:deploy-placeholder"},
		{"sandbox/30-migrate-job.yaml", harborProject + "/tappa-migrate:deploy-placeholder"},
	} {
		b, err := os.ReadFile(filepath.Join(repoRoot, "deploy", c.file))
		if err != nil {
			t.Fatal(err)
		}
		code := codeLinesOf(string(b))
		if n := countTrimmed(code, "image: "+c.image); n != 1 {
			t.Errorf("deploy/%s names the image %q %d time(s), want once", c.file, c.image, n)
		}
		containers := slices.IndexFunc(code, func(l string) bool { return strings.TrimSpace(l) == "containers:" })
		if containers < 0 {
			t.Fatalf("CONTROL FAILED: deploy/%s has no `containers:`", c.file)
		}
		var at []int
		for i, l := range code {
			if strings.TrimSpace(l) == "imagePullSecrets:" {
				at = append(at, i)
			}
		}
		if len(at) != 1 || indentOf(code[at[0]]) != indentOf(code[containers]) || at[0]+1 >= len(code) ||
			strings.TrimSpace(code[at[0]+1]) != "- name: "+pullSecretName || indentOf(code[at[0]+1]) <= indentOf(code[at[0]]) {
			t.Errorf("deploy/%s: want exactly one `imagePullSecrets:` at the pod spec's level (beside `containers:`) "+
				"followed by `- name: %s`; found it at code lines %v", c.file, pullSecretName, at)
		}
	}
	rbac := stripYAMLComments(readK8s(t, "01-rbac.yaml"))
	m := regexp.MustCompile(`resources: \["secrets"\]\s*\n\s*resourceNames: \[([^\]]*)\]\s*\n\s*verbs: \[([^\]]*)\]`).FindAllStringSubmatch(rbac, -1)
	if len(m) != 1 {
		t.Fatalf("01-rbac.yaml has %d secrets rule(s) of the shape resources/resourceNames/verbs, want one", len(m))
	}
	names := m[0][1]
	if !strings.Contains(names, strconv.Quote(pullSecretName)) || !strings.Contains(names, strconv.Quote("tappa-secrets")) || m[0][2] != strconv.Quote("get") {
		t.Errorf("01-rbac.yaml's secrets rule is resourceNames [%s] verbs [%s]; want both tappa-secrets and %s, and get only",
			m[0][1], m[0][2], pullSecretName)
	}
}

// runBlockOf returns the body of a step's `run: |` block, dedented.
func runBlockOf(t *testing.T, s workflowStep) string {
	t.Helper()
	at := slices.IndexFunc(s.code, func(l string) bool { return strings.TrimSpace(l) == "run: |" })
	if at < 0 || at+1 >= len(s.code) {
		t.Fatalf("step %q has no `run: |` block", s.name)
	}
	in := indentOf(s.code[at+1])
	var body []string
	for _, l := range s.code[at+1:] {
		if strings.TrimSpace(l) == "" {
			body = append(body, "")
			continue
		}
		if indentOf(l) < in {
			break
		}
		body = append(body, l[in:])
	}
	return strings.Join(body, "\n") + "\n"
}

// TestPackaging_TheDeploySecretsPreflightNamesWhatIsMissing RUNS the first step's body under
// bash, with the environment GitHub gives it — an undefined secret is an empty string — and a
// canary in every value.
//
// PART I — the rows: GREEN (exit 0) with all five set (an OAuth secret beginning tskey-client-);
// RED (exit 1) naming exactly the missing ones for each secret empty in turn, for each made a
// single space, U+00A0, U+3000, a tab-and-newline or punctuation only, and for all five empty; RED (exit 1) for an
// TS_OAUTH_SECRET that is an auth key (tskey-auth-…) or has the client prefix only in its middle.
// PART II — pinned on every row: the exit code, the missing names (and no other), that no output
// carries the canary, and the green line naming all five. PART III: a form not on this list is
// code review's subject.
//
// MUTANT (measured, OPS-2): KUBE_CONFIG dropped from the loop's list; the `case` glob turned
// into `*[[:graph:]]*` (round 2: survived until the punctuation-only row was added); the
// tskey-client- check deleted; `echo "$TS_OAUTH_SECRET"` added -> red.
func TestPackaging_TheDeploySecretsPreflightNamesWhatIsMissing(t *testing.T) {
	steps := deploySteps(t)
	body := runBlockOf(t, steps[stepIndex(t, steps, deploySecretsStep)])
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	const canary = "SECRETCANARY"
	good := map[string]string{
		"HARBOR_CI_ROBOT_NAME": "robot$tappa+ci" + canary, "HARBOR_CI_ROBOT_SECRET": "s3cr" + canary,
		"TS_OAUTH_CLIENT_ID": "k" + canary, "TS_OAUTH_SECRET": "tskey-client-" + canary, "KUBE_CONFIG": "YXBp" + canary,
	}
	run := func(env map[string]string) (string, int) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), bash, "-c", body)
		cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C.UTF-8"}
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		out, err := cmd.CombinedOutput()
		var ee *exec.ExitError
		switch {
		case errors.As(err, &ee):
			return string(out), ee.ExitCode()
		case err != nil:
			t.Fatal(err)
		}
		return string(out), 0
	}
	with := func(over map[string]string) map[string]string {
		env := map[string]string{}
		for k, v := range good {
			env[k] = v
		}
		for k, v := range over {
			env[k] = v
		}
		return env
	}
	missingIn := func(out string) []string {
		m := regexp.MustCompile(`production environment:((?: [A-Z_]+)+)\.`).FindStringSubmatch(out)
		if m == nil {
			return nil
		}
		return strings.Fields(m[1])
	}
	type row struct {
		name     string
		env      map[string]string
		wantCode int
		missing  []string
		mark     string
	}
	rows := []row{{name: "all five set", env: with(nil), mark: "present"}}
	for _, n := range deploySecrets {
		for _, v := range []string{"", " ", "\u00a0", "\u3000", "\t\n", "-*-"} {
			rows = append(rows, row{name: n + " = " + strings.ToValidUTF8(v, "?"), env: with(map[string]string{n: v}), wantCode: 1, missing: []string{n}})
		}
	}
	rows = append(rows,
		row{name: "all five empty", env: map[string]string{}, wantCode: 1, missing: deploySecrets},
		row{name: "an auth key in TS_OAUTH_SECRET", env: with(map[string]string{"TS_OAUTH_SECRET": "tskey-auth-k" + canary}),
			wantCode: 1, mark: "is not a Tailscale OAuth client secret"},
		row{name: "the prefix in the middle", env: with(map[string]string{"TS_OAUTH_SECRET": "x-tskey-client-" + canary}),
			wantCode: 1, mark: "is not a Tailscale OAuth client secret"},
	)
	for _, r := range rows {
		out, code := run(r.env)
		if code != r.wantCode || !slices.Equal(missingIn(out), r.missing) || (r.mark != "" && !strings.Contains(out, r.mark)) {
			t.Errorf("%s: exit %d, missing %v; want exit %d, missing %v, %q in:\n%s", r.name, code, missingIn(out), r.wantCode,
				r.missing, r.mark, out)
		}
		if strings.Contains(out, canary) {
			t.Errorf("%s: the output carries a secret's value:\n%s", r.name, out)
		}
		if r.wantCode == 0 && !strings.Contains(out, strings.Join(deploySecrets, " ")+" present") {
			t.Errorf("%s: the green line does not name all five secrets:\n%s", r.name, out)
		}
	}
}

// stubDocker answers `docker manifest inspect <ref>` from files in STUB_DIR: MODE.KEY.out/.rc
// for that ref, else MODE.out/.rc, where MODE is "anon" when DOCKER_CONFIG is set (the gate's
// anonymous half) and "auth" when it is not. Each call is recorded with DOCKER_CONFIG and the
// entries that directory held at that moment.
const stubDocker = `#!/bin/sh
for a in "$@"; do ref=$a; done
if [ -n "${DOCKER_CONFIG:-}" ]; then mode=anon; held=$(ls -A "$DOCKER_CONFIG" 2>&1 | tr '\n' ' '); else mode=auth; held=-; fi
printf '%s|%s|%s\n' "$mode" "$held" "$*" >> "$STUB_DIR/docker.calls"
[ "$1 $2" = "manifest inspect" ] || { echo "stub docker: unexpected call: $*" >&2; exit 97; }
key=$(printf '%s' "$ref" | tr -c 'A-Za-z0-9' '_')
for f in "$STUB_DIR/$mode.$key" "$STUB_DIR/$mode"; do
  if [ -f "$f.rc" ]; then
    if [ -f "$f.out" ]; then cat "$f.out"; fi
    exit "$(cat "$f.rc")"
  fi
done
echo "stub docker: no answer for $mode $ref" >&2
exit 98
`

// TestVerifyImage_TheAccessGateMeasuresBothDirections runs `verify-image.sh --access` on the
// deploy's two refs against stubDocker.
//
// PART I — the rows: GREEN (exit 0) when the credentialed call returns a manifest and the
// anonymous one is refused with "unauthorized" (Harbor's wording), "denied" (the registry
// spec's) or "authentication required"; RED (exit 1) when the anonymous call returns a manifest
// (the project is public), fails with no such host, times out, or exits 0 with nothing; when
// the credentialed call fails, or exits 0 with nothing; when only the migration image is public.
// PART II — pinned on every row: the exit code and the message; every anonymous call ran with
// DOCKER_CONFIG naming an EMPTY directory and every credentialed one without DOCKER_CONFIG; no
// anonymous call for a ref whose credentialed call failed; the credentialed call's output is
// never printed (a canary in it). No ref: exit 2.
// PART III: a form not on this list is code review's subject; no completeness claim.
//
// MUTANT (measured, OPS-2): the anonymous half accepting any failure; the `"schemaVersion"`
// check on the credentialed half reduced to the exit code; the anonymous call made without
// DOCKER_CONFIG; the credentialed output printed -> each red.
func TestVerifyImage_TheAccessGateMeasuresBothDirections(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(stubDocker), 0o755); err != nil {
		t.Fatal(err)
	}
	set := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reset := func() {
		old, _ := filepath.Glob(filepath.Join(dir, "*.*"))
		for _, f := range old {
			_ = os.Remove(f)
		}
	}
	const authCanary = "CREDENTIALEDOUTPUTCANARY"
	app, mig := harborProject+"/tappa:sha-0123456789ab", harborProject+"/tappa-migrate:sha-0123456789ab"
	manifest := `{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": []}`
	harbor401 := "unauthorized: unauthorized to access repository: tappa/tappa, action: pull: unauthorized to access repository: tappa/tappa, action: pull"
	run := func(args ...string) (string, int) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "bash", append([]string{scriptPathIn(t, "verify-image.sh")}, args...)...)
		var env []string
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "DOCKER_CONFIG=") && !strings.HasPrefix(e, "PATH=") {
				env = append(env, e)
			}
		}
		cmd.Env = append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "STUB_DIR="+dir)
		out, err := cmd.CombinedOutput()
		var ee *exec.ExitError
		switch {
		case errors.As(err, &ee):
			return string(out), ee.ExitCode()
		case err != nil:
			t.Fatal(err)
		}
		return string(out), 0
	}
	key := func(ref string) string { return regexp.MustCompile(`[^A-Za-z0-9]`).ReplaceAllString(ref, "_") }
	for _, tc := range []struct {
		name               string
		auth, authRC       string
		anon, anonRC       string
		migAnon, migAnonRC string
		wantCode           int
		wantMark           string
		wantAnonCalls      int
	}{
		{name: "private: Harbor's 401", auth: manifest, authRC: "0", anon: harbor401, anonRC: "1", wantMark: "refused: " + mig, wantAnonCalls: 2},
		{name: "private: denied", auth: manifest, authRC: "0", anon: "errors:\ndenied: requested access to the resource is denied", anonRC: "1", wantMark: "refused: " + app, wantAnonCalls: 2},
		{name: "private: authentication required", auth: manifest, authRC: "0", anon: "unauthorized: authentication required", anonRC: "1", wantMark: "refused", wantAnonCalls: 2},
		{name: "public", auth: manifest, authRC: "0", anon: manifest, anonRC: "0", wantCode: 1, wantMark: "IS pullable WITHOUT a credential", wantAnonCalls: 2},
		{name: "anonymous: no such host", auth: manifest, authRC: "0", anon: `Get "https://` + harborRegistry + `/v2/": dial tcp: lookup ` + harborRegistry + `: no such host`, anonRC: "1", wantCode: 1, wantMark: "NOT with an authorization refusal", wantAnonCalls: 2},
		{name: "anonymous: timeout", auth: manifest, authRC: "0", anon: "net/http: request canceled while waiting for connection (Client.Timeout exceeded while awaiting headers)", anonRC: "1", wantCode: 1, wantMark: "NOT with an authorization refusal", wantAnonCalls: 2},
		{name: "anonymous: exit 0, nothing", auth: manifest, authRC: "0", anon: "", anonRC: "0", wantCode: 1, wantMark: "NOT with an authorization refusal", wantAnonCalls: 2},
		{name: "credentialed: refused", auth: "unauthorized: " + authCanary, authRC: "1", anon: harbor401, anonRC: "1", wantCode: 1, wantMark: "is NOT pullable with the credential this job logged in with (docker manifest inspect exit 1", wantAnonCalls: 0},
		{name: "credentialed: exit 0, nothing", auth: "", authRC: "0", anon: harbor401, anonRC: "1", wantCode: 1, wantMark: "exit 0; its output is not shown", wantAnonCalls: 0},
		{name: "only the migration image public", auth: manifest, authRC: "0", anon: harbor401, anonRC: "1", migAnon: manifest, migAnonRC: "0", wantCode: 1, wantMark: mig + " IS pullable WITHOUT", wantAnonCalls: 2},
	} {
		reset()
		set("auth.out", tc.auth)
		set("auth.rc", tc.authRC)
		set("anon.out", tc.anon)
		set("anon.rc", tc.anonRC)
		if tc.migAnonRC != "" {
			set("anon."+key(mig)+".out", tc.migAnon)
			set("anon."+key(mig)+".rc", tc.migAnonRC)
		}
		out, code := run("--access", app, mig)
		if code != tc.wantCode || !strings.Contains(out, tc.wantMark) {
			t.Errorf("%s: exit %d; want exit %d and %q in:\n%s", tc.name, code, tc.wantCode, tc.wantMark, out)
		}
		if tc.name == "only the migration image public" && strings.Contains(out, app+" IS pullable") {
			t.Errorf("%s: the app image, refused anonymously, was reported public:\n%s", tc.name, out)
		}
		if strings.Contains(out, authCanary) {
			t.Errorf("%s: the credentialed call's output was printed:\n%s", tc.name, out)
		}
		b, _ := os.ReadFile(filepath.Join(dir, "docker.calls"))
		var anon, auth int
		for _, c := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			mode, rest, _ := strings.Cut(c, "|")
			held, argv, _ := strings.Cut(rest, "|")
			if !strings.HasPrefix(argv, "manifest inspect ") {
				t.Errorf("%s: docker was called as %q", tc.name, argv)
			}
			switch mode {
			case "anon":
				anon++
				if strings.TrimSpace(held) != "" {
					t.Errorf("%s: the anonymous call's DOCKER_CONFIG held %q, want an empty directory", tc.name, held)
				}
			case "auth":
				auth++
			}
		}
		if auth != 2 || anon != tc.wantAnonCalls {
			t.Errorf("%s: %d credentialed and %d anonymous calls, want 2 and %d", tc.name, auth, anon, tc.wantAnonCalls)
		}
	}
	if out, code := run("--access"); code != 2 {
		t.Errorf("--access with no ref: exit %d, want 2\n%s", code, out)
	}
}

// pullRobot is the Harbor robot harbor-pull is made from (operator step 4 (a)).
const pullRobot = "robot$tappa+k8s-pull"

// authsFor is a .dockerconfigjson whose auths hold one entry, host -> entry (a JSON object).
func authsFor(host, entry string) string {
	return `{"auths":{` + strconv.Quote(host) + `:` + entry + `}}`
}

// pullSecretAnswer is what `kubectl get secret harbor-pull -o jsonpath='{.type}|{.data...}'`
// prints for a dockerconfigjson Secret holding cfg.
func pullSecretAnswer(cfg string) string {
	return "kubernetes.io/dockerconfigjson|" + base64.StdEncoding.EncodeToString([]byte(cfg))
}

// TestVerifyDeployment_ThePullSecretModeHoldsTheOperatorsSecret runs `verify-deployment.sh
// pull-secret` — the deploy's own arguments — against the scriptRig's stub kubectl.
//
// PART I — the rows: GREEN (exit 0) for an auths entry for the registry with an `auth`, with a
// username and a password, spelled https://registry/ or with a path; RED (exit 1) for NotFound,
// a type other than kubernetes.io/dockerconfigjson (one with characters that are not shown),
// another registry only, a look-alike host, the registry on another port, an entry with no
// credential, an empty `auth`, data that is not base64 or not JSON, auths not an object; RED
// (exit 2) for Forbidden and any other read failure.
// PART II — pinned on every row: the exit code and the message; exactly one kubectl call, `-n
// tappa get secret harbor-pull -o jsonpath=…`; no output carries the credential's canary or
// kubectl's error text. With no arguments the mode reads tappa/harbor-pull for
// registry.everva.com.tr.
// PART III: a form not on this list is code review's subject; no completeness claim.
//
// MUTANT (measured, OPS-2): the host compared with a prefix match; the type check deleted; the
// Forbidden branch made exit 1; the encoded data echoed on the "nohost" branch (round 2: the
// canary check alone missed it — the whole config's base64 carries neither the canary nor the
// auth's own base64 — so the data itself is now looked for) -> each red.
func TestVerifyDeployment_ThePullSecretModeHoldsTheOperatorsSecret(t *testing.T) {
	rig := newScriptRig(t)
	const canary = "PULLCREDCANARY"
	cred := `{"auth":"` + base64.StdEncoding.EncodeToString([]byte(pullRobot+":"+canary)) + `"}`
	for _, tc := range []struct {
		name, out, rc, errText string
		wantCode               int
		wantMark               string
	}{
		{name: "auth", out: pullSecretAnswer(authsFor(harborRegistry, cred)), wantMark: "with a credential for " + harborRegistry},
		{name: "username and password", out: pullSecretAnswer(authsFor(harborRegistry, `{"username":`+strconv.Quote(pullRobot)+`,"password":`+strconv.Quote(canary)+`}`)), wantMark: "with a credential for"},
		{name: "https:// and a slash", out: pullSecretAnswer(authsFor("https://"+harborRegistry+"/", cred)), wantMark: "with a credential for"},
		{name: "with a path", out: pullSecretAnswer(authsFor(harborRegistry+"/v2/", cred)), wantMark: "with a credential for"},
		{name: "NotFound", rc: "1", errText: `Error from server (NotFound): secrets "harbor-pull" not found ` + canary, wantCode: 1, wantMark: "secret/harbor-pull does not exist in namespace tappa"},
		{name: "Forbidden", rc: "1", errText: `Error from server (Forbidden): secrets "harbor-pull" is forbidden: User "` + canary + `"`, wantCode: 2, wantMark: "re-apply that file"},
		{name: "connection refused", rc: "1", errText: "The connection to the server 10.43.0.1:443 was refused " + canary, wantCode: 2, wantMark: "could not be read (kubectl exit 1)"},
		{name: "Opaque", out: "Opaque|" + base64.StdEncoding.EncodeToString([]byte(authsFor(harborRegistry, cred))), wantCode: 1, wantMark: "is of type 'Opaque'"},
		{name: "a type with odd characters", out: "x y" + canary + "$|e30=", wantCode: 1, wantMark: "is of type '<not shown>'"},
		{name: "another registry only", out: pullSecretAnswer(authsFor("docker.io", cred)), wantCode: 1, wantMark: "holds no credential for " + harborRegistry + " (it names 1 other"},
		{name: "a look-alike host", out: pullSecretAnswer(authsFor(harborRegistry+".example", cred)), wantCode: 1, wantMark: "holds no credential for"},
		{name: "another port", out: pullSecretAnswer(authsFor(harborRegistry+":5000", cred)), wantCode: 1, wantMark: "holds no credential for"},
		{name: "no credential in the entry", out: pullSecretAnswer(authsFor(harborRegistry, `{}`)), wantCode: 1, wantMark: "carries neither an auth nor"},
		{name: "an empty auth", out: pullSecretAnswer(authsFor(harborRegistry, `{"auth":""}`)), wantCode: 1, wantMark: "carries neither an auth nor"},
		{name: "a username without a password", out: pullSecretAnswer(authsFor(harborRegistry, `{"username":`+strconv.Quote(canary)+`}`)), wantCode: 1, wantMark: "carries neither an auth nor"},
		{name: "not base64", out: "kubernetes.io/dockerconfigjson|%%%" + canary, wantCode: 1, wantMark: "does not hold a readable .dockerconfigjson"},
		{name: "not JSON", out: pullSecretAnswer(canary), wantCode: 1, wantMark: "does not hold a readable .dockerconfigjson"},
		{name: "auths not an object", out: pullSecretAnswer(`{"auths":["` + canary + `"]}`), wantCode: 1, wantMark: "does not hold a readable .dockerconfigjson"},
		{name: "empty data", out: "kubernetes.io/dockerconfigjson|", wantCode: 1, wantMark: "does not hold a readable .dockerconfigjson"},
	} {
		rig.clear("pullsecret.out", "pullsecret.rc", "pullsecret.err")
		rig.set("pullsecret.out", tc.out)
		if tc.rc != "" {
			rig.set("pullsecret.rc", tc.rc)
		}
		if tc.errText != "" {
			rig.set("pullsecret.err", tc.errText)
		}
		stdout, stderr, code := rig.run("pull-secret", "tappa", pullSecretName, harborRegistry)
		out := stdout + stderr
		if code != tc.wantCode || !strings.Contains(out, tc.wantMark) {
			t.Errorf("%s: exit %d; want exit %d and %q in:\n%s", tc.name, code, tc.wantCode, tc.wantMark, out)
		}
		if strings.Contains(out, canary) || strings.Contains(out, base64.StdEncoding.EncodeToString([]byte(pullRobot+":"+canary))) {
			t.Errorf("%s: the output carries the credential or kubectl's error text:\n%s", tc.name, out)
		}
		if _, data, _ := strings.Cut(tc.out, "|"); len(data) > 4 && strings.Contains(out, data) {
			t.Errorf("%s: the output carries the Secret's encoded .dockerconfigjson:\n%s", tc.name, out)
		}
		calls := rig.kubectlCalls()
		if len(calls) != 1 || !slices.Equal(calls[0][:6], []string{"-n", "tappa", "get", "secret", pullSecretName, "-o"}) {
			t.Errorf("%s: kubectl calls %q, want one `-n tappa get secret %s -o jsonpath=…`", tc.name, calls, pullSecretName)
		}
	}
	rig.clear("pullsecret.rc", "pullsecret.err")
	rig.set("pullsecret.out", pullSecretAnswer(authsFor(harborRegistry, cred)))
	stdout, stderr, code := rig.run("pull-secret")
	if calls := rig.kubectlCalls(); code != 0 || len(calls) != 1 || !slices.Equal(calls[0][:5], []string{"-n", "tappa", "get", "secret", pullSecretName}) ||
		!strings.Contains(stdout, harborRegistry) {
		t.Errorf("no arguments: exit %d, kubectl %q; want exit 0 reading tappa/%s for %s\n%s%s", code, calls, pullSecretName,
			harborRegistry, stdout, stderr)
	}
}
