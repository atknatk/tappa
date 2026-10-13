package checkin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/policy"
)

// TestDecisionLevel_NothingIsError pins the normative invariant observability.go
// declares in capitals and NOTHING was holding.
//
// 🔴 MEASURED BY MUTATION: making decisionLevel return slog.LevelError for reject
// and flag left FOUR packages green — internal/domain/checkin, internal/handler,
// internal/httpx and cmd/tappa. The comment above the function calls the mapping a
// contract with deploy/README.md; a contract nothing checks is a preference.
//
// 🔴 AND THE COST OF THE DRIFT IS NOT THE ONE THE COMMENT USED TO NAME. It said an
// ERROR verdict would "make the 5xx rule fire on a person tapping a retired plaque".
// It would not: the shipped rule is
//
//	body.msg = "http.request" AND body.status >= 500
//
// which keys on the EVENT and the STATUS, not on the level, so a tap.decision record
// at ERROR never enters it. What is actually lost is coarser and more useful —
// level=ERROR is the one filter an operator can type without knowing this product's
// event names, and it means "this server is broken". Putting the product working
// correctly (a refused tap IS the product working) at that level fills the operator's
// broadest filter with routine traffic, and the 5xx signal drowns in it rather than
// misfiring. That is a slow failure instead of a loud one, which is why nothing
// caught it.
//
// The reject/flag = WARN half matters too: it is what makes the level knob
// meaningful (TestObservability_TheShippedLogLevelAdmitsTheSignals in cmd/tappa
// measures what happens when the shipped level rises above INFO).
func TestDecisionLevel_NothingIsError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		verdict tap.Verdict
		want    slog.Level
	}{
		{"reject is a refused person, not a broken server", tap.VerdictReject, slog.LevelWarn},
		{"flag is a manager's queue, not a broken server", tap.VerdictFlag, slog.LevelWarn},
		{"ok is the ordinary case", tap.VerdictOK, slog.LevelInfo},
		{"ignored is the debounce", tap.VerdictIgnored, slog.LevelInfo},
		// The default arm, asserted rather than assumed: an unknown verdict must
		// not be able to reach ERROR either.
		{"an unknown verdict falls back to INFO", tap.Verdict("probe-not-a-verdict"), slog.LevelInfo},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := decisionLevel(tc.verdict)
			if got != tc.want {
				t.Errorf("decisionLevel(%q) = %v, want %v", tc.verdict, got, tc.want)
			}
			if got >= slog.LevelError {
				t.Errorf("decisionLevel(%q) = %v, which is ERROR or above. observability.go declares "+
					"NOTHING IS ERROR, DELIBERATELY: level=ERROR is the coarse operator filter for "+
					"\"this server is broken\" (httpx.AccessLog puts a 5xx there), and a tap verdict is "+
					"the product working correctly. Moving one here does not make the 5xx alert rule "+
					"misfire — that rule filters on msg and status, not level — it makes the broadest "+
					"filter an operator can type useless.", tc.verdict, got)
			}
		})
	}
}

// TestSecurityAlert_TheEvidenceIsWrittenBeforeTheNotification — M8-03 round 4, S6.
//
// 🔴 THE CONTRADICTION IT CLOSES. Two lines above the security block, checkin.go
// says "the audit row comes after the transaction row on purpose — the trail refers
// to a record that exists". The block itself then wrote the LOG LINE first and the
// AUDIT ROW second, so a panic between them cost the row and kept the notification —
// the exact inversion of the sentence, and of §4.6: the audit row is the evidence, a
// log line is only a message about it.
//
// ⚠️ IT IS HARDENING, NOT A LIVE DEFECT, AND THAT IS SAID PLAINLY. With the shipped
// logger the hazard is UNREACHABLE: slog does not panic on a write error. It is
// still worth ordering, because a slog.Handler CAN panic — internal/httpx had to
// contain exactly that, measured, when a handler's Handle raised — and this call
// sits inside no recovery of its own.
//
// IT IS AN AST TEST RATHER THAN A BEHAVIOURAL ONE, and the reason is honest: making
// this fail at runtime needs a logger that panics AND a full tap through a real
// database, and the assertion would then be about the panicking logger rather than
// about the order. The order is a property of the source, so the source is what is
// read. A grep would drift on formatting; the parser does not.
func TestSecurityAlert_TheEvidenceIsWrittenBeforeTheNotification(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "checkin.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing checkin.go: %v", err)
	}

	// selectorOf renders `s.log.WarnContext` / `s.record` for a call statement, or ""
	// for a statement that is not a call.
	selectorOf := callSelector

	var found bool
	ast.Inspect(file, func(n ast.Node) bool {
		ifStmt, ok := n.(*ast.IfStmt)
		if !ok {
			return true
		}
		sel, ok := ifStmt.Cond.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Security" {
			return true
		}
		found = true

		record, logLine := -1, -1
		for i, stmt := range ifStmt.Body.List {
			switch selectorOf(stmt) {
			case "s.record":
				record = i
			case "s.log.WarnContext":
				logLine = i
			}
		}
		if record < 0 {
			t.Errorf("the §5 row 4 block no longer writes an audit row at all — the evidence is gone")
			return false
		}
		if logLine < 0 {
			t.Errorf("the §5 row 4 block no longer raises %s — §5 calls this a security alert, "+
				"and an entry nobody is told about is not one", EventTapSecurityAlert)
			return false
		}
		if record > logLine {
			t.Errorf("the security NOTIFICATION (statement %d) is written before the audit ROW "+
				"(statement %d). A panic between them costs the evidence and keeps the message, "+
				"which is the inversion of §4.6 and of the sentence checkin.go states two lines "+
				"above this block.", logLine, record)
		}
		return false
	})

	if !found {
		t.Fatalf("no `if dec.Security` block found in checkin.go — this test is measuring nothing")
	}
}

