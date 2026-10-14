-- 00044 -- M11 API-1b: validate the six site constraints 00043 added NOT VALID (ADR 0027 §4:
-- "eşik 2 sn, aşılırsa CHECK/FK NOT VALID + ayrı VALIDATE").
--
-- WHY A FILE OF ITS OWN: goose runs each file in its own transaction, and a lock is held to the
-- end of the transaction that took it. Inside 00043 these VALIDATEs would have run under the
-- ACCESS EXCLUSIVE its ALTER TABLE took -- no tap written for as long as they scan. Here each
-- takes SHARE UPDATE EXCLUSIVE on transactions (and ROW SHARE on locations / tags for a key),
-- which a tap's INSERT does not wait for. The scan itself is what 00043's note measured (2 s at
-- about 2 000 000 rows); it now costs only time, not writes.
--
-- WHAT IT CANNOT FIND: every row older than 00043 has the three site columns NULL, which all six
-- admit, and every row since was checked on its INSERT. A failure here would mean a row was
-- written past a NOT VALID constraint -- which PostgreSQL does not do -- or by a hand that
-- disabled one; it stops the migration, which is the right answer to either.
--
-- TestHoldingMigration_AlterWithinTwoSeconds pins the split (00043's ALTER scans nothing, these
-- statements do); TestHolding00043And00044_DownGiveBackAndUpTakesItAgain pins the Down.

-- +goose Up
ALTER TABLE transactions VALIDATE CONSTRAINT transactions_site_needs_its_tenant;
ALTER TABLE transactions VALIDATE CONSTRAINT transactions_site_is_another_tenant;
ALTER TABLE transactions VALIDATE CONSTRAINT transactions_site_excludes_the_local_venue;
ALTER TABLE transactions VALIDATE CONSTRAINT transactions_site_by_channel;
ALTER TABLE transactions VALIDATE CONSTRAINT transactions_site_location_fk;
ALTER TABLE transactions VALIDATE CONSTRAINT transactions_site_tag_fk;

-- +goose Down
-- VALIDATE has no inverse statement, so the inverse is the state 00043 left: the same six
-- constraints, by the same text, NOT VALID. Dropping and adding a constraint again takes ACCESS
-- EXCLUSIVE, briefly -- a NOT VALID add scans nothing. The texts are 00043's verbatim (the Down
-- test compares pg_get_constraintdef before and after).
ALTER TABLE transactions
    DROP CONSTRAINT transactions_site_needs_its_tenant,
    DROP CONSTRAINT transactions_site_is_another_tenant,
    DROP CONSTRAINT transactions_site_excludes_the_local_venue,
    DROP CONSTRAINT transactions_site_by_channel,
    DROP CONSTRAINT transactions_site_location_fk,
    DROP CONSTRAINT transactions_site_tag_fk,
    ADD CONSTRAINT transactions_site_location_fk FOREIGN KEY (site_location_id, site_tenant_id)
        REFERENCES locations (id, tenant_id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT transactions_site_tag_fk FOREIGN KEY (site_tag_uid, site_tenant_id)
        REFERENCES tags (uid, tenant_id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT transactions_site_needs_its_tenant
        CHECK (site_tenant_id IS NOT NULL OR (site_location_id IS NULL AND site_tag_uid IS NULL)) NOT VALID,
    ADD CONSTRAINT transactions_site_is_another_tenant
        CHECK (site_tenant_id IS NULL OR site_tenant_id <> tenant_id) NOT VALID,
    ADD CONSTRAINT transactions_site_excludes_the_local_venue
        CHECK (site_tenant_id IS NULL OR (location_id IS NULL AND tag_uid IS NULL)) NOT VALID,
    ADD CONSTRAINT transactions_site_by_channel
        CHECK (site_tenant_id IS NULL
               OR (channel IN ('nfc', 'qr') AND site_tag_uid IS NOT NULL)
               OR (channel = 'manual' AND site_location_id IS NOT NULL AND site_tag_uid IS NULL)) NOT VALID;
