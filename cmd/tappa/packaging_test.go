package main

import (
	"bytes"
	"debug/buildinfo"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// packaging_test.go — the mechanical half of M8-01.
//
// 🔴 WHY THIS FILE EXISTS AT ALL, MEASURED BEFORE A LINE OF IT WAS WRITTEN. Three of
// the card's five acceptance criteria were ALREADY satisfied on 2026-08-15 at
// 5a86f9b: the Makefile already built with CGO_ENABLED=0 -trimpath, the toolchain
// already stamped the commit, no production code touched the disk at runtime, and
// nothing in cmd/ could migrate. Every one of them was satisfied ACCIDENTALLY —
// nothing anywhere would have turned red if a later task had undone any of it. This
// repository has watched that happen: M7-01 shipped a capability that was never
// mounted, M6-01 lost five protections while the suite stayed green, and the
// timezone embed this file pins was one blank import in a package cmd/tappa merely
// happens to reach.
//
// So the question each test here answers is not "is it true today?" but "does a
// change that breaks it come out red?".
//
// ⚠️ WHAT THESE TESTS COST: they run the compiler. The shipped command is built
// once and shared, plus three tiny control programs. Measured on this machine
// (warm cache, `go test -race`): about 6-9 s for the whole file. They are NOT put
// behind -short, deliberately — the Makefile documents that the short suite has
// exactly three skips, and a packaging guard that only runs in CI is a guard the
// person breaking it never sees.

// repoRoot is resolved at package initialisation, BEFORE any test can t.Chdir.
var repoRoot = func() string {
	abs, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		panic("packaging_test: resolving the repository root: " + err.Error())
	}
	return abs
}()

// ---------------------------------------------------------------- the artifact --

var (
	buildOnce sync.Once
	builtPath string
	buildErr  error
	buildCmd  string
)

// theArtifact builds the command THE WAY THE MAKEFILE DOES and returns its path.
//
// 🔴 THE COMMAND IS READ OUT OF THE MAKEFILE RATHER THAN COPIED HERE. A copy would
// be a second spelling of the build, free to keep saying -trimpath long after the
// Makefile stopped — the "second representation nothing keeps in sync" defect this
// repository has already been bitten by. What runs below is the Makefile's own
// recipe with $(BIN) redirected to a temporary file.
//
// It does NOT run `make build`, which would also run gen and css: those are codegen
// prerequisites covered by `make check`, and downloading the Tailwind binary inside
// a unit test would make this file depend on the network.
func theArtifact(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		recipe, err := makefileBuildRecipe()
		if err != nil {
			buildErr = err
			return
		}
		out := filepath.Join(os.TempDir(), fmt.Sprintf("tappa-packaging-%d", os.Getpid()))
		cmdline := strings.ReplaceAll(recipe, "$(BIN)", out)
		if strings.Contains(cmdline, "$(") {
			buildErr = fmt.Errorf("the Makefile's build recipe uses a variable this test cannot expand: %q", cmdline)
			return
		}
		buildCmd = cmdline
		cmd := exec.Command("/bin/sh", "-c", cmdline)
		cmd.Dir = repoRoot
		if combined, err := cmd.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("running the Makefile's build recipe (%s): %w\n%s", cmdline, err, combined)
			return
		}
		builtPath = out
	})
	if buildErr != nil {
		t.Fatalf("building the shipped artifact: %v", buildErr)
	}
	// No per-test cleanup: the binary is shared between tests and removed by TestMain.
	return builtPath
}

func TestMain(m *testing.M) {
	code := m.Run()
	if builtPath != "" {
		_ = os.Remove(builtPath)
	}
	os.Exit(code)
}

// makefileBuildRecipe returns the single command line under the Makefile's `build:`
// target.
func makefileBuildRecipe() (string, error) {
	b, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		return "", err
	}
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(line, "build:") {
			continue
		}
		var recipe []string
		for _, next := range lines[i+1:] {
			if !strings.HasPrefix(next, "\t") {
				break
			}
			recipe = append(recipe, strings.TrimSpace(next))
		}
		if len(recipe) != 1 {
			return "", fmt.Errorf("the build target has %d recipe lines (%v); this test expects one", len(recipe), recipe)
		}
		return recipe[0], nil
	}
	return "", fmt.Errorf("no `build:` target in the Makefile")
}

