-- +goose Up

-- Migration 0016 -- M6-12 phase A: what a month of this product COSTS, and the
-- frozen record of what it cost.
--
-- Three things, in the order they depend on each other:
--   1. tenants.price_per_employee_month -- the price, ON THE TENANT ROW.
--   2. two lifecycle predicates         -- ONE definition of "billable", callable
--                                          from every query that counts.
--   3. billing_periods                  -- the FROZEN monthly count and invoice
--                                          draft. APPEND-ONLY (section 4.3 family).
--
-- RED LINES:
--   section 4.3  A frozen invoice line that can be edited is not frozen. Same
--                belt+braces as transactions/audit_log/policy_versions: REVOKE
--                UPDATE, DELETE from tappa_app (privilege) AND a BEFORE UPDATE OR
--                DELETE trigger reusing tappa_forbid_mutation() from 0005 (which
--                binds the superuser tappa_owner too).
--   section 4.5  Tenant isolation: the RLS five plus an explicit GRANT.
--   section 6    Money is numeric. NO float anywhere on this path -- not in the
--                column, not in the multiplication (Postgres numeric arithmetic is
--                exact), and not in the Go type sqlc derives (pgtype.Numeric,
--                which carries a big.Int mantissa and a decimal exponent).
--   section 6    A MONTH IS RESOLVED IN THE TENANT'S OWN ZONE. Everything stored is
--                timestamptz UTC; the boundary that produced it is stored beside
--                it so a later timezone change cannot silently redefine a period
--                that has already been closed.


-- --- 1. The price ------------------------------------------------------------
--
-- WHY IT IS A COLUMN AND NOT A CONSTANT, and the reason is stronger than "do not
-- hardcode". docs/handoff.md section 11 sells a FOUNDING OFFER whose second half is
-- "omur boyu fiyat sabit" -- a lifetime price lock. A constant in Go would mean the
-- day the list price moves, it moves for the two businesses that were promised it
-- would not. The promise is per-tenant, so the price is per-tenant.
--
-- numeric(10,2), never float (section 6). DEFAULT 1.50 is the list price in
-- docs/handoff.md section 11; it is a DEFAULT rather than a CHECK because the whole
-- point is that a tenant's price may differ from it.
--
-- CHECK (>= 0): a negative price is a credit note, which this product does not
-- issue. Refusing it here means an invoice draft can never be built from one.
ALTER TABLE tenants
    ADD COLUMN price_per_employee_month numeric(10, 2) NOT NULL DEFAULT 1.50
        CONSTRAINT tenants_price_not_negative CHECK (price_per_employee_month >= 0);

-- THE PRICE AND THE PLAN ARE THE OPERATOR'S, NOT THE CUSTOMER'S -- and until this
-- statement they were not: 00001 granted tappa_app table-wide UPDATE and INSERT on
-- tenants, so any code path reaching either could set the price it is about to be
-- billed at, and the plan that decides whether it is billed at all.
--
-- 🔴 BOTH VERBS, AND THE SECOND ONE WAS MISSED IN THE FIRST DRAFT OF THIS MIGRATION.
-- Narrowing UPDATE alone leaves the same power one statement away: measured, as
-- tappa_app, `INSERT INTO tenants (..., plan, price_per_employee_month) VALUES (...,
-- 'founding', 0.00)` succeeded and produced a tenant that is free forever. A wizard
-- that creates its own tenant row is the natural shape of a self-service signup --
-- which does not exist yet, and is exactly why the column list can be closed now at
-- zero cost rather than after somebody needs it open. Nothing writes either column
-- today (`rg -n '(UPDATE|INSERT INTO) tenants' db/queries internal --glob
-- '!*_test.go'` is empty as of this migration).
--
-- THE REVOKE IS REQUIRED, NOT DECORATIVE (the M1-04 lesson, repeated in 0005/0013):
-- db-init's ALTER DEFAULT PRIVILEGES hands tappa_app all four DMLs (measured:
-- `SELECT defaclacl FROM pg_default_acl WHERE defaclobjtype='r'` -> tappa_app=arwd),
-- so omitting a column from a GRANT does nothing on its own. The table privilege
-- must be REVOKEd first; that also clears any column-level grants on it, which is
-- what makes the narrower GRANT the whole of the privilege (measured: after
-- `REVOKE <verb> ON t`, has_column_privilege is false for every column).
--
-- The listed columns are the tenant's own facts, which the panel may legitimately
-- write. `plan`, `price_per_employee_month` and `created_at` are absent from BOTH
-- lists ON PURPOSE -- the third because the founding window is computed FROM it
-- (tappa_first_chargeable_month), so a tenant able to write its own created_at can
-- move its own free months. A row written without any of the three takes this
-- table's DEFAULTs ('founding', 1.50, now()), which is the correct outcome for a new
-- customer and needs no privilege.
--
-- ⚠️ THE TWO LISTS DIFFER BY `id`, AND THE INSERT LIST NEEDS IT STRUCTURALLY. The
-- tenants policy scopes on `id`, so a row whose id is not the transaction's context
-- fails WITH CHECK; without INSERT on `id` the application could not create a tenant
-- at all -- the DEFAULT gen_random_uuid() would never match the context. `id` stays
-- out of the UPDATE list, where re-pointing a row's identity has no legitimate use.
REVOKE UPDATE ON tenants FROM tappa_app;
GRANT UPDATE (name, vat_number, business_type, structure, timezone)
    ON tenants TO tappa_app;
