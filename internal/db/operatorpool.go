package db

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atknatk/tappa/internal/config"
)

// OperatorDB is the platform operator's OWN pool (M10 OP-7; ADR 0021 §3.6, §4). It
// connects as tappa_operator through TAPPA_OPERATOR_DATABASE_URL, and it is the second
// pool this process may open -- *DB, tappa_app's, is the first. The two share no
// connection and no type, and the customer side is never handed this one (cmd/tappa's
// TestOperatorWiring_ThePoolReachesOnlyTheAuthenticator: the pool goes to the
// operator's Authenticator and, since OP-10 phase B, to the operator surface's legal
// slot, (OP-11 phase B) its tenant slot and (OP-13 phase B) its plaque slot -- and to no
// other name in cmd/tappa's main.go and operator.go, the scope that test reads by syntax;
// it is not a whole-program proof).
//
// WHAT IT IS NOT, AND EACH ABSENCE IS A DECISION:
//
//   - NO WithTenant. ADR 0021 §3.6: the operator's way past the tenant boundary is the
//     op_* functions and nothing else; this type sets no app.tenant_id and opens no
//     transaction for a caller (TestOperatorDB_HasNoTenantDoorAndNoRawSQLDoor).
//
//   - NO Exec, NO QueryRow -- so *OperatorDB is NOT itself an OperatorConn, although
//     the card's OP-4 block said it would be (corrected in the OP-7 card correction,
//     with the measurement). Its pool IS one, and every method below hands that pool
//     to the free function in operator.go of the same name, which owns the statement.
//     Exported Exec/QueryRow would let any package holding this value send SQL text
//     of its own on the operator's connection, beside the statements whose
//     bound-parameters-only rule TestOperatorSQL_OnlyBoundParameters reads; without
//     them such code does not compile (measured: "has no field or method Exec"). The
//     SQL is written once, in operator.go (TestOperatorDB_EveryMethodDelegatesVerbatim).
//
//   - NO DSN AND NO PASSWORD IN A FIELD. The pool is held by pointer and nothing else
//     is held. OP-6 measured that a PLAIN field of a Store implementation is printed on
//     fmt's reflection paths (m10-platform.md, OP-4 block, OP-7 rule (4));
//     TestOperatorDB_PrintsNoConnectionString renders this type -- one built by the
//     production constructor included -- through fmt's verbs, slog and encoding/json and
//     finds neither the DSN nor its password.
//
// It satisfies internal/operatorauth's Store and internal/handler/operator's LegalStore,
// TenantStore and PlaqueStore (none can be imported here -- both packages import this
// one -- so the external test asserts it, and cmd/tappa's wiring does not compile without
// it). Its method set is those four interfaces' and Close, derived from them by
// TestOperatorDB_IsTheStoreAndNothingMore.
type OperatorDB struct {
	pool *pgxpool.Pool
}

// The pool is an OperatorConn; that is what the methods hand to operator.go.
var _ OperatorConn = (*pgxpool.Pool)(nil)

// operatorRole is the one role this pool may run as (ADR 0021 §1).
const operatorRole = "tappa_operator"

