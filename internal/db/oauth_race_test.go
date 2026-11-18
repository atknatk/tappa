package db

// oauth_race_test.go -- issue #37 phase 1's concurrency proofs (ADR 0047 §5.2, §5.3 and its
// "Uygulama notları"), through the shipped store statements on a real tappa_app pool, each racer in a
// WithTenant transaction of its own. Run them with -race.
//
// FIXTURES ARE COMMITTED (a race needs two transactions that see the same rows): a fresh business
// with an owner per test, an app in the registry, and one shared disabled operator account that the
// apps name as their registrar (found again by its address, so runs do not pile accounts up). Fresh
// random ids keep runs apart; nothing is cleaned -- audit_log and operator_audit_log are append-only
// elsewhere in this package's fixtures and these rows are inert (a disabled operator signs nobody in,
// an app with a random secret hash authenticates nobody). No row of `tags` is written.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/store"
	"github.com/atknatk/tappa/test/fixtures"
)

const oauthRaceOperatorEmail = "oauth-race-fixture@example.test"

// errLoserWaited is the code race's own refusal: the winner gave up waiting for every racer to run
// its statement, so at least one loser was blocked on the row's lock.
var errLoserWaited = errors.New("a loser waited for the lock: not every racer ran its statement within 10 s")

type oauthRaceWorld struct {
	app, owner *DB
	tenant     uuid.UUID
	admin      uuid.UUID
	client     uuid.UUID
}

func newOAuthRaceWorld(t *testing.T) oauthRaceWorld {
	t.Helper()
	ctx := context.Background()
	w := oauthRaceWorld{app: appDB(t), owner: ownerDB(t), tenant: uuid.New(), admin: uuid.New()}
	if err := w.app.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
		                           VALUES ($1, 'oauth race fixture', $2, 'restaurant', 'multi')`, w.tenant, "VAT-OAUTH-"+w.tenant.String()); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
		                        VALUES ($1, $2, 'OAuth Race Owner', $3, $4, 'owner', 'active')`,
			w.admin, w.tenant, "oauth-race-"+w.admin.String()+"@m11.example", fixtures.UnusablePasswordHash)
		return err
	}); err != nil {
		t.Fatalf("the business: %v", err)
	}
	var operator uuid.UUID
	if err := w.owner.pool.QueryRow(ctx, `
		WITH made AS (
		    INSERT INTO platform_admins (email, display_name, status)
		    SELECT $1, 'oauth race fixture', 'disabled'
		     WHERE NOT EXISTS (SELECT 1 FROM platform_admins WHERE email = $1)
		    RETURNING id)
		SELECT id FROM made UNION ALL SELECT id FROM platform_admins WHERE email = $1 LIMIT 1`, oauthRaceOperatorEmail).Scan(&operator); err != nil {
		t.Fatalf("the registrar: %v", err)
	}
	if err := w.owner.pool.QueryRow(ctx, `INSERT INTO oauth_clients (name, redirect_uris, allowed_scopes, secret_hash, secret_hint, created_by)
	                                      VALUES ('OAuth race app', $1, '{timesheets:read,employees:write}', $2, 'tt_cs_test_Race', $3) RETURNING id`,
		[]string{oauthGoodRedirect}, randHash(t), operator).Scan(&w.client); err != nil {
		t.Fatalf("the app: %v", err)
	}
	return w
}

// ownerCount reads one count as the owner (it bypasses RLS: the test's view of every row).
func (w oauthRaceWorld) ownerCount(t *testing.T, sql string, args ...any) int64 {
	t.Helper()
	var n int64
	if err := w.owner.pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
	return n
}

// liveTokens counts the grant's live refresh and access tokens (unused for a refresh, unrevoked and
// unexpired for both -- ADR 0047 §5.3's "canlı").
func (w oauthRaceWorld) liveTokens(t *testing.T, grant uuid.UUID) (refresh, access int64) {
	t.Helper()
	refresh = w.ownerCount(t, `SELECT count(*) FROM oauth_tokens WHERE grant_id = $1 AND kind = 'refresh' AND used_at IS NULL
	                            AND revoked_at IS NULL AND expires_at > clock_timestamp()`, grant)
	access = w.ownerCount(t, `SELECT count(*) FROM oauth_tokens WHERE grant_id = $1 AND kind = 'access'
	                           AND revoked_at IS NULL AND expires_at > clock_timestamp()`, grant)
	return refresh, access
}

