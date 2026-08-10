package handler

// review_test.go -- M6-04's handler-level nets, with a fake queue and a fake
// reviewer.
//
// WHAT THIS FILE CAN AND CANNOT MEASURE, stated up front because the split with
// review_db_test.go is the same one transactions_db_test.go opens with. A fake
// proves NOTHING about §4.5 or §4.3: it has no second tenant to leak and no
// database to refuse an UPDATE. What it CAN prove is everything about the handler's
// own decisions -- what it does with a bad outcome, where it takes the tenant and
// the reviewer from, whether a refusal is told to the manager, and whether a failed
// read is allowed to look like an empty queue. Those are branches, and driving them
// through real Postgres would mean provoking a database failure on demand.

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/ledger"
	"github.com/atknatk/tappa/internal/domain/review"
	"github.com/atknatk/tappa/web/templates/pages"
)

// queueWith builds a fake holding n records waiting for review.
func queueWith(n int) *fakeLedger {
	f := newFakeLedger()
	f.pending = ledger.Pending{N: n}
	for i := 0; i < n; i++ {
		f.queue.Records = append(f.queue.Records, ledger.Record{
			ID:           uuid.New(),
			OccurredAt:   time.Date(2026, 8, 5, 9, i, 0, 0, time.UTC),
			Direction:    "in",
			Verdict:      "flag",
			Channel:      "nfc",
			Queued:       true,
			EmployeeName: "Someone",
			LocationName: "Somewhere",
		})
	}
	return f
}

// --- §4.6: a screen may not claim what it has not measured ------------------

// TestReviewSection_AFailedReadIsAnErrorAndNeverAnEmptyQueue is the §4.6 net, and
// the sentence it protects is worse here than on the day view: "every flagged tap
// has been decided" tells a manager there is no work waiting, on the strength of a
// database timeout, and the work it hides is somebody's pay.
func TestReviewSection_AFailedReadIsAnErrorAndNeverAnEmptyQueue(t *testing.T) {
	broken := newFakeLedger()
	broken.queueErr = errors.New("database is unreachable")
	b := panelBrowserWith(t, broken)

	rec := b.do(http.MethodGet, reviewHref, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("GET %s answered %d when the read failed, want 500", reviewHref, rec.Code)
	}
	body := htmlOf(t, rec)
	for _, claim := range []string{"Nothing is waiting", "has been decided"} {
		if strings.Contains(body, claim) {
			t.Errorf("GET %s could not read the database and told the manager %q.\n"+
				"That is a claim about the world made without measuring it.", reviewHref, claim)
		}
	}
}

// TestReviewSection_AnUnqueriedViewClaimsNothing is the same rule one layer down,
// at the view model, where the flag can be mutated on its own.
func TestReviewSection_AnUnqueriedViewClaimsNothing(t *testing.T) {
	silent := newFakeLedger()
	silent.queue = ledger.QueuePage{} // never queried
	b := panelBrowserWith(t, silent)

	body := htmlOf(t, b.do(http.MethodGet, reviewHref, nil))
	if strings.Contains(body, "Nothing is waiting") {
		t.Error("a view that never queried rendered the empty state. Queried is the " +
			"anti-fabrication flag and the template must not print a claim without it.")
	}
}

// TestReviewBadge_AFailedCountNeverReadsAsZERO is the same class in the ONE place
// it is easiest to get wrong, because the natural implementation is silent.
//
// 🔴 THE BADGE IS ON EVERY PANEL PAGE, so a count that fails and renders nothing
// tells a manager on every screen that the queue is clear. The page must still
// render -- refusing the whole section because a badge could not be counted would
// be worse -- but the badge has to say it does not know.
func TestReviewBadge_AFailedCountNeverReadsAsZero(t *testing.T) {
	broken := newFakeLedger()
	broken.pendingErr = errors.New("database is unreachable")
	b := panelBrowserWith(t, broken)

	for _, s := range pages.PanelSections {
		if s.Tab == pages.TabTransactions || s.Tab == pages.TabReview {
			continue // those two have their own read, which the fake still answers
		}
		rec := b.do(http.MethodGet, s.Href, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s answered %d when only the BADGE count failed; the section "+
				"itself is still readable and must still render", s.Href, rec.Code)
		}
		body := htmlOf(t, rec)
		if !strings.Contains(body, `class="tab-count"`) {
			t.Errorf("GET %s renders no badge at all after a failed count. An absent badge "+
				"reads as 'nothing is waiting', which is a claim nobody measured.", s.Href)
		}
		if !strings.Contains(body, "could not be read") {
			t.Errorf("GET %s renders a badge with no accessible explanation of the unknown "+
				"count.\nbody: %s", s.Href, truncateForMsg(body))
		}
	}
}

// TestReviewBadge_PrintsNothingWhenTheQueueIsMEASUREDEmpty is the other half, and
// it is what stops the fix above from being "always show a badge": zero waiting is
// a measured fact and the honest rendering of it is no badge.
func TestReviewBadge_PrintsNothingWhenTheQueueIsMeasuredEmpty(t *testing.T) {
	empty := newFakeLedger() // pending zero value: Known, N == 0
	b := panelBrowserWith(t, empty)
	body := htmlOf(t, b.do(http.MethodGet, reviewHref, nil))
	if strings.Contains(body, `class="tab-count"`) {
		t.Error("the navigation shows a badge for a queue that was counted and is empty")
	}
	if !strings.Contains(body, "Waiting for review: 0") {
		t.Errorf("the section does not state the measured count.\nbody: %s", truncateForMsg(body))
	}
}

// TestReviewBadge_CappedCountsSayTheyAreAFloor. A queue over the cap must not print
// the cap as if it were the total.
func TestReviewBadge_CappedCountsSayTheyAreAFloor(t *testing.T) {
	f := queueWith(1)
	f.pending = ledger.Pending{N: ledger.PendingCap, Capped: true}
	b := panelBrowserWith(t, f)
	body := htmlOf(t, b.do(http.MethodGet, reviewHref, nil))

	if !strings.Contains(body, "100+") {
		t.Errorf("a capped count does not render as a floor.\nbody: %s", truncateForMsg(body))
	}
	if !strings.Contains(body, "more than 100") {
		t.Errorf("a capped count has no accessible 'more than' wording.\nbody: %s", truncateForMsg(body))
	}
}

// --- the decision endpoint --------------------------------------------------

