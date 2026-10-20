package ledger

// tally_db_test.go -- M12-16: the day's verdict counts against real Postgres. The count is only
// worth printing if it is the list's count, so the central test walks the list itself.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/store"
)

// listed walks every page the list offers for f and counts the dockets.
func listed(t *testing.T, r *Reader, tenantID uuid.UUID, f Filter) int {
	t.Helper()
	n, pages := 0, 0
	for {
		pages++
		page, err := r.Day(context.Background(), tenantID, f)
		if err != nil {
			t.Fatal(err)
		}
		n += len(page.Records)
		if page.Next == nil || pages > 20 {
			return n
		}
		f.After = page.Next
	}
}

// TestTally_CountsWhatTheListShows: for a day with more records than a page holds -- every verdict,
// every channel, two venues, a department, a person who has LEFT, and records on the days either
// side -- the tally under each filter equals the dockets the list shows across all its pages, and
// each verdict's figure equals the list with that verdict chosen.
func TestTally_CountsWhatTheListShows(t *testing.T) {
	data := passDB(t)
	b := newPassTenant(t, data, "Tally", "Bar", "Kitchen")
	dept := uuid.New()
	b.exec(`INSERT INTO departments (id, tenant_id, location_id, name) VALUES ($1, $2, $3, 'Grill')`, dept, b.id, b.venues["Kitchen"])
	staying := b.person("Staying Person", "Bar")
	left := b.person("Gone Person", "Kitchen")
	b.exec(`UPDATE employees SET status = 'deactivated' WHERE id = $1`, left)

	at := func(clock string) time.Time { return hLocal(t, "2026-09-02", clock) }
	verdicts := []string{"ok", "flag", "reject", "ignored"}
	channels := []string{"nfc", "qr", "manual"}
	for i := 0; i < 34; i++ {
		who, venue := staying, "Bar"
		if i%3 == 0 {
			who, venue = left, "Kitchen"
		}
		v := verdicts[i%len(verdicts)]
		kind := ""
		if v == "ok" || v == "flag" {
			kind = "in"
		}
		var department any
		if venue == "Kitchen" {
			department = dept
		}
		b.exec(`INSERT INTO transactions (tenant_id, employee_id, location_id, department_id, occurred_at, type, verdict, channel)
		        VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), $7, $8)`,
			b.id, who, b.venues[venue], department, at(fmt.Sprintf("%02d:%02d:00", 1+i/3, (i*7)%60)), kind, v, channels[i%len(channels)])
	}
	// The days either side: not in the count.
	b.tap(passTap{who: staying, venue: "Bar", at: hLocal(t, "2026-09-01", "23:59:00"), kind: "in"})
	b.tap(passTap{who: staying, venue: "Bar", at: hLocal(t, "2026-09-03", "00:00:00"), kind: "in"})

	r := readerAt(t, data, at("23:00:00"), nil)
	day := Date{Year: 2026, Month: time.September, Day: 2}
	kitchen, bar := b.venues["Kitchen"], b.venues["Bar"]
	for _, c := range []struct {
		name string
		f    Filter
	}{
		{"no filter", Filter{Date: day}},
		{"a venue", Filter{Date: day, LocationID: &bar}},
		{"the other venue", Filter{Date: day, LocationID: &kitchen}},
		{"a department", Filter{Date: day, DepartmentID: &dept}},
		{"a person who has left, by name", Filter{Date: day, EmployeeName: "gone"}},
		{"manual entries", Filter{Date: day, Channel: "manual"}},
		{"QR and a venue", Filter{Date: day, Channel: "qr", LocationID: &kitchen}},
		{"a verdict chosen: the tally ignores it", Filter{Date: day, Verdict: "flag"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, err := r.Screen(context.Background(), b.id, c.f)
			if err != nil {
				t.Fatal(err)
			}
			got := s.Tally
			if got.State != Measured {
				t.Fatalf("the tally is %v, want Measured", got.State)
			}
			all := c.f
			all.Verdict = ""
			if want := listed(t, r, b.id, all); got.Records != want {
				t.Errorf("Records = %d, the list shows %d", got.Records, want)
			}
			for v, n := range map[string]int{"ok": got.Approved, "flag": got.Flagged, "reject": got.Rejected, "ignored": got.Ignored} {
				one := all
				one.Verdict = v
				if want := listed(t, r, b.id, one); n != want {
					t.Errorf("%s = %d, the list with that verdict shows %d", v, n, want)
				}
			}
			if sum := got.Approved + got.Flagged + got.Rejected + got.Ignored; sum != got.Records {
				t.Errorf("the verdicts sum to %d, Records is %d", sum, got.Records)
			}
			manual := all
			manual.Channel = "manual"
			if all.Channel == "" || all.Channel == "manual" {
				if want := listed(t, r, b.id, manual); got.Manual != want {
					t.Errorf("Manual = %d, the list of manual entries shows %d", got.Manual, want)
				}
			} else if got.Manual != 0 {
				t.Errorf("Manual = %d under a %s-only filter", got.Manual, all.Channel)
			}
		})
	}
	// Anti-vacuity: the fixture spans more than a page, and the leaver's records are found.
	s, err := r.Screen(context.Background(), b.id, Filter{Date: day})
	if err != nil {
		t.Fatal(err)
	}
	if s.Tally.Records != 34 || s.Tally.Records <= PageSize {
		t.Fatalf("the day counts %d records, want the fixture's 34 (more than a page of %d)", s.Tally.Records, PageSize)
	}
	gone, err := r.Screen(context.Background(), b.id, Filter{Date: day, EmployeeName: "Gone"})
	if err != nil {
		t.Fatal(err)
	}
	if gone.Tally.Records != 12 {
		t.Errorf("the person who has left has %d records counted, want 12 -- the name CTE must not filter on status", gone.Tally.Records)
	}
}

