package invite

// email_db_test.go — M10 EM-7B's e-mail route against REAL Postgres: the issuing
// limits counted under the business's lock, the address read in the minting
// transaction after the row, the four refusals that mint nothing, and the audit row
// each outcome leaves. The sink is a fake (the relay is internal/handler's to drive);
// the database, the locks and the trail are real, because they are the subject.
//
// WHAT ONE RUN LEAVES BEHIND (nothing here can be deleted by tappa_app, and no test
// reaches for the owner role — manager_db_test.go's convention), counted from the
// code and measured (EM-7B round 3: one run moved the dev database's counts of these
// fixtures by exactly these numbers), one full run of this file:
//
//	tenants ("EM7B Test …")   18, each with ONE venue and ONE administrator row
//	employees                 63
//	employee_invites rows    527 (the limit test seeds 49 + 299 + 3 of them;
//	                              ConcurrentPresses alone: 6 tenants, 42 employees,
//	                              159 invitations)
//	audit_log rows            92 — 35 invite.code_emailed, 55 invite.email_refused,
//	                              1 invite.undelivered, 1 invite.code_shown_to_manager
//
// Every id is a fresh random UUID and every address is under example.test; no real
// tenant's data is touched. `make db-reset` clears the dev database.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/mail"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// mailFixture is one business with a venue and an administrator, and the people a
// test adds to it.
type mailFixture struct {
	d        *db.DB
	tenantID uuid.UUID
	venueID  uuid.UUID
	admin    string // the administrator's address, mixed case
}

// newMailFixture commits a business whose VAT verification is verified (nil = NULL,
// never asked), a venue, and one owner holding a mixed-case address.
func newMailFixture(t *testing.T, d *db.DB, name string, verified *bool) mailFixture {
	t.Helper()
	f := mailFixture{d: d, tenantID: uuid.New(), venueID: uuid.New(),
		admin: "Owner." + strings.ToUpper(uuid.NewString()[:8]) + "@EM7B.example.test"}
	err := d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if _, e := tx.Exec(ctx,
			`INSERT INTO tenants (id, name, vat_number, business_type, structure, vat_verified)
			 VALUES ($1, $2, $3, 'restaurant', 'multi', $4)`,
			f.tenantID, name, "VAT-"+f.tenantID.String(), verified); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx,
			`INSERT INTO locations (id, tenant_id, name, static_ips, gps_lat, gps_lng)
			 VALUES ($1, $2, 'EM7B venue', '{203.0.113.0/24}', 35.899, 14.514)`,
			f.venueID, f.tenantID); e != nil {
			return e
		}
		_, e := tx.Exec(ctx,
			`INSERT INTO admin_users (id, tenant_id, full_name, email, password_hash, role, status)
			 VALUES ($1, $2, 'EM7B Owner', $3, $4, 'owner', 'active')`,
			// bcrypt's SHAPE (00018's CHECK) and nothing else: no password produces it.
			uuid.New(), f.tenantID, f.admin, "$2a$04$"+strings.Repeat("a", 53))
		return e
	})
	if err != nil {
		t.Fatalf("newMailFixture: %v", err)
	}
	return f
}

