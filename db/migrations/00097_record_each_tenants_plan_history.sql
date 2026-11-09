-- 00097 -- T27 (OP-K12, user decision 2026-11-08), the TENANT half: a tenant's PLAN HISTORY, and
-- the one reader the billing month asks for the plan IN FORCE instead of tenants.plan as it is
-- today. 00098 is the OPERATOR half (op_read_tenant_billing, the definer's grants, the changed_by
-- key to platform_admins); the split keeps this file free of operator objects, so the operator
-- tests' walk to an older version skips its Down (internal/db opAtVersionSkips), which refuses
-- once a change is recorded.
--
-- NORMATIVE SOURCE: docs/adr/0043-plan-gecmisi.md (the rule, the alternatives weighed, what is left
-- to OP-17). CLAUDE.md §4.3's family (append-only), §4.5, §6.
--
-- THE HARM IT CLOSES (backlog T27, re-read 2026-11-09). PreviewBillingPeriod and CloseBillingPeriod
-- (db/queries/billing.sql) and 00032's op_read_tenant_billing derived a month's free_period from
-- tappa_first_chargeable_month(tenants.plan, ...), i.e. from the plan as it is when the month is
-- READ. A month that had ended but was not yet closed followed a plan change made after it -- and
-- billing_periods is append-only (00016), so the close froze the moved answer.
--
-- THE RULE (ADR 0043 §2): month M is governed by the plan in force at M's FIRST INSTANT -- the
-- tenant's local midnight on the 1st resolved to UTC, the period_from billing_periods freezes. A
-- change made at instant X therefore governs the first month that STARTS at or after X: "from the
-- next billing period", the subscription-schedule pattern. The signup month starts before the
-- tenant existed, so the plan it started with governs it. A change can never reach a month that
-- has already begun, closed or not.
--
-- WHAT IT DOES, IN ORDER (one transaction -- goose's):
--   0. lock_timeout; tenants locked SHARE ROW EXCLUSIVE (CREATE TRIGGER's own mode, taken first so
--      no plan change slips between the backfill and the trigger); the precondition;
--   1. tenant_plan_history: the five (tenant_id NOT NULL, its index, ENABLE + FORCE RLS, USING +
--      WITH CHECK), append-only for every role (tappa_forbid_mutation, row + TRUNCATE);
--   2. the backfill: one row per tenant, its current plan, effective from its created_at;
--   3. tappa_record_plan_change() and its AFTER UPDATE trigger on tenants, WHEN the plan changed;
--   4. tappa_plan_in_force(): the one definition of "the plan in force at an instant";
--   5. tappa_app: SELECT on three columns and EXECUTE on the reader, nothing else;
--   6. the postcondition.
--
-- 🔴 NOTHING WRITES tenants.plan BUT THE OWNER TODAY (measured 2026-11-09 on a lane database: no
-- role but the superuser holds UPDATE on the column, no function body in public updates tenants
-- with `plan =`; read: no migration grants it). The operator's plan-writing
-- screen is OP-17, blocked on this change and not built here. So the history row is written by a
-- TRIGGER on tenants, in the same statement as the change, whoever makes it -- the owner by hand
-- today, OP-17's definer tomorrow. A plan change without its history row takes disabling the
-- trigger, which takes the owner's privilege.
--
-- WHO. changed_by is the platform operator (platform_admins.id -- 00098 adds the key) that made the
-- change, read from the transaction-local setting tappa.plan_changed_by: OP-17's function sets it
-- with set_config(..., true) before its UPDATE, beside its operator_audit_log row (ADR 0043 §4). It
-- is ATTRIBUTION, not authentication: only a role that may UPDATE tenants.plan can produce a row
-- at all. NULL = not through the operator surface (the backfill, a first row, a hand-written
-- UPDATE). tappa_app reads neither the column nor anything else of the operator.
--
-- LOCKS. tenants SHARE ROW EXCLUSIVE until the commit: a signup (INSERT) and a settings save wait;
-- a tap only reads tenants and does not. The work under it is one INSERT per tenant.

-- +goose Up
SET LOCAL lock_timeout = '1s';

LOCK TABLE public.tenants IN SHARE ROW EXCLUSIVE MODE;

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.tenant_plan_history') IS NOT NULL
       OR pg_catalog.to_regprocedure('public.tappa_record_plan_change()') IS NOT NULL
       OR pg_catalog.to_regprocedure('public.tappa_plan_in_force(uuid, timestamptz, text)') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_trigger g WHERE g.tgname = 'tenants_plan_history') THEN
        RAISE EXCEPTION 'migration 00097 (T27) creates tenant_plan_history, its trigger and two functions, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. tenant_plan_history