// TestTally_TheTwoStatementsShareTheirPredicates is the structural half: every predicate line of
// ListPanelTransactions' WHERE is in CountPanelTransactionsByVerdict's, except the verdict and the
// cursor, the count adds none, and the name CTE is the same text -- so a filter added to the list and not to the count (or the
// other way) is red before any fixture has to exercise it.
func TestTally_TheTwoStatementsShareTheirPredicates(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "db", "queries", "transactions.sql"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(raw)
	code := func(name string) string {
		i := strings.Index(src, "-- name: "+name+" ")
		if i < 0 {
			t.Fatalf("%s is not in transactions.sql", name)
		}
		body := src[i:]
		if j := strings.Index(body[1:], "-- name: "); j >= 0 {
			body = body[:j+1]
		}
		var code []string
		for _, line := range strings.Split(body, "\n") {
			if l := strings.TrimSpace(line); l != "" && !strings.HasPrefix(l, "--") {
				code = append(code, l)
			}
		}
		return strings.Join(code, " ")
	}
	where := func(name string) []string {
		stmt := code(name)
		w := regexp.MustCompile(`(?s)FROM transactions t .*?WHERE (.*?)(?:ORDER BY|;)`).FindStringSubmatch(stmt)
		if w == nil {
			t.Fatalf("%s: no WHERE over transactions t", name)
		}
		var preds []string
		for _, p := range regexp.MustCompile(`\s+AND\s+(?:\(|t\.)`).Split(" AND "+w[1], -1) {
			if p = strings.Join(strings.Fields(p), " "); p != "" {
				preds = append(preds, p)
			}
		}
		return preds
	}
	cte := regexp.MustCompile(`WITH matched_employees AS MATERIALIZED \(.*?\) SELECT`)
	if l, c := cte.FindString(code("ListPanelTransactions")), cte.FindString(code("CountPanelTransactionsByVerdict")); l == "" || l != c {
		t.Errorf("the name CTE differs between the list and the count:\n list:  %q\n count: %q", l, c)
	}
	list, count := where("ListPanelTransactions"), where("CountPanelTransactionsByVerdict")
	var expect []string
	for _, p := range list {
		if strings.Contains(p, "'verdict'") || strings.Contains(p, "cursor_") {
			continue
		}
		expect = append(expect, p)
	}
	if len(expect) < 6 {
		t.Fatalf("anti-vacuity: %d predicates read from ListPanelTransactions: %q", len(expect), list)
	}
	if !slices.Equal(expect, count) {
		t.Errorf("the count's predicates differ from the list's (verdict and cursor excepted):\n list:  %q\n count: %q", expect, count)
	}
}

// TestRLS_Tally_AnotherTenantsRecordsAreNotCounted: the count asked in A's context for B (its
// explicit filter admits B's rows) is zero; a tenant-predicate-free count in A's context covers
// A's records and not B's. Positive control: B's context counts B's.
func TestRLS_Tally_AnotherTenantsRecordsAreNotCounted(t *testing.T) {
	data := passDB(t)
	now := time.Now()
	a := newPassTenant(t, data, "Tally RLS A", "Door")
	b := newPassTenant(t, data, "Tally RLS B", "Door")
	from := now.Add(-time.Hour)
	a.tap(passTap{who: a.person("A", "Door"), venue: "Door", at: now.Add(-time.Minute), kind: "in"})
	for i := 0; i < 3; i++ {
		b.tap(passTap{who: b.person(fmt.Sprintf("B %d", i), "Door"), venue: "Door", at: now.Add(-time.Minute), kind: "in"})
	}
	count := func(ctxTenant, asked uuid.UUID) int64 {
		var n int64
		if err := data.WithTenant(context.Background(), ctxTenant, func(ctx context.Context, tx pgx.Tx) error {
			row, err := store.New(tx).CountPanelTransactionsByVerdict(ctx, store.CountPanelTransactionsByVerdictParams{
				TenantID: asked, FromAt: from, ToAt: now})
			n = row.Records
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := count(b.id, b.id); n != 3 {
		t.Fatalf("positive control: B's context counts %d of B's records, want 3", n)
	}
	if n := count(a.id, b.id); n != 0 {
		t.Errorf("RLS: A's context counting B's day got %d", n)
	}
	var mine, theirs int
	if err := data.WithTenant(context.Background(), a.id, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE t.location_id = $3), count(*) FILTER (WHERE t.location_id = $4)
		                         FROM transactions t WHERE t.occurred_at >= $1 AND t.occurred_at < $2`,
			from, now, a.venues["Door"], b.venues["Door"]).Scan(&mine, &theirs)
	}); err != nil {
		t.Fatal(err)
	}
	if mine != 1 {
		t.Fatalf("positive control: A's tenant-filter-free count sees %d of A's records, want 1", mine)
	}
	if theirs != 0 {
		t.Errorf("RLS: A's tenant-filter-free count sees %d of B's records", theirs)
	}
}
