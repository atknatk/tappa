package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/web/templates/pages"
)

// suspension_test.go -- ADR 0025 §3, OP-15 phase B: the panel's write gate, measured
// against the REAL router AdminAuth.Mount builds (chi.Walk), not against a list.
//
// THE TRIPWIRE PANEL. A refused request must cost no store call (ADR 0025 PART I). The
// panel the refusals are driven through is therefore built with every store dependency
// an EMPTY INTERFACE EMBEDDED IN A STRUCT: the value is not nil (NewAdminAuth accepts
// it), and any method called on it dereferences a nil interface and panics. The driver
// recovers that panic and reports the route, so "0 store calls" is a property the run
// would fail on rather than a counter somebody remembered to read.

// suspendedSince is a suspension's start, chosen so the business's own timezone
// (Europe/Malta, the fake's) and UTC fall on DIFFERENT dates -- 8 Oct there, 7 Oct
// here -- which is what lets the strip test tell the two apart.
var suspendedSince = time.Date(2026, 10, 7, 23, 30, 0, 0, time.UTC)

// suspensionCookieValue is a panel cookie value adminauth.Cookies accepts; the fake
// authenticator resolves every cookie the same way, so the value is never compared.
const suspensionCookieValue = "FAKEfakeFAKEfakeFAKEfakeFAKEfakeFAKEfake123"

// suspendedIdentity is a live owner of tenant whose business is suspended with code;
// a nil code means "not suspended".
func suspendedIdentity(tenant, admin, session uuid.UUID, code *db.SuspensionReason) *fakeAdmins {
	return suspendedIdentityAs("owner", tenant, admin, session, code)
}

// suspendedIdentityAs is suspendedIdentity for an admin of the given role. The gate
// must not care which: a suspension belongs to the business, not to its owner (2nd
// round, F2 -- every identity used to be an owner, so a gate that read the role passed).
func suspendedIdentityAs(role string, tenant, admin, session uuid.UUID, code *db.SuspensionReason) *fakeAdmins {
	return &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		r := adminauth.Resolved{
			SessionID: session, TenantID: tenant, AdminUserID: admin,
			Role: role, FullName: "Admin",
		}
		if code != nil {
			r.Suspended = &adminauth.Suspension{Since: suspendedSince, Reason: string(*code)}
		}
		return r, nil
	}}
}

func codePtr(c db.SuspensionReason) *db.SuspensionReason { return &c }

// tripwirePanel is AdminAuth.Mount over the tripwire dependencies described at the top
// of this file: only the authenticator and the audit recorder answer.
func tripwirePanel(t *testing.T, admins *fakeAdmins, trail auditRecorder, log *slog.Logger) http.Handler {
	t.Helper()
	h, err := NewAdminAuth(admins, trail,
		struct{ panelLedger }{}, struct{ panelQueue }{}, struct{ panelReviewer }{},
		struct{ panelStaff }{}, struct{ panelInviter }{}, struct{ panelVenues }{},
		struct{ panelPlaques }{}, struct{ panelRecorder }{}, struct{ panelRules }{},
		struct{ panelScribe }{}, struct{ panelBooks }{}, struct{ panelAccounts }{},
		struct{ panelBrands }{}, struct{ panelBrandWriter }{}, struct{ PlaqueEncoder }{},
		struct{ passwordNotifier }{}, adminTestConfig(), log)
	if err != nil {
		t.Fatalf("NewAdminAuth (tripwire): %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

// suspensionDrive sends one request the way a signed-in browser on this origin would,
// and recovers a tripwire's panic into the second result.
func suspensionDrive(h http.Handler, method, route string) (rec *httptest.ResponseRecorder, tripped any) {
	req := httptest.NewRequest(method, route, strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testBaseURL)
	req.RemoteAddr = "203.0.113.9:1234"
	req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: suspensionCookieValue})
	rec = httptest.NewRecorder()
	func() {
		defer func() { tripped = recover() }()
		h.ServeHTTP(rec, req)
	}()
	return rec, tripped
}

// walkedRoutes is every (method, pattern) a router serves, as "METHOD /pattern".
func walkedRoutes(t *testing.T, h http.Handler) []string {
	t.Helper()
	routes, ok := h.(chi.Routes)
	if !ok {
		t.Fatalf("%T is not a chi router; there is nothing to walk", h)
	}
	var out []string
	if err := chi.Walk(routes, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		out = append(out, method+" "+route)
		return nil
	}); err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	sort.Strings(out)
	return out
}

func isRead(methodRoute string) bool {
	return strings.HasPrefix(methodRoute, "GET ") || strings.HasPrefix(methodRoute, "HEAD ")
}

// suspensionExceptions is the panel's CLOSED exception list, by name: ADR 0025 §3's
// eight -- the three sign-in routes that were never behind the writing chain and the
// five record paths of mountWriting's sibling group -- and K-26's three defences (ADR
// 0025's M11 note; plan §4.7: revoking a key, revoking every key, switching the webhook
// off). It is written out rather than derived on purpose -- deriving it from the sibling
// group (or from internal/webhook/endpoints' table) would make a further route an
// exception by construction, and the point of the list is that a further one is a
// decision.
func suspensionExceptions() map[string]string {
	return map[string]string{
		"POST " + apiKeyRevokeHref:    "revoking an API key (a defence, K-26)",
		"POST " + apiKeyRevokeAllHref: "revoking every API key (a defence, K-26)",
		"POST " + webhookDisableHref:  "switching the webhook off (a defence, K-26; endpoints.OpenWhileSuspended)",
		"POST " + adminLoginPath:      "signing in (outside the writing chain)",
		"POST /admin/login/choose":    "choosing the business (outside the writing chain)",
		"POST /admin/logout":          "signing out (outside the writing chain)",
		"POST " + accountPasswordHref: "the admin's own password",
		"POST " + employeeAddHref:     "adding a person (a record path, K-B1)",
		"POST " + employeeInviteHref:  "re-inviting (a record path, K-B1 = (i))",
		"POST " + manualEntryHref:     "the manual record's review step (a record path, K-B1 = (i))",
		"POST " + manualRecordHref:    "the manual record itself (a record path, K-B1 = (i))",
	}
}

