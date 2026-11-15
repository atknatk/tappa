package appdevice

// db_test.go -- the verifier against a REAL Postgres (CLAUDE.md §8): the resolver, the nonce's
// one-statement consumption, RLS, the revocation's nonce kill and the lock order cannot be
// proven against a fake. Runs as tappa_app (NOBYPASSRLS, not the owner).
//
// FIXTURES ARE NOT CLEANED UP, for internal/session's reason: tappa_app cannot DELETE devices or
// employees, and teardown through the migration role is what redline R5 forbids. Every fixture
// uses fresh random UUIDs and fresh keys; the rows stay inert.
//
// Nothing here prints a key, a signature, a nonce or its hash: values are compared, never shown
// (§4.7, §7).

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"math/big"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/store"
)

func appDB(t *testing.T) *db.DB {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set; skipping appdevice DB tests (real Postgres required -- CLAUDE.md §8)")
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: config.NewDSN(raw)})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

// world is one tenant with a venue and an ACTIVE employee.
type world struct {
	tenant, venue, employee uuid.UUID
}

func newWorld(t *testing.T, d *db.DB) world {
	t.Helper()
	w := world{tenant: uuid.New(), venue: uuid.New(), employee: uuid.New()}
	err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure)
			 VALUES ($1, 'appdevice-test', $2, 'bar', 'single')`,
			w.tenant, "VAT-M1605-"+w.tenant.String()); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name) VALUES ($1, $2, 'loc')`, w.venue, w.tenant); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status)
			 VALUES ($1, $2, $3, 'Maria Borg', 'active')`, w.employee, w.tenant, w.venue)
		return e
	})
	if err != nil {
		t.Fatalf("newWorld: %v", err)
	}
	return w
}

// phone is one activated device: its id and the private key only the phone would hold.
type phone struct {
	id  uuid.UUID
	key *ecdsa.PrivateKey
	w   world
}

// addPhone registers a fresh key for w's employee through the product's query.
func addPhone(t *testing.T, d *db.DB, w world) phone {
	t.Helper()
	k := testKey(t)
	pub := rawPublic(t, k)
	if _, err := ParsePublicKey(pub); err != nil {
		t.Fatalf("a generated key fails the curve check: %v", err)
	}
	var id uuid.UUID
	err := d.WithTenant(context.Background(), w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		label := "iPhone"
		row, e := store.New(tx).CreateAppDevice(ctx, store.CreateAppDeviceParams{
			TenantID: w.tenant, EmployeeID: w.employee, Platform: "ios", PublicKey: pub, DeviceLabel: &label,
		})
		id = row.ID
		return e
	})
	if err != nil {
		t.Fatalf("create device: %v", err)
	}
	return phone{id: id, key: k, w: w}
}

func issue(t *testing.T, v *Verifier, p phone) Nonce {
	t.Helper()
	n, err := v.IssueNonce(context.Background(), p.id)
	if err != nil {
		t.Fatalf("IssueNonce: %v", err)
	}
	return n.Nonce
}

// signed is p's request over nonce n, signed by p's key.
func signed(t *testing.T, p phone, n string) SignedRequest {
	t.Helper()
	parts := defaultParts(p.id, n)
	return request(t, parts, sign(t, p.key, parts))
}

func revoke(t *testing.T, d *db.DB, p phone) {
	t.Helper()
	err := d.WithTenant(context.Background(), p.w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: p.id, TenantID: p.w.tenant})
		return e
	})
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
}

func dbVerifier(t *testing.T, d *db.DB) *Verifier {
	t.Helper()
	v, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// TestVerify_Table is the card's table against real Postgres: valid · wrong key · corrupt
// signature · replayed nonce · expired nonce · another device's nonce · another tenant's device
// · revoked device -- plus unknown device, a client-chosen nonce and the deactivated employee,
// whose device is NOT revoked and resolves (§5 row 4 is the caller's).
func TestVerify_Table(t *testing.T) {
	d := appDB(t)
	v := dbVerifier(t, d)
	ctx := context.Background()
	wA, wB := newWorld(t, d), newWorld(t, d)

	tests := []struct {
		name string
		// run returns the request to verify and the device it should resolve to on success.
		run  func(t *testing.T) (SignedRequest, phone)
		want error
	}{
		{name: "valid signature", run: func(t *testing.T) (SignedRequest, phone) {
			p := addPhone(t, d, wA)
			return signed(t, p, issue(t, v, p).Wire()), p
		}},
		{name: "wrong key", want: ErrBadSignature, run: func(t *testing.T) (SignedRequest, phone) {
			p := addPhone(t, d, wA)
			parts := defaultParts(p.id, issue(t, v, p).Wire())
			return request(t, parts, sign(t, testKey(t), parts)), p
		}},
		{name: "corrupt signature", want: ErrBadSignature, run: func(t *testing.T) (SignedRequest, phone) {
			p := addPhone(t, d, wA)
			parts := defaultParts(p.id, issue(t, v, p).Wire())
			sig := mustDecode(t, sign(t, p.key, parts))
			sig[len(sig)-1] ^= 0x01
			return request(t, parts, signatureEncoding.EncodeToString(sig)), p
		}},
		{name: "replayed nonce", want: ErrNonceRejected, run: func(t *testing.T) (SignedRequest, phone) {
			p := addPhone(t, d, wA)
			req := signed(t, p, issue(t, v, p).Wire())
			if _, err := v.Verify(ctx, req); err != nil {
				t.Fatalf("first use: %v", err)
			}
			return req, p
		}},
		{name: "expired nonce", want: ErrNonceRejected, run: func(t *testing.T) (SignedRequest, phone) {
			p := addPhone(t, d, wA)
			short := &Verifier{data: d, ttl: time.Second}
			n := issue(t, short, p)
			time.Sleep(1500 * time.Millisecond)
			return signed(t, p, n.Wire()), p
		}},
		{name: "another device's nonce", want: ErrNonceRejected, run: func(t *testing.T) (SignedRequest, phone) {
			p, q := addPhone(t, d, wA), addPhone(t, d, wA)
			return signed(t, p, issue(t, v, q).Wire()), p
		}},
		{name: "another tenant's device's nonce", want: ErrNonceRejected, run: func(t *testing.T) (SignedRequest, phone) {
			p, q := addPhone(t, d, wA), addPhone(t, d, wB)
			return signed(t, p, issue(t, v, q).Wire()), p
		}},
		{name: "another tenant's device resolves to its own tenant", run: func(t *testing.T) (SignedRequest, phone) {
			q := addPhone(t, d, wB)
			return signed(t, q, issue(t, v, q).Wire()), q
		}},
		{name: "revoked device", want: ErrRevokedDevice, run: func(t *testing.T) (SignedRequest, phone) {
			p := addPhone(t, d, wA)
			n := issue(t, v, p)
			revoke(t, d, p)
			return signed(t, p, n.Wire()), p
		}},
		{name: "unknown device", want: ErrUnknownDevice, run: func(t *testing.T) (SignedRequest, phone) {
			p := phone{id: uuid.New(), key: testKey(t), w: wA}
			return signed(t, p, randomNonceWire(t)), p
		}},
		{name: "nonce never issued", want: ErrNonceRejected, run: func(t *testing.T) (SignedRequest, phone) {
			p := addPhone(t, d, wA)
			return signed(t, p, randomNonceWire(t)), p
		}},
		{name: "deactivated employee's device resolves", run: func(t *testing.T) (SignedRequest, phone) {
			w := newWorld(t, d)
			p := addPhone(t, d, w)
			if err := d.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				_, e := store.New(tx).DeactivateEmployee(ctx, store.DeactivateEmployeeParams{TenantID: w.tenant, ID: w.employee})
				return e
			}); err != nil {
				t.Fatalf("deactivate: %v", err)
			}
			return signed(t, p, issue(t, v, p).Wire()), p
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, p := tc.run(t)
			got, err := v.Verify(ctx, req)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Verify: %v (%s)", err, reason(err))
				}
				want := Device{ID: p.id, TenantID: p.w.tenant, EmployeeID: p.w.employee}
				if got != want {
					t.Fatalf("Verify = %+v, want %+v", got, want)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Verify = %v (%s), want %s", err, reason(err), tc.want.(*AuthError).Reason())
			}
			if got != (Device{}) {
				t.Fatalf("a refusal carried a device: %+v", got)
			}
		})
	}
}

// TestVerify_RefusalWritesNothing: a refused request leaves last_used_at alone -- an unknown
// device and a revoked one write nothing (refused before the transaction), and a nonce refusal
// rolls the touch back with it. A success stamps it (positive control).
func TestVerify_RefusalWritesNothing(t *testing.T) {
	d := appDB(t)
	v := dbVerifier(t, d)
	ctx := context.Background()
	w := newWorld(t, d)
	lastUsed := func(p phone) *time.Time {
		var at *time.Time
		if err := d.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT last_used_at FROM app_devices WHERE tenant_id = $1 AND id = $2`, w.tenant, p.id).Scan(&at)
		}); err != nil {
			t.Fatalf("read last_used_at: %v", err)
		}
		return at
	}

	p := addPhone(t, d, w)
	if _, err := v.Verify(ctx, signed(t, p, randomNonceWire(t))); !errors.Is(err, ErrNonceRejected) {
		t.Fatalf("Verify = %v, want ErrNonceRejected", err)
	}
	if lastUsed(p) != nil {
		t.Fatal("a refused nonce left the device touched: the touch must roll back with the consumption")
	}
	bad := signed(t, phone{id: p.id, key: testKey(t), w: w}, issue(t, v, p).Wire())
	if _, err := v.Verify(ctx, bad); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("Verify = %v, want ErrBadSignature", err)
	}
	if lastUsed(p) != nil {
		t.Fatal("a bad signature touched the device")
	}
	if _, err := v.Verify(ctx, signed(t, p, issue(t, v, p).Wire())); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if lastUsed(p) == nil {
		t.Fatal("a verified request did not touch the device (positive control)")
	}
}

