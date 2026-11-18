-- 00107 -- issue #37 phase 1: the app registry's OPERATOR half -- tappa_opdefiner's reach on
-- oauth_clients, the registry's key to platform_admins, four audit kinds and their row shape, the
-- read kind 'oauth_clients', op_begin_read and op_read_audit replaced, and the five op_*:
-- op_register_oauth_client, op_rotate_oauth_client_secret, op_set_oauth_client_redirects,
-- op_disable_oauth_client (the four writes) and op_read_oauth_clients (the read).
--
-- NORMATIVE SOURCE: docs/adr/0047-oauth-bagli-uygulamalar.md §10.1-§10.2 (the registry, written by
-- four op_* and read by one; the redirect URI rule in SQL, 22023; the closed scope set without
-- webhooks:manage; the name rule; a disabled app is never re-opened; the secret's rotation kills
-- the old one at once), §14.1 (the grants), §14.4 (the kind CHECK widened by four, 00039's
-- whole-text precondition; the read uses 'read'; five functions on the owner pin); ADR 0021 §1-§3
-- (the op_* contract: tappa_opdefiner, op_touch_session first, the derived actor, one-phase writes,
-- the two-phase read, void/uuid answers with no state oracle, one wall-clock read per call, constant
-- messages, the integrity catch); 00042 (op_create_holding and op_read_holdings: this file's write
-- and read shapes) and 00063 (the bodies this file replaces).
--
-- WHY A FILE OF ITS OWN, LAST (00104's header): this is the one OAuth file that names operator
-- objects, so internal/db's opAtVersion walks its Down -- and only its -- when an operator test
-- reaches an older version. Its Down drops no table: the registry, its rows and the tenant tables
-- stay, and the operator surface goes back to 00063's exactly.
--
-- WHAT IT DOES, IN ORDER:
--   0. lock_timeout 1 s, the FIRST statement (00054's rule): the ALTERs take ACCESS EXCLUSIVE on
--      operator_audit_log, operator_read_tickets, oauth_clients and platform_admins (the new key);
--   1. the precondition: 00026's role checks (as 00027-00063 repeat them), 00063's operator surface
--      (the kind and ticket CHECKs by their whole text, op_begin_read and op_read_audit by
--      md5(prosrc)), nothing this file creates yet, and tappa_opdefiner reaching nothing of
--      oauth_clients (a wider grant another hand gave cannot be narrowed by a column grant);
--   2. tappa_opdefiner on oauth_clients -- by column, nothing else (no new EXECUTE: the redirect
--      rule is a CHECK, not a function); the registry's created_by gains its key to platform_admins;
--   3. the four audit kinds and operator_audit_log_oauth_client_shape; the read kind 'oauth_clients';
--   4. op_begin_read REPLACED (00063's body, the new read kind and the four kinds in its lists);
--   5. op_read_audit REPLACED (00063's body, the four kinds, the new scope, the row shape's arm);
--   6. the four writes; 7. the read.
--
-- 🔴 THE WRITES ANSWER THE SAME WHATEVER THEY FIND (ADR 0021 §2 v 7, B14). op_register_oauth_client
-- returns the id it created (the one answer §2 v 7 allows, op_create_holding's). The other three
-- return void for an app that exists, one that is disabled and an id that names nothing alike; each
-- writes the operator's row on every accepted call, naming the app by its id ({"client_id": ...}) and
-- nothing else -- no tenant, no scope, no page (operator_audit_log_oauth_client_shape). What
-- changed is read back through op_read_oauth_clients. A disabled app is not changed by any of them:
-- disabling is one way (00104's trigger) and the two edits skip it -- a closed app is re-registered,
-- never re-opened (§10.2).
-- 🔴 NO TENANT IS TOUCHED. The registry carries no tenant data (ADR 0047 §9): disabling an app
-- writes oauth_clients alone; its grants stop resolving through resolve_oauth_token_by_hash's
-- client_disabled_at (00105), not through a write on a tenant row. CLAUDE.md §4.5 gains no exception.
-- 🔴 THE SECRET IS A HASH HERE. Go mints the secret, HMACs it under TAPPA_OAUTH_HMAC_KEY and shows it
-- once (ADR 0047 §10.2); these functions take the hex hash and the hint, never the value. No
-- argument reaches a message: every RAISE EXCEPTION is constant, the LOG lines carry a constraint
-- name and a SQLSTATE (ADR 0021 §3.5).
--
-- CLOCKS (ADR 0021 §2 vii): every instant a write stores -- secret_rotated_at, disabled_at -- is ONE
-- clock_timestamp() read per call; a registration's created_at is the column's DEFAULT
-- clock_timestamp(). The operator row's `at` is 00033's trigger's.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 1. PRECONDITION: the roles, 00063's operator surface, the registry and the definer's reach.
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
        RAISE EXCEPTION 'migration 00107 (issue #37) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00107 (issue #37): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00107 (issue #37): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00107 (issue #37): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00107 (issue #37): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00107 (issue #37): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00107 (issue #37): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF pg_catalog.to_regclass('public.oauth_clients') IS NULL
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_begin_read(text, text, jsonb)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_audit(text, text, text, integer, integer)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR pg_catalog.to_regprocedure('public.op_register_oauth_client(text, text, text[], text[], text, text)') IS NOT NULL
       OR pg_catalog.to_regprocedure('public.op_rotate_oauth_client_secret(text, uuid, text, text)') IS NOT NULL
       OR pg_catalog.to_regprocedure('public.op_set_oauth_client_redirects(text, uuid, text[])') IS NOT NULL
       OR pg_catalog.to_regprocedure('public.op_disable_oauth_client(text, uuid)') IS NOT NULL
       OR pg_catalog.to_regprocedure('public.op_read_oauth_clients(text, text, integer, integer)') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_constraint c
                   WHERE (c.conrelid = 'public.operator_audit_log'::pg_catalog.regclass
                          AND c.conname = 'operator_audit_log_oauth_client_shape')
                      OR (c.conrelid = 'public.oauth_clients'::pg_catalog.regclass
                          AND c.conname = 'oauth_clients_creator_fk')) THEN
        RAISE EXCEPTION 'migration 00107 (issue #37) replaces op_begin_read and op_read_audit as migration 00063 left them and adds the registry''s operator surface over migration 00104''s table, and that state is not there (the registry, the two functions with their owner, none of this file''s functions or constraints yet). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF (SELECT pg_catalog.replace(pg_catalog.pg_get_constraintdef(c.oid), ' NOT VALID', '')
          FROM pg_catalog.pg_constraint c
         WHERE c.conrelid = 'public.operator_audit_log'::regclass
           AND c.conname = 'operator_audit_log_kind_check')
         IS DISTINCT FROM 'CHECK ((kind = ANY (ARRAY[''login_failed''::text, ''unknown_email''::text, ''totp_failed''::text, ''locked''::text, ''enrollment_failed''::text, ''password_ok''::text, ''login''::text, ''enrollment''::text, ''logout''::text, ''read''::text, ''legal_publish''::text, ''operator_created''::text, ''operator_mfa_reset''::text, ''operator_disabled''::text, ''tenant_vat_checked''::text, ''tenant_api_enabled''::text, ''tenant_api_disabled''::text, ''holding_created''::text, ''holding_member_added''::text, ''holding_member_removed''::text, ''tenant_suspended''::text, ''tenant_reinstated''::text])))'
       OR (SELECT pg_catalog.replace(pg_catalog.pg_get_constraintdef(c.oid), ' NOT VALID', '')
             FROM pg_catalog.pg_constraint c
            WHERE c.conrelid = 'public.operator_read_tickets'::regclass
              AND c.conname = 'operator_read_tickets_kind_check')
         IS DISTINCT FROM 'CHECK ((kind = ANY (ARRAY[''legal_versions''::text, ''tenants''::text, ''tenant_detail''::text, ''tenant_plaques''::text, ''operator_audit''::text, ''tenant_billing''::text, ''tenant_vat''::text, ''holdings''::text])))' THEN
        RAISE EXCEPTION 'migration 00107 (issue #37) widens operator_audit_log_kind_check and operator_read_tickets_kind_check as migrations 00063 and 00042 left them, and the constraints are not in that state -- a migration between 00063 and this one changed them; rebase this file onto it. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- THE TWO BODIES, BY DIGEST: md5(prosrc) of 00063's op_begin_read and op_read_audit -- the
    -- bodies sections 4 and 5 replace whole. This file's Down gives them back verbatim.
    IF (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = pg_catalog.to_regprocedure('public.op_begin_read(text, text, jsonb)'))
         IS DISTINCT FROM '0f57e3a5444bdefad336f2fdb877d984'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_audit(text, text, text, integer, integer)'))
         IS DISTINCT FROM '75df477d8d45c986b7e4943e7051ec18' THEN
        RAISE EXCEPTION 'migration 00107 (issue #37) replaces op_begin_read and op_read_audit whole with the bodies migration 00063 left plus its edits, and a live body is not that -- a migration between 00063 and this one changed it, and this file would silently undo that; rebase this file onto it. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.oauth_clients', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.oauth_clients', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.oauth_clients', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.oauth_clients', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.oauth_clients', 'TRUNCATE')
       OR EXISTS (SELECT 1
                    FROM pg_catalog.pg_roles who
                    JOIN pg_catalog.pg_roles r ON pg_catalog.pg_has_role(who.oid, r.oid, 'MEMBER')
                   WHERE who.rolname IN ('tappa_app', 'tappa_operator')
                     AND (pg_catalog.has_any_column_privilege(r.oid, 'public.oauth_clients', 'SELECT')
                          OR pg_catalog.has_any_column_privilege(r.oid, 'public.oauth_clients', 'INSERT')
                          OR pg_catalog.has_any_column_privilege(r.oid, 'public.oauth_clients', 'UPDATE')
                          OR pg_catalog.has_table_privilege(r.oid, 'public.oauth_clients', 'DELETE'))) THEN
        RAISE EXCEPTION 'migration 00107 (issue #37) grants tappa_opdefiner named columns of oauth_clients, and that role, tappa_app or tappa_operator already reaches the table through a grant no migration gave it. A column grant cannot narrow it. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 2. tappa_opdefiner on the registry, by column; the registry's key to platform_admins.
-- ---------------------------------------------------------------------------
--   SELECT  everything but secret_hash -- the read's columns, and what the writes' WHEREs and
--           RETURNING name (id, disabled_at); the hash is the resolver's alone (00105);
--   INSERT  the registration's columns; id, created_at, secret_rotated_at and disabled_at are not
--           in it (the DEFAULTs, and NULL at birth);
--   UPDATE  the three edits: the secret (hash, hint, rotated_at), the redirect list, disabled_at.
--           Never name or allowed_scopes (no op_* edits them in v1, §10.4), never created_*.
-- tappa_operator gets nothing on the table; tappa_app keeps nothing (00104).
GRANT SELECT (id, name, redirect_uris, allowed_scopes, secret_hint, secret_rotated_at, created_at, created_by, disabled_at)
    ON oauth_clients TO tappa_opdefiner;
GRANT INSERT (name, redirect_uris, allowed_scopes, secret_hash, secret_hint, created_by) ON oauth_clients TO tappa_opdefiner;
GRANT UPDATE (redirect_uris, secret_hash, secret_hint, secret_rotated_at, disabled_at) ON oauth_clients TO tappa_opdefiner;

ALTER TABLE oauth_clients
    ADD CONSTRAINT oauth_clients_creator_fk FOREIGN KEY (created_by) REFERENCES platform_admins (id) ON DELETE RESTRICT;

-- ---------------------------------------------------------------------------
-- 3. The four audit kinds, their row shape, and the read kind.
-- ---------------------------------------------------------------------------
--   'oauth_client_registered'        op_register_oauth_client: the new app's id;
--   'oauth_client_secret_rotated'    op_rotate_oauth_client_secret: every accepted call;
--   'oauth_client_redirects_changed' op_set_oauth_client_redirects: every accepted call;
--   'oauth_client_disabled'          op_disable_oauth_client: every accepted call.
-- SESSION kinds (actor_shape's third arm, unchanged) and APP kinds: operator_audit_log_oauth_client_shape
-- makes the row name the app and nothing else -- no tenant, no admin, no scope, no page, detail
-- exactly {"client_id": "<uuid>"} (00042's holding shape, without the tenant arm). The kind CHECK is
-- added NOT VALID only when a row of a kind outside the new set exists (00031's rule, as 00034-00063
-- applied it); both keep their names.
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
                                         'holding_member_removed', 'tenant_suspended', 'tenant_reinstated',
                                         'oauth_client_registered', 'oauth_client_secret_rotated',
                                         'oauth_client_redirects_changed', 'oauth_client_disabled'])) THEN
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                            'tenant_api_disabled', 'holding_created', 'holding_member_added',
                            'holding_member_removed', 'tenant_suspended', 'tenant_reinstated',
                            'oauth_client_registered', 'oauth_client_secret_rotated',
                            'oauth_client_redirects_changed', 'oauth_client_disabled'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                            'tenant_api_disabled', 'holding_created', 'holding_member_added',
                            'holding_member_removed', 'tenant_suspended', 'tenant_reinstated',
                            'oauth_client_registered', 'oauth_client_secret_rotated',
                            'oauth_client_redirects_changed', 'oauth_client_disabled'));
    END IF;