// refusalEvents are the trail's tenant.write_refused rows.
func refusalEvents(tr *fakeTrail) []audit.Event {
	var out []audit.Event
	for _, e := range tr.eventsSnapshot() {
		if e.Action == ActionTenantWriteRefused {
			out = append(out, e)
		}
	}
	return out
}

// TestSuspensionRoutes_EveryPanelWriteIsGatedOrANamedException is ADR 0025
// §3's derivation: over the router AdminAuth.Mount builds, every route that is not a
// GET or a HEAD is either one of the named exceptions (suspensionExceptions) or GATED, and "gated" is
// measured three ways per route --
//
//	suspended, tripwire panel   403, exactly ONE refusal row (this tenant, this admin,
//	                            the route's pattern, {"reason": code}), no store call
//	                            (a call would trip), no authenticator call besides the
//	                            resolver, and the body in the route's format: the
//	                            relay's JSON word, everyone else the problem page
//	not suspended, real fakes   NOT 403 -- the vacuity check: a route that refused
//	                            everybody would pass the first row
//
// and an exception, suspended, answers neither 403 nor a refusal row. Every exception
// must be a route the router serves, and there must be exactly eleven.
//
// PART II -- what it catches: the gate taken out of ProtectWriting; the gate moved
// ahead of the resolver (no identity -- no row); a route added to the sibling group
// (a further exception: not 403, not on the list), or a webhook act opened in
// internal/webhook/endpoints' table (the same); an exception moved behind the gate (a
// listed route answers 403); a refusal without its row, or with two; a store reached
// before the refusal; the relay answered in HTML. PART III: a form not on that list is
// code review's -- no completeness claim.
func TestSuspensionRoutes_EveryPanelWriteIsGatedOrANamedException(t *testing.T) {
	tenant, admin, session := uuid.New(), uuid.New(), uuid.New()
	exceptions := suspensionExceptions()
	if len(exceptions) != 11 {
		t.Fatalf("the exception list has %d entries; ADR 0025 §3 names eight and K-26 three", len(exceptions))
	}

	// BOTH ROLES (2nd round, F2): the gate runs before any handler's role check, so a
	// manager's write is refused exactly like an owner's -- 403, one row, no store
	// call. The vacuity check below is the owner's: a manager is refused some writes by
	// role anyway (owner-only routes answer 403 without a suspension too), so for the
	// manager it only requires that such a 403 is not the suspension's.
	for _, role := range []string{"owner", "manager"} {
		for _, code := range db.SuspensionReasons() {
			t.Run(role+"/"+string(code), func(t *testing.T) {
				everyPanelWriteIsGated(t, role, code, tenant, admin, session, exceptions)
			})
		}
	}
}

