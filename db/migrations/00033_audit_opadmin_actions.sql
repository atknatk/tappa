-- 00033 -- M10 OP-14 D (K14-2): the platform owner's three opadmin actions -- create,
-- reset-mfa, disable -- each leave ONE row in operator_audit_log, and a row's `at` is the
-- wall clock whoever writes it.
--
-- NORMATIVE SOURCES: docs/adr/0020-platform-operatoru-ayri-kimlik.md §5 (the audit; its
-- sentence "every audit row comes from a definer" gains ONE named exception here) and §6
-- (cmd/opadmin), docs/adr/0021-op-fonksiyonlari-tenant-otesi-erisim.md (the op_* contract;
-- limit 5 counts what a superuser can do to this table). Where this file decides something
-- those leave open, the decision is written next to the statement and in ADR 0021's "OP-14 D
-- uygulama notu".
--
-- WHY: ADR 0020's risk 1 -- one operator carries the platform's whole power, and the only
-- control is the audit. Until this file the three acts that make, re-open and close an
-- operator ACCOUNT left no audit row (deploy/README.md, limit O9-1): opadmin's SQL wrote
-- platform_admins and platform_sessions and nothing else, so the viewer (OP-14 B) showed every
-- sign-in of an account and never the act that made it.
--
-- WHAT IT DOES, IN ORDER:
--   0. re-checks the two cluster roles' shape (00026's precondition, as 00027-00032 repeated
--      it: two SECURITY DEFINER functions are replaced below) and that the database is at
--      00032 (its traces);
--   1. the audit kinds: operator_audit_log_kind_check gains the three OWNER kinds, and
--      operator_audit_log_actor_shape gains a THIRD ARM for them -- no session, no actor, the
--      account the act was about, and nothing else -- both re-added under their names, NOT
--      VALID only when a row of a kind outside the new set already exists;
--   2. a BEFORE INSERT trigger stamps every new row's `at` with the wall clock, whoever
--      writes the row and whatever it carries;
--   3. op_begin_read is REPLACED: its log-read filter list names the three kinds (00032's
--      body otherwise, byte for byte);
--   4. op_read_audit is REPLACED: its filter shape names the three kinds (00032's body
--      otherwise, byte for byte).
--
-- No table is created, so there is no redline waiver, no new RLS and no sequence. No grant
-- changes: tappa_app gains nothing, tappa_operator nothing (it still cannot write
-- operator_audit_log, and op_record_auth_event's closed set does not name an owner kind),
-- tappa_opdefiner nothing (its INSERT list on the log is 00026's, still without `at`).
--
-- 🔴 THE OWNER WRITES THESE ROWS, NOT A DEFINER -- ADR 0020 §5's exception, by name. Every
-- other row comes from an op_* that tappa_opdefiner owns and tappa_operator calls. These
-- three come from the SQL cmd/opadmin generates and tappa_owner applies with psql: the same
-- DO block that writes the account writes the row (one statement -- the account's change and
-- its trace commit together or not at all). A definer was NOT used: an op_* that writes an
-- owner kind would be a function tappa_operator can call, i.e. a door through which the
-- operator's DSN holder could print "the platform owner disabled this account" for any
-- account. The owner already holds every privilege on the table (it is its owner and, on the
-- deployed topology, a superuser -- ADR 0021 limit 5); the rows add a trace, not a power.
--
-- THE THIRD ARM IS THE ROW'S WHOLE SHAPE: kind one of the three, session NULL, actor NULL,
-- target_admin_id NOT NULL (the account), target_tenant_id, target_scope, page_number and
-- page_size NULL, detail exactly '{}'. So an owner row names an account by its id and says
-- nothing else -- not the address, not the display name (ADR 0020 §5: an operator is named by
-- id) -- and a later edit of opadmin that put either into detail is refused by the schema
-- (23514), not merely by a test. op_read_audit's last shape line (scope NULL, detail {})
-- already recognises such a row; its filter shape is what needs the three names (section 4).
--
-- `at` (the OP-14 card's C3; ADR 0021's OP-14 note md. 7 counted it open): the owner holds
-- INSERT on every column, `at` included (measured before writing this file, 2026-10-07:
-- has_column_privilege(tappa_owner, operator_audit_log, at, INSERT) = true). The generated
-- INSERT does not name `at` (cmd/opadmin's TestSQL_EachActionWritesOneAuditRowInItsDoBlock
-- pins its column list), and section 2 makes that true of the TABLE rather than of one
-- statement: a row's time is the wall clock at its INSERT, whoever wrote it -- the owner
-- included, a COPY included -- unless the table's owner disables the trigger, drops it or
-- replaces its function (section 2 names the paths; ADR 0021 limit 5). A definer's row is unchanged:
-- its `at` was the column's DEFAULT clock_timestamp() and is now the same clock read a
-- moment later, in the same INSERT.
--
-- CLOCKS (ADR 0021 §2 vii): the trigger reads clock_timestamp() only; the two replaced
-- functions' clocks are 00032's. TestOperator00026_NoFrozenClock walks the functions
-- tappa_opdefiner owns; the trigger function belongs to the migration role and its body is
-- pinned by TestOperator00033_TheKindsTheShapeAndTheClock.
--
-- SECRETS (ADR 0021 §3.5, CLAUDE.md §7): the RAISE EXCEPTIONs carry constant messages; the
-- formatted ones are the precondition's (role NAMES). The new rows carry no value at all.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION: the roles, and 00032.
-- ---------------------------------------------------------------------------
-- The role checks are 00026's, in 00026's order, as 00027-00032 repeated them: an op_* owned
-- by a SUPERUSER, a LOGIN or a member-carrying tappa_opdefiner is a general bypass with every
-- other check green, and a member of tappa_operator inherits EXECUTE on the two functions
-- replaced below.
-- 00032: the two functions this file replaces belong to tappa_opdefiner, 00032's own read
-- exists, and the audit CHECKs are 00031's -- the kind CHECK names 'password_ok' and not yet
-- this file's kinds, actor_shape is there. A database that fails this was not migrated by
-- goose in order, and replacing functions on it would be a guess.
-- +goose StatementBegin
DO $$
DECLARE
    v_missing text;
    v_kinds   text;
    v_shape   text;
BEGIN
    SELECT string_agg(r.name, ', ' ORDER BY r.name)
      INTO v_missing
      FROM unnest(ARRAY['tappa_operator', 'tappa_opdefiner']) AS r(name)
     WHERE NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles p WHERE p.rolname = r.name);
    IF v_missing IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00033 (M10 OP-14 D) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00033 (M10 OP-14 D): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00033 (M10 OP-14 D): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00033 (M10 OP-14 D): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00033 (M10 OP-14 D): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00033 (M10 OP-14 D): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00033 (M10 OP-14 D): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1: tappa_app gets no new privilege). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
         WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_audit(text, text, text, integer, integer)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_begin_read(text, text, jsonb)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR pg_catalog.to_regprocedure('public.op_read_tenant_billing(text, text, uuid, integer)') IS NULL THEN
        RAISE EXCEPTION 'migration 00033 (M10 OP-14 D) replaces op_begin_read and op_read_audit as migration 00032 left them, and 00032''s functions are not there (or do not belong to tappa_opdefiner). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    SELECT pg_catalog.pg_get_constraintdef(c.oid)
      INTO v_kinds
      FROM pg_catalog.pg_constraint c
     WHERE c.conrelid = 'public.operator_audit_log'::regclass
       AND c.conname = 'operator_audit_log_kind_check';
    SELECT pg_catalog.pg_get_constraintdef(c.oid)
      INTO v_shape
      FROM pg_catalog.pg_constraint c
     WHERE c.conrelid = 'public.operator_audit_log'::regclass
       AND c.conname = 'operator_audit_log_actor_shape';
    -- (position(... IN ...), the SQL syntax, not a schema-qualified call: redline R7 reads
    -- "catalog." followed by a call naming 'password' as a logging call.)
    IF v_kinds IS NULL OR v_shape IS NULL
       OR position('''password_ok''' IN v_kinds) = 0
       OR position('''operator_created''' IN v_kinds) > 0 THEN
        RAISE EXCEPTION 'migration 00033 (M10 OP-14 D) widens operator_audit_log_kind_check and operator_audit_log_actor_shape as migration 00031 left them, and the constraints are not in that state. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The audit kinds: three OWNER kinds, and actor_shape's third arm.
