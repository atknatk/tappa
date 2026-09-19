-- 00023 -- tags.app_key_ref: a SECOND per-plaque wrapped key, for NTAG 424 DNA
-- application key 0 (the AppMasterKey), in its OWN envelope column beside
-- aes_key_ref (application key 1). The decision is ADR 0018; this migration is its
-- schema half, and it is deliberately the TWIN of aes_key_ref's proven shape
-- (00004 born, 00013/00021/00022 hardened) rather than a new mechanism.
--
-- It adds NO table, so CLAUDE.md §6's five elements are 00004's and are NOT
-- touched here: tenant_id uuid NOT NULL, tags_tenant_idx (tenant_id, location_id),
-- ENABLE + FORCE ROW LEVEL SECURITY, tags_tenant_isolation over the NULLIF
-- expression with USING **and** WITH CHECK, and the tappa_app GRANT. 00013 and
-- 00022 wrote that same paragraph for their own additions; a COLUMN is not a
-- table, and a policy keyed on tenant_id covers every column the row will ever
-- have. VERIFIED against pg_catalog after applying, not asserted (the check and
-- its output are in the task report, and internal/db/appkeyref_test.go re-runs the
-- isolation and privilege halves against a live server on every `make test`).
--
-- ============================================================================
-- WHY A SECOND COLUMN AND NOT A DERIVATION (ADR 0018)
-- ============================================================================
-- The encode flow (internal/encode) today personalises only key 1
-- (K_SDMFileRead): a per-plaque random AES-128, wrapped with TAPPA_TAG_KEK, stored
-- as the 44-byte envelope in aes_key_ref. Key 0 is still at the PUBLIC factory
-- default, so anyone with physical access can AuthenticateEV2First as master and
-- rewrite the chip (ADR 0017 §5.0, ADR 0005 risk 8). ADR 0017 §5.1 step 8
-- (ChangeKey on key 0) closes that, but it was blocked on WHERE the new key 0
-- lives. ADR 0018 chose a separate column over the two rejected options:
--   * deriving key 0 from the plaque secret would chain two authorities (SDM read
--     and master) to ONE secret -- exactly the coupling ADR 0017 §5.0 Karar 1
--     refuses;
--   * write-and-forget would lose the key if the chip half-writes at step 8, a
--     §4.6 / §4.7 permanent loss the §5.3 probes could not even diagnose.
-- Two keys, two envelopes, two blast radii: if one leaks the other stays sealed.
--
-- 🔴 THE PLAIN KEY NEVER APPEARS -- not here, not in a log, not in the repo
-- (CLAUDE.md §4.7). app_key_ref holds ONLY the KEK-GCM envelope (44 bytes: nonce
-- 12 || ciphertext 16 || tag 16, ADR 0003 md. 4). The plain AES-128 is minted and
-- wrapped in Go (internal/encode, phase B) and only the envelope travels IN as a
-- bound parameter; nothing in this schema unwraps it.
--
-- NULLABLE, and both halves of that carry weight (ADR 0018 Sonuçlar). Rows that
-- existed before 00023 were born without it, so NOT NULL is impossible without
-- backfilling a value that does not exist. In the new flow the row is written WITH
-- app_key_ref BESIDE aes_key_ref at INSERT (ADR 0017 §5.2: DB before chip, because
-- "row, no chip" is recoverable and "chip, no row" is a §4.7 loss). So NULL is a
-- STATE with a name: no key 0 has been minted for this plaque -- which is also the
-- DB-side witness that ADR 0017 §5.1 step 8 has not run for it. aes_key_ref stays
-- NOT NULL (00004); this migration does NOT touch it.

-- +goose Up

-- Catalog-only: a NULLABLE column with no default rewrites no tuple (the
-- PostgreSQL 11 change was about non-null DEFAULTs). The CHECK is satisfied for
-- free by every existing row because they are all NULL (`app_key_ref IS NULL`
-- short-circuits), so no heap data is read to validate it.
--
-- 🔴 THE 44-BYTE CHECK IS aes_key_ref's, TWINNED (00021's
-- tags_aes_key_ref_is_kek_envelope). It refuses anything that is not the fixed
-- envelope shape -- a bare 16-byte AES-128 key, a truncated wrap, a mis-typed blob
-- -- at the DB boundary, which is the §4.7-integrity value of naming the length: a
-- plain key is 16 bytes, an envelope is 44, and a value of the wrong length is the
-- shape a leak of the raw key would have. NULLABLE, so the CHECK admits NULL (the
-- pre-step-8 state above); 00021's aes_key_ref CHECK omits the NULL arm only
-- because that column is NOT NULL.
ALTER TABLE tags ADD COLUMN app_key_ref bytea
    CONSTRAINT tags_app_key_ref_is_kek_envelope
    CHECK (app_key_ref IS NULL OR octet_length(app_key_ref) = 44);

