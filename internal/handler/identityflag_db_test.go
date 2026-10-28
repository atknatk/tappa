package handler

// identityflag_db_test.go -- M14-08 over HTTP against real Postgres, with the real adminauth.Manager:
// the panel identity flag OFF (ADR 0031 §8, R4) shows a session of a row LINKED to an identity
// exactly what it shows a session of an unlinked row, and a linked row's password change goes
// through the identity end to end (the dual-write is not the flag's).
//
// The identity is COMMITTED (the HTTP server reads it on its own connections), so the test holds
// internal/db's operator-tables lock SHARED -- through the harness, whose holdPanelTablesLock is this
// package's one re-entrant holder (M14-09) -- and takes the identity back at the end (cmd/opadmin's
// TestTablesLock_IsTakenOncePerTestTree lists this package).

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// operatorTablesTestLock is internal/db's schema-test lock (identityAuditTablesLock is the same
// string, taken transaction-scoped by a test that commits nothing).
const operatorTablesTestLock = identityAuditTablesLock

// sharedTablesLock takes the operator-tables lock SHARED on conn, held until conn closes. Its one
// caller is holdPanelTablesLock.
func sharedTablesLock(t *testing.T, ctx context.Context, conn *pgx.Conn) {
	t.Helper()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock_shared(hashtext($1))`, operatorTablesTestLock); err != nil {
		t.Fatalf("operator-tables test lock: %v", err)
	}
}

// panelTablesLock is this test binary's ONE holder of the operator-tables lock (holdPanelTablesLock).
var panelTablesLock struct {
	mu   sync.Mutex
	held int
	conn *pgx.Conn
}

// holdPanelTablesLock holds internal/db's operator-tables lock SHARED until t ends (M14-09; the M14-08
// audit's finding). Every panel harness commits admin_users rows -- and the identity tests commit
// identities -- into the database internal/db's backfill tests read whole under that lock held
// EXCLUSIVE (bfTx): TestAdminBackfill00073_TheClosingAssertion went red when a harness committed a
// row with an address while it ran (`go test ./...` runs the packages in parallel).
//
// 🔴 RE-ENTRANT, PER PROCESS, AND THAT IS THE DEADLOCK cmd/opadmin's TestTablesLock_IsTakenOncePerTestTree
// IS ABOUT. A test that builds two harnesses, or one inside a subtest, would otherwise ask for the
// lock a second time on a second connection while holding it on the first -- and a SHARED request
// queues behind a waiting EXCLUSIVE one, so the test would wait on internal/db, which waits on the
// test. Here only the FIRST holder sends the statement; every later one, while any is alive, counts
// itself in; the last to end closes the connection, which releases the lock. So no test of this
// package ever waits for the lock while it holds it. The scan knows this function as the package's
// re-entrant holder (tablesLockReentrant) and requires sharedTablesLock to have no other caller.
//
// Without DATABASE_MIGRATE_URL nothing is held: every test that takes the lock EXCLUSIVE connects as
// the owner and skips without it, so there is nothing to take turns with.
//
// It returns the release (idempotent; t's cleanup calls it too), so a test whose committed fixtures
// are already in place can stop holding the lock before a long stretch that writes none
// (TestPanelE2E_TimingIsFlatOverHTTP, TestPanelE2E_IdentityTimingIsFlatOverHTTP: cost-12 samples, a
// couple of minutes under -race, during which every other package's turn would queue behind them).
func holdPanelTablesLock(t *testing.T) (release func()) {
	t.Helper()
	dsn := os.Getenv("DATABASE_MIGRATE_URL")
	if dsn == "" {
		return func() {}
	}
	panelTablesLock.mu.Lock()
	defer panelTablesLock.mu.Unlock()
	if panelTablesLock.held == 0 {
		ctx := context.Background()
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatalf("connect as the owner for the operator-tables lock: %v", err)
		}
		ok := false
		defer func() {
			if ok {
				return
			}
			if err := conn.Close(context.Background()); err != nil {
				t.Errorf("close the owner connection: %v", err)
			}
		}()
		sharedTablesLock(t, ctx, conn)
		ok = true
		panelTablesLock.conn = conn
	}
	panelTablesLock.held++
	var once sync.Once
	release = func() {
		once.Do(func() {
			panelTablesLock.mu.Lock()
			defer panelTablesLock.mu.Unlock()
			panelTablesLock.held--
			if panelTablesLock.held > 0 {
				return
			}
			if err := panelTablesLock.conn.Close(context.Background()); err != nil {
				t.Errorf("release the operator-tables lock: %v", err)
			}
			panelTablesLock.conn = nil
		})
	}
	t.Cleanup(release)
	return release
}

// linkHarness links p's admin row to a new identity carrying the row's digest (the way the backfill
// does), on an owner connection that unlinks and deletes the identity at the end, and returns the
// connection. The operator-tables lock is p's (newPanelHarnessWith holds it while p lives).
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
	r4SamePanels(t, unlinked, linked)
}

// r4SamePanels is the R4 comparison: the shell, the account page and /admin/switch (404) of two
// signed-in harnesses, status, Content-Type and normalised body equal.
func r4SamePanels(t *testing.T, unlinked, linked *panelHarness) {
	t.Helper()
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

// TestPanelIdentityFlagOff_AWindowMovedBy00083ChangesNothing is M14-19's "00083 applied, flag still
// off: everything as today" (the migration must be safe to ship before the flag commit). The linked
// row's identity carries what 00083 leaves behind -- a verified address and a reset-scope window
// opened two days ago whose END was moved to now() + 7 days -- and with TAPPA_ADMIN_IDENTITY off:
// the shell, the account page and /admin/switch are byte for byte the unlinked session's (R4), no
// page says anything of the window, and "Keep all" -- the one press that would close it -- is not
// mounted (404) and leaves the window exactly as it was. CONTROL: the window really is open.
// MUTATION (measured): identityGate opened for the account surface while the flag is off ->
// the account page differs (red).
func TestPanelIdentityFlagOff_AWindowMovedBy00083ChangesNothing(t *testing.T) {
	opts := panelHarnessOptions{tenantName: "M14-19 Same Business Ltd"}
	unlinked := newPanelHarnessWith(t, opts)
	linked := newPanelHarnessWith(t, opts)
	owner := linkHarness(t, linked)
	ctx := context.Background()
	if _, err := owner.Exec(ctx, `UPDATE admin_identities
	                                 SET email_verified_at = now() - interval '2 days',
	                                     confirm_opened_at = now() - interval '2 days',
	                                     confirm_open_until = now() + interval '7 days',
	                                     confirm_scope = 'reset'
	                               WHERE id = (SELECT identity_id FROM admin_users WHERE id = $1)`, linked.adminID); err != nil {
		t.Fatalf("give the identity 00083's window: %v", err)
	}
	window := func() string {
		var w string
		if err := owner.QueryRow(ctx, `SELECT concat_ws('|', i.confirm_scope, i.confirm_opened_at::text, i.confirm_open_until::text, i.confirm_open_until > now())
		                                 FROM admin_identities i JOIN admin_users a ON a.identity_id = i.id WHERE a.id = $1`, linked.adminID).Scan(&w); err != nil {
			t.Fatalf("read the window: %v", err)
		}
		return w
	}
	before := window()
	if !strings.HasPrefix(before, "reset|") || !strings.HasSuffix(before, "|t") {
		t.Fatalf("CONTROL: the identity's window is not open: %q", before)
	}
	unlinked.signIn(t)
	linked.signIn(t)
	r4SamePanels(t, unlinked, linked)
	_, page := linked.get(t, accountHref)
	for _, word := range []string{"Keep all", accountKeepAllHref, "Your businesses"} {
		if strings.Contains(page, word) {
			t.Errorf("with the flag off the account page says %q", word)
		}
	}
	res, _ := linked.post(t, accountKeepAllHref, url.Values{})
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("POST %s with the flag off: %d, want 404", accountKeepAllHref, res.StatusCode)
	}
	if after := window(); after != before {
		t.Errorf("the window changed with the flag off: %q -> %q", before, after)
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

// TestHoldPanelTablesLock_IsReEntrant: a second harness built while one lives -- in a subtest, the
// shape cmd/opadmin's scan no longer flags for this holder -- does not ask for the lock again: the
// count goes up by one, the connection holding the lock is the same, and that connection holds the
// advisory lock exactly once. When the subtest ends the count comes back down and the lock is still
// held for the parent. MUTATION: holdPanelTablesLock connects and locks on every call -> red.
func TestHoldPanelTablesLock_IsReEntrant(t *testing.T) {
	if os.Getenv("DATABASE_MIGRATE_URL") == "" {
		t.Skip("DATABASE_MIGRATE_URL not set; nothing is held without the owner connection")
	}
	newPanelHarness(t)
	panelTablesLock.mu.Lock()
	first, before := panelTablesLock.conn, panelTablesLock.held
	panelTablesLock.mu.Unlock()
	if first == nil || before < 1 {
		t.Fatalf("a live harness holds nothing (conn %v, count %d)", first, before)
	}
	held := func(c *pgx.Conn) int {
		t.Helper()
		var n int
		if err := first.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND granted AND pid = $1`, c.PgConn().PID()).Scan(&n); err != nil {
			t.Fatalf("read pg_locks: %v", err)
		}
		return n
	}
	t.Run("a second harness", func(t *testing.T) {
		newPanelHarness(t)
		panelTablesLock.mu.Lock()
		conn, count := panelTablesLock.conn, panelTablesLock.held
		panelTablesLock.mu.Unlock()
		if conn != first || count != before+1 {
			t.Fatalf("the second harness asked again: connection replaced %v, count %d (was %d)", conn != first, count, before)
		}
		if n := held(first); n != 1 {
			t.Fatalf("the holding connection holds %d advisory locks, want 1", n)
		}
	})
	panelTablesLock.mu.Lock()
	conn, after := panelTablesLock.conn, panelTablesLock.held
	panelTablesLock.mu.Unlock()
	if conn != first || after != before {
		t.Fatalf("after the subtest: connection kept %v, count %d, want %d", conn == first, after, before)
	}
}
