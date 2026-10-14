-- 00048 -- M11 API-2: the punch feed's tables. A punch an integrator reads is the outward
-- projection of a transactions row; the DATABASE writes it (00049's two triggers), into the TARGET
-- tenant's feed, numbered by that tenant's own counter row. This file is the schema half: the four
-- tables, tappa_app's reach on them, the counter rows of every tenant and the trigger that opens a
-- new tenant's counter row. The writers and the backfill are 00049's.
--
-- NORMATIVE SOURCES: docs/adr/0028-dis-api-anahtar-akis-webhook.md §1 (the `arwd` rule: REVOKE ALL,
-- then column-listed grants; UPDATE always with a column list; REVOKE ALL ON FUNCTION FROM PUBLIC;
-- a trigger function EXECUTE in its owner alone and asserting TG_RELID), §4 (punch_events, its
-- snapshot columns, the roles, version 1 / 'valid', append-only; tappa_feedwriter), §5 (the counter
-- row per tenant, always there: this migration opens one for every tenant, an AFTER INSERT ON
-- tenants trigger owned by tappa_feedwriter opens a new tenant's), §6 (webhook_endpoints and
-- webhook_deliveries: one endpoint per tenant, a delivery per (endpoint, event), the closed state
-- and error-class sets; only a trigger writes a delivery); ADR 0027 §3 (K-34: the definer never
-- reads webhook_endpoints.url or signing_key_sealed); CLAUDE.md §4.3 (punch_events is append-only),
-- §4.5, §6 (the RLS five on every table), §7.
--
-- WHAT IT DOES, IN ORDER:
--   0. the precondition: 00046/00047 in place; none of this file's objects; the cluster role
--      tappa_feedwriter there, of the right shape, member of nothing and with no member;
--   1. feed_counters, punch_events, webhook_endpoints, webhook_deliveries -- each with the RLS
--      five; punch_events append-only (row + TRUNCATE guards);
--   2. tappa_app's reach: SELECT on the four, INSERT/UPDATE on the endpoint (the panel's -- API-9),
--      UPDATE on a delivery's lease and result columns (the worker's -- API-10), nothing else;
--   3. tappa_feedwriter's counter grants and the new tenant's counter row: feed_open_counter() and
--      its AFTER INSERT ON tenants trigger; one counter row for every tenant already there;
--   4. the postcondition.
--
-- 🔴 THE TENANTS TRIGGER SETS app.tenant_id, SCOPED TO ITSELF (measured, a throwaway PostgreSQL 17
-- container, 2026-10-14). tappa_feedwriter is NOBYPASSRLS, so its INSERT into feed_counters meets
-- the table's WITH CHECK, which reads the GUC. The application inserts a tenant inside that
-- tenant's context (tenants' own WITH CHECK demands it), but the seed, the operator's tooling and
-- the tests insert tenants as the owner with no context -- and the trigger would refuse them
-- (42501), costing the tenant. So the body sets app.tenant_id to NEW.id for its one INSERT, and
-- the function carries `SET app.tenant_id = ''` as an ATTRIBUTE: PostgreSQL restores a SET LOCAL
-- made inside a function whose proconfig names the same variable when the function exits -- the
-- caller's value came back unchanged after the call, both from no value and from another tenant's
-- (ADR 0028 §5's measurement of set_config leaking into the transaction was for a body WITHOUT the
-- attribute). The row it writes is NEW.id's alone, last_seq 0, ON CONFLICT DO NOTHING (§4.6: no
-- branch raises once the binding is right).
--
-- SECRETS: webhook_endpoints.signing_key_sealed holds a key sealed with its own KEK (ADR 0028 §6 --
-- CLAUDE.md §4.7's class); no definer reads it or the url (K-34). Nothing here is logged.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.holding_feed()') IS NULL
       OR pg_catalog.to_regprocedure('public.holding_site_locations(uuid)') IS NULL THEN
        RAISE EXCEPTION 'migration 00048 (M11 API-2) needs migrations 00046 and 00047 (site_punches, holding_feed, holding_site_locations). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles WHERE rolname = 'tappa_feedwriter') THEN
        RAISE EXCEPTION 'migration 00048 (M11 API-2) needs the cluster role tappa_feedwriter and deliberately does not create it. Run the one-time step in deploy/README.md, section "Feed role (M11 API-2)" (it applies the FEED ROLE block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_feedwriter'
                  AND (rolsuper OR rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00048 (M11 API-2): role tappa_feedwriter must be NOLOGIN NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the FEED ROLE block of scripts/db-init/01-roles.sql (deploy/README.md, section "Feed role (M11 API-2)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_feedwriter') THEN
        RAISE EXCEPTION 'migration 00048 (M11 API-2): role tappa_feedwriter has members; a member writes the feed with no trigger in the way (ADR 0028 §4). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_feedwriter') THEN
        RAISE EXCEPTION 'migration 00048 (M11 API-2): role tappa_feedwriter is a member of another role; the feed triggers'' owner must be a member of nothing, or they inherit that role''s privileges (ADR 0028 §4). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF pg_catalog.to_regclass('public.feed_counters') IS NOT NULL
       OR pg_catalog.to_regclass('public.punch_events') IS NOT NULL
       OR pg_catalog.to_regclass('public.webhook_endpoints') IS NOT NULL
       OR pg_catalog.to_regclass('public.webhook_deliveries') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc p WHERE p.proname IN ('feed_open_counter', 'punch_feed'))
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_trigger g WHERE g.tgname = 'tenants_feed_counter') THEN
        RAISE EXCEPTION 'migration 00048 (M11 API-2) creates the feed tables and the counter trigger, and one of them is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- A column grant cannot narrow a wider privilege some other hand gave (00024 measured it).
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_class c
                WHERE c.relnamespace = 'public'::regnamespace AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
                  AND (pg_catalog.has_any_column_privilege('tappa_feedwriter', c.oid, 'SELECT')
                       OR pg_catalog.has_any_column_privilege('tappa_feedwriter', c.oid, 'INSERT')
                       OR pg_catalog.has_any_column_privilege('tappa_feedwriter', c.oid, 'UPDATE')
                       OR pg_catalog.has_table_privilege('tappa_feedwriter', c.oid, 'DELETE')
                       OR pg_catalog.has_table_privilege('tappa_feedwriter', c.oid, 'TRUNCATE'))) THEN
        RAISE EXCEPTION 'migration 00048 (M11 API-2) gives tappa_feedwriter column privileges, and the role already reaches a relation in public that no migration gave it. A column grant cannot narrow that. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The tables (ADR 0028 §4, §5, §6) -- each in the tenant whose row it is.
