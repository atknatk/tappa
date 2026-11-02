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
-- p_verified_digest, p_purpose) -> TABLE(session_id, tenant_id, admin_user_id). p_purpose is 'sign_in'
-- (Manager.Issue) or 'link_proof' (openLegacyProofSession); anything else is 0 rows. It births a session only for the
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
-- row's, so the session cannot be stamped with another business.
--
-- IT WRITES THE BUSINESS'S AUDIT ROW ITSELF (CLAUDE.md §4.5 (3): a function that writes to a tenant
-- writes that tenant's audit_log), one per birth, by purpose (the orchestrator's decision, 2026-11-02):
--   'sign_in'    admin.login.succeeded, byte for byte identity_open_session's -- actor and target the
--                admin, detail {outcome: ok, session_id}; the handler no longer writes its own for a
--                session born here (internal/handler's completeLogin; no double row). The binary before
--                00086 never calls this function: its handler writes the row after its own INSERT.
--   'link_proof' admin.session.link_proof, detail {session_id} alone -- ADR 0031 §5.6's link writes NO
--                sign-in row in that business ("the proof session is not a sign-in"), yet a session was
--                born there.
-- Neither row carries a token, digest, address, identity or other business. last_login_at stays
-- Issue's MarkAdminLoggedIn.
--
-- WHAT IT DOES NOT CLOSE (ADR 0036, "Kabul edilen kalan risk" -- two threats, told apart). Against a
-- process that runs ARBITRARY SQL as tappa_app the status, tenant and digest conditions are not a
-- barrier: tappa_app holds UPDATE (status) and UPDATE (password_hash) on admin_users (00017), sets
-- app.tenant_id itself (set_config) and reads digests (resolve_admin_by_email,
-- GetAdminPasswordHashByID), so it can reactivate a row, choose any tenant context, read the digest and
-- have this function birth a session. What the database enforces against THAT attacker -- and the first
-- three hold only AFTER the contract: during the expand window tappa_app's raw INSERT on admin_sessions
-- bypasses all three (measured: no audit row on that path) -- a refused
-- membership (unlinked_at, which tappa_app can neither read nor write) gets no session; the session's id
-- and its proof instant are not chosen; every birth leaves an audit_log row tappa_app cannot delete
-- (00006 revokes DELETE) -- and, after the contract, there is no second statement that births one.
-- The status, tenant and digest conditions guard against the BINARY'S OWN MISTAKES (a mismatched pair,
-- a skipped check, a stale proof), which is what the query text used to be trusted with.
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
-- token_hash, authenticated_at) and SELECT (id) for RETURNING, audit_log INSERT (tenant_id, actor_id,
-- action, target, detail)). It reads no identity table.
--
-- No table is altered and no lock is taken on admin_users or admin_sessions: CREATE FUNCTION and a
-- function's ACL take none (00085's measurement).

-- +goose Up

-- +goose StatementBegin
CREATE FUNCTION public.identity_open_row_session(p_admin_user_id uuid, p_tenant_id uuid, p_token_hash text, p_verified_digest text, p_purpose text)
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

    -- Two purposes and no third: every birth leaves its business a row, so there is no silent arm to
    -- choose. Anything else, NULL included, is 0 rows before a lock is taken.
    IF (p_purpose = 'sign_in' OR p_purpose = 'link_proof') IS NOT TRUE THEN
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

    IF p_purpose = 'sign_in' THEN
        -- internal/handler's login-success row, byte for byte (identity_open_session's statement).
        INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
        VALUES (v_tenant, v_admin, 'admin.login.succeeded', v_admin::text,
                pg_catalog.jsonb_build_object('outcome', 'ok', 'session_id', v_session::text));
    ELSE
        -- The legacy side of a link's proof (ADR 0031 §5.6): no sign-in -- the person signs in as the
        -- identity, elsewhere, and this business's trail gets identity_link_legacy's
        -- admin.membership.linked -- but a session was born, so the business is told. The session's id
        -- alone: no other business, no identity, no token, digest or address.
        INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
        VALUES (v_tenant, v_admin, 'admin.session.link_proof', v_admin::text,
                pg_catalog.jsonb_build_object('session_id', v_session::text));
    END IF;

    RETURN QUERY SELECT v_session, v_tenant, v_admin;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_open_row_session(uuid, uuid, text, text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_open_row_session(uuid, uuid, text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_open_row_session(uuid, uuid, text, text, text) TO tappa_app;

-- +goose Down
-- The binary that calls the function must be gone before this runs (a Down is a rollback of the
-- deploy, ADR 0031 §8's "expand Down'ı yalnız eklediğini kaldırır"); the binary before 00086 sends
-- CreateAdminSession's INSERT, which this file never revoked.
DROP FUNCTION public.identity_open_row_session(uuid, uuid, text, text, text);
