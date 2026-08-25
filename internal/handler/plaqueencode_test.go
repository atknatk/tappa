package handler

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/encode"
)

// The plaque encode relay's tests — ADR 0017 §6 md. 10, md. 12 and md. 14, and the
// budget none of them names.
//
// 🔴 THE FOUR AST TESTS BELOW EXIST BECAUSE THE BEHAVIOURAL ONES CANNOT CLOSE WHAT
// THEY ASSERT. A test that posts one forged tenant proves that ONE spelling is
// ignored; the question "could a tenant arrive from the request" has an answer space
// nobody can enumerate (B2c-2a lost five consecutive designs to exactly that shape).
// So the source tests do not ask it. They ask a finite question instead: through which
// EXPRESSION does a tenant reach encode.Store.Begin. There is one, it is pinned
// character for character, and the behavioural tests are the positive controls that
// keep the pins from being vacuous.

// --- doubles ----------------------------------------------------------------------

// fakeEncoder records what the endpoint asked the store to do. It is a double for
// *encode.Store and implements PlaqueEncoder.
//
// 🔴 IT RECORDS THE TENANT AND THE ACTOR SEPARATELY, which is what makes an argument
// SWAP visible — the same reasoning internal/encode's recordingRows gives for its own
// portCall.
type fakeEncoder struct {
	mu sync.Mutex

	begins  []encodeBeginCall
	steps   []encodeStepCall
	aborts  []encode.ID
	nextErr error
	// progress is what Step returns when nextErr is nil.
	progress encode.Progress
}

type encodeBeginCall struct {
	tenant uuid.UUID
	// admin is the RESOLVED admin id the grant carries — recorded separately from the
	// actor LABEL so that a transposition of the two is visible, and so that
	// TestPlaqueEncode_TheTenantComesFromTheSessionAndNotFromTheBody can assert the
	// value that becomes audit_log.actor_id.
	admin uuid.UUID
	actor string
}

type encodeStepCall struct {
	id    encode.ID
	rapdu []byte
}

func (f *fakeEncoder) Begin(_ context.Context, tenantID, adminID uuid.UUID, actor string) (encode.ID, encode.Progress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.begins = append(f.begins, encodeBeginCall{tenant: tenantID, admin: adminID, actor: actor})
	if f.nextErr != nil {
		return "", encode.Progress{}, f.nextErr
	}
	return encode.ID("00112233445566778899aabbccddeeff"),
		encode.Progress{Command: []byte{0x00, 0xA4, 0x04, 0x00}, Step: "begin"}, nil
}

func (f *fakeEncoder) Step(_ context.Context, id encode.ID, rapdu []byte) (encode.Progress, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = append(f.steps, encodeStepCall{id: id, rapdu: append([]byte(nil), rapdu...)})
	if f.nextErr != nil {
		return f.progress, f.nextErr
	}
	return f.progress, nil
}

func (f *fakeEncoder) Abort(id encode.ID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.aborts = append(f.aborts, id)
}

func (f *fakeEncoder) beganWith() []encodeBeginCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]encodeBeginCall(nil), f.begins...)
}

func (f *fakeEncoder) stepped() []encodeStepCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]encodeStepCall(nil), f.steps...)
}

func (f *fakeEncoder) aborted() []encode.ID {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]encode.ID(nil), f.aborts...)
}

// signedInAdmins is the fixture every test here signs in with: one live panel
// session, in panelTestTenant, as panelTestAdmin.
func signedInAdmins() *fakeAdmins {
	return &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: "owner", FullName: "KF Owner",
		}, nil
	}}
}

// encodeRouter wires the REAL AdminAuth with the REAL middleware chain and the REAL
// budgets — the M5-04 rule that a test building its own limiter measures its own
// limiter — around the encoder the caller supplies.
func encodeRouter(t *testing.T, admins *fakeAdmins, enc PlaqueEncoder, log *slog.Logger) http.Handler {
	t.Helper()
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	h, err := NewAdminAuth(admins, &fakeTrail{}, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{},
		newFakeRules(), newFakeScribe(), newFakeBooks(), newFakeTexts(), newFakeAccount(),
		enc, adminTestConfig(), log)
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	return r
}

// signedIn returns a browser carrying a panel cookie the fake verifier accepts.
func signedIn(t *testing.T, h http.Handler) *browser {
	t.Helper()
	b := newBrowser(t, h)
	b.cookies[adminauth.CookieName] = strings.Repeat("A", 43)
	return b
}

func replyOf(t *testing.T, rec *httptest.ResponseRecorder) encodeReply {
	t.Helper()
	var out encodeReply
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the body is not an encodeReply (%v): %q", err, rec.Body.String())
	}
	return out
}

func faultOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var out encodeFault
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the body is not an encodeFault (%v): %q", err, rec.Body.String())
	}
	return out.Fault
}

// --- (A) ADR 0017 §6 md. 10 — the authorisation gate ------------------------------

