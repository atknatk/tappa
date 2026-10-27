package db

// punchdirectionsource_test.go -- M15-13 (migration 00067; ADR 0032 §B.6, §B.4, "Geri alma"; ADR 0028
// §4; ADR 0027 §9 item 3): the punch feed carries who chose a punch's direction.
//
// Everything runs in an owner transaction (opTx) that is rolled back; app-side writes go through
// holdingAppIn (tappa_app in the tenant's context), so the triggers fire as they do for a tap.
// Nothing here commits an event with a direction_source.

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/domain/tap"
)

// pdsExprRE is the one assignment both feed triggers make (00067's header): the reader rule's SQL
// copy, on one line.
var pdsExprRE = regexp.MustCompile(`(?m)^\s*v_dsrc := (.+);\s*$`)

// pdsFeedBodies49 are the md5s of 00049's punch_feed and holding_feed bodies -- what 00067's Up asks
// for and its Down gives back.
const (
	pdsPunchFeed49MD5   = "f9e0322dbb657a1573223168acf860d6"
	pdsHoldingFeed49MD5 = "eb918ce9f057bf264cbfce39f8daa178"
)

// pdsExpressions reads the source expression out of the INSTALLED bodies of the two triggers.
func pdsExpressions(t *testing.T, ctx context.Context, q opQuerier) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, fn := range []string{"punch_feed()", "holding_feed()"} {
		var src string
		if err := q.QueryRow(ctx, `SELECT prosrc FROM pg_proc WHERE oid = ('public.' || $1)::regprocedure`, fn).Scan(&src); err != nil {
			t.Fatalf("read %s: %v", fn, err)
		}
		for _, m := range pdsExprRE.FindAllStringSubmatch(src, -1) {
			out[fn] = append(out[fn], m[1])
		}
	}
	return out
}

// pdsParityFindings evaluates expr -- with v_channel, v_type and v_stored bound to every combination
// -- and holds each answer to tap.RecordedDirectionSource's.
func pdsParityFindings(t *testing.T, ctx context.Context, q opQuerier, expr string) []string {
	t.Helper()
	sql := expr
	for v, p := range map[string]string{"v_channel": "r.c", "v_type": "r.d", "v_stored": "r.s"} {
		sql = regexp.MustCompile(`\b`+v+`\b`).ReplaceAllLiteralString(sql, p)
	}
	if left := regexp.MustCompile(`\bv_[a-z_]+\b`).FindString(sql); left != "" {
		return []string{fmt.Sprintf("the expression reads %s, which is not the row's channel, direction or stored source: %s", left, expr)}
	}
	sql = "SELECT (" + sql + ")::text FROM (SELECT $1::text AS c, $2::text AS d, $3::text AS s) AS r"
	str := func(s string) *string { return &s }
	channels := []*string{str("nfc"), str("qr"), str("manual"), str(""), str("NFC"), nil}
	directions := []*string{str("in"), str("out"), nil}
	// Every value the Go vocabulary names (a constant added there is evaluated here too), and the
	// near misses the CHECK refuses.
	stored := []*string{nil, str("manager"), str(""), str("AUTO"), str(" employee")}
	for _, d := range tap.DirectionSources {
		stored = append(stored, str(string(d)))
	}
	show := func(p *string) string {
		if p == nil {
			return "NULL"
		}
		return fmt.Sprintf("%q", *p)
	}
	var out []string
	for _, c := range channels {
		for _, d := range directions {
			for _, s := range stored {
				var got *string
				if err := q.QueryRow(ctx, sql, c, d, s).Scan(&got); err != nil {
					t.Fatalf("evaluate %s: %v", sql, err)
				}
				channel := ""
				if c != nil {
					channel = *c
				}
				want, ok := tap.RecordedDirectionSource(tap.Channel(channel), (*tap.Type)(d), (*tap.DirectionSource)(s))
				switch {
				case ok && (got == nil || *got != string(want)):
					out = append(out, fmt.Sprintf("channel %s, direction %s, stored %s: SQL %s, Go %q", show(c), show(d), show(s), show(got), want))
				case !ok && got != nil:
					out = append(out, fmt.Sprintf("channel %s, direction %s, stored %s: SQL %s, Go no source", show(c), show(d), show(s), show(got)))
				}
			}
		}
	}
	return out
}

