package main

// sandboxbuild_test.go -- M11 API-11 (ADR 0028 §11, K-3, K-28): the sandbox is a separate BUILD of
// this command, and these tests hold the split -- in the dependency graph, in the binaries, in the
// nine hardened gates, in CI and at the image gate. They run in both builds; what a build can only
// say about itself is asked of the real artifacts, built here from the Makefile's own recipes.

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/internal/webhook"
)

const sandboxPackage = "github.com/atknatk/tappa/internal/sandbox"

// TestPackaging_ProdBuildHasNoSandboxPackage (ADR 0028 §11: "prod derlemesi için go list -deps
// negatif"): the untagged command's dependency graph does not contain internal/sandbox -- the
// simulation's code is not in the production binary at all (a symbol check would be blind:
// -ldflags=-s -w strips the table). And no production file but cmd/tappa/sandbox.go imports it,
// and that file is behind `//go:build sandbox`.
//
// MUTANTS (measured): an import of internal/sandbox added to nosandbox.go -> red; sandbox.go's
// constraint removed or inverted to `//go:build !sandbox` -> red (the two halves then both declare
// apiKeyPrefix and the package does not build).
func TestPackaging_ProdBuildHasNoSandboxPackage(t *testing.T) {
	deps := goList(t, "-deps", "./cmd/tappa")
	if !contains(deps, "github.com/atknatk/tappa/internal/handler/api") {
		t.Fatalf("CONTROL FAILED: the graph (%d packages) does not contain internal/handler/api", len(deps))
	}
	if contains(deps, sandboxPackage) {
		t.Fatalf("the production build links %s: the tap simulation would be in the production binary", sandboxPackage)
	}
	importers := 0
	for _, f := range productionSources(t) {
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly|parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) != sandboxPackage {
				continue
			}
			importers++
			rel, _ := filepath.Rel(repoRoot, f)
			if rel != filepath.Join("cmd", "tappa", "sandbox.go") {
				t.Errorf("%s imports %s; only cmd/tappa/sandbox.go may (ADR 0028 §13)", rel, sandboxPackage)
			}
			if len(file.Comments) == 0 || file.Comments[0].List[0].Text != "//go:build sandbox" {
				t.Errorf("%s imports %s without `//go:build sandbox` as its first line", rel, sandboxPackage)
			}
		}
	}
	if importers != 1 {
		t.Errorf("%s has %d importers, want exactly 1 (cmd/tappa/sandbox.go)", sandboxPackage, importers)
	}
}

// TestPackaging_SandboxBuildHasIt (ADR 0028 §11: "sandbox için pozitif"): with the tag the graph
// contains internal/sandbox -- the positive control of the test above, and the proof that the
// sandbox build is the one that carries the simulation.
//
// MUTANT (measured): sandbox.go's import of internal/sandbox removed (sandboxExtensions returning
// nil, nil) -> red.
func TestPackaging_SandboxBuildHasIt(t *testing.T) {
	deps := goList(t, "-tags", "sandbox", "-deps", "./cmd/tappa")
	if !contains(deps, sandboxPackage) {
		t.Fatalf("the sandbox build (%d packages) does not link %s", len(deps), sandboxPackage)
	}
}

var (
	sandboxBuildOnce sync.Once
	sandboxBuilt     string
	sandboxBuildErr  error
)

// sandboxArtifact builds the command THE WAY THE MAKEFILE'S build-sandbox TARGET DOES (theArtifact's
// reason: the recipe is read, not copied) and returns its path. Shared between tests; TestMain
// removes it.
func sandboxArtifact(t *testing.T) string {
	t.Helper()
	sandboxBuildOnce.Do(buildTheSandboxArtifact)
	if sandboxBuildErr != nil {
		t.Fatalf("building the sandbox artifact: %v", sandboxBuildErr)
	}
	return sandboxBuilt
}