// TestPlaqueEncode_TheTenantComesFromTheSessionAndNotFromTheBody is md. 10's
// behavioural half.
//
// The request posts a tenant_id naming somebody else's business, in the shape a
// caller would if the endpoint took one. The round must open in the SESSION's tenant.
//
// 🔴 IT IS THE POSITIVE CONTROL FOR THE SOURCE TEST BELOW, NOT THE PROOF. It shows
// that this ONE spelling is ignored; what closes the question is that a tenant reaches
// Begin through exactly one expression, which
// TestPlaqueEncodeGrant_IsBuiltOnlyFromTheResolvedAdminSession pins.
func TestPlaqueEncode_TheTenantComesFromTheSessionAndNotFromTheBody(t *testing.T) {
	enc := &fakeEncoder{}
	b := signedIn(t, encodeRouter(t, signedInAdmins(), enc, nil))

	other := uuid.MustParse("00000000-dead-4000-8000-00000000beef")
	rec := b.do(http.MethodPost, plaqueEncodeHref, url.Values{
		"tenant_id": {other.String()},
		"tenant":    {other.String()},
		"actor":     {"whoever-i-like"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s answered %d, want 200. body=%q", plaqueEncodeHref, rec.Code, rec.Body.String())
	}
	calls := enc.beganWith()
	if len(calls) != 1 {
		t.Fatalf("the store was asked to begin %d round(s), want 1", len(calls))
	}
	if calls[0].tenant != panelTestTenant {
		t.Fatalf("the round opened in tenant %s; the request named %s and the SESSION is %s. "+
			"ADR 0017 §6 md. 10: a tenant taken from the body does not close that item, it renames it",
			calls[0].tenant, other, panelTestTenant)
	}
	// 🔴 THE RESOLVED ADMIN ID REACHES THE PORT, AND IT BECOMES audit_log.actor_id.
	// Until a fourth audit it did not: internal/encode wrote nil there and the plaque
	// card rendered every encode row as "by the system" for something a human did. The
	// id travels as its own typed argument — never parsed out of the actor label, which
	// is caller-supplied (see internal/encode/rows.go's actorIDOf).
	if calls[0].admin != panelTestAdmin {
		t.Fatalf("the round was opened with admin %s, want the RESOLVED session's %s. That "+
			"value becomes audit_log.actor_id; a wrong or empty one puts 'by the system' on a "+
			"trail row a person is responsible for", calls[0].admin, panelTestAdmin)
	}
	if calls[0].admin == uuid.Nil {
		t.Fatalf("the admin id is nil, so the trail would say 'by the system'")
	}

	// The actor is the admin's own id, not the caller's string.
	if want := "admin:" + panelTestAdmin.String(); calls[0].actor != want {
		t.Fatalf("actor = %q, want %q — the label lands in audit_log.detail.claimed_by and "+
			"must be attributable, not claimed", calls[0].actor, want)
	}
	if strings.Contains(calls[0].actor, "whoever-i-like") {
		t.Fatalf("the caller's own actor string reached the store: %q", calls[0].actor)
	}
}

// TestPlaqueEncode_AnAnonymousCallerNeverReachesTheStore. The gate is
// AdminAuth.ProtectWriting, mounted by mountWriting; this asserts it is actually in
// front of all three routes and that nothing leaks through it.
func TestPlaqueEncode_AnAnonymousCallerNeverReachesTheStore(t *testing.T) {
	enc := &fakeEncoder{}
	anonymous := newBrowser(t, encodeRouter(t, &fakeAdmins{
		verify: func() (adminauth.Resolved, error) { return adminauth.Resolved{}, adminauth.ErrNoSession },
	}, enc, nil))

	for _, path := range []string{plaqueEncodeHref, plaqueEncodeStepHref, plaqueEncodeAbortHref} {
		rec := anonymous.do(http.MethodPost, path, url.Values{"session": {strings.Repeat("a", 32)}})
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != adminLoginPath {
			t.Errorf("anonymous POST %s: %d %q, want 303 %s", path, rec.Code,
				rec.Header().Get("Location"), adminLoginPath)
		}
	}
	if n := len(enc.beganWith()) + len(enc.stepped()) + len(enc.aborted()); n != 0 {
		t.Fatalf("an anonymous caller reached the store %d time(s)", n)
	}
}

// TestPlaqueEncode_TheRelayMustDeclareItsOrigin measures the CONTRACT the relay has
// to meet, in both directions.
//
// These are mutating routes, so they inherit sameOriginGate. A native client that
// sends neither an Origin nor a Sec-Fetch-Site header is refused BEFORE the resolver
// runs. That is not a defect to work around — it is the panel's CSRF defence — but it
// is a contract somebody writing the Android side has to be told about, so it is
// measured rather than described.
func TestPlaqueEncode_TheRelayMustDeclareItsOrigin(t *testing.T) {
	enc := &fakeEncoder{}
	b := signedIn(t, encodeRouter(t, signedInAdmins(), enc, nil))

	b.origin = "" // a native HTTP client's default
	rec := b.do(http.MethodPost, plaqueEncodeHref, url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("a request with no Origin answered %d, want 303 (sameOriginGate)", rec.Code)
	}
	if n := len(enc.beganWith()); n != 0 {
		t.Fatalf("a request with no Origin opened %d round(s); the gate is supposed to refuse "+
			"BEFORE any work happens", n)
	}

	// POSITIVE CONTROL: the same request WITH the header is served. Without this the
	// assertion above would pass over an endpoint that refuses everything.
	b.origin = testBaseURL
	rec = b.do(http.MethodPost, plaqueEncodeHref, url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("a request WITH Origin: %s answered %d, want 200. body=%q",
			testBaseURL, rec.Code, rec.Body.String())
	}
	if n := len(enc.beganWith()); n != 1 {
		t.Fatalf("the round opened %d time(s) with a correct Origin, want 1", n)
	}
}

// TestPlaqueEncodeGrant_IsBuiltOnlyFromTheResolvedAdminSession is md. 10's SOURCE
// half, and the one that closes it.
//
// Three facts, all read off this file's AST:
//
//  1. Store.Begin is called exactly ONCE in this package, and its tenant argument is
//     the grant's field.
//  2. plaqueEncodeGrant is constructed only inside plaqueEncodeGrantOf.
//  3. That function's tenantID comes from httpx.AdminOf(r) and from nothing else.
//
// Together they leave no expression through which a request-supplied value could
// become the tenant a row is written into — which is a statement about a PATH rather
// than about the shapes a leak might take, and is therefore finite.
func TestPlaqueEncodeGrant_IsBuiltOnlyFromTheResolvedAdminSession(t *testing.T) {
	fset, files := parsePackageSource(t, ".")

	// (1) — where is Begin called, and with what?
	var beginArgs []string
	var beginAdminArgs []string
	var beginFiles []string
	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			// 🔴 FOUR ARGUMENTS SINCE THE FOURTH ROUND, AND THE ARITY IS PINNED RATHER
			// THAN TOLERATED. Begin gained an adminID when audit_log.actor_id was bound
			// to the resolved admin; this walk still matched on THREE, found nothing, and
			// reported "0 call sites" — a net that goes blind rather than red when the
			// thing it guards changes shape. Both the tenant and the admin argument are
			// checked below, so a transposition of the two uuids is visible too.
			if !ok || sel.Sel.Name != "Begin" || len(call.Args) != 4 {
				return true
			}
			beginFiles = append(beginFiles, name)
			beginArgs = append(beginArgs, exprText(t, fset, call.Args[1]))
			beginAdminArgs = append(beginAdminArgs, exprText(t, fset, call.Args[2]))
			return true
		})
	}
	if len(beginArgs) != 1 {
		t.Fatalf("this package calls a four-argument Begin %d time(s) (%v); the gate below "+
			"describes ONE call site, so a second one is a second authority",
			len(beginArgs), beginFiles)
	}
	if beginArgs[0] != "g.tenantID" {
		t.Fatalf("the tenant handed to Begin is %q, want %q. ADR 0017 §6 md. 10 is closed by "+
			"the tenant coming off the RESOLVED SESSION; any other expression re-opens it",
			beginArgs[0], "g.tenantID")
	}
	if beginAdminArgs[0] != "g.adminID" {
		t.Fatalf("the admin id handed to Begin is %q, want %q. It becomes audit_log.actor_id; "+
			"any other expression puts an unverified value in the column a manager's screen "+
			"renders as a name", beginAdminArgs[0], "g.adminID")
	}

	// (2) — plaqueEncodeGrant is built in exactly one function.
	builders := map[string]int{}
	for _, f := range files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			ast.Inspect(fd, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				if id, ok := lit.Type.(*ast.Ident); ok && id.Name == "plaqueEncodeGrant" {
					builders[fd.Name.Name]++
				}
				return true
			})
		}
	}
	if len(builders) != 1 || builders["plaqueEncodeGrantOf"] == 0 {
		t.Fatalf("plaqueEncodeGrant is constructed in %v; it must be built in "+
			"plaqueEncodeGrantOf and nowhere else, or 'the tenant comes from the session' "+
			"becomes a claim about several places", builders)
	}

	// (3) — what does that function put in the field, and where did it come from?
	//
	// 🔴 EVERY POPULATED LITERAL IS CHECKED, NOT THE LAST ONE SEEN, AND THE FIRST
	// VERSION OF THIS BLOCK KEPT ONLY THE LAST — WHICH A MUTATION WALKED STRAIGHT
	// THROUGH. Measured on this tree: adding an early return
	//
	//	if v, err := uuid.Parse(r.PostFormValue("tenant_id")); err == nil {
	//	    return plaqueEncodeGrant{tenantID: v, …}, true
	//	}
	//
	// left the ORIGINAL literal in place, so "the last tenantID is id.Admin.TenantID"
	// was still true and this test stayed GREEN while the endpoint took its tenant off
	// the wire. Two other tests caught it, which is the only reason the hole was
	// visible — a net whose failure depends on another net is not a net. Collecting
	// every literal makes the statement total: there is no expression, anywhere in this
	// function, that becomes a tenant except the resolved session's.
	var (
		tenantExprs []string
		actorExprs  []string
		readsAdmin  bool
	)
	for _, f := range files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Name.Name != "plaqueEncodeGrantOf" {
				continue
			}
			ast.Inspect(fd, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					if exprText(t, fset, call.Fun) == "httpx.AdminOf" {
						readsAdmin = true
					}
				}
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				for _, el := range lit.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, _ := kv.Key.(*ast.Ident)
					if key == nil {
						continue
					}
					switch key.Name {
					case "tenantID":
						tenantExprs = append(tenantExprs, exprText(t, fset, kv.Value))
					case "actor":
						actorExprs = append(actorExprs, exprText(t, fset, kv.Value))
					}
				}
				return true
			})
		}
	}
	if !readsAdmin {
		t.Fatalf("plaqueEncodeGrantOf does not call httpx.AdminOf; whatever it reads instead is " +
			"not the resolved panel session (httpx.AdminIdentity.TenantID: 'IT IS THE OUTPUT OF " +
			"RESOLUTION, NEVER AN INPUT')")
	}
	if len(tenantExprs) != 1 || len(actorExprs) != 1 {
		t.Fatalf("plaqueEncodeGrantOf populates tenantID %d time(s) (%v) and actor %d time(s) "+
			"(%v); ONE grant is built, from ONE source, or the sentence 'the tenant comes from "+
			"the session' is a claim about several expressions",
			len(tenantExprs), tenantExprs, len(actorExprs), actorExprs)
	}
	for _, got := range tenantExprs {
		if got != "id.Admin.TenantID" {
			t.Fatalf("a grant's tenantID is %q, want %q", got, "id.Admin.TenantID")
		}
	}
	for _, got := range actorExprs {
		if !strings.Contains(got, "id.Admin.AdminUserID") {
			t.Fatalf("a grant's actor is %q; it must be derived from the resolved admin's id, "+
				"because it is copied into audit_log.detail.claimed_by and is what a forensic "+
				"reader joins back to admin_users", got)
		}
	}
}

