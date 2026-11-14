-- 00105 -- M16-04 (ADR 0045 §2.4): which client wrote a tap.
--
-- WHAT. transactions.client: 'browser' when the tap came through the tap page (POST /api/checkin),
-- 'app' when it came through the native app's signed surface (/app/v1, M16-07). Written once, by the
-- tap's INSERT (internal/domain/checkin insertParams), never updated (CLAUDE.md §4.3). The value is
-- DERIVED ON THE SERVER from which endpoint accepted the request -- never a body field, a header or a
-- user agent (ADR 0045 §2.4, the rule ADR 0004 §8 applies to the channel).
--
-- NULL IS A STATE, NOT A GAP. Every row before this column carries NULL, and this file back-fills
-- nothing (§4.3: it would add a claim to immutable rows -- 00065's rejected alternative, the same
-- reason). NULL means "no client recorded": every row before 00105, every row the previous binary
-- writes during the rollout, every manual row (a manager's record is not a tap: no client touched a
-- plaque) and every row no client wrote (the API sandbox's simulated tap, ADR 0028 §11).
--
-- NO DEFAULT. A DEFAULT 'browser' would write the claim on every row the binary does not name the
-- column for -- the three manual statements, a hand-written INSERT, the previous binary's tap -- and
-- would turn this ADD COLUMN from a catalogue change into a stored default the old rows read back as
-- 'browser'.
--
-- TWO CHECKS, AND BOTH ADMIT NULL (the M16-04 card; 00065:22-29 and :57's reasoning):
--   transactions_client_is_known       the value set: NULL, 'browser' or 'app', nothing else;
--   transactions_manual_has_no_client  a manual row carries none (a manager's record is not a tap).
-- The second is the database's half of clientColumn's manual rule (the M16-04 security review): the
-- Go write path forces NULL there, and a slip would now be 23514 instead of a false claim on an
-- immutable row. It is safe during the rollout: the previous binary writes NULL everywhere, and every
-- historical row is NULL. There is deliberately NO "a tap channel must carry a client" CHECK:
-- a NOT VALID CHECK still binds every NEW row, so in the window between this migration and the new
-- pod's rollout the previous binary -- which does not name the column -- would have every NFC and QR
-- tap refused with 23514, and a refused INSERT is a lost record (§4.6). Its VALIDATE (00106) would also
-- fail on every database with history. That new rows carry a value is proved in code, not here: the
-- Go tests (browser path 'browser', manual path NULL; the app path 'app' when M16-07 writes it) and
-- M16-10's end-to-end DB test. The value-set CHECK is written `client IS NULL OR ...` although a NULL
-- already passes `client IN (...)`, so the reader does not have to know that CHECK's NULL rule to see it.
--
-- BOTH NOT VALID HERE, VALIDATED IN 00106 (00044's pattern): a validating ADD CONSTRAINT CHECK scans every
-- row under the ACCESS EXCLUSIVE this ALTER takes; ADD COLUMN with no default scans nothing. Every old
-- row is NULL and passes; NOT VALID is chosen for the lock, not for them, and still binds every new
-- row (internal/db's txclient_test.go measures both).
--
-- THE FIRST STATEMENT IS lock_timeout (the hot-table rule, ADR 0032 §A.5; 00064-00066, 00088, 00095):
-- behind a long reader (the nightly dump) the file gives up with 55P03 and changes nothing, instead
-- of queueing every later tap behind its ACCESS EXCLUSIVE.
--
-- PRIVILEGES (ADR 0038's rule): tappa_app's INSERT on transactions is a column list since 00088, so
-- the new column is granted here by name and the DO block holds the result to "every column but id
-- and created_at", as 00088's and 00095's do. tappa_app keeps the table-level SELECT (it covers the
-- column) and no UPDATE (§4.3). No other role reaches it: tappa_feedwriter's (00049, 00067) and
-- tappa_holdingdefiner's (00046, 00067) grants are column lists -- the punch feed, /v1 and the holding
-- projection do not carry the client (the card: carrying it there is a separate decision).
--
-- THE OLD BINARY (expand/contract: the migrate Job runs before the rollout). Its four INSERTs name
-- every column they write and none of them is this one, so they land and the row reads NULL. Its
-- SELECTs name their columns (no SELECT * in db/queries), so a wider row changes none of its scans.

-- +goose Up
SET LOCAL lock_timeout = '1s';

ALTER TABLE transactions
    ADD COLUMN client text,
    ADD CONSTRAINT transactions_client_is_known
        CHECK (client IS NULL OR client IN ('browser', 'app')) NOT VALID,
    ADD CONSTRAINT transactions_manual_has_no_client
        CHECK (channel <> 'manual' OR client IS NULL) NOT VALID;

GRANT INSERT (client) ON transactions TO tappa_app;

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
        RAISE EXCEPTION 'migration 00105: tappa_app''s INSERT on transactions is wrong on: %. It must reach every column but id and created_at. Nothing was changed.', wrong
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF has_table_privilege('tappa_app', 'public.transactions', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00105: tappa_app holds a table-level INSERT on transactions. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- 🔴 A DOWN THAT WOULD ERASE WHICH CLIENT WROTE A TAP REFUSES (CLAUDE.md §4.3, §4.6; the 00043, 00064,
-- 00065, 00095 precedent). The client of a past tap cannot be recomputed -- the request is gone -- so
-- while a row carries one this Down stops and changes nothing. The way back after the first such row
-- is the binary, not the schema: the previous binary neither writes nor reads the column, and the
-- CHECK admits NULL.
--
-- transactions is LOCKED BEFORE IT IS LOOKED AT (00064/00065/00095's reason): between an unlocked
-- EXISTS and the ALTER a tap could commit a row with a client, and the DROP COLUMN would then erase it.
-- ACCESS EXCLUSIVE is what the ALTER takes anyway; taking it first only moves it ahead of the check.
SET LOCAL lock_timeout = '1s';

LOCK TABLE transactions IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.transactions WHERE client IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 00105 Down: transactions holds rows with a client (which client wrote a tap); it cannot be recomputed and records are immutable. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- The column grant goes with the column.
ALTER TABLE transactions
    DROP CONSTRAINT transactions_manual_has_no_client,
    DROP CONSTRAINT transactions_client_is_known,
    DROP COLUMN client;
