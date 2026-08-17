package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestAnnounceKEKRotationWindow — F3, and the pin an auditor's mutation showed was
// missing entirely.
//
// 🔴 WHAT IT DEFENDS. A KEK rotation is three steps and the THIRD one is the
// rotation: until TAPPA_TAG_KEK_PREVIOUS is unset and rolled out, the leaked key
// still opens every row it ever opened. An operator who stops after step 2 — or
// whose step 2 exits 3 — has bought nothing. Before this, an open window produced
// zero log lines, zero metrics and zero indicators, so nothing in the running
// system knew. Silencing the announcement left every test green.
func TestAnnounceKEKRotationWindow(t *testing.T) {
	capture := func(open bool) string {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // already done: the first announcement happens, then it returns
		announceKEKRotationWindow(ctx, open, log)
		return buf.String()
	}

	t.Run("an OPEN window announces itself", func(t *testing.T) {
		out := capture(true)
		if out == "" {
			t.Fatal("an open rotation window produced no log output at all")
		}
		if !strings.Contains(out, "WARN") {
			t.Errorf("an open window must be a WARNing, not an Info: %s", out)
		}
		for _, want := range []string{"TAPPA_TAG_KEK_PREVIOUS", "accepts_previous_kek=true", kekWindowOpen} {
			if !strings.Contains(out, want) {
				t.Errorf("the warning does not carry %q, so it cannot be searched for: %s", want, out)
			}
		}
	})

	t.Run("the CLOSED state announces itself POSITIVELY", func(t *testing.T) {
		// 🔴 THIS SUBTEST USED TO ASSERT THE OPPOSITE ("the steady state is
		// SILENT"), and that silence was the defect. The runbook's only gate
		// before step 3 — the step that lets an operator destroy the escrow copy
		// of a leaked KEK — was "grep finds no OPEN line". Nothing is also what
		// you get from a raised log level, a rotated log, the wrong pod, or not
		// having looked. "Closed" and "I cannot tell" were one reading.
		out := capture(false)
		if !strings.Contains(out, kekWindowClosed) {
			t.Fatalf("a closed window must say so POSITIVELY (%q), got: %s", kekWindowClosed, out)
		}
		if strings.Contains(out, kekWindowOpen) {
			t.Errorf("the closed line must not contain the open token, or the gate cannot tell them apart: %s", out)
		}
		if !strings.Contains(out, "accepts_previous_kek=false") {
			t.Errorf("the closed line should carry the machine-readable field too: %s", out)
		}
		// It is INFO, not WARN: a warning on every ordinary boot would be trained
		// away within a week, and the real one with it.
		if strings.Contains(out, "level=WARN") {
			t.Errorf("the ordinary steady state must not warn: %s", out)
		}
	})

	t.Run("each token MEANS its state — a swap must not pass", func(t *testing.T) {
		// 🔴 THE RENAME WAS PINNED; THE SWAP WAS NOT. Exchanging the two constants'
		// VALUES left every test green, and it inverts the gate: an OPEN window
		// would print the token the runbook reads as "closed", the step-3 table
		// would say "Pencere KAPANDI. Devam." and the operator would destroy the
		// escrow copy of a leaked KEK while the process still accepted it. That is
		// the exact outcome the table's third row exists to prevent.
		//
		// A count of occurrences cannot see a swap; only the MEANING can. The
		// tokens are anchored to their own words, and each state is asserted to
		// emit the token carrying its word.
		if !strings.Contains(kekWindowOpen, "open") || strings.Contains(kekWindowOpen, "closed") {
			t.Errorf("kekWindowOpen = %q; the OPEN token must say open and must not say closed", kekWindowOpen)
		}
		if !strings.Contains(kekWindowClosed, "closed") {
			t.Errorf("kekWindowClosed = %q; the CLOSED token must say closed", kekWindowClosed)
		}
		if out := capture(true); !strings.Contains(out, "open") || strings.Contains(out, "=closed") {
			t.Errorf("an OPEN window did not announce an open token: %s", out)
		}
		if out := capture(false); !strings.Contains(out, "=closed") || strings.Contains(out, "=open") {
			t.Errorf("a CLOSED window did not announce a closed token: %s", out)
		}
	})

	t.Run("the two tokens are mutually exclusive", func(t *testing.T) {
		// The gate counts both. If one were a substring of the other, or if a
		// state emitted both, the counts could not decide anything.
		if strings.Contains(kekWindowOpen, kekWindowClosed) || strings.Contains(kekWindowClosed, kekWindowOpen) {
			t.Fatal("one token contains the other; grep counts would be ambiguous")
		}
		openOut, closedOut := capture(true), capture(false)
		if strings.Contains(openOut, kekWindowClosed) || strings.Contains(closedOut, kekWindowOpen) {
			t.Error("a state emitted the other state's token")
		}
	})

	t.Run("it REPEATS rather than firing once", func(t *testing.T) {
		// The state is open-ended and gets more dangerous the longer it lasts. A
		// single line at start-up is invisible to the failure that matters: a pod
		// that booted three weeks ago on a step 3 nobody ever ran.
		old := kekRotationWarnEvery
		kekRotationWarnEvery = time.Millisecond
		defer func() { kekRotationWarnEvery = old }()

		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
		defer cancel()
		announceKEKRotationWindow(ctx, true, log)

		if n := strings.Count(buf.String(), "TAPPA_TAG_KEK_PREVIOUS"); n < 2 {
			t.Errorf("the warning fired %d time(s); an open window must keep announcing itself", n)
		}
	})

	t.Run("it never logs key material", func(t *testing.T) {
		// §4.7: the fact is loggable, the key is not. The STRUCTURAL half is that
		// this function is not given a key at all — it takes a bool — so there is
		// nothing for it to print. This assertion is the second belt, against a
		// future signature change.
		//
		// 🔴 IT ASSERTS ON THE SHAPE OF A KEY, NOT ON FIELD NAMES. The first
		// version listed forbidden substrings ("kek=", "previous_kek=") and failed
		// immediately — on `accepts_previous_kek=true`, a boolean. A scanner keyed
		// to names flags the honest field and would equally miss a key logged
		// under an innocent one. A 32-byte KEK is 44 base64 characters or 64 hex;
		// nothing legitimate in this line comes close.
		out := capture(true)
		for _, m := range regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`).FindAllString(out, -1) {
			t.Errorf("the warning contains a %d-character key-shaped run (%q...); §4.7 allows the FACT "+
				"of a rotation window, never the value", len(m), m[:8])
		}
		// CONTROL: the scanner catches what it claims to. Without this, a change
		// that emptied the log would make the loop above vacuously green.
		if !regexp.MustCompile(`[A-Za-z0-9+/]{40,}={0,2}`).MatchString(
			"x " + strings.Repeat("A", 43) + "= y") {
			t.Fatal("CONTROL FAILED: the key-shape scanner does not match a base64 32-byte key")
		}
	})
}

// TestKEKRotationWarnEvery_IsShortEnoughToBeANag pins the SHIPPED value.
//
// 🔴 THE REPEAT TEST BELOW SETS ITS OWN INTERVAL, so it proves the ticker uses the
// variable and proves NOTHING about the value that ships — exactly the pattern
// M8-01 measured (a probe timeout whose test substituted its own 50 ms and never
// drove the constant). An auditor changed this to thirty DAYS and the suite stayed
// green.
//
// The message states the CONSEQUENCE rather than the number: the open window is
// meant to keep nagging, so an operator who scrolls the log during a shift sees it
// again. The runbook's gate deliberately does NOT depend on this value any more —
// it reads the start-up line — so this bound is about human attention, not
// correctness, and it is asserted as a range rather than an exact number.
func TestKEKRotationWarnEvery_IsShortEnoughToBeANag(t *testing.T) {
	if kekRotationWarnEvery <= 0 {
		t.Fatalf("a non-positive interval (%v) makes time.NewTicker panic at start-up", kekRotationWarnEvery)
	}
	if kekRotationWarnEvery > time.Hour {
		t.Errorf("the open-window warning repeats every %v; an operator reading the log during a shift "+
			"would see it once and assume it was historical. An open window means a leaked KEK is still "+
			"accepted, so it has to keep asking.", kekRotationWarnEvery)
	}
}

// TestAnnounceKEKRotationWindow_IsActuallyWiredIntoTheCommand.
//
// 🔴 A TESTED FUNCTION NOBODY CALLS IS NOT A FEATURE. Every assertion above drives
// announceKEKRotationWindow directly, so deleting its call site in run() left the
// whole suite GREEN while the product emitted nothing at all — and the runbook's
// gate, which reads that output, would then permanently read "0 open / 0 closed"
// and block every rotation. Fail-safe, but broken and silently so.
//
// This is asserted on main.go's SOURCE, the same shape
// TestPackaging_TheCommandOwnsTheTimezoneEmbedItself uses for the tzdata import
// and for the same reason: the dependency is a call, not a type, so the compiler
// cannot be made to carry it.
func TestAnnounceKEKRotationWindow_IsActuallyWiredIntoTheCommand(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(repoRoot, "cmd", "tappa", "main.go"))
	if err != nil {
		t.Fatalf("reading main.go: %v", err)
	}
	text := string(src)

	call := regexp.MustCompile(`go\s+announceKEKRotationWindow\(`)
	if !call.MatchString(text) {
		t.Fatal("main.go never starts announceKEKRotationWindow; an open KEK rotation window would " +
			"produce no log line, and the runbook's gate before step 3 reads exactly that line")
	}
	// And it must be told the REAL state. Passing a literal would make the
	// announcement permanently right or permanently wrong.
	if !regexp.MustCompile(`announceKEKRotationWindow\(ctx,\s*len\(cfg\.TagKEKPrevious\)\s*>\s*0`).MatchString(text) {
		t.Error("announceKEKRotationWindow is not driven by cfg.TagKEKPrevious; the line it prints would " +
			"not track whether the process actually accepts the previous KEK")
	}
	// CONTROL: the matcher is capable of failing. Without this, a broken regex
	// would make both assertions above vacuous.
	if call.MatchString("go somethingElse(") {
		t.Fatal("CONTROL FAILED: the call matcher matches an unrelated call")
	}
}

// TestAnnounceKEKRotationWindow_SurvivesTheSHIPPEDLogLevel.
//
// 🔴 EVERY OTHER TEST IN THIS FILE BUILDS ITS HANDLER AT slog.LevelDebug, so none
// of them drives the level this deployment actually runs at. An auditor demoted
// the closed line from Info to Debug and the whole suite stayed GREEN — the same
// shape M8-01 measured when a probe-timeout test substituted its own 50 ms and
// never exercised the shipped constant.
//
// The consequence is not cosmetic. deploy/k8s/05-config.yaml ships
// TAPPA_LOG_LEVEL: "info". At Debug, the closed line never reaches the log, so the
// runbook's gate before step 3 reads "0 open / 0 closed" FOREVER and every
// rotation stops at the last step. Safe direction, unusable procedure.
//
// The level is taken FROM the shipped ConfigMap and pushed through the product's
// own parseLevel, so this cannot drift from what runs.
func TestAnnounceKEKRotationWindow_SurvivesTheSHIPPEDLogLevel(t *testing.T) {
	cfgSrc, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "k8s", "05-config.yaml"))
	if err != nil {
		t.Fatalf("reading 05-config.yaml: %v", err)
	}
	m := regexp.MustCompile(`(?m)^\s+TAPPA_LOG_LEVEL:\s*"?([a-zA-Z]+)"?`).FindStringSubmatch(string(cfgSrc))
	if m == nil {
		t.Fatal("05-config.yaml does not set TAPPA_LOG_LEVEL; this test cannot know what ships")
	}
	shipped := parseLevel(m[1])
	t.Logf("shipped TAPPA_LOG_LEVEL=%q -> %v", m[1], shipped)

	capture := func(open bool) string {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: shipped}))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		announceKEKRotationWindow(ctx, open, log)
		return buf.String()
	}

	// BOTH tokens must survive the shipped level, because the gate reads both and
	// treats "neither" as UNKNOWN and stops.
	if out := capture(false); !strings.Contains(out, kekWindowClosed) {
		t.Errorf("at the SHIPPED log level (%v) the CLOSED token never reaches the log, so the runbook's "+
			"gate would read 0/0 for ever and every rotation would stop at step 3: %q", shipped, out)
	}
	if out := capture(true); !strings.Contains(out, kekWindowOpen) {
		t.Errorf("at the SHIPPED log level (%v) the OPEN token never reaches the log, so an open rotation "+
			"window would be invisible: %q", shipped, out)
	}

	// CONTROL: the level really is a filter, so the assertions above are not
	// passing merely because everything is always emitted.
	var buf bytes.Buffer
	slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: shipped})).
		Debug("a debug line that must NOT survive an info-level handler")
	if shipped <= slog.LevelDebug {
		t.Skip("the shipped level is Debug or lower; this control cannot distinguish anything")
	}
	if buf.Len() != 0 {
		t.Fatal("CONTROL FAILED: a Debug line survived the shipped level, so this test proves nothing")
	}
}

// TestRunbookGateGrepsMatchTheShippedTokens.
//
// 🔴 THE TOKENS HAVE A SECOND REPRESENTATION AND NOTHING KEPT THEM IN SYNC. The
// strings live in main.go; their only consumers are hard-coded greps in
// deploy/README.md. Every Go assertion referred to them SYMBOLICALLY
// (kekWindowOpen), so renaming the VALUES left the suite green while the runbook's
// gate would read 0/0 for ever — the gate silently stops working and the tests
// applaud.
//
// This binds the two representations: the README must grep for the exact strings
// the product emits.
func TestRunbookGateGrepsMatchTheShippedTokens(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join(repoRoot, "deploy", "README.md"))
	if err != nil {
		t.Fatalf("reading deploy/README.md: %v", err)
	}
	text := string(readme)
	for _, tok := range []string{kekWindowOpen, kekWindowClosed} {
		n := strings.Count(text, tok)
		if n == 0 {
			t.Errorf("deploy/README.md never greps for %q, so the gate it documents cannot see the line "+
				"this process emits", tok)
			continue
		}
		t.Logf("README references %q %d times", tok, n)
	}
	// The gate is used at BOTH decision points (step 1 and step 3), so a single
	// mention would mean one of them lost it.
	if strings.Count(text, kekWindowOpen) < 2 || strings.Count(text, kekWindowClosed) < 2 {
		t.Error("each token should appear in both gate tables (step 1 and step 3); fewer means a gate " +
			"was dropped or rewritten to read something else")
	}
}
