package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/encode"
	"github.com/atknatk/tappa/internal/handler"
	"github.com/atknatk/tappa/internal/operatorauth"
)

// TestShutdownBudget_TheTwoGoWaitsFitInsideTheKubernetesGrace binds three numbers
// that live in three files and had nothing holding them together.
//
// 🔴 WHAT DRIFT COSTS, WHICH IS WHY THIS IS A TEST AND NOT A COMMENT (security audit
// F4, 2026-08-24). On SIGTERM the process drains HTTP for httpShutdownGrace and THEN
// — the encode store's Close is deferred, so it runs after Shutdown returns — waits up
// to encode.DefaultCloseGrace for a mid-step round before wiping every live session's
// plain plaque key. Kubernetes sends SIGKILL at terminationGracePeriodSeconds. If the
// two Go waits stop fitting inside it, sun.Zero runs under SIGKILL and ADR 0017 §6
// md. 7's wipe guarantee is gone — silently, because a killed process looks exactly
// like a clean one from outside.
//
// ⚠️ THE ORDER IS ASSERTED ELSEWHERE AND IS NOT RE-ASSERTED HERE: an audit measured
// that `defer encodeStore.Close()` is registered AFTER `defer data.Close()`, so LIFO
// runs the wipe while the pool is still open. This test is about the BUDGET.
//
// ⚠️ AND IT READS THE YAML RATHER THAN A COPY OF IT. A number transcribed into Go
// would be the second-representation defect this repository keeps paying for; the
// deployment manifest is the authority for what Kubernetes will do.
func TestShutdownBudget_TheTwoGoWaitsFitInsideTheKubernetesGrace(t *testing.T) {
	const manifest = "deploy/k8s/20-app.yaml"
	b, err := os.ReadFile(filepath.Join(repoRoot, manifest))
	if err != nil {
		t.Fatalf("reading %s: %v", manifest, err)
	}

	re := regexp.MustCompile(`(?m)^\s*terminationGracePeriodSeconds:\s*(\d+)`)
	m := re.FindSubmatch(b)
	if m == nil {
		t.Fatalf("%s no longer sets terminationGracePeriodSeconds. Without it Kubernetes uses "+
			"its own default and this budget is being kept against a number nobody wrote",
			manifest)
	}
	secs, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatalf("unparsable terminationGracePeriodSeconds %q", m[1])
	}
	kill := time.Duration(secs) * time.Second

	goWaits := httpShutdownGrace + encode.DefaultCloseGrace
	if goWaits >= kill {
		t.Fatalf("the two sequential shutdown waits are %v (httpShutdownGrace %v + "+
			"encode.DefaultCloseGrace %v) and SIGKILL arrives at %v.\n"+
			"sun.Zero would run under the kill, so no live session's plain plaque key is "+
			"wiped — and nothing about the shutdown would look wrong. Lower one of the two "+
			"Go waits, or raise terminationGracePeriodSeconds in %s and re-read ADR 0017 "+
			"§6 md. 7 while doing it.", goWaits, httpShutdownGrace, encode.DefaultCloseGrace,
			kill, manifest)
	}

	// A margin, not just an inequality: at exactly equal the wipe would begin as the
	// kill lands. Five seconds is encode.DefaultCloseGrace itself — enough for the
	// wipe to run once more if a round is mid-step when the grace expires.
	if slack := kill - goWaits; slack < encode.DefaultCloseGrace {
		t.Fatalf("only %v separates the shutdown waits (%v) from SIGKILL (%v); the wipe needs "+
			"room to finish rather than room to start", slack, goWaits, kill)
	}

	t.Logf("shutdown budget: %v HTTP + %v encode = %v, SIGKILL at %v (%v of slack)",
		httpShutdownGrace, encode.DefaultCloseGrace, goWaits, kill, kill-goWaits)
}

// TestShutdownBudget_TheDetachedRepairsNestInsideTheHTTPGrace binds the number that
// finishes an encode round after its request has gone away.
//
// 🔴 THE NUMBER WAS ARGUED BUT NOT DERIVED, AND NOTHING HELD THE ARGUMENT (ninth
// audit, 2026-08-24). It lived as an unexported constant in internal/encode/rows.go
// whose comment claimed it "nests inside httpShutdownGrace" — a relationship this
// package could not even see, because the identifier was not exported. Lowering
// httpShutdownGrace to 3s would have broken the nesting in complete silence.
//
// WHY NESTING RATHER THAN ADDING, which is the part worth getting right: the detached
// writes run INSIDE an in-flight request, and http.Server.Shutdown already waits up
// to httpShutdownGrace for in-flight requests to return. So they do not extend the
// shutdown; they must merely FIT in the wait that already exists. Adding them to
// TestShutdownBudget_TheTwoGoWaitsFitInsideTheKubernetesGrace's sum would be a wrong
// number in the other direction.
//
// TWICE the budget, because the two detached writes are sequential in the worst case:
// the marking spends its whole budget failing, and only then does the compensating
// plaque.unmarked entry start — on a full budget of its own, since WithoutCancel
// drops the parent deadline as well as its cancellation.
func TestShutdownBudget_TheDetachedRepairsNestInsideTheHTTPGrace(t *testing.T) {
	worst := 2 * encode.DefaultRepairGrace
	if worst > httpShutdownGrace {
		t.Fatalf("the two sequential detached writes can take %v (2 x encode.DefaultRepairGrace "+
			"%v) but Shutdown only waits httpShutdownGrace (%v) for the request they run "+
			"inside.\n"+
			"Past Progress.Done the chip is ALREADY personalised, so these writes are the "+
			"database catching up with a physical fact. A budget that outlives the drain "+
			"means the process can be killed mid-repair, which is the silent state "+
			"plaque.unmarked exists to prevent.\n"+
			"Either lower encode.DefaultRepairGrace or raise httpShutdownGrace — and if you "+
			"raise it, the other test in this file must still pass.",
			worst, encode.DefaultRepairGrace, httpShutdownGrace)
	}

	// POSITIVE CONTROL: the budget is not nailed so low that it is decorative. A
	// repair that cannot outlive one round-trip to Postgres would re-create the
	// failure detaching was introduced to remove.
	if encode.DefaultRepairGrace < time.Second {
		t.Errorf("encode.DefaultRepairGrace is %v, which is too short to complete one INSERT "+
			"against a real database; detaching from the request would then buy nothing",
			encode.DefaultRepairGrace)
	}
}