-- ---------------------------------------------------------------------------
-- One row per plan a tenant has had: the plan, and the instant from which it was the tenant's plan.
-- effective_from is NEVER a caller's: the backfill takes created_at, the trigger the wall clock
-- (clock_timestamp(), operator_audit_log's habit -- 00033). No product path can backdate a row
-- into a month that has already begun, which is the other half of T27.
-- UNIQUE (tenant_id, effective_from): "the latest row at or before X" must have one answer; the
-- unique index is also the reader's index (tenant_id first, scanned backwards) and §6's tenant_id
-- index.
CREATE TABLE tenant_plan_history (
    id             uuid        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id      uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    plan           text        NOT NULL,
    effective_from timestamptz NOT NULL,
    changed_by     uuid,
    recorded_at    timestamptz NOT NULL DEFAULT clock_timestamp(),
    -- tenants.plan's own closed set (00001)
    CONSTRAINT tenant_plan_history_plan_check CHECK (plan IN ('founding', 'standard')),
    CONSTRAINT tenant_plan_history_tenant_instant_key UNIQUE (tenant_id, effective_from)
);

ALTER TABLE tenant_plan_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE tenant_plan_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_plan_history_tenant_isolation ON tenant_plan_history
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TRIGGER tenant_plan_history_no_mutation
    BEFORE UPDATE OR DELETE ON tenant_plan_history
    FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation();
CREATE TRIGGER tenant_plan_history_no_truncate
    BEFORE TRUNCATE ON tenant_plan_history
    FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation();

-- ---------------------------------------------------------------------------
-- 2. THE BACKFILL -- one row per tenant (the migration role bypasses RLS)
-- ---------------------------------------------------------------------------
-- effective_from = created_at, not the start of the earliest billing month (ADR 0043 §3): the plan
-- was set when the row was (tenants.plan's DEFAULT at INSERT; tappa_app has never been able to
-- write it -- 00016), so created_at is the instant that is true, and a month start would date a
-- plan before the business existed. The reader needs no earlier instant: a month that starts
-- before a tenant's first row is governed by that first row (section 4).
--
-- ⚠️ IT IS THE BEST KNOWLEDGE, NOT PROOF, and the limit is written down rather than enforced. A
-- plan the owner changed BY HAND before this file left no trace but the plan a frozen month
-- recorded (billing_periods.plan); for such a tenant this row dates the current plan from signup.
-- That is never less true than the arithmetic it replaces -- which priced EVERY unclosed month
-- under the current plan -- and a frozen month is read from its own row, never from this one. A
-- refusal was weighed and dropped: development databases hold exactly that shape (billing fixtures
-- re-set a tenant's plan after closing its months -- measured on a lane database after one run of
-- the billing tests: 13 frozen months), and the question for production is a measurement (ADR 0043
-- §3 gives the query), not a guess this file could make.
INSERT INTO tenant_plan_history (tenant_id, plan, effective_from)
SELECT t.id, t.plan, t.created_at FROM tenants AS t;

-- ---------------------------------------------------------------------------
-- 3. THE WRITER -- AFTER UPDATE ON tenants, WHEN the plan changed
-- ---------------------------------------------------------------------------
-- SECURITY INVOKER, on purpose: the row is written with the privilege of whoever changed the plan.
-- The owner may; OP-17's definer is to be granted INSERT here with its UPDATE on plan; any role
-- that could write plan without INSERT here fails the whole UPDATE (42501) -- the plan cannot move
-- without its history. No role but the owner needs EXECUTE (PostgreSQL does not check EXECUTE when
-- a trigger fires), so PUBLIC's is revoked and none is granted.
--
-- A TENANT WITH NO ROW YET -- one created after this file: the signup path writes nothing here, so
-- tappa_app needs no write privilege on the table -- gets its FIRST row now, from OLD: the plan it
-- had since created_at. Then the change, from the wall clock. The UPDATE holds the tenant row's
-- lock, so two changes to one tenant are serialised and the EXISTS cannot race.
--
-- NOT `UPDATE OF plan` (security review, DÜŞÜK, 2026-11-09): a column-specific trigger fires only
-- when the statement's SET list names the column, so a BEFORE trigger that changed NEW.plan on an
-- UPDATE of another column would move the plan with no history row. An AFTER trigger's WHEN reads
-- the row as stored -- after every BEFORE trigger -- so this one fires whenever the stored plan
-- differs, whatever the statement named (TestTenantPlanHistory_ABeforeTriggerThatMovesThePlanStillLeavesItsRow).
-- The WHEN costs every other UPDATE of tenants one comparison and no function call.
-- +goose StatementBegin
CREATE FUNCTION public.tappa_record_plan_change()
    RETURNS trigger
    LANGUAGE plpgsql
    VOLATILE
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_by uuid;
BEGIN
    IF TG_WHEN <> 'AFTER' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'UPDATE'
       OR TG_RELID <> 'public.tenants'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'tappa_record_plan_change: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF NEW.plan IS NOT DISTINCT FROM OLD.plan THEN
        RETURN NULL;
    END IF;
    -- A value that is not a uuid fails the UPDATE (22P02): a wrong attribution is refused, not
    -- written as NULL. Since 00098 one that names no operator fails the foreign key (23503).
    v_by := NULLIF(pg_catalog.current_setting('tappa.plan_changed_by', true), '')::uuid;

    IF NOT EXISTS (SELECT 1 FROM public.tenant_plan_history AS h WHERE h.tenant_id = NEW.id) THEN
        INSERT INTO public.tenant_plan_history (tenant_id, plan, effective_from)
        VALUES (NEW.id, OLD.plan, OLD.created_at);
    END IF;
    INSERT INTO public.tenant_plan_history (tenant_id, plan, effective_from, changed_by)
    VALUES (NEW.id, NEW.plan, pg_catalog.clock_timestamp(), v_by);
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.tappa_record_plan_change() FROM PUBLIC;

CREATE TRIGGER tenants_plan_history
    AFTER UPDATE ON tenants
    FOR EACH ROW
    WHEN (OLD.plan IS DISTINCT FROM NEW.plan)
    EXECUTE FUNCTION public.tappa_record_plan_change();

-- ---------------------------------------------------------------------------
-- 4. THE READER -- the plan in force at an instant
-- ---------------------------------------------------------------------------
-- The ONE definition, called by PreviewBillingPeriod and CloseBillingPeriod (and, since 00098, the
-- operator's billing read) with the month's period_from -- billing.sql's "the arithmetic is not
-- written twice":
--   the latest row at or before p_at;
--   else the tenant's EARLIEST row -- a month that starts before the first row (the signup month)
--     is governed by the plan the tenant started with;
--   else p_current_plan (tenants.plan) -- a tenant with no row has never changed plan (section 3
--     writes the first row on the first change), so its one plan is its current one.
-- p_at is an INSTANT; a half-open month [period_from, period_to) is answered with its period_from
-- (ADR 0043 §2). The month is the tenant's LOCAL month resolved to UTC instants -- the bounds
-- billing_periods freezes -- not a UTC calendar month, which would disagree with the month it
-- governs by up to fourteen hours.
--
-- SECURITY INVOKER: RLS applies to tappa_app and the explicit tenant filter is the belt (§4.5). A
-- tappa_app caller outside a tenant context sees no row and would fall back to p_current_plan --
-- but both callers read p_current_plan from tenants in the same statement, and tenants yields no
-- row outside the context (its policy scopes on id), so there is no figure to fall back into.
-- STABLE: it reads a table. Not inlined (a SET clause prevents it): one call per month.
-- +goose StatementBegin
CREATE FUNCTION public.tappa_plan_in_force(p_tenant_id uuid, p_at timestamptz, p_current_plan text)
    RETURNS text
    LANGUAGE sql
    STABLE
    SET search_path = pg_catalog, pg_temp
AS $$
    SELECT coalesce(
        (SELECT h.plan FROM public.tenant_plan_history AS h
          WHERE h.tenant_id = p_tenant_id AND h.effective_from <= p_at
          ORDER BY h.effective_from DESC LIMIT 1),
        (SELECT h.plan FROM public.tenant_plan_history AS h
          WHERE h.tenant_id = p_tenant_id
          ORDER BY h.effective_from ASC LIMIT 1),
        p_current_plan);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.tappa_plan_in_force(uuid, timestamptz, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.tappa_plan_in_force(uuid, timestamptz, text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 5. PRIVILEGES -- ADR 0028 §1's `arwd` rule: production's default hands a new table all four verbs
-- (scripts/db-init/03-dev-production-default-acl.sql), so ALL is revoked first.
-- ---------------------------------------------------------------------------
-- tappa_app: the panel's preview and close call tappa_plan_in_force as tappa_app, so it reads the
-- three columns the reader names -- not changed_by (which operator made a change is the platform's
-- record, not the tenant's), not id, not recorded_at. No INSERT, UPDATE, DELETE or TRUNCATE: the
-- panel never writes a plan (00016 keeps plan out of tappa_app's tenants grants).
REVOKE ALL ON tenant_plan_history FROM PUBLIC;
REVOKE ALL ON tenant_plan_history FROM tappa_app;
GRANT SELECT (tenant_id, plan, effective_from) ON tenant_plan_history TO tappa_app;

-- ---------------------------------------------------------------------------
-- 6. POSTCONDITION -- measured as each role would meet it (has_* count membership and PUBLIC).
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    r text;
    c text;
BEGIN
    FOREACH c IN ARRAY ARRAY['tenant_id', 'plan', 'effective_from'] LOOP
        IF NOT pg_catalog.has_column_privilege('tappa_app', 'public.tenant_plan_history', c, 'SELECT') THEN
            RAISE EXCEPTION 'migration 00097 (T27): tappa_app cannot read tenant_plan_history.%. Nothing was changed.', c
                USING ERRCODE = 'object_not_in_prerequisite_state';
        END IF;
    END LOOP;
    FOREACH c IN ARRAY ARRAY['id', 'changed_by', 'recorded_at'] LOOP
        IF pg_catalog.has_column_privilege('tappa_app', 'public.tenant_plan_history', c, 'SELECT') THEN
            RAISE EXCEPTION 'migration 00097 (T27): tappa_app reads tenant_plan_history.% -- through PUBLIC or a role membership it does not. Fix the grants by hand, then migrate again. Nothing was changed.', c
                USING ERRCODE = 'object_not_in_prerequisite_state';
        END IF;
    END LOOP;
    IF pg_catalog.has_any_column_privilege('tappa_app', 'public.tenant_plan_history', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.tenant_plan_history', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.tenant_plan_history', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.tenant_plan_history', 'TRUNCATE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.tappa_plan_in_force(uuid, timestamptz, text)', 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00097 (T27): tappa_app may write tenant_plan_history, or cannot call its reader. Fix the grants by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- every other role but the owner and the superusers, read from the catalogue (00068's habit)
    FOR r IN SELECT o.rolname FROM pg_catalog.pg_roles AS o
              WHERE o.rolname <> 'tappa_app' AND o.rolname !~ '^pg_' AND NOT o.rolsuper
                AND o.oid <> (SELECT k.relowner FROM pg_catalog.pg_class AS k
                               WHERE k.oid = 'public.tenant_plan_history'::pg_catalog.regclass) LOOP
        IF pg_catalog.has_any_column_privilege(r, 'public.tenant_plan_history', 'SELECT, INSERT, UPDATE, REFERENCES')
           OR pg_catalog.has_table_privilege(r, 'public.tenant_plan_history', 'DELETE, TRUNCATE, TRIGGER')
           OR pg_catalog.has_function_privilege(r, 'public.tappa_plan_in_force(uuid, timestamptz, text)', 'EXECUTE')
           OR pg_catalog.has_function_privilege(r, 'public.tappa_record_plan_change()', 'EXECUTE') THEN
            RAISE EXCEPTION 'migration 00097 (T27): % reaches tenant_plan_history or one of its two functions, which no role but tappa_app (the table and its reader) may. Fix the grants by hand, then migrate again. Nothing was changed.', r
                USING ERRCODE = 'object_not_in_prerequisite_state';
        END IF;
    END LOOP;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc AS p
                WHERE p.oid IN ('public.tappa_record_plan_change()'::pg_catalog.regprocedure,
                                'public.tappa_plan_in_force(uuid, timestamptz, text)'::pg_catalog.regprocedure)
                  AND p.prosecdef)
       OR pg_catalog.has_function_privilege('public', 'public.tappa_record_plan_change()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.tappa_record_plan_change()', 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00097 (T27): a plan-history function is SECURITY DEFINER, or the trigger function is executable outside its owner. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- the backfill: exactly one row per tenant, its current plan, from its created_at
    IF EXISTS (SELECT 1 FROM public.tenants AS t
                WHERE (SELECT count(*) FROM public.tenant_plan_history AS h
                        WHERE h.tenant_id = t.id AND h.plan = t.plan AND h.effective_from = t.created_at) <> 1)
       OR (SELECT count(*) FROM public.tenant_plan_history) <> (SELECT count(*) FROM public.tenants) THEN
        RAISE EXCEPTION 'migration 00097 (T27): the backfill is not one row per tenant. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- 🔴 A DOWN THAT WOULD ERASE A RECORDED CHANGE REFUSES (CLAUDE.md §4.3's family; 00068's
-- precedent). A tenant with ONE row says nothing tenants.plan does not (its one plan, since signup
-- -- the backfill and a first row both say exactly that), so those may go. A tenant with TWO OR
-- MORE rows carries a change -- its instant and its author -- that no other row of the schema
-- holds; while one exists this Down stops and changes nothing. After the first change the way back
-- is the binary, not the schema. The binary that calls tappa_plan_in_force (db/queries/billing.sql)
-- must be rolled back FIRST, and 00098's Down must have run (goose's order does that).
--
-- The tables are LOCKED BEFORE THEY ARE LOOKED AT: a plan change writes the history under the
-- tenants row's lock, so between an unlocked check and the DROP a change could commit and the DROP
-- would erase it. DROP TRIGGER takes ACCESS EXCLUSIVE on tenants, under the same lock_timeout.
SET LOCAL lock_timeout = '1s';

LOCK TABLE public.tenants, public.tenant_plan_history IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.tenant_plan_history AS h
                GROUP BY h.tenant_id HAVING count(*) > 1) THEN
        RAISE EXCEPTION 'migration 00097 Down: tenant_plan_history records a plan change (a tenant with more than one row); dropping it would erase when the plan changed and who changed it, and it is append-only. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP TRIGGER tenants_plan_history ON tenants;
DROP FUNCTION public.tappa_record_plan_change();
DROP FUNCTION public.tappa_plan_in_force(uuid, timestamptz, text);
DROP TABLE tenant_plan_history;
