-- 00069 -- M14-03: the panel administrator's identity (expand). One tenant-scope-less table,
-- admin_identities; four columns on admin_users that tie a membership row to an identity
-- (identity_id, linked_via, linked_at, unlinked_at); one column on admin_sessions
-- (authenticated_at); tappa_app's SELECT on admin_users narrowed from the table to a column list and
-- its UPDATE (email) taken away; and the trigger that keeps a linked (or refused) row's credentials
-- in the identity definer's hands. No function that writes an identity is born here (M14-04,
-- M14-05); nothing here creates an identity or links a row (M14-06's backfill does).
--
-- NORMATIVE SOURCE: docs/adr/0031-tek-kimlik-cok-isletme.md §5.1 (the table, the columns, the grants,
-- the trigger and its two arms), §5.2 (tappa_identitydefiner: NOLOGIN, BYPASSRLS, NOSUPERUSER, no
-- member, no default privilege, made in scripts/db-init/01-roles.sql and NOT here), §6 (the third
-- §4.5 exception), §8 (expand: the binary before this file keeps working on it); the card is
-- docs/plan/m14-tek-kimlik.md -> M14-03. The UNIQUE (tenant_id, identity_id) index is 00070's, a
-- file of its own (CONCURRENTLY, goose NO TRANSACTION -- ADR 0031 §5.1, bulgu 19).
--
-- admin_identities IS TENANT-SCOPE-LESS (CLAUDE.md §6, ADR 0031 §6): a person's login identity
-- belongs to no one tenant -- that is the whole point of it. So: the R5 waiver below (visible as a
-- WARN on every redline run), an explicit REVOKE ALL from tappa_app (the dev and production default
-- ACL hands a new table `tappa_app=arwd` -- measured on this file's database before writing:
-- pg_default_acl tappa_owner/r = {tappa_app=arwd/tappa_owner}), ENABLE + FORCE row level security
-- and NO policy: a role that obeys RLS reads and writes nothing here, the restore residue a pg_dump
-- leaves included (00026's reasoning); the definer is BYPASSRLS and will reach it through column
-- grants alone (M14-04/M14-05 grant what their functions need -- this file grants it nothing). The
-- primary key is a uuid, so there is NO SEQUENCE to revoke (a sequence would carry the default
-- `rU`; internal/db's catalogue tests are red on a new one). No column is called tenant_id:
-- last_used_tenant is a hint the switch writes (ADR 0031 §5.4), not a scope.
-- redline: no-tenant-scope(admin_identities) — panel yoneticisinin kisi kimligi; hicbir tenant'a ait degil, yazari yalniz identity_* definer'lari, ADR 0031 §5.1/§6
--
-- tappa_app ON admin_users, MEASURED AT 00064 (has_table_privilege / has_column_privilege, this
-- worktree's database): table-level SELECT (00006:115); INSERT and UPDATE column by column (00017:
-- INSERT id, tenant_id, full_name, email, password_hash, role, status; UPDATE full_name, email,
-- password_hash, role, status, last_login_at); no DELETE. So the four new columns are NOT writable
-- by tappa_app without a grant -- none is given -- but they ARE readable through the table-level
-- SELECT, which also covers every future column and makes a column REVOKE inert. Hence (ADR 0031
-- §5.1 S9): REVOKE SELECT on the table, GRANT SELECT on the ten columns the ADR names; linked_via,
-- linked_at, unlinked_at stay unreadable (R16: `password` / `account` would tell a tenant's owner
-- "this person has another business"). identity_id stays readable -- the switch's rate bucket
-- (§5.4), ADR 0031 §6's named residue. No db/queries statement selects `*` on admin_users (grep, and
-- internal/db's EXPLAIN sweep of every generated statement as tappa_app pins it).
-- tappa_app ON admin_sessions: table-level SELECT and INSERT (00006), UPDATE (last_used_at,
-- revoked_at) only (00011). authenticated_at falls under the SELECT and the INSERT -- today's
-- CreateAdminSession names three columns and takes the DEFAULT -- and is NOT updatable (R15).
--
-- THE TRIGGER IS SECURITY INVOKER, NEVER DEFINER (ADR 0031 §5.1, R6): its test is current_user, and
-- in a DEFINER body current_user is the function's owner -- the check would read the owner on every
-- call and never see who wrote. EXECUTE in its owner alone (ADR 0028 §1; 00061's header: a trigger
-- fires without an EXECUTE check, so the triggers keep firing). The body asserts what it is bound to
-- (TG_RELID), like every trigger function since 00037. Dual-write is one way, identity -> rows: no
-- trigger copies a row's write onto the identity (tenant A's write would land on tenant B's row).
-- NAMED COST (ADR 0031 §5.1, §8): during the expand rollout the old pod's password change or reset
-- consumption on a LINKED row is refused (23001 -> a 500) -- minutes; no row is linked before
-- M14-06's backfill.
--
-- LOCKS. admin_users and admin_sessions are hot: every panel request runs TouchAdminSession (an
-- UPDATE of admin_sessions joined to admin_users and tenants). Every statement that locks either
-- table is preceded by its own `SET LOCAL lock_timeout = '1s'` (deploy/README.md's rule, 00037:52,
-- 00064:58): behind a long reader the file gives up with 55P03 -- nothing changes, it is run again at
-- a quiet moment -- instead of queueing every panel request behind an ACCESS EXCLUSIVE. admin_sessions
-- is altered before admin_users, the order TouchAdminSession reads them in. ADD COLUMN with no
-- default rewrites nothing; the CHECK and the foreign key scan admin_users (all identity_id NULL at
-- this point -- a few thousand rows at most). The CREATE TABLE's foreign key on tenants takes SHARE
-- ROW EXCLUSIVE on tenants (it blocks a tenant UPDATE, not a tap's read) under the same 1 s.
--
-- THE DOWN REFUSES WHILE AN IDENTITY EXISTS: dropping admin_identities would drop the only copy of
-- an identity whose rows were refused (a sentinel digest, M14-05) and every link. The way back from
-- the expand is the previous binary, which runs on this schema (ADR 0031 §8); the Down is for a
-- database no identity was born in (development, CI, an aborted deploy).

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'tappa_identitydefiner') THEN
        RAISE EXCEPTION 'migration 00069 (M14-03) needs the cluster role tappa_identitydefiner and deliberately does not create it. Run the one-time step in deploy/README.md, section "Identity role (M14-03)" (it applies the IDENTITY ROLE block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_identitydefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00069 (M14-03): role tappa_identitydefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the IDENTITY ROLE block of scripts/db-init/01-roles.sql (deploy/README.md, section "Identity role (M14-03)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_identitydefiner') THEN
        RAISE EXCEPTION 'migration 00069 (M14-03): role tappa_identitydefiner has members; a member is one SET ROLE from BYPASSRLS (ADR 0031 5.2). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_identitydefiner') THEN
        RAISE EXCEPTION 'migration 00069 (M14-03): role tappa_identitydefiner is a member of another role; its functions would inherit that role''s reach (ADR 0031 5.2). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_default_acl d
                WHERE d.defaclrole = 'tappa_identitydefiner'::pg_catalog.regrole
                   OR EXISTS (SELECT 1 FROM pg_catalog.aclexplode(d.defaclacl) a
                               WHERE a.grantee = 'tappa_identitydefiner'::pg_catalog.regrole)) THEN
        RAISE EXCEPTION 'migration 00069 (M14-03): a default privilege names tappa_identitydefiner; the role holds column grants alone (ADR 0031 5.2). Remove it, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- The definer's future column grants (M14-04/05) cannot narrow a wider privilege some other hand
    -- gave (00024 measured it), so a role that already reaches a relation in public is refused.
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_class c
                WHERE c.relnamespace = 'public'::pg_catalog.regnamespace AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
                  AND (pg_catalog.has_any_column_privilege('tappa_identitydefiner', c.oid, 'SELECT')
                       OR pg_catalog.has_any_column_privilege('tappa_identitydefiner', c.oid, 'INSERT')
                       OR pg_catalog.has_any_column_privilege('tappa_identitydefiner', c.oid, 'UPDATE')
                       OR pg_catalog.has_table_privilege('tappa_identitydefiner', c.oid, 'DELETE')
                       OR pg_catalog.has_table_privilege('tappa_identitydefiner', c.oid, 'TRUNCATE'))) THEN
        RAISE EXCEPTION 'migration 00069 (M14-03): tappa_identitydefiner already reaches a relation in public that no migration gave it; its column grants could not narrow that. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- The column REVOKEs below mean something only while tappa_app holds no TABLE-level INSERT or
    -- UPDATE on admin_users and no table-level UPDATE on admin_sessions (00011, 00017): a table grant
    -- covers every column, the new ones included.
    IF pg_catalog.has_table_privilege('tappa_app', 'public.admin_users', 'INSERT')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.admin_users', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.admin_users', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.admin_sessions', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.admin_sessions', 'DELETE') THEN
        RAISE EXCEPTION 'migration 00069 (M14-03): tappa_app holds a table-level INSERT, UPDATE or DELETE on admin_users or admin_sessions (00006/00011/00017 left column grants); a column-level rule cannot narrow it. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF pg_catalog.to_regclass('public.admin_identities') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid IN ('public.admin_users'::pg_catalog.regclass, 'public.admin_sessions'::pg_catalog.regclass)
                     AND a.attname IN ('identity_id', 'linked_via', 'linked_at', 'unlinked_at', 'authenticated_at')
                     AND NOT a.attisdropped)
       OR pg_catalog.to_regprocedure('public.admin_users_guard_identity_credentials()') IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00069 (M14-03) creates admin_identities, the identity columns and their trigger, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. admin_identities (ADR 0031 §5.1)