// TestPlaqueEncode_ReadsOnlyTheTwoFormKeysTheRelaySends is the supporting net.
//
// ⚠️ IT IS NOT THE PROOF AND MUST NOT BE READ AS ONE. A tenant could in principle
// arrive in a header, a path segment or a query string, and no list of accessor names
// can be complete. What makes the question closed is the PATH test above; this one
// exists so that a new form key is a visible edit rather than a silent one.
func TestPlaqueEncode_ReadsOnlyTheTwoFormKeysTheRelaySends(t *testing.T) {
	fset, files := parseOneFile(t, "plaqueencode.go")
	// ⚠️ FILE-SCOPED ON PURPOSE, UNLIKE THE WRITER GATES. Every other handler in this
	// package reads its own form keys, so a package-wide sweep of accessor names would
	// return the whole panel's vocabulary and assert nothing. What makes this safe to
	// leave narrow is that it is NOT the net for md. 10 — the PATH pin
	// (TestPlaqueEncodeGrant_IsBuiltOnlyFromTheResolvedAdminSession) is, and that one is
	// package-scoped.
	readers := map[string]bool{"PostFormValue": true, "FormValue": true, "Get": true}

	got := map[string]bool{}
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || !readers[sel.Sel.Name] || len(call.Args) != 1 {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				// Silence is the one answer a derivation may never give.
				t.Fatalf("%s reads a form key from a non-literal expression (%s); this scan "+
					"cannot evaluate it and skipping it silently is how a new key slips past",
					sel.Sel.Name, exprText(t, fset, call.Args[0]))
			}
			got[strings.Trim(lit.Value, `"`)] = true
			return true
		})
	}
	want := map[string]bool{"session": true, "rapdu": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("plaqueencode.go reads form keys %v, want %v. A tenant key here would be "+
			"ADR 0017 §6 md. 10 renamed rather than closed", keysOf(got), keysOf(want))
	}
}

// --- (D) ADR 0017 §6 md. 14 — what may leave the process --------------------------

// TestEncodeReply_HasExactlyTheFourFieldsTheRelayNeeds is the STRUCTURAL half of
// md. 14's open item.
//
// 🔴 IT PINS THE TWO STRUCTS AND NOTHING ELSE, WHICH IS LESS THAN IT ONCE CLAIMED.
// This comment used to say the question "what may a body contain" had been changed into
// one with a bounded answer, on the strength of these two types. Two audits proved
// otherwise — a writer taking `body any`, then a sealed interface beaten by embedding —
// so what this test does is narrower and is stated as such: it pins THESE TWO TYPES.
// Four scalar fields on one, a single string on the other, no []byte, map, interface or
// nested struct in either. Whether anything ELSE can reach the wire is a different
// assertion and lives in TestPlaqueEncode_NothingOnThisSurfaceSerialisesACallerSuppliedValue.
func TestEncodeReply_HasExactlyTheFourFieldsTheRelayNeeds(t *testing.T) {
	cases := []struct {
		name  string
		typ   reflect.Type
		want  []string
		kinds []reflect.Kind
	}{
		{
			name:  "encodeReply",
			typ:   reflect.TypeOf(encodeReply{}),
			want:  []string{"Session", "Command", "Step", "Done"},
			kinds: []reflect.Kind{reflect.String, reflect.String, reflect.String, reflect.Bool},
		},
		{
			name:  "encodeFault",
			typ:   reflect.TypeOf(encodeFault{}),
			want:  []string{"Fault"},
			kinds: []reflect.Kind{reflect.String},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var names []string
			for i := 0; i < tc.typ.NumField(); i++ {
				f := tc.typ.Field(i)
				names = append(names, f.Name)
				if i < len(tc.kinds) && f.Type.Kind() != tc.kinds[i] {
					t.Fatalf("%s.%s is a %s; every field of a response body must be a scalar, "+
						"so that 'what may a body contain' is a question about a TYPE rather "+
						"than about care", tc.name, f.Name, f.Type.Kind())
				}
				switch f.Type.Kind() {
				case reflect.Slice, reflect.Map, reflect.Interface, reflect.Struct,
					reflect.Ptr, reflect.Array, reflect.Chan, reflect.Func:
					t.Fatalf("%s.%s is a %s — a shape that can carry bytes. ADR 0017 §2.1 "+
						"and §4.7: nothing but the sealed C-APDU leaves this process",
						tc.name, f.Name, f.Type.Kind())
				}
			}
			if !reflect.DeepEqual(names, tc.want) {
				t.Fatalf("%s has fields %v, want %v", tc.name, names, tc.want)
			}
		})
	}
}

// TestPlaqueEncode_EveryReplyCallSitePassesTheDriversOwnValues pins the four argument
// expressions at every writeEncodeReply call site, package-wide.
//
// The shape gate says a caller cannot hand over a VALUE; this says what the caller may
// put in the four scalars it can hand over. Both are needed and neither substitutes:
// without this, `writeEncodeReply(w, 200, secret, nil, "abort", false)` is a perfectly
// well-shaped call.
//
// ⚠️ IT PINS EXPRESSIONS, NOT MEANINGS. A future call site with a legitimately
// different source will fail this and want a human — which is the intent. What it
// cannot do is judge whether `p.Command` itself is safe; that is
// internal/encode's TestRelay_NoPlaintextKeyMaterialEverReachesTheWire.
func TestPlaqueEncode_EveryReplyCallSitePassesTheDriversOwnValues(t *testing.T) {
	fset, files := parsePackageSource(t, ".")

	// The permitted argument tuples, written out. The abort path has no round to report
	// on, so it passes empty values and a literal step name.
	allowed := map[string]bool{
		`string(id) | p.Command | p.Step | p.Done`: true,
		`"" | nil | "abort" | false`:               true,
	}
	sites := 0
	for name, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || exprText(t, fset, call.Fun) != "writeEncodeReply" {
				return true
			}
			sites++
			if len(call.Args) != 6 {
				t.Errorf("%s: writeEncodeReply called with %d arguments, want 6", name, len(call.Args))
				return true
			}
			tuple := exprText(t, fset, call.Args[2]) + " | " + exprText(t, fset, call.Args[3]) +
				" | " + exprText(t, fset, call.Args[4]) + " | " + exprText(t, fset, call.Args[5])
			if !allowed[tuple] {
				t.Errorf("%s: writeEncodeReply is given (%s), which is not one of the permitted "+
					"tuples %v. The four scalars are the only thing a caller controls on this "+
					"surface, so what goes into them is pinned rather than reviewed", name, tuple,
					keysOf(allowed))
			}
			return true
		})
	}
	if sites < 4 {
		t.Fatalf("found %d writeEncodeReply call site(s); the scan is not reading the package", sites)
	}
	t.Logf("%d reply call sites, all passing the driver's own values", sites)
}