// TestReviewDecision_RefusesAnUnknownOutcomeWithoutTOUCHINGTheDatabase.
//
// 🔴 DEFAULTING WOULD BE THE PLAUSIBLE BUG AND IT IS THE DANGEROUS DIRECTION: a
// mistyped, truncated or absent outcome becoming "approved" silently adds paid
// hours. The reviewer must not be called at all.
func TestReviewDecision_RefusesAnUnknownOutcome(t *testing.T) {
	for _, outcome := range []string{"", "APPROVED", "approve", "yes", "ok", "deleted"} {
		t.Run("outcome="+outcome, func(t *testing.T) {
			reviewer := &fakeReviewer{}
			b := panelBrowserWithReviewer(t, queueWith(1), reviewer)

			rec := b.do(http.MethodPost, reviewHref, url.Values{
				"id": {uuid.NewString()}, "outcome": {outcome},
			})
			if rec.Code != http.StatusSeeOther {
				t.Errorf("POST with outcome %q answered %d, want 303", outcome, rec.Code)
			}
			// unreadable, NOT gone: an outcome outside the two is a malformed
			// REQUEST, and "gone" renders a sentence about the record's state that
			// nothing here looked up.
			if got := rec.Header().Get("Location"); !strings.Contains(got, "problem=unreadable") {
				t.Errorf("POST with outcome %q redirected to %q, want problem=unreadable", outcome, got)
			}
			if n := len(reviewer.recorded()); n != 0 {
				t.Errorf("POST with outcome %q reached the reviewer %d time(s); an "+
					"unrecognised outcome must never become a decision", outcome, n)
			}
		})
	}
}

// TestReviewDecision_TakesTheTenantAndTheReviewerFromTheSession is §4.5 at the
// handler boundary: the two identities that authorise the write are not inputs.
func TestReviewDecision_TakesTheTenantAndTheReviewerFromTheSession(t *testing.T) {
	reviewer := &fakeReviewer{}
	b := panelBrowserWithReviewer(t, queueWith(1), reviewer)

	otherTenant, otherAdmin := uuid.New(), uuid.New()
	txn := uuid.New()
	rec := b.do(http.MethodPost, reviewHref, url.Values{
		"id": {txn.String()}, "outcome": {"approved"},
		// Everything below is a lie the client is allowed to tell.
		"tenant_id": {otherTenant.String()},
		"reviewer":  {otherAdmin.String()},
		"verdict":   {"ok"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST answered %d, want 303", rec.Code)
	}
	calls := reviewer.recorded()
	if len(calls) != 1 {
		t.Fatalf("the reviewer was called %d time(s), want 1", len(calls))
	}
	switch {
	case calls[0].TenantID != panelTestTenant:
		t.Errorf("the decision carried tenant %s; the session's tenant is %s. A tenant "+
			"that can be posted is not a tenant boundary.", calls[0].TenantID, panelTestTenant)
	case calls[0].ReviewerID != panelTestAdmin:
		t.Errorf("the decision carried reviewer %s; the session's admin is %s",
			calls[0].ReviewerID, panelTestAdmin)
	case calls[0].TransactionID != txn:
		t.Errorf("the decision carried transaction %s, want %s", calls[0].TransactionID, txn)
	case calls[0].Outcome != review.Approved:
		t.Errorf("the decision carried outcome %q, want approved", calls[0].Outcome)
	}
}

// TestReviewDecision_TheLoserOfARaceIsTold is §4.6 on the concurrency path.
//
// 🔴 THE SILENT VERSION IS PLAUSIBLE AND WRONG. Redirecting a refused decision to a
// clean queue looks like success: the record is gone from the list (somebody else
// decided it), so the manager concludes their click is what did it -- and if the
// other manager pressed Reject, they walk away believing they approved a record
// that was rejected.
func TestReviewDecision_TheLoserOfARaceIsTold(t *testing.T) {
	reviewer := &fakeReviewer{err: review.ErrAlreadyDecided}
	b := panelBrowserWithReviewer(t, queueWith(0), reviewer)

	rec := b.do(http.MethodPost, reviewHref, url.Values{
		"id": {uuid.NewString()}, "outcome": {"approved"},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST answered %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "problem=taken") {
		t.Fatalf("a refused decision redirected to %q; the manager is not told", loc)
	}
	// And the sentence must actually appear on the page the redirect points at.
	body := htmlOf(t, b.do(http.MethodGet, loc, nil))
	if !strings.Contains(body, "Already decided") {
		t.Errorf("the queue does not tell the loser of a race what happened.\nbody: %s",
			truncateForMsg(body))
	}
	if strings.Contains(body, "Recorded as approved") {
		t.Error("the queue told the loser their decision was recorded. It was not.")
	}
}

// TestReviewDecision_ADoubleClickSaysYOU is the screen half of the same rule the
// database half measures in review_db_test.go.
//
// 🔴 THE BROKEN SENTENCE WAS "Somebody else decided that record first", SHOWN TO
// THE PERSON WHO DECIDED IT. This drives the branch through the real template so
// the assertion is about what a manager reads, not about which error value the
// handler received.
func TestReviewDecision_ADoubleClickSaysYou(t *testing.T) {
	reviewer := &fakeReviewer{err: &review.DecidedError{ByYou: true, Outcome: review.Rejected}}
	b := panelBrowserWithReviewer(t, queueWith(0), reviewer)

	rec := b.do(http.MethodPost, reviewHref, url.Values{
		"id": {uuid.NewString()}, "outcome": {"approved"},
	})
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "problem=yours") || !strings.Contains(loc, "was=rejected") {
		t.Fatalf("a caller's own second decision redirected to %q; the screen cannot "+
			"tell them it was theirs, nor which decision is on record", loc)
	}

	body := htmlOf(t, b.do(http.MethodGet, loc, nil))
	if strings.Contains(body, "Somebody else") {
		t.Error("the manager is told SOMEBODY ELSE decided a record they decided " +
			"themselves. The system had not measured the who, and said it anyway.")
	}
	for _, want := range []string{"You already decided this one", "as rejected"} {
		if !strings.Contains(body, want) {
			t.Errorf("the screen does not say %q.\nbody: %s", want, truncateForMsg(body))
		}
	}
	if strings.Contains(body, "Recorded as approved") {
		t.Error("the screen confirms a decision that was refused")
	}
}

// TestReviewDecision_TheYoursScreenNeverGUESSESTheOutcome. `was` is reflected, so
// it is held to the closed set — and an absent or unknown value must degrade to the
// sentence with no outcome in it rather than to a default.
func TestReviewDecision_TheYoursScreenNeverGuessesTheOutcome(t *testing.T) {
	b := panelBrowserWith(t, queueWith(0))
	for _, was := range []string{"", "APPROVED", "deleted", `"><script>alert(1)</script>`} {
		body := htmlOf(t, b.do(http.MethodGet,
			reviewHref+"?problem=yours&was="+url.QueryEscape(was), nil))
		if !strings.Contains(body, "You had already recorded a decision on this one") {
			t.Errorf("was=%q does not degrade to the outcome-less sentence.\nbody: %s",
				was, truncateForMsg(body))
		}
		for _, guessed := range []string{"as approved", "as rejected"} {
			if strings.Contains(body, guessed) {
				t.Errorf("was=%q made the screen claim %q", was, guessed)
			}
		}
		if strings.Contains(body, "<script>alert") {
			t.Errorf("was=%q was reflected into the page", was)
		}
	}
}

// TestReviewDecision_ARealFailureIsNotDressedAsARefusal. An outage must not be
// rendered as "that record is not waiting any more" -- one is a fact about the
// record and the other is a fact about us, and only one of them means "try again".
func TestReviewDecision_ARealFailureIsNotDressedAsARefusal(t *testing.T) {
	reviewer := &fakeReviewer{err: errors.New("connection refused")}
	b := panelBrowserWithReviewer(t, queueWith(1), reviewer)

	rec := b.do(http.MethodPost, reviewHref, url.Values{
		"id": {uuid.NewString()}, "outcome": {"rejected"},
	})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("a database failure answered %d, want 500 -- a redirect would tell the "+
			"manager their decision was refused when in fact nothing was asked", rec.Code)
	}
}

// TestReviewDecision_ConfirmsWhatWasRecordedAndSaysTheRecordIsUNCHANGED.
//
// The second half is §4.3's user-facing edge: a manager who has just pressed
// Approve must not be left thinking the tap record was altered.
func TestReviewDecision_ConfirmsAndSaysTheRecordIsUnchanged(t *testing.T) {
	records := queueWith(0)
	b := panelBrowserWithReviewer(t, records, &fakeReviewer{})

	txn := uuid.New()
	// THE FAKE MUST AGREE THAT THE DECISION EXISTS, because the banner is now
	// verified against the database rather than read off the URL. A fake that
	// "succeeded" while recording nothing is exactly the state the real system was
	// in when an audit found the banner printable from a link.
	records.decided(txn, "approved")

	rec := b.do(http.MethodPost, reviewHref, url.Values{
		"id": {txn.String()}, "outcome": {"approved"},
	})
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "done=approved") || !strings.Contains(loc, "for="+txn.String()) {
		t.Fatalf("a recorded decision redirected to %q; the confirmation must name the "+
			"record so the next request can check it", loc)
	}
	body := htmlOf(t, b.do(http.MethodGet, loc, nil))
	if !strings.Contains(body, "Recorded as approved") {
		t.Errorf("the queue does not confirm the decision.\nbody: %s", truncateForMsg(body))
	}
	if !strings.Contains(body, "unchanged") {
		t.Errorf("the confirmation does not say the tap record itself is unchanged -- which "+
			"is the whole point of Q20.\nbody: %s", truncateForMsg(body))
	}
}

