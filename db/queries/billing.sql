-- billing.sql -- M6-12 phase A: what a month of this product costs, and the frozen
-- record of what it cost.
--
-- FOUR STATEMENTS, and the split between them is the design:
--   PreviewBillingPeriod  reads a month LIVE. Writes nothing.
--   CloseBillingPeriod    FREEZES a month. The only write in this file.
--   GetBillingPeriod      reads one frozen month.
--   ListBillingPeriods    reads the frozen history, newest first.
--
-- 🔴 THE CALLER SUPPLIES A TENANT, A MONTH AND AN AUTHOR -- NEVER A NUMBER. Every
-- figure that lands on a frozen row is computed by the statement that writes it:
-- the headcount from `employees`, the price from `tenants`, the month's UTC
-- boundaries from the tenant's own zone, the free-period decision from the plan and
-- the signup date, and the total from a GENERATED column. An HTTP handler cannot
-- post an invoice amount, because there is no parameter to post it into. That
-- matters more here than anywhere else in this repository, because migration 0016
-- makes the table APPEND-ONLY: a wrong row is permanent.
--
-- 🔴 THE ARITHMETIC IS NOT WRITTEN TWICE. Preview and Close must agree exactly --
-- the number a manager saw before pressing the button has to be the number that
-- freezes -- so the two statements are the SAME three CTEs (scope / bounds /
-- counted) over the same five functions from migration 0016, and differ only in what
-- they do with the result. The list this file must keep true, and the command that
-- produces it:
--
--	rg -c 'tappa_employee_is_billable|tappa_employee_lifecycle_status|tappa_local_month_start|tappa_first_chargeable_month|tappa_billing_amount_due' db/queries/billing.sql
--
-- TENANT SCOPE (CLAUDE.md section 4.5, belt + braces on RLS): every statement here
-- carries an explicit tenant_id predicate AND runs inside db.(*DB).WithTenant. The
-- id comes from the signed panel session (httpx.AdminIdentity), never from the
-- client. On the write it is doubled again: the scope CTE reads `tenants` under the
-- tenants policy (which scopes on id), so a mismatched @tenant_id yields no source
-- row at all, and the RLS WITH CHECK would refuse the resulting value anyway.
--
-- 🔴 EVERY WHERE THAT READS A TENANT-SCOPED TABLE NAMES @tenant_id LITERALLY, and
-- the first draft did not: the employee counts were bound transitively, to a column
-- carried down from the scope CTE. That is correct SQL and it produces identical
-- figures -- the rewrite changed no number, which was checked before it was made.
-- Two things changed:
--
--	THE NET REFUSES THE TRANSITIVE FORM. internal/domain/billing/query_test.go asks
--	    every WHERE that reads a tenant-scoped table to bind the scope column to
--	    @tenant_id as a top-level conjunct, so a predicate that names another CTE's
--	    column instead does not satisfy it. Measured by putting the transitive form
--	    back: the net goes RED. ⚠️ AN EARLIER VERSION OF THIS PARAGRAPH SAID THE
--	    OPPOSITE -- that the net "cannot follow it at all, so deleting the scope
--	    CTE's predicate would have left the net green". That was never measured and
--	    it is false in both halves: the net does not follow the chain, it REFUSES
--	    the link, and with the scope predicate also deleted it reports the
--	    WHERE-less read of `tenants` as well.
--	THE BELT BECAME READABLE, which is the older half of §4.5's point. A reader
--	    asking "what scopes this?" now finds the answer in the clause in front of
--	    them instead of following three CTEs up.
--
-- AND THE BELT CONSTRAINS ON ITS OWN, which is what makes it a second defence
-- rather than decoration. Measured with the braces OFF -- as tappa_owner, a
-- superuser, so RLS applies to nothing:
--
--	SELECT count(*) FROM tenants t WHERE t.id = '<a tenant>';   -- 1
--	SELECT count(*) FROM tenants t;                             -- every tenant
--	SELECT count(*) FROM employees e WHERE e.tenant_id = '<a tenant>';        -- that payroll
--	SELECT count(*) FROM employees e WHERE e.tenant_id IN (SELECT id FROM tenants); -- all of them
--
-- The counts themselves are not written down: this is a development database and
-- they grow with every test run.
--
-- WHAT "BILLABLE" MEANS. A person is billable for a month when their employment
-- interval [activated_at, deactivated_at) overlaps it AND their status agrees with
-- their stamps. The second half is not pedantry: read naively, a row marked
-- 'deactivated' that carries no deactivated_at is "still employed" and is billed
-- forever. Count them for any tenant with
--
--	SELECT count(*) FILTER (WHERE tappa_employee_lifecycle_status(activated_at, deactivated_at) <> status)
--	FROM employees WHERE tenant_id = '...';
--
-- ⚠️ IN PRODUCTION THAT COUNT IS EXPECTED TO BE ZERO, and the conjunct is a
-- DEFENCE rather than a repair: the two lifecycle writes both COALESCE their stamp,
-- so no product path can create such a row. In a development database it is not
-- zero and it GROWS PER TEST RUN, because a handful of test fixtures insert
-- employees directly. 0016's tappa_employee_is_billable names the fixtures and the
-- two statements. Those rows are excluded from the count AND counted into
-- unstamped_employees, so "excluded" never means "invisible" (section 4.6's habit,
-- applied to money).
--
-- NO DELETE AND NO UPDATE STATEMENTS, BY DESIGN. 0016 REVOKEs both from tappa_app
-- and a trigger blocks them for the superuser too. A period frozen wrongly is
-- corrected the way section 4.3 corrects everything else -- a new record plus an
-- audit_log row -- and the shape of that correction is not invented here.