// TestVerify_NonceSurvivingRevocationIsRefused: the belt is load-bearing (M16-03 review). A
// revocation written WITHOUT the nonce kill -- the in-flight issue the revoke's snapshot cannot
// see leaves exactly this state -- keeps a live nonce. Verify refuses it at the resolver; the
// spend refuses it at TouchAppDevice even when handed the stale "live" resolution; and the
// consumption alone refuses it through EXISTS ... FOR SHARE.
func TestVerify_NonceSurvivingRevocationIsRefused(t *testing.T) {
	d := appDB(t)
	v := dbVerifier(t, d)
	ctx := context.Background()
	w := newWorld(t, d)
	p := addPhone(t, d, w)
	n := issue(t, v, p)
	stale, err := d.GetAppDeviceByID(ctx, p.id)
	if err != nil || stale.RevokedAt != nil {
		t.Fatalf("resolve before revocation: %v", err)
	}
	if err := d.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE app_devices SET revoked_at = now() WHERE tenant_id = $1 AND id = $2`, w.tenant, p.id)
		return e
	}); err != nil {
		t.Fatalf("revoke without the nonce kill: %v", err)
	}
	var live int
	if err := d.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM app_request_nonces WHERE tenant_id = $1 AND device_id = $2 AND consumed_at IS NULL`, w.tenant, p.id).Scan(&live)
	}); err != nil || live != 1 {
		t.Fatalf("the surviving nonce is not there (%d, %v): the test would prove nothing", live, err)
	}

	if _, err := v.Verify(ctx, signed(t, p, n.Wire())); !errors.Is(err, ErrRevokedDevice) {
		t.Fatalf("Verify = %v, want ErrRevokedDevice", err)
	}
	if err := v.spend(ctx, stale, n); !errors.Is(err, ErrRevokedDevice) {
		t.Fatalf("spend on a stale resolution = %v, want ErrRevokedDevice (TouchAppDevice)", err)
	}
	err = d.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		_, e := store.New(tx).ConsumeAppRequestNonce(ctx, store.ConsumeAppRequestNonceParams{
			TenantID: w.tenant, DeviceID: p.id, NonceHash: n.hash(),
		})
		return e
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("consumption alone = %v, want no rows (the EXISTS belt)", err)
	}
}

