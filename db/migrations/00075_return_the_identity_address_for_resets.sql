-- 00075 -- M14-14: the identity's REGISTERED address reaches the application in exactly two places --
-- resolve_identity_by_email (the reset request's recipient) and identity_consume_reset (the "your
-- password was changed" notice after an identity link was spent). Each is redefined (DROP + CREATE:
-- the return type gains one column, `email text`, appended); nothing else in either body changes.
--
-- WHY (ADR 0031 §5.2's 2026-10-26 note; the user's decision, option (A)). Before this file no function
-- returned admin_identities.email to tappa_app, which holds nothing on the identity tables (00069).
-- Two of M14-14's criteria could not be met without it:
--   1. THE RECIPIENT (M14-01's security audit, inherited note (a)). The per-identity link must go to
--      the address the DATABASE stores for the identity, never to the spelling the visitor typed --
--      the two are equal under citext (OPERATOR(public.=)), not byte for byte, and the reset request's
--      rule since M7-04 is "the recipient is read from the stored row, never from the request"
--      (internal/adminauth/resetrequest.go, ResetGrant). With the column on the resolver's row, the
--      identity id identity_open_reset is called with and the address the link goes to come from ONE
--      resolver result, bound by one Go type (internal/adminauth's identity grant).
--   2. THE NOTICE (M10 EM-9, ADR 0031 §5.14: one "your password was changed" per person). The
--      consumption returned only (identity_id, revoked_sessions); tappa_app cannot map an identity
--      id to any row or address (RLS, no identity grant), so the notice had nowhere to go.
--
-- md.7 (iii) (ADR 0002's M14 note, 2026-10-26): resolve_identity_by_email's new column is the stored
-- spelling of the very key the caller looked up -- no fact the caller did not already hold beyond its
-- case. identity_consume_reset returns it to the holder of a spent link: the mailbox that link was
-- sent to.
--
-- NO NEW GRANT. tappa_resolver holds SELECT (email) on admin_identities since 00071 (its WHERE reads
-- it); tappa_identitydefiner since 00072 (identity_link_legacy compares it). The precondition below
-- refuses a database where either cell is missing, and the postcondition pins the redefined shapes.
--
-- REDEFINITION RULE (ADR 0031 §5.2, T5): a DROP takes the grants with it, so every property is
-- restated -- SECURITY DEFINER, SET search_path = pg_catalog, pg_temp, the owner, REVOKE ALL FROM
-- PUBLIC, GRANT EXECUTE TO tappa_app.
--
-- EXPAND-SAFE (ADR 0031 §8). The binary before this file selects resolve_identity_by_email's columns
-- BY NAME (internal/db/identity.go resolveIdentityByEmailSQL) and keeps working on the wider row; no
-- binary before this file calls identity_consume_reset. The Down gives 00071's and 00072's
-- definitions back byte for byte (TestIdentity00075_DownGivesBackTheOldDefinitionsByteForByte).

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.resolve_identity_by_email(citext)') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_consume_reset(text,text)') IS NULL
       OR (SELECT pg_catalog.pg_get_function_result(p.oid) FROM pg_catalog.pg_proc p
            WHERE p.oid = 'public.resolve_identity_by_email(citext)'::pg_catalog.regprocedure)
          <> 'TABLE(id uuid, password_hash text, email_verified_at timestamp with time zone, has_active_membership boolean)'
       OR (SELECT pg_catalog.pg_get_function_result(p.oid) FROM pg_catalog.pg_proc p
            WHERE p.oid = 'public.identity_consume_reset(text,text)'::pg_catalog.regprocedure)
          <> 'TABLE(identity_id uuid, revoked_sessions integer)' THEN
        RAISE EXCEPTION 'migration 00075 (M14-14) redefines resolve_identity_by_email and identity_consume_reset as 00071/00072 left them, and they are not in that state. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF NOT pg_catalog.has_column_privilege('tappa_resolver', 'public.admin_identities', 'email', 'SELECT')
       OR NOT pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'email', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00075 (M14-14) returns admin_identities.email through tappa_resolver and tappa_identitydefiner, and one of them cannot read it (00071/00072 grant it). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. resolve_identity_by_email (ADR 0002 md.7; ADR 0031 §5.2, §5.6, §5.9) -- + email
-- ---------------------------------------------------------------------------
-- 00071's body and comment hold; the one change is the appended column, cast to text (the caller
-- scans a string; citext's own OID is not one pgx knows).
DROP FUNCTION public.resolve_identity_by_email(citext);
-- +goose StatementBegin
CREATE FUNCTION public.resolve_identity_by_email(p_email citext)
    RETURNS TABLE (
        id                    uuid,
        password_hash         text,
        email_verified_at     timestamptz,
        has_active_membership boolean,
        email                 text
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
                      AND a.unlinked_at IS NULL),
           i.email::text
      FROM public.admin_identities AS i
     WHERE i.email OPERATOR(public.=) p_email;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_identity_by_email(citext) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_identity_by_email(citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_identity_by_email(citext) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 2. identity_consume_reset (ADR 0031 §5.2, §5.9, §5.14) -- + email
-- ---------------------------------------------------------------------------
-- 00072's body, word for word, but for the last statement: the result reads the identity's address
-- from the row the body locked FOR UPDATE at its start (the lock is held to COMMIT; K-15 keeps the
-- address unchangeable anyway), keyed by the identity id the spent link named.
DROP FUNCTION public.identity_consume_reset(text, text);
-- +goose StatementBegin
CREATE FUNCTION public.identity_consume_reset(p_reset_hash text, p_new_hash text)
    RETURNS TABLE (identity_id uuid, revoked_sessions integer, email text)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_reset    uuid;
    v_identity uuid;
    v_now      timestamptz;
    v_expires  timestamptz;
    v_found    boolean;
    v_n        integer;
    v_all      integer := 0;
    m          record;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    SELECT r.id, r.identity_id INTO v_reset, v_identity
      FROM public.admin_identity_resets AS r
     WHERE r.token_hash = p_reset_hash
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL
       AND r.expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The identity first, its memberships, then the link, re-checked.
    PERFORM 1 FROM public.admin_identities AS i WHERE i.id = v_identity FOR UPDATE;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    SELECT r.expires_at INTO v_expires FROM public.admin_identity_resets AS r
     WHERE r.id = v_reset AND r.identity_id = v_identity
       AND r.used_at IS NULL AND r.cancelled_at IS NULL
       FOR UPDATE;
    -- The expiry after the link's own lock, with the clock read again (NULL fails closed).
    v_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    UPDATE public.admin_identity_resets AS r SET used_at = now() WHERE r.id = v_reset AND r.used_at IS NULL;
    UPDATE public.admin_identity_resets AS r SET cancelled_at = now()
     WHERE r.identity_id = v_identity AND r.id <> v_reset
       AND r.used_at IS NULL AND r.cancelled_at IS NULL AND r.expires_at > v_now;

    UPDATE public.admin_identities AS i
       SET password_hash      = p_new_hash,
           email_verified_at  = coalesce(i.email_verified_at, now()),
           confirm_opened_at  = now(),
           confirm_open_until = now() + interval '7 days',
           confirm_scope      = 'reset'
     WHERE i.id = v_identity;

    FOR m IN
        SELECT a.id, a.tenant_id FROM public.admin_users AS a
         WHERE a.identity_id = v_identity
           AND a.unlinked_at IS NULL
         ORDER BY a.tenant_id
    LOOP
        UPDATE public.admin_users AS a SET password_hash = p_new_hash
         WHERE a.id = m.id AND a.tenant_id = m.tenant_id AND a.identity_id = v_identity;
        UPDATE public.password_resets AS r SET cancelled_at = now()
         WHERE r.tenant_id = m.tenant_id AND r.admin_user_id = m.id
           AND r.used_at IS NULL AND r.cancelled_at IS NULL AND r.expires_at > v_now;
        UPDATE public.admin_sessions AS s SET revoked_at = now()
         WHERE s.tenant_id = m.tenant_id AND s.admin_user_id = m.id AND s.revoked_at IS NULL;
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_all := v_all + v_n;
        INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
        VALUES (m.tenant_id, m.id, 'admin.recovery.completed', m.id::text,
                pg_catalog.jsonb_build_object('outcome', 'ok', 'revoked_sessions', v_n));
    END LOOP;

    RETURN QUERY SELECT v_identity, v_all, i.email::text FROM public.admin_identities AS i WHERE i.id = v_identity;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_consume_reset(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_consume_reset(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_consume_reset(text, text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 3. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    f record;
BEGIN
    FOR f IN
        SELECT p.oid, p.prosecdef, p.proconfig, p.proowner, v.owner, v.result
          FROM (VALUES ('public.resolve_identity_by_email(citext)'::pg_catalog.regprocedure, 'tappa_resolver'::pg_catalog.regrole,
                        'TABLE(id uuid, password_hash text, email_verified_at timestamp with time zone, has_active_membership boolean, email text)'),
                       ('public.identity_consume_reset(text,text)'::pg_catalog.regprocedure, 'tappa_identitydefiner'::pg_catalog.regrole,
                        'TABLE(identity_id uuid, revoked_sessions integer, email text)')) AS v (fn, owner, result)
          JOIN pg_catalog.pg_proc AS p ON p.oid = v.fn
    LOOP
        IF NOT f.prosecdef
           OR f.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
           OR f.proowner <> f.owner
           OR pg_catalog.pg_get_function_result(f.oid) <> f.result
           OR pg_catalog.has_function_privilege('public', f.oid, 'EXECUTE')
           OR pg_catalog.has_function_privilege('tappa_operator', f.oid, 'EXECUTE')
           OR pg_catalog.has_function_privilege('tappa_opdefiner', f.oid, 'EXECUTE')
           OR NOT pg_catalog.has_function_privilege('tappa_app', f.oid, 'EXECUTE') THEN
            RAISE EXCEPTION 'migration 00075 postcondition: % is not the ADR''s shape', f.oid::pg_catalog.regprocedure;
        END IF;
    END LOOP;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- 00071's resolve_identity_by_email and 00072's identity_consume_reset, byte for byte, their
-- properties and grants restated (a DROP took them).
DROP FUNCTION public.resolve_identity_by_email(citext);
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

DROP FUNCTION public.identity_consume_reset(text, text);
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
    v_now      timestamptz;
    v_expires  timestamptz;
    v_found    boolean;
    v_n        integer;
    v_all      integer := 0;
    m          record;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    SELECT r.id, r.identity_id INTO v_reset, v_identity
      FROM public.admin_identity_resets AS r
     WHERE r.token_hash = p_reset_hash
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL
       AND r.expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The identity first, its memberships, then the link, re-checked.
    PERFORM 1 FROM public.admin_identities AS i WHERE i.id = v_identity FOR UPDATE;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    SELECT r.expires_at INTO v_expires FROM public.admin_identity_resets AS r
     WHERE r.id = v_reset AND r.identity_id = v_identity
       AND r.used_at IS NULL AND r.cancelled_at IS NULL
       FOR UPDATE;
    -- The expiry after the link's own lock, with the clock read again (NULL fails closed).
    v_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    UPDATE public.admin_identity_resets AS r SET used_at = now() WHERE r.id = v_reset AND r.used_at IS NULL;
    UPDATE public.admin_identity_resets AS r SET cancelled_at = now()
     WHERE r.identity_id = v_identity AND r.id <> v_reset
       AND r.used_at IS NULL AND r.cancelled_at IS NULL AND r.expires_at > v_now;

    UPDATE public.admin_identities AS i
       SET password_hash      = p_new_hash,
           email_verified_at  = coalesce(i.email_verified_at, now()),
           confirm_opened_at  = now(),
           confirm_open_until = now() + interval '7 days',
           confirm_scope      = 'reset'
     WHERE i.id = v_identity;

    FOR m IN
        SELECT a.id, a.tenant_id FROM public.admin_users AS a
         WHERE a.identity_id = v_identity
           AND a.unlinked_at IS NULL
         ORDER BY a.tenant_id
    LOOP
        UPDATE public.admin_users AS a SET password_hash = p_new_hash
         WHERE a.id = m.id AND a.tenant_id = m.tenant_id AND a.identity_id = v_identity;
        UPDATE public.password_resets AS r SET cancelled_at = now()
         WHERE r.tenant_id = m.tenant_id AND r.admin_user_id = m.id
           AND r.used_at IS NULL AND r.cancelled_at IS NULL AND r.expires_at > v_now;
        UPDATE public.admin_sessions AS s SET revoked_at = now()
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
ALTER FUNCTION public.identity_consume_reset(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_consume_reset(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_consume_reset(text, text) TO tappa_app;
