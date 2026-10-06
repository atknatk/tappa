package operator_test

// op13_test.go -- M10 OP-13 phase B: /operator/tenants/{id}/plaques on the shipped router
// with the fake store (rig_test.go). The header table of its response classes (C94-C103),
// the screen's dictionary (every shape its sentence, an unknown status kept visible), the
// plaque keys' absence (by type and on the rendered page), the banner and the refusals of
// a bad path or method, the first-200-of-N sentence, the contrast of the chips, and the
// read budget the five read routes share (OP-11 phase B's hand-over), with its source pin.
// Against PostgreSQL: op13_db_test.go.

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"go/ast"
	"go/constant"
	"go/types"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/handler/operator"
	"github.com/atknatk/tappa/internal/operatorauth"
	"github.com/atknatk/tappa/web/templates/operatorpages"
)

// seedPlaques puts one tenant's inventory into the fake and returns its id.
func (g *rig) seedPlaques(name string, plaques ...fakePlaque) uuid.UUID {
	g.t.Helper()
	id := uuid.New()
	g.store.mu.Lock()
	defer g.store.mu.Unlock()
	g.store.inventories[id] = fakeInventory{name: name, plaques: plaques}
	return id
}

// plaquePath is the plaque screen of tenant id.
func plaquePath(id string) string { return "/operator/tenants/" + id + "/plaques" }

// plaqueAt builds a plaque; loc nil is "no location". Its added time is stored at UTC+2
// (11:30 there is 09:30 UTC), so a render that skips the conversion to UTC prints 11:30
// (2nd round, B4: until then it was stored in UTC and a missing conversion was invisible).
func plaqueAt(uid, status string, loc *uuid.UUID, locName *string, encoded *time.Time) fakePlaque {
	return fakePlaque{TenantPlaque: db.TenantPlaque{UID: uid, Status: status, LocationID: loc, LocationName: locName,
		EncodedAt: encoded, CreatedAt: time.Date(2026, 7, 1, 11, 30, 0, 0, time.FixedZone("UTC+2", 2*3600))}}
}

func ptr[T any](v T) *T { return &v }

// TestOperatorHeaders_ThePlaqueClassesCarryThePolicy drives the ten response classes of
// the plaque screen (C94-C103) with the 40-class test's hostile drive and checks
// (runHeaderClasses), each held to its classRoutes entry.
//
// PART I -- measured on these ten, at WriteHeader (the recorder's Result().Header): the
// designed status, route, header names and values (designedHeaders: no Location but the
// sign-in's on any of them); no hostile value (hostileValues) in a header value or in the
// body, raw or query-escaped -- every one's last request carries the hostile query
// (hostileDrive adds it on /operator/tenants/{id}/plaques); no script; and the store counts
// below: C98 (a malformed id) and C102 (the read budget) make no TenantPlaques call, C103
// (POST) no store call at all.
//
// PART II -- the list above, on these ten classes.
//
// PART III -- This test measures the ten classes and the raw and query-escaped forms only;
// anything else (examples: a class a later handler adds, a hostile value echoed
// HTML-escaped or base32-encoded) is code review's -- no completeness claim.
func TestOperatorHeaders_ThePlaqueClassesCarryThePolicy(t *testing.T) {
	g := newRig(t)
	tenant := g.seedPlaques("FAKE Header Plaques Ltd", plaqueAt("04A1B2C3D4E5F6", "unassigned", nil, nil, nil))
	ad := &addrs{}
	last := &driveLog{}
	send := func(r req) *httptest.ResponseRecorder {
		if r.remote == "" {
			r.remote = ad.next() + ":1"
		}
		r.host = opHost
		*last = driveLog{r.method, r.path}
		return g.do(hostileDrive(r))
	}
	sfs := map[string]string{"Sec-Fetch-Site": "same-origin"}
	plaques := func(id string, c ...*http.Cookie) req {
		return req{method: http.MethodGet, path: plaquePath(id), cookies: c, header: sfs}
	}
	signIn := func() *http.Cookie {
		t.Helper()
		f := g.active()
		ch := cookie(send(req{method: http.MethodPost, path: "/operator/login", origin: opOrigin,
			form: url.Values{"email": {f.email}, "password": {f.password}}}), operatorauth.ChallengeCookieName)
		if ch == nil {
			t.Fatal("PREMISE: no challenge")
		}
		s := cookie(send(req{method: http.MethodPost, path: "/operator/login/totp", origin: opOrigin,
			form: url.Values{"code": {totpAt(f.key, g.now)}}, cookies: []*http.Cookie{ch}}), operatorauth.SessionCookieName)
		if s == nil {
			t.Fatal("PREMISE: no session")
		}
		return s
	}
	failing := func(err error, r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			rq := r()
			g.store.mu.Lock()
			g.store.fail["TenantPlaques"] = err
			g.store.mu.Unlock()
			defer func() { g.store.mu.Lock(); delete(g.store.fail, "TenantPlaques"); g.store.mu.Unlock() }()
			return send(rq)
		}
	}
	once := func(r func() req) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder { return send(r()) }
	}
	calls := map[string]map[string]int{}
	counted := func(class string, drive func() *httptest.ResponseRecorder) func() *httptest.ResponseRecorder {
		return func() *httptest.ResponseRecorder {
			before, total := g.store.count("TenantPlaques"), g.store.total()
			w := drive()
			calls[class] = map[string]int{"TenantPlaques": g.store.count("TenantPlaques") - before, "all": g.store.total() - total}
			return w
		}
	}
	dead := &http.Cookie{Name: operatorauth.SessionCookieName, Value: strings.Repeat("D", 43)}
	classes := []headerClass{
		{"C94 tenant plaques", 200, once(func() req { return plaques(tenant.String(), signIn()) })},
		{"C95 tenant plaques without a cookie", 303, once(func() req { return plaques(tenant.String()) })},
		{"C96 tenant plaques with a dead cookie", 303, once(func() req { return plaques(tenant.String(), dead) })},
		{"C97 tenant plaques, a same-site read", 303, once(func() req {
			r := plaques(tenant.String(), signIn())
			r.header = map[string]string{"Sec-Fetch-Site": "same-site"}
			return r
		})},
		{"C98 tenant plaques, a malformed id", 404, func() *httptest.ResponseRecorder {
			c := signIn()
			return counted("C98", once(func() req { return plaques(strings.ReplaceAll(tenant.String(), "-", ""), c) }))()
		}},
		{"C99 tenant plaques, an id no tenant has", 404, once(func() req { return plaques(uuid.NewString(), signIn()) })},
		{"C100 tenant plaques, the read fails", 503, failing(errFakeDB, func() req { return plaques(tenant.String(), signIn()) })},
		{"C101 tenant plaques, the read's session is refused", 303,
			failing(db.ErrOperatorRefused, func() req { return plaques(tenant.String(), signIn()) })},
		{"C102 tenant plaques, the read budget refused", 429, func() *httptest.ResponseRecorder {
			c := signIn()
			for i := 0; i < 60; i++ { // readLimit 60
				if w := send(plaques(tenant.String(), c)); w.Code != http.StatusOK {
					t.Fatalf("PREMISE: plaque read %d = %d", i+1, w.Code)
				}
			}
			return counted("C102", once(func() req { return plaques(tenant.String(), c) }))()
		}},
		{"C103 POST on tenant plaques", 405, counted("C103", once(func() req {
			return req{method: http.MethodPost, path: plaquePath(tenant.String()), origin: opOrigin, form: url.Values{}}
		}))},
	}
	if len(classes) != 10 {
		t.Fatalf("PREMISE: %d classes, the comment says 10", len(classes))
	}
	for i, c := range classes {
		if !strings.HasPrefix(c.name, "C"+strconv.Itoa(94+i)+" ") {
			t.Fatalf("PREMISE: class %d is named %q, want C%d", 94+i, c.name, 94+i)
		}
	}
	if scripted := runHeaderClasses(t, classes, last); len(scripted) != 0 {
		t.Errorf("classes %v loaded a script, want none", scripted)
	}
	for class, want := range map[string]map[string]int{
		"C98": {"TenantPlaques": 0}, "C102": {"TenantPlaques": 0}, "C103": {"all": 0},
	} {
		got, ok := calls[class]
		if !ok {
			t.Errorf("PREMISE: %s's store calls were not counted", class)
			continue
		}
		for m, n := range want {
			if got[m] != n {
				t.Errorf("%s: %d %s call(s), want %d", class, got[m], m, n)
			}
		}
	}
}

