package adminauth

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/db"
)

// switchLeftDB is a Database whose switch succeeds and whose GetAdminSessionSecondsLeft -- the one
// statement Manager.Switch runs in WithTenant -- answers row (or err). Every other method is the nil
// embedded interface: the test reaches none of them.
type switchLeftDB struct {
	Database
	left int32
	err  error
}

func (s *switchLeftDB) SwitchIdentitySession(context.Context, string, uuid.UUID, string) (db.IdentitySession, error) {
	return db.IdentitySession{SessionID: uuid.New(), TenantID: uuid.New(), AdminUserID: uuid.New()}, nil
}

func (s *switchLeftDB) WithTenant(ctx context.Context, _ uuid.UUID, fn db.TxFunc) error {
	return fn(ctx, leftTx{left: s.left, err: s.err})
}

// leftTx answers QueryRow with one int32 (or err); it is the only method store's seconds-left read calls.
type leftTx struct {
	pgx.Tx
	left int32
	err  error
}

func (t leftTx) QueryRow(context.Context, string, ...any) pgx.Row { return leftRow(t) }

type leftRow leftTx

func (r leftRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int32)) = r.left
	return nil
}

// TestSwitch_ASessionThatEndedBeforeItsCookieIsRefused (T183, the third eye's DÜŞÜK, 2026-10-31): the
// switch committed, but the new session's remaining time reads 0 rows (it ended in the second between
// the switch and the read) or 0 seconds -- Switch answers ErrSwitchRefused with a zero Issued, so the
// handler writes no cookie (TestAdminSwitch_ARefusalIsOneAnswerAndTouchesNoCookie). A positive
// remainder comes back as Left. MUTATION (measured): the branch removed -> red (a dead session's Issued
// with Left 0, i.e. a full-lifetime cookie).
func TestSwitch_ASessionThatEndedBeforeItsCookieIsRefused(t *testing.T) {
	tok, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		data *switchLeftDB
		ok   bool
	}{
		{"no row: ended between the switch and the read", &switchLeftDB{err: pgx.ErrNoRows}, false},
		{"zero seconds left", &switchLeftDB{left: 0}, false},
		{"seventeen seconds left", &switchLeftDB{left: 17}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, err := New(c.data, testConfig())
			if err != nil {
				t.Fatal(err)
			}
			is, err := m.Switch(context.Background(), tok, uuid.New())
			if c.ok {
				if err != nil || is.Left.Seconds() != 17 || is.Token.reveal() == "" {
					t.Fatalf("Switch = %+v, %v; want Left 17 s and a token", is, err)
				}
				return
			}
			if !errors.Is(err, ErrSwitchRefused) || is.Token.reveal() != "" || is.Left != 0 || is.Session.ID != uuid.Nil {
				t.Fatalf("Switch = %+v, %v; want ErrSwitchRefused and a zero Issued", is, err)
			}
		})
	}
}
