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

// ------------------------------------------- config <-> manifest injection --

// stripYAMLComments removes `#` comments so a NAME MENTIONED IN PROSE cannot be
// mistaken for a name the manifest actually injects.
//
// This is the whole reason the check below is not a grep, and the difference is
// not hypothetical: DATABASE_MIGRATE_URL appears TWICE in 20-app.yaml, both times
// inside the comment explaining why it is deliberately NOT injected. A grep would
// report it as injected — the exact inversion of the truth.
//
// It is deliberately naive about `#` inside quoted strings. The manifests are
// checked below to contain no quoted `#`, so the naivety is bounded by a test
// rather than by hope.
func stripYAMLComments(src string) string {
	var out strings.Builder
	for _, line := range strings.Split(src, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		out.WriteString(line)
		out.WriteString("\n")
	}
	return out.String()
}

// containerEnvEntries parses the env: list of ONE named container in a Deployment
// manifest and returns, per variable, whether that entry actually carries a source
// (`value:` or `valueFrom:`) and whether it is marked optional.
//
// 🔴 IT IS A SCOPED, STRUCTURAL PARSE AND NOT A FILE-WIDE REGEX, because the
// file-wide regex version was measured to be worthless in two separate ways. It
// matched `- name: X` ANYWHERE in the manifest and never asked what the entry
// carried, so both of these passed it while breaking the product:
//
//   - keep `- name: TAPPA_TAG_KEK_PREVIOUS`, delete its `valueFrom:` block. In
//     Kubernetes an EnvVar with no source defaults to value "", so the process
//     receives an EMPTY string, optionalKey32 returns nil, and the rotation window
//     is silently CLOSED while every manifest and every gate says it is open.
//   - move the entry into the wait-for-postgres initContainer's env:. That
//     container exits before the server starts; the serving process never sees the
//     variable at all. kubectl --dry-run=client parses the manifest happily.
//
// The parser is indentation-driven: it finds `containers:`, then the `- name: <c>`
// item, then that item's `env:` block, and reads the `- name:` entries inside it
// together with the lines that belong to each entry.
func containerEnvEntries(t *testing.T, manifest, container string) map[string]envEntry {
	t.Helper()
	entries := map[string]envEntry{}
	body := blockUnder(stripYAMLComments(manifest), "containers:")
	if body == nil {
		t.Fatalf("no containers: block found")
	}
	item := listItemNamed(body, container)
	if item == nil {
		t.Fatalf("no container named %q under containers:", container)
	}
	envBlock := blockUnder(strings.Join(item, "\n"), "env:")
	if envBlock == nil {
		return entries
	}
	cur := ""
	for _, line := range envBlock {
		txt := strings.TrimSpace(line)
		if strings.HasPrefix(txt, "- name: ") {
			cur = unquote(strings.TrimSpace(strings.TrimPrefix(txt, "- name: ")))
			entries[cur] = envEntry{}
			continue
		}
		if cur == "" {
			continue
		}
		e := entries[cur]
		if txt == "valueFrom:" || strings.HasPrefix(txt, "value:") {
			e.hasSource = true
		}
		if strings.HasPrefix(txt, "optional:") {
			e.optional = strings.Contains(txt, "true")
		}
		entries[cur] = e
	}
	return entries
}

// unquote strips one layer of matching YAML quotes.
//
// The parser was QUOTE-BLIND: `- name: "TAPPA_TAG_KEK"` produced the key
// `"TAPPA_TAG_KEK"` with the quotes attached, so a perfectly valid manifest read
// as "this variable is not injected". That direction is safe (a false RED) for
// every variable except the one on the never-inject list, where quoting would
// have turned a real finding into a pass — so it is fixed rather than tolerated.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// indentOf is the number of leading spaces.
func indentOf(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// blockUnder returns the lines strictly more indented than the first line whose
// trimmed text equals key, stopping at the first line that dedents back to or past
// key's own indent. Blank lines are skipped rather than ending the block.
func blockUnder(src, key string) []string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != key {
			continue
		}
		base := indentOf(line)
		var out []string
		for _, next := range lines[i+1:] {
			if strings.TrimSpace(next) == "" {
				continue
			}
			if indentOf(next) <= base {
				break
			}
			out = append(out, next)
		}
		return out
	}
	return nil
}

