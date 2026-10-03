package handler

// unbranded_golden_test.go -- M10 WL-9's byte-golden (ADR 0023 Iddia B): a business
// with NO brand gets, from the tap, result, tap-failure and activation screens, the
// HTML and the Content-Security-Policy it got before WL-9 touched the shared layout.
//
// THE GOLDEN FILES WERE WRITTEN FROM df544c1, BEFORE ANY WL-9 CHANGE, and that is the
// whole value of them: a golden written after the change would pin whatever the change
// did. testdata/unbranded-golden/ holds one file per render below; the write mode
// (TAPPA_WRITE_UNBRANDED_GOLDEN=1) writes ONLY files that do not exist and then fails,
// so replacing a golden means deleting it first -- a visible diff, never a silent one.
//
// WHAT IT CATCHES: a byte of difference -- status line, the Content-Security-Policy
// header, or the body -- in one of the renders listed in unbrandedScreenRenders, all driven
// through the mounted router and read from rec.Result(). WHAT IT DOES NOT: a render
// that is not in the list (ADR 0023 Iddia B: "a variant with no fixture is outside
// this test"), any other header, and the bytes of the tap screen's signed context,
// which carries its own mint time and is masked (its presence is asserted instead).
//
// The list, by name and count, is in the WL-9 card and the ADR 0023 WL-9 note.

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/invite"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
)

const unbrandedGoldenDir = "testdata/unbranded-golden"

// screenGolden is one response, reduced to what the golden holds.
type screenGolden struct {
	name   string
	status int
	csp    string
	body   []byte
}

// readRecorded takes the response the way a client receives it (rec.Result()), not
// the recorder's live header map (agent-brief, M10 OP-8).
func readRecorded(t *testing.T, name string, rec *httptest.ResponseRecorder) screenGolden {
	t.Helper()
	res := rec.Result()
	var body bytes.Buffer
	if _, err := body.ReadFrom(res.Body); err != nil {
		t.Fatalf("%s: reading the body: %v", name, err)
	}
	return screenGolden{name: name, status: res.StatusCode,
		csp: res.Header.Get("Content-Security-Policy"), body: body.Bytes()}
}

// goldenCtxFieldRE finds the tap screen's signed context. Its value carries the mint time,
// so the golden holds a placeholder; maskTapContext asserts the value was there.
var goldenCtxFieldRE = regexp.MustCompile(`(<input type="hidden" name="ctx" value=")([^"]*)(")`)

func maskTapContext(t *testing.T, name string, body []byte) []byte {
	t.Helper()
	m := goldenCtxFieldRE.FindAllSubmatch(body, -1)
	if len(m) != 1 || len(m[0][2]) == 0 {
		t.Fatalf("%s: want exactly one non-empty signed context field, found %d", name, len(m))
	}
	return goldenCtxFieldRE.ReplaceAll(body, []byte(`${1}{{TAP-CONTEXT}}${3}`))
}

