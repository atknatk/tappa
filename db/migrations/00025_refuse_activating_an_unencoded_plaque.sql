-- 00025 -- tags: a plaque whose encode was never recorded cannot be MOVED INTO
-- SERVICE, by any role (M10 Faz 0 F0-6b; defence in depth under F0-6's mount gate).
--
-- It adds NO table, so CLAUDE.md §6's five elements are 00004's and are NOT touched
-- here: tenant_id uuid NOT NULL, tags_tenant_idx (tenant_id, location_id), ENABLE +
-- FORCE ROW LEVEL SECURITY, tags_tenant_isolation over the NULLIF expression with
-- USING **and** WITH CHECK, and the tappa_app GRANT. This migration issues no grant,
-- revoke or policy statement; it adds one trigger and its function.
--
-- ============================================================================
-- WHY: THE GATE LIVED IN ONE STATEMENT, AND A STATEMENT BINDS ONLY ITSELF
-- ============================================================================
-- Incident A-1 (2026-09-24, live pilot): a re-encode died at WriteData with 91AE,
-- the row written at ADR 0017 §5.1 step 3 was never stamped at step 9, the panel
-- mounted it at Rusty Bar, and that door took 12 taps with 0 valid. F0-6 closed the
-- panel path by putting `encoded_at IS NOT NULL` into AssignTagToLocation's own
-- WHERE (db/queries/tags.sql). The security audit of F0-6 then named the residue
-- (LOW): the gate binds that ONE statement. A hand-written UPDATE as tappa_owner at
-- a psql prompt, or the next bind-shaped query somebody adds, never reads it.
-- 00022's write-once trigger on the same column binds every role; this is the same
-- belt for the transition the stamp exists to guard.
--
-- ============================================================================
-- THE CONDITION, AND WHY EACH HALF OF IT IS THERE (all rows measured, both roles)
-- ============================================================================
-- The whole condition is in the WHEN, 00013's and 00022's shape, so every UPDATE
-- that is not a transition INTO `active` never enters the function:
--
--   unassigned -> active, unstamped ................ REFUSED (tappa_app and tappa_owner)
--   lost / retired -> active, unstamped ............ REFUSED
--   unassigned -> active, stamped earlier .......... PASSES (the shipped mount)
--   unassigned -> active, stamp written by the SAME
--     statement (SET status, location_id, encoded_at) REFUSED -- see (c)
--   active -> active: last_ctr advance on an
--     unstamped `active` row ....................... PASSES -- see (a)
--   active -> retired / active -> unassigned,
--     unstamped .................................... PASSES -- see (b)
--
-- (a) `OLD.status IS DISTINCT FROM 'active'` IS WHAT KEEPS TAPS FLOWING (§4.6). Rows
--     that are ALREADY `active` without a stamp exist -- Rusty Bar's plaque on
--     production, and on the development database most test fixtures, because
--     00022 was not backfilled. Every tap on such a row runs AdvanceTagCounter, an
--     UPDATE whose NEW.status is `active`. A condition on NEW alone would refuse it
--     and lose the tap. `IS DISTINCT FROM` rather than `<>` is 00022's habit; status
--     is NOT NULL, so the two are equivalent here.
-- (b) The repair paths stay open, and must: retiring Rusty Bar's plaque, and taking
--     it off the wall, are how the incident is fixed. Neither is a transition INTO
--     `active`, so neither reaches the function.
-- (c) THE STAMP MUST PRE-DATE THE STATEMENT, NOT MERELY THE ROW AFTER IT -- which
--     is one term wider than the orchestrator's brief (`NEW.encoded_at IS NULL`),
--     and the widening is measured rather than argued. With NEW alone, a single
--     hand-written `UPDATE tags SET status = 'active', location_id = ..., encoded_at
--     = now() WHERE uid = ...` on an unstamped stock row PASSES: it forges the
--     record of step 9 and mounts in one breath, which is the exact bypass this
--     trigger exists for. Reading OLD.encoded_at refuses it. AssignTagToLocation's
--     own WHERE reads the PRE-statement row too, so the schema now says what the
--     shipped statement already said.
--     `OR NEW.encoded_at IS NULL` covers value -> NULL-and-activate in one
--     statement, and it is REDUNDANT with 00022's tags_encoded_at_write_once, which
--     already refuses any value -> NULL. It is kept so this gate does not lean on
--     another migration's trigger for that case -- and it is NOT PINNED, which was
--     measured rather than assumed (third-eye audit, 2026-09-26): with the term
--     removed, the Tags00025, Tags00022 and EncodedAt tests all stay green, because
--     the write-once trigger refuses the statement first. Pinning it would need that
--     trigger disabled inside a test, and `ALTER TABLE ... DISABLE TRIGGER` takes an
--     ACCESS EXCLUSIVE lock on `tags` while other packages' tests run in parallel
--     against the same database -- so the term stays unpinned and this sentence says
--     so.
--     No shipped statement writes status and encoded_at together (MarkTagEncoded
--     writes encoded_at alone; the seed stamps rows that are already `active`, so
--     OLD.status is `active` and the WHEN is false).
--
-- WHAT IT DOES NOT CLOSE, in 00021's tradition of saying so:
--   * Two statements -- stamp, then mount -- pass, exactly as they pass for the
--     shipped flow. A trigger sees the row, not the intent: whether the chip really
--     took its keys is not something the database can know (00022's column COMMENT
--     says the same of the timestamp's value).
--   * An `active` row moved to another venue (active -> active, location_id
--     changes) is not a transition and is not refused. No shipped statement does it.
--   * A SUPERUSER can still `ALTER TABLE tags DISABLE TRIGGER ...`. Defence in
--     depth, not an absolute.
--   * INSERT. See the next section.
--
-- ============================================================================
-- WHY THERE IS NO INSERT HALF (measured, and the residue is named)
-- ============================================================================
-- tappa_app holds TABLE-WIDE INSERT on `tags` (relacl `tappa_app=a`, 00004; backlog
-- T16) and tags.status still DEFAULTs to 'active' (00004). Measured as tappa_app in
-- its own tenant (SET LOCAL ROLE tappa_app: current_user tappa_app, rolsuper and
-- rolbypassrls both false), rolled back, 2026-09-26:
--
--   INSERT ... (uid, tenant_id, location_id, aes_key_ref, status='active') -> INSERT 0 1
--   INSERT ... (uid, tenant_id, location_id, aes_key_ref)   -- status omitted -> INSERT 0 1
--     both rows read back `active`, on a wall, encoded_at NULL
--
-- So the INSERT path CAN produce exactly the A-1 row, and a BEFORE INSERT trigger is
-- the obvious close -- but 00022 makes every row born unstamped, so such a trigger
-- would refuse EVERY `active` INSERT, and that was measured too: with a probe
-- trigger of that shape in place (rolled back), test/fixtures/seed.sql's plaque
-- INSERT fails -- even on a re-run where every row already exists, because a
-- BEFORE INSERT trigger fires before ON CONFLICT DO NOTHING is consulted. Beyond
-- the seed, `INSERT INTO tags` appears on 51 lines across 17 Go files outside
-- internal/store (fixtures and their helpers, most of which load `active` rows),
-- and internal/db's addPlaque helper alone is called with "active" 16 times.
-- Closing the INSERT half therefore means re-shaping those fixtures to
-- load -> stamp -> mount, and that is its own change set, not a Faz 0 belt.
--
-- What bounds the residue today is the SHIPPED statement, and two things pin it:
-- InsertUnassigned (db/queries/tags.sql) writes the literal 'unassigned' with an
-- explicit NULL location_id, which internal/encode's
-- TestDBRows_InsertLoadsStockAndWritesItsTrailEntryInOneTransaction asserts on the
-- row it lands (status = unassigned, location_id NULL, encoded_at NULL); and 00013's
-- tags_active_requires_location CHECK would refuse that statement outright were its
-- literal ever changed to 'active', because its location is NULL. Neither binds a
-- DIFFERENT INSERT that supplies a location -- which is exactly the residue measured
-- above. (cmd/tappa/insertscope_test.go is NOT part of this bound: it pins the
-- tenant predicate of every INSERT in db/queries, not its status; a status search of
-- that file finds nothing.)
--
-- ============================================================================
-- WHY A TRIGGER AND NOT A CHECK (measured)
-- ============================================================================
--   CHECK (status <> 'active' OR encoded_at IS NOT NULL) ......... cannot be added:
--     "is violated by some row" (every unstamped `active` row above)
--   the same CHECK ... NOT VALID, then a last_ctr advance on an unstamped `active`
--     row ....................................................... REFUSED (23514)
-- A NOT VALID CHECK is still enforced on every UPDATE of an old row, so it would
-- refuse the tap in (a). Its DETAIL line would also print the whole failing tuple,
-- aes_key_ref included, to a superuser session (00021 Part 2, §4.7). A trigger
-- sees OLD, which is the whole point.
--
-- ============================================================================
-- THE FUNCTION
-- ============================================================================
-- 23001 restrict_violation, the code 00011/00013/00022/00023 use for their guards,
-- so internal/db's sqlstateRestrictViolation already names it. The message prints
-- the table name and OLD.status and nothing else: no uid, no tenant, and never a
-- key (a RAISE has no DETAIL line, unlike a CHECK). SECURITY INVOKER (the default;
-- the body reads no data, so DEFINER would be a privilege surface for nothing) and
-- search_path pinned (injection defence, the shape every guard here uses).
--
-- THE SHIPPED MOUNT NEVER REACHES IT. AssignTagToLocation's WHERE excludes an
-- unstamped row, so the statement matches nothing and returns pgx.ErrNoRows, which
-- classifyMount (internal/domain/tenant/plaque.go) already turns into the refusal
-- sentence and audit row; the trigger fires only for a statement that does not
-- carry that predicate. Both halves are pinned in internal/db's
-- TestTags00025_TheShippedMountStillAnswersNoRowsAndTheTriggerCatchesTheRest.
--
-- NOT RETROACTIVE: a BEFORE UPDATE trigger changes no existing row. Rows already
-- `active` without a stamp stay exactly as they are, and keep taking taps.

-- +goose Up

-- +goose StatementBegin
CREATE FUNCTION tappa_forbid_unencoded_activation()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
    AS $$
    BEGIN
        RAISE EXCEPTION
            'a plaque whose encode was not recorded cannot go into service on %: % -> active refused (encoded_at must already be set)',
            TG_TABLE_NAME, OLD.status
            USING ERRCODE = 'restrict_violation';
    END;
    $$;
-- +goose StatementEnd

CREATE TRIGGER tags_active_requires_recorded_encode
    BEFORE UPDATE ON tags
    FOR EACH ROW
    WHEN (NEW.status = 'active'
          AND OLD.status IS DISTINCT FROM 'active'
          AND (OLD.encoded_at IS NULL OR NEW.encoded_at IS NULL))
    EXECUTE FUNCTION tappa_forbid_unencoded_activation();

-- +goose Down

-- The trigger first (it depends on the function), then the function. Dropping a
-- trigger this migration created is the one `drop trigger` redline R5b permits,
-- and only in a Down.
DROP TRIGGER IF EXISTS tags_active_requires_recorded_encode ON tags;
DROP FUNCTION IF EXISTS tappa_forbid_unencoded_activation();