// person adds an invited employee with the given address (nil = none on file).
func (f mailFixture) person(t *testing.T, name string, address *string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if err := f.d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, invited_at, email)
			 VALUES ($1, $2, $3, $4, 'invited', now(), $5)`,
			id, f.tenantID, f.venueID, name, address)
		return e
	}); err != nil {
		t.Fatalf("person: %v", err)
	}
	return id
}

// seedInvites writes n invitation rows for employee, created at `at` (an SQL
// expression evaluated by the database, e.g. "now() - interval '2 hours'"). The hash
// is random 64-hex — the CHECK's shape; no code exists for it.
func (f mailFixture) seedInvites(t *testing.T, employee uuid.UUID, n int, at string) {
	t.Helper()
	if err := f.d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, fmt.Sprintf(
			`INSERT INTO employee_invites (tenant_id, employee_id, code_hash, created_at, expires_at)
			 SELECT $1, $2, md5(random()::text || g::text) || md5(random()::text || g::text), %s, now() + interval '7 days'
			 FROM generate_series(1, $3) g`, at),
			f.tenantID, employee, n)
		return e
	}); err != nil {
		t.Fatalf("seedInvites: %v", err)
	}
}

// count runs one counting query in the business's own context.
func (f mailFixture) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := f.d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, q, args...).Scan(&n)
	}); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func (f mailFixture) invitesFor(t *testing.T, employee uuid.UUID) int {
	t.Helper()
	return f.count(t, `SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND employee_id = $2`, f.tenantID, employee)
}

func (f mailFixture) spendable(t *testing.T, employee uuid.UUID) int {
	t.Helper()
	return f.count(t, `SELECT count(*) FROM employee_invites WHERE tenant_id = $1 AND employee_id = $2
	                   AND used_at IS NULL AND cancelled_at IS NULL AND now() < expires_at`, f.tenantID, employee)
}

func (f mailFixture) trail(t *testing.T, action string, employee uuid.UUID) []string {
	t.Helper()
	var out []string
	if err := f.d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		rows, e := tx.Query(ctx, `SELECT detail::text FROM audit_log WHERE tenant_id = $1 AND action = $2 AND target = $3 ORDER BY at`,
			f.tenantID, action, employee.String())
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var s string
			if e := rows.Scan(&s); e != nil {
				return e
			}
			out = append(out, s)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("trail: %v", err)
	}
	return out
}

func (f mailFixture) detailKeys(t *testing.T, action string, employee uuid.UUID) string {
	t.Helper()
	var keys []string
	if err := f.d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT array_agg(DISTINCT k ORDER BY k) FROM audit_log, jsonb_object_keys(detail) k
			  WHERE tenant_id = $1 AND action = $2 AND target = $3`,
			f.tenantID, action, employee.String()).Scan(&keys)
	}); err != nil {
		t.Fatalf("detailKeys: %v", err)
	}
	return strings.Join(keys, ",")
}

// fakeMailSink records what it was handed and answers as told. capped is its answer
// to the per-mailbox cap's question (M10 EM-7C), and asked the addresses it was asked
// about.
type fakeMailSink struct {
	mu     sync.Mutex
	got    []Delivery
	id     string
	err    error
	block  chan struct{} // if set, SendInvitation waits on it
	capped bool
	asked  []string
	scopes []uuid.UUID // the business each question was asked for
}

func (s *fakeMailSink) RecipientCapped(tenantID uuid.UUID, address string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, address)
	s.scopes = append(s.scopes, tenantID)
	return s.capped
}

func (s *fakeMailSink) SendInvitation(_ context.Context, d Delivery) (string, error) {
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.got = append(s.got, d)
	return s.id, s.err
}

func (s *fakeMailSink) calls() []Delivery {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Delivery(nil), s.got...)
}

func mailRoute(t *testing.T, d *db.DB, sink MailSink) *EmailChannel {
	t.Helper()
	rec, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	actor := uuid.New()
	ch, err := NewEmailChannel(sink, rec, &actor)
	if err != nil {
		t.Fatal(err)
	}
	return ch
}

func addr(label string) *string {
	s := label + "." + strings.ToUpper(uuid.NewString()[:8]) + "@EM7B.example.test"
	return &s
}

func ptrBool(b bool) *bool { return &b }

