-- 00063 -- M10 OP-15, phase A (the data layer): the operator SUSPENDS a tenant and lifts the
-- suspension -- an op_* pair that CHANGES a tenant (after 00034's VAT verdict and 00039's API
-- gate), and the first whose change the customer's panel will read (phase B's gate; nothing in
-- the product reads it before B).
--
-- NORMATIVE SOURCE: docs/adr/0025-tenant-askiya-alma.md (accepted 2026-10-08) -- "Karar" §1 (two
-- columns on tenants, the closed reason set, no index, tappa_app reads and cannot write), §2 (the
-- two definers: their order, B14, the belt, idempotence, the Down's refusal), §6 (OP-K5 = (a): no
-- history table, billing arithmetic unchanged), "Sayılı sınırlar"; and ADR 0021 §1-§3 and §6 (the
-- op_* contract and its pins) with its "OP-16 uygulama notu" (the "first tenant-changing op_*"
-- load born in 00034: the definer's INSERT on audit_log, `at` included; the tenant-write CHECK;
-- B14 through the locked read's FOUND; FOR NO KEY UPDATE) and its "OP-15 uygulama notu". Where
-- this file decides something those leave open, the decision is written next to the statement.
--
-- WRITTEN ONTO 00062 (the port of 2026-10-20). This file was first written on 2026-10-08 as 00035,
-- onto 00034, and held while M11 shipped (00035-00062; the user's decision of 2026-10-17, 00057's
-- header and deploy/README.md, M11 deploy day (a)). Its decisions are unchanged; what moved is
-- what it replaces and what it compares against: op_begin_read and op_read_audit are now 00042's
-- bodies (00039's API kinds and 00042's holding kinds in their lists), op_read_tenant_detail is
-- 00057's (the API state at the end of its row), the audit kind set is 00042's twenty and the
-- tenant-write kinds are 00039's three; tappa_opdefiner reads and writes the two API columns of
-- tenants (00039, 00042). This file adds its two kinds AFTER those, its two columns AFTER theirs.
--
-- WHY: an operator has no in-product way to stop a tenant's writes (non-payment, a breach of
-- terms, a security hold, the customer's own request); the one blunt tool is the owner disabling
-- the tenant's admins in psql, which also stops reading and the CSV export and leaves a row the
-- tenant's own action cannot be told from. A suspension stops the panel's WRITES (phase B) and
-- nothing else: taps, activation, reading and the export carry on (CLAUDE.md §4.6).
--
-- 🔴 THE SUSPENSION IS TWO COLUMNS ON tenants AND NOTHING ELSE (ADR 0025 §1, OP-K5 = (a)):
-- suspended_at and suspension_reason, both set or both NULL, the reason from a CLOSED set of four
-- (non_payment, terms_breach, security_hold, customer_request -- no free text: the tenant reads it
-- and its code enters the tenant's append-only audit_log). The CHECKs bind the owner too. The two
-- columns carry NO INDEX on purpose (ADR 0025 §1: an indexed column would decide on every page
-- whether the UPDATE can be HOT, and the HOT counter would tell the previous state). No history
-- table: the history is the two append-only logs. No trigger fires on them: tenants' two triggers
-- (00041's tenants_holding_timezone_lock, BEFORE UPDATE WHEN the timezone changes; 00048's
-- tenants_feed_counter, AFTER INSERT) do not fire on an UPDATE of these columns.
--
-- 🔴 tappa_app READS THE TWO COLUMNS AND CANNOT WRITE THEM. Its table-level SELECT (00001) reaches
-- the new columns by itself; its UPDATE (00024: name, business_type, timezone) and INSERT lists
-- (00016/00017) are COLUMN lists and do not grow. 00024's ACL is not written again (the
-- orchestrator's K15-2): the precondition below refuses a cluster where a table-level UPDATE or
-- INSERT on tenants reaches tappa_app -- directly, through PUBLIC or through any role it is a
-- member of -- because a column added later falls under such a grant by itself and no column
-- grant can narrow it (00024 measured that; 00038 refused the same for its two columns). A
-- customer that could write these columns could lift its own suspension.
--
-- 🔴 THE TWO WRITES -- op_suspend_tenant(p_session, p_tenant_id, p_reason) and
-- op_reinstate_tenant(p_session, p_tenant_id), RETURNS void -- in ADR 0025 §2's order:
--   1. the session through op_touch_session (28000 for a dead one); the actor is derived;
--   2. the arguments refused before anything is read: p_tenant_id NULL, p_reason NULL or outside
--      the closed set -- one constant 22023, no DETAIL;
--   3. THE LOCK FIRST: the tenant's row read by its primary key alone, FOR NO KEY UPDATE, and
--      nothing of it returned (the body holds no previous state in a variable). FOUND says only
--      that the tenant exists. NO KEY UPDATE is the lock the UPDATE takes anyway: a foreign-key
--      check (FOR KEY SHARE) -- a tap's INSERT among them -- does not wait (OP-16's LV5 class);
--   4. ONLY IF FOUND, in this order: ONE wall-clock read, AFTER the lock (a clock read before the
--      lock would date the write back by the wait, and two racing calls could write their times
--      in the reverse of the order they were applied -- ADR 0025 §2 step 4); the UPDATE by the
--      primary key (the suspension keeps its FIRST time: coalesce reads the row under the lock --
--      K15-4; the reason is replaced); the tenant's own audit_log row with `at` that same clock
--      read, written explicitly (K6; 00005's DEFAULT is the transaction's start). For a tenant
--      that does not exist none of these runs: no foreign key fires, no 23503 -- no existence
--      oracle in the answer (B14);
--   5. the operator's row on EVERY accepted call, outside the IF: 'tenant_suspended' /
--      'tenant_reinstated', the session, the operator, the tenant, detail {} (00034's
--      operator_audit_log_tenant_write_shape, widened below, makes {} the only shape);
--   6. a constraint that refuses steps 3-5 is caught (00027's pattern): one 22023, no DETAIL, a
--      LOG line with the constraint's name and SQLSTATE only; its sub-transaction takes 3-5 with it.
-- The answer -- void, 28000 or 22023 -- depends on the session and the arguments only: not on
-- whether the tenant exists, is suspended or not, or was suspended for another reason (ADR 0021
-- §2 v 7). There is no "already suspended" and no row count. One exception is counted (ADR 0025
-- limit 5, OP-16's LV3 class): while ANOTHER transaction holds an existing tenant's row, step 3
-- waits, and what the caller's own limits make of the wait (55P03 under a lock_timeout) is an
-- answer that the tenant exists.
-- 🔴 NO BINDING TO A COMMITTED READ (ADR 0025 §2, "Commit edilmiş okumaya bağ — GEREKMEZ"): 00034
-- bound its write to a committed read because the write TESTED a caller's value against a stored
-- one; these two test nothing -- the id finds a row by its key, the reason is written and never
-- compared, the previous state is never read. No holding lock either: unlike 00042's
-- op_enable_tenant_api these writes compare nothing with a peer, so the holding's advisory lock
-- (ADR 0027 §1) has nothing to serialise here.
--
-- §4.5 -- CROSSED ON PURPOSE, IN THE ONE PLACE ADR 0021 ALLOWS. The functions belong to
-- tappa_opdefiner (NOLOGIN, BYPASSRLS): no row level security applies inside them. What stands
-- instead is `p_tenant_id` in every statement that names a tenant -- the locking read's WHERE,
-- the UPDATE's WHERE, the tenant row's tenant_id and the operator row's target_tenant_id (the
-- belt, ADR 0021 §3.2; TestOpSuspendTenant_ACallForOneTenantTouchesNoOther).
--
-- WHAT IT DOES, IN ORDER:
--   0. lock_timeout 1 s, the FIRST statement (00054's rule, deploy/README.md): the ALTER below
--      takes ACCESS EXCLUSIVE on tenants, which every request reads; behind a long reader the
--      file gives up with 55P03 instead of queueing every later tenant read behind its lock;
--   1. re-checks the two cluster roles' shape (00026's precondition, as 00027-00057 repeated it),
--      that the database is at 00062 on the operator surface this file replaces (the two CHECKs
--      by their whole text, the four bodies by md5(prosrc), the two reads' result types), and
--      that the definer's reach on tenants and audit_log is exactly what 00034, 00039 and 00042
--      left and no table-level write on tenants reaches tappa_app, tappa_operator or one of M11's
--      three definers (the drift check);
--   2. tenants gains the two columns and their two CHECKs;
--   3. the grants: tappa_opdefiner gains SELECT and UPDATE on the two columns -- nothing else;
--   4. the audit kinds: operator_audit_log_kind_check gains 'tenant_suspended' and
--      'tenant_reinstated' (NOT VALID only when a row of a kind outside the new set exists), and
--      operator_audit_log_tenant_write_shape names them;
--   5. op_begin_read is REPLACED: the two kinds join its log-read filter list (00042's body
--      otherwise, byte for byte);
--   6. op_read_audit is REPLACED: the two kinds join its filter shape (00042's body otherwise,
--      byte for byte);
--   7. op_read_tenants and op_read_tenant_detail are DROPPED and CREATED again (CREATE OR REPLACE
--      cannot change a result type): the list gains suspended_at at the END (00029's body
--      otherwise), the overview gains suspended_at and suspension_reason at the END, after
--      00057's API state (00057's body otherwise);
--   8. op_suspend_tenant -- the suspension;
--   9. op_reinstate_tenant -- the lifting.
--
-- No table is created, so there is no redline waiver, no new RLS and no sequence. tappa_app gains
-- nothing (it READS the two columns through 00001's table-level SELECT; ADR 0021 §1);
-- tappa_operator gains EXECUTE on the two writes and keeps it on the two re-created reads, and no
-- privilege on any table. The two new functions are EXECUTE-to-their-owner-and-tappa_operator
-- (REVOKE ALL ... FROM PUBLIC; ADR 0028 §1's rule from 00037 on, and ADR 0021 §2 vi).
--
-- CLOCKS (ADR 0021 §2 vii): every instant either write stores -- suspended_at and the tenant
-- row's `at` -- is ONE clock_timestamp() read per call, taken after the row lock. The operator
-- row's `at` is 00033's trigger's. TestOperator00026_NoFrozenClock walks every function
-- tappa_opdefiner owns.
--
-- SECRETS (ADR 0021 §3.5, CLAUDE.md §7): every RAISE EXCEPTION carries a constant message; the
-- formatted ones are the precondition's (role names) and the writes' LOG lines (a constraint name
-- and a SQLSTATE). No argument value reaches a message.
--
-- ⚠️ COUNTED, NOT CLOSED (ADR 0025 "Sayılı sınırlar"): the holder of tappa_operator's DSN can
-- suspend and reinstate any tenant (each call leaves two append-only rows); a call rolled back
-- inside a savepoint tells, through the statistics views, whether the id names a tenant (B14's
-- bit, 122 random bits, not repeatable into a search) and -- on a nearly full page only -- the
-- width of the tenant's row through the HOT counter, the relation size and the WAL volume; and a
-- lock wait tells that the tenant exists. Measured and pinned in internal/db's
-- operatorsuspend_test.go and operatorsuspendcalls_test.go.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 1. PRECONDITION: the roles, 00062's operator surface, and the reach on tenants and audit_log.
-- ---------------------------------------------------------------------------
-- The role checks are 00026's, in 00026's order, as 00027-00057 repeated them: an op_* owned by a
-- SUPERUSER, a LOGIN or a member-carrying tappa_opdefiner is a general bypass with every other
-- check green, and a member of tappa_operator inherits EXECUTE on the functions below -- two of
-- which suspend any tenant.
-- 00062's operator surface (00039's and 00042's precondition shape, one step on): the four
-- functions this file replaces belong to tappa_opdefiner and hold their bodies by md5(prosrc) --
-- op_begin_read and op_read_audit 00042's, op_read_tenants 00029's, op_read_tenant_detail
-- 00057's -- the two reads return 00029's and 00057's columns, the audit kind CHECK is 00042's
-- twenty (its NOT VALID suffix aside: a later Down may leave it NOT VALID) and the tenant-write
-- CHECK 00039's three, by their whole text, and nothing this file creates exists yet. A migration
-- between 00062 and this one that changed any of them stops this file here instead of being
-- silently undone by it: a rebase onto it takes its bodies, its digests and its sets here. This
-- file's Down gives every one of them back verbatim, so the Up composes over it.
-- THE REACH -- the drift check 00034 applied first (ADR 0025 §1, K15-2), compared as SORTED
-- column sets (attnum order differs on a database whose columns were dropped and added again):
--   * tappa_opdefiner on tenants: SELECT exactly 00029's, 00032's, 00034's, 00039's and 00042's
--     twelve columns, UPDATE exactly 00034's and 00039's four, no INSERT, no DELETE, TRUNCATE,
--     REFERENCES or TRIGGER; on audit_log: INSERT exactly 00034's six, nothing else. A column
--     grant cannot take back a wider privilege another hand gave, so "the definer writes only the
--     suspension, the verdict and the API gate" would be false on the day this file is applied
--     with every test of CI green;
--   * tappa_app and tappa_operator -- and M11's three definers, tappa_holdingdefiner,
--     tappa_feedwriter and tappa_resolver (the two lenses' DÜŞÜK-2, 2026-10-20: each owns
--     SECURITY DEFINER functions tappa_app calls, so a table-level write it held would reach the
--     two new columns through any body that names tenants, now or later) -- NO table-level UPDATE
--     or INSERT on tenants through any role they are a member of (pg_has_role MEMBER: themselves,
--     PUBLIC's grants included -- and a membership without INHERIT too, since SET ROLE reaches
--     it), because the two new columns would fall under such a grant on the spot (00034's D3
--     lesson, widened to the SET ROLE path). tappa_opdefiner is held to its exact lists above.
--     A column grant on a suspension column cannot exist yet (the columns do not); after this
--     file internal/db's opTenantWriteReach pins the same five roles on the two columns.
-- The 2026-10-08 draft (as 00035) looked for single kinds with position(); the CHECKs are now
-- compared WHOLE, as 00039 and 00042 compare them -- a set widened or narrowed by anything but
-- this file is a refusal, not a guess.
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
        RAISE EXCEPTION 'migration 00063 (M10 OP-15) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00063 (M10 OP-15): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00063 (M10 OP-15): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00063 (M10 OP-15): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00063 (M10 OP-15): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00063 (M10 OP-15): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00063 (M10 OP-15): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1: tappa_app gets no new privilege). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
         WHERE p.oid = pg_catalog.to_regprocedure('public.op_begin_read(text, text, jsonb)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_audit(text, text, text, integer, integer)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_tenants(text, text, text, integer, integer)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_tenant_detail(text, text, uuid)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR pg_catalog.pg_get_function_result(pg_catalog.to_regprocedure('public.op_read_tenants(text, text, text, integer, integer)'))
           IS DISTINCT FROM 'TABLE(tenant_id uuid, tenant_name text, created_at timestamp with time zone, plan text)'
       OR pg_catalog.pg_get_function_result(pg_catalog.to_regprocedure('public.op_read_tenant_detail(text, text, uuid)'))
           IS DISTINCT FROM 'TABLE(tenant_id uuid, tenant_name text, created_at timestamp with time zone, plan text, business_type text, location_count bigint, active_employee_count bigint, active_plaque_count bigint, active_admin_count bigint, api_enabled_at timestamp with time zone, api_code text)'
       OR pg_catalog.to_regprocedure('public.op_suspend_tenant(text, uuid, text)') IS NOT NULL
       OR pg_catalog.to_regprocedure('public.op_reinstate_tenant(text, uuid)') IS NOT NULL
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_attribute a
                   WHERE a.attrelid = 'public.tenants'::regclass AND NOT a.attisdropped
                     AND a.attname IN ('suspended_at', 'suspension_reason'))
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_constraint c
                   WHERE c.conrelid = 'public.tenants'::regclass
                     AND c.conname IN ('tenants_suspension_both_or_neither', 'tenants_suspension_reason_check')) THEN
        RAISE EXCEPTION 'migration 00063 (M10 OP-15) replaces op_begin_read, op_read_audit, op_read_tenants and op_read_tenant_detail as migration 00062 left them and adds the suspension, and 00062''s state is not there (the four functions with their owner, 00029''s and 00057''s result columns, no suspension column, constraint or function yet). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF (SELECT pg_catalog.replace(pg_catalog.pg_get_constraintdef(c.oid), ' NOT VALID', '')
          FROM pg_catalog.pg_constraint c
         WHERE c.conrelid = 'public.operator_audit_log'::regclass
           AND c.conname = 'operator_audit_log_kind_check')
         IS DISTINCT FROM 'CHECK ((kind = ANY (ARRAY[''login_failed''::text, ''unknown_email''::text, ''totp_failed''::text, ''locked''::text, ''enrollment_failed''::text, ''password_ok''::text, ''login''::text, ''enrollment''::text, ''logout''::text, ''read''::text, ''legal_publish''::text, ''operator_created''::text, ''operator_mfa_reset''::text, ''operator_disabled''::text, ''tenant_vat_checked''::text, ''tenant_api_enabled''::text, ''tenant_api_disabled''::text, ''holding_created''::text, ''holding_member_added''::text, ''holding_member_removed''::text])))'
       OR (SELECT pg_catalog.pg_get_constraintdef(c.oid) FROM pg_catalog.pg_constraint c
            WHERE c.conrelid = 'public.operator_audit_log'::regclass
              AND c.conname = 'operator_audit_log_tenant_write_shape')
         IS DISTINCT FROM 'CHECK (((kind <> ALL (ARRAY[''tenant_vat_checked''::text, ''tenant_api_enabled''::text, ''tenant_api_disabled''::text])) OR ((target_tenant_id IS NOT NULL) AND (target_admin_id IS NULL) AND (target_scope IS NULL) AND (page_number IS NULL) AND (page_size IS NULL) AND (detail = ''{}''::jsonb))))' THEN
        RAISE EXCEPTION 'migration 00063 (M10 OP-15) widens operator_audit_log_kind_check and operator_audit_log_tenant_write_shape as migrations 00042 and 00039 left them, and the constraints are not in that state -- a migration between 00062 and this one changed them; rebase this file onto it. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    -- THE FOUR BODIES, BY DIGEST: md5(prosrc) of 00042's op_begin_read and op_read_audit, 00029's
    -- op_read_tenants and 00057's op_read_tenant_detail -- the bodies sections 5-7 replace whole.
    IF (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
         WHERE p.oid = pg_catalog.to_regprocedure('public.op_begin_read(text, text, jsonb)'))
         IS DISTINCT FROM 'a9e5a8f97dd5ee5e0c46ef4c80338898'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_audit(text, text, text, integer, integer)'))
         IS DISTINCT FROM '9e81b37e4b89654f1aa64f8cbb6111b9'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_tenants(text, text, text, integer, integer)'))
         IS DISTINCT FROM 'b477d5d1f98361f4735271e789553470'
       OR (SELECT pg_catalog.md5(p.prosrc) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_tenant_detail(text, text, uuid)'))
         IS DISTINCT FROM 'adfaeaf3423253af58ce7aff1bb3cc83' THEN
        RAISE EXCEPTION 'migration 00063 (M10 OP-15) replaces op_begin_read, op_read_audit, op_read_tenants and op_read_tenant_detail whole with the bodies migrations 00042, 00029 and 00057 left plus its edits, and a live body is not that -- a migration between 00062 and this one changed it, and this file would silently undo that; rebase this file onto it. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF (SELECT coalesce(string_agg(a.attname, ',' ORDER BY a.attname), '') FROM pg_catalog.pg_attribute a
         WHERE a.attrelid = 'public.tenants'::regclass AND a.attnum > 0 AND NOT a.attisdropped
           AND pg_catalog.has_column_privilege('tappa_opdefiner', a.attrelid, a.attnum, 'SELECT'))
           IS DISTINCT FROM 'api_code,api_enabled_at,business_type,created_at,id,name,plan,price_per_employee_month,timezone,vat_checked_at,vat_number,vat_verified'
       OR (SELECT coalesce(string_agg(a.attname, ',' ORDER BY a.attname), '') FROM pg_catalog.pg_attribute a
            WHERE a.attrelid = 'public.tenants'::regclass AND a.attnum > 0 AND NOT a.attisdropped
              AND pg_catalog.has_column_privilege('tappa_opdefiner', a.attrelid, a.attnum, 'UPDATE'))
           IS DISTINCT FROM 'api_code,api_enabled_at,vat_checked_at,vat_verified'
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.tenants', 'INSERT')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.tenants', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.tenants', 'TRUNCATE')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.tenants', 'REFERENCES')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.tenants', 'TRIGGER')
       OR (SELECT coalesce(string_agg(a.attname, ',' ORDER BY a.attname), '') FROM pg_catalog.pg_attribute a
            WHERE a.attrelid = 'public.audit_log'::regclass AND a.attnum > 0 AND NOT a.attisdropped
              AND pg_catalog.has_column_privilege('tappa_opdefiner', a.attrelid, a.attnum, 'INSERT'))
           IS DISTINCT FROM 'action,actor_id,at,detail,target,tenant_id'
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.audit_log', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.audit_log', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.audit_log', 'DELETE')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.audit_log', 'TRUNCATE')
       OR EXISTS (SELECT 1
                    FROM pg_catalog.pg_roles who
                    JOIN pg_catalog.pg_roles r ON pg_catalog.pg_has_role(who.oid, r.oid, 'MEMBER')
                   WHERE who.rolname IN ('tappa_app', 'tappa_operator', 'tappa_holdingdefiner',
                                         'tappa_feedwriter', 'tappa_resolver')
                     AND (pg_catalog.has_table_privilege(r.oid, 'public.tenants', 'UPDATE')
                          OR pg_catalog.has_table_privilege(r.oid, 'public.tenants', 'INSERT'))) THEN
        RAISE EXCEPTION 'migration 00063 (M10 OP-15) grants tappa_opdefiner two column privileges on tenants and adds two columns tappa_app must not write, and a privilege no migration gave is already there (tappa_opdefiner reaches more or less of tenants or audit_log than 00034, 00039 and 00042 left it, or a table-level UPDATE or INSERT on tenants reaches tappa_app, tappa_operator, tappa_holdingdefiner, tappa_feedwriter or tappa_resolver through a role they can use). A column grant cannot narrow it. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 2. tenants: the two columns and their two CHECKs.