// mintPair writes a refresh token and an access token for the grant in tx -- what a token response
// hands out.
func mintPair(ctx context.Context, q *store.Queries, tenant, grant uuid.UUID, scopes []string) (refresh, access uuid.UUID, err error) {
	r, err := q.InsertOAuthToken(ctx, store.InsertOAuthTokenParams{TenantID: tenant, GrantID: grant, Kind: "refresh", TokenHash: randHashNoT()})
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	a, err := q.InsertOAuthToken(ctx, store.InsertOAuthTokenParams{TenantID: tenant, GrantID: grant, Kind: "access", TokenHash: randHashNoT(), Scopes: scopes})
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return r.ID, a.ID, nil
}

// randHashNoT is randHash for a goroutine (t.Fatal must not run off the test's goroutine).
func randHashNoT() string { return strings.ReplaceAll(uuid.NewString()+uuid.NewString(), "-", "") }

// refreshOnce is §5.3's refresh, step for step, in one WithTenant transaction: the grant's lock, the
// one-statement rotation, the 30-second tolerance or the reuse, and a new pair. It answers the branch
// it took ("rotated", "tolerance", "reuse", "invalid_grant") and the new pair.
func (w oauthRaceWorld) refreshOnce(ctx context.Context, grant, presented uuid.UUID) (branch string, refresh, access uuid.UUID, err error) {
	err = w.app.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		g, err := q.LockLiveOAuthGrant(ctx, store.LockLiveOAuthGrantParams{TenantID: w.tenant, GrantID: grant})
		if errors.Is(err, pgx.ErrNoRows) {
			branch = "invalid_grant"
			return nil
		}
		if err != nil {
			return err
		}
		_, err = q.RotateOAuthRefreshToken(ctx, store.RotateOAuthRefreshTokenParams{TenantID: w.tenant, TokenID: presented})
		switch {
		case err == nil:
			branch = "rotated"
		case errors.Is(err, pgx.ErrNoRows):
			s, err := q.GetOAuthRefreshTokenState(ctx, store.GetOAuthRefreshTokenStateParams{TenantID: w.tenant, TokenID: presented})
			if err != nil {
				return err
			}
			switch {
			case s.WithinTolerance:
				if _, err := q.RevokeOtherLiveOAuthTokens(ctx, store.RevokeOtherLiveOAuthTokensParams{TenantID: w.tenant, GrantID: grant}); err != nil {
					return err
				}
				branch = "tolerance"
			case s.UsedAt != nil && s.RevokedAt == nil:
				_, err := q.RevokeOAuthGrant(ctx, store.RevokeOAuthGrantParams{Reason: "refresh_reuse", TenantID: w.tenant, ID: grant})
				branch = "reuse"
				return err
			default:
				branch = "invalid_grant"
				return nil
			}
		default:
			return err
		}
		refresh, access, err = mintPair(ctx, q, w.tenant, grant, g.Scopes)
		return err
	})
	return branch, refresh, access, err
}

// newGrant commits a live grant and its first pair.
func (w oauthRaceWorld) newGrant(t *testing.T) (grant, refresh, access uuid.UUID) {
	t.Helper()
	if err := w.app.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		g, err := q.InsertOAuthGrant(ctx, store.InsertOAuthGrantParams{TenantID: w.tenant, ClientID: w.client, Scopes: []string{"timesheets:read"}, CreatedBy: w.admin})
		if err != nil {
			return err
		}
		grant = g.ID
		refresh, access, err = mintPair(ctx, q, w.tenant, grant, []string{"timesheets:read"})
		return err
	}); err != nil {
		t.Fatalf("a grant: %v", err)
	}
	return grant, refresh, access
}

