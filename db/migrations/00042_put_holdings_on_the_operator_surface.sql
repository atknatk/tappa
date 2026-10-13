-- 00042 -- M11 API-1a: the holdings on the operator's surface. The three writers of a holding's
-- membership (op_create_holding, op_add_holding_member, op_remove_holding_member), their one
-- read (op_read_holdings), the operator's audit kinds for them, and the API code that tells two
-- members of one holding apart -- in both directions (op_add_holding_member refuses a tenant
-- whose code a member carries; op_enable_tenant_api, REPLACED here, refuses a code a peer
-- carries). The holding tables, holding_peer and the timezone lock are 00041's.
--
-- NORMATIVE SOURCES: docs/adr/0027-holding-ve-capraz-tenant-dokunusu.md §1 (the three op_*, the
-- advisory lock and the canonical `tenants` locks, the audit table, K-36's comparison) and its
-- "API-1a uygulama notu" (the API code rule, the read); docs/adr/0021-op-fonksiyonlari-tenant-otesi-erisim.md
-- §2 (the op_* contract: the session first, one-phase writes returning void or the uuid they
-- created and NOTHING that depends on what was there before -- v 7 --, B14, two-phase reads, the
-- wall clock -- vii), §3 (inside an op_* the explicit tenant filter is the ONLY barrier; K6) and
-- §6 (the catalogue and behaviour pins), and its M11 note; docs/adr/0028-dis-api-anahtar-akis-webhook.md
-- §2 (K-22: op_enable_tenant_api is the API code's one writer). Where this file decides
-- something those leave open, the decision is written next to the statement.
--
-- WHAT IT DOES, IN ORDER:
--   0. the precondition: the two operator roles' shape and memberships (00026's checks, as
--      00027-00039 repeated them), 00041's objects in place, the database at 00039 on the
--      surface this file replaces (two CHECKs and the ticket CHECK by their full text, three
--      bodies by md5(prosrc) -- a migration that changed them in between stops this file
--      instead of being silently undone by it), none of this file's objects yet, and nobody
--      already reaching what the grants below give;
--   1. tappa_opdefiner's grants: what its four new functions read and write;
--   2. the operator's audit kinds holding_created, holding_member_added, holding_member_removed
--      and their row shape (operator_audit_log_holding_write_shape); the read kind 'holdings';
--   3. op_begin_read and 4. op_read_audit REPLACED: 00039's bodies, byte for byte, with the read
--      kind and the three audit kinds added to the lists each carries (and op_read_audit's
--      recognition of the holding rows' detail shape);
--   5. op_create_holding, op_add_holding_member, op_remove_holding_member;
--   6. op_read_holdings;
--   7. op_enable_tenant_api REPLACED.
--
-- 🔴 THE WRITES ANSWER THE SAME WHATEVER THEY FIND (ADR 0021 §2 v 7, B14). op_add_holding_member
-- REFUSES -- writes no membership and no tenant row -- an unknown holding, an unknown tenant, a
-- tenant that has a membership (live, or ended less than 60 seconds ago), a timezone that is not
-- the holding's members' (K-36), and an API code a member already carries; and it answers void in
-- every one of those cases exactly as it does when it adds. op_remove_holding_member answers void
-- whether there was a live membership to end or not; op_enable_tenant_api answers void whether
-- the code was written or a peer's equal code refused it. Each writes the OPERATOR's row on every
-- accepted call (00039's rule: the row records the act that was asked for) and the TENANTS' rows
-- only when something changed. The operator sees the outcome through op_read_holdings -- a
-- two-phase, audited read, which is what v 7 prescribes ("sonucu görmek isteyen iki aşamalı bir
-- okuma yapar"). ⚠️ COUNTED, NOT CLOSED (ADR 0021 limit 15, LV3's class): a call inside a
-- SAVEPOINT that is rolled back tells, through the statistics views or a lock wait, more than
-- its void -- here, whether a tenant could join a holding (its zone, its membership, its code);
-- every such call that commits leaves the operator row.
--
-- 🔴 SERIALISATION (ADR 0027 §1, measured by its rounds 1 and 2 and again for these files):
--   * every write that changes a holding's membership, and every write of a member's API code,
--     first takes pg_advisory_xact_lock(hashtextextended('holding-membership:' || <holding id>, 0))
--     -- db/queries/invites.sql's pattern; both functions are PUBLIC's, so no new privilege;
--   * op_add_holding_member then locks the `tenants` rows of the holding's members FOR SHARE and
--     the added tenant's FOR NO KEY UPDATE, in canonical order (smaller id first), and only then
--     compares -- an owner's timezone UPDATE holds the row FOR NO KEY UPDATE, so either the add
--     waits and its READ COMMITTED re-read sees the new zone, or the UPDATE waits and 00041's
--     trigger sees the committed membership. Two adds of ONE tenant to two holdings queue on its
--     row; two adds of two tenants to one holding queue on the advisory lock;
--   * op_enable_tenant_api takes the advisory lock of the tenant's holding (if it has one), then
--     the tenant's row FOR NO KEY UPDATE and its peers' FOR SHARE, in canonical order. Its holding
--     is read before the lock; if the tenant joined a holding between that read and its own row
--     lock, the call writes nothing (the tenant row lock makes that the only window, and refusing
--     inside it keeps op_add_holding_member's lock order -- advisory, then rows).
--   The row locks need no new privilege: tappa_opdefiner's UPDATE on four `tenants` columns
--   (00034, 00039) meets a row lock's "UPDATE on some column" (ADR 0027 "Bağlam"); were those
--   grants ever removed the lock fails closed with permission denied.
--
-- §4.5 -- CROSSED ON PURPOSE, IN THE PLACE ADR 0021 ALLOWS. The op_* belong to tappa_opdefiner
-- and see every tenant: the explicit `p_tenant_id` / `p_holding_id` on every statement is the
-- only barrier, as in every op_*.
--
-- CLOCKS: every instant the op_* compare or write is clock_timestamp() (ADR 0021 §2 vii); the K6
-- tenant rows' `at` is the instant the membership row took from its DEFAULT (RETURNING), or the
-- one left_at was set to -- one read per call. TestOperator00026_NoFrozenClock walks them.
--
-- SECRETS: none. Holding names and API codes are labels; every RAISE carries a constant message;
-- the formatted ones are the precondition's (role names) and the writes' LOG lines (a constraint
-- name and a SQLSTATE).

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
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
        RAISE EXCEPTION 'migration 00042 (M11 API-1a) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00042 (M11 API-1a): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00042 (M11 API-1a): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00042 (M11 API-1a): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00042 (M11 API-1a): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00042 (M11 API-1a): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00042 (M11 API-1a): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1: tappa_app gets no new privilege). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF pg_catalog.to_regclass('public.holdings') IS NULL
       OR pg_catalog.to_regclass('public.holding_members') IS NULL
       OR pg_catalog.to_regprocedure('public.holding_peer(uuid, uuid, text)') IS NULL
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_enable_tenant_api(text, uuid, text)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_audit(text, text, text, integer, integer)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_begin_read(text, text, jsonb)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR pg_catalog.to_regprocedure('public.op_disable_tenant_api(text, uuid)') IS NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                   WHERE p.proname IN ('op_create_holding', 'op_add_holding_member',
                                       'op_remove_holding_member', 'op_read_holdings')) THEN
        RAISE EXCEPTION 'migration 00042 (M11 API-1a) puts migration 00041''s holdings on the operator surface and replaces op_begin_read, op_read_audit and op_enable_tenant_api as migration 00039 left them, and that state is not there (00041''s tables and holding_peer, 00039''s functions, none of this file''s yet). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF (SELECT pg_catalog.replace(pg_catalog.pg_get_constraintdef(c.oid), ' NOT VALID', '')
          FROM pg_catalog.pg_constraint c
         WHERE c.conrelid = 'public.operator_audit_log'::regclass
           AND c.conname = 'operator_audit_log_kind_check')
         IS DISTINCT FROM 'CHECK ((kind = ANY (ARRAY[''login_failed''::text, ''unknown_email''::text, ''totp_failed''::text, ''locked''::text, ''enrollment_failed''::text, ''password_ok''::text, ''login''::text, ''enrollment''::text, ''logout''::text, ''read''::text, ''legal_publish''::text, ''operator_created''::text, ''operator_mfa_reset''::text, ''operator_disabled''::text, ''tenant_vat_checked''::text, ''tenant_api_enabled''::text, ''tenant_api_disabled''::text])))'
       OR (SELECT pg_catalog.pg_get_constraintdef(c.oid) FROM pg_catalog.pg_constraint c
            WHERE c.conrelid = 'public.operator_audit_log'::regclass
              AND c.conname = 'operator_audit_log_tenant_write_shape')
         IS DISTINCT FROM 'CHECK (((kind <> ALL (ARRAY[''tenant_vat_checked''::text, ''tenant_api_enabled''::text, ''tenant_api_disabled''::text])) OR ((target_tenant_id IS NOT NULL) AND (target_admin_id IS NULL) AND (target_scope IS NULL) AND (page_number IS NULL) AND (page_size IS NULL) AND (detail = ''{}''::jsonb))))'
       OR (SELECT pg_catalog.replace(pg_catalog.pg_get_constraintdef(c.oid), ' NOT VALID', '')
             FROM pg_catalog.pg_constraint c
            WHERE c.conrelid = 'public.operator_read_tickets'::regclass
              AND c.conname = 'operator_read_tickets_kind_check')
         IS DISTINCT FROM 'CHECK ((kind = ANY (ARRAY[''legal_versions''::text, ''tenants''::text, ''tenant_detail''::text, ''tenant_plaques''::text, ''operator_audit''::text, ''tenant_billing''::text, ''tenant_vat''::text])))'
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_constraint c
                   WHERE c.conrelid = 'public.operator_audit_log'::regclass
                     AND c.conname = 'operator_audit_log_holding_write_shape') THEN
        RAISE EXCEPTION 'migration 00042 (M11 API-1a) widens operator_audit_log_kind_check and operator_read_tickets_kind_check and adds operator_audit_log_holding_write_shape over the state migration 00039 left, and the constraints are not in that state -- a migration between 00039 and this one changed them; rebase this file onto it. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- THE THREE BODIES, BY DIGEST (00039's precondition, one step on): md5(prosrc) of 00039's
    -- Up bodies. This file's Down gives those back verbatim, so the Up still composes over it.
    -- A rebase onto a migration that changed one takes its body and its digest here.
    IF (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = pg_catalog.to_regprocedure('public.op_begin_read(text, text, jsonb)'))
         IS DISTINCT FROM 'd1327314ee37600077e453664988eb6b'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_audit(text, text, text, integer, integer)'))
         IS DISTINCT FROM '3182b5376cff359ed521631fa95dbac1'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_enable_tenant_api(text, uuid, text)'))
         IS DISTINCT FROM 'a385fee5eadf7c641fd6393d6f6e1b08' THEN
        RAISE EXCEPTION 'migration 00042 (M11 API-1a) replaces op_begin_read, op_read_audit and op_enable_tenant_api whole with migration 00039''s bodies plus its edits, and a live body is not 00039''s -- a migration between 00039 and this one changed it, and this file would silently undo that; rebase this file onto it. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- THE REACH: the grants below are column lists, and a column grant cannot take back a wider
    -- privilege some other hand gave (00024 measured it). tappa_opdefiner holds nothing on the
    -- two holding tables yet, no table-level SELECT on tenants, and does not read api_code.
    IF pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.holdings', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.holdings', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.holdings', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.holdings', 'DELETE')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.holding_members', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.holding_members', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.holding_members', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.holding_members', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.tenants', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_opdefiner', 'public.tenants', 'api_code', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00042 (M11 API-1a) grants tappa_opdefiner column privileges on the two holding tables and on tenants.api_code, and a privilege no migration gave is already there. A column grant cannot narrow it. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The grants -- each column named, each one a function's need below.