// buildTheSandboxArtifact is sandboxArtifact's build, run once (sandboxBuildOnce) -- by the first
// test that needs it or, earlier, by TestMain's prefetch (prefetch_test.go).
func buildTheSandboxArtifact() {
	recipe, err := makefileRecipe("build-sandbox")
	if err != nil {
		sandboxBuildErr = err
		return
	}
	out := filepath.Join(os.TempDir(), fmt.Sprintf("tappa-sandbox-%d", os.Getpid()))
	cmdline := strings.ReplaceAll(recipe, "$(BIN_SANDBOX)", out)
	if strings.Contains(cmdline, "$(") {
		sandboxBuildErr = fmt.Errorf("the build-sandbox recipe uses a variable this test cannot expand: %q", cmdline)
		return
	}
	cmd := exec.Command("/bin/sh", "-c", cmdline)
	cmd.Dir = repoRoot
	if combined, err := cmd.CombinedOutput(); err != nil {
		sandboxBuildErr = fmt.Errorf("running the build-sandbox recipe (%s): %w\n%s", cmdline, err, combined)
		return
	}
	sandboxBuilt = out
}

// makefileRecipe returns the single command line under the Makefile's target.
func makefileRecipe(target string) (string, error) {
	b, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, target+":") {
			continue
		}
		var recipe []string
		for _, next := range lines[i+1:] {
			if !strings.HasPrefix(next, "\t") {
				break
			}
			recipe = append(recipe, strings.TrimSpace(next))
		}
		if len(recipe) == 0 {
			return "", fmt.Errorf("the %s target has no recipe", target)
		}
		return strings.Join(recipe, "\n"), nil
	}
	return "", fmt.Errorf("no `%s:` target in the Makefile", target)
}

// bootEnv is an environment that config.Load accepts in the build whose TAPPA_ENV it names (the
// sandbox's e-mail flows are off by default), with the database on a closed port: a boot that gets
// past configuration fails on the dial.
func bootEnv(env string) []string {
	key := func(c byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{c}, 32)) }
	out := []string{
		"PATH=" + os.Getenv("PATH"),
		"DATABASE_URL=postgres://tappa_app@127.0.0.1:1/tappa?sslmode=disable",
		"TAPPA_ADDR=127.0.0.1:0",
		"TAPPA_SESSION_HMAC_KEY=" + key('S'), "TAPPA_TAG_KEK=" + key('K'), "TAPPA_INVITE_HMAC_KEY=" + key('I'),
		"TAPPA_API_KEY_HMAC_KEY=" + key('A'), "TAPPA_WEBHOOK_KEK=" + key('W'),
		"TAPPA_WEBHOOK_BLOCKED_ADDRESSES=203.0.113.10", "TAPPA_RETENTION_YEARS=2",
	}
	if env != "" {
		out = append(out, "TAPPA_ENV="+env)
	}
	return out
}

func boot(t *testing.T, bin, env string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin)
	cmd.Dir = t.TempDir()
	cmd.Env = bootEnv(env)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("%s with TAPPA_ENV=%q exited 0 against a closed database port", filepath.Base(bin), env)
	}
	return string(out)
}