// listItemNamed returns the lines of the `- name: <want>` list item at the
// SHALLOWEST list depth in block.
//
// 🔴 THE DEPTH RULE IS THE BUG THIS FUNCTION WAS WRITTEN AROUND. A first attempt
// treated every `- name:` as a container boundary, so `- name: http` under the
// container's own `ports:` reset the match and the parser returned ZERO entries —
// caught only because the test carries a control asserting a realistic entry count.
// Container items are the shallowest `- ` lines in the containers: block; anything
// deeper belongs to the container, not beside it.
func listItemNamed(block []string, want string) []string {
	depth := -1
	for _, line := range block {
		if strings.HasPrefix(strings.TrimSpace(line), "- ") {
			if d := indentOf(line); depth == -1 || d < depth {
				depth = d
			}
		}
	}
	if depth == -1 {
		return nil
	}
	var out []string
	collecting := false
	for _, line := range block {
		isItem := strings.HasPrefix(strings.TrimSpace(line), "- ") && indentOf(line) == depth
		if isItem {
			if collecting {
				break
			}
			collecting = unquote(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "- name: "))) == want &&
				strings.HasPrefix(strings.TrimSpace(line), "- name: ")
			if collecting {
				continue
			}
		}
		if collecting {
			out = append(out, line)
		}
	}
	if !collecting && out == nil {
		return nil
	}
	return out
}

// containerEnvFromKinds returns the REF KINDS in a container's envFrom: list —
// "configMapRef", "secretRef", or anything else a future API adds.
//
// 🔴 THIS EXISTS BECAUSE THE PREVIOUS GATE READ AN ABSENCE AND envFrom CARRIES NO
// NAMES AT ALL. Every check before this asked "is variable X listed with a
// source?", which cannot see a `- secretRef: {name: tappa-secrets}` two lines
// above: that single edit injects EVERY key of the Secret into the serving
// process — including DATABASE_MIGRATE_URL, whose role is initdb's bootstrap
// SUPERUSER with rolbypassrls. Measured: adding those two lines left
// `go test ./cmd/tappa/` at exit 0, while 20-app.yaml's own comment claimed this
// test was what prevented it.
//
// The gate built on this asserts a POSITIVE mechanism — "envFrom carries only
// configMapRef" — instead of enumerating forbidden names. A name list closes one
// of the injection forms; the mechanism closes the category.
func containerEnvFromKinds(t *testing.T, manifest, container string) []string {
	t.Helper()
	// BOTH lists: an initContainer is still a container in this pod, with the same
	// Secret in reach and a chance to run before the server does.
	clean := stripYAMLComments(manifest)
	var item []string
	for _, key := range []string{"containers:", "initContainers:"} {
		if body := blockUnder(clean, key); body != nil {
			if it := listItemNamed(body, container); it != nil {
				item = it
				break
			}
		}
	}
	if item == nil {
		t.Fatalf("no container named %q under containers: or initContainers:", container)
	}
	block := blockUnder(strings.Join(item, "\n"), "envFrom:")
	var kinds []string
	for _, line := range block {
		txt := strings.TrimSpace(line)
		if !strings.HasPrefix(txt, "- ") {
			continue
		}
		txt = strings.TrimSpace(strings.TrimPrefix(txt, "- "))
		// `- secretRef:` or an inline `- secretRef: {name: x}`
		if i := strings.Index(txt, ":"); i > 0 {
			kinds = append(kinds, txt[:i])
		}
	}
	return kinds
}

// disallowedEnvFromKinds returns the envFrom ref kinds that must never appear on
// the serving container.
//
// 🔴 IT IS A FUNCTION RATHER THAN AN `if` INSIDE A TEST, and the reason is a
// measured hole: when the check lived inline in the test, neutering it
// (`if k != "configMapRef"` -> `if false`) left the package GREEN, because a test
// is its own oracle and nothing watches it. Pulled out here it is ordinary code
// with its own table test, so the predicate can be broken and something notices.
//
// The rule is an ALLOW-LIST of one. configMapRef is the only source that cannot
// carry a Secret; everything else — secretRef today, whatever the API adds
// tomorrow — is refused by default rather than by enumeration.
func disallowedEnvFromKinds(kinds []string) []string {
	var bad []string
	for _, k := range kinds {
		if k != "configMapRef" {
			bad = append(bad, k)
		}
	}
	return bad
}

