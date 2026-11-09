package handler

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/web/templates/pages"
)

// ADR 0037's T148-0: the Rules screen stops selling sys:no-self-review, and the Managers form says
// the same thing to the owner about to create the first manager. The words are pinned in
// internal/domain/tenant (TestGuardrailPlanned_TheSelfReviewPlateSaysItIsNotEnforced); this file pins
// what the screens draw around them.

const (
	plannedSid      = "sys:no-self-review"
	plannedIntro    = "The last one below is still planned: it is not enforced yet, so it is not yet one of these promises."
	inviteWarning   = "A manager can currently decide their own flagged record and type their own hours."
	lockLabel       = "Cannot be switched off"
	engineSentence  = "Engine’s reason:"
	effectPillClass = "effect-pill--locked"
)

// plannedPlates renders the guarantees through fillPoliciesView, so a handler that drops the stage
// goes red too, and returns each rung's markup by sid.
func plannedPlates(t *testing.T) (intro string, plates map[string]string) {
	t.Helper()
	screen := plannedScreen(t)
	body := policyGuaranteeBytes(t, screen, false)
	plates = map[string]string{}
	for _, g := range screen.Guardrails {
		number := fmt.Sprintf(`<span class="rung-no">%02d</span>`, g.Order)
		at := strings.Index(body, number)
		if at < 0 {
			t.Fatalf("%s has no rung", g.Sid)
		}
		plate := body[at+len(number):]
		if next := strings.Index(plate, `class="rung-no"`); next >= 0 {
			plate = plate[:next]
		}
		plates[g.Sid] = plate
	}
	first := fmt.Sprintf(`<span class="rung-no">%02d</span>`, screen.Guardrails[0].Order)
	return body[:strings.Index(body, first)], plates
}

// plannedScreen is the fixture with the one guarantee that is planned today.
func plannedScreen(t *testing.T) tenant.PolicyScreen {
	t.Helper()
	screen := policyScreenFixture()
	seen := false
	for i := range screen.Guardrails {
		if screen.Guardrails[i].Sid == plannedSid {
			screen.Guardrails[i].Stage = tenant.GuardrailPlanned
			screen.Guardrails[i].When = "Planned: a manager decides their own record"
			screen.Guardrails[i].Rule = "Not enforced yet."
			seen = true
		}
	}
	if !seen {
		t.Fatalf("the fixture has no %s", plannedSid)
	}
	return screen
}

// TestRulesScreen_TheSummaryDoesNotCountAPlannedGuaranteeAsAGuarantee: the line above the page is
// what a reader takes first, and "11 guarantees" contradicted "not yet one of these promises". The
// engine still ships all of them; the control is the page with none planned.
func TestRulesScreen_TheSummaryDoesNotCountAPlannedGuaranteeAsAGuarantee(t *testing.T) {
	const line = `<p class="font-mono text-xs uppercase tracking-widest text-ink/70">`
	screen := plannedScreen(t)
	total := len(screen.Guardrails)
	if total < 2 {
		t.Fatalf("%d guarantee(s) in the fixture", total)
	}
	want := fmt.Sprintf("%s%d guardrails · 1 planned · ", line, total-1)
	if body := policyStatePage(t, policyPageState{screen: screen}); !strings.Contains(body, want) {
		t.Errorf("the page does not open with %q", want)
	}
	control := fmt.Sprintf("%s%d guardrails · %d baseline", line, total, len(policyScreenFixture().Baseline))
	if body := policyStatePage(t, policyPageState{screen: policyScreenFixture()}); !strings.Contains(body, control) {
		t.Errorf("with nothing planned the page does not open with %q", control)
	}
}

func TestRulesScreen_ThePlannedRungDrawsNoPromiseAndTheOthersKeepTheirs(t *testing.T) {
	intro, plates := plannedPlates(t)
	if flat := strings.Join(strings.Fields(intro), " "); strings.Count(flat, plannedIntro) != 1 {
		t.Errorf("the introduction does not say the last one is planned exactly once: %q", flat)
	}
	promises := []string{lockLabel, engineSentence, effectPillClass}
	planned := plates[plannedSid]
	for _, want := range []string{plannedSid, "Planned: a manager decides their own record", "Not enforced yet."} {
		if !strings.Contains(planned, want) {
			t.Errorf("the planned rung does not carry %q", want)
		}
	}
	for _, refused := range promises {
		if strings.Contains(planned, refused) {
			t.Errorf("the planned rung still draws %q", refused)
		}
	}
	if len(plates) < 2 {
		t.Fatalf("read %d rung(s); the other rungs are the control", len(plates))
	}
	for sid, plate := range plates {
		if sid == plannedSid {
			continue
		}
		for _, want := range promises {
			if !strings.Contains(plate, want) {
				t.Errorf("%s is enforced and its rung lost %q", sid, want)
			}
		}
	}
}

func TestManagersForm_TheWarningIsInTheFormAndNowhereElse(t *testing.T) {
	render := func(canSend bool) string {
		t.Helper()
		var buf bytes.Buffer
		v := pages.ManagersView{CanSend: canSend, InviteHref: managersInviteHref, CancelHref: managersCancelHref, NameMaxLength: 100}
		if err := pages.AdminManagers(v).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render: %v", err)
		}
		return buf.String()
	}
	with := render(true)
	if n := strings.Count(with, inviteWarning); n != 1 {
		t.Fatalf("the warning appears %d times with the form, want 1", n)
	}
	open := strings.Index(with, managersInviteHref)
	at := strings.Index(with, inviteWarning)
	end := strings.Index(with[open:], "</form>")
	if open < 0 || end < 0 || at < open || at > open+end {
		t.Errorf("the warning at %d is outside the invite form %d..%d", at, open, open+end)
	}
	if !strings.Contains(with[max(0, at-120):at], "md:basis-full") {
		t.Error("the warning does not take the form's whole row from the tablet up")
	}
	if strings.Contains(render(false), inviteWarning) {
		t.Error("no form is drawn on a server that sends no invitations, and the warning belongs to the form")
	}

	templates, err := filepath.Glob(filepath.Join("..", "..", "web", "templates", "*", "*.templ"))
	if err != nil || len(templates) < 10 {
		t.Fatalf("read %d template(s): %v", len(templates), err)
	}
	for _, path := range templates {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		n := strings.Count(string(src), inviteWarning)
		if want := filepath.Base(path) == "admininvites.templ"; want != (n == 1) || n > 1 {
			t.Errorf("%s carries the warning %d time(s)", path, n)
		}
	}
}