// everyPanelWriteIsGated is one (role, code) run of the test above.
func everyPanelWriteIsGated(t *testing.T, role string, code db.SuspensionReason, tenant, admin, session uuid.UUID, exceptions map[string]string) {
	t.Helper()
	suspendedAdmins := suspendedIdentityAs(role, tenant, admin, session, &code)
	trail := &fakeTrail{}
	gatedPanel := tripwirePanel(t, suspendedAdmins, trail, discardLogger())
	// The SAME suspended identity over the ordinary fakes, for the exceptions:
	// their handlers run, so a tripwire would trip on a legitimate call.
	exceptionTrail := &fakeTrail{}
	exceptionPanel := newAdminRouterWithActions(t, suspendedIdentityAs(role, tenant, admin, session, &code),
		exceptionTrail, newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{})
	openPanel := newAdminRouterWithActions(t, suspendedIdentityAs(role, tenant, admin, session, nil),
		&fakeTrail{}, newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{})
	isSuspensionAnswer := func(rec *httptest.ResponseRecorder) bool {
		return rec.Code == http.StatusForbidden &&
			(strings.Contains(rec.Body.String(), suspensionHeading) || strings.Contains(rec.Body.String(), faultSuspended))
	}

	walked := walkedRoutes(t, gatedPanel)
	present := map[string]bool{}
	gated := 0
	for _, mr := range walked {
		if isRead(mr) {
			continue
		}
		present[mr] = true
		method, route, _ := strings.Cut(mr, " ")
		if why, ok := exceptions[mr]; ok {
			before := len(refusalEvents(exceptionTrail))
			rec, tripped := suspensionDrive(exceptionPanel, method, route)
			if tripped != nil {
				t.Errorf("%s (%s) panicked: %v", mr, why, tripped)
				continue
			}
			refusedByRole := role != "owner" && rec.Code == http.StatusForbidden && !isSuspensionAnswer(rec)
			if (rec.Code == http.StatusForbidden && !refusedByRole) || len(refusalEvents(exceptionTrail)) != before {
				t.Errorf("%s is on ADR 0025's exception list (%s) and a suspended business's %s got %d "+
					"and %d refusal row(s) for it", mr, why, role, rec.Code, len(refusalEvents(exceptionTrail))-before)
			}
			continue
		}

		gated++
		before := trail.total()
		authBefore := len(suspendedAdmins.issuedFor) + suspendedAdmins.authCalls + suspendedAdmins.revokeCalls
		rec, tripped := suspensionDrive(gatedPanel, method, route)
		if tripped != nil {
			t.Errorf("%s reached a store while the business was suspended (tripwire: %v)", mr, tripped)
			continue
		}
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s answered %d for a suspended business, want 403: it is neither gated nor "+
				"one of the named exceptions", mr, rec.Code)
			continue
		}
		events := trail.eventsSnapshot()[before:]
		if len(events) != 1 {
			t.Errorf("%s: the refusal wrote %d audit row(s), want exactly 1", mr, len(events))
			continue
		}
		e := events[0]
		want := audit.Event{TenantID: tenant, ActorID: ptr(admin), Action: ActionTenantWriteRefused,
			Target: route, Detail: suspensionRefusalDetail{Reason: string(code)}}
		if !reflect.DeepEqual(e, want) {
			t.Errorf("%s: the refusal row is %+v, want %+v", mr, e, want)
		}
		if n := len(suspendedAdmins.issuedFor) + suspendedAdmins.authCalls + suspendedAdmins.revokeCalls; n != authBefore || suspendedAdmins.changeCalls() != 0 {
			t.Errorf("%s: the refusal reached the authenticator beyond resolving the session", mr)
		}
		ct := rec.Header().Get("Content-Type")
		if encodeRelayRoutes[route] {
			if !strings.HasPrefix(ct, "application/json") || strings.TrimSpace(rec.Body.String()) != `{"fault":"suspended"}` {
				t.Errorf("%s: the relay's refusal is %q %q, want its JSON word", mr, ct, rec.Body.String())
			}
		} else if !strings.HasPrefix(ct, "text/html") || !strings.Contains(rec.Body.String(), suspensionHeading) {
			t.Errorf("%s: the refusal is %q and does not say %q", mr, ct, suspensionHeading)
		}

		// THE VACUITY CHECK: the same route, the same chain, no suspension.
		open, tripped := suspensionDrive(openPanel, method, route)
		switch {
		case tripped != nil:
			t.Errorf("%s panicked without a suspension: %v", mr, tripped)
		case isSuspensionAnswer(open):
			t.Errorf("%s answers the SUSPENSION's 403 without a suspension", mr)
		case open.Code == http.StatusForbidden && role == "owner":
			t.Errorf("%s answers 403 WITHOUT a suspension too; the row above proves nothing about the gate", mr)
		}
	}
	for mr, why := range exceptions {
		if !present[mr] {
			t.Errorf("the exception %s (%s) is not a route the panel serves; the list names a route "+
				"that does not exist", mr, why)
		}
	}
	// ANTI-VACUITY: the panel writes far more than a handful of things, and the
	// walk found every one of them through the gate.
	if gated < 20 {
		t.Fatalf("only %d gated route(s) were driven; this walk is not reading the panel", gated)
	}
	if code == db.SuspensionNonPayment && role == "owner" {
		t.Logf("%d gated panel write route(s), %d named exception(s)", gated, len(exceptions))
	}
}

// TestSuspensionRelay_TheThreeRoutesAnswerSuspendedJSONAndTouchNothing is ADR 0025 §3
// refusal 4 on its own: the relay's three routes answer 403 with {"fault":"suspended"}
// -- a word the server's own vocabulary declares -- and the encoder is never reached
// (the tripwire panel's encoder would panic). The word is the CLIENT'S contract: a
// non-JSON body reads "Sign in again" on the phone, and refused blames the chip.
func TestSuspensionRelay_TheThreeRoutesAnswerSuspendedJSONAndTouchNothing(t *testing.T) {
	if !slices.Contains(encodeFaults, faultSuspended) || faultSuspended != "suspended" {
		t.Fatalf("faultSuspended is %q and declared=%v; ADR 0025 names the word `suspended`",
			faultSuspended, slices.Contains(encodeFaults, faultSuspended))
	}
	trail := &fakeTrail{}
	h := tripwirePanel(t, suspendedIdentity(uuid.New(), uuid.New(), uuid.New(), codePtr(db.SuspensionTermsBreach)), trail, discardLogger())
	routes := []string{plaqueEncodeHref, plaqueEncodeStepHref, plaqueEncodeAbortHref}
	if len(encodeRelayRoutes) != len(routes) {
		t.Fatalf("encodeRelayRoutes has %d entries; the relay has three routes", len(encodeRelayRoutes))
	}
	for _, route := range routes {
		if !encodeRelayRoutes[route] {
			t.Errorf("%s is not one of encodeRelayRoutes", route)
		}
		before := len(refusalEvents(trail))
		rec, tripped := suspensionDrive(h, http.MethodPost, route)
		if tripped != nil {
			t.Fatalf("POST %s reached the encoder or a store: %v", route, tripped)
		}
		var body map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("POST %s: the body is not JSON (%v): %q", route, err, rec.Body.String())
		}
		if rec.Code != http.StatusForbidden || len(body) != 1 || body["fault"] != faultSuspended {
			t.Errorf("POST %s = %d %v, want 403 {\"fault\":\"suspended\"}", route, rec.Code, body)
		}
		if rec.Header().Get("Cache-Control") != "no-store" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("POST %s: the refusal does not carry the relay's header block", route)
		}
		if n := len(refusalEvents(trail)) - before; n != 1 {
			t.Errorf("POST %s wrote %d refusal row(s), want 1", route, n)
		}
	}
}