// operatorRoleQuery adds to roleFactsQuery (shared with the customer pool, so the two
// gates cannot measure privilege differently) the two facts only this gate reads.
//
// 🔴 session_user, BECAUSE current_user ALONE WAS MEASURED TO BE FORGEABLE FROM THE
// DSN. A startup parameter `role=tappa_operator` on the OWNER's DSN (pgx sends any
// unknown query parameter as one) gave session_user=tappa_owner, current_user=
// tappa_operator (dev, 2026-10-01): a superuser session that is one SET ROLE NONE away
// from everything, wearing the operator's name (RESET ROLE is not the way out -- it
// returns to the startup parameter; SET ROLE NONE was measured to reach the superuser). roleFactsQuery reads current_user and
// would have passed it. So this gate requires BOTH to be tappa_operator.
//
// member_of_any_role, BECAUSE ADR 0021 §1 SAYS "MEMBER OF NO ROLE", not "member of no
// privileged role": tappa_operator as a member of tappa_app would inherit tappa_app's
// grants on every tenant table. roleFactsQuery's InheritsPrivilege sees only privileged
// parents.
//
// has_members, rolcreatedb, rolcreaterole, rolreplication (OP-7, 2nd round): the rest of
// ADR 0021 §1's description of the role -- NOCREATEDB, NOCREATEROLE, the NOREPLICATION
// that 01-roles.sql writes, and "no member" (a member of tappa_operator inherits EXECUTE
// on every op_*). 00026's precondition checks them when the migration runs; this reads
// them again each time the pool opens, on the session that will be used.
//
// Qualified with pg_catalog throughout, operators included, for roleFactsQuery's reason
// (2c; pool.go).
const operatorRoleQuery = `
	SELECT session_user,
	       EXISTS (SELECT 1
	                 FROM pg_catalog.pg_auth_members m
	                 JOIN pg_catalog.pg_roles mr ON mr.oid OPERATOR(pg_catalog.=) m.member
	                WHERE mr.rolname OPERATOR(pg_catalog.=) ANY (ARRAY[session_user, current_user])),
	       EXISTS (SELECT 1
	                 FROM pg_catalog.pg_auth_members m
	                 JOIN pg_catalog.pg_roles pr ON pr.oid OPERATOR(pg_catalog.=) m.roleid
	                WHERE pr.rolname OPERATOR(pg_catalog.=) ANY (ARRAY[session_user, current_user])),
	       r.rolcreatedb,
	       r.rolcreaterole,
	       r.rolreplication
	  FROM pg_catalog.pg_roles r
	 WHERE r.rolname OPERATOR(pg_catalog.=) current_user`

// operatorRoleFacts is what the gate measured about the connection it opened.
type operatorRoleFacts struct {
	RoleFacts        // current_user and its reach past RLS (roleFactsQuery), and session_user
	MemberOfAny bool // session_user or current_user is a member of some role
	HasMembers  bool // some role is a member of session_user or current_user
	CreateDB    bool // current_user: rolcreatedb
	CreateRole  bool // current_user: rolcreaterole
	Replication bool // current_user: rolreplication
}

// NewOperatorDB opens tappa_operator's pool from cfg.OperatorDatabaseURL, pins
// log_parameter_max_length_on_error to 0 on each connection it opens (logparams.go;
// measured through the test hook, at open and on a later connection:
// TestPin_AConnectionTheParameterDidNotReachIsRefused), and REFUSES to hand back a pool
// that is not tappa_operator as ADR 0021 §1 describes it
// (TestOperatorDB_RefusesEveryRoleButTappaOperator).
//
// 🔴 THE ROLE GATE REFUSES IN EVERY ENVIRONMENT, NOT ONLY IN PRODUCTION -- a deliberate
// difference from the customer pool's roleRefusal, which only warns outside production.
// That warning exists because a developer legitimately runs migrations, seeds and psql
// as the owner; nothing legitimate runs the OPERATOR surface as anything but
// tappa_operator (the development role gets a password from
// scripts/db-init/02-dev-only-password.sh, or the step in deploy/README.md), so the
// argument for warning does not carry over and the refusal is the whole rule. Production
// refuses, as the card asks; development refuses too.
//
// Errors name the step and a code -- a SQLSTATE, an errno, a context sentinel, a Go
// type -- and never the DSN, its password or pgx's own message text, which for a parse
// failure quotes the connection string through a redactor that misses a password
// given as a query parameter (pgconn's redactPW reads only the user-info part of a
// URL; TestOperatorDB_RefusalsCarryNoConnectionString measures it). A ping that could
// not REACH the server -- the closed list connectFailure and unreachableSQLSTATEs
// define -- wraps ErrOperatorUnreachable; every other refusal does not.
func NewOperatorDB(ctx context.Context, cfg *config.Config) (*OperatorDB, error) {
	return openOperatorDB(ctx, cfg.OperatorDatabaseURL, nil)
}

