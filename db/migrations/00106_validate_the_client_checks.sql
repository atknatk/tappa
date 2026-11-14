-- 00106 -- M16-04: validate the two CHECKs 00105 added NOT VALID -- transactions_client_is_known (the
-- value set) and transactions_manual_has_no_client (a manual row carries none).
--
-- WHY A FILE OF ITS OWN (00044's and 00066's pattern and reasoning): goose runs each file in its own
-- transaction, and a lock is held to the end of the transaction that took it. Inside 00105 these
-- VALIDATEs would scan transactions under the ACCESS EXCLUSIVE its ALTER took -- no tap written for as
-- long as they scan. Here each takes SHARE UPDATE EXCLUSIVE on transactions, which a tap's INSERT (ROW
-- EXCLUSIVE) does not wait for; the scans cost time, not writes.
--
-- lock_timeout FIRST, in both sections (the hot-table rule, ADR 0032 §A.5): the Up waits only behind
-- another SHARE UPDATE EXCLUSIVE or stronger holder (a VACUUM, a concurrent index build, another
-- migration) and gives up with 55P03 rather than sit in the queue; the Down drops and adds the CHECKs
-- again under ACCESS EXCLUSIVE, briefly.
--
-- WHAT IT CANNOT FIND: every row older than 00105 has client NULL, which both CHECKs admit (they are
-- written to admit NULL -- 00105's header says why there is no "a tap must carry a client" CHECK), and
-- every row since was checked on its INSERT. A failure here would mean a row was written past a NOT
-- VALID constraint -- which PostgreSQL does not do -- or by a hand that disabled one; it stops the
-- migration, which is the right answer to either.
--
-- internal/db's txclient_test.go pins the split (00105's ALTER scans nothing, these statements do)
-- and the Down.

-- +goose Up
SET LOCAL lock_timeout = '1s';

ALTER TABLE transactions VALIDATE CONSTRAINT transactions_client_is_known;
ALTER TABLE transactions VALIDATE CONSTRAINT transactions_manual_has_no_client;

-- +goose Down
-- VALIDATE has no inverse statement, so the inverse is the state 00105 left: the same two CHECKs, by
-- the same text, NOT VALID. Dropping and adding a constraint again takes ACCESS EXCLUSIVE, briefly -- a
-- NOT VALID add scans nothing. The texts are 00105's verbatim (the Down test compares
-- pg_get_constraintdef before and after). Nothing is dropped that holds data, so this Down needs no
-- refusal.
SET LOCAL lock_timeout = '1s';

ALTER TABLE transactions
    DROP CONSTRAINT transactions_client_is_known,
    DROP CONSTRAINT transactions_manual_has_no_client,
    ADD CONSTRAINT transactions_client_is_known
        CHECK (client IS NULL OR client IN ('browser', 'app')) NOT VALID,
    ADD CONSTRAINT transactions_manual_has_no_client
        CHECK (channel <> 'manual' OR client IS NULL) NOT VALID;
