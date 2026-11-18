-- 00108 -- M16-03 (ADR 0045 §2.3): app_devices, app_request_nonces and the device resolver,
-- resolve_app_device_by_id.
--
-- NORMATIVE SOURCES: docs/adr/0045-istege-bagli-native-uygulama.md §2.3 (the device key, no
-- bearer token, the single-use nonce, resolution before the tenant is known); docs/plan/m16-mobil.md
-- M16-03 (the contract); docs/adr/0002-tenant-baglami-ve-rls.md md.7 (a resolver's five
-- constraints); docs/adr/0028-dis-api-anahtar-akis-webhook.md §1 (the `arwd` rule: REVOKE ALL from
-- tappa_app, then the verbs and the columns it needs; UPDATE always by column; every new function
-- REVOKEd from PUBLIC; a trigger function EXECUTE in its owner alone, asserting TG_RELID);
-- 00099 (the one-way revocation trigger this file copies twice).
--
-- APP_DEVICES. One row per phone an employee ACTIVATED in the native app. The private key never
-- leaves the phone (Secure Enclave / Android Keystore); the server keeps the public half only.
--   * A ROW IS BORN ACTIVE, AT THE END OF ACTIVATION. The app's activation has two steps (ADR 0045
--     §2.3): `bind` records consent and the key on the INVITE -- the browser's ADR 0026 shape,
--     employee_invites.consent_binding_hash -- and does not create a device; `complete` consumes the
--     invite and creates this row in the same transaction (M16-07). So "a bound but not active
--     device" is not a state of this table: it is an invite carrying a key, and a device id that
--     does not exist yet cannot be issued a nonce (IssueAppRequestNonce reads this row).
--   * public_key: the RAW uncompressed P-256 point, 65 bytes, 0x04 || X || Y (SEC 1 §2.3.3), not
--     SPKI. Why: it is ONE canonical byte string per key -- SPKI is a DER wrapper that may name the
--     curve by OID or spell its parameters out, so two encodings of one key would hash to two
--     digests and the UNIQUE below would stop being a property of the KEY. iOS's
--     SecKeyCopyExternalRepresentation emits exactly this form; Android's getEncoded() SPKI ends
--     in it (the app strips the 26-byte prefix, M16-12; iOS M16-16); Go parses it with
--     crypto/ecdsa.ParseUncompressedPublicKey. The CHECK pins the length and the prefix byte; that
--     the point is ON the curve is internal/appdevice's check (M16-05) -- SQL cannot do the
--     arithmetic. It is a PUBLIC key (§4.7 does not reach it), but it is not logged (M16-25's list)
--     and tappa_app cannot SELECT it: only the resolver hands it to the verifier.
--   * key_digest: hex SHA-256 of public_key, a GENERATED column -- the database derives it, so
--     no caller can store a digest that is not its key's. Globally UNIQUE: one key, one device
--     row, across every tenant. A phone that activates for a second business generates a second
--     key pair (M16-07); re-registering a revoked device's key is refused (23505), so a stolen key
--     cannot be brought back under a new row. (The card calls it the key's "parmak izi"; the
--     column is key_digest because redline-check R1 reads the English word for it as a §4.1
--     trace -- the rule is kept blunt and the name reworded, this repository's direction.)
--   * device_label: a COARSE label ("iPhone"), nullable -- sessions.device_info's promise (00003):
--     never derived from probing the device (§4.1).
--   * revoked_at is ONE WAY in the database (00099's sessions shape): a stamp never changes, a first
--     stamp is not back- or forward-dated. A revoked device is §5 row 3's twin (401, re-activation,
--     no record); deactivation revokes NOTHING (the user's decision of 2026-08-07) -- the resolver
--     carries revoked_at and decides nothing.
--   * tappa_app never DELETEs a device: revocation is the stamp, and the trail of which phone was
--     whose stays (the panel's device list, M16-26).
--
-- APP_REQUEST_NONCES. A single-use nonce the server issues to an ACTIVE device; every
-- state-changing app request signs a canonical text that includes it (ADR 0045 §2.3). The nonce
-- itself is never stored: nonce_hash is its hex SHA-256 (a 256-bit server-random value needs no
-- keyed hash to resist guessing; internal/appdevice derives it, M16-05).
--   * Consumption is ONE statement, §4.4's shape (db/queries/appdevices.sql
--     ConsumeAppRequestNonce): UPDATE ... SET consumed_at = now() WHERE tenant, device, hash,
--     consumed_at IS NULL AND expires_at > now() RETURNING. 0 rows -> reject. Two racing
--     consumptions of one nonce: the second waits for the first's row lock, re-reads
--     consumed_at IS NULL against the committed row, and matches nothing.
--   * consumed_at is ONE WAY too (its own trigger): un-consuming a nonce would re-open it to a
--     replay, and tappa_app holds UPDATE on that column.
--   * A REVOCATION KILLS THE DEVICE'S NONCES (M16-03 security review): RevokeAppDevice and
--     RevokeAppDevicesForEmployee delete them in the revoking statement, and ConsumeAppRequestNonce
--     and IssueAppRequestNonce re-read the device row FOR SHARE, so neither passes a revocation that
--     committed first or was in flight. This needs no grant beyond the ones below: DELETE on the
--     nonces; SELECT (id, tenant_id, revoked_at) and UPDATE on a column of app_devices, which FOR
--     SHARE requires.
--   * Lifetime: at most 5 minutes from issue (CHECK). created_at is not writable by tappa_app, so it
--     is the database's now() and the bound is the database's clock.
--   * CLEANUP DECISION: dead nonces (consumed, or expired) are DELETED, by the issuing path, per
--     device (DeleteDeadAppRequestNonces, run before IssueAppRequestNonce). Deleting is fail-closed:
--     a consumption needs a live row to exist, so a missing row is a reject, never an accept. A
--     nonce carries no evidence -- the tap's record is `transactions` -- so nothing of §4.3/§4.6 is
--     lost, and redline-check R3 (transactions, punch_events) does not and need not cover this
--     table. A device that stops asking keeps its last few rows; M16-07 caps how many live ones it
--     may hold.
--
-- THE RESOLVER (ADR 0002 md.7's five, the 00003 / 00051 shape). A signed request arrives with a
-- device id and no tenant; FORCE RLS hides every app_devices row from a context-less tappa_app.
-- resolve_app_device_by_id: owner tappa_resolver (NOLOGIN, BYPASSRLS, NOSUPERUSER), SECURITY
-- DEFINER, STABLE, search_path = pg_catalog, pg_temp, the table schema-qualified; input the device
-- id alone; at most ONE row (the primary key); a fixed result (id, tenant_id, employee_id,
-- public_key, revoked_at); tappa_resolver reads exactly those five columns of app_devices and
-- nothing of app_request_nonces; EXECUTE to tappa_app alone. It carries, never applies: revoked_at
-- is returned, not filtered. Everything after the resolution -- the nonce's consumption included --
-- runs under SET LOCAL app.tenant_id as an ordinary query with its own tenant_id filter.
--
-- 🔴 lock_timeout 1 s, the FIRST statement of the Up and of the Down (00054's rule): the new keys
-- to tenants and employees take SHARE ROW EXCLUSIVE on both (the FK triggers), which an employee
-- write waits behind. Behind a long writer the file gives up with 55P03 instead of queueing it.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.sessions_revocation_is_one_way()') IS NULL THEN
        RAISE EXCEPTION 'migration 00108 (M16-03) needs migration 00099 (the one-way session revocation it copies). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF pg_catalog.to_regclass('public.app_devices') IS NOT NULL
       OR pg_catalog.to_regclass('public.app_request_nonces') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                   WHERE p.proname IN ('resolve_app_device_by_id', 'app_devices_revocation_is_one_way',
                                       'app_request_nonces_consumption_is_one_way')) THEN
        RAISE EXCEPTION 'migration 00108 (M16-03) creates app_devices, app_request_nonces and their three functions, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                    WHERE rolname = 'tappa_resolver' AND rolbypassrls AND NOT rolsuper AND NOT rolcanlogin) THEN
        RAISE EXCEPTION 'migration 00108 (M16-03): role tappa_resolver must exist, NOLOGIN, NOSUPERUSER and BYPASSRLS (scripts/db-init/01-roles.sql). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

CREATE TABLE app_devices (
    id              uuid        NOT NULL DEFAULT gen_random_uuid(),
    tenant_id       uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    employee_id     uuid        NOT NULL,
    platform        text        NOT NULL,
    public_key      bytea       NOT NULL,
    key_digest text        GENERATED ALWAYS AS (encode(sha256(public_key), 'hex')) STORED,
    device_label    text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_used_at    timestamptz,
    revoked_at      timestamptz,
    CONSTRAINT app_devices_pkey PRIMARY KEY (id),
    -- The nonces' composite key to (id, tenant_id): a nonce cannot name another tenant's device.
    CONSTRAINT app_devices_id_tenant_key UNIQUE (id, tenant_id),
    -- GLOBAL, not per tenant: one key, one device row (see the header).
    CONSTRAINT app_devices_key_digest_key UNIQUE (key_digest),
    CONSTRAINT app_devices_employee_fk FOREIGN KEY (employee_id, tenant_id)
        REFERENCES employees (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT app_devices_platform_check CHECK (platform IN ('ios', 'android')),
    CONSTRAINT app_devices_public_key_shape CHECK (octet_length(public_key) = 65 AND get_byte(public_key, 0) = 4),
    -- Coarse: one to 64 characters, no control character, no leading or trailing blank.
    CONSTRAINT app_devices_device_label_shape CHECK (
        device_label IS NULL
        OR (char_length(device_label) BETWEEN 1 AND 64 AND device_label = btrim(device_label)
            AND device_label !~ '[[:cntrl:]]')),
    CONSTRAINT app_devices_revoked_after_created CHECK (revoked_at IS NULL OR revoked_at >= created_at),
    CONSTRAINT app_devices_used_after_created CHECK (last_used_at IS NULL OR last_used_at >= created_at)
);

-- tenant_id leads (CLAUDE.md §6): RLS, the panel's per-employee list (M16-26) and the bulk sign-out
-- (RevokeAppDevicesForEmployee); it also serves the employee key's RESTRICT check.
CREATE INDEX app_devices_tenant_employee_idx ON app_devices (tenant_id, employee_id);

ALTER TABLE app_devices ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_devices FORCE ROW LEVEL SECURITY;
CREATE POLICY app_devices_tenant_isolation ON app_devices
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TABLE app_request_nonces (
    id          uuid        NOT NULL DEFAULT gen_random_uuid(),
    tenant_id   uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    device_id   uuid        NOT NULL,
    nonce_hash  text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL,
    consumed_at timestamptz,
    CONSTRAINT app_request_nonces_pkey PRIMARY KEY (id),
    -- GLOBAL: a hash names at most one nonce, so a consumption matches at most one row.
    CONSTRAINT app_request_nonces_nonce_hash_key UNIQUE (nonce_hash),
    CONSTRAINT app_request_nonces_device_fk FOREIGN KEY (device_id, tenant_id)
        REFERENCES app_devices (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT app_request_nonces_nonce_hash_shape CHECK (nonce_hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT app_request_nonces_lifetime CHECK (
        expires_at > created_at AND expires_at <= created_at + interval '5 minutes'),
    CONSTRAINT app_request_nonces_consumed_while_live CHECK (
        consumed_at IS NULL OR (consumed_at >= created_at AND consumed_at < expires_at))
);

-- tenant_id leads (CLAUDE.md §6): RLS, the per-device live count and the per-device sweep.
CREATE INDEX app_request_nonces_tenant_device_idx ON app_request_nonces (tenant_id, device_id);

ALTER TABLE app_request_nonces ENABLE ROW LEVEL SECURITY;
ALTER TABLE app_request_nonces FORCE ROW LEVEL SECURITY;
CREATE POLICY app_request_nonces_tenant_isolation ON app_request_nonces
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- --- one-way stamps (00099's shape, one function per table) ---------------------------------------

-- +goose StatementBegin
CREATE FUNCTION public.app_devices_revocation_is_one_way()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'UPDATE'
       OR TG_RELID <> 'public.app_devices'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'app_devices_revocation_is_one_way: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF OLD.revoked_at IS NOT NULL THEN
        RAISE EXCEPTION 'app device revocation is one way: revoked_at may go from NULL to a timestamp, never back and never to another instant'
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF NEW.revoked_at < pg_catalog.now() OR NEW.revoked_at > pg_catalog.clock_timestamp() THEN
        RAISE EXCEPTION 'app device revocation is stamped when it happens: revoked_at may not be back-dated or set in the future'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.app_devices_revocation_is_one_way() FROM PUBLIC;

CREATE TRIGGER app_devices_revocation_one_way
    BEFORE UPDATE ON public.app_devices
    FOR EACH ROW
    WHEN (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at)
    EXECUTE FUNCTION public.app_devices_revocation_is_one_way();

-- +goose StatementBegin
CREATE FUNCTION public.app_request_nonces_consumption_is_one_way()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'UPDATE'
       OR TG_RELID <> 'public.app_request_nonces'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'app_request_nonces_consumption_is_one_way: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF OLD.consumed_at IS NOT NULL THEN
        RAISE EXCEPTION 'nonce consumption is one way: consumed_at may go from NULL to a timestamp, never back and never to another instant'
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF NEW.consumed_at < pg_catalog.now() OR NEW.consumed_at > pg_catalog.clock_timestamp() THEN
        RAISE EXCEPTION 'nonce consumption is stamped when it happens: consumed_at may not be back-dated or set in the future'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.app_request_nonces_consumption_is_one_way() FROM PUBLIC;

CREATE TRIGGER app_request_nonces_consumption_one_way
    BEFORE UPDATE ON public.app_request_nonces
    FOR EACH ROW
    WHEN (NEW.consumed_at IS DISTINCT FROM OLD.consumed_at)
    EXECUTE FUNCTION public.app_request_nonces_consumption_is_one_way();

-- --- tappa_app (ADR 0028 §1's `arwd` rule) --------------------------------------------------------
-- app_devices: reads every column but public_key and key_digest (only the resolver needs the
-- key; nothing in the product reads a digest); inserts a device by naming its tenant, person,
-- platform, key and label (id, key_digest and created_at are the database's); updates
-- last_used_at and revoked_at alone; never deletes.
REVOKE ALL ON app_devices FROM tappa_app;
GRANT SELECT (id, tenant_id, employee_id, platform, device_label, created_at, last_used_at, revoked_at)
    ON app_devices TO tappa_app;
GRANT INSERT (tenant_id, employee_id, platform, public_key, device_label) ON app_devices TO tappa_app;
GRANT UPDATE (last_used_at, revoked_at) ON app_devices TO tappa_app;

-- app_request_nonces: reads every column (the consumption's WHERE needs nonce_hash -- no query
-- RETURNS it); inserts a nonce by naming its tenant, device, hash and expiry (created_at is the
-- database's now(), which is what the lifetime CHECK bounds); updates consumed_at alone; deletes
-- (the dead-nonce sweep -- fail-closed, see the header).
REVOKE ALL ON app_request_nonces FROM tappa_app;
GRANT SELECT (id, tenant_id, device_id, nonce_hash, created_at, expires_at, consumed_at)
    ON app_request_nonces TO tappa_app;
GRANT INSERT (tenant_id, device_id, nonce_hash, expires_at) ON app_request_nonces TO tappa_app;
GRANT UPDATE (consumed_at) ON app_request_nonces TO tappa_app;
GRANT DELETE ON app_request_nonces TO tappa_app;

-- --- the resolver ---------------------------------------------------------------------------------
-- tappa_resolver: the resolution's five columns of app_devices; platform, label, digest and
-- the use stamps stay unreadable, and app_request_nonces is not reached at all.
GRANT SELECT (id, tenant_id, employee_id, public_key, revoked_at) ON app_devices TO tappa_resolver;

-- +goose StatementBegin
CREATE FUNCTION public.resolve_app_device_by_id(p_device_id uuid)
    RETURNS TABLE (
        id          uuid,
        tenant_id   uuid,
        employee_id uuid,
        public_key  bytea,
        revoked_at  timestamptz
    )
    LANGUAGE sql
    STABLE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    -- <= 1 row: id is the primary key. A fixed column list. revoked_at is returned and not
    -- filtered (the decision is the caller's, §5 row 3's twin).
    SELECT d.id, d.tenant_id, d.employee_id, d.public_key, d.revoked_at
      FROM public.app_devices AS d
     WHERE d.id = p_device_id;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.resolve_app_device_by_id(uuid) OWNER TO tappa_resolver;
REVOKE ALL ON FUNCTION public.resolve_app_device_by_id(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.resolve_app_device_by_id(uuid) TO tappa_app;

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_column_privilege('tappa_app', 'public.app_devices', 'public_key', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.app_devices', 'key_digest', 'SELECT')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.app_devices', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.app_devices', 'TRUNCATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.app_request_nonces', 'TRUNCATE')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.app_devices'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND a.attname NOT IN ('last_used_at', 'revoked_at')
                     AND pg_catalog.has_column_privilege('tappa_app', a.attrelid, a.attnum, 'UPDATE'))
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.app_request_nonces'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND a.attname <> 'consumed_at'
                     AND pg_catalog.has_column_privilege('tappa_app', a.attrelid, a.attnum, 'UPDATE'))
       OR pg_catalog.has_column_privilege('tappa_app', 'public.app_request_nonces', 'created_at', 'INSERT')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.app_devices_revocation_is_one_way()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.app_request_nonces_consumption_is_one_way()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('public', 'public.resolve_app_device_by_id(uuid)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.resolve_app_device_by_id(uuid)', 'EXECUTE')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.app_devices'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND a.attname NOT IN ('id', 'tenant_id', 'employee_id', 'public_key', 'revoked_at')
                     AND pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'SELECT'))
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.app_request_nonces'::pg_catalog.regclass AND a.attnum > 0 AND NOT a.attisdropped
                     AND pg_catalog.has_column_privilege('tappa_resolver', a.attrelid, a.attnum, 'SELECT')) THEN
        RAISE EXCEPTION 'migration 00108 (M16-03): tappa_app must not read a device key or digest, delete or truncate a device, truncate the nonces, update anything but last_used_at/revoked_at and consumed_at, write a nonce''s created_at or run a trigger function, must EXECUTE the resolver where PUBLIC does not, and tappa_resolver must read no column of app_devices but the resolution''s five and none of app_request_nonces -- through PUBLIC or a role membership one of these does not hold. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- 🔴 A DOWN THAT WOULD ERASE A DEVICE REFUSES (the 00068 / 00097 precedent). A device row is an
-- employee's activation -- which phone, whose, since when, revoked or not -- and no other row holds
-- it; dropping it would sign every app user out AND lose the trail the panel's device list reads.
-- So while either table holds a row this Down stops and changes nothing; on a database with none it
-- takes the tables and the three functions away. After the first activation the way back is the
-- binary, not the schema: the previous binary neither reads nor writes these tables.
--
-- The tables are LOCKED BEFORE THEY ARE LOOKED AT: between an unlocked EXISTS and the DROP an
-- activation could commit and the DROP would erase it. The DROPs also take ACCESS EXCLUSIVE on
-- tenants and employees (the two keys' triggers there -- measured by the identity walks' lock check),
-- so all four are taken HERE, in one statement, child tables first: the Down either holds every lock
-- it will need before it reads or changes anything, or gives up with 55P03 having done nothing.
SET LOCAL lock_timeout = '1s';

LOCK TABLE public.app_request_nonces, public.app_devices, public.employees, public.tenants IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.app_devices) OR EXISTS (SELECT 1 FROM public.app_request_nonces) THEN
        RAISE EXCEPTION 'migration 00108 Down: app_devices or app_request_nonces holds rows (an activated phone, a nonce issued to one); dropping them would sign every app user out and erase which phone was whose. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP FUNCTION public.resolve_app_device_by_id(uuid);
DROP TABLE public.app_request_nonces;
DROP TABLE public.app_devices;
DROP FUNCTION public.app_request_nonces_consumption_is_one_way();
DROP FUNCTION public.app_devices_revocation_is_one_way();
