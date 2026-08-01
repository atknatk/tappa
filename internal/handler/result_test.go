package handler

// M5-06 — the confirmation screen and the failure screens, measured through the
// PRODUCTION path.
//
// 🔴 WHERE EACH ASSERTION GETS ITS HTML — counted, because an earlier version of
// this header said "EVERY ASSERTION HERE IS ON HTML THAT CAME OUT OF THE MOUNTED
// ROUTER" and that was false of five tests in this file:
//
//	THROUGH THE MOUNTED ROUTER (chi + Mount's middleware order + the real handler)
//	  every confirmation-screen test, via resultBody -> POST /api/checkin;
//	  TestProblemScreens_ReachTheBrowserThroughTheRouter, which drives five of the
//	  six failure screens end to end; AND MOST OF THE SUBTESTS of the three tests
//	  listed under "bare recorder" below — those are MIXED, not router-less, and an
//	  earlier version of this bucket left them out entirely:
//	  TestProblemScreen_RetryOnlyWhereRefetchingCanHelp reaches the GET and POST
//	  screens through get/postForm and drops to a recorder only for the 429 and the
//	  POST guard; TestTapPage_EveryGetServerErrorBranchIsAccountedFor drives three
//	  of its four branches through get(); and
//	  TestTapResponses_CarryTheContentSecurityPolicy takes seven of its eight
//	  readings from routed responses; and
//	  TestProblemScreen_RetryHrefEscapesAHostileQuery, which drives a hostile query
//	  string through get() onto a screen that renders the retry link.
//	BY RENDERING THE COMPONENT DIRECTLY (3 call sites, in 3 tests — two of which
//	are MIXED)
//	  TestProblemScreens_SayExactlyThisAndNothingElse is the only one that is
//	  component-only: it hands pages.Problem a view it built, so it proves the
//	  TEMPLATE and the CONSTANTS agree and nothing about what the product serves —
//	  which is why TestProblemScreens_ReachTheBrowserThroughTheRouter exists beside
//	  it. TestScreens_RenderOnlyTheseElements and
//	  TestScreens_ReferenceOnlyOurOwnAssets each render the component directly ONLY
//	  for their failure-screen subtest; their confirmation-screen subtest goes
//	  through mustBody, i.e. the mounted router. Listing them as "component only"
//	  undersold them.
//	BY CALLING A HANDLER METHOD ON A BARE RECORDER (3 tests, 4 call sites)
//	  TestProblemScreen_RetryOnlyWhereRefetchingCanHelp (x2),
//	  TestTapPage_EveryGetServerErrorBranchIsAccountedFor,
//	  TestTapResponses_CarryTheContentSecurityPolicy. Each drives a branch the
//	  router cannot reach cheaply — a 429 without tripping the limiter, an
//	  unresolved identity, a POST guard.
//	ON NO HTML AT ALL (2 tests)
//	  TestResultView_FieldCountIsTheSpec (reflection),
//	  TestCompiledCSS_GeneratesNoText (the built stylesheet).
//
// WHY THE SPLIT IS ACCEPTABLE, and why it was NOT before this round: a test that
// builds the object it audits cannot see a wiring mistake between two of them
// (state.md / agent-brief: M5-04's NewTap shipped a limiter with no recorder and
// the suite stayed green because the tests built their own). The component-level
// tests were the ONLY thing holding the six failure screens' copy, so exactly that
// gap was open — Tap.renderProblem could have been pointed at another template and
// they would all have stayed green. It is closed by the router-level test above,
// which reads the SAME literal table (problemScreens); the component-level tests
// now add per-variant breadth on top of a wired proof rather than standing in for
// one. Measured (counting `--- FAIL:` LINES, which include subtests, not test
// functions — the unit matters, and two audits reported different totals for the
// same mutation before it was stated): diverting renderProblem to a different view
// produced 9 FAIL lines when this was written and 17 after later tests were added;
// pointing it at a different template, 18 then 20. The number moves with the suite;
// what does not move is that both go RED.
//
// The one thing the tests DO choose is what the domain answers with — a
// checkin.Result, through the same fakeCheckins the rest of this package uses.
// The decision engine's own correctness is proved in internal/domain/tap; what
// is proved here is that a decision reaches a screen intact.

import (
	"context"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/session"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/web"
	"github.com/atknatk/tappa/web/templates/pages"
)

// errTapProbe is the shape of "something below us broke". It is deliberately an
// OUTAGE rather than a corrupt key ref, because those two arrive at the same
// branch and the handler cannot tell them apart — a limit tap.go now states
// instead of denying.
var errTapProbe = errors.New("connection reset by peer")

func typePtr(s string) *tap.Type { v := tap.Type(s); return &v }

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return v
}

// Bare request/recorder builders, for the FOUR call sites (in three tests) that
// call a render method directly rather than through the router — enumerated in
// this file's header, with the reason each one cannot use the router. The count
// used to say "two"; it was counted this round, not estimated.
func httptestRecorder() *httptest.ResponseRecorder { return httptest.NewRecorder() }

func getRequest(target string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, target, nil)
	r.RemoteAddr = "203.0.113.5:41234"
	return r
}

func postRequest(target string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, target, strings.NewReader(""))
	r.RemoteAddr = "203.0.113.5:41234"
	return r
}

// resultBody drives one tap all the way to a rendered page.
//
// It mints a REAL signed context with the production key and posts it with a
// coordinate attached, so every body IT RETURNS has had a GPS fix in the request
// that produced it — which is what makes the §4.7 leak check below mean something
// (a screen cannot be shown not to print a coordinate it never had). The scope is
// this function's output, not the whole file's: the GET /t failure bodies
// elsewhere in here carry no coordinate, because that request has none to carry.
func resultBody(t *testing.T, res checkin.Result, form url.Values) (string, int) {
	t.Helper()
	svc := &fakeCheckins{result: res}
	sess := fixedSessions()
	h, tp := newCheckinHandler(t, svc, sess)

	ctxValue := mintedContext(t, tp, tapFixedSessionID, tapContext{
		UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true,
		TagTenantID: testTenant, LocationID: tapLocation,
	})
	if form == nil {
		form = url.Values{}
	}
	form.Set("ctx", ctxValue)
	if form.Get("lat") == "" {
		form.Set("lat", resultProbeLat)
		form.Set("lng", resultProbeLng)
	}
	w := postForm(t, h, form, sessionCookie())
	return w.Body.String(), w.Code
}

// A coordinate the tests send and then look for. §4.2 allows reading it at the
// moment of a tap; §4.7 forbids it reaching a screen or a log. Distinctive
// enough that a substring search cannot collide with markup.
const (
	resultProbeLat = "35.917123"
	resultProbeLng = "14.409456"
)

// decisionOf builds the decision a verdict/direction pair produces, so the table
// below reads as rows rather than as struct literals.
func decisionOf(verdict, direction string, practice bool) tap.Decision {
	d := tap.Decision{Verdict: tap.Verdict(verdict), Trust: 100, Practice: practice}
	if direction != "" {
		ty := tap.Type(direction)
		d.Type = &ty
	}
	return d
}

// ---------------------------------------------------------------------------
// The stamp: text AND colour, never colour alone.
// ---------------------------------------------------------------------------

// TestResultScreen_StampCarriesTheWordAndTheBrandClass is the accessibility
// criterion and the brand's fixed status-to-colour mapping in one table, because
// they are two halves of one rule (skill tappa-brand): ok -> tappa-green, flag ->
// saffron, reject -> tomato, ignored -> line, and the WORD says the same thing so
// a SCREEN READER gets the verdict.
//
// ⚠️ THIS TEST IS NOT EVIDENCE THAT THE STAMP IS LEGIBLE, and it never was. It
// reads the HTML, so the word and its class are all it can see; the colours are
// in the stylesheet. That distinction was load-bearing: with the word in the
// status colour, .stamp at 11px bold and opacity .8, stamp--ignored (line
// #C9D2C8) was 1.52:1 and stamp--flagged (saffron #D98E2B) 2.62:1 against the
// docket's bg-paper — far below AA's 4.5:1, and 1.39:1 / 2.14:1 once the opacity
// was applied, which also took stamp--rejected under the line at 3.77:1. Three of
// the five as rendered, and every case here stayed green through all of it.
//
// FIXED IN THE STYLESHEET ON 2026-08-01 (user decision): the word is `ink` and
// the status colour is the frame, the inner ring and a 10% ground, which is why
// the class assertions below are unchanged — the mapping moved, it did not go
// away. Measured after the change: 13.85 / 14.81 / 13.99 / 15.55 / 13.27:1.
// TestCompiledCSS_StampWordIsInk is what holds it, and it holds it only where a
// stylesheet has been built (never on CI — see its comment).
//
// The other carriers, unchanged: the closing sentence is text-ink/70 on the
// body's bg-porcelain, because closing() renders OUTSIDE the docket, which
// composites to 5.70:1 — over AA's 4.5:1, but not the "~16:1" an earlier version
// of this comment copied from the HEADLINE (solid ink on bg-paper inside the
// docket, 16.17:1). The headline helps on two of the four verdicts, not on flag.
// The words are pinned by TestResultScreen_SaysExactlyThisAndNothingElse.
//
// The class names are asserted as the strings that must appear in the HTML,
// which is also what Tailwind scans for — a class that stops being a literal in a
// .templ file stops being compiled (result.templ's comment has the measurement).
func TestResultScreen_StampCarriesTheWordAndTheBrandClass(t *testing.T) {
	tests := []struct {
		name    string
		verdict string
		word    string
		class   string
	}{
		{"ok is APPROVED in tappa-green", "ok", "APPROVED", "stamp--approved"},
		{"flag is FLAGGED in saffron", "flag", "FLAGGED", "stamp--flagged"},
		{"reject is REJECTED in tomato", "reject", "REJECTED", "stamp--rejected"},
		{"ignored is IGNORED in line grey", "ignored", "IGNORED", "stamp--ignored"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, code := resultBody(t, checkin.Result{
				Outcome:      checkin.OutcomeRecorded,
				Decision:     decisionOf(tc.verdict, "in", false),
				LocationName: "St Julians",
				Timezone:     "Europe/Malta",
				BusinessType: "restaurant",
			}, nil)
			if code != http.StatusOK {
				t.Fatalf("status = %d, want 200", code)
			}
			if !strings.Contains(body, tc.word) {
				t.Fatalf("the stamp word %q is missing: status would be carried by colour alone", tc.word)
			}
			if !strings.Contains(body, tc.class) {
				t.Fatalf("the stamp class %q is missing: the brand's fixed colour mapping is not applied", tc.class)
			}
			// The word and the class must be on the SAME element. Two correct halves
			// on different elements is exactly the inversion this test exists to
			// catch (an APPROVED word in a rejected tomato frame).
			if !strings.Contains(body, `class="stamp `+tc.class+`">`+tc.word) {
				t.Fatalf("the word %q and the class %q are not on one element: the stamp says two things", tc.word, tc.class)
			}
			for _, other := range tests {
				if other.class == tc.class {
					continue
				}
				if strings.Contains(body, other.class) {
					t.Fatalf("verdict %q also rendered %q: the status-to-colour mapping is not one-to-one",
						tc.verdict, other.class)
				}
			}
		})
	}
}

// TestResultScreen_PracticeIsStampedAndSaysItDoesNotCount is §5's practice tap:
// TRAINING, and an explicit sentence that the hours do not count. The brand
// message is deliberately absent — "have a great shift" beside "this does not
// count toward your hours" contradicts itself.
//
// 🔴 THE CLASS STRING IS PINNED EXACTLY, and it was not until an audit walked
// through the gap. The four verdict stamps above are held to
// `class="stamp X">WORD` as one literal; TRAINING was held to
// strings.Contains("stamp--training"), which a SIXTH class slides straight past.
// Measured: `class="stamp stamp--training text-saffron"` — an ordinary Tailwind
// utility in the markup, the normal way colour is added in this repo — left the
// WHOLE SUITE GREEN while the word rendered at 2.15:1 against its own ground,
// because .text-saffron lands in the utilities layer at equal specificity and
// later in the file than .stamp. The stylesheet-side check cannot see that (its
// LIMITS list says so); this is the side that can, so it is held as tightly as
// its four neighbours.
func TestResultScreen_PracticeIsStampedAndSaysItDoesNotCount(t *testing.T) {
	body, _ := resultBody(t, checkin.Result{
		Outcome:      checkin.OutcomeRecorded,
		Decision:     decisionOf("ok", "in", true),
		LocationName: "St Julians",
		BusinessType: "restaurant",
	}, nil)

	// One literal: the word, the class, and NOTHING ELSE on the element.
	if !strings.Contains(body, `<span class="stamp stamp--training">TRAINING</span>`) {
		t.Fatalf("the TRAINING stamp is not exactly `<span class=\"stamp stamp--training\">TRAINING</span>`.\n"+
			"An extra class here is a colour override at equal specificity — measured at 2.15:1 "+
			"for text-saffron, where AA asks 4.5:1 (user decision, 2026-08-01: the word is ink and "+
			"the status colour is the frame).\nbody: %s", body)
	}
	if !strings.Contains(body, "does not count toward your hours") {
		t.Fatal("a practice tap did not say the hours do not count")
	}
	if strings.Contains(body, "kebabs rolling") {
		t.Fatal("a practice tap was sent off as if a shift had started")
	}
}

// ---------------------------------------------------------------------------
// No button on success — CLAUDE.md §9, the sacred screen.
// ---------------------------------------------------------------------------

