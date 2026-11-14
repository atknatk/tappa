-- 00101 -- T173 EC-1 (ADR 0044 §5.2, the user's decisions of 2026-11-12): the schema and the privileges a
-- panel identity needs to change its sign-in address by mailbox proof. Expand only: the binary before
-- this file (and 00102 / 00103) keeps working on it.
--
-- NORMATIVE SOURCE: docs/adr/0044-panel-kimliginin-e-posta-degisikligi.md §5.2 (this file), §5.3 (00102's
-- functions), §5.9 (00103's sweep), §6 (the rollout and the way back); the card is
-- docs/plan/t173-eposta-degistirme.md -> EC-1, items 1-5. ADR 0031 §5.1 / §5.2 (the identity tables, the
-- closed list, the definer's verb x column matrix) carry the dated note of 2026-11-12 that names this
-- file's additions.
--
-- WHAT THIS FILE DOES:
--   1. admin_identity_verifications -- the one home of a link mailed for an EXISTING identity (00084's
--      reason) -- gains two purposes, 'change' (the link the NEW address receives; 24 hours, K-E4) and
--      'revert' (the undo link the OLD address receives; 7 days, K-E3), and two columns:
--        address      citext NULL -- the new address of a 'change' row, the old address of a 'revert'
--                     row; NULL on 'verify' / 'link' rows (a CHECK ties the column to the purpose). The
--                     address's SHAPE is not a CHECK: a CHECK violation's DETAIL prints the failing row
--                     -- the address -- into the error and the server log (ADR 0031 §5.1's lesson). 00076's
--                     rule (6b) is a predicate in every writer instead (00102).
--        cancelled_at timestamptz NULL -- a link retired without being spent (a newer request, a change,
--                     an undo); used and cancelled are never the same column (00019's rule).
--      The TTL ceiling becomes per purpose: 24 hours for 'verify', 'link' and 'change' (today's), 7 days
--      for 'revert'. Both CHECKs keep their names (00084's purpose_known, 00072's ttl_ceiling).
--   2. admin_identity_events_event_known gains email_change_requested, email_changed, email_reverted --
--      detail empty (00072's closed keys: no tenant, no scope, no address).
--   3. The definer's matrix (ADR 0031 §5.2, K-E1 (i) (a)): UPDATE (email) on admin_identities and on
--      admin_users (the mirror, K-14 -- every linked row of the person, in every business); on the link
--      table SELECT (address, cancelled_at), INSERT (address), UPDATE (cancelled_at). The sweep's DELETE
--      on the link table is 00103's, with the sweep that uses it. tappa_app and tappa_resolver gain
--      NOTHING; tappa_app's UPDATE (email) on admin_users stays revoked (00069) and 00069's trigger still
--      refuses a linked row's e-mail write by anyone but the definer.
--
-- EXPAND-SAFE (ADR 0031 §8; deploy/k8s/20-app.yaml: the migrate Job runs before the rollout, one replica,
-- maxSurge 1, maxUnavailable 0): the old pod writes only 'verify' / 'link' rows -- address NULL, 24 hours
-- -- which both widened CHECKs accept; its functions read neither new column; no grant it relies on moves.
--
-- LOCKS: admin_users is hot (every panel request); this file only GRANTs a column on it, under its own
-- lock_timeout (00072's pattern); admin_users is not ALTERed. The link and event tables are small and
-- not hot: their CHECK swaps validate under this file's lock_timeout. Past it the file fails with 55P03
-- and changes nothing -- run it again.
--
-- THE DOWN REFUSES (the same block opens 00102's and 00103's Downs: goose runs Downs newest first, each in
-- its own transaction, so a block in this file alone would let `down-to 100` commit 00103 and 00102 --
-- drop the undo function and give identity_unlink_backfilled back its unguarded body -- before stopping
-- here; ADR 0044 §6) while EITHER holds: a live 'revert' row exists (the person's way back would be
-- taken away), or admin_identity_events holds an email_* event (the event CHECK cannot go back to 00079's
-- set, and an event row is never deleted but by the 90-day sweep -- R17). Otherwise it deletes the
-- 'change' / 'revert' rows (each spent, cancelled or expired by then), takes the grants back and gives
-- both CHECKs and the event set 00084's / 00072's / 00079's text back, byte for byte. Changed addresses
-- stay -- they are data.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.sessions_revocation_is_one_way()') IS NULL
       OR pg_catalog.to_regclass('public.admin_identity_verifications') IS NULL
       OR pg_catalog.to_regclass('public.admin_identity_events') IS NULL
       OR NOT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute
                       WHERE attrelid = 'public.admin_identity_verifications'::pg_catalog.regclass
                         AND attname = 'purpose' AND NOT attisdropped) THEN
        RAISE EXCEPTION 'migration 00101 (T173 EC-1) needs 00099 and the identity link table with its purpose (00072, 00084). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_attribute
                WHERE attrelid = 'public.admin_identity_verifications'::pg_catalog.regclass
                  AND attname IN ('address', 'cancelled_at') AND NOT attisdropped) THEN
        RAISE EXCEPTION 'migration 00101 (T173 EC-1) adds address and cancelled_at to admin_identity_verifications, and one of them is already there. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The link table: two purposes, the address, the retirement, the TTL per purpose
-- ---------------------------------------------------------------------------
ALTER TABLE admin_identity_verifications
    ADD COLUMN address      citext,
    ADD COLUMN cancelled_at timestamptz,
    DROP CONSTRAINT admin_identity_verifications_purpose_known,
    ADD CONSTRAINT admin_identity_verifications_purpose_known
        CHECK (purpose IN ('verify', 'link', 'change', 'revert')),
    ADD CONSTRAINT admin_identity_verifications_address_for_its_purpose
        CHECK ((address IS NOT NULL) = (purpose IN ('change', 'revert'))),
    DROP CONSTRAINT admin_identity_verifications_ttl_ceiling,
    ADD CONSTRAINT admin_identity_verifications_ttl_ceiling
        CHECK (expires_at <= created_at + interval '24 hours'
               OR (purpose = 'revert' AND expires_at <= created_at + interval '7 days'));

-- ---------------------------------------------------------------------------
-- 2. The event set: + the three address events
-- ---------------------------------------------------------------------------
ALTER TABLE admin_identity_events
    DROP CONSTRAINT admin_identity_events_event_known,
    ADD CONSTRAINT admin_identity_events_event_known CHECK (event IN (
        'login_failed', 'login_limited', 'reset_requested', 'email_verified',
        'link_refused', 'membership_unlinked', 'membership_relinked', 'confirm_kept',
        'email_change_requested', 'email_changed', 'email_reverted'));

-- ---------------------------------------------------------------------------
-- 3. The definer's cells (ADR 0031 §5.2's matrix, K-E1 (i) (a))
-- ---------------------------------------------------------------------------
GRANT UPDATE (email) ON admin_identities TO tappa_identitydefiner;
GRANT SELECT (address, cancelled_at), INSERT (address), UPDATE (cancelled_at)
    ON admin_identity_verifications TO tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
GRANT UPDATE (email) ON admin_users TO tappa_identitydefiner;

-- ---------------------------------------------------------------------------
-- 4. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'email', 'UPDATE')
       OR NOT pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'email', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.admin_users', 'email', 'UPDATE')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.admin_identity_verifications', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_resolver', 'public.admin_identity_verifications', 'address', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00101 postcondition: the definer lacks an e-mail cell, or tappa_app / tappa_resolver reached a cell ADR 0044 5.2 does not give them. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- The two tables first: no undo link and no address event can be written between the check and the
-- CHECKs' restore (00079's Down's order).
LOCK TABLE admin_identity_verifications, admin_identity_events IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_identity_verifications AS v
                WHERE v.purpose = 'revert' AND v.used_at IS NULL AND v.cancelled_at IS NULL
                  AND v.expires_at > pg_catalog.clock_timestamp())
       OR EXISTS (SELECT 1 FROM public.admin_identity_events AS e
                   WHERE e.event IN ('email_change_requested', 'email_changed', 'email_reverted')) THEN
        RAISE EXCEPTION 'migration 00101 Down: an e-mail change can still be undone, or admin_identity_events holds an e-mail change event (it ages out after 90 days); the way back is the previous binary, which runs on this schema (ADR 0044 6). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- Every 'change' / 'revert' row is spent, cancelled or expired here (the block above): it can be spent
-- by nothing, and with the column gone it would read as a verification link.
DELETE FROM admin_identity_verifications WHERE purpose IN ('change', 'revert');

SET LOCAL lock_timeout = '1s';
REVOKE UPDATE (email) ON admin_users FROM tappa_identitydefiner;
REVOKE UPDATE (email) ON admin_identities FROM tappa_identitydefiner;

ALTER TABLE admin_identity_events
    DROP CONSTRAINT admin_identity_events_event_known,
    ADD CONSTRAINT admin_identity_events_event_known CHECK (event IN (
        'login_failed', 'login_limited', 'reset_requested', 'email_verified',
        'link_refused', 'membership_unlinked', 'membership_relinked', 'confirm_kept'));

ALTER TABLE admin_identity_verifications
    DROP CONSTRAINT admin_identity_verifications_ttl_ceiling,
    ADD CONSTRAINT admin_identity_verifications_ttl_ceiling CHECK (expires_at <= created_at + interval '24 hours'),
    DROP CONSTRAINT admin_identity_verifications_address_for_its_purpose,
    DROP CONSTRAINT admin_identity_verifications_purpose_known,
    ADD CONSTRAINT admin_identity_verifications_purpose_known CHECK (purpose IN ('verify', 'link')),
    DROP COLUMN cancelled_at,
    DROP COLUMN address;