// TestPackaging_TheBuildRecipeIsTheOneTheCardRequires reads the recipe and checks
// the flags the deployment depends on.
//
// This is the CHEAP guard, and it is deliberately not the only one: a recipe can
// say -trimpath and still produce something else if a GOFLAGS environment variable
// overrides it, which is why the next test reads the flags back out of the binary.
func TestPackaging_TheBuildRecipeIsTheOneTheCardRequires(t *testing.T) {
	recipe, err := makefileBuildRecipe()
	if err != nil {
		t.Fatalf("reading the build recipe: %v", err)
	}
	t.Logf("Makefile build recipe: %s", recipe)

	for _, want := range []string{"CGO_ENABLED=0", "-trimpath", "./cmd/tappa"} {
		if !strings.Contains(recipe, want) {
			t.Errorf("the build recipe does not contain %q: %s", want, recipe)
		}
	}
	// 🔴 -buildvcs=false WOULD SILENTLY REMOVE THE COMMIT. It is the one flag that
	// undoes half this card without changing anything visible: the binary still
	// builds, still runs, and can no longer say what it is.
	if strings.Contains(recipe, "-buildvcs=false") || strings.Contains(recipe, "-buildvcs false") {
		t.Errorf("the build recipe disables VCS stamping, so the artifact cannot name its commit: %s", recipe)
	}
}

// TestPackaging_TheArtifactKnowsWhatItWasBuiltFrom reads the flags and the commit
// back OUT of a real binary produced by the Makefile's own recipe.
//
// The positive control is a pair of throwaway programs built with and without
// -trimpath, which establishes that this reader distinguishes the two at all. It is
// independent of the mechanism under test: a three-line program in a temporary
// module, not a second view of the tappa build.
func TestPackaging_TheArtifactKnowsWhatItWasBuiltFrom(t *testing.T) {
	bin := theArtifact(t)
	t.Logf("built with: %s", buildCmd)

	info, err := buildinfo.ReadFile(bin)
	if err != nil {
		t.Fatalf("reading the build info out of %s: %v", bin, err)
	}
	settings := map[string]string{}
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	t.Logf("artifact: module %s %s, go %s, settings %v", info.Path, info.Main.Version, info.GoVersion, settings)

	if settings["CGO_ENABLED"] != "0" {
		t.Errorf("CGO_ENABLED = %q, want 0 — a cgo build is not a static binary", settings["CGO_ENABLED"])
	}
	if settings["-trimpath"] != "true" {
		t.Errorf("-trimpath = %q, want true — without it the binary carries this machine's paths", settings["-trimpath"])
	}
	rev := settings["vcs.revision"]
	if len(rev) != 40 {
		t.Errorf("vcs.revision = %q, want a 40-character git revision — the artifact cannot be traced to a commit", rev)
	}
	if settings["vcs.time"] == "" {
		t.Error("vcs.time is empty; the artifact cannot say when its commit was made")
	}
	if info.Main.Version == "" || info.Main.Version == "(devel)" {
		t.Errorf("the artifact's version is %q; with no git tag the toolchain should still derive a pseudo-version", info.Main.Version)
	}
	// ⚠️ vcs.modified IS NOT REQUIRED TO BE false HERE, and that is honest rather
	// than lax: this test builds from the working tree, which is modified whenever
	// anybody is working — including right now. What must exist is the FIELD, so a
	// deployment can tell a traceable build from an untraceable one; cmd/tappa logs
	// it at WARN when it is true.
	if _, ok := settings["vcs.modified"]; !ok {
		t.Error("vcs.modified is absent, so nothing can tell whether this build is traceable")
	}
	t.Logf("this build's vcs.modified = %q (true is expected while the tree is dirty)", settings["vcs.modified"])

	// POSITIVE CONTROL: does buildinfo.ReadFile actually report -trimpath, or does
	// it say "true" for everything?
	with := controlBinary(t, controlPlainSource, true)
	without := controlBinary(t, controlPlainSource, false)
	withInfo, err := buildinfo.ReadFile(with)
	if err != nil {
		t.Fatalf("control (trimpath): %v", err)
	}
	withoutInfo, err := buildinfo.ReadFile(without)
	if err != nil {
		t.Fatalf("control (no trimpath): %v", err)
	}
	got := func(bi *buildinfo.BuildInfo) string {
		for _, s := range bi.Settings {
			if s.Key == "-trimpath" {
				return s.Value
			}
		}
		return ""
	}
	if got(withInfo) != "true" || got(withoutInfo) == "true" {
		t.Errorf("CONTROL FAILED: a control built WITH -trimpath reports %q and one built WITHOUT reports %q; "+
			"this reader does not distinguish the flag, so the assertion above proves nothing",
			got(withInfo), got(withoutInfo))
	}
}

