-- 00029 -- M10 OP-11, phase A (the data layer): the operator reads the tenant list
-- (with a search) and one tenant's overview, through two op_read_* and nothing else.
--
-- NORMATIVE SOURCES: docs/adr/0021-op-fonksiyonlari-tenant-otesi-erisim.md §2 (the op_*
-- contract: the session predicate, two-phase reads, the wall clock, the LIMIT ceiling,
-- the fixed column list), §3 (the tenant boundary: inside an op_* the explicit
-- tenant_id filter is the ONLY barrier) and §6 (the catalogue and behaviour pins);
-- ADR 0020 §9 (a tenant-crossing screen names its tenant). Where this file had to decide
-- something those left open, the decision is written next to the statement and in ADR
-- 0021's "OP-11 notu".
--
-- 🔴 THIS IS THE FIRST op_* THAT READS TENANT DATA -- CLAUDE.md §4.5 CROSSED ON PURPOSE,
-- IN THE ONE PLACE ADR 0021 ALLOWS. Both functions belong to tappa_opdefiner (NOLOGIN,
-- BYPASSRLS), so NO ROW LEVEL SECURITY APPLIES INSIDE THEM (ADR 0021 §3.2): the
-- tenant-isolation policies of tenants, locations, employees, tags and admin_users do
-- not bind the definer. What stands instead, named:
--   * op_read_tenants scans EVERY tenant on purpose -- it is the list ADR 0021 §3.2 names
--     ("bilerek butun tenant'lari tarayan fonksiyonlar (tenant listesi)") -- and is
--     bounded by its fixed four columns and the 200-row ceiling;
--   * op_read_tenant_detail names ONE tenant, and every query of tenant data in it (the
--     row and its four counts) carries `x.tenant_id = p_tenant_id` (the row:
--     `t.id = p_tenant_id`) -- the belt with no braces behind it. A forgotten filter would count every tenant's rows, which is what the
--     behaviour test with two tenants of different sizes measures
--     (TestOpReadTenantDetail_CountsOnlyTheNamedTenantsRows).
--
-- WHAT IT DOES, IN ORDER:
--   0. re-checks the two cluster roles' shape -- 00026's precondition, repeated as
--      00027 repeated it, because two new SECURITY DEFINER functions are about to
--      belong to tappa_opdefiner;
--   1. widens operator_read_tickets_kind_check (00027: "extended by the migration that
--      adds the next op_read_*") with 'tenants' and 'tenant_detail';
--   2. the grants: tappa_opdefiner gains a column SELECT on the five tables the two
--      reads touch -- and on none of ADR 0021 §1's "asla" columns;
--   3. op_begin_read is REPLACED (00027: "CREATE OR REPLACE") so it names the two new
--      kinds and their parameter objects; the legal_versions branch is unchanged;
--   4. two functions: op_read_tenants and op_read_tenant_detail.
--
-- No table is created here, so there is no redline waiver, no new RLS and no sequence
-- to REVOKE. tappa_app gains nothing (ADR 0021 §1) and tappa_operator gains EXECUTE on
-- the two functions only -- no privilege on any tenant table (pinned:
-- TestOperator00029_TheDefinerReadsNamedColumnsAndNoSecret).
--
-- CLOCKS (ADR 0021 §2 vii): the one comparison the two reads make
-- (expires_at > clock_timestamp()) and the one timestamp they write (consumed_at) come
-- from clock_timestamp(); op_begin_read's clocks are 00027's. The catalogue scan of ADR
-- 0021 §6 (TestOperator00026_NoFrozenClock) walks every function tappa_opdefiner owns.
--
-- SECRETS (ADR 0021 §3.5, CLAUDE.md §7): the RAISE EXCEPTIONs carry constant messages;
-- the one formatted RAISE EXCEPTION is the precondition's (it formats role NAMES); the
-- one RAISE LOG is op_begin_read's (a constraint name and a SQLSTATE -- 00027's). The
-- search term is personal data when it is an address and is NEVER written to a column:
-- not to operator_audit_log (ADR 0021 §2 v 1 -- neither raw nor hashed) and not to the
-- ticket row, which holds sha256(raw ticket || the bound parameters). What the audit row
-- does carry is the term's CLASS (none, text, address, id -- section 3), which holds no
-- character of the term.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION: the two roles still have the shape ADR 0021 §1 names.
-- ---------------------------------------------------------------------------
-- 00026's checks, in 00026's order, as 00027 repeated them: an op_* owned by a
-- SUPERUSER, a LOGIN or a member-carrying tappa_opdefiner is a general bypass with every
-- other check green, and a member of tappa_operator inherits EXECUTE on the functions
-- below -- which READ EVERY TENANT.
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
        RAISE EXCEPTION 'migration 00029 (M10 OP-11) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00029 (M10 OP-11): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00029 (M10 OP-11): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00029 (M10 OP-11): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00029 (M10 OP-11): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00029 (M10 OP-11): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00029 (M10 OP-11): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1: tappa_app gets no new privilege). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The closed set of read kinds widens.