-- name: PreviewBillingPeriod :one
-- One month of billing, computed LIVE, for a month that has not been frozen.
--
-- 🔴 IT WRITES NOTHING, AND THAT IS THE POINT OF ITS EXISTING SEPARATELY. The
-- obvious alternative -- have the screen freeze a finished month the first time
-- somebody looks at it -- would make a GET mutate, which this panel has measured and
-- protected three times (M6-07, M6-09, M6-11: "reading the screen writes nothing",
-- and internal/domain/ledger's package comment states there is no non-SELECT store
-- call in it). It would also hand the permanent, unrepeatable act of fixing an
-- invoice to whoever happened to open a page first, including a crawler, a prefetch
-- or a retried request.
--
-- period_has_ended and period_is_after_signup are the SCREEN's copy of two of the
-- three conditions CloseBillingPeriod enforces itself; they exist so the button can
-- be disabled with a reason, never as the authority. The third condition -- one row
-- per month -- is answered by GetBillingPeriod returning a row.
--
-- amount_due comes from the same function the frozen column is generated from, so a
-- preview and the row it becomes cannot print different totals.
WITH scope AS (
    SELECT t.id AS tenant_id, t.name, t.plan, t.timezone, t.created_at,
           t.price_per_employee_month,
           date_trunc('month', @period_month::date)::date AS month
    FROM tenants t
    WHERE t.id = @tenant_id
), bounds AS (
    SELECT s.tenant_id, s.name, s.plan, s.timezone, s.price_per_employee_month, s.month,
           tappa_local_month_start(s.month, s.timezone) AS from_at,
           tappa_local_month_start((s.month + interval '1 month')::date, s.timezone) AS to_at,
           tappa_first_chargeable_month(s.plan, s.created_at, s.timezone) AS first_chargeable,
           date_trunc('month', s.created_at AT TIME ZONE s.timezone)::date AS signup_month
    FROM scope s
), counted AS (
    SELECT b.tenant_id, b.name, b.plan, b.timezone, b.price_per_employee_month, b.month,
           b.from_at, b.to_at, b.first_chargeable, b.signup_month,
           (b.month < b.first_chargeable) AS free_period,
           (SELECT count(*) FROM employees e
             WHERE e.tenant_id = @tenant_id
               AND tappa_employee_is_billable(
                       e.status, e.activated_at, e.deactivated_at, b.from_at, b.to_at))::integer
               AS billable,
           (SELECT count(*) FROM employees e
             WHERE e.tenant_id = @tenant_id
               AND tappa_employee_lifecycle_status(e.activated_at, e.deactivated_at) <> e.status)::integer
               AS unstamped
    FROM bounds b
    -- The belt REPEATED on the row that carries the figures out. It is redundant
    -- against the scope CTE and that is what belt-and-braces means; it is here
    -- rather than on the final SELECT because sqlc v1.28's analyser cannot resolve
    -- a qualified CTE reference in an outer WHERE ("table alias c does not exist"),
    -- and an unqualified one it calls ambiguous. Measured both ways.
    WHERE b.tenant_id = @tenant_id
)
SELECT c.tenant_id,
       c.name                     AS tenant_name,
       c.plan,
       c.timezone,
       c.month                    AS period_month,
       c.from_at                  AS period_from,
       c.to_at                    AS period_to,
       c.first_chargeable         AS first_chargeable_month,
       c.free_period,
       c.billable                 AS employee_count,
       c.unstamped                AS unstamped_employees,
       c.price_per_employee_month AS unit_price,
       tappa_billing_amount_due(c.free_period, c.price_per_employee_month, c.billable)::numeric(12, 2)
                                  AS amount_due,
       (c.to_at <= now())         AS period_has_ended,
       (c.month >= c.signup_month) AS period_is_after_signup
