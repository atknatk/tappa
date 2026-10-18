-- 00059 -- T130: the retention sweep's index, webhook_deliveries_tenant_settled_idx.
--
-- WHAT READS IT. internal/webhook/delivery's sweep (db/queries/webhooks.sql,
-- DeleteSettledWebhookDeliveries) takes, for ONE business, at most a batch of its settled deliveries
-- older than the retention, oldest first. tenant_id leads (CLAUDE.md §6's index shape); settled_at is
-- the range and the order; the predicate is the query's own state clause, so the planner proves it
-- and the index holds the settled rows alone -- the live queue (pending) is 00048's
-- webhook_deliveries_tenant_due_idx. Without it the sweep would walk the business's rows by
-- webhook_deliveries_tenant_created_idx (created_at is not settled_at) or the whole table; with it a
-- sweep that finds nothing to delete -- every sweep in steady state -- reads one index page (the
-- plan pin is internal/db's TestWebhookRetention_TheSweepReadsTheSettledIndex).
--
-- A FILE OF ITS OWN (CLAUDE.md §6: a hot table's index is not in a migration a Down walk enters --
-- 00058's Down is walked by internal/db's feed tests). webhook_deliveries is written on every tap that
-- has an endpoint (00049's triggers, in the tap's transaction).
--
-- Built inside goose's transaction (not CONCURRENTLY): it takes a SHARE lock on webhook_deliveries for
-- the build, so a tap whose trigger writes a delivery waits for it -- under the trigger's 2 s
-- lock_timeout. MEASURED (ADR 0028's T130 note, a disposable PostgreSQL 17): the build is proportional
-- to the settled rows; the size past which this needs CONCURRENTLY in a migration of its own (goose
-- NO TRANSACTION) is in deploy/README.md. Production's table is a pilot's (days old at T130).
--
-- 🔴 lock_timeout 1 s, THE FIRST STATEMENT (00054's rule, deploy/README.md): behind an open delivery
-- write the build gives up with 55P03 instead of queueing every later tap behind its SHARE lock.

-- +goose Up
SET LOCAL lock_timeout = '1s';
CREATE INDEX webhook_deliveries_tenant_settled_idx ON public.webhook_deliveries (tenant_id, settled_at)
    WHERE state IN ('delivered', 'dead');

-- +goose Down
SET LOCAL lock_timeout = '1s';
DROP INDEX public.webhook_deliveries_tenant_settled_idx;
