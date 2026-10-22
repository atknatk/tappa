-- 00071 -- M14-04: the identity resolver and the four session functions of ADR 0031 §5.2 -- the
-- first members of the CLOSED identity_* list -- and the column grants they need, nothing more:
--
--   resolve_identity_by_email(citext)                      tappa_resolver   (md.7, read-only, <= 1 row)
--   identity_open_session(uuid, uuid, text)                tappa_identitydefiner  (named exception 1)
--   identity_memberships(text)                             tappa_identitydefiner  (live session)
--   identity_switch_session(text, uuid, text)              tappa_identitydefiner  (live session, §5.4)
--   identity_revoke_sessions(text, boolean)                tappa_identitydefiner  (live session)
--
-- and, in a block of its own (section 6), tappa_app's table-level INSERT on admin_sessions narrowed
-- to a column list without authenticated_at (orchestrator decision on M14-03's audit).
--
-- NORMATIVE SOURCE: docs/adr/0031-tek-kimlik-cok-isletme.md §5.1 (authenticated_at: written by the
-- definer, copied from the old session's ROW on a switch, never from the client -- R15), §5.2 (the
-- closed list, the signatures, the verb x column matrix, "every tenant-table statement inside a
-- BYPASSRLS body carries an explicit tenant_id predicate", "a function that writes to a tenant
-- writes that tenant's audit_log and the row names no other tenant"), §5.4 (the switch), §5.6 (the
-- login, R14's has_active_membership), §6 (the third §4.5 exception); the card is
-- docs/plan/m14-tek-kimlik.md -> M14-04. M14-05 (00072) adds the rest of the list and redefines
-- resolve_admin_by_email; M14-06b adds identity_reconcile() -- the one function whose EXECUTE is
-- tappa_owner's, not tappa_app's (ADR 0031 §5.2, ONAY note 1; internal/db's catalogue test already
-- names that exception and waits for the function).
--
-- EVERY FUNCTION HERE (ADR 0031 §5.2): SECURITY DEFINER, `SET search_path = pg_catalog, pg_temp`,
-- schema-qualified bodies, no dynamic SQL, a fixed result, REVOKE ALL FROM PUBLIC, EXECUTE granted
-- to tappa_app alone. Owners: tappa_resolver for the md.7 resolver, tappa_identitydefiner (NOLOGIN,
-- BYPASSRLS, NOSUPERUSER, no member, column grants alone -- 00069's precondition) for the rest.
--
-- 🔴 THE BELT IS THE ONLY BARRIER IN THESE BODIES. Both owners are BYPASSRLS: no policy runs here.
-- So (ADR 0021 §3.2's op_* rule, ADR 0031 §5.2) every reference to a tenant table names its
-- tenant: `x.tenant_id = <the tenant this statement is about>` -- the session row's tenant once the
-- secret has resolved it, the target tenant of a switch -- and every INSERT names tenant_id in its
-- column list. The statements that read an identity's memberships ACROSS tenants are deliberately
-- tenant-wide and are named, each bounded by the identity a secret (or, for the named exception
-- identity_open_session, the Go-typed verified identity) gave: the membership list (LIMIT 50), the
-- login's oldest-membership fallback (LIMIT 1), "sign out everywhere"'s driver, and the resolver's
-- has_active_membership. internal/db's text scan holds every other statement to the rule.
--
-- THE SESSION IS THE KEY (ADR 0031 §5.2): the three session functions find the person only through
-- admin_sessions.token_hash (globally UNIQUE, 00006) -> a LIVE session (revoked_at IS NULL) of an
-- ACTIVE, NOT-REFUSED (unlinked_at IS NULL) membership row -> its identity_id. An e-mail never
-- reaches a writer; resolve_identity_by_email writes nothing (STABLE sql).
--
-- THE PERSON'S LOCK: the switch and "sign out everywhere" take the identity row FOR UPDATE before
-- they touch a session, and only then re-read the session under FOR UPDATE. Both take the two locks
-- in that order, so they cannot deadlock each other, and N parallel switches of one session queue on
-- the identity: the first revokes the session it came from, every later one re-reads it revoked
-- (READ COMMITTED: each statement a fresh snapshot) and returns 0 rows -- exactly one new session.
--
-- AUDIT ROWS ARE BYTE-IDENTICAL TO THE ORDINARY ONES (ADR 0031 §5.4 step 6, bulgu 17): a sign-out in
-- the tenant left is internal/handler's Logout row and a sign-in in the tenant entered is its
-- login-success row -- action, actor, target and the detail {"outcome":"ok","session_id":"<id>"} --
-- with NO via, no other tenant's id or name, no membership count and no identity_id. Neither tenant's
-- owner learns from its own log that the person has another business. internal/handler's
-- identityaudit_db_test.go compares them with the rows the handler's own events write.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.admin_identities') IS NULL
       OR NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'tappa_identitydefiner'
                                                          AND NOT rolsuper AND rolbypassrls AND NOT rolcanlogin) THEN
        RAISE EXCEPTION 'migration 00071 (M14-04) needs 00069 (admin_identities) and the role tappa_identitydefiner (NOLOGIN, BYPASSRLS, NOSUPERUSER). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                WHERE p.pronamespace = 'public'::pg_catalog.regnamespace
                  AND p.proname IN ('resolve_identity_by_email', 'identity_open_session', 'identity_memberships',
                                    'identity_switch_session', 'identity_revoke_sessions')) THEN
        RAISE EXCEPTION 'migration 00071 (M14-04) creates the identity resolver and the four session functions, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- The INSERT narrowing below means something only while tappa_app's INSERT on admin_sessions is
    -- the table-level one 00006 gave (a column REVOKE could not narrow a table grant).
    IF NOT pg_catalog.has_table_privilege('tappa_app', 'public.admin_sessions', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00071 (M14-04): tappa_app no longer holds 00006''s table-level INSERT on admin_sessions; someone changed it by hand. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The column grants (ADR 0031 §5.2's matrix -- the part these five functions use)
-- ---------------------------------------------------------------------------
-- Each column is here because a statement below reads or writes it; internal/db's
-- identitycatalog_test.go revokes them one at a time and shows a call refused without it, and holds
-- the whole list inside the ADR's matrix. tappa_identitydefiner holds nothing else (00069 gave it
-- none): no table-level privilege anywhere, no DELETE, no UPDATE of an e-mail.
--
-- admin_identities: the person's lock (FOR UPDATE needs an UPDATE privilege), last_used_tenant (the
--   login's landing, the switch's hint) and last_login_at. Not email, not password_hash: no function
--   of this file reads the address or the digest -- the login compares the digest in Go, from the
--   resolver.
-- admin_users: the membership's identity, state and role; its last_login_at, stamped as an ordinary
--   login stamps it (MarkAdminLoggedIn) so the tenant entered sees what it sees after any login.
-- admin_sessions: the session the secret names, the new one, the revocation.
-- tenants: the membership list's name and suspension flag (K-10).
-- audit_log: the ordinary rows.
SET LOCAL lock_timeout = '1s';
GRANT SELECT (id, last_used_tenant), UPDATE (last_used_tenant, last_login_at)
    ON admin_identities TO tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
GRANT SELECT (id, tenant_id, role, status, created_at, identity_id, unlinked_at), UPDATE (last_login_at)
    ON admin_users TO tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
GRANT SELECT (id, tenant_id, admin_user_id, token_hash, revoked_at, authenticated_at),
      INSERT (tenant_id, admin_user_id, token_hash, authenticated_at),
      UPDATE (revoked_at)
    ON admin_sessions TO tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
GRANT SELECT (id, name, suspended_at) ON tenants TO tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
GRANT INSERT (tenant_id, actor_id, action, target, detail) ON audit_log TO tappa_identitydefiner;

-- tappa_resolver (ADR 0031 §5.2, T6): what resolve_identity_by_email returns and filters on. The
-- admin_users pair is the ADR's "yeni (identity_id, unlinked_at)" -- M14-05's redefined
-- resolve_admin_by_email reads the same two.
SET LOCAL lock_timeout = '1s';
GRANT SELECT (id, email, password_hash, email_verified_at) ON admin_identities TO tappa_resolver;
SET LOCAL lock_timeout = '1s';
GRANT SELECT (identity_id, unlinked_at) ON admin_users TO tappa_resolver;

-- ---------------------------------------------------------------------------
-- 2. resolve_identity_by_email (ADR 0002 md.7; ADR 0031 §5.2, §5.6)
-- ---------------------------------------------------------------------------
-- <= 1 row, STRUCTURALLY: admin_identities.email is globally UNIQUE (00069) -- md.7 (ii) comes back
-- for the login. The citext equality is OPERATOR(public.=): public is not on the pinned path, and a
-- bare `=` there would compare citext as text (00011's measured trap). STABLE sql: it cannot write.
-- has_active_membership (R14): the login's fifth timing case -- correct password, every membership
-- disabled or refused -- is decided without a second round trip; the app makes a dummy call
-- instead of identity_open_session. It is a fact about the person's own memberships, not a count.
-- The digest is returned because the comparison happens in Go (ADR 0031 §5.6), exactly as
-- resolve_admin_by_email returns the row digest today.
-- +goose StatementBegin
CREATE FUNCTION public.resolve_identity_by_email(p_email citext)
    RETURNS TABLE (
        id                    uuid,
        password_hash         text,
        email_verified_at     timestamptz,
        has_active_membership boolean
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    SELECT i.id, i.password_hash, i.email_verified_at,
           EXISTS (SELECT 1
                     FROM public.admin_users AS a
                    WHERE a.identity_id = i.id
                      AND a.status = 'active'
                      AND a.unlinked_at IS NULL)
      FROM public.admin_identities AS i
     WHERE i.email OPERATOR(public.=) p_email;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_identity_by_email(citext) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_identity_by_email(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_identity_by_email(citext) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 3. identity_open_session -- NAMED EXCEPTION 1 (ADR 0031 §5.2, §5.6)
-- ---------------------------------------------------------------------------
-- Not keyed by a secret: the identity's password was verified in Go (the resolver's digest), and
-- the one call path is the verified-identity type there -- the twin of today's Issue(Verified)
-- (00011 OBLIGATION 5). What the database CAN hold it holds: the membership is the identity's own,
-- active and not refused; the session's tenant and admin come from that row; authenticated_at is
-- now() (the password proof just happened); the login's ordinary audit row lands in that tenant.
-- Landing (K-4): the requested business if it is an active membership, else the one used last, else
-- the oldest active membership. 0 rows: no such identity, or no active membership (the app's flat
-- timing path makes a dummy call instead -- R14, S15).
-- +goose StatementBegin
CREATE FUNCTION public.identity_open_session(p_identity_id uuid, p_tenant uuid, p_token_hash text)
    RETURNS TABLE (session_id uuid, tenant_id uuid, admin_user_id uuid)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict error
DECLARE
    v_last    uuid;
    v_tenant  uuid;
    v_admin   uuid;
    v_session uuid;
BEGIN
    IF p_identity_id IS NULL OR p_token_hash IS NULL THEN
        RETURN;
    END IF;

    -- The person's lock (the order every identity_* writer takes it in), and the landing hint.
    SELECT i.last_used_tenant INTO v_last
      FROM public.admin_identities AS i
     WHERE i.id = p_identity_id
       FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF p_tenant IS NOT NULL THEN
        SELECT a.id INTO v_admin
          FROM public.admin_users AS a
         WHERE a.tenant_id = p_tenant
           AND a.identity_id = p_identity_id
           AND a.status = 'active'
           AND a.unlinked_at IS NULL;
        IF FOUND THEN
            v_tenant := p_tenant;
        END IF;
    END IF;
    IF v_tenant IS NULL AND v_last IS NOT NULL THEN
        SELECT a.id INTO v_admin
          FROM public.admin_users AS a
         WHERE a.tenant_id = v_last
           AND a.identity_id = p_identity_id
           AND a.status = 'active'
           AND a.unlinked_at IS NULL;
        IF FOUND THEN
            v_tenant := v_last;
        END IF;
    END IF;
    IF v_tenant IS NULL THEN
        -- NAMED TENANT-WIDE READ (the oldest active membership): bounded by the identity and LIMIT 1.
        SELECT a.id, a.tenant_id INTO v_admin, v_tenant
          FROM public.admin_users AS a
         WHERE a.identity_id = p_identity_id
           AND a.status = 'active'
           AND a.unlinked_at IS NULL
         ORDER BY a.created_at, a.tenant_id
         LIMIT 1;
        IF NOT FOUND THEN
            RETURN;
        END IF;
    END IF;

    INSERT INTO public.admin_sessions AS n (tenant_id, admin_user_id, token_hash, authenticated_at)
    VALUES (v_tenant, v_admin, p_token_hash, pg_catalog.now())
    RETURNING n.id INTO v_session;

    UPDATE public.admin_users AS a
       SET last_login_at = pg_catalog.now()
     WHERE a.id = v_admin
       AND a.tenant_id = v_tenant;

    UPDATE public.admin_identities AS i
       SET last_login_at = pg_catalog.now(),
           last_used_tenant = v_tenant
     WHERE i.id = p_identity_id;

    -- internal/handler's login-success row, byte for byte.
    INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
    VALUES (v_tenant, v_admin, 'admin.login.succeeded', v_admin::text,
            pg_catalog.jsonb_build_object('outcome', 'ok', 'session_id', v_session::text));

    RETURN QUERY SELECT v_session, v_tenant, v_admin;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_open_session(uuid, uuid, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_open_session(uuid, uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_open_session(uuid, uuid, text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 4. identity_memberships (ADR 0031 §5.2; K-5, K-10)
-- ---------------------------------------------------------------------------
-- The person's active memberships, at most 50, the current one first. Keyed by the live session:
-- no input names an identity, so no input can list another identity's memberships. A session whose
-- row is not linked lists itself alone. STABLE: it cannot write. 0 rows: no live session of an
-- active, not-refused membership.
-- +goose StatementBegin
CREATE FUNCTION public.identity_memberships(p_session_hash text)
    RETURNS TABLE (tenant_id uuid, tenant_name text, role text, suspended boolean, is_current boolean)
    LANGUAGE plpgsql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict error
DECLARE
    v_tenant   uuid;
    v_admin    uuid;
    v_identity uuid;
BEGIN
    SELECT s.tenant_id, s.admin_user_id, a.identity_id INTO v_tenant, v_admin, v_identity
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF v_identity IS NULL THEN
        RETURN QUERY
            SELECT t.id, t.name, a.role, t.suspended_at IS NOT NULL, true
              FROM public.admin_users AS a
              JOIN public.tenants AS t ON t.id = v_tenant
             WHERE a.id = v_admin
               AND a.tenant_id = v_tenant;
        RETURN;
    END IF;

    -- NAMED TENANT-WIDE READ (the list itself): bounded by the session's identity and LIMIT 50.
    RETURN QUERY
        SELECT a.tenant_id, t.name, a.role, t.suspended_at IS NOT NULL, a.tenant_id = v_tenant
          FROM public.admin_users AS a
          JOIN public.tenants AS t ON t.id = a.tenant_id
         WHERE a.identity_id = v_identity
           AND a.status = 'active'
           AND a.unlinked_at IS NULL
         ORDER BY (a.tenant_id = v_tenant) DESC, t.name, a.tenant_id
         LIMIT 50;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_memberships(text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_memberships(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_memberships(text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 5. identity_switch_session (ADR 0031 §5.4)
-- ---------------------------------------------------------------------------
-- One call, one transaction:
--   1. the session is live, its row active, not refused and LINKED;
--   2. the target tenant holds an active, not-refused membership of the SAME identity -- else 0 rows,
--      one uniform answer (no business / not a member / disabled are not told apart). The current
--      tenant is not a target (0 rows): a switch that went nowhere would write a sign-out and a
--      sign-in for nothing;
--   3. a new session there, authenticated_at copied from the OLD SESSION'S ROW (R15; NULL stays
--      NULL -- an old session is not freshly authenticated);
--   4. the old session revoked (`revoked_at IS NULL` in the predicate -- admins.sql's rule for every
--      write of revoked_at);
--   5. last_used_tenant; the target membership's last_login_at, as an ordinary login stamps it;
--   6. the two ordinary audit rows.
-- The target is the caller's choice; the AUTHORITY is the session's identity and the target row
-- (ADR 0031 §3) -- the form selects, it does not permit.
-- +goose StatementBegin
CREATE FUNCTION public.identity_switch_session(p_session_hash text, p_target_tenant uuid, p_new_hash text)
    RETURNS TABLE (session_id uuid, tenant_id uuid, admin_user_id uuid)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict error
DECLARE
    v_session  uuid;
    v_tenant   uuid;
    v_admin    uuid;
    v_identity uuid;
    v_auth     timestamptz;
    v_target   uuid;
    v_new      uuid;
BEGIN
    IF p_target_tenant IS NULL OR p_new_hash IS NULL THEN
        RETURN;
    END IF;

    SELECT s.id, s.tenant_id, s.admin_user_id, a.identity_id INTO v_session, v_tenant, v_admin, v_identity
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND OR p_target_tenant = v_tenant THEN
        RETURN;
    END IF;

    -- The person's lock, then the session again under its own lock: a concurrent switch of the same
    -- session that won is seen here as a revoked session.
    PERFORM 1
       FROM public.admin_identities AS i
      WHERE i.id = v_identity
        FOR UPDATE;
    SELECT s.authenticated_at INTO v_auth
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

    SELECT b.id INTO v_target
      FROM public.admin_users AS b
     WHERE b.tenant_id = p_target_tenant
       AND b.identity_id = v_identity
       AND b.status = 'active'
       AND b.unlinked_at IS NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    INSERT INTO public.admin_sessions AS n (tenant_id, admin_user_id, token_hash, authenticated_at)
    VALUES (p_target_tenant, v_target, p_new_hash, v_auth)
    RETURNING n.id INTO v_new;

    UPDATE public.admin_sessions AS o
       SET revoked_at = pg_catalog.now()
     WHERE o.id = v_session
       AND o.tenant_id = v_tenant
       AND o.revoked_at IS NULL;
    IF NOT FOUND THEN
        -- Unreachable under the row lock above; if it ever is, nothing of this switch stands.
        RAISE EXCEPTION 'identity_switch_session: the session left was not revoked'
            USING ERRCODE = 'serialization_failure';
    END IF;

    UPDATE public.admin_users AS b
       SET last_login_at = pg_catalog.now()
     WHERE b.id = v_target
       AND b.tenant_id = p_target_tenant;

    UPDATE public.admin_identities AS i
       SET last_used_tenant = p_target_tenant
     WHERE i.id = v_identity;

    -- internal/handler's Logout row in the tenant left, its login-success row in the tenant entered;
    -- neither names the other.
    INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
    VALUES (v_tenant, v_admin, 'admin.logout', v_admin::text,
            pg_catalog.jsonb_build_object('outcome', 'ok', 'session_id', v_session::text));
    INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
    VALUES (p_target_tenant, v_target, 'admin.login.succeeded', v_target::text,
            pg_catalog.jsonb_build_object('outcome', 'ok', 'session_id', v_new::text));

    RETURN QUERY SELECT v_new, p_target_tenant, v_target;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_switch_session(text, uuid, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_switch_session(text, uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_switch_session(text, uuid, text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 6. identity_revoke_sessions (ADR 0031 §5.2: "Sign out everywhere")
-- ---------------------------------------------------------------------------
-- Every live session of every membership of the session's identity, in every business, the current
-- one kept when p_keep_current. Each revoked session leaves its own tenant an ordinary sign-out row
-- (the membership as actor), nothing more. Returns how many were revoked; 0 rows when the session is
-- not live or its row is not linked (the app then revokes that one admin's sessions as today).
-- +goose StatementBegin
CREATE FUNCTION public.identity_revoke_sessions(p_session_hash text, p_keep_current boolean)
    RETURNS TABLE (revoked integer)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict error
DECLARE
    v_session  uuid;
    v_identity uuid;
    v_n        integer := 0;
    m          record;
    k          record;
BEGIN
    IF p_keep_current IS NULL THEN
        RETURN;
    END IF;

    SELECT s.id, a.identity_id INTO v_session, v_identity
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

    PERFORM 1
       FROM public.admin_identities AS i
      WHERE i.id = v_identity
        FOR UPDATE;

    -- NAMED TENANT-WIDE READ (the driver): the identity's memberships; every write below is pinned
    -- to one of them, tenant and row.
    FOR m IN
        SELECT a.id, a.tenant_id
          FROM public.admin_users AS a
         WHERE a.identity_id = v_identity
         ORDER BY a.tenant_id, a.id
    LOOP
        FOR k IN
            UPDATE public.admin_sessions AS x
               SET revoked_at = pg_catalog.now()
             WHERE x.tenant_id = m.tenant_id
               AND x.admin_user_id = m.id
               AND x.revoked_at IS NULL
               AND (NOT p_keep_current OR x.id <> v_session)
            RETURNING x.id
        LOOP
            INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
            VALUES (m.tenant_id, m.id, 'admin.logout', m.id::text,
                    pg_catalog.jsonb_build_object('outcome', 'ok', 'session_id', k.id::text));
            v_n := v_n + 1;
        END LOOP;
    END LOOP;

    RETURN QUERY SELECT v_n;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_revoke_sessions(text, boolean) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_revoke_sessions(text, boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_revoke_sessions(text, boolean) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 7. tappa_app's INSERT on admin_sessions, narrowed (ADR 0031 §5.1, R15)
-- ---------------------------------------------------------------------------
-- 00006 gave tappa_app a TABLE-level INSERT on admin_sessions, which covers authenticated_at: the
-- application could open a session stamped `'infinity'` and pass every freshness predicate M14-05
-- writes (identity_link_legacy, identity_unlink_backfilled: authenticated_at in the last five
-- minutes) forever. ADR 0031 §5.1: the definer writes it, the client never. Measured before writing
-- (db/queries + every Go file): the one production INSERT is CreateAdminSession, which names
-- (tenant_id, admin_user_id, token_hash) and takes id, created_at and authenticated_at from their
-- DEFAULTs -- the binary before this file keeps working. `id` is granted as well, by 00017's
-- reasoning for admin_users: the RLS and composite-FK tests across internal/db insert sessions with
-- an explicit id as tappa_app, and an id cannot carry the harm (the primary key, bound to the
-- caller's own tenant by WITH CHECK). created_at, last_used_at, revoked_at and authenticated_at are
-- outside: none may be chosen at birth by the application.
SET LOCAL lock_timeout = '1s';
REVOKE INSERT ON admin_sessions FROM tappa_app;
GRANT INSERT (id, tenant_id, admin_user_id, token_hash) ON admin_sessions TO tappa_app;

-- ---------------------------------------------------------------------------
-- 8. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    f record;
BEGIN
    FOR f IN SELECT p.oid, p.proname, p.prosecdef, p.proconfig, pg_catalog.pg_get_userbyid(p.proowner) AS owner
               FROM pg_catalog.pg_proc p
              WHERE p.pronamespace = 'public'::pg_catalog.regnamespace
                AND p.proname IN ('resolve_identity_by_email', 'identity_open_session', 'identity_memberships',
                                  'identity_switch_session', 'identity_revoke_sessions') LOOP
        IF NOT f.prosecdef
           OR f.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
           OR (f.proname = 'resolve_identity_by_email') <> (f.owner = 'tappa_resolver')
           OR (f.proname <> 'resolve_identity_by_email') <> (f.owner = 'tappa_identitydefiner')
           OR pg_catalog.has_function_privilege('public', f.oid, 'EXECUTE')
           OR pg_catalog.has_function_privilege('tappa_operator', f.oid, 'EXECUTE')
           OR pg_catalog.has_function_privilege('tappa_opdefiner', f.oid, 'EXECUTE')
           OR NOT pg_catalog.has_function_privilege('tappa_app', f.oid, 'EXECUTE') THEN
            RAISE EXCEPTION 'migration 00071 postcondition: % is not the ADR''s shape', f.proname;
        END IF;
    END LOOP;
    IF pg_catalog.has_column_privilege('tappa_app', 'public.admin_sessions', 'authenticated_at', 'INSERT')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.admin_sessions', 'INSERT')
       OR NOT pg_catalog.has_column_privilege('tappa_app', 'public.admin_sessions', 'token_hash', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00071 postcondition: tappa_app''s INSERT on admin_sessions is not (id, tenant_id, admin_user_id, token_hash)';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- tappa_app's 00070 INSERT: the table-level one of 00006 (a table-level REVOKE also clears the
-- column-level grants, so the catalogue is 00070's byte for byte).
REVOKE INSERT ON admin_sessions FROM tappa_app;
GRANT INSERT ON admin_sessions TO tappa_app;

DROP FUNCTION public.identity_revoke_sessions(text, boolean);
DROP FUNCTION public.identity_switch_session(text, uuid, text);
DROP FUNCTION public.identity_memberships(text);
DROP FUNCTION public.identity_open_session(uuid, uuid, text);
DROP FUNCTION public.resolve_identity_by_email(citext);

SET LOCAL lock_timeout = '1s';
REVOKE SELECT (identity_id, unlinked_at) ON admin_users FROM tappa_resolver;
SET LOCAL lock_timeout = '1s';
REVOKE SELECT (id, email, password_hash, email_verified_at) ON admin_identities FROM tappa_resolver;
SET LOCAL lock_timeout = '1s';
REVOKE INSERT (tenant_id, actor_id, action, target, detail) ON audit_log FROM tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
REVOKE SELECT (id, name, suspended_at) ON tenants FROM tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
REVOKE SELECT (id, tenant_id, admin_user_id, token_hash, revoked_at, authenticated_at),
       INSERT (tenant_id, admin_user_id, token_hash, authenticated_at),
       UPDATE (revoked_at)
    ON admin_sessions FROM tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
REVOKE SELECT (id, tenant_id, role, status, created_at, identity_id, unlinked_at), UPDATE (last_login_at)
    ON admin_users FROM tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
REVOKE SELECT (id, last_used_tenant), UPDATE (last_used_tenant, last_login_at)
    ON admin_identities FROM tappa_identitydefiner;
