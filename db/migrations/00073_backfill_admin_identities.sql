-- 00073 -- M14-06: the backfill. Every filled e-mail group of admin_users gets one identity in
-- admin_identities, and exactly one row of the group is linked to it (linked_via = 'backfill'):
-- the group's OLDEST ACTIVE row, or -- when no row of the group is active -- its oldest. The
-- identity is born with that row's password digest (dual-write, ADR 0031 §5.1: on a linked row
-- password_hash is a copy of the identity's digest). Every other row of the group stays unlinked;
-- a row with no e-mail gets nothing.
--
-- NORMATIVE SOURCE: docs/adr/0031-tek-kimlik-cok-isletme.md §5.8 (the rule, the role assertion,
-- the closing assertion and its empty-database exemption -- bulgu 22, R11), §5.1 (the columns, the
-- CHECK (identity_id IS NULL OR linked_at IS NOT NULL) -- T12, the trigger), §8 (expand: this file
-- runs in the migrate Job BEFORE the rollout, with the previous binary live -- replicas 1, maxSurge
-- 1, maxUnavailable 0). The card is docs/plan/m14-tek-kimlik.md -> M14-06. M14-06b's
-- identity_reconcile() applies the same incumbent-first rule to the rows born after this file
-- (linked_via = 'reconcile'); nothing here is named for it or stands in its way.
--
-- WHY "OLDEST ACTIVE" AND NOT "OLDEST" (ADR 0031 §5.8, the card's fixture): an e-mail address in
-- admin_users was never proven (signup does not verify it -- ADR 0013 risk 3), so a group can hold a
-- row the address's owner did not make. The incumbent -- the account that was there first and is
-- still in use -- births the identity; a LATER row carrying the same address (an attacker's
-- business) is not linked, and an EARLIER row that is disabled does not birth it either. The
-- confirmation window (M14-05, M14-18) lets the mailbox owner refuse a backfilled link later.
-- Ties on created_at go by tenant_id, then id: deterministic, never the planner's choice.
--
-- "FILLED" E-MAIL: email IS NOT NULL and not blank. An empty or all-blank string names no mailbox;
-- an identity under it would be an address no one can prove. (Production, measured by M14-02 on
-- 2026-10-22: 0 NULL, 0 blank, 0 padded addresses.) Grouping is citext equality, so a case variant
-- is the same address -- admin_identities.email is citext UNIQUE.
--
-- THE ROLE ASSERTION (first DO block): admin_users and admin_identities are FORCE ROW LEVEL
-- SECURITY. A runner that is neither superuser nor BYPASSRLS -- the tables' owner included, FORCE
-- makes the owner obey -- would see 0 rows here: no app.tenant_id is set, admin_users' policy
-- matches nothing and admin_identities has no policy at all. The backfill would then link nothing,
-- the closing assertion would see no filled e-mail and pass, and goose would record 00073 as done.
-- So the file refuses such a runner before it reads a row. (tappa_owner on the live cluster:
-- rolsuper = t, rolbypassrls = t -- deploy/README.md, M14-02.)
--
-- THE CLOSING ASSERTION (last DO block): if any row has a filled e-mail, at least one row is linked
-- -- otherwise the backfill did not happen and the file fails. A database with NO filled e-mail
-- passes unconditionally (R11): CI, testcontainers and a new development database run the
-- migrations before the seed, so admin_users is empty there. It also asserts the two invariants
-- this file writes: every linked row's password_hash equals its identity's digest (§5.1, bulgu 6),
-- and no identity is linked through more than one backfill row.
--
-- THE DIGEST: admin_identities.password_hash carries 00018's bcrypt CHECK VALIDATED; on admin_users
-- the same CHECK is NOT VALID (00018), so a row older than 00018 may carry a digest that is not
-- bcrypt-shaped. ADR 0031 does not name that case (§5.1 gives the identity 00018's CHECK, §5.8
-- copies the chosen row's digest). Such a digest cannot be copied, and the INSERT's own
-- check_violation would print the failing row -- the address -- into the migrate Job's log
-- (CLAUDE.md §7). So the backfill counts the chosen rows whose digest is not bcrypt-shaped first and
-- refuses with the COUNT alone; nothing is written. Production: 0 such rows (M14-02, 08i/08j).
--
-- IDEMPOTENT: a candidate is a row with a filled e-mail, no identity and no refusal stamp
-- (unlinked_at); a group is taken only when no identity has its address yet and no row of it is
-- linked or refused. A second run finds every group it took already holding an identity and does
-- nothing; the closing assertion passes again.
--
-- THE TRIGGER (00069's admin_users_identity_credentials): its WHEN is OLD.identity_id IS NOT NULL OR
-- OLD.unlinked_at IS NOT NULL, and every row this Up updates is unlinked and unrefused -- the
-- trigger does not fire. Neither does the file write password_hash or email on any admin_users row,
-- so the trigger has nothing to refuse even where it fires (the Down's unlink) -- the migration runs
-- as tappa_owner, not as tappa_identitydefiner, and needs no way around it.
--
-- LOCKS AND DURATION. admin_users is hot (TouchAdminSession on every panel request, last_login_at on
-- every login) and the previous binary is live. The file takes NO table lock beyond the ROW
-- EXCLUSIVE its UPDATE needs; it locks the candidate ROWS, in id order, FOR UPDATE -- so no
-- candidate's status or digest can change between the choice and the copy -- under its own
-- `SET LOCAL lock_timeout = '1s'` (deploy/README.md's rule, 00069): behind a writer holding a row the
-- file gives up with 55P03, nothing is written, the Job is Failed and the rollout does not happen;
-- it is run again at a quiet moment. A row born after the lock (the old pod's signup) is not a
-- candidate: it is M14-06b's. ONE transaction, no batching: production holds 4 admin_users rows in
-- 3 e-mail groups (M14-02, 2026-10-22) -> 3 identities, 3 linked rows. Measured on this file's
-- database (PostgreSQL 17, no ANALYZE after the rows were written -- the worst statistics), the
-- backfill statement, i.e. the time the candidate rows stay locked: 4 rows 8 ms, 3 000 rows (1 500
-- groups of 2) 0.48 s, 20 000 rows 3.9 s -- linear, the writes dominate (deploy/README.md, "Backfill
-- (M14-06)"). Batching -- 00050's NO TRANSACTION chunks -- would buy nothing at production's size and
-- would split the closing assertion from the writes it asserts; above ~5 000 rows it is worth
-- revisiting.
--
-- THE DOWN unlinks the rows linked_via = 'backfill' and deletes the identities they were linked to --
-- this file's identities and nothing else (bulgu 22). It REFUSES, changing nothing, when one of
-- those identities has lived beyond the backfill: another row linked to it by another way
-- (password, account, invite, signup, claim, reconcile), a refused row carrying its address or its
-- backfill mark, a verified address, an opened confirmation window, a login, a last-used business,
-- or a row of another table pointing at it. Deleting it then would drop the person's link, proof or
-- history; the way back from the expand is the previous binary (ADR 0031 §8), which runs on this
-- schema. A foreign key onto admin_identities that would CASCADE or SET NULL a delete is refused
-- outright (the delete would take another table's rows with it).

-- +goose Up
-- ---------------------------------------------------------------------------
-- 0. THE ROLE ASSERTION (ADR 0031 §5.8)
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                    WHERE rolname = current_user AND (rolsuper OR rolbypassrls)) THEN
        RAISE EXCEPTION 'migration 00073 (M14-06): the running role is neither superuser nor BYPASSRLS; under FORCE ROW LEVEL SECURITY it would see no admin_users row and back-fill nothing (ADR 0031 5.8). Run the migrations as tappa_owner. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. THE BACKFILL (ADR 0031 §5.8)
-- ---------------------------------------------------------------------------
SET LOCAL lock_timeout = '1s';
-- +goose StatementBegin
DO $$
DECLARE
    locked uuid[];
    chosen uuid[];
    bad    bigint;
BEGIN
    -- The candidates, locked in id order: from here on no candidate's status, digest or e-mail
    -- changes under this transaction.
    SELECT coalesce(pg_catalog.array_agg(c.id), '{}') INTO locked
      FROM (SELECT a.id
              FROM public.admin_users a
             WHERE a.email IS NOT NULL
               AND pg_catalog.btrim(a.email::text) <> ''
               AND a.identity_id IS NULL
               AND a.unlinked_at IS NULL
             ORDER BY a.id
               FOR UPDATE) c;

    -- One row per group that has no identity yet and no linked or refused row: the oldest active,
    -- else the oldest (ties: tenant_id, id). THE PLAN IS THE TEXT'S, not the statistics' (00050's
    -- lesson, measured here too): the rows are reached through the locked ids (unnest -> the primary
    -- key), the ranking sorts them once behind an OFFSET 0 fence, and each group's two exclusions are
    -- scalar sub-selects on an indexed address (admin_identities_email_key, admin_users_email_idx) --
    -- never an anti-join the planner may turn into a nested loop over the whole table (measured: on
    -- 3 000 freshly written rows that nested loop took 4.9 s with every candidate row locked).
    SELECT coalesce(pg_catalog.array_agg(r.id), '{}') INTO chosen
      FROM (SELECT a.id, a.email,
                   pg_catalog.row_number() OVER (PARTITION BY a.email
                                                 ORDER BY (a.status = 'active') DESC, a.created_at, a.tenant_id, a.id) AS rank
              FROM pg_catalog.unnest(locked) AS l(id)
              JOIN public.admin_users a ON a.id = l.id
            OFFSET 0) r
     WHERE r.rank = 1
       AND (SELECT 1 FROM public.admin_identities i WHERE i.email = r.email LIMIT 1) IS NULL
       AND (SELECT 1 FROM public.admin_users o
             WHERE o.email = r.email
               AND (o.identity_id IS NOT NULL OR o.unlinked_at IS NOT NULL)
             LIMIT 1) IS NULL;

    -- A digest the identity's CHECK refuses: the count alone, before anything is written.
    SELECT count(*) INTO bad
      FROM pg_catalog.unnest(chosen) AS c(id)
      JOIN public.admin_users a ON a.id = c.id
     WHERE a.password_hash !~ '^\$2[aby]\$(0[4-9]|1[0-4])\$[./A-Za-z0-9]{53}$';
    IF bad > 0 THEN
        RAISE EXCEPTION 'migration 00073 (M14-06): % chosen admin_users row(s) carry a password digest that is not bcrypt-shaped (00018''s CHECK); an identity cannot be born with it. Reset that account''s password with the previous binary, then migrate again. Nothing was changed.', bad
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- The identity, born with the chosen row's address and digest, and the link -- one statement.
    -- The identity's id is drawn beside the row's id, so the link is keyed by the row's primary key
    -- (a citext join of the born addresses back to the rows measured ~1.3 s at 1 500 groups). The
    -- data-modifying CTE runs to completion whether or not the UPDATE reads it, and the UPDATE's
    -- foreign key is checked at the end of the statement, after it.
    WITH pick AS (
        SELECT a.id AS row_id, pg_catalog.gen_random_uuid() AS identity_id, a.email, a.password_hash
          FROM pg_catalog.unnest(chosen) AS c(id)
          JOIN public.admin_users a ON a.id = c.id
    ), born AS (
        INSERT INTO public.admin_identities (id, email, password_hash)
        SELECT p.identity_id, p.email, p.password_hash FROM pick p
        RETURNING id
    )
    UPDATE public.admin_users a
       SET identity_id = p.identity_id,
           linked_via  = 'backfill',
           linked_at   = pg_catalog.now()
      FROM pick p
     WHERE a.id = p.row_id;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 2. THE CLOSING ASSERTION (ADR 0031 §5.8, R11; §5.1's digest invariant)
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_users
                WHERE email IS NOT NULL AND pg_catalog.btrim(email::text) <> '')
       AND NOT EXISTS (SELECT 1 FROM public.admin_users WHERE identity_id IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 00073 postcondition: admin_users holds rows with an e-mail address and none of them is linked to an identity; the backfill did not happen (ADR 0031 5.8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM public.admin_users a
                WHERE a.identity_id IS NOT NULL
                  AND a.password_hash IS DISTINCT FROM
                      (SELECT i.password_hash FROM public.admin_identities i WHERE i.id = a.identity_id)) THEN
        RAISE EXCEPTION 'migration 00073 postcondition: a linked admin_users row does not carry its identity''s password digest (ADR 0031 5.1). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM public.admin_users
                WHERE linked_via = 'backfill' AND identity_id IS NOT NULL
                GROUP BY identity_id HAVING count(*) > 1) THEN
        RAISE EXCEPTION 'migration 00073 postcondition: an identity is linked through more than one backfill row; the backfill links one row per e-mail group (ADR 0031 5.8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                    WHERE rolname = current_user AND (rolsuper OR rolbypassrls)) THEN
        RAISE EXCEPTION 'migration 00073 Down: the running role is neither superuser nor BYPASSRLS; under FORCE ROW LEVEL SECURITY it would see no linked row and undo nothing. Run the migrations as tappa_owner. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

SET LOCAL lock_timeout = '1s';
-- +goose StatementBegin
DO $$
DECLARE
    backfilled uuid[];
    theirs     uuid[];
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_constraint c
                WHERE c.contype = 'f'
                  AND c.confrelid = 'public.admin_identities'::pg_catalog.regclass
                  AND c.confdeltype NOT IN ('a', 'r')) THEN
        RAISE EXCEPTION 'migration 00073 Down: a foreign key onto admin_identities cascades or nulls a delete; deleting the backfill''s identities would take another table''s rows with them. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    SELECT coalesce(pg_catalog.array_agg(l.id), '{}'), coalesce(pg_catalog.array_agg(DISTINCT l.identity_id), '{}')
      INTO backfilled, theirs
      FROM (SELECT a.id, a.identity_id
              FROM public.admin_users a
             WHERE a.linked_via = 'backfill' AND a.identity_id IS NOT NULL
             ORDER BY a.id
               FOR UPDATE) l;

    IF EXISTS (SELECT 1 FROM public.admin_users a
                WHERE a.identity_id = ANY (theirs) AND a.linked_via IS DISTINCT FROM 'backfill')
       OR EXISTS (SELECT 1 FROM public.admin_users a
                   WHERE a.unlinked_at IS NOT NULL
                     AND (a.linked_via = 'backfill'
                          OR EXISTS (SELECT 1 FROM public.admin_identities i
                                      WHERE i.id = ANY (theirs) AND i.email = a.email)))
       OR EXISTS (SELECT 1 FROM public.admin_identities i
                   WHERE i.id = ANY (theirs)
                     AND (i.email_verified_at IS NOT NULL OR i.confirm_opened_at IS NOT NULL
                          OR i.last_login_at IS NOT NULL OR i.last_used_tenant IS NOT NULL)) THEN
        RAISE EXCEPTION 'migration 00073 Down: an identity the backfill made has lived since (another link, a refusal, a verified address, a confirmation window or a login); deleting it would drop that. Go back with the previous binary, which runs on this schema (ADR 0031 8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    UPDATE public.admin_users
       SET identity_id = NULL, linked_via = NULL, linked_at = NULL
     WHERE id = ANY (backfilled);

    BEGIN
        DELETE FROM public.admin_identities WHERE id = ANY (theirs);
    EXCEPTION WHEN foreign_key_violation THEN
        RAISE EXCEPTION 'migration 00073 Down: a row of another table points at an identity the backfill made; deleting it would drop that row''s subject. Go back with the previous binary (ADR 0031 8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END;
END
$$;
-- +goose StatementEnd
