package api

// directory_db_test.go -- GET /v1/directory (M11 API-8; ADR 0028 §2, §8, §9, §10; ADR 0027 §2; K-1f,
// K-1h, K-21, K-24, K-33; plan §5 API-8's acceptance) over the REAL chain, the real reader and the
// database: the business's own staff through the RLS read, its holding peers' through
// holding_directory (00055).
//
// THE WORLD is newPunchWorld's -- KF and KM peers in H, C and D peers in H2 (K-33's fixture: the
// outsider is a member of ANOTHER holding), each with a company code, a key and an employee whose
// e-mail address carries the world's canary -- plus E, a business in no holding at all.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// dir is one GET of /v1/directory with the raw query.
func (w *punchWorld) dir(raw, query string) *httptest.ResponseRecorder {
	return w.r.do(http.MethodGet, "/v1/directory?"+query, raw, "")
}

// dirPageOf decodes a page, refusing any key the contract does not name.
func dirPageOf(t *testing.T, rec *httptest.ResponseRecorder) directoryPage {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var p directoryPage
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		t.Fatalf("the answer %q is not a page of the directory: %v", rec.Body.String(), err)
	}
	return p
}

// dirWalk reads b's directory from one request without a cursor to hasMore=false, limit at a time,
// with since as updatedSince ("" for none); it checks the walk's own rules on the way (nextCursor
// exactly when hasMore; one asOf on every page) and returns the entries, the asOf and the pages.
func (w *punchWorld) dirWalk(b punchBiz, since string, limit int) ([]directoryEntryBody, time.Time, int) {
	w.r.t.Helper()
	var out []directoryEntryBody
	var asOf time.Time
	cursor := ""
	for pages := 1; pages < 100000; pages++ {
		q := "limit=" + strconv.Itoa(limit)
		if since != "" {
			q += "&updatedSince=" + url.QueryEscape(since)
		}
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		p := dirPageOf(w.r.t, w.dir(b.raw, q))
		if pages == 1 {
			asOf = p.AsOf
		} else if !p.AsOf.Equal(asOf) {
			w.r.t.Fatalf("page %d of a walk carries asOf %v, the first page %v", pages, p.AsOf, asOf)
		}
		if p.AsOf.Location() != time.UTC {
			w.r.t.Fatalf("asOf %v is not UTC", p.AsOf)
		}
		out = append(out, p.Data...)
		if (p.NextCursor != nil) != p.HasMore {
			w.r.t.Fatalf("page %d: nextCursor %v with hasMore %v -- nextCursor is set exactly when hasMore is", pages, p.NextCursor, p.HasMore)
		}
		if !p.HasMore {
			return out, asOf, pages
		}
		cursor = *p.NextCursor
	}
	w.r.t.Fatal("the walk did not end")
	return nil, time.Time{}, 0
}

// staffOf is what the owner (no RLS) reads of tenants' employees, in the directory's order and on
// the wire's shape -- the answer a business's walk must give for them.
func (w *punchWorld) staffOf(tenants ...uuid.UUID) []directoryEntryBody {
	w.r.t.Helper()
	rows, err := w.r.owner.Query(w.ctx, `
		SELECT e.id, t.api_code, e.external_ref, e.employee_code, e.first_name, e.last_name, e.full_name, e.status <> 'deactivated', e.updated_at
		  FROM employees e JOIN tenants t ON t.id = e.tenant_id
		 WHERE e.tenant_id = ANY($1) ORDER BY e.updated_at, e.id`, tenants)
	if err != nil {
		w.r.t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (directoryEntryBody, error) {
		var e directoryEntryBody
		var id uuid.UUID
		err := r.Scan(&id, &e.Company, &e.ExternalRef, &e.EmployeeCode, &e.FirstName, &e.LastName, &e.FullName, &e.Active, &e.UpdatedAt)
		e.ID, e.UpdatedAt = employeeIDPrefix+id.String(), e.UpdatedAt.UTC()
		return e, err
	})
	if err != nil {
		w.r.t.Fatal(err)
	}
	return out
}

