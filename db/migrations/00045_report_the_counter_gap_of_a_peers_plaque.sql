-- 00045 -- M11 API-1b, the user's decision of 2026-10-14: holding_advance_tag_counter also
-- returns ctr_gap -- the reads the plaque made that never reached the server, measured in the
-- same statement that advances it -- so the EMPLOYER's decision sees the URL-hoarding signal of a
-- tap on a peer's plaque exactly as it sees its own (base:ctr-gap-review, Q21).
--
-- NORMATIVE SOURCES: docs/adr/0027-holding-ve-capraz-tenant-dokunusu.md §2 (the signature table
-- -- this widens one result of the CLOSED list, which needs the user's approval and the ADR: the
-- user decided it on 2026-10-14, K-1i in the ADR and plan §3) and its "API-1b uygulama notu";
-- CLAUDE.md §4.4 (one statement, the comparison inside it), §4.5.
--
-- THE SEMANTICS ARE THE SINGLE-TENANT STATEMENT'S, TO THE NUMBER (db/queries/tags.sql's
-- AdvanceTagCounter): the gap is `t.last_ctr - prev.old_ctr - 1` computed in the UPDATE's
-- RETURNING, where t.last_ctr is the new value (= p_ctr) and prev.old_ctr the value the row
-- held under the statement's FOR UPDATE -- so a plaque at 100 touched at 150 reports 49 on
-- either path. It is set ONLY when the outcome is 'advanced'; replay, peer_inactive and not_peer
-- carry NULL (nothing moved, there is no gap to speak of -- and not_peer must say nothing more
-- than an unknown uid does). No new privilege: last_ctr is already the definer's to read.
--
-- WHY A NEW FILE AND NOT 00043 EDITED IN PLACE (00043 is on this branch only -- tappa-m11-db and
-- CI -- and editing it was allowed): 00043's Down refuses while transactions holds a row that
-- names a site, and the M11 test database holds 362 of them, committed by the tap tests; walking
-- 00043 down and up there would need those rows deleted under a disabled append-only trigger. A
-- 00045 reaches every database the same way, through `goose up`.
--
-- The result's type changes, so the function is DROPPED and CREATED (CREATE OR REPLACE cannot
-- change a RETURNS TABLE), its owner, its PUBLIC revoke and its one grant re-made in the same
-- transaction. No body names it except checkin's call (internal/db/holding.go).

-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.holding_advance_tag_counter(character, integer)') IS NULL
       OR pg_catalog.pg_get_function_result('public.holding_advance_tag_counter(character, integer)'::pg_catalog.regprocedure)
          <> 'TABLE(outcome text, tag_status text)' THEN
        RAISE EXCEPTION 'migration 00045 (M11 API-1b) widens holding_advance_tag_counter as 00043 created it, and that is not what is there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP FUNCTION public.holding_advance_tag_counter(character, integer);

-- holding_advance_tag_counter -- 00043's statement, with the gap:
--   not a peer (or no such plaque, or no tenant context) ... ('not_peer', NULL, NULL)
--   a peer, the plaque not active ........................ ('peer_inactive', its status, NULL)
--   a peer, active, the counter did not go up ............ ('replay', 'active', NULL)
--   a peer, active, last_ctr moved to p_ctr .............. ('advanced', 'active', p_ctr - old - 1)
-- +goose StatementBegin
CREATE FUNCTION public.holding_advance_tag_counter(p_uid character, p_ctr integer)
    RETURNS TABLE(outcome text, tag_status text, ctr_gap integer)
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
     RETURNING t.status, (t.last_ctr - v.old_ctr - 1)::integer AS gap
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
           END,
           (SELECT m.gap FROM moved AS m)
$$;
-- +goose StatementEnd
ALTER FUNCTION public.holding_advance_tag_counter(character, integer) OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_advance_tag_counter(character, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.holding_advance_tag_counter(character, integer) TO tappa_app;

-- +goose Down

-- 00043's function, verbatim: the two-column result.
DROP FUNCTION IF EXISTS public.holding_advance_tag_counter(character, integer);
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
