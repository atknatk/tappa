-- 00037 -- M11 API-1, the single-tenant half of the integration schema: external
-- identities on employees and locations, the directory clock, the invitation queue.
--
-- NORMATIVE SOURCES: docs/adr/0028-dis-api-anahtar-akis-webhook.md §1 (the `arwd` rule),
-- §9 (K-14, K-17, K-18, K-20, K-21, K-24) and docs/plan/m11-entegrasyon-api.md §4.0 and §5
-- (API-1). The tenants' API gate (K-22) is migration 00038's.
--
-- WHAT IT DOES:
--   1. employees gains external_ref, employee_code, first_name, last_name and updated_at;
--      location_id becomes NULLABLE for API-managed rows only (K-14: a row without a home
--      venue must carry an external_ref -- the integrator's identity is then its anchor);
--   2. the e-mail uniqueness stops counting deactivated rows (K-24) and KEEPS ITS NAME,
--      employees_tenant_email_key: internal/domain/tenant recognises the 23505 by that name
--      and turns it into a sentence (409 on the panel), never a 500;
--   3. one live employee per external_ref in a tenant; a deactivated row keeps its ref, so a
--      re-hire is a NEW row under the SAME ref (ADR 0010, plan §2 item 9);
--   4. updated_at belongs to a trigger (K-21): it moves on INSERT and whenever a column the
--      directory shows changes -- the two names, full_name, employee_code, external_ref,
--      status -- and on no other UPDATE. Every writer (the four UPDATE statements in
--      db/queries today, the panel's external-ref write, API-6's writes) is covered without
--      remembering to; a writer that names updated_at itself is overruled;
--   5. locations gains external_ref (unique in the tenant) and active (K-20: a LABEL the
--      integrator sets; nothing in the decision engine reads it);
--   6. invite_requests is born (K-17's queue; its worker is API-6's): the five columns ADR
--      0028 §9 names and nothing else -- in particular NO code and no hash of one: a code is
--      minted at send time, inside the worker's transaction, and never waits in a table.
--
-- 🔴 THE `arwd` RULE (ADR 0028 §1, CLAUDE.md §6) -- the new table is born in production with
-- tappa_app=arwd (pg_default_acl) and in a fresh CI database with `ar`; so it REVOKEs ALL and
-- states every verb and column it grants. UPDATE is a column list. The trigger function is
-- revoked from PUBLIC: a trigger function runs at fire time without an EXECUTE check, so
-- nobody needs to call it, and its body asserts the table it is bound to (TG_RELID).
-- internal/db's TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd holds the whole
-- result against a database migrated under production's default.
--
-- The two existing tables keep their grants: employees and locations hand tappa_app
-- table-level SELECT/INSERT/UPDATE (00003, 00002), which covers a column added later, and the
-- product's own statements write these columns (the panel's external-ref field; API-5/6).
--
-- NOTHING IS BACKFILLED. Existing rows get NULL in the new text columns, true in
-- locations.active and this migration's instant in employees.updated_at (a constant default:
-- no table rewrite); every one of them still has its home venue, so K-14's CHECK validates.
--
-- 🔴 lock_timeout 1 s, THE FIRST STATEMENT of the Up and of the Down (00054's rule,
-- deploy/README.md; the API-13 security review): the ALTERs take ACCESS EXCLUSIVE on employees
-- and locations. Behind a long reader -- the 02:30 backup's dump, an open panel transaction --
-- the file gives up with 55P03 (the migrate Job fails, nothing rolls out, it is run again at a
-- quiet moment) instead of queueing every later read and tap of those tables behind its lock.
-- The setting is transaction-local: it changes no schema object (the catalogue is the same).

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- ---------------------------------------------------------------------------
-- 1. employees: the integrator's identity and the directory's columns.
-- ---------------------------------------------------------------------------
-- external_ref is the integrator's key and ends up in a URL path (/v1/employees/{ref}), so
-- its alphabet is RFC 3986's unreserved set and it starts with a letter or a digit ("." and
-- ".." are path segments, not identities). employee_code and the two names are labels: no
-- control characters, no surrounding spaces, bounded (the roster's measured lesson -- an
-- unbounded name on a page boundary hid everybody behind it; 00003 could not bound
-- full_name after the fact, these columns are bounded from birth).
ALTER TABLE employees ADD COLUMN external_ref text
    CONSTRAINT employees_external_ref_shape
    CHECK (external_ref ~ '^[A-Za-z0-9][A-Za-z0-9._~-]{0,63}$');
ALTER TABLE employees ADD COLUMN employee_code text
    CONSTRAINT employees_employee_code_shape
    CHECK (char_length(employee_code) BETWEEN 1 AND 64
           AND employee_code = btrim(employee_code)
           AND employee_code !~ '[[:cntrl:]]');
ALTER TABLE employees ADD COLUMN first_name text
    CONSTRAINT employees_first_name_shape
    CHECK (char_length(first_name) BETWEEN 1 AND 120
           AND first_name = btrim(first_name)
           AND first_name !~ '[[:cntrl:]]');
