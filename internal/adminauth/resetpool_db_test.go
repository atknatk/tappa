package adminauth

// resetpool_db_test.go -- T236 (a) against real Postgres: a wait that is NOT a lock -- here the pool's,
// every connection busy -- on the one step of a request only a registered address reaches. The mint
// gives up when the request's ResetMintLockWait is spent instead of waiting for a connection, reports
// the account stalled, and writes nothing. The pool is starved for real (pgxpool's
// own acquire wait), right after the resolver read, so the resolver -- which an unregistered address
// pays too -- is not slowed.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// testPoolConns is withSmallPool's pool_max_conns: how many connections the starver must hold.
const testPoolConns = 4

// poolStarver is the real database with one change: right after GetAdminByEmail returns -- the last
// read an unregistered address makes -- every pool connection is taken by another goroutine for
// hold, so the next transaction waits in pgxpool's acquire. Armed once per arm().
type poolStarver struct {
	*db.DB
	t    *testing.T
	hold time.Duration

	mu    sync.Mutex
	armed bool
	freed []chan struct{}
	// starved is when the last connection was taken: the start of the wait under test.
	starved time.Time
}

// since is how long ago the pool was starved.
func (s *poolStarver) since() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.starved)
}

func (s *poolStarver) arm() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.armed = true
}

// release frees every connection the starver holds and waits for them to return to the pool.
func (s *poolStarver) release() {
	s.mu.Lock()
	freed := s.freed
	s.freed = nil
	s.mu.Unlock()
	for _, f := range freed {
		<-f
	}
}

func (s *poolStarver) GetAdminByEmail(ctx context.Context, email string) ([]db.ResolvedAdmin, error) {
	rows, err := s.DB.GetAdminByEmail(ctx, email)
	s.mu.Lock()
	armed := s.armed
	s.armed = false
	s.mu.Unlock()
	if !armed {
		return rows, err
	}
	var taken sync.WaitGroup
	taken.Add(testPoolConns)
	for i := 0; i < testPoolConns; i++ {
		done := make(chan struct{})
		s.mu.Lock()
		s.freed = append(s.freed, done)
		s.mu.Unlock()
		go func() {
			defer close(done)
			if e := s.DB.WithTenant(context.Background(), uuid.New(), func(context.Context, pgx.Tx) error {
				taken.Done()
				time.Sleep(s.hold)
				return nil
			}); e != nil {
				s.t.Errorf("hold a pool connection: %v", e)
			}
		}()
	}
	taken.Wait()
	s.mu.Lock()
	s.starved = time.Now()
	s.mu.Unlock()
	return rows, err
}

// TestIssueForEmail_AStarvedPoolStallsTheMintNotTheRequest: every pool connection is busy for 3 s from
// the moment the resolver read returns. The registered address's mint gives up when the request's
// ResetMintLockWait is spent, returns the account stalled with a nil error, and leaves no link; an unregistered address
// under the same starvation is timed beside it (its request ends at the resolver). Before T236 (a) the
// mint waited for a connection: the 3 s here, the request's 30 s deadline and a 500 in production.
//
// MUTATION: no bound on the acquire (mintStep returning ctx unchanged) -> the call waits the 3 s and
// mints -> red.
func TestIssueForEmail_AStarvedPoolStallsTheMintNotTheRequest(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	tenantID := newTenantRow(t, d, "Starved Pool Ltd")
	email := randEmail(t)
	admin := newAdminRow(t, d, tenantID, email, "p", "active", "owner", "Starved")
	s := &poolStarver{DB: d, t: t, hold: 3 * time.Second}
	r := cheapResets(t, s)

	s.arm()
	_, _, _, err := r.IssueForEmail(ctx, "nobody-"+uuid.NewString()+"@m6.example")
	unknown := s.since()
	s.release()
	if err != nil {
		t.Fatalf("IssueForEmail, unknown address: %v", err)
	}

	s.arm()
	grants, kept, stalled, err := r.IssueForEmail(ctx, email)
	took := s.since()
	s.release()
	t.Logf("from the starved pool to the answer: registered %v, unregistered %v (budget %v)",
		took, unknown, ResetMintLockWait)
	if err != nil {
		t.Fatalf("IssueForEmail: %v -- a stall must not be an error: the caller would answer a registered address differently", err)
	}
	if len(grants) != 0 || len(kept) != 0 || len(stalled) != 1 || stalled[0].AdminUserID != admin {
		t.Fatalf("%d grant(s), %d kept, stalled %+v; want exactly this account stalled", len(grants), len(kept), stalled)
	}
	if took > stallCeiling {
		t.Errorf("the call took %v, over %v: the mint waited for a pool connection instead of giving up", took, stallCeiling)
	}
	if ids := liveLinkIDs(t, d, tenantID, admin); len(ids) != 0 {
		t.Fatalf("%d live link(s) after a stalled mint, want 0", len(ids))
	}

	grants, _, stalled, err = r.IssueForEmail(ctx, email)
	if err != nil || len(grants) != 1 || len(stalled) != 0 {
		t.Fatalf("with the pool free again: %v, %d grant(s), %d stalled; want one fresh link", err, len(grants), len(stalled))
	}
}

