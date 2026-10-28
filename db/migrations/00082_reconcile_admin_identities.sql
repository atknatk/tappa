-- 00082 -- M14-06b: the reconciliation. identity_reconcile() applies 00073's rule to the admin_users
-- rows born AFTER the backfill (the previous pod's signups during the expand rollout, every signup
-- between the expand and M14-17): each filled e-mail group that has no identity yet gets one, born
-- with the digest of the group's OLDEST ACTIVE row (else its oldest), and that row alone is linked
-- (linked_via = 'reconcile', linked_at = now()). This file creates the function and calls it once
-- (Deploy 2, ADR 0031 §8 step 3); the runbook calls it again right before M14-19 (deploy/README.md,
-- "Uzlaştırma (M14-06b)"); contract B (M14-20b) drops it.
--
-- NORMATIVE SOURCE: docs/adr/0031-tek-kimlik-cok-isletme.md §5.2 (the maintenance function: owner
-- tappa_identitydefiner, EXECUTE in tappa_owner ALONE -- the one named exception to "EXECUTE only
-- tappa_app", ONAY note 1 -- created and called by this file, re-called by the runbook, dropped by
-- contract B; T3, T8), §5.8 (same incumbent-first rule, idempotent, never links a refused row), §8
-- (Deploy 2; "Binary'yi expand'in altına geri almak yalnız uzlaştırmayla birlikte"), §4.5 (3) of
-- CLAUDE.md (the three maintenance writers). The card is docs/plan/m14-tek-kimlik.md -> M14-06b.
--
-- THE RULE IS 00073's, CASE FOR CASE, so the two never diverge (adminreconcile_test.go
-- runs one fixture through both and compares) -- plus the address rule below, which 00073 (applied
-- before the rule existed) does not carry:
--   * a candidate is a row with a filled e-mail (NOT NULL, not blank), no identity and no refusal
--     stamp (unlinked_at) whose address passes the address rule;
--   * a group is taken only when NO IDENTITY HAS ITS ADDRESS YET and NO ROW OF IT IS LINKED OR
--     REFUSED. This is the M14-06 security note: a reconcile that joined a later row to an identity
--     already under the address would hand the victim's business to an identity an EARLIER active
--     attacker's row birthed -- so the group is left alone, and the confirmation window (M14-18) is
--     the mailbox owner's way to refuse what the backfill linked;
--   * the chosen row is the oldest ACTIVE one, else the oldest; ties on created_at by tenant_id, then
--     id; the identity carries that row's address and digest; every other row stays unlinked;
--   * a chosen digest that is not bcrypt-shaped (admin_users' 00018 CHECK is NOT VALID, the identity's
--     is VALIDATED) stops the run with a message that names no row and no address -- the INSERT's own
--     check_violation would print the address (CLAUDE.md §7) -- and nothing is written.
-- E-mail equality is citext's, written OPERATOR(public.=): the body's search_path is pg_catalog,
-- pg_temp, where a bare `=` would fall back to text's case-sensitive one.
--
-- THE ADDRESS RULE (M14-08a, the user's decision of 2026-10-26; ADR 0031 §5.1 and §5.8's notes): a
-- group's candidates are only the rows whose address internal/mail would send to -- mail.ValidRecipient,
-- written out as 00076's predicate 6b with its pattern byte for byte (at most 254 octets, no "=?", the
-- addr-spec in printable ASCII). Each row the group's lock takes is held to it by an
-- `IF (...) IS NOT TRUE THEN CONTINUE` refusal BEFORE its id joins the locked set the choice reads, so a
-- homoglyph row (U+0130, U+212A: citext folds them onto ASCII letters -- 00076's finding) is never
-- chosen, not even as the group's oldest active row: the identity is born with the spelling and the
-- digest of the group's oldest active rule-passing row (else its oldest); a group of rule-failing rows
-- alone births nothing -- the named residue the M14-19 precheck counts apart (deploy/README.md).
-- identityascii_test.go's TestIdentityAddressRule_EveryIdentityInsertDecidesIt sees this function as a
-- writer that decides the rule; adminreconcile_test.go holds the refusal to the value it guards. A
-- rule-failing row is still locked with its group (the FOR UPDATE takes every candidate row), and it
-- shares the group's advisory lock: citext's equality is lower()'s, so a homoglyph and the ASCII
-- spelling it folds onto have one lock key ('admin-identity-email:' || lower(email)).
--
-- THE APPLICATION IS LIVE WHILE IT RUNS (T8). Per group, in this order -- the lock order of M14-05
-- (identity -> membership rows -> token / session rows) with the identity not yet born:
--   1. the address's namespaced advisory lock, pg_advisory_xact_lock(hashtextextended(
--      'admin-identity-email:' || lower(email), 0)) -- the key every identity INSERT takes
--      (identity_create_for_signup, identity_accept_invite_new, identity_claim_signup: ADR 0031
--      §5.2, S13); a second reconcile, or a signup of the same address, queues here;
--   2. the address check (no identity under it), read after that lock (a writer that held it has
--      committed);
--   3. the group's candidate rows FOR UPDATE, in id order, their ids kept; THEN the linked-or-refused
--      group check, read after the row locks (a link committed meanwhile by a writer that does not
--      take the address's lock -- identity_link_legacy -- is seen); THEN the choice, among the
--      LOCKED ids only (00073's way) and read after the wait -- a row committed while it waited is
--      never chosen unlocked, so the digest the identity is born with cannot change before the link;
--   4. the identity INSERT with ON CONFLICT (email) DO NOTHING -- an identity born by a writer that
--      did not take the lock (none should; this is the second belt) makes the group skipped, never a
--      23505 and never a 500;
--   5. the link, pinned by the row's id AND tenant, guarded again by identity_id / unlinked_at IS
--      NULL; anything but exactly one row linked stops the run (no identity is left unlinked).
-- No clock comparison is made after a wait: linked_at is now(), as the backfill's.
--
-- THE TRIGGER (00069's admin_users_identity_credentials): its WHEN is OLD.identity_id IS NOT NULL
-- OR OLD.unlinked_at IS NOT NULL; every row this function updates is unlinked and unrefused, so it
-- does not fire, and the function writes neither password_hash nor email on admin_users.
--
-- THE BELT (internal/db/identitybelt_test.go). The function runs as BYPASSRLS and reads admin_users
-- ACROSS every tenant by design -- a maintenance pass has no tenant to start from. Its four reads of
-- admin_users that name no tenant are a named class of their own there (identityMaintenanceScans,
-- ADR 0021 §3.2: "bilerek bütün tenant'ları tarayan fonksiyonlar adıyla bellidir"), pinned by count
-- and never a write; the one write (the link) is pinned to the chosen row's own tenant. Its address
-- check of admin_identities is keyed by the address (the identity does not exist yet -- no secret
-- can name it), its INSERT carries ON CONFLICT and its choice reads `a.id = ANY (v_locked)`; each is
-- named in identityBeltAllowedConstructs and pinned by count (identityBeltAllowedCounts).
--
-- GRANTS: NONE. The two cells the body needs beyond 00071/00072's -- SELECT (password_hash) on
-- admin_users (the chosen row's digest) and INSERT (email, password_hash) on admin_identities, both
-- inside ADR 0031 §5.2's matrix -- are 00074's (identity_create_for_signup needs the same two); the
-- precondition checks they are held and the Down leaves them to 00074's Down. EXECUTE: tappa_owner
-- alone (plus the owner).
--
-- THE CARDS SINCE (00075-00081). The rows an invitation's acceptance (00077, linked_via 'invite') and
-- a registration's claim (00078/00080, 'claim') write are born LINKED: no candidate, and their group
-- holds a linked row, so it is left alone. identity_create_for_signup (00074/00076) links 'signup' at
-- birth, or leaves the row unlinked under an address that already has an identity (skipped here) or
-- that fails the rule (filtered here). The confirmation window (00079) lists 'reconcile' links on
-- first_verify as it lists the backfill's (ADR 0031 §5.8); this pass opens no window and writes no
-- identity column but email and password_hash at the birth. resolve_admin_owners_by_tenant (00081)
-- reads links and writes none.
--
-- THE POSTCONDITION (last DO block), as 00073's: the runner must be superuser or BYPASSRLS (first DO
-- block) so it sees every row, then -- (a) no group the rule would take is left among the rows born
-- before this transaction began (a definer that lost BYPASSRLS would see 0 rows, link nothing and
-- "succeed"; a database with no filled e-mail passes unconditionally -- R11); (b) every row linked
-- 'reconcile' carries its identity's digest (§5.1); (c) no identity is linked through two reconcile
-- rows; (d) the function's catalogue shape; (e) no identity linked 'reconcile' carries an address the
-- rule refuses. (a) counts only rule-passing rows (a group of rule-failing rows alone is the named
-- residue, not a miss), and its `created_at < now()` keeps the previous pod's
-- signups that start while this file runs out of it; one whose transaction began before this one and
-- commits in the milliseconds between the call and the check fails the file -- nothing is written,
-- the Job is Failed and is run again.
--
-- LOCKS AND DURATION. No table lock beyond the ROW EXCLUSIVE of the link; the candidate rows of one
-- group at a time FOR UPDATE, held to the commit; `SET LOCAL lock_timeout = '1s'` (deploy/README.md's
-- rule): behind a writer holding a row the call gives up with 55P03, nothing is written, the Job is
-- Failed and runs again at a quiet moment. Production at Deploy 2: the rows born during the expand
-- rollout window -- expected 0 to a handful.
--
-- THE DOWN mirrors 00073's for linked_via = 'reconcile': it unlinks those rows and deletes the
-- identities they were linked to (each was born by this function -- a reconcile link is always to an
-- identity it made in the same call), and REFUSES, changing nothing, when one of those identities has
-- lived since (another row linked to it, a refused row of its address or carrying the reconcile mark,
-- a verified address, an opened window, a login, a last-used business, a row of another table
-- pointing at it) -- the way back from the expand is the previous binary (ADR 0031 §8), which runs on
-- this schema; a binary below the expand is brought back up only together with the reconciliation
-- (§5.8). ADR 0031 §8: "expand Down'ı yalnız eklediğini kaldırır". Then it drops the function; the
-- two grants it used stay (00074's).

-- +goose Up
-- ---------------------------------------------------------------------------
-- 0. PRECONDITION AND THE ROLE ASSERTION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                    WHERE rolname = current_user AND (rolsuper OR rolbypassrls)) THEN
        RAISE EXCEPTION 'migration 00082 (M14-06b): the running role is neither superuser nor BYPASSRLS; under FORCE ROW LEVEL SECURITY it could not check what the reconciliation left (ADR 0031 5.8). Run the migrations as tappa_owner. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regclass('public.admin_identities') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_unlink_backfilled(text,uuid)') IS NULL THEN
        RAISE EXCEPTION 'migration 00082 (M14-06b) needs 00069-00073 (admin_identities, the identity functions, the backfill). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- 00076 (M14-08a) first: its address rule is the one the body repeats.
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                    WHERE p.oid = pg_catalog.to_regprocedure('public.identity_create_for_signup(uuid,uuid)')
                      AND pg_catalog.strpos(p.prosrc, '-- 6b. THE ADDRESS RULE (M14-08a)') > 0) THEN
        RAISE EXCEPTION 'migration 00082 (M14-06b) needs 00076 (M14-08a: the address rule in identity_create_for_signup). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- The two cells the body needs beyond 00071/00072's are 00074's; this file grants nothing.
    IF NOT (pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'password_hash', 'SELECT')
            AND pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'email', 'INSERT')
            AND pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'password_hash', 'INSERT')) THEN
        RAISE EXCEPTION 'migration 00082 (M14-06b) needs 00074''s grants to tappa_identitydefiner (admin_users SELECT (password_hash), admin_identities INSERT (email, password_hash)). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regprocedure('public.identity_reconcile()') IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00082 (M14-06b) creates identity_reconcile() and it is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. identity_reconcile() (ADR 0031 §5.2, §5.8; T3, T8)