// retiredSentence is the retired shape's sentence: it holds for a retired plaque with a
// successor and for one without (tags.replaced_by is nullable), so it names neither.
const retiredSentence = "Taken out of service, with or without a replacement; taps on it are rejected."

// plaqueRowRe is one plaque row of the screen.
var plaqueRowRe = regexp.MustCompile(`(?s)<li class="op-row">(.*?)</li>`)

// TestPlaqueScreen_EveryShapeHasItsSentenceAndAnUnknownStatusStaysVisible: the plaque
// screen's dictionary, through the shipped handler (plaques.go's plaqueWords and
// operatorpages' chip).
//
// PART I -- a tenant with eleven plaques, in this order: in stock (encoded), in stock (not
// encoded), a FIFTH status the schema does not have ("quarantined"), an active plaque with
// no location (a combination the schema forbids), on a wall (encoded; its location's name
// marked up), on a wall NEVER ENCODED (incident A-1's shape), retired (replaced, with a
// retirement time), lost, an empty status, "Active" in capitals, and retired at its
// location with NO successor and no retirement time (the schema allows both). The page
// lists eleven
// rows in that order, each opening with its uid in the data face; each row's chip carries
// the label and the tone class written below and its sentence is the one written below --
// the seven shapes each with its own sentence, A-1 under its own name; the four
// unrecognised rows each show their stored status quoted (the fifth status, "", and the
// capitalised one) and are NOT drawn as a neighbouring state; no other row shows a stored
// status. The two retired rows share the label, the tone and the sentence -- which says
// neither "replaced" nor "not replaced" -- and only the replaced one carries "Replaced by"
// and "Retired" facts (2nd round, B7). The facts: an encode time, a retirement time and
// every row's added time stored at UTC+2 printed in UTC (no 11:30 on the page), the
// counter, the successor, the location's name escaped in a bdi, "No location", "No encode
// recorded", an unnamed location by its id. The page's only form and only button are the
// bar's sign-out; it carries no hx- attribute.
//
// PART II -- the list above. PART III -- These eleven plaques and these strings only (no
// completeness claim).
func TestPlaqueScreen_EveryShapeHasItsSentenceAndAnUnknownStatusStaysVisible(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	plus2 := time.FixedZone("x", 2*3600)
	encoded := time.Date(2026, 7, 5, 11, 30, 0, 0, plus2)
	loc, blankLoc := uuid.New(), uuid.New()
	const locName = `Hamrun <b>&</b> "Door"`
	type want struct {
		uid, label, tone, sentence string
		stored                     string // "" = no stored status shown
	}
	wants := []want{
		{"04A1B2C3D4E501", "In stock", "tally--stock", "Not on a wall; encoded and ready to mount.", ""},
		{"04A1B2C3D4E502", "In stock, not encoded", "tally--unencoded",
			"Not on a wall, and it cannot be mounted until its encode is recorded.", ""},
		{"04A1B2C3D4E503", "Unrecognised", "tally--unrecognised",
			"Its stored status and location are not a state this screen knows; the status is shown as stored.", `"quarantined"`},
		{"04A1B2C3D4E504", "Unrecognised", "tally--unrecognised",
			"Its stored status and location are not a state this screen knows; the status is shown as stored.", `"active"`},
		{"04A1B2C3D4E505", "On a wall", "tally--mounted", "In service at its location; its encode was recorded.", ""},
		{"04A1B2C3D4E506", "On a wall, never encoded", "tally--unencoded",
			"Mounted and in service, but no encode was ever recorded for it.", ""},
		{"04A1B2C3D4E507", "Retired", "tally--withdrawn", retiredSentence, ""},
		{"04A1B2C3D4E508", "Lost", "tally--withdrawn", "Reported lost; taps on it are rejected.", ""},
		{"04A1B2C3D4E509", "Unrecognised", "tally--unrecognised",
			"Its stored status and location are not a state this screen knows; the status is shown as stored.", `""`},
		{"04A1B2C3D4E50A", "Unrecognised", "tally--unrecognised",
			"Its stored status and location are not a state this screen knows; the status is shown as stored.", `"Active"`},
		{"04A1B2C3D4E50B", "Retired", "tally--withdrawn", retiredSentence, ""},
	}
	retired := plaqueAt(wants[6].uid, "retired", &loc, ptr(locName), nil)
	retired.RetiredAt, retired.ReplacedBy, retired.LastCtr = ptr(encoded), ptr(wants[4].uid), 41
	onWall := plaqueAt(wants[4].uid, "active", &loc, ptr(locName), &encoded)
	onWall.LastCtr = 34349
	tenant := g.seedPlaques("FAKE Shapes Ltd",
		plaqueAt(wants[0].uid, "unassigned", nil, nil, &encoded),
		plaqueAt(wants[1].uid, "unassigned", nil, nil, nil),
		plaqueAt(wants[2].uid, "quarantined", nil, nil, &encoded),
		plaqueAt(wants[3].uid, "active", nil, nil, &encoded),
		onWall,
		plaqueAt(wants[5].uid, "active", &blankLoc, ptr("\u200b "), nil),
		retired,
		plaqueAt(wants[7].uid, "lost", nil, nil, &encoded),
		plaqueAt(wants[8].uid, "", &loc, ptr(locName), &encoded),
		plaqueAt(wants[9].uid, "Active", &loc, ptr(locName), &encoded),
		plaqueAt(wants[10].uid, "retired", &loc, ptr(locName), &encoded),
	)
	w := g.get(plaquePath(tenant.String()), c)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("the plaque screen = %d", w.Code)
	}
	rows := plaqueRowRe.FindAllStringSubmatch(body, -1)
	if len(rows) != len(wants) {
		t.Fatalf("the screen lists %d row(s), want %d", len(rows), len(wants))
	}
	labels := map[string]bool{}
	for i, wt := range wants {
		r := rows[i][1]
		if !strings.HasPrefix(strings.TrimSpace(r), `<span class="font-mono font-bold">`+wt.uid+`</span>`) {
			t.Errorf("row %d does not open with plaque %s in the data face", i+1, wt.uid)
		}
		if !strings.Contains(r, `<span class="tally `+wt.tone+`">`+html.EscapeString(wt.label)+`</span>`) {
			t.Errorf("row %d (%s): no %s chip saying %q", i+1, wt.uid, wt.tone, wt.label)
		}
		if !strings.Contains(r, `<span class="w-full text-sm">`+html.EscapeString(wt.sentence)+`</span>`) {
			t.Errorf("row %d (%s): the sentence is not %q", i+1, wt.uid, wt.sentence)
		}
		gotStored := strings.Contains(r, "Stored status")
		if wt.stored == "" && gotStored {
			t.Errorf("row %d (%s) shows a stored status, want none", i+1, wt.uid)
		}
		if wt.stored != "" && !strings.Contains(r, `Stored status <span class="font-mono">`+html.EscapeString(wt.stored)+`</span>`) {
			t.Errorf("row %d (%s) does not show its stored status %s", i+1, wt.uid, wt.stored)
		}
		labels[wt.label] = true
	}
	if len(labels) != 7 {
		t.Fatalf("PREMISE: the fixture drives %d labels, want the seven shapes'", len(labels))
	}
	for i, fact := range map[int][]string{
		0: {"No location", `Encoded <span class="font-mono">2026-07-05 09:30 UTC</span>`},
		1: {"No encode recorded", `Added <span class="font-mono">2026-07-01 09:30 UTC</span>`},
		4: {`At <bdi>` + html.EscapeString(locName) + `</bdi>`, `Counter <span class="font-mono">34349</span>`},
		5: {`At an unnamed location <span class="break-all font-mono">` + blankLoc.String() + `</span>`, "No encode recorded"},
		6: {`Retired <span class="font-mono">2026-07-05 09:30 UTC</span>`, `Replaced by <span class="font-mono">` + wants[4].uid + `</span>`,
			`Counter <span class="font-mono">41</span>`},
		10: {`At <bdi>` + html.EscapeString(locName) + `</bdi>`, `Added <span class="font-mono">2026-07-01 09:30 UTC</span>`},
	} {
		for _, f := range fact {
			if !strings.Contains(rows[i][1], f) {
				t.Errorf("row %d does not carry %q", i+1, f)
			}
		}
	}
	if strings.Contains(rows[10][1], "Replaced by") || strings.Contains(rows[10][1], "Retired <span") {
		t.Error("the retired plaque with no successor and no retirement time shows a successor or a retirement time")
	}
	if strings.Contains(body, "11:30") || strings.Contains(body, "<b>") {
		t.Error("a time is printed in its stored zone, or a location's markup is written raw")
	}
	if n := strings.Count(body, "<form"); n != 1 || !strings.Contains(body, `<form method="post" action="/operator/logout">`) {
		t.Errorf("the screen has %d form(s); want only the bar's sign-out", n)
	}
	if n := strings.Count(body, "<button"); n != 1 || strings.Contains(body, "hx-") {
		t.Errorf("the screen has %d button(s) or an hx- attribute; want only the sign-out button", n)
	}
}