// TestEmailRouteDB_TheDeliveryCarriesTheRowsAddressAndTheVerification: the sink is
// handed the address byte for byte as stored, both names, and TenantVerified exactly
// when vat_verified IS TRUE — FALSE and NULL (never asked) are both "not verified".
// One successful press writes ONE invite.code_emailed row with exactly the five keys
// and no address, and NO invite.code_shown_to_manager row.
func TestEmailRouteDB_TheDeliveryCarriesTheRowsAddressAndTheVerification(t *testing.T) {
	m, d := testManager(t)
	for _, tc := range []struct {
		label    string
		verified *bool
		want     bool
		id       string
	}{
		{"verified", ptrBool(true), true, "relay-id-0001"},
		{"refused by VIES", ptrBool(false), false, "relay-id-0001"},
		{"never asked", nil, false, "relay-id-0001"},
		// EM-7B round 3: a relay that gave no id (or one internal/mail dropped for echoing
		// what it was sent) still writes the SAME five keys — "message_id" explicitly "".
		{"no relay id", ptrBool(true), true, ""},
	} {
		f := newMailFixture(t, d, "EM7B Test "+tc.label, tc.verified)
		a := addr("Maria")
		emp := f.person(t, "Maria Borg", a)
		sink := &fakeMailSink{id: tc.id}
		inv, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: emp}, mailRoute(t, d, sink))
		if err != nil {
			t.Fatalf("%s: %v", tc.label, err)
		}
		got := sink.calls()
		if len(got) != 1 {
			t.Fatalf("%s: the sink was called %d time(s), want 1", tc.label, len(got))
		}
		r := got[0].Recipient
		if r.Address != *a || r.EmployeeName != "Maria Borg" || r.TenantName != "EM7B Test "+tc.label {
			t.Errorf("%s: recipient %q / %q / %q, want the row's", tc.label, r.Address, r.EmployeeName, r.TenantName)
		}
		if r.TenantVerified != tc.want {
			t.Errorf("%s: TenantVerified = %v, want %v", tc.label, r.TenantVerified, tc.want)
		}
		if !strings.HasPrefix(got[0].ActivationURL, "http://localhost:8080/activate?code=") || got[0].Invite.ID != inv.ID {
			t.Errorf("%s: the delivery is not this invitation's link", tc.label)
		}
		rows := f.trail(t, ActionCodeEmailed, emp)
		if len(rows) != 1 {
			t.Fatalf("%s: %d invite.code_emailed row(s), want 1", tc.label, len(rows))
		}
		if keys := f.detailKeys(t, ActionCodeEmailed, emp); keys != "channel,employee_id,expires_at,invite_id,message_id" {
			t.Errorf("%s: code_emailed keys %q", tc.label, keys)
		}
		if strings.Contains(rows[0], "@") || !strings.Contains(rows[0], `"message_id": "`+tc.id+`"`) || !strings.Contains(rows[0], `"channel": "email"`) {
			t.Errorf("%s: code_emailed detail %s", tc.label, rows[0])
		}
		if n := len(f.trail(t, ActionCodeShownToManager, emp)); n != 0 {
			t.Errorf("%s: %d invite.code_shown_to_manager row(s) in the e-mail mode, want 0", tc.label, n)
		}
	}
}

// TestEmailRouteDB_EachAddressRefusalMintsNothing: no address, a non-ASCII address, an
// ASCII address the relay's rule refuses, and an administrator's address (stored in
// OTHER capitals — citext equality) each refuse BEFORE anything is kept: no new row,
// the person's earlier link still spendable (the transaction that retired it rolled
// back), the sink never called, ONE invite.email_refused row with the reason and no
// address. A fifth person with a good address is the control.
func TestEmailRouteDB_EachAddressRefusalMintsNothing(t *testing.T) {
	m, d := testManager(t)
	f := newMailFixture(t, d, "EM7B Test refusals", ptrBool(true))
	nonASCII := "Mária." + uuid.NewString()[:8] + "@EM7B.example.test"
	malformed := "two@@" + uuid.NewString()[:8] + ".example.test"
	adminOther := strings.ToLower(f.admin)
	if adminOther == f.admin {
		t.Fatal("PREMISE: the administrator's address must differ from its lower-case form")
	}
	for _, tc := range []struct {
		label   string
		address *string
		want    error
		reason  string
	}{
		{"no address", nil, ErrNoAddress, "no_address"},
		{"not ASCII", &nonASCII, ErrAddressNotASCII, "address_not_ascii"},
		{"relay refuses", &malformed, ErrAddressRefused, "address_not_deliverable"},
		{"an administrator's, other capitals", &adminOther, ErrAddressIsAdministrators, "address_is_an_administrators"},
	} {
		emp := f.person(t, "Refused "+tc.label, tc.address)
		// An earlier, spendable link minted in the panel mode: a refusal must not retire it.
		if _, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: emp}, &captureChannel{}); err != nil {
			t.Fatalf("%s: seeding an earlier link: %v", tc.label, err)
		}
		sink := &fakeMailSink{}
		_, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: emp}, mailRoute(t, d, sink))
		if !errors.Is(err, tc.want) {
			t.Fatalf("%s: %v, want %v", tc.label, err, tc.want)
		}
		if n := f.invitesFor(t, emp); n != 1 {
			t.Errorf("%s: %d invitation row(s), want the earlier one only", tc.label, n)
		}
		if n := f.spendable(t, emp); n != 1 {
			t.Errorf("%s: %d spendable link(s), want the earlier one still alive", tc.label, n)
		}
		if n := len(sink.calls()); n != 0 {
			t.Errorf("%s: the sink was called %d time(s)", tc.label, n)
		}
		rows := f.trail(t, ActionEmailRefused, emp)
		if len(rows) != 1 || !strings.Contains(rows[0], `"reason": "`+tc.reason+`"`) || strings.Contains(rows[0], "@") {
			t.Errorf("%s: invite.email_refused rows %v, want one with reason %q and no address", tc.label, rows, tc.reason)
		}
		if keys := f.detailKeys(t, ActionEmailRefused, emp); keys != "channel,employee_id,outcome,reason" {
			t.Errorf("%s: refusal keys %q", tc.label, keys)
		}
	}
	good := f.person(t, "Control", addr("Good"))
	sink := &fakeMailSink{}
	if _, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: good}, mailRoute(t, d, sink)); err != nil {
		t.Fatalf("CONTROL: a good address was refused: %v", err)
	}
	if len(sink.calls()) != 1 || f.invitesFor(t, good) != 1 {
		t.Error("CONTROL: a good address did not mint and send once")
	}
}