// openOperatorDB is NewOperatorDB with the per-connection hook pinLogParameters runs
// first. Production passes nil. This package's tests pass a hook that switches the
// owner's connection to tappa_operator (SET SESSION AUTHORIZATION, which sets BOTH
// identities the gate reads), because tappa_operator is born NOLOGIN; every other step
// -- the pin, the read-back, the role gate -- is the production path.
func openOperatorDB(ctx context.Context, dsn string, before func(context.Context, *pgx.Conn) error) (*OperatorDB, error) {
	if dsn == "" {
		return nil, errors.New("db: operator pool: TAPPA_OPERATOR_DATABASE_URL is empty; a process with no operator " +
			"configuration does not open this pool at all")
	}
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("db: operator pool: TAPPA_OPERATOR_DATABASE_URL is not a valid PostgreSQL connection " +
			"string (the parser's message is not repeated: it quotes the value)")
	}
	if err := requireBoundParameters(poolCfg.ConnConfig, "TAPPA_OPERATOR_DATABASE_URL", true); err != nil {
		return nil, err
	}
	pinLogParameters(poolCfg, "TAPPA_OPERATOR_DATABASE_URL", before)
	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, operatorStepErr("build the pool", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, operatorConnectErr(err)
	}
	facts, err := readOperatorRole(ctx, pool)
	if err != nil {
		pool.Close()
		return nil, err
	}
	if err := operatorRoleRefusal(facts); err != nil {
		pool.Close()
		return nil, err
	}
	return &OperatorDB{pool: pool}, nil
}

// readOperatorRole runs the two role statements on ONE connection, so the facts
// describe the same session.
//
// Nothing here is "unreachable": the server was reached (the ping succeeded), so a
// failure from now on fails CLOSED and stops the boot (operatorStepErr), whatever its
// SQLSTATE -- TestReadOperatorRole_AFailureIsNeverUnreachability drives a class-08 code
// through this exact step.
func readOperatorRole(ctx context.Context, pool *pgxpool.Pool) (operatorRoleFacts, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return operatorRoleFacts{}, operatorStepErr("read its role", err)
	}
	defer conn.Release()
	return readOperatorRoleOn(ctx, conn)
}

// rowQuerier is the one method readOperatorRoleOn needs of a connection.
type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func readOperatorRoleOn(ctx context.Context, q rowQuerier) (operatorRoleFacts, error) {
	var f operatorRoleFacts
	if err := q.QueryRow(ctx, roleFactsQuery).Scan(&f.User, &f.Super, &f.BypassRLS, &f.OwnsScopedTable, &f.InheritsPrivilege); err != nil {
		return operatorRoleFacts{}, operatorStepErr("read its role", err)
	}
	if err := q.QueryRow(ctx, operatorRoleQuery).Scan(&f.Session, &f.MemberOfAny, &f.HasMembers,
		&f.CreateDB, &f.CreateRole, &f.Replication); err != nil {
		return operatorRoleFacts{}, operatorStepErr("read its role", err)
	}
	return f, nil
}

// operatorRoleRefusal is the gate: nil only for a session that is tappa_operator on
// both identities, reaches past no row level security, is a member of nothing, has no
// member, and holds none of CREATEDB, CREATEROLE, REPLICATION.
// The message names the two role names and the measured attributes -- configuration
// the server reported, the customer pool's roleRefusal precedent -- and nothing else.
func operatorRoleRefusal(f operatorRoleFacts) error {
	if f.Session == operatorRole && f.User == operatorRole && !f.Privileged() && !f.MemberOfAny &&
		!f.HasMembers && !f.CreateDB && !f.CreateRole && !f.Replication {
		return nil
	}
	return fmt.Errorf("db: operator pool: refusing to open: TAPPA_OPERATOR_DATABASE_URL must sign in as %s and the "+
		"session must stay %s -- NOSUPERUSER, NOBYPASSRLS, NOCREATEDB, NOCREATEROLE, NOREPLICATION, owner of no "+
		"row-security table, member of no role and with no member (ADR 0021 §1) -- and it is session_user=%q "+
		"current_user=%q rolsuper=%v rolbypassrls=%v owns_or_can_become_owner_of_an_rls_table=%v "+
		"member_of_a_superuser_or_bypassrls_role=%v member_of_any_role=%v has_members=%v rolcreatedb=%v "+
		"rolcreaterole=%v rolreplication=%v",
		operatorRole, operatorRole, f.Session, f.User, f.Super, f.BypassRLS, f.OwnsScopedTable, f.InheritsPrivilege,
		f.MemberOfAny, f.HasMembers, f.CreateDB, f.CreateRole, f.Replication)
}

