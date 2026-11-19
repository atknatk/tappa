-- 00113 -- M16-19: validate the four channel CHECKs 00112 added NOT VALID that were VALID before it --
-- transactions_channel_check, transactions_site_by_channel, punch_events_channel and
-- site_punches_channel_check. transactions_shift_snapshot_shape stays NOT VALID, as 00095 left it
-- (its header: a VALIDATE would read the hottest table to prove what the ALTER already guarantees).
--
-- WHY A FILE OF ITS OWN (00044's, 00066's and 00110's pattern and reasoning): goose runs each file in
-- its own transaction, and a lock is held to the end of the transaction that took it. Inside 00112
-- these VALIDATEs would scan transactions, punch_events and site_punches under the ACCESS EXCLUSIVE
-- its ALTERs took -- no tap written for as long as they scan. Here each takes SHARE UPDATE EXCLUSIVE
-- on its table, which a tap's INSERT (ROW EXCLUSIVE) does not wait for; the scans cost time, not
-- writes.
--
-- lock_timeout FIRST, in both sections (the hot-table rule, ADR 0032 §A.5): the Up waits only behind
-- another SHARE UPDATE EXCLUSIVE or stronger holder (a VACUUM, a concurrent index build, another
-- migration) and gives up with 55P03 rather than sit in the queue; the Down drops and adds the CHECKs
-- again under ACCESS EXCLUSIVE, briefly, the three tables locked first in a tap's order (00112's).
--
-- WHAT IT CANNOT FIND: every row older than 00112 satisfied the narrower CHECK it replaced, and each
-- new set only widens; every row since was checked on its INSERT. A failure here would mean a row was
-- written past a NOT VALID constraint -- which PostgreSQL does not do -- or by a hand that disabled
-- one; it stops the migration, which is the right answer to either.

-- +goose Up
SET LOCAL lock_timeout = '1s';

ALTER TABLE transactions VALIDATE CONSTRAINT transactions_channel_check;
ALTER TABLE transactions VALIDATE CONSTRAINT transactions_site_by_channel;
ALTER TABLE punch_events VALIDATE CONSTRAINT punch_events_channel;
ALTER TABLE site_punches VALIDATE CONSTRAINT site_punches_channel_check;

-- +goose Down
-- VALIDATE has no inverse statement, so the inverse is the state 00112 left: the same four CHECKs, by
-- the same text, NOT VALID. A NOT VALID add scans nothing. Nothing is dropped that holds data, so this
-- Down needs no refusal (00112's Down is the one that refuses).
SET LOCAL lock_timeout = '1s';

LOCK TABLE public.transactions, public.punch_events, public.site_punches IN ACCESS EXCLUSIVE MODE;

ALTER TABLE transactions
    DROP CONSTRAINT transactions_channel_check,
    DROP CONSTRAINT transactions_site_by_channel,
    ADD CONSTRAINT transactions_channel_check
        CHECK (channel IN ('nfc', 'qr', 'qr_live', 'manual')) NOT VALID,
    ADD CONSTRAINT transactions_site_by_channel
        CHECK (site_tenant_id IS NULL
               OR (channel IN ('nfc', 'qr') AND site_tag_uid IS NOT NULL)
               OR (channel = 'manual' AND site_location_id IS NOT NULL AND site_tag_uid IS NULL)
               OR (channel = 'qr_live' AND site_location_id IS NOT NULL AND site_tag_uid IS NULL)) NOT VALID;

ALTER TABLE punch_events
    DROP CONSTRAINT punch_events_channel,
    ADD CONSTRAINT punch_events_channel CHECK (channel IN ('nfc', 'qr', 'qr_live')) NOT VALID;

ALTER TABLE site_punches
    DROP CONSTRAINT site_punches_channel_check,
    ADD CONSTRAINT site_punches_channel_check CHECK (channel IN ('nfc', 'qr', 'qr_live', 'manual')) NOT VALID;
