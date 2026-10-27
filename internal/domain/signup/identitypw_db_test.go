package signup

// identitypw_db_test.go -- M14-17b (ADR 0031 §5.10, §5.2; migration 00080, the user's decision of
// 2026-10-27): an identity whose memberships are all inactive registers a new business by confirming the
// draft with ITS password. Through the REAL Provision (db.WithTenant, top level), committed, through the
// fixture that holds internal/db's lock and takes the identities, their events and the drafts back.
//
//   - the production chain once: Open, Resolve, ProveWithIdentityPassword, Provision;
//   - a reset that commits while the claim waits on the person's lock refuses the claim (the digest the
//     application compared is no longer the identity's -- identity_open_session's rule);
//   - the claim waits on the address's namespaced lock (S13, 00074's key);
//   - concurrent password claims of one identity, in N businesses: ONE wins, the rest are refused (the
//     winner's owner row is an active membership -- from then on the person signs in), no 500 (S13: the
//     person's lock orders them).

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// plantSignInless commits an identity for email with fixtureDigest and ONE disabled membership: it cannot
// sign in. Unverified, so a claim writes its mailbox proof. The fixture's cleanup takes it back.
func plantSignInless(t *testing.T, owner *pgx.Conn, email string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	var identity uuid.UUID
	if err := owner.QueryRow(ctx, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2) RETURNING id`,
		email, fixtureDigest).Scan(&identity); err != nil {
		t.Fatalf("plant the identity: %v", err)
	}
	tenant := uuid.New()
	if _, err := owner.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure) VALUES ($1, 'Lapsed', $2, 'other', 'single')`,
		tenant, "VAT-M1417B-"+tenant.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO admin_users (tenant_id, full_name, email, password_hash, role, status, identity_id, linked_via, linked_at)
	                              VALUES ($1, 'Lapsed Owner', $2, $3, 'owner', 'disabled', $4, 'backfill', now())`,
		tenant, email, fixtureDigest, identity); err != nil {
		t.Fatalf("plant the disabled membership: %v", err)
	}
	return identity
}

// signInlessClaim opens d's draft (d's address is the identity's) and proves it as the production chain
// would after the password matched: the identity's digest, read back by the one e-mail -> identity path.
func signInlessClaim(t *testing.T, data *db.DB, d Draft) testClaim {
	t.Helper()
	c := openClaim(t, data, d)
	r, err := data.ResolveIdentityByEmail(context.Background(), d.Account.Email)
	if err != nil || r.HasActiveMembership {
		t.Fatalf("PREMISE: the address's identity (%v), active membership %v", err, r.HasActiveMembership)
	}
	c.verified = r.PasswordHash
	return c
}

func probeConn(t *testing.T) *pgx.Conn {
	t.Helper()
	probe, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := probe.Close(context.Background()); err != nil {
			t.Errorf("close the probe: %v", err)
		}
	})
	return probe
}

// TestSignupProvision_ASignInlessIdentityClaimsWithItsPassword drives the production chain once end to
// end: adminauth.SignupClaims.Open (the shipped bcrypt cost), Resolve (the draft is the identity-password
// kind), ProveWithIdentityPassword -- the draft's own password proves nothing, the identity's does -- and
// Provision: the owner row is linked (`claim`) to THAT identity with its digest, no second identity, the
// mailbox proved.
func TestSignupProvision_ASignInlessIdentityClaimsWithItsPassword(t *testing.T) {
	p, data, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()
	d := draftFor(t, StructureSingle, "Front door")
	identity := plantSignInless(t, owner, strings.ToLower(d.Account.Email))
	claims, err := adminauth.NewSignupClaims(data, &config.Config{SessionHMACKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	draft, _, err := EncodeDraft(d)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := claims.Open(ctx, d.Account.Email, "the draft's own password", draft)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !grant.KnownAddress {
		t.Fatal("a sign-in's address reads as new")
	}
	link := grant.Link("https://app.example/admin/signup/confirm")
	c, err := claims.Resolve(ctx, adminauth.ParseSignupClaimToken(link[strings.Index(link, "?t=")+3:]))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !c.IdentityPassword {
		t.Fatal("the draft of a sign-in with no active membership does not ask for its password")
	}
	if _, ok := claims.ProveWithIdentityPassword(c, "the draft's own password"); ok {
		t.Fatal("the draft's password proved a sign-in's draft")
	}
	proof, ok := claims.ProveWithIdentityPassword(c, fixturePassword)
	if !ok {
		t.Fatal("the identity's password did not prove the draft")
	}
	res, err := p.Provision(ctx, proof)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	l := readSignupLink(t, owner, res.AdminUserID)
	if l.identity == nil || *l.identity != identity || l.via == nil || *l.via != "claim" || !l.verified ||
		l.rowDigest == nil || *l.rowDigest != fixtureDigest {
		t.Fatalf("the owner row: identity %v via %v verified %v, want the planted identity via claim, verified, its digest", l.identity, l.via, l.verified)
	}
	var n int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_identities WHERE email = $1`, d.Account.Email).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d identities for the address (%v), want 1", n, err)
	}
}

