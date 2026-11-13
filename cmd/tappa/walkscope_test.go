package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// walkScopeListed is every tree walk in this repository's tests that carries NO nested-checkout wire
// (T87's rule, backlog T218 (a)), each with why it needs none. Keys are "file:Func" (with "#n" for the
// n-th walk of one function, n >= 2). A walk that is neither wired nor here is red -- so a new walk, or
// a wire removed from an old one, is a decision somebody writes down, not something only a nested
// worktree on one developer's disk would reveal (CI's fresh checkout holds none).
//
// The reasons are read, not measured; "a subtree" means the walk starts below the repository root at a
// directory no checkout is put in.
var walkScopeListed = map[string]string{
	"cmd/opadmin/tableslock_test.go:testDirs":                                                             "skips every directory whose name starts with a dot (.claude/worktrees among them)",
	"cmd/tappa/limiterallowed_test.go:TestLimiterAllowed_IsCalledByNoProductionCode":                      "subtrees: cmd, internal",
	"cmd/tappa/noteprovenance_test.go:derivedPolicyVersionWriters":                                        "a subtree: db",
	"cmd/tappa/packaging_test.go:productionSources":                                                       "subtrees: cmd, internal, web",
	"cmd/tappa/sandboxbuild_test.go:sandboxTaggedPackages":                                                "skips every directory whose name starts with a dot",
	"cmd/tappa/sandboxskip_test.go:TestSandboxSkip_SkipsOnlyTestsTheTagCannotChange":                      "subtrees: the listed packages",
	"internal/adminauth/identity_shape_test.go:TestOpenIdentitySession_HasNoCallerOutsideTheVerifiedType": "subtrees: internal, cmd",
	"internal/adminauth/rowsession_test.go:TestOpenRowSession_HasItsTwoCallersAndTheOldInsertNone":        "subtrees: internal, cmd",
	"internal/adminauth/signupclaim_test.go:TestClaimSignup_HasNoCallerOutsideTheProof":                   "subtrees: internal, cmd",
	"internal/config/secretfields_test.go:readInternalTree":                                               "a subtree: internal (its one caller)",
	"internal/db/execmodetypes_test.go:constrainedProductFiles":                                           "skips every directory holding a go.mod file, which a nested checkout of this module does (T87 measured it)",
	"internal/handler/api/suspension_db_test.go:TestSuspension_NoWorkerOrTriggerReadsIt":                  "subtrees: internal/webhook, internal/invite",
	"internal/handler/brandupload_test.go:TestBrandUpload_TheGateIsBuiltOnceWithTheRulesSlots":            "subtrees: internal, cmd",
	"internal/handler/dashboard_test.go:TestLayoutShells_EveryOneIsActuallyRendered":                      "a subtree: web/templates",
	"internal/handler/dashboard_test.go:assertGeneratedMatchesDeclarations":                               "a subtree: web/templates (its one caller)",
	"internal/handler/dashboard_test.go:brandSources":                                                     "a subtree: web/templates",
	"internal/handler/dashboard_test.go:expandGlob":                                                       "subtrees: the part of each tailwind content glob before **",
	"internal/handler/forrestaurants_facts_test.go:productGoFiles":                                        "subtrees: internal, cmd",
	"internal/handler/inlinestyle_test.go:TestTemplates_CarryNoInlineStyle":                               "a subtree: web/templates",
	"internal/handler/marketing_facts_test.go:nonTestGoSource":                                            "subtrees: the directories its callers name",
	"internal/handler/marketing_facts_test.go:shippedScripts":                                             "the embedded static file system, not the disk",
	"internal/handler/marketing_test.go:TestCookieNotice_ListsExactlyTheCookiesTheProductSets":            "subtrees: internal, cmd",
	"internal/handler/marketing_test.go:TestFreeMonths_NoUserFacingSourceCountsThemTheOldWay":             "subtrees: web/templates, internal, cmd, docs/api, web/static/js",
	"internal/handler/marketing_test.go:TestMarketing_ReachesNoThirdParty":                                "a subtree: web/templates",
	"internal/handler/panelsource_test.go:classFuncReturns":                                               "a subtree: web/templates",
	"internal/handler/panelsource_test.go:templateSources":                                                "a subtree: web/templates",
	"internal/handler/panelstart_test.go:TestSignupStructure_DecidesNothingAfterSignUp#2":                 "a subtree: internal/domain/signup",
	"internal/handler/panelwide_test.go:readPanelTemplates":                                               "a subtree: web/templates",
	"internal/handler/policies_test.go:templSwitchArms":                                                   "a subtree: web/templates (its one caller)",
	"internal/handler/storeparamsleak_test.go:TestStoreParams_AreNeverHandedToAPrintingCall":              "subtrees: internal, cmd",
	"internal/handler/suspension_test.go:TestSuspension_TheTapPathNeverNamesASuspension":                  "subtrees: the tap path's packages",
	"internal/httpx/static_cache_test.go:TestStaticImmutableRule_BytesMatchTheREADMEDigests":              "the embedded static file system, not the disk",
	"internal/httpx/static_cache_test.go:TestStaticImmutableRule_OnlyVersionedNames":                      "the embedded static file system, not the disk",
	"internal/sandbox/virtualuid_db_test.go:theWritersAskTheDoorFirst":                                    "skips every directory whose path starts with a dot",
	"internal/webhook/delivery/delivery_test.go:TestPace_IsReachedOnlyFromTests":                          "skips every directory whose name starts with a dot",
}

