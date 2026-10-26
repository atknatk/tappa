package handler

import (
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
)

// adminswitchpage_test.go -- M14-13, the switch page itself (ADR 0031 §5.4, K-5, K-10) against
// fakes: the Choose board's dockets inside a session, the one-business state, the refusal's copy
// after a double press, and the sign-in picker's paragraph under both states of the flag. The
// route's security (synchronizer, budgets, uniform refusal, flag gate) is adminswitch_test.go's.

// switchPaused is a suspended business among the fakes' memberships.
var switchPaused = uuid.MustParse("9f1c0a2e-0000-4000-8000-0000000000c3")

// switchDocket lifts each docket of a rendered page.
var switchDocket = regexp.MustCompile(`(?s)<article class="docket[^"]*">(.*?)</article>`)

// switchPageWith renders GET /admin/switch for a signed-in owner whose memberships are ms, with the
// tenant binding left in the page.
func switchPageWith(t *testing.T, ms []adminauth.Membership) (*browser, string) {
	t.Helper()
	b := switchBrowser(t, true, &fakeAdmins{memberships: func() ([]adminauth.Membership, error) { return ms, nil }})
	b.raw = true
	rec := b.do(http.MethodGet, adminSwitchHref, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s: %d", adminSwitchHref, rec.Code)
	}
	return b, rec.Body.String()
}

// TestAdminSwitch_ThePageIsTheChooseBoardsDockets (M14-13): one docket per OTHER business, each
// with the business's name, the person's role in it, "Continue to {name}", and its own form that
// posts to the switch with this session's tenant binding, the synchronizer and that business's id
// -- and the suspended one, and only that one, carries the K-10 mark; the current business is
// named above the heading with its own mark, and has no docket. The page offers exactly the
// person's own businesses: the tenant ids in it are the other memberships', and nothing else.
//
// MUTATION (measured): the handler not copying Membership.Suspended into the row -> red (no mark);
// businessDocketBody ignoring its paused argument -> red; the current business drawn as a docket
// -> red (three dockets, an id that is not a target).
func TestAdminSwitch_ThePageIsTheChooseBoardsDockets(t *testing.T) {
	ms := []adminauth.Membership{
		{TenantID: panelTestTenant, TenantName: "Current Ltd", Role: "owner", Current: true, Suspended: true},
		{TenantID: switchOther, TenantName: "Other Ltd", Role: "manager"},
		{TenantID: switchPaused, TenantName: "Paused Ltd", Role: "owner", Suspended: true},
	}
	b, page := switchPageWith(t, ms)
	if n := strings.Count(page, "<h1"); n != 1 {
		t.Errorf("%d <h1> on the page, want 1", n)
	}
	head, _, _ := strings.Cut(page, "<h1")
	if !strings.Contains(head, "You are in Current Ltd") || !strings.Contains(head, `<span class="tally tally--changes-paused">Changes paused</span>`) {
		t.Errorf("the heading does not name the current business with its suspension mark: %s", head)
	}

	dockets := switchDocket.FindAllStringSubmatch(page, -1)
	if len(dockets) != 2 {
		t.Fatalf("%d dockets, want one per other business (2)", len(dockets))
	}
	csrf := csrfFrom(t, page)
	action := `action="` + adminSwitchHref + `?tb=` + bindingFor(b.h, b.cookies[adminauth.CookieName]) + `"`
	for i, want := range ms[1:] {
		d := dockets[i][1]
		for _, s := range []string{
			`<form method="post" ` + action,
			`name="csrf" value="` + csrf + `"`,
			`name="tenant_id" value="` + want.TenantID.String() + `"`,
			">" + want.TenantName + "<",
			">" + want.Role + "<",
			"Continue to " + want.TenantName,
		} {
			if !strings.Contains(d, s) {
				t.Errorf("the docket of %s lacks %q:\n%s", want.TenantName, s, d)
			}
		}
		if n := strings.Count(d, "<form"); n != 1 {
			t.Errorf("the docket of %s holds %d forms, want its own one", want.TenantName, n)
		}
		if marked := strings.Contains(d, "tally--changes-paused"); marked != want.Suspended {
			t.Errorf("the docket of %s: suspension mark %v, want %v", want.TenantName, marked, want.Suspended)
		}
	}

	var ids []string
	for _, m := range regexp.MustCompile(`name="tenant_id" value="([^"]*)"`).FindAllStringSubmatch(page, -1) {
		ids = append(ids, m[1])
	}
	if want := []string{switchOther.String(), switchPaused.String()}; !slices.Equal(ids, want) {
		t.Errorf("the page offers the targets %v, want exactly the other memberships %v", ids, want)
	}
	if strings.Contains(page, panelTestTenant.String()) {
		t.Error("the page prints the current business's id")
	}
}