// TestPlaqueWords_NameEveryShapeAndNothingElse holds plaques.go's dictionary to the values
// db.TenantPlaque.Shape can return, DERIVED: the constants of type db.PlaqueShape in
// internal/db's export data (the build being run), read with go/types -- so an eighth shape
// added to internal/db is red here until the screen has words for it.
//
// PART I -- the derived set is the dictionary's keys; each entry has a label and a
// sentence, no two entries share either; db.PlaqueUnrecognised's tone is
// PlaqueToneUnknown, the zero tone; and a shape the dictionary does not name is said as
// db.PlaqueUnrecognised (plaqueWordFor). CONTROL: the derived set holds the A-1 shape and
// seven members.
//
// PART II -- red on: a constant of the type with no entry; an entry for a value that is no
// constant; an empty or shared label or sentence; a fallback other than the unrecognised
// entry. PART III -- These checks only (no completeness claim).
func TestPlaqueWords_NameEveryShapeAndNothingElse(t *testing.T) {
	tp := typedOperator(t)
	dbPkg := tp.imported(t, "github.com/atknatk/tappa/internal/db")
	shapeType, _ := dbPkg.Scope().Lookup("PlaqueShape").(*types.TypeName)
	if shapeType == nil {
		t.Fatal("PREMISE: internal/db declares no PlaqueShape")
	}
	var derived []string
	for _, name := range dbPkg.Scope().Names() {
		c, ok := dbPkg.Scope().Lookup(name).(*types.Const)
		if !ok || !types.Identical(c.Type(), shapeType.Type()) {
			continue
		}
		derived = append(derived, constant.StringVal(c.Val()))
	}
	slices.Sort(derived)
	if len(derived) != 7 || !slices.Contains(derived, string(db.PlaqueOnAWallNeverEncoded)) {
		t.Fatalf("CONTROL: the derived shapes are %v", derived)
	}
	words := operator.PlaqueWordsForTest()
	var keys []string
	for k := range words {
		keys = append(keys, string(k))
	}
	slices.Sort(keys)
	if !slices.Equal(keys, derived) {
		t.Errorf("the screen's dictionary names %v; db.TenantPlaque.Shape can return %v", keys, derived)
	}
	labels, sentences := map[string]bool{}, map[string]bool{}
	for k, w := range words {
		if w.Label == "" || w.Sentence == "" || labels[w.Label] || sentences[w.Sentence] {
			t.Errorf("%s: an empty or shared label or sentence (%q, %q)", k, w.Label, w.Sentence)
		}
		labels[w.Label], sentences[w.Sentence] = true, true
	}
	if words[db.PlaqueUnrecognised].Tone != operatorpages.PlaqueToneUnknown || operatorpages.PlaqueToneUnknown != 0 {
		t.Error("the unrecognised entry's tone is not the zero tone")
	}
	if got := operator.PlaqueWordForShapeForTest("damaged"); got != words[db.PlaqueUnrecognised] {
		t.Errorf("a shape the dictionary does not name is said as %+v, want the unrecognised entry", got)
	}
}

// keyNeedles are the forms a key value is searched in: its bytes as text, hex in both
// cases, PostgreSQL's bytea text form, and the four base64 alphabets and paddings.
func keyNeedles(b []byte) []string {
	h := hex.EncodeToString(b)
	return []string{string(b), h, strings.ToUpper(h), `\x` + h, base64.StdEncoding.EncodeToString(b),
		base64.RawStdEncoding.EncodeToString(b), base64.URLEncoding.EncodeToString(b), base64.RawURLEncoding.EncodeToString(b)}
}

// keyShaped is a run of 32 or more hex digits -- the text form of an AES-128 key or
// longer.
var keyShaped = regexp.MustCompile(`[0-9A-Fa-f]{32,}`)