// TestConfig_UntaggedBinaryRefusesSandbox (ADR 0028 §11: "tag'siz ikili sandbox değerini reddeder";
// "sandbox ikilisi yalnız TAPPA_ENV = sandbox ile başlar (boş değerin dev'e düşmesi kapanır)"),
// measured on the two REAL artifacts: the production build (the Makefile's `build`) stops with
// TAPPA_ENV=sandbox at configuration, before the database is dialled; the sandbox build (the
// Makefile's `build-sandbox`) stops at configuration with prod, staging, dev and an unset value.
// CONTROLS: the production build with prod, and the sandbox build with sandbox, pass configuration
// and stop on the closed database port -- so each refusal above is the build's, not a missing
// setting.
//
// MUTANTS (measured): envForBuild's untagged branch removed (the production binary boots to the
// dial with sandbox -- red); the sandbox branch's empty value let through (the unset row reaches the
// dial -- red); build_sandbox.go's constant set to false (the sandbox control refuses -- red).
func TestConfig_UntaggedBinaryRefusesSandbox(t *testing.T) {
	prod, sandbox := theArtifact(t), sandboxArtifact(t)
	const dial = "db: "
	if out := boot(t, prod, config.EnvSandbox); !strings.Contains(out, "built without the sandbox tag") || strings.Contains(out, dial) {
		t.Errorf("the production binary with TAPPA_ENV=sandbox did not stop at configuration:\n%s", out)
	}
	if out := boot(t, prod, config.EnvProd); strings.Contains(out, "config:") || !strings.Contains(out, dial) {
		t.Errorf("CONTROL: the production binary with TAPPA_ENV=prod did not reach the dial:\n%s", out)
	}
	for _, env := range []string{config.EnvProd, config.EnvStaging, config.EnvDev, ""} {
		if out := boot(t, sandbox, env); !strings.Contains(out, "is the sandbox build") || strings.Contains(out, dial) {
			t.Errorf("the sandbox binary with TAPPA_ENV=%q did not stop at configuration:\n%s", env, out)
		}
	}
	if out := boot(t, sandbox, config.EnvSandbox); strings.Contains(out, "config:") || !strings.Contains(out, dial) {
		t.Errorf("CONTROL: the sandbox binary with TAPPA_ENV=sandbox did not reach the dial:\n%s", out)
	}
}

// hardenedGates are K-28's nine (ADR 0028 Bağlam: "Üretim kodunda dokuz IsProd() çağrısı"), as
// file:function -- the nine that asked "production?" and now ask config.IsHardened.
var hardenedGates = []string{
	"internal/adminauth/cookie.go:NewCookies",
	"internal/config/config.go:Load",
	"internal/config/config.go:loadMail",
	"internal/db/pool.go:newDB",
	"internal/handler/activate.go:NewActivation",
	"internal/handler/cookies.go:newCodeCookies",
	"internal/handler/logincontext.go:newAdminCookies",
	"internal/handler/signupstate.go:newSignupCookies",
	"internal/session/cookie.go:NewCookies",
}

// envComparisons are the production code's comparisons with a TAPPA_ENV value outside
// internal/config (which owns the closed set), as file:function:value. The three EnvDev ones are the
// development gates K-28 leaves closed in the sandbox by being dev-only.
var envComparisons = []string{
	"cmd/tappa/main.go:run:EnvDev",
	"internal/db/pool.go:newDB:EnvSandbox",
	"internal/handler/devtap.go:DevToolsEnabled:EnvDev",
	"internal/sandbox/sandbox.go:New:EnvSandbox",
	"internal/sun/mint.go:WithDevelopmentMinting:EnvDev",
}