// TestReviewSection_EchoesOnlyTheTwoWordsItKnows. The done/problem flags are
// reflected into the page, so they are held to a closed set rather than escaped
// more carefully.
func TestReviewSection_EchoesOnlyTheTwoWordsItKnows(t *testing.T) {
	b := panelBrowserWith(t, queueWith(0))
	for _, hostile := range []string{
		`"><script>alert(1)</script>`,
		`approved" onload="x`,
		`deleted`,
		`APPROVED`,
	} {
		body := htmlOf(t, b.do(http.MethodGet,
			reviewHref+"?done="+url.QueryEscape(hostile)+"&problem="+url.QueryEscape(hostile), nil))
		if strings.Contains(body, "<script>alert") || strings.Contains(body, "onload=") {
			t.Errorf("the section reflected %q into the page", hostile)
		}
		if strings.Contains(body, "Recorded as") || strings.Contains(body, "Already decided") {
			t.Errorf("the section rendered a confirmation for the unknown value %q", hostile)
		}
	}
}

// --- the note ---------------------------------------------------------------

// TestReviewNote_SurvivesHostileInput. It is employeeNameFilter's test applied to
// the second free-text field the panel has, and the two 500s behind it are real:
// a NUL byte cannot be stored in a PostgreSQL text column, and cutting by BYTE
// splits the ċ ġ ħ ż and İ that Maltese and Turkish managers type.
func TestReviewNote_SurvivesHostileInput(t *testing.T) {
	tests := []struct {
		name  string
		in    string
		check func(t *testing.T, got string, clipped bool)
	}{
		{"a nul byte is removed", "on\x00 the terrace", func(t *testing.T, got string, clipped bool) {
			if strings.ContainsRune(got, 0) {
				t.Error("a NUL survived; PostgreSQL refuses the whole statement")
			}
			if clipped {
				t.Error("removing a NUL was reported as a clip. Nothing the manager meant " +
					"to write was lost, and a false clip warning is its own wrong claim.")
			}
		}},
		{"whitespace only is no note", "   \t\n ", func(t *testing.T, got string, clipped bool) {
			if got != "" {
				t.Errorf("got %q, want the empty string", got)
			}
			if clipped {
				t.Error("trimming whitespace was reported as a clip")
			}
		}},
		{"exactly at the limit is NOT a clip", strings.Repeat("a", maxReviewNote),
			func(t *testing.T, got string, clipped bool) {
				if n := len([]rune(got)); n != maxReviewNote {
					t.Errorf("kept %d runes, want %d", n, maxReviewNote)
				}
				if clipped {
					t.Error("a note of exactly the limit was reported as clipped: the " +
						"boundary is off by one and a manager is told text was lost when " +
						"none was")
				}
			}},
		{"one over the limit IS a clip", strings.Repeat("a", maxReviewNote+1),
			func(t *testing.T, got string, clipped bool) {
				if n := len([]rune(got)); n != maxReviewNote {
					t.Errorf("kept %d runes, want %d", n, maxReviewNote)
				}
				if !clipped {
					t.Error("501 characters were cut to 500 and NOT reported. Making that " +
						"cut visible is the whole reason the note is rendered at all " +
						"(user decision, 2026-08-06).")
				}
			}},
		{"truncation counts runes", strings.Repeat("a", maxReviewNote-1) + "\u0127" + "XYZ",
			func(t *testing.T, got string, clipped bool) {
				if n := len([]rune(got)); n != maxReviewNote {
					t.Errorf("kept %d runes, want %d", n, maxReviewNote)
				}
				if !strings.HasSuffix(got, "\u0127") {
					t.Errorf("the cut landed inside a multi-byte character: %q", got[len(got)-4:])
				}
				if !clipped {
					t.Error("a multi-byte note was cut and not reported")
				}
			}},
		{"invalid utf-8 is dropped", "caf\xff\xfe", func(t *testing.T, got string, clipped bool) {
			if strings.ContainsRune(got, 0xFFFD) || !isValidUTF8(got) {
				t.Errorf("invalid bytes reached the domain: %q", got)
			}
			if clipped {
				t.Error("dropping invalid bytes was reported as a clip")
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			note, clipped := reviewNote(tc.in)
			tc.check(t, note, clipped)
		})
	}
}

// TestReviewDecision_ANoteIsCarriedThroughVerbatim -- the boundary cleans, it does
// not escape. templ escapes on output; escaping here too would store the escape and
// show it back, which is the bug M6-03 shipped once on the name filter.
func TestReviewDecision_ANoteIsCarriedThroughVerbatim(t *testing.T) {
	reviewer := &fakeReviewer{}
	b := panelBrowserWithReviewer(t, queueWith(1), reviewer)

	const note = `Maria was on the terrace & the router was <down>`
	rec := b.do(http.MethodPost, reviewHref, url.Values{
		"id": {uuid.NewString()}, "outcome": {"approved"}, "note": {note},
	})
	if loc := rec.Header().Get("Location"); strings.Contains(loc, "clipped") {
		t.Errorf("a %d-character note was reported as clipped: %q", len([]rune(note)), loc)
	}
	calls := reviewer.recorded()
	if len(calls) != 1 {
		t.Fatalf("the reviewer was called %d time(s), want 1", len(calls))
	}
	if calls[0].Note != note {
		t.Errorf("the note reached the domain as %q, want %q -- the boundary cleans, it "+
			"does not escape", calls[0].Note, note)
	}
}

// TestReviewDecision_RefusesAnOversizedBody is the net for maxReviewBody.
//
// 🔴 IT SHIPPED WITHOUT ONE. ParseForm reads the WHOLE body into memory before the
// handler sees a field, so a bare call inherits net/http's 10 MB default on the
// panel's only write — 300 x 10 MB per session per window from a caller who has
// merely signed in. internal/handler/checkin.go bounds the tap endpoint for exactly
// this reason and this route did not.
func TestReviewDecision_RefusesAnOversizedBody(t *testing.T) {
	reviewer := &fakeReviewer{}
	b := panelBrowserWithReviewer(t, queueWith(1), reviewer)

	// POSITIVE CONTROL: a body just UNDER the ceiling is accepted, so "refused"
	// below is about the size and not about the shape of a raw post.
	under := url.Values{
		"id": {uuid.NewString()}, "outcome": {"approved"},
		"note": {strings.Repeat("a", 1<<10)},
	}
	if rec := b.doRaw(http.MethodPost, reviewHref, under.Encode()); rec.Code != http.StatusSeeOther {
		t.Fatalf("a %d-byte body answered %d, want 303", len(under.Encode()), rec.Code)
	}
	if n := len(reviewer.recorded()); n != 1 {
		t.Fatalf("the control body recorded %d decision(s), want 1", n)
	}

	over := url.Values{
		"id": {uuid.NewString()}, "outcome": {"approved"},
		"note": {strings.Repeat("a", maxReviewBody*2)},
	}
	body := over.Encode()
	if len(body) <= maxReviewBody {
		t.Fatalf("the oversized body is %d bytes, which is not over the %d-byte ceiling",
			len(body), maxReviewBody)
	}
	rec := b.doRaw(http.MethodPost, reviewHref, body)
	if rec.Code != http.StatusSeeOther {
		t.Errorf("a %d-byte body answered %d, want 303 (a refusal, not a crash)",
			len(body), rec.Code)
	}
	if n := len(reviewer.recorded()); n != 1 {
		t.Errorf("a %d-byte body reached the reviewer; the total recorded is now %d, "+
			"want 1 (the control only). ParseForm must fail on the read rather than "+
			"buffer the whole body first.", len(body), n)
	}
}

// TestReviewDecision_AMalformedREQUESTSaysNothingAboutTheRECORD is F3's net, and
// it is B4's rule applied one layer out: a refusal may not name a fact the server
// never looked up.
//
// 🔴 THE THREE BRANCHES BELOW NEVER TOUCH THE DATABASE. An oversized body, an id
// that is not a uuid and an outcome outside the closed pair are all decided from
// the request alone — yet all three used to redirect to problem=gone, whose screen
// says "that record is not waiting for a decision any more". On the oversized-body
// path the record IS still waiting, and on the unparseable-id path there is no
// record to say anything about.
//
// ⚠️ THE EXISTENCE ORACLE IS STILL CLOSED and this test asserts that too: the
// three REAL record questions (absent / another tenant's / not flagged) keep
// collapsing into one answer, which is what stops this endpoint telling a stranger
// whether an id exists elsewhere.
func TestReviewDecision_AMalformedRequestSaysNothingAboutTheRecord(t *testing.T) {
	reviewer := &fakeReviewer{}
	b := panelBrowserWithReviewer(t, queueWith(1), reviewer)

	cases := []struct {
		name string
		post func() *httptest.ResponseRecorder
	}{
		{"an id that is not a uuid", func() *httptest.ResponseRecorder {
			return b.do(http.MethodPost, reviewHref, url.Values{
				"id": {"not-a-uuid"}, "outcome": {"approved"},
			})
		}},
		{"an outcome outside the two", func() *httptest.ResponseRecorder {
			return b.do(http.MethodPost, reviewHref, url.Values{
				"id": {uuid.NewString()}, "outcome": {"maybe"},
			})
		}},
		{"a body over the ceiling", func() *httptest.ResponseRecorder {
			over := url.Values{
				"id": {uuid.NewString()}, "outcome": {"approved"},
				"note": {strings.Repeat("a", maxReviewBody*2)},
			}
			return b.doRaw(http.MethodPost, reviewHref, over.Encode())
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := tc.post()
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("answered %d, want 303", rec.Code)
			}
			loc := rec.Header().Get("Location")
			if !strings.Contains(loc, "problem=unreadable") {
				t.Errorf("redirected to %q; a request the server could not read must not "+
					"be reported as a fact about the record", loc)
			}
			body := htmlOf(t, b.do(http.MethodGet, loc, nil))
			if strings.Contains(body, "not waiting for a decision any more") {
				t.Error("the screen says the record is no longer waiting. Nothing looked " +
					"it up: on the oversized-body path it IS still waiting, and on the " +
					"unparseable-id path there is no record to describe.")
			}
			if !strings.Contains(body, "could not read that") {
				t.Errorf("the screen does not say the submission was unreadable.\nbody: %s",
					truncateForMsg(body))
			}
			if !strings.Contains(body, "still in the queue") {
				t.Errorf("the screen does not tell the manager the record is still there, "+
					"which is the only actionable fact on this path.\nbody: %s",
					truncateForMsg(body))
			}
		})
	}
	if n := len(reviewer.recorded()); n != 0 {
		t.Errorf("a malformed request reached the reviewer %d time(s)", n)
	}
}

