-- 00031 -- M10 OP-14, phase A (the data layer): the operator reads its OWN audit log --
-- every operator act, newest first, a page at a time, optionally one kind only -- through
-- one op_read_* and nothing else; and the pre-session audit kinds gain 'password_ok', the
-- trace of a sign-in whose password was accepted and whose second factor has not (yet)
-- been.
--
-- NORMATIVE SOURCES: docs/adr/0020-platform-operatoru-ayri-kimlik.md §3 (the pre-session
-- writer) and §5 (the audit), docs/adr/0021-op-fonksiyonlari-tenant-otesi-erisim.md §1
-- (tappa_operator never SELECTs operator_audit_log: "goruntuleyici iki asamali
-- op_read_audit ile okur -- OP-14"), §2 (the op_* contract: the session predicate,
-- two-phase reads, the wall clock, the LIMIT ceiling, the fixed column list) and §6 (the
-- catalogue and behaviour pins). Where this file decides something those leave open, the
-- decision is written next to the statement and in ADR 0021's "OP-14 uygulama notu".
--
-- WHAT IT DOES, IN ORDER:
--   0. re-checks the two cluster roles' shape -- 00026's precondition, repeated as 00027,
--      00029 and 00030 repeated it, because a new SECURITY DEFINER function is about to
--      belong to tappa_opdefiner and two existing ones are replaced;
--   1. the audit kinds: operator_audit_log_kind_check gains 'password_ok', and
--      operator_audit_log_actor_shape puts it in the PRE-SESSION arm (no session, no
--      actor) -- both re-added under their names, NOT VALID only when a row of a kind
--      outside the new set already exists (a later migration's Down can leave one);
--   2. the read kinds: operator_read_tickets_kind_check gains 'operator_audit', under the
--      same NOT VALID rule;
--   3. the grants: tappa_opdefiner gains a column SELECT on every operator_audit_log
--      column but id (which 00027 granted) -- the read below needs them; nothing else;
--   4. op_record_auth_event is REPLACED: its closed set names 'password_ok' (by account
--      id only; the lock counter is not touched), and a constraint its write meets is
--      caught and answered without the failing row;
--   5. op_begin_read is REPLACED: one more read kind, 'operator_audit', with its own
--      parameter object and page bound; its four other branches are 00030's;
--   6. one function: op_read_audit.
--
-- No table is created here, so there is no redline waiver, no new RLS and no sequence to
-- REVOKE. tappa_app gains nothing (ADR 0021 §1); tappa_operator gains EXECUTE on the one
-- new function and no privilege on any table -- it still holds NO SELECT on
-- operator_audit_log (TestOperator00026_PrivilegeMatrix).
--
-- CLOCKS (ADR 0021 §2 vii): the one comparison the read makes (expires_at >
-- clock_timestamp()) and the one timestamp it writes (consumed_at) come from
-- clock_timestamp(); op_begin_read's and op_record_auth_event's clocks are unchanged
-- (00027's and 00026's). The catalogue scan of ADR 0021 §6 (TestOperator00026_NoFrozenClock)
-- walks every function tappa_opdefiner owns.
--
-- SECRETS (ADR 0021 §3.5, CLAUDE.md §7): the RAISE EXCEPTIONs carry constant messages; the
-- one formatted RAISE EXCEPTION is the precondition's (it formats role NAMES); the RAISE
-- LOGs format a constraint name and a SQLSTATE, never a value. The read does NOT return
-- the audit row's detail: a closed list of shapes is read out of it (section 6) and a row
-- whose detail is not on the list is marked, not shown. A read ticket, a TOTP code, a
-- token or a search term therefore has no column to leave through -- none of them is
-- written to the log in the first place (00026, 00029), and a value some later writer
-- put into detail by mistake is not echoed.
--
-- 🔴 THE OWNER WRITES `at` (measured, 2026-10-03 and again before writing this file:
-- tappa_owner holds INSERT on every column of operator_audit_log, `at` included -- it is
-- the table's owner, and a superuser on the deployed topology). The definer cannot
-- (00026: `at` is in nobody's INSERT list; TestOperator00026_PrivilegeMatrix's named
-- cell). What follows for the read, named: a row the OWNER writes carries the time the
-- owner chose, so the read's order -- newest first -- is the owner's claim for such a
-- row; a row dated in the future heads every unfiltered first page, above the viewer's
-- own 'read' row. ADR 0021 limit 5 already counts what a superuser can do to this table;
-- the consequence for the viewer is counted in the OP-14 note.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION: the two roles still have the shape ADR 0021 §1 names.
-- ---------------------------------------------------------------------------
-- 00026's checks, in 00026's order, as 00027, 00029 and 00030 repeated them: an op_*
-- owned by a SUPERUSER, a LOGIN or a member-carrying tappa_opdefiner is a general bypass
-- with every other check green, and a member of tappa_operator inherits EXECUTE on the
-- function below -- which reads every operator act and names every tenant they touched.
-- +goose StatementBegin
DO $$
DECLARE
    v_missing text;
BEGIN
    SELECT string_agg(r.name, ', ' ORDER BY r.name)
      INTO v_missing
      FROM unnest(ARRAY['tappa_operator', 'tappa_opdefiner']) AS r(name)
     WHERE NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles p WHERE p.rolname = r.name);
    IF v_missing IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00031 (M10 OP-14) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00031 (M10 OP-14): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00031 (M10 OP-14): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00031 (M10 OP-14): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00031 (M10 OP-14): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00031 (M10 OP-14): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00031 (M10 OP-14): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1: tappa_app gets no new privilege). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The audit kinds: 'password_ok', a PRE-SESSION kind.
