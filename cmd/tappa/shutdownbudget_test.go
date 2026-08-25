package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/encode"
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