// TestResultScreen_HasNoButtonOrFormOnAnyVerdict. §9 says the confirmation screen
// carries no button: the next action is a new physical touch of the plaque, and
// nothing on a screen can produce an attendance record. The assertion covers ALL
// FOUR verdicts rather than only `ok`, because the tempting place to add a "Try
// again" is precisely the three that did not count an hour — and a retry there
// would either duplicate a real record or reproduce the same answer.
func TestResultScreen_HasNoButtonOrFormOnAnyVerdict(t *testing.T) {
	for _, verdict := range []string{"ok", "flag", "reject", "ignored"} {
		t.Run(verdict, func(t *testing.T) {
			body, _ := resultBody(t, checkin.Result{
				Outcome:      checkin.OutcomeRecorded,
				Decision:     decisionOf(verdict, "in", false),
				LocationName: "St Julians",
				BusinessType: "restaurant",
			}, nil)
			for _, forbidden := range []string{"<button", "<form", "Try again"} {
				if strings.Contains(body, forbidden) {
					t.Fatalf("the confirmation screen rendered %q on verdict %q (§9: no button here)", forbidden, verdict)
				}
			}
		})
	}
}

// TestResultScreen_OnlyASuccessSaysAllDone is §4.6 on the employee's own surface.
//
// 🔴 THE FLAG ROW IS THE POINT. Until M5-06 the closing line came from one bool
// ("did this put an hour on the record"), which grouped flag with ok and told
// somebody whose tap had just entered a manager's approval queue "All done — you
// can close this page." That is the silent approval §4.6 forbids. Every row here
// pins one sentence to one verdict; none of the three non-ok rows may claim the
// tap is finished, and none of them may offer a retry.
func TestResultScreen_OnlyASuccessSaysAllDone(t *testing.T) {
	tests := []struct {
		name     string
		verdict  string
		want     string
		wantNot  []string
		alsoWant string
	}{
		{
			name:     "ok is done",
			verdict:  "ok",
			want:     "All done — you can close this page.",
			alsoWant: "Have a great shift",
		},
		{
			name:    "flag says approval is still needed, and does not promise it",
			verdict: "flag",
			want:    "needs your manager's approval before it counts toward your hours",
			wantNot: []string{"All done", "Have a great shift", "keep those kebabs rolling",
				// A manager can REFUSE a flagged tap, so the screen may not state
				// the outcome; and the appeal must stay open on the one verdict
				// that most needs it (§4.6).
				"will confirm", "nothing for you to do"},
			alsoWant: "Tell them if that looks wrong",
		},
		{
			name:    "reject says recorded but not counted",
			verdict: "reject",
			want:    "recorded but not counted",
			wantNot: []string{"All done", "Have a great shift"},
		},
		{
			name:    "ignored says only that THIS tap did not count",
			verdict: "ignored",
			want:    "was not counted.",
			wantNot: []string{"All done", "Have a great shift"},
			// The escape hatch must survive: this is the one verdict whose
			// predecessor the screen knows nothing about.
			alsoWant: "Tell your manager if that looks wrong",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := resultBody(t, checkin.Result{
				Outcome:      checkin.OutcomeRecorded,
				Decision:     decisionOf(tc.verdict, "in", false),
				LocationName: "St Julians",
				BusinessType: "restaurant",
			}, nil)
			if !strings.Contains(body, tc.want) {
				t.Fatalf("verdict %q did not say %q", tc.verdict, tc.want)
			}
			if tc.alsoWant != "" && !strings.Contains(body, tc.alsoWant) {
				t.Fatalf("verdict %q did not say %q", tc.verdict, tc.alsoWant)
			}
			for _, no := range tc.wantNot {
				if strings.Contains(body, no) {
					t.Fatalf("verdict %q said %q: a tap that was not counted must not read as finished (§4.6)", tc.verdict, no)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// The tenant's own brand line.
// ---------------------------------------------------------------------------

// TestResultScreen_BrandMessageFollowsTheTenantKindAndTheDirection is the card's
// "tenant-specific message" criterion, pinned in both dimensions it varies over.
//
// It also pins what the card does NOT say and the schema forces: the copy is
// keyed on the tenant's BUSINESS TYPE, not on the tenant, so an unknown or empty
// type must land on a neutral sentence — never silence (a missing line reads as a
// bug) and never somebody else's trade (a hotel porter is not keeping kebabs
// rolling). Per-tenant editable copy is M9-04.
func TestResultScreen_BrandMessageFollowsTheTenantKindAndTheDirection(t *testing.T) {
	tests := []struct {
		name         string
		businessType string
		direction    string
		want         string
		wantNot      []string
	}{
		{
			name: "restaurant in", businessType: "restaurant", direction: "in",
			want:    "Have a great shift — keep those kebabs rolling! 🌯",
			wantNot: []string{"stay safe on the floor"},
		},
		{
			name: "restaurant out", businessType: "restaurant", direction: "out",
			want:    "Great work today. See you next shift! 👋",
			wantNot: []string{"keep those kebabs rolling", "Thank you for your work today"},
		},
		{
			name: "production in", businessType: "production", direction: "in",
			want:    "Have a productive shift — stay safe on the floor! 🏭",
			wantNot: []string{"keep those kebabs rolling"},
		},
		{
			name: "production out", businessType: "production", direction: "out",
			want:    "Shift complete. Thank you for your work today! 👋",
			wantNot: []string{"See you next shift", "stay safe on the floor"},
		},
		{
			name: "an unlisted type gets the neutral line in", businessType: "hotel", direction: "in",
			want:    "Have a great shift!",
			wantNot: []string{"kebabs", "floor", "🏭"},
		},
		{
			name: "an unlisted type gets the neutral line out", businessType: "hotel", direction: "out",
			want:    "Thanks for today. See you next time! 👋",
			wantNot: []string{"kebabs", "Thank you for your work today"},
		},
		{
			name: "an EMPTY type still says something", businessType: "", direction: "in",
			want:    "Have a great shift!",
			wantNot: []string{"kebabs", "floor"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := resultBody(t, checkin.Result{
				Outcome:      checkin.OutcomeRecorded,
				Decision:     decisionOf("ok", tc.direction, false),
				LocationName: "St Julians",
				BusinessType: tc.businessType,
			}, nil)
			if !strings.Contains(body, tc.want) {
				t.Fatalf("business type %q direction %q did not render %q", tc.businessType, tc.direction, tc.want)
			}
			for _, no := range tc.wantNot {
				if strings.Contains(body, no) {
					t.Fatalf("business type %q direction %q also rendered %q: the wrong tenant's copy", tc.businessType, tc.direction, no)
				}
			}
			// The direction also has to survive into the headline, or "Tapped in"
			// and the send-off could disagree with each other.
			wantHeadline := "Tapped " + tc.direction
			if !strings.Contains(body, wantHeadline) {
				t.Fatalf("headline is not %q: the direction did not reach the screen", wantHeadline)
			}
		})
	}
}

// TestResultScreen_BrandMessageIsAbsentUnlessTheTapCounted. §4.6 again, from the
// copy side: a warm send-off over a tap that is waiting for a manager, was
// refused, or was ignored as a duplicate would read as approval.
func TestResultScreen_BrandMessageIsAbsentUnlessTheTapCounted(t *testing.T) {
	for _, verdict := range []string{"flag", "reject", "ignored"} {
		t.Run(verdict, func(t *testing.T) {
			body, _ := resultBody(t, checkin.Result{
				Outcome:      checkin.OutcomeRecorded,
				Decision:     decisionOf(verdict, "in", false),
				LocationName: "St Julians",
				BusinessType: "restaurant",
			}, nil)
			for _, no := range []string{"kebabs rolling", "Have a great shift", "See you next shift"} {
				if strings.Contains(body, no) {
					t.Fatalf("verdict %q was sent off with %q", verdict, no)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Mono, and what the screen refuses to print.
// ---------------------------------------------------------------------------

// TestResultScreen_ClockIsMono. Skill tappa-brand: every number is mono, and the
// docket feeling comes from exactly that. The wall clock is the number a person
// reads; the trust score is the one a manager asks about.
//
// LIMIT, stated rather than claimed: there is no DURATION on this screen (a
// shift total is a report, M6), so the card's "clock and duration in mono" is
// satisfied here by the only one of the two that exists.
func TestResultScreen_ClockIsMono(t *testing.T) {
	body, _ := resultBody(t, checkin.Result{
		Outcome:      checkin.OutcomeRecorded,
		Decision:     tap.Decision{Verdict: "ok", Trust: 70, Type: typePtr("in")},
		LocationName: "St Julians",
		OccurredAt:   mustTime(t, "2026-07-31T12:03:22Z"),
		Timezone:     "Europe/Malta",
		BusinessType: "restaurant",
	}, nil)

	// Europe/Malta is UTC+2 in July: 12:03:22Z is 14:03:22 local (§6 — the
	// conversion happens at the render boundary and nowhere below it).
	if !strings.Contains(body, `<span class="block font-mono text-xl font-normal text-ink/70">14:03:22</span>`) {
		t.Fatal("the wall clock is not rendered in mono at the tenant's local time")
	}
	if !strings.Contains(body, `<dd class="font-mono">70</dd>`) {
		t.Fatal("the trust score is not rendered in mono")
	}
	// .docket itself sets font-mono for the card (input.css), which is where the
	// docket motif comes from; without it the two spans above are the only mono.
	if !strings.Contains(body, `class="docket"`) {
		t.Fatal("the confirmation is not a docket")
	}
}

// uuidRE and coordRE are the §4.7 tripwires: an id or a coordinate appearing in
// a body is a leak regardless of which field carried it there.
var (
	uuidRE  = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	coordRE = regexp.MustCompile(`\d{1,3}\.\d{4,}`)
)

// TestResultScreen_LeaksNoIdentifierOrCoordinate is §4.7 measured on the wire
// rather than argued from the field list. The request that produced this body
// carried a session cookie, a signed context over real ids and a GPS fix; none of
// them may come back out.
func TestResultScreen_LeaksNoIdentifierOrCoordinate(t *testing.T) {
	for _, verdict := range []string{"ok", "flag", "reject", "ignored"} {
		t.Run(verdict, func(t *testing.T) {
			body, _ := resultBody(t, checkin.Result{
				Outcome:       checkin.OutcomeRecorded,
				Decision:      decisionOf(verdict, "in", false),
				TransactionID: uuid.New(),
				LocationName:  "St Julians",
				Timezone:      "Europe/Malta",
				BusinessType:  "restaurant",
			}, nil)

			if m := uuidRE.FindString(body); m != "" {
				t.Fatalf("an identifier reached the screen: %q", m)
			}
			for _, secret := range []string{resultProbeLat, resultProbeLng, sessionCookie().Value, tapUID} {
				if strings.Contains(body, secret) {
					t.Fatalf("a §4.7 value reached the screen: %q", secret)
				}
			}
			if m := coordRE.FindString(body); m != "" {
				t.Fatalf("something coordinate-shaped reached the screen: %q", m)
			}
			// The business type chooses a sentence; it is never printed.
			if strings.Contains(body, "restaurant") {
				t.Fatal("the tenant's business type was printed rather than used to pick a sentence")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// "Try again" — where it is honest, and where a button would be a lie.
// ---------------------------------------------------------------------------

// TestProblemScreen_RetryOnlyWhereRefetchingCanHelp is the M5-06 card's "a
// failure has Try again", implemented as narrowly as honesty allows.
//
// Three of the FIVE GET-path server errors get a real link back to the plaque's
// own URL. That is safe because GET /t writes no record and advances no counter —
// measured against real Postgres by TestTapDB_PageNeverMovesTheCounter
// (tap_db_test.go), which is INHERITED from M5-04/M5-05 and not written by this
// task; it is cited as an existing guarantee this screen leans on, not as new
// evidence.
//
// The other two GET branches and every POST screen get an INSTRUCTION instead,
// each for its own reason: re-fetching would reproduce the failure exactly (a
// malformed URL, an unknown plaque, a mint failure, an unknown session state);
// or it would spend the rate-limit budget the page is asking the caller to stop
// spending (429); or there is no address to offer at all (every POST screen —
// the tap context is single-use and the chip's SUN URL is not in the request).
func TestProblemScreen_RetryOnlyWhereRefetchingCanHelp(t *testing.T) {
	t.Run("GET /t server error offers a link to the same tap url", func(t *testing.T) {
		pv := &fakePreviewer{err: errTapProbe}
		h, _ := newTapHandler(t, pv, &fakeDirectory{facts: okFacts()}, fixedSessions())

		w := get(t, h, tapURL(), sessionCookie())
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "Try again") {
			t.Fatal("a transient GET failure offered no retry at all")
		}
		// The ampersands come back HTML-escaped, which is templ doing its job; the
		// browser resolves it to exactly the request URI that failed.
		wantHref := `href="` + strings.ReplaceAll(tapURL(), "&", "&amp;") + `"`
		if !strings.Contains(body, wantHref) {
			t.Fatalf("the retry does not point back at the tap url (want %s); body: %s", wantHref, body)
		}
		// A LINK, never a form: an anchor issues a GET and cannot resubmit a POST,
		// so no failure screen can produce a second attendance record.
		if strings.Contains(body, "<form") || strings.Contains(body, "<button") {
			t.Fatal("the retry is a form or a button: it could resubmit")
		}
	})

	t.Run("a mint failure gets no link: it is deterministic", func(t *testing.T) {
		// The one GET-path 500 that is PROVABLY not worth retrying. mint refuses
		// exactly two conditions (tapcontext.go); a nil session id is the one a test
		// can produce, because Identity.Live() reads only the STATE — a Verify that
		// succeeds with a zero id is SessionLive with nothing to bind a signature to.
		//
		// It is the honest half of the narrowing tap.go documents: the other three
		// 500 branches keep their link even though their error class is MIXED,
		// because the handler cannot prove those are permanent. This one it can.
		sess := &fakeSessions{verify: func() (session.Resolved, error) {
			return session.Resolved{ID: uuid.Nil, TenantID: testTenant, EmployeeID: testEmployee}, nil
		}}
		h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)},
			&fakeDirectory{facts: okFacts()}, sess)

		w := get(t, h, tapURL(), sessionCookie())
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 (the mint branch); this test is not reaching it", w.Code)
		}
		if strings.Contains(w.Body.String(), "Try again") {
			t.Fatal("the mint failure offers a retry that reproduces itself every time")
		}
		if !strings.Contains(w.Body.String(), "Tap the plaque again in a moment.") {
			t.Fatal("the mint failure lost the instruction that replaces the button")
		}
	})

	t.Run("an unknown plaque gets an instruction and no button", func(t *testing.T) {
		pv := &fakePreviewer{err: sun.ErrUnknownTag}
		h, _ := newTapHandler(t, pv, &fakeDirectory{facts: okFacts()}, fixedSessions())

		w := get(t, h, tapURL(), sessionCookie())
		if w.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", w.Code)
		}
		body := w.Body.String()
		if strings.Contains(body, "Try again") {
			t.Fatal("an unknown plaque offered a retry that cannot succeed")
		}
		if !strings.Contains(body, "Tell your manager which door you tapped.") {
			t.Fatal("an unknown plaque did not say what to do instead")
		}
	})

	t.Run("a malformed tap url gets an instruction and no button", func(t *testing.T) {
		h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)},
			&fakeDirectory{facts: okFacts()}, fixedSessions())

		w := get(t, h, "/t?tag=nonsense", sessionCookie())
		body := w.Body.String()
		if strings.Contains(body, "Try again") {
			t.Fatal("a URL that cannot parse offered to fetch itself again")
		}
		if !strings.Contains(body, "Try tapping the plaque again.") {
			t.Fatal("a malformed url did not say what to do instead")
		}
	})

	t.Run("a POST failure never offers a link", func(t *testing.T) {
		// Every POST-side failure screen, driven through the real endpoint. None of
		// them may carry a retry: /api/checkin is not a GET, and a link to it would
		// be a 405 dressed as help.
		for name, mutate := range map[string]func(*fakeCheckins, url.Values){
			"stale or tampered context": func(_ *fakeCheckins, f url.Values) { f.Set("ctx", "not-a-context") },
			"unknown tag":               func(s *fakeCheckins, _ url.Values) { s.err = checkin.ErrUnknownTag },
			"write failed":              func(s *fakeCheckins, _ url.Values) { s.err = errTapProbe },
			"invalid request":           func(s *fakeCheckins, _ url.Values) { s.err = checkin.ErrInvalidRequest },
			"foreign tenant": func(s *fakeCheckins, _ url.Values) {
				s.result = checkin.Result{Outcome: checkin.OutcomeForeignTenant}
			},
			"bad occurred_at": func(_ *fakeCheckins, f url.Values) { f.Set("occurred_at", "yesterday") },
		} {
			t.Run(name, func(t *testing.T) {
				svc := &fakeCheckins{}
				h, tp := newCheckinHandler(t, svc, fixedSessions())
				form := url.Values{"ctx": {mintedContext(t, tp, tapFixedSessionID, tapContext{
					UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true,
					TagTenantID: testTenant, LocationID: tapLocation,
				})}}
				mutate(svc, form)

				body := postForm(t, h, form, sessionCookie()).Body.String()
				if strings.Contains(body, "Try again") {
					t.Fatalf("%s offered a retry on a POST screen", name)
				}
				if strings.Contains(body, `href="/api/checkin"`) {
					t.Fatalf("%s linked back to the POST endpoint", name)
				}
			})
		}
	})

	t.Run("no shared problem view carries a retry of its own", func(t *testing.T) {
		// The retry is attached PER REQUEST, by renderRetryableProblem, and never
		// baked into a shared constant — otherwise the 429 screen would inherit one,
		// and a retry button on a rate limit invites spending the very budget the
		// page is asking the caller to stop spending.
		for _, v := range []pages.ProblemView{
			tapProblemBadURL, tapProblemUnknownTag, tapProblemServer,
			tapProblemTooMany, tapProblemStale, tapProblemForeignTenant,
		} {
			if v.RetryURL != "" {
				t.Fatalf("a package-level problem view carries a retry url: %q", v.Title)
			}
		}
	})

	t.Run("the 429 body offers no retry", func(t *testing.T) {
		w := httptestRecorder()
		h, tp := newTapHandler(t, &fakePreviewer{preview: okPreview(true)},
			&fakeDirectory{facts: okFacts()}, fixedSessions())
		_ = h
		tp.renderTooManyRequests(w, getRequest(tapURL()))
		if strings.Contains(w.Body.String(), "Try again") {
			t.Fatal("the rate-limit screen offers a button that spends the budget it is rationing")
		}
	})

	t.Run("renderRetryableProblem refuses to build a link on a POST", func(t *testing.T) {
		// The structural guard, exercised directly: r.URL.RequestURI() on a POST is
		// /api/checkin, and a link to it would be a 405 dressed as help.
		_, tp := newTapHandler(t, &fakePreviewer{preview: okPreview(true)},
			&fakeDirectory{facts: okFacts()}, fixedSessions())
		w := httptestRecorder()
		tp.renderRetryableProblem(w, postRequest("/api/checkin"), http.StatusInternalServerError, tapProblemServer)
		if strings.Contains(w.Body.String(), "Try again") {
			t.Fatal("a POST was given a retry link")
		}
	})
}

// ---------------------------------------------------------------------------
// The one thing the ignored screen may never say.
// ---------------------------------------------------------------------------

// screenText is what this screen puts in front of a person, with the markup taken
// out — and its LIMITS are stated here rather than left to be discovered, because
// three rounds of this task were failed by a comment claiming more than its code
// delivered.
//
// WHAT IT SEES:
//   - every text node in the WHOLE DOCUMENT, which is the <body> AND the <head>'s
//     <title>. Not just <main>: an audit added a paragraph to layout/base.templ,
//     one line below </main>, where it renders on every screen in the product,
//     and a <main>-scoped comparison did not notice.
//   - the value of every attribute ON textAttrRE'S LIST — not "every text-bearing
//     attribute", which is what an earlier version of this comment claimed and
//     could not deliver: the list had missed `value` and aria-roledescription, and
//     an audit walked a sentence through both. The list is the contract and it is
//     written out at textAttrRE. It is NOT backed by anything that makes it safe
//     to be incomplete — an earlier version of this sentence said it was, naming a
//     "TestScreens_CarryNoFormControls" that no longer exists (it became
//     TestScreens_RenderOnlyTheseElements) and quoting that test's original
//     description, which this same file documents as MEASURED FALSE a few hundred
//     lines below. What actually exists is a neighbouring whitelist of ELEMENTS;
//     it narrows where an attribute can live, and it does not make this list
//     complete. These values are read aloud by a screen reader or painted on the
//     screen, so they are copy; aria-label is the sharpest of them, because it
//     REPLACES the element's text for a screen reader rather than adding to it.
//
// WHAT IT DOES NOT SEE, stated as LIMITS and not defended as safe:
//   - TEXT GENERATED BY CSS. `content:` on a ::before/::after puts real words on
//     the screen and no amount of HTML reading finds them; an audit compiled
//     `after:content-['Your_earlier_tap_stands.']` into app.css and the suite
//     stayed green. That hole is covered SEPARATELY and partially by
//     TestCompiledCSS_GeneratesNoText, which reads the built stylesheet.
//   - anything a future <script> writes at runtime. There is no script on these
//     screens today (only the tap page has one, and it fills form fields).
//   - <meta> content, <link> and <script> attributes: machine-facing, not read by
//     a person, deliberately out of scope. A measured mutation put a sentence in
//     <meta name="description"> and this comparison did not see it; nothing
//     renders that to a visitor, so it is a stated LIMIT and not a hole that was
//     closed.
//   - TEXT CARRIED BY AN ELEMENT RATHER THAN BY A TEXT NODE — an <iframe srcdoc>,
//     an <object data="data:text/html,…">, an SVG <text> inside an <img>. This
//     function cannot see any of them, and there is no version of it that could
//     without becoming a browser. It is narrowed from the other side, by
//     TestScreens_RenderOnlyTheseElements refusing every element outside the
//     measured set — narrowed, not eliminated: an ALLOWED element carrying text in
//     an attribute nobody reads is exactly how <link href="data:text/css,…"> got
//     through after that set was written.
//
// KNOWN OPEN CHANNELS — kept as a list rather than assumed empty, because nine
// audit rounds of this task each ended by finding one more, and every blocking
// finding was a sentence claiming more than its code delivered. These are NOT
// covered by anything here:
//
//   - <meta name="description"> content. Measured green; it renders to no
//     visitor, which is why it was left rather than closed. (A <meta> carrying
//     http-equiv is a different matter and is refused outright by assertRefs,
//     whatever the attribute order — because a refresh moves the visitor to
//     another origin. That refusal was itself order-dependent for one round, so
//     it is stated as "refused in the writings measured at assertRefs", not as
//     "meta is handled".)
//   - RESPONSE HEADERS. Nothing here reads them, and Tap.render sets four; a
//     fifth, `Refresh: 30;url=…`, produces the meta-refresh effect without
//     touching a template at all, and was measured green. Only the
//     Content-Security-Policy header is asserted anywhere
//     (TestTapResponses_CarryTheContentSecurityPolicy). The URL channel on this
//     surface is therefore NOT "meta refresh" — it is "meta refresh AND any
//     navigational response header nobody is watching".
//   - CSS `content:`, except on a developer machine. TestCompiledCSS_GeneratesNoText
//     reads the built stylesheet, and CI never builds one, so it ALWAYS SKIPS
//     there; a skip is not a pass.
//   - anything a future <script> writes at runtime. No script on these screens
//     today.
//   - the five ACTIVATION failure screens, which share pages.Problem with the six
//     pinned here but are served without a Content-Security-Policy and whose
//     Message strings nothing in this package asserts. That is M5-02's flow and
//     deliberately out of this task's scope.
//   - a hand-edit of the generated *_templ.go files. Nothing regenerates them
//     inside `make test`; `make gen` is a separate step and CI's `make check`
//     catches only the diff, not the intent.
//
// AND ONE LIMIT THAT IS ABOUT COVERAGE RATHER THAN MECHANISM: everything above is
// PER SCREEN. A template added to this package later is covered by neither this
// comparison nor the element set until somebody lists it in both. Nothing enforces
// that; it is written here because it was previously true and unwritten.
//
// MAINTENANCE CONTRACT. If a case fails and you changed the LAYOUT SHELL or
// deliberately reworded a screen, update the `want`. If it fails because a
// sentence appeared that nobody meant to add, that is the test doing its job:
// §9 says ask before adding to this surface, and §4.6 says nothing here may imply
// an approval nothing granted, nor deny a record that exists.
func screenText(t *testing.T, body string) string {
	t.Helper()
	if !strings.Contains(body, "<body") {
		t.Fatal("no <body> in the rendered page")
	}
	// Each tag becomes a space PLUS whatever human-facing text it carries in its
	// attributes, so nothing is glued together and nothing readable is dropped.
	out := tagRE.ReplaceAllStringFunc(body, func(tag string) string {
		var carried []string
		for _, m := range textAttrRE.FindAllStringSubmatch(tag, -1) {
			carried = append(carried, m[2])
		}
		if len(carried) == 0 {
			return " "
		}
		return " " + strings.Join(carried, " ") + " "
	})
	out = strings.NewReplacer("&#34;", `"`, "&#39;", "'", "&amp;", "&", "&lt;", "<", "&gt;", ">").Replace(out)
	return strings.Join(strings.Fields(out), " ")
}

// textAttrRE finds attribute values a person perceives.
//
// 🔴 IT IS A LIST, NOT A RULE, AND THE LIST HAS BEEN WRONG TWICE. The first
// version omitted `value` on the theory that it is "machine-facing" — it is the
// most visible attribute in HTML, a readonly <input> prints it on the screen, and
// an audit used exactly that to put "Your earlier tap stands." back on the ignored
// branch with the whole suite green. The same version omitted
// aria-roledescription, which a screen reader announces in place of the element's
// role. Both are here now, together with the remaining ARIA string-valued
// attributes that get announced.
//
// BECAUSE A LIST IS A RACE, IT IS NOT THE ONLY LAYER — and the second layer had
// to be rebuilt once for exactly the same reason. It was a BLACKLIST of form
// controls, described as "removes the elements whose whole purpose is to display
// an attribute"; that was false, and <iframe srcdoc>, <object data> and an
// <img> holding an SVG <text> each walked a sentence through it.
// TestScreens_RenderOnlyTheseElements is now a CLOSED SET: these two screen
// families may render only the tags measured from their templates, so an element
// nobody has thought of yet fails rather than passes. The attribute list catches
// text smuggled through an attribute of an ALLOWED element; the closed set stops
// a new element arriving to carry it.
//
// Deliberately excluded, and these are machine-facing: href, src, class, id,
// name, type, rel, charset. Pulling those in would make every expectation a copy
// of the markup and would break on a Tailwind class change, which is not a §4.6
// event.
var textAttrRE = regexp.MustCompile(`(?i)\b(title|value|alt|placeholder|` +
	`aria-label|aria-description|aria-roledescription|aria-valuetext|` +
	`aria-placeholder|aria-braillelabel|aria-brailleroledescription)="([^"]*)"`)

var tagRE = regexp.MustCompile(`<[^>]*>`)

// The notes production actually puts on these screens, copied from a measured run
// against real Postgres rather than invented here. TestCheckinDB_ScreenTextIsWhatProductionRenders
// asserts these same strings come out of the real decision engine — if a policy
// reason is reworded, that test fails first and names the new wording.
const (
	noteIPMatched = "network proof of place: the source IP matches the location"
	noteNoPlace   = "neither IP nor GPS could place this tap"
	noteDeadTag   = "this tag is no longer active"
	noteDebounce  = "duplicate tap by the same person within the debounce window"
	// THE TENTH SHAPE. tap.Decide's appendNote joins the deciding rule's reason to
	// a direction annotation with "; ", so a Note is not always one sentence — a
	// check-OUT closing a stale open check-in carries both. The table claimed to
	// cover "every shape the product can render" while missing this one.
	noteStaleOpenIn = "stale open check-in (possible forgotten checkout)"
)

// What the shell contributes to every confirmation screen: the document <title>
// (an audit smuggled a claim into it) and the two words in the header.
const shellText = "Tapped — Tappa tappa punchless"

// problemRetryURL is what renderRetryableProblem puts on the three GET branches
// that offer one: the plaque's own request URI.
const problemRetryURL = "/t?tag=04AC7E55000601&ctr=000641&cmac=83c0104d8ac39077"

// TestResultScreen_SaysExactlyThisAndNothingElse is the whole-screen whitelist,
// over every shape the product can actually render.
//
// 🔴 A NOTE IS PRESENT ON EVERY PRODUCTION SCREEN, and getting that wrong is what
// sank the previous attempt. Decision.Note is the deciding rule's Reason
// (tap/decide.go), every guardrail and baseline rule carries one, and even the
// no-match fallback does — so the earlier golden, built by hand with an empty
// note, pinned a body production never emits and a sentence hidden inside the
// note block sailed straight past it (measured: real ignored <main> was 1061
// bytes against a 971-byte golden). Every row below therefore carries the note
// production would carry.
//
// THE EMPTY-NOTE ROW IS NOT DECORATION EITHER. It is reachable: internal/policy's
// validate.go bounds a tenant rule's Reason to maxReasonLen but never requires one,
// so a tenant-authored rule with no reason renders this shape.
func TestResultScreen_SaysExactlyThisAndNothingElse(t *testing.T) {
	tests := []struct {
		name string
		res  checkin.Result
		want string
	}{
		{
			name: "ok in, restaurant",
			res: checkin.Result{Decision: withNote(decisionOf("ok", "in", false), noteIPMatched),
				BusinessType: "restaurant"},
			want: shellText + " St Julians Tapped in 14:03:22 APPROVED Trust 100 " + noteIPMatched +
				" Have a great shift — keep those kebabs rolling! 🌯 All done — you can close this page.",
		},
		{
			name: "ok out, production",
			res: checkin.Result{Decision: withNote(decisionOf("ok", "out", false), noteIPMatched),
				BusinessType: "production"},
			want: shellText + " St Julians Tapped out 14:03:22 APPROVED Trust 100 " + noteIPMatched +
				" Shift complete. Thank you for your work today! 👋 All done — you can close this page.",
		},
		{
			name: "ok, practice: the training line replaces the send-off",
			res: checkin.Result{Decision: withNote(decisionOf("ok", "in", true), noteIPMatched),
				BusinessType: "restaurant"},
			want: shellText + " St Julians Tapped in 14:03:22 APPROVED TRAINING This practice tap does not" +
				" count toward your hours. Trust 100 " + noteIPMatched + " All done — you can close this page.",
		},
		{
			name: "ok with an empty note (a tenant rule with no reason)",
			res:  checkin.Result{Decision: decisionOf("ok", "in", false), BusinessType: "restaurant"},
			want: shellText + " St Julians Tapped in 14:03:22 APPROVED Trust 100" +
				" Have a great shift — keep those kebabs rolling! 🌯 All done — you can close this page.",
		},
		{
			// §4.6's own branch: recorded, NOT approved, approval not promised, and
			// the appeal left open.
			name: "flag",
			res: checkin.Result{Decision: withNote(decisionOf("flag", "in", false), noteNoPlace),
				BusinessType: "restaurant"},
			want: shellText + " St Julians Tapped in 14:03:22 FLAGGED Trust 100 " + noteNoPlace +
				" Recorded — it needs your manager's approval before it counts toward your hours." +
				" Tell them if that looks wrong. You can close this page.",
		},
		{
			name: "flag, practice: one voice, no 'before it counts'",
			res: checkin.Result{Decision: withNote(decisionOf("flag", "in", true), noteNoPlace),
				BusinessType: "restaurant"},
			want: shellText + " St Julians Tapped in 14:03:22 FLAGGED TRAINING This practice tap does not" +
				" count toward your hours. Trust 100 " + noteNoPlace +
				" Recorded, and your manager will see it. Tell them if that looks wrong. You can close this page.",
		},
		{
			name: "reject: the record exists, the hours did not count",
			res: checkin.Result{Decision: withNote(decisionOf("reject", "", false), noteDeadTag),
				BusinessType: "restaurant"},
			want: shellText + " St Julians Not counted 14:03:22 REJECTED Trust 100 " + noteDeadTag +
				" This tap was recorded but not counted. Tell your manager if that looks wrong." +
				" You can close this page.",
		},
		{
			// THE SHAPE THE PREVIOUS GOLDEN MISSED: the note block is where a
			// smuggled sentence hid.
			name: "ignored: says nothing about the earlier tap",
			res: checkin.Result{Decision: withNote(decisionOf("ignored", "", false), noteDebounce),
				BusinessType: "restaurant"},
			want: shellText + " St Julians Already tapped 14:03:22 IGNORED Trust 100 " + noteDebounce +
				" You tapped a moment ago, so this one was not counted. Tell your manager if that looks" +
				" wrong. You can close this page.",
		},
		{
			// Two sentences in one Note, joined by tap.Decide's appendNote.
			name: "ok out closing a stale check-in: a two-part note",
			res: checkin.Result{
				Decision:     withNote(decisionOf("ok", "out", false), noteIPMatched+"; "+noteStaleOpenIn),
				BusinessType: "restaurant"},
			want: shellText + " St Julians Tapped out 14:03:22 APPROVED Trust 100 " +
				noteIPMatched + "; " + noteStaleOpenIn +
				" Great work today. See you next shift! 👋 All done — you can close this page.",
		},
		{
			// The direction annotation with NO policy reason in front of it: a
			// tenant rule that carries no reason (validate.go does not require one)
			// on a check-out that closes a stale open check-in. appendNote returns
			// the annotation alone, with no "; ".
			name: "ok out, stale check-in, and no rule reason at all",
			res: checkin.Result{Decision: withNote(decisionOf("ok", "out", false), noteStaleOpenIn),
				BusinessType: "production"},
			want: shellText + " St Julians Tapped out 14:03:22 APPROVED Trust 100 " + noteStaleOpenIn +
				" Shift complete. Thank you for your work today! 👋 All done — you can close this page.",
		},
		{
			name: "an unrecognised verdict claims nothing in either direction",
			res: checkin.Result{Decision: withNote(decisionOf("something-new", "in", false), noteNoPlace),
				BusinessType: "restaurant"},
			want: shellText + " St Julians Tapped 14:03:22 RECORDED Trust 100 " + noteNoPlace +
				" This tap was recorded but not counted. Tell your manager if that looks wrong." +
				" You can close this page.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := tc.res
			res.Outcome = checkin.OutcomeRecorded
			res.LocationName = "St Julians"
			res.Timezone = "Europe/Malta"
			res.OccurredAt = mustTime(t, "2026-07-31T12:03:22Z")

			got := screenText(t, mustBody(t, res))
			if got != tc.want {
				t.Fatalf("this screen no longer says exactly what it is allowed to say.\n got: %s\nwant: %s\n\n"+
					"A SENTENCE that nobody meant to add is what this test exists to stop (§9: ask before "+
					"adding to this surface; §4.6: nothing here may imply an approval nothing granted, nor "+
					"deny a record that exists). If the wording or the shell changed on purpose, update want.",
					got, tc.want)
			}
		})
	}
}

// withNote attaches the deciding rule's sentence, the way tap.Decide does.
func withNote(d tap.Decision, note string) tap.Decision { d.Note = note; return d }

// mustBody renders one result through the production router.
func mustBody(t *testing.T, res checkin.Result) string {
	t.Helper()
	body, code := resultBody(t, res, nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	return body
}

// TestResultScreen_UnknownVerdictIsTreatedAsNotCounted drives the branch nothing
// else reaches. A verdict this screen does not recognise must fall to the
// CONSERVATIVE answer (§4.6): a stamp that claims nothing, and copy that does not
// say the hours were counted. The deleted ResultView.Stamp() had the same
// fallback and nothing pinned it.
func TestResultScreen_UnknownVerdictIsTreatedAsNotCounted(t *testing.T) {
	body, _ := resultBody(t, checkin.Result{
		Outcome:      checkin.OutcomeRecorded,
		Decision:     decisionOf("something-new", "in", false),
		LocationName: "St Julians",
		BusinessType: "restaurant",
	}, nil)

	if !strings.Contains(body, `class="stamp stamp--ignored">RECORDED`) {
		t.Fatal("an unrecognised verdict did not fall back to the neutral RECORDED stamp")
	}
	for _, no := range []string{"All done", "APPROVED", "stamp--approved", "kebabs rolling"} {
		if strings.Contains(body, no) {
			t.Fatalf("an unrecognised verdict rendered %q: the fallback must claim nothing", no)
		}
	}
	if !strings.Contains(body, "recorded but not counted") {
		t.Fatal("the fallback copy does not say the tap was not counted")
	}
}

// TestResultScreen_PracticeAndFlagSpeakWithOneVoice. tap.Decide sets Practice on
// the same ok/flag gate that sets direction, so a training tap with no evidence
// is a real combination. The docket already says a practice tap never counts
// toward hours; the flag line must not then say a manager will confirm it
// "before it counts", because approval changes nothing about a practice tap's
// hours and the two sentences would pull in opposite directions.
func TestResultScreen_PracticeAndFlagSpeakWithOneVoice(t *testing.T) {
	body, _ := resultBody(t, checkin.Result{
		Outcome:      checkin.OutcomeRecorded,
		Decision:     decisionOf("flag", "in", true),
		LocationName: "St Julians",
		BusinessType: "restaurant",
	}, nil)

	if !strings.Contains(body, "does not count toward your hours") {
		t.Fatal("a practice tap lost the line that says it never counts")
	}
	if strings.Contains(body, "before it counts") {
		t.Fatal("a practice flag says a manager will confirm it 'before it counts', contradicting the training line")
	}
	if !strings.Contains(body, "your manager will see it") {
		t.Fatal("a practice flag no longer says a manager will look at it (§4.6: not silently approved)")
	}
	if strings.Contains(body, "All done") {
		t.Fatal("a flagged tap read as finished")
	}
}

// TestResultScreen_BrandMessageNeverGoesSilent. Both switches in brandMessage
// have a default, and this is why: an ok whose direction or business type is a
// value nobody anticipated must still say something. Unreachable today —
// tap.Decide gives every ok a direction — which is exactly why it is pinned
// rather than trusted.
func TestResultScreen_BrandMessageNeverGoesSilent(t *testing.T) {
	body, _ := resultBody(t, checkin.Result{
		Outcome:      checkin.OutcomeRecorded,
		Decision:     decisionOf("ok", "", false), // counted, no direction
		LocationName: "St Julians",
		BusinessType: "restaurant",
	}, nil)

	if !strings.Contains(body, "Thanks — have a good one!") {
		t.Fatal("an ok with no direction rendered no send-off at all")
	}
	for _, no := range []string{"kebabs rolling", "See you next shift"} {
		if strings.Contains(body, no) {
			t.Fatalf("a directionless tap was given the %q line, which says which way they were going", no)
		}
	}
}

// TestResultView_FieldCountIsTheSpec turns view.go's "EIGHT FIELDS, AND THE LIST
// IS THE SPEC" from prose into a tripwire.
//
// A number in a comment catches a smuggled field only if somebody re-counts, and
// this one was WRONG inside a single task (it said seven while the struct carried
// eight). §9 calls the tap surface sacred and the rule is to ask before adding to
// it; a view model with a field cannot help rendering it, so the field list is
// where that rule is enforceable. Changing this number is fine — changing it
// without that conversation is what this fails on.
func TestResultView_FieldCountIsTheSpec(t *testing.T) {
	const want = 8
	got := reflect.TypeOf(pages.ResultView{}).NumField()
	if got != want {
		t.Fatalf("ResultView has %d fields, want %d.\n"+
			"Adding one is adding a feature to the screen CLAUDE.md §9 calls sacred: ask first, "+
			"then update this number AND the field-list comment in web/templates/pages/view.go.", got, want)
	}
}

// ---------------------------------------------------------------------------
// The headline: the biggest text on the page, and the easiest place to lie.
// ---------------------------------------------------------------------------

// TestResultScreen_NoVerdictDeniesTheRecord is §4.6 measured where it is loudest.
//
// 🔴 THE DEFECT THIS PINS WAS SHIPPED AND SURVIVED TWO ROUNDS. The `reject`
// headline read "Not recorded" in 48px type, four lines above "This tap WAS
// recorded but not counted" — the page contradicting itself, with the false half
// in the larger font. It is false by measurement, not by opinion:
// internal/domain/checkin returns an ERROR when the insert fails and only builds
// OutcomeRecorded afterwards, so a rendered Result page is itself proof that a
// `transactions` row exists.
//
// WHY IT MATTERS MORE THAN A WORDING SLIP: §5 rows 1, 2 and 4 — retired or lost
// plaque, invalid SUN or replay, deactivated account — are ALL `reject`. §4.6
// gives the person exactly one thing in those cases, a record they can dispute,
// and a headline saying there is no record withdraws it in the largest type on
// the screen.
//
// The previous round's tests could not catch it: the reject row pinned the
// closing sentence ("recorded but not counted") and nothing anywhere asserted or
// forbade the headline — grep for "Not recorded" matched one file, view.go, and
// no test at all.
func TestResultScreen_NoVerdictDeniesTheRecord(t *testing.T) {
	// Every phrasing that would tell somebody their tap left no trace. This IS a
	// blacklist, and a blacklist cannot prove the absence of a meaning — the known
	// limits are listed at screenText. It is the cheap second layer under the exact
	// headline assertions below, which are the real wire. (This comment used to
	// point at "ignoredGolden", a hand-built byte golden that was removed two
	// rounds later precisely because it pinned a body production never renders.)
	denials := []string{"Not recorded", "not recorded", "Nothing was recorded", "No record"}

	for _, verdict := range []string{"ok", "flag", "reject", "ignored", "something-new"} {
		t.Run(verdict, func(t *testing.T) {
			body, _ := resultBody(t, checkin.Result{
				Outcome:      checkin.OutcomeRecorded,
				Decision:     decisionOf(verdict, "in", false),
				LocationName: "St Julians",
				BusinessType: "restaurant",
			}, nil)
			for _, denial := range denials {
				if strings.Contains(body, denial) {
					t.Fatalf("verdict %q renders %q, but reaching this screen at all means the row was "+
						"INSERTED (§4.6: the record is the one thing a rejected tap still gets)", verdict, denial)
				}
			}
		})
	}
}

// TestResultScreen_HeadlineMatchesTheVerdict pins the exact word for every
// verdict, so the headline cannot drift back into disagreeing with the sentence
// four lines under it.
func TestResultScreen_HeadlineMatchesTheVerdict(t *testing.T) {
	tests := []struct {
		name      string
		verdict   string
		direction string
		want      string
	}{
		{"ok in", "ok", "in", "Tapped in"},
		{"ok out", "ok", "out", "Tapped out"},
		{"flag keeps its direction", "flag", "in", "Tapped in"},
		{"ok with no direction is neutral", "ok", "", "Tapped"},
		{"ignored says a tap happened, nothing more", "ignored", "", "Already tapped"},
		// THE ONE THIS TEST EXISTS FOR: the hours were refused, the record was not.
		{"reject says the hours did not count", "reject", "", "Not counted"},
		// An unrecognised verdict used to fall through to the DIRECTION arms and
		// render "Tapped in" — a counted-looking headline under a RECORDED stamp.
		{"an unknown verdict is neutral, not directional", "something-new", "in", "Tapped"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := resultBody(t, checkin.Result{
				Outcome:      checkin.OutcomeRecorded,
				Decision:     decisionOf(tc.verdict, tc.direction, false),
				LocationName: "St Julians",
				BusinessType: "restaurant",
			}, nil)
			// The trailing <span> is part of the assertion ON PURPOSE: it is the
			// boundary. Without it "Tapped " is a PREFIX of "Tapped in ", so a
			// mutation that made an unknown verdict directional again passed this
			// test (measured — it stayed green until this line was tightened).
			want := `tracking-tight text-ink">` + tc.want + ` <span class="block font-mono`
			if !strings.Contains(body, want) {
				t.Fatalf("verdict %q direction %q: headline is not exactly %q", tc.verdict, tc.direction, tc.want)
			}
		})
	}
}

// TestTapPage_EveryGetServerErrorBranchIsAccountedFor makes tap.go's enumeration
// of the GET-path 500s checkable instead of countable-by-eye.
//
// The comment there listed FOUR branches when the file has FIVE — it silently
// dropped the unknown-session-state one — which is the same failure mode as a
// field-count comment that stopped matching its struct. This drives four of the
// five through the router or the handler and asserts which of them carry a link.
//
// LIMIT, stated: the fifth (unknown session state) is NOT driven. httpx.Identity's
// State is set by the middleware from a closed set, so a test cannot hand the
// handler a value outside it without changing production code. Its retry-less
// branch is verified by reading, not by running.
func TestTapPage_EveryGetServerErrorBranchIsAccountedFor(t *testing.T) {
	const link = "Try again"

	t.Run("identity unresolved: RETRY", func(t *testing.T) {
		// The zero Identity — the handler called with no Identify in front of it.
		_, tp := newTapHandler(t, &fakePreviewer{preview: okPreview(true)},
			&fakeDirectory{facts: okFacts()}, fixedSessions())
		w := httptestRecorder()
		tp.Page(w, getRequest(tapURL()))
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", w.Code)
		}
		if !strings.Contains(w.Body.String(), link) {
			t.Fatal("identity-unresolved offers no retry; through Mount this state means the lookup FAILED")
		}
	})

	t.Run("sun pre-check: RETRY", func(t *testing.T) {
		h, _ := newTapHandler(t, &fakePreviewer{err: errTapProbe}, &fakeDirectory{facts: okFacts()}, fixedSessions())
		if !strings.Contains(get(t, h, tapURL(), sessionCookie()).Body.String(), link) {
			t.Fatal("the sun pre-check branch offers no retry")
		}
	})

	t.Run("directory read: RETRY", func(t *testing.T) {
		h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)},
			&fakeDirectory{err: errTapProbe}, fixedSessions())
		w := get(t, h, tapURL(), sessionCookie())
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", w.Code)
		}
		if !strings.Contains(w.Body.String(), link) {
			t.Fatal("the directory branch offers no retry; its errors are query errors")
		}
	})

	t.Run("mint: NO retry", func(t *testing.T) {
		sess := &fakeSessions{verify: func() (session.Resolved, error) {
			return session.Resolved{ID: uuid.Nil, TenantID: testTenant, EmployeeID: testEmployee}, nil
		}}
		h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: okFacts()}, sess)
		if strings.Contains(get(t, h, tapURL(), sessionCookie()).Body.String(), link) {
			t.Fatal("the mint branch offers a retry, and mint fails on two deterministic conditions")
		}
	})
}

