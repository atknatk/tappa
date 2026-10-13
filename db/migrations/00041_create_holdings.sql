-- 00041 -- M11 API-1a: the holding entity -- its two tenant-scope-less tables (holdings,
-- holding_members), the single peer predicate holding_peer, and the timezone lock on a member
-- tenant (K-36). Its writers and its read are the operator's and come in 00042.
--
-- NORMATIVE SOURCES: docs/adr/0027-holding-ve-capraz-tenant-dokunusu.md §1 (the two tables and
-- their invariants, K-36), §2 (tappa_holdingdefiner, the function contract, the closed list,
-- holding_peer's five modes: SECURITY INVOKER, VOLATILE, owned by the definer, EXECUTE in the
-- owner alone), §3 (the definer's verb x column matrix, lock_anchor) and §6 (the 60-second
-- tolerance); docs/plan/m11-entegrasyon-api.md §4.0 (REVOKE ALL from tappa_app on every new
-- table; REVOKE ALL ON FUNCTION ... FROM PUBLIC; a trigger function's EXECUTE in its owner alone
-- and a TG_RELID assert); CLAUDE.md §6 (the tenant-scope-less class: a visible R5 waiver, an
-- explicit REVOKE ALL from tappa_app, ENABLE and FORCE row level security, no column named
-- tenant_id). Where this file decides something those leave open, the decision is written next
-- to the statement and in ADR 0027's "API-1a uygulama notu".
--
-- WHY TWO FILES (00041 here, 00042 the operator surface): internal/db's operator tests reach an
-- older operator surface by running the later migrations' Downs inside their own transaction
-- (opAtVersion), and this file's Down takes ACCESS EXCLUSIVE on tenants -- DROP TRIGGER on
-- tenants and the drop of holding_members' foreign key to tenants both do (measured) -- which
-- would stall every other package's test that reads tenants for as long as such a test runs.
-- This file names no operator object, so opAtVersion skips it (00037/00038's precedent,
-- TestOpAtVersion_SkipsOnlyMigrationsWithoutAnOperatorObject); 00042's Down takes no lock on
-- tenants.
--
-- WHAT IT DOES, IN ORDER:
--   0. the precondition: tappa_holdingdefiner's shape and memberships (it is a cluster role,
--      made by 01-roles.sql's HOLDING ROLE block -- never by a migration), none of this file's
--      objects yet, and the role holding nothing in public but USAGE;
--   1. holdings and holding_members -- tenant-scope-less (R5 waivers below), ENABLE + FORCE row
--      level security with NO policy, REVOKE ALL from tappa_app, one live membership per tenant
--      as a partial UNIQUE index;
--   2. tappa_holdingdefiner's grants -- ADR 0027 §3's holding_members row and nothing else;
--   3. holding_peer -- the ONE answer to "are these two tenants peers in one holding";
--   4. holding_timezone_lock and its trigger on tenants (K-36);
--   5. the postcondition: tappa_app reaches neither table nor either function.
--
-- §4.5: holding_peer and holding_timezone_lock belong to tappa_holdingdefiner (BYPASSRLS; ADR
-- 0027's closed list); neither takes a caller's tenant -- the trigger reads the row being
-- updated, and holding_peer is callable by its owner alone.
--
-- CLOCKS: holding_peer's statement modes use statement_timestamp() on purpose (ADR 0027 §6: a
-- BEFORE and an AFTER trigger of one statement see one clock); its other modes and both tables'
-- DEFAULTs are clock_timestamp().
--
-- SECRETS: none. Every RAISE carries a constant message; the formatted one is the
-- precondition's (a role name).

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- ADR 0027 §2: NOLOGIN, BYPASSRLS, NOSUPERUSER, NOCREATEDB, NOCREATEROLE, NOREPLICATION, no
-- member and a member of nothing -- a member of it is one SET ROLE from every tenant's rows, and
-- its own membership would hand its functions another role's reach. THE REACH: the grants below
-- are column lists, and a column grant cannot take back a wider privilege some other hand gave
-- (00024 measured it) -- the role is born with nothing in public but USAGE (01-roles.sql).
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'tappa_holdingdefiner') THEN
        RAISE EXCEPTION 'migration 00041 (M11 API-1a) needs the cluster role tappa_holdingdefiner and deliberately does not create it. Run the one-time step in deploy/README.md, section "Holding role (M11 API-1a)" (it applies the HOLDING ROLE block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_holdingdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00041 (M11 API-1a): role tappa_holdingdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the HOLDING ROLE block of scripts/db-init/01-roles.sql (deploy/README.md, section "Holding role (M11 API-1a)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_holdingdefiner') THEN
        RAISE EXCEPTION 'migration 00041 (M11 API-1a): role tappa_holdingdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0027 §2). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_holdingdefiner') THEN
        RAISE EXCEPTION 'migration 00041 (M11 API-1a): role tappa_holdingdefiner is a member of another role; the holding functions'' owner must be a member of nothing, or they inherit that role''s privileges (ADR 0027 §2). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF pg_catalog.to_regclass('public.holdings') IS NOT NULL
       OR pg_catalog.to_regclass('public.holding_members') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                   WHERE p.proname IN ('holding_peer', 'holding_timezone_lock'))
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_trigger g
                   WHERE g.tgrelid = 'public.tenants'::regclass
                     AND g.tgname = 'tenants_holding_timezone_lock') THEN
        RAISE EXCEPTION 'migration 00041 (M11 API-1a) creates the holding tables, holding_peer and the timezone trigger, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_class c
                WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
                  AND (pg_catalog.has_any_column_privilege('tappa_holdingdefiner', c.oid, 'SELECT')
                       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', c.oid, 'INSERT')
                       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', c.oid, 'UPDATE')
                       OR pg_catalog.has_table_privilege('tappa_holdingdefiner', c.oid, 'DELETE')
                       OR pg_catalog.has_table_privilege('tappa_holdingdefiner', c.oid, 'TRUNCATE')))
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_class s
                   WHERE s.relnamespace = 'public'::regnamespace AND s.relkind = 'S'
                     AND (pg_catalog.has_sequence_privilege('tappa_holdingdefiner', s.oid, 'USAGE')
                          OR pg_catalog.has_sequence_privilege('tappa_holdingdefiner', s.oid, 'SELECT')
                          OR pg_catalog.has_sequence_privilege('tappa_holdingdefiner', s.oid, 'UPDATE'))) THEN
        RAISE EXCEPTION 'migration 00041 (M11 API-1a) gives tappa_holdingdefiner column privileges, and the role already reaches a relation in public that no migration gave it. A column grant cannot narrow that. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The two tables -- tenant-scope-less (CLAUDE.md §6, ADR 0027 §1).
