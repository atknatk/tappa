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
