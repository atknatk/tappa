-- 00058 -- T130: a delivered or dead webhook delivery is deleted 90 days after it settled; a pending
-- one never is.
--
-- THE DECISION (the user's, 2026-10-18): webhook_deliveries' `delivered` and `dead` rows are deleted
-- 90 days after the delivery ended; a delivery still owed (`pending` -- queued, retrying, or waiting at
-- a closed gate) is NEVER deleted. site_punches' retention is not this file's.
--
-- NORMATIVE SOURCES: docs/adr/0028-dis-api-anahtar-akis-webhook.md §1 (the matrix: tappa_app's
-- column-listed reach; the trigger function's EXECUTE in its owner alone, its body asserting
-- TG_RELID), §6 (the table and its states), the "T130 uygulama notu"; CLAUDE.md §4.5 (tenant isolation
-- in every layer), §6 (the RLS five; a hot table's index in a file of its own -- 00059), §7.
--
-- WHICH CLOCK. "90 days after" is counted from the moment the delivery SETTLED -- arrived (delivered)
-- or was given up (dead) -- not from created_at: a delivery that waited weeks at a closed gate and
-- then failed must stay in the owner's log for 90 days after it failed. The table had no column for
-- that moment (delivered_at is the arrival only; a dead row's last try is nowhere -- next_attempt_at
-- is when the try was DUE, which a closed gate can push arbitrarily far before the try), so this file
-- adds one: settled_at. ITS ONE WRITER IS THE DATABASE: a BEFORE trigger sets it to clock_timestamp()
-- when a row leaves `pending` and to NULL when it goes back (a resend), and keeps it on every other
-- write; tappa_app holds no INSERT or UPDATE on it (00053's lesson: a column a security rule reads is
-- not the application's to write -- a backdated value would make a fresh row deletable at once).
--
-- THE ROWS ALREADY THERE. Their settling moment is unknown for a dead row (see above) and known for a
-- delivered one, but writing delivered_at into every delivered row REWRITES them under this file's
-- ACCESS EXCLUSIVE lock -- which every tap waits on (00049's triggers insert a delivery inside the
-- tap's transaction, under a 2 s lock_timeout). So the column is added with DEFAULT now() -- a
-- non-volatile default is stored once in the catalogue, no row is rewritten -- which dates EVERY
-- existing row at this migration; the pending ones (the live queue, small) are then set back to NULL
-- and the default is dropped (new rows are pending, NULL). A row that settled before this migration is
-- therefore kept 90 days from the migration: never less than the rule, at most its age today more.
--
-- THREE GUARDS, EACH ALONE ENOUGH (kuşak + kemer; each measured on its own in
-- internal/db/webhookretention_test.go):
--   1. the query (db/queries/webhooks.sql, DeleteSettledWebhookDeliveries): the explicit tenant_id,
--      state IN ('delivered', 'dead'), settled_at older than the retention, at most a batch;
--   2. the RESTRICTIVE policy below: a DELETE -- of any statement, a code bug's included -- removes a
--      row only of the context's tenant, settled and older than 90 days by the database's clock. A
--      restrictive policy is AND-ed with the permissive tenant_isolation one (00048), so the reach
--      can only shrink;
--   3. the trigger: tappa_app cannot date a row; only leaving `pending` starts the clock.
--
-- 🔴 lock_timeout 1 s, THE FIRST STATEMENT (deploy/README.md's rule for a migration that locks what a
-- tap waits on -- the feed triggers' 2 s): behind an open delivery write the ALTER gives up with 55P03,
-- the migrate Job fails, nothing is rolled out, and the file is run again at a quiet moment. Under the
-- lock the work is the catalogue changes and ONE UPDATE of the pending rows (measured in ADR 0028's
-- T130 note; the threshold is in deploy/README.md).

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.webhook_deliveries') IS NULL THEN
        RAISE EXCEPTION 'migration 00058 (T130) needs webhook_deliveries (00048). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_attribute
                WHERE attrelid = 'public.webhook_deliveries'::pg_catalog.regclass AND attname = 'settled_at' AND NOT attisdropped)
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc WHERE proname = 'webhook_delivery_settle')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_policy
                   WHERE polrelid = 'public.webhook_deliveries'::pg_catalog.regclass AND polname = 'webhook_deliveries_delete_settled')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.webhook_deliveries', 'DELETE') THEN
        RAISE EXCEPTION 'migration 00058 (T130) adds settled_at, its trigger, the delete policy and tappa_app''s DELETE on webhook_deliveries, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- 1. The settling moment. Every existing row is dated now (the catalogue's default, no rewrite); the
--    queue goes back to NULL; new rows start NULL.
ALTER TABLE public.webhook_deliveries ADD COLUMN settled_at timestamptz DEFAULT now();
UPDATE public.webhook_deliveries SET settled_at = NULL WHERE state = 'pending';
ALTER TABLE public.webhook_deliveries ALTER COLUMN settled_at DROP DEFAULT;

-- 2. The column's one writer. INSERT: a pending row (every row a trigger writes) has none; UPDATE OF
--    state or settled_at: a change of state starts the clock (leaving pending) or clears it (a resend
--    back to pending); any other write keeps the old value -- so not even a statement that names the
--    column (the owner's) moves it without a change of state. ROW-VALUE IS DISTINCT FROM is not needed:
--    state is NOT NULL.
-- +goose StatementBegin
CREATE FUNCTION public.webhook_delivery_settle()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_RELID IS DISTINCT FROM 'public.webhook_deliveries'::regclass
       OR TG_WHEN IS DISTINCT FROM 'BEFORE' OR TG_LEVEL IS DISTINCT FROM 'ROW'
       OR TG_OP NOT IN ('INSERT', 'UPDATE') THEN
        RAISE EXCEPTION 'webhook_delivery_settle: bound to the wrong table or event'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF TG_OP = 'INSERT' OR NEW.state <> OLD.state THEN
        NEW.settled_at := CASE WHEN NEW.state = 'pending' THEN NULL ELSE clock_timestamp() END;
    ELSE
        NEW.settled_at := OLD.settled_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.webhook_delivery_settle() FROM PUBLIC;

CREATE TRIGGER webhook_deliveries_settled_at
    BEFORE INSERT OR UPDATE OF state, settled_at ON public.webhook_deliveries
    FOR EACH ROW EXECUTE FUNCTION public.webhook_delivery_settle();

-- 3. The delete's reach, in the database: the context's tenant, settled, 90 days by the database's
--    clock. statement_timestamp() (stable): the bound is one value per statement, the index's
--    condition (00059), and the same clock the query's bound reads.
CREATE POLICY webhook_deliveries_delete_settled ON public.webhook_deliveries
    AS RESTRICTIVE
    FOR DELETE
    USING (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
           AND state IN ('delivered', 'dead')
           AND settled_at < statement_timestamp() - interval '90 days');

-- 4. tappa_app: reads the column (the sweep's WHERE, the panel's window) and deletes (the sweep).
--    No INSERT or UPDATE of settled_at -- the trigger is its writer.
GRANT SELECT (settled_at) ON public.webhook_deliveries TO tappa_app;
GRANT DELETE ON public.webhook_deliveries TO tappa_app;

-- 5. POSTCONDITION -- effective privilege, as the roles meet it (membership and PUBLIC counted); and
--    no pending row carries a date (the queue alone is read -- 00048's partial due index -- not the
--    table: every other row was dated by the default).
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_column_privilege('tappa_app', 'public.webhook_deliveries', 'settled_at', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.webhook_deliveries', 'settled_at', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.webhook_deliveries', 'INSERT')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.webhook_deliveries', 'TRUNCATE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.webhook_delivery_settle()', 'EXECUTE')
       OR NOT pg_catalog.has_table_privilege('tappa_app', 'public.webhook_deliveries', 'DELETE')
       OR EXISTS (SELECT 1 FROM public.webhook_deliveries WHERE state = 'pending' AND settled_at IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 00058: tappa_app''s reach on webhook_deliveries is not the matrix''s, or a pending delivery is dated. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
REVOKE DELETE ON public.webhook_deliveries FROM tappa_app;
DROP POLICY webhook_deliveries_delete_settled ON public.webhook_deliveries;
DROP TRIGGER webhook_deliveries_settled_at ON public.webhook_deliveries;
DROP FUNCTION public.webhook_delivery_settle();
-- The column's grant goes with it.
ALTER TABLE public.webhook_deliveries DROP COLUMN settled_at;
