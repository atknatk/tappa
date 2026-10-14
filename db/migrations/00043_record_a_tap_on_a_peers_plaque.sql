-- 00043 -- M11 API-1b: a tap on a peer's plaque. A member of a holding (the EMPLOYER, the
-- session's tenant) records its employee's tap on another member's plaque (the SITE) in its own
-- transactions, with the site named in three new columns; the three callable holding functions
-- of ADR 0027 §2's closed list that the tap path needs (holding_tap_site,
-- holding_advance_tag_counter, holding_logo); and the BEFORE INSERT membership gate of a row
-- that names a site (holding_site_gate).
--
-- NORMATIVE SOURCES: docs/adr/0027-holding-ve-capraz-tenant-dokunusu.md §2 (the signature
-- table, verbatim; the function contract; holding_peer as the one predicate -- its body is
-- 00041's and is NOT touched here), §3 (tappa_holdingdefiner's verb x column matrix: the rows
-- these functions need, each column re-earned -- TestHoldingDefiner_EveryGrantIsUsed), §4 (the
-- site columns and the CHECK by channel, K-30), §5 and §6 (the tap path and the channel x time
-- model, K-29: the strict advance, the two tolerant arms of the gate, FOR SHARE before deciding,
-- statement_timestamp()), §10 (the brand: the logo only for a business VIES verified) and §12;
-- docs/plan/m11-entegrasyon-api.md §4.0 (REVOKE ALL ON FUNCTION ... FROM PUBLIC; EXECUTE on a
-- callable holding_* to tappa_app alone; a trigger function's EXECUTE in its owner alone and a
-- TG_RELID assert); CLAUDE.md §4.3, §4.4, §4.5, §4.6, §6.
--
-- WHAT IT DOES, IN ORDER:
--   0. the precondition: 00041's role and predicate in place, none of this file's objects, and
--      tappa_holdingdefiner reaching nothing but 00041's holding_members row;
--   1. transactions.site_tenant_id, site_location_id, site_tag_uid, their two composite foreign
--      keys and the four CHECKs of K-30 (NOT VALID -- 00044 validates them) and a partial index;
--   2. tappa_holdingdefiner's grants on tags, locations, tenants and tenant_branding;
--   3. holding_tap_site, holding_advance_tag_counter, holding_logo;
--   4. holding_site_gate and its trigger on transactions;
--   5. the postcondition.
--
-- §4.5 -- CROSSED ON PURPOSE, IN THE PLACE ADR 0027 ALLOWS. The three functions and the gate
-- belong to tappa_holdingdefiner (BYPASSRLS): inside them there is no row level security, and the
-- one barrier is the explicit filter "the key's tenant is a peer of the caller's tenant" --
-- holding_peer, once per call -- plus the explicit tenant equality on every join. The caller's
-- tenant is app.tenant_id, never an argument; an unknown key, a key of a tenant that is not a
-- peer and a call with no tenant context are one answer (0 rows; 'not_peer' for the advance).
--
-- 🔴 §4.4: holding_advance_tag_counter is the SECOND copy of the replay statement
-- (db/queries/tags.sql's AdvanceTagCounter): one statement, `prev.old_ctr < p_ctr` inside it, the
-- row locked FOR UPDATE in the same statement, and the peer condition in prev AND in the UPDATE's
-- WHERE, from one materialised CTE (the membership is asked once: clock_timestamp() is not read
-- twice). TestHoldingAdvance_ConcurrentSameCounterExactlyOneWins races it against the
-- single-tenant statement.
--
-- 🔴 §4.6: the gate refuses a row only where ADR 0027 §6 says the membership must be live at the
-- INSERT (QR, manual, an NFC row whose counter was not advanced); an advanced NFC tap and every
-- refusal on a tap channel are written within 60 seconds after left_at (the tolerant arms), so a
-- membership that ends between the advance and the write does not cost the record.
--
-- CLOCKS: the three callables ask holding_peer 'strict' (clock_timestamp()); the gate asks
-- 'strict_stmt' / 'tolerant_stmt' (statement_timestamp() -- ADR 0027 §6: one statement, one clock)
-- after taking 'lock' (FOR SHARE on the two memberships, which queues behind an uncommitted
-- op_remove_holding_member and re-reads its left_at).
--
-- SECRETS: none. static_ips and the GPS pair leave holding_tap_site as server-side decision
-- inputs only (K-39); every RAISE carries a constant message.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'tappa_holdingdefiner')
       OR pg_catalog.to_regprocedure('public.holding_peer(uuid, uuid, text)') IS NULL
       OR pg_catalog.to_regclass('public.holding_members') IS NULL THEN
        RAISE EXCEPTION 'migration 00043 (M11 API-1b) needs migration 00041 (tappa_holdingdefiner, holding_members, holding_peer). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                WHERE p.proname IN ('holding_tap_site', 'holding_advance_tag_counter', 'holding_logo', 'holding_site_gate'))
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_trigger g
                   WHERE g.tgrelid = 'public.transactions'::regclass AND g.tgname = 'transactions_holding_site_gate')
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.transactions'::regclass AND NOT a.attisdropped
                     AND a.attname IN ('site_tenant_id', 'site_location_id', 'site_tag_uid')) THEN
        RAISE EXCEPTION 'migration 00043 (M11 API-1b) creates the site columns, the three tap functions and the site gate, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- The grants below are column lists, and a column grant cannot narrow a wider privilege
    -- some other hand gave (00024 measured it): the role must reach nothing but 00041's row.
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_class c
                WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
                  AND c.relname <> 'holding_members'
                  AND (pg_catalog.has_any_column_privilege('tappa_holdingdefiner', c.oid, 'SELECT')
                       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', c.oid, 'INSERT')
                       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', c.oid, 'UPDATE')
                       OR pg_catalog.has_table_privilege('tappa_holdingdefiner', c.oid, 'DELETE')
                       OR pg_catalog.has_table_privilege('tappa_holdingdefiner', c.oid, 'TRUNCATE'))) THEN
        RAISE EXCEPTION 'migration 00043 (M11 API-1b) gives tappa_holdingdefiner column privileges, and the role already reaches a relation in public that no migration gave it. A column grant cannot narrow that. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The site of a tap on a peer's plaque (ADR 0027 §4, K-30).
