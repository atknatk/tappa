package handler

// identityflag_db_test.go -- M14-08 over HTTP against real Postgres, with the real adminauth.Manager:
// the panel identity flag OFF (ADR 0031 §8, R4) shows a session of a row LINKED to an identity
// exactly what it shows a session of an unlinked row, and a linked row's password change goes
// through the identity end to end (the dual-write is not the flag's).
//
// The identity is COMMITTED (the HTTP server reads it on its own connections), so the test holds
// internal/db's operator-tables lock SHARED on an owner connection and takes the identity back at
// the end (cmd/opadmin's TestTablesLock_IsTakenOncePerTestTree lists this package).

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// operatorTablesTestLock is internal/db's schema-test lock (identityAuditTablesLock is the same
// string, taken transaction-scoped by a test that commits nothing).
const operatorTablesTestLock = identityAuditTablesLock

// sharedTablesLock takes the operator-tables lock SHARED on conn, held until conn closes.
func sharedTablesLock(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock_shared(hashtext($1))`, operatorTablesTestLock); err != nil {
		t.Fatalf("operator-tables test lock: %v", err)
	}
}

// linkHarness links p's admin row to a new identity carrying the row's digest (the way the backfill
// does), on an owner connection that holds the lock for the test and unlinks and deletes the
// identity at the end. It returns the connection.
func linkHarness(t *testing.T, p *panelHarness) *pgx.Conn {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; the identity fixture is written as the owner (real Postgres required)")
	}
	ctx := context.Background()
	owner, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect as the owner: %v", err)
	}
	t.Cleanup(func() {
		if err := owner.Close(context.Background()); err != nil {
			t.Errorf("close the owner connection: %v", err)
		}
	})
	sharedTablesLock(t, ctx, owner)
	var ident uuid.UUID
	if err := owner.QueryRow(ctx, `INSERT INTO admin_identities (email, password_hash)
	                                SELECT email, password_hash FROM admin_users WHERE id = $1 RETURNING id`, p.adminID).Scan(&ident); err != nil {
		t.Fatalf("birth the identity: %v", err)
	}
	t.Cleanup(func() {
		for _, sql := range []string{
			`UPDATE admin_users SET identity_id = NULL, linked_via = NULL, linked_at = NULL WHERE identity_id = $1`,
			`DELETE FROM admin_identities WHERE id = $1`,
		} {
			if _, err := owner.Exec(context.Background(), sql, ident); err != nil {
				t.Errorf("take the identity back (%s): %v", sql, err)
			}
		}
	})
	if _, err := owner.Exec(ctx, `UPDATE admin_users SET identity_id = $2, linked_via = 'backfill', linked_at = now() WHERE id = $1`, p.adminID, ident); err != nil {
		t.Fatalf("link the row: %v", err)
	}
	return owner
}

// r4Token matches the values a page carries that are its SESSION's (ADR 0031 §8's exception): the
// synchronizer token of every form and a CSP nonce.
var r4Token = regexp.MustCompile(`(name="csrf" value=")[^"]*(")|(nonce=")[^"]*(")|('nonce-)[^']*(')`)

// r4Normal is one response with what belongs to ITS harness replaced: the business's and the
// admin's ids, the address, the VAT number, the server's origin -- and the session's tokens.
func r4Normal(p *panelHarness, body string) string {
	for from, to := range map[string]string{
		p.server.URL: "ORIGIN", p.tenantID.String(): "TENANT", p.adminID.String(): "ADMIN", p.email: "EMAIL",
		"VAT-" + p.tenantID.String(): "VAT",
	} {
		body = strings.ReplaceAll(body, from, to)
	}
	return r4Token.ReplaceAllString(body, "${1}${3}${5}TOKEN${2}${4}${6}")
}

// TestPanelIdentityFlagOff_ALinkedAndAnUnlinkedSessionSeeTheSamePanel is the card's R4 criterion:
// with TAPPA_ADMIN_IDENTITY off, a session of a single-membership row LINKED to an identity and a
// session of an UNLINKED row get the same status, the same Content-Type and byte for byte the same
// body -- the business's name and the session-specific tokens aside -- for the shell (/admin), the
// account page and /admin/switch (404). The sign-in itself is today's for both (the linked row
// carries the identity's digest). CONTROLS: both really signed in (the shell names the operator) and
// the linked row really is linked. What normalisation removes is only the harness's own values and
// the session's tokens (the log line says, per page, whether the raw bytes were already equal).
func TestPanelIdentityFlagOff_ALinkedAndAnUnlinkedSessionSeeTheSamePanel(t *testing.T) {
	opts := panelHarnessOptions{tenantName: "R4 Same Business Ltd"}
	unlinked := newPanelHarnessWith(t, opts)
	linked := newPanelHarnessWith(t, opts)
	owner := linkHarness(t, linked)
	var linkedNow bool
	if err := owner.QueryRow(context.Background(), `SELECT identity_id IS NOT NULL FROM admin_users WHERE id = $1`, linked.adminID).Scan(&linkedNow); err != nil || !linkedNow {
		t.Fatalf("CONTROL: the linked harness's row is not linked (%v)", err)
	}
	unlinked.signIn(t) // today's sign-in: the flag is off
	linked.signIn(t)

	for _, path := range []string{"/admin", accountHref, "/admin/switch"} {
		ur, ub := unlinked.get(t, path)
		lr, lb := linked.get(t, path)
		if path == "/admin" && (!strings.Contains(ub, "E2E Owner") || !strings.Contains(lb, "E2E Owner")) {
			t.Fatalf("CONTROL: a shell does not name the signed-in operator (statuses %d / %d)", ur.StatusCode, lr.StatusCode)
		}
		t.Logf("%s: %d bytes, raw bodies identical before normalisation: %v", path, len(lb), ub == lb)
		if ur.StatusCode != lr.StatusCode || ur.Header.Get("Content-Type") != lr.Header.Get("Content-Type") {
			t.Errorf("%s: unlinked %d %q, linked %d %q", path, ur.StatusCode, ur.Header.Get("Content-Type"), lr.StatusCode, lr.Header.Get("Content-Type"))
		}
		if path == "/admin/switch" && ur.StatusCode != http.StatusNotFound {
			t.Errorf("/admin/switch with the flag off: %d, want 404", ur.StatusCode)
		}
		if nu, nl := r4Normal(unlinked, ub), r4Normal(linked, lb); nu != nl {
			t.Errorf("%s: the linked session's page differs from the unlinked one's:\n%s", path, r4FirstDifference(nu, nl))
		}
	}
}

// r4FirstDifference shows where two bodies part.
func r4FirstDifference(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	lo := max(0, i-120)
	return "unlinked …" + a[lo:min(len(a), i+120)] + "…\nlinked   …" + b[lo:min(len(b), i+120)] + "…"
}

// TestPanelPassword_ALinkedRowChangesThroughTheIdentity: over HTTP, the account page's password change
// of a LINKED row (flag off -- the write is not the flag's) answers like any change (303 to the
// flash), writes the identity and the row (the same new digest), leaves exactly ONE
// admin.password_changed row in the business (the database's -- the handler writes none), and the
// person signs in with the new password. Without M14-08 this was a 500: the row refuses a direct
// write (00069's trigger).
func TestPanelPassword_ALinkedRowChangesThroughTheIdentity(t *testing.T) {
	p := newPanelHarnessWith(t, panelHarnessOptions{})
	owner := linkHarness(t, p)
	p.signIn(t)

	next := "a-new-password-" + uuid.NewString()
	res, _ := p.post(t, accountPasswordHref, url.Values{
		"current_password": {p.password}, "new_password": {next}, "confirm_password": {next},
	})
	if res.StatusCode != http.StatusSeeOther || !strings.Contains(res.Header.Get("Location"), "done=password") {
		t.Fatalf("the change: %d %q, want 303 to the done=password flash", res.StatusCode, res.Header.Get("Location"))
	}
	var same bool
	if err := owner.QueryRow(context.Background(), `SELECT a.password_hash = i.password_hash AND a.password_hash <> $2
	                                                  FROM admin_users a JOIN admin_identities i ON i.id = a.identity_id
	                                                 WHERE a.id = $1`, p.adminID, ownerDigest).Scan(&same); err != nil || !same {
		t.Fatalf("the identity and the row do not carry the same NEW digest (%v)", err)
	}
	if n := p.auditCount(t, ActionAdminPasswordChanged); n != 1 {
		t.Fatalf("%d %s rows in the business, want exactly the database's one", n, ActionAdminPasswordChanged)
	}
	res, _ = p.post(t, "/admin/logout", url.Values{})
	if res.StatusCode != http.StatusSeeOther {
		t.Fatalf("sign-out: %d", res.StatusCode)
	}
	p.password = next
	p.signIn(t)
}
