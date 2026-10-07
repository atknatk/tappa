package db

import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// The platform operator's database accessors (M10; ADR 0021 §2 vi, §4). Hand-written
// for the reason resolve.go gives -- sqlc v1.28 cannot type these calls -- and
// mirrored verbatim in db/queries/operator.sql, which is the canonical, `-- name:`-less
// document of every statement below.
//
// 🔴 WHERE THEY RUN: ON A tappa_operator CONNECTION, NEVER ON *DB. These are free
// functions over an OperatorConn rather than methods on the customer pool, and that
// is the structure, not a style: *DB is tappa_app's pool, which holds no EXECUTE on
// any op_* and no privilege on the four operator tables (00026), so a call through it
// fails loudly (42501) -- ADR 0021 §4's "iki DSN'in yer değiştirmesi iki yönde de
// gürültülü". The operator's own pool (db.OperatorDB) is OP-7's; it satisfies
// OperatorConn and delegates here. In tests the conn is a transaction that
// impersonates tappa_operator (SET LOCAL SESSION AUTHORIZATION), the identity
// PostgreSQL actually checks.
//
// NOTHING HERE CALLS set_config: no tenant context is produced or consumed (ADR 0021
// §3.6), and none of the five op_* born in 00026 touches a tenant table. The two reads
// 00029 adds (OP-11) and the one 00030 adds (OP-13) read tenant tables WITHOUT a tenant
// context: they cross the boundary as their BYPASSRLS owner, the one crossing ADR 0021
// permits, and their statements name the tenant themselves. 00031's read of the operator's
// own log (OP-14) reads one tenant column, the name of each tenant its page's rows name,
// by the value each row carries.
//
// THREE RULES THE OP-7 CARD NAMED, KEPT HERE BECAUSE THE SQL NOW LIVES HERE
// (m10-platform.md, "Kabullere bağlananlar — OP-7" (b), (c), (d)):
//
//	(b) every value is a BOUND PARAMETER. The statements are package constants with
//	    $n placeholders and no quoted literal (TestOperatorSQL_OnlyBoundParameters reads
//	    them); a value spliced into SQL text would reach the server log through
//	    log_statement / the error STATEMENT whatever log_parameter_max_length says
//	    (ADR 0021 "Karar verilmedi", condition 2).
//	(c) a *pgconn.PgError is NEVER returned or wrapped. Its Detail carries row values
//	    (00026's own history: "Failing row contains (...)" held a digest and an
//	    envelope), so operatorErr keeps the SQLSTATE and drops the rest -- errors.As
//	    cannot reach a PgError through anything this file returns.
//	(d) the statements themselves, which the OP-7 card had and OP-6 needed first
//	    (the OP-6 card correction says why they moved).

// OperatorConn is the connection an operator statement runs on: tappa_operator's
// pool (OP-7) or a transaction impersonating it. *pgxpool.Pool, *pgx.Conn and pgx.Tx
// all satisfy it. Query is for the op_read_* functions, which return rows (OP-10).
type OperatorConn interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ErrOperatorRefused is every op_* refusal: 00026 raises SQLSTATE 28000 for a dead
// session, a used or wrong token, a stale or poisoned TOTP step, a lock and a wrong
// status alike, with one message per function. Nothing else on these paths produces
// 28000, so a missing grant (42501) can never be mistaken for it.
var ErrOperatorRefused = errors.New("db: operator call refused")

// ErrNoOperator is the login lookup finding no ACTIVE operator. Pending, disabled and
// unknown are the same answer by construction: tappa_operator's only policy on
// platform_admins shows active rows (00026), and the statement filters on status as
// well (the belt; RLS is the braces).
var ErrNoOperator = errors.New("db: no active operator")

// OperatorAuthEvent is op_record_auth_event's CLOSED kind set (00026, ADR 0021 §1): the
// pre-session rows -- the five failures and, since 00031 (OP-14), OperatorPasswordOK. The
// database refuses any other value with 22023.
type OperatorAuthEvent string

const (
	OperatorLoginFailed      OperatorAuthEvent = "login_failed"
	OperatorUnknownEmail     OperatorAuthEvent = "unknown_email"
	OperatorTOTPFailed       OperatorAuthEvent = "totp_failed"
	OperatorLocked           OperatorAuthEvent = "locked"
	OperatorEnrollmentFailed OperatorAuthEvent = "enrollment_failed"
	// OperatorPasswordOK (00031): the password was accepted and the second factor is still
	// to come. Not a failure and not a success of the sign-in (that is op_open_session's
	// 'login' row). The database takes it by account id ONLY -- an address, or no id, is
	// 22023 -- and it moves no lock counter. Its writer is OP-14 phase C.
	OperatorPasswordOK OperatorAuthEvent = "password_ok"
)

// OperatorAccount is what the login needs of an ACTIVE operator: the digest to
// compare, the sealed TOTP secret to open, and the lock stamp to REPORT (the lock is
// DECIDED inside op_open_session's UPDATE -- ADR 0020 §3; nothing in Go reads this
// field to refuse anything).
//
// Both credentials travel wrapped: a bcrypt digest in a log is an offline cracking
// target (PasswordHash's own argument), and the envelope, though useless without
// TAPPA_OPERATOR_TOTP_KEK, is on ADR 0020 §5's never-log list.
type OperatorAccount struct {
	ID          uuid.UUID
	Digest      PasswordHash
	Sealed      SealedSecret
	LockedUntil *time.Time
}

// OperatorSession is op_touch_session's answer: the session and the operator it
// resolves to. Never the hash (ADR 0021 §1: the session's hash is seen by the definer
// and returned by nothing).
type OperatorSession struct {
	SessionID uuid.UUID
	AdminID   uuid.UUID
}

// The statements, mirrored in db/queries/operator.sql.
//
// The two lookups read the table directly: tappa_operator holds a column SELECT on
// platform_admins for exactly this (ADR 0021 §1, sınır 11), under one policy that
// shows active rows. `status = 'active'` is written AGAIN in the statement -- the
// belt beside RLS's braces (CLAUDE.md §4.5's shape, applied to a table no tenant
// owns) -- and the citext equality is schema-qualified so a pinned search_path cannot
// turn it case-sensitive (ADR 0002's M6-01 trap).
//
// ⚠️ The literal 'active' is the one quoted value in this file, and it is a constant
// of the schema, not a caller's value; TestOperatorSQL_OnlyBoundParameters names it.
const (
	operatorByEmailSQL = `SELECT id, password_hash, totp_secret_sealed, totp_locked_until
FROM public.platform_admins
WHERE email OPERATOR(public.=) $1::public.citext AND status = 'active'`

	operatorByIDSQL = `SELECT id, password_hash, totp_secret_sealed, totp_locked_until
FROM public.platform_admins
WHERE id = $1 AND status = 'active'`

	recordOperatorAuthEventSQL = `SELECT public.op_record_auth_event($1, $2, $3)`

	openOperatorSessionSQL = `SELECT public.op_open_session($1, $2, $3)`

	completeOperatorEnrollmentSQL = `SELECT public.op_complete_enrollment($1, $2, $3, $4, $5, $6)`

	touchOperatorSessionSQL = `SELECT session_id, admin_id FROM public.op_touch_session($1)`

	closeOperatorSessionSQL = `SELECT public.op_close_session($1)`

	// OP-10 (migration 00027). The read kind and its parameters are bound values like
	// everything else; $3 is the parameters' JSON text, cast on the server.
	beginOperatorReadSQL = `SELECT public.op_begin_read($1, $2, $3::jsonb)`

	readLegalVersionsSQL = `SELECT version_id, slug, published_at, body_bytes, publisher_kind,
       publisher_admin_id, publisher_name, is_current
FROM public.op_read_legal_versions($1, $2, $3, $4)`

	publishLegalSQL = `SELECT public.op_publish_legal($1, $2, $3)`

	// OP-11 (migration 00029). Phase one of both is beginOperatorReadSQL above.
	readTenantsSQL = `SELECT tenant_id, tenant_name, created_at, plan
FROM public.op_read_tenants($1, $2, $3, $4, $5)`

	readTenantDetailSQL = `SELECT tenant_id, tenant_name, created_at, plan, business_type,
       location_count, active_employee_count, active_plaque_count, active_admin_count
FROM public.op_read_tenant_detail($1, $2, $3)`

	// OP-13 (migration 00030). Phase one is beginOperatorReadSQL above.
	readTenantPlaquesSQL = `SELECT tenant_id, tenant_name, uid, status, location_id, location_name,
       encoded_at, created_at, retired_at, replaced_by, last_ctr, plaque_count
FROM public.op_read_tenant_plaques($1, $2, $3)`

	// OP-14 (migration 00031). Phase one is beginOperatorReadSQL above.
	readOperatorAuditSQL = `SELECT audit_id, at, kind, session_id, actor_admin_id, actor_name,
       target_admin_id, target_admin_name, target_tenant_id, target_tenant_name,
       target_scope, page_number, page_size, search_class, filter_kind, legal_slug,
       legal_bytes, detail_recognised
FROM public.op_read_audit($1, $2, $3, $4, $5)`
)