-- ---------------------------------------------------------------------------
--   'password_ok'  written by op_record_auth_event (section 4) when the process has
--                  accepted an operator's PASSWORD and the second factor is still to
--                  come. Before this file such a sign-in left a process log line only
--                  (internal/operatorauth, the OP-6 card's 12c): a password known to
--                  someone without the operator's device left no lasting trace unless
--                  that someone then TRIED a code. The row claims no session and no
--                  actor (the session does not exist yet) and names the account by the
--                  id the process holds; it is not a success of the sign-in -- that is
--                  the 'login' row op_open_session writes with the session it opens.
-- 🔴 BOTH CONSTRAINTS MOVE TOGETHER (the OP-14 card's T1): the kind set alone would put
-- 'password_ok' in actor_shape's SESSION arm -- every row of it would need a session and
-- an actor, and op_record_auth_event's INSERT would fail. Both keep their NAMES, so the
-- next migration finds them where this one did (00027's and 00030's practice).
-- NOT VALID, AND WHEN: a row whose kind the new set does not name can only be one a LATER
-- migration wrote and whose Down left it behind under a NOT VALID CHECK. With such a row
-- present a VALIDATED re-add fails with 23514 (00030's counted limit L11, for tickets);
-- so both CHECKs are then added NOT VALID -- binding every new row, not re-checking the
-- old one -- and VALIDATED otherwise. The same question for tickets is section 2's.
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_actor_shape;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_audit_log
                WHERE kind <> ALL (ARRAY['login_failed', 'unknown_email', 'totp_failed', 'locked',
                                         'enrollment_failed', 'password_ok', 'login', 'enrollment',
                                         'logout', 'read', 'legal_publish'])) THEN
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish'))
            NOT VALID;
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_actor_shape CHECK (
            (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                      'enrollment_failed', 'password_ok')
             AND session_id IS NULL AND actor_admin_id IS NULL)
            OR
            (kind NOT IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                          'enrollment_failed', 'password_ok')
             AND session_id IS NOT NULL AND actor_admin_id IS NOT NULL)
        ) NOT VALID;
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish'));
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_actor_shape CHECK (
            (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                      'enrollment_failed', 'password_ok')
             AND session_id IS NULL AND actor_admin_id IS NULL)
            OR
            (kind NOT IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                          'enrollment_failed', 'password_ok')
             AND session_id IS NOT NULL AND actor_admin_id IS NOT NULL)
        );
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 2. The read kinds: 'operator_audit'.
-- ---------------------------------------------------------------------------
--   'operator_audit'  op_read_audit: a page of the operator's own audit log.
-- No audit KIND is added for it: the read is one 'read' row whose target_scope is the read
-- kind (00027 section 1) -- the viewer's own row, written before anything is shown.
-- NOT VALID under section 1's rule, for tickets: a ticket of a kind outside the five can
-- only be a later migration's, left behind by its Down -- consumed or not: production's
-- tickets are consumed, and a consumed ticket is a row the CHECK refuses all the same, so
-- the condition is the kind alone (the Down's likewise; both measured with consumed tickets).
ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_read_tickets
                WHERE kind <> ALL (ARRAY['legal_versions', 'tenants', 'tenant_detail',
                                         'tenant_plaques', 'operator_audit'])) THEN
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit'));
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 3. Grants: what the read touches and no earlier file granted.
-- ---------------------------------------------------------------------------
-- tappa_opdefiner, column by column (ADR 0021 §1; the WHOLE resulting list is pinned in
-- TestOperator00026_PrivilegeMatrix):
--   operator_audit_log  every column but id (00027 granted id for op_begin_read's
--                       RETURNING). SELECT only: no UPDATE, no DELETE -- the table is
--                       append-only and its triggers bind the owner too (00026) -- and
--                       still no INSERT on `at`.
--   platform_admins     nothing new: id (00026) and display_name (00027) name the actor
--                       and the target account.
--   tenants             nothing new: id and name (00029) name the tenant a row touched.
-- 🔴 id is NOT granted again, and this file's Down does NOT revoke it (and does not write
-- REVOKE ALL): a column holds ONE ACL entry per grantee, so taking back id would take
-- 00027's grant with it and op_begin_read's INSERT ... RETURNING id would fail (00030's
-- lesson, the same trap).
-- tappa_operator gets NOTHING on operator_audit_log: it reads the log through
-- op_read_audit only (ADR 0021 §1). tappa_app's grants are not touched.
GRANT SELECT (at, kind, session_id, actor_admin_id, target_admin_id, target_tenant_id,
              target_scope, page_number, page_size, detail)
    ON operator_audit_log TO tappa_opdefiner;

