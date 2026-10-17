-- 00057 -- M11 API-13a (K-22's operator screen): op_read_tenant_detail also returns the tenant's
-- integration API state -- api_enabled_at and api_code, the two columns 00038 added and 00039's
-- op_enable_tenant_api / op_disable_tenant_api write -- so the operator's tenant overview can say
-- whether the API is open, since when and under which code, and the screen's writes (which answer
-- the same whatever they found: ADR 0021 §2 v 7) can be read back.
--
-- NORMATIVE SOURCES: docs/adr/0028-dis-api-anahtar-akis-webhook.md §2 (K-22) and its "API-13a
-- uygulama notu"; docs/adr/0021-op-fonksiyonlari-tenant-otesi-erisim.md §2 (ii: a read's fixed
-- column list is the whole of what it can return; vi: the owner, REVOKE and GRANT written again),
-- §3.2 (the explicit tenant filter is the only barrier); OP-15's held branch made the same move for
-- the suspension (its section 6) and is the emsal of the shape.
--
-- WHAT IT DOES, IN ORDER:
--   0. re-checks the two cluster roles' shape (00026's precondition, as 00027-00042 repeated it),
--      that op_read_tenant_detail is 00029's -- its result type by text and its body by md5(prosrc),
--      so a migration that changed it in between stops this file loudly instead of being silently
--      undone by it -- and that tappa_opdefiner
--      already holds SELECT on the two columns (00039 api_enabled_at, 00042 api_code): this file
--      grants NOTHING;
--   1. op_read_tenant_detail is DROPPED and CREATED again (CREATE OR REPLACE cannot change a result
--      type) with 00029's body and two more columns at the END of its row (Go scans by position):
--      api_enabled_at (NULL: the API is closed) and api_code (kept by the disable -- punches still
--      carry it -- so a closed tenant can show a code). Both come from the same `t` row the read
--      already names by `t.id = p_tenant_id`; the counts' four filters are 00029's verbatim.
--
-- THE READ IS STILL ONE TICKET KIND ('tenant_detail'), one 'read' row per view in
-- operator_audit_log (00029's phase one): the API state is part of the overview the operator
-- already reads, not a new door. No table, no grant, no kind.
--
-- ORDER AGAINST OP-15 (the user's decision of 2026-10-17 -- M11 ships now; deploy/README.md, M11
-- deploy day (a)): M11 ships BEFORE OP-15. OP-15's suspension migration -- which rewrites this same
-- function with the two suspension columns, on a held branch -- moves to 00058 and is written ONTO
-- this file: its body is this one plus the two suspension columns at the end (13 columns),
-- its precondition names this file's result type and md5(prosrc), its Down comes back here. This file
-- is not rewritten for it.
--
-- CLOCKS: the function reads clock_timestamp() for the ticket as 00029's does; it writes no instant.
-- SECRETS: none -- an API code is a public label (00039's header).

-- +goose Up

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
        RAISE EXCEPTION 'migration 00057 (M11 API-13a) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00057 (M11 API-13a): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00057 (M11 API-13a): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00057 (M11 API-13a): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00057 (M11 API-13a): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00057 (M11 API-13a): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00057 (M11 API-13a): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1: tappa_app gets no new privilege). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
         WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_tenant_detail(text, text, uuid)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR pg_catalog.pg_get_function_result(pg_catalog.to_regprocedure('public.op_read_tenant_detail(text, text, uuid)'))
           IS DISTINCT FROM 'TABLE(tenant_id uuid, tenant_name text, created_at timestamp with time zone, plan text, business_type text, location_count bigint, active_employee_count bigint, active_plaque_count bigint, active_admin_count bigint)'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_tenant_detail(text, text, uuid)'))
           IS DISTINCT FROM '86d4081dbdae53b128a701784b8f3deb' THEN
        RAISE EXCEPTION 'migration 00057 (M11 API-13a) replaces op_read_tenant_detail as migration 00029 left it, and the live function is not that (its owner, its result columns or its body) -- a migration between 00029 and this one changed it, and this file would silently undo that. A migration that changes op_read_tenant_detail (OP-15) comes AFTER this file, written onto it (deploy/README.md, M11 deploy day (a)). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF NOT pg_catalog.has_column_privilege('tappa_opdefiner', 'public.tenants', 'api_enabled_at', 'SELECT')
       OR NOT pg_catalog.has_column_privilege('tappa_opdefiner', 'public.tenants', 'api_code', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00057 (M11 API-13a) reads tenants.api_enabled_at and tenants.api_code as tappa_opdefiner, which 00039 and 00042 granted, and a grant is missing. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. op_read_tenant_detail: 00029's overview and the API state, at the END of the row.
-- ---------------------------------------------------------------------------
DROP FUNCTION public.op_read_tenant_detail(text, text, uuid);
-- +goose StatementBegin
CREATE FUNCTION public.op_read_tenant_detail(p_session text, p_ticket text, p_tenant_id uuid)
    RETURNS TABLE (tenant_id uuid, tenant_name text, created_at timestamptz, plan text,
                   business_type text, location_count bigint,
                   active_employee_count bigint, active_plaque_count bigint,
                   active_admin_count bigint, api_enabled_at timestamptz,
                   api_code text)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    UPDATE public.operator_read_tickets AS k
       SET consumed_at = clock_timestamp()
     WHERE k.ticket_hash = encode(sha256(convert_to(
               p_ticket || jsonb_build_object('tenant_id', p_tenant_id)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'tenant_detail'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_tenant_detail: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    RETURN QUERY
        SELECT t.id, t.name, t.created_at, t.plan, t.business_type,
               (SELECT count(*) FROM public.locations AS l
                 WHERE l.tenant_id = p_tenant_id),
               (SELECT count(*) FROM public.employees AS e
                 WHERE e.tenant_id = p_tenant_id AND e.status = 'active'),
               (SELECT count(*) FROM public.tags AS g
                 WHERE g.tenant_id = p_tenant_id AND g.status = 'active'),
               (SELECT count(*) FROM public.admin_users AS a
                 WHERE a.tenant_id = p_tenant_id AND a.status = 'active'),
               t.api_enabled_at, t.api_code
          FROM public.tenants AS t
         WHERE t.id = p_tenant_id;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_tenant_detail(text, text, uuid) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_tenant_detail(text, text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_tenant_detail(text, text, uuid) TO tappa_operator;

-- +goose Down

-- 🔴 WHAT A SUCCESSFUL Down DOES: op_read_tenant_detail goes back to 00029's definition VERBATIM --
-- dropped and created again (its result type changes back), with its owner, REVOKE and GRANT
-- written again. Nothing else: this file granted nothing and wrote no row. No table is locked (a
-- function's DROP and CREATE touch the catalogue only), so opAtVersion walks this Down inside other
-- packages' test runs without a table lock.
DROP FUNCTION IF EXISTS public.op_read_tenant_detail(text, text, uuid);
-- +goose StatementBegin
CREATE FUNCTION public.op_read_tenant_detail(p_session text, p_ticket text, p_tenant_id uuid)
    RETURNS TABLE (tenant_id uuid, tenant_name text, created_at timestamptz, plan text,
                   business_type text, location_count bigint,
                   active_employee_count bigint, active_plaque_count bigint,
                   active_admin_count bigint)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    UPDATE public.operator_read_tickets AS k
       SET consumed_at = clock_timestamp()
     WHERE k.ticket_hash = encode(sha256(convert_to(
               p_ticket || jsonb_build_object('tenant_id', p_tenant_id)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'tenant_detail'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_tenant_detail: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    RETURN QUERY
        SELECT t.id, t.name, t.created_at, t.plan, t.business_type,
               (SELECT count(*) FROM public.locations AS l
                 WHERE l.tenant_id = p_tenant_id),
               (SELECT count(*) FROM public.employees AS e
                 WHERE e.tenant_id = p_tenant_id AND e.status = 'active'),
               (SELECT count(*) FROM public.tags AS g
                 WHERE g.tenant_id = p_tenant_id AND g.status = 'active'),
               (SELECT count(*) FROM public.admin_users AS a
                 WHERE a.tenant_id = p_tenant_id AND a.status = 'active')
          FROM public.tenants AS t
         WHERE t.id = p_tenant_id;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_tenant_detail(text, text, uuid) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_tenant_detail(text, text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_tenant_detail(text, text, uuid) TO tappa_operator;