COMMENT ON COLUMN tags.app_key_ref IS
    'KEK-wrapped NTAG 424 DNA application key 0 (AppMasterKey), the twin of '
    'aes_key_ref (key 1). 44-byte AES-GCM envelope (TAPPA_TAG_KEK); the plain key '
    'is NEVER stored, logged or in the repo (CLAUDE.md §4.7). NULL = no key 0 has '
    'been minted for this plaque, i.e. ADR 0017 §5.1 step 8 has not run. Written '
    'beside aes_key_ref at INSERT (ADR 0017 §5.2, DB before chip); write-once '
    '(tags_app_key_ref_write_once) and off tappa_app''s UPDATE/SELECT grants, so '
    'it is never rewritten and never read by the application role (ADR 0018).';

-- --- The write-once guard (the structural half) -------------------------------
-- A grant says WHICH column may be written, never HOW MANY TIMES. For this column
-- the dangerous value is a SECOND one: app_key_ref answers "what master key did we
-- put on this chip", a chip's key 0 is changed once (ADR 0017 §5.1 step 8, which
-- ends the session and is irreversible without the new key), and a second envelope
-- on one row is a claim about a key change that did not happen written over the
-- record of one that did -- and it would strand the physical chip, whose key 0 no
-- longer matches any envelope we hold (§4.7).
--
-- 🔴 THE WHOLE CONDITION LIVES IN THE `WHEN`, which is 00022's shape and is what
-- makes the two legitimate writes free:
--   NULL  -> value ....... PASSES. A backfill: a pre-00023 or pre-step-8 row gets
--                          its key 0 minted (by the owner; tappa_app has no UPDATE
--                          grant, see below). This is why the new flow can also
--                          write it at INSERT and never need an UPDATE.
--   value -> SAME value .. PASSES. An idempotent retry is not a rewrite (00011's
--                          BOUNDARY 2: a guard that fires on a duplicate makes the
--                          caller report failure for work that succeeded).
--   value -> other value . REFUSED.
--   value -> NULL ........ REFUSED. `IS DISTINCT FROM` (not `<>`) is what catches
--                          this: the column is NULLABLE, so a plain inequality
--                          against NULL is NULL, the WHEN would not fire, and
--                          un-setting an encoded key would be SILENTLY allowed.
--                          00013's counter guard can use a plain `<` only because
--                          its two operands are both NOT NULL.
--
-- 🔴 THE MESSAGE NAMES NO VALUE, AND THAT IS THE ONE PLACE THIS GUARD DIVERGES
-- FROM 00022's. 00022's encoded_at guard prints OLD -> NEW because timestamps are
-- not secrets; here OLD and NEW are KEK-wrapped KEYS, and §4.7 forbids putting
-- aes_key_ref/app_key_ref in a log line. So the RAISE is given ONLY the table
-- name. A RAISE carries no DETAIL (unlike a CHECK, whose DETAIL is the whole
-- failing tuple -- migration 00021 Part 2), so nothing else leaks either.
--
-- SECURITY INVOKER (default): the body reads no data, so DEFINER would be a
-- privilege surface for nothing. search_path pinned (injection defence, 00004's
-- resolver / 00013's / 00022's shape). It binds tappa_owner too, which no REVOKE
-- can (the belt 00005 puts over transactions, 00013 over the counter, 00022 over
-- encoded_at): tappa_owner is a SUPERUSER, so FORCE ROW LEVEL SECURITY does not
-- bind it and a column REVOKE cannot either -- a trigger can. Defence in depth, not
-- an absolute: a superuser can still DISABLE the trigger.
-- +goose StatementBegin
CREATE FUNCTION tappa_forbid_app_key_ref_rewrite()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
    AS $$
    BEGIN
        RAISE EXCEPTION
            'application master key ref is write-once on %: app_key_ref may be set once and never rewritten or cleared',
            TG_TABLE_NAME
            USING ERRCODE = 'restrict_violation';
    END;
    $$;
-- +goose StatementEnd

CREATE TRIGGER tags_app_key_ref_write_once
    BEFORE UPDATE ON tags
    FOR EACH ROW
    WHEN (OLD.app_key_ref IS NOT NULL AND NEW.app_key_ref IS DISTINCT FROM OLD.app_key_ref)
    EXECUTE FUNCTION tappa_forbid_app_key_ref_rewrite();

