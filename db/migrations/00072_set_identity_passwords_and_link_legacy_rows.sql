-- 00072 -- M14-05: the identity's password, its resets, its links to legacy rows, its refusals, its
-- e-mail verification and its event log (expand). Three tenant-scope-less tables
-- (admin_identity_resets, admin_identity_verifications, admin_identity_events), fifteen functions
-- of ADR 0031 §5.2's closed list, and resolve_admin_by_email redefined (DROP + CREATE: its return
-- type gains identity_id and it no longer returns a refused row).
--
-- NORMATIVE SOURCE: docs/adr/0031-tek-kimlik-cok-isletme.md §5.1 (the tables, the trigger's two arms
-- this file's writers pass as the definer), §5.2 (the closed list, the signatures, the definer's
-- verb x column matrix, the three named exceptions -- identity_record_failed_login is this file's),
-- §5.6 (the legacy arm and T2), §5.8 (the confirmation window, its two scopes, the sentinel digest,
-- unlink / relink), §5.9 (resets), §8 (expand: the binary before this file keeps working on it);
-- the card is docs/plan/m14-tek-kimlik.md -> M14-05. M14-04 (00071, a parallel lane) owns the
-- session functions and the signature catalogue; this file implements none of them.
--
-- WHO IS FOUND HOW (ADR 0031 §5.2, bulgu 2). Every function here that writes an identity finds it
-- ONLY through a secret: a live session's token hash (admin_sessions.token_hash ->
-- admin_users.identity_id, NOT NULL), a reset token hash (admin_identity_resets / password_resets)
-- or a verification token hash. No body resolves an identity from an e-mail address; the two e-mail
-- comparisons below (identity_link_legacy, identity_relink_refused) are a defence layer between
-- rows the secrets already found, never a lookup key. Two functions here are keyed by the identity
-- id the login / the reset request resolved (resolve_identity_by_email), not by a secret, and neither
-- writes the identity: the named exception identity_record_failed_login (ONE admin_identity_events
-- row, bounded by the per-identity bucket -- §5.6) and identity_open_reset (ONE reset link and ONE
-- reset_requested event, at most one live link per identity -- §5.9, the user's 2026-10-22 decision).
-- identity_withdraw_reset is keyed by the link's own hash.
--
-- THE BELT (ADR 0031 §5.2, §6; ADR 0021 §3.2's rule for the operator's definers). These bodies run as a BYPASSRLS role,
-- so the tenant isolation RLS gives tappa_app is not there. The rule this file keeps, and that
-- identitypassword_test.go reads out of pg_proc.prosrc: every WRITE on a tenant table (admin_users,
-- admin_sessions, password_resets, audit_log) names its tenant with a variable -- `tenant_id =
-- v_tenant`, `= p_tenant`, `= m.tenant_id` (a membership the identity owns), or audit_log's
-- tenant_id column set from one; every READ on a tenant table either carries a tenant_id predicate
-- or is one of the two lookups that PRODUCE the tenant and are marked so: `/* secret-scope */` (a
-- token hash -> its row, the md.7 shape) and `/* identity-scope */` (an identity -> its
-- memberships; reaching every business of ONE person is what ADR 0031 §6 makes the third
-- exception for). Neither marker may sit on a write.
--
-- FRESHNESS (R3, S4, M14-03's audit). "Fresh" is authenticated_at within the last five minutes AND
-- not in the future: `authenticated_at > statement_timestamp() - interval '5 minutes' AND
-- authenticated_at <= statement_timestamp()`. A NULL (a session older than 00069) is never fresh; a future-dated value
-- (or 'infinity') is never fresh either, whatever wrote it.
--
-- LOCK ORDER AND THE RE-CHECK (M14-05's security audit, measured on 17.10). One order everywhere:
-- the identity's row lock FIRST; then membership rows (every row linked to the identity, and the one
-- other row a link or a relink names); only then token rows (password_resets, admin_identity_resets,
-- verifications) and session rows. A token is READ before the identity lock only to learn which
-- identity to lock, and locked and re-checked after it -- the old token-first order deadlocked a
-- consumption against an unlink (40P01, third-eye's measurement). A session-keyed writer reads its
-- session lock-free, takes the identity lock, then re-reads the session FOR UPDATE OF s with the
-- same predicates (the bounded freshness included) and answers 0 rows if it no longer holds: a reset
-- that committed while the call waited (it revokes every session) is not undone by the session it
-- revoked. A token-keyed writer re-checks its link the same way. Liveness and freshness compare with
-- statement_timestamp(): after a lock wait now() is the transaction's start (passwordresets.sql:129).
--
-- THE CONFIRMATION WINDOW (§5.8). Two scopes, opened `now() .. now() + 7 days`:
--   first_verify -- the identity's FIRST verification (identity_verify_email here; invite and claim
--                   clicks in M14-15/17): lists links with linked_via IN (backfill, reconcile, signup);
--   reset        -- EVERY reset consumption (identity_consume_row_reset, identity_consume_reset): lists
--                   every link made before the window opened (linked_at < confirm_opened_at),
--                   claim and invite included (S3, T1).
-- Unlinking (identity_unlink_backfilled) stamps unlinked_at, writes the SENTINEL digest -- the
-- bcrypt (cost 12, adminauth.Cost) of 32 random bytes that are discarded in the same expression; no
-- constant in the repository (S20) -- cancels the row's unexpired password_resets links (T5) and its
-- sessions. The row keeps linked_via and linked_at, so identity_relink_refused can restore the link
-- with the ORIGINAL linked_at inside the same window (T4).
--
-- CONTRACT B AND THE UNLINK (T1, ONAY notu 2). A row born after contract B cannot become unlinked
-- (its CHECK requires identity_id); for it "unlink" keeps the link, stamps unlinked_at and sets
-- status = 'disabled'. Before contract B no such CHECK exists, so that branch is taken only when
-- the unlinking UPDATE meets a CHECK on admin_users that is NOT one of the five this file knows --
-- i.e. contract B's (whose body replaces this one anyway, ADR 0031 §8 1b). The test proves the
-- branch with a stand-in CHECK.
--
-- AUDIT (ADR 0031 §5.2). A function that writes a tenant's rows writes that tenant's audit_log, one
-- row per tenant, ordinary actions (admin.password_changed, admin.recovery.completed) or the three
-- membership actions (admin.membership.linked / unlinked / relinked). No row names another tenant,
-- the identity, linked_via or a membership count; the actor is the membership row of THAT tenant
-- (§5.13). Identity-level facts go to admin_identity_events instead (no e-mail, no IP; 90 days).
--
-- THE THREE TABLES ARE TENANT-SCOPE-LESS (CLAUDE.md §6, ADR 0031 §6): R5 waivers below (visible as
-- WARNs), ENABLE + FORCE, no policy, explicit REVOKE ALL from tappa_app (the dev and production
-- default ACL hands a new table `tappa_app=arwd`), uuid keys (no sequence), no column called
-- tenant_id. The definer and the resolver reach them through the column grants of section 4 alone.
-- redline: no-tenant-scope(admin_identity_resets) — kimlik basina sifirlama linki; hicbir tenant'a ait degil, yazari yalniz identity_* definer'lari, ADR 0031 §5.1/§6
-- redline: no-tenant-scope(admin_identity_verifications) — kimligin e-posta dogrulama linki; tenant'a ait degil, yazari yalniz identity_* definer'lari, ADR 0031 §5.1/§6
-- redline: no-tenant-scope(admin_identity_events) — kimlik duzeyi olaylar (e-posta/IP yok, 90 gun); tenant'a ait degil, yazari yalniz identity_* definer'lari, ADR 0031 §5.1/§6
--
-- resolve_admin_by_email (T2, T5, T6). DROP + CREATE because the return type changes; the new body
-- restates SECURITY DEFINER, the pinned search_path, owner tappa_resolver, REVOKE ALL FROM PUBLIC
-- and GRANT EXECUTE TO tappa_app (a DROP takes the grants with it). The binary before this file
-- selects `id, tenant_id, password_hash, status` by name (internal/db/resolve.go getAdminByEmailSQL)
-- and keeps working. tappa_resolver gains SELECT (identity_id, unlinked_at) on admin_users.
--
-- pgcrypto (crypt, gen_salt, gen_random_bytes) comes from scripts/db-init/01-roles.sql, not from a
-- migration; section 0 refuses a database without it rather than failing on the first unlink.
--
-- THE DOWN REFUSES while one of the three tables holds a row or a membership row is refused
-- (unlinked_at): the reset links, the verification links and the event log would go, and the old
-- resolve_admin_by_email would offer a refused row to the legacy login again. The way back from the
-- expand is the previous binary, which runs on this schema (ADR 0031 §8).

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.admin_identities') IS NULL
       OR pg_catalog.to_regprocedure('public.admin_users_guard_identity_credentials()') IS NULL
       OR NOT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute
                       WHERE attrelid = 'public.admin_sessions'::pg_catalog.regclass
                         AND attname = 'authenticated_at' AND NOT attisdropped) THEN
        RAISE EXCEPTION 'migration 00072 (M14-05) needs 00069 (admin_identities, the link columns, authenticated_at). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF pg_catalog.to_regclass('public.admin_identity_resets') IS NOT NULL
       OR pg_catalog.to_regclass('public.admin_identity_verifications') IS NOT NULL
       OR pg_catalog.to_regclass('public.admin_identity_events') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc
                   WHERE pronamespace = 'public'::pg_catalog.regnamespace
                     AND proname IN ('identity_password_for_session', 'identity_set_password',
                                     'identity_consume_row_reset', 'resolve_identity_reset_by_token_hash',
                                     'identity_consume_reset', 'identity_link_legacy',
                                     'identity_unlink_backfilled', 'identity_relink_refused',
                                     'identity_request_verification', 'resolve_identity_verification_by_token_hash',
                                     'identity_verify_email', 'identity_record_failed_login',
                                     'identity_sweep_events', 'identity_open_reset', 'identity_withdraw_reset')) THEN
        RAISE EXCEPTION 'migration 00072 (M14-05) creates the identity reset, verification and event tables and their functions, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- The redefinition replaces exactly 00017's resolve_admin_by_email.
    IF pg_catalog.to_regprocedure('public.resolve_admin_by_email(citext)') IS NULL
       OR (SELECT pg_catalog.pg_get_function_result(p.oid) FROM pg_catalog.pg_proc p
            WHERE p.oid = 'public.resolve_admin_by_email(citext)'::pg_catalog.regprocedure)
          <> 'TABLE(id uuid, tenant_id uuid, password_hash text, status text)' THEN
        RAISE EXCEPTION 'migration 00072 (M14-05) redefines resolve_admin_by_email as 00017 left it, and it is not in that state. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- The sentinel digest is computed in SQL (S20): pgcrypto from scripts/db-init/01-roles.sql.
    IF pg_catalog.to_regprocedure('public.crypt(text,text)') IS NULL
       OR pg_catalog.to_regprocedure('public.gen_salt(text,integer)') IS NULL
       OR pg_catalog.to_regprocedure('public.gen_random_bytes(integer)') IS NULL
       OR NOT pg_catalog.has_function_privilege('tappa_identitydefiner', 'public.crypt(text,text)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_identitydefiner', 'public.gen_salt(text,integer)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_identitydefiner', 'public.gen_random_bytes(integer)', 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00072 (M14-05) computes the unlink sentinel with pgcrypto (crypt, gen_salt, gen_random_bytes in schema public, executable by tappa_identitydefiner) and they are not there. scripts/db-init/01-roles.sql creates the extension. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. admin_identity_resets (ADR 0031 §5.1): at most one live link per identity
-- ---------------------------------------------------------------------------
-- The shape of password_resets after 00019: token_hash is the 64-hex SHA-256 of the token (the token
-- itself is never stored), the TTL ceiling is 24 hours, used_at and cancelled_at are never confused.
-- identity_open_reset issues them (one live link per identity -- EM-7C), identity_withdraw_reset
-- retires an undelivered one, identity_consume_reset spends them.
CREATE TABLE admin_identity_resets (
    id           uuid        NOT NULL DEFAULT gen_random_uuid(),
    identity_id  uuid        NOT NULL,
    token_hash   text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    used_at      timestamptz,
    cancelled_at timestamptz,
    CONSTRAINT admin_identity_resets_pkey PRIMARY KEY (id),
    CONSTRAINT admin_identity_resets_token_hash_key UNIQUE (token_hash),
    CONSTRAINT admin_identity_resets_token_hash_shape CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT admin_identity_resets_ttl_ceiling CHECK (expires_at <= created_at + interval '24 hours'),
    CONSTRAINT admin_identity_resets_identity_fk FOREIGN KEY (identity_id)
        REFERENCES admin_identities (id) ON DELETE RESTRICT
);
CREATE INDEX admin_identity_resets_identity_idx ON admin_identity_resets (identity_id);
ALTER TABLE admin_identity_resets ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_identity_resets FORCE ROW LEVEL SECURITY;
REVOKE ALL ON admin_identity_resets FROM tappa_app;

-- ---------------------------------------------------------------------------
-- 2. admin_identity_verifications (ADR 0031 §5.1, M14-18): verify an existing identity's address
-- ---------------------------------------------------------------------------
-- Bound to the IDENTITY that asked (identity_request_verification), consumed only with a live session
-- of that identity in the clicking browser (identity_verify_email). E-mail CHANGE is out of M14
-- (K-15): no column for a new address.
CREATE TABLE admin_identity_verifications (
    id          uuid        NOT NULL DEFAULT gen_random_uuid(),
    identity_id uuid        NOT NULL,
    token_hash  text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    CONSTRAINT admin_identity_verifications_pkey PRIMARY KEY (id),
    CONSTRAINT admin_identity_verifications_token_hash_key UNIQUE (token_hash),
    CONSTRAINT admin_identity_verifications_token_hash_shape CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT admin_identity_verifications_ttl_ceiling CHECK (expires_at <= created_at + interval '24 hours'),
    CONSTRAINT admin_identity_verifications_identity_fk FOREIGN KEY (identity_id)
        REFERENCES admin_identities (id) ON DELETE RESTRICT
);
CREATE INDEX admin_identity_verifications_identity_idx ON admin_identity_verifications (identity_id);
ALTER TABLE admin_identity_verifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_identity_verifications FORCE ROW LEVEL SECURITY;
REVOKE ALL ON admin_identity_verifications FROM tappa_app;

-- ---------------------------------------------------------------------------
-- 3. admin_identity_events (ADR 0031 §5.1, R9, R17)
-- ---------------------------------------------------------------------------
-- Identity-level facts. identity_id NOT NULL: an unknown address writes nothing (an anonymous
-- append-only table would grow without bound -- adminratelimit.go's M5-02 lesson). event is a
-- closed set (the ADR's "failed login, reset request, verification, link refusal, unlink");
-- reset_requested is identity_open_reset's. detail has closed keys -- `tenant` (a uuid) and
-- `scope` (a window scope) -- so an e-mail address or an IP cannot be written into it. Retention:
-- 90 days, deleted only by identity_sweep_events (00058's pattern).
CREATE TABLE admin_identity_events (
    id          uuid        NOT NULL DEFAULT gen_random_uuid(),
    identity_id uuid        NOT NULL,
    event       text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    detail      jsonb       NOT NULL DEFAULT '{}'::jsonb,
    CONSTRAINT admin_identity_events_pkey PRIMARY KEY (id),
    CONSTRAINT admin_identity_events_event_known CHECK (event IN (
        'login_failed', 'login_limited', 'reset_requested', 'email_verified',
        'link_refused', 'membership_unlinked', 'membership_relinked')),
    CONSTRAINT admin_identity_events_detail_closed CHECK (
        jsonb_typeof(detail) = 'object'
        AND (detail - ARRAY['tenant', 'scope']) = '{}'::jsonb
        AND (NOT detail ? 'tenant'
             OR (jsonb_typeof(detail -> 'tenant') = 'string'
                 AND (detail ->> 'tenant') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'))
        AND (NOT detail ? 'scope'
             OR (jsonb_typeof(detail -> 'scope') = 'string' AND (detail ->> 'scope') IN ('first_verify', 'reset')))),
    CONSTRAINT admin_identity_events_identity_fk FOREIGN KEY (identity_id)
        REFERENCES admin_identities (id) ON DELETE RESTRICT
);
CREATE INDEX admin_identity_events_identity_idx ON admin_identity_events (identity_id, created_at);
CREATE INDEX admin_identity_events_created_idx ON admin_identity_events (created_at);
ALTER TABLE admin_identity_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_identity_events FORCE ROW LEVEL SECURITY;
REVOKE ALL ON admin_identity_events FROM tappa_app;

-- ---------------------------------------------------------------------------
-- 4. Column grants (ADR 0031 §5.2's matrix -- only what THIS file's functions need; M14-04 adds
--    its own; identitypassword_test.go pins this file's set)
-- ---------------------------------------------------------------------------
-- Some of these columns are granted by 00071 (M14-04) too; GRANT is idempotent, and the Down below
-- revokes only what this file alone adds (its comment lists the shared ones).
-- tappa_identitydefiner. admin_identities: what the bodies read and write -- never email (K-15), never
-- last_used_tenant / last_login_at (M14-04's). admin_users: no INSERT here, UPDATE without email.
-- status is the post-contract unlink's disable form (T1). FOR UPDATE needs an UPDATE grant on the
-- locked table, which each locked table has.
GRANT SELECT (id, email, password_hash, email_verified_at, confirm_opened_at, confirm_open_until, confirm_scope),
      UPDATE (password_hash, email_verified_at, confirm_opened_at, confirm_open_until, confirm_scope)
    ON admin_identities TO tappa_identitydefiner;
GRANT SELECT (id, identity_id, token_hash, expires_at, used_at, cancelled_at),
      INSERT (identity_id, token_hash, expires_at),
      UPDATE (used_at, cancelled_at)
    ON admin_identity_resets TO tappa_identitydefiner;
GRANT SELECT (id, identity_id, token_hash, expires_at, used_at),
      INSERT (identity_id, token_hash, expires_at),
      UPDATE (used_at)
    ON admin_identity_verifications TO tappa_identitydefiner;
GRANT SELECT (created_at), INSERT (identity_id, event, detail), DELETE
    ON admin_identity_events TO tappa_identitydefiner;

SET LOCAL lock_timeout = '1s';
GRANT SELECT (id, tenant_id, email, status, identity_id, linked_via, linked_at, unlinked_at),
      UPDATE (password_hash, identity_id, linked_via, linked_at, unlinked_at, status)
    ON admin_users TO tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
GRANT SELECT (id, tenant_id, admin_user_id, token_hash, revoked_at, authenticated_at),
      UPDATE (revoked_at)
    ON admin_sessions TO tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
GRANT SELECT (id, tenant_id, admin_user_id, token_hash, expires_at, used_at, cancelled_at),
      UPDATE (used_at, cancelled_at)
    ON password_resets TO tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
GRANT INSERT (tenant_id, actor_id, action, target, detail) ON audit_log TO tappa_identitydefiner;

-- tappa_resolver (T6): resolve_admin_by_email's filter and its new column; the two token resolvers'
-- columns.
SET LOCAL lock_timeout = '1s';
GRANT SELECT (identity_id, unlinked_at) ON admin_users TO tappa_resolver;
GRANT SELECT (id, identity_id, token_hash, expires_at, used_at, cancelled_at) ON admin_identity_resets TO tappa_resolver;
GRANT SELECT (id, identity_id, token_hash, expires_at, used_at) ON admin_identity_verifications TO tappa_resolver;

-- ---------------------------------------------------------------------------
-- 5. resolve_admin_by_email, redefined (T2, T5, S8)
-- ---------------------------------------------------------------------------
-- md.7 (iii) widening, ADR 0031 §5.2: identity_id joins the pre-authentication surface so the
-- identity path's legacy arm can drop the identity's OWN rows from its eight candidates (T2). It is
-- a correlation key, not a secret; it reaches no response, log or audit row. unlinked_at IS NULL: a
-- refused row is no candidate on any login, the flag rolled back included (S8). The order is 00017's,
-- incumbent first.
DROP FUNCTION resolve_admin_by_email(citext);

-- +goose StatementBegin
CREATE FUNCTION public.resolve_admin_by_email(p_email citext)
    RETURNS TABLE (
        id            uuid,
        tenant_id     uuid,
        password_hash text,
        status        text,
        identity_id   uuid
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    SELECT a.id, a.tenant_id, a.password_hash, a.status, a.identity_id
      FROM public.admin_users AS a
     WHERE a.email OPERATOR(public.=) p_email
       AND a.unlinked_at IS NULL
     ORDER BY a.created_at, a.tenant_id;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_admin_by_email(citext) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_admin_by_email(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_admin_by_email(citext) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 6. The two token resolvers (ADR 0002 md.7: read-only, <= 1 row from a UNIQUE, owner tappa_resolver)
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE FUNCTION public.resolve_identity_reset_by_token_hash(p_token_hash text)
    RETURNS TABLE (
        id           uuid,
        identity_id  uuid,
        expires_at   timestamptz,
        used_at      timestamptz,
        cancelled_at timestamptz
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    SELECT r.id, r.identity_id, r.expires_at, r.used_at, r.cancelled_at
      FROM public.admin_identity_resets AS r
     WHERE r.token_hash = p_token_hash;
$$;

CREATE FUNCTION public.resolve_identity_verification_by_token_hash(p_token_hash text)
    RETURNS TABLE (
        id          uuid,
        identity_id uuid,
        expires_at  timestamptz,
        used_at     timestamptz
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    SELECT v.id, v.identity_id, v.expires_at, v.used_at
      FROM public.admin_identity_verifications AS v
     WHERE v.token_hash = p_token_hash;
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 7. The identity_* writers (owner tappa_identitydefiner)
-- ---------------------------------------------------------------------------

-- identity_password_for_session: the identity's digest for the account page's "current password"
-- check. A session whose row is not linked -> 0 rows (the application uses today's one-row path).
-- +goose StatementBegin
CREATE FUNCTION public.identity_password_for_session(p_session_hash text)
    RETURNS TABLE (password_hash text)
    LANGUAGE plpgsql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_identity uuid;
BEGIN
    SELECT a.identity_id INTO v_identity
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash /* secret-scope */
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    RETURN QUERY SELECT i.password_hash FROM public.admin_identities i WHERE i.id = v_identity;
END;
$$;
-- +goose StatementEnd

-- identity_set_password: the identity and EVERY row linked to it take the new digest; every other
-- session of every linked row is revoked, the calling session kept. One audit row per tenant written
-- (admin.password_changed, the actor that tenant's membership). Unlinked session -> 0 rows: the
-- application writes that row alone, as today.
-- +goose StatementBegin
CREATE FUNCTION public.identity_set_password(p_session_hash text, p_new_hash text)
    RETURNS TABLE (revoked_other_sessions integer)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
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
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash /* secret-scope */
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    PERFORM 1 FROM public.admin_identities i WHERE i.id = v_identity FOR UPDATE;
    -- Lock order (identity -> membership rows -> token / session rows): every membership of the
    -- identity, before any token or session row.
    PERFORM 1 FROM public.admin_users a WHERE a.identity_id = v_identity /* identity-scope */ FOR UPDATE;
    -- Re-checked under the identity lock (READ COMMITTED: the lock-free read above may predate a
    -- reset or a revocation that committed while this call waited).
    PERFORM 1
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.id = v_session AND s.tenant_id = v_tenant
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF s;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    UPDATE public.admin_identities i SET password_hash = p_new_hash WHERE i.id = v_identity;

    FOR m IN
        SELECT a.id, a.tenant_id FROM public.admin_users a
         WHERE a.identity_id = v_identity /* identity-scope */
           AND a.unlinked_at IS NULL
         ORDER BY a.tenant_id
    LOOP
        UPDATE public.admin_users a SET password_hash = p_new_hash
         WHERE a.id = m.id AND a.tenant_id = m.tenant_id AND a.identity_id = v_identity;
        UPDATE public.admin_sessions s SET revoked_at = now()
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
$$;
-- +goose StatementEnd

-- identity_consume_row_reset: an OLD per-row password_resets link (M14-08 on). If its row is linked:
-- the link is spent, the identity and every linked row take the new digest, every session of every
-- linked row is revoked (no session is issued), every other live link of those rows and every live
-- identity link is cancelled, email_verified_at is filled (the click is the mailbox's -- S5) and a
-- RESET-scope window opens. Unlinked rows of the same address are not linked. Not linked (or not
-- live) -> 0 rows: the application's legacy consumption decides.
-- +goose StatementBegin
CREATE FUNCTION public.identity_consume_row_reset(p_reset_hash text, p_new_hash text)
    RETURNS TABLE (admin_user_id uuid, tenant_id uuid, reset_id uuid, revoked_sessions integer)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_reset    uuid;
    v_tenant   uuid;
    v_admin    uuid;
    v_identity uuid;
    v_n        integer;
    v_own      integer := 0;
    m          record;
BEGIN
    SELECT r.id, r.tenant_id, r.admin_user_id, a.identity_id INTO v_reset, v_tenant, v_admin, v_identity
      FROM public.password_resets r
      JOIN public.admin_users a ON a.id = r.admin_user_id AND a.tenant_id = r.tenant_id
     WHERE r.token_hash = p_reset_hash /* secret-scope */
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL
       AND statement_timestamp() < r.expires_at
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The identity first (the order every identity writer keeps), its memberships, then the link,
    -- re-checked.
    PERFORM 1 FROM public.admin_identities i WHERE i.id = v_identity FOR UPDATE;
    -- Lock order (identity -> membership rows -> token / session rows): every membership of the
    -- identity, before any token or session row.
    PERFORM 1 FROM public.admin_users a WHERE a.identity_id = v_identity /* identity-scope */ FOR UPDATE;
    PERFORM 1
      FROM public.password_resets r
      JOIN public.admin_users a ON a.id = r.admin_user_id AND a.tenant_id = r.tenant_id
     WHERE r.id = v_reset AND r.tenant_id = v_tenant
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL
       AND statement_timestamp() < r.expires_at
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF r;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    UPDATE public.password_resets r SET used_at = now()
     WHERE r.id = v_reset AND r.tenant_id = v_tenant AND r.used_at IS NULL;

    UPDATE public.admin_identities i
       SET password_hash      = p_new_hash,
           email_verified_at  = coalesce(i.email_verified_at, now()),
           confirm_opened_at  = now(),
           confirm_open_until = now() + interval '7 days',
           confirm_scope      = 'reset'
     WHERE i.id = v_identity;
    UPDATE public.admin_identity_resets r SET cancelled_at = now()
     WHERE r.identity_id = v_identity AND r.used_at IS NULL AND r.cancelled_at IS NULL AND statement_timestamp() < r.expires_at;

    FOR m IN
        SELECT a.id, a.tenant_id FROM public.admin_users a
         WHERE a.identity_id = v_identity /* identity-scope */
           AND a.unlinked_at IS NULL
         ORDER BY a.tenant_id
    LOOP
        UPDATE public.admin_users a SET password_hash = p_new_hash
         WHERE a.id = m.id AND a.tenant_id = m.tenant_id AND a.identity_id = v_identity;
        UPDATE public.password_resets r SET cancelled_at = now()
         WHERE r.tenant_id = m.tenant_id AND r.admin_user_id = m.id
           AND r.used_at IS NULL AND r.cancelled_at IS NULL AND statement_timestamp() < r.expires_at;
        UPDATE public.admin_sessions s SET revoked_at = now()
         WHERE s.tenant_id = m.tenant_id AND s.admin_user_id = m.id AND s.revoked_at IS NULL;
        GET DIAGNOSTICS v_n = ROW_COUNT;
        IF m.tenant_id = v_tenant THEN
            v_own := v_own + v_n;
        END IF;
        INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
        VALUES (m.tenant_id, m.id, 'admin.recovery.completed', m.id::text,
                pg_catalog.jsonb_build_object('outcome', 'ok', 'revoked_sessions', v_n));
    END LOOP;

    RETURN QUERY SELECT v_admin, v_tenant, v_reset, v_own;
END;
$$;
-- +goose StatementEnd

-- identity_consume_reset: an identity link (M14-14). The same as the row link's linked case: single
-- use, the identity and its linked rows written, every session revoked, no session issued, no
-- unlinked row linked, email_verified_at filled, a RESET-scope window on EVERY consumption (R2, S3).
-- +goose StatementBegin
CREATE FUNCTION public.identity_consume_reset(p_reset_hash text, p_new_hash text)
    RETURNS TABLE (identity_id uuid, revoked_sessions integer)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_reset    uuid;
    v_identity uuid;
    v_n        integer;
    v_all      integer := 0;
    m          record;
BEGIN
    SELECT r.id, r.identity_id INTO v_reset, v_identity
      FROM public.admin_identity_resets r
     WHERE r.token_hash = p_reset_hash
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL
       AND statement_timestamp() < r.expires_at;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The identity first, its memberships, then the link, re-checked.
    PERFORM 1 FROM public.admin_identities i WHERE i.id = v_identity FOR UPDATE;
    -- Lock order (identity -> membership rows -> token / session rows): every membership of the
    -- identity, before any token or session row.
    PERFORM 1 FROM public.admin_users a WHERE a.identity_id = v_identity /* identity-scope */ FOR UPDATE;
    PERFORM 1 FROM public.admin_identity_resets r
     WHERE r.id = v_reset AND r.identity_id = v_identity
       AND r.used_at IS NULL AND r.cancelled_at IS NULL AND statement_timestamp() < r.expires_at
       FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    UPDATE public.admin_identity_resets r SET used_at = now() WHERE r.id = v_reset AND r.used_at IS NULL;
    UPDATE public.admin_identity_resets r SET cancelled_at = now()
     WHERE r.identity_id = v_identity AND r.id <> v_reset
       AND r.used_at IS NULL AND r.cancelled_at IS NULL AND statement_timestamp() < r.expires_at;

    UPDATE public.admin_identities i
       SET password_hash      = p_new_hash,
           email_verified_at  = coalesce(i.email_verified_at, now()),
           confirm_opened_at  = now(),
           confirm_open_until = now() + interval '7 days',
           confirm_scope      = 'reset'
     WHERE i.id = v_identity;

    FOR m IN
        SELECT a.id, a.tenant_id FROM public.admin_users a
         WHERE a.identity_id = v_identity /* identity-scope */
           AND a.unlinked_at IS NULL
         ORDER BY a.tenant_id
    LOOP
        UPDATE public.admin_users a SET password_hash = p_new_hash
         WHERE a.id = m.id AND a.tenant_id = m.tenant_id AND a.identity_id = v_identity;
        UPDATE public.password_resets r SET cancelled_at = now()
         WHERE r.tenant_id = m.tenant_id AND r.admin_user_id = m.id
           AND r.used_at IS NULL AND r.cancelled_at IS NULL AND statement_timestamp() < r.expires_at;
        UPDATE public.admin_sessions s SET revoked_at = now()
         WHERE s.tenant_id = m.tenant_id AND s.admin_user_id = m.id AND s.revoked_at IS NULL;
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_all := v_all + v_n;
        INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
        VALUES (m.tenant_id, m.id, 'admin.recovery.completed', m.id::text,
                pg_catalog.jsonb_build_object('outcome', 'ok', 'revoked_sessions', v_n));
    END LOOP;

    RETURN QUERY SELECT v_identity, v_all;
END;
$$;
-- +goose StatementEnd

-- identity_link_legacy: TWO live sessions (K-2, bulgu 9) -- one on a linked row (the identity's),
-- one on an unlinked, unrefused row -- BOTH fresh (R3). Defence layer: the two addresses are equal.
-- The legacy row takes the identity and its digest (until contract B), linked_via 'password' (the
-- proof is the two passwords; 'account' is the relink's), linked_at now; the legacy session is
-- revoked; the legacy tenant gets admin.membership.linked (naming no other tenant). A membership of
-- the identity already in that tenant -> 0 rows. When the identity's session is valid and the link
-- is refused, one link_refused event is written for that identity.
-- +goose StatementBegin
CREATE FUNCTION public.identity_link_legacy(p_identity_session_hash text, p_legacy_session_hash text)
    RETURNS TABLE (admin_user_id uuid, tenant_id uuid)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_identity       uuid;
    v_identity_email public.citext;
    v_digest         text;
    v_legacy_session uuid;
    v_legacy_tenant  uuid;
    v_legacy_admin   uuid;
    v_legacy_email   public.citext;
    v_id_session     uuid;
    v_id_tenant      uuid;
    v_legacy_live    boolean := false;
    v_legacy_found   boolean;
BEGIN
    SELECT s.id, s.tenant_id, a.identity_id INTO v_id_session, v_id_tenant, v_identity
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_identity_session_hash /* secret-scope */
       AND s.revoked_at IS NULL
       AND s.authenticated_at > statement_timestamp() - interval '5 minutes'
       AND s.authenticated_at <= statement_timestamp()
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT i.email, i.password_hash INTO v_identity_email, v_digest
      FROM public.admin_identities i WHERE i.id = v_identity FOR UPDATE;
    -- Lock order (identity -> membership rows -> token / session rows): every membership of the
    -- identity, before any token or session row.
    PERFORM 1 FROM public.admin_users a WHERE a.identity_id = v_identity /* identity-scope */ FOR UPDATE;

    SELECT s.id, s.tenant_id, a.id, a.email INTO v_legacy_session, v_legacy_tenant, v_legacy_admin, v_legacy_email
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_legacy_session_hash /* secret-scope */
       AND s.token_hash <> p_identity_session_hash
       AND s.revoked_at IS NULL
       AND s.authenticated_at > statement_timestamp() - interval '5 minutes'
       AND s.authenticated_at <= statement_timestamp()
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NULL
       FOR UPDATE OF a;
    v_legacy_found := FOUND;

    -- Then the sessions: the identity's re-checked under the identity lock (READ COMMITTED: the
    -- lock-free read above may predate a reset or a revocation that committed while this call
    -- waited), the legacy one locked and re-checked.
    PERFORM 1
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.id = v_id_session AND s.tenant_id = v_id_tenant
       AND s.revoked_at IS NULL
       AND s.authenticated_at > statement_timestamp() - interval '5 minutes'
       AND s.authenticated_at <= statement_timestamp()
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF s;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    IF v_legacy_found THEN
        PERFORM 1 FROM public.admin_sessions s
         WHERE s.id = v_legacy_session AND s.tenant_id = v_legacy_tenant
           AND s.revoked_at IS NULL
           AND s.authenticated_at > statement_timestamp() - interval '5 minutes'
           AND s.authenticated_at <= statement_timestamp()
           FOR UPDATE;
        v_legacy_live := FOUND;
    END IF;
    IF NOT v_legacy_live
       OR v_legacy_email IS NULL
       OR NOT (v_legacy_email OPERATOR(public.=) v_identity_email)
       OR EXISTS (SELECT 1 FROM public.admin_users o
                   WHERE o.tenant_id = v_legacy_tenant AND o.identity_id = v_identity) THEN
        INSERT INTO public.admin_identity_events (identity_id, event) VALUES (v_identity, 'link_refused');
        RETURN;
    END IF;

    UPDATE public.admin_users a
       SET identity_id = v_identity, linked_via = 'password', linked_at = now(), password_hash = v_digest
     WHERE a.id = v_legacy_admin AND a.tenant_id = v_legacy_tenant AND a.identity_id IS NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    UPDATE public.admin_sessions s SET revoked_at = now()
     WHERE s.id = v_legacy_session AND s.tenant_id = v_legacy_tenant AND s.revoked_at IS NULL;
    INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
    VALUES (v_legacy_tenant, v_legacy_admin, 'admin.membership.linked', v_legacy_admin::text,
            pg_catalog.jsonb_build_object('outcome', 'ok'));

    RETURN QUERY SELECT v_legacy_admin, v_legacy_tenant;
END;
$$;
-- +goose StatementEnd

-- identity_unlink_backfilled: refuse ONE membership from the confirmation list. A FRESH session of
-- the identity (S4; the application re-asks the identity's password first), a verified address, an
-- open window; the row must be listed in the window's scope (first_verify: backfill / reconcile /
-- signup links; reset: every link made before the window opened -- S3, T1). The row: identity_id
-- NULL, unlinked_at now, the SENTINEL digest (S20); linked_via and linked_at kept (T4); its unexpired
-- password_resets links cancelled (T5); its sessions in that tenant revoked. After contract B (a CHECK
-- on admin_users this file does not know refuses identity_id NULL): the link is kept, unlinked_at
-- stamped and status 'disabled' (T1, ONAY notu 2).
-- +goose StatementBegin
CREATE FUNCTION public.identity_unlink_backfilled(p_session_hash text, p_tenant uuid)
    RETURNS TABLE (admin_user_id uuid, revoked_sessions integer)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_identity   uuid;
    v_scope      text;
    v_opened     timestamptz;
    v_target     uuid;
    v_sentinel   text;
    v_constraint text;
    v_n          integer;
    v_session    uuid;
    v_tenant     uuid;
BEGIN
    SELECT s.id, s.tenant_id, a.identity_id INTO v_session, v_tenant, v_identity
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash /* secret-scope */
       AND s.revoked_at IS NULL
       AND s.authenticated_at > statement_timestamp() - interval '5 minutes'
       AND s.authenticated_at <= statement_timestamp()
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT i.confirm_scope, i.confirm_opened_at INTO v_scope, v_opened
      FROM public.admin_identities i
     WHERE i.id = v_identity
       AND i.email_verified_at IS NOT NULL
       AND statement_timestamp() < i.confirm_open_until
       FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    -- Lock order (identity -> membership rows -> token / session rows): every membership of the
    -- identity, before any token or session row.
    PERFORM 1 FROM public.admin_users a WHERE a.identity_id = v_identity /* identity-scope */ FOR UPDATE;
    -- Re-checked under the identity lock (READ COMMITTED: the lock-free read above may predate a
    -- reset or a revocation that committed while this call waited).
    PERFORM 1
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.id = v_session AND s.tenant_id = v_tenant
       AND s.revoked_at IS NULL
       AND s.authenticated_at > statement_timestamp() - interval '5 minutes'
       AND s.authenticated_at <= statement_timestamp()
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF s;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT a.id INTO v_target
      FROM public.admin_users a
     WHERE a.tenant_id = p_tenant
       AND a.identity_id = v_identity
       AND a.unlinked_at IS NULL
       AND ((v_scope = 'first_verify' AND a.linked_via IN ('backfill', 'reconcile', 'signup'))
            OR (v_scope = 'reset' AND a.linked_at < v_opened))
       FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- ORDER (M14-01's audit): the row's UPDATE (sentinel + unlinked_at) runs BEFORE the statement that
    -- revokes the tenant's sessions. A legacy login that holds the row FOR SHARE while it inserts a
    -- session makes the unlink wait; the revocation, a LATER statement, then sees that session and
    -- revokes it (READ COMMITTED takes a new snapshot per statement). Here the wait already comes at
    -- the FOR UPDATE above, so both statements follow it; the order keeps that true if the lock moves.
    BEGIN
        -- The 32 random bytes exist only inside this expression (S20).
        v_sentinel := public.crypt(pg_catalog.encode(public.gen_random_bytes(32), 'hex'), public.gen_salt('bf', 12));
        UPDATE public.admin_users a
           SET identity_id = NULL, unlinked_at = now(), password_hash = v_sentinel
         WHERE a.id = v_target AND a.tenant_id = p_tenant AND a.identity_id = v_identity;
    EXCEPTION WHEN check_violation THEN
        GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME;
        IF v_constraint IN ('admin_users_link_has_a_time', 'admin_users_linked_via_known',
                            'admin_users_password_hash_is_bcrypt', 'admin_users_role_check',
                            'admin_users_status_check') THEN
            RAISE;
        END IF;
        UPDATE public.admin_users a
           SET unlinked_at = now(), status = 'disabled'
         WHERE a.id = v_target AND a.tenant_id = p_tenant AND a.identity_id = v_identity;
    END;

    UPDATE public.password_resets r SET cancelled_at = now()
     WHERE r.tenant_id = p_tenant AND r.admin_user_id = v_target
       AND r.used_at IS NULL AND r.cancelled_at IS NULL AND statement_timestamp() < r.expires_at;
    UPDATE public.admin_sessions s SET revoked_at = now()
     WHERE s.tenant_id = p_tenant AND s.admin_user_id = v_target AND s.revoked_at IS NULL;
    GET DIAGNOSTICS v_n = ROW_COUNT;
    INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
    VALUES (p_tenant, v_target, 'admin.membership.unlinked', v_target::text,
            pg_catalog.jsonb_build_object('outcome', 'ok', 'revoked_sessions', v_n));
    INSERT INTO public.admin_identity_events (identity_id, event, detail)
    VALUES (v_identity, 'membership_unlinked',
            pg_catalog.jsonb_build_object('tenant', p_tenant::text, 'scope', v_scope));

    RETURN QUERY SELECT v_target, v_n;
END;
$$;
-- +goose StatementEnd

-- identity_relink_refused: undo an unlink while THIS window is open (S4, T4). A fresh session of the
-- identity; the row in p_tenant with the identity's address, unlinked (identity_id NULL) at or after
-- the window opened; no other membership of the identity in that tenant. The row is linked again
-- (linked_via 'account', the ORIGINAL linked_at kept -- it stays on the list for the rest of the
-- window), unlinked_at NULL, the identity's digest copied (until contract B).
-- +goose StatementBegin
CREATE FUNCTION public.identity_relink_refused(p_session_hash text, p_tenant uuid)
    RETURNS TABLE (admin_user_id uuid)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_identity uuid;
    v_email    public.citext;
    v_digest   text;
    v_opened   timestamptz;
    v_target   uuid;
    v_session  uuid;
    v_tenant   uuid;
    v_found    boolean;
BEGIN
    SELECT s.id, s.tenant_id, a.identity_id INTO v_session, v_tenant, v_identity
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash /* secret-scope */
       AND s.revoked_at IS NULL
       AND s.authenticated_at > statement_timestamp() - interval '5 minutes'
       AND s.authenticated_at <= statement_timestamp()
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT i.email, i.password_hash, i.confirm_opened_at INTO v_email, v_digest, v_opened
      FROM public.admin_identities i
     WHERE i.id = v_identity
       AND statement_timestamp() < i.confirm_open_until
       FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    -- Lock order (identity -> membership rows -> token / session rows): every membership of the
    -- identity, before any token or session row.
    PERFORM 1 FROM public.admin_users a WHERE a.identity_id = v_identity /* identity-scope */ FOR UPDATE;
    SELECT a.id INTO v_target
      FROM public.admin_users a
     WHERE a.tenant_id = p_tenant
       AND a.email OPERATOR(public.=) v_email
       AND a.identity_id IS NULL
       AND a.unlinked_at >= v_opened
       FOR UPDATE;
    v_found := FOUND;
    -- Re-checked under the identity lock (READ COMMITTED: the lock-free read above may predate a
    -- reset or a revocation that committed while this call waited).
    PERFORM 1
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.id = v_session AND s.tenant_id = v_tenant
       AND s.revoked_at IS NULL
       AND s.authenticated_at > statement_timestamp() - interval '5 minutes'
       AND s.authenticated_at <= statement_timestamp()
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF s;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    IF NOT v_found
       OR EXISTS (SELECT 1 FROM public.admin_users o
                   WHERE o.tenant_id = p_tenant AND o.identity_id = v_identity) THEN
        RETURN;
    END IF;

    UPDATE public.admin_users a
       SET identity_id = v_identity, linked_via = 'account', unlinked_at = NULL, password_hash = v_digest
     WHERE a.id = v_target AND a.tenant_id = p_tenant AND a.identity_id IS NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
    VALUES (p_tenant, v_target, 'admin.membership.relinked', v_target::text,
            pg_catalog.jsonb_build_object('outcome', 'ok'));
    INSERT INTO public.admin_identity_events (identity_id, event, detail)
    VALUES (v_identity, 'membership_relinked', pg_catalog.jsonb_build_object('tenant', p_tenant::text));

    RETURN QUERY SELECT v_target;
EXCEPTION WHEN unique_violation THEN
    -- A membership of the identity in p_tenant committed between the check and the UPDATE.
    RETURN;
END;
$$;
-- +goose StatementEnd

-- identity_request_verification: a verification link bound to the IDENTITY behind the session (not to
-- the session -- S16). An identity already verified gets none. 24 hours, the table's ceiling.
-- +goose StatementBegin
CREATE FUNCTION public.identity_request_verification(p_session_hash text, p_token_hash text)
    RETURNS TABLE (verification_id uuid, expires_at timestamptz)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_identity uuid;
    v_id       uuid;
    v_expires  timestamptz;
BEGIN
    SELECT a.identity_id INTO v_identity
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash /* secret-scope */
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    INSERT INTO public.admin_identity_verifications AS v (identity_id, token_hash, expires_at)
    SELECT i.id, p_token_hash, now() + interval '24 hours'
      FROM public.admin_identities i
     WHERE i.id = v_identity AND i.email_verified_at IS NULL
    RETURNING v.id, v.expires_at INTO v_id, v_expires;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    RETURN QUERY SELECT v_id, v_expires;
END;
$$;
-- +goose StatementEnd

-- identity_verify_email: the link AND a live session of the SAME identity in the clicking browser
-- (R2, S16) -- any of its memberships, so a business switch between request and click does not break
-- it; a click from a browser without that identity's session verifies nothing. The FIRST
-- verification opens a first_verify window.
-- +goose StatementBegin
CREATE FUNCTION public.identity_verify_email(p_verification_hash text, p_session_hash text)
    RETURNS TABLE (first_verification boolean)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_verification uuid;
    v_identity     uuid;
    v_first        boolean;
    v_session      uuid;
    v_tenant       uuid;
BEGIN
    SELECT v.id, v.identity_id INTO v_verification, v_identity
      FROM public.admin_identity_verifications v
     WHERE v.token_hash = p_verification_hash
       AND v.used_at IS NULL
       AND statement_timestamp() < v.expires_at;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT s.id, s.tenant_id INTO v_session, v_tenant
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash /* secret-scope */
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT i.email_verified_at IS NULL INTO v_first
      FROM public.admin_identities i WHERE i.id = v_identity FOR UPDATE;
    PERFORM 1 FROM public.admin_identity_verifications v
     WHERE v.id = v_verification AND v.used_at IS NULL AND statement_timestamp() < v.expires_at
       FOR UPDATE;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    -- Re-checked under the identity lock (READ COMMITTED: the lock-free read above may predate a
    -- reset or a revocation that committed while this call waited).
    PERFORM 1
      FROM public.admin_sessions s
      JOIN public.admin_users a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.id = v_session AND s.tenant_id = v_tenant
       AND s.revoked_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF s;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    UPDATE public.admin_identity_verifications v SET used_at = now() WHERE v.id = v_verification AND v.used_at IS NULL;
    IF v_first THEN
        UPDATE public.admin_identities i
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
$$;
-- +goose StatementEnd

-- identity_record_failed_login -- NAMED EXCEPTION 3 (ADR 0031 §5.2, R9): keyed by the identity id the
-- login resolved, not by a secret, because the password did NOT verify. Harmless by construction: it
-- writes one admin_identity_events row (login_failed, or login_limited for the bucket's single
-- FirstOverLimit row), detail empty -- no address, no IP -- and touches nothing else. An unknown
-- identity or another event -> 0 rows. The bound is the application's per-identity bucket (§5.6).
-- +goose StatementBegin
CREATE FUNCTION public.identity_record_failed_login(p_identity_id uuid, p_event text)
    RETURNS TABLE (recorded boolean)
    LANGUAGE sql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    INSERT INTO public.admin_identity_events (identity_id, event)
    SELECT i.id, p_event
      FROM public.admin_identities i
     WHERE i.id = p_identity_id
       AND p_event IN ('login_failed', 'login_limited')
    RETURNING true;
$$;

-- identity_sweep_events: the ONLY delete path of admin_identity_events (R17, 00058's 90 days). No
-- argument; by age alone.
CREATE FUNCTION public.identity_sweep_events()
    RETURNS TABLE (deleted bigint)
    LANGUAGE sql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    WITH gone AS (
        DELETE FROM public.admin_identity_events e
         WHERE e.created_at < statement_timestamp() - interval '90 days'
        RETURNING 1
    )
    SELECT count(*) FROM gone;
$$;
-- +goose StatementEnd

-- identity_open_reset -- KEYED BY THE IDENTITY ID, NOT BY A SECRET (ADR 0031 §5.2, §5.9; the user's
-- decision of 2026-10-22): the reset request has no secret by nature -- the caller found the identity
-- from the typed address through resolve_identity_by_email. Harmless by construction: it writes ONE
-- admin_identity_resets row and ONE reset_requested event (detail empty -- no address, no IP), and
-- never the identity, a membership or a tenant's audit. ONE LIVE LINK PER IDENTITY (M10 EM-7C,
-- internal/adminauth/resetrequest.go): under a per-identity advisory lock, a link that could be spent
-- right now -- not used, not cancelled, not expired,
-- the consumption's own conditions -- is returned with issued = false and NOTHING is written or
-- retired; only when there is none is a new one issued. Nobody can kill the owner's pending link by
-- asking again, and the account receives at most one recovery mail per link lifetime. An unknown
-- identity -> 0 rows.
-- +goose StatementBegin
CREATE FUNCTION public.identity_open_reset(p_identity_id uuid, p_token_hash text, p_expires_at timestamptz)
    RETURNS TABLE (reset_id uuid, expires_at timestamptz, issued boolean)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_id      uuid;
    v_expires timestamptz;
BEGIN
    -- The per-identity lock: a namespaced transaction advisory lock (passwordresets.sql's
    -- LockAdminForResetIssue pattern; the 'admin-identity-reset:' prefix keeps it apart from the tap
    -- debounce's key space and from 'admin-identity-email:'). N concurrent requests for one identity
    -- queue here; each reads the link the one before it committed.
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-reset:' || p_identity_id::text, 0));
    PERFORM 1 FROM public.admin_identities i WHERE i.id = p_identity_id;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT r.id, r.expires_at INTO v_id, v_expires
      FROM public.admin_identity_resets r
     WHERE r.identity_id = p_identity_id
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL
       AND statement_timestamp() < r.expires_at
     ORDER BY r.expires_at DESC
     LIMIT 1;
    IF FOUND THEN
        RETURN QUERY SELECT v_id, v_expires, false;
        RETURN;
    END IF;

    INSERT INTO public.admin_identity_resets AS r (identity_id, token_hash, expires_at)
    VALUES (p_identity_id, p_token_hash, p_expires_at)
    RETURNING r.id, r.expires_at INTO v_id, v_expires;
    INSERT INTO public.admin_identity_events (identity_id, event) VALUES (p_identity_id, 'reset_requested');
    RETURN QUERY SELECT v_id, v_expires, true;
END;
$$;
-- +goose StatementEnd

-- identity_withdraw_reset: retire ONE link that was issued and could not be handed to its recipient
-- (EM-7C's Withdraw, resetrequest.go): without it one live link per identity would turn a failed send
-- into a link lifetime of nothing. Only cancelled_at moves, only from NULL, only for a link not spent;
-- a link already gone is false, not an error. Keyed by the link's own hash (the caller minted it);
-- writes nothing else -- today's Withdraw records nothing either.
-- +goose StatementBegin
CREATE FUNCTION public.identity_withdraw_reset(p_token_hash text)
    RETURNS boolean
    LANGUAGE sql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    WITH withdrawn AS (
        UPDATE public.admin_identity_resets r
           SET cancelled_at = now()
         WHERE r.token_hash = p_token_hash
           AND r.used_at IS NULL
           AND r.cancelled_at IS NULL
        RETURNING 1
    )
    SELECT EXISTS (SELECT 1 FROM withdrawn);
$$;
-- +goose StatementEnd

-- Owners, PUBLIC out, EXECUTE to tappa_app alone (ADR 0031 §5.2).
ALTER FUNCTION public.resolve_identity_reset_by_token_hash(text) OWNER TO tappa_resolver;
ALTER FUNCTION public.resolve_identity_verification_by_token_hash(text) OWNER TO tappa_resolver;
ALTER FUNCTION public.identity_password_for_session(text) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_set_password(text, text) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_consume_row_reset(text, text) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_consume_reset(text, text) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_link_legacy(text, text) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_unlink_backfilled(text, uuid) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_relink_refused(text, uuid) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_request_verification(text, text) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_verify_email(text, text) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_record_failed_login(uuid, text) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_sweep_events() OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_open_reset(uuid, text, timestamptz) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_withdraw_reset(text) OWNER TO tappa_identitydefiner;

REVOKE ALL ON FUNCTION public.resolve_identity_reset_by_token_hash(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.resolve_identity_verification_by_token_hash(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_password_for_session(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_set_password(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_consume_row_reset(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_consume_reset(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_link_legacy(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_unlink_backfilled(text, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_relink_refused(text, uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_request_verification(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_verify_email(text, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_record_failed_login(uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_sweep_events() FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_open_reset(uuid, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_withdraw_reset(text) FROM PUBLIC;

GRANT EXECUTE ON FUNCTION public.resolve_identity_reset_by_token_hash(text) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.resolve_identity_verification_by_token_hash(text) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_password_for_session(text) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_set_password(text, text) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_consume_row_reset(text, text) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_consume_reset(text, text) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_link_legacy(text, text) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_unlink_backfilled(text, uuid) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_relink_refused(text, uuid) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_request_verification(text, text) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_verify_email(text, text) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_record_failed_login(uuid, text) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_sweep_events() TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_open_reset(uuid, text, timestamptz) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_withdraw_reset(text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 8. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    t   text;
    who text;
BEGIN
    -- The three tables: no role but the two identity owners reaches them (PUBLIC included; a
    -- superuser passes every check and says nothing here).
    FOREACH t IN ARRAY ARRAY['admin_identity_resets', 'admin_identity_verifications', 'admin_identity_events'] LOOP
        FOR who IN SELECT 'public' UNION ALL
                   SELECT rolname::text FROM pg_catalog.pg_roles
                    WHERE NOT rolsuper AND rolname !~ '^pg_'
                      AND rolname NOT IN ('tappa_identitydefiner', 'tappa_resolver') LOOP
            IF pg_catalog.has_any_column_privilege(who, ('public.' || t)::pg_catalog.regclass, 'SELECT')
               OR pg_catalog.has_any_column_privilege(who, ('public.' || t)::pg_catalog.regclass, 'INSERT')
               OR pg_catalog.has_any_column_privilege(who, ('public.' || t)::pg_catalog.regclass, 'UPDATE')
               OR pg_catalog.has_table_privilege(who, ('public.' || t)::pg_catalog.regclass, 'DELETE')
               OR pg_catalog.has_table_privilege(who, ('public.' || t)::pg_catalog.regclass, 'TRUNCATE') THEN
                RAISE EXCEPTION 'migration 00072 postcondition: % reaches %', who, t;
            END IF;
        END LOOP;
    END LOOP;
    -- The sixteen functions: SECURITY DEFINER, the pinned search_path, EXECUTE granted to tappa_app
    -- and to nobody else but the owner.
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                WHERE p.pronamespace = 'public'::pg_catalog.regnamespace
                  AND p.proname IN ('resolve_admin_by_email', 'resolve_identity_reset_by_token_hash',
                                    'resolve_identity_verification_by_token_hash', 'identity_password_for_session',
                                    'identity_set_password', 'identity_consume_row_reset', 'identity_consume_reset',
                                    'identity_link_legacy', 'identity_unlink_backfilled', 'identity_relink_refused',
                                    'identity_request_verification', 'identity_verify_email',
                                    'identity_record_failed_login', 'identity_sweep_events',
                                    'identity_open_reset', 'identity_withdraw_reset')
                  AND (NOT p.prosecdef
                       OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
                       OR NOT pg_catalog.has_function_privilege('tappa_app', p.oid, 'EXECUTE')
                       OR EXISTS (SELECT 1 FROM pg_catalog.aclexplode(p.proacl) x
                                   WHERE x.privilege_type = 'EXECUTE'
                                     AND x.grantee NOT IN (p.proowner, 'tappa_app'::pg_catalog.regrole)))) THEN
        RAISE EXCEPTION 'migration 00072 postcondition: an identity function is not SECURITY DEFINER with the pinned search_path and EXECUTE for tappa_app alone';
    END IF;
    IF pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'email', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'email', 'UPDATE') THEN
        RAISE EXCEPTION 'migration 00072 postcondition: the identity definer may write an e-mail (K-15 keeps it out of M14)';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- Every table the Down takes a strong lock on -- the three it drops, admin_identities (their keys'
-- target) and admin_users (the refusal check reads it) -- in ONE statement, BEFORE the check.
LOCK TABLE admin_identities, admin_identity_resets, admin_identity_verifications, admin_identity_events, admin_users
    IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_identity_resets)
       OR EXISTS (SELECT 1 FROM public.admin_identity_verifications)
       OR EXISTS (SELECT 1 FROM public.admin_identity_events)
       OR EXISTS (SELECT 1 FROM public.admin_users WHERE unlinked_at IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 00072 Down: the identity reset, verification or event tables hold rows, or a membership row is refused; dropping them would lose live links and the event log, and the old resolve_admin_by_email would offer a refused row to the legacy login. Go back with the previous binary, which runs on this schema (ADR 0031 8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP FUNCTION public.identity_withdraw_reset(text);
DROP FUNCTION public.identity_open_reset(uuid, text, timestamptz);
DROP FUNCTION public.identity_sweep_events();
DROP FUNCTION public.identity_record_failed_login(uuid, text);
DROP FUNCTION public.identity_verify_email(text, text);
DROP FUNCTION public.identity_request_verification(text, text);
DROP FUNCTION public.identity_relink_refused(text, uuid);
DROP FUNCTION public.identity_unlink_backfilled(text, uuid);
DROP FUNCTION public.identity_link_legacy(text, text);
DROP FUNCTION public.identity_consume_reset(text, text);
DROP FUNCTION public.identity_consume_row_reset(text, text);
DROP FUNCTION public.identity_set_password(text, text);
DROP FUNCTION public.identity_password_for_session(text);
DROP FUNCTION public.resolve_identity_verification_by_token_hash(text);
DROP FUNCTION public.resolve_identity_reset_by_token_hash(text);

-- resolve_admin_by_email exactly as 00017 left it, grants restated.
DROP FUNCTION public.resolve_admin_by_email(citext);
-- +goose StatementBegin
CREATE FUNCTION resolve_admin_by_email(p_email citext)
    RETURNS TABLE (
        id            uuid,
        tenant_id     uuid,
        password_hash text,
        status        text
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    AS $$
        SELECT a.id, a.tenant_id, a.password_hash, a.status
        FROM public.admin_users AS a
        WHERE a.email OPERATOR(public.=) p_email
        ORDER BY a.created_at, a.tenant_id;
    $$;
-- +goose StatementEnd
ALTER FUNCTION resolve_admin_by_email(citext) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION resolve_admin_by_email(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION resolve_admin_by_email(citext) TO tappa_app;

-- ONLY WHAT 00072 ALONE GRANTS. A column privilege is not counted: M14-04's 00071 grants some of the
-- same columns to the same roles, and revoking them here would take them from 00071 too -- a
-- database walked down to 71 would hold a broken identity_open_session / switch. The shared ones
-- stay (00071's Down revokes them):
--   tappa_identitydefiner  admin_identities SELECT (id)
--                          admin_users      SELECT (id, tenant_id, status, identity_id, unlinked_at)
--                          admin_sessions   SELECT (id, tenant_id, admin_user_id, token_hash, revoked_at,
--                                           authenticated_at), UPDATE (revoked_at)
--                          audit_log        INSERT (tenant_id, actor_id, action, target, detail)
--   tappa_resolver         admin_users      SELECT (identity_id, unlinked_at)
-- On a database without 00071 they are left behind by this Down -- a NOLOGIN role with no member and
-- no function that reads them; 00069's Down/Up walk revokes them in its test for that case. The
-- grants on the three tables go with the tables.
REVOKE SELECT (email, linked_via, linked_at),
       UPDATE (password_hash, identity_id, linked_via, linked_at, unlinked_at, status)
    ON admin_users FROM tappa_identitydefiner;
REVOKE SELECT (id, tenant_id, admin_user_id, token_hash, expires_at, used_at, cancelled_at),
       UPDATE (used_at, cancelled_at)
    ON password_resets FROM tappa_identitydefiner;
REVOKE SELECT (email, password_hash, email_verified_at, confirm_opened_at, confirm_open_until, confirm_scope),
       UPDATE (password_hash, email_verified_at, confirm_opened_at, confirm_open_until, confirm_scope)
    ON admin_identities FROM tappa_identitydefiner;

DROP TABLE admin_identity_events;
DROP TABLE admin_identity_verifications;
DROP TABLE admin_identity_resets;
