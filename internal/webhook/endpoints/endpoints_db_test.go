package endpoints

// endpoints_db_test.go -- the owner's endpoint acts against REAL Postgres: tappa_app through db.DB
// (DATABASE_URL), the owner role (DATABASE_MIGRATE_URL) only for what tappa_app cannot write -- the
// operator's API gate (K-22) -- and for the catalogue and the seal-moving probe. The sender is a fake:
// nothing here reaches the network (internal/egress has the dial-level tests). Fixtures are committed
// and never cleaned (audit_log is append-only); fresh random ids keep runs apart.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/egress"
	"github.com/atknatk/tappa/internal/sun"
	"github.com/atknatk/tappa/internal/webhook"
	"github.com/atknatk/tappa/test/fixtures"
)

var keyShape = regexp.MustCompile(`tt_whsec_[A-Za-z0-9_-]{43}`)

func appDB(t *testing.T) *db.DB {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" || os.Getenv("DATABASE_MIGRATE_URL") == "" {
		t.Skip("DATABASE_URL / DATABASE_MIGRATE_URL not set; skipping (real Postgres required)")
	}
	d, err := db.New(context.Background(), &config.Config{DatabaseURL: dsn})
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	t.Cleanup(d.Close)
	return d
}

func ownerConn(t *testing.T) *pgx.Conn {
	t.Helper()
	c, err := pgx.Connect(context.Background(), os.Getenv("DATABASE_MIGRATE_URL"))
	if err != nil {
		t.Fatalf("owner connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func ownerExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, err := ownerConn(t).Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("owner: %v", err)
	}
}

// business is one tenant with an active owner and a manager, its API open.
type business struct{ tenant, owner, manager uuid.UUID }

func newBusiness(t *testing.T, d *db.DB) business {
	t.Helper()
	b := business{tenant: uuid.New(), owner: uuid.New(), manager: uuid.New()}
	err := d.WithTenant(context.Background(), b.tenant, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure)
		                          VALUES ($1, 'API-9 fixture', $2, 'restaurant', 'multi')`, b.tenant, "VAT-API9-"+b.tenant.String()); e != nil {
			return e
		}
		for _, a := range []struct {
			id   uuid.UUID
			role string
		}{{b.owner, "owner"}, {b.manager, "manager"}} {
			if _, e := tx.Exec(ctx, `INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			                          VALUES ($1, $2, 'API-9 Admin', $3, $4, $5, 'active')`,
				a.id, b.tenant, "api9-"+a.id.String()+"@m11.example", fixtures.UnusablePasswordHash, a.role); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("new business: %v", err)
	}
	setAPI(t, b.tenant, true)
	return b
}

func setAPI(t *testing.T, tenant uuid.UUID, open bool) {
	t.Helper()
	if open {
		ownerExec(t, `UPDATE tenants SET api_code = 'APININE', api_enabled_at = now() WHERE id = $1`, tenant)
		return
	}
	ownerExec(t, `UPDATE tenants SET api_enabled_at = NULL WHERE id = $1`, tenant)
}

// fakeSender records each Post and answers with result (and runs during, if set, while "in flight").
type fakeSender struct {
	mu     sync.Mutex
	posts  []sent
	result egress.Result
	during func()
}

type sent struct {
	to     egress.Endpoint
	body   []byte
	header http.Header
}

func (f *fakeSender) Post(_ context.Context, to egress.Endpoint, body []byte, header http.Header) egress.Result {
	if f.during != nil {
		f.during()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.posts = append(f.posts, sent{to: to, body: append([]byte(nil), body...), header: header.Clone()})
	if f.result == (egress.Result{}) {
		return egress.Result{Status: 204}
	}
	return f.result
}

func (f *fakeSender) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.posts)
}

func randomKEK(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func newService(t *testing.T, d *db.DB, kek []byte, s Sender) *Service {
	t.Helper()
	trail, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(kek, d, trail, s)
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

// trailRows reads the business's trail rows of one action: target and detail.
func trailRows(t *testing.T, d *db.DB, tenant uuid.UUID, action string) [][2]string {
	t.Helper()
	var out [][2]string
	err := d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT coalesce(target, ''), detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2 ORDER BY at`, tenant, action)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a, b string
			if err := rows.Scan(&a, &b); err != nil {
				return err
			}
			out = append(out, [2]string{a, b})
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// allTrail is every trail row of the business, as text.
func allTrail(t *testing.T, d *db.DB, tenant uuid.UUID) string {
	t.Helper()
	var sb strings.Builder
	err := d.WithTenant(context.Background(), tenant, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT action || ' ' || coalesce(target, '') || ' ' || coalesce(detail::text, '') FROM audit_log WHERE tenant_id = $1`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				return err
			}
			sb.WriteString(s + "\n")
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return sb.String()
}

func endpointRows(t *testing.T, tenant uuid.UUID) (n int, url string, sealed []byte, id uuid.UUID, active bool) {
	t.Helper()
	c := ownerConn(t)
	if err := c.QueryRow(context.Background(), `SELECT count(*) OVER (), url, signing_key_sealed, id, active FROM webhook_endpoints WHERE tenant_id = $1`, tenant).
		Scan(&n, &url, &sealed, &id, &active); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	return
}

const hookURL = "https://hooks.api9-canary.example.com/taptime/in"

// TestWebhookEndpoint_OnePerTenant: sixteen concurrent creations for one business -> exactly one
// endpoint and one trail row; every other answer is ErrExists; a later creation is ErrExists too.
// Another business creates its own.
//
// MUTANT (measured, a throwaway PostgreSQL 17 container): webhook_endpoints_one_per_tenant dropped ->
// sixteen creations succeed, red.
func TestWebhookEndpoint_OnePerTenant(t *testing.T) {
	d := appDB(t)
	svc := newService(t, d, randomKEK(t), &fakeSender{})
	b := newBusiness(t, d)
	const n = 16
	var wg sync.WaitGroup
	errs := make([]error, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, errs[i] = svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: hookURL})
		}()
	}
	close(start)
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrExists):
		default:
			t.Errorf("unexpected: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d creations succeeded, want 1", ok)
	}
	if rows, _, _, _, _ := endpointRows(t, b.tenant); rows != 1 {
		t.Fatalf("%d endpoints on file, want 1", rows)
	}
	if got := len(trailRows(t, d, b.tenant, ActionCreated)); got != 1 {
		t.Fatalf("%d created rows, want 1", got)
	}
	if _, err := svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: "https://other.example.com/"}); !errors.Is(err, ErrExists) {
		t.Fatalf("a second endpoint: %v", err)
	}
	other := newBusiness(t, d)
	if _, err := svc.Create(context.Background(), CreateCommand{TenantID: other.tenant, ActorID: other.owner, Address: hookURL}); err != nil {
		t.Fatalf("CONTROL another business: %v", err)
	}
}

// TestWebhookEndpoint_KeyShownOnce is K-12 in the domain: the key is in Created (and Rotated) and in
// no other answer; the row holds it SEALED (no plaintext, no base64 of it), the seal opens under the
// KEK with the endpoint's own id as AAD and NOT with another endpoint's id; the trail's rows name the
// endpoint and no key; rotating gives a new key, and from then on the ping is signed by the new key
// and not by the old one; List carries no key field at all.
//
// MUTANT (measured): the AAD made a constant (the same for every endpoint) -> the seal no longer opens
// with the endpoint's own id (and would open on any row) -> red.
func TestWebhookEndpoint_KeyShownOnce(t *testing.T) {
	d := appDB(t)
	kek := randomKEK(t)
	snd := &fakeSender{}
	svc := newService(t, d, kek, snd)
	b := newBusiness(t, d)
	c, err := svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: hookURL})
	if err != nil {
		t.Fatal(err)
	}
	first := c.Key.RevealToTheOwnerOnce()
	if !keyShape.MatchString(first) || keyShape.FindString(first) != first {
		t.Fatalf("the created key is not a tt_whsec_ key")
	}
	_, url, sealed, id, _ := endpointRows(t, b.tenant)
	if id != c.ID || url != hookURL {
		t.Fatalf("row: id %v url ok %v", id == c.ID, url == hookURL)
	}
	if strings.Contains(string(sealed), first[len(webhook.SigningKeyPrefix):]) || strings.Contains(string(sealed), "tt_whsec_") {
		t.Fatal("the row holds the key in the clear")
	}
	plain, err := sun.Open(kek, id[:], sealed)
	if err != nil || string(plain) != first {
		t.Fatalf("the seal does not open to the key with its own id: %v", err)
	}
	other := uuid.New()
	if _, err := sun.Open(kek, other[:], sealed); err == nil {
		t.Fatal("the seal opens with another endpoint's id as AAD")
	}
	if _, err := sun.Open(randomKEK(t), id[:], sealed); err == nil {
		t.Fatal("the seal opens under another KEK")
	}
	if f, ok := reflect.TypeOf(Endpoint{}).FieldByName("Key"); ok {
		t.Fatalf("the listing's Endpoint has a key field: %v", f)
	}
	for _, f := range reflect.VisibleFields(reflect.TypeOf(Listing{})) {
		if f.Type == reflect.TypeOf(webhook.SigningKey{}) {
			t.Fatal("the listing carries a SigningKey")
		}
	}

	if _, err := svc.Ping(context.Background(), b.tenant, b.owner); err != nil {
		t.Fatal(err)
	}
	r, err := svc.Rotate(context.Background(), b.tenant, b.owner)
	if err != nil {
		t.Fatal(err)
	}
	second := r.Key.RevealToTheOwnerOnce()
	if second == first || !keyShape.MatchString(second) || r.ID != c.ID || r.RotatedAt.IsZero() {
		t.Fatal("rotation did not give a new key for the same endpoint")
	}
	if _, err := svc.Ping(context.Background(), b.tenant, b.owner); err != nil {
		t.Fatal(err)
	}
	if snd.count() != 2 {
		t.Fatalf("%d posts", snd.count())
	}
	sig := func(key string, body []byte) string {
		m := hmac.New(sha256.New, []byte(key))
		m.Write(body)
		return "sha256=" + hex.EncodeToString(m.Sum(nil))
	}
	before, after := snd.posts[0], snd.posts[1]
	if before.header.Get(webhook.SignatureHeader) != sig(first, before.body) {
		t.Fatal("the first ping is not signed by the first key")
	}
	if got := after.header.Get(webhook.SignatureHeader); got != sig(second, after.body) || got == sig(first, after.body) {
		t.Fatal("after rotation the ping is not signed by the new key alone")
	}
	trail := allTrail(t, d, b.tenant)
	for _, k := range []string{first, second, first[len(webhook.SigningKeyPrefix) : len(webhook.SigningKeyPrefix)+16]} {
		if strings.Contains(trail, k) {
			t.Fatal("the trail holds a key")
		}
	}
	if len(trailRows(t, d, b.tenant, ActionRotated)) != 1 {
		t.Fatal("no rotation row")
	}
}

// TestWebhookPing_SharesTheResendBucket is ADR 0028 §6's bucket: ten slots an hour per business,
// shared by the ping and API-10's "resend". Ten pings pass, the eleventh is BucketFull with the next
// slot an hour after the oldest -- and nothing is sent; a business whose hour holds six resend rows has
// four pings left; another business's full bucket does not touch this one; sixteen concurrent pings on
// a fresh business spend exactly ten slots and send exactly ten requests (the lock); and a ping that
// was refused for another reason (switched off) spends nothing.
//
// MUTANTS (measured): the advisory lock removed from LockWebhookBucket (in the generated statement) ->
// the concurrent case spends more than ten (11 in the measured run), red; ActionResend dropped from
// BucketActions -> the shared case sends a fifth ping, red; the count's refusal removed -> the eleventh
// ping is sent, red.
func TestWebhookPing_SharesTheResendBucket(t *testing.T) {
	d := appDB(t)
	snd := &fakeSender{}
	svc := newService(t, d, randomKEK(t), snd)
	create := func(b business) {
		if _, err := svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: hookURL}); err != nil {
			t.Fatal(err)
		}
	}

	a := newBusiness(t, d)
	create(a)
	for i := range BucketLimit {
		if _, err := svc.Ping(context.Background(), a.tenant, a.owner); err != nil {
			t.Fatalf("ping %d: %v", i+1, err)
		}
	}
	_, err := svc.Ping(context.Background(), a.tenant, a.owner)
	var full *BucketFull
	if !errors.As(err, &full) {
		t.Fatalf("the eleventh ping: %v", err)
	}
	// The hour is written as time.Hour, not BucketWindow: the test states ADR 0028 §6's number rather
	// than reading it back from the constant it is checking. The oldest slot is read back from the
	// DATABASE (its `at` is the database's clock, 00053), so the comparison is exact and one clock's:
	// Go's time.Now() against a NextAt dated by the database drifted with the Docker VM's clock (API-9
	// DÜŞÜK D-a). TestWebhookPing_CountsTheLastHourOnly dates its slots the same way.
	var oldest time.Time
	if err := ownerConn(t).QueryRow(context.Background(),
		`SELECT min(at) FROM audit_log WHERE tenant_id = $1 AND action = $2`, a.tenant, ActionPing).Scan(&oldest); err != nil {
		t.Fatal(err)
	}
	if !full.NextAt.Equal(oldest.Add(time.Hour)) {
		t.Fatalf("next slot at %v: want exactly an hour after the oldest slot (%v)", full.NextAt, oldest.Add(time.Hour))
	}
	if snd.count() != BucketLimit || len(trailRows(t, d, a.tenant, ActionPing)) != BucketLimit {
		t.Fatalf("%d sent, %d slots: the refused ping was sent or spent", snd.count(), len(trailRows(t, d, a.tenant, ActionPing)))
	}
	l, err := svc.List(context.Background(), a.tenant, a.owner)
	if err != nil || l.Bucket.Spent != BucketLimit || l.Bucket.NextAt.IsZero() {
		t.Fatalf("the listing's bucket: %+v %v", l.Bucket, err)
	}

	// Shared with "resend": six resend rows (API-10's action) leave four pings.
	s := newBusiness(t, d)
	create(s)
	trail, _ := audit.New(d)
	for range 6 {
		if err := d.WithTenant(context.Background(), s.tenant, func(ctx context.Context, tx pgx.Tx) error {
			_, err := trail.RecordTx(ctx, tx, audit.Event{TenantID: s.tenant, ActorID: &s.owner, Action: ActionResend,
				Target: uuid.NewString(), Detail: map[string]string{"via": "panel"}})
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	sentBefore := snd.count()
	for i := range 4 {
		if _, err := svc.Ping(context.Background(), s.tenant, s.owner); err != nil {
			t.Fatalf("shared: ping %d: %v", i+1, err)
		}
	}
	if _, err := svc.Ping(context.Background(), s.tenant, s.owner); !errors.As(err, &full) {
		t.Fatalf("shared: the fifth ping after six resends: %v", err)
	}
	if snd.count()-sentBefore != 4 {
		t.Fatalf("shared: %d sent, want 4", snd.count()-sentBefore)
	}

	// Per business: a's full bucket does not reach a fresh one.
	c := newBusiness(t, d)
	create(c)
	if _, err := svc.Ping(context.Background(), c.tenant, c.owner); err != nil {
		t.Fatalf("another business's ping: %v", err)
	}

	// Concurrency: sixteen at once, exactly ten.
	cc := newBusiness(t, d)
	create(cc)
	sentBefore = snd.count()
	var wg sync.WaitGroup
	var mu sync.Mutex
	okN, fullN := 0, 0
	start := make(chan struct{})
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := svc.Ping(context.Background(), cc.tenant, cc.owner)
			mu.Lock()
			defer mu.Unlock()
			var f *BucketFull
			switch {
			case err == nil:
				okN++
			case errors.As(err, &f):
				fullN++
			default:
				t.Errorf("concurrent ping: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if okN != BucketLimit || fullN != 16-BucketLimit || snd.count()-sentBefore != BucketLimit ||
		len(trailRows(t, d, cc.tenant, ActionPing)) != BucketLimit {
		t.Fatalf("concurrent: %d ok, %d full, %d sent, %d slots", okN, fullN, snd.count()-sentBefore, len(trailRows(t, d, cc.tenant, ActionPing)))
	}

	// A refused ping spends nothing.
	off := newBusiness(t, d)
	create(off)
	if _, err := svc.Disable(context.Background(), off.tenant, off.owner); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Ping(context.Background(), off.tenant, off.owner); !errors.Is(err, ErrDisabled) {
		t.Fatalf("a switched-off endpoint's ping: %v", err)
	}
	if n := len(trailRows(t, d, off.tenant, ActionPing)); n != 0 {
		t.Fatalf("a refused ping spent %d slots", n)
	}
}

// TestWebhookPing_CountsTheLastHourOnly is the bucket's "an hour" (ADR 0028 §6), measured at its
// edge with slots the database itself dates: ten slots spent 59 minutes ago fill the bucket -- the
// ping is BucketFull, nothing is sent, nothing is spent, and the next slot is EXACTLY an hour after
// the oldest; ten slots spent 61 minutes ago are out of it -- the ping goes out. The rows are written
// by the owner role because `at` is not tappa_app's to write (00053).
//
// MUTANTS (measured, each in its own container): the window in ReadWebhookBucket written as
// interval '1 minute' -> the 59-minute business pings, red; as interval '100 years' -> the 61-minute
// business is BucketFull, red; BucketWindow = time.Minute -> the 59-minute business pings (the SQL
// takes its window from the constant), red.
func TestWebhookPing_CountsTheLastHourOnly(t *testing.T) {
	d := appDB(t)
	snd := &fakeSender{}
	svc := newService(t, d, randomKEK(t), snd)
	// spend writes the bucket's ten slots for b, dated ago before the database's now, and returns the
	// slots' moment as the database recorded it.
	spend := func(b business, ago time.Duration) time.Time {
		t.Helper()
		if _, err := svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: hookURL}); err != nil {
			t.Fatal(err)
		}
		var at time.Time
		err := ownerConn(t).QueryRow(context.Background(), `
			WITH spent AS (
				INSERT INTO audit_log (tenant_id, actor_id, action, target, detail, at)
				SELECT $1, $2, $3, gen_random_uuid()::text, '{"via":"panel"}', now() - make_interval(secs => $4::float8)
				FROM generate_series(1, $5::int)
				RETURNING at)
			SELECT min(at) FROM spent`,
			b.tenant, b.owner, ActionPing, ago.Seconds(), BucketLimit).Scan(&at)
		if err != nil {
			t.Fatalf("spend the bucket: %v", err)
		}
		return at
	}

	inside := newBusiness(t, d)
	oldest := spend(inside, 59*time.Minute)
	_, err := svc.Ping(context.Background(), inside.tenant, inside.owner)
	var full *BucketFull
	if !errors.As(err, &full) {
		t.Fatalf("ten slots 59 minutes old: the ping was not refused (%v)", err)
	}
	if !full.NextAt.Equal(oldest.Add(time.Hour)) {
		t.Fatalf("ten slots 59 minutes old: next slot at %v, want an hour after the oldest (%v)", full.NextAt, oldest.Add(time.Hour))
	}
	if snd.count() != 0 || len(trailRows(t, d, inside.tenant, ActionPing)) != BucketLimit {
		t.Fatalf("ten slots 59 minutes old: %d sent, %d slots", snd.count(), len(trailRows(t, d, inside.tenant, ActionPing)))
	}
	if l, err := svc.List(context.Background(), inside.tenant, inside.owner); err != nil ||
		l.Bucket.Spent != BucketLimit || !l.Bucket.NextAt.Equal(oldest.Add(time.Hour)) {
		t.Fatalf("ten slots 59 minutes old: the listing's bucket %+v (%v)", l.Bucket, err)
	}

	outside := newBusiness(t, d)
	spend(outside, 61*time.Minute)
	if _, err := svc.Ping(context.Background(), outside.tenant, outside.owner); err != nil {
		t.Fatalf("ten slots 61 minutes old: the ping was refused (%v)", err)
	}
	if snd.count() != 1 {
		t.Fatalf("ten slots 61 minutes old: %d sent, want 1", snd.count())
	}
	if l, err := svc.List(context.Background(), outside.tenant, outside.owner); err != nil ||
		l.Bucket.Spent != 1 || !l.Bucket.NextAt.IsZero() {
		t.Fatalf("ten slots 61 minutes old: the listing's bucket %+v (%v)", l.Bucket, err)
	}
}

// TestWebhookEndpoint_ClosedApiRefused is K-22 on every path but one: with the operator's gate closed,
// creating, rotating, re-addressing, switching on and pinging are ErrAPIClosed and write nothing (no
// row, no change, no trail row, no request); switching OFF still works (K-26) and leaves its row.
func TestWebhookEndpoint_ClosedApiRefused(t *testing.T) {
	d := appDB(t)
	kek := randomKEK(t)
	snd := &fakeSender{}
	svc := newService(t, d, kek, snd)

	fresh := newBusiness(t, d)
	setAPI(t, fresh.tenant, false)
	if _, err := svc.Create(context.Background(), CreateCommand{TenantID: fresh.tenant, ActorID: fresh.owner, Address: hookURL}); !errors.Is(err, ErrAPIClosed) {
		t.Fatalf("create on a closed API: %v", err)
	}
	if n, _, _, _, _ := endpointRows(t, fresh.tenant); n != 0 {
		t.Fatal("an endpoint was created on a closed API")
	}

	b := newBusiness(t, d)
	if _, err := svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: hookURL}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Disable(context.Background(), b.tenant, b.owner); err != nil {
		t.Fatal(err)
	}
	_, url0, sealed0, _, _ := endpointRows(t, b.tenant)
	setAPI(t, b.tenant, false)
	trail0 := allTrail(t, d, b.tenant)
	if _, err := svc.Rotate(context.Background(), b.tenant, b.owner); !errors.Is(err, ErrAPIClosed) {
		t.Errorf("rotate: %v", err)
	}
	if _, err := svc.Readdress(context.Background(), b.tenant, b.owner, "https://moved.example.com/"); !errors.Is(err, ErrAPIClosed) {
		t.Errorf("readdress: %v", err)
	}
	if _, err := svc.Enable(context.Background(), b.tenant, b.owner); !errors.Is(err, ErrAPIClosed) {
		t.Errorf("enable: %v", err)
	}
	if _, err := svc.Ping(context.Background(), b.tenant, b.owner); !errors.Is(err, ErrAPIClosed) {
		t.Errorf("ping: %v", err)
	}
	_, url1, sealed1, _, active := endpointRows(t, b.tenant)
	if url1 != url0 || string(sealed1) != string(sealed0) || active || snd.count() != 0 || allTrail(t, d, b.tenant) != trail0 {
		t.Fatal("a refused act on a closed API changed something")
	}
	// Switching off stays open: switch on with the gate open, close it, switch off.
	setAPI(t, b.tenant, true)
	if _, err := svc.Enable(context.Background(), b.tenant, b.owner); err != nil {
		t.Fatal(err)
	}
	setAPI(t, b.tenant, false)
	if _, err := svc.Disable(context.Background(), b.tenant, b.owner); err != nil {
		t.Fatalf("disable on a closed API (K-26): %v", err)
	}
	if _, _, _, _, active := endpointRows(t, b.tenant); active {
		t.Fatal("the endpoint is still on")
	}
	if got := len(trailRows(t, d, b.tenant, ActionDisabled)); got != 2 {
		t.Fatalf("%d disabled rows, want 2", got)
	}
	if _, err := svc.Disable(context.Background(), b.tenant, b.owner); !errors.Is(err, ErrUnchanged) {
		t.Fatalf("switching off twice: %v", err)
	}
}

// TestWebhookEndpoint_OnlyAnActiveOwner: a manager of the business, and an owner of ANOTHER business,
// get ErrNotPermitted at every act; nothing is written.
func TestWebhookEndpoint_OnlyAnActiveOwner(t *testing.T) {
	d := appDB(t)
	snd := &fakeSender{}
	svc := newService(t, d, randomKEK(t), snd)
	b := newBusiness(t, d)
	if _, err := svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: hookURL}); err != nil {
		t.Fatal(err)
	}
	stranger := newBusiness(t, d)
	trail0 := allTrail(t, d, b.tenant)
	for _, actor := range []uuid.UUID{b.manager, stranger.owner} {
		errs := []error{}
		_, err := svc.List(context.Background(), b.tenant, actor)
		errs = append(errs, err)
		_, err = svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: actor, Address: hookURL})
		errs = append(errs, err)
		_, err = svc.Rotate(context.Background(), b.tenant, actor)
		errs = append(errs, err)
		_, err = svc.Readdress(context.Background(), b.tenant, actor, "https://moved.example.com/")
		errs = append(errs, err)
		_, err = svc.Disable(context.Background(), b.tenant, actor)
		errs = append(errs, err)
		_, err = svc.Enable(context.Background(), b.tenant, actor)
		errs = append(errs, err)
		_, err = svc.Ping(context.Background(), b.tenant, actor)
		errs = append(errs, err)
		for i, err := range errs {
			if !errors.Is(err, ErrNotPermitted) {
				t.Errorf("actor %v act %d: %v", actor == b.manager, i, err)
			}
		}
	}
	if snd.count() != 0 || allTrail(t, d, b.tenant) != trail0 {
		t.Fatal("a refused actor changed something")
	}
}

// TestWebhookEndpoint_TheAddressRuleAndTheReaddress: a refused address is the egress rule's sentinel
// and writes nothing; a new address replaces the old one, keeps the key, and leaves a row naming the
// endpoint and not the address.
func TestWebhookEndpoint_TheAddressRuleAndTheReaddress(t *testing.T) {
	d := appDB(t)
	snd := &fakeSender{}
	svc := newService(t, d, randomKEK(t), snd)
	b := newBusiness(t, d)
	for _, bad := range []string{"http://hooks.example.com/", "https://10.0.0.1/", "https://hooks.example.com:8443/",
		"https://u:p@hooks.example.com/",
		"https://hooks.example.com/?t=1", "https://hooks.example.com/#x"} {
		if _, err := svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if n, _, _, _, _ := endpointRows(t, b.tenant); n != 0 {
		t.Fatal("a refused address created an endpoint")
	}
	if _, err := svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: hookURL}); err != nil {
		t.Fatal(err)
	}
	_, _, sealed0, _, _ := endpointRows(t, b.tenant)
	if _, err := svc.Readdress(context.Background(), b.tenant, b.owner, "https://user@moved.example.com/"); !errors.Is(err, egress.ErrUserinfo) {
		t.Fatalf("a refused readdress: %v", err)
	}
	if _, err := svc.Readdress(context.Background(), b.tenant, b.owner, "https://Moved.api9-canary.example.com/new"); err != nil {
		t.Fatal(err)
	}
	_, url, sealed, _, _ := endpointRows(t, b.tenant)
	if url != "https://moved.api9-canary.example.com/new" || string(sealed) != string(sealed0) {
		t.Fatalf("readdress: url %q, key kept %v", url, string(sealed) == string(sealed0))
	}
	if strings.Contains(allTrail(t, d, b.tenant), "api9-canary") {
		t.Fatal("the trail holds an address")
	}
	if len(trailRows(t, d, b.tenant, ActionReaddressed)) != 1 {
		t.Fatal("no readdress row")
	}
	if _, err := svc.Ping(context.Background(), b.tenant, b.owner); err != nil {
		t.Fatal(err)
	}
	if got := snd.posts[0].to.ShowToTheOwner(); got != "https://moved.api9-canary.example.com/new" {
		t.Fatalf("the ping went to %q", got)
	}
}

// TestWebhookPing_TheBodyIsSyntheticAndSigned: the ping's body is {event, id, sentAt} and nothing
// else (no punch, no employee, no venue), Content-Type JSON, the signature header over the exact
// bytes sent; the result row carries the class and the status.
func TestWebhookPing_TheBodyIsSyntheticAndSigned(t *testing.T) {
	d := appDB(t)
	snd := &fakeSender{result: egress.Result{Class: egress.ClassHTTP5xx, Status: 503}}
	svc := newService(t, d, randomKEK(t), snd)
	b := newBusiness(t, d)
	c, err := svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: hookURL})
	if err != nil {
		t.Fatal(err)
	}
	p, err := svc.Ping(context.Background(), b.tenant, b.owner)
	if err != nil || p.Result != (egress.Result{Class: egress.ClassHTTP5xx, Status: 503}) || p.ID != c.ID {
		t.Fatalf("ping: %+v %v", p, err)
	}
	got := snd.posts[0]
	var m map[string]any
	if err := json.Unmarshal(got.body, &m); err != nil {
		t.Fatal(err)
	}
	if len(m) != 3 || m["event"] != "ping" || m["id"] == "" || m["sentAt"] == "" {
		t.Fatalf("body: %s", got.body)
	}
	if got.header.Get("Content-Type") != "application/json" || got.header.Get(webhook.SignatureHeader) != c.Key.Sign(got.body) {
		t.Fatal("headers")
	}
	rows := trailRows(t, d, b.tenant, ActionPingResult)
	if len(rows) != 1 || rows[0][0] != c.ID.String() || !strings.Contains(rows[0][1], `"class": "http_5xx"`) || !strings.Contains(rows[0][1], `"status": 503`) {
		t.Fatalf("result row: %v", rows)
	}
}

// TestWebhookPing_NoTransactionOpenDuringTheRequest: while the request is in flight, the bucket's
// lock is free and no transaction of the ping is open -- a second connection takes the same advisory
// lock with a 300 ms lock timeout from inside the sender.
func TestWebhookPing_NoTransactionOpenDuringTheRequest(t *testing.T) {
	d := appDB(t)
	snd := &fakeSender{}
	svc := newService(t, d, randomKEK(t), snd)
	b := newBusiness(t, d)
	if _, err := svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: hookURL}); err != nil {
		t.Fatal(err)
	}
	var probe error
	snd.during = func() {
		probe = d.WithTenant(context.Background(), b.tenant, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout = '300ms'`); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('webhook-bucket:' || $1::text, 0))`, b.tenant)
			return err
		})
	}
	if _, err := svc.Ping(context.Background(), b.tenant, b.owner); err != nil {
		t.Fatal(err)
	}
	if probe != nil {
		t.Fatalf("the bucket's lock was held while the request was in flight: %v", probe)
	}
}

// TestWebhookPing_ASealMovedToAnotherEndpointDoesNotOpen: the owner role copies business A's sealed
// key onto business B's endpoint row; B's ping is ErrKeyUnreadable -- nothing is signed or sent, no
// slot is spent. The AAD (the endpoint's id) is what refuses it; under a KEK that changed, the same.
func TestWebhookPing_ASealMovedToAnotherEndpointDoesNotOpen(t *testing.T) {
	d := appDB(t)
	kek := randomKEK(t)
	snd := &fakeSender{}
	svc := newService(t, d, kek, snd)
	a, b := newBusiness(t, d), newBusiness(t, d)
	for _, x := range []business{a, b} {
		if _, err := svc.Create(context.Background(), CreateCommand{TenantID: x.tenant, ActorID: x.owner, Address: hookURL}); err != nil {
			t.Fatal(err)
		}
	}
	ownerExec(t, `UPDATE webhook_endpoints SET signing_key_sealed = (SELECT signing_key_sealed FROM webhook_endpoints WHERE tenant_id = $1) WHERE tenant_id = $2`, a.tenant, b.tenant)
	if _, err := svc.Ping(context.Background(), b.tenant, b.owner); !errors.Is(err, ErrKeyUnreadable) {
		t.Fatalf("a moved seal: %v", err)
	}
	other := newService(t, d, randomKEK(t), snd)
	if _, err := other.Ping(context.Background(), a.tenant, a.owner); !errors.Is(err, ErrKeyUnreadable) {
		t.Fatalf("another KEK: %v", err)
	}
	if snd.count() != 0 || len(trailRows(t, d, b.tenant, ActionPing)) != 0 || len(trailRows(t, d, a.tenant, ActionPing)) != 0 {
		t.Fatal("an unreadable key sent or spent")
	}
	if _, err := svc.Ping(context.Background(), a.tenant, a.owner); err != nil {
		t.Fatalf("CONTROL a's own seal: %v", err)
	}
}

// TestWebhookPing_NothingOfTheAddressOrTheKeyReachesATrail: for each failure class the sender can
// report, the business's trail holds the endpoint's id, the class and the status -- and not the
// address, its host, any part of the key, or Go's error words.
func TestWebhookPing_NothingOfTheAddressOrTheKeyReachesATrail(t *testing.T) {
	d := appDB(t)
	for _, cls := range egress.Classes {
		snd := &fakeSender{result: egress.Result{Class: cls}}
		svc := newService(t, d, randomKEK(t), snd)
		b := newBusiness(t, d)
		c, err := svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: hookURL})
		if err != nil {
			t.Fatal(err)
		}
		p, err := svc.Ping(context.Background(), b.tenant, b.owner)
		if err != nil || p.Result.Class != cls {
			t.Fatalf("%s: %+v %v", cls, p, err)
		}
		trail := allTrail(t, d, b.tenant)
		if !strings.Contains(trail, string(cls)) || !strings.Contains(trail, c.ID.String()) {
			t.Fatalf("%s: CONTROL the trail does not name the class and the endpoint: %s", cls, trail)
		}
		key := c.Key.RevealToTheOwnerOnce()
		for _, leak := range []string{"api9-canary", "example.com", "taptime/in", "https://", key[len(webhook.SigningKeyPrefix) : len(webhook.SigningKeyPrefix)+10],
			"tt_whsec_", "dial tcp", "lookup", "x509"} {
			if strings.Contains(trail, leak) {
				t.Fatalf("%s: the trail holds %q", cls, leak)
			}
		}
	}
}

// TestRLS_WebhookEndpoints_AnotherTenantsEndpointIsInvisible: in A's context a query WITHOUT a tenant
// filter sees A's endpoint and not B's -- the 0 comes from RLS, not from a WHERE (CLAUDE.md §6); an
// UPDATE aimed at B's row by id changes nothing; an INSERT naming B is refused by the policy's check.
func TestRLS_WebhookEndpoints_AnotherTenantsEndpointIsInvisible(t *testing.T) {
	d := appDB(t)
	svc := newService(t, d, randomKEK(t), &fakeSender{})
	a, b := newBusiness(t, d), newBusiness(t, d)
	ca, err := svc.Create(context.Background(), CreateCommand{TenantID: a.tenant, ActorID: a.owner, Address: hookURL})
	if err != nil {
		t.Fatal(err)
	}
	cb, err := svc.Create(context.Background(), CreateCommand{TenantID: b.tenant, ActorID: b.owner, Address: hookURL})
	if err != nil {
		t.Fatal(err)
	}
	err = d.WithTenant(context.Background(), a.tenant, func(ctx context.Context, tx pgx.Tx) error {
		var mine, theirs int
		if err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE id = $1), count(*) FILTER (WHERE id = $2) FROM webhook_endpoints`, ca.ID, cb.ID).Scan(&mine, &theirs); err != nil {
			return err
		}
		if mine != 1 || theirs != 0 {
			t.Errorf("A sees its own %d, B's %d", mine, theirs)
		}
		tag, err := tx.Exec(ctx, `UPDATE webhook_endpoints SET active = false WHERE id = $1`, cb.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 0 {
			t.Errorf("A updated B's endpoint")
		}
		_, err = tx.Exec(ctx, `SAVEPOINT s`)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO webhook_endpoints (tenant_id, id, url, signing_key_sealed, active) VALUES ($1, gen_random_uuid(), 'https://x.example.com/', '\x01', true)`, b.tenant); err == nil {
			t.Errorf("A inserted an endpoint for B")
		}
		_, err = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT s`)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, active := endpointRows(t, b.tenant); !active {
		t.Fatal("B's endpoint changed")
	}
}

