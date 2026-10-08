package operator_test

// op16_db_test.go -- M10 OP-16 phase B end to end against PostgreSQL: the operator signs in
// through the surface, opens a tenant's VAT screen (op_begin_read + op_read_tenant_vat, 00034),
// asks VIES -- a fake VIES client, never the real register -- and the answer is recorded
// through op_record_vat_check, on newLegalE2E's committed rig (op10_db_test.go: one owner
// connection switched to tappa_operator, each statement its own committed transaction; the
// write is bound to a COMMITTED read of the same session, 00034 §7 (b2), which this flow makes
// by construction). The customer's own account read (internal/domain/tenant's
// Accounts.Settings, on tappa_app's pool) then reads the new verdict.
//
// WHAT ONE RUN LEAVES BEHIND. TestE2E_VATRecheckRecordsVIESAnswerAndTheTenantReadsIt COMMITS a
// fixture tenant -- the operator's read and write reach committed rows only, through the
// operator's own connection -- on tappa_app's pool, in the tenant's own context: 1 tenant
// ('FAKE op16b …', a made-up IE-shaped VAT number, never a real business's), and the two
// 'tenant.vat_rechecked' rows the run's two recorded answers write into that tenant's own
// audit_log (append-only, 00005). Nothing else of any tenant is written: the run reads and
// writes only its own fixture tenant. newLegalE2E leaves, by construction, one operator account
// (op10b-…, disabled at cleanup), its sessions (revoked at cleanup) and its operator_audit_log
// rows -- login, password_ok, and the run's own: five 'read' rows naming the fixture tenant
// (scope tenant_vat) and one naming its overview (tenant_detail), and two 'tenant_vat_checked'
// rows; read tickets are deleted at cleanup. The class is the backlog's T81.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/audit"
	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
	"github.com/atknatk/tappa/internal/domain/tenant"
	"github.com/atknatk/tappa/internal/handler/operator"
)

// liveVIES is operator.VATChecker for the committed rig: it answers what the test sets, records
// the numbers it is asked about, and runs probe while it is being asked -- the test's
// measurement of what the database holds at that moment (the card's T2).
type liveVIES struct {
	mu       sync.Mutex
	answer   operator.VATAnswer
	asked    []string
	probe    func() string
	findings []string
}

func (v *liveVIES) CheckVAT(_ context.Context, number string) operator.VATAnswer {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.asked = append(v.asked, number)
	if v.probe != nil {
		if f := v.probe(); f != "" {
			v.findings = append(v.findings, f)
		}
	}
	return v.answer
}

func (v *liveVIES) set(a operator.VATAnswer) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.answer = a
}

// vatFixture is one committed tenant, never asked about its VAT number, and the customer's own
// account reader over tappa_app's pool.
type vatFixture struct {
	id       uuid.UUID
	name     string
	number   string
	accounts *tenant.Accounts
}

// newVATFixture commits the tenant (the file header says what it leaves). Its VAT number is
// IE-shaped and made for the run -- a digit, a letter, five digits, a letter, from crypto/rand
// -- so it is in the format VIES takes and (tenants.vat_number is UNIQUE across every tenant)
// it is taken again on the rare collision.
func newVATFixture(t *testing.T, l *legalE2E) *vatFixture {
	t.Helper()
	appDSN := os.Getenv("DATABASE_URL")
	app, err := db.New(l.ctx, &config.Config{DatabaseURL: appDSN})
	if err != nil {
		t.Fatalf("tappa_app's pool: %v", err)
	}
	t.Cleanup(app.Close)
	trail, err := audit.New(app)
	if err != nil {
		t.Fatal(err)
	}
	f := &vatFixture{id: uuid.New()}
	f.name = "FAKE op16b E2E " + f.id.String()[:8]
	if f.accounts, err = tenant.NewAccounts(app, trail, nil); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; ; attempt++ {
		b := randBytes(t, 5)
		f.number = fmt.Sprintf("IE%d%c%05d%c", b[0]%10, 'A'+rune(b[1]%26), (int(b[2])<<8|int(b[3]))%100000, 'A'+rune(b[4]%26))
		if l.ownerInt(`SELECT count(*)::int FROM tenants WHERE vat_number = $1`, f.number) != 0 {
			if attempt == 5 {
				t.Fatal("PREMISE: six made-up numbers were all taken")
			}
			continue
		}
		break
	}
	err = app.WithTenant(l.ctx, f.id, func(ctx context.Context, tx pgx.Tx) error {
		_, e := tx.Exec(ctx, `INSERT INTO tenants (id, name, vat_number, business_type, structure, timezone)
		                      VALUES ($1, $2, $3, 'restaurant', 'single', 'Europe/Malta')`, f.id, f.name, f.number)
		return e
	})
	if err != nil {
		t.Fatalf("commit the VAT fixture: %v", err)
	}
	return f
}

// vatRow is a 'tenant.vat_rechecked' row's detail, read back.
type vatRow struct {
	ActorKind string `json:"actor_kind"`
	Before    struct {
		Verified  *bool   `json:"verified"`
		CheckedAt *string `json:"checked_at"`
	} `json:"before"`
	After struct {
		Verified  *bool   `json:"verified"`
		CheckedAt *string `json:"checked_at"`
	} `json:"after"`
}

