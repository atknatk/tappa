package main

// operatorvies_test.go -- M10 OP-16 B: the adapter between the sign-up wizard's VIES client and
// the operator surface's VAT re-check (operator.go's operatorVIES and viesForOperator), and the
// re-check's place in the shutdown budget.

import (
	"bytes"
	"context"
	"log"
	"log/slog"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/domain/signup"
	"github.com/atknatk/tappa/internal/handler/operator"
)

// recordingCheck is a viesChecker that answers ans and records the numbers and contexts it was
// handed.
type recordingCheck struct {
	ans     signup.VATStatus
	numbers []string
	ctxs    []context.Context
}

func (r *recordingCheck) Check(ctx context.Context, n string) signup.VATStatus {
	r.numbers = append(r.numbers, n)
	r.ctxs = append(r.ctxs, ctx)
	return r.ans
}

type ctxKey struct{}

// TestOperatorVIES_OnlyAVerdictBecomesAVerdict: the adapter maps signup.VATValid to
// operator.VATAnswerValid, signup.VATInvalid to operator.VATAnswerInvalid, and every other value
// -- signup.VATUnknown and values signup does not name (99, -1) -- to operator.VATAnswerUnknown,
// the one the surface never writes. It hands the client the number and the context it was given,
// unchanged. It writes no line on the process's global log sinks (log/slog's default logger and
// the log package's, captured) -- the adapter's link of backlog T107; the number is in no form of
// the captured output (raw, lower case, quoted). A nil client is a nil operator.VATChecker, and
// configuredSurface then refuses to build the surface (operator.New's refusal). CONTROL: a line
// written through each captured sink is found.
func TestOperatorVIES_OnlyAVerdictBecomesAVerdict(t *testing.T) {
	var captured bytes.Buffer
	prevSlog, prevFlags, prevOut := slog.Default(), log.Flags(), log.Writer()
	slog.SetDefault(slog.New(slog.NewTextHandler(&captured, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.SetOutput(&captured)
	t.Cleanup(func() {
		slog.SetDefault(prevSlog)
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	const number = "IE9F23456K"
	ctx := context.WithValue(context.Background(), ctxKey{}, "the request's")
	for _, c := range []struct {
		in   signup.VATStatus
		want operator.VATAnswer
	}{
		{signup.VATValid, operator.VATAnswerValid},
		{signup.VATInvalid, operator.VATAnswerInvalid},
		{signup.VATUnknown, operator.VATAnswerUnknown},
		{signup.VATStatus(99), operator.VATAnswerUnknown},
		{signup.VATStatus(-1), operator.VATAnswerUnknown},
	} {
		rc := &recordingCheck{ans: c.in}
		got := viesForOperator(rc).CheckVAT(ctx, number)
		if got != c.want {
			t.Errorf("signup's %d became %d, want %d", c.in, got, c.want)
		}
		if len(rc.numbers) != 1 || rc.numbers[0] != number || rc.ctxs[0].Value(ctxKey{}) != "the request's" {
			t.Errorf("signup's %d: the client was handed %v, want the number once with the caller's context", c.in, rc.numbers)
		}
	}
	for _, form := range []string{number, strings.ToLower(number), strconv.Quote(number)} {
		if strings.Contains(captured.String(), form) {
			t.Errorf("the adapter's run left the number on a global log sink (%q)", form)
		}
	}
	if captured.Len() != 0 {
		t.Errorf("the adapter wrote to a global log sink: %q", captured.String())
	}
	if viesForOperator(nil) != nil {
		t.Error("a nil client became a VATChecker; operator.New would not refuse it")
	}
	cfg := &config.Config{OperatorTOTPKEK: randBytes(t, 32), OperatorTokenHMACKey: randBytes(t, 32), OperatorHost: "ops.taptime.mt",
		BaseURL: "https://taptime.mt"}
	if s, err := configuredSurface(noStore{}, noTexts{}, nil, cfg, slog.New(slog.DiscardHandler)); err == nil || s != nil {
		t.Error("a configured surface was built without its VIES client")
	}
	slog.Info("control", "n", number)
	log.Print("control " + number)
	if strings.Count(captured.String(), number) != 2 {
		t.Fatalf("CONTROL: a line through each captured sink is not found: %q", captured.String())
	}
}

// vatRecheckGateAllowance is what the HTTP drain must still hold, past the re-check's three
// bounds, for what runs before them on the router's deadline: the session gate's one
// statement and the read budget's charge. One second is several times the statement on a
// reachable database.
const vatRecheckGateAllowance = time.Second

// TestShutdownBudget_TheVATRecheckNestsInsideTheHTTPGrace binds the operator's VAT re-check
// (M10 OP-16 B) to the drain it runs inside -- the pattern of shutdownbudget_test.go's
// TestShutdownBudget_ tests (OP-14 E's among them).
//
// WHY NESTING, AND WHY THE SUM: the re-check runs IN its request, past the session gate on
// answered(r) -- detached from the client AND from the router's deadline
// (httpx.RequestTimeout), so http.Server.Shutdown waits for it and nothing else ends it. Its
// three bounds run in sequence, each on its own clock -- the read, the VIES call, the write --
// so the worst case is their sum, operator.VATRecheckWorstCase (19 s; its three parts are
// pinned as numbers by internal/handler/operator's TestVATRecheck_TheThreeBoundsAreTheirNumbers).
// If it stopped fitting in httpShutdownGrace, a re-check in flight at a deploy could be cut
// after VIES answered and before the verdict was written: an answer asked for, paid for in
// the session's VIES budget, and lost.
//
// What this does NOT bound: the gate's statement runs on the router's deadline, not on a
// bound of its own; vatRecheckGateAllowance is the room left for it, not a limit on it.
func TestShutdownBudget_TheVATRecheckNestsInsideTheHTTPGrace(t *testing.T) {
	worst := operator.VATRecheckWorstCase + vatRecheckGateAllowance
	if worst > httpShutdownGrace {
		t.Fatalf("a VAT re-check can run %v past its session gate (operator.VATRecheckWorstCase) and "+
			"%v more is held for the gate, %v in all, but Shutdown only waits httpShutdownGrace (%v) "+
			"for the request it runs inside; lower one of vat.go's three bounds or raise "+
			"httpShutdownGrace, and re-run the kill-budget test in shutdownbudget_test.go if you raise it",
			operator.VATRecheckWorstCase, vatRecheckGateAllowance, worst, httpShutdownGrace)
	}
	// POSITIVE CONTROL, as in shutdownbudget_test.go: a sum too short for two statements and one
	// VIES conversation (the checker's own bound is three seconds) would make the detach
	// decorative -- the bounds would end the re-check before the client could.
	if operator.VATRecheckWorstCase < 5*time.Second {
		t.Errorf("operator.VATRecheckWorstCase is %v, too short for a read, a VIES call and a write", operator.VATRecheckWorstCase)
	}
}