// TestVerify_HighSTwinOfASpentRequestIsRefused: signature malleability buys no replay. The
// (r, n-s) twin of a spent request VERIFIES (it passes checkSignature) and is refused by the
// nonce, which is what high-S acceptance relies on (key.go).
func TestVerify_HighSTwinOfASpentRequestIsRefused(t *testing.T) {
	d := appDB(t)
	v := dbVerifier(t, d)
	ctx := context.Background()
	p := addPhone(t, d, newWorld(t, d))
	parts := defaultParts(p.id, issue(t, v, p).Wire())
	text, err := CanonicalText(parts.method, parts.path, parts.body, parts.nonce, p.id.String())
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256Sum(text)
	r, s, err := ecdsa.Sign(rand.Reader, p.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	n := elliptic.P256().Params().N
	orig := request(t, parts, signatureEncoding.EncodeToString(derSeq(derInt(r), derInt(s))))
	twin := request(t, parts, signatureEncoding.EncodeToString(derSeq(derInt(r), derInt(new(big.Int).Sub(n, s)))))

	if _, err := v.Verify(ctx, orig); err != nil {
		t.Fatalf("original: %v", err)
	}
	dev, err := d.GetAppDeviceByID(ctx, p.id)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkSignature(dev, twin); err != nil {
		t.Fatalf("the twin must pass the signature check, or this test proves nothing: %v", err)
	}
	if _, err := v.Verify(ctx, twin); !errors.Is(err, ErrNonceRejected) {
		t.Fatalf("twin = %v, want ErrNonceRejected", err)
	}
}

// TestIssueNonce: unknown and revoked get the Verify refusals (one text); a live device gets a
// nonce stored only as its hash, expiring after the TTL by the database's clock; dead nonces are
// swept after the next issue, live ones are not.
func TestIssueNonce(t *testing.T) {
	d := appDB(t)
	v := dbVerifier(t, d)
	ctx := context.Background()
	w := newWorld(t, d)

	if _, err := v.IssueNonce(ctx, uuid.New()); !errors.Is(err, ErrUnknownDevice) {
		t.Fatalf("unknown device: %v", err)
	}
	if _, err := v.IssueNonce(ctx, uuid.Nil); !errors.Is(err, ErrMalformed) {
		t.Fatalf("nil device: %v", err)
	}
	gone := addPhone(t, d, w)
	revoke(t, d, gone)
	if _, err := v.IssueNonce(ctx, gone.id); !errors.Is(err, ErrRevokedDevice) {
		t.Fatalf("revoked device: %v", err)
	}

	p := addPhone(t, d, w)
	before := time.Now()
	first, err := v.IssueNonce(ctx, p.id)
	if err != nil {
		t.Fatal(err)
	}
	if lead := first.ExpiresAt.Sub(before); lead < NonceTTL-5*time.Second || lead > NonceTTL+5*time.Second {
		t.Fatalf("ExpiresAt is %v after issue, want about %v", lead, NonceTTL)
	}
	type row struct {
		hash     string
		consumed bool
	}
	rows := func() []row {
		var out []row
		if err := d.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
			rs, e := tx.Query(ctx, `SELECT nonce_hash, consumed_at IS NOT NULL FROM app_request_nonces WHERE tenant_id = $1 AND device_id = $2`, w.tenant, p.id)
			if e != nil {
				return e
			}
			defer rs.Close()
			for rs.Next() {
				var r row
				if e := rs.Scan(&r.hash, &r.consumed); e != nil {
					return e
				}
				out = append(out, r)
			}
			return rs.Err()
		}); err != nil {
			t.Fatal(err)
		}
		return out
	}
	got := rows()
	if len(got) != 1 || got[0].hash != first.Nonce.hash() || got[0].hash == first.Nonce.Wire() {
		t.Fatalf("stored nonce rows: %d; the one row must hold the hash, not the wire value", len(got))
	}

	if _, err := v.Verify(ctx, signed(t, p, first.Nonce.Wire())); err != nil {
		t.Fatalf("spend: %v", err)
	}
	second := issue(t, v, p)
	third := issue(t, v, p)
	got = rows()
	if len(got) != 2 {
		t.Fatalf("after spending one and issuing two, %d rows remain; want the two live ones (the spent one swept)", len(got))
	}
	for _, r := range got {
		if r.consumed || (r.hash != second.hash() && r.hash != third.hash()) {
			t.Fatal("a swept nonce survived, or a live one was swept")
		}
	}
}