// TestReviewDecision_TheRecordQuestionsStillCollapse is the other side of the test
// above: separating a REQUEST error from a RECORD error must not have separated the
// three RECORD errors from each other.
func TestReviewDecision_TheRecordQuestionsStillCollapse(t *testing.T) {
	reviewer := &fakeReviewer{err: review.ErrNotReviewable}
	b := panelBrowserWithReviewer(t, queueWith(1), reviewer)

	rec := b.do(http.MethodPost, reviewHref, url.Values{
		"id": {uuid.NewString()}, "outcome": {"approved"},
	})
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "problem=gone") {
		t.Fatalf("a record the domain refused redirected to %q, want problem=gone", loc)
	}
	body := htmlOf(t, b.do(http.MethodGet, loc, nil))
	for _, leak := range []string{"another business", "does not exist", "not flagged", "wrong tenant"} {
		if strings.Contains(strings.ToLower(body), leak) {
			t.Errorf("the screen distinguishes WHY the record was refused (%q). The three "+
				"questions collapse on purpose -- answering them separately tells a "+
				"stranger whether an id exists in somebody else's business.", leak)
		}
	}
}

// TestReviewDecision_AMalformedFormNeverLogsWhatWasSubmitted is T6's net.
//
// 🔴 net/url QUOTES THE OFFENDING INPUT. A body with a broken percent-escape makes
// ParseForm answer `invalid URL escape "%ZZ"`, so logging that error verbatim put
// about three bytes of the manager's note into the process log — on the ONE path
// that could leak any of it, in a file whose own rule is "THE NOTE IS NOT LOGGED".
// A security audit found it with a canary; this is that canary, kept.
//
// THE POSITIVE CONTROL IS THE LOG LINE ITSELF: the refusal must still be recorded,
// with a classified reason, or the fix would be "stop logging" rather than "stop
// logging the body".
func TestReviewDecision_AMalformedFormNeverLogsWhatWasSubmitted(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	records := queueWith(1)
	reviewer := &fakeReviewer{}
	h, err := NewAdminAuth(admins, &fakeTrail{}, records, records, reviewer, &fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, adminTestConfig(), logger)
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	// The canary sits either side of a broken escape, so whichever fragment the
	// parser decides to quote carries it.
	rec := b.doRaw(http.MethodPost, reviewHref,
		"id=x&outcome=approved&note=CANARYONE%ZZCANARYTWO")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a malformed body answered %d, want 303", rec.Code)
	}
	line := buf.String()

	// POSITIVE CONTROL: the refusal IS logged, and it says which kind.
	if !strings.Contains(line, "the form could not be read") {
		t.Fatalf("the refusal left no log line at all; the canary below would pass "+
			"over an empty buffer.\nlog: %q", line)
	}
	if !strings.Contains(line, "malformed form") {
		t.Errorf("the log does not classify the cause, so the fix removed the "+
			"diagnostic instead of the payload.\nlog: %q", line)
	}
	for _, canary := range []string{"CANARYONE", "CANARYTWO", "%ZZ"} {
		if strings.Contains(line, canary) {
			t.Errorf("the log carries %q from the submitted body. §4.7 and this file's "+
				"own reviewNote block say the note is not logged; net/url quotes the "+
				"input it choked on, so the ERROR may not be printed verbatim.\nlog: %q",
				canary, line)
		}
	}
	if n := len(reviewer.recorded()); n != 0 {
		t.Errorf("a malformed body reached the reviewer %d time(s)", n)
	}
}