// TestIssueForPerson_AStarvedPoolStallsTheOpening: the same starvation on the per-identity path (flag
// on, the deployed state). The unlinked row's mint and the identity's opening both give up inside the
// one budget; nothing is written, the call returns both stalled with a nil error, and once the pool is
// free the next request opens the identity's link.
//
// MUTATION: no bound on the acquire -> the call waits the 3 s and opens -> red.
func TestIssueForPerson_AStarvedPoolStallsTheOpening(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	w := newPersonResetWorld(t)
	s := &poolStarver{DB: d, t: t, hold: 3 * time.Second}
	r := cheapResets(t, s)

	s.arm()
	if _, err := r.IssueForPerson(ctx, "nobody-"+uuid.NewString()+"@m6.example"); err != nil {
		t.Fatalf("IssueForPerson, unknown address: %v", err)
	}
	unknown := s.since()
	s.release()

	s.arm()
	p, err := r.IssueForPerson(ctx, w.typed)
	took := s.since()
	s.release()
	t.Logf("from the starved pool to the answer: registered %v, unregistered %v (budget %v)",
		took, unknown, ResetMintLockWait)
	if err != nil {
		t.Fatalf("IssueForPerson: %v -- a stall must not be an error", err)
	}
	if len(p.Grants) != 0 || p.IdentityKept != nil || len(p.Stalled) != 2 ||
		p.Stalled[0].AdminUserID != w.rowB || p.Stalled[1].IdentityID != w.ident {
		t.Fatalf("%d grant(s), identity kept %v, stalled %+v; want the unlinked row and the identity stalled",
			len(p.Grants), p.IdentityKept, p.Stalled)
	}
	if took > stallCeiling {
		t.Errorf("the call took %v, over %v: the opening waited for a pool connection instead of giving up", took, stallCeiling)
	}
	if n := w.int(`SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1`, w.ident); n != 0 {
		t.Fatalf("%d identity link(s) after a stalled opening, want 0", n)
	}
	if n := w.rowLinks(w.rowB); n != 0 {
		t.Fatalf("%d link(s) for the unlinked row after a stalled mint, want 0", n)
	}

	p, err = r.IssueForPerson(ctx, w.typed)
	if err != nil || len(p.Stalled) != 0 || len(p.Grants) != 2 || p.Grants[0].IdentityID != w.ident {
		t.Fatalf("with the pool free again: %v, %+v; want the identity's link and the row's", err, p)
	}
}

// lateOpeningDB opens the identity's link only after the step's bound has run out, on a context of its
// own -- the shape the third-eye review measured in the pool's hook (a guard marked cut, the opening
// still begun and committed).
type lateOpeningDB struct {
	*db.DB
	after time.Duration
}

func (l lateOpeningDB) OpenIdentityReset(_ context.Context, id uuid.UUID, h string, exp time.Time, wait time.Duration) (db.OpenedIdentityReset, error) {
	time.Sleep(l.after)
	return l.DB.OpenIdentityReset(context.Background(), id, h, exp, wait)
}

// TestIssueForPerson_AnOpeningThatReturnedIsNotACut (third-eye review of T236 (a)): an opening that
// RETURNED a link committed it, whatever the step's bound says afterwards. It is a grant -- reported cut
// instead, the identity would hold a live link nobody was sent and nobody withdraws, and every later
// request of the owner would be answered kept for ResetTTL.
//
// MUTATION: `if cut() && err != nil` back to `if cut()` -> the identity reported stalled with a live
// link -> red.
func TestIssueForPerson_AnOpeningThatReturnedIsNotACut(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	w := newPersonResetWorld(t)
	r := cheapResets(t, lateOpeningDB{DB: d, after: 300 * time.Millisecond})
	r.mintLockWait = 50 * time.Millisecond

	p, err := r.IssueForPerson(ctx, w.typed)
	if err != nil {
		t.Fatalf("IssueForPerson: %v", err)
	}
	for _, st := range p.Stalled {
		if st.IdentityID == w.ident {
			t.Fatalf("the identity reported stalled although its opening returned a link (stalled %+v)", p.Stalled)
		}
	}
	if len(p.Grants) == 0 || p.Grants[0].IdentityID != w.ident {
		t.Fatalf("grants %+v; want the identity's link first", p.Grants)
	}
	if n := w.int(`SELECT count(*) FROM admin_identity_resets WHERE identity_id = $1`, w.ident); n != 1 {
		t.Fatalf("%d identity link(s), want the one granted", n)
	}
}
