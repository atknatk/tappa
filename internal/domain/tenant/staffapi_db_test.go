package tenant

// staffapi_db_test.go -- M11 API-6's employee domain (staffapi.go) against REAL Postgres: K-25's
// deactivation budget (counted from the trail, under the key's lock, in the deactivation's own
// transaction), K-15's retirement in the PUT's transaction, K-18's binding inside one business and
// K-24's 409. The HTTP surface's tests (internal/handler/api, employees_db_test.go) drive the same
// code through the chain; these pin the mechanisms where they live.
//
// FIXTURES ARE NOT CLEANED UP (staff_db_test.go's reason): fresh random ids, refs and addresses
// keep runs apart.

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// withPoolSize sets pool_max_conns on a DSN.
func withPoolSize(t *testing.T, dsn string, n int) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("pool_max_conns", strconv.Itoa(n))
	u.RawQuery = q.Encode()
	return u.String()
}

// admitAll is a checkpoint that lets every write through and remembers what it was asked.
type admitLog struct {
	mu  sync.Mutex
	got []EmployeeWrite
}

func (a *admitLog) admit(_ context.Context, w EmployeeWrite) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.got = append(a.got, w)
	return nil
}

func apiRef(prefix string) string {
	return prefix + "-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
}

// put runs PutEmployeeTx in a transaction of the tenant's own, as the API runner does.
func (f *staffFixture) put(t *testing.T, tenantID uuid.UUID, c EmployeePut) (EmployeePutResult, error) {
	t.Helper()
	c.TenantID = tenantID
	if c.ActorID == uuid.Nil {
		c.ActorID = f.actorID
	}
	if c.Via == "" {
		c.Via = "api"
	}
	a := &admitLog{}
	var out EmployeePutResult
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = f.staff.PutEmployeeTx(ctx, tx, c, a.admit)
		return err
	})
	return out, err
}

func (f *staffFixture) del(staff *Staff, tenantID, key uuid.UUID, ref string, budget int) (EmployeePutResult, error) {
	var out EmployeePutResult
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = staff.DeleteEmployeeTx(ctx, tx, EmployeeDelete{TenantID: tenantID, ActorID: key, Via: "api", ExternalRef: ref, Budget: budget})
		return err
	})
	return out, err
}

func (f *staffFixture) hire(t *testing.T, tenantID uuid.UUID, ref string, email *string) EmployeePutResult {
	t.Helper()
	res, err := f.put(t, tenantID, EmployeePut{ExternalRef: ref, FirstName: "Maria", LastName: "Borg", Email: email, Active: true})
	if err != nil {
		t.Fatalf("hire %s: %v", ref, err)
	}
	return res
}

func (f *staffFixture) countRows(t *testing.T, tenantID uuid.UUID, q string, args ...any) int {
	t.Helper()
	var n int
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, q, args...).Scan(&n)
	})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// TestDeactivationBudget_ComesFromTheDatabase (ADR 0028 §12, K-25: "bellekte değil"; plan §5 API-6:
// "bütçe DB'den ... yeniden başlatmada sıfırlanmıyor"): with a budget of 3, a key deactivates three
// employees and the fourth is *DeactivationBudgetExceeded (Retry-After within the hour, nothing
// written); a NEW Staff -- a restarted process, nothing shared in memory -- still refuses; a SECOND
// key of the same business has its own budget; an administrator's panel deactivation is never
// charged to a key; a deactivation older than the hour no longer counts (a trail row written with
// an earlier `at`); and an already deactivated ref is answered as it is, never charged.
func TestDeactivationBudget_ComesFromTheDatabase(t *testing.T) {
	f := newStaffFixture(t)
	key, other := uuid.New(), uuid.New()
	refs := make([]string, 6)
	for i := range refs {
		refs[i] = apiRef("BUD")
		f.hire(t, f.tenantID, refs[i], nil)
	}
	const budget = 3
	for i := 0; i < budget; i++ {
		res, err := f.del(f.staff, f.tenantID, key, refs[i], budget)
		if err != nil || !res.Deactivated || res.Employee.Active() {
			t.Fatalf("deactivation %d within the budget: %+v %v", i+1, res, err)
		}
	}
	// An administrator's deactivation in the panel: not the key's.
	if _, err := f.staff.Deactivate(context.Background(), DeactivateCommand{TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID}); err != nil {
		t.Fatal(err)
	}
	_, err := f.del(f.staff, f.tenantID, key, refs[budget], budget)
	var over *DeactivationBudgetExceeded
	if !errors.As(err, &over) || over.Budget != budget || over.RetryAfter <= 0 || over.RetryAfter > time.Hour {
		t.Fatalf("the fourth: %v, want *DeactivationBudgetExceeded with a Retry-After within the hour", err)
	}
	if got := f.liveRef(t, f.tenantID, refs[budget]); got != 1 {
		t.Fatalf("the refused deactivation left %d live rows under the ref, want 1 (nothing written)", got)
	}
	restarted, err := NewStaff(f.data, f.trail, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.del(restarted, f.tenantID, key, refs[budget], budget); !errors.As(err, &over) {
		t.Fatalf("after a restart: %v, want the budget still spent", err)
	}
	if res, err := f.del(restarted, f.tenantID, other, refs[budget], budget); err != nil || !res.Deactivated {
		t.Fatalf("another key of the business: %+v %v, want its own budget", res, err)
	}
	// Already deactivated: answered as it is, nothing charged -- even past the budget.
	if res, err := f.del(restarted, f.tenantID, key, refs[0], budget); err != nil || res.Deactivated || res.Changed || res.Employee.Active() {
		t.Fatalf("an already deactivated ref past the budget: %+v %v, want the row as it is", res, err)
	}
	// The window, both edges: a trail row 61 minutes old does not count, one 59 minutes old does
	// (API-6 review: a mutant narrowing the window to a minute stayed green without the second half).
	// `at` is the database's own (00053: tappa_app cannot write it); the owner backdates the rows.
	f2 := newStaffFixture(t)
	for _, c := range []struct {
		age     string
		refused bool
	}{{"61 minutes", false}, {"59 minutes", true}} {
		key := uuid.New()
		for range budget {
			ownerExec(t, `INSERT INTO audit_log (tenant_id, actor_id, action, target, detail, at)
			              VALUES ($1, $2, 'employee.deactivated', $3, '{"via":"api"}', now() - $4::interval)`,
				f2.tenantID, key, uuid.NewString(), c.age)
		}
		ref := apiRef("BUD")
		f2.hire(t, f2.tenantID, ref, nil)
		res, err := f2.del(f2.staff, f2.tenantID, key, ref, budget)
		var over *DeactivationBudgetExceeded
		switch {
		case c.refused && !errors.As(err, &over):
			t.Fatalf("%d deactivations %s old: %+v %v, want the budget spent", budget, c.age, res, err)
		case !c.refused && (err != nil || !res.Deactivated):
			t.Fatalf("%d deactivations %s old: %+v %v, want room in the budget", budget, c.age, res, err)
		}
	}
}

// ownerExec runs one statement as the owner (DATABASE_MIGRATE_URL) -- what the application role
// may not do, such as dating a trail row.
func ownerExec(t *testing.T, sql string, args ...any) {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL is required to date a trail row")
	}
	conn, err := pgx.Connect(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(context.Background()) }()
	if _, err := conn.Exec(context.Background(), sql, args...); err != nil {
		t.Fatal(err)
	}
}

func (f *staffFixture) liveRef(t *testing.T, tenantID uuid.UUID, ref string) int {
	return f.countRows(t, tenantID, `SELECT count(*) FROM employees WHERE tenant_id = $1 AND external_ref = $2 AND status <> 'deactivated'`, tenantID, ref)
}