-- ---------------------------------------------------------------------------
-- redline: no-tenant-scope(holdings) — a holding is a group of tenants, not a tenant's row; only op_* write it and only the operator reads it (ADR 0027 §1)
-- redline: no-tenant-scope(holding_members) — the membership of a tenant in a holding; written by op_* alone, read by op_* and holding_peer alone; the member is member_tenant_id, never tenant_id (ADR 0027 §1)
--
-- holdings.id is the column's DEFAULT and the INSERT grant does not name it, so no caller chooses
-- a holding's id; created_at is the wall clock and not writable (ADR 0021 §2 vii's rule for a
-- time column). The name is the operator's label: trimmed, 1-120 characters, no control
-- character -- the same rule op_create_holding refuses with 22023 before it writes.
CREATE TABLE holdings (
    id         uuid        NOT NULL DEFAULT gen_random_uuid(),
    name       text        NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT holdings_pkey PRIMARY KEY (id),
    CONSTRAINT holdings_name_shape CHECK (name = btrim(name) AND char_length(name) BETWEEN 1 AND 120
                                          AND name !~ '[[:cntrl:]]')
);

-- One row per membership; a tenant that leaves and joins again gets a NEW row (the old one is
-- history), so the key is the member and the instant it joined. joined_at is the column's DEFAULT
-- (the wall clock; the INSERT grant does not name it). left_at is written once, by
-- op_remove_holding_member, guarded by `left_at IS NULL` in its one UPDATE; it is never moved or
-- cleared (ADR 0027 §1 -- TestHoldingOps_LeftAtCannotBeUndone).
-- lock_anchor exists for ONE reason: PostgreSQL wants UPDATE on some column of a table before a
-- role may lock its rows FOR SHARE (ADR 0027 "Bağlam", re-measured for this file), and K-29's
-- gate (holding_peer's 'lock' mode) locks membership rows FOR SHARE. Granting that UPDATE on a
-- membership column would let the definer rewrite memberships; on this column the CHECK leaves
-- one writable value, the one already there (ADR 0027 §3, "kilit çapası").
CREATE TABLE holding_members (
    holding_id       uuid        NOT NULL,
    member_tenant_id uuid        NOT NULL,
    joined_at        timestamptz NOT NULL DEFAULT clock_timestamp(),
    left_at          timestamptz,
    lock_anchor      boolean     NOT NULL DEFAULT false,
    CONSTRAINT holding_members_pkey PRIMARY KEY (member_tenant_id, joined_at),
    CONSTRAINT holding_members_holding_fk FOREIGN KEY (holding_id)
        REFERENCES holdings (id) ON DELETE RESTRICT,
    CONSTRAINT holding_members_tenant_fk FOREIGN KEY (member_tenant_id)
        REFERENCES tenants (id) ON DELETE RESTRICT,
    CONSTRAINT holding_members_left_after_joined CHECK (left_at IS NULL OR left_at >= joined_at),
    CONSTRAINT holding_members_lock_anchor_is_inert CHECK (lock_anchor = false)
);

-- The backstop of "one membership at a time": a second LIVE row for a tenant is refused by the
-- index whatever writes it. The 60-second exit window is op_add_holding_member's rule (an index
-- cannot see a clock).
CREATE UNIQUE INDEX holding_members_one_live_membership ON holding_members (member_tenant_id)
    WHERE left_at IS NULL;