// TestPlaqueEncode_NothingOnThisSurfaceSerialisesACallerSuppliedValue is the SHAPE
// gate, third design, and it is PACKAGE-SCOPED.
//
// 🔴 THE TWO DESIGNS IT REPLACES BOTH DIED THE SAME DEATH, AND THE SECOND ONE IS THE
// INSTRUCTIVE ONE. Design 1: `writeEncodeJSON(w, status, body any)` — an anonymous
// struct with a fifth field went out and the whole package stayed green. Design 2: a
// sealed `encodeBody` interface — the design-1 mutation became a compile error, and a
// different auditor beat it in one line:
//
//	struct{ encodeReply; Leak string }{body, "AUDITPROBE-…"}
//
// EMBEDDING promotes isEncodeBody, so the type satisfied the interface without
// declaring anything; encoding/json flattens embedded structs, so the leak went on the
// wire while all five gates passed. Type identity in Go is COMPOSABLE, so "which type
// may be serialised" is a question with no bottom.
//
// 🔴 SO THE QUESTION IS REMOVED RATHER THAN ANSWERED AGAIN: no function on this
// surface takes a body. This test is what keeps it that way. It requires that every
// argument to json.NewEncoder(...).Encode(...) anywhere on the encode surface is a
// COMPOSITE LITERAL OF ONE OF THE TWO DECLARED TYPES, written in place — never an
// identifier, never a parameter, never a conversion.
//
// ⚠️ WHAT IT IS AND IS NOT: this is a SHAPE gate. What may go INSIDE the four scalars
// is a CONTENT question and is answered elsewhere — `fault` by the vocabulary test
// below, and `command` by internal/encode's relay-exposure test
// (TestRelay_NoPlaintextKeyMaterialEverReachesTheWire). Describing a shape gate as if it settled content is how the last two rounds
// went wrong, so the two are named separately and neither is claimed to cover the
// other.
func TestPlaqueEncode_NothingOnThisSurfaceSerialisesACallerSuppliedValue(t *testing.T) {
	fset, files := parsePackageSource(t, ".")

	declared := map[string]bool{"encodeReply": true, "encodeFault": true}
	encodes := 0
	for name, f := range files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			if !encodeSurfaceFunc(t, fset, fd) {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Encode" || len(call.Args) != 1 {
					return true
				}
				encodes++
				lit, ok := call.Args[0].(*ast.CompositeLit)
				if !ok {
					t.Errorf("%s: %s serialises %s — an expression rather than a literal built "+
						"in place. A value that arrives from somewhere can be embedded, wrapped "+
						"or substituted; that is exactly how the sealed-interface design fell",
						name, fd.Name.Name, exprText(t, fset, call.Args[0]))
					return true
				}
				id, ok := lit.Type.(*ast.Ident)
				if !ok || !declared[id.Name] {
					t.Errorf("%s: %s serialises a literal of type %s, want encodeReply or "+
						"encodeFault", name, fd.Name.Name, exprText(t, fset, lit.Type))
				}
				return true
			})
		}
	}
	if encodes != 2 {
		t.Fatalf("found %d JSON Encode call(s) on the encode surface, want 2 (one per reply "+
			"function). Fewer means the scan has gone blind; more means a third place composes "+
			"a body", encodes)
	}
	t.Logf("encode-surface JSON writes: %d, both composite literals of the two declared types", encodes)
}

// encodeSurfaceFunc reports whether fd belongs to the plaque encode HTTP surface.
//
// 🔴 IT IS DERIVED, NOT LISTED, AND THAT IS THE ANSWER TO A COUNTED LIMIT AN AUDIT
// NAMED. Until this round the writer gates read ONE FILE (plaqueencode.go), so a
// fourth encode handler written into a NEW FILE of this package would have been
// invisible to all of them — the claim "what may a body contain" was true of a file
// and written as if it were true of the surface. The surface is now defined by what a
// function TOUCHES: the encoder port, the grant, the reply writers, or one of the three
// route constants. A new file that drives the relay necessarily mentions one of them.
//
// ⚠️ THE RESIDUAL, NAMED: a function that reaches the relay without naming any of
// these — through a field alias, a wrapper struct, an interface of its own — is not
// seen. Nothing does; and the honest form of this net is "every function that TOUCHES
// THESE NAMES", not "every function that could possibly write an encode response".
func encodeSurfaceFunc(t *testing.T, fset *token.FileSet, fd *ast.FuncDecl) bool {
	t.Helper()
	markers := map[string]bool{
		"a.encoder":             true,
		"plaqueEncodeGrantOf":   true,
		"writeEncodeReply":      true,
		"writeEncodeFault":      true,
		"writeEncodeStoreFault": true,
		"encodeResponseHeaders": true,
		"plaqueEncodeHref":      true,
		"plaqueEncodeStepHref":  true,
		"plaqueEncodeAbortHref": true,
	}
	found := false
	ast.Inspect(fd, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.Ident:
			if markers[v.Name] {
				found = true
			}
		case *ast.SelectorExpr:
			if markers[exprText(t, fset, v)] {
				found = true
			}
		}
		return !found
	})
	return found
}

// TestPlaqueEncode_NoHandlerTouchesTheResponseWriterExceptThroughOneWriter asks about
// REACHING a writer rather than about the shapes a leak might take.
//
// 🔴 THE FORM OF THE CLAIM IS THE POINT. In net/http a body can only be produced
// through the http.ResponseWriter, so instead of enumerating the ways to write one
// (w.Write, fmt.Fprintf, an encoder, a template, http.Error, a redirect …) this walk
// enumerates every FUNCTION IN THIS FILE THAT HOLDS ONE and requires that the value
// only ever be HANDED ON, to a callee in a list whose size is itself ratcheted. Exactly
// one function may do anything else with it, and it is named here.
//
// ⚠️ AND IT IS NOT THE LOAD-BEARING LOCK — WHICH IS A CORRECTION EARNED TWICE OVER.
// This test permits a writer to be HANDED ON, so it can never be stronger than the
// signature it is handed to: while that signature took `body any`, and again while it
// took a sealed interface, this walk saw nothing wrong because forwarding is exactly
// what it allows. The stronger lock is that no signature takes a caller-composed struct
// (TestPlaqueEncode_NothingOnThisSurfaceSerialisesACallerSuppliedValue). This one stops
// a handler from reaching the writer by some other route.
//
// 🔴 PACKAGE-SCOPED SINCE THE THIRD ROUND. It read ONE FILE, so a fourth encode
// handler in a new file of this package was outside it — a limit an audit named. It now
// walks every function encodeSurfaceFunc identifies.
func TestPlaqueEncode_NoHandlerTouchesTheResponseWriterExceptThroughOneWriter(t *testing.T) {
	fset, files := parsePackageSource(t, ".")

	// The sinks a writer may be handed to. EVERY ENTRY MUST BE EARNED — the assertion
	// at the end reports an allowance nothing uses, so an unused entry cannot sit here
	// widening the net for a future edit.
	sinks := map[string]bool{
		"writeEncodeReply":      true, // the one success body; EXEMPT, it writes
		"writeEncodeFault":      true, // the one failure body; EXEMPT, it writes
		"writeEncodeStoreFault": true, // the closed error mapping; calls writeEncodeFault
		"encodeResponseHeaders": true, // the shared header block; EXEMPT, it writes
		"http.MaxBytesReader":   true, // bounds the request body; writes nothing
		"next.ServeHTTP":        true, // the middleware hands the request on
	}
	used := map[string]bool{}

	// 🔴 THE EXEMPT FUNCTIONS ARE NAMED AND THERE ARE EXACTLY THREE. Everything else
	// may only HAND the writer on; these three may call methods on it. They went from
	// one to three when writeEncodeJSON was deleted: the two reply functions now own
	// their own literal and share only a header block, which is the change that removed
	// the body parameter an auditor drove a leak through.
	exempt := map[string]bool{
		"writeEncodeReply":      true,
		"writeEncodeFault":      true,
		"encodeResponseHeaders": true,
	}
	inspected := 0
	var offences []string

	// 🔴 FUNCTION LITERALS ARE WALKED TOO, AND THE FIRST VERSION OF THIS SCAN DID NOT
	// — WHICH MADE IT BLIND TO THE MIDDLEWARE. encodeGate's writer is a parameter of an
	// inner `func(w http.ResponseWriter, r *http.Request)`, not of the FuncDecl, so a
	// walk over declarations alone examined THREE functions and reported success while
	// never looking at the one that bounds the body. Measured: the allow-list entries
	// for http.MaxBytesReader and next.ServeHTTP came back "never used", which is what
	// exposed it. A scan whose blind spot is a middleware is a scan with a hole exactly
	// where a request first arrives.
	inspect := func(label string, body *ast.BlockStmt, names map[string]bool) {
		if len(names) == 0 || body == nil {
			return
		}
		inspected++
		// 🔴 AN EXEMPT FUNCTION IS STILL WALKED FOR THE `used` BOOKKEEPING, AND SKIPPING
		// IT WAS A REAL BUG IN THIS SCAN (found by its own assertion, third round). The
		// two reply functions are the only callers of encodeResponseHeaders, so
		// returning early left that allowance looking unused — and the "every allowance
		// must be earned" check, which exists to stop a dead entry widening the net,
		// fired on a live one. What exemption means is "may do more than hand it on",
		// not "is not read".
		isExempt := exempt[label]
		seen := map[string]bool{}
		ast.Inspect(body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			callee := exprText(t, fset, call.Fun)
			for _, arg := range call.Args {
				id, ok := arg.(*ast.Ident)
				if !ok || !names[id.Name] {
					continue
				}
				if sinks[callee] {
					used[callee] = true
				} else if !isExempt {
					offences = append(offences, label+" hands the writer to "+callee)
				}
				seen[fset.Position(id.Pos()).String()] = true
			}
			return true
		})
		if isExempt {
			return
		}
		// Anything else touching the writer — a selector like w.Write, an assignment,
		// the value being stored or returned — is an offence.
		ast.Inspect(body, func(n ast.Node) bool {
			id, ok := n.(*ast.Ident)
			if !ok || !names[id.Name] {
				return true
			}
			if !seen[fset.Position(id.Pos()).String()] {
				offences = append(offences, label+" uses the writer at "+
					fset.Position(id.Pos()).String()+" other than by handing it to a reply function")
			}
			return true
		})
	}

	for _, f := range files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			// PACKAGE-SCOPED, ENCODE-SURFACE-FILTERED. Every other handler in this
			// package legitimately calls a.render, http.Error and a redirect; the
			// discipline is about the encode surface, and encodeSurfaceFunc is what
			// defines it (derived from what a function touches, not from a list).
			if !encodeSurfaceFunc(t, fset, fd) {
				continue
			}
			inspect(fd.Name.Name, fd.Body, writerParams(t, fset, fd.Type))
			// … and every closure inside it, under the enclosing function's name.
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.FuncLit)
				if !ok {
					return true
				}
				inspect(fd.Name.Name+"'s closure", lit.Body, writerParams(t, fset, lit.Type))
				return true
			})
		}
	}

	if inspected < 8 {
		t.Fatalf("only %d function(s) holding an http.ResponseWriter were examined; the scan "+
			"has gone blind and every assertion here is vacuous", inspected)
	}
	if len(offences) > 0 {
		sort.Strings(offences)
		t.Fatalf("the response body is supposed to be produced in ONE place. These do "+
			"something else with the writer:\n  %s", strings.Join(offences, "\n  "))
	}
	// Every allowance must be earned.
	for name := range sinks {
		if !used[name] {
			t.Errorf("the sink allow-list carries %q and nothing in plaqueencode.go hands a "+
				"writer to it. An unused allowance is a hole waiting for an edit; delete it",
				name)
		}
	}
}

