-- 00065 -- M15-09 (ADR 0032 §B.4, §A.5, "Geri alma"): who chose the direction of a tap.
--
-- WHAT. transactions.direction_source: 'auto' when the engine's direction was recorded,
-- 'employee' when the person asked for the other one and that was recorded (ADR 0032 §B.3's
-- invariant: 'employee' <=> the recorded direction differs from the D the engine computed under
-- the person's lock). Written once, by the tap's INSERT (M15-11's insertParams), never updated
-- (CLAUDE.md §4.3). Until then every caller passes NULL.
--
-- NULL IS A STATE, NOT A GAP. Every row before this column -- and every row M15-11's binary does
-- not yet write -- carries NULL, and this migration back-fills nothing (§4.3; ADR 0032's rejected
-- alternative "Tarihsel satırlara direction_source='auto' doldurmak": it would add a claim to
-- immutable rows). The reader rule: on a DIRECTED nfc/qr row, NULL means "before M15, the engine's
-- choice" -- one Go helper says so (tap.RecordedDirectionSource), so no reader spells it twice.
-- On a manual row the column is NULL and means nothing: the direction is the manager's, which
-- the channel and entered_by already say -- there is no 'manager' value, and the CHECK below
-- refuses one. On an undirected row (reject, ignored) there is no direction to have a source.
--
-- NO DEFAULT. A DEFAULT 'auto' would write the claim on every row the binary does not name the
-- column for -- the two manual statements, a hand-written INSERT -- and would turn this ADD
-- COLUMN from a catalogue change into a stored default the old rows read back as 'auto'.
--
-- THE TWO CHECKS ARE THE SECOND SAFETY (ADR 0032 §A.5, the same arrangement as 00064's
-- business_day_start): the code forces NULL for an undirected decision and for the manual
-- channel; a slip that reached a CHECK would refuse the INSERT with 23514 and lose the record
-- (§4.6). They are added NOT VALID here and VALIDATEd in a file of their own (00066 -- 00044's
-- pattern: a validating ADD CONSTRAINT CHECK scans every row under the ACCESS EXCLUSIVE this
-- ALTER takes; ADD COLUMN with no default scans nothing). Historical rows are NULL and pass both
-- either way: NOT VALID is chosen for the lock, not for them. NOT VALID still binds every new
-- row (measured in internal/db's directionsource_test.go).
--
-- THE FIRST STATEMENT IS lock_timeout (00037:52, 00038:39, ADR 0032 §A.5): the ALTER takes
-- ACCESS EXCLUSIVE on the table every tap writes. Behind a long reader (the nightly dump) the
-- file gives up with 55P03 -- nothing changes, run it again at a quiet moment -- instead of
-- queueing every later tap behind its lock. One table, so no lock order to keep.
--
-- PRIVILEGES, MEASURED AT 00064 (has_column_privilege, tappa-m15-db): tappa_app holds SELECT and
-- INSERT on transactions table-wide (00005) and nothing else, which the new column falls under --
-- no grant. Every other role's reach on transactions is a column list (tappa_feedwriter's 00049,
-- tappa_holdingdefiner's 00046), so a new column is outside it: the feed writers do not read the
-- column until M15-13 names it. internal/db's m11schema_test.go matrix carries the result.

-- +goose Up
SET LOCAL lock_timeout = '1s';

ALTER TABLE transactions
    ADD COLUMN direction_source text,
    ADD CONSTRAINT transactions_direction_source_is_known
        CHECK (direction_source IN ('auto', 'employee')) NOT VALID,
    ADD CONSTRAINT transactions_direction_source_needs_a_direction
        CHECK (direction_source IS NULL OR type IS NOT NULL) NOT VALID;

-- +goose Down
-- 🔴 A DOWN THAT WOULD ERASE WHO CHOSE A DIRECTION REFUSES (CLAUDE.md §4.3, §4.6; ADR 0032
-- "Geri alma"; the 00043 and 00064 precedent). direction_source cannot be recomputed at all --
-- the engine's D at the moment of the tap is gone -- so while a row carries one this Down stops
-- and changes nothing. The way back after the first such row is the binary, not the schema: the
-- old code neither writes nor reads the column, and the CHECKs admit NULL.
--
-- transactions is LOCKED BEFORE IT IS LOOKED AT: between an unlocked EXISTS and the ALTER a tap
-- could commit a row with a source, and the ALTER would then drop it. ACCESS EXCLUSIVE is what
-- the ALTER takes anyway; taking it first only moves it ahead of the check (whose scan then runs
-- under it -- a Down is run at a quiet moment, and lock_timeout bounds only the wait).
SET LOCAL lock_timeout = '1s';

LOCK TABLE transactions IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.transactions WHERE direction_source IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 00065 Down: transactions holds rows with a direction_source (who chose the direction of a tap); it cannot be recomputed and records are immutable. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE transactions
    DROP CONSTRAINT transactions_direction_source_needs_a_direction,
    DROP CONSTRAINT transactions_direction_source_is_known,
    DROP COLUMN direction_source;
