-- 00032 -- M10 OP-12, phase A (the data layer): the operator reads ONE tenant's billing
-- timeline -- twelve of the tenant's own local months a page, five pages back -- through one
-- op_read_* and nothing else: what each month costs, whether that figure is FROZEN or a LIVE
-- preview, and whether a month has ended without the business closing it.
--
-- NORMATIVE SOURCES: docs/adr/0021-op-fonksiyonlari-tenant-otesi-erisim.md §1 (the definer
-- holds column grants, tappa_operator holds nothing on a tenant table), §2 (the op_* contract:
-- the session predicate, two-phase reads, the wall clock, the fixed column list), §3 (inside an
-- op_* the explicit tenant filter is the ONLY barrier) and §6 (the catalogue and behaviour
-- pins); ADR 0020 §9 (a tenant-crossing screen names its tenant); migration 00016 and
-- db/queries/billing.sql (the arithmetic this read must agree with). Where this file decides
-- something those leave open, the decision is written next to the statement and in ADR 0021's
-- "OP-12 uygulama notu".
--
-- 🔴 THE THIRD COPY OF THE ARITHMETIC. db/queries/billing.sql says "THE ARITHMETIC IS NOT
-- WRITTEN TWICE": PreviewBillingPeriod and CloseBillingPeriod are the same three CTEs (scope,
-- bounds, counted) over 00016's five functions. The read below is a THIRD copy of that glue --
-- the five functions stay ONE definition, called here exactly as they are called there. The
-- alternative, one shared RETURNS TABLE function both paths call, is the call sqlc v1.28
-- cannot type (ADR 0002 md.7) and would rewrite the tenant's own statements, which is not this
-- task. What keeps the copies from drifting is a TEST, not this comment:
-- TestOpReadTenantBilling_EveryMonthIsTheTenantsOwnFigure asks the tenant's own path
-- (GetBillingPeriod, and PreviewBillingPeriod for a month with no frozen row) about EVERY month
-- this read returns and compares them field by field. It turns red on a change to either copy
-- that shows in ITS fixtures -- three zones, the roster and price edges it builds, signups on
-- both sides of a UTC month boundary, the months it reads when it runs. A change that shows
-- only at another instant -- the month the tenant is in, taken in another zone than the
-- tenant's -- is TestOpReadTenantBilling_TheNewestMonthIsTheZonesAtAnyInstant's, which runs
-- this read's own query at chosen instants either side of a UTC month boundary. Any other form
-- is code review's; the two tests make no claim of completeness.
--
-- 🔴 CLAUDE.md §4.5 CROSSED ON PURPOSE, IN THE ONE PLACE ADR 0021 ALLOWS. The read belongs to
-- tappa_opdefiner (NOLOGIN, BYPASSRLS), so NO ROW LEVEL SECURITY APPLIES INSIDE IT -- not
-- billing_periods' policy, not employees', not tenants'. What stands instead is one tenant
-- filter on every table reference: `t.id = p_tenant_id`, `b.tenant_id = p_tenant_id`,
-- `e.tenant_id = p_tenant_id` (TestOpReadTenantBilling_ReturnsOnlyTheNamedTenantsFigures drives
-- two tenants through it).
--
-- §4.6, THE MONEY FORM: a figure that cannot be computed is not a zero. A month before the
-- business signed up carries NULL figures (after_signup = false), never 0.00; and the count of
-- employee rows whose status disagrees with their stamps (unstamped_employees) is returned
-- beside every count it shrank, as the tenant's own screen returns it.
-- §6: money is numeric and never float. RETURNS TABLE drops a type modifier (a declared
-- numeric(12,2) result column is plain numeric), so the SCALE of the live amount is set in the
-- expression -- `::numeric(12, 2)`, PreviewBillingPeriod's own cast -- and measured
-- (TestOpReadTenantBilling_MoneyIsNumericAtScaleTwo); no column of the result is a float
-- (TestOperator00032_TheFunctionsAndTheirExactSignatures reads the catalogue).
-- §4.3's family: billing_periods is append-only (00016). The read writes nothing to it, and the
-- definer holds SELECT on it and nothing else -- and not on closed_by: who closed a month is a
-- tenant's administrator, and the operator's question is whether and when, not who.
--
-- WHAT IT DOES, IN ORDER:
--   0. re-checks the two cluster roles' shape (00026's precondition, as 00027-00031 repeated
--      it), the server's major version, and that the database is at 00031 (its traces);
--   1. the read kinds: operator_read_tickets_kind_check gains 'tenant_billing' -- NOT VALID
--      only when a ticket of a kind outside the new set already exists;
--   2. the grants: tappa_opdefiner gains column SELECTs on tenants, employees and
--      billing_periods that the read names and no earlier file granted, and EXECUTE on 00016's
--      five billing functions -- nothing else;
--   3. op_begin_read is REPLACED: one more read kind, 'tenant_billing', with its own
--      parameter object and page bound; its five other branches are 00031's;
--   4. op_read_audit is REPLACED: 'tenant_billing' is a read scope its closed list knows (its
--      detail is {}), so the operator's own log shows these reads as what they are;
--   5. one function: op_read_tenant_billing.
--
-- No table is created here, so there is no redline waiver, no new RLS and no sequence to
-- REVOKE. tappa_app gains nothing (ADR 0021 §1); tappa_operator gains EXECUTE on the one new
-- function and no privilege on any table.
--
-- CLOCKS (ADR 0021 §2 vii): every instant the read compares or computes -- the ticket's expiry,
-- its consumption, the month the tenant is in, whether a month has ended -- comes from
-- clock_timestamp(). db/queries/billing.sql's period_has_ended reads now(), which an op_* may
-- not (TestOperator00026_NoFrozenClock walks every function tappa_opdefiner owns). That scan
-- does not see INTO the five helpers; their sources read no clock at all (they take instants
-- as arguments) -- read, not pinned: ADR 0021's OP-12 note counts it.
--
-- SECRETS (ADR 0021 §3.5, CLAUDE.md §7): the RAISE EXCEPTIONs carry constant messages; the
-- formatted ones are the precondition's (role NAMES, a server version number). The one RAISE
-- LOG is op_begin_read's (a constraint name and a SQLSTATE -- 00027's). No employee row, name
-- or stamp leaves this file: the read returns COUNTS, the way billing_periods stores them.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION: the roles, the server, and 00031.
-- ---------------------------------------------------------------------------
-- The role checks are 00026's, in 00026's order, as 00027, 00029, 00030 and 00031 repeated
-- them: an op_* owned by a SUPERUSER, a LOGIN or a member-carrying tappa_opdefiner is a
-- general bypass with every other check green, and a member of tappa_operator inherits EXECUTE
-- on the function below -- which reads ANY tenant's invoices.
-- THE SERVER: PostgreSQL 17 or later. The facts this read rests on were measured on 17 (the
-- development database, 17.10; CI and production run postgres:17): a RETURNS TABLE result
-- dropping numeric's type modifier, jsonb's canonical key order that the ticket hash binds,
-- and the five helpers not being inlined. A floor, not an equality: a later major passes
-- and is re-measured when it arrives. (This branch cannot be driven on a 17 server; the
-- precondition test pins its text -- a counted limit of the OP-12 note.)
-- 00031: the two functions this file replaces exist with 00031's identities and belong to
-- tappa_opdefiner, the five functions it grants exist with 00016's identities, and the ticket
-- kind CHECK names 00031's 'operator_audit' and not yet this file's kind. A database that
-- fails this was not migrated by goose in order, and replacing functions on it would be a
-- guess.
-- +goose StatementBegin
DO $$
DECLARE
    v_missing text;
    v_check   text;