REVOKE INSERT ON tenants FROM tappa_app;
GRANT INSERT (id, name, vat_number, business_type, structure, timezone)
    ON tenants TO tappa_app;


-- --- 2. ONE definition of the lifecycle, and ONE definition of "billable" -----
--
-- WHY THESE ARE FUNCTIONS RATHER THAN A PREDICATE COPIED INTO EVERY QUERY. Two
-- queries count billable employees -- the live preview of a month that is still
-- open, and the statement that freezes a month that has closed -- and a third will
-- exist the first time somebody asks "why is my bill different this month?". Copies
-- of one rule, each self-consistent, drifting invisibly, are the exact class
-- internal/domain/ledger/report.go names at readLimit and fixed the same way: one
-- definition, many callers. A SQL function that is IMMUTABLE and a single
-- expression is inlined by the planner, so the sharing costs nothing at run time.
--
-- SECURITY INVOKER (the default) and no table access: both bodies take VALUES, not
-- relations, so there is no RLS surface here at all. Nothing to bypass.

-- The status the STAMPS imply, which -- when the data is sound -- is the status the
-- row carries.
--
-- 🔴 THIS IS ONLY TOTAL BECAUSE THE LIFECYCLE IS ONE-WAY, and that is a property of
-- db/queries, not of this schema: ConsumeInviteAndActivate is the only statement
-- that writes status='active' and it requires status IN ('invited','active') in BOTH
-- the CTE's EXISTS and the outer UPDATE, so a deactivated employee can never come
-- back (docs/adr/0010 records the cost of that and the ways out that were weighed).
-- One monotone interval per person: invited -> active -> deactivated. If a
-- reactivation path is ever added, a person gets SEVERAL intervals, this function
-- stops being able to describe them, and the honest fix is a lifecycle EVENTS table
-- rather than a cleverer expression over two stamps.
-- +goose StatementBegin
CREATE FUNCTION tappa_employee_lifecycle_status(
        p_activated_at   timestamptz,
        p_deactivated_at timestamptz)
    RETURNS text
    LANGUAGE sql
    IMMUTABLE
    SET search_path = pg_catalog, pg_temp
    AS $$
        SELECT CASE
            WHEN p_deactivated_at IS NOT NULL THEN 'deactivated'
            WHEN p_activated_at   IS NOT NULL THEN 'active'
            ELSE 'invited'
        END;
    $$;
-- +goose StatementEnd

