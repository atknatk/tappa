-- 00047 -- M11 API-1c: holding_site_locations, the closed list's fourth callable -- a peer's
-- venues, for the EMPLOYER's readers: the panel names the venue (and its shift) of a row that
-- names a site, and a manager picks a peer's venue for a manual record (ADR 0027 §9).
--
-- NORMATIVE SOURCES: docs/adr/0027-holding-ve-capraz-tenant-dokunusu.md §2 (the signature table,
-- verbatim: `p_cursor uuid` -> TABLE(site_tenant_id uuid, location_id uuid, location_name text,
-- location_external_ref text, shift_start time without time zone, shift_end time without time
-- zone, overnight boolean); the contract; "Maliyet -- eş kümesi bir kez": the peer set is
-- computed ONCE, over tenants, in a materialised CTE, and joined to the data rows -- holding_peer is
-- never asked per location), §3 (no new grant: 00043 gave the definer every column read here),
-- §6 (reads are strict), §9 (the employer's nine readers and the manual record), K-33, K-39
-- (static_ips and GPS NEVER -- not in the result, not in any expression of the body).
--
-- PAGED BY THE LOCATION ID: p_cursor NULL is the first page, else the rows after that id; at most
-- 1000 rows a page. No tenant context, an outsider's context and a context with no live
-- membership are one answer: 0 rows.
--
-- WHAT IT RETURNS FOR A VENUE THAT IS NOT ACTIVE (locations.active = false): the row, like any
-- other -- an employer's history names the venue it happened at. The manual-record picker shows
-- them as they are; the result has no `active` column, and adding one widens the closed list
-- (ADR 0027 §2: the user's approval and the ADR).

-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.holding_peer(uuid, uuid, text)') IS NULL
       OR pg_catalog.to_regclass('public.site_punches') IS NULL THEN
        RAISE EXCEPTION 'migration 00047 (M11 API-1c) needs migration 00046. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p WHERE p.proname = 'holding_site_locations') THEN
        RAISE EXCEPTION 'migration 00047 (M11 API-1c) creates holding_site_locations, and it is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION public.holding_site_locations(p_cursor uuid)
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
ALTER FUNCTION public.holding_site_locations(uuid) OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_site_locations(uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.holding_site_locations(uuid) TO tappa_app;

-- +goose StatementBegin
DO $$
BEGIN
    IF NOT pg_catalog.has_function_privilege('tappa_app', 'public.holding_site_locations(uuid)', 'EXECUTE')
       OR pg_catalog.pg_get_function_result('public.holding_site_locations(uuid)'::pg_catalog.regprocedure)
          <> 'TABLE(site_tenant_id uuid, location_id uuid, location_name text, location_external_ref text, shift_start time without time zone, shift_end time without time zone, overnight boolean)' THEN
        RAISE EXCEPTION 'migration 00047 (M11 API-1c): holding_site_locations is not what ADR 0027 §2''s signature table says, or tappa_app cannot call it. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Nothing stored, nothing to keep: the function goes. It names no operator object and takes no
-- table lock (opAtVersion skips it).
DROP FUNCTION IF EXISTS public.holding_site_locations(uuid);