-- --- GRANTS: NOTHING IS ADDED, AND EACH OF THE THREE IS DELIBERATE (§4.7) -------
-- app_key_ref follows aes_key_ref's privilege shape EXACTLY, and after 00013 and
-- 00022 a NEW column reaches that shape WITHOUT a single statement here. Measured
-- on this database after the ALTER (has_*_privilege as tappa_app):
--
--   INSERT .. GRANTED, and it needed no line. tappa_app holds TABLE-WIDE INSERT
--             (pg_class.relacl = tappa_app=ar, from db-init's default privileges +
--             00004; 00013 declined to revoke it), which covers every column an
--             ALTER adds. This is the ONE privilege the flow needs: InsertUnassigned
--             writes app_key_ref beside aes_key_ref at step 3 (ADR 0018 §5.2), and
--             it is an INSERT, not an UPDATE.
--   UPDATE .. NOT GRANTED, on purpose. 00013 revoked table-wide UPDATE and
--             re-granted five columns; 00022 added a sixth (encoded_at); a new
--             column is NOT on that list, so tappa_app cannot UPDATE app_key_ref.
--             That IS the write-once guarantee on the application side -- the same
--             mechanism that makes aes_key_ref write-once (it is off the UPDATE
--             list too). The trigger above is the belt that also binds tappa_owner.
--             ⚠️ ADR 0018 Sonuçlar names a "column-level UPDATE (app_key_ref)"
--             grant; that line is deliberately NOT followed, and the reason is the
--             ADR's own §5.2 -- the value is written at INSERT (step 3), never by
--             UPDATE, so an UPDATE grant would authorise a write the flow never
--             performs (surplus authority over a secret, §4.7 least privilege).
--             Recovery/second-attempt READS the envelope; it does not rewrite it.
--   SELECT .. NOT GRANTED, mirroring aes_key_ref. 00022 revoked table-wide SELECT
--             and re-granted nine columns, deliberately excluding aes_key_ref; a
--             new column is not on that list, so tappa_app cannot SELECT
--             app_key_ref. The tap path reads aes_key_ref through the SECURITY
--             DEFINER resolve_tag_by_uid (00004) because a tap has NO tenant
--             context; app_key_ref is used only by the encode flow, which DOES have
--             tenant context, so a future recovery read belongs behind a
--             tenant-scoped path added THEN -- not a standing SELECT on a secret
--             today. No sqlc query returns it, which the key-walls in
--             cmd/tappa/storekeyshape_test.go and internal/domain/tenant's
--             NoShippedTagQuerySelectsTheKey both enforce.
--
-- So this migration issues NO grant, revoke or policy statement. Every grant on
-- `tags` stays exactly as 00004/00013/00022 left it, verified by the privilege
-- test in internal/db/appkeyref_test.go rather than assumed.

-- No index: app_key_ref is never a search key or a join key -- it is fetched by
-- uid (the PK) on the one path that ever reads a wrapped key, and never filtered
-- or ordered on. An index would be write cost for a read that does not exist.

-- +goose Down
--
-- Reverse order: the trigger, then its function, then the column (which takes its
-- CHECK constraint and its COMMENT with it). ORDER MATTERS: a trigger's WHEN
-- clause names the column, so dropping the column first fails with 2BP01 ("cannot
-- drop column app_key_ref ... because other objects depend on it") -- the same
-- dependency 00022's Down documents. Dropping the trigger first is what lets this
-- Down run at all.
--
-- 🔴 A SUCCESSFUL Down DESTROYS EVIDENCE, and `make migrate-down` is a target
-- someone will type. DROP COLUMN takes every app_key_ref value with it, and there
-- is no second copy in `tags`: aes_key_ref is a DIFFERENT key, and status /
-- location_id say nothing about key 0. A re-applied 00023 brings back an all-NULL
-- column. The honest qualifier (00022's): the encode round's audit trail survives
-- in audit_log (append-only for every role, 00005), so which plaques had key 0
-- personalised is reconstructible, not restorable.
--
-- NO GRANT IS REVOKED HERE because none was granted (see the Up side). DROP COLUMN
-- removes no privilege that survives -- the table-wide INSERT that covered the
-- column is 00004's, on the table not the column, and it stays, which is 00022's
-- state exactly. A REVOKE here would be a no-op that reads like a working guard
-- (00021 Part 1's rule), so none is written.
--
-- Down runs UNCONDITIONALLY: nothing here restores a NARROWER rule than it
-- removes, so unlike 00013's there is no data guard.

DROP TRIGGER IF EXISTS tags_app_key_ref_write_once ON tags;
DROP FUNCTION IF EXISTS tappa_forbid_app_key_ref_rewrite();
ALTER TABLE tags DROP COLUMN app_key_ref;
