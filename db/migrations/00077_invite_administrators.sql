-- 00077 -- M14-15: an owner invites a manager by e-mail (M7-07 in the identity model). One
-- tenant-scoped table, admin_invites (the RLS five + column grants: the owner's panel lists, issues
-- and cancels its own business's invitations), and three functions of ADR 0031 §5.2's closed list:
-- the md.7 resolver resolve_admin_invite_by_token_hash (owner tappa_resolver) and the two writers
-- identity_accept_invite / identity_accept_invite_new (owner tappa_identitydefiner).
--
-- NORMATIVE SOURCE: docs/adr/0031-tek-kimlik-cok-isletme.md §5.1 (admin_invites, tenant-scoped,
-- composite FK to admin_users), §5.2 (the resolver's row, the two writers' rows, the definer's verb x
-- column matrix: admin_invites SELECT + UPDATE (used_at), admin_users INSERT (tenant_id, full_name,
-- email, password_hash, role, identity_id, linked_via, linked_at)), §5.11 (the invitation: owner-only,
-- single use, expiring, hashed, mailed to the invitee, never shown on the panel; acceptance by POST
-- with the two functions; no membership without consent; an existing identity's click is a mailbox
-- proof; the owner does not learn whether the address has an identity -- R18; a refused row of the
-- address is RELINKED, ONAY note 3), §6 (the third §4.5 exception); the card is
-- docs/plan/m14-tek-kimlik.md -> M14-15.
--
-- admin_invites IS TENANT-SCOPED (CLAUDE.md §6): tenant_id NOT NULL, ENABLE + FORCE row level
-- security, the policy on tenant_id (NULLIF form, ADR 0002 / Q27), indexes leading with tenant_id,
-- and tappa_app's reach given column by column after an explicit REVOKE ALL (the dev and production
-- default ACL hands a new table `tappa_app=arwd` -- measured, 00069's header). tappa_app SELECTs every
-- column but token_hash (a lookup by token is the resolver's, never the panel's), INSERTs the
-- invitation (created_at, used_at, cancelled_at are not its to write) and UPDATEs cancelled_at alone:
-- the owner lists and cancels (ADR 0031 §5.1). No DELETE -- an invitation is a trail, not a draft.
--
-- full_name: the name the owner types for the manager, the new membership row's full_name
-- (admin_users.full_name is NOT NULL, 00006). The ADR's column list for admin_invites is a summary
-- ("özet"); the matrix gives the definer SELECT on the table without a column list, and the definer
-- INSERTs admin_users.full_name -- a column the matrix names.
-- role: 'manager' only. M7-07's invitation makes a SECOND administrator, a manager; ownership moves
-- by M14-16's two-step transfer, never by an e-mail link.
-- One open invitation per address per business (a partial UNIQUE): the panel cancels the open one
-- before it issues another, so a business never holds two live links for one mailbox.
--
-- WHO IS FOUND HOW (ADR 0031 §5.2). Both writers are keyed by the INVITATION TOKEN's hash (the
-- secret mailed to the invitee); identity_accept_invite also by a live SESSION's hash, through which
-- -- and nothing else -- it finds the identity. No body resolves an identity from an e-mail address:
-- identity_accept_invite compares the session's identity's address with the invitation's (the
-- forwarded-link rule, bulgu 12) and refuses on a mismatch; identity_accept_invite_new births an
-- identity only for an address that has NONE (ON CONFLICT (email) DO NOTHING under the address's
-- namespaced lock -- the same expression identity_create_for_signup takes, S13: a concurrent
-- acceptance and registration of one new address end with one identity and no 500).
--
-- THE TWO OUTCOMES OF AN ADDRESS THAT IS ALREADY IN THE BUSINESS (ONAY note 3). 00006's partial
-- UNIQUE (tenant_id, email) keeps a REFUSED row's address (unlinked_at stamped by
-- identity_unlink_backfilled), so "a new row by invitation" cannot exist there: the acceptance
-- RELINKS that row (identity_id, linked_via 'invite', a new linked_at, unlinked_at NULL, the digest
-- copy until contract B, status 'active' -- the owner's invitation is the intent to have the person
-- back; §5.11 names 'active' after contract B, and before it a refused row is 'active' unless an owner
-- disabled it, which this owner's fresh invitation overrides). Any OTHER row of the address in the
-- business (active or disabled, linked or not) refuses the acceptance with 0 rows and leaves the
-- invitation open: the answer goes to the person who clicked -- the mailbox owner -- and the inviting
-- owner sees the same open invitation as for an ignored e-mail. The issuing path never reads a
-- membership or an identity (internal/adminauth's AdminInvites), so the owner's answer, its timing and
-- the list's state are one shape whatever the address is (R18).
--
-- THE MAILBOX PROOF (§5.11, K-7's orchestrator note). identity_accept_invite is clicked with a live
-- session of the identity whose address the invitation names: when the identity's email_verified_at
-- is empty it is filled and a first_verify window opens (identity_verify_email's shape, its event row
-- included); when it is filled nothing about the identity changes. identity_accept_invite_new's
-- identity is born from the click: email_verified_at = now(), no window (it has no other membership).
-- NEITHER GIVES A SESSION (ADR 0015 §1's pattern): the person signs in afterwards.
--
-- THE BELT (ADR 0031 §5.2, identitybelt_test.go): every body is written in the subset M14-04's
-- scanner models -- `public.<name> AS <alias>`, WHERE / ON as conjunctions of `X = Y` and IS [NOT]
-- NULL, every tenant-table reference anchored by its tenant (a trusted variable), by its own token
-- (admin_sessions.token_hash, and from this file admin_invites.token_hash -- UNIQUE) or bounded by an
-- identity; anything else decided in plpgsql with IS NOT TRUE so a NULL fails closed. LOCK ORDER as
-- 00072's: the identity (row lock -- or, for an address with no identity yet, its namespaced advisory
-- lock), then membership rows, then the token row (the invitation), then the session; every expiry
-- and freshness compared with clock_timestamp() read AFTER the locks.
--
-- AUDIT (ADR 0031 §5.2): the acceptance writes the invitation's business ONE row,
-- admin.invite.accepted, actor and target the membership row, detail {outcome, invite_id} -- the same
-- shape for both functions, so the row does not say whether the person had an identity, how many
-- businesses they hold or which (no identity_id, no count, no other tenant).
--
-- EXPAND (ADR 0031 §8): a new table, new functions, new grants; nothing the binary before this file
-- calls changes. THE DOWN refuses while an invitation exists (it would drop the business's trail);
-- the memberships an invitation made STAY (they are memberships like any other, linked_via 'invite').

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.admin_identities') IS NULL
       OR pg_catalog.to_regprocedure('public.identity_create_for_signup(uuid,uuid)') IS NULL
       OR (SELECT pg_catalog.pg_get_function_result(p.oid) FROM pg_catalog.pg_proc p
            WHERE p.oid = 'public.identity_consume_reset(text,text)'::pg_catalog.regprocedure)
          <> 'TABLE(identity_id uuid, revoked_sessions integer, email text)' THEN
        RAISE EXCEPTION 'migration 00077 (M14-15) needs 00069-00075 (the identity tables and functions). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regclass('public.admin_invites') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                   WHERE p.pronamespace = 'public'::pg_catalog.regnamespace
                     AND p.proname IN ('resolve_admin_invite_by_token_hash', 'identity_accept_invite', 'identity_accept_invite_new')) THEN
        RAISE EXCEPTION 'migration 00077 (M14-15) creates admin_invites and its three functions, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- The column grants below cannot narrow a table-level privilege some other hand gave.
    IF pg_catalog.has_table_privilege('tappa_identitydefiner', 'public.admin_users', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00077 (M14-15): tappa_identitydefiner holds a table-level INSERT on admin_users; its column grant could not narrow it. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. admin_invites (ADR 0031 §5.1)
-- ---------------------------------------------------------------------------
-- token_hash: the 64-hex HMAC of the token (internal/adminauth; the token itself is never stored),
--   UNIQUE -- the resolver's and the writers' key.
-- expires_at: the application's TTL; the CHECK is the ceiling (7 days), like 00019's for resets.
-- used_at / cancelled_at: never both (an accepted invitation cannot be cancelled and vice versa).
-- invited_by: the owner's membership row, composite FK (same tenant, structurally -- 00006's
--   admin_users_id_tenant_key).
-- The CREATE TABLE's two foreign keys take SHARE ROW EXCLUSIVE on tenants and admin_users (hot:
-- TouchAdminSession) -- under this file's 1 s lock_timeout, which gives up rather than queue the panel.
SET LOCAL lock_timeout = '1s';
CREATE TABLE admin_invites (
    id           uuid        NOT NULL DEFAULT gen_random_uuid(),
    tenant_id    uuid        NOT NULL,
    email        citext      NOT NULL,
    full_name    text        NOT NULL,
    role         text        NOT NULL,
    token_hash   text        NOT NULL,
    invited_by   uuid        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    used_at      timestamptz,
    cancelled_at timestamptz,
    CONSTRAINT admin_invites_pkey PRIMARY KEY (id),
    CONSTRAINT admin_invites_token_hash_key UNIQUE (token_hash),
    CONSTRAINT admin_invites_token_hash_shape CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT admin_invites_role_manager CHECK (role = 'manager'),
    CONSTRAINT admin_invites_email_filled CHECK (pg_catalog.btrim(email::text) <> ''),
    -- THE ASCII ADDRESS RULE (the user's decision of 2026-10-26: "the stored address is an address we
    -- can send to"): printable ASCII, no space -- the byte rule of internal/mail's ValidRecipient, which
    -- the panel applies before it writes. citext folds 'İ' (U+0130) and the Kelvin sign (U+212A) onto
    -- ASCII letters (measured, M14-08a), so a non-ASCII address could stand for a victim's address.
    CONSTRAINT admin_invites_email_ascii CHECK (email::text ~ '^[\x21-\x7e]+$'),
    CONSTRAINT admin_invites_full_name_filled
        CHECK (pg_catalog.btrim(full_name) <> '' AND pg_catalog.char_length(full_name) <= 120),
    CONSTRAINT admin_invites_ttl_ceiling
        CHECK (expires_at > created_at AND expires_at <= created_at + interval '7 days'),
    CONSTRAINT admin_invites_used_or_cancelled CHECK (used_at IS NULL OR cancelled_at IS NULL),
    CONSTRAINT admin_invites_tenant_fk FOREIGN KEY (tenant_id)
        REFERENCES tenants (id) ON DELETE RESTRICT,
    CONSTRAINT admin_invites_inviter_fk FOREIGN KEY (invited_by, tenant_id)
        REFERENCES admin_users (id, tenant_id) ON DELETE RESTRICT
);

-- tenant_id leading (CLAUDE.md §6): the panel's list (newest first) and RLS.
CREATE INDEX admin_invites_tenant_idx ON admin_invites (tenant_id, created_at);
-- One open invitation per address per business.
CREATE UNIQUE INDEX admin_invites_one_open_per_address
    ON admin_invites (tenant_id, email) WHERE used_at IS NULL AND cancelled_at IS NULL;

ALTER TABLE admin_invites ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_invites FORCE ROW LEVEL SECURITY;

CREATE POLICY admin_invites_tenant_isolation ON admin_invites
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- NOT a no-op: the default ACL has just handed tappa_app `arwd`.
REVOKE ALL ON admin_invites FROM tappa_app;
GRANT SELECT (id, tenant_id, email, full_name, role, invited_by, created_at, expires_at, used_at, cancelled_at),
      INSERT (tenant_id, email, full_name, role, token_hash, invited_by, expires_at),
      UPDATE (cancelled_at)
    ON admin_invites TO tappa_app;

-- ---------------------------------------------------------------------------
-- 2. Column grants (ADR 0031 §5.2's matrix -- what THIS file's functions need)
-- ---------------------------------------------------------------------------
-- tappa_identitydefiner: the invitation it reads and spends (admin_invites: SELECT, UPDATE (used_at)),
-- and the membership row an acceptance births (admin_users INSERT -- the matrix's eight columns). The
-- relink of a refused row uses the UPDATE columns 00072 granted; FOR UPDATE on admin_invites needs
-- the UPDATE grant it has.
GRANT SELECT (id, tenant_id, email, full_name, role, token_hash, expires_at, used_at, cancelled_at),
      UPDATE (used_at)
    ON admin_invites TO tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
GRANT INSERT (tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
    ON admin_users TO tappa_identitydefiner;
-- tappa_resolver (T6): exactly what resolve_admin_invite_by_token_hash returns and filters on.
GRANT SELECT (id, tenant_id, role, token_hash, expires_at, used_at, cancelled_at)
    ON admin_invites TO tappa_resolver;

-- ---------------------------------------------------------------------------
-- 3. resolve_admin_invite_by_token_hash (ADR 0002 md.7: read-only, <= 1 row from a UNIQUE)
-- ---------------------------------------------------------------------------
-- The acceptance page's question "is this link still usable" -- it PRODUCES the invitation's tenant
-- rather than running inside one. It returns no address, no name and no inviter.
-- +goose StatementBegin
CREATE FUNCTION public.resolve_admin_invite_by_token_hash(p_token_hash text)
    RETURNS TABLE (
        id           uuid,
        tenant_id    uuid,
        role         text,
        expires_at   timestamptz,
        used_at      timestamptz,
        cancelled_at timestamptz
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    SELECT v.id, v.tenant_id, v.role, v.expires_at, v.used_at, v.cancelled_at
      FROM public.admin_invites AS v
     WHERE v.token_hash = p_token_hash;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_admin_invite_by_token_hash(text) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_admin_invite_by_token_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_admin_invite_by_token_hash(text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 4. identity_accept_invite -- invitation token + a live session of the SAME address's identity
-- ---------------------------------------------------------------------------
-- 0 rows (nothing written) when: the invitation is unknown, spent, cancelled or expired; the session
-- is not live, or its row is not an active, unrefused, LINKED membership; the identity's address is
-- not the invitation's (a forwarded link -- bulgu 12); the business already holds a row of the address
-- that is not a refused one ("address already in this business"); a membership of the identity in that
-- business appeared meanwhile (unique_violation). Otherwise: the membership (a new row, or the refused
-- row relinked), the invitation spent, the mailbox proof, one audit row. No session.
-- +goose StatementBegin
CREATE FUNCTION public.identity_accept_invite(p_invite_hash text, p_session_hash text)
    RETURNS TABLE (admin_user_id uuid, tenant_id uuid)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_invite         uuid;
    v_tenant         uuid;
    v_email          public.citext;
    v_role           text;
    v_name           text;
    v_expires        timestamptz;
    v_found          boolean;
    v_session        uuid;
    v_session_tenant uuid;
    v_identity       uuid;
    v_identity_email public.citext;
    v_digest         text;
    v_verified       timestamptz;
    v_row            uuid;
    v_row_identity   uuid;
    v_row_refused    timestamptz;
    v_row_found      boolean;
    v_admin          uuid;
    v_now            timestamptz;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    -- The invitation, read without a lock, only to learn its business.
    SELECT v.id, v.tenant_id, v.email INTO v_invite, v_tenant, v_email
      FROM public.admin_invites AS v
     WHERE v.token_hash = p_invite_hash
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       AND v.expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;

    -- The session, read without a lock, only to learn its identity (the ONLY way to it).
    SELECT s.id, s.tenant_id, a.identity_id INTO v_session, v_session_tenant, v_identity
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

    -- Lock order: the identity, its memberships, the business's row of the address, the invitation,
    -- then the session.
    SELECT i.email, i.password_hash, i.email_verified_at INTO v_identity_email, v_digest, v_verified
      FROM public.admin_identities AS i
     WHERE i.id = v_identity
       FOR UPDATE;
    -- The forwarded link (bulgu 12): the session's person must be the invitation's address -- and both
    -- are ASCII (the 2026-10-26 address rule: citext folds 'İ' and the Kelvin sign onto ASCII letters, so
    -- a non-ASCII identity address could equal the invited one under OPERATOR(public.=)). NULL fails
    -- closed.
    IF (v_identity_email::text ~ '^[\x21-\x7e]+$'
        AND v_email::text ~ '^[\x21-\x7e]+$'
        AND v_identity_email OPERATOR(public.=) v_email) IS NOT TRUE THEN
        RETURN;
    END IF;
    PERFORM 1 FROM public.admin_users AS a WHERE a.identity_id = v_identity FOR UPDATE;
    SELECT a.id, a.identity_id, a.unlinked_at INTO v_row, v_row_identity, v_row_refused
      FROM public.admin_users AS a
     WHERE a.tenant_id = v_tenant
       AND a.email OPERATOR(public.=) v_email
       FOR UPDATE;
    v_row_found := FOUND;
    SELECT v.expires_at, v.role, v.full_name INTO v_expires, v_role, v_name
      FROM public.admin_invites AS v
     WHERE v.id = v_invite
       AND v.tenant_id = v_tenant
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       FOR UPDATE;
    v_found := FOUND;
    -- Re-checked under the identity lock (READ COMMITTED: a reset or a revocation may have committed
    -- while this call waited).
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
    -- The expiry after the invitation's own lock, with the clock read again (NULL fails closed).
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;

    IF v_row_found THEN
        -- Only a REFUSED row of the address is taken back (ONAY note 3); any other row means the
        -- address is already in this business.
        IF (v_row_identity IS NULL AND v_row_refused IS NOT NULL) IS NOT TRUE THEN
            RETURN;
        END IF;
        UPDATE public.admin_users AS a
           SET identity_id   = v_identity,
               linked_via    = 'invite',
               linked_at     = pg_catalog.now(),
               unlinked_at   = NULL,
               password_hash = v_digest,
               status        = 'active'
         WHERE a.id = v_row
           AND a.tenant_id = v_tenant
           AND a.identity_id IS NULL;
        IF NOT FOUND THEN
            RETURN;
        END IF;
        v_admin := v_row;
    ELSE
        INSERT INTO public.admin_users AS a (tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
        VALUES (v_tenant, v_name, v_identity_email, v_digest, v_role, v_identity, 'invite', pg_catalog.now())
        RETURNING a.id INTO v_admin;
    END IF;

    -- The click with this identity's live session is a mailbox proof (§5.11): the FIRST proof
    -- verifies the address and opens a first_verify window, as identity_verify_email does.
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

    UPDATE public.admin_invites AS v SET used_at = pg_catalog.now()
     WHERE v.id = v_invite
       AND v.tenant_id = v_tenant
       AND v.used_at IS NULL;
    INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
    VALUES (v_tenant, v_admin, 'admin.invite.accepted', v_admin::text,
            pg_catalog.jsonb_build_object('outcome', 'ok', 'invite_id', v_invite::text));

    RETURN QUERY SELECT v_admin, v_tenant;
EXCEPTION WHEN unique_violation THEN
    -- A membership of the identity in that business, or a row of the address, committed meanwhile.
    RETURN;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_accept_invite(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_accept_invite(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_accept_invite(text, text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 5. identity_accept_invite_new -- invitation token + a NEW password, for an address with no identity
-- ---------------------------------------------------------------------------
-- 0 rows (nothing written) when: the invitation is unknown, spent, cancelled or expired; the digest is
-- not the bcrypt shape admin_identities' CHECK demands (checked BEFORE the INSERT: a CHECK violation
-- would put the digest into the error's DETAIL); the address is not printable ASCII (the 2026-10-26
-- address rule); the business already holds a row of the address;
-- an identity already carries the address (ON CONFLICT (email) DO NOTHING -- under the address's
-- lock, so a concurrent registration or acceptance of the same new address waits and then births
-- nothing: S13, no 500). Otherwise: the identity (email_verified_at = the click), its membership, the
-- invitation spent, one audit row. No session.
-- +goose StatementBegin
CREATE FUNCTION public.identity_accept_invite_new(p_invite_hash text, p_new_hash text)
    RETURNS TABLE (admin_user_id uuid, tenant_id uuid)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict use_column
DECLARE
    v_invite   uuid;
    v_tenant   uuid;
    v_email    public.citext;
    v_role     text;
    v_name     text;
    v_expires  timestamptz;
    v_found    boolean;
    v_identity uuid;
    v_admin    uuid;
    v_now      timestamptz;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    SELECT v.id, v.tenant_id, v.email INTO v_invite, v_tenant, v_email
      FROM public.admin_invites AS v
     WHERE v.token_hash = p_invite_hash
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       AND v.expires_at > v_now;
    IF NOT FOUND THEN
        RETURN;
    END IF;
    IF (p_new_hash ~ '^\$2[aby]\$(0[4-9]|1[0-4])\$[./A-Za-z0-9]{53}$') IS NOT TRUE THEN
        RETURN;
    END IF;
    -- THE ASCII ADDRESS RULE (2026-10-26): no identity is ever born for a non-ASCII address -- citext
    -- folds 'İ' (U+0130) and the Kelvin sign (U+212A) onto ASCII letters, so such an identity would squat
    -- the ASCII address it folds to. Checked before the lock and any write; NULL fails closed.
    IF (v_email::text ~ '^[\x21-\x7e]+$') IS NOT TRUE THEN
        RETURN;
    END IF;

    -- The address's lock stands in for the identity's row lock (there is no identity yet); then the
    -- business's row of the address, then the invitation.
    PERFORM pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended('admin-identity-email:' || pg_catalog.lower(v_email::text), 0));
    PERFORM 1
       FROM public.admin_users AS a
      WHERE a.tenant_id = v_tenant
        AND a.email OPERATOR(public.=) v_email
        FOR UPDATE;
    IF FOUND THEN
        RETURN;
    END IF;
    SELECT v.expires_at, v.role, v.full_name INTO v_expires, v_role, v_name
      FROM public.admin_invites AS v
     WHERE v.id = v_invite
       AND v.tenant_id = v_tenant
       AND v.used_at IS NULL
       AND v.cancelled_at IS NULL
       FOR UPDATE;
    v_found := FOUND;
    v_now := pg_catalog.clock_timestamp();
    IF (v_found AND v_expires > v_now) IS NOT TRUE THEN
        RETURN;
    END IF;

    INSERT INTO public.admin_identities AS i (email, password_hash)
    VALUES (v_email, p_new_hash)
    ON CONFLICT (email) DO NOTHING
    RETURNING i.id INTO v_identity;
    IF v_identity IS NULL THEN
        RETURN;
    END IF;
    -- The click is the mailbox proof the identity is born with.
    UPDATE public.admin_identities AS i SET email_verified_at = pg_catalog.now()
     WHERE i.id = v_identity;

    INSERT INTO public.admin_users AS a (tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
    VALUES (v_tenant, v_name, v_email, p_new_hash, v_role, v_identity, 'invite', pg_catalog.now())
    RETURNING a.id INTO v_admin;

    UPDATE public.admin_invites AS v SET used_at = pg_catalog.now()
     WHERE v.id = v_invite
       AND v.tenant_id = v_tenant
       AND v.used_at IS NULL;
    INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail)
    VALUES (v_tenant, v_admin, 'admin.invite.accepted', v_admin::text,
            pg_catalog.jsonb_build_object('outcome', 'ok', 'invite_id', v_invite::text));

    RETURN QUERY SELECT v_admin, v_tenant;
EXCEPTION WHEN unique_violation THEN
    -- A row of the address in that business committed meanwhile: the identity born above goes with
    -- this block's rollback.
    RETURN;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.identity_accept_invite_new(text, text) OWNER TO tappa_identitydefiner;
REVOKE ALL ON FUNCTION public.identity_accept_invite_new(text, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.identity_accept_invite_new(text, text) TO tappa_app;

-- ---------------------------------------------------------------------------
-- 6. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    f      text;
    owner  text;
BEGIN
    FOREACH f IN ARRAY ARRAY['public.resolve_admin_invite_by_token_hash(text)',
                             'public.identity_accept_invite(text,text)',
                             'public.identity_accept_invite_new(text,text)'] LOOP
        owner := CASE WHEN f LIKE 'public.resolve%' THEN 'tappa_resolver' ELSE 'tappa_identitydefiner' END;
        IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                        WHERE p.oid = f::pg_catalog.regprocedure AND p.prosecdef
                          AND p.proconfig = ARRAY['search_path=pg_catalog, pg_temp']
                          AND p.proowner = owner::pg_catalog.regrole)
           OR pg_catalog.has_function_privilege('public', f::pg_catalog.regprocedure, 'EXECUTE')
           OR pg_catalog.has_function_privilege('tappa_operator', f::pg_catalog.regprocedure, 'EXECUTE')
           OR pg_catalog.has_function_privilege('tappa_opdefiner', f::pg_catalog.regprocedure, 'EXECUTE')
           OR NOT pg_catalog.has_function_privilege('tappa_app', f::pg_catalog.regprocedure, 'EXECUTE') THEN
            RAISE EXCEPTION 'migration 00077 postcondition: % is not the ADR''s shape', f;
        END IF;
    END LOOP;
    IF pg_catalog.has_column_privilege('tappa_app', 'public.admin_invites', 'token_hash', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.admin_invites', 'used_at', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.admin_invites', 'created_at', 'INSERT')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.admin_invites', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.admin_invites', 'TRUNCATE')
       OR NOT pg_catalog.has_column_privilege('tappa_app', 'public.admin_invites', 'cancelled_at', 'UPDATE') THEN
        RAISE EXCEPTION 'migration 00077 postcondition: tappa_app''s reach on admin_invites is not the file''s';
    END IF;
    IF pg_catalog.has_any_column_privilege('tappa_operator', 'public.admin_invites', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.admin_invites', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00077 postcondition: an operator role reaches admin_invites';
    END IF;
    IF pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'email', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'role', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'email_verified_at', 'INSERT')
       OR pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_invites', 'cancelled_at', 'UPDATE') THEN
        RAISE EXCEPTION 'migration 00077 postcondition: the identity definer reaches a write it may not';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- The DROP TABLE below removes the two foreign keys' triggers on tenants and admin_users: their locks
-- are taken here first, named, in the order the Up took them, then the table's own.
LOCK TABLE tenants, admin_users IN SHARE ROW EXCLUSIVE MODE;
LOCK TABLE admin_invites IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_invites) THEN
        RAISE EXCEPTION 'migration 00077 Down: admin_invites holds invitations; dropping it would drop the businesses'' trail of them. Go back with the previous binary, which runs on this schema (ADR 0031 8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP FUNCTION public.identity_accept_invite_new(text, text);
DROP FUNCTION public.identity_accept_invite(text, text);
DROP FUNCTION public.resolve_admin_invite_by_token_hash(text);
SET LOCAL lock_timeout = '1s';
REVOKE INSERT (tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
    ON admin_users FROM tappa_identitydefiner;
DROP TABLE admin_invites;