// --- the gate ---------------------------------------------------------------

// TestReviewDecision_IsBehindTheSessionGate. The section itself is covered by
// TestPanelSections_EveryOneIsRoutedAndBehindTheGate, which ranges over the table;
// the POST is not in that table and has to be asserted on its own.
func TestReviewDecision_IsBehindTheSessionGate(t *testing.T) {
	reviewer := &fakeReviewer{}
	anonymous := newBrowser(t, newAdminRouterWithReviewer(t,
		&fakeAdmins{verify: func() (adminauth.Resolved, error) { return adminauth.Resolved{}, adminauth.ErrNoSession }},
		&fakeTrail{}, queueWith(1), reviewer))

	rec := anonymous.do(http.MethodPost, reviewHref, url.Values{
		"id": {uuid.NewString()}, "outcome": {"approved"},
	})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Errorf("anonymous POST %s: %d %q, want 303 /admin/login", reviewHref,
			rec.Code, rec.Header().Get("Location"))
	}
	if n := len(reviewer.recorded()); n != 0 {
		t.Errorf("an anonymous POST reached the reviewer %d time(s)", n)
	}
}

// TestReviewDecision_IsRefusedCrossOrigin is the net for the ONE middleware this
// task added, and it is written so that removing a.sameOriginGate from
// mountSections turns it red.
//
// 🔴 IT ASSERTS THE REVIEWER WAS NOT CALLED, not merely that the response was a
// redirect. A cross-origin POST that is refused AFTER the write has already
// happened is not a refusal, and status-only assertions cannot tell the two apart --
// which is exactly how M6-01's sameOriginGate was found to be deletable with the
// suite still green (the handler re-checked Origin, so the RESULT was the same and
// only the cost differed).
func TestReviewDecision_IsRefusedCrossOrigin(t *testing.T) {
	reviewer := &fakeReviewer{}
	b := panelBrowserWithReviewer(t, queueWith(1), reviewer)
	b.origin = "https://evil.example"

	rec := b.do(http.MethodPost, reviewHref, url.Values{
		"id": {uuid.NewString()}, "outcome": {"approved"},
	})
	if n := len(reviewer.recorded()); n != 0 {
		t.Errorf("a cross-origin POST recorded %d decision(s). A page on another origin "+
			"can drive a signed-in manager's session; this route WRITES.", n)
	}
	if rec.Code != http.StatusSeeOther {
		t.Errorf("a cross-origin POST answered %d, want 303", rec.Code)
	}

	// POSITIVE CONTROL: the same request from our own origin IS recorded. Without
	// it, a handler that refused everything would satisfy the assertion above.
	b.origin = testBaseURL
	if rec := b.do(http.MethodPost, reviewHref, url.Values{
		"id": {uuid.NewString()}, "outcome": {"approved"},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("same-origin POST answered %d, want 303", rec.Code)
	}
	if n := len(reviewer.recorded()); n != 1 {
		t.Errorf("a same-origin POST recorded %d decision(s), want 1 -- the negative "+
			"above would be vacuous over a route that records nothing at all", n)
	}
}

// TestReviewDecision_ACrossOriginRefusalCostsNoDatabaseWork is the half a status
// assertion cannot see, and it is the one an audit had to find.
//
// 🔴 "REFUSED" AND "REFUSED FOR FREE" ARE DIFFERENT CLAIMS. M6-04 first shipped
// sameOriginGate INSIDE the protected group — after requireAdmin and after
// sessionGate — while claiming it was "the same defence POST /admin/logout uses".
// The response was identical (303 to /admin), so every status-based assertion
// passed; what differed was that each refusal had already paid a session lookup AND
// spent a slot of the operator's own 300-request budget. Measured then:
//
//	cross-origin POST /admin/review  -> 303, resolver calls 1
//	300 of them                      -> the operator's own GET /admin answered 429
//
// A page on a different origin of the SAME SITE (a subdomain, the http twin) does
// send the Lax cookie, so this was a real way to lock a manager out of the queue
// they were meant to be clearing.
func TestReviewDecision_ACrossOriginRefusalCostsNoDatabaseWork(t *testing.T) {
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	reviewer := &fakeReviewer{}
	b := newBrowser(t, newAdminRouterWithReviewer(t, admins, &fakeTrail{}, queueWith(1), reviewer))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	// POSITIVE CONTROL FIRST: a same-origin POST DOES resolve. Without it, a router
	// that never reaches the resolver at all would satisfy the zero below.
	if rec := b.do(http.MethodPost, reviewHref, url.Values{
		"id": {uuid.NewString()}, "outcome": {"approved"},
	}); rec.Code != http.StatusSeeOther {
		t.Fatalf("same-origin POST answered %d, want 303", rec.Code)
	}
	baseline := admins.verifiedCount()
	if baseline == 0 {
		t.Fatal("a served POST resolved no session; the counter is not wired and the " +
			"measurement below would be meaningless")
	}

	// Now the refusals. adminSessionLimit of them: if any of these reached the
	// resolver, the operator's own budget would be gone by the end.
	b.origin = "https://evil.example"
	for i := 0; i < adminSessionLimit; i++ {
		if rec := b.do(http.MethodPost, reviewHref, url.Values{
			"id": {uuid.NewString()}, "outcome": {"approved"},
		}); rec.Code != http.StatusSeeOther {
			t.Fatalf("cross-origin POST #%d answered %d, want 303", i+1, rec.Code)
		}
	}
	if n := admins.verifiedCount() - baseline; n != 0 {
		t.Errorf("%d cross-origin POSTs cost %d session lookup(s), want 0.\n"+
			"adminlogin.go's rule for a state-changing route is that the Origin check "+
			"is 'a FREE refusal, BEFORE the resolver'. A gate mounted after it refuses "+
			"the request and pays for it anyway.", adminSessionLimit, n)
	}
	if n := len(reviewer.recorded()); n != 1 {
		t.Errorf("the reviewer was called %d time(s); only the one same-origin control "+
			"should have reached it", n)
	}

	// 🔴 AND THE OPERATOR'S OWN BUDGET SURVIVES, which is the harm the ordering
	// causes rather than the mechanism that causes it.
	b.origin = testBaseURL
	if rec := b.do(http.MethodGet, reviewHref, nil); rec.Code != http.StatusOK {
		t.Errorf("after %d cross-origin POSTs the operator's OWN GET %s answered %d, "+
			"want 200 -- a third party spent their session budget", adminSessionLimit,
			reviewHref, rec.Code)
	}
}

// TestSameOriginRefusal_NamesTheRouteItRefused.
//
// 🔴 THE GATE HAD ONE USER AND ITS LOG LINE WAS HARD-CODED TO THAT USER. It said
// "panel sign-out refused: not same-origin" whatever it had refused, so M6-04's
// decision endpoint inherited a message about a different route: a cross-origin
// attack on the panel's only WRITE would appear in the log as a sign-out event, and
// an incident response would look at the wrong endpoint. The two are only
// distinguishable by method and path, so both are asserted.
func TestSameOriginRefusal_NamesTheRouteItRefused(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	records := queueWith(1)
	h, err := NewAdminAuth(admins, &fakeTrail{}, records, records, &fakeReviewer{}, &fakeStaff{}, &fakeInviter{},
		&fakeVenues{}, &fakePlaques{}, adminTestConfig(), logger)
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)

	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	b.origin = "https://evil.example"

	for _, route := range []string{reviewHref, "/admin/logout"} {
		buf.Reset()
		b.do(http.MethodPost, route, url.Values{"id": {uuid.NewString()}, "outcome": {"approved"}})
		line := buf.String()
		if line == "" {
			t.Fatalf("a cross-origin POST %s logged nothing at WARN; the refusal leaves no "+
				"trace at all", route)
		}
		if !strings.Contains(line, route) {
			t.Errorf("the refusal of POST %s logged %q, which does not name the route.\n"+
				"One gate now guards two endpoints, and a message naming the wrong one "+
				"sends an incident response to the wrong place.", route, strings.TrimSpace(line))
		}
	}
}