// TestOAuthCode_ConcurrentPresentationsSpendItOnce -- the ADR's "Uygulama notları" (aynı kodla N
// goroutine): N presentations of ONE code, all reaching the statement while the first exchange's
// transaction is open (the winner holds it until every racer has run its statement), spend it exactly
// once; every loser is answered "not reached" (the row was locked -- SKIP LOCKED, no wait) and none
// counts as a reuse, so the grant the winner commits is LIVE afterwards. A presentation AFTER that
// commit reaches the free, spent row and is the reuse: the grant it bore is revoked ('code_reuse').
func TestOAuthCode_ConcurrentPresentationsSpendItOnce(t *testing.T) {
	w := newOAuthRaceWorld(t)
	ctx := context.Background()
	var code uuid.UUID
	if err := w.app.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		r, err := store.New(tx).InsertOAuthCode(ctx, store.InsertOAuthCodeParams{TenantID: w.tenant, CodeHash: randHash(t), ClientID: w.client,
			CreatedBy: w.admin, RedirectUri: oauthGoodRedirect, Scopes: []string{"timesheets:read"}, CodeChallenge: strings.Repeat("R", 43)})
		code = r.ID
		return err
	}); err != nil {
		t.Fatalf("the code: %v", err)
	}
	const racers = 16
	var attempted, done sync.WaitGroup
	attempted.Add(racers)
	start := make(chan struct{})
	results := make([]store.ConsumeOAuthCodeRow, racers)
	errs := make([]error, racers)
	var grant uuid.UUID
	var gaveUp atomic.Bool
	for i := range racers {
		done.Add(1)
		go func() {
			defer done.Done()
			<-start
			errs[i] = w.app.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				q := store.New(tx)
				r, err := q.ConsumeOAuthCode(ctx, store.ConsumeOAuthCodeParams{CodeID: code, TenantID: w.tenant})
				results[i] = r
				attempted.Done()
				if err != nil || !r.Spent {
					return err
				}
				// The winner keeps its transaction -- and the row's lock -- open until every racer has
				// run its statement against it, but for 10 s at most: a loser that WAITS for the lock
				// (no SKIP LOCKED) never gets there, and the test must say so rather than run into the
				// server's idle-in-transaction timeout.
				// (Once one winner gave up, a loser that then gets the lock does not wait again: the mutant's
				// verdict comes after 10 s, not after 10 s per racer.)
				if gaveUp.Load() {
					return errLoserWaited
				}
				all := make(chan struct{})
				go func() { attempted.Wait(); close(all) }()
				select {
				case <-all:
				case <-time.After(10 * time.Second):
					gaveUp.Store(true)
					return errLoserWaited
				}
				g, err := q.InsertOAuthGrant(ctx, store.InsertOAuthGrantParams{TenantID: w.tenant, ClientID: w.client, Scopes: r.Scopes, CreatedBy: *r.CreatedBy})
				if err != nil {
					return err
				}
				grant = g.ID
				_, err = q.SetOAuthCodeGrant(ctx, store.SetOAuthCodeGrantParams{GrantID: g.ID, TenantID: w.tenant, CodeID: code})
				return err
			})
		}()
	}
	close(start)
	done.Wait()
	spent, notReached := 0, 0
	for i := range results {
		if errors.Is(errs[i], errLoserWaited) {
			t.Fatalf("racer %d won and %v -- the consumption must take the row FOR UPDATE SKIP LOCKED", i, errs[i])
		}
	}
	for i, r := range results {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		switch {
		case r.Spent && r.Reached:
			spent++
		case !r.Reached && !r.Spent && r.PriorGrantID == nil:
			notReached++
		default:
			t.Errorf("racer %d: %+v -- neither the winner nor a skipped loser", i, r)
		}
	}
	if spent != 1 || notReached != racers-1 {
		t.Fatalf("%d racers spent the code and %d were skipped; want exactly 1 and %d", spent, notReached, racers-1)
	}
	if n := w.ownerCount(t, `SELECT count(*) FROM oauth_grants WHERE id = $1 AND revoked_at IS NULL`, grant); n != 1 {
		t.Fatalf("the winner's grant is not live after the race (%d): a concurrent loser counted as a reuse", n)
	}
	// After the commit: a sequential presentation is the reuse.
	if err := w.app.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		r, err := q.ConsumeOAuthCode(ctx, store.ConsumeOAuthCodeParams{CodeID: code, TenantID: w.tenant})
		if err != nil {
			return err
		}
		if !r.Reached || r.Spent || r.PriorGrantID == nil || *r.PriorGrantID != grant {
			t.Fatalf("the presentation after the commit: %+v; want reached, not spent, naming the grant", r)
		}
		_, err = q.RevokeOAuthGrant(ctx, store.RevokeOAuthGrantParams{Reason: "code_reuse", TenantID: w.tenant, ID: grant})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := w.ownerCount(t, `SELECT count(*) FROM oauth_grants WHERE id = $1 AND revoked_reason = 'code_reuse'`, grant); n != 1 {
		t.Fatalf("the reuse did not revoke the grant the code bore")
	}
}