// maxOperatorEmailBytes is 00026's CHECK on platform_admins.email (254). A longer
// address cannot be an operator's, so it is answered without a round trip -- and
// without handing the server a megabyte to cast to citext.
const maxOperatorEmailBytes = 254

// storableAddress reports whether PostgreSQL can hold email as text at all: valid
// UTF-8 (the server encoding) and no NUL byte. Anything else cannot be an operator's
// address, and sending it is not a lookup but an error -- measured (OP-6 verification,
// 2026-09-30): "a\x00b@…" and "a\xffb@…" came back as SQLSTATE 22021, and the password
// step answered a database error with NO bcrypt and NO row, outside ADR 0020 §3's
// "same answer, same time" set. So such an address takes the over-long address's path:
// no round trip, ErrNoOperator, and the caller pays the dummy comparison and writes its
// unknown_email row like for any other unknown address.
func storableAddress(email string) bool {
	return utf8.ValidString(email) && !strings.ContainsRune(email, 0)
}

// OperatorByEmail is the login lookup: the ACTIVE operator with this address
// (case-insensitive), or ErrNoOperator.
func OperatorByEmail(ctx context.Context, c OperatorConn, email string) (OperatorAccount, error) {
	if email == "" || len(email) > maxOperatorEmailBytes || !storableAddress(email) {
		return OperatorAccount{}, ErrNoOperator
	}
	return scanOperator(c.QueryRow(ctx, operatorByEmailSQL, email), "operator by email")
}

// OperatorByID is the TOTP step's lookup: the account the signed login challenge
// names, if it is still ACTIVE.
func OperatorByID(ctx context.Context, c OperatorConn, id uuid.UUID) (OperatorAccount, error) {
	return scanOperator(c.QueryRow(ctx, operatorByIDSQL, id), "operator by id")
}

func scanOperator(row pgx.Row, what string) (OperatorAccount, error) {
	var (
		a      OperatorAccount
		digest *string
		sealed []byte
	)
	if err := row.Scan(&a.ID, &digest, &sealed, &a.LockedUntil); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return OperatorAccount{}, ErrNoOperator
		}
		return OperatorAccount{}, operatorErr(what, err)
	}
	// 00026's CHECK makes an active row without both credentials unrepresentable;
	// should one ever appear it is treated as absent, so the caller takes the same
	// dummy-comparison path as for an unknown address.
	if digest == nil || len(sealed) == 0 {
		return OperatorAccount{}, ErrNoOperator
	}
	a.Digest = NewPasswordHash(*digest)
	a.Sealed = NewSealedSecret(sealed)
	clear(sealed)
	return a, nil
}

// RecordOperatorAuthEvent writes ONE pre-session row through op_record_auth_event (a
// failure, or OperatorPasswordOK). The address (if any) and the account id (if any) are
// only looked up by the definer; neither is stored (ADR 0021 §1). An empty email, one no
// operator can have (over-long or not storable as text) and uuid.Nil are sent as NULL.
func RecordOperatorAuthEvent(ctx context.Context, c OperatorConn, kind OperatorAuthEvent, email string, admin uuid.UUID) error {
	var e, a any
	if email != "" && len(email) <= maxOperatorEmailBytes && storableAddress(email) {
		e = email
	}
	if admin != uuid.Nil {
		a = admin
	}
	_, err := c.Exec(ctx, recordOperatorAuthEventSQL, string(kind), e, a)
	return operatorErr("record operator auth event", err)
}

// OpenOperatorSession is op_open_session: advance the TOTP step, check the lock, and
// give birth to an MFA-stamped session and its 'login' row -- one statement. A
// refusal (replayed or stale step, lock, not active) is ErrOperatorRefused.
func OpenOperatorSession(ctx context.Context, c OperatorConn, admin uuid.UUID, sessionHash string, step int64) error {
	_, err := c.Exec(ctx, openOperatorSessionSQL, admin, sessionHash, step)
	return operatorErr("open operator session", err)
}

// CompleteOperatorEnrollment is op_complete_enrollment. rawToken is the enrollment
// token AS IT TRAVELLED in the link; the definer hashes it (keyless SHA-256 of its
// UTF-8 text, 00026) and consumes it in the same statement that writes the
// credentials and opens the first session. One refusal for every reason.
func CompleteOperatorEnrollment(ctx context.Context, c OperatorConn, admin uuid.UUID, rawToken, digest string,
	sealed []byte, step int64, sessionHash string) error {
	_, err := c.Exec(ctx, completeOperatorEnrollmentSQL, admin, rawToken, digest, sealed, step, sessionHash)
	return operatorErr("complete operator enrollment", err)
}

// TouchOperatorSession is THE session predicate (ADR 0021 §2 i): alive (8 h absolute,
// 30 min idle, both by the database's wall clock), MFA-stamped, not revoked, operator
// active -- and last_used_at advanced in the same statement. Anything else is
// ErrOperatorRefused.
func TouchOperatorSession(ctx context.Context, c OperatorConn, sessionHash string) (OperatorSession, error) {
	var s OperatorSession
	if err := c.QueryRow(ctx, touchOperatorSessionSQL, sessionHash).Scan(&s.SessionID, &s.AdminID); err != nil {
		return OperatorSession{}, operatorErr("touch operator session", err)
	}
	return s, nil
}

// CloseOperatorSession is op_close_session: revoke through the touch predicate and
// write the 'logout' row in one statement. A dead session is ErrOperatorRefused.
func CloseOperatorSession(ctx context.Context, c OperatorConn, sessionHash string) error {
	_, err := c.Exec(ctx, closeOperatorSessionSQL, sessionHash)
	return operatorErr("close operator session", err)
}

// ------------------------------------------------------------------ OP-10 --
//
// The legal texts on the operator's surface (migration 00027; ADR 0020 §7, ADR 0021
// §2 v). Two exported calls -- a version list and a publication -- and, unexported,
// the two phases the version list is made of. They are free functions over an
// OperatorConn like the seven above; since OP-10 phase B *OperatorDB delegates to the
// two exported ones, as internal/handler/operator's LegalStore (the /operator/legal
// screen's interface), and TestOperatorDB_IsTheStoreAndNothingMore derives that type's
// method set from LegalStore and operatorauth.Store.

// legalVersionsReadKind is op_begin_read's read kind for the version list -- the one
// value of operator_read_tickets_kind_check (00027).
const legalVersionsReadKind = "legal_versions"

// LegalVersionsPage is a page of the version list: Number from 1, Size 1..200 (ADR 0021
// §2 iii's ceiling). The database refuses anything else (op_begin_read, 22023) before
// writing a row; nothing here re-checks it, so the rule lives in one place.
type LegalVersionsPage struct {
	Number int32
	Size   int32
}

// LegalPublisherKind says who published a version, as op_read_legal_versions answers.
type LegalPublisherKind string

const (
	// LegalPublishedByOperator: published_by is a platform_admins.id (op_publish_legal).
	LegalPublishedByOperator LegalPublisherKind = "operator"
	// LegalPublishedByLegacy: any other row -- the M7-06 panel's (a customer admin's id,
	// or none) or one the owner wrote. ADR 0020 §7: the screen says "tenant admin
	// (legacy)". The customer admin's id is not returned.
	LegalPublishedByLegacy LegalPublisherKind = "legacy"
)

// LegalVersion is one row of op_read_legal_versions -- a fixed column list (ADR 0021
// §2 ii). BodyBytes is the stored text's length in BYTES (octet_length: a letter that
// UTF-8 writes in two bytes counts two -- measured on a Maltese text by
// TestOpReadLegalVersions_PagesAreCappedAndOrdered); the text itself is not part of the
// list.
type LegalVersion struct {
	ID          uuid.UUID
	Slug        string
	PublishedAt time.Time
	BodyBytes   int32
	PublishedBy LegalPublisherKind
	// PublisherID and PublisherName are the operator's for an operator row, nil for a
	// legacy one.
	PublisherID   *uuid.UUID
	PublisherName *string
	// Current: this is the version the public page serves (the newest of its slug).
	Current bool
}