// TestReviewDecision_AnAbsentOriginIsAlsoRefused. adminlogin.go's sameOrigin is
// STRICT: no Origin and no fetch metadata is a refusal, not a pass. That is the
// difference between this and the activation flow, and it is worth pinning where a
// write depends on it.
func TestReviewDecision_AnAbsentOriginIsAlsoRefused(t *testing.T) {
	reviewer := &fakeReviewer{}
	b := panelBrowserWithReviewer(t, queueWith(1), reviewer)
	b.origin = "" // browser.do omits the header entirely

	b.do(http.MethodPost, reviewHref, url.Values{
		"id": {uuid.NewString()}, "outcome": {"approved"},
	})
	if n := len(reviewer.recorded()); n != 0 {
		t.Errorf("a POST with no Origin and no fetch metadata recorded %d decision(s)", n)
	}
}

// --- the form and the route are one fact ------------------------------------

// formActionRE lifts the action off every POST form on a page.
var formActionRE = regexp.MustCompile(`(?is)<form\b[^>]*\bmethod\s*=\s*["']?post["']?[^>]*>`)

// TestReviewForm_PostsToTheRouteThatIsActuallyMounted.
//
// 🔴 IT EXISTS BECAUSE A SHIPPED VERSION OF THIS SCREEN WOULD HAVE 404'd ON EVERY
// BUTTON AND NOTHING WOULD HAVE GONE RED. The template wrote its action out as a
// literal while internal/handler/review.go claimed the URL was read from
// pages.PanelSections; an audit set the literal to /admin/nowhere, left the section
// table alone, and the whole handler package passed — real HTTP, real Postgres
// included.
//
// 🔴 THE ASSERTION IS NOT "the action equals a string I typed here". That is the
// same defect one level up: a test literal goes stale exactly when the product
// does. What is asserted is that the address the FORM RENDERS is (a) the address
// the SECTION TABLE names, and (b) an address that is really mounted and really
// records a decision. Both halves are derived; neither can be satisfied by a stale
// constant.
func TestReviewForm_PostsToTheRouteThatIsActuallyMounted(t *testing.T) {
	reviewer := &fakeReviewer{}
	b := panelBrowserWithReviewer(t, queueWith(2), reviewer)
	body := htmlOf(t, b.do(http.MethodGet, reviewHref, nil))

	forms := formActionRE.FindAllString(body, -1)
	if len(forms) == 0 {
		t.Fatal("the review section renders no POST form at all; every check below " +
			"would pass over nothing")
	}

	// The href the SECTION TABLE names, derived rather than typed.
	var want string
	for _, s := range pages.PanelSections {
		if s.Tab == pages.TabReview {
			want = s.Href
		}
	}
	if want == "" {
		t.Fatal("pages.PanelSections has no review section; this test is measuring nothing")
	}

	decisionForms := 0
	for _, tag := range forms {
		m := attrValueRE("action").FindStringSubmatch(tag)
		if len(m) != 2 {
			continue // the sign-out form in the chrome carries its own action
		}
		got := m[1]
		if got != want && !strings.HasSuffix(got, "/logout") {
			t.Errorf("a decision form posts to %q; the review section's own URL is %q.\n"+
				"The form target and the route are ONE fact and it lives in "+
				"pages.PanelSections. A hand-written literal here 404s in the browser "+
				"and passes every other test in this package.", got, want)
			continue
		}
		if got == want {
			decisionForms++
		}
	}
	if decisionForms != 2 {
		t.Fatalf("found %d decision form(s) pointing at %q, want 2 (one per queued "+
			"record)", decisionForms, want)
	}

	// 🔴 AND THE ADDRESS IS REALLY MOUNTED. Agreeing with the table is not enough --
	// the table could name a path nobody registered. This drives the exact address
	// the page rendered and requires a decision to come out the other end.
	rec := b.do(http.MethodPost, want, url.Values{
		"id": {uuid.NewString()}, "outcome": {"approved"},
	})
	if rec.Code == http.StatusNotFound {
		t.Fatalf("POST %s (the address the form renders) answered 404 -- the form points "+
			"somewhere nothing is mounted", want)
	}
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s answered %d, want 303", want, rec.Code)
	}
	if n := len(reviewer.recorded()); n != 1 {
		t.Errorf("POST %s recorded %d decision(s), want 1 -- the address answers but "+
			"does not reach the reviewer", want, n)
	}
}

