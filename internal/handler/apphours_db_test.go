package handler

// apphours_db_test.go -- M16-31 end to end: [G31-d] with the setting off a signed request is 404
// not_found and an unsigned one is 401; [G31-h] /me carries the window; on, the person's own
// check-in is listed in plain words and another person's never is; a week outside the window and a
// cursor of the wrong shape are 400.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/domain/ledger"
)

func TestAppHoursDB_TheSettingTheWindowAndThePerson(t *testing.T) {
	h := newAppHarness(t)
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if ownerDSN == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the fixture's records are written as the owner")
	}
	reader, err := ledger.NewReader(h.data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.ServeHours(reader); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	setWeeks := func(n any) {
		t.Helper()
		if _, err := owner.Exec(ctx, `UPDATE tenants SET staff_hours_weeks = $2 WHERE id = $1`, h.tenantID, n); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { setWeeks(nil) })
	write := func(person any, at time.Time) {
		t.Helper()
		tx, err := owner.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, h.tenantID.String()); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, created_at, verdict, channel, type, practice)
		                           VALUES ($1, $2, $3, $4, $4, 'flag', 'nfc', 'in', false)`, h.tenantID, person, h.locationID, at); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	other := uuid.New()
	if _, err := owner.Exec(ctx, `INSERT INTO employees (id, tenant_id, external_ref, full_name, status, invited_at, location_id)
	                              VALUES ($1, $2, $3, 'Other person', 'active', now(), $4)`,
		other, h.tenantID, "ext-"+other.String()[:8], h.locationID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	write(h.employeeID, now.Add(-time.Minute))
	write(other, now.Add(-2*time.Minute))

	phone := newAppPhone(t)
	h.register(t, phone, h.employeeID)

	unsigned := httptest.NewRecorder()
	h.router.ServeHTTP(unsigned, httptest.NewRequest(http.MethodPost, AppHoursPath, strings.NewReader(`{}`)))
	if unsigned.Code != http.StatusUnauthorized {
		t.Fatalf("unsigned = %d, want 401", unsigned.Code)
	}
	if w := h.read(t, phone, http.MethodPost, AppHoursPath, `{}`); w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"not_found"`) {
		t.Fatalf("off = %d %s, want 404 not_found", w.Code, w.Body.String())
	}

	setWeeks(1)
	w := h.read(t, phone, http.MethodGet, AppMePath, "")
	var me appMeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil || me.HoursWeeks != 1 {
		t.Fatalf("/me hoursWeeks = %d (%v) %s", me.HoursWeeks, err, w.Body.String())
	}
	w = h.read(t, phone, http.MethodPost, AppHoursPath, `{}`)
	if w.Code != http.StatusOK {
		t.Fatalf("on = %d %s", w.Code, w.Body.String())
	}
	var got appHoursResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.WeeksVisible != 1 || got.HasPrev || got.HasNext || len(got.Days) != 7 {
		t.Fatalf("window: %+v", got)
	}
	if len(got.Records) != 1 || got.Records[0].State != "waiting" || got.Records[0].Direction != "in" || got.Waiting.Records != 1 {
		t.Fatalf("records %+v waiting %+v -- want the person's one flag, not the other's", got.Records, got.Waiting)
	}
	prev := now.AddDate(0, 0, -14).Format("2006-01-02")
	for _, body := range []string{`{"week":"` + prev + `"}`, `{"after":"nope"}`, `{"week":"x"}`, `{"extra":1}`} {
		if w := h.read(t, phone, http.MethodPost, AppHoursPath, body); w.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400", body, w.Code)
		}
	}
}

