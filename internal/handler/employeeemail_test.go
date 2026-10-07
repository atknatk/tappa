package handler

// employeeemail_test.go -- M10 EM-6's unit-level nets: what the address route builds
// from a request, who may reach the domain, what a refusal says, and what never
// reaches a log, a redirect or the trail. The rule, the transaction and the tenant
// boundary are measured against real Postgres in
// internal/domain/tenant/staffemail_db_test.go and employeeemail_db_test.go.

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// emailTestAddress is distinctive enough that finding it — or its local part — in a
// log, a URL or a trail row can only mean it leaked.
//
// 🔴 IT CARRIES CAPITALS ON PURPOSE (EM-6 round 2). An all-lower-case fixture cannot
// tell "forwarded verbatim" from "lower-cased on the way": a handler that folded the
// posted address passed every assertion here while the test claimed to measure the
// RAW value (the audit's A23). The leak checks below compare emailTestLocal against
// the LOWER-CASED haystack, so a leak in any case is still found.
const (
	emailTestAddress = "Maria.ZZ7Q.Borg@Kebab.example.test"
	emailTestLocal   = "maria.zz7q.borg"
)

// emailBrowser is the panel with a session of the given ROLE, its process log captured
// and the trail and the staff under the caller's control.
func emailBrowser(t *testing.T, role string, staff *fakeStaff, trail *fakeTrail) (*browser, *fakeAdmins, *strings.Builder) {
	t.Helper()
	logs := &strings.Builder{}
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: role, FullName: "KF Admin",
		}, nil
	}}
	h, err := NewAdminAuth(admins, trail, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		staff, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(),
		nil, adminTestConfig(), slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b, admins, logs
}

