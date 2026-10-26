package signup

// identity_db_test.go -- M14-17 (ADR 0031 §5.10, §5.2; migration 00078): a CONFIRMED registration
// through the REAL Provision path births the identity of a new address (verified, linked `claim`), an
// address that already has an identity is claimed only through that person's own session, and
// confirmations of one new address racing each other -- and racing the old pod's registration, which
// still calls identity_create_for_signup during this deploy's rollout (R8) -- end in ONE identity and no
// failure (S13). Committed, through the fixture that holds internal/db's lock and takes the identities
// and drafts back (signup_db_test.go).
//
// (M14-08's tests of the registration's dual-write lived here; Provision no longer calls
// identity_create_for_signup. The function stays until contract B and internal/db's
// identitysignup_test.go still holds it.)

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
	"github.com/atknatk/tappa/internal/store"
)

// signupLink is what the owner reads of one registration's row and its identity.
type signupLink struct {
	identity               *uuid.UUID
	via                    *string
	rowDigest, identDigest *string
	verified               bool
}

func readSignupLink(t *testing.T, owner *pgx.Conn, admin uuid.UUID) signupLink {
	t.Helper()
	var l signupLink
	if err := owner.QueryRow(context.Background(),
		`SELECT a.identity_id, a.linked_via, a.password_hash, i.password_hash, coalesce(i.email_verified_at IS NOT NULL, false)
		   FROM admin_users a LEFT JOIN admin_identities i ON i.id = a.identity_id
		  WHERE a.id = $1`, admin).Scan(&l.identity, &l.via, &l.rowDigest, &l.identDigest, &l.verified); err != nil {
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

// TestSignupProvision_ANewAddressBirthsItsIdentity is the real-path test (S13's and S2's): Provision --
// db.WithTenant, no savepoint -- of a confirmed draft for an address nobody has births ONE identity,
// its mailbox proved by the click, and the owner row is linked (`claim`) with the identity's digest
// byte for byte (ADR 0031 §5.1's invariant).
//
// MUTATION (measured, see the card's report): Provision's tenant written inside a SAVEPOINT
// (db.WithTenantIn) -> identity_claim_signup refuses (tenants.xmin is the subtransaction's), red here.
func TestSignupProvision_ANewAddressBirthsItsIdentity(t *testing.T) {
	p, _, owner := newProvisionerFixtureWithOwner(t)
	d := draftFor(t, StructureSingle, "Front door")
	res, err := provisionDraft(t, p, d)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	l := readSignupLink(t, owner, res.AdminUserID)
	if l.identity == nil || l.via == nil || *l.via != "claim" || !l.verified {
		t.Fatalf("a confirmed registration of a new address: identity %v via %v verified %v, want a verified identity via claim", l.identity, l.via, l.verified)
	}
	if l.identDigest == nil || l.rowDigest == nil || *l.identDigest != *l.rowDigest || *l.rowDigest != fixtureDigest {
		t.Fatal("the linked row's digest is not its identity's, or not the draft's")
	}
	var n int
	if err := owner.QueryRow(context.Background(), `SELECT count(*) FROM admin_identities WHERE email = $1`,
		strings.ToUpper(d.Account.Email)).Scan(&n); err != nil || n != 1 {
		t.Fatalf("%d identities for the address (%v), want exactly 1", n, err)
	}
}

// TestSignupProvision_AKnownAddressIsClaimedOnlyByItsPerson (R18): a draft for an address that already
// has an identity, confirmed as a new address (no session) -> ErrClaimRefused, NOTHING created (the
// business rolled back, the VAT number free) and the identity's digest byte for byte as it was; the
// same draft confirmed through a live session of that person -> the owner row is linked to the SAME
// identity, no second identity.
func TestSignupProvision_AKnownAddressIsClaimedOnlyByItsPerson(t *testing.T) {
	p, data, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()

	known := draftFor(t, StructureSingle, "Front door")
	first, err := provisionDraft(t, p, known)
	if err != nil {
		t.Fatalf("the first registration: %v", err)
	}
	before := readSignupLink(t, owner, first.AdminUserID)
	if before.identity == nil {
		t.Fatal("PREMISE: the first registration birthed no identity")
	}

	again := draftFor(t, StructureSingle, "Back door")
	again.Account.Email = strings.ToUpper(known.Account.Email)
	c := openClaim(t, data, again)
	if _, err := p.Provision(ctx, c); !errors.Is(err, ErrClaimRefused) {
		t.Fatalf("a known address confirmed without its person's session: %v, want ErrClaimRefused", err)
	}
	if after := readSignupLink(t, owner, first.AdminUserID); *after.identDigest != *before.identDigest {
		t.Fatal("the existing identity's digest changed")
	}
	var tenants int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM tenants WHERE vat_number = $1`, again.Business.VATNumber).Scan(&tenants); err != nil || tenants != 0 {
		t.Fatalf("%d businesses for the refused draft (%v), want 0", tenants, err)
	}

	// The person's own live session (a fresh one on the first business's owner row).
	session := strings.Repeat("ab", 32)
	if _, err := owner.Exec(ctx, `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES ($1, $2, $3)`,
		first.TenantID, first.AdminUserID, session); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := owner.Exec(context.Background(), `DELETE FROM admin_sessions WHERE token_hash = $1`, session); err != nil {
			t.Errorf("remove the fixture session: %v", err)
		}
	})
	c.session = &session
	res, err := p.Provision(ctx, c)
	if err != nil {
		t.Fatalf("the person's own confirmation: %v", err)
	}
	if l := readSignupLink(t, owner, res.AdminUserID); l.identity == nil || *l.identity != *before.identity || *l.via != "claim" {
		t.Fatalf("the claimed row is linked to %v via %v, want the person's identity %s via claim", l.identity, l.via, *before.identity)
	}
}

// TestSignupProvision_TheRealProofsReachTheClaim drives the production chain once end to end:
// adminauth.SignupClaims.Open (the shipped bcrypt cost), Resolve by the link's token, ProveWithPassword
// with the re-entered password, Provision with that proof.
func TestSignupProvision_TheRealProofsReachTheClaim(t *testing.T) {
	p, data, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()
	claims, err := adminauth.NewSignupClaims(data, &config.Config{SessionHMACKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	d := draftFor(t, StructureSingle, "Front door")
	draft, _, err := EncodeDraft(d)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := claims.Open(ctx, d.Account.Email, d.Account.Password, draft)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if grant.KnownAddress {
		t.Fatal("a new address reads as known")
	}
	link := grant.Link("https://app.example/admin/signup/confirm")
	c, err := claims.Resolve(ctx, adminauth.ParseSignupClaimToken(link[strings.Index(link, "?t=")+3:]))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	proof, ok := claims.ProveWithPassword(c, d.Account.Password)
	if !ok {
		t.Fatal("the chosen password did not prove the draft")
	}
	res, err := p.Provision(ctx, proof)
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}
	if l := readSignupLink(t, owner, res.AdminUserID); l.rowDigest == nil || !strings.HasPrefix(*l.rowDigest, "$2a$12$") {
		t.Fatal("the owner row's digest is not adminauth.Hash's at the shipped cost")
	}
}

// legacyRegistration is the OLD POD's registration during this deploy's rollout (R8): a business, its
// owner row and identity_create_for_signup, in one top-level transaction -- what M14-08's Provision did.
func legacyRegistration(ctx context.Context, data *db.DB, d Draft) (bool, error) {
	tenantID := uuid.New()
	var linked bool
	err := data.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		q := store.New(tx)
		if _, e := q.CreateTenant(ctx, store.CreateTenantParams{ID: tenantID, Name: d.Business.Name, VatNumber: d.Business.VATNumber,
			BusinessType: d.Business.BusinessType, Structure: d.Business.Structure, Timezone: DefaultTimezone}); e != nil {
			return e
		}
		email := d.Account.Email
		row, e := q.CreateAdminUser(ctx, store.CreateAdminUserParams{TenantID: tenantID, FullName: d.Account.FullName, Email: &email,
			PasswordHash: fixtureDigest, Role: "owner"})
		if e != nil {
			return e
		}
		linked, e = db.CreateIdentityForSignup(ctx, tx, tenantID, row.ID)
		return e
	})
	return linked, err
}

// TestSignupProvision_ConcurrentBirthsOfOneNewAddress is S13 (run with -race): N confirmations of
// drafts for ONE address nobody has, at once, in N businesses -- and, among them, the old pod's
// registration of the same address -- end in exactly ONE identity. Every confirmation that did not win
// is ErrClaimRefused with nothing created; the old pod's registration always commits (its row unlinked
// when it lost); no other error, i.e. no 500. The address's namespaced lock serialises them and ON
// CONFLICT (email) DO NOTHING is the second belt (00074, 00078).
//
// THE N ARE MADE TO OVERLAP: the test's own transaction inserts, uncommitted, a business carrying each
// draft's VAT number, so every registration's first statement waits on that row
// (tenants_vat_number_key); once all are seen waiting the barrier rolls back and they run at once.
//
// MUTATIONS (measured): the ON CONFLICT removed from identity_claim_signup -> a confirmation fails with
// 23505 on admin_identities_email_key (alone, and with the lock removed too): in the claim it is the ON
// CONFLICT that turns a lost race into a refusal -- there is no "identity exists" read before the
// INSERT. The lock removed alone stays green (the INSERT waits on the uncommitted row); the lock is
// what orders a confirmation against the old pod's identity_create_for_signup, whose "no other row"
// read ON CONFLICT cannot stand in for (TestSignupProvision_TheClaimWaitsOnTheAddressLock pins its key).
func TestSignupProvision_ConcurrentBirthsOfOneNewAddress(t *testing.T) {
	p, data, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()

	const claimsN = 5
	email := "concurrent-" + uuid.NewString() + "@signup.example"
	drafts := make([]Draft, claimsN+1)
	for i := range drafts {
		drafts[i] = draftFor(t, StructureSingle, "Front door")
		drafts[i].Account.Email = email
	}
	claimsOf := make([]testClaim, claimsN)
	for i := range claimsN {
		claimsOf[i] = openClaim(t, data, drafts[i])
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
	var (
		wg        sync.WaitGroup
		errs      = make([]error, claimsN)
		legacyOK  bool
		legacyErr error
	)
	for i := range claimsN {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = p.Provision(ctx, claimsOf[i])
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		legacyOK, legacyErr = legacyRegistration(ctx, data, drafts[claimsN])
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
	deadline := time.Now().Add(4 * time.Minute)
	for waiting := 0; waiting < claimsN+1; {
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d registrations reached the barrier", waiting, claimsN+1)
		}
		if err := probe.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE NOT granted AND locktype = 'transactionid'
		                                AND transactionid::text = $1`, barrierXID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	release()
	wg.Wait()

	if legacyErr != nil {
		t.Fatalf("the old pod's registration failed: %v", legacyErr)
	}
	won := 0
	for i, err := range errs {
		switch {
		case err == nil:
			won++
		case errors.Is(err, ErrClaimRefused):
		default:
			t.Fatalf("confirmation %d of %d failed with %v -- a 500, not a refusal", i+1, claimsN, err)
		}
	}
	if legacyOK {
		won++
	}
	var identities, linked int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_identities WHERE email = $1`, email).Scan(&identities); err != nil {
		t.Fatal(err)
	}
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id
	                               WHERE i.email = $1 AND a.password_hash = i.password_hash`, email).Scan(&linked); err != nil {
		t.Fatal(err)
	}
	if identities != 1 || linked != 1 || won != 1 {
		t.Fatalf("%d identities, %d linked rows and %d winners after %d concurrent births of one new address, want 1, 1 and 1",
			identities, linked, won, claimsN+1)
	}
}

// TestSignupProvision_TheClaimWaitsOnTheAddressLock pins the claim's lock and its key's spelling --
// 00074's, so a confirmation and the old pod's identity_create_for_signup exclude each other:
// hashtextextended('admin-identity-email:' || lower(email), 0). A session holds the lock and, while a
// confirmation of the address waits on it, commits an identity for the address; released, the
// confirmation is refused (ErrClaimRefused) and creates nothing.
//
// MUTATION (measured): the key spelled otherwise (the address not lower-cased) -> the confirmation never
// waits, red.
func TestSignupProvision_TheClaimWaitsOnTheAddressLock(t *testing.T) {
	p, data, owner := newProvisionerFixtureWithOwner(t)
	ctx := context.Background()
	d := draftFor(t, StructureSingle, "Front door")
	d.Account.Email = "Lock-" + uuid.NewString() + "@Signup.Example"
	c := openClaim(t, data, d)

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
		res, err := p.Provision(ctx, c)
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
			t.Fatalf("the confirmation finished (%v) without waiting for the address's lock", o.err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the confirmation never waited on the address's lock")
		}
		if err := probe.QueryRow(ctx, `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
		                                AND objsubid = 1 AND objid::bigint = ($1::bigint & 4294967295)`, key).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := holder.Exec(ctx, `INSERT INTO admin_identities (email, password_hash) VALUES ($1, $2)`,
		strings.ToLower(d.Account.Email), "$2a$04$"+strings.Repeat("f", 53)); err != nil {
		t.Fatal(err)
	}
	release(true)

	if o := <-done; !errors.Is(o.err, ErrClaimRefused) {
		t.Fatalf("the waiting confirmation: %v, want ErrClaimRefused", o.err)
	}
	var tenants int
	if err := owner.QueryRow(ctx, `SELECT count(*) FROM tenants WHERE vat_number = $1`, d.Business.VATNumber).Scan(&tenants); err != nil || tenants != 0 {
		t.Fatalf("%d businesses for the refused confirmation (%v), want 0", tenants, err)
	}
}