// TestWebhookEndpointCatalog pins the table as API-9 relies on it (migration 00048): the columns, one
// endpoint per business, the id+tenant key the deliveries reference, the https/length check, the RLS
// five (ENABLE, FORCE, the policy on app.tenant_id with NULLIF, an index led by tenant_id), and
// tappa_app's reach -- SELECT on every column, INSERT on the five it names, UPDATE on url, the seal,
// active and rotated_at only (never tenant_id, id or created_at), no DELETE, no TRUNCATE.
// TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd (internal/db) holds the same matrix under
// production's arwd default.
func TestWebhookEndpointCatalog(t *testing.T) {
	appDB(t)
	c := ownerConn(t)
	ctx := context.Background()
	var cols string
	if err := c.QueryRow(ctx, `SELECT string_agg(attname, ',' ORDER BY attnum) FROM pg_attribute
	                           WHERE attrelid = 'public.webhook_endpoints'::regclass AND attnum > 0 AND NOT attisdropped`).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	if cols != "tenant_id,id,url,signing_key_sealed,active,created_at,rotated_at" {
		t.Fatalf("columns: %s", cols)
	}
	var rls, force bool
	if err := c.QueryRow(ctx, `SELECT relrowsecurity, relforcerowsecurity FROM pg_class WHERE oid = 'public.webhook_endpoints'::regclass`).Scan(&rls, &force); err != nil || !rls || !force {
		t.Fatalf("RLS %v FORCE %v %v", rls, force, err)
	}
	var qual, check string
	if err := c.QueryRow(ctx, `SELECT qual, with_check FROM pg_policies WHERE tablename = 'webhook_endpoints'`).Scan(&qual, &check); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{qual, check} {
		if !strings.Contains(p, "tenant_id") || !strings.Contains(p, "NULLIF(current_setting('app.tenant_id'::text, true), ''::text)") {
			t.Fatalf("policy: %s", p)
		}
	}
	cons := map[string]string{}
	rows, err := c.Query(ctx, `SELECT conname, pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid = 'public.webhook_endpoints'::regclass`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n, def string
		if err := rows.Scan(&n, &def); err != nil {
			t.Fatal(err)
		}
		cons[n] = def
	}
	rows.Close()
	for name, want := range map[string]string{
		"webhook_endpoints_one_per_tenant": "UNIQUE (tenant_id)",
		"webhook_endpoints_id_tenant_key":  "UNIQUE (id, tenant_id)",
		"webhook_endpoints_pkey":           "PRIMARY KEY (id)",
	} {
		if cons[name] != want {
			t.Errorf("%s: %q, want %q", name, cons[name], want)
		}
	}
	if !strings.Contains(cons["webhook_endpoints_url_shape"], "https://") || !strings.Contains(cons["webhook_endpoints_url_shape"], "2048") {
		t.Errorf("url check: %q", cons["webhook_endpoints_url_shape"])
	}
	var led bool
	if err := c.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_index i JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = i.indkey[0]
	                           WHERE i.indrelid = 'public.webhook_endpoints'::regclass AND a.attname = 'tenant_id')`).Scan(&led); err != nil || !led {
		t.Fatalf("no index led by tenant_id: %v", err)
	}
	for _, col := range strings.Split(cols, ",") {
		for verb, want := range map[string]bool{
			"SELECT": true,
			"INSERT": col == "tenant_id" || col == "id" || col == "url" || col == "signing_key_sealed" || col == "active",
			"UPDATE": col == "url" || col == "signing_key_sealed" || col == "active" || col == "rotated_at",
		} {
			var may bool
			if err := c.QueryRow(ctx, `SELECT has_column_privilege('tappa_app', 'public.webhook_endpoints', $1, $2)`, col, verb).Scan(&may); err != nil {
				t.Fatal(err)
			}
			if may != want {
				t.Errorf("tappa_app %s on %s: %v, want %v", verb, col, may, want)
			}
		}
	}
	for _, verb := range []string{"DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"} {
		var may bool
		if err := c.QueryRow(ctx, `SELECT has_table_privilege('tappa_app', 'public.webhook_endpoints', $1)`, verb).Scan(&may); err != nil {
			t.Fatal(err)
		}
		if may {
			t.Errorf("tappa_app holds %s", verb)
		}
	}
}
