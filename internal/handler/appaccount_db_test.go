package handler

// appaccount_db_test.go -- M16-37 end to end: the signed request answers 204, the device is then
// signed out (401 on its next read), the trail carries one request, and no record is deleted.

import (
	"log/slog"
	"net/http"
	"testing"

	"github.com/atknatk/tappa/internal/domain/tenant"
)

func TestAppAccountDeleteDB_SignsOutAndTrailsTheRequest(t *testing.T) {
	h := newAppHarness(t)
	staff, err := tenant.NewStaff(h.data, h.trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.app.ServeDeletion(staff); err != nil {
		t.Fatal(err)
	}
	phone := newAppPhone(t)
	h.register(t, phone, h.employeeID)
	before := h.transactionCount(t)

	if w := h.post(t, phone, AppAccountDeletePath, "{}"); w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s, want 204", w.Code, w.Body.String())
	}
	if w := h.read(t, phone, http.MethodGet, AppMePath, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("/me after the request = %d, want 401 (signed out)", w.Code)
	}
	if n := h.countWhere(t, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'employee.deletion_requested' AND target = $2::text`, h.employeeID); n != 1 {
		t.Fatalf("%d trail rows, want 1", n)
	}
	if after := h.transactionCount(t); after != before {
		t.Fatalf("transactions %d -> %d: a deletion request deletes no record", before, after)
	}
}
