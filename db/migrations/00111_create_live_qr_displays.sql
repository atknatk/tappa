-- 00111 -- M16-19 (ADR 0045 §2.5, Bedel 5): live_qr_displays -- a venue screen paired to one location
-- that shows a rotating signed QR -- and its three context-less resolvers.
--
-- NORMATIVE SOURCES: docs/adr/0045-istege-bagli-native-uygulama.md §2.5 (the live QR: a screen page
-- served by the Go binary, paired from the panel to a location, no secret on the device, only a
-- paired screen session); docs/plan/m16-mobil.md M16-19 (this contract), M16-20 (the pairing and the
-- screen page) and M16-21 (the tap); docs/adr/0002-tenant-baglami-ve-rls.md md.7 (a resolver's five
-- constraints); docs/adr/0028-dis-api-anahtar-akis-webhook.md §1 (the `arwd` rule: REVOKE ALL from
-- tappa_app, then the verbs and the columns it needs; UPDATE always by column; every new function
-- REVOKEd from PUBLIC; a trigger function EXECUTE in its owner alone, asserting TG_RELID); 00108 (the
-- app device table and resolver this file follows).
--
-- ONE ROW IS ONE SCREEN, PAIRED ONCE. The panel creates the row with a single-use pairing code (its
-- digest and an expiry); the screen page sends the code, and the ONE statement that consumes it
-- (db/queries/liveqr.sql ConsumeLiveQRDisplayPairing) clears the code's digest, stamps paired_at and
-- stores the digest of the screen's session cookie. From then on the screen is known by that
-- session. A lost cookie, a moved tablet, a second screen: a NEW row -- a row's pairing never
-- changes (the one-way trigger below), so "which screen produced this token" has one answer for the
-- row's whole life.
--   * pairing_code_hash / session_hash: hex SHA-256 digests (64 lowercase hex; the CHECKs). The code
--     and the cookie value never reach the database. Whether the code's digest is keyed (a short,
--     typeable code needs a server-side key against an offline guess) is M16-20's choice -- the
--     column holds either. Both are GLOBALLY UNIQUE: each resolver answers at most one row, across
--     every tenant (ADR 0002 md.7 (ii)).
--   * A CODE'S DIGEST LIVES ONLY AS LONG AS THE CODE CAN BE USED (M16-19 security review). Held for
--     ever it would burn its value in the global UNIQUE -- with a short code, one tenant's dead
--     screens would refuse another tenant's new code (23505). So the digest is NULL in every state
--     but "waiting": a pairing clears it, a revocation clears it (RevokeLiveQRDisplay), and an
--     expired pairing may have it cleared (ClearExpiredLiveQRPairingCodes -- M16-20 calls it in the
--     creating tenant before drawing a code). Three states, nothing between (the CHECK): waiting
--     (code, no session), paired (session, no code), and DEAD UNPAIRED (neither -- revoked or
--     expired before its code was used). ⚠️ The sweep is tenant-scoped (RLS; K-19's lazy-delete
--     shape): another tenant's expired codes stay until that tenant creates or revokes a screen.
--     Against that residue the code's strength is M16-20's (its card: 64 bits, or a short code with
--     a limit; the M16-19 handover note under M16-20).
--   * The pairing's lifetime is bounded by the database clock: created_at is not writable by
--     tappa_app, pairing_expires_at is at most one hour after it (the ceiling; M16-20 picks the
--     actual, shorter, TTL), and a pairing is stamped before the expiry (CHECK).
--   * revoked_at is ONE WAY (00099 / 00108's shape) and FINAL: once a screen is revoked nothing about
--     it changes but dropping a code it still holds. Revocation is §5 row 1's twin for the live QR (M16-21): a revoked screen's last
--     token lives ~20 s, and the revocation is in this row, not in the token -- which is why the tap
--     path resolves the row (the third resolver) instead of trusting the token alone.
--   * tappa_app never DELETEs a screen: revocation is the stamp, the trail of which screen stood at
--     which location stays (CLAUDE.md §6: a status field, not a delete).
--   * No personal data: no name, no person, no device probe. A screen is a location's.
--
-- THE THREE RESOLVERS (ADR 0002 md.7's five, the 00003 / 00004 / 00108 shape): owner tappa_resolver
-- (NOLOGIN, BYPASSRLS, NOSUPERUSER), SECURITY DEFINER, STABLE, search_path = pg_catalog, pg_temp,
-- every table schema-qualified; a key-only input; at most ONE row (a UNIQUE or the primary key); a
-- fixed result; EXECUTE to tappa_app alone. Each CARRIES the state and decides nothing (revoked_at,
-- the expiry, the location's state are returned, never filtered):
--   resolve_live_qr_display_by_pairing_code_hash(text)  the screen page's pairing request: no
--       tenant, only a code. -> (id, tenant_id, location_id, pairing_expires_at, revoked_at). The
--       consumption then runs under SET LOCAL app.tenant_id as an ordinary, single, tenant-filtered
--       UPDATE.
--   resolve_live_qr_display_by_session_hash(text)  the screen page's every later request (the token
--       refresh): no tenant, only the cookie. -> (id, tenant_id, location_id, revoked_at).
--   resolve_live_qr_display_by_id(uuid)  THE TAP (M16-21): the employee's request names the screen
--       through the signed token. It runs without a tenant context and, in a holding (ADR 0027 --
--       a KM employee at a KF screen), from ANOTHER tenant's session, so it must read another
--       tenant's row: the twin of 00004's resolve_tag_by_uid (00004_create_tags.sql:143, owner
--       tappa_resolver :178, EXECUTE tappa_app :182-183). -> (id, tenant_id, location_id,
--       revoked_at, location_active). "The signed token carries the tenant, so a tenant-scoped read
--       would do" was NOT taken (the card): the token cannot see a revocation, and in a holding the
--       session's tenant is not the screen's -- a scoped read would see no row.
-- tappa_resolver reads exactly the columns these need: seven of live_qr_displays (not paired_at,
-- not created_at) and THREE of locations (id, tenant_id, active) -- the first locations grant this
-- role holds (locations_test.go names it; the reason is the third resolver's location state, a
-- boolean that is no evidence: static_ips, GPS and Wi-Fi stay unreadable to it).
--
-- 🔴 lock_timeout 1 s, the FIRST statement of the Up and of the Down (00054's rule): the new keys to
-- tenants and locations take SHARE ROW EXCLUSIVE on both (the FK triggers), which a venue write
-- waits behind. Behind a long writer the file gives up with 55P03 instead of queueing it.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regclass('public.live_qr_displays') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                   WHERE p.proname IN ('resolve_live_qr_display_by_pairing_code_hash', 'resolve_live_qr_display_by_session_hash',
                                       'resolve_live_qr_display_by_id', 'live_qr_displays_state_is_one_way')) THEN
        RAISE EXCEPTION 'migration 00111 (M16-19) creates live_qr_displays and its four functions, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                    WHERE rolname = 'tappa_resolver' AND rolbypassrls AND NOT rolsuper AND NOT rolcanlogin) THEN
        RAISE EXCEPTION 'migration 00111 (M16-19): role tappa_resolver must exist, NOLOGIN, NOSUPERUSER and BYPASSRLS (scripts/db-init/01-roles.sql). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- A column grant cannot narrow a wider privilege some other hand gave (00024 measured it).
    IF pg_catalog.has_table_privilege('tappa_resolver', 'public.locations', 'SELECT')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.locations'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'SELECT')) THEN
        RAISE EXCEPTION 'migration 00111 (M16-19) gives tappa_resolver SELECT on three columns of locations, and it already reads locations. A column grant cannot narrow that. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

CREATE TABLE live_qr_displays (
    id                 uuid        NOT NULL DEFAULT gen_random_uuid(),
    tenant_id          uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    location_id        uuid        NOT NULL,
    pairing_code_hash  text,
    pairing_expires_at timestamptz NOT NULL,
    paired_at          timestamptz,
    session_hash       text,
    created_at         timestamptz NOT NULL DEFAULT now(),
    revoked_at         timestamptz,
    CONSTRAINT live_qr_displays_pkey PRIMARY KEY (id),
    -- The screen's location is its own tenant's (a screen cannot stand at another tenant's venue).
    CONSTRAINT live_qr_displays_location_fk FOREIGN KEY (location_id, tenant_id)
        REFERENCES locations (id, tenant_id) ON DELETE RESTRICT,
    -- GLOBAL, not per tenant: each digest names at most one screen (the resolvers' <= 1 row).
    CONSTRAINT live_qr_displays_pairing_code_hash_key UNIQUE (pairing_code_hash),
    CONSTRAINT live_qr_displays_session_hash_key UNIQUE (session_hash),
    CONSTRAINT live_qr_displays_pairing_code_hash_shape CHECK (pairing_code_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT live_qr_displays_session_hash_shape CHECK (session_hash ~ '^[0-9a-f]{64}$'),
    -- Three states and nothing between: waiting for its code; paired with a session; dead unpaired
    -- (revoked or expired, its code cleared -- the trigger allows that state only then, and a row is
    -- born waiting).
    CONSTRAINT live_qr_displays_pairing_state CHECK (
        (paired_at IS NULL AND pairing_code_hash IS NOT NULL AND session_hash IS NULL)
        OR (paired_at IS NOT NULL AND pairing_code_hash IS NULL AND session_hash IS NOT NULL)
        OR (paired_at IS NULL AND pairing_code_hash IS NULL AND session_hash IS NULL)),
    CONSTRAINT live_qr_displays_pairing_lifetime CHECK (
        pairing_expires_at > created_at AND pairing_expires_at <= created_at + interval '1 hour'),
    CONSTRAINT live_qr_displays_paired_while_live CHECK (
        paired_at IS NULL OR (paired_at >= created_at AND paired_at < pairing_expires_at)),
    CONSTRAINT live_qr_displays_revoked_after_created CHECK (revoked_at IS NULL OR revoked_at >= created_at)
);

-- tenant_id leads (CLAUDE.md §6): RLS, the panel's per-location list (M16-20), and the location
-- key's RESTRICT check.
CREATE INDEX live_qr_displays_tenant_location_idx ON live_qr_displays (tenant_id, location_id);

ALTER TABLE live_qr_displays ENABLE ROW LEVEL SECURITY;
ALTER TABLE live_qr_displays FORCE ROW LEVEL SECURITY;
CREATE POLICY live_qr_displays_tenant_isolation ON live_qr_displays
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- --- the one-way state (00099 / 00108's shape) -------------------------------------------------------
-- A row is BORN WAITING (a code, no pairing, no session, not revoked). A paired screen keeps its
-- session; an unpaired screen's code is never swapped, and it is dropped only by pairing it, by
-- revoking the screen, or once the pairing has expired -- and a dropped code never comes back; a
-- revoked screen is final but for dropping a code it still holds; both stamps are the moment they
-- happen. The CHECKs hold the shape of a row, this holds its history -- tappa_app holds INSERT on the
-- code and UPDATE on the four state columns. The time test is here, not in the CHECK: a CHECK that
-- read now() would refuse a restore.
-- +goose StatementBegin
CREATE FUNCTION public.live_qr_displays_state_is_one_way()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP NOT IN ('INSERT', 'UPDATE')
       OR TG_RELID <> 'public.live_qr_displays'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'live_qr_displays_state_is_one_way: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF TG_OP = 'INSERT' THEN
        IF NEW.pairing_code_hash IS NULL OR NEW.paired_at IS NOT NULL OR NEW.session_hash IS NOT NULL
           OR NEW.revoked_at IS NOT NULL THEN
            RAISE EXCEPTION 'a live QR screen is born waiting for its code: a code, no pairing, no session, not revoked'
                USING ERRCODE = 'restrict_violation';
        END IF;
        RETURN NEW;
    END IF;
    IF OLD.revoked_at IS NOT NULL THEN
        -- Final, but for dropping the code an unpaired screen still holds.
        IF NEW.revoked_at IS DISTINCT FROM OLD.revoked_at OR NEW.paired_at IS DISTINCT FROM OLD.paired_at
           OR NEW.session_hash IS DISTINCT FROM OLD.session_hash OR NEW.pairing_code_hash IS NOT NULL THEN
            RAISE EXCEPTION 'a revoked live QR screen is final: nothing about it changes but dropping its code; pair a new screen instead'
                USING ERRCODE = 'restrict_violation';
        END IF;
        RETURN NEW;
    END IF;
    IF NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
       AND (NEW.revoked_at < pg_catalog.now() OR NEW.revoked_at > pg_catalog.clock_timestamp()) THEN
        RAISE EXCEPTION 'a live QR screen''s revocation is stamped when it happens: revoked_at may not be back-dated or set in the future'
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF NEW.paired_at IS DISTINCT FROM OLD.paired_at
       OR NEW.session_hash IS DISTINCT FROM OLD.session_hash
       OR NEW.pairing_code_hash IS DISTINCT FROM OLD.pairing_code_hash THEN
        IF OLD.paired_at IS NOT NULL THEN
            RAISE EXCEPTION 'a live QR screen is paired once: its pairing and its session never change; pair a new screen instead'
                USING ERRCODE = 'restrict_violation';
        END IF;
        IF NEW.paired_at IS NULL THEN
            -- Not a pairing: the one other change is dropping the code of a screen revoked in this
            -- statement or whose pairing has expired.
            IF NEW.pairing_code_hash IS NOT NULL OR NEW.session_hash IS NOT NULL
               OR NOT (NEW.revoked_at IS NOT NULL OR OLD.pairing_expires_at <= pg_catalog.now()) THEN
                RAISE EXCEPTION 'an unpaired live QR screen''s code is never swapped, and is dropped only by pairing it, revoking it or once its pairing has expired; create a new screen instead'
                    USING ERRCODE = 'restrict_violation';
            END IF;
            RETURN NEW;
        END IF;
        IF OLD.pairing_code_hash IS NULL THEN
            RAISE EXCEPTION 'a live QR screen whose code was dropped never pairs; create a new screen instead'
                USING ERRCODE = 'restrict_violation';
        END IF;
        IF NEW.paired_at < pg_catalog.now() OR NEW.paired_at > pg_catalog.clock_timestamp() THEN
            RAISE EXCEPTION 'a live QR screen''s pairing is stamped when it happens: paired_at may not be back-dated or set in the future'
                USING ERRCODE = 'restrict_violation';
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.live_qr_displays_state_is_one_way() FROM PUBLIC;

CREATE TRIGGER live_qr_displays_born_waiting
    BEFORE INSERT ON public.live_qr_displays
    FOR EACH ROW
    EXECUTE FUNCTION public.live_qr_displays_state_is_one_way();

CREATE TRIGGER live_qr_displays_state_one_way
    BEFORE UPDATE ON public.live_qr_displays
    FOR EACH ROW
    WHEN (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
          OR NEW.paired_at IS DISTINCT FROM OLD.paired_at
          OR NEW.session_hash IS DISTINCT FROM OLD.session_hash
          OR NEW.pairing_code_hash IS DISTINCT FROM OLD.pairing_code_hash)
    EXECUTE FUNCTION public.live_qr_displays_state_is_one_way();

-- --- tappa_app (ADR 0028 §1's `arwd` rule) --------------------------------------------------------
-- Reads every column but session_hash (only the session resolver matches it; nothing returns it);
-- pairing_code_hash IS readable because the consumption's WHERE names it (no query RETURNS it).
-- Inserts a screen by naming its tenant, location, code digest and expiry (id and created_at are the
-- database's -- the lifetime CHECK is bounded by the database's clock). Updates the four state
-- columns alone (the pairing and the revocation; the trigger holds their history). Never deletes.
REVOKE ALL ON live_qr_displays FROM tappa_app;
GRANT SELECT (id, tenant_id, location_id, pairing_code_hash, pairing_expires_at, paired_at, created_at, revoked_at)
    ON live_qr_displays TO tappa_app;
GRANT INSERT (tenant_id, location_id, pairing_code_hash, pairing_expires_at) ON live_qr_displays TO tappa_app;
GRANT UPDATE (pairing_code_hash, paired_at, session_hash, revoked_at) ON live_qr_displays TO tappa_app;

-- --- the resolvers ----------------------------------------------------------------------------------
GRANT SELECT (id, tenant_id, location_id, pairing_code_hash, pairing_expires_at, session_hash, revoked_at)
    ON live_qr_displays TO tappa_resolver;
GRANT SELECT (id, tenant_id, active) ON locations TO tappa_resolver;

-- +goose StatementBegin
CREATE FUNCTION public.resolve_live_qr_display_by_pairing_code_hash(p_code_hash text)
    RETURNS TABLE (
        id                 uuid,
        tenant_id          uuid,
        location_id        uuid,
        pairing_expires_at timestamptz,
        revoked_at         timestamptz
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    -- <= 1 row: pairing_code_hash is UNIQUE (a consumed, revoked or swept code is NULL and matches nothing). A fixed
    -- column list. The expiry and the revocation are returned, not filtered: the consumption's own
    -- WHERE decides, under the resolved tenant's context.
    SELECT d.id, d.tenant_id, d.location_id, d.pairing_expires_at, d.revoked_at
      FROM public.live_qr_displays AS d
     WHERE d.pairing_code_hash = p_code_hash;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_live_qr_display_by_pairing_code_hash(text) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_live_qr_display_by_pairing_code_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_live_qr_display_by_pairing_code_hash(text) TO tappa_app;

-- +goose StatementBegin
CREATE FUNCTION public.resolve_live_qr_display_by_session_hash(p_session_hash text)
    RETURNS TABLE (
        id          uuid,
        tenant_id   uuid,
        location_id uuid,
        revoked_at  timestamptz
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    -- <= 1 row: session_hash is UNIQUE. revoked_at is returned, not filtered (a revoked screen's
    -- page stops showing a code -- the caller's decision, M16-20).
    SELECT d.id, d.tenant_id, d.location_id, d.revoked_at
      FROM public.live_qr_displays AS d
     WHERE d.session_hash = p_session_hash;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_live_qr_display_by_session_hash(text) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_live_qr_display_by_session_hash(text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_live_qr_display_by_session_hash(text) TO tappa_app;

-- +goose StatementBegin
CREATE FUNCTION public.resolve_live_qr_display_by_id(p_display_id uuid)
    RETURNS TABLE (
        id              uuid,
        tenant_id       uuid,
        location_id     uuid,
        revoked_at      timestamptz,
        location_active boolean
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    -- <= 1 row: id is the primary key, and the composite key to locations makes the join one row.
    -- The tenant equality is written on the join. The revocation and the location's state are
    -- returned, not filtered: M16-21's guardrail (§5 row 1's twin) decides, and records (§4.6).
    SELECT d.id, d.tenant_id, d.location_id, d.revoked_at, l.active
      FROM public.live_qr_displays AS d
      JOIN public.locations AS l
        ON l.id = d.location_id
       AND l.tenant_id = d.tenant_id
     WHERE d.id = p_display_id;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_live_qr_display_by_id(uuid) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_live_qr_display_by_id(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_live_qr_display_by_id(uuid) TO tappa_app;

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_column_privilege('tappa_app', 'public.live_qr_displays', 'session_hash', 'SELECT')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.live_qr_displays', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.live_qr_displays', 'TRUNCATE')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.live_qr_displays'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND a.attname NOT IN ('pairing_code_hash', 'paired_at', 'session_hash', 'revoked_at')
                     AND pg_catalog.has_column_privilege('tappa_app', a.attrelid, a.attnum, 'UPDATE'))
       OR pg_catalog.has_column_privilege('tappa_app', 'public.live_qr_displays', 'created_at', 'INSERT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.live_qr_displays', 'paired_at', 'INSERT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.live_qr_displays', 'session_hash', 'INSERT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.live_qr_displays', 'revoked_at', 'INSERT')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.live_qr_displays_state_is_one_way()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('public', 'public.resolve_live_qr_display_by_pairing_code_hash(text)', 'EXECUTE')
       OR pg_catalog.has_function_privilege('public', 'public.resolve_live_qr_display_by_session_hash(text)', 'EXECUTE')
       OR pg_catalog.has_function_privilege('public', 'public.resolve_live_qr_display_by_id(uuid)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.resolve_live_qr_display_by_pairing_code_hash(text)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.resolve_live_qr_display_by_session_hash(text)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.resolve_live_qr_display_by_id(uuid)', 'EXECUTE')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.live_qr_displays'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND a.attname NOT IN ('id', 'tenant_id', 'location_id', 'pairing_code_hash', 'pairing_expires_at',
                                           'session_hash', 'revoked_at')
                     AND pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'SELECT'))
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.locations'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND a.attname NOT IN ('id', 'tenant_id', 'active')
                     AND pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'SELECT')) THEN
        RAISE EXCEPTION 'migration 00111 (M16-19): tappa_app must not read a screen''s session digest, delete or truncate a screen, update anything but the four state columns, write created_at or a state column at INSERT, or run the trigger function; it must EXECUTE the three resolvers where PUBLIC does not; and tappa_resolver must read no column of live_qr_displays but the resolutions'' seven and none of locations but id, tenant_id and active -- through PUBLIC or a role membership one of these does not hold. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- 🔴 A DOWN THAT WOULD ERASE A SCREEN REFUSES (the 00068 / 00097 / 00108 precedent). A screen row is
-- which venue screen stood at which location, when it was paired and when revoked -- the evidence
-- behind every live QR tap M16-21 records -- and no other row holds it. So while the table holds a
-- row this Down stops and changes nothing; on a database with none it takes the table, the four
-- functions and tappa_resolver's three locations columns away. After the first screen the way back is
-- the binary, not the schema: the previous binary neither reads nor writes this table.
--
-- The table is LOCKED BEFORE IT IS LOOKED AT: between an unlocked EXISTS and the DROP a pairing
-- could commit and the DROP would erase it. The DROP also takes ACCESS EXCLUSIVE on tenants and
-- locations (the two keys' triggers there), so all three are taken HERE, in one statement, child
-- first: the Down either holds every lock it will need before it reads or changes anything, or gives
-- up with 55P03 having done nothing.
SET LOCAL lock_timeout = '1s';

LOCK TABLE public.live_qr_displays, public.locations, public.tenants IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.live_qr_displays) THEN
        RAISE EXCEPTION 'migration 00111 Down: live_qr_displays holds rows (a venue screen paired to a location); dropping them would erase which screen stood where behind every live QR tap. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP FUNCTION public.resolve_live_qr_display_by_id(uuid);
DROP FUNCTION public.resolve_live_qr_display_by_session_hash(text);
DROP FUNCTION public.resolve_live_qr_display_by_pairing_code_hash(text);
DROP TABLE public.live_qr_displays;
DROP FUNCTION public.live_qr_displays_state_is_one_way();
REVOKE SELECT (id, tenant_id, active) ON public.locations FROM tappa_resolver;