// TestAdminSwitch_OneBusinessIsSaidNotDrawn (M14-13, for M14-12's always-visible link): a person
// whose sign-in has one business is told so -- by name -- and how others would appear, with no
// docket, no switch form and the way back; a list that names no business at all says the same
// without a name. CONTROL: the two-business page draws a form.
//
// MUTATION (measured): the one-business branch removed (the page as it was: a heading and nothing)
// -> red.
func TestAdminSwitch_OneBusinessIsSaidNotDrawn(t *testing.T) {
	for name, tc := range map[string]struct {
		ms   []adminauth.Membership
		says string
	}{
		"one business": {
			ms:   []adminauth.Membership{{TenantID: panelTestTenant, TenantName: "Current Ltd", Role: "owner", Current: true}},
			says: "Current Ltd is the only business on this sign-in",
		},
		"an empty list": {says: "There is no other business on this sign-in"},
	} {
		_, page := switchPageWith(t, tc.ms)
		for _, s := range []string{tc.says, "you can move between them without signing out", `href="` + transactionsHref + `"`} {
			if !strings.Contains(page, s) {
				t.Errorf("%s: the page does not say %q", name, s)
			}
		}
		if strings.Contains(page, "<form") || strings.Contains(page, "<article") || strings.Contains(page, "Continue to") {
			t.Errorf("%s: the page draws a docket or a form with nothing to switch to", name)
		}
	}
	_, page := switchPageWith(t, []adminauth.Membership{
		{TenantID: panelTestTenant, TenantName: "Current Ltd", Role: "owner", Current: true},
		{TenantID: switchOther, TenantName: "Other Ltd", Role: "manager"},
	})
	if !strings.Contains(page, "<form") || strings.Contains(page, "the only business") {
		t.Error("CONTROL: the two-business page draws no form, or says there is one business")
	}
}

// TestAdminSwitch_TheRefusalIsTrueAfterADoublePress (M14-13): the one 403 is what the losing half
// of a double press shows -- the first press won and moved the cookie -- so its copy must hold
// then too. It says this request changed nothing and that a first press may already have
// switched, offers the way back to the panel, and never claims the person is still where they
// were; it names neither business. Its uniformity across reasons is
// TestAdminSwitch_ARefusalIsOneAnswerAndTouchesNoCookie's.
//
// MUTATION (measured): problemSwitchRefused restored to M14-10's copy ("You are still in the
// business you were in") -> red.
func TestAdminSwitch_TheRefusalIsTrueAfterADoublePress(t *testing.T) {
	admins := &fakeAdmins{switchTo: func(uuid.UUID) (adminauth.Issued, error) { return adminauth.Issued{}, adminauth.ErrSwitchRefused }}
	b := switchBrowser(t, true, admins)
	rec := b.do(http.MethodPost, adminSwitchHref, url.Values{"csrf": {switchCSRF(t, b)}, "tenant_id": {switchOther.String()}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a refused switch: %d, want 403", rec.Code)
	}
	page := rec.Body.String()
	for _, s := range []string{"This request changed nothing", "pressed the button twice", `href="` + transactionsHref + `"`} {
		if !strings.Contains(page, s) {
			t.Errorf("the refusal does not say %q", s)
		}
	}
	for _, s := range []string{"still in the business", "Current Ltd", "Other Ltd", switchOther.String()} {
		if strings.Contains(page, s) {
			t.Errorf("the refusal says %q", s)
		}
	}
}

// TestAdminChoose_TheParagraphNamesTheSwitchOnlyWhereItExists (M14-13): the sign-in's picker
// explains how to move between businesses -- with the flag on (the transition arm: rows not yet
// linked to one sign-in), how to do it without signing out; with the flag off, today's "sign out
// and switch", byte for byte, because there is no switch to name.
//
// MUTATION (measured): the handler's PanelSwitch made true (or false) whatever the flag -> one of
// the two rows is red.
func TestAdminChoose_TheParagraphNamesTheSwitchOnlyWhereItExists(t *testing.T) {
	a := adminauth.Attempt{AdminUserID: uuid.New(), TenantID: uuid.New(), PasswordMatched: true, Active: true}
	c := adminauth.Attempt{AdminUserID: uuid.New(), TenantID: uuid.New(), PasswordMatched: true, Active: true}
	const today = "You manage more than one. Pick the one you want to work in — you can sign out and switch at any time."
	const linkThem = "To move between them without signing out, link them to one sign-in on your account page."

	off := newBrowser(t, newAdminRouter(t, &fakeAdmins{authenticate: func(string, string) (adminauth.Authentication, error) {
		return legacyOf(a, c), nil
	}}, &fakeTrail{}))
	if rec := signInWith(t, off, "right"); rec.Header().Get("Location") != "/admin/login/choose" {
		t.Fatalf("flag off: %d to %q, want the picker", rec.Code, rec.Header().Get("Location"))
	}
	page := htmlOf(t, off.do(http.MethodGet, "/admin/login/choose", nil))
	if !strings.Contains(page, today) || strings.Contains(page, linkThem) {
		t.Errorf("flag off: the picker does not say today's sentence alone:\n%s", page)
	}

	_, h := newLoginAuth(t, &fakeAdmins{person: func(string, string, uuid.UUID, adminauth.FailureBudget) (adminauth.PersonSignIn, error) {
		return adminauth.PersonSignIn{Legacy: legacyOf(a, c)}, nil
	}}, &fakeTrail{}, true)
	on := newBrowser(t, h)
	if rec := signInWith(t, on, "right"); rec.Header().Get("Location") != "/admin/login/choose" {
		t.Fatalf("flag on: %d to %q, want the picker", rec.Code, rec.Header().Get("Location"))
	}
	page = htmlOf(t, on.do(http.MethodGet, "/admin/login/choose", nil))
	if !strings.Contains(page, linkThem) || strings.Contains(page, today) {
		t.Errorf("flag on: the picker does not explain switching without signing out:\n%s", page)
	}
}
