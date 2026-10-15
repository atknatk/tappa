package api

// punchhistory_db_test.go -- the history query, GET /v1/punches?from=…&to=… (M11 API-7b; ADR 0028
// §5, plan §2 item 18 and §5 API-7b), over the real chain, reader and database, in punches_db_test.go's
// world. The guarantee measured is the SNAPSHOT's: a walk answers exactly the events at or below the
// S0 its first page read, in (punchTime, position) order, none twice.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/tenant"
)

// histBase is a whole second ten days ago: the ranges below sit around it.
func histBase() time.Time { return time.Now().UTC().Truncate(time.Second).Add(-10 * 24 * time.Hour) }

func rfc(t time.Time) string { return url.QueryEscape(t.UTC().Format(time.RFC3339Nano)) }

func tp(t time.Time) *time.Time { return &t }

// historyQ is the query string of a history page.
func historyQ(scope string, from, to time.Time, limit int, cursor string) string {
	q := "scope=" + scope + "&from=" + rfc(from) + "&to=" + rfc(to) + "&limit=" + fmt.Sprint(limit)
	if cursor != "" {
		q += "&cursor=" + url.QueryEscape(cursor)
	}
	return q
}

// walk reads [from, to) of b's scope to hasMore=false, limit at a time; it returns the punches and
// the S0 the walk is bound to (from its first cursor).
func (w *punchWorld) walk(b punchBiz, scope string, from, to time.Time, limit int) ([]punchBody, int64) {
	w.r.t.Helper()
	var out []punchBody
	cursor := ""
	for range 100000 {
		p := pageOf(w.r.t, w.get(b.raw, historyQ(scope, from, to, limit, cursor)))
		out = append(out, p.Data...)
		hc, ok := parseHistoryCursor(p.NextCursor)
		if !ok || (cursor != "" && hc.pos.S0 != mustHist(w.r.t, cursor).pos.S0) {
			w.r.t.Fatalf("nextCursor %q is not a history cursor of the same snapshot", p.NextCursor)
		}
		cursor = p.NextCursor
		if !p.HasMore {
			return out, hc.pos.S0
		}
	}
	w.r.t.Fatal("the walk did not end")
	return nil, 0
}

func mustHist(t *testing.T, c string) historyCursor {
	t.Helper()
	hc, ok := parseHistoryCursor(c)
	if !ok {
		t.Fatalf("%q is not a history cursor", c)
	}
	return hc
}

// snapshotOf is what a walk bound to s0 must answer: b's events of scope in [from, to) at or below
// s0, in (punch_time, seq) order -- read as the owner.
func (w *punchWorld) snapshotOf(b punchBiz, scope string, from, to time.Time, s0 int64) []string {
	w.r.t.Helper()
	rows, err := w.r.owner.Query(w.ctx, `SELECT punch_id FROM punch_events WHERE tenant_id = $1 AND $2::text = ANY (roles)
	                                       AND punch_time >= $3 AND punch_time < $4 AND seq <= $5 ORDER BY punch_time, seq`, b.id, scope, from, to, s0)
	if err != nil {
		w.r.t.Fatal(err)
	}
	ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		w.r.t.Fatal(err)
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, pch(id))
	}
	return out
}

func pchs(ids ...uuid.UUID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, pch(id))
	}
	return out
}