// ------------------------------------------------------------------- timezone --

// tzMarker is a file name inside the zip that time/tzdata embeds — the first entry
// of the IANA database. Its discriminating power is not assumed: the control
// binaries below carry it exactly when they import time/tzdata.
const tzMarker = "Africa/Abidjan"

const controlPlainSource = `package main

import (
	"fmt"
	"time"
)

func main() { fmt.Println(time.Now()) }
`

const controlTZSource = `package main

import (
	"fmt"
	"time"
	_ "time/tzdata"
)

func main() { fmt.Println(time.Now()) }
`

// controlBinary compiles a throwaway program in its own module and returns its path.
func controlBinary(t *testing.T, src string, trimpath bool) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o600); err != nil {
		t.Fatalf("writing the control program: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module control\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatalf("writing the control module: %v", err)
	}
	out := filepath.Join(dir, "control.bin")
	args := []string{"build"}
	if trimpath {
		args = append(args, "-trimpath")
	}
	args = append(args, "-ldflags=-s -w", "-o", out, ".")
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the control program: %v\n%s", err, combined)
	}
	return out
}

// TestPackaging_TheArtifactCarriesTheTimezoneDatabase.
//
// 🔴 THIS IS THE CRITERION WITH THE LARGEST BLAST RADIUS AND THE THINNEST THREAD.
// Every shift start, every lateness verdict (CLAUDE.md §5) and eleven
// customer-visible dates resolve through time.LoadLocation, which FAILS on an image
// with no /usr/share/zoneinfo unless the database is compiled in. A developer
// machine has system tzdata, so nothing on it notices the difference — the product
// would simply fall back to UTC in production while every test stayed green.
//
// It is measured on the BINARY rather than on the dependency graph, because the
// graph is what the linker is given and the binary is what is deployed.
//
// ⚠️ WHAT THIS TEST DOES NOT PROVE, AND WHERE THE PROOF IS. "The database is in the
// binary" and "time.LoadLocation resolves on an image with no system tzdata" are
// two claims, and only the first is asserted here. The second was MEASURED on
// 2026-08-15 and deliberately NOT added to this suite; the run is reproducible:
//
//	# two three-line programs differing only by the blank import, cross-built for
//	# the deploy platform, run where /usr/share/zoneinfo does not exist:
//	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o without.bin .   # no import
//	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o with.bin .      # _ "time/tzdata"
//	docker run --rm -v "$PWD:/probe:ro" alpine:3 sh -c 'ls /usr/share/zoneinfo; /probe/without.bin; /probe/with.bin'
//
//	ls: /usr/share/zoneinfo: No such file or directory
//	WITHOUT time/tzdata:
//	FAIL Europe/Malta unknown time zone Europe/Malta
//	FAIL Europe/Istanbul unknown time zone Europe/Istanbul
//	FAIL America/New_York unknown time zone America/New_York
//	exit=3
//	WITH time/tzdata:
//	OK   Europe/Malta 04:00 local = 02:00 UTC
//	OK   Europe/Istanbul 04:00 local = 01:00 UTC
//	OK   America/New_York 04:00 local = 08:00 UTC
//	exit=0
//
// WHY IT IS NOT A TEST HERE, measured rather than preferred: no test in this
// repository shells out to docker (`grep -rn '"docker"' --include='*_test.go'` → 0),
// the artifact built on a developer machine is darwin/amd64 so the check needs a
// SECOND, cross-compiled build, and `alpine:3` had to be PULLED — a network
// dependency `make test` does not otherwise have. CI does run docker (`make up`),
// so this could be added later; it would be a deliberate widening of what the unit
// suite depends on, not a free assertion.
func TestPackaging_TheArtifactCarriesTheTimezoneDatabase(t *testing.T) {
	bin := theArtifact(t)

	// CONTROLS FIRST, and they are independent of the product: two three-line
	// programs that differ by one blank import.
	withTZ, err := os.ReadFile(controlBinary(t, controlTZSource, true))
	if err != nil {
		t.Fatalf("reading the tzdata control: %v", err)
	}
	withoutTZ, err := os.ReadFile(controlBinary(t, controlPlainSource, true))
	if err != nil {
		t.Fatalf("reading the plain control: %v", err)
	}
	marker := []byte(tzMarker)
	if !bytes.Contains(withTZ, marker) {
		t.Fatalf("CONTROL FAILED: a program importing time/tzdata does not contain %q, so this marker cannot detect the database", tzMarker)
	}
	if bytes.Contains(withoutTZ, marker) {
		t.Fatalf("CONTROL FAILED: a program NOT importing time/tzdata contains %q anyway, so the marker proves nothing", tzMarker)
	}
	t.Logf("controls: with tzdata %d bytes, without %d bytes (the database itself is the ~%d KB difference)",
		len(withTZ), len(withoutTZ), (len(withTZ)-len(withoutTZ))/1024)

	shipped, err := os.ReadFile(bin)
	if err != nil {
		t.Fatalf("reading the artifact: %v", err)
	}
	if !bytes.Contains(shipped, marker) {
		t.Fatal("the shipped binary does not contain the IANA timezone database: on an image with no system tzdata, " +
			"every shift start and every lateness verdict would silently resolve in UTC")
	}
}