// ---------------------------------------------------------------------------
// The failure screens, held to the same discipline.
// ---------------------------------------------------------------------------

// TestProblemScreens_SayExactlyThisAndNothingElse pins the complete text of all
// six tap failure screens.
//
// 🔴 THE WHOLE FAMILY WAS UNPROTECTED, and an audit measured what that allowed:
// a sentence appended to tapProblemTooMany.Message, another to
// tapProblemStale.Message, and a plain paragraph added to the SHARED
// pages.Problem template — which renders on all six at once — and the entire
// suite stayed green (13 ok, 0 failing). Nothing had ever asserted a Message
// string; only the presence or absence of "Try again" and three Hints.
//
// THAT MATTERS BECAUSE FIVE OF THE SIX MAKE A CLAIM ABOUT THE RECORD, which is
// §4.6 territory on the same page as the confirmation screen. The audit's first
// mutation was a good example of the failure mode: "Nothing was recorded and
// nothing was logged" is FALSE — a rate-limited request with a resolved session
// writes an audit_log row (internal/httpx's limiter, wired in NewTap). Every
// string below was traced to the code path it describes and is true today; this
// test is what keeps it that way.
//
// The screens are rendered through pages.Problem directly rather than through
// six handler paths, because the constants ARE the copy — the handler's job is
// only to pick one, and which one it picks is asserted in
// TestProblemScreen_RetryOnlyWhereRefetchingCanHelp.
//
// 🔴 BOTH RETRY VARIANTS, and the first version of this test rendered only one of
// them. Every case here passed RetryURL == "", while renderRetryableProblem serves
// three GET branches with it FILLED — so the shared template's
// `if v.RetryURL != ""` block, which real users see on those three screens, was
// rendered by the element set and the reference set (both loop over the variant)
// and by no whole-text assertion at all. An audit put a paragraph inside that
// block carrying two sentences this repo documents as false, and the suite stayed
// green. The anchor's own words are appended to each case's `want` in the retry
// pass below, for the same reason: "Try again" had only ever been checked with
// strings.Contains, so "Try again — your earlier tap stands" would have passed.
// problemScreenShell is what the layout contributes to a failure screen: the
// document <title> and the two header words.
const problemScreenShell = "Tappa tappa punchless"

