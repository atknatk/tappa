package handler

// accountpasswordrace_db_test.go -- T179 over HTTP: the account page's password change of an
// UNLINKED row whose row is linked to an identity between adminauth's identity check and
// SetOwnAdminPassword. The real panel (flag on), the real adminauth.Manager over the real pool,
// 00069's trigger; the link is written by the owner inside the Manager's own transaction, right
// before the statement is sent (wrapAdminData). Before T179 both cases answered 500 (the trigger's
// restrict_violation); internal/adminauth's linkrace_db_test.go holds both timings and the reset.

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/atknatk/tappa/internal/adminauth"
	"github.com/atknatk/tappa/internal/db"
)

// linkBeforeStatement is a database whose WithTenant hands the callback a transaction that runs act
// immediately before the sqlc statement called name is sent -- once.
type linkBeforeStatement struct {
	*db.DB
	name string
	once sync.Once
	act  func()
}

func (l *linkBeforeStatement) WithTenant(ctx context.Context, tenantID uuid.UUID, fn db.TxFunc) error {
	return l.DB.WithTenant(ctx, tenantID, func(ctx context.Context, tx pgx.Tx) error {
		return fn(ctx, linkHookTx{Tx: tx, l: l})
	})
}

type linkHookTx struct {
	pgx.Tx
	l *linkBeforeStatement
}

func (h linkHookTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.HasPrefix(sql, "-- name: "+h.l.name+" ") && h.l.act != nil {
		h.l.once.Do(h.l.act)
	}
	return h.Tx.QueryRow(ctx, sql, args...)
}

// TestPanelPassword_ARowLinkedMeanwhileIsNeverA500 (T179):
//
//	reconcile  the identity is born from the row and links it (identity_reconcile's shape): the
//	           change goes through the identity -- 303 to the done=password flash, exactly ONE
//	           admin.password_changed row (the database's), the identity and the row carry the new
//	           digest, and this browser is still signed in;
//	email      the identity has a password of its own and links the row the way the link by
//	           e-mail does (its digest over the row's, the row's sessions revoked): 303 to the
//	           sign-in page with the panel cookie cleared, no admin.password_changed row, the
//	           identity's digest untouched.
func TestPanelPassword_ARowLinkedMeanwhileIsNeverA500(t *testing.T) {
	for _, shape := range []string{"reconcile", "email"} {
		t.Run(shape, func(t *testing.T) {
			hook := &linkBeforeStatement{name: "SetOwnAdminPassword"}
			p := newPanelHarnessWith(t, panelHarnessOptions{adminIdentity: true, tenantName: "T179 " + shape + " Ltd",
				wrapAdminData: func(d *db.DB) adminauth.Database { hook.DB = d; return hook }})
			owner := holdingOwner(t)
			ctx := context.Background()
			t.Cleanup(func() {
				for _, sql := range []string{
					`DELETE FROM admin_identity_events WHERE identity_id IN (SELECT id FROM admin_identities WHERE email = $1)`,
					`UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL WHERE email = $1`,
					`DELETE FROM admin_identities WHERE email = $1`,
				} {
					if _, err := owner.Exec(context.Background(), sql, p.email); err != nil {
						t.Errorf("take the identity back (%s): %v", sql, err)
					}
				}
			})
			// No identity yet: the flag-on sign-in takes the unlinked row's arm (ADR 0031 §5.6).
			p.signIn(t)

			identityPW := "the-identitys-own-" + uuid.NewString()
			hook.act = func() {
				var stmts []string
				switch shape {
				case "reconcile":
					stmts = []string{
						`INSERT INTO admin_identities (email, password_hash) SELECT email, password_hash FROM admin_users WHERE id = $1 AND $2::text IS NOT NULL`,
						`UPDATE admin_users SET identity_id = (SELECT id FROM admin_identities WHERE email = admin_users.email),
						        linked_via = 'reconcile', linked_at = now() WHERE id = $1 AND $2::text IS NOT NULL`,
					}
				case "email":
					stmts = []string{
						`INSERT INTO admin_identities (email, password_hash) SELECT email, crypt($2, gen_salt('bf', 4)) FROM admin_users WHERE id = $1`,
						`UPDATE admin_users SET identity_id = (SELECT id FROM admin_identities WHERE email = admin_users.email), linked_via = 'email',
						        linked_at = now(), password_hash = (SELECT password_hash FROM admin_identities WHERE email = admin_users.email)
						  WHERE id = $1 AND $2::text IS NOT NULL`,
						`UPDATE admin_sessions SET revoked_at = now() WHERE admin_user_id = $1 AND revoked_at IS NULL AND $2::text IS NOT NULL`,
					}
				}
				for _, s := range stmts {
					if _, err := owner.Exec(ctx, s, p.adminID, identityPW); err != nil {
						t.Errorf("the linker: %s: %v", s, err)
					}
				}
			}

			next := "a-new-password-" + uuid.NewString()
			res, _ := p.post(t, accountPasswordHref, url.Values{
				"current_password": {p.password}, "new_password": {next}, "confirm_password": {next},
			})
			var linked bool
			if err := owner.QueryRow(ctx, `SELECT identity_id IS NOT NULL FROM admin_users WHERE id = $1`, p.adminID).Scan(&linked); err != nil || !linked {
				t.Fatalf("PREMISE: the linker did not link the row (%v)", err)
			}
			if res.StatusCode == http.StatusInternalServerError {
				t.Fatalf("a row linked meanwhile answered 500")
			}

			var identityVerifies bool
			switch shape {
			case "reconcile":
				if res.StatusCode != http.StatusSeeOther || !strings.Contains(res.Header.Get("Location"), "done=password") {
					t.Fatalf("the change: %d %q, want 303 to the done=password flash", res.StatusCode, res.Header.Get("Location"))
				}
				if n := p.auditCount(t, ActionAdminPasswordChanged); n != 1 {
					t.Errorf("%d %s rows in the business, want exactly the database's one", n, ActionAdminPasswordChanged)
				}
				if err := owner.QueryRow(ctx, `SELECT i.password_hash = crypt($2, i.password_hash) AND a.password_hash = i.password_hash
				                                 FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id WHERE a.id = $1`,
					p.adminID, next).Scan(&identityVerifies); err != nil || !identityVerifies {
					t.Errorf("the identity and the row do not carry the NEW password's digest (%v)", err)
				}
				if !p.signedIn(t) {
					t.Error("the browser that made the change was signed out")
				}
			case "email":
				if res.StatusCode != http.StatusSeeOther || res.Header.Get("Location") != adminLoginPath {
					t.Fatalf("the change whose session the link ended: %d %q, want 303 to %s", res.StatusCode, res.Header.Get("Location"), adminLoginPath)
				}
				if c := p.panelCookie(t); c != "" {
					t.Error("the dead session's cookie was kept")
				}
				if n := p.auditCount(t, ActionAdminPasswordChanged); n != 0 {
					t.Errorf("%d %s rows for a change that did not happen", n, ActionAdminPasswordChanged)
				}
				if err := owner.QueryRow(ctx, `SELECT i.password_hash = crypt($2, i.password_hash) FROM admin_identities i WHERE i.email = $1`,
					p.email, identityPW).Scan(&identityVerifies); err != nil || !identityVerifies {
					t.Errorf("the identity's own password no longer verifies; nothing may have been written (%v)", err)
				}
			}
		})
	}
}
