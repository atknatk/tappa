package api

// punches_db_test.go -- GET /v1/punches (M11 API-7; ADR 0028 §2, §4, §5, §8, §10; K-4, K-13, K-31,
// K-32, K-40; plan §5 API-7's acceptance) over the REAL chain, the real feed reader and the database,
// the events written by the SHIPPED triggers (00049) from rows inserted as tappa_app in the
// employer's context -- the shape the tap path writes.
//
// THE WORLD (newPunchWorld), COMMITTED: businesses KF and KM, peers in holding H; C and D in holding
// H2 (K-33's fixture: the outsider is a member of ANOTHER holding). Each has company code, a venue
// with a ref, an active plaque far ahead of any counter a row names, an employee with a ref, a code
// and an e-mail address, an owner and a key. The holdings are written by the owner and removed at
// the end.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/apikey"
)

type punchBiz struct {
	id, admin, venue, emp uuid.UUID
	code, raw, tag        string
	keyID                 uuid.UUID
	email                 string
}

type punchWorld struct {
	r      *dbRig
	ctx    context.Context
	kf, km punchBiz
	c, d   punchBiz
	h, h2  uuid.UUID
	canary string
}

func newPunchWorld(t *testing.T) *punchWorld {
	t.Helper()
	r := newDBRig(t)
	w := &punchWorld{r: r, ctx: context.Background(), canary: "CANARY" + strings.ToUpper(hex.EncodeToString(randomBytes(t, 6)))}
	w.kf, w.km, w.c, w.d = w.biz("KF"), w.biz("KM"), w.biz("CC"), w.biz("DD")
	w.h, w.h2 = w.holding(), w.holding()
	t.Cleanup(func() {
		bg := context.Background()
		if _, err := r.owner.Exec(bg, `DELETE FROM holding_members WHERE holding_id = ANY($1)`, []uuid.UUID{w.h, w.h2}); err != nil {
			t.Errorf("cleanup memberships: %v", err)
		}
		if _, err := r.owner.Exec(bg, `DELETE FROM holdings WHERE id = ANY($1)`, []uuid.UUID{w.h, w.h2}); err != nil {
			t.Errorf("cleanup holdings: %v", err)
		}
	})
	w.join(w.h, w.kf.id)
	w.join(w.h, w.km.id)
	w.join(w.h2, w.c.id)
	w.join(w.h2, w.d.id)
	return w
}

func randomBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func (w *punchWorld) exec(sql string, args ...any) {
	w.r.t.Helper()
	if _, err := w.r.owner.Exec(w.ctx, sql, args...); err != nil {
		w.r.t.Fatalf("%s: %v", sql, err)
	}
}

// biz is a business with an open API under code, a venue, a plaque, an employee and a key.
func (w *punchWorld) biz(code string) punchBiz {
	w.r.t.Helper()
	b := punchBiz{code: code}
	b.id, b.admin = w.r.business(true)
	w.exec(`UPDATE tenants SET api_code = $2 WHERE id = $1`, b.id, code)
	if err := w.r.owner.QueryRow(w.ctx, `INSERT INTO locations (tenant_id, name, external_ref) VALUES ($1, $2, $3) RETURNING id`,
		b.id, code+" Venue", code+"-LOC").Scan(&b.venue); err != nil {
		w.r.t.Fatal(err)
	}
	b.tag = strings.ToUpper(hex.EncodeToString(randomBytes(w.r.t, 7)))
	w.exec(`INSERT INTO tags (uid, tenant_id, location_id, aes_key_ref, last_ctr, status) VALUES ($1, $2, $3, $4, 1000000, 'active')`,
		b.tag, b.id, b.venue, randomBytes(w.r.t, 44))
	b.emp = uuid.New()
	b.email = strings.ToLower(w.canary) + "." + code + "@punches.example.test"
	w.exec(`INSERT INTO employees (id, tenant_id, external_ref, employee_code, full_name, email, status, invited_at)
	        VALUES ($1, $2, $3, $4, 'Punch Staff', $5, 'active', now())`, b.emp, b.id, code+"-EMP", code+"-C1", b.email)
	b.raw, b.keyID = w.r.key(b.id, b.admin)
	return b
}

func (w *punchWorld) holding() uuid.UUID {
	w.r.t.Helper()
	var id uuid.UUID
	if err := w.r.owner.QueryRow(w.ctx, `INSERT INTO holdings (name) VALUES ($1) RETURNING id`, "api7 "+uuid.NewString()[:8]).Scan(&id); err != nil {
		w.r.t.Fatal(err)
	}
	return id
}

func (w *punchWorld) join(h, tenant uuid.UUID) {
	w.r.t.Helper()
	w.exec(`INSERT INTO holding_members (holding_id, member_tenant_id, joined_at) VALUES ($1, $2, clock_timestamp() - interval '10 minutes')`, h, tenant)
}

// punchRow is one transactions row written as tappa_app in the employer's context. at is how
// long ago the phone declared it.
type punchRow struct {
	employer punchBiz
	site     *punchBiz // a holding punch on the site's plaque; nil: the employer's own plaque
	verdict  string
	channel  string
	kind     string // "" = no direction
	practice bool
	ago      time.Duration
}

// own is an ok tap on b's own plaque.
func own(b punchBiz, channel, kind string) punchRow {
	return punchRow{employer: b, verdict: "ok", channel: channel, kind: kind, ago: time.Minute}
}

// at is an ok tap of employer's employee on site's plaque (a holding punch).
func at(employer, site punchBiz, kind string) punchRow {
	return punchRow{employer: employer, site: &site, verdict: "ok", channel: "nfc", kind: kind, ago: 2 * time.Minute}
}

