-- 00055 -- M11 API-8: holding_directory, the closed list's fifth callable -- a holding PEER's
-- employees, for GET /v1/directory (ADR 0028 §2, §9). The key's business's OWN employees never come
-- from here: they are read through the ordinary RLS path (db/queries/directory.sql), and this
-- function answers the peers' alone -- never the caller's tenant (holding_peer(a, a, …) = false,
-- and the peer set excludes the caller's own id besides).
--
-- NORMATIVE SOURCES: docs/adr/0027-holding-ve-capraz-tenant-dokunusu.md §2 (the signature table,
-- verbatim: `p_updated_since timestamp with time zone, p_cursor uuid` -> TABLE(employee_id uuid,
-- employer_tenant_id uuid, api_code text, external_ref text, employee_code text, first_name text,
-- last_name text, active boolean, updated_at timestamp with time zone); the contract -- the caller's
-- tenant from the GUC, never an argument; a fixed result; at most 1000 rows a page; no dynamic SQL;
-- no error-swallowing block; "Maliyet -- eş kümesi bir kez": the peer set is computed ONCE, over
-- tenants, in a materialised CTE, and joined to the data rows -- holding_peer is never asked per
-- employee), §3 (K-34: the definer's employees row is the matrix's -- this file grants the four
-- columns API-1a left for API-8: first_name, last_name, status, updated_at; employees.email is on the
-- "never SELECT" list and is not granted, not read), §6 (the reads are strict); ADR 0028 §2 (the
-- directory is the union of the two sources), §9 (K-21's overlap window, asOf, paged, active/inactive
-- -- K-1h --, no e-mail -- K-1f, K-24).
--
-- THE PAGE IS A KEYSET ON (updated_at, id), the two arguments together:
--   p_cursor NULL      the rows with updated_at >= p_updated_since (NULL: every row) -- a walk's
--                      first page, where the server has already taken K-21's ten minutes off
--                      the integrator's updatedSince;
--   p_cursor not NULL  the rows after the position (p_updated_since, p_cursor) in (updated_at, id)
--                      order -- every later page, the position being the last row the previous
--                      page handed out (own or peer: ids are unique across tenants, so the two
--                      sources share one order).
-- Ordered by (updated_at, id), at most 1000 rows, at most 1000 per peer (LATERAL ... LIMIT) -- the
-- position is applied INSIDE the per-peer read, before its LIMIT, or a peer's first 1000 rows would
-- crowd out the ones after the cursor. In this body's generic plan the planner prices every peer at
-- the average business and reads it through employees_tenant_idx and a sort, the position a filter
-- (measured, ADR 0028's API-8 note: a peer of two thousand rows 1-8 ms; of two hundred thousand,
-- 35-58 ms on top of the peer set -- a named cost). The index 00056 adds is the OWN read's
-- (db/queries/directory.sql), which walks it from the position.
--
-- WHAT A ROW SAYS (K-1f, K-1h): the employee's id and employer, the employer's company code
-- (tenants.api_code), the external ref, the code, the first and last names, whether the person is
-- active (status <> 'deactivated' -- an invited person is active, as in GET /v1/employees), and
-- updated_at. No e-mail, no home venue, no full_name: the signature table names the columns, and a
-- column more is the user's decision (ADR 0027 §2).
--
-- No tenant context, an outsider's context (C, a member of another holding) and a context whose
-- membership has ended are one answer: 0 rows.
--
-- THE MEMBERSHIP GATE -- T124, ADR 0027 §13 item 21 (this file, both multi-row functions). The peer
-- set is still computed once over tenants with holding_peer 'strict' (§2, "eş kümesi bir kez"), but
-- only after ONE call of holding_peer(caller, NULL, 'is_member') -- an existing mode, the one
-- predicate's own answer to "is the caller a member at all" -- has said yes (an InitPlan: the tenants
-- scan is not run when it says no). is_member's 60-second tolerance is a SUPERSET of 'strict' (a live
-- strict membership is a tolerated one), so the gate can hide no peer: it only spares a business in
-- no holding the walk over every tenant. The decision stays strict and stays holding_peer's; no body
-- here names the membership table (§2's scan). MEASURED (a disposable PostgreSQL 17, 21 000 tenants of
-- two thousand rows each, JIT off): a business in no holding -- holding_directory 0.70-0.79 s ->
-- 1.0-1.3 ms, holding_site_locations 0.68-0.74 s -> 1.2-1.4 ms; a member still pays the walk
-- (0.96-1.03 s) -- the rest of T124, which the scan rule keeps open (ADR 0028's API-8 note).
--
-- THE INDEX the keyset walks is 00056's (employees_tenant_updated_idx): a migration of its own, so the
-- Down walks of the holding tests -- which run this file's Down -- never take employees' ACCESS
-- EXCLUSIVE lock (a DROP INDEX), and this file takes no table lock at all.

-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.holding_peer(uuid, uuid, text)') IS NULL
       OR pg_catalog.to_regprocedure('public.holding_site_locations(uuid)') IS NULL THEN
        RAISE EXCEPTION 'migration 00055 (M11 API-8) needs migration 00047. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p WHERE p.oid = 'public.holding_site_locations(uuid)'::pg_catalog.regprocedure)
       IS DISTINCT FROM '63bf5ecfca8a794ac9d8125d19dce0c1' THEN
        RAISE EXCEPTION 'migration 00055 (M11 API-8) replaces holding_site_locations'' body, and it is not 00047''s. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p WHERE p.proname = 'holding_directory') THEN
        RAISE EXCEPTION 'migration 00055 (M11 API-8) creates holding_directory, and it is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- The Down revokes exactly what the Up grants: a column the role already reached would be taken
    -- away by it. And the e-mail is never reachable (K-34).
    IF pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.employees', 'first_name', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.employees', 'last_name', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.employees', 'status', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.employees', 'updated_at', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.employees', 'email', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00055 (M11 API-8) gives tappa_holdingdefiner SELECT on four employees columns, and the role already reaches one of them (or the e-mail). Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- K-34: the matrix's four remaining employees columns (ADR 0027 §3) -- holding_directory reads them.
GRANT SELECT (first_name, last_name, status, updated_at) ON employees TO tappa_holdingdefiner;

-- +goose StatementBegin
CREATE FUNCTION public.holding_directory(p_updated_since timestamptz, p_cursor uuid)
    RETURNS TABLE(employee_id uuid, employer_tenant_id uuid, api_code text, external_ref text, employee_code text,
                  first_name text, last_name text, active boolean, updated_at timestamptz)
    LANGUAGE sql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    WITH caller AS MATERIALIZED (
        SELECT NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid AS id
    ), member AS MATERIALIZED (
        SELECT public.holding_peer(c.id, NULL, 'is_member') AS yes FROM caller AS c
    ), peers AS MATERIALIZED (
        SELECT s.id, s.api_code
          FROM public.tenants AS s, caller AS c
         WHERE (SELECT m.yes FROM member AS m)
           AND s.id <> c.id
           AND public.holding_peer(c.id, s.id, 'strict')
    ), bound AS MATERIALIZED (
        SELECT coalesce(p_updated_since, '-infinity'::timestamptz)
                   - CASE WHEN p_cursor IS NULL THEN interval '1 microsecond' ELSE interval '0' END AS at,
               coalesce(p_cursor, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid) AS id
    )
    SELECT e.id, e.tenant_id, p.api_code, e.external_ref, e.employee_code, e.first_name, e.last_name,
           e.status <> 'deactivated', e.updated_at
      FROM peers AS p
     CROSS JOIN bound AS a
     CROSS JOIN LATERAL (
            SELECT x.id, x.tenant_id, x.external_ref, x.employee_code, x.first_name, x.last_name, x.status, x.updated_at
              FROM public.employees AS x
             WHERE x.tenant_id = p.id
               AND (x.updated_at, x.id) > (a.at, a.id)
             ORDER BY x.updated_at, x.id
             LIMIT 1000
          ) AS e
     ORDER BY e.updated_at, e.id
     LIMIT 1000
$$;
-- +goose StatementEnd
ALTER FUNCTION public.holding_directory(timestamptz, uuid) OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_directory(timestamptz, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.holding_directory(timestamptz, uuid) TO tappa_app;

-- T124 (ADR 0027 §13 item 21): holding_site_locations gets the same membership gate. CREATE OR
-- REPLACE keeps its owner, its EXECUTE grants and its signature; only the body changes, and the Down
-- gives back 00047's body byte for byte (the precondition holds the body to it first).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.holding_site_locations(p_cursor uuid)
    RETURNS TABLE(site_tenant_id uuid, location_id uuid, location_name text, location_external_ref text,
                  shift_start time, shift_end time, overnight boolean)
    LANGUAGE sql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    WITH caller AS MATERIALIZED (
        SELECT NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid AS id
    ), member AS MATERIALIZED (
        SELECT public.holding_peer(c.id, NULL, 'is_member') AS yes FROM caller AS c
    ), peers AS MATERIALIZED (
        SELECT s.id
          FROM public.tenants AS s, caller AS c
         WHERE (SELECT m.yes FROM member AS m)
           AND public.holding_peer(c.id, s.id, 'strict')
    )
    SELECT l.tenant_id, l.id, l.name, l.external_ref, l.shift_start, l.shift_end, l.overnight
      FROM public.locations AS l
      JOIN peers AS p ON p.id = l.tenant_id
     WHERE p_cursor IS NULL OR l.id > p_cursor
     ORDER BY l.id
     LIMIT 1000
$$;
-- +goose StatementEnd

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT pg_catalog.has_function_privilege('tappa_app', 'public.holding_directory(timestamptz, uuid)', 'EXECUTE')
       OR pg_catalog.pg_get_function_identity_arguments('public.holding_directory(timestamptz, uuid)'::pg_catalog.regprocedure)
          <> 'p_updated_since timestamp with time zone, p_cursor uuid'
       OR pg_catalog.pg_get_function_result('public.holding_directory(timestamptz, uuid)'::pg_catalog.regprocedure)
          <> 'TABLE(employee_id uuid, employer_tenant_id uuid, api_code text, external_ref text, employee_code text, first_name text, last_name text, active boolean, updated_at timestamp with time zone)'
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.employees', 'email', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00055 (M11 API-8): holding_directory is not what ADR 0027 §2''s signature table says, tappa_app cannot call it, or the definer reaches the e-mail. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Nothing stored: the function and the four column grants go, and holding_site_locations gets 00047's
-- body back. No table lock (opAtVersion skips this file: no operator object).
DROP FUNCTION IF EXISTS public.holding_directory(timestamptz, uuid);
-- 00047's holding_site_locations, verbatim (md5(prosrc) 63bf5ecfca8a794ac9d8125d19dce0c1).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.holding_site_locations(p_cursor uuid)
    RETURNS TABLE(site_tenant_id uuid, location_id uuid, location_name text, location_external_ref text,
                  shift_start time, shift_end time, overnight boolean)
    LANGUAGE sql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    WITH caller AS MATERIALIZED (
        SELECT NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid AS id
    ), peers AS MATERIALIZED (
        SELECT s.id
          FROM public.tenants AS s, caller AS c
         WHERE public.holding_peer(c.id, s.id, 'strict')
    )
    SELECT l.tenant_id, l.id, l.name, l.external_ref, l.shift_start, l.shift_end, l.overnight
      FROM public.locations AS l
      JOIN peers AS p ON p.id = l.tenant_id
     WHERE p_cursor IS NULL OR l.id > p_cursor
     ORDER BY l.id
     LIMIT 1000
$$;
-- +goose StatementEnd
REVOKE SELECT (first_name, last_name, status, updated_at) ON employees FROM tappa_holdingdefiner;
