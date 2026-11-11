-- 00100 -- the user's decision of 2026-11-11: an employee's ACTIVATION TAP (ADR 0026) may complete on
-- a plaque of a LIVE PEER in the same holding (ADR 0027), and the peer's trail says so. ONE new
-- callable on ADR 0027 §2's closed list -- holding_activation_tap(p_uid, p_ctr) -- approved by the
-- user for exactly this purpose ("kullanıcı onaylı, 2026-11-11").
--
-- NORMATIVE SOURCES: docs/adr/0027-holding-ve-capraz-tenant-dokunusu.md §2 (the function contract, the
-- closed list, its signature table), §3 (tappa_holdingdefiner's verb x column matrix) and its
-- "Aktivasyon eşin plaketinde" note (2026-11-11); docs/adr/0026 (the activating tap) and its note of the
-- same day; CLAUDE.md §4.4 (one statement, the comparison inside it), §4.5 (the second exception).
--
-- WHY A FUNCTION AT ALL. The activation advances the PEER's tags.last_ctr (holding_advance_tag_counter,
-- in the invitation tenant's context -- the employer's, as for every holding tap). A holding check-in
-- leaves a trail on the site (site_punches, holding_feed); an activation writes no transactions row,
-- so nothing would: one tenant's counter moved by another tenant's request, traceless on its side --
-- exactly the gap activate.go's "audit R5" closed for a non-peer. tappa_app cannot write another
-- tenant's audit_log (RLS, WITH CHECK), so the row is written by the definer.
--
-- WHY NOT A FREE-STANDING TRAIL WRITER. A function that only inserted the row would let tappa_app write
-- the peer's trail without anything having happened. This one BINDS the row to the advance: it calls
-- holding_advance_tag_counter (the existing single statement, unchanged -- the counter is still moved
-- by that statement and by nothing here) and writes the row ONLY when the outcome is 'advanced', in the
-- same transaction. No advance, no row; an advance on this path, always a row (a failed INSERT raises
-- and the advance rolls back with it). The peer check is the advance's own (strict, left_at >
-- clock_timestamp(), in the statement that locks the plaque): 'advanced' means "a live peer's active
-- plaque, and its counter moved" -- K-29's exit window is the BEFORE gate's, for transactions rows, and
-- does not reach this path, as it does not reach the advance of a holding check-in.
--
-- THE ROW (the site may already know every fact in it -- its own plaque, its own wall):
--   tenant_id  the plaque's tenant (the PEER), with an explicit predicate: the plaque by uid and a
--              tenant that is not the caller's (an own plaque never reaches this function -- and if
--              one did, the advance would have answered not_peer).
--   actor_id   NULL (no admin acted).
--   action     'holding.activation_tap' -- fixed in the body, no argument reaches it.
--   target     the plaque's uid.
--   detail     {"location_id": the plaque's wall}.
--   at         the database's DEFAULT now() (CLAUDE.md §6: the trail's clock is the database's).
-- NOTHING ABOUT THE PERSON OR THE EMPLOYER: no employee id or name, no invitation, no employer tenant
-- id -- the site learns that a plaque of its own was used to activate a phone, and nothing else.
--
-- THE MATRIX GROWS BY ONE ROW: INSERT (tenant_id, action, target, detail) ON audit_log -- no SELECT, no
-- actor_id, no at. The definer's tags columns are 00043's (uid, tenant_id, location_id).
--
-- Its result is the advance's outcome and the plaque's status AT THE LOCK -- (outcome text, tag_status
-- text), the four outcomes of holding_advance_tag_counter, always one row. ctr_gap is not returned: an
-- activation is not judged, so nothing reads it.
--
-- No table lock: CREATE FUNCTION, GRANT and REVOKE take none on a relation. lock_timeout first anyway.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.to_regprocedure('public.holding_advance_tag_counter(character, integer)') IS NULL
       OR pg_catalog.pg_get_function_result('public.holding_advance_tag_counter(character, integer)'::pg_catalog.regprocedure)
          <> 'TABLE(outcome text, tag_status text, ctr_gap integer)' THEN
        RAISE EXCEPTION 'migration 00100 needs holding_advance_tag_counter as 00045 made it. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM pg_catalog.pg_proc p WHERE p.proname = 'holding_activation_tap') THEN
        RAISE EXCEPTION 'migration 00100 creates holding_activation_tap, and one is already there. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    -- The Down takes back INSERT on audit_log whole: it may only take back what this file gave.
    IF pg_catalog.has_table_privilege('tappa_holdingdefiner', 'public.audit_log', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.audit_log', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.audit_log', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00100: tappa_holdingdefiner already reaches audit_log; this file grants INSERT on four columns and its Down takes the table back. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

GRANT INSERT (tenant_id, action, target, detail) ON public.audit_log TO tappa_holdingdefiner;

-- +goose StatementBegin
CREATE FUNCTION public.holding_activation_tap(p_uid character, p_ctr integer)
    RETURNS TABLE(outcome text, tag_status text)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_caller  uuid;
    v_outcome text;
    v_status  text;
    v_written integer;
BEGIN
    v_caller := NULLIF(pg_catalog.current_setting('app.tenant_id', true), '')::uuid;

    -- The one counter statement (§4.4), its peer check inside it. Nothing here compares a counter.
    SELECT a.outcome, a.tag_status INTO v_outcome, v_status
      FROM public.holding_advance_tag_counter(p_uid, p_ctr) AS a;

    IF v_outcome = 'advanced' THEN
        -- The plaque's row is locked FOR UPDATE by the advance until this transaction ends, so its
        -- tenant is the one the advance judged a peer.
        INSERT INTO public.audit_log (tenant_id, action, target, detail)
        SELECT t.tenant_id, 'holding.activation_tap', t.uid,
               pg_catalog.jsonb_build_object('location_id', t.location_id)
          FROM public.tags AS t
         WHERE t.uid = p_uid
           AND v_caller IS NOT NULL
           AND t.tenant_id <> v_caller;
        GET DIAGNOSTICS v_written = ROW_COUNT;
        IF v_written <> 1 THEN
            -- Unreachable while the advance's peer check stands. Raised, never swallowed: the
            -- advance rolls back with it, so a peer's counter never moves on this path untrailed.
            RAISE EXCEPTION 'holding_activation_tap: the peer''s trail row was not written'
                USING ERRCODE = 'internal_error';
        END IF;
    END IF;

    RETURN QUERY SELECT v_outcome, v_status;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.holding_activation_tap(character, integer) OWNER TO tappa_holdingdefiner;
REVOKE ALL ON FUNCTION public.holding_activation_tap(character, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.holding_activation_tap(character, integer) TO tappa_app;

-- POSTCONDITION: the shape above, the grants exactly these.
-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.pg_get_userbyid((SELECT p.proowner FROM pg_catalog.pg_proc p
                                    WHERE p.oid = 'public.holding_activation_tap(character, integer)'::pg_catalog.regprocedure))
           <> 'tappa_holdingdefiner'
       OR (SELECT NOT p.prosecdef OR p.proconfig IS DISTINCT FROM ARRAY['search_path=pg_catalog, pg_temp']
             FROM pg_catalog.pg_proc p WHERE p.oid = 'public.holding_activation_tap(character, integer)'::pg_catalog.regprocedure)
       OR EXISTS (SELECT 1
                    FROM pg_catalog.pg_proc p,
                         pg_catalog.aclexplode(coalesce(p.proacl, pg_catalog.acldefault('f', p.proowner))) AS x
                   WHERE p.oid = 'public.holding_activation_tap(character, integer)'::pg_catalog.regprocedure
                     AND x.privilege_type = 'EXECUTE' AND x.grantee <> p.proowner
                     AND x.grantee <> 'tappa_app'::pg_catalog.regrole)
       OR NOT pg_catalog.has_function_privilege('tappa_app', 'public.holding_activation_tap(character, integer)', 'EXECUTE')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.audit_log', 'actor_id', 'INSERT')
       OR pg_catalog.has_column_privilege('tappa_holdingdefiner', 'public.audit_log', 'at', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_holdingdefiner', 'public.audit_log', 'SELECT') THEN
        RAISE EXCEPTION 'migration 00100: holding_activation_tap is not the shape this file writes (owner, DEFINER, the search path, EXECUTE), or the definer reaches more of audit_log than INSERT on four columns. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- The rows the function wrote stay (audit_log is append-only); only the function and the grant go.
SET LOCAL lock_timeout = '1s';
DROP FUNCTION IF EXISTS public.holding_activation_tap(character, integer);
REVOKE INSERT ON public.audit_log FROM tappa_holdingdefiner;