// ErrOperatorUnreachable marks a failure to USE the operator's database at start-up
// that is not a refusal of this process -- the network, name resolution, a timeout, a
// cancelled start-up, and the server answers in unreachableSQLSTATEs. cmd/tappa answers
// it by keeping the customer product up and the operator surface unavailable, instead of
// refusing to boot. The case it was decided on: after a restore onto a fresh cluster,
// 01-roles.sql creates tappa_operator NOLOGIN again and no dump carries a role password,
// so a configured DSN fails authentication (28P01) -- with a fatal rule that would be a
// customer outage caused by the operator's credential.
//
// EVERYTHING ELSE IS A REFUSAL AND STOPS THE BOOT (2nd round, the 1st auditor measured
// the earlier "every SQLSTATE at connect" rule letting five of them through): a server
// that answered 42501 (the DSN asks for a role or a setting it may not have), 22023 (a
// role that does not exist), 42704 (an unknown setting) or any other code outside the
// table refused WHAT this process asked for; so do the role gate and the pin's read-back.
// A failure this package does not RECOGNISE is a refusal too -- a TLS alert, a
// certificate this side would not accept, a server that declines TLS -- and so is a
// connection made of several attempts (a multi-host DSN, sslmode=prefer) of which even
// ONE was not unreachable (2b: connectFailure).
var ErrOperatorUnreachable = errors.New("operator database unreachable")

// unreachableSQLSTATEs is the CLOSED list of server answers at connect time that count
// as unreachability. A two-character entry is a whole SQLSTATE class; five characters,
// one code. TestOperatorConnectErr_OnlyTheTableIsUnreachable pins it from a literal
// copy, in both directions.
var unreachableSQLSTATEs = []struct{ code, why string }{
	{"08", "connection exception: the connection itself failed or was lost"},
	{"28", "invalid authorization: the credential was not accepted -- 28P01 is the restore case " +
		"(a fresh cluster's role without its password), 28000 a pg_hba rejection or a NOLOGIN role"},
	{"3D000", "invalid catalog name: the database the DSN names does not exist on that server"},
	{"53300", "too many connections: the server is full at this moment"},
	{"57P01", "admin shutdown: the server is stopping"},
	{"57P02", "crash shutdown: the server is going down after a crash"},
	{"57P03", "cannot connect now: the server is starting up or recovering"},
}

func unreachableSQLSTATE(code string) bool {
	for _, u := range unreachableSQLSTATEs {
		if code == u.code || (len(u.code) == 2 && len(code) == 5 && code[:2] == u.code) {
			return true
		}
	}
	return false
}

// connectOutcome is what connectFailure makes of an error: a CODE for the message, and
// three facts about it.
type connectOutcome struct {
	// reason is a SQLSTATE, a context outcome, an errno (a fixed string from the OS,
	// such as "connection refused"), a name-resolution, network or TLS failure, or else
	// a Go type -- never text from pgx or from the server: a server's message echoes the
	// names and values the DSN sent (a role name, a setting's value), and pgx's messages
	// are a denylist away from a credential (NewOperatorDB's doc comment).
	reason      string
	unreachable bool
	// server: the attempt that decided the outcome was a SQLSTATE, i.e. the server
	// answered. A refusal says "the server refused" only then; a TLS or certificate
	// failure may have been refused by THIS side (an untrusted certificate), and an
	// unrecognised error by anyone.
	server bool
	ctxErr error
}