// TestShutdownBudget_TheRefusalRecordNestsInsideTheHTTPGrace binds the budget of the
// OTHER detached write in this process: recording a refused mount (M10 F0-6, security
// audit follow-up 2026-09-25). It runs inside the panel request that was refused, after
// that request may have been abandoned, so — like the encode repairs above — it must
// FIT in the drain Shutdown already waits for, not extend it. It is one write, never
// followed by a second, so the bound is the budget itself rather than twice it.
func TestShutdownBudget_TheRefusalRecordNestsInsideTheHTTPGrace(t *testing.T) {
	if tenant.RefusalRecordGrace > httpShutdownGrace {
		t.Fatalf("recording a refused mount can take %v (tenant.RefusalRecordGrace) but "+
			"Shutdown only waits httpShutdownGrace (%v) for the request it runs inside; a "+
			"refusal whose record is cut off by the drain is the silent state the detach "+
			"exists to prevent", tenant.RefusalRecordGrace, httpShutdownGrace)
	}
	// POSITIVE CONTROL, as above: a budget too short for one INSERT would make the
	// detach decorative.
	if tenant.RefusalRecordGrace < time.Second {
		t.Errorf("tenant.RefusalRecordGrace is %v, too short to complete one INSERT", tenant.RefusalRecordGrace)
	}
}

// TestShutdownBudget_TheResetDrainNestsInsideTheHTTPGrace binds the reset outbox's
// drain budget to the HTTP drain it runs BESIDE (M10 EM-5, ADR 0022 §6.6(a)).
//
// WHY NESTING RATHER THAN ADDING, as for the two tests above: the shutdown sequence
// starts the drain in its own goroutine before Shutdown and waits for both
// (shutdown, in main.go), so the drain costs max(httpShutdownGrace, ResetDrainGrace)
// — and stays out of TestShutdownBudget_TheTwoGoWaitsFitInsideTheKubernetesGrace's sum
// only while it nests. That it RUNS beside Shutdown is a behaviour, held by
// TestShutdown_DrainsTheResetOutboxAlongsideTheHTTPServer; this holds the numbers.
func TestShutdownBudget_TheResetDrainNestsInsideTheHTTPGrace(t *testing.T) {
	if handler.ResetDrainGrace > httpShutdownGrace {
		t.Fatalf("the reset outbox may take %v to drain (handler.ResetDrainGrace) but the HTTP drain it "+
			"runs beside only lasts httpShutdownGrace (%v); past that the drain is a third wait in "+
			"sequence, and the kill budget above does not count it", handler.ResetDrainGrace, httpShutdownGrace)
	}
	// The drain's two phases (send, then write what was not sent) must both exist: a
	// write reserve as long as the whole budget leaves no time to send anything queued.
	if handler.ResetDrainWriteReserve <= 0 || handler.ResetDrainWriteReserve >= handler.ResetDrainGrace {
		t.Fatalf("handler.ResetDrainWriteReserve is %v against a drain budget of %v: it must be positive "+
			"(the unsent rows need time inside the budget) and shorter than the budget (the queued "+
			"grants need time to be sent)", handler.ResetDrainWriteReserve, handler.ResetDrainGrace)
	}
	// POSITIVE CONTROL, as above: a drain too short for one relay round trip would turn
	// every grant still queued at a deploy into an undelivered row.
	if handler.ResetDrainGrace-handler.ResetDrainWriteReserve < time.Second {
		t.Errorf("the drain leaves %v for sending, too short for one relay conversation",
			handler.ResetDrainGrace-handler.ResetDrainWriteReserve)
	}
}

// passwordChangeAllowance is what the HTTP drain must still hold, AFTER the notice's
// two budgets, for the change the notice is about: on the Account path that is three
// cost-12 bcrypt runs and one transaction, before the notice begins. Five seconds is
// several times the three runs at the cost internal/adminauth pins.
const passwordChangeAllowance = 5 * time.Second