// TestPunchHistory_FourShapesTimesTwoScopes (plan §5 API-7b: "kapsam ve tenant süzgeci scope ile
// aynı"): the four shapes inside [from, to), declared out of their write order, with an event exactly
// at from (in), one exactly at to (out), one a second before from (out) and two at one instant (by
// their feed position). Each business's scope answers exactly its events of the range in (punchTime,
// position) order, each equal to its own feed row field by field, at every page size; C and D answer
// none.
func TestPunchHistory_FourShapesTimesTwoScopes(t *testing.T) {
	w := newPunchWorld(t)
	t0 := histBase()
	from, to := t0, t0.Add(5*time.Hour)
	in := func(r punchRow, at time.Time) punchRow { r.at = tp(at); return r }
	kmkm := w.write(in(own(w.km, "qr", "out"), t0.Add(2*time.Hour)))
	kfkf := w.write(in(own(w.kf, "nfc", "in"), t0.Add(1*time.Hour)))
	kfkm := w.write(in(at(w.kf, w.km, "out"), t0.Add(4*time.Hour)))
	kmkf := w.write(in(at(w.km, w.kf, "in"), t0.Add(3*time.Hour)))
	atFrom := w.write(in(own(w.kf, "nfc", "in"), from))
	w.write(in(own(w.kf, "nfc", "out"), to))
	w.write(in(own(w.kf, "nfc", "out"), from.Add(-time.Second)))
	tie1 := w.write(in(own(w.km, "nfc", "in"), t0.Add(90*time.Minute)))
	tie2 := w.write(in(own(w.km, "nfc", "out"), t0.Add(90*time.Minute)))

	for _, c := range []struct {
		b     punchBiz
		scope string
		want  []string
	}{
		{w.kf, "employer", pchs(atFrom, kfkf, kfkm)},
		{w.kf, "location", pchs(atFrom, kfkf, kmkf)},
		{w.km, "employer", pchs(tie1, tie2, kmkm, kmkf)},
		{w.km, "location", pchs(tie1, tie2, kmkm, kfkm)},
		{w.c, "employer", nil}, {w.c, "location", nil}, {w.d, "employer", nil}, {w.d, "location", nil},
	} {
		feed := map[string]feedRow{}
		for _, f := range w.feedOf(c.b.id) {
			feed[pch(f.punch)] = f
		}
		for _, limit := range []int{1, 2, 1000} {
			got, _ := w.walk(c.b, c.scope, from, to, limit)
			if !slices.Equal(ids(got), c.want) {
				t.Fatalf("%s scope=%s limit=%d answered %v, want %v", c.b.code, c.scope, limit, ids(got), c.want)
			}
			for _, p := range got {
				if err := sameAsFeed(p, feed[p.ID]); err != nil {
					t.Fatalf("%s scope=%s %s: %v", c.b.code, c.scope, p.ID, err)
				}
			}
		}
	}
}

// TestPunchHistory_NoManualNoRejected (plan §5 API-7b: "K-4 listesinin tamamı yok; onaylı flag bir
// kez"): inside the range a manual record, a practice tap and an approved practice flag, a reject, an
// ignored repeat, a rejected flag, a waiting flag, an approved manual flag, a holding manual record
// and a holding reject answer nowhere; the approved flag -- own and holding -- answers once, at its
// declared punchTime's place.
func TestPunchHistory_NoManualNoRejected(t *testing.T) {
	w := newPunchWorld(t)
	t0 := histBase()
	from, to := t0, t0.Add(24*time.Hour)
	row := func(b punchBiz, site *punchBiz, verdict, channel, kind string, practice bool, h int) punchRow {
		return punchRow{employer: b, site: site, verdict: verdict, channel: channel, kind: kind, practice: practice, at: tp(t0.Add(time.Duration(h) * time.Hour))}
	}
	ok1 := w.write(row(w.kf, nil, "ok", "nfc", "in", false, 1))
	w.write(row(w.kf, nil, "ok", "manual", "out", false, 2))
	w.write(row(w.kf, nil, "ok", "nfc", "in", true, 3))
	practiceFlag := w.write(row(w.kf, nil, "flag", "qr", "in", true, 4))
	w.write(row(w.kf, nil, "reject", "nfc", "", false, 5))
	w.write(row(w.kf, nil, "ignored", "qr", "", false, 6))
	rejected := w.write(row(w.kf, nil, "flag", "nfc", "out", false, 7))
	w.write(row(w.kf, nil, "flag", "qr", "in", false, 8))
	approved := w.write(row(w.kf, nil, "flag", "qr", "out", false, 9))
	manualFlag := w.write(row(w.kf, nil, "flag", "manual", "in", false, 10))
	holdingFlag := w.write(row(w.km, &w.kf, "flag", "nfc", "in", false, 11))
	w.write(row(w.km, &w.kf, "ok", "manual", "out", false, 12))
	w.write(row(w.km, &w.kf, "reject", "nfc", "", false, 13))
	ok2 := w.write(row(w.kf, nil, "ok", "qr", "out", false, 14))
	w.review(w.kf, rejected, "rejected")
	w.review(w.kf, practiceFlag, "approved")
	w.review(w.kf, approved, "approved")
	w.review(w.kf, manualFlag, "approved")
	w.review(w.km, holdingFlag, "approved")
	for _, c := range []struct {
		b     punchBiz
		scope string
		want  []string
	}{
		{w.kf, "employer", pchs(ok1, approved, ok2)},
		{w.kf, "location", pchs(ok1, approved, holdingFlag, ok2)},
		{w.km, "employer", pchs(holdingFlag)},
		{w.km, "location", nil},
	} {
		if got, _ := w.walk(c.b, c.scope, from, to, 2); !slices.Equal(ids(got), c.want) {
			t.Errorf("%s scope=%s answered %v, want %v", c.b.code, c.scope, ids(got), c.want)
		}
	}
}