// envEntry is what a manifest entry actually promises the process.
type envEntry struct {
	hasSource bool // carries value: or valueFrom: — without one the process gets ""
	optional  bool
}

// configMapKeys returns the keys the tappa-config ConfigMap defines; the
// Deployment pulls that map in wholesale with envFrom, so those names really do
// reach the process.
func configMapKeys(t *testing.T, src string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, m := range regexp.MustCompile(`(?m)^\s{2}([A-Z][A-Z0-9_]*):`).
		FindAllStringSubmatch(stripYAMLComments(src), -1) {
		out[m[1]] = true
	}
	return out
}

// notInjectedOnPurpose is the ONE variable internal/config names that the running
// server must never receive. It is a map rather than a bare skip so the reason
// travels with it and so the test below can assert the ABSENCE, not just tolerate
// it.
// servingContainer is the container that actually runs the product. Scoping the
// env parse to it by NAME is what makes an entry hidden in an initContainer a
// failure rather than a pass.
const servingContainer = "tappa"

var notInjectedOnPurpose = map[string]string{
	"DATABASE_MIGRATE_URL": "the migration role's DSN. tappa_owner is initdb's bootstrap SUPERUSER and " +
		"bypasses RLS unconditionally; internal/config names it only to REFUSE a deployment where the two " +
		"DSNs are equal. A server process that could read it would hold a ready-made cross-tenant credential.",
}

// TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest.
//
// 🔴 THIS TEST EXISTS BECAUSE A SHIPPED CHANGE ADDED A CONFIGURATION VARIABLE AND
// NO MANIFEST. TAPPA_TAG_KEK_PREVIOUS was read by internal/config, documented in
// .env.example and named by the rotation runbook — and 20-app.yaml, which pulls
// the Secret KEY BY KEY rather than with envFrom, listed four names and not that
// one. The failure mode was silent in every direction that matters: the Secret
// would hold the key, the rollout would go green, the pod would never see the
// variable, config.Load would read "" and treat the rotation window as closed,
// and the runbook's first step would leave the whole park sealed under a key the
// server does not hold. Every tap 500, no transaction row written (§4.6).
//
// The point is the CLASS, not that one variable: key-by-key injection means every
// future variable is one forgotten manifest edit away from the same silence, and
// nothing in Go or YAML connects the two files. This does.
func TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, "internal", "config", "config.go"))
	if err != nil {
		t.Fatalf("reading config.go: %v", err)
	}
	wanted := map[string]bool{}
	for _, m := range regexp.MustCompile(`"((?:TAPPA|DATABASE)_[A-Z0-9_]+)"`).
		FindAllStringSubmatch(string(src), -1) {
		wanted[m[1]] = true
	}
	if len(wanted) < 10 {
		t.Fatalf("only %d configuration variables found in config.go; the scan has gone blind", len(wanted))
	}
	// 🔴 A COUNT IS NOT COVERAGE. Narrowing this scan's regex from
	// (TAPPA|DATABASE)_ to (TAPPA)_ kept the count above ten and quietly stopped
	// requiring the DATABASE_ variables to be injected at all — including the one
	// on the never-inject list, whose exemption is only meaningful while it is
	// still being scanned for. These names are asserted by NAME.
	for _, must := range []string{"DATABASE_URL", "DATABASE_MIGRATE_URL", "TAPPA_TAG_KEK", "TAPPA_TAG_KEK_PREVIOUS"} {
		if !wanted[must] {
			t.Fatalf("the config scan did not find %s; its pattern has narrowed and whole families of "+
				"variables are no longer being checked", must)
		}
	}
	t.Logf("internal/config names %d environment variables", len(wanted))

	appSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "20-app.yaml"))
	if err != nil {
		t.Fatalf("reading 20-app.yaml: %v", err)
	}
	cfgSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "05-config.yaml"))
	if err != nil {
		t.Fatalf("reading 05-config.yaml: %v", err)
	}
	// THE SERVING container, by name. An entry in any other container (the
	// wait-for-postgres initContainer, say) never reaches the process that reads
	// the configuration.
	env := containerEnvEntries(t, string(appSrc), servingContainer)
	cm := configMapKeys(t, string(cfgSrc))

	// CONTROLS: both parses found something realistic. A blind parse would make
	// every assertion below vacuous in one direction or the other.
	if len(env) < 4 {
		t.Fatalf("only %d env entries parsed out of the %q container; the parse has gone blind", len(env), servingContainer)
	}
	if len(cm) < 5 {
		t.Fatalf("only %d ConfigMap keys parsed; the parse has gone blind", len(cm))
	}
	for _, must := range []string{"DATABASE_URL", "TAPPA_TAG_KEK"} {
		if e, ok := env[must]; !ok || !e.hasSource {
			t.Fatalf("CONTROL FAILED: %s is not parsed as a sourced env entry of the %q container", must, servingContainer)
		}
	}

	for name := range wanted {
		if reason, exempt := notInjectedOnPurpose[name]; exempt {
			if _, found := env[name]; found {
				t.Errorf("%s IS an env entry of the serving container, but it is on the never-inject list: %s", name, reason)
			}
			if cm[name] {
				t.Errorf("%s IS a ConfigMap key, but it is on the never-inject list: %s", name, reason)
			}
			continue
		}
		e, inEnv := env[name]
		switch {
		case !inEnv && !cm[name]:
			t.Errorf("internal/config reads %s but the serving container does not receive it. "+
				"20-app.yaml pulls the Secret key by key, so an unlisted name is NEVER seen by the "+
				"process — it reads as unset, and the deployment goes green while the feature is dead. "+
				"Add it to the %q container's env: in deploy/k8s/20-app.yaml (secretKeyRef; use "+
				"`optional: true` if it may be absent) or to deploy/k8s/05-config.yaml.", name, servingContainer)
		case inEnv && !e.hasSource:
			// The measured failure: an EnvVar with neither value nor valueFrom is
			// not "unconfigured", it is configured to the EMPTY STRING.
			t.Errorf("%s is listed in the %q container's env: but carries neither `value:` nor "+
				"`valueFrom:`. Kubernetes gives such an entry the value \"\", so the process reads it as "+
				"unset — the manifest looks correct and the feature is silently off.", name, servingContainer)
		}
	}
}

// TestPackaging_TheRotationKEKIsOptionalSoTheSteadyStateStarts.
//
// TAPPA_TAG_KEK_PREVIOUS is absent from tappa-secrets except during a rotation.
// Without `optional: true` kubelet refuses to start a container whose secretKeyRef
// names a missing key, so every ordinary deploy would stall — with
// maxUnavailable: 0 the old pod keeps serving, so it is a STUCK ROLLOUT rather
// than an outage, but it is a deploy nobody can complete and the cause is three
// levels down in a kubelet event.
//
// The manifest comment calls the flag "load-bearing"; an auditor deleted it and
// the suite stayed green. This is that claim, pinned.
func TestPackaging_TheRotationKEKIsOptionalSoTheSteadyStateStarts(t *testing.T) {
	appSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "20-app.yaml"))
	if err != nil {
		t.Fatalf("reading 20-app.yaml: %v", err)
	}
	env := containerEnvEntries(t, string(appSrc), servingContainer)
	e, ok := env["TAPPA_TAG_KEK_PREVIOUS"]
	if !ok {
		t.Fatal("TAPPA_TAG_KEK_PREVIOUS is not an env entry of the serving container")
	}
	if !e.optional {
		t.Error("TAPPA_TAG_KEK_PREVIOUS is not marked `optional: true`. The key is absent from " +
			"tappa-secrets whenever no rotation is in progress, and kubelet refuses to start a " +
			"container whose secretKeyRef names a missing key — so every ordinary deploy stalls.")
	}
	// CONTROL: a variable that must always be present is NOT optional, so this
	// test cannot pass by calling everything optional.
	if req, ok := env["TAPPA_TAG_KEK"]; !ok || req.optional {
		t.Error("CONTROL FAILED: TAPPA_TAG_KEK should be a required (non-optional) entry")
	}
}