// callSelector renders `s.log.WarnContext` / `s.record` for a call statement, or ""
// for a statement that is not a call. It was a closure inside the test above until
// a second test needed the same reading; the body is unchanged.
func callSelector(stmt ast.Stmt) string {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return ""
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok {
		return ""
	}
	var parts []string
	for node := call.Fun; ; {
		sel, ok := node.(*ast.SelectorExpr)
		if !ok {
			if id, ok := node.(*ast.Ident); ok {
				parts = append([]string{id.Name}, parts...)
			}
			break
		}
		parts = append([]string{sel.Sel.Name}, parts...)
		node = sel.X
	}
	return strings.Join(parts, ".")
}

// TestSecurityAlert_TheDecisionLineComesAfterTheEvidence — M8-03 round 5, and it is
// the SAME rule as the test above, one statement further out.
//
// 🔴 THE TREE CONCEDED THE HAZARD AND THEN IGNORED IT TWO LINES AWAY. Round 4
// ordered the audit row before the security notification, and wrote the reason into
// checkin.go: "a slog.Handler CAN panic". But the per-tap `tap.decision` line ran
// BEFORE the whole `if dec.Security` block and inside no recovery, so a handler that
// raised there cost the audit_log row outright — the exact failure the neighbouring
// comment was guarding against, reached by a shorter path.
//
// MEASURED end to end (handler.TestSeedDB_APanickingLoggerCannotCostTheSecurityAuditRow,
// a writer that raises on the tap.decision record): audit rows for tap_security_alert
// went 530 -> 530 with the old order and 530 -> 531 with this one.
//
// ⚠️ WHAT WAS NEVER AT RISK, said plainly so this is not oversold: the ATTENDANCE
// record. db.WithTenant has committed the transaction before any of these lines run,
// so the timesheet survives a panic either way (§4.3/§4.6 are not in play). What the
// order protects is the security event's evidence and its notification.
//
// WHY REORDER RATHER THAN recover(): a recover here would swallow a panic the process
// should probably die on, would have to be repeated at every log site, and would add
// a branch nothing else in this package has. The order states one rule instead —
// everything that writes EVIDENCE runs before anything that only writes a MESSAGE.
//
// AST rather than behaviour, for the reason its sibling gives: the running proof
// lives in internal/handler where a real database and a real HTTP boundary exist;
// what is asserted HERE is the property of the source that makes it true.
func TestSecurityAlert_TheDecisionLineComesAfterTheEvidence(t *testing.T) {
	t.Parallel()

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "checkin.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing checkin.go: %v", err)
	}

	// decisionLogIndex reports the position of the `s.log.Log(..., EventTapDecision,
	// ...)` statement in a block, or -1. It matches on the EVENT CONSTANT rather than
	// on the method name: s.log.Log is generic and a future second call would make a
	// name-only match ambiguous.
	decisionLogIndex := func(list []ast.Stmt) int {
		for i, stmt := range list {
			if callSelector(stmt) != "s.log.Log" {
				continue
			}
			for _, arg := range stmt.(*ast.ExprStmt).X.(*ast.CallExpr).Args {
				if id, ok := arg.(*ast.Ident); ok && id.Name == "EventTapDecision" {
					return i
				}
			}
		}
		return -1
	}

	var checked bool
	ast.Inspect(file, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok {
			return true
		}
		logAt := decisionLogIndex(block.List)
		if logAt < 0 {
			return true
		}
		securityAt := -1
		for i, stmt := range block.List {
			ifStmt, ok := stmt.(*ast.IfStmt)
			if !ok {
				continue
			}
			if sel, ok := ifStmt.Cond.(*ast.SelectorExpr); ok && sel.Sel.Name == "Security" {
				securityAt = i
			}
		}
		if securityAt < 0 {
			t.Errorf("the block holding the %s line no longer holds the `if dec.Security` block; "+
				"they must stay together or this ordering cannot be stated at all", EventTapDecision)
			checked = true
			return false
		}
		checked = true
		if logAt < securityAt {
			t.Errorf("the %s line is statement %d and the security block is statement %d: the "+
				"MESSAGE runs before the EVIDENCE. A slog.Handler that panics on the decision "+
				"record then costs the %s audit row entirely — measured 530 -> 530 in that order, "+
				"530 -> 531 in this one.", EventTapDecision, logAt, securityAt, ActionTapSecurityAlert)
		}
		return false
	})

	if !checked {
		t.Fatalf("no block containing the %s line was found in checkin.go — this test is "+
			"measuring nothing", EventTapDecision)
	}
}

