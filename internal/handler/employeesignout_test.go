package handler

// employeesignout_test.go -- T59's "Sign out their phones" through the panel's router, against
// fakes: the gate, where the card offers the act, who may press it, every refusal's sentence, the
// verified banner, the log, and the golden of every new screen. Real Postgres is
// employeesignout_db_test.go's.

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// signedInStaff is an active person with n live sessions, measured.
func signedInStaff(n int) *fakeStaff {
	first := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	s := tenant.PersonSummary{State: tenant.Measured, Status: ledger.StatusActive, LiveDevices: n}
	if n > 0 {
		s.FirstSignedIn = &first
	}
	return &fakeStaff{summary: s}
}

// signOutConfirmed walks the warning and returns the form a sign-out must carry.
func signOutConfirmed(t *testing.T, b *browser, employeeID uuid.UUID) url.Values {
	t.Helper()
	page := htmlOf(t, b.do(http.MethodGet, managedRosterHref(employeeID)+"&confirm=signout", nil))
	return url.Values{"id": {employeeID.String()}, confirmField: {confirmTokenIn(t, page)}}
}

func TestEmployeeSignOut_TheConfirmationStepIsENFORCED(t *testing.T) {
	employeeID := uuid.New()

	t.Run("an unconfirmed POST writes nothing and says why", func(t *testing.T) {
		staff := signedInStaff(2)
		b := panelBrowserWithActions(t, staff, &fakeInviter{})
		rec := b.do(http.MethodPost, employeeSignOutHref, url.Values{"id": {employeeID.String()}})
		loc := rec.Header().Get("Location")
		if rec.Code != http.StatusSeeOther || !strings.Contains(loc, "problem=signout-confirm-required") {
			t.Fatalf("answered %d %q, want 303 problem=signout-confirm-required", rec.Code, loc)
		}
		if n := len(staff.signOutCommands()); n != 0 {
			t.Errorf("an unconfirmed POST reached the domain %d time(s)", n)
		}
		body := htmlOf(t, b.do(http.MethodGet, loc, nil))
		if !strings.Contains(body, "Signing somebody&#39;s phones out needs the warning screen first") &&
			!strings.Contains(body, "Signing somebody's phones out needs the warning screen first") {
			t.Error("the page does not explain why nothing happened")
		}
	})

	t.Run("the confirmed flow works, with the session's tenant and actor", func(t *testing.T) {
		staff := signedInStaff(2)
		b := panelBrowserWithActions(t, staff, &fakeInviter{})
		form := signOutConfirmed(t, b, employeeID)
		// A body claiming another tenant and actor is one line of curl; neither is read.
		form.Set("tenant_id", uuid.NewString())
		form.Set("actor_id", uuid.NewString())
		rec := b.do(http.MethodPost, employeeSignOutHref, form)
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "done=signed-out") {
			t.Fatalf("a confirmed sign-out redirected to %q, want done=signed-out", loc)
		}
		cmds := staff.signOutCommands()
		if len(cmds) != 1 {
			t.Fatalf("the domain saw %d sign-out(s), want 1", len(cmds))
		}
		if c := cmds[0]; c.TenantID != panelTestTenant || c.ActorID != panelTestAdmin || c.EmployeeID != employeeID {
			t.Errorf("the command is %+v, want the session's tenant %s and admin %s for %s",
				c, panelTestTenant, panelTestAdmin, employeeID)
		}
		// The browser honoured the cleared cookie, so the same value is refused.
		again := b.do(http.MethodPost, employeeSignOutHref, form)
		if loc := again.Header().Get("Location"); !strings.Contains(loc, "problem=signout-confirm-required") {
			t.Errorf("a replay by an ordinary browser redirected to %q", loc)
		}
		if n := len(staff.signOutCommands()); n != 1 {
			t.Errorf("a replay reached the domain (%d sign-outs)", n)
		}
	})

	t.Run("a deactivation's confirmation does not open the sign-out", func(t *testing.T) {
		staff := signedInStaff(2)
		b := panelBrowserWithActions(t, staff, &fakeInviter{})
		form := deactivateConfirmed(t, b, employeeID, nil)
		rec := b.do(http.MethodPost, employeeSignOutHref, form)
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=signout-confirm-required") {
			t.Errorf("a deactivation's value at the sign-out redirected to %q", loc)
		}
		if n := len(staff.signOutCommands()); n != 0 {
			t.Errorf("a value minted for another act signed somebody out (%d)", n)
		}
	})

	t.Run("a sign-out confirmed for somebody else is refused", func(t *testing.T) {
		staff := signedInStaff(2)
		b := panelBrowserWithActions(t, staff, &fakeInviter{})
		form := signOutConfirmed(t, b, uuid.New())
		form.Set("id", employeeID.String())
		rec := b.do(http.MethodPost, employeeSignOutHref, form)
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=confirm-stale") {
			t.Errorf("redirected to %q, want problem=confirm-stale", loc)
		}
		if n := len(staff.signOutCommands()); n != 0 {
			t.Errorf("a value bound to another person signed somebody out (%d)", n)
		}
	})

	t.Run("a forged value is refused", func(t *testing.T) {
		staff := signedInStaff(2)
		b := panelBrowserWithActions(t, staff, &fakeInviter{})
		signOutConfirmed(t, b, employeeID)
		rec := b.do(http.MethodPost, employeeSignOutHref, url.Values{
			"id": {employeeID.String()}, confirmField: {"not-the-value"},
		})
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "problem=signout-confirm-required") {
			t.Errorf("a forged value redirected to %q", loc)
		}
		if n := len(staff.signOutCommands()); n != 0 {
			t.Errorf("a forged value signed somebody out (%d)", n)
		}
	})
}