// TestPunchHistory_OutsiderSeesNone (plan §5 API-7b: "C görmez"): C (holding H2) asks the range KF's
// and KM's punches -- own and holding, both directions -- sit in: only its own five punches, in both
// scopes; KF's and KM's keys never answer C's. A cursor KF was handed, in C's hands with the same
// scope and range while C's counter is past KF's S0, is 400: the binding names the business.
func TestPunchHistory_OutsiderSeesNone(t *testing.T) {
	w := newPunchWorld(t)
	t0 := histBase()
	from, to := t0, t0.Add(6*time.Hour)
	in := func(r punchRow, h int) punchRow { r.at = tp(t0.Add(time.Duration(h) * time.Hour)); return r }
	w.write(in(own(w.kf, "nfc", "in"), 1))
	w.write(in(at(w.km, w.kf, "in"), 2))
	w.write(in(at(w.kf, w.km, "out"), 3))
	w.write(in(own(w.km, "nfc", "in"), 4))
	cc := w.write(in(own(w.c, "nfc", "in"), 5))
	var cs []uuid.UUID
	for range 4 {
		cs = append(cs, w.write(in(own(w.c, "qr", "in"), 0)))
	}
	cAll := pchs(append(cs, cc)...)
	for _, scope := range []string{"employer", "location"} {
		if got, _ := w.walk(w.c, scope, from, to, 1); !slices.Equal(ids(got), cAll) {
			t.Fatalf("C scope=%s answered %v", scope, ids(got))
		}
		for _, b := range []punchBiz{w.kf, w.km} {
			got, _ := w.walk(b, scope, from, to, 1000)
			if slices.Contains(ids(got), pch(cc)) {
				t.Fatalf("%s scope=%s answered C's punch", b.code, scope)
			}
		}
	}
	// KF's cursor in C's hands -- C's counter (5) is past KF's S0 (3), so S0 alone no longer refuses
	// it: the binding names the business, and it is 400 (API-7b review: it was a short, silent read).
	p := pageOf(t, w.get(w.kf.raw, historyQ("employer", from, to, 1, "")))
	if hc := mustHist(t, p.NextCursor); hc.pos.S0 >= 5 {
		t.Fatalf("KF's S0 %d; the case needs C's counter past it", hc.pos.S0)
	}
	if rec := w.get(w.c.raw, historyQ("employer", from, to, 1, p.NextCursor)); rec.Code != http.StatusBadRequest || problemOf(t, rec).Details["parameter"] != "cursor" {
		t.Fatalf("KF's history cursor in C's hands: %d %s, want 400", rec.Code, rec.Body.String())
	}
}

