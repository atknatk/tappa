package billing

// billing_test.go -- the arithmetic this package actually owns.
//
// ⚠️ WHAT IS DELIBERATELY NOT TESTED HERE, because it is deliberately not written
// here: the billable definition, the month's UTC boundaries, the founding-offer
// window and the multiplication. All four live in migration 0016's SQL functions so
// that the live preview and the statement that freezes cannot disagree, and a Go
// re-implementation to test them against would be the second definition this package
// exists to avoid. They are tested against real Postgres in billing_db_test.go,
// where the expected instants are written out by hand rather than derived.

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
)

// The two stubs exist ONLY for NewBook's refusal test. Nothing else in this file
// reaches a database; everything that does is in billing_db_test.go against real
// Postgres, because a fake cannot test RLS, an append-only trigger or a month
// boundary resolved by tzdata.
type stubDatabase struct{}

func (stubDatabase) WithTenant(context.Context, uuid.UUID, db.TxFunc) error { return nil }

type stubTrail struct{}

func (stubTrail) RecordTx(context.Context, pgx.Tx, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, nil
}

func TestMonth_ParseAndRender(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Month
		bad  bool
	}{
		{name: "an ordinary month", in: "2026-08", want: Month{2026, time.August}},
		{name: "January", in: "2026-01", want: Month{2026, time.January}},
		{name: "December", in: "2026-12", want: Month{2026, time.December}},
		{name: "empty means no month was asked for", in: "", want: Month{}},
		{name: "a day is not a month", in: "2026-08-01", bad: true},
		{name: "month 13", in: "2026-13", bad: true},
		{name: "prose", in: "August", bad: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ParseMonth(c.in)
			if c.bad {
				if err == nil {
					t.Fatalf("ParseMonth(%q) accepted it as %v", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseMonth(%q): %v", c.in, err)
			}
			if got != c.want {
				t.Fatalf("ParseMonth(%q) = %v, want %v", c.in, got, c.want)
			}
			// Round trip: what String renders, ParseMonth reads back.
			if !c.want.Zero() {
				if s := got.String(); s != c.in {
					t.Fatalf("String() = %q, want %q", s, c.in)
				}
			} else if got.String() != "" {
				t.Fatalf("the zero Month renders as %q, want the empty string", got.String())
			}
		})
	}
}

func TestMonth_AddRollsTheYear(t *testing.T) {
	cases := []struct {
		name string
		in   Month
		n    int
		want Month
	}{
		{"forward inside a year", Month{2026, time.May}, 3, Month{2026, time.August}},
		{"forward over December", Month{2026, time.November}, 3, Month{2027, time.February}},
		{"back over January", Month{2026, time.January}, -1, Month{2025, time.December}},
		{"a whole year", Month{2026, time.August}, 12, Month{2027, time.August}},
		{"nowhere", Month{2026, time.August}, 0, Month{2026, time.August}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.in.Add(c.n); got != c.want {
				t.Fatalf("%v.Add(%d) = %v, want %v", c.in, c.n, got, c.want)
			}
		})
	}
}