-- ===========================================================================
-- 4. op_record_auth_event, REPLACED: 'password_ok', and no failing row in an error
-- ===========================================================================
-- Everything 00026 section 5.2 says of this function still holds -- sessionless
-- (ADR 0021 §1's first named exception), no actor claimed, the address never stored (not
-- even hashed), the target resolved HERE by the database's own lookup, RETURNS void, the
-- counter moved by 'totp_failed' alone, in the same statement that writes its row, and
-- only on an active account. What changes:
--   * THE CLOSED SET names 'password_ok'. It is PRE-SESSION -- the password is the first
--     factor -- but it is not a failure, so the refusal's message no longer says
--     "failure" (22023 as before, for a kind outside the set).
--   * 'password_ok' NAMES ITS ACCOUNT BY ID ONLY: p_admin must be given and p_email must
--     not (22023 otherwise, before anything is written -- a caller bug, decided by the
--     arguments alone, so it is not a state oracle). The process holds the id of the
--     account whose digest it has just compared; an address would be a second key for the
--     same account, and the address of an unknown sign-in is never written (00026).
--   * 🔴 THE COUNTER IS NOT TOUCHED: the UPDATE still runs for 'totp_failed' only.
--     A row that says "the password was right" must not lock the account it names, and
--     it must not unlock it either (only op_open_session resets the counter, with a
--     session). TestOpRecordAuthEvent_PasswordOKNamesItsAccountAndTouchesNoCounter.
--   * 🔴 A CONSTRAINT THE WRITE MEETS IS CAUGHT (the OP-14 card's T1 named the hole: the
--     function had no handler, so a constraint violation answered the caller -- and the
--     server log -- with PostgreSQL's DETAIL, "Failing row contains (...)", which holds
--     the row: the target account's id among it, i.e. whether the address or the id named
--     an account). No argument reaches a constraint while the closed set and the CHECKs
--     agree; two ways still reach one: the two drifting apart (section 1 is that drift's
--     exact shape if its actor_shape half were forgotten) and the target account deleted
--     between the lookup and the foreign-key check. Caught, a LOG line names the
--     constraint and the SQLSTATE -- never a value -- and the caller gets ONE fixed message
--     with the caught SQLSTATE and no DETAIL. Not 28000: this is not the refusal of an
--     operator but a broken write, and it stays visible as what it is.
--     TestOpRecordAuthEvent_AConstraintRefusalCarriesNoRow -- which reads the LOG line
--     itself, word for word, through the channel a caller has to it (client_min_messages =
--     log; ADR 0021's OP-14 note, limit L10); the server log is not read by a test.
-- ⚠️ Counted (ADR 0021 limit 7, corrected in the same change): a holder of
-- tappa_operator's DSN can call this with 'password_ok' and write a false "the password
-- is known" row for any account id. It opens no session and moves no counter.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_record_auth_event(p_kind text,
                                                       p_email text DEFAULT NULL,
                                                       p_admin uuid DEFAULT NULL)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_constraint text;
    v_state      text;
BEGIN
    IF p_kind IS NULL OR p_kind NOT IN ('login_failed', 'unknown_email', 'totp_failed',
                                        'locked', 'enrollment_failed', 'password_ok') THEN
        RAISE EXCEPTION 'op_record_auth_event: kind is not a pre-session kind'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF p_kind = 'password_ok' AND (p_admin IS NULL OR p_email IS NOT NULL) THEN
        RAISE EXCEPTION 'op_record_auth_event: password_ok names its account by id alone'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    BEGIN
        WITH target AS (
            SELECT a.id
              FROM public.platform_admins AS a
             WHERE (p_email IS NOT NULL AND a.email OPERATOR(public.=) p_email::public.citext)
                OR (p_email IS NULL AND a.id = p_admin)
        ), counted AS (
            UPDATE public.platform_admins AS a
               SET totp_failures     = a.totp_failures + 1,
                   totp_locked_until = CASE WHEN a.totp_failures + 1 >= 5
                                            THEN clock_timestamp() + interval '15 minutes'
                                            ELSE a.totp_locked_until
                                       END
              FROM target AS t
             WHERE p_kind = 'totp_failed'
               AND a.id = t.id
               AND a.status = 'active'
            RETURNING a.id
        )
        INSERT INTO public.operator_audit_log (kind, target_admin_id)
        SELECT p_kind, (SELECT t.id FROM target AS t);
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_record_auth_event: the audit row failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_record_auth_event: the audit row was refused'
                USING ERRCODE = v_state;
    END;