// TestConfig_IsHardenedCoversTheNineGates (K-28: "IsHardened() = prod ∨ sandbox; dokuz IsProd()
// çağrısının dokuzu bu yükleme geçer; EnvDev'e bağlı üç kapı sandbox'ta kapalı kalır"):
//
//	the code      every call of IsHardened in production code is one of the nine, each of the nine
//	              calls it, nothing calls a method named IsProd (there is none: config removed it),
//	              and every comparison with a TAPPA_ENV value outside internal/config is a known one
//	the behaviour a sandbox configuration is the production one where a gate is reachable from
//	              here: the session and panel cookies are Secure over plain http; the dev tap
//	              simulator and dev minting are off even with TAPPA_DEV_TOOLS set and a loopback
//	              base URL (CONTROL: the same configuration as dev turns both on)
//
// MUTANTS (measured): one gate written back as `cfg.Env == config.EnvProd` (the code half: eight
// calls and an unknown comparison -- red); IsHardened = `Env == EnvProd` (the cookie rows -- red);
// DevToolsEnabled's `!= config.EnvDev` replaced by `== config.EnvProd` (the dev-tools row -- red).
func TestConfig_IsHardenedCoversTheNineGates(t *testing.T) {
	var calls, comparisons, prodCalls []string
	for _, f := range productionSources(t) {
		rel, _ := filepath.Rel(repoRoot, f)
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range file.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			where := rel + ":" + fd.Name.Name
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
						switch sel.Sel.Name {
						case "IsHardened":
							calls = append(calls, where)
						case "IsProd":
							prodCalls = append(prodCalls, where)
						}
					}
				case *ast.BinaryExpr:
					if strings.HasPrefix(rel, filepath.Join("internal", "config")+string(filepath.Separator)) {
						return true
					}
					for _, side := range []ast.Expr{x.X, x.Y} {
						if sel, ok := side.(*ast.SelectorExpr); ok && isIdent(sel.X, "config") && strings.HasPrefix(sel.Sel.Name, "Env") {
							comparisons = append(comparisons, where+":"+sel.Sel.Name)
						}
					}
				}
				return true
			})
		}
	}
	sort.Strings(calls)
	sort.Strings(comparisons)
	if !slices.Equal(calls, hardenedGates) {
		t.Errorf("the IsHardened calls in production code are\n  %v\nwant the nine\n  %v", calls, hardenedGates)
	}
	if len(prodCalls) > 0 {
		t.Errorf("production code calls IsProd at %v", prodCalls)
	}
	if !slices.Equal(comparisons, envComparisons) {
		t.Errorf("the TAPPA_ENV comparisons outside internal/config are\n  %v\nwant\n  %v", comparisons, envComparisons)
	}

	sandbox := &config.Config{Env: config.EnvSandbox, BaseURL: "http://localhost:8080", DevTools: true}
	dev := &config.Config{Env: config.EnvDev, BaseURL: "http://localhost:8080", DevTools: true}
	if !session.NewCookies(sandbox).Secure() || !adminauth.NewCookies(sandbox).Secure() {
		t.Error("a sandbox over plain http gets a session or panel cookie without Secure")
	}
	if session.NewCookies(dev).Secure() {
		t.Error("CONTROL: a development configuration over plain http is Secure; the cookie row is blind")
	}
	if handler.DevToolsEnabled(sandbox) || !handler.DevToolsEnabled(dev) {
		t.Errorf("the dev tap simulator: sandbox %v (want off), dev %v (want on)", handler.DevToolsEnabled(sandbox), handler.DevToolsEnabled(dev))
	}
	v := sun.NewVerifier(nil, make([]byte, 32))
	if _, err := v.WithDevelopmentMinting(sandbox); !errors.Is(err, sun.ErrDevMintingDisabled) {
		t.Errorf("dev minting in the sandbox: %v, want ErrDevMintingDisabled", err)
	}
	if _, err := v.WithDevelopmentMinting(dev); err != nil {
		t.Errorf("CONTROL: dev minting in development: %v", err)
	}
	if (&config.Config{Env: config.EnvSandbox}).IsHardened() != true || (&config.Config{Env: config.EnvDev}).IsHardened() {
		t.Error("IsHardened is not prod-or-sandbox")
	}
}