// TestEmailRouteDB_LimitsAreCountedFromTheRows: ADR 0022 §9 at its edges, counted from
// employee_invites. The business's 50th invitation in the hour is minted and its 51st
// refused; its 300th in the day minted and its 301st refused (299 seeded two hours ago
// — inside the day, outside the hour); a person's 3rd in the hour minted and 4th
// refused, with three rows from 61 minutes ago not counted. Each refusal mints
// nothing and writes one row with its reason.
func TestEmailRouteDB_LimitsAreCountedFromTheRows(t *testing.T) {
	m, d := testManager(t)
	issue := func(f mailFixture, emp uuid.UUID) error {
		_, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: emp}, mailRoute(t, d, &fakeMailSink{}))
		return err
	}

	hour := newMailFixture(t, d, "EM7B Test hour", nil)
	other := hour.person(t, "Other", addr("Other"))
	hour.seedInvites(t, other, TenantHourLimit-1, "now()")
	fiftieth, fiftyFirst := hour.person(t, "Fiftieth", addr("A")), hour.person(t, "FiftyFirst", addr("B"))
	if err := issue(hour, fiftieth); err != nil {
		t.Fatalf("the business's %dth invitation in the hour was refused: %v", TenantHourLimit, err)
	}
	if err := issue(hour, fiftyFirst); !errors.Is(err, ErrTenantHourLimit) {
		t.Fatalf("the business's %dth invitation in the hour: %v, want ErrTenantHourLimit", TenantHourLimit+1, err)
	}
	if hour.invitesFor(t, fiftyFirst) != 0 || len(hour.trail(t, ActionEmailRefused, fiftyFirst)) != 1 {
		t.Error("the refused hourly press minted something or left no refusal row")
	}

	day := newMailFixture(t, d, "EM7B Test day", nil)
	old := day.person(t, "Old", addr("Old"))
	day.seedInvites(t, old, TenantDayLimit-1, "now() - interval '2 hours'")
	threeHundredth, next := day.person(t, "ThreeHundredth", addr("C")), day.person(t, "Next", addr("D"))
	if err := issue(day, threeHundredth); err != nil {
		t.Fatalf("the business's %dth invitation in the day was refused: %v", TenantDayLimit, err)
	}
	if err := issue(day, next); !errors.Is(err, ErrTenantDayLimit) {
		t.Fatalf("the business's %dth invitation in the day: %v, want ErrTenantDayLimit", TenantDayLimit+1, err)
	}
	if day.invitesFor(t, next) != 0 {
		t.Error("the refused daily press minted a row")
	}

	person := newMailFixture(t, d, "EM7B Test person", nil)
	p := person.person(t, "Pressed", addr("P"))
	person.seedInvites(t, p, EmployeeHourLimit, "now() - interval '61 minutes'")
	for i := 1; i <= EmployeeHourLimit; i++ {
		if err := issue(person, p); err != nil {
			t.Fatalf("the person's invitation %d in the hour was refused: %v (rows from 61 minutes ago must not count)", i, err)
		}
	}
	if err := issue(person, p); !errors.Is(err, ErrEmployeeHourLimit) {
		t.Fatalf("the person's invitation %d in the hour: %v, want ErrEmployeeHourLimit", EmployeeHourLimit+1, err)
	}
	if n := person.invitesFor(t, p); n != 2*EmployeeHourLimit {
		t.Errorf("%d row(s) for the person, want %d seeded + %d minted", n, EmployeeHourLimit, EmployeeHourLimit)
	}
	rows := person.trail(t, ActionEmailRefused, p)
	if len(rows) != 1 || !strings.Contains(rows[0], `"reason": "person_hourly_limit"`) {
		t.Errorf("person limit refusal rows %v", rows)
	}
}