// scriptedData is a Database whose WithTenant answers from a script, one entry per
// call, and NEVER runs the callback. A nil entry is a call that succeeded and read
// nothing — to load that is a tenant with no policy rows, to materialise a write that
// went through — so each of forTenant's four fallback paths is one script:
//
//	load fails                      [err]
//	materialise fails               [nil, err]
//	re-load fails                   [nil, nil, err]
//	incomplete after materialising  [nil, nil, nil]  (the read-back still finds nothing)
//
// The last two are why this is a script and not a database: both need the SECOND
// read to disagree with the write just before it, which a healthy Postgres will not
// do on request. calls is checked after every run, so a test cannot pass by walking
// a different path than its script names.
type scriptedData struct {
	script []error
	calls  int
}

func (s *scriptedData) WithTenant(_ context.Context, _ uuid.UUID, _ db.TxFunc) error {
	i := s.calls
	s.calls++
	if i < len(s.script) {
		return s.script[i]
	}
	return nil
}

func (s *scriptedData) GetTagByUID(context.Context, string) (db.ResolvedTag, error) {
	return db.ResolvedTag{}, errors.New("scriptedData: the policy set never resolves a tag")
}

// logRecords decodes a JSON handler's output, one record per line.
func logRecords(t *testing.T, out []byte) []map[string]any {
	t.Helper()
	var recs []map[string]any
	for _, ln := range bytes.Split(bytes.TrimSpace(out), []byte("\n")) {
		if len(ln) == 0 {
			continue
		}
		m := map[string]any{}
		if err := json.Unmarshal(ln, &m); err != nil {
			t.Fatalf("a log line is not JSON: %q: %v", ln, err)
		}
		recs = append(recs, m)
	}
	return recs
}

// probePgError is a unique violation as pgx hands one over, wrapped once the way
// materialise wraps its statements.
func probePgError() (*pgconn.PgError, error) {
	pg := &pgconn.PgError{
		Severity:       "ERROR",
		Code:           "23505",
		Message:        `duplicate key value violates unique constraint "policies_pkey"`,
		ConstraintName: "policies_pkey",
	}
	return pg, fmt.Errorf("ensure baseline policy %q: %w", "probe", pg)
}