// sameEntries compares two lists entry by entry, every field.
func sameEntries(a, b []directoryEntryBody) error {
	if len(a) != len(b) {
		return fmt.Errorf("%d entries, want %d", len(a), len(b))
	}
	for i := range a {
		x, _ := json.Marshal(a[i])
		y, _ := json.Marshal(b[i])
		if !bytes.Equal(x, y) {
			return fmt.Errorf("entry %d is %s, want %s", i, x, y)
		}
	}
	return nil
}

// person gives b one more employee: status, names (first "" = none -- a panel-made person, homed at
// b's venue, named only by its full name) and an e-mail address carrying the world's canary. The full
// name is "<first> <last>", or "Panel <code> <id>" for a panel-made person.
func (w *punchWorld) person(b punchBiz, status, first, last string) uuid.UUID {
	w.r.t.Helper()
	id := uuid.New()
	var ref, f, l, home any
	full := "Panel " + b.code + " " + id.String()[:8]
	if first != "" {
		ref, f, l, full = "dir-"+id.String()[:8], first, last, first+" "+last
	} else {
		home = b.venue
	}
	w.exec(`INSERT INTO employees (id, tenant_id, location_id, external_ref, employee_code, first_name, last_name, full_name, email, status, invited_at)
	        VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now())`,
		id, b.id, home, ref, "DC-"+id.String()[:4], f, l, full, strings.ToLower(w.canary)+"."+id.String()[:8]+"@directory.example.test", status)
	return id
}

// staffShapes gives b the directory's four shapes of person beside its punch employee.
func (w *punchWorld) staffShapes(b punchBiz) {
	w.r.t.Helper()
	w.person(b, "active", "Ada", b.code)
	w.person(b, "deactivated", "Dee", b.code)
	w.person(b, "invited", "Ivo", b.code)
	w.person(b, "active", "", "")
}

// TestDirectory_HoldingTenantsOnlyNoEmailWithActive is plan §5 API-8's core: each business's walk is
// EXACTLY its own staff and its holding peers' -- KF: KF + KM; KM: KM + KF; C (a member of H2): C + D,
// nothing of H's; E (in no holding): E alone -- every entry, field by field, as the owner reads it, in
// (updatedAt, id) order, with the employer's company code and active false for the deactivated (K-1h);
// the panel-made person -- no first or last name -- by its fullName, its own business's and its peer's
// (the user's decision, 2026-10-15). No answer carries an e-mail address, the canary or the words email,
// location or venue (K-1f, K-24), and each entry carries exactly the contract's nine keys.
func TestDirectory_HoldingTenantsOnlyNoEmailWithActive(t *testing.T) {
	w := newPunchWorld(t)
	e := w.biz("EE")
	for _, b := range []punchBiz{w.kf, w.km, w.c, w.d, e} {
		w.staffShapes(b)
	}
	for _, c := range []struct {
		b    punchBiz
		with []uuid.UUID
	}{
		{w.kf, []uuid.UUID{w.kf.id, w.km.id}}, {w.km, []uuid.UUID{w.km.id, w.kf.id}},
		{w.c, []uuid.UUID{w.c.id, w.d.id}}, {w.d, []uuid.UUID{w.d.id, w.c.id}}, {e, []uuid.UUID{e.id}},
	} {
		// One page holds every entry (a dozen at most): the walk is that one request, and its bytes are
		// judged below -- a member's page costs the peer set (T124's member half), so requests are few.
		rec := w.dir(c.b.raw, "limit=1000")
		page := dirPageOf(t, rec)
		if page.HasMore || page.NextCursor != nil {
			t.Fatalf("%s: one page, hasMore %v", c.b.code, page.HasMore)
		}
		got := page.Data
		want := w.staffOf(c.with...)
		if err := sameEntries(got, want); err != nil {
			t.Errorf("%s's directory: %v", c.b.code, err)
		}
		inactive, panelMade := 0, map[string]bool{}
		for _, x := range got {
			if !x.Active {
				inactive++
			}
			// The panel-made person -- no first or last name -- is named by fullName, its own business's
			// and its peer's alike (the user's decision, 2026-10-15).
			if x.FirstName == nil && x.LastName == nil && strings.HasPrefix(x.FullName, "Panel ") && x.Company != nil {
				panelMade[*x.Company] = true
			}
		}
		for _, id := range c.with {
			code := map[uuid.UUID]string{w.kf.id: "KF", w.km.id: "KM", w.c.id: "CC", w.d.id: "DD", e.id: "EE"}[id]
			if !panelMade[code] {
				t.Errorf("%s's directory does not name %s's panel-made person by fullName", c.b.code, code)
			}
		}
		if inactive != len(c.with) {
			t.Errorf("%s's directory has %d inactive entries, want one per business (the deactivated person)", c.b.code, inactive)
		}
		body := strings.ToLower(rec.Body.String())
		for _, never := range []string{strings.ToLower(w.canary), "@", "example.test", "email", "location", "venue"} {
			if strings.Contains(body, never) {
				t.Errorf("%s's directory carries %q", c.b.code, never)
			}
		}
		var raw struct {
			Data []map[string]json.RawMessage `json:"data"`
		}
		var top map[string]json.RawMessage
		if json.Unmarshal(rec.Body.Bytes(), &raw) != nil || json.Unmarshal(rec.Body.Bytes(), &top) != nil {
			t.Fatal("not JSON")
		}
		if !slices.Equal(keysOf(top), []string{"asOf", "data", "hasMore", "nextCursor"}) {
			t.Errorf("the page's keys: %v", keysOf(top))
		}
		for _, x := range raw.Data {
			if k := keysOf(x); !slices.Equal(k, []string{"active", "company", "employeeCode", "externalRef", "firstName", "fullName", "id", "lastName", "updatedAt"}) {
				t.Errorf("an entry's keys: %v", k)
			}
		}
	}
}

