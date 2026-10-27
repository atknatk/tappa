-- 00080 -- M14-17b: an identity whose memberships are ALL inactive can register a new business. Before
-- this, the registration routed a known address to identity_claim_signup's existing-identity branch,
-- which needs that identity's LIVE session -- and with no active membership the person cannot sign in
-- (a dead end; backlog T170). The new address's branch answers 0 rows for it (ON CONFLICT).
--
-- NORMATIVE SOURCE: docs/adr/0031-tek-kimlik-cok-isletme.md §5.2 (the closed list: identity_claim_signup's
-- row and its M14-17b note -- the user's decision of 2026-10-27, "parola kanıtıyla sahiplenme",
-- option (A) "Dördüncü parametre"), §5.10 (the wizard's three paths), §5.8 (the confirmation window),
-- §8 (the flag does not gate registration verification -- nor this branch).
--
-- THE CHANGE: identity_claim_signup gains a FOURTH argument, p_verified_digest text DEFAULT NULL, and a
-- third branch for it: p_session_hash IS NULL AND p_verified_digest IS NOT NULL. Everything else is
-- 00078's body byte for byte (TestIdentity00080_TheBodyIs00078sPlusThePasswordBranch), plus one guard at
-- the top: a session AND a digest together is refused (one proof per call).
--
-- HOW THE DEFINER VERIFIES THE PASSWORD PROOF (identity_open_session's pattern, 00071, the user's
-- decision of 2026-10-23): the APPLICATION compares the re-entered password with the identity's digest
-- (resolve_identity_by_email's password_hash -- no new resolver) and calls this only on a match, with
-- that digest -- adminauth.ProveWithIdentityPassword is the Go type's only producer for this branch
-- (S12). The definer then
--   1. finds the identity by the CLAIM'S address (the claim secret found the claim; R18 holds by
--      construction) under the address's namespaced lock (00074's key -- S13),
--   2. locks it `FOR UPDATE` only WHILE `password_hash = p_verified_digest`: a reset or a password
--      change that committed while this waited re-evaluates to no row under READ COMMITTED -> 0 rows,
--   3. under that lock requires NO active membership (status 'active', unlinked_at IS NULL --
--      has_active_membership's predicate): an identity that can sign in proves itself with its live
--      session (the branch above), never with the password alone -> 0 rows,
--   4. fills email_verified_at when empty, opening the first_verify window with its email_verified
--      event (the click is the mailbox proof, as on the session path), and births the owner row
--      linked_via = 'claim' with the identity's address and digest.
-- 6b (the ASCII address rule) runs before every branch, as in 00078.
--
-- EXPAND-SAFE: the signature changes, so DROP + CREATE in one transaction; the old binary's three-argument
-- call resolves to the new function through the DEFAULT (TestIdentityClaimSignup00080_TheOldBinarysCallStillClaims).
-- Owner, SECURITY DEFINER, search_path, REVOKE ALL FROM PUBLIC and GRANT EXECUTE TO tappa_app are
-- restated (a DROP takes them -- T5). NO NEW GRANT: the branch reads and writes only cells 00071, 00072
-- and 00078 gave the definer (admin_identities SELECT email/password_hash/email_verified_at, UPDATE of the
-- window columns; admin_users SELECT identity_id/status/unlinked_at; admin_identity_events INSERT).
-- The Down gives 00078's definition back byte for byte, its ACL included
-- (TestIdentity00080_DownGivesBack00078sDefinitionByteForByte).
--
-- INDEPENDENT OF 00081 (M14-16b) and of the main-merge lane: it names nothing they add.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.identity_claim_signup(text, uuid, text)') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_claim_signup(text, uuid, text, text)') IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00080 (M14-17b) redefines 00078''s identity_claim_signup(text, uuid, text), and it is not there or the new one already is. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- The signature changes (a fourth argument), so DROP + CREATE; the properties and the grants are
-- restated below (a DROP takes them -- ADR 0031 §5.2, T5).
DROP FUNCTION public.identity_claim_signup(text, uuid, text);
-- +goose StatementBegin
CREATE FUNCTION public.identity_claim_signup(p_claim_hash text, p_tenant_id uuid, p_session_hash text DEFAULT NULL,
                                             p_verified_digest text DEFAULT NULL)
    RETURNS TABLE (admin_user_id uuid)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
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
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_claim_signup(text, uuid, text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_claim_signup(text, uuid, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_claim_signup(text, uuid, text, text) TO tappa_app;

-- POSTCONDITION: one identity_claim_signup, the ADR's shape.
-- +goose StatementBegin
DO $$
DECLARE
    f record;
BEGIN
    IF (SELECT pg_catalog.count(*) FROM pg_catalog.pg_proc p
         WHERE p.pronamespace = 'public'::pg_catalog.regnamespace
           AND p.proname = 'identity_claim_signup') <> 1 THEN
        RAISE EXCEPTION 'migration 00080 postcondition: identity_claim_signup is not exactly one function';
    END IF;
    SELECT p.oid, p.prosecdef, p.proconfig, p.proowner INTO f
      FROM pg_catalog.pg_proc p
     WHERE p.oid = pg_catalog.to_regprocedure('public.identity_claim_signup(text, uuid, text, text)');
    IF NOT FOUND
       OR NOT f.prosecdef
       OR f.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
       OR f.proowner <> 'tappa_identitydefiner'::pg_catalog.regrole
       OR pg_catalog.has_function_privilege('public', f.oid, 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_operator', f.oid, 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_opdefiner', f.oid, 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', f.oid, 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00080 postcondition: identity_claim_signup is not the ADR''s shape';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- 00078's definition, byte for byte, its properties and grants restated. The memberships the password
-- branch made STAY (owner rows like any other, linked_via = 'claim').
DROP FUNCTION public.identity_claim_signup(text, uuid, text, text);
-- +goose StatementBegin
CREATE FUNCTION public.identity_claim_signup(p_claim_hash text, p_tenant_id uuid, p_session_hash text DEFAULT NULL)
    RETURNS TABLE (admin_user_id uuid)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
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
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_claim_signup(text, uuid, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_claim_signup(text, uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_claim_signup(text, uuid, text) TO tappa_app;