// legalVersionsParams is the version list's parameter object as op_begin_read takes
// it: exactly these two keys (00027). The database rebuilds the object it hashes from
// the typed values, so this spelling is not what binds the ticket.
type legalVersionsParams struct {
	PageNumber int32 `json:"page_number"`
	PageSize   int32 `json:"page_size"`
}

// LegalVersions is the two-phase read of ADR 0021 §2 v for the version list: phase one
// (op_begin_read) writes the read's audit row and a ticket and must COMMIT; phase two
// (op_read_legal_versions) consumes the ticket and returns the rows.
//
// 🔴 TWO TRANSACTIONS, AND THE CONNECTION DECIDES WHETHER THEY ARE TWO. On the pool
// (*pgxpool.Pool, which is how *OperatorDB holds its connection) each statement is an
// implicit transaction of its own, so phase one has committed before phase two starts
// -- measured on a pool built by the production constructor:
// TestLegalVersions_OnThePoolTheTwoPhasesAreTwoTransactions. On a pgx.Tx both phases
// run in the caller's transaction and the database refuses phase two (the ticket's
// transaction has not committed), which comes back as ErrOperatorRefused -- measured in
// the same test. ADR 0021 §4: "ikisini tek transaction'da birleştiren bir erişimci
// yazılamaz -- veritabanı zaten reddeder".
func LegalVersions(ctx context.Context, c OperatorConn, sessionHash string, page LegalVersionsPage) ([]LegalVersion, error) {
	params, err := json.Marshal(legalVersionsParams{PageNumber: page.Number, PageSize: page.Size})
	if err != nil {
		return nil, fmt.Errorf("db: legal versions: encode the page: %w", err)
	}
	t, err := beginOperatorRead(ctx, c, sessionHash, legalVersionsReadKind, params)
	if err != nil {
		return nil, err
	}
	return readLegalVersions(ctx, c, sessionHash, t, page)
}

// PublishLegal is op_publish_legal: one new version of one document and its
// 'legal_publish' audit row, in one statement. The publisher is the operator the
// session resolves to; there is no parameter for it. A dead session is
// ErrOperatorRefused; a document the database will not take (a slug outside the closed
// set, a blank body, more than 256 KiB) is a database error carrying SQLSTATE 22023.
// The caller refreshes the public snapshot afterwards (internal/domain/legal.Store.Refresh
// -- ADR 0020 §7, one replica).
func PublishLegal(ctx context.Context, c OperatorConn, sessionHash, slug, body string) error {
	_, err := c.Exec(ctx, publishLegalSQL, sessionHash, slug, body)
	return operatorErr("publish legal document", err)
}

// beginOperatorRead is op_begin_read: the RAW ticket of a read whose audit row the
// statement wrote. kind is a read kind (00027's closed set) and params that kind's
// parameter object as JSON.
func beginOperatorRead(ctx context.Context, c OperatorConn, sessionHash, kind string, params []byte) (readTicket, error) {
	var raw string
	if err := c.QueryRow(ctx, beginOperatorReadSQL, sessionHash, kind, string(params)).Scan(&raw); err != nil {
		return readTicket{}, operatorErr("begin operator read", err)
	}
	return readTicket{v: &raw}, nil
}

// readLegalVersions is op_read_legal_versions: consume the ticket (bound to the session,
// the read kind and these page values), then the page of versions.
func readLegalVersions(ctx context.Context, c OperatorConn, sessionHash string, t readTicket, page LegalVersionsPage) ([]LegalVersion, error) {
	rows, err := c.Query(ctx, readLegalVersionsSQL, sessionHash, t.reveal(), page.Number, page.Size)
	if err != nil {
		return nil, operatorErr("read legal versions", err)
	}
	defer rows.Close()
	var out []LegalVersion
	for rows.Next() {
		var (
			v    LegalVersion
			kind string
		)
		if err := rows.Scan(&v.ID, &v.Slug, &v.PublishedAt, &v.BodyBytes, &kind,
			&v.PublisherID, &v.PublisherName, &v.Current); err != nil {
			return nil, operatorErr("read legal versions", err)
		}
		v.PublishedBy = LegalPublisherKind(kind)
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, operatorErr("read legal versions", err)
	}
	return out, nil
}

// ------------------------------------------------------------------ OP-11 --
//
// The tenant list (with its search) and one tenant's overview on the operator's surface
// (migration 00029; ADR 0021 §2 v, §3.2). Two exported two-phase reads in LegalVersions'
// shape -- free functions over an OperatorConn; since OP-11 phase B *OperatorDB delegates
// to both, as internal/handler/operator's TenantStore (the /operator/tenants screens'
// interface), and TestOperatorDB_IsTheStoreAndNothingMore derives that type's method set
// from TenantStore, LegalStore and operatorauth.Store.
//
// These are the first operator reads of TENANT data, and inside the two functions no
// row level security applies (their owner is BYPASSRLS): what keeps one tenant's rows
// out of another's overview is the tenant filter each statement of
// op_read_tenant_detail carries, and what limits the list is its fixed column list and
// the 200-row ceiling (00029 section 4).

// The read kinds of 00029 -- members of operator_read_tickets_kind_check.
const (
	tenantsReadKind      = "tenants"
	tenantDetailReadKind = "tenant_detail"
)

// MaxTenantSearchRunes is the tenant list's bound on the search term, in characters
// (00029: the longest thing the term can usefully be is an e-mail address). TenantList
// refuses a longer term itself, before a round trip; op_begin_read refuses it again
// (SQLSTATE 22023) for a caller that is not TenantList. The two copies are held equal
// by TestTenantList_TheSearchTermMeetsTheSameBoundInGoAndSQL.
const MaxTenantSearchRunes = 254

// ErrTenantSearchRefused is TenantList's answer for a search term it will not send: one
// PostgreSQL cannot hold as text at all (invalid UTF-8, a NUL character -- it would fail
// inside the statement with an encoding error, 22021 or 22P05, not with op_begin_read's
// own refusal, and no name or address can contain it) or one longer than
// MaxTenantSearchRunes characters. It is decided here, without a round trip. For the long
// term the reason is the server's statement log: where statements are logged with their
// parameters (the development database, log_statement = all -- the OP-11 A security
// review measured it there) sending the term writes it to that log in full, only for
// op_begin_read to refuse it. (By the same mechanism a term within the bound reaches that
// log in development -- an inference from that measurement, the log was not read here;
// production logs no statements -- ADR 0021, "Karar verilmedi".) No row is written
// (nothing was read).
var ErrTenantSearchRefused = errors.New("db: tenant search term refused")

// ErrNoSuchTenant is TenantDetail's answer when the id names no tenant. It is NOT a
// refusal: phase one has committed the 'read' row naming that id before phase two looked
// (00029 section 4.2), so the trail already holds the attempt.
var ErrNoSuchTenant = errors.New("db: no such tenant")

// TenantListQuery is a page of the tenant list. Search is matched against a tenant's
// name (case-insensitive substring, metacharacter-free), an admin's address (exact,
// case-insensitive; only for a term containing '@') and the tenant's id (a whole
// hyphenated uuid); "" lists every tenant. The read's audit row records the term's class
// -- none, text, address or id -- never the term (00029 section 3). Number from 1, Size
// 1..200; Search at most MaxTenantSearchRunes characters. The database refuses any other
// page (op_begin_read, 22023) before writing a row.
type TenantListQuery struct {
	Search string
	Number int32
	Size   int32
}

// TenantSummary is one row of op_read_tenants -- a fixed column list (ADR 0021 §2 ii):
// what tells one tenant from another in a list, nothing more. Newest first.
type TenantSummary struct {
	ID        uuid.UUID
	Name      string
	CreatedAt time.Time
	Plan      string
}

// TenantOverview is op_read_tenant_detail's row: the tenant's identity for the banner of
// its screens (ADR 0020 §9) and four counts of what is live -- not the tenant's data.
// tenants.structure is deliberately not in it: nothing reads that column after sign-up
// (internal/handler's TestSignupStructure_DecidesNothingAfterSignUp), the operator's
// overview included.
type TenantOverview struct {
	ID           uuid.UUID
	Name         string
	CreatedAt    time.Time
	Plan         string
	BusinessType string
	// Locations counts every location; the other three count rows whose status is
	// 'active' (employees, plaques, panel accounts).
	Locations       int64
	ActiveEmployees int64
	ActivePlaques   int64
	ActiveAdmins    int64
}

// tenantsParams and tenantDetailParams are the two kinds' parameter objects as
// op_begin_read takes them: exactly these keys (00029). The database rebuilds the object
// it hashes from the typed values, so this spelling does not bind the ticket.
type tenantsParams struct {
	PageNumber int32  `json:"page_number"`
	PageSize   int32  `json:"page_size"`
	Query      string `json:"query"`
}