// TestPlaqueEncode_WritesOnlyDeclaredFaults is the CONTENT gate for one of the four
// scalars: the ONE string a failure body can carry comes from a closed vocabulary, so
// an error's text can never reach a caller.
//
// 🔴 IT BINDS THE VALUES AND NOT ONLY THE COUNT — WHICH IS A REPAIR (audit, third
// round). It required the SET of constants to have seven members and every call site
// to name one; changing an existing constant's VALUE
// (faultServer = "server-error-KSesAuthENC-deadbeef…") was GREEN. The consequence was
// bounded — a compile-time literal cannot carry a run-time secret — but "a closed
// seven-word list" bound the SIZE of the list and not the WORDS in it, and that is not
// what the sentence said. The seven strings are pinned exactly below.
//
// 🔴 AND IT IS PACKAGE-SCOPED. It read one file, so a fault written from a second file
// of this package was outside it.
func TestPlaqueEncode_WritesOnlyDeclaredFaults(t *testing.T) {
	fset, files := parsePackageSource(t, ".")

	// THE WORDS THEMSELVES, pinned. A wire vocabulary is a contract with the relay as
	// well as a §4.7 bound, so changing one is a decision, not an edit.
	wantWords := []string{
		"bad-request", "busy", "encode-unavailable", "refused",
		"server-error", "too-many-rounds", "unknown-session",
	}
	gotWords := append([]string(nil), encodeFaults...)
	sort.Strings(gotWords)
	if !reflect.DeepEqual(gotWords, wantWords) {
		t.Fatalf("the fault vocabulary is %v, want %v.\n"+
			"These strings go on the wire. The list's SIZE being right is not the same as its "+
			"WORDS being right — an audit changed one value and every gate stayed green",
			gotWords, wantWords)
	}

	declared := map[string]bool{}
	for _, v := range encodeFaults {
		declared[v] = true
	}
	if len(declared) != len(encodeFaults) {
		t.Fatalf("encodeFaults has duplicate entries: %v", encodeFaults)
	}

	// Every constant whose name starts with `fault` must be in the list, and vice
	// versa — so adding a word without listing it is red.
	fromSource := map[string]string{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for i, name := range vs.Names {
					if !strings.HasPrefix(name.Name, "fault") {
						continue
					}
					if i >= len(vs.Values) {
						t.Fatalf("%s is declared with no value this scan can read", name.Name)
					}
					lit, ok := vs.Values[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s is not a string literal (%T); this scan cannot evaluate it "+
							"and a silent skip is indistinguishable from finding nothing",
							name.Name, vs.Values[i])
					}
					fromSource[name.Name] = strings.Trim(lit.Value, `"`)
				}
			}
		}
	}
	if len(fromSource) != len(encodeFaults) {
		t.Fatalf("plaqueencode.go declares %d fault constants (%v) but encodeFaults lists %d (%v); "+
			"the list is what a test can assert against and a word missing from it is a word "+
			"nobody checked", len(fromSource), fromSource, len(encodeFaults), encodeFaults)
	}
	for name, value := range fromSource {
		if !declared[value] {
			t.Fatalf("%s = %q is not in encodeFaults", name, value)
		}
	}

	// And every call site names one of the constants — never an expression.
	sites := 0
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if exprText(t, fset, call.Fun) != "writeEncodeFault" || len(call.Args) != 3 {
				return true
			}
			sites++
			id, ok := call.Args[2].(*ast.Ident)
			if !ok {
				t.Errorf("writeEncodeFault at %s is given %s rather than a declared constant; "+
					"an expression here is how an error's text reaches a body",
					fset.Position(call.Pos()), exprText(t, fset, call.Args[2]))
				return true
			}
			if _, ok := fromSource[id.Name]; !ok {
				t.Errorf("writeEncodeFault at %s names %s, which is not a fault constant",
					fset.Position(call.Pos()), id.Name)
			}
			return true
		})
	}
	if sites < 5 {
		t.Fatalf("only %d writeEncodeFault call site(s) were found; the scan is not reading "+
			"the file", sites)
	}
	t.Logf("%d fault constants, %d call sites, all named", len(fromSource), sites)
}

// TestPlaqueEncoder_NamesExactlyTheThreeMethodsTheRelayNeeds is ŞART 2 mechanised.
//
// 🔴 internal/encode's KEYRING IS NOT PROTECTED BY THE STORE'S MUTEX. Its owner is
// whichever goroutine holds s.busy, so a "what is this session holding" surface that
// took st.mu and walked the ring would be a DATA RACE ON LIVE KEY MATERIAL — an audit
// produced one under -race in B2c-2a, and having taken st.mu would make it LOOK safe.
// The rule is that this endpoint never reads a session's contents, and the mechanism
// is that the interface it holds cannot express it. Widening this method set is the
// visible edit that makes somebody think about it.
func TestPlaqueEncoder_NamesExactlyTheThreeMethodsTheRelayNeeds(t *testing.T) {
	typ := reflect.TypeOf((*PlaqueEncoder)(nil)).Elem()
	var names []string
	for i := 0; i < typ.NumMethod(); i++ {
		names = append(names, typ.Method(i).Name)
	}
	sort.Strings(names)
	want := []string{"Abort", "Begin", "Step"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("PlaqueEncoder has methods %v, want %v.\n"+
			"A method that returns anything from INSIDE a live round is a read of state whose "+
			"owner is another goroutine — see this test's own comment", names, want)
	}
	// *encode.Store really satisfies it, so the interface is not a fiction.
	var _ PlaqueEncoder = (*encode.Store)(nil)
}

