package main

// operatorsuspend_test.go -- M10 OP-15 C: the operator's suspension writes' place in the shutdown
// budget.

import (
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/handler/operator"
)

// suspensionGateAllowance is what the HTTP drain must still hold, past the write's bound, for
// what runs before it on the router's deadline: the session gate's one statement, the form read
// from a body of at most 16 KiB, and the request budget's charge. One second is several times
// that on a reachable database -- vatRecheckGateAllowance's reasoning and number.
const suspensionGateAllowance = time.Second

// TestShutdownBudget_TheSuspensionWriteNestsInsideTheHTTPGrace binds the operator's two
// suspension writes (M10 OP-15 C; ADR 0025 §5) to the drain they run inside -- the pattern of
// shutdownbudget_test.go's TestShutdownBudget_ tests and of the VAT re-check's
// (TestShutdownBudget_TheVATRecheckNestsInsideTheHTTPGrace).
//
// WHY NESTING: either write runs IN its request, past the session gate on answered(r) -- detached
// from the client AND from the router's deadline (httpx.RequestTimeout) -- so http.Server.Shutdown
// waits for it and only its own bound ends it: operator.SuspensionWriteWorstCase, one statement
// (internal/handler/operator's TestSuspension_TheWriteBoundIsItsNumber pins it at 10 s). If it
// stopped fitting in httpShutdownGrace, a suspension pressed at a deploy could be cut after the
// statement was sent and before the operator was answered -- the "not confirmed" the overview
// would have to resolve for every write in flight.
//
// What this does NOT bound: the gate's statement runs on the router's deadline, not on a bound
// of its own; suspensionGateAllowance is the room left for it, not a limit on it.
func TestShutdownBudget_TheSuspensionWriteNestsInsideTheHTTPGrace(t *testing.T) {
	worst := operator.SuspensionWriteWorstCase + suspensionGateAllowance
	if worst > httpShutdownGrace {
		t.Fatalf("a suspension write can run %v past its session gate (operator.SuspensionWriteWorstCase) and "+
			"%v more is held for the gate, %v in all, but Shutdown only waits httpShutdownGrace (%v) "+
			"for the request it runs inside; lower suspension.go's bound or raise httpShutdownGrace, and "+
			"re-run the kill-budget test in shutdownbudget_test.go if you raise it",
			operator.SuspensionWriteWorstCase, suspensionGateAllowance, worst, httpShutdownGrace)
	}
	// POSITIVE CONTROL, as in shutdownbudget_test.go: a bound too short for one statement that may
	// wait for the tenant's row lock would make the detach decorative.
	if operator.SuspensionWriteWorstCase < time.Second {
		t.Errorf("operator.SuspensionWriteWorstCase is %v, too short to complete one statement", operator.SuspensionWriteWorstCase)
	}
}
