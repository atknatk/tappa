-- 00034 -- M10 OP-16, phase A (the data layer): the operator reads ONE tenant's VAT status and
-- records the verdict of a VIES re-check -- the FIRST op_* that CHANGES a tenant.
--
-- NORMATIVE SOURCES: docs/adr/0021-op-fonksiyonlari-tenant-otesi-erisim.md §1 (the definer
-- holds column grants; tappa_app and tappa_operator gain nothing on a tenant table), §2 (the
-- op_* contract: the session predicate, two-phase reads, a write returns void and nothing that
-- depends on what the tenant held before -- v 7 --, B14, the wall clock -- vii), §3 (inside an
-- op_* the explicit tenant filter is the ONLY barrier; §3.3: every write grant is its own
-- named decision; §3.4: a tenant-changing write audits into BOTH logs in one transaction -- K6)
-- and §6 (the catalogue and behaviour pins); ADR 0020 §5 (K6: actor_id is the operator,
-- detail.actor_kind = "operator"); migration 00017 (the four states of a VAT check). Where this
-- file decides something those leave open, the decision is written next to the statement and
-- in ADR 0021's "OP-16 uygulama notu".
--
-- WHY: 00017 recorded a sign-up's VIES answer and deliberately opened no way to ask again
-- ("RE-CHECKING A VAT NUMBER LATER IS NOT POSSIBLE"), and 00024 kept tappa_app's UPDATE off the
-- three VAT columns. So a tenant whose check met an outage at sign-up -- or whose outage was
-- recorded as "not valid" -- stays in that state for good. This file lets the OPERATOR record a
-- new verdict; the customer still cannot (tappa_app gains nothing here).
--
-- 🔴 THE FIRST op_* THAT CHANGES A TENANT -- WHAT IS BORN HERE (ADR 0021 §3.4, K6, B14):
--   * tappa_opdefiner gains INSERT on the TENANT's audit_log, `at` among its columns: K6 writes
--     the tenant row's time explicitly from the wall clock (00005's DEFAULT is now(), the
--     transaction's start). That is the writable time column OP-10 avoided on the operator's
--     log; here it is pinned by behaviour (TestOpRecordVATCheck_TheTenantRowsTimeIsTheWallClock),
--     since a catalogue scan cannot see which value an INSERT list puts in it;
--   * the tenant row is written only inside the IF the locked read decides -- the tenant was
--     found and its number is p_vat_number --, so a tenant that does not exist gets no row and
--     the foreign key never fires (no 23503, no existence oracle in the answer -- B14);
--   * the write is BOUND TO A COMMITTED READ of the same tenant by the same session (section 7
--     (b2), round 3): no tenant's number is tested before the trail of reading it has committed;
--   * the change, the tenant row and the operator's row are ONE call, i.e. one statement of the
--     caller's transaction: they commit together or not at all
--     (TestOpRecordVATCheck_TheThreeWritesShareOneTransaction);
--   * a new CHECK on operator_audit_log, operator_audit_log_tenant_write_shape: a row of a
--     tenant-writing kind names its tenant and nothing else -- no account, no scope, no page,
--     detail exactly {} (the orchestrator's K16-3; ADR 0020 §5 names an operator by id and a
--     tenant by id). OP-15's two kinds join its list.
--
-- 🔴 THE DEFINER WRITES THE VERDICT, NOT THE NUMBER (ADR 0021 §3.3's named decision): UPDATE on
-- vat_verified and vat_checked_at, and NOT on vat_number. vat_number is globally UNIQUE (00001),
-- so writing it is a cross-tenant act (00024's argument), and a verdict is about ONE number. The
-- write is bound to the number the operator saw (p_vat_number, T3): a verdict asked for one
-- number never lands on another.
--
-- §4.6 -- "NO ANSWER" IS NEVER WRITTEN AS A VERDICT. p_valid NULL is refused (22023) before
-- anything is read or written: a VIES outage leaves the tenant's state as it was, here as in the
-- screen that calls this (OP-16 B) and the client that asks VIES (OP-16 C). 00017's four states
-- are unchanged; this file can move a tenant into "valid" or "invalid" only, each with the time
-- of the check.
--
-- §4.5 -- CROSSED ON PURPOSE, IN THE ONE PLACE ADR 0021 ALLOWS. Both functions belong to
-- tappa_opdefiner (NOLOGIN, BYPASSRLS), so NO ROW LEVEL SECURITY applies inside them -- not
-- tenants', not audit_log's. What stands instead is `p_tenant_id` on every reference to a
-- tenant: the read's `t.id = p_tenant_id`; the write's locking read and its UPDATE
-- (`t.id = p_tenant_id`, nothing else), the tenant row's tenant_id, and the operator row's
-- target_tenant_id (TestOpReadTenantVAT_ReturnsOnlyTheNamedTenantsNumberAndState,
-- TestOpRecordVATCheck_ACallForOneTenantTouchesNoOther).
--
-- 🔴 THE NUMBER IS NEVER A CONDITION OF A STATEMENT (the third eye's F1, round 2): it is
-- compared in a variable, with the number the locked read returned for p_tenant_id. Round 1
-- put `t.vat_number = p_vat_number` in the locked read, the UPDATE and the EXISTS, and the
-- planner answered those through tenants_vat_number_key with the id as a filter: a call for ANY
-- tenant id, rolled back inside a savepoint, moved the transaction's statistics when the number
-- was registered to SOME tenant -- a membership test on a globally UNIQUE column, across
-- tenants, with no committed trail. A read by the primary key alone cannot reach another
-- tenant's row, whatever the plan (TestOpRecordVATCheck_AnUnknownTenantIsTheSameVoid pins every
-- counter of pg_stat_xact_user_tables alike for an unregistered number and another tenant's).
--
-- WHAT IT DOES, IN ORDER:
--   0. re-checks the two cluster roles' shape (00026's precondition, as 00027-00033 repeated
--      it), that the database is at 00033 (its traces), and that the grants this file adds are
--      the definer's whole reach on the two tables (no drifted ACL);
--   1. the read kinds: operator_read_tickets_kind_check gains 'tenant_vat' -- NOT VALID only
--      when a ticket of a kind outside the new set already exists;
--   2. the audit kinds: operator_audit_log_kind_check gains 'tenant_vat_checked' (same rule),
--      and operator_audit_log_tenant_write_shape is born;
--   3. the grants: tappa_opdefiner gains column SELECTs and a two-column UPDATE on tenants, and
--      a six-column INSERT on audit_log -- nothing else;
--   4. op_begin_read is REPLACED: one more read kind, 'tenant_vat', in the {tenant_id} branch;
--      one more kind in its log-read filter list (00033's body otherwise, byte for byte);
--   5. op_read_audit is REPLACED: the new kind in its filter shape, the new read kind in its two
--      scope lists (00033's body otherwise, byte for byte);
--   6. op_read_tenant_vat -- the read;
--   7. op_record_vat_check -- the write.
--
-- No table is created, so there is no redline waiver, no new RLS and no sequence. tappa_app
-- gains nothing (ADR 0021 §1); tappa_operator gains EXECUTE on the two new functions and no
-- privilege on any table.
--
-- CLOCKS (ADR 0021 §2 vii): every instant either function compares or writes -- the ticket's
-- expiry and consumption, vat_checked_at, the tenant row's `at` -- is clock_timestamp(), and the
-- write reads it ONCE: the verdict's time, the detail's "after" time and the tenant row's `at`
-- are one instant. The operator row's `at` is 00033's trigger's. TestOperator00026_NoFrozenClock
-- walks every function tappa_opdefiner owns.
--
-- SECRETS (ADR 0021 §3.5, CLAUDE.md §7; K16-4): every RAISE carries a constant message; the
-- formatted ones are the precondition's (role names) and the write's LOG line (a constraint
-- name and a SQLSTATE). The VAT number is never in a message, a LOG line or a detail: the
-- tenant row's detail carries the two verdicts and their times, the operator row's detail is {}.
--
-- ⚠️ COUNTED, NOT CLOSED (ADR 0021's OP-16 note): the holder of tappa_operator's DSN can record
-- any verdict without asking VIES (the definer cannot tell an asked verdict from an invented
-- one -- op_open_session's limit in another place); every such write leaves the operator row
-- and the tenant row. And a call inside a SAVEPOINT that is rolled back tells, through the
-- statistics views (ADR 0021 limit 15), whether the tenant id exists and, for one that does,
-- whether p_vat_number is ITS number; and through a lock wait, whether the tenant id exists.
-- Neither tells anything about a number registered to another tenant (measured and pinned, see
-- above), and -- round 3 -- neither is reached without a COMMITTED read of that tenant by the
-- same session (section 7 (b2)), whose second phase returns the number itself.

-- +goose Up

-- ---------------------------------------------------------------------------
-- 0. PRECONDITION: the roles, 00033, and the definer's reach on the two tables.
-- ---------------------------------------------------------------------------
-- The role checks are 00026's, in 00026's order, as 00027-00033 repeated them: an op_* owned by
-- a SUPERUSER, a LOGIN or a member-carrying tappa_opdefiner is a general bypass with every other
-- check green, and a member of tappa_operator inherits EXECUTE on the functions below -- one of
-- which WRITES any tenant's VAT verdict.
-- 00033: the two functions this file replaces belong to tappa_opdefiner, 00032's read exists,
-- 00033's `at` trigger is on operator_audit_log, the audit kind CHECK names 00033's
-- 'operator_disabled' and not yet this file's kind, the ticket kind CHECK names 00032's
-- 'tenant_billing' and not yet this file's, and this file's CHECK is not there. A database that
-- fails this was not migrated by goose in order, and replacing functions on it would be a guess.
-- THE REACH (the OP-15 card's drift check, applied here first): the grants below are written as
-- column lists, and a column grant cannot take back a wider privilege some other hand gave
-- (00024 measured it for tappa_app). So: tappa_opdefiner holds no UPDATE on tenants and no
-- privilege at all on audit_log, and tappa_app holds no UPDATE on the three VAT columns -- else
-- "the definer writes the verdict and not the number" and "the customer cannot write a verdict"
-- would be false on the day this file is applied, with every test of CI green.
-- (position(... IN ...), the SQL syntax, rather than a schema-qualified call: 00033's note on
-- redline R7.)
-- +goose StatementBegin
DO $$
DECLARE
    v_missing text;
    v_kinds   text;
    v_tickets text;
BEGIN
    SELECT string_agg(r.name, ', ' ORDER BY r.name)
      INTO v_missing
      FROM unnest(ARRAY['tappa_operator', 'tappa_opdefiner']) AS r(name)
     WHERE NOT EXISTS (SELECT 1 FROM pg_catalog.pg_roles p WHERE p.rolname = r.name);
    IF v_missing IS NOT NULL THEN
        RAISE EXCEPTION 'migration 00034 (M10 OP-16) needs the cluster role(s) % and deliberately does not create them. Run the one-time step in deploy/README.md, section "Operator roles (M10 OP-5)" (it applies the OPERATOR ROLES block of scripts/db-init/01-roles.sql), then migrate again. Nothing was changed.', v_missing
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_opdefiner'
                  AND (rolsuper OR NOT rolbypassrls OR rolcanlogin
                       OR rolcreaterole OR rolcreatedb OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00034 (M10 OP-16): role tappa_opdefiner must be NOLOGIN NOSUPERUSER BYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_roles
                WHERE rolname = 'tappa_operator'
                  AND (rolsuper OR rolbypassrls OR rolcreaterole OR rolcreatedb
                       OR rolreplication)) THEN
        RAISE EXCEPTION 'migration 00034 (M10 OP-16): role tappa_operator must be NOSUPERUSER NOBYPASSRLS NOCREATEDB NOCREATEROLE NOREPLICATION. Re-run the OPERATOR ROLES block of scripts/db-init/01-roles.sql (deploy/README.md, section "Operator roles (M10 OP-5)"). Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00034 (M10 OP-16): role tappa_opdefiner has members; membership is one SET ROLE away from BYPASSRLS (ADR 0021 §1). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_opdefiner') THEN
        RAISE EXCEPTION 'migration 00034 (M10 OP-16): role tappa_opdefiner is a member of another role; the op_* owner must be a member of nothing, or its functions inherit that role''s privileges (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.member
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00034 (M10 OP-16): role tappa_operator is a member of another role; it must be a member of nothing (ADR 0021 §1). Revoke that membership, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF EXISTS (SELECT 1 FROM pg_catalog.pg_auth_members m
                 JOIN pg_catalog.pg_roles r ON r.oid = m.roleid
                WHERE r.rolname = 'tappa_operator') THEN
        RAISE EXCEPTION 'migration 00034 (M10 OP-16): role tappa_operator has members; a member inherits EXECUTE on every op_* (ADR 0021 §1: tappa_app gets no new privilege). Revoke them, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
         WHERE p.oid = pg_catalog.to_regprocedure('public.op_read_audit(text, text, text, integer, integer)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR (SELECT pg_catalog.pg_get_userbyid(p.proowner) FROM pg_catalog.pg_proc p
            WHERE p.oid = pg_catalog.to_regprocedure('public.op_begin_read(text, text, jsonb)'))
           IS DISTINCT FROM 'tappa_opdefiner'
       OR pg_catalog.to_regprocedure('public.op_read_tenant_billing(text, text, uuid, integer)') IS NULL
       OR NOT EXISTS (SELECT 1 FROM pg_catalog.pg_trigger t
                       WHERE t.tgrelid = 'public.operator_audit_log'::regclass
                         AND t.tgname = 'operator_audit_log_at_is_the_wall_clock') THEN
        RAISE EXCEPTION 'migration 00034 (M10 OP-16) replaces op_begin_read and op_read_audit as migration 00033 left them, and 00033''s state is not there (the two functions, 00032''s read, the `at` trigger). Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    SELECT pg_catalog.pg_get_constraintdef(c.oid)
      INTO v_kinds
      FROM pg_catalog.pg_constraint c
     WHERE c.conrelid = 'public.operator_audit_log'::regclass
       AND c.conname = 'operator_audit_log_kind_check';
    SELECT pg_catalog.pg_get_constraintdef(c.oid)
      INTO v_tickets
      FROM pg_catalog.pg_constraint c
     WHERE c.conrelid = 'public.operator_read_tickets'::regclass
       AND c.conname = 'operator_read_tickets_kind_check';
    IF v_kinds IS NULL OR v_tickets IS NULL
       OR position('''operator_disabled''' IN v_kinds) = 0
       OR position('''tenant_vat_checked''' IN v_kinds) > 0
       OR position('''tenant_billing''' IN v_tickets) = 0
       OR position('''tenant_vat''' IN v_tickets) > 0
       OR EXISTS (SELECT 1 FROM pg_catalog.pg_constraint c
                   WHERE c.conrelid = 'public.operator_audit_log'::regclass
                     AND c.conname = 'operator_audit_log_tenant_write_shape') THEN
        RAISE EXCEPTION 'migration 00034 (M10 OP-16) widens operator_audit_log_kind_check and operator_read_tickets_kind_check as migration 00033 left them and adds operator_audit_log_tenant_write_shape, and the constraints are not in that state. Migrate in order. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    IF pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.tenants', 'UPDATE')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.audit_log', 'SELECT')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.audit_log', 'INSERT')
       OR pg_catalog.has_any_column_privilege('tappa_opdefiner', 'public.audit_log', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_opdefiner', 'public.audit_log', 'DELETE')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.tenants', 'vat_number', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.tenants', 'vat_verified', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.tenants', 'vat_checked_at', 'UPDATE') THEN
        RAISE EXCEPTION 'migration 00034 (M10 OP-16) grants tappa_opdefiner column privileges on tenants and audit_log, and a privilege no migration gave is already there (tappa_opdefiner holds UPDATE on tenants or a privilege on audit_log, or tappa_app may UPDATE a VAT column). A column grant cannot narrow it. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 1. The read kinds: 'tenant_vat'.
-- ---------------------------------------------------------------------------
--   'tenant_vat'  op_read_tenant_vat: one tenant's VAT number and the verdict on it.
-- The read is one 'read' row whose target_scope is the read kind, whose target_tenant_id names
-- the tenant, with no page and detail {} -- written before anything is shown (00027 section 1).
-- NOT VALID, AND WHEN (00031's rule, for tickets; 00032 section 1): a ticket of a kind outside
-- the seven can only be a later migration's, left behind by its Down -- consumed or not -- so the
-- condition is the kind alone. With such a ticket present a VALIDATED re-add fails with 23514,
-- so the CHECK is then added NOT VALID -- binding every new row, not re-checking the old one --
-- and VALIDATED otherwise. The constraint keeps its NAME, so the next migration finds it here.
ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_read_tickets
                WHERE kind <> ALL (ARRAY['legal_versions', 'tenants', 'tenant_detail',
                                         'tenant_plaques', 'operator_audit', 'tenant_billing',
                                         'tenant_vat'])) THEN
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit', 'tenant_billing', 'tenant_vat'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit', 'tenant_billing', 'tenant_vat'));
    END IF;
END
$$;
-- +goose StatementEnd

-- ---------------------------------------------------------------------------
-- 2. The audit kinds: 'tenant_vat_checked', and the tenant-write shape.
-- ---------------------------------------------------------------------------
--   'tenant_vat_checked'  op_record_vat_check: the operator recorded a VIES verdict for the
--                         tenant -- written on EVERY accepted call, whether or not the number
--                         matched and whether or not the tenant exists (B14; the tenant's own
--                         row is the one that says a verdict changed).
-- The name follows the log's pattern for a fact (the thing, then what happened to it). It is a
-- SESSION kind: actor_shape's third arm (00033) excludes every kind outside its two lists, so the
-- row must carry its session and its actor with actor_shape unchanged.
-- 🔴 operator_audit_log_tenant_write_shape -- BORN HERE (the "first tenant-changing op_*" load):
-- a row of a tenant-writing kind names its tenant (target_tenant_id NOT NULL) and nothing else --
-- no account, no scope, no page, detail exactly {} (K16-3). So the operator's row cannot say
-- what was written: the tenant's own audit_log row does, in the tenant's log, and the number is
-- in neither. The kind list is written `kind <> ALL (ARRAY[...])` so OP-15's two kinds join it
-- as two more names, and op_read_audit's last shape line already recognises such a row (scope
-- NULL, detail {}).
-- NOT VALID, AND WHEN (00031's rule, for the kind CHECK): a row of a kind outside the fifteen
-- can only be a later migration's, left behind by its Down; the question is the kind alone.
-- The tenant-write CHECK needs no such branch: it constrains only rows of its own kinds and
-- passes every other row, and a row of its kind can exist only if this file's Up admitted it,
-- under this very CHECK (00033's kind CHECK, which a Down of this file puts back, refuses a NEW
-- one -- NOT VALID binds new rows). A later migration that changed this shape and left a row of
-- the other shape behind would make this VALIDATED add fail -- ADR 0021's OP-16 note counts it;
-- no such migration exists.
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_audit_log
                WHERE kind <> ALL (ARRAY['login_failed', 'unknown_email', 'totp_failed', 'locked',
                                         'enrollment_failed', 'password_ok', 'login', 'enrollment',
                                         'logout', 'read', 'legal_publish', 'operator_created',
                                         'operator_mfa_reset', 'operator_disabled',
                                         'tenant_vat_checked'])) THEN
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked'));
    END IF;
END
$$;
-- +goose StatementEnd
ALTER TABLE operator_audit_log ADD CONSTRAINT operator_audit_log_tenant_write_shape CHECK (
    kind <> ALL (ARRAY['tenant_vat_checked'])
    OR (target_tenant_id IS NOT NULL AND target_admin_id IS NULL AND target_scope IS NULL
        AND page_number IS NULL AND page_size IS NULL AND detail = '{}'::jsonb)
);

-- ---------------------------------------------------------------------------
-- 3. Grants: what the two functions touch and no earlier file granted.
-- ---------------------------------------------------------------------------
-- tappa_opdefiner, column by column (ADR 0021 §1; the WHOLE resulting lists are pinned in
-- TestOperator00026_PrivilegeMatrix's allow-list):
--   tenants SELECT  vat_number, vat_verified, vat_checked_at. With 00029's id and name, the
--                   read's whole view (the tenant, its number, the verdict and its time); and the
--                   write's: its WHERE names id alone, its locking read returns the number (to be
--                   compared in a variable) and the two verdict columns as they were (the
--                   "before" of the tenant row).
--   tenants UPDATE  vat_verified, vat_checked_at -- the verdict and the time it was reached.
--                   🔴 NOT vat_number (ADR 0021 §3.3: the named decision; the header says why).
--                   A row lock (the write's FOR NO KEY UPDATE) needs UPDATE on at least one
--                   column of the table, and this is that column grant. id, name and every other
--                   column stay off the list.
--   audit_log INSERT  tenant_id, actor_id, action, target, detail, at -- K6's row. `at` is on the
--                   list ON PURPOSE: the row's time is written explicitly from the wall clock
--                   (00005's DEFAULT now() is the transaction's start -- ADR 0021 §2 vii, "Yazılan
--                   zaman damgaları"). NOT id (its DEFAULT), and no SELECT, UPDATE or DELETE:
--                   the definer appends and reads nothing back (no RETURNING).
-- NOTHING NEW for the write's binding to a committed read (section 7 (b2)): it reads
-- operator_read_tickets' session_id, kind, target_tenant_id, audit_id and created_xact (00026's
-- SELECT) and operator_audit_log's id, kind, session_id, target_scope and target_tenant_id
-- (00027's and 00031's) -- the columns op_read_* and op_read_audit already read.
-- 🔴 00029's and 00032's tenants columns are NOT granted again, and this file's Down does NOT
-- revoke them (and does not write REVOKE ALL): a column holds ONE ACL entry per grantee and
-- privilege, so taking back a column an earlier file granted would take that file's grant
-- with it (00030's lesson). tappa_operator gets nothing on either table; tappa_app's grants are
-- not touched (it keeps INSERT on the three VAT columns, sign-up's, and no UPDATE on them).
GRANT SELECT (vat_number, vat_verified, vat_checked_at) ON tenants TO tappa_opdefiner;
GRANT UPDATE (vat_verified, vat_checked_at) ON tenants TO tappa_opdefiner;
GRANT INSERT (tenant_id, actor_id, action, target, detail, at) ON audit_log TO tappa_opdefiner;

-- ===========================================================================
-- 4. op_begin_read, REPLACED: one more read kind
-- ===========================================================================
-- 00033's body but for three lists: the closed set of read kinds names 'tenant_vat'; the
-- {tenant_id} branch -- 00029's tenant_detail, 00030's tenant_plaques -- names it too, so its
-- parameter object is EXACTLY {tenant_id} (a uuid in its hyphenated form, either case; v_bound
-- holds the uuid VALUE) and its 'read' row names the tenant with no page and detail {}; and the
-- 'operator_audit' read's filter list names 'tenant_vat_checked'. The three reads of that branch
-- hash the SAME {tenant_id} text; the ticket's KIND -- written by this function, checked by each
-- read's consuming UPDATE -- is what tells them apart (TestOpReadTenantVAT_AForgedTicketIsRefused).
-- 🔴 op_begin_read STILL DOES NOT LOOK THE TENANT UP (00029 section 3, the B14 argument for a
-- read): an unknown tenant is answered by phase two -- zero rows -- after the 'read' row naming
-- it has committed.
-- Everything 00027-00033 say of this function still holds, branch for branch; the refusals keep
-- 00027's two messages and codes. The five copies of the audit kind set (this list, the CHECK,
-- op_read_audit's filter shape, internal/db's OperatorAuditKinds and the viewer's words) are held
-- equal by TestOperatorAuditKinds_TheSchemaTheFunctionsAndTheGoListAgree and
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
    IF p_kind IS NULL OR p_kind NOT IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques', 'operator_audit', 'tenant_billing', 'tenant_vat') THEN
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
                                                   'operator_mfa_reset', 'operator_disabled', 'tenant_vat_checked') THEN
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
-- 5. op_read_audit, REPLACED: the new kind and the new scope are on its lists
-- ===========================================================================
-- 00033's body but for three lists: the values an 'operator_audit' read's {"filter": ...} may
-- hold (the new kind -- otherwise a read filtered to it would come back "detail not shown"), the
-- read kinds whose detail is {} (the new read's 'read' row), and the scopes the read returns as
-- themselves. The operator's 'tenant_vat_checked' row needs no new line: its scope is NULL and
-- its detail {} (section 2's CHECK makes both certain), which the list's last line recognises.
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
                                                            'operator_disabled', 'tenant_vat_checked')
                       WHEN p.kind = 'read' THEN
                            p.target_scope IN ('legal_versions', 'tenant_detail', 'tenant_plaques', 'tenant_billing',
                                                'tenant_vat')
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
                                            'tenant_plaques', 'operator_audit', 'tenant_billing',
                                            'tenant_vat')
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
-- 6. op_read_tenant_vat -- one tenant's VAT number and the verdict on it
-- ===========================================================================
-- THE ROW (zero or one), a fixed column list (§2 ii): tenant_id, tenant_name (ADR 0020 §9: the
-- screen names its tenant), vat_number, vat_verified, vat_checked_at -- 00017's four states read
-- from the last two (never asked: both NULL; asked, no answer: a time and no verdict; valid;
-- invalid). The number is returned because the screen sends it to VIES (OP-16 B reads it HERE,
-- on the server, and takes none from the browser -- the orchestrator's K16-1) and binds the
-- write to it. Nothing else of the tenant.
-- AN UNKNOWN TENANT reads ZERO rows -- not an error (00029 section 4.2's argument): the 'read'
-- row naming that id has committed by then.
-- 🔴 THE BELT: `t.id = p_tenant_id` is the only barrier (no RLS runs here).
-- The consuming UPDATE is 00029's tenant_detail one with its kind -- the six conditions of ADR
-- 0021 §2 v 2/4 in the positive spelling (TestOpRead_EveryReadConsumesItsTicketAsTheADRSays).
-- +goose StatementBegin
CREATE FUNCTION public.op_read_tenant_vat(p_session text, p_ticket text, p_tenant_id uuid)
    RETURNS TABLE (tenant_id uuid, tenant_name text, vat_number text, vat_verified boolean,
                   vat_checked_at timestamptz)
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
       AND k.kind = 'tenant_vat'
       AND k.consumed_at IS NULL
       AND k.expires_at > clock_timestamp()
       AND pg_xact_status(k.created_xact) = 'committed';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'op_read_tenant_vat: read refused'
            USING ERRCODE = 'invalid_authorization_specification';
    END IF;

    RETURN QUERY
        SELECT t.id, t.name, t.vat_number, t.vat_verified, t.vat_checked_at
          FROM public.tenants AS t
         WHERE t.id = p_tenant_id;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_read_tenant_vat(text, text, uuid) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_read_tenant_vat(text, text, uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_read_tenant_vat(text, text, uuid) TO tappa_operator;

-- ===========================================================================
-- 7. op_record_vat_check -- the operator records a VIES verdict for one tenant
-- ===========================================================================
-- A ONE-PHASE WRITE (ADR 0021 §2 v 7): the change and both audit rows in one call, RETURNS
-- void. Its order:
--   (a) the session through op_touch_session (28000 for a dead one; the actor is derived, never
--       an argument -- §2 i);
--   (b) the arguments: p_tenant_id, p_vat_number or p_valid NULL is refused (22023, one constant
--       message) before anything is read or written. 🔴 p_valid NULL IS "VIES DID NOT ANSWER":
--       this is where the database itself refuses to record an outage as a verdict (§4.6; the
--       second copy of "no write in an outage" -- the screen's is the first);
--   (b2) 🔴 BOUND TO A COMMITTED READ (round 3, the orchestrator's decision on the third eye's
--       D1): THIS session must have read THIS tenant's VAT number before -- a 'tenant_vat'
--       ticket of the session naming p_tenant_id, written by a transaction that COMMITTED, and
--       its 'read' row in the operator's log (session, scope 'tenant_vat', the tenant). It is
--       op_read_tenant_vat's "created by a COMMITTED transaction" test (pg_xact_status of the
--       ticket's created_xact; the ticket and its 'read' row are one op_begin_read, one
--       transaction, joined by audit_id), so a read opened inside the calling transaction does
--       not count. Else the same 22023 as (b), one constant message, no DETAIL -- and nothing
--       is read from tenants, locked or written (the check names no tenant table). WHY: round
--       2's remaining bit -- is p_vat_number THIS tenant's number -- answered a call rolled
--       back inside a savepoint, so a single transaction could try candidate numbers one after
--       another and find the number with no committed trace; ADR 0021's two-phase read exists
--       so that a rollback cannot erase the trail of looking at tenant data. Bound to a
--       committed read, every such search follows a committed 'read' row of that tenant, whose
--       second phase returns the number itself. THE BOUND IS THE SESSION, not the ticket's
--       lifetime: the ticket may be consumed or expired (OP-16 B's flow asks VIES outside the
--       database between the read and the write, for as long as VIES takes); the session's
--       own idle and absolute limits (op_touch_session) end the binding
--       (TestOpRecordVATCheck_IsBoundToACommittedReadOfTheTenant);
--   (c) the tenant's number and its verdict as it was -- vat_number, vat_verified and
--       vat_checked_at -- read with a row lock (FOR NO KEY UPDATE) by the tenant ALONE
--       (`t.id = p_tenant_id`, the primary key): two operators re-checking at once write one
--       after the other, and each "before" is what the other left. PostgreSQL 17 has no
--       RETURNING OLD (18's), so the old values are held in variables. 🔴 NO KEY UPDATE, NOT
--       UPDATE (the orchestrator's decision): it is the lock the UPDATE in (d) takes anyway -- no
--       key column changes -- so it serialises two re-checks of the tenant and nothing else; a
--       FOR UPDATE lock would conflict with the FOR KEY SHARE every foreign-key check takes, and
--       while the call ran every INSERT of a row that names the tenant -- a tap among them --
--       would wait (TestOpRecordVATCheck_TheRowLockLetsTheTenantsWritesThrough measures both).
--       🔴 BY THE ID ALONE, NOT BY THE NUMBER (the third eye's F1, round 2): see the header --
--       a statement that names p_vat_number may be answered from tenants_vat_number_key, which
--       reaches whichever tenant holds that number;
--   (d) ONLY IF the tenant was found AND the number (c) returned is p_vat_number -- compared in
--       a variable, never in a statement (T3: a verdict about one number never lands on another:
--       if the tenant's owner changed the number between the operator's read and this call,
--       nothing is written to the tenant; and the row lock of (c) keeps the number from changing
--       until this transaction ends) -- the UPDATE, by the tenant: the verdict and ONE
--       wall-clock read;
--   (e) and, in the same IF, THE TENANT'S ROW (K6): action 'tenant.vat_rechecked', actor_id the
--       operator, target the tenant id, `at` that same wall-clock read, and detail
--         {"actor_kind": "operator",
--          "before": {"verified": <bool|null>, "checked_at": <UTC text|null>},
--          "after":  {"verified": <bool>,      "checked_at": <UTC text>}}
--       -- the times as text in UTC with a Z (to_char of the instant AT TIME ZONE 'UTC'), so the
--       detail does not depend on the caller's TimeZone; no number. For a tenant that does not
--       exist, or whose number is not p_vat_number, no row: no foreign key fires (B14). The
--       "before" is the locked row's, so no concurrent change can slip between it and the write;
--   (f) THE OPERATOR'S ROW, on EVERY accepted call, outside the IF: 'tenant_vat_checked', the
--       session, the actor, target_tenant_id = p_tenant_id, detail {} (K16-3; section 2's CHECK).
-- The answer -- void, or 28000/22023 decided by the session, the arguments and the session's
-- committed reads (b2) -- does not depend on what the tenant held, whether the number matched
-- or whether the tenant exists (TestOpRecordVATCheck_TheAnswerIsTheSameWhateverTheTenantsState;
-- the refusal of (b2) moves no counter of tenants for an existing and an absent tenant alike,
-- TestOpRecordVATCheck_IsBoundToACommittedReadOfTheTenant). ⚠️ ONE EXCEPTION, COUNTED (ADR
-- 0021's OP-16 note, LV3): while ANOTHER transaction holds the tenant's row, (c) waits, and
-- what the caller's own limits make of the wait is an answer -- 55P03 under a lock_timeout
-- (measured), 40001 under REPEATABLE READ when the holder changed the row and committed
-- (PostgreSQL's documentation). It waits for an existing tenant whatever the number, never for
-- an id that names no tenant, and never without a committed read of the tenant -- (b2) refuses
-- first (TestOpRecordVATCheck_ALockWaitTellsTheTenantNotTheNumber).
-- A constraint that refuses (c)-(f) is caught (00027's pattern): one fixed 22023, no DETAIL --
-- an uncaught "Failing row contains (...)" would echo the row, the number included -- and a LOG
-- line carrying the constraint's name and the SQLSTATE only. The catch's sub-transaction takes
-- everything (c)-(f) wrote with it.
-- +goose StatementBegin
CREATE FUNCTION public.op_record_vat_check(p_session text, p_tenant_id uuid, p_vat_number text,
                                           p_valid boolean)
    RETURNS void
    LANGUAGE plpgsql
    VOLATILE
    SECURITY DEFINER
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_session        uuid;
    v_admin          uuid;
    v_number         text;
    v_before_valid   boolean;
    v_before_checked timestamptz;
    v_at             timestamptz;
    v_constraint     text;
    v_state          text;
BEGIN
    SELECT t.session_id, t.admin_id
      INTO v_session, v_admin
      FROM public.op_touch_session(p_session) AS t;

    IF p_tenant_id IS NULL OR p_vat_number IS NULL OR p_valid IS NULL THEN
        RAISE EXCEPTION 'op_record_vat_check: check refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    IF NOT EXISTS (SELECT 1
                     FROM public.operator_read_tickets AS k
                     JOIN public.operator_audit_log AS r ON r.id = k.audit_id
                    WHERE k.session_id = v_session
                      AND k.kind = 'tenant_vat'
                      AND k.target_tenant_id = p_tenant_id
                      AND pg_xact_status(k.created_xact) = 'committed'
                      AND r.kind = 'read'
                      AND r.session_id = v_session
                      AND r.target_scope = 'tenant_vat'
                      AND r.target_tenant_id = p_tenant_id) THEN
        RAISE EXCEPTION 'op_record_vat_check: check refused'
            USING ERRCODE = 'invalid_parameter_value';
    END IF;

    BEGIN
        SELECT t.vat_number, t.vat_verified, t.vat_checked_at
          INTO v_number, v_before_valid, v_before_checked
          FROM public.tenants AS t
         WHERE t.id = p_tenant_id
           FOR NO KEY UPDATE;

        IF FOUND AND v_number = p_vat_number THEN
            v_at := clock_timestamp();

            UPDATE public.tenants AS t
               SET vat_verified = p_valid,
                   vat_checked_at = v_at
             WHERE t.id = p_tenant_id;

            INSERT INTO public.audit_log (tenant_id, actor_id, action, target, detail, at)
            VALUES (p_tenant_id, v_admin, 'tenant.vat_rechecked', p_tenant_id::text,
                    jsonb_build_object(
                        'actor_kind', 'operator',
                        'before', jsonb_build_object(
                            'verified', v_before_valid,
                            'checked_at', to_char(v_before_checked AT TIME ZONE 'UTC',
                                                  'YYYY-MM-DD"T"HH24:MI:SS.US"Z"')),
                        'after', jsonb_build_object(
                            'verified', p_valid,
                            'checked_at', to_char(v_at AT TIME ZONE 'UTC',
                                                  'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'))),
                    v_at);
        END IF;

        INSERT INTO public.operator_audit_log (kind, session_id, actor_admin_id, target_tenant_id)
        VALUES ('tenant_vat_checked', v_session, v_admin, p_tenant_id);
    EXCEPTION
        WHEN integrity_constraint_violation THEN
            GET STACKED DIAGNOSTICS v_constraint = CONSTRAINT_NAME,
                                    v_state      = RETURNED_SQLSTATE;
            RAISE LOG 'op_record_vat_check: a write failed constraint "%" (SQLSTATE %); refused',
                v_constraint, v_state;
            RAISE EXCEPTION 'op_record_vat_check: check refused'
                USING ERRCODE = 'invalid_parameter_value';
    END;
END;
$$;
-- +goose StatementEnd
ALTER FUNCTION public.op_record_vat_check(text, uuid, text, boolean) OWNER TO tappa_opdefiner;
REVOKE ALL ON FUNCTION public.op_record_vat_check(text, uuid, text, boolean) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.op_record_vat_check(text, uuid, text, boolean) TO tappa_operator;

-- +goose Down

-- 🔴 WHAT A SUCCESSFUL Down DOES, named (00030-00033's house style):
--   * the two functions go; op_read_audit and op_begin_read go back to 00033's Up bodies
--     VERBATIM -- replaced, not dropped, so their owners and ACLs stay
--     (TestOperator00034_DownGivesBack00033AndUpTakesItAgain compares each body with 00033's
--     file);
--   * tappa_opdefiner loses EXACTLY what section 3 granted, by column -- NOT `REVOKE ALL`, which
--     would take 00029's and 00032's tenants columns with it and break the tenant overview and
--     the billing read; tappa_app's grants are not touched;
--   * operator_audit_log_tenant_write_shape goes; operator_audit_log_kind_check goes back to
--     00033's fourteen kinds and operator_read_tickets_kind_check to 00032's six -- each NOT VALID
--     when a row of a kind its previous set does not name exists (this file's kind or a later
--     migration's, consumed or not), VALIDATED when none does (00030's Down rule: the condition
--     is "outside the previous set", or the Downs do not compose);
--   * the rows STAY: the operator's 'tenant_vat_checked' rows and 'read' rows (operator_audit_log
--     is append-only, 00026), the tenants' 'tenant.vat_rechecked' rows (audit_log is append-only,
--     00005) and the verdicts the operator recorded (vat_verified, vat_checked_at keep their
--     values: a Down takes a door away, not a fact). Nothing here deletes or updates a row.
--   ⚠️ T9, MEASURED IN TestOperator00034_DownGivesBack00033AndUpTakesItAgain: the column REVOKEs
--   on tenants and audit_log change the catalogue only; the test records the lock modes this
--   session holds on the two tables after the Down and that another session's ROW EXCLUSIVE --
--   what an INSERT or an UPDATE takes -- is still granted at once.
DROP FUNCTION IF EXISTS public.op_record_vat_check(text, uuid, text, boolean);
DROP FUNCTION IF EXISTS public.op_read_tenant_vat(text, text, uuid);

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

REVOKE INSERT (tenant_id, actor_id, action, target, detail, at) ON audit_log FROM tappa_opdefiner;
REVOKE UPDATE (vat_verified, vat_checked_at) ON tenants FROM tappa_opdefiner;
REVOKE SELECT (vat_number, vat_verified, vat_checked_at) ON tenants FROM tappa_opdefiner;

ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_tenant_write_shape;
ALTER TABLE operator_audit_log DROP CONSTRAINT operator_audit_log_kind_check;
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
    ELSE
        ALTER TABLE public.operator_audit_log ADD CONSTRAINT operator_audit_log_kind_check
            CHECK (kind IN ('login_failed', 'unknown_email', 'totp_failed', 'locked',
                            'enrollment_failed', 'password_ok', 'login', 'enrollment',
                            'logout', 'read', 'legal_publish', 'operator_created',
                            'operator_mfa_reset', 'operator_disabled'));
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE operator_read_tickets DROP CONSTRAINT operator_read_tickets_kind_check;
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.operator_read_tickets
                WHERE kind <> ALL (ARRAY['legal_versions', 'tenants', 'tenant_detail',
                                         'tenant_plaques', 'operator_audit', 'tenant_billing'])) THEN
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit', 'tenant_billing'))
            NOT VALID;
    ELSE
        ALTER TABLE public.operator_read_tickets ADD CONSTRAINT operator_read_tickets_kind_check
            CHECK (kind IN ('legal_versions', 'tenants', 'tenant_detail', 'tenant_plaques',
                            'operator_audit', 'tenant_billing'));
    END IF;
END
$$;
-- +goose StatementEnd
