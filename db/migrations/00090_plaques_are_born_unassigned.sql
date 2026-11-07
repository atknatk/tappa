-- 00090 -- T76, step 1 of 3: a tags row that does not name its status is born 'unassigned', not
-- 'active'. 00004:55 wrote DEFAULT 'active' when the lifecycle had three states and a plaque was
-- loaded straight onto a wall; 00013 added 'unassigned' (loaded, not yet mounted) and kept the old
-- default, so an INSERT that omits the status can still land the A-1 shape -- 'active', on a wall,
-- never stamped (00025's header; incident A-1, 2026-09-24). With this default the same INSERT is
-- either an honest 'unassigned' row (no location) or refused by 00013's
-- tags_unassigned_has_no_location (a location named): never 'active' by omission.
--
-- WHAT IT DOES NOT DO (T76 steps 2 and 3, left on the backlog line on purpose): it does not refuse an
-- INSERT that SAYS 'active'. That is a BEFORE INSERT trigger, and it waits for the fixtures to load
-- -> stamp -> mount, because a BEFORE INSERT trigger fires before ON CONFLICT DO NOTHING and would
-- break test/fixtures/seed.sql's re-run (00025's measurement).
--
-- THE OLD BINARY (the migrate Job runs before the rollout while the old pod serves). Read: the one
-- production INSERT into tags is InsertUnassigned (db/queries/tags.sql), which writes the literal
-- 'unassigned' and an explicit NULL location_id -- it does not consult the default, and db/queries
-- is identical on main (9b4aacbe). No function body inserts into tags (the two `INSERT INTO tags` in
-- db/migrations are comments, 00022 and 00025); test/fixtures/seed.sql names the status of every row
-- it loads. In production code InsertUnassigned is the only INSERT into tags; every fixture must name
-- the status (test/fixtures included -- a fixture that names a location_id and no status is refused
-- from this file on). Three test INSERTs relied on the old default
-- with a location (internal/db's tagsinventory_test.go and the two of rls_test.go's tags case) and now
-- say 'active'.
--
-- LOCKS. ALTER COLUMN ... SET DEFAULT takes ACCESS EXCLUSIVE on tags and scans nothing (a catalogue
-- change: existing rows keep their status, only a later INSERT that omits it reads the default).
-- Measured on PG 17.10 (tappa-m14-dbprivs-db, psql, rolled back): pg_locks shows AccessExclusiveLock
-- on tags inside the statement's transaction, and behind a transaction that holds ROW EXCLUSIVE on
-- tags (a tap's atomic counter advance in flight) it waited and lock_timeout ended the wait with
-- 55P03 at 1.003 s. tags is on every NFC tap's path, so lock_timeout comes first and the file gives up
-- rather than queue every later tap behind its lock -- the Job's retry (backoffLimit 2) runs it again.
-- One transaction, goose's own.
--
-- THE DOWN gives back 00004's DEFAULT 'active'. Nothing is read or rewritten; rows born 'unassigned'
-- under this default stay 'unassigned' (they are honest unmounted plaques either way).

-- +goose Up
SET LOCAL lock_timeout = '1s';

ALTER TABLE tags ALTER COLUMN status SET DEFAULT 'unassigned';

-- +goose Down
SET LOCAL lock_timeout = '1s';

ALTER TABLE tags ALTER COLUMN status SET DEFAULT 'active';