-- Was this person ACTIVE at any instant in [p_from, p_to)?
--
-- 🔴 THE FIRST CONJUNCT IS DEFENSIVE, NOT CORRECTIVE, AND THE DIFFERENCE IS THE
-- WHOLE OF WHAT THIS COMMENT HAS TO GET RIGHT.
--
-- What it refuses: a row whose status DISAGREES with its stamps. Somebody marked
-- 'deactivated' who carries no deactivated_at left at an unknown moment, and the
-- naive interval reading -- "no end stamp means still employed" -- bills them
-- FOREVER, in the closing month and in every month after it.
--
-- ⚠️ BUT NO PRODUCT PATH CAN WRITE THAT ROW, and an earlier draft of this comment
-- said the opposite by blaming the demo seed. Both lifecycle writes stamp
-- unconditionally:
--
--	db/queries/employees.sql  DeactivateEmployee        deactivated_at = COALESCE(deactivated_at, now())
--	db/queries/invites.sql    ConsumeInviteAndActivate  activated_at   = COALESCE(e.activated_at, now())
--
-- and test/fixtures/seed.sql computes both stamps from the status it is inserting,
-- so a freshly seeded database has NONE of these rows. What produces them is TEST
-- FIXTURES that insert employees directly, naming a status without the stamp that
-- matches it.
--
-- ⚠️ THE DEACTIVATION SIDE OF THAT IS NOW CLOSED AT ITS ONE SOURCE. An earlier
-- version of this paragraph named internal/handler/day_db_test.go as a SECOND
-- producer; it is not an independent one -- it calls the same seedFlow.hire() helper,
-- and that helper (internal/handler/seedflow_db_test.go) now writes deactivated_at
-- when the status says so. What still produces disagreeing rows is the ACTIVATION
-- side: fixtures across several packages insert status='active' with no
-- activated_at. Those are counted rather than closed -- the CHECK measurement below
-- is why.
--
-- 🔴 AND THE POPULATION STILL GROWS WITH EVERY TEST RUN, SO NO COUNT OF IT KEEPS.
-- Any number written here would be stale before it was read; what is durable is the
-- command:
--
--	SELECT count(*) FILTER (WHERE tappa_employee_lifecycle_status(activated_at, deactivated_at) <> status)
--	     , count(*)
--	FROM employees WHERE tenant_id = '...';
--
-- In production this conjunct is expected to be a NO-OP -- that is the claim, and
-- the same command is how anybody checks it. What it buys is that a demo, a restored
-- fixture or a hand-written repair cannot put a ghost employee on an invoice.
--
-- AND IT ERRS TOWARD THE CUSTOMER, deliberately: a person this schema cannot prove
-- was employed is not charged for. The opposite error -- charging for somebody who
-- had left -- is the one that arrives on an invoice, gets disputed, and cannot be
-- undone once the period is frozen.
--
-- WHAT IT COSTS, SAID OUT LOUD: somebody who genuinely worked six months but whose
-- deactivated_at was lost is billed for none of them. That is under-billing, it is
-- silent in the number alone, and it is why every query that calls this ALSO counts
-- the disagreeing rows and stores the count on the frozen period. Excluded is not
-- allowed to mean invisible (the same rule Report.PracticeTaps follows).
--
-- ⚠️ WHAT WAS WEIGHED AND REJECTED: a CHECK constraint on employees forcing the
-- stamps to match the status, which would make this conjunct unnecessary. Measured
-- rather than argued -- each half added NOT VALID and the suite run under it,
-- SEPARATELY, because running both together lets one half's failures mask the
-- other's (that mistake produced the first, too-low figures for the cheap half):
--
--	psql: ALTER TABLE employees ADD CONSTRAINT probe CHECK (<half>) NOT VALID;
--	make test          # count violations, red tests and red packages
--	psql: ALTER TABLE employees DROP CONSTRAINT probe;
--
-- The two halves are not the same size by any measure taken: the activation half
-- (status='active' implies activated_at) fails by more than an order of magnitude
-- more insert sites, and across nearly every package that seeds an employee, while
-- the deactivation half touches a handful. Exact counts are deliberately absent for
-- the reason above -- they move every run.
--
-- 🔴 AND A CHECK WOULD MAKE THIS RULE UNTESTABLE, which is the argument that does
-- not depend on any count. A disagreeing row is the INPUT to the case that proves
-- the conjunct works, so each half of the constraint would reject one of the cases
-- in TestBillingDB_BillableDefinition along with the accidents -- a different case
-- each:
--
--	the deactivation half rejects  "marked as left but carrying no leaving date"
--	the activation half rejects    "marked active but carrying no activation date"
--
-- (The third disagreeing case, "marked invited but carrying an activation date",
-- survives both -- which is its own reason not to trust a pair of CHECKs to define
-- this: they do not cover the class.) A half-enforced invariant is also worse than
-- an explicit predicate, because a reader would trust it.
-- +goose StatementBegin
CREATE FUNCTION tappa_employee_is_billable(
        p_status         text,
        p_activated_at   timestamptz,
        p_deactivated_at timestamptz,
        p_from           timestamptz,
        p_to             timestamptz)
    RETURNS boolean
    LANGUAGE sql
    IMMUTABLE
    SET search_path = pg_catalog, pg_temp
    AS $$
        -- public.-qualified because search_path is pinned above and does NOT
        -- include public: the same discipline 0005's trigger bodies follow, and
        -- the reason an unqualified call fails at CREATE time rather than
        -- resolving to whatever a caller's search_path happens to hold.
        SELECT public.tappa_employee_lifecycle_status(p_activated_at, p_deactivated_at) = p_status
           AND p_activated_at IS NOT NULL
           -- 🔴 BOTH INTERVALS ARE HALF-OPEN AND BOTH COMPARISONS ARE STRICT, and
           -- that pairing is the whole of the boundary rule. Employment is
           -- [activated_at, deactivated_at) and the period is [p_from, p_to), so
           -- they overlap exactly when each STARTS BEFORE the other ENDS:
           -- activated_at < p_to and deactivated_at > p_from.
           --
           -- The second one was written `>=` in the first draft of this migration
           -- and it was wrong by one instant in the direction that costs a customer
           -- money: somebody deactivated at exactly local midnight on the 1st has a
           -- last active instant of 23:59:59 on the previous month's last day --
           -- they belong to the month that already billed them, not to this one.
           -- Pinned from both sides by the two boundary rows in
           -- TestBillingDB_BillableDefinition (deactivated AT the boundary is not
           -- billable; one microsecond later is).
           AND p_activated_at < p_to
           AND (p_deactivated_at IS NULL OR p_deactivated_at > p_from);
    $$;