// TestDeactivationBudget_ConcurrentLastSlotExactlyOne (ADR 0028 §12: "eşzamanlılıkta tam 1"; run
// with -race): (a) a key with ONE slot left and 12 concurrent deactivations of 12 different
// employees -> exactly 1 deactivated, 11 refused; (b) a fresh key with a budget of 5 and 12
// concurrent deactivations -> exactly 5. The trail agrees with the rows.
//
// MUTANTS (measured; ADR 0028 "API-6 uygulama notu"): the advisory lock removed -> more than one;
// the trail row written in a transaction of its own (audit.Record) -> more than one.
func TestDeactivationBudget_ConcurrentLastSlotExactlyOne(t *testing.T) {
	f := newStaffFixture(t)
	// A pool wide enough for every racer, so none waits for a connection while the others race.
	wide, err := db.New(context.Background(), &config.Config{DatabaseURL: withPoolSize(t, os.Getenv("DATABASE_URL"), 20)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wide.Close)
	f.data = wide
	if f.staff, err = NewStaff(wide, f.trail, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	run := func(t *testing.T, budget, spent, n int) (ok, refused int) {
		t.Helper()
		f.staff.beforeBudget = nil
		key := uuid.New()
		for range spent {
			ref := apiRef("CON")
			f.hire(t, f.tenantID, ref, nil)
			if _, err := f.del(f.staff, f.tenantID, key, ref, budget); err != nil {
				t.Fatal(err)
			}
		}
		refs := make([]string, n)
		for i := range refs {
			refs[i] = apiRef("CON")
			f.hire(t, f.tenantID, refs[i], nil)
		}
		// THE MEETING POINT: every racer's transaction is open and has found its employee before
		// any of them takes the budget's lock -- so without the lock all n would count the same
		// number (measured: a cold pool otherwise serialises the racers and hides the mutant).
		// A racer that does not arrive within two seconds does not hold the others forever.
		arrived := make(chan struct{}, n)
		release := make(chan struct{})
		f.staff.beforeBudget = func(context.Context) {
			arrived <- struct{}{}
			<-release
		}
		go func() {
			deadline := time.After(2 * time.Second)
			for range n {
				select {
				case <-arrived:
				case <-deadline:
					close(release)
					return
				}
			}
			close(release)
		}()
		var wg sync.WaitGroup
		var mu sync.Mutex
		start := make(chan struct{})
		for _, ref := range refs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				_, err := f.del(f.staff, f.tenantID, key, ref, budget)
				var over *DeactivationBudgetExceeded
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					ok++
				case errors.As(err, &over):
					refused++
				default:
					t.Errorf("a deactivation failed: %v", err)
				}
			}()
		}
		close(start)
		wg.Wait()
		rows := f.countRows(t, f.tenantID, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND actor_id = $2 AND action = 'employee.deactivated'`, f.tenantID, key)
		if rows != spent+ok {
			t.Fatalf("the trail holds %d of the key's deactivations, want %d", rows, spent+ok)
		}
		return ok, refused
	}
	t.Run("the last slot", func(t *testing.T) {
		if ok, refused := run(t, 5, 4, 12); ok != 1 || refused != 11 {
			t.Fatalf("one slot left, 12 at once: %d deactivated, %d refused; want exactly 1 and 11", ok, refused)
		}
	})
	t.Run("the whole budget", func(t *testing.T) {
		if ok, refused := run(t, 5, 0, 12); ok != 5 || refused != 7 {
			t.Fatalf("a budget of 5, 12 at once: %d deactivated, %d refused; want exactly 5 and 7", ok, refused)
		}
	})
}

// TestEmployeesPutDomain_EmailChangeRetiresInTheSameTransaction (K-15): a PUT that changes the
// address of somebody not activated retires every spendable invitation, writes the address, queues
// a new request (releasing a leased one) and leaves one employee.email_changed row -- one
// transaction: when the address is another live employee's (409), NOTHING of it stands (the
// invitation is still spendable, the address and the queue as they were). An activated person's
// new address queues nothing.
func TestEmployeesPutDomain_EmailChangeRetiresInTheSameTransaction(t *testing.T) {
	f := newStaffFixture(t)
	ref := apiRef("EML")
	a, b := uniqueAddress("old"), uniqueAddress("new")
	res := f.hire(t, f.tenantID, ref, &a)
	id := res.Employee.ID
	if !res.Created || !res.Queued || res.Employee.Invitation != InvitationQueued {
		t.Fatalf("a new employee with an address: %+v, want created and queued", res)
	}
	inv := f.seedInvite(t, f.tenantID, id, time.Hour, false)
	// A worker holds the request's lease.
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE invite_requests SET lease_until = clock_timestamp() + interval '60 seconds' WHERE tenant_id = $1 AND employee_id = $2`, f.tenantID, id)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	// 409: another live employee holds the address -- nothing of the PUT stands.
	taken := uniqueAddress("taken")
	f.seedColleague(t, f.tenantID, f.locationID, &taken)
	_, err := f.put(t, f.tenantID, EmployeePut{ExternalRef: ref, FirstName: "Maria", LastName: "Borg", Email: &taken, Active: true})
	if !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("another live employee's address: %v, want ErrEmailTaken", err)
	}
	if _, cancelled := f.inviteState(t, f.tenantID, inv); cancelled {
		t.Fatal("the refused change retired the invitation; its transaction must roll the retirement back")
	}
	if got := f.storedEmail(t, f.tenantID, id); got == nil || *got != a {
		t.Fatal("the refused change wrote the address")
	}
	// The change.
	res, err = f.put(t, f.tenantID, EmployeePut{ExternalRef: ref, FirstName: "Maria", LastName: "Borg", Email: &b, Active: true})
	if err != nil || !res.Changed || !res.Queued {
		t.Fatalf("the change: %+v %v", res, err)
	}
	if _, cancelled := f.inviteState(t, f.tenantID, inv); !cancelled {
		t.Fatal("the address changed and the old invitation is still spendable")
	}
	if got := f.storedEmail(t, f.tenantID, id); got == nil || *got != b {
		t.Fatal("the new address was not written byte for byte")
	}
	if n := f.countRows(t, f.tenantID, `SELECT count(*) FROM invite_requests WHERE tenant_id = $1 AND employee_id = $2 AND lease_until IS NULL AND attempts = 0`, f.tenantID, id); n != 1 {
		t.Fatalf("the request after the change: %d released rows, want 1 (the worker's lease cleared)", n)
	}
	rows := f.countRows(t, f.tenantID, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2 AND action = 'employee.email_changed' AND detail->>'via' = 'api' AND (detail->>'retired_invitations')::int = 1`, f.tenantID, id.String())
	if rows != 1 {
		t.Fatalf("%d employee.email_changed rows with one retired invitation, want 1", rows)
	}
	trail := f.countRows(t, f.tenantID, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2 AND (detail::text ILIKE $3 OR detail::text ILIKE $4)`, f.tenantID, id.String(), "%"+a+"%", "%"+b+"%")
	if trail != 0 {
		t.Fatal("a trail row carries an address")
	}

	// Activated: a new address queues nothing.
	if err := f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `DELETE FROM invite_requests WHERE tenant_id = $1 AND employee_id = $2`, f.tenantID, id)
		if e != nil {
			return e
		}
		code, binding := randHash(t), randHash(t)
		if _, e := tx.Exec(ctx, `INSERT INTO employee_invites (tenant_id, employee_id, code_hash, expires_at, consented_at, consent_binding_hash)
		                         VALUES ($1, $2, $3, now() + interval '1 hour', now(), $4)`, f.tenantID, id, code, binding); e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `WITH c AS (UPDATE employee_invites SET used_at = now() WHERE tenant_id = $1 AND code_hash = $2 RETURNING employee_id)
		                     UPDATE employees SET status = 'active', activated_at = now() WHERE tenant_id = $1 AND id = (SELECT employee_id FROM c)`, f.tenantID, code)
		return e
	}); err != nil {
		t.Fatal(err)
	}
	c := uniqueAddress("after")
	res, err = f.put(t, f.tenantID, EmployeePut{ExternalRef: ref, FirstName: "Maria", LastName: "Borg", Email: &c, Active: true})
	if err != nil || !res.Changed || res.Queued || res.Employee.Invitation != InvitationNone {
		t.Fatalf("an activated person's new address: %+v %v, want written and nothing queued", res, err)
	}
}

