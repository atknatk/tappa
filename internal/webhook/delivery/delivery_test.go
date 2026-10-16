package delivery

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/egress"
	"github.com/atknatk/tappa/internal/punchwire"
)

// TestBackoff_TheScheduleIsTheADRs pins ADR 0028 §6's "1 dk · 5 dk · 30 dk · 2 sa · 6 sa → dead" read
// literally: the n-th failed try (n = 1..5) waits Backoff[n-1] -- the numbers are written here, not
// read back from the table under test -- and the SIXTH failure is dead; every class fails the same way;
// a 2xx is delivered whatever the count; a revived dead delivery (seven tries and on) is dead again
// after its one try.
func TestBackoff_TheScheduleIsTheADRs(t *testing.T) {
	waits := []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}
	for _, class := range egress.Classes {
		res := egress.Result{Class: class, Status: 0}
		if class == egress.ClassHTTP4xx {
			res.Status = 404
		}
		if class == egress.ClassHTTP5xx {
			res.Status = 503
		}
		for before := int32(0); before < 5; before++ {
			r := Decide(before, res)
			if r.State != StatePending || r.Delay != waits[before] || r.Class == nil || *r.Class != string(class) {
				t.Errorf("%s, try %d: %+v, want pending after %v", class, before+1, r, waits[before])
			}
			if (res.Status == 0) != (r.Status == nil) || (r.Status != nil && int(*r.Status) != res.Status) {
				t.Errorf("%s: the status %v", class, r.Status)
			}
		}
		for _, before := range []int32{5, 6, 9} {
			if r := Decide(before, res); r.State != StateDead || r.Delay != 0 {
				t.Errorf("%s, try %d: %+v, want dead", class, before+1, r)
			}
		}
	}
	for _, before := range []int32{0, 5, 7} {
		if r := Decide(before, egress.Result{Status: 204}); r.State != StateDelivered || r.Class != nil || r.Status == nil || *r.Status != 204 {
			t.Errorf("a 204 after %d tries: %+v", before, r)
		}
	}
	if MaxAttempts != 6 || len(Backoff) != 5 {
		t.Fatalf("MaxAttempts %d, %d waits", MaxAttempts, len(Backoff))
	}
	// A non-2xx with no class cannot come from egress; it is recorded in the closed set all the same.
	if r := Decide(0, egress.Result{Status: 302}); r.Class == nil || *r.Class != string(egress.ClassRefused) {
		t.Fatalf("an unclassed failure: %+v", r)
	}
}

// TestBody_IsTheFeedItemPlusEventAndDeliveryID: a delivery's body is GET /v1/punches' item
// (punchwire.Of, the same function the feed answers with) with two keys more -- event "punch" and the
// delivery's dlv_ id -- and the same bytes every time it is encoded (a retry is the same body).
func TestBody_IsTheFeedItemPlusEventAndDeliveryID(t *testing.T) {
	ref, code, co := "E-1", "C-1", "KF"
	loc := uuid.New()
	tag := "04A1B2C3D4E5F6"
	e := tenant.PunchEvent{Seq: 7, PunchID: uuid.New(), Version: 1, Status: "valid", PunchTime: time.Date(2026, 10, 15, 8, 0, 0, 0, time.UTC),
		RecordedAt: time.Date(2026, 10, 15, 8, 0, 1, 0, time.UTC), Channel: "nfc", Direction: "in", EmployeeID: uuid.New(),
		EmployeeRef: &ref, EmployeeCode: &code, EmployeeCompany: &co, LocationID: &loc, LocationRef: &ref, LocationName: &code,
		LocationCompany: &co, TagUID: &tag}
	id := uuid.New()
	body, err := Encode(id, e)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := Encode(id, e)
	if string(body) != string(again) {
		t.Fatal("two encodings of one delivery differ")
	}
	var got, item map[string]any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(punchwire.Of(e))
	if err := json.Unmarshal(raw, &item); err != nil {
		t.Fatal(err)
	}
	if got["event"] != "punch" || got["deliveryId"] != "dlv_"+id.String() {
		t.Fatalf("event %v, deliveryId %v", got["event"], got["deliveryId"])
	}
	delete(got, "event")
	delete(got, "deliveryId")
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(item)
	if string(a) != string(b) {
		t.Fatalf("the body without its two keys is not the feed item:\n%s\n%s", a, b)
	}
}

