-- 00066 -- M15-09 (ADR 0032 §A.5): validate the three CHECKs 00064 and 00065 added NOT VALID --
-- transactions_business_day_needs_a_direction (00064) and transactions_direction_source_is_known,
-- transactions_direction_source_needs_a_direction (00065).
--
-- WHY A FILE OF ITS OWN (00044's pattern and reasoning): goose runs each file in its own
-- transaction, and a lock is held to the end of the transaction that took it. Inside 00064 or
-- 00065 these VALIDATEs would scan transactions under the ACCESS EXCLUSIVE their ALTER took --
-- no tap written for as long as they scan (~195 ms at ~2 M rows for one CHECK, the M15 engine
-- lens's measurement). Here each takes SHARE UPDATE EXCLUSIVE on transactions, which a tap's
-- INSERT (ROW EXCLUSIVE) does not wait for; the scans cost time, not writes.
--
-- lock_timeout FIRST, in both sections (ADR 0032 §A.5's rule for every ALTER TABLE transactions
-- file): the Up waits only behind another SHARE UPDATE EXCLUSIVE or stronger holder (a VACUUM, a
-- concurrent index build, another migration), and gives up with 55P03 rather than sit in the
-- queue; the Down drops and adds the CHECKs again under ACCESS EXCLUSIVE, briefly.
--
-- WHAT IT CANNOT FIND: every row older than 00064/00065 has business_day_start and
-- direction_source NULL, which all three admit, and every row since was checked on its INSERT. A
-- failure here would mean a row was written past a NOT VALID constraint -- which PostgreSQL does
-- not do -- or by a hand that disabled one; it stops the migration, which is the right answer to
-- either.
--
-- internal/db's directionsource_test.go pins the split (00065's ALTER scans nothing, these
-- statements do) and the Downs.

-- +goose Up
SET LOCAL lock_timeout = '1s';

ALTER TABLE transactions VALIDATE CONSTRAINT transactions_business_day_needs_a_direction;
ALTER TABLE transactions VALIDATE CONSTRAINT transactions_direction_source_is_known;
ALTER TABLE transactions VALIDATE CONSTRAINT transactions_direction_source_needs_a_direction;

-- +goose Down
-- VALIDATE has no inverse statement, so the inverse is the state 00064 and 00065 left: the same
-- three CHECKs, by the same text, NOT VALID. Dropping and adding a constraint again takes ACCESS
-- EXCLUSIVE, briefly -- a NOT VALID add scans nothing. The texts are 00064's and 00065's verbatim
-- (the Down test compares pg_get_constraintdef before and after). Nothing is dropped that holds
-- data, so this Down needs no refusal.
SET LOCAL lock_timeout = '1s';

ALTER TABLE transactions
    DROP CONSTRAINT transactions_business_day_needs_a_direction,
    DROP CONSTRAINT transactions_direction_source_is_known,
    DROP CONSTRAINT transactions_direction_source_needs_a_direction,
    ADD CONSTRAINT transactions_business_day_needs_a_direction
        CHECK (business_day_start IS NULL OR type IS NOT NULL) NOT VALID,
    ADD CONSTRAINT transactions_direction_source_is_known
        CHECK (direction_source IN ('auto', 'employee')) NOT VALID,
    ADD CONSTRAINT transactions_direction_source_needs_a_direction
        CHECK (direction_source IS NULL OR type IS NOT NULL) NOT VALID;
