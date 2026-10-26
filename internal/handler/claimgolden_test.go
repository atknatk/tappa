package handler

// claimgolden_test.go -- the claim card's screens, byte for byte (M15-17, ADR 0032 §C.1–§C.5): the
// result screen in each shape that carries a card, and every answer of POST /api/claim.
//
// THESE ARE NEW SCREENS, so their goldens were written from this card's own render -- there is no
// "before" for a screen that did not exist (M15-12's reasoning for its two directed tap screens).
// What the card must NOT move is held elsewhere and unchanged: every screen without a card is
// TestUnbrandedScreens_AreByteIdenticalToTheGolden's, rendered through noClaims. The write mode
// (TAPPA_WRITE_CLAIM_GOLDEN=1) writes only files that do not exist and then fails, the unbranded
// golden's rule: replacing a golden means deleting it first.
//
// The signed claim context carries its mint time, so its value is masked and its presence
// asserted (maskTapContext: exactly one ctx field per card screen).

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/domain/checkin"
	"github.com/atknatk/tappa/internal/domain/claim"
	"github.com/atknatk/tappa/internal/domain/tap"
)

const claimGoldenDir = "testdata/claim-golden"

// claimScreenCount is how many renders the golden holds; a render and its file deleted together
// keep the list and the directory in step, and this number is what notices.
const claimScreenCount = 13

func claimScreenRenders(t *testing.T) []screenGolden {
	t.Helper()
	var out []screenGolden
	result := func(name string, res checkin.Result, card *claim.Card) {
		h, tp := newClaimHandler(t, &fakeCheckins{result: res}, &fakeClaims{card: card})
		r := readRecorded(t, name, postTapThrough(t, h, tp))
		r.body = maskTapContext(t, name, r.body)
		out = append(out, r)
	}
	// Yesterday's open check-in, the shift's end prefilled; on ok and on flag (N5: the closing
	// sentence under the card), and with no shift (the field empty and required).
	result("result-ok-in-card-yesterday-prefilled", cardResult("ok"), outCard(true))
	result("result-flag-in-card-yesterday-prefilled", cardResult("flag"), outCard(true))
	result("result-ok-in-card-yesterday-no-shift", cardResult("ok"), outCard(false))
	// The split shift (Ü-O2): 11:00 in, "Not right? Clock in" at 18:00 -- same day; the screen's
	// note keeps "you chose to clock in" and loses the left-open part (N6).
	split := cardResult("ok")
	split.OccurredAt = time.Date(2026, 7, 31, 16, 0, 0, 0, time.UTC)
	split.Decision.EmployeeNote = noteIPMatched + "; " + noteLeftOpenScreen + "; " + tap.ChoseClockInEmployeeNote
	split.Decision.EmployeeNoteAsked = noteIPMatched + "; " + tap.ChoseClockInEmployeeNote
	sameDay := &claim.Card{Kind: claim.KindOut, AnchorID: cardAnchorID, AnchorAt: time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC),
		Window: claim.Window{From: claim.Bound{At: time.Date(2026, 7, 31, 9, 0, 0, 0, time.UTC)}, To: claim.Bound{At: split.OccurredAt}}}
	result("result-ok-in-card-same-day", split, sameDay)
	// An older open check-in, named by its day.
	older := outCard(true)
	older.AnchorAt = time.Date(2026, 7, 27, 7, 12, 0, 0, time.UTC)
	result("result-ok-in-card-earlier-day", cardResult("ok"), older)
	// "Not right? Clock out" with no check-in before it: the check-in claim on the out's own screen.
	chosenOut := cardResult("ok")
	chosenOut.Decision = withNote(decisionOf("ok", "out", false), noteIPMatched+"; "+tap.ChoseClockOutEmployeeNote)
	chosenOut.Decision.EmployeeNoteAsked = chosenOut.Decision.EmployeeNote
	start := time.Date(2026, 7, 31, 7, 0, 0, 0, time.UTC)
	in := &claim.Card{Kind: claim.KindIn, AnchorID: cardTapID, AnchorAt: cardTapAt, Prefilled: &start,
		Window: claim.Window{From: claim.Bound{At: time.Date(2026, 7, 31, 3, 0, 0, 0, time.UTC), Inclusive: true}, To: claim.Bound{At: cardTapAt}}}
	result("result-ok-out-card-check-in", chosenOut, in)

	// --- POST /api/claim's answers -------------------------------------------------------------
	answer := func(name string, err error, at string, masked bool, mutate func(*claimContext)) {
		fc := &fakeClaims{submitErr: err}
		h, tp := newClaimHandler(t, &fakeCheckins{}, fc)
		cc := julyContext()
		if mutate != nil {
			mutate(&cc)
		}
		r := readRecorded(t, name, claimPost(t, h, url.Values{"ctx": {mintClaim(t, tp, tapFixedSessionID, cc)}, "at": {at}}))
		if masked {
			r.body = maskTapContext(t, name, r.body)
		}
		out = append(out, r)
	}
	answer("claim-sent", nil, "17:00", false, nil)
	answer("claim-form-outside-window", claim.ErrOutsideWindow, "08:00", true, nil)
	answer("claim-form-empty", nil, "", true, nil)
	answer("claim-problem-expired", claim.ErrExpired, "17:00", false, nil)
	answer("claim-problem-resolved", claim.ErrResolved, "17:00", false, nil)
	answer("claim-problem-not-active", claim.ErrNotActive, "17:00", false, nil)
	answer("claim-problem-server", errors.New("connection reset by peer"), "17:00", false, nil)
	return out
}

