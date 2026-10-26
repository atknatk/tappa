package ledger

// inmissing_db_test.go -- M15-06 against real Postgres (CLAUDE.md §8): a checkout with no open
// check-in for it to close -- ADR 0032 §A.6's (b) and (c) -- is counted once by the reports
// section (Totals.OutWithoutIn, or StartedEarlier at the period's seam) and listed by Odd shapes'
// "check-in missing" list (ListCheckoutsWithoutCheckIn); a correction's surplus (e), a row with no
// business day (d) and a too-long pair (a) are on neither.
//
// EVERY TEST BUILDS ITS OWN TENANT (cpNewWorld, checkout_parity_db_test.go) and never touches the
// seed tenant. A row that comes LATER in a case -- the break's chosen checkout, the backdated
// claim, a manager's correction, Phase C's approved checkout -- is written either through
// manual.Recorder or with created_at left to its DEFAULT (cpRow.serverWrite), never with a
// created_at in the past: W is "written later" by the database's clock, and a fixture that wrote
// it would build W wrong (the card's trap; the engine lens's N3).

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/audit"
	manualentry "github.com/atknatk/tappa/internal/domain/manual"
	"github.com/atknatk/tappa/internal/store"
)

// inMissingRow is one row of the "check-in missing" list, by person name and instant.
type inMissingRow struct {
	name            string
	at              time.Time
	afterEarlierDay bool
}

// inMissingListed is ListCheckoutsWithoutCheckIn over a window, in the tenant's own context.
func (w *cpWorld) inMissingListed(from, to time.Time) []inMissingRow {
	w.t.Helper()
	var out []inMissingRow
	if err := w.data.WithTenant(w.ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := store.New(tx).ListCheckoutsWithoutCheckIn(ctx, store.ListCheckoutsWithoutCheckInParams{
			TenantID: w.tenant, FromAt: from, ToAt: to, RowLimit: 1000})
		for _, r := range rows {
			out = append(out, inMissingRow{name: deref(r.EmployeeName), at: r.OccurredAt.UTC(), afterEarlierDay: r.AfterEarlierDay})
		}
		return err
	}); err != nil {
		w.t.Fatalf("list checkouts without a check-in: %v", err)
	}
	return out
}

// typeOut writes a manager's checkout the way the product does (manual.Recorder): no business day,
// created_at the database's now().
func (w *cpWorld) typeOut(person uuid.UUID, at time.Time) {
	w.t.Helper()
	trail, err := audit.New(w.data)
	if err != nil {
		w.t.Fatal(err)
	}
	rec, err := manualentry.NewRecorder(w.data, trail, slog.New(slog.DiscardHandler))
	if err != nil {
		w.t.Fatal(err)
	}
	if _, err := rec.Record(w.ctx, manualentry.Entry{TenantID: w.tenant, EmployeeID: person, EnteredBy: w.admin,
		Direction: manualentry.Out, At: at}); err != nil {
		w.t.Fatalf("type the checkout: %v", err)
	}
}