-- ---------------------------------------------------------------------------
-- email: GLOBALLY unique (the login resolves an address to at most one identity -- ADR 0002 md.7
--   (ii) for the login, §5.2), citext so a case variant is the same address.
-- password_hash: 00018's bcrypt CHECK, the same shape and the same cost ceiling (a cost-31 digest
--   makes one login attempt cost hours of CPU -- 00018's reason).
-- email_verified_at: the mailbox proof (a verification, a reset link, an invite click -- §5.8).
-- confirm_opened_at / confirm_open_until / confirm_scope: the confirmation window (R1, S3, S5) -- the
--   instant it opened, its end and its scope; the three travel together and the end lies after the
--   opening. Unlinking is allowed only while `email_verified_at IS NOT NULL AND now() <
--   confirm_open_until` (M14-05's SQL).
-- last_used_tenant: the business the person used last (the switch writes it, §5.4) -- a hint for
--   which membership a login opens, never an authority (the membership row decides).
-- NO status column (bulgu 24): no writer in M14 locks an identity.
CREATE TABLE admin_identities (
    id                 uuid        NOT NULL DEFAULT gen_random_uuid(),
    email              citext      NOT NULL,
    password_hash      text        NOT NULL,
    email_verified_at  timestamptz,
    confirm_opened_at  timestamptz,
    confirm_open_until timestamptz,
    confirm_scope      text,
    last_used_tenant   uuid,
    created_at         timestamptz NOT NULL DEFAULT now(),
    last_login_at      timestamptz,
    CONSTRAINT admin_identities_pkey PRIMARY KEY (id),
    CONSTRAINT admin_identities_email_key UNIQUE (email),
    CONSTRAINT admin_identities_password_hash_is_bcrypt
        CHECK (password_hash ~ '^\$2[aby]\$(0[4-9]|1[0-4])\$[./A-Za-z0-9]{53}$'),
    CONSTRAINT admin_identities_confirm_scope_known
        CHECK (confirm_scope IN ('first_verify', 'reset')),
    CONSTRAINT admin_identities_confirm_window_whole
        CHECK ((confirm_opened_at IS NULL) = (confirm_open_until IS NULL)
               AND (confirm_open_until IS NULL) = (confirm_scope IS NULL)),
    CONSTRAINT admin_identities_confirm_window_forward
        CHECK (confirm_open_until > confirm_opened_at),
    CONSTRAINT admin_identities_last_used_tenant_fk FOREIGN KEY (last_used_tenant)
        REFERENCES tenants (id) ON DELETE RESTRICT
);

ALTER TABLE admin_identities ENABLE ROW LEVEL SECURITY;
ALTER TABLE admin_identities FORCE ROW LEVEL SECURITY;

-- NOT a no-op: the dev and production default ACL has just handed tappa_app `arwd` on this table.
REVOKE ALL ON admin_identities FROM tappa_app;

-- ---------------------------------------------------------------------------
-- 2. admin_sessions.authenticated_at (ADR 0031 §5.1, R15)
-- ---------------------------------------------------------------------------
-- The instant of the session's password proof. NULL first, the DEFAULT in a statement of its own:
-- `ADD COLUMN ... DEFAULT now()` would stamp every existing session with the ALTER's instant, i.e.
-- call a days-old session freshly authenticated. NULL means old / expired to every freshness check
-- that reads it (fail closed -- R3). From here on today's CreateAdminSession, which names three
-- columns, takes the DEFAULT -- the login is the password proof. tappa_app may not UPDATE it (no
-- table-level UPDATE; its column list stays last_used_at, revoked_at).
SET LOCAL lock_timeout = '1s';
ALTER TABLE admin_sessions ADD COLUMN authenticated_at timestamptz;

SET LOCAL lock_timeout = '1s';
ALTER TABLE admin_sessions ALTER COLUMN authenticated_at SET DEFAULT now();

-- ---------------------------------------------------------------------------
-- 3. admin_users: the link to the identity (ADR 0031 §5.1)
-- ---------------------------------------------------------------------------
-- identity_id: the identity this membership belongs to; NULL = not linked (every row today).
-- linked_via: how the link was made -- the closed set of ADR 0031 §5.1, 'claim' included (S7).
-- linked_at: when; written with every link (the CHECK below -- T12) and kept through a relink.
-- unlinked_at: the refusal stamp (R1, S4, S8) -- the person refused this membership from the
--   confirmation list; the trigger's second arm guards the row's password from then on.
SET LOCAL lock_timeout = '1s';
ALTER TABLE admin_users
    ADD COLUMN identity_id uuid,
    ADD COLUMN linked_via  text,
    ADD COLUMN linked_at   timestamptz,
    ADD COLUMN unlinked_at timestamptz,
    ADD CONSTRAINT admin_users_linked_via_known
        CHECK (linked_via IN ('backfill', 'reconcile', 'password', 'account', 'invite', 'signup', 'claim')),
    ADD CONSTRAINT admin_users_link_has_a_time
        CHECK (identity_id IS NULL OR linked_at IS NOT NULL);

-- The foreign key in a statement of its own (ADR 0031 §5.1): it takes SHARE ROW EXCLUSIVE on
-- admin_identities and scans admin_users once.
SET LOCAL lock_timeout = '1s';
ALTER TABLE admin_users
    ADD CONSTRAINT admin_users_identity_fk FOREIGN KEY (identity_id)
        REFERENCES admin_identities (id) ON DELETE RESTRICT;

-- ---------------------------------------------------------------------------
-- 4. tappa_app on admin_users (ADR 0031 §5.1, S9, R16, bulgu 21)
-- ---------------------------------------------------------------------------
-- REVOKE SELECT on the table takes the column-level SELECTs with it; the GRANT then names the ten
-- columns the ADR lists. No INSERT or UPDATE is given on any new column (tappa_app could otherwise
-- write a victim's identity_id into its own tenant -- the twin of manager.go:285-292). UPDATE (email)
-- goes in the expand: no statement writes it (ADR 0031 §2), so K-14's mirror is structural from day
-- one.
SET LOCAL lock_timeout = '1s';
REVOKE SELECT ON admin_users FROM tappa_app;
GRANT SELECT (id, tenant_id, full_name, email, password_hash, role, status, created_at, last_login_at, identity_id)
    ON admin_users TO tappa_app;
REVOKE UPDATE (email) ON admin_users FROM tappa_app;

-- ---------------------------------------------------------------------------
-- 5. The trigger (ADR 0031 §5.1, R6, S8)
-- ---------------------------------------------------------------------------
-- First arm: a LINKED row's password_hash or email changes, and the writer is not
-- tappa_identitydefiner -> refused (fail closed; the owner and tappa_app alike). Second arm: a
-- REFUSED row's (unlinked_at stamped) password_hash changes, and the writer is not the definer ->
-- refused: the mailbox owner's per-row reset cannot put a usable password over the sentinel. The
-- e-mail is compared as text, so a case change counts as a change. The WHEN keeps every other row
-- -- every row today -- out of the function entirely; current_user is read in the body, where
-- SECURITY INVOKER keeps it the writer's.
-- +goose StatementBegin
CREATE FUNCTION public.admin_users_guard_identity_credentials()
    RETURNS trigger
    LANGUAGE plpgsql
    SECURITY INVOKER
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'UPDATE'
       OR TG_RELID <> 'public.admin_users'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'admin_users_guard_identity_credentials: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF current_user = 'tappa_identitydefiner' THEN
        RETURN NEW;
    END IF;
    IF OLD.identity_id IS NOT NULL
       AND (NEW.password_hash IS DISTINCT FROM OLD.password_hash
            OR NEW.email::text IS DISTINCT FROM OLD.email::text) THEN
        RAISE EXCEPTION 'admin_users: the password and e-mail of a membership linked to an identity are written through the identity alone (ADR 0031)'
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF OLD.unlinked_at IS NOT NULL
       AND NEW.password_hash IS DISTINCT FROM OLD.password_hash THEN
        RAISE EXCEPTION 'admin_users: the password of a membership refused from an identity is written by the identity definer alone (ADR 0031)'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.admin_users_guard_identity_credentials() FROM PUBLIC;

SET LOCAL lock_timeout = '1s';
CREATE TRIGGER admin_users_identity_credentials
    BEFORE UPDATE ON admin_users
    FOR EACH ROW
    WHEN (OLD.identity_id IS NOT NULL OR OLD.unlinked_at IS NOT NULL)
    EXECUTE FUNCTION public.admin_users_guard_identity_credentials();

-- ---------------------------------------------------------------------------
-- 6. POSTCONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    col text;
    who text;
BEGIN
    FOREACH col IN ARRAY ARRAY['identity_id', 'linked_via', 'linked_at', 'unlinked_at', 'email'] LOOP
        IF pg_catalog.has_column_privilege('tappa_app', 'public.admin_users', col, 'UPDATE')
           OR (col <> 'email' AND pg_catalog.has_column_privilege('tappa_app', 'public.admin_users', col, 'INSERT')) THEN
            RAISE EXCEPTION 'migration 00069 postcondition: tappa_app may write admin_users.%', col;
        END IF;
    END LOOP;
    FOREACH col IN ARRAY ARRAY['linked_via', 'linked_at', 'unlinked_at'] LOOP
        IF pg_catalog.has_column_privilege('tappa_app', 'public.admin_users', col, 'SELECT') THEN
            RAISE EXCEPTION 'migration 00069 postcondition: tappa_app may read admin_users.%', col;
        END IF;
    END LOOP;
    IF pg_catalog.has_table_privilege('tappa_app', 'public.admin_users', 'SELECT')
       OR NOT pg_catalog.has_column_privilege('tappa_app', 'public.admin_users', 'identity_id', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.admin_sessions', 'authenticated_at', 'UPDATE') THEN
        RAISE EXCEPTION 'migration 00069 postcondition: tappa_app''s reach on admin_users / admin_sessions is not the ADR''s';
    END IF;
    -- No role but the owner reaches the table at 00069 -- PUBLIC and every role that is not a
    -- superuser (a superuser passes every privilege check and says nothing here).
    FOR who IN SELECT 'public' UNION ALL SELECT rolname::text FROM pg_catalog.pg_roles WHERE NOT rolsuper AND rolname !~ '^pg_' LOOP
        IF pg_catalog.has_any_column_privilege(who, 'public.admin_identities', 'SELECT')
           OR pg_catalog.has_any_column_privilege(who, 'public.admin_identities', 'INSERT')
           OR pg_catalog.has_any_column_privilege(who, 'public.admin_identities', 'UPDATE')
           OR pg_catalog.has_table_privilege(who, 'public.admin_identities', 'DELETE')
           OR pg_catalog.has_table_privilege(who, 'public.admin_identities', 'TRUNCATE') THEN
            RAISE EXCEPTION 'migration 00069 postcondition: % reaches admin_identities', who;
        END IF;
    END LOOP;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p
                WHERE p.oid = 'public.admin_users_guard_identity_credentials()'::pg_catalog.regprocedure
                  AND (p.prosecdef OR pg_catalog.has_function_privilege('public', p.oid, 'EXECUTE')
                       OR pg_catalog.has_function_privilege('tappa_app', p.oid, 'EXECUTE'))) THEN
        RAISE EXCEPTION 'migration 00069 postcondition: the trigger function is SECURITY DEFINER or executable outside its owner';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- Both hot tables in ONE statement, in the Up's order, BEFORE the check: between an unlocked check
-- and the DROP an identity could be born and linked.
LOCK TABLE admin_sessions, admin_users IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.admin_identities)
       OR EXISTS (SELECT 1 FROM public.admin_users WHERE identity_id IS NOT NULL OR unlinked_at IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 00069 Down: admin_identities holds identities (or a membership row is linked or refused); dropping them would drop the only copy of an identity and its links. Go back with the previous binary, which runs on this schema (ADR 0031 8). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP TRIGGER admin_users_identity_credentials ON admin_users;
DROP FUNCTION public.admin_users_guard_identity_credentials();

-- tappa_app's 00064 reach: table-level SELECT (00006:115) and UPDATE (email) again. The REVOKE first
-- clears the column-level SELECTs, so the catalogue is 00064's byte for byte.
REVOKE SELECT ON admin_users FROM tappa_app;
GRANT SELECT ON admin_users TO tappa_app;
GRANT UPDATE (email) ON admin_users TO tappa_app;

-- 00070's index (if a Down walk did not take it first) goes with identity_id.
ALTER TABLE admin_users
    DROP CONSTRAINT admin_users_identity_fk,
    DROP CONSTRAINT admin_users_link_has_a_time,
    DROP CONSTRAINT admin_users_linked_via_known,
    DROP COLUMN unlinked_at,
    DROP COLUMN linked_at,
    DROP COLUMN linked_via,
    DROP COLUMN identity_id;

ALTER TABLE admin_sessions DROP COLUMN authenticated_at;

DROP TABLE admin_identities;