// problemScreens is the expected text of each failure screen, as LITERALS.
//
// It lives at package level because TWO tests read it — the component-level one
// below and the router-level one beside it — and one literal table read twice is
// the only shape that keeps them agreeing. It is deliberately NOT derived from
// the pages.ProblemView fields: a `want` computed from view.Message would move
// with any edit to view.Message, which is precisely the mutation class this table
// exists to catch (an audit appended a false sentence to two of these constants).
var problemScreens = []struct {
	name string
	view pages.ProblemView
	want string
}{
	{
		name: "bad url", view: tapProblemBadURL,
		want: problemScreenShell + " That link didn't work This page opens when you hold your phone to the" +
			" plaque at the door. Try tapping the plaque again.",
	},
	{
		name: "unknown plaque", view: tapProblemUnknownTag,
		want: problemScreenShell + " We don't know that plaque This plaque isn't set up with Tappa, or it has" +
			" been replaced. Tell your manager which door you tapped.",
	},
	{
		// "Nothing was recorded" is TRUE on both paths that use it: GET /t writes
		// no transactions row at all, and on POST the domain returns an error
		// only when the insert did not happen.
		name: "server error", view: tapProblemServer,
		want: problemScreenShell + " Something went wrong on our side Nothing was recorded. This is not your" +
			" fault and it is not your time. Tap the plaque again in a moment.",
	},
	{
		// Deliberately silent about records: a refused request MAY still have
		// left an audit_log row, so this screen does not claim otherwise.
		name: "rate limited", view: tapProblemTooMany,
		want: problemScreenShell + " Too many taps from here We have paused requests from this network for a" +
			" few minutes. Wait a moment and tap the plaque again. Tell your manager if it keeps happening.",
	},
	{
		name: "stale context", view: tapProblemStale,
		want: problemScreenShell + " That tap has expired Nothing was recorded. Tap pages are only good for a" +
			" few minutes. Hold your phone to the plaque again.",
	},
	{
		name: "another employer's plaque", view: tapProblemForeignTenant,
		want: problemScreenShell + " That plaque isn't yours This plaque belongs to a different employer, so" +
			" nothing was recorded. Use your own workplace's plaque, or tell your manager.",
	},
}