CREATE INDEX holding_members_holding_idx ON holding_members (holding_id, member_tenant_id);

-- ENABLE and FORCE together (scripts/pg-restore-verify.sh refuses a restore whose two counts
-- differ), and NO policy: every role that obeys row level security reads and writes nothing
-- here -- the restore residue a pg_dump leaves (01-roles.sql) included. The two BYPASSRLS
-- definers reach the tables through their column grants alone.
ALTER TABLE holdings ENABLE ROW LEVEL SECURITY;
ALTER TABLE holdings FORCE ROW LEVEL SECURITY;
ALTER TABLE holding_members ENABLE ROW LEVEL SECURITY;
ALTER TABLE holding_members FORCE ROW LEVEL SECURITY;

-- Explicit, whatever the default ACL handed out at CREATE TABLE (`arwd` in production and
-- development, `ar` on a fresh production volume -- ADR 0028 §1).
REVOKE ALL ON holdings FROM tappa_app;
REVOKE ALL ON holding_members FROM tappa_app;

-- ---------------------------------------------------------------------------
-- 2. tappa_holdingdefiner's grants (ADR 0027 §3, K-34) -- the holding_members row of the
-- matrix, and only it.
-- ---------------------------------------------------------------------------
-- SELECT on the four membership columns (holding_peer) and UPDATE on lock_anchor alone (the
-- FOR SHARE of holding_peer's 'lock' mode). The matrix's other rows (tags, locations, tenants,
-- tenant_branding, employees, transactions, ...) belong to the functions API-1b, API-1c and
-- API-2 bring, and each is granted with its function -- the matrix is a ceiling that every
-- task re-earns by measurement (ADR 0027 §3).
GRANT SELECT (holding_id, member_tenant_id, joined_at, left_at) ON holding_members TO tappa_holdingdefiner;
GRANT UPDATE (lock_anchor) ON holding_members TO tappa_holdingdefiner;

-- ===========================================================================
-- 3. holding_peer -- the ONE answer to "are a and b peers in one holding" (ADR 0027 §2, K-33).
-- ===========================================================================
-- Every holding_* function and every holding trigger asks it, and it is the only function owned
-- by tappa_holdingdefiner that names holding_members (the writers being the op_* of migration 00042 --
-- TestHoldingPeer_IsTheOnlyBodyThatTouchesHoldingMembers). Its five modes are a CLOSED set; an
-- unknown mode is an error, never a default.
--
--   strict         a and b in the same holding, both memberships live by the wall clock
--   strict_stmt    the same, by statement_timestamp() (a BEFORE and an AFTER trigger of one
--                  statement see one clock -- ADR 0027 §6)
--   tolerant_stmt  the same holding, both memberships live or ended less than 60 seconds before
--                  statement_timestamp(), AND the two memberships overlapped in time: a tenant
--                  that left is never paired with one that joined after it (ADR 0027 §2,
--                  round 2)
--   lock           locks, FOR SHARE, in canonical order (smaller tenant id, then joined_at), the
--                  membership rows tolerant_stmt would pair; decides nothing and answers NULL
--   is_member      b NULL: has a a membership live or ended less than 60 seconds ago by the wall
--                  clock (K-36's question -- one tenant, not a pair)
--
-- a = b is never a pair (false; NULL for 'lock'): a tenant's own data comes through RLS.
-- SECURITY INVOKER and EXECUTE in its owner alone: it runs only inside the owner's definer
-- bodies, where current_user is tappa_holdingdefiner -- tappa_app can neither call it (a free-
-- argument membership oracle) nor, were it granted by mistake, read holding_members through it.
-- VOLATILE because 'lock' takes row locks (PostgreSQL refuses a locking SELECT in a function that
-- is not), and its ONE row lock is FOR SHARE -- no stronger lock and no write appears in its
-- body (TestHoldingPeer_OnlyRowLockIsForShare).
-- +goose StatementBegin
CREATE FUNCTION public.holding_peer(a uuid, b uuid, mod text)
    RETURNS boolean
    LANGUAGE plpgsql
    VOLATILE
    SECURITY INVOKER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_found boolean;
BEGIN
    IF mod IS NULL OR mod NOT IN ('strict', 'strict_stmt', 'tolerant_stmt', 'lock', 'is_member') THEN
        RAISE EXCEPTION 'holding_peer: unknown mode'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    IF mod = 'is_member' THEN
        IF b IS NOT NULL THEN
            RAISE EXCEPTION 'holding_peer: is_member takes one tenant'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        SELECT EXISTS (
                   SELECT 1
                     FROM public.holding_members AS m
                    WHERE m.member_tenant_id = a
                      AND (m.left_at IS NULL OR m.left_at + interval '60 seconds' > clock_timestamp()))
          INTO v_found;
        RETURN coalesce(v_found, false);
    END IF;

    IF a IS NULL OR b IS NULL OR a = b THEN
        IF mod = 'lock' THEN
            RETURN NULL;
        END IF;
        RETURN false;
    END IF;

    IF mod = 'lock' THEN
        PERFORM 1
           FROM public.holding_members AS m
          WHERE m.member_tenant_id IN (a, b)
            AND EXISTS (
                    SELECT 1
                      FROM public.holding_members AS ma
                      JOIN public.holding_members AS mb ON mb.holding_id = ma.holding_id
                     WHERE ma.member_tenant_id = a
                       AND mb.member_tenant_id = b
                       AND (ma.left_at IS NULL OR ma.left_at + interval '60 seconds' > statement_timestamp())
                       AND (mb.left_at IS NULL OR mb.left_at + interval '60 seconds' > statement_timestamp())
                       AND ma.joined_at < coalesce(mb.left_at, 'infinity'::timestamptz)
                       AND mb.joined_at < coalesce(ma.left_at, 'infinity'::timestamptz)
                       AND ((m.member_tenant_id = ma.member_tenant_id AND m.joined_at = ma.joined_at)
                            OR (m.member_tenant_id = mb.member_tenant_id AND m.joined_at = mb.joined_at)))
          ORDER BY m.member_tenant_id, m.joined_at
            FOR SHARE OF m;
        RETURN NULL;
    END IF;

    IF mod = 'strict' THEN
        SELECT EXISTS (
                   SELECT 1
                     FROM public.holding_members AS ma
                     JOIN public.holding_members AS mb ON mb.holding_id = ma.holding_id
                    WHERE ma.member_tenant_id = a
                      AND mb.member_tenant_id = b
                      AND (ma.left_at IS NULL OR ma.left_at > clock_timestamp())
                      AND (mb.left_at IS NULL OR mb.left_at > clock_timestamp()))
          INTO v_found;
    ELSIF mod = 'strict_stmt' THEN
        SELECT EXISTS (
                   SELECT 1
                     FROM public.holding_members AS ma
                     JOIN public.holding_members AS mb ON mb.holding_id = ma.holding_id
                    WHERE ma.member_tenant_id = a
                      AND mb.member_tenant_id = b
                      AND (ma.left_at IS NULL OR ma.left_at > statement_timestamp())
                      AND (mb.left_at IS NULL OR mb.left_at > statement_timestamp()))
          INTO v_found;
    ELSE
        SELECT EXISTS (
                   SELECT 1
                     FROM public.holding_members AS ma
                     JOIN public.holding_members AS mb ON mb.holding_id = ma.holding_id
                    WHERE ma.member_tenant_id = a
                      AND mb.member_tenant_id = b
                      AND (ma.left_at IS NULL OR ma.left_at + interval '60 seconds' > statement_timestamp())
                      AND (mb.left_at IS NULL OR mb.left_at + interval '60 seconds' > statement_timestamp())
                      AND ma.joined_at < coalesce(mb.left_at, 'infinity'::timestamptz)
                      AND mb.joined_at < coalesce(ma.left_at, 'infinity'::timestamptz))
          INTO v_found;
    END IF;
    RETURN coalesce(v_found, false);
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.holding_peer(uuid, uuid, text) OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_peer(uuid, uuid, text) FROM PUBLIC;

-- ===========================================================================
-- 4. holding_timezone_lock -- a member tenant's timezone does not move (K-36, ADR 0027 §1).
-- ===========================================================================
-- A BEFORE UPDATE trigger on tenants, fired only when the zone changes. A tenant that is a member
-- -- live, or within the 60-second exit window, whose tolerant rows are still worked out in the
-- old zone -- keeps its zone: the change is refused with 23514 naming holding_timezone_lock, which
-- internal/domain/tenant recognises and the owner's panel answers with one sentence. There is no
-- op_* that moves the zone of every member at once in v1 (ADR 0027 §1), and the lock is passed by
-- no signal tappa_app could write.
-- SECURITY DEFINER, owned by tappa_holdingdefiner (the closed list): it asks holding_peer, which
-- only that role may call. Its EXECUTE is the owner's alone and it refuses to run bound to any
-- other table or event (TG_RELID and friends) -- a trigger function is not "uncallable", it is
-- bindable, and tappa_app has TEMP (plan §4.0). lock_timeout is an attribute (ADR 0028 §5): the
-- body takes no row lock, and a stuck catalogue lock may not hold an owner's save forever.
-- +goose StatementBegin
CREATE FUNCTION public.holding_timezone_lock()
    RETURNS trigger
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET lock_timeout = '2s'
AS $$
BEGIN
    IF TG_RELID IS DISTINCT FROM 'public.tenants'::pg_catalog.regclass
       OR TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'UPDATE' THEN
        RAISE EXCEPTION 'holding_timezone_lock: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF NEW.timezone IS DISTINCT FROM OLD.timezone
       AND public.holding_peer(NEW.id, NULL, 'is_member') THEN
        RAISE EXCEPTION 'holding_timezone_lock: the timezone of a holding member does not change'
            USING ERRCODE = 'check_violation', CONSTRAINT = 'holding_timezone_lock';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.holding_timezone_lock() OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_timezone_lock() FROM PUBLIC;

CREATE TRIGGER tenants_holding_timezone_lock
    BEFORE UPDATE ON tenants
    FOR EACH ROW
    WHEN (OLD.timezone IS DISTINCT FROM NEW.timezone)
    EXECUTE FUNCTION public.holding_timezone_lock();

-- ---------------------------------------------------------------------------
-- 5. POSTCONDITION: tappa_app reaches neither table -- measured as tappa_app would meet it
-- (has_* count role membership and PUBLIC), so a privilege some other hand gave fails here, loud.
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_any_column_privilege('tappa_app', 'public.holdings', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.holdings', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.holdings', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.holdings', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.holdings', 'TRUNCATE')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.holding_members', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.holding_members', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.holding_members', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.holding_members', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.holding_members', 'TRUNCATE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.holding_peer(uuid, uuid, text)', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.holding_timezone_lock()', 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00041 (M11 API-1a) revoked tappa_app''s privileges on holdings and holding_members, and tappa_app still reaches them (or holding_peer, or the timezone trigger) -- through PUBLIC or a role it is a member of. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- 🔴 WHAT A SUCCESSFUL Down DOES: the trigger, its function and holding_peer go; holdings and
-- holding_members are DROPPED with every membership in them -- a Down takes the holding away
-- as a thing the schema knows. The facts stay where they were recorded: migration 00042's
-- operator rows (operator_audit_log is append-only) and each tenant's own rows (audit_log is
-- append-only) name every membership that was ever written or ended. tappa_holdingdefiner
-- keeps its USAGE ON SCHEMA public (01-roles.sql's; the role is a cluster object) and holds
-- nothing else once the table is gone. 00042's Down must run first (goose's order): its
-- functions name these tables.
-- ⚠️ Both DROPs below take ACCESS EXCLUSIVE on tenants (DROP TRIGGER; the FK the table drop
-- removes -- measured), which is why this file names no operator object and opAtVersion
-- (internal/db's operator tests) skips it.
DROP TRIGGER IF EXISTS tenants_holding_timezone_lock ON tenants;
DROP FUNCTION IF EXISTS public.holding_timezone_lock();
DROP FUNCTION IF EXISTS public.holding_peer(uuid, uuid, text);
DROP TABLE IF EXISTS holding_members;
DROP TABLE IF EXISTS holdings;
