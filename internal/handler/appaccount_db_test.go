package handler

// appaccount_db_test.go -- M16-37 end to end: the signed request answers 204, the device is then
// signed out (401 on its next read), the trail carries one request, and no record is deleted; and the
// business's owners are e-mailed off the request path (ownernotice.go) through the real breaker, the
// real owners' read (resolve_admin_owners_by_tenant) and the real address path.

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/mail"
)

// deletionOwners serves "Delete my account" on h with the owners' notices over the real directory
// (adminauth.Resets) and a real breaker around p; tripped fills the breaker's window first. The queue is
// drained at cleanup.
func deletionOwners(t *testing.T, h *appHarness, p *breakerProbe, tripped bool) *OwnerNotices {
	t.Helper()
	staff, err := tenant.NewStaff(h.data, h.trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	resets, err := adminauth.NewResets(h.data, h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	b, err := mail.NewBreaker(p, mail.BreakerConfig{Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	if tripped {
		fillBreaker(t, b, 300)
		if !b.Refusing() {
			t.Fatal("CONTROL FAILED: the breaker passes after 300 sends")
		}
	}
	log := slog.New(slog.NewJSONHandler(h.logs, nil))
	q, err := NewEmailOwnerNotices(b, true, resets, h.cfg.BaseURL, h.trail, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = q.Drain(ctx)
	})
	if err := h.app.ServeDeletion(staff, q); err != nil {
		t.Fatal(err)
	}
	return q
}

// addAdmin adds one panel member to h's business with an address; it returns the row's id.
func (h *appHarness) addAdmin(t *testing.T, role, status, address string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		                      VALUES ($1, $2, 'Owner Fixture', $3, $4, $5, $6)`, id, h.tenantID, address, ownerDigest, role, status)
		return e
	})
	if err != nil {
		t.Fatalf("add admin: %v", err)
	}
	return id
}

func drainNow(t *testing.T, q *OwnerNotices) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := q.Drain(ctx); err != nil {
		t.Fatalf("drain: %v", err)
	}
}

// noticeRows reads the business's owner-notice rows for employee: each one's action, actor and detail.
func (h *appHarness) noticeRows(t *testing.T, employee uuid.UUID) []ownerNoticeDetailRow {
	t.Helper()
	var out []ownerNoticeDetailRow
	err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT action, actor_id IS NULL, detail::text FROM audit_log
		                          WHERE tenant_id = $1 AND target = $2::text AND action LIKE 'employee.deletion_notice.%'`, h.tenantID, employee)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var (
				action, detail string
				noActor        bool
			)
			if e := rows.Scan(&action, &noActor, &detail); e != nil {
				return e
			}
			if !noActor {
				t.Errorf("row %q has an actor", action)
			}
			if strings.Contains(detail, "@") {
				t.Errorf("row %q carries an '@': %s", action, detail)
			}
			var d ownerNoticeDetailRow
			if e := json.Unmarshal([]byte(detail), &d); e != nil {
				return e
			}
			d.action = action
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestAppAccountDeleteDB_SignsOutAndTrailsTheRequest(t *testing.T) {
	h := newAppHarness(t)
	deletionOwners(t, h, &breakerProbe{}, false)
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

// TestAppAccountDeleteDB_TheOwnersAreEMailedWhileTheBreakerRefuses [G37-d]: with the process-wide breaker
// refusing, a deletion request still e-mails each owner who can sign in -- to the address on their own
// row, and nobody else (not a manager, not a disabled owner) -- in id order; each owner has one sent row
// (target the employee, no actor, the owner's id and no address); neither the e-mails nor the rows nor
// the log carry an '@' (the employee's own stored address included).
func TestAppAccountDeleteDB_TheOwnersAreEMailedWhileTheBreakerRefuses(t *testing.T) {
	h := newAppHarness(t)
	p := &breakerProbe{}
	q := deletionOwners(t, h, p, true)
	label := strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	addr := func(who string) string { return who + "-" + label + "@" + "owners.example.test" }
	ownerA := h.addAdmin(t, "owner", "active", addr("owner-a"))
	ownerB := h.addAdmin(t, "owner", "active", addr("owner-b"))
	h.addAdmin(t, "manager", "active", addr("manager"))
	h.addAdmin(t, "owner", "disabled", addr("disabled"))
	if err := h.data.WithTenant(context.Background(), h.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE employees SET email = $3 WHERE tenant_id = $1 AND id = $2`, h.tenantID, h.employeeID, addr("employee"))
		return e
	}); err != nil {
		t.Fatal(err)
	}
	phone := newAppPhone(t)
	h.register(t, phone, h.employeeID)

	if w := h.post(t, phone, AppAccountDeletePath, "{}"); w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s, want 204", w.Code, w.Body.String())
	}
	drainNow(t, q)

	if p.count() != 300+2 {
		t.Fatalf("%d messages reached the sender, want the 300 fill and 2 notices", p.count())
	}
	p.mu.Lock()
	notices := append([]mail.Message(nil), p.sent[300:]...)
	p.mu.Unlock()
	want := map[string]bool{addr("owner-a"): true, addr("owner-b"): true}
	for _, m := range notices {
		if !want[m.To] {
			t.Errorf("a notice went to an address that is not an owner's who can sign in")
		}
		delete(want, m.To)
		if m.Subject != "An employee asked to delete their Taptime account" {
			t.Errorf("subject %q", m.Subject)
		}
		if strings.Contains(m.Text, "@") || strings.Contains(m.HTML, "@") {
			t.Errorf("a notice's body carries an '@'")
		}
	}
	rows := h.noticeRows(t, h.employeeID)
	got := map[string]bool{}
	for _, r := range rows {
		if r.action != ActionEmployeeDeletionNoticeSent || r.Outcome != "sent" {
			t.Errorf("row %+v, want sent", r)
		}
		got[r.AdminUserID] = true
	}
	if len(rows) != 2 || !got[ownerA.String()] || !got[ownerB.String()] {
		t.Fatalf("rows %+v, want one sent row for each of the two owners", rows)
	}
	if bytes.Contains([]byte(h.logs.String()), []byte("@")) {
		t.Errorf("a log line carries an '@'")
	}
}

// TestAppAccountDeleteDB_ADeactivatedPersonsRequestStillTellsTheOwners: the deactivated person keeps
// their credentials (T59) and the owners are still e-mailed. Their SECOND request inside tenant.DeletionRepeatWindow answers the same 204
// and adds nothing: one trail row and one notice in total (the device still signs, so without the rule
// every press is a row and an e-mail per owner).
func TestAppAccountDeleteDB_ADeactivatedPersonsRequestStillTellsTheOwners(t *testing.T) {
	h := newAppHarness(t)
	p := &breakerProbe{}
	q := deletionOwners(t, h, p, false)
	owner := h.addAdmin(t, "owner", "active", "owner-"+uuid.NewString()[:8]+"@"+"owners.example.test")
	gone := h.newEmployee(t, "deactivated")
	phone := newAppPhone(t)
	h.register(t, phone, gone)

	for i := range 2 {
		if w := h.post(t, phone, AppAccountDeletePath, "{}"); w.Code != http.StatusNoContent {
			t.Fatalf("delete #%d = %d %s, want 204", i+1, w.Code, w.Body.String())
		}
	}
	drainNow(t, q)
	if p.count() != 1 {
		t.Fatalf("%d e-mails, want 1", p.count())
	}
	if n := h.countWhere(t, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND action = 'employee.deletion_requested' AND target = $2::text`, gone); n != 1 {
		t.Fatalf("%d trail rows after a repeat, want 1", n)
	}
	rows := h.noticeRows(t, gone)
	if len(rows) != 1 || rows[0].action != ActionEmployeeDeletionNoticeSent || rows[0].AdminUserID != owner.String() {
		t.Fatalf("rows %+v, want one sent row for the owner", rows)
	}
	if h.liveDevices(t, gone) != 1 {
		t.Fatal("the deactivated person's device was revoked")
	}
}

// TestAppAccountDeleteDB_TheAnswerDoesNotWaitForABlockedSender: with the relay holding the owner's e-mail,
// the request still answers 204 at once; the e-mail goes when the relay lets it.
func TestAppAccountDeleteDB_TheAnswerDoesNotWaitForABlockedSender(t *testing.T) {
	h := newAppHarness(t)
	p := &breakerProbe{}
	q := deletionOwners(t, h, p, false)
	h.addAdmin(t, "owner", "active", "owner-"+uuid.NewString()[:8]+"@"+"owners.example.test")
	entered, release := p.holdNext()
	defer release()
	phone := newAppPhone(t)
	h.register(t, phone, h.employeeID)

	start := time.Now()
	w := h.post(t, phone, AppAccountDeletePath, "{}")
	took := time.Since(start)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s, want 204", w.Code, w.Body.String())
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the owner's e-mail never reached the relay")
	}
	if n := len(h.noticeRows(t, h.employeeID)); n != 0 {
		t.Fatalf("%d notice rows while the relay holds the e-mail, want 0: the answer came first", n)
	}
	if took > 2*time.Second {
		t.Errorf("the answer took %v with the relay holding the e-mail", took)
	}
	release()
	drainNow(t, q)
	if rows := h.noticeRows(t, h.employeeID); len(rows) != 1 || rows[0].action != ActionEmployeeDeletionNoticeSent {
		t.Fatalf("rows %+v, want one sent row after the relay let go", rows)
	}
}
