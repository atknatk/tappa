package tenant

// staffemail_db_test.go -- M10 EM-6 against REAL Postgres: the address read, the
// change, the invitations it retires, the trail row, the tenant boundary and the two
// races the change's lock order exists for.
//
// 🔴 A FAKE CANNOT TEST ANY OF THIS. What is under test is what a mock would agree with
// unconditionally: that the retirement, the write and the trail row share ONE
// transaction (measured by breaking the trail), that RLS plus the explicit predicate
// keep another tenant's address unreadable and unwritable, that the unique index
// answers a taken address, and that the lock order holds against a concurrent issue
// and a concurrent activation.
//
// FIXTURES ARE NOT CLEANED UP, staff_db_test.go's reason: tappa_app holds no DELETE on
// employees, employee_invites or audit_log. Fresh random ids and addresses keep runs
// apart.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// randHash is a code_hash of the shape 00009's CHECK admits. It is the HMAC of no
// code: these invitations are never spent through the product.
func randHash(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

// uniqueAddress is an ASCII address no other run will use.
//
// 🔴 IT IS MIXED-CASE ON PURPOSE (EM-6 round 2). With all-lower-case fixtures a read
// that lower-cased the column (`lower(email)`) or a write that folded case passed
// every test here, because the folded value equalled the fixture (the audit's A18).
// Every comparison against a stored or read address below is byte equality.
func uniqueAddress(label string) string {
	return label + "." + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:12]) + "@EM6.example.test"
}

// seedInvite inserts one invitation for employee, committed, in its tenant's own
// context. ttl < 0 makes it already expired; used stamps it consumed.
func (f *staffFixture) seedInvite(t *testing.T, tenantID, employee uuid.UUID, ttl time.Duration, used bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		if e := tx.QueryRow(ctx,
			`INSERT INTO employee_invites (tenant_id, employee_id, code_hash, expires_at)
			 VALUES ($1, $2, $3, now() + make_interval(secs => $4)) RETURNING id`,
			tenantID, employee, randHash(t), ttl.Seconds()).Scan(&id); e != nil {
			return e
		}
		if used {
			_, e := tx.Exec(ctx,
				`UPDATE employee_invites SET used_at = now() WHERE tenant_id = $1 AND id = $2`,
				tenantID, id)
			return e
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed invite: %v", err)
	}
	return id
}

// inviteState reads one invitation's two stamps in its tenant's context.
func (f *staffFixture) inviteState(t *testing.T, tenantID, id uuid.UUID) (used, cancelled bool) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT used_at IS NOT NULL, cancelled_at IS NOT NULL
			   FROM employee_invites WHERE tenant_id = $1 AND id = $2`,
			tenantID, id).Scan(&used, &cancelled)
	})
	if err != nil {
		t.Fatalf("read invite: %v", err)
	}
	return used, cancelled
}

// storedEmail reads the address column byte-for-byte in the tenant's own context.
func (f *staffFixture) storedEmail(t *testing.T, tenantID, employee uuid.UUID) *string {
	t.Helper()
	var email *string
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT email::text FROM employees WHERE tenant_id = $1 AND id = $2`,
			tenantID, employee).Scan(&email)
	})
	if err != nil {
		t.Fatalf("read email: %v", err)
	}
	return email
}

// setStoredEmail writes an address directly, as the fixture's own setup.
func (f *staffFixture) setStoredEmail(t *testing.T, tenantID, employee uuid.UUID, email string) {
	t.Helper()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `UPDATE employees SET email = $3 WHERE tenant_id = $1 AND id = $2`,
			tenantID, employee, email)
		return e
	})
	if err != nil {
		t.Fatalf("set email: %v", err)
	}
}

// seedColleague adds a second employee to a tenant, at its first venue.
func (f *staffFixture) seedColleague(t *testing.T, tenantID, venue uuid.UUID, email *string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	err := f.data.WithTenant(context.Background(), tenantID, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx,
			`INSERT INTO employees (id, tenant_id, location_id, full_name, status, email)
			 VALUES ($1, $2, $3, 'Joe Camilleri', 'invited', $4)`,
			id, tenantID, venue, email)
		return e
	})
	if err != nil {
		t.Fatalf("seed colleague: %v", err)
	}
	return id
}