// TestVerify_ConcurrentReplay_ExactlyOne: one signed request sent N times at once -- exactly one
// is accepted, every other is ErrNonceRejected, and nothing else happens (§8's replay race, -race).
func TestVerify_ConcurrentReplay_ExactlyOne(t *testing.T) {
	d := appDB(t)
	v := dbVerifier(t, d)
	p := addPhone(t, d, newWorld(t, d))
	req := signed(t, p, issue(t, v, p).Wire())

	const n = 24
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = v.Verify(context.Background(), req)
		}()
	}
	close(start)
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrNonceRejected):
		default:
			t.Fatalf("unexpected outcome: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d of %d concurrent replays were accepted, want exactly 1", ok, n)
	}
}

// TestVerify_ConcurrentRequestsOfOneDevice_AllSpend: rule D2 -- Touch before consume. N requests
// of ONE device over N different nonces, at once: every one is accepted and none deadlocks
// (consume-then-touch upgraded a share lock and gave 40P01, M16-03's re-check).
func TestVerify_ConcurrentRequestsOfOneDevice_AllSpend(t *testing.T) {
	d := appDB(t)
	v := dbVerifier(t, d)
	p := addPhone(t, d, newWorld(t, d))
	// MaxLiveNonces, not more (M16-07): the cap drops the oldest live nonce past it, so a device
	// holding more than the cap at once is exactly the case the cap exists to end.
	const n = MaxLiveNonces
	reqs := make([]SignedRequest, n)
	for i := range reqs {
		reqs[i] = signed(t, p, issue(t, v, p).Wire())
	}
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = v.Verify(context.Background(), reqs[i])
		}()
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("request %d: %v (deadlock: %v)", i, err, isDeadlock(err))
		}
	}
}