// unbrandedScreenRenders is the golden's list. Every entry is a business with no brand:
// the fakes carry none, which is what a business that never opened the Account
// editor reads as (no tenant_branding row).
func unbrandedScreenRenders(t *testing.T) []screenGolden {
	t.Helper()
	var out []screenGolden
	add := func(r screenGolden) { out = append(out, r) }

	// --- the tap screen (GET /t) ------------------------------------------------
	tapPage := func(name string, dir *fakeDirectory) {
		h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, dir, fixedSessions())
		r := readRecorded(t, name, get(t, h, tapURL(), sessionCookie()))
		r.body = maskTapContext(t, name, r.body)
		add(r)
	}
	tapPage("tap-page", &fakeDirectory{facts: okFacts()})
	tapPage("tap-page-foreign-plaque", &fakeDirectory{
		facts: tenant.TapPageFacts{EmployeeName: "Maria Borg"}, err: tenant.ErrForeignLocation,
	})

	// --- the tap family's failure screens ----------------------------------------
	hBad, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, fixedSessions())
	add(readRecorded(t, "tap-problem-bad-url", get(t, hBad, "/t?tag=nonsense", sessionCookie())))
	hUnknown, _ := newTapHandler(t, &fakePreviewer{err: sun.ErrUnknownTag}, &fakeDirectory{facts: okFacts()}, fixedSessions())
	add(readRecorded(t, "tap-problem-unknown-plaque", get(t, hUnknown, tapURL(), sessionCookie())))
	hDown, _ := newTapHandler(t, &fakePreviewer{err: errTapProbe}, &fakeDirectory{facts: okFacts()}, fixedSessions())
	add(readRecorded(t, "tap-problem-server-with-retry", get(t, hDown, tapURL(), sessionCookie())))
	_, tpLimited := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, fixedSessions())
	limited := httptest.NewRecorder()
	tpLimited.renderTooManyRequests(limited, getRequest(tapURL()))
	add(readRecorded(t, "tap-problem-too-many", limited))

	postWith := func(name string, svc *fakeCheckins, signed bool) {
		h, tp := newCheckinHandler(t, svc, fixedSessions())
		form := url.Values{"ctx": {"not-a-context"}}
		if signed {
			form.Set("ctx", mintedContext(t, tp, tapFixedSessionID, tapContext{
				UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true,
				TagTenantID: testTenant, LocationID: tapLocation,
			}))
			form.Set("lat", resultProbeLat)
			form.Set("lng", resultProbeLng)
		}
		add(readRecorded(t, name, postForm(t, h, form, sessionCookie())))
	}
	postWith("tap-problem-stale-context", &fakeCheckins{}, false)
	postWith("tap-problem-post-unknown-plaque", &fakeCheckins{err: checkin.ErrUnknownTag}, true)
	postWith("tap-problem-another-employers-plaque", &fakeCheckins{result: checkin.Result{Outcome: checkin.OutcomeForeignTenant}}, true)

	// --- the result screen (POST /api/checkin): verdict x direction x business
	// type x practice, chosen so every branch of result.templ's stamp, closing and
	// brandMessage switches renders at least once -----------------------------------
	result := func(name string, d tapDecisionShape) {
		res := checkin.Result{
			Outcome:      checkin.OutcomeRecorded,
			Decision:     withNote(decisionOf(d.verdict, d.direction, d.practice), d.note),
			LocationName: d.venue,
			Timezone:     "Europe/Malta",
			OccurredAt:   mustTime(t, "2026-07-31T12:03:22Z"),
			BusinessType: d.business,
		}
		postWith(name, &fakeCheckins{result: res}, true)
	}
	for _, c := range []struct {
		name string
		d    tapDecisionShape
	}{
		{"result-ok-in-restaurant", tapDecisionShape{"ok", "in", "restaurant", false, noteIPMatched, "St Julians"}},
		{"result-ok-out-restaurant", tapDecisionShape{"ok", "out", "restaurant", false, noteIPMatched + "; " + noteStaleOpenIn, "St Julians"}},
		{"result-ok-in-production", tapDecisionShape{"ok", "in", "production", false, noteIPMatched, "St Julians"}},
		{"result-ok-out-production", tapDecisionShape{"ok", "out", "production", false, noteIPMatched, "St Julians"}},
		{"result-ok-in-other-trade", tapDecisionShape{"ok", "in", "bar", false, noteIPMatched, "St Julians"}},
		{"result-ok-out-other-trade", tapDecisionShape{"ok", "out", "bar", false, noteIPMatched, "St Julians"}},
		{"result-ok-no-direction", tapDecisionShape{"ok", "", "restaurant", false, noteIPMatched, "St Julians"}},
		{"result-ok-in-practice", tapDecisionShape{"ok", "in", "restaurant", true, noteIPMatched, "St Julians"}},
		{"result-ok-empty-note-no-venue", tapDecisionShape{"ok", "in", "restaurant", false, "", ""}},
		{"result-flag-in", tapDecisionShape{"flag", "in", "restaurant", false, noteNoPlace, "St Julians"}},
		{"result-flag-in-practice", tapDecisionShape{"flag", "in", "restaurant", true, noteNoPlace, "St Julians"}},
		{"result-reject", tapDecisionShape{"reject", "", "restaurant", false, noteDeadTag, "St Julians"}},
		{"result-ignored", tapDecisionShape{"ignored", "", "restaurant", false, noteDebounce, "St Julians"}},
		{"result-unknown-verdict", tapDecisionShape{"something-new", "in", "restaurant", false, noteNoPlace, "St Julians"}},
	} {
		result(c.name, c.d)
	}

	// --- the activation family (its own router; no CSP header -- recorded as "") ---
	hAct := newHandler(t, &fakeInvites{}, &fakeSessions{}, &fakeAudit{})
	add(readRecorded(t, "activate-landing", get(t, hAct, "/activate")))
	add(readRecorded(t, "activate-form", get(t, hAct, "/activate", codeCookie())))
	hFail := newHandler(t, &fakeInvites{lookup: func(invite.Code) (invite.Context, error) {
		return invite.Context{}, invite.ErrUnknownCode
	}}, &fakeSessions{}, &fakeAudit{})
	add(readRecorded(t, "activate-failure", post(t, hFail, consent(), codeCookie())))
	inv, sess := victimHolds(t)
	hVictim := newHandler(t, inv, sess, &fakeAudit{})
	victim := &http.Cookie{Name: session.CookieName, Value: victimSession}
	add(readRecorded(t, "activate-phone-in-use-form", post(t, hVictim, consent(), codeCookie(), victim)))
	crossSite := httptest.NewRequest(http.MethodGet, "/activate?code="+fakeCode, nil)
	crossSite.RemoteAddr = "203.0.113.9:5000"
	crossSite.Header.Set("Sec-Fetch-Site", "cross-site")
	crossSite.AddCookie(victim)
	inv2, sess2 := victimHolds(t)
	hVictim2 := newHandler(t, inv2, sess2, &fakeAudit{})
	confirm := httptest.NewRecorder()
	hVictim2.ServeHTTP(confirm, crossSite)
	add(readRecorded(t, "activate-continue", confirm))
	add(readRecorded(t, "activate-done", get(t, hAct, "/activate/done", &http.Cookie{Name: session.CookieName, Value: fakeCode})))
	add(readRecorded(t, "activate-done-without-session", get(t, hAct, "/activate/done")))
	for step := 1; step <= 3; step++ {
		add(readRecorded(t, fmt.Sprintf("activate-tour-%d", step), get(t, hAct, fmt.Sprintf("/activate/tour?step=%d", step), tourCookie())))
	}
	return out
}

