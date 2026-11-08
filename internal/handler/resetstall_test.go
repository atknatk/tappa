package handler

// resetstall_test.go -- T211 and T219 against fakes: a mint that gave up waiting for a lock answers
// like every other address, and a kept row is an outcome with the request's clock and the worker's
// second try, which never touches the live link it names.

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/audit"
)

// lineStalledMint is logStalledMint's sentence.
const lineStalledMint = "minting a link waited too long for a lock, so none was made"

// TestAdminReset_AStalledMintAnswersLikeEveryOtherAddress (T219): an address whose account -- or
// identity, flag on -- stalled gets the status and the body an unknown address gets; nothing is sent,
// withdrawn or written to the trail; the stall is one ERROR line with the id and never the address,
// through the per-account budget; and the unknown-address line is not written for it (it resolved).
//
// MUTATIONS: no logStalledMint call -> no line -> red. The stalled accounts left out of the "nothing
// resolved" test -> the unknown-address line for a registered address -> red.
func TestAdminReset_AStalledMintAnswersLikeEveryOtherAddress(t *testing.T) {
	t.Parallel()
	const stalledAddr = "stalled@stall.example.test"
	const unknown = "nobody@stall.example.test"
	row := adminauth.StalledReset{TenantID: uuid.New(), AdminUserID: uuid.New()}
	ident := adminauth.StalledReset{IdentityID: uuid.New()}
	for _, tc := range []struct {
		name   string
		flag   bool
		resets *fakeResets
		id     string
	}{
		{"per row, flag off", false, &fakeResets{stalledFor: map[string][]adminauth.StalledReset{stalledAddr: {row}}}, row.AdminUserID.String()},
		{"per identity, flag on", true, &fakeResets{personFor: map[string]adminauth.PersonResets{
			stalledAddr: {Stalled: []adminauth.StalledReset{ident}}}}, ident.IdentityID.String()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var logged bytes.Buffer
			trail := &fakeTrail{}
			mail := &recordingChannel{}
			router, h := newResetRouterFlag(t, tc.resets, mail, trail, tc.flag, &logged)
			ask := func(i int, email string) (int, string) {
				b := newBrowser(t, router)
				b.ip = fmt.Sprintf("198.51.100.%d:1234", i+1)
				csrf := csrfFrom(t, htmlOf(t, b.do(http.MethodGet, adminResetPath, nil)))
				rec := b.do(http.MethodPost, adminResetPath, url.Values{"csrf": {csrf}, "email": {email}})
				return rec.Code, rec.Body.String()
			}
			wantCode, wantBody := ask(0, unknown)
			const asks = adminResetAccountLimit + 2
			for i := 1; i <= asks; i++ {
				if code, body := ask(i, stalledAddr); code != wantCode || body != wantBody {
					t.Fatalf("a stalled address answered %d (%d bytes), an unknown one %d (%d bytes): the answer tells them apart",
						code, len(body), wantCode, len(wantBody))
				}
			}
			drain(t, h)
			out := logged.String()
			if n := strings.Count(out, lineStalledMint); n != adminResetAccountLimit {
				t.Errorf("%d stalled-mint line(s) for %d requests, want the account budget's %d:\n%s", n, asks, adminResetAccountLimit, out)
			}
			if n := strings.Count(out, "panel recovery rate limited"); n != 1 {
				t.Errorf("%d rate-limited line(s), want 1", n)
			}
			for _, l := range strings.Split(out, "\n") {
				if strings.Contains(l, lineStalledMint) && (!strings.Contains(l, tc.id) || !strings.Contains(l, "level=ERROR")) {
					t.Errorf("a stalled-mint line without the id or not an ERROR: %s", l)
				}
			}
			if strings.Contains(out, stalledAddr) {
				t.Error("the stalled address reached the log")
			}
			if n := strings.Count(out, "no active admin for that address"); n != 1 {
				t.Errorf("%d unknown-address line(s), want 1 (the unknown address's): a stalled address resolved", n)
			}
			if n := len(mail.all()); n != 0 || trail.total() != 0 || len(tc.resets.withdrawals()) != 0 {
				t.Errorf("%d send(s), %d row(s), %d withdrawal(s); want none: nothing was minted", n, trail.total(), len(tc.resets.withdrawals()))
			}
		})
	}
}

