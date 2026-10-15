package webhook

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

// countingJob records the tenants it was handed.
type countingJob struct {
	mu      sync.Mutex
	tenants []uuid.UUID
	ran     chan struct{}
	block   bool
}

func (j *countingJob) RunFor(ctx context.Context, tenant uuid.UUID) error {
	j.mu.Lock()
	j.tenants = append(j.tenants, tenant)
	j.mu.Unlock()
	if j.ran != nil {
		j.ran <- struct{}{}
	}
	if j.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (j *countingJob) got() []uuid.UUID {
	j.mu.Lock()
	defer j.mu.Unlock()
	return slices.Clone(j.tenants)
}

func quiet() *slog.Logger { return slog.New(slog.DiscardHandler) }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestWorker_NoticedTenantsReachEveryJob (K-19): a noticed tenant is handed to every job; one noticed
// many times before the loop takes it is handed once; the nil tenant is ignored; nothing is handed
// that was not noticed -- the worker has no other source of tenants.
func TestWorker_NoticedTenantsReachEveryJob(t *testing.T) {
	gate := make(chan struct{})
	first := &countingJob{}
	hold := jobFunc(func(ctx context.Context, _ uuid.UUID) error { <-gate; return nil })
	second := &countingJob{}
	w, err := NewWorker(quiet(), hold, first, second)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Drain(context.Background()) })
	a, b := uuid.New(), uuid.New()
	w.Notice(a) // the loop takes {a} and blocks in `hold`
	time.Sleep(20 * time.Millisecond)
	for range 50 {
		w.Notice(b)
	}
	w.Notice(uuid.Nil)
	close(gate)
	waitFor(t, func() bool { return len(second.got()) == 2 })
	time.Sleep(50 * time.Millisecond)
	for _, j := range []*countingJob{first, second} {
		got := j.got()
		slices.SortFunc(got, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
		want := []uuid.UUID{a, b}
		slices.SortFunc(want, func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) })
		if !slices.Equal(got, want) {
			t.Fatalf("a job was handed %v, want [a b] once each", got)
		}
	}
	if _, err := NewWorker(quiet(), nil); err == nil {
		t.Fatal("a nil job was accepted")
	}
	if err := w.Start(t.Context()); err == nil {
		t.Fatal("a second Start was accepted")
	}
}

type jobFunc func(ctx context.Context, tenant uuid.UUID) error

func (f jobFunc) RunFor(ctx context.Context, tenant uuid.UUID) error { return f(ctx, tenant) }

// TestWorker_DrainsWithinTheShutdownBudget: Drain cancels a job in flight and returns as soon as the
// loop does -- well inside the budget -- and refuses notices from then on; a job that ignores its
// context is not waited for past the budget (Drain says so instead). A worker never started drains
// at once.
func TestWorker_DrainsWithinTheShutdownBudget(t *testing.T) {
	j := &countingJob{block: true, ran: make(chan struct{}, 1)}
	w, err := NewWorker(quiet(), j)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	w.Notice(uuid.New())
	<-j.ran
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	if err := w.Drain(ctx); err != nil {
		t.Fatalf("Drain: %v", err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("Drain took %v with a cooperative job", d)
	}
	w.Notice(uuid.New())
	if n := len(w.take()); n != 0 {
		t.Fatalf("%d tenants accepted after Drain", n)
	}

	stubborn := jobFunc(func(context.Context, uuid.UUID) error { time.Sleep(time.Second); return nil })
	w2, _ := NewWorker(quiet(), stubborn)
	_ = w2.Start(context.Background())
	w2.Notice(uuid.New())
	time.Sleep(20 * time.Millisecond)
	short, cancel2 := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel2()
	start = time.Now()
	if err := w2.Drain(short); err == nil || time.Since(start) > 500*time.Millisecond {
		t.Fatalf("a stubborn job: Drain %v after %v, want an error at the budget", err, time.Since(start))
	}
	w3, _ := NewWorker(quiet())
	if err := w3.Drain(short); err != nil {
		t.Fatalf("a worker never started: %v", err)
	}
}

// TestWorker_TwoProcessesBothRunTheJob measures the set's scope: it is per PROCESS. Two workers (two
// pods) noticing one tenant both hand it to their job -- so the "iki pod tek iş" guarantee cannot
// come from here; a Job takes its rows under a lease (ADR 0028 §6, API-10's acceptance).
func TestWorker_TwoProcessesBothRunTheJob(t *testing.T) {
	var runs atomic.Int64
	tenant := uuid.New()
	job := jobFunc(func(_ context.Context, got uuid.UUID) error {
		if got == tenant {
			runs.Add(1)
		}
		return nil
	})
	for range 2 {
		w, _ := NewWorker(quiet(), job)
		_ = w.Start(t.Context())
		t.Cleanup(func() { _ = w.Drain(context.Background()) })
		w.Notice(tenant)
	}
	waitFor(t, func() bool { return runs.Load() == 2 })
}

// TestWorker_NoTenantWideRead pins K-19's shape on the package's source (non-test files):
//
//	imports      no internal/db, no internal/store, no pgx -- the worker itself reads nothing;
//	             a Job brings its own queries
//	Job          exactly one method, RunFor(context.Context, uuid.UUID) error: a job is handed ONE
//	             tenant and has no other entry
//	queries      every sqlc query a file of the package names (none today; API-10's delivery and
//	             API-6's queue will) carries an explicit `tenant_id = @tenant_id` filter -- checked
//	             against db/queries; the checker is shown red on a synthetic file naming an unscoped
//	             query
//
// A tenant list built any other way (a loop over tenants, a query without the filter) is what
// K-19 forbids; the import and query arms catch the two ways it could arrive.
func TestWorker_NoTenantWideRead(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	queries := sqlcQueries(t)
	unscoped := ""
	for name, sql := range queries {
		if !tenantScoped(sql) {
			unscoped = name
			break
		}
	}
	if unscoped == "" {
		t.Fatal("CONTROL: db/queries has no query without a tenant filter to show the checker red on")
	}
	if bad := unscopedCalls("synthetic.go", "package webhook\nfunc f(q x) { q."+unscoped+"(ctx) }\n", queries); len(bad) != 1 {
		t.Fatalf("CONTROL: the checker did not see %s in a synthetic file: %v", unscoped, bad)
	}
	parsed := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		parsed++
		file, err := parser.ParseFile(token.NewFileSet(), f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, im := range file.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			if strings.HasSuffix(p, "/internal/db") || strings.HasSuffix(p, "/internal/store") || strings.Contains(p, "jackc/pgx") {
				t.Errorf("%s imports %s: the worker reads nothing itself (K-19)", f, p)
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Job" {
				return true
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok || len(it.Methods.List) != 1 || it.Methods.List[0].Names[0].Name != "RunFor" {
				t.Errorf("Job is not the one method RunFor")
				return false
			}
			ft := it.Methods.List[0].Type.(*ast.FuncType)
			var params []string
			for _, p := range ft.Params.List {
				for range max(len(p.Names), 1) {
					params = append(params, types(p.Type))
				}
			}
			if !slices.Equal(params, []string{"context.Context", "uuid.UUID"}) {
				t.Errorf("RunFor takes %v, want (context.Context, uuid.UUID)", params)
			}
			return false
		})
		if bad := unscopedCalls(f, string(src), queries); len(bad) > 0 {
			t.Errorf("%s names queries without a tenant filter: %v", f, bad)
		}
	}
	if parsed == 0 {
		t.Fatal("anti-vacuity: no source file read")
	}
}