// TestEmailRouteDB_ConcurrentPressesNeverPassALimit: presses that arrive TOGETHER are
// counted one after another (LockTenantForInviteLimits). Per person: N simultaneous
// presses mint exactly EmployeeHourLimit. Per business: with TenantHourLimit−5
// already minted in the hour, N simultaneous presses for N different people mint
// exactly 5. Three rounds each, fresh people every round; run with -race.
func TestEmailRouteDB_ConcurrentPressesNeverPassALimit(t *testing.T) {
	m, d := testManager(t)
	const n = 12
	press := func(f mailFixture, emps []uuid.UUID) (ok int, limited int) {
		var wg sync.WaitGroup
		var mu sync.Mutex
		start := make(chan struct{})
		for _, emp := range emps {
			wg.Add(1)
			go func(emp uuid.UUID) {
				defer wg.Done()
				<-start
				_, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: emp}, mailRoute(t, d, &fakeMailSink{}))
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					ok++
				case errors.Is(err, ErrEmployeeHourLimit), errors.Is(err, ErrTenantHourLimit):
					limited++
				default:
					t.Errorf("a concurrent press failed otherwise: %v", err)
				}
			}(emp)
		}
		close(start)
		wg.Wait()
		return ok, limited
	}
	for round := 1; round <= 3; round++ {
		f := newMailFixture(t, d, fmt.Sprintf("EM7B Test race person %d", round), nil)
		emp := f.person(t, "Raced", addr("Raced"))
		same := make([]uuid.UUID, n)
		for i := range same {
			same[i] = emp
		}
		ok, limited := press(f, same)
		if ok != EmployeeHourLimit || limited != n-EmployeeHourLimit || f.invitesFor(t, emp) != EmployeeHourLimit {
			t.Errorf("round %d, one person: %d minted, %d limited, %d row(s); want %d, %d, %d",
				round, ok, limited, f.invitesFor(t, emp), EmployeeHourLimit, n-EmployeeHourLimit, EmployeeHourLimit)
		}

		g := newMailFixture(t, d, fmt.Sprintf("EM7B Test race business %d", round), nil)
		seed := g.person(t, "Seed", addr("Seed"))
		g.seedInvites(t, seed, TenantHourLimit-5, "now()")
		many := make([]uuid.UUID, n)
		for i := range many {
			many[i] = g.person(t, fmt.Sprintf("Raced %d", i), addr("R"))
		}
		ok, limited = press(g, many)
		total := g.count(t, `SELECT count(*) FROM employee_invites WHERE tenant_id = $1`, g.tenantID)
		if ok != 5 || limited != n-5 || total != TenantHourLimit {
			t.Errorf("round %d, one business: %d minted, %d limited, %d row(s) in all; want 5, %d, %d",
				round, ok, limited, total, n-5, TenantHourLimit)
		}
	}
}

