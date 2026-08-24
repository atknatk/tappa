-- 00022 -- tags.encoded_at: the mark ADR 0017 §5.1 step 9 asks for, and the
-- sentence in an applied migration that it REFUTES.
--
-- It adds NO table, so CLAUDE.md §6's five elements belong to 00004 and are NOT
-- touched here: tenant_id uuid NOT NULL, tags_tenant_idx (tenant_id,
-- location_id), ENABLE + FORCE ROW LEVEL SECURITY, tags_tenant_isolation
-- carrying USING **and** WITH CHECK over the NULLIF expression, and the
-- tappa_app GRANT. 00013 wrote that same paragraph for its own additions and
-- this migration repeats it for the same reason: a COLUMN is not a table, and a
-- policy keyed on tenant_id covers every column the row will ever have. VERIFIED
-- against pg_catalog after applying rather than asserted -- the check and its
-- output are in the task report, and TestRLS_Tags00022_EncodedMarkerIsIsolatedByTenant
-- re-runs the isolation half against a live server on every `make test`.
--
-- ============================================================================
-- PART 1 -- WHAT THE COLUMN IS FOR
-- ============================================================================
-- ADR 0017 §5.1's ninth and last step reads: "[sunucu] Zero(anahtarlar); satiri
-- 'encode edildi' olarak isaretle". internal/encode/session.go declares the port
-- for it (Rows.MarkEncoded) and its own comment says, correctly, that there was
-- nowhere to write it: "`tags` HAS NO SUCH COLUMN TODAY". This migration is that
-- column.
--
-- NULLABLE, NO DEFAULT, and both halves carry weight. NULL is not "unknown", it
-- is a STATE with a name: the inventory row exists and the chip was never
-- confirmed personalised. A DEFAULT now() would stamp every row the moment it is
-- inserted -- i.e. it would record the INTENT to encode as though it were the
-- outcome, which is precisely the confusion Part 2 exists to end. There is no
-- backfill either: the 139 445 rows on the development database at the time this
-- was written are test residue and seed data, and NONE of them came out of the
-- encode flow, so every one of them is honestly NULL.
--
-- NO INDEX, and the omission is a decision rather than an oversight (CLAUDE.md
-- §6 asks three questions of an additive change; this is the third). No shipped
-- query filters or orders on encoded_at -- the two statements added in this same
-- change set reach a row by its PRIMARY KEY -- so an index would be paid for on
-- every write and read by nothing. The day a screen asks "which plaques are
-- loaded but never encoded", the predicate is `encoded_at IS NULL` inside one
-- tenant, and the index worth measuring then is a partial one on (tenant_id)
-- WHERE encoded_at IS NULL. Measure it then; do not add it now.
--
-- ============================================================================
-- PART 2 -- 🔴 THIS COLUMN REFUTES A SENTENCE IN APPLIED MIGRATION 00013
-- ============================================================================
-- 00013 Part 3 states the inventory model and closes it with:
--
--     "So: pending = location_id IS NULL. encoded = the row exists (aes_key_ref
--      is NOT NULL and Tappa wrapped it before loading), which is what makes the
--      card's criterion representable at all."
--
-- That sentence WAS TRUE ON THE DAY IT WAS WRITTEN, and it is worth saying why
-- rather than filing it as an error. In the 2026-08-08 loading model the only
-- writers of a `tags` row were tappa_owner by hand and test/fixtures/seedkeys,
-- and both of them ran AFTER the chip was personalised -- the row was the
-- RECEIPT. Under that order, "the row exists" really did imply "this chip is
-- encoded".
--
-- ADR 0017 §5.2 INVERTS THE ORDER, deliberately and with the asymmetry measured:
-- the row is written BEFORE the chip's first irreversible command, because the
-- two half-write modes are not equally bad. "Row, no chip" leaves a dead
-- inventory row that is recoverable (the wrapped key is still ours, the same key
-- can be driven onto the chip again). "Chip, no row" leaves a chip carrying a
-- key that exists NOWHERE -- unauthenticatable, unverifiable, permanently
-- scrap -- which is a §4.7 key-management loss. So the row goes first, and from
-- that decision on, "the row exists" means "WE INTENDED TO ENCODE THIS", not
-- "this chip is encoded".
--
-- The distance between those two readings is exactly the failure this column
-- closes: without it, a half-finished encode round and a completed one are the
-- SAME ROW, and no query, screen or operator can tell them apart.
--
-- 00013 cannot be edited -- an applied migration is immutable (CLAUDE.md §6) --
-- so this migration is where the correction lives, and the column COMMENT below
-- carries the short form of it into the catalog, where a reader of the schema
-- meets it without reading either file.

