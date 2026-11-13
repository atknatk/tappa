package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/google/uuid"
)

// cancelHonouringTrail behaves as the database driver does about its context: a write under a
// context that is already done fails with that context's error and stores nothing.
type cancelHonouringTrail struct {
	mu     sync.Mutex
	events []audit.Event
}

func (f *cancelHonouringTrail) Record(ctx context.Context, e audit.Event) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	return uuid.New(), nil
}

func (f *cancelHonouringTrail) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// TestAuditWrites_SurviveTheClientLeaving (T284 (a), user decision 2026-11-12): every handler
// family that writes an audit row on a request's behalf writes it under audit.Detached, so a
// request whose client has already gone still leaves its row, and no EventWriteFailed line is
// written for the client's own cancel. The control (the fake against the cancelled context
// itself) proves the fake would have refused.
func TestAuditWrites_SurviveTheClientLeaving(t *testing.T) {
	t.Parallel()
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (&cancelHonouringTrail{}).Record(gone, audit.Event{}); err == nil {
		t.Fatal("CONTROL FAILED: the fake writes under a cancelled context, so nothing below is measured")
	}
	e := audit.Event{TenantID: panelTestTenant, ActorID: ptr(panelTestAdmin), Action: ActionReportExported, Target: "x"}
	for _, c := range []struct {
		name  string
		write func(trail *cancelHonouringTrail, log *slog.Logger) error
	}{
		{"panel record", func(tr *cancelHonouringTrail, log *slog.Logger) error {
			(&AdminAuth{audit: tr, log: log}).record(gone, e)
			return nil
		}},
		{"panel recordFirst (the export gate)", func(tr *cancelHonouringTrail, log *slog.Logger) error {
			return (&AdminAuth{audit: tr, log: log}).recordFirst(gone, e)
		}},
		{"activation record", func(tr *cancelHonouringTrail, log *slog.Logger) error {
			(&Activation{audit: tr, log: log}).record(gone, e)
			return nil
		}},
		{"recovery record (the spend trail and recordForLink)", func(tr *cancelHonouringTrail, log *slog.Logger) error {
			(&AdminReset{audit: tr, log: log}).record(gone, e)
			return nil
		}},
	} {
		var logged strings.Builder
		trail := &cancelHonouringTrail{}
		err := c.write(trail, slog.New(slog.NewTextHandler(&logged, nil)))
		if err != nil || trail.count() != 1 {
			t.Errorf("%s under a cancelled request context: %d row(s), err %v; want the row written", c.name, trail.count(), err)
		}
		if strings.Contains(logged.String(), audit.EventWriteFailed) {
			t.Errorf("%s wrote %q for the client's own cancel:\n%s", c.name, audit.EventWriteFailed, logged.String())
		}
	}
}

// TestSuspensionGate_TheRefusalRowSurvivesTheClientLeaving is the same promise for the
// suspension gate's inline write (ADR 0025 §3): a refused write whose client has gone still
// leaves the refusal's row, and the answer is still 403.
func TestSuspensionGate_TheRefusalRowSurvivesTheClientLeaving(t *testing.T) {
	t.Parallel()
	code := db.SuspensionSecurityHold
	trail := &cancelHonouringTrail{}
	h := tripwirePanel(t, suspendedIdentity(uuid.New(), uuid.New(), uuid.New(), &code), trail,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	gone, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodPost, boundPath(h, suspensionCookieValue, venueSaveHref),
		strings.NewReader("full_name=x")).WithContext(gone)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", testBaseURL)
	req.RemoteAddr = "203.0.113.9:1234"
	req.AddCookie(&http.Cookie{Name: adminauth.CookieName, Value: suspensionCookieValue})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || trail.count() != 1 {
		t.Fatalf("a suspended write whose client left: %d with %d row(s), want 403 and the row", rec.Code, trail.count())
	}
}
