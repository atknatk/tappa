-- 00060 -- T124: holding_peer's SET FORM, and the two multi-row holding_* functions read the peer set
-- through it instead of walking every tenant.
--
-- NORMATIVE SOURCES: docs/adr/0027-holding-ve-capraz-tenant-dokunusu.md §2 (the one predicate, K-33;
-- "holding_members'a gövdesinde değinen TEK fonksiyon holding_peer'dir"; the closed list and its
-- signature table; "Maliyet -- eş kümesi bir kez"), §13 item 21 and its "T124 uygulama notu" (the
-- user's decision of 2026-10-18: T124 is closed now, by a mode of holding_peer that returns the set);
-- migration 00055 (the half of T124 the membership gate closed -- the precedent this file replaces).
--
-- THE COST IT REMOVES. 00047 and 00055 compute a member's peer set as
--     SELECT s.id FROM tenants s WHERE holding_peer(caller, s.id, 'strict')
-- -- one PL/pgSQL call and one membership join per TENANT of the database, on every call: a member's
-- directory page and its venue list paid ~1 s at 21 000 tenants (ADR 0028's API-8 note; under the
-- full -race load past the 5 s API read bound -> 503, backlog T129), and a key's 600 requests a
-- minute made that a DoS lever. 00055's is_member gate spared businesses in no holding; a member
-- still paid the walk.
--
-- THE SET FORM -- holding_peer(a uuid, mod text) RETURNS SETOF uuid. The same NAME, a second
-- signature: the answer to "who are a's peers" lives where the answer to "are a and b peers" lives,
-- so ADR 0027 §2's rule -- the ONE function whose body names holding_members is holding_peer
-- (TestHoldingPeer_IsTheOnlyBodyThatTouchesHoldingMembers, by name) -- holds as written, and no
-- holding_* callable reads the membership table itself. Its modes are a CLOSED set of one:
--
--   strict   every b for which holding_peer(a, b, 'strict') is true: the same holding, both
--            memberships live by the wall clock (left_at IS NULL OR left_at > the instant), a
--            itself never in it; a NULL a is the empty set
--
-- and anything else is 22023 (never a default). The set is the pair form's answer, b by b
-- (TestHoldingPeer_TheSetFormIsThePairFormsAnswer -- a property test over random holdings,
-- memberships, exits and rejoins: for every caller and every candidate, b in the set exactly when
-- the pair form says true). The wall clock is read ONCE per call (one instant for the whole set --
-- holding_advance_tag_counter's "üyelik tek CTE'de bir kez" rule); the pair form reads it per
-- membership row of its one statement -- the same instant to within that statement.
--
-- Its shape is the pair form's: SECURITY INVOKER, VOLATILE, search_path pinned, owned by
-- tappa_holdingdefiner, EXECUTE in its owner alone (tappa_app may never ask it: a free-argument
-- membership oracle -- ADR 0027 §2, 9th round), no row lock and no write (it decides; it locks
-- nothing -- the lock mode stays the pair form's), no dynamic SQL. ROWS 10: a holding is a handful
-- of businesses, not the planner's default thousand.
--
-- THE TWO CALLERS. holding_site_locations and holding_directory take their peer set from ONE call
-- of the set form (still once, in a materialised CTE -- "eş kümesi bir kez"), and the 3-argument
-- holding_peer is no longer asked by them at all (TestHoldingFunctions_AskThePeerSetOnceNeverPerTenant
-- counts it). 00055's is_member gate goes: the set form answers a business in no holding with one
-- index probe. Their results, their order and their pages are 00055's, row for row
-- (TestHoldingFunctions_00060AnswersWhat00055Answered: the same data through both bodies).
-- holding_directory keeps its belt (the caller's own id is kept out of the peer set by the set form
-- AND by `s.id <> c.id`) and still reads the peers' tenants rows for api_code; holding_site_locations
-- needs nothing of tenants (holding_members' key to tenants makes every member a tenant).
--
-- MEASURED (a disposable PostgreSQL 17, 21 000 tenants -- API-8's layout; ADR 0027's T124 note):
-- the numbers are in the ADR.
--
-- No table lock: CREATE FUNCTION and CREATE OR REPLACE take none on a relation. The Down gives back
-- 00055's two bodies byte for byte (the precondition holds both to them first) and drops the set
-- form.

-- +goose Up

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.holding_directory(timestamptz, uuid)') IS NULL
       OR pg_catalog.to_regprocedure('public.holding_site_locations(uuid)') IS NULL
       OR pg_catalog.to_regprocedure('public.holding_peer(uuid, uuid, text)') IS NULL THEN
        RAISE EXCEPTION 'migration 00060 (T124) needs migration 00055. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p WHERE p.proname = 'holding_peer'
                                                    AND p.oid <> 'public.holding_peer(uuid, uuid, text)'::pg_catalog.regprocedure) THEN
        RAISE EXCEPTION 'migration 00060 (T124) creates holding_peer''s set form, and a second holding_peer is already there. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- The set form must answer what THIS pair form answers; and the Down gives back 00055's bodies.
    IF (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p WHERE p.oid = 'public.holding_peer(uuid, uuid, text)'::pg_catalog.regprocedure)
           IS DISTINCT FROM '8044a5597320487de4f1d19bed399a0e'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p WHERE p.oid = 'public.holding_site_locations(uuid)'::pg_catalog.regprocedure)
           IS DISTINCT FROM '8401897e362b1ae818f67c5fe2272c0c'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p WHERE p.oid = 'public.holding_directory(timestamptz, uuid)'::pg_catalog.regprocedure)
           IS DISTINCT FROM '3778704c97ca47c3bf355af8ff0610ac' THEN
        RAISE EXCEPTION 'migration 00060 (T124) replaces holding_site_locations'' and holding_directory''s bodies beside holding_peer, and one of the three is not 00041''s / 00055''s. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- holding_peer's set form (ADR 0027 §2, T124 note). Every identifier is qualified; the one statement
-- is the strict row of the pair form's table, with b free.
-- +goose StatementBegin
CREATE FUNCTION public.holding_peer(a uuid, mod text)
    RETURNS SETOF uuid
    LANGUAGE plpgsql
    VOLATILE
    SECURITY INVOKER
    SET search_path = pg_catalog, pg_temp
    ROWS 10
AS $$
DECLARE
    v_now timestamptz;
BEGIN
    IF mod IS NULL OR mod NOT IN ('strict') THEN
        RAISE EXCEPTION 'holding_peer: unknown mode'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    IF a IS NULL THEN
        RETURN;
    END IF;

    v_now := clock_timestamp();
    RETURN QUERY
        SELECT DISTINCT mb.member_tenant_id
          FROM public.holding_members AS ma
          JOIN public.holding_members AS mb ON mb.holding_id = ma.holding_id
         WHERE ma.member_tenant_id = a
           AND mb.member_tenant_id <> a
           AND (ma.left_at IS NULL OR ma.left_at > v_now)
           AND (mb.left_at IS NULL OR mb.left_at > v_now);
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.holding_peer(uuid, text) OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_peer(uuid, text) FROM PUBLIC;

-- holding_site_locations: 00055's body less the gate, the peer set from the set form. CREATE OR
-- REPLACE keeps its owner, its EXECUTE grants and its signature.
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
        SELECT p.id
          FROM caller AS c
         CROSS JOIN LATERAL public.holding_peer(c.id, 'strict') AS p(id)
    )
    SELECT l.tenant_id, l.id, l.name, l.external_ref, l.shift_start, l.shift_end, l.overnight
      FROM public.locations AS l
      JOIN peers AS p ON p.id = l.tenant_id
     WHERE p_cursor IS NULL OR l.id > p_cursor
     ORDER BY l.id
     LIMIT 1000
