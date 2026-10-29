-- 00084 -- M14-22: link a person's other businesses by e-mail proof (ADR 0031 §5.2, §5.7's note of
-- 2026-10-29; the plan's card M14-22). The user's two decisions (2026-10-28/29): (1) "E-posta koduyla,
-- tek tık" -- the proof that another business's row is the person's is the MAILBOX (Slack's "find your
-- workspaces" confirmation), not the two passwords of identity_link_legacy's account form; (2) the
-- panel's banner does not name the business before that proof.
--
-- WHAT THIS FILE DOES -- expand only, the binary before it (main fd53d34e) keeps working on it:
--   1. admin_identity_verifications.purpose ('verify' | 'link', DEFAULT 'verify'). The table stays the
--      one place a mailed link of an EXISTING identity lives (ADR 0031 §5.1, M14-18); the column keeps
--      the two intents apart: a verification link never links a business, and a link link is issued
--      to an identity whose address is already verified (identity_request_verification's 'verify'
--      purpose refuses one -- M14-18's rule, unchanged).
--   2. admin_users.linked_via gains 'email' (the CHECK keeps its name: identity_unlink_backfilled
--      reads it by name, 00072/00079).
--   3. identity_request_verification gains a third argument, p_purpose text DEFAULT 'verify' (DROP +
--      CREATE: the signature changes; ADR 0031 §5.2's T5 rule -- owner, SECURITY DEFINER, search_path,
--      REVOKE ALL FROM PUBLIC, GRANT EXECUTE TO tappa_app restated). The old binary's two-argument
--      call resolves to it through the DEFAULT and gets 00072's behaviour byte for byte (00080's
--      precedent for identity_claim_signup).
--   4. identity_link_by_email(p_session_hash, p_verification_hash) -- the ONE new function of the
--      closed list (ADR 0031 §5.2's M14-22 note). Keyed by TWO SECRETS: the mailed link's token hash
--      (purpose 'link', unspent, unexpired) and a live session of THE SAME identity in the clicking
--      browser (R2, S16 -- identity_verify_email's rule). It finds the identity from them, never from an
--      address (CLAUDE.md §4.5 (3): no new named exception), and then links every ACTIVE, UNREFUSED,
--      UNLINKED row of the identity's own registered address, in one transaction:
--        - 6b (00076's address rule) on every candidate row: a homoglyph row is never linked;
--        - the row takes the identity's digest (the dual-write, ADR 0031 §5.1), linked_via 'email',
--          linked_at now; its sessions are revoked; its business gets admin.membership.linked
--          (identity_link_legacy's row, byte for byte: no other business, no count, no identity);
--        - the link is spent; the first mailbox proof verifies the address and opens a first_verify
--          window ('email' links are not on that list -- like 'claim' and 'invite');
--        - it returns (linked, first_verification, email): the count and the registered address for
--          the one notification letter (00075's precedent for a returned address).
--
-- WHAT IT DOES NOT DO: identity_link_legacy keeps tappa_app's EXECUTE. The binary before this file
-- calls it from the account page's two-password form AND from the sign-in's automatic link (ADR 0031
-- §5.6, §5.7 (1)); this card's binary still calls it from the sign-in. A REVOKE here would break the
-- old pod during the rollout and the new binary's sign-in after it (ADR 0031 §8's expand/contract
-- rule). The function stays until contract B either way.
--
-- LOCK ORDER (00072's header): the identity, every membership of it, the candidate rows (FOR UPDATE, in
-- (tenant, id) order), the link, the session. Two presses of one link queue on the identity's lock; the
-- second finds the link spent under its own lock and links nothing.
--
-- LOCKS: admin_users and admin_sessions are hot (every panel request); this file alters only admin_users
-- (admin_sessions is untouched). The CHECK swap takes ACCESS EXCLUSIVE on admin_users for one scan of a
-- small table, under this file's lock_timeout; past it the
-- file fails with 55P03 and changes nothing -- run it again. The verification table is not hot.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.identity_request_verification(text,text)') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_verify_email(text,text)') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_reconcile()') IS NULL
       OR pg_catalog.to_regclass('public.admin_identity_verifications') IS NULL THEN
        RAISE EXCEPTION 'migration 00084 (M14-22) needs 00072 (the verification table and its two functions) and 00082. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The link's purpose
-- ---------------------------------------------------------------------------
ALTER TABLE admin_identity_verifications
    ADD COLUMN purpose text NOT NULL DEFAULT 'verify',
    ADD CONSTRAINT admin_identity_verifications_purpose_known CHECK (purpose IN ('verify', 'link'));
GRANT SELECT (purpose), INSERT (purpose) ON admin_identity_verifications TO tappa_identitydefiner;

-- ---------------------------------------------------------------------------
-- 2. linked_via gains 'email'
-- ---------------------------------------------------------------------------
SET LOCAL lock_timeout = '1s';
ALTER TABLE admin_users
    DROP CONSTRAINT admin_users_linked_via_known,
    ADD CONSTRAINT admin_users_linked_via_known
        CHECK (linked_via IN ('backfill', 'reconcile', 'password', 'account', 'invite', 'signup', 'claim', 'email'));

-- ---------------------------------------------------------------------------
-- 3. identity_request_verification, with the purpose
-- ---------------------------------------------------------------------------
-- 'verify' (the DEFAULT) is 00072's function: an identity already verified gets none. 'link' is issued
-- whatever the verification says. Anything else: 0 rows. 24 hours, the table's ceiling, both.
DROP FUNCTION public.identity_request_verification(text, text);
-- +goose StatementBegin
CREATE FUNCTION public.identity_request_verification(p_session_hash text, p_token_hash text, p_purpose text DEFAULT 'verify')
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
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_request_verification(text, text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_request_verification(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_request_verification(text, text, text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 4. identity_link_by_email
-- ---------------------------------------------------------------------------
-- 0 rows (nothing written, the link not spent) when: the link is unknown, a verification link, spent
-- or expired; the clicking browser holds no live session of the link's identity; a membership of the
-- identity committed in one of the businesses meanwhile. Otherwise one row: how many businesses were
-- linked (0 when none was left -- the link is spent all the same), whether this was the address's first
-- verification, and the identity's registered address.
-- +goose StatementBegin
CREATE FUNCTION public.identity_link_by_email(p_session_hash text, p_verification_hash text)
    RETURNS TABLE (linked integer, first_verification boolean, email text)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
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
    -- of it, then the rows to be linked, then the link and the session.
    SELECT i.email, i.password_hash, i.email_verified_at INTO v_identity_email, v_digest, v_verified
      FROM public.admin_identities AS i
     WHERE i.id = v_identity
       FOR UPDATE;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;

    -- The link again, under its own lock, the expiry with the clock read again (NULL fails closed):
    -- of two presses of one link, the second finds it spent here and links nothing.
    SELECT v.expires_at INTO v_expires
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
    -- secrets found): every business's active, unrefused, unlinked row of it, locked in (tenant, id)
    -- order. The mailbox proof is the link; the person's live session is the second key.
    FOR r IN
        SELECT a.id, a.tenant_id, a.email
          FROM public.admin_users AS a
         WHERE a.email OPERATOR(public.=) v_identity_email
           AND a.identity_id IS NULL
           AND a.unlinked_at IS NULL
           AND a.status = 'active'
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
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_link_by_email(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_link_by_email(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_link_by_email(text, text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 5. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    -- The two functions: the definer's, SECURITY DEFINER, the pinned search_path, EXECUTE for
    -- tappa_app and nobody else but the owner; the link function carries 6b.
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                WHERE p.oid IN ('public.identity_request_verification(text,text,text)'::pg_catalog.regprocedure,
                                'public.identity_link_by_email(text,text)'::pg_catalog.regprocedure)
                  AND (NOT p.prosecdef
                       OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
                       OR p.proowner <> 'tappa_identitydefiner'::pg_catalog.regrole
                       OR NOT pg_catalog.has_function_privilege('tappa_app', p.oid, 'EXECUTE')
                       OR EXISTS (SELECT 1 FROM pg_catalog.aclexplode(p.proacl) x
                                   WHERE x.privilege_type = 'EXECUTE'
                                     AND x.grantee NOT IN (p.proowner, 'tappa_app'::pg_catalog.regrole))))
       OR NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                       WHERE p.oid = 'public.identity_link_by_email(text,text)'::pg_catalog.regprocedure
                         AND pg_catalog.strpos(p.prosrc, '-- 6b. THE ADDRESS RULE (M14-08a') > 0)
       OR pg_catalog.to_regprocedure('public.identity_request_verification(text,text)') IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00084 postcondition: the request or the link function is not the ADR''s shape (ADR 0031 5.2). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- identity_link_legacy is untouched: the previous binary and this one call it (the header).
    IF NOT pg_catalog.has_function_privilege('tappa_app', 'public.identity_link_legacy(text,text)'::pg_catalog.regprocedure, 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00084 postcondition: tappa_app lost identity_link_legacy, which the running binaries call. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- REFUSES while a business is linked by e-mail (00069's CHECK would not hold, and unlinking it here
-- would take a membership from a person behind their back) or a LINK link is still live (with the column
-- gone it would read as a verification link). A spent or expired link link is harmless: it can be
-- spent by nothing, so it stays as a row of the table (no column says it was a link). A link committed
-- between the check and the CHECK swap below makes the swap fail (nothing changed); a link link issued
-- in that gap becomes a verification link -- a mailbox proof all the same, it can verify and link nothing.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_users WHERE linked_via = 'email')
       OR EXISTS (SELECT 1 FROM public.admin_identity_verifications
                   WHERE purpose = 'link' AND used_at IS NULL AND expires_at > pg_catalog.now()) THEN
        RAISE EXCEPTION 'migration 00084 Down: a business is linked by e-mail or a link e-mail is still live; the way back is the previous binary, which runs on this schema (ADR 0031 8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP FUNCTION public.identity_link_by_email(text, text);
DROP FUNCTION public.identity_request_verification(text, text, text);

-- 00072's identity_request_verification, byte for byte, its owner and grants restated.
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
    INSERT INTO public.admin_identity_verifications AS v (identity_id, token_hash, expires_at)
    SELECT i.id, p_token_hash, now() + interval '24 hours'
      FROM public.admin_identities AS i
     WHERE i.id = v_identity AND i.email_verified_at IS NULL
    RETURNING v.id, v.expires_at INTO v_id, v_expires;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    RETURN QUERY SELECT v_id, v_expires;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_request_verification(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_request_verification(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_request_verification(text, text) TO tappa_app;

SET LOCAL lock_timeout = '1s';
ALTER TABLE admin_users
    DROP CONSTRAINT admin_users_linked_via_known,
    ADD CONSTRAINT admin_users_linked_via_known
        CHECK (linked_via IN ('backfill', 'reconcile', 'password', 'account', 'invite', 'signup', 'claim'));

REVOKE SELECT (purpose), INSERT (purpose) ON admin_identity_verifications FROM tappa_identitydefiner;
ALTER TABLE admin_identity_verifications
    DROP CONSTRAINT admin_identity_verifications_purpose_known,
    DROP COLUMN purpose;