// TestDirectory_Paged is plan §2 item 29 and §5 API-8's "sayfalı (≤ 1000)": KM's walk over KM's and
// KF's staff (60 people, the two businesses interleaved in updatedAt) is the same list at limit 3, 7,
// 59, 60, 1000 and the default, and limit 1 is a page of the first one with more to come (a member's
// page costs the peer set -- T124's member half -- so the walks are kept few); a walk is ceil(n/limit) pages (a last page that is exactly full
// already says hasMore false), nextCursor set exactly while hasMore, asOf one value for the walk. limit 0, 1001,
// a letter, a sign and an exponent are 400 naming limit; the query's names are the contract (another
// case, a misspelling, a repeat, a malformed string: 400, nothing echoed); updatedSince is RFC 3339
// (the unencoded '+' says so). The cursor: malformed, another business's, sent with another
// updatedSince or without the one it was issued for, and one carrying an asOf past the database's
// clock are 400 naming the cursor; a well-formed cursor of this business at a position it chose
// answers this business's directory after it, nothing else.
func TestDirectory_Paged(t *testing.T) {
	w := newPunchWorld(t)
	for i := range 29 {
		w.person(w.kf, "active", fmt.Sprintf("K%02d", i), "Paged")
		w.person(w.km, "active", fmt.Sprintf("M%02d", i), "Paged")
	}
	want := w.staffOf(w.km.id, w.kf.id)
	if len(want) != 60 {
		t.Fatalf("PREMISE: %d people", len(want))
	}
	for _, limit := range []int{3, 7, 59, 60, 1000} {
		got, _, pages := w.dirWalk(w.km, "", limit)
		if err := sameEntries(got, want); err != nil {
			t.Errorf("limit %d: %v", limit, err)
		}
		if wantPages := (len(want) + limit - 1) / limit; pages != wantPages {
			t.Errorf("limit %d: %d pages, want %d", limit, pages, wantPages)
		}
	}
	if p := dirPageOf(t, w.dir(w.km.raw, "")); len(p.Data) != 60 || p.HasMore || p.NextCursor != nil {
		t.Errorf("the default limit: %d entries, hasMore %v", len(p.Data), p.HasMore)
	}
	if p := dirPageOf(t, w.dir(w.km.raw, "limit=1")); len(p.Data) != 1 || !p.HasMore || p.NextCursor == nil || sameEntries(p.Data, want[:1]) != nil {
		t.Errorf("limit=1: %d entries, hasMore %v", len(p.Data), p.HasMore)
	}
	for _, bad := range []string{"0", "1001", "abc", "-1", "+5", "", "1e3", "99999999999"} {
		rec := w.dir(w.km.raw, "limit="+url.QueryEscape(bad))
		if pr := problemOf(t, rec); rec.Code != http.StatusBadRequest || pr.Code != CodeInvalidRequest || pr.Details["parameter"] != "limit" {
			t.Errorf("limit=%q: %d %s", bad, rec.Code, rec.Body.String())
		}
	}
	for _, c := range []struct{ query, field, reason string }{
		{"UpdatedSince=2026-10-01T00:00:00Z", "", ""}, {"updatedsince=2026-10-01T00:00:00Z", "", ""}, {"since=2026-10-01T00:00:00Z", "", ""},
		{"scope=employer", "", ""}, {"limit=5&limit=6", "", ""}, {"zzunknownzz=1", "", ""}, {"%zz=1", "", ""}, {"limit=5;cursor=x", "", ""},
		{"updatedSince=2026-10-01T00:00:00Z&updatedSince=2026-10-02T00:00:00Z", "", ""},
		{"updatedSince=2026-10-01", paramUpdatedSince, ""}, {"updatedSince=2026-10-01%2000:00:00Z", paramUpdatedSince, ""},
		{"updatedSince=2026-10-01T00:00:00.0000001Z", paramUpdatedSince, ""}, {"updatedSince=2026-10-01T00:00:00+24:00", paramUpdatedSince, ""},
		{"updatedSince=2026-10-01T00:00:00", paramUpdatedSince, ""}, {"updatedSince=", paramUpdatedSince, ""},
		{"updatedSince=2026-10-01T00:00:00+02:00", paramUpdatedSince, "unencoded_plus"},
	} {
		rec := w.dir(w.km.raw, c.query)
		pr := problemOf(t, rec)
		if rec.Code != http.StatusBadRequest || pr.Code != CodeInvalidRequest {
			t.Errorf("%q: %d %s", c.query, rec.Code, rec.Body.String())
			continue
		}
		if c.field != "" && pr.Details["parameter"] != c.field {
			t.Errorf("%q: details %v, want the parameter %s", c.query, pr.Details, c.field)
		}
		if c.reason != "" && pr.Details["reason"] != c.reason {
			t.Errorf("%q: details %v, want the reason %s", c.query, pr.Details, c.reason)
		}
		for _, echo := range []string{"UpdatedSince", "updatedsince", "since=", "zzunknownzz", "%zz", "2026-10-0"} {
			if strings.Contains(c.query, echo) && strings.Contains(rec.Body.String(), echo) {
				t.Errorf("%q: the refusal repeats %q: %s", c.query, echo, rec.Body.String())
			}
		}
	}
	if p := dirPageOf(t, w.dir(w.km.raw, "updatedSince=2026-10-01T00:00:00%2B02:00")); len(p.Data) != 60 {
		t.Errorf("an encoded '+' offset: %d entries", len(p.Data))
	}

	first := dirPageOf(t, w.dir(w.km.raw, "limit=5"))
	kfFirst := dirPageOf(t, w.dir(w.kf.raw, "limit=5"))
	since := url.QueryEscape("2020-01-01T00:00:00Z")
	sinceFirst := dirPageOf(t, w.dir(w.km.raw, "limit=5&updatedSince="+since))
	if first.NextCursor == nil || kfFirst.NextCursor == nil || sinceFirst.NextCursor == nil {
		t.Fatal("PREMISE: the first pages have a cursor")
	}
	parts := strings.Split(strings.TrimPrefix(*first.NextCursor, directoryCursorPrefix), "_")
	future := directoryCursorPrefix + strconv.FormatInt(time.Now().Add(time.Hour).UnixMicro(), 10) + "_" + parts[1] + "_" + parts[2] + "_" + parts[3]
	for _, c := range []struct{ what, query string }{
		{"KF's cursor in KM's hands", "cursor=" + url.QueryEscape(*kfFirst.NextCursor)},
		{"a walk without updatedSince, continued with one", "cursor=" + url.QueryEscape(*first.NextCursor) + "&updatedSince=" + since},
		{"a walk with updatedSince, continued without it", "cursor=" + url.QueryEscape(*sinceFirst.NextCursor)},
		{"a walk with updatedSince, continued with another", "cursor=" + url.QueryEscape(*sinceFirst.NextCursor) + "&updatedSince=" + url.QueryEscape("2020-01-01T00:00:01Z")},
		{"an asOf past the database's clock", "cursor=" + url.QueryEscape(future)},
	} {
		rec := w.dir(w.km.raw, c.query)
		if pr := problemOf(t, rec); rec.Code != http.StatusBadRequest || pr.Details["parameter"] != "cursor" {
			t.Errorf("%s: %d %s", c.what, rec.Code, rec.Body.String())
		}
	}
	good := *first.NextCursor
	for _, bad := range []string{"", "dir_", "x" + good, strings.ToUpper(good), "cur_1", "hst_1_1_1_0123456789abcdef", strings.TrimPrefix(good, directoryCursorPrefix),
		good + "_", good[:len(good)-1], good[:len(good)-1] + "G", strings.Replace(good, "-", "", 1),
		directoryCursorPrefix + "01_" + parts[1] + "_" + parts[2] + "_" + parts[3],
		directoryCursorPrefix + parts[0] + "_" + parts[1] + "_{" + parts[2] + "}_" + parts[3],
		directoryCursorPrefix + parts[0] + "_" + parts[1] + "_" + strings.ToUpper(parts[2]) + "_" + parts[3],
		directoryCursorPrefix + "-1_" + parts[1] + "_" + parts[2] + "_" + parts[3],
		directoryCursorPrefix + parts[0] + "_99999999999999999999_" + parts[2] + "_" + parts[3]} {
		rec := w.dir(w.km.raw, "cursor="+url.QueryEscape(bad))
		pr := problemOf(t, rec)
		if rec.Code != http.StatusBadRequest || pr.Details["parameter"] != "cursor" {
			t.Errorf("cursor=%q: %d %s", bad, rec.Code, rec.Body.String())
		}
		if len(bad) > 4 && strings.Contains(rec.Body.String(), bad) {
			t.Errorf("cursor=%q: the refusal repeats it", bad)
		}
	}
	// A cursor of KM's own, at a position it chose (a KF person's place in the order): KM's directory
	// after that position -- the rest of the same list.
	at := want[30]
	at30, _ := uuid.Parse(strings.TrimPrefix(at.ID, employeeIDPrefix))
	chosen := formatDirectoryCursor(first.AsOf, tenant.DirectoryPosition{At: at.UpdatedAt, ID: at30}, directoryBinding(w.km.id, nil))
	p := dirPageOf(t, w.dir(w.km.raw, "limit=1000&cursor="+url.QueryEscape(chosen)))
	if err := sameEntries(p.Data, want[31:]); err != nil {
		t.Errorf("a chosen position: %v", err)
	}
}

