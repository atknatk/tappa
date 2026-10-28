package adminauth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/config"
	"github.com/atknatk/tappa/internal/db"
)

// suspension_test.go -- the K15-9 mapping pin (ADR 0025 §3): adminauth.Resolved's
// Suspended field has its HARMFUL value at zero (nil = writes open), so the copy from
// TouchAdminSession's two columns into it is the one line a suspension depends on.
// This test drives Verify over a scripted statement and fails if that copy is dropped,
// half-done or inverted. The end-to-end pin is internal/handler's
// TestPanelSuspensionDB_TheNextPostOfTheSameSessionIsRefused.

// touchScript is one TouchAdminSession answer: the nine columns in RETURNING order (00071 -- M14-04,
// ADR 0031 §5.3 -- added the business's name and the membership's identity_id), or an error.
type touchScript struct {
	at     *time.Time
	reason *string
	err    error
}

// scriptedDB resolves every token to one live session and answers TouchAdminSession
// from its script. Nothing else is implemented: a second statement would panic on the
// embedded nil Tx, which is the point -- Verify runs exactly one.
type scriptedDB struct {
	noIdentityData
	tenant, admin, session uuid.UUID
	role                   string
	script                 touchScript
	sql                    string
	// tenantName is the business's name the statement answers ("" answers "Scripted Business").
	tenantName string
	// statements counts the statements run through the tenant transaction (M14-12: the shell's
	// name costs none of its own).
	statements int
}

func (s *scriptedDB) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	if tenantID != s.tenant {
		return errors.New("scriptedDB: wrong tenant context")
	}
	return fn(ctx, &scriptedTx{db: s})
}

func (s *scriptedDB) GetAdminByEmail(context.Context, string) ([]db.ResolvedAdmin, error) {
	return nil, errors.New("scriptedDB: not a sign-in")
}

func (s *scriptedDB) GetAdminSessionByTokenHash(context.Context, string) (db.ResolvedAdminSession, error) {
	return db.ResolvedAdminSession{ID: s.session, TenantID: s.tenant, AdminUserID: s.admin}, nil
}

type scriptedTx struct {
	pgx.Tx
	db *scriptedDB
}

func (t *scriptedTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	t.db.sql = sql
	t.db.statements++
	return scriptedRow{t.db}
}

type scriptedRow struct{ db *scriptedDB }

func (r scriptedRow) Scan(dest ...any) error {
	if r.db.script.err != nil {
		return r.db.script.err
	}
	if len(dest) != 9 {
		return errors.New("scriptedRow: TouchAdminSession no longer returns nine columns")
	}
	now := time.Now()
	*dest[0].(*uuid.UUID) = r.db.session
	*dest[1].(**time.Time) = &now
	*dest[2].(*uuid.UUID) = r.db.admin
	*dest[3].(*string) = r.db.role
	*dest[4].(*string) = "Owner"
	*dest[5].(**time.Time) = r.db.script.at
	*dest[6].(**string) = r.db.script.reason
	*dest[7].(*string) = "Scripted Business"
	if r.db.tenantName != "" {
		*dest[7].(*string) = r.db.tenantName
	}
	*dest[8].(**uuid.UUID) = nil
	return nil
}

func ptrTo(s string) *string { return &s }

// verifyCase is one scripted statement answer and what Verify must make of it.
type verifyCase struct {
	name   string
	script touchScript
	want   *Suspension
	err    error
}

// TestVerify_CarriesTheSuspensionTheStatementReturned: Verify copies the business's
// suspension from the statement that proves the session -- both columns, unchanged --
// and a row with neither column set is not suspended. Half a suspension (a row the
// CHECK forbids) is still a suspension. A statement that finds no row (the tenant row
// hidden from it, or the session gone) is "not signed in", never "not suspended". And
// the statement it ran is the one that joins tenants and returns both columns.
func TestVerify_CarriesTheSuspensionTheStatementReturned(t *testing.T) {
	since := time.Date(2026, 10, 8, 9, 15, 0, 0, time.UTC)
	reason := string(db.SuspensionSecurityHold)
	cases := []verifyCase{
		{"not suspended", touchScript{}, nil, nil},
		{"only the time (a row the CHECK forbids)", touchScript{at: &since}, &Suspension{Since: since}, nil},
		{"only the code (a row the CHECK forbids)", touchScript{reason: &reason}, &Suspension{Reason: string(db.SuspensionSecurityHold)}, nil},
		{"no row: the tenant cannot be seen", touchScript{err: pgx.ErrNoRows}, nil, ErrNoSession},
		{"a code this build does not know", touchScript{at: &since, reason: ptrTo("a_code_from_a_newer_build")},
			&Suspension{Since: since, Reason: "a_code_from_a_newer_build"}, nil},
	}
	// EVERY CODE OF THE CLOSED SET (2nd round, F2): a mapping that dropped one of the
	// four -- customer_request, say -- would open that business's writes, and a table
	// with one code in it could not see it.
	for _, r := range db.SuspensionReasons() {
		code := string(r)
		cases = append(cases, verifyCase{"suspended: " + code, touchScript{at: &since, reason: &code},
			&Suspension{Since: since, Reason: code}, nil})
	}
	// BOTH ROLES (2nd round, F2): a suspension is the business's, so a manager's session
	// carries it exactly as an owner's does.
	for _, role := range []string{"owner", "manager"} {
		for _, c := range cases {
			verifyCarries(t, role, c)
		}
	}
}

// verifyCarries is one (role, case) run of the test above.
func verifyCarries(t *testing.T, role string, c verifyCase) {
	t.Helper()
	t.Run(role+"/"+c.name, func(t *testing.T) {
		data := &scriptedDB{tenant: uuid.New(), admin: uuid.New(), session: uuid.New(), role: role, script: c.script}
		m, err := New(data, &config.Config{SessionHMACKey: []byte("0123456789abcdef0123456789abcdef")})
		if err != nil {
			t.Fatal(err)
		}
		tok, err := newToken()
		if err != nil {
			t.Fatal(err)
		}
		res, err := m.Verify(context.Background(), tok)
		if !errors.Is(err, c.err) || (c.err == nil && err != nil) {
			t.Fatalf("Verify err = %v, want %v", err, c.err)
		}
		switch {
		case c.want == nil && res.Suspended != nil:
			t.Errorf("Verify reports a suspension %+v the statement did not return", *res.Suspended)
		case c.want != nil && (res.Suspended == nil || *res.Suspended != *c.want):
			t.Errorf("Verify reports %+v, want %+v -- a suspension the statement returned was dropped "+
				"(ADR 0025 K15-9: nil opens every write)", res.Suspended, *c.want)
		}
		sql := strings.Join(strings.Fields(data.sql), " ")
		for _, must := range []string{"FROM admin_users a, tenants t", "AND t.id = s.tenant_id", "t.suspended_at, t.suspension_reason"} {
			if !strings.Contains(sql, must) {
				t.Errorf("the statement Verify ran does not contain %q:\n%s", must, sql)
			}
		}
		if strings.Contains(strings.ToUpper(sql), "LEFT JOIN") {
			t.Errorf("the statement joins tenants with an outer join; a tenant it cannot see must be 0 rows")
		}
	})
}
