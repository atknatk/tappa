-- +goose NO TRANSACTION
-- 00094 -- T157 (third-eye round): transactions_tenant_employee_checkin_idx, a person's non-practice
-- CHECK-INS in (occurred_at, id) order.
--
-- WHAT READS IT. ListCheckoutsWithoutCheckIn's `i` (db/queries/anomalies.sql): "the latest `in` before
-- this checkout", `type = 'in' AND NOT practice`, ORDER BY (occurred_at, id) DESC LIMIT 1. The predicate
-- below is those two clauses word for word, so the planner proves it, and the probe reads ONE entry --
-- the latest check-in -- whatever lies between it and the checkout.
--
-- WHY 00091 WAS NOT ENOUGH (third-eye ORTA-1, measured). Through 00091's directed index the probe walked
-- back to the latest `in` FILTERING every `out` on the way, and a run of consecutive checkouts is
-- buildable: an employee may choose `out` with no open check-in (ADR 0032 §B.3, tap.Decision's
-- MissingIn), the 60 s debounce is the only brake, and tappa_app has no statement_timeout. On a lane
-- database, 9 900 consecutive outs inside one business day plus 30 000 before the week: the `i` probes
-- read 2 709 380 shared buffers (4 905 rows filtered per probe, 10 032 probes); with this index each
-- probe reads its one entry (numbers in T157's close-out, docs/backlog.md). A range bound on the day in
-- the subquery would bound the walk too, but then the planner keeps 00005's index and filters there.
--
-- THE PRICE. Every non-practice `in` -- about half of all directed taps, and every manual check-in --
-- writes one more index entry: a third index on transactions for check-ins (00005's, 00091's, this),
-- small (one entry per check-in, no directionless row, no checkout). Accepted by the coordinator's
-- decision (option (a), 2026-11-08) over an unbounded read per checkout.
--
-- WHAT IT DOES NOT CHANGE. No query text, no result. The previous binary runs on this schema unchanged.
--
-- A FILE OF ITS OWN, CONCURRENTLY, OUTSIDE A TRANSACTION -- 00091's header, word for word: no request
-- waits for the build (SHARE UPDATE EXCLUSIVE), the build waits for older transactions, no lock_timeout
-- (a session setting would outlive the statement on goose's pooled connection), re-runnable (a leftover
-- INVALID index of this name is dropped first), the Down drops it CONCURRENTLY too, and the test walks
-- over later migrations' Downs skip this file.
--
-- NUMBERED 00094, NOT 00093: 00093 is branch m14-opname's; the two branches ship in ONE deploy
-- (deploy/README.md's T157 note says why -- goose refuses a missing lower version).

-- +goose Up
DROP INDEX CONCURRENTLY IF EXISTS public.transactions_tenant_employee_checkin_idx;
CREATE INDEX CONCURRENTLY transactions_tenant_employee_checkin_idx
    ON public.transactions (tenant_id, employee_id, occurred_at, id)
    WHERE type = 'in' AND NOT practice;

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS public.transactions_tenant_employee_checkin_idx;
