package checkin

import (
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/domain/tap"
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