func TestProblemScreens_SayExactlyThisAndNothingElse(t *testing.T) {
	for _, tc := range problemScreens {
		for _, retry := range []string{"", problemRetryURL} {
			name, want := tc.name, tc.want
			if retry != "" {
				// The three GET branches that offer a retry render this instead.
				name += " (with the retry link)"
				want += " Try again"
			}
			t.Run(name, func(t *testing.T) {
				view := tc.view
				view.RetryURL = retry
				var sb strings.Builder
				if err := pages.Problem(view).Render(context.Background(), &sb); err != nil {
					t.Fatalf("render: %v", err)
				}
				if got := screenText(t, sb.String()); got != want {
					t.Fatalf("failure screen text changed.\n got: %s\nwant: %s\n\n"+
						"These sentences make claims about whether a record exists (§4.6). If you changed "+
						"one on purpose, check the claim is still TRUE of the path that renders it, then "+
						"update want.", got, want)
				}
			})
		}
	}
}

// TestCompiledCSS_GeneratesNoText is the narrow cover for the one channel
// screenText structurally cannot see.
//
// CSS puts real words on a screen: `content: "..."` on a ::before or ::after
// renders text no HTML reader will ever find. An audit added the Tailwind
// arbitrary value after:content-['Your_earlier_tap_stands.'] to the ignored
// paragraph, confirmed it compiled into app.css, and watched the whole suite stay
// green. So the stylesheet is read directly and any `content:` carrying letters
// or digits is refused.
//
// TWO LIMITS, both real:
//
//   - 🔴 IT NEVER RUNS IN CI, AND THAT IS MEASURED, NOT SUSPECTED.
//     .github/workflows/ci.yml runs exactly `make tools`, `make up`, `make check`
//     and `make audit`; none of those builds CSS, and .gitignore line 16 keeps
//     /web/static/css/app.css out of the checkout. So on CI this test ALWAYS
//     SKIPS, and the CSS channel is guarded only on a developer machine after a
//     manual `make css`. A skip is not a pass — it is the check having nothing to
//     read. Adding a `make css` step to CI would fix it and is not this task's to
//     make.
//   - IT IS A SCAN, NOT A PROOF. Decorative glyphs are allowed through, so a
//     single letter dressed as decoration would pass. It closes the mutation that
//     was measured, not the category.
func TestCompiledCSS_GeneratesNoText(t *testing.T) {
	raw, err := fs.ReadFile(web.Static(), "css/app.css")
	if err != nil {
		t.Skipf("no compiled stylesheet to read (%v) — run `make css`. THIS IS NOT A PASS.", err)
	}
	// Only real `content:` declarations: the leading delimiter keeps this off
	// justify-content and align-content.
	decls := regexp.MustCompile(`[;{]content:([^;}]*)`).FindAllStringSubmatch(string(raw), -1)
	if len(decls) == 0 {
		t.Fatal("no content: declaration found at all — .docket's perforations use two, " +
			"so this scan is reading the wrong file and would pass on anything")
	}
	hasWord := regexp.MustCompile(`[A-Za-z0-9]`)
	for _, d := range decls {
		if hasWord.MatchString(d[1]) {
			t.Fatalf("compiled CSS generates TEXT: content:%s\n"+
				"Words on the screen must come from a template, where the whole-text tests can see "+
				"them (§4.6: an added sentence is a claim, wherever it is written).", d[1])
		}
	}
}