// TestPackaging_TheCommandOwnsTheTimezoneEmbedItself.
//
// The binary test above passes whether the embed comes from cmd/tappa or from any
// package it happens to reach. That is exactly the fragility this task was warned
// about: before M8-01 the ONLY import was in internal/domain/tap, so a change to
// what cmd/tappa depends on could have dropped the database with nothing failing to
// compile. This asserts the command's own source carries it.
func TestPackaging_TheCommandOwnsTheTimezoneEmbedItself(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, "cmd", "tappa", "main.go"))
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	if !regexp.MustCompile(`(?m)^\s*_ "time/tzdata"`).Match(src) {
		t.Error(`cmd/tappa/main.go does not import _ "time/tzdata" itself; the deployable artifact's ` +
			`timezone data would again depend on a blank import in some other package`)
	}
	// And the linker really is given it (the graph half of the same statement).
	deps := goList(t, "-deps", "./cmd/tappa")
	if !contains(deps, "time/tzdata") {
		t.Error("time/tzdata is not in the command's dependency graph")
	}
}

// ------------------------------------------------------------------ migration --

// TestPackaging_TheCommandCannotMigrate — the card's "no automatic migration at
// start-up (wrong-role risk)".
//
// 🔴 THE POINT IS NOT TIDINESS, IT IS WHICH ROLE RUNS. Migrations run as
// tappa_owner; the application connects as tappa_app, which is NOBYPASSRLS and not
// the table owner, and that difference is what makes RLS apply to every query the
// product makes (ADR 0002). A start-up that migrated would either need the owner
// DSN in the application's environment or would fail — and the first of those is
// tenant isolation quietly voided for the lifetime of the process.
//
// It is asserted on the DEPENDENCY GRAPH rather than by grepping for "goose": a
// migration runner that is not linked in cannot run.
func TestPackaging_TheCommandCannotMigrate(t *testing.T) {
	deps := goList(t, "-deps", "./cmd/tappa")
	t.Logf("the command links %d packages", len(deps))
	for _, dep := range deps {
		if strings.Contains(dep, "goose") || strings.Contains(dep, "golang-migrate") || strings.Contains(dep, "/migrate") {
			t.Errorf("the command links %q: a migration runner in the serving binary is one env var away from running as the owner role", dep)
		}
	}
	// POSITIVE CONTROL: the list is real. If goList silently returned nothing, the
	// loop above would pass against anything.
	if !contains(deps, "github.com/atknatk/tappa/internal/db") {
		t.Fatalf("the dependency list does not contain internal/db (%d entries); it is not the command's graph", len(deps))
	}

	// AND THE OWNER DSN IS NEVER READ BY THE PRODUCT. config.Load mentions
	// DATABASE_MIGRATE_URL exactly once, to REFUSE an application configured with
	// it; nothing else in non-test code may touch it.
	var readers []string
	for _, f := range productionSources(t) {
		if f == filepath.Join(repoRoot, "internal", "config", "config.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		if bytes.Contains(b, []byte("DATABASE_MIGRATE_URL")) {
			readers = append(readers, f)
		}
	}
	if len(readers) > 0 {
		t.Errorf("the migration role's connection string is referenced outside internal/config: %v", readers)
	}
}