ALTER TABLE employees ADD COLUMN last_name text
    CONSTRAINT employees_last_name_shape
    CHECK (char_length(last_name) BETWEEN 1 AND 120
           AND last_name = btrim(last_name)
           AND last_name !~ '[[:cntrl:]]');
-- A constant default (now() is STABLE): existing rows take this migration's instant without a
-- rewrite. New rows never keep it -- the trigger below writes the wall clock on INSERT.
ALTER TABLE employees ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- K-14: the home venue is optional ONLY for a row the integrator owns. The composite FK
-- employees_location_fk (00003) is MATCH SIMPLE, so a NULL location_id is simply not checked;
-- a non-NULL one is still bound to a venue of the same tenant.
ALTER TABLE employees ALTER COLUMN location_id DROP NOT NULL;
ALTER TABLE employees ADD CONSTRAINT employees_home_or_external_ref
    CHECK (location_id IS NOT NULL OR external_ref IS NOT NULL);

-- ---------------------------------------------------------------------------
-- 2. K-24: the address is unique among the LIVE rows of a tenant, under the SAME name.
-- ---------------------------------------------------------------------------
-- A deactivated row is a closed employment (ADR 0010: one-way); it must not stop the same
-- person from being hired again with the same address. Two live rows still cannot share it.
-- Nothing moves a row OUT of 'deactivated' (no statement in db/queries does, ADR 0010), so a
-- row cannot re-enter the index and collide.
-- 🔴 THE NAME IS LOAD-BEARING: internal/domain/tenant.isEmailTaken compares the violation's
-- CONSTRAINT with "employees_tenant_email_key" (a partial unique index reports its own name
-- there), and a 23505 under any other name is a 500.
DROP INDEX employees_tenant_email_key;
CREATE UNIQUE INDEX employees_tenant_email_key
    ON employees (tenant_id, email)
    WHERE email IS NOT NULL AND status <> 'deactivated';

-- ---------------------------------------------------------------------------
-- 3. K-24: one live employee per external_ref in a tenant.
-- ---------------------------------------------------------------------------
-- NULL refs (every panel-made row) never collide. A deactivated row keeps its ref and leaves
-- the index, so a re-hire opens a new row under the same ref.
CREATE UNIQUE INDEX employees_tenant_external_ref_key
    ON employees (tenant_id, external_ref)
    WHERE status <> 'deactivated';

-- ---------------------------------------------------------------------------
-- 4. K-21: updated_at is the directory's clock, and only a trigger writes it.
-- ---------------------------------------------------------------------------
-- clock_timestamp(), not now(): the directory answers `updated_at >= updatedSince - 10 min`,
-- and a transaction-start instant would date a change by when its transaction OPENED.
-- The directory-visible set is the one ADR 0028 §9 lists (names, code, external_ref, status)
-- plus full_name: a panel-made row has no first/last name, and full_name is the name the
-- directory can show for it, so renaming it must reach the integrator.
-- ⚠️ ROW-VALUE IS DISTINCT FROM compares column by column with NULLs as values, so NULL ->
-- 'x' and 'x' -> NULL both count as a change.
-- +goose StatementBegin
CREATE FUNCTION public.tappa_employees_touch_updated_at()
    RETURNS trigger
    LANGUAGE plpgsql
    SET search_path = pg_catalog, pg_temp
AS $$
BEGIN
    IF TG_RELID IS DISTINCT FROM 'public.employees'::regclass
       OR TG_WHEN IS DISTINCT FROM 'BEFORE' OR TG_LEVEL IS DISTINCT FROM 'ROW' THEN
        RAISE EXCEPTION 'tappa_employees_touch_updated_at: bound to the wrong table or event'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF TG_OP = 'INSERT' THEN
        NEW.updated_at := clock_timestamp();
    ELSIF (NEW.full_name, NEW.first_name, NEW.last_name, NEW.employee_code,
           NEW.external_ref, NEW.status)
          IS DISTINCT FROM
          (OLD.full_name, OLD.first_name, OLD.last_name, OLD.employee_code,
           OLD.external_ref, OLD.status) THEN
        NEW.updated_at := clock_timestamp();
    ELSE
        NEW.updated_at := OLD.updated_at;
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION public.tappa_employees_touch_updated_at() FROM PUBLIC;

CREATE TRIGGER employees_directory_clock
    BEFORE INSERT OR UPDATE ON employees
    FOR EACH ROW EXECUTE FUNCTION public.tappa_employees_touch_updated_at();

-- ---------------------------------------------------------------------------
-- 5. locations: the integrator's identity and K-20's label.
-- ---------------------------------------------------------------------------
-- K-18: a location is bound to its ref by the OWNER in the panel ("External ref"); the API
-- never matches one by name. Unique in the tenant, NULLs free.
ALTER TABLE locations ADD COLUMN external_ref text
    CONSTRAINT locations_external_ref_shape
    CHECK (external_ref ~ '^[A-Za-z0-9][A-Za-z0-9._~-]{0,63}$');