// The card offers the act only where it can do something, and mints the warning only then.
func TestEmployeeSignOut_TheCardOffersItOnlyWhereItCanDoSomething(t *testing.T) {
	first := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		staff *fakeStaff
		offer bool
	}{
		{"active with two phones signed in", signedInStaff(2), true},
		{"active with one", signedInStaff(1), true},
		{"active with none", signedInStaff(0), false},
		{"deactivated, phones still signed in on purpose", &fakeStaff{
			person: &tenant.Person{Name: "Dragan Nikolic", Status: ledger.StatusDeactivated, LocationID: &panelTestLocation},
			summary: tenant.PersonSummary{State: tenant.Measured, Status: ledger.StatusDeactivated,
				LiveDevices: 2, FirstSignedIn: &first},
		}, false},
		{"the summary could not be read", &fakeStaff{summary: tenant.PersonSummary{State: tenant.ReadFailed}}, false},
		{"the summary was never read", &fakeStaff{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := panelBrowserWithActions(t, tc.staff, &fakeInviter{})
			id := uuid.New()
			card := cardOf(t, htmlOf(t, b.do(http.MethodGet, managedRosterHref(id), nil)))
			if got := strings.Contains(card, "confirm=signout"); got != tc.offer {
				t.Errorf("the card offers the sign-out: %v, want %v\n%s", got, tc.offer, card)
			}
			delete(b.cookies, adminConfirmCookieName)
			page := htmlOf(t, b.do(http.MethodGet, managedRosterHref(id)+"&confirm=signout", nil))
			_, minted := b.cookies[adminConfirmCookieName]
			if minted != tc.offer {
				t.Errorf("the warning minted a confirmation: %v, want %v", minted, tc.offer)
			}
			if got := strings.Contains(page, `action="`+employeeSignOutHref+`"`); got != tc.offer {
				t.Errorf("the warning renders the sign-out form: %v, want %v", got, tc.offer)
			}
		})
	}
}

