package signup

// identity_db_test.go -- M14-08 (ADR 0031 §5.2 named exception 2, §8's dual-write): a registration
// through the REAL Provision path births the identity of a new address, leaves a known address's
// identity byte for byte as it was, and two registrations of one new address at once end in one
// identity and no failure (R13). Committed, through the fixture that holds internal/db's lock and
// takes the identities back (signup_db_test.go).

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// signupLink is what the owner reads of one registration's row and its identity.
type signupLink struct {
	identity               *uuid.UUID
	via                    *string
	rowDigest, identDigest *string
}

func readSignupLink(t *testing.T, owner *pgx.Conn, admin uuid.UUID) signupLink {
	t.Helper()
	var l signupLink
	if err := owner.QueryRow(context.Background(),
		`SELECT a.identity_id, a.linked_via, a.password_hash, i.password_hash
		   FROM admin_users a LEFT JOIN admin_identities i ON i.id = a.identity_id
		  WHERE a.id = $1`, admin).Scan(&l.identity, &l.via, &l.rowDigest, &l.identDigest); err != nil {
		t.Fatalf("read the registration's link: %v", err)
	}
	return l
}

// TestSignupFixtures_TakeTheLockInternalDBTakes: the lock's spelling is internal/db's, read from its
// source, or the two packages stop taking turns.
func TestSignupFixtures_TakeTheLockInternalDBTakes(t *testing.T) {
	src, err := os.ReadFile("../../db/operatorschema_test.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `const operatorTablesTestLock = "`+operatorTablesTestLock+`"`) {
		t.Fatalf("internal/db no longer names the %q lock", operatorTablesTestLock)
	}
}