// sandboxTaggedPackages are the packages of this tree with a file whose build constraint names the
// sandbox tag -- the packages the tag changes -- as ./ paths, plus internal/sandbox, which only the
// sandbox build links.
func sandboxTaggedPackages(t *testing.T) []string {
	t.Helper()
	set := map[string]bool{"./internal/sandbox": true}
	err := filepath.WalkDir(repoRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n != "." && (strings.HasPrefix(n, ".") || n == "android" || n == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(line, "package ") {
				break
			}
			if strings.HasPrefix(line, "//go:build") && strings.Contains(line, "sandbox") {
				rel, _ := filepath.Rel(repoRoot, filepath.Dir(path))
				set["./"+filepath.ToSlash(rel)] = true
				break
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// TestCI_RunsVetAndTestWithTheSandboxTag (ADR 0028 "Test edilecekler", API-11: "iş akışı go vet
// -tags sandbox ./... ve go test -tags sandbox'ı da koşar"): ci.yml runs `make check-sandbox` as its
// own step, after `make check`, with no `if:` and no continue-on-error; the Makefile's
// check-sandbox vets and staticchecks the WHOLE tree with the tag and tests, with the tag and -race,
// a package list that covers every package the tag changes -- recomputed here from the tree, so a
// sandbox-tagged file added to a package outside the list is red. (Why not ./... for the tests:
// the Makefile's block and ADR 0028's API-11 note -- every other package is the same code in both
// builds.)
//
// MUTANTS (measured): the step's run replaced; `if: false` on the step; `-tags sandbox` dropped from
// the vet line or the test line; ./cmd/tappa/... dropped from SANDBOX_TEST_PKGS; a `//go:build
// sandbox` file added under internal/httpx -- each red.
func TestCI_RunsVetAndTestWithTheSandboxTag(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
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
		if len(steps) > 0 {
			steps[len(steps)-1].code = append(steps[len(steps)-1].code, l)
		}
	}
	runs := func(cmd string) []int {
		var out []int
		for i, s := range steps {
			if slices.ContainsFunc(s.code, func(l string) bool { return strings.TrimSpace(l) == "run: "+cmd }) {
				out = append(out, i)
			}
		}
		return out
	}
	check, sandbox := runs("make check"), runs("make check-sandbox")
	if len(check) != 1 {
		t.Fatalf("CONTROL FAILED: ci.yml runs `make check` in %d steps", len(check))
	}
	if len(sandbox) != 1 {
		t.Fatalf("ci.yml runs `make check-sandbox` in %d steps, want 1", len(sandbox))
	}
	if sandbox[0] < check[0] {
		t.Errorf("`make check-sandbox` (step %d) runs before `make check` (step %d)", sandbox[0], check[0])
	}
	for _, l := range steps[sandbox[0]].code {
		if tl := strings.TrimSpace(l); strings.HasPrefix(tl, "if:") || strings.HasPrefix(tl, "continue-on-error:") {
			t.Errorf("the sandbox step carries %q", tl)
		}
	}
	recipe, err := makefileRecipe("check-sandbox")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(recipe, "\n")
	if !slices.Contains(lines, "go vet -tags sandbox ./...") {
		t.Errorf("check-sandbox does not vet the whole tree with the tag: %q", recipe)
	}
	if !slices.ContainsFunc(lines, func(l string) bool {
		return strings.Contains(l, "staticcheck") && strings.HasSuffix(l, "-tags sandbox ./...")
	}) {
		t.Errorf("check-sandbox does not staticcheck the whole tree with the tag: %q", recipe)
	}
	test := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, "go test ") })
	if test < 0 || !strings.Contains(lines[test], " -tags sandbox ") || !strings.Contains(lines[test], " -race ") ||
		!strings.HasSuffix(lines[test], "$(SANDBOX_TEST_PKGS)") {
		t.Fatalf("check-sandbox's test line is not `go test -race ... -tags sandbox $(SANDBOX_TEST_PKGS)`: %q", recipe)
	}
	mk, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, l := range strings.Split(string(mk), "\n") {
		if v, ok := strings.CutPrefix(l, "SANDBOX_TEST_PKGS := "); ok {
			for _, p := range strings.Fields(v) {
				listed = append(listed, strings.TrimSuffix(p, "/..."))
			}
		}
	}
	for _, want := range sandboxTaggedPackages(t) {
		if !slices.ContainsFunc(listed, func(p string) bool { return p == want || strings.HasPrefix(want, p+"/") || p == "." }) {
			t.Errorf("%s has a sandbox-tagged file (or is the sandbox's) and SANDBOX_TEST_PKGS %v does not test it", want, listed)
		}
	}
	if len(sandboxTaggedPackages(t)) < 3 {
		t.Fatalf("CONTROL FAILED: the tree's sandbox-tagged packages are %v", sandboxTaggedPackages(t))
	}
}