// TestPunchHistory_TheRangeAndTheCursorAreTheContract (plan §5 API-7b): [from, to), from before to;
// from or to alone, from = to,
// a reversed range, a malformed instant, one with no offset and one finer than a microsecond are 400
// naming both parameters; a feed cursor (cur_) with from/to, and a history cursor (hst_) without them,
// are 400 -- never silently a position of the other kind; a history cursor sent with another scope,
// from or to, a malformed one, one whose S0 is past the counter and one whose position is outside the
// range are 400. The range is at most 31 days and one hour (Malta's October and March are one query
// each; an hour and a microsecond more is not); an offset of +24:00, a comma before the fraction, a
// lower-case t, an hour of 25 and a 31st of February -- which Go's parser partly takes -- are 400; an
// unencoded '+' is 400 with reason unencoded_plus. No refusal repeats what was sent.
func TestPunchHistory_TheRangeAndTheCursorAreTheContract(t *testing.T) {
	w := newPunchWorld(t)
	t0 := histBase()
	for i := range 3 {
		r := own(w.kf, "nfc", "in")
		r.at = tp(t0.Add(time.Duration(i+1) * time.Hour))
		w.write(r)
	}
	from, to := t0, t0.Add(24*time.Hour)
	ok := func(q string) punchPage { t.Helper(); return pageOf(t, w.get(w.kf.raw, q)) }
	ok(historyQ("employer", from, from.Add(31*24*time.Hour+time.Hour), 10, ""))
	// A calendar month of Malta, both daylight-saving changes (the user-facing reading of "31 days").
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []time.Month{time.October, time.March} {
		a, b := time.Date(2026, m, 1, 0, 0, 0, 0, malta), time.Date(2026, m+1, 1, 0, 0, 0, 0, malta)
		ok("scope=employer&from=" + url.QueryEscape(a.Format(time.RFC3339)) + "&to=" + url.QueryEscape(b.Format(time.RFC3339)))
	}
	ok("scope=employer&from=" + url.QueryEscape(from.In(time.FixedZone("x", 3600)).Format(time.RFC3339)) + "&to=" + rfc(to))
	bad := func(name, q, param string, echo ...string) {
		t.Helper()
		rec := w.get(w.kf.raw, q)
		pr := problemOf(t, rec)
		if rec.Code != http.StatusBadRequest || pr.Code != CodeInvalidRequest {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
			return
		}
		if param == "cursor" && pr.Details["parameter"] != "cursor" {
			t.Errorf("%s: details %v, want the cursor", name, pr.Details)
		}
		if param == "range" && fmt.Sprint(pr.Details["parameters"]) != "[from to]" {
			t.Errorf("%s: details %v, want from and to", name, pr.Details)
		}
		for _, e := range echo {
			if strings.Contains(rec.Body.String(), e) {
				t.Errorf("%s: the refusal repeats %q", name, e)
			}
		}
	}
	fs, ts := rfc(from), rfc(to)
	bad("from alone", "scope=employer&from="+fs, "range")
	bad("to alone", "scope=employer&to="+ts, "range")
	bad("from = to", "scope=employer&from="+fs+"&to="+fs, "range")
	bad("reversed", "scope=employer&from="+ts+"&to="+fs, "range")
	bad("31 days, an hour and a microsecond", historyQ("employer", from, from.Add(31*24*time.Hour+time.Hour+time.Microsecond), 10, ""), "range")
	bad("an offset of 24 hours", "scope=employer&from=2026-10-01T00:00:00%2B24:00&to="+ts, "range", "24:00")
	bad("a comma before the fraction", "scope=employer&from=2026-10-01T00:00:00,5Z&to="+ts, "range", "00,5")
	bad("a lower-case t", "scope=employer&from=2026-10-01t00:00:00Z&to="+ts, "range", "01t00")
	bad("an hour of 25", "scope=employer&from=2026-10-01T25:00:00Z&to="+ts, "range")
	bad("a 31st of February", "scope=employer&from=2026-02-31T00:00:00Z&to=2026-03-02T00:00:00Z", "range")
	plus := w.get(w.kf.raw, "scope=employer&from=2026-10-01T00:00:00+02:00&to=2026-10-02T00:00:00+02:00")
	if pr := problemOf(t, plus); plus.Code != http.StatusBadRequest || pr.Details["reason"] != "unencoded_plus" || strings.Contains(plus.Body.String(), "02:00") {
		t.Errorf("an unencoded '+': %d %s, want 400 naming the encoding and repeating nothing", plus.Code, plus.Body.String())
	}
	bad("no offset", "scope=employer&from=2026-10-01T00:00:00&to="+ts, "range", "2026-10-01")
	bad("a date", "scope=employer&from=2026-10-01&to="+ts, "range", "2026-10-01")
	bad("garbage", "scope=employer&from=yesterday&to="+ts, "range", "yesterday")
	bad("nanoseconds", "scope=employer&from=2026-10-01T00:00:00.000000001Z&to=2026-10-02T00:00:00Z", "range", "000000001")
	bad("a feed cursor with from/to", historyQ("employer", from, to, 10, "cur_0"), "cursor")

	first := ok(historyQ("employer", from, to, 1, ""))
	if !first.HasMore || len(first.Data) != 1 || !strings.HasPrefix(first.NextCursor, "hst_") {
		t.Fatalf("the first page: %+v", first)
	}
	hc := mustHist(t, first.NextCursor)
	bad("a history cursor without from/to", "scope=employer&cursor="+url.QueryEscape(first.NextCursor), "cursor")
	bad("another scope", historyQ("location", from, to, 1, first.NextCursor), "cursor")
	bad("another from", historyQ("employer", from.Add(-time.Second), to, 1, first.NextCursor), "cursor")
	bad("another to", historyQ("employer", from, to.Add(time.Microsecond), 1, first.NextCursor), "cursor")
	bind := historyBinding(w.kf.id, "employer", from, to)
	forge := func(s0 int64, at time.Time, seq int64) string {
		return formatHistoryCursor(tenant.HistoryPosition{S0: s0, Time: at.UTC(), Seq: seq}, bind)
	}
	bad("S0 past the counter", historyQ("employer", from, to, 1, forge(hc.pos.S0+1, hc.pos.Time, hc.pos.Seq)), "cursor")
	bad("a position before from", historyQ("employer", from, to, 1, forge(hc.pos.S0, from.Add(-time.Microsecond), 0)), "cursor")
	bad("a position at to", historyQ("employer", from, to, 1, forge(hc.pos.S0, to, 0)), "cursor")
	for _, junk := range []string{"hst_", "hst_1_2_3", "hst_1_2_3_XYZ", "hst_01_2_3_" + bind, "hst_1_-0_3_" + bind, "hst_1_2_3_" + strings.ToUpper(bind),
		"hst_-1_2_3_" + bind, "hst_1_2_3_" + bind + "_4", "HST_1_2_3_" + bind, "hst_99999999999999999999_2_3_" + bind} {
		bad("junk "+junk, historyQ("employer", from, to, 1, junk), "cursor", junk)
	}
	// The forged cursor that is consistent is a position, nothing more: it answers this feed.
	if p := ok(historyQ("employer", from, to, 10, forge(hc.pos.S0, from, 0))); len(p.Data) != 3 {
		t.Fatalf("a well-formed cursor at the range's start answered %d punches, want 3", len(p.Data))
	}
}