// TestCompiledCSS_StampWordIsInk pins the brand decision of 2026-08-01: the
// stamp's WORD is `ink` and the status COLOUR is the frame.
//
// 🔴 WHY IT EXISTS: THE CONTRAST WAS PROTECTED BY NOTHING, and that was measured
// rather than suspected. Putting the word back on the status colour — the exact
// regression this decision reverses — left the whole suite GREEN: 1158 PASS, 0
// FAIL, measured with the mutation applied and BEFORE this test was written (with
// it, the same mutation produces one FAIL line, which is this test). Every stamp
// assertion in this file reads the
// HTML, and the HTML did not change: the class names and the words are identical
// either way, so the only place the difference exists is the compiled stylesheet.
//
// WHAT IT READS, stated as what the code asks rather than as what one would want
// it to ask, and it is deliberately not a cascade simulation: every rule whose
// SELECTOR TEXT CONTAINS `.stamp` must, if it declares `color:` at all, declare
// ink. Plus at least one such declaration, so the word has a colour rather than
// inheriting one. Everything that phrasing leaves out is LIMIT 5.
//
// 🔴 THAT SHAPE IS THE SECOND VERSION, AND THE FIRST ONE HAD TWO MEASURED HOLES.
// It classified selectors by their SHAPE — `== ".stamp"` or the prefix
// `.stamp--` — and an audit walked saffron back onto the word twice with the test
// green: `.stamp.stamp--flagged{color:…}` is a compound selector that is neither,
// and a SECOND `.stamp{color:…}` after the base rule passed because the check only
// asked whether SOME `.stamp` rule set ink. Both are refused now.
//
// ⚠️ AND THE SENTENCE THAT USED TO STAND HERE IS RETRACTED. It said the new shape
// "has no shape to dodge" and that the over-approximation below produces "a false
// RED, never a false green". BOTH WERE FALSE, and a second audit produced three
// false greens against them — they are LIMIT 5. What this check actually asks is
// narrower than what that sentence promised: it asks about rules whose selector
// TEXT CONTAINS `.stamp`, not about every rule that can MATCH a stamp.
//
// It is still an over-approximation in the other direction — `.stamp + p{color:…}`
// would be refused though it colours a sibling — and that half is only a false RED.
// The claim that is gone is the one about the other half.
//
// Before the 2026-08-01 change every modifier carried a `color:`, and against the
// docket's bg-paper #FFFDF4 that put stamp--ignored (line #C9D2C8) at 1.52:1 and
// stamp--flagged (saffron #D98E2B) at 2.62:1 where AA asks 4.5:1 of 11px bold.
//
// FIVE LIMITS, none of them papered over:
//
//   - 🔴 IT NEVER RUNS IN CI, for the same measured reason as
//     TestCompiledCSS_GeneratesNoText above: .github/workflows/ci.yml runs `make
//     tools`, `make up`, `make check` and `make audit`, none of which builds CSS,
//     and .gitignore line 16 keeps app.css out of the checkout. So this SKIPS on
//     CI, always. A skip is not a pass — it is the check having nothing to read.
//
//   - IT READS A DECLARED COLOUR, NOT A RENDERED PIXEL. An ancestor `opacity`, a
//     `filter`, or a ground darker than paper would all change the contrast a
//     person actually gets, and none of them is visible to a text scan of the
//     stylesheet. The measured ratios live in the comment at .stamp in input.css.
//
//   - IT KNOWS ONE SPELLING OF ink. Tailwind writes `rgb(21 34 25/…)`; a change
//     in the generator's output format would fail this test without anything
//     being wrong. That is a loud failure rather than a silent pass, which is the
//     right way round, but it is a maintenance cost and not a free check.
//
//   - IT READS THE STYLESHEET, SO IT CANNOT SEE WHETHER A TEMPLATE STILL USES A
//     CLASS. A modifier can survive in app.css purely because its name appears in
//     PROSE in a scanned .templ file — measured: `stamp--approved` occurs twice in
//     result.templ, once in a class attribute and once inside a comment. The other
//     half is TestResultScreen_StampCarriesTheWordAndTheBrandClass, which asserts
//     the class reaches the HTML.
//
//   - 🔴 A RULE CAN RECOLOUR A STAMP WITHOUT THIS SCAN SEEING IT. Four were
//     measured, each leaving the WHOLE SUITE GREEN:
//
//     1. `class="stamp stamp--training text-saffron"` — an ordinary utility in
//     the MARKUP, which is the normal way colour is added in this repo.
//     .text-saffron sits in the utilities layer at equal specificity and later
//     in the file, so it wins; the word rendered at 2.15:1. CLOSED, but on the
//     OTHER side: TestResultScreen_PracticeIsStampedAndSaysItDoesNotCount pins
//     the exact class string, the way the four verdict stamps already were. It
//     is not closed here, and the same trick through a class this file does not
//     pin would be invisible again.
//     2. `.docket * { color: … }` — equal specificity, later in the file. OPEN.
//     3. `span[class~=stamp] { color: … }` — HIGHER specificity, and the
//     minifier drops the quotes, so the selector does not even contain the
//     text `.stamp`. OPEN.
//     4. `.stamp--flagged { @apply text-opacity-10; }` — this one DOES name the
//     class, and still walks past: it compiles to
//     `.stamp--flagged{--tw-text-opacity:0.1}` at byte 7785, after the base
//     rule at 5956, and changes NO `color:` declaration at all. The base
//     rule's string is still `color:rgb(21 34 25/var(--tw-text-opacity,1))`,
//     so the scan reads ink and passes while FLAGGED renders at 1.22:1 —
//     WORSE than the 2.62:1 this whole change started from. OPEN. It belongs
//     to the second limit above as much as to this one: the declared colour is
//     ink, the rendered pixel is not.
//
//     2, 3 and 4 are OPEN. Closing them needs the cascade resolved over
//     arbitrary selectors and Tailwind's own variables interpolated — that is a
//     browser, not a scan — so they are listed rather than defended.
//
// NOTE WHAT IS DELIBERATELY NOT PINNED: `opacity` on .stamp. Removing it was a
// judgement about the FRAME (the colour carrier since this change), not about the
// word, and pinning it would freeze a brand choice this test has no accessibility
// grounds to freeze.
//
// ⚠️ THE REASON THAT USED TO BE GIVEN FOR THAT WAS TOO NARROW: "measured, ink at
// .8 over paper is 8.54:1 and still clears AA". True of .8, and it says nothing
// about any other value — at .1 the same arithmetic gives 1.22:1 (escape 4 in
// LIMIT 5, measured). So the honest statement is not "opacity is safe here"; it is
// that NOTHING IN THIS FILE CONSTRAINS OPACITY AT ALL, deliberately, and the
// contrast that results is carried by the measurements in input.css and by review.
func TestCompiledCSS_StampWordIsInk(t *testing.T) {
	raw, err := fs.ReadFile(web.Static(), "css/app.css")
	if err != nil {
		t.Skipf("no compiled stylesheet to read (%v) — run `make css`. THIS IS NOT A PASS.", err)
	}
	// ink #152219 as the Tailwind standalone CLI writes it.
	const inkColor = "rgb(21 34 25"

	// One flat rule: a selector list, then declarations with no nested braces. It
	// matches rules inside an @media block too, because the inner rule is flat.
	ruleRE := regexp.MustCompile(`([^{}]*)\{([^{}]*)\}`)
	// "This selector names a class called stamp, or one whose name starts with
	// stamp-". \b keeps `.stamped` out; `.stamp.stamp--flagged`, `.docket .stamp`
	// and `.stamp--flagged` are all in, which is the point — see the comment above.
	stampSelRE := regexp.MustCompile(`\.stamp\b`)
	// The VALUE of each `color:` declaration. The leading delimiter is what keeps
	// this off background-color and border-color, which are supposed to carry the
	// status colour.
	colorValRE := regexp.MustCompile(`(?:^|;)color:([^;]*)`)
	modifierRE := regexp.MustCompile(`\.stamp--([a-z][a-z0-9-]*)`)

	inkDecls := 0
	modifiers := map[string]bool{}
	for _, m := range ruleRE.FindAllStringSubmatch(string(raw), -1) {
		selectors, decls := m[1], m[2]
		for _, mod := range modifierRE.FindAllStringSubmatch(selectors, -1) {
			modifiers[mod[1]] = true
		}
		if !stampSelRE.MatchString(selectors) {
			continue
		}
		for _, c := range colorValRE.FindAllStringSubmatch(decls, -1) {
			if !strings.Contains(c[1], inkColor) {
				t.Fatalf("a rule whose selector names a stamp sets the WORD's colour to "+
					"something other than ink:\n  %s{%s}\n"+
					"The status colour belongs to the frame and the ground (border-color, "+
					"background-color); the word is ink. That is how stamp--ignored ended up at "+
					"1.52:1 and stamp--flagged at 2.62:1 against bg-paper, where AA asks 4.5:1 "+
					"of 11px bold (user decision, 2026-08-01).", strings.TrimSpace(selectors), decls)
			}
			inkDecls++
		}
	}
	if inkDecls == 0 {
		t.Fatalf("no rule matching a stamp sets %q, so the word's colour is whatever it "+
			"inherits.\nThe word is what carries the verdict to a sighted user; it is ink on "+
			"purpose.", inkColor)
	}
	// DISTINCT names, not occurrences. Counting occurrences left slack — there are
	// nine `.stamp--*` selectors in a fresh build for five modifiers, so a threshold
	// of five passed with two whole modifiers missing (measured). Absence is the
	// failure this guards: when the class names lived in Go, Tailwind never saw
	// them, none of these rules compiled and every stamp rendered bare
	// (result.templ's comment carries that history).
	for _, want := range []string{"approved", "flagged", "rejected", "ignored", "training"} {
		if !modifiers[want] {
			t.Fatalf("the compiled CSS carries no .stamp--%s rule (found: %v).\n"+
				"A stamp whose modifier did not compile renders with no frame, no ground and no "+
				"tilt — the brand's fixed status-to-colour mapping silently gone.", want, sortedKeys(modifiers))
		}
	}
}

// sortedKeys renders a set for an error message, in a stable order.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestScreens_RenderOnlyTheseElements is the structural half of the copy defence,
// and it is a CLOSED SET rather than a growing list of things to forbid.
//
// 🔴 WHY A BLACKLIST WAS THE WRONG SHAPE, measured across two rounds. The first
// version forbade form controls — <input>, <textarea>, <select>, <button>,
// <form> — on the theory that it "removes the elements whose whole purpose is to
// display an attribute". That claim was false, and an audit spent one round
// proving it: <iframe srcdoc="…">, <object data="data:text/html,…"> and
// <img src="data:image/svg+xml,…<text>…"> each put a sentence on the screen with
// the entire suite green, and the fourth put "Nothing was recorded and nothing
// was logged" — a sentence this repo documents as FALSE — into the SHARED Problem
// template, where it reaches all eleven failure screens.
//
// ⚠️ AND THE FIRST VERSION OF THIS PARAGRAPH GOT THE THREAT MODEL BACKWARDS. It
// said "there is no CSP on these responses (only tap.go's page sets one)", which
// is wrong: handler.Tap.render sets Content-Security-Policy on EVERY response it
// writes — pages.Tap, pages.Result and pages.Problem alike — and tapCSP's
// default-src 'none' blocks every one of those four elements in a browser. The
// five ACTIVATION failure screens are the ones served without a policy
// (Activation.render), and the shared template means a sentence added here lands
// on both sets. So the tests below are what protect the ELEVEN screens uniformly;
// the header protects six of them and is itself pinned by
// TestTapResponses_CarryTheContentSecurityPolicy.
//
// A blacklist can always be beaten by one more element; there is no version of it
// that is done. So the assertion is inverted: these two screen families are small
// and their markup is small, and the test asserts the rendered tag set EQUALS the
// set measured from the templates. Adding ANY element — a frame, an
// image, an object, a control, or something invented later — fails. Removing one
// fails too, which is deliberate: it keeps the list from accumulating entries
// nobody renders any more.
//
// WHAT THIS DOES AND DOES NOT GIVE:
//   - it refuses any ELEMENT outside the measured set, so an element nobody has
//     thought of fails rather than passes. It does NOT stop "text reaching the
//     screen": an ALLOWED element can still carry text in an attribute this file
//     does not read, which is exactly what <link href="data:text/css,…{content:'…'}">
//     did after this set was written, and what <meta http-equiv="refresh"> did
//     after the reference set was written. Each took another narrow test. The
//     honest summary is that three narrow whitelists — text, elements, references
//     — together cover more than any one of them, and that the list of KNOWN OPEN
//     CHANNELS is kept at screenText rather than assumed empty;
//   - it says NOTHING about a screen that is not listed below. A new template
//     added to this package is NOT covered until somebody adds it here AND to
//     TestResultScreen_SaysExactlyThisAndNothingElse. That is an unwritten
//     assumption made written: the coverage is per-screen and manual.
//
// The TAP page is deliberately absent: it legitimately renders a form, a button
// and three hidden inputs (pages.Tap), and its shape is asserted in tap_test.go.
func TestScreens_RenderOnlyTheseElements(t *testing.T) {
	// MEASURED from the rendered output, not chosen. Confirmation screens carry a
	// description list for the trust score; failure screens carry the retry
	// anchor; both share the layout shell.
	confirmation := []string{
		"html", "head", "meta", "title", "link", "body", "main", "header",
		"section", "div", "p", "h1", "span", "dl", "dt", "dd",
	}
	failure := []string{
		"html", "head", "meta", "title", "link", "body", "main", "header",
		"section", "div", "p", "h1", "span", "a",
	}

	t.Run("every confirmation screen", func(t *testing.T) {
		got := map[string]bool{}
		for _, verdict := range []string{"ok", "flag", "reject", "ignored", "something-new"} {
			for _, practice := range []bool{false, true} {
				body := mustBody(t, checkin.Result{
					Outcome:      checkin.OutcomeRecorded,
					Decision:     withNote(decisionOf(verdict, "in", practice), noteNoPlace),
					LocationName: "St Julians", Timezone: "Europe/Malta", BusinessType: "restaurant",
				})
				collectTags(body, got)
			}
		}
		assertTagSet(t, "confirmation", got, confirmation)
	})

	t.Run("every failure screen", func(t *testing.T) {
		got := map[string]bool{}
		for _, v := range []pages.ProblemView{
			tapProblemBadURL, tapProblemUnknownTag, tapProblemServer,
			tapProblemTooMany, tapProblemStale, tapProblemForeignTenant,
		} {
			for _, retry := range []string{"", problemRetryURL} {
				view := v
				view.RetryURL = retry
				var sb strings.Builder
				if err := pages.Problem(view).Render(context.Background(), &sb); err != nil {
					t.Fatalf("render: %v", err)
				}
				collectTags(sb.String(), got)
			}
		}
		assertTagSet(t, "failure", got, failure)
	})
}