-- ---------------------------------------------------------------------------

-- The cursor's source (K-13): one row per tenant, the last seq handed out in its feed. A writer
-- raises it under the row's lock and commits in that order, so `seq > cursor` skips nothing (ADR
-- 0028 §5's proof). Opened for every tenant (below, and feed_open_counter for a new one).
CREATE TABLE feed_counters (
    tenant_id uuid   NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    last_seq  bigint NOT NULL DEFAULT 0,
    CONSTRAINT feed_counters_pkey PRIMARY KEY (tenant_id),
    CONSTRAINT feed_counters_not_negative CHECK (last_seq >= 0)
);

-- One row per (target tenant, punch): the TARGET reads it (K-31 -- a holding punch has two, the
-- employer's {employer} and the plaque's tenant's {location}; a single-tenant punch one, both
-- roles). The snapshot columns (K-32) are written once, at the event, and never change: the
-- employee's id, ref, code and company code; the venue's id, ref, name and company code; the
-- plaque's uid. No coordinate, address, distance, note, policy context, verdict or e-mail (ADR 0028
-- §6 "Yük"; FeedCatalog pins the column list). version / status are v1's only values (K-5).
--   punch_time   the transactions row's occurred_at -- the instant the phone declared
--   recorded_at  the server's instant the punch entered the feed: the trigger's now() (a
--                single-tenant row's own INSERT; an approved flag's approval), never a column a
--                caller writes
CREATE TABLE punch_events (
    tenant_id        uuid          NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    seq              bigint        NOT NULL,
    punch_id         uuid          NOT NULL,
    punch_tenant_id  uuid          NOT NULL,
    roles            text[]        NOT NULL,
    version          integer       NOT NULL DEFAULT 1,
    status           text          NOT NULL DEFAULT 'valid',
    punch_time       timestamptz   NOT NULL,
    recorded_at      timestamptz   NOT NULL,
    channel          text          NOT NULL,
    employee_id      uuid          NOT NULL,
    employee_ref     text,
    employee_code    text,
    employee_company text,
    location_id      uuid,
    location_ref     text,
    location_name    text,
    location_company text,
    tag_uid          character(14),
    created_at       timestamptz   NOT NULL DEFAULT now(),
    CONSTRAINT punch_events_pkey PRIMARY KEY (tenant_id, seq),
    CONSTRAINT punch_events_seq_positive CHECK (seq > 0),
    -- K-31's "exactly once per target": a second event of one punch in one feed is refused.
    CONSTRAINT punch_events_once UNIQUE (tenant_id, punch_id, version),
    CONSTRAINT punch_events_roles CHECK (roles IN ('{employer,location}'::text[], '{employer}'::text[], '{location}'::text[])),
    -- the location row is the only one written into a tenant other than the punch's
    CONSTRAINT punch_events_location_is_another_tenant CHECK ((roles = '{location}'::text[]) = (punch_tenant_id <> tenant_id)),
    CONSTRAINT punch_events_v1 CHECK (version = 1 AND status = 'valid'),
    CONSTRAINT punch_events_channel CHECK (channel IN ('nfc', 'qr')),
    -- the employer's row: an append-only table that names it cannot point at nothing
    CONSTRAINT punch_events_punch_fk FOREIGN KEY (punch_id, punch_tenant_id)
        REFERENCES transactions (id, tenant_id) ON DELETE RESTRICT
);
-- The history query (from/to, <= 31 days -- ADR 0028 §5).
CREATE INDEX punch_events_tenant_time_idx ON punch_events (tenant_id, punch_time);