// TestAppHoursDB_TheCursorIsBoundToThePersonAndTheWeek [G31-j]: a 201-record week pages; the next
// page answers with the cursor and its week; the same cursor with another week, from another person's
// device, or in the older unbound shape is 400 bad_request -- never a page of a different walk.
func TestAppHoursDB_TheCursorIsBoundToThePersonAndTheWeek(t *testing.T) {
	h := newAppHarness(t)
	ownerDSN := os.Getenv("DATABASE_MIGRATE_URL")
	if ownerDSN == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the fixture's records are written as the owner")
	}
	reader, err := ledger.NewReader(h.data, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.ServeHours(reader); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	owner, err := pgx.Connect(ctx, ownerDSN)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close(context.Background()) })
	if _, err := owner.Exec(ctx, `UPDATE tenants SET staff_hours_weeks = 2 WHERE id = $1`, h.tenantID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = owner.Exec(context.Background(), `UPDATE tenants SET staff_hours_weeks = NULL WHERE id = $1`, h.tenantID)
	})
	var zoneName string
	if err := owner.QueryRow(ctx, `SELECT timezone FROM tenants WHERE id = $1`, h.tenantID).Scan(&zoneName); err != nil {
		t.Fatal(err)
	}
	zone, err := time.LoadLocation(zoneName)
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().In(zone)
	thisMonday := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, zone)
	for thisMonday.Weekday() != time.Monday {
		thisMonday = thisMonday.AddDate(0, 0, -1)
	}
	lastMonday := thisMonday.AddDate(0, 0, -7)
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, h.tenantID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO transactions (tenant_id, employee_id, location_id, occurred_at, created_at, verdict, channel, type, practice)
	                           SELECT $1, $2, $3, $4::timestamptz + make_interval(secs => g), $4::timestamptz + make_interval(secs => g), 'flag', 'nfc', 'in', false
	                           FROM generate_series(0, $5 - 1) AS g`,
		h.tenantID, h.employeeID, h.locationID, lastMonday.Add(12*time.Hour).UTC(), ledger.OwnPageSize+1); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	phone := newAppPhone(t)
	h.register(t, phone, h.employeeID)
	week := lastMonday.Format("2006-01-02")

	hours := func(p *appPhone, body string) (int, appHoursResponse) {
		t.Helper()
		w := h.read(t, p, http.MethodPost, AppHoursPath, body)
		var got appHoursResponse
		if w.Code == http.StatusOK {
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
		} else if !strings.Contains(w.Body.String(), `"bad_request"`) {
			t.Fatalf("%s = %d %s", body, w.Code, w.Body.String())
		}
		return w.Code, got
	}
	code, first := hours(phone, `{"week":"`+week+`"}`)
	if code != http.StatusOK || first.NextCursor == nil || len(first.Records) != ledger.OwnPageSize {
		t.Fatalf("page one = %d, %d records, cursor %v", code, len(first.Records), first.NextCursor)
	}
	cursor := *first.NextCursor
	if code, second := hours(phone, `{"week":"`+week+`","after":"`+cursor+`"}`); code != http.StatusOK || len(second.Records) != 1 || second.NextCursor != nil {
		t.Fatalf("page two = %d, %d records, cursor %v", code, len(second.Records), second.NextCursor)
	}
	if code, _ := hours(phone, `{"week":"`+thisMonday.Format("2006-01-02")+`","after":"`+cursor+`"}`); code != http.StatusBadRequest {
		t.Errorf("the cursor with another week = %d, want 400", code)
	}
	if code, _ := hours(phone, `{"after":"`+cursor+`"}`); code != http.StatusBadRequest {
		t.Errorf("the cursor with no week (this week) = %d, want 400", code)
	}
	unbound := cursor[:strings.LastIndex(cursor, "_")]
	if code, _ := hours(phone, `{"week":"`+week+`","after":"`+unbound+`"}`); code != http.StatusBadRequest {
		t.Errorf("the older unbound cursor = %d, want 400", code)
	}
	other := h.newEmployee(t, "active")
	theirs := newAppPhone(t)
	h.register(t, theirs, other)
	if code, _ := hours(theirs, `{"week":"`+week+`","after":"`+cursor+`"}`); code != http.StatusBadRequest {
		t.Errorf("another person's cursor = %d, want 400", code)
	}
}