// TestEmailRouteDB_TheAddressIsReadAfterTheMintInTheSameTransaction is EM-6's counted
// limit 1, closed and measured: an address change holds the person's row FOR UPDATE
// (as Staff.ChangeEmail's step 2 does) while a press is under way. The press's
// CreateInvite waits on that lock (its foreign-key check takes FOR KEY SHARE), so the
// address it reads AFTER the insert is the NEW one, committed by the change. Read
// before the insert, the same press would read the OLD address — the change has not
// committed — and mail a live code to it.
func TestEmailRouteDB_TheAddressIsReadAfterTheMintInTheSameTransaction(t *testing.T) {
	m, d := testManager(t)
	f := newMailFixture(t, d, "EM7B Test race address", nil)
	before, after := addr("Before"), addr("After")
	emp := f.person(t, "Moving", before)

	locked, release, changed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		changed <- d.WithTenant(context.Background(), f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			if _, e := tx.Exec(ctx, `SELECT id FROM employees WHERE tenant_id = $1 AND id = $2 FOR UPDATE`, f.tenantID, emp); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, `UPDATE employees SET email = $3 WHERE tenant_id = $1 AND id = $2`, f.tenantID, emp, *after); e != nil {
				return e
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	sink := &fakeMailSink{}
	done := make(chan error, 1)
	go func() {
		_, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: emp}, mailRoute(t, d, sink))
		done <- err
	}()
	select {
	case err := <-done:
		close(release)
		t.Fatalf("the press finished while the change held the row (%v): the mint did not wait on it", err)
	case <-time.After(400 * time.Millisecond):
	}
	close(release)
	if err := <-changed; err != nil {
		t.Fatalf("the change: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("the press: %v", err)
	}
	got := sink.calls()
	if len(got) != 1 || got[0].Recipient.Address != *after {
		var to string
		if len(got) == 1 {
			to = map[bool]string{true: "the OLD address", false: "another address"}[got[0].Recipient.Address == *before]
		}
		t.Fatalf("the press mailed %d message(s), to %s; want one, to the address the change committed", len(got), to)
	}
}

// TestEmailRouteDB_AFailedSendIsRecordedAndTheRowStands: the relay's *mail.SendError
// reaches the caller intact (errors.As) inside an error naming the invitation, ONE
// invite.undelivered row carries its class and reply code and no address, no
// code_emailed row is written, and the minted row stands (B12, counted limit 11 — the
// next press replaces it).
func TestEmailRouteDB_AFailedSendIsRecordedAndTheRowStands(t *testing.T) {
	m, d := testManager(t)
	f := newMailFixture(t, d, "EM7B Test failure", nil)
	emp := f.person(t, "Unlucky", addr("Unlucky"))
	sink := &fakeMailSink{err: &mail.SendError{Class: mail.ClassRejected, SMTPCode: 550}}
	inv, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: emp}, mailRoute(t, d, sink))
	var se *mail.SendError
	if !errors.As(err, &se) || se.SMTPCode != 550 || inv.ID == uuid.Nil || !strings.Contains(err.Error(), inv.ID.String()) {
		t.Fatalf("err %v (SendError %v), invite %s: want the relay's error inside one naming the invitation", err, se, inv.ID)
	}
	rows := f.trail(t, ActionUndelivered, emp)
	if len(rows) != 1 || !strings.Contains(rows[0], `"class": "rejected"`) || !strings.Contains(rows[0], `"smtp_code": 550`) || strings.Contains(rows[0], "@") {
		t.Errorf("invite.undelivered rows %v", rows)
	}
	if keys := f.detailKeys(t, ActionUndelivered, emp); keys != "channel,class,employee_id,expires_at,invite_id,smtp_code" {
		t.Errorf("undelivered keys %q", keys)
	}
	if n := len(f.trail(t, ActionCodeEmailed, emp)); n != 0 {
		t.Errorf("%d code_emailed row(s) for a failed send", n)
	}
	if f.invitesFor(t, emp) != 1 {
		t.Error("the minted row does not stand")
	}
}

