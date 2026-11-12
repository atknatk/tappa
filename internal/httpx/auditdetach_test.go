package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/session"
)

// liveCtxRecorder refuses a write under a context that is already done, as the driver does.
type liveCtxRecorder struct {
	mu sync.Mutex
	n  int
}

func (r *liveCtxRecorder) Record(ctx context.Context, _ audit.Event) (uuid.UUID, error) {
	if err := ctx.Err(); err != nil {
		return uuid.Nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.n++
	return uuid.New(), nil
}

// TestTapLimiter_TheRefusalRowSurvivesTheClientLeaving (T284 (a), user decision 2026-11-12):
// the limiter's refusal row is written under audit.Detached, so a tapper whose connection is
// already gone still leaves it, and the answer is still 429.
func TestTapLimiter_TheRefusalRowSurvivesTheClientLeaving(t *testing.T) {
	t.Parallel()
	rec := &liveCtxRecorder{}
	if _, err := rec.Record(canceledCtx(), audit.Event{}); err == nil {
		t.Fatal("CONTROL FAILED: the fake writes under a cancelled context")
	}
	tl := NewTapLimiter(TapLimitParams{Audit: rec, Log: quietLog(), SessionLimit: 1})
	next, _ := tapProbe()
	id := Identity{State: SessionLive, Session: session.Resolved{ID: uuid.New(), TenantID: uuid.New(), EmployeeID: uuid.New()}}
	serve := func(ctx context.Context) int {
		w := httptest.NewRecorder()
		r := tapRequest("203.0.113.9:5000")
		tl.BySession(next).ServeHTTP(w, r.WithContext(context.WithValue(ctx, identityKey{}, id)))
		return w.Code
	}
	if got := serve(context.Background()); got != http.StatusOK {
		t.Fatalf("the first request answered %d", got)
	}
	if got := serve(canceledCtx()); got != http.StatusTooManyRequests || rec.n != 1 {
		t.Fatalf("an over-budget request whose client left: %d with %d row(s), want 429 and the row", got, rec.n)
	}
}

func canceledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}