-- ---------------------------------------------------------------------------
--   suspended_at       when the suspension BEGAN: the first suspending call's wall clock (a
--                      second suspension keeps it -- K15-4); NULL when the tenant is not
--                      suspended.
--   suspension_reason  one code of the closed set, replaced by a second suspension; NULL when
--                      the tenant is not suspended. Its sentence is the panel's and the
--                      operator screen's (phases B and C); the code is what is stored and logged.
-- The CHECKs bind every writer, the owner included: both set or both NULL
-- (tenants_suspension_both_or_neither), and the reason in the closed set
-- (tenants_suspension_reason_check -- a NULL reason passes it, the first CHECK decides when NULL
-- is allowed). The set is written here, in op_suspend_tenant's refusal below, in internal/db's
-- SuspensionReasons -- three copies held equal by TestOperator00063_TheReasonSetIsOneSetInEveryCopy
-- (phase B's panel sentences and phase C's operator labels join that test).
-- No DEFAULT and no NOT NULL: adding the columns rewrites nothing. One statement, so the two
-- CHECKs are validated in ONE scan of the table. NO INDEX (ADR 0025 §1).
ALTER TABLE tenants
    ADD COLUMN suspended_at timestamptz,
    ADD COLUMN suspension_reason text,
    ADD CONSTRAINT tenants_suspension_both_or_neither
        CHECK ((suspended_at IS NULL) = (suspension_reason IS NULL)),
    ADD CONSTRAINT tenants_suspension_reason_check
        CHECK (suspension_reason IN ('non_payment', 'terms_breach', 'security_hold', 'customer_request'));

