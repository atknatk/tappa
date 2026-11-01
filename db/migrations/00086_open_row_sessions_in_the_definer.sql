-- 00086 -- T187, ADR 0036: a panel session on a single admin_users row is born in ONE SECURITY DEFINER
-- function, identity_open_row_session, instead of tappa_app's own INSERT (the user's decision of
-- 2026-11-01, "Evet, taşı": the function joins ADR 0031 §5.2's closed list).
--
-- THE PROBLEM, MEASURED. tappa_app holds INSERT (id, tenant_id, admin_user_id, token_hash) on
-- admin_sessions (00071:574-575) and authenticated_at takes 00069's DEFAULT now(). The password proof
-- of CreateAdminSession (db/queries/admins.sql: `a.password_hash = @verified_digest`, `a.status =
-- 'active'`, FOR NO KEY UPDATE OF a) lives in the QUERY TEXT: any other statement the process sends
-- as tappa_app -- `INSERT INTO admin_sessions (tenant_id, admin_user_id, token_hash) VALUES (...)` --
-- births a fresh 12-hour session (ADR 0035's lifetime does not bound it) for a disabled admin, for a
-- membership the person refused (unlinked_at, which tappa_app cannot even read -- R16), with no digest
-- at all. Measured callers of that INSERT in this binary: two, both internal/adminauth --
-- Manager.Issue (manager.go, the sign-in) and openLegacyProofSession (identity.go, the legacy side of
-- a link's proof). No other production statement inserts into admin_sessions as tappa_app; the
-- definers identity_open_session and identity_switch_session birth sessions as tappa_identitydefiner.
--
-- THE FUNCTION. identity_open_row_session(p_admin_user_id, p_tenant_id, p_token_hash,
-- p_verified_digest) -> TABLE(session_id, tenant_id, admin_user_id). It births a session only for the
-- row (id, tenant) that is, under that row's FOR NO KEY UPDATE lock:
--   * in the transaction's own tenant context (app.tenant_id = p_tenant_id -- what RLS WITH CHECK held
--     tappa_app's INSERT to; a BYPASSRLS body has to say it itself);
--   * status = 'active' (a disabled membership gets nothing -- CreateAdminSession's predicate);
--   * unlinked_at IS NULL (a refused membership gets nothing -- stated, where the legacy statement
--     relied on the refusal's sentinel digest because tappa_app cannot read the column);
--   * password_hash = p_verified_digest (the digest the caller compared the typed password with is
--     still the row's credential: a password change, a reset or a refusal that committed in between
--     makes it 0 rows; under the lock a CONCURRENT one is waited for and READ COMMITTED re-evaluates
--     the predicate on the committed row -- M14-07's measurement, kept).
-- authenticated_at is now() -- written, not defaulted. The session's tenant and admin are the locked
-- row's, so the session cannot be stamped with another business. It writes no audit row and no
-- last_login_at: Issue's MarkAdminLoggedIn and the handler's login row stay where they are (the old
-- binary writes them the same way), and a proof session is no sign-in.
--
-- WHAT IT DOES NOT CLOSE (ADR 0036, "Kabul edilen riskler"): the proof is the CURRENT DIGEST, and
-- tappa_app can read digests (resolve_admin_by_email, GetAdminPasswordHashByID) and can write
-- password_hash (00017) -- a process that runs arbitrary SQL as tappa_app can still read a row's
-- digest and pass it. What moves into the database is every other condition: no session for a
-- disabled or a refused row, none without the current digest, none in another tenant, never a chosen
-- id or proof instant -- and, after the contract, no second statement that births one.
--
-- EXPAND ONLY. This file adds a function and grants EXECUTE; it revokes nothing. The binary before
-- this file sends CreateAdminSession's INSERT, which keeps working: the migrate Job runs before the
-- rollout and the old pod serves until the new one is ready (ADR 0031 §8, replicas 1, maxSurge 1,
-- maxUnavailable 0). The REVOKE of tappa_app's INSERT on admin_sessions is ADR 0036's named CONTRACT
-- step, in a LATER deploy, once no running binary sends the INSERT.
--
-- NO NEW GRANT: tappa_identitydefiner's matrix already holds every cell the body uses (00071:
-- admin_users SELECT (id, tenant_id, status, unlinked_at) and UPDATE (last_login_at) -- the row lock;
-- 00074: admin_users SELECT (password_hash); 00071: admin_sessions INSERT (tenant_id, admin_user_id,
-- token_hash, authenticated_at) and SELECT (id) for RETURNING). It reads no identity table.
--
-- No table is altered and no lock is taken on admin_users or admin_sessions: CREATE FUNCTION and a
-- function's ACL take none (00085's measurement).

-- +goose Up

-- +goose StatementBegin
CREATE FUNCTION public.identity_open_row_session(p_admin_user_id uuid, p_tenant_id uuid, p_token_hash text, p_verified_digest text)
    RETURNS TABLE (session_id uuid, tenant_id uuid, admin_user_id uuid)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict error
DECLARE
    v_admin   uuid;
    v_tenant  uuid;
    v_session uuid;
BEGIN
    IF p_admin_user_id IS NULL OR p_tenant_id IS NULL OR p_token_hash IS NULL OR p_verified_digest IS NULL THEN
        RETURN;
    END IF;

    -- The caller's own business and no other: the transaction's tenant context, the expression every
    -- RLS policy reads (the WITH CHECK that bound tappa_app's INSERT). NULL on either side is 0 rows.
    IF (p_tenant_id = NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid) IS NOT TRUE THEN
        RETURN;
    END IF;

    -- The row, its credential still the one the password verified, under the lock the sign-in's
    -- last_login_at UPDATE takes anyway (FOR NO KEY UPDATE, never FOR SHARE -- M14-07's deadlock).
    SELECT a.id, a.tenant_id INTO v_admin, v_tenant
      FROM public.admin_users AS a
     WHERE a.id = p_admin_user_id
       AND a.tenant_id = p_tenant_id
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.password_hash = p_verified_digest
       FOR NO KEY UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    INSERT INTO public.admin_sessions AS n (tenant_id, admin_user_id, token_hash, authenticated_at)
    VALUES (v_tenant, v_admin, p_token_hash, pg_catalog.now())
    RETURNING n.id INTO v_session;

    RETURN QUERY SELECT v_session, v_tenant, v_admin;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_open_row_session(uuid, uuid, text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_open_row_session(uuid, uuid, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_open_row_session(uuid, uuid, text, text) TO tappa_app;

-- +goose Down
-- The binary that calls the function must be gone before this runs (a Down is a rollback of the
-- deploy, ADR 0031 §8's "expand Down'ı yalnız eklediğini kaldırır"); the binary before 00086 sends
-- CreateAdminSession's INSERT, which this file never revoked.
DROP FUNCTION public.identity_open_row_session(uuid, uuid, text, text);
