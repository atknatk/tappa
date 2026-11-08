-- +goose NO TRANSACTION
-- 00091 -- T157: transactions_tenant_employee_directed_idx, a person's DIRECTED, non-practice records
-- in (occurred_at, id) order.
--
-- WHAT READS IT. The chain lookups of db/queries/anomalies.sql: ListOpenCheckIns' and
-- CountMaskedOpenCheckIns' next-record LATERAL (`n`) and ListCheckoutsWithoutCheckIn's previous-record
-- LATERAL (`pv`). Each asks for the person's next / previous record with
-- `type IS NOT NULL AND NOT practice` in (occurred_at, id) order. The predicate below is those
-- clauses word for word, so the planner proves it; the key ends in id, so the row comparison
-- `(occurred_at, id) > (t.occurred_at, t.id)` is the index condition and the ORDER BY is the index
-- order -- no sort, and no directionless row read. (ListCheckoutsWithoutCheckIn's "latest `in` before
-- it", `i`, could read this index too, but would filter every checkout on the way back -- 00094 is its
-- index; the third-eye round measured why.)
--
-- WHY. Through transactions_tenant_employee_occurred_idx (00005) the lookup FILTERED the two clauses
-- during the scan, so every directionless row of the person's (a refused or an ignored tap -- type
-- NULL, transactions_refusal_has_no_direction) between the anchor and its neighbour was read and
-- thrown away, once per lookup. Those rows are the ones a person can multiply at will (the QR channel
-- has no counter; only the 60 s debounce and the rate limit stand in front, and an ignored tap is
-- still written -- CLAUDE.md §4.6). The directionless rows are not in this index at all, so a lookup
-- reads the neighbour and stops, however many of them lie between. Measured before / after on a lane
-- database (T157's close-out in docs/backlog.md carries the numbers and the load).
--
-- WHAT IT DOES NOT CHANGE. No result: an index is never a semantic. The previous binary
-- runs on this schema unchanged (the migrate Job runs before the rollout); 00005's index stays -- the
-- tap path (GetLastOpenTransaction's anti-join names `type = 'out'` without `NOT practice`, which the
-- planner cannot prove from it), the debounce and the panel lists read it.
--
-- A FILE OF ITS OWN, CONCURRENTLY, OUTSIDE A TRANSACTION (00070's rule; CLAUDE.md §6's hot-table rule):
-- transactions is written by every tap, and a plain CREATE INDEX holds SHARE on it for the build --
-- every tap's INSERT would queue behind it. CONCURRENTLY takes SHARE UPDATE EXCLUSIVE, which no DML
-- waits for; what it waits for itself is the transactions older than its snapshots (a long reader --
-- the 02:30 dump -- holds it, never a tap). No lock_timeout here, for 00070's reason: the build blocks
-- no request, and in a NO TRANSACTION file a session setting would outlive the statement on goose's
-- pooled connection (or land on another one).
--
-- RE-RUNNABLE: an interrupted CONCURRENTLY build leaves an INVALID index behind, and goose records a
-- NO TRANSACTION file only at its end -- so the Up first drops a leftover of that name (only this file
-- ever makes it) and builds it again. The Down drops it the same way, CONCURRENTLY: a plain DROP INDEX
-- takes ACCESS EXCLUSIVE on transactions.
--
-- The test walks that run later migrations' Downs inside a test transaction (internal/db's
-- opAtVersion, aiLaterMigrations) skip this file: CONCURRENTLY cannot run there, and an index names no
-- operator or identity object.

-- +goose Up
DROP INDEX CONCURRENTLY IF EXISTS public.transactions_tenant_employee_directed_idx;
CREATE INDEX CONCURRENTLY transactions_tenant_employee_directed_idx
    ON public.transactions (tenant_id, employee_id, occurred_at, id)
    WHERE type IS NOT NULL AND NOT practice;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.transactions_tenant_employee_directed_idx;
