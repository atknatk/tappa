package handler

// tapprediction_test.go -- M15-12 against FAKES, through the mounted router: the tap screen says
// the predicted direction (ADR 0032 §B.1, the user's §9 decision of 2026-10-21) only through its
// gate (§B.2; security lens G-O3, F-1; K5), the correction is a submit button of the one form and
// never the accent's, and tap.js writes the pressed direction before anything else (§B.3). The
// real-Postgres half -- the directory's half of the gate, the server's clock, the peer's plaque --
// is tapprediction_db_test.go.

import (
	"bytes"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/domain/tap"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/web"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The three shapes of the form's buttons, as the screen writes them.
const (
	undirectedButton = `<button type="submit" class="tap-button" data-tap-button>Tap</button>`
	clockInButton    = `<button type="submit" name="want" value="in" class="tap-button" data-tap-button>Clock in</button>`
	clockOutButton   = `<button type="submit" name="want" value="out" class="tap-button" data-tap-button>Clock out</button>`
	correctionClass  = `mt-2 inline-flex min-h-11 items-center self-center px-3 text-base text-ink underline underline-offset-4 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2`
	correctToOut     = `<button type="submit" name="want" value="out" class="` + correctionClass + `" data-tap-correction>Not right? Clock out</button>`
	correctToIn      = `<button type="submit" name="want" value="in" class="` + correctionClass + `" data-tap-correction>Not right? Clock in</button>`
)

// predictionAt is a business day's start for the fakes' predictions: any instant will do, the
// screen reads only that there is one.
var predictionAt = time.Date(2026, 10, 22, 3, 0, 0, 0, time.UTC)

// predicted is okFacts with the directory's prediction d and a business day.
func predicted(d tap.Type) tenant.TapPageFacts {
	f := okFacts()
	s := predictionAt
	f.Prediction = &tap.Prediction{Direction: d, BusinessDayStart: &s}
	return f
}

// tapFormOf is the page's one form, and fails unless there is exactly one.
func tapFormOf(t *testing.T, body string) string {
	t.Helper()
	if n := strings.Count(body, "<form"); n != 1 {
		t.Fatalf("%d forms on the tap screen, want exactly 1 (a second form would carry no signed context)", n)
	}
	i := strings.Index(body, "<form")
	j := strings.Index(body[i:], "</form>")
	if j < 0 {
		t.Fatal("the form is not closed")
	}
	return body[i : i+j+len("</form>")]
}

// staticWantRE finds a want field written into the HTML: tap.js adds the one hidden field at
// the press (ADR 0032 §B.3), so the served page carries none.
var staticWantRE = regexp.MustCompile(`<input[^>]*name="want"`)

// assertDirection checks the form's buttons are exactly the shape d: the primary button and, for
// a direction, the correction under it -- nothing else, no static want, no link.
func assertDirection(t *testing.T, body string, d pages.TapDirection) {
	t.Helper()
	form := tapFormOf(t, body)
	var want string
	switch d {
	case pages.TapClockIn:
		want = clockInButton + correctToOut
	case pages.TapClockOut:
		want = clockOutButton + correctToIn
	default:
		want = undirectedButton
	}
	if !strings.HasSuffix(form, want+"</form>") {
		t.Errorf("the form does not end with exactly the buttons of shape %d:\nwant …%s</form>\n got %s", d, want, form)
	}
	buttons := map[pages.TapDirection]int{pages.TapClockIn: 2, pages.TapClockOut: 2}[d]
	if buttons == 0 {
		buttons = 1
	}
	if n := strings.Count(body, "<button"); n != buttons {
		t.Errorf("%d buttons, want %d", n, buttons)
	}
	if n := strings.Count(body, `class="tap-button"`); n != 1 {
		t.Errorf("%d tap-button classes, want exactly 1: one primary button (§9)", n)
	}
	if staticWantRE.MatchString(body) {
		t.Error("the HTML carries a static want field: a second, unpressed value (ADR 0032 §B.3)")
	}
	if n := strings.Count(body, `name="want"`); n != map[bool]int{true: 2}[d != pages.TapUndirected] {
		t.Errorf("%d want names, want one per button of a direction and none for Tap", n)
	}
	for _, forbidden := range []string{"<a ", "<nav", "<select", "<details"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the tap screen contains %q: the correction is a button of the form, not a link (§9)", forbidden)
		}
	}
	for _, field := range []string{"lat", "lng", "acc"} {
		if !strings.Contains(form, `name="`+field+`" value=""`) {
			t.Errorf("the %s field is missing or filled: GPS is read at the press only (§4.2)", field)
		}
	}
}