END
$$;
-- +goose StatementEnd
ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_oauth_client_shape CHECK (
    kind <> ALL (ARRAY['oauth_client_registered', 'oauth_client_secret_rotated', 'oauth_client_redirects_changed',
                       'oauth_client_disabled'])
    OR (target_admin_id IS NULL AND target_tenant_id IS NULL AND target_scope IS NULL
        AND page_number IS NULL AND page_size IS NULL
        AND detail ? 'client_id' AND (detail - 'client_id') = '{}'::jsonb
        AND jsonb_typeof(detail -> 'client_id') = 'string'
        AND (detail ->> 'client_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')
);

ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_read_tickets
                WHERE kind <> ALL (ARRAY['legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                                         'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings',
                                         'oauth_clients'])) THEN
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings', 'oauth_clients'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings', 'oauth_clients'));
    END IF;
END
$$;
-- +goose StatementEnd

-- ===========================================================================
-- 4. op_begin_read, REPLACED: the read kind 'oauth_clients' and the four kinds in the log filter
-- ===========================================================================
-- 00063's body but for TWO lists: the closed set of read kinds gains 'oauth_clients' (its
-- parameters are the paged lists' {page_number, page_size} -- the ELSE branch, op_read_holdings'
-- shape), and the 'operator_audit' read's filter names the four kinds. Everything 00027-00063 say
-- of this function still holds; the copies of the audit kind set (this list, the CHECK,
-- op_read_audit's filter shape, internal/db's OperatorAuditKinds, the viewer's words) are held equal
-- by TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree and
-- TestAuditWords_NameEveryKindAndNothingElse.
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
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings', 'oauth_clients') THEN
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
                                                   'holding_member_removed', 'tenant_suspended',
                                                   'tenant_reinstated', 'oauth_client_registered',
                                                   'oauth_client_secret_rotated',
                                                   'oauth_client_redirects_changed',
                                                   'oauth_client_disabled') THEN
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
-- 5. op_read_audit, REPLACED: the four kinds, the read scope, and the app rows' shape
-- ===========================================================================
-- 00063's body but for four edits: the values an 'operator_audit' read's {"filter": ...} may hold
-- gain the four kinds; the scopes a 'read' row may carry with detail {} gain 'oauth_clients' (and
-- so does the scope column's closed list); and the four app rows are recognised by their shape --
-- target_scope NULL, detail exactly {"client_id": "<uuid>"} (section 3's CHECK makes it certain),
-- the holding arm's form (00042). Without the arm they would come back "detail not shown".
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
                                                            'holding_member_removed', 'tenant_suspended',
                                                            'tenant_reinstated', 'oauth_client_registered',
                                                            'oauth_client_secret_rotated',
                                                            'oauth_client_redirects_changed',
                                                            'oauth_client_disabled')
                       WHEN p.kind = 'read' THEN
                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing',
                                                'tenant_vat', 'holdings', 'oauth_clients')
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
                       WHEN p.kind IN ('oauth_client_registered', 'oauth_client_secret_rotated',
                                       'oauth_client_redirects_changed', 'oauth_client_disabled') THEN
                            p.target_scope IS NULL
                            AND p.detail ? 'client_id' AND (p.detail - 'client_id') = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'client_id') = 'string'
                            AND (p.detail ->> 'client_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
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
                                            'tenant_vat', 'holdings', 'oauth_clients')
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
-- 6. The four writers (ADR 0047 §10.2; ADR 0021's class). ONE PHASE: the change and the operator's
-- row are one call. Arguments are refused with one constant 22023 BEFORE anything is read or written;
-- a constraint that refuses the write (a secret hash colliding with another app's, say) is caught --
-- one 22023 and a LOG line with the constraint's name and the SQLSTATE (00027's pattern).
-- The redirect list is tested against 00104's rule -- the SAME fixed regular expression, the same
-- 512-byte and 1-to-5 bounds oauth_clients_redirect_uris_shape carries (the three copies are held equal
-- by internal/db's TestOAuth_TheRedirectRuleIsOneExpression) -- and a list naming one address twice is
-- refused.
-- The scopes must be a non-empty subset of the closed set (webhooks:manage is not in it, §13); they
-- are stored sorted and without repeats.
-- ===========================================================================

-- op_register_oauth_client: a new app. The answer is the id it created (ADR 0021 §2 v 7 allows
-- exactly that, op_create_holding's precedent); the operator's row names it.
-- +goose StatementBegin
CREATE FUNCTION public.op_register_oauth_client(p_session text, p_name text, p_redirect_uris text[],
                                                p_scopes text[], p_secret_hash text, p_secret_hint text)
    RETURNS uuid
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session    uuid;
    v_admin      uuid;
    v_client     uuid;
    v_scopes     text[];
    v_constraint text;
    v_state      text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    IF coalesce(p_name IS NULL OR p_name <> btrim(p_name) OR char_length(p_name) NOT BETWEEN 1 AND 64
                OR p_name ~ '[\u0001-\u001F\u007F-\u009F\u2028\u2029\u202A-\u202E\u2066-\u2069]'
                OR (p_redirect_uris IS NULL OR array_ndims(p_redirect_uris) IS DISTINCT FROM 1
                 OR array_lower(p_redirect_uris, 1) IS DISTINCT FROM 1
                 OR cardinality(p_redirect_uris) NOT BETWEEN 1 AND 5
                 OR EXISTS (SELECT 1 FROM unnest(p_redirect_uris) AS u(v)
                             WHERE u.v IS NULL OR octet_length(u.v) > 512
                                OR u.v !~ '^(https://([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?(:443)?|http://(localhost|127\.0\.0\.1):[1-9][0-9]{0,4})/[A-Za-z0-9._~!$&()*+,;=:%/-]*$')
                 OR (SELECT count(DISTINCT u.v) FROM unnest(p_redirect_uris) AS u(v)) <> cardinality(p_redirect_uris))
                OR p_scopes IS NULL OR array_ndims(p_scopes) IS DISTINCT FROM 1 OR cardinality(p_scopes) = 0
                OR array_position(p_scopes, NULL) IS NOT NULL
                OR NOT (p_scopes <@ ARRAY['timesheets:read', 'employees:read', 'employees:write', 'locations:write',
                                          'punches:read'])
                OR p_secret_hash IS NULL OR p_secret_hash !~ '^[0-9a-f]{64}$'
                OR p_secret_hint IS NULL OR p_secret_hint !~ '^tt_cs_(live|test)_[A-Za-z0-9_-]{4}$', true) THEN
        RAISE EXCEPTION 'op_register_oauth_client: change refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_scopes := (SELECT array_agg(DISTINCT s.v ORDER BY s.v) FROM unnest(p_scopes) AS s(v));

    BEGIN
        INSERT INTO public.oauth_clients (name, redirect_uris, allowed_scopes, secret_hash, secret_hint, created_by)
        VALUES (p_name, p_redirect_uris, v_scopes, p_secret_hash, p_secret_hint, v_admin)
        RETURNING id INTO v_client;

        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, detail)
        VALUES ('oauth_client_registered', v_session, v_admin, jsonb_build_object('client_id', v_client));
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_register_oauth_client: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_register_oauth_client: change refused'
                USING ERRCODE = 'invalid_parameter_value';
    END;

    RETURN v_client;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_register_oauth_client(text, text, text[], text[], text, text) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_register_oauth_client(text, text, text[], text[], text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_register_oauth_client(text, text, text[], text[], text, text) TO tappa_operator;

-- op_rotate_oauth_client_secret: the app's secret is replaced and the old one dies on the spot (the
-- resolver compares against the stored hash, there is no second slot -- §10.2: overlapping rotation
-- is a later job). The grants and their tokens are NOT touched: a rotation stops the token endpoint
-- for whoever holds the old secret, not the /v1 access already issued (ADR 0047 §17).
-- +goose StatementBegin
CREATE FUNCTION public.op_rotate_oauth_client_secret(p_session text, p_client_id uuid, p_secret_hash text,
                                                     p_secret_hint text)
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

    IF coalesce(p_client_id IS NULL
                OR p_secret_hash IS NULL OR p_secret_hash !~ '^[0-9a-f]{64}$'
                OR p_secret_hint IS NULL OR p_secret_hint !~ '^tt_cs_(live|test)_[A-Za-z0-9_-]{4}$', true) THEN
        RAISE EXCEPTION 'op_rotate_oauth_client_secret: change refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    BEGIN
        v_at := clock_timestamp();
        UPDATE public.oauth_clients AS c
           SET secret_hash = p_secret_hash, secret_hint = p_secret_hint, secret_rotated_at = v_at
         WHERE c.id = p_client_id AND c.disabled_at IS NULL;

        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, detail)
        VALUES ('oauth_client_secret_rotated', v_session, v_admin, jsonb_build_object('client_id', p_client_id));
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_rotate_oauth_client_secret: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_rotate_oauth_client_secret: change refused'
                USING ERRCODE = 'invalid_parameter_value';
    END;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_rotate_oauth_client_secret(text, uuid, text, text) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_rotate_oauth_client_secret(text, uuid, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_rotate_oauth_client_secret(text, uuid, text, text) TO tappa_operator;

-- op_set_oauth_client_redirects: the app's whole redirect list is replaced. A consent page drawn
-- before the change is re-checked against the new list at the POST (ADR 0047 §4.2, item 3).
-- +goose StatementBegin
CREATE FUNCTION public.op_set_oauth_client_redirects(p_session text, p_client_id uuid, p_redirect_uris text[])
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session    uuid;
    v_admin      uuid;
    v_constraint text;
    v_state      text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    IF coalesce(p_client_id IS NULL
                OR (p_redirect_uris IS NULL OR array_ndims(p_redirect_uris) IS DISTINCT FROM 1
                 OR array_lower(p_redirect_uris, 1) IS DISTINCT FROM 1
                 OR cardinality(p_redirect_uris) NOT BETWEEN 1 AND 5
                 OR EXISTS (SELECT 1 FROM unnest(p_redirect_uris) AS u(v)
                             WHERE u.v IS NULL OR octet_length(u.v) > 512
                                OR u.v !~ '^(https://([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]([a-z0-9-]{0,61}[a-z0-9])?(:443)?|http://(localhost|127\.0\.0\.1):[1-9][0-9]{0,4})/[A-Za-z0-9._~!$&()*+,;=:%/-]*$')
                 OR (SELECT count(DISTINCT u.v) FROM unnest(p_redirect_uris) AS u(v)) <> cardinality(p_redirect_uris)),
                true) THEN
        RAISE EXCEPTION 'op_set_oauth_client_redirects: change refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    BEGIN
        UPDATE public.oauth_clients AS c
           SET redirect_uris = p_redirect_uris
         WHERE c.id = p_client_id AND c.disabled_at IS NULL;

        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, detail)
        VALUES ('oauth_client_redirects_changed', v_session, v_admin, jsonb_build_object('client_id', p_client_id));
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_set_oauth_client_redirects: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_set_oauth_client_redirects: change refused'
                USING ERRCODE = 'invalid_parameter_value';
    END;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_set_oauth_client_redirects(text, uuid, text[]) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_set_oauth_client_redirects(text, uuid, text[]) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_set_oauth_client_redirects(text, uuid, text[]) TO tappa_operator;

-- op_disable_oauth_client: the app is closed for every business, ONE WAY (00104's trigger; a closed
-- app is re-registered, never re-opened). No grant and no token row is written (§9, §11.4): the
-- token resolver carries client_disabled_at, so every access token of the app is 401 on its next
-- request, the token endpoint refuses the app's credentials, and the consent page refuses its id.
-- +goose StatementBegin
CREATE FUNCTION public.op_disable_oauth_client(p_session text, p_client_id uuid)
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

    IF p_client_id IS NULL THEN
        RAISE EXCEPTION 'op_disable_oauth_client: change refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    BEGIN
        v_at := clock_timestamp();
        UPDATE public.oauth_clients AS c
           SET disabled_at = v_at
         WHERE c.id = p_client_id AND c.disabled_at IS NULL;

        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, detail)
        VALUES ('oauth_client_disabled', v_session, v_admin, jsonb_build_object('client_id', p_client_id));
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_disable_oauth_client: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_disable_oauth_client: change refused'
                USING ERRCODE = 'invalid_parameter_value';
    END;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_disable_oauth_client(text, uuid) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_disable_oauth_client(text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_disable_oauth_client(text, uuid) TO tappa_operator;

-- ===========================================================================
-- 7. op_read_oauth_clients -- the registry, the two-phase read (ADR 0021 §2 v; op_read_holdings'
-- shape). op_begin_read wrote the 'read' row (target_scope 'oauth_clients', the page, no tenant,
-- detail {}) and a ticket bound to the session and the page; this consumes it and returns the page,
-- newest app first. A fixed column list (ADR 0021 §2 ii): never secret_hash -- the hint is what the
-- screen shows. created_by_name is the registering operator's display name.
-- ===========================================================================
-- +goose StatementBegin
CREATE FUNCTION public.op_read_oauth_clients(p_session text, p_ticket text,
                                             p_page_number integer, p_page_size integer)
    RETURNS TABLE (client_id uuid, name text, redirect_uris text[], allowed_scopes text[],
                   secret_hint text, secret_rotated_at timestamptz, created_at timestamptz,
                   created_by uuid, created_by_name text, disabled_at timestamptz)
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
       AND k.kind = 'oauth_clients'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_oauth_clients: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    v_limit := least(p_page_size, 200);

    RETURN QUERY
        SELECT c.id, c.name, c.redirect_uris, c.allowed_scopes, c.secret_hint, c.secret_rotated_at,
               c.created_at, c.created_by, a.display_name, c.disabled_at
          FROM public.oauth_clients AS c
          LEFT JOIN public.platform_admins AS a ON a.id = c.created_by
         ORDER BY c.created_at DESC, c.id
         LIMIT v_limit
        OFFSET (p_page_number - 1)::bigint * v_limit;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_oauth_clients(text, text, integer, integer) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_oauth_clients(text, text, integer, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_oauth_clients(text, text, integer, integer) TO tappa_operator;

-- +goose Down
SET LOCAL lock_timeout = '1s';

-- 🔴 WHAT THE Down DOES, named (00030-00063's house style):
--   * the five functions go; op_read_audit and op_begin_read go back to 00063's Up bodies VERBATIM
--     (replaced, so their owners and ACLs stay);
--   * the read kind CHECK goes back to 00042's eight, the audit kind CHECK to 00063's twenty-two --
--     each NOT VALID when a row of a kind outside its set exists (this file's, or a later one's),
--     VALIDATED when none does; operator_audit_log_oauth_client_shape goes;
--   * the registry loses its key to platform_admins, and tappa_opdefiner EXACTLY what section 2
--     granted, by column (no EXECUTE was granted: the URI rule is a CHECK, not a function);
--   * the rows STAY: the operator's oauth_client_* rows (operator_audit_log is append-only) and the
--     registry itself (00104's, with every grant that names it).
-- The tables are locked FIRST, in one statement: the ALTERs below take ACCESS EXCLUSIVE on
-- operator_audit_log, operator_read_tickets, oauth_clients and platform_admins (dropping the key),
-- and a Down walked inside a test transaction (internal/db's opAtVersion) must never wait for one of
-- them while holding another -- downTables reads this list.
LOCK TABLE public.operator_audit_log, public.operator_read_tickets, public.oauth_clients, public.platform_admins
    IN ACCESS EXCLUSIVE MODE;

DROP FUNCTION IF EXISTS public.op_read_oauth_clients(text, text, integer, integer);
DROP FUNCTION IF EXISTS public.op_disable_oauth_client(text, uuid);
DROP FUNCTION IF EXISTS public.op_set_oauth_client_redirects(text, uuid, text[]);
DROP FUNCTION IF EXISTS public.op_rotate_oauth_client_secret(text, uuid, text, text);
DROP FUNCTION IF EXISTS public.op_register_oauth_client(text, text, text[], text[], text, text);

-- op_read_audit and op_begin_read: 00063's Up bodies, VERBATIM.
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
                                                            'holding_member_removed', 'tenant_suspended',
                                                            'tenant_reinstated')
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
                                                   'holding_member_removed', 'tenant_suspended',
                                                   'tenant_reinstated') THEN
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

ALTER TABLE operator_audit_log DROP CONSTRAINT IF EXISTS operator_audit_log_oauth_client_shape;
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
                                         'holding_member_removed', 'tenant_suspended', 'tenant_reinstated'])) THEN
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                            'tenant_api_disabled', 'holding_created', 'holding_member_added',
                            'holding_member_removed', 'tenant_suspended', 'tenant_reinstated'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                            'tenant_api_disabled', 'holding_created', 'holding_member_added',
                            'holding_member_removed', 'tenant_suspended', 'tenant_reinstated'));
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE oauth_clients DROP CONSTRAINT IF EXISTS oauth_clients_creator_fk;
REVOKE UPDATE (redirect_uris, secret_hash, secret_hint, secret_rotated_at, disabled_at) ON oauth_clients FROM tappa_opdefiner;
REVOKE INSERT (name, redirect_uris, allowed_scopes, secret_hash, secret_hint, created_by) ON oauth_clients FROM tappa_opdefiner;
REVOKE SELECT (id, name, redirect_uris, allowed_scopes, secret_hint, secret_rotated_at, created_at, created_by, disabled_at)
    ON oauth_clients FROM tappa_opdefiner;