// connectFailure classifies a failure to connect.
//
// ONE ERROR CAN BE SEVERAL ATTEMPTS, AND ALL OF THEM DECIDE (2b, the 2nd auditor's
// measurement). pgx tries every host of a multi-host DSN, and both halves of
// sslmode=prefer or allow, and returns the attempts' errors JOINED (pgconn's
// ConnectConfig: errors.Join). errors.As over a join answers the FIRST match anywhere in
// it, so a closed port beside a server's refusal, a TLS alert, a declined TLS request or
// a rejected certificate read as the closed port -- unreachable, and the boot went on.
// So the error is split into its attempts (connectAttempts) and it is unreachability
// ONLY when EVERY attempt is; the first attempt that is not makes the whole a refusal,
// and the reason names it. A single error is one attempt. When every attempt was
// unreachable the reason lists each one's code in order -- "attempt 1: connection
// refused; attempt 2: SQLSTATE 28P01" -- because they can differ and the first is not
// the whole story (2d, the closing auditor: a closed port beside a wrong password said
// only "connection refused").
//
// Within ONE attempt the order of the cases below cannot change an answer for the errors
// pgx builds: a SQLSTATE and an errno are both the END of an unwrap chain (neither type
// unwraps), so one linear chain holds at most one of them; joins, the only way to hold
// both, are split before this point.
func connectFailure(err error) connectOutcome {
	attempts := connectAttempts(err)
	var all connectOutcome
	var each []string
	for i, a := range attempts {
		o := classifyAttempt(a)
		if !o.unreachable {
			if len(attempts) > 1 {
				o.reason = fmt.Sprintf("%s; attempt %d of %d", o.reason, i+1, len(attempts))
			}
			return o
		}
		if i == 0 {
			all = o
		}
		if all.ctxErr == nil {
			all.ctxErr = o.ctxErr
		}
		each = append(each, fmt.Sprintf("attempt %d: %s", i+1, o.reason))
	}
	if len(attempts) > 1 {
		all.reason = strings.Join(each, "; ")
	}
	return all
}

// connectAttempts returns the attempts err stands for: the leaves of every multi-error
// (errors.Join, fmt.Errorf with several %w) on its unwrap chain, in order and depth
// first. An error with no multi-error on its chain is one attempt and is returned whole.
func connectAttempts(err error) []error {
	for e := err; e != nil; {
		switch u := e.(type) {
		case interface{ Unwrap() []error }:
			var out []error
			for _, c := range u.Unwrap() {
				if c != nil {
					out = append(out, connectAttempts(c)...)
				}
			}
			if len(out) == 0 {
				return []error{err}
			}
			return out
		case interface{ Unwrap() error }:
			e = u.Unwrap()
		default:
			return []error{err}
		}
	}
	return []error{err}
}

// classifyAttempt classifies ONE connection attempt.
func classifyAttempt(err error) connectOutcome {
	var pg *pgconn.PgError
	var op *net.OpError
	var cert *tls.CertificateVerificationError
	var errno syscall.Errno
	var dns *net.DNSError
	var netErr net.Error
	switch {
	case errors.As(err, &pg):
		return connectOutcome{reason: "SQLSTATE " + pg.Code, unreachable: unreachableSQLSTATE(pg.Code), server: true}
	case errors.Is(err, context.DeadlineExceeded):
		return connectOutcome{reason: "timed out", unreachable: true, ctxErr: context.DeadlineExceeded}
	case errors.Is(err, context.Canceled):
		return connectOutcome{reason: "cancelled", unreachable: true, ctxErr: context.Canceled}
	case errors.As(err, &op) && (op.Op == "remote error" || op.Op == "local error"):
		// crypto/tls reports a TLS alert as a *net.OpError with exactly these two Ops: a
		// "remote error" is the peer's alert (a server that requires a client
		// certificate, measured by the 2nd auditor), a "local error" one this side sent.
		// Something answered and the negotiation failed; it is not the network, although
		// the type is a net.Error and the case below would have taken it.
		return connectOutcome{reason: "a TLS alert (" + op.Op + ")"}
	case errors.As(err, &cert):
		return connectOutcome{reason: "this side did not accept the server's TLS certificate"}
	case errors.As(err, &errno):
		return connectOutcome{reason: errno.Error(), unreachable: true}
	case errors.As(err, &dns):
		return connectOutcome{reason: "the database host name does not resolve", unreachable: true}
	case errors.As(err, &netErr):
		return connectOutcome{reason: "network error", unreachable: true}
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return connectOutcome{reason: "the server closed the connection", unreachable: true}
	default:
		return connectOutcome{reason: fmt.Sprintf("an error of type %T", innermost(err))}
	}
}