// TestVerifyImage_RefusesAWrongBuildTag (ADR 0028 §11: "deploy kapısı imajın tag'ini doğrular --
// sandbox imajı sandbox tag'siz, prod imajı tag'li ise reddedilir"): scripts/verify-image.sh's tag
// judgment, run on the two real artifacts through the script's --binary-tags mode (the same
// judge_tags function its image modes call): the production build passes as prod and is refused as
// sandbox, the sandbox build the other way round. And the image modes call it: the production
// mode with "prod", --sandbox with "sandbox".
//
// MUTANTS (measured): judge_tags' prod branch accepting the tag, its sandbox branch accepting its
// absence, the word match replaced by a substring match of "sandbox" in another tag (`-tags=nosandbox`)
// -- each red; the image path's judge_tags call deleted -> the code half red.
func TestVerifyImage_RefusesAWrongBuildTag(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is required")
	}
	script := filepath.Join(repoRoot, "scripts", "verify-image.sh")
	judge := func(kind, bin string) (string, bool) {
		cmd := exec.Command("bash", script, "--binary-tags", kind, bin)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		return string(out), err == nil
	}
	prod, sandbox := theArtifact(t), sandboxArtifact(t)
	for _, tc := range []struct {
		kind, bin string
		pass      bool
	}{{"prod", prod, true}, {"prod", sandbox, false}, {"sandbox", sandbox, true}, {"sandbox", prod, false}} {
		out, ok := judge(tc.kind, tc.bin)
		if ok != tc.pass {
			t.Errorf("verify-image.sh --binary-tags %s %s: passed %v, want %v\n%s", tc.kind, filepath.Base(tc.bin), ok, tc.pass, out)
		}
	}
	// A tag list that names another tag containing the word must not read as the sandbox's.
	other := filepath.Join(t.TempDir(), "other")
	build := exec.Command("go", "build", "-tags", "nosandbox,x", "-o", other, "./cmd/tappa")
	build.Dir = repoRoot
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the control binary: %v\n%s", err, out)
	}
	if out, ok := judge("prod", other); !ok {
		t.Errorf("a production binary built with -tags=nosandbox,x was refused as a sandbox build:\n%s", out)
	}
	src, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(codeLinesOf(string(src)), "\n")
	for _, want := range []string{`judge_tags "$stamps" "$kind"`, "kind=prod", "kind=sandbox"} {
		if !strings.Contains(text, want) {
			t.Errorf("verify-image.sh's image path does not carry %q", want)
		}
	}
}

// TestDockerfile_APlainBuildIsTheProductionServer (API-11 fix 1, the third eye's finding 4): a
// `docker build` with no --target builds the Dockerfile's LAST stage, so the last stage must be `app`
// -- the production server, whose binary is `make build`'s bin/tappa -- and `app-sandbox` must come
// before it. A sandbox stage moved to the end would make every untargeted build a sandbox server
// (scripts/verify-image.sh would refuse it as a production image, but only where it runs).
//
// MUTANT (measured): the app-sandbox stage moved after app -- red.
func TestDockerfile_APlainBuildIsTheProductionServer(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	type stage struct {
		name string
		body []string
	}
	var stages []stage
	for _, line := range strings.Split(string(src), "\n") {
		f := strings.Fields(line)
		if len(f) > 0 && strings.EqualFold(f[0], "FROM") {
			name := ""
			if len(f) >= 4 && strings.EqualFold(f[len(f)-2], "AS") {
				name = f[len(f)-1]
			}
			stages = append(stages, stage{name: name})
			continue
		}
		if len(stages) > 0 && len(f) > 0 && !strings.HasPrefix(f[0], "#") {
			stages[len(stages)-1].body = append(stages[len(stages)-1].body, strings.TrimSpace(line))
		}
	}
	index := map[string]int{}
	for i, st := range stages {
		index[st.name] = i
	}
	sb, okSB := index["app-sandbox"]
	app, okApp := index["app"]
	if !okSB || !okApp {
		t.Fatalf("CONTROL: the Dockerfile's stages are %v; want app-sandbox and app among them", index)
	}
	last := stages[len(stages)-1]
	if last.name != "app" || sb > app {
		t.Errorf("the last stage is %q (app-sandbox at %d, app at %d); a plain build must be the production server", last.name, sb, app)
	}
	if !slices.Contains(last.body, "COPY --from=build /src/bin/tappa /tappa") {
		t.Errorf("the last stage does not copy make build's bin/tappa: %q", last.body)
	}
	for _, l := range last.body {
		if strings.Contains(l, "tappa-sandbox") {
			t.Errorf("the last stage carries the sandbox binary: %q", l)
		}
	}
}

