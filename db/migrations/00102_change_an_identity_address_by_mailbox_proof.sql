-- 00102 -- T173 EC-1 (ADR 0044 §5.3, §5.5-§5.7; the user's decisions of 2026-11-12): three functions join
-- ADR 0031 §5.2's closed list, and two of its members change. Expand only: the binary before this file
-- calls none of the new three, and the two redefinitions keep what it calls working.
--
--   identity_request_email_change(p_session_hash, p_verified_digest, p_link_hash, p_new_email)
--       keyed by the LIVE SESSION (12 hours, 00085) and the digest Go verified, re-checked under the
--       identity's lock (a reset or a password change in between -> 'stale'). Writes ONE 'change' link
--       for the new address (24 hours, K-E4), cancels the identity's earlier live one, one
--       email_change_requested event. It never asks whether the new address belongs to someone (K-15's
--       fourth requirement: the answer is the same for every address) -- the confirmation does.
--   identity_change_email(p_link_hash, p_session_hash, p_revert_hash)
--       keyed by TWO SECRETS: the 'change' link and a live session of THE SAME identity in the clicking
--       browser (R2). In one transaction: the identity's address, the mirror in every linked row of the
--       person (disabled included -- identityOfSession reads the mirror), every other session, every
--       live reset link (the identity's and the rows'), one admin.email.changed row per business, the
--       link spent, one 'revert' link for the OLD address (7 days, K-E3) and one email_changed event.
--   identity_revert_email_change(p_revert_hash)
--       keyed by the 'revert' link alone -- a RESET-CLASS secret (ADR 0044 §5.3: it writes the address
--       and the password). The old address back, the password a sentinel digest made here in SQL, the
--       confirmation window closed, EVERY session ended, every live link cancelled, one
--       admin.email.reverted row per business, one email_reverted event. It opens no session; the caller
--       mails a reset link to the old address (K-E11, identity_open_reset with its recipient).
--   identity_open_reset -- DROP + CREATE: a fourth argument, p_recipient text DEFAULT NULL (S-1).
--   identity_unlink_backfilled -- the same signature: 0 rows while the identity holds a live 'revert'
--       link (K-E16 (a)).
--
-- NO NEW NAMED EXCEPTION (ADR 0044 §5.3): each writer finds the identity from a secret. The one address
-- READ that is not from a secret's row is the request's "rows of the person's own address that are linked
-- to nobody" (K-E9 (a)) -- identity_link_by_email's named e-mail-wide read (identityEmailWideReads),
-- keyed by the identity's OWN address. The change and the undo read admin_identities by an ADDRESS a
-- secret's row gave them ("no identity holds it") -- the belt's fourth class (identityClaimAddressKeys).
--
-- T5 (ADR 0031 §5.2): every function here is owned by tappa_identitydefiner, SECURITY DEFINER, search_path
-- pinned to pg_catalog, pg_temp, PUBLIC's EXECUTE revoked, EXECUTE to tappa_app; the redefinitions restate
-- all of it. No dynamic SQL. Every statement on a tenant table names its tenant or is one of the belt's
-- named classes (internal/db/identitybelt_test.go), and the bodies are written in its SQL subset.
--
-- LOCK ORDER (ADR 0044 §5.3, S-1): the change and the undo take, in this order, the ADDRESS's advisory lock
-- ('admin-identity-email:' || lower(address) -- every identity birth's key, 00074), then the identity's
-- RESET advisory lock ('admin-identity-reset:' || id -- identity_open_reset's, 00072), then the identity's
-- row, every membership of it, the link, the session. Address before identity is the claim's password
-- branch's order (ADR 0031 §5.6); the reset lock before the row because identity_open_reset's INSERT takes
-- the identity row's FOR KEY SHARE while holding that lock -- the other order would deadlock. An open of a
-- reset link therefore either commits before the change (which then cancels it) or runs after it and
-- reads the new address (its recipient no longer matches: 0 rows).
--
-- EVERY BODY OF THE CHANGE AND THE UNDO IS ONE unique_violation BLOCK (00084's pattern): a writer that
-- wrote the same address without the lock -- the identity's UNIQUE (email), a business's UNIQUE (tenant,
-- email) -- makes the call 0 rows with NOTHING kept (the block is a subtransaction; an inner block would
-- keep the writes before it). An uncaught violation would print the address in its DETAIL and the log.
-- Every token hash is checked for its shape first: a NULL or malformed hash would fail a CHECK whose
-- DETAIL prints the row -- the address.
--
-- AUDIT (ADR 0044 §5.7): one row per linked business, `{"outcome": "ok"}` and -- only when not zero -- the
-- business's revoked session count; never an address, a link, the identity or another business. The
-- request writes no audit row (it is no business's). Events carry an empty detail.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_attribute
                    WHERE attrelid = 'public.admin_identity_verifications'::pg_catalog.regclass
                      AND attname = 'address' AND NOT attisdropped)
       OR NOT pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'email', 'UPDATE')
       OR pg_catalog.to_regprocedure('public.identity_open_reset(uuid,text,timestamptz)') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_unlink_backfilled(text,uuid)') IS NULL THEN
        RAISE EXCEPTION 'migration 00102 (T173 EC-1) needs 00101 (the address column and the definer''s e-mail cells) and 00072''s identity_open_reset. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc
                WHERE pronamespace = 'public'::pg_catalog.regnamespace
                  AND proname IN ('identity_request_email_change', 'identity_change_email', 'identity_revert_email_change')) THEN
        RAISE EXCEPTION 'migration 00102 (T173 EC-1) creates the three e-mail change functions, and one of them is already there. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. identity_request_email_change
-- ---------------------------------------------------------------------------
-- 0 rows: the session is not live (12 hours, revoked, its row disabled, refused or not linked), or it
-- ended while this call waited for the identity's lock, or the link hash is not a hash. Otherwise ONE row:
-- the link written (change_id, expires_at; refusal NULL), or a refusal (change_id NULL) from a CLOSED set
-- that speaks only of the person's own state or of the SHAPE of what they typed:
--   invalid_address  the new address fails 6b (00076's rule)
--   stale            the verified digest is no longer the identity's (a reset or a password change won)
--   same_address     the new address is the identity's own (citext: a case change is no change -- K-E13)
--   unverified       the identity's current address was never proved (K-E9 (a))
--   window_open      a confirmation window is open (K-E9 (a))
--   unlinked_rows    the identity's own address holds an active row linked to nobody (K-E9 (a))
--   revert_live      an undo link of an earlier change lives (K-E9 (d)); expires_at is ITS end (K-E18)
-- +goose StatementBegin
CREATE FUNCTION public.identity_request_email_change(p_session_hash text, p_verified_digest text, p_link_hash text, p_new_email citext)
    RETURNS TABLE (change_id uuid, expires_at timestamptz, refusal text)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
    v_session  uuid;
    v_tenant   uuid;
    v_identity uuid;
    v_email    public.citext;
    v_digest   text;
    v_verified timestamptz;
    v_until    timestamptz;
    v_revert   timestamptz;
    v_now      timestamptz;
    v_id       uuid;
    v_expires  timestamptz;
BEGIN
    IF (p_link_hash ~ '^[0-9a-f]{64}$') IS NOT TRUE THEN
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
    -- 6b. THE ADDRESS RULE (M14-08a, 00076's predicate) on the new address: only an address internal/mail
    -- would send to is written into a link. NULL fails closed.
    IF (pg_catalog.octet_length(p_new_email::text) <= 254
        AND pg_catalog.strpos(p_new_email::text, '=?') = 0
        AND p_new_email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$') IS NOT TRUE THEN
        RETURN QUERY SELECT NULL::uuid, NULL::timestamptz, 'invalid_address'::text;
        RETURN;
    END IF;

    -- Lock order: the identity, every membership of it, then the session (re-checked).
    SELECT i.email, i.password_hash, i.email_verified_at, i.confirm_open_until INTO v_email, v_digest, v_verified, v_until
      FROM public.admin_identities AS i
     WHERE i.id = v_identity
       FOR UPDATE;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
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

    -- The digest the application verified, under the lock (the user's decision of 2026-10-23).
    IF (v_digest = p_verified_digest) IS NOT TRUE THEN
        RETURN QUERY SELECT NULL::uuid, NULL::timestamptz, 'stale'::text;
        RETURN;
    END IF;
    IF (p_new_email OPERATOR(public.=) v_email) IS TRUE THEN
        RETURN QUERY SELECT NULL::uuid, NULL::timestamptz, 'same_address'::text;
        RETURN;
    END IF;
    -- K-E9 (a): the current address is proved, no window is open, no row of it waits to be linked.
    IF v_verified IS NULL THEN
        RETURN QUERY SELECT NULL::uuid, NULL::timestamptz, 'unverified'::text;
        RETURN;
    END IF;
    IF (v_until > v_now) IS TRUE THEN
        RETURN QUERY SELECT NULL::uuid, NULL::timestamptz, 'window_open'::text;
        RETURN;
    END IF;
    -- NAMED E-MAIL-WIDE READ (identityEmailWideReads): the identity's OWN address, read above from the row
    -- the session found -- the rows M14-22's banner offers to link (HasBusinessToAdd's set). After a change
    -- the banner would read the new address and never show them again.
    PERFORM 1
      FROM public.admin_users AS a
     WHERE a.email OPERATOR(public.=) v_email
       AND a.identity_id IS NULL
       AND a.unlinked_at IS NULL
       AND a.status = 'active';
    IF FOUND THEN
        RETURN QUERY SELECT NULL::uuid, NULL::timestamptz, 'unlinked_rows'::text;
        RETURN;
    END IF;
    -- K-E9 (d): one change per undo window -- no chain of undo links (ADR 0044 §5.5, §8 risk 6).
    SELECT v.expires_at INTO v_revert
      FROM public.admin_identity_verifications AS v
     WHERE v.identity_id = v_identity
       AND v.purpose = 'revert'
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       AND v.expires_at > v_now
     ORDER BY v.expires_at DESC
     LIMIT 1;
    IF FOUND THEN
        RETURN QUERY SELECT NULL::uuid, v_revert, 'revert_live'::text;
        RETURN;
    END IF;

    -- One live change per identity: the earlier one is retired, this one written.
    UPDATE public.admin_identity_verifications AS v SET cancelled_at = pg_catalog.now()
     WHERE v.identity_id = v_identity
       AND v.purpose = 'change'
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL;
    INSERT INTO public.admin_identity_verifications AS v (identity_id, token_hash, expires_at, purpose, address)
    VALUES (v_identity, p_link_hash, pg_catalog.now() + interval '24 hours', 'change', p_new_email)
    RETURNING v.id, v.expires_at INTO v_id, v_expires;
    INSERT INTO public.admin_identity_events (identity_id, event) VALUES (v_identity, 'email_change_requested');
    RETURN QUERY SELECT v_id, v_expires, NULL::text;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_request_email_change(text, text, text, citext) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_request_email_change(text, text, text, citext) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_request_email_change(text, text, text, citext) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 2. identity_change_email
-- ---------------------------------------------------------------------------
-- 0 rows, nothing written: the link is unknown, cancelled, spent, expired or not a 'change' link; the
-- clicking browser holds no live session of the link's identity; the new address fails 6b, belongs to
-- an identity now, or is another row's in one of the person's businesses; a K-E9 precondition no longer
-- holds; the undo link's hash is not a hash; a concurrent writer of the same address committed. Otherwise
-- ONE row: the two addresses (the notice's and the answer's recipients -- 00075's returned-address
-- precedent), the sessions ended in the clicking browser's business, the undo link's end.
-- +goose StatementBegin
CREATE FUNCTION public.identity_change_email(p_link_hash text, p_session_hash text, p_revert_hash text)
    RETURNS TABLE (old_email text, new_email text, revoked_sessions integer, revert_expires_at timestamptz)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'; -- 00085: a session lives 12 h from its password proof
    v_link     uuid;
    v_identity uuid;
    v_new      public.citext;
    v_session  uuid;
    v_tenant   uuid;
    v_old      public.citext;
    v_verified timestamptz;
    v_until    timestamptz;
    v_expires  timestamptz;
    v_address  public.citext;
    v_found    boolean;
    v_now      timestamptz;
    v_n        integer;
    v_own      integer := 0;
    v_detail   jsonb;
    v_revert   timestamptz;
    m          record;
BEGIN
    IF (p_revert_hash ~ '^[0-9a-f]{64}$') IS NOT TRUE THEN
        RETURN;
    END IF;
    v_now := pg_catalog.clock_timestamp();
    -- The link, read without a lock, only to learn its identity and the new address.
    SELECT v.id, v.identity_id, v.address INTO v_link, v_identity, v_new
      FROM public.admin_identity_verifications AS v
     WHERE v.token_hash = p_link_hash
       AND v.purpose = 'change'
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       AND v.expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    -- The clicking browser's session, read without a lock: a live session of THE LINK'S identity (R2).
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
    -- 6b. THE ADDRESS RULE (M14-08a, 00076's predicate) on the address the identity takes. NULL fails closed.
    IF (pg_catalog.octet_length(v_new::text) <= 254
        AND pg_catalog.strpos(v_new::text, '=?') = 0
        AND v_new::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$') IS NOT TRUE THEN
        RETURN;
    END IF;

    -- Lock order (the header): the new address, the identity's reset lock, the identity, its memberships,
    -- the link, the session.
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-email:' || pg_catalog.lower(v_new::text), 0));
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-reset:' || v_identity::text, 0));
    SELECT i.email, i.email_verified_at, i.confirm_open_until INTO v_old, v_verified, v_until
      FROM public.admin_identities AS i
     WHERE i.id = v_identity
       FOR UPDATE;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    SELECT v.expires_at, v.address INTO v_expires, v_address
      FROM public.admin_identity_verifications AS v
     WHERE v.id = v_link
       AND v.identity_id = v_identity
       AND v.purpose = 'change'
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       FOR UPDATE;
    v_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now AND v_address::text = v_new::text) IS NOT TRUE THEN
        RETURN;
    END IF;
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

    -- THE RE-CHECKS, under the locks. No identity holds the new address now (the fourth class of the
    -- belt: an address a secret's row gave -- identityClaimAddressKeys).
    PERFORM 1 FROM public.admin_identities AS o WHERE o.email OPERATOR(public.=) v_new;
    IF FOUND THEN
        RETURN;
    END IF;
    -- K-E9 (a) and (d), still: the address proved, no window open, no row of it waits to be linked (the
    -- named e-mail-wide read, the identity's own address), no undo link lives.
    IF v_verified IS NULL OR (v_until > v_now) IS TRUE THEN
        RETURN;
    END IF;
    PERFORM 1
      FROM public.admin_users AS a
     WHERE a.email OPERATOR(public.=) v_old
       AND a.identity_id IS NULL
       AND a.unlinked_at IS NULL
       AND a.status = 'active';
    IF FOUND THEN
        RETURN;
    END IF;
    PERFORM 1
      FROM public.admin_identity_verifications AS v
     WHERE v.identity_id = v_identity
       AND v.purpose = 'revert'
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       AND v.expires_at > v_now;
    IF FOUND THEN
        RETURN;
    END IF;
    -- In each of the person's businesses -- active or disabled -- no OTHER row holds the new address
    -- (admin_users_tenant_email_key, 00006): the mirror could not be written there.
    FOR m IN
        SELECT a.id, a.tenant_id FROM public.admin_users AS a
         WHERE a.identity_id = v_identity
           AND a.unlinked_at IS NULL
         ORDER BY a.tenant_id
    LOOP
        PERFORM 1
          FROM public.admin_users AS o
         WHERE o.tenant_id = m.tenant_id
           AND o.email OPERATOR(public.=) v_new
           AND o.id <> m.id;
        IF FOUND THEN
            RETURN;
        END IF;
    END LOOP;

    -- THE CHANGE. The new mailbox is proved (the link); the window does NOT open -- the control did not
    -- change hands (password + session + the new mailbox).
    UPDATE public.admin_identities AS i
       SET email = v_new, email_verified_at = pg_catalog.now()
     WHERE i.id = v_identity;
    FOR m IN
        SELECT a.id, a.tenant_id FROM public.admin_users AS a
         WHERE a.identity_id = v_identity
           AND a.unlinked_at IS NULL
         ORDER BY a.tenant_id
    LOOP
        -- The mirror (K-14), pinned to the loop record's id, tenant and identity.
        UPDATE public.admin_users AS a SET email = v_new
         WHERE a.id = m.id AND a.tenant_id = m.tenant_id AND a.identity_id = v_identity;
        -- K-E6 (a): the confirming session stays, every other one ends (identity_set_password's rule).
        UPDATE public.admin_sessions AS s SET revoked_at = pg_catalog.now()
         WHERE s.tenant_id = m.tenant_id AND s.admin_user_id = m.id
           AND s.revoked_at IS NULL AND s.id <> v_session;
        GET DIAGNOSTICS v_n = ROW_COUNT;
        IF m.tenant_id = v_tenant THEN
            v_own := v_own + v_n;
        END IF;
        -- The row's live per-row reset links went to the old address (ADR 0044 §5.5).
        UPDATE public.password_resets AS r SET cancelled_at = pg_catalog.now()
         WHERE r.tenant_id = m.tenant_id AND r.admin_user_id = m.id
           AND r.used_at IS NULL AND r.cancelled_at IS NULL AND r.expires_at > v_now;
        -- The business's trail: outcome, and the count only when not zero (a 0 would say "per identity").
        v_detail := pg_catalog.jsonb_build_object('outcome', 'ok');
        IF v_n > 0 THEN
            v_detail := v_detail || pg_catalog.jsonb_build_object('revoked_other_sessions', v_n);
        END IF;
        INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
        VALUES (m.tenant_id, m.id, 'admin.email.changed', m.id::text, v_detail);
    END LOOP;

    -- The identity's live reset links went to the old address: one secret writes the identity (§5.5).
    UPDATE public.admin_identity_resets AS r SET cancelled_at = pg_catalog.now()
     WHERE r.identity_id = v_identity
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL;
    -- The link is spent; any other live change link retired ('verify' / 'link' need two secrets: untouched).
    UPDATE public.admin_identity_verifications AS v SET used_at = pg_catalog.now()
     WHERE v.id = v_link
       AND v.identity_id = v_identity
       AND v.used_at IS NULL;
    UPDATE public.admin_identity_verifications AS v SET cancelled_at = pg_catalog.now()
     WHERE v.identity_id = v_identity
       AND v.purpose = 'change'
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL;
    -- K-E2 (a), K-E3: the old address's undo link. K-E9 (d) refused the request while another lived, and
    -- the re-check above refused this call, so this is the identity's one live undo link.
    INSERT INTO public.admin_identity_verifications AS v (identity_id, token_hash, expires_at, purpose, address)
    VALUES (v_identity, p_revert_hash, pg_catalog.now() + interval '7 days', 'revert', v_old)
    RETURNING v.expires_at INTO v_revert;
    INSERT INTO public.admin_identity_events (identity_id, event) VALUES (v_identity, 'email_changed');

    RETURN QUERY SELECT v_old::text, v_new::text, v_own, v_revert;
EXCEPTION WHEN unique_violation THEN
    -- The same address written meanwhile by a writer that did not take its lock: nothing of this call
    -- stays, the link is not spent, the answer is the one refusal (no 500, no address in a DETAIL).
    RETURN;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_change_email(text, text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_change_email(text, text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_change_email(text, text, text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 3. identity_revert_email_change
-- ---------------------------------------------------------------------------
-- 0 rows, nothing written: the link is unknown, cancelled, spent, expired or not a 'revert' link; the old
-- address fails 6b or another identity holds it now; one of the person's businesses holds another row of
-- the old address (the row the person refused there keeps it -- ADR 0044 §8 risk 8); a concurrent writer
-- of it committed. Otherwise ONE row: the address given back (the reset link's recipient, K-E11) and how
-- many sessions ended, in every business together.
-- +goose StatementBegin
CREATE FUNCTION public.identity_revert_email_change(p_revert_hash text)
    RETURNS TABLE (restored_email text, revoked_sessions integer)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_link     uuid;
    v_identity uuid;
    v_old      public.citext;
    v_expires  timestamptz;
    v_found    boolean;
    v_now      timestamptz;
    v_sentinel text;
    v_n        integer;
    v_all      integer := 0;
    v_detail   jsonb;
    m          record;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    SELECT v.id, v.identity_id, v.address INTO v_link, v_identity, v_old
      FROM public.admin_identity_verifications AS v
     WHERE v.token_hash = p_revert_hash
       AND v.purpose = 'revert'
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       AND v.expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    -- 6b. THE ADDRESS RULE (M14-08a, 00076's predicate) on the address the identity takes back.
    IF (pg_catalog.octet_length(v_old::text) <= 254
        AND pg_catalog.strpos(v_old::text, '=?') = 0
        AND v_old::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$') IS NOT TRUE THEN
        RETURN;
    END IF;

    -- Lock order (the header): the old address, the identity's reset lock, the identity, its
    -- memberships, the link.
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-email:' || pg_catalog.lower(v_old::text), 0));
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-reset:' || v_identity::text, 0));
    PERFORM 1 FROM public.admin_identities AS i WHERE i.id = v_identity FOR UPDATE;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    SELECT v.expires_at INTO v_expires
      FROM public.admin_identity_verifications AS v
     WHERE v.id = v_link
       AND v.identity_id = v_identity
       AND v.purpose = 'revert'
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       FOR UPDATE;
    v_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- No OTHER identity holds the old address now (the fourth class: the address the link's row gave).
    PERFORM 1 FROM public.admin_identities AS o WHERE o.email OPERATOR(public.=) v_old AND o.id <> v_identity;
    IF FOUND THEN
        RETURN;
    END IF;
    -- In each of the person's businesses no OTHER row holds the old address (YÜKSEK-1: a row the person
    -- refused there keeps it, 00072; the mirror could not go back -- admin_users_tenant_email_key).
    FOR m IN
        SELECT a.id, a.tenant_id FROM public.admin_users AS a
         WHERE a.identity_id = v_identity
           AND a.unlinked_at IS NULL
         ORDER BY a.tenant_id
    LOOP
        PERFORM 1
          FROM public.admin_users AS o
         WHERE o.tenant_id = m.tenant_id
           AND o.email OPERATOR(public.=) v_old
           AND o.id <> m.id;
        IF FOUND THEN
            RETURN;
        END IF;
    END LOOP;

    -- THE UNDO. The password becomes the SENTINEL -- identity_unlink_backfilled's (00079): the bcrypt of
    -- 32 random bytes that exist only inside this expression (S20), made here and never taken from the
    -- caller (a digest argument could fail 00069's bcrypt CHECK, whose DETAIL prints the address). The
    -- window closes in the same statement (00069's CHECK keeps its three columns together): one an
    -- attacker opened with a reset at the new address ends here, and K-E11's reset opens the person's.
    v_sentinel := public.crypt(pg_catalog.encode(public.gen_random_bytes(32), 'hex'), public.gen_salt('bf', 12));
    UPDATE public.admin_identities AS i
       SET email = v_old, email_verified_at = pg_catalog.now(), password_hash = v_sentinel,
           confirm_opened_at = NULL, confirm_open_until = NULL, confirm_scope = NULL
     WHERE i.id = v_identity;
    FOR m IN
        SELECT a.id, a.tenant_id FROM public.admin_users AS a
         WHERE a.identity_id = v_identity
           AND a.unlinked_at IS NULL
         ORDER BY a.tenant_id
    LOOP
        -- The mirror and the digest's copy (the dual-write until contract B -- ADR 0031 §5.1).
        UPDATE public.admin_users AS a SET email = v_old, password_hash = v_sentinel
         WHERE a.id = m.id AND a.tenant_id = m.tenant_id AND a.identity_id = v_identity;
        -- EVERY session ends (a takeover is assumed -- ADR 0044 §5.6).
        UPDATE public.admin_sessions AS s SET revoked_at = pg_catalog.now()
         WHERE s.tenant_id = m.tenant_id AND s.admin_user_id = m.id AND s.revoked_at IS NULL;
        GET DIAGNOSTICS v_n = ROW_COUNT;
        v_all := v_all + v_n;
        UPDATE public.password_resets AS r SET cancelled_at = pg_catalog.now()
         WHERE r.tenant_id = m.tenant_id AND r.admin_user_id = m.id
           AND r.used_at IS NULL AND r.cancelled_at IS NULL AND r.expires_at > v_now;
        v_detail := pg_catalog.jsonb_build_object('outcome', 'ok');
        IF v_n > 0 THEN
            v_detail := v_detail || pg_catalog.jsonb_build_object('revoked_sessions', v_n);
        END IF;
        INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
        VALUES (m.tenant_id, m.id, 'admin.email.reverted', m.id::text, v_detail);
    END LOOP;

    -- Every live link the address change left: the identity's resets, its change links, the link itself
    -- (spent) and any other undo link.
    UPDATE public.admin_identity_resets AS r SET cancelled_at = pg_catalog.now()
     WHERE r.identity_id = v_identity
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL;
    UPDATE public.admin_identity_verifications AS v SET used_at = pg_catalog.now()
     WHERE v.id = v_link
       AND v.identity_id = v_identity
       AND v.used_at IS NULL;
    UPDATE public.admin_identity_verifications AS v SET cancelled_at = pg_catalog.now()
     WHERE v.identity_id = v_identity
       AND v.purpose = 'change'
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL;
    UPDATE public.admin_identity_verifications AS v SET cancelled_at = pg_catalog.now()
     WHERE v.identity_id = v_identity
       AND v.purpose = 'revert'
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL;
    INSERT INTO public.admin_identity_events (identity_id, event) VALUES (v_identity, 'email_reverted');

    RETURN QUERY SELECT v_old::text, v_all;
EXCEPTION WHEN unique_violation THEN
    -- The old address written meanwhile by a writer that did not take its lock: nothing stays (no 500,
    -- no address in a DETAIL).
    RETURN;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_revert_email_change(text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_revert_email_change(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_revert_email_change(text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 4. identity_open_reset: the recipient (S-1)
-- ---------------------------------------------------------------------------
-- 00072's body byte for byte but for the fourth argument, one DECLARE line and the recipient block after
-- the existence test. DROP + CREATE (the signature changes); T5 restated. The binary before this file
-- calls it with three arguments and gets 00072's behaviour through the DEFAULT -- the rollout's named
-- cost (ADR 0044 §8 risk 9); T295 takes the DEFAULT away in the next deploy.
DROP FUNCTION public.identity_open_reset(uuid, text, timestamptz);
-- +goose StatementBegin
CREATE FUNCTION public.identity_open_reset(p_identity_id uuid, p_token_hash text, p_expires_at timestamptz, p_recipient text DEFAULT NULL)
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
    v_now     timestamptz;
    v_address public.citext; -- 00102 (T173, S-1): the identity's address under the lock
BEGIN
    -- The per-identity lock: a namespaced transaction advisory lock (passwordresets.sql's
    -- LockAdminForResetIssue pattern; the 'admin-identity-reset:' prefix keeps it apart from the tap
    -- debounce's key space and from 'admin-identity-email:'). N concurrent requests for one identity
    -- queue here; each reads the link the one before it committed.
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-reset:' || p_identity_id::text, 0));
    PERFORM 1 FROM public.admin_identities AS i WHERE i.id = p_identity_id;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    -- 00102 (T173, ADR 0044 §5.3, S-1): THE RECIPIENT. The caller resolved the address the link goes to
    -- BEFORE this call; an e-mail change (or its undo) committed in between moved the identity off it, and
    -- a link mailed there would hand the identity to the mailbox it just left. Read under this lock --
    -- identity_change_email and identity_revert_email_change take it too, before the identity's row -- the
    -- address is the one the link goes to or nothing is written (byte for byte: the resolver's row is the
    -- stored spelling). NULL: the binary before 00102, three arguments, the DEFAULT -- today's behaviour
    -- for the rollout's minutes (ADR 0044 §6, T295).
    IF p_recipient IS NOT NULL THEN
        SELECT i.email INTO v_address FROM public.admin_identities AS i WHERE i.id = p_identity_id;
        IF (v_address::text = p_recipient) IS NOT TRUE THEN
            RETURN;
        END IF;
    END IF;

    v_now := pg_catalog.clock_timestamp();
    SELECT r.id, r.expires_at INTO v_id, v_expires
      FROM public.admin_identity_resets AS r
     WHERE r.identity_id = p_identity_id
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL
       AND r.expires_at > v_now
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
ALTER FUNCTION public.identity_open_reset(uuid, text, timestamptz, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_open_reset(uuid, text, timestamptz, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_open_reset(uuid, text, timestamptz, text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 5. identity_unlink_backfilled: no unlink while an undo link lives (K-E16 (a))
-- ---------------------------------------------------------------------------
-- 00079's body byte for byte but for one block after the window test; the same signature (CREATE OR
-- REPLACE; T5 restated).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_unlink_backfilled(p_session_hash text, p_tenant uuid)
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
    v_via        text;
    v_linked_at  timestamptz;
    v_sentinel   text;
    v_constraint text;
    v_n          integer;
    v_session    uuid;
    v_tenant     uuid;
    v_now        timestamptz;
    v_fresh      timestamptz;
    v_auth       timestamptz;
    v_auth_found boolean;
    v_until      timestamptz;
    v_verified   timestamptz;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    v_fresh := v_now - interval '5 minutes';
    SELECT s.id, s.tenant_id, a.identity_id INTO v_session, v_tenant, v_identity
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.revoked_at IS NULL
       AND s.authenticated_at > v_fresh
       AND s.authenticated_at <= v_now
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT i.confirm_scope, i.confirm_opened_at, i.confirm_open_until, i.email_verified_at
      INTO v_scope, v_opened, v_until, v_verified
      FROM public.admin_identities AS i
     WHERE i.id = v_identity
       FOR UPDATE;
    -- Lock order: the identity (above), every membership of it, then the session and token rows.
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    -- Re-checked under the identity lock (READ COMMITTED: the lock-free read above may predate a
    -- reset or a revocation that committed while this call waited); its freshness is judged AFTER the
    -- row lock, with the clock read again -- a lock wait must not leave a stale v_now.
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
    v_auth_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_auth_found AND v_auth > v_now - interval '5 minutes' AND v_auth <= v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- The window too, after the locks (NULL fails closed).
    IF (v_verified IS NOT NULL AND v_until > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- 00102 (T173, ADR 0044 §5.6, K-E16 (a)): NO UNLINK WHILE AN UNDO LINK LIVES. Whoever changed the
    -- address can reset the password at the new one, which opens a reset window; unlinking then would
    -- outlive the undo (an unlinked row keeps the new address and cannot be relinked from the old one),
    -- so the person's memberships stay linked until the undo link is spent, cancelled or expired. "Live"
    -- is judged with v_now as read again AFTER the locks above (a wait must not leave a stale clock).
    PERFORM 1 FROM public.admin_identity_verifications AS v
     WHERE v.identity_id = v_identity
       AND v.purpose = 'revert'
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       AND v.expires_at > v_now;
    IF FOUND THEN
        RETURN;
    END IF;

    -- The membership in p_tenant (UNIQUE (tenant_id, identity_id): at most one); the window's scope
    -- decides, in plpgsql, whether it is on the list.
    SELECT a.id, a.linked_via, a.linked_at INTO v_target, v_via, v_linked_at
      FROM public.admin_users AS a
     WHERE a.tenant_id = p_tenant
       AND a.identity_id = v_identity
       AND a.unlinked_at IS NULL
       FOR UPDATE;
    -- Fails closed on NULL (a NULL linked_via or linked_at is on no list): `IS NOT TRUE`, never `NOT`.
    IF NOT FOUND
       OR ((v_scope = 'first_verify' AND v_via IN ('backfill', 'reconcile', 'signup', 'account'))
           OR (v_scope = 'reset' AND v_linked_at < v_opened)) IS NOT TRUE THEN
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
        UPDATE public.admin_users AS a
           SET identity_id = NULL, unlinked_at = now(), password_hash = v_sentinel
         WHERE a.id = v_target AND a.tenant_id = p_tenant AND a.identity_id = v_identity;
    EXCEPTION WHEN check_violation THEN
        GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME;
        -- Only a CHECK this file does not know (contract B's) takes the disable branch; no name (GET
        -- STACKED DIAGNOSTICS gives '' for a bare RAISE, NULL never) re-raises.
        IF coalesce(v_constraint, '') = '' OR v_constraint IN ('admin_users_link_has_a_time', 'admin_users_linked_via_known',
                            'admin_users_password_hash_is_bcrypt', 'admin_users_role_check',
                            'admin_users_status_check') THEN
            RAISE;
        END IF;
        UPDATE public.admin_users AS a
           SET unlinked_at = now(), status = 'disabled'
         WHERE a.id = v_target AND a.tenant_id = p_tenant AND a.identity_id = v_identity;
    END;

    UPDATE public.password_resets AS r SET cancelled_at = now()
     WHERE r.tenant_id = p_tenant AND r.admin_user_id = v_target
       AND r.used_at IS NULL AND r.cancelled_at IS NULL AND r.expires_at > v_now;
    UPDATE public.admin_sessions AS s SET revoked_at = now()
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
ALTER FUNCTION public.identity_unlink_backfilled(text, uuid) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_unlink_backfilled(text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_unlink_backfilled(text, uuid) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 6. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                WHERE p.oid IN ('public.identity_request_email_change(text,text,text,citext)'::pg_catalog.regprocedure,
                                'public.identity_change_email(text,text,text)'::pg_catalog.regprocedure,
                                'public.identity_revert_email_change(text)'::pg_catalog.regprocedure,
                                'public.identity_open_reset(uuid,text,timestamptz,text)'::pg_catalog.regprocedure,
                                'public.identity_unlink_backfilled(text,uuid)'::pg_catalog.regprocedure)
                  AND (NOT p.prosecdef
                       OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
                       OR p.proowner <> 'tappa_identitydefiner'::pg_catalog.regrole
                       OR NOT pg_catalog.has_function_privilege('tappa_app', p.oid, 'EXECUTE')
                       OR EXISTS (SELECT 1 FROM pg_catalog.aclexplode(p.proacl) x
                                   WHERE x.privilege_type = 'EXECUTE'
                                     AND x.grantee NOT IN (p.proowner, 'tappa_app'::pg_catalog.regrole))))
       OR (SELECT count(*) FROM pg_catalog.pg_proc p
            WHERE p.oid IN ('public.identity_request_email_change(text,text,text,citext)'::pg_catalog.regprocedure,
                            'public.identity_change_email(text,text,text)'::pg_catalog.regprocedure,
                            'public.identity_revert_email_change(text)'::pg_catalog.regprocedure)
              AND pg_catalog.strpos(p.prosrc, '-- 6b. THE ADDRESS RULE (M14-08a') > 0) <> 3
       OR pg_catalog.to_regprocedure('public.identity_open_reset(uuid,text,timestamptz)') IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00102 postcondition: an e-mail change function or a redefinition is not the ADR''s shape (ADR 0044 5.3, ADR 0031 5.2). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- 00101's refusal, the same block (ADR 0044 §6): while an undo link lives or an address event exists,
-- nothing here is taken back -- the undo and K-E16's guard stay together. The two tables are held so
-- that neither can be written between the check and the end of this file.
LOCK TABLE admin_identity_verifications, admin_identity_events IN SHARE MODE;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_identity_verifications AS v
                WHERE v.purpose = 'revert' AND v.used_at IS NULL AND v.cancelled_at IS NULL
                  AND v.expires_at > pg_catalog.clock_timestamp())
       OR EXISTS (SELECT 1 FROM public.admin_identity_events AS e
                   WHERE e.event IN ('email_change_requested', 'email_changed', 'email_reverted')) THEN
        RAISE EXCEPTION 'migration 00102 Down: an e-mail change can still be undone, or admin_identity_events holds an e-mail change event (it ages out after 90 days); the way back is the previous binary, which runs on this schema (ADR 0044 6). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- 00079's identity_unlink_backfilled, byte for byte, its properties restated.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.identity_unlink_backfilled(p_session_hash text, p_tenant uuid)
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
    v_via        text;
    v_linked_at  timestamptz;
    v_sentinel   text;
    v_constraint text;
    v_n          integer;
    v_session    uuid;
    v_tenant     uuid;
    v_now        timestamptz;
    v_fresh      timestamptz;
    v_auth       timestamptz;
    v_auth_found boolean;
    v_until      timestamptz;
    v_verified   timestamptz;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    v_fresh := v_now - interval '5 minutes';
    SELECT s.id, s.tenant_id, a.identity_id INTO v_session, v_tenant, v_identity
      FROM public.admin_sessions AS s
      JOIN public.admin_users AS a ON a.id = s.admin_user_id AND a.tenant_id = s.tenant_id
     WHERE s.token_hash = p_session_hash
       AND s.revoked_at IS NULL
       AND s.authenticated_at > v_fresh
       AND s.authenticated_at <= v_now
       AND a.status = 'active'
       AND a.unlinked_at IS NULL
       AND a.identity_id IS NOT NULL;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    SELECT i.confirm_scope, i.confirm_opened_at, i.confirm_open_until, i.email_verified_at
      INTO v_scope, v_opened, v_until, v_verified
      FROM public.admin_identities AS i
     WHERE i.id = v_identity
       FOR UPDATE;
    -- Lock order: the identity (above), every membership of it, then the session and token rows.
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    -- Re-checked under the identity lock (READ COMMITTED: the lock-free read above may predate a
    -- reset or a revocation that committed while this call waited); its freshness is judged AFTER the
    -- row lock, with the clock read again -- a lock wait must not leave a stale v_now.
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
    v_auth_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_auth_found AND v_auth > v_now - interval '5 minutes' AND v_auth <= v_now) IS NOT TRUE THEN
        RETURN;
    END IF;
    -- The window too, after the locks (NULL fails closed).
    IF (v_verified IS NOT NULL AND v_until > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;

    -- The membership in p_tenant (UNIQUE (tenant_id, identity_id): at most one); the window's scope
    -- decides, in plpgsql, whether it is on the list.
    SELECT a.id, a.linked_via, a.linked_at INTO v_target, v_via, v_linked_at
      FROM public.admin_users AS a
     WHERE a.tenant_id = p_tenant
       AND a.identity_id = v_identity
       AND a.unlinked_at IS NULL
       FOR UPDATE;
    -- Fails closed on NULL (a NULL linked_via or linked_at is on no list): `IS NOT TRUE`, never `NOT`.
    IF NOT FOUND
       OR ((v_scope = 'first_verify' AND v_via IN ('backfill', 'reconcile', 'signup', 'account'))
           OR (v_scope = 'reset' AND v_linked_at < v_opened)) IS NOT TRUE THEN
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
        UPDATE public.admin_users AS a
           SET identity_id = NULL, unlinked_at = now(), password_hash = v_sentinel
         WHERE a.id = v_target AND a.tenant_id = p_tenant AND a.identity_id = v_identity;
    EXCEPTION WHEN check_violation THEN
        GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME;
        -- Only a CHECK this file does not know (contract B's) takes the disable branch; no name (GET
        -- STACKED DIAGNOSTICS gives '' for a bare RAISE, NULL never) re-raises.
        IF coalesce(v_constraint, '') = '' OR v_constraint IN ('admin_users_link_has_a_time', 'admin_users_linked_via_known',
                            'admin_users_password_hash_is_bcrypt', 'admin_users_role_check',
                            'admin_users_status_check') THEN
            RAISE;
        END IF;
        UPDATE public.admin_users AS a
           SET unlinked_at = now(), status = 'disabled'
         WHERE a.id = v_target AND a.tenant_id = p_tenant AND a.identity_id = v_identity;
    END;

    UPDATE public.password_resets AS r SET cancelled_at = now()
     WHERE r.tenant_id = p_tenant AND r.admin_user_id = v_target
       AND r.used_at IS NULL AND r.cancelled_at IS NULL AND r.expires_at > v_now;
    UPDATE public.admin_sessions AS s SET revoked_at = now()
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
ALTER FUNCTION public.identity_unlink_backfilled(text, uuid) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_unlink_backfilled(text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_unlink_backfilled(text, uuid) TO tappa_app;

-- 00072's identity_open_reset, byte for byte, its owner and grants restated.
DROP FUNCTION public.identity_open_reset(uuid, text, timestamptz, text);
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
    v_now     timestamptz;
BEGIN
    -- The per-identity lock: a namespaced transaction advisory lock (passwordresets.sql's
    -- LockAdminForResetIssue pattern; the 'admin-identity-reset:' prefix keeps it apart from the tap
    -- debounce's key space and from 'admin-identity-email:'). N concurrent requests for one identity
    -- queue here; each reads the link the one before it committed.
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-reset:' || p_identity_id::text, 0));
    PERFORM 1 FROM public.admin_identities AS i WHERE i.id = p_identity_id;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    v_now := pg_catalog.clock_timestamp();
    SELECT r.id, r.expires_at INTO v_id, v_expires
      FROM public.admin_identity_resets AS r
     WHERE r.identity_id = p_identity_id
       AND r.used_at IS NULL
       AND r.cancelled_at IS NULL
       AND r.expires_at > v_now
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
ALTER FUNCTION public.identity_open_reset(uuid, text, timestamptz) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_open_reset(uuid, text, timestamptz) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_open_reset(uuid, text, timestamptz) TO tappa_app;

DROP FUNCTION public.identity_revert_email_change(text);
DROP FUNCTION public.identity_change_email(text, text, text);
DROP FUNCTION public.identity_request_email_change(text, text, text, citext);