-- +goose StatementEnd


-- The UTC instant at which a LOCAL month begins, in p_timezone.
--
-- 🔴 A MONTH BEGINS IN MALTA, NOT IN UTC (section 6). The Rusty Bar works
-- 18:00-02:00, so the boundary is one an overnight shift crosses: resolving the
-- first of the month as a UTC midnight would put the last two hours of the previous
-- month's final night into the new month, in summer AND winter, by different
-- amounts. AT TIME ZONE on a bare timestamp reads the wall clock in that zone,
-- which is exactly the question being asked.
--
-- STABLE rather than IMMUTABLE: zone rules are looked up and can be updated by a
-- tzdata release. That is also why billing_periods stores the instants this returns
-- instead of recomputing them -- see period_from below.
--
-- date_trunc first, so a caller passing any day of the month gets that month.
-- +goose StatementBegin
CREATE FUNCTION tappa_local_month_start(p_month date, p_timezone text)
    RETURNS timestamptz
    LANGUAGE sql
    STABLE
    SET search_path = pg_catalog, pg_temp
    AS $$
        SELECT date_trunc('month', p_month::timestamp) AT TIME ZONE p_timezone;
    $$;
-- +goose StatementEnd

-- The first month this tenant can be CHARGED for -- i.e. the month the founding
-- offer's free window ends.
--
-- THE OFFER IS "ilk 3 ay ucretsiz" (docs/handoff.md section 11) AND THIS READS IT AS
-- THREE BILLING PERIODS, which is how a customer counts free months: the month they
-- signed up in and the two after it. Three zero invoices, not two and a fraction.
--
-- ⚠️ IT IS DELIBERATELY MONTH-ALIGNED, AND THAT IS A DEPARTURE FROM READING THE
-- OFFER AS AN INSTANT (`created_at + interval '3 months'`). A signup lands on an
-- ARBITRARY DAY of the month, so the instant reading lands on an arbitrary day too
-- -- and when that day is not the 1st it falls in the MIDDLE of a billing period.
-- A period is either invoiced or it is not: there is no half-invoice in this
-- product, and pro-rating a two-customer founding offer would be inventing a rule
-- nobody asked for. The month-aligned reading rounds that boundary UP, deliberately,
-- so the customer never gets less than three whole free invoices.
--
-- The two readings differ only for a mid-month signup, and only about the month the
-- instant falls in; for a signup on the 1st they coincide exactly.
--
-- ⚠️ NO DATE FROM THE DEVELOPMENT SEED IS QUOTED HERE, and an earlier draft quoted
-- two. test/fixtures/seed.sql writes the design partners' created_at as
-- `now() - interval '90 days'`, so their signup date -- and therefore their free
-- months and their first invoice -- is a property of THE DAY THE DATABASE WAS
-- SEEDED, not of this repository. Any tenant's actual window is a query, never a
-- constant:
--
--	SELECT name, (created_at AT TIME ZONE timezone)::date AS signed_up_local,
--	       tappa_first_chargeable_month(plan, created_at, timezone) AS first_invoice
--	FROM tenants;
--
-- 'standard' adds no free months, so its signup month is already chargeable and
-- free_period is false for every period. Returning a date in both cases (rather
-- than NULL for standard) keeps every caller's test a single `<` comparison.
--
-- STABLE for the same reason as tappa_local_month_start: it reads a zone.
-- +goose StatementBegin
CREATE FUNCTION tappa_first_chargeable_month(
        p_plan       text,
        p_created_at timestamptz,
        p_timezone   text)
    RETURNS date
    LANGUAGE sql
    STABLE
    SET search_path = pg_catalog, pg_temp
    AS $$
        SELECT (date_trunc('month', p_created_at AT TIME ZONE p_timezone)
                + CASE WHEN p_plan = 'founding' THEN interval '3 months'
                       ELSE interval '0' END)::date;
    $$;