// TestOAuthRefresh_ConcurrentRotationsInsideTheWindowLeaveOnePair -- §5.3's "Eşzamanlılık kanıtı":
// N >= 3 presentations of one refresh token at once queue on the grant's lock; the first rotates it,
// every other one takes the tolerance -- N of N get a pair -- and at the end the grant has exactly ONE
// live refresh token and ONE live access token, born together (the last pair), while the presented
// token stays used and unrevoked.
func TestOAuthRefresh_ConcurrentRotationsInsideTheWindowLeaveOnePair(t *testing.T) {
	w := newOAuthRaceWorld(t)
	grant, r0, _ := w.newGrant(t)
	const racers = 5
	ctx := context.Background()
	start := make(chan struct{})
	var wg sync.WaitGroup
	branches := make([]string, racers)
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			branches[i], _, _, errs[i] = w.refreshOnce(ctx, grant, r0)
		}()
	}
	close(start)
	wg.Wait()
	rotated, tolerated := 0, 0
	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		switch branches[i] {
		case "rotated":
			rotated++
		case "tolerance":
			tolerated++
		default:
			t.Errorf("racer %d took %q", i, branches[i])
		}
	}
	if rotated != 1 || tolerated != racers-1 {
		t.Fatalf("%d rotations and %d tolerances, want 1 and %d (N of N get a pair)", rotated, tolerated, racers-1)
	}
	if r, a := w.liveTokens(t, grant); r != 1 || a != 1 {
		t.Fatalf("the grant has %d live refresh and %d live access tokens after %d concurrent refreshes, want 1 and 1", r, a, racers)
	}
	if n := w.ownerCount(t, `SELECT count(DISTINCT created_at) FROM oauth_tokens WHERE grant_id = $1 AND revoked_at IS NULL
	                          AND (kind = 'access' OR used_at IS NULL)`, grant); n != 1 {
		t.Errorf("the live refresh and access tokens were born in %d transactions, want one pair", n)
	}
	if n := w.ownerCount(t, `SELECT count(*) FROM oauth_tokens WHERE id = $1 AND used_at IS NOT NULL AND revoked_at IS NULL`, r0); n != 1 {
		t.Error("the presented refresh token is not used-and-unrevoked after the race")
	}
}

// TestOAuthRefresh_AnOldTokenInsideTheWindowKillsTheNewerLineage -- §5.3's lineage-free rule: after
// R0 -> R1 -> R2, presenting R0 within its 30 seconds revokes R2 and every access token and mints R3;
// R0 and R1 stay used and unrevoked; one live pair remains. A revoked token presented next is
// invalid_grant and the grant lives.
func TestOAuthRefresh_AnOldTokenInsideTheWindowKillsTheNewerLineage(t *testing.T) {
	w := newOAuthRaceWorld(t)
	ctx := context.Background()
	grant, r0, a0 := w.newGrant(t)
	step := func(presented uuid.UUID, want string) (uuid.UUID, uuid.UUID) {
		t.Helper()
		b, r, a, err := w.refreshOnce(ctx, grant, presented)
		if err != nil || b != want {
			t.Fatalf("presenting %s: branch %q (%v), want %q", presented, b, err, want)
		}
		return r, a
	}
	r1, _ := step(r0, "rotated")
	r2, a2 := step(r1, "rotated")
	r3, a3 := step(r0, "tolerance")
	if n := w.ownerCount(t, `SELECT count(*) FROM oauth_tokens WHERE id IN ($1, $2, $3) AND revoked_at IS NOT NULL`, r2, a2, a0); n != 3 {
		t.Errorf("%d of R2, A2 and A0 are revoked after R0's tolerance, want 3", n)
	}
	if n := w.ownerCount(t, `SELECT count(*) FROM oauth_tokens WHERE id IN ($1, $2) AND used_at IS NOT NULL AND revoked_at IS NULL`, r0, r1); n != 2 {
		t.Errorf("%d of R0 and R1 are used and unrevoked, want 2", n)
	}
	if n := w.ownerCount(t, `SELECT count(*) FROM oauth_tokens WHERE id IN ($1, $2) AND revoked_at IS NULL`, r3, a3); n != 2 {
		t.Error("the tolerance's pair is not live")
	}
	if r, a := w.liveTokens(t, grant); r != 1 || a != 1 {
		t.Fatalf("%d live refresh and %d live access tokens, want 1 and 1", r, a)
	}
	step(r2, "invalid_grant")
	if n := w.ownerCount(t, `SELECT count(*) FROM oauth_grants WHERE id = $1 AND revoked_at IS NULL`, grant); n != 1 {
		t.Error("presenting a revoked refresh token revoked the grant; it must only be invalid_grant")
	}
}