// TestEmailRouteDB_TheOwnersFallbackRequiresNoAddress: IssueParams.OnlyWithoutAddress
// refuses somebody with an address (ErrHasAddress) and keeps nothing; for somebody
// without one it mints and shows through the panel channel, writing
// invite.code_shown_to_manager with channel manager_panel. An e-mail channel cannot
// be combined with it.
func TestEmailRouteDB_TheOwnersFallbackRequiresNoAddress(t *testing.T) {
	m, d := testManager(t)
	f := newMailFixture(t, d, "EM7B Test fallback", nil)
	with, without := f.person(t, "Has", addr("Has")), f.person(t, "HasNot", nil)
	rec, err := audit.New(d)
	if err != nil {
		t.Fatal(err)
	}
	sink := &recordingSink{}
	actor := uuid.New()
	panel, err := NewManagerVisibleChannel(sink, rec, &actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: with, OnlyWithoutAddress: true}, panel); !errors.Is(err, ErrHasAddress) {
		t.Fatalf("fallback for somebody with an address: %v, want ErrHasAddress", err)
	}
	if f.invitesFor(t, with) != 0 || sink.n != 0 {
		t.Error("the refused fallback kept a row or showed a link")
	}
	if _, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: without, OnlyWithoutAddress: true}, panel); err != nil {
		t.Fatalf("fallback for somebody without an address: %v", err)
	}
	rows := f.trail(t, ActionCodeShownToManager, without)
	if sink.n != 1 || len(rows) != 1 || !strings.Contains(rows[0], `"channel": "manager_panel"`) {
		t.Errorf("the fallback showed %d link(s) and wrote %v", sink.n, rows)
	}
	if _, err := m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: without, OnlyWithoutAddress: true},
		mailRoute(t, d, &fakeMailSink{})); err == nil {
		t.Error("an e-mail channel was accepted with OnlyWithoutAddress")
	}
}

// failingRecorder refuses every row.
type failingRecorder struct{}

func (failingRecorder) Record(context.Context, audit.Event) (uuid.UUID, error) {
	return uuid.Nil, errors.New("audit: insert refused")
}

// TestEmailRouteDB_ARefusalWhoseRowFailsIsStillTheRefusal (EM-7B round 3): when the
// invite.email_refused row cannot be written, IssueAndDeliver's error carries BOTH the
// refusal's sentinel (so the caller still says why nothing was sent) and
// ErrNotRecorded (so it can say the row is missing) — and nothing was minted.
func TestEmailRouteDB_ARefusalWhoseRowFailsIsStillTheRefusal(t *testing.T) {
	m, d := testManager(t)
	f := newMailFixture(t, d, "EM7B Test rowless refusal", nil)
	emp := f.person(t, "Rowless", nil)
	actor := uuid.New()
	ch, err := NewEmailChannel(&fakeMailSink{}, failingRecorder{}, &actor)
	if err != nil {
		t.Fatal(err)
	}
	_, err = m.IssueAndDeliver(context.Background(), IssueParams{TenantID: f.tenantID, EmployeeID: emp}, ch)
	if !errors.Is(err, ErrNoAddress) || !errors.Is(err, ErrNotRecorded) {
		t.Fatalf("err %v: want both ErrNoAddress and ErrNotRecorded", err)
	}
	if reason, ok := RefusalReason(err); !ok || reason != "no_address" {
		t.Errorf("RefusalReason(%v) = %q, %v; want no_address", err, reason, ok)
	}
	if n := f.invitesFor(t, emp); n != 0 {
		t.Errorf("%d invitation row(s), want 0", n)
	}
}

// TestEmailRoute_TheLimitsAreTheADRsNumbers binds the three constants to LITERALS — ADR
// 0022 §9's 50 an hour and 300 a day per business, 3 an hour per person — because the
// database tests above derive their edges from the constants and would follow a
// changed one (EM-9 round 2's lesson: a test comparing a limiter with its own constant
// verifies nothing). Changing a number is a decision; this is where it is made visible.
func TestEmailRoute_TheLimitsAreTheADRsNumbers(t *testing.T) {
	if TenantHourLimit != 50 || TenantDayLimit != 300 || EmployeeHourLimit != 3 {
		t.Errorf("limits %d/hour, %d/day per business and %d/hour per person; ADR 0022 §9 says 50, 300 and 3",
			TenantHourLimit, TenantDayLimit, EmployeeHourLimit)
	}
	if EmailSendGrace != 10*time.Second || EmailRecordGrace != 5*time.Second {
		t.Errorf("send grace %s, record grace %s; the EM-7B note says 10 s and 5 s", EmailSendGrace, EmailRecordGrace)
	}
}

// recordingSink is a LinkSink that counts.
type recordingSink struct{ n int }

func (s *recordingSink) ShowActivationLink(context.Context, Delivery) error { s.n++; return nil }
