-- 00079 -- M14-18: the confirmation window reaches the account page. Two functions join ADR 0031
-- §5.2's closed list (the user's decision of 2026-10-27, ADR 0031 §5.2's note of that day):
--
--   identity_confirm_window(p_session_hash)  STABLE, keyed by the live session: the window of the
--                                            person behind it -- verified or not, its scope, its end
--                                            -- and, while it is open, the memberships on its list,
--                                            the rows refused inside it, and whether a listed row is
--                                            its business's last active owner. At most 50 rows.
--   identity_keep_all(p_session_hash)        keyed by the live session: "Keep all" (T11), ONE click --
--                                            the window's three columns NULL in ONE UPDATE, one
--                                            confirm_kept event. Nothing else.
--
-- WHY (M14-18's STOP, 2026-10-26). 00069-00075 OPEN the window (identity_verify_email, the two reset
-- consumptions; 00077's invitation) and act on it (identity_unlink_backfilled,
-- identity_relink_refused), but nothing READ it for tappa_app -- which holds nothing on
-- admin_identities and cannot read linked_via / linked_at / unlinked_at (R16) -- and nothing CLOSED
-- it: the list, the "last active owner" warning, the Undo of a refused row and "Keep all" could not be
-- drawn or pressed.
--
-- NO NEW GRANT. Every column both bodies read or write is already tappa_identitydefiner's: on
-- admin_identities SELECT (id, email, email_verified_at, confirm_opened_at, confirm_open_until,
-- confirm_scope) and UPDATE (confirm_opened_at, confirm_open_until, confirm_scope) since 00072; on
-- admin_users SELECT (id, tenant_id, email, role, status, identity_id, linked_via, linked_at,
-- unlinked_at) since 00071/00072; on tenants SELECT (id, name) since 00071; on admin_sessions SELECT
-- and UPDATE (revoked_at) -- the row lock -- since 00071; on admin_identity_events INSERT since 00072.
-- The precondition refuses a database where one is missing. So the Down REVOKEs nothing: a column
-- REVOKE here would take another migration's grant away (M14-17's measured hazard).
--
-- THE EVENT SET GROWS BY ONE: 'confirm_kept' (admin_identity_events_event_known, 00072). It is the
-- named mitigation of "Keep all"'s accepted risk (ADR 0031 §5.8's 2026-10-27 note): someone holding a
-- stolen live session can close the window and hide the list from the person; a reset revokes every
-- session and opens a new window, and the press leaves this row. detail is {"scope": <the closed
-- window's scope>} -- 00072's closed keys, no tenant, no address.
--
-- THE THREE COLUMNS ARE NULL TOGETHER, by the statement and by 00069's CHECK
-- admin_identities_confirm_window_whole: identity_keep_all writes them in ONE UPDATE, and a write that
-- left one behind would be refused by that CHECK. After it every window-keyed function fails closed:
-- identity_unlink_backfilled and identity_relink_refused test `v_until > v_now`, NULL is not TRUE.
-- email_verified_at is NOT touched -- a verified address stays verified.
--
-- THE BELT (ADR 0031 §5.2, identitybelt_test.go). Both bodies are written in M14-04's SQL subset:
--   * the session by its own key (admin_sessions.token_hash = p_session_hash), joined by both halves
--     of the composite key -- one per function (identitySessionKeyLookups);
--   * identity_confirm_window's membership list, bounded by the session's identity
--     (`a.identity_id = v_identity`, LIMIT 50) -- a NAMED tenant-wide read (identityTenantWideReads);
--   * its refused rows, by the identity's OWN address (`a.email OPERATOR(public.=) v_email`, v_email
--     read from the identity row the session found, `unlinked_at >= confirm_opened_at`, LIMIT 50) --
--     the NAMED e-mail-wide read identity_relink_refused's predicate already is
--     (identityEmailWideReads); each row's business name is then read by that row's tenant;
--   * the last-owner test, per listed row, pinned to that row's tenant (`o.tenant_id = r.tenant_id`).
-- No CASE, no IN, no subquery, no CTE: the scope test runs in plpgsql, outside every statement.
--
-- THE ROWS (identity_confirm_window). The FIRST row is the header: tenant_id NULL; verified,
-- scope and open_until the identity's (scope and open_until NULL unless the window is open -- the
-- address verified AND now < confirm_open_until; a closed window is no list). While it is open, after
-- it: each listed membership (refused false; first_verify: linked_via backfill / reconcile / signup;
-- reset: linked_at < confirm_opened_at, every linked_via -- 00072's identity_unlink_backfilled's two
-- scopes, the same test), then each row refused in THIS window (refused true; the rows
-- identity_relink_refused would take back). At most 49 of them, so at most 50 rows in all. A session
-- that is not live, or whose row is not linked, gets 0 rows: an unlinked row has no window.
--   last_active_owner: the listed row is an active owner and its business holds no OTHER active,
--   not-refused owner -- the ADR's "son aktif owner" warning (S4, T1). It is a fact about a business
--   the person belongs to, read under BYPASSRLS for that business alone; it is never a count.
--
-- EXPAND-SAFE (ADR 0031 §8): two new functions and a wider CHECK; no binary before this file calls
-- either, and none writes the new event.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.identity_unlink_backfilled(text,uuid)') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_relink_refused(text,uuid)') IS NULL
       OR pg_catalog.to_regclass('public.admin_identity_events') IS NULL THEN
        RAISE EXCEPTION 'migration 00079 (M14-18) reads the window 00072 opens and acts on, and 00072 is not applied. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regprocedure('public.identity_confirm_window(text)') IS NOT NULL
       OR pg_catalog.to_regprocedure('public.identity_keep_all(text)') IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00079 (M14-18) creates identity_confirm_window and identity_keep_all, and one of them is already there. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- NO NEW GRANT: every cell the two bodies need must already be the definer's.
    IF NOT (pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'email', 'SELECT')
            AND pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'email_verified_at', 'SELECT')
            AND pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'confirm_opened_at', 'UPDATE')
            AND pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'confirm_open_until', 'UPDATE')
            AND pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'confirm_scope', 'UPDATE')
            AND pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'role', 'SELECT')
            AND pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'linked_via', 'SELECT')
            AND pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'linked_at', 'SELECT')
            AND pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'unlinked_at', 'SELECT')
            AND pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.tenants', 'name', 'SELECT')
            AND pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identity_events', 'event', 'INSERT')) THEN
        RAISE EXCEPTION 'migration 00079 (M14-18) grants nothing, and tappa_identitydefiner lacks a column 00071/00072 grant. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The event set: + confirm_kept