// TestOAuthGrant_TwoExchangesForOnePairMeetTheUniqueIndex -- O-8 / §5.2 step 7: two exchanges for the
// same (business, app) each supersede (finding nothing) and insert a grant; the second insert waits on
// oauth_grants_live_client_key for the first and, once it commits, fails 23505 -- its transaction rolls
// back, so exactly one live grant exists. A later exchange supersedes it.
func TestOAuthGrant_TwoExchangesForOnePairMeetTheUniqueIndex(t *testing.T) {
	w := newOAuthRaceWorld(t)
	ctx := context.Background()
	var superseded sync.WaitGroup
	superseded.Add(2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = w.app.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				q := store.New(tx)
				_, err := q.SupersedeLiveOAuthGrants(ctx, store.SupersedeLiveOAuthGrantsParams{TenantID: w.tenant, ClientID: w.client})
				superseded.Done()
				if err != nil {
					return err
				}
				superseded.Wait()
				_, err = q.InsertOAuthGrant(ctx, store.InsertOAuthGrantParams{TenantID: w.tenant, ClientID: w.client, Scopes: []string{"timesheets:read"}, CreatedBy: w.admin})
				return err
			})
		}()
	}
	wg.Wait()
	ok, unique := 0, 0
	for _, err := range errs {
		var pg *pgconn.PgError
		switch {
		case err == nil:
			ok++
		case errors.As(err, &pg) && pg.Code == sqlstateUniqueViolation && pg.ConstraintName == "oauth_grants_live_client_key":
			unique++
		default:
			t.Errorf("an exchange failed otherwise: %v", err)
		}
	}
	if ok != 1 || unique != 1 {
		t.Fatalf("%d exchanges committed and %d met the unique index, want 1 and 1", ok, unique)
	}
	if n := w.ownerCount(t, `SELECT count(*) FROM oauth_grants WHERE tenant_id = $1 AND client_id = $2 AND revoked_at IS NULL`, w.tenant, w.client); n != 1 {
		t.Fatalf("%d live grants for one (business, app), want 1", n)
	}
	var old []uuid.UUID
	if err := w.app.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		var err error
		if old, err = q.SupersedeLiveOAuthGrants(ctx, store.SupersedeLiveOAuthGrantsParams{TenantID: w.tenant, ClientID: w.client}); err != nil {
			return err
		}
		_, err = q.InsertOAuthGrant(ctx, store.InsertOAuthGrantParams{TenantID: w.tenant, ClientID: w.client, Scopes: []string{"timesheets:read"}, CreatedBy: w.admin})
		return err
	}); err != nil || len(old) != 1 {
		t.Fatalf("a later exchange superseded %d grants (%v), want 1", len(old), err)
	}
	if n := w.ownerCount(t, `SELECT count(*) FROM oauth_grants WHERE id = $1 AND revoked_reason = 'superseded'`, old[0]); n != 1 {
		t.Error("the superseded grant does not read revoked_reason = superseded")
	}
}