// TestPunchDirectionSource_TheFeedsExpressionIsTheReaderRule is M15-09's hand-over, item 1: the
// reader rule is spelled again in SQL, and this binds the SQL to tap.RecordedDirectionSource.
//   - punch_feed and holding_feed each assign v_dsrc exactly once, and by the SAME text (one rule,
//     two copies -- a change to one is red);
//   - that text, evaluated for every channel (nfc, qr, manual, the empty string, a capital, NULL) x
//     direction (in, out, NULL) x stored value (NULL, every tap.DirectionSources value, 'manager',
//     the empty string, a capital), answers what the Go helper answers: a directed nfc/qr row's NULL
//     is 'auto'; a manual row's value is never believed; an undirected row has no source; a value
//     the CHECK refuses is not guessed at.
//
// CONTROLS: three wrong rules -- one that trusts the manual row, one that leaves a directed row's
// NULL empty, one that passes any stored value -- are each caught by the same comparison.
func TestPunchDirectionSource_TheFeedsExpressionIsTheReaderRule(t *testing.T) {
	ctx, tx := opTx(t)
	exprs := pdsExpressions(t, ctx, tx)
	pf, hf := exprs["punch_feed()"], exprs["holding_feed()"]
	if len(pf) != 1 || len(hf) != 1 {
		t.Fatalf("punch_feed assigns v_dsrc %d times and holding_feed %d, want once each", len(pf), len(hf))
	}
	if pf[0] != hf[0] {
		t.Errorf("the two triggers spell the rule differently:\n punch_feed   %s\n holding_feed %s", pf[0], hf[0])
	}
	for _, f := range pdsParityFindings(t, ctx, tx, pf[0]) {
		t.Error(f)
	}
	for _, c := range []struct{ what, old, new string }{
		{"trusts a manual row", "WHEN v_channel IN ('nfc', 'qr') AND v_type IS NOT NULL", "WHEN v_type IS NOT NULL"},
		{"leaves a directed row's NULL empty", "WHEN v_stored IS NULL THEN 'auto' ", ""},
		{"passes any stored value", "WHEN v_stored IN ('auto', 'employee') THEN v_stored", "ELSE v_stored"},
	} {
		if !strings.Contains(pf[0], c.old) {
			t.Errorf("CONTROL %s: %q is not in the expression, so the mutation changes nothing", c.what, c.old)
			continue
		}
		if len(pdsParityFindings(t, ctx, tx, strings.Replace(pf[0], c.old, c.new, 1))) == 0 {
			t.Errorf("CONTROL %s: the parity comparison does not see it", c.what)
		}
	}
}

// TestPunchDirectionSource_BothFeedWritersCarryTheRowsSource is the card's "işveren ve saha satırında
// aynı değer; manuel satırlar akışa yine girmez (K-6)", through the triggers as tappa_app (each case
// in a savepoint of its own -- one counter set per transaction):
//   - an own-tenant ok tap (punch_feed): stored NULL -> 'auto' (the tap before M15-11), 'auto' ->
//     'auto', 'employee' -> 'employee', on nfc and on qr, in and out;
//   - an approved flag (punch_feed's approval arm, which READS the approved row): 'employee' ->
//     'employee';
//   - a holding punch (holding_feed): the employer's row {employer} AND the site's row {location}
//     carry the same value -- for NULL, for 'employee', and for an approved holding flag;
//   - a manual record carrying 'employee' (tappa_app can write it; no code path does) is no event.
func TestPunchDirectionSource_BothFeedWritersCarryTheRowsSource(t *testing.T) {
	ctx, tx := opTx(t)
	w := feedWorldTx(t, ctx, tx, "")
	kfReviewer := holdingNewAdmin(t, ctx, tx, w.kf)
	kmReviewer := holdingNewAdmin(t, ctx, tx, w.km)
	str := func(s string) *string { return &s }
	type tc struct {
		name   string
		write  func(sp pgx.Tx) uuid.UUID
		want   *string // nil: no event at all
		events int
	}
	own := func(channel, kind string, source *string) feedRow {
		r := w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, channel, "ok")
		r.kind, r.source = kind, source
		return r
	}
	holding := func(verdict string, source *string) feedRow {
		r := w.kmAtKF("nfc", verdict)
		r.source = source
		return r
	}
	approved := func(sp pgx.Tx, tenant, reviewer uuid.UUID, r feedRow) uuid.UUID {
		id := feedMust(t, ctx, sp, r)
		if n := len(feedEventsOf(t, ctx, sp, id)); n != 0 {
			t.Fatalf("a flag is %d events before its approval", n)
		}
		if err := feedReview(t, ctx, sp, tenant, reviewer, id, "approved"); err != nil {
			t.Fatalf("approve: %v", err)
		}
		return id
	}
	tests := []tc{
		{"own nfc in, stored NULL", func(sp pgx.Tx) uuid.UUID { return feedMust(t, ctx, sp, own("nfc", "in", nil)) }, str("auto"), 1},
		{"own qr out, stored NULL", func(sp pgx.Tx) uuid.UUID { return feedMust(t, ctx, sp, own("qr", "out", nil)) }, str("auto"), 1},
		{"own nfc out, stored auto", func(sp pgx.Tx) uuid.UUID { return feedMust(t, ctx, sp, own("nfc", "out", str("auto"))) }, str("auto"), 1},
		{"own nfc out, stored employee", func(sp pgx.Tx) uuid.UUID { return feedMust(t, ctx, sp, own("nfc", "out", str("employee"))) }, str("employee"), 1},
		{"own qr in, stored employee", func(sp pgx.Tx) uuid.UUID { return feedMust(t, ctx, sp, own("qr", "in", str("employee"))) }, str("employee"), 1},
		{"own approved flag, stored employee", func(sp pgx.Tx) uuid.UUID {
			r := w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "qr", "flag")
			r.kind, r.source = "out", str("employee")
			return approved(sp, w.kf, kfReviewer, r)
		}, str("employee"), 1},
		{"holding ok, stored NULL", func(sp pgx.Tx) uuid.UUID { return feedMust(t, ctx, sp, holding("ok", nil)) }, str("auto"), 2},
		{"holding ok, stored employee", func(sp pgx.Tx) uuid.UUID { return feedMust(t, ctx, sp, holding("ok", str("employee"))) }, str("employee"), 2},
		{"holding approved flag, stored employee", func(sp pgx.Tx) uuid.UUID {
			return approved(sp, w.km, kmReviewer, holding("flag", str("employee")))
		}, str("employee"), 2},
		{"own manual, stored employee", func(sp pgx.Tx) uuid.UUID {
			r := w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "manual", "ok")
			r.tag, r.source = nil, str("employee")
			return feedMust(t, ctx, sp, r)
		}, nil, 0},
	}
	for _, c := range tests {
		t.Run(c.name, func(t *testing.T) {
			holdingInSavepoint(t, tx, func(sp pgx.Tx) {
				id := c.write(sp)
				events := feedEventsOf(t, ctx, sp, id)
				if len(events) != c.events {
					t.Fatalf("%d events, want %d", len(events), c.events)
				}
				for _, e := range events {
					if (e.directionSource == nil) != (c.want == nil) || (e.directionSource != nil && *e.directionSource != *c.want) {
						t.Errorf("the %s row (tenant %s) carries %v, want %v", e.roles, e.tenant, feedStr(e.directionSource), feedStr(c.want))
					}
				}
				if c.events == 2 && (events[0].roles != "employer" || events[1].roles != "location" || events[1].tenant != w.kf) {
					t.Errorf("a holding punch's rows are %s@%s and %s@%s, want the employer's and the site's (KF)",
						events[0].roles, events[0].tenant, events[1].roles, events[1].tenant)
				}
			})
		})
	}
}