-- +goose StatementEnd


-- What a period COSTS. One line of arithmetic, and it lives here because it has
-- THREE callers that must never disagree: the generated column below, the live
-- preview of a month that is still open, and whatever renders either of them.
--
-- 🔴 NO FLOAT TOUCHES IT (section 6). numeric x integer in Postgres is exact
-- decimal arithmetic, and numeric(10,2) x integer has scale 2 exactly, so storing
-- the result in numeric(12,2) rounds nothing. Go never performs this multiplication
-- at all -- internal/domain/billing carries the value, it does not compute it --
-- which is why there is no rounding mode to argue about on the Go side.
--
-- IMMUTABLE is required (a STORED generated column accepts nothing else) and is
-- honest here: CASE, comparison and multiplication over the arguments, no reads.
-- +goose StatementBegin
CREATE FUNCTION tappa_billing_amount_due(
        p_free_period    boolean,
        p_unit_price     numeric,
        p_employee_count integer)
    RETURNS numeric
    LANGUAGE sql
    IMMUTABLE
    SET search_path = pg_catalog, pg_temp
    AS $$
        SELECT CASE WHEN p_free_period THEN 0::numeric
                    ELSE p_unit_price * p_employee_count END;
    $$;
-- +goose StatementEnd


-- --- 2b. Who may EXECUTE the five ---------------------------------------------
--
-- Functions get PUBLIC EXECUTE by default. Every resolver in this schema takes it
-- back (00003, 00004, 00009, 00011, 00012 all pair `REVOKE ALL ... FROM PUBLIC`
-- with a GRANT to tappa_app), and these five follow that house rule rather than the
-- one exception to it.
--
-- ⚠️ THE EXCEPTION IS 00011's TRIGGER FUNCTION, AND ITS REASON DOES NOT APPLY HERE.
-- 00011:482-500 keeps PUBLIC EXECUTE and says so out loud, but its stated reason is
-- that 00011 was ALREADY APPLIED and editing an applied migration's DDL is what
-- CLAUDE.md section 6 forbids -- it also says "a REVOKE would still be tidier and
-- costs nothing at runtime". This migration is not yet applied anywhere, so the
-- tidier form is simply available.
--
-- WHAT THIS IS NOT: a containment measure. None of the five is SECURITY DEFINER
-- (all run as the caller), none reads a relation, and none takes a tenant id -- they
-- are arithmetic over their arguments, so a hostile caller learns nothing by running
-- one. The REVOKE is the same "close it now, at zero cost, before anybody needs it
-- open" argument the tenants column lists are closed on, applied to a surface that
-- is currently harmless.
--
-- tappa_app needs EXECUTE for two reasons, not one: db/queries/billing.sql calls
-- four of them directly, AND billing_periods.amount_due is a STORED generated column
-- whose expression is evaluated as the INSERTING role.
REVOKE ALL ON FUNCTION tappa_employee_lifecycle_status(timestamptz, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION tappa_employee_is_billable(text, timestamptz, timestamptz, timestamptz, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION tappa_local_month_start(date, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION tappa_first_chargeable_month(text, timestamptz, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION tappa_billing_amount_due(boolean, numeric, integer) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION tappa_employee_lifecycle_status(timestamptz, timestamptz) TO tappa_app;
GRANT EXECUTE ON FUNCTION tappa_employee_is_billable(text, timestamptz, timestamptz, timestamptz, timestamptz) TO tappa_app;
GRANT EXECUTE ON FUNCTION tappa_local_month_start(date, text) TO tappa_app;
GRANT EXECUTE ON FUNCTION tappa_first_chargeable_month(text, timestamptz, text) TO tappa_app;
GRANT EXECUTE ON FUNCTION tappa_billing_amount_due(boolean, numeric, integer) TO tappa_app;


-- --- 3. billing_periods -- the FROZEN month ----------------------------------
--
-- 🔴 WHY IT IS A TABLE AT ALL, when every figure in it is derivable. Because they
-- stop being derivable. The M6-12 card asks that "a past month's figure is not
-- recomputed, the frozen value is read (the invoice does not change even if an
-- employee is deleted later)" -- and the roster is mutable (people are deactivated,
-- moved, and GDPR-anonymised), the price can change, and the tenant's TIMEZONE can
-- change, which would move the very boundary the month was counted over. A recomputed
-- invoice is one that quietly disagrees with the one that was sent.
--
-- 🔴 WHICH IS ALSO WHY IT IS APPEND-ONLY. A row here is the answer to "what did we
-- invoice, and on what basis". If it can be edited, freezing it bought nothing.
-- Corrections take the shape section 4.3 already prescribes everywhere else: a new
-- record plus an audit_log entry, never an edit. There is deliberately no
-- credit-note shape in this migration -- the first correction that is actually
-- needed decides it, with its own migration and its own ADR.
CREATE TABLE billing_periods (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- tenant_id: the mandatory scope key (section 4.5). RESTRICT: a business with
    -- billing history is not deletable.
    tenant_id    uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,

    -- period_month is the LABEL: the first LOCAL day of the month this row bills.
    -- A date, not a timestamptz, because a month is not an instant -- it is the
    -- same thing internal/domain/ledger.Date says about a calendar day, and the
    -- reason the instants it resolves to are stored SEPARATELY below.
    period_month date NOT NULL
        CONSTRAINT billing_periods_month_is_first_of_month
            CHECK (EXTRACT(DAY FROM period_month) = 1),

    -- period_from / period_to are that label RESOLVED, once, in timezone below:
    -- the UTC instants of local midnight on the 1st and of local midnight on the
    -- 1st of the next month. To is EXCLUSIVE.
    --
    -- 🔴 THEY ARE STORED RATHER THAN RECOMPUTED BECAUSE THE ZONE CAN MOVE. A tenant
    -- that changes Europe/Malta to Europe/Istanbul would shift every historical
    -- month boundary by an hour -- and an hour at a month boundary is a night
    -- shift, which is the class of bug section 6 names. Freezing the instants makes
    -- a closed period say what it counted, not what it would count today.
    period_from  timestamptz NOT NULL,
    period_to    timestamptz NOT NULL,
    -- The zone that produced them, frozen with them so the row explains itself.
    timezone     text NOT NULL,

    -- The commercial state AT CLOSE, frozen so a month that has been invoiced keeps
    -- the terms it was invoiced under.
    plan         text NOT NULL CHECK (plan IN ('founding', 'standard')),
    -- free_period: was this month inside the founding offer's free window?
    -- Frozen rather than derived -- the window's definition lives in
    -- db/queries/billing.sql and is the only place it is computed.
    --
    -- ⚠️ WHAT FREEZING BUYS, BOUNDED TO WHAT IT ACTUALLY GUARANTEES. A CLOSED month
    -- cannot change: moving a tenant to 'standard' afterwards leaves its plan and
    -- its free_period exactly as they were, because nothing may UPDATE this table.
    -- It does NOT protect a month that has not been closed yet: free_period is
    -- computed from tenants.plan at the instant of the close, and nothing in this
    -- schema forces periods to be closed in order or promptly. So a founding tenant
    -- switched to 'standard' before somebody gets round to closing an old free month
    -- WILL have that month closed as chargeable. Making that impossible needs either
    -- a plan-history table or an ordering constraint on closes; neither is built
    -- here, and this sentence exists so nobody assumes one is.
    free_period  boolean NOT NULL,

    -- employee_count is the billable headcount as tappa_employee_is_billable
    -- defined it over [period_from, period_to).
    --
    -- 🔴 THE COUNT IS FROZEN; WHO WAS COUNTED IS NOT, AND NOTHING ELSE IN THIS
    -- SCHEMA WILL ANSWER THAT LATER. This is the limit of what freezing buys and it
    -- is stated here because it is invisible otherwise -- the row looks like a
    -- complete record of a month and is a complete record of a NUMBER.
    --
    -- Concretely: "why 24?" is answerable only by re-running the definition against
    -- `employees` AS IT IS NOW, and the roster is mutable by design. A person moved
    -- or deactivated afterwards still resolves; a person ANONYMISED under GDPR
    -- (Q13, an UPDATE rather than a delete) does not, and after retention expiry the
    -- question has no answer at all. So a frozen period defends the INVOICE against
    -- a changing roster -- which is what it was built for -- and cannot defend an
    -- ITEMISATION of it.
    --
    -- WHAT WAS NOT DONE, AND WHY IT IS NOT A LINE: storing the counted employee ids
    -- would make the answer durable, and it would also make this table a permanent,
    -- undeletable copy of personal data on an APPEND-ONLY row -- i.e. a store that
    -- GDPR erasure cannot reach, in the one place in this schema that takes no
    -- UPDATE and no DELETE from anybody. That trade is a product and privacy
    -- decision, not a column, so it is named rather than taken.
    --
    -- ⚠️ AND WHAT REMAINS IS NOT "ZERO PERSONAL DATA" -- it is an UNIDENTIFIED
    -- AGGREGATE THAT CAN NARROW AT N=1. This row names nobody, but on a single-person
    -- tenant `employee_count = 1` over [period_from, period_to) is a durable,
    -- undeletable statement that exactly one person was employed in that window. That
    -- is not a section 4 violation and it is not a reason to drop the count; it is the
    -- honest description of the residue, written here so nobody later claims the
    -- table survives erasure carrying nothing at all.
    employee_count integer NOT NULL
        CONSTRAINT billing_periods_count_not_negative CHECK (employee_count >= 0),

    -- unstamped_employees is the honesty column: how many of the tenant's employee
    -- rows had a status that DISAGREED with their lifecycle stamps at the instant
    -- this period was closed. Zero is the ordinary value; anything else means
    -- employee_count is a FLOOR and the screen must say so.
    --
    -- 🔴 IT IS TENANT-WIDE AND POINT-IN-TIME, NOT "THE ROWS THIS PERIOD LOST", and
    -- the difference is worth stating because the column sits beside a period and
    -- invites the narrower reading. It counts every disagreeing row the tenant has,
    -- including people who left years before this month and were never candidates
    -- for it, so it is an UPPER BOUND on the effect and a data-health signal rather
    -- than an adjustment.
    --
    -- ⚠️ IT CANNOT BE NARROWED TO THE PERIOD, and that was measured rather than
    -- assumed. Narrowing means intersecting each row's employment interval with
    -- [period_from, period_to) -- but a disagreeing row is precisely one whose
    -- interval is not known: in every class present in the development database
    -- (`SELECT status, activated_at IS NOT NULL, deactivated_at IS NOT NULL,
    -- count(*) FROM employees WHERE tappa_employee_lifecycle_status(activated_at,
    -- deactivated_at) <> status GROUP BY 1,2,3`) at least one endpoint is missing.
    -- A row could in principle carry both stamps and still contradict its status;
    -- then the interval exists but WHICH fact is wrong is the unknown, so the
    -- intersection would still be a guess. Guessing is the one thing
    -- tappa_employee_is_billable refuses to do, so the count stays wide and honest.
    unstamped_employees integer NOT NULL DEFAULT 0
        CONSTRAINT billing_periods_unstamped_not_negative CHECK (unstamped_employees >= 0),

    -- unit_price: the tenant's price AT CLOSE. Copied, not joined -- the lifetime
    -- price lock means today's tenants.price_per_employee_month is not necessarily
    -- the one this month was billed at.
    unit_price   numeric(10, 2) NOT NULL
        CONSTRAINT billing_periods_price_not_negative CHECK (unit_price >= 0),

    -- currency: an amount without one is not an amount. DEFAULT 'EUR' because every
    -- tenant is Maltese today (docs/handoff.md section 11 prices in EUR) and there
    -- is no second currency to choose from; it lives HERE rather than on tenants so
    -- that a frozen row stays self-describing without adding a tenant field nothing
    -- maintains. The day a tenant bills in another currency, that column moves to
    -- tenants and these rows already carry what they were.
    currency     text NOT NULL DEFAULT 'EUR'
        CONSTRAINT billing_periods_currency_is_iso CHECK (currency ~ '^[A-Z]{3}$'),

    -- 🔴 amount_due IS GENERATED, so no caller can supply a total that disagrees
    -- with the count and the price beside it. That is the difference between a
    -- frozen figure and a figure somebody typed: the arithmetic is the DATABASE's,
    -- it is exact numeric (never float, section 6), and because the table is
    -- append-only it can never be edited into disagreement afterwards. The
    -- expression is tappa_billing_amount_due so the live preview of an OPEN month
    -- computes the identical thing -- see that function for why it is shared.
    amount_due   numeric(12, 2) NOT NULL
        GENERATED ALWAYS AS (
            tappa_billing_amount_due(free_period, unit_price, employee_count)
        ) STORED,

    -- closed_by: WHO closed the period. NOT NULL and same-tenant, because unlike
    -- transactions.entered_by (a tap has no author) a close is always somebody's
    -- deliberate act -- see db/queries/billing.sql for why it is an act and not a
    -- side effect of a page view.
    closed_by    uuid NOT NULL,
    closed_at    timestamptz NOT NULL DEFAULT now(),
    created_at   timestamptz NOT NULL DEFAULT now(),

    -- 🔴 ONE ROW PER TENANT PER MONTH, and this constraint is what makes "frozen"
    -- real rather than a convention: with the table append-only, a second close
    -- cannot overwrite the first, and this refuses to let it sit beside the first
    -- either. A month has exactly one answer.
    --
    -- It is also the tenant_id-leading index section 6 requires (a UNIQUE constraint
    -- builds one), and it serves the only listing this table has -- a tenant's
    -- periods newest first, which a btree scans backwards.
    CONSTRAINT billing_periods_tenant_month_key UNIQUE (tenant_id, period_month),

    CONSTRAINT billing_periods_span_is_ordered CHECK (period_from < period_to),

    -- Same-tenant composite FK, the M1-03 pattern: the admin who closed the period
    -- must belong to the tenant being billed. admin_users_id_tenant_key (00006)
    -- exists precisely so composite references like this are possible.
    CONSTRAINT billing_periods_closed_by_fk
        FOREIGN KEY (closed_by, tenant_id)
        REFERENCES admin_users (id, tenant_id) ON DELETE RESTRICT
);

ALTER TABLE billing_periods ENABLE ROW LEVEL SECURITY;
-- FORCE: the table owner is subject to the policy too (superuser excepted -- M0-03).
ALTER TABLE billing_periods FORCE ROW LEVEL SECURITY;

-- The policy expression is NORMATIVE and verbatim per ADR 0002 madde 3 / Q27: with
-- no context the GUC is either NULL (never written) or '' (written once, tx over);
-- NULLIF collapses both to NULL so no row matches (fail-closed). A bare ::uuid cast
-- is FORBIDDEN -- it raises on the empty string, making behaviour depend on the
-- connection's history.
CREATE POLICY billing_periods_tenant_isolation ON billing_periods
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- APPEND-ONLY (section 4.3) -- BRACE 1: privilege. NOTE the REVOKE is mandatory:
-- db-init's ALTER DEFAULT PRIVILEGES grants all four DMLs on every new table, so
-- leaving UPDATE/DELETE out of the GRANT does nothing by itself (M1-04 lesson).
GRANT SELECT, INSERT ON billing_periods TO tappa_app;
REVOKE UPDATE, DELETE ON billing_periods FROM tappa_app;

-- APPEND-ONLY -- BELT 2: the trigger, which also binds the superuser tappa_owner,
-- the one path no REVOKE can close. Reuses tappa_forbid_mutation() from 0005.
CREATE TRIGGER billing_periods_no_mutation
    BEFORE UPDATE OR DELETE ON billing_periods
    FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation();


-- +goose Down

-- Order: the table first (its trigger references the shared function, which is NOT
-- dropped here -- 0005 owns it and four other tables still use it), then the two
-- predicates, then the tenants changes.
DROP TABLE billing_periods;

DROP FUNCTION IF EXISTS tappa_billing_amount_due(boolean, numeric, integer);
DROP FUNCTION IF EXISTS tappa_first_chargeable_month(text, timestamptz, text);
DROP FUNCTION IF EXISTS tappa_local_month_start(date, text);
DROP FUNCTION IF EXISTS tappa_employee_is_billable(text, timestamptz, timestamptz, timestamptz, timestamptz);
DROP FUNCTION IF EXISTS tappa_employee_lifecycle_status(timestamptz, timestamptz);

-- Restore 00001's grant EXACTLY: table-wide UPDATE **and INSERT** back. Revoking
-- each table privilege first also clears the column-level grants on it (measured),
-- so this leaves no residue behind; dropping the column takes its own column ACL
-- with it in any case. Verified after `make migrate-down` with
--   SELECT relacl FROM pg_class WHERE relname = 'tenants';
--   SELECT attname, attacl FROM pg_attribute
--    WHERE attrelid = 'tenants'::regclass AND attnum > 0 AND attacl IS NOT NULL;
-- which must read tappa_app=arwd and NO rows respectively.
REVOKE UPDATE ON tenants FROM tappa_app;
REVOKE INSERT ON tenants FROM tappa_app;
ALTER TABLE tenants DROP COLUMN price_per_employee_month;
GRANT UPDATE, INSERT ON tenants TO tappa_app;