// TestPolicyFallback_EachForTenantPathNamesItsReason — T122. Each of the four paths on
// which forTenant decides a tap on the guardrails alone writes EXACTLY ONE
// EventPolicyFallback record at ERROR, carrying the tenant and its own LogReason —
// and, where a PostgreSQL error is behind it, the SQLSTATE pair.
//
// 🔴 THE FAILURE THIS EXISTS FOR IS A PATH THAT FALLS BACK WITHOUT PAGING.
// deploy/README.md's M8-03 rule 8 counts this event and nothing else, so a path that
// answers with the guardrails alone under any other msg is a tap recorded as a `flag`
// that no operator hears about. cmd/tappa's pins hold the NAME; only running the
// paths holds the EMISSION.
func TestPolicyFallback_EachForTenantPathNamesItsReason(t *testing.T) {
	t.Parallel()

	pg, wrapped := probePgError()
	tests := []struct {
		name    string
		script  []error
		reason  string
		withErr bool
	}{
		{"the first read fails", []error{wrapped}, FallbackLoadFailed, true},
		{"writing the missing baseline fails", []error{nil, wrapped}, FallbackMaterialiseFailed, true},
		{"reading it back fails", []error{nil, nil, wrapped}, FallbackReloadFailed, true},
		{"the read-back is still incomplete", []error{nil, nil, nil}, FallbackIncomplete, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := &scriptedData{script: tc.script}
			var out bytes.Buffer
			sets := policySets{data: data, params: policy.DefaultParams(), log: slog.New(slog.NewJSONHandler(&out, nil))}
			tenantID := uuid.New()

			set := sets.forTenant(context.Background(), tenantID)

			if data.calls != len(tc.script) {
				t.Fatalf("forTenant made %d database calls, the script names %d: the path under test "+
					"is not the one this case describes", data.calls, len(tc.script))
			}
			if len(set.Baseline) != 0 || len(set.Guardrails) == 0 {
				t.Fatalf("the Set has %d baseline and %d guardrail policies; this path must answer with "+
					"the guardrails ALONE", len(set.Baseline), len(set.Guardrails))
			}

			var events []map[string]any
			for _, r := range logRecords(t, out.Bytes()) {
				if r[slog.MessageKey] == EventPolicyFallback {
					events = append(events, r)
				}
			}
			if len(events) != 1 {
				t.Fatalf("%d %s records, want exactly 1. A tap decided on the guardrails alone that "+
					"writes no such record pages nobody (M8-03 rule 8). Output:\n%s",
					len(events), EventPolicyFallback, out.String())
			}
			ev := events[0]
			if ev[slog.LevelKey] != slog.LevelError.String() {
				t.Errorf("level = %v, want %s", ev[slog.LevelKey], slog.LevelError)
			}
			if ev[LogReason] != tc.reason {
				t.Errorf("%s = %v, want %q", LogReason, ev[LogReason], tc.reason)
			}
			if ev[LogTenantID] != tenantID.String() {
				t.Errorf("%s = %v, want %s", LogTenantID, ev[LogTenantID], tenantID)
			}
			_, hasErr := ev["err"]
			_, hasState := ev[LogSQLState]
			_, hasConstraint := ev[LogConstraint]
			if !tc.withErr {
				if hasErr || hasState || hasConstraint {
					t.Errorf("a path with no error carries err/%s/%s: %v", LogSQLState, LogConstraint, ev)
				}
				return
			}
			if !hasErr {
				t.Errorf("the record does not carry the error at all: %v", ev)
			}
			if ev[LogSQLState] != pg.Code || ev[LogConstraint] != pg.ConstraintName {
				t.Errorf("%s = %v, %s = %v; want %s and %s from the PgError in the chain",
					LogSQLState, ev[LogSQLState], LogConstraint, ev[LogConstraint], pg.Code, pg.ConstraintName)
			}
		})
	}
}

// TestPolicyFallback_TheStoredSetNeverPages: the panel's read (StoredSet) answers its
// own failures with the guardrails alone too, and writes a line for each — but never
// EventPolicyFallback. There the fallback is an authorisation REFUSAL, and for a
// business no tap has provisioned yet it is the expected state; paging on it would
// teach the operator to ignore rule 8.
func TestPolicyFallback_TheStoredSetNeverPages(t *testing.T) {
	t.Parallel()

	_, wrapped := probePgError()
	tests := []struct {
		name   string
		script []error
		level  slog.Level
	}{
		{"the stored read fails", []error{wrapped}, slog.LevelError},
		{"the stored baseline is incomplete", []error{nil}, slog.LevelWarn},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := &scriptedData{script: tc.script}
			var out bytes.Buffer
			svc := &Service{policies: policySets{data: data, params: policy.DefaultParams(),
				log: slog.New(slog.NewJSONHandler(&out, nil))}}

			set := svc.StoredSet(context.Background(), uuid.New())

			if data.calls != len(tc.script) || len(set.Baseline) != 0 {
				t.Fatalf("StoredSet made %d calls (script %d) and kept %d baseline policies: not the "+
					"guardrails-alone path this case describes", data.calls, len(tc.script), len(set.Baseline))
			}
			// POSITIVE CONTROL: the line IS written. Without it, "no fallback event" would
			// also be what a silenced logger says.
			var written bool
			for _, r := range logRecords(t, out.Bytes()) {
				if r[slog.LevelKey] == tc.level.String() {
					written = true
				}
				if r[slog.MessageKey] == EventPolicyFallback {
					t.Errorf("StoredSet wrote %s; the panel's refusal must not page the operator: %v",
						EventPolicyFallback, r)
				}
			}
			if !written {
				t.Fatalf("StoredSet wrote no %s line on this path; the case proves nothing. Output:\n%s",
					tc.level, out.String())
			}
		})
	}
}

