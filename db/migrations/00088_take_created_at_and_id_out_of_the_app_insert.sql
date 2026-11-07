-- 00088 -- T163: tappa_app may no longer write transactions.created_at or transactions.id. Its INSERT on
-- the table goes from table-wide (00005:194) to a column list: every column but those two.
--
-- WHY. Two readers trust created_at as the DATABASE's word for when a row was written, never a
-- caller's: the debounce's server leg (ADR 0006; db/queries/transactions.sql
-- SecondsSinceLastRecordedTap, `clock_timestamp() - created_at`, ORDER BY created_at) and M15's
-- "written later" W (ADR 0032 §A.6, the checkout classification). 00068's decision trigger bounds an
-- approval by it too. ADR 0032 §A.6 pinned "created_at has one writer, the database" with a source
-- scan (internal/domain/ledger/createdat_test.go) and called that scan a net, not a lock: a process
-- running arbitrary SQL as tappa_app could still back-date a predecessor's created_at and age it past
-- the 60 s brake. This file is the lock. id is the same kind of column -- the database draws it
-- (DEFAULT gen_random_uuid(), 00005), nothing in the product names it, and a caller that could would
-- get a cross-tenant existence oracle on a guessed or leaked id (23505 on transactions_pkey) for
-- nothing in return. 00068 gave punch_claims and punch_claim_decisions the same shape at birth.
--
-- WHY A COLUMN GRANT AND NOT A TRIGGER THAT OVERWRITES created_at (T163's two proposals, measured on
-- goose 87, ADR 0038):
--   * the trigger would rewrite the value silently. 25 fixture INSERTs in 17 test files wrote a chosen
--     created_at as tappa_app on purpose (an aged predecessor, a late manual row -- measured: the ones
--     this file turned 42501); under a trigger they would keep running with now() and assert on a row
--     shape they no longer have. Under the grant they are 42501 -- loud -- and they moved to the owner,
--     who can still write a past created_at (a trigger would bind the owner too, leaving no honest way
--     to build an aged row at all). 12 more in 4 files named id and now read it back (RETURNING id);
--   * now() in a trigger is what DEFAULT now() already writes, so it buys nothing over the grant for an
--     honest writer; clock_timestamp() would change what the debounce's DB-age leg measures (ADR 0006's
--     "written, not done" note: a row written after W seconds of lock wait) and M15's W ordering -- a
--     behaviour change this card is not;
--   * the grant is the shape §6 and the other roles on this table already have (tappa_feedwriter 00049,
--     tappa_holdingdefiner 00046: column lists), and it fails on the safe side: a future column that is
--     not granted makes the tap's INSERT 42501 in CI, which every checkin DB test exercises, and
--     internal/db's createdatgrant_test.go holds the list to the table minus (id, created_at).
--
-- THE OLD BINARY (expand/contract: the migrate Job runs before the rollout while the old pod serves).
-- Read, not guessed: db/queries is identical on main (9b4aacbe) and here, and its four INSERTs into
-- transactions name, of the two columns, neither --
--   InsertTransaction                   30 columns: exactly the list below
--   InsertManualTransaction             13 of them
--   InsertManualSiteTransaction         15 of them
--   InsertManualTransactionAtAnchor     13 of them
-- and every one RETURNs id and created_at, which the table-level SELECT (kept) covers. No function
-- body inserts into transactions (pg_proc read; the feed and holding triggers write other tables).
-- createdatgrant_test.go runs the four statements' column lists against the catalogue.
--
-- THE FORM. A table-level REVOKE also clears column-level grants (00087's measurement), so the Down's
-- REVOKE leaves nothing of the list behind. The DO block checks the result the way the tests do --
-- has_column_privilege over EVERY column, so a membership or a PUBLIC grant that still reaches id or
-- created_at is caught here and the file changes nothing.
--
-- LOCKS. GRANT and REVOKE take no lock on transactions (measured on PG 17.10: pg_locks shows none for
-- the relation inside the statement's transaction, and both returned in under 5 ms while another
-- transaction held ACCESS EXCLUSIVE on it -- a SELECT under the same lock_timeout gave 55P03). The
-- lock_timeout is the hot-table rule all the same (ADR 0032 §A.5, 00066, 00087), so a stuck catalogue
-- row fails this file fast instead of queueing every tap behind it. One transaction, goose's own.
--
-- THE DOWN gives back 00005's table-wide INSERT. The binary this file ships with names neither column,
-- so the Down is a shape of the catalogue, not a caller's need.

-- +goose Up
SET LOCAL lock_timeout = '1s';

REVOKE INSERT ON transactions FROM tappa_app;
GRANT INSERT (
    tenant_id, employee_id, location_id, department_id, tag_uid, ctr, type, occurred_at, source_ip,
    ip_match, gps_lat, gps_lng, gps_match, sun_valid, trust, verdict, note, channel, entered_by,
    practice, queued, policy_version_id, matched_sid, policy_layer, policy_context, site_tenant_id,
    site_location_id, site_tag_uid, business_day_start, direction_source
) ON transactions TO tappa_app;

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
        RAISE EXCEPTION 'migration 00088: tappa_app''s INSERT on transactions is wrong on: %. It must reach every column but id and created_at. Nothing was changed.', wrong
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF has_table_privilege('tappa_app', 'public.transactions', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00088: tappa_app still holds a table-level INSERT on transactions. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';

REVOKE INSERT ON transactions FROM tappa_app;
GRANT INSERT ON transactions TO tappa_app;