// pdsSurface is what 00067 adds or changes, read from the catalogue.
type pdsSurface struct {
	column, constraint, bodies, grants string
}

func pdsRead(t *testing.T, ctx context.Context, q opQuerier) pdsSurface {
	t.Helper()
	var s pdsSurface
	for dst, sql := range map[*string]string{
		&s.column: `
			SELECT coalesce((SELECT format_type(a.atttypid, a.atttypmod) || ' notnull=' || a.attnotnull
			                        || ' default=' || coalesce(pg_get_expr(ad.adbin, ad.adrelid), '-') || ' missing=' || a.atthasmissing
			                   FROM pg_attribute a LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
			                  WHERE a.attrelid = 'public.punch_events'::regclass AND a.attname = 'direction_source' AND NOT a.attisdropped), '')`,
		&s.constraint: `
			SELECT coalesce(string_agg(conname || ' validated=' || convalidated || ' ' || pg_get_constraintdef(oid), ' | ' ORDER BY conname), '')
			  FROM pg_constraint WHERE conrelid = 'public.punch_events'::regclass AND pg_get_constraintdef(oid) LIKE '%direction_source%'`,
		&s.bodies: `
			SELECT (SELECT md5(prosrc) FROM pg_proc WHERE oid = 'public.punch_feed()'::regprocedure) || '/' ||
			       (SELECT md5(prosrc) FROM pg_proc WHERE oid = 'public.holding_feed()'::regprocedure)`,
	} {
		if err := q.QueryRow(ctx, sql).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	for _, g := range []struct{ role, table, priv string }{
		{"tappa_feedwriter", "transactions", "SELECT"}, {"tappa_holdingdefiner", "transactions", "SELECT"},
		{"tappa_feedwriter", "punch_events", "INSERT"}, {"tappa_holdingdefiner", "punch_events", "INSERT"},
		{"tappa_app", "punch_events", "SELECT"},
	} {
		s.grants += fmt.Sprintf("%s %s %s: %s\n", g.role, g.priv, g.table, opColumns(t, ctx, q, g.role, g.table, g.priv))
	}
	return s
}

// TestPunchDirectionSource00067_DownRefusesAChoiceAndOtherwiseGivesBack is the card's "Down dolu
// değerde reddeder" (ADR 0032 "Geri alma"; the 00065 precedent):
//   - at HEAD the column is a nullable text with no default and no stored missing value, its CHECK is
//     NOT VALID (the ALTER scans nothing under its ACCESS EXCLUSIVE), and the two bodies are not
//     00049's;
//   - with an event carrying EITHER source -- each alone, the others set aside -- the Down is 55000
//     naming 00067 and changes nothing;
//   - with none it gives 00049's two bodies back byte for byte (md5), drops the column, its CHECK and
//     the five column grants, takes no strong lock outside punch_events, and a tap after it is still an
//     event; the Up takes HEAD's catalogue again.
func TestPunchDirectionSource00067_DownRefusesAChoiceAndOtherwiseGivesBack(t *testing.T) {
	ctx, tx := opTx(t)
	up, down := opMigrationSections(t, ds00067File)
	tables := downTables(down)
	slices.Sort(tables)
	tables = slices.Compact(tables)
	if strings.Join(tables, ",") != "punch_events" {
		t.Fatalf("PREMISE: 00067's Down names %v, want punch_events alone", tables)
	}
	lockForDowns(t, ctx, tx, tables)
	head := pdsRead(t, ctx, tx)
	const known = "punch_events_direction_source_is_known validated=false CHECK ((direction_source = ANY (ARRAY['auto'::text, 'employee'::text]))) NOT VALID"
	if head.column != "text notnull=false default=- missing=false" || head.constraint != known ||
		head.bodies == pdsPunchFeed49MD5+"/"+pdsHoldingFeed49MD5 || strings.Count(head.grants, "direction_source") != 5 {
		t.Fatalf("PREMISE at HEAD:\n %+v\nwant a nullable text with no default, %q, 00067's bodies and the five grants", head, known)
	}

	t.Run("an event with either source: the Down refuses and changes nothing", func(t *testing.T) {
		for _, source := range tap.DirectionSources {
			dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
				feedSetAsideDirectionSources(t, ctx, sp)
				w := feedWorldTx(t, ctx, sp, "")
				r := w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ok")
				v := string(source)
				r.source = &v
				feedMust(t, ctx, sp, r)
				if n := opInt(t, ctx, sp, `SELECT count(*) FROM punch_events WHERE direction_source IS NOT NULL`); n != 1 {
					t.Fatalf("PREMISE: %d events carry a source, want this one", n)
				}
				nested, err := sp.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				_, runErr := nested.Conn().PgConn().Exec(ctx, down).ReadAll()
				if err := nested.Rollback(ctx); err != nil {
					t.Fatal(err)
				}
				var pg *pgconn.PgError
				if !errors.As(runErr, &pg) || pg.Code != "55000" || !strings.Contains(pg.Message, "migration 00067 Down") {
					t.Errorf("00067's Down over an event with %q answered %v, want 55000 naming 00067", source, runErr)
				}
				if after := pdsRead(t, ctx, sp); after != head {
					t.Errorf("the refused Down changed the catalogue:\n %+v\nwant HEAD's\n %+v", after, head)
				}
			})
		}
	})

	t.Run("no source: the Down gives 00049's bodies back and the Up takes HEAD again", func(t *testing.T) {
		dsSavepoint(t, ctx, tx, func(sp pgx.Tx) {
			feedSetAsideDirectionSources(t, ctx, sp)
			opRunSection(t, ctx, sp, down, "00067 Down")
			at66 := pdsRead(t, ctx, sp)
			want := pdsSurface{bodies: pdsPunchFeed49MD5 + "/" + pdsHoldingFeed49MD5}
			if at66.column != "" || at66.constraint != "" || at66.bodies != want.bodies || strings.Contains(at66.grants, "direction_source") {
				t.Errorf("after 00067's Down: %+v\nwant no column, no CHECK, 00049's bodies (%s) and no direction_source grant", at66, want.bodies)
			}
			if extra := strongTableLocksOutside(t, ctx, sp, tables); len(extra) > 0 {
				t.Errorf("the Down took a strong lock on %v, outside the table it names", extra)
			}
			dsSavepoint(t, ctx, sp, func(tsp pgx.Tx) {
				w := feedWorldTx(t, ctx, tsp, "")
				id := feedMust(t, ctx, tsp, w.own(w.kf, w.kfEmployee, w.kfVenue, w.kfTag, "nfc", "ok"))
				if n := opInt(t, ctx, tsp, `SELECT count(*) FROM punch_events WHERE punch_id = $1`, id); n != 1 {
					t.Errorf("with 00067 down an ok tap is %d events, want 1 (00049's punch_feed)", n)
				}
			})
			opRunSection(t, ctx, sp, up, "00067 Up again")
			if again := pdsRead(t, ctx, sp); again != head {
				t.Errorf("after 00067's Up again:\n %+v\nwant HEAD's\n %+v", again, head)
			}
		})
	})
}