// TestEmployeeEmail_OnlyAnOwnerReachesTheDomain is the role gate, both sides.
//
// OWNER: exactly one EmailCommand, carrying the SESSION's tenant and actor (a posted
// tenant_id/actor_id is ignored), the posted employee id and the RAW posted address
// (trimming and the rule are the domain's), and no refusal row.
//
// MANAGER, NO ROLE, UNDEFINED ROLE (the last two since round 5; every arm posts
// role=owner): zero calls on the staff surface, a 303 to the card with
// problem=not-permitted, and exactly one employee.email_change_refused row — session
// tenant, session actor, the posted employee as target, and a detail whose keys are
// exactly outcome/reason/role/required_role with role = the SESSION's role,
// required_role=owner and no trace of the address.
func TestEmployeeEmail_OnlyAnOwnerReachesTheDomain(t *testing.T) {
	employeeID := uuid.New()
	// 🔴 THE FORM CLAIMS AUTHORITY IT DOES NOT HAVE, on every arm: a foreign tenant, a
	// foreign actor and (EM-6 round 5) role=owner. The tenant claim was already pinned
	// (M25); the role claim was not — a gate that also accepted the body's role=owner
	// stayed green on every EM-6 test (the security audit's S10). The refused arms
	// below carry it, so a gate that listens to it lets a refused session through.
	form := url.Values{
		"id":        {employeeID.String()},
		"email":     {"  " + emailTestAddress + " "},
		"tenant_id": {uuid.NewString()},
		"actor_id":  {uuid.NewString()},
		"role":      {"owner"},
	}

	t.Run("owner", func(t *testing.T) {
		staff, trail := &fakeStaff{}, &fakeTrail{}
		b, _, _ := emailBrowser(t, "owner", staff, trail)
		rec := b.do(http.MethodPost, employeeEmailHref, form)
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("owner POST answered %d, want 303", rec.Code)
		}
		if loc := rec.Header().Get("Location"); !strings.Contains(loc, "done=email") ||
			!strings.Contains(loc, "manage="+employeeID.String()) {
			t.Errorf("owner POST redirected to %q, want the card with done=email", loc)
		}
		cmds := staff.emailCommands()
		if len(cmds) != 1 {
			t.Fatalf("the domain saw %d change(s), want 1", len(cmds))
		}
		c := cmds[0]
		if c.TenantID != panelTestTenant || c.ActorID != panelTestAdmin || c.EmployeeID != employeeID {
			t.Errorf("command = tenant %s actor %s employee %s; want the session's tenant %s, the "+
				"session's admin %s and the posted employee %s", c.TenantID, c.ActorID, c.EmployeeID,
				panelTestTenant, panelTestAdmin, employeeID)
		}
		if c.Email != "  "+emailTestAddress+" " {
			t.Errorf("command carried %q; the handler must forward the raw value and leave the "+
				"rule to the domain", c.Email)
		}
		if n := trail.count(ActionEmployeeEmailChangeRefused); n != 0 {
			t.Errorf("an owner's change wrote %d refusal row(s)", n)
		}
	})

	// THE REFUSED ARMS. A manager is the role the screen exists to stop; the other two
	// are live sessions whose role is not one the product defines — empty, and a name it
	// has never issued. EM-6 round 5: with only the manager arm, a gate rewritten from
	// the allow-list (role == owner) to a deny-list (role != manager) stayed green on
	// every EM-6 test (the security audit's S2). The gate must let ONE role through,
	// not keep one out, and only these arms can tell the two apart.
	for _, refused := range []struct{ name, role string }{
		{"manager", "manager"},
		{"no role", ""},
		{"undefined role", "auditor"},
	} {
		t.Run(refused.name, func(t *testing.T) {
			staff, trail := &fakeStaff{}, &fakeTrail{}
			b, _, logs := emailBrowser(t, refused.role, staff, trail)
			rec := b.do(http.MethodPost, employeeEmailHref, form)
			if rec.Code != http.StatusSeeOther {
				t.Fatalf("a %q POST answered %d, want 303", refused.role, rec.Code)
			}
			loc := rec.Header().Get("Location")
			if !strings.Contains(loc, "problem=not-permitted") {
				t.Errorf("a %q POST redirected to %q, want problem=not-permitted", refused.role, loc)
			}
			// THE REFUSAL TOUCHES THE STAFF SURFACE NOT AT ALL — no Person read, no address
			// read, no change — measured BEFORE the redirect is followed, because rendering
			// the card afterwards reads both legitimately. (EM-6 round 2: the audit added an
			// address read to this branch and the suite stayed green — the fake did not
			// count reads; A38.)
			if persons, reads, changes := staff.staffCalls(); persons+reads+changes != 0 {
				t.Fatalf("the refused POST made %d Person read(s), %d address read(s) and %d change(s); "+
					"a refusal must cost the trail row and nothing on the staff surface", persons, reads, changes)
			}
			// THE WORD IS RENDERED AS A SENTENCE (§4.6), not merely carried.
			if page := htmlOf(t, b.do(http.MethodGet, loc, nil)); !strings.Contains(page, "Only the business owner can change an employee") {
				t.Errorf("the page at %s does not say who may change an address", loc)
			}
			if n := len(staff.emailCommands()); n != 0 {
				t.Fatalf("a %q session's change reached the domain %d time(s), want 0", refused.role, n)
			}
			events := trail.eventsSnapshot()
			if len(events) != 1 || events[0].Action != ActionEmployeeEmailChangeRefused {
				t.Fatalf("trail = %d event(s) (%v); want exactly one %s", len(events), actionsOf(events),
					ActionEmployeeEmailChangeRefused)
			}
			e := events[0]
			if e.TenantID != panelTestTenant || e.ActorID == nil || *e.ActorID != panelTestAdmin ||
				e.Target != employeeID.String() {
				t.Errorf("refusal row = tenant %s actor %v target %q; want the session's tenant, the "+
					"session's admin and the posted employee", e.TenantID, e.ActorID, e.Target)
			}
			detail := detailJSON(t, e.Detail)
			var keys map[string]any
			if err := json.Unmarshal([]byte(detail), &keys); err != nil {
				t.Fatalf("detail %s: %v", detail, err)
			}
			got := make([]string, 0, len(keys))
			for k := range keys {
				got = append(got, k)
			}
			sort.Strings(got)
			if strings.Join(got, ",") != "outcome,reason,required_role,role" {
				t.Errorf("refusal detail keys = %v, want exactly outcome, reason, required_role, role", got)
			}
			// THE ROW NAMES THE SESSION'S ROLE, NOT THE POSTED ONE: the form says
			// role=owner on every arm, and a row claiming an owner was refused would be a
			// false statement in an append-only table.
			if keys["role"] != refused.role || keys["required_role"] != "owner" {
				t.Errorf("refusal detail = %s, want role=%q required_role=owner", detail, refused.role)
			}
			for _, where := range []struct{ name, text string }{
				{"the refusal row", detail}, {"the process log", logs.String()}, {"the redirect", loc},
			} {
				if strings.Contains(strings.ToLower(where.text), emailTestLocal) {
					t.Errorf("the posted address reached %s", where.name)
				}
			}
		})
	}
}

// changeEmailInputRE is the owner's change-email input element, whole.
var changeEmailInputRE = regexp.MustCompile(`(?is)<input\b[^>]*\bid="change-email"[^>]*>`)