-- ---------------------------------------------------------------------------
-- 3. Grants: the two columns, to the definer, and nothing else.
-- ---------------------------------------------------------------------------
-- tappa_opdefiner (ADR 0025 §1; the WHOLE resulting lists are pinned in
-- TestOperator00026_PrivilegeMatrix's allow-list and in internal/db's privilege catalogue):
--   tenants SELECT  suspended_at, suspension_reason -- the overview's and the list's new columns,
--                   and the suspension's own UPDATE (coalesce reads suspended_at);
--   tenants UPDATE  suspended_at, suspension_reason -- the two writes. A row lock (the writes'
--                   FOR NO KEY UPDATE) needs UPDATE on some column; 00034's verdict columns and
--                   00039's API columns already gave it, these do too.
-- NOT id, NOT name, NOT any other column; nothing on audit_log -- 00034's INSERT (tenant_id,
-- actor_id, action, target, detail, at) is what the tenant row needs, and it is not granted again
-- (00053 narrowed tappa_app's INSERT on audit_log and left the definer's untouched).
-- tappa_operator gets nothing on tenants; tappa_app's grants are not touched (it reads the two
-- columns through 00001's table-level SELECT and writes neither: section 1 refused the cluster
-- where it could). No other role is touched: tappa_holdingdefiner, tappa_feedwriter and
-- tappa_resolver read named tenants columns (00043, 00049, 00051), none of these two.
GRANT SELECT (suspended_at, suspension_reason) ON tenants TO tappa_opdefiner;
GRANT UPDATE (suspended_at, suspension_reason) ON tenants TO tappa_opdefiner;