// TestE2E_VATRecheckRecordsVIESAnswerAndTheTenantReadsIt is OP-16's end-to-end acceptance on
// PostgreSQL (the file header says what it commits):
//
//  1. the tenant's overview links its VAT screen; the screen (one committed 'read' row, scope
//     tenant_vat, the tenant named) shows the number and "Not checked";
//  2. VIES does not answer: 503 with the screen and its sentence; one more 'read' row and
//     NOTHING written -- no 'tenant_vat_checked' row, no 'tenant.vat_rechecked' row, the
//     tenant's two columns still NULL, the customer's account read still never-checked;
//  3. VIES confirms: 303 to the screen; one 'tenant_vat_checked' row (the session, the
//     operator, the tenant, detail {}); one 'tenant.vat_rechecked' row in the tenant's own
//     audit_log -- the operator as actor, the tenant as target, detail exactly
//     {actor_kind: operator, before: {null, null}, after: {true, the stamp}}, its `at` the stamp;
//     the tenant's vat_verified true and vat_checked_at the stamp (the database's wall clock,
//     between the request's start and end); the customer's account read is valid with that time;
//     the screen the redirect lands on says "Confirmed" and the stamp's minute (UTC);
//  4. VIES does not know the number: 303; a second operator row, a second tenant row whose
//     before is the first's after, the verdict false; the account read is invalid;
//  5. VIES was asked three times, each about the tenant's own number, and while it was asked the
//     operator's connection held no transaction (pgconn's TxStatus idle) and no store call was in
//     flight (liveStore's lock free) -- the card's T2, measured on the real connection.
func TestE2E_VATRecheckRecordsVIESAnswerAndTheTenantReadsIt(t *testing.T) {
	l := newLegalE2E(t)
	fx := newVATFixture(t, l)
	l.vies.probe = func() string {
		if !l.live.mu.TryLock() {
			return "a store call was in flight on the operator connection while VIES was asked"
		}
		defer l.live.mu.Unlock()
		if st := l.op.PgConn().TxStatus(); st != 'I' {
			return fmt.Sprintf("the operator connection's transaction status was %q while VIES was asked", st)
		}
		return ""
	}
	sess := l.signIn(l.f)
	path := vatPathOf(fx.id.String())
	reads := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'read' AND actor_admin_id = $1
		                   AND target_scope = 'tenant_vat' AND target_tenant_id = $2`, l.f.id, fx.id)
	}
	checks := func() int {
		return l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'tenant_vat_checked' AND target_tenant_id = $1`, fx.id)
	}
	tenantRows := func() []vatRow {
		t.Helper()
		rows, err := l.owner.Query(l.ctx, `SELECT detail::text, actor_id, target, at FROM audit_log
		                                   WHERE tenant_id = $1 AND action = 'tenant.vat_rechecked' ORDER BY at, id`, fx.id)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []vatRow
		for rows.Next() {
			var detail, target string
			var actor uuid.UUID
			var at time.Time
			if err := rows.Scan(&detail, &actor, &target, &at); err != nil {
				t.Fatal(err)
			}
			var r vatRow
			if err := json.Unmarshal([]byte(detail), &r); err != nil {
				t.Fatalf("a tenant row's detail is not JSON: %v", err)
			}
			if actor != l.f.id || target != fx.id.String() {
				t.Errorf("a tenant row names actor %s and target %q, want the operator and the tenant", actor, target)
			}
			if r.After.CheckedAt == nil || *r.After.CheckedAt != at.UTC().Format("2006-01-02T15:04:05.000000Z") {
				t.Errorf("a tenant row's after time is not its own `at` (%v)", at)
			}
			var keys map[string]json.RawMessage
			if err := json.Unmarshal([]byte(detail), &keys); err != nil || len(keys) != 3 || strings.Contains(detail, fx.number) {
				t.Errorf("a tenant row's detail carries more than actor_kind, before and after, or the number")
			}
			out = append(out, r)
		}
		return out
	}
	verdict := func() (*bool, *time.Time) {
		t.Helper()
		var v *bool
		var at *time.Time
		if err := l.owner.QueryRow(l.ctx, `SELECT vat_verified, vat_checked_at FROM tenants WHERE id = $1`, fx.id).Scan(&v, &at); err != nil {
			t.Fatal(err)
		}
		return v, at
	}
	account := func() tenant.VATCheck {
		t.Helper()
		s, err := fx.accounts.Settings(l.ctx, fx.id)
		if err != nil {
			t.Fatalf("the customer's account read: %v", err)
		}
		return s.VAT
	}

	// 1. The overview links the screen; the screen says never checked.
	if w := l.get("/operator/tenants/"+fx.id.String(), sess); w.Code != http.StatusOK ||
		!strings.Contains(w.Body.String(), `<a href="`+path+`" class="op-link">See this tenant's VAT number</a>`) {
		t.Fatalf("the overview = %d, or it does not link %s", w.Code, path)
	}
	w := l.get(path, sess)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), fx.number) ||
		!strings.Contains(w.Body.String(), `<span class="tally tally--vat-not-checked">Not checked</span>`) || reads() != 1 {
		t.Fatalf("the screen = %d with %d 'read' row(s); want 200, the number, Not checked and one row; log: %s", w.Code, reads(), l.logs.String())
	}

	// 2. VIES does not answer: nothing written.
	l.vies.set(operator.VATAnswerUnknown)
	w = l.post(path, nil, sess)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "Nothing was changed; the result below still stands.") {
		t.Fatalf("no answer = %d, want 503 with the screen's sentence", w.Code)
	}
	if v, at := verdict(); v != nil || at != nil || checks() != 0 || len(tenantRows()) != 0 || reads() != 2 ||
		account().State() != tenant.VATNeverChecked {
		t.Fatalf("an unanswered ask wrote: verdict %v at %v, %d operator row(s), %d tenant row(s), %d read(s)", v, at, checks(), len(tenantRows()), reads())
	}

	// 3. VIES confirms.
	l.vies.set(operator.VATAnswerValid)
	start := time.Now()
	w = l.post(path, nil, sess)
	end := time.Now()
	if w.Code != http.StatusSeeOther || w.Result().Header.Get("Location") != path {
		t.Fatalf("a confirmation = %d to %q, want 303 to the screen; log: %s", w.Code, w.Result().Header.Get("Location"), l.logs.String())
	}
	v, at := verdict()
	if v == nil || !*v || at == nil || at.Before(start.Add(-2*time.Second)) || at.After(end.Add(2*time.Second)) {
		t.Fatalf("the confirmation stored verdict %v at %v, want true at the wall clock of the request", v, at)
	}
	if n := checks(); n != 1 {
		t.Errorf("%d tenant_vat_checked row(s), want 1", n)
	}
	if n := l.ownerInt(`SELECT count(*)::int FROM operator_audit_log WHERE kind = 'tenant_vat_checked' AND target_tenant_id = $1
	                     AND actor_admin_id = $2 AND session_id IS NOT NULL AND detail = '{}'::jsonb AND target_scope IS NULL
	                     AND target_admin_id IS NULL`, fx.id, l.f.id); n != 1 {
		t.Errorf("the operator row is not the session's, the operator's and the tenant's alone (%d of that shape)", n)
	}
	rows := tenantRows()
	stamp := at.UTC().Format("2006-01-02T15:04:05.000000Z")
	if len(rows) != 1 || rows[0].ActorKind != "operator" || rows[0].Before.Verified != nil || rows[0].Before.CheckedAt != nil ||
		rows[0].After.Verified == nil || !*rows[0].After.Verified || *rows[0].After.CheckedAt != stamp {
		t.Fatalf("the tenant's audit row is not the before (never asked) and after (true, %s): %+v", stamp, rows)
	}
	if a := account(); a.State() != tenant.VATValid || a.CheckedAt == nil || !a.CheckedAt.Equal(*at) {
		t.Errorf("the customer's account read = %q at %v, want valid at the stamp", a.State(), a.CheckedAt)
	}
	w = l.get(path, sess)
	if !strings.Contains(w.Body.String(), `<span class="tally tally--vat-confirmed">Confirmed</span>`) ||
		!strings.Contains(w.Body.String(), `Last answer <span class="font-mono">`+at.UTC().Format("2006-01-02 15:04")+` UTC</span>`) {
		t.Errorf("the screen after the confirmation does not say Confirmed with the stamp's minute")
	}

	// 4. VIES does not know the number.
	l.vies.set(operator.VATAnswerInvalid)
	if w := l.post(path, nil, sess); w.Code != http.StatusSeeOther {
		t.Fatalf("a refusal = %d, want 303", w.Code)
	}
	v2, at2 := verdict()
	rows = tenantRows()
	if v2 == nil || *v2 || at2 == nil || !at2.After(*at) || checks() != 2 || len(rows) != 2 ||
		rows[1].Before.Verified == nil || !*rows[1].Before.Verified || *rows[1].Before.CheckedAt != stamp ||
		rows[1].After.Verified == nil || *rows[1].After.Verified {
		t.Fatalf("the refusal: verdict %v at %v, %d operator row(s), tenant rows %+v; want false later, its before the first's after", v2, at2, checks(), rows)
	}
	if a := account(); a.State() != tenant.VATInvalid {
		t.Errorf("the customer's account read = %q, want invalid", a.State())
	}

	// 5. What VIES was asked, and what the database held meanwhile.
	l.vies.mu.Lock()
	asked, findings := append([]string(nil), l.vies.asked...), append([]string(nil), l.vies.findings...)
	l.vies.mu.Unlock()
	if len(asked) != 3 {
		t.Errorf("VIES was asked %d time(s), want 3", len(asked))
	}
	for _, n := range asked {
		if n != fx.number {
			t.Errorf("VIES was asked about another number than the tenant's")
		}
	}
	for _, f := range findings {
		t.Error(f)
	}
	if reads() != 5 {
		t.Errorf("%d 'read' row(s) naming the tenant's VAT, want 5 (two views, three asks)", reads())
	}
}