// TestPlaqueScreen_NoKeyReachesThePage: CLAUDE.md §4.7 on the screen.
//
// PART I -- BY TYPE: the fields of db.TenantPlaque, db.TenantPlaqueInventory,
// operatorpages.PlaqueRow and operatorpages.TenantPlaquesView are EXACTLY the lists below
// (a field added to any of the four is red here until it is argued for -- the slot a key
// would need). ON THE PAGE: the fake keeps, beside each of three plaques, an aes_key_ref
// and an app_key_ref of key shape (random bytes of 44 and 16) that db.TenantPlaque has no
// field for; the rendered screen carries none of the six in any of keyNeedles' eight forms,
// and no run of 32 or more hex digits at all. CONTROL: the same scan finds a key put in a
// field the page does show (a location's name), in its hex form and as a 32-digit run.
//
// PART II -- the four field lists and the scan above. PART III -- A key copied into a
// field that IS on the list (a location's name, say) is not caught by the type wall; the
// database half -- the definer holds no SELECT on either key column -- is internal/db's
// (ADR 0021, OP-13 note), and the end-to-end half op13_db_test.go's. No completeness claim.
func TestPlaqueScreen_NoKeyReachesThePage(t *testing.T) {
	fields := func(v any) []string {
		var out []string
		rt := reflect.TypeOf(v)
		for i := 0; i < rt.NumField(); i++ {
			out = append(out, rt.Field(i).Name)
		}
		slices.Sort(out)
		return out
	}
	for _, c := range []struct {
		v    any
		want []string
	}{
		{db.TenantPlaque{}, []string{"CreatedAt", "EncodedAt", "LastCtr", "LocationID", "LocationName", "ReplacedBy", "RetiredAt", "Status", "UID"}},
		{db.TenantPlaqueInventory{}, []string{"Plaques", "TenantID", "TenantName", "Total"}},
		{operatorpages.PlaqueRow{}, []string{"CreatedAt", "EncodedAt", "Label", "LastCtr", "Location", "LocationID", "ReplacedBy",
			"RetiredAt", "Sentence", "StoredStatus", "Tone", "UID"}},
		{operatorpages.TenantPlaquesView{}, []string{"ID", "Name", "Noun", "OverviewPath", "Rows", "Shown", "Total", "Truncated"}},
	} {
		if got := fields(c.v); !slices.Equal(got, c.want) {
			t.Errorf("%T's fields are %v, want %v -- a new field is a new slot the page can print", c.v, got, c.want)
		}
	}

	g := newRig(t)
	c := g.signIn(g.active())
	loc := uuid.New()
	var keys [][]byte
	var plaques []fakePlaque
	for i, status := range []string{"active", "unassigned", "retired"} {
		p := plaqueAt(fmt.Sprintf("04A1B2C3D4E5%02X", 0xB0+i), status, nil, nil, nil)
		if status != "unassigned" {
			p.LocationID, p.LocationName = &loc, ptr("FAKE Key Door")
		}
		p.aesKeyRef, p.appKeyRef = randBytes(t, 44), randBytes(t, 16)
		keys = append(keys, p.aesKeyRef, p.appKeyRef)
		plaques = append(plaques, p)
	}
	tenant := g.seedPlaques("FAKE Keys Ltd", plaques...)
	w := g.get(plaquePath(tenant.String()), c)
	body := w.Body.String()
	if w.Code != http.StatusOK || len(plaqueRowRe.FindAllString(body, -1)) != 3 {
		t.Fatalf("PREMISE: the plaque screen = %d with %d row(s)", w.Code, len(plaqueRowRe.FindAllString(body, -1)))
	}
	found := func(page string) int {
		n := 0
		for _, k := range keys {
			for _, needle := range keyNeedles(k) {
				if strings.Contains(page, needle) {
					n++
				}
			}
		}
		return n
	}
	if n := found(body); n != 0 {
		t.Errorf("the page carries a plaque key %d time(s)", n)
	}
	if m := keyShaped.FindAllString(body, -1); len(m) != 0 {
		t.Errorf("the page carries %d run(s) of 32 or more hex digits", len(m))
	}
	// CONTROL: a key in a field the page shows is found by both scans.
	g.store.mu.Lock()
	x := g.store.inventories[tenant]
	x.plaques[0].LocationName = ptr(hex.EncodeToString(keys[0]))
	g.store.inventories[tenant] = x
	g.store.mu.Unlock()
	control := g.get(plaquePath(tenant.String()), c).Body.String()
	if found(control) == 0 || len(keyShaped.FindAllString(control, -1)) == 0 {
		t.Fatal("CONTROL: a key printed in a location's name is not found by the scan")
	}
}