// actionsOf lists the actions of some events, for a failure message.
func actionsOf(events []audit.Event) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Action)
	}
	return out
}

// TestEmployeeEmail_TheCardOffersTheFormOnlyToAnOwner: the address is shown to every
// admin; the FORM only to an owner — the predicate the POST uses — and a manager reads
// the sentence that says who can. The owner's card is the positive control, so a
// template that rendered the form for nobody fails here.
func TestEmployeeEmail_TheCardOffersTheFormOnlyToAnOwner(t *testing.T) {
	for _, tc := range []struct {
		role     string
		wantForm bool
	}{{"owner", true}, {"manager", false}} {
		t.Run(tc.role, func(t *testing.T) {
			b, _, _ := emailBrowser(t, tc.role, &fakeStaff{email: emailTestAddress}, &fakeTrail{})
			body := htmlOf(t, b.do(http.MethodGet, managedRosterHref(uuid.New()), nil))
			if !strings.Contains(body, "Managing") {
				t.Fatal("no action card rendered; every assertion below would be about an empty page")
			}
			if !strings.Contains(body, emailTestAddress) {
				t.Errorf("the %s's card does not show the address on file", tc.role)
			}
			offers := strings.Contains(body, `action="`+employeeEmailHref+`"`)
			if offers != tc.wantForm {
				t.Errorf("the %s's card offers the address form = %v, want %v", tc.role, offers, tc.wantForm)
			}
			says := strings.Contains(body, "Only the business owner can change an address.")
			if says == tc.wantForm {
				t.Errorf("the %s's card says who may change the address = %v, want %v", tc.role, says, !tc.wantForm)
			}
			// THE FORM SAYS WHAT SAVING DOES TO OUTSTANDING LINKS, before the save. The
			// behaviour itself is TestEmployeeEmailDB_AChangedAddressKillsTheLinkSentBefore.
			warns := strings.Contains(body, "that has not been used yet")
			if warns != tc.wantForm {
				t.Errorf("the %s's card warns about outstanding links = %v, want %v", tc.role, warns, tc.wantForm)
			}
			// THE FORM OPENS ON THE ADDRESS ON FILE, BYTE FOR BYTE (EM-6 round 2: deleting
			// the input's value left this test green, because the address also appears as
			// text — A33). The input is found by its id and its value read on its own.
			if tc.wantForm {
				input := changeEmailInputRE.FindString(body)
				if input == "" {
					t.Fatal("the owner's card has no change-email input")
				}
				m := attrValueRE("value").FindStringSubmatch(input)
				if len(m) != 2 || m[1] != emailTestAddress {
					t.Errorf("the change-email input is %s; want its value to be the address on file %q verbatim",
						input, emailTestAddress)
				}
			}
		})
	}
	// NO ADDRESS ON FILE is a sentence, not a blank.
	b, _, _ := emailBrowser(t, "owner", &fakeStaff{}, &fakeTrail{})
	if body := htmlOf(t, b.do(http.MethodGet, managedRosterHref(uuid.New()), nil)); !strings.Contains(body, "No address on file.") {
		t.Error("a person with no address renders no sentence saying so")
	}
}