FROM counted c;

-- name: CloseBillingPeriod :one
-- FREEZES one month: counts it, prices it, and writes the row that will never change
-- (0016 is append-only). Returns nothing -- pgx.ErrNoRows -- when the month may not
-- be closed, which the caller reports rather than treats as a failure.
--
-- 🔴 IT IS AN ACT, NOT A SIDE EFFECT, and phase B must keep it one: an owner presses
-- a button, this statement runs once, and audit_log records who did it. The
-- alternative that was weighed -- freeze lazily on the first read of a finished
-- month -- is rejected in PreviewBillingPeriod's header.
--
-- THREE GUARDS, ALL IN THE STATEMENT, because append-only means a junk row is
-- forever and a caller's `if` is not part of the schema:
--
--   1. `to_at <= now()` -- A MONTH THAT HAS NOT ENDED CANNOT BE CLOSED. Freezing
--      August on the 12th of August would bill a headcount that is still moving, and
--      nothing could correct it afterwards. The comparison is against the tenant's
--      own local month end already converted to UTC, so it means the same thing for
--      a Malta tenant and an Istanbul one.
--   2. `month >= signup_month` -- a period from before the business existed is not a
--      billing period. Without this, closing 2019-03 succeeds and leaves a permanent
--      zero-amount row nobody can delete.
--   3. billing_periods_tenant_month_key (UNIQUE) -- a second close of the same month
--      raises a unique violation rather than adding a second answer. The caller
--      surfaces that as "already closed"; it cannot overwrite, because the table
--      takes no UPDATE from anybody, tappa_owner included.
--
-- 🔴 EVERY VALUE EXCEPT @closed_by IS DERIVED HERE. period_from/period_to/timezone
-- are frozen ALONGSIDE the figures on purpose: a tenant who later changes zone would
-- otherwise move the boundary a closed month was counted over -- an hour at a month
-- boundary is an overnight shift, which is exactly the class of bug section 6 names.
-- The row records what it counted, not what it would count today.
--
-- amount_due is absent from the column list because it is GENERATED; it is returned.
WITH scope AS (
    SELECT t.id AS tenant_id, t.plan, t.timezone, t.created_at,
           t.price_per_employee_month,
           date_trunc('month', @period_month::date)::date AS month
    FROM tenants t
    WHERE t.id = @tenant_id
), bounds AS (
    SELECT s.tenant_id, s.plan, s.timezone, s.price_per_employee_month, s.month,
           tappa_local_month_start(s.month, s.timezone) AS from_at,
           tappa_local_month_start((s.month + interval '1 month')::date, s.timezone) AS to_at,
           tappa_first_chargeable_month(s.plan, s.created_at, s.timezone) AS first_chargeable,
           date_trunc('month', s.created_at AT TIME ZONE s.timezone)::date AS signup_month
    FROM scope s
), counted AS (
    SELECT b.tenant_id, b.plan, b.timezone, b.price_per_employee_month, b.month,
           b.from_at, b.to_at, b.signup_month,
           (b.month < b.first_chargeable) AS free_period,
           (SELECT count(*) FROM employees e
             WHERE e.tenant_id = @tenant_id
               AND tappa_employee_is_billable(
                       e.status, e.activated_at, e.deactivated_at, b.from_at, b.to_at))::integer
               AS billable,
           (SELECT count(*) FROM employees e
             WHERE e.tenant_id = @tenant_id
               AND tappa_employee_lifecycle_status(e.activated_at, e.deactivated_at) <> e.status)::integer
               AS unstamped
    FROM bounds b
    -- THE BELT AND THE TWO GUARDS SIT TOGETHER, in the CTE that produces the row
    -- this statement inserts. They are here rather than on the INSERT's own SELECT
    -- for the reason PreviewBillingPeriod states (sqlc v1.28 cannot resolve a CTE
    -- reference in an outer WHERE), and the placement changes nothing about what is
    -- enforced: a CTE that yields no row inserts nothing.
    WHERE b.tenant_id = @tenant_id
      AND b.to_at <= now()
      AND b.month >= b.signup_month
)
INSERT INTO billing_periods (
    tenant_id, period_month, period_from, period_to, timezone,
    plan, free_period, employee_count, unstamped_employees, unit_price, closed_by)
