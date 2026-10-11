-- 00039 -- M11 API-1 (K-22): the operator opens and closes a tenant's integration API -- the
-- ONLY writer of tenants.api_enabled_at and tenants.api_code (migration 00038).
--
-- NORMATIVE SOURCES: docs/adr/0028-dis-api-anahtar-akis-webhook.md §2 (K-22: API and webhooks
-- work for a tenant only once the operator has opened them; the writer is an op_* of ADR 0021's
-- class: owned by tappa_opdefiner, audited into operator_audit_log AND the tenant's audit_log,
-- no state oracle) and docs/adr/0021-op-fonksiyonlari-tenant-otesi-erisim.md §2 (the contract:
-- the session predicate, a write returns void whatever the tenant held -- v 7 --, B14, the wall
-- clock -- vii), §3 (inside an op_* the explicit tenant filter is the ONLY barrier; K6) and §6
-- (the catalogue and behaviour pins), and its "M11 notu". Where this file decides something
-- those leave open, the decision is written next to the statement.
--
-- WHAT IT DOES, IN ORDER:
--   0. re-checks the two cluster roles' shape (00026's precondition, as 00027-00034 repeated it),
--      that the database is at 00034 EXACTLY on the operator surface (the kind CHECK and the
--      tenant-write shape compared by their full text, so a migration that widened them in
--      between -- OP-15's, waiting on another branch as 00036 -- stops this file loudly instead
--      of being silently undone by it), that 00038's two columns exist, and that nobody already
--      reaches them;
--   1. operator_audit_log_kind_check gains 'tenant_api_enabled' and 'tenant_api_disabled' (NOT
--      VALID only when a row of a kind outside the new set already exists), and
--      operator_audit_log_tenant_write_shape names both: the operator's row names its tenant and
--      nothing else;
--   2. tappa_opdefiner gains SELECT (api_enabled_at) and UPDATE (api_enabled_at, api_code) on
--      tenants -- nothing else (the K6 INSERT on audit_log is 00034's);
--   3. op_begin_read and 4. op_read_audit are REPLACED: 00034's bodies, byte for byte, with the
--      two kinds added to the one list each carries (the five copies of the kind set are held
--      equal by TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree and the viewer's
--      words by TestAuditWords_NameEveryKindAndNothingElse);
--   5. op_enable_tenant_api and op_disable_tenant_api -- the two writes.
--
-- THE TWO WRITES (OP-15's suspend/reinstate pair is their emsal, as written on its branch):
--   * ONE PHASE (ADR 0021 §2 v 7): the change, the tenant's own audit_log row and the operator's
--     row are one call -- they commit together or not at all; RETURNS void;
--   * (a) the session through op_touch_session (28000 for a dead one; the actor is derived);
--     (b) the arguments, refused with ONE constant 22023 before anything is read: a NULL tenant,
--     and for the enable a code outside 00038's shape (^[A-Z][A-Z0-9]{1,15}$) -- checked here, so
--     a malformed code is refused the same way whether or not the tenant exists; (c) the
--     tenant's row by its primary key ALONE, FOR NO KEY UPDATE, nothing of it returned (PERFORM)
--     -- the lock the UPDATE takes anyway, which does not conflict with the FOR KEY SHARE a
--     foreign-key insert takes, so a tap naming the tenant is not held up; (d) only IF FOUND:
--     ONE wall-clock read; the UPDATE by the primary key; the tenant's row (K6) -- action
--     'tenant.api_enabled' with detail {"actor_kind": "operator", "api_code": <code>}, or
--     'tenant.api_disabled' with {"actor_kind": "operator"} --, actor_id the operator, target
--     the tenant id, `at` that same clock read (00005's DEFAULT is the transaction's start); (e)
--     on EVERY accepted call, outside the IF: the operator's row ('tenant_api_enabled' /
--     'tenant_api_disabled'), naming the tenant, detail {} (operator_audit_log_tenant_write_shape);
--   * THE ANSWER IS THE SAME WHATEVER THE TENANT HELD OR WHETHER IT EXISTS: void, or 28000 / 22023
--     decided by the session and the arguments alone. For an id that names no tenant nothing is
--     written to any tenant and no foreign key fires (B14); only the operator's row is written.
--     The body never reads a value of the tenant into a decision: the enable keeps the first
--     opening instant with coalesce inside the UPDATE (the CURRENT value, under the row lock),
--     and replaces the code; the disable clears api_enabled_at and KEEPS the code -- punches
--     still carry it as their `company`, open API or not (ADR 0028 §2: events are written for a
--     closed tenant too). Called again, each writes all three rows again: the tenant row says
--     "the action was applied", not "the state changed".
--   * ⚠️ COUNTED, NOT CLOSED (ADR 0021 limit 15, LV3 as OP-16 counted it): a call inside a
--     SAVEPOINT that is rolled back tells, through the statistics views or a lock wait, whether
--     the tenant id exists -- and nothing else: no code is compared with a stored value. The
--     holder of tappa_operator's DSN can open any tenant's API; every such write leaves the
--     operator row and the tenant row.
--   * A constraint that refuses (c)-(e) is caught (00027's pattern): one fixed 22023, a LOG line
--     with the constraint's name and the SQLSTATE only.
--
-- CLOCKS (ADR 0021 §2 vii): every instant either function writes is clock_timestamp(), read
-- ONCE per call; the operator row's `at` is 00033's trigger's. TestOperator00026_NoFrozenClock
-- walks every function tappa_opdefiner owns.
--
-- SECRETS: none here -- an API code is a public label. Every RAISE carries a constant message.
--
-- No table is created, so there is no redline waiver, no RLS and no sequence; tappa_app gains
-- nothing; tappa_operator gains EXECUTE on the two new functions and no table privilege.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION: the roles, 00034's operator surface exactly, 00038's columns, the reach.
-- ---------------------------------------------------------------------------
-- The role checks are 00026's, in its order, as 00027-00034 repeated them: an op_* owned by a
-- SUPERUSER, a LOGIN or a member-carrying tappa_opdefiner is a general bypass with every other
-- check green, and a member of tappa_operator inherits EXECUTE on the two functions below.
-- 00034's state: the three functions it left, and the two CHECKs compared by their whole text
-- (the kind CHECK's NOT VALID suffix aside: this file's own Down leaves 00034's set NOT VALID
-- when rows of its kinds remain, and the Up must compose over that).
-- THE REACH: the grants below are column lists, and a column grant cannot take back a wider
-- privilege some other hand gave (00024 measured it) -- so nobody may already reach the two
-- columns, and tappa_opdefiner holds no table-level UPDATE on tenants.
-- +goose StatementBegin
DO $$
DECLARE
    v_missing text;
BEGIN
    SELECT string_agg(r.name, ', ' ORDER BY r.name)
      INTO v_missing
      FROM unnest(ARRAY['tappa_operator', 'tappa_opdefiner']) AS r(name)
     WHERE NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles p WHERE p.rolname = r.name);
    IF v_missing IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00039 (M11 API-1) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00039 (M11 API-1): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00039 (M11 API-1): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00039 (M11 API-1): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00039 (M11 API-1): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00039 (M11 API-1): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00039 (M11 API-1): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1: tappa_app gets no new privilege). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
         WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_audit(text, text, text, integer, integer)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_begin_read(text, text, jsonb)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_record_vat_check(text, uuid, text, boolean)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR pg_catalog.to_regprocedure('public.op_enable_tenant_api(text, uuid, text)') IS NOT NULL
       OR pg_catalog.to_regprocedure('public.op_disable_tenant_api(text, uuid)') IS NOT NULL
       OR NOT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                       WHERE a.attrelid = 'public.tenants'::regclass AND a.attname = 'api_code'
                         AND NOT a.attisdropped)
       OR NOT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                       WHERE a.attrelid = 'public.tenants'::regclass AND a.attname = 'api_enabled_at'
                         AND NOT a.attisdropped) THEN
        RAISE EXCEPTION 'migration 00039 (M11 API-1) replaces op_begin_read and op_read_audit as migration 00034 left them and writes the two columns 00038 added, and that state is not there (00034''s three functions, none of this file''s yet, 00038''s columns). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF (SELECT pg_catalog.replace(pg_catalog.pg_get_constraintdef(c.oid), ' NOT VALID', '')
          FROM pg_catalog.pg_constraint c
         WHERE c.conrelid = 'public.operator_audit_log'::regclass
           AND c.conname = 'operator_audit_log_kind_check')
         IS DISTINCT FROM 'CHECK ((kind = ANY (ARRAY[''login_failed''::text, ''unknown_email''::text, ''totp_failed''::text, ''locked''::text, ''enrollment_failed''::text, ''password_ok''::text, ''login''::text, ''enrollment''::text, ''logout''::text, ''read''::text, ''legal_publish''::text, ''operator_created''::text, ''operator_mfa_reset''::text, ''operator_disabled''::text, ''tenant_vat_checked''::text])))'
       OR (SELECT pg_catalog.pg_get_constraintdef(c.oid) FROM pg_catalog.pg_constraint c
            WHERE c.conrelid = 'public.operator_audit_log'::regclass
              AND c.conname = 'operator_audit_log_tenant_write_shape')
         IS DISTINCT FROM 'CHECK (((kind <> ALL (ARRAY[''tenant_vat_checked''::text])) OR ((target_tenant_id IS NOT NULL) AND (target_admin_id IS NULL) AND (target_scope IS NULL) AND (page_number IS NULL) AND (page_size IS NULL) AND (detail = ''{}''::jsonb))))' THEN
        RAISE EXCEPTION 'migration 00039 (M11 API-1) widens operator_audit_log_kind_check and operator_audit_log_tenant_write_shape as migration 00034 left them, and the constraints are not in that state -- a migration between 00034 and this one widened them; rebase this file onto it. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF pg_catalog.has_table_privilege('tappa_opdefiner', 'public.tenants', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_opdefiner', 'public.tenants', 'api_enabled_at', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_opdefiner', 'public.tenants', 'api_enabled_at', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_opdefiner', 'public.tenants', 'api_code', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_opdefiner', 'public.tenants', 'api_code', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.tenants', 'api_enabled_at', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.tenants', 'api_code', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.tenants', 'api_enabled_at', 'INSERT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.tenants', 'api_code', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00039 (M11 API-1) grants tappa_opdefiner column privileges on the two API columns of tenants, and a privilege no migration gave is already there (tappa_opdefiner reaches them or holds a table-level UPDATE on tenants, or tappa_app may write them). A column grant cannot narrow it. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The audit kinds and the tenant-write shape.