// TestTapPage_SaysThePredictedDirectionInThreeShapes is the card's first criterion: the
// directory's prediction `in`, `out` and none, through the mounted router -- "Clock in" with
// "Not right? Clock out" under it, "Clock out" with "Not right? Clock in", and the
// direction-neutral "Tap" with nothing under it. Each button sends what it says; none of the three
// pages carries a static want, a second form or a link.
func TestTapPage_SaysThePredictedDirectionInThreeShapes(t *testing.T) {
	for _, c := range []struct {
		name  string
		facts tenant.TapPageFacts
		want  pages.TapDirection
	}{
		{"predicted in", predicted(tap.TypeIn), pages.TapClockIn},
		{"predicted out", predicted(tap.TypeOut), pages.TapClockOut},
		{"no prediction", okFacts(), pages.TapUndirected},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := &fakeDirectory{facts: c.facts}
			h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, dir, fixedSessions())
			w := get(t, h, tapURL(), sessionCookie())
			if w.Code != http.StatusOK {
				t.Fatalf("status %d", w.Code)
			}
			if !dir.gotPredict {
				t.Fatal("a verified NFC tap on an active plaque did not ask the directory for a prediction")
			}
			assertDirection(t, w.Body.String(), c.want)
		})
	}
}

// TestTapPage_ThePlaqueAndChannelHalfOfTheGate is the gate's handler half (ADR 0032 §B.2;
// security lens G-O3; K5) over every plaque status and both channels: the handler asks the
// directory for a prediction -- and the page says it -- for an NFC URL whose CMAC verified and for a
// QR URL, on an ACTIVE plaque; never for a CMAC that did not verify, nor for a retired, lost or
// unassigned plaque on either channel. The directory's fake answers a prediction whenever it is
// asked, so a page that shows one without asking is impossible and a gate that asks too often is
// measured here.
func TestTapPage_ThePlaqueAndChannelHalfOfTheGate(t *testing.T) {
	qr := "/t?tag=" + tapUID
	for _, c := range []struct {
		name    string
		target  string
		preview sun.Preview
		allowed bool
	}{
		{"NFC, CMAC verified, active", tapURL(), okPreview(true), true},
		{"NFC, CMAC did not verify, active", tapURL(), okPreview(false), false},
		{"NFC, retired", tapURL(), previewWithStatus(false, "retired"), false},
		{"NFC, lost", tapURL(), previewWithStatus(false, "lost"), false},
		{"NFC, unassigned", tapURL(), previewInStock(), false},
		// A preview says CMACValid=true only for an active plaque (internal/sun); the case below
		// is a fake's impossibility that pins the status check on its own.
		{"NFC, CMAC verified, retired (status alone refuses)", tapURL(), previewWithStatus(true, "retired"), false},
		{"QR, active (K5)", qr, previewWithStatus(false, "active"), true},
		{"QR, retired", qr, previewWithStatus(false, "retired"), false},
		{"QR, lost", qr, previewWithStatus(false, "lost"), false},
		{"QR, unassigned", qr, previewInStock(), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := &fakeDirectory{facts: predicted(tap.TypeOut)}
			h, _ := newTapHandler(t, &fakePreviewer{preview: c.preview}, dir, fixedSessions())
			w := get(t, h, c.target, sessionCookie())
			if w.Code != http.StatusOK {
				t.Fatalf("status %d: the page renders for every plaque state (§4.6)", w.Code)
			}
			if dir.gotPredict != c.allowed {
				t.Errorf("the handler asked for a prediction: %v, want %v", dir.gotPredict, c.allowed)
			}
			want := pages.TapUndirected
			if c.allowed {
				want = pages.TapClockOut
			}
			assertDirection(t, w.Body.String(), want)
		})
	}
}

