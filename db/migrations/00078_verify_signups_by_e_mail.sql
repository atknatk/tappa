-- 00078 -- M14-17: a registration is verified by e-mail BEFORE anything is born. POST /signup no longer
-- creates a business: it writes a DRAFT (admin_signup_claims, tenant-scope-less, 24-hour TTL) and
-- mails a link. The tenant, the identity and the owner membership are born only when that link is
-- confirmed (POST, never the GET) -- in ONE transaction: the application creates the tenant and its
-- venues (db.WithTenant, today's Provision), and identity_claim_signup consumes the draft and writes
-- the identity (new address) and the owner row (linked_via = 'claim').
--
-- NORMATIVE SOURCE: docs/adr/0031-tek-kimlik-cok-isletme.md §5.10 (the wizard, R2, R18, the two
-- paths), §5.1 (admin_signup_claims, R17's named sweep), §5.2 (the closed list: the four functions
-- below, their keys, the verb x column matrix -- tenants (vat_number) and the system column xmin
-- included -- and the 2026-10-26 user decision: the resolver also returns the draft), §5.8 (the
-- confirmation window of an existing identity), §6 (the third §4.5 exception), §8 (the flag does not
-- gate this: registration verification is open with TAPPA_ADMIN_IDENTITY off); the card is
-- docs/plan/m14-tek-kimlik.md -> M14-17.
--
-- NOTHING BEFORE THE CLICK (K-9, bulgu 11, R18): identity_open_signup_claim writes the draft row and
-- touches no other table. For a new and a known address it writes the same row; the response and its
-- time are the application's (internal/handler's signup floor).
--
-- admin_signup_claims IS TENANT-SCOPE-LESS (CLAUDE.md §6, ADR 0031 §5.1): a registration belongs to
-- no tenant -- there is none yet. Hence the R5 waiver below, an explicit REVOKE ALL from tappa_app (the
-- dev and production default ACL hands a new table `arwd` -- 00069's measurement), ENABLE + FORCE row
-- level security and NO policy. uuid key, so no sequence. No column is called tenant_id.
-- redline: no-tenant-scope(admin_signup_claims) — kayit taslagi, dogrulama tiklamasindan once hicbir tenant yok; yazari yalniz identity_* definer'lari, silen yalniz identity_sweep_signup_claims, ADR 0031 §5.1/§5.10/§6
--
-- THE FOUR FUNCTIONS (ADR 0031 §5.2):
--   resolve_signup_claim_by_token_hash  md.7, owner tappa_resolver: (id, email, password_hash,
--       expires_at, used_at, draft) by the claim token's hash. The digest and the draft come back only
--       to the holder of the claim secret (S12; the draft: the 2026-10-26 user decision -- the
--       application creates the tenant from it, and the content is what the requester typed).
--   identity_open_signup_claim  ⚠️ not keyed by a secret, writes no identity (S11): one draft row,
--       keyed by the token the caller just drew; bounded by signupCreateLimit and the per-recipient
--       cap (the application's). It refuses (NULL, nothing written) every value a CHECK below would
--       refuse -- BEFORE the INSERT, because the definer writes password_hash and a CHECK violation
--       would put the digest into the error's DETAIL (00074's lesson).
--   identity_claim_signup  keyed by the claim token (+ a live session on the existing-identity path).
--       Consumes the draft and writes the identity and the owner row into the tenant the APPLICATION
--       created in this very transaction. The structural predicates (S2, T7) -- each a refusal (0
--       rows) when it does not hold:
--         1. the tenant has NO admin_users row (THE MAIN GUARANTEE: a business someone already runs
--            is never a registration);
--         2. tenants.created_at = now() (the transaction's start; a savepoint does not change it;
--            tappa_app cannot write created_at -- 00016's grants);
--         3. tenants.xmin = pg_current_xact_id()::xid (written by THIS transaction -- true at the top
--            level, false under a SAVEPOINT: measured on 17.10 in 00074; the registration runs in the
--            top-level db.WithTenant);
--         4. p_tenant_id = current_setting('app.tenant_id') (the caller's own RLS context);
--         5. the tenant's VAT number and name are the draft's.
--       An existing identity claims only with its OWN live session and only when its address equals
--       the claim's (R18); the click is its mailbox proof: email_verified_at fills when empty and a
--       first_verify window opens (§5.8, §13 K-9). A new address gets an identity with the claim's
--       digest (the APPLICATION compared the re-entered password with it before calling -- S12, a
--       Go-type-bound guarantee), email_verified_at = the click, under the address's namespaced lock
--       with ON CONFLICT (email) DO NOTHING (S13) -- a concurrent birth ends in 0 rows, never a 500.
--       Here ON CONFLICT is what refuses the loser (measured: without it, 23505); the lock -- 00074's
--       key, byte for byte -- orders the claim against the old pod's identity_create_for_signup, whose
--       "no other membership row" read the ON CONFLICT cannot stand in for.
--       NEVER for an address that is not printable ASCII (the user's ASCII address rule, 2026-10-26):
--       the stored address is one this product can send to.
--       It writes the registration's own signup.completed row (the tenant's trail; the same row on
--       both paths, ADR 0031 §5.2: a function that writes a tenant writes that tenant's audit_log).
--   identity_sweep_signup_claims  ⚠️ fixed-period delete (S11, R17): expired or used drafts, no
--       argument, by age and state alone -- the ONE delete path of the table.
--
-- identity_create_for_signup (00074) STAYS: the binary of this deploy no longer calls it, but an old
-- pod in this rollout may (R8). M14-19 takes tappa_app's EXECUTE, contract B (M14-20b) drops it.
--
-- THE GRANTS (ADR 0031 §5.2's matrix; identitycatalog_test.go pins them and shows each one used):
-- admin_signup_claims -- the definer: SELECT (id, token_hash, email, draft, password_hash, expires_at,
-- used_at), INSERT (token_hash, email, draft, password_hash, expires_at), UPDATE (used_at), DELETE (the
-- sweep alone); the resolver: SELECT of the six it returns and the key. admin_users INSERT (tenant_id,
-- full_name, email, password_hash, role, identity_id, linked_via, linked_at) -- the owner row; tenants
-- SELECT (vat_number) and the system column xmin -- predicates 3 and 5. Everything else the bodies touch
-- 00071, 00072 and 00074 already granted.
--
-- INDEPENDENT OF 00076 AND 00077 (built in parallel; goose runs without allow-missing): nothing here
-- reads or names what they add.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.identity_create_for_signup(uuid,uuid)') IS NULL
       OR pg_catalog.to_regprocedure('public.resolve_identity_by_email(citext)') IS NULL
       OR pg_catalog.to_regclass('public.admin_identities') IS NULL
       OR pg_catalog.to_regclass('public.admin_identity_events') IS NULL THEN
        RAISE EXCEPTION 'migration 00078 (M14-17) needs 00069-00075 (the identity tables and functions). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regclass('public.admin_signup_claims') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                   WHERE p.pronamespace = 'public'::pg_catalog.regnamespace
                     AND p.proname IN ('identity_open_signup_claim', 'identity_claim_signup',
                                       'identity_sweep_signup_claims', 'resolve_signup_claim_by_token_hash')) THEN
        RAISE EXCEPTION 'migration 00078 (M14-17) creates admin_signup_claims and its four functions, and one is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. admin_signup_claims (ADR 0031 §5.1, §5.10)
-- ---------------------------------------------------------------------------
-- token_hash: the HMAC of the link's token (internal/adminauth), 00019's shape, globally unique.
-- email: the address the link was sent to (citext, as admin_identities.email).
-- draft: the registration (closed keys -- internal/domain/signup's encoder is the one writer of its
--   contents; the CHECK holds the shape): full_name, business_name, vat_number, business_type,
--   structure, vat_check, venues (1..25 strings) -- ADR 0031 §5.2, the 2026-10-26 user decision adds
--   business_type, structure and vat_check.
-- password_hash: the digest of the password the form chose (00018's bcrypt CHECK); the new identity is
--   born with it, after the re-entered password matched it (R2, S12).
-- expires_at: at most 24 hours after created_at (00019's ceiling for a link).
CREATE TABLE admin_signup_claims (
    id            uuid        NOT NULL DEFAULT gen_random_uuid(),
    token_hash    text        NOT NULL,
    email         citext      NOT NULL,
    draft         jsonb       NOT NULL,
    password_hash text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    expires_at    timestamptz NOT NULL,
    used_at       timestamptz,
    CONSTRAINT admin_signup_claims_pkey PRIMARY KEY (id),
    CONSTRAINT admin_signup_claims_token_hash_key UNIQUE (token_hash),
    CONSTRAINT admin_signup_claims_token_hash_shape CHECK (token_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT admin_signup_claims_password_hash_is_bcrypt
        CHECK (password_hash ~ '^\$2[aby]\$(0[4-9]|1[0-4])\$[./A-Za-z0-9]{53}$'),
    CONSTRAINT admin_signup_claims_ttl_ceiling
        CHECK (expires_at > created_at AND expires_at <= created_at + interval '24 hours'),
    CONSTRAINT admin_signup_claims_draft_closed CHECK (
        jsonb_typeof(draft) = 'object'
        AND (draft - ARRAY['full_name', 'business_name', 'vat_number', 'business_type', 'structure',
                           'vat_check', 'venues']) = '{}'::jsonb
        AND jsonb_typeof(draft -> 'full_name') = 'string'
        AND jsonb_typeof(draft -> 'business_name') = 'string'
        AND jsonb_typeof(draft -> 'vat_number') = 'string'
        AND jsonb_typeof(draft -> 'business_type') = 'string'
        AND jsonb_typeof(draft -> 'structure') = 'string'
        AND (draft ->> 'vat_check') IN ('valid', 'invalid', 'unknown')
        AND jsonb_typeof(draft -> 'venues') = 'array'
        AND jsonb_array_length(draft -> 'venues') BETWEEN 1 AND 25
        AND NOT jsonb_path_exists(draft -> 'venues', '$[*] ? (@.type() != "string")'))
);
-- The sweep reads by age.
CREATE INDEX admin_signup_claims_expires_idx ON admin_signup_claims (expires_at);
ALTER TABLE admin_signup_claims ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_signup_claims FORCE ROW LEVEL SECURITY;
-- NOT a no-op: the dev and production default ACL has just handed tappa_app `arwd` on this table.
REVOKE ALL ON admin_signup_claims FROM tappa_app;

-- ---------------------------------------------------------------------------
-- 2. The column grants (ADR 0031 §5.2's matrix -- what THESE functions need and 00071-00074 did not give)
-- ---------------------------------------------------------------------------
GRANT SELECT (id, token_hash, email, draft, password_hash, expires_at, used_at),
      INSERT (token_hash, email, draft, password_hash, expires_at),
      UPDATE (used_at),
      DELETE
   ON admin_signup_claims TO tappa_identitydefiner;
GRANT SELECT (id, token_hash, email, password_hash, expires_at, used_at, draft)
   ON admin_signup_claims TO tappa_resolver;
SET LOCAL lock_timeout = '1s';
GRANT INSERT (tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
   ON admin_users TO tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
GRANT SELECT (vat_number, xmin) ON tenants TO tappa_identitydefiner;

-- ---------------------------------------------------------------------------
-- 3. The resolver (ADR 0002 md.7: read-only, <= 1 row from a UNIQUE, owner tappa_resolver)
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
CREATE FUNCTION public.resolve_signup_claim_by_token_hash(p_token_hash text)
    RETURNS TABLE (
        id            uuid,
        email         text,
        password_hash text,
        expires_at    timestamptz,
        used_at       timestamptz,
        draft         jsonb
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    SELECT c.id, c.email::text, c.password_hash, c.expires_at, c.used_at, c.draft
      FROM public.admin_signup_claims AS c
     WHERE c.token_hash = p_token_hash;
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 4. The writers (owner tappa_identitydefiner)
-- ---------------------------------------------------------------------------
-- Written in the SQL subset M14-04's belt models (identitybelt_test.go): every relation as
-- `public.<name> AS <alias>`, every WHERE a conjunction of `X = Y`, comparisons and IS [NOT] NULL;
-- everything else is decided in plpgsql, each test `IS NOT TRUE` -> refused, so a NULL fails closed.

-- identity_open_signup_claim -- ⚠️ NOT KEYED BY A SECRET, WRITES NO IDENTITY (S11). NULL and nothing
-- written for anything the table would refuse (see the header: the CHECKs are restated here so a
-- violation never reaches an error's DETAIL with the digest in it) and for an address that is not
-- printable ASCII.
-- +goose StatementBegin
CREATE FUNCTION public.identity_open_signup_claim(p_token_hash text, p_email citext, p_draft jsonb,
                                                  p_password_hash text, p_expires_at timestamptz)
    RETURNS uuid
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict error
DECLARE
    v_claim uuid;
BEGIN
    IF (p_token_hash ~ '^[0-9a-f]{64}$'
        AND pg_catalog.octet_length(p_email::text) <= 254
        AND pg_catalog.strpos(p_email::text, '=?') = 0
        AND p_email::text ~ '^[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*@([-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+(\.[-A-Za-z0-9!#$%&''*+/=?^_`{|}~]+)*|\[((25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|(([0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}|([0-9A-Fa-f]{1,4}:){6}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,6})?|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,5})?|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,4})?|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,3})?|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,2})?|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}(:[0-9A-Fa-f]{1,4}){0,1})?|([0-9A-Fa-f]{1,4}:){5}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4})?|([0-9A-Fa-f]{1,4}:){6}[0-9A-Fa-f]{1,4}::|::([0-9A-Fa-f]{1,4}:){0,5}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,4}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|[0-9A-Fa-f]{1,4}:[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,3}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){2}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,2}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){3}[0-9A-Fa-f]{1,4}::([0-9A-Fa-f]{1,4}:){0,1}(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])|([0-9A-Fa-f]{1,4}:){4}[0-9A-Fa-f]{1,4}::(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])\.(25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9][0-9]|[0-9])))\])$'
        AND p_password_hash ~ '^\$2[aby]\$(0[4-9]|1[0-4])\$[./A-Za-z0-9]{53}$'
        AND p_expires_at > pg_catalog.now()
        AND p_expires_at <= pg_catalog.now() + interval '24 hours'
        AND pg_catalog.jsonb_typeof(p_draft) = 'object'
        AND (p_draft - ARRAY['full_name', 'business_name', 'vat_number', 'business_type', 'structure',
                             'vat_check', 'venues']) = '{}'::jsonb
        AND pg_catalog.jsonb_typeof(p_draft -> 'full_name') = 'string'
        AND pg_catalog.jsonb_typeof(p_draft -> 'business_name') = 'string'
        AND pg_catalog.jsonb_typeof(p_draft -> 'vat_number') = 'string'
        AND pg_catalog.jsonb_typeof(p_draft -> 'business_type') = 'string'
        AND pg_catalog.jsonb_typeof(p_draft -> 'structure') = 'string'
        AND (p_draft ->> 'vat_check') IN ('valid', 'invalid', 'unknown')
        AND pg_catalog.jsonb_typeof(p_draft -> 'venues') = 'array'
        AND pg_catalog.jsonb_array_length(p_draft -> 'venues') BETWEEN 1 AND 25
        AND NOT pg_catalog.jsonb_path_exists(p_draft -> 'venues', '$[*] ? (@.type() != "string")')) IS NOT TRUE THEN
        RETURN NULL;
    END IF;
    INSERT INTO public.admin_signup_claims AS c (token_hash, email, draft, password_hash, expires_at)
    VALUES (p_token_hash, p_email, p_draft, p_password_hash, p_expires_at)
    RETURNING c.id INTO v_claim;
    RETURN v_claim;
END;
$$;
-- +goose StatementEnd

-- identity_claim_signup -- keyed by the claim token (and, on the existing-identity path, a live
-- session). See the header for the predicates. 0 rows is the one refusal; the caller rolls its
-- transaction back (the tenant it created goes with it).
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

-- identity_sweep_signup_claims -- ⚠️ FIXED-PERIOD DELETE (S11, R17): the ONE delete path of the table.
-- No argument; expired drafts by age, used ones by state (their unkeyed DELETEs are named in
-- identitybelt_test.go's allow-list).
-- +goose StatementBegin
CREATE FUNCTION public.identity_sweep_signup_claims()
    RETURNS integer
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
#variable_conflict error
DECLARE
    v_now     timestamptz;
    v_expired integer;
    v_used    integer;
BEGIN
    v_now := pg_catalog.clock_timestamp();
    DELETE FROM public.admin_signup_claims AS c WHERE c.expires_at < v_now;
    GET DIAGNOSTICS v_expired = ROW_COUNT;
    DELETE FROM public.admin_signup_claims AS c WHERE c.used_at IS NOT NULL;
    GET DIAGNOSTICS v_used = ROW_COUNT;
    RETURN v_expired + v_used;
END;
$$;
-- +goose StatementEnd

ALTER FUNCTION public.resolve_signup_claim_by_token_hash(text) OWNER TO tappa_resolver;
ALTER FUNCTION public.identity_open_signup_claim(text, citext, jsonb, text, timestamptz) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_claim_signup(text, uuid, text) OWNER TO tappa_identitydefiner;
ALTER FUNCTION public.identity_sweep_signup_claims() OWNER TO tappa_identitydefiner;

REVOKE ALL ON FUNCTION public.resolve_signup_claim_by_token_hash(text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_open_signup_claim(text, citext, jsonb, text, timestamptz) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_claim_signup(text, uuid, text) FROM PUBLIC;
REVOKE ALL ON FUNCTION public.identity_sweep_signup_claims() FROM PUBLIC;

GRANT EXECUTE ON FUNCTION public.resolve_signup_claim_by_token_hash(text) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_open_signup_claim(text, citext, jsonb, text, timestamptz) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_claim_signup(text, uuid, text) TO tappa_app;
GRANT EXECUTE ON FUNCTION public.identity_sweep_signup_claims() TO tappa_app;

-- ---------------------------------------------------------------------------
-- 5. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    f record;
BEGIN
    FOR f IN
        SELECT p.oid, p.proname, p.prosecdef, p.proconfig, p.proowner
          FROM pg_catalog.pg_proc p
         WHERE p.pronamespace = 'public'::pg_catalog.regnamespace
           AND p.proname IN ('identity_open_signup_claim', 'identity_claim_signup',
                             'identity_sweep_signup_claims', 'resolve_signup_claim_by_token_hash')
    LOOP
        IF NOT f.prosecdef
           OR f.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
           OR f.proowner <> (CASE WHEN f.proname = 'resolve_signup_claim_by_token_hash'
                                  THEN 'tappa_resolver' ELSE 'tappa_identitydefiner' END)::pg_catalog.regrole
           OR pg_catalog.has_function_privilege('public', f.oid, 'EXECUTE')
           OR pg_catalog.has_function_privilege('tappa_operator', f.oid, 'EXECUTE')
           OR pg_catalog.has_function_privilege('tappa_opdefiner', f.oid, 'EXECUTE')
           OR NOT pg_catalog.has_function_privilege('tappa_app', f.oid, 'EXECUTE') THEN
            RAISE EXCEPTION 'migration 00078 postcondition: % is not the ADR''s shape', f.proname;
        END IF;
    END LOOP;
    IF (SELECT pg_catalog.count(*) FROM pg_catalog.pg_proc p
         WHERE p.pronamespace = 'public'::pg_catalog.regnamespace
           AND p.proname IN ('identity_open_signup_claim', 'identity_claim_signup',
                             'identity_sweep_signup_claims', 'resolve_signup_claim_by_token_hash')) <> 4 THEN
        RAISE EXCEPTION 'migration 00078 postcondition: the four functions are not all there';
    END IF;
    IF pg_catalog.has_table_privilege('tappa_app', 'public.admin_signup_claims', 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.admin_signup_claims', 'SELECT, INSERT, UPDATE, REFERENCES')
       OR pg_catalog.has_any_column_privilege('tappa_operator', 'public.admin_signup_claims', 'SELECT, INSERT, UPDATE, REFERENCES')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.admin_signup_claims', 'SELECT, INSERT, UPDATE, REFERENCES') THEN
        RAISE EXCEPTION 'migration 00078 postcondition: a role outside the identity definer and the resolver reaches admin_signup_claims';
    END IF;
    IF pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'email', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_users', 'email', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_identitydefiner', 'public.admin_identities', 'email_verified_at', 'INSERT')
       OR pg_catalog.has_table_privilege('tappa_identitydefiner', 'public.tenants', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00078 postcondition: the identity definer reaches an e-mail write, a mailbox proof at INSERT or a tenant INSERT it may not';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- The drafts go with their table (they are drafts, not records -- R17); the identities and the owner
-- rows a confirmation made STAY (identities and memberships like any other, linked_via = 'claim').
DROP FUNCTION public.identity_sweep_signup_claims();
DROP FUNCTION public.identity_claim_signup(text, uuid, text);
DROP FUNCTION public.identity_open_signup_claim(text, citext, jsonb, text, timestamptz);
DROP FUNCTION public.resolve_signup_claim_by_token_hash(text);
SET LOCAL lock_timeout = '1s';
REVOKE INSERT (tenant_id, full_name, email, password_hash, role, identity_id, linked_via, linked_at)
    ON admin_users FROM tappa_identitydefiner;
SET LOCAL lock_timeout = '1s';
REVOKE SELECT (vat_number, xmin) ON tenants FROM tappa_identitydefiner;
DROP TABLE admin_signup_claims;