// TestSuspensionRoutes_ReadsAnswerTheSameWithOrWithoutASuspension is ADR 0025 §4's
// read half at the panel level: every GET and HEAD AdminAuth.Mount serves answers the
// same status for a suspended business as for an unsuspended one, and writes no
// refusal row. (The two exports answering 200 over real data, and the hours export's
// own trail row, are TestPanelSuspensionDB_ReadsExportsSignInAndResetStayOpen's.)
func TestSuspensionRoutes_ReadsAnswerTheSameWithOrWithoutASuspension(t *testing.T) {
	tenant, admin, session := uuid.New(), uuid.New(), uuid.New()
	suspendedTrail := &fakeTrail{}
	suspended := newAdminRouterWithActions(t, suspendedIdentity(tenant, admin, session, codePtr(db.SuspensionNonPayment)),
		suspendedTrail, newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{})
	open := newAdminRouterWithActions(t, suspendedIdentity(tenant, admin, session, nil),
		&fakeTrail{}, newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{})
	reads := 0
	for _, mr := range walkedRoutes(t, suspended) {
		if !isRead(mr) {
			continue
		}
		reads++
		method, route, _ := strings.Cut(mr, " ")
		if strings.Contains(route, "{") {
			t.Fatalf("%s carries a parameter; this test drives patterns as paths and would need a value", mr)
		}
		a, tripA := suspensionDrive(suspended, method, route)
		b, tripB := suspensionDrive(open, method, route)
		if tripA != nil || tripB != nil {
			t.Fatalf("%s panicked: %v / %v", mr, tripA, tripB)
		}
		if a.Code != b.Code {
			t.Errorf("%s answers %d for a suspended business and %d for an unsuspended one", mr, a.Code, b.Code)
		}
	}
	if n := len(refusalEvents(suspendedTrail)); n != 0 {
		t.Errorf("reading a suspended business's panel wrote %d refusal row(s)", n)
	}
	if reads < len(pages.PanelSections) {
		t.Fatalf("only %d read route(s) were walked; the panel has at least one per section", reads)
	}
}

// TestSuspensionStrip_EverySectionShowsItInTheBusinesssTimezone is ADR 0025 §3's
// "Panel kabuğu": every section of a suspended business carries the strip, dated in the
// business's OWN timezone (Europe/Malta: 01:30 on 8 Oct for a suspension at 23:30 UTC
// on 7 Oct), with the reason's sentence; an unsuspended business's sections carry no
// strip; and a timezone that cannot be read leaves the strip up, dated in UTC, saying so.
func TestSuspensionStrip_EverySectionShowsItInTheBusinesssTimezone(t *testing.T) {
	tenant, admin, session := uuid.New(), uuid.New(), uuid.New()
	render := func(code *db.SuspensionReason, accounts *fakeAccounts, href string) string {
		t.Helper()
		h, err := NewAdminAuth(suspendedIdentity(tenant, admin, session, code), &fakeTrail{}, newFakeLedger(), newFakeLedger(),
			&fakeReviewer{}, &fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
			newFakeScribe(), newFakeBooks(), accounts, newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{},
			adminTestConfig(), discardLogger())
		if err != nil {
			t.Fatalf("NewAdminAuth: %v", err)
		}
		r := chi.NewRouter()
		h.Mount(r)
		rec, tripped := suspensionDrive(r, http.MethodGet, href)
		if tripped != nil || rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d (%v)", href, rec.Code, tripped)
		}
		return rec.Body.String()
	}
	for _, s := range pages.PanelSections {
		t.Run(s.Href, func(t *testing.T) {
			page := render(codePtr(db.SuspensionNonPayment), newFakeAccount(), s.Href)
			for _, want := range []string{suspensionHeading, htmlText(suspensionSentenceNonPayment), "8 Oct 2026", htmlText(suspensionStillWorks)} {
				if !strings.Contains(page, want) {
					t.Errorf("a suspended business's %s does not show %q", s.Href, want)
				}
			}
			if strings.Contains(page, "7 Oct 2026") {
				t.Errorf("%s dates the suspension in UTC although the business's timezone was read", s.Href)
			}
			// THE TONE (2nd round, F3): the strip is the product's WARNING notice --
			// saffron frame, saffron ground, ink words (tappa-brand: a warning is
			// saffron; tomato is a refusal or a danger). Read off the <section> that
			// carries the strip's heading.
			if class := stripSectionClass(t, page); !strings.Contains(class, "border-saffron") ||
				!strings.Contains(class, "bg-saffron-lite") || strings.Contains(class, "tomato") {
				t.Errorf("%s: the strip's notice is %q, want the saffron warning tone", s.Href, class)
			}
			if open := render(nil, newFakeAccount(), s.Href); strings.Contains(open, suspensionHeading) {
				t.Errorf("an unsuspended business's %s shows the suspension strip", s.Href)
			}
		})
	}
	broken := newFakeAccount()
	broken.readErr = errors.New("the settings read failed")
	page := render(codePtr(db.SuspensionNonPayment), broken, transactionsHref)
	if !strings.Contains(page, suspensionHeading) || !strings.Contains(page, "7 Oct 2026 (UTC)") {
		t.Errorf("with the timezone unreadable the strip must stay up and say it is dated in UTC")
	}
}

// stripSectionClass is the class attribute of the <section> that holds the strip's
// heading: the last section opened before the heading's text.
func stripSectionClass(t *testing.T, page string) string {
	t.Helper()
	at := strings.Index(page, ">"+suspensionHeading+"<")
	if at < 0 {
		t.Fatalf("no strip heading in the page")
	}
	open := strings.LastIndex(page[:at], `<section class="`)
	if open < 0 {
		t.Fatalf("the strip's heading is not inside a <section class=...>")
	}
	rest := page[open+len(`<section class="`):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("an unterminated class attribute")
	}
	return rest[:end]
}