-- ---------------------------------------------------------------------------
-- Returns the number of rows it linked in this call.
-- +goose StatementBegin
CREATE FUNCTION public.identity_reconcile()
    RETURNS integer
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    c          record;
    r          record;
    v_locked   uuid[];
    v_row      uuid;
    v_tenant   uuid;
    v_email    public.citext;
    v_digest   text;
    v_identity uuid;
    v_n        integer;
    v_linked   integer := 0;
BEGIN
    -- The candidate addresses: one per citext group of rows with a filled e-mail, no identity and no
    -- refusal stamp. (A maintenance scan: every tenant, by design.)
    FOR c IN
        SELECT a.email
          FROM public.admin_users AS a
         WHERE a.identity_id IS NULL
           AND a.unlinked_at IS NULL
           AND a.email IS NOT NULL
           AND pg_catalog.btrim(a.email::text) <> ''
         GROUP BY a.email
         ORDER BY a.email
    LOOP
        -- 1. The address's lock: every identity INSERT of this address queues here (T8, S13).
        PERFORM pg_catalog.pg_advisory_xact_lock(
            pg_catalog.hashtextextended('admin-identity-email:' || pg_catalog.lower(c.email::text), 0));

        -- 2. An address that already has an identity is left alone: the group's later rows never
        -- join an identity an earlier row birthed (the M14-06 security note).
        IF EXISTS (SELECT 1
                     FROM public.admin_identities AS i
                    WHERE i.email OPERATOR(public.=) c.email) THEN
            CONTINUE;
        END IF;
        -- 3. The group's candidates, locked in id order; the ids of those whose address passes the
        -- address rule are kept. A row committed while this waited is not among them and is not chosen
        -- (it is the next call's).
        v_locked := '{}';
        FOR r IN
            SELECT a.id, a.email
              FROM public.admin_users AS a
             WHERE a.email OPERATOR(public.=) c.email
               AND a.identity_id IS NULL
               AND a.unlinked_at IS NULL
             ORDER BY a.id
               FOR UPDATE
        LOOP
            -- 6b. THE ADDRESS RULE (M14-08a, 00076's predicate): only a row whose address is one
            -- internal/mail would send to -- mail.ValidRecipient: at most 254 octets, no "=?", an
            -- addr-spec net/mail parses back to itself in printable ASCII -- may be chosen. A homoglyph
            -- row (U+0130, U+212A) is not, even as the group's oldest active one. NULL fails closed.
            IF (pg_catalog.octet_length(r.email::text) <= 254
                AND pg_catalog.strpos(r.email::text, '=?') = 0
                AND r.email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$') IS NOT TRUE THEN
                CONTINUE;
            END IF;
            v_locked := pg_catalog.array_append(v_locked, r.id);
        END LOOP;

        -- ... and, read AFTER those locks, a group holding a linked or a refused row is left alone
        -- (00073's rule): a link committed by a writer that does not take the address's lock
        -- (identity_link_legacy) while this waited is seen here.
        IF EXISTS (SELECT 1
                     FROM public.admin_users AS o
                    WHERE o.email OPERATOR(public.=) c.email
                      AND (o.identity_id IS NOT NULL OR o.unlinked_at IS NOT NULL)) THEN
            CONTINUE;
        END IF;

        -- 4. The choice, among the LOCKED rule-passing rows only, read after the wait: no status or
        -- digest of a row it may choose can change before the link. None (every row of the group fails
        -- the address rule): the group births nothing.
        SELECT a.id, a.tenant_id, a.email, a.password_hash
          INTO v_row, v_tenant, v_email, v_digest
          FROM public.admin_users AS a
         WHERE a.id = ANY (v_locked)
           AND a.identity_id IS NULL
           AND a.unlinked_at IS NULL
         ORDER BY (a.status = 'active') DESC, a.created_at, a.tenant_id, a.id
         LIMIT 1;
        IF v_row IS NULL THEN
            CONTINUE;
        END IF;

        -- A digest the identity's CHECK refuses: no row, no address in the message (CLAUDE.md §7).
        IF (v_digest ~ '^\$2[aby]\$(0[4-9]|1[0-4])\$[./A-Za-z0-9]{53}$') IS NOT TRUE THEN
            RAISE EXCEPTION 'identity_reconcile (M14-06b): the chosen admin_users row of an e-mail group carries a password digest that is not bcrypt-shaped (00018''s CHECK); an identity cannot be born with it. Reset that account''s password, then run it again. Nothing was changed.'
                USING ERRCODE = 'object_not_in_prerequisite_state';
        END IF;

        -- 5. The identity. An identity under the address committed meanwhile by a writer outside the
        -- lock: nothing is inserted, the group is left alone.
        INSERT INTO public.admin_identities (email, password_hash)
        VALUES (v_email, v_digest)
        ON CONFLICT (email) DO NOTHING
        RETURNING id INTO v_identity;
        IF v_identity IS NULL THEN
            CONTINUE;
        END IF;

        -- 6. The link: the chosen row alone, by its own key and tenant (§4.5's belt).
        UPDATE public.admin_users AS a
           SET identity_id = v_identity,
               linked_via  = 'reconcile',
               linked_at   = pg_catalog.now()
         WHERE a.id = v_row
           AND a.tenant_id = v_tenant
           AND a.identity_id IS NULL
           AND a.unlinked_at IS NULL;
        GET DIAGNOSTICS v_n = ROW_COUNT;
        IF (v_n = 1) IS NOT TRUE THEN
            RAISE EXCEPTION 'identity_reconcile (M14-06b): the chosen row changed under its lock; an identity would be left without its link. Nothing was changed.'
                USING ERRCODE = 'object_not_in_prerequisite_state';
        END IF;
        v_linked := v_linked + 1;
    END LOOP;
    RETURN v_linked;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION public.identity_reconcile() OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_reconcile() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_reconcile() TO tappa_owner;

-- ---------------------------------------------------------------------------
-- 2. DEPLOY 2: THE CALL (ADR 0031 §8 step 3)
-- ---------------------------------------------------------------------------
SET LOCAL lock_timeout = '1s';
SELECT public.identity_reconcile();

-- ---------------------------------------------------------------------------
-- 3. THE POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_users a
                WHERE a.email IS NOT NULL AND pg_catalog.btrim(a.email::text) <> ''
                  AND a.identity_id IS NULL AND a.unlinked_at IS NULL
                  AND a.created_at < pg_catalog.now()
                  AND pg_catalog.octet_length(a.email::text) <= 254
                  AND pg_catalog.strpos(a.email::text, '=?') = 0
                  AND a.email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$'
                  AND NOT EXISTS (SELECT 1 FROM public.admin_identities i WHERE i.email = a.email)
                  AND NOT EXISTS (SELECT 1 FROM public.admin_users o
                                   WHERE o.email = a.email
                                     AND (o.identity_id IS NOT NULL OR o.unlinked_at IS NOT NULL))) THEN
        RAISE EXCEPTION 'migration 00082 postcondition: an e-mail group the reconciliation should have taken holds no identity; identity_reconcile saw fewer rows than this runner (is tappa_identitydefiner still BYPASSRLS?) (ADR 0031 5.8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM public.admin_users a
                WHERE a.linked_via = 'reconcile' AND a.identity_id IS NOT NULL
                  AND a.password_hash IS DISTINCT FROM
                      (SELECT i.password_hash FROM public.admin_identities i WHERE i.id = a.identity_id)) THEN
        RAISE EXCEPTION 'migration 00082 postcondition: a row linked by the reconciliation does not carry its identity''s password digest (ADR 0031 5.1). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM public.admin_users
                WHERE linked_via = 'reconcile' AND identity_id IS NOT NULL
                GROUP BY identity_id HAVING count(*) > 1) THEN
        RAISE EXCEPTION 'migration 00082 postcondition: an identity is linked through more than one reconcile row; the reconciliation links one row per e-mail group (ADR 0031 5.8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM public.admin_users a
                 JOIN public.admin_identities i ON i.id = a.identity_id
                WHERE a.linked_via = 'reconcile'
                  AND (pg_catalog.octet_length(i.email::text) <= 254
                       AND pg_catalog.strpos(i.email::text, '=?') = 0
                       AND i.email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$') IS NOT TRUE) THEN
        RAISE EXCEPTION 'migration 00082 postcondition: an identity the reconciliation linked carries an address mail.ValidRecipient refuses (M14-08a, ADR 0031 5.8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                WHERE p.oid = 'public.identity_reconcile()'::pg_catalog.regprocedure
                  AND (NOT p.prosecdef
                       OR p.proowner <> 'tappa_identitydefiner'::pg_catalog.regrole
                       OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
                       OR p.proacl IS NULL
                       OR EXISTS (SELECT 1 FROM pg_catalog.aclexplode(p.proacl) x
                                   WHERE x.privilege_type = 'EXECUTE'
                                     AND x.grantee NOT IN (p.proowner, 'tappa_owner'::pg_catalog.regrole)))) THEN
        RAISE EXCEPTION 'migration 00082 postcondition: identity_reconcile is not a SECURITY DEFINER of tappa_identitydefiner with the pinned search_path and EXECUTE for tappa_owner alone (ADR 0031 5.2)';
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
        RAISE EXCEPTION 'migration 00082 Down: the running role is neither superuser nor BYPASSRLS; under FORCE ROW LEVEL SECURITY it would see no linked row and undo nothing. Run the migrations as tappa_owner. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

SET LOCAL lock_timeout = '1s';
-- +goose StatementBegin
DO $$
DECLARE
    reconciled uuid[];
    theirs     uuid[];
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_constraint c
                WHERE c.contype = 'f'
                  AND c.confrelid = 'public.admin_identities'::pg_catalog.regclass
                  AND c.confdeltype NOT IN ('a', 'r')) THEN
        RAISE EXCEPTION 'migration 00082 Down: a foreign key onto admin_identities cascades or nulls a delete; deleting the reconciliation''s identities would take another table''s rows with them. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    SELECT coalesce(pg_catalog.array_agg(l.id), '{}'), coalesce(pg_catalog.array_agg(DISTINCT l.identity_id), '{}')
      INTO reconciled, theirs
      FROM (SELECT a.id, a.identity_id
              FROM public.admin_users a
             WHERE a.linked_via = 'reconcile' AND a.identity_id IS NOT NULL
             ORDER BY a.id
               FOR UPDATE) l;

    -- Their identities, locked in id order BEFORE the checks below read them (00073's Down: a
    -- verification, a login or a window committed between an unlocked check and the DELETE would be
    -- lost with the identity).
    PERFORM 1 FROM public.admin_identities WHERE id = ANY (theirs) ORDER BY id FOR UPDATE;

    IF EXISTS (SELECT 1 FROM public.admin_users a
                WHERE a.identity_id = ANY (theirs) AND a.linked_via IS DISTINCT FROM 'reconcile')
       OR EXISTS (SELECT 1 FROM public.admin_users a
                   WHERE a.unlinked_at IS NOT NULL
                     AND (a.linked_via = 'reconcile'
                          OR EXISTS (SELECT 1 FROM public.admin_identities i
                                      WHERE i.id = ANY (theirs) AND i.email = a.email)))
       OR EXISTS (SELECT 1 FROM public.admin_identities i
                   WHERE i.id = ANY (theirs)
                     AND (i.email_verified_at IS NOT NULL OR i.confirm_opened_at IS NOT NULL
                          OR i.last_login_at IS NOT NULL OR i.last_used_tenant IS NOT NULL)) THEN
        RAISE EXCEPTION 'migration 00082 Down: an identity the reconciliation made has lived since (another link, a refusal, a verified address, a confirmation window or a login); deleting it would drop that. Go back with the previous binary, which runs on this schema (ADR 0031 8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    UPDATE public.admin_users
       SET identity_id = NULL, linked_via = NULL, linked_at = NULL
     WHERE id = ANY (reconciled);

    BEGIN
        DELETE FROM public.admin_identities WHERE id = ANY (theirs);
    EXCEPTION WHEN foreign_key_violation THEN
        RAISE EXCEPTION 'migration 00082 Down: a row of another table points at an identity the reconciliation made; deleting it would drop that row''s subject. Go back with the previous binary (ADR 0031 8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END;
END
$$;
-- +goose StatementEnd

DROP FUNCTION public.identity_reconcile();

-- NO REVOKE: this file granted nothing. The grants the function used stay with the files that made
-- them (their Downs revoke them):
--   00071/00072  admin_users      SELECT (id, tenant_id, email, status, created_at, identity_id,
--                                         unlinked_at), UPDATE (identity_id, linked_via, linked_at)
--                admin_identities SELECT (id, email)
--   00074        admin_users      SELECT (password_hash)
--                admin_identities INSERT (email, password_hash)