// TestPlaqueScreen_NamesTheTenantAndRefusesABadPath: the banner, the refusals, and the
// path and method escapes of GET /operator/tenants/{id}/plaques.
//
// PART I -- a tenant whose name closes markup and opens a script: 200, the banner carries
// the name escaped in a bdi and the document title is "Plaques — <name> — Taptime
// operator", no script element on the page; the view made ONE store read, TenantPlaques
// for the path's id, and no other (no overview read names the banner); the id in capitals
// reaches the same tenant. The tenant's overview (opened with the id in
// capitals) links the screen by its canonical path as "See this tenant's plaques" (no
// "every plaque": the screen lists 200 at most), and the screen links back to the
// overview. Six tenants whose names show nothing
// (blankNames): the banner says "Unnamed tenant" with the id under it, the title
// "Plaques — Unnamed tenant <id> — Taptime operator". A path id that is not the
// 36-character hyphenated form -- 32 hex digits, braces, urn:uuid:, 35 and 37 characters, a
// non-hex letter, "x" -- is 404 with the not-an-id page and no TenantPlaques call. The
// path with a trailing slash, an extra segment, and the id followed by an escaped slash
// (%2F) and "plaques" are 404 with no TenantPlaques call. HEAD, POST, PUT and DELETE are
// 405 with no store call at all. An id the store holds no tenant for: 404 with the
// no-such-tenant page after one call. The store failing: 503, the log line naming the
// tenant's id and not the session's hash or token. The store refusing the session: the
// sign-in's 303. A store answering for ANOTHER tenant than the path's: 503 and that
// tenant's name on no page.
//
// PART II -- the list above. PART III -- no completeness claim.
func TestPlaqueScreen_NamesTheTenantAndRefusesABadPath(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	const name = `Rusty <Bar> & "Grill" <script>alert(1)</script>`
	id := g.seedPlaques(name, plaqueAt("04A1B2C3D4E5C1", "unassigned", nil, nil, nil))
	asks := func() int { g.store.mu.Lock(); defer g.store.mu.Unlock(); return len(g.store.plaqueAsks) }
	esc := html.EscapeString(name)
	others := func() int {
		return g.store.count("TenantDetail") + g.store.count("TenantList") + g.store.count("LegalVersions")
	}
	otherReads := others()
	w := g.get(plaquePath(id.String()), c)
	body := w.Body.String()
	if others() != otherReads || asks() != 1 {
		t.Errorf("the plaque view made %d other read(s) and %d plaque read(s); want 0 and 1 -- the banner's name is the plaque read's",
			others()-otherReads, asks())
	}
	banner := strings.Index(body, `<section class="op-tenant" aria-label="Tenant">`)
	if w.Code != http.StatusOK || banner < 0 || !strings.Contains(body[banner:], "<bdi>"+esc+"</bdi>") ||
		!strings.Contains(body, "<title>Plaques — "+esc+" — Taptime operator</title>") || strings.Contains(body, "<script") {
		t.Fatalf("the plaque screen = %d; the banner or the title does not carry the escaped name, or a script was written", w.Code)
	}
	g.store.mu.Lock()
	asked := append([]uuid.UUID(nil), g.store.plaqueAsks...)
	g.store.mu.Unlock()
	if len(asked) != 1 || asked[0] != id {
		t.Errorf("the store was asked for %v, want the path's id", asked)
	}
	if w := g.get(plaquePath(strings.ToUpper(id.String())), c); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), esc) {
		t.Errorf("the id in capitals = %d, want the same tenant", w.Code)
	}
	// The way in: the tenant's overview links this screen by its canonical path (the id in
	// lower case), and the screen links back to the overview. The link's words do not
	// promise every plaque: the screen lists db.MaxTenantPlaques at most (2nd round, B6).
	g.seedOverview(db.TenantOverview{ID: id, Name: name, Plan: "founding", BusinessType: "bar"})
	if w := g.get("/operator/tenants/"+strings.ToUpper(id.String()), c); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), `<a href="`+plaquePath(id.String())+`" class="op-link">See this tenant's plaques</a>`) ||
		strings.Contains(w.Body.String(), "every plaque") {
		t.Errorf("the overview = %d, or it does not link %s as \"See this tenant's plaques\"", w.Code, plaquePath(id.String()))
	}
	if !strings.Contains(body, `<a href="/operator/tenants/`+id.String()+`" class="op-link">Back to the overview</a>`) {
		t.Error("the plaque screen does not link back to the tenant's overview")
	}
	for label, blank := range blankNames {
		u := g.seedPlaques(blank)
		body := g.get(plaquePath(u.String()), c).Body.String()
		b := strings.Index(body, `<section class="op-tenant" aria-label="Tenant">`)
		if b < 0 || !strings.Contains(body[b:], `<p class="font-display text-xl font-bold tracking-tight">Unnamed tenant</p>`) ||
			!strings.Contains(body[b:], `<p class="break-all font-mono text-sm">`+u.String()+`</p>`) ||
			!strings.Contains(body, "<title>Plaques — Unnamed tenant "+u.String()+" — Taptime operator</title>") {
			t.Errorf("a tenant named %s: the plaque screen does not name it by its id", label)
		}
	}
	for _, bad := range []string{strings.ReplaceAll(id.String(), "-", ""), "%7B" + id.String() + "%7D", "urn:uuid:" + id.String(),
		id.String()[:35], id.String() + "0", "g" + id.String()[1:], "x"} {
		before := asks()
		w := g.get(plaquePath(bad), c)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "That link does not name a tenant") || asks() != before {
			t.Errorf("the path id %q = %d with %d store call(s), want 404, the not-an-id page and none", bad, w.Code, asks()-before)
		}
	}
	for _, p := range []string{plaquePath(id.String()) + "/", plaquePath(id.String()) + "/x",
		"/operator/tenants/" + id.String() + "%2Fplaques", "/operator/tenants/" + id.String() + "%2fplaques"} {
		before, details := asks(), g.store.count("TenantDetail")
		if w := g.get(p, c); w.Code != http.StatusNotFound || asks() != before || g.store.count("TenantDetail") != details {
			t.Errorf("GET %s = %d with %d plaque and %d overview read(s), want 404 and none", p, w.Code, asks()-before,
				g.store.count("TenantDetail")-details)
		}
	}
	for _, m := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodDelete} {
		before := g.store.total()
		r := req{method: m, host: opHost, path: plaquePath(id.String()), cookies: []*http.Cookie{c}, origin: opOrigin}
		if m != http.MethodHead {
			r.form = url.Values{}
		}
		if w := g.do(r); w.Code != http.StatusMethodNotAllowed || g.store.total() != before {
			t.Errorf("%s on the plaque screen = %d with %d store call(s), want 405 and none", m, w.Code, g.store.total()-before)
		}
	}
	before := asks()
	if w := g.get(plaquePath(uuid.NewString()), c); w.Code != http.StatusNotFound ||
		!strings.Contains(w.Body.String(), "There is no tenant with that id") || asks() != before+1 {
		t.Errorf("an id no tenant has = %d with %d store call(s), want 404, the no-such-tenant page and one", w.Code, asks()-before)
	}
	g.store.mu.Lock()
	g.store.fail["TenantPlaques"] = errFakeDB
	var hashes []string
	for h := range g.store.live {
		hashes = append(hashes, h)
	}
	g.store.mu.Unlock()
	g.logs.Reset()
	if w := g.get(plaquePath(id.String()), c); w.Code != http.StatusServiceUnavailable ||
		!strings.Contains(w.Body.String(), "The plaques could not be loaded") {
		t.Errorf("a failing read = %d", w.Code)
	}
	if !strings.Contains(g.logs.String(), "plaques could not be read") || !strings.Contains(g.logs.String(), id.String()) {
		t.Error("the failing read's log line does not name the tenant")
	}
	for _, h := range hashes {
		if strings.Contains(g.logs.String(), h) || strings.Contains(g.logs.String(), c.Value) {
			t.Error("the failing read's log carries the session's hash or token")
		}
	}
	g.store.mu.Lock()
	g.store.fail["TenantPlaques"] = db.ErrOperatorRefused
	g.store.mu.Unlock()
	if w := g.get(plaquePath(id.String()), c); w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != "/operator/login" {
		t.Errorf("a refused session's plaque screen = %d %q, want the sign-in's 303", w.Code, w.Result().Header.Get("Location"))
	}
	g.store.mu.Lock()
	delete(g.store.fail, "TenantPlaques")
	other := uuid.New()
	g.store.inventories[other] = fakeInventory{name: "FAKE Someone Else Ltd", asTenant: uuid.New(),
		plaques: []fakePlaque{plaqueAt("04A1B2C3D4E5C2", "unassigned", nil, nil, nil)}}
	g.store.mu.Unlock()
	if w := g.get(plaquePath(other.String()), c); w.Code != http.StatusServiceUnavailable ||
		strings.Contains(w.Body.String(), "Someone Else") || strings.Contains(w.Body.String(), "04A1B2C3D4E5C2") {
		t.Errorf("a store answering for another tenant = %d, or its name or plaque is on the page", w.Code)
	}
}

// TestPlaqueScreen_TheFirst200OfNAndAnEmptyTenant: the count sentence (Truncated, Total).
//
// PART I -- 205 plaques: 200 rows, "The first 200 of 205 plaques are listed", the figures
// in the data face, and the 200 rows are the store's first 200 in order; exactly 200: 200
// rows and "200 plaques", no "first"; a store reporting 1 000 for three rows: "The first 3
// of 1000"; one plaque: "1 plaque"; none: "This tenant has no plaques." and no docket, no
// list.
//
// PART II -- the list above. PART III -- no completeness claim.
func TestPlaqueScreen_TheFirst200OfNAndAnEmptyTenant(t *testing.T) {
	g := newRig(t)
	c := g.signIn(g.active())
	many := func(n int) []fakePlaque {
		var out []fakePlaque
		for i := 0; i < n; i++ {
			out = append(out, plaqueAt(fmt.Sprintf("04ABCD%08X", i), "unassigned", nil, nil, nil))
		}
		return out
	}
	page := func(id uuid.UUID) string {
		w := g.get(plaquePath(id.String()), c)
		if w.Code != http.StatusOK {
			t.Fatalf("the plaque screen = %d", w.Code)
		}
		return w.Body.String()
	}
	mono := func(s string) string { return `<span class="font-mono">` + s + `</span>` }
	body := page(g.seedPlaques("FAKE 205 Ltd", many(205)...))
	rows := plaqueRowRe.FindAllStringSubmatch(body, -1)
	if len(rows) != 200 || !strings.Contains(body, "The first "+mono("200")+" of "+mono("205")+" plaques are listed") {
		t.Errorf("205 plaques: %d row(s), or no first-200-of-205 sentence", len(rows))
	} else if !strings.Contains(rows[0][1], fmt.Sprintf("04ABCD%08X", 0)) || !strings.Contains(rows[199][1], fmt.Sprintf("04ABCD%08X", 199)) {
		t.Error("205 plaques: the rows are not the store's first 200 in order")
	}
	body = page(g.seedPlaques("FAKE 200 Ltd", many(200)...))
	if n := len(plaqueRowRe.FindAllString(body, -1)); n != 200 || !strings.Contains(body, mono("200")+" plaques,") || strings.Contains(body, "The first") {
		t.Errorf("200 plaques: %d row(s), or the count sentence is not the whole count", n)
	}
	reported := g.seedPlaques("FAKE Reported Ltd", many(3)...)
	g.store.mu.Lock()
	x := g.store.inventories[reported]
	x.total = 1000
	g.store.inventories[reported] = x
	g.store.mu.Unlock()
	if body := page(reported); !strings.Contains(body, "The first "+mono("3")+" of "+mono("1000")+" plaques are listed") {
		t.Error("a store reporting 1 000 for three rows: no first-3-of-1000 sentence")
	}
	if body := page(g.seedPlaques("FAKE One Ltd", many(1)...)); !strings.Contains(body, mono("1")+" plaque,") {
		t.Error("one plaque: the count sentence is not singular")
	}
	body = page(g.seedPlaques("FAKE None Ltd"))
	if !strings.Contains(body, "This tenant has no plaques.") || strings.Contains(body, `<section class="docket"`) || strings.Contains(body, "<ol") {
		t.Error("a tenant with no plaques: no sentence, or an empty docket")
	}
}