// TestTapPage_APredictionWithoutABusinessDayIsUndirected is the M15-10 lens's hand-off: a
// prediction whose business day could not be resolved (a zone the server cannot load) is the
// engine's "no business day", where the decision DROPS a request (ADR 0032 §B.5) -- so the screen
// offers none: the direction-neutral "Tap", no want, no "Not right?". In both directions.
func TestTapPage_APredictionWithoutABusinessDayIsUndirected(t *testing.T) {
	for _, d := range []tap.Type{tap.TypeIn, tap.TypeOut} {
		f := okFacts()
		f.Prediction = &tap.Prediction{Direction: d}
		h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: f}, fixedSessions())
		assertDirection(t, get(t, h, tapURL(), sessionCookie()).Body.String(), pages.TapUndirected)
	}
}

// TestTapPage_APredictionIsDrawnOnlyWhenTheDirectoryAnsweredCleanly: a failed prediction read
// (PredictionErr) is the "Tap" page and one ERROR line, never a failed page (§4.6); and a
// prediction the directory handed back WITH ErrForeignLocation or ErrBrandUnread is not drawn --
// the page's rule for the brand (look) is the rule for the direction.
func TestTapPage_APredictionIsDrawnOnlyWhenTheDirectoryAnsweredCleanly(t *testing.T) {
	failed := okFacts()
	failed.PredictionErr = errors.New("prediction read failed")
	var buf bytes.Buffer
	dir := &fakeDirectory{facts: failed}
	h, tp := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, dir, fixedSessions())
	tp.log = slog.New(slog.NewJSONHandler(&buf, nil))
	w := get(t, h, tapURL(), sessionCookie())
	if w.Code != http.StatusOK {
		t.Fatalf("a failed prediction read cost the page: status %d", w.Code)
	}
	assertDirection(t, w.Body.String(), pages.TapUndirected)
	if n := strings.Count(buf.String(), "reading the predicted direction failed"); n != 1 {
		t.Errorf("%d error lines for the failed prediction read, want 1:\n%s", n, buf.String())
	}

	for _, err := range []error{tenant.ErrForeignLocation, tenant.ErrBrandUnread} {
		f := predicted(tap.TypeOut)
		if errors.Is(err, tenant.ErrForeignLocation) {
			f.LocationName = ""
		}
		h, _ := newTapHandler(t, &fakePreviewer{preview: okPreview(true)}, &fakeDirectory{facts: f, err: err}, fixedSessions())
		assertDirection(t, get(t, h, tapURL(), sessionCookie()).Body.String(), pages.TapUndirected)
	}
}

// TestTapPage_APeersPlaqueAsksWithTheSameGate: on a peer's plaque (M11 API-1b) the handler passes
// the same plaque-and-channel half to PeerTapPage, and the peer read's prediction is what the
// button says -- the employer's chain, which the directory reads on the session's context
// (tapprediction_db_test.go measures that half).
func TestTapPage_APeersPlaqueAsksWithTheSameGate(t *testing.T) {
	peer := uuid.New()
	for _, c := range []struct {
		name    string
		preview sun.Preview
		allowed bool
	}{
		{"verified", okPreview(true), true},
		{"not verified", okPreview(false), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			pf := predicted(tap.TypeOut)
			pf.LocationName = "KF Sliema"
			dir := &fakeDirectory{
				facts: tenant.TapPageFacts{EmployeeName: "Maria Borg"}, err: tenant.ErrForeignLocation,
				peerFacts: pf, peerSite: peer,
			}
			h, _ := newTapHandler(t, &fakePreviewer{preview: c.preview}, dir, fixedSessions())
			body := get(t, h, tapURL(), sessionCookie()).Body.String()
			if dir.peerPredict != c.allowed || dir.gotPredict != c.allowed {
				t.Errorf("predict to the page read %v and the peer read %v, want %v for both", dir.gotPredict, dir.peerPredict, c.allowed)
			}
			if !strings.Contains(body, "KF Sliema") {
				t.Fatal("anti-vacuity: the peer's venue is not on the page")
			}
			want := pages.TapUndirected
			if c.allowed {
				want = pages.TapClockOut
			}
			assertDirection(t, body, want)
		})
	}
}