// TestPackaging_TheCommentStripperIsNotFooledByProse is the control for the test
// above, and it is a separate test because the control is the load-bearing half:
// without comment stripping the check would report DATABASE_MIGRATE_URL as
// injected — it appears twice in 20-app.yaml, both times in the comment saying it
// must NOT be. A green result from a blind scanner is worse than no scanner.
func TestPackaging_TheCommentStripperIsNotFooledByProse(t *testing.T) {
	app, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "20-app.yaml"))
	if err != nil {
		t.Fatalf("reading 20-app.yaml: %v", err)
	}
	raw := string(app)
	if !strings.Contains(raw, "DATABASE_MIGRATE_URL") {
		t.Skip("20-app.yaml no longer mentions DATABASE_MIGRATE_URL; this control needs a new subject")
	}
	if strings.Contains(stripYAMLComments(raw), "DATABASE_MIGRATE_URL") {
		t.Error("after stripping comments DATABASE_MIGRATE_URL is still present: either it is genuinely " +
			"injected now (a serious regression) or the stripper is broken (the injection test is blind)")
	}
	// And the stripper's own naivety is bounded: no quoted '#' anywhere in the two
	// manifests it parses, so cutting at the first '#' cannot truncate real data.
	for _, f := range []string{"20-app.yaml", "05-config.yaml"} {
		b, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", f))
		if err != nil {
			t.Fatalf("reading %s: %v", f, err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if h := strings.Index(line, "#"); h >= 0 && strings.Count(line[:h], `"`)%2 == 1 {
				t.Errorf("%s:%d has a '#' inside a quoted string; the comment stripper would truncate it", f, i+1)
			}
		}
	}
}