func types(e ast.Expr) string {
	if s, ok := e.(*ast.SelectorExpr); ok {
		if id, ok := s.X.(*ast.Ident); ok {
			return id.Name + "." + s.Sel.Name
		}
	}
	return "?"
}

// sqlcQueries maps every sqlc query name in db/queries to its SQL text.
func sqlcQueries(t *testing.T) map[string]string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "db", "queries", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("db/queries: %v", err)
	}
	out := map[string]string{}
	head := regexp.MustCompile(`(?m)^-- name: (\w+) `)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		idx := head.FindAllStringSubmatchIndex(string(b), -1)
		for i, m := range idx {
			end := len(b)
			if i+1 < len(idx) {
				end = idx[i+1][0]
			}
			out[string(b[m[2]:m[3]])] = string(b[m[1]:end])
		}
	}
	return out
}

func tenantScoped(sql string) bool {
	return regexp.MustCompile(`tenant_id\s*=\s*(@tenant_id|\$\d+|sqlc\.arg\(tenant_id\))`).MatchString(sql)
}

// unscopedCalls names every query of queries that src calls as a method and that carries no tenant filter.
func unscopedCalls(name, src string, queries map[string]string) []string {
	var bad []string
	for _, m := range regexp.MustCompile(`\.(\w+)\(`).FindAllStringSubmatch(src, -1) {
		if sql, ok := queries[m[1]]; ok && !tenantScoped(sql) {
			bad = append(bad, name+": "+m[1])
		}
	}
	return bad
}

// laterJob answers "come back in after" the first n times it runs, then nil; failing joins a
// failure to the first answer.
type laterJob struct {
	mu      sync.Mutex
	runs    int
	n       int
	after   time.Duration
	failing bool
}

type dueIn time.Duration

func (d dueIn) Error() string        { return "due later" }
func (d dueIn) DueIn() time.Duration { return time.Duration(d) }

func (j *laterJob) RunFor(context.Context, uuid.UUID) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.runs++
	if j.runs > j.n {
		return nil
	}
	if j.failing && j.runs == 1 {
		return errors.Join(errors.New("one request failed"), dueIn(j.after))
	}
	return dueIn(j.after)
}

func (j *laterJob) count() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.runs
}

// recordingHandler counts error records.
type recordingHandler struct {
	slog.Handler
	errors atomic.Int32
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *recordingHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelError {
		h.errors.Add(1)
	}
	return nil
}

// TestWorker_ALaterTenantIsNoticedAgainAndIsNoFailure (M11 API-6, K-19): a job that answers DueLater
// (the invitation queue's *invite.Later: a limit, a back-off) has its tenant noticed AGAIN after the
// delay -- a timer for that one tenant, nothing else -- and a bare DueLater is not logged as a
// failure; one joined with a failure is. Nothing is noticed after Drain.
func TestWorker_ALaterTenantIsNoticedAgainAndIsNoFailure(t *testing.T) {
	for _, failing := range []bool{false, true} {
		h := &recordingHandler{Handler: slog.DiscardHandler}
		j := &laterJob{n: 2, after: 20 * time.Millisecond, failing: failing}
		w, err := NewWorker(slog.New(h), j)
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		w.Notice(uuid.New())
		waitFor(t, func() bool { return j.count() == 3 })
		want := int32(0)
		if failing {
			want = 1
		}
		if got := h.errors.Load(); got != want {
			t.Fatalf("failing=%v: %d error records, want %d", failing, got, want)
		}
		if err := w.Drain(context.Background()); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
		if j.count() != 3 {
			t.Fatalf("the job ran %d times; after it answered nil nothing noticed the tenant again", j.count())
		}
	}
}