-- ---------------------------------------------------------------------------
-- The row is the EMPLOYER's (tenant_id = the session's tenant; transactions' row level security
-- is unchanged) and names the site in three nullable columns, written at INSERT and never after
-- (§4.3 -- transactions takes no UPDATE). The CHECKs, by channel:
--   * a tap (nfc, qr) names the site tenant and the plaque; the site's location MAY be NULL -- the
--     refusal of a plaque with no wall (unassigned) is written too (§4.6);
--   * a manual record names the site tenant and its location, and no plaque;
--   * always: the site is another tenant, and a row naming a site names no local location and no
--     local plaque (the local and the site columns never share a row);
--   * and a row with no site tenant names no site location and no site plaque: the foreign keys
--     are MATCH SIMPLE, so without this a row with site_tenant_id NULL could carry another
--     tenant's location id past every key (ADR 0027 §4, 7th round).
-- The composite keys tie the location and the plaque to THAT tenant; RESTRICT is a location
-- delete's last belt (§9). Whether the site is a PEER of the employer is the gate's (part 4).
--
-- 🔴 NOT VALID, AND 00044 VALIDATES (ADR 0027 §4: "eşik 2 sn, aşılırsa CHECK/FK NOT VALID + ayrı
-- VALIDATE"). Measured on a throwaway PostgreSQL 17.10 with production-shaped rows (582 bytes on
-- average): validating the four CHECKs and the two keys inside this statement held ACCESS
-- EXCLUSIVE on transactions for 0.68-0.90 s at 688 000 rows and 2.06-3.46 s at 2 000 000 -- past
-- the 2 s threshold well inside the table's foreseeable size. NOT VALID makes this statement
-- O(1) (3.5 ms at 2 000 000), and it gives up nothing: every existing row has the three new
-- columns NULL, which all six admit, and a NOT VALID constraint is enforced on every row written
-- from here on. 00044 validates them in its own transaction under SHARE UPDATE EXCLUSIVE, which
-- does not stop a tap being written. TestHoldingMigration_AlterWithinTwoSeconds pins both halves.
ALTER TABLE transactions
    ADD COLUMN site_tenant_id   uuid,
    ADD COLUMN site_location_id uuid,
    ADD COLUMN site_tag_uid     character(14),
    ADD CONSTRAINT transactions_site_location_fk FOREIGN KEY (site_location_id, site_tenant_id)
        REFERENCES locations (id, tenant_id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT transactions_site_tag_fk FOREIGN KEY (site_tag_uid, site_tenant_id)
        REFERENCES tags (uid, tenant_id) ON DELETE RESTRICT NOT VALID,
    ADD CONSTRAINT transactions_site_needs_its_tenant
        CHECK (site_tenant_id IS NOT NULL OR (site_location_id IS NULL AND site_tag_uid IS NULL)) NOT VALID,
    ADD CONSTRAINT transactions_site_is_another_tenant
        CHECK (site_tenant_id IS NULL OR site_tenant_id <> tenant_id) NOT VALID,
    ADD CONSTRAINT transactions_site_excludes_the_local_venue
        CHECK (site_tenant_id IS NULL OR (location_id IS NULL AND tag_uid IS NULL)) NOT VALID,
    ADD CONSTRAINT transactions_site_by_channel
        CHECK (site_tenant_id IS NULL
               OR (channel IN ('nfc', 'qr') AND site_tag_uid IS NOT NULL)
               OR (channel = 'manual' AND site_location_id IS NOT NULL AND site_tag_uid IS NULL)) NOT VALID;

-- The partial index (a location delete's RESTRICT check, and API-1c's readers). Its build is the
-- one scan this file makes under the lock above: it finds no row to index (every site column is
-- NULL), and it cost 0.12 s at 688 000 rows and 0.35 s at 2 000 000 (measured as above).
CREATE INDEX transactions_site_idx ON transactions (site_tenant_id, site_location_id)
    WHERE site_tenant_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- 2. tappa_holdingdefiner's grants (ADR 0027 §3, K-34) -- the matrix rows the three functions and
-- the gate read, each column one of them reads (TestHoldingDefiner_EveryGrantIsUsed revokes them
-- one at a time). The matrix is a ceiling: tenants.api_enabled_at, employees, transactions and the
-- written tables are API-1c's and API-2's to earn.
-- ---------------------------------------------------------------------------
-- tags: the plaque, its wall and status; last_ctr for the advance (whose FOR UPDATE this UPDATE
-- grant also meets) and for the gate's "the counter was advanced" arm.
GRANT SELECT (uid, tenant_id, location_id, status, last_ctr) ON tags TO tappa_holdingdefiner;
GRANT UPDATE (last_ctr) ON tags TO tappa_holdingdefiner;
-- locations: the venue and its evidence (static_ips and GPS leave holding_tap_site alone -- K-39).
GRANT SELECT (id, tenant_id, name, external_ref, static_ips, gps_lat, gps_lng, shift_start, shift_end, overnight)
    ON locations TO tappa_holdingdefiner;
-- tenants: the site's clock and code, the VIES gate, and its name as the logo's alt.
GRANT SELECT (id, name, timezone, vat_verified, api_code) ON tenants TO tappa_holdingdefiner;
-- tenant_branding: the brand meta, and the logo bytes (holding_logo alone reads `logo`).
GRANT SELECT (tenant_id, accent, logo, logo_sha256, logo_mime, logo_width, logo_height)
    ON tenant_branding TO tappa_holdingdefiner;

-- ===========================================================================
-- 3. The three callable functions (ADR 0027 §2's signature table, verbatim).
-- ===========================================================================
-- Each: SECURITY DEFINER owned by tappa_holdingdefiner, search_path pinned, every table
-- public.-qualified, no dynamic SQL, fixed result columns, the caller's tenant from app.tenant_id,
-- the key the server resolved as the one argument, holding_peer once (a materialised CTE), and the
-- tenant equality written on every join. VOLATILE: holding_peer is, and the advance writes.

-- holding_tap_site -- the peer's plaque: its tenant and status; its venue and that venue's
-- evidence when it has a wall; the site's clock and code; the brand meta -- the accent always, the
-- logo (digest, type, box) and the business's name as its alt ONLY for a business VIES verified
-- (ADR 0027 §10; ADR 0023's WL-13 gate). The tap page (GET /t) and checkin's gather read it.
-- +goose StatementBegin
CREATE FUNCTION public.holding_tap_site(p_uid character)
    RETURNS TABLE(site_tenant_id uuid, tag_status text, location_id uuid, location_name text,
                  location_external_ref text, static_ips cidr[], gps_lat numeric, gps_lng numeric,
                  shift_start time, shift_end time, overnight boolean, timezone text, api_code text,
                  accent character, logo_sha256 text, logo_mime text, logo_width integer,
                  logo_height integer, business_name text)
    LANGUAGE sql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    WITH caller AS MATERIALIZED (
        SELECT NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid AS id
    ), plaque AS MATERIALIZED (
        SELECT t.uid, t.tenant_id, t.location_id, t.status
          FROM public.tags AS t
         WHERE t.uid = p_uid
    ), peer AS MATERIALIZED (
        SELECT p.uid, p.tenant_id, p.location_id, p.status
          FROM plaque AS p, caller AS c
         WHERE public.holding_peer(c.id, p.tenant_id, 'strict')
    )
    SELECT p.tenant_id, p.status,
           l.id, l.name, l.external_ref, l.static_ips, l.gps_lat, l.gps_lng,
           l.shift_start, l.shift_end, l.overnight,
           s.timezone, s.api_code, b.accent,
           CASE WHEN s.vat_verified IS TRUE AND b.logo_sha256 IS NOT NULL THEN b.logo_sha256 END,
           CASE WHEN s.vat_verified IS TRUE AND b.logo_sha256 IS NOT NULL THEN b.logo_mime END,
           CASE WHEN s.vat_verified IS TRUE AND b.logo_sha256 IS NOT NULL THEN b.logo_width END,
           CASE WHEN s.vat_verified IS TRUE AND b.logo_sha256 IS NOT NULL THEN b.logo_height END,
           CASE WHEN s.vat_verified IS TRUE AND b.logo_sha256 IS NOT NULL THEN s.name END
      FROM peer AS p
      JOIN public.tenants AS s ON s.id = p.tenant_id
      LEFT JOIN public.locations AS l ON l.id = p.location_id AND l.tenant_id = p.tenant_id
      LEFT JOIN public.tenant_branding AS b ON b.tenant_id = p.tenant_id
     LIMIT 1
$$;
-- +goose StatementEnd
ALTER FUNCTION public.holding_tap_site(character) OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_tap_site(character) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.holding_tap_site(character) TO tappa_app;

-- holding_advance_tag_counter -- §4.4's replay statement for a peer's plaque, ONE statement:
--   not a peer (or no such plaque, or no tenant context) ... ('not_peer', NULL)  -- nothing moved
--   a peer, the plaque not active ........................ ('peer_inactive', its status)
--   a peer, active, the counter did not go up ............ ('replay', 'active')
--   a peer, active, last_ctr moved to p_ctr .............. ('advanced', 'active')
-- The status is the plaque's AT THE LOCK (prev's FOR UPDATE re-reads the row under READ
-- COMMITTED), so the decision sees the plaque as it is now, not as the resolver read it (ADR 0027
-- §5). The peer condition is in prev AND in the UPDATE's WHERE, from the one `peer` CTE.
-- 'not_peer' carries no status: an unknown uid and another holding's plaque answer alike.
-- +goose StatementBegin
CREATE FUNCTION public.holding_advance_tag_counter(p_uid character, p_ctr integer)
    RETURNS TABLE(outcome text, tag_status text)
    LANGUAGE sql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    WITH caller AS MATERIALIZED (
        SELECT NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid AS id
    ), plaque AS MATERIALIZED (
        SELECT t.uid, t.tenant_id
          FROM public.tags AS t
         WHERE t.uid = p_uid
    ), peer AS MATERIALIZED (
        SELECT p.uid, p.tenant_id
          FROM plaque AS p, caller AS c
         WHERE public.holding_peer(c.id, p.tenant_id, 'strict')
    ), prev AS (
        SELECT g.uid AS old_uid, g.tenant_id AS old_tenant, g.last_ctr AS old_ctr, g.status AS old_status
          FROM public.tags AS g
          JOIN peer AS e ON e.uid = g.uid AND e.tenant_id = g.tenant_id
           FOR UPDATE OF g
    ), moved AS (
        UPDATE public.tags AS t
           SET last_ctr = p_ctr
          FROM prev AS v, peer AS e
         WHERE t.uid = v.old_uid
           AND t.tenant_id = v.old_tenant
           AND e.uid = t.uid
           AND e.tenant_id = t.tenant_id
           AND v.old_status = 'active'
           AND v.old_ctr < p_ctr
     RETURNING t.status
    )
    SELECT CASE
               WHEN NOT EXISTS (SELECT 1 FROM peer) THEN 'not_peer'
               WHEN EXISTS (SELECT 1 FROM moved) THEN 'advanced'
               WHEN (SELECT v.old_status FROM prev AS v) IS DISTINCT FROM 'active' THEN 'peer_inactive'
               ELSE 'replay'
           END,
           CASE
               WHEN NOT EXISTS (SELECT 1 FROM peer) THEN NULL
               ELSE coalesce((SELECT m.status FROM moved AS m), (SELECT v.old_status FROM prev AS v))
           END
$$;
-- +goose StatementEnd
ALTER FUNCTION public.holding_advance_tag_counter(character, integer) OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_advance_tag_counter(character, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.holding_advance_tag_counter(character, integer) TO tappa_app;

-- holding_logo -- the CURRENT logo bytes of a peer's plaque's business, only for a business VIES
-- verified and only for the digest it holds now (the one holding_tap_site hands out): another
-- business's digest, an old one and an unverified business are the same 0 rows (ADR 0027 §10,
-- ADR 0024 §5's byte-identical 404).
-- +goose StatementBegin
CREATE FUNCTION public.holding_logo(p_uid character, p_digest text)
    RETURNS TABLE(logo bytea, logo_mime text)
    LANGUAGE sql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    WITH caller AS MATERIALIZED (
        SELECT NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid AS id
    ), plaque AS MATERIALIZED (
        SELECT t.tenant_id
          FROM public.tags AS t
         WHERE t.uid = p_uid
    ), peer AS MATERIALIZED (
        SELECT p.tenant_id
          FROM plaque AS p, caller AS c
         WHERE public.holding_peer(c.id, p.tenant_id, 'strict')
    )
    SELECT b.logo, b.logo_mime
      FROM peer AS p
      JOIN public.tenants AS s ON s.id = p.tenant_id AND s.vat_verified IS TRUE
      JOIN public.tenant_branding AS b ON b.tenant_id = p.tenant_id
     WHERE b.logo_sha256 = p_digest
       AND b.logo IS NOT NULL
     LIMIT 1
$$;
-- +goose StatementEnd
ALTER FUNCTION public.holding_logo(character, text) OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_logo(character, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.holding_logo(character, text) TO tappa_app;

-- ===========================================================================
-- 4. holding_site_gate -- a row that names a site is written only for a peer (K-29, K-33).
-- ===========================================================================
-- BEFORE INSERT ON transactions, only for a row with a site. In order:
--   1. lock: holding_peer(employer, site, 'lock') takes the two memberships FOR SHARE, in canonical
--      order -- an op_remove_holding_member that has written left_at and not committed holds the
--      row, so the gate waits for it and the next statement reads the committed left_at;
--   2. the arm: TOLERANT (60 s after left_at, memberships that overlapped) for
--        (i)  an NFC row whose counter WAS advanced: sun_valid IS TRUE and the site plaque's
--             last_ctr >= NEW.ctr, and
--        (ii) a refusal on a tap channel: verdict 'reject' and (sun_valid IS NOT TRUE or the site
--             plaque's last_ctr >= NEW.ctr) -- a refusal earns no hours (no direction, never
--             approved), and the last condition bounds a raw writer that claims sun_valid with a
--             counter the plaque never reached;
--      STRICT (statement_timestamp()) for everything else -- QR and manual rows, an NFC row whose
--      counter was not advanced;
--   3. not a peer in that arm: refused, 23514 naming holding_site_gate. The employer's own rows
--      (no site) never reach this function (the trigger's WHEN).
-- SECURITY DEFINER owned by tappa_holdingdefiner (it asks holding_peer, which only that role may
-- call); EXECUTE in its owner alone; it refuses to run bound to anything but transactions'
-- BEFORE INSERT ROW (TG_RELID and friends -- a trigger function is bindable, and tappa_app has
-- TEMP); lock_timeout 2 s (ADR 0028 §5's starting value): a remover stuck holding a membership
-- costs this INSERT "try again", not a connection held for ever.
-- +goose StatementBegin
CREATE FUNCTION public.holding_site_gate()
    RETURNS trigger
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET lock_timeout = '2s'
AS $$
DECLARE
    v_advanced boolean;
    v_tolerant boolean;
    v_peer     boolean;
BEGIN
    IF TG_RELID IS DISTINCT FROM 'public.transactions'::pg_catalog.regclass
       OR TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'holding_site_gate: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF NEW.site_tenant_id IS NULL THEN
        RETURN NEW;
    END IF;

    PERFORM public.holding_peer(NEW.tenant_id, NEW.site_tenant_id, 'lock');

    v_advanced := NEW.ctr IS NOT NULL AND EXISTS (
        SELECT 1
          FROM public.tags AS t
         WHERE t.uid = NEW.site_tag_uid
           AND t.tenant_id = NEW.site_tenant_id
           AND t.last_ctr >= NEW.ctr);
    v_tolerant := NEW.channel IN ('nfc', 'qr')
                  AND ((NEW.channel = 'nfc' AND NEW.sun_valid IS TRUE AND v_advanced)
                       OR (NEW.verdict = 'reject' AND (NEW.sun_valid IS NOT TRUE OR v_advanced)));

    IF v_tolerant THEN
        v_peer := public.holding_peer(NEW.tenant_id, NEW.site_tenant_id, 'tolerant_stmt');
    ELSE
        v_peer := public.holding_peer(NEW.tenant_id, NEW.site_tenant_id, 'strict_stmt');
    END IF;
    IF v_peer IS NOT TRUE THEN
        RAISE EXCEPTION 'holding_site_gate: the site of this row is not a peer of its tenant'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'holding_site_gate';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.holding_site_gate() OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_site_gate() FROM PUBLIC;

CREATE TRIGGER transactions_holding_site_gate
    BEFORE INSERT ON transactions
    FOR EACH ROW
    WHEN (NEW.site_tenant_id IS NOT NULL)
    EXECUTE FUNCTION public.holding_site_gate();

-- ---------------------------------------------------------------------------
-- 5. POSTCONDITION -- measured as tappa_app would meet it (has_* count membership and PUBLIC).
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT pg_catalog.has_function_privilege('tappa_app', 'public.holding_tap_site(character)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.holding_advance_tag_counter(character, integer)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.holding_logo(character, text)', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.holding_site_gate()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.holding_peer(uuid, uuid, text)', 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00043 (M11 API-1b): tappa_app must EXECUTE the three tap functions and neither holding_peer nor the site gate -- through PUBLIC or a role it is a member of it does not. Fix the grants by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- 🔴 A Down THAT WOULD ERASE WHERE A RECORDED TAP HAPPENED REFUSES (CLAUDE.md §4.3, §4.6; the
-- 00037 precedent). Dropping the site columns would leave every holding tap a row with no venue
-- and no plaque -- the record stays but its "where" is gone, and transactions takes no correction.
-- So while a row names a site this Down stops and changes nothing; on a database with none it
-- takes the gate, the three functions, the grants and the columns away.
-- ⚠️ It takes ACCESS EXCLUSIVE on transactions (DROP TRIGGER, ALTER TABLE): this file names no
-- operator object, and opAtVersion (internal/db's operator tests) skips it.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.transactions WHERE site_tenant_id IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 00043 Down: transactions holds rows that name a site (a tap on a peer''s plaque); dropping the site columns would erase where they happened, and records are immutable. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS transactions_holding_site_gate ON transactions;
DROP FUNCTION IF EXISTS public.holding_site_gate();
DROP FUNCTION IF EXISTS public.holding_logo(character, text);
DROP FUNCTION IF EXISTS public.holding_advance_tag_counter(character, integer);
DROP FUNCTION IF EXISTS public.holding_tap_site(character);
REVOKE SELECT (tenant_id, accent, logo, logo_sha256, logo_mime, logo_width, logo_height)
    ON tenant_branding FROM tappa_holdingdefiner;
REVOKE SELECT (id, name, timezone, vat_verified, api_code) ON tenants FROM tappa_holdingdefiner;
REVOKE SELECT (id, tenant_id, name, external_ref, static_ips, gps_lat, gps_lng, shift_start, shift_end, overnight)
    ON locations FROM tappa_holdingdefiner;
REVOKE UPDATE (last_ctr) ON tags FROM tappa_holdingdefiner;
REVOKE SELECT (uid, tenant_id, location_id, status, last_ctr) ON tags FROM tappa_holdingdefiner;
DROP INDEX IF EXISTS transactions_site_idx;
ALTER TABLE transactions
    DROP CONSTRAINT IF EXISTS transactions_site_by_channel,
    DROP CONSTRAINT IF EXISTS transactions_site_excludes_the_local_venue,
    DROP CONSTRAINT IF EXISTS transactions_site_is_another_tenant,
    DROP CONSTRAINT IF EXISTS transactions_site_needs_its_tenant,
    DROP CONSTRAINT IF EXISTS transactions_site_tag_fk,
    DROP CONSTRAINT IF EXISTS transactions_site_location_fk,
    DROP COLUMN IF EXISTS site_tag_uid,
    DROP COLUMN IF EXISTS site_location_id,
    DROP COLUMN IF EXISTS site_tenant_id;