// TestReviewConfirmation_IsCheckedAgainstTheDatabase is T5's net.
//
// 🔴 THE BANNER WAS PRINTABLE FROM A LINK. A security audit measured it:
// GET /admin/review?done=approved&clipped=1 rendered "Recorded as approved" and the
// note-was-cut sentence with ZERO review rows in the database, and the URL could be
// sent to somebody else. No XSS, no lost record — and a product stating a fact it
// had not looked up, which is the class M5-11 closed and this task has now paid for
// four times.
func TestReviewConfirmation_IsCheckedAgainstTheDatabase(t *testing.T) {
	records := queueWith(0)
	b := panelBrowserWith(t, records)

	real := uuid.New()
	records.decided(real, "approved")

	// POSITIVE CONTROL: a decision that IS on record confirms. Without it, a handler
	// that never renders the banner would satisfy every negative below.
	body := htmlOf(t, b.do(http.MethodGet,
		reviewHref+"?done=approved&for="+real.String(), nil))
	if !strings.Contains(body, "Recorded as approved") {
		t.Fatalf("a decision that IS on record does not confirm.\nbody: %s", truncateForMsg(body))
	}

	for _, tc := range []struct{ name, query string }{
		{"a decision nobody made", "?done=approved&for=" + uuid.NewString()},
		{"no record named at all", "?done=approved"},
		{"an id that is not a uuid", "?done=approved&for=not-a-uuid"},
		{"the WRONG outcome for a real decision", "?done=rejected&for=" + real.String()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := htmlOf(t, b.do(http.MethodGet, reviewHref+tc.query, nil))
			for _, claim := range []string{"Recorded as approved", "Recorded as rejected"} {
				if strings.Contains(body, claim) {
					t.Errorf("the panel printed %q for %s. The banner must be a measurement, "+
						"not an echo of the address bar.", claim, tc.name)
				}
			}
			if strings.Contains(body, "only the first 500 were") {
				t.Errorf("the clip sentence rendered for %s; it rides on a confirmation "+
					"that was never established", tc.name)
			}
		})
	}

	// A FAILED CHECK IS NOT A CONFIRMATION, and it is not a 500 either: the queue is
	// what the page is for and it answered.
	records.decisionErr = errors.New("database is unreachable")
	rec := b.do(http.MethodGet, reviewHref+"?done=approved&for="+real.String(), nil)
	if rec.Code != http.StatusOK {
		t.Errorf("a failed confirmation check answered %d; the section itself is still "+
			"readable and must still render", rec.Code)
	}
	if strings.Contains(htmlOf(t, rec), "Recorded as approved") {
		t.Error("the panel confirmed a decision it could not read")
	}
	records.decisionErr = nil

	// 🔴 AND THE TENANT IS THE SESSION'S. The lookup must not be reachable for
	// another business, or the confirmation becomes an existence oracle for foreign
	// record ids.
	records.mu.Lock()
	asked := append([]uuid.UUID(nil), records.decisionTenant...)
	records.mu.Unlock()
	if len(asked) == 0 {
		t.Fatal("the confirmation never consulted the ledger at all")
	}
	for _, id := range asked {
		if id != panelTestTenant {
			t.Errorf("the confirmation looked up tenant %s; the session's tenant is %s",
				id, panelTestTenant)
		}
	}
}

// --- the request budget -----------------------------------------------------