-- ---------------------------------------------------------------------------
-- 4. The audit kinds: 'tenant_suspended', 'tenant_reinstated', and the tenant-write shape.
-- ---------------------------------------------------------------------------
--   'tenant_suspended'   op_suspend_tenant: written on EVERY accepted call, whether the tenant
--                        exists and whatever it held (B14; the tenant's own row says what changed);
--   'tenant_reinstated'  op_reinstate_tenant: the same.
-- Both are SESSION kinds (actor_shape's third arm, unchanged) and TENANT-WRITE kinds: they join
-- operator_audit_log_tenant_write_shape's list, so the operator's row names its tenant and nothing
-- else -- detail exactly {} (the orchestrator's K15-5 became the schema's shape in 00034).
-- operator_audit_log_holding_write_shape (00042) does not name them and is not touched.
-- NOT VALID, AND WHEN (00031's rule, as 00034, 00039 and 00042 applied it): a row of a kind
-- outside the twenty-two can only be a later migration's, left behind by its Down; the condition
-- is the kind alone. The tenant-write CHECK is re-added VALIDATED (00034's argument, its LV7): it
-- constrains only its own kinds, and a row of those kinds was written under this very shape.
-- Both constraints keep their NAMES, so the next migration finds them here.
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_audit_log
                WHERE kind <> ALL (ARRAY['login_failed', 'unknown_email', 'totp_failed', 'locked',
                                         'enrollment_failed', 'password_ok', 'login', 'enrollment',
                                         'logout', 'read', 'legal_publish', 'operator_created',
                                         'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                                         'tenant_api_disabled', 'holding_created', 'holding_member_added',
                                         'holding_member_removed', 'tenant_suspended', 'tenant_reinstated'])) THEN
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                            'tenant_api_disabled', 'holding_created', 'holding_member_added',
                            'holding_member_removed', 'tenant_suspended', 'tenant_reinstated'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                            'tenant_api_disabled', 'holding_created', 'holding_member_added',
                            'holding_member_removed', 'tenant_suspended', 'tenant_reinstated'));
    END IF;
END
$$;
-- +goose StatementEnd
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_tenant_write_shape;
ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_tenant_write_shape CHECK (
    kind <> ALL (ARRAY['tenant_vat_checked', 'tenant_api_enabled', 'tenant_api_disabled', 'tenant_suspended',
                       'tenant_reinstated'])
    OR (target_tenant_id IS NOT NULL AND target_admin_id IS NULL AND target_scope IS NULL
        AND page_number IS NULL AND page_size IS NULL AND detail = '{}'::jsonb)
);

-- ===========================================================================
-- 5. op_begin_read, REPLACED: two more kinds in the log-read filter list
-- ===========================================================================
-- 00042's body but for ONE list: the 'operator_audit' read's filter names the two kinds (else a
-- read filtered to them would be refused with 22023). No read kind is added -- the overview's
-- read kind, 'tenant_detail', carries the suspension -- so the closed set of read kinds and the
-- {tenant_id} branch are 00042's. Everything 00027-00042 say of this function still holds; the
-- five copies of the audit kind set (this list, the CHECK, op_read_audit's filter shape,
-- internal/db's OperatorAuditKinds and the viewer's words) are held equal by
-- TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree and
-- TestAuditWords_NameEveryKindAndNothingElse.
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
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings') THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_keys := (SELECT array_agg(k.key ORDER BY k.key) FROM jsonb_object_keys(p_params) AS k(key));

    IF p_kind IN ('tenant_detail', 'tenant_plaques', 'tenant_vat') THEN
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
                                                   'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked',
                                                   'tenant_api_enabled', 'tenant_api_disabled',
                                                   'holding_created', 'holding_member_added',
                                                   'holding_member_removed', 'tenant_suspended',
                                                   'tenant_reinstated') THEN
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
-- 6. op_read_audit, REPLACED: the two kinds in its filter shape
-- ===========================================================================
-- 00042's body but for ONE list: the values an 'operator_audit' read's {"filter": ...} may hold
-- (else a read filtered to the new kinds would come back "detail not shown"). The operator's two
-- new rows need no new arm: their scope is NULL and their detail {} (section 4's CHECK makes
-- both certain), which the arms' last line (ELSE) recognises -- the holding arm (00042) names its
-- three kinds only. No scope is added (no new read kind).
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
                                                            'operator_disabled', 'tenant_vat_checked',
                                                            'tenant_api_enabled', 'tenant_api_disabled',
                                                            'holding_created', 'holding_member_added',
                                                            'holding_member_removed', 'tenant_suspended',
                                                            'tenant_reinstated')
                       WHEN p.kind = 'read' THEN
                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing',
                                                'tenant_vat', 'holdings')
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
                       WHEN p.kind IN ('holding_created', 'holding_member_added', 'holding_member_removed') THEN
                            p.target_scope IS NULL
                            AND p.detail ? 'holding_id' AND (p.detail - 'holding_id') = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'holding_id') = 'string'
                            AND (p.detail ->> 'holding_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                       ELSE p.target_scope IS NULL AND p.detail = '{}'::jsonb
                   END, false) AS recognised
              FROM page AS p
        )
        SELECT s.id, s.at, s.kind, s.session_id,
               s.actor_admin_id, actor_row.display_name,
               s.target_admin_id, target_row.display_name,
               s.target_tenant_id, tenant_row.name,
               CASE WHEN s.target_scope IN ('legal_versions', 'tenants', 'tenant_detail',
                                            'tenant_plaques', 'operator_audit', 'tenant_billing',
                                            'tenant_vat', 'holdings')
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

