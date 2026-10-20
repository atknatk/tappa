package api

// suspension_db_test.go -- M11 API-4b: the suspension checkpoint against OP-15's column (00063's
// tenants.suspended_at), read by the shipped tenant.WriteSuspension, with businesses the OPERATOR
// suspended -- op_suspend_tenant and op_reinstate_tenant (db.SuspendTenant, db.ReinstateTenant) as
// tappa_operator, committed. ADR 0028 §10 and plan §4.7, cell by cell.
//
// HOW A TEST SUSPENDS (operatorFixture): one operator account per rig (random id, @example.test, an
// unusable synthetic digest and envelope -- it can never sign in) with one MFA-verified session row,
// written by the owner and COMMITTED, as internal/operatorauth's committing tests write theirs; then
// each suspension and each lift is ONE committed call as tappa_operator (SET LOCAL SESSION
// AUTHORIZATION on the owner connection). Every call leaves its two append-only rows (the tenant's
// audit_log, the operator's log) -- the price of measuring the column the operator really writes.
// The calls take internal/db's operator-tables test lock SHARED (as internal/operatorauth does),
// so they wait out internal/db's DDL on those tables instead of racing it. Every business a test
// suspends is reinstated in t.Cleanup: a suspension left committed makes 00063's Down refuse (K15-6)
// -- and no real business is ever suspended (the fixtures are this test's own).

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
)

// operatorTablesTestLock is the cross-package advisory lock internal/db's opTx takes EXCLUSIVE and
// internal/operatorauth's database tests take SHARED (its TestHarness_TheTablesLockIsTheOneInternalDBTakes
// pins the spelling against internal/db; TestSuspension_TheOperatorFixtureTakesTheTablesLock pins
// this copy against that one).
const operatorTablesTestLock = "tappa/test/operator-tables"

// operatorFixture is one rig's operator: its session's token hash (the value op_touch_session
// compares; a fixture, there is no token behind it) and the businesses it suspended.
type operatorFixture struct {
	session   string
	suspended map[uuid.UUID]bool
}

