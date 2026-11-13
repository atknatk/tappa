-- 00100 -- T76, step 3 of 3: a tags row is BORN IN STOCK. A BEFORE INSERT trigger refuses any row
-- that is not the shape the encode flow writes at ADR 0017 §5.1 step 3 (InsertUnassigned):
--
--   status 'unassigned' · location_id NULL · encoded_at NULL · last_ctr 0 · retired_at NULL ·
--   replaced_by NULL
--
-- Everything else a plaque becomes it becomes by an UPDATE, and every one of those has its gate:
-- the stamp (MarkTagEncoded; 00022 write-once), the mount (AssignTagToLocation's `encoded_at IS NOT
-- NULL`; 00025's tags_active_requires_recorded_encode for every role), the counter (AdvanceTagCounter;
-- 00013's tags_counter_monotonic), the retirement (RetireTagForReplacement). Before this file the
-- INSERT was the one door around all of them: 00025 refused the TRANSITION into 'active' without a
-- stamp but not a row BORN 'active' -- incident A-1's shape (on a wall, in service, never stamped,
-- 2026-09-24) -- and tappa_app holds table-level INSERT on tags (00004; backlog T16). 00090 (step 1)
-- closed the door by omission (DEFAULT 'unassigned'); this closes it for a row that SAYS 'active',
-- and for the other birth-time shortcuts (a counter already moved, a row born lost or retired).
--
-- THE PREDICATE IS THE WHOLE BIRTH SHAPE, ON PURPOSE, even where another guard already refuses part
-- of it: an 'unassigned' row with a location is also tags_unassigned_has_no_location's (00013), a
-- stamped row is also tags_encoded_at_not_settable_at_insert's (00022). The trigger is named so it
-- fires AFTER 00022's (BEFORE triggers fire in name order: tags_encoded_at_… < tags_is_…), so a
-- stamped INSERT keeps 00022's own message; CHECK constraints are evaluated after every BEFORE
-- trigger, so an 'unassigned' row with a location is refused here first. Neither overlap loosens
-- anything: drop either older guard and this one still holds the shape. created_at is not part of
-- the shape (a back-dated load date describes nothing a tap or a mount reads; seed.sql back-dates it
-- for the demo), and neither are the two key envelopes (their shape is 00021/00023's CHECKs').
--
-- WHO IT BINDS: every role. tappa_owner included -- a hand-typed INSERT at a psql prompt is refused
-- like the app's (00025's reasoning). A SUPERUSER can still switch ordinary triggers off for its own
-- session (session_replication_role = replica, what a data-only restore does): that is how the test
-- fixtures write incident A-1's legacy row (test/fixtures/plaque.go), and no product path does it.
-- A full restore loads every row before it creates any trigger (pg_dump writes
-- triggers in its post-data section -- the reason 00022's BEFORE INSERT trigger has never refused
-- seed.sql's stamped plaques when scripts/restore-chain-check.sh restores them, on every CI run).
--
-- EVERY PRODUCTION INSERT INTO tags, AND WHAT THIS DOES TO IT (read, 2026-11-11):
--   * internal/encode/rows.go DBRows.InsertUnassigned -> store.InsertUnassigned: literal
--     'unassigned', explicit NULL location_id, no encoded_at / last_ctr / retired_at / replaced_by
--     (defaults NULL, 0, NULL, NULL). Passes. db/queries/tags.sql's statement is identical on main
--     (04af4bc6), so the PREVIOUS BINARY passes too while the migrate Job runs before the rollout.
--   * internal/sandbox/plaque.go writePlaque -> the same store.InsertUnassigned, then MarkTagEncoded
--     and AssignTagToLocation. Passes.
--   * No function body in db/migrations inserts into tags (the `INSERT INTO tags` there are
--     comments: 00013, 00022, 00025, 00090). No script under scripts/ or deploy/ and no runbook
--     inserts into tags.
--   * Development and CI only: test/fixtures/seed.sql (scripts/seed.sh, tappa_owner) and
--     test/fixtures/restore-chain.sql (scripts/restore-chain-check.sh) load every plaque in stock
--     and then stamp and mount it, in this change. seed.sql's INSERT keeps ON CONFLICT (uid) DO
--     NOTHING: the trigger fires BEFORE the conflict is detected, and a row in the stock shape
--     passes it, so a second load still inserts nothing (T76's step-2-before-step-3 ordering).
--
-- SECURITY INVOKER, EXECUTE in its owner alone (00061; ADR 0028 §1), the body asserting where it is
-- bound (00099's shape): a copy bound to another table or event refuses rather than guarding nothing.
-- The message carries NEW.status and nothing else -- no uid, no tenant, no location.
--
-- LOCKS. CREATE TRIGGER takes SHARE ROW EXCLUSIVE on tags and DROP TRIGGER ACCESS EXCLUSIVE (read:
-- PostgreSQL's RemoveTriggerById); both conflict with every NFC tap's counter advance (ROW
-- EXCLUSIVE), so lock_timeout is the first statement of each section and the file
-- gives up with 55P03 rather than queue every tap behind its lock (00090's measurement on tags; the
-- Job's retry runs it again). Existing rows are not read: a BEFORE INSERT trigger has no history,
-- and the A-1 rows already in production stay what they are (ADR 0017 §5.2: repaired by Replace).
--
-- THE DOWN drops exactly what the Up created: the trigger, then its function.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
CREATE FUNCTION public.tappa_forbid_birth_outside_stock()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'INSERT'
       OR TG_RELID <> 'public.tags'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'tappa_forbid_birth_outside_stock: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    RAISE EXCEPTION 'a plaque row is born in stock on % (status unassigned, no location, unstamped, counter 0, never retired); this INSERT (status %) is refused -- stamp, mount and retire are UPDATEs',
        TG_TABLE_NAME, NEW.status
        USING ERRCODE = 'restrict_violation';
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.tappa_forbid_birth_outside_stock() FROM PUBLIC;

-- WHEN carries the predicate: InsertUnassigned's row never enters the function.
CREATE TRIGGER tags_is_born_in_stock
    BEFORE INSERT ON public.tags
    FOR EACH ROW
    WHEN (NEW.status IS DISTINCT FROM 'unassigned'
          OR NEW.location_id IS NOT NULL
          OR NEW.encoded_at IS NOT NULL
          OR NEW.last_ctr IS DISTINCT FROM 0
          OR NEW.retired_at IS NOT NULL
          OR NEW.replaced_by IS NOT NULL)
    EXECUTE FUNCTION public.tappa_forbid_birth_outside_stock();

-- +goose Down
SET LOCAL lock_timeout = '1s';

DROP TRIGGER IF EXISTS tags_is_born_in_stock ON public.tags;
DROP FUNCTION IF EXISTS public.tappa_forbid_birth_outside_stock();