-- ---------------------------------------------------------------------------
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_audit_log
                WHERE kind <> ALL (ARRAY['login_failed', 'unknown_email', 'totp_failed', 'locked',
                                         'enrollment_failed', 'password_ok', 'login', 'enrollment',
                                         'logout', 'read', 'legal_publish', 'operator_created',
                                         'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                                         'tenant_api_disabled'])) THEN
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                            'tenant_api_disabled'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                            'tenant_api_disabled'));
    END IF;
END
$$;
-- +goose StatementEnd
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_tenant_write_shape;
ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_tenant_write_shape CHECK (
    kind <> ALL (ARRAY['tenant_vat_checked', 'tenant_api_enabled', 'tenant_api_disabled'])
    OR (target_tenant_id IS NOT NULL AND target_admin_id IS NULL AND target_scope IS NULL
        AND page_number IS NULL AND page_size IS NULL AND detail = '{}'::jsonb)
);

-- ---------------------------------------------------------------------------
-- 2. The grants -- the whole of what is new for tappa_opdefiner (the WHOLE resulting lists are
-- pinned in TestOperator00026_PrivilegeMatrix's allow-list):
--   tenants SELECT (api_enabled_at)          the enable keeps the first opening instant;
--   tenants UPDATE (api_enabled_at, api_code) the two columns, and the row lock both writes take
--                                             (FOR NO KEY UPDATE needs UPDATE on some column).
-- audit_log's K6 INSERT is 00034's; operator_audit_log's INSERT is 00026-00033's. Unchanged.
-- ---------------------------------------------------------------------------
GRANT SELECT (api_enabled_at) ON tenants TO tappa_opdefiner;
GRANT UPDATE (api_enabled_at, api_code) ON tenants TO tappa_opdefiner;

-- ===========================================================================
-- 3. op_begin_read, REPLACED: 00034's body with the two kinds in its log-read filter list.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_begin_read(p_session text, p_kind text, p_params jsonb)
    RETURNS text
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session     uuid;
    v_admin       uuid;
    v_keys        text[];
    v_want        text[];
    v_page_number integer;
    v_page_size   integer;
    v_query       text;
    v_filter      text;
    v_tenant      uuid;
    v_detail      jsonb := '{}'::jsonb;
    v_bound       jsonb;
    v_ticket      text;
    v_rows        bigint;
    v_constraint  text;
    v_state       text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    -- Nested rather than one OR chain: SQL does not promise to evaluate OR's arms in
    -- order, jsonb_object_keys on a non-object raises instead of answering false, and a
    -- NULL arm would turn the whole condition NULL -- which IF reads as false.
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'operator_audit', 'tenant_billing', 'tenant_vat') THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_keys := (SELECT array_agg(k.key ORDER BY k.key) FROM jsonb_object_keys(p_params) AS k(key));

    IF p_kind IN ('tenant_detail', 'tenant_plaques', 'tenant_vat') THEN
        IF v_keys IS DISTINCT FROM ARRAY['tenant_id']
           OR jsonb_typeof(p_params -> 'tenant_id') IS DISTINCT FROM 'string' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        IF ((p_params ->> 'tenant_id') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') IS NOT TRUE THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_tenant := (p_params ->> 'tenant_id')::uuid;
        v_bound := jsonb_build_object('tenant_id', v_tenant);
    ELSIF p_kind = 'tenant_billing' THEN
        -- One tenant and a page of twelve local months: EXACTLY {tenant_id, page_number},
        -- the page 1..5 (sixty months).
        IF v_keys IS DISTINCT FROM ARRAY['page_number', 'tenant_id']
           OR jsonb_typeof(p_params -> 'tenant_id') IS DISTINCT FROM 'string'
           OR jsonb_typeof(p_params -> 'page_number') IS DISTINCT FROM 'number' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        IF ((p_params ->> 'tenant_id') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') IS NOT TRUE
           OR (p_params ->> 'page_number') !~ '^[1-9][0-9]{0,8}$' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_tenant      := (p_params ->> 'tenant_id')::uuid;
        v_page_number := (p_params ->> 'page_number')::integer;
        IF v_page_number > 5 THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_page_size := 12;
        v_bound := jsonb_build_object('tenant_id', v_tenant, 'page_number', v_page_number);
    ELSE
        -- (Two IFs, not a CASE inside the condition: PL/pgSQL ends an IF condition at the
        -- first THEN it reads, a CASE's included.)
        IF p_kind = 'tenants' THEN
            v_want := ARRAY['page_number', 'page_size', 'query'];
        ELSIF p_kind = 'operator_audit' THEN
            v_want := ARRAY['kind', 'page_number', 'page_size'];
        ELSE
            v_want := ARRAY['page_number', 'page_size'];
        END IF;
        IF v_keys IS DISTINCT FROM v_want
           OR jsonb_typeof(p_params -> 'page_number') IS DISTINCT FROM 'number'
           OR jsonb_typeof(p_params -> 'page_size') IS DISTINCT FROM 'number' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        -- Plain positive integers only: page_number 1..999999999, page_size 1..200 (the
        -- LIMIT ceiling of ADR 0021 §2 iii, and operator_audit_log's own CHECK).
        IF (p_params ->> 'page_number') !~ '^[1-9][0-9]{0,8}$'
           OR (p_params ->> 'page_size') !~ '^[1-9][0-9]{0,2}$' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_page_number := (p_params ->> 'page_number')::integer;
        v_page_size   := (p_params ->> 'page_size')::integer;
        IF v_page_size > 200 THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        IF p_kind = 'tenants' THEN
            IF jsonb_typeof(p_params -> 'query') IS DISTINCT FROM 'string' THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_query := p_params ->> 'query';
            IF char_length(v_query) > 254 THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_bound := jsonb_build_object('page_number', v_page_number, 'page_size', v_page_size,
                                          'query', v_query);
            -- The search CLASS (section 3's rules, in their order).
            IF v_query = '' THEN
                v_detail := jsonb_build_object('search', 'none');
            ELSIF v_query ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
                v_detail := jsonb_build_object('search', 'id');
            ELSIF strpos(v_query, '@') > 0 THEN
                v_detail := jsonb_build_object('search', 'address');
            ELSE
                v_detail := jsonb_build_object('search', 'text');
            END IF;
        ELSIF p_kind = 'operator_audit' THEN
            -- The log's page bound and the kind filter: '' or a member of the audit kind set.
            IF v_page_number > 1000 THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            IF jsonb_typeof(p_params -> 'kind') IS DISTINCT FROM 'string' THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_filter := p_params ->> 'kind';
            IF v_filter <> '' AND v_filter NOT IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                                                   'enrollment_failed', 'password_ok', 'login', 'enrollment',
                                                   'logout', 'read', 'legal_publish', 'operator_created',
                                                   'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked',
                                                   'tenant_api_enabled', 'tenant_api_disabled') THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_bound := jsonb_build_object('kind', v_filter, 'page_number', v_page_number,
                                          'page_size', v_page_size);
            IF v_filter = '' THEN
                v_detail := jsonb_build_object('filter', 'all');
            ELSE
                v_detail := jsonb_build_object('filter', v_filter);
            END IF;
        ELSE
            v_bound := jsonb_build_object('page_number', v_page_number, 'page_size', v_page_size);
        END IF;
    END IF;

    v_ticket := encode(sha256(uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())), 'hex');

    BEGIN
        WITH audit AS (
            INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id,
                                                   target_scope, page_number, page_size, detail)
            VALUES ('read', v_session, v_admin, v_tenant, p_kind, v_page_number, v_page_size, v_detail)
            RETURNING id
        )
        INSERT INTO public.operator_read_tickets (ticket_hash, session_id, kind, target_tenant_id,
                                                  audit_id, expires_at)
        SELECT encode(sha256(convert_to(v_ticket || v_bound::text, 'UTF8')), 'hex'),
               v_session, p_kind, v_tenant, audit.id, clock_timestamp() + interval '30 seconds'
          FROM audit;

        GET DIAGNOSTICS v_rows = ROW_COUNT;
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_begin_read: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            v_rows := 0;
    END;

    IF v_rows <> 1 THEN
        RAISE EXCEPTION 'op_begin_read: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    RETURN v_ticket;
END;
$$;
-- +goose StatementEnd
-- CREATE OR REPLACE keeps the owner and the ACL; they are written again so this file, read
-- alone, shows the whole contract of the function it (re)defines (ADR 0021 §2 vi).
ALTER FUNCTION public.op_begin_read(text, text, jsonb) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_begin_read(text, text, jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_begin_read(text, text, jsonb) TO tappa_operator;

-- ===========================================================================
-- 4. op_read_audit, REPLACED: 00034's body with the two kinds in its filter shape. The rows
-- themselves need no new line: scope NULL and detail {} (section 1's CHECK) is the ELSE arm.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_read_audit(p_session text, p_ticket text, p_kind text,
                                     p_page_number integer, p_page_size integer)
    RETURNS TABLE (audit_id uuid, at timestamptz, kind text, session_id uuid,
                   actor_admin_id uuid, actor_name text,
                   target_admin_id uuid, target_admin_name text,
                   target_tenant_id uuid, target_tenant_name text,
                   target_scope text, page_number integer, page_size integer,
                   search_class text, filter_kind text, legal_slug text, legal_bytes integer,
                   detail_recognised boolean)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
    v_limit   integer;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    UPDATE public.operator_read_tickets AS k
       SET consumed_at = clock_timestamp()
     WHERE k.ticket_hash = encode(sha256(convert_to(
               p_ticket || jsonb_build_object('kind', p_kind,
                                              'page_number', p_page_number,
                                              'page_size', p_page_size)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'operator_audit'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_audit: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    v_limit := least(p_page_size, 200);

    RETURN QUERY
        WITH page AS (
            SELECT l.id, l.at, l.kind, l.session_id, l.actor_admin_id, l.target_admin_id,
                   l.target_tenant_id, l.target_scope, l.page_number, l.page_size, l.detail
              FROM public.operator_audit_log AS l
             WHERE p_kind = '' OR l.kind = p_kind
             ORDER BY l.at DESC, l.id DESC
             LIMIT v_limit
            OFFSET (least(p_page_number, 1000) - 1)::bigint * v_limit
        ), shaped AS (
            SELECT p.*,
                   coalesce(CASE
                       WHEN jsonb_typeof(p.detail) IS DISTINCT FROM 'object' THEN false
                       WHEN p.kind = 'read' AND p.target_scope = 'tenants' THEN
                            p.detail ? 'search' AND (p.detail - 'search') = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'search') = 'string'
                            AND (p.detail ->> 'search') IN ('none', 'text', 'address', 'id')
                       WHEN p.kind = 'read' AND p.target_scope = 'operator_audit' THEN
                            p.detail ? 'filter' AND (p.detail - 'filter') = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'filter') = 'string'
                            AND (p.detail ->> 'filter') IN ('all', 'login_failed', 'unknown_email',
                                                            'totp_failed', 'locked', 'enrollment_failed',
                                                            'password_ok', 'login', 'enrollment',
                                                            'logout', 'read', 'legal_publish',
                                                            'operator_created', 'operator_mfa_reset',
                                                            'operator_disabled', 'tenant_vat_checked',
                                                            'tenant_api_enabled', 'tenant_api_disabled')
                       WHEN p.kind = 'read' THEN
                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing',
                                                'tenant_vat')
                            AND p.detail = '{}'::jsonb
                       WHEN p.kind = 'legal_publish' THEN
                            p.target_scope IS NULL
                            AND p.detail ?& ARRAY['bytes', 'document_id', 'slug']
                            AND (p.detail - ARRAY['bytes', 'document_id', 'slug']) = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'slug') = 'string'
                            AND (p.detail ->> 'slug') IN ('privacy', 'terms', 'imprint', 'cookies')
                            AND jsonb_typeof(p.detail -> 'document_id') = 'string'
                            AND (p.detail ->> 'document_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                            AND jsonb_typeof(p.detail -> 'bytes') = 'number'
                            AND CASE WHEN (p.detail ->> 'bytes') ~ '^[1-9][0-9]{0,5}$'
                                     THEN (p.detail ->> 'bytes')::integer <= 262144
                                     ELSE false
                                END
                       ELSE p.target_scope IS NULL AND p.detail = '{}'::jsonb
                   END, false) AS recognised
              FROM page AS p
        )
        SELECT s.id, s.at, s.kind, s.session_id,
               s.actor_admin_id, actor_row.display_name,
               s.target_admin_id, target_row.display_name,
               s.target_tenant_id, tenant_row.name,
               CASE WHEN s.target_scope IN ('legal_versions', 'tenants', 'tenant_detail',
                                            'tenant_plaques', 'operator_audit', 'tenant_billing',
                                            'tenant_vat')
                    THEN s.target_scope END,
               s.page_number, s.page_size,
               CASE WHEN s.recognised AND s.kind = 'read' AND s.target_scope = 'tenants'
                    THEN s.detail ->> 'search' END,
               CASE WHEN s.recognised AND s.kind = 'read' AND s.target_scope = 'operator_audit'
                    THEN s.detail ->> 'filter' END,
               CASE WHEN s.recognised AND s.kind = 'legal_publish'
                    THEN s.detail ->> 'slug' END,
               CASE WHEN s.recognised AND s.kind = 'legal_publish'
                    THEN (s.detail ->> 'bytes')::integer END,
               s.recognised
          FROM shaped AS s
          LEFT JOIN public.platform_admins AS actor_row ON actor_row.id = s.actor_admin_id
          LEFT JOIN public.platform_admins AS target_row ON target_row.id = s.target_admin_id
          LEFT JOIN public.tenants AS tenant_row ON tenant_row.id = s.target_tenant_id
         ORDER BY s.at DESC, s.id DESC;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_audit(text, text, text, integer, integer) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_audit(text, text, text, integer, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_audit(text, text, text, integer, integer) TO tappa_operator;

-- ===========================================================================
-- 5. The two writes (the header gives their order and their answer).
-- ===========================================================================
-- +goose StatementBegin
CREATE FUNCTION public.op_enable_tenant_api(p_session text, p_tenant_id uuid, p_api_code text)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session    uuid;
    v_admin      uuid;
    v_at         timestamptz;
    v_constraint text;
    v_state      text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    IF p_tenant_id IS NULL OR p_api_code IS NULL
       OR p_api_code !~ '^[A-Z][A-Z0-9]{1,15}$' THEN
        RAISE EXCEPTION 'op_enable_tenant_api: change refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    BEGIN
        PERFORM t.id
           FROM public.tenants AS t
          WHERE t.id = p_tenant_id
            FOR NO KEY UPDATE;

        IF FOUND THEN
            v_at := clock_timestamp();

            UPDATE public.tenants AS t
               SET api_enabled_at = coalesce(t.api_enabled_at, v_at),
                   api_code = p_api_code
             WHERE t.id = p_tenant_id;

            INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail, at)
            VALUES (p_tenant_id, v_admin, 'tenant.api_enabled', p_tenant_id::text,
                    jsonb_build_object('actor_kind', 'operator', 'api_code', p_api_code),
                    v_at);
        END IF;

        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id)
        VALUES ('tenant_api_enabled', v_session, v_admin, p_tenant_id);
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_enable_tenant_api: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_enable_tenant_api: change refused'
                USING ERRCODE = 'invalid_parameter_value';
    END;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_enable_tenant_api(text, uuid, text) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_enable_tenant_api(text, uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_enable_tenant_api(text, uuid, text) TO tappa_operator;

-- +goose StatementBegin
CREATE FUNCTION public.op_disable_tenant_api(p_session text, p_tenant_id uuid)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session    uuid;
    v_admin      uuid;
    v_at         timestamptz;
    v_constraint text;
    v_state      text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    IF p_tenant_id IS NULL THEN
        RAISE EXCEPTION 'op_disable_tenant_api: change refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    BEGIN
        PERFORM t.id
           FROM public.tenants AS t
          WHERE t.id = p_tenant_id
            FOR NO KEY UPDATE;

        IF FOUND THEN
            v_at := clock_timestamp();

            UPDATE public.tenants AS t
               SET api_enabled_at = NULL
             WHERE t.id = p_tenant_id;

            INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail, at)
            VALUES (p_tenant_id, v_admin, 'tenant.api_disabled', p_tenant_id::text,
                    jsonb_build_object('actor_kind', 'operator'),
                    v_at);
        END IF;

        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id)
        VALUES ('tenant_api_disabled', v_session, v_admin, p_tenant_id);
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_disable_tenant_api: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_disable_tenant_api: change refused'
                USING ERRCODE = 'invalid_parameter_value';
    END;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_disable_tenant_api(text, uuid) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_disable_tenant_api(text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_disable_tenant_api(text, uuid) TO tappa_operator;

-- +goose Down

-- 🔴 WHAT A SUCCESSFUL Down DOES (00034's house style):
--   * the two functions go; op_read_audit and op_begin_read go back to 00034's Up bodies
--     VERBATIM -- replaced, not dropped, so their owners and ACLs stay;
--   * tappa_opdefiner loses EXACTLY what section 2 granted, by column -- not REVOKE ALL, which
--     would take 00029's, 00032's and 00034's tenants columns with it;
--   * operator_audit_log_tenant_write_shape goes back to 00034's one kind; the kind CHECK to
--     00034's fifteen -- NOT VALID when a row of a kind outside that set exists (this file's or a
--     later migration's), VALIDATED otherwise;
--   * the rows STAY: the operator's 'tenant_api_*' rows (operator_audit_log is append-only), the
--     tenants' 'tenant.api_*' rows (audit_log is append-only) and the columns' values (00038's
--     Down is what takes the columns away).

DROP FUNCTION IF EXISTS public.op_disable_tenant_api(text, uuid);
DROP FUNCTION IF EXISTS public.op_enable_tenant_api(text, uuid, text);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_read_audit(p_session text, p_ticket text, p_kind text,
                                     p_page_number integer, p_page_size integer)
    RETURNS TABLE (audit_id uuid, at timestamptz, kind text, session_id uuid,
                   actor_admin_id uuid, actor_name text,
                   target_admin_id uuid, target_admin_name text,
                   target_tenant_id uuid, target_tenant_name text,
                   target_scope text, page_number integer, page_size integer,
                   search_class text, filter_kind text, legal_slug text, legal_bytes integer,
                   detail_recognised boolean)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
    v_limit   integer;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    UPDATE public.operator_read_tickets AS k
       SET consumed_at = clock_timestamp()
     WHERE k.ticket_hash = encode(sha256(convert_to(
               p_ticket || jsonb_build_object('kind', p_kind,
                                              'page_number', p_page_number,
                                              'page_size', p_page_size)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'operator_audit'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_audit: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    v_limit := least(p_page_size, 200);

    RETURN QUERY
        WITH page AS (
            SELECT l.id, l.at, l.kind, l.session_id, l.actor_admin_id, l.target_admin_id,
                   l.target_tenant_id, l.target_scope, l.page_number, l.page_size, l.detail
              FROM public.operator_audit_log AS l
             WHERE p_kind = '' OR l.kind = p_kind
             ORDER BY l.at DESC, l.id DESC
             LIMIT v_limit
            OFFSET (least(p_page_number, 1000) - 1)::bigint * v_limit
        ), shaped AS (
            SELECT p.*,
                   coalesce(CASE
                       WHEN jsonb_typeof(p.detail) IS DISTINCT FROM 'object' THEN false
                       WHEN p.kind = 'read' AND p.target_scope = 'tenants' THEN
                            p.detail ? 'search' AND (p.detail - 'search') = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'search') = 'string'
                            AND (p.detail ->> 'search') IN ('none', 'text', 'address', 'id')
                       WHEN p.kind = 'read' AND p.target_scope = 'operator_audit' THEN
                            p.detail ? 'filter' AND (p.detail - 'filter') = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'filter') = 'string'
                            AND (p.detail ->> 'filter') IN ('all', 'login_failed', 'unknown_email',
                                                            'totp_failed', 'locked', 'enrollment_failed',
                                                            'password_ok', 'login', 'enrollment',
                                                            'logout', 'read', 'legal_publish',
                                                            'operator_created', 'operator_mfa_reset',
                                                            'operator_disabled', 'tenant_vat_checked')
                       WHEN p.kind = 'read' THEN
                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing',
                                                'tenant_vat')
                            AND p.detail = '{}'::jsonb
                       WHEN p.kind = 'legal_publish' THEN
                            p.target_scope IS NULL
                            AND p.detail ?& ARRAY['bytes', 'document_id', 'slug']
                            AND (p.detail - ARRAY['bytes', 'document_id', 'slug']) = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'slug') = 'string'
                            AND (p.detail ->> 'slug') IN ('privacy', 'terms', 'imprint', 'cookies')
                            AND jsonb_typeof(p.detail -> 'document_id') = 'string'
                            AND (p.detail ->> 'document_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                            AND jsonb_typeof(p.detail -> 'bytes') = 'number'
                            AND CASE WHEN (p.detail ->> 'bytes') ~ '^[1-9][0-9]{0,5}$'
                                     THEN (p.detail ->> 'bytes')::integer <= 262144
                                     ELSE false
                                END
                       ELSE p.target_scope IS NULL AND p.detail = '{}'::jsonb
                   END, false) AS recognised
              FROM page AS p
        )
        SELECT s.id, s.at, s.kind, s.session_id,
               s.actor_admin_id, actor_row.display_name,
               s.target_admin_id, target_row.display_name,
               s.target_tenant_id, tenant_row.name,
               CASE WHEN s.target_scope IN ('legal_versions', 'tenants', 'tenant_detail',
                                            'tenant_plaques', 'operator_audit', 'tenant_billing',
                                            'tenant_vat')
                    THEN s.target_scope END,
               s.page_number, s.page_size,
               CASE WHEN s.recognised AND s.kind = 'read' AND s.target_scope = 'tenants'
                    THEN s.detail ->> 'search' END,
               CASE WHEN s.recognised AND s.kind = 'read' AND s.target_scope = 'operator_audit'
                    THEN s.detail ->> 'filter' END,
               CASE WHEN s.recognised AND s.kind = 'legal_publish'
                    THEN s.detail ->> 'slug' END,
               CASE WHEN s.recognised AND s.kind = 'legal_publish'
                    THEN (s.detail ->> 'bytes')::integer END,
               s.recognised
          FROM shaped AS s
          LEFT JOIN public.platform_admins AS actor_row ON actor_row.id = s.actor_admin_id
          LEFT JOIN public.platform_admins AS target_row ON target_row.id = s.target_admin_id
          LEFT JOIN public.tenants AS tenant_row ON tenant_row.id = s.target_tenant_id
         ORDER BY s.at DESC, s.id DESC;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_audit(text, text, text, integer, integer) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_audit(text, text, text, integer, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_audit(text, text, text, integer, integer) TO tappa_operator;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_begin_read(p_session text, p_kind text, p_params jsonb)
    RETURNS text
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session     uuid;
    v_admin       uuid;
    v_keys        text[];
    v_want        text[];
    v_page_number integer;
    v_page_size   integer;
    v_query       text;
    v_filter      text;
    v_tenant      uuid;
    v_detail      jsonb := '{}'::jsonb;
    v_bound       jsonb;
    v_ticket      text;
    v_rows        bigint;
    v_constraint  text;
    v_state       text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    -- Nested rather than one OR chain: SQL does not promise to evaluate OR's arms in
    -- order, jsonb_object_keys on a non-object raises instead of answering false, and a
    -- NULL arm would turn the whole condition NULL -- which IF reads as false.
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'operator_audit', 'tenant_billing', 'tenant_vat') THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_keys := (SELECT array_agg(k.key ORDER BY k.key) FROM jsonb_object_keys(p_params) AS k(key));

    IF p_kind IN ('tenant_detail', 'tenant_plaques', 'tenant_vat') THEN
        IF v_keys IS DISTINCT FROM ARRAY['tenant_id']
           OR jsonb_typeof(p_params -> 'tenant_id') IS DISTINCT FROM 'string' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        IF ((p_params ->> 'tenant_id') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') IS NOT TRUE THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_tenant := (p_params ->> 'tenant_id')::uuid;
        v_bound := jsonb_build_object('tenant_id', v_tenant);
    ELSIF p_kind = 'tenant_billing' THEN
        -- One tenant and a page of twelve local months: EXACTLY {tenant_id, page_number},
        -- the page 1..5 (sixty months).
        IF v_keys IS DISTINCT FROM ARRAY['page_number', 'tenant_id']
           OR jsonb_typeof(p_params -> 'tenant_id') IS DISTINCT FROM 'string'
           OR jsonb_typeof(p_params -> 'page_number') IS DISTINCT FROM 'number' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        IF ((p_params ->> 'tenant_id') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') IS NOT TRUE
           OR (p_params ->> 'page_number') !~ '^[1-9][0-9]{0,8}$' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_tenant      := (p_params ->> 'tenant_id')::uuid;
        v_page_number := (p_params ->> 'page_number')::integer;
        IF v_page_number > 5 THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_page_size := 12;
        v_bound := jsonb_build_object('tenant_id', v_tenant, 'page_number', v_page_number);
    ELSE
        -- (Two IFs, not a CASE inside the condition: PL/pgSQL ends an IF condition at the
        -- first THEN it reads, a CASE's included.)
        IF p_kind = 'tenants' THEN
            v_want := ARRAY['page_number', 'page_size', 'query'];
        ELSIF p_kind = 'operator_audit' THEN
            v_want := ARRAY['kind', 'page_number', 'page_size'];
        ELSE
            v_want := ARRAY['page_number', 'page_size'];
        END IF;
        IF v_keys IS DISTINCT FROM v_want
           OR jsonb_typeof(p_params -> 'page_number') IS DISTINCT FROM 'number'
           OR jsonb_typeof(p_params -> 'page_size') IS DISTINCT FROM 'number' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        -- Plain positive integers only: page_number 1..999999999, page_size 1..200 (the
        -- LIMIT ceiling of ADR 0021 §2 iii, and operator_audit_log's own CHECK).
        IF (p_params ->> 'page_number') !~ '^[1-9][0-9]{0,8}$'
           OR (p_params ->> 'page_size') !~ '^[1-9][0-9]{0,2}$' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_page_number := (p_params ->> 'page_number')::integer;
        v_page_size   := (p_params ->> 'page_size')::integer;
        IF v_page_size > 200 THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        IF p_kind = 'tenants' THEN
            IF jsonb_typeof(p_params -> 'query') IS DISTINCT FROM 'string' THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_query := p_params ->> 'query';
            IF char_length(v_query) > 254 THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_bound := jsonb_build_object('page_number', v_page_number, 'page_size', v_page_size,
                                          'query', v_query);
            -- The search CLASS (section 3's rules, in their order).
            IF v_query = '' THEN
                v_detail := jsonb_build_object('search', 'none');
            ELSIF v_query ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
                v_detail := jsonb_build_object('search', 'id');
            ELSIF strpos(v_query, '@') > 0 THEN
                v_detail := jsonb_build_object('search', 'address');
            ELSE
                v_detail := jsonb_build_object('search', 'text');
            END IF;
        ELSIF p_kind = 'operator_audit' THEN
            -- The log's page bound and the kind filter: '' or a member of the audit kind set.
            IF v_page_number > 1000 THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            IF jsonb_typeof(p_params -> 'kind') IS DISTINCT FROM 'string' THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_filter := p_params ->> 'kind';
            IF v_filter <> '' AND v_filter NOT IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                                                   'enrollment_failed', 'password_ok', 'login', 'enrollment',
                                                   'logout', 'read', 'legal_publish', 'operator_created',
                                                   'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked') THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_bound := jsonb_build_object('kind', v_filter, 'page_number', v_page_number,
                                          'page_size', v_page_size);
            IF v_filter = '' THEN
                v_detail := jsonb_build_object('filter', 'all');
            ELSE
                v_detail := jsonb_build_object('filter', v_filter);
            END IF;
        ELSE
            v_bound := jsonb_build_object('page_number', v_page_number, 'page_size', v_page_size);
        END IF;
    END IF;

    v_ticket := encode(sha256(uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())), 'hex');

    BEGIN
        WITH audit AS (
            INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id,
                                                   target_scope, page_number, page_size, detail)
            VALUES ('read', v_session, v_admin, v_tenant, p_kind, v_page_number, v_page_size, v_detail)
            RETURNING id
        )
        INSERT INTO public.operator_read_tickets (ticket_hash, session_id, kind, target_tenant_id,
                                                  audit_id, expires_at)
        SELECT encode(sha256(convert_to(v_ticket || v_bound::text, 'UTF8')), 'hex'),
               v_session, p_kind, v_tenant, audit.id, clock_timestamp() + interval '30 seconds'
          FROM audit;

        GET DIAGNOSTICS v_rows = ROW_COUNT;
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_begin_read: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            v_rows := 0;
    END;

    IF v_rows <> 1 THEN
        RAISE EXCEPTION 'op_begin_read: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    RETURN v_ticket;
END;
$$;
-- +goose StatementEnd
-- CREATE OR REPLACE keeps the owner and the ACL; they are written again so this file, read
-- alone, shows the whole contract of the function it (re)defines (ADR 0021 §2 vi).
ALTER FUNCTION public.op_begin_read(text, text, jsonb) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_begin_read(text, text, jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_begin_read(text, text, jsonb) TO tappa_operator;

REVOKE UPDATE (api_enabled_at, api_code) ON tenants FROM tappa_opdefiner;
REVOKE SELECT (api_enabled_at) ON tenants FROM tappa_opdefiner;

ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_tenant_write_shape;
ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_tenant_write_shape CHECK (
    kind <> ALL (ARRAY['tenant_vat_checked'])
    OR (target_tenant_id IS NOT NULL AND target_admin_id IS NULL AND target_scope IS NULL
        AND page_number IS NULL AND page_size IS NULL AND detail = '{}'::jsonb)
);

ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_audit_log
                WHERE kind <> ALL (ARRAY['login_failed', 'unknown_email', 'totp_failed', 'locked',
                                         'enrollment_failed', 'password_ok', 'login', 'enrollment',
                                         'logout', 'read', 'legal_publish', 'operator_created',
                                         'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked'])) THEN
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked'));
    END IF;
END
$$;
-- +goose StatementEnd