// TestPunchHistory_ALateEventIsTheFeedsNotTheSnapshots (plan §5 API-7b: "geç gelen olaylar yalnız
// akışta kesin görünür"; ADR 0028 API-7b note): six punches in the range; a walk reads its first page,
// then -- while it is under way -- a flag declared BEFORE the page it read is approved, a phone's
// offline tap declared INSIDE the read stretch and one declared in the unread stretch are recorded.
// The walk answers the six and none of the three (they are above its S0), in order, none twice; the
// feed, from the position it stood at, answers all three; a new walk of the same range answers nine.
func TestPunchHistory_ALateEventIsTheFeedsNotTheSnapshots(t *testing.T) {
	w := newPunchWorld(t)
	t0 := histBase()
	from, to := t0, t0.Add(12*time.Hour)
	flag := w.write(punchRow{employer: w.kf, verdict: "flag", channel: "nfc", kind: "in", at: tp(t0.Add(30 * time.Minute))})
	var six []uuid.UUID
	for i := range 6 {
		r := own(w.kf, "nfc", []string{"in", "out"}[i%2])
		r.at = tp(t0.Add(time.Duration(i+1) * time.Hour))
		six = append(six, w.write(r))
	}
	feedAt := pageOf(t, w.get(w.kf.raw, "scope=employer")).NextCursor
	first := pageOf(t, w.get(w.kf.raw, historyQ("employer", from, to, 2, "")))
	got := append([]punchBody(nil), first.Data...)

	w.review(w.kf, flag, "approved")
	inRead := own(w.kf, "qr", "in")
	inRead.at = tp(t0.Add(90 * time.Minute))
	lateRead := w.write(inRead)
	inUnread := own(w.kf, "qr", "out")
	inUnread.at = tp(t0.Add(5*time.Hour + 30*time.Minute))
	lateUnread := w.write(inUnread)

	cursor := first.NextCursor
	for more := first.HasMore; more; {
		p := pageOf(t, w.get(w.kf.raw, historyQ("employer", from, to, 2, cursor)))
		got = append(got, p.Data...)
		cursor, more = p.NextCursor, p.HasMore
	}
	if !slices.Equal(ids(got), pchs(six...)) {
		t.Fatalf("the walk answered %v, want exactly the six of its snapshot %v", ids(got), pchs(six...))
	}
	feed := pageOf(t, w.get(w.kf.raw, "scope=employer&cursor="+feedAt))
	if !slices.Equal(ids(feed.Data), pchs(flag, lateRead, lateUnread)) {
		t.Fatalf("the feed after the walk's start answered %v, want the three late punches", ids(feed.Data))
	}
	again, _ := w.walk(w.kf, "employer", from, to, 1000)
	want := pchs(flag, six[0], lateRead, six[1], six[2], six[3], six[4], lateUnread, six[5])
	if !slices.Equal(ids(again), want) {
		t.Fatalf("a new walk answered %v, want %v", ids(again), want)
	}
}