// insertPunch writes row in tx (the employer's context). The evidence columns carry the world's
// canary, so a leak of any of them into an answer is visible.
func (w *punchWorld) insertPunch(ctx context.Context, tx pgx.Tx, row punchRow) (uuid.UUID, error) {
	var kind any
	if row.kind != "" {
		kind = row.kind
	}
	b := row.employer
	var location, siteTenant, siteLocation, enteredBy *uuid.UUID
	var tag, siteTag *string
	var ctr *int32
	var sun *bool
	switch {
	case row.site != nil && row.channel == "manual":
		siteTenant, siteLocation, enteredBy = &row.site.id, &row.site.venue, &b.admin
	case row.site != nil:
		siteTenant, siteLocation, siteTag = &row.site.id, &row.site.venue, &row.site.tag
		c, s := int32(100), true
		ctr, sun = &c, &s
	case row.channel == "manual":
		location, enteredBy = &b.venue, &b.admin
	default:
		location, tag = &b.venue, &b.tag
	}
	evidence := row.channel != "manual"
	var note, ip, sid, pctx, layer *string
	var lat, lng *float64
	if evidence {
		n, i, s, c, l := w.canary+"-note", "198.51.100.213", "sys:"+strings.ToLower(w.canary), `{"canary":"`+w.canary+`-context"}`, "guardrail"
		la, lo := 35.987654, 14.123456
		note, ip, sid, pctx, layer, lat, lng = &n, &i, &s, &c, &l, &la, &lo
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO transactions (tenant_id, employee_id, location_id, tag_uid, occurred_at, verdict, channel, type, practice,
		                          site_tenant_id, site_location_id, site_tag_uid, ctr, sun_valid, entered_by,
		                          note, source_ip, gps_lat, gps_lng, trust, policy_layer, matched_sid, policy_context)
		VALUES ($1, $2, $3, $4, date_trunc('milliseconds', clock_timestamp()) - $5::interval, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15,
		        $16, $17::inet, $18, $19, 77, $20, $21, $22::jsonb)
		RETURNING id`,
		b.id, b.emp, location, tag, fmt.Sprintf("%d milliseconds", row.ago.Milliseconds()), row.verdict, row.channel, kind, row.practice,
		siteTenant, siteLocation, siteTag, ctr, sun, enteredBy,
		note, ip, lat, lng, layer, sid, pctx).Scan(&id)
	return id, err
}

// write commits row in its own transaction (one counter set per transaction -- API-2 D-1).
func (w *punchWorld) write(row punchRow) uuid.UUID {
	w.r.t.Helper()
	var id uuid.UUID
	if err := w.r.d.WithTenant(w.ctx, row.employer.id, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		id, err = w.insertPunch(ctx, tx, row)
		return err
	}); err != nil {
		w.r.t.Fatalf("write %s/%s/%s: %v", row.channel, row.verdict, row.kind, err)
	}
	return id
}

// review records the employer's owner's decision on a flag.
func (w *punchWorld) review(b punchBiz, id uuid.UUID, outcome string) {
	w.r.t.Helper()
	if err := w.r.d.WithTenant(w.ctx, b.id, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO transaction_reviews (tenant_id, transaction_id, reviewer_id, outcome) VALUES ($1, $2, $3, $4)`,
			b.id, id, b.admin, outcome)
		return err
	}); err != nil {
		w.r.t.Fatalf("review %s: %v", outcome, err)
	}
}

// get is one GET of /v1/punches with the raw query.
func (w *punchWorld) get(raw, query string) *httptest.ResponseRecorder {
	return w.r.do(http.MethodGet, "/v1/punches?"+query, raw, "")
}