// TestSuspensionSentences_OnlyTwoReasonsHaveTheirOwnSentence pins ADR 0025 §3's
// "Nötr cümle PAYLAŞILIR": security_hold, customer_request and a code this build does
// not know resolve to ONE constant, and only non_payment and terms_breach have a
// sentence of their own -- three distinct sentences in all. Splitting one of the three
// off (a security_hold sentence of its own, which the public source would give away)
// or folding one of the two into the neutral one (taking away what the business needs
// to fix it) is red. The neutral sentence names no reason.
func TestSuspensionSentences_OnlyTwoReasonsHaveTheirOwnSentence(t *testing.T) {
	for _, shared := range []string{string(db.SuspensionSecurityHold), string(db.SuspensionCustomerRequest), "a-code-from-a-newer-build", ""} {
		if got := suspensionSentence(shared); got != suspensionSentenceNeutral {
			t.Errorf("suspensionSentence(%q) = %q, want the shared neutral sentence", shared, got)
		}
	}
	own := map[string]string{
		string(db.SuspensionNonPayment):  suspensionSentenceNonPayment,
		string(db.SuspensionTermsBreach): suspensionSentenceTermsBreach,
	}
	for code, want := range own {
		if got := suspensionSentence(code); got != want || got == suspensionSentenceNeutral {
			t.Errorf("suspensionSentence(%q) = %q, want its own sentence %q", code, got, want)
		}
	}
	distinct := map[string]bool{}
	for _, s := range suspensionSentences {
		distinct[s] = true
	}
	if len(distinct) != 3 || suspensionSentenceNonPayment == suspensionSentenceTermsBreach {
		t.Errorf("the map carries %d distinct sentence(s), want 3 (two own, one shared)", len(distinct))
	}
	lower := strings.ToLower(suspensionSentenceNeutral)
	for _, word := range []string{"security", "secur", "breach", "payment", "request", "customer", "compromis", "fraud", "suspicious"} {
		if strings.Contains(lower, word) {
			t.Errorf("the neutral sentence names a reason (%q): %q", word, suspensionSentenceNeutral)
		}
	}
	if !strings.Contains(suspensionSentenceNeutral, "Contact Taptime") {
		t.Errorf("the neutral sentence must say to contact Taptime: %q", suspensionSentenceNeutral)
	}
}

// TestSuspensionSentences_CoverTheClosedReasonSet binds the panel's sentence map --
// ADR 0025 §1's fourth copy of the reason set -- to internal/db's closed set, which
// TestOperator00063_TheReasonSetIsOneSetInEveryCopy binds to the schema's CHECK and to
// op_suspend_tenant's refusal. It lives here, not in that test, because that test is
// package db and internal/handler imports internal/db: the reverse import is a cycle.
// And the binding is a test rather than a shared type because the panel's production
// code may not name internal/db's operator names (ADR 0021 §3.6,
// TestCustomerPanel_ImportsNoOperatorPackage); this file is a test and may.
func TestSuspensionSentences_CoverTheClosedReasonSet(t *testing.T) {
	var keys, set []string
	for k := range suspensionSentences {
		keys = append(keys, k)
	}
	for _, r := range db.SuspensionReasons() {
		set = append(set, string(r))
	}
	sort.Strings(keys)
	sort.Strings(set)
	if !slices.Equal(keys, set) {
		t.Errorf("the sentence map's codes are %v, the closed set is %v", keys, set)
	}
}

// TestSuspensionScreens_NeverRenderTheReasonCode: no code -- of the closed four, or one
// this build does not know -- reaches a body, on the 403 page, on the relay's JSON, or
// on a section's strip (ADR 0025 §3: "ham kod hiçbir yüzeyde render edilmez").
func TestSuspensionScreens_NeverRenderTheReasonCode(t *testing.T) {
	codes := append(db.SuspensionReasons(), "a_code_from_a_newer_build")
	for _, code := range codes {
		code := code
		tenant, admin, session := uuid.New(), uuid.New(), uuid.New()
		gated := tripwirePanel(t, suspendedIdentity(tenant, admin, session, &code), &fakeTrail{}, discardLogger())
		sections := newAdminRouterWithActions(t, suspendedIdentity(tenant, admin, session, &code),
			&fakeTrail{}, newFakeLedger(), &fakeReviewer{}, &fakeStaff{}, &fakeInviter{})
		bodies := map[string]string{}
		for _, target := range []struct {
			h      http.Handler
			method string
			route  string
		}{
			{gated, http.MethodPost, reviewHref},
			{gated, http.MethodPost, accountHref},
			{gated, http.MethodPost, plaqueEncodeStepHref},
			{sections, http.MethodGet, transactionsHref},
			{sections, http.MethodGet, accountHref},
		} {
			rec, tripped := suspensionDrive(target.h, target.method, target.route)
			if tripped != nil {
				t.Fatalf("%s %s panicked: %v", target.method, target.route, tripped)
			}
			bodies[target.method+" "+target.route] = rec.Body.String()
		}
		for where, body := range bodies {
			if body == "" {
				t.Fatalf("%s rendered nothing; the assertion below would be vacuous", where)
			}
			for _, form := range []string{string(code), strings.ReplaceAll(string(code), "_", " ")} {
				if strings.Contains(body, form) {
					t.Errorf("%s renders the reason code %q (as %q)", where, code, form)
				}
			}
		}
	}
}