// TestSignupProvision_ANewAddressBirthsItsIdentity is S13's real-path test: Provision -- the wizard's
// own call, db.WithTenant, no savepoint -- of an address nobody has births ONE identity for it, the
// row is linked (`signup`) and carries the identity's digest byte for byte (ADR 0031 §5.1's tested
// invariant: a linked row born by registration holds the identity's digest).
//
// MUTATION (measured, see the card's report): Provision's transaction made a SAVEPOINT (the identity
// call through db.WithTenantIn inside it) -> no identity, red here.
func TestSignupProvision_ANewAddressBirthsItsIdentity(t *testing.T) {
	p, _, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()

	d := draftFor(t, StructureSingle, "Front door")
	res, err := p.Provision(ctx, d)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	l := readSignupLink(t, owner, res.AdminUserID)
	if l.identity == nil || l.via == nil || *l.via != "signup" {
		t.Fatalf("a registration of a new address left its row unlinked (identity %v, via %v): the "+
			"dual-write did not run, or ran under a savepoint (S13)", l.identity, l.via)
	}
	if l.identDigest == nil || l.rowDigest == nil || *l.identDigest != *l.rowDigest {
		t.Fatal("the linked row's digest is not its identity's (ADR 0031 §5.1's invariant)")
	}
	var n int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_identities WHERE email = $1 AND email_verified_at IS NULL`,
		strings.ToUpper(d.Account.Email)).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d unverified identities for the address (%v), want exactly 1", n, err)
	}
}

// TestSignupProvision_AKnownAddressLeavesItsIdentityAsItWas: a registration of an address that is
// already an identity's -- with another password -- writes nothing to that identity (its digest byte
// for byte) and births its row UNLINKED; an address only another business's row carries births no
// identity at all. Either way the registration itself succeeds exactly as before.
func TestSignupProvision_AKnownAddressLeavesItsIdentityAsItWas(t *testing.T) {
	p, _, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()

	known := draftFor(t, StructureSingle, "Front door")
	first, err := p.Provision(ctx, known)
	if err != nil {
		t.Fatalf("the first registration: %v", err)
	}
	before := readSignupLink(t, owner, first.AdminUserID)
	if before.identity == nil {
		t.Fatal("PREMISE: the first registration of the address birthed no identity")
	}

	again := draftFor(t, StructureSingle, "Back door")
	again.Account.Email = strings.ToUpper(known.Account.Email)
	again.Account.Password = "another passphrase entirely"
	second, err := p.Provision(ctx, again)
	if err != nil {
		t.Fatalf("a registration of a known address: %v", err)
	}
	if l := readSignupLink(t, owner, second.AdminUserID); l.identity != nil {
		t.Fatalf("the second registration's row was linked to %s; a known address is born unlinked", *l.identity)
	}
	if after := readSignupLink(t, owner, first.AdminUserID); after.identDigest == nil || *after.identDigest != *before.identDigest {
		t.Fatal("the existing identity's digest changed")
	}

	// An address a business carries with no identity (a row the backfill would have grouped): no
	// identity is born for it by a later registration either.
	plantedEmail := "planted-" + uuid.NewString() + "@signup.example"
	plantedTenant := uuid.New()
	if _, err := owner.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
	                              VALUES ($1, 'Planted', $2, 'other', 'single')`, plantedTenant, "VAT-M1408P-"+plantedTenant.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, `INSERT INTO admin_users (tenant_id, full_name, email, password_hash, role)
	                              VALUES ($1, 'Planted', $2, $3, 'owner')`, plantedTenant, plantedEmail, "$2a$04$"+strings.Repeat("p", 53)); err != nil {
		t.Fatal(err)
	}
	third := draftFor(t, StructureSingle, "Side door")
	third.Account.Email = plantedEmail
	res, err := p.Provision(ctx, third)
	if err != nil {
		t.Fatalf("a registration of a planted address: %v", err)
	}
	if l := readSignupLink(t, owner, res.AdminUserID); l.identity != nil {
		t.Fatal("an address another business's row carries got an identity from a registration")
	}
	var n int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_identities WHERE email = $1`, plantedEmail).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d identities for the planted address (%v), want 0", n, err)
	}
}

// TestSignupProvision_ConcurrentRegistrationsOfOneNewAddress is R13: N registrations of one address
// nobody has, at once, in N businesses -- every one succeeds (no 500), exactly ONE identity is born
// and exactly one row is linked to it; the others are born unlinked. The address's namespaced lock
// serialises the N, ON CONFLICT (email) DO NOTHING is the second belt (00074's header).
//
// THE N ARE MADE TO OVERLAP, not left to the scheduler: each registration hashes its password first
// (~0.3 s of bcrypt), so N goroutines would otherwise reach the database one after another and the
// test would prove nothing about concurrency. A barrier holds them: the test's own transaction
// inserts, uncommitted, a business carrying each draft's VAT number, so every registration's first
// statement waits on that row (tenants_vat_number_key); once all N are seen waiting the barrier
// rolls back and the N run their transactions at the same instant.
//
// MUTATION (measured, see the card's report): the lock AND the ON CONFLICT removed from the body -> a
// registration fails with 23505 on admin_identities_email_key. Either one alone keeps it green: each
// is enough by itself, which is why there are two.
func TestSignupProvision_ConcurrentRegistrationsOfOneNewAddress(t *testing.T) {
	p, _, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()

	// Four, not more: each registration first pays a cost-12 bcrypt, ~11 s under -race on a CI
	// runner, and the four run on its two cores while this test holds the operator-tables lock.
	const n = 4
	email := "concurrent-" + uuid.NewString() + "@signup.example"
	drafts := make([]Draft, n)
	for i := range drafts {
		drafts[i] = draftFor(t, StructureSingle, "Front door")
		drafts[i].Account.Email = email
	}
	barrier, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// RELEASED ON EVERY EXIT, a failure included (CI run 39960777540): a registration still waiting on
	// the barrier holds a pool connection, and the pool's Close in the fixture's cleanup waits for it
	// -- a barrier left standing hangs the test until the binary's timeout. Deferred calls run before
	// the cleanups.
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
	var (
		wg     sync.WaitGroup
		admins = make([]uuid.UUID, n)
		errs   = make([]error, n)
	)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := p.Provision(ctx, drafts[i])
			admins[i], errs[i] = res.AdminUserID, err
		}(i)
	}
	// The barrier's waiters are counted on another connection (the barrier's is busy holding it).
	probe, err := pgx.Connect(ctx, os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := probe.Close(context.Background()); err != nil {
			t.Errorf("close the probe: %v", err)
		}
	}()
	deadline := time.Now().Add(4 * time.Minute)
	for waiting := 0; waiting < n; {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d registrations reached the barrier", waiting, n)
		}
		if err := probe.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE NOT granted AND locktype = 'transactionid'
		                                AND transactionid::text = $1`, barrierXID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	release()
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("registration %d of %d failed: %v", i+1, n, err)
		}
	}
	var identities, linked int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_identities WHERE email = $1`, email).Scan(&identities); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id
	                               WHERE a.id = ANY ($1) AND a.linked_via = 'signup' AND a.password_hash = i.password_hash`, admins).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if identities != 1 || linked != 1 {
		t.Fatalf("%d identities and %d linked rows after %d concurrent registrations of one new address, want 1 and 1", identities, linked, n)
	}
}