-- +goose Up

-- Catalog-only: a NULLABLE column with no default has never required a tuple
-- rewrite (the PostgreSQL 11 change was about non-null DEFAULTs). Measured on
-- the development database, 139 445 rows / 18 MB heap / 41 MB total relation:
-- goose reported the WHOLE migration -- this ALTER, the COMMENT, the function,
-- the trigger and the GRANT -- at 21.8 ms there, and 6.2 / 13.2 ms on two
-- throwaway clones with no rows at all, i.e. the spread is this machine rather
-- than the row count. pg_relation_size('tags') and pg_total_relation_size('tags')
-- were unchanged across it (18 MB / 41 MB before and after), and all 139 445 rows
-- read back encoded_at IS NULL. (An ACCESS EXCLUSIVE lock is still taken for the
-- catalog update -- brief, but not free, and named because the same statement
-- behind a long-running reader on a busy production table is a queue, not a
-- no-op.)
ALTER TABLE tags ADD COLUMN encoded_at timestamptz;

COMMENT ON COLUMN tags.encoded_at IS
    'When this chip completed its personalisation round (ADR 0017 §5.1 step 9). '
    'NULL = the inventory row exists but the chip was never confirmed '
    'personalised. 🔴 This REFUTES migration 00013 Part 3''s "encoded = the row '
    'exists": ADR 0017 §5.2 writes the row BEFORE the chip''s first irreversible '
    'command, so the row now means "we intended to encode this". A row is BORN '
    'UNSTAMPED (tags_encoded_at_not_settable_at_insert) and a stamp never changes '
    '(tags_encoded_at_write_once); the shipped statement uses the server clock. '
    'What the schema does NOT enforce: that the value is now() rather than any '
    'other timestamp -- a trigger cannot see intent, only the two rules above.';