// TestSuspensionGate_AFailedRefusalRowStillRefusesAndLogsThreeFields is ADR 0025 §3
// refusal 3: when the refusal row cannot be written the answer is still 403, and the
// ERROR line carries the tenant id, the SQLSTATE and the code from the closed set --
// and nothing else: not the database's message or detail, not the route, not the
// admin, not the form. A code this build does not know is logged as "unrecognised".
func TestSuspensionGate_AFailedRefusalRowStillRefusesAndLogsThreeFields(t *testing.T) {
	const sentinel = "SENTINEL-the-database-said-this"
	for _, c := range []struct {
		code db.SuspensionReason
		want string
	}{
		{db.SuspensionSecurityHold, "security_hold"},
		{"a_code_from_a_newer_build", "unrecognised"},
	} {
		for _, route := range []string{venueSaveHref, plaqueEncodeHref} {
			var buf bytes.Buffer
			log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
			trail := &refusingTrail{err: &pgconn.PgError{Code: "42501", Message: sentinel, Detail: sentinel}}
			tenant := uuid.New()
			code := c.code
			h := tripwirePanel(t, suspendedIdentity(tenant, uuid.New(), uuid.New(), &code), trail, log)
			req := httptest.NewRequest(http.MethodPost, route, strings.NewReader("full_name=PROBE-FORM-VALUE"))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("Origin", testBaseURL)
			req.RemoteAddr = "203.0.113.9:1234"
			req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: suspensionCookieValue})
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden || trail.calls != 1 {
				t.Fatalf("%s with a failing trail: %d after %d write attempt(s), want 403 after 1", route, rec.Code, trail.calls)
			}
			lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
			var found int
			for _, line := range lines {
				var m map[string]any
				if err := json.Unmarshal([]byte(line), &m); err != nil {
					t.Fatalf("a log line is not JSON: %q", line)
				}
				if m["level"] != "ERROR" {
					continue
				}
				found++
				var keys []string
				for k := range m {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				if want := []string{"level", "msg", "reason", "sqlstate", "tenant_id", "time"}; !slices.Equal(keys, want) {
					t.Errorf("the ERROR line carries %v, want exactly %v", keys, want)
				}
				if m["tenant_id"] != tenant.String() || m["sqlstate"] != "42501" || m["reason"] != c.want {
					t.Errorf("the ERROR line is %v", m)
				}
			}
			if found != 1 {
				t.Errorf("%d ERROR line(s), want 1", found)
			}
			for _, leak := range []string{sentinel, "PROBE-FORM-VALUE", route, suspensionCookieValue} {
				if strings.Contains(buf.String(), leak) {
					t.Errorf("the log carries %q", leak)
				}
			}
		}
	}
	if got := sqlstateOf(errors.New("no code")); got != "none" {
		t.Errorf("sqlstateOf(an error without a code) = %q, want none", got)
	}
}

// TestSuspensionGate_TheRowIsWrittenBeforeTheAnswer is ADR 0025 §3 refusal 1's ORDER:
// the tenant's refusal row is written first and only then is 403 answered -- on the
// problem page and on the relay's JSON alike. The trail looks at the response at the
// moment it is asked to write: nothing may have been sent yet.
func TestSuspensionGate_TheRowIsWrittenBeforeTheAnswer(t *testing.T) {
	for _, route := range []string{venueSaveHref, plaqueEncodeStepHref} {
		trail := &orderTrail{}
		h := tripwirePanel(t, suspendedIdentity(uuid.New(), uuid.New(), uuid.New(), codePtr(db.SuspensionNonPayment)), trail, discardLogger())
		req := httptest.NewRequest(http.MethodPost, route, strings.NewReader(""))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", testBaseURL)
		req.RemoteAddr = "203.0.113.9:1234"
		req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: suspensionCookieValue})
		trail.rec = httptest.NewRecorder()
		h.ServeHTTP(trail.rec, req)
		if trail.rec.Code != http.StatusForbidden || trail.calls != 1 {
			t.Fatalf("%s: %d after %d row(s), want 403 after 1", route, trail.rec.Code, trail.calls)
		}
		if trail.answeredFirst {
			t.Errorf("%s: the 403 had already been written when the refusal row was", route)
		}
	}
}

// orderTrail notes whether the response had begun when a row was written.
type orderTrail struct {
	rec           *httptest.ResponseRecorder
	calls         int
	answeredFirst bool
}

func (o *orderTrail) Record(_ context.Context, _ audit.Event) (uuid.UUID, error) {
	o.calls++
	// NewRecorder starts at 200 with an empty body; WriteHeader(403) changes the code.
	if o.rec.Code != http.StatusOK || o.rec.Body.Len() > 0 {
		o.answeredFirst = true
	}
	return uuid.New(), nil
}

// refusingTrail refuses every row it is given.
type refusingTrail struct {
	err   error
	calls int
}

func (f *refusingTrail) Record(_ context.Context, _ audit.Event) (uuid.UUID, error) {
	f.calls++
	return uuid.Nil, f.err
}