// treeWalk is one filepath.Walk / filepath.WalkDir / fs.WalkDir call in a test file.
type treeWalk struct {
	key   string // "file:Func" or "file:Func#n"
	wired bool   // its callback skips a nested checkout
}

// treeWalksIn lists src's walks. A walk is WIRED when its callback calls skipScanDir or skipGoDir
// (this package's rule), isCheckout, or os.Lstat on a ".git" path (the shape the walks outside this
// package copy).
func treeWalksIn(rel string, src []byte) ([]treeWalk, error) {
	f, err := parser.ParseFile(token.NewFileSet(), rel, src, 0)
	if err != nil {
		return nil, err
	}
	var out []treeWalk
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		n := 0
		ast.Inspect(fd.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok || !isTreeWalk(call) {
				return true
			}
			n++
			key := rel + ":" + fd.Name.Name
			if n > 1 {
				key += "#" + strconv.Itoa(n)
			}
			wired := false
			for _, arg := range call.Args {
				if lit, ok := arg.(*ast.FuncLit); ok && skipsNestedCheckouts(lit.Body) {
					wired = true
				}
			}
			out = append(out, treeWalk{key: key, wired: wired})
			return true
		})
	}
	return out, nil
}

func isTreeWalk(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	switch pkg.Name + "." + sel.Sel.Name {
	case "filepath.Walk", "filepath.WalkDir", "fs.WalkDir":
		return true
	}
	return false
}

func skipsNestedCheckouts(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return !found
		}
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			switch fun.Name {
			case "skipScanDir", "skipGoDir", "isCheckout":
				found = true
			}
		case *ast.SelectorExpr:
			if x, ok := fun.X.(*ast.Ident); ok && x.Name == "os" && fun.Sel.Name == "Lstat" && mentionsDotGit(call) {
				found = true
			}
		}
		return !found
	})
	return found
}

func mentionsDotGit(call *ast.CallExpr) bool {
	found := false
	ast.Inspect(call, func(node ast.Node) bool {
		if lit, ok := node.(*ast.BasicLit); ok && lit.Kind == token.STRING && lit.Value == `".git"` {
			found = true
		}
		return !found
	})
	return found
}