BEGIN
    SELECT string_agg(r.name, ', ' ORDER BY r.name)
      INTO v_missing
      FROM unnest(ARRAY['tappa_operator', 'tappa_opdefiner']) AS r(name)
     WHERE NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles p WHERE p.rolname = r.name);
    IF v_missing IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00032 (M10 OP-12) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00032 (M10 OP-12): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00032 (M10 OP-12): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00032 (M10 OP-12): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00032 (M10 OP-12): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00032 (M10 OP-12): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00032 (M10 OP-12): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1: tappa_app gets no new privilege). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF pg_catalog.current_setting('server_version_num')::integer < 170000 THEN
        RAISE EXCEPTION 'migration 00032 (M10 OP-12) was measured on PostgreSQL 17 and refuses an older server (server_version_num %). Nothing was changed.', pg_catalog.current_setting('server_version_num')
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
         WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_audit(text, text, text, integer, integer)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_begin_read(text, text, jsonb)'))
           IS DISTINCT FROM 'tappa_opdefiner' THEN
        RAISE EXCEPTION 'migration 00032 (M10 OP-12) replaces op_begin_read and op_read_audit as migration 00031 left them, and 00031''s functions are not there (or do not belong to tappa_opdefiner). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF pg_catalog.to_regprocedure('public.tappa_employee_lifecycle_status(timestamp with time zone, timestamp with time zone)') IS NULL
       OR pg_catalog.to_regprocedure('public.tappa_employee_is_billable(text, timestamp with time zone, timestamp with time zone, timestamp with time zone, timestamp with time zone)') IS NULL
       OR pg_catalog.to_regprocedure('public.tappa_local_month_start(date, text)') IS NULL
       OR pg_catalog.to_regprocedure('public.tappa_first_chargeable_month(text, timestamp with time zone, text)') IS NULL
       OR pg_catalog.to_regprocedure('public.tappa_billing_amount_due(boolean, numeric, integer)') IS NULL THEN
        RAISE EXCEPTION 'migration 00032 (M10 OP-12) reads with the five billing functions of migration 00016, and one of them is not there under its name. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    SELECT pg_catalog.pg_get_constraintdef(c.oid)
      INTO v_check
      FROM pg_catalog.pg_constraint c
     WHERE c.conrelid = 'public.operator_read_tickets'::regclass
       AND c.conname = 'operator_read_tickets_kind_check';
    IF v_check IS NULL OR pg_catalog.strpos(v_check, '''operator_audit''') = 0
       OR pg_catalog.strpos(v_check, '''tenant_billing''') > 0 THEN
        RAISE EXCEPTION 'migration 00032 (M10 OP-12) widens operator_read_tickets_kind_check as migration 00031 left it, and the constraint is not in that state. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The read kinds: 'tenant_billing'.
