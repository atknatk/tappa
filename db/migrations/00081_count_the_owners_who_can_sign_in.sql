-- 00081 -- M14-16b (ADR 0031 §5.2 and §5.12, the notes of 2026-10-27; the user's decisions of
-- 2026-10-27, "bayraktan önce kart" and "M14-16b ile birlikte kapat"). Two of M14-16's leftovers
-- (backlog T171 / T172):
--
--   resolve_admin_owners_by_tenant(uuid)        tappa_resolver   NEW: read-only, the business's active
--                                                                owners that can still get a session
--   identity_open_session(uuid, uuid, text, text)  tappa_identitydefiner  REDEFINED, same signature:
--                                                                its membership reads lock the row
--   identity_switch_session(text, uuid, text)   tappa_identitydefiner  REDEFINED, same signature:
--                                                                its target read locks the row
--
-- and ONE column grant: tappa_resolver SELECT (role) ON admin_users -- the resolver's role filter.
-- Nothing else is granted, nothing is revoked from anyone.
--
-- 1. THE GHOST OWNER (T171). A membership the person refused on the confirmation list keeps
-- role='owner', status='active' (before contract B the unlink leaves it 'active') with unlinked_at
-- stamped -- and tappa_app cannot read unlinked_at (R16). M14-16's last-owner rule therefore counted
-- such a row as an owner and offered the real owner "Step down"; after it, no owner who can sign in
-- was left and there was no way back in the product. resolve_admin_owners_by_tenant gives the panel
-- the one fact it lacks: which of the business's active owner rows can still get a session
-- (unlinked_at IS NULL -- the very filter every session-giving path applies). It returns membership
-- ids and nothing else: no address, no identity_id, no linked_via / linked_at / unlinked_at, no
-- count; a refused row is told apart only by its ABSENCE from the set.
--
-- 🔴 ITS TENANT IS THE CALLER'S OWN CONTEXT, NEVER A CHOICE. p_tenant_id must equal the transaction's
-- app.tenant_id (the very expression every RLS policy reads, NULLIF(...)::uuid -- ADR 0002); anything
-- else, NULL included, is 0 rows. That is identity_create_for_signup's / identity_claim_signup's
-- predicate (00074, 00078): a tappa_app transaction set to business A can ask about A alone -- what an
-- RLS-scoped read of A's admin_users would let it see, narrowed to one derived fact. The parameter is
-- there for the belt: the body's one statement is anchored by it (`a.tenant_id = p_tenant_id`,
-- identitybelt_test.go), as every statement in a BYPASSRLS body must be.
--
-- IT IS NOT AN md.7 RESOLVER IN THE CONTEXT-FREE SENSE: md.7's resolvers PRODUCE the tenant context
-- (they run before it exists); this one runs INSIDE it and reads only it. It shares md.7's other
-- constraints: read-only (STABLE; it cannot write), a fixed result (one column), no SELECT * surface,
-- tappa_app holds no SELECT on the column it filters on, and no "show everything when the context is
-- NULL" branch (NULL context -> 0 rows). Bounded: LIMIT 200 (ListAdminMembers' bound).
--
-- 2. THE RACE WINDOW (T172). identity_open_session and identity_switch_session read the target
-- membership without a lock and insert the session; a disable (internal/adminauth.Memberships) that
-- committed between that read and the function's own UPDATE ... last_login_at revoked the row's
-- sessions without seeing the one being written, so a live session could land on a disabled row
-- (M14-16's security audit measured it; the enable's revocation was what kept it from coming back).
-- Both reads now take FOR NO KEY UPDATE -- the lock the UPDATE ... last_login_at a few lines later
-- takes on the row it lands on, so that row's lock is only taken earlier and never upgraded (FOR
-- SHARE would be: M14-07's 40P01, ADR 0031 §5.6). One lock is new: a read whose re-check drops the
-- row (a disable committed while it waited) keeps that row's lock to the end of the transaction, so a
-- sign-in that falls through to its next branch may hold the disabled membership and the one it lands
-- on together. Both are taken after the identity's lock, in branch order, and no writer waits on the
-- identity while it holds a membership row -- there is no cycle. A disable that commits first makes
-- the read find nothing (READ COMMITTED re-checks status on the newest version); one that comes
-- second waits on the row and its revocation, a later statement, sees the session.
-- The bodies are 00071's but for the lock clauses and a comment beside each; the Down gives 00071's
-- bodies back byte for byte.
--
-- EVERY FUNCTION HERE (ADR 0031 §5.2, T5): SECURITY DEFINER, `SET search_path = pg_catalog, pg_temp`,
-- schema-qualified bodies, no dynamic SQL, a fixed result, the owner, REVOKE ALL FROM PUBLIC and
-- EXECUTE to tappa_app, restated after each CREATE (OR REPLACE).
--
-- EXPAND-SAFE (ADR 0031 §8): a new function no older binary calls, and two redefinitions with their
-- signatures and results unchanged. The flag is not involved.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.identity_open_session(uuid,uuid,text,text)') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_switch_session(text,uuid,text)') IS NULL
       OR NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'tappa_resolver' AND NOT rolsuper AND rolbypassrls) THEN
        RAISE EXCEPTION 'migration 00081 (M14-16b) redefines 00071''s session functions and adds a tappa_resolver function, and 00071 or the role is missing. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regprocedure('public.resolve_admin_owners_by_tenant(uuid)') IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00081 (M14-16b) creates resolve_admin_owners_by_tenant, and it is already there. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- The Down gives role back by revoking it, so the Up must be the one granting it.
    IF pg_catalog.has_column_privilege('tappa_resolver', 'public.admin_users', 'role', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00081 (M14-16b): tappa_resolver already reads admin_users.role; someone granted it by hand. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. resolve_admin_owners_by_tenant (ADR 0031 §5.2's note of 2026-10-27)
-- ---------------------------------------------------------------------------
-- tappa_resolver already reads admin_users (id, tenant_id, status, unlinked_at -- 00011/00017/00071);
-- role is the one column it lacked.
SET LOCAL lock_timeout = '1s';
GRANT SELECT (role) ON admin_users TO tappa_resolver;

-- +goose StatementBegin
CREATE FUNCTION public.resolve_admin_owners_by_tenant(p_tenant_id uuid)
    RETURNS TABLE (admin_user_id uuid)
    LANGUAGE plpgsql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict error
BEGIN
    -- The caller's own business and no other: the transaction's tenant context, the expression every
    -- RLS policy reads. NULL on either side is 0 rows.
    IF (p_tenant_id = NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid) IS NOT TRUE THEN
        RETURN;
    END IF;

    -- The business's active owners that can still get a session: every session-giving path filters
    -- unlinked_at IS NULL (identity_open_session, identity_switch_session, resolve_admin_by_email; the
    -- legacy CreateAdminSession meets the refusal's sentinel digest). Ids only.
    RETURN QUERY
        SELECT a.id
          FROM public.admin_users AS a
         WHERE a.tenant_id = p_tenant_id
           AND a.role = 'owner'
           AND a.status = 'active'
           AND a.unlinked_at IS NULL
         ORDER BY a.id
         LIMIT 200;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_admin_owners_by_tenant(uuid) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_admin_owners_by_tenant(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_admin_owners_by_tenant(uuid) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 2. identity_open_session: its three membership reads lock the row (T172)
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_open_session(p_identity_id uuid, p_tenant uuid, p_token_hash text, p_verified_digest text)
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
    IF p_identity_id IS NULL OR p_token_hash IS NULL OR p_verified_digest IS NULL THEN
        RETURN;
    END IF;

    -- The person's lock (the order every identity_* writer takes it in), the proof still current,
    -- and the landing hint.
    SELECT i.last_used_tenant INTO v_last
      FROM public.admin_identities AS i
     WHERE i.id = p_identity_id
       AND i.password_hash = p_verified_digest
       FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- Each branch sets v_admin and v_tenant from the membership row it found (SELECT INTO leaves
    -- both NULL when it finds none) -- never by assignment: the belt trusts a variable as a tenant
    -- only when a statement that names its tenant wrote it (identitybelt_test.go).
    -- 00081 (M14-16b): each membership read below takes the row's FOR NO KEY UPDATE -- the lock the
    -- UPDATE ... last_login_at further down took anyway, now held from the read on (no upgrade; never
    -- FOR SHARE -- M14-07's deadlock). A disable of the row that commits first makes the read find
    -- nothing; one that comes second waits for this commit and its revocation sees this session.
    IF p_tenant IS NOT NULL THEN
        SELECT a.id, a.tenant_id INTO v_admin, v_tenant
          FROM public.admin_users AS a
         WHERE a.tenant_id = p_tenant
           AND a.identity_id = p_identity_id
           AND a.status = 'active'
           AND a.unlinked_at IS NULL
           FOR NO KEY UPDATE;
    END IF;
    IF v_tenant IS NULL AND v_last IS NOT NULL THEN
        SELECT a.id, a.tenant_id INTO v_admin, v_tenant
          FROM public.admin_users AS a
         WHERE a.tenant_id = v_last
           AND a.identity_id = p_identity_id
           AND a.status = 'active'
           AND a.unlinked_at IS NULL
           FOR NO KEY UPDATE;
    END IF;
    IF v_tenant IS NULL THEN
        -- NAMED TENANT-WIDE READ (the oldest active membership): bounded by the identity and LIMIT 1.
        SELECT a.id, a.tenant_id INTO v_admin, v_tenant
          FROM public.admin_users AS a
         WHERE a.identity_id = p_identity_id
           AND a.status = 'active'
           AND a.unlinked_at IS NULL
         ORDER BY a.created_at, a.tenant_id
         LIMIT 1
           FOR NO KEY UPDATE;
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
ALTER FUNCTION public.identity_open_session(uuid, uuid, text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_open_session(uuid, uuid, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_open_session(uuid, uuid, text, text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 3. identity_switch_session: its target read locks the row (T172)
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_switch_session(p_session_hash text, p_target_tenant uuid, p_new_hash text)
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
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_switch_session(text, uuid, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_switch_session(text, uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_switch_session(text, uuid, text) TO tappa_app;

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- 00071's two bodies, byte for byte, their properties restated (CREATE OR REPLACE keeps the ACL; it is
-- written again anyway -- T5). No table is locked: functions and one column grant alone.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_open_session(p_identity_id uuid, p_tenant uuid, p_token_hash text, p_verified_digest text)
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
    IF p_identity_id IS NULL OR p_token_hash IS NULL OR p_verified_digest IS NULL THEN
        RETURN;
    END IF;

    -- The person's lock (the order every identity_* writer takes it in), the proof still current,
    -- and the landing hint.
    SELECT i.last_used_tenant INTO v_last
      FROM public.admin_identities AS i
     WHERE i.id = p_identity_id
       AND i.password_hash = p_verified_digest
       FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- Each branch sets v_admin and v_tenant from the membership row it found (SELECT INTO leaves
    -- both NULL when it finds none) -- never by assignment: the belt trusts a variable as a tenant
    -- only when a statement that names its tenant wrote it (identitybelt_test.go).
    IF p_tenant IS NOT NULL THEN
        SELECT a.id, a.tenant_id INTO v_admin, v_tenant
          FROM public.admin_users AS a
         WHERE a.tenant_id = p_tenant
           AND a.identity_id = p_identity_id
           AND a.status = 'active'
           AND a.unlinked_at IS NULL;
    END IF;
    IF v_tenant IS NULL AND v_last IS NOT NULL THEN
        SELECT a.id, a.tenant_id INTO v_admin, v_tenant
          FROM public.admin_users AS a
         WHERE a.tenant_id = v_last
           AND a.identity_id = p_identity_id
           AND a.status = 'active'
           AND a.unlinked_at IS NULL;
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
ALTER FUNCTION public.identity_open_session(uuid, uuid, text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_open_session(uuid, uuid, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_open_session(uuid, uuid, text, text) TO tappa_app;

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_switch_session(p_session_hash text, p_target_tenant uuid, p_new_hash text)
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

DROP FUNCTION public.resolve_admin_owners_by_tenant(uuid);
SET LOCAL lock_timeout = '1s';
REVOKE SELECT (role) ON admin_users FROM tappa_resolver;