// TestPlaqueScreen_TheChipsAndTextClearAA recomputes, from tailwind.config.js's palette, the
// contrast of the plaque screen's text on its grounds (WCAG 2.1 relative luminance, sRGB;
// a translucent token composited on paper, the docket's and the card's ground): ink on the
// five chip grounds (green-lite, saffron-lite, tomato at 10%, line at 10% and ink at 10%
// over paper), ink and ink at 70% on paper (the rows, the help line), and tappa-green on
// paper (the card's links) -- each against AA's 4.5:1 (the chips' words are 11px, the rest
// 14px and smaller: none is large text).
func TestPlaqueScreen_TheChipsAndTextClearAA(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "tailwind.config.js"))
	if err != nil {
		t.Fatal(err)
	}
	hexOf := func(token string) [3]float64 {
		m := regexp.MustCompile(`'?` + regexp.QuoteMeta(token) + `'?:\s*'#([0-9A-Fa-f]{6})'`).FindSubmatch(b)
		if m == nil {
			t.Fatalf("tailwind.config.js has no %s", token)
		}
		var c [3]float64
		for i := 0; i < 3; i++ {
			v, err := strconv.ParseUint(string(m[1][2*i:2*i+2]), 16, 8)
			if err != nil {
				t.Fatal(err)
			}
			c[i] = float64(v)
		}
		return c
	}
	over := func(fg [3]float64, alpha float64, bg [3]float64) [3]float64 {
		var c [3]float64
		for i := range c {
			c[i] = alpha*fg[i] + (1-alpha)*bg[i]
		}
		return c
	}
	ink, paper, green := hexOf("ink"), hexOf("paper"), hexOf("tappa-green")
	for _, c := range []struct {
		what   string
		fg, bg [3]float64
	}{
		{"ink on green-lite (on a wall)", ink, hexOf("green-lite")},
		{"ink on saffron-lite (no encode recorded)", ink, hexOf("saffron-lite")},
		{"ink on tomato at 10% over paper (retired, lost)", ink, over(hexOf("tomato"), 0.1, paper)},
		{"ink on line at 10% over paper (in stock)", ink, over(hexOf("line"), 0.1, paper)},
		{"ink on ink at 10% over paper (unrecognised)", ink, over(ink, 0.1, paper)},
		{"ink on paper", ink, paper},
		{"ink at 70% on paper", over(ink, 0.7, paper), paper},
		{"tappa-green on paper", green, paper},
	} {
		if got := contrast(c.fg, c.bg); got < 4.5 {
			t.Errorf("%s = %.2f:1, want at least 4.5:1", c.what, got)
		} else {
			t.Logf("%s = %.2f:1", c.what, got)
		}
	}
}

