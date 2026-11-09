-- +goose NO TRANSACTION
-- 00092 -- T157: transactions_tenant_employee_practice_idx, a person's PRACTICE records by occurred_at.
--
-- WHAT READS IT. "Does a practice row of this person's sit after this check-in": ListOpenCheckIns'
-- was_maskable and CountMaskedOpenCheckIns' EXISTS (db/queries/anomalies.sql), both
-- `p.practice AND p.occurred_at > t.occurred_at` for one person. The predicate is the query's own
-- `practice`, so the planner proves it.
--
-- WHY, AND WHY BESIDE 00091. 00091 takes the directionless rows out of the next/previous-record walk;
-- this read is the same walk's other half. Through transactions_tenant_employee_occurred_idx it read
-- EVERY row of the person's after the check-in -- the flood of refused and ignored taps 00091 is about
-- included -- to find the practice ones, once per check-in of the window (CountMaskedOpenCheckIns asks
-- it for every check-in, not only the listed ones), or the planner gave up and scanned the whole table.
-- Measured before / after on a lane database (T157's close-out in docs/backlog.md).
--
-- WHY IT COSTS NOTHING ON THE TAP PATH. No tap is born practice any more (ADR 0026 -- internal/domain/tap's
-- Decision.Practice is always false; a manual row writes the literal false); the rows this index holds
-- are the historical ones, fixed for ever by §4.3. An INSERT whose row fails a partial index's
-- predicate does not touch the index, so every tap from now on writes nothing to it.
--
-- WHAT IT DOES NOT CHANGE. No query text, no result. The previous binary runs on this schema unchanged.
--
-- A FILE OF ITS OWN, CONCURRENTLY, OUTSIDE A TRANSACTION -- 00091's header, word for word: no request
-- waits for the build (SHARE UPDATE EXCLUSIVE), the build waits for older transactions, no lock_timeout
-- (a session setting would outlive the statement on goose's pooled connection), re-runnable (a leftover
-- INVALID index of this name is dropped first), the Down drops it CONCURRENTLY too, and the test walks
-- over later migrations' Downs skip this file.

-- +goose Up
DROP INDEX CONCURRENTLY IF EXISTS public.transactions_tenant_employee_practice_idx;
CREATE INDEX CONCURRENTLY transactions_tenant_employee_practice_idx
    ON public.transactions (tenant_id, employee_id, occurred_at)
    WHERE practice;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.transactions_tenant_employee_practice_idx;
