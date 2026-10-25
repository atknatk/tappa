-- 00068 -- M15-15 (ADR 0032 §C.2, §C.3, §C.5, "Geri alma"): an employee's claim of the missing
-- half of a punch, and the manager's decision on it.
--
-- WHAT. Two append-only tables, both tenant-scoped with CLAUDE.md §6's five and the grant:
--   punch_claims            the employee's statement "I left at 17:00" (kind 'out', the anchor an
--                           open 'in') or "I came in at 09:00" (kind 'in', the anchor an orphan
--                           'out'). anchor_transaction_id is the transactions row whose missing
--                           half is claimed; claimed_at the moment the employee gave; prefilled_at
--                           the REAL end of the anchor's resolved shift the form offered, only when
--                           that end lay inside the window (NULL otherwise -- no shift, or an end
--                           outside it; ADR 0032 §C.2: there is no prefill clipped to the window).
--                           "Default not changed" is a computation, not a column:
--                           prefilled_at IS NOT NULL AND claimed_at = prefilled_at.
--   punch_claim_decisions   the manager's answer: approved (with the manual row the approval wrote
--                           and its moment -- approved_at differs from claimed_at after an Edit)
--                           or rejected (neither). A decision is a row of its own rather than a
--                           status UPDATE on the claim: ADR 0009's pattern, §4.3's treatment.
-- A claim is not a record (ADR 0032 §C.1): it writes no transactions row and moves no hour. Only
-- an approval does, through the manual row it names -- which transactions' own rules govern.
--
-- WHAT THE SCHEMA HOLDS AND WHAT IT LEAVES TO THE DOMAIN (M15-16). The schema holds what is TRUE
-- OF THE ROWS and cannot change: transactions is immutable (§4.3), so a fact about the anchor row
-- checked at the claim's INSERT stays true for ever, and one check is enough (00005's argument for
-- tappa_check_transaction_review). It does NOT hold what depends on the CHAIN, which grows: whether
-- the anchor is still open / still an orphan (ADR 0032 §A.6's classification, the reader's class --
-- M15-10's lens), the window's other ends (next / prev event, Next(S), now, StaleOpenIn's 18 h), the
-- shift band, MaxBackdate. Those are M15-16's, under the person's advisory lock, and are checked
-- again at the decision; a schema rule over them would freeze a moving answer into a row.
--
-- 🔴 THE ANCHOR IS THE SAME TENANT'S AND THE SAME PERSON'S ROW. ADR 0032 §C.3: the client's anchor
-- id is not trusted, the INSERT is built from the anchor row with the tenant_id + employee_id
-- predicate (db/queries/punchclaims.sql, InsertPunchClaim -- a foreign anchor is 0 rows, not an
-- error). Under it, in the schema:
--   * same tenant: the composite FK (anchor_transaction_id, tenant_id) -> transactions (id,
--     tenant_id) -- 00005's transactions_id_tenant_key; a claim cannot point across tenants even as
--     the migration role, which bypasses RLS (an FK does too -- which is why it is the FK and not
--     the policy that says this);
--   * same person, a directed anchor, the missing half, a business day, not a practice row, the
--     claimed (and prefilled) moment on the anchor's open side: tappa_check_punch_claim, a BEFORE
--     INSERT trigger -- a CHECK cannot read another table. An FK on (anchor, tenant_id,
--     employee_id) would say "same person" too, but needs a UNIQUE on transactions' three columns:
--     an index build on the table every tap writes, under a lock that stops the taps, for a fact
--     one indexed lookup on transactions' primary key proves.
-- The decision the same way: the claim is the same tenant's (composite FK to punch_claims (id,
-- tenant_id)); the approving manual row is the same tenant's (composite FK to transactions); the
-- decider is a same-tenant admin (composite FK to admin_users (id, tenant_id) -- billing_periods'
-- closed_by precedent, 00016); and tappa_check_punch_claim_decision holds the approving row to the
-- claim: the manual channel, the claim's person, the claimed direction, the approved moment, the
-- decider as entered_by.
--
-- THE TRIGGERS DEFER, NEVER WAVE THROUGH, WHEN THEY CANNOT SEE THE ROW. Both are SECURITY INVOKER
-- (00005's reasoning: the body runs as tappa_app, subject to RLS -- a DEFINER would be privilege
-- surface for nothing) and read with an explicit tenant predicate. A row they cannot see is left
-- to the FK and the WITH CHECK, which refuse it: for tappa_app in a context the anchor of the
-- context's own tenant is always visible (transactions' policy is the tenant equality alone), so
-- "invisible" means the row's tenant_id is not the context's -- 42501 from WITH CHECK -- or the row
-- does not exist -- 23503 from the FK. Raising there instead would make the WITH CHECK
-- unreachable from tappa_app and so untestable (internal/db/punchclaims_test.go forges across
-- tenants and reads 42501). The migration role bypasses RLS and sees every row, so the checks bind
-- it as well.
--
-- APPEND-ONLY (CLAUDE.md §4.3's treatment, 00005 + 00021, ADR 0032 §C.2): no UPDATE, DELETE or
-- TRUNCATE for tappa_app (REVOKE ALL; the grants below are SELECT and INSERT alone), and for every
-- role -- the migration role included -- the row trigger tappa_forbid_mutation on UPDATE OR DELETE
-- and the statement trigger on TRUNCATE (00021:96-116; a TRUNCATE trigger fires for a table reached
-- by CASCADE too, so `TRUNCATE transactions CASCADE`, which now reaches both, is refused by
-- transactions' own guard first). A rejected claim stays rejected; a claim is never withdrawn; a
-- second claim on the same anchor is a unique violation (23505 -- M15-17 maps it to "Sent").
--
-- PRIVILEGES -- ADR 0028 §1's `arwd` rule: production's default hands a new table all four verbs
-- (scripts/db-init/03-dev-production-default-acl.sql reproduces it), so ALL is revoked first and
-- the verbs and columns tappa_app needs are granted by name. SELECT on every column (the queue and
-- the result card read them); INSERT on the columns a writer names -- id and created_at take their
-- defaults, so created_at is the database's clock, never the client's. No other role is granted
-- anything: the holding and feed definers, the resolver and the platform's roles never read a
-- claim (the postcondition below asserts it for every non-superuser role but the owner), and no
-- function of the platform's names these tables.
--
-- LOCKS, MEASURED (PostgreSQL 17, pg_locks inside the Up's transaction): the foreign keys take
-- SHARE ROW EXCLUSIVE on the four tables they reference -- tenants, employees, transactions and
-- admin_users -- until the commit. transactions' conflicts with every tap's INSERT (ROW EXCLUSIVE),
-- admin_users' with a panel login's UPDATE (MarkAdminLoggedIn). So the FIRST STATEMENT is lock_timeout
-- (deploy/README.md's rule; 00064, 00065): behind a long writer the file gives up with 55P03 and
-- changes nothing, instead of queueing every later tap behind its wait. Once the locks are taken
-- the work is catalogue-only (two empty tables).

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 1. punch_claims
-- ---------------------------------------------------------------------------
CREATE TABLE punch_claims (
    id                    uuid        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id             uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    employee_id           uuid        NOT NULL,
    anchor_transaction_id uuid        NOT NULL,
    kind                  text        NOT NULL,
    claimed_at            timestamptz NOT NULL,
    prefilled_at          timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    -- the missing half: 'out' for an open 'in', 'in' for an orphan 'out'
    CONSTRAINT punch_claims_kind_is_a_direction CHECK (kind IN ('in', 'out')),
    -- ADR 0032 §C.2: one claim per anchor, ever (a rejection is final -- §C.5)
    CONSTRAINT punch_claims_one_per_anchor UNIQUE (tenant_id, anchor_transaction_id),
    -- the decisions' composite FK target
    CONSTRAINT punch_claims_id_tenant_key UNIQUE (id, tenant_id),
    CONSTRAINT punch_claims_employee_fk FOREIGN KEY (employee_id, tenant_id)
        REFERENCES employees (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT punch_claims_anchor_fk FOREIGN KEY (anchor_transaction_id, tenant_id)
        REFERENCES transactions (id, tenant_id) ON DELETE RESTRICT
);

-- The queue's read: a tenant's claims, newest first (M15-18 joins the decisions to it).
CREATE INDEX punch_claims_tenant_created_idx ON punch_claims (tenant_id, created_at DESC);

ALTER TABLE punch_claims ENABLE ROW LEVEL SECURITY;
ALTER TABLE punch_claims FORCE ROW LEVEL SECURITY;
CREATE POLICY punch_claims_tenant_isolation ON punch_claims
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TRIGGER punch_claims_no_mutation
    BEFORE UPDATE OR DELETE ON punch_claims
    FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation();
CREATE TRIGGER punch_claims_no_truncate
    BEFORE TRUNCATE ON punch_claims
    FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation();

-- The anchor's immutable facts (the header says why these and not the chain's). Every refusal is
-- 23514 and names no value: a claim is logged by opaque ids only (ADR 0032 §C.3), and the caller
-- has the ids.
-- +goose StatementBegin
CREATE FUNCTION public.tappa_check_punch_claim()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_employee uuid;
    v_type     text;
    v_day      timestamptz;
    v_at       timestamptz;
    v_practice boolean;
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'INSERT'
       OR TG_RELID <> 'public.punch_claims'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'tappa_check_punch_claim: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    SELECT a.employee_id, a.type, a.business_day_start, a.occurred_at, a.practice
      INTO v_employee, v_type, v_day, v_at, v_practice
      FROM public.transactions AS a
     WHERE a.id = NEW.anchor_transaction_id
       AND a.tenant_id = NEW.tenant_id;
    IF NOT FOUND THEN
        -- another tenant's row (WITH CHECK refuses it) or no row (the FK does) -- the header
        RETURN NEW;
    END IF;

    IF v_employee IS DISTINCT FROM NEW.employee_id THEN
        RAISE EXCEPTION 'punch claim: the anchor is another person''s record'
            USING ERRCODE = 'check_violation';
    END IF;
    IF v_type IS NULL THEN
        RAISE EXCEPTION 'punch claim: the anchor has no direction (a refused or ignored tap has no missing half)'
            USING ERRCODE = 'check_violation';
    END IF;
    IF v_type = NEW.kind THEN
        RAISE EXCEPTION 'punch claim: a claim names the missing half, the direction opposite the anchor''s'
            USING ERRCODE = 'check_violation';
    END IF;
    IF v_day IS NULL THEN
        -- ADR 0032 §C.1 (Ü-Y2): a record written before the business day is never an anchor
        RAISE EXCEPTION 'punch claim: the anchor has no business day'
            USING ERRCODE = 'check_violation';
    END IF;
    IF v_practice THEN
        -- ADR 0008: a practice row stays out of the direction chain
        RAISE EXCEPTION 'punch claim: the anchor is a practice tap'
            USING ERRCODE = 'check_violation';
    END IF;
    -- The window's one end the anchor fixes (ADR 0032 §C.3): an out after the open in, an in before
    -- the orphan out, both strictly. The other ends move with the chain and are M15-16's.
    IF (NEW.kind = 'out' AND NOT NEW.claimed_at > v_at)
       OR (NEW.kind = 'in' AND NOT NEW.claimed_at < v_at) THEN
        RAISE EXCEPTION 'punch claim: the claimed moment is on the wrong side of the anchor'
            USING ERRCODE = 'check_violation';
    END IF;
    IF NEW.prefilled_at IS NOT NULL
       AND ((NEW.kind = 'out' AND NOT NEW.prefilled_at > v_at)
            OR (NEW.kind = 'in' AND NOT NEW.prefilled_at < v_at)) THEN
        RAISE EXCEPTION 'punch claim: the prefilled moment is on the wrong side of the anchor'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.tappa_check_punch_claim() FROM PUBLIC;

CREATE TRIGGER punch_claims_validate
    BEFORE INSERT ON punch_claims
    FOR EACH ROW EXECUTE FUNCTION public.tappa_check_punch_claim();

-- ---------------------------------------------------------------------------
-- 2. punch_claim_decisions
-- ---------------------------------------------------------------------------
-- decided_by and the manual row's entered_by are the same admin_users id (ADR 0032 §C.4: entered_by
-- = the approver). transaction_id is the manual row the approval wrote: one row answers one claim.
CREATE TABLE punch_claim_decisions (
    id             uuid        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    tenant_id      uuid        NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    claim_id       uuid        NOT NULL,
    outcome        text        NOT NULL,
    decided_by     uuid        NOT NULL,
    approved_at    timestamptz,
    transaction_id uuid,
    note           text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT punch_claim_decisions_outcome_is_known CHECK (outcome IN ('approved', 'rejected')),
    -- an approval names its record and its moment; a rejection names neither
    CONSTRAINT punch_claim_decisions_shape_by_outcome CHECK (
        (outcome = 'approved' AND transaction_id IS NOT NULL AND approved_at IS NOT NULL)
        OR (outcome = 'rejected' AND transaction_id IS NULL AND approved_at IS NULL)),
    -- ADR 0032 §C.2 "claim_id (UNIQUE)": claim_id alone decides its tenant (the FK below), so this
    -- is that uniqueness, written tenant-first to serve as the table's tenant index
    CONSTRAINT punch_claim_decisions_one_per_claim UNIQUE (tenant_id, claim_id),
    -- one manual row answers at most one claim (NULLs -- the rejections -- are distinct)
    CONSTRAINT punch_claim_decisions_one_per_record UNIQUE (tenant_id, transaction_id),
    CONSTRAINT punch_claim_decisions_claim_fk FOREIGN KEY (claim_id, tenant_id)
        REFERENCES punch_claims (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT punch_claim_decisions_transaction_fk FOREIGN KEY (transaction_id, tenant_id)
        REFERENCES transactions (id, tenant_id) ON DELETE RESTRICT,
    CONSTRAINT punch_claim_decisions_decided_by_fk FOREIGN KEY (decided_by, tenant_id)
        REFERENCES admin_users (id, tenant_id) ON DELETE RESTRICT
);

ALTER TABLE punch_claim_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE punch_claim_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY punch_claim_decisions_tenant_isolation ON punch_claim_decisions
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TRIGGER punch_claim_decisions_no_mutation
    BEFORE UPDATE OR DELETE ON punch_claim_decisions
    FOR EACH ROW EXECUTE FUNCTION tappa_forbid_mutation();
CREATE TRIGGER punch_claim_decisions_no_truncate
    BEFORE TRUNCATE ON punch_claim_decisions
    FOR EACH STATEMENT EXECUTE FUNCTION tappa_forbid_mutation();

-- The approving row is the claimed half (the header). The self-decision clause is 00005:354's
-- comparison and carries the same honest limit (ADR 0032 §C.4, G-O1): decided_by is an admin_users
-- id and employee_id an employees id -- two id spaces, so today it never fires; it is no guarantee
-- that one person cannot approve their own claim (that waits for M14's identity, ADR 0031).
-- +goose StatementBegin
CREATE FUNCTION public.tappa_check_punch_claim_decision()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
DECLARE
    v_employee uuid;
    v_kind     text;
    t_employee uuid;
    t_type     text;
    t_channel  text;
    t_at       timestamptz;
    t_by       uuid;
BEGIN
    IF TG_WHEN <> 'BEFORE' OR TG_LEVEL <> 'ROW' OR TG_OP <> 'INSERT'
       OR TG_RELID <> 'public.punch_claim_decisions'::pg_catalog.regclass THEN
        RAISE EXCEPTION 'tappa_check_punch_claim_decision: bound to a table or an event it is not for'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;

    SELECT c.employee_id, c.kind
      INTO v_employee, v_kind
      FROM public.punch_claims AS c
     WHERE c.id = NEW.claim_id
       AND c.tenant_id = NEW.tenant_id;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;

    IF NEW.decided_by = v_employee THEN
        RAISE EXCEPTION 'punch claim decision: the decider is the claimant'
            USING ERRCODE = 'check_violation';
    END IF;

    IF NEW.transaction_id IS NULL THEN
        -- a rejection; the shape CHECK holds the rest
        RETURN NEW;
    END IF;

    SELECT t.employee_id, t.type, t.channel, t.occurred_at, t.entered_by
      INTO t_employee, t_type, t_channel, t_at, t_by
      FROM public.transactions AS t
     WHERE t.id = NEW.transaction_id
       AND t.tenant_id = NEW.tenant_id;
    IF NOT FOUND THEN
        RETURN NEW;
    END IF;

    IF t_channel <> 'manual' THEN
        RAISE EXCEPTION 'punch claim decision: an approval names the manual record it wrote'
            USING ERRCODE = 'check_violation';
    END IF;
    IF t_employee IS DISTINCT FROM v_employee THEN
        RAISE EXCEPTION 'punch claim decision: the approving record is another person''s'
            USING ERRCODE = 'check_violation';
    END IF;
    IF t_type IS DISTINCT FROM v_kind THEN
        RAISE EXCEPTION 'punch claim decision: the approving record is not the claimed direction'
            USING ERRCODE = 'check_violation';
    END IF;
    IF t_at IS DISTINCT FROM NEW.approved_at THEN
        RAISE EXCEPTION 'punch claim decision: the approving record is not at the approved moment'
            USING ERRCODE = 'check_violation';
    END IF;
    IF t_by IS DISTINCT FROM NEW.decided_by THEN
        RAISE EXCEPTION 'punch claim decision: the approving record was entered by somebody else'
            USING ERRCODE = 'check_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.tappa_check_punch_claim_decision() FROM PUBLIC;

CREATE TRIGGER punch_claim_decisions_validate
    BEFORE INSERT ON punch_claim_decisions
    FOR EACH ROW EXECUTE FUNCTION public.tappa_check_punch_claim_decision();

-- ---------------------------------------------------------------------------
-- 3. tappa_app -- reads both, inserts both, changes neither
-- ---------------------------------------------------------------------------
REVOKE ALL ON punch_claims FROM tappa_app;
REVOKE ALL ON punch_claim_decisions FROM tappa_app;
GRANT SELECT (id, tenant_id, employee_id, anchor_transaction_id, kind, claimed_at, prefilled_at, created_at)
    ON punch_claims TO tappa_app;
GRANT INSERT (tenant_id, employee_id, anchor_transaction_id, kind, claimed_at, prefilled_at)
    ON punch_claims TO tappa_app;
GRANT SELECT (id, tenant_id, claim_id, outcome, decided_by, approved_at, transaction_id, note, created_at)
    ON punch_claim_decisions TO tappa_app;
GRANT INSERT (tenant_id, claim_id, outcome, decided_by, approved_at, transaction_id, note)
    ON punch_claim_decisions TO tappa_app;

-- ---------------------------------------------------------------------------
-- 4. POSTCONDITION -- measured as each role would meet it (has_* count membership and PUBLIC).
-- ---------------------------------------------------------------------------
-- +goose StatementBegin
DO $$
DECLARE
    r text;
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['public.punch_claims', 'public.punch_claim_decisions'] LOOP
        IF NOT pg_catalog.has_any_column_privilege('tappa_app', t, 'SELECT')
           OR NOT pg_catalog.has_any_column_privilege('tappa_app', t, 'INSERT')
           OR pg_catalog.has_any_column_privilege('tappa_app', t, 'UPDATE')
           OR pg_catalog.has_table_privilege('tappa_app', t, 'DELETE')
           OR pg_catalog.has_table_privilege('tappa_app', t, 'TRUNCATE')
           OR pg_catalog.has_column_privilege('tappa_app', t, 'created_at', 'INSERT') THEN
            RAISE EXCEPTION 'migration 00068 (M15-15): tappa_app must read and insert % and nothing more -- through PUBLIC or a role membership it does not. Fix the grants by hand, then migrate again. Nothing was changed.', t
                USING ERRCODE = 'object_not_in_prerequisite_state';
        END IF;
        -- every other role but the owner and the superusers, read from the catalogue rather than
        -- named: a role added later is held to the same line
        FOR r IN SELECT o.rolname FROM pg_catalog.pg_roles AS o
                  WHERE o.rolname <> 'tappa_app' AND o.rolname !~ '^pg_' AND NOT o.rolsuper
                    AND o.oid <> (SELECT c.relowner FROM pg_catalog.pg_class AS c WHERE c.oid = t::pg_catalog.regclass) LOOP
            IF pg_catalog.has_any_column_privilege(r, t, 'SELECT, INSERT, UPDATE, REFERENCES')
               OR pg_catalog.has_table_privilege(r, t, 'DELETE, TRUNCATE, TRIGGER') THEN
                RAISE EXCEPTION 'migration 00068 (M15-15): % reaches %, which no role but tappa_app may. Fix the grants by hand, then migrate again. Nothing was changed.', r, t
                    USING ERRCODE = 'object_not_in_prerequisite_state';
            END IF;
        END LOOP;
    END LOOP;
    IF pg_catalog.has_function_privilege('tappa_app', 'public.tappa_check_punch_claim()', 'EXECUTE')
       OR pg_catalog.has_function_privilege('tappa_app', 'public.tappa_check_punch_claim_decision()', 'EXECUTE') THEN
        RAISE EXCEPTION 'migration 00068 (M15-15): tappa_app may EXECUTE a claim trigger function (ADR 0028 §1: EXECUTE in the owner alone). Fix the grants by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
-- 🔴 A DOWN THAT WOULD ERASE A CLAIM OR A DECISION REFUSES (CLAUDE.md §4.3, §4.6; ADR 0032 "Geri
-- alma"; the 00043, 00046 and 00064 precedent). A claim is the employee's statement and a decision
-- the manager's act; neither can be rebuilt from any other row -- an approval's manual row says
-- what was written, not that a claim asked for it or who refused one. So while either table holds
-- a row this Down stops and changes nothing; on a database with none it takes the tables and the
-- two functions away. After the first claim the way back is the binary, not the schema: the old
-- code neither writes nor reads these tables.
--
-- The two tables are LOCKED BEFORE THEY ARE LOOKED AT: between an unlocked EXISTS and the DROP a
-- claim could commit, and the DROP would then erase it. A claim's INSERT holds ROW EXCLUSIVE on its
-- table until it commits, so the LOCK waits for it and the EXISTS then sees it. The DROPs go on to
-- take ACCESS EXCLUSIVE on the referenced tables (the foreign keys' triggers there; measured:
-- transactions, employees, admin_users and tenants) under the same lock_timeout: behind a tap the
-- Down gives up with 55P03 and changes nothing.
SET LOCAL lock_timeout = '1s';

LOCK TABLE punch_claims, punch_claim_decisions IN ACCESS EXCLUSIVE MODE;

-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.punch_claims) OR EXISTS (SELECT 1 FROM public.punch_claim_decisions) THEN
        RAISE EXCEPTION 'migration 00068 Down: punch_claims or punch_claim_decisions holds rows (an employee''s claim of a missing half, a manager''s decision on it); dropping them would erase them, and they are append-only. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP TABLE punch_claim_decisions;
DROP TABLE punch_claims;
DROP FUNCTION public.tappa_check_punch_claim_decision();
DROP FUNCTION public.tappa_check_punch_claim();
