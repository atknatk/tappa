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
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
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
//   - the secrets preflight is the step right after the trigger gate (backlog T140, which
//     TestPackaging_TheDeployRunsOnlyForAPushToThisRepositorysMain pins as the first step), so a
//     missing secret is named before the checkout and before anything is built; the registry
//     login (the CI robot, the secret piped)
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
	if len(steps) < 2 || steps[0].name != triggerGateStep || steps[1].name != deploySecretsStep {
		t.Errorf("the first two steps are not %q then %q: the trigger is re-checked first (T140) and a missing secret is "+
			"named next, before anything is checked out or built", triggerGateStep, deploySecretsStep)
	}
	if checkout := stepIndex(t, steps, "Checkout that exact commit"); checkout < 2 {
		t.Errorf("%q is step %d, before the secrets preflight", "Checkout that exact commit", checkout)
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

// goImageVersion and goImageDigest are the build's base image: golang:<version>-bookworm by its
// index digest on Docker Hub (measured 2026-10-19), the same bytes Harbor's proxy cache serves.
const (
	goImageVersion = "1.26.9"
	goImageDigest  = "sha256:d9c68c2c51161e12fd77e4c6320687c9cd86e1af1e3ad6e6cd63ff970641453c"
	harborProxy    = harborRegistry + "/dockerhub"
)

// TestPackaging_TheBuildPullsItsBaseImageThroughHarborByDigest (OPS-2 fix round 1): since OPS-2 the
// deploy job holds a Harbor login only, so the base image comes through Harbor's private
// proxy-cache project `dockerhub`, by digest:
//
//   - "Build both images" sets GO_BUILD_IMAGE once, to
//     registry.everva.com.tr/dockerhub/library/golang:<v>-bookworm@<digest>, and both of its
//     `docker build` lines -- the only two in deploy.yml -- pass --build-arg GO_IMAGE="$GO_BUILD_IMAGE";
//   - the registry login comes before that step (the proxy project is private);
//   - the Dockerfile's default is golang:<v>-bookworm@<the same digest> (Docker Hub's own name: a
//     build without the override holds no Harbor credential), once, and `FROM ${GO_IMAGE} AS build`
//     is its only use;
//   - <v> is the Go both workflows' setup-go pins (ci.yml never builds the Dockerfile).
//
// MUTANT (measured, OPS-2 fix round 1): the --build-arg dropped from the migrate build; one hex
// digit of deploy.yml's digest changed; the Dockerfile default back to the bare tag;
// GO_BUILD_IMAGE pointed at docker.io/library/golang; the login moved after the build -> each red.
func TestPackaging_TheBuildPullsItsBaseImageThroughHarborByDigest(t *testing.T) {
	steps := deploySteps(t)
	build := stepIndex(t, steps, "Build both images")
	if login := stepIndex(t, steps, registryLoginStep); login >= build {
		t.Errorf("%q (step %d) comes after %q (step %d): the build pulls its base image through the private proxy "+
			"project with that login", registryLoginStep, login, "Build both images", build)
	}
	wantSet := "GO_BUILD_IMAGE=" + harborProxy + "/library/golang:" + goImageVersion + "-bookworm@" + goImageDigest
	if n := countTrimmed(steps[build].code, wantSet); n != 1 {
		t.Errorf("%q sets %q %d time(s), want once", "Build both images", wantSet, n)
	}
	sets := 0
	for _, l := range steps[build].code {
		if strings.Contains(l, "GO_BUILD_IMAGE=") {
			sets++
		}
	}
	if sets != 1 {
		t.Errorf("%q assigns GO_BUILD_IMAGE %d time(s), want once", "Build both images", sets)
	}
	b, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "deploy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var builds []string
	for _, l := range codeLinesOf(string(b)) {
		if strings.Contains(l, "docker build") {
			builds = append(builds, strings.TrimSpace(l))
		}
	}
	wantBuilds := []string{
		`docker build --build-arg GO_IMAGE="$GO_BUILD_IMAGE" --target app     -t "$APP_IMAGE:sha-$SHORT" .`,
		`docker build --build-arg GO_IMAGE="$GO_BUILD_IMAGE" --target migrate -t "$MIGRATE_IMAGE:sha-$SHORT" .`,
	}
	if !slices.Equal(builds, wantBuilds) {
		t.Errorf("deploy.yml's docker build lines are %q, want exactly %q", builds, wantBuilds)
	}
	if !slices.ContainsFunc(steps[build].code, func(l string) bool { return strings.TrimSpace(l) == wantBuilds[0] }) {
		t.Errorf("the docker build lines are not in %q", "Build both images")
	}

	df, err := os.ReadFile(filepath.Join(repoRoot, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	dcode := codeLinesOf(string(df))
	wantArg := "ARG GO_IMAGE=golang:" + goImageVersion + "-bookworm@" + goImageDigest
	var args, uses []string
	for _, l := range dcode {
		tl := strings.TrimSpace(l)
		if strings.HasPrefix(tl, "ARG GO_IMAGE") {
			args = append(args, tl)
		}
		if strings.Contains(tl, "GO_IMAGE}") || strings.Contains(tl, "$GO_IMAGE") {
			uses = append(uses, tl)
		}
	}
	if !slices.Equal(args, []string{wantArg}) {
		t.Errorf("the Dockerfile declares GO_IMAGE as %q, want exactly %q (the digest deploy.yml pulls through Harbor)", args, wantArg)
	}
	if !slices.Equal(uses, []string{"FROM ${GO_IMAGE} AS build"}) {
		t.Errorf("the Dockerfile uses GO_IMAGE in %q, want only `FROM ${GO_IMAGE} AS build`", uses)
	}
	if !regexp.MustCompile(`^sha256:[0-9a-f]{64}$`).MatchString(goImageDigest) {
		t.Errorf("goImageDigest %q is not a sha256 digest", goImageDigest)
	}
	for _, wf := range []string{"ci.yml", "deploy.yml"} {
		w, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", wf))
		if err != nil {
			t.Fatal(err)
		}
		if n := countTrimmed(codeLinesOf(string(w)), `go-version: "`+goImageVersion+`"`); n != 1 {
			t.Errorf("%s pins go-version %q %d time(s), want once: the toolchain the tests run is the one the image builds with", wf, goImageVersion, n)
		}
	}
}

// secretRef is any expression reading a secret, spaced or not (`${{ secrets.X }}`,
// `${{secrets.X}}`, `secrets['X']`), and the bulk forms.
var secretRef = regexp.MustCompile(`\$\{\{[^}]*\bsecrets\b|\bsecrets:\s*inherit\b|toJSON\(\s*secrets\s*\)`)

// secretPlacementFaults returns, for a workflow's source, every place a secret is read other
// than a step's OWN `env:` or the `with:` of a step that `uses:` an action.
func secretPlacementFaults(src string) []string {
	code := codeLinesOf(src)
	var faults []string
	for i, l := range code {
		if !secretRef.MatchString(l) {
			continue
		}
		step := stepOf(code, i)
		if step < 0 {
			faults = append(faults, "outside every step (the job's or the workflow's level): "+strings.TrimSpace(l))
			continue
		}
		key := -1
		for k := i - 1; k > step; k-- {
			if strings.TrimSpace(code[k]) != "" && indentOf(code[k]) < indentOf(code[i]) {
				key = k
				break
			}
		}
		// A step's own keys sit two columns right of its `- name:` dash.
		own := key > step && indentOf(code[key]) == indentOf(code[step])+2
		parent := ""
		if key >= 0 {
			parent = strings.TrimSpace(code[key])
		}
		switch {
		case own && parent == "env:":
		case own && parent == "with:" && stepUses(code, step):
		default:
			faults = append(faults, "under "+strconv.Quote(parent)+" of step "+strconv.Quote(strings.TrimSpace(code[step]))+": "+strings.TrimSpace(l))
		}
	}
	return faults
}

// TestPackaging_TheDockerfileNamesNoFrontendImage (OPS-2 follow-up). A `syntax` parser directive
// makes BuildKit pull a frontend IMAGE before it reads an instruction -- docker/dockerfile:1 was
// a moving tag, pulled anonymously from Docker Hub, the one Docker Hub pull left in the deploy
// build once the base image came through Harbor's proxy. Measured 2026-10-19 (docker 29.8.2,
// BuildKit v0.33.1): with the directive aimed at an unreachable registry the build fails at
// "failed to resolve source metadata" before parsing; without it BuildKit's built-in frontend
// parses this Dockerfile (`--call=targets`: five stages; `--call=check`: no warnings).
//
//   - the Dockerfile's parser directives (BuildKit's rule: `# key=value` lines at the very top,
//     case-insensitive, spaces allowed, ended by the first other line) name no `syntax`, and no
//     line anywhere in it looks like one (a directive moved below the header is inert today and
//     live the day the header is removed);
//   - no workflow, Makefile or deploy/README.md line passes BUILDKIT_SYNTAX (the build-arg that
//     names a frontend from outside the file).
//
// MUTANT (measured): the old first line put back; `#SYNTAX = docker/dockerfile:1` as the first
// line; the directive put below the header comment; `--build-arg BUILDKIT_SYNTAX=...` added to
// deploy.yml's app build -> each red.
func TestPackaging_TheDockerfileNamesNoFrontendImage(t *testing.T) {
	df, err := os.ReadFile(filepath.Join(repoRoot, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	directive := regexp.MustCompile(`^#\s*([A-Za-z][A-Za-z0-9]*)\s*=\s*(.*)$`)
	lines := strings.Split(string(df), "\n")
	for _, l := range lines {
		m := directive.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			break
		}
		if strings.EqualFold(m[1], "syntax") {
			t.Errorf("the Dockerfile opens with a syntax directive (%q): BuildKit would pull that frontend image "+
				"before it reads an instruction", strings.TrimSpace(l))
		}
	}
	if !strings.HasPrefix(lines[0], "# Tappa") {
		t.Errorf("CONTROL: the Dockerfile's first line is %q, want its header comment", lines[0])
	}
	anywhere := regexp.MustCompile(`(?i)^#\s*syntax\s*=`)
	for i, l := range lines {
		if anywhere.MatchString(strings.TrimSpace(l)) {
			t.Errorf("Dockerfile:%d looks like a syntax directive (%q)", i+1, strings.TrimSpace(l))
		}
	}
	for _, f := range []string{".github/workflows/deploy.yml", ".github/workflows/ci.yml", "Makefile", "deploy/README.md"} {
		b, err := os.ReadFile(filepath.Join(repoRoot, f))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "BUILDKIT_SYNTAX") {
			t.Errorf("%s names BUILDKIT_SYNTAX: a frontend image would be pulled for the build", f)
		}
	}
}

// TestPackaging_TheProductionSecretsAreReadOnlyByTheStepsThatNeedThem (OPS-2 fix round 1): every
// secret deploy.yml reads -- the `production` environment's -- sits in the `env:` of the step that
// uses it or in the `with:` of the one action step, never at the JOB's level. A job-level `env:`
// is inherited by EVERY step, including `actions/checkout`, `setup-go`, the build (the
// repository's own Makefile and code run there) and the third-party tailnet action; a step-level
// one reaches that step's process only.
//
// PART I -- deploy.yml as it is: no fault, and at least ten secret reads seen (CONTROL). PART II
// -- the same file mutated in this test, each of which must be a fault: the registry secret moved
// from the login step into the job's `env:`; the same, spelled `${{secrets.X}}` with no spaces;
// a secret in the workflow's own `env:`; a secret written into a `run:` body, inline or under a
// line of that body that reads `env:`; a secret under the `with:` of a step that uses no action;
// `secrets: inherit`.
//
// MUTANT (measured, OPS-2 fix round 1): HARBOR_CI_ROBOT_SECRET moved by hand into the job-level
// `env:` of deploy.yml -> PART I red (TestPackaging_TheDeployReachesTheClusterOnlyThroughTheTailnet,
// which accepts any `env:` parent, stayed green on it); the `own` condition deleted (any `env:` or
// `with:` parent accepted) -> the run-body `env:` row is not a fault -> red.
func TestPackaging_TheProductionSecretsAreReadOnlyByTheStepsThatNeedThem(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "deploy.yml"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if f := secretPlacementFaults(src); len(f) != 0 {
		t.Errorf("deploy.yml reads a secret outside a step's own env:/with: (%d):\n  %s", len(f), strings.Join(f, "\n  "))
	}
	reads := 0
	for _, l := range codeLinesOf(src) {
		if secretRef.MatchString(l) {
			reads++
		}
	}
	if reads < 10 {
		t.Fatalf("CONTROL FAILED: %d secret read(s) seen in deploy.yml, want at least 10", reads)
	}

	const loginSecret = "          HARBOR_CI_ROBOT_SECRET: ${{ secrets.HARBOR_CI_ROBOT_SECRET }}\n"
	const jobEnv = "\n    env:\n"
	login := strings.Index(src, "      - name: "+registryLoginStep+"\n")
	if login < 0 || strings.Count(src, jobEnv) != 1 || !strings.Contains(src[login:], loginSecret) {
		t.Fatal("CONTROL FAILED: the login step, its secret line or the job-level env: is not where the mutants expect it")
	}
	moveToJob := func(line string) string {
		out := src[:login] + strings.Replace(src[login:], loginSecret, "", 1)
		return strings.Replace(out, jobEnv, jobEnv+line, 1)
	}
	for _, m := range []struct{ name, src string }{
		{"moved to the job's env:", moveToJob("      HARBOR_CI_ROBOT_SECRET: ${{ secrets.HARBOR_CI_ROBOT_SECRET }}\n")},
		{"moved to the job's env:, no spaces", moveToJob("      HARBOR_CI_ROBOT_SECRET: ${{secrets.HARBOR_CI_ROBOT_SECRET}}\n")},
		{"in the workflow's env:", strings.Replace(src, "\njobs:\n", "\nenv:\n  K: ${{ secrets.KUBE_CONFIG }}\njobs:\n", 1)},
		{"in a run body", strings.Replace(src, "        run: docker logout \"$REGISTRY\"\n",
			"        run: echo ${{ secrets.KUBE_CONFIG }} && docker logout \"$REGISTRY\"\n", 1)},
		{"under an `env:` line inside a run body", strings.Replace(src, "        run: docker logout \"$REGISTRY\"\n",
			"        run: |\n          env:\n            K: ${{ secrets.KUBE_CONFIG }}\n          docker logout \"$REGISTRY\"\n", 1)},
		{"under with: of a step without uses:", strings.Replace(src, "        run: docker logout \"$REGISTRY\"\n",
			"        with:\n          k: ${{ secrets.KUBE_CONFIG }}\n        run: docker logout \"$REGISTRY\"\n", 1)},
		{"secrets: inherit", strings.Replace(src, "    runs-on: ubuntu-latest\n", "    runs-on: ubuntu-latest\n    secrets: inherit\n", 1)},
	} {
		if m.src == src {
			t.Errorf("CONTROL FAILED: mutant %q changed nothing", m.name)
			continue
		}
		if len(secretPlacementFaults(m.src)) == 0 {
			t.Errorf("mutant %q: no fault found -- the pin cannot see a secret in that place", m.name)
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

// stubDocker answers `docker manifest inspect <ref>` -- the gate's CREDENTIALED half -- from
// files in STUB_DIR: auth.KEY.out/.rc for that ref, else auth.out/.rc. Every call is recorded.
// Since OPS-2 fix round 1 the anonymous half does not use docker at all, so a call made with
// DOCKER_CONFIG set is answered with exit 96 and recorded as such.
const stubDocker = `#!/bin/sh
for a in "$@"; do ref=$a; done
if [ -n "${DOCKER_CONFIG:-}" ]; then mode=docker-config; else mode=auth; fi
printf '%s|%s\n' "$mode" "$*" >> "$STUB_DIR/docker.calls"
[ "$mode" = auth ] || { echo "stub docker: called with DOCKER_CONFIG" >&2; exit 96; }
[ "$1 $2" = "manifest inspect" ] || { echo "stub docker: unexpected call: $*" >&2; exit 97; }
key=$(printf '%s' "$ref" | tr -c 'A-Za-z0-9' '_')
for f in "$STUB_DIR/auth.$key" "$STUB_DIR/auth"; do
  if [ -f "$f.rc" ]; then
    if [ -f "$f.out" ]; then cat "$f.out"; fi
    exit "$(cat "$f.rc")"
  fi
done
echo "stub docker: no answer for $ref" >&2
exit 98
`

// stubRegistryCurl stands in front of the REAL curl: it records the arguments, refuses a call whose
// first argument is not -q (curl reads ~/.curlrc unless -q comes first, and a .curlrc can add a
// credential), and sends every connection for the registry to the local harborStub with
// --connect-to (STUB_HOST:STUB_PORT; the DNS row names a host that does not resolve). -k because the stub's certificate is not the registry's: what this test runs is
// the token flow and its verdicts, not TLS. STUB_CURL_TAIL is appended last (curl's last
// --max-time wins), which is how the timeout row makes curl give up.
const stubRegistryCurl = `#!/bin/sh
printf '%s\n' "$*" >> "$STUB_DIR/curl.calls"
[ "$1" = "-q" ] || { echo "stub curl: -q is not the first argument" >&2; exit 96; }
shift
exec "$REAL_CURL" -q -k --connect-to "` + harborRegistry + `:443:$STUB_HOST:$STUB_PORT" "$@" $STUB_CURL_TAIL
`

// harborAnswer is one row's registry: what Harbor answers, unless a field overrides it.
type harborAnswer struct {
	v2Code      int      // /v2/: 0 = Harbor's 401 with its Bearer challenge
	v2Challenge string   // "" = Harbor's: Bearer realm="https://<registry>/service/token",service="harbor-registry"
	tokenCode   int      // /service/token: 0 = 200
	tokenBody   string   // "" = an anonymous token, or a granting one for a public repository
	public      []string // repositories an anonymous token may pull
	manCode     int      // the manifest's code when the token does not grant: 0 = 401 UNAUTHORIZED
	manBody     string   // its body when manCode is set
	manEmpty    bool     // close the connection without an answer on the manifest
	manSlow     bool     // answer the manifest only after the client has given up
}

// harborRequest is what the stub saw.
type harborRequest struct{ path, query, auth string }

// harborStub serves a, the way Harbor does (measured by the OPS-2 auditor against
// registry.everva.com.tr: /v2/ -> 401, the manifest -> 401 UNAUTHORIZED), and records requests.
func harborStub(t *testing.T, a *harborAnswer, seen *[]harborRequest, mu *sync.Mutex) *httptest.Server {
	t.Helper()
	const challengeOf = `Bearer realm="https://` + harborRegistry + `/service/token",service="harbor-registry"`
	unauthorized := func(w http.ResponseWriter, repo string) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Www-Authenticate", challengeOf+`,scope="repository:`+repo+`:pull"`)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"errors":[{"code":"UNAUTHORIZED","message":"unauthorized to access repository: `+repo+`, action: pull: unauthorized to access repository: `+repo+`, action: pull"}]}`+"\n")
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*seen = append(*seen, harborRequest{r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")})
		mu.Unlock()
		switch {
		case r.URL.Path == "/v2/":
			if a.v2Code != 0 && a.v2Code != http.StatusUnauthorized {
				w.WriteHeader(a.v2Code)
				return
			}
			ch := challengeOf
			if a.v2Challenge != "" {
				ch = a.v2Challenge
			}
			w.Header().Set("Www-Authenticate", ch)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"errors":[{"code":"UNAUTHORIZED","message":"unauthorized: unauthorized"}]}`+"\n")
		case r.URL.Path == "/service/token":
			if a.tokenCode != 0 && a.tokenCode != http.StatusOK {
				w.WriteHeader(a.tokenCode)
				return
			}
			body := a.tokenBody
			if body == "" {
				tok := "ANONYMOUS"
				q := r.URL.Query()
				if repo, ok := strings.CutSuffix(strings.TrimPrefix(q.Get("scope"), "repository:"), ":pull"); ok &&
					q.Get("service") == "harbor-registry" && strings.HasPrefix(q.Get("scope"), "repository:") && slices.Contains(a.public, repo) {
					tok = "GRANT." + repo
				}
				body = `{"token":"` + tok + `","access_token":"` + tok + `","expires_in":1800,"issued_at":"2026-10-19T00:00:00Z"}`
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, body)
		case strings.HasPrefix(r.URL.Path, "/v2/") && strings.Contains(r.URL.Path, "/manifests/"):
			repo, _, _ := strings.Cut(strings.TrimPrefix(r.URL.Path, "/v2/"), "/manifests/")
			if r.Header.Get("Authorization") == "Bearer GRANT."+repo || (a.v2Code == http.StatusOK && slices.Contains(a.public, repo)) {
				w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
				_, _ = io.WriteString(w, `{"schemaVersion":2,"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[]}`)
				return
			}
			switch {
			case a.manSlow:
				select {
				case <-r.Context().Done():
				case <-time.After(10 * time.Second):
				}
				return
			case a.manEmpty:
				if c, _, err := w.(http.Hijacker).Hijack(); err == nil {
					_ = c.Close()
				}
				return
			case a.manCode != 0:
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(a.manCode)
				_, _ = io.WriteString(w, a.manBody)
				return
			}
			unauthorized(w, repo)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestVerifyImage_TheAccessGateMeasuresBothDirections runs `verify-image.sh --access` on the
// deploy's two refs: the credentialed half against stubDocker, the anonymous half with the REAL
// curl (stubRegistryCurl) against harborStub.
//
// PART I -- the rows. GREEN (exit 0) for Harbor's own answers, the FIRST row and the one the gate
// exists for (/v2/ 401 with a Bearer challenge, an anonymous token, the manifest 401
// UNAUTHORIZED), and for a registry that asks nothing at /v2/ but refuses the manifest with 401
// UNAUTHORIZED. RED (exit 1) when the anonymous token pulls the manifest (both images public;
// only the migration image public; /v2/ open and the manifest 200); when /v2/ is 503, or 401
// without a Bearer challenge; when the token service answers 401 or 200 without a token; when
// the manifest is 503, 404 MANIFEST_UNKNOWN, 403 DENIED, 401 with no error code, no answer at
// all, or not before curl gives up; when the registry is unreachable or its name does not resolve; when the credentialed call
// fails or exits 0 with nothing.
// PART II -- pinned on every row: the exit code and the message; docker is called exactly twice,
// `manifest inspect <ref>`, never with DOCKER_CONFIG; every curl call has -q first and
// --proto =https and no -u/--user/--netrc/Basic; no request the stub saw carries anything but
// the anonymous token (the token request carries nothing); the token is asked for with
// service=harbor-registry and scope=repository:<repo>:pull (the public rows are green only if
// it is, so a misspelt scope -- which would make every repository look private -- is red); no
// anonymous request for a ref whose credentialed call failed; the credentialed call's output is
// never printed (a canary). No ref: exit 2.
// PART III: a form not on this list is code review's subject; no completeness claim.
//
// MUTANT (measured, OPS-2 fix round 1): the 401 arm accepting any 401 (no UNAUTHORIZED check);
// the 200 arm deleted; the token check reduced to the HTTP code (an empty token passes); the
// scope spelt `repository:$repo:push`; -q dropped from anon_get; the `*)` arm of /v2/ made a
// refusal; the anonymous half put back on `DOCKER_CONFIG=... docker manifest inspect`; the
// `"schemaVersion"` check on the credentialed half reduced to the exit code -> each red.
func TestVerifyImage_TheAccessGateMeasuresBothDirections(t *testing.T) {
	realCurl, err := exec.LookPath("curl")
	if err != nil {
		t.Fatalf("no curl on PATH (%v): the gate's anonymous half is curl, and so is this test's", err)
	}
	dir := t.TempDir()
	for name, body := range map[string]string{"docker": stubDocker, "curl": stubRegistryCurl} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
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
	appRepo, migRepo := "tappa/tappa", "tappa/tappa-migrate"
	app, mig := harborRegistry+"/"+appRepo+":sha-0123456789ab", harborRegistry+"/"+migRepo+":sha-0123456789ab"
	manifest := `{"schemaVersion": 2, "mediaType": "application/vnd.oci.image.index.v1+json", "manifests": []}`
	var (
		mu   sync.Mutex
		seen []harborRequest
		ans  harborAnswer
	)
	srv := harborStub(t, &ans, &seen, &mu)
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	run := func(stubHost, stubPort, tail string, args ...string) (string, int) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "bash", append([]string{scriptPathIn(t, "verify-image.sh")}, args...)...)
		var env []string
		for _, e := range os.Environ() {
			if !strings.HasPrefix(e, "DOCKER_CONFIG=") && !strings.HasPrefix(e, "PATH=") {
				env = append(env, e)
			}
		}
		cmd.Env = append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "STUB_DIR="+dir,
			"REAL_CURL="+realCurl, "STUB_HOST="+stubHost, "STUB_PORT="+stubPort, "STUB_CURL_TAIL="+tail)
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
	notReached := "NOT the 401 UNAUTHORIZED a private repository answers"
	for _, tc := range []struct {
		name         string
		ans          harborAnswer
		auth, authRC string
		unreachable  bool
		noDNS        bool
		tail         string
		wantCode     int
		wantMarks    []string
		notMark      string
		wantAnonRefs int // refs whose anonymous half ran
	}{
		{name: "Harbor (measured): /v2/ 401, an anonymous token, the manifest 401 UNAUTHORIZED",
			wantMarks: []string{"refused: " + app + " (HTTP 401 UNAUTHORIZED)", "refused: " + mig + " (HTTP 401 UNAUTHORIZED)", "2 image(s) pullable with the registry credential and refused without one"}, wantAnonRefs: 2},
		{name: "/v2/ asks nothing, the manifest 401 UNAUTHORIZED", ans: harborAnswer{v2Code: 200},
			wantMarks: []string{"refused: " + app}, wantAnonRefs: 2},
		{name: "both public: the anonymous token pulls", ans: harborAnswer{public: []string{appRepo, migRepo}}, wantCode: 1,
			wantMarks: []string{app + " IS pullable WITHOUT a credential", mig + " IS pullable WITHOUT a credential"}, wantAnonRefs: 2},
		{name: "only the migration image public", ans: harborAnswer{public: []string{migRepo}}, wantCode: 1,
			wantMarks: []string{"refused: " + app, mig + " IS pullable WITHOUT"}, notMark: app + " IS pullable", wantAnonRefs: 2},
		{name: "/v2/ asks nothing and the manifest is 200", ans: harborAnswer{v2Code: 200, public: []string{appRepo, migRepo}}, wantCode: 1,
			wantMarks: []string{app + " IS pullable WITHOUT"}, wantAnonRefs: 2},
		{name: "/v2/ 503", ans: harborAnswer{v2Code: 503}, wantCode: 1, wantMarks: []string{"/v2/ answered HTTP 503", "neither a 401 challenge nor 200"}, wantAnonRefs: 2},
		{name: "/v2/ 401 without a Bearer challenge", ans: harborAnswer{v2Challenge: `Basic realm="harbor"`}, wantCode: 1,
			wantMarks: []string{"but no Bearer challenge"}, wantAnonRefs: 2},
		{name: "the token service 401", ans: harborAnswer{tokenCode: 401}, wantCode: 1,
			wantMarks: []string{"did not hand out an anonymous token (HTTP 401"}, wantAnonRefs: 2},
		{name: "the token service 200 without a token", ans: harborAnswer{tokenBody: `{"expires_in":1800}`}, wantCode: 1,
			wantMarks: []string{"did not hand out an anonymous token (HTTP 200"}, wantAnonRefs: 2},
		{name: "the manifest 503", ans: harborAnswer{manCode: 503, manBody: `{"errors":[{"code":"UNAVAILABLE"}]}`}, wantCode: 1,
			wantMarks: []string{"got HTTP 503 (registry code UNAVAILABLE", notReached}, wantAnonRefs: 2},
		{name: "the manifest 404 MANIFEST_UNKNOWN", ans: harborAnswer{manCode: 404, manBody: `{"errors":[{"code":"MANIFEST_UNKNOWN","message":"manifest unknown"}]}`}, wantCode: 1,
			wantMarks: []string{"got HTTP 404 (registry code MANIFEST_UNKNOWN", notReached}, wantAnonRefs: 2},
		{name: "the manifest 403 DENIED", ans: harborAnswer{manCode: 403, manBody: `{"errors":[{"code":"DENIED","message":"requested access to the resource is denied"}]}`}, wantCode: 1,
			wantMarks: []string{"got HTTP 403 (registry code DENIED", notReached}, wantAnonRefs: 2},
		{name: "the manifest 401 with no error code", ans: harborAnswer{manCode: 401, manBody: ""}, wantCode: 1,
			wantMarks: []string{"got HTTP 401 (registry code none", notReached}, wantAnonRefs: 2},
		{name: "the manifest: no answer at all", ans: harborAnswer{manEmpty: true}, wantCode: 1,
			wantMarks: []string{"got HTTP 000", notReached}, wantAnonRefs: 2},
		{name: "the manifest: curl gives up (timeout)", ans: harborAnswer{manSlow: true}, tail: "--max-time 1", wantCode: 1,
			wantMarks: []string{"got HTTP 000", "curl exit 28", notReached}, wantAnonRefs: 2},
		{name: "the registry unreachable", unreachable: true, wantCode: 1,
			wantMarks: []string{"/v2/ answered HTTP 000", "curl exit 7"}, wantAnonRefs: 2},
		{name: "the registry's name does not resolve", noDNS: true, wantCode: 1,
			wantMarks: []string{"/v2/ answered HTTP 000", "curl exit 6"}, wantAnonRefs: 2},
		{name: "credentialed: refused", auth: "unauthorized: " + authCanary, authRC: "1", wantCode: 1,
			wantMarks: []string{"is NOT pullable with the credential this job logged in with (docker manifest inspect exit 1"}, wantAnonRefs: 0},
		{name: "credentialed: exit 0, nothing", auth: "", authRC: "0", wantCode: 1,
			wantMarks: []string{"exit 0; its output is not shown"}, wantAnonRefs: 0},
	} {
		reset()
		if tc.authRC == "" {
			tc.auth, tc.authRC = manifest, "0"
		}
		set("auth.out", tc.auth)
		set("auth.rc", tc.authRC)
		mu.Lock()
		ans, seen = tc.ans, nil
		mu.Unlock()
		stubHost, stubPort := "127.0.0.1", strconv.Itoa(port)
		if tc.unreachable {
			stubPort = "1"
		}
		if tc.noDNS {
			stubHost = "no-such-host.invalid"
		}
		out, code := run(stubHost, stubPort, tc.tail, "--access", app, mig)
		if code != tc.wantCode {
			t.Errorf("%s: exit %d, want %d:\n%s", tc.name, code, tc.wantCode, out)
		}
		for _, m := range tc.wantMarks {
			if !strings.Contains(out, m) {
				t.Errorf("%s: %q not in:\n%s", tc.name, m, out)
			}
		}
		if tc.notMark != "" && strings.Contains(out, tc.notMark) {
			t.Errorf("%s: %q in:\n%s", tc.name, tc.notMark, out)
		}
		if strings.Contains(out, authCanary) {
			t.Errorf("%s: the credentialed call's output was printed:\n%s", tc.name, out)
		}

		b, _ := os.ReadFile(filepath.Join(dir, "docker.calls"))
		calls := strings.Split(strings.TrimSpace(string(b)), "\n")
		if len(calls) != 2 || calls[0] != "auth|manifest inspect "+app || calls[1] != "auth|manifest inspect "+mig {
			t.Errorf("%s: docker calls %q, want exactly `manifest inspect` on each ref, without DOCKER_CONFIG", tc.name, calls)
		}
		b, _ = os.ReadFile(filepath.Join(dir, "curl.calls"))
		var curls []string
		if s := strings.TrimSpace(string(b)); s != "" {
			curls = strings.Split(s, "\n")
		}
		for _, c := range curls {
			f := strings.Fields(c)
			if len(f) == 0 || f[0] != "-q" || !strings.Contains(c, "--proto =https") {
				t.Errorf("%s: curl called as %q: want -q first (no ~/.curlrc) and --proto =https", tc.name, c)
			}
			for _, never := range []string{" -u ", " --user", "--netrc", " -n ", "Basic"} {
				if strings.Contains(" "+c+" ", never) {
					t.Errorf("%s: curl called with %q: the anonymous half carries no credential:\n%s", tc.name, never, c)
				}
			}
		}
		if (len(curls) == 0) != (tc.wantAnonRefs == 0) {
			t.Errorf("%s: %d curl call(s), want the anonymous half to run for %d ref(s)", tc.name, len(curls), tc.wantAnonRefs)
		}
		mu.Lock()
		got := slices.Clone(seen)
		mu.Unlock()
		manifests := map[string]bool{}
		for _, r := range got {
			switch {
			case r.path == "/service/token":
				q, _ := url.ParseQuery(r.query)
				if r.auth != "" || q.Get("service") != "harbor-registry" || (q.Get("scope") != "repository:"+appRepo+":pull" && q.Get("scope") != "repository:"+migRepo+":pull") {
					t.Errorf("%s: the token was asked for with Authorization %q and %q; want none, service=harbor-registry and "+
						"scope=repository:<repo>:pull", tc.name, r.auth, r.query)
				}
			case strings.Contains(r.path, "/manifests/"):
				manifests[r.path] = true
				if r.auth != "" && !strings.HasPrefix(r.auth, "Bearer ANONYMOUS") && !strings.HasPrefix(r.auth, "Bearer GRANT.") {
					t.Errorf("%s: the manifest was asked for with Authorization %q, want only the anonymous token", tc.name, r.auth)
				}
			default:
				if r.auth != "" {
					t.Errorf("%s: %s was asked for with Authorization %q", tc.name, r.path, r.auth)
				}
			}
		}
		if !tc.unreachable && !tc.noDNS && tc.wantAnonRefs > 0 && tc.ans.v2Code == 0 && tc.ans.v2Challenge == "" && tc.ans.tokenCode == 0 && tc.ans.tokenBody == "" &&
			(!manifests["/v2/"+appRepo+"/manifests/sha-0123456789ab"] || !manifests["/v2/"+migRepo+"/manifests/sha-0123456789ab"]) {
			t.Errorf("%s: the stub saw manifest requests %v, want one for each image", tc.name, manifests)
		}
	}
	if out, code := run("127.0.0.1", strconv.Itoa(port), "", "--access"); code != 2 {
		t.Errorf("--access with no ref: exit %d, want 2\n%s", code, out)
	}
}

// stubMigrateKubectl answers the kubectl calls of deploy.yml's "Migrate" step. Each poll of the
// loop reads `.status.succeeded` first, which advances STUB_DIR/poll; the poll's answers are
// succeeded.N, failed.N and pull.N (the pod's container=reason list). From poll 6 on, the Job
// reports failed=3, so a loop that does not end by itself ends there instead of hanging the test.
const stubMigrateKubectl = `#!/bin/sh
printf '%s\n' "$*" >> "$STUB_DIR/kubectl.calls"
n=$(cat "$STUB_DIR/poll" 2>/dev/null || echo 0)
case "$*" in
  *"apply -f -"*) cat > "$STUB_DIR/applied.yaml" ;;
  *"get job tappa-migrate"*"succeeded"*)
    n=$((n + 1)); echo "$n" > "$STUB_DIR/poll"
    if [ -f "$STUB_DIR/succeeded.$n" ]; then cat "$STUB_DIR/succeeded.$n"; fi ;;
  *"get job tappa-migrate"*"failed"*)
    if [ "$n" -ge 6 ]; then printf 3; elif [ -f "$STUB_DIR/failed.$n" ]; then cat "$STUB_DIR/failed.$n"; fi ;;
  *"get pod -l job-name=tappa-migrate"*"waiting.reason"*)
    if [ -f "$STUB_DIR/pull.$n" ]; then cat "$STUB_DIR/pull.$n"; fi ;;
  *"get pod -l job-name=tappa-migrate"*"waiting.message"*)
    echo "goose: Back-off pulling image \"registry.everva.com.tr/tappa/tappa-migrate:sha-0123456789ab\"" ;;
esac
exit 0
`

// TestPackaging_TheMigrationWaitEndsOnAnImageThatCannotBePulled RUNS the body of deploy.yml's
// "Migrate" step under bash, from the repository root, with stubMigrateKubectl and a no-op sleep
// (OPS-2 fix round 1: a pod in ImagePullBackOff never counts as failed, so the wait used to run
// its full 600 s and then report a timeout).
//
// PART I -- the rows: RED (exit 1) at the FIRST poll when the goose container or the
// wait-for-postgres init container is in ImagePullBackOff, naming it and printing the kubelet's
// message; GREEN (exit 0) when the pod shows ErrImagePull on the first poll and the Job succeeds on
// the second (a pull that fails once and is retried is the kubelet's business, not a verdict);
// GREEN at the first poll when the Job has succeeded; RED with "the migration Job failed" when it
// reports three failures. PART II -- pinned on every row: the exit code, the message, the number of
// polls; the applied manifest names the migration image with this deploy's tag.
//
// MUTANT (measured, OPS-2 fix round 1): the ImagePullBackOff case deleted (the loop runs to the
// stub's failed=3); its pattern widened to ErrImagePull; the `exit 1` in it dropped -> each red.
func TestPackaging_TheMigrationWaitEndsOnAnImageThatCannotBePulled(t *testing.T) {
	steps := deploySteps(t)
	body := runBlockOf(t, steps[stepIndex(t, steps, "Migrate (its own step, its own role, before any new binary serves)")])
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, src := range map[string]string{"kubectl": stubMigrateKubectl, "sleep": "#!/bin/sh\nexit 0\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name      string
		answers   map[string]string
		wantCode  int
		wantMarks []string
		notMark   string
		wantPolls int
	}{
		{name: "goose in ImagePullBackOff", answers: map[string]string{"pull.1": "wait-for-postgres= goose=ImagePullBackOff "},
			wantCode: 1, wantMarks: []string{"cannot pull an image (container=reason:", "goose=ImagePullBackOff", "goose: Back-off pulling image"}, wantPolls: 1},
		{name: "the init container in ImagePullBackOff", answers: map[string]string{"pull.1": "wait-for-postgres=ImagePullBackOff "},
			wantCode: 1, wantMarks: []string{"wait-for-postgres=ImagePullBackOff"}, wantPolls: 1},
		{name: "ErrImagePull once, then success", answers: map[string]string{"pull.1": "wait-for-postgres= goose=ErrImagePull ", "succeeded.2": "1"},
			wantMarks: []string{"migration complete"}, notMark: "cannot pull", wantPolls: 2},
		{name: "succeeded at once", answers: map[string]string{"succeeded.1": "1"}, wantMarks: []string{"migration complete"}, wantPolls: 1},
		{name: "three failures", answers: map[string]string{"failed.1": "3"}, wantCode: 1, wantMarks: []string{"the migration Job failed"}, notMark: "cannot pull", wantPolls: 1},
	} {
		old, _ := filepath.Glob(filepath.Join(dir, "*.*"))
		old = append(old, filepath.Join(dir, "poll"))
		for _, f := range old {
			_ = os.Remove(f)
		}
		for k, v := range tc.answers {
			if err := os.WriteFile(filepath.Join(dir, k), []byte(v), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		cmd := exec.CommandContext(t.Context(), bash, "-c", body)
		cmd.Dir = repoRoot
		cmd.Env = []string{"PATH=" + dir + string(os.PathListSeparator) + "/usr/bin:/bin", "STUB_DIR=" + dir, "SHORT=0123456789ab", "LC_ALL=C"}
		outB, err := cmd.CombinedOutput()
		out, code := string(outB), 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatal(err)
		}
		if code != tc.wantCode {
			t.Errorf("%s: exit %d, want %d:\n%s", tc.name, code, tc.wantCode, out)
		}
		for _, m := range tc.wantMarks {
			if !strings.Contains(out, m) {
				t.Errorf("%s: %q not in:\n%s", tc.name, m, out)
			}
		}
		if tc.notMark != "" && strings.Contains(out, tc.notMark) {
			t.Errorf("%s: %q in:\n%s", tc.name, tc.notMark, out)
		}
		if p, _ := os.ReadFile(filepath.Join(dir, "poll")); strings.TrimSpace(string(p)) != strconv.Itoa(tc.wantPolls) {
			t.Errorf("%s: %s poll(s), want %d", tc.name, strings.TrimSpace(string(p)), tc.wantPolls)
		}
		if a, _ := os.ReadFile(filepath.Join(dir, "applied.yaml")); !strings.Contains(string(a), "image: "+harborProject+"/tappa-migrate:sha-0123456789ab") {
			t.Errorf("%s: the applied Job does not name the migration image with this deploy's tag", tc.name)
		}
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