// TestSuspensionChain_TheGateIsTheLastRingOfProtectWriting pins the STRUCTURE ADR 0025
// §3 decides, read from the source: ProtectWriting nests exactly floodGate,
// sameOriginGate, requireAdmin, sessionGate and suspensionGate, in that order;
// protectWritingThroughSuspension is the same four without the last; mountWriting has
// exactly two groups -- the first on ProtectWriting, the second on
// protectWritingThroughSuspension -- and the second registers exactly the seven handlers
// of the exception list it holds by name (the record paths and the two key revocations)
// plus the webhook's OPEN acts (mountWebhookWrites(r, true)), the first the webhook's
// closed ones (mountWebhookWrites(r, false)); mountWebhookWrites is called nowhere else;
// and nothing else in the package names the exempt chain. Which webhook route each call
// registers is internal/webhook/endpoints' table, measured on the router by
// TestSuspensionRoutes_EveryPanelWriteIsGatedOrANamedException.
func TestSuspensionChain_TheGateIsTheLastRingOfProtectWriting(t *testing.T) {
	fset, files := parsePackageSource(t, ".")
	funcs := map[string]*ast.FuncDecl{}
	exemptUses := 0
	for _, f := range files {
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok && fd.Body != nil {
				funcs[fd.Name.Name] = fd
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && sel.Sel.Name == "protectWritingThroughSuspension" {
				exemptUses++
			}
			return true
		})
	}
	// ringsOf follows a(b(c(next))) and returns the method names outermost first.
	ringsOf := func(name string) []string {
		fd := funcs[name]
		if fd == nil {
			t.Fatalf("%s is gone; this test reads it by name", name)
		}
		var lit *ast.FuncLit
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			if l, ok := n.(*ast.FuncLit); ok && lit == nil {
				lit = l
			}
			return true
		})
		if lit == nil || len(lit.Body.List) != 1 {
			t.Fatalf("%s does not return one middleware literal with one statement", name)
		}
		ret, ok := lit.Body.List[0].(*ast.ReturnStmt)
		if !ok || len(ret.Results) != 1 {
			t.Fatalf("%s's literal does not return one expression", name)
		}
		var rings []string
		e := ret.Results[0]
		for {
			call, ok := e.(*ast.CallExpr)
			if !ok {
				break
			}
			switch fn := call.Fun.(type) {
			case *ast.SelectorExpr: // a.sameOriginGate(…), a.suspensionGate(…)
				rings = append(rings, fn.Sel.Name)
			case *ast.CallExpr: // a.floodGate("…")(…), a.requireAdmin()(…)
				if sel, ok := fn.Fun.(*ast.SelectorExpr); ok {
					rings = append(rings, sel.Sel.Name)
				}
			}
			if len(call.Args) != 1 {
				break
			}
			e = call.Args[0]
		}
		if id, ok := e.(*ast.Ident); !ok || id.Name != "next" {
			t.Fatalf("%s's chain does not end in next", name)
		}
		return rings
	}
	if got, want := ringsOf("ProtectWriting"), []string{"floodGate", "sameOriginGate", "requireAdmin", "sessionGate", "suspensionGate"}; !slices.Equal(got, want) {
		t.Errorf("ProtectWriting is %v, want %v", got, want)
	}
	if got, want := ringsOf("protectWritingThroughSuspension"), []string{"floodGate", "sameOriginGate", "requireAdmin", "sessionGate"}; !slices.Equal(got, want) {
		t.Errorf("protectWritingThroughSuspension is %v, want %v", got, want)
	}
	// The declaration's name is not a selector, so the one selector is mountWriting's
	// call; a second is a second route group on the exempt chain.
	if exemptUses != 1 {
		t.Errorf("protectWritingThroughSuspension is called %d time(s); want once, in mountWriting", exemptUses)
	}

	mw := funcs["mountWriting"]
	if mw == nil {
		t.Fatal("mountWriting is gone; this test reads it by name")
	}
	var groups []*ast.FuncLit
	for _, st := range mw.Body.List {
		es, ok := st.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := es.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Group" && len(call.Args) == 1 {
			if lit, ok := call.Args[0].(*ast.FuncLit); ok {
				groups = append(groups, lit)
			}
		}
	}
	if len(groups) != 2 {
		t.Fatalf("mountWriting has %d top-level group(s), want 2 (the gated one and the exempt sibling)", len(groups))
	}
	chainOf := func(g *ast.FuncLit) string {
		for _, st := range g.Body.List {
			es, ok := st.(*ast.ExprStmt)
			if !ok {
				continue
			}
			call, ok := es.X.(*ast.CallExpr)
			if !ok {
				continue
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Use" && len(call.Args) == 1 {
				if inner, ok := call.Args[0].(*ast.CallExpr); ok {
					if s, ok := inner.Fun.(*ast.SelectorExpr); ok {
						return s.Sel.Name
					}
				}
			}
		}
		return ""
	}
	if got := chainOf(groups[0]); got != "ProtectWriting" {
		t.Errorf("mountWriting's first group uses %q, want ProtectWriting", got)
	}
	if got := chainOf(groups[1]); got != "protectWritingThroughSuspension" {
		t.Errorf("mountWriting's second group uses %q, want protectWritingThroughSuspension", got)
	}
	var exempt []string
	ast.Inspect(groups[1], func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Group" {
			t.Errorf("the exempt group nests a group at %s; it holds routes, not chains", fset.Position(call.Pos()))
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name != "Use" && sel.Sel.Name != "Group" && len(call.Args) == 2 {
			if h, ok := call.Args[1].(*ast.SelectorExpr); ok {
				exempt = append(exempt, sel.Sel.Name+" "+h.Sel.Name)
			}
		}
		return true
	})
	sort.Strings(exempt)
	want := []string{"Post accountPasswordSave", "Post apiKeyRevoke", "Post apiKeyRevokeAll", "Post employeeAdd",
		"Post employeeInvite", "Post manualEntryRecord", "Post manualEntryReview"}
	if !slices.Equal(exempt, want) {
		t.Errorf("the exempt group registers %v, want exactly %v", exempt, want)
	}
	// The webhook's two calls: one per group, each with its literal class.
	webhookCalls := func(n ast.Node) []string {
		var out []string
		ast.Inspect(n, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "mountWebhookWrites" && len(call.Args) == 2 {
				if id, ok := call.Args[1].(*ast.Ident); ok {
					out = append(out, id.Name)
				} else {
					out = append(out, "?")
				}
			}
			return true
		})
		return out
	}
	if got := webhookCalls(groups[0]); !slices.Equal(got, []string{"false"}) {
		t.Errorf("the gated group calls mountWebhookWrites with %v, want exactly [false]", got)
	}
	if got := webhookCalls(groups[1]); !slices.Equal(got, []string{"true"}) {
		t.Errorf("the exempt group calls mountWebhookWrites with %v, want exactly [true]", got)
	}
	all := 0
	for _, f := range files {
		all += len(webhookCalls(f))
	}
	if all != 2 {
		t.Errorf("mountWebhookWrites is called %d time(s) in the package; want twice, once per group of mountWriting", all)
	}
}