// TestDirectory_OverlapWindow is K-21 and plan §2 item 19: a walk from updatedSince reads from ten
// minutes before it. After KM's first walk (asOf A):
//   - a person whose updatedAt is A - 9 min 59 s, and one at A - 10 min exactly, are in the walk from
//     updatedSince = A; one at A - 10 min 1 s is not (the owner put them there, the clock trigger off);
//   - a KM person renamed and a KF person deactivated after A are in it, with the new name and active
//     false -- a peer's change too;
//   - a KF person renamed by a transaction that was OPEN when the second walk began and committed
//     after it -- the late commit the window exists for -- is not in the second walk and IS in the
//     third, from updatedSince = the second's asOf, although its updatedAt is before that asOf;
//   - the people already read come again inside the window (overwritten by id, same content).
func TestDirectory_OverlapWindow(t *testing.T) {
	w := newPunchWorld(t)
	for i := range 6 {
		w.person(w.kf, "active", fmt.Sprintf("K%d", i), "Window")
		w.person(w.km, "active", fmt.Sprintf("M%d", i), "Window")
	}
	_, a, _ := w.dirWalk(w.km, "", 1000)
	in1, in2, out := w.person(w.km, "active", "In", "Window"), w.person(w.kf, "active", "Edge", "Window"), w.person(w.km, "active", "Out", "Window")
	for id, at := range map[uuid.UUID]time.Time{in1: a.Add(-10*time.Minute + time.Second), in2: a.Add(-10 * time.Minute), out: a.Add(-10*time.Minute - time.Second)} {
		w.setUpdatedAt(id, at)
	}
	renamed := w.person(w.km, "active", "Before", "Rename")
	w.setUpdatedAt(renamed, a.Add(-time.Hour))
	gone := w.person(w.kf, "active", "Leaving", "Peer")
	w.setUpdatedAt(gone, a.Add(-time.Hour))
	w.exec(`UPDATE employees SET first_name = 'After' WHERE id = $1`, renamed)
	w.exec(`UPDATE employees SET status = 'deactivated', deactivated_at = now() WHERE id = $1`, gone)

	got, b, _ := w.dirWalk(w.km, a.Format(time.RFC3339Nano), 2)
	byID := map[string]directoryEntryBody{}
	for _, e := range got {
		byID[e.ID] = e
	}
	for what, id := range map[string]uuid.UUID{"A - 9 min 59 s": in1, "A - 10 min": in2, "the renamed KM person": renamed, "the deactivated KF person": gone} {
		if _, ok := byID[employeeIDPrefix+id.String()]; !ok {
			t.Errorf("%s is not in the walk from updatedSince = A", what)
		}
	}
	if _, ok := byID[employeeIDPrefix+out.String()]; ok {
		t.Error("A - 10 min 1 s is in the walk from updatedSince = A")
	}
	if e := byID[employeeIDPrefix+renamed.String()]; e.FirstName == nil || *e.FirstName != "After" {
		t.Errorf("the renamed person: %+v", e)
	}
	if e := byID[employeeIDPrefix+gone.String()]; e.Active || e.Company == nil || *e.Company != "KF" {
		t.Errorf("the deactivated peer: %+v", e)
	}
	truth := map[string]directoryEntryBody{}
	for _, e := range w.staffOf(w.km.id, w.kf.id) {
		truth[e.ID] = e
	}
	for id, e := range byID {
		if err := sameEntries([]directoryEntryBody{e}, []directoryEntryBody{truth[id]}); err != nil {
			t.Errorf("%s came again changed: %v", id, err)
		}
	}

	// The late commit: a KF person renamed in a transaction open across the third walk's first page.
	late := w.person(w.kf, "active", "Slow", "Commit")
	w.setUpdatedAt(late, b.Add(-time.Hour))
	tx, err := w.r.owner.Begin(w.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(w.ctx, `UPDATE employees SET first_name = 'Committed' WHERE id = $1`, late); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	third, c, _ := w.dirWalk(w.km, b.Format(time.RFC3339Nano), 1000)
	if err := tx.Commit(w.ctx); err != nil {
		t.Fatal(err)
	}
	for _, e := range third {
		if e.ID == employeeIDPrefix+late.String() && e.FirstName != nil && *e.FirstName == "Committed" {
			t.Fatal("PREMISE: the uncommitted rename was read")
		}
	}
	var updatedAt time.Time
	if err := w.r.owner.QueryRow(w.ctx, `SELECT updated_at FROM employees WHERE id = $1`, late).Scan(&updatedAt); err != nil {
		t.Fatal(err)
	}
	if !updatedAt.Before(c) {
		t.Fatalf("PREMISE: the late rename's updatedAt %v is not before the walk's asOf %v", updatedAt, c)
	}
	fourth, _, _ := w.dirWalk(w.km, c.Format(time.RFC3339Nano), 1000)
	found := false
	for _, e := range fourth {
		found = found || (e.ID == employeeIDPrefix+late.String() && e.FirstName != nil && *e.FirstName == "Committed")
	}
	if !found {
		t.Errorf("the rename committed after the walk began (updatedAt %v, that walk's asOf %v) is not in the next walk", updatedAt, c)
	}
}

// setUpdatedAt puts a person at a chosen place in the order: the owner writes updated_at with the
// triggers off (00037's clock overwrites any value a statement writes).
func (w *punchWorld) setUpdatedAt(id uuid.UUID, at time.Time) {
	w.r.t.Helper()
	tx, err := w.r.owner.Begin(w.ctx)
	if err != nil {
		w.r.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(w.ctx) }()
	for _, s := range []string{`SET LOCAL session_replication_role = replica`, `UPDATE employees SET updated_at = $2 WHERE id = $1`} {
		var args []any
		if strings.HasPrefix(s, "UPDATE") {
			args = []any{id, at}
		}
		if _, err := tx.Exec(w.ctx, s, args...); err != nil {
			w.r.t.Fatal(err)
		}
	}
	if err := tx.Commit(w.ctx); err != nil {
		w.r.t.Fatal(err)
	}
}

// TestDirectory_OwnStaffAlwaysFromTheRLSQuery (ADR 0028 §2: "kendi çalışanları her zaman sıradan,
// RLS'li, açık tenant_id filtreli bir sorgudan"): a business's own people do not depend on its holding.
// KM's walk holds KM's people and KF's; KM leaves H (left_at = now) -- KM's next walk holds exactly
// KM's people, unchanged, and KF's walk exactly KF's. The own half's RLS probe (the shipped statement
// without its tenant predicate reads one business's rows alone) is the tenant package's
// TestEmployeeDirectory_RLSHoldsWithoutTheFilter.
func TestDirectory_OwnStaffAlwaysFromTheRLSQuery(t *testing.T) {
	w := newPunchWorld(t)
	w.staffShapes(w.kf)
	w.staffShapes(w.km)
	before, _, _ := w.dirWalk(w.km, "", 3)
	if err := sameEntries(before, w.staffOf(w.km.id, w.kf.id)); err != nil {
		t.Fatalf("PREMISE: %v", err)
	}
	w.exec(`UPDATE holding_members SET left_at = clock_timestamp() WHERE member_tenant_id = $1 AND left_at IS NULL`, w.km.id)
	for _, b := range []punchBiz{w.km, w.kf} {
		got, _, _ := w.dirWalk(b, "", 3)
		if err := sameEntries(got, w.staffOf(b.id)); err != nil {
			t.Errorf("%s after KM left the holding: %v", b.code, err)
		}
	}
}

// TestDirectory_KeyOfATenantWithoutHoldingSeesOnlyItsOwnStaff (ADR 0028 §2, K-33): E's key -- E in no
// holding -- reads E's people and nobody else's; holding_directory answers E's context no row; and C's
// key (a member of ANOTHER holding) reads C's and D's people, none of H's and none of E's.
func TestDirectory_KeyOfATenantWithoutHoldingSeesOnlyItsOwnStaff(t *testing.T) {
	w := newPunchWorld(t)
	e := w.biz("EE")
	for _, b := range []punchBiz{w.kf, w.km, w.c, e} {
		w.staffShapes(b)
	}
	got, _, _ := w.dirWalk(e, "", 2)
	if err := sameEntries(got, w.staffOf(e.id)); err != nil {
		t.Errorf("E's directory: %v", err)
	}
	var n int
	if err := w.r.d.WithTenant(w.ctx, e.id, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM public.holding_directory(NULL, NULL)`).Scan(&n)
	}); err != nil || n != 0 {
		t.Errorf("holding_directory in E's context: %d rows (%v), want none", n, err)
	}
	cGot, _, _ := w.dirWalk(w.c, "", 1000)
	if err := sameEntries(cGot, w.staffOf(w.c.id, w.d.id)); err != nil {
		t.Errorf("C's directory: %v", err)
	}
}

// TestDirectory_ClosedIs403RevokedIs401SuspendedIsOpen (ADR 0028 §2 K-22, §3, §10): the operator closes
// KM's API -> 403 api_disabled; reopened -> 200; a suspended business still reads its directory (§10:
// /v1/directory is open); a revoked key is 401.
func TestDirectory_ClosedIs403RevokedIs401SuspendedIsOpen(t *testing.T) {
	w := newPunchWorld(t)
	w.exec(`UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, w.km.id)
	if rec := w.dir(w.km.raw, ""); rec.Code != http.StatusForbidden || problemOf(t, rec).Code != CodeAPIDisabled {
		t.Fatalf("a closed API: %d %s", rec.Code, rec.Body.String())
	}
	w.exec(`UPDATE tenants SET api_enabled_at = now() WHERE id = $1`, w.km.id)
	w.r.suspended.suspend(w.km.id)
	if got, _, _ := w.dirWalk(w.km, "", 1000); sameEntries(got, w.staffOf(w.km.id, w.kf.id)) != nil {
		t.Fatalf("suspended: %d entries", len(got))
	}
	if _, err := w.r.keys.Revoke(w.ctx, apikey.RevokeCommand{TenantID: w.km.id, ActorID: w.km.admin, KeyID: w.km.keyID}); err != nil {
		t.Fatal(err)
	}
	if rec := w.dir(w.km.raw, ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked key: %d %s", rec.Code, rec.Body.String())
	}
}
