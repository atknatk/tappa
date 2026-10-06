-- 00030 -- M10 OP-13, phase A (the data layer): the operator reads ONE tenant's plaque
-- inventory -- every plaque the tenant holds, in stock, on a wall, retired or lost, with
-- its encode stamp -- through one op_read_* and nothing else.
--
-- NORMATIVE SOURCES: docs/adr/0021-op-fonksiyonlari-tenant-otesi-erisim.md §1 (the
-- "asla" columns: tags.aes_key_ref and tags.app_key_ref are never SELECTed by
-- tappa_opdefiner), §2 (the op_* contract: the session predicate, two-phase reads, the
-- wall clock, the LIMIT ceiling, the fixed column list), §3 (inside an op_* the explicit
-- tenant filter is the ONLY barrier) and §6 (the catalogue and behaviour pins); ADR 0020
-- §9 (a tenant-crossing screen names its tenant). Where this file decides something
-- those leave open, the decision is written next to the statement and in ADR 0021's
-- "OP-13 uygulama notu".
--
-- 🔴 CLAUDE.md §4.5 CROSSED ON PURPOSE, IN THE ONE PLACE ADR 0021 ALLOWS, AND §4.7 NOT
-- CROSSED AT ALL. The new function belongs to tappa_opdefiner (NOLOGIN, BYPASSRLS), so NO
-- ROW LEVEL SECURITY APPLIES INSIDE IT (ADR 0021 §3.2). What stands instead, named:
--   * every table reference names the tenant itself: `t.id = p_tenant_id`,
--     `g.tenant_id = p_tenant_id`, `l.tenant_id = p_tenant_id` -- the belt with no
--     braces behind it (TestOpReadTenantPlaques_ReturnsOnlyTheNamedTenantsPlaques drives
--     two tenants through it);
--   * the plaque keys are not in the function's column grants: tappa_opdefiner holds no
--     SELECT on aes_key_ref or app_key_ref, and PostgreSQL asks for SELECT on a column
--     wherever a statement names it -- the result, a WHERE, an expression such as
--     `app_key_ref IS NOT NULL` (ADR 0021 §1's measurement). The forms measured to fail
--     with 42501 -- a changed function body among them -- are listed in ADR 0021's
--     "OP-13 uygulama notu", PART I (TestOperator00030_TheDefinerCannotReadAPlaqueKey).
--     The one encode signal the read returns is encoded_at (ADR 0017 §5.1 step 9: the
--     chip took its keys);
--   * the read writes nothing to tags: the definer holds no INSERT, UPDATE, DELETE or
--     TRUNCATE on it, so last_ctr is only ever READ here and CLAUDE.md §4.4's atomic
--     advance (db/queries/tags.sql AdvanceTagCounter) is untouched
--     (TestOpReadTenantPlaques_TheReadWritesNoPlaque).
--
-- WHAT IT DOES, IN ORDER:
--   0. re-checks the two cluster roles' shape -- 00026's precondition, repeated as 00027
--      and 00029 repeated it, because a new SECURITY DEFINER function is about to belong
--      to tappa_opdefiner;
--   1. widens operator_read_tickets_kind_check with 'tenant_plaques';
--   2. the grants: tappa_opdefiner gains a column SELECT on the tags and locations
--      columns the read names and 00029 did not grant -- and on neither plaque key;
--   3. op_begin_read is REPLACED (00027: "CREATE OR REPLACE") so it names the new kind;
--      its other three branches are 00029's, unchanged;
--   4. one function: op_read_tenant_plaques.
--
-- No table is created here, so there is no redline waiver, no new RLS and no sequence to
-- REVOKE. tappa_app gains nothing (ADR 0021 §1); tappa_operator gains EXECUTE on the one
-- function and no privilege on any table.
--
-- CLOCKS (ADR 0021 §2 vii): the one comparison the read makes (expires_at >
-- clock_timestamp()) and the one timestamp it writes (consumed_at) come from
-- clock_timestamp(); op_begin_read's clocks are 00027's. The catalogue scan of ADR 0021
-- §6 (TestOperator00026_NoFrozenClock) walks every function tappa_opdefiner owns.
--
-- SECRETS (ADR 0021 §3.5, CLAUDE.md §4.7, §7): the RAISE EXCEPTIONs carry constant
-- messages; the one formatted RAISE EXCEPTION is the precondition's (it formats role
-- NAMES); the one RAISE LOG is op_begin_read's (a constraint name and a SQLSTATE --
-- 00027's). No key, no key-derived value and no "has a key 0" flag leaves this file.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION: the two roles still have the shape ADR 0021 §1 names.
-- ---------------------------------------------------------------------------
-- 00026's checks, in 00026's order, as 00027 and 00029 repeated them: an op_* owned by a
-- SUPERUSER, a LOGIN or a member-carrying tappa_opdefiner is a general bypass with every
-- other check green, and a member of tappa_operator inherits EXECUTE on the function
-- below -- which reads ANY tenant's plaques.
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
        RAISE EXCEPTION 'migration 00030 (M10 OP-13) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00030 (M10 OP-13): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00030 (M10 OP-13): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00030 (M10 OP-13): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00030 (M10 OP-13): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00030 (M10 OP-13): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00030 (M10 OP-13): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1: tappa_app gets no new privilege). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The closed set of read kinds widens.