// TestSandboxWiring_TheSimulationTakesTheServersPoolConfigAndWorker (ADR 0028 §11; API-11 fix 1): run()
// hands sandboxExtensions its configuration, the pool (db.New's result) and K-19's worker
// (webhook.NewWorker's result), and hands its result to openAPISurface, which gives it to api.New as
// Extensions; sandbox.go builds the simulation from exactly those parameters. The simulation builds
// its tap path itself, with the tap page's constructors over the runner's transaction
// (internal/sandbox's TestNew_TheTapPathIsTheTapPagesCodeOnTheRunner) -- it no longer takes the tap
// page's pool-bound objects, which were the nested connection of the security review's ORTA-1.
//
// MUTANTS (measured): api.New's Extensions dropped; sandbox.go's Data given a fresh &db.DB{}; Work
// given nil -- each red (openAPISurface handed nil instead does not compile: the result is then
// unused).
func TestSandboxWiring_TheSimulationTakesTheServersPoolConfigAndWorker(t *testing.T) {
	parse := func(name string) *ast.File {
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot, "cmd", "tappa", name), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	fn := func(f *ast.File, name string) *ast.FuncDecl {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
				return fd
			}
		}
		t.Fatalf("no func %s", name)
		return nil
	}
	isSel := func(e ast.Expr, pkg, name string) bool {
		sel, ok := e.(*ast.SelectorExpr)
		return ok && sel.Sel.Name == name && isIdent(sel.X, pkg)
	}
	run := fn(parse("main.go"), "run")
	assigned := map[string]string{} // "pkg.Func" -> the identifier run() assigns its result to
	var ext, surface *ast.CallExpr
	extResult := ""
	for _, st := range run.Body.List {
		as, ok := st.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || len(as.Lhs) == 0 {
			continue
		}
		c, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok {
			continue
		}
		lhs, _ := as.Lhs[0].(*ast.Ident)
		switch f := c.Fun.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := f.X.(*ast.Ident); ok && lhs != nil {
				assigned[pkg.Name+"."+f.Sel.Name] = lhs.Name
			}
		case *ast.Ident:
			switch f.Name {
			case "sandboxExtensions":
				if ext != nil {
					t.Fatal("run() calls sandboxExtensions more than once")
				}
				ext = c
				if lhs != nil {
					extResult = lhs.Name
				}
			case "openAPISurface":
				surface = c
			}
		}
	}
	if ext == nil || surface == nil || extResult == "" {
		t.Fatalf("run(): sandboxExtensions %v (result %q), openAPISurface %v -- each a statement of its own body",
			ext != nil, extResult, surface != nil)
	}
	want := []string{"cfg", assigned["db.New"], assigned["webhook.NewWorker"]}
	if len(ext.Args) != 4 {
		t.Fatalf("sandboxExtensions takes %d arguments, want 4 (cfg, the pool, the worker, the logger)", len(ext.Args))
	}
	for i, w := range want {
		if w == "" || identName(ext.Args[i]) != w {
			t.Errorf("sandboxExtensions argument %d is not %q", i, w)
		}
	}
	if len(surface.Args) < 7 || identName(surface.Args[5]) != extResult {
		t.Errorf("openAPISurface is not handed sandboxExtensions' result (%s) as its extensions", extResult)
	}
	open := fn(parse("api.go"), "openAPISurface")
	param := open.Type.Params.List[5].Names[0].Name
	found := false
	ast.Inspect(open.Body, func(n ast.Node) bool {
		if kv, ok := n.(*ast.KeyValueExpr); ok && isIdent(kv.Key, "Extensions") && isIdent(kv.Value, param) {
			found = true
		}
		return true
	})
	if !found {
		t.Errorf("api.New is not given Extensions: %s", param)
	}
	sb := fn(parse("sandbox.go"), "sandboxExtensions")
	var names []string
	for _, f := range sb.Type.Params.List {
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
	}
	fields := map[string]string{}
	ast.Inspect(sb.Body, func(n ast.Node) bool {
		if cl, ok := n.(*ast.CompositeLit); ok && isSel(cl.Type, "sandbox", "Config") {
			for _, e := range cl.Elts {
				kv := e.(*ast.KeyValueExpr)
				if v, ok := kv.Value.(*ast.Ident); ok {
					fields[kv.Key.(*ast.Ident).Name] = v.Name
				}
			}
		}
		return true
	})
	if len(names) != 4 {
		t.Fatalf("sandbox.go's sandboxExtensions has %d parameters, want 4", len(names))
	}
	for k, v := range map[string]string{"App": names[0], "Data": names[1], "Work": names[2], "Log": names[3]} {
		if fields[k] != v {
			t.Errorf("sandbox.go: sandbox.Config's %s is %q, want %q", k, fields[k], v)
		}
	}
}