// The permission is deactivation's: any admin of the business, so a MANAGER may press it too.
func TestEmployeeSignOut_AManagerMayDoItAsTheyMayDeactivate(t *testing.T) {
	staff := signedInStaff(1)
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "manager", FullName: "KF Manager",
		}, nil
	}}
	b := newBrowser(t, newAdminRouterWithActions(t, admins, &fakeTrail{}, newFakeLedger(), &fakeReviewer{}, staff, &fakeInviter{}))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	id := uuid.New()
	rec := b.do(http.MethodPost, employeeSignOutHref, signOutConfirmed(t, b, id))
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "done=signed-out") {
		t.Fatalf("a manager's confirmed sign-out redirected to %q", loc)
	}
	// CONTROL: the same manager may deactivate -- the parity this test claims.
	rec = b.do(http.MethodPost, employeeDeactivateHref, deactivateConfirmed(t, b, id, nil))
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "done=deactivated") {
		t.Fatalf("the parity control failed: a manager's deactivation redirected to %q", loc)
	}
}

// A suspended business -- every reason, security_hold included -- can still sign a stolen phone out
// (ADR 0025 §3, user decision 2026-11-09): the act narrows who can tap and records nothing. A
// manager's press is the case: the gate runs before any role check, so it is the stricter one.
func TestEmployeeSignOut_ASuspendedBusinessesManagerCanStillSignPhonesOut(t *testing.T) {
	for _, code := range db.SuspensionReasons() {
		t.Run(string(code), func(t *testing.T) {
			staff := signedInStaff(1)
			trail := &fakeTrail{}
			admins := suspendedIdentityAs("manager", panelTestTenant, panelTestAdmin, panelTestSession, codePtr(code))
			b := newBrowser(t, newAdminRouterWithActions(t, admins, trail, newFakeLedger(), &fakeReviewer{}, staff, &fakeInviter{}))
			b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
			rec := b.do(http.MethodPost, employeeSignOutHref, signOutConfirmed(t, b, uuid.New()))
			if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.Contains(loc, "done=signed-out") {
				t.Fatalf("a suspended (%s) business's sign-out answered %d %q, want 303 done=signed-out", code, rec.Code, loc)
			}
			if n := len(staff.signOutCommands()); n != 1 {
				t.Errorf("the domain saw %d sign-out(s), want 1", n)
			}
			if n := len(refusalEvents(trail)); n != 0 {
				t.Errorf("%d suspension refusal row(s) for a listed exception", n)
			}
			// CONTROL: the same suspended identity is refused a gated write -- deactivation.
			rec = b.do(http.MethodPost, employeeDeactivateHref, deactivateConfirmed(t, b, uuid.New(), nil))
			if rec.Code != http.StatusForbidden {
				t.Errorf("the control: a suspended business's deactivation answered %d, want 403", rec.Code)
			}
		})
	}
}

func TestEmployeeSignOut_EveryRefusalIsASentence(t *testing.T) {
	for _, tc := range []struct {
		err  error
		word string
		says string
	}{
		{tenant.ErrNoLiveSessions, "no-phones", "No phone was signed in as them, so nothing was signed out and nothing was recorded."},
		{tenant.ErrSignOutDeactivated, "signout-deactivated", "every tap they make is refused and still recorded."},
		{tenant.ErrUnknownEmployee, "unknown", "That person is not on this business"},
	} {
		t.Run(tc.word, func(t *testing.T) {
			staff := signedInStaff(1)
			staff.signOutErr = tc.err
			b := panelBrowserWithActions(t, staff, &fakeInviter{})
			rec := b.do(http.MethodPost, employeeSignOutHref, signOutConfirmed(t, b, uuid.New()))
			loc := rec.Header().Get("Location")
			if !strings.Contains(loc, "problem="+tc.word) {
				t.Fatalf("%v redirected to %q, want problem=%s", tc.err, loc, tc.word)
			}
			body := strings.Join(strings.Fields(htmlOf(t, b.do(http.MethodGet, loc, nil))), " ")
			if !strings.Contains(strings.ReplaceAll(body, "&#39;", "'"), tc.says) {
				t.Errorf("the page after %s does not say %q", tc.word, tc.says)
			}
		})
	}
	t.Run("a database failure is not a refusal", func(t *testing.T) {
		staff := signedInStaff(1)
		staff.signOutErr = errors.New("connection reset")
		b := panelBrowserWithActions(t, staff, &fakeInviter{})
		rec := b.do(http.MethodPost, employeeSignOutHref, signOutConfirmed(t, b, uuid.New()))
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("a failed write answered %d %q, want 500", rec.Code, rec.Header().Get("Location"))
		}
	})
}