// TestPunchHistory_RLSHoldsWithoutTheFilter (plan §5 API-7b: "filtresiz RLS sondası"): the history's
// range predicate WITHOUT the tenant filter, as tappa_app in KF's context, reads KF's rows only -- KM's
// rows of the same range exist (the owner sees them) and RLS hides them; with no context, nothing.
func TestPunchHistory_RLSHoldsWithoutTheFilter(t *testing.T) {
	w := newPunchWorld(t)
	t0 := histBase()
	from, to := t0, t0.Add(5*time.Hour)
	for i, r := range []punchRow{own(w.kf, "nfc", "in"), own(w.km, "nfc", "in"), at(w.km, w.kf, "in"), at(w.kf, w.km, "out")} {
		r.at = tp(t0.Add(time.Duration(i+1) * time.Hour))
		w.write(r)
	}
	var kmRows int
	if err := w.r.owner.QueryRow(w.ctx, `SELECT count(*) FROM punch_events WHERE tenant_id = $1 AND punch_time >= $2 AND punch_time < $3`, w.km.id, from, to).Scan(&kmRows); err != nil || kmRows != 3 {
		t.Fatalf("KM's rows in the range: %d %v, want 3", kmRows, err)
	}
	var tenants []uuid.UUID
	if err := w.r.d.WithTenant(w.ctx, w.kf.id, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT tenant_id FROM punch_events WHERE punch_time >= $1 AND punch_time < $2 ORDER BY punch_time, seq`, from, to)
		if err != nil {
			return err
		}
		tenants, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(tenants) != 3 {
		t.Fatalf("KF's context read %d rows of the range, want KF's 3", len(tenants))
	}
	for _, tn := range tenants {
		if tn != w.kf.id {
			t.Fatalf("KF's context read a row of %s", tn)
		}
	}
}

// TestPunchHistory_WalksNeitherSkipNorRepeatUnderConcurrentWriters is the snapshot's guarantee under
// concurrent writers (plan §5 API-7b: "atlama/yineleme yokluğu S0'a göre ... sayfalama sürerken"):
// 12 writers x 40 rows over KF, KM, C and D -- own and holding taps, each DECLARED at a random instant
// of the range (backdated, as offline queues are), a tenth rolled back whole, a tenth with a row rolled
// back to a savepoint -- while a reader per business and scope walks the range again and again with
// small random page sizes. Every finished walk answered exactly the events of the range at or below
// its S0, in (punchTime, position) order, none twice.
func TestPunchHistory_WalksNeitherSkipNorRepeatUnderConcurrentWriters(t *testing.T) {
	w := newPunchWorld(t, unmetered)
	t0 := histBase()
	from, to := t0, t0.Add(24*time.Hour)
	bizs := []punchBiz{w.kf, w.km, w.c, w.d}
	peer := map[uuid.UUID]punchBiz{w.kf.id: w.km, w.km.id: w.kf, w.c.id: w.d, w.d.id: w.c}
	type walkResult struct {
		b     punchBiz
		scope string
		s0    int64
		got   []string
	}
	var (
		mu       sync.Mutex
		walks    []walkResult
		failures []error
	)
	fail := func(err error) { mu.Lock(); failures = append(failures, err); mu.Unlock() }
	done := make(chan struct{})
	var rwg sync.WaitGroup
	for _, b := range bizs {
		for _, scope := range []string{"employer", "location"} {
			rwg.Add(1)
			go func() {
				defer rwg.Done()
				for {
					select {
					case <-done:
						return
					default:
					}
					res := walkResult{b: b, scope: scope}
					cursor := ""
					for {
						rec := w.get(b.raw, historyQ(scope, from, to, 1+int(randomBytes(t, 1)[0])%7, cursor))
						if rec.Code != http.StatusOK {
							fail(fmt.Errorf("%s %s: %d %s", b.code, scope, rec.Code, rec.Body.String()))
							return
						}
						var p punchPage
						if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
							fail(err)
							return
						}
						res.got = append(res.got, ids(p.Data)...)
						cursor = p.NextCursor
						if !p.HasMore {
							break
						}
					}
					hc, ok := parseHistoryCursor(cursor)
					if !ok {
						fail(fmt.Errorf("a walk ended on %q", cursor))
						return
					}
					res.s0 = hc.pos.S0
					mu.Lock()
					walks = append(walks, res)
					mu.Unlock()
				}
			}()
		}
	}
	var wwg sync.WaitGroup
	start := make(chan struct{})
	for i := range 12 {
		wwg.Add(1)
		go func() {
			defer wwg.Done()
			<-start
			for r := range 40 {
				employer := bizs[(i+r)%4]
				row := own(employer, []string{"nfc", "qr"}[r%2], []string{"in", "out"}[(i+r)%2])
				if randomBytes(t, 1)[0]%2 == 0 {
					row = at(employer, peer[employer.id], row.kind)
				}
				b := randomBytes(t, 4)
				row.at = tp(t0.Add(time.Duration(int(b[0])<<16|int(b[1])<<8|int(b[2])) * time.Millisecond % (24 * time.Hour)))
				mode := int(b[3]) % 10
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
					if _, err := w.insertPunch(ctx, tx, row); err != nil {
						return err
					}
					time.Sleep(time.Duration(randomBytes(t, 1)[0]%3) * time.Millisecond)
					if mode == 1 {
						return errAbort
					}
					return nil
				})
				if err != nil && !errors.Is(err, errAbort) {
					fail(err)
				}
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
	bad, events := 0, 0
	for _, wk := range walks {
		want := w.snapshotOf(wk.b, wk.scope, from, to, wk.s0)
		events += len(wk.got)
		if !slices.Equal(wk.got, want) {
			bad++
			if bad <= 5 {
				seen, dup := map[string]bool{}, 0
				for _, id := range wk.got {
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
				t.Errorf("%s scope=%s S0=%d: read %d, want %d -- %d missed, %d repeated", wk.b.code, wk.scope, wk.s0, len(wk.got), len(want), missed, dup)
			}
		}
	}
	if len(walks) < 16 {
		t.Fatalf("only %d walks finished; the measure needs walks under way while the writers write", len(walks))
	}
	t.Logf("%d walks, %d events answered, %d walks off their snapshot", len(walks), events, bad)
}

// tieGroup gives b n events at ONE punch_time whose heap order is the REVERSE of their feed order.
// The triggers cannot produce that (a tenant's seq and its row are written under one lock, so the
// heap follows seq and an index scan of (tenant_id, punch_time) returns ties in seq order by
// accident); a tie broken by anything but seq is then invisible. So the rows are a fixture: n flags
// (no event), and as the owner -- in b's context, append-only respected (INSERT only) -- their events
// with seq counter+n down to counter+1, the counter moved to counter+n. Returns the punch ids in feed
// (seq) order.
func (w *punchWorld) tieGroup(b punchBiz, n int, at time.Time) []uuid.UUID {
	w.r.t.Helper()
	ids := make([]uuid.UUID, n)
	for i := range ids {
		ids[i] = w.write(punchRow{employer: b, verdict: "flag", channel: "nfc", kind: "in", at: &at})
	}
	tx, err := w.r.owner.Begin(w.ctx)
	if err != nil {
		w.r.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(w.ctx, `SELECT set_config('app.tenant_id', $1, true)`, b.id.String()); err != nil {
		w.r.t.Fatal(err)
	}
	var base int64
	if err := tx.QueryRow(w.ctx, `SELECT last_seq FROM feed_counters WHERE tenant_id = $1 FOR UPDATE`, b.id).Scan(&base); err != nil {
		w.r.t.Fatal(err)
	}
	for i := n - 1; i >= 0; i-- {
		if _, err := tx.Exec(w.ctx, `
			INSERT INTO punch_events (tenant_id, seq, punch_id, punch_tenant_id, roles, punch_time, recorded_at, channel, direction,
			                          employee_id, employee_ref, employee_code, employee_company, location_id, location_ref, location_name,
			                          location_company, tag_uid)
			VALUES ($1, $2, $3, $1, '{employer,location}', $4, now(), 'nfc', 'in', $5, $6, $7, $8, $9, $10, $11, $8, $12)`,
			b.id, base+int64(i)+1, ids[i], at, b.emp, b.code+"-EMP", b.code+"-C1", b.code, b.venue, b.code+"-LOC", b.code+" Venue", b.tag); err != nil {
			w.r.t.Fatal(err)
		}
	}
	if _, err := tx.Exec(w.ctx, `UPDATE feed_counters SET last_seq = $2 WHERE tenant_id = $1`, b.id, base+int64(n)); err != nil {
		w.r.t.Fatal(err)
	}
	if err := tx.Commit(w.ctx); err != nil {
		w.r.t.Fatal(err)
	}
	return ids
}

// TestPunchHistory_ATieIsBrokenByTheFeedOrder (API-7b review, E-ORTA): 32 punches at one instant,
// written so the heap holds them in the REVERSE of their feed order (tieGroup), between two
// punches before and after them. Walked with every page size from 1 to 7 and with 50, the range
// answers them in feed order, each once -- the tie is broken by the feed position everywhere it
// is ordered (the window, its end, the page). MUTANT (measured): ORDER BY punch_time alone, in the
// window, its end and the page -- red.
func TestPunchHistory_ATieIsBrokenByTheFeedOrder(t *testing.T) {
	w := newPunchWorld(t)
	t0 := histBase()
	from, to := t0, t0.Add(3*time.Hour)
	before := own(w.kf, "nfc", "in")
	before.at = tp(t0.Add(30 * time.Minute))
	first := w.write(before)
	tie := w.tieGroup(w.kf, 32, t0.Add(time.Hour))
	after := own(w.kf, "nfc", "out")
	after.at = tp(t0.Add(2 * time.Hour))
	last := w.write(after)
	want := pchs(append(append([]uuid.UUID{first}, tie...), last)...)
	for _, limit := range []int{1, 2, 3, 4, 5, 6, 7, 50} {
		for _, scope := range []string{"employer", "location"} {
			if got, _ := w.walk(w.kf, scope, from, to, limit); !slices.Equal(ids(got), want) {
				t.Fatalf("scope=%s limit=%d answered %d punches %v, want the %d in feed order", scope, limit, len(got), ids(got), len(want))
			}
		}
	}
}
