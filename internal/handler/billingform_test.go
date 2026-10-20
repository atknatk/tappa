package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/domain/billing"
	"github.com/atknatk/tappa/web/templates/pages"
)

// The freeze control's OWN forms, submitted exactly as rendered.
//
// 🔴 WHY THIS FILE EXISTS: the "Freeze <month>" form on the billing section posted no
// `month` field, while readBillingMonth refuses a POST without one, so pressing the
// button always redirected with ?problem=unreadable. Every other test in this package
// builds the POST body by hand (billingForm), which supplies the month the page
// forgot — so all of them were green while the button was dead. These tests never
// write a body: they lift the form out of the rendered HTML (action and every named
// field with its value) and post precisely that.

var (
	pageFormRE  = regexp.MustCompile(`(?is)<form\b([^>]*)>(.*?)</form>`)
	pageFieldRE = regexp.MustCompile(`(?is)<(input|select|textarea|button)\b([^>]*)>`)
	disabledRE  = regexp.MustCompile(`(?i)\sdisabled[\s>/=]`)
)

// postFormAt returns the body a browser would send for the page's one POST form whose
// action is `action`. It fails unless exactly one such form exists, so a page that grew
// a second copy (or lost its only one) cannot pass by picking whichever came first.
func postFormAt(t *testing.T, page, action string) url.Values {
	t.Helper()
	var found []url.Values
	for _, f := range pageFormRE.FindAllStringSubmatch(page, -1) {
		method, _ := attr(" "+f[1], "method")
		got, _ := attr(" "+f[1], "action")
		if !strings.EqualFold(method, "post") || htmlUnescaper.Replace(got) != action {
			continue
		}
		body := url.Values{}
		for _, in := range pageFieldRE.FindAllStringSubmatch(f[2], -1) {
			name, named := attr(" "+in[2], "name")
			if !named || disabledRE.MatchString(in[2]+">") {
				continue
			}
			// A submit button sends its name only when it is the one pressed; none of
			// these forms names one, and an unnamed button is skipped above.
			value, _ := attr(" "+in[2], "value")
			body.Add(htmlUnescaper.Replace(name), htmlUnescaper.Replace(value))
		}
		found = append(found, body)
	}
	if len(found) != 1 {
		t.Fatalf("the page has %d POST form(s) to %s, want exactly 1", len(found), action)
	}
	return found[0]
}

// TestBillingClose_ThePagesOwnFormsFreezeTheMonthTheyShow drives the button the way a
// browser does: render the section, post ITS form, post the warning's form, and land on
// the frozen month.
//
// A MONTH OTHER THAN THE FIXTURE'S DEFAULT IS NAMED, so the hidden value is proved to
// follow the card the owner is looking at rather than to happen to equal July.
func TestBillingClose_ThePagesOwnFormsFreezeTheMonthTheyShow(t *testing.T) {
	month := billing.Month{Year: 2026, Month: time.May}
	books := newFakeBooks()
	b := billingBrowser(t, books, billingTestOwnerRole)

	href := billingHref + "?month=" + month.String()
	rec := b.do(http.MethodGet, href, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d, want 200", href, rec.Code)
	}
	ask := postFormAt(t, htmlOf(t, rec), billingCloseHref)
	if got := ask.Get("month"); got != month.String() {
		t.Fatalf("the freeze form sends month=%q, want %q (the month on the card)", got, month.String())
	}

	// STEP 1: the section's own form must reach the warning, not a refusal.
	rec = b.do(http.MethodPost, billingCloseHref, ask)
	if loc := rec.Header().Get("Location"); strings.Contains(loc, "problem=") {
		t.Fatalf("POST %s with the page's own form was refused: %d -> %q", billingCloseHref, rec.Code, loc)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s with the page's own form: %d, want 200 (the warning screen)", billingCloseHref, rec.Code)
	}
	if n := books.closeCount(); n != 0 {
		t.Fatalf("the warning step froze %d period(s); it must write nothing", n)
	}

	// STEP 2: the warning's own form freezes that month, once.
	confirm := postFormAt(t, htmlOf(t, rec), billingFreezeHref)
	if got := confirm.Get("month"); got != month.String() {
		t.Fatalf("the warning's form sends month=%q, want %q", got, month.String())
	}
	if confirm.Get(confirmField) == "" {
		t.Fatal("the warning's form carries no confirmation token")
	}
	rec = b.do(http.MethodPost, billingFreezeHref, confirm)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST %s with the warning's own form: %d, want 303", billingFreezeHref, rec.Code)
	}
	if got, want := rec.Header().Get("Location"), billingMonthHref(month); got != want {
		t.Fatalf("after freezing it redirects to %q, want %q", got, want)
	}
	if n := books.closeCount(); n != 1 {
		t.Fatalf("the write step froze %d period(s), want exactly 1", n)
	}
	if got := books.closedMonths[0]; got != month.String() {
		t.Errorf("it froze %q, want %q", got, month.String())
	}
}

// TestBillingDraft_TheFreezeFormCarriesTheMonth is the template alone, with no router
// and no store: a closable draft renders a hidden month field in the exact form
// billing.ParseMonth reads, inside the form that posts to the close step.
func TestBillingDraft_TheFreezeFormCarriesTheMonth(t *testing.T) {
	d := newFakeBooks().draft
	d.Month = billing.Month{Year: 2025, Month: time.December}
	var v pages.BillingView
	fillBillingView(&v, d, nil, false, time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC))
	if !v.CanClose {
		t.Fatal("the fixture draft is not closable, so this test would prove nothing")
	}
	var buf bytes.Buffer
	if err := pages.AdminBilling(v).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	body := postFormAt(t, buf.String(), billingCloseHref)
	got := body.Get("month")
	if got != "2025-12" {
		t.Fatalf("the freeze form sends month=%q, want %q", got, "2025-12")
	}
	if m, err := billing.ParseMonth(got); err != nil || m != d.Month {
		t.Fatalf("billing.ParseMonth(%q) = %v, %v; want %v", got, m, err, d.Month)
	}
}

// TestBillingClose_TheWarningsFormCarriesTheMonthAndTheTokenAndNothingElse is M12-40's
// form criterion, read from the rendered page: the write takes no figure, so the form
// has no field one could be typed into, and the page's one h1 is the act and its month
// with a trail back to that month on the section.
func TestBillingClose_TheWarningsFormCarriesTheMonthAndTheTokenAndNothingElse(t *testing.T) {
	b := billingBrowser(t, newFakeBooks(), billingTestOwnerRole)
	rec := b.do(http.MethodPost, billingCloseHref, billingForm(billingTestMonth.String(), ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s: %d, want the warning screen", billingCloseHref, rec.Code)
	}
	html := htmlOf(t, rec)
	body := postFormAt(t, html, billingFreezeHref)
	if len(body) != 2 || body.Get("month") != billingTestMonth.String() || body.Get(confirmField) == "" {
		t.Errorf("the freeze form sends %v, want exactly month=%s and the confirmation", body, billingTestMonth)
	}
	if n := strings.Count(html, "<h1"); n != 1 {
		t.Errorf("the warning page has %d h1, want 1", n)
	}
	if !strings.Contains(html, ">Freeze July 2026</h1>") {
		t.Error("the page's h1 is not the act and its month")
	}
	if !strings.Contains(html, `href="`+billingMonthHref(billingTestMonth)+`" class="panel-crumb"`) {
		t.Error("the trail does not lead back to this month on the section")
	}
}