// The banner is a state the same request read: only beside a measured zero.
func TestEmployeeSignOut_TheBannerNeedsAMeasuredZero(t *testing.T) {
	for _, tc := range []struct {
		name  string
		staff *fakeStaff
		shown bool
	}{
		{"nobody signed in now", signedInStaff(0), true},
		{"a phone signed in again", signedInStaff(1), false},
		{"the summary failed", &fakeStaff{summary: tenant.PersonSummary{State: tenant.ReadFailed}}, false},
		{"deactivated since", &fakeStaff{
			person:  &tenant.Person{Name: "Dragan Nikolic", Status: ledger.StatusDeactivated, LocationID: &panelTestLocation},
			summary: tenant.PersonSummary{State: tenant.Measured, Status: ledger.StatusDeactivated},
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := panelBrowserWithActions(t, tc.staff, &fakeInviter{})
			body := htmlOf(t, b.do(http.MethodGet, managedRosterHref(uuid.New())+"&done=signed-out", nil))
			if got := strings.Contains(body, "No phone is signed in as"); got != tc.shown {
				t.Errorf("the banner is shown: %v, want %v", got, tc.shown)
			}
		})
	}
}

// §7: the confirmation value and the panel cookie never reach the log.
func TestEmployeeSignOut_LogsNoValueThatCouldBeSpent(t *testing.T) {
	b, logs := panelBrowserWithLogsAndLedger(t, newFakeLedger(), signedInStaff(2), &fakeInviter{})
	form := signOutConfirmed(t, b, uuid.New())
	b.do(http.MethodPost, employeeSignOutHref, form)
	b.do(http.MethodPost, employeeSignOutHref, form) // the refused replay logs too
	out := logs.String()
	if !strings.Contains(out, "sessions_revoked") {
		t.Fatalf("the log has no sign-out line; this test would be vacuous:\n%s", out)
	}
	for _, never := range []string{form.Get(confirmField), b.cookies[adminauth.CookieName]} {
		if never != "" && strings.Contains(out, never) {
			t.Errorf("the log carries a value that could be spent:\n%s", out)
		}
	}
}

// --- golden ------------------------------------------------------------------------------------

const signOutGoldenDir = "testdata/signout-golden"

// noticeOf isolates the page's first notice (a PanelNotice: a div with a role).
func noticeOf(t *testing.T, body string) string {
	t.Helper()
	start := regexp.MustCompile(`<div role="(status|alert)"`).FindStringIndex(body)
	if start == nil {
		t.Fatal("the page carries no notice")
	}
	depth := 0
	for i := start[0]; i < len(body); {
		open := strings.Index(body[i:], "<div")
		end := strings.Index(body[i:], "</div>")
		if end < 0 {
			break
		}
		if open >= 0 && open < end {
			depth++
			i += open + len("<div")
			continue
		}
		depth--
		i += end + len("</div>")
		if depth == 0 {
			return body[start[0]:i]
		}
	}
	t.Fatal("the notice is not closed")
	return ""
}

var confirmValueRE = regexp.MustCompile(`(name="` + confirmField + `" value=")[^"]*(")`)