// TestContainerEnvEntries_DegenerateManifests tests the PARSER, against inputs it
// is meant to get wrong, using a synthetic manifest written here rather than the
// real one.
//
// 🔴 WHY THIS EXISTS. The manifest checks above are only as good as this parse,
// and this parse has already been wrong twice: a file-wide regex that matched
// `- name:` anywhere (so a variable hidden in an initContainer passed), and a
// state machine that treated `- name: http` under `ports:` as a container
// boundary and returned ZERO entries. Both were found by mutation and by a
// control, not by the parser's own tests — because it had none. The scoping is
// load-bearing and nothing pinned it directly: widening it back to the whole file
// left the suite green.
//
// The fixture deliberately contains every shape that has bitten: a port entry, an
// initContainer, a second serving container, a sourceless entry, an optional
// entry, and a comment that mentions a variable it does not inject.
func TestContainerEnvEntries_DegenerateManifests(t *testing.T) {
	const manifest = `
apiVersion: apps/v1
kind: Deployment
spec:
  template:
    spec:
      initContainers:
        - name: wait-for-postgres
          env:
            - name: ONLY_IN_INITCONTAINER
              valueFrom:
                secretKeyRef:
                  name: s
                  key: k
      containers:
        - name: tappa
          image: x
          ports:
            - name: http
              containerPort: 8080
          # SOURCELESS_MENTIONED_IN_A_COMMENT is discussed here but not injected.
          env:
            - name: WITH_SECRET
              valueFrom:
                secretKeyRef:
                  name: s
                  key: k
            - name: WITH_LITERAL
              value: "hello"
            - name: OPTIONAL_ONE
              valueFrom:
                secretKeyRef:
                  name: s
                  key: k
                  optional: true
            - name: "QUOTED_NAME"
              value: "q"
            - name: SOURCELESS
            - name: BODY_BUT_NO_SOURCE
              fieldRef:
                fieldPath: metadata.name
          volumeMounts:
            - name: tmp
              mountPath: /tmp
        - name: sidecar
          env:
            - name: ONLY_IN_SIDECAR
              value: "y"
`
	env := containerEnvEntries(t, manifest, "tappa")

	// The ports entry must not be mistaken for a container boundary — that bug
	// made the parser return nothing at all.
	if len(env) != 6 {
		t.Fatalf("parsed %d env entries, want 6 (%v)", len(env), env)
	}
	for name, want := range map[string]envEntry{
		"WITH_SECRET":  {hasSource: true, optional: false},
		"WITH_LITERAL": {hasSource: true, optional: false},
		"OPTIONAL_ONE": {hasSource: true, optional: true},
		"SOURCELESS":   {hasSource: false, optional: false},
		// 🔴 THE SHAPE THIS FIXTURE WAS MISSING. A sourceless entry that is LAST
		// in the block has no body lines at all, so a parser that marks every
		// body line as a source still reports it correctly — measured: exactly
		// that mutation stayed GREEN until this entry existed. An entry with a
		// body that is NOT a source is what actually exercises the check.
		"BODY_BUT_NO_SOURCE": {hasSource: false, optional: false},
		// Quoting is legal YAML and the parser used to keep the quotes in the
		// key, so a perfectly valid manifest read as "not injected".
		"QUOTED_NAME": {hasSource: true, optional: false},
	} {
		got, ok := env[name]
		if !ok {
			t.Errorf("%s missing from the serving container's env", name)
			continue
		}
		if got != want {
			t.Errorf("%s parsed as %+v, want %+v", name, got, want)
		}
	}
	// THE SCOPE. Each of these would be a real product failure if it counted as
	// injected: an initContainer exits before the server starts, and a sidecar is
	// a different process entirely.
	for _, elsewhere := range []string{"ONLY_IN_INITCONTAINER", "ONLY_IN_SIDECAR", "http"} {
		if _, found := env[elsewhere]; found {
			t.Errorf("%s is NOT in the serving container's env but the parser reported it; a variable "+
				"the serving process never sees would count as injected", elsewhere)
		}
	}
	// A name that appears only in prose is not an injection.
	if _, found := env["SOURCELESS_MENTIONED_IN_A_COMMENT"]; found {
		t.Error("a variable named only in a comment was parsed as an env entry")
	}

	// The other container parses independently, so the scoping is a real filter
	// rather than an accident of this fixture's ordering.
	if side := containerEnvEntries(t, manifest, "sidecar"); len(side) != 1 {
		t.Errorf("the sidecar should have exactly 1 env entry, got %d (%v)", len(side), side)
	}
}

// TestPackaging_TheServingEnvironmentComesOnlyFromExplicitEntriesAndTheConfigMap.
//
// 🔴 THIS IS THE MECHANISM 20-app.yaml's OWN COMMENT CLAIMS, ASSERTED. That comment
// says `envFrom: secretRef` "would put EVERY key of tappa-secrets into this
// process's environment — including DATABASE_MIGRATE_URL", and then names
// TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest as "what actually
// keeps this list honest". Measured: it did not. Adding two lines to the existing
// envFrom: list injected the whole Secret and the package stayed GREEN, because
// every gate up to that point asked "is this NAME listed with a source?" and
// envFrom carries no names at all.
//
// So the claim changed shape. Instead of enumerating names that must not appear —
// a list that closes one injection form out of several — this asserts the positive
// property the design actually depends on:
//
//	the serving container's environment comes from its explicit env: entries,
//	plus exactly one envFrom source, and that source is a ConfigMap.
//
// A Secret reaches this process only by being named, one key at a time, in env:.
// That is a statement a command resolves, and it stays true against injection
// forms nobody has thought of yet.
func TestPackaging_TheServingEnvironmentComesOnlyFromExplicitEntriesAndTheConfigMap(t *testing.T) {
	appSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "20-app.yaml"))
	if err != nil {
		t.Fatalf("reading 20-app.yaml: %v", err)
	}
	// EVERY container in the pod, not just the serving one. 20-app.yaml's claim is
	// written at POD level ("THE SECRET IS PULLED IN KEY BY KEY AND NOT WITH
	// envFrom"), and an initContainer with `envFrom: secretRef` would hand
	// DATABASE_MIGRATE_URL to wait-for-postgres — a container that runs before the
	// server, in the same pod, with the same Secret in reach. Measured: two lines
	// there left the suite green.
	var kinds []string
	for _, c := range podContainerNames(t, string(appSrc)) {
		kinds = append(kinds, containerEnvFromKinds(t, string(appSrc), c)...)
	}

	// CONTROL: the parse found the envFrom block at all. Without this an empty
	// result would satisfy every assertion below.
	if len(kinds) == 0 {
		t.Fatalf("no envFrom entries parsed for the %q container; the check would pass vacuously", servingContainer)
	}
	for _, k := range disallowedEnvFromKinds(kinds) {
		{
			t.Errorf("the %q container's envFrom carries a %q. Only configMapRef is allowed: any Secret "+
				"reference here injects EVERY key of that Secret into the serving process, including "+
				"DATABASE_MIGRATE_URL — the migration role's DSN, which is a superuser with BYPASSRLS. "+
				"Tenant isolation (CLAUDE.md 4.5) would be one manifest edit away. Name the keys you "+
				"need individually under env: instead.", servingContainer, k)
		}
	}
	// And the one source it does have is the expected ConfigMap, so a second
	// ConfigMap cannot smuggle values in either.
	if n := strings.Count(strings.Join(kinds, ","), "configMapRef"); n != 1 {
		t.Errorf("expected exactly one configMapRef in envFrom, got %d (%v)", n, kinds)
	}
	if !strings.Contains(string(appSrc), "name: tappa-config") {
		t.Error("the envFrom ConfigMap is not tappa-config")
	}
}