$$;
-- +goose StatementEnd

-- holding_directory: 00055's body less the gate, the peer set from the set form joined to the peers'
-- tenants rows (api_code), the belt kept.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.holding_directory(p_updated_since timestamptz, p_cursor uuid)
    RETURNS TABLE(employee_id uuid, employer_tenant_id uuid, api_code text, external_ref text, employee_code text,
                  first_name text, last_name text, full_name text, active boolean, updated_at timestamptz)
    LANGUAGE sql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
    WITH caller AS MATERIALIZED (
        SELECT NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid AS id
    ), peers AS MATERIALIZED (
        SELECT s.id, s.api_code
          FROM caller AS c
         CROSS JOIN LATERAL public.holding_peer(c.id, 'strict') AS p(id)
          JOIN public.tenants AS s ON s.id = p.id
         WHERE s.id <> c.id
    ), bound AS MATERIALIZED (
        SELECT coalesce(p_updated_since, '-infinity'::timestamptz)
                   - CASE WHEN p_cursor IS NULL THEN interval '1 microsecond' ELSE interval '0' END AS at,
               coalesce(p_cursor, 'ffffffff-ffff-ffff-ffff-ffffffffffff'::uuid) AS id
    )
    SELECT e.id, e.tenant_id, p.api_code, e.external_ref, e.employee_code, e.first_name, e.last_name, e.full_name,
           e.status <> 'deactivated', e.updated_at
      FROM peers AS p
     CROSS JOIN bound AS a
     CROSS JOIN LATERAL (
            SELECT x.id, x.tenant_id, x.external_ref, x.employee_code, x.first_name, x.last_name, x.full_name, x.status, x.updated_at
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

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.pg_get_userbyid((SELECT p.proowner FROM pg_catalog.pg_proc p WHERE p.oid = 'public.holding_peer(uuid, text)'::pg_catalog.regprocedure))
           <> 'tappa_holdingdefiner'
       OR (SELECT p.prosecdef OR p.provolatile <> 'v' OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
             FROM pg_catalog.pg_proc p WHERE p.oid = 'public.holding_peer(uuid, text)'::pg_catalog.regprocedure)
       OR pg_catalog.pg_get_function_result('public.holding_peer(uuid, text)'::pg_catalog.regprocedure) <> 'SETOF uuid'
       OR EXISTS (SELECT 1
                    FROM pg_catalog.pg_proc p,
                         pg_catalog.aclexplode(coalesce(p.proacl, pg_catalog.acldefault('f', p.proowner))) AS x
                   WHERE p.oid = 'public.holding_peer(uuid, text)'::pg_catalog.regprocedure
                     AND x.privilege_type = 'EXECUTE' AND x.grantee <> p.proowner)
       OR pg_catalog.has_function_privilege('tappa_app', 'public.holding_peer(uuid, text)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.holding_site_locations(uuid)', 'EXECUTE')
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.holding_directory(timestamptz, uuid)', 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00060 (T124): holding_peer''s set form is not the pair form''s shape (owner, INVOKER, VOLATILE, the search path, SETOF uuid, EXECUTE in its owner alone), or tappa_app lost one of the two callables. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- Nothing stored: the two bodies go back to 00055's, byte for byte (the Up's precondition names their
-- md5s), and the set form goes after them -- no body names it any more. No table lock.
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
CREATE OR REPLACE FUNCTION public.holding_directory(p_updated_since timestamptz, p_cursor uuid)
    RETURNS TABLE(employee_id uuid, employer_tenant_id uuid, api_code text, external_ref text, employee_code text,
                  first_name text, last_name text, full_name text, active boolean, updated_at timestamptz)
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
    SELECT e.id, e.tenant_id, p.api_code, e.external_ref, e.employee_code, e.first_name, e.last_name, e.full_name,
           e.status <> 'deactivated', e.updated_at
      FROM peers AS p
     CROSS JOIN bound AS a
     CROSS JOIN LATERAL (
            SELECT x.id, x.tenant_id, x.external_ref, x.employee_code, x.first_name, x.last_name, x.full_name, x.status, x.updated_at
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
DROP FUNCTION IF EXISTS public.holding_peer(uuid, text);