// pageOf decodes a page, refusing any key the contract does not name.
func pageOf(t *testing.T, rec *httptest.ResponseRecorder) punchPage {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var p punchPage
	dec := json.NewDecoder(bytes.NewReader(rec.Body.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		t.Fatalf("the answer %q is not a page of punches: %v", rec.Body.String(), err)
	}
	return p
}

// all reads b's scope from the beginning to hasMore=false, limit at a time.
func (w *punchWorld) all(b punchBiz, scope string, limit int) []punchBody {
	w.r.t.Helper()
	var out []punchBody
	cursor := ""
	for range 10000 {
		q := "scope=" + scope + "&limit=" + fmt.Sprint(limit)
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		p := pageOf(w.r.t, w.get(b.raw, q))
		out = append(out, p.Data...)
		cursor = p.NextCursor
		if !p.HasMore {
			return out
		}
	}
	w.r.t.Fatal("the feed did not end")
	return nil
}

// feedRow is a punch_events row as the owner reads it.
type feedRow struct {
	seq                               int64
	punch, employee                   uuid.UUID
	roles, channel, direction, status string
	version                           int32
	punchTime, recordedAt             time.Time
	empRef, empCode, empCompany       *string
	locID                             *uuid.UUID
	locRef, locName, locCompany, tag  *string
}

// feedOf is tenant's feed in seq order, as the owner (no RLS) reads it.
func (w *punchWorld) feedOf(tenant uuid.UUID) []feedRow {
	w.r.t.Helper()
	rows, err := w.r.owner.Query(w.ctx, `
		SELECT seq, punch_id, employee_id, array_to_string(roles, ','), channel, direction, status, version, punch_time, recorded_at,
		       employee_ref, employee_code, employee_company, location_id, location_ref, location_name, location_company, tag_uid
		  FROM punch_events WHERE tenant_id = $1 ORDER BY seq`, tenant)
	if err != nil {
		w.r.t.Fatal(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (feedRow, error) {
		var f feedRow
		err := r.Scan(&f.seq, &f.punch, &f.employee, &f.roles, &f.channel, &f.direction, &f.status, &f.version, &f.punchTime, &f.recordedAt,
			&f.empRef, &f.empCode, &f.empCompany, &f.locID, &f.locRef, &f.locName, &f.locCompany, &f.tag)
		return f, err
	})
	if err != nil {
		w.r.t.Fatal(err)
	}
	return out
}

func ids(ps []punchBody) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

func pch(id uuid.UUID) string { return "pch_" + id.String() }

func sp(s string) *string { return &s }

func eqStr(a, b *string) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// sameAsFeed says whether the answer is exactly the event row (every field, from the owner's read).
func sameAsFeed(p punchBody, f feedRow) error {
	var locID *string
	if f.locID != nil {
		locID = sp("loc_" + f.locID.String())
	}
	var tag *string
	if f.tag != nil {
		tag = sp("tag_" + strings.TrimRight(*f.tag, " "))
	}
	switch {
	case p.ID != pch(f.punch) || p.Version != f.version || p.Status != f.status || p.Channel != f.channel || p.Direction != f.direction:
		return fmt.Errorf("identity %s v%d %s %s %s, want %s v%d %s %s %s", p.ID, p.Version, p.Status, p.Channel, p.Direction,
			pch(f.punch), f.version, f.status, f.channel, f.direction)
	case !p.PunchTime.Equal(f.punchTime) || !p.RecordedAt.Equal(f.recordedAt) || p.PunchTime.Location() != time.UTC:
		return fmt.Errorf("times %v / %v, want %v / %v (UTC)", p.PunchTime, p.RecordedAt, f.punchTime, f.recordedAt)
	case p.Employee.ID != "emp_"+f.employee.String() || !eqStr(p.Employee.ExternalRef, f.empRef) ||
		!eqStr(p.Employee.EmployeeCode, f.empCode) || !eqStr(p.Employee.Company, f.empCompany):
		return fmt.Errorf("employee %+v, want the event's %s", p.Employee, f.employee)
	case !eqStr(p.Location.ID, locID) || !eqStr(p.Location.ExternalRef, f.locRef) || !eqStr(p.Location.Name, f.locName) ||
		!eqStr(p.Location.Company, f.locCompany):
		return fmt.Errorf("location %+v, want the event's", p.Location)
	case !eqStr(p.TagID, tag):
		return fmt.Errorf("tagId %v, want %v", p.TagID, tag)
	}
	return nil
}

// checkScope: b's scope answers exactly want (punch ids, in order), each equal to b's own event row.
func (w *punchWorld) checkScope(b punchBiz, scope string, want []uuid.UUID) {
	w.r.t.Helper()
	got := w.all(b, scope, 1000)
	wantIDs := make([]string, 0, len(want))
	for _, id := range want {
		wantIDs = append(wantIDs, pch(id))
	}
	if !slices.Equal(ids(got), wantIDs) {
		w.r.t.Fatalf("%s scope=%s answered %v, want %v", b.code, scope, ids(got), wantIDs)
	}
	feed := map[string]feedRow{}
	for _, f := range w.feedOf(b.id) {
		feed[pch(f.punch)] = f
	}
	for _, p := range got {
		f, ok := feed[p.ID]
		if !ok {
			w.r.t.Fatalf("%s scope=%s answered %s, which is not in %s's feed", b.code, scope, p.ID, b.code)
		}
		if !strings.Contains(","+f.roles+",", ","+scope+",") {
			w.r.t.Fatalf("%s scope=%s answered %s whose roles are %s", b.code, scope, p.ID, f.roles)
		}
		if err := sameAsFeed(p, f); err != nil {
			w.r.t.Fatalf("%s scope=%s %s: %v", b.code, scope, p.ID, err)
		}
	}
}

// TestPunches_FourShapesTimesTwoScopes is plan §5 API-7's acceptance ("dört şekil × iki kapsam doğru
// tenant'a, anlık görüntü alanlarıyla"; ADR 0028 §2, §4, K-31, K-32, K-40): KF@KF (nfc, in), KM@KM
// (qr, out), KM@KF (a KM employee on KF's plaque, in) and KF@KM (out) -- and each answer is exactly
// the event its own business's feed holds, field by field:
//
//	KF employer  KF@KF, KF@KM       (KF@KM: KF's employee, KM's location -- company KM)
//	KF location  KF@KF, KM@KF       (KM@KF: KM's employee -- company KM -- at KF's location)
//	KM employer  KM@KM, KM@KF
//	KM location  KM@KM, KF@KM
//
// A holding punch's two answers carry the same id (pch_ + the employer's record), the same times,
// the same direction (the employer's -- K-40) and the same snapshot. C and D (holding H2) answer
// none of them.
func TestPunches_FourShapesTimesTwoScopes(t *testing.T) {
	w := newPunchWorld(t)
	kfkf := w.write(own(w.kf, "nfc", "in"))
	kmkm := w.write(own(w.km, "qr", "out"))
	kmkf := w.write(at(w.km, w.kf, "in"))
	kfkm := w.write(at(w.kf, w.km, "out"))

	w.checkScope(w.kf, "employer", []uuid.UUID{kfkf, kfkm})
	w.checkScope(w.kf, "location", []uuid.UUID{kfkf, kmkf})
	w.checkScope(w.km, "employer", []uuid.UUID{kmkm, kmkf})
	w.checkScope(w.km, "location", []uuid.UUID{kmkm, kfkm})
	for _, b := range []punchBiz{w.c, w.d} {
		w.checkScope(b, "employer", nil)
		w.checkScope(b, "location", nil)
	}

	// The shapes' meaning, beyond "equal to the row": who is named where.
	find := func(b punchBiz, scope string, id uuid.UUID) punchBody {
		for _, p := range w.all(b, scope, 1000) {
			if p.ID == pch(id) {
				return p
			}
		}
		t.Fatalf("%s scope=%s has no %s", b.code, scope, pch(id))
		return punchBody{}
	}
	is := func(p punchBody, emp, loc punchBiz, tag, channel, direction string) error {
		switch {
		case p.Employee.ID != "emp_"+emp.emp.String() || !eqStr(p.Employee.ExternalRef, sp(emp.code+"-EMP")) ||
			!eqStr(p.Employee.EmployeeCode, sp(emp.code+"-C1")) || !eqStr(p.Employee.Company, sp(emp.code)):
			return fmt.Errorf("employee %+v, want %s's", p.Employee, emp.code)
		case !eqStr(p.Location.ID, sp("loc_"+loc.venue.String())) || !eqStr(p.Location.ExternalRef, sp(loc.code+"-LOC")) ||
			!eqStr(p.Location.Name, sp(loc.code+" Venue")) || !eqStr(p.Location.Company, sp(loc.code)):
			return fmt.Errorf("location %+v, want %s's", p.Location, loc.code)
		case !eqStr(p.TagID, sp("tag_"+tag)) || p.Channel != channel || p.Direction != direction || p.Version != 1 || p.Status != "valid":
			return fmt.Errorf("tag %v channel %s direction %s v%d %s", p.TagID, p.Channel, p.Direction, p.Version, p.Status)
		}
		return nil
	}
	for _, c := range []struct {
		name      string
		b         punchBiz
		scope     string
		id        uuid.UUID
		emp, loc  punchBiz
		tag, ch   string
		direction string
	}{
		{"KF@KF in KF employer", w.kf, "employer", kfkf, w.kf, w.kf, w.kf.tag, "nfc", "in"},
		{"KF@KF in KF location", w.kf, "location", kfkf, w.kf, w.kf, w.kf.tag, "nfc", "in"},
		{"KM@KM in KM employer", w.km, "employer", kmkm, w.km, w.km, w.km.tag, "qr", "out"},
		{"KM@KM in KM location", w.km, "location", kmkm, w.km, w.km, w.km.tag, "qr", "out"},
		{"KM@KF in KM employer", w.km, "employer", kmkf, w.km, w.kf, w.kf.tag, "nfc", "in"},
		{"KM@KF in KF location", w.kf, "location", kmkf, w.km, w.kf, w.kf.tag, "nfc", "in"},
		{"KF@KM in KF employer", w.kf, "employer", kfkm, w.kf, w.km, w.km.tag, "nfc", "out"},
		{"KF@KM in KM location", w.km, "location", kfkm, w.kf, w.km, w.km.tag, "nfc", "out"},
	} {
		if err := is(find(c.b, c.scope, c.id), c.emp, c.loc, c.tag, c.ch, c.direction); err != nil {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// One holding punch, two answers, one punch.
	a, b := find(w.km, "employer", kmkf), find(w.kf, "location", kmkf)
	if !a.PunchTime.Equal(b.PunchTime) || !a.RecordedAt.Equal(b.RecordedAt) || a.Direction != b.Direction {
		t.Errorf("KM@KF's two answers differ: %+v / %+v", a, b)
	}
	var occurred time.Time
	if err := w.r.owner.QueryRow(w.ctx, `SELECT occurred_at FROM transactions WHERE id = $1`, kmkf).Scan(&occurred); err != nil {
		t.Fatal(err)
	}
	if !a.PunchTime.Equal(occurred) || !a.RecordedAt.After(occurred) {
		t.Errorf("punchTime %v recordedAt %v; want the declared %v and the server's later instant (plan §2 item 5)", a.PunchTime, a.RecordedAt, occurred)
	}
}

// TestPunches_OutsiderSeesNone is plan §5 API-7 ("C hiçbirini görmez"; ADR 0028 §2: "holding dışı
// tenant'ın anahtarı yalnız kendi tenant'ını görür"): C, a member of ANOTHER holding (H2, with D),
// reads in both scopes only its own punch -- none of KF's, KM's or their holding punches, from the
// beginning and from every position KF's feed hands out; and KF's and KM's keys never answer C's
// punch. The read is WithTenant(the key's) + the explicit filter: the key is the only tenant source.
func TestPunches_OutsiderSeesNone(t *testing.T) {
	w := newPunchWorld(t)
	w.write(own(w.kf, "nfc", "in"))
	w.write(at(w.km, w.kf, "in"))
	w.write(at(w.kf, w.km, "out"))
	w.write(own(w.km, "nfc", "in"))
	cc := w.write(own(w.c, "nfc", "in"))
	dd := w.write(own(w.d, "qr", "in"))
	for _, scope := range []string{"employer", "location"} {
		w.checkScope(w.c, scope, []uuid.UUID{cc})
		w.checkScope(w.d, scope, []uuid.UUID{dd})
		for _, b := range []punchBiz{w.kf, w.km} {
			for _, p := range w.all(b, scope, 1) {
				if p.ID == pch(cc) || p.ID == pch(dd) {
					t.Fatalf("%s scope=%s answered H2's punch %s", b.code, scope, p.ID)
				}
			}
		}
		// Every position of KF's feed, in C's hands: C's feed after it, never KF's events.
		for pos := 0; pos <= len(w.feedOf(w.kf.id)); pos++ {
			rec := w.get(w.c.raw, "scope="+scope+"&cursor=cur_"+fmt.Sprint(pos))
			if rec.Code == http.StatusBadRequest {
				continue // a position C's feed has not reached: refused, nothing answered
			}
			for _, p := range pageOf(t, rec).Data {
				if p.ID != pch(cc) {
					t.Fatalf("C with KF's position %d answered %s", pos, p.ID)
				}
			}
		}
	}
}

// TestPunches_NoManualNoRejected is plan §5 API-7 ("manuel ve reddedilmiş yok"; ADR 0028 §4, K-4,
// K-6): of KF's rows only the ok taps and the APPROVED flag answer -- the flag once, at its approval
// (recordedAt the approval's instant, punchTime the tap's), in its place in the feed order; a manual
// record, a practice row (ok, and a flag approved), a reject, an ignored repeat, a rejected flag and
// a flag still waiting never. The same for a holding punch: KM's flag on KF's plaque answers in KM's
// employer scope and KF's location scope once each, at the approval; KM's manual record for KF's
// venue and KM's reject on KF's plaque answer nowhere.
func TestPunches_NoManualNoRejected(t *testing.T) {
	w := newPunchWorld(t)
	first := w.write(own(w.kf, "nfc", "in"))
	w.write(punchRow{employer: w.kf, verdict: "ok", channel: "manual", kind: "out", ago: time.Hour})
	w.write(punchRow{employer: w.kf, verdict: "ok", channel: "nfc", kind: "in", practice: true, ago: time.Minute})
	practiceFlag := w.write(punchRow{employer: w.kf, verdict: "flag", channel: "qr", kind: "in", practice: true, ago: time.Minute})
	w.write(punchRow{employer: w.kf, verdict: "reject", channel: "nfc", ago: time.Minute})
	w.write(punchRow{employer: w.kf, verdict: "ignored", channel: "qr", ago: time.Minute})
	rejected := w.write(punchRow{employer: w.kf, verdict: "flag", channel: "nfc", kind: "out", ago: time.Minute})
	waiting := w.write(punchRow{employer: w.kf, verdict: "flag", channel: "qr", kind: "in", ago: time.Minute})
	approved := w.write(punchRow{employer: w.kf, verdict: "flag", channel: "qr", kind: "out", ago: 3 * time.Minute})
	manualFlag := w.write(punchRow{employer: w.kf, verdict: "flag", channel: "manual", kind: "in", ago: time.Hour})
	holdingFlag := w.write(punchRow{employer: w.km, site: &w.kf, verdict: "flag", channel: "nfc", kind: "in", ago: time.Minute})
	w.write(punchRow{employer: w.km, site: &w.kf, verdict: "ok", channel: "manual", kind: "out", ago: time.Hour})
	w.write(punchRow{employer: w.km, site: &w.kf, verdict: "reject", channel: "nfc", ago: time.Minute})
	second := w.write(own(w.kf, "qr", "out"))

	before := w.all(w.kf, "employer", 1000)
	if !slices.Equal(ids(before), []string{pch(first), pch(second)}) {
		t.Fatalf("before any review KF answers %v, want the two ok taps", ids(before))
	}
	w.review(w.kf, rejected, "rejected")
	w.review(w.kf, practiceFlag, "approved")
	w.review(w.kf, approved, "approved")
	w.review(w.kf, manualFlag, "approved")
	w.review(w.km, holdingFlag, "approved")

	w.checkScope(w.kf, "employer", []uuid.UUID{first, second, approved})
	w.checkScope(w.kf, "location", []uuid.UUID{first, second, approved, holdingFlag})
	w.checkScope(w.km, "employer", []uuid.UUID{holdingFlag})
	w.checkScope(w.km, "location", nil)
	got := w.all(w.kf, "employer", 1000)
	var occurred, reviewed time.Time
	if err := w.r.owner.QueryRow(w.ctx, `SELECT t.occurred_at, r.reviewed_at FROM transactions t JOIN transaction_reviews r ON r.transaction_id = t.id WHERE t.id = $1`,
		approved).Scan(&occurred, &reviewed); err != nil {
		t.Fatal(err)
	}
	if a := got[2]; !a.PunchTime.Equal(occurred) || a.RecordedAt.Before(reviewed.Add(-time.Second)) || !a.RecordedAt.After(got[1].RecordedAt) {
		t.Errorf("the approved flag: punchTime %v (tap %v), recordedAt %v (approval %v, after the later tap's %v)",
			a.PunchTime, occurred, a.RecordedAt, reviewed, got[1].RecordedAt)
	}
	for _, id := range []uuid.UUID{rejected, waiting, practiceFlag, manualFlag} {
		for _, scope := range []string{"employer", "location"} {
			for _, p := range w.all(w.kf, scope, 1000) {
				if p.ID == pch(id) {
					t.Errorf("scope=%s answers %s, which never enters the feed", scope, p.ID)
				}
			}
		}
	}
}

// TestPunches_ApprovalAfterTheHoldingEndedNamesNoCounterpart (ADR 0027 §8, ADR 0028 API-2 note
// deviation 4): KM's flag on KF's plaque, approved AFTER KM left the holding, answers once in KM's
// employer scope with the location fields null (the site's identities are not written past the
// membership) but tagId the plaque's; KF's location scope answers nothing.
func TestPunches_ApprovalAfterTheHoldingEndedNamesNoCounterpart(t *testing.T) {
	w := newPunchWorld(t)
	flag := w.write(punchRow{employer: w.km, site: &w.kf, verdict: "flag", channel: "nfc", kind: "out", ago: time.Minute})
	w.exec(`UPDATE holding_members SET left_at = clock_timestamp() WHERE member_tenant_id = $1 AND left_at IS NULL`, w.km.id)
	w.review(w.km, flag, "approved")
	w.checkScope(w.km, "employer", []uuid.UUID{flag})
	w.checkScope(w.kf, "location", nil)
	p := w.all(w.km, "employer", 1000)[0]
	if p.Location.ID != nil || p.Location.ExternalRef != nil || p.Location.Name != nil || p.Location.Company != nil ||
		!eqStr(p.TagID, sp("tag_"+w.kf.tag)) || p.Direction != "out" || !eqStr(p.Employee.Company, sp("KM")) {
		t.Fatalf("past the membership: %+v, want a null location, KF's plaque and KM's own employee", p)
	}
}

// TestPunches_Limit1001Is400 is plan §5 API-7 ("limit 1001 → 400"; ADR 0028 §8: limit ≤ 1000, default
// 500): 0, 1001, a letter, a sign, a blank, a second value and an exponent are 400 invalid_request
// naming the parameter; 1 is a page of one with hasMore; 1000 and the default answer every event of a
// small feed with hasMore false.
func TestPunches_Limit1001Is400(t *testing.T) {
	w := newPunchWorld(t)
	for range 3 {
		w.write(own(w.kf, "nfc", "in"))
	}
	for _, bad := range []string{"0", "1001", "abc", "-1", "+5", "", "1e3", "5&limit=6", "99999999999"} {
		rec := w.get(w.kf.raw, "scope=employer&limit="+bad)
		if pr := problemOf(t, rec); rec.Code != http.StatusBadRequest || pr.Code != CodeInvalidRequest || pr.Details["parameter"] != "limit" {
			if !(bad == "5&limit=6" && rec.Code == http.StatusBadRequest && pr.Code == CodeInvalidRequest) {
				t.Errorf("limit=%q: %d %s", bad, rec.Code, rec.Body.String())
			}
		}
	}
	one := pageOf(t, w.get(w.kf.raw, "scope=employer&limit=1"))
	if len(one.Data) != 1 || !one.HasMore || one.NextCursor != "cur_1" {
		t.Fatalf("limit=1: %d events, hasMore %v, next %s", len(one.Data), one.HasMore, one.NextCursor)
	}
	for _, q := range []string{"scope=employer&limit=1000", "scope=employer"} {
		p := pageOf(t, w.get(w.kf.raw, q))
		if len(p.Data) != 3 || p.HasMore || p.NextCursor != "cur_3" {
			t.Fatalf("%s: %d events, hasMore %v, next %s", q, len(p.Data), p.HasMore, p.NextCursor)
		}
	}
}

// TestPunches_TheQueryIsTheContract (ADR 0028 §8; the API-5 body rule applied to the query): scope
// is required and is one of the two names exactly; a name the endpoint does not take (another case,
// a misspelling, from/to), a name twice and a query string that does not parse are 400 -- and no
// refusal repeats what the request sent.
func TestPunches_TheQueryIsTheContract(t *testing.T) {
	w := newPunchWorld(t)
	w.write(own(w.kf, "nfc", "in"))
	for _, c := range []struct{ query, field string }{
		{"", "scope"}, {"limit=5", "scope"}, {"scope=", "scope"}, {"scope=Employer", "scope"}, {"scope=employers", "scope"},
		{"scope=both", "scope"}, {"scope=employer,location", "scope"},
		{"scope=employer&scope=location", ""}, {"Scope=employer", ""}, {"scope=employer&curser=cur_0", ""},
		{"scope=employer&from=2026-10-01T00:00:00Z&to=2026-10-02T00:00:00Z", ""}, {"scope=employer&zzunknownzz=1", ""},
		{"scope=employer&%zz=1", ""}, {"scope=employer;limit=5", ""},
	} {
		rec := w.get(w.kf.raw, c.query)
		pr := problemOf(t, rec)
		if rec.Code != http.StatusBadRequest || pr.Code != CodeInvalidRequest {
			t.Errorf("%q: %d %s", c.query, rec.Code, rec.Body.String())
			continue
		}
		if c.field != "" && pr.Details["parameter"] != c.field {
			t.Errorf("%q: details %v, want the parameter %s", c.query, pr.Details, c.field)
		}
		for _, echo := range []string{"Employer", "employers", "curser", "zzunknownzz", "%zz", "2026-10-01"} {
			if strings.Contains(c.query, echo) && strings.Contains(rec.Body.String(), echo) {
				t.Errorf("%q: the refusal repeats %q: %s", c.query, echo, rec.Body.String())
			}
		}
	}
}

// TestPunches_CursorGames (ADR 0028 §5: "imleç seq'i opak taşır; tenant ve kapsam filtresi her
// sorguda ayrıca"): the cursor is cur_<position> and carries nothing else. A negative, a letter, a
// leading zero, a bare number, another prefix, an empty value, a value past int64 and one past the
// feed's end are 400 invalid_request naming the cursor, repeating none of it. Another business's
// cursor is a position in THIS feed: past this feed's end it is refused like any other; inside it, it
// answers this business's events after that position -- never the other business's. The same cursor
// read under the other scope is a position in the same feed. And an empty page keeps the position
// the reader reached: nextCursor is never absent and never goes back.
func TestPunches_CursorGames(t *testing.T) {
	w := newPunchWorld(t)
	kf1 := w.write(own(w.kf, "nfc", "in"))
	for range 6 {
		w.write(own(w.km, "nfc", "in"))
	}
	kf2 := w.write(own(w.kf, "qr", "out"))
	for _, bad := range []string{"cur_-1", "-1", "cur_abc", "abc", "cur_01", "cur_", "", "3", "1", "0", "CUR_1", "pch_1", "cur_1x", "cur_ 1",
		"cur_9223372036854775808", "cur_99999999999999999999", "cur_9223372036854775807", "cur_3", "emp_" + w.kf.emp.String(),
		"cur_١"} {
		rec := w.get(w.kf.raw, "scope=employer&cursor="+url.QueryEscape(bad))
		pr := problemOf(t, rec)
		if rec.Code != http.StatusBadRequest || pr.Code != CodeInvalidRequest || pr.Details["parameter"] != "cursor" {
			t.Errorf("cursor=%q: %d %s", bad, rec.Code, rec.Body.String())
		}
		if len(bad) > 3 && strings.Contains(rec.Body.String(), bad) {
			t.Errorf("cursor=%q: the refusal repeats it", bad)
		}
	}
	// KM's last position (6) is past KF's feed (2): refused.
	kmNext := pageOf(t, w.get(w.km.raw, "scope=employer")).NextCursor
	if rec := w.get(w.kf.raw, "scope=employer&cursor="+kmNext); rec.Code != http.StatusBadRequest {
		t.Fatalf("KM's cursor %s in KF's hands: %d %s", kmNext, rec.Code, rec.Body.String())
	}
	// KM's position 1 in KF's hands: KF's events after 1 -- KF's second tap, nothing of KM's.
	if p := pageOf(t, w.get(w.kf.raw, "scope=employer&cursor=cur_1")); !slices.Equal(ids(p.Data), []string{pch(kf2)}) {
		t.Fatalf("cur_1 in KF's feed answered %v, want KF's second tap", ids(p.Data))
	}
	// The same position, the other scope: the same feed.
	if p := pageOf(t, w.get(w.kf.raw, "scope=location&cursor=cur_0&limit=1")); !slices.Equal(ids(p.Data), []string{pch(kf1)}) || p.NextCursor != "cur_1" || !p.HasMore {
		t.Fatalf("location from cur_0: %v %s %v", ids(p.Data), p.NextCursor, p.HasMore)
	}
	// Caught up: an empty page keeps the reader's position.
	for _, c := range []string{"cur_2", ""} {
		q := "scope=employer"
		if c != "" {
			q += "&cursor=" + c
		}
		p := pageOf(t, w.get(w.kf.raw, q))
		if p.NextCursor != "cur_2" || p.HasMore {
			t.Fatalf("%q: next %s hasMore %v, want cur_2 and false", q, p.NextCursor, p.HasMore)
		}
	}
	// A business with no event: the beginning is cur_0.
	if p := pageOf(t, w.get(w.d.raw, "scope=location")); len(p.Data) != 0 || p.NextCursor != "cur_0" || p.HasMore {
		t.Fatalf("an empty feed: %+v", p)
	}
}

// TestPunches_NothingButTheFeedsColumnsAnswers (K-1f, K-39; ADR 0028 §6 "Yük"): every row behind the
// answers carries the world's canary in its note, source address, coordinate, rule, policy context
// and trust, and every employee an e-mail address -- the bytes of every page, in both scopes of every
// business, contain none of them, nor the words verdict, trust, note, email, gps, ip or policy; each
// punch carries exactly the contract's keys.
func TestPunches_NothingButTheFeedsColumnsAnswers(t *testing.T) {
	w := newPunchWorld(t)
	w.write(own(w.kf, "nfc", "in"))
	w.write(at(w.km, w.kf, "in"))
	w.write(at(w.kf, w.km, "out"))
	flag := w.write(punchRow{employer: w.km, site: &w.kf, verdict: "flag", channel: "nfc", kind: "out", ago: time.Minute})
	w.review(w.km, flag, "approved")
	n := 0
	for _, b := range []punchBiz{w.kf, w.km} {
		for _, scope := range []string{"employer", "location"} {
			rec := w.get(b.raw, "scope="+scope)
			body := strings.ToLower(rec.Body.String())
			for _, never := range []string{strings.ToLower(w.canary), "198.51.100.213", "35.98", "14.12", "@punches.example.test", "punch staff",
				"verdict", "trust", "note", "email", "gps", "\"ip", "policy", "sid", "matched", "practice", "roles"} {
				if strings.Contains(body, never) {
					t.Errorf("%s scope=%s: the answer carries %q", b.code, scope, never)
				}
			}
			var raw struct {
				Data []map[string]json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
				t.Fatal(err)
			}
			for _, p := range raw.Data {
				n++
				if got := keysOf(p); !slices.Equal(got, []string{"channel", "direction", "employee", "id", "location", "punchTime", "recordedAt", "status", "tagId", "version"}) {
					t.Errorf("a punch's keys: %v", got)
				}
				var e, l map[string]json.RawMessage
				if json.Unmarshal(p["employee"], &e) != nil || json.Unmarshal(p["location"], &l) != nil {
					t.Fatal("employee or location is not an object")
				}
				if !slices.Equal(keysOf(e), []string{"company", "employeeCode", "externalRef", "id"}) ||
					!slices.Equal(keysOf(l), []string{"company", "externalRef", "id", "name"}) {
					t.Errorf("employee keys %v, location keys %v", keysOf(e), keysOf(l))
				}
			}
		}
	}
	if n != 8 {
		t.Fatalf("%d punches judged, want 8 (KF 2+2, KM 2+2)", n)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// TestPunches_ClosedIs403RevokedIs401SuspendedIsOpen (ADR 0028 §2 K-22, §3, §10): the operator
// closes KF's API -> the next read is 403 api_disabled; reopened -> 200; a suspended business still
// reads its punches (§10: every GET is open); a revoked key is 401.
func TestPunches_ClosedIs403RevokedIs401SuspendedIsOpen(t *testing.T) {
	w := newPunchWorld(t)
	id := w.write(own(w.kf, "nfc", "in"))
	w.exec(`UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, w.kf.id)
	if rec := w.get(w.kf.raw, "scope=employer"); rec.Code != http.StatusForbidden || problemOf(t, rec).Code != CodeAPIDisabled {
		t.Fatalf("a closed API: %d %s", rec.Code, rec.Body.String())
	}
	w.exec(`UPDATE tenants SET api_enabled_at = now() WHERE id = $1`, w.kf.id)
	w.r.suspended.suspend(w.kf.id)
	if p := pageOf(t, w.get(w.kf.raw, "scope=employer")); !slices.Equal(ids(p.Data), []string{pch(id)}) {
		t.Fatalf("suspended: %v", ids(p.Data))
	}
	if _, err := w.r.keys.Revoke(w.ctx, apikey.RevokeCommand{TenantID: w.kf.id, ActorID: w.kf.admin, KeyID: w.kf.keyID}); err != nil {
		t.Fatal(err)
	}
	if rec := w.get(w.kf.raw, "scope=employer"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked key: %d %s", rec.Code, rec.Body.String())
	}
}

// TestPunches_PagesNeitherSkipNorRepeatUnderConcurrentWriters is ADR 0028 §5's proof measured from
// the READ END (the API-2 stress, re-run through GET /v1/punches): 12 writers x 40 rows over KF, KM,
// C and D (own taps and holding taps both ways inside each holding; a tenth rolled back whole, a
// tenth with a row rolled back to a savepoint before the committed one) while a reader per business
// and scope pages through the endpoint with small random limits, following nextCursor. At the end
// every reader has every event of its scope exactly once, in feed order -- no skip, no repeat -- and
// the feeds hold exactly the committed rows' targets.
func TestPunches_PagesNeitherSkipNorRepeatUnderConcurrentWriters(t *testing.T) {
	w := newPunchWorld(t)
	bizs := []punchBiz{w.kf, w.km, w.c, w.d}
	peer := map[uuid.UUID]punchBiz{w.kf.id: w.km, w.km.id: w.kf, w.c.id: w.d, w.d.id: w.c}
	const writers, rounds = 12, 40
	type reader struct {
		b     punchBiz
		scope string
		got   []string
		pages int
	}
	var readers []*reader
	for _, b := range bizs {
		for _, scope := range []string{"employer", "location"} {
			readers = append(readers, &reader{b: b, scope: scope})
		}
	}
	var (
		mu        sync.Mutex
		committed = map[uuid.UUID]map[string]bool{}
		failures  []error
	)
	for _, b := range bizs {
		committed[b.id] = map[string]bool{}
	}
	poll := func(rd *reader, cursor string) (string, bool, error) {
		q := "scope=" + rd.scope + "&limit=" + fmt.Sprint(1+int(randomBytes(t, 1)[0])%7)
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		rec := w.get(rd.b.raw, q)
		if rec.Code != http.StatusOK {
			return cursor, false, fmt.Errorf("%s %s: %d %s", rd.b.code, q, rec.Code, rec.Body.String())
		}
		var p punchPage
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
			return cursor, false, err
		}
		rd.pages++
		rd.got = append(rd.got, ids(p.Data)...)
		return p.NextCursor, p.HasMore, nil
	}
	done := make(chan struct{})
	var rwg sync.WaitGroup
	for _, rd := range readers {
		rwg.Add(1)
		go func() {
			defer rwg.Done()
			cursor := ""
			for {
				select {
				case <-done:
					for range 100000 {
						next, more, err := poll(rd, cursor)
						if err != nil {
							mu.Lock()
							failures = append(failures, err)
							mu.Unlock()
							return
						}
						cursor = next
						if !more {
							return
						}
					}
					return
				default:
					next, _, err := poll(rd, cursor)
					if err != nil {
						mu.Lock()
						failures = append(failures, err)
						mu.Unlock()
						return
					}
					cursor = next
				}
			}
		}()
	}
	var wwg sync.WaitGroup
	start := make(chan struct{})
	for i := range writers {
		wwg.Add(1)
		go func() {
			defer wwg.Done()
			<-start
			for r := range rounds {
				employer := bizs[(i+r)%4]
				row := own(employer, []string{"nfc", "qr"}[r%2], []string{"in", "out"}[(i+r)%2])
				if randomBytes(t, 1)[0]%2 == 0 {
					row = at(employer, peer[employer.id], row.kind)
				}
				mode := int(randomBytes(t, 1)[0]) % 10
				var id uuid.UUID
				errAbort := errors.New("abort")
				err := w.r.d.WithTenant(w.ctx, employer.id, func(ctx context.Context, tx pgx.Tx) error {
					if mode == 0 {
						sp, err := tx.Begin(ctx)
						if err != nil {
							return err
						}
						if _, err := w.insertPunch(ctx, sp, row); err != nil {
							return err
						}
						if err := sp.Rollback(ctx); err != nil {
							return err
						}
					}
					var err error
					if id, err = w.insertPunch(ctx, tx, row); err != nil {
						return err
					}
					time.Sleep(time.Duration(randomBytes(t, 1)[0]%3) * time.Millisecond)
					if mode == 1 {
						return errAbort
					}
					return nil
				})
				mu.Lock()
				switch {
				case err == nil:
					committed[employer.id][pch(id)] = true
					if row.site != nil {
						committed[row.site.id][pch(id)] = true
					}
				case errors.Is(err, errAbort):
				default:
					failures = append(failures, err)
				}
				mu.Unlock()
			}
		}()
	}
	close(start)
	wwg.Wait()
	close(done)
	rwg.Wait()
	if len(failures) > 0 {
		t.Fatalf("%d failures, the first: %v", len(failures), failures[0])
	}
	events, pages := 0, 0
	for _, rd := range readers {
		var want []string
		total := map[string]bool{}
		for _, f := range w.feedOf(rd.b.id) {
			total[pch(f.punch)] = true
			if strings.Contains(","+f.roles+",", ","+rd.scope+",") {
				want = append(want, pch(f.punch))
			}
		}
		if rd.scope == "employer" && len(total) != len(committed[rd.b.id]) {
			t.Errorf("%s's feed holds %d punches for %d committed targets", rd.b.code, len(total), len(committed[rd.b.id]))
		}
		for id := range committed[rd.b.id] {
			if !total[id] {
				t.Errorf("%s: the committed %s has no event", rd.b.code, id)
			}
		}
		if !slices.Equal(rd.got, want) {
			seen, dup := map[string]bool{}, 0
			for _, id := range rd.got {
				if seen[id] {
					dup++
				}
				seen[id] = true
			}
			missed := 0
			for _, id := range want {
				if !seen[id] {
					missed++
				}
			}
			t.Errorf("%s scope=%s: read %d, want %d -- %d missed, %d repeated (out of order otherwise)", rd.b.code, rd.scope, len(rd.got), len(want), missed, dup)
		}
		events += len(rd.got)
		pages += rd.pages
	}
	t.Logf("%d events read over %d pages by %d readers; 0 missed, 0 repeated", events, pages, len(readers))
}