-- ---------------------------------------------------------------------------
--   'tenant_plaques'  op_read_tenant_plaques: one tenant's plaque inventory.
-- No audit kind is added: the read is one 'read' row whose target_scope is the read kind
-- (00027 section 1) and whose target_tenant_id names the tenant (00026's column, no
-- foreign key -- ADR 0021 B14).
-- The constraint keeps its NAME so the next migration finds it where this one did.
ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
    CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques'));

-- ---------------------------------------------------------------------------
-- 2. Grants: what the read touches and 00029 did not already grant.
-- ---------------------------------------------------------------------------
-- tappa_opdefiner, column by column (ADR 0021 §1; the WHOLE resulting list is pinned in
-- TestOperator00026_PrivilegeMatrix's allow-list):
--   tags       uid, location_id, last_ctr, retired_at, replaced_by, created_at,
--              encoded_at -- with 00029's tenant_id and status, every column of the
--              plaque EXCEPT the two keys. NOT aes_key_ref, NOT app_key_ref: ADR 0021
--              §1's "asla" columns. last_ctr is the chip's read counter, not a secret
--              (in plain SDM it travels in the URL, ADR 0003) -- the support question
--              "did a tap ever reach the server" is answered by it (orchestrator decision
--              K13-1, OP-13 card);
--   locations  id, name -- which entrance a plaque is mounted at, by its name. Not the
--              address ranges, the coordinates, the shift or the Wi-Fi name.
--   tenants    nothing new: 00029's id and name are what the header needs.
-- 🔴 00029's columns are NOT granted again, and this file's Down does NOT revoke them
-- (and does not write REVOKE ALL): a column holds ONE ACL entry per grantee, so taking
-- back a column 00029 granted would take 00029's grant with it and break the tenant
-- overview's counts (TestOperator00030_DownGivesBack00029AndUpTakesItAgain measures the
-- definer's lists after Down equal to 00029's).
-- tappa_operator gets nothing on either table: it reaches the rows through the function
-- only. tappa_app's own grants on these tables are not touched.
GRANT SELECT (uid, location_id, last_ctr, retired_at, replaced_by, created_at, encoded_at)
    ON tags TO tappa_opdefiner;
GRANT SELECT (id, name) ON locations TO tappa_opdefiner;