// --- (C) the budget ---------------------------------------------------------------

// TestEncodeBudget_IsDerivedFromTheRoundAndIsPinned.
//
// The number is the product decision; adminEncodeLimit's comment is the argument for
// it. It is pinned here as a literal — the reason adminratelimit.go pins the panel's
// six — and its DERIVATION is asserted too, so a change to the step table moves it
// rather than breaking it.
func TestEncodeBudget_IsDerivedFromTheRoundAndIsPinned(t *testing.T) {
	if encode.RequestsPerRound() != 11 {
		t.Fatalf("a complete round is %d requests, was 11 when this budget was sized. "+
			"That is not a failure — it means ADR 0017 §5.1's table changed (step 8?) and "+
			"adminEncodeLimit's arithmetic wants re-reading", encode.RequestsPerRound())
	}
	if encodePlaquesPerWindow != 20 {
		t.Fatalf("encodePlaquesPerWindow = %d, want 20. It is DERIVED: adminSessionLimit (300) "+
			"minus ~80 requests of panel headroom, divided by encode.RequestsPerRound() — see its "+
			"comment for why a figure derived from how fast a person handles a plaque was the "+
			"wrong denominator", encodePlaquesPerWindow)
	}
	if adminEncodeLimit != encodePlaquesPerWindow*encode.RequestsPerRound() {
		t.Fatalf("adminEncodeLimit = %d, want %d — it must be DERIVED from the round rather "+
			"than written out, or it is a second representation of the step table",
			adminEncodeLimit, encodePlaquesPerWindow*encode.RequestsPerRound())
	}
	if adminEncodeLimit != 220 {
		t.Fatalf("adminEncodeLimit = %d, want 220", adminEncodeLimit)
	}
	// 🔴 THE PROPERTY THE FIRST VERSION OF THIS BUDGET LOST, AND THE REASON THIS LINE
	// EXISTS. adminSessionLimit is charged by EVERY panel request including these, so a
	// budget at or above it can NEVER fire: measured on the real router, a 550-request
	// encode budget met sessionGate's HTML 429 at request 301 and the encode gate was
	// dead code. A gate that cannot refuse is a number with nothing behind it.
	if adminEncodeLimit >= adminSessionLimit {
		t.Fatalf("adminEncodeLimit (%d) is not below adminSessionLimit (%d); sessionGate would "+
			"refuse first and this gate could never fire", adminEncodeLimit, adminSessionLimit)
	}
	// 🔴 AND THE HEADROOM IS THE NUMBER THAT DECIDES THE BUDGET, SO IT IS ASSERTED
	// RATHER THAN DESCRIBED. The check above admits a headroom of ONE, which would
	// satisfy "the gate can fire" and leave an operator with a panel they cannot use.
	// An audit named exactly that gap: the 300 − 80 arithmetic lived only in a comment.
	// The headroom is DERIVED from the two limits, so this pins the shipped value and
	// the behavioural test below pins that the derivation is what a real session sees.
	// 🔴 79 AND NOT 80, AND THE −1 IS THE GATE ORDER: sessionGate runs before
	// encodeGate, so the request that discovers the encode budget is spent has already
	// been charged to the panel's bucket. Measured, not reasoned — the first version of
	// this constant said 80 and the run below returned 79.
	if encodePanelHeadroom != 79 {
		t.Fatalf("encodePanelHeadroom = %d, want 79 (adminSessionLimit %d − adminEncodeLimit %d "+
			"− 1 for the refused request sessionGate charges first)",
			encodePanelHeadroom, adminSessionLimit, adminEncodeLimit)
	}
	// 🔴 ADR 0017 §6 md. 12's REAL BOUNDS, PINNED — and the numbers moved twice getting
	// here. They were published as 55 and 750 from a hand-written denominator of four;
	// encode.RequestsBeforeTheRowIsWritten() measures FIVE against a real round, so the
	// true figures are 44 and 600. An audit named them as bound to nothing, and deriving
	// them is what exposed the off-by-one.
	if d := encode.RequestsBeforeTheRowIsWritten(); d != 5 {
		t.Fatalf("a row is written after %d requests, was 5 when these bounds were sized. "+
			"That is not a failure — ADR 0017 §5.1's sequence changed and md. 12's arithmetic "+
			"wants re-reading", d)
	}
	if encodeRowsPerWindow != 44 || encodeRowsPerAddress != 600 {
		t.Fatalf("uids squattable per window = %d per session and %d per address, want 44 and "+
			"600 (adminEncodeLimit %d and adminFloodLimit %d over %d requests per row)",
			encodeRowsPerWindow, encodeRowsPerAddress, adminEncodeLimit, adminFloodLimit,
			encode.RequestsBeforeTheRowIsWritten())
	}
	// The gate must TIGHTEN the per-session figure rather than widen it; without this
	// the whole md. 12 argument for a separate bucket is decoration.
	if before := adminSessionLimit / encode.RequestsBeforeTheRowIsWritten(); encodeRowsPerWindow >= before {
		t.Fatalf("the encode gate allows %d squatted rows per session and the panel budget "+
			"alone allowed %d; a gate that does not tighten the number it is justified by is "+
			"not a mitigation", encodeRowsPerWindow, before)
	}
	t.Logf("md. 12 bounds: %d uids per session, %d per address (%d requests per row)",
		encodeRowsPerWindow, encodeRowsPerAddress, encode.RequestsBeforeTheRowIsWritten())

	if encodePanelHeadroom < 50 {
		t.Fatalf("encodePanelHeadroom is %d. The whole reason the encode bucket is SEPARATE "+
			"rather than wider is that a batch must leave a usable panel behind; below about "+
			"fifty requests it stops leaving one, and encodePlaquesPerWindow is the lever",
			encodePanelHeadroom)
	}
	if adminEncodePeriod != 10*time.Minute {
		t.Fatalf("adminEncodePeriod = %v, want 10m — every panel window is the same so an "+
			"operator has one recovery time to learn", adminEncodePeriod)
	}
	// It is its OWN bucket. Sharing the panel's would let an encode batch spend the
	// budget the rest of the panel depends on.
	if adminEncodeLimit == adminSessionLimit {
		t.Fatalf("the encode budget is the panel's session budget; the point of the gate is " +
			"that they are separate")
	}
}

// TestPlaqueEncode_TheBudgetRefusesAndTheRoundDiesOnItsOwn is the DECISION at
// encodeGate, measured.
//
// A round refused mid-flight is NOT aborted by the gate. What makes that safe is not
// a promise — it is encode.Store's two-way sweeper: the abandoned session's plain
// plaque key is wiped when its deadline passes, whether or not anybody comes back.
// This drives a REAL store (with an injected clock, because a 90 s TTL cannot be
// tested by sleeping) through the REAL router and the REAL budget.
func TestPlaqueEncode_TheBudgetRefusesAndTheRoundDiesOnItsOwn(t *testing.T) {
	clock := newEncodeTestClock()
	store, err := encode.NewStore(encode.Config{
		Rows:    &noopEncodeRows{},
		Wrapper: &noopEncodeWrapper{},
		BaseURL: "https://tap.example.test/t",
		Clock:   clock,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})

	b := signedIn(t, encodeRouter(t, signedInAdmins(), store, nil))

	// One real round is opened and left in flight.
	rec := b.do(http.MethodPost, plaqueEncodeHref, url.Values{})
	if rec.Code != http.StatusOK {
		t.Fatalf("Begin answered %d: %q", rec.Code, rec.Body.String())
	}
	first := replyOf(t, rec)
	if first.Session == "" || first.Command == "" {
		t.Fatalf("Begin returned no handle or no command: %+v", first)
	}
	if store.Live() != 1 {
		t.Fatalf("after Begin the store holds %d session(s), want 1", store.Live())
	}

	// Burn the rest of the budget on aborts of a handle that does not exist. They are
	// charged by the gate and do nothing else, which is what isolates the BUDGET from
	// the round.
	bogus := strings.Repeat("f", 32)
	served, refused := 1, 0
	for i := 0; i < adminEncodeLimit+5; i++ {
		rec := b.do(http.MethodPost, plaqueEncodeAbortHref, url.Values{"session": {bogus}})
		switch rec.Code {
		case http.StatusOK:
			served++
		case http.StatusTooManyRequests:
			if refused == 0 {
				if got := faultOf(t, rec); got != faultTooMany {
					t.Fatalf("the first refusal says %q, want %q", got, faultTooMany)
				}
				if served != adminEncodeLimit {
					t.Fatalf("the first 429 arrived after %d served requests, want %d",
						served, adminEncodeLimit)
				}
			}
			refused++
		default:
			t.Fatalf("request %d answered %d: %q", i, rec.Code, rec.Body.String())
		}
	}
	if refused == 0 {
		t.Fatalf("the encode budget never refused anything after %d requests", served+refused)
	}
	t.Logf("%d served, %d refused; the budget is %d requests = %d plaques per window",
		served, refused, adminEncodeLimit, encodePlaquesPerWindow)

	// A step for the LIVE round is refused too — this is the mid-round case the
	// decision is about.
	rec = b.do(http.MethodPost, plaqueEncodeStepHref, url.Values{
		"session": {first.Session}, "rapdu": {"9000"},
	})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("a mid-round step answered %d, want 429", rec.Code)
	}
	// THE DECISION: the refusal did NOT abort the round.
	if n := store.Live(); n != 1 {
		t.Fatalf("after a refused step the store holds %d session(s), want 1. encodeGate is "+
			"specified NOT to abort — it refuses before it can even read the handle", n)
	}

	// AND THE ROUND DIES ON ITS OWN. This is what makes "let it die" safe rather than
	// merely cheap.
	clock.Advance(encode.DefaultTTL + time.Second)
	clock.tick(t)
	if n := store.Live(); n != 0 {
		t.Fatalf("the abandoned round is still live %v after its deadline (%d session(s)). "+
			"ADR 0017 §6 md. 7 rejects 'the process will die eventually'", encode.DefaultTTL, n)
	}
}