-- ---------------------------------------------------------------------------
-- tappa_opdefiner (ADR 0021's class):
--   holdings        SELECT (id, name, created_at)   op_add_holding_member's existence check,
--                                                   op_read_holdings
--                   INSERT (name)                   op_create_holding (id and time are DEFAULTs)
--   holding_members SELECT (holding_id, member_tenant_id, joined_at, left_at)
--                                                   the membership rules, the K6 rows' "other
--                                                   members", op_read_holdings, and the code rule
--                   INSERT (holding_id, member_tenant_id)
--                                                   op_add_holding_member (joined_at is the DEFAULT)
--                   UPDATE (left_at)                op_remove_holding_member -- one column, so no
--                                                   op_* can move a membership to another holding
--                                                   or another tenant
--   tenants         SELECT (api_code)               the code rule in both directions, and
--                                                   op_read_holdings (id, name, timezone are 00029's)
GRANT SELECT (id, name, created_at) ON holdings TO tappa_opdefiner;
GRANT INSERT (name) ON holdings TO tappa_opdefiner;
GRANT SELECT (holding_id, member_tenant_id, joined_at, left_at) ON holding_members TO tappa_opdefiner;
GRANT INSERT (holding_id, member_tenant_id) ON holding_members TO tappa_opdefiner;
GRANT UPDATE (left_at) ON holding_members TO tappa_opdefiner;
GRANT SELECT (api_code) ON tenants TO tappa_opdefiner;

-- ---------------------------------------------------------------------------
-- 2. The operator's audit kinds, the holding rows' shape, the read kind.
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
                                         'tenant_api_disabled', 'holding_created', 'holding_member_added',
                                         'holding_member_removed'])) THEN
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                            'tenant_api_disabled', 'holding_created', 'holding_member_added',
                            'holding_member_removed'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                            'tenant_api_disabled', 'holding_created', 'holding_member_added',
                            'holding_member_removed'));
    END IF;