func TestMonth_BeforeOrdersAcrossYears(t *testing.T) {
	cases := []struct {
		name string
		a, b Month
		want bool
	}{
		{"same year, earlier month", Month{2026, time.July}, Month{2026, time.August}, true},
		{"same year, later month", Month{2026, time.August}, Month{2026, time.July}, false},
		{"the same month", Month{2026, time.August}, Month{2026, time.August}, false},
		{"December before the next January", Month{2025, time.December}, Month{2026, time.January}, true},
		{"a later year with an earlier month is NOT before", Month{2027, time.January}, Month{2026, time.December}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.a.Before(c.b); got != c.want {
				t.Fatalf("%v.Before(%v) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}

// TestMonthOf_IsTheTenantsMonthNotUTCs.
//
// 🔴 THE TWO ANSWERS DIFFER FOR TWO HOURS OF EVERY MONTH, which is the same seam §6
// names for the overnight shift. The instants below are the last hour of a Maltese
// month in summer (UTC+2) and in winter (UTC+1); read as UTC they fall in the month
// BEFORE. Getting this wrong would put a night shift's month -- and so somebody's
// invoice -- on the wrong side of a boundary.
func TestMonthOf_IsTheTenantsMonthNotUTCs(t *testing.T) {
	malta, err := time.LoadLocation("Europe/Malta")
	if err != nil {
		t.Skipf("Europe/Malta unavailable in this runtime: %v", err)
	}
	cases := []struct {
		name      string
		utc       time.Time
		wantMalta Month
		wantUTC   Month
	}{
		{
			name:      "summer: 23:30 UTC on 31 July is already August in Malta",
			utc:       time.Date(2026, time.July, 31, 23, 30, 0, 0, time.UTC),
			wantMalta: Month{2026, time.August},
			wantUTC:   Month{2026, time.July},
		},
		{
			name:      "winter: 23:30 UTC on 31 December is already January in Malta",
			utc:       time.Date(2026, time.December, 31, 23, 30, 0, 0, time.UTC),
			wantMalta: Month{2027, time.January},
			wantUTC:   Month{2026, time.December},
		},
		{
			name:      "mid-month, where the two agree",
			utc:       time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC),
			wantMalta: Month{2026, time.August},
			wantUTC:   Month{2026, time.August},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MonthOf(c.utc, malta); got != c.wantMalta {
				t.Errorf("MonthOf(%s, Europe/Malta) = %v, want %v", c.utc, got, c.wantMalta)
			}
			if got := MonthOf(c.utc, time.UTC); got != c.wantUTC {
				t.Errorf("MonthOf(%s, UTC) = %v, want %v", c.utc, got, c.wantUTC)
			}
		})
	}
}

// TestMonth_DateRoundTrip -- the pgtype.Date a query takes and the Month it comes
// back as are the same month, and an invalid date does not become a plausible one.
func TestMonth_DateRoundTrip(t *testing.T) {
	for _, m := range []Month{{2026, time.January}, {2026, time.August}, {2026, time.December}, {1999, time.February}} {
		d := m.date()
		if !d.Valid {
			t.Fatalf("%v.date() is not valid", m)
		}
		if d.Time.Day() != 1 {
			t.Fatalf("%v.date() is day %d; the schema CHECK requires the 1st", m, d.Time.Day())
		}
		if got := monthFrom(d); got != m {
			t.Fatalf("round trip of %v gave %v", m, got)
		}
	}
	if got := monthFrom(pgtype.Date{Valid: false}); !got.Zero() {
		t.Fatalf("an invalid date became %v; the zero Month renders as \"\" and a wrong "+
			"month renders as a real one", got)
	}
}

// TestReadLimit_MakesTruncationDetectable.
//
// 🔴 THE LIMIT MUST EXCEED THE CAP OR THE VERDICT IS DEAD CODE -- if the query asked
// for exactly HistoryCap rows, len(rows) could never exceed it, truncatedBy would be
// false forever and a history that had silently ended early would present itself as
// complete. The identical pair and the identical argument are in
// internal/domain/ledger/report.go; this test is measured going red on `>=` and on a
// limit equal to the cap.
func TestReadLimit_MakesTruncationDetectable(t *testing.T) {
	if int(readLimit()) <= HistoryCap {
		t.Fatalf("readLimit() = %d and the cap is %d: the query can never return more rows "+
			"than can be used, so truncation is undetectable", readLimit(), HistoryCap)
	}
	cases := []struct {
		name string
		rows int
		want bool
	}{
		{"nothing at all", 0, false},
		{"one short of the cap", HistoryCap - 1, false},
		{"exactly the cap is a COMPLETE read", HistoryCap, false},
		{"one more than the cap is the first row that did not fit", HistoryCap + 1, true},
		{"everything the limit allows", int(readLimit()), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := truncatedBy(c.rows); got != c.want {
				t.Fatalf("truncatedBy(%d) = %v, want %v", c.rows, got, c.want)
			}
		})
	}
}

func TestNewBook_RefusesToBeUnusable(t *testing.T) {
	if _, err := NewBook(nil, nil, nil); err == nil {
		t.Error("a Book with no database was constructed")
	}
	if _, err := NewBook(stubDatabase{}, nil, nil); err == nil {
		t.Error("a Book with no audit trail was constructed; closing a period without a " +
			"trail is the untraceable act §4.3 forbids")
	}
	if _, err := NewBook(stubDatabase{}, stubTrail{}, nil); err != nil {
		t.Errorf("a Book with a nil logger was refused: %v (the logger defaults)", err)
	}
}