// TestVerify_RaceWithRevoke_NoDeadlockAndNothingAfter: rule D1 and the lock order, under load.
// Each round runs verifies, issues and ONE revoke of the same device at once. The revoke must
// never fail (the measured D1 victim was the revoke -- a stolen phone could keep it failing),
// nothing deadlocks, and once the revoke has committed no nonce of that device is spendable and
// none is issued.
func TestVerify_RaceWithRevoke_NoDeadlockAndNothingAfter(t *testing.T) {
	d := appDB(t)
	v := dbVerifier(t, d)
	ctx := context.Background()
	w := newWorld(t, d)
	const rounds, verifies, issues = 12, 6, 6

	for round := range rounds {
		p := addPhone(t, d, w)
		reqs := make([]SignedRequest, verifies)
		late := make([]SignedRequest, 2) // signed over nonces issued before the revoke, sent after it
		for i := range reqs {
			reqs[i] = signed(t, p, issue(t, v, p).Wire())
		}
		for i := range late {
			late[i] = signed(t, p, issue(t, v, p).Wire())
		}
		// DEAD nonces before the race: D1 needs the sweep to have rows to lock. Without them a
		// sweep-before-issue mutation deadlocked one run in three; with them it deadlocks in
		// every run measured (the mutation note in the report).
		for range 3 {
			if _, err := v.Verify(ctx, signed(t, p, issue(t, v, p).Wire())); err != nil {
				t.Fatalf("round %d: spend before the race: %v", round, err)
			}
		}

		var wg sync.WaitGroup
		start := make(chan struct{})
		var mu sync.Mutex
		var failures []error
		fail := func(err error) { mu.Lock(); failures = append(failures, err); mu.Unlock() }

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			err := d.WithTenant(ctx, w.tenant, func(ctx context.Context, tx pgx.Tx) error {
				_, e := store.New(tx).RevokeAppDevice(ctx, store.RevokeAppDeviceParams{ID: p.id, TenantID: w.tenant})
				return e
			})
			if err != nil {
				fail(errors.New("the revoke failed (deadlock " + boolText(isDeadlock(err)) + ")"))
			}
		}()
		for i := range verifies {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := v.Verify(ctx, reqs[i])
				if err != nil && !errors.Is(err, ErrRevokedDevice) && !errors.Is(err, ErrNonceRejected) {
					fail(err)
				}
			}()
		}
		for range issues {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := v.IssueNonce(ctx, p.id)
				if err != nil && !errors.Is(err, ErrRevokedDevice) {
					fail(err)
				}
			}()
		}
		close(start)
		wg.Wait()
		for _, err := range failures {
			t.Errorf("round %d: %v (deadlock: %v)", round, err, isDeadlock(err))
		}
		for _, req := range late {
			if _, err := v.Verify(ctx, req); !errors.Is(err, ErrRevokedDevice) {
				t.Errorf("round %d: a nonce issued before the revoke, spent after it: %v", round, err)
			}
		}
		if _, err := v.IssueNonce(ctx, p.id); !errors.Is(err, ErrRevokedDevice) {
			t.Errorf("round %d: a nonce was issued after the revoke: %v", round, err)
		}
		if t.Failed() {
			return
		}
	}
}