// TestContainerEnvFromKinds_DegenerateManifests gives the envFrom parser its own
// degenerate inputs, which is the step that was missing when the previous parser
// shipped: its fixture list had initContainers, sidecars and ports, but not the
// injection form that actually got through.
func TestContainerEnvFromKinds_DegenerateManifests(t *testing.T) {
	tests := []struct {
		name     string
		envFrom  string
		wantKind []string
	}{
		{"only a ConfigMap (the shipped shape)", `
          envFrom:
            - configMapRef:
                name: tappa-config`, []string{"configMapRef"}},
		{"a Secret appended to the list — the measured escape", `
          envFrom:
            - configMapRef:
                name: tappa-config
            - secretRef:
                name: tappa-secrets`, []string{"configMapRef", "secretRef"}},
		{"a Secret alone", `
          envFrom:
            - secretRef:
                name: tappa-secrets`, []string{"secretRef"}},
		{"inline flow style", `
          envFrom:
            - secretRef: {name: tappa-secrets}`, []string{"secretRef"}},
		{"no envFrom at all", ``, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := `
spec:
  template:
    spec:
      containers:
        - name: tappa
          image: x` + tc.envFrom + `
          env:
            - name: A
              value: "1"
`
			got := containerEnvFromKinds(t, manifest, "tappa")
			if strings.Join(got, ",") != strings.Join(tc.wantKind, ",") {
				t.Errorf("kinds = %v, want %v", got, tc.wantKind)
			}
		})
	}
}

// TestDisallowedEnvFromKinds is the watchman's watchman: the manifest gate's
// PREDICATE, tested as code.
//
// Mutating an assertion that lives inside a test can never be caught by that
// test — measured, `if k != "configMapRef"` -> `if false` stayed green. Once the
// predicate is a function, breaking it fails here.
func TestDisallowedEnvFromKinds(t *testing.T) {
	tests := []struct {
		name  string
		kinds []string
		want  []string
	}{
		{"the shipped shape", []string{"configMapRef"}, nil},
		{"a Secret appended", []string{"configMapRef", "secretRef"}, []string{"secretRef"}},
		{"a Secret alone", []string{"secretRef"}, []string{"secretRef"}},
		{"nothing at all", nil, nil},
		{"several ConfigMaps are allowed by this rule", []string{"configMapRef", "configMapRef"}, nil},
		// DEFAULT-DENY: a kind nobody has heard of is refused without being named.
		{"a future ref kind is refused by default", []string{"someFutureRef"}, []string{"someFutureRef"}},
		{"case matters — a near miss is not the allowed kind", []string{"ConfigMapRef"}, []string{"ConfigMapRef"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := disallowedEnvFromKinds(tc.kinds)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("disallowedEnvFromKinds(%v) = %v, want %v", tc.kinds, got, tc.want)
			}
		})
	}
}