// TestDeliveryJob_NamesOnlyBeltedQueries pins K-19 and CLAUDE.md §4.5 on this package: every store
// query a non-test file calls is declared in db/queries/webhooks.sql or apikeys.sql -- the two files
// whose every statement TestWebhookQueries_CarryAnExplicitTenantPredicate and
// TestAPIKeyQueries_CarryAnExplicitTenantPredicate hold to an explicit tenant predicate. A query from
// anywhere else (a scan across tenants included) is red here first. CONTROL: the calls are found.
func TestDeliveryJob_NamesOnlyBeltedQueries(t *testing.T) {
	declared := map[string]bool{}
	re := regexp.MustCompile(`(?m)^-- name: (\w+) `)
	for _, f := range []string{"webhooks.sql", "apikeys.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "queries", f))
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
			declared[m[1]] = true
		}
	}
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var called []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// store.New(tx).X(...) or q.X(...) where q is a *store.Queries.
			switch x := sel.X.(type) {
			case *ast.CallExpr:
				if inner, ok := x.Fun.(*ast.SelectorExpr); ok && inner.Sel.Name == "New" {
					if id, ok := inner.X.(*ast.Ident); ok && id.Name == "store" {
						called = append(called, sel.Sel.Name)
					}
				}
			case *ast.Ident:
				if x.Name == "q" {
					called = append(called, sel.Sel.Name)
				}
			}
			return true
		})
	}
	slices.Sort(called)
	called = slices.Compact(called)
	if len(called) < 5 {
		t.Fatalf("CONTROL: only %v found", called)
	}
	for _, c := range called {
		if !declared[c] {
			t.Errorf("%s is not declared in webhooks.sql or apikeys.sql (the belted files)", c)
		}
	}
	t.Logf("store calls: %v", called)
}

// TestNew_RefusesAMissingPart: a KEK of the wrong size, or no database or sender, builds no job.
func TestNew_RefusesAMissingPart(t *testing.T) {
	if _, err := New(make([]byte, 16), nil, nil, nil); err == nil {
		t.Fatal("a 16-byte KEK")
	}
	if _, err := New(make([]byte, 32), nil, nil, nil); err == nil {
		t.Fatal("no database or sender")
	}
}

// TestPace_IsReachedOnlyFromTests pins Pace's doc ("cmd/tappa never calls it"): Pace is an exported
// seam -- the worker's fairness tests in internal/egress shorten the run budget and the gate recheck
// with it -- so the compiler lets any package call it, and a production call would silently change
// the turn bound and the recheck the ADR measures (ADR 0028, the API-10 note). Every non-test .go file
// of the repository is parsed and no selector `.Pace` may appear in one (a call or a method value).
// CONTROL: the scanner finds the test files' uses, and a synthetic production file's call.
//
// MUTANT (measured): `_ = deliveries.Pace` / a `deliveries.Pace(...)` call added to cmd/tappa/main.go
// -> red.
func TestPace_IsReachedOnlyFromTests(t *testing.T) {
	uses := func(name string, src []byte) int {
		if !strings.Contains(string(src), "Pace") {
			return 0
		}
		file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		n := 0
		ast.Inspect(file, func(node ast.Node) bool {
			if sel, ok := node.(*ast.SelectorExpr); ok && sel.Sel.Name == "Pace" {
				n++
			}
			return true
		})
		return n
	}
	if uses("synthetic.go", []byte("package main\nfunc f(j *x) { _ = j.Pace(1, 2) }\n")) != 1 {
		t.Fatal("CONTROL: the scanner does not see a call")
	}
	root := filepath.Join("..", "..", "..")
	inTests, files := 0, 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || d.Name() == "testdata" || d.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files++
		n := uses(path, src)
		if strings.HasSuffix(path, "_test.go") {
			inTests += n
			return nil
		}
		if n > 0 {
			t.Errorf("%s reaches delivery.Job.Pace %d time(s); only tests may (the run budget and the recheck are the design)", path, n)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 100 || inTests == 0 {
		t.Fatalf("CONTROL: %d .go files read, %d uses in tests found -- the walk does not see the repository", files, inTests)
	}
}