// TestPlaqueEncode_ASpentEncodeBudgetLeavesTheOperatorAPANEL is the whole reason the
// bucket is separate rather than wider, measured end to end on the real router.
//
// Without this gate an operator's batch would spend adminSessionLimit and the panel
// would be dead for the rest of the window — they could not even open the plaque list
// to see what they had just encoded. With it, the encode surface refuses first and
// the panel still answers.
func TestPlaqueEncode_ASpentEncodeBudgetLeavesTheOperatorAPANEL(t *testing.T) {
	enc := &fakeEncoder{}
	b := signedIn(t, encodeRouter(t, signedInAdmins(), enc, nil))

	refusedAt := 0
	for i := 1; i <= adminEncodeLimit+2 && refusedAt == 0; i++ {
		rec := b.do(http.MethodPost, plaqueEncodeAbortHref, url.Values{"session": {strings.Repeat("a", 32)}})
		if rec.Code != http.StatusTooManyRequests {
			continue
		}
		refusedAt = i
		// The ENCODE gate refused, not the panel's: it answers JSON with a word.
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("request %d was refused by a gate that answered %q; the encode budget is "+
				"supposed to be the binding one and it answers JSON", i, ct)
		}
		if got := faultOf(t, rec); got != faultTooMany {
			t.Fatalf("the refusal says %q, want %q", got, faultTooMany)
		}
	}
	if refusedAt != adminEncodeLimit+1 {
		t.Fatalf("the encode budget refused at request %d, want %d", refusedAt, adminEncodeLimit+1)
	}

	// 🔴 THE POINT, AND IT IS COUNTED RATHER THAN SAMPLED. An earlier version made ONE
	// GET and concluded "the panel still works" — true, and silent about HOW MUCH panel
	// is left, which is the number encodePanelHeadroom promises. An audit named that:
	// the 300 − 80 arithmetic was bound to nothing. So the panel is driven until IT is
	// refused, and the served count must be exactly the headroom.
	served := 0
	for i := 1; i <= encodePanelHeadroom+5; i++ {
		rec := b.do(http.MethodGet, "/admin", nil)
		if rec.Code == http.StatusOK {
			served++
			continue
		}
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("panel request %d after the batch answered %d, want 200 or 429", i, rec.Code)
		}
		break
	}
	if served != encodePanelHeadroom {
		t.Fatalf("after spending the whole encode budget the panel served %d more requests, "+
			"but encodePanelHeadroom promises %d. The separate bucket exists so an encode "+
			"batch leaves an operator a WORKING panel, and how much of one is the number "+
			"that decides encodePlaquesPerWindow", served, encodePanelHeadroom)
	}
	t.Logf("encode budget refused at request %d (%d plaques); the panel then served %d more "+
		"requests before its own budget (adminSessionLimit=%d) refused — headroom promised %d",
		refusedAt, encodePlaquesPerWindow, served, adminSessionLimit, encodePanelHeadroom)
}

// --- the unconfigured deployment ---------------------------------------------------

// TestPlaqueEncode_WithoutARelayTheRoutesRefuseRatherThanVanish.
//
// TAPPA_BASE_URL defaults to http://localhost:8080 and encode.NewStore refuses a base
// URL that is not https, so a development machine has no encode surface. The routes
// are mounted anyway (dashboard.go: "the routing table is complete or it is not") and
// say so.
func TestPlaqueEncode_WithoutARelayTheRoutesRefuseRatherThanVanish(t *testing.T) {
	b := signedIn(t, encodeRouter(t, signedInAdmins(), nil, nil))
	for _, path := range []string{plaqueEncodeHref, plaqueEncodeStepHref, plaqueEncodeAbortHref} {
		rec := b.do(http.MethodPost, path, url.Values{"session": {strings.Repeat("a", 32)}})
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("POST %s with no relay answered %d, want 503", path, rec.Code)
		}
		if got := faultOf(t, rec); got != faultUnavailable {
			t.Errorf("POST %s said %q, want %q", path, got, faultUnavailable)
		}
	}
}

// --- the relay's own contract -------------------------------------------------------