-- ===========================================================================
-- 7. op_read_tenants and op_read_tenant_detail: the suspension, at the END of each row
-- ===========================================================================
-- CREATE OR REPLACE cannot change a function's result type, so each is DROPPED and CREATED, and
-- its owner, its REVOKE and its GRANT are written again here (ADR 0021 §2 vi; the reverse
-- catalogue pin would see a forgotten OWNER TO, the forward pin a forgotten REVOKE or GRANT).
-- The earlier bodies otherwise -- 00029's list; 00057's overview, the API state included -- the
-- consuming UPDATE, the belt, the 200-row ceiling, the zero rows for an unknown id; the new
-- columns are LAST (Go scans by position):
--   op_read_tenants        + suspended_at                     (the list's chip -- K15-3);
--   op_read_tenant_detail  + suspended_at, suspension_reason  (the overview's status line), after
--                            00057's api_enabled_at and api_code.
-- The reason is the stored code; its label is the operator screen's (phase C), and a code this
-- build does not know is shown as itself and "unrecognised" there.
-- 🔴 THE BELT: the columns come from the same `t` row each read already names by its filter --
-- the detail's `t.id = p_tenant_id`; the list's rows are the page it already returns.
DROP FUNCTION public.op_read_tenants(text, text, text, integer, integer);
-- +goose StatementBegin
CREATE FUNCTION public.op_read_tenants(p_session text, p_ticket text, p_query text,
                                       p_page_number integer, p_page_size integer)
    RETURNS TABLE (tenant_id uuid, tenant_name text, created_at timestamptz, plan text,
                   suspended_at timestamptz)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
    v_limit   integer;
    v_id      uuid;
    v_address boolean;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    UPDATE public.operator_read_tickets AS k
       SET consumed_at = clock_timestamp()
     WHERE k.ticket_hash = encode(sha256(convert_to(
               p_ticket || jsonb_build_object('page_number', p_page_number,
                                              'page_size', p_page_size,
                                              'query', p_query)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'tenants'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_tenants: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    v_limit := least(p_page_size, 200);
    IF p_query ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        v_id := p_query::uuid;
    END IF;
    v_address := strpos(p_query, '@') > 0;

    RETURN QUERY
        SELECT t.id, t.name, t.created_at, t.plan, t.suspended_at
          FROM public.tenants AS t
         WHERE strpos(lower(t.name), lower(p_query)) > 0
            OR t.id = v_id
            OR (v_address AND t.id IN (SELECT u.tenant_id
                                         FROM public.admin_users AS u
                                        WHERE u.email OPERATOR(public.=) p_query::public.citext))
         ORDER BY t.created_at DESC, t.id DESC
         LIMIT v_limit
        OFFSET (p_page_number - 1)::bigint * v_limit;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_tenants(text, text, text, integer, integer) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_tenants(text, text, text, integer, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_tenants(text, text, text, integer, integer) TO tappa_operator;

DROP FUNCTION public.op_read_tenant_detail(text, text, uuid);
-- +goose StatementBegin
CREATE FUNCTION public.op_read_tenant_detail(p_session text, p_ticket text, p_tenant_id uuid)
    RETURNS TABLE (tenant_id uuid, tenant_name text, created_at timestamptz, plan text,
                   business_type text, location_count bigint,
                   active_employee_count bigint, active_plaque_count bigint,
                   active_admin_count bigint, api_enabled_at timestamptz,
                   api_code text, suspended_at timestamptz,
                   suspension_reason text)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    UPDATE public.operator_read_tickets AS k
       SET consumed_at = clock_timestamp()
     WHERE k.ticket_hash = encode(sha256(convert_to(
               p_ticket || jsonb_build_object('tenant_id', p_tenant_id)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'tenant_detail'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_tenant_detail: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    RETURN QUERY
        SELECT t.id, t.name, t.created_at, t.plan, t.business_type,
               (SELECT count(*) FROM public.locations AS l
                 WHERE l.tenant_id = p_tenant_id),
               (SELECT count(*) FROM public.employees AS e
                 WHERE e.tenant_id = p_tenant_id AND e.status = 'active'),
               (SELECT count(*) FROM public.tags AS g
                 WHERE g.tenant_id = p_tenant_id AND g.status = 'active'),
               (SELECT count(*) FROM public.admin_users AS a
                 WHERE a.tenant_id = p_tenant_id AND a.status = 'active'),
               t.api_enabled_at, t.api_code, t.suspended_at, t.suspension_reason
          FROM public.tenants AS t
         WHERE t.id = p_tenant_id;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_tenant_detail(text, text, uuid) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_tenant_detail(text, text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_tenant_detail(text, text, uuid) TO tappa_operator;

-- ===========================================================================
-- 8. op_suspend_tenant -- the operator suspends one tenant
-- ===========================================================================
-- A ONE-PHASE WRITE (ADR 0021 §2 v 7), RETURNS void, in the order the header gives:
--   (a) the session (28000 for a dead one; the actor is derived, never an argument -- §2 i);
--   (b) p_tenant_id NULL, p_reason NULL or a reason outside the closed set: 22023, one constant
--       message, before anything else is read (the comparison is exact: no case folding, no
--       trimming -- the CHECK holds the codes in lower case);
--   (c) the tenant's row by its primary key alone, FOR NO KEY UPDATE, nothing of it returned
--       (PERFORM: no variable holds what the tenant held);
--   (d) only IF FOUND: one clock read after the lock; the UPDATE by the primary key --
--       suspended_at keeps its first value (coalesce over the LOCKED row: K15-4), the reason is
--       replaced; the tenant's own audit_log row: action 'tenant.suspended', the operator as
--       actor_id, the tenant id as target, detail exactly {"actor_kind": "operator", "reason":
--       <code>}, `at` that same clock read;
--   (e) on EVERY accepted call, outside the IF: the operator's 'tenant_suspended' row.
-- Called again for a suspended tenant, the call writes all three again (the tenant row on every
-- call -- the row says "the action was applied", not "the state changed").
-- The answer does not depend on what the tenant held or whether it exists
-- (TestOpSuspendTenant_TheAnswerIsTheSameWhateverTheTenantsState), but for a wait on a row
-- another transaction holds (ADR 0025 limit 5;
-- TestOpSuspendTenant_ALockWaitTellsOnlyThatTheTenantExists).
-- +goose StatementBegin
CREATE FUNCTION public.op_suspend_tenant(p_session text, p_tenant_id uuid, p_reason text)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session    uuid;
    v_admin      uuid;
    v_at         timestamptz;
    v_constraint text;
    v_state      text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    IF p_tenant_id IS NULL OR p_reason IS NULL
       OR p_reason NOT IN ('non_payment', 'terms_breach', 'security_hold', 'customer_request') THEN
        RAISE EXCEPTION 'op_suspend_tenant: suspension refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    BEGIN
        PERFORM t.id
           FROM public.tenants AS t
          WHERE t.id = p_tenant_id
            FOR NO KEY UPDATE;

        IF FOUND THEN
            v_at := clock_timestamp();

            UPDATE public.tenants AS t
               SET suspended_at = coalesce(t.suspended_at, v_at),
                   suspension_reason = p_reason
             WHERE t.id = p_tenant_id;

            INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail, at)
            VALUES (p_tenant_id, v_admin, 'tenant.suspended', p_tenant_id::text,
                    jsonb_build_object('actor_kind', 'operator', 'reason', p_reason),
                    v_at);
        END IF;

        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id)
        VALUES ('tenant_suspended', v_session, v_admin, p_tenant_id);
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_suspend_tenant: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_suspend_tenant: suspension refused'
                USING ERRCODE = 'invalid_parameter_value';
    END;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_suspend_tenant(text, uuid, text) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_suspend_tenant(text, uuid, text) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_suspend_tenant(text, uuid, text) TO tappa_operator;

-- ===========================================================================
-- 9. op_reinstate_tenant -- the operator lifts one tenant's suspension
-- ===========================================================================
-- op_suspend_tenant's shape: (a) the session; (b) p_tenant_id NULL is 22023, one constant message;
-- (c) the row lock by the primary key; (d) only IF FOUND: one clock read after the lock, the
-- UPDATE that clears both columns, and the tenant's row -- action 'tenant.reinstated', detail
-- exactly {"actor_kind": "operator"}, `at` that clock read; (e) the operator's 'tenant_reinstated'
-- row on every accepted call. A tenant that is not suspended gets its tenant row all the same
-- (K15-11: the row says the action was applied; the body never reads the previous state).
-- +goose StatementBegin
CREATE FUNCTION public.op_reinstate_tenant(p_session text, p_tenant_id uuid)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session    uuid;
    v_admin      uuid;
    v_at         timestamptz;
    v_constraint text;
    v_state      text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    IF p_tenant_id IS NULL THEN
        RAISE EXCEPTION 'op_reinstate_tenant: reinstatement refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    BEGIN
        PERFORM t.id
           FROM public.tenants AS t
          WHERE t.id = p_tenant_id
            FOR NO KEY UPDATE;

        IF FOUND THEN
            v_at := clock_timestamp();

            UPDATE public.tenants AS t
               SET suspended_at = NULL,
                   suspension_reason = NULL
             WHERE t.id = p_tenant_id;

            INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail, at)
            VALUES (p_tenant_id, v_admin, 'tenant.reinstated', p_tenant_id::text,
                    jsonb_build_object('actor_kind', 'operator'),
                    v_at);
        END IF;

        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id)
        VALUES ('tenant_reinstated', v_session, v_admin, p_tenant_id);
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_reinstate_tenant: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_reinstate_tenant: reinstatement refused'
                USING ERRCODE = 'invalid_parameter_value';
    END;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_reinstate_tenant(text, uuid) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_reinstate_tenant(text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_reinstate_tenant(text, uuid) TO tappa_operator;

-- +goose Down
SET LOCAL lock_timeout = '1s';

-- 🔴 lock_timeout 1 s, THE FIRST STATEMENT (00054's rule, deploy/README.md; 00038's Down does the
-- same): the lock below and the tenants DDL at the end wait at most a second behind a long
-- reader, and the file gives up with 55P03 instead of queueing every tenant read behind it.
-- 🔴 A Down REFUSES WHILE A TENANT IS SUSPENDED (ADR 0025 §2, the orchestrator's K15-6). Dropping
-- the columns would lift every suspension in silence: a Down takes a door away, not a fact (ADR
-- 0021's OP-16 note, md. 7). Reinstate the tenant through op_reinstate_tenant first -- the
-- operator surface, which leaves the two rows that say who lifted it -- then migrate down again.
-- (internal/db's opAtVersion lifts suspensions INSIDE its rolled-back test transaction before it
-- runs this Down; nothing commits.)
-- 🔴 THE CHECK HOLDS UNTIL THE COLUMNS ARE GONE: the Down locks tenants in SHARE ROW EXCLUSIVE
-- mode BEFORE the check (its first statement after the timeout). Without it the check read tenants
-- under ACCESS SHARE only, which every UPDATE's ROW EXCLUSIVE passes, so a suspension committed
-- between the check and the tenants DDL at the end was dropped in silence (the third eye's F1, 3rd
-- round). SHARE ROW EXCLUSIVE is the narrowest mode that shuts every tenants writer out (it
-- conflicts with ROW EXCLUSIVE) AND is self-exclusive (SHARE shuts writers out too, but two SHARE
-- holders that both go on to the DDL's ACCESS EXCLUSIVE deadlock each other), while the hot path
-- goes on: a tap's read (ACCESS SHARE) and the foreign-key checks of the rows that name a tenant
-- (FOR KEY SHARE, ROW SHARE) are granted until the DDL's ACCESS EXCLUSIVE at the end. Taken
-- before the check: under READ COMMITTED (goose's, and the server's default) the check's own
-- snapshot comes after the lock and sees every commit made before the lock was granted; under
-- REPEATABLE READ that holds only when the Down opens its transaction. Its price, counted: a
-- session that locks a tenant row (ROW SHARE -- the op_* writes' FOR NO KEY UPDATE) while this
-- Down runs and then waits for its UPDATE closes a cycle with the ACCESS EXCLUSIVE at the end, and
-- one of the two gets 40P01 after deadlock_timeout: loud, and the Down is one transaction, so
-- nothing is half done (TestOperator00063_TheDownLocksOutTenantWritesBeforeItLooksForASuspension).
-- 🔴 WHAT A SUCCESSFUL Down DOES, named (00030-00042's house style):
--   * the two writes go; op_read_tenants goes back to 00029's definition and op_read_tenant_detail
--     to 00057's, each VERBATIM (dropped and created, so their owner, REVOKE and GRANT are written
--     again -- REVOKE EXECUTE, a function's one privilege); op_read_audit and op_begin_read go
--     back to 00042's Up bodies VERBATIM (replaced, so their owners and ACLs stay)
--     (TestOperator00063_DownGivesBack00062AndUpTakesItAgain compares each with its file);
--   * tappa_opdefiner loses EXACTLY what section 3 granted, by column -- NOT `REVOKE ALL`, which
--     would take 00029's, 00032's, 00034's, 00039's and 00042's tenants columns with it;
--   * operator_audit_log_tenant_write_shape goes back to 00039's three kinds and
--     operator_audit_log_kind_check to 00042's twenty -- the kind CHECK NOT VALID when a row of a
--     kind outside the twenty exists (this file's kinds or a later migration's), VALIDATED when
--     none does; the shape CHECK always VALIDATED (it names fewer kinds, so every row passes it);
--   * the rows STAY: the operator's 'tenant_suspended' and 'tenant_reinstated' rows
--     (operator_audit_log is append-only, 00026) and the tenants' 'tenant.suspended' and
--     'tenant.reinstated' rows (audit_log is append-only, 00005). Nothing here deletes a row;
--   * LAST, the two CHECKs and the two columns of tenants (ADR 0025 limit 9: the light statements
--     first, the tenants DDL last). ⚠️ That DDL takes ACCESS EXCLUSIVE on tenants and holds it
--     until the transaction ends -- inside a test that runs this Down (opAtVersion, which takes
--     the lock first: downTables names tenants), until the test's rollback; measured in
--     TestOperator00063_DownGivesBack00062AndUpTakesItAgain.
LOCK TABLE public.tenants IN SHARE ROW EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.tenants AS t
                WHERE t.suspended_at IS NOT NULL OR t.suspension_reason IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 00063 (M10 OP-15) Down: a tenant is suspended, and dropping the suspension columns would lift its suspension in silence (ADR 0025, K15-6). Reinstate it through the operator surface (op_reinstate_tenant), then migrate down again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP FUNCTION IF EXISTS public.op_reinstate_tenant(text, uuid);
DROP FUNCTION IF EXISTS public.op_suspend_tenant(text, uuid, text);

DROP FUNCTION public.op_read_tenant_detail(text, text, uuid);
-- +goose StatementBegin
CREATE FUNCTION public.op_read_tenant_detail(p_session text, p_ticket text, p_tenant_id uuid)
    RETURNS TABLE (tenant_id uuid, tenant_name text, created_at timestamptz, plan text,
                   business_type text, location_count bigint,
                   active_employee_count bigint, active_plaque_count bigint,
                   active_admin_count bigint, api_enabled_at timestamptz,
                   api_code text)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    UPDATE public.operator_read_tickets AS k
       SET consumed_at = clock_timestamp()
     WHERE k.ticket_hash = encode(sha256(convert_to(
               p_ticket || jsonb_build_object('tenant_id', p_tenant_id)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'tenant_detail'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_tenant_detail: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    RETURN QUERY
        SELECT t.id, t.name, t.created_at, t.plan, t.business_type,
               (SELECT count(*) FROM public.locations AS l
                 WHERE l.tenant_id = p_tenant_id),
               (SELECT count(*) FROM public.employees AS e
                 WHERE e.tenant_id = p_tenant_id AND e.status = 'active'),
               (SELECT count(*) FROM public.tags AS g
                 WHERE g.tenant_id = p_tenant_id AND g.status = 'active'),
               (SELECT count(*) FROM public.admin_users AS a
                 WHERE a.tenant_id = p_tenant_id AND a.status = 'active'),
               t.api_enabled_at, t.api_code
          FROM public.tenants AS t
         WHERE t.id = p_tenant_id;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_tenant_detail(text, text, uuid) OWNER TO tappa_opdefiner;
REVOKE EXECUTE ON FUNCTION public.op_read_tenant_detail(text, text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_tenant_detail(text, text, uuid) TO tappa_operator;

DROP FUNCTION public.op_read_tenants(text, text, text, integer, integer);
-- +goose StatementBegin
CREATE FUNCTION public.op_read_tenants(p_session text, p_ticket text, p_query text,
                                       p_page_number integer, p_page_size integer)
    RETURNS TABLE (tenant_id uuid, tenant_name text, created_at timestamptz, plan text)
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session uuid;
    v_admin   uuid;
    v_limit   integer;
    v_id      uuid;
    v_address boolean;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    UPDATE public.operator_read_tickets AS k
       SET consumed_at = clock_timestamp()
     WHERE k.ticket_hash = encode(sha256(convert_to(
               p_ticket || jsonb_build_object('page_number', p_page_number,
                                              'page_size', p_page_size,
                                              'query', p_query)::text,
               'UTF8')), 'hex')
       AND k.session_id = v_session
       AND k.kind = 'tenants'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_tenants: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    v_limit := least(p_page_size, 200);
    IF p_query ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN
        v_id := p_query::uuid;
    END IF;
    v_address := strpos(p_query, '@') > 0;

    RETURN QUERY
        SELECT t.id, t.name, t.created_at, t.plan
          FROM public.tenants AS t
         WHERE strpos(lower(t.name), lower(p_query)) > 0
            OR t.id = v_id
            OR (v_address AND t.id IN (SELECT u.tenant_id
                                         FROM public.admin_users AS u
                                        WHERE u.email OPERATOR(public.=) p_query::public.citext))
         ORDER BY t.created_at DESC, t.id DESC
         LIMIT v_limit
        OFFSET (p_page_number - 1)::bigint * v_limit;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_tenants(text, text, text, integer, integer) OWNER TO tappa_opdefiner;
REVOKE EXECUTE ON FUNCTION public.op_read_tenants(text, text, text, integer, integer) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_tenants(text, text, text, integer, integer) TO tappa_operator;

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
                                                            'operator_disabled', 'tenant_vat_checked',
                                                            'tenant_api_enabled', 'tenant_api_disabled',
                                                            'holding_created', 'holding_member_added',
                                                            'holding_member_removed')
                       WHEN p.kind = 'read' THEN
                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing',
                                                'tenant_vat', 'holdings')
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
                       WHEN p.kind IN ('holding_created', 'holding_member_added', 'holding_member_removed') THEN
                            p.target_scope IS NULL
                            AND p.detail ? 'holding_id' AND (p.detail - 'holding_id') = '{}'::jsonb
                            AND jsonb_typeof(p.detail -> 'holding_id') = 'string'
                            AND (p.detail ->> 'holding_id') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
                       ELSE p.target_scope IS NULL AND p.detail = '{}'::jsonb
                   END, false) AS recognised
              FROM page AS p
        )
        SELECT s.id, s.at, s.kind, s.session_id,
               s.actor_admin_id, actor_row.display_name,
               s.target_admin_id, target_row.display_name,
               s.target_tenant_id, tenant_row.name,
               CASE WHEN s.target_scope IN ('legal_versions', 'tenants', 'tenant_detail',
                                            'tenant_plaques', 'operator_audit', 'tenant_billing',
                                            'tenant_vat', 'holdings')
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
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'operator_audit', 'tenant_billing', 'tenant_vat', 'holdings') THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    IF jsonb_typeof(p_params) IS DISTINCT FROM 'object' THEN
        RAISE EXCEPTION 'op_begin_read: read parameters refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;
    v_keys := (SELECT array_agg(k.key ORDER BY k.key) FROM jsonb_object_keys(p_params) AS k(key));

    IF p_kind IN ('tenant_detail', 'tenant_plaques', 'tenant_vat') THEN
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
                                                   'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked',
                                                   'tenant_api_enabled', 'tenant_api_disabled',
                                                   'holding_created', 'holding_member_added',
                                                   'holding_member_removed') THEN
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

REVOKE UPDATE (suspended_at, suspension_reason) ON tenants FROM tappa_opdefiner;
REVOKE SELECT (suspended_at, suspension_reason) ON tenants FROM tappa_opdefiner;

ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_tenant_write_shape;
ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_tenant_write_shape CHECK (
    kind <> ALL (ARRAY['tenant_vat_checked', 'tenant_api_enabled', 'tenant_api_disabled'])
    OR (target_tenant_id IS NOT NULL AND target_admin_id IS NULL AND target_scope IS NULL
        AND page_number IS NULL AND page_size IS NULL AND detail = '{}'::jsonb)
);
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_audit_log
                WHERE kind <> ALL (ARRAY['login_failed', 'unknown_email', 'totp_failed', 'locked',
                                         'enrollment_failed', 'password_ok', 'login', 'enrollment',
                                         'logout', 'read', 'legal_publish', 'operator_created',
                                         'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                                         'tenant_api_disabled', 'holding_created', 'holding_member_added',
                                         'holding_member_removed'])) THEN
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                            'tenant_api_disabled', 'holding_created', 'holding_member_added',
                            'holding_member_removed'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked', 'tenant_api_enabled',
                            'tenant_api_disabled', 'holding_created', 'holding_member_added',
                            'holding_member_removed'));
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE tenants
    DROP CONSTRAINT tenants_suspension_reason_check,
    DROP CONSTRAINT tenants_suspension_both_or_neither,
    DROP COLUMN suspension_reason,
    DROP COLUMN suspended_at;