var tagNameRE = regexp.MustCompile(`</?([a-zA-Z][a-zA-Z0-9-]*)`)

func collectTags(body string, into map[string]bool) {
	for _, m := range tagNameRE.FindAllStringSubmatch(body, -1) {
		into[strings.ToLower(m[1])] = true
	}
}

func assertTagSet(t *testing.T, family string, got map[string]bool, allowed []string) {
	t.Helper()
	allow := map[string]bool{}
	for _, a := range allowed {
		allow[a] = true
	}
	var added, missing []string
	for tag := range got {
		if !allow[tag] {
			added = append(added, tag)
		}
	}
	for _, a := range allowed {
		if !got[a] {
			missing = append(missing, a)
		}
	}
	sort.Strings(added)
	sort.Strings(missing)
	if len(added) > 0 {
		t.Fatalf("%s screens render element(s) that are not in the closed set: %v\n"+
			"An element is how text reaches a screen without passing through the text a "+
			"whole-text test reads — srcdoc, data:, an SVG <text>. If this element is meant "+
			"to be here, add it to the set AND make sure whatever it displays is covered by "+
			"TestResultScreen_SaysExactlyThisAndNothingElse.", family, added)
	}
	if len(missing) > 0 {
		t.Fatalf("%s screens no longer render %v. The set is exact on purpose: an entry nobody "+
			"renders is an entry nobody would notice being abused. Remove it from the list.",
			family, missing)
	}
}

// TestScreens_ReferenceOnlyOurOwnAssets closes the channel that beat the closed
// element set, and binds a brand rule that until now rested on discipline alone.
//
// 🔴 AN ALLOWED ELEMENT WITH AN UNREAD ATTRIBUTE. `link` is in both element sets,
// legitimately — the layout shell loads one stylesheet — and `href` is on
// textAttrRE's DELIBERATE EXCLUSION list, because it is machine-facing. Put those
// two facts together and a stylesheet can be inlined as a URL:
//
//	<link rel="stylesheet" href="data:text/css,.docket::after{content:'…'}">
//
// Measured, twice: on the confirmation screen and in the SHARED Problem template,
// both with the whole suite green. TestCompiledCSS_GeneratesNoText cannot help —
// it reads web/static/css/app.css, and a data: stylesheet is never compiled.
//
// SO THE REFERENCES THEMSELVES ARE PINNED. The set of href/src values a screen
// emits must be exactly the set below.
//
// ⚠️ AND THE FIRST VERSION OF THIS PARAGRAPH OVERCLAIMED. It said scanning href
// and src was enough "because no other URL-bearing element can appear on these
// screens, so there is no other attribute for a URL to hide in". `meta` is in both
// element sets, and <meta http-equiv="refresh" content="30;url=https://…"> is a
// URL on an allowed element — measured, in the shared layout shell, on EVERY
// screen in the product, with the suite green and nothing in tapCSP stopping a
// top-level meta-refresh navigation. Meta refresh is read below now. What is NOT
// claimed any more is that the attribute list is finished: it covers href, src,
// `ping` and meta-refresh, and any OTHER http-equiv directive is refused outright
// rather than reasoned about, because this test does not know their semantics.
//
// AND THAT DISCLAIMER EARNED ITS KEEP A SECOND TIME. `ping` is on the list because
// an audit put <a href="/activate/done" ping="https://evil.example/beacon"> on a
// tour slide and the whole handler package stayed green — a POST to a third party
// on click, from an attribute nothing fetches and nothing here was reading. It is
// one more name, not the end of the class.
//
// IT IS ALSO THE FIRST TEST OF A BRAND RULE. skill tappa-brand: "no RUNTIME
// connection to Google Fonts or any other third party (GDPR + working offline) —
// every url() points at our OWN origin, absolute URL count 0". That was true and
// entirely unenforced; a scheme in any of these values now fails.
func TestScreens_ReferenceOnlyOurOwnAssets(t *testing.T) {
	const stylesheet = "/static/css/app.css"
	// problemRetryURL, not a second copy: this file's own rule is one literal read
	// by however many tests need it (see problemScreens), and two of the three
	// call sites here were re-typing the string.
	retryURL := problemRetryURL

	t.Run("every confirmation screen", func(t *testing.T) {
		for _, verdict := range []string{"ok", "flag", "reject", "ignored", "something-new"} {
			body := mustBody(t, checkin.Result{
				Outcome: checkin.OutcomeRecorded, Decision: withNote(decisionOf(verdict, "in", false), noteNoPlace),
				LocationName: "St Julians", Timezone: "Europe/Malta", BusinessType: "restaurant",
			})
			assertRefs(t, "confirmation "+verdict, body, stylesheet)
		}
	})

	t.Run("every failure screen", func(t *testing.T) {
		for _, v := range []pages.ProblemView{
			tapProblemBadURL, tapProblemUnknownTag, tapProblemServer,
			tapProblemTooMany, tapProblemStale, tapProblemForeignTenant,
		} {
			for _, retry := range []string{"", retryURL} {
				view := v
				view.RetryURL = retry
				var sb strings.Builder
				if err := pages.Problem(view).Render(context.Background(), &sb); err != nil {
					t.Fatalf("render: %v", err)
				}
				want := []string{stylesheet}
				if retry != "" {
					want = append(want, retryURL)
				}
				assertRefs(t, "failure "+view.Title, sb.String(), want...)
			}
		}
	})
}

// refRE finds the ordinary URL attributes. metaTagRE and httpEquivRE together
// find the channel that lives on an ALLOWED element rather than on an element of
// its own — see the note in assertRefs for how it was found. (They replaced a
// single metaHTTPEquivRE that matched both attributes in one pattern, and
// therefore in one ORDER; that name is gone and this sentence used to still use
// it.)
//
// 🔴 `ping` WAS ADDED AFTER AN AUDIT WALKED A THIRD-PARTY BEACON PAST THIS TEST.
// It is not an ordinary URL attribute — nothing FETCHES it — which is exactly why
// it was missed: on click the browser POSTs to every URL in it, so
//
//	<a href="/activate/done" ping="https://evil.example/beacon">
//
// is a runtime connection to a third party on a page whose brand rule is "absolute
// URL count 0", and it reached a rendered slide with the whole handler package
// green (measured). It costs nothing here: `ping` appears in NO template in this
// repo, so no existing expectation moves.
//
// THE LIST IS STILL A LIST, and assertRefs says so in its own words. This entry is
// one more name on it, not the end of the class.
var (
	refRE = regexp.MustCompile(`(?i)\b(href|src|ping)="([^"]*)"`)
	// metaTagRE + httpEquivRE are deliberately two dumb questions asked in order —
	// "is this a meta tag" then "does it carry http-equiv, however written" —
	// rather than one clever pattern that assumes an attribute order HTML does not
	// have. Whitespace around the `=` is tolerated because HTML tolerates it.
	metaTagRE   = regexp.MustCompile(`(?i)<meta\b[^>]*>`)
	httpEquivRE = regexp.MustCompile(`(?i)\bhttp-equiv\s*=`)
)

// attrValueRE reads one attribute's value out of a tag, for ERROR MESSAGES ONLY.
// Nothing decides on it: the refusal above is decided by the presence of the
// attribute, so a value this cannot parse still fails.
func attrValueRE(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\s*=\s*"([^"]*)"`)
}

func assertRefs(t *testing.T, where, body string, allowed ...string) {
	t.Helper()
	allow := map[string]bool{}
	for _, a := range allowed {
		allow[a] = true
	}
	// <meta http-equiv=...>: an allowed element that can carry a URL.
	//
	// 🔴 THE DECISION IS "DOES THIS TAG CARRY http-equiv AT ALL", and it is made in
	// ONE place, on the tag rather than on a shape. The first version matched
	// `<meta …http-equiv="X"…content="Y">` in that ORDER, and HTML gives attribute
	// order no meaning while templ preserves whatever the author typed — so
	// `<meta content="30;url=https://evil.example/x" http-equiv="refresh">` in the
	// shared shell put a third-party navigation on every screen in the product with
	// the suite green. That is this session's canonical lesson repeating inside a
	// check written to enforce it (agent-brief: "HTTPS://", "Cross-Site",
	// "::ffff:0.0.0.0/96" — the check must see the same representation the consumer
	// does). The fix is not to enumerate writings; it is to stop depending on them
	// — WITH ONE ASSUMPTION LEFT, stated rather than glossed: metaTagRE ends a tag
	// at the first `>`, and HTML does not (a `>` inside a quoted attribute value
	// stays inside the tag). A value written that way slips past the decision
	// function. It cannot be produced through .templ — templ escapes `>` to `&gt;`,
	// and the end-to-end attempt fails RED with content-target="…?q=&gt;" — so the
	// only way to reach it is hand-editing a generated *_templ.go, which is the
	// "hand-edit of the generated *_templ.go files" entry in the KNOWN OPEN
	// CHANNELS list at screenText. (Named, not numbered: that list is bulleted and
	// the card's is numbered, so any number written here is wrong somewhere and
	// goes stale the moment either list is reordered.) The directive and target
	// below are extracted BEST-EFFORT, for the message only — never to decide.
	for _, tag := range metaTagRE.FindAllString(body, -1) {
		if !httpEquivRE.MatchString(tag) {
			continue
		}
		directive, target := "?", "?"
		if m := attrValueRE("http-equiv").FindStringSubmatch(tag); m != nil {
			directive = strings.ToLower(strings.TrimSpace(m[1]))
		}
		if m := attrValueRE("content").FindStringSubmatch(tag); m != nil {
			target = strings.TrimSpace(m[1])
			if i := strings.Index(strings.ToLower(target), "url"); i >= 0 {
				if j := strings.IndexByte(target[i:], '='); j >= 0 {
					target = strings.Trim(strings.TrimSpace(target[i+j+1:]), `'"`)
				}
			}
		}
		t.Fatalf("%s emits <meta http-equiv=%q content-target=%q>.\n"+
			"NO http-equiv is allowed on these screens. A `refresh` moves the visitor to "+
			"another origin with no click and no link, and no directive in tapCSP stops a "+
			"top-level navigation (skill tappa-brand: no runtime connection to any third "+
			"party); every other directive is refused because this test does not know its "+
			"semantics and will not wave through what it cannot reason about.",
			where, directive, target)
	}

	seen := map[string]bool{}
	for _, m := range refRE.FindAllStringSubmatch(body, -1) {
		// templ escapes & in attributes; compare against what the author wrote.
		ref := strings.ReplaceAll(m[2], "&amp;", "&")
		seen[ref] = true
		// The brand rule, checked before the set so the message names the reason:
		// our own origin only, and no scheme of any kind.
		if strings.HasPrefix(ref, "//") {
			t.Fatalf("%s references %q — a scheme-relative URL leaves our origin", where, ref)
		}
		if i := strings.IndexByte(ref, ':'); i >= 0 && !strings.ContainsRune(ref[:i], '/') {
			t.Fatalf("%s references %q, which carries a SCHEME.\n"+
				"skill tappa-brand: no runtime connection to any third party, absolute URL count 0. "+
				"A data: URL is the same violation and is also how a stylesheet — and therefore "+
				"screen text via content: — gets in without being compiled.", where, ref)
		}
		if !allow[ref] {
			t.Fatalf("%s references %q, which is not one of this screen's assets %v", where, ref, allowed)
		}
	}
	for _, a := range allowed {
		if !seen[a] {
			t.Fatalf("%s no longer references %q; the expected set is exact", where, a)
		}
	}
}

