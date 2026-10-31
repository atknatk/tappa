-- 00085 -- T183: a panel session ENDS ON THE SERVER 12 hours after its password proof (ADR 0035; the
-- user's decision of 2026-10-31, "Sunucuda 12 saat mutlak" -- OWASP's absolute session timeout).
--
-- THE PROBLEM, MEASURED. The panel cookie's Max-Age of 12 hours (internal/adminauth/cookie.go) was a
-- browser hint: admin_sessions carries created_at, last_used_at, revoked_at and authenticated_at but no
-- expiry, and nothing on the server compared any of them, so a kept or copied cookie (curl, a scraped
-- value, a browser profile copied off a disk) stayed valid until somebody revoked it. Production held
-- 48 un-revoked sessions in one business on 2026-10-31.
--
-- THE RULE. A session is live only while `authenticated_at > now() - interval '12 hours'`. The clock is
-- the PASSWORD PROOF, not the row's birth: a switch of business births a new row but copies the old
-- row's authenticated_at (00081's identity_switch_session, under the session's lock), so switching does
-- NOT restart the clock -- with created_at it did, and a person with two businesses (or whoever held the
-- cookie) could switch once every eleven hours and never end (the orchestrator's decision, 2026-10-31,
-- closing ADR 0035's first draft's open question). Where the orchestrator wrote COALESCE(authenticated_at,
-- created_at), this file reads NULL as ended instead, for the measured reason below. The switched
-- cookie's Max-Age is the remaining time (internal/adminauth: Issued.Left), so the browser and the
-- server still end at the same moment.
--
-- NULL IS ENDED (ADR 0031 §5.1: "NULL = eski / süresi geçmiş ... ileride gelecek mutlak ömür bunu
-- fail-closed okur"). Measured, every session INSERT after this file writes a non-NULL authenticated_at:
--   - CreateAdminSession (db/queries/admins.sql; tappa_app's INSERT is (id, tenant_id, admin_user_id,
--     token_hash) -- it cannot name the column) takes 00069's DEFAULT now();
--   - identity_open_session writes now();
--   - identity_switch_session copies the origin's, and the origin passed `authenticated_at > bound`
--     on its lookup below, so it is not NULL (and no role but the definer's revocation writes the row).
-- NULL rows are therefore exactly the rows born before 00069 (Deploy 1, 2026-10-28 -- every one more
-- than 12 hours old at this file's deploy) and the rows switched from one of them since (a switch copies
-- NULL; TestIdentitySwitch_ANewSessionTheOldRevokedTheProofCarried measured it, and since this file
-- measures the refusal). Read as "created_at instead" the second kind would restart its clock at every
-- switch -- the chain this file closes -- so NULL is read as ended. The deploy's read-only check counts them first (deploy/README.md, T183).
--
-- WHERE THE CHECK LIVES -- IN THE DATABASE, ON EVERY PATH A SESSION HASH REACHES (the security review's
-- point: a resolver-only check let the definers below treat an expired session as alive):
--   1. resolve_admin_session_by_token_hash (00011, md.7, tappa_resolver): RETURNED, NOT FILTERED -- the
--      resolver's principle since 00011. Its revoked_at column becomes the session's END: the revocation
--      if there is one, now() for a session with no proof, authenticated_at + 12 h once that has passed.
--      Same signature, same four columns, authenticated_at read and never returned. So every caller that
--      refuses a revoked session refuses an ended one -- the binary BEFORE this file included
--      (adminauth.Verify's `rs.RevokedAt != nil`): the migrate Job runs before the rollout and the old
--      pod is already covered. Expand-only.
--   2. TouchAdminSession (db/queries/admins.sql, the new binary's query text): the same predicate, so a
--      Go caller that forgot the resolver's answer still gets 0 rows (tappa_app reads the table).
--   3. The twelve identity_* definers that find a session by its hash and take it at its word (ADR 0031
--      §5.2's "canlı oturum" key; identitybelt_test.go's identitySessionKeyLookups counts them): the
--      predicate on each lookup by token_hash -- identity_memberships, _switch_session,
--      _revoke_sessions, _password_for_session, _set_password, _request_verification, _verify_email,
--      _accept_invite, _claim_signup, _confirm_window, _keep_all, _link_by_email. Same signatures
--      (CREATE OR REPLACE; SECURITY DEFINER, search_path, owner, REVOKE PUBLIC and the GRANT restated,
--      ADR 0031 §5.2 T5). The bound is a DECLARE variable, not an expression in the WHERE: the belt
--      (identitybelt_test.go) admits no function call or keyword in a conjunct, and 00072's `v_fresh` is
--      the precedent. The re-reads under a lock (by id) are left alone: they guard against a revocation
--      committed while the call waited, and the clock read at the call's start is the one a request is
--      judged by.
--      The other three session-keyed definers are NOT redefined because they already hold a TIGHTER
--      bound on the same column: identity_unlink_backfilled, identity_relink_refused and
--      identity_link_legacy (both its sessions) accept a session only while `authenticated_at >
--      now() - 5 minutes` (R3, 00072/00079) -- five minutes inside twelve hours, NULL refused. A
--      second, looser predicate there could not be proven by any test (it never decides), so it is
--      not written; adminsessionlifetime_test.go holds both kinds.
--   Not changed, on purpose: identity_open_session and identity_accept_invite_new BIRTH a session (no
--   session key); identity_consume_reset / identity_consume_row_reset only revoke; the revocation writers
--   (RevokeAdminSession*, identity_revoke_sessions' loop) still stamp an ended row -- harmless (the
--   trigger keeps the stamp monotonic), and exactly what they did before this file.
--
-- THE GRANT -- the only privilege change: SELECT (authenticated_at) on admin_sessions to tappa_resolver
-- (accepted by the orchestrator, 2026-10-31; ADR 0035, ADR 0002's md.7 note). tappa_identitydefiner has
-- read the column since 00071. The resolver never returns it.
--
-- NO SCHEMA CHANGE ON THE HOT TABLE: no column, no index, no data written (the end is computed; no
-- revoked_at is stamped in bulk, no row is deleted). A column GRANT takes no lock on admin_sessions
-- (measured: pg_locks shows none); CREATE OR REPLACE FUNCTION none either. The lock_timeout is the
-- precedent's, kept so a stuck catalogue row fails this file fast.
--
-- USER-VISIBLE EFFECT AT DEPLOY: every session whose password proof is older than 12 hours (or absent)
-- stops resolving -- the person signs in again. Normal browsers already dropped those cookies; what ends
-- is a kept or copied one, and a session switched into more than 12 hours after its sign-in.
--
-- THE DOWN gives back the bodies this file found, byte for byte (pg_get_functiondef at 00084), and takes
-- the resolver's cell back.

-- +goose Up
SET LOCAL lock_timeout = '1s';
-- The one privilege change (ADR 0035): the resolver reads the proof's moment. It never returns it.
GRANT SELECT (authenticated_at) ON admin_sessions TO tappa_resolver;

-- md.7's panel resolver (owner tappa_resolver).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.resolve_admin_session_by_token_hash(p_token_hash text)
 RETURNS TABLE(id uuid, tenant_id uuid, admin_user_id uuid, revoked_at timestamp with time zone)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
        -- <=1 row: token_hash is globally UNIQUE. Fixed column list, no SELECT *.
        -- token_hash is the INPUT and is never returned (section 4.7); created_at
        -- and last_used_at are not needed to resolve.
        -- 00085 (T183, ADR 0035): revoked_at is the session's END -- its revocation, or, once
        -- 12 hours from its password proof (authenticated_at) have passed, that moment. A session
        -- with no proof on record (NULL: a row from before 00069, or switched from one) has ended
        -- already: ADR 0031 §5.1 reads NULL fail-closed, and now() stands in for the moment.
        -- RETURNED, NOT FILTERED (this file's principle since 00011): a session past its lifetime
        -- resolves like a revoked one, so every caller that refuses a revoked session -- the binary
        -- before 00085 included -- refuses it. authenticated_at is read, never returned.
        SELECT s.id, s.tenant_id, s.admin_user_id,
               CASE WHEN s.revoked_at IS NOT NULL THEN s.revoked_at
                    WHEN s.authenticated_at IS NULL THEN pg_catalog.now()
                    WHEN s.authenticated_at <= pg_catalog.now() - interval '12 hours' THEN s.authenticated_at + interval '12 hours'
               END
        FROM public.admin_sessions AS s
        WHERE s.token_hash = p_token_hash;
    $function$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_admin_session_by_token_hash(text) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_admin_session_by_token_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_admin_session_by_token_hash(text) TO tappa_app;

-- identity_memberships: 1 session lookup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_memberships(p_session_hash text)
 RETURNS TABLE(tenant_id uuid, tenant_name text, role text, suspended boolean, is_current boolean)
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict error
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
    v_tenant   uuid;
    v_admin    uuid;
    v_identity uuid;
BEGIN
    SELECT s.tenant_id, s.admin_user_id, a.identity_id INTO v_tenant, v_admin, v_identity
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.authenticated_at > v_proven_after
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
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_memberships(text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_memberships(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_memberships(text) TO tappa_app;

-- identity_switch_session: 1 session lookup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_switch_session(p_session_hash text, p_target_tenant uuid, p_new_hash text)
 RETURNS TABLE(session_id uuid, tenant_id uuid, admin_user_id uuid)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict error
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
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
       AND s.authenticated_at > v_proven_after
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

    -- 00081 (M14-16b): the target membership under the row lock its UPDATE ... last_login_at takes
    -- below anyway (FOR NO KEY UPDATE: no upgrade; never FOR SHARE -- M14-07's deadlock). A disable
    -- that commits first makes this read find nothing; one that comes second waits for this commit
    -- and its revocation sees the new session.
    SELECT b.id INTO v_target
      FROM public.admin_users AS b
     WHERE b.tenant_id = p_target_tenant
       AND b.identity_id = v_identity
       AND b.status = 'active'
       AND b.unlinked_at IS NULL
       FOR NO KEY UPDATE;
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
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_switch_session(text, uuid, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_switch_session(text, uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_switch_session(text, uuid, text) TO tappa_app;

-- identity_revoke_sessions: 1 session lookup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_revoke_sessions(p_session_hash text, p_keep_current boolean)
 RETURNS TABLE(revoked integer)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict error
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
    v_session  uuid;
    v_tenant   uuid;
    v_identity uuid;
    v_n        integer := 0;
    m          record;
    k          record;
BEGIN
    IF p_keep_current IS NULL THEN
        RETURN;
    END IF;

    SELECT s.id, s.tenant_id, a.identity_id INTO v_session, v_tenant, v_identity
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.authenticated_at > v_proven_after
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
    -- The session again, under its own lock (the M14-04 security audit): a call that resolved its
    -- session before a concurrent sign-out-everywhere or reset committed must not go on to revoke the
    -- sessions that came after -- a session revoked meanwhile is no key any more.
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

    -- NAMED TENANT-WIDE READ (the driver): the identity's memberships; every write below is pinned
    -- to one of them, tenant and row.
    FOR m IN
        SELECT a.id, a.tenant_id, (a.status = 'active' AND a.unlinked_at IS NULL) AS ordinary
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
            IF m.ordinary THEN
                INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
                VALUES (m.tenant_id, m.id, 'admin.logout', m.id::text,
                        pg_catalog.jsonb_build_object('outcome', 'ok', 'session_id', k.id::text));
            END IF;
            v_n := v_n + 1;
        END LOOP;
    END LOOP;

    RETURN QUERY SELECT v_n;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_revoke_sessions(text, boolean) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_revoke_sessions(text, boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_revoke_sessions(text, boolean) TO tappa_app;

-- identity_password_for_session: 1 session lookup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_password_for_session(p_session_hash text)
 RETURNS TABLE(password_hash text)
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
    v_identity uuid;
BEGIN
    SELECT a.identity_id INTO v_identity
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.authenticated_at > v_proven_after
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    RETURN QUERY SELECT i.password_hash FROM public.admin_identities AS i WHERE i.id = v_identity;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_password_for_session(text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_password_for_session(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_password_for_session(text) TO tappa_app;

-- identity_set_password: 1 session lookup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_set_password(p_session_hash text, p_new_hash text)
 RETURNS TABLE(revoked_other_sessions integer)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
    v_session  uuid;
    v_tenant   uuid;
    v_identity uuid;
    v_n        integer;
    v_own      integer := 0;
    m          record;
BEGIN
    SELECT s.id, s.tenant_id, a.identity_id INTO v_session, v_tenant, v_identity
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.authenticated_at > v_proven_after
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- Lock order: the identity, every membership of it, then token and session rows.
    PERFORM 1 FROM public.admin_identities AS i WHERE i.id = v_identity FOR UPDATE;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    -- Re-checked under the identity lock (READ COMMITTED: the lock-free read above may predate a
    -- reset or a revocation that committed while this call waited).
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

    UPDATE public.admin_identities AS i SET password_hash = p_new_hash WHERE i.id = v_identity;

    FOR m IN
        SELECT a.id, a.tenant_id FROM public.admin_users AS a
         WHERE a.identity_id = v_identity
           AND a.unlinked_at IS NULL
         ORDER BY a.tenant_id
    LOOP
        UPDATE public.admin_users AS a SET password_hash = p_new_hash
         WHERE a.id = m.id AND a.tenant_id = m.tenant_id AND a.identity_id = v_identity;
        UPDATE public.admin_sessions AS s SET revoked_at = now()
         WHERE s.tenant_id = m.tenant_id AND s.admin_user_id = m.id
           AND s.revoked_at IS NULL AND s.id <> v_session;
        GET DIAGNOSTICS v_n = ROW_COUNT;
        IF m.tenant_id = v_tenant THEN
            v_own := v_own + v_n;
        END IF;
        INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
        VALUES (m.tenant_id, m.id, 'admin.password_changed', m.id::text,
                pg_catalog.jsonb_build_object('outcome', 'ok', 'revoked_other_sessions', v_n));
    END LOOP;

    RETURN QUERY SELECT v_own;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_set_password(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_set_password(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_set_password(text, text) TO tappa_app;

-- identity_request_verification: 1 session lookup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_request_verification(p_session_hash text, p_token_hash text, p_purpose text DEFAULT 'verify'::text)
 RETURNS TABLE(verification_id uuid, expires_at timestamp with time zone)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
    v_identity uuid;
    v_id       uuid;
    v_expires  timestamptz;
BEGIN
    IF (p_purpose IN ('verify', 'link')) IS NOT TRUE THEN
        RETURN;
    END IF;
    SELECT a.identity_id INTO v_identity
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.authenticated_at > v_proven_after
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    IF p_purpose = 'verify' THEN
        INSERT INTO public.admin_identity_verifications AS v (identity_id, token_hash, expires_at, purpose)
        SELECT i.id, p_token_hash, now() + interval '24 hours', 'verify'
          FROM public.admin_identities AS i
         WHERE i.id = v_identity AND i.email_verified_at IS NULL
        RETURNING v.id, v.expires_at INTO v_id, v_expires;
    ELSE
        INSERT INTO public.admin_identity_verifications AS v (identity_id, token_hash, expires_at, purpose)
        SELECT i.id, p_token_hash, now() + interval '24 hours', 'link'
          FROM public.admin_identities AS i
         WHERE i.id = v_identity
        RETURNING v.id, v.expires_at INTO v_id, v_expires;
    END IF;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    RETURN QUERY SELECT v_id, v_expires;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_request_verification(text, text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_request_verification(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_request_verification(text, text, text) TO tappa_app;

-- identity_verify_email: 1 session lookup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_verify_email(p_verification_hash text, p_session_hash text)
 RETURNS TABLE(first_verification boolean)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
    v_verification uuid;
    v_identity     uuid;
    v_first        boolean;
    v_session      uuid;
    v_tenant       uuid;
    v_now          timestamptz;
    v_expires      timestamptz;
    v_found        boolean;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    SELECT v.id, v.identity_id INTO v_verification, v_identity
      FROM public.admin_identity_verifications AS v
     WHERE v.token_hash = p_verification_hash
       AND v.purpose = 'verify'
       AND v.used_at IS NULL
       AND v.expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT s.id, s.tenant_id INTO v_session, v_tenant
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.authenticated_at > v_proven_after
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The identity first, then the link and the session, re-checked.
    SELECT i.email_verified_at IS NULL INTO v_first
      FROM public.admin_identities AS i WHERE i.id = v_identity FOR UPDATE;
    SELECT v.expires_at INTO v_expires FROM public.admin_identity_verifications AS v
     WHERE v.id = v_verification AND v.used_at IS NULL
       AND v.purpose = 'verify'
       FOR UPDATE;
    -- The expiry after the link's own lock, with the clock read again (NULL fails closed).
    v_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- Re-checked under the identity lock (READ COMMITTED: the lock-free read above may predate a
    -- reset or a revocation that committed while this call waited).
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
    UPDATE public.admin_identity_verifications AS v SET used_at = now() WHERE v.id = v_verification AND v.used_at IS NULL;
    IF v_first IS TRUE THEN
        UPDATE public.admin_identities AS i
           SET email_verified_at  = now(),
               confirm_opened_at  = now(),
               confirm_open_until = now() + interval '7 days',
               confirm_scope      = 'first_verify'
         WHERE i.id = v_identity;
        INSERT INTO public.admin_identity_events (identity_id, event, detail)
        VALUES (v_identity, 'email_verified', pg_catalog.jsonb_build_object('scope', 'first_verify'));
    END IF;
    RETURN QUERY SELECT v_first;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_verify_email(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_verify_email(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_verify_email(text, text) TO tappa_app;

-- identity_accept_invite: 1 session lookup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_accept_invite(p_invite_hash text, p_session_hash text)
 RETURNS TABLE(admin_user_id uuid, tenant_id uuid)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
    v_invite         uuid;
    v_tenant         uuid;
    v_email          public.citext;
    v_role           text;
    v_name           text;
    v_expires        timestamptz;
    v_found          boolean;
    v_session        uuid;
    v_session_tenant uuid;
    v_identity       uuid;
    v_identity_email public.citext;
    v_digest         text;
    v_verified       timestamptz;
    v_row            uuid;
    v_row_identity   uuid;
    v_row_refused    timestamptz;
    v_row_role       text;
    v_suspended      timestamptz;
    v_tenant_found   boolean;
    v_row_found      boolean;
    v_admin          uuid;
    v_now            timestamptz;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    -- The invitation, read without a lock, only to learn its business.
    SELECT v.id, v.tenant_id, v.email INTO v_invite, v_tenant, v_email
      FROM public.admin_invites AS v
     WHERE v.token_hash = p_invite_hash
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       AND v.expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The session, read without a lock, only to learn its identity (the ONLY way to it).
    SELECT s.id, s.tenant_id, a.identity_id INTO v_session, v_session_tenant, v_identity
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.authenticated_at > v_proven_after
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- Lock order: the identity, its memberships, the business's row of the address, the invitation,
    -- then the session.
    SELECT i.email, i.password_hash, i.email_verified_at INTO v_identity_email, v_digest, v_verified
      FROM public.admin_identities AS i
     WHERE i.id = v_identity
       FOR UPDATE;
    -- The forwarded link (bulgu 12): the session's person must be the invitation's address -- and both
    -- are ASCII (the 2026-10-26 address rule: citext folds 'İ' and the Kelvin sign onto ASCII letters, so
    -- a non-ASCII identity address could equal the invited one under OPERATOR(public.=)). NULL fails
    -- closed.
    IF (v_identity_email::text ~ '^[\x21-\x7e]+$'
        AND v_email::text ~ '^[\x21-\x7e]+$'
        AND v_identity_email OPERATOR(public.=) v_email) IS NOT TRUE THEN
        RETURN;
    END IF;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    SELECT a.id, a.identity_id, a.unlinked_at, a.role INTO v_row, v_row_identity, v_row_refused, v_row_role
      FROM public.admin_users AS a
     WHERE a.tenant_id = v_tenant
       AND a.email OPERATOR(public.=) v_email
       FOR UPDATE;
    v_row_found := FOUND;
    SELECT v.expires_at, v.role, v.full_name INTO v_expires, v_role, v_name
      FROM public.admin_invites AS v
     WHERE v.id = v_invite
       AND v.tenant_id = v_tenant
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       FOR UPDATE;
    v_found := FOUND;
    -- Re-checked under the identity lock (READ COMMITTED: a reset or a revocation may have committed
    -- while this call waited).
    PERFORM 1
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.id = v_session
       AND s.tenant_id = v_session_tenant
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF s;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    -- The expiry after the invitation's own lock, with the clock read again (NULL fails closed).
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- A SUSPENDED business takes no new manager (the user's decision of 2026-10-27): read after the
    -- locks; set -- or no row at all -- refuses, nothing is written, the invitation stays open.
    SELECT t.suspended_at INTO v_suspended
      FROM public.tenants AS t
     WHERE t.id = v_tenant;
    v_tenant_found := FOUND;
    IF (v_tenant_found AND v_suspended IS NULL) IS NOT TRUE THEN
        RETURN;
    END IF;

    IF v_row_found THEN
        -- Only a REFUSED row of the address is taken back (ONAY note 3), and only when it holds the
        -- invitation's role: a manager invitation never brings a refused OWNER row back (the definer
        -- holds no UPDATE (role), and an owner seat is M14-16's two-step transfer). Any other row --
        -- active, disabled-unrefused, linked, or of another role -- refuses with the one answer, and the
        -- invitation stays open.
        IF (v_row_identity IS NULL AND v_row_refused IS NOT NULL AND v_row_role = v_role) IS NOT TRUE THEN
            RETURN;
        END IF;
        UPDATE public.admin_users AS a
           SET identity_id   = v_identity,
               linked_via    = 'invite',
               linked_at     = pg_catalog.now(),
               unlinked_at   = NULL,
               password_hash = v_digest,
               status        = 'active'
         WHERE a.id = v_row
           AND a.tenant_id = v_tenant
           AND a.identity_id IS NULL;
        IF NOT FOUND THEN
            RETURN;
        END IF;
        v_admin := v_row;
    ELSE
        INSERT INTO public.admin_users AS a (tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
        VALUES (v_tenant, v_name, v_email, v_digest, v_role, v_identity, 'invite', pg_catalog.now())
        RETURNING a.id INTO v_admin;
    END IF;

    -- The click with this identity's live session is a mailbox proof (§5.11): the FIRST proof
    -- verifies the address and opens a first_verify window, as identity_verify_email does.
    IF v_verified IS NULL THEN
        UPDATE public.admin_identities AS i
           SET email_verified_at  = pg_catalog.now(),
               confirm_opened_at  = pg_catalog.now(),
               confirm_open_until = pg_catalog.now() + interval '7 days',
               confirm_scope      = 'first_verify'
         WHERE i.id = v_identity
           AND i.email_verified_at IS NULL;
        INSERT INTO public.admin_identity_events (identity_id, event, detail)
        VALUES (v_identity, 'email_verified', pg_catalog.jsonb_build_object('scope', 'first_verify'));
    END IF;

    UPDATE public.admin_invites AS v SET used_at = pg_catalog.now()
     WHERE v.id = v_invite
       AND v.tenant_id = v_tenant
       AND v.used_at IS NULL;
    INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
    VALUES (v_tenant, v_admin, 'admin.invite.accepted', v_admin::text,
            pg_catalog.jsonb_build_object('outcome', 'ok', 'invite_id', v_invite::text));

    RETURN QUERY SELECT v_admin, v_tenant;
EXCEPTION WHEN unique_violation THEN
    -- A membership of the identity in that business, or a row of the address, committed meanwhile.
    RETURN;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_accept_invite(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_accept_invite(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_accept_invite(text, text) TO tappa_app;

-- identity_claim_signup: 1 session lookup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_claim_signup(p_claim_hash text, p_tenant_id uuid, p_session_hash text DEFAULT NULL::text, p_verified_digest text DEFAULT NULL::text)
 RETURNS TABLE(admin_user_id uuid)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
    v_claim       uuid;
    v_email       public.citext;
    v_draft       jsonb;
    v_digest      text;
    v_expires     timestamptz;
    v_used        timestamptz;
    v_now         timestamptz;
    v_name        text;
    v_vat         text;
    v_born        timestamptz;
    v_xmin        xid;
    v_identity    uuid;
    v_id_email    public.citext;
    v_id_digest   text;
    v_verified    timestamptz;
    v_admin       uuid;
BEGIN
    IF p_claim_hash IS NULL OR p_tenant_id IS NULL THEN
        RETURN;
    END IF;
    -- ONE PROOF (M14-17b): a live session or a verified digest, never both -- no caller sends both,
    -- and neither path may decide for the other.
    IF p_session_hash IS NOT NULL AND p_verified_digest IS NOT NULL THEN
        RETURN;
    END IF;
    -- 4. The caller's own tenant context (the GUC db.WithTenant sets, NULLIF as every policy reads it).
    IF (p_tenant_id = NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid) IS NOT TRUE THEN
        RETURN;
    END IF;

    -- The claim, locked: a second confirmation of the same link waits here and then finds it used.
    SELECT c.id, c.email, c.draft, c.password_hash, c.expires_at, c.used_at
      INTO v_claim, v_email, v_draft, v_digest, v_expires, v_used
      FROM public.admin_signup_claims AS c
     WHERE c.token_hash = p_claim_hash
       FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    v_now := pg_catalog.clock_timestamp();
    IF (v_used IS NULL AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- 6b. THE ADDRESS RULE (M14-08a): the address is one internal/mail would send to --
    -- mail.ValidRecipient, the rule Send applies, written out: at most 254 octets, no "=?", and an
    -- addr-spec net/mail parses back to itself in printable ASCII (dot-atom @ dot-atom or an IP
    -- domain literal). Anything else births and links nothing. (00076's pattern, byte for byte --
    -- internal/db's identityascii_test.go holds every identity INSERT body to it.)
    IF (pg_catalog.octet_length(v_email::text) <= 254
        AND pg_catalog.strpos(v_email::text, '=?') = 0
        AND v_email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$') IS NOT TRUE THEN
        RETURN;
    END IF;

    -- 2, 3, 5 -- the business this transaction created, and the draft's.
    SELECT t.name, t.vat_number, t.created_at, t.xmin INTO v_name, v_vat, v_born, v_xmin
      FROM public.tenants AS t
     WHERE t.id = p_tenant_id;
    IF (FOUND
        AND v_born = pg_catalog.now()
        AND v_xmin = pg_catalog.pg_current_xact_id()::xid
        AND v_name = (v_draft ->> 'business_name')
        AND v_vat = (v_draft ->> 'vat_number')) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- 1. THE MAIN GUARANTEE: the business has no membership at all.
    PERFORM 1
       FROM public.admin_users AS a
      WHERE a.tenant_id = p_tenant_id;
    IF FOUND THEN
        RETURN;
    END IF;

    IF p_session_hash IS NOT NULL THEN
        -- AN EXISTING IDENTITY: found only through its own live session (a secret), never by address.
        SELECT a.identity_id INTO v_identity
          FROM public.admin_sessions AS s
          JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
         WHERE s.token_hash = p_session_hash
           AND s.authenticated_at > v_proven_after
           AND s.revoked_at IS NULL
           AND a.status = 'active'
           AND a.unlinked_at IS NULL
           AND a.identity_id IS NOT NULL;
        IF NOT FOUND THEN
            RETURN;
        END IF;
        SELECT i.email, i.password_hash, i.email_verified_at INTO v_id_email, v_id_digest, v_verified
          FROM public.admin_identities AS i
         WHERE i.id = v_identity
           FOR UPDATE;
        -- R18: the session's person must BE the claim's address (citext, as every identity address).
        IF (FOUND AND v_id_email OPERATOR(public.=) v_email) IS NOT TRUE THEN
            RETURN;
        END IF;
        -- The click with that person's own live session is the mailbox proof (§5.10, §13 K-9).
        IF v_verified IS NULL THEN
            UPDATE public.admin_identities AS i
               SET email_verified_at  = pg_catalog.now(),
                   confirm_opened_at  = pg_catalog.now(),
                   confirm_open_until = pg_catalog.now() + interval '7 days',
                   confirm_scope      = 'first_verify'
             WHERE i.id = v_identity;
            INSERT INTO public.admin_identity_events (identity_id, event, detail)
            VALUES (v_identity, 'email_verified', pg_catalog.jsonb_build_object('scope', 'first_verify'));
        END IF;
    ELSIF p_verified_digest IS NOT NULL THEN
        -- AN EXISTING IDENTITY WITH NO ACTIVE MEMBERSHIP (M14-17b, the user's decision of 2026-10-27):
        -- it cannot sign in, so its proof is its PASSWORD -- compared IN THE APPLICATION with the digest
        -- resolve_identity_by_email returned (adminauth.ProveWithIdentityPassword, Go-type-bound like the
        -- new address's proof, S12) -- and p_verified_digest is that very digest. The identity is found
        -- through the CLAIM'S address (the claim secret found the claim), under the address's namespaced
        -- lock (S13; the new address's key, byte for byte), and locked only WHILE its digest is still the
        -- verified one: a reset or a password change that committed while this waited is 0 rows
        -- (identity_open_session's rule, the user's decision of 2026-10-23). The digest is read, never
        -- returned.
        PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-email:' || pg_catalog.lower(v_email::text), 0));
        SELECT i.id, i.email, i.password_hash, i.email_verified_at INTO v_identity, v_id_email, v_id_digest, v_verified
          FROM public.admin_identities AS i
         WHERE i.email OPERATOR(public.=) v_email
           AND i.password_hash = p_verified_digest
           FOR UPDATE;
        IF NOT FOUND THEN
            RETURN;
        END IF;
        -- Under that lock: NO ACTIVE MEMBERSHIP (resolve_identity_by_email's has_active_membership). A
        -- person who can sign in proves themself with a live session, the branch above: the password
        -- alone never skips the session.
        PERFORM 1
           FROM public.admin_users AS a
          WHERE a.identity_id = v_identity
            AND a.status = 'active'
            AND a.unlinked_at IS NULL;
        IF FOUND THEN
            RETURN;
        END IF;
        -- The click is the mailbox proof here too (§5.10, §13 K-9).
        IF v_verified IS NULL THEN
            UPDATE public.admin_identities AS i
               SET email_verified_at  = pg_catalog.now(),
                   confirm_opened_at  = pg_catalog.now(),
                   confirm_open_until = pg_catalog.now() + interval '7 days',
                   confirm_scope      = 'first_verify'
             WHERE i.id = v_identity;
            INSERT INTO public.admin_identity_events (identity_id, event, detail)
            VALUES (v_identity, 'email_verified', pg_catalog.jsonb_build_object('scope', 'first_verify'));
        END IF;
    ELSE
        -- A NEW ADDRESS: the application compared the re-entered password with v_digest (S12) before
        -- calling. Under the address's namespaced lock (S13); a concurrent birth -> no row -> refused.
        PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-email:' || pg_catalog.lower(v_email::text), 0));
        INSERT INTO public.admin_identities AS i (email, password_hash)
        VALUES (v_email, v_digest)
        ON CONFLICT (email) DO NOTHING
        RETURNING i.id INTO v_identity;
        IF v_identity IS NULL THEN
            RETURN;
        END IF;
        UPDATE public.admin_identities AS i
           SET email_verified_at = pg_catalog.now()
         WHERE i.id = v_identity;
        v_id_email := v_email;
        v_id_digest := v_digest;
    END IF;

    -- The owner row: the identity's address and the copy of its digest (dual-write until contract B).
    INSERT INTO public.admin_users (tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
    VALUES (p_tenant_id, v_draft ->> 'full_name', v_id_email, v_id_digest, 'owner', v_identity, 'claim', pg_catalog.now())
    RETURNING id INTO v_admin;

    UPDATE public.admin_signup_claims AS c
       SET used_at = pg_catalog.now()
     WHERE c.id = v_claim
       AND c.used_at IS NULL;

    -- The registration's trail, in the business just created: internal/domain/signup's detail, key for
    -- key, and the same row whichever path ran. No address, no identity id.
    INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
    VALUES (p_tenant_id, v_admin, 'signup.completed', p_tenant_id::text,
            pg_catalog.jsonb_build_object(
                'vat_number', v_vat,
                'vat_check', v_draft ->> 'vat_check',
                'business_type', v_draft ->> 'business_type',
                'structure', v_draft ->> 'structure',
                'venues', pg_catalog.jsonb_array_length(v_draft -> 'venues')));

    RETURN QUERY SELECT v_admin;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_claim_signup(text, uuid, text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_claim_signup(text, uuid, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_claim_signup(text, uuid, text, text) TO tappa_app;

-- identity_confirm_window: 1 session lookup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_confirm_window(p_session_hash text)
 RETURNS TABLE(verified boolean, scope text, open_until timestamp with time zone, tenant_id uuid, tenant_name text, role text, refused boolean, last_active_owner boolean)
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
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
       AND s.authenticated_at > v_proven_after
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

    -- NAMED TENANT-WIDE READ: the identity's own memberships, bounded by it; the scope decides, in
    -- plpgsql, which are on the list (identity_unlink_backfilled's test as section 4 leaves it).
    v_n := 0;
    FOR r IN SELECT a.id, a.tenant_id, a.role, a.status, a.linked_via, a.linked_at, t.name
               FROM public.admin_users AS a
               JOIN public.tenants AS t ON t.id = a.tenant_id
              WHERE a.identity_id = v_identity
                AND a.unlinked_at IS NULL
              ORDER BY a.linked_at, a.tenant_id
    LOOP
        -- The 49 list rows are taken AFTER the scope test, so a person with more memberships than the
        -- list holds loses no in-scope row to rows the scope drops (the read is bounded by the
        -- identity -- the person's own memberships).
        EXIT WHEN v_n >= 49;
        v_listed := ((v_scope = 'first_verify' AND r.linked_via IN ('backfill', 'reconcile', 'signup', 'account'))
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
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_confirm_window(text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_confirm_window(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_confirm_window(text) TO tappa_app;

-- identity_keep_all: 1 session lookup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_keep_all(p_session_hash text)
 RETURNS TABLE(kept boolean)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
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
       AND s.authenticated_at > v_proven_after
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
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_keep_all(text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_keep_all(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_keep_all(text) TO tappa_app;

-- identity_link_by_email: 1 session lookup.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_link_by_email(p_session_hash text, p_verification_hash text)
 RETURNS TABLE(linked integer, first_verification boolean, email text)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
    v_verification   uuid;
    v_identity       uuid;
    v_session        uuid;
    v_session_tenant uuid;
    v_identity_email public.citext;
    v_digest         text;
    v_verified       timestamptz;
    v_expires        timestamptz;
    v_born           timestamptz;
    v_found          boolean;
    v_now            timestamptz;
    v_member         boolean;
    v_linked         integer := 0;
    r                record;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    -- The link, read without a lock, only to learn its identity: a LINK link (purpose), unspent,
    -- unexpired. A verification link of the same table never links anything.
    SELECT v.id, v.identity_id INTO v_verification, v_identity
      FROM public.admin_identity_verifications AS v
     WHERE v.token_hash = p_verification_hash
       AND v.purpose = 'link'
       AND v.used_at IS NULL
       AND v.expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The clicking browser's session, read without a lock: a live session of THE LINK'S identity (R2:
    -- a click from a browser without one links nothing), any of its businesses (S16).
    SELECT s.id, s.tenant_id INTO v_session, v_session_tenant
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.authenticated_at > v_proven_after
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- Lock order (00072's header, sign-in and switch alike): the identity FIRST, then every membership
    -- of it, then the link and the session (re-checked), and only then -- after the link is spent --
    -- the rows to be linked, FOR UPDATE in the loop below.
    SELECT i.email, i.password_hash, i.email_verified_at INTO v_identity_email, v_digest, v_verified
      FROM public.admin_identities AS i
     WHERE i.id = v_identity
       FOR UPDATE;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;

    -- The link again, under its own lock, the expiry with the clock read again (NULL fails closed):
    -- of two presses of one link, the second finds it spent here and links nothing.
    SELECT v.expires_at, v.created_at INTO v_expires, v_born
      FROM public.admin_identity_verifications AS v
     WHERE v.id = v_verification
       AND v.identity_id = v_identity
       AND v.purpose = 'link'
       AND v.used_at IS NULL
       FOR UPDATE;
    v_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- The session again, under the identity lock (READ COMMITTED: a reset or a revocation may have
    -- committed while this call waited).
    PERFORM 1
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.id = v_session
       AND s.tenant_id = v_session_tenant
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF s;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    UPDATE public.admin_identity_verifications AS v SET used_at = pg_catalog.now()
     WHERE v.id = v_verification
       AND v.identity_id = v_identity
       AND v.used_at IS NULL;

    -- NAMED E-MAIL-WIDE READ (the identity's OWN registered address, read above from the row the two
    -- secrets found): every business's active, unrefused, unlinked row of it that existed BEFORE the link
    -- was asked for, locked in (tenant, id) order. The mailbox proof is the link; the person's live
    -- session is the second key. A row born after the link (created_at is the database's, never the
    -- application's -- 00017) is not what the person saw on the banner when they asked: it waits for a
    -- link of its own.
    FOR r IN
        SELECT a.id, a.tenant_id, a.email
          FROM public.admin_users AS a
         WHERE a.email OPERATOR(public.=) v_identity_email
           AND a.identity_id IS NULL
           AND a.unlinked_at IS NULL
           AND a.status = 'active'
           AND a.created_at < v_born
         ORDER BY a.tenant_id, a.id
           FOR UPDATE
    LOOP
        -- 6b. THE ADDRESS RULE (M14-08a, 00076's predicate): only a row whose address is one
        -- internal/mail would send to may be linked. A homoglyph row (U+0130, U+212A) that citext folds
        -- onto the identity's address is never linked. NULL fails closed.
        IF (pg_catalog.octet_length(r.email::text) <= 254
            AND pg_catalog.strpos(r.email::text, '=?') = 0
            AND r.email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$') IS NOT TRUE THEN
            CONTINUE;
        END IF;
        -- One membership per business (UNIQUE (tenant_id, identity_id)): a business where the identity
        -- already has one is left alone.
        PERFORM 1 FROM public.admin_users AS o WHERE o.tenant_id = r.tenant_id AND o.identity_id = v_identity;
        v_member := FOUND;
        IF v_member THEN
            CONTINUE;
        END IF;
        -- The link: the identity, its digest (the dual-write until contract B -- ADR 0031 §5.1), the
        -- way and the time. The row's old password stops signing in with this statement.
        UPDATE public.admin_users AS a
           SET identity_id   = v_identity,
               linked_via    = 'email',
               linked_at     = pg_catalog.now(),
               password_hash = v_digest
         WHERE a.id = r.id
           AND a.tenant_id = r.tenant_id
           AND a.identity_id IS NULL
           AND a.unlinked_at IS NULL;
        IF NOT FOUND THEN
            CONTINUE;
        END IF;
        -- The row's own sessions end: each was opened with the old password.
        UPDATE public.admin_sessions AS s SET revoked_at = pg_catalog.now()
         WHERE s.tenant_id = r.tenant_id
           AND s.admin_user_id = r.id
           AND s.revoked_at IS NULL;
        -- That business's trail: the same row identity_link_legacy writes -- no other business, no
        -- count, no identity, no way of linking.
        INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
        VALUES (r.tenant_id, r.id, 'admin.membership.linked', r.id::text,
                pg_catalog.jsonb_build_object('outcome', 'ok'));
        v_linked := v_linked + 1;
    END LOOP;

    -- The click with this identity's live session is a mailbox proof (§5.11's rule): the FIRST proof
    -- verifies the address and opens a first_verify window, as identity_verify_email does. The rows
    -- linked here are not on that list (linked_via 'email', like 'claim' and 'invite').
    IF v_verified IS NULL THEN
        UPDATE public.admin_identities AS i
           SET email_verified_at  = pg_catalog.now(),
               confirm_opened_at  = pg_catalog.now(),
               confirm_open_until = pg_catalog.now() + interval '7 days',
               confirm_scope      = 'first_verify'
         WHERE i.id = v_identity
           AND i.email_verified_at IS NULL;
        INSERT INTO public.admin_identity_events (identity_id, event, detail)
        VALUES (v_identity, 'email_verified', pg_catalog.jsonb_build_object('scope', 'first_verify'));
    END IF;

    RETURN QUERY SELECT v_linked, (v_verified IS NULL), v_identity_email::text;
EXCEPTION WHEN unique_violation THEN
    -- A membership of the identity in one of those businesses committed meanwhile: nothing of this
    -- call stays, the link is not spent, the answer is the one refusal (no 500).
    RETURN;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_link_by_email(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_link_by_email(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_link_by_email(text, text) TO tappa_app;

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- The bodies 00085 found (00011's resolver; 00071/00072/00077/00079/00080/00081/00084's definers), byte for
-- byte, their properties restated; then the resolver's cell back.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.resolve_admin_session_by_token_hash(p_token_hash text)
 RETURNS TABLE(id uuid, tenant_id uuid, admin_user_id uuid, revoked_at timestamp with time zone)
 LANGUAGE sql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
        -- <=1 row: token_hash is globally UNIQUE. Fixed column list, no SELECT *.
        -- token_hash is the INPUT and is never returned (section 4.7); created_at
        -- and last_used_at are not needed to resolve.
        SELECT s.id, s.tenant_id, s.admin_user_id, s.revoked_at
        FROM public.admin_sessions AS s
        WHERE s.token_hash = p_token_hash;
    $function$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_admin_session_by_token_hash(text) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_admin_session_by_token_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_admin_session_by_token_hash(text) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_memberships(p_session_hash text)
 RETURNS TABLE(tenant_id uuid, tenant_name text, role text, suspended boolean, is_current boolean)
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
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
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_memberships(text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_memberships(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_memberships(text) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_switch_session(p_session_hash text, p_target_tenant uuid, p_new_hash text)
 RETURNS TABLE(session_id uuid, tenant_id uuid, admin_user_id uuid)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
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

    -- 00081 (M14-16b): the target membership under the row lock its UPDATE ... last_login_at takes
    -- below anyway (FOR NO KEY UPDATE: no upgrade; never FOR SHARE -- M14-07's deadlock). A disable
    -- that commits first makes this read find nothing; one that comes second waits for this commit
    -- and its revocation sees the new session.
    SELECT b.id INTO v_target
      FROM public.admin_users AS b
     WHERE b.tenant_id = p_target_tenant
       AND b.identity_id = v_identity
       AND b.status = 'active'
       AND b.unlinked_at IS NULL
       FOR NO KEY UPDATE;
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
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_switch_session(text, uuid, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_switch_session(text, uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_switch_session(text, uuid, text) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_revoke_sessions(p_session_hash text, p_keep_current boolean)
 RETURNS TABLE(revoked integer)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict error
DECLARE
    v_session  uuid;
    v_tenant   uuid;
    v_identity uuid;
    v_n        integer := 0;
    m          record;
    k          record;
BEGIN
    IF p_keep_current IS NULL THEN
        RETURN;
    END IF;

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

    PERFORM 1
       FROM public.admin_identities AS i
      WHERE i.id = v_identity
        FOR UPDATE;
    -- The session again, under its own lock (the M14-04 security audit): a call that resolved its
    -- session before a concurrent sign-out-everywhere or reset committed must not go on to revoke the
    -- sessions that came after -- a session revoked meanwhile is no key any more.
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

    -- NAMED TENANT-WIDE READ (the driver): the identity's memberships; every write below is pinned
    -- to one of them, tenant and row.
    FOR m IN
        SELECT a.id, a.tenant_id, (a.status = 'active' AND a.unlinked_at IS NULL) AS ordinary
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
            IF m.ordinary THEN
                INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
                VALUES (m.tenant_id, m.id, 'admin.logout', m.id::text,
                        pg_catalog.jsonb_build_object('outcome', 'ok', 'session_id', k.id::text));
            END IF;
            v_n := v_n + 1;
        END LOOP;
    END LOOP;

    RETURN QUERY SELECT v_n;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_revoke_sessions(text, boolean) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_revoke_sessions(text, boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_revoke_sessions(text, boolean) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_password_for_session(p_session_hash text)
 RETURNS TABLE(password_hash text)
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_identity uuid;
BEGIN
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
    RETURN QUERY SELECT i.password_hash FROM public.admin_identities AS i WHERE i.id = v_identity;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_password_for_session(text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_password_for_session(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_password_for_session(text) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_set_password(p_session_hash text, p_new_hash text)
 RETURNS TABLE(revoked_other_sessions integer)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_session  uuid;
    v_tenant   uuid;
    v_identity uuid;
    v_n        integer;
    v_own      integer := 0;
    m          record;
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

    -- Lock order: the identity, every membership of it, then token and session rows.
    PERFORM 1 FROM public.admin_identities AS i WHERE i.id = v_identity FOR UPDATE;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    -- Re-checked under the identity lock (READ COMMITTED: the lock-free read above may predate a
    -- reset or a revocation that committed while this call waited).
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

    UPDATE public.admin_identities AS i SET password_hash = p_new_hash WHERE i.id = v_identity;

    FOR m IN
        SELECT a.id, a.tenant_id FROM public.admin_users AS a
         WHERE a.identity_id = v_identity
           AND a.unlinked_at IS NULL
         ORDER BY a.tenant_id
    LOOP
        UPDATE public.admin_users AS a SET password_hash = p_new_hash
         WHERE a.id = m.id AND a.tenant_id = m.tenant_id AND a.identity_id = v_identity;
        UPDATE public.admin_sessions AS s SET revoked_at = now()
         WHERE s.tenant_id = m.tenant_id AND s.admin_user_id = m.id
           AND s.revoked_at IS NULL AND s.id <> v_session;
        GET DIAGNOSTICS v_n = ROW_COUNT;
        IF m.tenant_id = v_tenant THEN
            v_own := v_own + v_n;
        END IF;
        INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
        VALUES (m.tenant_id, m.id, 'admin.password_changed', m.id::text,
                pg_catalog.jsonb_build_object('outcome', 'ok', 'revoked_other_sessions', v_n));
    END LOOP;

    RETURN QUERY SELECT v_own;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_set_password(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_set_password(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_set_password(text, text) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_request_verification(p_session_hash text, p_token_hash text, p_purpose text DEFAULT 'verify'::text)
 RETURNS TABLE(verification_id uuid, expires_at timestamp with time zone)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_identity uuid;
    v_id       uuid;
    v_expires  timestamptz;
BEGIN
    IF (p_purpose IN ('verify', 'link')) IS NOT TRUE THEN
        RETURN;
    END IF;
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
    IF p_purpose = 'verify' THEN
        INSERT INTO public.admin_identity_verifications AS v (identity_id, token_hash, expires_at, purpose)
        SELECT i.id, p_token_hash, now() + interval '24 hours', 'verify'
          FROM public.admin_identities AS i
         WHERE i.id = v_identity AND i.email_verified_at IS NULL
        RETURNING v.id, v.expires_at INTO v_id, v_expires;
    ELSE
        INSERT INTO public.admin_identity_verifications AS v (identity_id, token_hash, expires_at, purpose)
        SELECT i.id, p_token_hash, now() + interval '24 hours', 'link'
          FROM public.admin_identities AS i
         WHERE i.id = v_identity
        RETURNING v.id, v.expires_at INTO v_id, v_expires;
    END IF;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    RETURN QUERY SELECT v_id, v_expires;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_request_verification(text, text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_request_verification(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_request_verification(text, text, text) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_verify_email(p_verification_hash text, p_session_hash text)
 RETURNS TABLE(first_verification boolean)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_verification uuid;
    v_identity     uuid;
    v_first        boolean;
    v_session      uuid;
    v_tenant       uuid;
    v_now          timestamptz;
    v_expires      timestamptz;
    v_found        boolean;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    SELECT v.id, v.identity_id INTO v_verification, v_identity
      FROM public.admin_identity_verifications AS v
     WHERE v.token_hash = p_verification_hash
       AND v.purpose = 'verify'
       AND v.used_at IS NULL
       AND v.expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT s.id, s.tenant_id INTO v_session, v_tenant
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The identity first, then the link and the session, re-checked.
    SELECT i.email_verified_at IS NULL INTO v_first
      FROM public.admin_identities AS i WHERE i.id = v_identity FOR UPDATE;
    SELECT v.expires_at INTO v_expires FROM public.admin_identity_verifications AS v
     WHERE v.id = v_verification AND v.used_at IS NULL
       AND v.purpose = 'verify'
       FOR UPDATE;
    -- The expiry after the link's own lock, with the clock read again (NULL fails closed).
    v_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- Re-checked under the identity lock (READ COMMITTED: the lock-free read above may predate a
    -- reset or a revocation that committed while this call waited).
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
    UPDATE public.admin_identity_verifications AS v SET used_at = now() WHERE v.id = v_verification AND v.used_at IS NULL;
    IF v_first IS TRUE THEN
        UPDATE public.admin_identities AS i
           SET email_verified_at  = now(),
               confirm_opened_at  = now(),
               confirm_open_until = now() + interval '7 days',
               confirm_scope      = 'first_verify'
         WHERE i.id = v_identity;
        INSERT INTO public.admin_identity_events (identity_id, event, detail)
        VALUES (v_identity, 'email_verified', pg_catalog.jsonb_build_object('scope', 'first_verify'));
    END IF;
    RETURN QUERY SELECT v_first;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_verify_email(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_verify_email(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_verify_email(text, text) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_accept_invite(p_invite_hash text, p_session_hash text)
 RETURNS TABLE(admin_user_id uuid, tenant_id uuid)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_invite         uuid;
    v_tenant         uuid;
    v_email          public.citext;
    v_role           text;
    v_name           text;
    v_expires        timestamptz;
    v_found          boolean;
    v_session        uuid;
    v_session_tenant uuid;
    v_identity       uuid;
    v_identity_email public.citext;
    v_digest         text;
    v_verified       timestamptz;
    v_row            uuid;
    v_row_identity   uuid;
    v_row_refused    timestamptz;
    v_row_role       text;
    v_suspended      timestamptz;
    v_tenant_found   boolean;
    v_row_found      boolean;
    v_admin          uuid;
    v_now            timestamptz;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    -- The invitation, read without a lock, only to learn its business.
    SELECT v.id, v.tenant_id, v.email INTO v_invite, v_tenant, v_email
      FROM public.admin_invites AS v
     WHERE v.token_hash = p_invite_hash
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       AND v.expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The session, read without a lock, only to learn its identity (the ONLY way to it).
    SELECT s.id, s.tenant_id, a.identity_id INTO v_session, v_session_tenant, v_identity
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

    -- Lock order: the identity, its memberships, the business's row of the address, the invitation,
    -- then the session.
    SELECT i.email, i.password_hash, i.email_verified_at INTO v_identity_email, v_digest, v_verified
      FROM public.admin_identities AS i
     WHERE i.id = v_identity
       FOR UPDATE;
    -- The forwarded link (bulgu 12): the session's person must be the invitation's address -- and both
    -- are ASCII (the 2026-10-26 address rule: citext folds 'İ' and the Kelvin sign onto ASCII letters, so
    -- a non-ASCII identity address could equal the invited one under OPERATOR(public.=)). NULL fails
    -- closed.
    IF (v_identity_email::text ~ '^[\x21-\x7e]+$'
        AND v_email::text ~ '^[\x21-\x7e]+$'
        AND v_identity_email OPERATOR(public.=) v_email) IS NOT TRUE THEN
        RETURN;
    END IF;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    SELECT a.id, a.identity_id, a.unlinked_at, a.role INTO v_row, v_row_identity, v_row_refused, v_row_role
      FROM public.admin_users AS a
     WHERE a.tenant_id = v_tenant
       AND a.email OPERATOR(public.=) v_email
       FOR UPDATE;
    v_row_found := FOUND;
    SELECT v.expires_at, v.role, v.full_name INTO v_expires, v_role, v_name
      FROM public.admin_invites AS v
     WHERE v.id = v_invite
       AND v.tenant_id = v_tenant
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       FOR UPDATE;
    v_found := FOUND;
    -- Re-checked under the identity lock (READ COMMITTED: a reset or a revocation may have committed
    -- while this call waited).
    PERFORM 1
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.id = v_session
       AND s.tenant_id = v_session_tenant
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF s;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    -- The expiry after the invitation's own lock, with the clock read again (NULL fails closed).
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- A SUSPENDED business takes no new manager (the user's decision of 2026-10-27): read after the
    -- locks; set -- or no row at all -- refuses, nothing is written, the invitation stays open.
    SELECT t.suspended_at INTO v_suspended
      FROM public.tenants AS t
     WHERE t.id = v_tenant;
    v_tenant_found := FOUND;
    IF (v_tenant_found AND v_suspended IS NULL) IS NOT TRUE THEN
        RETURN;
    END IF;

    IF v_row_found THEN
        -- Only a REFUSED row of the address is taken back (ONAY note 3), and only when it holds the
        -- invitation's role: a manager invitation never brings a refused OWNER row back (the definer
        -- holds no UPDATE (role), and an owner seat is M14-16's two-step transfer). Any other row --
        -- active, disabled-unrefused, linked, or of another role -- refuses with the one answer, and the
        -- invitation stays open.
        IF (v_row_identity IS NULL AND v_row_refused IS NOT NULL AND v_row_role = v_role) IS NOT TRUE THEN
            RETURN;
        END IF;
        UPDATE public.admin_users AS a
           SET identity_id   = v_identity,
               linked_via    = 'invite',
               linked_at     = pg_catalog.now(),
               unlinked_at   = NULL,
               password_hash = v_digest,
               status        = 'active'
         WHERE a.id = v_row
           AND a.tenant_id = v_tenant
           AND a.identity_id IS NULL;
        IF NOT FOUND THEN
            RETURN;
        END IF;
        v_admin := v_row;
    ELSE
        INSERT INTO public.admin_users AS a (tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
        VALUES (v_tenant, v_name, v_email, v_digest, v_role, v_identity, 'invite', pg_catalog.now())
        RETURNING a.id INTO v_admin;
    END IF;

    -- The click with this identity's live session is a mailbox proof (§5.11): the FIRST proof
    -- verifies the address and opens a first_verify window, as identity_verify_email does.
    IF v_verified IS NULL THEN
        UPDATE public.admin_identities AS i
           SET email_verified_at  = pg_catalog.now(),
               confirm_opened_at  = pg_catalog.now(),
               confirm_open_until = pg_catalog.now() + interval '7 days',
               confirm_scope      = 'first_verify'
         WHERE i.id = v_identity
           AND i.email_verified_at IS NULL;
        INSERT INTO public.admin_identity_events (identity_id, event, detail)
        VALUES (v_identity, 'email_verified', pg_catalog.jsonb_build_object('scope', 'first_verify'));
    END IF;

    UPDATE public.admin_invites AS v SET used_at = pg_catalog.now()
     WHERE v.id = v_invite
       AND v.tenant_id = v_tenant
       AND v.used_at IS NULL;
    INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
    VALUES (v_tenant, v_admin, 'admin.invite.accepted', v_admin::text,
            pg_catalog.jsonb_build_object('outcome', 'ok', 'invite_id', v_invite::text));

    RETURN QUERY SELECT v_admin, v_tenant;
EXCEPTION WHEN unique_violation THEN
    -- A membership of the identity in that business, or a row of the address, committed meanwhile.
    RETURN;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_accept_invite(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_accept_invite(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_accept_invite(text, text) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_claim_signup(p_claim_hash text, p_tenant_id uuid, p_session_hash text DEFAULT NULL::text, p_verified_digest text DEFAULT NULL::text)
 RETURNS TABLE(admin_user_id uuid)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_claim       uuid;
    v_email       public.citext;
    v_draft       jsonb;
    v_digest      text;
    v_expires     timestamptz;
    v_used        timestamptz;
    v_now         timestamptz;
    v_name        text;
    v_vat         text;
    v_born        timestamptz;
    v_xmin        xid;
    v_identity    uuid;
    v_id_email    public.citext;
    v_id_digest   text;
    v_verified    timestamptz;
    v_admin       uuid;
BEGIN
    IF p_claim_hash IS NULL OR p_tenant_id IS NULL THEN
        RETURN;
    END IF;
    -- ONE PROOF (M14-17b): a live session or a verified digest, never both -- no caller sends both,
    -- and neither path may decide for the other.
    IF p_session_hash IS NOT NULL AND p_verified_digest IS NOT NULL THEN
        RETURN;
    END IF;
    -- 4. The caller's own tenant context (the GUC db.WithTenant sets, NULLIF as every policy reads it).
    IF (p_tenant_id = NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid) IS NOT TRUE THEN
        RETURN;
    END IF;

    -- The claim, locked: a second confirmation of the same link waits here and then finds it used.
    SELECT c.id, c.email, c.draft, c.password_hash, c.expires_at, c.used_at
      INTO v_claim, v_email, v_draft, v_digest, v_expires, v_used
      FROM public.admin_signup_claims AS c
     WHERE c.token_hash = p_claim_hash
       FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    v_now := pg_catalog.clock_timestamp();
    IF (v_used IS NULL AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- 6b. THE ADDRESS RULE (M14-08a): the address is one internal/mail would send to --
    -- mail.ValidRecipient, the rule Send applies, written out: at most 254 octets, no "=?", and an
    -- addr-spec net/mail parses back to itself in printable ASCII (dot-atom @ dot-atom or an IP
    -- domain literal). Anything else births and links nothing. (00076's pattern, byte for byte --
    -- internal/db's identityascii_test.go holds every identity INSERT body to it.)
    IF (pg_catalog.octet_length(v_email::text) <= 254
        AND pg_catalog.strpos(v_email::text, '=?') = 0
        AND v_email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$') IS NOT TRUE THEN
        RETURN;
    END IF;

    -- 2, 3, 5 -- the business this transaction created, and the draft's.
    SELECT t.name, t.vat_number, t.created_at, t.xmin INTO v_name, v_vat, v_born, v_xmin
      FROM public.tenants AS t
     WHERE t.id = p_tenant_id;
    IF (FOUND
        AND v_born = pg_catalog.now()
        AND v_xmin = pg_catalog.pg_current_xact_id()::xid
        AND v_name = (v_draft ->> 'business_name')
        AND v_vat = (v_draft ->> 'vat_number')) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- 1. THE MAIN GUARANTEE: the business has no membership at all.
    PERFORM 1
       FROM public.admin_users AS a
      WHERE a.tenant_id = p_tenant_id;
    IF FOUND THEN
        RETURN;
    END IF;

    IF p_session_hash IS NOT NULL THEN
        -- AN EXISTING IDENTITY: found only through its own live session (a secret), never by address.
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
        SELECT i.email, i.password_hash, i.email_verified_at INTO v_id_email, v_id_digest, v_verified
          FROM public.admin_identities AS i
         WHERE i.id = v_identity
           FOR UPDATE;
        -- R18: the session's person must BE the claim's address (citext, as every identity address).
        IF (FOUND AND v_id_email OPERATOR(public.=) v_email) IS NOT TRUE THEN
            RETURN;
        END IF;
        -- The click with that person's own live session is the mailbox proof (§5.10, §13 K-9).
        IF v_verified IS NULL THEN
            UPDATE public.admin_identities AS i
               SET email_verified_at  = pg_catalog.now(),
                   confirm_opened_at  = pg_catalog.now(),
                   confirm_open_until = pg_catalog.now() + interval '7 days',
                   confirm_scope      = 'first_verify'
             WHERE i.id = v_identity;
            INSERT INTO public.admin_identity_events (identity_id, event, detail)
            VALUES (v_identity, 'email_verified', pg_catalog.jsonb_build_object('scope', 'first_verify'));
        END IF;
    ELSIF p_verified_digest IS NOT NULL THEN
        -- AN EXISTING IDENTITY WITH NO ACTIVE MEMBERSHIP (M14-17b, the user's decision of 2026-10-27):
        -- it cannot sign in, so its proof is its PASSWORD -- compared IN THE APPLICATION with the digest
        -- resolve_identity_by_email returned (adminauth.ProveWithIdentityPassword, Go-type-bound like the
        -- new address's proof, S12) -- and p_verified_digest is that very digest. The identity is found
        -- through the CLAIM'S address (the claim secret found the claim), under the address's namespaced
        -- lock (S13; the new address's key, byte for byte), and locked only WHILE its digest is still the
        -- verified one: a reset or a password change that committed while this waited is 0 rows
        -- (identity_open_session's rule, the user's decision of 2026-10-23). The digest is read, never
        -- returned.
        PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-email:' || pg_catalog.lower(v_email::text), 0));
        SELECT i.id, i.email, i.password_hash, i.email_verified_at INTO v_identity, v_id_email, v_id_digest, v_verified
          FROM public.admin_identities AS i
         WHERE i.email OPERATOR(public.=) v_email
           AND i.password_hash = p_verified_digest
           FOR UPDATE;
        IF NOT FOUND THEN
            RETURN;
        END IF;
        -- Under that lock: NO ACTIVE MEMBERSHIP (resolve_identity_by_email's has_active_membership). A
        -- person who can sign in proves themself with a live session, the branch above: the password
        -- alone never skips the session.
        PERFORM 1
           FROM public.admin_users AS a
          WHERE a.identity_id = v_identity
            AND a.status = 'active'
            AND a.unlinked_at IS NULL;
        IF FOUND THEN
            RETURN;
        END IF;
        -- The click is the mailbox proof here too (§5.10, §13 K-9).
        IF v_verified IS NULL THEN
            UPDATE public.admin_identities AS i
               SET email_verified_at  = pg_catalog.now(),
                   confirm_opened_at  = pg_catalog.now(),
                   confirm_open_until = pg_catalog.now() + interval '7 days',
                   confirm_scope      = 'first_verify'
             WHERE i.id = v_identity;
            INSERT INTO public.admin_identity_events (identity_id, event, detail)
            VALUES (v_identity, 'email_verified', pg_catalog.jsonb_build_object('scope', 'first_verify'));
        END IF;
    ELSE
        -- A NEW ADDRESS: the application compared the re-entered password with v_digest (S12) before
        -- calling. Under the address's namespaced lock (S13); a concurrent birth -> no row -> refused.
        PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-email:' || pg_catalog.lower(v_email::text), 0));
        INSERT INTO public.admin_identities AS i (email, password_hash)
        VALUES (v_email, v_digest)
        ON CONFLICT (email) DO NOTHING
        RETURNING i.id INTO v_identity;
        IF v_identity IS NULL THEN
            RETURN;
        END IF;
        UPDATE public.admin_identities AS i
           SET email_verified_at = pg_catalog.now()
         WHERE i.id = v_identity;
        v_id_email := v_email;
        v_id_digest := v_digest;
    END IF;

    -- The owner row: the identity's address and the copy of its digest (dual-write until contract B).
    INSERT INTO public.admin_users (tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
    VALUES (p_tenant_id, v_draft ->> 'full_name', v_id_email, v_id_digest, 'owner', v_identity, 'claim', pg_catalog.now())
    RETURNING id INTO v_admin;

    UPDATE public.admin_signup_claims AS c
       SET used_at = pg_catalog.now()
     WHERE c.id = v_claim
       AND c.used_at IS NULL;

    -- The registration's trail, in the business just created: internal/domain/signup's detail, key for
    -- key, and the same row whichever path ran. No address, no identity id.
    INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
    VALUES (p_tenant_id, v_admin, 'signup.completed', p_tenant_id::text,
            pg_catalog.jsonb_build_object(
                'vat_number', v_vat,
                'vat_check', v_draft ->> 'vat_check',
                'business_type', v_draft ->> 'business_type',
                'structure', v_draft ->> 'structure',
                'venues', pg_catalog.jsonb_array_length(v_draft -> 'venues')));

    RETURN QUERY SELECT v_admin;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_claim_signup(text, uuid, text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_claim_signup(text, uuid, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_claim_signup(text, uuid, text, text) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_confirm_window(p_session_hash text)
 RETURNS TABLE(verified boolean, scope text, open_until timestamp with time zone, tenant_id uuid, tenant_name text, role text, refused boolean, last_active_owner boolean)
 LANGUAGE plpgsql
 STABLE SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
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

    -- NAMED TENANT-WIDE READ: the identity's own memberships, bounded by it; the scope decides, in
    -- plpgsql, which are on the list (identity_unlink_backfilled's test as section 4 leaves it).
    v_n := 0;
    FOR r IN SELECT a.id, a.tenant_id, a.role, a.status, a.linked_via, a.linked_at, t.name
               FROM public.admin_users AS a
               JOIN public.tenants AS t ON t.id = a.tenant_id
              WHERE a.identity_id = v_identity
                AND a.unlinked_at IS NULL
              ORDER BY a.linked_at, a.tenant_id
    LOOP
        -- The 49 list rows are taken AFTER the scope test, so a person with more memberships than the
        -- list holds loses no in-scope row to rows the scope drops (the read is bounded by the
        -- identity -- the person's own memberships).
        EXIT WHEN v_n >= 49;
        v_listed := ((v_scope = 'first_verify' AND r.linked_via IN ('backfill', 'reconcile', 'signup', 'account'))
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
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_confirm_window(text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_confirm_window(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_confirm_window(text) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_keep_all(p_session_hash text)
 RETURNS TABLE(kept boolean)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
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
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_keep_all(text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_keep_all(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_keep_all(text) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_link_by_email(p_session_hash text, p_verification_hash text)
 RETURNS TABLE(linked integer, first_verification boolean, email text)
 LANGUAGE plpgsql
 SECURITY DEFINER
 SET search_path TO 'pg_catalog', 'pg_temp'
AS $function$
#variable_conflict use_column
DECLARE
    v_verification   uuid;
    v_identity       uuid;
    v_session        uuid;
    v_session_tenant uuid;
    v_identity_email public.citext;
    v_digest         text;
    v_verified       timestamptz;
    v_expires        timestamptz;
    v_born           timestamptz;
    v_found          boolean;
    v_now            timestamptz;
    v_member         boolean;
    v_linked         integer := 0;
    r                record;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    -- The link, read without a lock, only to learn its identity: a LINK link (purpose), unspent,
    -- unexpired. A verification link of the same table never links anything.
    SELECT v.id, v.identity_id INTO v_verification, v_identity
      FROM public.admin_identity_verifications AS v
     WHERE v.token_hash = p_verification_hash
       AND v.purpose = 'link'
       AND v.used_at IS NULL
       AND v.expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The clicking browser's session, read without a lock: a live session of THE LINK'S identity (R2:
    -- a click from a browser without one links nothing), any of its businesses (S16).
    SELECT s.id, s.tenant_id INTO v_session, v_session_tenant
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- Lock order (00072's header, sign-in and switch alike): the identity FIRST, then every membership
    -- of it, then the link and the session (re-checked), and only then -- after the link is spent --
    -- the rows to be linked, FOR UPDATE in the loop below.
    SELECT i.email, i.password_hash, i.email_verified_at INTO v_identity_email, v_digest, v_verified
      FROM public.admin_identities AS i
     WHERE i.id = v_identity
       FOR UPDATE;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;

    -- The link again, under its own lock, the expiry with the clock read again (NULL fails closed):
    -- of two presses of one link, the second finds it spent here and links nothing.
    SELECT v.expires_at, v.created_at INTO v_expires, v_born
      FROM public.admin_identity_verifications AS v
     WHERE v.id = v_verification
       AND v.identity_id = v_identity
       AND v.purpose = 'link'
       AND v.used_at IS NULL
       FOR UPDATE;
    v_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- The session again, under the identity lock (READ COMMITTED: a reset or a revocation may have
    -- committed while this call waited).
    PERFORM 1
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.id = v_session
       AND s.tenant_id = v_session_tenant
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF s;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    UPDATE public.admin_identity_verifications AS v SET used_at = pg_catalog.now()
     WHERE v.id = v_verification
       AND v.identity_id = v_identity
       AND v.used_at IS NULL;

    -- NAMED E-MAIL-WIDE READ (the identity's OWN registered address, read above from the row the two
    -- secrets found): every business's active, unrefused, unlinked row of it that existed BEFORE the link
    -- was asked for, locked in (tenant, id) order. The mailbox proof is the link; the person's live
    -- session is the second key. A row born after the link (created_at is the database's, never the
    -- application's -- 00017) is not what the person saw on the banner when they asked: it waits for a
    -- link of its own.
    FOR r IN
        SELECT a.id, a.tenant_id, a.email
          FROM public.admin_users AS a
         WHERE a.email OPERATOR(public.=) v_identity_email
           AND a.identity_id IS NULL
           AND a.unlinked_at IS NULL
           AND a.status = 'active'
           AND a.created_at < v_born
         ORDER BY a.tenant_id, a.id
           FOR UPDATE
    LOOP
        -- 6b. THE ADDRESS RULE (M14-08a, 00076's predicate): only a row whose address is one
        -- internal/mail would send to may be linked. A homoglyph row (U+0130, U+212A) that citext folds
        -- onto the identity's address is never linked. NULL fails closed.
        IF (pg_catalog.octet_length(r.email::text) <= 254
            AND pg_catalog.strpos(r.email::text, '=?') = 0
            AND r.email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$') IS NOT TRUE THEN
            CONTINUE;
        END IF;
        -- One membership per business (UNIQUE (tenant_id, identity_id)): a business where the identity
        -- already has one is left alone.
        PERFORM 1 FROM public.admin_users AS o WHERE o.tenant_id = r.tenant_id AND o.identity_id = v_identity;
        v_member := FOUND;
        IF v_member THEN
            CONTINUE;
        END IF;
        -- The link: the identity, its digest (the dual-write until contract B -- ADR 0031 §5.1), the
        -- way and the time. The row's old password stops signing in with this statement.
        UPDATE public.admin_users AS a
           SET identity_id   = v_identity,
               linked_via    = 'email',
               linked_at     = pg_catalog.now(),
               password_hash = v_digest
         WHERE a.id = r.id
           AND a.tenant_id = r.tenant_id
           AND a.identity_id IS NULL
           AND a.unlinked_at IS NULL;
        IF NOT FOUND THEN
            CONTINUE;
        END IF;
        -- The row's own sessions end: each was opened with the old password.
        UPDATE public.admin_sessions AS s SET revoked_at = pg_catalog.now()
         WHERE s.tenant_id = r.tenant_id
           AND s.admin_user_id = r.id
           AND s.revoked_at IS NULL;
        -- That business's trail: the same row identity_link_legacy writes -- no other business, no
        -- count, no identity, no way of linking.
        INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
        VALUES (r.tenant_id, r.id, 'admin.membership.linked', r.id::text,
                pg_catalog.jsonb_build_object('outcome', 'ok'));
        v_linked := v_linked + 1;
    END LOOP;

    -- The click with this identity's live session is a mailbox proof (§5.11's rule): the FIRST proof
    -- verifies the address and opens a first_verify window, as identity_verify_email does. The rows
    -- linked here are not on that list (linked_via 'email', like 'claim' and 'invite').
    IF v_verified IS NULL THEN
        UPDATE public.admin_identities AS i
           SET email_verified_at  = pg_catalog.now(),
               confirm_opened_at  = pg_catalog.now(),
               confirm_open_until = pg_catalog.now() + interval '7 days',
               confirm_scope      = 'first_verify'
         WHERE i.id = v_identity
           AND i.email_verified_at IS NULL;
        INSERT INTO public.admin_identity_events (identity_id, event, detail)
        VALUES (v_identity, 'email_verified', pg_catalog.jsonb_build_object('scope', 'first_verify'));
    END IF;

    RETURN QUERY SELECT v_linked, (v_verified IS NULL), v_identity_email::text;
EXCEPTION WHEN unique_violation THEN
    -- A membership of the identity in one of those businesses committed meanwhile: nothing of this
    -- call stays, the link is not spent, the answer is the one refusal (no 500).
    RETURN;
END;
$function$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_link_by_email(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_link_by_email(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_link_by_email(text, text) TO tappa_app;

REVOKE SELECT (authenticated_at) ON admin_sessions FROM tappa_resolver;