// TestPlaqueEncode_TheRelayRoundTripsBytesAndNothingElse. The C-APDU comes back as
// hex and the R-APDU goes in as hex, byte for byte.
func TestPlaqueEncode_TheRelayRoundTripsBytesAndNothingElse(t *testing.T) {
	enc := &fakeEncoder{progress: encode.Progress{Command: []byte{0x90, 0x60, 0x00, 0x00, 0x00}, Step: "getversion.1"}}
	b := signedIn(t, encodeRouter(t, signedInAdmins(), enc, nil))

	rec := b.do(http.MethodPost, plaqueEncodeHref, url.Values{})
	begin := replyOf(t, rec)
	if begin.Command != "00a40400" {
		t.Fatalf("the first command is %q, want the ISO SELECT the double returns", begin.Command)
	}

	rec = b.do(http.MethodPost, plaqueEncodeStepHref, url.Values{
		"session": {begin.Session}, "rapdu": {"AF0102039100"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("step answered %d: %q", rec.Code, rec.Body.String())
	}
	steps := enc.stepped()
	if len(steps) != 1 {
		t.Fatalf("the store saw %d step(s), want 1", len(steps))
	}
	want, _ := hex.DecodeString("AF0102039100")
	if !bytes.Equal(steps[0].rapdu, want) {
		t.Fatalf("the relayed R-APDU arrived as %x, want %x", steps[0].rapdu, want)
	}
	if string(steps[0].id) != begin.Session {
		t.Fatalf("the step was driven against handle %q, want %q", steps[0].id, begin.Session)
	}
	if got := replyOf(t, rec).Command; got != "9060000000" {
		t.Fatalf("the next command came back as %q", got)
	}
}

// TestPlaqueEncode_ACompletedRoundIsReportedDoneEvenWhenTheMarkerFails is
// encode.Progress.Done's rule at the HTTP boundary, and it is the most consequential
// branch in the file.
//
// Step returns Done=true WITH an error when the chip completed but the row could not
// be marked. A relay told "not done" re-runs; the re-run dies on a duplicate primary
// key; that reads like stale inventory; the obvious fix is deleting the row; the row
// holds the only stored copy of the plaque key.
func TestPlaqueEncode_ACompletedRoundIsReportedDoneEvenWhenTheMarkerFails(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	enc := &fakeEncoder{
		progress: encode.Progress{Done: true, Step: "changefilesettings"},
		nextErr:  errMarkerFailed,
	}
	b := signedIn(t, encodeRouter(t, signedInAdmins(), enc, log))

	rec := b.do(http.MethodPost, plaqueEncodeStepHref, url.Values{
		"session": {strings.Repeat("a", 32)}, "rapdu": {"9100"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("a completed round with a failed marker answered %d, want 200", rec.Code)
	}
	got := replyOf(t, rec)
	if !got.Done {
		t.Fatalf("done = false for a round that completed on silicon. encode.Progress.Done: "+
			"'READ Done BEFORE READING THE ERROR … the round SUCCEEDED on silicon and must NOT "+
			"be re-run'. body=%q", rec.Body.String())
	}
	if !strings.Contains(buf.String(), "do NOT re-run") {
		t.Fatalf("the failure left no operator-facing log line; the caller is told the round "+
			"succeeded and nobody is told the row is unmarked.\nlog: %q", buf.String())
	}
	// And the error's own text stayed out of the body.
	if strings.Contains(rec.Body.String(), "marker") {
		t.Fatalf("the error's text reached the response body: %q", rec.Body.String())
	}
}

var errMarkerFailed = &encodeTestError{"the row for 04AC7E55000601 could not be marked"}

type encodeTestError struct{ msg string }

func (e *encodeTestError) Error() string { return e.msg }

// TestPlaqueEncode_AStoreErrorNeverReachesTheBody. Every sentinel maps to a word.
func TestPlaqueEncode_AStoreErrorNeverReachesTheBody(t *testing.T) {
	cases := []struct {
		err    error
		status int
		fault  string
	}{
		{encode.ErrUnknownSession, http.StatusNotFound, faultUnknownSession},
		{encode.ErrBusy, http.StatusConflict, faultBusy},
		{encode.ErrPlaqueBusy, http.StatusConflict, faultBusy},
		{encode.ErrTooManySessions, http.StatusConflict, faultBusy},
		{encode.ErrStoreClosed, http.StatusServiceUnavailable, faultUnavailable},
		{&encode.RelayMismatchError{RowUID: "04AAAAAAAAAAAA", ChipUID: "04BBBBBBBBBBBB"},
			http.StatusUnprocessableEntity, faultRefused},
	}
	for _, tc := range cases {
		enc := &fakeEncoder{nextErr: tc.err}
		b := signedIn(t, encodeRouter(t, signedInAdmins(), enc, nil))
		rec := b.do(http.MethodPost, plaqueEncodeHref, url.Values{})
		if rec.Code != tc.status {
			t.Errorf("%v answered %d, want %d", tc.err, rec.Code, tc.status)
		}
		if got := faultOf(t, rec); got != tc.fault {
			t.Errorf("%v said %q, want %q", tc.err, got, tc.fault)
		}
		// The RelayMismatchError case is the one that matters: its message carries two
		// uids and an instruction. Neither may be in the body.
		if strings.Contains(rec.Body.String(), "04AAAAAAAAAAAA") ||
			strings.Contains(rec.Body.String(), "retire the row") {
			t.Errorf("an error's text reached the body: %q", rec.Body.String())
		}
	}
}

// --- helpers -----------------------------------------------------------------------

func parsePackageSource(t *testing.T, dir string) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}
	out := map[string]*ast.File{}
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			out[name] = f
		}
	}
	if len(out) < 10 {
		t.Fatalf("parsed only %d file(s) from %s; the scan is not reading the package", len(out), dir)
	}
	return fset, out
}

func parseOneFile(t *testing.T, name string) (*token.FileSet, map[string]*ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, name, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return fset, map[string]*ast.File{name: f}
}

func exprText(t *testing.T, fset *token.FileSet, e ast.Expr) string {
	t.Helper()
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, fset, e); err != nil {
		t.Fatalf("print expression: %v", err)
	}
	return buf.String()
}

// writerParams returns the parameter names of type http.ResponseWriter, for either a
// declaration or a function literal (both carry an *ast.FuncType).
func writerParams(t *testing.T, fset *token.FileSet, ft *ast.FuncType) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	if ft == nil || ft.Params == nil {
		return out
	}
	for _, field := range ft.Params.List {
		if exprText(t, fset, field.Type) != "http.ResponseWriter" {
			continue
		}
		for _, name := range field.Names {
			if name.Name != "_" {
				out[name.Name] = true
			}
		}
	}
	return out
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// --- doubles for the real store ----------------------------------------------------

// noopEncodeRows is an encode.Rows that touches no database. The budget test drives
// the store's CUSTODY (sessions, deadlines, the sweeper), not its persistence.
type noopEncodeRows struct{}

func (noopEncodeRows) InsertUnassigned(context.Context, uuid.UUID, uuid.UUID, string, []byte, string) error {
	return nil
}
func (noopEncodeRows) MarkEncoded(context.Context, uuid.UUID, uuid.UUID, string, string) error {
	return nil
}

type noopEncodeWrapper struct{}

func (noopEncodeWrapper) WrapKey(_, _ []byte) ([]byte, error) { return make([]byte, 44), nil }

// encodeTestClock is the injected clock. A 90 s TTL cannot be exercised by sleeping.
type encodeTestClock struct {
	mu    sync.Mutex
	now   time.Time
	ticks chan time.Time
}

func newEncodeTestClock() *encodeTestClock {
	return &encodeTestClock{
		now:   time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC),
		ticks: make(chan time.Time),
	}
}

func (c *encodeTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *encodeTestClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *encodeTestClock) NewTicker(time.Duration) (<-chan time.Time, func()) {
	return c.ticks, func() {}
}

// tick fires the sweeper and returns only once the sweep it fired has finished. The
// barrier is the second send: the channel is unbuffered, so the sweeper can only
// receive tick two after it has finished processing tick one.
func (c *encodeTestClock) tick(t *testing.T) {
	t.Helper()
	for i := 0; i < 2; i++ {
		select {
		case c.ticks <- c.Now():
		case <-time.After(5 * time.Second):
			t.Fatalf("the sweeper did not accept tick %d; is its goroutine running?", i+1)
		}
	}
}

// TestEncodeSurface_TheEncoderInterfaceIsNotAWallButWhatItReachesIsACount measures
// the qualifier the blast-radius paragraph carries, instead of asserting it.
//
// 🔴 THE PARAGRAPH IT BELONGS TO HAS BEEN WRONG ONCE ALREADY, in the dangerous
// direction ("no secret is reachable"), so its remaining narrow claims are measured
// rather than reasoned. PlaqueEncoder declares THREE methods, and the honest reading
// is that this bounds what a line here can call only if nobody type-asserts — which
// the language permits, because the field holds a *encode.Store.
//
// ⚠️ AND THE RESULT IS THE REASON THE GAP STAYS COUNTED RATHER THAN GROWING: what
// the assertion reaches is a NUMBER. Live() is the count of live rounds, one
// process-wide integer of the same class as `step`. That is a real widening of "what
// this surface can touch" and it is nothing like a.confirm.key. Both halves are said.
func TestEncodeSurface_TheEncoderInterfaceIsNotAWallButWhatItReachesIsACount(t *testing.T) {
	store, err := encode.NewStore(encode.Config{
		Rows:    &noopEncodeRows{},
		Wrapper: &noopEncodeWrapper{},
		BaseURL: "https://tap.example.test/t",
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	var enc PlaqueEncoder = store

	concrete, ok := enc.(*encode.Store)
	if !ok {
		// Not a failure of the product: it would mean the field stopped holding the
		// concrete type, and the paragraph's qualifier could then be RETRACTED.
		t.Skip("PlaqueEncoder no longer carries a *encode.Store; the counted qualifier can go")
	}
	// The reach is real...
	live := concrete.Live()
	if live != 0 {
		t.Errorf("a fresh store reports %d live rounds, want 0", live)
	}
	// ...and this is the whole of it that this test claims: an int, not a secret.
	if reflect.TypeOf(concrete.Live()).Kind() != reflect.Int {
		t.Errorf("Live() no longer returns an int (%T). The counted qualifier says what is "+
			"reachable through a type assertion is A NUMBER; if that stopped being true the "+
			"blast-radius paragraph is understating the surface again, which is the exact "+
			"failure it was rewritten to correct", concrete.Live())
	}
}