// innermost is the end of err's unwrap chain: its type names the failure more closely
// than pgx's wrappers do (a declined TLS request is an *errors.errorString inside them).
func innermost(err error) error {
	for {
		next := errors.Unwrap(err)
		if next == nil {
			return err
		}
		err = next
	}
}

// operatorConnectErr is the PING's error -- the one step at which unreachability can
// exist. The per-connection checks' three refusals (the setting is not 0; reading it
// failed; the connection's query exec mode interpolates) pass through intact -- they
// carry only the server's value, a SQLSTATE or a mode's name -- and are not
// unreachability.
func operatorConnectErr(err error) error {
	var notPinned *logParameterNotPinnedError
	if errors.As(err, &notPinned) {
		return fmt.Errorf("db: operator pool: %w", notPinned)
	}
	var readFailed *logParameterReadError
	if errors.As(err, &readFailed) {
		return fmt.Errorf("db: operator pool: %w", readFailed)
	}
	var modeRefused *execModeRefusedError
	if errors.As(err, &modeRefused) {
		return modeRefused
	}
	o := connectFailure(err)
	switch {
	case !o.unreachable && o.server:
		return fmt.Errorf("db: operator pool: the server refused the connection (%s)", o.reason)
	case !o.unreachable:
		return fmt.Errorf("db: operator pool: the connection attempt failed (%s), which is not one of the "+
			"failures that count as unreachable", o.reason)
	case o.ctxErr != nil:
		return fmt.Errorf("db: operator pool: cannot connect (%s): %w: %w", o.reason, ErrOperatorUnreachable, o.ctxErr)
	default:
		return fmt.Errorf("db: operator pool: cannot connect (%s): %w", o.reason, ErrOperatorUnreachable)
	}
}

// operatorStepErr is every OTHER step's error: the same code-only reason, and NEVER
// unreachability -- a step after the ping ran on a server that was reached, and a
// failure there fails closed.
func operatorStepErr(step string, err error) error {
	o := connectFailure(err)
	if o.ctxErr != nil {
		return fmt.Errorf("db: operator pool: cannot %s (%s): %w", step, o.reason, o.ctxErr)
	}
	return fmt.Errorf("db: operator pool: cannot %s (%s)", step, o.reason)
}

// Close drains and closes the pool. cmd/tappa defers it for the process's lifetime.
func (o *OperatorDB) Close() { o.pool.Close() }

// The seven methods below are internal/operatorauth's Store. Each is ONE statement on
// the pool and hands its arguments, in order, to operator.go's function of the same
// name -- which owns the SQL, the bound parameters and the error contract. (The ones
// after them are the legal screen's, the tenant screens' and the plaque screen's,
// delegated the same way; LegalVersions, TenantList, TenantDetail and TenantPlaques are
// two statements each.)

// OperatorByEmail is the login lookup (operator.go).
func (o *OperatorDB) OperatorByEmail(ctx context.Context, email string) (OperatorAccount, error) {
	return OperatorByEmail(ctx, o.pool, email)
}

// OperatorByID is the TOTP step's lookup (operator.go).
func (o *OperatorDB) OperatorByID(ctx context.Context, id uuid.UUID) (OperatorAccount, error) {
	return OperatorByID(ctx, o.pool, id)
}

