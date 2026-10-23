-- 00074 -- M14-08: identity_create_for_signup, ADR 0031 §5.2's NAMED EXCEPTION 2 -- the sign-up
-- wizard's dual-write. From this file on, a registration whose address belongs to NO identity and
-- to NO other membership row births an identity with the new owner's digest and links the new row
-- to it (linked_via = 'signup'); an address that is already anyone's leaves the new row unlinked and
-- the identity untouched. Its effect is the reconciliation's (M14-06b): an identity from a one-row
-- e-mail group (R13). The binary calls it in the registration's own transaction whatever
-- TAPPA_ADMIN_IDENTITY says -- write paths do not wait for the flag (ADR 0031 §8). M14-17 stops
-- calling it; M14-19 takes tappa_app's EXECUTE away; contract B (M14-20b) drops it.
--
-- AND (section 2b, M14-08's audit) identity_consume_row_reset is redefined with its signature
-- unchanged, so that a linked row's recovery leaves its business the handler's own
-- admin.recovery.completed detail, key for key -- the row's shape must not say "linked".
--
-- NORMATIVE SOURCE: docs/adr/0031-tek-kimlik-cok-isletme.md §5.2 (the function's row: the
-- structural predicates, the namespaced e-mail lock + ON CONFLICT DO NOTHING -- S13, the SAVEPOINT
-- trap; the definer's verb x column matrix), §5.10 ("the difference between an existing and a new
-- address is visible only through the surfaces the flag closes" -- bulgu 7), §6 (the third §4.5
-- exception); the card is docs/plan/m14-tek-kimlik.md -> M14-08.
--
-- ⚠️ NOT KEYED BY A SECRET -- WHY IT IS HARMLESS AND WHAT MAKES THAT STRUCTURAL. The function finds
-- the identity's address in the admin_users row it is handed, not in a token. What keeps a caller
-- from pointing it at somebody else's row, each a predicate it checks and every one a refusal (false,
-- nothing written) when it does not hold:
--   1. p_tenant_id is the transaction's own app.tenant_id -- the tenant the caller's RLS context
--      already scopes it to;
--   2. the row is p_admin_user_id of THAT tenant, unlinked, not refused, active;
--   3. the row was WRITTEN IN THIS TRANSACTION: admin_users.created_at = now() (now() is the
--      transaction's start, a SAVEPOINT does not change it, and tappa_app cannot write created_at --
--      00017) and, in addition, xmin = pg_current_xact_id()::xid (xmin says "written by this
--      transaction", not "inserted" -- an UPDATE sets it too; both together leave only a row this
--      transaction made);
--   4. the business was born in this transaction too: tenants.created_at = now() (tappa_app cannot
--      write it -- 00016's grants);
--   5. THE MAIN GUARANTEE (T7): that row is the business's ONLY admin_users row -- a business someone
--      already runs is never a sign-up;
--   6. the address is filled and the digest has the bcrypt shape admin_identities' CHECK demands
--      (checked here, BEFORE the INSERT: the definer reads password_hash, so a CHECK violation would
--      put the digest into the error's DETAIL -- "Failing row contains" -- and the server log);
--   7. under the address's namespaced lock, the address is in no other admin_users row (any tenant,
--      refused rows included) and in no identity (ON CONFLICT (email) DO NOTHING).
-- So a caller can birth an identity only for the one row it inserted, in a business it created, in
-- this very transaction -- what the reconciliation would do for that row a deploy later.
--
-- 🔴 THE SAVEPOINT TRAP (S13), MEASURED ON 17.10 BEFORE WRITING: a row inserted inside a SAVEPOINT
-- carries the SUBtransaction's xid, and pg_current_xact_id() is the TOP-level one -- `xmin =
-- pg_current_xact_id()::xid` is true at the top level and false under a savepoint. The refusal is
-- safe (the row is born unlinked) but silent, so internal/domain/signup's Provision runs in the
-- top-level db.WithTenant (signup.go), never in the savepointing db.WithTenantIn, and its real-path
-- test is red if no identity is born.
--
-- CONCURRENCY (R13). Two registrations of one new address in READ COMMITTED would both see "no other
-- row, no identity". The lock -- pg_advisory_xact_lock(hashtextextended('admin-identity-email:' ||
-- lower(email), 0)), the repository's namespaced-key pattern (passwordresets.sql's 'admin-reset:',
-- invites.sql's 'invite-limits:'), apart from the tap debounce's "<tenant>:<employee>" space --
-- serialises them: the second waits for the first's COMMIT and then sees its row (a fresh snapshot per
-- statement), so it links nothing. ON CONFLICT (email) DO NOTHING is the second belt (a writer that
-- does not take the lock). Neither answers 500. Every identity INSERT of ADR 0031 §5.2 (reconcile,
-- accept_invite_new, claim_signup) takes the same lock with the same expression.
--
-- NO AUDIT ROW, ON PURPOSE. A row here ("linked") would exist for a new address and not for a known
-- one -- the very difference §5.10 keeps off every surface the flag does not close, written into the
-- registering business's own trail. The backfill (00073) and the reconciliation link without one
-- for the same reason this does what they do. The registration's own signup.completed row is
-- unchanged and identical in both cases.
--
-- THE GRANTS (ADR 0031 §5.2's matrix; identitycatalog_test.go pins them and shows each one used):
-- admin_identities INSERT (email, password_hash) -- the birth; admin_users SELECT (password_hash) --
-- the digest the identity is born with -- and the system column xmin (predicate 3); tenants SELECT
-- (created_at) -- predicate 4. Everything else the body touches 00071/00072 already granted.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.identity_set_password(text,text)') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_consume_row_reset(text,text)') IS NULL
       OR pg_catalog.to_regclass('public.admin_identities') IS NULL THEN
        RAISE EXCEPTION 'migration 00074 (M14-08) needs 00069-00073 (admin_identities and the identity functions). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                WHERE p.pronamespace = 'public'::pg_catalog.regnamespace
                  AND p.proname = 'identity_create_for_signup') THEN
        RAISE EXCEPTION 'migration 00074 (M14-08) creates identity_create_for_signup, and it is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The column grants (ADR 0031 §5.2's matrix -- what THIS function needs and 00071/00072 did not give)
-- ---------------------------------------------------------------------------
GRANT INSERT (email, password_hash) ON admin_identities TO tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
GRANT SELECT (password_hash, xmin) ON admin_users TO tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
GRANT SELECT (created_at) ON tenants TO tappa_identitydefiner;

-- ---------------------------------------------------------------------------
-- 2. identity_create_for_signup -- NAMED EXCEPTION 2 (ADR 0031 §5.2)
-- ---------------------------------------------------------------------------
-- Written in the SQL subset M14-04's belt models (identitybelt_test.go): every relation as
-- `public.<name> AS <alias>`, every WHERE a conjunction of `X = Y` and IS [NOT] NULL; the predicates
-- that are not a row's key are decided in plpgsql, each one `IS NOT TRUE` -> refused, so a NULL
-- fails closed. The e-mail-wide read is the belt's one named e-mail-bounded statement
-- (identityEmailWideReads). It returns true when it linked the row to a new identity, false
-- otherwise; the caller keeps the answer to itself (see the header: no surface may show it).
-- +goose StatementBegin
CREATE FUNCTION public.identity_create_for_signup(p_tenant_id uuid, p_admin_user_id uuid)
    RETURNS boolean
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict error
DECLARE
    v_admin       uuid;
    v_tenant      uuid;
    v_email       public.citext;
    v_digest      text;
    v_xmin        xid;
    v_born        timestamptz;
    v_tenant_born timestamptz;
    v_rows        bigint;
    v_identity    uuid;
BEGIN
    IF p_tenant_id IS NULL OR p_admin_user_id IS NULL THEN
        RETURN false;
    END IF;
    -- 1. The caller's own tenant context (the GUC db.WithTenant sets, NULLIF as every policy reads it).
    IF (p_tenant_id = NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid) IS NOT TRUE THEN
        RETURN false;
    END IF;

    -- 2. The row, of that tenant, unlinked, not refused, active.
    SELECT a.id, a.tenant_id, a.email, a.password_hash, a.xmin, a.created_at
      INTO v_admin, v_tenant, v_email, v_digest, v_xmin, v_born
      FROM public.admin_users AS a
     WHERE a.id = p_admin_user_id
       AND a.tenant_id = p_tenant_id
       AND a.identity_id IS NULL
       AND a.unlinked_at IS NULL
       AND a.status = 'active';
    IF NOT FOUND THEN
        RETURN false;
    END IF;

    -- 4. The business's birth.
    SELECT t.created_at INTO v_tenant_born
      FROM public.tenants AS t
     WHERE t.id = p_tenant_id;

    -- 5. Its every membership row (the main guarantee).
    SELECT pg_catalog.count(*) INTO v_rows
      FROM public.admin_users AS a
     WHERE a.tenant_id = p_tenant_id;

    -- 3, 4, 5, 6 -- together; any NULL fails closed.
    IF (v_xmin = pg_catalog.pg_current_xact_id()::xid
        AND v_born = pg_catalog.now()
        AND v_tenant_born = pg_catalog.now()
        AND v_rows = 1
        AND pg_catalog.btrim(v_email::text) <> ''
        AND v_digest ~ '^\$2[aby]\$(0[4-9]|1[0-4])\$[./A-Za-z0-9]{53}$') IS NOT TRUE THEN
        RETURN false;
    END IF;

    -- 7. The address's lock, then no other row and no identity carries it.
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-email:' || pg_catalog.lower(v_email::text), 0));
    -- NAMED E-MAIL-WIDE READ: every tenant's rows of this one address, refused ones included; it
    -- writes nothing and returns nothing but FOUND.
    PERFORM 1
       FROM public.admin_users AS a
      WHERE a.email OPERATOR(public.=) v_email
        AND a.id <> p_admin_user_id;
    IF FOUND THEN
        RETURN false;
    END IF;

    INSERT INTO public.admin_identities AS i (email, password_hash)
    VALUES (v_email, v_digest)
    ON CONFLICT (email) DO NOTHING
    RETURNING i.id INTO v_identity;
    IF v_identity IS NULL THEN
        RETURN false;
    END IF;

    UPDATE public.admin_users AS a
       SET identity_id = v_identity,
           linked_via  = 'signup',
           linked_at   = pg_catalog.now()
     WHERE a.id = p_admin_user_id
       AND a.tenant_id = p_tenant_id
       AND a.identity_id IS NULL;
    IF NOT FOUND THEN
        -- Unreachable: the row is this transaction's own and was unlinked a moment ago. If it ever is,
        -- the identity just born must not stand without its link -- the registration fails whole.
        RAISE EXCEPTION 'identity_create_for_signup: the new row could not be linked'
            USING ERRCODE = 'serialization_failure';
    END IF;
    RETURN true;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_create_for_signup(uuid, uuid) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_create_for_signup(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_create_for_signup(uuid, uuid) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 2b. identity_consume_row_reset, redefined (M14-08 audit: the trail of a linked row's recovery)
-- ---------------------------------------------------------------------------
-- From M14-08 on, a per-row link of a LINKED row is spent by this function and the handler writes no
-- row of its own (adminauth.ConsumedReset.Recorded). 00072's body wrote {"outcome":"ok",
-- "revoked_sessions":n} -- no reset_id, no retired_count, and revoked_sessions even when 0, where the
-- handler's row (adminResetDetail, omitempty) has reset_id, retired_count when > 0 and
-- revoked_sessions when > 0. The SHAPE of the row would then tell the business's owner that the
-- account is linked -- on a surface the flag does not close. The body below writes, in the link's
-- own business, exactly the handler's keys and encodings (retired_count is the cancel UPDATE's
-- ROW_COUNT, the legacy statement's `retired` set: the row's other unused, uncancelled, unexpired
-- links); internal/handler's identityresetaudit_db_test.go compares the two rows byte for byte.
-- SAME SIGNATURE (CREATE OR REPLACE: owner and ACL survive, and are restated anyway, ADR 0031 §5.2
-- T5); the Down puts 00072's body back. Everything else is 00072's body, unchanged.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_consume_row_reset(p_reset_hash text, p_new_hash text)
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
    v_now      timestamptz;
    v_expires  timestamptz;
    v_found    boolean;
    v_n        integer;
    v_retired  integer;
    v_detail   jsonb;
    v_own      integer := 0;
    m          record;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    -- Read without a lock, only to learn which identity to lock.
    SELECT r.id, r.tenant_id, r.admin_user_id, a.identity_id INTO v_reset, v_tenant, v_admin, v_identity
      FROM public.password_resets AS r
      JOIN public.admin_users AS a ON a.id = r.admin_user_id AND a.tenant_id = r.tenant_id
     WHERE r.token_hash = p_reset_hash
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL
       AND r.expires_at > v_now
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The identity first, its memberships, then the link, re-checked.
    PERFORM 1 FROM public.admin_identities AS i WHERE i.id = v_identity FOR UPDATE;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    SELECT r.expires_at INTO v_expires
      FROM public.password_resets AS r
      JOIN public.admin_users AS a ON a.id = r.admin_user_id AND a.tenant_id = r.tenant_id
     WHERE r.id = v_reset
       AND r.tenant_id = v_tenant
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF r;
    -- The expiry after the link's own lock, with the clock read again (NULL fails closed).
    v_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    UPDATE public.password_resets AS r SET used_at = now()
     WHERE r.id = v_reset AND r.tenant_id = v_tenant AND r.used_at IS NULL;

    UPDATE public.admin_identities AS i
       SET password_hash      = p_new_hash,
           email_verified_at  = coalesce(i.email_verified_at, now()),
           confirm_opened_at  = now(),
           confirm_open_until = now() + interval '7 days',
           confirm_scope      = 'reset'
     WHERE i.id = v_identity;
    UPDATE public.admin_identity_resets AS r SET cancelled_at = now()
     WHERE r.identity_id = v_identity AND r.used_at IS NULL AND r.cancelled_at IS NULL AND r.expires_at > v_now;

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
        GET DIAGNOSTICS v_retired = ROW_COUNT;
        UPDATE public.admin_sessions AS s SET revoked_at = now()
         WHERE s.tenant_id = m.tenant_id AND s.admin_user_id = m.id AND s.revoked_at IS NULL;
        GET DIAGNOSTICS v_n = ROW_COUNT;
        IF m.tenant_id = v_tenant THEN
            v_own := v_own + v_n;
        END IF;
        -- internal/handler's admin.recovery.completed detail, key for key (adminResetDetail, its
        -- omitempty tags): the link's own business gets reset_id and, when not zero, the siblings this
        -- consumption cancelled (retired_count) -- the legacy statement's `retired` count; every
        -- business gets revoked_sessions only when not zero.
        v_detail := pg_catalog.jsonb_build_object('outcome', 'ok');
        IF m.tenant_id = v_tenant THEN
            v_detail := v_detail || pg_catalog.jsonb_build_object('reset_id', v_reset::text);
            IF v_retired > 0 THEN
                v_detail := v_detail || pg_catalog.jsonb_build_object('retired_count', v_retired);
            END IF;
        END IF;
        IF v_n > 0 THEN
            v_detail := v_detail || pg_catalog.jsonb_build_object('revoked_sessions', v_n);
        END IF;
        INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
        VALUES (m.tenant_id, m.id, 'admin.recovery.completed', m.id::text, v_detail);
    END LOOP;

    RETURN QUERY SELECT v_admin, v_tenant, v_reset, v_own;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_consume_row_reset(text, text) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_consume_row_reset(text, text) SECURITY DEFINER;
ALTER FUNCTION public.identity_consume_row_reset(text, text) SET search_path = pg_catalog, pg_temp;
REVOKE ALL ON FUNCTION public.identity_consume_row_reset(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_consume_row_reset(text, text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 3. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    f oid := 'public.identity_create_for_signup(uuid,uuid)'::pg_catalog.regprocedure;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                    WHERE p.oid = f AND p.prosecdef
                      AND p.proconfig = ARRAY['search_path=pg_catalog, pg_temp']
                      AND p.proowner = 'tappa_identitydefiner'::pg_catalog.regrole)
       OR pg_catalog.has_function_privilege('public', f, 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_operator', f, 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_opdefiner', f, 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', f, 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00074 postcondition: identity_create_for_signup is not the ADR''s shape';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                    WHERE p.oid = 'public.identity_consume_row_reset(text,text)'::pg_catalog.regprocedure
                      AND p.prosecdef AND p.proconfig = ARRAY['search_path=pg_catalog, pg_temp']
                      AND p.proowner = 'tappa_identitydefiner'::pg_catalog.regrole
                      AND NOT pg_catalog.has_function_privilege('public', p.oid, 'EXECUTE')
                      AND pg_catalog.has_function_privilege('tappa_app', p.oid, 'EXECUTE')) THEN
        RAISE EXCEPTION 'migration 00074 postcondition: the redefined identity_consume_row_reset is not the ADR''s shape';
    END IF;
    IF pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'email', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'email', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'email_verified_at', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00074 postcondition: the identity definer reaches an e-mail write or a mailbox proof it may not';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- The identities this function birthed STAY (they are identities like any other, their rows linked
-- with linked_via = 'signup'); only the door goes, and the grants no other file gives.
DROP FUNCTION public.identity_create_for_signup(uuid, uuid);
REVOKE INSERT (email, password_hash) ON admin_identities FROM tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
REVOKE SELECT (password_hash, xmin) ON admin_users FROM tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
REVOKE SELECT (created_at) ON tenants FROM tappa_identitydefiner;

-- 00072's identity_consume_row_reset, byte for byte, its grants restated.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_consume_row_reset(p_reset_hash text, p_new_hash text)
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
    v_now      timestamptz;
    v_expires  timestamptz;
    v_found    boolean;
    v_n        integer;
    v_own      integer := 0;
    m          record;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    -- Read without a lock, only to learn which identity to lock.
    SELECT r.id, r.tenant_id, r.admin_user_id, a.identity_id INTO v_reset, v_tenant, v_admin, v_identity
      FROM public.password_resets AS r
      JOIN public.admin_users AS a ON a.id = r.admin_user_id AND a.tenant_id = r.tenant_id
     WHERE r.token_hash = p_reset_hash
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL
       AND r.expires_at > v_now
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The identity first, its memberships, then the link, re-checked.
    PERFORM 1 FROM public.admin_identities AS i WHERE i.id = v_identity FOR UPDATE;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    SELECT r.expires_at INTO v_expires
      FROM public.password_resets AS r
      JOIN public.admin_users AS a ON a.id = r.admin_user_id AND a.tenant_id = r.tenant_id
     WHERE r.id = v_reset
       AND r.tenant_id = v_tenant
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id = v_identity
       FOR UPDATE OF r;
    -- The expiry after the link's own lock, with the clock read again (NULL fails closed).
    v_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    UPDATE public.password_resets AS r SET used_at = now()
     WHERE r.id = v_reset AND r.tenant_id = v_tenant AND r.used_at IS NULL;

    UPDATE public.admin_identities AS i
       SET password_hash      = p_new_hash,
           email_verified_at  = coalesce(i.email_verified_at, now()),
           confirm_opened_at  = now(),
           confirm_open_until = now() + interval '7 days',
           confirm_scope      = 'reset'
     WHERE i.id = v_identity;
    UPDATE public.admin_identity_resets AS r SET cancelled_at = now()
     WHERE r.identity_id = v_identity AND r.used_at IS NULL AND r.cancelled_at IS NULL AND r.expires_at > v_now;

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
ALTER FUNCTION public.identity_consume_row_reset(text, text) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_consume_row_reset(text, text) SECURITY DEFINER;
ALTER FUNCTION public.identity_consume_row_reset(text, text) SET search_path = pg_catalog, pg_temp;
REVOKE ALL ON FUNCTION public.identity_consume_row_reset(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_consume_row_reset(text, text) TO tappa_app;
