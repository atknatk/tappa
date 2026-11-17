-- Tappa -- the privileges of tappa_readonly and tappa_readwrite (ADR 0048). PRODUCTION ONLY,
-- applied by the OPERATOR out of band, as tappa_owner, in ONE transaction, together with (and
-- after) the SCOPED ACCESS ROLES block of scripts/db-init/01-roles.sql:
--
--   { sed -n '/>>> SCOPED ACCESS ROLES (ADR 0048) >>>/,/<<< SCOPED ACCESS ROLES (ADR 0048) <<</p' \
--       scripts/db-init/01-roles.sql; cat deploy/db/scoped-access.sql; } \
--   | kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
--       sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -v ON_ERROR_STOP=1 -1 -U tappa_owner -d tappa'
--
-- (deploy/README.md -> "Kapsamli erisim (ADR 0048)" carries it; this header is the reference.)
-- It is the database's 01-rbac.yaml: every grant is here because the user asked for it, and every
-- grant that is NOT here was measured and withheld for a red line.
--
-- 🔴 WHY NOT A MIGRATION, AND WHY NOT AN INIT SCRIPT -- both measured, not assumed.
--   * Not a goose migration: migrations run in development, CI and the sandbox too, and these two
--     roles exist to let ONE PERSON past RLS in PRODUCTION. A migration that grants a BYPASSRLS
--     role every table is exactly what redline R5b exists to stop, and the right answer to that
--     is not a waiver.
--   * Not deploy/k8s/postgres-init: postgres's entrypoint runs /docker-entrypoint-initdb.d ONCE,
--     on an empty PGDATA. The production volume was initialised 93 days before this file
--     (tappa-postgres-0, AGE 93d, 2026-11-17), so an init script would never reach it -- and on
--     a fresh volume there are no tables yet to grant on.
--   So, like deploy/k8s/01-rbac.yaml: the operator's, idempotent, re-run whenever needed.
--
-- 🔴 WHEN TO RE-RUN IT: after any deploy whose migrations CREATE a table. The default privileges
-- below give a new table SELECT to both roles and INSERT to tappa_readwrite automatically; they
-- DELIBERATELY do not give UPDATE or DELETE (next paragraph), so until this file runs again
-- tappa_readwrite gets "permission denied" on a new table's UPDATE/DELETE. That is the closed
-- direction and it is noisy.
--
-- 🔴 WHY THE DEFAULT PRIVILEGE STOPS AT SELECT, INSERT (§4.3). A default privilege cannot tell an
-- append-only table from a mutable one: it applies at CREATE TABLE, before the migration's own
-- `CREATE TRIGGER ... tappa_forbid_mutation` exists. A default that granted UPDATE, DELETE would
-- hand tappa_readwrite both verbs on the NEXT append-only table the moment it is created -- and
-- the trigger is a second belt, not the first (pg-restore-verify.sh section 4: the privilege is
-- what must be absent; the trigger's P0001 is the fallback). So UPDATE/DELETE are granted ONLY by
-- the DO block below, which reads the guard from pg_trigger at the moment it runs: a table that
-- carries a tappa_forbid_mutation row trigger never gets them, whatever its name. No list to go
-- stale -- the same derivation TestAppendOnlyTablesAreNamedByBothScripts makes from db/migrations.
-- Measured on production 2026-11-17 (goose 107): twelve such tables -- audit_log, billing_periods,
-- legal_documents, operator_audit_log, policy_versions, punch_claim_decisions, punch_claims,
-- punch_events, site_punches, tenant_plan_history, transaction_reviews, transactions -- which is
-- exactly pg-restore-verify.sh's trunc_tables and redline-check.sh's APPEND_ONLY.
--
-- 🔴 WHAT ELSE IS WITHHELD, EACH FOR A NAMED RULE:
--   * tags: no DELETE, no INSERT, no table-level privilege at all. A tags row is never deleted
--     (the plaque's key lives there; delete it and the chip is locked for good), and DELETE +
--     INSERT resets last_ctr to 0, i.e. replay protection (§4.4) -- measured for tappa_app in
--     pg-restore-verify.sh's header. Column grants instead, derived from tappa_app's OWN column
--     grants on tags (so the two never diverge) minus three columns:
--       aes_key_ref, app_key_ref -- the wrapped keys (§4.7). tappa_app can neither read nor write
--                                   them; neither can these roles.
--       last_ctr (UPDATE only)  -- tags_counter_monotonic refuses a rewind, but nothing refuses a
--                                   JUMP: one UPDATE to a huge value makes every later genuine tap
--                                   a replay, and the counter can never come back down. Only the
--                                   atomic tap path writes it (§4.4).
--   * goose_db_version: SELECT only. A forged or deleted row makes goose skip or re-run a
--     migration; that is the deploy's ledger, not data.
--   * TRUNCATE, REFERENCES, TRIGGER, MAINTAIN: never. No CREATE on schema public (measured: only
--     pg_database_owner holds it), no table ownership, no membership in any role.
--   * EXECUTE on SECURITY DEFINER functions: none is granted, and the precondition below REFUSES
--     to run if either role could execute one (through PUBLIC, say). The op_* / holding_* /
--     identity_* / resolve_* doors keep their own boundaries (ADR 0020/0021/0027/0031).
--
-- 🔴 AND WHAT BYPASSRLS MEANS HERE, STATED: both roles read every tenant's rows and tappa_readwrite
-- changes them. Triggers still fire for them (they are not superusers and cannot change
-- session_replication_role), so the append-only guards, the tags write-once guards and every
-- other row trigger still refuse what they refuse.
--
-- OTHER DATABASES: PUBLIC loses CONNECT on `postgres` and `template1`, so these roles can open
-- only `tappa`. Measured before (2026-11-17): every DSN in tappa-secrets names /tappa
-- (tappa_app, tappa_operator, tappa_owner), pg_stat_database shows 2 sessions on `postgres`
-- since the server started and none live, and every script that connects to `postgres`
-- (deploy/README.md A YOLU 4) does so as tappa_owner -- a superuser, which CONNECT does not bind.
-- template0 already refuses connections (datallowconn = false). ⚠️ What CANNOT be hidden:
-- pg_database is readable by every role, so `\l` still LISTS the four names. On this instance
-- they are postgres, tappa, template0, template1 -- no other product's database exists here
-- (it is a dedicated StatefulSet). See ADR 0048 "Sinirlar".

-- Precondition: the roles exist (the 01-roles.sql block ran first), have the shape that block
-- gives them, and hold nothing they should not. A failure here aborts the whole transaction.
DO $$
DECLARE
    bad text;
BEGIN
    SELECT string_agg(r.rolname || ' super=' || r.rolsuper || ' bypassrls=' || r.rolbypassrls
                      || ' createrole=' || r.rolcreaterole || ' createdb=' || r.rolcreatedb
                      || ' replication=' || r.rolreplication, '; ')
      INTO bad
      FROM pg_catalog.pg_roles r
     WHERE r.rolname IN ('tappa_readonly', 'tappa_readwrite')
       AND (r.rolsuper OR NOT r.rolbypassrls OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication);
    IF (SELECT count(*) FROM pg_catalog.pg_roles WHERE rolname IN ('tappa_readonly', 'tappa_readwrite')) <> 2 THEN
        RAISE EXCEPTION 'scoped-access: tappa_readonly and tappa_readwrite must both exist; run the SCOPED ACCESS ROLES block of scripts/db-init/01-roles.sql first (deploy/README.md)';
    END IF;
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'scoped-access: wrong role attributes: %', bad;
    END IF;
    SELECT string_agg(pg_catalog.pg_get_userbyid(m.member) || ' in ' || pg_catalog.pg_get_userbyid(m.roleid), '; ')
      INTO bad
      FROM pg_catalog.pg_auth_members m
     WHERE m.member IN (SELECT oid FROM pg_catalog.pg_roles WHERE rolname IN ('tappa_readonly', 'tappa_readwrite'))
        OR m.roleid IN (SELECT oid FROM pg_catalog.pg_roles WHERE rolname IN ('tappa_readonly', 'tappa_readwrite'));
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'scoped-access: a membership in either direction (a member inherits BYPASSRLS; a role it is a member of lends it privileges): %', bad;
    END IF;
    SELECT string_agg(c.oid::regclass::text, ', ')
      INTO bad
      FROM pg_catalog.pg_class c
     WHERE c.relowner IN (SELECT oid FROM pg_catalog.pg_roles WHERE rolname IN ('tappa_readonly', 'tappa_readwrite'));
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'scoped-access: these roles own relations (an owner can ALTER, DROP and DISABLE TRIGGER): %', bad;
    END IF;
END
$$;

-- Only `tappa` can be opened (see the header).
REVOKE CONNECT ON DATABASE postgres FROM PUBLIC;
REVOKE CONNECT ON DATABASE template1 FROM PUBLIC;

-- Start from nothing on every existing object, so a re-run converges instead of accumulating.
-- (A table-level REVOKE also removes that table's column grants.)
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM tappa_readonly, tappa_readwrite;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA public FROM tappa_readonly, tappa_readwrite;
REVOKE ALL ON ALL FUNCTIONS IN SCHEMA public FROM tappa_readonly, tappa_readwrite;
REVOKE CREATE ON SCHEMA public FROM tappa_readonly, tappa_readwrite;

-- tappa_readonly: read everything (tags through the column list below).
GRANT SELECT ON ALL TABLES IN SCHEMA public TO tappa_readonly;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO tappa_readonly;

-- tappa_readwrite: read and append everywhere; change and delete only where the DO block allows.
GRANT SELECT, INSERT ON ALL TABLES IN SCHEMA public TO tappa_readwrite;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO tappa_readwrite;

DO $$
DECLARE
    t record;
    cols text;
BEGIN
    FOR t IN
        SELECT c.oid, c.relname
          FROM pg_catalog.pg_class c
         WHERE c.relnamespace = 'public'::regnamespace
           AND c.relkind IN ('r', 'p')
           AND c.relname NOT IN ('tags', 'goose_db_version')
           AND NOT EXISTS (
                 SELECT 1
                   FROM pg_catalog.pg_trigger g
                   JOIN pg_catalog.pg_proc p ON p.oid = g.tgfoid
                  WHERE g.tgrelid = c.oid
                    AND NOT g.tgisinternal
                    AND p.proname = 'tappa_forbid_mutation'
                    AND p.pronamespace = 'public'::regnamespace)
         ORDER BY c.relname
    LOOP
        EXECUTE format('GRANT UPDATE, DELETE ON public.%I TO tappa_readwrite', t.relname);
    END LOOP;

    -- The append-only tables, by the same guard: SELECT, INSERT and nothing else, whatever ran
    -- before. Re-stated as REVOKEs so the transaction's own state is explicit.
    FOR t IN
        SELECT DISTINCT c.relname
          FROM pg_catalog.pg_class c
          JOIN pg_catalog.pg_trigger g ON g.tgrelid = c.oid AND NOT g.tgisinternal
          JOIN pg_catalog.pg_proc p ON p.oid = g.tgfoid
         WHERE c.relnamespace = 'public'::regnamespace
           AND p.proname = 'tappa_forbid_mutation'
           AND p.pronamespace = 'public'::regnamespace
    LOOP
        EXECUTE format('REVOKE UPDATE, DELETE, TRUNCATE ON public.%I FROM tappa_readwrite', t.relname);
    END LOOP;

    -- goose's ledger: read only.
    REVOKE INSERT, UPDATE, DELETE ON public.goose_db_version FROM tappa_readwrite;

    -- tags: no table-level privilege; columns from tappa_app's own grants, minus the keys.
    REVOKE ALL ON public.tags FROM tappa_readonly, tappa_readwrite;
    SELECT string_agg(quote_ident(a.attname), ', ' ORDER BY a.attnum) INTO cols
      FROM pg_catalog.pg_attribute a
     WHERE a.attrelid = 'public.tags'::regclass AND a.attnum > 0 AND NOT a.attisdropped
       AND a.attname NOT IN ('aes_key_ref', 'app_key_ref')
       AND has_column_privilege('tappa_app', 'public.tags'::regclass, a.attnum, 'SELECT');
    IF cols IS NULL THEN
        RAISE EXCEPTION 'scoped-access: tappa_app reads no column of tags; refusing to guess';
    END IF;
    EXECUTE format('GRANT SELECT (%s) ON public.tags TO tappa_readonly, tappa_readwrite', cols);
    SELECT string_agg(quote_ident(a.attname), ', ' ORDER BY a.attnum) INTO cols
      FROM pg_catalog.pg_attribute a
     WHERE a.attrelid = 'public.tags'::regclass AND a.attnum > 0 AND NOT a.attisdropped
       AND a.attname NOT IN ('aes_key_ref', 'app_key_ref', 'last_ctr')
       AND has_column_privilege('tappa_app', 'public.tags'::regclass, a.attnum, 'UPDATE');
    IF cols IS NOT NULL THEN
        EXECUTE format('GRANT UPDATE (%s) ON public.tags TO tappa_readwrite', cols);
    END IF;
END
$$;

-- Future objects created by the migration role (every table in public is owned by tappa_owner --
-- measured: `SELECT DISTINCT tableowner FROM pg_tables WHERE schemaname = 'public'` -> one row,
-- and DATABASE_MIGRATE_URL's user is tappa_owner).
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  GRANT SELECT ON TABLES TO tappa_readonly;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  GRANT SELECT ON SEQUENCES TO tappa_readonly;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  GRANT SELECT, INSERT ON TABLES TO tappa_readwrite;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO tappa_readwrite;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  REVOKE UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLES FROM tappa_readonly, tappa_readwrite;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  REVOKE INSERT ON TABLES FROM tappa_readonly;

-- Postcondition: no SECURITY DEFINER function in public is executable by either role, whatever
-- path (PUBLIC included) would have handed it over.
DO $$
DECLARE
    bad text;
BEGIN
    SELECT string_agg(r.rolname || ':' || p.oid::regprocedure::text, ', ')
      INTO bad
      FROM pg_catalog.pg_proc p
     CROSS JOIN pg_catalog.pg_roles r
     WHERE p.pronamespace = 'public'::regnamespace
       AND p.prosecdef
       AND r.rolname IN ('tappa_readonly', 'tappa_readwrite')
       AND has_function_privilege(r.oid, p.oid, 'EXECUTE');
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'scoped-access: these roles can EXECUTE a SECURITY DEFINER function (the doors of ADR 0020/0021/0027/0031 are not theirs): %', bad;
    END IF;
END
$$;