// TestClaimScreens_AreByteIdenticalToTheGolden: every listed render of the claim card's screens is
// byte-identical -- status, Content-Security-Policy and body -- to its golden; the list, the
// directory and claimScreenCount name the same renders.
func TestClaimScreens_AreByteIdenticalToTheGolden(t *testing.T) {
	renders := claimScreenRenders(t)
	if len(renders) != claimScreenCount {
		t.Errorf("the golden list has %d renders, want %d", len(renders), claimScreenCount)
	}
	write := os.Getenv("TAPPA_WRITE_CLAIM_GOLDEN") == "1"
	seen := map[string]bool{}
	var written []string
	for _, r := range renders {
		if seen[r.name] {
			t.Fatalf("two renders are named %q", r.name)
		}
		seen[r.name] = true
		path := filepath.Join(claimGoldenDir, r.name+".golden")
		got := goldenBytes(r)
		want, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) && write {
			if err := os.MkdirAll(claimGoldenDir, 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatalf("writing %s: %v", path, err)
			}
			written = append(written, r.name)
			continue
		}
		if err != nil {
			t.Fatalf("%s: no golden (%v)", r.name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s differs from its golden.\n got:\n%s\nwant:\n%s", r.name, got, want)
		}
	}
	entries, err := os.ReadDir(claimGoldenDir)
	if err != nil {
		t.Fatalf("reading %s: %v", claimGoldenDir, err)
	}
	for _, e := range entries {
		if n := strings.TrimSuffix(e.Name(), ".golden"); !seen[n] {
			t.Errorf("golden %s is rendered by nothing", e.Name())
		}
	}
	if len(entries) != claimScreenCount && len(written) == 0 {
		t.Errorf("%s holds %d files, want %d", claimGoldenDir, len(entries), claimScreenCount)
	}
	if len(written) > 0 {
		t.Fatalf("wrote %d missing golden file(s): %s -- unset TAPPA_WRITE_CLAIM_GOLDEN and rerun", len(written), strings.Join(written, ", "))
	}
}

// TestClaimScreens_RenderOnlyTheseElements is TestScreens_RenderOnlyTheseElements' closed set for
// the card's family: the result screen with a card adds exactly the form, its two inputs, a label
// and the send button; the answers of POST /api/claim are the result shell, the card again, or the
// shared failure screen.
func TestClaimScreens_RenderOnlyTheseElements(t *testing.T) {
	got := map[string]bool{}
	for _, r := range claimScreenRenders(t) {
		collectTags(string(r.body), got)
	}
	assertTagSet(t, "claim card", got, []string{
		"html", "head", "meta", "title", "link", "body", "main", "header",
		"section", "div", "p", "h1", "span", "dl", "dt", "dd",
		"form", "input", "label", "button",
	})
}

// TestResultScreen_TheCardDrawsTheLogoAndNoAccent is D-C with the card: a business with a logo and
// an accent gets the logo and still no theme link, and outside the header the screen is the one a
// business with no brand gets -- the card's send button is the palette's, never the accent's slot.
func TestResultScreen_TheCardDrawsTheLogoAndNoAccent(t *testing.T) {
	body := func(dir *fakeDirectory) string {
		tp, err := NewTap(&fakePreviewer{preview: okPreview(true)}, dir, fixedSessions(), &fakeCheckins{result: cardResult("ok")},
			&fakeClaims{card: outCard(true)}, noActivation{}, &fakeAudit{}, tapCfg(), slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		a := readAnswer(t, postTapThrough(t, mountTap(tp), tp))
		return string(maskTapContext(t, "card", a.body))
	}
	branded := body(&fakeDirectory{facts: okFacts(), resultBrand: testPageBrand(t, true, true)})
	plain := body(&fakeDirectory{facts: okFacts()})
	if strings.Contains(branded, "/brand/theme/") || strings.Count(branded, `rel="stylesheet"`) != 1 {
		t.Error("the result screen with a card links a theme")
	}
	for _, slot := range []string{"tap-button", "bg-brand", "text-on-brand", "brand-edge"} {
		if strings.Contains(branded, slot) {
			t.Errorf("the result screen with a card uses the accent's slot %q", slot)
		}
	}
	if !strings.Contains(branded, "/t/logo/"+brandLogoSHA) || !strings.Contains(branded, "data-claim-card") {
		t.Fatal("control: the logo or the card is missing")
	}
	if withoutBrandSlots([]byte(branded)) != withoutBrandSlots([]byte(plain)) {
		t.Errorf("outside the header the screen with a card differs:\n%s\n%s", branded, plain)
	}
}