// TestEmployeeEmail_EveryOutcomeIsASentenceAndNoneCarriesTheAddress is §4.6 and §4.7
// at this boundary, one table over EVERY BRANCH employeeEmail has (EM-6 round 2: the
// first table had six rows, never read the 500's body, and had no row for the role
// refusal or for an unreadable request):
//
//	readAction        a body ParseForm refuses, and an id that is not a uuid
//	the role gate     a manager
//	ChangeEmail       success, the four refusals, and a failure the caller did not
//	                  cause (500, whose BODY is read too: since round 3 it is the
//	                  WRITER's page, problemPanelWriteFailed, whose claims
//	                  employeeemail_db_test.go measures against Postgres; since
//	                  round 4 a wrapped 40P01 *pgconn.PgError is a row of its own)
//
// Each answer is rendered as a sentence the test reads on the page it lands on, and on
// every row the posted address (matched case-insensitively) appears in neither the
// redirect, the response body, nor the process log.
func TestEmployeeEmail_EveryOutcomeIsASentenceAndNoneCarriesTheAddress(t *testing.T) {
	posted := "email=" + url.QueryEscape(emailTestAddress)
	for _, tc := range []struct {
		name   string
		role   string
		body   string
		err    error
		code   int
		want   string
		expect string
	}{
		// The apostrophe is HTML-escaped on the page, so the sentence is matched after it.
		{"changed", "owner", "", nil, http.StatusSeeOther, "done=email", "address on file is " + emailTestAddress + "."},
		{"bad address", "owner", "", tenant.ErrEmployeeEmail, http.StatusSeeOther, "problem=bad-email", "Use a plain address such as name@example.com"},
		{"taken", "owner", "", tenant.ErrEmailTaken, http.StatusSeeOther, "problem=email-taken", "Somebody in this business already has that address"},
		{"same", "owner", "", tenant.ErrSameEmail, http.StatusSeeOther, "problem=same-email", "That is already their address"},
		{"unknown", "owner", "", tenant.ErrUnknownEmployee, http.StatusSeeOther, "problem=unknown", "not on this business"},
		{"not permitted", "manager", "", nil, http.StatusSeeOther, "problem=not-permitted", "Only the business owner can change an employee"},
		{"unreadable form", "owner", posted + "&id=%zz", nil, http.StatusSeeOther, "problem=unreadable", "We could not read that submission"},
		{"unreadable id", "owner", posted + "&id=not-a-uuid", nil, http.StatusSeeOther, "problem=unreadable", "We could not read that submission"},
		{"database", "owner", "", errors.New("connection refused"), http.StatusInternalServerError, "", "We could not save that"},
		// 🔴 A DEADLOCK IS A DATABASE ERROR THE PAGE MUST STILL CALL A FAILED WRITE (EM-6
		// round 4). ADR 0022's EM-6 limit 2 sends the 40P01 a change can lose to a
		// concurrent activation to this branch. The plain error above cannot see a
		// handler that routes the *pgconn.PgError subset elsewhere — an audit routed it
		// back to the reader's page through a helper method, where the census (which
		// reads only the handler's own body) and the row above both stayed green. The
		// error is wrapped the way tenant.wrap wraps it.
		{"deadlock", "owner", "", fmt.Errorf("tenant: change email: %w", &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}), http.StatusInternalServerError, "", "We could not save that"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			staff := &fakeStaff{changeErr: tc.err, email: emailTestAddress}
			b, _, logs := emailBrowser(t, tc.role, staff, &fakeTrail{})
			body := tc.body
			if body == "" {
				body = posted + "&id=" + uuid.NewString()
			}
			rec := b.doRaw(http.MethodPost, employeeEmailHref, body)
			if rec.Code != tc.code {
				t.Fatalf("answered %d, want %d", rec.Code, tc.code)
			}
			loc := rec.Header().Get("Location")
			if strings.Contains(strings.ToLower(loc), emailTestLocal) || strings.Contains(loc, "%40") {
				t.Errorf("the redirect %q carries the address", loc)
			}
			if strings.Contains(strings.ToLower(logs.String()), emailTestLocal) {
				t.Error("the posted address reached the process log (R7b)")
			}
			page := htmlOf(t, rec)
			if tc.code == http.StatusSeeOther {
				if !strings.Contains(loc, tc.want) {
					t.Fatalf("redirected to %q, want %q", loc, tc.want)
				}
				page = htmlOf(t, b.do(http.MethodGet, loc, nil))
			}
			if !strings.Contains(page, tc.expect) {
				t.Errorf("the page for %q does not say %q", tc.name, tc.expect)
			}
			// THE ADDRESS ON FILE IS LEGITIMATELY ON THE CARD (done=email, and the card a
			// refusal returns to); what must not appear is the POSTED value on a page
			// that is not the card — the problem page of a 500, or the roster a refusal
			// without a person returns to.
			if !strings.Contains(page, "Managing") && strings.Contains(strings.ToLower(page), emailTestLocal) {
				t.Errorf("the response for %q carries the posted address outside the card", tc.name)
			}
		})
	}
}

// TestEmployeeEmail_TheDoneNoticeStatesWhatIsOnFile: done=email is in the "moved"
// class — the notice prints the address the SAME request read, so a hand-made URL
// over a person with no address says so rather than claiming a change.
func TestEmployeeEmail_TheDoneNoticeStatesWhatIsOnFile(t *testing.T) {
	b, _, _ := emailBrowser(t, "owner", &fakeStaff{}, &fakeTrail{})
	body := htmlOf(t, b.do(http.MethodGet, managedRosterHref(uuid.New())+"&done=email", nil))
	if !strings.Contains(body, "Address on file") || !strings.Contains(body, "Maria Borg has no address on file.") {
		t.Error("done=email over a person with no address does not state that there is none")
	}
}

