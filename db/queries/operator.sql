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

-- RecordOperatorAuthEvent -- one pre-session row (closed kind set: the five failures and,
-- since 00031, 'password_ok', which names its account by id alone and moves no lock
-- counter; the address and the id are looked up by the definer, never stored).
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
-- RAW ticket. $2 is a read kind (00027's closed set: 'legal_versions'; 00029 added
-- 'tenants' and 'tenant_detail', 00030 'tenant_plaques', 00031 'operator_audit'), $3 that
-- kind's parameter object. The caller COMMITS before phase two.
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

-- ============================================================================
-- OP-11 (migration 00029): the tenant list and one tenant's overview. Phase one of
-- both is BeginOperatorRead above, with kind 'tenants' ({page_number, page_size,
-- query}) or 'tenant_detail' ({tenant_id}). Inside both functions no row level
-- security applies (their owner is BYPASSRLS); the detail's statements name the tenant
-- themselves, and the list is bounded by its four columns and the 200-row ceiling.

-- ReadTenants -- phase two of the list: $2 is the RAW ticket, $3 the search term ('' for
-- every tenant), $4/$5 the page -- all bound in the ticket. The term matches a name as a
-- case-insensitive substring (strpos, no LIKE metacharacters), an admin's address
-- exactly (citext) -- only when the term contains '@' --, or the tenant's id when the
-- whole term is a hyphenated uuid; those two gates are the predicates by which phase
-- one writes the read's class ('address', 'id') into its audit row. Newest tenant first,
-- capped at 200 rows.
--   SELECT tenant_id, tenant_name, created_at, plan
--   FROM public.op_read_tenants($1, $2, $3, $4, $5);

-- ReadTenantDetail -- phase two of the overview: $2 is the RAW ticket, $3 the tenant id
-- bound in it. Zero rows for an id that names no tenant; otherwise the identity and four
-- counts (locations; active employees, plaques and panel accounts).
--   SELECT tenant_id, tenant_name, created_at, plan, business_type,
--          location_count, active_employee_count, active_plaque_count, active_admin_count
--   FROM public.op_read_tenant_detail($1, $2, $3);

-- ============================================================================
-- OP-13 (migration 00030): one tenant's plaque inventory. Phase one is BeginOperatorRead
-- above, with kind 'tenant_plaques' and the overview's parameter object ({tenant_id}) --
-- the KIND tells the two reads' tickets apart. Inside the function no row level security
-- applies (its owner is BYPASSRLS); each of its three table references names the tenant,
-- and the definer holds no SELECT on either plaque key (the forms measured to fail: ADR
-- 0021, "OP-13 uygulama notu", PART I). Three of them, the writes' RETURNING *, do not
-- isolate the key grant: they need a write privilege on tags as well, which the definer
-- does not hold either, and each missing grant refuses them on its own (measured).

-- ReadTenantPlaques -- phase two of the inventory: $2 is the RAW ticket, $3 the tenant id
-- bound in it. Zero rows for an id that names no tenant; one row with NULL plaque columns
-- and plaque_count 0 for a tenant without plaques; otherwise one row per plaque, stock
-- first then by uid, capped at 200, every row carrying the tenant's name and plaque_count
-- (every plaque the tenant holds, counted before the cap). status is returned verbatim;
-- encoded_at is the one encode signal.
--   SELECT tenant_id, tenant_name, uid, status, location_id, location_name,
--          encoded_at, created_at, retired_at, replaced_by, last_ctr, plaque_count
--   FROM public.op_read_tenant_plaques($1, $2, $3);

-- ============================================================================
-- OP-14 (migration 00031): the operator's own audit log. Phase one is BeginOperatorRead
-- above, with kind 'operator_audit' and the parameter object {kind, page_number,
-- page_size}: kind '' (every kind) or one of operator_audit_log's kinds, page 1..1000. The
-- read's own 'read' row records the filter ({"filter": <kind>|"all"}) and the page.
-- tappa_operator holds no SELECT on operator_audit_log; this is the one way to read it.

-- ReadOperatorAudit -- phase two of the log: $2 is the RAW ticket, $3 the kind filter, $4/$5
-- the page -- all bound in the ticket. Newest first (at, then id), capped at 200 rows, the
-- OFFSET bounded at page 1000. The rows' detail is NOT returned raw: a closed list of shapes is
-- read out of it (search_class, filter_kind, legal_slug, legal_bytes) and every other shape
-- is detail_recognised = false with those columns NULL. Names come from platform_admins and
-- tenants by the ids each row carries.
--   SELECT audit_id, at, kind, session_id, actor_admin_id, actor_name,
--          target_admin_id, target_admin_name, target_tenant_id, target_tenant_name,
--          target_scope, page_number, page_size, search_class, filter_kind, legal_slug,
--          legal_bytes, detail_recognised
--   FROM public.op_read_audit($1, $2, $3, $4, $5);