ALTER TABLE locations ADD CONSTRAINT locations_tenant_external_ref_key
    UNIQUE (tenant_id, external_ref);
-- K-20: a label only. The decision engine, the policies and the tap path never read it.
ALTER TABLE locations ADD COLUMN active boolean NOT NULL DEFAULT true;

-- ---------------------------------------------------------------------------
-- 6. invite_requests: K-17's queue (the worker is API-6's).
-- ---------------------------------------------------------------------------
-- One live request per employee: the primary key is (tenant_id, employee_id), so a second
-- request for somebody already queued is an ON CONFLICT no-op (plan §2 item 12) and the
-- tenant-leading key is also CLAUDE.md §6's tenant index. A request leaves the queue when the
-- worker has minted and sent the code (DELETE): there is no "sent" state to keep, and the
-- invitation itself is employee_invites' row.
-- requested_at is the queue's order and the DEFAULT writes it; tappa_app cannot.
-- lease_until is the 60-second lease of K-17 (two pods never mint the same invitation);
-- attempts counts tries for the back-off.
CREATE TABLE invite_requests (
    tenant_id    uuid NOT NULL REFERENCES tenants (id) ON DELETE RESTRICT,
    employee_id  uuid NOT NULL,
    requested_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    lease_until  timestamptz,
    attempts     integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    CONSTRAINT invite_requests_pkey PRIMARY KEY (tenant_id, employee_id),
    CONSTRAINT invite_requests_employee_fk
        FOREIGN KEY (employee_id, tenant_id)
        REFERENCES employees (id, tenant_id) ON DELETE RESTRICT
);

ALTER TABLE invite_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE invite_requests FORCE ROW LEVEL SECURITY;

CREATE POLICY invite_requests_tenant_isolation ON invite_requests
    USING      (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
    WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- The whole of tappa_app's reach, stated (ADR 0028 §1):
--   SELECT  every column (the worker reads the queue; resend reads it to stay a no-op);
--   INSERT  (tenant_id, employee_id) -- requested_at, lease_until and attempts start at their
--           defaults, so nobody queues an employee ahead of the line or pre-leased;
--   UPDATE  (lease_until, attempts) -- the lease and the try count, nothing else: a request
--           cannot be moved to another employee or tenant, or re-dated;
--   DELETE  the request leaves the queue once sent.
REVOKE ALL ON invite_requests FROM tappa_app;
GRANT SELECT ON invite_requests TO tappa_app;
GRANT INSERT (tenant_id, employee_id) ON invite_requests TO tappa_app;
GRANT UPDATE (lease_until, attempts) ON invite_requests TO tappa_app;
GRANT DELETE ON invite_requests TO tappa_app;

-- +goose Down

-- 🔴 A DOWN THAT WOULD LOSE OR REWRITE A ROW REFUSES INSTEAD (CLAUDE.md §4.6): an employee
-- without a home venue cannot be given one by a migration, and two rows that share an address
-- under the narrowed index (a deactivated one and a live one) cannot both survive the old,
-- wider one. Either state is named and nothing is changed; the queue's rows are transient
-- requests (their employees and invitations are untouched) and go with the table.
SET LOCAL lock_timeout = '1s';
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM public.employees WHERE location_id IS NULL) THEN
        RAISE EXCEPTION 'migration 00037 Down: an employee has no home venue (an API-managed row); location_id cannot become NOT NULL again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
    IF EXISTS (SELECT 1 FROM public.employees
                WHERE email IS NOT NULL
                GROUP BY tenant_id, email HAVING count(*) > 1) THEN
        RAISE EXCEPTION 'migration 00037 Down: two employees of one tenant share an address (one of them deactivated); the wider unique index cannot be rebuilt. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

DROP TABLE invite_requests;

ALTER TABLE locations DROP COLUMN active;
ALTER TABLE locations DROP CONSTRAINT locations_tenant_external_ref_key;
ALTER TABLE locations DROP COLUMN external_ref;

DROP TRIGGER employees_directory_clock ON employees;
DROP FUNCTION public.tappa_employees_touch_updated_at();

DROP INDEX employees_tenant_external_ref_key;
DROP INDEX employees_tenant_email_key;
CREATE UNIQUE INDEX employees_tenant_email_key
    ON employees (tenant_id, email) WHERE email IS NOT NULL;

ALTER TABLE employees DROP CONSTRAINT employees_home_or_external_ref;
ALTER TABLE employees ALTER COLUMN location_id SET NOT NULL;

ALTER TABLE employees DROP COLUMN updated_at;
ALTER TABLE employees DROP COLUMN last_name;
ALTER TABLE employees DROP COLUMN first_name;
ALTER TABLE employees DROP COLUMN employee_code;
ALTER TABLE employees DROP COLUMN external_ref;