-- ---------------------------------------------------------------------------
--   'operator_created'    cmd/opadmin create: a pending account was written, with a link.
--   'operator_mfa_reset'  cmd/opadmin reset-mfa: the account went back to pending -- its TOTP
--                         key, password digest and lock counter removed, its live sessions
--                         revoked -- with a new link.
--   'operator_disabled'   cmd/opadmin disable: the account is disabled and its live sessions
--                         revoked (each application writes its row, the second one too: the
--                         act was applied, whatever it found).
-- The names follow the log's own pattern for a fact about an account (login_failed,
-- enrollment_failed): the thing, then what happened to it.
-- 🔴 BOTH CONSTRAINTS MOVE TOGETHER (the OP-14 card's T1, again): the kind set alone would put
-- the three in actor_shape's SESSION arm, and every owner row would need a session and an
-- actor it cannot have. Both keep their NAMES (00027's and 00031's practice), so the next
-- migration finds them where this one did.
-- NOT VALID, AND WHEN (00031's rule): a row whose kind the new set does not name can only be one
-- a LATER migration wrote and whose Down left it behind under a NOT VALID CHECK. With such a row
-- present a VALIDATED re-add fails with 23514, so both CHECKs are then added NOT VALID --
-- binding every new row, not re-checking the old one -- and VALIDATED otherwise. The question is
-- the KIND alone, as in 00031: a later migration that changed the third arm's shape and left an
-- owner row of the other shape behind would make this VALIDATED re-add fail (ADR 0021's OP-14 D
-- note counts it; no such migration exists).
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_actor_shape;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_audit_log
                WHERE kind <> ALL (ARRAY['login_failed', 'unknown_email', 'totp_failed', 'locked',
                                         'enrollment_failed', 'password_ok', 'login', 'enrollment',
                                         'logout', 'read', 'legal_publish', 'operator_created',
                                         'operator_mfa_reset', 'operator_disabled'])) THEN
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled'))
            NOT VALID;
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_actor_shape CHECK (
            (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                      'enrollment_failed', 'password_ok')
             AND session_id IS NULL AND actor_admin_id IS NULL)
            OR
            (kind IN ('operator_created', 'operator_mfa_reset', 'operator_disabled')
             AND session_id IS NULL AND actor_admin_id IS NULL
             AND target_admin_id IS NOT NULL AND target_tenant_id IS NULL AND target_scope IS NULL
             AND page_number IS NULL AND page_size IS NULL AND detail = '{}'::jsonb)
            OR
            (kind NOT IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                          'enrollment_failed', 'password_ok', 'operator_created',
                          'operator_mfa_reset', 'operator_disabled')
             AND session_id IS NOT NULL AND actor_admin_id IS NOT NULL)
        ) NOT VALID;
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled'));
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_actor_shape CHECK (
            (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                      'enrollment_failed', 'password_ok')
             AND session_id IS NULL AND actor_admin_id IS NULL)
            OR
            (kind IN ('operator_created', 'operator_mfa_reset', 'operator_disabled')
             AND session_id IS NULL AND actor_admin_id IS NULL
             AND target_admin_id IS NOT NULL AND target_tenant_id IS NULL AND target_scope IS NULL
             AND page_number IS NULL AND page_size IS NULL AND detail = '{}'::jsonb)
            OR
            (kind NOT IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                          'enrollment_failed', 'password_ok', 'operator_created',
                          'operator_mfa_reset', 'operator_disabled')
             AND session_id IS NOT NULL AND actor_admin_id IS NOT NULL)
        );
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 2. `at` is the wall clock, whoever writes the row
-- ---------------------------------------------------------------------------
-- A BEFORE INSERT ... FOR EACH ROW trigger overwrites NEW.at with clock_timestamp() -- always,
-- not only when it is NULL (it never is: the column is NOT NULL with a DEFAULT, so a trigger
-- that "fills a missing time" would change nothing and bind no one). What it binds:
--   * the OWNER, whose INSERT privilege covers `at` (the header): a row it writes -- opadmin's,
--     or any other -- is dated by the clock, not by the statement. Before this file a row
--     dated in the future headed every unfiltered first page of the viewer, above the viewer's
--     own 'read' row (ADR 0021's OP-14 note, limit L2, measured);
--   * every path that inserts: a plain INSERT, an INSERT ... SELECT, a COPY into the table.
-- What it does not bind, named: tappa_owner, which owns both the table and the function
-- (ownership is what these need, not superuser -- PostgreSQL's documentation), can disable the
-- trigger, drop it, or replace its function (CREATE OR REPLACE:
-- TestOperator00033_TheClockFunctionRunsOnlyAsItsTrigger does exactly that inside a
-- savepoint); and a superuser's session with session_replication_role = replica skips it
-- (PostgreSQL's documentation; not measured). ADR 0021 limit 5 -- the append-only triggers
-- share it. An UPDATE of `at` is the append-only trigger's business (it refuses every UPDATE;
-- this trigger does not fire on one).
-- The append-only triggers fire on UPDATE/DELETE (row) and TRUNCATE (statement); this one on
-- INSERT alone, so no two of them ever fire for the same event and their order is moot.
-- A definer's write is unchanged in meaning: its INSERT lists never named `at` (00026), the
-- DEFAULT gave it clock_timestamp(), and the trigger gives it clock_timestamp() a moment later.
-- The function is the migration role's, SECURITY INVOKER, with the search_path every trigger
-- function in this schema pins; it reads no table and takes no argument. PUBLIC keeps EXECUTE
-- on it, as on the eight trigger functions before it (measured 2026-10-07): a trigger function
-- cannot be called except as a trigger, and attaching it to a table (CREATE TRIGGER) needs the
-- TRIGGER privilege on that table -- the privilege, not ownership -- which tappa_app,
-- tappa_operator and the definer do not hold on any operator table
-- (TestOperator00026_PrivilegeMatrix). 00011's note measured both refusals for tappa_app; its
-- parenthesis names ownership, the condition is the privilege.
-- +goose StatementBegin
CREATE FUNCTION public.tappa_audit_at_is_the_wall_clock()
    RETURNS trigger
    LANGUAGE plpgsql
    VOLATILE
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    NEW.at := pg_catalog.clock_timestamp();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER operator_audit_log_at_is_the_wall_clock
    BEFORE INSERT ON operator_audit_log
    FOR EACH ROW EXECUTE FUNCTION public.tappa_audit_at_is_the_wall_clock();

-- ===========================================================================
-- 3. op_begin_read, REPLACED: the log read's filter list names the owner kinds
-- ===========================================================================
-- 00032's body but for ONE list: the kinds an 'operator_audit' read may be filtered to -- '' or
-- a member of operator_audit_log_kind_check, which section 1 widened. Everything 00027-00032 say
-- of this function still holds, branch for branch; the refusals keep 00027's messages and codes.
-- The four copies of the kind set (this list, the CHECK, op_read_audit's filter shape and
-- internal/db's OperatorAuditKinds) are held equal by
-- TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree.
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
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'operator_audit', 'tenant_billing') THEN
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
    ELSIF p_kind = 'tenant_billing' THEN
        -- One tenant and a page of twelve local months: EXACTLY {tenant_id, page_number},
        -- the page 1..5 (sixty months).
        IF v_keys IS DISTINCT FROM ARRAY['page_number', 'tenant_id']
           OR jsonb_typeof(p_params -> 'tenant_id') IS DISTINCT FROM 'string'
           OR jsonb_typeof(p_params -> 'page_number') IS DISTINCT FROM 'number' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        IF ((p_params ->> 'tenant_id') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') IS NOT TRUE
           OR (p_params ->> 'page_number') !~ '^[1-9][0-9]{0,8}$' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_tenant      := (p_params ->> 'tenant_id')::uuid;
        v_page_number := (p_params ->> 'page_number')::integer;
        IF v_page_number > 5 THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_page_size := 12;
        v_bound := jsonb_build_object('tenant_id', v_tenant, 'page_number', v_page_number);
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
                                                   'logout', 'read', 'legal_publish', 'operator_created',
                                                   'operator_mfa_reset', 'operator_disabled') THEN
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
-- CREATE OR REPLACE keeps the owner and the ACL; they are written again so this file, read
-- alone, shows the whole contract of the function it (re)defines (ADR 0021 §2 vi).
ALTER FUNCTION public.op_begin_read(text, text, jsonb) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_begin_read(text, text, jsonb) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_begin_read(text, text, jsonb) TO tappa_operator;

-- ===========================================================================
-- 4. op_read_audit, REPLACED: the filter shape names the owner kinds
-- ===========================================================================
-- 00032's body but for ONE list: the values an 'operator_audit' read's {"filter": ...} may
-- hold. Without it, a read filtered to an owner kind would come back "detail not shown" --
-- fail-closed, but a lie about a shipped shape. The owner rows themselves need no new line:
-- their scope is NULL and their detail {} (section 1's third arm makes both certain), which
-- the list's last line already recognises.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_read_audit(p_session text, p_ticket text, p_kind text,
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
                                                            'logout', 'read', 'legal_publish',
                                                            'operator_created', 'operator_mfa_reset',
                                                            'operator_disabled')
                       WHEN p.kind = 'read' THEN
                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing')
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
                                            'tenant_plaques', 'operator_audit', 'tenant_billing')
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

-- 🔴 WHAT A SUCCESSFUL Down DOES, named (00030-00032's house style):
--   * op_read_audit and op_begin_read go back to 00032's Up bodies VERBATIM -- replaced, not
--     dropped, so their owners and ACLs stay (TestOperator00033_DownGivesBack00032AndUpTakesItAgain
--     compares each body with 00032's file);
--   * the `at` trigger and its function go: the owner may write `at` again, as at 00032;
--   * operator_audit_log_kind_check and operator_audit_log_actor_shape go back to 00031's
--     eleven kinds and two arms -- BOTH NOT VALID when any row exists whose kind 00031's set
--     does not name (an owner row, or a later migration's kind), both VALIDATED when none does.
--     An owner row violates both old constraints (its kind is not in the set, and with no
--     session it fails the session arm), so the two move together, as in section 1;
--   * the owner rows STAY: operator_audit_log is append-only (00026: its triggers bind the owner
--     too) and they are the evidence. Nothing here deletes a row, and no grant changes (none
--     did in the Up).
--   ⚠️ COUNTED, NOT CLOSED: a cmd/opadmin script of THIS change applied to a database taken
--   back to 00032 is refused -- by operator_audit_log_actor_shape, named, nothing changed (the
--   CHECKs run in name order; the owner kind falls to 00031's session arm) -- and, once an owner
--   row exists, 00027's applied, immutable Up cannot be re-run over it (ADR 0021's OP-14 note,
--   limit L9's class: the same is measured there for a 'password_ok' row).
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION public.op_read_audit(p_session text, p_ticket text, p_kind text,
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
                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing')
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
                                            'tenant_plaques', 'operator_audit', 'tenant_billing')
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
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'operator_audit', 'tenant_billing') THEN
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
    ELSIF p_kind = 'tenant_billing' THEN
        -- One tenant and a page of twelve local months: EXACTLY {tenant_id, page_number},
        -- the page 1..5 (sixty months).
        IF v_keys IS DISTINCT FROM ARRAY['page_number', 'tenant_id']
           OR jsonb_typeof(p_params -> 'tenant_id') IS DISTINCT FROM 'string'
           OR jsonb_typeof(p_params -> 'page_number') IS DISTINCT FROM 'number' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        IF ((p_params ->> 'tenant_id') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') IS NOT TRUE
           OR (p_params ->> 'page_number') !~ '^[1-9][0-9]{0,8}$' THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_tenant      := (p_params ->> 'tenant_id')::uuid;
        v_page_number := (p_params ->> 'page_number')::integer;
        IF v_page_number > 5 THEN
            RAISE EXCEPTION 'op_begin_read: read parameters refused'
                USING ERRCODE = 'invalid_parameter_value';
        END IF;
        v_page_size := 12;
        v_bound := jsonb_build_object('tenant_id', v_tenant, 'page_number', v_page_number);
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

DROP TRIGGER IF EXISTS operator_audit_log_at_is_the_wall_clock ON operator_audit_log;
DROP FUNCTION IF EXISTS public.tappa_audit_at_is_the_wall_clock();

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
