-- operator.sql -- the platform operator's statements (M10; ADR 0021 §2 vi). A
-- DOCUMENT, not a sqlc input: like resolve.sql it declares NO `-- name:` query, so
-- sqlc skips it, and every statement below is a comment. The Go constants in
-- internal/db/operator.go mirror them verbatim; keep the two in sync by hand.
--
-- WHY HAND-WRITTEN: the five op_* functions of migration 00026 are called as
-- `SELECT op_x(...)` or `SELECT ... FROM op_touch_session($1)` (RETURNS TABLE / OUT
-- parameters), which sqlc v1.28 cannot type -- resolve.sql records the three
-- measurements. The two login lookups COULD be sqlc queries, but they read
-- platform_admins, a table with no tenant_id, through tappa_operator -- a role the
-- generated store (tappa_app's) never runs as -- so they live with the calls they
-- serve rather than in a package whose every query carries a tenant predicate.
--
-- WHO RUNS THEM: tappa_operator only (ADR 0021 §1). tappa_app holds no EXECUTE on any
-- op_* and no privilege on platform_admins, so the same statement on the customer
-- pool fails with 42501 -- loudly, in both directions (ADR 0021 §4).
--
-- EVERY VALUE IS A BOUND PARAMETER ($n). A value spliced into the SQL text would be
-- logged with the statement whatever log_parameter_max_length says (ADR 0021 "Karar
-- verilmedi", condition 2). The one quoted literal is the schema constant 'active'.
--
-- ============================================================================

-- OperatorByEmail -- the password step's lookup. tappa_operator's single policy on
-- platform_admins shows ACTIVE rows only (00026); `status = 'active'` repeats it in
-- the statement (belt beside the braces). Pending, disabled and unknown addresses are
-- therefore one answer: no row. The citext equality is schema-qualified (ADR 0002's
-- M6-01 trap: under a pinned search_path an unqualified = on citext silently turns
-- case-sensitive).
--   SELECT id, password_hash, totp_secret_sealed, totp_locked_until
--   FROM public.platform_admins
--   WHERE email OPERATOR(public.=) $1::public.citext AND status = 'active';

-- OperatorByID -- the TOTP step's lookup, keyed by the id the signed login challenge
-- carries.
--   SELECT id, password_hash, totp_secret_sealed, totp_locked_until
--   FROM public.platform_admins
--   WHERE id = $1 AND status = 'active';

-- RecordOperatorAuthEvent -- one pre-session FAILURE row (closed kind set; the
-- address and the id are looked up by the definer, never stored).
--   SELECT public.op_record_auth_event($1, $2, $3);

-- OpenOperatorSession -- TOTP step advance + lock check + session + 'login' row, one
-- statement (00026 §5.3).
--   SELECT public.op_open_session($1, $2, $3);

-- CompleteOperatorEnrollment -- token consumption + credentials + first session +
-- 'enrollment' row, one statement (00026 §5.4). $2 is the RAW token text; the
-- definer hashes it.
--   SELECT public.op_complete_enrollment($1, $2, $3, $4, $5, $6);

-- TouchOperatorSession -- THE session predicate (00026 §5.1).
--   SELECT session_id, admin_id FROM public.op_touch_session($1);

-- CloseOperatorSession -- logout through the touch predicate (00026 §5.5).
--   SELECT public.op_close_session($1);

-- ============================================================================
-- OP-10 (migration 00027): the legal texts on the operator's surface.

-- BeginOperatorRead -- phase one of the two-phase reads (ADR 0021 §2 v 1): resolves
-- the session, writes the read's 'read' audit row and a ticket bound to it, returns the
-- RAW ticket. $2 is a read kind (00027's closed set: 'legal_versions'), $3 that kind's
-- parameter object. The caller COMMITS before phase two.
--   SELECT public.op_begin_read($1, $2, $3::jsonb);

-- ReadLegalVersions -- phase two of the version list: $2 is the RAW ticket, $3/$4 the
-- page the ticket was bound to. One consuming UPDATE (session, kind, hash of ticket and
-- page, unconsumed, unexpired by the wall clock, created by a COMMITTED transaction),
-- then the page, newest first, capped at 200 rows.
--   SELECT version_id, slug, published_at, body_bytes, publisher_kind,
--          publisher_admin_id, publisher_name, is_current
--   FROM public.op_read_legal_versions($1, $2, $3, $4);

-- PublishLegal -- one new version and its 'legal_publish' audit row, one statement
-- (ADR 0020 §7). published_by is the session's operator; there is no actor argument.
--   SELECT public.op_publish_legal($1, $2, $3);