func strp(s string) *string { return &s }

// show prints a nullable address for a failure message.
func show(p *string) string {
	if p == nil {
		return "NULL"
	}
	return fmt.Sprintf("%q", *p)
}

// TestStaffEmailDB_AChangeRetiresTheLinksInTheSameTransaction is ADR 0022 §7's
// "aynı transaction": a written change retires EXACTLY the employee's spendable
// invitations (not an expired one, not a used one, not a colleague's), stores the
// trimmed address, and writes ONE trail row whose detail is exactly
// had_email/has_email/retired_invitations and carries no address.
//
// THE FAILING-TRAIL ARM COMES FIRST and is the point: with a trail that refuses, the
// address is unchanged AND the two links are still spendable — the retirement rolled
// back with the write. A version that retired in a separate transaction passes the
// second arm and fails this one.
func TestStaffEmailDB_AChangeRetiresTheLinksInTheSameTransaction(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	pendingA := f.seedInvite(t, f.tenantID, f.employeeID, 24*time.Hour, false)
	pendingB := f.seedInvite(t, f.tenantID, f.employeeID, 48*time.Hour, false)
	expired := f.seedInvite(t, f.tenantID, f.employeeID, -time.Hour, false)
	spent := f.seedInvite(t, f.tenantID, f.employeeID, 24*time.Hour, true)
	colleague := f.seedColleague(t, f.tenantID, f.locationID, nil)
	theirs := f.seedInvite(t, f.tenantID, colleague, 24*time.Hour, false)
	address := uniqueAddress("maria")

	broken, err := NewStaff(f.data, brokenTrail{err: errors.New("audit is down")}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	if _, err := broken.ChangeEmail(ctx, EmailCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID, Email: address,
	}); err == nil {
		t.Fatal("a failing trail was reported as success")
	}
	if got := f.storedEmail(t, f.tenantID, f.employeeID); got != nil {
		t.Fatalf("address = %q after a FAILED change; the write must roll back with the trail", *got)
	}
	for _, id := range []uuid.UUID{pendingA, pendingB} {
		if _, cancelled := f.inviteState(t, f.tenantID, id); cancelled {
			t.Fatal("a link was retired by a change whose trail row failed; the retirement must " +
				"share the transaction")
		}
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeEmailChanged, f.employeeID); n != 0 {
		t.Fatalf("%d trail row(s) after a failed change", n)
	}

	out, err := f.staff.ChangeEmail(ctx, EmailCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID,
		Email: "  " + address + "\t",
	})
	if err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	if out.Retired != 2 || out.HadEmail || !out.HasEmail {
		t.Errorf("result = %+v, want Retired 2, HadEmail false, HasEmail true", out)
	}
	if got := f.storedEmail(t, f.tenantID, f.employeeID); got == nil || *got != address {
		t.Errorf("stored address = %s, want the trimmed %q byte for byte", show(got), address)
	}
	for _, c := range []struct {
		name                    string
		id                      uuid.UUID
		wantUsed, wantCancelled bool
	}{
		{"pending A", pendingA, false, true},
		{"pending B", pendingB, false, true},
		{"expired (already unspendable, expires_at says why)", expired, false, false},
		{"spent (history, never rewritten)", spent, true, false},
		{"a colleague's", theirs, false, false},
	} {
		used, cancelled := f.inviteState(t, f.tenantID, c.id)
		if used != c.wantUsed || cancelled != c.wantCancelled {
			t.Errorf("%s: used=%v cancelled=%v, want used=%v cancelled=%v",
				c.name, used, cancelled, c.wantUsed, c.wantCancelled)
		}
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeEmailChanged, f.employeeID); n != 1 {
		t.Fatalf("%d trail row(s), want exactly 1", n)
	}
	detail, actor := f.auditDetail(t, f.tenantID, ActionEmployeeEmailChanged, f.employeeID)
	if actor == nil || *actor != f.actorID {
		t.Errorf("trail actor = %v, want %s", actor, f.actorID)
	}
	var keys []string
	if err := f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT array_agg(k ORDER BY k) FROM audit_log, jsonb_object_keys(detail) k
			  WHERE tenant_id = $1 AND target = $2 AND action = $3`,
			f.tenantID, f.employeeID.String(), ActionEmployeeEmailChanged).Scan(&keys)
	}); err != nil {
		t.Fatalf("read trail keys: %v", err)
	}
	if strings.Join(keys, ",") != "had_email,has_email,retired_invitations" {
		t.Errorf("trail detail keys = %v, want exactly had_email, has_email, retired_invitations", keys)
	}
	if !strings.Contains(detail, `"retired_invitations": 2`) && !strings.Contains(detail, `"retired_invitations":2`) {
		t.Errorf("trail detail %s does not count the two retired links", detail)
	}
	local := address[:strings.Index(address, "@")]
	if strings.Contains(strings.ToLower(detail), strings.ToLower(local)) || strings.Contains(detail, "@") {
		t.Errorf("the trail row carries the address: %s", detail)
	}
}

// TestStaffEmailDB_ARefusedChangeRetiresNothing: each refusal leaves the address, the
// spendable link and the trail exactly as they were — including the two refusals that
// happen INSIDE the transaction after its first retirement ran (same address, taken
// address), which is the measurement that the rollback really undoes step 1.
func TestStaffEmailDB_ARefusedChangeRetiresNothing(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	mine := uniqueAddress("maria")
	f.setStoredEmail(t, f.tenantID, f.employeeID, mine)
	taken := uniqueAddress("joe")
	f.seedColleague(t, f.tenantID, f.locationID, strp(taken))
	link := f.seedInvite(t, f.tenantID, f.employeeID, 24*time.Hour, false)

	for _, c := range []struct {
		name  string
		email string
		want  error
	}{
		{"the same address", mine, ErrSameEmail},
		{"the same address with surrounding space", " " + mine + " ", ErrSameEmail},
		// citext: capitals do not make an address someone else's to take.
		{"a colleague's address in other capitals", strings.ToUpper(taken), ErrEmailTaken},
		{"an address the send rule refuses", "Maria <" + mine + ">", ErrEmployeeEmail},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := f.staff.ChangeEmail(ctx, EmailCommand{
				TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID, Email: c.email,
			})
			if !errors.Is(err, c.want) {
				t.Fatalf("ChangeEmail(%q) = %v, want %v", c.email, err, c.want)
			}
			if got := f.storedEmail(t, f.tenantID, f.employeeID); got == nil || *got != mine {
				t.Errorf("the address moved to %s on a refused change", show(got))
			}
			if _, cancelled := f.inviteState(t, f.tenantID, link); cancelled {
				t.Error("a refused change retired the employee's link")
			}
			if n := f.auditRows(t, f.tenantID, ActionEmployeeEmailChanged, f.employeeID); n != 0 {
				t.Errorf("%d trail row(s) for a refused change", n)
			}
		})
	}
}

// TestStaffEmailDB_ACapitalsOnlyChangeIsAWrite is decision 4 (ADR 0022's EM-6 note):
// "the same address" is BYTE equality, not the column's case-insensitive one. Changing
// only the capitals of the address on file is a write — the new spelling is stored,
// the person's spendable link is retired, and one trail row is added. (EM-6 round 2:
// with the comparison turned into strings.EqualFold the suite stayed green — A11.)
func TestStaffEmailDB_ACapitalsOnlyChangeIsAWrite(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	onFile := uniqueAddress("Maria")
	f.setStoredEmail(t, f.tenantID, f.employeeID, onFile)
	link := f.seedInvite(t, f.tenantID, f.employeeID, 24*time.Hour, false)
	recapped := strings.ToLower(onFile)
	if recapped == onFile {
		t.Fatalf("fixture: %q has no capitals to change", onFile)
	}

	out, err := f.staff.ChangeEmail(ctx, EmailCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID, Email: recapped,
	})
	if err != nil {
		t.Fatalf("a capitals-only change = %v, want a write", err)
	}
	if got := f.storedEmail(t, f.tenantID, f.employeeID); got == nil || *got != recapped {
		t.Errorf("stored %s, want the new spelling %q", show(got), recapped)
	}
	if _, cancelled := f.inviteState(t, f.tenantID, link); !cancelled || out.Retired != 1 {
		t.Errorf("link cancelled=%v, Retired=%d; a written change retires the spendable link", cancelled, out.Retired)
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeEmailChanged, f.employeeID); n != 1 {
		t.Errorf("%d trail row(s), want 1", n)
	}
}

// TestStaffEmailDB_TheRuleIsTheSendRule: what the change stores is what internal/mail
// would send to. Refused (nothing written): a non-ASCII letter, a Cyrillic look-alike,
// a display name, CR LF with an injected header, an encoded word, NUL, 255 bytes.
// Accepted and stored byte for byte after trimming, capitals kept: plus-addressing,
// mixed case, the 254-byte edge. And "" clears the address to NULL — and two people
// with no address are legal (the partial unique index).
func TestStaffEmailDB_TheRuleIsTheSendRule(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	at := "@" + strings.ReplaceAll(uuid.NewString(), "-", "") + ".example.test"
	edge := strings.Repeat("a", 254-len(at)) + at

	for _, bad := range []string{
		"\xc4\xa7" + at,                  // U+0127
		"\xd0\xb0li" + at,                // U+0430 Cyrillic a
		"Maria Borg <maria" + at + ">",   // display name
		"maria" + at + "\r\nBcc: x" + at, // header injection
		"=?utf-8?q?m?=" + at,             // encoded word
		"ma\x00ria" + at,                 // NUL
		"a" + edge,                       // 255 bytes
		"no-at-sign.example.test",
	} {
		_, err := f.staff.ChangeEmail(ctx, EmailCommand{
			TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID, Email: bad,
		})
		if !errors.Is(err, ErrEmployeeEmail) {
			t.Errorf("ChangeEmail(%q) = %v, want ErrEmployeeEmail", bad, err)
		}
	}
	if got := f.storedEmail(t, f.tenantID, f.employeeID); got != nil {
		t.Fatalf("a refused address was stored: %q", *got)
	}

	for _, good := range []string{"Maria.Borg+payroll" + at, edge} {
		if _, err := f.staff.ChangeEmail(ctx, EmailCommand{
			TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID, Email: " " + good + " ",
		}); err != nil {
			t.Fatalf("ChangeEmail(%q): %v", good, err)
		}
		if got := f.storedEmail(t, f.tenantID, f.employeeID); got == nil || *got != good {
			t.Errorf("stored %s, want %q byte for byte (trimmed, capitals kept)", show(got), good)
		}
		// AND THE CARD'S READ RETURNS IT BYTE FOR BYTE (EM-6 round 2, A18): the read
		// goes through GetEmployeeEmail, the oracle above through raw SQL.
		if read, err := f.staff.Email(ctx, f.tenantID, f.employeeID); err != nil || read != good {
			t.Errorf("Staff.Email = (%q, %v), want %q byte for byte", read, err, good)
		}
	}

	if _, err := f.staff.ChangeEmail(ctx, EmailCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID, Email: "   ",
	}); err != nil {
		t.Fatalf("clearing the address: %v", err)
	}
	if got := f.storedEmail(t, f.tenantID, f.employeeID); got != nil {
		t.Errorf("a cleared address is %q, want NULL", *got)
	}
	// A SECOND PERSON WITH NO ADDRESS IS LEGAL — NULL, not '', reached the column.
	other := f.seedColleague(t, f.tenantID, f.locationID, strp(uniqueAddress("joe")))
	if _, err := f.staff.ChangeEmail(ctx, EmailCommand{
		TenantID: f.tenantID, EmployeeID: other, ActorID: f.actorID, Email: "",
	}); err != nil {
		t.Errorf("clearing a second person's address: %v (an empty string reached the "+
			"partial unique index)", err)
	}
	// AND CLEARING WHAT IS ALREADY CLEAR IS "the same".
	if _, err := f.staff.ChangeEmail(ctx, EmailCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID, Email: "",
	}); !errors.Is(err, ErrSameEmail) {
		t.Errorf("clearing an absent address = %v, want ErrSameEmail", err)
	}
}

// TestStaffEmailDB_ATenantCannotReadOrChangeAnotherTenantsAddress is §4.5 with two
// real tenants: through A's session, B's employee's address cannot be read (no row)
// and cannot be changed (no row) — B's address, B's link and both trails unchanged.
// The positive controls: B reads its own address, and A may hold the SAME address for
// its own employee (uniqueness is per business, and must answer nothing about B).
func TestStaffEmailDB_ATenantCannotReadOrChangeAnotherTenantsAddress(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	theirs := uniqueAddress("foreign")
	f.setStoredEmail(t, f.foreignTenant, f.foreignEmployee, theirs)
	theirLink := f.seedInvite(t, f.foreignTenant, f.foreignEmployee, 24*time.Hour, false)

	if got, err := f.staff.Email(ctx, f.tenantID, f.foreignEmployee); !errors.Is(err, ErrUnknownEmployee) || got != "" {
		t.Errorf("A read B's employee's address: (%q, %v), want (\"\", ErrUnknownEmployee)", got, err)
	}
	if _, err := f.staff.ChangeEmail(ctx, EmailCommand{
		TenantID: f.tenantID, EmployeeID: f.foreignEmployee, ActorID: f.actorID, Email: uniqueAddress("hijack"),
	}); !errors.Is(err, ErrUnknownEmployee) {
		t.Fatalf("A changing B's employee's address = %v, want ErrUnknownEmployee", err)
	}
	if got := f.storedEmail(t, f.foreignTenant, f.foreignEmployee); got == nil || *got != theirs {
		t.Errorf("B's address is now %s", show(got))
	}
	if _, cancelled := f.inviteState(t, f.foreignTenant, theirLink); cancelled {
		t.Error("A's attempt retired B's link")
	}
	for _, tenantID := range []uuid.UUID{f.tenantID, f.foreignTenant} {
		if n := f.auditRows(t, tenantID, ActionEmployeeEmailChanged, f.foreignEmployee); n != 0 {
			t.Errorf("%d trail row(s) about B's employee in tenant %s", n, tenantID)
		}
	}

	if got, err := f.staff.Email(ctx, f.foreignTenant, f.foreignEmployee); err != nil || got != theirs {
		t.Errorf("control: B reads (%q, %v), want its own address", got, err)
	}
	if _, err := f.staff.ChangeEmail(ctx, EmailCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID, Email: theirs,
	}); err != nil {
		t.Errorf("control: A storing an address B also holds = %v; uniqueness is per business "+
			"and must not answer anything about another one", err)
	}
}

// TestStaffEmailDB_TwoOwnersSavingTheSameAddressWriteOnce: eight changes to the same
// new address at once produce ONE write and ONE trail row; the rest are ErrSameEmail.
// It holds because the comparison is against the value LockEmployeeForEmailChange
// locked, not a value read before the lock.
//
// ⚠️ ROUNDS, BECAUSE THE BROKEN SHAPE LOSES ONLY SOMETIMES. Measured with the lock
// removed from the read: one round of eight racers wrote 5 times once in five runs and
// exactly once in the other four — the racers' transactions mostly do not overlap. So
// the test plays many rounds, each on a fresh address, and every round must write
// exactly once; the correct code is deterministic in every round.
func TestStaffEmailDB_TwoOwnersSavingTheSameAddressWriteOnce(t *testing.T) {
	f := newStaffFixture(t)
	const rounds, racers = 25, 8
	for round := 1; round <= rounds; round++ {
		address := uniqueAddress("maria")
		results := make([]error, racers)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < racers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				_, results[i] = f.staff.ChangeEmail(context.Background(), EmailCommand{
					TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID, Email: address,
				})
			}(i)
		}
		close(start)
		wg.Wait()
		wrote := 0
		for i, err := range results {
			switch {
			case err == nil:
				wrote++
			case errors.Is(err, ErrSameEmail):
			default:
				t.Fatalf("round %d racer %d: %v", round, i, err)
			}
		}
		if wrote != 1 {
			t.Fatalf("round %d: %d of %d identical concurrent changes wrote, want exactly 1", round, wrote, racers)
		}
	}
	if n := f.auditRows(t, f.tenantID, ActionEmployeeEmailChanged, f.employeeID); n != rounds {
		t.Errorf("%d trail row(s) after %d rounds, want one per round", n, rounds)
	}
}

// TestStaffEmailDB_AnInvitationBeingIssuedIsRetiredByTheChange is the race step 2 and
// step 3 exist for. A second transaction issues an invitation the way
// invite.Manager.IssueAndDeliver does — retire the siblings, then CreateInvite's
// INSERT, the two statements in that order — and holds its transaction OPEN. The
// change then starts:
//
//	it must NOT finish while the issuing transaction is open (step 2's FOR UPDATE
//	  waits on the FOR KEY SHARE the insert's foreign-key check holds); and
//	once the issuer commits, the new invitation is RETIRED (step 3 sees it).
//
// Without the lock the change finishes at once and the new invitation survives it — a
// live link, issued against the address before the change, outliving the change.
// ⚠️ THE ISSUER IS AN IMITATION of IssueAndDeliver's transaction, not the function:
// that function commits before it returns, so nothing outside it can hold its
// transaction open. The statements and their order are the function's.
func TestStaffEmailDB_AnInvitationBeingIssuedIsRetiredByTheChange(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	inserted, release := make(chan uuid.UUID), make(chan struct{})
	issuerDone := make(chan error, 1)
	go func() {
		issuerDone <- f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			if _, e := tx.Exec(ctx,
				`UPDATE employee_invites SET cancelled_at = now()
				  WHERE tenant_id = $1 AND employee_id = $2
				    AND used_at IS NULL AND cancelled_at IS NULL AND now() < expires_at`,
				f.tenantID, f.employeeID); e != nil {
				return e
			}
			var id uuid.UUID
			if e := tx.QueryRow(ctx,
				`INSERT INTO employee_invites (tenant_id, employee_id, code_hash, expires_at)
				 VALUES ($1, $2, $3, now() + interval '7 days') RETURNING id`,
				f.tenantID, f.employeeID, randHash(t)).Scan(&id); e != nil {
				return e
			}
			inserted <- id
			<-release
			return nil
		})
	}()
	issued := <-inserted

	changeDone := make(chan error, 1)
	go func() {
		_, err := f.staff.ChangeEmail(ctx, EmailCommand{
			TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID, Email: uniqueAddress("maria"),
		})
		changeDone <- err
	}()
	select {
	case err := <-changeDone:
		close(release)
		<-issuerDone
		t.Fatalf("the change finished (%v) while an invitation was being inserted; it must wait "+
			"for that transaction (FOR UPDATE against the insert's FOR KEY SHARE)", err)
	case <-time.After(400 * time.Millisecond):
	}
	close(release)
	if err := <-issuerDone; err != nil {
		t.Fatalf("issuer: %v", err)
	}
	if err := <-changeDone; err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	if _, cancelled := f.inviteState(t, f.tenantID, issued); !cancelled {
		t.Error("the invitation issued alongside the change survived it: a live link outlives " +
			"the address it was issued against")
	}
}

// TestStaffEmailDB_AnActivationInFlightDoesNotDeadlockTheChange is the race step 1
// exists for. ConsumeInviteAndActivate locks the INVITATION first (its CTE) and the
// EMPLOYEE second (its outer UPDATE); the imitation below holds the invitation's lock,
// lets the change start, then updates the employee. With the change locking the
// invitations first (step 1) the two queue up and BOTH succeed: the activation spends
// the link, and the change retires nothing because the link is spent. A change that
// took the employee lock first would hold what the activation needs while waiting for
// what the activation holds — 40P01, and one of the two aborted.
//
// ⚠️ THE ACTIVATION IS AN IMITATION: the real statement is one statement, which cannot
// be paused between its two locks. The imitation takes the same two locks in the same
// order, in two statements.
func TestStaffEmailDB_AnActivationInFlightDoesNotDeadlockTheChange(t *testing.T) {
	f := newStaffFixture(t)
	ctx := context.Background()
	link := f.seedInvite(t, f.tenantID, f.employeeID, 24*time.Hour, false)
	locked, release := make(chan struct{}), make(chan struct{})
	activationDone := make(chan error, 1)
	go func() {
		activationDone <- f.data.WithTenant(ctx, f.tenantID, func(ctx context.Context, tx pgx.Tx) error {
			if _, e := tx.Exec(ctx,
				`UPDATE employee_invites SET used_at = now()
				  WHERE tenant_id = $1 AND id = $2 AND used_at IS NULL AND cancelled_at IS NULL`,
				f.tenantID, link); e != nil {
				return e
			}
			close(locked)
			<-release
			_, e := tx.Exec(ctx,
				`UPDATE employees SET status = 'active', activated_at = COALESCE(activated_at, now())
				  WHERE tenant_id = $1 AND id = $2 AND status IN ('invited', 'active')`,
				f.tenantID, f.employeeID)
			return e
		})
	}()
	<-locked

	changeDone := make(chan error, 1)
	go func() {
		_, err := f.staff.ChangeEmail(ctx, EmailCommand{
			TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID, Email: uniqueAddress("maria"),
		})
		changeDone <- err
	}()
	// Long enough for the change to reach whatever it waits on; its own statements take
	// milliseconds.
	time.Sleep(300 * time.Millisecond)
	close(release)
	if err := <-activationDone; err != nil {
		t.Errorf("the activation failed: %v (40P01 is the lock-order deadlock)", err)
	}
	if err := <-changeDone; err != nil {
		t.Errorf("the change failed: %v (40P01 is the lock-order deadlock)", err)
	}
	if used, cancelled := f.inviteState(t, f.tenantID, link); !used || cancelled {
		t.Errorf("the link is used=%v cancelled=%v; the activation queued first, so it is spent "+
			"and the change had nothing to retire", used, cancelled)
	}
}

// TestStaffEmailDB_TheDomainNeverLogsTheAddress: the change's log line says THAT
// there is an address, never which — neither the new one nor the one it replaced
// (TestStaffDB_TheDomainLogsNeitherTheNameNorTheEmail's rule, for the second writer of
// the column). The positive control is the line itself.
func TestStaffEmailDB_TheDomainNeverLogsTheAddress(t *testing.T) {
	f := newStaffFixture(t)
	var logged strings.Builder
	staff, err := NewStaff(f.data, f.trail, slog.New(slog.NewTextHandler(&logged,
		&slog.HandlerOptions{Level: slog.LevelDebug})))
	if err != nil {
		t.Fatalf("NewStaff: %v", err)
	}
	old, next := uniqueAddress("zzold"), uniqueAddress("zznew")
	f.setStoredEmail(t, f.tenantID, f.employeeID, old)
	if _, err := staff.ChangeEmail(context.Background(), EmailCommand{
		TenantID: f.tenantID, EmployeeID: f.employeeID, ActorID: f.actorID, Email: next,
	}); err != nil {
		t.Fatalf("ChangeEmail: %v", err)
	}
	written := logged.String()
	if !strings.Contains(written, "employee email changed") {
		t.Fatalf("the domain logged nothing for a change, so the absences below prove nothing:\n%s", written)
	}
	for _, a := range []string{old, next} {
		if strings.Contains(strings.ToLower(written), strings.ToLower(a[:strings.Index(a, "@")])) {
			t.Errorf("the process log carries an address:\n%s", written)
		}
	}
}