-- --- The write-once guard (the structural half) -------------------------------
-- A column grant says WHICH column may be written, never WHAT VALUE it may take
-- and never HOW MANY TIMES. For a counter the dangerous value is a smaller one
-- (00013's tags_counter_monotonic); for this column the dangerous value is a
-- SECOND one. encoded_at answers "when did this physical chip get its keys", and
-- a chip is personalised once: ADR 0017 §5.1 step 6 moves K_SDMFileRead
-- (application key 0x01) off its factory default, and nothing moves it back
-- without the new key. So a second timestamp on one row is not a correction --
-- it is a claim about an event that did not happen, written over the record of
-- one that did.
--
-- ⚠️ ONE QUALIFIER, because the sequence is not finished: §5.1 step 8 (key 0x00,
-- the AppMasterKey) is written as a DECISION but is BLOCKED -- §6 md. 5 records
-- that `tags` carries a single aes_key_ref and ADR 0003 md. 4 fixes it at one
-- 44-byte envelope, so there is nowhere to store a second per-plaque key. Until
-- that schema decision lands, an "encoded" plaque still has key 0 at the public
-- factory default, and §5.1's own line is that such a plaque may be built and
-- tested but MUST NOT GO ON A WALL (the pilot gate, M8-06 item 7). This column
-- records that the ROUND completed; it is not a certificate that the chip is
-- fully locked down, and nothing should read it as one.
--
-- 🔴 THE WHOLE CONDITION LIVES IN THE `WHEN`, not in the function body, which is
-- 00013's shape and is what makes the two legitimate writes free:
--
--   NULL -> value ......... PASSES. The one write this flow performs.
--   value -> SAME value ... PASSES. An idempotent retry is not a rewrite. This
--                           is 00013 Part 2's stated rule ("re-writing the SAME
--                           value is allowed on purpose") and 00011's BOUNDARY
--                           2: a guard that fires on a duplicate makes the
--                           caller report failure for work that SUCCEEDED.
--                           MarkTagEncoded's coalesce() means the retry writes
--                           the stored timestamp back, so it lands here.
--   value -> other value .. REFUSED.
--   value -> NULL ......... REFUSED. `IS DISTINCT FROM` rather than `<>` is what
--                           catches this one: the column is NULLABLE, so a plain
--                           inequality would evaluate to NULL, the WHEN would
--                           not fire, and un-marking an encoded plaque would be
--                           SILENTLY ALLOWED. That is the difference between
--                           this trigger and 00013's, whose two operands are
--                           both NOT NULL and which therefore uses a plain `<`.
--
-- Every UPDATE that does not touch the column (a bind, an unbind, a retire, a
-- counter advance) never enters the function at all.
--
-- SECURITY INVOKER (the default): the body reads no data, so DEFINER would be a
-- privilege surface bought for nothing. search_path is pinned (injection
-- defence, the same shape 00004's resolver and 00013's guard use).
--
-- THE MESSAGE PRINTS THE TWO TIMESTAMPS AND NOTHING ELSE, and that limit is
-- deliberate. 00021 Part 2 recorded, with the measurement, that a CHECK
-- violation's DETAIL line is the WHOLE FAILING TUPLE and that on this table that
-- tuple contains aes_key_ref (§4.7). A RAISE has no such DETAIL -- it prints
-- only what the format string is given -- so this guard is written to be given
-- only two values, and both of them are times, which are not secrets. Nothing
-- here names uid, tenant or key.
--
-- IT BINDS tappa_owner TOO, which no REVOKE can (the same belt 00005 puts over
-- transactions/audit_log and 00013 over the counter). Measured on a throwaway
-- v22 clone, connected as tappa_owner -- a SUPERUSER, i.e. the identity FORCE ROW
-- LEVEL SECURITY cannot reach -- with all five statements run in order:
--
--   NULL -> value ......... UPDATE 1
--   value -> SAME value ... UPDATE 1
--   value -> other value .. ERROR 23001: encoded marker is write-once on tags:
--                           encoded_at may be stamped once and never rewritten
--                           (2026-08-24 06:13:21.184481+00 ->
--                            2026-08-24 07:13:21.188685+00 refused)
--   value -> NULL ......... ERROR 23001: ... (2026-08-24 06:13:21.184481+00 ->
--                            <NULL> refused)
--   status='lost' ......... UPDATE 1   (an unrelated column never enters the WHEN)
--
-- 23001 is `restrict_violation`, which is what USING ERRCODE names; the tests
-- assert the CODE and not the message text, because a message can be reworded and
-- a code cannot (internal/db/tagsinventory_test.go's sqlstateRestrictViolation is
-- already that constant, from 00011's guard).
--
-- WHAT IT DOES NOT DO, in 00021's tradition of saying it rather than leaving it
-- to be discovered: a SUPERUSER can still
-- `ALTER TABLE tags DISABLE TRIGGER tags_encoded_at_write_once`. This is defence
-- in depth, not an absolute.
-- +goose StatementBegin
CREATE FUNCTION tappa_forbid_encoded_at_rewrite()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
    AS $$
    BEGIN
        RAISE EXCEPTION
            'encoded marker is write-once on %: encoded_at may be stamped once and never rewritten (% -> % refused)',
            TG_TABLE_NAME, OLD.encoded_at, NEW.encoded_at
            USING ERRCODE = 'restrict_violation';
    END;
    $$;
-- +goose StatementEnd

CREATE TRIGGER tags_encoded_at_write_once
    BEFORE UPDATE ON tags
    FOR EACH ROW
    WHEN (OLD.encoded_at IS NOT NULL AND NEW.encoded_at IS DISTINCT FROM OLD.encoded_at)
    EXECUTE FUNCTION tappa_forbid_encoded_at_rewrite();

-- --- The INSERT half, which the first version of this migration DID NOT BIND -----
-- 🔴 A SECURITY AUDIT MEASURED THE HOLE AND IT WAS EXACTLY THE SHAPE THIS FILE'S
-- OWN COMMENT CLAIMED WAS SHUT (2026-08-24). The column COMMENT below says
-- "Server-side now() only, and write-once". Both halves were true of the SHIPPED
-- UPDATE and neither was true of the COLUMN, because the guard above is
-- BEFORE UPDATE and nothing was watching INSERT. Measured as tappa_app, in its own
-- tenant, inside BEGIN ... ROLLBACK:
--
--   INSERT INTO tags (..., encoded_at) VALUES (..., '1999-01-01 00:00:00+00')
--     -> ACCEPTED. The row read back with a fabricated stamp.
--
-- That is a caller-supplied timestamp on the one column whose whole purpose is to
-- say when a PHYSICAL CHIP was personalised, and it widens 00013's counted residue:
-- tappa_app holds TABLE-WIDE INSERT (measured -- pg_class.relacl is `tappa_app=ar`
-- and there are NO INSERT column grants at all, has_column_privilege is `t` for all
-- ten columns), so an injection or a mis-written query could load a plaque that
-- LOOKS confirmed.
--
-- ⚠️ WHY THIS IS A SECOND TRIGGER RATHER THAN THE OBVIOUS ONE-WORD FIX. Widening
-- the guard above to BEFORE INSERT OR UPDATE is NOT POSSIBLE, and that was measured
-- rather than reasoned about:
--
--   ERROR:  INSERT trigger's WHEN condition cannot reference OLD values
--
-- The two rules are genuinely different -- "an existing stamp may not change" needs
-- OLD, "a new row may not arrive stamped" must not mention it -- so they are two
-- triggers. A CHECK constraint cannot express the second either: it would refuse the
-- value on every UPDATE too, including the one legitimate write.
--
-- THE RULE: a row is BORN UNSTAMPED. encoded_at is set by ADR 0017 §5.1 step 9 and
-- by nothing else, which makes the column COMMENT's two claims true of the COLUMN
-- and not merely of one statement.
--
-- MEASURED, all three, as tappa_app with a probe trigger of this exact shape:
--   INSERT carrying a stamp .......... REFUSED (restrict_violation)
--   the SHIPPED insert (no stamp) .... INSERT 0 1
--   the SHIPPED update, then a retry . stamped, and idempotent
--
-- ⚠️ NOT RETROACTIVE, and worth saying because the number is not zero: rows on the
-- development database already carry a stamp (test fixtures, written by the very
-- tests that prove the marker works). A BEFORE INSERT trigger sees only new rows,
-- so nothing existing is frozen or rewritten.
--
-- 🔴 THE COUNT IS DELIBERATELY NOT THE POINT, AND THAT IS A CORRECTION. This line
-- first read "223 rows"; an audit re-ran it and got 73, and a third reading the
-- same day gave 79 -- because every suite run adds fixtures and the Down/Up cycle
-- this migration went through DROPPED the column and took them with it. The figure
-- is a READING OF A MOVING POPULATION, not a fact about the schema, exactly as
-- 00013 says of its own two-byte residue count. What is stable, and is all this
-- paragraph needs, is the DIRECTION: it is never zero, and the trigger does not
-- touch any of them. (Reading at the time of writing, for whoever wants the shape:
-- 79 stamped rows of 142 369 on 2026-08-24, after a Down/Up cycle.)
-- +goose StatementBegin
CREATE FUNCTION tappa_forbid_encoded_at_at_insert()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
    AS $$
    BEGIN
        RAISE EXCEPTION
            'the encoded marker is not settable at INSERT on %: a plaque row is born unstamped and is marked only when its chip completes (% refused)',
            TG_TABLE_NAME, NEW.encoded_at
            USING ERRCODE = 'restrict_violation';
    END;
    $$;
-- +goose StatementEnd

CREATE TRIGGER tags_encoded_at_not_settable_at_insert
    BEFORE INSERT ON tags
    FOR EACH ROW
    WHEN (NEW.encoded_at IS NOT NULL)
    EXECUTE FUNCTION tappa_forbid_encoded_at_at_insert();

-- --- The grant, and why it is NOT the 00013 mistake ---------------------------
-- 🔴 REQUIRED, NOT CONVENIENT. ADR 0017 §3.1 closes the role question with a
-- measurement rather than a preference: the encode flow is an HTTP endpoint, so
-- it runs in the server process, and internal/db/pool.go REFUSES a DSN whose
-- role is a superuser, holds BYPASSRLS, or owns an RLS table -- in production.
-- (§3.1's own qualifier, and it belongs here too: roleRefusal returns nil unless
-- cfg.IsProd(), so a developer machine WARNS instead of refusing. "Architecturally
-- there is no other option" is a sentence about production.) The tappa_owner
-- option is therefore gone, the marker is an UPDATE, and without this line the
-- shipped flow answers "permission denied for table tags" on its last step --
-- after the chip is already personalised, i.e. in the one place a failure costs
-- a physical plaque.
--
-- 🔴 AND THE 00013 LESSON DOES NOT APPLY HERE, which is worth stating because
-- it looks like it should. 00013 Part 2 warns: "NARROWING a grant does nothing
-- -- the table-wide privilege must be REVOKEd first", and it had to write
-- REVOKE UPDATE before its GRANT UPDATE (five columns) because 00004 had granted
-- table-wide UPDATE on top of db-init's default privileges. That work is DONE
-- and is not undone by this line. This grant is ADDITIVE: it adds a sixth column
-- to an already-narrowed set. A REVOKE here would remove the five columns 00013
-- deliberately granted and break the bind, unbind and retire flows.
--
-- MEASURED, both before and after (has_column_privilege as tappa_app, on the
-- development database; the full 9-row table is in the task report):
--   before .. UPDATE on exactly (location_id, last_ctr, status, retired_at,
--             replaced_by) -- 00013's five
--   after ... those five PLUS encoded_at, and nothing else
--
-- THE FOUR COLUMNS THAT MUST NEVER MOVE STAY UNGRANTED FOR UPDATE, and they are
-- 00013's four: uid (renaming a plaque makes the URL printed on the wall resolve
-- to nothing), tenant_id (moving a row between businesses), aes_key_ref (a
-- plaque that answers 500 forever, §4.7) and created_at. Re-verified with
-- has_column_privilege after this migration, because adding a column to a table
-- is exactly the kind of change that can widen a table-level ACL by accident.
--
-- ⚠️ THE HONEST WEAK POINT, and it is 00013's own, restated because this
-- migration is the one that makes it reachable: tappa_app holds TABLE-WIDE
-- INSERT on `tags`, aes_key_ref included (00004), and 00013 declined to revoke
-- it with the note "the day a loader writes plaques through the application
-- role, this line is what has to be revisited". The query added in this same
-- change set (InsertUnassigned) IS that loader. What bounds the exposure is that
-- the value crossing the boundary is the 44-byte KEK envelope and never the
-- plain key (ADR 0017 §3.1, limit 1), and that aes_key_ref remains write-ONCE
-- because it is not on the UPDATE list. The open half is backlog T16 -- a
-- COLUMN-level INSERT restriction -- which this migration does NOT close and
-- does not pretend to.
GRANT UPDATE (encoded_at) ON tags TO tappa_app;

-- --- THE READ SIDE: tappa_app MAY NOT SELECT aes_key_ref -------------------------
-- 🔴 THIS CLOSES A CLASS FIVE ROUNDS OF STATIC ANALYSIS COULD NOT, AND THE REASON IS
-- STRUCTURAL RATHER THAN A BETTER SCANNER. Every text or AST gate this repository
-- built asks "what does a leak LOOK like" -- a column name, a wildcard, a []byte, a
-- changed signature. A security audit then produced a leak that looks like NOTHING:
--
--   db/queries/tags.sql, GetTagForTenant's select list, one substitution
--     g.status   ->   to_jsonb(g)::text AS status
--
-- sqlc emits a row type BYTE-IDENTICAL to the shipped one (Status is already a
-- string), so the signature inventory says "112 inventoried and matched", the
-- []byte-binding gate never looks (there is no []byte), the SQL text wall sees no
-- column name and no wildcard, and redline-check exits 0. Measured, all four.
-- The value that reaches Go is the whole row as JSON -- 390 characters, of which 90
-- are the wrapped key -- travelling through tenant.Plaque.Status onto the plaque card.
--
-- THE PRIVILEGE SYSTEM DOES NOT ASK THAT QUESTION ABOUT A **DIRECT** EXPRESSION. If
-- the column cannot be read from `tags`, the expression used to try does not matter.
--
-- 🔴 AND THAT QUALIFIER IS NOT DECORATION -- THE UNQUALIFIED SENTENCE WAS FALSIFIED
-- BY THE NEXT AUDIT (2026-08-24). The key is still readable through
-- resolve_tag_by_uid, which is SECURITY DEFINER owned by tappa_resolver and RETURNS
-- aes_key_ref, and to which tappa_app holds EXECUTE. Measured: with this REVOKE in
-- force, `(SELECT to_jsonb(r)::text FROM resolve_tag_by_uid(g.uid) r LIMIT 1)`
-- returned the 44-byte envelope, and a CTE over the same call returned it too.
--
-- 🔴 THAT DOOR CANNOT BE CLOSED, AND SAYING SO IS THE POINT. The tap path MUST
-- unwrap the envelope to verify a SUN, and a tap arrives with NO tenant context --
-- ADR 0002 md. 7 built the resolver for exactly that. So the wall's shape is not
-- "the application role cannot read the key"; that sentence can never be true. It is:
--
--   every DIRECT expression over tags.aes_key_ref is refused by privilege, and the
--   key remains readable through resolve_tag_by_uid, which CANNOT be closed.
--
-- ⚠️ AND THE CREDIT IS SPLIT, WHICH AN EARLIER DRAFT OF THIS LINE DID NOT DO. It said
-- "ten of eleven attempted shapes answer permission denied" and filed all ten under
-- this migration. Measured: two of the ten are refused by privileges this migration
-- never touched -- CREATE VIEW answers `permission denied for SCHEMA public` and
-- SET ROLE answers `permission denied to SET ROLE`. THIS MIGRATION'S OWN SHARE IS
-- EIGHT (the column, to_jsonb, row_to_json, SELECT *, octet_length, a quoted
-- correlated subselect, COPY TO STDOUT, and a self-made pg_temp definer). A later
-- audit then refused THIRTY more direct shapes against this same REVOKE, so eight is
-- its share of the original eleven, not a ceiling.
--
-- ⚠️ AND THE SECOND HALF OF THAT SENTENCE WAS ALSO TOO BIG, CORRECTED 2026-08-24: it
-- said the remaining path is limited to one caller "and that path is INVENTORIED".
-- The inventory (cmd/tappa/storekeyshape_test.go) is TEXT MATCHING and was beaten
-- twice in one sitting -- a Unicode-escaped function name that sqlc accepts, and a
-- split string literal in a new Go file; a third shape (a future definer with a new
-- name) is uncovered by construction, and a FOURTH needs no evasion at all (adding a
-- line to the inventory, which now carries a size ratchet).
--
-- 🔴 NO MECHANISM IS IN FORCE TODAY, AND ONE EXISTS -- IT WAS NOT TAKEN. An earlier
-- draft said "nothing mechanical limits that path"; measured (BEGIN ... ROLLBACK,
-- proacl checked before and after), EXECUTE on the function is an ordinary privilege:
-- a separate role holding it, with tappa_app's revoked, answers `permission denied
-- for function resolve_tag_by_uid`. The cost is a SECOND POOL in internal/db, which
-- touches FAZ B2c-2b's pool design, so it is deferred as a named option rather than
-- done here. "Cannot be closed" (true: the tap needs the envelope) and "no mechanism
-- exists" (false) are different sentences.
--
-- It is a COUNTED gap, recorded in cmd/tappa/storekeyshape_test.go and in the M8-05
-- hand-over list under its own title (grep: SAYILDI, KAPATILMADI), and it is
-- deliberately not claimed closed.
--
-- ⚠️ THE POINTER IS A TITLE BECAUSE THE NUMBER WAS WRONG. It read "item 17"; the
-- list's labels ran 1..16, 18, 19, 17, and CommonMark renumbers from the first item,
-- so "item 17" rendered as a different entry. The order is fixed in the card; this
-- reference no longer depends on it.
--
-- ⚠️ SECOND CONSEQUENCE, AND IT IS §4.5 RATHER THAN §4.7: tappa_resolver holds
-- BYPASSRLS, so ANY query calling that function crosses the tenant boundary.
-- Measured -- one tenant's connection read another tenant's envelope in full. ADR
-- 0002 md. 7 accepted that for the RESOLUTION path, where the tenant is the answer
-- rather than the question; a PANEL query calling it is OUTSIDE that decision. This is CLAUDE.md §4.5's belt-and-braces
-- shape applied to §4.7: there, RLS plus an explicit predicate; here, PRIVILEGE plus
-- the scanners, and the privilege is the load-bearing half.
--
-- 🔴 MEASURED, EACH IN ITS OWN BEGIN ... ROLLBACK, AS tappa_app WITH A TENANT SET:
--
--   SELECT octet_length(aes_key_ref) FROM tags        -> permission denied for table tags
--   SELECT to_jsonb(g)::text FROM tags g              -> permission denied   <- the escape
--   SELECT row_to_json(g) FROM tags g                 -> permission denied
--   SELECT * FROM tags                                -> permission denied
--   SELECT (SELECT g2.aes_key_ref FROM "tags" g2 ...) -> permission denied   <- quoted, the
--                                                        shape the []byte gate's
--                                                        tokenizer also missed
--   the ordinary panel column list                    -> OK
--   the shipped INSERT (INSERT is a separate privilege) -> OK
--   the shipped UPDATE ... RETURNING uid, encoded_at  -> OK
--   AdvanceTagCounter's FOR UPDATE row lock           -> OK
--
-- ⚠️ WHAT IT DOES NOT TOUCH, and both were checked rather than assumed:
--   * THE TAP PATH. internal/db/resolve.go reads the key through
--     resolve_tag_by_uid, which is SECURITY DEFINER owned by tappa_resolver
--     (measured: prosecdef = t, proowner = tappa_resolver). A definer function runs
--     with its OWNER's privileges, so revoking the caller's changes nothing there.
--     That is the same containment ADR 0002 md. 7 designed, used for a second job.
--     🔴 AND IT IS THEREFORE ALSO THE ONE REMAINING READ PATH -- see the paragraph
--     above. This bullet used to file the risk under "a FUTURE definer written
--     without care"; the definer that matters is the one SHIPPED SINCE 00004.
--   * tappa_owner. The rotatekek runbook (deploy/README.md) reads and rewrites every
--     envelope as the owner; this REVOKE names tappa_app only, and the owner's
--     table ACL is untouched (measured before and after).
--
-- ⚠️ AND WHAT IT STILL DOES NOT CLOSE, counted rather than implied: the definer path
-- above; a column the application CAN read is still readable by any expression, so
-- this bounds aes_key_ref and nothing else. It is also not a defence against tappa_owner, a
-- superuser, or a future SECURITY DEFINER function written without care.
--
-- THE 00013 LESSON APPLIES AND IS FOLLOWED: db-init's ALTER DEFAULT PRIVILEGES and
-- 00004's table-wide GRANT both hand out table-level SELECT, and NARROWING a grant
-- does nothing while the table-level one stands -- it must be REVOKEd first.
-- ⚠️ A NOTE ABOUT THE `DETAIL` CHANNEL, AND IT IS A CORRECTION OF THIS FILE'S OWN
-- FIRST DRAFT (audit, 2026-08-24). That draft claimed this REVOKE closed the leak
-- class migration 00021 Part 2 measured -- a CHECK violation's DETAIL line being the
-- WHOLE FAILING TUPLE, aes_key_ref included, in the server log. The two-role reading
-- reproduces:
--
--   as tappa_owner  ERROR: ... violates "tags_active_requires_location"
--                   DETAIL: Failing row contains (0DE7A1..., ..., \xdededede..., ...)
--   as tappa_app    ERROR: ... violates "tags_active_requires_location"   (NO DETAIL)
--
-- 🔴 BUT THE MECHANISM IS NOT THIS REVOKE, AND THE CONTROL SAYS SO. On `employees`,
-- where tappa_app holds FULL table SELECT and no column grant exists, the same
-- experiment gives the SAME split: tappa_app gets no DETAIL, tappa_owner gets the
-- whole tuple. The discriminator is the ROLE -- tappa_owner is rolsuper/rolbypassrls,
-- tappa_app is neither, so with RLS active PostgreSQL never builds the tuple
-- description at all. This REVOKE adds NOTHING to that channel.
--
-- ⚠️ AND IT WAS ALREADY COUNTED THREE TIMES, ON TABLES WITH NO SUCH REVOKE: migration
-- 00018, migration 00019 and ADR 0015 all record it. Those are applied and are not
-- edited (CLAUDE.md §6); this note is where the correction lives. What is true is that
-- the application role does not see the tuple; what is false is that this migration is
-- why.
REVOKE SELECT ON tags FROM tappa_app;
GRANT SELECT (uid, tenant_id, location_id, last_ctr, status, retired_at, replaced_by,
              created_at, encoded_at) ON tags TO tappa_app;

-- +goose Down

-- Reverse order: both triggers, then their functions, then the column.
--
-- 🔴 READ THIS FIRST: A SUCCESSFUL Down DESTROYS EVIDENCE, and `make migrate-down`
-- is a documented target somebody will type. Named plainly, in 00013's and
-- 00021's house style:
--
--   * EVERY RECORD OF WHICH PLAQUES WERE CONFIRMED PERSONALISED IS DESTROYED.
--     DROP COLUMN takes the values with it and there is no second copy of them
--     in `tags` -- not in status (an encoded plaque and an unencoded one are
--     both 'unassigned' until a manager mounts one), not in location_id, not in
--     aes_key_ref, which is written before the chip is touched and therefore
--     says nothing about whether the chip took the keys. Re-applying 00022
--     brings back an all-NULL column.
--
--   * THE HONEST QUALIFIER, because "unrecoverable" would be too strong: the
--     AUDIT TRAIL survives, and it ships in this same change set. ADR 0017 §6
--     md. 8 owed an `audit_log` event for the encode round; M8-05 FAZ B2c-2a
--     defines it as `plaque.loaded` (step 3, the row) and `plaque.encoded`
--     (step 9, this column) -- internal/encode/rows.go, constants declared in
--     internal/domain/tenant/plaque.go. audit_log is append-only for EVERY role
--     including the owner (00005's trigger, 00021's TRUNCATE guard), so those
--     rows outlive this Down and the column can be rebuilt from them with
--     `SELECT target, at FROM audit_log WHERE action = 'plaque.encoded'`.
--
--     ⚠️ REBUILT IS NOT THE SAME AS RESTORED, and the difference is measurable:
--     `at` is when the EVENT was recorded and `encoded_at` is what the UPDATE
--     stamped, both server-side and inside one transaction, so they agree to
--     within that transaction -- but a plaque marked before this event existed
--     (none today) would have no row at all. Say "reconstructible", not
--     "backed up".
--
--     ⚠️ AND A NOTE FOR WHOEVER READS ADR 0017 AFTER THIS: md. 8 writes the
--     pattern as `tag.retired`. There is no `tag.*` action anywhere in the tree
--     (measured: grep over internal/ and db/ returns nothing); the shipped
--     spelling is `plaque.<past participle>`. The tree's wins.
--
--   * the write-once guard goes with it, so on a re-applied 00022 the column is
--     rewritable until this migration runs again. That is not a separate risk
--     (the column is gone either way), it is named so the order is understood:
--     the guard is not a property of the data, it is a property of the schema.
--
-- Down runs UNCONDITIONALLY -- unlike 00013's, nothing here refuses on data,
-- because nothing here restores a NARROWER rule than the one it removes.
--
-- THE COLUMN GRANT NEEDS NO REVOKE, and this was MEASURED rather than assumed
-- (the claim "DROP COLUMN takes its grant with it" is exactly the kind that is
-- true until it is not). On a throwaway v22 clone, inside BEGIN ... ROLLBACK:
--
--   before the drop .. has_column_privilege(tappa_app, tags, encoded_at, UPDATE) = t
--   after the drop ... pg_attribute keeps ONE attisdropped row, renamed
--                      '........pg.dropped.10........', and it carries NO attacl;
--                      the eight surviving columns' attacl lists are 00013's
--                      exactly, and the table ACL is still tappa_app=ar
--   re-added column .. UPDATE = f  (the grant did not survive and does not
--                      resurrect), INSERT = t (that one is the TABLE-wide INSERT
--                      of 00004, which is a different privilege and is untouched
--                      here -- see the weak-point note above)
--
-- The reason an explicit REVOKE is NOT written anyway is 00021 Part 1's rule: a
-- no-op statement reads like a working guard.
--
-- The COMMENT goes with the column too (there is nothing to reset, unlike
-- 00013's Down, which had to write COMMENT ... IS NULL for comments it put on
-- columns that SURVIVE its rollback).
--
-- ORDER IS NOT COSMETIC: a trigger's WHEN clause depends on the columns it
-- names, so dropping the column while the trigger still exists fails with
-- "cannot drop column encoded_at of table tags because other objects depend on
-- it" (2BP01). Measured on the clone. Dropping the trigger first is what makes
-- this Down run at all.
-- 🔴 THE READ SIDE GOES BACK FIRST, AND ITS RETURN IS A SECURITY REGRESSION: after
-- this, tappa_app can SELECT aes_key_ref again, and with it every whole-row
-- expression (to_jsonb, row_to_json, SELECT *) that the column grant refuses. The
-- static gates do NOT cover that class -- see the Up side for the measurement.
-- Revoking the column grant first is what makes the table-level GRANT the only
-- privilege left, which is 00004's state exactly.
REVOKE SELECT ON tags FROM tappa_app;
GRANT SELECT ON tags TO tappa_app;

DROP TRIGGER IF EXISTS tags_encoded_at_not_settable_at_insert ON tags;
DROP TRIGGER IF EXISTS tags_encoded_at_write_once ON tags;
DROP FUNCTION IF EXISTS tappa_forbid_encoded_at_at_insert();
DROP FUNCTION IF EXISTS tappa_forbid_encoded_at_rewrite();

ALTER TABLE tags DROP COLUMN encoded_at;
