-- 00076 -- M14-08a: the stored address is an address we can send to. identity_create_for_signup
-- (00074, ADR 0031 §5.2 named exception 2) is redefined, SAME SIGNATURE, so that an address
-- internal/mail would not send to births and links nothing, and such a row elsewhere no longer keeps
-- a sendable address from its own identity.
--
-- THE FINDING (M14-08's security audit, measured on PostgreSQL 17.10 musl -- the live image and
-- locale): citext equality folds exactly two non-ASCII code points onto ASCII, U+0130 LATIN CAPITAL
-- LETTER I WITH DOT ABOVE (-> 'i') and U+212A KELVIN SIGN (-> 'k'); the scan covered 0x80..0x10FFFF.
-- So 'ceo@cor' || U&'\0130' || 'p.com' = 'ceo@corip.com' under citext, and the address's
-- advisory-lock key is the same too. Sign-up accepted any UTF-8 address, so whoever registered the
-- homoglyph FIRST had an identity born under their spelling --
-- and the owner of the real mailbox could never recover the address: mail goes to printable-ASCII
-- recipients alone (mail.ValidRecipient), so a reset to the squatted identity's registered spelling
-- is refused. No takeover, no enumeration -- a permanent loss of access.
--
-- THE RULE (user decision 2026-10-26, ADR 0031 §5.1's note): every stored address that can birth or
-- key an identity passes mail.ValidRecipient; a row whose address does not is never linked to or born
-- into an identity. internal/domain/signup refuses such an address at the boundary
-- (ValidateAccount); this file is the database's half, for the binary before it (the expand rollout
-- window: the old pod keeps calling this function with the same two arguments) and for any caller
-- that skips the boundary.
--
-- WHAT CHANGES IN THE BODY (everything else is 00074's, byte for byte):
--   6b. a new predicate, before the address's lock: the row's address is at most 254 octets, holds no
--       "=?" and matches the addr-spec net/mail parses back to itself, in printable ASCII -- dot-atom
--       "@" dot-atom, or an IP domain literal exactly as net.ParseIP accepts it (four decimal octets
--       without a leading zero; the IPv6 forms with "::" standing for at least one group and an
--       optional IPv4 tail; no zone). That is ValidRecipient's rule written out, and
--       internal/db's identityascii_test.go holds the two to the same answer on a table of inputs
--       and a generated corpus (U+0130, U+212A, punycode, spaces, controls, quoting, dots, every literal
--       form). `IS NOT TRUE` -> refused, so a NULL fails closed.
--   7.  the named e-mail-wide read no longer counts a row whose address is not printable ASCII.
--       Without that, a homoglyph row written by the old binary (or by any path before this file)
--       would still keep the real mailbox's owner from their identity at sign-up -- not squatting the
--       identity any more, but blocking its birth. On that set printable ASCII IS the full rule: the
--       caller's own address has passed 6b, and an ASCII address citext-equal to it differs from it
--       in letter case alone, which the rule does not see. The read becomes a FOR loop (the belt's
--       model: the regex is decided in plpgsql, not in the WHERE); it still returns nothing and
--       writes nothing.
--
-- 🔴 NO CHECK ON admin_identities.email -- MEASURED, NOT ASSUMED. A backstop CHECK (printable ASCII)
-- was tried on this database in a rolled-back transaction, with an INSERT from a SECURITY DEFINER
-- body owned by tappa_identitydefiner called as tappa_app. The violation's DETAIL was
--   Failing row contains (id, email, password_hash, ...) = (<uuid>, probe@cor<U+0130>p.example, $2a$12$..., ...)
-- -- the address AND the bcrypt digest, into the error the caller receives and the server log
-- (tappa_identitydefiner is BYPASSRLS, so PostgreSQL's row-description filter keeps every column it
-- can SELECT; it can SELECT both). That is the trap 00073's and 00074's headers document for the
-- digest, and CLAUDE.md §7 bans the address from every log. A CHECK would fire only when a writer
-- forgot the predicate -- and then it would leak. The predicates stay the line: each identity INSERT
-- decides the rule BEFORE it inserts (this body; M14-06b's identity_reconcile gets the same filter
-- in its lane), and identityascii_test.go fails while any function body that inserts into
-- admin_identities lacks this file's rule.
--
-- EXPAND-SAFE (ADR 0031 §8): CREATE OR REPLACE, same arguments, same boolean result; the old binary's
-- call works unchanged and, for a non-ASCII address, now gets false (the row is born unlinked, as for
-- a known address -- ADR 0031 §5.10: the caller never shows the answer). CREATE OR REPLACE keeps the
-- owner and the ACL; both are restated anyway (ADR 0031 §5.2, T5), and so are SECURITY DEFINER and
-- the search_path. No table is touched: no lock but the function's own.
--
-- 00073 (the backfill) is NOT edited -- it is applied on every development and CI database and
-- CLAUDE.md §6 forbids changing an applied migration. Its guard is a read-only precheck before
-- Deploy 1 (deploy/README.md, the expand deploy's second precheck, "adres kuralı"): the backfill's
-- candidate rows whose address is not printable ASCII must number 0, or the deploy stops.
--
-- DOWN: 00074's definition back, byte for byte (TestIdentity00076_DownGivesBack00074ByteForByte).
-- An identity the new body refused to birth was never written, so there is nothing to undo.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.identity_create_for_signup(uuid,uuid)') IS NULL
       OR (SELECT pg_catalog.pg_get_function_result(p.oid) FROM pg_catalog.pg_proc p
            WHERE p.oid = 'public.identity_create_for_signup(uuid,uuid)'::pg_catalog.regprocedure) <> 'boolean' THEN
        RAISE EXCEPTION 'migration 00076 (M14-08a) redefines identity_create_for_signup as 00074 created it, and it is not there in that shape. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. identity_create_for_signup -- 00074's body + 6b + the e-mail-wide read's ASCII rule
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_create_for_signup(p_tenant_id uuid, p_admin_user_id uuid)
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
    r             record;
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

    -- 6b. THE ADDRESS RULE (M14-08a): the address is one internal/mail would send to --
    -- mail.ValidRecipient, the rule Send applies, written out: at most 254 octets, no "=?", and an
    -- addr-spec net/mail parses back to itself in printable ASCII (dot-atom @ dot-atom or an IP
    -- domain literal). Anything else births and links nothing.
    IF (pg_catalog.octet_length(v_email::text) <= 254
        AND pg_catalog.strpos(v_email::text, '=?') = 0
        AND v_email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$') IS NOT TRUE THEN
        RETURN false;
    END IF;

    -- 7. The address's lock, then no other row and no identity carries it.
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-email:' || pg_catalog.lower(v_email::text), 0));
    -- NAMED E-MAIL-WIDE READ: every tenant's rows of this one address, refused ones included; it
    -- writes nothing and returns nothing but whether one of them counts. A row whose address is not
    -- printable ASCII does not (M14-08a): it is no address mail goes to and never an identity's, and
    -- citext folds it onto this one (U+0130, U+212A). On this set printable ASCII IS the address rule:
    -- an ASCII row citext-equal to the valid v_email differs from it in letter case alone, and 6b's
    -- rule does not see case.
    FOR r IN
        SELECT a.email
          FROM public.admin_users AS a
         WHERE a.email OPERATOR(public.=) v_email
           AND a.id <> p_admin_user_id
    LOOP
        IF (r.email::text ~ '^[\x21-\x7e]+$') IS NOT FALSE THEN
            RETURN false;
        END IF;
    END LOOP;

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
ALTER FUNCTION public.identity_create_for_signup(uuid, uuid) SECURITY DEFINER;
ALTER FUNCTION public.identity_create_for_signup(uuid, uuid) SET search_path = pg_catalog, pg_temp;
REVOKE ALL ON FUNCTION public.identity_create_for_signup(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_create_for_signup(uuid, uuid) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 2. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    f oid := 'public.identity_create_for_signup(uuid,uuid)'::pg_catalog.regprocedure;
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                    WHERE p.oid = f AND p.prosecdef
                      AND p.proconfig = ARRAY['search_path=pg_catalog, pg_temp']
                      AND p.proowner = 'tappa_identitydefiner'::pg_catalog.regrole
                      AND pg_catalog.strpos(p.prosrc, '-- 6b. THE ADDRESS RULE (M14-08a)') > 0)
       OR pg_catalog.has_function_privilege('public', f, 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_operator', f, 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_opdefiner', f, 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', f, 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00076 postcondition: identity_create_for_signup is not the ADR''s shape';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- 00074's identity_create_for_signup, byte for byte, its properties and grants restated.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_create_for_signup(p_tenant_id uuid, p_admin_user_id uuid)
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
ALTER FUNCTION public.identity_create_for_signup(uuid, uuid) SECURITY DEFINER;
ALTER FUNCTION public.identity_create_for_signup(uuid, uuid) SET search_path = pg_catalog, pg_temp;
REVOKE ALL ON FUNCTION public.identity_create_for_signup(uuid, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_create_for_signup(uuid, uuid) TO tappa_app;