// TestPolicyFallback_AnEndedRequestDoesNotPage — T122 round 2. When the tap request's
// OWN context is over (client gone, httpx.RequestTimeout reached), every forTenant
// path writes the non-paging policyFallbackAbandoned line at WARN and not
// EventPolicyFallback: the WithTenant that would record the tap runs on the same dead
// context and fails, so "an ok recorded as a flag" would be a false page.
//
// THE OTHER HALF IS THE POINT AS MUCH AS THIS ONE: the guard reads ctx.Err(), not
// the error. A context error from inside the driver while the request is ALIVE (a
// pool or statement deadline) is a database problem and must still page — so does
// every path under a live context.
func TestPolicyFallback_AnEndedRequestDoesNotPage(t *testing.T) {
	t.Parallel()

	_, pgWrapped := probePgError()
	ended := func() context.Context {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx
	}
	expired := func() context.Context {
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		t.Cleanup(cancel)
		return ctx
	}
	live := context.Background
	canceledErr := fmt.Errorf("checkin: list policy set: %w", context.Canceled)
	driverDeadline := fmt.Errorf("checkin: list policy set: %w", context.DeadlineExceeded)

	tests := []struct {
		name   string
		ctx    func() context.Context
		script []error
		reason string
		pages  bool
	}{
		{"cancelled request, load fails", ended, []error{canceledErr}, FallbackLoadFailed, false},
		{"cancelled request, materialise fails", ended, []error{nil, canceledErr}, FallbackMaterialiseFailed, false},
		{"cancelled request, re-load fails", ended, []error{nil, nil, canceledErr}, FallbackReloadFailed, false},
		{"cancelled request, read-back incomplete", ended, []error{nil, nil, nil}, FallbackIncomplete, false},
		{"timed-out request, load fails", expired, []error{driverDeadline}, FallbackLoadFailed, false},
		// The guard is the REQUEST, not the error: the same deadline error under a live
		// request is the driver's, and pages.
		{"live request, the driver's own deadline", live, []error{driverDeadline}, FallbackLoadFailed, true},
		{"live request, load fails", live, []error{pgWrapped}, FallbackLoadFailed, true},
		{"live request, read-back incomplete", live, []error{nil, nil, nil}, FallbackIncomplete, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := &scriptedData{script: tc.script}
			var out bytes.Buffer
			sets := policySets{data: data, params: policy.DefaultParams(), log: slog.New(slog.NewJSONHandler(&out, nil))}

			set := sets.forTenant(tc.ctx(), uuid.New())

			if data.calls != len(tc.script) || len(set.Baseline) != 0 {
				t.Fatalf("forTenant made %d calls (script %d) and kept %d baseline policies: not the "+
					"path this case describes", data.calls, len(tc.script), len(set.Baseline))
			}
			var events, abandoned []map[string]any
			for _, r := range logRecords(t, out.Bytes()) {
				switch r[slog.MessageKey] {
				case EventPolicyFallback:
					events = append(events, r)
				case policyFallbackAbandoned:
					abandoned = append(abandoned, r)
				}
			}
			if tc.pages {
				if len(events) != 1 || len(abandoned) != 0 {
					t.Fatalf("%d %s and %d abandoned lines under a live request, want 1 and 0. A live "+
						"request's fallback is a real one and must page. Output:\n%s",
						len(events), EventPolicyFallback, len(abandoned), out.String())
				}
				return
			}
			if len(events) != 0 {
				t.Fatalf("an ENDED request wrote %s: rule 8 would page on a tap that was never "+
					"recorded. Output:\n%s", EventPolicyFallback, out.String())
			}
			if len(abandoned) != 1 {
				t.Fatalf("%d abandoned lines, want exactly 1 — the failure must still be visible, "+
					"just not paged. Output:\n%s", len(abandoned), out.String())
			}
			if a := abandoned[0]; a[slog.LevelKey] != slog.LevelWarn.String() || a[LogReason] != tc.reason {
				t.Errorf("abandoned line: level %v reason %v, want %s and %q", a[slog.LevelKey], a[LogReason],
					slog.LevelWarn, tc.reason)
			}
		})
	}
}