// tapDecisionShape is one result-screen row: verdict, direction, business type,
// practice, the note production would carry, and the venue name.
type tapDecisionShape struct {
	verdict, direction, business string
	practice                     bool
	note, venue                  string
}

func goldenBytes(r screenGolden) []byte {
	var b bytes.Buffer
	fmt.Fprintf(&b, "status: %d\ncontent-security-policy: %s\n---\n", r.status, r.csp)
	b.Write(r.body)
	return b.Bytes()
}

// unbrandedScreenCount is how many renders the golden holds, by name, in the WL-9
// card and ADR 0023's WL-9 note. A render and its golden file deleted TOGETHER keep
// the list and the directory in step; this number is what notices (WL-9 2nd round,
// X22).
const unbrandedScreenCount = 33

// TestUnbrandedScreens_AreByteIdenticalToTheGolden is ADR 0023 Iddia B's PART I for
// WL-9: every listed render of a business with no brand is byte-identical -- status,
// Content-Security-Policy and body -- to the golden written before WL-9 changed the
// layout; the list, the golden directory and unbrandedScreenCount name the same
// number of renders. The run logs the list and its size.
func TestUnbrandedScreens_AreByteIdenticalToTheGolden(t *testing.T) {
	renders := unbrandedScreenRenders(t)
	if len(renders) != unbrandedScreenCount {
		t.Errorf("the golden list has %d renders, the card and the ADR name %d", len(renders), unbrandedScreenCount)
	}
	write := os.Getenv("TAPPA_WRITE_UNBRANDED_GOLDEN") == "1"

	seen := map[string]bool{}
	var names, written []string
	for _, r := range renders {
		if seen[r.name] {
			t.Fatalf("two renders are named %q; the golden would hold one of them", r.name)
		}
		seen[r.name] = true
		names = append(names, r.name)
		path := filepath.Join(unbrandedGoldenDir, r.name+".golden")
		got := goldenBytes(r)
		want, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) && write {
			if err := os.MkdirAll(unbrandedGoldenDir, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatalf("writing %s: %v", path, err)
			}
			written = append(written, r.name)
			continue
		}
		if err != nil {
			t.Fatalf("%s: no golden (%v). The golden is written from the code BEFORE a layout "+
				"change, never after; see this file's header.", r.name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from its golden (%d bytes vs %d).\n got:\n%s\nwant:\n%s",
				r.name, len(got), len(want), got, want)
		}
	}
	// Every golden on disk is one this test renders: a golden nobody renders any more
	// is a fixture that silently stopped being checked.
	entries, err := os.ReadDir(unbrandedGoldenDir)
	if err != nil {
		t.Fatalf("reading %s: %v", unbrandedGoldenDir, err)
	}
	for _, e := range entries {
		if n := strings.TrimSuffix(e.Name(), ".golden"); !seen[n] {
			t.Errorf("golden %s is rendered by nothing in unbrandedScreenRenders", e.Name())
		}
	}
	if len(entries) != unbrandedScreenCount {
		t.Errorf("%s holds %d files, the card and the ADR name %d", unbrandedGoldenDir, len(entries), unbrandedScreenCount)
	}
	if len(written) > 0 {
		t.Fatalf("wrote %d missing golden file(s): %s -- unset TAPPA_WRITE_UNBRANDED_GOLDEN and rerun",
			len(written), strings.Join(written, ", "))
	}
	t.Logf("unbranded golden: %d renders: %s", len(names), strings.Join(names, "; "))
}
