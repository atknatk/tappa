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
// 00029 adds (OP-11) read tenant tables WITHOUT a tenant context: they cross the
// boundary as their BYPASSRLS owner, the one crossing ADR 0021 permits, and their
// statements name the tenant themselves.
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

// OperatorAuthEvent is op_record_auth_event's CLOSED kind set (00026, ADR 0021 §1):
// pre-session FAILURES only. The database refuses any other value with 22023.
type OperatorAuthEvent string

const (
	OperatorLoginFailed      OperatorAuthEvent = "login_failed"
	OperatorUnknownEmail     OperatorAuthEvent = "unknown_email"
	OperatorTOTPFailed       OperatorAuthEvent = "totp_failed"
	OperatorLocked           OperatorAuthEvent = "locked"
	OperatorEnrollmentFailed OperatorAuthEvent = "enrollment_failed"
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

// RecordOperatorAuthEvent writes ONE pre-session failure row through
// op_record_auth_event. The address (if any) and the account id (if any) are only
// looked up by the definer; neither is stored (ADR 0021 §1). An empty email, one no
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

// readTicket is op_begin_read's answer: the RAW read ticket, which CLAUDE.md §7 and ADR
// 0021 §3.5 put on the never-log list (raw or hashed). The type is unexported and no
// exported function takes or returns it -- LegalVersions, TenantList and TenantDetail
// each hand it from their phase one to their phase two -- and it is the SealedSecret
// pattern all the same: the five redacting
// methods, and the value behind a *string (SealedSecret's comment says why a *string and
// not a byte slice). Through fmt's verbs, slog and encoding/json it prints the
// placeholder (TestReadTicket_PrintsOnlyThePlaceholder's matrix).
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