func identName(e ast.Expr) string {
	if id, ok := e.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// TestSandboxExtensions_AreTheBuilds: the build's sandboxExtensions, called as run() calls it --
// the sandbox build answers the simulation (one extension), the production build none; and the
// sandbox build's refuses a configuration that is not the sandbox's (sandbox.New's own gate).
//
// MUTANT (measured): sandbox.go's sandboxExtensions returning nil, nil before it builds the
// simulation -- the AST pins above stay green, this is red (under -tags sandbox, which CI runs).
func TestSandboxExtensions_AreTheBuilds(t *testing.T) {
	kek := make([]byte, 32)
	cfg := &config.Config{Env: config.EnvSandbox, TagKEK: kek, GPSRadiusMeters: 150, Debounce: time.Minute, Freshness: 3 * time.Minute}
	work, err := webhook.NewWorker(slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	got, err := sandboxExtensions(cfg, &db.DB{}, work, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("sandboxExtensions: %v", err)
	}
	want := 0
	if config.SandboxBuild() {
		want = 1
	}
	if len(got) != want {
		t.Fatalf("this build (sandbox %v) answers %d extensions, want %d", config.SandboxBuild(), len(got), want)
	}
	if config.SandboxBuild() {
		prod := *cfg
		prod.Env = config.EnvProd
		if _, err := sandboxExtensions(&prod, &db.DB{}, work, slog.New(slog.DiscardHandler)); err == nil {
			t.Error("the sandbox build built the simulation for TAPPA_ENV=prod")
		}
	}
}

// productionBuildOnly skips a test that loads the PRODUCTION ConfigMap (TAPPA_ENV=prod) through
// config.Load in the sandbox build, which refuses that value before anything else is read
// (TestConfig_UntaggedBinaryRefusesSandbox); the untagged run covers it.
func productionBuildOnly(t *testing.T) {
	t.Helper()
	if config.SandboxBuild() {
		t.Skip("loads the production ConfigMap, which the sandbox build refuses; the untagged run covers it")
	}
}
