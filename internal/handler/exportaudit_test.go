package handler

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/domain/ledger"
)

// exportAuditBrowser is the owner's panel with the trail and the log under the test's
// control: the two CSV routes (hours and billing) read their fixtures from records and
// books.
func exportAuditBrowser(t *testing.T, trail *fakeTrail, logged *bytes.Buffer) *browser {
	t.Helper()
	records := newFakeLedger()
	records.hours = reportWith(func(r *ledger.Report) {
		r.Totals = ledger.Totals{Worked: 9 * time.Hour, Shifts: 2}
		r.People = []ledger.PersonHours{{
			EmployeeID: uuid.New(), Name: "Maria Borg", Worked: 9 * time.Hour, Shifts: 2,
			Daily: make([]time.Duration, ledger.ReportDays),
		}}
	})
	admins := &fakeAdmins{verify: func() (adminauth.Resolved, error) {
		return adminauth.Resolved{
			SessionID: panelTestSession, TenantID: panelTestTenant, AdminUserID: panelTestAdmin,
			Role: billingTestOwnerRole, FullName: "KF Owner",
		}, nil
	}}
	h, err := NewAdminAuth(admins, trail, records, records, &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{},
		adminTestConfig(), slog.New(slog.NewTextHandler(logged, &slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	r := chi.NewRouter()
	h.Mount(r)
	registerPanel(r, h)
	b := newBrowser(t, r)
	b.cookies[adminauth.CookieName] = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	return b
}

// unwritableTrail is a database failure the way the driver reports one: the sentence names
// the host, the port and the role, and the chain carries the socket's *net.OpError.
func unwritableTrail() error {
	op := &net.OpError{Op: "dial", Net: "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(10, 9, 8, 7), Port: 6543},
		Err:  errors.New("connect: connection refused")}
	return fmt.Errorf("audit: record: failed to connect to user=tappa_app database=tappa: 10.9.8.7:6543: %w", op)
}

// TestExports_RefuseTheFileWhenTheTrailRowCannotBeWritten is backlog T18 (user decision
// 2026-11-08): a bulk export whose audit row cannot be written is refused with the panel's
// 503 page and no byte of the file; the ERROR line names the class and not the database.
// The control half runs the same browser with a working trail and gets the file and one row.
func TestExports_RefuseTheFileWhenTheTrailRowCannotBeWritten(t *testing.T) {
	routes := []struct {
		name, path, action string
	}{
		{"hours", reportsCSVHref + "?week=2026-08-05", ActionReportExported},
		{"billing", billingCSVHref, ActionBillingExported},
	}
	for _, c := range routes {
		t.Run(c.name, func(t *testing.T) {
			var logged bytes.Buffer
			trail := &fakeTrail{}
			b := exportAuditBrowser(t, trail, &logged)

			// CONTROL: with a working trail the route hands over the file and writes one row.
			ok := b.do(http.MethodGet, c.path, nil)
			if ok.Code != http.StatusOK || !strings.HasPrefix(ok.Header().Get("Content-Type"), "text/csv") ||
				!strings.HasPrefix(ok.Body.String(), reportCSVBOM) {
				t.Fatalf("control: %d %q, want 200 text/csv starting with the BOM", ok.Code, ok.Header().Get("Content-Type"))
			}
			if n := trail.count(c.action); n != 1 {
				t.Fatalf("control: %d %s row(s), want 1", n, c.action)
			}

			trail.mu.Lock()
			trail.fail = unwritableTrail()
			trail.mu.Unlock()
			logged.Reset()
			rec := b.do(http.MethodGet, c.path, nil)

			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status %d, want 503", rec.Code)
			}
			if got := rec.Header().Get("Content-Disposition"); got != "" {
				t.Errorf("a refused export still names an attachment: %q", got)
			}
			if got := rec.Header().Get("Content-Type"); strings.HasPrefix(got, "text/csv") {
				t.Errorf("a refused export is served as %q", got)
			}
			if got := rec.Header().Get("Retry-After"); got != "60" {
				t.Errorf("Retry-After %q, want 60", got)
			}
			body := rec.Body.String()
			for _, never := range []string{reportCSVBOM, "Maria Borg", "employee_count", "worked_minutes"} {
				if strings.Contains(body, never) {
					t.Errorf("the refusal carries %q, a piece of the file", never)
				}
			}
			for _, want := range []string{problemExportNotRecorded.Title, problemExportNotRecorded.Message, problemExportNotRecorded.Hint} {
				if !strings.Contains(body, htmlText(want)) {
					t.Errorf("the refusal does not say %q", want)
				}
			}
			if n := trail.count(c.action); n != 1 {
				t.Errorf("%d %s row(s) after the refusal, want the control's 1", n, c.action)
			}

			line := logged.String()
			for _, want := range []string{"level=ERROR", "action=" + c.action, LogErrClassKey + "=dial"} {
				if !strings.Contains(line, want) {
					t.Errorf("the log does not carry %q:\n%s", want, line)
				}
			}
			for _, never := range []string{"10.9.8.7", "6543", "tappa_app", "database=tappa"} {
				if strings.Contains(line, never) {
					t.Errorf("the log carries %q, a piece of the driver's sentence:\n%s", never, line)
				}
			}
		})
	}
}

// TestRecordFirst_ReturnsTheFailure pins the half of T18 the export refuses on: recordFirst
// hands a failed write back instead of only logging it (record's contract), and a working
// write returns nil and leaves one row.
func TestRecordFirst_ReturnsTheFailure(t *testing.T) {
	var logged bytes.Buffer
	trail := &fakeTrail{fail: unwritableTrail()}
	h, err := NewAdminAuth(&fakeAdmins{}, trail, newFakeLedger(), newFakeLedger(), &fakeReviewer{},
		&fakeStaff{}, &fakeInviter{}, &fakeVenues{}, &fakePlaques{}, &fakeRecorder{}, newFakeRules(),
		newFakeScribe(), newFakeBooks(), newFakeAccount(), newFakeBrands(), newFakeBrandWriter(), nil, &fakeNotices{},
		adminTestConfig(), slog.New(slog.NewTextHandler(&logged, nil)))
	if err != nil {
		t.Fatalf("NewAdminAuth: %v", err)
	}
	e := audit.Event{TenantID: panelTestTenant, ActorID: ptr(panelTestAdmin), Action: ActionReportExported, Target: "2026-08-03"}
	if err := h.recordFirst(t.Context(), e); err == nil {
		t.Fatal("recordFirst swallowed a failed write")
	}
	if line := logged.String(); !strings.Contains(line, "level=ERROR") {
		t.Errorf("a failed write is not an ERROR line:\n%s", line)
	}

	// A request the client abandoned is refused the same way, at WARN.
	logged.Reset()
	trail.fail = fmt.Errorf("audit: record: %w", context.Canceled)
	if err := h.recordFirst(t.Context(), e); err == nil {
		t.Fatal("recordFirst swallowed a cancelled write")
	}
	if line := logged.String(); !strings.Contains(line, "level=WARN") || !strings.Contains(line, LogErrClassKey+"=canceled") {
		t.Errorf("a cancelled write is not a WARN with its class:\n%s", line)
	}

	trail.fail = nil
	if err := h.recordFirst(t.Context(), e); err != nil {
		t.Fatalf("recordFirst on a working trail: %v", err)
	}
	if n := trail.count(ActionReportExported); n != 1 {
		t.Fatalf("%d row(s), want 1", n)
	}
}