func boolText(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// liveNonces counts p's unspent, unexpired nonces as the tenant (RLS on).
func liveNonces(t *testing.T, d *db.DB, p phone) int {
	t.Helper()
	var n int32
	err := d.WithTenant(context.Background(), p.w.tenant, func(ctx context.Context, tx pgx.Tx) error {
		var e error
		n, e = store.New(tx).CountLiveAppRequestNonces(ctx, store.CountLiveAppRequestNoncesParams{TenantID: p.w.tenant, DeviceID: p.id})
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return int(n)
}

// TestIssueNonce_TheCapDropsTheOldestAndNeverRefuses: M16-07's live-nonce cap. Past
// MaxLiveNonces every issue still SUCCEEDS (a refusal would let whoever can sign for the device
// starve the real phone) and the oldest live nonces are the ones dropped: the newest cap's worth
// verify, the first issued does not.
func TestIssueNonce_TheCapDropsTheOldestAndNeverRefuses(t *testing.T) {
	d := appDB(t)
	v := dbVerifier(t, d)
	p := addPhone(t, d, newWorld(t, d))
	var wires []string
	for range MaxLiveNonces + 3 {
		wires = append(wires, issue(t, v, p).Wire())
	}
	if n := liveNonces(t, d, p); n != MaxLiveNonces {
		t.Fatalf("%d live nonces after %d issues, want the cap %d", n, len(wires), MaxLiveNonces)
	}
	if _, err := v.Verify(context.Background(), signed(t, p, wires[0])); !errors.Is(err, ErrNonceRejected) {
		t.Fatalf("the oldest nonce: %v, want ErrNonceRejected (dropped by the cap)", err)
	}
	for _, w := range wires[len(wires)-MaxLiveNonces:] {
		if _, err := v.Verify(context.Background(), signed(t, p, w)); err != nil {
			t.Fatalf("one of the newest %d nonces was refused: %v", MaxLiveNonces, err)
		}
	}
}

// TestIssueNonce_ConcurrentIssuesStayUnderTheCap: the cap is atomic because the device row is
// locked in a PREVIOUS statement (LockAppDeviceForNonce) -- N concurrent issues of one device all
// succeed, none deadlocks, and the device ends with at most MaxLiveNonces live nonces. A count
// folded into the INSERT was measured exceeding a cap of 1 (M16-03); this is the shape the card
// asked for instead.
func TestIssueNonce_ConcurrentIssuesStayUnderTheCap(t *testing.T) {
	d := appDB(t)
	v := dbVerifier(t, d)
	p := addPhone(t, d, newWorld(t, d))
	const n = 4 * MaxLiveNonces
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = v.IssueNonce(context.Background(), p.id)
		}()
	}
	close(start)
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("issue %d: %v (deadlock: %v)", i, err, isDeadlock(err))
		}
	}
	if live := liveNonces(t, d, p); live > MaxLiveNonces || live < 1 {
		t.Fatalf("%d live nonces after %d concurrent issues, want 1..%d", live, n, MaxLiveNonces)
	}
}

// TestRevoke_TheDevicesOwnSignOut: Verifier.Revoke stamps the device once (a repeat is
// idempotent), kills its nonces in the same statement, and from then on the device is refused
// before its signature -- and is given no nonce.
func TestRevoke_TheDevicesOwnSignOut(t *testing.T) {
	d := appDB(t)
	v := dbVerifier(t, d)
	p := addPhone(t, d, newWorld(t, d))
	n := issue(t, v, p)
	dev := Device{ID: p.id, TenantID: p.w.tenant, EmployeeID: p.w.employee}
	if err := v.Revoke(context.Background(), dev); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if err := v.Revoke(context.Background(), dev); err != nil {
		t.Fatalf("a second Revoke: %v (want idempotent)", err)
	}
	if live := liveNonces(t, d, p); live != 0 {
		t.Fatalf("%d live nonces after the revocation, want 0", live)
	}
	if _, err := v.Verify(context.Background(), signed(t, p, n.Wire())); !errors.Is(err, ErrRevokedDevice) {
		t.Fatalf("Verify after Revoke: %v, want ErrRevokedDevice", err)
	}
	if _, err := v.IssueNonce(context.Background(), p.id); !errors.Is(err, ErrRevokedDevice) {
		t.Fatalf("IssueNonce after Revoke: %v, want ErrRevokedDevice", err)
	}
	if err := v.Revoke(context.Background(), Device{}); err == nil {
		t.Fatal("Revoke of a zero device succeeded")
	}
}