// TestEmployeesPutDomain_LinkRehireAndNames (K-18, K-24, ADR 0010): a panel employee with no ref and
// the PUT's address -- differing in capitals -- is BOUND (employee.linked), not duplicated, and the
// change of capitals is an address change; the same address in ANOTHER business is never bound;
// active:false then active:true opens a NEW row under the same ref (rehire_of the old one); and the
// name rule: a control character, a direction override or a word joiner is hidden_character, a name
// of only spaces "cannot be stored", the two names together past 120 runes too.
func TestEmployeesPutDomain_LinkRehireAndNames(t *testing.T) {
	f := newStaffFixture(t)
	addr := uniqueAddress("link")
	panelRow := f.seedColleague(t, f.tenantID, f.locationID, &addr)
	// The same address in the OTHER business -- K-24: never searched.
	foreignRow := f.seedColleague(t, f.foreignTenant, f.foreignVenue, &addr)
	ref := apiRef("LNK")
	lower := strings.ToLower(addr)
	res, err := f.put(t, f.tenantID, EmployeePut{ExternalRef: ref, FirstName: "Joe", LastName: "Camilleri", Email: &lower, Active: true})
	if err != nil || !res.Linked || res.Created || res.Employee.ID != panelRow {
		t.Fatalf("the binding: %+v %v, want the panel row bound", res, err)
	}
	if n := f.countRows(t, f.tenantID, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2 AND action = 'employee.linked'`, f.tenantID, panelRow.String()); n != 1 {
		t.Fatalf("%d employee.linked rows, want 1", n)
	}
	if got := f.storedEmail(t, f.tenantID, panelRow); got == nil || *got != lower {
		t.Fatal("the capitals the PUT sent were not written")
	}
	var foreignRef *string
	if err := f.data.WithTenant(context.Background(), f.foreignTenant, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT external_ref FROM employees WHERE tenant_id = $1 AND id = $2`, f.foreignTenant, foreignRow).Scan(&foreignRef)
	}); err != nil || foreignRef != nil {
		t.Fatalf("the other business's employee with the same address: ref %v %v, want untouched", foreignRef, err)
	}
	// Re-hire.
	res, err = f.put(t, f.tenantID, EmployeePut{ExternalRef: ref, FirstName: "Joe", LastName: "Camilleri", Email: &lower})
	if err != nil || !res.Deactivated {
		t.Fatalf("active:false: %+v %v", res, err)
	}
	res, err = f.put(t, f.tenantID, EmployeePut{ExternalRef: ref, FirstName: "Joe", LastName: "Camilleri", Email: &lower, Active: true})
	if err != nil || !res.Created || !res.Rehired || res.Employee.ID == panelRow || res.Employee.ExternalRef == nil || *res.Employee.ExternalRef != ref {
		t.Fatalf("active:true after a deactivation: %+v %v, want a NEW row under the same ref", res, err)
	}
	if n := f.countRows(t, f.tenantID, `SELECT count(*) FROM audit_log WHERE tenant_id = $1 AND target = $2 AND action = 'employee.added' AND detail->>'rehire_of' = $3`, f.tenantID, res.Employee.ID.String(), panelRow.String()); n != 1 {
		t.Fatal("the re-hire's employee.added row does not name the row it follows")
	}
	// Names.
	for _, tc := range []struct {
		first, last string
		want        error
		field       EmployeeField
	}{
		{"Ma\u0007ria", "Borg", ErrNameHiddenCharacter, FieldFirstName},
		{"Maria", "Bo\trg", ErrNameHiddenCharacter, FieldLastName},
		{"Maria", "Bo\u202erg", ErrNameHiddenCharacter, FieldLastName},
		{"Ma\u2060ria", "Borg", ErrNameHiddenCharacter, FieldFirstName},
		{"   ", "Borg", ErrEmployeeName, FieldFirstName},
		{strings.Repeat("a", 60), strings.Repeat("b", 60), ErrEmployeeName, FieldLastName},
	} {
		_, err := f.put(t, f.tenantID, EmployeePut{ExternalRef: apiRef("NAM"), FirstName: tc.first, LastName: tc.last, Active: true})
		var fe *EmployeeFieldError
		if !errors.Is(err, tc.want) || !errors.As(err, &fe) || fe.Field != tc.field {
			t.Errorf("%q %q: %v, want %v on %s", tc.first, tc.last, err, tc.want, tc.field)
		}
	}
	if res, err := f.put(t, f.tenantID, EmployeePut{ExternalRef: apiRef("NAM"), FirstName: " Ma\u00adria ", LastName: "de  la Cruz", Active: true}); err != nil ||
		res.Employee.FirstName == nil || *res.Employee.FirstName != "Maria" || res.Employee.FullName != "Maria de la Cruz" {
		t.Fatalf("cleaning: %+v %v, want Maria / Maria de la Cruz", res.Employee, err)
	}
}