// RecordOperatorAuthEvent writes one pre-session failure row (operator.go).
func (o *OperatorDB) RecordOperatorAuthEvent(ctx context.Context, kind OperatorAuthEvent, email string, admin uuid.UUID) error {
	return RecordOperatorAuthEvent(ctx, o.pool, kind, email, admin)
}

// OpenOperatorSession is op_open_session (operator.go).
func (o *OperatorDB) OpenOperatorSession(ctx context.Context, admin uuid.UUID, sessionHash string, step int64) error {
	return OpenOperatorSession(ctx, o.pool, admin, sessionHash, step)
}

// CompleteOperatorEnrollment is op_complete_enrollment (operator.go).
func (o *OperatorDB) CompleteOperatorEnrollment(ctx context.Context, admin uuid.UUID, rawToken, digest string,
	sealed []byte, step int64, sessionHash string) error {
	return CompleteOperatorEnrollment(ctx, o.pool, admin, rawToken, digest, sealed, step, sessionHash)
}

// TouchOperatorSession is the session predicate (operator.go).
func (o *OperatorDB) TouchOperatorSession(ctx context.Context, sessionHash string) (OperatorSession, error) {
	return TouchOperatorSession(ctx, o.pool, sessionHash)
}

// CloseOperatorSession is op_close_session (operator.go).
func (o *OperatorDB) CloseOperatorSession(ctx context.Context, sessionHash string) error {
	return CloseOperatorSession(ctx, o.pool, sessionHash)
}

// The two methods below are internal/handler/operator's LegalStore (M10 OP-10, phase
// B): the /operator/legal screen's version list and publication. Same shape as the
// seven above -- one call to operator.go's function of the same name with the pool as
// its connection.

// LegalVersions is the version list's two-phase read (operator.go). On the POOL each of
// its two statements is an implicit transaction of its own, so phase one (op_begin_read,
// the audit row) has committed before phase two (op_read_legal_versions) runs --
// measured on a pool built by the production constructor:
// TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions.
func (o *OperatorDB) LegalVersions(ctx context.Context, sessionHash string, page LegalVersionsPage) ([]LegalVersion, error) {
	return LegalVersions(ctx, o.pool, sessionHash, page)
}

// PublishLegal is op_publish_legal (operator.go).
func (o *OperatorDB) PublishLegal(ctx context.Context, sessionHash, slug, body string) error {
	return PublishLegal(ctx, o.pool, sessionHash, slug, body)
}

// The two methods below are internal/handler/operator's TenantStore (M10 OP-11, phase
// B): the /operator/tenants screens' list (with its search) and one tenant's overview.
// Same shape as the nine above. Each is a two-phase read, and on the POOL its two
// statements are two implicit transactions -- the methods themselves are driven on a
// pool built by the production constructor in
// TestTenantList_OnThePoolTheTwoPhasesAreTwoTransactions.

// TenantList is the tenant list's two-phase read (operator.go).
func (o *OperatorDB) TenantList(ctx context.Context, sessionHash string, q TenantListQuery) ([]TenantSummary, error) {
	return TenantList(ctx, o.pool, sessionHash, q)
}

// TenantDetail is one tenant's overview, a two-phase read (operator.go).
func (o *OperatorDB) TenantDetail(ctx context.Context, sessionHash string, tenantID uuid.UUID) (TenantOverview, error) {
	return TenantDetail(ctx, o.pool, sessionHash, tenantID)
}

// The method below is internal/handler/operator's PlaqueStore (M10 OP-13, phase B): the
// /operator/tenants/{id}/plaques screen's one read. Same shape as the eleven above; on the
// POOL its two statements are two implicit transactions -- the method itself is driven on
// a pool built by the production constructor in
// TestTenantPlaques_OnThePoolTheTwoPhasesAreTwoTransactions.

// TenantPlaques is one tenant's plaque inventory, a two-phase read (operator.go).
func (o *OperatorDB) TenantPlaques(ctx context.Context, sessionHash string, tenantID uuid.UUID) (TenantPlaqueInventory, error) {
	return TenantPlaques(ctx, o.pool, sessionHash, tenantID)
}