type tenantDetailParams struct {
	TenantID uuid.UUID `json:"tenant_id"`
}

// storableText reports whether PostgreSQL can hold s as text: valid UTF-8 (the server
// encoding) and no NUL. storableAddress's rule, for a search term.
func storableText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// TenantList is the two-phase read of the tenant list: op_begin_read writes the read's
// 'read' row -- the page and the term's class, never the term -- and a ticket bound to the
// session and to every parameter; op_read_tenants consumes it and returns the page. As
// with LegalVersions, the two phases are two transactions only on a pool; on a pgx.Tx the
// database refuses phase two and this returns ErrOperatorRefused. A dead session is
// ErrOperatorRefused; a term it will not send is ErrTenantSearchRefused; a page outside
// the database's bounds is a database error carrying 22023.
func TenantList(ctx context.Context, c OperatorConn, sessionHash string, q TenantListQuery) ([]TenantSummary, error) {
	if !storableText(q.Search) || utf8.RuneCountInString(q.Search) > MaxTenantSearchRunes {
		return nil, ErrTenantSearchRefused
	}
	params, err := json.Marshal(tenantsParams{PageNumber: q.Number, PageSize: q.Size, Query: q.Search})
	if err != nil {
		return nil, fmt.Errorf("db: tenant list: encode the page: %w", err)
	}
	t, err := beginOperatorRead(ctx, c, sessionHash, tenantsReadKind, params)
	if err != nil {
		return nil, err
	}
	return readTenants(ctx, c, sessionHash, t, q)
}

// TenantDetail is the two-phase read of one tenant's overview. An id that names no
// tenant is ErrNoSuchTenant -- after the 'read' row naming it has committed. A dead
// session is ErrOperatorRefused (and so is a pgx.Tx, as for TenantList).
func TenantDetail(ctx context.Context, c OperatorConn, sessionHash string, tenantID uuid.UUID) (TenantOverview, error) {
	params, err := json.Marshal(tenantDetailParams{TenantID: tenantID})
	if err != nil {
		return TenantOverview{}, fmt.Errorf("db: tenant detail: encode the parameters: %w", err)
	}
	t, err := beginOperatorRead(ctx, c, sessionHash, tenantDetailReadKind, params)
	if err != nil {
		return TenantOverview{}, err
	}
	return readTenantDetail(ctx, c, sessionHash, t, tenantID)
}