// TestTapResponses_CarryTheContentSecurityPolicy pins a header nothing was
// asserting.
//
// 🔴 IT IS LOAD-BEARING FOR THIS TASK, not a generic hardening check. The element
// channel that took a whole audit round to close — <iframe srcdoc>, <object
// data>, <img src="data:…">, <link href="data:text/css,…"> — is stopped in the
// BROWSER by tapCSP's default-src 'none' before the test suite ever gets a vote.
// So the closed element set and the reference set are the belt, and this header
// is the braces; and a grep showed the braces were unmeasured — the only test
// mentioning Content-Security-Policy fetched the tap page, so the confirmation
// screen and all six failure screens relied on a header no test would notice
// disappearing.
//
// SCOPE, stated exactly, because an earlier version of this file got it backwards
// and said these responses carry NO policy: handler.Tap.render sets it on every
// response it writes, which is pages.Tap, pages.Result and pages.Problem — the
// whole tap family. What does NOT carry it is the ACTIVATION flow's five failure
// screens, which render through Activation.render (measured: one
// Header().Set("Content-Security-Policy") in the whole of internal/handler, in
// tap.go). That is M5-02's deliberate boundary, recorded at tapCSP, and this task
// does not change it — but it does mean the shared pages.Problem template is
// served with a policy on six screens and without one on five.
func TestTapResponses_CarryTheContentSecurityPolicy(t *testing.T) {
	want := "default-src 'none'; script-src 'self'; style-src 'self'; font-src 'self'; " +
		"form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

	check := func(t *testing.T, what string, h http.Header) {
		t.Helper()
		if got := h.Get("Content-Security-Policy"); got != want {
			t.Fatalf("%s: Content-Security-Policy = %q, want %q.\n"+
				"This header is what stops a data: stylesheet or an <iframe srcdoc> in the browser; "+
				"the test-side element and reference sets are the other half.", what, got, want)
		}
	}

	t.Run("the confirmation screen", func(t *testing.T) {
		svc := &fakeCheckins{result: checkin.Result{
			Outcome: checkin.OutcomeRecorded, Decision: decisionOf("ok", "in", false),
		}}
		h, tp := newCheckinHandler(t, svc, fixedSessions())
		form := url.Values{"ctx": {mintedContext(t, tp, tapFixedSessionID, tapContext{
			UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true,
			TagTenantID: testTenant, LocationID: tapLocation,
		})}}
		w := postForm(t, h, form, sessionCookie())
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", w.Code)
		}
		check(t, "POST /api/checkin", w.Header())
	})

	t.Run("the failure screens", func(t *testing.T) {
		// Driven through the real routes, one per rendering path.
		h, tp := newTapHandler(t, &fakePreviewer{err: sun.ErrUnknownTag},
			&fakeDirectory{facts: okFacts()}, fixedSessions())
		check(t, "GET /t unknown plaque", get(t, h, tapURL(), sessionCookie()).Header())
		check(t, "GET /t malformed url", get(t, h, "/t?tag=nonsense", sessionCookie()).Header())

		h2, _ := newTapHandler(t, &fakePreviewer{err: errTapProbe},
			&fakeDirectory{facts: okFacts()}, fixedSessions())
		check(t, "GET /t server error (with retry link)", get(t, h2, tapURL(), sessionCookie()).Header())

		svc := &fakeCheckins{err: checkin.ErrUnknownTag}
		h3, tp3 := newCheckinHandler(t, svc, fixedSessions())
		form := url.Values{"ctx": {mintedContext(t, tp3, tapFixedSessionID, tapContext{
			UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true,
			TagTenantID: testTenant, LocationID: tapLocation,
		})}}
		check(t, "POST unknown tag", postForm(t, h3, form, sessionCookie()).Header())
		check(t, "POST stale context", postForm(t, h3, url.Values{"ctx": {"not-a-context"}}, sessionCookie()).Header())

		// sys:tenant-mismatch, the SIXTH failure screen. It was missing while the
		// card claimed all seven responses were pinned; six were.
		h4, tp4 := newCheckinHandler(t, &fakeCheckins{
			result: checkin.Result{Outcome: checkin.OutcomeForeignTenant},
		}, fixedSessions())
		form4 := url.Values{"ctx": {mintedContext(t, tp4, tapFixedSessionID, tapContext{
			UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true,
			TagTenantID: testTenant, LocationID: tapLocation,
		})}}
		w4 := postForm(t, h4, form4, sessionCookie())
		if w4.Code != http.StatusForbidden {
			t.Fatalf("foreign tenant status = %d, want 403; this case is not on the branch it names", w4.Code)
		}
		check(t, "POST another employer's plaque", w4.Header())

		// The 429 body, which the limiter renders through the same helper.
		w := httptestRecorder()
		tp.renderTooManyRequests(w, getRequest(tapURL()))
		check(t, "429", w.Header())
	})
}

// TestProblemScreens_ReachTheBrowserThroughTheRouter is the wiring proof for the
// failure copy, and it exists because the test above cannot be one.
//
// 🔴 A TEST THAT BUILDS THE OBJECT IT AUDITS CANNOT SEE A WIRING MISTAKE.
// TestProblemScreens_SayExactlyThisAndNothingElse hands a pages.ProblemView it
// constructed to pages.Problem and reads the string back: it proves the TEMPLATE
// and the CONSTANTS agree, and nothing at all about whether the product still
// serves them. Point Tap.renderProblem at a different template or a different
// view and that test stays green — the only router-side coverage those six
// screens had was strings.Contains on "Try again" and three Hints. That is
// verbatim the M5-04 lesson this repo already paid for (agent-brief: "a mutation
// that passes BETWEEN two solid proofs"), sitting in the file whose own header
// claimed to have avoided it.
//
// So five of the six are driven through the MOUNTED ROUTER — chi, the middleware
// order Mount fixes, the real handler — and compared against the SAME literal
// table. The sixth (tapProblemTooMany) is reached only by the rate limiter, whose
// ADDRESS budget is 3000 requests (internal/httpx: tapAddressLimit) — thousands,
// not hundreds — which is why it is not driven from here. It is NOT thereby
// component-only, and saying so would undersell it: tap_test.go's
// TestTapPage_RefusalBodyIsBrandedAtTheProductWiring already spends that
// production budget through the MOUNTED ROUTER and asserts the branded body comes
// back, and TestTapResponses_CarryTheContentSecurityPolicy pins its header. What
// this table adds for that screen is the whole-text pin, at component level.
func TestProblemScreens_ReachTheBrowserThroughTheRouter(t *testing.T) {
	want := func(name string, retry bool) string {
		t.Helper()
		for _, sc := range problemScreens {
			if sc.name == name {
				if retry {
					return sc.want + " Try again"
				}
				return sc.want
			}
		}
		t.Fatalf("no expectation named %q in problemScreens", name)
		return ""
	}

	// The signed context these POSTs carry; the failures are chosen by what the
	// fake domain answers, not by malforming it (except the stale case, which is
	// about the context itself).
	newPost := func(t *testing.T, svc *fakeCheckins) (http.Handler, url.Values) {
		t.Helper()
		h, tp := newCheckinHandler(t, svc, fixedSessions())
		return h, url.Values{"ctx": {mintedContext(t, tp, tapFixedSessionID, tapContext{
			UID: tapUID, Ctr: 641, Channel: "nfc", CMACVerified: true,
			TagTenantID: testTenant, LocationID: tapLocation,
		})}}
	}

	tests := []struct {
		name string
		want string
		body func(t *testing.T) string
	}{
		{
			name: "GET /t with an unparseable url", want: want("bad url", false),
			body: func(t *testing.T) string {
				h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)},
					&fakeDirectory{facts: okFacts()}, fixedSessions())
				return get(t, h, "/t?tag=nonsense", sessionCookie()).Body.String()
			},
		},
		{
			name: "GET /t with a uid we do not have", want: want("unknown plaque", false),
			body: func(t *testing.T) string {
				h, _ := newTapHandler(t, &fakePreviewer{err: sun.ErrUnknownTag},
					&fakeDirectory{facts: okFacts()}, fixedSessions())
				return get(t, h, tapURL(), sessionCookie()).Body.String()
			},
		},
		{
			// The one screen a browser sees WITH the retry link, which makes this the
			// only end-to-end check of that block's copy.
			name: "GET /t when the pre-check fails", want: want("server error", true),
			body: func(t *testing.T) string {
				h, _ := newTapHandler(t, &fakePreviewer{err: errTapProbe},
					&fakeDirectory{facts: okFacts()}, fixedSessions())
				return get(t, h, tapURL(), sessionCookie()).Body.String()
			},
		},
		{
			name: "POST with a context that does not verify", want: want("stale context", false),
			body: func(t *testing.T) string {
				h, _ := newPost(t, &fakeCheckins{})
				return postForm(t, h, url.Values{"ctx": {"not-a-context"}}, sessionCookie()).Body.String()
			},
		},
		{
			name: "POST against another employer's plaque", want: want("another employer's plaque", false),
			body: func(t *testing.T) string {
				h, form := newPost(t, &fakeCheckins{
					result: checkin.Result{Outcome: checkin.OutcomeForeignTenant},
				})
				return postForm(t, h, form, sessionCookie()).Body.String()
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := screenText(t, tc.body(t)); got != tc.want {
				t.Fatalf("the screen a browser receives is not the one the constants describe.\n"+
					" got: %s\nwant: %s\n\n"+
					"This test drives the MOUNTED ROUTER, so it fails when the handler stops "+
					"serving the view or the template the component-level test checks — the gap "+
					"a test that builds its own view cannot see.", got, tc.want)
			}
		})
	}

	t.Run("the retry variant really is the one the router serves", func(t *testing.T) {
		// Guards the `want(..., true)` above from quietly becoming vacuous: if the
		// server-error branch stopped offering a link, its expectation would still
		// carry " Try again" and the case would fail — but for a reason worth
		// naming, so it is named here instead.
		h, _ := newTapHandler(t, &fakePreviewer{err: errTapProbe},
			&fakeDirectory{facts: okFacts()}, fixedSessions())
		if !strings.Contains(get(t, h, tapURL(), sessionCookie()).Body.String(), ">Try again</a>") {
			t.Fatal("the pre-check branch no longer serves the retry anchor")
		}
	})
}

// TestProblemScreen_RetryHrefEscapesAHostileQuery is a regression net under an
// escape that two audits verified by hand and no test held.
//
// 🔴 WHY IT WAS NEEDED. ProblemView.RetryURL is r.URL.RequestURI(), and RawQuery
// is the request line verbatim: internal/sun's parser ignores parameters it does
// not know, so /t?tag=<14 hex>&anything=… reaches this screen and "anything" is
// reflected into the href. The reflected bytes are inert because templ escapes the
// attribute — measured repeatedly, never asserted. An escape nobody asserts is an
// escape that can be removed in a refactor without a single test noticing.
//
// The two mechanisms that make this safe are documented at ProblemView.RetryURL:
// chi pins the path to /t (a `//host` or `/%2Ft` form answers 404 before the
// handler), and templ escapes the value. This test covers the SECOND one.
//
// HOW FAR THE MUTATION COULD GO, stated as a boundary rather than a claim. The
// escape is emitted BY templ (templ.EscapeString in the generated file) and there
// is no way to switch it off from the .templ source — `templ.SafeURL` and
// `templ.URL` both flow through the same escaping write. So the mutation that
// proves this test bites had to edit web/templates/pages/activate_templ.go
// directly — the "hand-edit of the generated *_templ.go files" entry in
// screenText's KNOWN OPEN CHANNELS list, named rather than numbered for the reason
// given at assertRefs — and `make gen` puts it straight back:
// removing the EscapeString call produces 3 `--- FAIL:` lines across 2 test
// functions, including this one. What that
// means honestly: this test does not guard against a source-level mistake nobody
// can currently make; it guards against the generator's behaviour CHANGING — a
// templ upgrade, or a future refactor that builds the attribute some other way —
// which is exactly the kind of silent change an unasserted escape invites.
func TestProblemScreen_RetryHrefEscapesAHostileQuery(t *testing.T) {
	const payload = `"><script>alert(1)</script>`

	h, _ := newTapHandler(t, &fakePreviewer{err: errTapProbe},
		&fakeDirectory{facts: okFacts()}, fixedSessions())
	body := get(t, h, tapURL()+"&j="+payload, sessionCookie()).Body.String()

	if !strings.Contains(body, ">Try again</a>") {
		t.Fatal("this request did not reach a screen with a retry link; the test is not testing the href")
	}
	// The raw payload must not appear ANYWHERE in the document: escaped, it cannot
	// close the attribute or open an element.
	if strings.Contains(body, payload) {
		t.Fatalf("the retry href reflected a hostile query UNESCAPED.\nbody: %s", body)
	}
	if strings.Contains(body, "<script>") {
		t.Fatal("a <script> element reached the rendered page through the retry href")
	}
	// And it must still be THERE, escaped — otherwise a change that silently
	// dropped the query would pass the two checks above while breaking the link.
	if !strings.Contains(body, "&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("the query is neither escaped nor present; the href no longer round-trips.\nbody: %s", body)
	}
}