-- ---------------------------------------------------------------------------
--   'tenants'        op_read_tenants: the list, with an optional search term;
--   'tenant_detail'  op_read_tenant_detail: one tenant's overview.
-- No audit kind is added: a read is one 'read' row whose target_scope is the read kind
-- (00027 section 1), so the two reads are told apart by target_scope = 'tenants' /
-- 'tenant_detail', and the detail's row names its tenant in target_tenant_id (00026's
-- column, no foreign key -- ADR 0021 B14).
-- The constraint keeps its NAME so the next migration finds it where this one did.
ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
ALTER TABLE operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
    CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail'));

-- ---------------------------------------------------------------------------
-- 2. Grants: what the two reads touch, column by column.
-- ---------------------------------------------------------------------------
-- tappa_opdefiner, exactly what the two functions name (ADR 0021 §1: column-level; the
-- list below is pinned whole in TestOperator00026_PrivilegeMatrix's allow-list):
--   tenants      id, name, created_at, plan -- the list's four columns -- and
--                business_type for the overview. NOT structure: tenants.structure is
--                read by nothing after sign-up (M7-03 B's decision, pinned by
--                internal/handler's TestSignupStructure_DecidesNothingAfterSignUp),
--                and the overview does not become its first reader. NOT vat_number (a
--                search key the operator does not get here; OP-16's), NOT
--                price_per_employee_month, timezone or the VAT verdict (OP-12/OP-16
--                read what they need).
--   locations    tenant_id: the overview counts a tenant's places.
--   employees    tenant_id, status: the overview counts the ACTIVE people. Not the
--                name, not the address.
--   tags         tenant_id, status: the overview counts the ACTIVE plaques. NOT
--                aes_key_ref, NOT app_key_ref -- ADR 0021 §1's "asla" columns.
--   admin_users  tenant_id, status: the overview counts the active panel accounts;
--                email: the list's search matches an address EXACTLY (section 4.2).
--                The address is used in a WHERE and returned by nothing. NOT
--                password_hash ("asla").
-- tappa_operator gets nothing on any of them: it reaches the rows through the two
-- functions only. tappa_app's own grants on these tables are not touched.
GRANT SELECT (id, name, created_at, plan, business_type) ON tenants TO tappa_opdefiner;
GRANT SELECT (tenant_id) ON locations TO tappa_opdefiner;
GRANT SELECT (tenant_id, status) ON employees TO tappa_opdefiner;
GRANT SELECT (tenant_id, status) ON tags TO tappa_opdefiner;
GRANT SELECT (tenant_id, email, status) ON admin_users TO tappa_opdefiner;

-- ===========================================================================
-- 3. op_begin_read, REPLACED: two more read kinds
-- ===========================================================================
-- Everything 00027 section 4.1 says of this function still holds: the session through
-- op_touch_session, the read's 'read' row and its ticket written by ONE statement and
-- tied by audit_id, the raw ticket drawn from three gen_random_uuid() (pinned by
-- TestOpBeginRead_TheTicketIsDrawnFromTheStrongRandomSource), 30 seconds of life, the
-- hashed text rebuilt from TYPED values (v_bound), and the caught constraint. What
-- changes is the closed set of kinds and the parameter object each one names EXACTLY:
--   legal_versions  {page_number, page_size}            -- 00027's, unchanged;
--   tenants         {page_number, page_size, query}     -- query a JSON string of at
--                   most 254 characters ('' lists every tenant). 254 is the longest
--                   thing the term can usefully be: an e-mail address (platform_admins'
--                   CHECK and the SMTP path limit); a company name is at most 120
--                   (internal/domain/signup.MaxCompanyNameRunes). The Go accessor
--                   carries the same number (db.MaxTenantSearchRunes), pinned against
--                   this check by TestTenantList_TheSearchTermMeetsTheSameBoundInGoAndSQL;
--   tenant_detail   {tenant_id}                          -- a uuid in its hyphenated
--                   form (either case); v_bound holds the uuid VALUE, so the canonical
--                   lower-case text is what is hashed whatever case was sent.
-- THE AUDIT ROW names what was read and nothing the operator typed:
--   tenants        target_scope 'tenants', page_number/page_size, and detail
--                  {"search": <class>} -- the "arama yapildi" fact ADR 0021 §2 v 1 asks
--                  the row to carry, with the KIND of search, derived from the term and
--                  holding none of it. The rules, in this order (first match wins):
--                    'none'     the term is '' (the whole list);
--                    'id'       the whole term is a hyphenated uuid (either case);
--                    'address'  the term contains '@';
--                    'text'     anything else.
--                  WHY A CLASS: an exact address search answers "which tenant has an admin
--                  with this address" -- a lookup of personal data -- and with a bare
--                  {"search": true} an operator (or a DSN holder) trying a list of
--                  addresses showed up as N searches, indistinguishable from N name
--                  searches. The class makes the volume of that use visible by its kind.
--                  WHY THESE RULES AND THIS ORDER: they are the branches op_read_tenants
--                  (4.1) can take for the term -- the id branch runs only for a whole uuid,
--                  the address branch only for a term with '@' (gated there on the SAME
--                  predicate), and the name match always -- so the class names the
--                  narrowest branch the term can reach: a 'text' search can match names
--                  only; an 'address' search can also match an admin's address; an 'id'
--                  search can also match a tenant's id. A uuid holds no '@', so 'id' and
--                  'address' cannot both apply; 'none' first because '' is neither.
--                  THE CLASS CANNOT BE FORGED BY THE CALLER: it is derived HERE from the
--                  term, and the term is bound into the ticket hash, so the read that
--                  follows runs exactly the term the class was derived from. The term
--                  itself is bound only inside the ticket hash. (The number of results
--                  cannot be in the row: it is committed BEFORE the query runs -- the
--                  point of the two phases.)
--   tenant_detail  target_scope 'tenant_detail', target_tenant_id = the tenant asked
--                  for, no page, detail {}. The ticket row carries the same
--                  target_tenant_id (00026's column): informational -- what binds the
--                  ticket to the tenant is the hash.
-- 🔴 op_begin_read DOES NOT LOOK THE TENANT UP. Its refusal leaves no row (ADR 0021 §2 v
-- 9), so a phase one that refused an unknown tenant would answer "does this tenant
-- exist" to a caller who rolls it back -- untraced (the B14 argument, here for a read).
-- The existence of the tenant is answered by phase two, after the 'read' row naming it
-- has committed: an unknown id reads zero rows.
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
-- CREATE OR REPLACE keeps the owner and the ACL; they are written again so this file,
-- read alone, shows the whole contract of the function it (re)defines (ADR 0021 §2 vi).
ALTER FUNCTION public.op_begin_read(text, text, jsonb) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_begin_read(text, text, jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_begin_read(text, text, jsonb) TO tappa_operator;

-- ===========================================================================
-- 4. THE TWO READS
-- ===========================================================================
-- Both carry 00026's contract (pinned in the live catalogue for every function
-- tappa_opdefiner owns) and the op_read_* shape 00027 gave the first one and
-- TestOpRead_EveryReadConsumesItsTicketAsTheADRSays pins for EVERY op_read_* by name:
-- the session through op_touch_session first; ONE consuming UPDATE of the ticket whose
-- WHERE holds the six conditions POSITIVELY -- the hash of (raw ticket, this read's
-- parameters rebuilt from its own typed arguments), this session, this kind, not yet
-- consumed, not expired by the WALL clock, created by a COMMITTED transaction --; the
-- refusal at once (28000); the rows only after it. ADR 0021 limit 4 holds unchanged (a
-- rolled-back read un-consumes its ticket for the rest of its 30 seconds; the re-read
-- writes no new row and the committed one names the same read).

-- ---------------------------------------------------------------------------
-- 4.1 op_read_tenants -- the tenant list and its search
-- ---------------------------------------------------------------------------
-- THE ROWS: every tenant, or the tenants the term matches; a fixed column list (§2 ii):
--   tenant_id, tenant_name, created_at, plan
-- -- what tells one tenant from another in a list and nothing more (no address, no VAT
-- number, no price, no counts: the overview is a read of its own, audited on its own).
--
-- THE MATCH, ONE TERM AGAINST THREE THINGS (any one is enough):
--   * the NAME, as a case-insensitive SUBSTRING: strpos(lower(name), lower(term)) > 0.
--     🔴 NOT LIKE/ILIKE, AND THAT IS THE ANSWER TO "ESCAPE % _ \": strpos has no
--     metacharacter, so there is nothing to escape and no escape to get wrong -- a term
--     of '%' finds the names that contain a percent sign and no other (measured with a
--     name of each kind: TestOpReadTenants_LikeMetacharactersAreLiteral). '' is a
--     substring of every name: the empty term lists every tenant.
--   * an ADMIN'S ADDRESS, EXACTLY (case-insensitively -- citext, and its = is written
--     OPERATOR(public.=) because under this search_path a bare = would silently compare
--     text: ADR 0002's M6-01 trap). Exact, not a substring, ON PURPOSE: a substring
--     search over addresses is a browse of personal data ('@gmail' would list every
--     tenant with such an admin); an exact match is a lookup -- the operator must
--     already know the address, which is the support case (a customer writes in). The
--     address is used in the WHERE and returned by nothing. Every admin_users row
--     counts, whatever its status or role: support looks up a disabled owner too.
--     employees.email is NOT searched: an employee is not the operator's customer.
--     The match is t.id IN (the admins' tenant_ids) -- correlated by value, so an
--     address finds ITS tenant only (TestOpReadTenants_SearchMatchesNameAddressAndIDOnly
--     drives an address of one tenant against several). It RUNS ONLY FOR A TERM WITH
--     '@' -- the predicate op_begin_read classes 'address' by -- so a 'text' search in the
--     audit trail can have matched names only. Lost by the gate: an admin address
--     without '@', which sign-up refuses (internal/domain/signup) and of which the
--     development database holds none (2026-10-03: 0 of every admin_users row, by
--     `SELECT count(*) FROM admin_users WHERE strpos(email::text, '@') = 0`).
--   * the tenant's ID, when the whole term is a hyphenated uuid (either case) -- the
--     predicate op_begin_read classes 'id' by.
--   Nothing else is matched: not the plan, the business type, the structure, the VAT
--   number or the time zone (the same test drives each as a term).
-- THE ORDER: created_at DESC, id DESC -- newest customer first, and a key no tenant can
-- move: tappa_app may UPDATE tenants.name but not created_at (00024), so a tenant that
-- renames itself between two page loads cannot shift the operator's OFFSET pages. The
-- page: LIMIT least(size, 200) in the body (§2 iii; op_begin_read already refuses a
-- larger one -- the body does not trust that), OFFSET (number - 1) * that.
-- COST, AN OBSERVATION NOT A TARGET (dev database, 2026-10-03, 586 538 tenants by
-- `SELECT count(*) FROM tenants` -- the suite's fixtures, not customers; the live pilot
-- has a handful): the statement is a
-- parallel sequential scan of tenants plus a top-N sort, 88-196 ms for a first page with
-- or without a term; a deep page sorts everything (page 2000 of 200: 580 ms, external
-- merge). No index was added: production holds tenants by the dozen, and an index for
-- the development database's test residue would be a write cost paid for nothing.
-- +goose StatementBegin
CREATE FUNCTION public.op_read_tenants(p_session text, p_ticket text, p_query text,
                                       p_page_number integer, p_page_size integer)
    RETURNS TABLE (tenant_id uuid, tenant_name text, created_at timestamptz, plan text)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
    v_limit   integer;
    v_id      uuid;
    v_address boolean;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    UPDATE public.operator_read_tickets AS k
       SET consumed_at = clock_timestamp()
     WHERE k.ticket_hash = encode(sha256(convert_to(
               p_ticket || jsonb_build_object('page_number', p_page_number,
                                              'page_size', p_page_size,
                                              'query', p_query)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'tenants'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_tenants: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    v_limit := least(p_page_size, 200);
    IF p_query ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        v_id := p_query::uuid;
    END IF;
    v_address := strpos(p_query, '@') > 0;

    RETURN QUERY
        SELECT t.id, t.name, t.created_at, t.plan
          FROM public.tenants AS t
         WHERE strpos(lower(t.name), lower(p_query)) > 0
            OR t.id = v_id
            OR (v_address AND t.id IN (SELECT u.tenant_id
                                         FROM public.admin_users AS u
                                        WHERE u.email OPERATOR(public.=) p_query::public.citext))
         ORDER BY t.created_at DESC, t.id DESC
         LIMIT v_limit
        OFFSET (p_page_number - 1)::bigint * v_limit;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_tenants(text, text, text, integer, integer) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_tenants(text, text, text, integer, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_tenants(text, text, text, integer, integer) TO tappa_operator;

-- ---------------------------------------------------------------------------
-- 4.2 op_read_tenant_detail -- one tenant's overview (the header of its screens)
-- ---------------------------------------------------------------------------
-- THE ROW (zero or one): the tenant's identity for the banner every tenant screen
-- carries (ADR 0020 §9: "girdigi tenant'i basligi ADIYLA") and four counts that say
-- whether the tenant is using the product -- a fixed column list (§2 ii):
--   tenant_id, tenant_name, created_at, plan, business_type,
--   location_count         every location of the tenant (a location has no status);
--   active_employee_count  employees.status = 'active' (not invited, not deactivated);
--   active_plaque_count    tags.status = 'active' (on a wall and in service);
--   active_admin_count     admin_users.status = 'active' (panel accounts that can log in).
-- Not the tenant's data: no names, no addresses, no times of attendance, no plaque uid,
-- no key. The inventory by status is OP-13's read, the billable headcount OP-12's.
-- 🔴 EVERY QUERY OF TENANT DATA NAMES THE TENANT (the belt with no braces, ADR 0021
-- §3.2): the four counts are scalar subqueries each with its own
-- `x.tenant_id = p_tenant_id`, and the row itself is `t.id = p_tenant_id`.
-- AN UNKNOWN TENANT reads ZERO rows -- not an error. By then the 'read' row naming that id
-- has committed (phase one), so "this tenant does not exist" is an answer the trail
-- already holds; and it is distinct from every refusal (28000) and every argument error
-- (22023), so a caller can tell "no such tenant" from "not allowed". (The counts' plans
-- are lazy: for zero rows none of the four subqueries runs.)
-- COST, AN OBSERVATION: the development tenant with the most employees (test residue
-- that grows with every run, so its size is not quoted -- the query is `SELECT tenant_id,
-- count(*) FROM employees GROUP BY 1 ORDER BY 2 DESC LIMIT 1`) read in 10-13 ms warm and
-- 1.4 s cold on 2026-10-03 (the employee bitmap heap scan); each count walks its table's
-- tenant index.
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

-- +goose Down

-- 🔴 WHAT A SUCCESSFUL Down DOES, named (00022's, 00026's and 00027's house style):
--   * the two reads go, and op_begin_read goes back to 00027's body VERBATIM (one read
--     kind, legal_versions) -- replaced, not dropped, so its owner and ACL stay;
--   * tappa_opdefiner loses every privilege on the five tables (00027 left it none on
--     any of them -- TestOperator00026_PrivilegeMatrix's allow-list before this file);
--   * the 'read' rows the two reads committed STAY: operator_audit_log is append-only
--     (00026: its triggers bind the owner too) and they are the evidence. 'read' is
--     00027's kind and target_scope is a shape, so they need nothing from this Down;
--   * operator_read_tickets_kind_check goes back to 00027's closed set -- VALIDATED when
--     no ticket of the two kinds exists, NOT VALID when one does. Tickets are never
--     deleted by any product path (no role but the owner holds DELETE), so on a database
--     that has served the tenant screens they exist; the CHECK then binds every new row
--     and leaves the old ones alone (00027's own Down does the same for its audit kinds).
--     Measured on the development database: with no such ticket the schema after Down
--     equals 00028's in a pg_dump comparison.
DROP FUNCTION IF EXISTS public.op_read_tenant_detail(text, text, uuid);
DROP FUNCTION IF EXISTS public.op_read_tenants(text, text, text, integer, integer);

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
    v_page_number integer;
    v_page_size   integer;
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
    -- order, and jsonb_object_keys on a non-object raises instead of answering false.
    IF p_kind IS DISTINCT FROM 'legal_versions' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF (SELECT array_agg(k.key ORDER BY k.key) FROM jsonb_object_keys(p_params) AS k(key))
           IS DISTINCT FROM ARRAY['page_number', 'page_size']
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
    v_bound := jsonb_build_object('page_number', v_page_number, 'page_size', v_page_size);

    v_ticket := encode(sha256(uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())), 'hex');

    BEGIN
        WITH audit AS (
            INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_scope,
                                                   page_number, page_size)
            VALUES ('read', v_session, v_admin, p_kind, v_page_number, v_page_size)
            RETURNING id
        )
        INSERT INTO public.operator_read_tickets (ticket_hash, session_id, kind, audit_id,
                                                  expires_at)
        SELECT encode(sha256(convert_to(v_ticket || v_bound::text, 'UTF8')), 'hex'),
               v_session, p_kind, audit.id, clock_timestamp() + interval '30 seconds'
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

REVOKE ALL ON tenants FROM tappa_opdefiner;
REVOKE ALL ON locations FROM tappa_opdefiner;
REVOKE ALL ON employees FROM tappa_opdefiner;
REVOKE ALL ON tags FROM tappa_opdefiner;
REVOKE ALL ON admin_users FROM tappa_opdefiner;

ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_read_tickets
                WHERE kind IN ('tenants', 'tenant_detail')) THEN
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions'));
    END IF;
END
$$;
-- +goose StatementEnd