// TestReviewBudget_ADecisionCostsTwoChargedRequests MEASURES the number
// adminratelimit.go's new paragraph is derived from, rather than asserting the
// arithmetic somebody wrote there.
//
// 🔴 THE DENOMINATOR IS "PER DECISION", NOT "PER VIEW", and naming it is the whole
// point: this task adds a second way to spend the session budget, and it is the
// first one in the panel where a manager's ordinary work costs more than one
// request each time. A decision is POST + the 303's GET.
func TestReviewBudget_ADecisionCostsTwoChargedRequests(t *testing.T) {
	b := panelBrowserWithReviewer(t, queueWith(0), &fakeReviewer{})

	if adminSessionLimit >= adminFloodLimit {
		t.Fatalf("adminSessionLimit (%d) is not below adminFloodLimit (%d); this test "+
			"could not tell the two gates apart", adminSessionLimit, adminFloodLimit)
	}

	// 🔴 THE COUNTERS ARE SERVED AND REFUSED, SEPARATELY, and the first version of
	// this test did not separate them: it divided TOTAL requests (301, including the
	// one that was refused) by completed decisions (150) and printed 2.007 under the
	// label "charged requests per decision". Same class of mistake this session has
	// paid for repeatedly -- a number measured over one denominator and reported
	// under another.
	served, refused, decisions := 0, 0, 0
	for served+refused < adminSessionLimit+10 {
		rec := b.do(http.MethodPost, reviewHref, url.Values{
			"id": {uuid.NewString()}, "outcome": {"approved"},
		})
		if rec.Code == http.StatusTooManyRequests {
			refused++
			break
		}
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("decision %d answered %d", decisions+1, rec.Code)
		}
		served++
		got := b.do(http.MethodGet, rec.Header().Get("Location"), nil)
		if got.Code == http.StatusTooManyRequests {
			refused++
			break
		}
		served++
		decisions++
	}

	t.Logf("MEASURED: %d served + %d refused; %d complete decisions = %.3f SERVED "+
		"requests per decision (session ceiling %d)",
		served, refused, decisions, float64(served)/float64(decisions), adminSessionLimit)

	if decisions == 0 {
		t.Fatal("no decision completed at all; this measured nothing")
	}
	if served != 2*decisions {
		t.Errorf("%d served requests bought %d decisions, i.e. %.3f per decision rather "+
			"than 2. adminratelimit.go's derivation is written in terms of that number "+
			"and has to be re-derived with it, rather than this expectation being "+
			"adjusted to whatever the code does.",
			served, decisions, float64(served)/float64(decisions))
	}
	if served != adminSessionLimit {
		t.Errorf("%d requests were served before the ceiling bit, want exactly %d",
			served, adminSessionLimit)
	}
	// The budget must actually bite: a flow that is never refused would satisfy the
	// ratio above while proving the gate is not in the chain at all.
	if refused == 0 {
		t.Errorf("%d served requests never met the ceiling of %d; the POST route is "+
			"outside sessionGate", served, adminSessionLimit)
	}
}

// --- the docket -------------------------------------------------------------

// TestTransactionDocket_ShowsTheEngineVerdictAndTheManagerDecisionSeparately is
// §4.3 made visible, and it is the assertion that stops the obvious "improvement":
// swapping the FLAGGED stamp for APPROVED once a manager has approved.
//
// 🔴 THAT WOULD BE Q20's FAILURE PERFORMED IN THE READ PATH. The whole reason the
// decision lives in another table is that the tap record does not change; a screen
// that renders it as changed makes the guarantee invisible, and the next person to
// read the code would have no reason to keep it.
func TestTransactionDocket_ShowsTheEngineVerdictAndTheManagerDecisionSeparately(t *testing.T) {
	fake := newFakeLedger()
	fake.page.Records = []ledger.Record{
		{ID: uuid.New(), OccurredAt: time.Now().UTC(), Verdict: "flag", Channel: "nfc",
			EmployeeName: "Approved Person", Queued: true, Review: "approved"},
		{ID: uuid.New(), OccurredAt: time.Now().UTC(), Verdict: "flag", Channel: "nfc",
			EmployeeName: "Rejected Person", Queued: true, Review: "rejected"},
		{ID: uuid.New(), OccurredAt: time.Now().UTC(), Verdict: "flag", Channel: "nfc",
			EmployeeName: "Waiting Person", Queued: true},
	}
	b := panelBrowserWith(t, fake)
	body := htmlOf(t, b.do(http.MethodGet, transactionsHref, nil))

	cards := map[string]string{}
	for _, card := range strings.Split(body, "<article") {
		for _, who := range []string{"Approved Person", "Rejected Person", "Waiting Person"} {
			if strings.Contains(card, who) {
				cards[who] = card
			}
		}
	}
	if len(cards) != 3 {
		t.Fatalf("isolated %d docket(s), want 3", len(cards))
	}

	for who, card := range cards {
		if !strings.Contains(card, "FLAGGED") {
			t.Errorf("%s's docket lost its FLAGGED stamp. The engine's verdict is "+
				"immutable (§4.3) and the screen must go on showing it.", who)
		}
		if strings.Contains(card, "APPROVED") || strings.Contains(card, "REJECTED") {
			t.Errorf("%s's docket carries a second STAMP. A manager's decision is a tally, "+
				"not a verdict; two stamps would say the record was re-judged.", who)
		}
	}
	for who, want := range map[string]string{
		"Approved Person": "Approved by a manager",
		"Rejected Person": "Rejected by a manager",
		"Waiting Person":  "Waiting for review",
	} {
		if !strings.Contains(cards[who], want) {
			t.Errorf("%s's docket does not say %q.\ncard: %s", who, want, truncateForMsg(cards[who]))
		}
	}
	// A DECIDED RECORD MUST NOT STILL SAY IT IS WAITING. transactions.queued can
	// never be cleared (§4.3), so the template has to gate on the review instead --
	// and if it gates on `queued` alone, this is what catches it.
	for _, who := range []string{"Approved Person", "Rejected Person"} {
		if strings.Contains(cards[who], "Waiting for review") {
			t.Errorf("%s's record has been decided and the docket still says it is waiting. "+
				"queued is a column that cannot be cleared; the tally must read the "+
				"review, not the column.", who)
		}
	}
}

// --- helpers ----------------------------------------------------------------

func panelBrowserWithReviewer(t *testing.T, records *fakeLedger, reviewer panelReviewer) *browser {
	t.Helper()
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
	b := newBrowser(t, newAdminRouterWithReviewer(t, admins, &fakeTrail{}, records, reviewer))
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

func isValidUTF8(s string) bool { return utf8.ValidString(s) }
