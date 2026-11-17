-- Tappa -- the privileges of tappa_readonly and tappa_readwrite (ADR 0048). PRODUCTION ONLY,
-- applied by the OPERATOR out of band, as tappa_owner, in ONE transaction, together with (and
-- after) the SCOPED ACCESS ROLES block of scripts/db-init/01-roles.sql:
--
--   { sed -n '/>>> SCOPED ACCESS ROLES (ADR 0048) >>>/,/<<< SCOPED ACCESS ROLES (ADR 0048) <<</p' \
--       scripts/db-init/01-roles.sql; cat deploy/db/scoped-access.sql; } \
--   | kubectl --context hetzner-k8s-1 -n tappa exec -i statefulset/tappa-postgres -- \
--       sh -c 'PGPASSWORD="$POSTGRES_PASSWORD" psql -X -q -v ON_ERROR_STOP=1 -1 -f - -U tappa_owner -d tappa'
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
-- 🔴 THE MODEL: READ EVERYTHING; WRITE EXACTLY WHAT THE APPLICATION MAY WRITE, BUT WITHOUT RLS.
--   tappa_readonly  SELECT on every table (tags: a column list, below).
--   tappa_readwrite the same SELECT, plus every INSERT/UPDATE/DELETE that tappa_app ITSELF holds,
--                   at the same level (table or column), copied from pg_class.relacl and
--                   pg_attribute.attacl at the moment this file runs -- minus the tags
--                   exceptions and goose's ledger below.
--   WHY tappa_app AND NOT "every table without an append-only trigger" (the first version of this
--   file, 2026-11-17). That rule was a DENY list, so it failed OPEN: the security review measured it
--   handing tappa_readwrite INSERT/UPDATE/DELETE on tables only a SECURITY DEFINER may write --
--     feed_counters   (tappa_feedwriter, tappa_holdingdefiner): rewinding last_seq makes punch_feed()
--                     hit punch_events' primary key (23505) and every ok tap of that tenant is
--                     dropped WITHOUT a record -- CLAUDE.md §4.6;
--     holding_members, holdings (op_* only): a cross-tenant peer membership with no audit row;
--     oauth_clients, platform_admins, platform_sessions, operator_read_tickets (op_* only): an
--                     unaudited OAuth client, an operator enrolment taken over;
--     admin_identities (identity_* only): the panel identity behind every membership.
--   tappa_app's own grants are an ALLOW list that every migration already reviews against its
--   matrix (internal/db privcatalog_test.go, m11schema_test.go), so the derivation inherits that
--   review and fails CLOSED: a table tappa_app cannot write, this role cannot write either. The
--   postconditions at the end re-measure it -- readwrite's writes are a subset of tappa_app's, the
--   eight tables above take no write, the append-only tables take no UPDATE/DELETE.
--   The append-only tables (§4.3) need no rule of their own under this model: tappa_app holds
--   neither UPDATE nor DELETE on any of them (measured, goose 107: audit_log, billing_periods,
--   legal_documents, operator_audit_log, policy_versions, punch_claim_decisions, punch_claims,
--   punch_events, site_punches, tenant_plan_history, transaction_reviews, transactions -- the tables
--   carrying a tappa_forbid_mutation row guard, which is pg-restore-verify.sh's trunc_tables and
--   redline-check.sh's APPEND_ONLY). The postcondition still reads the guard from pg_trigger.
--
-- 🔴 WHAT IS SUBTRACTED FROM tappa_app's SHAPE, EACH FOR A NAMED RULE:
--   * tags: no INSERT, no DELETE, no table-level privilege at all. A tags row is never deleted
--     (the plaque's key lives there; delete it and the chip is locked for good), and DELETE +
--     INSERT resets last_ctr to 0, i.e. replay protection (§4.4) -- measured for tappa_app in
--     pg-restore-verify.sh's header. Column grants instead, from tappa_app's own, minus:
--       aes_key_ref, app_key_ref -- the wrapped keys (§4.7). tappa_app can neither read nor write
--                                   them; neither can these roles.
--       last_ctr (UPDATE)       -- tappa_app HOLDS it (the atomic counter advance); this role does
--                                   not. tags_counter_monotonic refuses a rewind, but nothing
--                                   refuses a JUMP: one UPDATE to a huge value makes every later
--                                   genuine tap a replay, and the counter never comes back down.
--   * goose_db_version: SELECT only (tappa_app holds nothing there either).
--   * TRUNCATE, REFERENCES, TRIGGER, MAINTAIN: never. No CREATE on schema public (measured: only
--     pg_database_owner holds it), no table ownership, no membership in any role.
--   * EXECUTE on SECURITY DEFINER functions: none is granted, and the postcondition REFUSES to
--     finish if either role could execute one (through PUBLIC, say). The op_* / holding_* /
--     identity_* / resolve_* doors keep their own boundaries (ADR 0020/0021/0027/0031).
--
-- 🔴 WHEN TO RE-RUN IT: after any deploy whose migrations CREATE a table or change tappa_app's
-- grants. The default privileges give a NEW table SELECT to both roles and NOTHING else: a write on
-- it reaches tappa_readwrite only when this file runs again and copies tappa_app's grant. Until
-- then: "permission denied" -- the closed direction, and noisy. (A default cannot know, at CREATE
-- TABLE, what the migration will grant or revoke a few lines later; copying is the only honest
-- source.)
--
-- 🔴 AND WHAT BYPASSRLS MEANS HERE, STATED: both roles read every tenant's rows and tappa_readwrite
-- changes them. Triggers still fire for them (they are not superusers and cannot change
-- session_replication_role), so the append-only guards, the tags write-once guards and every
-- other row trigger still refuse what they refuse.
--
-- OTHER DATABASES: PUBLIC loses CONNECT on `postgres` and `template1`, so these roles can open
-- only `tappa`. Measured before (2026-11-17): every DSN in tappa-secrets names /tappa
-- (tappa_app, tappa_operator, tappa_owner), pg_stat_database shows 2 sessions on `postgres`
-- since its statistics began and none live, and every script that connects to `postgres`
-- (deploy/README.md A YOLU 4) does so as tappa_owner -- a superuser, which CONNECT does not bind.
-- template0 already refuses connections (datallowconn = false). ⚠️ What CANNOT be hidden:
-- pg_database is readable by every role, so `\l` still LISTS the four names. On this instance
-- they are postgres, tappa, template0, template1 -- no other product's database exists here
-- (it is a dedicated StatefulSet). TEMPORARY on `tappa` stays with PUBLIC (both roles can make
-- session-private temp tables): measured, no application path creates one (the two temp tables in
-- the tree, cmd/opadmin and cmd/rotatekek, run as tappa_owner), so it is documented, not revoked.
-- See ADR 0048 "Sinirlar".

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

-- Reading: both roles, every table and sequence (tags through the column list below).
GRANT SELECT ON ALL TABLES IN SCHEMA public TO tappa_readonly, tappa_readwrite;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO tappa_readonly, tappa_readwrite;

DO $$
DECLARE
    t record;
    cols text;
BEGIN
    -- Writing: tappa_app's own TABLE-level INSERT/UPDATE/DELETE, verb by verb.
    FOR t IN
        SELECT DISTINCT c.relname, x.privilege_type
          FROM pg_catalog.pg_class c
         CROSS JOIN LATERAL aclexplode(c.relacl) x
         WHERE c.relnamespace = 'public'::regnamespace
           AND c.relkind IN ('r', 'p')
           AND c.relname NOT IN ('tags', 'goose_db_version')
           AND x.grantee = 'tappa_app'::regrole
           AND x.privilege_type IN ('INSERT', 'UPDATE', 'DELETE')
         ORDER BY 1, 2
    LOOP
        EXECUTE format('GRANT %s ON public.%I TO tappa_readwrite', t.privilege_type, t.relname);
    END LOOP;

    -- ... and its COLUMN-level INSERT/UPDATE, with the same column list.
    FOR t IN
        SELECT c.relname, x.privilege_type,
               string_agg(quote_ident(a.attname), ', ' ORDER BY a.attnum) AS cols
          FROM pg_catalog.pg_attribute a
          JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
         CROSS JOIN LATERAL aclexplode(a.attacl) x
         WHERE c.relnamespace = 'public'::regnamespace
           AND c.relkind IN ('r', 'p')
           AND c.relname NOT IN ('tags', 'goose_db_version')
           AND a.attnum > 0 AND NOT a.attisdropped
           AND x.grantee = 'tappa_app'::regrole
           AND x.privilege_type IN ('INSERT', 'UPDATE')
         GROUP BY c.relname, x.privilege_type
         ORDER BY 1, 2
    LOOP
        EXECUTE format('GRANT %s (%s) ON public.%I TO tappa_readwrite', t.privilege_type, t.cols, t.relname);
    END LOOP;

    -- Sequences: USAGE only where tappa_app holds it.
    FOR t IN
        SELECT DISTINCT c.relname
          FROM pg_catalog.pg_class c
         CROSS JOIN LATERAL aclexplode(c.relacl) x
         WHERE c.relnamespace = 'public'::regnamespace AND c.relkind = 'S'
           AND x.grantee = 'tappa_app'::regrole AND x.privilege_type = 'USAGE'
    LOOP
        EXECUTE format('GRANT USAGE ON SEQUENCE public.%I TO tappa_readwrite', t.relname);
    END LOOP;

    -- tags: no table-level privilege; columns from tappa_app's own grants, minus the keys (and,
    -- for UPDATE, minus last_ctr).
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
-- and DATABASE_MIGRATE_URL's user is tappa_owner). SELECT only, for both roles; a write is copied
-- from tappa_app by the next run of this file (header).
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  GRANT SELECT ON TABLES TO tappa_readonly, tappa_readwrite;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  GRANT SELECT ON SEQUENCES TO tappa_readonly, tappa_readwrite;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  REVOKE INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER, MAINTAIN ON TABLES FROM tappa_readonly, tappa_readwrite;
ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner IN SCHEMA public
  REVOKE USAGE, UPDATE ON SEQUENCES FROM tappa_readonly, tappa_readwrite;

-- Postconditions: measured on the result, not on the intent. Any one aborts the transaction.
DO $$
DECLARE
    bad text;
BEGIN
    -- (1) every write tappa_readwrite holds, tappa_app holds too (a subset), column by column.
    SELECT string_agg(c.relname || '.' || a.attname || ':' || v.p, ', ')
      INTO bad
      FROM pg_catalog.pg_class c
      JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum > 0 AND NOT a.attisdropped
     CROSS JOIN (VALUES ('INSERT'), ('UPDATE')) v(p)
     WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
       AND has_column_privilege('tappa_readwrite', c.oid, a.attnum, v.p)
       AND NOT has_column_privilege('tappa_app', c.oid, a.attnum, v.p);
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'scoped-access: tappa_readwrite writes columns tappa_app cannot: %', bad;
    END IF;
    SELECT string_agg(c.relname, ', ')
      INTO bad
      FROM pg_catalog.pg_class c
     WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
       AND has_table_privilege('tappa_readwrite', c.oid, 'DELETE')
       AND NOT has_table_privilege('tappa_app', c.oid, 'DELETE');
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'scoped-access: tappa_readwrite deletes from tables tappa_app cannot: %', bad;
    END IF;

    -- (2) the tables only a SECURITY DEFINER writes take no write at all (security review,
    -- 2026-11-17), whatever tappa_app's grants say one day.
    SELECT string_agg(c.relname || ':' || v.p, ', ')
      INTO bad
      FROM pg_catalog.pg_class c
     CROSS JOIN (VALUES ('INSERT'), ('UPDATE'), ('DELETE')) v(p)
     WHERE c.relnamespace = 'public'::regnamespace
       AND c.relname IN ('feed_counters', 'holding_members', 'holdings', 'platform_admins',
                         'platform_sessions', 'oauth_clients', 'operator_read_tickets', 'admin_identities')
       AND ((v.p = 'DELETE' AND has_table_privilege('tappa_readwrite', c.oid, 'DELETE'))
         OR (v.p <> 'DELETE' AND has_any_column_privilege('tappa_readwrite', c.oid, v.p)));
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'scoped-access: tappa_readwrite can write a definer-only table: %', bad;
    END IF;

    -- (3) no UPDATE/DELETE on any table carrying the append-only row guard (§4.3).
    SELECT string_agg(DISTINCT c.relname, ', ')
      INTO bad
      FROM pg_catalog.pg_class c
      JOIN pg_catalog.pg_trigger g ON g.tgrelid = c.oid AND NOT g.tgisinternal
      JOIN pg_catalog.pg_proc p ON p.oid = g.tgfoid
     WHERE c.relnamespace = 'public'::regnamespace
       AND p.proname = 'tappa_forbid_mutation' AND p.pronamespace = 'public'::regnamespace
       AND (has_any_column_privilege('tappa_readwrite', c.oid, 'UPDATE')
         OR has_table_privilege('tappa_readwrite', c.oid, 'DELETE'));
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'scoped-access: tappa_readwrite can UPDATE or DELETE an append-only table: %', bad;
    END IF;

    -- (4) tags: no key column for either role, no last_ctr UPDATE, no INSERT, no DELETE.
    SELECT string_agg(r.n || ':' || a.attname || ':' || v.p, ', ')
      INTO bad
      FROM pg_catalog.pg_attribute a
     CROSS JOIN (VALUES ('tappa_readonly'), ('tappa_readwrite')) r(n)
     CROSS JOIN (VALUES ('SELECT'), ('INSERT'), ('UPDATE')) v(p)
     WHERE a.attrelid = 'public.tags'::regclass AND a.attnum > 0 AND NOT a.attisdropped
       AND (a.attname IN ('aes_key_ref', 'app_key_ref') OR (v.p <> 'SELECT' AND a.attname = 'last_ctr')
            OR v.p = 'INSERT')
       AND has_column_privilege(r.n, 'public.tags'::regclass, a.attnum, v.p);
    IF bad IS NOT NULL OR has_table_privilege('tappa_readwrite', 'public.tags'::regclass, 'DELETE') THEN
        RAISE EXCEPTION 'scoped-access: tags is reachable beyond its column list: % (delete=%)', bad,
            has_table_privilege('tappa_readwrite', 'public.tags'::regclass, 'DELETE');
    END IF;

    -- (5) tappa_readonly writes nothing.
    SELECT string_agg(c.relname || ':' || v.p, ', ')
      INTO bad
      FROM pg_catalog.pg_class c
     CROSS JOIN (VALUES ('INSERT'), ('UPDATE'), ('DELETE')) v(p)
     WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p')
       AND ((v.p = 'DELETE' AND has_table_privilege('tappa_readonly', c.oid, 'DELETE'))
         OR (v.p <> 'DELETE' AND has_any_column_privilege('tappa_readonly', c.oid, v.p)));
    IF bad IS NOT NULL THEN
        RAISE EXCEPTION 'scoped-access: tappa_readonly can write: %', bad;
    END IF;

    -- (6) no SECURITY DEFINER function in public is executable by either role, whatever path
    -- (PUBLIC included) would have handed it over.
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