-- One endpoint per tenant (ADR 0028 §6). The url and the sealed signing key are the panel's to
-- write and the worker's to read (API-9, API-10); no trigger and no definer reads either (K-34).
CREATE TABLE webhook_endpoints (
    tenant_id          uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    id                 uuid        NOT NULL DEFAULT gen_random_uuid(),
    url                text        NOT NULL,
    signing_key_sealed bytea       NOT NULL,
    active             boolean     NOT NULL DEFAULT true,
    created_at         timestamptz NOT NULL DEFAULT now(),
    rotated_at         timestamptz,
    CONSTRAINT webhook_endpoints_pkey PRIMARY KEY (id),
    CONSTRAINT webhook_endpoints_one_per_tenant UNIQUE (tenant_id),
    CONSTRAINT webhook_endpoints_id_tenant_key UNIQUE (id, tenant_id),
    CONSTRAINT webhook_endpoints_url_shape CHECK (url LIKE 'https://%' AND char_length(url) <= 2048),
    CONSTRAINT webhook_endpoints_key_present CHECK (octet_length(signing_key_sealed) > 0)
);

-- One delivery per (endpoint, event): the trigger that writes an event writes it, when the target's
-- API is open (K-22) and its endpoint active. The worker leases, sends and records (API-10); the
-- remote's response body is neither stored nor logged (ADR 0028 §6).
CREATE TABLE webhook_deliveries (
    tenant_id        uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    id               uuid        NOT NULL DEFAULT gen_random_uuid(),
    endpoint_id      uuid        NOT NULL,
    punch_event_seq  bigint      NOT NULL,
    attempt          integer     NOT NULL DEFAULT 0,
    next_attempt_at  timestamptz NOT NULL DEFAULT now(),
    lease_until      timestamptz,
    state            text        NOT NULL DEFAULT 'pending',
    last_status      integer,
    last_error_class text,
    created_at       timestamptz NOT NULL DEFAULT now(),
    delivered_at     timestamptz,
    CONSTRAINT webhook_deliveries_pkey PRIMARY KEY (id),
    CONSTRAINT webhook_deliveries_once UNIQUE (endpoint_id, punch_event_seq),
    CONSTRAINT webhook_deliveries_attempt CHECK (attempt >= 0),
    CONSTRAINT webhook_deliveries_state CHECK (state IN ('pending', 'delivered', 'dead')),
    CONSTRAINT webhook_deliveries_status CHECK (last_status IS NULL OR last_status BETWEEN 100 AND 599),
    CONSTRAINT webhook_deliveries_error_class CHECK (last_error_class IS NULL OR last_error_class IN
        ('timeout', 'tls', 'refused', 'http_4xx', 'http_5xx', 'blocked_address')),
    CONSTRAINT webhook_deliveries_endpoint_fk FOREIGN KEY (endpoint_id, tenant_id)
        REFERENCES webhook_endpoints (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT webhook_deliveries_event_fk FOREIGN KEY (tenant_id, punch_event_seq)
        REFERENCES punch_events (tenant_id, seq) ON DELETE RESTRICT
);
CREATE INDEX webhook_deliveries_tenant_created_idx ON webhook_deliveries (tenant_id, created_at DESC);
CREATE INDEX webhook_deliveries_tenant_due_idx ON webhook_deliveries (tenant_id, next_attempt_at) WHERE state = 'pending';

ALTER TABLE feed_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE feed_counters FORCE ROW LEVEL SECURITY;
CREATE POLICY feed_counters_tenant_isolation ON feed_counters
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE punch_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE punch_events FORCE ROW LEVEL SECURITY;
CREATE POLICY punch_events_tenant_isolation ON punch_events
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE webhook_endpoints ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_endpoints FORCE ROW LEVEL SECURITY;
CREATE POLICY webhook_endpoints_tenant_isolation ON webhook_endpoints
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

ALTER TABLE webhook_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE webhook_deliveries FORCE ROW LEVEL SECURITY;
CREATE POLICY webhook_deliveries_tenant_isolation ON webhook_deliveries
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- APPEND-ONLY (CLAUDE.md §4.3's treatment, 00005 + 00021): an event an integrator may already have
-- read is never rewritten or removed -- the row trigger stops UPDATE and DELETE for every role, the
-- superuser included; the statement trigger stops TRUNCATE.
CREATE TRIGGER punch_events_no_mutation
    BEFORE UPDATE OR DELETE ON punch_events
    FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation();
CREATE TRIGGER punch_events_no_truncate
    BEFORE TRUNCATE ON punch_events
    FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation();

-- ---------------------------------------------------------------------------
-- 2. tappa_app (ADR 0028 §1's matrix) -- ALL revoked first: production's default hands a new
-- table all four verbs.
-- ---------------------------------------------------------------------------
REVOKE ALL ON feed_counters FROM tappa_app;
REVOKE ALL ON punch_events FROM tappa_app;
REVOKE ALL ON webhook_endpoints FROM tappa_app;
REVOKE ALL ON webhook_deliveries FROM tappa_app;
-- The counter and the events: READ only -- the triggers are the one writer.
GRANT SELECT (tenant_id, last_seq) ON feed_counters TO tappa_app;
GRANT SELECT (tenant_id, seq, punch_id, punch_tenant_id, roles, version, status, punch_time, recorded_at, channel,
              employee_id, employee_ref, employee_code, employee_company, location_id, location_ref, location_name,
              location_company, tag_uid, created_at)
    ON punch_events TO tappa_app;
-- The endpoint: the panel creates it (the id is chosen first -- it is the sealed key's AAD), and
-- changes the address, the key, the switch and the rotation instant -- never the tenant. No DELETE.
GRANT SELECT (tenant_id, id, url, signing_key_sealed, active, created_at, rotated_at) ON webhook_endpoints TO tappa_app;
GRANT INSERT (tenant_id, id, url, signing_key_sealed, active) ON webhook_endpoints TO tappa_app;
GRANT UPDATE (url, signing_key_sealed, active, rotated_at) ON webhook_endpoints TO tappa_app;
-- A delivery: the worker reads it and moves its lease and result. No INSERT (the triggers write
-- it), no DELETE.
GRANT SELECT (tenant_id, id, endpoint_id, punch_event_seq, attempt, next_attempt_at, lease_until, state, last_status,
              last_error_class, created_at, delivered_at)
    ON webhook_deliveries TO tappa_app;
GRANT UPDATE (lease_until, attempt, next_attempt_at, state, last_status, last_error_class, delivered_at)
    ON webhook_deliveries TO tappa_app;

-- ---------------------------------------------------------------------------
-- 3. The counter row of every tenant (ADR 0028 §5: "her tenant için her zaman vardır").
-- ---------------------------------------------------------------------------
-- tappa_feedwriter's grants on the counter (TestFeedwriter_EveryGrantIsIndividuallyNecessary):
--   SELECT (tenant_id, last_seq)  ON CONFLICT's target and DO UPDATE ... RETURNING (3rd round: an
--                                 upsert without SELECT was refused 42501 on the first touch)
--   INSERT (tenant_id, last_seq)  a new tenant's row; the upsert's insert arm
--   UPDATE (last_seq)             the upsert's update arm
GRANT SELECT (tenant_id, last_seq) ON feed_counters TO tappa_feedwriter;
GRANT INSERT (tenant_id, last_seq) ON feed_counters TO tappa_feedwriter;
GRANT UPDATE (last_seq) ON feed_counters TO tappa_feedwriter;

-- +goose StatementBegin
CREATE FUNCTION public.feed_open_counter()
    RETURNS trigger
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
    SET lock_timeout = '2s'
    SET app.tenant_id = ''
AS $$
BEGIN
    IF TG_WHEN <> 'AFTER' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'INSERT'
       OR TG_RELID <> 'public.tenants'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'feed_open_counter: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- The new tenant's context for this one INSERT -- restored when the function returns (the
    -- attribute above; the header says why).
    PERFORM pg_catalog.set_config('app.tenant_id', NEW.id::text, true);
    INSERT INTO public.feed_counters (tenant_id, last_seq) VALUES (NEW.id, 0)
    ON CONFLICT (tenant_id) DO NOTHING;
    RETURN NULL;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.feed_open_counter() OWNER TO tappa_feedwriter;
REVOKE ALL ON FUNCTION public.feed_open_counter() FROM PUBLIC;

CREATE TRIGGER tenants_feed_counter
    AFTER INSERT ON tenants
    FOR EACH ROW
    EXECUTE FUNCTION public.feed_open_counter();

-- Every tenant already there (the migration role bypasses RLS).
INSERT INTO feed_counters (tenant_id, last_seq)
SELECT t.id, 0 FROM tenants t
ON CONFLICT (tenant_id) DO NOTHING;

-- ---------------------------------------------------------------------------
-- 4. POSTCONDITION -- measured as the roles would meet it (has_* count membership and PUBLIC).
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_any_column_privilege('tappa_app', 'public.feed_counters', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.feed_counters', 'UPDATE')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.punch_events', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.punch_events', 'UPDATE')
       OR pg_catalog.has_any_column_privilege('tappa_app', 'public.webhook_deliveries', 'INSERT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.webhook_endpoints', 'tenant_id', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.feed_counters', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.punch_events', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.webhook_endpoints', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.webhook_deliveries', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.punch_events', 'TRUNCATE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.feed_open_counter()', 'EXECUTE')
       OR (SELECT count(*) FROM public.feed_counters) <> (SELECT count(*) FROM public.tenants) THEN
        RAISE EXCEPTION 'migration 00048 (M11 API-2): tappa_app must read the feed tables and write none of them but the endpoint and a delivery''s lease, must not EXECUTE feed_open_counter, and every tenant must have a counter row -- through PUBLIC or a role membership one of these does not hold. Fix it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down

-- 🔴 A Down THAT WOULD ERASE A FEED REFUSES (CLAUDE.md §4.3, §4.6; the 00043 / 00046 precedent).
-- punch_events is append-only and holds what integrators may already have read -- an event's seq,
-- once handed out, is a promise; an endpoint holds an owner's address and sealed key; a delivery
-- holds what was sent. While any of the three holds a row this Down stops and changes nothing; on a
-- database with none it takes the trigger, the function, the grants and the tables away (the
-- counter rows go with their table -- they are re-derived by the Up).
-- ⚠️ It takes ACCESS EXCLUSIVE on tenants (DROP TRIGGER; the tables' keys to it) and transactions
-- (punch_events' key): opAtVersion skips this file (no operator object), and the holding tests'
-- Down walks lock holdingDownTables first.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.punch_events)
       OR EXISTS (SELECT 1 FROM public.webhook_endpoints)
       OR EXISTS (SELECT 1 FROM public.webhook_deliveries) THEN
        RAISE EXCEPTION 'migration 00048 Down: punch_events, webhook_endpoints or webhook_deliveries holds rows (a feed integrators may have read, an owner''s endpoint, what was sent); dropping them would erase that. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd
DROP TRIGGER IF EXISTS tenants_feed_counter ON tenants;
DROP FUNCTION IF EXISTS public.feed_open_counter();
DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhook_endpoints;
DROP TABLE IF EXISTS punch_events;
DROP TABLE IF EXISTS feed_counters;
