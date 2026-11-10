-- 00099 -- T59 security review (ORTA-2): a tap session's revocation is one way IN THE DATABASE.
--
-- WHY. sessions.revoked_at was one way only by convention (db/queries/sessions.sql says so in as many
-- words: "That is a property of this file, not an enforcement by the database"). tappa_app holds
-- table-wide UPDATE on sessions (00003) and no trigger guarded the column, so -- measured on a lane
-- database before this file -- `UPDATE sessions SET revoked_at = NULL ... RETURNING` made a revoked
-- session live again. Since T59 a manager's "Sign out their phones" IS that revocation: a kill switch
-- for a stolen phone, which must not have an off position. This is the trigger 00011 invited for
-- `sessions` beside its admin_sessions_revocation_monotonic, written in 00051's stricter shape.
--
-- WHAT IT REFUSES (SQLSTATE 23001, restrict_violation):
--   * a stamped revocation changing at all -- NOT NULL -> NULL (un-revoking) and NOT NULL -> another
--     instant (back- or forward-dating the answer to "when did this session die");
--   * a FIRST stamp that is back-dated or in the future -- 00011's BOUNDARY 1, closed here: NULL -> t is
--     refused when t < now() (before this transaction began) or t > clock_timestamp().
-- An idempotent COALESCE(revoked_at, now()) on a revoked row writes the same value, so the WHEN clause
-- is false and the function is never entered; so is every last_used_at refresh.
--
-- WHAT THE PREVIOUS BINARY DOES ON THIS SCHEMA: the same. Its three writers of revoked_at are in
-- db/queries/sessions.sql -- RevokeSession (COALESCE(revoked_at, now())), RevokeSessionsForEmployee
-- (now(), `revoked_at IS NULL` in its WHERE) and TouchSession (does not write it) -- and no migration
-- body updates sessions (read: every UPDATE on sessions in db/ and internal/). Each writes now(), which
-- is inside the bound, or nothing. Concurrency stays safe for the reason 00011's BOUNDARY 2 measured:
-- both statements re-check `revoked_at IS NULL` / re-evaluate COALESCE against the committed row.
--
-- SECURITY INVOKER, EXECUTE in its owner alone (ADR 0028 §1's rule; 00061 for the reason), its body
-- asserting where it is bound (00051's api_keys_revocation_is_one_way: a copy bound elsewhere refuses).
--
-- 🔴 AND tappa_app's UPDATE ON sessions NARROWS TO (last_used_at, revoked_at) -- 00011's admin_sessions
-- pattern (security re-check of this file, ORTA-2's second half). The trigger alone guarded one
-- column of a table tappa_app could update whole: measured on a lane database as tappa_app, a revoked
-- row's token_hash could be parked (`SET token_hash = 'parked'`) and the stolen cookie's hash written
-- into a live row (UPDATE 1) or a new one (INSERT 0 1), and the stolen cookie resolved as live again.
-- After this file token_hash, employee_id, tenant_id, id, device_info and created_at are not
-- updatable by tappa_app at all (42501). The product's three UPDATEs write only the two kept columns:
-- TouchSession sets last_used_at, RevokeSession and RevokeSessionsForEmployee set revoked_at (read,
-- db/queries/sessions.sql); their WHERE and RETURNING clauses need SELECT, which is unchanged.
--
-- INSERT IS LEFT TABLE-LEVEL, and what that leaves is stated rather than claimed away. CreateSession
-- (the activation path) names four columns -- tenant_id, employee_id, token_hash, device_info -- so a
-- column list would be possible, but it would not stop the one INSERT that matters: re-inserting a
-- stolen cookie's hash names exactly those four. What stops it is sessions_token_hash_key (00003's
-- global UNIQUE on token_hash) while the revoked row exists -- 23505 -- and the revoked row cannot be
-- moved out of the way any more: tappa_app has no DELETE on sessions (00003) and, after this file, no
-- UPDATE of token_hash, so parking it is impossible. (Many test fixtures insert sessions with an id,
-- which a four-column grant would refuse for no gain.)
--
-- 🔴 lock_timeout 1 s, the FIRST statement of the Up and of the Down (00054's rule, 00037's wording):
-- CREATE/DROP TRIGGER take SHARE ROW EXCLUSIVE on sessions, which every tap's TouchSession writes.
-- Behind a long reader the file gives up with 55P03 instead of queueing every tap behind its lock.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
CREATE FUNCTION public.sessions_revocation_is_one_way()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'UPDATE'
       OR TG_RELID <> 'public.sessions'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'sessions_revocation_is_one_way: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF OLD.revoked_at IS NOT NULL THEN
        RAISE EXCEPTION 'session revocation is one way: revoked_at may go from NULL to a timestamp, never back and never to another instant'
            USING ERRCODE = 'restrict_violation';
    END IF;
    IF NEW.revoked_at < pg_catalog.now() OR NEW.revoked_at > pg_catalog.clock_timestamp() THEN
        RAISE EXCEPTION 'session revocation is stamped when it happens: revoked_at may not be back-dated or set in the future'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.sessions_revocation_is_one_way() FROM PUBLIC;

-- WHEN carries the condition: only a write that CHANGES revoked_at enters the function.
CREATE TRIGGER sessions_revocation_one_way
    BEFORE UPDATE ON public.sessions
    FOR EACH ROW
    WHEN (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at)
    EXECUTE FUNCTION public.sessions_revocation_is_one_way();

-- GRANT and REVOKE take no relation lock (measured, holdingtap_test's note).
REVOKE UPDATE ON public.sessions FROM tappa_app;
GRANT UPDATE (last_used_at, revoked_at) ON public.sessions TO tappa_app;

-- +goose Down
SET LOCAL lock_timeout = '1s';
-- 00003's grant back: table-level UPDATE (a table REVOKE also takes the column grant away).
REVOKE UPDATE ON public.sessions FROM tappa_app;
GRANT UPDATE ON public.sessions TO tappa_app;
DROP TRIGGER IF EXISTS sessions_revocation_one_way ON public.sessions;
DROP FUNCTION IF EXISTS public.sessions_revocation_is_one_way();