// TestPlaqueScreen_EachChipHasTheRuleTheContrastTestComputes ties the chips the screen
// writes to the grounds TestPlaqueScreen_TheChipsAndTextClearAA computes: for each of the
// five tone classes the plaque screen's chip switch renders (read off a render of every
// tone), web/static/css/input.css has exactly one rule whose selector list names it, and
// that rule's @apply names the ground and the frame of its tone -- the brand's fixed
// mapping (green-lite and tappa-green for a plaque in service, saffron-lite and saffron for
// one with no encode recorded, line at 10% and line for stock, tomato at 10% and tomato for
// retired or lost, ink at 10% and ink for an unrecognised state). It reads input.css, the
// committed source, not the compiled app.css (which CI does not build).
//
// PART II -- red on: a tone class the screen writes with no rule, with two rules, or with a
// rule of another ground or frame. PART III -- A rule written outside the one-line
// `selectors { @apply … }` shape this reads, or a later rule that overrides one, is code
// review's -- no completeness claim.
func TestPlaqueScreen_EachChipHasTheRuleTheContrastTestComputes(t *testing.T) {
	css, err := os.ReadFile(filepath.Join("..", "..", "..", "web", "static", "css", "input.css"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[operatorpages.PlaqueTone][2]string{ // ground, frame
		operatorpages.PlaqueToneInService: {"bg-green-lite", "border-tappa-green"},
		operatorpages.PlaqueToneAttention: {"bg-saffron-lite", "border-saffron"},
		operatorpages.PlaqueToneStock:     {"bg-line/10", "border-line"},
		operatorpages.PlaqueToneOut:       {"bg-tomato/10", "border-tomato"},
		operatorpages.PlaqueToneUnknown:   {"bg-ink/10", "border-ink"},
	}
	chipRe := regexp.MustCompile(`<span class="tally (tally--[a-z-]+)">`)
	// A rule: a selector list (one or more lines of `.name,`) and its one-line @apply.
	ruleRe := regexp.MustCompile(`(?m)((?:^\s*\.[a-z-]+,\s*\n)*^\s*\.[a-z-]+\s*)\{\s*@apply ([^;]+);\s*\}`)
	rules := ruleRe.FindAllStringSubmatch(string(css), -1)
	if len(rules) < 10 {
		t.Fatalf("PREMISE: %d one-line @apply rule(s) read from input.css", len(rules))
	}
	for tone, w := range want {
		var b strings.Builder
		if err := operatorpages.TenantPlaques(operatorpages.TenantPlaquesView{Name: mustName(t, "FAKE Tone Ltd"), Total: "1", Shown: "1",
			Noun: "plaque", OverviewPath: "/operator/tenants/x",
			Rows: []operatorpages.PlaqueRow{{UID: "04A1B2C3D4E5F6", Label: "L", Sentence: "S", Tone: tone}}}).Render(t.Context(), &b); err != nil {
			t.Fatal(err)
		}
		m := chipRe.FindAllStringSubmatch(b.String(), -1)
		if len(m) != 1 {
			t.Fatalf("tone %d: %d chip(s) rendered", tone, len(m))
		}
		class := m[0][1]
		var found []string
		for _, r := range rules {
			for _, sel := range strings.Split(r[1], ",") {
				if strings.TrimSpace(sel) == "."+class {
					found = append(found, r[2])
				}
			}
		}
		if len(found) != 1 {
			t.Errorf("%s: %d rule(s) in input.css, want one", class, len(found))
			continue
		}
		tokens := strings.Fields(found[0])
		if !slices.Contains(tokens, w[0]) || !slices.Contains(tokens, w[1]) {
			t.Errorf("%s's rule applies %q; want the ground %s and the frame %s", class, found[0], w[0], w[1])
		}
	}
}

// mustName is a tenant's name for a render, or the test's end.
func mustName(t *testing.T, s string) operatorpages.TenantName {
	t.Helper()
	n, err := operatorpages.NewTenantName(s)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestReadBudget_TheReadsOfEveryScreenShareOneBudgetPerSession measures surface.go's
// readLimit (60 reads per session per window, OP-13 phase B) across the five read routes,
// and sessionLimit (100 requests) beside it.
//
// PART I -- one session, from a fresh address per request: 12 legal views, 12 list views,
// 12 searches, 12 overviews and 12 plaque views -- 60 x 200; then one more of each of the
// five is 429 with the predicate run (TouchOperatorSession +1) and the store's read not
// (+0 for each method), each of the five the SIGNED-IN form of the page ("Too many
// requests" with the bar's sign-out: problemTooMany(true)), and the window's first refusal
// wrote ONE read-budget WARN record, naming limit=60 and period=10m0s;
// then 35 console views are 200 (65 + 35 = 100 requests) and the 101st request is 429 at
// the gate, the signed-in form too. Refusals that spend no read (surface.go's readLimit
// comment: "a malformed id, a refused term, page or form"): a second session's 5
// malformed-id plaque views, 5 malformed-id overviews, 5 searches with a term over the
// bound, 5 with a page outside 1..1000 (0 and 1001), 5 search bodies over maxFormBytes and
// 5 unreadable search forms (a bad percent escape) are 404, 404, 400, 400, 413 and 400 with
// no store call, and the session still reads 60 times -- all 200 -- before its 61st is 429
// (2nd round, B3: the page and the two form refusals were not driven before). CONTROL: a
// new session of the FIRST session's operator reads -- the budget is keyed on the
// session, not the account.
//
// PART II -- the counts above. PART III -- These sequences only; a read a later screen adds
// is code review's (and its own test's) -- no completeness claim.
func TestReadBudget_TheReadsOfEveryScreenShareOneBudgetPerSession(t *testing.T) {
	g := newRig(t)
	g.seedTenants(3, "FAKE read budget tenant")
	tenant := g.seedOverview(db.TenantOverview{Name: "FAKE Read Budget Ltd", Plan: "founding", BusinessType: "bar"})
	g.store.mu.Lock()
	g.store.inventories[tenant] = fakeInventory{name: "FAKE Read Budget Ltd", plaques: []fakePlaque{plaqueAt("04A1B2C3D4E5D1", "unassigned", nil, nil, nil)}}
	g.store.mu.Unlock()
	n := 0
	send := func(r req) *httptest.ResponseRecorder {
		n++
		r.host = opHost
		r.remote = fmt.Sprintf("198.18.%d.%d:1", n/200, n%200+1)
		if r.method == http.MethodGet {
			r.header = map[string]string{"Sec-Fetch-Site": "same-origin"}
		}
		return g.do(r)
	}
	reads := []struct {
		name, store string
		req         func(*http.Cookie) req
	}{
		{"a legal view", "LegalVersions", func(c *http.Cookie) req {
			return req{method: http.MethodGet, path: "/operator/legal", cookies: []*http.Cookie{c}}
		}},
		{"a list view", "TenantList", func(c *http.Cookie) req {
			return req{method: http.MethodGet, path: "/operator/tenants", cookies: []*http.Cookie{c}}
		}},
		{"a search", "TenantList", func(c *http.Cookie) req {
			return req{method: http.MethodPost, path: "/operator/tenants", form: searchForm("FAKE", ""), origin: opOrigin, cookies: []*http.Cookie{c}}
		}},
		{"an overview", "TenantDetail", func(c *http.Cookie) req {
			return req{method: http.MethodGet, path: "/operator/tenants/" + tenant.String(), cookies: []*http.Cookie{c}}
		}},
		{"a plaque view", "TenantPlaques", func(c *http.Cookie) req {
			return req{method: http.MethodGet, path: plaquePath(tenant.String()), cookies: []*http.Cookie{c}}
		}},
	}
	f := g.active()
	a := g.signIn(f)
	for _, rd := range reads {
		for i := 0; i < 12; i++ {
			if w := send(rd.req(a)); w.Code != http.StatusOK {
				t.Fatalf("%s %d of one session = %d, want 200", rd.name, i+1, w.Code)
			}
		}
	}
	g.logs.Reset()
	for _, rd := range reads {
		touches, stored := g.store.count("TouchOperatorSession"), g.store.count(rd.store)
		if w := send(rd.req(a)); w.Code != http.StatusTooManyRequests || !signedInTooMany(w.Body.String()) {
			t.Errorf("%s after 60 reads = %d, want 429 with the signed-in page (the bar's sign-out)", rd.name, w.Code)
		}
		if g.store.count("TouchOperatorSession") != touches+1 || g.store.count(rd.store) != stored {
			t.Errorf("%s refused by the read budget: predicate +%d, %s +%d; want +1, +0", rd.name,
				g.store.count("TouchOperatorSession")-touches, rd.store, g.store.count(rd.store)-stored)
		}
	}
	if n := strings.Count(g.logs.String(), "operator read budget reached"); n != 2 { // the text and the JSON handler: one record
		t.Errorf("the read budget's refusals wrote %d WARN line(s) across the two log formats, want one record (2 lines)", n)
	}
	// The record names the budget it applied: 60 per 10 minutes (the window is not otherwise
	// measured here -- the tests do not wait one out).
	if !strings.Contains(g.logs.String(), "level=WARN msg=\"operator read budget reached\"") ||
		!strings.Contains(g.logs.String(), "limit=60 period=10m0s") {
		t.Error("the read budget's WARN record does not name limit=60 period=10m0s")
	}
	for i := 0; i < 35; i++ {
		if w := send(req{method: http.MethodGet, path: "/operator", cookies: []*http.Cookie{a}}); w.Code != http.StatusOK {
			t.Fatalf("console view %d after 65 requests = %d, want 200", i+1, w.Code)
		}
	}
	if w := send(req{method: http.MethodGet, path: "/operator", cookies: []*http.Cookie{a}}); w.Code != http.StatusTooManyRequests ||
		!signedInTooMany(w.Body.String()) {
		t.Errorf("the 101st request = %d, want 429 at the gate with the signed-in page", w.Code)
	}

	b := g.signIn(g.active())
	readCalls := func() int {
		return g.store.count("TenantPlaques") + g.store.count("TenantDetail") + g.store.count("TenantList") + g.store.count("LegalVersions")
	}
	// sendRaw posts a search body as written (a bad escape cannot be made with url.Values).
	sendRaw := func(body string, c *http.Cookie) *httptest.ResponseRecorder {
		n++
		r := httptest.NewRequest(http.MethodPost, "http://"+opHost+"/operator/tenants", strings.NewReader(body))
		r.Host = opHost
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", opOrigin)
		r.AddCookie(c)
		r.RemoteAddr = fmt.Sprintf("198.18.%d.%d:1", n/200, n%200+1)
		w := httptest.NewRecorder()
		g.h.ServeHTTP(w, r)
		return w
	}
	refusals := []struct {
		name string
		want int
		send func(i int) *httptest.ResponseRecorder
	}{
		{"a malformed-id plaque view", http.StatusNotFound, func(int) *httptest.ResponseRecorder {
			return send(req{method: http.MethodGet, path: plaquePath(strings.ReplaceAll(tenant.String(), "-", "")), cookies: []*http.Cookie{b}})
		}},
		{"a malformed-id overview", http.StatusNotFound, func(int) *httptest.ResponseRecorder {
			return send(req{method: http.MethodGet, path: "/operator/tenants/" + strings.ReplaceAll(tenant.String(), "-", ""), cookies: []*http.Cookie{b}})
		}},
		{"a search term over the bound", http.StatusBadRequest, func(int) *httptest.ResponseRecorder {
			return send(req{method: http.MethodPost, path: "/operator/tenants", form: searchForm(strings.Repeat("ż", 255), ""), origin: opOrigin,
				cookies: []*http.Cookie{b}})
		}},
		{"a search page outside 1..1000", http.StatusBadRequest, func(i int) *httptest.ResponseRecorder {
			page := "0"
			if i%2 == 1 {
				page = strconv.Itoa(operator.MaxTenantPageForTest + 1)
			}
			return send(req{method: http.MethodPost, path: "/operator/tenants", form: searchForm("FAKE", page), origin: opOrigin,
				cookies: []*http.Cookie{b}})
		}},
		{"a search body over maxFormBytes", http.StatusRequestEntityTooLarge, func(int) *httptest.ResponseRecorder {
			return sendRaw("q="+strings.Repeat("x", 16<<10), b)
		}},
		{"an unreadable search form", http.StatusBadRequest, func(int) *httptest.ResponseRecorder {
			return sendRaw("q=%zz", b)
		}},
	}
	before := readCalls()
	for i := 0; i < 5; i++ {
		for _, rf := range refusals {
			if w := rf.send(i); w.Code != rf.want {
				t.Fatalf("%s = %d, want %d", rf.name, w.Code, rf.want)
			}
		}
	}
	if n := readCalls() - before; n != 0 {
		t.Fatalf("PREMISE: the 30 refusals made %d store read(s), want 0", n)
	}
	plaqueReads := g.store.count("TenantPlaques")
	for i := 0; i < 60; i++ {
		if w := send(reads[4].req(b)); w.Code != http.StatusOK {
			t.Fatalf("plaque view %d after 30 refusals = %d, want 200 -- a refusal spent a read", i+1, w.Code)
		}
	}
	if g.store.count("TenantPlaques") != plaqueReads+60 {
		t.Fatalf("PREMISE: %d plaque read(s) for 60 views", g.store.count("TenantPlaques")-plaqueReads)
	}
	if w := send(reads[4].req(b)); w.Code != http.StatusTooManyRequests {
		t.Errorf("the 61st plaque view after 30 refusals = %d, want 429", w.Code)
	}
	// The budget is the SESSION's: a new session of the operator whose first session spent
	// its reads (and its requests) above reads at once.
	if w := send(reads[4].req(g.signIn(f))); w.Code != http.StatusOK {
		t.Errorf("CONTROL: a new session of the same operator = %d on the plaque view, want 200", w.Code)
	}
}

// TestReadBudget_ConcurrentReadsOfOneSessionStopAtTheLimit: 100 plaque views of ONE session
// sent at once, each from its own address (so the flood gate is not what refuses): exactly
// 60 are 200 and reach the store, 40 are 429 and do not. Run under -race in the
// repository's chain. This is the BEHAVIOUR side and it is probabilistic: a budget that
// asks before it charges (Limiter.Allowed, then Charge) passes here unless the race fires
// -- measured on the third eye's X01 (2nd round): red in 8 of 40 runs under -race, 1 of 40
// without (the third eye's own count: 10 and 1).
// What holds it every time is the source pin below
// (TestReadBudget_NoOperatorSourceAsksTheLimiterBeforeCharging): the count the refusal is
// decided on is the one Charge returns, one locked increment, with no read before it.
func TestReadBudget_ConcurrentReadsOfOneSessionStopAtTheLimit(t *testing.T) {
	g := newRig(t)
	tenant := g.seedPlaques("FAKE Concurrent Ltd", plaqueAt("04A1B2C3D4E5E1", "unassigned", nil, nil, nil))
	c := g.signIn(g.active())
	var wg sync.WaitGroup
	codes := make([]int, 100)
	start := make(chan struct{})
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			codes[i] = g.do(req{method: http.MethodGet, host: opHost, path: plaquePath(tenant.String()), cookies: []*http.Cookie{c},
				remote: fmt.Sprintf("198.18.3.%d:1", i+1), header: map[string]string{"Sec-Fetch-Site": "same-origin"}}).Code
		}(i)
	}
	close(start)
	wg.Wait()
	ok, refused := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusTooManyRequests:
			refused++
		}
	}
	if ok != 60 || refused != 40 || g.store.count("TenantPlaques") != 60 {
		t.Errorf("100 concurrent reads: %d x 200, %d x 429, %d store read(s); want 60, 40, 60", ok, refused, g.store.count("TenantPlaques"))
	}
}