-- ---------------------------------------------------------------------------
--   'tenant_billing'  op_read_tenant_billing: one tenant's billing months, a page of twelve.
-- No audit KIND is added: the read is one 'read' row whose target_scope is the read kind
-- (00027 section 1), whose target_tenant_id names the tenant and whose page_number and
-- page_size are the page (size twelve, the months of a page) -- written before anything is
-- shown.
-- NOT VALID, AND WHEN (00031's rule, for tickets): a ticket of a kind outside the six can only
-- be a later migration's, left behind by its Down -- consumed or not: production's tickets are
-- consumed, and a consumed ticket is a row the CHECK refuses all the same, so the condition is
-- the kind alone. With such a ticket present a VALIDATED re-add fails with 23514, so the CHECK
-- is then added NOT VALID -- binding every new row, not re-checking the old one -- and
-- VALIDATED otherwise. The constraint keeps its NAME, so the next migration finds it here.
ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_read_tickets
                WHERE kind <> ALL (ARRAY['legal_versions', 'tenants', 'tenant_detail',
                                         'tenant_plaques', 'operator_audit', 'tenant_billing'])) THEN
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit', 'tenant_billing'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit', 'tenant_billing'));
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 2. Grants: what the read touches and no earlier file granted.
-- ---------------------------------------------------------------------------
-- tappa_opdefiner, column by column (ADR 0021 §1; the WHOLE resulting lists are pinned in
-- TestOperator00026_PrivilegeMatrix's allow-list):
--   tenants          timezone and price_per_employee_month. With 00029's id, name, plan and
--                    created_at, the read's whole view of the tenant: the zone a month is
--                    resolved in, the price a live month is priced at, the plan and the
--                    signup instant the founding window is derived from.
--   employees        activated_at and deactivated_at. With 00029's tenant_id and status, the
--                    four arguments of the billable predicate -- and nothing else of a person:
--                    no name, no address, no id. The read returns COUNTS; ADR 0021 limit 9's
--                    rule (the definer reads personal data it does not return) names these two.
--   billing_periods  the frozen month's columns the read returns, and tenant_id for its
--                    filter -- NOT id, NOT created_at, and NOT closed_by (the tenant's
--                    administrator who closed it: the operator learns that a month was closed,
--                    and when, not by whom). SELECT only: the table is append-only (00016) and
--                    the definer may neither write it nor close a month.
-- EXECUTE on 00016's five functions, which REVOKEd PUBLIC's EXECUTE (00016 section 2b) and
-- granted tappa_app's alone. The read calls four of them directly; the fifth,
-- tappa_employee_lifecycle_status, it calls too (the unstamped count) -- AND
-- tappa_employee_is_billable calls it inside its body, which runs as its caller (SECURITY
-- INVOKER), so without EXECUTE on it the billable predicate itself fails with 42501 (measured,
-- TestOperator00032_TheDefinerExecutesExactlyTheFiveBillingHelpers). None of the five is a
-- SECURITY DEFINER and none reads a relation: they are arithmetic over their arguments.
-- 🔴 00029's columns are NOT granted again, and this file's Down does NOT revoke them (and does
-- not write REVOKE ALL): a column holds ONE ACL entry per grantee, so taking back a column
-- 00029 granted would take 00029's grant with it and break the tenant overview's counts
-- (00030's lesson). tappa_operator gets nothing on any of the three tables, and tappa_app's
-- grants -- and its EXECUTE on the five -- are not touched.
GRANT SELECT (timezone, price_per_employee_month) ON tenants TO tappa_opdefiner;
GRANT SELECT (activated_at, deactivated_at) ON employees TO tappa_opdefiner;
GRANT SELECT (tenant_id, period_month, period_from, period_to, timezone, plan, free_period,
              employee_count, unstamped_employees, unit_price, currency, amount_due, closed_at)
    ON billing_periods TO tappa_opdefiner;
GRANT EXECUTE ON FUNCTION public.tappa_employee_lifecycle_status(timestamptz, timestamptz) TO tappa_opdefiner;
GRANT EXECUTE ON FUNCTION public.tappa_employee_is_billable(text, timestamptz, timestamptz, timestamptz, timestamptz) TO tappa_opdefiner;
GRANT EXECUTE ON FUNCTION public.tappa_local_month_start(date, text) TO tappa_opdefiner;
GRANT EXECUTE ON FUNCTION public.tappa_first_chargeable_month(text, timestamptz, text) TO tappa_opdefiner;
GRANT EXECUTE ON FUNCTION public.tappa_billing_amount_due(boolean, numeric, integer) TO tappa_opdefiner;

-- ===========================================================================
-- 3. op_begin_read, REPLACED: one more read kind
-- ===========================================================================
-- Everything 00027 section 4.1, 00029 section 3, 00030 section 3 and 00031 section 5 say of
-- this function still holds: the session through op_touch_session, the read's 'read' row and
-- its ticket written by ONE statement and tied by audit_id, the raw ticket drawn from three
-- gen_random_uuid() (TestOpBeginRead_TheTicketIsDrawnFromTheStrongRandomSource), 30 seconds of
-- life, the hashed text rebuilt from TYPED values (v_bound), the caught constraint, and the
-- five kinds 00031 named, branch for branch. What changes:
--   * the closed set names 'tenant_billing';
--   * its parameter object is EXACTLY {tenant_id, page_number}:
--       tenant_id    a uuid in its hyphenated form (either case); v_bound holds the uuid
--                    VALUE, so the canonical lower-case text is what is hashed;
--       page_number  1..5 -- THE PAGE BOUND (orchestrator decision K12-3): five pages of
--                    twelve months are sixty, the tenant's own history depth
--                    (internal/domain/billing HistoryCap). A rule with no content: it reads
--                    nothing, so a refused page is refused before a row is written -- and an
--                    unbounded page would reach date arithmetic that overflows only AFTER the
--                    read's 'read' row had committed (the OP-12 card's T7);
--   * its audit row: target_scope 'tenant_billing', target_tenant_id the tenant, the page,
--     page_size 12 (the months a page holds) and detail {} -- the page is the only parameter
--     besides the tenant, and it has its column.
-- 🔴 op_begin_read STILL DOES NOT LOOK THE TENANT UP (00029 section 3, the B14 argument for a
-- read): an unknown tenant is answered by phase two -- zero rows -- after the 'read' row
-- naming it has committed.
-- The refusals keep 00027's two messages and codes.
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
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'operator_audit', 'tenant_billing') THEN
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
                                                   'logout', 'read', 'legal_publish') THEN
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
-- 4. op_read_audit, REPLACED: 'tenant_billing' is a scope the closed list knows
-- ===========================================================================
-- Everything 00031 section 6 says of this function still holds, and its body is 00031's but
-- for two lists: the scope it returns (a 'read' row's target_scope, only when the list names
-- it) and the read kinds whose detail is the empty object. 'tenant_billing' joins both -- its
-- 'read' row carries detail {} (the page has its own column), so it is recognised with nothing
-- to read out. Without this, every billing read would come back with no scope and its detail
-- "not shown": fail-closed, but a lie about a shipped shape -- and ADR 0021's OP-14 note
-- (md. 5) made it a test's business: every kind the ticket CHECK names must be a scope this
-- list knows (TestOpReadAudit_TheDetailIsShownOnlyInAShapeOnTheList, "every read kind").
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
                                                            'logout', 'read', 'legal_publish')
                       WHEN p.kind = 'read' THEN
                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing')
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
                                            'tenant_plaques', 'operator_audit', 'tenant_billing')
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
-- 5. op_read_tenant_billing -- one tenant's billing months
-- ===========================================================================
-- The op_read_* shape 00027 gave the first read and TestOpRead_EveryReadConsumesItsTicketAsTheADRSays
-- pins for EVERY op_read_* by name: the session through op_touch_session first; ONE consuming
-- UPDATE of the ticket whose WHERE holds the six conditions POSITIVELY -- the hash of (raw
-- ticket, {tenant_id, page_number} rebuilt from the typed arguments), this session, this kind,
-- not yet consumed, not expired by the WALL clock, created by a COMMITTED transaction --; the
-- refusal at once (28000); the rows only after it. ADR 0021 limit 4 holds unchanged.
--
-- ONE READ, THREE ANSWERS (00030's shape): the tenant's NAME on every row, for the header of
-- the screen (ADR 0020 §9); WHETHER THE TENANT EXISTS -- an unknown id reads ZERO rows, after
-- the 'read' row naming it has committed; and EXACTLY TWELVE months for a tenant that does.
--
-- THE PAGE: twelve of the tenant's own LOCAL months, newest first. Page 1 is the month the
-- tenant is in -- `date_trunc('month', clock_timestamp() AT TIME ZONE <its zone>)`, the wall
-- clock read in the zone the tenant's months are resolved in -- and the eleven before it; page
-- k starts 12 x (k - 1) months earlier. The body bounds the page itself: op_begin_read refuses
-- any page but 1..5, and the body does not trust that (a ticket the owner forges is the one
-- way past phase one -- 00031's least(..., 1000) did the same), so a page below 1 or none reads
-- page 1 and a page above 5 reads page 5 -- never a date arithmetic that overflows.
--
-- EACH MONTH IS ONE OF THREE THINGS, and the row says which:
--   FROZEN  billing_periods holds the month (`b.tenant_id = p_tenant_id`): every figure is
--           READ from that row and none is recomputed -- GetBillingPeriod's rule, the M6-12
--           card's fourth criterion: a roster or a price that changed after the close cannot
--           move it. first_chargeable_month is NULL (a frozen row keeps the decision, not the
--           rule); closed_at is the row's; after_signup and period_has_ended are true -- the
--           close refused to run otherwise (internal/domain/billing's frozenDraft says the same).
--   LIVE    no frozen row and the month is on or after the signup month in the tenant's
--           zone: PreviewBillingPeriod's arithmetic, below. currency is NULL -- a live month
--           has no frozen currency, and the reader takes the package default
--           (internal/domain/billing DefaultCurrency, held equal to the column default by its
--           own test). period_has_ended is `to_at <= clock_timestamp()`.
--   BEFORE  no frozen row and the month is before the signup month: after_signup is false and
--           EVERY figure is NULL -- not a zero invoice (§4.6's money form).
-- A frozen row wins whatever the signup month reads today (a zone changed after the close can
-- move the signup month; it cannot move a frozen month -- the tenant's Book.Period answers the
-- same).
--
-- THE LIVE ARITHMETIC, PreviewBillingPeriod's term for term -- the five functions, the same
-- arguments, the same comparisons:
--   from_at / to_at      tappa_local_month_start(month, zone) and (month + 1 month, zone);
--   first_chargeable     tappa_first_chargeable_month(plan, created_at, zone);
--   free_period          month < first_chargeable;
--   employee_count       the tenant's rows for which tappa_employee_is_billable(status,
--                        activated_at, deactivated_at, from_at, to_at) holds;
--   unstamped_employees  the tenant's rows whose tappa_employee_lifecycle_status(activated_at,
--                        deactivated_at) <> status -- tenant-wide and point-in-time, so the
--                        same for every live month of the page;
--   unit_price           tenants.price_per_employee_month, as it is;
--   amount_due           tappa_billing_amount_due(free_period, unit_price, employee_count),
--                        cast to numeric(12, 2) as the preview casts it (§6 above).
-- What differs is the SHAPE, not the arithmetic: the roster is read ONCE and grouped by month
-- (the preview reads it once per month it is asked), the month bounds are computed once per
-- month (MATERIALIZED: left to the planner they were inlined into the per-employee filter --
-- measured), and the unstamped count once per page.
--
-- 🔴 THE BELT, ONE FILTER PER TABLE REFERENCE (ADR 0021 §3.2):
--   `t.id = p_tenant_id`         the one tenant row;
--   `b.tenant_id = p_tenant_id`  the frozen months -- without it another tenant's frozen row
--                                for the same month joins the page;
--   `e.tenant_id = p_tenant_id`  the roster -- without it every tenant's employees are counted.
--
-- COST, AN OBSERVATION (measured, the OP-12 note): the read calls tappa_employee_is_billable
-- once per (employee, live month) and tappa_employee_lifecycle_status once per employee, and
-- neither is inlined -- a function with a SET clause never is -- so the time grows with the
-- roster times twelve. The roster is read through employees_tenant_idx, the frozen months
-- through billing_periods_tenant_month_key. No index is added.
-- +goose StatementBegin
CREATE FUNCTION public.op_read_tenant_billing(p_session text, p_ticket text, p_tenant_id uuid,
                                              p_page_number integer)
    RETURNS TABLE (tenant_id uuid, tenant_name text, period_month date, after_signup boolean,
                   frozen boolean, period_from timestamptz, period_to timestamptz,
                   period_timezone text, plan text, first_chargeable_month date,
                   free_period boolean, employee_count integer, unstamped_employees integer,
                   unit_price numeric, currency text, amount_due numeric,
                   closed_at timestamptz, period_has_ended boolean)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
    v_page    integer;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    UPDATE public.operator_read_tickets AS k
       SET consumed_at = clock_timestamp()
     WHERE k.ticket_hash = encode(sha256(convert_to(
               p_ticket || jsonb_build_object('tenant_id', p_tenant_id,
                                              'page_number', p_page_number)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'tenant_billing'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_tenant_billing: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    v_page := greatest(1, least(coalesce(p_page_number, 1), 5));

    RETURN QUERY
        WITH scope AS (
            SELECT t.id, t.name, t.plan AS tenant_plan, t.timezone AS zone,
                   t.price_per_employee_month AS price,
                   public.tappa_first_chargeable_month(t.plan, t.created_at, t.timezone) AS first_chargeable,
                   date_trunc('month', t.created_at AT TIME ZONE t.timezone)::date AS signup_month,
                   date_trunc('month', clock_timestamp() AT TIME ZONE t.timezone)::date AS this_month
              FROM public.tenants AS t
             WHERE t.id = p_tenant_id
        ), months AS (
            SELECT (s.this_month - make_interval(months => (v_page - 1) * 12 + g.i))::date AS month
              FROM scope AS s, generate_series(0, 11) AS g(i)
        ), closed AS (
            SELECT b.period_month AS month, b.period_from AS from_at, b.period_to AS to_at,
                   b.timezone AS zone, b.plan AS closed_plan, b.free_period AS free,
                   b.employee_count AS billable, b.unstamped_employees AS unstamped,
                   b.unit_price AS price, b.currency AS money, b.amount_due AS amount,
                   b.closed_at AS when_closed
              FROM public.billing_periods AS b
             WHERE b.tenant_id = p_tenant_id
               AND b.period_month IN (SELECT m.month FROM months AS m)
        ), live AS MATERIALIZED (
            SELECT m.month,
                   public.tappa_local_month_start(m.month, s.zone) AS from_at,
                   public.tappa_local_month_start((m.month + interval '1 month')::date, s.zone) AS to_at,
                   (m.month < s.first_chargeable) AS free
              FROM months AS m, scope AS s
             WHERE m.month >= s.signup_month
               AND NOT EXISTS (SELECT 1 FROM closed AS c WHERE c.month = m.month)
        ), roster AS MATERIALIZED (
            SELECT e.status, e.activated_at, e.deactivated_at
              FROM public.employees AS e
             WHERE e.tenant_id = p_tenant_id
        ), counted AS (
            SELECT l.month,
                   count(r.status) FILTER (WHERE public.tappa_employee_is_billable(
                       r.status, r.activated_at, r.deactivated_at, l.from_at, l.to_at))::integer AS billable
              FROM live AS l
              LEFT JOIN roster AS r ON true
             GROUP BY l.month
        ), disagreeing AS (
            SELECT (count(*) FILTER (WHERE public.tappa_employee_lifecycle_status(
                       r.activated_at, r.deactivated_at) <> r.status))::integer AS n
              FROM roster AS r
        )
        SELECT s.id, s.name, m.month,
               c.month IS NOT NULL OR l.month IS NOT NULL,
               c.month IS NOT NULL,
               coalesce(c.from_at, l.from_at),
               coalesce(c.to_at, l.to_at),
               CASE WHEN c.month IS NOT NULL THEN c.zone WHEN l.month IS NOT NULL THEN s.zone END,
               CASE WHEN c.month IS NOT NULL THEN c.closed_plan WHEN l.month IS NOT NULL THEN s.tenant_plan END,
               CASE WHEN l.month IS NOT NULL THEN s.first_chargeable END,
               coalesce(c.free, l.free),
               coalesce(c.billable, n.billable),
               CASE WHEN c.month IS NOT NULL THEN c.unstamped WHEN l.month IS NOT NULL THEN u.n END,
               CASE WHEN c.month IS NOT NULL THEN c.price WHEN l.month IS NOT NULL THEN s.price END,
               c.money,
               CASE WHEN c.month IS NOT NULL THEN c.amount
                    WHEN l.month IS NOT NULL
                    THEN public.tappa_billing_amount_due(l.free, s.price, n.billable)::numeric(12, 2) END,
               c.when_closed,
               CASE WHEN c.month IS NOT NULL THEN true WHEN l.month IS NOT NULL THEN l.to_at <= clock_timestamp() END
          FROM scope AS s
          CROSS JOIN months AS m
          LEFT JOIN closed AS c ON c.month = m.month
          LEFT JOIN live AS l ON l.month = m.month
          LEFT JOIN counted AS n ON n.month = m.month
          CROSS JOIN disagreeing AS u
         ORDER BY m.month DESC;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_tenant_billing(text, text, uuid, integer) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_tenant_billing(text, text, uuid, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_tenant_billing(text, text, uuid, integer) TO tappa_operator;

-- +goose Down

-- 🔴 WHAT A SUCCESSFUL Down DOES, named (00030's and 00031's house style):
--   * the read goes; op_begin_read and op_read_audit go back to 00031's Up bodies VERBATIM --
--     replaced, not dropped, so their owners and ACLs stay
--     (TestOperator00032_DownGivesBack00031AndUpTakesItAgain compares each body with 00031's
--     file);
--   * tappa_opdefiner loses EXACTLY what section 2 granted -- the column SELECTs, by column,
--     and the EXECUTE on the five functions -- NOT `REVOKE ALL` (which would take 00029's
--     tenants and employees columns with it and break the tenant overview's counts);
--     tappa_app keeps its EXECUTE on the five (00016's) and everything else it held;
--   * the 'read' rows the read committed STAY: operator_audit_log is append-only (00026: its
--     triggers bind the owner too) and they are the evidence;
--   * billing_periods is not touched (the read never wrote it);
--   * operator_read_tickets_kind_check goes back to 00031's closed set -- NOT VALID when ANY
--     ticket exists whose kind that set does not name (this file's kind or a later
--     migration's, consumed or not), VALIDATED when none does (00030's Down rule: the
--     condition is "outside the previous set", never "this file's kind", or the Downs do not
--     compose).
DROP FUNCTION IF EXISTS public.op_read_tenant_billing(text, text, uuid, integer);

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
                                                            'logout', 'read', 'legal_publish')
                       WHEN p.kind = 'read' THEN
                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques')
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
                                            'tenant_plaques', 'operator_audit')
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
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'operator_audit') THEN
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
                                                   'logout', 'read', 'legal_publish') THEN
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

REVOKE EXECUTE ON FUNCTION public.tappa_employee_lifecycle_status(timestamptz, timestamptz) FROM tappa_opdefiner;
REVOKE EXECUTE ON FUNCTION public.tappa_employee_is_billable(text, timestamptz, timestamptz, timestamptz, timestamptz) FROM tappa_opdefiner;
REVOKE EXECUTE ON FUNCTION public.tappa_local_month_start(date, text) FROM tappa_opdefiner;
REVOKE EXECUTE ON FUNCTION public.tappa_first_chargeable_month(text, timestamptz, text) FROM tappa_opdefiner;
REVOKE EXECUTE ON FUNCTION public.tappa_billing_amount_due(boolean, numeric, integer) FROM tappa_opdefiner;
REVOKE SELECT (tenant_id, period_month, period_from, period_to, timezone, plan, free_period,
               employee_count, unstamped_employees, unit_price, currency, amount_due, closed_at)
    ON billing_periods FROM tappa_opdefiner;
REVOKE SELECT (activated_at, deactivated_at) ON employees FROM tappa_opdefiner;
REVOKE SELECT (timezone, price_per_employee_month) ON tenants FROM tappa_opdefiner;

ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_read_tickets
                WHERE kind <> ALL (ARRAY['legal_versions', 'tenants', 'tenant_detail',
                                         'tenant_plaques', 'operator_audit'])) THEN
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit'));
    END IF;
END
$$;
-- +goose StatementEnd