// TestCheckoutsWithoutCheckIn_EachShapeIsCountedOnceAndListedOnlyWhenACheckInIsMissing is the
// card's criteria (M15-06), each in a tenant of its own, through Hours (the reports section) and
// ListCheckoutsWithoutCheckIn (Odd shapes):
//
//	(b)                 Monday 15:00 in, Tuesday 07:00 out (S Tuesday 05:00): OutWithoutIn 1,
//	                    TooLong 0, StartedEarlier 0; Monday on the open list, Tuesday 07:00 listed
//	(a AND b)           24 h with S set -> (b), not TooLong
//	(c), S in the week  OutWithoutIn 1, listed
//	(c), S before it    StartedEarlier 1 on the report (its seam); listed (a row question)
//	(e)                 the manager's correction through manual.Recorder: StartedEarlier, not listed
//	(a) historical      S NULL, 24 h: TooLong 1, not listed
//	(d)                 a typed checkout alone: StartedEarlier 1, not listed
//	the break           09:00 in, 12:00 out, 17:00 out written after it: (c) -- OutWithoutIn 1,
//	                    StartedEarlier 0, 17:00 listed
//	the backdated claim the break + an out written last claiming 16:59:59: 17:00 (e), 16:59:59 (c)
//	Phase C's own flow  Monday 15:00 in, Tuesday 07:00 out, Monday's 23:00 typed later: Tuesday
//	                    07:00 still (c) -- no check-in on its day
func TestCheckoutsWithoutCheckIn_EachShapeIsCountedOnceAndListedOnlyWhenACheckInIsMissing(t *testing.T) {
	type want struct {
		worked                                       time.Duration
		outWithoutIn, startedEarlier, tooLong, opens int
		listed                                       []time.Time
		afterEarlierDay                              []bool
	}
	cases := []struct {
		name  string
		build func(w *cpWorld, person uuid.UUID)
		want  func(w *cpWorld) want
	}{
		{"(b) Monday 15:00 in, Tuesday 07:00 out", func(w *cpWorld, p uuid.UUID) {
			in, out := w.day(0, 15, 0), w.day(1, 7, 0)
			w.chain(p, []cpRow{{dir: "in", at: in, day: w.s(in), serverWrite: true}, {dir: "out", at: out, day: w.s(out), serverWrite: true}})
		}, func(w *cpWorld) want {
			return want{outWithoutIn: 1, opens: 1, listed: []time.Time{w.day(1, 7, 0)}, afterEarlierDay: []bool{true}}
		}},
		{"(a AND b) 24 h with a business day is (b)", func(w *cpWorld, p uuid.UUID) {
			in, out := w.day(0, 7, 0), w.day(1, 7, 0)
			w.chain(p, []cpRow{{dir: "in", at: in, day: w.s(in), serverWrite: true}, {dir: "out", at: out, day: w.s(out), serverWrite: true}})
		}, func(w *cpWorld) want {
			return want{outWithoutIn: 1, opens: 1, listed: []time.Time{w.day(1, 7, 0)}, afterEarlierDay: []bool{true}}
		}},
		{"(c) its business day inside the week", func(w *cpWorld, p uuid.UUID) {
			out := w.day(1, 10, 0)
			w.chain(p, []cpRow{{dir: "out", at: out, day: w.s(out), serverWrite: true}})
		}, func(w *cpWorld) want {
			return want{outWithoutIn: 1, listed: []time.Time{w.day(1, 10, 0)}, afterEarlierDay: []bool{false}}
		}},
		{"(c) its business day before the week: the report's seam", func(w *cpWorld, p uuid.UUID) {
			out := w.day(0, 3, 0) // Monday 03:00, its business day Sunday 05:00
			w.chain(p, []cpRow{{dir: "out", at: out, day: w.s(out), serverWrite: true}})
		}, func(w *cpWorld) want {
			return want{startedEarlier: 1, listed: []time.Time{w.day(0, 3, 0)}, afterEarlierDay: []bool{false}}
		}},
		{"(e) the manager's correction", func(w *cpWorld, p uuid.UUID) {
			in, out := w.day(0, 9, 0), w.day(0, 20, 0)
			w.chain(p, []cpRow{{dir: "in", at: in, day: w.s(in), serverWrite: true}, {dir: "out", at: out, day: w.s(out), serverWrite: true}})
			w.typeOut(p, w.day(0, 17, 0))
		}, func(w *cpWorld) want { return want{worked: 8 * time.Hour, startedEarlier: 1} }},
		{"(e) with the check-in exactly at the business day's start (I's bound is inclusive)", func(w *cpWorld, p uuid.UUID) {
			// Review DÜŞÜK-3 m7: Monday 05:00 in -- exactly S under the 05:00 cutoff --, 20:00 out,
			// the manager's 17:00 typed later. 20:00's P is that 17:00 checkout (W) and its business
			// day holds the 05:00 check-in (I: occurred_at >= S, inclusive) -> (e), not listed.
			in, out := w.day(0, 5, 0), w.day(0, 20, 0)
			if !w.s(in).Equal(in) {
				w.t.Fatalf("PREMISE: Monday 05:00 is not its business day's start (%v)", w.s(in))
			}
			w.chain(p, []cpRow{{dir: "in", at: in, day: w.s(in), serverWrite: true}, {dir: "out", at: out, day: w.s(out), serverWrite: true}})
			w.typeOut(p, w.day(0, 17, 0))
		}, func(w *cpWorld) want { return want{worked: 12 * time.Hour, startedEarlier: 1} }},
		{"the window's bounds: an out at the week's first instant is listed, one at its end is not", func(w *cpWorld, p uuid.UUID) {
			// Review DÜŞÜK-3 m2, m9: From is inclusive and To exclusive, on the list as on the
			// report. Monday 00:00 out alone (its business day Sunday 05:00, the week before: the
			// report's seam) and next Monday 00:00 out (next week's).
			first, end := w.week.From, w.week.To
			w.chain(p, []cpRow{{dir: "out", at: first, day: w.s(first), serverWrite: true}, {dir: "out", at: end, day: w.s(end), serverWrite: true}})
		}, func(w *cpWorld) want {
			return want{startedEarlier: 1, listed: []time.Time{w.week.From.UTC()}, afterEarlierDay: []bool{false}}
		}},
		{"(a) the historical stale shape, S NULL", func(w *cpWorld, p uuid.UUID) {
			w.chain(p, []cpRow{{dir: "in", at: w.day(0, 9, 0), serverWrite: true}, {dir: "out", at: w.day(1, 9, 5), serverWrite: true}})
		}, func(w *cpWorld) want { return want{tooLong: 1, opens: 1} }},
		{"(d) a typed checkout alone", func(w *cpWorld, p uuid.UUID) {
			w.typeOut(p, w.day(0, 17, 0))
		}, func(w *cpWorld) want { return want{startedEarlier: 1} }},
		{"the break: 09 in, 12 out, 17 out written after it", func(w *cpWorld, p uuid.UUID) {
			var rows []cpRow
			for _, r := range []struct {
				dir    string
				hh, mm int
			}{{"in", 9, 0}, {"out", 12, 0}, {"out", 17, 0}} {
				at := w.day(0, r.hh, r.mm)
				rows = append(rows, cpRow{dir: r.dir, at: at, day: w.s(at), serverWrite: true})
			}
			w.chain(p, rows)
		}, func(w *cpWorld) want {
			return want{worked: 3 * time.Hour, outWithoutIn: 1, listed: []time.Time{w.day(0, 17, 0)}, afterEarlierDay: []bool{false}}
		}},
		{"the break moved by a backdated claim: + an out written last claiming 16:59:59", func(w *cpWorld, p uuid.UUID) {
			var rows []cpRow
			for _, at := range []time.Time{w.day(0, 9, 0), w.day(0, 12, 0), w.day(0, 17, 0), w.day(0, 16, 59).Add(59 * time.Second)} {
				dir := "out"
				if at.Equal(w.day(0, 9, 0)) {
					dir = "in"
				}
				rows = append(rows, cpRow{dir: dir, at: at, day: w.s(at), serverWrite: true})
			}
			w.chain(p, rows)
		}, func(w *cpWorld) want {
			return want{worked: 3 * time.Hour, outWithoutIn: 1, startedEarlier: 1,
				listed: []time.Time{w.day(0, 16, 59).Add(59 * time.Second)}, afterEarlierDay: []bool{false}}
		}},
		{"Phase C's own flow: Monday's checkout approved later", func(w *cpWorld, p uuid.UUID) {
			in, out := w.day(0, 15, 0), w.day(1, 7, 0)
			w.chain(p, []cpRow{{dir: "in", at: in, day: w.s(in), serverWrite: true}, {dir: "out", at: out, day: w.s(out), serverWrite: true}})
			w.typeOut(p, w.day(0, 23, 0))
		}, func(w *cpWorld) want {
			return want{worked: 8 * time.Hour, outWithoutIn: 1, listed: []time.Time{w.day(1, 7, 0)}, afterEarlierDay: []bool{false}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := cpNewWorld(t)
			person := w.employee("Missing")
			tc.build(w, person)
			wt := tc.want(w)
			rep, err := w.r.Hours(w.ctx, w.tenant, ReportFilter{Day: w.week.FirstDay})
			if err != nil {
				t.Fatal(err)
			}
			if rep.Totals.Worked != wt.worked || rep.Totals.OutWithoutIn != wt.outWithoutIn || rep.StartedEarlier != wt.startedEarlier ||
				rep.Totals.TooLong != wt.tooLong || rep.Totals.Open != wt.opens {
				t.Errorf("Worked %v OutWithoutIn %d StartedEarlier %d TooLong %d Open %d; want %v %d %d %d %d",
					rep.Totals.Worked, rep.Totals.OutWithoutIn, rep.StartedEarlier, rep.Totals.TooLong, rep.Totals.Open,
					wt.worked, wt.outWithoutIn, wt.startedEarlier, wt.tooLong, wt.opens)
			}
			if l := len(w.openListed(w.week.From, w.week.To)); l != wt.opens {
				t.Errorf("open list holds %d, want %d (the report's Open)", l, wt.opens)
			}
			got := w.inMissingListed(w.week.From, w.week.To)
			var at []time.Time
			var after []bool
			for _, r := range got {
				at = append(at, r.at)
				after = append(after, r.afterEarlierDay)
			}
			if len(at) != len(wt.listed) {
				t.Fatalf("check-in missing lists %v, want %v", at, wt.listed)
			}
			for i := range at {
				if !at[i].Equal(wt.listed[i]) || after[i] != wt.afterEarlierDay[i] {
					t.Errorf("check-in missing row %d: %v (after an earlier day %v), want %v (%v)", i, at[i], after[i], wt.listed[i], wt.afterEarlierDay[i])
				}
			}
		})
	}
}

// TestCheckoutsWithoutCheckIn_TheListIsTheClassificationsBAndC: over every chain of the parity
// test (all six classes, the bounds, the ties), a checkout of the week is on the list exactly when
// tappa_checkout_class -- fed P, I and W the way the parity test feeds it -- says other_day or
// orphan. So the list's own P/I/W subqueries are the classification's, not a second reading of it.
func TestCheckoutsWithoutCheckIn_TheListIsTheClassificationsBAndC(t *testing.T) {
	w := cpNewWorld(t)
	want := map[string]bool{}
	classes := map[string]int{}
	for _, tc := range w.cpCases() {
		person := w.employee("Class")
		w.chain(person, tc.rows)
		for _, c := range w.sqlClasses(person) {
			if c.At.Before(w.week.From) || !c.At.Before(w.week.To) {
				continue
			}
			classes[c.Class]++
			if c.Class == "other_day" || c.Class == "orphan" {
				want[person.String()[:6]+" @ "+c.At.Format(time.RFC3339Nano)] = true
			}
		}
	}
	got := map[string]bool{}
	for _, r := range w.inMissingListed(w.week.From, w.week.To) {
		key := r.name[strings.LastIndex(r.name, " ")+1:] + " @ " + r.at.Format(time.RFC3339Nano)
		if got[key] {
			t.Errorf("listed twice: %s", key)
		}
		got[key] = true
	}
	for k := range want {
		if !got[k] {
			t.Errorf("(b) or (c) but not listed: %s", k)
		}
	}
	for k := range got {
		if !want[k] {
			t.Errorf("listed but neither (b) nor (c): %s", k)
		}
	}
	for _, c := range []string{"closes", "too_long", "other_day", "orphan", "extra", "unanchored"} {
		if classes[c] == 0 {
			t.Errorf("PREMISE: no checkout of the week is %q (%v)", c, classes)
		}
	}
}

// TestRLS_CheckoutsWithoutCheckIn_AnotherTenantsAreNotListed: B has a checkout with no open check-in to close.
// Asked in A's context FOR B (the explicit filter admits B's row), the list is empty -- RLS's
// doing, not the WHERE's. Positive control: B's context lists it. And the belt: in B's context
// asking for A, the explicit tenant filter alone leaves B's row out.
func TestRLS_CheckoutsWithoutCheckIn_AnotherTenantsAreNotListed(t *testing.T) {
	a, b := cpNewWorld(t), cpNewWorld(t)
	out := b.day(1, 10, 0)
	b.chain(b.employee("Theirs"), []cpRow{{dir: "out", at: out, day: b.s(out), serverWrite: true}})
	list := func(ctxTenant, asked uuid.UUID) int {
		var n int
		if err := a.data.WithTenant(a.ctx, ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			rows, err := store.New(tx).ListCheckoutsWithoutCheckIn(ctx, store.ListCheckoutsWithoutCheckInParams{
				TenantID: asked, FromAt: b.week.From, ToAt: b.week.To, RowLimit: 100})
			n = len(rows)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := list(b.tenant, b.tenant); n != 1 {
		t.Fatalf("positive control: B's context lists %d of B's checkouts, want 1", n)
	}
	if n := list(a.tenant, b.tenant); n != 0 {
		t.Errorf("RLS: A's context asking for B lists %d of B's checkouts", n)
	}
	if n := list(b.tenant, a.tenant); n != 0 {
		t.Errorf("explicit filter: B's context asking for A lists %d of B's checkouts", n)
	}
}

// TestCheckoutsWithoutCheckIn_ACheckoutNamingNoEmployeeCannotBeWritten (review DÜŞÜK-3 m4): the
// list's `o.employee_id IS NOT NULL` cannot be shown to matter by a row, because no such row can
// exist -- measured: a checkout naming no employee is refused under every verdict by two validated
// CHECKs of migrations 00005 and 00014 (transactions_decided_has_employee: an `ok` or `flag` row
// names an employee; transactions_refusal_has_no_direction: a `reject` or `ignored` row has no
// direction). So the predicate is a belt over the schema, and what is pinned here is the schema
// half: if either CHECK is ever loosened this goes red, and the predicate becomes the one thing
// keeping a chainless checkout off the list. Positive control: a named orphan beside them is listed.
func TestCheckoutsWithoutCheckIn_ACheckoutNamingNoEmployeeCannotBeWritten(t *testing.T) {
	w := cpNewWorld(t)
	at := w.day(1, 10, 0)
	for _, verdict := range []string{"ok", "flag", "reject", "ignored"} {
		tx, err := w.owner.Begin(w.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(w.ctx, `SELECT set_config('app.tenant_id', $1, true)`, w.tenant.String()); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(w.ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, verdict, channel, type, business_day_start)
		                         VALUES ($1, NULL, $2, $3, $4, 'nfc', 'out', $5)`, w.tenant, w.venue, at, verdict, w.s(at))
		_ = tx.Rollback(w.ctx)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("verdict %s: a checkout naming no employee was not refused by a CHECK (err %v)", verdict, err)
		}
	}
	named := w.employee("Named")
	w.chain(named, []cpRow{{dir: "out", at: at, day: w.s(at), serverWrite: true}})
	if got := w.inMissingListed(w.week.From, w.week.To); len(got) != 1 || !strings.HasPrefix(got[0].name, "Named ") {
		t.Errorf("listed %+v, want the named employee's checkout alone", got)
	}
}
