-- 00064 -- M15-02 (ADR 0032 §A.2, §A.5, §A.6): the business day.
--
-- WHAT. Four cutoff columns -- the hierarchy K1, most specific wins: the person
-- (employees.day_cutoff), their department (departments.day_cutoff), their PROFILE branch
-- (locations.day_cutoff, read through employees.location_id -- never the tapped branch: the
-- direction chain is the person's) and the business (tenants.day_cutoff, NOT NULL DEFAULT 05:00).
-- NULL on the three lower levels means "inherit". A cutoff is a WALL-CLOCK time of the tenant's
-- zone (tenants.timezone, the only zone the code has); the instant is resolved in Go (tap
-- package), never here.
--
-- And transactions.business_day_start: the start of the business day a directed nfc/qr decision
-- was made in, in UTC, FROZEN on the row (§A.5). A cutoff can change later; a reader that
-- recomputed the past from today's setting would change last week's pairing -- the same reason
-- 00016 freezes a billing period's zone. So the value is evidence that cannot be recomputed, and
-- this file's Down refuses to drop it once one is written (the 00043 precedent). Historical rows,
-- manual rows and undirected decisions (reject, ignored) carry NULL; this migration back-fills
-- nothing (CLAUDE.md §4.3). Who writes it is M15-04's insertParams; until then every caller
-- passes NULL.
--
-- And tappa_checkout_class: the ONE SQL form of §A.6's six-class checkout classification. It
-- reads no table -- its caller finds P, I and W with subqueries and hands them in -- so it is
-- IMMUTABLE, SECURITY INVOKER and needs nothing but EXECUTE.
--
-- THE CHECK ON business_day_start IS THE SECOND SAFETY, NOT THE FIRST (§A.5): insertParams forces
-- NULL in code for an undirected decision and for the manual channel. A code slip that reached the
-- CHECK would refuse the INSERT with 23514 and lose the record (§4.6), which is why the CHECK is
-- added NOT VALID here and VALIDATEd in a file of its own (00066 -- 00044's pattern: a plain
-- ADD CONSTRAINT CHECK scans every row under ACCESS EXCLUSIVE, ~195 ms at ~2 M rows, measured by
-- the M15 engine lens; ADD COLUMN with no default is a catalogue change and scans nothing).
-- Historical rows are NULL and pass it either way: NOT VALID is chosen for the lock, not for them.
--
-- THE FIRST STATEMENT IS lock_timeout (00037:52, 00038:39): every ALTER below takes ACCESS
-- EXCLUSIVE on a table the tap path reads. Behind a long reader the file gives up with 55P03
-- (nothing changes, run it again at a quiet moment) instead of queueing every later tap behind
-- its lock. The tables are altered in the order the tap path reads them -- the four tables
-- GetEmployeeForTap joins in one statement first, transactions last -- so this file's lock order is
-- the tap's (a tap that already holds transactions holds the other four too).
--
-- PRIVILEGES, MEASURED AT 00063 (has_table_privilege / has_column_privilege, tappa-m15-db):
--   tenants      tappa_app holds SELECT table-wide and UPDATE COLUMN BY COLUMN (00024:120 --
--                name, business_type, timezone), so a new column is not writable until it is
--                named: GRANT UPDATE (day_cutoff) below. Not INSERT: a new tenant takes the
--                default (signup's column list does not name it).
--   locations, departments, employees
--                tappa_app holds SELECT, INSERT, UPDATE table-wide (00002:102, 00003:105; no
--                later migration narrows them), which a new column falls under -- no grant.
--   transactions tappa_app holds SELECT and INSERT table-wide (00005) and nothing else: the new
--                column is written once, by the tap's INSERT, and never updated (§4.3).
-- No other role reaches a new column: their grants on these tables are column lists.
-- internal/db's m11schema_test.go matrix and privcatalog_test.go allowlist carry the result.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 1. The four cutoffs (§A.2). A cutoff is a minute of the day: no seconds (a business day that
--    starts at 05:00:30 is a typing slip, not a setting) and below 24:00 -- '24:00'::time is a
--    valid PostgreSQL value and would land on the same instant as 00:00 of the next day, a second
--    spelling of one boundary (M15 engine lens, low finding 3). date_part('second', …) carries the
--    fractional part too, so 05:00:00.5 is refused by the same test.
-- ---------------------------------------------------------------------------
ALTER TABLE employees
    ADD COLUMN day_cutoff time,
    ADD CONSTRAINT employees_day_cutoff_is_a_minute_of_the_day
        CHECK (date_part('second', day_cutoff) = 0 AND day_cutoff < '24:00');

ALTER TABLE departments
    ADD COLUMN day_cutoff time,
    ADD CONSTRAINT departments_day_cutoff_is_a_minute_of_the_day
        CHECK (date_part('second', day_cutoff) = 0 AND day_cutoff < '24:00');

ALTER TABLE locations
    ADD COLUMN day_cutoff time,
    ADD CONSTRAINT locations_day_cutoff_is_a_minute_of_the_day
        CHECK (date_part('second', day_cutoff) = 0 AND day_cutoff < '24:00');

-- The business level never inherits: it is the end of the coalesce, so it is NOT NULL. A constant
-- DEFAULT on ADD COLUMN is a catalogue change since PostgreSQL 11 (no table rewrite).
ALTER TABLE tenants
    ADD COLUMN day_cutoff time NOT NULL DEFAULT '05:00',
    ADD CONSTRAINT tenants_day_cutoff_is_a_minute_of_the_day
        CHECK (date_part('second', day_cutoff) = 0 AND day_cutoff < '24:00');

-- The owner sets the business cutoff on the Account screen (M15-07): tenants' UPDATE is a column
-- list (00024), so the column is named. A table-wide GRANT UPDATE here would hand tappa_app
-- vat_number, structure, plan and the suspension columns 00024 and 00063 keep shut.
GRANT UPDATE (day_cutoff) ON tenants TO tappa_app;

-- ---------------------------------------------------------------------------
-- 2. The frozen business day of a directed tap (§A.5). Nullable, no default, no back-fill.
-- ---------------------------------------------------------------------------
ALTER TABLE transactions
    ADD COLUMN business_day_start timestamptz,
    ADD CONSTRAINT transactions_business_day_needs_a_direction
        CHECK (business_day_start IS NULL OR type IS NOT NULL) NOT VALID;

-- ---------------------------------------------------------------------------
-- 3. The checkout classification (§A.6). One call per `out` row; the caller supplies:
--
--   prev_in_at          P's occurred_at when P is an `in`, NULL when P is an `out` or absent.
--                       P = the person's directed, non-practice event immediately BEFORE this
--                       out in (occurred_at, id) order.
--   prev_is_out         P is an `out` (consulted only when prev_in_at is NULL).
--   day_has_in          I: a directed, non-practice `in` before this out in (occurred_at, id)
--                       order with occurred_at >= out_day_start.
--   prev_written_later  W: P.created_at > out.created_at (equality is false -> (c), the
--                       over-reporting side; ADR 0032 §A.6 "Eşzamanlı yazım").
--   out_at              the out's occurred_at.
--   out_day_start       the out's business_day_start (NULL on rows before M15 and manual rows).
--
-- Result, the six disjoint classes and their order (ADR 0032 §A.6):
--   no `in` before it  (prev_in_at NULL)
--       out_day_start NULL                                -> 'unanchored' (d)
--       prev_is_out AND day_has_in AND prev_written_later -> 'extra'      (e)
--       otherwise                                         -> 'orphan'     (c)
--   an `in` before it
--       out_day_start set AND prev_in_at < out_day_start  -> 'other_day'  (b)  (a AND b -> b)
--       out_at - prev_in_at > 18 hours                    -> 'too_long'   (a)
--       otherwise                                         -> 'closes'
-- The two closing bounds are inclusive on purpose: an in AT the business day start belongs to the
-- new day (the boundary instant is the new day's -- §A.2) and closes; an out exactly 18 h after
-- its in closes (tap.StaleOpenIn is "> 18 h is stale").
--
-- out_at NULL -> NULL: a row without an instant has no class. It cannot come from a row
-- (transactions.occurred_at is NOT NULL); it is written down so that a broken caller is visible
-- rather than counted as 'closes' by a NULL comparison. A NULL boolean counts as false (IS TRUE),
-- which leans to (c) -- reporting, never hiding.
--
-- THE 18 HOURS IS tap.StaleOpenIn's COPY (internal/domain/tap/decide.go). Two representations of
-- one rule; M15-05's parity test binds them, as TestGuardrails_TagNotActiveIsAnAllowList binds
-- the tag statuses.
--
-- search_path is pinned (the repo's rule for every function, pg_catalog first), which also means
-- the planner does not inline it; it is one CASE per row either way.
-- +goose StatementBegin
CREATE FUNCTION public.tappa_checkout_class(
    prev_in_at timestamptz,
    prev_is_out boolean,
    day_has_in boolean,
    prev_written_later boolean,
    out_at timestamptz,
    out_day_start timestamptz
)
    RETURNS text
    LANGUAGE sql
    IMMUTABLE
    PARALLEL SAFE
    SECURITY INVOKER
    SET search_path = pg_catalog, pg_temp
AS $$
    SELECT CASE
        WHEN out_at IS NULL THEN NULL
        WHEN prev_in_at IS NULL THEN
            CASE
                WHEN out_day_start IS NULL THEN 'unanchored'
                WHEN prev_is_out IS TRUE AND day_has_in IS TRUE AND prev_written_later IS TRUE THEN 'extra'
                ELSE 'orphan'
            END
        WHEN out_day_start IS NOT NULL AND prev_in_at < out_day_start THEN 'other_day'
        WHEN out_at - prev_in_at > interval '18 hours' THEN 'too_long'
        ELSE 'closes'
    END
$$;
-- +goose StatementEnd

-- EXECUTE to the application alone (ADR 0028 §1's rule since 00037; 00061 took PUBLIC's default
-- off the older functions). Its readers are tappa_app's own queries (M15-05).
REVOKE ALL ON FUNCTION public.tappa_checkout_class(timestamptz, boolean, boolean, boolean, timestamptz, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.tappa_checkout_class(timestamptz, boolean, boolean, boolean, timestamptz, timestamptz) TO tappa_app;

-- +goose Down
-- 🔴 A Down THAT WOULD ERASE A FROZEN BUSINESS DAY REFUSES (CLAUDE.md §4.3, §4.6; ADR 0032
-- "Geri alma"; the 00043 precedent). business_day_start cannot be recomputed -- the cutoff that
-- produced it may have changed since -- so while a row carries one this Down stops and changes
-- nothing. The way back after the first tap is the binary, not the schema: the old code neither
-- writes nor reads the column, and the CHECK admits NULL. The four cutoff columns are
-- configuration, not evidence (their history is audit_log's), and are dropped.
--
-- transactions is LOCKED BEFORE IT IS LOOKED AT: between an unlocked EXISTS and the ALTER a tap
-- could commit a row with a business day, and the ALTER would then drop it. ACCESS EXCLUSIVE is
-- what the ALTERs take anyway; taking it first only moves it ahead of the check. All five tables
-- are taken in ONE statement, in the Up's (the tap's) order, so the Down does not hold
-- transactions while it waits for a table a tap holding transactions also holds.
SET LOCAL lock_timeout = '1s';

LOCK TABLE employees, departments, locations, tenants, transactions IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.transactions WHERE business_day_start IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 00064 Down: transactions holds rows with a business_day_start (the frozen business day of a tap); it cannot be recomputed and records are immutable. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP FUNCTION public.tappa_checkout_class(timestamptz, boolean, boolean, boolean, timestamptz, timestamptz);

ALTER TABLE transactions
    DROP CONSTRAINT transactions_business_day_needs_a_direction,
    DROP COLUMN business_day_start;

-- The column grant goes with the column; the REVOKE is stated so the Down reads as the Up's inverse.
REVOKE UPDATE (day_cutoff) ON tenants FROM tappa_app;

ALTER TABLE employees
    DROP CONSTRAINT employees_day_cutoff_is_a_minute_of_the_day,
    DROP COLUMN day_cutoff;

ALTER TABLE departments
    DROP CONSTRAINT departments_day_cutoff_is_a_minute_of_the_day,
    DROP COLUMN day_cutoff;

ALTER TABLE locations
    DROP CONSTRAINT locations_day_cutoff_is_a_minute_of_the_day,
    DROP COLUMN day_cutoff;

ALTER TABLE tenants
    DROP CONSTRAINT tenants_day_cutoff_is_a_minute_of_the_day,
    DROP COLUMN day_cutoff;