// TestTapPage_ThePredictionCarriesNoPlaceAndNoCounter: GET /t reads no position (§4.2) and the
// page receives no counter (§4.4): neither the directory's facts, nor the engine's prediction,
// nor the view has a field a coordinate or a counter could travel in.
func TestTapPage_ThePredictionCarriesNoPlaceAndNoCounter(t *testing.T) {
	bad := regexp.MustCompile(`(?i)lat|lng|lon|gps|coord|accuracy|ctr|counter`)
	for _, typ := range []reflect.Type{reflect.TypeOf(tap.Prediction{}), reflect.TypeOf(tenant.TapPageFacts{}), reflect.TypeOf(pages.TapView{})} {
		for i := 0; i < typ.NumField(); i++ {
			if f := typ.Field(i); bad.MatchString(f.Name) {
				t.Errorf("%s.%s: a place or a counter has a field on the tap page's path", typ.Name(), f.Name)
			}
		}
	}
}

// TestTapScript_WritesThePressedDirectionFirst is the card's source-level criterion for tap.js
// (ADR 0032 §B.3), TestTapScript_DoublePressCannotEscapeAsANativeSubmit's pattern: the submit
// handler reads event.submitter and falls back to what the click handler remembered; it looks the
// hidden want field up before creating one (one field, however many submits); and that write comes
// BEFORE markPending, before the location branch and before form.submit(); and markPending
// disables every submit button, not only the primary one. A SOURCE net, not a behavioural proof --
// there is no JavaScript runtime in this repository (CLAUDE.md §1); the browser half is M15-14's.
func TestTapScript_WritesThePressedDirectionFirst(t *testing.T) {
	raw, err := fs.ReadFile(web.Static(), "js/tap.js")
	if err != nil {
		t.Fatalf("read the served script: %v", err)
	}
	src := string(raw)
	code := stripJSComments(src)

	handler := strings.Index(code, "form.addEventListener('submit'")
	if handler < 0 {
		t.Fatal("no submit handler")
	}
	body := code[handler:]
	write := strings.Index(body, "writeWant(pressedValue(event));")
	if write < 0 {
		t.Fatal("the submit handler does not write the pressed direction")
	}
	guard := strings.Index(body, "if (submitting) {")
	for name, at := range map[string]int{
		"markPending()":               strings.Index(body, "markPending()"),
		"if (!navigator.geolocation)": strings.Index(body, "if (!navigator.geolocation)"),
		"the position call":           strings.Index(body, "navigator.geolocation.getCurrentPosition("),
	} {
		if at < 0 || at < write {
			t.Errorf("%s comes before the want is written (or is gone): a disabled submitter, and form.submit(), send no value", name)
		}
	}
	if guard < 0 || guard > write {
		t.Error("the re-entry guard is not before the write: a second press could rewrite a press already under way")
	}
	if send := strings.Index(code, "form.submit()"); send < 0 || strings.Index(code, "function send()") > send {
		t.Fatal("anti-vacuity: no send() calling form.submit()")
	}
	// form.submit() lives in send(), which runs only after the handler's write: send is called
	// from finish, inside the handler past the write.
	if i := strings.Index(body, "send()"); i >= 0 && i < write {
		t.Error("send() is reached before the want is written")
	}

	for _, want := range []string{
		"event.submitter",          // the pressed button, where the browser reports it
		"addEventListener('click'", // the fallback's memory
		"return pressed;",          // the fallback's read
		`querySelector('input[type="hidden"][name="want"]')`, // looked up first
		"document.createElement('input')",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("the script lacks %q", want)
		}
	}
	fn := code[strings.Index(code, "function writeWant("):]
	fn = fn[:strings.Index(fn, "\n  }")]
	if look, create := strings.Index(fn, "form.querySelector("), strings.Index(fn, "createElement("); look < 0 || create < 0 || look > create {
		t.Errorf("writeWant does not look the field up before it creates one:\n%s", fn)
	}
	if n := strings.Count(code, "createElement("); n != 1 {
		t.Errorf("%d createElement calls, want 1 (one hidden want field)", n)
	}
	mp := code[strings.Index(code, "function markPending("):]
	mp = mp[:strings.Index(mp, "\n  }")]
	if !strings.Contains(mp, "buttons[j].disabled = true;") {
		t.Errorf("markPending does not disable every submit button:\n%s", mp)
	}
	if !strings.Contains(code, `form.querySelectorAll('button[type="submit"]')`) {
		t.Error("the buttons markPending disables are not the form's submit buttons")
	}
	if n := strings.Count(src, "navigator.geolocation.getCurrentPosition("); n != 1 {
		t.Errorf("%d position calls, want exactly 1 (§4.2)", n)
	}
}