// TestSignupProvision_AResetWhileThePasswordClaimWaitsRefusesIt is the 2026-10-23 rule on this path: the
// application compared the password with the identity's digest; before the claim runs, another session
// holds the person's row lock and changes the digest (a reset), then commits. The waiting claim
// re-evaluates its `password_hash = p_verified_digest` predicate on the committed row (READ COMMITTED) and
// is refused -- ErrClaimRefused, NOTHING created. The control: the holder rolls back instead and the same
// claim goes through.
//
// MUTATION (measured, see the card's report): the digest predicate removed from 00080's branch -> the
// claim succeeds after the reset, red.
func TestSignupProvision_AResetWhileThePasswordClaimWaitsRefusesIt(t *testing.T) {
	p, data, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()
	probe := probeConn(t)

	run := func(t *testing.T, reset bool) error {
		t.Helper()
		d := draftFor(t, StructureSingle, "Front door")
		identity := plantSignInless(t, owner, strings.ToLower(d.Account.Email))
		c := signInlessClaim(t, data, d)
		holder, err := owner.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		done := false
		defer func() {
			if !done {
				_ = holder.Rollback(context.Background())
			}
		}()
		if _, err := holder.Exec(ctx, `SELECT 1 FROM admin_identities WHERE id = $1 FOR UPDATE`, identity); err != nil {
			t.Fatal(err)
		}
		var xid string
		if err := holder.QueryRow(ctx, `SELECT pg_current_xact_id()::xid::text`).Scan(&xid); err != nil {
			t.Fatal(err)
		}
		out := make(chan error, 1)
		go func() {
			_, err := p.Provision(ctx, c)
			out <- err
		}()
		deadline := time.Now().Add(3 * time.Minute)
		for waiting := 0; waiting == 0; {
			select {
			case err := <-out:
				t.Fatalf("the claim finished (%v) without waiting for the person's lock", err)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatal("the claim never waited on the person's lock")
			}
			if err := probe.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE NOT granted AND locktype = 'transactionid'
			                                AND transactionid::text = $1`, xid).Scan(&waiting); err != nil {
				t.Fatal(err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if reset {
			if _, err := holder.Exec(ctx, `UPDATE admin_identities SET password_hash = $2 WHERE id = $1`, identity, "$2a$04$"+strings.Repeat("r", 53)); err != nil {
				t.Fatal(err)
			}
			err = holder.Commit(ctx)
		} else {
			err = holder.Rollback(ctx)
		}
		done = true
		if err != nil {
			t.Fatal(err)
		}
		claimErr := <-out
		var tenants int
		if err := owner.QueryRow(ctx, `SELECT count(*) FROM tenants WHERE vat_number = $1`, d.Business.VATNumber).Scan(&tenants); err != nil {
			t.Fatal(err)
		}
		if (claimErr == nil) != (tenants == 1) {
			t.Fatalf("the claim answered %v and %d business(es) exist", claimErr, tenants)
		}
		return claimErr
	}
	t.Run("the reset commits", func(t *testing.T) {
		if err := run(t, true); !errors.Is(err, ErrClaimRefused) {
			t.Fatalf("the claim after a reset: %v, want ErrClaimRefused", err)
		}
	})
	t.Run("control: the holder rolls back", func(t *testing.T) {
		if err := run(t, false); err != nil {
			t.Fatalf("the claim with the digest unchanged: %v", err)
		}
	})
}

// TestSignupProvision_ThePasswordClaimWaitsOnTheAddressLock pins this branch's lock and its key's spelling
// -- 00074's, hashtextextended('admin-identity-email:' || lower(email), 0) -- so it is ordered against a
// new address's birth of the same address (S13). A session holds the lock; the claim waits on it; released,
// the claim goes through.
//
// MUTATION (measured): the PERFORM of the lock removed from 00080's branch -> the claim never waits, red.
func TestSignupProvision_ThePasswordClaimWaitsOnTheAddressLock(t *testing.T) {
	p, data, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()
	probe := probeConn(t)
	d := draftFor(t, StructureSingle, "Front door")
	d.Account.Email = "PwLock-" + uuid.NewString() + "@Signup.Example"
	plantSignInless(t, owner, strings.ToLower(d.Account.Email))
	c := signInlessClaim(t, data, d)

	holder, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	defer func() {
		if !released {
			_ = holder.Rollback(context.Background())
		}
	}()
	var key int64
	if err := holder.QueryRow(ctx, `SELECT hashtextextended('admin-identity-email:' || lower($1), 0)`, d.Account.Email).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
		t.Fatal(err)
	}
	out := make(chan error, 1)
	go func() {
		_, err := p.Provision(ctx, c)
		out <- err
	}()
	deadline := time.Now().Add(3 * time.Minute)
	for waiting := 0; waiting == 0; {
		select {
		case err := <-out:
			t.Fatalf("the claim finished (%v) without waiting for the address's lock", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the claim never waited on the address's lock")
		}
		if err := probe.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
		                                AND objsubid = 1 AND objid::bigint = ($1::bigint & 4294967295)`, key).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	released = true
	if err := <-out; err != nil {
		t.Fatalf("the claim after the lock was released: %v", err)
	}
}

// TestSignupProvision_ConcurrentPasswordClaimsOfOneIdentity is S13 for this branch (run with -race): N
// drafts of ONE sign-in-less identity, in N businesses, confirmed at once. The person's lock orders them:
// the first to take it claims, and its owner row is an ACTIVE membership, so every later one re-checks
// "no active membership" under the same lock and is refused -- from then on the person signs in and claims
// with a session. Exactly one winner, every other confirmation ErrClaimRefused with nothing created, no
// other error (no 500, no deadlock), one identity, one claimed membership, one email_verified event.
//
// MUTATION (measured): the "no active membership" test removed -> all N claim, red.
//
// THE N ARE MADE TO OVERLAP as in TestSignupProvision_ConcurrentBirthsOfOneNewAddress: the test's own
// transaction holds, uncommitted, a business carrying each draft's VAT number.
func TestSignupProvision_ConcurrentPasswordClaimsOfOneIdentity(t *testing.T) {
	p, data, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()
	const n = 3
	email := "concurrent-pw-" + uuid.NewString() + "@signup.example"
	identity := plantSignInless(t, owner, email)
	drafts := make([]Draft, n)
	claimsOf := make([]testClaim, n)
	for i := range drafts {
		drafts[i] = draftFor(t, StructureSingle, "Front door")
		drafts[i].Account.Email = email
		claimsOf[i] = signInlessClaim(t, data, drafts[i])
	}
	barrier, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		if err := barrier.Rollback(context.Background()); err != nil {
			t.Errorf("release the barrier: %v", err)
		}
	}
	defer release()
	for _, d := range drafts {
		if _, err := barrier.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
		                                VALUES ($1, 'Barrier', $2, 'other', 'single')`, uuid.New(), d.Business.VATNumber); err != nil {
			t.Fatalf("barrier: %v", err)
		}
	}
	var barrierXID string
	if err := barrier.QueryRow(ctx, `SELECT pg_current_xact_id()::xid::text`).Scan(&barrierXID); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = p.Provision(ctx, claimsOf[i])
		}(i)
	}
	probe := probeConn(t)
	deadline := time.Now().Add(4 * time.Minute)
	for waiting := 0; waiting < n; {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d confirmations reached the barrier", waiting, n)
		}
		if err := probe.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE NOT granted AND locktype = 'transactionid'
		                                AND transactionid::text = $1`, barrierXID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	release()
	wg.Wait()
	won := 0
	for i, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, ErrClaimRefused):
		default:
			t.Fatalf("confirmation %d of %d failed with %v -- a 500, not a refusal", i+1, n, err)
		}
	}
	var identities, linked, events int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_identities WHERE email = $1`, email).Scan(&identities); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_users WHERE identity_id = $1 AND linked_via = 'claim' AND status = 'active'`,
		identity).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_identity_events WHERE identity_id = $1 AND event = 'email_verified'`,
		identity).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if identities != 1 || won != 1 || linked != 1 || events != 1 {
		t.Fatalf("%d identities, %d winners, %d claimed memberships and %d email_verified events, want 1 of each", identities, won, linked, events)
	}
}
