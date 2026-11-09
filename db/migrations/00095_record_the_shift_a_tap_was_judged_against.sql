-- 00095 -- T152 (ADR 0042): the shift a directed tap was judged against, frozen on its record.
--
-- WHY. Lateness was computed at READ time: ListWorkedShiftEvents joined today's locations and
-- departments rows, so editing a venue's or a department's hours rewrote the late count of every
-- past week (the records did not change; the report did). The tap path already resolves the shift
-- (internal/domain/checkin gather: the DEPARTMENT's when it has one, otherwise the TAPPED venue's --
-- CLAUDE.md §5, Q17) and hands it to the engine; this file gives that answer a place on the row, the
-- same move 00064 made for the business day (a cutoff can change later; a reader that recomputed the
-- past from today's setting would change last week).
--
-- WHAT. Five nullable columns, no default, no back-fill (§4.3):
--   shift_source     'department' | 'location' | 'none' -- where the shift came from, or 'none' when
--                    the tap was judged against no shift (lateness not computed). NULL = not recorded:
--                    every row before this file, every manual row, every undirected decision.
--   shift_start, shift_end, shift_overnight
--                    the wall clock, copied from the source row (00002's encoding: `time` + the
--                    overnight flag for the Rusty Bar 18:00-02:00 shape).
--   shift_timezone   the IANA zone the wall clock was read in (the employer's tenants.timezone, or a
--                    holding peer venue's) -- a wall clock without its zone is no instant, and the
--                    tenant's zone is editable too.
-- Written by the tap's INSERT only (InsertTransaction), on a DIRECTED nfc/qr decision -- the rule
-- business_day_start and direction_source already follow. The three manual statements do not name
-- the columns: a manager's row declares an occurred_at that may lie before the shift was last
-- edited, so the shift in force when it is TYPED is not the one in force when it HAPPENED (ADR 0042).
--
-- THE CHECK IS THE SECOND SAFETY, NOT THE FIRST (00064's arrangement): insertParams forces the
-- shape in code; a slip that reached the CHECK would refuse the INSERT with 23514 and lose the record
-- (§4.6). It is NOT VALID and stays so: ADD COLUMN with no default leaves every existing row NULL,
-- which the CHECK's first arm admits, so a VALIDATE would read every row of the hottest table to
-- prove what the ALTER already guarantees. NOT VALID is still enforced on every new row. Each arm
-- that names a source states `shift_source IS NOT NULL` first: without it a row with values and NO
-- source made the last arm NULL rather than false, and a CHECK that answers NULL admits the row
-- (measured: the "values with no source" case landed until the guard was written).
--
-- PRIVILEGES (ADR 0038's rule): tappa_app's INSERT on transactions is a column list (00088), so the
-- five columns are granted here by name, and the DO block holds the result to "every column but id
-- and created_at", as 00088's does. tappa_app keeps the table-level SELECT (covers them) and no
-- UPDATE (§4.3). No other role reaches a new column: their grants on transactions are column lists
-- (tappa_feedwriter 00049, tappa_holdingdefiner 00046/00067 -- privcatalog_test.go).
--
-- THE OLD BINARY (expand/contract: the migrate Job runs before the rollout). Its four INSERTs name
-- none of the five columns, so they keep inserting what they are granted and the new columns read
-- NULL -- "not recorded", the old report's own answer. Its SELECTs name their columns (no SELECT *
-- in db/queries), so a wider row changes none of its scans.
--
-- LOCKS. ADD COLUMN without a default and ADD CONSTRAINT ... NOT VALID are catalogue changes under a
-- brief ACCESS EXCLUSIVE; lock_timeout is the first statement (the hot-table rule, 00064-00066,
-- 00088), so behind a long reader the file gives up with 55P03 and changes nothing.
--
-- THE DOWN REFUSES WHILE A ROW CARRIES A SNAPSHOT (00043, 00064, 00065's precedent): once a venue's
-- hours have changed the snapshot cannot be recomputed, and records are immutable. The way back after
-- the first tap is the binary, not the schema (the old binary neither writes nor reads the columns).

-- +goose Up
SET LOCAL lock_timeout = '1s';

ALTER TABLE transactions
    ADD COLUMN shift_source text,
    ADD COLUMN shift_start time,
    ADD COLUMN shift_end time,
    ADD COLUMN shift_overnight boolean,
    ADD COLUMN shift_timezone text,
    ADD CONSTRAINT transactions_shift_snapshot_shape CHECK (
        (shift_source IS NULL
            AND shift_start IS NULL AND shift_end IS NULL AND shift_overnight IS NULL AND shift_timezone IS NULL)
        OR (shift_source IS NOT NULL AND shift_source = 'none' AND type IS NOT NULL AND channel IN ('nfc', 'qr')
            AND shift_start IS NULL AND shift_end IS NULL AND shift_overnight IS NULL AND shift_timezone IS NULL)
        OR (shift_source IS NOT NULL AND shift_source IN ('department', 'location') AND type IS NOT NULL AND channel IN ('nfc', 'qr')
            AND shift_start IS NOT NULL AND shift_end IS NOT NULL AND shift_overnight IS NOT NULL AND shift_timezone IS NOT NULL)
    ) NOT VALID;

GRANT INSERT (shift_source, shift_start, shift_end, shift_overnight, shift_timezone) ON transactions TO tappa_app;

-- +goose StatementBegin
DO $$
DECLARE
    wrong text;
BEGIN
    SELECT string_agg(a.attname, ', ' ORDER BY a.attnum) INTO wrong
      FROM pg_catalog.pg_attribute a
     WHERE a.attrelid = 'public.transactions'::regclass AND a.attnum > 0 AND NOT a.attisdropped
       AND has_column_privilege('tappa_app', 'public.transactions', a.attname, 'INSERT')
           = (a.attname IN ('id', 'created_at'));
    IF wrong IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00095: tappa_app''s INSERT on transactions is wrong on: %. It must reach every column but id and created_at. Nothing was changed.', wrong
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF has_table_privilege('tappa_app', 'public.transactions', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00095: tappa_app holds a table-level INSERT on transactions. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- transactions is LOCKED BEFORE IT IS LOOKED AT (00064/00065's reason): between an unlocked EXISTS
-- and the ALTER a tap could commit a row with a snapshot, and the DROP COLUMN would then erase it.
SET LOCAL lock_timeout = '1s';

LOCK TABLE transactions IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.transactions WHERE shift_source IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 00095 Down: transactions holds rows with a shift snapshot (the shift a tap was judged against); it cannot be recomputed and records are immutable. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- The column grants go with the columns.
ALTER TABLE transactions
    DROP CONSTRAINT transactions_shift_snapshot_shape,
    DROP COLUMN shift_timezone,
    DROP COLUMN shift_overnight,
    DROP COLUMN shift_end,
    DROP COLUMN shift_start,
    DROP COLUMN shift_source;