// TestShutdownBudget_ThePasswordNoticeNestsInsideTheHTTPGrace binds the "your password
// was changed" notice's budgets (M10 EM-9) to the drain they run inside.
//
// WHY NESTING RATHER THAN ADDING, as for the detached writes above: the notice is sent
// IN the request that changed the password (handler.AdminReset.passwordChanged — it
// does not use the reset outbox), so http.Server.Shutdown already waits for it. Its
// two budgets are SEQUENTIAL — the send (with the address read) and then the row,
// each on its own clock — so the worst case is their sum, and the change itself runs
// before both. If they stop fitting, a request in flight at a deploy is cut after the
// password changed and before its notice row, which is the silent state §4.6 forbids.
func TestShutdownBudget_ThePasswordNoticeNestsInsideTheHTTPGrace(t *testing.T) {
	worst := handler.PasswordNoticeSendGrace + handler.PasswordNoticeRecordGrace + passwordChangeAllowance
	if worst > httpShutdownGrace {
		t.Fatalf("a password change and its notice can take %v (handler.PasswordNoticeSendGrace %v + "+
			"handler.PasswordNoticeRecordGrace %v + %v for the change) but Shutdown only waits "+
			"httpShutdownGrace (%v) for the request they run inside; lower a notice budget or "+
			"raise httpShutdownGrace, and re-run the kill-budget test above if you raise it",
			worst, handler.PasswordNoticeSendGrace, handler.PasswordNoticeRecordGrace,
			passwordChangeAllowance, httpShutdownGrace)
	}
	// POSITIVE CONTROL, as above: budgets too short for one relay conversation or one
	// INSERT would make the notice decorative.
	if handler.PasswordNoticeSendGrace < 2*time.Second || handler.PasswordNoticeRecordGrace < time.Second {
		t.Errorf("the notice's budgets (%v to send, %v to record) are too short for one relay "+
			"conversation and one INSERT", handler.PasswordNoticeSendGrace, handler.PasswordNoticeRecordGrace)
	}
}

// TestShutdownBudget_TheFirstFactorRecordNestsInsideTheHTTPGrace binds the budget of the
// operator sign-in's detached write (M10 OP-14 phase C, 2nd round): the 'password_ok'
// row is written on a context detached from the request, so a client that hangs up
// during the comparison cannot spend the cap without a row. It runs inside the password
// step's request, so -- like the refusal record above -- it must FIT in the drain
// Shutdown already waits for, not extend it; and it is one write, so the bound is the
// budget itself.
func TestShutdownBudget_TheFirstFactorRecordNestsInsideTheHTTPGrace(t *testing.T) {
	if operatorauth.FirstFactorRecordGrace > httpShutdownGrace {
		t.Fatalf("the detached 'password_ok' write can take %v (operatorauth.FirstFactorRecordGrace) but "+
			"Shutdown only waits httpShutdownGrace (%v) for the request it runs inside; a trail the drain "+
			"cuts off is the silent state the detach exists to prevent",
			operatorauth.FirstFactorRecordGrace, httpShutdownGrace)
	}
	// POSITIVE CONTROL, as above: a budget too short for one INSERT would make the
	// detach decorative.
	if operatorauth.FirstFactorRecordGrace < time.Second {
		t.Errorf("operatorauth.FirstFactorRecordGrace is %v, too short to complete one INSERT", operatorauth.FirstFactorRecordGrace)
	}
}

// TestShutdownBudget_TheSignInStatementsNestInsideTheHTTPGrace binds the budget of the
// operator sign-in's OTHER detached statements (M10 OP-14 phase E): the password-less rows,
// the code step after its account budget is charged, and the enrollment's last statement
// run on contexts detached from the request, so a client that hangs up can neither spend a
// shared budget without a row nor tell a refusal from a success by the answer. They run
// inside the step's request, so -- like the first-factor record above -- they must FIT in
// the drain Shutdown already waits for.
//
// THREE TIMES THE BUDGET, because each statement has its own bound and the longest path
// runs three in sequence: the code step's lookup, op_open_session refused, and the refused
// code's row (operatorauth.SignInStatementGrace's comment holds the count; the enrollment
// runs two, the password step one).
func TestShutdownBudget_TheSignInStatementsNestInsideTheHTTPGrace(t *testing.T) {
	if worst := 3 * operatorauth.SignInStatementGrace; worst > httpShutdownGrace {
		t.Fatalf("the code step's three detached statements can take %v (3 x operatorauth.SignInStatementGrace %v) "+
			"but Shutdown only waits httpShutdownGrace (%v) for the request they run inside; a trail the drain "+
			"cuts off is the silent state the detach exists to prevent",
			worst, operatorauth.SignInStatementGrace, httpShutdownGrace)
	}
	// POSITIVE CONTROL, as above: a budget too short for one statement would make the
	// detach decorative.
	if operatorauth.SignInStatementGrace < time.Second {
		t.Errorf("operatorauth.SignInStatementGrace is %v, too short to complete one statement", operatorauth.SignInStatementGrace)
	}
}
