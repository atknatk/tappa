-- 00062 -- GRP-1: the panel's Group section reads the business's own holding -- its name, the live
-- members (name, company code, joined) -- through ONE new narrow definer, holding_group(), and
-- holding_peer's ROSTER FORM, the one predicate's third signature.
--
-- NORMATIVE SOURCES: docs/adr/0027-holding-ve-capraz-tenant-dokunusu.md §2 (the function contract,
-- the closed list and its signature table, "the ONE function whose body names holding_members is
-- holding_peer"), §3 (tappa_holdingdefiner's verb x column matrix), K-1d (inside a holding: a name and
-- a code) and its "GRP-1 uygulama notu"; the user's decision of 2026-10-19 (the panel shows the
-- membership in a Group section of its own, read only; the holding is run from the operator's side).
--
-- WHY NO EXISTING FUNCTION SERVES. The group's NAME is holdings.name, which no definer reads (the
-- matrix had no holdings column at all); WHICH holding the caller is in, and since when every member
-- belongs to it, are holding_members' rows, which only holding_peer may name. holding_peer's set form
-- answers "who are a's peers" -- uuids, no holding, no date, a itself never in it.
--
-- THE ROSTER FORM -- holding_peer(a uuid) RETURNS TABLE(holding_id uuid, member_tenant_id uuid,
-- joined_at timestamptz). The same NAME, a third signature, so the rule "holding_members is named by
-- holding_peer's body alone" (TestHoldingPeer_IsTheOnlyBodyThatTouchesHoldingMembers, by name) holds
-- as written. Its one question is the set form's, with the holding and the date kept: every live
-- membership of a's holding(s) -- a's own row INCLUDED -- where a's membership and the member's are
-- both live by the wall clock (left_at IS NULL OR left_at > the instant), the instant read ONCE. A
-- NULL a is no row. Two live rows of one member in one holding (the schema allows a live one and one
-- that ends in the future) are one member: grouped, the earlier joined_at. It has no mode (one rule,
-- strict -- nothing to open). It is the set form plus a: for every a, {member <> a} is exactly
-- holding_peer(a, 'strict') and a is in it exactly when a holds a live membership
-- (TestHoldingPeer_TheRosterFormIsTheSetFormsAnswer, a property test over random holdings).
--
-- Its shape is the other two forms': SECURITY INVOKER, VOLATILE, search_path pinned, owned by
-- tappa_holdingdefiner, EXECUTE in its owner alone (tappa_app may never ask it -- a free-argument
-- membership oracle, ADR 0027 §2, 9th round), no row lock, no write, no dynamic SQL, ROWS 10.
--
-- holding_group() -- the callable. Its contract is §2's, item by item: search_path pinned, every name
-- qualified, no dynamic SQL; a FIXED result (below) and LIMIT 1000; the caller from app.tenant_id
-- (NULLIF, never an argument -- it takes NO argument); the filter is the roster form's (a's live
-- holding only: another holding's members are not reached, a business in no holding and a member
-- whose membership has ended get no row -- the same answer as no context); no EXCEPTION block. Its
-- result:
--
--   holding_id       the holding (an opaque key: the panel groups by it and shows none of it)
--   holding_name     holdings.name
--   member_name      tenants.name
--   member_api_code  tenants.api_code (NULL when the member never had one -- K-22)
--   joined_at        the member's live membership's joined_at
--   is_self          the member is the caller
--
-- and NOTHING else of a member: no VAT number, no address or e-mail, no headcount, no plan or
-- billing, no API switch, no timezone, and no member that has LEFT (a left member is not listed --
-- listing it would be a new fact K-1d does not cover; the GRP-1 note says why). The definer's reach
-- does not even hold most of those columns (§3's matrix, pinned); the result is pinned by the
-- signature table (TestHoldingCatalog_TheFunctionsAndTheirExactSignatures).
--
-- THE MATRIX GROWS BY ONE ROW: SELECT (id, name) ON holdings -- holdings.created_at stays out. The
-- peer set is read ONCE (one roster call in a materialised CTE -- ADR 0027 §2, "the peer set once";
-- TestHoldingFunctions_PeerSetComputedOnce counts it).
--
-- No table lock: CREATE FUNCTION, GRANT and REVOKE take none on a relation (measured for GRANT on
-- holdings: no pg_locks row). lock_timeout is the first statement anyway (deploy/README.md's rule).
-- The Down drops the two functions and takes the grant back.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.holding_peer(uuid, uuid, text)') IS NULL
       OR pg_catalog.to_regprocedure('public.holding_peer(uuid, text)') IS NULL THEN
        RAISE EXCEPTION 'migration 00062 (GRP-1) needs migration 00060 (holding_peer''s pair and set forms). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF (SELECT count(*) FROM pg_catalog.pg_proc p WHERE p.proname = 'holding_peer') <> 2
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_proc p WHERE p.proname = 'holding_group') THEN
        RAISE EXCEPTION 'migration 00062 (GRP-1) creates holding_peer''s roster form and holding_group, and a third holding_peer or a holding_group is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- The Down takes back SELECT on holdings whole: it may only take back what this file gave.
    IF pg_catalog.has_table_privilege('tappa_holdingdefiner', 'public.holdings', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.holdings', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00062 (GRP-1): tappa_holdingdefiner already reads holdings; this file grants (id, name) and its Down takes the table back. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- holding_peer's roster form (ADR 0027 §2, GRP-1 note). Every identifier is qualified; the one
-- statement is the set form's with the holding and the date kept and a itself not excluded.
-- +goose StatementBegin
CREATE FUNCTION public.holding_peer(a uuid)
    RETURNS TABLE(holding_id uuid, member_tenant_id uuid, joined_at timestamptz)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY INVOKER
    SET search_path = pg_catalog, pg_temp
    ROWS 10
AS $$
DECLARE
    v_now timestamptz;
BEGIN
    IF a IS NULL THEN
        RETURN;
    END IF;

    v_now := clock_timestamp();
    RETURN QUERY
        SELECT mb.holding_id, mb.member_tenant_id, min(mb.joined_at)
          FROM public.holding_members AS ma
          JOIN public.holding_members AS mb ON mb.holding_id = ma.holding_id
         WHERE ma.member_tenant_id = a
           AND (ma.left_at IS NULL OR ma.left_at > v_now)
           AND (mb.left_at IS NULL OR mb.left_at > v_now)
         GROUP BY mb.holding_id, mb.member_tenant_id;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.holding_peer(uuid) OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_peer(uuid) FROM PUBLIC;

-- The definer's one new reach: the holding's name (and its key, to join on).
GRANT SELECT (id, name) ON public.holdings TO tappa_holdingdefiner;

-- holding_group(): the caller's own holding, its live members. No argument: the caller is
-- app.tenant_id, and the roster form is asked once.
-- +goose StatementBegin
CREATE FUNCTION public.holding_group()
    RETURNS TABLE(holding_id uuid, holding_name text, member_name text, member_api_code text,
                  joined_at timestamptz, is_self boolean)
    LANGUAGE sql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    WITH caller AS MATERIALIZED (
        SELECT NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid AS id
    ), roster AS MATERIALIZED (
        SELECT r.holding_id, r.member_tenant_id, r.joined_at, r.member_tenant_id = c.id AS is_self
          FROM caller AS c
         CROSS JOIN LATERAL public.holding_peer(c.id) AS r
    )
    SELECT h.id, h.name, t.name, t.api_code, r.joined_at, r.is_self
      FROM roster AS r
      JOIN public.holdings AS h ON h.id = r.holding_id
      JOIN public.tenants AS t ON t.id = r.member_tenant_id
     ORDER BY h.id, r.is_self DESC, t.name, t.id
     LIMIT 1000
$$;
-- +goose StatementEnd
ALTER FUNCTION public.holding_group() OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_group() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.holding_group() TO tappa_app;

-- POSTCONDITION: the two new functions have the shapes above, the grants are exactly these.
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.pg_get_userbyid((SELECT p.proowner FROM pg_catalog.pg_proc p WHERE p.oid = 'public.holding_peer(uuid)'::pg_catalog.regprocedure))
           <> 'tappa_holdingdefiner'
       OR (SELECT p.prosecdef OR p.provolatile <> 'v' OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
             FROM pg_catalog.pg_proc p WHERE p.oid = 'public.holding_peer(uuid)'::pg_catalog.regprocedure)
       OR EXISTS (SELECT 1
                    FROM pg_catalog.pg_proc p,
                         pg_catalog.aclexplode(coalesce(p.proacl, pg_catalog.acldefault('f', p.proowner))) AS x
                   WHERE p.oid = 'public.holding_peer(uuid)'::pg_catalog.regprocedure
                     AND x.privilege_type = 'EXECUTE' AND x.grantee <> p.proowner)
       OR pg_catalog.pg_get_userbyid((SELECT p.proowner FROM pg_catalog.pg_proc p WHERE p.oid = 'public.holding_group()'::pg_catalog.regprocedure))
           <> 'tappa_holdingdefiner'
       OR (SELECT NOT p.prosecdef OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
             FROM pg_catalog.pg_proc p WHERE p.oid = 'public.holding_group()'::pg_catalog.regprocedure)
       OR EXISTS (SELECT 1
                    FROM pg_catalog.pg_proc p,
                         pg_catalog.aclexplode(coalesce(p.proacl, pg_catalog.acldefault('f', p.proowner))) AS x
                   WHERE p.oid = 'public.holding_group()'::pg_catalog.regprocedure
                     AND x.privilege_type = 'EXECUTE' AND x.grantee <> p.proowner
                     AND x.grantee <> 'tappa_app'::pg_catalog.regrole)
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.holding_group()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.holding_peer(uuid)', 'EXECUTE')
       OR NOT pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.holdings', 'name', 'SELECT')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.holdings', 'created_at', 'SELECT')
       OR pg_catalog.has_table_privilege('tappa_holdingdefiner', 'public.holdings', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00062 (GRP-1): holding_peer''s roster form or holding_group is not the shape this file writes (owner, INVOKER/DEFINER, the search path, EXECUTE), or the definer reads more of holdings than (id, name). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Nothing stored: the callable first (its body names the roster form), then the roster form, then
-- the grant. No table lock.
SET LOCAL lock_timeout = '1s';
DROP FUNCTION IF EXISTS public.holding_group();
DROP FUNCTION IF EXISTS public.holding_peer(uuid);
REVOKE SELECT ON public.holdings FROM tappa_holdingdefiner;