-- ---------------------------------------------------------------------------
-- admin_identity_events is tenant-scope-less and small (90 days, bounded writers); the re-added CHECK
-- validates it under this file's lock_timeout.
ALTER TABLE admin_identity_events
    DROP CONSTRAINT admin_identity_events_event_known,
    ADD CONSTRAINT admin_identity_events_event_known CHECK (event IN (
        'login_failed', 'login_limited', 'reset_requested', 'email_verified',
        'link_refused', 'membership_unlinked', 'membership_relinked', 'confirm_kept'));

-- ---------------------------------------------------------------------------
-- 2. identity_confirm_window (ADR 0031 §5.2, §5.8)
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE FUNCTION public.identity_confirm_window(p_session_hash text)
    RETURNS TABLE (
        verified          boolean,
        scope             text,
        open_until        timestamptz,
        tenant_id         uuid,
        tenant_name       text,
        role              text,
        refused           boolean,
        last_active_owner boolean
    )
    LANGUAGE plpgsql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_identity uuid;
    v_email    public.citext;
    v_verified timestamptz;
    v_scope    text;
    v_opened   timestamptz;
    v_until    timestamptz;
    v_now      timestamptz;
    v_listed   boolean;
    v_last     boolean;
    v_name     text;
    v_n        integer;
    r          record;
    q          record;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    SELECT a.identity_id INTO v_identity
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT i.email, i.email_verified_at, i.confirm_scope, i.confirm_opened_at, i.confirm_open_until
      INTO v_email, v_verified, v_scope, v_opened, v_until
      FROM public.admin_identities AS i
     WHERE i.id = v_identity;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The header. A closed window (never opened, kept, or past its end) is no list (NULL fails closed).
    IF (v_verified IS NOT NULL AND v_until > v_now) IS NOT TRUE THEN
        RETURN QUERY SELECT v_verified IS NOT NULL, NULL::text, NULL::timestamptz, NULL::uuid, NULL::text, NULL::text, false, false;
        RETURN;
    END IF;
    RETURN QUERY SELECT true, v_scope, v_until, NULL::uuid, NULL::text, NULL::text, false, false;

    -- NAMED TENANT-WIDE READ: the identity's own memberships, bounded by it and by LIMIT 50; the scope
    -- decides, in plpgsql, which are on the list (identity_unlink_backfilled's test, verbatim).
    v_n := 0;
    FOR r IN SELECT a.id, a.tenant_id, a.role, a.status, a.linked_via, a.linked_at, t.name
               FROM public.admin_users AS a
               JOIN public.tenants AS t ON t.id = a.tenant_id
              WHERE a.identity_id = v_identity
                AND a.unlinked_at IS NULL
              ORDER BY a.linked_at, a.tenant_id
              LIMIT 50
    LOOP
        v_listed := ((v_scope = 'first_verify' AND r.linked_via IN ('backfill', 'reconcile', 'signup'))
                     OR (v_scope = 'reset' AND r.linked_at < v_opened)) IS TRUE;
        IF v_listed AND v_n < 49 THEN
            v_last := false;
            IF r.role = 'owner' AND r.status = 'active' THEN
                PERFORM 1
                   FROM public.admin_users AS o
                  WHERE o.tenant_id = r.tenant_id
                    AND o.role = 'owner'
                    AND o.status = 'active'
                    AND o.unlinked_at IS NULL
                    AND o.id <> r.id;
                v_last := NOT FOUND;
            END IF;
            v_n := v_n + 1;
            RETURN QUERY SELECT true, v_scope, v_until, r.tenant_id, r.name, r.role, false, v_last;
        END IF;
    END LOOP;

    -- NAMED E-MAIL-WIDE READ: the rows refused in THIS window -- identity_relink_refused's predicate
    -- (the identity's own address, unlinked, unlinked_at >= confirm_opened_at), each business's name
    -- read by that row's tenant.
    FOR q IN SELECT a.tenant_id, a.role
               FROM public.admin_users AS a
              WHERE a.email OPERATOR(public.=) v_email
                AND a.identity_id IS NULL
                AND a.unlinked_at >= v_opened
              ORDER BY a.unlinked_at, a.tenant_id
              LIMIT 50
    LOOP
        IF v_n < 49 THEN
            SELECT t.name INTO v_name FROM public.tenants AS t WHERE t.id = q.tenant_id;
            v_n := v_n + 1;
            RETURN QUERY SELECT true, v_scope, v_until, q.tenant_id, v_name, q.role, true, false;
        END IF;
    END LOOP;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_confirm_window(text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_confirm_window(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_confirm_window(text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 3. identity_keep_all (ADR 0031 §5.8, T11)
-- ---------------------------------------------------------------------------
-- ONE click with a LIVE session (not a fresh one -- the user's decision of 2026-10-27): keeping every
-- membership changes no link. Lock order (00072's header): the identity first, then the session,
-- re-checked under the lock. The window must still be open after the locks; then the three columns
-- go NULL in one statement and the event is written. 0 rows: no live linked session, or no open window.
-- +goose StatementBegin
CREATE FUNCTION public.identity_keep_all(p_session_hash text)
    RETURNS TABLE (kept boolean)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_identity uuid;
    v_session  uuid;
    v_tenant   uuid;
    v_scope    text;
    v_until    timestamptz;
    v_now      timestamptz;
BEGIN
    SELECT s.id, s.tenant_id, a.identity_id INTO v_session, v_tenant, v_identity
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT i.confirm_scope, i.confirm_open_until INTO v_scope, v_until
      FROM public.admin_identities AS i
     WHERE i.id = v_identity
       FOR UPDATE;
    -- Re-checked under the identity lock (READ COMMITTED: a reset or a revocation may have committed
    -- while this call waited).
    PERFORM 1
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.id = v_session
       AND s.tenant_id = v_tenant
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF s;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    v_now := pg_catalog.clock_timestamp();
    IF (v_until > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;

    -- The three columns together, in ONE statement (00069's admin_identities_confirm_window_whole
    -- would refuse any other shape).
    UPDATE public.admin_identities AS i
       SET confirm_opened_at = NULL, confirm_open_until = NULL, confirm_scope = NULL
     WHERE i.id = v_identity;
    INSERT INTO public.admin_identity_events (identity_id, event, detail)
    VALUES (v_identity, 'confirm_kept', pg_catalog.jsonb_build_object('scope', v_scope));

    RETURN QUERY SELECT true;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_keep_all(text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_keep_all(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_keep_all(text) TO tappa_app;

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- The event set goes back to 00072's, so a confirm_kept row would break the CHECK: refuse while one is
-- there (00072's Down refuses on its rows the same way). Nothing is REVOKEd -- this file granted
-- nothing (see the header).
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_identity_events AS e WHERE e.event = 'confirm_kept') THEN
        RAISE EXCEPTION 'migration 00079 Down: admin_identity_events holds confirm_kept rows, which 00072''s event set refuses. They age out after 90 days (identity_sweep_events). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd
DROP FUNCTION public.identity_keep_all(text);
DROP FUNCTION public.identity_confirm_window(text);
ALTER TABLE admin_identity_events
    DROP CONSTRAINT admin_identity_events_event_known,
    ADD CONSTRAINT admin_identity_events_event_known CHECK (event IN (
        'login_failed', 'login_limited', 'reset_requested', 'email_verified',
        'link_refused', 'membership_unlinked', 'membership_relinked'));