// asOperator runs fn as tappa_operator in ONE committed transaction on the owner connection, the
// operator-tables lock held shared. lockTimeout, when not empty, bounds every lock wait in it.
func (r *dbRig) asOperator(lockTimeout string, fn func(ctx context.Context, tx pgx.Tx) error) error {
	ctx := context.Background()
	tx, err := r.owner.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(hashtext($1))`, operatorTablesTestLock); err != nil {
		return err
	}
	if lockTimeout != "" {
		if _, err := tx.Exec(ctx, `SELECT set_config('lock_timeout', $1, true)`, lockTimeout); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `SET LOCAL SESSION AUTHORIZATION tappa_operator`); err != nil {
		return err
	}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// operatorSession writes the rig's operator account and session (once) and returns the session's hash.
func (r *dbRig) operatorSession() string {
	r.t.Helper()
	if r.op.session != "" {
		return r.op.session
	}
	ctx := context.Background()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		r.t.Fatal(err)
	}
	hash, admin := hex.EncodeToString(raw), uuid.New()
	tx, err := r.owner.Begin(ctx)
	if err != nil {
		r.t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, q := range []struct {
		sql  string
		args []any
	}{
		{`SELECT pg_advisory_xact_lock_shared(hashtext($1))`, []any{operatorTablesTestLock}},
		{`INSERT INTO platform_admins (id, email, display_name, status, password_hash, totp_secret_sealed)
		  VALUES ($1, $2, 'API-4b fixture operator', 'active', $3, $4)`,
			[]any{admin, "api4b-" + admin.String()[:12] + "@example.test", "$2a$12$" + strings.Repeat("u", 53), bytes.Repeat([]byte{0x5A}, 48)}},
		{`INSERT INTO platform_sessions (admin_id, token_hash, mfa_verified_at) VALUES ($1, $2, clock_timestamp())`, []any{admin, hash}},
	} {
		if _, err := tx.Exec(ctx, q.sql, q.args...); err != nil {
			r.t.Fatalf("the operator fixture: %v", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		r.t.Fatal(err)
	}
	r.op.session = hash
	return hash
}

// suspend has the operator suspend biz (committed) and lifts it when the test ends.
func (r *dbRig) suspend(biz uuid.UUID) {
	r.t.Helper()
	if err := r.trySuspend(biz, ""); err != nil {
		r.t.Fatalf("op_suspend_tenant: %v", err)
	}
}

// trySuspend is suspend with the database's answer returned (and a lock timeout, when given).
func (r *dbRig) trySuspend(biz uuid.UUID, lockTimeout string) error {
	r.t.Helper()
	hash := r.operatorSession()
	r.liftAtEnd(biz)
	return r.asOperator(lockTimeout, func(ctx context.Context, tx pgx.Tx) error {
		return db.SuspendTenant(ctx, tx, hash, biz, db.SuspensionSecurityHold)
	})
}

// liftAtEnd registers, once per business, the t.Cleanup that has the operator lift biz's suspension.
// A test that also holds a write open calls it BEFORE heldWrite: cleanups run last-registered first,
// so the held write is released before the lift -- which, under a locking read (the FOR SHARE
// mutant), would otherwise wait on it until the write's transaction was killed (30 s).
func (r *dbRig) liftAtEnd(biz uuid.UUID) {
	r.t.Helper()
	hash := r.operatorSession()
	if r.op.suspended == nil {
		r.op.suspended = map[uuid.UUID]bool{}
	}
	if r.op.suspended[biz] {
		return
	}
	r.op.suspended[biz] = true
	r.t.Cleanup(func() {
		if err := r.asOperator("", func(ctx context.Context, tx pgx.Tx) error { return db.ReinstateTenant(ctx, tx, hash, biz) }); err != nil {
			r.t.Errorf("lift the fixture's suspension (00063's Down refuses while one stands): %v", err)
		}
	})
}

// reinstate has the operator lift biz's suspension (committed).
func (r *dbRig) reinstate(biz uuid.UUID) {
	r.t.Helper()
	hash := r.operatorSession()
	if err := r.asOperator("", func(ctx context.Context, tx pgx.Tx) error { return db.ReinstateTenant(ctx, tx, hash, biz) }); err != nil {
		r.t.Fatalf("op_reinstate_tenant: %v", err)
	}
}

// isSuspended reads the column as the owner -- the fixture's own check that the operator's call landed.
func (r *dbRig) isSuspended(biz uuid.UUID) bool {
	r.t.Helper()
	var s bool
	if err := r.owner.QueryRow(context.Background(), `SELECT suspended_at IS NOT NULL FROM tenants WHERE id = $1`, biz).Scan(&s); err != nil {
		r.t.Fatal(err)
	}
	return s
}

// trace is every BUSINESS row a /v1 write of biz could leave, read as the owner: the venues and the
// employees row for row (any column), and the counts of the trail, the LIVE Idempotency-Key ledger
// (rows of the last 24 hours), the feed, the invitation queue and the invitations. Two equal traces:
// no business row was written. The two maintenance writes every authenticated request may make are
// outside it on purpose and measured on their own (TestSuspension_TheSection47Table): the runner's
// lazy delete of EXPIRED ledger rows, committed before the checkpoint (apikey.Writes.Run), and the
// chain's api_keys.last_used_at (at most once a minute).
func (r *dbRig) trace(biz uuid.UUID) string {
	r.t.Helper()
	var f string
	err := r.owner.QueryRow(context.Background(), `
		SELECT concat_ws('|',
		  (SELECT md5(coalesce(string_agg(l::text, ',' ORDER BY l.id), '')) FROM locations l WHERE l.tenant_id = $1),
		  (SELECT md5(coalesce(string_agg(e::text, ',' ORDER BY e.id), '')) FROM employees e WHERE e.tenant_id = $1),
		  (SELECT count(*) FROM audit_log WHERE tenant_id = $1),
		  (SELECT count(*) FROM api_idempotency WHERE tenant_id = $1 AND created_at >= now() - interval '24 hours'),
		  (SELECT count(*) FROM punch_events WHERE tenant_id = $1),
		  (SELECT count(*) FROM invite_requests WHERE tenant_id = $1),
		  (SELECT count(*) FROM employee_invites WHERE tenant_id = $1))`, biz).Scan(&f)
	if err != nil {
		r.t.Fatal(err)
	}
	return f
}

// expiredLedger plants one Idempotency-Key row of biz's key dated 25 hours ago (the lazy delete's
// prey), as the owner, and returns how many expired rows biz now has.
func (r *dbRig) expiredLedger(biz, key uuid.UUID) int {
	r.t.Helper()
	ctx := context.Background()
	if _, err := r.owner.Exec(ctx, `INSERT INTO api_idempotency (tenant_id, key_id, idem_key, request_hash, status, created_at)
	                                VALUES ($1, $2, $3, $4, 201, now() - interval '25 hours')`,
		biz, key, "expired-"+uuid.NewString(), strings.Repeat("ab", 32)); err != nil {
		r.t.Fatal(err)
	}
	return r.expiredRows(biz)
}

func (r *dbRig) expiredRows(biz uuid.UUID) int {
	r.t.Helper()
	return r.ownerCount(`SELECT count(*) FROM api_idempotency WHERE tenant_id = $1 AND created_at < now() - interval '24 hours'`, biz)
}

// wantSuspendedProblem: the answer is §8's JSON error, 403 tenant_suspended, with its constant message
// and empty details.
func wantSuspendedProblem(t *testing.T, w *httptest.ResponseRecorder, what string) {
	t.Helper()
	if w.Code != http.StatusForbidden || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("%s: %d %q %s, want 403 application/json", what, w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	p := problemOf(t, w)
	if p.Code != CodeTenantSuspended || p.Message != problemTenantSuspended.Message || len(p.Details) != 0 {
		t.Fatalf("%s: %+v, want tenant_suspended with its message and empty details", what, p)
	}
}

// s47Fixture is one business's side of the table: refs that exist before the suspension.
type s47Fixture struct {
	b                                    punchBiz
	venue, rename, off, gone, resend, me string
	email                                string
}

func (w *punchWorld) s47(b punchBiz) s47Fixture {
	t := w.r.t
	t.Helper()
	f := s47Fixture{b: b, venue: b.code + "-LOC", me: b.code + "-EMP", email: uniqueEmail("s47")}
	resendAddr := uniqueEmail("s47resend")
	for _, e := range []struct {
		ref   *string
		email *string // nil: no address (active:false and DELETE send none)
	}{{&f.rename, &f.email}, {&f.off, nil}, {&f.gone, nil}, {&f.resend, &resendAddr}} {
		*e.ref = newRef(b.code + "S47")
		if rec := w.r.putEmp(b.raw, *e.ref, empBody("Table", "Cell", e.email, true)); rec.Code != http.StatusCreated {
			t.Fatalf("fixture employee %s: %d %s", *e.ref, rec.Code, rec.Body.String())
		}
	}
	return f
}

// s47Cell is one §4.7 cell for /v1: the request, and what it is answered when the business is NOT
// suspended. open: answered the same while suspended.
type s47Cell struct {
	name, method, route string // route: the router's pattern this cell exercises
	path, body          func(f s47Fixture) string
	ok                  int
	open                bool
}

func s47Cells() []s47Cell {
	none := func(s47Fixture) string { return "" }
	return []s47Cell{
		// Open: every GET (the router's, below), a NEW employee, resend-activation.
		{"GET employees", http.MethodGet, "/v1/employees", func(s47Fixture) string { return "/v1/employees" }, none, 200, true},
		{"GET one employee", http.MethodGet, "/v1/employees/{ref}", func(f s47Fixture) string { return "/v1/employees/" + f.me }, none, 200, true},
		{"GET punches", http.MethodGet, "/v1/punches", func(s47Fixture) string { return "/v1/punches?scope=employer" }, none, 200, true},
		{"GET directory", http.MethodGet, "/v1/directory", func(s47Fixture) string { return "/v1/directory" }, none, 200, true},
		{"GET openapi.yaml", http.MethodGet, "/v1/openapi.yaml", func(s47Fixture) string { return "/v1/openapi.yaml" }, none, 200, true},
		{"GET guide.md", http.MethodGet, "/v1/guide.md", func(s47Fixture) string { return "/v1/guide.md" }, none, 200, true},
		{"GET guide.tr.md", http.MethodGet, "/v1/guide.tr.md", func(s47Fixture) string { return "/v1/guide.tr.md" }, none, 200, true},
		{"PUT a new employee", http.MethodPut, "/v1/employees/{ref}",
			func(f s47Fixture) string { return "/v1/employees/" + newRef(f.b.code+"NEW") },
			func(s47Fixture) string { a := uniqueEmail("s47new"); return empBody("New", "Hire", &a, true) }, 201, true},
		{"POST resend-activation", http.MethodPost, "/v1/employees/{ref}/resend-activation",
			func(f s47Fixture) string { return "/v1/employees/" + f.resend + "/resend-activation" }, none, 202, true},
		// 403 tenant_suspended.
		{"PUT a new location", http.MethodPut, "/v1/locations/{ref}",
			func(f s47Fixture) string { return "/v1/locations/" + newRef(f.b.code+"LOC") },
			func(s47Fixture) string { return `{"name":"Marsaxlokk","active":true}` }, 201, false},
		{"PUT a location that exists", http.MethodPut, "/v1/locations/{ref}",
			func(f s47Fixture) string { return "/v1/locations/" + f.venue },
			func(s47Fixture) string { return `{"name":"Renamed ` + uuid.NewString()[:6] + `","active":true}` }, 200, false},
		{"PUT changing an employee", http.MethodPut, "/v1/employees/{ref}",
			func(f s47Fixture) string { return "/v1/employees/" + f.rename },
			func(f s47Fixture) string { return empBody("Table", "Renamed"+uuid.NewString()[:4], &f.email, true) }, 200, false},
		{"PUT active:false", http.MethodPut, "/v1/employees/{ref}",
			func(f s47Fixture) string { return "/v1/employees/" + f.off },
			func(s47Fixture) string { return empBody("Table", "Cell", nil, false) }, 200, false},
		{"DELETE an employee", http.MethodDelete, "/v1/employees/{ref}",
			func(f s47Fixture) string { return "/v1/employees/" + f.gone }, none, 200, false},
		{"an unclassified write", http.MethodPut, "",
			func(s47Fixture) string { return "/v1/_unclassified/" + uuid.NewString() }, func(s47Fixture) string { return `{}` }, 201, false},
	}
}

// TestSuspension_TheSection47Table is plan §4.7 / ADR 0028 §10 for /v1, cell by cell, each a subtest
// run against a business the operator suspended (KF) and its unsuspended twin (KM -- a holding peer,
// so the twin is also the belt: one business's suspension is not its peer's):
//
//	open      every GET the router registers (derived: chi.Walk over the shipped routes -- a GET added
//	          later fails this test until it is a cell), a NEW employee (201), resend-activation (202):
//	          the same answer suspended or not
//	closed    PUT /v1/locations (new and existing), PUT changing an employee, PUT active:false,
//	          DELETE /v1/employees, and a write nobody classified (the zero WriteClass): suspended ->
//	          403 tenant_suspended (§8's JSON body) and NO BUSINESS ROW written -- the venues and
//	          employees row for row, the trail, the live ledger, the feed, the invitation queue and
//	          the invitations are unchanged; the twin -> its 2xx, and its trace DID change (the
//	          control: the trace sees a write)
//	maintenance  the two writes a refused request still makes, measured rather than hidden: an
//	          EXPIRED ledger row planted before each closed cell is gone after the 403 (the runner's
//	          lazy delete commits on its own before the checkpoint -- apikey.Writes.Run, API-4's
//	          design, maintenance and not business data); the chain's last_used_at is the other
//	          (TouchAPIKey, at most once a minute) and is not in the trace
//	lifted    after op_reinstate_tenant every closed cell is its 2xx for KF too
//
// Every non-GET route the router registers is a cell (the walk again): a write added later is in
// this table or the test is red.
//
// MUTANTS (measured, API-4b report): the reader always false; WriteLocationPut open while suspended;
// WriteEmployeeChange open; the zero class open -- each red here.
func TestSuspension_TheSection47Table(t *testing.T) {
	w := newPunchWorld(t, unmetered)
	r := w.r
	cells := s47Cells()

	// Derive the router's routes: every one is a cell, and every cell's route is the router's.
	saved := r.s.testRoutes
	r.s.testRoutes = nil
	mux := chi.NewRouter()
	r.s.Mount(mux)
	r.s.testRoutes = saved
	var routes []string
	if err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		route = strings.TrimSuffix(strings.ReplaceAll(route, "/"+wildcard+"/", "/"), "/"+wildcard)
		if route != Prefix && route != Prefix+"/" {
			routes = append(routes, method+" "+route)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	covered := map[string]bool{}
	for _, c := range cells {
		if c.route != "" {
			covered[c.method+" "+c.route] = true
		}
	}
	var uncovered []string
	for _, rt := range routes {
		if !covered[rt] {
			uncovered = append(uncovered, rt)
		}
		delete(covered, rt)
	}
	sort.Strings(uncovered)
	if len(routes) < 9 || len(uncovered) != 0 || len(covered) != 0 {
		t.Fatalf("the router's /v1 routes %v: not a cell %v, a cell no route has %v", routes, uncovered, covered)
	}

	sus, twin := w.s47(w.kf), w.s47(w.km)
	r.suspend(sus.b.id)
	if !r.isSuspended(sus.b.id) || r.isSuspended(twin.b.id) {
		t.Fatal("PREMISE: the operator's call did not suspend KF alone")
	}
	for _, c := range cells {
		t.Run(c.name, func(t *testing.T) {
			var expired int
			if !c.open {
				expired = r.expiredLedger(sus.b.id, sus.b.keyID)
			}
			before := r.trace(sus.b.id)
			got := r.do(c.method, c.path(sus), sus.b.raw, c.body(sus))
			if c.open {
				if got.Code != c.ok {
					t.Fatalf("suspended: %d %s, want %d (open while suspended)", got.Code, got.Body.String(), c.ok)
				}
			} else {
				wantSuspendedProblem(t, got, "suspended")
				if after := r.trace(sus.b.id); after != before {
					t.Fatalf("a refused write wrote a business row: %s -> %s", before, after)
				}
				if expired == 0 || r.expiredRows(sus.b.id) != 0 {
					t.Fatalf("the lazy delete: %d expired ledger row(s) before the refused write, %d after; want >0 swept to 0 (the expected maintenance write)",
						expired, r.expiredRows(sus.b.id))
				}
			}
			twinBefore := r.trace(twin.b.id)
			if got := r.do(c.method, c.path(twin), twin.b.raw, c.body(twin)); got.Code != c.ok {
				t.Fatalf("not suspended: %d %s, want %d", got.Code, got.Body.String(), c.ok)
			}
			if !c.open && r.trace(twin.b.id) == twinBefore {
				t.Fatal("CONTROL: the twin's write left no trace in the trace -- it would not see a refused one either")
			}
		})
	}
	r.reinstate(sus.b.id)
	if r.isSuspended(sus.b.id) {
		t.Fatal("PREMISE: op_reinstate_tenant did not lift the suspension")
	}
	for _, c := range cells {
		if c.open {
			continue
		}
		t.Run("lifted/"+c.name, func(t *testing.T) {
			if got := r.do(c.method, c.path(sus), sus.b.raw, c.body(sus)); got.Code != c.ok {
				t.Fatalf("after op_reinstate_tenant: %d %s, want %d", got.Code, got.Body.String(), c.ok)
			}
		})
	}
}

// TestSuspension_ARefusedWriteLeavesNoIdempotencyRow (ADR 0028 §8 + §10): the Idempotency-Key claim
// is made BEFORE the work, in the write's transaction, so the 403 rolls it back with everything else.
// Measured and pinned: a closed PUT with a key is 403 and leaves no ledger row; the SAME request
// with the same key again is 403 again (not a replay -- nothing was recorded); once the operator
// lifts the suspension, the same request with the same key is a FIRST write (201, no
// Idempotent-Replayed) and the next one its replay. A 403 never burns a key.
func TestSuspension_ARefusedWriteLeavesNoIdempotencyRow(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	r.suspend(biz)
	ref, idem, body := newRef("IDEM"), "idem-"+uuid.NewString(), `{"name":"Zejtun","active":true}`
	for i := range 2 {
		w := r.putLoc(raw, ref, body, "Idempotency-Key", idem)
		wantSuspendedProblem(t, w, fmt.Sprintf("suspended, attempt %d", i+1))
		if w.Header().Get("Idempotent-Replayed") != "" || r.ledgerRows(biz, idem) != 0 || len(r.venuesByRef(biz, ref)) != 0 {
			t.Fatalf("attempt %d: replayed %q, %d ledger row(s), %d venue(s); want none of them",
				i+1, w.Header().Get("Idempotent-Replayed"), r.ledgerRows(biz, idem), len(r.venuesByRef(biz, ref)))
		}
	}
	r.reinstate(biz)
	first := r.putLoc(raw, ref, body, "Idempotency-Key", idem)
	if first.Code != http.StatusCreated || first.Header().Get("Idempotent-Replayed") != "" || r.ledgerRows(biz, idem) != 1 {
		t.Fatalf("lifted, the same key: %d %v, %d ledger row(s); want a first 201 and one row", first.Code, first.Header(), r.ledgerRows(biz, idem))
	}
	again := r.putLoc(raw, ref, body, "Idempotency-Key", idem)
	if again.Code != http.StatusCreated || again.Header().Get("Idempotent-Replayed") != "true" || again.Body.String() != first.Body.String() {
		t.Fatalf("the retry: %d %v %s; want the replay of the first", again.Code, again.Header(), again.Body.String())
	}
}

// TestSuspension_OneBusinessesSuspensionIsNotAnothers is the belt across tenants (§4.5): with A
// suspended, B's closed writes are B's 2xx and A's are 403; then the other way round (A lifted, B
// suspended). The reader asks for the KEY's business in the key's RLS context, so no business ever
// reads another's suspension.
func TestSuspension_OneBusinessesSuspensionIsNotAnothers(t *testing.T) {
	r := newDBRig(t)
	a, aOwner := r.business(true)
	b, bOwner := r.business(true)
	aRaw, _ := r.key(a, aOwner)
	bRaw, _ := r.key(b, bOwner)
	put := func(raw string) *httptest.ResponseRecorder {
		return r.putLoc(raw, newRef("BELT"), `{"name":"Qormi","active":true}`)
	}
	r.suspend(a)
	wantSuspendedProblem(t, put(aRaw), "A suspended, A's write")
	if w := put(bRaw); w.Code != http.StatusCreated {
		t.Fatalf("A suspended, B's write: %d %s, want 201", w.Code, w.Body.String())
	}
	r.reinstate(a)
	r.suspend(b)
	wantSuspendedProblem(t, put(bRaw), "B suspended, B's write")
	if w := put(aRaw); w.Code != http.StatusCreated {
		t.Fatalf("B suspended (A lifted), A's write: %d %s, want 201", w.Code, w.Body.String())
	}
}

// TestSuspension_ARacingNewRefIsAskedAgainAsAChange pins PutEmployeeTx's race branch
// (internal/domain/tenant/staffapi.go, errRefTaken): a PUT of a ref nobody holds passes the checkpoint
// as a NEW employee (open while suspended) and then meets, on the ref's unique index, a row another
// transaction inserted and commits -- TestEmployeesPutDomain_ConcurrentNewRefOpensOneRow's shape. The
// PUT is now a CHANGE of that row, and the checkpoint is asked again for it: in the suspended business
// 403 tenant_suspended and the row stays as its holder wrote it (no trail row for it); in the
// unsuspended twin the same race is 200 and the row carries the PUT's names (the control).
//
// MUTANT (measured, API-4b DÜŞÜK round): the race branch asking admit(ctx, EmployeeOpens) -> red here
// (the suspended business's PUT is 200 and rewrites the row); it was green everywhere before.
func TestSuspension_ARacingNewRefIsAskedAgainAsAChange(t *testing.T) {
	r := newDBRig(t)
	sus, sOwner := r.business(true)
	twin, tOwner := r.business(true)
	sRaw, _ := r.key(sus, sOwner)
	tRaw, _ := r.key(twin, tOwner)
	r.suspend(sus)
	ctx := context.Background()
	race := func(biz uuid.UUID, raw string) (*httptest.ResponseRecorder, string, string) {
		t.Helper()
		ref := newRef("RACE")
		inserted, decide, holder := make(chan struct{}), make(chan bool, 1), make(chan error, 1)
		go func() {
			holder <- r.d.WithTenant(ctx, biz, func(ctx context.Context, tx pgx.Tx) error {
				if _, e := tx.Exec(ctx, `INSERT INTO employees (tenant_id, external_ref, full_name) VALUES ($1, $2, 'Held Row')`, biz, ref); e != nil {
					close(inserted)
					return e
				}
				close(inserted)
				if !<-decide {
					return errors.New("roll the holder back")
				}
				return nil
			})
		}()
		<-inserted
		put := make(chan *httptest.ResponseRecorder, 1)
		go func() { put <- r.putEmp(raw, ref, empBody("Maria", "Borg", nil, true)) }()
		deadline := time.Now().Add(10 * time.Second)
		for r.ownerCount(`SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE '%name: CreateEmployeeFromAPI%'`) < 1 {
			if time.Now().After(deadline) {
				decide <- false
				<-holder
				t.Fatal("the PUT never waited on the held row's index entry")
			}
			time.Sleep(10 * time.Millisecond)
		}
		decide <- true
		if err := <-holder; err != nil {
			t.Fatal(err)
		}
		w := <-put
		var full, first string
		if err := r.owner.QueryRow(ctx, `SELECT full_name, coalesce(first_name, '') FROM employees WHERE tenant_id = $1 AND external_ref = $2`, biz, ref).Scan(&full, &first); err != nil {
			t.Fatal(err)
		}
		return w, full, first
	}
	w, full, first := race(sus, sRaw)
	wantSuspendedProblem(t, w, "the suspended business's racing PUT")
	if full != "Held Row" || first != "" {
		t.Fatalf("the suspended business's racing PUT changed the held row: %q %q", full, first)
	}
	w, full, first = race(twin, tRaw)
	if w.Code != http.StatusOK || full != "Maria Borg" || first != "Maria" {
		t.Fatalf("CONTROL, the twin's racing PUT: %d %s, row %q %q; want 200 and the PUT's names", w.Code, w.Body.String(), full, first)
	}
}

// elsewhere is the shipped reader asked about ANOTHER business than the key's: inside the key's RLS
// context that row is invisible, so the read fails -- the shape of every read failure the checkpoint
// must answer closed.
type elsewhere struct{ other uuid.UUID }

func (e elsewhere) Suspended(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (bool, error) {
	return tenant.WriteSuspension{}.Suspended(ctx, tx, e.other)
}

// TestSuspension_AReadThatFailsIsClosed is TestWrite_ACheckpointThatCannotReadIsClosed with the
// SHIPPED reader (API-4 review D2, re-run on the column):
//
//	the reader   in A's context: A's own row answers (false, then true once suspended); B's row and
//	             an id no business has are errors wrapping pgx.ErrNoRows -- RLS hides B (the braces),
//	             and the query names the id it was asked for (the belt) -- never "not suspended"
//	the surface  with a reader whose read fails, every class -- the two open while suspended included
//	             -- is 503 unavailable, and nothing of the business changes
//
// MUTANTS (measured, API-4b report): Admit treating the error as "not suspended"; the query's
// `id = @tenant_id` dropped (B's id then reads A's row) -- each red here.
func TestSuspension_AReadThatFailsIsClosed(t *testing.T) {
	r := newDBRig(t)
	a, aOwner := r.business(true)
	b, _ := r.business(true)
	read := func(id uuid.UUID) (bool, error) {
		var s bool
		err := r.d.WithTenant(context.Background(), a, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			s, err = tenant.WriteSuspension{}.Suspended(ctx, tx, id)
			return err
		})
		return s, err
	}
	if s, err := read(a); err != nil || s {
		t.Fatalf("A's own row, not suspended: %v %v", s, err)
	}
	r.suspend(a)
	if s, err := read(a); err != nil || !s {
		t.Fatalf("A's own row, suspended: %v %v", s, err)
	}
	r.reinstate(a)
	for what, id := range map[string]uuid.UUID{"B's row in A's context": b, "an id no business has": uuid.New()} {
		if s, err := read(id); !errors.Is(err, pgx.ErrNoRows) || s {
			t.Fatalf("%s: %v %v, want an error wrapping pgx.ErrNoRows", what, s, err)
		}
	}

	// Through the surface: the rig's reader now asks about B.
	r.s.suspension = elsewhere{other: b}
	raw, _ := r.key(a, aOwner)
	before := r.trace(a)
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPut, "/v1/locations/" + newRef("FAIL"), `{"name":"Mosta","active":true}`},
		{http.MethodPut, "/v1/employees/" + newRef("FAIL"), empBody("New", "Hire", nil, true)},
		{http.MethodPut, "/v1/_unclassified/" + uuid.NewString(), `{}`},
	} {
		w := r.do(c.method, c.path, raw, c.body)
		if w.Code != http.StatusServiceUnavailable || problemOf(t, w).Code != CodeUnavailable {
			t.Fatalf("%s %s with a failing read: %d %s, want 503 unavailable", c.method, c.path, w.Code, w.Body.String())
		}
	}
	if after := r.trace(a); after != before {
		t.Fatalf("a write whose checkpoint could not read wrote: %s -> %s", before, after)
	}
}

// heldWrites starts n /_probe PUTs of raw (refs ref-0 .. ref-<n-1>) and holds each right after its
// checkpoint passed -- its transaction open, before its effect -- until release; it returns once all
// n are held. A probe that passes its checkpoint after release goes straight on. done carries the
// n answers. The hold is released when the test ends whatever happens.
func heldWrites(t *testing.T, r *dbRig, raw, ref string, n int) (release func(), done <-chan *httptest.ResponseRecorder) {
	t.Helper()
	adm, rel, out := make(chan struct{}, n), make(chan struct{}), make(chan *httptest.ResponseRecorder, n)
	hook := func() {
		select {
		case <-rel:
			return
		default:
		}
		adm <- struct{}{}
		<-rel
	}
	r.afterAdmit.Store(&hook)
	var once bool
	release = func() {
		if !once {
			once = true
			close(rel)
		}
	}
	t.Cleanup(func() {
		release()
		r.afterAdmit.Store(nil)
	})
	for i := range n {
		go func() { out <- r.do(http.MethodPut, fmt.Sprintf("/v1/_probe/%s-%d", ref, i), raw, `{}`) }()
	}
	for range n {
		select {
		case <-adm:
		case <-time.After(30 * time.Second):
			t.Fatal("a write never reached its checkpoint")
		}
	}
	return release, out
}

// TestSuspension_TheReadHoldsNoLockOnTheTenant is API-4b's lock decision, pinned (ADR 0028 §10, "API-4b
// eki"; suspension.go): while a /v1 write of the business is in flight PAST its checkpoint, the
// operator's op_suspend_tenant -- FOR NO KEY UPDATE on the tenant's row -- takes the row at once
// (under a 300 ms lock_timeout: no 55P03, well under it) and commits. The checkpoint's read holds no
// lock the operator's write could wait for.
//
// MUTANT (measured, API-4b report): GetTenantSuspended with FOR SHARE -> the operator's call 55P03 --
// red; with FOR KEY SHARE -> green (it does not conflict with FOR NO KEY UPDATE: orders nothing).
func TestSuspension_TheReadHoldsNoLockOnTheTenant(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	r.liftAtEnd(biz) // before the hold: the held write is released first at the end (LIFO)
	release, done := heldWrites(t, r, raw, "held-"+uuid.NewString(), 1)
	start := time.Now()
	err := r.trySuspend(biz, "300ms")
	took := time.Since(start)
	if err != nil {
		t.Fatalf("op_suspend_tenant beside a write in flight: %v after %v; want it through at once", err, took)
	}
	if took > 250*time.Millisecond {
		t.Fatalf("op_suspend_tenant took %v beside a write in flight; want no wait", took)
	}
	t.Logf("op_suspend_tenant beside a /v1 write in flight: %v", took)
	release()
	<-done
}

// TestSuspension_AWriteInFlightAtTheSuspensionIsLetThrough is the price of the plain read, named and
// pinned (suspension.go): EVERY write that passed its checkpoint BEFORE the operator's suspension
// committed commits AFTER it (201, its effect written) -- one write per request already past its
// read, and in total every request in flight at the suspension's commit (here three at once; the
// bound is the request's: RequestTimeout 30 s, idle_in_transaction_session_timeout 30 s, the pool and
// the key bucket) -- and the next write, which reads after the commit, is 403 tenant_suspended.
func TestSuspension_AWriteInFlightAtTheSuspensionIsLetThrough(t *testing.T) {
	r := newDBRig(t)
	biz, owner := r.business(true)
	raw, _ := r.key(biz, owner)
	ref := "inflight-" + uuid.NewString()
	const inFlight = 3
	r.liftAtEnd(biz) // before the hold: the held writes are released first at the end (LIFO)
	release, done := heldWrites(t, r, raw, ref, inFlight)
	// Bounded: under a locking read the suspension would wait on the held writes (the FOR SHARE
	// mutant measured 30 s, until their transactions were killed); here it fails in 2 s instead.
	if err := r.trySuspend(biz, "2s"); err != nil {
		t.Fatalf("op_suspend_tenant beside %d writes in flight: %v", inFlight, err)
	}
	if !r.isSuspended(biz) {
		t.Fatal("PREMISE: the suspension is not committed")
	}
	release()
	for range inFlight {
		if w := <-done; w.Code != http.StatusCreated {
			t.Fatalf("a write in flight: %d %s; want 201 -- the plain read's named price", w.Code, w.Body.String())
		}
	}
	for i := range inFlight {
		if n := r.auditCount(biz, fmt.Sprintf("%s-%d", ref, i)); n != 1 {
			t.Fatalf("write in flight %d: %d effect(s), want its one", i, n)
		}
	}
	next := "after-" + uuid.NewString()
	wantSuspendedProblem(t, r.do(http.MethodPut, "/v1/_probe/"+next, raw, `{}`), "the next write")
	if r.auditCount(biz, next) != 0 {
		t.Fatal("the next write wrote")
	}
}

// TestSuspension_ATapGoesOn (plan §4.7, left column: "tetikleyicilerin olay/teslim/izdüşüm yazması --
// askı dokunuşu durdurmaz, holding dokunuşu dahil"): KF and KM are both suspended by the operator, and
//
//	own tap      KF's employee on KF's plaque -> KF's feed has the event (the 00049 trigger)
//	delivery     the same with an active endpoint, in a transaction rolled back -> the trigger wrote
//	             the delivery row (nothing of it stays)
//	holding tap  KM's employee on KF's plaque -> the event in KM's feed (employer) AND in KF's
//	             (location), and KF's site_punches projection row
//	the read     GET /v1/punches of the suspended KF answers the events (every GET is open)
//
// No trigger function reads the suspension at all: TestSuspension_NoWorkerOrTriggerReadsIt.
func TestSuspension_ATapGoesOn(t *testing.T) {
	w := newPunchWorld(t)
	r := w.r
	r.suspend(w.kf.id)
	r.suspend(w.km.id)
	count := func(q string, args ...any) int { return r.ownerCount(q, args...) }
	kfFeed := count(`SELECT count(*) FROM punch_events WHERE tenant_id = $1`, w.kf.id)
	kmFeed := count(`SELECT count(*) FROM punch_events WHERE tenant_id = $1`, w.km.id)
	kfSite := count(`SELECT count(*) FROM site_punches WHERE tenant_id = $1`, w.kf.id)

	ownID := w.write(own(w.kf, "nfc", "in"))
	if n := count(`SELECT count(*) FROM punch_events WHERE tenant_id = $1`, w.kf.id); n != kfFeed+1 {
		t.Fatalf("KF's own tap while suspended: KF's feed %d -> %d, want one event", kfFeed, n)
	}
	holdingID := w.write(at(w.km, w.kf, "in"))
	if n := count(`SELECT count(*) FROM punch_events WHERE tenant_id = $1`, w.km.id); n != kmFeed+1 {
		t.Fatalf("KM@KF while both suspended: KM's feed %d -> %d, want one event", kmFeed, n)
	}
	if n := count(`SELECT count(*) FROM punch_events WHERE tenant_id = $1`, w.kf.id); n != kfFeed+2 {
		t.Fatalf("KM@KF while both suspended: KF's feed %d -> %d, want the location's event too", kfFeed+1, n)
	}
	if n := count(`SELECT count(*) FROM site_punches WHERE tenant_id = $1 AND transaction_id = $2`, w.kf.id, holdingID); n != 1 || count(`SELECT count(*) FROM site_punches WHERE tenant_id = $1`, w.kf.id) != kfSite+1 {
		t.Fatalf("KM@KF while both suspended: %d projection row(s) in KF, want one", n)
	}

	// The delivery, in a transaction rolled back: an active endpoint and a tap, as tappa_app in KF's
	// context; the trigger's delivery row is counted inside it.
	ctx := context.Background()
	tx, err := r.owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var endpoint uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO webhook_endpoints (tenant_id, url, signing_key_sealed, active)
	                           VALUES ($1, 'https://hooks.example.test/tappa', '\x00'::bytea, true) RETURNING id`, w.kf.id).Scan(&endpoint); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`SELECT set_config('app.tenant_id', '` + w.kf.id.String() + `', true)`, `SET LOCAL ROLE tappa_app`} {
		if _, err := tx.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.insertPunch(ctx, tx, own(w.kf, "nfc", "out")); err != nil {
		t.Fatalf("a tap as tappa_app while suspended: %v", err)
	}
	if _, err := tx.Exec(ctx, `RESET ROLE`); err != nil {
		t.Fatal(err)
	}
	var deliveries int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE tenant_id = $1 AND endpoint_id = $2`, w.kf.id, endpoint).Scan(&deliveries); err != nil || deliveries != 1 {
		t.Fatalf("the tap's delivery while suspended: %d (%v), want one", deliveries, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	got := ids(pageOf(t, w.get(w.kf.raw, "scope=employer")).Data)
	if !slices.Contains(got, pch(ownID)) {
		t.Fatalf("GET /v1/punches of the suspended KF: %v, want its tap %s", got, pch(ownID))
	}
	loc := ids(pageOf(t, w.get(w.kf.raw, "scope=location")).Data)
	if !slices.Contains(loc, pch(holdingID)) {
		t.Fatalf("GET /v1/punches?scope=location of the suspended KF: %v, want the holding tap %s", loc, pch(holdingID))
	}
}

// TestSuspension_NoWorkerOrTriggerReadsIt (plan §4.7, left column: webhook sending, the invitation
// queue's worker, the triggers' event/delivery/projection writes are OPEN while suspended -- they
// never consult the suspension):
//
//	the workers   no non-test Go file of the sender (internal/webhook, internal/webhook/delivery) or
//	              of internal/invite (the queue's worker), and no query file they read (webhooks.sql,
//	              invitequeue.sql, invites.sql), names the suspension -- its two columns, the store
//	              read, the reader, the word
//	the database  (DATABASE_URL) the only functions whose bodies name suspended_at are 00063's op_*
//	              (owned by tappa_opdefiner, tappa_operator's surface): no trigger function reads it
func TestSuspension_NoWorkerOrTriggerReadsIt(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	words := []string{"suspended_at", "suspension_reason", "GetTenantSuspended", "WriteSuspension", "Suspended(", "Suspension", "suspend"}
	scanned := 0
	for _, dir := range []string{"internal/webhook", "internal/invite"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			// internal/webhook/endpoints is the PANEL's endpoint management (create, rotate, re-address,
			// test ping, "send again"), which ADR 0028 §10 closes while suspended -- OP-15's panel gate,
			// not a worker. The sender is internal/webhook (worker.go) and internal/webhook/delivery.
			if d.IsDir() && d.Name() == "endpoints" {
				return filepath.SkipDir
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			scanned++
			for _, word := range words {
				if strings.Contains(string(b), word) {
					t.Errorf("%s names %q: a worker that reads the suspension", path, word)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"webhooks.sql", "invitequeue.sql", "invites.sql"} {
		b, err := os.ReadFile(filepath.Join(root, "db", "queries", f))
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		for _, word := range []string{"suspended_at", "suspension_reason"} {
			if strings.Contains(string(b), word) {
				t.Errorf("db/queries/%s names %s", f, word)
			}
		}
	}
	if scanned < 10 {
		t.Fatalf("anti-vacuity: %d files scanned", scanned)
	}

	if os.Getenv("DATABASE_MIGRATE_URL") == "" {
		t.Log("DATABASE_MIGRATE_URL not set: the database arm is skipped (CI runs it)")
		return
	}
	r := newDBRig(t)
	rows, err := r.owner.Query(context.Background(), `
		SELECT p.proname, pg_catalog.pg_get_userbyid(p.proowner), p.prorettype = 'trigger'::regtype
		  FROM pg_catalog.pg_proc p JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
		 WHERE n.nspname = 'public' AND p.prosrc LIKE '%suspended_at%'
		 ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name, owner string
		var trigger bool
		if err := rows.Scan(&name, &owner, &trigger); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
		if trigger || !strings.HasPrefix(name, "op_") || owner != "tappa_opdefiner" {
			t.Errorf("%s (owner %s, trigger %v) reads suspended_at: only the operator's op_* may", name, owner, trigger)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(names, "op_suspend_tenant") {
		t.Fatalf("anti-vacuity: the functions naming suspended_at are %v -- op_suspend_tenant is not among them", names)
	}
}

// TestSuspension_TheOperatorFixtureTakesTheTablesLock keeps this file's spelling of the operator-tables
// test lock equal to internal/operatorauth's (which pins internal/db's): a different text is a
// different lock, and this package's operator calls would race internal/db's DDL again.
func TestSuspension_TheOperatorFixtureTakesTheTablesLock(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "operatorauth", "harness_db_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `const operatorTablesTestLock = "`+operatorTablesTestLock+`"`) {
		t.Fatalf("internal/operatorauth spells the operator-tables lock differently from %q", operatorTablesTestLock)
	}
}
