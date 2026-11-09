-- 00098 -- T27 (OP-K12), the OPERATOR half of 00097: the operator's billing read prices a live
-- month under the plan IN FORCE for it, and a recorded plan change names its operator by key.
--
-- NORMATIVE SOURCES: docs/adr/0043-plan-gecmisi.md; ADR 0021 §1 (the definer holds column grants,
-- tappa_operator nothing on a tenant table), §2 (the op_* contract), §3 (inside an op_* the
-- explicit tenant filter is the ONLY barrier); 00032 (the read this file replaces).
--
-- WHAT IT DOES, IN ORDER (one transaction -- goose's):
--   0. lock_timeout; the precondition (00097 in place, op_read_tenant_billing still 00032's body,
--      the definer not yet reaching what this file grants);
--   1. tenant_plan_history.changed_by REFERENCES platform_admins (id) -- an attribution that names
--      no operator is refused (23503), in the statement that changes the plan;
--   2. tappa_opdefiner: SELECT on the three columns 00097 gave tappa_app, and EXECUTE on
--      tappa_plan_in_force -- nothing to write (OP-17 grants its writer what it needs, with it);
--   3. op_read_tenant_billing REPLACED;
--   4. the postcondition.
--
-- 🔴 THE READ, 00032's BODY WITH ONE CHANGE, in its live months: the plan a month is priced under
-- is tappa_plan_in_force(p_tenant_id, the month's from_at, tenants.plan), and first_chargeable and
-- free_period follow from THAT plan -- PreviewBillingPeriod's arithmetic after 00097 term for term,
-- which TestOpReadTenantBilling_EveryMonthIsTheTenantsOwnFigure holds the two copies to. The ticket,
-- the page bound, the frozen months, the roster and the belt (`t.id`, `b.tenant_id`, `e.tenant_id`
-- = p_tenant_id, and the reader's own `h.tenant_id = p_tenant_id` -- the only barrier there too,
-- the definer bypassing RLS) are 00032's, unchanged. CREATE OR REPLACE keeps the owner, the ACL,
-- SECURITY DEFINER and the search_path; the definition restates the last two and the statements
-- after it restate the first two. The read's audit row is op_begin_read's (phase one), which this
-- file does not touch.
--
-- LOCKS (measured, PostgreSQL 17, pg_locks inside the transaction). The foreign key takes SHARE ROW
-- EXCLUSIVE on tenant_plan_history and platform_admins until the commit -- an operator login's
-- UPDATE of platform_admins waits that long, under lock_timeout -- and validates the rows 00097
-- wrote (changed_by all NULL: nothing to look up). Nothing a tap waits on.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.tenant_plan_history') IS NULL
       OR pg_catalog.to_regprocedure('public.tappa_plan_in_force(uuid, timestamptz, text)') IS NULL THEN
        RAISE EXCEPTION 'migration 00098 (T27) needs migration 00097 (the plan history). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_constraint c
                WHERE c.conname = 'tenant_plan_history_changed_by_fkey') THEN
        RAISE EXCEPTION 'migration 00098 (T27) adds tenant_plan_history_changed_by_fkey, and it is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- The body this file replaces must be 00032's, byte for byte (00049's md5 precedent): a read
    -- changed by another hand would be overwritten silently.
    IF (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = 'public.op_read_tenant_billing(text, text, uuid, integer)'::pg_catalog.regprocedure)
       IS DISTINCT FROM '77ee6860a67963c86ac976cb3d94e8a0' THEN
        RAISE EXCEPTION 'migration 00098 (T27) replaces op_read_tenant_billing, and its body is not 00032''s. Another change reached it first: rebase this file on it. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- A column grant cannot narrow a wider privilege some other hand gave (00024 measured it).
    IF pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.tenant_plan_history', 'SELECT')
       OR pg_catalog.has_function_privilege('tappa_opdefiner', 'public.tappa_plan_in_force(uuid, timestamptz, text)', 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00098 (T27): tappa_opdefiner already reaches tenant_plan_history or its reader, and a column grant cannot narrow that. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. WHO -- the key
-- ---------------------------------------------------------------------------
ALTER TABLE tenant_plan_history
    ADD CONSTRAINT tenant_plan_history_changed_by_fkey
    FOREIGN KEY (changed_by) REFERENCES platform_admins (id) ON DELETE RESTRICT;

-- ---------------------------------------------------------------------------
-- 2. THE DEFINER'S GRANTS (ADR 0021 §1, column by column)
-- ---------------------------------------------------------------------------
-- The three columns tappa_plan_in_force reads, which runs as its caller (SECURITY INVOKER): the
-- read calls it as tappa_opdefiner, so without them the read fails with 42501. Not changed_by, id
-- or recorded_at: the read returns a month's plan, not its history. tappa_operator gets nothing.
GRANT SELECT (tenant_id, plan, effective_from) ON tenant_plan_history TO tappa_opdefiner;
GRANT EXECUTE ON FUNCTION public.tappa_plan_in_force(uuid, timestamptz, text) TO tappa_opdefiner;

-- ---------------------------------------------------------------------------
-- 3. op_read_tenant_billing -- REPLACED
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_read_tenant_billing(p_session text, p_ticket text, p_tenant_id uuid,
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
                   t.price_per_employee_month AS price, t.created_at AS signed_up_at,
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
        ), bounded AS (
            SELECT m.month,
                   public.tappa_local_month_start(m.month, s.zone) AS from_at,
                   public.tappa_local_month_start((m.month + interval '1 month')::date, s.zone) AS to_at
              FROM months AS m, scope AS s
             WHERE m.month >= s.signup_month
               AND NOT EXISTS (SELECT 1 FROM closed AS c WHERE c.month = m.month)
        ), governed AS (
            SELECT d.month, d.from_at, d.to_at,
                   public.tappa_plan_in_force(p_tenant_id, d.from_at, s.tenant_plan) AS plan
              FROM bounded AS d, scope AS s
        ), live AS MATERIALIZED (
            SELECT g.month, g.from_at, g.to_at, g.plan,
                   public.tappa_first_chargeable_month(g.plan, s.signed_up_at, s.zone) AS first_chargeable
              FROM governed AS g, scope AS s
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
               CASE WHEN c.month IS NOT NULL THEN c.closed_plan WHEN l.month IS NOT NULL THEN l.plan END,
               CASE WHEN l.month IS NOT NULL THEN l.first_chargeable END,
               coalesce(c.free, l.month < l.first_chargeable),
               coalesce(c.billable, n.billable),
               CASE WHEN c.month IS NOT NULL THEN c.unstamped WHEN l.month IS NOT NULL THEN u.n END,
               CASE WHEN c.month IS NOT NULL THEN c.price WHEN l.month IS NOT NULL THEN s.price END,
               c.money,
               CASE WHEN c.month IS NOT NULL THEN c.amount
                    WHEN l.month IS NOT NULL
                    THEN public.tappa_billing_amount_due(l.month < l.first_chargeable, s.price, n.billable)::numeric(12, 2) END,
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

-- ---------------------------------------------------------------------------
-- 4. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    c text;
BEGIN
    FOREACH c IN ARRAY ARRAY['tenant_id', 'plan', 'effective_from'] LOOP
        IF NOT pg_catalog.has_column_privilege('tappa_opdefiner', 'public.tenant_plan_history', c, 'SELECT') THEN
            RAISE EXCEPTION 'migration 00098 (T27): tappa_opdefiner cannot read tenant_plan_history.%. Nothing was changed.', c
                USING ERRCODE = 'object_not_in_prerequisite_state';
        END IF;
    END LOOP;
    FOREACH c IN ARRAY ARRAY['id', 'changed_by', 'recorded_at'] LOOP
        IF pg_catalog.has_column_privilege('tappa_opdefiner', 'public.tenant_plan_history', c, 'SELECT') THEN
            RAISE EXCEPTION 'migration 00098 (T27): tappa_opdefiner reads tenant_plan_history.%. Nothing was changed.', c
                USING ERRCODE = 'object_not_in_prerequisite_state';
        END IF;
    END LOOP;
    IF pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.tenant_plan_history', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.tenant_plan_history', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.tenant_plan_history', 'DELETE, TRUNCATE')
       OR pg_catalog.has_any_column_privilege('tappa_operator', 'public.tenant_plan_history', 'SELECT, INSERT, UPDATE, REFERENCES')
       OR pg_catalog.has_function_privilege('tappa_operator', 'public.tappa_plan_in_force(uuid, timestamptz, text)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_opdefiner', 'public.tappa_plan_in_force(uuid, timestamptz, text)', 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00098 (T27): the operator roles'' reach on tenant_plan_history is not this file''s. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                    WHERE p.oid = 'public.op_read_tenant_billing(text, text, uuid, integer)'::pg_catalog.regprocedure
                      AND p.prosecdef AND p.proowner = 'tappa_opdefiner'::pg_catalog.regrole
                      AND p.proconfig = ARRAY['search_path=pg_catalog, pg_temp']
                      AND NOT pg_catalog.has_function_privilege('tappa_app', p.oid, 'EXECUTE')
                      AND NOT pg_catalog.has_function_privilege('public', p.oid, 'EXECUTE')
                      AND pg_catalog.has_function_privilege('tappa_operator', p.oid, 'EXECUTE')) THEN
        RAISE EXCEPTION 'migration 00098 (T27): op_read_tenant_billing is not the definer''s, SECURITY DEFINER with its search_path and executable by tappa_operator alone. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- 🔴 WHAT A SUCCESSFUL Down DOES, named: op_read_tenant_billing goes back to 00032's body VERBATIM
-- (replaced, so owner and ACL stay; restated anyway); tappa_opdefiner loses EXACTLY what section 2
-- granted (the three columns and the EXECUTE -- no other file granted it either); the key goes
-- (changed_by keeps its values: 00097's rows are not touched -- that table is append-only and 00097's
-- Down decides about it). Nothing refuses: no row is erased.
-- DROP CONSTRAINT takes ACCESS EXCLUSIVE on tenant_plan_history and on platform_admins (measured: the
-- key's triggers live on both), both locked first, under lock_timeout.
SET LOCAL lock_timeout = '1s';

LOCK TABLE public.tenant_plan_history, public.platform_admins IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_read_tenant_billing(p_session text, p_ticket text, p_tenant_id uuid,
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

REVOKE EXECUTE ON FUNCTION public.tappa_plan_in_force(uuid, timestamptz, text) FROM tappa_opdefiner;
REVOKE SELECT (tenant_id, plan, effective_from) ON tenant_plan_history FROM tappa_opdefiner;
ALTER TABLE tenant_plan_history DROP CONSTRAINT tenant_plan_history_changed_by_fkey;