// signedInTooMany reports whether body is the budgets' 429 page in its signed-in form:
// "Too many requests" and the bar's sign-out (problemTooMany(true); the signed-out form
// has none, and would leave a signed-in operator without the bar's way out).
func signedInTooMany(body string) bool {
	return strings.Contains(body, "Too many requests") && strings.Contains(body, `<form method="post" action="/operator/logout">`)
}

// TestReadBudget_NoOperatorSourceAsksTheLimiterBeforeCharging is the source side of the
// concurrency test above (2nd round, B2): that test sees a read-then-write budget only
// when the race fires; this pin sees it every time.
//
// PART I -- the shipped package's files (typedOperator: the build's own GoFiles,
// type-checked; a load failure is red, not an empty scan): no selector is named Allowed --
// whatever its receiver, called or not -- and no identifier denotes httpx.Limiter's
// Allowed. CONTROL: the selectors that denote httpx.Limiter's Charge are the four budgets'
// at least (sign-out, origin refusals, sessions, reads), one of them in spendRead -- the
// scan sees the package's selectors.
//
// PART II -- red on: RB1 a selector expression whose name is Allowed (fail-closed by name:
// httpx.Limiter's, a wrapper's, an interface's, a method value or expression); RB2 an
// identifier that denotes httpx.Limiter's Allowed by any other route.
//
// PART III -- This pin catches the list in PART II only; a form not on it (examples: a
// read-then-write built from two Charge calls, or from a count the package keeps itself) is
// code review's -- no completeness claim.
func TestReadBudget_NoOperatorSourceAsksTheLimiterBeforeCharging(t *testing.T) {
	tp := typedOperator(t)
	hx := tp.imported(t, httpxPkgPath)
	allowed := lookupMember(t, hx, "Limiter", "Allowed")
	charge := lookupMember(t, hx, "Limiter", "Charge")
	spendRead := tp.method(t, "Surface", "spendRead")
	var bad []string
	charges, inSpendRead := 0, 0
	for _, f := range tp.files {
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name == "Allowed" {
				bad = append(bad, "RB1 the selector .Allowed at "+tp.where(sel.Pos()))
			}
			if tp.info.Uses[sel.Sel] == charge {
				charges++
				if tp.in(spendRead, sel.Pos()) {
					inSpendRead++
				}
			}
			return true
		})
	}
	for _, id := range tp.usesOf(allowed) {
		bad = append(bad, "RB2 httpx.Limiter's Allowed at "+tp.where(id.Pos()))
	}
	slices.Sort(bad)
	for _, b := range bad {
		t.Error(b)
	}
	if charges < 4 || inSpendRead != 1 {
		t.Fatalf("CONTROL: %d selector(s) denote httpx.Limiter's Charge, %d of them in spendRead; want 4 or more, and 1", charges, inSpendRead)
	}
}