END
$$;
-- +goose StatementEnd

-- A holding row names the holding in its detail and nothing else -- {"holding_id": "<uuid>"} --
-- and no account, scope or page; the two membership kinds name their tenant, holding_created
-- names none (a new holding has no member).
ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_holding_write_shape CHECK (
    kind <> ALL (ARRAY['holding_created', 'holding_member_added', 'holding_member_removed'])
    OR (target_admin_id IS NULL AND target_scope IS NULL AND page_number IS NULL AND page_size IS NULL
        AND (target_tenant_id IS NULL) = (kind = 'holding_created')
        AND detail ? 'holding_id' AND (detail - 'holding_id') = '{}'::jsonb
        AND jsonb_typeof(detail -> 'holding_id') = 'string'
        AND (detail ->> 'holding_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')
);

ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_read_tickets
                WHERE kind <> ALL (ARRAY['legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                                         'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings'])) THEN
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings'));
    END IF;
END
$$;
-- +goose StatementEnd

-- ===========================================================================
-- 3. op_begin_read, REPLACED: 00039's body with the read kind 'holdings' in its kind list (the
-- {page_number, page_size} branch takes it as it takes 'legal_versions') and the three holding
-- kinds in its log-read filter list.
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
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings') THEN
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
                                                   'tenant_api_enabled', 'tenant_api_disabled',
                                                   'holding_created', 'holding_member_added',
                                                   'holding_member_removed') THEN
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
-- 4. op_read_audit, REPLACED: 00039's body with the three holding kinds in its filter shape,
-- 'holdings' in its two scope lists, and one arm that recognises a holding row's detail
-- ({"holding_id": "<uuid>"}, scope NULL) -- the shape section 3's CHECK already holds them to.
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
                                                            'tenant_api_enabled', 'tenant_api_disabled',
                                                            'holding_created', 'holding_member_added',
                                                            'holding_member_removed')
                       WHEN p.kind = 'read' THEN
                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing',
                                                'tenant_vat', 'holdings')
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
                       WHEN p.kind IN ('holding_created', 'holding_member_added', 'holding_member_removed') THEN
                            p.target_scope IS NULL
                            AND p.detail ? 'holding_id' AND (p.detail - 'holding_id') = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'holding_id') = 'string'
                            AND (p.detail ->> 'holding_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
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
                                            'tenant_vat', 'holdings')
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
-- 5. The three writers (ADR 0027 §1; ADR 0021's class). ONE PHASE: the change, the tenants' K6
-- rows and the operator's row are one call. A constraint that refuses (a race the locks lose to
-- the unique index, say) is caught -- one fixed 22023 and a LOG line with the constraint's name
-- and the SQLSTATE (00027's pattern).
-- ===========================================================================

-- op_create_holding: an empty holding. Its answer is the id it created (ADR 0021 §2 v 7 allows
-- exactly that), its name is refused with 22023 before anything is written when it breaks
-- holdings_name_shape, and its operator row names the holding and no tenant.
-- +goose StatementBegin
CREATE FUNCTION public.op_create_holding(p_session text, p_name text)
    RETURNS uuid
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session    uuid;
    v_admin      uuid;
    v_holding    uuid;
    v_constraint text;
    v_state      text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    IF p_name IS NULL OR p_name <> btrim(p_name) OR char_length(p_name) NOT BETWEEN 1 AND 120
       OR p_name ~ '[[:cntrl:]]' THEN
        RAISE EXCEPTION 'op_create_holding: change refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    BEGIN
        INSERT INTO public.holdings (name)
        VALUES (p_name)
        RETURNING id INTO v_holding;

        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, detail)
        VALUES ('holding_created', v_session, v_admin, jsonb_build_object('holding_id', v_holding));
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_create_holding: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_create_holding: change refused'
                USING ERRCODE = 'invalid_parameter_value';
    END;

    RETURN v_holding;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_create_holding(text, text) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_create_holding(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_create_holding(text, text) TO tappa_operator;

-- op_add_holding_member: the tenant joins the holding -- only if, decided AFTER the locks (the
-- header's SERIALISATION), the holding and the tenant exist, the tenant has no membership live or
-- ended less than 60 seconds ago (in ANY holding -- ADR 0027 §1, "aynı anda tek üyelik"), every
-- member's timezone (live or in its exit window) is the tenant's (K-36), and no such member
-- carries the tenant's API code (NULL codes never collide). The 60 seconds are ADR 0027 §6's
-- tolerance, copied here because this function cannot call holding_peer (whose EXECUTE is
-- tappa_holdingdefiner's alone); TestHoldingOps_RejoinWaitsForTheToleranceWindow holds the
-- copies equal.
-- K6 (ADR 0027 §1's table), only when the membership was written, at the instant joined_at took:
-- 'tenant.holding_joined' in the added tenant's audit_log, 'tenant.holding_member_joined' in
-- every OTHER live member's (detail naming the holding and the tenant that joined). The operator's
-- 'holding_member_added' row on every accepted call, naming the tenant and the holding.
-- +goose StatementBegin
CREATE FUNCTION public.op_add_holding_member(p_session text, p_holding_id uuid, p_tenant_id uuid)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session    uuid;
    v_admin      uuid;
    v_row        record;
    v_found      boolean := false;
    v_zone       text;
    v_code       text;
    v_ok         boolean;
    v_at         timestamptz;
    v_constraint text;
    v_state      text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    IF p_holding_id IS NULL OR p_tenant_id IS NULL THEN
        RAISE EXCEPTION 'op_add_holding_member: change refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    PERFORM pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended('holding-membership:' || p_holding_id::text, 0));

    BEGIN
        FOR v_row IN
            SELECT x.id
              FROM (SELECT p_tenant_id AS id
                    UNION
                    SELECT m.member_tenant_id
                      FROM public.holding_members AS m
                     WHERE m.holding_id = p_holding_id
                       AND (m.left_at IS NULL OR m.left_at + interval '60 seconds' > clock_timestamp())) AS x
             ORDER BY x.id
        LOOP
            IF v_row.id = p_tenant_id THEN
                SELECT t.timezone, t.api_code
                  INTO v_zone, v_code
                  FROM public.tenants AS t
                 WHERE t.id = p_tenant_id
                   FOR NO KEY UPDATE;
                v_found := FOUND;
            ELSE
                PERFORM 1
                   FROM public.tenants AS t
                  WHERE t.id = v_row.id
                    FOR SHARE;
            END IF;
        END LOOP;

        v_ok := v_found
            AND EXISTS (SELECT 1 FROM public.holdings AS h WHERE h.id = p_holding_id)
            AND NOT EXISTS (SELECT 1
                              FROM public.holding_members AS m
                             WHERE m.member_tenant_id = p_tenant_id
                               AND (m.left_at IS NULL OR m.left_at + interval '60 seconds' > clock_timestamp()))
            AND NOT EXISTS (SELECT 1
                              FROM public.holding_members AS m
                              JOIN public.tenants AS t ON t.id = m.member_tenant_id
                             WHERE m.holding_id = p_holding_id
                               AND (m.left_at IS NULL OR m.left_at + interval '60 seconds' > clock_timestamp())
                               AND (t.timezone <> v_zone OR t.api_code = v_code));

        IF v_ok THEN
            INSERT INTO public.holding_members (holding_id, member_tenant_id)
            VALUES (p_holding_id, p_tenant_id)
            RETURNING joined_at INTO v_at;

            INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail, at)
            VALUES (p_tenant_id, v_admin, 'tenant.holding_joined', p_holding_id::text,
                    jsonb_build_object('actor_kind', 'operator', 'holding_id', p_holding_id),
                    v_at);

            INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail, at)
            SELECT m.member_tenant_id, v_admin, 'tenant.holding_member_joined', p_holding_id::text,
                   jsonb_build_object('actor_kind', 'operator', 'holding_id', p_holding_id,
                                      'member_tenant_id', p_tenant_id),
                   v_at
              FROM public.holding_members AS m
             WHERE m.holding_id = p_holding_id
               AND m.left_at IS NULL
               AND m.member_tenant_id <> p_tenant_id;
        END IF;

        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, detail)
        VALUES ('holding_member_added', v_session, v_admin, p_tenant_id,
                jsonb_build_object('holding_id', p_holding_id));
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_add_holding_member: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_add_holding_member: change refused'
                USING ERRCODE = 'invalid_parameter_value';
    END;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_add_holding_member(text, uuid, uuid) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_add_holding_member(text, uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_add_holding_member(text, uuid, uuid) TO tappa_operator;

-- op_remove_holding_member: the tenant's LIVE membership of the holding ends at the instant of the
-- call (the wall clock; never a chosen past instant). Its one UPDATE carries `left_at IS NULL`, so
-- a membership that already ended keeps its left_at. K6, only when a membership ended, at that
-- instant: 'tenant.holding_left' in the leaving tenant's audit_log and
-- 'tenant.holding_member_left' in every remaining live member's. The operator's
-- 'holding_member_removed' row on every accepted call. The holding's advisory lock is the add's,
-- so "the remaining members" cannot race an add (ADR 0027 §1).
-- +goose StatementBegin
CREATE FUNCTION public.op_remove_holding_member(p_session text, p_holding_id uuid, p_tenant_id uuid)
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

    IF p_holding_id IS NULL OR p_tenant_id IS NULL THEN
        RAISE EXCEPTION 'op_remove_holding_member: change refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    PERFORM pg_catalog.pg_advisory_xact_lock(
        pg_catalog.hashtextextended('holding-membership:' || p_holding_id::text, 0));

    BEGIN
        UPDATE public.holding_members AS m
           SET left_at = clock_timestamp()
         WHERE m.holding_id = p_holding_id
           AND m.member_tenant_id = p_tenant_id
           AND m.left_at IS NULL
        RETURNING m.left_at INTO v_at;

        IF FOUND THEN
            INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail, at)
            VALUES (p_tenant_id, v_admin, 'tenant.holding_left', p_holding_id::text,
                    jsonb_build_object('actor_kind', 'operator', 'holding_id', p_holding_id),
                    v_at);

            INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail, at)
            SELECT m.member_tenant_id, v_admin, 'tenant.holding_member_left', p_holding_id::text,
                   jsonb_build_object('actor_kind', 'operator', 'holding_id', p_holding_id,
                                      'member_tenant_id', p_tenant_id),
                   v_at
              FROM public.holding_members AS m
             WHERE m.holding_id = p_holding_id
               AND m.left_at IS NULL
               AND m.member_tenant_id <> p_tenant_id;
        END IF;

        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id, detail)
        VALUES ('holding_member_removed', v_session, v_admin, p_tenant_id,
                jsonb_build_object('holding_id', p_holding_id));
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_remove_holding_member: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_remove_holding_member: change refused'
                USING ERRCODE = 'invalid_parameter_value';
    END;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_remove_holding_member(text, uuid, uuid) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_remove_holding_member(text, uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_remove_holding_member(text, uuid, uuid) TO tappa_operator;

-- ===========================================================================
-- 6. op_read_holdings -- the operator's read of the holdings and their memberships, two-phase
-- (ADR 0021 §2 v): op_begin_read('holdings', {page_number, page_size}) commits the 'read' row and
-- the ticket; this consumes it. One row per membership (live and ended -- the history the exit
-- window and a rejoin leave), and one row with NULL member columns for a holding without any;
-- newest holding first, live memberships before ended ones. Of each member tenant: its id, name,
-- timezone and API code -- what explains an add that did not happen (K-36, the code rule) --
-- and nothing else. LIMIT <= 200 (§2 iii).
-- ===========================================================================
-- +goose StatementBegin
CREATE FUNCTION public.op_read_holdings(p_session text, p_ticket text,
                                       p_page_number integer, p_page_size integer)
    RETURNS TABLE (holding_id uuid, holding_name text, holding_created_at timestamptz,
                   member_tenant_id uuid, member_name text, member_timezone text,
                   member_api_code text, joined_at timestamptz, left_at timestamptz)
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
               p_ticket || jsonb_build_object('page_number', p_page_number,
                                              'page_size', p_page_size)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'holdings'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_holdings: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    v_limit := least(p_page_size, 200);

    RETURN QUERY
        SELECT h.id,
               h.name,
               h.created_at,
               m.member_tenant_id,
               t.name,
               t.timezone,
               t.api_code,
               m.joined_at,
               m.left_at
          FROM public.holdings AS h
          LEFT JOIN public.holding_members AS m ON m.holding_id = h.id
          LEFT JOIN public.tenants AS t ON t.id = m.member_tenant_id
         ORDER BY h.created_at DESC, h.id, (m.left_at IS NULL) DESC, m.joined_at DESC, m.member_tenant_id
         LIMIT v_limit
        OFFSET (p_page_number - 1)::bigint * v_limit;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_holdings(text, text, integer, integer) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_holdings(text, text, integer, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_holdings(text, text, integer, integer) TO tappa_operator;

-- ===========================================================================
-- 7. op_enable_tenant_api, REPLACED (K-22 + ADR 0027's holding; the API-1 review's hand-over):
-- a member tenant cannot be given the API code a peer in its holding carries -- a peer live or
-- within its exit window -- because the spec's `company` field (KF, KM) is how a punch names its
-- employer and its site, and two members sharing one would attribute one tenant's events to the
-- other. op_add_holding_member refuses the other direction. NULL codes never collide (a member
-- without a code has no API). Everything else is 00039's: the session, the arguments (22023 before
-- anything is read), the row FOR NO KEY UPDATE, coalesce for the first opening instant, the K6 row
-- 'tenant.api_enabled' {actor_kind, api_code} at the one clock read, the operator's
-- 'tenant_api_enabled' row on every accepted call, the same void whatever was found. What is new
-- is ORDER: the holding's advisory lock (the one every membership write takes), then the rows in
-- canonical order (the tenant FOR NO KEY UPDATE, its peers FOR SHARE), then the decision -- the
-- header's SERIALISATION says why that order and why a holding that appears in between writes
-- nothing.
-- ===========================================================================
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_enable_tenant_api(p_session text, p_tenant_id uuid, p_api_code text)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session    uuid;
    v_admin      uuid;
    v_holding    uuid;
    v_row        record;
    v_found      boolean := false;
    v_ok         boolean;
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

    SELECT m.holding_id
      INTO v_holding
      FROM public.holding_members AS m
     WHERE m.member_tenant_id = p_tenant_id
       AND (m.left_at IS NULL OR m.left_at + interval '60 seconds' > clock_timestamp())
     ORDER BY m.joined_at DESC
     LIMIT 1;

    IF v_holding IS NOT NULL THEN
        PERFORM pg_catalog.pg_advisory_xact_lock(
            pg_catalog.hashtextextended('holding-membership:' || v_holding::text, 0));
    END IF;

    BEGIN
        FOR v_row IN
            SELECT x.id
              FROM (SELECT p_tenant_id AS id
                    UNION
                    SELECT m.member_tenant_id
                      FROM public.holding_members AS m
                     WHERE m.holding_id = v_holding
                       AND (m.left_at IS NULL OR m.left_at + interval '60 seconds' > clock_timestamp())) AS x
             ORDER BY x.id
        LOOP
            IF v_row.id = p_tenant_id THEN
                PERFORM t.id
                   FROM public.tenants AS t
                  WHERE t.id = p_tenant_id
                    FOR NO KEY UPDATE;
                v_found := FOUND;
            ELSE
                PERFORM 1
                   FROM public.tenants AS t
                  WHERE t.id = v_row.id
                    FOR SHARE;
            END IF;
        END LOOP;

        v_ok := v_found
            AND (SELECT m.holding_id
                   FROM public.holding_members AS m
                  WHERE m.member_tenant_id = p_tenant_id
                    AND (m.left_at IS NULL OR m.left_at + interval '60 seconds' > clock_timestamp())
                  ORDER BY m.joined_at DESC
                  LIMIT 1) IS NOT DISTINCT FROM v_holding
            AND NOT EXISTS (SELECT 1
                              FROM public.holding_members AS m
                              JOIN public.tenants AS t ON t.id = m.member_tenant_id
                             WHERE m.holding_id = v_holding
                               AND m.member_tenant_id <> p_tenant_id
                               AND (m.left_at IS NULL OR m.left_at + interval '60 seconds' > clock_timestamp())
                               AND t.api_code = p_api_code);

        IF v_ok THEN
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
-- CREATE OR REPLACE keeps the owner and the ACL; they are written again so this file, read alone,
-- shows the whole contract of the function it redefines (ADR 0021 §2 vi).
ALTER FUNCTION public.op_enable_tenant_api(text, uuid, text) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_enable_tenant_api(text, uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_enable_tenant_api(text, uuid, text) TO tappa_operator;

-- +goose Down

-- 🔴 WHAT A SUCCESSFUL Down DOES (00039's house style):
--   * the four op_* go; op_begin_read, op_read_audit and op_enable_tenant_api go back to 00039's
--     Up bodies VERBATIM -- replaced, not dropped, so their owners and ACLs stay;
--   * tappa_opdefiner loses EXACTLY what section 1 granted, by column -- not REVOKE ALL, which
--     would take 00029's, 00034's and 00039's tenants columns with it;
--   * the operator's kind CHECK and the ticket kind CHECK go back to 00039's sets -- NOT VALID
--     when a row of a kind outside them exists (this file's or a later migration's), VALIDATED
--     otherwise -- and the holding-row shape is dropped;
--   * the rows STAY: the operator's holding_* rows (operator_audit_log is append-only), the
--     tenants' rows (audit_log is append-only) and the memberships (00041's Down is what takes
--     the tables away).
-- No statement here locks tenants beyond what a REVOKE of a column grant does (nothing --
-- measured): opAtVersion walks this Down inside other packages' test runs.

DROP FUNCTION IF EXISTS public.op_read_holdings(text, text, integer, integer);
DROP FUNCTION IF EXISTS public.op_remove_holding_member(text, uuid, uuid);
DROP FUNCTION IF EXISTS public.op_add_holding_member(text, uuid, uuid);
DROP FUNCTION IF EXISTS public.op_create_holding(text, text);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_enable_tenant_api(p_session text, p_tenant_id uuid, p_api_code text)
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

REVOKE SELECT (api_code) ON tenants FROM tappa_opdefiner;
REVOKE UPDATE (left_at) ON holding_members FROM tappa_opdefiner;
REVOKE INSERT (holding_id, member_tenant_id) ON holding_members FROM tappa_opdefiner;
REVOKE SELECT (holding_id, member_tenant_id, joined_at, left_at) ON holding_members FROM tappa_opdefiner;
REVOKE INSERT (name) ON holdings FROM tappa_opdefiner;
REVOKE SELECT (id, name, created_at) ON holdings FROM tappa_opdefiner;

ALTER TABLE operator_audit_log DROP CONSTRAINT IF EXISTS operator_audit_log_holding_write_shape;

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

ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_read_tickets
                WHERE kind <> ALL (ARRAY['legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                                         'operator_audit', 'tenant_billing', 'tenant_vat'])) THEN
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit', 'tenant_billing', 'tenant_vat'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit', 'tenant_billing', 'tenant_vat'));
    END IF;
END
$$;
-- +goose StatementEnd