// podContainerNames lists every container in the pod template — initContainers and
// containers alike — so a pod-level claim can be checked at pod level.
func podContainerNames(t *testing.T, manifest string) []string {
	t.Helper()
	var out []string
	clean := stripYAMLComments(manifest)
	for _, key := range []string{"containers:", "initContainers:"} {
		block := blockUnder(clean, key)
		depth := -1
		for _, line := range block {
			if strings.HasPrefix(strings.TrimSpace(line), "- ") {
				if d := indentOf(line); depth == -1 || d < depth {
					depth = d
				}
			}
		}
		for _, line := range block {
			txt := strings.TrimSpace(line)
			if strings.HasPrefix(txt, "- name: ") && indentOf(line) == depth {
				out = append(out, unquote(strings.TrimPrefix(txt, "- name: ")))
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("no containers found in the pod template; the pod-level check would be vacuous")
	}
	return out
}

// TestPackaging_PodContainerNames_FindsBothLists is the control for the widening:
// if initContainers stopped being scanned, the pod-level claim would silently
// narrow back to one container.
func TestPackaging_PodContainerNames_FindsBothLists(t *testing.T) {
	appSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "20-app.yaml"))
	if err != nil {
		t.Fatalf("reading 20-app.yaml: %v", err)
	}
	names := podContainerNames(t, string(appSrc))
	found := map[string]bool{}
	for _, n := range names {
		found[n] = true
	}
	for _, want := range []string{servingContainer, "wait-for-postgres"} {
		if !found[want] {
			t.Errorf("container %q not found by the pod-level scan (found %v); an envFrom hidden there "+
				"would go unnoticed", want, names)
		}
	}
}

// TestPackaging_EverySecretEnvEntryUsesSecretKeyRef.
//
// The injection gate asked whether an entry has A source, never WHICH KIND. An
// auditor changed TAPPA_TAG_KEK's secretKeyRef to a fieldRef and the suite stayed
// green — the process would then receive a pod field (or nothing useful) where a
// key belongs, and config.Load would refuse or, worse, read something valid-looking.
//
// The rule is narrow on purpose: the four required key/DSN variables must come
// from a SECRET, not from any other source. Everything else stays free.
func TestPackaging_EverySecretEnvEntryUsesSecretKeyRef(t *testing.T) {
	appSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "20-app.yaml"))
	if err != nil {
		t.Fatalf("reading 20-app.yaml: %v", err)
	}
	item := listItemNamed(blockUnder(stripYAMLComments(string(appSrc)), "containers:"), servingContainer)
	if item == nil {
		t.Fatalf("container %q not found", servingContainer)
	}
	envBlock := strings.Join(blockUnder(strings.Join(item, "\n"), "env:"), "\n")

	mustBeSecret := []string{"DATABASE_URL", "TAPPA_SESSION_HMAC_KEY", "TAPPA_TAG_KEK",
		"TAPPA_INVITE_HMAC_KEY", "TAPPA_TAG_KEK_PREVIOUS"}
	for _, name := range mustBeSecret {
		i := strings.Index(envBlock, "- name: "+name)
		if i < 0 {
			t.Errorf("%s is not an env entry of the serving container", name)
			continue
		}
		rest := envBlock[i:]
		if j := strings.Index(rest[1:], "- name: "); j >= 0 {
			rest = rest[:j+1]
		}
		if !strings.Contains(rest, "secretKeyRef:") {
			t.Errorf("%s does not come from a secretKeyRef. A key or DSN sourced from anywhere else "+
				"(configMapKeyRef, fieldRef, a literal value) is either not the secret or not secret.", name)
		}
	}
	// CONTROL: the slicing really isolates one entry, so a secretKeyRef belonging
	// to a neighbour cannot satisfy the check for its predecessor.
	if strings.Count(envBlock, "secretKeyRef:") < len(mustBeSecret) {
		t.Errorf("only %d secretKeyRef entries for %d required variables", strings.Count(envBlock, "secretKeyRef:"), len(mustBeSecret))
	}
}