SELECT c.tenant_id, c.month, c.from_at, c.to_at, c.timezone,
       c.plan, c.free_period, c.billable, c.unstamped, c.price_per_employee_month,
       @closed_by
FROM counted c
-- RETURNING carries no tenant name (a RETURNING clause cannot join); the caller
-- already knows which tenant it just closed a period for.
RETURNING id, tenant_id, period_month, period_from, period_to, timezone, plan,
          free_period, employee_count, unstamped_employees, unit_price, currency,
          amount_due, closed_by, closed_at;

-- name: GetBillingPeriod :one
-- The FROZEN row for one month, or pgx.ErrNoRows when the month has not been closed.
--
-- 🔴 NO FIGURE IS RECOMPUTED HERE, which is the M6-12 card's fourth criterion in one
-- statement: no join to employees, no counting function, and no reference to
-- tenants.price_per_employee_month. A person deactivated or anonymised after the
-- close, or a price that changed since, cannot move this answer.
--
-- ⚠️ THE ONE THING THAT IS NOT FROZEN IS THE NAME, and the asymmetry is deliberate
-- rather than an oversight. An invoice is ADDRESSED to whoever the business is now;
-- if they renamed last month, the document should carry the name that identifies
-- them today. The FIGURES answer a different question -- what did this month cost --
-- and those are the ones that must not move. The join is one row under the tenants
-- policy (which scopes on id), so it adds no scope surface.
SELECT b.id, b.tenant_id, t.name AS tenant_name, b.period_month, b.period_from,
       b.period_to, b.timezone, b.plan, b.free_period, b.employee_count,
       b.unstamped_employees, b.unit_price, b.currency, b.amount_due,
       b.closed_by, b.closed_at
FROM billing_periods b
JOIN tenants t ON t.id = b.tenant_id
WHERE b.tenant_id = @tenant_id
  AND b.period_month = date_trunc('month', @period_month::date)::date;

-- name: ListBillingPeriods :many
-- The frozen history, newest month first.
--
-- THE LIMIT IS THE CALLER'S AND IT IS ASKED FOR ONE ROW MORE THAN IT CAN SHOW -- the
-- readLimit/truncatedBy pair internal/domain/ledger/report.go names, reused here so
-- a history longer than the page says so instead of silently ending. Unlike an hours
-- total, a truncated LIST is honest on its own; the extra row only decides whether
-- the screen offers older periods.
--
-- Served by billing_periods_tenant_month_key (tenant_id, period_month), scanned
-- backwards. The tenant name is joined for the reason GetBillingPeriod states: the
-- figures are frozen, the addressee is current.
SELECT b.id, b.tenant_id, t.name AS tenant_name, b.period_month, b.period_from,
       b.period_to, b.timezone, b.plan, b.free_period, b.employee_count,
       b.unstamped_employees, b.unit_price, b.currency, b.amount_due,
       b.closed_by, b.closed_at
FROM billing_periods b
JOIN tenants t ON t.id = b.tenant_id
WHERE b.tenant_id = @tenant_id
ORDER BY b.period_month DESC
LIMIT @row_limit;