// TestSuspension_TheTapPathNeverNamesASuspension is ADR 0025 §4's structural half: the
// decision engine, the policy engine, the check-in orchestrator, SUN verification, the
// employee session, the tap page's reader (internal/domain/tenant's directory.go --
// Directory.TapPage), the holding cross-tap's (peerplaque.go -- Directory.PeerTapPage --
// and internal/db's holding.go, the definer calls) and the tap, check-in and activation
// handlers name no suspension
// -- no identifier and no string literal (comments are not read: tapcontext.go's clock
// note mentions "a suspended VM", which is about clocks) -- and neither does the SQL of
// any db/queries statement those files call by name (comment lines excluded).
//
// 🔴 THE PAGE'S READER AND THE SQL ARE THE 2nd ROUND'S (F1). The first version scanned
// the check-in's side only; a suspension read added to Directory.TapPage -- the read
// GET /t makes before it mints a context -- refused a suspended business's tap PAGE and
// stayed green here. The query set is DERIVED: every `-- name:` block whose name is an
// identifier in the scanned Go, so a statement the tap path starts calling joins the
// scan without an edit. The behavioural half (a suspended business's taps -- page and
// check-in -- are decided exactly as an unsuspended one's) is
// TestSuspensionDB_TapsAreDecidedAsIfThereWereNoSuspension, and it alone sees a read
// that names nothing.
func TestSuspension_TheTapPathNeverNamesASuspension(t *testing.T) {
	root := filepath.Join("..", "..")
	dirs := []string{
		filepath.Join(root, "internal", "domain", "tap"),
		filepath.Join(root, "internal", "policy"),
		filepath.Join(root, "internal", "domain", "checkin"),
		filepath.Join(root, "internal", "sun"),
		filepath.Join(root, "internal", "session"),
	}
	files := []string{"tap.go", "tapcontext.go", "checkin.go", "activate.go",
		filepath.Join(root, "internal", "domain", "tenant", "directory.go"),
		// The holding cross-tap (M11, ADR 0027): the peer's page and the definer calls.
		filepath.Join(root, "internal", "domain", "tenant", "peerplaque.go"),
		filepath.Join(root, "internal", "db", "holding.go")}
	var scanned []string
	for _, d := range dirs {
		if err := filepath.WalkDir(d, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !e.IsDir() && strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go") {
				scanned = append(scanned, p)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			t.Fatalf("%s: %v -- a file of the tap path moved; this scan names it", f, err)
		}
		scanned = append(scanned, f)
	}
	if len(scanned) < 20 {
		t.Fatalf("scanned %d file(s); the tap path has more", len(scanned))
	}
	idents := map[string]bool{}
	names := 0
	for _, p := range scanned {
		f, err := parser.ParseFile(token.NewFileSet(), p, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			var text string
			switch v := n.(type) {
			case *ast.Ident:
				text = v.Name
				idents[v.Name] = true
				names++
			case *ast.BasicLit:
				text = v.Value
			default:
				return true
			}
			if strings.Contains(strings.ToLower(text), "suspen") {
				t.Errorf("%s names a suspension (%s); ADR 0025 §4 keeps the tap path ignorant of it", p, text)
			}
			return true
		})
	}
	if names < 1000 {
		t.Fatalf("the scan read %d identifier(s); the tap path has far more", names)
	}

	// The SQL those files run: every named db/queries statement they call.
	sqlFiles, err := filepath.Glob(filepath.Join(root, "db", "queries", "*.sql"))
	if err != nil || len(sqlFiles) == 0 {
		t.Fatalf("no db/queries/*.sql (%v)", err)
	}
	var queried []string
	for _, p := range sqlFiles {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		blocks := strings.Split(string(b), "-- name: ")
		for _, block := range blocks[1:] {
			name := strings.Fields(block)[0]
			if !idents[name] {
				continue
			}
			queried = append(queried, name)
			for _, line := range strings.Split(block, "\n")[1:] {
				if strings.HasPrefix(strings.TrimSpace(line), "--") {
					continue
				}
				if strings.Contains(strings.ToLower(line), "suspen") {
					t.Errorf("%s's SQL (%s) names a suspension: %q; ADR 0025 §4 keeps the tap path ignorant of it",
						name, filepath.Base(p), strings.TrimSpace(line))
				}
			}
		}
	}
	// ANTI-VACUITY: the page's reader's statements are among them, by name.
	for _, must := range []string{"GetEmployeeActivationContext", "GetLocationWiFi", "GetTenantBrand"} {
		if !slices.Contains(queried, must) {
			t.Fatalf("the derived query set %v lacks %s, which Directory.TapPage runs -- the scan is not reading "+
				"the page's path", queried, must)
		}
	}
	t.Logf("%d file(s), %d identifier(s), %d db/queries statement(s) scanned", len(scanned), names, len(queried))
}