// ---------------------------------------------------------- runtime file deps --

// diskAPIs are the calls that would make the deployment depend on files next to the
// binary. It is a DENYLIST and is named as one: it sees what it lists, and the
// serving test below is the behavioural check that does not depend on this list
// being complete.
var diskAPIs = regexp.MustCompile(`\bos\.(Open|OpenFile|ReadFile|WriteFile|ReadDir|Create|Stat|Lstat|Mkdir|MkdirAll)\(|\bhttp\.Dir\(|\bos\.DirFS\(|\bioutil\.`)

// TestPackaging_NoProductionCodeReadsTheDisk.
//
// Static assets are served from web/embed.go's embed.FS and templates are compiled
// into Go by templ, so a deployment is one file. Measured at 5a86f9b: zero hits.
// Nothing held that — an http.Dir is one line away from the FileServer in
// internal/httpx/router.go — so this is the line.
func TestPackaging_NoProductionCodeReadsTheDisk(t *testing.T) {
	// CONTROL: the scanner sees what it claims to see. The input is written here,
	// not derived from the product.
	if !diskAPIs.MatchString(`f, _ := os.ReadFile("x")`) || !diskAPIs.MatchString(`http.Dir("web/static")`) {
		t.Fatal("CONTROL FAILED: the scanner does not match a call it is supposed to catch")
	}
	if diskAPIs.MatchString(`web.Static()`) {
		t.Fatal("CONTROL FAILED: the scanner matches something harmless, so its silence would mean nothing")
	}

	files := productionSources(t)
	if len(files) < 100 {
		t.Fatalf("the scan covered only %d files; it has gone blind", len(files))
	}
	t.Logf("scanned %d non-test source files", len(files))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		if m := diskAPIs.FindAllString(string(b), -1); len(m) > 0 {
			rel, _ := filepath.Rel(repoRoot, f)
			t.Errorf("%s reads the filesystem at runtime (%v); the deployment is meant to be one file", rel, m)
		}
	}
}

// productionSources lists every non-test .go file under cmd/, internal/ and web/.
func productionSources(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, dir := range []string{"cmd", "internal", "web"} {
		err := filepath.WalkDir(filepath.Join(repoRoot, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			out = append(out, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s: %v", dir, err)
		}
	}
	return out
}

// ------------------------------------------------------------------- helpers --

func goList(t *testing.T, args ...string) []string {
	t.Helper()
	cmd := exec.Command("go", append([]string{"list"}, args...)...)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list %v: %v", args, err)
	}
	return strings.Fields(string(out))
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