// TestEmployeeSignOut_TheScreensAreTheGolden pins the new screens byte for byte: the card's two
// steps, the banner, the three refusals and the activation page a signed-out phone lands on. The
// panel screens are new; the landing's text changed with them (2026-11-09). Each golden was written
// from this render. TAPPA_WRITE_SIGNOUT_GOLDEN=1 writes only files that do not exist and then
// fails: replacing a golden means deleting it first. The confirmation value carries its mint time,
// so it is masked and its presence asserted.
func TestEmployeeSignOut_TheScreensAreTheGolden(t *testing.T) {
	employeeID := uuid.MustParse("6a1b0f4e-0d55-4f5e-9a51-1c2d3e4f5a6b")
	screens := map[string]string{}

	b := panelBrowserWithActions(t, signedInStaff(2), &fakeInviter{})
	screens["card-offer"] = cardOf(t, htmlOf(t, b.do(http.MethodGet, managedRosterHref(employeeID), nil)))
	warning := cardOf(t, htmlOf(t, b.do(http.MethodGet, managedRosterHref(employeeID)+"&confirm=signout", nil)))
	if !confirmValueRE.MatchString(warning) {
		t.Fatal("the warning carries no confirmation value to mask")
	}
	screens["card-warning-two"] = confirmValueRE.ReplaceAllString(warning, "${1}MASKED${2}")
	one := panelBrowserWithActions(t, signedInStaff(1), &fakeInviter{})
	screens["card-warning-one"] = confirmValueRE.ReplaceAllString(cardOf(t, htmlOf(t,
		one.do(http.MethodGet, managedRosterHref(employeeID)+"&confirm=signout", nil))), "${1}MASKED${2}")

	zero := panelBrowserWithActions(t, signedInStaff(0), &fakeInviter{})
	screens["notice-signed-out"] = noticeOf(t, htmlOf(t, zero.do(http.MethodGet, managedRosterHref(employeeID)+"&done=signed-out", nil)))
	for _, word := range []string{"signout-confirm-required", "no-phones", "signout-deactivated"} {
		screens["notice-"+word] = noticeOf(t, htmlOf(t, zero.do(http.MethodGet,
			managedRosterHref(employeeID)+"&problem="+word, nil)))
	}

	// The page a signed-out phone lands on (§5 row 3): one text for a second activation and a
	// manager's sign-out (user decision, 2026-11-09).
	landing := get(t, newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{}), activationSignedOut)
	if landing.Code != http.StatusOK {
		t.Fatalf("the signed-out landing answered %d", landing.Code)
	}
	screens["activate-signed-out"] = landing.Body.String()

	// ONE TAG PER LINE: a golden is still the render byte for byte, split at every "><". On one
	// line the masked confirmation field sat beside the card's ids, which is the shape the
	// redline secret scan (R7d, a0-token) refuses.
	for name, got := range screens {
		screens[name] = strings.ReplaceAll(got, "><", ">\n<")
	}
	write := os.Getenv("TAPPA_WRITE_SIGNOUT_GOLDEN") == "1"
	wrote := 0
	for name, got := range screens {
		path := filepath.Join(signOutGoldenDir, name+".golden")
		want, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) && write {
			if err := os.MkdirAll(signOutGoldenDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			wrote++
			continue
		}
		if err != nil {
			t.Errorf("%s: %v (run with TAPPA_WRITE_SIGNOUT_GOLDEN=1 to write a missing golden)", name, err)
			continue
		}
		if string(want) != got {
			t.Errorf("%s differs from its golden.\n--- want\n%s\n--- got\n%s", name, want, got)
		}
	}
	entries, err := os.ReadDir(signOutGoldenDir)
	if err == nil && len(entries) != len(screens) && !write {
		t.Errorf("%s holds %d file(s) for %d screen(s)", signOutGoldenDir, len(entries), len(screens))
	}
	if wrote > 0 {
		t.Fatalf("wrote %d golden(s); rerun without TAPPA_WRITE_SIGNOUT_GOLDEN", wrote)
	}
}