-- ===========================================================================
-- 3. op_begin_read, REPLACED: one more read kind
-- ===========================================================================
-- Everything 00027 section 4.1 and 00029 section 3 say of this function still holds:
-- the session through op_touch_session, the read's 'read' row and its ticket written by
-- ONE statement and tied by audit_id, the raw ticket drawn from three gen_random_uuid()
-- (pinned by TestOpBeginRead_TheTicketIsDrawnFromTheStrongRandomSource), 30 seconds of
-- life, the hashed text rebuilt from TYPED values (v_bound), the caught constraint, and
-- the three kinds 00029 named, branch for branch. What changes is two lines:
--   * the closed set names 'tenant_plaques';
--   * 'tenant_plaques' takes 00029's 'tenant_detail' branch: the parameter object is
--     EXACTLY {tenant_id}, a uuid in its hyphenated form (either case), and v_bound holds
--     the uuid VALUE, so the canonical lower-case text is what is hashed. The two kinds
--     therefore hash the SAME text for the same tenant, and what keeps a detail ticket
--     from opening the inventory (and the other way round) is the KIND, which the ticket
--     row carries and both reads compare (k.kind = '...'); the source pin
--     TestOpRead_EveryReadConsumesItsTicketAsTheADRSays holds that condition in every
--     op_read_*, and TestOpReadTenantPlaques_AForgedTicketIsRefused drives the confusion
--     both ways.
-- THE AUDIT ROW names what was read: target_scope 'tenant_plaques', target_tenant_id =
-- the tenant asked for, no page, detail {}. The ticket row carries the same
-- target_tenant_id (informational -- what binds the ticket to the tenant is the hash).
-- 🔴 op_begin_read STILL DOES NOT LOOK THE TENANT UP (00029 section 3, the B14 argument
-- for a read): an unknown tenant is answered by phase two -- zero rows -- after the 'read'
-- row naming it has committed.
-- The refusals keep 00027's two messages and codes (22023 for a kind or parameter object
-- the function does not name, 28000 for a write a constraint refuses).
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
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques') THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_keys := (SELECT array_agg(k.key ORDER BY k.key) FROM jsonb_object_keys(p_params) AS k(key));

    IF p_kind IN ('tenant_detail', 'tenant_plaques') THEN
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
    ELSE
        -- (Two IFs, not a CASE inside the condition: PL/pgSQL ends an IF condition at the
        -- first THEN it reads, a CASE's included.)
        IF p_kind = 'tenants' THEN
            v_want := ARRAY['page_number', 'page_size', 'query'];
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
-- CREATE OR REPLACE keeps the owner and the ACL; they are written again so this file,
-- read alone, shows the whole contract of the function it (re)defines (ADR 0021 §2 vi).
ALTER FUNCTION public.op_begin_read(text, text, jsonb) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_begin_read(text, text, jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_begin_read(text, text, jsonb) TO tappa_operator;

-- ===========================================================================
-- 4. op_read_tenant_plaques -- one tenant's plaque inventory
-- ===========================================================================
-- The op_read_* shape 00027 gave the first read and TestOpRead_EveryReadConsumesItsTicketAsTheADRSays
-- pins for EVERY op_read_* by name: the session through op_touch_session first; ONE
-- consuming UPDATE of the ticket whose WHERE holds the six conditions POSITIVELY -- the
-- hash of (raw ticket, {tenant_id} rebuilt from the typed argument), this session, this
-- kind, not yet consumed, not expired by the WALL clock, created by a COMMITTED
-- transaction --; the refusal at once (28000); the rows only after it. ADR 0021 limit 4
-- holds unchanged (a rolled-back read un-consumes its ticket for the rest of its 30
-- seconds; the re-read writes no new row and the committed one names the same read).
--
-- ONE READ, THREE ANSWERS -- so the screen pays for one read and the trail holds one row:
--   * the tenant's NAME for the header of the screen (ADR 0020 §9), on every row;
--   * WHETHER THE TENANT EXISTS: an unknown id reads ZERO rows -- not an error, and
--     distinct from every refusal (28000) and every argument error (22023). By then the
--     'read' row naming that id has committed (phase one), so "no such tenant" is an
--     answer the trail already holds;
--   * the PLAQUES: a tenant with none reads ONE row whose plaque columns are NULL and
--     whose plaque_count is 0 (the LEFT JOIN keeps the tenant's row).
--
-- THE COLUMNS (a fixed list, ADR 0021 §2 ii) -- the tenant's own plaque list
-- (db/queries/tags.sql ListTagsForTenant) and the location's name, nothing else:
--   tenant_id, tenant_name          the tenant (tenants.id, tenants.name);
--   uid                             the plaque (public: printed on it, in its URL);
--   status                          VERBATIM -- no CASE, no mapping, no WHERE on it. A
--                                   value tags_status_check does not name today reaches
--                                   the caller as itself and is not dropped or renamed;
--                                   the Go side's closed mapping
--                                   (internal/db TenantPlaque.Shape) answers
--                                   "unrecognised" for it (fail-closed);
--   location_id, location_name      the entrance it is mounted at (NULL in stock);
--   encoded_at                      the ONE encode signal (ADR 0017 §5.1 step 9). An
--                                   'active' row with no stamp is the A-1 shape (backlog
--                                   T75: on a wall, never encoded) and is told apart by
--                                   status + encoded_at in the row itself;
--   created_at, retired_at          the row's life;
--   replaced_by                     the uid that replaced a retired plaque. It can only
--                                   name a plaque of THE SAME tenant: tags_replaced_by_fk
--                                   is (replaced_by, tenant_id) -> tags (uid, tenant_id).
--                                   Returned as text, never joined;
--   last_ctr                        the read counter (section 2, K13-1);
--   plaque_count                    EVERY plaque of the tenant, counted before the LIMIT
--                                   (a window over the joined rows; window functions are
--                                   evaluated before ORDER BY and LIMIT), so a caller
--                                   can say "the first 200 of N".
-- NOT aes_key_ref, NOT app_key_ref, and nothing computed from them -- see the header.
--
-- 🔴 THE BELT, ONE FILTER PER TABLE REFERENCE (ADR 0021 §3.2):
--   `t.id = p_tenant_id`         the one tenant row;
--   `g.tenant_id = p_tenant_id`  the plaques -- in the JOIN condition itself, not through
--                                t: without it the LEFT JOIN would bring every tenant's
--                                plaques (measured by the two-tenant test);
--   `l.tenant_id = p_tenant_id`  the location. Structurally redundant today --
--                                tags_location_fk is (location_id, tenant_id) ->
--                                locations (id, tenant_id), so a plaque's location is
--                                always its tenant's -- and written anyway, because the
--                                rule is "every reference names the tenant", not "every
--                                reference whose removal a test can see".
-- THE ORDER is the tenant's own list's (ListTagsForTenant): stock first (location_id
-- NULLS FIRST), then by uid -- uid is the primary key, so the order is total.
-- THE CEILING: LIMIT 200 in the body (ADR 0021 §2 iii). No paging (orchestrator decision
-- K13-2): a venue holds plaques by the handful; plaque_count says when there are more.
-- COST, AN OBSERVATION: the plaques come from tags_tenant_idx (tenant_id, location_id),
-- the locations from locations_tenant_idx, one sort of the tenant's rows -- the plan
-- `EXPLAIN` gives for the body's SELECT with the tenant that holds the most plaques
-- (`SELECT tenant_id FROM tags GROUP BY 1 ORDER BY count(*) DESC LIMIT 1`).
-- +goose StatementBegin
CREATE FUNCTION public.op_read_tenant_plaques(p_session text, p_ticket text, p_tenant_id uuid)
    RETURNS TABLE (tenant_id uuid, tenant_name text, uid text, status text,
                   location_id uuid, location_name text, encoded_at timestamptz,
                   created_at timestamptz, retired_at timestamptz, replaced_by text,
                   last_ctr integer, plaque_count bigint)
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
       AND k.kind = 'tenant_plaques'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_tenant_plaques: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    RETURN QUERY
        SELECT t.id, t.name, g.uid::text, g.status, g.location_id, l.name,
               g.encoded_at, g.created_at, g.retired_at, g.replaced_by::text, g.last_ctr,
               count(g.uid) OVER ()
          FROM public.tenants AS t
          LEFT JOIN public.tags AS g
                 ON g.tenant_id = p_tenant_id
          LEFT JOIN public.locations AS l
                 ON l.id = g.location_id AND l.tenant_id = p_tenant_id
         WHERE t.id = p_tenant_id
         ORDER BY g.location_id NULLS FIRST, g.uid
         LIMIT 200;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_tenant_plaques(text, text, uuid) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_tenant_plaques(text, text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_tenant_plaques(text, text, uuid) TO tappa_operator;

-- +goose Down

-- 🔴 WHAT A SUCCESSFUL Down DOES, named (00022's, 00026's, 00027's and 00029's house style):
--   * the read goes, and op_begin_read goes back to 00029's Up body VERBATIM (three read
--     kinds) -- replaced, not dropped, so its owner and ACL stay
--     (TestOperator00030_DownGivesBack00029AndUpTakesItAgain compares it with 00029's
--     file);
--   * tappa_opdefiner loses EXACTLY the column privileges section 2 granted -- by column,
--     NOT `REVOKE ALL` (00029's Down writes REVOKE ALL on these tables; repeated here it
--     would take 00029's tags (tenant_id, status) and locations (tenant_id) away too, and
--     the tenant overview's counts would fail with 42501). After Down the definer's lists
--     on tags and locations are 00029's;
--   * the 'read' rows the read committed STAY: operator_audit_log is append-only (00026:
--     its triggers bind the owner too) and they are the evidence;
--   * operator_read_tickets_kind_check goes back to 00029's closed set -- NOT VALID when
--     ANY ticket exists whose kind that set does not name, consumed or not, this file's
--     kind or a later migration's; VALIDATED when none does. Tickets are never deleted by
--     any product path (no role but the owner holds DELETE), so on a database that has
--     served the plaque screen they exist; the CHECK then binds every new row and leaves
--     the old ones alone (00027's and 00029's Downs do the same).
--     🔴 WHY "ANY KIND OUTSIDE THE SET" AND NOT "THIS FILE'S KIND" (OP-13 A, 2nd round, a
--     review measured it): a later migration whose Down leaves ITS kind's tickets behind
--     under a NOT VALID CHECK hands this Down a table holding a kind this file never
--     named; asking only for 'tenant_plaques' then picked the VALIDATED branch and the
--     ADD failed (23514) -- the Downs did not compose. The set below is 00029's, word for
--     word (TestOperator00030_DownGivesBack00029AndUpTakesItAgain derives it from 00029's
--     file and compares; the same test drives a consumed ticket and a later kind).
--     ⚠️ COUNTED, NOT CLOSED: 00029's own Down (applied, immutable) picks NOT VALID only
--     when a ticket of ITS two kinds ('tenants', 'tenant_detail') exists. So with a ticket
--     of a kind outside 00029's set present and NONE of those two kinds, the 30 -> 29 step
--     succeeds (NOT VALID) and the 29 -> 28 step fails with 23514 (measured in the same
--     test). With a 'tenants' or 'tenant_detail' ticket present as well -- the shape of a
--     database whose tenant screens have served -- 00029's Down takes its NOT VALID branch
--     and 29 -> 28 succeeds (the closing review measured it). A Down that ran on from 29
--     with neither would need the outside kinds' tickets gone first; nothing in this file
--     deletes a ticket.
DROP FUNCTION IF EXISTS public.op_read_tenant_plaques(text, text, uuid);

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
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail') THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_keys := (SELECT array_agg(k.key ORDER BY k.key) FROM jsonb_object_keys(p_params) AS k(key));

    IF p_kind = 'tenant_detail' THEN
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
    ELSE
        -- (Two IFs, not a CASE inside the condition: PL/pgSQL ends an IF condition at the
        -- first THEN it reads, a CASE's included.)
        IF p_kind = 'tenants' THEN
            v_want := ARRAY['page_number', 'page_size', 'query'];
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

REVOKE SELECT (uid, location_id, last_ctr, retired_at, replaced_by, created_at, encoded_at)
    ON tags FROM tappa_opdefiner;
REVOKE SELECT (id, name) ON locations FROM tappa_opdefiner;

ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_read_tickets
                WHERE kind <> ALL (ARRAY['legal_versions', 'tenants', 'tenant_detail'])) THEN
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail'));
    END IF;
END
$$;
-- +goose StatementEnd
