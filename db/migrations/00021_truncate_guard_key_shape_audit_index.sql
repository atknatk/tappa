-- 00021 -- four pre-pilot debts, all of them measured before they were written.
--
-- It adds NO table, so CLAUDE.md section 6's five elements belong to the
-- migrations that created these tables and are NOT touched here: every object
-- below is a TRIGGER, a CHECK, a VALIDATE or an INDEX on an existing table.
-- The three questions section 6 asks of an additive change are answered where
-- each part is written: does it need a GRANT, does it change RLS, does the index
-- lead with tenant_id.
--
-- PART 1 (F2, HIGH)  BEFORE TRUNCATE on the six append-only tables
-- PART 2 (T7)        tags.aes_key_ref must have the shape of a KEK envelope
-- PART 3 (T15)       tags_uid_canonical_hex stops being NOT VALID
-- PART 4 (T17)       audit_log (tenant_id, target)
--
-- ============================================================================
-- PART 1 (F2) -- SIX APPEND-ONLY TABLES, ZERO TRUNCATE GUARDS
-- ============================================================================
-- 00005 built the append-only pattern as belt + braces: REVOKE UPDATE/DELETE
-- from tappa_app (privilege) AND a BEFORE UPDATE OR DELETE trigger that binds
-- tappa_owner too (00005 lines 190-202). 00011, 00016 and 00020 copied it. The
-- copy was faithful and the original was incomplete: TRUNCATE is neither UPDATE
-- nor DELETE, so no trigger in this schema fired on it.
--
-- MEASURED BEFORE THIS MIGRATION (2026-08-19, dev database, every probe inside
-- BEGIN ... ROLLBACK so the rows survived the measurement):
--
--   as tappa_app  -> ERROR: permission denied for table audit_log
--                    (has_table_privilege(tappa_app, <t>, 'TRUNCATE') = false
--                     on all six; db-init grants SELECT+INSERT and TRUNCATE is
--                     not among the four DML privileges a restore re-widens to)
--   as tappa_owner (SUPERUSER -- the identity cmd/migrate authenticates as on
--                   every deploy; the variable holding its connection string is
--                   deliberately not named here, because redline R5 reads that
--                   name under db/ as "the application connects with the
--                   migration role", which is the right reflex)
--                -> ALL SIX SUCCEEDED:
--     TRUNCATE transactions CASCADE .... rows inside tx = 0 (and 0 reviews)
--     TRUNCATE audit_log ............... rows inside tx = 0
--     TRUNCATE transaction_reviews ..... rows inside tx = 0
--     TRUNCATE billing_periods ......... rows inside tx = 0
--     TRUNCATE policy_versions CASCADE . rows inside tx = 0 (and 0 transactions)
--     TRUNCATE legal_documents ......... rows inside tx = 0
--
-- So section 4.3's own table could be emptied by the role a deploy authenticates
-- as, and nothing in the schema said no. Backlog T13 named this for audit_log
-- only; the measurement covers all six.
--
-- 🔴 CASCADE IS THE PART THAT HAD TO BE MEASURED RATHER THAN REASONED ABOUT,
-- because a guard that only fires on the NAMED table would be a guard with a
-- documented bypass: policy_versions is a FK parent of transactions
-- (transactions_policy_version_fk), which is a FK parent of transaction_reviews,
-- so `TRUNCATE policy_versions CASCADE` empties section 4.3's table WITHOUT
-- naming it. Probed on the real tables with the trigger created inside a
-- rolled-back transaction:
--
--   guard on transactions ONLY, then TRUNCATE policy_versions CASCADE
--     -> NOTICE: truncate cascades to table "transactions"
--        ERROR: append-only table transactions: TRUNCATE is forbidden
--   guard on audit_log ONLY, then TRUNCATE tenants CASCADE (16 cascaded tables)
--     -> NOTICE: truncate cascades to table "audit_log"
--        ERROR: append-only table audit_log: TRUNCATE is forbidden
--
-- i.e. a per-statement TRUNCATE trigger fires for tables reached BY CASCADE, not
-- only for the table written in the statement. That is what makes six triggers
-- enough, and it is why `TRUNCATE tenants CASCADE` -- the one statement that
-- would erase a whole business -- is now refused as well, by a table it does not
-- mention.
--
-- THE FUNCTION IS 00005's, NOT A NEW ONE. tappa_forbid_mutation() raises with
-- TG_TABLE_NAME and TG_OP and reads no row, so it is already correct for a
-- statement-level trigger (TG_OP is 'TRUNCATE'; the return value of a BEFORE
-- STATEMENT trigger is ignored, and this body never returns). A second function
-- would be a second place for the message to drift.
--
-- FOR EACH STATEMENT is not a choice: PostgreSQL only accepts statement-level
-- triggers for TRUNCATE.
--
-- WHAT THIS DOES NOT DO, said plainly rather than left to be discovered:
--   * a superuser can still ALTER TABLE ... DISABLE TRIGGER. This is defence in
--     depth, exactly as 00005 says of its own trigger, not an absolute.
--   * it does not stop DROP TABLE. Measured, because `goose down` had to keep
--     working: 00005's Down drops all three tables and a TRUNCATE trigger has no
--     say in that (proven by running goose down/up over this migration).
--   * no GRANT changes. tappa_app has no TRUNCATE privilege on any of the six
--     (measured above) and this migration does not give it one; a REVOKE would
--     be a no-op today and is deliberately NOT written, because a no-op REVOKE
--     reads like a working guard. The privilege is asserted by a test instead
--     (internal/db/appendonly_truncate_test.go), which fails if it ever changes.
--   * RLS is untouched. TRUNCATE ignores row-level security by definition --
--     which is precisely why the privilege layer and this trigger are the only
--     two things standing between a compromised owner credential and the
--     evidence.

-- +goose Up

CREATE TRIGGER transactions_no_truncate
    BEFORE TRUNCATE ON transactions
    FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation();

CREATE TRIGGER audit_log_no_truncate
    BEFORE TRUNCATE ON audit_log
    FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation();

CREATE TRIGGER transaction_reviews_no_truncate
    BEFORE TRUNCATE ON transaction_reviews
    FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation();

CREATE TRIGGER billing_periods_no_truncate
    BEFORE TRUNCATE ON billing_periods
    FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation();

CREATE TRIGGER policy_versions_no_truncate
    BEFORE TRUNCATE ON policy_versions
    FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation();

CREATE TRIGGER legal_documents_no_truncate
    BEFORE TRUNCATE ON legal_documents
    FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation();

-- ============================================================================
-- PART 2 (T7) -- aes_key_ref MUST HAVE THE SHAPE OF A KEK ENVELOPE
-- ============================================================================
-- ADR 0003 article 4 fixes the envelope at 44 bytes: nonce(12) || ciphertext(16)
-- || tag(16), AES-256-GCM under the operator's KEK. Until now the schema said
-- only `bytea NOT NULL` (00004) -- MEASURED, tags carried no CHECK on this
-- column at all -- so "this is a wrapped key" lived entirely in the discipline of
-- whoever wrote the INSERT.
--
-- WHAT THAT COST, and it is not hypothetical: a row whose aes_key_ref is two
-- bytes long is indistinguishable, in the panel, from a genuinely encoded plaque
-- (the screen reads location_id, not the key), and the first NFC tap on it dies
-- in sun.Unwrap's length check with a 500 -- with NO transactions row written.
-- That is a section 4.6 hole on that path: the evidence of the tap does not
-- exist because the request never reached the decision engine.
--
-- 🔴 NOT VALID, AND THE NUMBER IS WHY. Measured on the dev database immediately
-- before writing this (2026-08-19), octet_length(aes_key_ref) over 115 585 rows:
--
--   44 bytes (a real envelope) ......... 54 552
--    2 bytes ('\xDEAD' test marker) .... 45 728
--   12 bytes .......................... 12 129
--    1 byte ............................  3 176
--   -> 61 033 rows violate a validated CHECK
--
-- A VALIDATED constraint therefore cannot be added today, and the rows cannot be
-- rewritten into shape either: a plausible 44 bytes would be a FABRICATED key,
-- and 00013 already recorded that `tags` rows are referenced by append-only
-- `transactions`. So the constraint is born NOT VALID, exactly as 00013's
-- canonical-uid CHECK was, and for the same reason.
--
-- WHAT NOT VALID BUYS AND WHAT IT DOES NOT -- the same three facts 00013 wrote
-- for its own, restated because they are what a reader needs:
--   * INSERT of a non-44-byte ref -> REFUSED, FOR EVERY ROLE INCLUDING
--     tappa_owner. Measured, because the first draft of the test assumed NOT
--     VALID exempted the table owner: it does not, a superuser INSERT of a
--     two-byte ref answers 23514 as well. This is the whole point, and it is why
--     the test fixtures and test/fixtures/seed.sql's placeholder are changed in
--     this same change set: a constraint whose own repository cannot satisfy it
--     would be reverted within a week.
--   * UPDATE of ANY column of an already-violating row -> REFUSED (a CHECK is
--     evaluated on every new row version; NOT VALID only skips the initial
--     scan). The 61 033 residue rows are FROZEN. Harmless here -- every test
--     builds its own fixture and nothing reads them -- but said out loud rather
--     than discovered.
--   * the 61 033 rows keep existing. This constraint does not pretend otherwise;
--     `make db-reset` (rewritten in this same change set, backlog T22) is what
--     removes them, and on a fresh or production database the constraint is born
--     effectively validated because zero rows violate it.
--
-- ⚠️ THE HONEST LIMIT: 44 bytes is a SHAPE, not a proof. A 44-byte value that is
-- not a genuine envelope still fails at GCM authentication and still answers 500
-- -- later, and with a different error. What the CHECK removes is the class the
-- panel could not see and the seed-time drift guard in test/fixtures/seedkeys
-- was invented to catch after the fact.
--
-- ⚠️ SECOND COUNTED LIMIT: THE VIOLATION MESSAGE PRINTS THE ROW, aes_key_ref
-- INCLUDED. PostgreSQL attaches a DETAIL line to every CHECK failure and it is
-- the whole tuple. Measured on this constraint (inside BEGIN ... ROLLBACK):
--
--   ERROR:  new row for relation "tags" violates check constraint
--           "tags_aes_key_ref_is_kek_envelope"
--   DETAIL: Failing row contains (04AC7E55009902, <tenant>, <location>,
--           \x53484f52542d46414b45, 0, active, null, null, <ts>)
--
-- 🔴 THIS CONSTRAINT'S OWN DETAIL IS NOT A SECTION 4.7 BREACH, AND THE REASON IS
-- THE CONSTRAINT ITSELF, NOT AN ASSURANCE. The scope of that claim is THIS CHECK
-- and nothing wider. The only value that can appear in ITS DETAIL is a value IT
-- rejected, and it rejects exactly the values that are not 44 bytes. A genuine KEK
-- envelope is 44 bytes by construction (ADR 0003 article 4), so a real wrapped key
-- can never be the thing THIS constraint prints: the rows it passes are never
-- reported, and the rows it reports are by definition not envelopes. The bytes
-- above are the ASCII string used to provoke the error. The one statement in this
-- migration that touches EXISTING rows was measured separately: Part 3's
-- VALIDATE CONSTRAINT answers "is violated by some row" with no DETAIL and no row.
--
-- ⚠️ THE SAME SENTENCE IS NOT TRUE OF `tags` AS A WHOLE, and it is written here
-- rather than left for a reader to assume the opposite. A row carrying a REAL
-- 44-byte envelope DOES get that envelope printed if it violates a DIFFERENT check
-- on this table. Measured: an INSERT with a lower-case uid and a 44-byte
-- aes_key_ref fails tags_uid_canonical_hex, and the DETAIL is the whole failing
-- tuple with the key bytes in it (psql prints the leading 31 of them and then
-- truncates). That path arrives with 00004 and 00013; this migration neither
-- created it nor widened it, and it is not closed here.
--
-- MEASURED MITIGATION, and it is a narrowing rather than a fix: the DETAIL does not
-- reach this application's own logs. pgx returns a *pgconn.PgError whose Error()
-- is severity + message + SQLSTATE and does NOT include Detail (read in
-- pgx/v5 pgconn/errors.go), so an error wrapped into slog carries the constraint
-- name and not the row. Where it does land is the POSTGRES SERVER log, which this
-- repository does not control -- the DETAIL is libpq's, it is on by default, and
-- development runs with log_statement=all (docker-compose.yml).
--
-- If a future change ever makes a 44-byte value REJECTABLE by THIS constraint, the
-- first paragraph above expires with it and its DETAIL becomes a real leak.
-- Anything that widens this CHECK must revisit this paragraph.
ALTER TABLE tags
    ADD CONSTRAINT tags_aes_key_ref_is_kek_envelope
    CHECK (octet_length(aes_key_ref) = 44) NOT VALID;

-- ============================================================================
-- PART 3 (T15) -- THE ONE-LINER 00013 LEFT FOR WHOEVER RESET THE DATABASE
-- ============================================================================
-- 00013 shipped tags_uid_canonical_hex as NOT VALID and wrote the finishing line
-- itself (its lines 80-84), gated on there being no lower-case uid left. When it
-- was written there were 18 010. Measured again here, on the same database:
--
--   SELECT count(*) FILTER (WHERE uid ~ '[a-f]')                 -> 0
--   SELECT count(*) FILTER (WHERE uid !~ '^[0-9A-F]{14}$')       -> 0
--   of 115 585 rows
--
-- The residue is gone (the environment was refreshed after 00013 closed its
-- source), so the constraint can be validated and the schema can stop carrying a
-- rule it only half enforces.
--
-- ⚠️ WHAT THIS COSTS ON A DATABASE THAT STILL HAS THEM: this statement is not
-- conditional, so applying 00021 to a database with a single lower-case uid
-- FAILS with 23514 and the whole migration rolls back. That is the correct
-- failure -- a validated constraint that quietly skipped validation would be a
-- lie -- and the remedy is `make db-reset`, which is why T22 is in this same
-- change set rather than a later one.
ALTER TABLE tags VALIDATE CONSTRAINT tags_uid_canonical_hex;

-- ============================================================================
-- PART 4 (T17) -- THE INDEX ListPlaqueHistory HAS BEEN ASKING FOR
-- ============================================================================
-- db/queries/audit.sql's own header states the problem and declines to fix it
-- because 00013 was that phase's slot: `@row_limit BOUNDS THE OUTPUT, NOT THE
-- WORK`. audit_log had exactly two indexes (audit_log_pkey and
-- audit_log_tenant_at_idx -- measured), so a plaque card scans every audit row
-- the TENANT owns and throws them away.
--
-- MEASURED HERE, 2026-08-19, as tappa_app with the tenant GUC set (the shape
-- production runs, RLS in force), tenant 10000000-...-0001 holding 3 688 of
-- 193 064 audit rows in a 49 MB heap, asked for the plaque with NO history --
-- the worst case, because nothing short-circuits:
--
--   BEFORE  Bitmap Heap Scan on audit_log
--             Rows Removed by Filter: 3 688 . Heap Blocks: exact=1 184
--             Buffers: shared hit=103 read=1 113
--           Execution Time 6.219 ms cold / 2.082 / 1.963 / 1.948 ms warm
--   AFTER   Index Scan using audit_log_tenant_target_idx
--             (the after-numbers are in the task report and in
--              internal/db/auditindex_test.go, which asserts the SHAPE rather
--              than a duration -- a timing assertion in CI is a flake)
--
-- (tenant_id, target) RATHER THAN (tenant_id, action, target): both audit
-- queries that filter on a subject -- ListPlaqueHistory and ConfirmRecentRemoval
-- (db/queries/audit.sql) -- carry `tenant_id = @tenant_id AND target = @target`,
-- and only one of them compares `action` with equality; ListPlaqueHistory uses
-- `action LIKE 'plaque.%'`, which a middle btree column cannot serve. So the
-- three-column form would pay for a column one caller cannot use.
--
-- SECTION 6's QUESTIONS FOR AN INDEX: tenant_id LEADS (R5's requirement, and the
-- reason this index is usable at all under RLS). No GRANT: indexes carry none.
-- No RLS change: the policy is on the table.
--
-- COST, measured on this database: over 193 064 rows the index is 9 784 kB
-- against a 49 MB heap and a 67 MB total relation -- i.e. about a fifth of the
-- heap, and almost exactly the size of the (tenant_id, at DESC) index that is
-- already there (9 896 kB), which is the right comparison because that one is
-- the same shape. audit_log is INSERT-only, so the write cost is one more btree
-- insert per audited act; there is no UPDATE path to pay for, because the table
-- is append-only by trigger -- which Part 1 has just finished making true.
CREATE INDEX audit_log_tenant_target_idx ON audit_log (tenant_id, target);

-- +goose Down

-- Reverse order: index, then the validation, then the CHECK, then the triggers.
--
-- 🔴 WHAT A SUCCESSFUL Down GIVES BACK, in 00013's tradition of naming the
-- consequence rather than leaving it to be rediscovered:
--   * tappa_owner -- the identity every deploy authenticates as -- can TRUNCATE
--     the six append-only tables again, including section 4.3's own, and
--     including by CASCADE from tenants or policy_versions.
--   * a plaque row can carry any-length bytes in aes_key_ref again, so the
--     "encoded, looks fine, answers 500 with no transactions row" state becomes
--     insertable again.
--   * tags_uid_canonical_hex goes back to NOT VALID (there is no
--     ALTER CONSTRAINT ... SET NOT VALID in PostgreSQL, so it is dropped and
--     re-added in 00013's exact wording). New rows are still checked; the table
--     is simply no longer known to be clean.
--   * plaque history goes back to scanning the tenant's whole audit volume.
--
-- Down runs unconditionally: nothing here refuses on data, because nothing here
-- restores a NARROWER rule than the one it removes. Driven end to end
-- (goose down -> version 20, goose up -> version 21) before this file shipped.
DROP INDEX IF EXISTS audit_log_tenant_target_idx;

ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_uid_canonical_hex;
ALTER TABLE tags
    ADD CONSTRAINT tags_uid_canonical_hex CHECK (uid ~ '^[0-9A-F]{14}$') NOT VALID;

ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_aes_key_ref_is_kek_envelope;

DROP TRIGGER IF EXISTS legal_documents_no_truncate ON legal_documents;
DROP TRIGGER IF EXISTS policy_versions_no_truncate ON policy_versions;
DROP TRIGGER IF EXISTS billing_periods_no_truncate ON billing_periods;
DROP TRIGGER IF EXISTS transaction_reviews_no_truncate ON transaction_reviews;
DROP TRIGGER IF EXISTS audit_log_no_truncate ON audit_log;
DROP TRIGGER IF EXISTS transactions_no_truncate ON transactions;