// t122Mark prefixes every PgError field a log line must NOT carry.
const t122Mark = "T122MARK"

// TestPolicyFallback_CarriesTheSQLStateAndNeverTheDetail — T122. A PostgreSQL error
// behind a fallback line leaves as its SQLSTATE and constraint name and NOTHING ELSE
// of the PgError, on every line that carries the pair, in both log formats.
//
// 🔴 DETAIL IS THE FIELD THAT CARRIES ROW VALUES ("Key (id)=(…) already exists",
// "Failing row contains (…)"), and errors.As hands the whole struct to whoever asks.
// So every string field of the PgError other than the four that may appear —
// Severity, Code and Message (they are err.Error()) and ConstraintName — is set to a
// recognisable marker, by reflection so a field pgx adds later is covered too, and the
// marker is searched for in the WHOLE output rather than in one attribute.
//
// POSITIVE CONTROL: the same PgError logged as a struct value DOES print the markers,
// so a clean scan below is the helper's doing and not a blind search.
func TestPolicyFallback_CarriesTheSQLStateAndNeverTheDetail(t *testing.T) {
	t.Parallel()

	pg, _ := probePgError()
	allowed := map[string]bool{"Severity": true, "Code": true, "Message": true, "ConstraintName": true}
	v := reflect.ValueOf(pg).Elem()
	var marked []string
	for i := 0; i < v.NumField(); i++ {
		f := v.Type().Field(i)
		if f.Type.Kind() != reflect.String || allowed[f.Name] {
			continue
		}
		v.Field(i).SetString(t122Mark + "-" + f.Name + "-" + uuid.NewString())
		marked = append(marked, f.Name)
	}
	if !strings.HasPrefix(pg.Detail, t122Mark) || !strings.HasPrefix(pg.Where, t122Mark) ||
		!strings.HasPrefix(pg.Hint, t122Mark) {
		t.Fatalf("Detail, Where and Hint were not all marked (marked: %v); pgx renamed a field and "+
			"this test would scan for nothing", marked)
	}
	wrapped := fmt.Errorf("ensure baseline policy %q: %w", "probe", pg)

	var ctl bytes.Buffer
	slog.New(slog.NewJSONHandler(&ctl, nil)).Info("probe", "pg", *pg)
	if !strings.Contains(ctl.String(), pg.Detail) {
		t.Fatalf("the positive control did not print the Detail marker: %s", ctl.String())
	}

	formats := []struct {
		name string
		make func(w io.Writer) slog.Handler
	}{
		{"json", func(w io.Writer) slog.Handler { return slog.NewJSONHandler(w, nil) }},
		{"text", func(w io.Writer) slog.Handler { return slog.NewTextHandler(w, nil) }},
	}
	decide := func(p policySets) { p.forTenant(context.Background(), uuid.New()) }
	paths := []struct {
		name   string
		script []error
		run    func(p policySets)
	}{
		{"forTenant: load", []error{wrapped}, decide},
		{"forTenant: materialise", []error{nil, wrapped}, decide},
		{"forTenant: re-load", []error{nil, nil, wrapped}, decide},
		{"stored: load", []error{wrapped}, func(p policySets) { p.stored(context.Background(), uuid.New()) }},
	}
	for _, f := range formats {
		for _, pc := range paths {
			var out bytes.Buffer
			pc.run(policySets{data: &scriptedData{script: pc.script}, params: policy.DefaultParams(),
				log: slog.New(f.make(&out))})
			got := out.String()
			if strings.Contains(got, t122Mark) {
				t.Errorf("%s / %s: the output carries a PgError field outside Code and ConstraintName "+
					"(marked: %v). Detail holds row values; it must never reach a log:\n%s",
					f.name, pc.name, marked, got)
			}
			// Both formats print a key next to its value; only the quoting differs.
			wantState := LogSQLState + "=" + pg.Code
			wantConstraint := LogConstraint + "=" + pg.ConstraintName
			if f.name == "json" {
				wantState = `"` + LogSQLState + `":"` + pg.Code + `"`
				wantConstraint = `"` + LogConstraint + `":"` + pg.ConstraintName + `"`
			}
			if !strings.Contains(got, wantState) || !strings.Contains(got, wantConstraint) {
				t.Errorf("%s / %s: the line does not carry %s and %s:\n%s",
					f.name, pc.name, wantState, wantConstraint, got)
			}
		}
	}
}