// TestResetOutcome_AKeptRowRetriedLaterLeavesTheLiveLinkAlone (T211): the kept row is an outcome of
// one write. A transient failure on the request is tried once more by the worker, a permanent one is
// not, both tries carry one id, and no try of any kind withdraws the live link the row names (the
// owner's, in their inbox).
//
// MUTATIONS: recordKept back to one write on its own clock (recordForAdmin) -> the "stalled once" row
// lost -> red. A kept outcome owing a withdrawal -> the live link withdrawn -> red.
func TestResetOutcome_AKeptRowRetriedLaterLeavesTheLiveLinkAlone(t *testing.T) {
	t.Parallel()
	stall := context.DeadlineExceeded
	permanent := &pgconn.PgError{Code: "23514"}
	for _, tc := range []struct {
		name     string
		rowErrs  []error
		wantRow  int
		wantRows int
		wantLost int
	}{
		{name: "nothing fails", wantRow: 1, wantRows: 1},
		{name: "stalled once", rowErrs: []error{stall}, wantRow: 2, wantRows: 1},
		{name: "stays down", rowErrs: []error{stall, stall, stall}, wantRow: 2, wantLost: 1},
		{name: "refused", rowErrs: []error{permanent}, wantRow: 1, wantLost: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			const holding = "holding@kept-retry.example.test"
			f := &flakyWrites{fakeResets: &fakeResets{keptFor: map[string][]adminauth.Reset{holding: {keptReset()}}}, rowErrs: tc.rowErrs}
			var logged bytes.Buffer
			router, h := newOutboxFlow(t, f, &recordingChannel{}, f, &logged)
			h.clocks.retryPause = time.Millisecond
			requestReset(t, newBrowser(t, router), holding)
			drain(t, h)
			withdraw, row := f.tries()
			if withdraw != 0 {
				t.Errorf("%d withdrawal(s): a kept row must never retire the live link it names", withdraw)
			}
			if row != tc.wantRow {
				t.Errorf("%d try(ies) of the row, want %d", row, tc.wantRow)
			}
			if n := f.count(ActionAdminResetKept); n != tc.wantRows {
				t.Errorf("%d kept row(s), want %d", n, tc.wantRows)
			}
			if n := strings.Count(logged.String(), lineRowLost); n != tc.wantLost {
				t.Errorf("%d lost-row line(s), want %d:\n%s", n, tc.wantLost, logged.String())
			}
			if ids := f.ids; len(ids) > 0 && (ids[0] == uuid.Nil || ids[len(ids)-1] != ids[0]) {
				t.Errorf("row ids %v: the tries of one row must carry one id", ids)
			}
		})
	}
}

// deadlineTrail records the deadline each row's write was given.
type deadlineTrail struct {
	fakeTrail
	mu        sync.Mutex
	deadlines []time.Time
}

func (d *deadlineTrail) Record(ctx context.Context, e audit.Event) (uuid.UUID, error) {
	dl, ok := ctx.Deadline()
	d.mu.Lock()
	if !ok {
		dl = time.Time{}
	}
	d.deadlines = append(d.deadlines, dl)
	d.mu.Unlock()
	return d.fakeTrail.Record(ctx, e)
}

// TestAdminReset_AKeptRowWaitsNoLongerThanTheFloor (T211): every kept row a request writes is given a
// deadline no later than the moment its response leaves -- the floor's end -- so a stalled INSERT can
// never hold the answer past it, however many accounts the address names. Deterministic: it reads the
// deadline, not the clock.
//
// MUTATION: the kept rows on the fallback writes' clock (resetFallbackBudget) -> a deadline about a
// second after the response -> red.
func TestAdminReset_AKeptRowWaitsNoLongerThanTheFloor(t *testing.T) {
	t.Parallel()
	const holding = "holding@kept-floor.example.test"
	kept := make([]adminauth.Reset, adminauth.ResetWindow)
	for i := range kept {
		kept[i] = keptReset()
	}
	resets := &fakeResets{keptFor: map[string][]adminauth.Reset{holding: kept}}
	trail := &deadlineTrail{}
	var logged bytes.Buffer
	router, h := newOutboxFlow(t, resets, &recordingChannel{}, trail, &logged)
	requestReset(t, newBrowser(t, router), holding)
	answered := time.Now()
	drain(t, h)
	trail.mu.Lock()
	defer trail.mu.Unlock()
	if len(trail.deadlines) != adminauth.ResetWindow {
		t.Fatalf("%d row write(s), want %d", len(trail.deadlines), adminauth.ResetWindow)
	}
	for i, dl := range trail.deadlines {
		if dl.IsZero() {
			t.Errorf("row %d was written with no deadline", i)
			continue
		}
		if dl.After(answered) {
			t.Errorf("row %d had until %v after the response left: a stall there is read from the clock",
				i, dl.Sub(answered))
		}
	}
}