END;
$$;
-- +goose StatementEnd
-- CREATE OR REPLACE keeps the owner and the ACL; they are written again so this file,
-- read alone, shows the whole contract of the function it (re)defines (ADR 0021 §2 vi).
ALTER FUNCTION public.op_record_auth_event(text, text, uuid) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_record_auth_event(text, text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_record_auth_event(text, text, uuid) TO tappa_operator;

-- ===========================================================================
-- 5. op_begin_read, REPLACED: one more read kind
-- ===========================================================================
-- Everything 00027 section 4.1, 00029 section 3 and 00030 section 3 say of this function
-- still holds: the session through op_touch_session, the read's 'read' row and its ticket
-- written by ONE statement and tied by audit_id, the raw ticket drawn from three
-- gen_random_uuid() (TestOpBeginRead_TheTicketIsDrawnFromTheStrongRandomSource), 30
-- seconds of life, the hashed text rebuilt from TYPED values (v_bound), the caught
-- constraint, and the four kinds 00030 named, branch for branch. What changes:
--   * the closed set names 'operator_audit';
--   * its parameter object is EXACTLY {kind, page_number, page_size}:
--       kind         a JSON string: '' (every kind) or a member of operator_audit_log's
--                    kind set (section 1) -- the literal list below; a fourth copy of the
--                    set lives in internal/db (OperatorAuditKinds), and the four are held
--                    equal by TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree;
--       page_number  1..1000 -- THE OFFSET BOUND (the OP-14 card's K14-4, the tenant
--                    list's screen constant): the log only grows, and a page is OFFSET
--                    (page-1) x size over it; past page 1000 a kind filter is the way
--                    in, until that kind itself passes 200,000 rows (ADR 0021 L14);
--       page_size    1..200, the shared page check;
--   * its audit row: target_scope 'operator_audit', the page, and detail {"filter": <the
--     kind>} -- or {"filter": "all"} for '' -- a value of the closed set, never free text
--     (the OP-14 card's K14-3: the trail says WHAT kind was looked for).
-- The refusals keep 00027's two messages and codes.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_begin_read(p_session text, p_kind text, p_params jsonb)
    RETURNS text
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session     uuid;
    v_admin       uuid;
    v_keys        text[];
    v_want        text[];
    v_page_number integer;
    v_page_size   integer;
    v_query       text;
    v_filter      text;
    v_tenant      uuid;
    v_detail      jsonb := '{}'::jsonb;
    v_bound       jsonb;
    v_ticket      text;
    v_rows        bigint;
    v_constraint  text;
    v_state       text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    -- Nested rather than one OR chain: SQL does not promise to evaluate OR's arms in
    -- order, jsonb_object_keys on a non-object raises instead of answering false, and a
    -- NULL arm would turn the whole condition NULL -- which IF reads as false.
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'operator_audit') THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_keys := (SELECT array_agg(k.key ORDER BY k.key) FROM jsonb_object_keys(p_params) AS k(key));

    IF p_kind IN ('tenant_detail', 'tenant_plaques') THEN
        IF v_keys IS DISTINCT FROM ARRAY['tenant_id']
           OR jsonb_typeof(p_params -> 'tenant_id') IS DISTINCT FROM 'string' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        IF ((p_params ->> 'tenant_id') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') IS NOT TRUE THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_tenant := (p_params ->> 'tenant_id')::uuid;
        v_bound := jsonb_build_object('tenant_id', v_tenant);
    ELSE
        -- (Two IFs, not a CASE inside the condition: PL/pgSQL ends an IF condition at the
        -- first THEN it reads, a CASE's included.)
        IF p_kind = 'tenants' THEN
            v_want := ARRAY['page_number', 'page_size', 'query'];
        ELSIF p_kind = 'operator_audit' THEN
            v_want := ARRAY['kind', 'page_number', 'page_size'];
        ELSE
            v_want := ARRAY['page_number', 'page_size'];
        END IF;
        IF v_keys IS DISTINCT FROM v_want
           OR jsonb_typeof(p_params -> 'page_number') IS DISTINCT FROM 'number'
           OR jsonb_typeof(p_params -> 'page_size') IS DISTINCT FROM 'number' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        -- Plain positive integers only: page_number 1..999999999, page_size 1..200 (the
        -- LIMIT ceiling of ADR 0021 §2 iii, and operator_audit_log's own CHECK).
        IF (p_params ->> 'page_number') !~ '^[1-9][0-9]{0,8}$'
           OR (p_params ->> 'page_size') !~ '^[1-9][0-9]{0,2}$' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_page_number := (p_params ->> 'page_number')::integer;
        v_page_size   := (p_params ->> 'page_size')::integer;
        IF v_page_size > 200 THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        IF p_kind = 'tenants' THEN
            IF jsonb_typeof(p_params -> 'query') IS DISTINCT FROM 'string' THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_query := p_params ->> 'query';
            IF char_length(v_query) > 254 THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_bound := jsonb_build_object('page_number', v_page_number, 'page_size', v_page_size,
                                          'query', v_query);
            -- The search CLASS (section 3's rules, in their order).
            IF v_query = '' THEN
                v_detail := jsonb_build_object('search', 'none');
            ELSIF v_query ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
                v_detail := jsonb_build_object('search', 'id');
            ELSIF strpos(v_query, '@') > 0 THEN
                v_detail := jsonb_build_object('search', 'address');
            ELSE
                v_detail := jsonb_build_object('search', 'text');
            END IF;
        ELSIF p_kind = 'operator_audit' THEN
            -- The log's page bound and the kind filter: '' or a member of the audit kind set.
            IF v_page_number > 1000 THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            IF jsonb_typeof(p_params -> 'kind') IS DISTINCT FROM 'string' THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_filter := p_params ->> 'kind';
            IF v_filter <> '' AND v_filter NOT IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                                                   'enrollment_failed', 'password_ok', 'login', 'enrollment',
                                                   'logout', 'read', 'legal_publish') THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_bound := jsonb_build_object('kind', v_filter, 'page_number', v_page_number,
                                          'page_size', v_page_size);
            IF v_filter = '' THEN
                v_detail := jsonb_build_object('filter', 'all');
            ELSE
                v_detail := jsonb_build_object('filter', v_filter);
            END IF;
        ELSE
            v_bound := jsonb_build_object('page_number', v_page_number, 'page_size', v_page_size);
        END IF;
    END IF;

    v_ticket := encode(sha256(uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())), 'hex');

    BEGIN
        WITH audit AS (
            INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id,
                                                   target_scope, page_number, page_size, detail)
            VALUES ('read', v_session, v_admin, v_tenant, p_kind, v_page_number, v_page_size, v_detail)
            RETURNING id
        )
        INSERT INTO public.operator_read_tickets (ticket_hash, session_id, kind, target_tenant_id,
                                                  audit_id, expires_at)
        SELECT encode(sha256(convert_to(v_ticket || v_bound::text, 'UTF8')), 'hex'),
               v_session, p_kind, v_tenant, audit.id, clock_timestamp() + interval '30 seconds'
          FROM audit;

        GET DIAGNOSTICS v_rows = ROW_COUNT;
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_begin_read: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            v_rows := 0;
    END;

    IF v_rows <> 1 THEN
        RAISE EXCEPTION 'op_begin_read: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    RETURN v_ticket;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_begin_read(text, text, jsonb) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_begin_read(text, text, jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_begin_read(text, text, jsonb) TO tappa_operator;

-- ===========================================================================
-- 6. op_read_audit -- a page of the operator's own audit log
-- ===========================================================================
-- The op_read_* shape 00027 gave the first read and TestOpRead_EveryReadConsumesItsTicketAsTheADRSays
-- pins for EVERY op_read_* by name: the session through op_touch_session first; ONE
-- consuming UPDATE of the ticket whose WHERE holds the six conditions POSITIVELY -- the
-- hash of (raw ticket, {kind, page_number, page_size} rebuilt from the typed arguments),
-- this session, this kind, not yet consumed, not expired by the WALL clock, created by a
-- COMMITTED transaction --; the refusal at once (28000); the rows only after it. ADR 0021
-- limit 4 holds unchanged (a rolled-back read un-consumes its ticket for the rest of its
-- 30 seconds; the re-read writes no new row and the committed one names the same read).
--
-- WHAT IS READ: operator_audit_log, every operator, every kind (or the one the ticket
-- binds), newest first. The viewer's OWN 'read' row was committed by phase one before
-- this runs, so on an unfiltered (or 'read'-filtered) first page it comes first -- unless
-- a row dated later exists: one committed between the two phases, or one the owner wrote
-- with a time of its choosing (the header's `at` note). Both are counted in the OP-14
-- note; neither is hidden by the order, they are above it.
--
-- THE ORDER: at DESC, id DESC. `at` is the wall clock at INSERT (its DEFAULT; no definer
-- may write it); id breaks a tie -- random, so the tie's order means nothing, but it is
-- total and therefore stable from page to page.
-- THE PAGE: LIMIT least(size, 200) and OFFSET (least(number, 1000) - 1) x that, in the
-- body (§2 iii; op_begin_read already refuses a larger size or page -- the body does not
-- trust that).
--
-- THE COLUMNS (a fixed list, ADR 0021 §2 ii):
--   audit_id, at, kind               the row, its time, its kind VERBATIM (the kind CHECK
--                                    makes it a member of a closed set; the Go side's
--                                    reading of it is closed as well -- OperatorAuditKind);
--   session_id                       the operator session the act was done under (NULL
--                                    for a pre-session row). The session's id, not its
--                                    hash: the hash is a credential, the id is not, and
--                                    no op_* takes an id;
--   actor_admin_id, actor_name       the operator, by id and display name;
--   target_admin_id, target_admin_name
--                                    the account a pre-session row is ABOUT;
--   target_tenant_id, target_tenant_name
--                                    the tenant a read touched, and its name -- ADR 0020
--                                    §9: a tenant is named by its name. ⚠️ A TENANT-
--                                    CROSSING READ OF NAMES: one tenants row per distinct
--                                    tenant the page's rows name, by the value each row
--                                    carries -- there is no tenant parameter whose filter
--                                    could be forgotten, and the page bounds the number;
--   target_scope                     the read kind of a 'read' row -- returned only when
--                                    it is one of the five read kinds the list below
--                                    names, NULL otherwise;
--   page_number, page_size           a read's content-free page;
--   search_class, filter_kind, legal_slug, legal_bytes
--                                    what the CLOSED LIST below reads out of detail;
--   detail_recognised                the row's (kind, target_scope, detail) is on that list.
--
-- 🔴 detail ITSELF IS NOT RETURNED, AND THE LIST IS CLOSED (FAIL-CLOSED). The table is
-- append-only, so every shape any build ever wrote stays in it -- the development database
-- holds five that no shipped code writes any more (the OP-14 card measured them: a
-- 'tenants' read with {}, {"search": true} and {"kind": "text"}, a 'legal_versions' and a
-- 'tenant_detail' read with {"search": "id"}) -- and a later writer can put a value
-- into detail by mistake. So nothing is echoed; a value is read out only where the whole
-- triple matches one of these shapes, and only a value of a closed set or a bounded
-- integer:
--   ('read', 'tenants', {"search": none|text|address|id})                 -> search_class
--   ('read', 'operator_audit', {"filter": all|<an audit kind>})           -> filter_kind
--   ('read', legal_versions|tenant_detail|tenant_plaques, {})
--   ('legal_publish', NULL, {"slug": <one of the four>, "document_id": <a uuid>,
--                            "bytes": 1..262144})                         -> legal_slug, legal_bytes
--   (any other kind, NULL, {})
-- Anything else -- an unknown key, a missing one, another type, a value outside its set,
-- an unknown or a misplaced scope -- is detail_recognised = false with every value column
-- NULL. The row itself is NOT dropped (kind, time, ids and names still say an act
-- happened); its detail is "not shown". An audit kind no migration names yet is under the
-- same list: its bare row (no scope, detail {}) matches the last line and is recognised --
-- there is nothing to read out of it -- while a FILLED detail of it reads as unrecognised
-- until this list names that shape (ADR 0021's OP-14 note, limit L3). The read kinds' list
-- and the filter's list are held equal to the two kind CHECKs by tests, so a migration that
-- widens a CHECK and not this function turns them red
-- (TestOpReadAudit_TheDetailIsShownOnlyInAShapeOnTheList,
-- TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree).
-- COST, AN OBSERVATION: the page walks operator_audit_log_at_idx backwards; a filter on a
-- rare kind walks it until it has a page, and a deep page skips (page-1) x size rows first --
-- both grow with the table. No index is added (an observation, counted in the OP-14 note).
-- +goose StatementBegin
CREATE FUNCTION public.op_read_audit(p_session text, p_ticket text, p_kind text,
                                     p_page_number integer, p_page_size integer)
    RETURNS TABLE (audit_id uuid, at timestamptz, kind text, session_id uuid,
                   actor_admin_id uuid, actor_name text,
                   target_admin_id uuid, target_admin_name text,
                   target_tenant_id uuid, target_tenant_name text,
                   target_scope text, page_number integer, page_size integer,
                   search_class text, filter_kind text, legal_slug text, legal_bytes integer,
                   detail_recognised boolean)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
    v_limit   integer;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    UPDATE public.operator_read_tickets AS k
       SET consumed_at = clock_timestamp()
     WHERE k.ticket_hash = encode(sha256(convert_to(
               p_ticket || jsonb_build_object('kind', p_kind,
                                              'page_number', p_page_number,
                                              'page_size', p_page_size)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'operator_audit'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_audit: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    v_limit := least(p_page_size, 200);

    RETURN QUERY
        WITH page AS (
            SELECT l.id, l.at, l.kind, l.session_id, l.actor_admin_id, l.target_admin_id,
                   l.target_tenant_id, l.target_scope, l.page_number, l.page_size, l.detail
              FROM public.operator_audit_log AS l
             WHERE p_kind = '' OR l.kind = p_kind
             ORDER BY l.at DESC, l.id DESC
             LIMIT v_limit
            OFFSET (least(p_page_number, 1000) - 1)::bigint * v_limit
        ), shaped AS (
            SELECT p.*,
                   coalesce(CASE
                       WHEN jsonb_typeof(p.detail) IS DISTINCT FROM 'object' THEN false
                       WHEN p.kind = 'read' AND p.target_scope = 'tenants' THEN
                            p.detail ? 'search' AND (p.detail - 'search') = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'search') = 'string'
                            AND (p.detail ->> 'search') IN ('none', 'text', 'address', 'id')
                       WHEN p.kind = 'read' AND p.target_scope = 'operator_audit' THEN
                            p.detail ? 'filter' AND (p.detail - 'filter') = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'filter') = 'string'
                            AND (p.detail ->> 'filter') IN ('all', 'login_failed', 'unknown_email',
                                                            'totp_failed', 'locked', 'enrollment_failed',
                                                            'password_ok', 'login', 'enrollment',
                                                            'logout', 'read', 'legal_publish')
                       WHEN p.kind = 'read' THEN
                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques')
                            AND p.detail = '{}'::jsonb
                       WHEN p.kind = 'legal_publish' THEN
                            p.target_scope IS NULL
                            AND p.detail ?& ARRAY['bytes', 'document_id', 'slug']
                            AND (p.detail - ARRAY['bytes', 'document_id', 'slug']) = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'slug') = 'string'
                            AND (p.detail ->> 'slug') IN ('privacy', 'terms', 'imprint', 'cookies')
                            AND jsonb_typeof(p.detail -> 'document_id') = 'string'
                            AND (p.detail ->> 'document_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                            AND jsonb_typeof(p.detail -> 'bytes') = 'number'
                            AND CASE WHEN (p.detail ->> 'bytes') ~ '^[1-9][0-9]{0,5}$'
                                     THEN (p.detail ->> 'bytes')::integer <= 262144
                                     ELSE false
                                END
                       ELSE p.target_scope IS NULL AND p.detail = '{}'::jsonb
                   END, false) AS recognised
              FROM page AS p
        )
        SELECT s.id, s.at, s.kind, s.session_id,
               s.actor_admin_id, actor_row.display_name,
               s.target_admin_id, target_row.display_name,
               s.target_tenant_id, tenant_row.name,
               CASE WHEN s.target_scope IN ('legal_versions', 'tenants', 'tenant_detail',
                                            'tenant_plaques', 'operator_audit')
                    THEN s.target_scope END,
               s.page_number, s.page_size,
               CASE WHEN s.recognised AND s.kind = 'read' AND s.target_scope = 'tenants'
                    THEN s.detail ->> 'search' END,
               CASE WHEN s.recognised AND s.kind = 'read' AND s.target_scope = 'operator_audit'
                    THEN s.detail ->> 'filter' END,
               CASE WHEN s.recognised AND s.kind = 'legal_publish'
                    THEN s.detail ->> 'slug' END,
               CASE WHEN s.recognised AND s.kind = 'legal_publish'
                    THEN (s.detail ->> 'bytes')::integer END,
               s.recognised
          FROM shaped AS s
          LEFT JOIN public.platform_admins AS actor_row ON actor_row.id = s.actor_admin_id
          LEFT JOIN public.platform_admins AS target_row ON target_row.id = s.target_admin_id
          LEFT JOIN public.tenants AS tenant_row ON tenant_row.id = s.target_tenant_id
         ORDER BY s.at DESC, s.id DESC;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_audit(text, text, text, integer, integer) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_audit(text, text, text, integer, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_audit(text, text, text, integer, integer) TO tappa_operator;

-- +goose Down

-- 🔴 WHAT A SUCCESSFUL Down DOES, named (00026's, 00027's, 00029's and 00030's house style):
--   * the read goes; op_begin_read goes back to 00030's Up body VERBATIM (four read kinds)
--     and op_record_auth_event to 00026's VERBATIM (five failure kinds, no handler) --
--     replaced, not dropped, so their owners and ACLs stay
--     (TestOperator00031_DownGivesBack00030AndUpTakesItAgain compares each body with the
--     file that defined it);
--   * tappa_opdefiner loses EXACTLY the column privileges section 3 granted -- by column,
--     NOT `REVOKE ALL` (which would take 00027's id with it and break op_begin_read);
--   * the 'read' rows the read committed and any 'password_ok' row STAY:
--     operator_audit_log is append-only (00026: its triggers bind the owner too) and they
--     are the evidence;
--   * operator_read_tickets_kind_check goes back to 00030's closed set -- NOT VALID when
--     ANY ticket exists whose kind that set does not name (this file's kind or a later
--     migration's, consumed or not), VALIDATED when none does (00030's Down rule, 2nd
--     round: the condition is "outside the previous set", never "this file's kind", or the
--     Downs do not compose);
--   * operator_audit_log_kind_check and operator_audit_log_actor_shape go back to 00027's
--     ten kinds and 00026's two arms -- BOTH NOT VALID when any row exists whose kind
--     00027's set does not name (a 'password_ok' row, or a later migration's kind), both
--     VALIDATED when none does. A 'password_ok' row violates both old constraints (its
--     kind is not in the set, and with no session it fails the session arm), so the two
--     move together, as in section 1.
--   ⚠️ COUNTED, NOT CLOSED (measured in the Down test): with a 'password_ok' row present,
--   00027's applied, immutable Up re-adds its ten-kind CHECK VALIDATED and fails with
--   23514; and 00027's own Down takes its NOT VALID branch only when a 'read' or
--   'legal_publish' row exists. A database with 'password_ok' rows (OP-14 phase C writes
--   them) cannot have 00027 re-applied over it without those rows gone first; nothing in
--   this file deletes a row.
DROP FUNCTION IF EXISTS public.op_read_audit(text, text, text, integer, integer);

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_begin_read(p_session text, p_kind text, p_params jsonb)
    RETURNS text
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session     uuid;
    v_admin       uuid;
    v_keys        text[];
    v_want        text[];
    v_page_number integer;
    v_page_size   integer;
    v_query       text;
    v_tenant      uuid;
    v_detail      jsonb := '{}'::jsonb;
    v_bound       jsonb;
    v_ticket      text;
    v_rows        bigint;
    v_constraint  text;
    v_state       text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    -- Nested rather than one OR chain: SQL does not promise to evaluate OR's arms in
    -- order, jsonb_object_keys on a non-object raises instead of answering false, and a
    -- NULL arm would turn the whole condition NULL -- which IF reads as false.
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques') THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_keys := (SELECT array_agg(k.key ORDER BY k.key) FROM jsonb_object_keys(p_params) AS k(key));

    IF p_kind IN ('tenant_detail', 'tenant_plaques') THEN
        IF v_keys IS DISTINCT FROM ARRAY['tenant_id']
           OR jsonb_typeof(p_params -> 'tenant_id') IS DISTINCT FROM 'string' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        IF ((p_params ->> 'tenant_id') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') IS NOT TRUE THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_tenant := (p_params ->> 'tenant_id')::uuid;
        v_bound := jsonb_build_object('tenant_id', v_tenant);
    ELSE
        -- (Two IFs, not a CASE inside the condition: PL/pgSQL ends an IF condition at the
        -- first THEN it reads, a CASE's included.)
        IF p_kind = 'tenants' THEN
            v_want := ARRAY['page_number', 'page_size', 'query'];
        ELSE
            v_want := ARRAY['page_number', 'page_size'];
        END IF;
        IF v_keys IS DISTINCT FROM v_want
           OR jsonb_typeof(p_params -> 'page_number') IS DISTINCT FROM 'number'
           OR jsonb_typeof(p_params -> 'page_size') IS DISTINCT FROM 'number' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        -- Plain positive integers only: page_number 1..999999999, page_size 1..200 (the
        -- LIMIT ceiling of ADR 0021 §2 iii, and operator_audit_log's own CHECK).
        IF (p_params ->> 'page_number') !~ '^[1-9][0-9]{0,8}$'
           OR (p_params ->> 'page_size') !~ '^[1-9][0-9]{0,2}$' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_page_number := (p_params ->> 'page_number')::integer;
        v_page_size   := (p_params ->> 'page_size')::integer;
        IF v_page_size > 200 THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        IF p_kind = 'tenants' THEN
            IF jsonb_typeof(p_params -> 'query') IS DISTINCT FROM 'string' THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_query := p_params ->> 'query';
            IF char_length(v_query) > 254 THEN
                RAISE EXCEPTION 'op_begin_read: read parameters refused'
                    USING ERRCODE = 'invalid_parameter_value';
            END IF;
            v_bound := jsonb_build_object('page_number', v_page_number, 'page_size', v_page_size,
                                          'query', v_query);
            -- The search CLASS (section 3's rules, in their order).
            IF v_query = '' THEN
                v_detail := jsonb_build_object('search', 'none');
            ELSIF v_query ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
                v_detail := jsonb_build_object('search', 'id');
            ELSIF strpos(v_query, '@') > 0 THEN
                v_detail := jsonb_build_object('search', 'address');
            ELSE
                v_detail := jsonb_build_object('search', 'text');
            END IF;
        ELSE
            v_bound := jsonb_build_object('page_number', v_page_number, 'page_size', v_page_size);
        END IF;
    END IF;

    v_ticket := encode(sha256(uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())
                              || uuid_send(pg_catalog.gen_random_uuid())), 'hex');

    BEGIN
        WITH audit AS (
            INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id,
                                                   target_scope, page_number, page_size, detail)
            VALUES ('read', v_session, v_admin, v_tenant, p_kind, v_page_number, v_page_size, v_detail)
            RETURNING id
        )
        INSERT INTO public.operator_read_tickets (ticket_hash, session_id, kind, target_tenant_id,
                                                  audit_id, expires_at)
        SELECT encode(sha256(convert_to(v_ticket || v_bound::text, 'UTF8')), 'hex'),
               v_session, p_kind, v_tenant, audit.id, clock_timestamp() + interval '30 seconds'
          FROM audit;

        GET DIAGNOSTICS v_rows = ROW_COUNT;
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_begin_read: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            v_rows := 0;
    END;

    IF v_rows <> 1 THEN
        RAISE EXCEPTION 'op_begin_read: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    RETURN v_ticket;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_record_auth_event(p_kind text,
                                                       p_email text DEFAULT NULL,
                                                       p_admin uuid DEFAULT NULL)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF p_kind IS NULL OR p_kind NOT IN ('login_failed', 'unknown_email', 'totp_failed',
                                        'locked', 'enrollment_failed') THEN
        RAISE EXCEPTION 'op_record_auth_event: kind is not a pre-session failure kind'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    WITH target AS (
        SELECT a.id
          FROM public.platform_admins AS a
         WHERE (p_email IS NOT NULL AND a.email OPERATOR(public.=) p_email::public.citext)
            OR (p_email IS NULL AND a.id = p_admin)
    ), counted AS (
        UPDATE public.platform_admins AS a
           SET totp_failures     = a.totp_failures + 1,
               totp_locked_until = CASE WHEN a.totp_failures + 1 >= 5
                                        THEN clock_timestamp() + interval '15 minutes'
                                        ELSE a.totp_locked_until
                                   END
          FROM target AS t
         WHERE p_kind = 'totp_failed'
           AND a.id = t.id
           AND a.status = 'active'
        RETURNING a.id
    )
    INSERT INTO public.operator_audit_log (kind, target_admin_id)
    SELECT p_kind, (SELECT t.id FROM target AS t);
END;
$$;
-- +goose StatementEnd

REVOKE SELECT (at, kind, session_id, actor_admin_id, target_admin_id, target_tenant_id,
               target_scope, page_number, page_size, detail)
    ON operator_audit_log FROM tappa_opdefiner;

ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_read_tickets
                WHERE kind <> ALL (ARRAY['legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques'])) THEN
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques'));
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_actor_shape;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_audit_log
                WHERE kind <> ALL (ARRAY['login_failed', 'unknown_email', 'totp_failed', 'locked',
                                         'enrollment_failed', 'login', 'enrollment', 'logout',
                                         'read', 'legal_publish'])) THEN
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'login', 'enrollment', 'logout',
                            'read', 'legal_publish'))
            NOT VALID;
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_actor_shape CHECK (
            (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                      'enrollment_failed')
             AND session_id IS NULL AND actor_admin_id IS NULL)
            OR
            (kind NOT IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                          'enrollment_failed')
             AND session_id IS NOT NULL AND actor_admin_id IS NOT NULL)
        ) NOT VALID;
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'login', 'enrollment', 'logout',
                            'read', 'legal_publish'));
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_actor_shape CHECK (
            (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                      'enrollment_failed')
             AND session_id IS NULL AND actor_admin_id IS NULL)
            OR
            (kind NOT IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                          'enrollment_failed')
             AND session_id IS NOT NULL AND actor_admin_id IS NOT NULL)
        );
    END IF;
END
$$;
-- +goose StatementEnd
