-- 00038 -- M11 API-1 (K-22): the two tenant columns behind the integration API's gate.
--
-- NORMATIVE SOURCES: docs/adr/0028-dis-api-anahtar-akis-webhook.md §1 (the matrix row for
-- `tenants`: tappa_app holds NO UPDATE on these columns) and §2 (K-22: the API and the webhooks
-- work for a tenant only once the OPERATOR has switched them on; the one writer of both columns
-- is an op_*, migration 00039).
--
--   api_enabled_at  NULL = the API is closed for this tenant; otherwise the instant it was
--                   opened. Every API path reads it (ADR 0028 §2: "kapı her yolda").
--   api_code        the spec's `company` field (e.g. KF, KM): the short code a punch carries as
--                   employee.company / location.company. NOT unique: uniqueness across tenants
--                   would make writing it a cross-tenant act (00024's argument about vat_number)
--                   and a refusal would tell one tenant about another's code. Upper-case
--                   letters and digits, 2-16 characters, starting with a letter.
--
-- 🔴 WHY THIS FILE HOLDS NO op_* AND 00039 DOES. The columns are ordinary schema; the writer is
-- the operator surface. Kept apart so internal/db's opAtVersion -- which walks the Downs of every
-- later migration to reach an operator version -- can leave this one alone: its Down drops two
-- columns of `tenants` (an ACCESS EXCLUSIVE lock every tenant write would queue behind) and has
-- nothing to do with the operator surface.
--
-- 🔴 tappa_app CANNOT WRITE EITHER COLUMN, and that is a property of the grants, not of a
-- REVOKE here: 00024 left tappa_app column-level INSERT and UPDATE on `tenants`, which a column
-- added later does not inherit -- a TABLE-level grant would. So the precondition refuses a
-- database where tappa_app holds table-level INSERT or UPDATE on tenants (directly or through a
-- role it can use), and the postcondition proves the two new columns are unwritable for it, by
-- column. Either failing rolls the whole file back. (The operator's login role holds nothing on
-- any tenant table; internal/db's catalogue tests pin that, and this file names no operator
-- object -- internal/db's opAtVersion skips it on that premise.)
--
-- 🔴 lock_timeout 1 s, THE FIRST STATEMENT of the Up and of the Down (00054's rule,
-- deploy/README.md; the API-13 security review): ADD COLUMN, ADD CONSTRAINT and DROP COLUMN take
-- ACCESS EXCLUSIVE on tenants, which every request reads. Behind a long reader -- the 02:30
-- backup's dump -- the file gives up with 55P03 (the migrate Job fails, nothing rolls out)
-- instead of queueing every later tenant read behind its lock. Transaction-local: no schema
-- object changes.

-- +goose Up
SET LOCAL lock_timeout = '1s';

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_table_privilege('tappa_app', 'public.tenants', 'UPDATE')
       OR pg_catalog.has_table_privilege('tappa_app', 'public.tenants', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00038 (M11 API-1) adds two columns tappa_app must not write, and tappa_app holds a TABLE-level INSERT or UPDATE on tenants that a new column would fall under. Revoke it by hand, then migrate again. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

ALTER TABLE tenants ADD COLUMN api_enabled_at timestamptz;
ALTER TABLE tenants ADD COLUMN api_code text
    CONSTRAINT tenants_api_code_shape CHECK (api_code ~ '^[A-Z][A-Z0-9]{1,15}$');
-- An open API with no code would publish punches whose `company` is empty: the code comes
-- first (op_enable_tenant_api writes both in one statement).
ALTER TABLE tenants ADD CONSTRAINT tenants_api_needs_a_code
    CHECK (api_enabled_at IS NULL OR api_code IS NOT NULL);

-- +goose StatementBegin
DO $$
BEGIN
    IF pg_catalog.has_column_privilege('tappa_app', 'public.tenants', 'api_enabled_at', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.tenants', 'api_enabled_at', 'INSERT')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.tenants', 'api_code', 'UPDATE')
       OR pg_catalog.has_column_privilege('tappa_app', 'public.tenants', 'api_code', 'INSERT') THEN
        RAISE EXCEPTION 'migration 00038 (M11 API-1): tappa_app could write the new API columns. Nothing was changed.'
            USING ERRCODE = 'object_not_in_prerequisite_state';
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
SET LOCAL lock_timeout = '1s';

-- The columns go with what they held: the operator's decisions are in operator_audit_log and in
-- the tenants' own audit_log rows (00039), which stay.
ALTER TABLE tenants DROP CONSTRAINT tenants_api_needs_a_code;
ALTER TABLE tenants DROP COLUMN api_code;
ALTER TABLE tenants DROP COLUMN api_enabled_at;