// TestEmployeesPutDomain_ConcurrentNewRefOpensOneRow (ADR 0028 §9: a repeated PUT opens no row): an
// uncommitted employee under a new ref is held in another transaction while four PUTs of the same
// ref wait on its index entry (pg_stat_activity: each CreateEmployeeFromAPI in a lock wait). The
// holder COMMITS -> the four become changes of its row (no Created), one live row; the holder ROLLS
// BACK -> exactly one PUT creates, three change it, one live row. The insert runs in a savepoint, so
// the losers carry on in their own transactions.
//
// MUTANT (measured; ADR 0028 "API-6 uygulama notu"): the savepoint's errRefTaken arm removed -> the
// losers fail with the 23505.
func TestEmployeesPutDomain_ConcurrentNewRefOpensOneRow(t *testing.T) {
	f := newStaffFixture(t)
	wide, err := db.New(context.Background(), &config.Config{DatabaseURL: withPoolSize(t, os.Getenv("DATABASE_URL"), 12)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(wide.Close)
	f.data = wide
	if f.staff, err = NewStaff(wide, f.trail, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatal(err)
	}
	for _, commit := range []bool{true, false} {
		ref := apiRef("RACE")
		inserted, decide := make(chan struct{}), make(chan bool)
		holder := make(chan error, 1)
		go func() {
			holder <- f.data.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
				if _, e := tx.Exec(ctx, `INSERT INTO employees (tenant_id, external_ref, full_name) VALUES ($1, $2, 'Held Row')`, f.tenantID, ref); e != nil {
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
		type result struct {
			res EmployeePutResult
			err error
		}
		results := make(chan result, 4)
		for range 4 {
			go func() {
				res, err := f.put(t, f.tenantID, EmployeePut{ExternalRef: ref, FirstName: "Maria", LastName: "Borg", Active: true})
				results <- result{res, err}
			}()
		}
		deadline := time.Now().Add(10 * time.Second)
		for f.countRows(t, f.tenantID, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query LIKE '%name: CreateEmployeeFromAPI%'`) < 4 {
			if time.Now().After(deadline) {
				decide <- false
				t.Fatal("the four PUTs never all waited on the held row")
			}
			time.Sleep(10 * time.Millisecond)
		}
		decide <- commit
		herr := <-holder
		if commit && herr != nil {
			t.Fatal(herr)
		}
		created := 0
		for range 4 {
			r := <-results
			if r.err != nil {
				t.Fatalf("commit=%v: a racing PUT failed: %v", commit, r.err)
			}
			if r.res.Created {
				created++
			}
		}
		want := 0
		if !commit {
			want = 1
		}
		if created != want || f.liveRef(t, f.tenantID, ref) != 1 {
			t.Fatalf("commit=%v: %d created, %d live rows; want %d and 1", commit, created, f.liveRef(t, f.tenantID, ref), want)
		}
	}
}