// readTenants is op_read_tenants: consume the ticket (bound to the session, the kind,
// the term and the page), then the page of tenants.
func readTenants(ctx context.Context, c OperatorConn, sessionHash string, t readTicket, q TenantListQuery) ([]TenantSummary, error) {
	rows, err := c.Query(ctx, readTenantsSQL, sessionHash, t.reveal(), q.Search, q.Number, q.Size)
	if err != nil {
		return nil, operatorErr("read tenants", err)
	}
	defer rows.Close()
	var out []TenantSummary
	for rows.Next() {
		var s TenantSummary
		if err := rows.Scan(&s.ID, &s.Name, &s.CreatedAt, &s.Plan); err != nil {
			return nil, operatorErr("read tenants", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, operatorErr("read tenants", err)
	}
	return out, nil
}

// readTenantDetail is op_read_tenant_detail: consume the ticket (bound to the session,
// the kind and the tenant id), then the overview -- one row, or none for an unknown id.
func readTenantDetail(ctx context.Context, c OperatorConn, sessionHash string, t readTicket, tenantID uuid.UUID) (TenantOverview, error) {
	var o TenantOverview
	err := c.QueryRow(ctx, readTenantDetailSQL, sessionHash, t.reveal(), tenantID).Scan(
		&o.ID, &o.Name, &o.CreatedAt, &o.Plan, &o.BusinessType,
		&o.Locations, &o.ActiveEmployees, &o.ActivePlaques, &o.ActiveAdmins)
	if errors.Is(err, pgx.ErrNoRows) {
		return TenantOverview{}, ErrNoSuchTenant
	}
	if err != nil {
		return TenantOverview{}, operatorErr("read tenant detail", err)
	}
	return o, nil
}

// ------------------------------------------------------------------ OP-13 --
//
// One tenant's plaque inventory on the operator's surface (migration 00030; ADR 0021 §2 v,
// §3.2; CLAUDE.md §4.7). One exported two-phase read in TenantDetail's shape -- a free
// function over an OperatorConn; since OP-13 phase B *OperatorDB delegates to it, as
// internal/handler/operator's PlaqueStore (the /operator/tenants/{id}/plaques screen's
// interface), and TestOperatorDB_IsTheStoreAndNothingMore derives that type's method set
// from PlaqueStore too.
//
// Inside op_read_tenant_plaques no row level security applies (its owner is BYPASSRLS):
// what keeps another tenant's plaques out is the tenant filter on each of its three table
// references, and what keeps the plaque KEYS out is the definer's column grant -- it holds
// no SELECT on aes_key_ref or app_key_ref; the forms measured to fail (42501) are listed in
// ADR 0021's "OP-13 uygulama notu", PART I -- a changed function body among those that fail
// on that grant. Three of the listed forms, the writes' RETURNING *, do not isolate it: they
// need a write privilege on tags too, which the definer does not hold either, and each of
// the two missing grants refuses them on its own (measured). No field below carries a key,
// a key's presence or anything computed from one.

// tenantPlaquesReadKind is 00030's read kind -- a member of operator_read_tickets_kind_check.
const tenantPlaquesReadKind = "tenant_plaques"

// MaxTenantPlaques is op_read_tenant_plaques' row ceiling: the body's LIMIT 200 (ADR 0021
// §2 iii). There is no paging (OP-13 decision K13-2); TenantPlaqueInventory.Total says how
// many plaques the tenant holds in all.
const MaxTenantPlaques = 200

// TenantPlaqueInventory is the one read's three answers: the tenant -- its name for the
// header of the screen (ADR 0020 §9) --, the fact that it exists (an id that names no tenant
// is ErrNoSuchTenant, never an empty inventory), and its plaques: the first
// MaxTenantPlaques of them in the tenant's own list order (stock first, then by uid) and
// Total, every plaque it holds, counted by the same statement.
type TenantPlaqueInventory struct {
	TenantID   uuid.UUID
	TenantName string
	Total      int64
	Plaques    []TenantPlaque
}

// Truncated reports whether the tenant holds more plaques than the read returned.
func (i TenantPlaqueInventory) Truncated() bool { return i.Total > int64(len(i.Plaques)) }

// TenantPlaque is one plaque: the columns of the tenant's own list (db/queries/tags.sql
// ListTagsForTenant) and the name of the location it is mounted at. Status is the
// database's value VERBATIM -- the function maps nothing -- and Shape is the closed reading
// of it.
type TenantPlaque struct {
	UID          string
	Status       string
	LocationID   *uuid.UUID
	LocationName *string
	// EncodedAt is the one record that the chip took its keys (ADR 0017 §5.1 step 9); nil
	// means the encode was never recorded.
	EncodedAt  *time.Time
	CreatedAt  time.Time
	RetiredAt  *time.Time
	ReplacedBy *string
	// LastCtr is the chip's read counter as last accepted. Read here, never written: the
	// advance is AdvanceTagCounter's one conditional statement (CLAUDE.md §4.4).
	LastCtr int32
}

// PlaqueShape is a plaque's state read from status, encoded_at and location together.
type PlaqueShape string

// The shapes. Six name the states the schema allows (tags_status_check's four values,
// the two in-service ones split by the encode stamp); PlaqueUnrecognised is everything
// else.
const (
	// PlaqueOnAWall: active, mounted, encode recorded.
	PlaqueOnAWall PlaqueShape = "on_a_wall"
	// PlaqueOnAWallNeverEncoded: active and mounted with NO encode stamp -- the A-1 shape
	// (backlog T75): the plaque takes taps although nothing records that it ever received
	// its keys.
	PlaqueOnAWallNeverEncoded PlaqueShape = "on_a_wall_never_encoded"
	// PlaqueInStock: unassigned (no wall), encode recorded -- ready to mount.
	PlaqueInStock PlaqueShape = "in_stock"
	// PlaqueInStockNotEncoded: unassigned with no encode stamp -- the panel refuses to
	// mount it (migration 00025).
	PlaqueInStockNotEncoded PlaqueShape = "in_stock_not_encoded"
	// PlaqueRetired: replaced; taps reject (ReplacedBy names the successor, if any).
	PlaqueRetired PlaqueShape = "retired"
	// PlaqueLost: reported lost; taps reject.
	PlaqueLost PlaqueShape = "lost"
	// PlaqueUnrecognised: a status the mapping does not name, or a combination the schema
	// forbids (an active or retired plaque with no location, an unassigned one with a
	// location). FAIL-CLOSED: such a row is neither dropped nor read as a neighbouring
	// state -- it is this, for the screen to show as unrecognised.
	PlaqueUnrecognised PlaqueShape = "unrecognised"
)

// Shape is the closed mapping. The status comparison is exact (no case folding, no
// trimming): the database's CHECK writes the four values in lower case, and anything
// else is not one of them.
func (p TenantPlaque) Shape() PlaqueShape {
	mounted := p.LocationID != nil
	switch p.Status {
	case "active":
		switch {
		case !mounted:
			return PlaqueUnrecognised
		case p.EncodedAt == nil:
			return PlaqueOnAWallNeverEncoded
		default:
			return PlaqueOnAWall
		}
	case "unassigned":
		switch {
		case mounted:
			return PlaqueUnrecognised
		case p.EncodedAt == nil:
			return PlaqueInStockNotEncoded
		default:
			return PlaqueInStock
		}
	case "retired":
		if !mounted {
			return PlaqueUnrecognised
		}
		return PlaqueRetired
	case "lost":
		return PlaqueLost
	}
	return PlaqueUnrecognised
}

// TenantPlaques is the two-phase read of one tenant's plaque inventory: op_begin_read
// writes the read's 'read' row (target_scope 'tenant_plaques', the tenant named) and a
// ticket bound to the session and the tenant; op_read_tenant_plaques consumes it and
// returns the inventory. An id that names no tenant is ErrNoSuchTenant -- after the 'read'
// row naming it has committed. A dead session is ErrOperatorRefused, and so is a pgx.Tx (the
// two phases are two transactions only on a pool, as for TenantList).
func TenantPlaques(ctx context.Context, c OperatorConn, sessionHash string, tenantID uuid.UUID) (TenantPlaqueInventory, error) {
	// The parameter object is the overview's, {tenant_id}: 00030 gives the kind 00029's
	// tenant_detail branch, and the KIND is what tells the two reads' tickets apart.
	params, err := json.Marshal(tenantDetailParams{TenantID: tenantID})
	if err != nil {
		return TenantPlaqueInventory{}, fmt.Errorf("db: tenant plaques: encode the parameters: %w", err)
	}
	t, err := beginOperatorRead(ctx, c, sessionHash, tenantPlaquesReadKind, params)
	if err != nil {
		return TenantPlaqueInventory{}, err
	}
	return readTenantPlaques(ctx, c, sessionHash, t, tenantID)
}

// errPlaqueOfAnotherTenant is readTenantPlaques' refusal of a row that names another tenant
// than the one asked for. The statement cannot produce one (`t.id = p_tenant_id`); this is
// the Go side's copy of that filter, so a regression in the SQL is an error here rather
// than another tenant's plaques on the screen.
var errPlaqueOfAnotherTenant = errors.New("db: read tenant plaques: a row names another tenant")

// readTenantPlaques is op_read_tenant_plaques: consume the ticket (bound to the session, the
// kind and the tenant id), then the inventory -- zero rows for an unknown id, one row with
// no plaque for a tenant without plaques, otherwise one row per plaque (at most
// MaxTenantPlaques), every row carrying the tenant and the total.
func readTenantPlaques(ctx context.Context, c OperatorConn, sessionHash string, t readTicket, tenantID uuid.UUID) (TenantPlaqueInventory, error) {
	rows, err := c.Query(ctx, readTenantPlaquesSQL, sessionHash, t.reveal(), tenantID)
	if err != nil {
		return TenantPlaqueInventory{}, operatorErr("read tenant plaques", err)
	}
	defer rows.Close()
	var (
		inv  TenantPlaqueInventory
		seen bool
	)
	for rows.Next() {
		// The plaque columns are NULL on the one row of a tenant without plaques (the
		// LEFT JOIN), so they scan into pointers whatever the column's own nullability.
		var (
			id          uuid.UUID
			name        string
			uid, status *string
			p           TenantPlaque
			createdAt   *time.Time
			lastCtr     *int32
			total       int64
		)
		if err := rows.Scan(&id, &name, &uid, &status, &p.LocationID, &p.LocationName,
			&p.EncodedAt, &createdAt, &p.RetiredAt, &p.ReplacedBy, &lastCtr, &total); err != nil {
			return TenantPlaqueInventory{}, operatorErr("read tenant plaques", err)
		}
		if id != tenantID {
			return TenantPlaqueInventory{}, errPlaqueOfAnotherTenant
		}
		seen = true
		inv.TenantID, inv.TenantName, inv.Total = id, name, total
		if uid == nil {
			continue
		}
		// A missing status stays "" -- which Shape reads as unrecognised (fail-closed) --
		// rather than becoming any status.
		p.UID, p.Status, p.CreatedAt, p.LastCtr = *uid, valueOr(status), valueOr(createdAt), valueOr(lastCtr)
		inv.Plaques = append(inv.Plaques, p)
	}
	if err := rows.Err(); err != nil {
		return TenantPlaqueInventory{}, operatorErr("read tenant plaques", err)
	}
	if !seen {
		return TenantPlaqueInventory{}, ErrNoSuchTenant
	}
	return inv, nil
}

// valueOr is *p, or T's zero value for nil.
func valueOr[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}

// ------------------------------------------------------------------ OP-14 --
//
// The operator's own audit log on the operator's surface (migration 00031; ADR 0020 §5, ADR
// 0021 §1, §2 v). One exported two-phase read in TenantList's shape -- a free function over
// an OperatorConn; since OP-14 phase B *OperatorDB delegates to it, as
// internal/handler/operator's AuditStore (the /operator/audit screen's interface), and
// TestOperatorDB_IsTheStoreAndNothingMore derives that type's method set from AuditStore too.
//
// tappa_operator holds no SELECT on operator_audit_log: the only way to read it is this
// read, and every read of it is itself a 'read' row, committed before a row is returned. The
// rows' detail is never returned raw: op_read_audit reads a CLOSED list of shapes out of it
// (search class, filter, legal slug and byte length -- values of closed sets or a bounded
// integer) and marks every other shape unrecognised -- so a value a writer put into detail
// by mistake has no field below to arrive in.

// operatorAuditReadKind is 00031's read kind -- a member of operator_read_tickets_kind_check.
const operatorAuditReadKind = "operator_audit"

// OperatorAuditKind is a kind of operator_audit_log row: the closed set of
// operator_audit_log_kind_check. The schema's CHECK, op_begin_read's filter list,
// op_read_audit's filter shape and OperatorAuditKinds are four copies of one set, held equal
// by TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree (each against the
// database) and TestOperatorAuditKinds_TheTypedConstantsAreTheList (the constants of this
// type, type-checked, against the list).
type OperatorAuditKind string

// The kinds, in the CHECK's order: the six pre-session ones (no session, no actor) first.
const (
	OperatorAuditLoginFailed      OperatorAuditKind = "login_failed"
	OperatorAuditUnknownEmail     OperatorAuditKind = "unknown_email"
	OperatorAuditTOTPFailed       OperatorAuditKind = "totp_failed"
	OperatorAuditLocked           OperatorAuditKind = "locked"
	OperatorAuditEnrollmentFailed OperatorAuditKind = "enrollment_failed"
	OperatorAuditPasswordOK       OperatorAuditKind = "password_ok"
	OperatorAuditLogin            OperatorAuditKind = "login"
	OperatorAuditEnrollment       OperatorAuditKind = "enrollment"
	OperatorAuditLogout           OperatorAuditKind = "logout"
	OperatorAuditRead             OperatorAuditKind = "read"
	OperatorAuditLegalPublish     OperatorAuditKind = "legal_publish"
)

// operatorAuditKinds is the closed set, one array; OperatorAuditKinds hands out copies.
var operatorAuditKinds = [...]OperatorAuditKind{
	OperatorAuditLoginFailed, OperatorAuditUnknownEmail, OperatorAuditTOTPFailed,
	OperatorAuditLocked, OperatorAuditEnrollmentFailed, OperatorAuditPasswordOK,
	OperatorAuditLogin, OperatorAuditEnrollment, OperatorAuditLogout, OperatorAuditRead,
	OperatorAuditLegalPublish,
	OperatorAuditOperatorCreated, OperatorAuditOperatorMFAReset, OperatorAuditOperatorDisabled,
}

// OperatorAuditKinds returns the closed set of audit kinds, in the CHECK's order -- a fresh
// slice the caller may keep.
func OperatorAuditKinds() []OperatorAuditKind {
	out := make([]OperatorAuditKind, len(operatorAuditKinds))
	copy(out, operatorAuditKinds[:])
	return out
}

// Known reports whether k is a member of the closed set. The comparison is exact -- no case
// folding, no trimming: the CHECK writes the kinds in lower case and anything else is not
// one of them.
func (k OperatorAuditKind) Known() bool {
	for _, m := range operatorAuditKinds {
		if k == m {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------- OP-14 D --
//
// The platform owner's three cmd/opadmin actions (migration 00033; ADR 0020 §5's one named
// exception to "every audit row comes from a definer"): the SQL opadmin generates and
// tappa_owner applies writes ONE row per action in the same DO block that changes the account.
// No function here writes them and no op_* can -- op_record_auth_event's closed set does not
// name them, and tappa_operator holds no INSERT on the log -- so these constants are what the
// read and the screen name, not what this package sends.

// The owner kinds, in the CHECK's order (after legal_publish). Each row names the account by
// its id (target_admin_id) and carries nothing else: no session, no actor, no tenant, no scope,
// no page, detail {} -- operator_audit_log_actor_shape's third arm.
const (
	OperatorAuditOperatorCreated  OperatorAuditKind = "operator_created"
	OperatorAuditOperatorMFAReset OperatorAuditKind = "operator_mfa_reset"
	OperatorAuditOperatorDisabled OperatorAuditKind = "operator_disabled"
)

// ByOwner reports whether k is one of the three kinds the platform owner's opadmin SQL writes:
// the kinds of actor_shape's third arm, which TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree
// holds equal to this answer for every kind the CHECK names. A row of such a kind was written
// under no operator session -- by the database's owner, outside the operator surface.
func (k OperatorAuditKind) ByOwner() bool {
	switch k {
	case OperatorAuditOperatorCreated, OperatorAuditOperatorMFAReset, OperatorAuditOperatorDisabled:
		return true
	}
	return false
}

// MaxOperatorAuditPage is the last page the log can be read at (00031: op_begin_read refuses
// a later one with 22023, and op_read_audit's OFFSET is bounded by it). The log only grows and
// a page is OFFSET (page-1) x size over it; an older row is reached through the kind filter.
const MaxOperatorAuditPage = 1000

// ErrOperatorAuditFilterRefused is OperatorAudit's answer for a kind filter that is neither
// empty nor a member of the closed set. It is decided here, without a round trip (the
// database would refuse it with 22023 as well) -- the value has no business in a statement.
// No row is written (nothing was read).
var ErrOperatorAuditFilterRefused = errors.New("db: operator audit filter refused")

// OperatorAuditQuery is a page of the log. Kind is "" for every kind, or one member of the
// closed set; Number from 1 to MaxOperatorAuditPage; Size 1..200. The read's own audit row
// records the filter (the kind, or "all") and the page -- both values of closed sets or plain
// integers. The database refuses any other page (op_begin_read, 22023) before writing a row.
type OperatorAuditQuery struct {
	Kind   OperatorAuditKind
	Number int32
	Size   int32
}

// OperatorAuditEntry is one row of op_read_audit -- a fixed column list (ADR 0021 §2 ii),
// newest first. What a field does NOT carry matters as much as what it does:
//   - Kind is the row's kind VERBATIM (OperatorAuditKind(e.Kind).Known() says whether this
//     build names it -- a later migration's kind reads as unknown, never as a neighbour);
//   - SessionID is the operator session's id, not its hash (no op_* takes an id);
//   - ActorName, TargetAdminName and TargetTenantName are names other tables hold (an
//     operator's display name, a tenant's name) -- free text, for the screen to escape;
//   - Scope is the read kind of a 'read' row, and only when it is one op_read_audit names;
//   - SearchClass, FilterKind, LegalSlug and LegalBytes are what the closed list of detail
//     shapes reads out, and only when DetailRecognised; the detail itself is never returned.
type OperatorAuditEntry struct {
	ID               uuid.UUID
	At               time.Time
	Kind             string
	SessionID        *uuid.UUID
	ActorID          *uuid.UUID
	ActorName        *string
	TargetAdminID    *uuid.UUID
	TargetAdminName  *string
	TargetTenantID   *uuid.UUID
	TargetTenantName *string
	Scope            *string
	PageNumber       *int32
	PageSize         *int32
	SearchClass      *string
	FilterKind       *string
	LegalSlug        *string
	LegalBytes       *int32
	// DetailRecognised: the row's (kind, scope, detail) is a shape on op_read_audit's closed
	// list. false is FAIL-CLOSED: the detail is not shown, the row still is.
	DetailRecognised bool
}

// operatorAuditParams is the read's parameter object as op_begin_read takes it: exactly these
// keys (00031). The database rebuilds the object it hashes from the typed values, so this
// spelling does not bind the ticket.
type operatorAuditParams struct {
	Kind       string `json:"kind"`
	PageNumber int32  `json:"page_number"`
	PageSize   int32  `json:"page_size"`
}

// OperatorAudit is the two-phase read of the operator's own log: op_begin_read writes the
// read's 'read' row (scope 'operator_audit', the page, the filter) and a ticket bound to the
// session, the filter and the page; op_read_audit consumes it and returns the page. As with
// TenantList, the two phases are two transactions only on a pool; on a pgx.Tx the database
// refuses phase two and this returns ErrOperatorRefused. A dead session is
// ErrOperatorRefused; a filter outside the set is ErrOperatorAuditFilterRefused; a page
// outside the database's bounds is a database error carrying 22023.
func OperatorAudit(ctx context.Context, c OperatorConn, sessionHash string, q OperatorAuditQuery) ([]OperatorAuditEntry, error) {
	if q.Kind != "" && !q.Kind.Known() {
		return nil, ErrOperatorAuditFilterRefused
	}
	params, err := json.Marshal(operatorAuditParams{Kind: string(q.Kind), PageNumber: q.Number, PageSize: q.Size})
	if err != nil {
		return nil, fmt.Errorf("db: operator audit: encode the page: %w", err)
	}
	t, err := beginOperatorRead(ctx, c, sessionHash, operatorAuditReadKind, params)
	if err != nil {
		return nil, err
	}
	return readOperatorAudit(ctx, c, sessionHash, t, q)
}

// readOperatorAudit is op_read_audit: consume the ticket (bound to the session, the kind
// filter and the page), then the page of the log.
func readOperatorAudit(ctx context.Context, c OperatorConn, sessionHash string, t readTicket, q OperatorAuditQuery) ([]OperatorAuditEntry, error) {
	rows, err := c.Query(ctx, readOperatorAuditSQL, sessionHash, t.reveal(), string(q.Kind), q.Number, q.Size)
	if err != nil {
		return nil, operatorErr("read operator audit", err)
	}
	defer rows.Close()
	var out []OperatorAuditEntry
	for rows.Next() {
		var e OperatorAuditEntry
		if err := rows.Scan(&e.ID, &e.At, &e.Kind, &e.SessionID, &e.ActorID, &e.ActorName,
			&e.TargetAdminID, &e.TargetAdminName, &e.TargetTenantID, &e.TargetTenantName,
			&e.Scope, &e.PageNumber, &e.PageSize, &e.SearchClass, &e.FilterKind, &e.LegalSlug,
			&e.LegalBytes, &e.DetailRecognised); err != nil {
			return nil, operatorErr("read operator audit", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, operatorErr("read operator audit", err)
	}
	return out, nil
}

// ------------------------------------------------------------------ OP-12 --
//
// One tenant's billing months on the operator's surface (migration 00032; ADR 0021 §2 v,
// §3.2; CLAUDE.md §4.6, §6). One exported two-phase read in TenantPlaques' shape -- a free
// function over an OperatorConn; it hands its readTicket from phase one to phase two (the
// type's comment lists it). The screen and *OperatorDB's method are OP-12 phase B.
//
// 🔴 THE FIGURES ARE THE TENANT'S OWN. op_read_tenant_billing is a third copy of the glue
// around 00016's five billing functions (db/queries/billing.sql's Preview and Close are the
// other two), compared with the tenant's path month by month, on its fixtures, by
// TestOpReadTenantBilling_EveryMonthIsTheTenantsOwnFigure (the month the tenant is in, at
// instants that test does not run at, is TestOpReadTenantBilling_TheNewestMonthIsTheZonesAtAnyInstant's;
// migration 00032's header names the limit). Nothing here computes a figure.
//
// 🔴 MONEY STAYS A DECIMAL. UnitPrice and AmountDue are pgtype.Numeric -- a big.Int mantissa
// and a decimal exponent, exact -- and leave this package that way: package db cannot import
// internal/domain/billing (billing imports db), so turning them into billing.Money
// (MoneyFromNumeric) is the caller's. MoneyFromNumeric refuses MORE than two decimal places
// and accepts fewer: a value whose scale was lost (1.5, or a zero, which pgx carries as 0 x
// 10^0) converts without complaint, so it is no scale barrier -- the scale is pinned in SQL
// (TestOpReadTenantBilling_MoneyIsNumericAtScaleTwo; ADR 0021's OP-12 note, L4). No float64 is
// produced here and nothing here multiplies.

// tenantBillingReadKind is 00032's read kind -- a member of operator_read_tickets_kind_check.
const tenantBillingReadKind = "tenant_billing"

// The page: TenantBillingMonthsPerPage of the tenant's local months, newest first, pages 1 to
// MaxTenantBillingPage (00032: op_begin_read refuses any other page with 22023 and
// op_read_tenant_billing bounds the page in its body as well). Five pages of twelve are sixty
// months -- the tenant's own history depth, internal/domain/billing HistoryCap (held equal by
// TestTenantBilling_FivePagesOfTwelveAreTheTenantsHistoryCap, from outside the package).
const (
	MaxTenantBillingPage       = 5
	TenantBillingMonthsPerPage = 12
)

// readTenantBillingSQL is phase two; phase one is beginOperatorReadSQL. Mirrored in
// db/queries/operator.sql.
const readTenantBillingSQL = `SELECT tenant_id, tenant_name, period_month, after_signup, frozen, period_from,
       period_to, period_timezone, plan, first_chargeable_month, free_period, employee_count,
       unstamped_employees, unit_price, currency, amount_due, closed_at, period_has_ended
FROM public.op_read_tenant_billing($1, $2, $3, $4)`

// 🔴 PHASE TWO CARRIES ITS OWN STATEMENT TIMEOUT (OP-12 phase B; ADR 0021's OP-12 note, L5).
// The operator's pool sets none (the server's is 0) and the read was measured taking seconds
// on a large roster, so without a bound a slow read holds a connection and the operator's
// request for as long as it runs. Phase two therefore runs in a transaction of its own whose
// first statement is tenantBillingStatementTimeoutSQL: LOCAL, so it ends with that
// transaction and never reaches the next user of the pooled connection. A read past it is
// SQLSTATE 57014, which operatorErr turns into a plain database error -- the screen's 503,
// never a page of zero amounts -- and the rollback undoes the ticket's consumption with
// everything else phase two did; the 'read' row phase one committed stays.
//
// TenantBillingReadTimeout is that bound as a Go value; the statement spells the same number
// in milliseconds (SET takes no parameter, and the statements carry no quoted literal --
// TestOperatorSQL_OnlyBoundParameters). TestTenantBilling_TheStatementSpellsTheReadTimeout
// holds the two equal. WHY 15 SECONDS, arithmetic rather than taste:
//
//	the slowest read measured (L5: dev's largest roster, twelve live months)  6.1 s
//	x 2 headroom                                                              12.2 s  <= 15 s
//	the request's own deadline (httpx.RequestTimeout)                          30 s
//	/ 2: the bound fires first, with room for the session predicate, phase one
//	and the render, so a slow read is the database's 57014 and the screen's
//	designed 503 -- not the router's deadline                                 15 s
//
// 15 s is the largest number under the second line and clears the first; the handler's test
// holds 2 x TenantBillingReadTimeout <= httpx.RequestTimeout
// (TestBillingRead_TheBoundFiresBeforeTheRequestDeadline). Production rosters count tens, and a
// read of them is milliseconds.
const (
	tenantBillingStatementTimeoutSQL = `SET LOCAL statement_timeout = 15000`
	TenantBillingReadTimeout         = 15 * time.Second
)

// operatorTxConn is an OperatorConn that can open a transaction -- *pgxpool.Pool, *pgx.Conn
// and pgx.Tx each are one (on a pgx.Tx, Begin is a savepoint). TenantBilling's phase two needs
// a transaction of its own for its SET LOCAL.
type operatorTxConn interface {
	OperatorConn
	Begin(ctx context.Context) (pgx.Tx, error)
}

// errBillingNeedsATransaction is TenantBilling's refusal of a connection that cannot open a
// transaction, answered BEFORE phase one: a read that could not bound its second phase does
// not write the 'read' row of the first.
var errBillingNeedsATransaction = errors.New("db: tenant billing: the connection cannot open a transaction for the read's time bound")

// TenantBillingTimeline is the one read's three answers: the tenant -- its name for the
// header of the screen (ADR 0020 §9) --, the fact that it exists (an id that names no tenant
// is ErrNoSuchTenant, never an empty timeline), and a page of its months: exactly
// TenantBillingMonthsPerPage of them, newest first.
type TenantBillingTimeline struct {
	TenantID   uuid.UUID
	TenantName string
	Page       int32
	Months     []TenantBillingMonth
}

// TenantBillingMonth is one local month of the tenant. It is one of three things, and the two
// booleans say which -- the figures mean something different in each:
//   - Frozen: the month was closed. Every figure was READ from the frozen row and none was
//     recomputed; ClosedAt is when; FirstChargeableMonth is not valid (a frozen row keeps the
//     decision, Free, not the rule); AfterSignup and HasEnded are true.
//   - !Frozen && AfterSignup: a live preview -- the tenant's own PreviewBillingPeriod
//     arithmetic over today's roster and price. Currency is nil (a live month has no frozen
//     currency: the caller's default applies); HasEnded says whether the month is over, so
//     !Frozen && *HasEnded is a month that ended and was not closed.
//   - !AfterSignup: before the business signed up. EVERY figure is nil or not valid -- never
//     a zero invoice (CLAUDE.md §4.6, the money form).
//
// UnstampedEmployees is the count of the tenant's employee rows whose status disagrees with
// their lifecycle stamps; when it is above zero EmployeeCount is a floor (migration 00016 says
// why). No employee is named or listed: the read returns counts only.
type TenantBillingMonth struct {
	Month                pgtype.Date
	AfterSignup          bool
	Frozen               bool
	From, To             *time.Time
	Zone                 *string
	Plan                 *string
	FirstChargeableMonth pgtype.Date
	Free                 *bool
	EmployeeCount        *int32
	UnstampedEmployees   *int32
	UnitPrice            pgtype.Numeric
	Currency             *string
	AmountDue            pgtype.Numeric
	ClosedAt             *time.Time
	HasEnded             *bool
}

// tenantBillingParams is the read's parameter object as op_begin_read takes it: exactly these
// keys (00032). The database rebuilds the object it hashes from the typed values, so this
// spelling does not bind the ticket.
type tenantBillingParams struct {
	TenantID   uuid.UUID `json:"tenant_id"`
	PageNumber int32     `json:"page_number"`
}

// TenantBilling is the two-phase read of one tenant's billing months: op_begin_read writes the
// read's 'read' row (target_scope 'tenant_billing', the tenant named, the page) and a ticket
// bound to the session, the tenant and the page; op_read_tenant_billing consumes it and
// returns the page. An id that names no tenant is ErrNoSuchTenant -- after the 'read' row
// naming it has committed. A dead session is ErrOperatorRefused, and so is a pgx.Tx (the two
// phases are two transactions only on a pool, as for TenantList); a page outside
// 1..MaxTenantBillingPage is a database error carrying 22023. Phase two runs in a transaction
// of its own under TenantBillingReadTimeout (above): a read past it is a database error
// carrying 57014, its ticket left unconsumed. A connection that cannot open that transaction
// is refused before phase one (errBillingNeedsATransaction).
func TenantBilling(ctx context.Context, c OperatorConn, sessionHash string, tenantID uuid.UUID, page int32) (TenantBillingTimeline, error) {
	tc, ok := c.(operatorTxConn)
	if !ok {
		return TenantBillingTimeline{}, errBillingNeedsATransaction
	}
	params, err := json.Marshal(tenantBillingParams{TenantID: tenantID, PageNumber: page})
	if err != nil {
		return TenantBillingTimeline{}, fmt.Errorf("db: tenant billing: encode the parameters: %w", err)
	}
	t, err := beginOperatorRead(ctx, tc, sessionHash, tenantBillingReadKind, params)
	if err != nil {
		return TenantBillingTimeline{}, err
	}
	return readTenantBilling(ctx, tc, sessionHash, t, tenantID, page)
}

// errBillingOfAnotherTenant is readTenantBilling's refusal of a row that names another tenant
// than the one asked for. The statement cannot produce one (`t.id = p_tenant_id`); this is the
// Go side's copy of that filter, so a regression in the SQL is an error here rather than
// another tenant's invoice on the screen.
var errBillingOfAnotherTenant = errors.New("db: read tenant billing: a row names another tenant")

// readTenantBilling is op_read_tenant_billing: consume the ticket (bound to the session, the
// kind, the tenant id and the page), then the page -- zero rows for an unknown id, otherwise
// one row per month, every row carrying the tenant. It runs in a transaction of its own
// (Begin: on a pool a connection's transaction, on a pgx.Tx a savepoint) whose first
// statement is tenantBillingStatementTimeoutSQL; the transaction is committed once the rows
// are read -- an unknown id included, so its ticket is consumed as on any read -- and rolled
// back on every error.
func readTenantBilling(ctx context.Context, c operatorTxConn, sessionHash string, t readTicket, tenantID uuid.UUID, page int32) (TenantBillingTimeline, error) {
	tx, err := c.Begin(ctx)
	if err != nil {
		return TenantBillingTimeline{}, operatorErr("read tenant billing", err)
	}
	tl, err := scanTenantBilling(ctx, tx, sessionHash, t, tenantID, page)
	if err != nil && !errors.Is(err, ErrNoSuchTenant) {
		// The read's own error is the answer; a failed rollback only adds that the connection
		// is broken, which the pool discards.
		if rerr := tx.Rollback(ctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
			return TenantBillingTimeline{}, fmt.Errorf("%w (and the rollback: %w)", err, operatorErr("read tenant billing", rerr))
		}
		return TenantBillingTimeline{}, err
	}
	if cerr := tx.Commit(ctx); cerr != nil {
		return TenantBillingTimeline{}, operatorErr("read tenant billing", cerr)
	}
	return tl, err
}

// scanTenantBilling is phase two's two statements on the read's own transaction: the time
// bound, then the read and its scan.
func scanTenantBilling(ctx context.Context, tx pgx.Tx, sessionHash string, t readTicket, tenantID uuid.UUID, page int32) (TenantBillingTimeline, error) {
	if _, err := tx.Exec(ctx, tenantBillingStatementTimeoutSQL); err != nil {
		return TenantBillingTimeline{}, operatorErr("read tenant billing", err)
	}
	rows, err := tx.Query(ctx, readTenantBillingSQL, sessionHash, t.reveal(), tenantID, page)
	if err != nil {
		return TenantBillingTimeline{}, operatorErr("read tenant billing", err)
	}
	defer rows.Close()
	tl := TenantBillingTimeline{Page: page}
	for rows.Next() {
		var (
			id   uuid.UUID
			name string
			m    TenantBillingMonth
		)
		if err := rows.Scan(&id, &name, &m.Month, &m.AfterSignup, &m.Frozen, &m.From, &m.To, &m.Zone,
			&m.Plan, &m.FirstChargeableMonth, &m.Free, &m.EmployeeCount, &m.UnstampedEmployees,
			&m.UnitPrice, &m.Currency, &m.AmountDue, &m.ClosedAt, &m.HasEnded); err != nil {
			return TenantBillingTimeline{}, operatorErr("read tenant billing", err)
		}
		if id != tenantID {
			return TenantBillingTimeline{}, errBillingOfAnotherTenant
		}
		tl.TenantID, tl.TenantName = id, name
		tl.Months = append(tl.Months, m)
	}
	if err := rows.Err(); err != nil {
		return TenantBillingTimeline{}, operatorErr("read tenant billing", err)
	}
	if len(tl.Months) == 0 {
		return TenantBillingTimeline{}, ErrNoSuchTenant
	}
	return tl, nil
}

// readTicket is op_begin_read's answer: the RAW read ticket, which CLAUDE.md §7 and ADR
// 0021 §3.5 put on the never-log list (raw or hashed). The type is unexported and no
// exported function takes or returns it -- LegalVersions, TenantList, TenantDetail,
// TenantPlaques, OperatorAudit and TenantBilling each hand it from their phase one to their phase two --
// and it is the SealedSecret pattern all the same: the five redacting methods, and the
// value behind a *string (SealedSecret's comment says why a *string and not a byte slice).
// Through fmt's verbs, slog and encoding/json it prints the placeholder
// (TestReadTicket_PrintsOnlyThePlaceholder's matrix).
type readTicket struct{ v *string }

const ticketRedacted = "db.readTicket(redacted)"

var (
	_ fmt.Formatter          = readTicket{}
	_ fmt.Stringer           = readTicket{}
	_ fmt.GoStringer         = readTicket{}
	_ slog.LogValuer         = readTicket{}
	_ encoding.TextMarshaler = readTicket{}
)

// reveal is the one reader: the raw ticket for op_read_*, or "" for the zero value
// (which no ticket hash matches -- the fail-closed direction).
func (t readTicket) reveal() string {
	if t.v == nil {
		return ""
	}
	return *t.v
}

func (readTicket) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(ticketRedacted)) }
func (readTicket) String() string               { return ticketRedacted }
func (readTicket) GoString() string             { return ticketRedacted }
func (readTicket) LogValue() slog.Value         { return slog.StringValue(ticketRedacted) }
func (readTicket) MarshalText() ([]byte, error) { return []byte(ticketRedacted), nil }

// operatorErr is rule (c): a PostgreSQL error leaves this file as ITS SQLSTATE ONLY.
// 28000 becomes ErrOperatorRefused; any other SQLSTATE becomes a fresh error naming
// the call and the code -- NOT wrapping the *pgconn.PgError, whose Detail, Where and
// ConstraintName are exactly what 00026 took pains to keep off the wire. Errors that
// are not PostgreSQL's (a cancelled context, a broken connection) carry no row value
// and are wrapped so errors.Is(context.Canceled) still works.
func operatorErr(what string, err error) error {
	if err == nil {
		return nil
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		if pg.Code == "28000" {
			return ErrOperatorRefused
		}
		return fmt.Errorf("db: %s: database error (SQLSTATE %s)", what, pg.Code)
	}
	return fmt.Errorf("db: %s: %w", what, err)
}

// SealedSecret is platform_admins.totp_secret_sealed in transit: the AES-256-GCM
// envelope of an operator's TOTP secret (internal/sun.Seal, AAD = the account id).
// It is PasswordHash's pattern -- the five redacting methods plus the bytes behind a
// *string -- because the envelope is on ADR 0020 §5's never-log list: useless
// without TAPPA_OPERATOR_TOTP_KEK, and a RCE that has the process has the KEK too
// (ADR 0020 risk 3), so a logged envelope is one step from a logged secret.
//
// WHY *string AND NOT *[]byte (OP-6 verification, 7th round, the 6th auditor
// measured): fmt reaches no method of a value held in a caller's UNEXPORTED field and
// prints it by reflection; for a verb a pointer does not take (%s %q %e %f %t %c %U)
// fmt's badVerb opens the pointer ONCE and a *[]byte opens to the bytes -- the
// envelope printed. A *string is not opened (fmt opens a pointer to an array, a slice,
// a struct or a map only), so it prints an address. The first version held *[]byte.
//
// The placeholder differs from PasswordHash's so the two cannot vouch for each other
// in a leak test that greps for one of them.
type SealedSecret struct{ v *string }

const envelopeRedacted = "db.SealedSecret(redacted)"

var (
	_ fmt.Formatter          = SealedSecret{}
	_ fmt.Stringer           = SealedSecret{}
	_ fmt.GoStringer         = SealedSecret{}
	_ slog.LogValuer         = SealedSecret{}
	_ encoding.TextMarshaler = SealedSecret{}
)

// NewSealedSecret wraps a COPY of b; the caller's slice is not aliased.
func NewSealedSecret(b []byte) SealedSecret {
	c := string(b)
	return SealedSecret{v: &c}
}

// RevealForOpen returns a copy of the envelope for internal/sun.Open, or nil for the
// zero value (which then fails to open -- the fail-closed direction). The one reader.
func (s SealedSecret) RevealForOpen() []byte {
	if s.v == nil {
		return nil
	}
	return []byte(*s.v)
}

// Format, String, GoString, LogValue, MarshalText: every path that reaches a method
// emits the placeholder (PasswordHash's comments give the path each one covers); the
// paths that reach none print the *string's address (operatorauth's leak test drives
// the matrix).
func (SealedSecret) Format(f fmt.State, _ rune)   { _, _ = f.Write([]byte(envelopeRedacted)) }
func (SealedSecret) String() string               { return envelopeRedacted }
func (SealedSecret) GoString() string             { return envelopeRedacted }
func (SealedSecret) LogValue() slog.Value         { return slog.StringValue(envelopeRedacted) }
func (SealedSecret) MarshalText() ([]byte, error) { return []byte(envelopeRedacted), nil }