// TestScanScope_EveryTreeWalkIsWiredOrListed is T218 (a)'s pin: every tree walk in the repository's
// tests either skips nested checkouts (treeWalksIn's wires) or is in walkScopeListed with a reason, and
// the list names no walk that is gone or has since been wired.
//
// It found two repository-root walks with a name list and no wire, both wired with it (T218 (a)):
// internal/sqlbelt's TestNoProductionImport (measured: a nested checkout holding a non-test file that
// imports sqlbelt turned it red) and internal/handler's allTestFuncs.
//
// MUTATIONS (measured, each red here on a fresh checkout's tree -- no nested worktree needed): the
// isCheckout clause removed from TestAPIWiring_TheSuspensionReaderFollowsTheColumn's walk; the
// os.Lstat clause removed from TestComments_DoNotQuoteTheDriftingRosterSize's; skipGoDir replaced in
// eachShippedGoFile's.
func TestScanScope_EveryTreeWalkIsWiredOrListed(t *testing.T) {
	t.Parallel()
	var walks []treeWalk
	files := 0
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(repoRoot, path)
		if rerr != nil {
			return rerr
		}
		if info.IsDir() {
			if skipGoDir(rel) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		files++
		w, perr := treeWalksIn(filepath.ToSlash(rel), src)
		if perr != nil {
			return perr
		}
		walks = append(walks, w...)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 100 || len(walks) < 40 {
		t.Fatalf("PREMISE: %d test file(s), %d walk(s) read -- the scan is reading the wrong tree", files, len(walks))
	}
	var unlisted, stale []string
	seen := map[string]bool{}
	for _, w := range walks {
		seen[w.key] = true
		_, listed := walkScopeListed[w.key]
		switch {
		case !w.wired && !listed:
			unlisted = append(unlisted, w.key)
		case w.wired && listed:
			stale = append(stale, w.key+" (wired now)")
		}
	}
	for k := range walkScopeListed {
		if !seen[k] {
			stale = append(stale, k+" (no such walk)")
		}
	}
	slices.Sort(unlisted)
	slices.Sort(stale)
	if len(unlisted) > 0 {
		t.Errorf("%d tree walk(s) neither skip a nested checkout (skipScanDir / skipGoDir in cmd/tappa, "+
			"isCheckout, os.Lstat on .git) nor are listed in walkScopeListed with a reason:\n  %s",
			len(unlisted), strings.Join(unlisted, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("walkScopeListed is stale:\n  %s", strings.Join(stale, "\n  "))
	}
	wired := 0
	for _, w := range walks {
		if w.wired {
			wired++
		}
	}
	t.Logf("%d walk(s) in %d test file(s): %d wired, %d listed", len(walks), files, wired, len(walks)-wired)
}

// TestScanScope_TheWalkInventorySeesEachWire is the inventory's own control: each wire shape is found,
// and a walk without one is not.
func TestScanScope_TheWalkInventorySeesEachWire(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		wired      bool
	}{
		{"skipScanDir", `if skipScanDir(rel) { return filepath.SkipDir }`, true},
		{"skipGoDir", `if skipGoDir(rel) { return filepath.SkipDir }`, true},
		{"isCheckout", `if path != root && isCheckout(path) { return filepath.SkipDir }`, true},
		{"Lstat of .git", `if _, e := os.Lstat(filepath.Join(path, ".git")); e == nil && path != root { return filepath.SkipDir }`, true},
		{"a name list only", `switch d.Name() { case ".git", "node_modules": return filepath.SkipDir }`, false},
		{"Lstat of something else", `if _, e := os.Lstat(filepath.Join(path, "go.mod")); e == nil { return filepath.SkipDir }`, false},
	} {
		src := fmt.Sprintf("package x\nfunc F() {\n\t_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {\n\t\t%s\n\t\treturn nil\n\t})\n}\n", tc.body)
		w, err := treeWalksIn("x_test.go", []byte(src))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if len(w) != 1 || w[0].key != "x_test.go:F" || w[0].wired != tc.wired {
			t.Errorf("%s: %+v, want one walk x_test.go:F wired=%v", tc.name, w, tc.wired)
		}
	}
	w, err := treeWalksIn("x_test.go", []byte("package x\nfunc G() { filepath.Walk(a, nil); fs.WalkDir(b, \".\", nil) }\n"))
	if err != nil || len(w) != 2 || w[0].key != "x_test.go:G" || w[1].key != "x_test.go:G#2" {
		t.Errorf("two walks in one function: %+v, %v; want x_test.go:G and x_test.go:G#2", w, err)
	}
}