// TestTapScript_APageRestoredFromTheCacheReloadsItself is the user's decisions of 2026-10-23
// (M15-14), TestTapScript_DoublePressCannotEscapeAsANativeSubmit's pattern: back from the
// confirmation screen, a page the browser restored from its back-forward cache came back with
// `submitting` set and both buttons disabled, so a second press posted nothing (measured in
// Chrome 156). A QR page reloads itself; an NFC page does not -- its counter is spent, and a press
// there records a sun-invalid reject that debounces the person's real tap. The channel is the
// form's server-rendered data-tap-channel. The pageshow listener is pinned WHOLE: a reload outside
// the condition (measured: 96 GETs in 5 s in Chrome) or a condition without the channel is red.
// It asks for no position (§4.2) and leaves the submit handler alone. A SOURCE net: the restore
// itself is measured in a browser (M15-14), not here.
func TestTapScript_APageRestoredFromTheCacheReloadsItself(t *testing.T) {
	raw, err := fs.ReadFile(web.Static(), "js/tap.js")
	if err != nil {
		t.Fatalf("read the served script: %v", err)
	}
	code := stripJSComments(string(raw))
	norm := func(s string) string { return strings.Join(strings.Fields(s), " ") }
	const listener = "window.addEventListener('pageshow', function (event) {"
	at := strings.Index(code, listener)
	if at < 0 {
		t.Fatal("no pageshow listener: a QR page restored from the back-forward cache keeps its disabled buttons")
	}
	block := code[at:]
	end := strings.Index(block, "\n  });")
	if end < 0 {
		t.Fatal("could not delimit the pageshow listener")
	}
	got := norm(block[:end+len("\n  });")])
	want := norm(listener + `
    if (event.persisted === true && restoredPageReloads) {
      window.location.reload();
    }
  });`)
	if got != want {
		t.Errorf("the pageshow listener is not exactly the restored-QR-page reload:\n got %s\nwant %s", got, want)
	}
	if !strings.Contains(code, `var restoredPageReloads = form.getAttribute('data-tap-channel') === 'qr';`) {
		t.Error("the reload is not tied to the server-rendered QR channel")
	}
	if n := strings.Count(code, "location.reload("); n != 1 {
		t.Errorf("%d reload calls, want exactly 1 -- the restored QR page's", n)
	}
	if n := strings.Count(code, "pageshow"); n != 1 {
		t.Errorf("%d pageshow mentions in the code, want 1", n)
	}
	for _, never := range []string{"location.search", "location.href", "URLSearchParams"} {
		if strings.Contains(code, never) {
			t.Errorf("the script reads %q: the channel is the server's attribute, never parsed from the address", never)
		}
	}
}

// TestTapPage_OnlyAQRPageSaysItsChannel: the form of a page opened from the plaque's QR code
// carries data-tap-channel="qr" -- what tap.js reloads a restored page by -- and an NFC page's
// form carries no channel attribute at all, in every shape of the button.
func TestTapPage_OnlyAQRPageSaysItsChannel(t *testing.T) {
	const attr = `data-tap-channel="qr"`
	for _, c := range []struct {
		name    string
		target  string
		preview sun.Preview
		facts   tenant.TapPageFacts
		qr      bool
	}{
		{"QR, predicted", "/t?tag=" + tapUID, previewWithStatus(false, "active"), predicted(tap.TypeIn), true},
		{"QR, undirected", "/t?tag=" + tapUID, previewWithStatus(false, "retired"), okFacts(), true},
		{"NFC, predicted", tapURL(), okPreview(true), predicted(tap.TypeIn), false},
		{"NFC, undirected", tapURL(), okPreview(false), okFacts(), false},
	} {
		t.Run(c.name, func(t *testing.T) {
			h, _ := newTapHandler(t, &fakePreviewer{preview: c.preview}, &fakeDirectory{facts: c.facts}, fixedSessions())
			form := tapFormOf(t, get(t, h, c.target, sessionCookie()).Body.String())
			open := form[:strings.Index(form, ">")]
			if got := strings.Contains(open, attr); got != c.qr {
				t.Errorf("the form says %s: %v, want %v\n%s", attr, got, c.qr, open)
			}
			if !c.qr && strings.Contains(form, "data-tap-channel") {
				t.Errorf("an NFC page carries a channel attribute: %s", open)
			}
		})
	}
}
