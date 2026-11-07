-- 00089 -- T7: validate tags_aes_key_ref_is_kek_envelope, the CHECK (octet_length(aes_key_ref) = 44)
-- 00021 added NOT VALID. Every row written or updated since 00021 was checked (NOT VALID skips only
-- the initial scan -- 00021's test measured it binds every new row version, the owner's included);
-- the rows before it were never read by the schema. A row with another shape is a plaque the panel
-- calls encoded and the tap path answers 500 for, with no record written (§4.6). Production held
-- 0 violations of 13 rows on 2026-10-21 (backlog T7); the deploy re-reads the count first (the
-- report of the card names the query: a count, never a key).
--
-- WHAT THE UP DOES ON A DATABASE THAT STILL HAS ONE. The DO block counts the rows of another shape
-- and refuses with that number alone -- no uid, no byte of a key -- and the file changes nothing.
-- VALIDATE itself would refuse too, and its error names no row either (measured on PG 17.10: 23514
-- "check constraint ... is violated by some row", no DETAIL); the count is there so the message
-- says how many. In the deploy that is the migrate Job failing three times (backoffLimit 2): the
-- workflow reports "the migration Job failed; the deployment is NOT rolled out" and stops, the old
-- pod keeps serving on 00088 (00088 is its own committed file and the old binary runs on it). That
-- is the safe direction: a constraint the data does not meet is never claimed, and nothing is
-- rewritten -- a key is never touched by a migration (CLAUDE.md §4.7; plaque rows are never deleted).
--
-- LOCKS (measured on PG 17.10, tappa-m14-dbprivs-db). VALIDATE CONSTRAINT takes SHARE UPDATE
-- EXCLUSIVE on tags, which a tap's atomic counter advance (ROW EXCLUSIVE) does not conflict
-- with: with a transaction holding ROW EXCLUSIVE on tags the VALIDATE's transaction finished in
-- 11 ms, and an UPDATE tags under lock_timeout ran in 5 ms while a VALIDATE was held. It waits only
-- behind SHARE UPDATE EXCLUSIVE or stronger (a VACUUM, an index build, another migration): CONTROL,
-- behind a held SHARE UPDATE EXCLUSIVE it gave up with 55P03 at 1.006 s. The scan: 57 ms over
-- 101 077 rows; production has 13. lock_timeout comes first (00066's rule for a hot table); a 55P03
-- fails the Job and its retry runs the file again. One transaction, goose's own.
--
-- THE DOWN: VALIDATE has no inverse statement, so the Down restores 00021's state -- the same CHECK
-- by the same text, NOT VALID (internal/db's tagkeyshape_test.go compares pg_get_constraintdef).
-- Dropping and adding a constraint takes ACCESS EXCLUSIVE on tags, briefly (a NOT VALID add scans
-- nothing), behind the same lock_timeout.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
DECLARE
    n bigint;
BEGIN
    SELECT count(*) INTO n FROM public.tags WHERE octet_length(aes_key_ref) <> 44;
    IF n > 0 THEN
        RAISE EXCEPTION 'migration 00089: % tags rows carry an aes_key_ref that is not a 44-byte KEK envelope; tags_aes_key_ref_is_kek_envelope stays NOT VALID. Nothing was changed.', n
            USING ERRCODE = 'check_violation';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE tags VALIDATE CONSTRAINT tags_aes_key_ref_is_kek_envelope;

-- +goose Down
SET LOCAL lock_timeout = '1s';

ALTER TABLE tags
    DROP CONSTRAINT tags_aes_key_ref_is_kek_envelope,
    ADD CONSTRAINT tags_aes_key_ref_is_kek_envelope CHECK (octet_length(aes_key_ref) = 44) NOT VALID;