// TestSignupProvision_TheAddressLockWaitsForAnotherRegistration pins the address's advisory lock
// and its key's spelling (00074: hashtextextended('admin-identity-email:' || lower(email), 0)). A
// session holds that lock and, while a registration of the address waits on it, commits ANOTHER
// business's row carrying the address -- the shape of a concurrent registration that got there
// first. Released, the waiting registration must see that row and birth no identity (its own row
// unlinked). ON CONFLICT (email) cannot stand in for the lock here: there is no identity to conflict
// with, only a membership row the registration's "no other row" read would miss without the wait.
//
// MUTATION (measured): the lock dropped from the body (or its key spelled otherwise -- the address
// not lower-cased) -> the registration never waits and births an identity, red.
func TestSignupProvision_TheAddressLockWaitsForAnotherRegistration(t *testing.T) {
	p, _, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()
	d := draftFor(t, StructureSingle, "Front door")
	d.Account.Email = "Lock-" + uuid.NewString() + "@Signup.Example"

	holder, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	released := false
	release := func(commit bool) {
		if released {
			return
		}
		released = true
		var err error
		if commit {
			err = holder.Commit(context.Background())
		} else {
			err = holder.Rollback(context.Background())
		}
		if err != nil {
			t.Errorf("release the address lock: %v", err)
		}
	}
	defer release(false)
	var key int64
	if err := holder.QueryRow(ctx, `SELECT hashtextextended('admin-identity-email:' || lower($1), 0)`, d.Account.Email).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, key); err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		res Result
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := p.Provision(ctx, d)
		done <- outcome{res, err}
	}()

	probe, err := pgx.Connect(ctx, os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := probe.Close(context.Background()); err != nil {
			t.Errorf("close the probe: %v", err)
		}
	}()
	deadline := time.Now().Add(3 * time.Minute)
	for waiting := 0; waiting == 0; {
		select {
		case o := <-done:
			t.Fatalf("the registration finished (%v) without waiting for the address's lock", o.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the registration never waited on the address's lock")
		}
		if err := probe.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
		                                AND objsubid = 1 AND objid::bigint = ($1::bigint & 4294967295)`, key).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	other := uuid.New()
	if _, err := holder.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
	                               VALUES ($1, 'First there', $2, 'other', 'single')`, other, "VAT-M1408L-"+other.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := holder.Exec(ctx, `INSERT INTO admin_users (tenant_id, full_name, email, password_hash, role)
	                               VALUES ($1, 'First there', $2, $3, 'owner')`, other, strings.ToLower(d.Account.Email), "$2a$04$"+strings.Repeat("f", 53)); err != nil {
		t.Fatal(err)
	}
	release(true)

	o := <-done
	if o.err != nil {
		t.Fatalf("the waiting registration: %v", o.err)
	}
	if l := readSignupLink(t, owner, o.res.AdminUserID); l.identity != nil {
		t.Fatalf("the registration that waited linked its row to %s; the address was another business's first", *l.identity)
	}
	var n int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_identities WHERE email = $1`, d.Account.Email).Scan(&n); err != nil || n != 0 {
		t.Fatalf("%d identities for the address (%v), want 0", n, err)
	}
}