// TestEmployeeEmail_AStoredAddressIsEscapedWhereItIsRendered: the change form refuses
// markup, but the ADD form's weaker storage rule (M6-13) does not, so the card can be
// handed a stored address carrying it. It is rendered twice — as text and in the
// form's value attribute — and escaped in both; the escaped text is the control.
func TestEmployeeEmail_AStoredAddressIsEscapedWhereItIsRendered(t *testing.T) {
	hostile := `x"><script>alert(1)</script>@example.test`
	b, _, _ := emailBrowser(t, "owner", &fakeStaff{email: hostile}, &fakeTrail{})
	body := htmlOf(t, b.do(http.MethodGet, managedRosterHref(uuid.New()), nil))
	if strings.Contains(body, "<script>alert(1)") || strings.Contains(body, `x">`) {
		t.Error("a stored address reached the page unescaped")
	}
	if !strings.Contains(body, "&lt;script&gt;alert(1)&lt;/script&gt;@example.test") {
		t.Error("control: the escaped address is not on the page, so the absence above proves nothing")
	}
}

// TestEmployeeEmail_AFailedAddressReadShowsNoCard: if the address cannot be read the
// card is withheld with the "could not load" sentence — never a card reading "No
// address on file", which would invite an owner to type over an address that exists.
func TestEmployeeEmail_AFailedAddressReadShowsNoCard(t *testing.T) {
	b, _, _ := emailBrowser(t, "owner", &fakeStaff{emailErr: errors.New("connection refused")}, &fakeTrail{})
	body := htmlOf(t, b.do(http.MethodGet, managedRosterHref(uuid.New()), nil))
	if strings.Contains(body, "Managing") || strings.Contains(body, "No address on file.") {
		t.Error("a failed address read still rendered the card")
	}
	if !strings.Contains(body, "We could not load this person") {
		t.Error("a failed address read is not explained")
	}
}

// TestEmployeeEmail_IsBehindTheWriteChain: the route takes ProtectWriting like every
// other panel write. A cross-origin POST costs ZERO resolver reads and reaches nothing;
// an anonymous one is sent to sign in; an oversized body is refused by the read. The
// same-origin owner POST first is the positive control for the counter.
func TestEmployeeEmail_IsBehindTheWriteChain(t *testing.T) {
	staff := &fakeStaff{}
	b, admins, _ := emailBrowser(t, "owner", staff, &fakeTrail{})
	form := url.Values{"id": {uuid.NewString()}, "email": {emailTestAddress}}
	if rec := b.do(http.MethodPost, employeeEmailHref, form); rec.Code != http.StatusSeeOther {
		t.Fatalf("same-origin POST answered %d, want 303", rec.Code)
	}
	baseline := admins.verifiedCount()
	if baseline == 0 || len(staff.emailCommands()) != 1 {
		t.Fatal("the control POST resolved no session or reached no domain; the counters are not wired")
	}

	b.origin = "https://evil.example"
	rec := b.do(http.MethodPost, employeeEmailHref, form)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin" {
		t.Errorf("cross-origin POST: %d %q, want 303 /admin", rec.Code, rec.Header().Get("Location"))
	}
	if got := admins.verifiedCount() - baseline; got != 0 {
		t.Errorf("a cross-origin POST cost %d resolver read(s), want 0", got)
	}
	b.origin = testBaseURL

	huge := "id=" + uuid.NewString() + "&pad=" + strings.Repeat("x", maxEmployeeActionBody+1)
	if rec := b.doRaw(http.MethodPost, employeeEmailHref, huge); !strings.Contains(rec.Header().Get("Location"), "problem=unreadable") {
		t.Errorf("an oversized POST redirected to %q, want problem=unreadable", rec.Header().Get("Location"))
	}
	if n := len(staff.emailCommands()); n != 1 {
		t.Errorf("the domain saw %d change(s) after the refused requests, want still 1", n)
	}

	anon := &fakeAdmins{verify: func() (adminauth.Resolved, error) { return adminauth.Resolved{}, adminauth.ErrNoSession }}
	anonStaff := &fakeStaff{}
	ab := newBrowser(t, newAdminRouterWithActions(t, anon, &fakeTrail{}, newFakeLedger(), &fakeReviewer{}, anonStaff, &fakeInviter{}))
	if rec := ab.do(http.MethodPost, employeeEmailHref, form); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/admin/login" {
		t.Errorf("anonymous POST: %d %q, want 303 /admin/login", rec.Code, rec.Header().Get("Location"))
	}
	if n := len(anonStaff.emailCommands()); n != 0 {
		t.Errorf("an anonymous POST reached the domain %d time(s)", n)
	}
}
