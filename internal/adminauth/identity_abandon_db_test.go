package adminauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/atknatk/tappa/internal/db"
)

// failingLink is the real database with identity_link_legacy failing as err: a failure AFTER
// identity_open_session committed.
type failingLink struct {
	Database
	err error
}

func (f failingLink) LinkLegacySession(context.Context, string, string) (db.LinkedMembership, error) {
	return db.LinkedMembership{}, f.err
}

// TestAuthenticatePerson_AFailureAfterTheOpenAbandonsTheSession (M14-09's audit, DÜŞÜK-1): the same
// password proves the identity and an unlinked row, the identity session opens (committed, with the
// business's admin.login.succeeded row), and the link then fails as a lost deadlock. The sign-in fails
// with that error -- never as bad credentials, never as a legacy sign-in -- and hands the session back
// as Abandoned, REVOKED: no live session is left in either business, the proof session is revoked, the
// row stays unlinked, and the database's success row stays (the handler writes the rest of the trail).
// MUTATIONS: abandon without the revocation -> a live identity session, red; AuthenticatePerson
// dropping Abandoned -> red.
func TestAuthenticatePerson_AFailureAfterTheOpenAbandonsTheSession(t *testing.T) {
	d := testDB(t)
	m := testManager(t, d)
	lost := &pgconn.PgError{Code: "40P01", Message: "deadlock detected"}
	m.data = failingLink{Database: d, err: lost}
	w := newIDWorld(t, ownerConn(t), true)
	ta := w.tenant("M14-09 Abandon A")
	w.linked(ta, "active", "backfill", 48*time.Hour)
	tb := w.tenant("M14-09 Abandon B")
	legacy := w.row(tb, w.digest, "active", time.Hour)

	in, err := m.AuthenticatePerson(context.Background(), w.email, w.pw, uuid.Nil, generousBudget())
	if err == nil || errors.Is(err, ErrBadCredentials) || !errors.Is(err, lost) {
		t.Fatalf("AuthenticatePerson error = %v; want the link's failure, not bad credentials", err)
	}
	if in.Session != nil || len(in.Legacy.Verified()) != 0 {
		t.Fatalf("a failed sign-in returned a session or a legacy sign-in: %+v", in)
	}
	if in.Abandoned == nil || !in.Abandoned.Revoked || in.Abandoned.Session.TenantID != ta {
		t.Fatalf("Abandoned = %+v; want the identity session in %s, revoked", in.Abandoned, ta)
	}
	if n := w.sessions(ta, true); n != 0 {
		t.Errorf("%d live sessions in the identity's business after an abandoned sign-in, want 0", n)
	}
	if n := w.sessions(ta, false); n != 1 {
		t.Errorf("%d sessions in the identity's business, want the one opened (and revoked)", n)
	}
	if n := w.sessions(tb, true); n != 0 {
		t.Errorf("%d live sessions in the legacy business (the proof session), want 0", n)
	}
	if id, _ := w.link(legacy); id != uuid.Nil {
		t.Errorf("the legacy row was linked although the link failed")
	}
	if n := w.audit(ta, "admin.login.succeeded"); n != 1 {
		t.Errorf("%d admin.login.succeeded rows in the identity's business, want the database's one", n)
	}
}
